package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/refunddomestic"
)

// 服务层连接本地退款账本与微信官方SDK。
// 它不实现第二套签名算法或自定义退款网关。
// 商户私钥、平台公钥和渠道身份来自原配置，
// 浏览器无法选择替代收款账户。
// 停用渠道仍允许处理历史退款，
// 但必须按启用渠道的完整性规则校验凭据。
// 公私钥格式在回收权益之前实际解析，
// 明确的坏配置不能先扣掉用户权益再报错。
// 准备完成后才开始外部请求，
// 避免数据库事务跨越网络等待时间。
// 微信退款金额使用人民币整数分，
// 原订单总额和本次退款额完全相同。
// 本轮只支持整单退款，
// 不接受浏览器提供的部分退款金额。
// 首次提交返回受理不等于退款成功，
// 服务必须再查询原商户退款号。
// 准备状态可能代表尚未请求或响应丢失，
// 恢复时使用完全相同的请求参数再次提交。
// 微信对此身份提供幂等保证，
// 网络重试不会成为第二个退款意图。
// 处理中状态已有可信受理依据，
// 之后的恢复只查询，不重新提交。
// 原交易号和退款号都要匹配，
// 仅有正确签名不能认领另一笔交易。
// 总金额、退款额和币种都要一致，
// 少一分、多一分或缺少币种都会拒绝落账。
// 未知状态不能降级成成功，
// 必须保留可定位的协议错误。
// 微信成功时间用于财务退款日期，
// 管理员稍后查询不应把退款移到查询当天。
// 平台受理后关闭或异常属于待核对结果，
// 不会自动恢复权益或换退款号继续请求。
// 后台只轮询已受理的处理中记录，
// 不自动重放尚未核实的准备态请求。
// 分页使用递增主键游标，
// 长期未完成的早期订单不能阻挡后面的记录。
// 后台错误保留原退款编号供定位，
// 下一轮仍可继续查询相同持久记录。
// SDK错误可能带有完整响应头和正文，
// 面向页面的错误只保留安全错误码。
// 传输或验签故障明确视为结果未知，
// 不能用笼统的请求失败推断退款未发生。

