package model

import (
	"errors"
	"math"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InvoiceProfile 是一个用户当前使用的抬头，不与登录资料或公开联系方式混用。
// UserId同时为主键，数据库保证每个用户只有一份当前配置。
// 下单会复制完整快照，修改或删除抬头不能影响已付款的申请。
type InvoiceProfile struct {
	UserId      int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"` // 用户所有者来自会话，不允许在JSON中改成他人。
	TitleType   string `json:"title_type" gorm:"size:16;not null"`            // 企业与个人采用不同必填税号规则。
	Title       string `json:"title" gorm:"size:128;not null"`                // 完整发票抬头，不使用账户显示名自动补齐。
	TaxId       string `json:"tax_id" gorm:"size:32"`                         // 税务标识属于私有资料，禁止进入公开状态。
	Email       string `json:"email" gorm:"size:254;not null"`                // 用于发票交付联系，不同步改登录邮箱。
	Address     string `json:"address" gorm:"size:255"`                       // 开票地址可选，不影响已有用户地址体系。
	Phone       string `json:"phone" gorm:"size:32"`                          // 只用于发票联系，不作为认证手机号。
	BankName    string `json:"bank_name" gorm:"size:128"`                     // 可选开户行，不与支付收款商户配置混用。
	BankAccount string `json:"bank_account" gorm:"size:64"`                   // 可选开票银行账号，接口受本人范围保护。
	UpdatedAt   int64  `json:"updated_at" gorm:"autoUpdateTime"`              // 仅记录当前配置更新时间，历史申请另有快照。
}

// InvoiceRequest 记录下单快照与人工开票状态，业务订单号是唯一申请身份。
// ProfileJSON是持久快照，Profile只用于受保护API序列化，不另写第二份数据。
// FilePath不进入任何JSON，附件只能通过归属或财务权限校验后的接口下载。
// PaidAt由真实支付完成事务设置，浏览器和财务状态更新均不能伪造付款。
type InvoiceRequest struct {
	Id            int            `json:"id"`                                             // 管理列表与受保护下载使用的稳定申请标识。
	UserId        int            `json:"user_id" gorm:"index;not null"`                  // 申请始终归属付款账户，不能随抬头编辑转移。
	TradeNo       string         `json:"trade_no" gorm:"size:255;uniqueIndex;not null"`  // 每笔付款最多一份申请，数据库唯一约束防重复。
	BusinessType  string         `json:"business_type" gorm:"size:16;not null"`          // 区分钱包充值和订阅，不把两种权益混为余额。
	ProfileJSON   string         `json:"-" gorm:"type:text;not null"`                    // 收费时保存的完整抬头，之后个人资料修改不回写。
	Profile       InvoiceProfile `json:"profile" gorm:"-"`                               // 只还原已验证快照供API展示，不建立关联更新。
	BaseAmount    float64        `json:"base_amount" gorm:"type:decimal(18,2);not null"` // 优惠后的原实付金额，不是平台到账额度。
	FeeRate       float64        `json:"fee_rate" gorm:"type:decimal(10,4);not null"`    // 百分数使用六表示百分之六，持久化所选费率。
	FeeAmount     float64        `json:"fee_amount" gorm:"type:decimal(18,2);not null"`  // 附加费单独保存，不能加入用户平台余额。
	PayAmount     float64        `json:"pay_amount" gorm:"type:decimal(18,2);not null"`  // 订单与网关匹配使用的最终总收款金额。
	Currency      string         `json:"currency" gorm:"size:8;not null"`                // 当前仅人民币，本地支付之外明确拒绝。
	Status        string         `json:"status" gorm:"size:32;index;not null"`           // 待付款与待开票分开，避免财务处理未收款申请。
	InvoiceNumber string         `json:"invoice_number" gorm:"size:64"`                  // 由财务开具后填写，用户不能提交伪发票号。
	Remark        string         `json:"remark" gorm:"size:1000"`                        // 人工处理说明，拒绝时必须解释原因。
	FilePath      string         `json:"-" gorm:"size:255"`                              // 私有文件路径从不返回浏览器或公开静态目录。
	FileMime      string         `json:"-" gorm:"size:64"`                               // 由文件内容识别，下载时不信任原文件名。
	HasFile       bool           `json:"has_file" gorm:"-"`                              // 展示标记由私有文件引用派生，不接受客户端写入。
	PaidAt        int64          `json:"-"`                                              // 付款事务唯一写入，财务状态不能伪造成功付款。
	CreatedAt     int64          `json:"created_at" gorm:"autoCreateTime"`               // 保留下单申请时刻，不在支付重试时复用旧身份。
	UpdatedAt     int64          `json:"updated_at" gorm:"autoUpdateTime"`               // 处理更新由数据库写入流程维护。
	IssuedAt      int64          `json:"issued_at"`                                      // 只在附件和发票号码一起完成时记录。
}

// NormalizeInvoiceProfile是配置入口唯一校验，内部快照遵从此契约。
// 企业抬头必须填写税号；个人不强制税号，邮箱用于发票交付联系。
// 只接受纯邮箱地址，不接受带显示名的地址，防止存储与表单含义不同。
// 地址、电话和银行信息都是可选的普通发票资料，不作为授权依据。
// 入口截去空白并统一税号大小写，快照与页面保存结果保持一致。
// 长度以数据库能保存的约束校验，避免下单才遇到字段截断。
func NormalizeInvoiceProfile(profile *InvoiceProfile) error {
	profile.TitleType, profile.Title = strings.TrimSpace(profile.TitleType), strings.TrimSpace(profile.Title)
	profile.TaxId, profile.Email = strings.ToUpper(strings.TrimSpace(profile.TaxId)), strings.TrimSpace(profile.Email)
	profile.Address, profile.Phone = strings.TrimSpace(profile.Address), strings.TrimSpace(profile.Phone)
	profile.BankName, profile.BankAccount = strings.TrimSpace(profile.BankName), strings.TrimSpace(profile.BankAccount)
	if profile.TitleType != "business" && profile.TitleType != "personal" {
		return common.NewMessage("Invoice title type must be business or personal")
	}
	if utf8.RuneCountInString(profile.Title) < 2 || utf8.RuneCountInString(profile.Title) > 128 {
		return common.NewMessage("Invoice title must contain 2 to 128 characters")
	}
	if (profile.TitleType == "business" && profile.TaxId == "") || len(profile.TaxId) > 32 {
		return common.NewMessage("A valid taxpayer identification number is required for business invoices")
	}
	address, err := mail.ParseAddress(profile.Email)
	if err != nil || address.Address != profile.Email || len(profile.Email) > 254 {
		return common.NewMessage("A valid invoice delivery email is required")
	}
	if utf8.RuneCountInString(profile.Address) > 255 || len(profile.Phone) > 32 || utf8.RuneCountInString(profile.BankName) > 128 || len(profile.BankAccount) > 64 {
		return common.NewMessage("Invoice contact or bank details exceed the allowed length")
	}
	return nil
}

// GetInvoiceProfile只按认证用户查找，缺失是一种明确的未配置状态。
// 不用其他用户或站点默认抬头补齐，避免开票给错误主体。
func GetInvoiceProfile(userID int) (*InvoiceProfile, error) {
	var profile InvoiceProfile
	if err := DB.Where("user_id = ?", userID).First(&profile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &profile, nil
}

// SaveInvoiceProfile在边界校验后按用户主键更新，客户端不能覆盖归属。
// 邮箱和税号不写入操作日志，既有认证资料不受此配置影响。
func SaveInvoiceProfile(userID int, profile InvoiceProfile) (*InvoiceProfile, error) {
	profile.UserId = userID
	if err := NormalizeInvoiceProfile(&profile); err != nil {
		return nil, err
	}
	profile.UpdatedAt = common.GetTimestamp()
	err := DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"title_type", "title", "tax_id", "email", "address", "phone", "bank_name", "bank_account", "updated_at"})}).Create(&profile).Error
	return &profile, err
}

