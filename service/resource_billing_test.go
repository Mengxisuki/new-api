package service

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 显式来源不能被用户旧偏好重排，退款必须同步释放真实钱包和令牌。
// 重复退款与退款后再次结算不得产生额外加额/扣款。
// 用户旧偏好故意设为仅订阅，与本次显式钱包来源相反。
// 会话必须执行外层已经选择的来源，不能再次按旧偏好选择。
// 创建真实用户和 Key 预算，让预扣金额可以从数据库直接核对。
// ForcePreConsume 说明统一来源不能依赖可能漏记的信任额度跳过预留。
// 连续退款用于复现超时处理和结束处理都触发清理的情形。
// 退款后钱包和 Key 都必须回到初始预算，不能多退一份。
// 已退款的会话不能再次结算，防止一个请求生命周期产生反向重复记账。
func TestResourceBillingExplicitWalletAndRefund(t *testing.T) {
	truncate(t)
	seedUser(t, 901, 1000)
	seedToken(t, 901, 901, "resource-billing", 1000)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("oak.billing_source", "wallet") // 模拟外层已明确选择的钱包来源，本层不能重新排序。
	info := &relaycommon.RelayInfo{UserId: 901, TokenId: 901, TokenKey: "resource-billing", UserSetting: dto.UserSetting{BillingPreference: "subscription_only"}}
	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	require.True(t, info.ForcePreConsume) // 统一来源必须先得到可靠预算，不能走信任额度捷径。
	require.Equal(t, "wallet", info.BillingSource)
	quota, err := model.GetUserQuota(901, false)
	require.NoError(t, err)
	require.Equal(t, 900, quota)
	require.NoError(t, session.RefundSync())
	require.NoError(t, session.RefundSync())
	quota, err = model.GetUserQuota(901, false)
	require.NoError(t, err)
	require.Equal(t, 1000, quota)
	token, err := model.GetTokenById(901)
	require.NoError(t, err)
	require.Equal(t, 1000, token.RemainQuota)
	require.Error(t, session.Settle(100)) // 已结束的退款生命周期不能再次反向扣款。
}

// 当显式订阅无额度时不内置切换钱包，留给外层排序处理。
// 预扣失败后的令牌回滚完成后才可返回不足，真实数据库故障必须阻止回退。
// 钱包余额足够但没有可用订阅，确保不会因钱包不足而偶然拒绝回退。
// 旧偏好设为仅钱包，显式订阅来源仍然必须约束本次预扣。
// 订阅失败要返回没有可执行会话，不能留下已批准的付费对象。
// 用户钱包保持不变，证明内部没有暗中切换资金来源。
// Key 可能先被预扣，因此失败前必须同步释放这部分预算。
// 断言重新读取数据库，不通过错误码推测退款是否真正完成。
// 外层是否继续下一个来源由其策略决定，本层只报告当前来源结果。
func TestResourceBillingSubscriptionFailureRestoresToken(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionPreConsumeRecord{}))
	seedUser(t, 902, 1000)
	seedToken(t, 902, 902, "resource-sub", 1000)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("oak.billing_source", "subscription") // 来源来自已授权策略，旧用户偏好不得覆盖。
	info := &relaycommon.RelayInfo{UserId: 902, TokenId: 902, TokenKey: "resource-sub", RequestId: "resource-sub-one", UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, session) // 失败不能留下可继续结算的半成品会话。
	require.NotNil(t, apiErr)
	quota, err := model.GetUserQuota(902, false)
	require.NoError(t, err)
	require.Equal(t, 1000, quota)
	token, err := model.GetTokenById(902)
	require.NoError(t, err)
	require.Equal(t, 1000, token.RemainQuota)
}

