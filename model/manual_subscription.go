package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// AdminBindSubscriptionWithFinance 将管理员发放订阅与人工分类记账一起提交。
// 钱包字段不适用，保留 NULL；续期前后权益快照使多次续期可分别对账。
// 不生成网关订单或 TopUp 镜像，避免赠送和挂帐被汇总成现金收入。
//
// 输入边界：
//   - actor 和 operatorRole 必须来自已鉴权会话，不接收请求体中的操作人。
//   - planID 指向平台套餐，不是上游账号订阅或人工号池周期。
//   - 后台允许按既有规则发放套餐，不使用用户自助购买的商户就绪条件。
//   - 人工收款金额允许与公开售价不同，折扣由本次记录明确保存。
//   - 额度撤回仅适用于钱包，本函数不负责取消订阅或退款。
//   - Note 和现金凭证不会被当作可执行内容或外部支付地址。
//
// 数据归属：
//   - 分类记录是操作来源，SourceID 才是实际被开通/延长的权益实例。
//   - 同套餐可能存在历史多实例，Before 保存当时的活跃集合而非猜测一条。
//   - After 只保存本次真正修改的实例，后续续期不得回写这个快照。
//   - 钱包前后余额为空与零余额不同，订阅不修改钱包字段。
//   - 现金和欠款由请求显式提供，不能使用当前汇率折算套餐额度。
//   - 挂帐占订阅模块授信，不借用钱包充值授权。
//
// 事务与重试：
//   - 用户锁串行化同一用户的开通、限购与续期检查。
//   - 请求身份先于套餐读取恢复，已完成的旧请求不被后来改价重新解释。
//   - 第一次成功才刷新分组缓存，重放不会重复发放或再写用户日志。
//   - 欠款和权益均回滚时不得单独保留线下收入。
//   - 只有尝试插入来源单后才允许查并发结果，权限失败不走成功恢复。
//   - 首次普通错误保留原错误；查不到已提交记录不假报成功。
//   - 提示语来自原权益快照，保持后台开通接口的分组升级返回合同。
func AdminBindSubscriptionWithFinance(userID, planID, actor, operatorRole int, finance ManualFinanceInput) (*common.Message, error) {
	if userID <= 0 || planID <= 0 || actor <= 0 || operatorRole < common.RoleAdminUser {
		return nil, ErrQuotaLedgerInvalid
	}
	if err := finance.Normalize(); err != nil {
		return nil, err
	}
	if finance.OriginalID != 0 { // 原钱包更正编号不能冒充订阅撤销授权。
		return nil, ErrQuotaLedgerInvalid
	}
	switch finance.Reason { // 后台开通不接受额度撤回类原因。
	case QuotaAdjustReasonGrant, QuotaAdjustReasonCompensation, QuotaAdjustReasonOnsiteCash, QuotaAdjustReasonOnAccount:
	default:
		return nil, ErrQuotaLedgerReason
	}
	hash, err := manualFinanceHash(ManualSourceSubscription, userID, "grant", planID, finance)
	if err != nil {
		return nil, err
	}
	var ledger QuotaLedger                                          // 同一次请求只返回首次提交的快照。
	completed, attemptedInsert, groupChanged := false, false, false // 区分已提交、唯一键竞争与缓存更新。
	err = DB.Transaction(func(tx *gorm.DB) error {
		var user User // 不依赖可能过期的用户缓存作授权判断。
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		if operatorRole != common.RoleRootUser && operatorRole <= user.Role { // 普通管理员不能替同级或更高角色发权益。
			return ErrUserQuotaPermission
		}
		previous, err := findManualFinanceEntry(tx, actor, finance, hash)
		if err != nil {
			return err
		}
		if previous != nil { // 这是已提交原结果，不执行后面的续期事务。
			ledger = *previous
			return nil
		}
		// 已完成的原请求可恢复，新开通必须拒绝停用用户。
		if user.Status != common.UserStatusEnabled {
			return ErrCreditUnavailable
		}
		plan, err := GetSubscriptionPlanById(planID)
		if err != nil {
			return err
		}
		var before []UserSubscription // 保留多实例历史，不能擅自合并既有订阅。
		if err := tx.Where("user_id = ? AND plan_id = ? AND status = ?", userID, planID, "active").Order("id").Find(&before).Error; err != nil {
			return err
		}
		beforeJSON, err := common.Marshal(before) // 序列化失败与数据库错误同样阻止本次记账。
		if err != nil {
			return err
		}
		ledger = newManualFinanceEntry(&user, actor, ManualSourceSubscription, hash, finance)
		ledger.Mode, ledger.SubscriptionBefore = "grant", string(beforeJSON)
		// 来源单先创建，唯一请求身份决定本次是否能继续发放权益。
		attemptedInsert = true
		if err := tx.Create(&ledger).Error; err != nil {
			return err
		}
		// 复用唯一续期/限购实现，后台赠送与购买不能各自维护一套时长计算。
		subscription, err := CreateUserSubscriptionFromPlanTx(tx, userID, plan, "admin")
		if err != nil {
			return err
		}
		afterJSON, err := common.Marshal(subscription) // 保存实际权益，不保存可被后来改写的套餐引用。
		if err != nil {
			return err
		}
		ledger.SourceID, ledger.SubscriptionAfter = subscription.Id, string(afterJSON)
		groupChanged = subscription.PrevUserGroup != "" // 与旧开通入口保持相同的分组缓存触发条件。
		if finance.Reason == QuotaAdjustReasonOnAccount {
			entry, err := ChargeCreditTx(tx, userID, CreditModuleSubscription, ledger.TradeNo, finance.ReceivableCents)
			if err != nil {
				return err
			}
			ledger.CreditEntryID = entry.ID // 双向关联供后续结清核对原单，而非只凭模块名放行。
		}
		// 订阅、人工记录和欠款任一步失败都回滚，不留下半次续期。
		if err := tx.Model(&ledger).Updates(map[string]any{"source_id": ledger.SourceID, "subscription_after": ledger.SubscriptionAfter, "credit_entry_id": ledger.CreditEntryID}).Error; err != nil {
			return err
		}
		completed = true
		return nil
	})
	if err != nil {
		if !attemptedInsert {
			return nil, err
		}
		// 唯一键竞争后在事务外恢复原结果，不能在 PostgreSQL 失败事务中继续查。
		previous, lookupErr := findManualFinanceEntry(DB, actor, finance, hash)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if previous == nil { // 没有并发成功记录时，必须返回本次真实失败。
			return nil, err
		}
		ledger, completed = *previous, false
	}
	if completed && groupChanged {
		refreshSubscriptionUserGroupCache(userID, common.LogText("admin subscription creation"))
	}
	// 主库来源单已保存操作者/原因；日志只作为用户可读辅助，重复请求不再写。
	if completed {
		RecordLog(userID, LogTypeManage, common.NewMessage("Administrator granted a subscription with classification {{reason}}", map[string]any{"reason": ledger.Reason}))
	}
	// 幂等恢复也根据原快照返回同一提示，不读取后来被改动的套餐。
	var granted UserSubscription
	if err := common.UnmarshalJsonStr(ledger.SubscriptionAfter, &granted); err != nil {
		return nil, err
	}
	if granted.PrevUserGroup != "" { // 重试与首次响应沿用同一分组提示。
		return common.NewMessage("User group will be upgraded to {{group}}", map[string]any{"group": granted.UpgradeGroup}), nil
	}
	return nil, nil
}
