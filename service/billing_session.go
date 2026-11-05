package service

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// BillingSession — 统一计费会话
// ---------------------------------------------------------------------------

// BillingSession 封装单次请求的预扣费/结算/退款生命周期。
// 实现 relaycommon.BillingSettler 接口。
type BillingSession struct {
	relayInfo        *relaycommon.RelayInfo
	funding          FundingSource
	durable          bool   // 统一来源请求的资金与令牌调整必须同步确认。
	preConsumedQuota int    // 实际预扣额度（信任用户可能为 0）
	tokenConsumed    int    // 令牌额度实际扣减量
	extraReserved    int    // 发送前补充预扣的额度（订阅退款时需要单独回滚）
	trusted          bool   // 是否命中信任额度旁路
	fundingSettled   bool   // funding.Settle 已成功，资金来源已提交
	settled          bool   // Settle 全部完成（资金 + 令牌）
	refunded         bool   // Refund 已调用
	refundErr        error  // 同步退款的未知结果不可因再次调用变成成功
	lang             string // 请求声明的语言，为空表示未声明
	mu               sync.Mutex
}

// Settle 根据实际消耗额度进行结算。
// 资金来源和令牌额度分两步提交：若资金来源已提交但令牌调整失败，
// 会标记 fundingSettled 防止 Refund 对已提交的资金来源执行退款。
// 事件观察记录整笔总额，资金来源方法仍按补扣或退还差额执行。
// 生产钱包与订阅由主库原子提交，通知功能不能使两者变成双重扣费。
// 零差额也有最终事实，必须覆盖预扣恰好匹配实际费用的正常请求。
// 跨会话幂等命中跳过Key二次扣减，不能只保护用户余额。
// 缓存确认失败时数据库可能已完成，因此冻结后续自动退款与重试。
// 同步资金成功后Key更新失败保留既有错误，不把实际消费误退回用户。
// 自定义资金来源不假设其实现使用本地主库，继续遵循原接口合同。
// 异步任务观察仅在完成后晋级，提交阶段不会拿估价告警。
func (s *BillingSession) Settle(actualQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled {
		return nil
	}
	// 已退款的会话不能被迟到的结算重新扣费。
	if s.refunded {
		return errors.New("billing session already refunded")
	}
	delta := actualQuota - s.preConsumedQuota
	// 事件捕获需要保存零差额的最终事实，不能提前返回漏掉整笔样本。
	// 捕获关闭时保留原有轻量路径，独立测试工具无需事件表。
	if delta == 0 && !model.HubCaptureEnabled() {
		s.settled = true
		return nil
	}
	// 1) 调整资金来源（仅在尚未提交时执行，防止重复调用）
	if !s.fundingSettled {
		var fundingErr error
		fundingApplied := true
		// 生产计费在资金事务中保存观察，避免提交后进程退出漏告警。
		// 自定义 FundingSource 仍遵循原接口合同，不能猜测其数据库实现。
		switch s.funding.(type) {
		case *WalletFunding, *SubscriptionFunding:
			if model.HubCaptureEnabled() {
				fundingApplied, fundingErr = settleHubBillingFunding(s.relayInfo, actualQuota, delta)
			} else {
				fundingErr = s.funding.Settle(delta)
			}
		default:
			fundingErr = s.funding.Settle(delta)
		}
		if fundingErr != nil {
			err := fundingErr
			// 同步会话的数据库或缓存确认失败可能发生在提交之后。
			// 禁止随后再次退款或结算，把该请求留给对账而非猜测资金状态。
			if s.durable || model.HubCaptureEnabled() {
				s.refunded, s.refundErr = true, err
			}
			return err
		}
		s.fundingSettled = true
		// 跨进程重投已由资金观察认领，不能在这里再次扣 Key 预算。
		// 同实例已完成调用仍由函数最前面的 settled 标志处理。
		if !fundingApplied {
			s.settled = true
			return nil
		}
	}
	// 2) 调整令牌额度
	var tokenErr error
	if !s.relayInfo.IsPlayground {
		tokenErr = s.adjustTokenQuota(-delta)
		if tokenErr != nil {
			// 资金来源已提交，令牌调整失败只能记录日志；标记 settled 防止 Refund 误退资金
			common.SysLog(common.LogText("error adjusting token quota after funding settled (userId=%d, tokenId=%d, delta=%d): %s",
				s.relayInfo.UserId, s.relayInfo.TokenId, delta, tokenErr.Error()))
		}
	}
	// 3) 更新 relayInfo 上的订阅 PostDelta（用于日志）
	if s.funding.Source() == BillingSourceSubscription {
		s.relayInfo.SubscriptionPostDelta += int64(delta)
	}
	s.settled = true
	return tokenErr
}

