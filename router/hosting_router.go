package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

// registerHostingRoutes 不公开执行层管理接口，所有路径先验证 Oak 会话/PAT。
// 敏感动作由控制器绑定具体账号后校验单次证明；普通列表不要求反复验证。
// 所有路由沿用用户控制台认证，不把管理接口加入模型数据面。
// 用户范围 PAT 的路径授权表需要与新增路由同步维护。
// 读路由不要求重复敏感验证，便于用户查询未知业务结果。
// 写路由分别节流，避免轮询观察影响真实创建与重置的操作预算。
// 回调和取消只处理本地绑定的授权编号，不暴露远端 Stage ID。
// 原厂重置消费与恢复都使用 POST，GET 必须保持无消费副作用。
// 资源策略属于用户与 Key 的管理设置，不能由模型访问密钥修改。
// 审计中间件只读取模板路径和最终状态，不读取授权请求正文。
// 统一 Key 模型目录由根调度实现，账号目录不能替代它。
func registerHostingRoutes(api *gin.RouterGroup) {
	route := api.Group("/hosting", middleware.UserAuth(), middleware.DisableCache(), controller.AuditHostingRequests)
	route.GET("/accounts", controller.GetHostingAccounts)
	route.GET("/accounts/:id", controller.GetHostingAccount)
	// 创建只消费已绑定本人且经过敏感验证的授权，不直接接收任何凭据。
	route.POST("/accounts", middleware.UserCriticalRateLimit("hosting-manage"), controller.CreateHostingAccount)
	route.PATCH("/accounts/:id", middleware.UserCriticalRateLimit("hosting-manage"), controller.UpdateHostingAccount)
	route.DELETE("/accounts/:id", middleware.UserCriticalRateLimit("hosting-manage"), controller.DeleteHostingAccount)
	route.GET("/accounts/:id/models", controller.GetHostingAccountModels)
	route.GET("/accounts/:id/observation", controller.GetHostingObservation)
	// 观察刷新和真实测试各自节流，打开列表本身没有上游副作用。
	route.POST("/accounts/:id/refresh-observation", middleware.UserCriticalRateLimit("hosting-observation"), controller.RefreshHostingObservation)
	route.POST("/accounts/:id/test", middleware.UserCriticalRateLimit("hosting-test"), controller.TestHostingAccount)
	route.POST("/accounts/:id/reset-credits/consume", middleware.UserCriticalRateLimit("hosting-reset"), controller.ConsumeHostingResetCredit)
	route.GET("/accounts/:id/reset-operations", controller.GetHostingResetOperations)
	route.GET("/reset-operations/:id", controller.GetHostingResetOperation)
	// 恢复消费是 POST；GET 状态查询不能再次扣原厂重置次数。
	route.POST("/reset-operations/:id/resume", middleware.UserCriticalRateLimit("hosting-reset"), controller.ResumeHostingResetOperation)
	route.POST("/authorizations", middleware.UserCriticalRateLimit("hosting-auth"), controller.BeginHostingAuthorization)
	route.GET("/authorizations/:id", controller.GetHostingAuthorization)
	route.POST("/authorizations/:id/resume", middleware.UserCriticalRateLimit("hosting-auth"), controller.ResumeHostingAuthorization)
	route.POST("/authorizations/:id/callback", middleware.UserCriticalRateLimit("hosting-auth"), controller.CompleteHostingAuthorization)
	route.DELETE("/authorizations/:id", middleware.UserCriticalRateLimit("hosting-auth"), controller.CancelHostingAuthorization)
	// 两层策略复用同一格式和校验，不给每种资源建第二套 API Key。
	route.GET("/policy", controller.GetHostingResourcePolicy)
	route.PUT("/policy", middleware.UserCriticalRateLimit("hosting-policy"), controller.UpdateHostingResourcePolicy)
	route.GET("/tokens/:token_id/policy", controller.GetHostingResourcePolicy)
	route.PUT("/tokens/:token_id/policy", middleware.UserCriticalRateLimit("hosting-policy"), controller.UpdateHostingResourcePolicy)
	route.DELETE("/tokens/:token_id/policy", middleware.UserCriticalRateLimit("hosting-policy"), controller.DeleteHostingResourcePolicy)
	route.GET("/tokens/:token_id/models", controller.GetHostingTokenModels)
}
