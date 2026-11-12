package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetFinanceSubscriptions 独立于收入时段查询，便于查看未来订阅到期安排。
// 路由复用 finance_overview:read 和真实登录会话要求。
// 页大小固定为二十条；非法参数不回退默认值，避免展示错误月份。
// 未指定分类表示全部，不能把未知分类静默解释为全部。
// 查询只返回已有平台用户订阅和手工号池配置，不访问挂靠执行层。
// 两个来源共用相交区间与分页规则，浏览器不需要合并多份分页结果。
// 请求开始和结束都是必填项，省略日期通常表示表单尚未填完。
// 上限校验先于差值运算，防止极端整数溢出形成合法短区间。
// 分页页码有明确上限，避免 int 偏移计算溢出。
// 读取成功时即使没有条目也返回结构化空数组及 total。
// 底层数据库错误原样进入统一错误响应，不替换为无订阅。
func GetFinanceSubscriptions(c *gin.Context) {
	start, startErr := strconv.ParseInt(c.Query("start"), 10, 64)
	end, endErr := strconv.ParseInt(c.Query("end"), 10, 64)
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	category := c.DefaultQuery("category", "all")
	// 范围与财务总览一致，最大三百六十六天，限制单次查询跨度。
	if startErr != nil || endErr != nil || pageErr != nil || page < 1 || page > 1000000 ||
		start <= 0 || end > 253402300800 || end <= start || end-start > model.FinanceOverviewMaxDays*86400 ||
		(category != "all" && category != "user" && category != "pool") {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	result, err := model.GetFinanceSubscriptions(start, end, category, page)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

// SaveFinancePoolSubscription 创建和编辑共用白名单输入，不接收分类或用户编号。
// 写路由仅超级管理员可访问，财务总览读权限不能转化为写权限。
// 日期是 Unix 秒，允许保存当前图表范围之外的未来周期。
// body 不包含 ID，记录身份只取可信路由参数。
// 创建路由没有 id 参数，因此零值只表示插入新记录。
// 编辑路由必须有正整数 id，不允许把非法编号改写成创建操作。
// 名称的空白、字符数和起止顺序由模型统一验证。
// 控制器只负责 JSON 解码，不在两条写路由复制业务校验。
// 保存接口不接受备注中的 JSON 作为额外字段或远端操作。
// 修改后的周期可以不与当前查询窗口相交，刷新后不显示是正确结果。
// 此操作没有付款步骤，成功只表示人工周期已持久化。
func SaveFinancePoolSubscription(c *gin.Context) {
	var input struct {
		Name      string `json:"name"`
		StartTime int64  `json:"start_time"`
		EndTime   int64  `json:"end_time"`
	}
	id := 0
	if c.Param("id") != "" {
		parsed, err := strconv.Atoi(c.Param("id"))
		if err != nil || parsed <= 0 {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
		id = parsed
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	period, err := model.SaveFinancePoolSubscription(id, input.Name, input.StartTime, input.EndTime)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, period)
}

// DeleteFinancePoolSubscription 只移除手工维护的展示记录。
// 路由和正整数校验共同限定资源，不能落到用户订阅的删除流程。
// 错误返回前端统一处理，失败时不能关闭确认弹窗并伪装成功。
// 查询总览的只读能力不能作为删除授权。
// 删除失败继续返回失败响应，客户端不能提前移除时间条。
// 该接口没有批量删除语义，一次仅能处理路由指定的记录。
func DeleteFinancePoolSubscription(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err := model.DeleteFinancePoolSubscription(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