// 退款写库失败后的结果必须持久在会话内，重试不能重复钱包加额。
// 只隔离数据库错误边界，不替换真实预扣/退款实现。
// 先真实预扣成功，再只在用户表更新边界注入数据库写入失败。
// 故障并未替换退款实现，因此仍验证真实状态推进与错误保存。
// 移除故障后再次退款，不能把第一次未知结果简单当作从未发生。
// 会话必须保留原错误，防止重复加额后向调用者伪报成功。
// 数据库余额停留在预扣后的值，表明需要进一步对账而非猜测性恢复。
// 测试清理移除回调，避免故障注入影响其他计费用例。
// 此场景只证明当前会话内不会重复退款，不宣称跨进程对账已经完成。
func TestResourceBillingRefundFailureCannotBecomeSuccess(t *testing.T) {
	truncate(t)
	seedUser(t, 903, 1000)
	seedToken(t, 903, 903, "resource-refund", 1000)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("oak.billing_source", "wallet") // 模拟外层已明确选择的钱包来源，本层不能重新排序。
	info := &relaycommon.RelayInfo{UserId: 903, TokenId: 903, TokenKey: "resource-refund"}
	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	injected := errors.New("database write unavailable")
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("resource-refund-failure", func(db *gorm.DB) {
		if db.Statement.Table == "users" {
			db.AddError(injected)
		}
	}))
	t.Cleanup(func() { model.DB.Callback().Update().Remove("resource-refund-failure") })
	require.ErrorIs(t, session.RefundSync(), injected) // 重试必须保留第一次失败事实而非伪造退款成功。
	model.DB.Callback().Update().Remove("resource-refund-failure")
	require.ErrorIs(t, session.RefundSync(), injected) // 重试必须保留第一次失败事实而非伪造退款成功。
	quota, err := model.GetUserQuota(903, false)
	require.NoError(t, err)
	require.Equal(t, 900, quota)
}

// 付费预算不足应带稳定哨兵，鉴权可据此保留免费来源。
// 此处验证原子预留未发生，后续免费调用不会继承负预算。
// 用户钱包充足而 Key 预算只有一个单位，隔离限制究竟来自哪一层。
// 请求预留金额明显超过 Key 余额，必须在扣钱包之前确定拒绝。
// 稳定预算不足哨兵让外层可以区别可跳过候选与真实系统故障。
// 失败不能留下一个看似可执行的会话，避免后续误用已拒绝预算。
// 用户钱包保持原值，证明没有先扣后忘记退款的副作用。
// Key 自身预算与启用状态都不改变，免费来源仍可使用合法身份。
// 该用例不把预算不足升级为永久禁用，金额限制与身份限制分别检查。
func TestResourceBillingTokenInsufficientDoesNotReserveWallet(t *testing.T) {
	truncate(t)
	seedUser(t, 904, 1000)
	seedToken(t, 904, 904, "resource-small", 1)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("oak.billing_source", "wallet") // 模拟外层已明确选择的钱包来源，本层不能重新排序。
	info := &relaycommon.RelayInfo{UserId: 904, TokenId: 904, TokenKey: "resource-small"}
	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, session) // 失败不能留下可继续结算的半成品会话。
	require.ErrorIs(t, apiErr, ErrInsufficientTokenBudget)
	quota, err := model.GetUserQuota(904, false)
	require.NoError(t, err)
	require.Equal(t, 1000, quota)
	token, err := model.GetTokenById(904)
	require.NoError(t, err)
	require.Equal(t, 1, token.RemainQuota)
	require.Equal(t, common.TokenStatusEnabled, token.Status) // 预算不足不能升级为永久身份禁用。
}

