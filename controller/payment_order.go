package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// 管理列表和退款提交是不同动作。
// 列表路由只要求读取能力，
// 不能通过查询参数触发微信请求。
// 退款路由还要求退款能力，
// 普通用户和财务角色不能越过管理员门槛。
// 两个入口都要求真实登录会话，
// 个人访问令牌不能承接外部资金动作。
// 页码在控制器校验后才交给查询层，
// 避免极大偏移或负值变成无界查询。
// 交易号仅作为绑定参数使用，
// 不允许浏览器提供表名和查询表达式。
// 退款原因是商户展示用的业务文本，
// 最终长度和空值约束由模型统一检查。
// 恢复请求的原因不替换原退款原因，
// 以保持微信幂等请求的参数完全一致。
// 处理器不接收金额、用户或商户配置，
// 这三项都必须从原订单找到。
// 进程锁不能替代持久锁和唯一索引，
// 多个进程仍由模型层保护一次回收。
// 接口报错不表示微信一定没收到请求，
// 管理员应刷新原单再使用恢复动作。

// GetPaymentOrders 只返回订单安全摘要，权限在管理路由强制执行。
// 三业务独立分页，交易号搜索不会包含代充值敏感资料。
// 页面读取不调用微信，也不会修改付款或退款状态。
func GetPaymentOrders(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > 1000000 {
		common.ApiError(c, model.ErrRefundInvalid)
		return
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	if len(keyword) > 255 {
		common.ApiError(c, model.ErrRefundInvalid)
		return
	}
	items, total, err := model.ListPaymentOrders(c.DefaultQuery("module", "topup"), keyword, c.Query("status"), page)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total})
}

// RefundPaymentOrder 明确区分首次整单退款和原单恢复。
// 恢复只能作用于已经存在的退款，不会重新建立意图或回收权益。
// 所有金额在后端冻结，客户端仅提交原单身份与管理员原因。
// 进程锁减少同实例重复点击，跨实例仍依赖原单行锁和微信幂等号。
func RefundPaymentOrder(c *gin.Context) {
	var req struct {
		Module  string `json:"module"`
		TradeNo string `json:"trade_no"`
		Reason  string `json:"reason"`
		Resume  bool   `json:"resume"`
	}
	if !rechargeBody(c, &req) {
		return
	}
	if req.TradeNo == "" || len(req.TradeNo) > 255 {
		common.ApiError(c, model.ErrRefundInvalid)
		return
	}
	LockOrder("refund:" + req.Module + ":" + req.TradeNo)
	defer UnlockOrder("refund:" + req.Module + ":" + req.TradeNo)
	result, err := service.RefundPaymentOrder(c.Request.Context(), req.Module, req.TradeNo, req.Reason, c.GetInt("id"), req.Resume)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
