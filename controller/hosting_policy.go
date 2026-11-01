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

// hostingPolicyToken 先确认 Key 归属，默认策略用保留值零表示。
// 即使没有覆盖记录，查询他人 Key 也不能得到用户默认策略。
// 路径无 token_id 表示用户默认，数字零不是可编辑的真实 API Key。
// 非法或非正整数统一按不存在处理，避免产生不受支持的策略层级。
// 查询必须包含当前用户，管理员身份也不能在个人入口越权。
// 数据库故障保留原错误，不能错误地显示成 Key 已删除。
// 成功返回的编号只用于本次查询，不授予执行层任何额外权限。
func hostingPolicyToken(c *gin.Context) (int, bool) {
	if c.Param("token_id") == "" {
		return 0, true
	}
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

// GetHostingResourcePolicy 将继承来源与当前可编辑版本分开。
// 没有覆盖时 revision=0，即使继承的用户默认已经更新多次。
// 响应中的 revision 表示当前可编辑行，继承时为零。
// effective_revision 用于标识实际生效层级，不直接作为编辑版本提交。
// legacy 仅表示两层均无记录，前端不能把空 sources 自行解释成旧合同。
// Key 未配置覆盖时继承最新默认，默认变更无需批量改写所有 Key。
// 默认为空数组时仍是一份有效策略，应显示禁用全部来源。
// 数据损坏和读取失败会中止响应，不显示猜测的默认付费顺序。
// 此入口只读策略，不触发原厂额度观察或模型测试。
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
	inherited := tokenID != 0 && errors.Is(err, gorm.ErrRecordNotFound)
	snapshot, err := model.GetEffectiveResourcePolicy(userID, tokenID)
	if err != nil {
		hostingError(c, err)
		return
	}
	sources := []model.ResourceSource{}
	effectiveRevision := ""
	if snapshot != nil {
		sources, effectiveRevision = snapshot.Sources, snapshot.Revision
	}
	common.ApiSuccess(c, gin.H{"revision": row.Revision, "effective_revision": effectiveRevision, "inherited": inherited, "legacy": snapshot == nil, "sources": sources})
}

// UpdateHostingResourcePolicy 完整替换顺序，不与旧 BillingPreference 叠加。
// 空 sources 是用户明确禁用所有来源，不能当作删除覆盖。
// revision 与 sources 必须明确提供，缺字段不能按零值自动保存。
// 完整替换顺序而非合并，使用户看到的排列与实际授权一致。
// Key 所有权在控制器和事务层分别按各自边界确认。
// 格式错误、版本冲突和数据库失败保留不同的 HTTP 语义。
// 保存成功后重新读取统一响应，避免前后端对继承状态各算一套。
// 禁用全部来源是合法操作，不替用户补一个钱包兜底。
// 保存策略不要求先有可用额度，账号状态在执行请求时验证。
// 新接入账号不会因为保存其他设置被自动插入当前来源列表。
// 只有顺序与 enabled 字段决定来源授权，界面描述文字不参与执行。
// 删除覆盖是另外的明确操作，保存空数组不会调用删除路径。
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

// DeleteHostingResourcePolicy 仅删除本人 Key 覆盖，恢复最新用户默认。
// 不提供删除用户默认的入口，避免把明确禁用钱包的策略悄悄还原成旧偏好。
// 删除覆盖意味着重新继承默认，而不是禁用此 Key。
// 禁用全部来源应保存空数组，避免把两种动作混为一谈。
// 用户默认不允许通过此入口删除，否则旧 Key 会意外恢复旧计费合同。
// 数据库删除条件始终包含用户与 Key，不能批量清空别人设置。
// 删除完成后重新读取继承状态，让界面看到最新默认顺序。
// 没有覆盖行时删除也是幂等的，不需要创建空记录。
// 失败会原样保留已有覆盖，不能响应一个实际上未生效的继承结果。
func DeleteHostingResourcePolicy(c *gin.Context) {
	tokenID, ok := hostingPolicyToken(c)
	if !ok {
		return
	}
	if tokenID == 0 {
		hostingError(c, &hosting.Error{Status: 400, Code: "hosting_invalid_request"})
		return
	}
	if err := model.DB.Where("user_id = ? AND token_id = ?", c.GetInt("id"), tokenID).Delete(&model.ResourcePolicy{}).Error; err != nil {
		hostingError(c, err)
		return
	}
	GetHostingResourcePolicy(c)
}