// 有效包月来源优先时仅消费订阅，即使旧偏好钱包优先也不改变来源。
// 实际用量超出预留仍结算到同一个订阅，不能在响应后从钱包补差。
// 创建真实套餐和用户订阅，并建立计划关联，使预扣走完整订阅链路。
// 旧偏好钱包优先与本次显式订阅相反，可发现二次选择来源的错误。
// 初次预留小于最终用量，覆盖结算补差而非只有等额路径。
// 补差仍必须落在原订阅，不能在已经输出后转扣钱包。
// 结算后再调用退款应保持终态，不应把已消费订阅额度还回去。
// 订阅用量、钱包余额和 Key 剩余额度分别核对各自职责。
// 测试结束清理新增计划与预扣记录，避免后续用例误匹配残留订阅。
func TestResourceBillingExplicitSubscriptionSettlesSameSource(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionPreConsumeRecord{}))
	seedUser(t, 905, 1000)
	seedToken(t, 905, 905, "resource-paid-sub", 1000)
	plan := &model.SubscriptionPlan{Title: "resource-plan", Enabled: true, TotalAmount: 1000, QuotaResetPeriod: "never"}
	require.NoError(t, model.DB.Create(plan).Error)
	t.Cleanup(func() {
		model.DB.Delete(plan)
		model.DB.Where("user_id = ?", 905).Delete(&model.SubscriptionPreConsumeRecord{})
	})
	seedSubscription(t, 905, 905, 1000, 0)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", 905).Update("plan_id", plan.Id).Error)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("oak.billing_source", "subscription") // 来源来自已授权策略，旧用户偏好不得覆盖。
	info := &relaycommon.RelayInfo{UserId: 905, TokenId: 905, TokenKey: "resource-paid-sub", RequestId: "resource-sub-paid", UserSetting: dto.UserSetting{BillingPreference: "wallet_first"}}
	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	require.Equal(t, "subscription", info.BillingSource)
	require.NoError(t, session.Settle(150))
	require.NoError(t, session.RefundSync())
	var sub model.UserSubscription
	require.NoError(t, model.DB.First(&sub, 905).Error)
	require.EqualValues(t, 150, sub.AmountUsed) // 实际用量补差只能落到原订阅。
	quota, err := model.GetUserQuota(905, false)
	require.NoError(t, err)
	require.Equal(t, 1000, quota)
	token, err := model.GetTokenById(905)
	require.NoError(t, err)
	require.Equal(t, 850, token.RemainQuota) // Key 记账反映最终用量，不能停在最初预留。
}

// 查询订阅故障属于系统错误，不能伪装成无套餐并回退钱包。
// 定向注入查询失败还验证了已预扣令牌会同步恢复。
// 故障只作用于用户订阅查询，钱包与 Key 查询仍使用真实数据库。
// 没有可读订阅不等于没有订阅，错误必须保留真实根因。
// 外层必须收到系统错误，不能把它当作额度不足再调用钱包。
// 预扣流程已经触碰 Key 时，失败仍需要把预算同步还原。
// 钱包充足便于发现任何未经授权的内部付费回退。
// 用数据库余额确认回滚完成，而不是仅验证错误类型。
// 回调注册和删除配对，避免注入故障污染随后其他服务测试。
func TestResourceBillingSubscriptionQueryErrorStopsFallback(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionPreConsumeRecord{}))
	seedUser(t, 906, 1000)
	seedToken(t, 906, 906, "resource-sub-db-error", 1000)
	injected := errors.New("subscription database unavailable")
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("resource-sub-query", func(db *gorm.DB) {
		if db.Statement.Table == "user_subscriptions" {
			db.AddError(injected)
		}
	}))
	t.Cleanup(func() { model.DB.Callback().Query().Remove("resource-sub-query") })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("oak.billing_source", "subscription") // 来源来自已授权策略，旧用户偏好不得覆盖。
	info := &relaycommon.RelayInfo{UserId: 906, TokenId: 906, TokenKey: "resource-sub-db-error", RequestId: "resource-sub-db-error"}
	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, session)              // 失败不能留下可继续结算的半成品会话。
	require.ErrorIs(t, apiErr, injected) // 系统故障需保留根因，避免外层错误启动收费回退。
	quota, err := model.GetUserQuota(906, false)
	require.NoError(t, err)
	require.Equal(t, 1000, quota)
	token, err := model.GetTokenById(906)
	require.NoError(t, err)
	require.Equal(t, 1000, token.RemainQuota)
}

