package model

import "gorm.io/gorm"

// 查询只投影授信和用户公开标识，不包含账户凭据、邮箱或税务资料。
// 财务可读授信占用，但修改配置仍仅允许支付设置管理员。
type CreditAccountView struct {
	CreditAccount
	Username    string `json:"username"` // 用户删除后保留编号和历史账本。
	DisplayName string `json:"display_name"`
}

// 账户列表使用服务器分页；筛选条件为用户名或显示名。
// SQL 值全部绑定，不能把搜索内容拼入查询语法。
// 查询以授信表为主体，用户删除不会让欠款行消失。
// 名称仅用于当下识别，不冒充下单时的历史名称。
// 金额字段直接来自持久账本，不从当页流水重算。
// 列表读取不会自动新建空授信账户。
// 计数与取页分别克隆查询会话，避免 GORM 累加联表。
// 搜索结果为空时返回零总数和空数组。
// 底层错误保持返回，控制器统一隐藏数据库细节。
func ListCreditAccounts(page, size int, keyword string) ([]CreditAccountView, int64, error) {
	query := DB.Table("credit_accounts").Joins("LEFT JOIN users ON users.id = credit_accounts.user_id")
	if keyword != "" {
		query = query.Where("users.username LIKE ? OR users.display_name LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []CreditAccountView{} // 空集合仍返回数组，不能以 null 混淆读取失败。
	err := query.Session(&gorm.Session{}).Select("credit_accounts.*, COALESCE(users.username, '') AS username, COALESCE(users.display_name, '') AS display_name").
		Order("credit_accounts.user_id DESC").Offset((page - 1) * size).Limit(size).Scan(&items).Error
	return items, total, err
}

// 债务列表附带姓名用于对账，结清凭证仅出现在受保护财务接口。
type CreditEntryView struct {
	CreditEntry
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// 筛选和汇总采用同一范围；总额是筛选结果的金额，不是钱包余额。
// 未结清、已结清与撤销分开展示，不把所有记录求和当作当前欠款。
// 用户筛选零表示所有授权可见的财务记录。
// 该全量范围只可由受保护财务路由调用。
// 状态值已在控制器白名单校验，查询不解释未知业务状态。
// 金额汇总覆盖全部匹配页，不是当前二十行的小计。
// 各终态仍保留原金额，状态筛选决定汇总的业务含义。
// 只读统计不恢复额度，也不修改债务生命周期。
// 每次聚合和取页使用独立查询会话，防止累积 JOIN。
// 分页期间允许新流水写入，界面可以通过刷新获取新快照。
// 因此本列表用于运营查看，不作为原子资金操作的依据。
func ListCreditEntries(page, size, userID int, status, keyword string) ([]CreditEntryView, int64, int64, error) {
	query := DB.Table("credit_entries").Joins("LEFT JOIN users ON users.id = credit_entries.user_id")
	if userID > 0 {
		query = query.Where("credit_entries.user_id = ?", userID)
	}
	if status != "" {
		query = query.Where("credit_entries.status = ?", status)
	}
	if keyword != "" {
		query = query.Where("credit_entries.trade_no LIKE ? OR users.username LIKE ? OR users.display_name LIKE ?", "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
	}
	var total, amount int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	if err := query.Session(&gorm.Session{}).Select("COALESCE(SUM(credit_entries.amount_cents), 0)").Scan(&amount).Error; err != nil {
		return nil, 0, 0, err
	}
	items := []CreditEntryView{}
	err := query.Session(&gorm.Session{}).Select("credit_entries.*, COALESCE(users.username, '') AS username, COALESCE(users.display_name, '') AS display_name").
		Order("credit_entries.id DESC").Offset((page - 1) * size).Limit(size).Scan(&items).Error
	return items, total, amount, err
}