// NewInvoiceRequest为报价和下单共用的可信计算入口。
// 费基是原优惠后的应付金额；附加费用四舍五入保留到分，平台额度不变。
// 只支持人民币本地支付，不能静默把外币金额当成人民币开票。
func NewInvoiceRequest(userID int, group, businessType string, base float64, required bool) (*InvoiceRequest, float64, error) {
	if !required {
		return nil, base, nil
	}
	if !operation_setting.IsCNYDisplay() {
		return nil, 0, common.NewMessage("Invoices are supported only for CNY local payment methods")
	}
	if math.IsNaN(base) || math.IsInf(base, 0) || base <= 0 || base > 9999999999999 {
		return nil, 0, common.NewMessage("Invalid invoice payment amount")
	}
	profile, err := GetInvoiceProfile(userID)
	if err != nil {
		return nil, 0, err
	}
	if profile == nil { // 没有配置必须让用户先补齐，不能自动采用管理员抬头。
		return nil, 0, common.NewMessage("Please configure your invoice title first")
	}
	rate, err := operation_setting.InvoiceRate(group) // 分组由服务端用户记录取得，客户端不能用低费率组冒充。
	if err != nil {
		return nil, 0, err
	}
	data, err := common.Marshal(profile) // 下单资料一次序列化，后续只读取此冻结内容。
	if err != nil {
		return nil, 0, err
	}
	baseMoney := decimal.NewFromFloat(base).Round(2) // 费用基础先落到分，和发送网关的精度一致。
	if baseMoney.LessThan(decimal.RequireFromString("0.01")) {
		return nil, 0, common.NewMessage("Invalid invoice payment amount")
	}
	fee := baseMoney.Mul(decimal.NewFromFloat(rate)).Div(decimal.NewFromInt(100)).Round(2)
	gross := baseMoney.Add(fee) // 总价只有这一个实现，报价和付款不各算一遍。
	invoice := &InvoiceRequest{UserId: userID, BusinessType: businessType, ProfileJSON: string(data), Profile: *profile, BaseAmount: baseMoney.InexactFloat64(), FeeRate: rate, FeeAmount: fee.InexactFloat64(), PayAmount: gross.InexactFloat64(), Currency: "CNY", Status: "pending_payment"}
	return invoice, invoice.PayAmount, nil
}

