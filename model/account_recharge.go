package model

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// 代充值是独立商品业务，
// 付款不能开通平台订阅或增加平台额度。
// 金额使用人民币分，
// 避免商品报价及付款回调出现浮点尾差。
// 商品下架保留历史订单，
// 付款时复制商品内容形成不可追溯修改的快照。
// 商品启用只控制新购买，
// 历史支付通知仍可确认原单。
// 允许余额是逐商品策略，
// 不代表系统全局关闭微信收款。
// 价格不引用模型倍率、充值折扣或包月额度计算。
// 说明由管理员填写账户要求、套餐周期、办理时效和售后规则。
// 商品不保存上游接口、密钥或自动履约配置。
// 所有历史订单继续使用其冻结说明，
// 不能反向读取最新商品替换。
type AccountRechargeProduct struct {
	ID                 int    `json:"id"`
	Title              string `json:"title" gorm:"size:128;not null"` // 面向用户的套餐名称。
	Description        string `json:"description" gorm:"type:text"`   // 账户条件与售后等购买说明。
	Summary            string `json:"summary" gorm:"size:500"`        // 套餐权益简介，与购买限制分开。
	Provider           string `json:"provider" gorm:"size:80"`        // 服务商展示名，不参与渠道路由。
	Period             string `json:"period" gorm:"size:80"`          // 套餐周期文案，不代表自动续费。
	DeliveryTime       string `json:"delivery_time" gorm:"size:160"`  // 人工办理预计时效，非上游状态。
	Icon               string `json:"icon" gorm:"size:128"`           // 模型广场图标键，不存远程图片地址。
	PriceCents         int64  `json:"price_cents" gorm:"not null"`    // 始终为人民币分。
	Enabled            bool   `json:"enabled"`                        // 零值不上架，禁止迁移默认自动售卖。
	AllowWalletPayment bool   `json:"allow_wallet_payment"`           // 单商品控制余额付款。
	SortOrder          int    `json:"sort_order"`
	Version            int    `json:"version" gorm:"not null"` // 编辑和报价的乐观版本。
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
}

