package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// 本模块区分权益回收和现金退款两个事实。
// 本地事务负责回收，微信查询负责确认现金。
// 数据库和微信没有共同的分布式事务，
// 因此中间状态必须持久、可见和可恢复。
// 先回收权益再发出退款请求，
// 防止现金退回后用户继续花掉同一笔额度。
// 这项选择意味着上游失败时权益仍被冻结，
// 管理员需要恢复原退款或在商户平台核对。
// 不能依据HTTP超时直接补回用户权益，
// 因为请求可能已经在微信侧执行成功。
// 一笔业务订单只有一个退款记录，
// 复合唯一索引同时保护不同业务模块。
// 商户退款号从业务身份稳定派生，
// 同一意图重试不会创造第二笔现金支出。
// 退款号不包含姓名、手机号或用户凭据，
// 也不依赖进程启动时间和本地内存。
// 原单行锁是整个事务的入口，
// 同一订单的不同管理请求必须按此串行。
// 用户行锁串行化不同订单对同一钱包的回收，
// 并与订阅完成时的用户锁顺序保持一致。
// 查询退款身份发生在权益变更之前，
// 已有记录直接返回，不再重复回收。
// 管理员重新填写原因不会覆盖原审批原因，
// 原操作人和金额也不会被重试改写。
// 未付款订单没有可以返还的实收，
// 不能通过退款入口为它制造资金记录。
// 挂帐不是微信实收，
// 不得把欠款撤销伪装成微信原路退款。
// 钱包只扣下单时冻结的内部额度，
// 不能用当前汇率重算历史充值权益。
// 优惠和开票附加费影响实付金额，
// 但不改变该单实际发到钱包的额度。
// 余额不足时整单失败，
// 不分次扣除，也不把钱包扣成负值。
// Redis可能已经预扣了在途模型请求，
// 数据库余额不能单独代表可回收余额。
// 缓存预留使用独立订单标记，
// 不会和代充值付款的原标记互相认领。
// 只有重新锁单确认事务未提交，
// 才可以释放失败退款的缓存预留。
// 数据库状态未知时宁可留下待恢复结果，
// 不能用无依据的补偿扩大余额。
// 订阅回收使用实际增加的秒数，
// 月周期长度由付款时的日历计算决定。
// 重复购买会延长同一份订阅，
// 因此退款必须扣本单增量而非删除整份实例。
// 回收不会清零原订阅已经发生的用量，
// 也不会给仍有效的其他周期重新发放额度。
// 缩短后的到期时间不早于订阅开始时间，
// 失去全部有效时长后才取消实例。
// 其他有效升级订阅仍可维持用户分组，
// 不能因为退一单而撤销另一单的分组权益。
// 已经删除或无法关联的历史权益需要人工核对，
// 系统不寻找一个看起来相似的替代实例。
// 代充值退款首先冻结人工办理，
// 避免退款与业务员完成订单同时成功。
// 成功前保留原在线资料以便核对，
// 确认现金退款后才清除在线密文。
// 已开具发票不直接删除，
// 未开具申请则随退款终止。
// 财务收入与退款分别按各自日期核算，
// 不能把原付款订单改成未支付来冲减收入。
// 最终结果仍按原订单锁序写入，
// 后台查询与管理员恢复可以安全并发。
// 成功终态不可被较旧的处理中响应覆盖，
// 重复成功也不再新增退款事件。