// Refund 退还所有预扣费，幂等安全，异步执行。
func (s *BillingSession) Refund(c *gin.Context) {
	s.mu.Lock()
	if s.settled || s.refunded || !s.needsRefundLocked() {
		s.mu.Unlock()
		return
	}
	s.refunded = true
	s.mu.Unlock()

	logger.LogInfo(c, common.LogText("user %d request failed, refunding the pre-consumed quota (token_quota=%s, funding=%s)",
		s.relayInfo.UserId,
		logger.FormatQuota(s.tokenConsumed),
		s.funding.Source(),
	))

	// 复制需要的值到闭包中
	tokenId := s.relayInfo.TokenId
	tokenKey := s.relayInfo.TokenKey
	isPlayground := s.relayInfo.IsPlayground
	tokenConsumed := s.tokenConsumed
	extraReserved := s.extraReserved
	subscriptionId := s.relayInfo.SubscriptionId
	funding := s.funding

	gopool.Go(func() {
		// 1) 退还资金来源
		if err := funding.Refund(); err != nil {
			common.SysLog(common.LogText("error refunding billing source: %s", err.Error()))
		}
		if extraReserved > 0 && funding.Source() == BillingSourceSubscription && subscriptionId > 0 {
			if err := model.PostConsumeUserSubscriptionDelta(subscriptionId, -int64(extraReserved)); err != nil {
				common.SysLog(common.LogText("error refunding subscription extra reserved quota: %s", err.Error()))
			}
		}
		// 2) 退还令牌额度
		if tokenConsumed > 0 && !isPlayground {
			if err := model.IncreaseTokenQuota(tokenId, tokenKey, tokenConsumed); err != nil {
				common.SysLog(common.LogText("error refunding token quota: %s", err.Error()))
			}
		}
	})
}

// NeedsRefund 返回是否存在需要退还的预扣状态。
func (s *BillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsRefundLocked()
}

func (s *BillingSession) needsRefundLocked() bool {
	if s.settled || s.refunded || s.fundingSettled {
		// fundingSettled 时资金来源已提交结算，不能再退预扣费
		return false
	}
	if s.tokenConsumed > 0 {
		return true
	}
	// 免费平台模型或内部调用可不消费令牌，但钱包预留仍必须可退款。
	if wallet, ok := s.funding.(*WalletFunding); ok && wallet.consumed > 0 {
		return true
	}
	// 订阅可能在 tokenConsumed=0 时仍预扣了额度
	if sub, ok := s.funding.(*SubscriptionFunding); ok && sub.preConsumed > 0 {
		return true
	}
	return false
}

// GetPreConsumedQuota 返回实际预扣的额度。
func (s *BillingSession) GetPreConsumedQuota() int {
	return s.preConsumedQuota
}

func (s *BillingSession) Reserve(targetQuota int) error {
	return s.reserve(targetQuota, false)
}

// ReserveBeforeSubmit 在发起上游请求前补扣，钱包余额必须覆盖差额。
func (s *BillingSession) ReserveBeforeSubmit(targetQuota int) error {
	return s.reserve(targetQuota, true)
}