// CreateInvoiceForOrderTx只在订单创建的同一个事务中调用。
// 失败使订单也回滚，不保留收费了却没有申请快照的半份状态。
func CreateInvoiceForOrderTx(tx *gorm.DB, invoice *InvoiceRequest, tradeNo string) error {
	if invoice == nil {
		return nil
	}
	invoice.TradeNo = tradeNo // 订单号由服务端创建，申请不能关联用户提交的他人交易。
	return tx.Create(invoice).Error
}

// MarkInvoicePaidTx由真实到账事务调用，不从用户返回页面推断付款。
// 原状态限制保证重复回调不会把已开票申请改回待处理。
func MarkInvoicePaidTx(tx *gorm.DB, tradeNo string) error {
	result := tx.Model(&InvoiceRequest{}).Where("trade_no = ? AND status = ?", tradeNo, "pending_payment").Updates(map[string]any{"status": "pending", "paid_at": common.GetTimestamp()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 { // 收取了开票费但缺少申请时回滚到账，不允许静默丢单。
		return common.NewMessage("Invoice request is missing for this paid order")
	}
	return nil
}

// InvoiceRecords分页查询只有调用方明确传入0时才查看全部用户。
// 本人接口始终传认证ID；关键词只匹配订单号和发票号，不搜索私密抬头。
func InvoiceRecords(userID, page, size int, status, keyword string) ([]InvoiceRequest, int64, error) {
	query := DB.Model(&InvoiceRequest{}) // 管理与本人列表共用同一状态和金额来源。
	if userID > 0 {
		query = query.Where("user_id = ?", userID) // 用户范围来自会话，不接受请求覆盖。
	}
	if status != "" {
		query = query.Where("status = ?", status) // 状态已在HTTP入口限制为已知集合。
	}
	if keyword != "" {
		query = query.Where("trade_no LIKE ? OR invoice_number LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	var count int64
	if err := query.Count(&count).Error; err != nil { // 统计失败不伪装为空列表。
		return nil, 0, err
	}
	var records []InvoiceRequest
	if err := query.Order("id desc").Offset((page - 1) * size).Limit(size).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	for i := range records {
		if err := records[i].Expand(); err != nil {
			return nil, 0, err
		}
	}
	return records, count, nil
}

// Expand仅还原已经验证过的抬头快照，坏数据必须可见而非返回空抬头。
// 附件存在性按持久记录派生，不暴露本地存储路径。
func (invoice *InvoiceRequest) Expand() error {
	invoice.HasFile = invoice.FilePath != ""
	return common.UnmarshalJsonStr(invoice.ProfileJSON, &invoice.Profile)
}

// InvoiceByID对用户下载必须包含所有权约束；财务调用显式传0。
// 查不到和不属于本人使用相同错误，避免枚举别人的发票记录。
func InvoiceByID(id, userID int) (*InvoiceRequest, error) {
	var invoice InvoiceRequest
	query := DB.Where("id = ?", id)
	if userID > 0 {
		query = query.Where("user_id = ?", userID) // 用户范围来自会话，不接受请求覆盖。
	}
	if err := query.First(&invoice).Error; err != nil {
		return nil, err
	}
	if err := invoice.Expand(); err != nil {
		return nil, err
	}
	return &invoice, nil // 返回受范围约束的快照，下载不能绕过同一查询。
}

// InvoiceProcessing将财务动作与订单快照分开，不开放金额或抬头修改。
// 文件引用只由上传控制器构造，外部JSON不会绑定到这些内部字段。
// 附件上传与文字处理复用此输入，更新时均在同一申请行锁下校验。
// 已开具与已拒绝保持终态，退款或重开需要新的明确流程。
type InvoiceProcessing struct {
	Status        string
	InvoiceNumber string
	Remark        string
	FilePath      string
	FileMime      string
}

// UpdateInvoiceRequest只接受财务处理状态，不能将申请改成已支付。
// 税号、抬头和收费均来自原订单快照，不允许开票页面偷偷修改。
// 发票已开具或已拒绝后保持终态，重开和退款不在当前人工流程范围。
func UpdateInvoiceRequest(id int, update InvoiceProcessing) (*InvoiceRequest, error) {
	status, number, remark, filePath, mimeType := update.Status, update.InvoiceNumber, update.Remark, update.FilePath, update.FileMime
	status, number, remark = strings.TrimSpace(status), strings.TrimSpace(number), strings.TrimSpace(remark)
	if status != "processing" && status != "issued" && status != "rejected" {
		return nil, common.NewMessage("Invalid invoice status")
	}
	if len(number) > 64 || utf8.RuneCountInString(remark) > 1000 {
		return nil, common.NewMessage("Invoice number or remark exceeds the allowed length")
	}
	if status == "issued" && number == "" { // 不能用上传一个文件替代完整发票身份。
		return nil, common.NewMessage("Invoice number is required")
	}
	if status == "rejected" && remark == "" { // 拒绝不会自动退款，用户需要可见的处理说明。
		return nil, common.NewMessage("Please provide an invoice rejection reason")
	}
	var invoice InvoiceRequest
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ?", id).First(&invoice).Error; err != nil {
			return err
		}
		if invoice.PaidAt == 0 || invoice.Status == "pending_payment" { // 状态标签和真实付款标记必须同时成立。
			return common.NewMessage("Only paid orders can be invoiced")
		}
		if status == "issued" && filePath == "" && invoice.FilePath == "" { // 发票必须确有可下载附件，不能只改个标签冒充完成。
			return common.NewMessage("Please upload the invoice file before marking it issued")
		}
		if invoice.Status == "issued" || invoice.Status == "rejected" { // 并发处理的一方成功后，另一方明确失败。
			return common.NewMessage("This invoice request has already been processed")
		}
		updates := map[string]any{"status": status, "remark": remark, "invoice_number": number, "updated_at": common.GetTimestamp()}
		if status == "issued" {
			updates["issued_at"] = common.GetTimestamp()
			if filePath != "" {
				updates["file_path"], updates["file_mime"] = filePath, mimeType
			}
		}
		if err := tx.Model(&invoice).Updates(updates).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return InvoiceByID(invoice.Id, 0) // 重新读数据库，API返回实际持久状态而非预期草稿。
}
