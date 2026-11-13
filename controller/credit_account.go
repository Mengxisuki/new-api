package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// 授信配置和财务读取共用安全投影，路由分别校验各自权限。
// 分页在入口归一化，错误经共享映射返回稳定业务原因。
func GetCreditAccounts(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	items, total, err := model.ListCreditAccounts(max(page, 1), min(max(size, 1), 100), strings.TrimSpace(c.Query("keyword")))
	if err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total})
}

// 保存接口只绑定配置字段，占用额和审计字段始终由模型事务维护。
// 三个模块开关可独立保存；关闭总开关或全部模块是合法的撤销授权操作。
func SaveCreditAccount(c *gin.Context) {
	var input struct {
		UserID              int   `json:"user_id"`
		Enabled             bool  `json:"enabled"`
		LimitCents          int64 `json:"limit_cents"`
		RechargeEnabled     bool  `json:"recharge_enabled"`
		TopupEnabled        bool  `json:"topup_enabled"`
		SubscriptionEnabled bool  `json:"subscription_enabled"`
		Version             int   `json:"version"`
	}
	if !rechargeBody(c, &input) {
		return
	}
	err := model.SaveCreditAccount(model.CreditAccount{
		UserID: input.UserID, Enabled: input.Enabled, LimitCents: input.LimitCents,
		RechargeEnabled: input.RechargeEnabled, TopupEnabled: input.TopupEnabled,
		SubscriptionEnabled: input.SubscriptionEnabled, Version: input.Version,
	}, c.GetInt("id"))
	if err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// 用户选择器只返回启用用户的公开标识，不复用包含密钥的用户模型响应。
func SearchCreditUsers(c *gin.Context) {
	items := []struct {
		ID          int    `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	}{}
	keyword := strings.TrimSpace(c.Query("keyword"))
	query := model.DB.Model(&model.User{}).Select("id", "username", "display_name").Where("status = ?", common.UserStatusEnabled)
	if keyword != "" {
		id, _ := strconv.Atoi(keyword)
		query = query.Where("id = ? OR username LIKE ? OR display_name LIKE ?", id, "%"+keyword+"%", "%"+keyword+"%")
	}
	if err := query.Order("id DESC").Limit(30).Scan(&items).Error; err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

// 财务列表支持状态、模块、用户与交易号搜索；身份由受保护路由决定。
// 未传 module 保持旧接口语义，传入未知模块则直接拒绝。
func GetCreditEntries(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	userID, err := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	if err != nil || userID < 0 {
		rechargeError(c, model.ErrCreditInvalid)
		return
	}
	status := c.Query("status")
	switch status {
	case "", "outstanding", "settled", "cancelled", "refunded", "write_off":
	default:
		rechargeError(c, model.ErrCreditInvalid)
		return
	}
	module := strings.TrimSpace(c.Query("module"))
	if module != "" && module != model.CreditModuleRecharge && module != model.CreditModuleTopup && module != model.CreditModuleSubscription {
		rechargeError(c, model.ErrCreditInvalid)
		return
	}
	items, total, amount, err := model.ListCreditEntriesWithModule(max(page, 1), 20, userID, status, module, strings.TrimSpace(c.Query("keyword")))
	if err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "amount_cents": amount})
}

// 财务确认只接收收款凭证和说明，不能任意调整结清金额或债务所有者。
// 模型事务会再次锁定来源单、用户和债务，并保留首次登记信息。
func SettleCreditEntry(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		rechargeError(c, model.ErrCreditInvalid)
		return
	}
	var input struct {
		Reference string `json:"reference"`
		Note      string `json:"note"`
	}
	if !rechargeBody(c, &input) {
		return
	}
	if err := model.SettleCreditEntry(id, c.GetInt("id"), input.Reference, input.Note); err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// WriteOffCreditEntry 只接受必填核销原因；核销不计现金、不撤销已履行权益。
func WriteOffCreditEntry(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		rechargeError(c, model.ErrCreditInvalid)
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if !rechargeBody(c, &input) {
		return
	}
	if strings.TrimSpace(input.Reason) == "" {
		rechargeError(c, model.ErrCreditInvalid)
		return
	}
	if err := model.WriteOffCreditEntry(id, c.GetInt("id"), input.Reason); err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// GetSelfCreditAccount 仅按会话身份读取本人授信，不能指定其他用户。
// 未配置返回 null；数据库失败与没有授信分开表达。
// 个人资料页只展示额度，调整授权仍使用 Root 管理入口。
func GetSelfCreditAccount(c *gin.Context) {
	account, err := model.GetCreditAccount(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, account)
}