// 回滚写入已提交但调用方收到错误时，禁止随后再次全额退款。
// 错误注入在真实数据库提交之后，复现响应丢失而非简单未执行失败。
// 先完成一笔小预留，再请求超过剩余 Key 预算的追加预留。
// 注入点位于真实数据库提交之后，模拟提交已发生但确认响应丢失。
// 第二次用户表写入对应追加预留的回滚，测试故意让这个确认变得未知。
// 移除故障后不代表历史结果已确定，后续全额退款必须冻结。
// 保持初次预留是为了等待对账，不能用猜测性的余额恢复掩盖差额。
// 用户与 Key 均保留相应初始预留，避免一侧退回而另一侧仍扣住。
// 错误必须向上传播并保持身份，禁止将未知回滚重新解释为普通不足。
func TestResourceBillingUnknownReserveRollbackFreezesRefund(t *testing.T) {
	truncate(t)
	seedUser(t, 907, 1000)
	seedToken(t, 907, 907, "resource-rollback", 100)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("oak.billing_source", "wallet") // 模拟外层已明确选择的钱包来源，本层不能重新排序。
	info := &relaycommon.RelayInfo{UserId: 907, TokenId: 907, TokenKey: "resource-rollback"}
	session, apiErr := NewBillingSession(ctx, info, 50)
	require.Nil(t, apiErr)
	writes := 0
	unknown := errors.New("wallet rollback acknowledgment lost")
	require.NoError(t, model.DB.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("resource-unknown-rollback", func(db *gorm.DB) {
		if db.Statement.Table == "users" {
			writes++
			if writes == 2 {
				db.AddError(unknown)
			}
		}
	}))
	t.Cleanup(func() { model.DB.Callback().Update().Remove("resource-unknown-rollback") })
	require.ErrorIs(t, session.ReserveBeforeSubmit(200), unknown)
	model.DB.Callback().Update().Remove("resource-unknown-rollback")
	quota, err := model.GetUserQuota(907, false)
	require.NoError(t, err)
	require.Equal(t, 950, quota)
	require.ErrorIs(t, session.RefundSync(), unknown) // 确认丢失后的未知结果不能靠再退一次来消除。
	quota, err = model.GetUserQuota(907, false)
	require.NoError(t, err)
	require.Equal(t, 950, quota)
	// 初次预留保留待对账，不能用猜测性的退款掩盖未确认差额。
	token, err := model.GetTokenById(907)
	require.NoError(t, err)
	require.Equal(t, 50, token.RemainQuota) // 原先确定的预留仍需等待对账而不能猜测释放。
}

// 补预留时订阅数据库异常不等于额度不足，不能触发外层钱包回退。
// 同一真实调用入口仍把确定的额度超限区分为可跳过候选。
// 同一真实 ReserveBeforeSubmit 入口先验证确定超限，再验证查询故障。
// 初始订阅只余一半额度，追加较大金额应得到明确的不足类别。
// 随后追加较小金额本应可用，只因数据库读取故障而失败。
// 后者必须归为更新数据错误，不能继承前一次不足判断。
// 外层调度依赖这个分类决定是否允许尝试下一来源。
// 底层注入错误仍应可通过 errors.Is 追溯，不能被通用文案吞掉。
// 该测试直接使用真实订阅资金对象，不复制一套额度判定逻辑。
func TestResourceBillingSubscriptionReserveErrorClassification(t *testing.T) {
	truncate(t)
	seedUser(t, 908, 1000)
	seedSubscription(t, 908, 908, 100, 50)
	session := &BillingSession{relayInfo: &relaycommon.RelayInfo{UserId: 908}, funding: &SubscriptionFunding{subscriptionId: 908}, lang: "en"}
	var apiErr *relaytypes.NewAPIError
	require.ErrorAs(t, session.ReserveBeforeSubmit(60), &apiErr)
	require.Equal(t, relaytypes.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	injected := errors.New("subscription lookup unavailable")
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("resource-reserve-db-error", func(db *gorm.DB) {
		if db.Statement.Table == "user_subscriptions" {
			db.AddError(injected)
		}
	}))
	t.Cleanup(func() { model.DB.Callback().Query().Remove("resource-reserve-db-error") })
	err := session.ReserveBeforeSubmit(10)
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, relaytypes.ErrorCodeUpdateDataError, apiErr.GetErrorCode()) // 读取故障不能被分类为可跳过的额度不足。
	require.ErrorIs(t, err, injected)                                            // 错误包装后仍须支持根因定位。
}