// PaymentRefund 保存一单一次整单退款；订单收款历史保持不变。
// prepared 表示权益已回收，必须继续用原退款号向微信恢复结果。
// processing/abnormal/closed 均不等于现金退回，不自动返还已回收权益。
// 不确定的网络结果保留原身份，禁止生成第二个退款号重试。
// succeeded 仅由已验签、金额及订单匹配的微信查询结果写入。
// 商户密钥与用户提交的代充值资料均不进入本表。
// 本表不会储存可供再次付款的链接或二维码。
// 退款号索引使用有限长度，兼容MySQL旧版索引限制。
// 日期使用Unix秒，和原财务查询的时间口径一致。
// 状态不使用数据库专属枚举，三种数据库采用同一迁移。
// 金额采用bigint整数分，避免多次格式化产生精度差异。
// 操作人只记录内部编号，不复制完整用户资料。
// 原订单保持唯一归属，本表不提供删除后重新退款的入口。
// 新记录创建时间表示平台批准，完成时间来自微信成功响应。
type PaymentRefund struct {
	ID          int    `json:"id"`
	Module      string `json:"module" gorm:"size:24;uniqueIndex:idx_payment_refund_order,priority:1"`
	TradeNo     string `json:"trade_no" gorm:"size:255;uniqueIndex:idx_payment_refund_order,priority:2"`
	RefundNo    string `json:"refund_no" gorm:"size:64;uniqueIndex"` // 固定商户退款身份。
	UserID      int    `json:"user_id" gorm:"index"`
	ChannelID   string `json:"channel_id" gorm:"size:64"` // 只能沿原支付渠道退款。
	AmountCents int64  `json:"amount_cents" gorm:"type:bigint"`
	Status      string `json:"status" gorm:"size:24;index"`
	Reason      string `json:"reason" gorm:"size:80"` // 微信原因上限80字节，在入口校验。
	ActorID     int    `json:"actor_id"`
	ProviderRef string `json:"provider_ref" gorm:"size:128"`
	CreatedAt   int64  `json:"created_at"`
	CompletedAt int64  `json:"completed_at" gorm:"index"`
}

var (
	ErrRefundInvalid     = common.NewMessage("Invalid refund request")
	ErrRefundUnsupported = common.NewMessage("Only official WeChat payments support automatic refunds")
	ErrRefundState       = common.NewMessage("The order cannot be refunded in its current state")
	ErrRefundRights      = common.NewMessage("The original subscription entitlement is unavailable; manual reconciliation is required")
)

// GetPaymentRefund 返回持久原记录；不存在与数据库故障分开处理。
// 恢复流程不根据浏览器传入的金额或退款号创建交易。
func GetPaymentRefund(module, trade string) (*PaymentRefund, error) {
	var r PaymentRefund
	err := DB.Where("module = ? AND trade_no = ?", module, trade).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &r, err
}

