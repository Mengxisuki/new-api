package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

// 事件消费独立于通用API压缩组，保证心跳与消息立即刷新。
// 所有入口保留请求限流、路径标记和no-store；密钥不出现在URL。
// 事件令牌只允许消费和ACK，管理面只接受Root网页登录。
// 消费路由不挂通用UserAuth，防止事件凭证被当成后台访问令牌。
// Root管理路由再检查真实浏览器会话，个人令牌不能生成新凭证。
// ACK使用POST并把投递编号放正文，认证密钥只放请求头。
// 令牌创建额外使用用户级限流和已有单次安全证明。
// 接口路径均为固定模板，审计不能采集用户提供的资料正文。
// 本组无gzip中间件，保留Gin响应刷新与写入期限能力。
func registerEventHubRoutes(router *gin.Engine) {
	hub := router.Group("/api/event-hub", middleware.RouteTag("api"), middleware.GlobalAPIRateLimit())
	hub.GET("/stream", controller.HubClientAuth(), controller.HubStream)
	hub.POST("/ack", controller.HubClientAuth(), controller.HubAck)
	admin := hub.Group("", middleware.RootAuth(), controller.HubBrowserOnly)
	admin.GET("", controller.HubOverview)
	admin.PUT("/settings", controller.HubUpdateSettings)
	admin.GET("/events", controller.HubListEvents)
	admin.POST("/consumers", controller.HubCreateConsumer)
	admin.PUT("/consumers/:id", controller.HubUpdateConsumer)
	admin.GET("/consumers/:id", controller.HubConsumerDetail)
	admin.POST("/consumers/:id/tokens", middleware.UserCriticalRateLimit("event-hub-token"), controller.HubCreateToken)
	admin.DELETE("/consumers/:id/tokens/:token_id", controller.HubRevokeToken)
	admin.POST("/consumers/:id/replay", controller.HubReplayEvent)
}