// 批量写库开关不能降低统一来源的持久化保证，包括无限预算的令牌。
// 同时覆盖支持 Redis 协议及 Lua 的内存测试服务与不启用缓存两条路径。
// 分为缓存关闭和开启两条路径，保证同一持久化约束不依赖 Redis。
// 即使全局允许批量写入，统一来源预扣也必须在返回前可靠落库。
// 无限预算只取消 Key 的金额上限，不取消它的实际用量记账。
// 首先直接读数据库，防止缓存显示正确数字却掩盖持久层未更新。
// 随后同步退款，再次核对用户与 Key 两侧数据都已恢复。
// 开启缓存时最后读取用户额度，确认持久层和缓存的退款结论一致。
// 所有全局开关和 Redis 客户端都会在子测试退出时恢复。
// 内存 Redis 服务仅隔离外部依赖，实际缓存命令与预扣代码仍执行。
// 两条路径使用相同初始预算与预留金额，使差异仅来自缓存配置。
func TestResourceBillingDurableWithBatchQueue(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprint(cached), func(t *testing.T) {
			truncate(t)
			previousBatch, previousRedis, previousRDB := common.BatchUpdateEnabled, common.RedisEnabled, common.RDB
			common.BatchUpdateEnabled = true // 显式开启可能延迟落库的配置以覆盖真实风险。
			common.RedisEnabled = cached
			t.Cleanup(func() {
				common.BatchUpdateEnabled, common.RedisEnabled, common.RDB = previousBatch, previousRedis, previousRDB
			})
			if cached {
				server := miniredis.RunT(t)
				common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
				t.Cleanup(func() { common.RDB.Close() })
			}
			seedUser(t, 909, 1000)
			seedToken(t, 909, 909, "resource-batch", 1000)
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 909).Update("unlimited_quota", true).Error)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			ctx.Set("oak.billing_source", "wallet") // 模拟外层已明确选择的钱包来源，本层不能重新排序。
			info := &relaycommon.RelayInfo{UserId: 909, TokenId: 909, TokenKey: "resource-batch", TokenUnlimited: true}
			session, apiErr := NewBillingSession(ctx, info, 100)
			require.Nil(t, apiErr)
			// 直接读数据库，不能让缓存中的正确数字掩盖仍未落库的预扣。
			var user model.User
			var token model.Token
			require.NoError(t, model.DB.First(&user, 909).Error)
			require.Equal(t, 900, user.Quota)
			require.NoError(t, model.DB.First(&token, 909).Error)
			require.Equal(t, 900, token.RemainQuota)
			require.NoError(t, session.RefundSync())
			require.NoError(t, model.DB.First(&user, 909).Error)
			require.Equal(t, 1000, user.Quota)
			require.NoError(t, model.DB.First(&token, 909).Error)
			require.Equal(t, 1000, token.RemainQuota)
			if cached {
				quota, err := model.GetUserQuota(909, false)
				require.NoError(t, err)
				require.Equal(t, 1000, quota)
			}
		})
	}
}

// TestResourceBillingUnknownSettlementDoesNotRefund 覆盖已提交结算但确认丢失的边界。
// 数据库真实完成差额扣款后，再由回调注入错误模拟网络确认丢失。
// 只替代外部故障信号，不替代资金来源或结算实现。
// 调用者不能把这个错误解释为尚未消费，从而退款并继续其他来源。
// 故障移除后重复调用仍须保留未知结论，不能再次追加消费或加回预留。
// 钱包已经完成实际扣款，令牌暂保留预留，供后续对账识别不一致。
func TestResourceBillingUnknownSettlementDoesNotRefund(t *testing.T) {
	truncate(t)
	seedUser(t, 910, 1000)
	seedToken(t, 910, 910, "resource-settlement", 1000)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("oak.billing_source", "wallet") // 显式来源启用同步持久化合同。
	info := &relaycommon.RelayInfo{UserId: 910, TokenId: 910, TokenKey: "resource-settlement"}
	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	unknown := errors.New("wallet settlement acknowledgment lost")
	// 在事务提交之后报错，确保不是普通的提交前失败。
	require.NoError(t, model.DB.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("resource-unknown-settlement", func(db *gorm.DB) {
		if db.Statement.Table == "users" {
			db.AddError(unknown)
		}
	}))
	t.Cleanup(func() { model.DB.Callback().Update().Remove("resource-unknown-settlement") })
	require.ErrorIs(t, session.Settle(150), unknown)
	model.DB.Callback().Update().Remove("resource-unknown-settlement")
	// 确认错误不会因下一次网络正常而自动消失。
	require.ErrorIs(t, session.RefundSync(), unknown)
	require.Error(t, session.Settle(150))
	quota, err := model.GetUserQuota(910, false)
	require.NoError(t, err)
	require.Equal(t, 850, quota) // 真实消费只扣一次，不退成950或1000。
	token, err := model.GetTokenById(910)
	require.NoError(t, err)
	require.Equal(t, 900, token.RemainQuota) // 未确认完整结算时不猜测令牌差额。
}
