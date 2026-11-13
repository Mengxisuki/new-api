package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// FinanceExpenseInput 是支出台账写入合同；操作人来自登录会话，不从请求体读取。
type FinanceExpenseInput struct {
	Kind                  string `json:"kind"`
	Title                 string `json:"title"`
	AmountCents           int64  `json:"amount_cents"`
	OccurredAt            int64  `json:"occurred_at"`
	CoverageStart         int64  `json:"coverage_start"`
	CoverageEnd           int64  `json:"coverage_end"`
	RemainingBalanceCents int64  `json:"remaining_balance_cents"`
	Note                  string `json:"note"`
}

// GetFinanceExpenses 返回指定期间的现金支出和成本支出，失败时不返回零值假数据。
func GetFinanceExpenses(c *gin.Context) {
	// 查询范围与总览保持同一秒级左闭右开合同，避免两个看板期间不一致。
	start, end, ok := parseFinanceExpenseRange(c)
	if !ok {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	result, err := model.GetFinanceExpenseSummary(start, end)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

// CreateFinanceExpense 登记一笔现金支出；订阅覆盖期与充值初始余额由后端严格校验。
func CreateFinanceExpense(c *gin.Context) {
	// JSON 解析失败直接拒绝，不把缺失字段当作零金额或默认日期。
	var input FinanceExpenseInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	expense, err := model.CreateFinanceExpense(model.FinanceExpense{
		// 操作人由会话中间件记录，业务模型只保存支出事实和说明。
		Kind: input.Kind, Title: input.Title, AmountCents: input.AmountCents,
		OccurredAt: input.OccurredAt, CoverageStart: input.CoverageStart, CoverageEnd: input.CoverageEnd,
		RemainingBalanceCents: input.RemainingBalanceCents, Note: input.Note,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, expense)
}

// ConsumeFinanceExpenseBalance 用当前剩余余额登记充值采购消耗，操作不可回退。
func ConsumeFinanceExpenseBalance(c *gin.Context) {
	// 路径编号必须是正整数，防止把任意字符串带入查询条件。
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var input struct {
		RemainingBalanceCents int64 `json:"remaining_balance_cents"`
		ConsumedAt            int64 `json:"consumed_at"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if input.ConsumedAt == 0 {
		// 未提供历史发生时间时使用服务器时间，保证默认记录可追溯。
		input.ConsumedAt = common.GetTimestamp()
	}
	expense, err := model.ConsumeFinanceExpenseBalance(id, input.RemainingBalanceCents, input.ConsumedAt)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, expense)
}

func parseFinanceExpenseRange(c *gin.Context) (int64, int64, bool) {
	// 结束时间限制在四位年份范围内，且跨度复用财务总览的 366 天上限。
	start, e1 := strconv.ParseInt(c.Query("start"), 10, 64)
	end, e2 := strconv.ParseInt(c.Query("end"), 10, 64)
	return start, end, e1 == nil && e2 == nil && start > 0 && end > start && end <= 253402300800 && end-start <= model.FinanceOverviewMaxDays*86400
}