// 普通序列化不会返回凭据、内部备注或支付入口原文。
// 客户和业务员的列表只读取这份安全摘要，
// 敏感读取必须走独立接口。
// 订单号和用户幂等键分别唯一，
// 支付通知不能跨业务认领。
// 购买者和处理人属于两个不同权限域，
// 不能相互替代。
// 支付状态只由验签到账、余额事务或管理员退款确认改变。
// 办理状态只记录本平台人工结果，
// 不表示已验证供应商结果。
// 有效支付凭据生成后保持原付款方式，
// 避免同单双收。
// 用户重复请求使用用户编号与请求UUID 的唯一组合识别。
// 付款方式及报价参与请求摘要，
// 同一请求键不能改成另一商品。
// 密文字段包含邮箱与完整JSON，
// 邮箱掩码不能用作凭据校验。
// 普通序列化不含内部说明；
// 运营详情另按处理权投影。
// 退款同步标志区分数据库已返还与缓存尚未确认两个事实。
// 业务员不能直接更新本结构，
// 控制器只接受动作白名单。
// 完成时间来自实际人工确认，
// 支付回调不能填入该字段。
// 过期时间是付款窗口，
// 不是支付未发生的证明。
// 订单不能硬删除，
// 否则无法核对迟到通知及历史退款。
// 版本在处理和敏感读取时推进，
// 防止旧页面覆盖新分配。
// 退款实际扣除值不受之后的汇率或商品价格变化影响。
// 所有终态清除在线密文；
// 备份保留仍由部署策略管理。
type AccountRechargeOrder struct {
	ID                int    `json:"id"`
	UserID            int    `json:"user_id" gorm:"index;uniqueIndex:idx_recharge_request,priority:1"`
	RequestID         string `json:"-" gorm:"size:36;uniqueIndex:idx_recharge_request,priority:2"`
	RequestHash       string `json:"-" gorm:"size:64"` // 同一幂等键不能换商品或付款方式。
	ProductID         int    `json:"product_id" gorm:"index"`
	Title             string `json:"title" gorm:"size:128"`         // 购买时的商品名称。
	Description       string `json:"description" gorm:"type:text"`  // 购买时的规则快照。
	Summary           string `json:"summary" gorm:"size:500"`       // 套餐权益简介，与购买限制分开。
	Provider          string `json:"provider" gorm:"size:80"`       // 服务商展示名，不参与渠道路由。
	Period            string `json:"period" gorm:"size:80"`         // 套餐周期文案，不代表自动续费。
	DeliveryTime      string `json:"delivery_time" gorm:"size:160"` // 人工办理预计时效，非上游状态。
	Icon              string `json:"icon" gorm:"size:128"`          // 模型广场图标键，不存远程图片地址。
	BaseCents         int64  `json:"base_cents"`
	MoneyCents        int64  `json:"money_cents"` // 包括可选开票附加费。
	TradeNo           string `json:"trade_no" gorm:"size:32;uniqueIndex"`
	PaymentMethod     string `json:"payment_method" gorm:"size:64"`
	PaymentProvider   string `json:"payment_provider" gorm:"size:32"`
	PaymentStatus     string `json:"payment_status" gorm:"size:24;index"`
	PaymentData       string `json:"-" gorm:"type:text"`   // 原订单二维码/跳转资料供继续付款。
	ProviderRef       string `json:"-" gorm:"size:128"`    // 支付方交易号仅用于内部对账。
	WalletQuota       int    `json:"-" gorm:"type:bigint"` // 冻结实际扣除的内部额度整数。
	WalletRate        string `json:"-" gorm:"size:64"`     // 冻结付款时汇率，退款不重新换算。
	InvoiceRequired   bool   `json:"invoice_required"`
	Status            string `json:"status" gorm:"size:24;index"`
	AssigneeID        int    `json:"assignee_id" gorm:"index"`
	EmailMask         string `json:"email_mask" gorm:"size:254"`
	Secret            string `json:"-" gorm:"type:text"` // 邮箱和session一起认证加密。
	InfoSubmittedAt   int64  `json:"info_submitted_at"`
	PublicNote        string `json:"public_note" gorm:"size:1000"`
	InternalNote      string `json:"-" gorm:"size:1000"`
	RefundRef         string `json:"refund_ref,omitempty" gorm:"size:128"` // 微信退款编号或历史人工退款凭证。
	WalletSyncPending bool   `json:"wallet_sync_pending"`                  // 退款已入库但缓存尚未同步时，管理员可恢复原单。
	RefundRequested   bool   `json:"refund_requested"`
	Version           int    `json:"version" gorm:"not null"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
	PaidAt            int64  `json:"paid_at"`
	CompletedAt       int64  `json:"completed_at"`
	ExpiresAt         int64  `json:"expires_at"`
}

// 动作记录与订单状态同事务保存，
// 不保存完整账户资料。
// 普通用户不读取内部审计；
// 处理说明在订单摘要中独立提供。
// 以版本和动作作为唯一约束，
// 重复操作不能追加重复办理记录。
type AccountRechargeEvent struct {
	ID        int    `json:"id"`
	OrderID   int    `json:"order_id" gorm:"index;uniqueIndex:idx_recharge_event,priority:1"`
	Version   int    `json:"version" gorm:"uniqueIndex:idx_recharge_event,priority:2"`
	Action    string `json:"action" gorm:"size:32;uniqueIndex:idx_recharge_event,priority:3"`
	ActorID   int    `json:"actor_id"`
	Note      string `json:"note" gorm:"size:1000"`
	CreatedAt int64  `json:"created_at"`
}

var (
	ErrRechargeInvalid     = errors.New("Invalid recharge request")
	ErrRechargeConflict    = errors.New("Recharge order changed. Refresh and try again.")
	ErrRechargeForbidden   = errors.New("You cannot process this recharge order")
	ErrRechargeUnavailable = errors.New("Recharge product or payment method is unavailable")
	ErrRechargeBalance     = errors.New("Insufficient wallet balance")
	ErrRechargeCache       = errors.New("Wallet synchronization is pending. Check this order before retrying.")
)

// 校验价格和说明的数据库边界；
// 所有保存入口共用同一规则。
// 首期定价上限为十万元，
// 禁止零价商品跳过支付流程。
// 开关显式传入，
// 不使用数据库布尔默认覆盖管理员选择。
// 新商品使用数据库主键，
// 浏览器不能自定义新记录编号。
// 已有商品必须带当前版本，
// 竞争修改失败返回冲突而非覆盖。
// 上下架不删除商品，
// 已付款订单无需依赖商品继续存在。
// 价格上限用于限制输入边界，
// 不表达支付渠道的运营限额。
// 此函数只维护销售配置，
// 不触发订单履约或收款。
func SaveAccountRechargeProduct(p *AccountRechargeProduct) error {
	p.Title = strings.TrimSpace(p.Title)
	p.Description = strings.TrimSpace(p.Description)
	// 展示字段在入口统一规范化；订单只复制验证后的快照。
	// 按字符限制长度，避免中文被按字节过早拒绝。
	// 空值允许清除，不从描述猜测或拆分旧数据。
	for _, field := range []struct {
		value *string
		limit int
	}{
		{&p.Summary, 500}, {&p.Provider, 80}, {&p.Period, 80},
		{&p.DeliveryTime, 160}, {&p.Icon, 128},
	} {
		*field.value = strings.TrimSpace(*field.value)
		if utf8.RuneCountInString(*field.value) > field.limit {
			return ErrRechargeInvalid
		}
	}
	if p.Title == "" || utf8.RuneCountInString(p.Title) > 128 || utf8.RuneCountInString(p.Description) > 4000 || p.PriceCents < 1 || p.PriceCents > 10000000 {
		return ErrRechargeInvalid
	}
	now := common.GetTimestamp()
	if p.ID == 0 {
		p.Version = 1
		p.CreatedAt = now
		p.UpdatedAt = now
		return DB.Create(p).Error
	}
	// 版本条件防止多人编辑覆盖最新价格；
	// 包含布尔零值的map才能下架。
	result := DB.Model(&AccountRechargeProduct{}).Where("id = ? AND version = ?", p.ID, p.Version).Updates(map[string]any{
		"summary": p.Summary, "provider": p.Provider, "period": p.Period, "delivery_time": p.DeliveryTime, "icon": p.Icon,
		"title": p.Title, "description": p.Description, "price_cents": p.PriceCents, "enabled": p.Enabled,
		"allow_wallet_payment": p.AllowWalletPayment, "sort_order": p.SortOrder, "version": p.Version + 1, "updated_at": now,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrRechargeConflict
	}
	p.Version++
	p.UpdatedAt = now
	return nil
}

// 列表永远分页，
// 管理员可以读取下架商品供历史维护。
// 排序字段固定，
// 不能由用户拼接SQL表达式。
func ListAccountRechargeProducts(admin bool) ([]AccountRechargeProduct, error) {
	items := []AccountRechargeProduct{}
	q := DB.Order("sort_order DESC, id DESC")
	if !admin {
		q = q.Where("enabled = ?", true)
	}
	err := q.Limit(200).Find(&items).Error
	return items, err
}

// 查询返回相同的不存在错误，
// 普通用户不能枚举他人订单。
// 内部调用传零用户仅能在已验证运营能力的处理器中使用。
func GetAccountRechargeOrder(id, userID int) (*AccountRechargeOrder, error) {
	var order AccountRechargeOrder
	q := DB.Where("id = ?", id)
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	err := q.First(&order).Error
	return &order, err
}

// 参数均经占位符绑定；
// 普通用户范围由服务端会话指定。
// mine只是运营列表筛选，
// 不是后端操作授权依据。
// 业务员的本人筛选只缩小列表，
// 不替代后续订单归属检查。
// 关键词采用完整交易号匹配，
// 不能搜索完整邮箱或 JSON。
// 页码与每页数量由入口限制，
// 查询不接受任意排序表达式。
// 列表只提供脱敏字段，
// 敏感资料需要另一个明确动作。
// 总数来自同样的条件，
// 不能把其他用户的订单数泄漏给买家。
func ListAccountRechargeOrders(userID, assignee, page, size int, status, keyword string) ([]AccountRechargeOrder, int64, error) {
	items := []AccountRechargeOrder{}
	var total int64
	q := DB.Model(&AccountRechargeOrder{})
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	if assignee > 0 {
		q = q.Where("assignee_id = ?", assignee)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if keyword != "" {
		q = q.Where("trade_no = ?", keyword)
	}
	if err := q.Count(&total).Error; err != nil {
		return items, 0, err
	}
	// 待领取队列先显示最早交资料的订单，
	// 避免新订单持续挤压旧待办。
	orderBy := "created_at DESC, id DESC"
	if userID == 0 && status == "pending" {
		orderBy = "info_submitted_at ASC, id ASC"
	}
	err := q.Order(orderBy).Offset((page - 1) * size).Limit(size).Find(&items).Error
	return items, total, err
}

// 状态和事件必须同事务；
// 失败时订单更新也回滚。
// 记录仅含人工说明，
// 不把邮箱和session传给此函数。
func saveRechargeEvent(tx *gorm.DB, o *AccountRechargeOrder, actor int, action, note string) error {
	o.Version++
	o.UpdatedAt = common.GetTimestamp()
	if err := tx.Save(o).Error; err != nil {
		return err
	}
	return tx.Create(&AccountRechargeEvent{OrderID: o.ID, Version: o.Version, ActorID: actor, Action: action, Note: note, CreatedAt: o.UpdatedAt}).Error
}

// 首次资料提交后进入待领取；
// 补交后仍由原业务员负责。
// 状态检查在行锁内完成，
// 重复提交不会把已完成订单重新入队。
// 此层只接收密文，
// 数据库SQL日志不接触认证资料原文。
// 订单归属和版本在同一行锁中验证，不能接受其他用户的版本号。
// 通知仅包含业务状态，接收方需要通过原授权接口读取处理资料。
// 保留已领取订单的处理人，补交不会重新成为公共待领取订单。
// 业务流水与推送事件共用事务，任一写入失败都允许原版本重试。
// 服务暂停投递时仍保存事件，稍后恢复不需要用户重新提交资料。
// 状态先保存再引用递增版本，事件身份与最终订单事实保持一致。
func SubmitAccountRechargeInfo(id, userID, version int, secret, mask string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var o AccountRechargeOrder
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", id, userID).First(&o).Error; err != nil {
			return err
		}
		if o.Version != version || o.PaymentStatus != "paid" || (o.Status != "awaiting_info" && o.Status != "action_required") {
			return ErrRechargeConflict
		}
		// 在覆盖状态前区分首次交资料与要求补交，不把原文放进事件。
		submission := "initial"
		if o.Status == "action_required" {
			submission = "resubmitted"
		}
		o.Secret = secret
		o.EmailMask = mask
		o.InfoSubmittedAt = common.GetTimestamp()
		o.PublicNote = ""
		o.Status = "pending"
		if o.AssigneeID > 0 {
			o.Status = "processing"
		}
		if err := saveRechargeEvent(tx, &o, userID, "submit_info", ""); err != nil {
			return err
		}
		// 订单版本由既有业务函数递增，通知引用同一已保存版本。
		// 事件失败必须与资料保存一并回滚，不能只记日志继续成功。
		if HubCaptureEnabled() {
			return AppendHubEventTx(tx, fmt.Sprintf("recharge:%d:info:%d", o.ID, o.Version), HubTopicRecharge, userID, map[string]any{
				"order_id": o.ID, "order_version": o.Version, "status": o.Status,
				"submission": submission, "assignee_id": o.AssigneeID,
			})
		}
		return nil
	})
}

// 业务动作只允许显式状态迁移，
// 不能接受任意订单字段更新。
// 所有者验证位于事务内，
// 旧页面与转派并发时不能继续办理。
// 管理员同样不能改付款金额或把未付款订单标记完成。
// 领取只允许已付款且已提交资料的未分配订单。
// 领取与转派都取得数据库行锁，
// 双人同时操作仅能有一方成功。
// 转派对象必须是启用的业务员或管理员，
// 不接收任意账号编号。
// 已完成及已取消订单不重新入队，
// 避免重复外部充值。
// 要求补交会立即停止旧凭据读取，
// 等待原购买者提供新资料。
// 重新提交保留原处理人，
// 不制造第二次领取机会。
// 内部备注不改变用户可见说明，
// 处理结果则明确对用户展示。
// 退款申请只标记需要审核，
// 不能修改支付状态或返还资金。
// 退款被拒绝后解除完成限制，
// 保留原处理人继续核实。
// 具体退款能力由控制器独立校验，
// 管理员身份不是资金接口替代。
// 本函数不接受实际收款金额，
// 也不允许标记待付订单完成。
func ActAccountRechargeOrder(id, actor, version int, admin bool, action, note string, assignee int) error {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 1000 {
		return ErrRechargeInvalid
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var o AccountRechargeOrder
		if err := lockForUpdate(tx).First(&o, id).Error; err != nil {
			return err
		}
		if o.Version != version || o.PaymentStatus != "paid" {
			return ErrRechargeConflict
		}
		if o.Status == "completed" || o.Status == "cancelled" || o.Status == "awaiting_info" {
			return ErrRechargeConflict
		}
		// 领取和转派只改变处理人，
		// 外部办理进度必须由人员交接。
		switch action {
		case "claim":
			if o.Status != "pending" || o.AssigneeID != 0 {
				return ErrRechargeConflict
			}
			o.AssigneeID = actor
			o.Status = "processing"
		case "assign":
			if !admin {
				return ErrRechargeForbidden
			}
			if note == "" || assignee <= 0 {
				return ErrRechargeInvalid
			}
			var target User
			if err := tx.Where("id = ? AND status = ?", assignee, common.UserStatusEnabled).First(&target).Error; err != nil {
				return ErrRechargeInvalid
			}
			if target.Role != common.RoleSalesUser && target.Role < common.RoleAdminUser {
				return ErrRechargeInvalid
			}
			o.AssigneeID = assignee
			if o.Status == "pending" {
				o.Status = "processing"
			}
		default:
			if (!admin && o.AssigneeID != actor) || o.AssigneeID == 0 {
				return ErrRechargeForbidden
			}
			// 补资料期间不能完成，
			// 必须由用户重新交付可办理资料。
			if o.Status != "processing" && o.Status != "exception" {
				return ErrRechargeConflict
			}
			if note == "" {
				return ErrRechargeInvalid
			}
			switch action {
			case "need_info":
				o.Status = "action_required"
				o.Secret = ""
				o.PublicNote = note
			case "complete":
				if o.Secret == "" || o.RefundRequested {
					return ErrRechargeConflict
				}
				o.Status = "completed"
				o.CompletedAt = common.GetTimestamp()
				o.Secret = ""
				o.PublicNote = note
			case "exception":
				o.Status = "exception"
				o.PublicNote = note
			case "reject_refund":
				if !admin || !o.RefundRequested {
					return ErrRechargeForbidden
				}
				o.RefundRequested = false
				o.PublicNote = note
			case "request_refund":
				o.RefundRequested = true
				o.Status = "exception"
				o.PublicNote = note
			case "note":
				o.InternalNote = note
			default:
				return ErrRechargeInvalid
			}
		}
		return saveRechargeEvent(tx, &o, actor, action, note)
	})
}

// 敏感读取必须在取密文时再次核对状态、处理人和版本。
// 审计写入失败就不返回密文，
// 读取动作不能成为无记录的旁路。
// 读取推进版本并返回新摘要，
// 旧页面须刷新后才能继续办理。
// 行锁内检查避免验证结束后订单已被转派却继续读取。
// 支付已进入退款流程时，
// 即使旧处理人仍保留编号也拒绝读取。
// 管理员查看也需要留下逐次事件，
// 不能通过列表批量下载。
// 读取事件只记录动作和操作者，
// 不接受凭据作为说明。
// 返回密文交由服务层解密，
// 模型层从不接触完整 JSON。
// 调用者收到版本变化后应刷新详情，
// 不能重用旧版本办理。
// 解密失败不会回退到旧凭据或伪造一份空账户资料。
func ReadAccountRechargeSecret(id, actor, version int, admin bool) (string, error) {
	var secret string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var o AccountRechargeOrder
		if err := lockForUpdate(tx).First(&o, id).Error; err != nil {
			return err
		}
		if o.PaymentStatus != "paid" || o.Version != version || o.Secret == "" || (o.Status != "processing" && o.Status != "exception") {
			return ErrRechargeConflict
		}
		if !admin && o.AssigneeID != actor {
			return ErrRechargeForbidden
		}
		// 每次揭示分配新事件版本，
		// 避免把两次真实敏感读取合并。
		o.Version++
		o.UpdatedAt = common.GetTimestamp()
		if err := tx.Save(&o).Error; err != nil {
			return err
		}
		if err := tx.Create(&AccountRechargeEvent{OrderID: id, Version: o.Version, ActorID: actor, Action: "reveal", CreatedAt: o.UpdatedAt}).Error; err != nil {
			return err
		}
		secret = o.Secret
		return nil
	})
	return secret, err
}
