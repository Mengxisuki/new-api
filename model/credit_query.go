package model

import "gorm.io/gorm"

// 查询只投影授信和用户公开标识，不包含账户凭据、邮箱或税务资料。
// 财务可读授信占用，但修改配置仍仅允许支付设置管理员。
type CreditAccountView struct {
	CreditAccount
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// 账户列表使用服务器分页；筛选条件为用户名或显示名。
// SQL 值全部绑定，不能把搜索内容拼入查询语法。
func ListCreditAccounts(page, size int, keyword string) ([]CreditAccountView, int64, error) {
	query := DB.Table("credit_accounts").Joins("LEFT JOIN users ON users.id = credit_accounts.user_id")
	if keyword != "" {
		query = query.Where("users.username LIKE ? OR users.display_name LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []CreditAccountView{}
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

// ListCreditEntries 保留旧参数顺序，调用方未传模块时查询全部模块。
func ListCreditEntries(page, size, userID int, status, keyword string) ([]CreditEntryView, int64, int64, error) {
	return ListCreditEntriesWithModule(page, size, userID, status, "", keyword)
}

// ListCreditEntriesWithModule 按用户、状态和模块筛选同一组债务记录。
// 汇总与分页使用完全相同的过滤条件，未知模块由控制器拒绝。
func ListCreditEntriesWithModule(page, size, userID int, status, module, keyword string) ([]CreditEntryView, int64, int64, error) {
	query := DB.Table("credit_entries").Joins("LEFT JOIN users ON users.id = credit_entries.user_id")
	if userID > 0 {
		query = query.Where("credit_entries.user_id = ?", userID)
	}
	if status != "" {
		query = query.Where("credit_entries.status = ?", status)
	}
	if module != "" {
		query = query.Where("credit_entries.module = ?", module)
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
