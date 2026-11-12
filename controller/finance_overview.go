package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetFinanceOverview 返回 [start, end) 时段的经营汇总，时间为 Unix 秒。
// 路由层已校验财务总览读取能力并限制为网页登录；这里只校验时间范围。
// 范围缺失、倒置或超过上限都直接拒绝，不替调用方猜一个默认时段。
// 读取失败返回错误，不能用零值冒充“没有收入”。
func GetFinanceOverview(c *gin.Context) {
	start, startErr := strconv.ParseInt(c.Query("start"), 10, 64)
	end, endErr := strconv.ParseInt(c.Query("end"), 10, 64)
	// 仅接受公历四位年份可表达的时刻，避免极端整数在分日加偏移时溢出。
	if startErr != nil || endErr != nil || start <= 0 || end > 253402300800 || end <= start || end-start > model.FinanceOverviewMaxDays*86400 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	overview, err := model.GetFinanceOverview(start, end)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

// GetFinanceAdjustments 只读人工权益记录，受独立总览权限而非普通财务办理权限保护。
// 不读取可清理日志推算现金，分页与汇总使用同一时间范围。
// 开始和结束使用与总览相同的秒级左闭右开合同。
// 用户筛选只缩小已获授权的范围，不能给普通用户授予他人账本权限。
// 按原因筛选不修改记录，撤回与原始发放始终作为独立记录返回。
// 列表包含凭证与人工说明，因此仅通过会话专用总览路由暴露。
// 原记录不可编辑，前端传入的分页、关键字和时间都不会写进账本。
// 绑定参数避免把用户输入拼接成 SQL，查询失败不伪造空结果。
// size 由公共分页组件限制，再封顶为一百行，避免全量凭证一次返回。
// total 是整个筛选范围内的笔数，delta 不是当前页的小计。
// delta 是内部额度净变化，人民币实收由总览的 cash_cents 单独表示。
// 订阅记录的 delta 为零，客户端必须按 source 展示而非推断成零价。
// 旧日志无法可靠回填历史来源，这个入口只返回已经存在的主库记录。
// 当前名称与记账时名称可能不同，显示使用账本保留的原用户名。
// JSON 序列化不会返回请求参数摘要，不泄露不需要的内部幂等信息。
func GetFinanceAdjustments(c *gin.Context) {
	start, e1 := strconv.ParseInt(c.Query("start"), 10, 64)
	end, e2 := strconv.ParseInt(c.Query("end"), 10, 64)
	if e1 != nil || e2 != nil || start <= 0 || end > 253402300800 || end <= start || end-start > model.FinanceOverviewMaxDays*86400 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	page, size := common.GetPageQuery(c).GetPage(), min(common.GetPageQuery(c).GetPageSize(), 100)
	userID := 0
	if c.Query("user_id") != "" {
		var err error
		userID, err = strconv.Atoi(c.Query("user_id"))
		if err != nil || userID <= 0 {
			common.ApiErrorT(c, "Invalid parameters")
			return
		}
	}
	keyword := c.Query("keyword") // 只参与参数化筛选，不能成为 SQL 列名或表达式。
	if len([]rune(keyword)) > 100 {
		common.ApiErrorT(c, "Search keyword is too long")
		return
	}
	items, total, delta, err := model.ListQuotaLedgers(page, size, userID, c.Query("reason"), keyword, start, end)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "delta": delta, "page": page, "size": size})
}
