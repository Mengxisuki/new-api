package model

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// FinancePoolSubscription 是人工维护的号池周期，不参与用户权益或收入结算。
// 每条记录可以代表一个号池或其中一个账号；名称由经营者自行定义。
// 续订可以新增另一条周期，保留旧周期供历史月份查询。
// 不保存供应商凭据，也不假定号池与渠道是一对一关系。
type FinancePoolSubscription struct {
	ID        int    `json:"id"`
	Name      string `json:"name" gorm:"type:varchar(128);not null"` // 名称只作展示，不作为远端资源引用。
	StartTime int64  `json:"start_time" gorm:"not null;index"`       // Unix 秒，区间左闭右开。
	EndTime   int64  `json:"end_time" gorm:"not null;index"`         // 日期输入的结束日由前端换为次日零点。
	CreatedAt int64  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

// FinanceSubscriptionPeriod 是两类周期统一的公开投影。
// id 只在所属分类内唯一；客户端以 category 与 id 联合标识行。
// user_id 为零表示人工号池记录，不代表平台用户零号。
// 缺失用户或套餐名称时仍保留编号，历史订阅不能因关联删除而消失。
// 名称反映当前关联记录，不是购买时的价格或权益快照。
// 该查询不连接订单镜像，因此不会因同交易号 TopUp 重复产生时间条。
// 用户删除后的订阅仍以 user_id 追溯，读取不会修改订阅生命周期。
// 状态仅供标注；取消记录仍展示原合同日期，不虚构取消发生时间。
// 号池的主键由数据库生成，不能接受客户端指定自增序列。
type FinanceSubscriptionPeriod struct {
	ID        int    `json:"id"`
	Category  string `json:"category"`
	Name      string `json:"name"`
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	PlanID    int    `json:"plan_id"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	Status    string `json:"status"` // 用户订阅保留取消状态，其他状态由时间推导。
}

// 分页在数据库中完成，避免平台用户增长后一次传输全部订阅。
// total 是所选分类、时段内的记录数，而非用户数或账号数。
type FinanceSubscriptionPage struct {
	Items []FinanceSubscriptionPeriod `json:"items"`
	Total int64                       `json:"total"`
}

// GetFinanceSubscriptions 查询与 [start,end) 相交的周期，包括未来和已取消记录。
// 两类都只投影展示字段，不读取用户密码、渠道密钥或上游账号凭据。
// UNION ALL 保留同一用户的多份订阅，不做会丢失历史的去重。
// 使用 GORM 子查询绑定参数，使 SQLite、MySQL、PostgreSQL 共用语义。
func GetFinanceSubscriptions(start, end int64, category string, page int) (*FinanceSubscriptionPage, error) {
	users := DB.Table("user_subscriptions AS s").
		Select("s.id, ? AS category, COALESCE(p.title, '') AS name, s.user_id, COALESCE(u.username, '') AS username, s.plan_id, s.start_time, s.end_time, s.status", "user").
		Joins("LEFT JOIN users AS u ON u.id = s.user_id").
		Joins("LEFT JOIN subscription_plans AS p ON p.id = s.plan_id").
		Where("s.start_time < ? AND s.end_time > ?", end, start)
	pools := DB.Model(&FinancePoolSubscription{}).
		Select("id, ? AS category, name, 0 AS user_id, '' AS username, 0 AS plan_id, start_time, end_time, '' AS status", "pool").
		Where("start_time < ? AND end_time > ?", end, start)
	// 分类值由控制器白名单校验；这里只决定参与查询的来源。
	source := DB.Raw("? UNION ALL ?", users, pools)
	if category == "user" {
		source = users
	}
	if category == "pool" {
		source = pools
	}
	result := &FinanceSubscriptionPage{Items: []FinanceSubscriptionPeriod{}}
	// 计数与分页都包裹同一投影，避免筛选条件在两个入口逐渐分叉。
	query := DB.Table("(?) AS periods", source)
	if err := query.Session(&gorm.Session{}).Count(&result.Total).Error; err != nil {
		return nil, err
	}
	// 相同开始时间也保持稳定顺序，跨页不会因数据库默认顺序重复记录。
	err := query.Order("start_time ASC, category ASC, id ASC").Limit(20).Offset((page - 1) * 20).Scan(&result.Items).Error
	return result, err
}

// SaveFinancePoolSubscription 只写人工周期，不修改真实套餐及任何账户权益。
// 服务端重复检查时间与名称，不能依赖浏览器表单验证。
// 更新只选择允许编辑的列，创建时间和记录身份不受请求覆盖。
// 不存在的更新明确返回错误，禁止把 UPDATE 零行当作成功。
func SaveFinancePoolSubscription(id int, name string, start, end int64) (*FinancePoolSubscription, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 128 || start <= 0 || end <= start || end > 253402300800 {
		return nil, errors.New("Invalid parameters")
	}
	period := &FinancePoolSubscription{ID: id, Name: name, StartTime: start, EndTime: end}
	if id == 0 {
		return period, DB.Create(period).Error
	}
	// 先定位已有记录，使时间未变的重复保存也能在 MySQL 上正确成功。
	if err := DB.First(&FinancePoolSubscription{}, id).Error; err != nil {
		return nil, err
	}
	result := DB.Model(&FinancePoolSubscription{}).Where("id = ?", id).
		Updates(map[string]any{"name": name, "start_time": start, "end_time": end, "updated_at": common.GetTimestamp()})
	return period, result.Error
}

// DeleteFinancePoolSubscription 删除的只是人工图表配置，没有资金或凭据副作用。
// 真实用户订阅不经过本函数，不能通过猜测编号删除用户购买记录。
// 未找到记录时返回可见错误，便于操作者发现过期页面。
func DeleteFinancePoolSubscription(id int) error {
	result := DB.Delete(&FinancePoolSubscription{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