func (s *BillingSession) reserve(targetQuota int, requireAvailableQuota bool) error {
	// 统一来源的任何发送前补预留都必须覆盖差额，不能借旧 Reserve 语义透支。
	requireAvailableQuota = requireAvailableQuota || s.durable
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.relayInfo != nil {
		_, imageRequest := s.relayInfo.Request.(*dto.ImageRequest)
		requireAvailableQuota = requireAvailableQuota || imageRequest || s.relayInfo.ImageRequestCount > 0
	}
	if s.settled || s.refunded || s.trusted && !requireAvailableQuota || targetQuota <= s.preConsumedQuota {
		return nil
	}

	delta := targetQuota - s.preConsumedQuota
	if delta <= 0 {
		return nil
	}

	if err := s.reserveFunding(delta, requireAvailableQuota); err != nil {
		return err
	}
	if err := s.reserveToken(delta); err != nil {
		if rollbackErr := s.rollbackFundingReserve(delta); rollbackErr != nil {
			// 补扣回滚可能已提交，不能随后再次全额退款而重复加回差额。
			// 冻结整个会话并保留未知结果，由错误日志关联原请求进行对账。
			s.refunded, s.refundErr = true, rollbackErr
			return types.NewError(rollbackErr, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		return err
	}

	s.preConsumedQuota += delta
	s.tokenConsumed += delta
	s.extraReserved += delta
	if requireAvailableQuota {
		s.trusted = false
	}
	s.syncRelayInfo()
	return nil
}

// ---------------------------------------------------------------------------
// PreConsume — 统一预扣费入口（含信任额度旁路）
// ---------------------------------------------------------------------------

// preConsume 执行预扣费：信任检查 -> 令牌预扣 -> 资金来源预扣。
// 任一步骤失败时原子回滚已完成的步骤。
func (s *BillingSession) preConsume(c *gin.Context, quota int) *types.NewAPIError {
	effectiveQuota := quota

	// ---- 信任额度旁路 ----
	if s.shouldTrust(c) {
		s.trusted = true
		effectiveQuota = 0
		logger.LogInfo(c, common.LogText("user %d has enough quota, trusted without pre-consume (funding=%s)", s.relayInfo.UserId, s.funding.Source()))
	} else if effectiveQuota > 0 {
		logger.LogInfo(c, common.LogText("user %d needs pre-consume %s (funding=%s)", s.relayInfo.UserId, logger.FormatQuota(effectiveQuota), s.funding.Source()))
	}

	// ---- 1) 预扣令牌额度 ----
	if effectiveQuota > 0 {
		if err := preConsumeTokenQuota(s.relayInfo, effectiveQuota, s.durable); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		s.tokenConsumed = effectiveQuota
	}

	// ---- 2) 预扣资金来源 ----
	if err := s.funding.PreConsume(effectiveQuota); err != nil {
		// 预扣费失败，回滚令牌额度
		if s.tokenConsumed > 0 && !s.relayInfo.IsPlayground {
			if rollbackErr := s.adjustTokenQuota(s.tokenConsumed); rollbackErr != nil {
				common.SysLog(common.LogText("error rolling back token quota (userId=%d, tokenId=%d, amount=%d, fundingErr=%s): %s",
					s.relayInfo.UserId, s.relayInfo.TokenId, s.tokenConsumed, err.Error(), rollbackErr.Error()))
				// 回滚失败不是额度不足，必须阻止后续资金来源继续执行。
				return types.NewError(rollbackErr, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
			}
			s.tokenConsumed = 0
		}
		if errors.Is(err, ErrInsufficientWalletQuota) {
			userQuota, quotaErr := model.GetUserQuota(s.relayInfo.UserId, false)
			if quotaErr != nil {
				userQuota = 0
			}
			return types.NewErrorWithStatusCode(
				errors.New(i18n.T(c, i18n.MsgQuotaUserInsufficient, map[string]any{"Remaining": logger.FormatQuota(userQuota)})),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		errMsg := err.Error()
		if errors.Is(err, model.ErrNoActiveSubscription) || errors.Is(err, model.ErrInsufficientSubscriptionQuota) {
			return types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgQuotaSubscriptionInsufficient, map[string]any{"Error": errMsg})), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}

	s.preConsumedQuota = effectiveQuota

	// ---- 同步 RelayInfo 兼容字段 ----
	s.syncRelayInfo()

	return nil
}

func (s *BillingSession) reserveFunding(delta int, requireAvailableQuota bool) error {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		if requireAvailableQuota {
			// Image quantities and task retry prices are known before
			// submission. Reserve atomically instead of admitting wallet debt.
			if err := funding.PreConsume(delta); err != nil {
				if errors.Is(err, ErrInsufficientWalletQuota) {
					return types.NewErrorWithStatusCode(err, types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
				}
				return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
			}
			return nil
		}
		// 与结算补扣（SettleBilling 正差额 → WalletFunding.Settle）语义一致：
		// 全额无条件扣减，余额不足的部分记为欠费（余额可为负），不中断请求，
		// 保证日志记录的预扣额度与用户余额的实际变动始终对账一致。
		// DecreaseUserQuota 仅在数据库错误时失败。
		if err := model.DecreaseUserQuota(funding.userId, delta, false); err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		funding.consumed += delta
		return nil
	case *SubscriptionFunding:
		if err := model.PostConsumeUserSubscriptionDelta(funding.subscriptionId, int64(delta)); err != nil {
			// 仅确定的额度上限错误允许回退，数据库和提交未知必须停止。
			if !errors.Is(err, model.ErrSubscriptionLimitExceeded) {
				return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
			}
			return types.NewErrorWithStatusCode(
				errors.New(i18n.Translate(s.lang, i18n.MsgQuotaSubscriptionInsufficient, map[string]any{"Error": err.Error()})),
				types.ErrorCodeInsufficientUserQuota,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}
		return nil
	default:
		return types.NewError(fmt.Errorf("unsupported funding source: %s", s.funding.Source()), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
}

func (s *BillingSession) rollbackFundingReserve(delta int) error {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		var err error
		if s.durable {
			err = model.ApplyUserQuotaDeltaSync(funding.userId, delta)
		} else {
			err = model.IncreaseUserQuota(funding.userId, delta, false)
		}
		if err != nil {
			return err
		} else {
			funding.consumed -= delta
		}
	case *SubscriptionFunding:
		if err := model.PostConsumeUserSubscriptionDelta(funding.subscriptionId, -int64(delta)); err != nil {
			return err
		}
	}
	return nil
}

func (s *BillingSession) reserveToken(delta int) error {
	if delta <= 0 || s.relayInfo.IsPlayground {
		return nil
	}
	if err := preConsumeTokenQuota(s.relayInfo, delta, s.durable); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	return nil
}

// shouldTrust 统一信任额度检查，适用于钱包和订阅。
func (s *BillingSession) shouldTrust(c *gin.Context) bool {
	// 异步任务（ForcePreConsume=true）必须预扣全额，不允许信任旁路
	if s.relayInfo.ForcePreConsume {
		return false
	}

	trustQuota := operation_setting.GetQuotaSetting().TrustQuotaUSD * common.QuotaPerUnit
	if trustQuota <= 0 || math.IsNaN(trustQuota) || math.IsInf(trustQuota, 0) {
		return false
	}

	// 检查令牌是否充足
	tokenTrusted := s.relayInfo.TokenUnlimited
	if !tokenTrusted {
		tokenQuota := c.GetInt("token_quota")
		tokenTrusted = float64(tokenQuota) > trustQuota
	}
	if !tokenTrusted {
		return false
	}

	switch s.funding.Source() {
	case BillingSourceWallet:
		return float64(s.relayInfo.UserQuota) > trustQuota
	case BillingSourceSubscription:
		// 订阅不能启用信任旁路。原因：
		// 1. PreConsumeUserSubscription 要求 amount>0 来创建预扣记录并锁定订阅
		// 2. SubscriptionFunding.PreConsume 忽略参数，始终用 s.amount 预扣
		// 3. 若信任旁路将 effectiveQuota 设为 0，会导致 preConsumedQuota 与实际订阅预扣不一致
		return false
	default:
		return false
	}
}

// syncRelayInfo 将 BillingSession 的状态同步到 RelayInfo 的兼容字段上。
func (s *BillingSession) syncRelayInfo() {
	info := s.relayInfo
	info.FinalPreConsumedQuota = s.preConsumedQuota
	info.BillingSource = s.funding.Source()

	if sub, ok := s.funding.(*SubscriptionFunding); ok {
		info.SubscriptionId = sub.subscriptionId
		info.SubscriptionPreConsumed = sub.preConsumed + int64(s.extraReserved)
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = sub.AmountTotal
		info.SubscriptionAmountUsedAfterPreConsume = sub.AmountUsedAfter + int64(s.extraReserved)
		info.SubscriptionPlanId = sub.PlanId
		info.SubscriptionPlanTitle = sub.PlanTitle
	} else {
		info.SubscriptionId = 0
		info.SubscriptionPreConsumed = 0
	}
}

// ---------------------------------------------------------------------------
// NewBillingSession 工厂 — 根据计费偏好创建会话并处理回退
// ---------------------------------------------------------------------------

// NewBillingSession 根据用户计费偏好创建 BillingSession，处理 subscription_first / wallet_first 的回退。
func newBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError) {
	if relayInfo == nil {
		return nil, types.NewError(fmt.Errorf("relayInfo is nil"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	pref := common.NormalizeBillingPreference(relayInfo.UserSetting.BillingPreference)
	// 调度层显式指定来源时冻结本次计费，不修改用户持久化偏好。
	// 只允许两个精确来源，未知值必须拒绝，不能偷偷按默认偏好收费。
	if source := c.GetString("oak.billing_source"); source != "" {
		switch source {
		case BillingSourceWallet:
			pref = "wallet_only"
		case BillingSourceSubscription:
			pref = "subscription_only"
		default:
			return nil, types.NewError(errors.New("invalid resource funding source"), types.ErrorCodeInvalidRequest)
		}
		relayInfo.ForcePreConsume = true
		// 表达式可能估算为零，但收费请求仍不能使用已耗尽的有限 Key。
		// 鉴权为免费挂靠保留身份，因此在确定平台收费后恢复这项预算约束。
		// 非零预扣由原子扣减检查；这里只补零估算无法触发扣减的入口。
		// 直接读取当前 Key，WebSocket 下一次生成不能沿用前一次握手预算。
		// 免费免预扣请求在进入工厂之前分流，不受这项收费预算门槛影响。
		if preConsumedQuota == 0 && !relayInfo.IsPlayground && !relayInfo.TokenUnlimited {
			token, err := model.GetTokenById(relayInfo.TokenId)
			if err != nil {
				return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
			}
			if !token.UnlimitedQuota && token.RemainQuota <= 0 {
				return nil, types.NewErrorWithStatusCode(ErrInsufficientTokenBudget, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry())
			}
		}
	}

	// 钱包路径需要先检查用户额度
	tryWallet := func() (*BillingSession, *types.NewAPIError) {
		userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if userQuota <= 0 {
			return nil, types.NewErrorWithStatusCode(
				errors.New(i18n.T(c, i18n.MsgQuotaUserInsufficient, map[string]any{"Remaining": logger.FormatQuota(userQuota)})),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		if userQuota-preConsumedQuota < 0 {
			return nil, types.NewErrorWithStatusCode(
				errors.New(i18n.T(c, i18n.MsgQuotaPreConsumeInsufficient, map[string]any{"Remaining": logger.FormatQuota(userQuota), "Required": logger.FormatQuota(preConsumedQuota)})),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		relayInfo.UserQuota = userQuota

		session := &BillingSession{
			relayInfo: relayInfo,
			funding:   &WalletFunding{userId: relayInfo.UserId, durable: c.GetString("oak.billing_source") != ""},
			durable:   c.GetString("oak.billing_source") != "",
			lang:      i18n.StatedLang(c),
		}
		if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	trySubscription := func() (*BillingSession, *types.NewAPIError) {
		subConsume := int64(preConsumedQuota)
		if subConsume <= 0 {
			subConsume = 1
		}
		session := &BillingSession{
			relayInfo: relayInfo,
			lang:      i18n.StatedLang(c),
			durable:   c.GetString("oak.billing_source") != "",
			funding: &SubscriptionFunding{
				requestId: relayInfo.RequestId,
				userId:    relayInfo.UserId,
				modelName: relayInfo.GetBillingModelName(),
				amount:    subConsume,
			},
		}
		// 必须传 subConsume 而非 preConsumedQuota，保证 SubscriptionFunding.amount、
		// preConsume 参数和 FinalPreConsumedQuota 三者一致，避免订阅多扣费。
		if apiErr := session.preConsume(c, int(subConsume)); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	switch pref {
	case "subscription_only":
		return trySubscription()
	case "wallet_only":
		return tryWallet()
	case "wallet_first":
		session, err := tryWallet()
		if err != nil {
			if err.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				return trySubscription()
			}
			return nil, err
		}
		return session, nil
	case "subscription_first":
		fallthrough
	default:
		hasSub, subCheckErr := model.HasActiveUserSubscription(relayInfo.UserId)
		if subCheckErr != nil {
			return nil, types.NewError(subCheckErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if !hasSub {
			return tryWallet()
		}
		session, apiErr := trySubscription()
		if apiErr != nil {
			if apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				// 仅当用户的活跃订阅允许钱包回退时才回退到钱包，否则返回订阅额度不足错误
				allowOverflow, overflowErr := model.UserActiveSubscriptionsAllowWalletOverflow(relayInfo.UserId)
				if overflowErr != nil {
					return nil, types.NewError(overflowErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
				}
				if allowOverflow {
					return tryWallet()
				}
				return nil, apiErr
			}
			return nil, apiErr
		}
		return session, nil
	}
}

// RefundSync 在切换资源前完成退款，并把未知结果保留为失败。
// 资金与令牌均成功释放才允许继续；钱包加额非幂等，绝不自动重试。
// 与旧异步 Refund 共用互斥状态，成功结算后不会再退费。
// 该方法只用于尚未成功结算的请求；已经提交资金来源时禁止退款。
// 一次退款包含初次预扣、订阅补充预留以及令牌预算三个部分。
// 部分失败会尝试释放其他独立预留，但最终结果仍然是失败。
// 错误会被缓存，后续调用不会再次执行非幂等钱包加额。
// 不采用异步 goroutine，因为外层必须依据返回结果决定能否回退。
// 订阅自身有操作 ID 幂等保护，其内部有限重试沿用原合同。
// 此方法不能用来修复已成功回答后结算失败的资金来源。
// 发生未知提交结果时需要对账，不允许通过重新调用会话猜测成功。
func (s *BillingSession) RefundSync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refunded {
		return s.refundErr
	}
	if s.settled || s.fundingSettled || !s.needsRefundLocked() {
		return nil
	}
	// 先占用退款状态，防止网络结果未知时被重复调用而多退额度。
	s.refunded = true
	var failures []error
	if err := s.funding.Refund(); err != nil {
		failures = append(failures, err)
	}
	// 订阅初次预留与补充预留使用不同记录，必须分别释放。
	if s.extraReserved > 0 && s.funding.Source() == BillingSourceSubscription && s.relayInfo.SubscriptionId > 0 {
		if err := model.PostConsumeUserSubscriptionDelta(s.relayInfo.SubscriptionId, -int64(s.extraReserved)); err != nil {
			failures = append(failures, err)
		}
	}
	// 即使资金退款失败也尝试释放令牌预算，返回合并错误供人工核对。
	if s.tokenConsumed > 0 && !s.relayInfo.IsPlayground {
		if err := s.adjustTokenQuota(s.tokenConsumed); err != nil {
			failures = append(failures, err)
		}
	}
	s.refundErr = errors.Join(failures...)
	return s.refundErr
}

// adjustTokenQuota 对本次资源会话使用同步持久化，历史计费仍走原批量合同。
// 正数释放预算、负数追加消费，调用方提供的数量已由统一计费边界约束。
func (s *BillingSession) adjustTokenQuota(delta int) error {
	if s.durable { // 请求级合同决定持久性，不修改全局批量开关。
		return model.ApplyTokenQuotaDeltaSync(s.relayInfo.TokenId, s.relayInfo.TokenKey, delta)
	}
	if delta >= 0 { // 历史请求仍使用原有释放入口及其缓存规则。
		return model.IncreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, delta)
	}
	return model.DecreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, -delta)
}
