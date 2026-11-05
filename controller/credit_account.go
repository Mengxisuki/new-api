package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// 授信配置和财务读取共用只读投影，路由分别校验各自能力。
// 分页在入口归一化，避免用户传入无界列表大小。
// 支付设置调用此接口时使用超级管理员权限。
// 财务调用时仅需财务读取能力，返回相同的安全摘要。
// 没有读取权限的请求应在路由阶段被拒绝。
// 不接受客户端指定另一套表或自由排序表达式。
// 空搜索表示全部已配置用户，不自动列出所有注册用户。
// 返回的总数用于服务器分页，不依赖浏览器筛选。
// 读取失败不能返回空列表冒充尚未配置。
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

// 不绑定完整账本结构，前端不能写 used_cents 或更新人。
// 首期预留模块字段明确拒绝开启，避免配置成功但实际不可用。
// 更新人始终取认证上下文，不能由请求体替换。
// 金额要求整数分，JSON 小数绑定会被拒绝。
// 占用余额不出现在输入结构，避免任意清零欠款。
// 版本来自用户打开配置时的快照，保存冲突需刷新。
// 关闭授权使用显式 false，不能被数据库默认值覆盖。
// 保存成功仅表示授权修改，没有创建消费或收款。
// 旧账单不会随用户模块授权变化而迁移业务归属。
// 错误经共享映射返回稳定业务原因，不能输出 SQL。
func SaveCreditAccount(c *gin.Context) {
	var input struct {
		UserID              int   `json:"user_id"`
		Enabled             bool  `json:"enabled"`
		LimitCents          int64 `json:"limit_cents"` // 接口以整数分传输。
		RechargeEnabled     bool  `json:"recharge_enabled"`
		TopupEnabled        bool  `json:"topup_enabled"` // 仅用于明确拒绝未接入模块。
		SubscriptionEnabled bool  `json:"subscription_enabled"`
		Version             int   `json:"version"` // 修改必须提供读取到的配置版本。
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

// 用户选择器只返回启用用户的编号和名称，不能复用包含密钥的用户模型响应。
// 搜索结果有界，授权仍由独立保存接口决定。
// 只查询启用用户，停用账户不能获得新的挂帐授权。
// 保存阶段仍会检查状态，搜索结果不构成授权承诺。
// 编号采用整数绑定，与用户名匹配共用有界结果。
// 显示名称可能为空，前端同时显示编号和用户名。
// 搜索候选不包含钱包余额或个人认证信息。
// 该接口仅处于支付设置路由，不对普通用户开放。
// 查询无结果与数据库不可用需要明确区分。
func SearchCreditUsers(c *gin.Context) {
	var items = []struct {
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

// 财务列表支持状态、用户与交易号搜索；身份由真实路由决定。
// 用户自行拼接查询参数不会获得此路由的读取能力。
// 页面默认请求未结清，接口也支持显式查看历史终态。
// 用户编号为负或不能解析时直接拒绝，不默认为全部。
// 未知状态拒绝，防止拼写错误意外扩大查询范围。
// 页大小固定二十条，前端按返回总数控制翻页。
// 当前金额合计与列表使用相同过滤条件。
// 不把收款凭证带入代充值买家普通摘要。
// 结清与退款写入通过各自事务，读取没有资金副作用。
func GetCreditEntries(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	userID, err := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	if err != nil || userID < 0 {
		rechargeError(c, model.ErrCreditInvalid)
		return
	}
	status := c.Query("status")
	switch status {
	case "", "outstanding", "settled", "cancelled", "refunded":
	default:
		rechargeError(c, model.ErrCreditInvalid)
		return
	}
	items, total, amount, err := model.ListCreditEntries(max(page, 1), 20, userID, status, strings.TrimSpace(c.Query("keyword")))
	if err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "amount_cents": amount})
}

// 财务确认只接收收款凭证和说明，不能任意调整结清金额或所有者。
// 真实结清与恢复额度由模型原子完成，不使用前端计算结果。
// 路由要求财务写能力和真实网页登录身份。
// 订单是否还可结清由模型在事务内再次判断。
// 不使用客户端传来的金额、状态或交易号更新债务。
// 空凭证被拒绝，备注可为空但长度受限。
// 返回成功后客户端重新获取授信和债务摘要。
// 网络结果未知时允许查询原记录再重试。
// 重复成功不会生成第二份收款登记或授信返还。
func SettleCreditEntry(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		rechargeError(c, model.ErrCreditInvalid)
		return
	}
	var input struct {
		Reference string `json:"reference"` // 线下收款凭证，必填。
		Note      string `json:"note"`      // 可选说明，不能包含付款账户凭据。
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
