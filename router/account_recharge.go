package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

// 代充值只开放控制台网页登录；
// 网关通知单独验签，
// 不接受浏览器付款声明。
// 普通用户、业务员和商品管理员使用不同路由组，
// 避免整体提升角色门槛。
// 每条运营路由绑定明确动作能力，
// 不能仅依赖前端菜单是否出现。
// 用户提交与运营读取分别节流，
// 查看摘要不会消耗敏感动作预算。
// 退款接口不放进业务员组，
// 避免办理权限间接获得资金能力。
// 微信回调使用网关签名而非登录cookie，
// 独立于用户在线状态。
// 接口模板中只有订单编号，
// 完整邮箱和凭据不得放进 URL。
// 敏感响应设置no-store，
// 普通列表也不能被共享代理缓存。
// PAT 没有此业务的路由授权，
// 不能替代网页登录和单次证明。
// 新增路由不修改模型推理的数据面，
// 也不要求 GPTLoad改造。
// 后台审计由 RechargeAuth执行，
// 不能依赖管理员等级才留痕。
func registerRechargeRoutes(api *gin.RouterGroup) {
	api.GET("/account-recharge/epay/notify", controller.RechargeEpayNotify)
	api.POST("/account-recharge/epay/notify", controller.RechargeEpayNotify)
	// 本人订单在控制器里固定当前用户ID，
	// 客户端不能指定所有者。
	user := api.Group("/account-recharge", middleware.UserAuth(), controller.RechargeSessionOnly)
	user.GET("/products", controller.GetRechargeProducts)
	user.POST("/quote", controller.QuoteRechargeOrder)
	user.POST("/orders", middleware.UserCriticalRateLimit("recharge-payment"), controller.CreateRechargeOrder)
	user.GET("/orders/self", controller.GetRechargeOrders)
	user.GET("/orders/:id", controller.GetRechargeOrder)
	user.POST("/orders/:id/pay", middleware.UserCriticalRateLimit("recharge-payment"), controller.PayRechargeOrder)
	user.POST("/orders/:id/account-info", middleware.UserCriticalRateLimit("recharge-info"), controller.SubmitRechargeInfo)
	// 业务员采用低门槛认证和独立权限，
	// 同时启用既有管理写审计。
	ops := api.Group("/account-recharge/operations", middleware.RechargeAuth(), controller.RechargeSessionOnly)
	ops.GET("/orders", middleware.RequirePermission(authz.RechargeRead), controller.GetRechargeOrders)
	ops.GET("/orders/:id", middleware.RequirePermission(authz.RechargeRead), controller.GetRechargeOrder)
	ops.POST("/orders/:id/action", middleware.RequirePermission(authz.RechargeWrite), controller.ActRechargeOrder)
	ops.POST("/orders/:id/reveal-account-info", middleware.RequirePermission(authz.RechargeReveal), middleware.UserCriticalRateLimit("recharge-reveal"), controller.RevealRechargeInfo)
	// 商品与资金管理要求管理员身份及具体能力，
	// 财务不会自动获得资料权限。
	admin := api.Group("/account-recharge/admin", middleware.AdminAuth(), controller.RechargeSessionOnly)
	admin.GET("/products", middleware.RequirePermission(authz.RechargeProductsRead), controller.GetRechargeProducts)
	admin.PUT("/products", middleware.RequirePermission(authz.RechargeProductsWrite), controller.SaveRechargeProduct)
	admin.POST("/orders/:id/refund", middleware.RequirePermission(authz.RechargeRefund), middleware.UserCriticalRateLimit("recharge-refund"), controller.RefundRechargeOrder)
}
