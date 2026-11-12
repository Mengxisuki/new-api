package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// PayAccountRechargeCredit 在原代充值订单上履行挂帐。
// 订单锁、用户状态、商品状态、授信占用、债务和订单事件必须同事务提交。
// 订单的 paid 仍表示允许人工履约；真实现金到账只由独立结清记录表示。
func PayAccountRechargeCredit(id, userID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var order AccountRechargeOrder
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", id, userID).First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != PaymentProviderCredit || order.InvoiceRequired {
			return ErrCreditInvalid
		}
		if order.PaymentStatus == "paid" {
			return nil
		}
		if order.PaymentStatus != "pending" || order.MoneyCents <= 0 {
			return ErrCreditConflict
		}
		if order.ExpiresAt <= common.GetTimestamp() {
			return ErrRechargeUnavailable
		}
		// 商品下架只阻止新履约；已完成订单的历史快照不依赖商品当前状态。
		var product AccountRechargeProduct
		if err := tx.First(&product, order.ProductID).Error; err != nil {
			return err
		}
		if !product.Enabled {
			return ErrRechargeUnavailable
		}
		// 订单之后再锁用户，和结清、退款的来源锁序保持一致。
		var user User
		if err := lockForUpdate(tx).Select("id", "status").First(&user, userID).Error; err != nil {
			return err
		}
		if user.Status != common.UserStatusEnabled {
			return ErrCreditUnavailable
		}
		if _, err := ChargeCreditTx(tx, userID, CreditModuleRecharge, order.TradeNo, order.MoneyCents); err != nil {
			return err
		}
		// 不填 PaidAt：该字段代表现金支付时间，挂帐履约不能伪造实收。
		order.PaymentStatus = "paid"
		return saveRechargeEvent(tx, &order, userID, "credit_charge", "")
	})
}

// 未结清订单退款实际是撤销债务，直接恢复占用，不发生现金返还。
// 已结清款项必须登记外部退款凭证；已核销债务没有现金且不能二次释放。
// 调用者已经按订单锁序进入本函数，避免退款与结清形成反向等待。
func refundRechargeCreditTx(tx *gorm.DB, order *AccountRechargeOrder, actor int, reference string) error {
	var entry CreditEntry
	if err := lockForUpdate(tx).Where("module = ? AND trade_no = ?", CreditModuleRecharge, order.TradeNo).First(&entry).Error; err != nil {
		return err
	}
	if entry.UserID != order.UserID || entry.AmountCents != order.MoneyCents {
		return ErrCreditConflict
	}
	status := creditStatusCancelled
	switch entry.Status {
	case creditStatusOutstanding:
		if err := releaseCreditTx(tx, &entry); err != nil {
			return err
		}
	case creditStatusSettled:
		if strings.TrimSpace(reference) == "" {
			return ErrCreditInvalid
		}
		status = creditStatusRefunded
	case creditStatusWriteOff:
		// 核销已经释放授信且没有实收，退款只终止原单履约，保留核销首因。
		// 不要求虚构退款凭证，也不能把已释放额度再扣减一次。
		return nil
	default:
		return ErrCreditConflict
	}
	return tx.Model(&entry).Updates(map[string]any{
		"status": status, "closed_at": common.GetTimestamp(), "closed_by": actor, "refund_ref": reference,
	}).Error
}