// RefundPaymentOrder 复用既有商户配置、公钥响应验签和官方退款SDK。
// 退款不受当前销售开关影响；停用渠道仍须能处理历史实付。
// 配置必须先通过校验，避免明确缺配置时提前回收用户权益。
// 金额、订单号、渠道与退款身份全部来自持久订单，浏览器不能覆盖。
// 发起和恢复共用入口，prepared重试使用相同微信退款号与请求参数。
// 受理结果仍需查询确认，未拿到可信结果时不得写入成功。
// 本服务不读取付款人隐私，也不把SDK原始响应写入日志或前端。
func RefundPaymentOrder(ctx context.Context, module, trade, reason string, actor int, resume bool) (*model.PaymentRefund, error) {
	channelID, err := model.PaymentOrderChannel(module, trade)
	if err != nil {
		return nil, err
	}
	channel, ok := operation_setting.GetOfficialPaymentChannel(channelID, false)
	if !ok || channel.ChannelType != operation_setting.PaymentChannelWechat {
		return nil, model.ErrRefundUnsupported
	}
	validation := *channel
	validation.Enabled = true // 停用渠道也必须具备完整退款凭据。
	if err := operation_setting.ValidateOfficialPaymentChannel(validation); err != nil {
		return nil, fmt.Errorf("%w: refund channel", ErrOfficialPaymentConfig)
	}
	// 私钥与平台公钥先实际解析，错误配置不能导致提前回收权益。
	client, err := newOfficialWechatClient(ctx, channel.Config)
	if err != nil {
		return nil, fmt.Errorf("%w: refund client", ErrOfficialPaymentConfig)
	}
	var refund *model.PaymentRefund
	if resume {
		refund, err = model.GetPaymentRefund(module, trade)
		if err == nil && refund == nil {
			return nil, model.ErrRefundInvalid
		}
	} else {
		refund, err = model.PreparePaymentRefund(module, trade, reason, actor)
	}
	if err != nil {
		return nil, err
	}
	if refund.Status == "succeeded" {
		return refund, nil
	}
	if refund.ChannelID != channel.ID {
		return nil, model.ErrRefundInvalid
	}
	// 与支付接口一致使用短超时；超时留下prepared/processing供管理员恢复。
	ctx, cancel := context.WithTimeout(ctx, officialPaymentTimeout)
	defer cancel()
	api := refunddomestic.RefundsApiService{Client: client}
	if refund.Status == "prepared" {
		// 同一退款号多次提交只退一笔；不因HTTP错误改用新的随机身份。
		_, _, err = api.Create(ctx, refunddomestic.CreateRequest{
			OutTradeNo: core.String(refund.TradeNo), OutRefundNo: core.String(refund.RefundNo), Reason: core.String(refund.Reason),
			Amount: &refunddomestic.AmountReq{Refund: core.Int64(refund.AmountCents), Total: core.Int64(refund.AmountCents), Currency: core.String("CNY")},
		})
		if err != nil {
			return nil, paymentRefundGatewayError("submit", err)
		}
	}
	// SDK官方合同要求通过查询确认最终结果，不能把申请受理当作退款到账。
	result, _, err := api.QueryByOutRefundNo(ctx, refunddomestic.QueryByOutRefundNoRequest{OutRefundNo: core.String(refund.RefundNo)})
	if err != nil {
		return nil, paymentRefundGatewayError("query", err)
	}
	if result == nil || result.OutTradeNo == nil || *result.OutTradeNo != refund.TradeNo || result.OutRefundNo == nil || *result.OutRefundNo != refund.RefundNo || result.RefundId == nil || result.Amount == nil || result.Amount.Total == nil || *result.Amount.Total != refund.AmountCents || result.Amount.Refund == nil || *result.Amount.Refund != refund.AmountCents || result.Amount.Currency == nil || *result.Amount.Currency != "CNY" || result.Status == nil {
		return nil, fmt.Errorf("%w: refund identity or amount mismatch", ErrOfficialPaymentResponse)
	}
	var state string
	switch string(*result.Status) {
	case "SUCCESS":
		state = "succeeded"
	case "PROCESSING":
		state = "processing"
	case "CLOSED":
		state = "closed"
	case "ABNORMAL":
		state = "abnormal"
	default:
		return nil, fmt.Errorf("%w: unknown refund status", ErrOfficialPaymentResponse)
	}
	// 退款按微信实际成功时间入账，延迟查询不挪动退款所属日期。
	completedAt := int64(0)
	if state == "succeeded" {
		if result.SuccessTime == nil {
			return nil, fmt.Errorf("%w: missing refund success time", ErrOfficialPaymentResponse)
		}
		completedAt = result.SuccessTime.Unix()
	}
	// 只有验签、身份和整单金额均核实后，才允许推进本地状态。
	if err := model.ApplyPaymentRefundResult(refund.ID, state, *result.RefundId, completedAt); err != nil {
		return nil, err
	}
	return model.GetPaymentRefund(module, trade)
}

// StartPaymentRefundReconciler 每分钟同步已受理的微信退款，不依赖管理员保持页面打开。
// 只查询processing，不自动重放尚未确认受理的prepared或人工异常单。
// 游标分页避免前一批长期处理中阻挡后续订单；进程重启从持久状态恢复。
// 多实例查询可以重复，最终落库仍由订单行锁和成功终态保护。
func StartPaymentRefundReconciler() {
	if !common.IsMasterNode {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			lastID := 0
			for {
				var pending []model.PaymentRefund
				if err := model.DB.Where("status = ? AND id > ?", "processing", lastID).Order("id").Limit(50).Find(&pending).Error; err != nil {
					common.SysError(fmt.Sprintf("query pending payment refunds: %v", err))
					break
				}
				if len(pending) == 0 {
					break
				}
				for _, refund := range pending {
					lastID = refund.ID
					// 使用原操作人仅作恢复参数，不生成新的审批或重复权益变更。
					if _, err := RefundPaymentOrder(context.Background(), refund.Module, refund.TradeNo, "", refund.ActorID, true); err != nil {
						common.SysError(fmt.Sprintf("reconcile payment refund %d: %v", refund.ID, err))
					}
				}
			}
		}
	}()
}

// paymentRefundGatewayError 保留网关错误码供定位，同时排除SDK错误携带的响应头和原始正文。
// 传输与验签失败属于未知结果，提示管理员恢复原单，不生成新的退款身份。
// 业务错误（如余额不足）保留微信代码，便于在商户平台准确排查。
func paymentRefundGatewayError(operation string, err error) error {
	code := "TRANSPORT_OR_SIGNATURE_ERROR"
	var upstream *core.APIError
	if errors.As(err, &upstream) && upstream.Code != "" {
		code = upstream.Code
	}
	message := common.NewMessage("WeChat refund {{operation}} failed ({{code}}). Recover the original refund after resolving the issue.", map[string]any{"operation": operation, "code": code})
	return fmt.Errorf("%w: %w", ErrOfficialPaymentRequest, message)
}
