package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/hosting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// hostingPolicyToken 每次操作都绑定本人具体 API Key，不接受全局保留值。
// 没有策略记录也要先确认 Key 归属，避免越权查询暴露历史计费状态。
// 非法、缺失或非正整数都按不存在处理，不能默认为用户全局设置。
// 数据库故障保留原错误，不能误报为 Key 已删除。
// 管理员的个人接口也不放宽这一所有者约束。
func hostingPolicyToken(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("token_id"))
	if err != nil || id <= 0 {
		hostingError(c, gorm.ErrRecordNotFound)
		return 0, false
	}
	if _, err := model.GetTokenByIds(id, c.GetInt("id")); err != nil {
		hostingError(c, err)
		return 0, false
	}
	return id, true
}

// GetHostingResourcePolicy 返回具体 Key 的可编辑版本与来源。
// revision=0 且 legacy=true 表示没有策略，仍使用既有平台计费。
// 空数组但 legacy=false 表示该 Key 明确禁用全部来源。
// 不返回继承或全局版本字段，客户端不能再推导全局优先级。
// 读取失败和坏 JSON 显式返回错误，不猜测钱包或订阅的默认顺序。
// 此端点只读配置，不访问执行层或消耗原厂额度。
// 来源顺序由该 Key 单独维护，其他 Key 的编辑不会影响此响应。
// 即使历史默认行尚在数据库，公开读取也不会再返回它。
// legacy 表示既有平台合同，不表示钱包收费已经被用户再次确认。
func GetHostingResourcePolicy(c *gin.Context) {
	tokenID, ok := hostingPolicyToken(c)
	if !ok {
		return
	}
	userID := c.GetInt("id")
	var row model.ResourcePolicy
	err := model.DB.Where("user_id = ? AND token_id = ?", userID, tokenID).First(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		hostingError(c, err)
		return
	}
	snapshot, err := model.GetEffectiveResourcePolicy(userID, tokenID)
	if err != nil {
		hostingError(c, err)
		return
	}
	sources := []model.ResourceSource{}
	if snapshot != nil {
		sources = snapshot.Sources
	}
	common.ApiSuccess(c, gin.H{"revision": row.Revision, "legacy": snapshot == nil, "sources": sources})
}

// UpdateHostingResourcePolicy 完整替换顺序，不与旧 BillingPreference 叠加。
// 空 sources 是用户明确禁用该 Key 全部来源，不能当作删除策略。
// revision 与 sources 必须明确提供，缺字段不能按零值自动保存。
// 完整替换顺序而非合并，使用户看到的排列与实际授权一致。
// Key 所有权在控制器和事务层分别按各自边界确认。
// 格式错误、版本冲突和数据库失败保留不同的 HTTP 语义。
// 保存成功后重新读取统一响应，避免前后端各自猜测新版本。
// 禁用全部来源是合法操作，不替用户补一个钱包兜底。
// 保存策略不要求先有可用额度，账号状态在执行请求时验证。
// 新接入账号不会因为保存其他设置被自动插入当前来源列表。
// 只有顺序与 enabled 字段决定来源授权，界面描述文字不参与执行。
// 删除策略恢复旧平台合同，保存空数组不会调用删除路径。
// 当前响应可用于立即刷新界面，调用方不应自行猜测新的版本号。
func UpdateHostingResourcePolicy(c *gin.Context) {
	tokenID, ok := hostingPolicyToken(c)
	if !ok {
		return
	}
	var input struct {
		Revision *int64                  `json:"revision"`
		Sources  *[]model.ResourceSource `json:"sources"`
	}
	if !hostingBody(c, &input) {
		return
	}
	if input.Revision == nil || *input.Revision < 0 || input.Sources == nil {
		hostingError(c, &hosting.Error{Status: 400, Code: "hosting_invalid_request"})
		return
	}
	_, err := model.SaveResourcePolicy(c.GetInt("id"), tokenID, *input.Revision, *input.Sources)
	if err != nil {
		hostingError(c, err)
		return
	}
	GetHostingResourcePolicy(c)
}

// DeleteHostingResourcePolicy 明确恢复该 Key 的既有平台计费合同。
// 此操作不会继承任何全局来源，也不会影响同一用户的其他 Key。
// 禁用全部来源应保存空数组，不能用删除策略代替。
// 删除条件始终包含用户与 Key，避免批量清理他人授权。
// 没有策略行时删除也是幂等的，不创建新的空记录。
// 数据库失败会保留原策略并返回错误，不能伪报已恢复平台计费。
// 删除后立即返回 legacy=true，使界面能明确展示计费语义变化。
func DeleteHostingResourcePolicy(c *gin.Context) {
	tokenID, ok := hostingPolicyToken(c)
	if !ok {
		return
	}

	if err := model.DB.Where("user_id = ? AND token_id = ?", c.GetInt("id"), tokenID).Delete(&model.ResourcePolicy{}).Error; err != nil {
		hostingError(c, err)
		return
	}
	GetHostingResourcePolicy(c)
}