// PreparePaymentRefund 先锁原订单，再回收权益并冻结退款身份。
// 所有本地变更同事务提交，网络请求始终在事务外执行。
// 重试读取原退款，不再次扣额度、减时长或清除资料。
// 钱包余额不足明确失败，不能先退现金再留下负额度。
// 订阅按付款时记录的增量回收，不读取当前套餐周期。
// 代充值已完成时拒绝，退款中继续沿用原履约冻结状态。
func PreparePaymentRefund(module, trade, reason string, actor int) (*PaymentRefund, error) {
	reason = strings.TrimSpace(reason)
	if trade == "" || len(trade) > 255 || reason == "" || len(reason) > 80 || actor <= 0 {
		return nil, ErrRefundInvalid
	}
	var refund PaymentRefund
	var reserved *AccountRechargeOrder // 复用现有钱包原子缓存预留，标记独立于代充值单。
	err := DB.Transaction(func(tx *gorm.DB) error {
		var topup TopUp
		var subscription SubscriptionOrder
		var recharge AccountRechargeOrder
		var userID int
		var channel, provider, status string
		var cents int64
		// 原单行锁统一所有同单入口，唯一索引提供跨进程的最后一道保护。
		switch module {
		case CreditModuleTopup:
			if err := lockForUpdate(tx).Where("trade_no = ?", trade).First(&topup).Error; err != nil {
				return err
			}
			// 订阅收款镜像绝不能作为钱包充值退款，避免重复退现金和误扣钱包。
			var count int64
			if err := tx.Model(&SubscriptionOrder{}).Where("trade_no = ?", trade).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrRefundInvalid
			}
			userID, channel, provider, status = topup.UserId, topup.PaymentMethod, topup.PaymentProvider, topup.Status
			var err error
			cents, err = CreditMoneyCents(topup.Money)
			if err != nil {
				return err
			}
		case CreditModuleSubscription:
			if err := lockForUpdate(tx).Where("trade_no = ?", trade).First(&subscription).Error; err != nil {
				return err
			}
			userID, channel, provider, status = subscription.UserId, subscription.PaymentMethod, subscription.PaymentProvider, subscription.Status
			var err error
			cents, err = CreditMoneyCents(subscription.Money)
			if err != nil {
				return err
			}
		case CreditModuleRecharge:
			if err := lockForUpdate(tx).Where("trade_no = ?", trade).First(&recharge).Error; err != nil {
				return err
			}
			userID, channel, provider, status, cents = recharge.UserID, recharge.PaymentMethod, recharge.PaymentProvider, recharge.PaymentStatus, recharge.MoneyCents
		default:
			return ErrRefundInvalid
		}
		// 原记录优先返回，已退款或失败查询不能产生另一笔退款意图。
		err := tx.Where("module = ? AND trade_no = ?", module, trade).First(&refund).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if provider != PaymentProviderWechat && !(module == CreditModuleRecharge && provider == "wechat") {
			return ErrRefundUnsupported
		}
		if cents <= 0 || cents > MaxCreditCents {
			return ErrRefundInvalid
		}
		if module == CreditModuleRecharge {
			if (status != "paid" && status != "refunding") || recharge.Status == "completed" {
				return ErrRefundState
			}
		} else if status != common.TopUpStatusSuccess {
			return ErrRefundState
		}
		// 同一用户不同退款与续期按用户行串行化，兼容三种主库。
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		switch module {
		case CreditModuleTopup:
			quota, err := topup.QuotaToCredit()
			if err != nil {
				return err
			}
			if user.Quota < quota {
				return ErrRechargeBalance
			}
			reserved = &AccountRechargeOrder{UserID: userID, TradeNo: "order-refund:" + trade, WalletQuota: quota}
			// Redis预扣先保护在途消费；失败结果不能当成未扣款。
			if err := rechargeCache(reserved, "debit"); err != nil {
				return err
			}
			result := tx.Model(&User{}).Where("id = ? AND quota >= ?", userID, quota).Update("quota", gorm.Expr("quota - ?", quota))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrRechargeBalance
			}
		case CreditModuleSubscription:
			// 历史订单缺少精确关联，禁止猜同套餐实例或拿现价换算天数。
			if subscription.FulfilledSubscriptionID <= 0 || subscription.GrantedSeconds <= 0 {
				return ErrRefundRights
			}
			var sub UserSubscription
			if err := lockForUpdate(tx).Where("id = ? AND user_id = ? AND plan_id = ?", subscription.FulfilledSubscriptionID, userID, subscription.PlanId).First(&sub).Error; err != nil {
				return ErrRefundRights
			}
			sub.EndTime = max(sub.StartTime, sub.EndTime-subscription.GrantedSeconds)
			if sub.NextResetTime >= sub.EndTime {
				sub.NextResetTime = 0
			}
			if sub.EndTime <= GetDBTimestamp() {
				sub.Status = "cancelled"
				if _, err := downgradeUserGroupForSubscriptionTx(tx, &sub, GetDBTimestamp()); err != nil {
					return err
				}
			}
			if err := tx.Save(&sub).Error; err != nil {
				return err
			}
		case CreditModuleRecharge:
			// 冻结办理后才允许请求微信，资料清除留到现金退款确认成功。
			recharge.PaymentStatus, recharge.PublicNote = "refunding", reason
			if err := saveRechargeEvent(tx, &recharge, actor, "refund_approve", reason); err != nil {
				return err
			}
		}
		hash := sha256.Sum256([]byte(module + "/" + trade))
		refund = PaymentRefund{Module: module, TradeNo: trade, RefundNo: fmt.Sprintf("RF%x", hash[:15]), UserID: userID, ChannelID: channel, AmountCents: cents, Status: "prepared", Reason: reason, ActorID: actor, CreatedAt: common.GetTimestamp()}
		return tx.Create(&refund).Error
	})
	if err != nil && reserved != nil {
		// 重新锁原订单并确认事务未提交，才允许释放缓存预扣。
		// 数据库未知时保持预留，原单重试能够恢复；不能误加另一成功退款的额度。
		reconcile := DB.Transaction(func(tx *gorm.DB) error {
			var original TopUp
			if e := lockForUpdate(tx).Where("trade_no = ?", trade).First(&original).Error; e != nil {
				return e
			}
			var count int64
			if e := tx.Model(&PaymentRefund{}).Where("module = ? AND trade_no = ?", module, trade).Count(&count).Error; e != nil {
				return e
			}
			if count == 0 {
				return rechargeCache(reserved, "release")
			}
			return nil
		})
		if reconcile != nil {
			return nil, ErrRechargeCache
		}
	}
	if err != nil {
		return nil, err
	}
	if module == CreditModuleSubscription {
		refreshSubscriptionUserGroupCache(refund.UserID, "subscription refund")
	}
	return &refund, nil
}

