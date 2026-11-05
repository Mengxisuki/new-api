package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// 挂帐放行履约，但不会给钱包加额，也不宣称现金已经到账。
// 订单、债务和授信占用同事务提交；唯一交易键保证跨进程幂等。
// 入口接收服务器确定的订单编号和当前会话用户。
// 查询必须同时绑定二者，订单编号本身不提供授权。
// 支付来源在创建时冻结，后续付款不能把微信改成挂帐。
// 报价只是可用性快照，不承诺为用户预留额度。
// 因此实际支付时重新检查账户启用和代充值授权。
// 商品在付款前下架时停止新占额，既有成功结果仍可恢复。
// 到期的未付订单需要重新购买，避免无限期使用旧价格。
// 订单金额必须为正，不能用零价订单跳过授信审查。
// 账户币种固定人民币，当前模型显示单位不参与运算。
// 其他模块预留字段不能成为绕过模块白名单的路径。
// 同单重试先识别终态，再尝试更新授信行。
// 不同订单争抢额度时由数据库条件更新决定最终结果。
// 条件内重复验证启用状态，覆盖管理员在预读后的撤销操作。
// 条件失败不会创建账本，客户端刷新后可读取最新可用额度。
// 占用增加后插入账本失败会回滚，不能出现孤立占额。
// 订单事件保存失败同样回滚账本和占用。
// 该事务不访问 Redis，挂帐不会竞争模型调用的钱包缓存。
// 也不发起外部商户请求，不存在二维码或第三方回调。
// 调用方不得将挂帐成功描述为已经收到线下款项。
// 履约和收款分别以订单状态和账本状态为准。
// 现金到账时间只在财务实际登记结清后记录到挂帐流水。
// 如果发生数据库锁冲突，失败保持可见并可按原订单重试。
func PayAccountRechargeCredit(id, userID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var order AccountRechargeOrder
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", id, userID).First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != "credit" || order.InvoiceRequired {
			return ErrCreditInvalid
		}
		if order.PaymentStatus == "paid" {
			return nil
		} // 授权撤销后仍可恢复已完成挂帐的原单。
		if order.PaymentStatus != "pending" || order.MoneyCents <= 0 {
			return ErrCreditConflict
		}
		if order.ExpiresAt <= common.GetTimestamp() {
			return ErrRechargeUnavailable
		}
		var product AccountRechargeProduct
		if err := tx.First(&product, order.ProductID).Error; err != nil {
			return err
		}
		if !product.Enabled {
			return ErrRechargeUnavailable
		}
		// 用户状态与模块权限再次查库，旧页面不能绕过停用。
		var user User
		if err := tx.Select("id", "status").First(&user, userID).Error; err != nil {
			return err
		}
		if user.Status != common.UserStatusEnabled {
			return ErrCreditUnavailable
		}
		var account CreditAccount
		// 不先持有授信行锁再写流水，避免与结清形成反向锁序。
		if err := tx.First(&account, userID).Error; err != nil {
			return err
		}
		if err := CheckCreditAvailable(&account, CreditModuleRecharge, order.MoneyCents); err != nil {
			return err
		}
		// 原子条件覆盖跨订单并发；预读额度只用于提前返回明确原因。
		result := tx.Model(&CreditAccount{}).
			Where("user_id = ? AND enabled = ? AND recharge_enabled = ? AND used_cents <= limit_cents - ?", userID, true, true, order.MoneyCents).
			UpdateColumn("used_cents", gorm.Expr("used_cents + ?", order.MoneyCents))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCreditLimit
		}
		entry := CreditEntry{UserID: userID, Module: CreditModuleRecharge, TradeNo: order.TradeNo, AmountCents: order.MoneyCents, Status: "outstanding", CreatedAt: common.GetTimestamp()}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		// paid 在旧订单合同中表示允许履约；实际欠款以独立账本为准。
		// PaidAt 保持零，不能被财务当成现金到账时间。
		order.PaymentStatus = "paid"
		return saveRechargeEvent(tx, &order, userID, "credit_charge", "")
	})
}

// 未结清订单退款实际是撤销债务，直接恢复占用，不发生现金返还。
// 已结清退款必须登记外部退款凭证，不允许再次释放授信。
// 本函数只能在已锁定原订单的退款事务中调用。
// 模块和交易号联合定位债务，不能按用户名模糊撤销。
// 退款批准阶段已经停止履约，确认阶段才改变债务。
// 未结清退款不需要编造网关退款凭证。
// 已结清款项必须在线下实际退还后登记凭证。
// 保留原结清人和收款号，便于追踪先收后退的完整过程。
// 两种退款终态都不能再次参与结清。
// 重复确认由订单退款幂等分支处理，不再次进入本函数。
// 额度释放与订单终态在同一外层事务，不存在半笔退款。
func refundRechargeCreditTx(tx *gorm.DB, order *AccountRechargeOrder, actor int, reference string) error {
	var entry CreditEntry
	if err := lockForUpdate(tx).Where("module = ? AND trade_no = ?", CreditModuleRecharge, order.TradeNo).First(&entry).Error; err != nil {
		return err
	}
	status := "cancelled"
	switch entry.Status {
	case "outstanding":
		if err := releaseCreditTx(tx, &entry); err != nil {
			return err
		}
	case "settled":
		if strings.TrimSpace(reference) == "" {
			return ErrCreditInvalid
		}
		status = "refunded"
	default:
		return ErrCreditConflict // 非退款终态不得通过新操作重复归还资金。
	}
	return tx.Model(&entry).Updates(map[string]any{
		"status": status, "closed_at": common.GetTimestamp(), "closed_by": actor, "refund_ref": reference,
	}).Error
}
