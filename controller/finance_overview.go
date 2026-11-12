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