// ApplyPaymentRefundResult 只接收服务层验签并匹配原退款的结果。
// 收款订单保持 success，以保留原收款日收入；退款日单独扣减现金。
// 代充值事件用于履约审计，现金统计使用本表并排除重复事件。
// 退款处理中不重新回收权益，也不把网关受理等同于到账。
func ApplyPaymentRefundResult(id int, state, reference string, completedAt int64) error {
	if state == "succeeded" && completedAt <= 0 {
		return ErrRefundInvalid
	}
	if state != "processing" && state != "succeeded" && state != "closed" && state != "abnormal" {
		return ErrRefundInvalid
	}
	if reference == "" || len(reference) > 128 {
		return ErrRefundInvalid
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var refund PaymentRefund
		// 先读取不可变关联，再锁原单，保持与Prepare相同的锁序。
		if err := tx.First(&refund, id).Error; err != nil {
			return err
		}
		if refund.Module == CreditModuleRecharge {
			var o AccountRechargeOrder
			if err := lockForUpdate(tx).Where("trade_no = ?", refund.TradeNo).First(&o).Error; err != nil {
				return err
			}
		} else if refund.Module == CreditModuleSubscription {
			var o SubscriptionOrder
			if err := lockForUpdate(tx).Where("trade_no = ?", refund.TradeNo).First(&o).Error; err != nil {
				return err
			}
		} else {
			var o TopUp
			if err := lockForUpdate(tx).Where("trade_no = ?", refund.TradeNo).First(&o).Error; err != nil {
				return err
			}
		}
		if err := lockForUpdate(tx).First(&refund, id).Error; err != nil {
			return err
		}
		if refund.Status == "succeeded" {
			return nil
		} // 较旧查询结果不能回退已经完成的退款。
		if refund.ProviderRef != "" && refund.ProviderRef != reference {
			return ErrRefundInvalid
		}
		refund.Status, refund.ProviderRef = state, reference
		if state == "succeeded" {
			refund.CompletedAt = completedAt
			if refund.Module == CreditModuleRecharge {
				var o AccountRechargeOrder
				if err := tx.Where("trade_no = ?", refund.TradeNo).First(&o).Error; err != nil {
					return err
				}
				if o.PaymentStatus != "refunding" {
					return ErrRefundState
				}
				o.PaymentStatus, o.Status, o.Secret, o.RefundRef = "refunded", "cancelled", "", reference
				if err := saveRechargeEvent(tx, &o, refund.ActorID, "refund", refund.Reason); err != nil {
					return err
				}
			}
			// 未开具申请终止，已开具记录保留供财务依法处理红冲。
			if err := tx.Model(&InvoiceRequest{}).Where("trade_no = ? AND status IN ?", refund.TradeNo, []string{"pending", "processing"}).Updates(map[string]any{"status": "rejected", "remark": "Order refunded", "updated_at": common.GetTimestamp()}).Error; err != nil {
				return err
			}
		}
		return tx.Save(&refund).Error
	})
}
