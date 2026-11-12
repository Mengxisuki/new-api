package model

import (
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// CreditMoneyCents 校验已经冻结的人民币金额，不以截断掩盖非整分输入。
// 报价可以先按渠道合同四舍五入；落库后的金额则必须精确到分。
// 先拒绝非有限浮点数，避免 decimal 转换触发 panic。
// 上限在转换成 int64 前检查，超大金额不会溢出成小额债务。
func CreditMoneyCents(money float64) (int64, error) {
	if math.IsNaN(money) || math.IsInf(money, 0) {
		return 0, ErrCreditInvalid
	}
	cents := decimal.NewFromFloat(money).Mul(decimal.NewFromInt(100))
	if !cents.IsInteger() || cents.LessThanOrEqual(decimal.Zero) || cents.GreaterThan(decimal.NewFromInt(MaxCreditCents)) {
		return 0, ErrCreditInvalid
	}
	return cents.IntPart(), nil // 已经证明是有界整数，这里不会丢失小数。
}

// FindCreditCheckout 在重新报价之前恢复原单，避免改价后同请求产生第二张单。
// 两张业务表均使用已有 trade_no 唯一约束，不给历史空请求编号添加唯一索引。
// 同键异参不能认领旧成功结果，归属和渠道也必须完全相符。
// 查询失败向调用者返回，只有真实不存在才允许建单。
func FindCreditCheckout(userID int, module, tradeNo, requestHash string) (bool, error) {
	var owner int             // 数据库归属也参与恢复校验，不能只信摘要碰撞概率。
	var provider, hash string // 旧现金订单即使交易号相同也不能被认领。
	var err error
	switch module {
	case CreditModuleTopup: // 钱包订单与订阅镜像必须按业务区分。
		var order TopUp
		err = DB.Where("trade_no = ?", tradeNo).First(&order).Error
		owner, provider, hash = order.UserId, order.PaymentProvider, order.RequestHash
	case CreditModuleSubscription: // 订阅原单保存独立快照，不从镜像恢复。
		var order SubscriptionOrder
		err = DB.Where("trade_no = ?", tradeNo).First(&order).Error
		owner, provider, hash = order.UserId, order.PaymentProvider, order.RequestHash
	default:
		return false, ErrCreditInvalid
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil // 只有缺失原单才允许读取现价创建。
	}
	if err != nil {
		return false, err
	}
	if owner != userID || provider != PaymentProviderCredit || hash != requestHash {
		return false, ErrCreditConflict // 异参重试保留原单，不覆盖冻结意图。
	}
	return true, nil
}

// InsertCreditCheckout 为创建竞争提供唯一约束兜底，不依赖进程内订单锁。
// Create 的自动事务结束后才查询冲突订单，兼容 PostgreSQL aborted transaction。
// 不只匹配重复键错误码：若并发写入已形成同一原单，可直接恢复该订单。
// 没有匹配订单时保留原始创建错误，不能将数据库故障伪装为成功。
func InsertCreditCheckout(order any, userID int, module, tradeNo, requestHash string) error {
	err := DB.Create(order).Error // 依赖数据库交易号唯一键处理跨进程竞争。
	if err == nil {
		return nil
	}
	found, lookupErr := FindCreditCheckout(userID, module, tradeNo, requestHash)
	if lookupErr != nil {
		return lookupErr
	}
	if found { // 接受竞争者已经提交的同一请求，而非重新发放。
		return nil
	}
	return err // 创建失败且没有可恢复原单，保持故障可见。
}

// creditCheckoutPending 将自助付款窗口与真实网关的迟到回调分开。
// 成功幂等必须由调用方提前处理；这里仅验证尚未履约的原单。
// 十分钟以后不能按历史低价无限期取得授信与权益。
func creditCheckoutPending(createdAt int64) error {
	// 原单恢复不能成为绕过合规关闭后继续付款的通道。
	if !operation_setting.IsPaymentComplianceConfirmed() {
		return ErrCreditUnavailable
	}
	now := common.GetTimestamp()
	if createdAt <= 0 || createdAt > now || now-createdAt >= OfficialTopUpTTLSeconds {
		return ErrTopUpExpired // 窗口起点非法也拒绝，不能让零值变成无限期订单。
	}
	return nil
}
