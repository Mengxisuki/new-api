package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// 管理视图只负责检索已存在的交易。
// 它不发额、不结清债务，也不请求退款。
// 支付状态与退款状态分开保存和展示，
// 已退款不意味着原收款从未发生。
// 钱包与订阅的订单号可能同时出现在账单表，
// 因为订阅到账会创建钱包账单镜像。
// 因此钱包查询必须排除订阅交易号，
// 不能依赖额度为零来猜测镜像身份。
// 三个业务使用各自真实订单表，
// 字段别名只统一返回结构。
// 代充值金额由整数分换算为展示金额，
// 这次换算不参与退款记账。
// 其他网关金额可能使用不同货币或合同，
// 列表不把未知网关金额假设为人民币。
// 查询最多返回二十条，
// 额外退款查询的开销因此有明确上界。
// 退款状态来自独立持久记录，
// 不能由按钮点击或订单时间推断。
// 服务端禁止原因只反映已知静态条件，
// 钱包余额仍须在退款事务里再次校验。
// 管理员打开列表之后可能发生消费，
// 因此按钮可用不构成退款承诺。
// 历史订阅没有精确权益关联时显示拒绝原因，
// 不能按最近一份同套餐订阅代替原权益。
// 代充值已完成意味着外部办理已经兑现，
// 本轮不允许该状态再发起自动退款。
// 已有退款应始终提供原记录，
// 不因原订单仍显示支付成功而再次建单。
// 空数组是合法无订单结果，
// 数据库失败必须整体返回错误。
// 统计总数和分页投影使用独立查询会话，
// 避免GORM复用终结操作造成条件污染。
// 渠道定位按原订单付款方式读取，
// 退款入口不得选用另一商户或支付方式。
// 停用渠道不等于删除其历史交易，
// 能否退款取决于原配置是否仍完整可用。
// 未知业务类型在查询前拒绝，
// 不能落入默认钱包分支造成跨业务退款。

// PaymentOrderView 是管理面板的安全投影，不传递支付载荷或代充值密文。
// 三类订单按业务筛选分页，订阅镜像不作为第二笔钱包交易展示。
// 金额保留原渠道记录值，只有官方微信使用人民币自动退款。
// Refund 独立于原付款状态，避免退款后抹掉历史收款。
type PaymentOrderView struct {
	ID              int            `json:"id"`
	Module          string         `json:"module"`
	UserID          int            `json:"user_id"`
	TradeNo         string         `json:"trade_no"`
	Money           float64        `json:"money"`
	PaymentProvider string         `json:"payment_provider"`
	PaymentMethod   string         `json:"payment_method"`
	Status          string         `json:"status"`
	CreatedAt       int64          `json:"created_at"`
	PaidAt          int64          `json:"paid_at"`
	Refund          *PaymentRefund `json:"refund" gorm:"-"`       // 未发起明确为null。
	RefundBlock     string         `json:"refund_block" gorm:"-"` // 服务端解释已知的禁止条件。
}

// ListPaymentOrders 查询始终在服务器限制页大小，不下载全量交易。
// 筛选参数只绑定值，业务字段由白名单确定，不能拼接任意列名。
// GET只读；查询微信及恢复退款使用独立POST写入口。
func ListPaymentOrders(module, keyword, state string, page int) ([]PaymentOrderView, int64, error) {
	var query *gorm.DB
	var selection string
	switch module {
	case CreditModuleTopup:
		mirror := DB.Model(&SubscriptionOrder{}).Select("1").Where("subscription_orders.trade_no = top_ups.trade_no")
		query = DB.Model(&TopUp{}).Where("NOT EXISTS (?)", mirror)
		selection = "id, user_id, trade_no, money, payment_provider, payment_method, status, create_time AS created_at, complete_time AS paid_at"
	case CreditModuleSubscription:
		query = DB.Model(&SubscriptionOrder{})
		selection = "id, user_id, trade_no, money, payment_provider, payment_method, status, create_time AS created_at, complete_time AS paid_at"
	case CreditModuleRecharge:
		query = DB.Model(&AccountRechargeOrder{})
		selection = "id, user_id, trade_no, money_cents / 100.0 AS money, payment_provider, payment_method, payment_status AS status, created_at, paid_at"
	default:
		return nil, 0, ErrRefundInvalid
	}
	if keyword != "" {
		query = query.Where("trade_no LIKE ?", "%"+keyword+"%")
	}
	if state != "" {
		col := "status"
		if module == CreditModuleRecharge {
			col = "payment_status"
		}
		query = query.Where(col+" = ?", state)
	}
	var total int64
	// 分页和聚合各用独立Session，防止GORM终结操作污染投影。
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []PaymentOrderView{}
	if err := query.Select(selection).Order("id DESC").Offset((max(1, page) - 1) * 20).Limit(20).Scan(&items).Error; err != nil {
		return nil, 0, err
	}
	for i := range items {
		row := &items[i]
		row.Module = module
		refund, err := GetPaymentRefund(module, row.TradeNo)
		if err != nil {
			return nil, 0, err
		}
		row.Refund = refund
		// 存在退款时展示恢复动作，不依据原付款成功状态重新发起。
		if refund != nil {
			continue
		}
		if row.PaymentProvider != PaymentProviderWechat && !(module == CreditModuleRecharge && row.PaymentProvider == "wechat") {
			row.RefundBlock = ErrRefundUnsupported.Error()
		} else if row.Status != common.TopUpStatusSuccess && !(module == CreditModuleRecharge && (row.Status == "paid" || row.Status == "refunding")) {
			row.RefundBlock = ErrRefundState.Error()
		} else if module == CreditModuleSubscription {
			var order SubscriptionOrder
			if err := DB.Select("fulfilled_subscription_id", "granted_seconds").First(&order, row.ID).Error; err != nil {
				return nil, 0, err
			}
			if order.FulfilledSubscriptionID <= 0 || order.GrantedSeconds <= 0 {
				row.RefundBlock = ErrRefundRights.Error()
			}
		} else if module == CreditModuleRecharge {
			var order AccountRechargeOrder
			if err := DB.Select("status").First(&order, row.ID).Error; err != nil {
				return nil, 0, err
			}
			if order.Status == "completed" {
				row.RefundBlock = ErrRefundState.Error()
			}
		}
	}
	return items, total, nil
}

// PaymentOrderChannel 只定位原付款渠道，不使用管理请求传入的商户身份。
// 支付方式停用不阻止旧款退款，但删除配置后必须由管理员恢复原渠道。
func PaymentOrderChannel(module, trade string) (string, error) {
	switch module {
	case CreditModuleTopup:
		var o TopUp
		err := DB.Select("payment_method").Where("trade_no = ?", trade).First(&o).Error
		return o.PaymentMethod, err
	case CreditModuleSubscription:
		var o SubscriptionOrder
		err := DB.Select("payment_method").Where("trade_no = ?", trade).First(&o).Error
		return o.PaymentMethod, err
	case CreditModuleRecharge:
		var o AccountRechargeOrder
		err := DB.Select("payment_method").Where("trade_no = ?", trade).First(&o).Error
		return o.PaymentMethod, err
	default:
		return "", ErrRefundInvalid
	}
}
