package controller

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// 只公开稳定错误，
// 不输出 SQL、会话JSON或支付商户配置。
// 未知故障保留 500，
// 不能用空数据把操作伪装为成功。
// 不存在与越权查找使用相同响应，
// 避免通过编号枚举其他买家的订单。
// 冲突表示当前版本已经变化，
// 客户端应刷新而非盲目重放。
// 余额不足与缓存同步待恢复属于不同错误，
// 不能混成付款失败。
// 数据库驱动错误不直接发送浏览器，
// 防止连接信息与 SQL内容泄漏。
// 此函数只写响应，
// 调用者必须立即返回，
// 不能继续执行资金动作。
func rechargeError(c *gin.Context, err error) {
	code, message := http.StatusInternalServerError, "Recharge request failed"
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		code, message = 404, "Recharge order or product not found"
	case errors.Is(err, model.ErrCreditConflict), errors.Is(err, model.ErrRechargeConflict):
		code, message = 409, err.Error()
	case errors.Is(err, model.ErrRechargeForbidden):
		code, message = 403, err.Error()
	case errors.Is(err, model.ErrCreditInvalid), errors.Is(err, model.ErrCreditUnavailable), errors.Is(err, model.ErrCreditLimit), errors.Is(err, model.ErrRechargeInvalid), errors.Is(err, model.ErrRechargeUnavailable), errors.Is(err, model.ErrRechargeBalance):
		code, message = 400, err.Error()
	case errors.Is(err, model.ErrRechargeCache):
		code, message = 503, err.Error()
	}
	c.JSON(code, gin.H{"success": false, "message": message})
}

// 限制凭据正文大小；
// 解析失败只返回固定文案，
// 不回显字段值。
// 解析限制在读取前设置，
// 巨型 JSON不应先被完整加载到内存。
// 资料正文比普通操作大，
// 但仍限制为固定的小尺寸。
// 不使用绑定错误原文，
// 因为它可能包含用户提交的凭据片段。
// 成功只表示传输结构可解码，
// 归属和业务状态由各自处理器验证。
// 未知字段不能成为任意订单更新通道，
// 后续只使用白名单结构。
func rechargeBody(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 96*1024)
	if c.ShouldBindJSON(target) != nil {
		rechargeError(c, model.ErrRechargeInvalid)
		return false
	}
	return true
}

// 首期所有代充值接口限网页登录，
// PAT不能获取人工办理资料。
// 此检查位于鉴权之后，
// 真实会话状态由既有认证链路验证。
// UserAuth或更高门槛中间件必须先于此函数运行。
// 当前角色、账号启用状态和会话版本由统一认证链路实时读取。
// 拒绝个人访问令牌，
// 即使令牌具有历史管理员能力也不能访问。
// 客户端 Cookie 或Bearer字符串本身不等同于已经验证的会话。
// 此处不收集会话正文，
// 不把凭据写入审计或错误提示。
// 所有响应都设置no-store，
// 包括失败响应和普通订单摘要。
func RechargeSessionOnly(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, ok := middleware.GetSessionAuthIdentity(c); !ok {
		c.AbortWithStatusJSON(403, gin.H{"success": false, "message": "Please sign in to continue"})
		return
	}
	c.Next()
}

// 支付渠道仅投影公开名称与标识，
// 任何配置密钥都不返回前端。
// 商品接口告知部署是否就绪；
// 未配置时界面仍可显示商品和已有订单。
// 路由模板决定是否为管理员列表，
// 客户端查询参数不能切换范围。
// 下架商品只在已保护的管理员路径返回，
// 买家不能创建新购买。
// 可用渠道来自既有配置校验，
// 不在此接口复制密钥或 SDK设置。
// 微信网关与官方微信允许并存，
// 通过不同前缀标识实际选择。
// 平台余额能力属于商品字段，
// 不代表全站必须支持余额支付。
// 缺少资料加密配置时仍可管理商品，
// 但不开放新的购买。
// 付款合规配置继续沿用现有平台规则，
// 不由代充值创建另一套开关。
func GetRechargeProducts(c *gin.Context) {
	admin := strings.Contains(c.FullPath(), "/admin/")
	products, err := model.ListAccountRechargeProducts(admin)
	if err != nil {
		rechargeError(c, err)
		return
	}
	// 目录按登录用户返回挂帐方式，未授权不暴露入口；读取失败不能当成未配置。
	credit, err := model.GetCreditAccount(c.GetInt("id"))
	if err != nil {
		rechargeError(c, err)
		return
	}
	methods := rechargePaymentMethods()
	if operation_setting.IsPaymentComplianceConfirmed() && model.CheckCreditAvailable(credit, model.CreditModuleRecharge, 0) == nil {
		methods = append(methods, gin.H{"id": "credit", "name": "On account"})
	}
	common.ApiSuccess(c, gin.H{
		"credit": credit,
		"items":  products, "payment_methods": methods,
		"enabled":    operation_setting.GetRechargeSetting().Enabled,
		"configured": operation_setting.GetRechargeSetting().Enabled && service.RechargeConfigured() && operation_setting.IsPaymentComplianceConfirmed(),
	})
}

// 商品目录和设置诊断共用微信渠道判断，避免后台显示就绪却无法选购。
// 返回的只有公开渠道编号与名称，不包含商户密钥。
// 微信网关和官方渠道可以同时返回，由买家在结账时选择。
// 渠道列表允许为空，不把余额方式伪装成一个微信渠道。
// 逐商品余额开关另在报价时校验，这里不改变商品配置。
// 支付确认未完成时没有可用渠道，余额购买也受原确认门禁保护。
func rechargePaymentMethods() []gin.H {
	methods := []gin.H{}
	if operation_setting.IsPaymentComplianceConfirmed() {
		if GetEpayClient() != nil && operation_setting.ContainsPayMethod("wxpay") {
			methods = append(methods, gin.H{"id": "epay:wxpay", "name": "WeChat Pay"})
		}
		for _, channel := range operation_setting.GetAvailableOfficialPaymentChannels() {
			if channel.ChannelType == operation_setting.PaymentChannelWechat {
				methods = append(methods, gin.H{"id": "official:" + channel.ID, "name": channel.Name})
			}
		}
	}
	return methods
}

// 新报价、新订单和未付订单继续付款采用相同门禁。
// 关闭开关不会删除订单，也不能阻止合法迟到回调或已付履约。
// 配置错误明确返回原因，不能只依赖前端禁用按钮。
// 开关优先返回暂停原因；管理员诊断仍单独显示环境缺失项。
// 密钥失效时也停止未付订单收款，避免产生无法收资料的新付款。
// 不将此检查挂到整个路由组，否则会阻断已付订单履约。
func requireRechargeSales(c *gin.Context) bool {
	if !operation_setting.GetRechargeSetting().Enabled {
		common.ApiErrorT(c, "Account top-ups are paused")
		return false
	}
	if !service.RechargeConfigured() {
		common.ApiErrorT(c, "Recharge encryption is not configured")
		return false
	}
	return requirePaymentCompliance(c)
}

// 商品更新只绑定允许配置的字段，
// 禁止接收订单或权限字段。
// 修改使用版本校验，
// 下架和余额开关的false都会真实保存。
// 商品创建和更新共用一个服务端校验入口。
// 数据库主键为零表示创建，
// 负数编号不能进入查询更新。
// 商品版本来自管理员打开时的快照，
// 不能默认覆盖最新编辑。
// 未上架草稿可保存，
// 后续由管理员明确启用。
// 接口不会改动既有订单的金额、说明或支付状态。
func SaveRechargeProduct(c *gin.Context) {
	var p model.AccountRechargeProduct
	if !rechargeBody(c, &p) {
		return
	}
	if p.ID < 0 {
		rechargeError(c, model.ErrRechargeInvalid)
		return
	}
	if err := model.SaveAccountRechargeProduct(&p); err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, p)
}

// 请求键冻结本次结账内容，
// 不包含任何充值账户凭据。
type rechargePurchase struct {
	ProductID       int    `json:"product_id"`
	ProductVersion  int    `json:"product_version"`
	PaymentMethod   string `json:"payment_method"`
	InvoiceRequired bool   `json:"invoice_required"`
	RequestID       string `json:"request_id"`
	QuotedCents     int64  `json:"quoted_cents"` // 用户确认的总价，报价后费率变化必须重新确认。
}

// 报价与创建共享开票及钱包换算合同；
// 真实扣款仍由订单事务校验。
// 汇率非法时明确拒绝，
// 不能回退到固定值或扣除零额度。
// 商品必须启用且版本一致，
// 避免过期卡片使用已撤回的价格。
// 支付方法必须明确属于余额、网关微信或官方微信，
// 不猜测回退。
// 钱包换算始终从人民币分转为内部额度，
// 不能直接把元数当quota。
// 汇率和单位必须能产生正的安全整数，
// 异常配置拒绝下单。
// 余额商品不允许重复开票，
// 避免同一钱包充值与消费重复收费开票。
// 官方渠道不仅核对存在，
// 还核对微信类型，
// 支付宝 ID不会被误认。
// 用户分组从当前用户记录获得，
// 客户端不能指定低费率组。
// 发票抬头和费率由原开票服务生成快照，
// 不能在此另写收费公式。
// 总价保留到人民币分，
// 创建时再核对用户确认过的分金额。
// 只返回订单草稿，
// 此处不会插入订单或扣除余额。
// 任何步骤出错都不返回部分可用报价，
// 以免界面误用旧数据。
func rechargeQuote(c *gin.Context, req rechargePurchase) (*model.AccountRechargeOrder, *model.InvoiceRequest, error) {
	var p model.AccountRechargeProduct
	if err := model.DB.First(&p, req.ProductID).Error; err != nil {
		return nil, nil, err
	}
	if !p.Enabled || p.Version != req.ProductVersion {
		return nil, nil, model.ErrRechargeUnavailable
	}
	o := &model.AccountRechargeOrder{UserID: c.GetInt("id"), ProductID: p.ID, Title: p.Title, Description: p.Description, BaseCents: p.PriceCents, MoneyCents: p.PriceCents, RequestID: req.RequestID, InvoiceRequired: req.InvoiceRequired}
	switch {
	case req.PaymentMethod == "credit":
		// 挂帐未收到现金，本期不进入现有付款开票链路。
		if req.InvoiceRequired {
			return nil, nil, model.ErrCreditInvalid
		}
		account, err := model.GetCreditAccount(o.UserID)
		if err != nil {
			return nil, nil, err
		}
		if err := model.CheckCreditAvailable(account, model.CreditModuleRecharge, p.PriceCents); err != nil {
			return nil, nil, err
		}
		o.PaymentProvider, o.PaymentMethod = "credit", "credit"
	case req.PaymentMethod == "wallet":
		if !p.AllowWalletPayment || req.InvoiceRequired {
			return nil, nil, model.ErrRechargeUnavailable
		}
		rate := operation_setting.USDExchangeRate
		if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			return nil, nil, model.ErrRechargeUnavailable
		}
		quota, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(p.PriceCents).Div(decimal.NewFromInt(100)).Div(decimal.NewFromFloat(rate)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).Floor())
		if err != nil || quota <= 0 {
			return nil, nil, model.ErrRechargeInvalid
		}
		o.PaymentProvider, o.PaymentMethod, o.WalletQuota, o.WalletRate = "wallet", "wallet", quota, decimal.NewFromFloat(rate).String()
	case req.PaymentMethod == "epay:wxpay":
		if GetEpayClient() == nil || !operation_setting.ContainsPayMethod("wxpay") {
			return nil, nil, model.ErrRechargeUnavailable
		}
		o.PaymentProvider, o.PaymentMethod = model.PaymentProviderEpay, "wxpay"
	case strings.HasPrefix(req.PaymentMethod, "official:"):
		channel, ok := operation_setting.GetOfficialPaymentChannel(strings.TrimPrefix(req.PaymentMethod, "official:"), true)
		if !ok || channel.ChannelType != operation_setting.PaymentChannelWechat {
			return nil, nil, model.ErrRechargeUnavailable
		}
		o.PaymentProvider, o.PaymentMethod = model.PaymentProviderWechat, channel.ID
	default:
		return nil, nil, model.ErrRechargeUnavailable
	}
	group, err := model.GetUserGroup(o.UserID, true)
	if err != nil {
		return nil, nil, err
	}
	invoice, money, err := model.NewInvoiceRequest(o.UserID, group, "recharge", float64(p.PriceCents)/100, req.InvoiceRequired)
	if err != nil {
		return nil, nil, err
	}
	o.MoneyCents = decimal.NewFromFloat(money).Mul(decimal.NewFromInt(100)).IntPart()
	return o, invoice, nil
}

// 用户确认展示的总价后再付款；
// 报价本身没有订单或资金副作用。
// 报价可在用户切换方式或开票选择时重新调用，
// 不产生资金副作用。
// 请求结构与创建相同，
// 避免两个入口对支付选择有不同解释。
// 返回基础金额和总额，
// 界面能够清晰区分开票附加费用。
// 不返回服务端密钥、完整渠道配置或其他用户抬头。
// 报价失败使用明确提示，
// 不能静默采用商品原价继续收款。
func QuoteRechargeOrder(c *gin.Context) {
	if !requireRechargeSales(c) {
		return
	}
	var req rechargePurchase
	if !rechargeBody(c, &req) {
		return
	}
	o, invoice, err := rechargeQuote(c, req)
	if err != nil {
		if errors.Is(err, model.ErrCreditUnavailable) || errors.Is(err, model.ErrCreditLimit) || errors.Is(err, model.ErrCreditInvalid) {
			rechargeError(c, err)
		} else {
			common.ApiErrorT(c, "Recharge quote is unavailable. Check the product, payment method and invoice profile.")
		}
		return
	}
	common.ApiSuccess(c, gin.H{"money_cents": o.MoneyCents, "base_cents": o.BaseCents, "wallet_quota": o.WalletQuota, "invoice": invoice})
}

// 创建订单和付款分两步：
// 创建成功后页面始终能回到原单继续。
// 请求超时使用同一 UUID查询/重试，
// 不重新生成第二笔付款。
// 部署预检先检查加密可用性，
// 避免收款后无法接收账户资料。
// 规范 UUID防止任意长字符串占用唯一索引或产生等价请求身份。
// 摘要覆盖用户确认的商品、版本、方式、开票选择和金额。
// 先恢复已有请求，
// 再读取当前商品，
// 历史订单不能因下架而消失。
// 找到相同请求但摘要不同必须拒绝，
// 不向新的选择返回旧订单。
// 随机订单号使用独立业务前缀，
// 支付回调据此前缀选择完成函数。
// 订单号长度遵循微信外部交易号约束，
// 不使用无限长用户编号拼接。
// 商品及发票快照由模型事务最终保存，
// 控制器预读不是唯一校验。
// 本接口只创建待付款订单，
// 不能让用户通过提交数据宣称付款成功。
// 实际支付入口由订单详情调用，
// 超时后也能从本人列表继续。
// 已创建订单不接受前端指定新的所有者或直接修改付款方式。
func CreateRechargeOrder(c *gin.Context) {
	var req rechargePurchase
	if !rechargeBody(c, &req) {
		return
	}
	key, err := uuid.Parse(req.RequestID)
	if err != nil || key.Version() != 4 || key.String() != req.RequestID {
		rechargeError(c, model.ErrRechargeInvalid)
		return
	}
	raw, err := common.Marshal(req)
	if err != nil {
		rechargeError(c, err)
		return
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	// 先恢复原单，
	// 商品改价或下架不能使既有已付订单丢失。
	var existing model.AccountRechargeOrder
	err = model.DB.Where("user_id = ? AND request_id = ?", c.GetInt("id"), req.RequestID).First(&existing).Error
	if err == nil {
		if existing.RequestHash != hash {
			rechargeError(c, model.ErrRechargeConflict)
			return
		}
		common.ApiSuccess(c, existing)
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		rechargeError(c, err)
		return
	}
	if !requireRechargeSales(c) {
		return
	}
	order, invoice, err := rechargeQuote(c, req)
	if err != nil {
		rechargeError(c, err)
		return
	}
	if req.QuotedCents != order.MoneyCents {
		rechargeError(c, model.ErrRechargeConflict)
		return
	}
	order.RequestHash, order.TradeNo = hash, "RCHUSR"+strings.ReplaceAll(uuid.NewString(), "-", "")[:26]
	order.ExpiresAt = common.GetTimestamp() + 1800
	order, err = model.CreateAccountRechargeOrder(order, req.ProductVersion, invoice)
	if err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, order)
}

// 普通订单详情只返回模型的安全字段，
// 支付入口通过独立 POST获取。
// 后台记录不包含密文；
// 非负责业务员只得到摘要，
// 不能浏览内部备注。
// 运营路径使用独立权限中间件，
// 数字角色不能替代动作能力。
// 普通用户始终传入会话ID，
// 只能读取自己的购买记录。
// 我的待办是过滤选项，
// 不是隐藏在列表后面的授权规则。
// 页码和每页数量做边界限制，
// 避免无界下载全部历史。
// 关键词原样作为绑定参数，
// 不拼接到SQL 条件或排序表达式。
// 返回总数与列表来自相同查询条件，
// 分页不泄露别人的订单数量。
// 普通用户不读取内部备注或业务事件，
// 即使是自己的购买订单。
// 业务员只有本人领取订单才查看内部记录，
// 队列其他订单仅给摘要。
// 管理员按原权限模型可查看全部事件，
// 但不自动读取账户凭据。
// 历史事件数量有上限，
// 防止单次详情响应无界增长。
// 模型的私密字段不会序列化，
// 详情中没有解密或密码回显路径。
// 读取详情不领取订单、不更新付款，
// 也不触发上游处理。
func GetRechargeOrders(c *gin.Context) {
	userID, assignee := c.GetInt("id"), 0
	if strings.Contains(c.FullPath(), "/operations/") {
		userID = 0
		if c.Query("mine") == "true" {
			assignee = c.GetInt("id")
		}
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	items, total, err := model.ListAccountRechargeOrders(userID, assignee, max(page, 1), min(max(size, 1), 100), c.Query("status"), c.Query("keyword"))
	if err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total})
}

// 路径 ID不是授权信息；
// 本人接口额外加入user_id查询约束。
// 所有需要单笔订单的处理器都采用这个编号解析和归属入口。
// 路径编号只表达用户希望查看的资源，
// 不能视为其已拥有权限。
// 内部运营读取必须由调用者明确选择，
// 并由路由能力保护。
// 本人订单不存在与不属于本人使用相同错误。
// 函数返回摘要对象，
// 后续资金或办理写入仍在事务中重新验证。
// 不能基于这里的预读结果直接绕过订单版本或处理人检查。
func rechargeOrder(c *gin.Context, operations bool) (*model.AccountRechargeOrder, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		rechargeError(c, gorm.ErrRecordNotFound)
		return nil, false
	}
	owner := c.GetInt("id")
	if operations {
		owner = 0
	}
	o, err := model.GetAccountRechargeOrder(id, owner)
	if err != nil {
		rechargeError(c, err)
		return nil, false
	}
	return o, true
}

// 内部说明和事件只在运营详情的权限与归属范围返回。
// 先确认订单归属，再读取该订单的税务摘要。
// 开票状态不依赖代充值履约状态，已完成订单仍可等待开票。
// 未申请订单不额外查询发票表，避免增加无关读取。
// 发票申请与订单已经在创建事务中关联，本接口不补建缺失数据。
// 本次查询不修改申请或文件，也不向支付网关发起请求。
func GetRechargeOrder(c *gin.Context) {
	operations := strings.Contains(c.FullPath(), "/operations/")
	o, ok := rechargeOrder(c, operations)
	if !ok {
		return
	}
	result := gin.H{"order": o}
	// 只返回当前订单的欠款状态，不扩散财务收款凭证或其他用户授信。
	if o.PaymentProvider == "credit" && o.PaymentStatus != "pending" {
		var entry model.CreditEntry
		if err := model.DB.Where("module = ? AND trade_no = ?", model.CreditModuleRecharge, o.TradeNo).First(&entry).Error; err != nil {
			rechargeError(c, err)
			return
		}
		result["credit_status"] = entry.Status
	}
	// 发票属于买家的税务资料，运营读单权限不扩大为财务权限。
	// 仅本人详情返回摘要；业务员仍从原财务流程协作开票。
	if !operations {
		result["invoice"] = nil // 未申请开票与申请读取失败必须区分。
		if o.InvoiceRequired {
			// 用服务器订单的交易号、所有者和业务类型精确关联。
			// 不使用模糊列表搜索，历史订单不会受分页或同号片段影响。
			var invoice model.InvoiceRequest
			err := model.DB.Where("trade_no = ? AND user_id = ? AND business_type = ?", o.TradeNo, o.UserID, "recharge").First(&invoice).Error
			if err == nil {
				err = invoice.Expand() // 读取冻结抬头，不用用户后来修改的常用抬头。
			}
			if err != nil {
				// 坏快照或丢失记录不能伪装成待开票，也不回显税务原文。
				common.ApiErrorT(c, "Failed to load invoice requests")
				return
			}
			// 只返回展示和下载所需字段，不扩散税号、银行资料或磁盘路径。
			// 下载仍用原本人发票接口逐次验证归属，摘要不授予文件访问权。
			result["invoice"] = gin.H{
				"id": invoice.Id, "status": invoice.Status, "title": invoice.Profile.Title,
				"pay_amount": invoice.PayAmount, "invoice_number": invoice.InvoiceNumber,
				"remark": invoice.Remark, "has_file": invoice.HasFile,
			}
		}
	}
	if operations && (c.GetInt("role") >= common.RoleAdminUser || o.AssigneeID == c.GetInt("id")) {
		events := []model.AccountRechargeEvent{}
		if err := model.DB.Where("order_id = ?", o.ID).Order("id DESC").Limit(200).Find(&events).Error; err != nil {
			rechargeError(c, err)
			return
		}
		result["events"], result["internal_note"] = events, o.InternalNote
	}
	common.ApiSuccess(c, result)
}

// 支付凭据持久化在原单，
// 重复点击返回相同交易号和付款入口。
// 官方请求结果不确定时仍用原交易号重试，
// 禁止转成钱包付款。
// 先按本人归属读取订单，
// 再以交易号锁定当前进程重复点击。
// 跨进程幂等依赖数据库状态和同一网关交易号，
// 不依赖进程锁。
// 余额订单只调用钱包事务，
// 不能借微信表单路径改变来源。
// 已付款或退款订单不再次创建外部收款，
// 客户端重新查询真实状态。
// 付款窗口到期不自动宣称未付款，
// 提示先核对原订单。
// 已经保存的支付入口原样返回，
// 避免一次点击产生两个交易号。
// 网关表单采用当前配置签名，
// 但仍绑定原单冻结的金额和方式。
// 异步通知地址独立于钱包和订阅回调，
// 不能让代充值增加平台额度。
// 浏览器回跳只返回购买页面，
// 不从回跳参数直接入账。
// 官方渠道重新检查是否可用于新支付，
// 停用不影响独立通知验签。
// 支付方创建结果不确定时仍保留原订单，
// 不自动切换到余额扣款。
// 这里只保存付款交互资料，
// 不存完整支付方响应或商户配置。
// 付款入口保存失败必须报错，
// 不能向用户假报可继续状态。
// 接口响应只包含二维码、跳转或表单的公开交互字段。
func PayRechargeOrder(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	o, ok := rechargeOrder(c, false)
	if !ok {
		return
	}
	// 已付请求保持幂等；暂停时拒绝生成或重发未付订单的支付入口。
	if o.PaymentStatus == "pending" && !requireRechargeSales(c) {
		return
	}
	LockOrder(o.TradeNo)
	defer UnlockOrder(o.TradeNo)
	if o.PaymentProvider == "credit" {
		// 实际占用额度在后端完成；接口重试只恢复同一订单。
		if err := model.PayAccountRechargeCredit(o.ID, c.GetInt("id")); err != nil {
			rechargeError(c, err)
			return
		}
		common.ApiSuccess(c, gin.H{"payment_kind": "paid"})
		return
	}
	if o.PaymentProvider == "wallet" {
		if err := model.PayAccountRechargeWallet(o.ID, c.GetInt("id")); err != nil {
			rechargeError(c, err)
			return
		}
		common.ApiSuccess(c, gin.H{"payment_kind": "paid"})
		return
	}
	if o.PaymentStatus != "pending" {
		common.ApiSuccess(c, gin.H{"payment_kind": "paid"})
		return
	}
	if o.ExpiresAt <= common.GetTimestamp() {
		common.ApiErrorT(c, "Payment window expired. Check the order before purchasing again.")
		return
	}
	if o.PaymentData != "" {
		var payment gin.H
		if common.Unmarshal([]byte(o.PaymentData), &payment) != nil {
			rechargeError(c, model.ErrRechargeInvalid)
			return
		}
		common.ApiSuccess(c, payment)
		return
	}
	amount := decimal.NewFromInt(o.MoneyCents).Div(decimal.NewFromInt(100))
	var payment gin.H
	if o.PaymentProvider == model.PaymentProviderEpay {
		client := GetEpayClient()
		if client == nil {
			rechargeError(c, model.ErrRechargeUnavailable)
			return
		}
		notify, err := url.Parse(strings.TrimRight(service.GetCallbackAddress(), "/") + "/api/account-recharge/epay/notify")
		if err != nil {
			rechargeError(c, err)
			return
		}
		returnURL, err := url.Parse(strings.TrimRight(service.GetCallbackAddress(), "/") + "/recharge")
		if err != nil {
			rechargeError(c, err)
			return
		}
		uri, params, err := client.Purchase(&epay.PurchaseArgs{Type: "wxpay", ServiceTradeNo: o.TradeNo, Name: o.Title, Money: amount.StringFixed(2), Device: epay.PC, NotifyUrl: notify, ReturnUrl: returnURL})
		if err != nil {
			common.ApiErrorT(c, "Failed to start payment")
			return
		}
		payment = gin.H{"payment_kind": "form", "url": uri, "params": params, "trade_no": o.TradeNo, "expires_at": o.ExpiresAt}
	} else {
		channel, ok := operation_setting.GetOfficialPaymentChannel(o.PaymentMethod, true)
		if !ok {
			rechargeError(c, model.ErrRechargeUnavailable)
			return
		}
		result, err := service.CreateOfficialPayment(c.Request.Context(), *channel, o.TradeNo, amount, o.Title, c.ClientIP(), strings.TrimRight(service.GetCallbackAddress(), "/")+"/api/user/wechat-pay/notify/"+channel.ID, paymentReturnPath("/recharge"), time.Unix(o.ExpiresAt, 0))
		if err != nil {
			common.ApiErrorT(c, "Failed to start payment")
			return
		}
		payment = officialPaymentResponse(o.TradeNo, o.ExpiresAt, result)
	}
	encoded, err := common.Marshal(payment)
	if err != nil {
		rechargeError(c, err)
		return
	}
	if err = model.DB.Model(o).Update("payment_data", string(encoded)).Error; err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, payment)
}

// 网关通知必须验证签名、成功状态、方式和精确金额；
// 浏览器跳转不入账。
// 返回 fail 使支付方重试，
// 不将异常伪装成支付成功。
// POST 表单与 GET参数都由标准解析器处理，
// 不接受客户端指定验证密钥。
// SDK 验签通过之后仍检查商户编号，
// 避免共享密钥配置串商户。
// 只接收微信成功状态，
// 其他方式或非成功通知不能推进订单。
// 支付金额按十进制解析，
// 非法数字不会回退到零或商品价格。
// 业务前缀必须属于代充值，
// 无法借此完成钱包或平台订阅订单。
// 最终订单金额和渠道匹配位于数据库事务内，
// 避免预读竞争。
// 失败返回 fail供支付方重试，
// 不吞掉数据库或金额核对错误。
// 通知正文和支付者资料不写入订单操作记录，
// 只保留必要参考号。
func RechargeEpayNotify(c *gin.Context) {
	params := map[string]string{}
	if err := c.Request.ParseForm(); err != nil {
		c.String(200, "fail")
		return
	}
	for key := range c.Request.Form {
		params[key] = c.Request.Form.Get(key)
	}
	client := GetEpayClient()
	if client == nil {
		c.String(200, "fail")
		return
	}
	info, err := client.Verify(params)
	if err != nil || !info.VerifyStatus || info.TradeStatus != epay.StatusTradeSuccess || info.Type != "wxpay" || params["pid"] != operation_setting.EpayId {
		c.String(200, "fail")
		return
	}
	amount, err := decimal.NewFromString(info.Money)
	if err != nil || !strings.HasPrefix(info.ServiceTradeNo, "RCHUSR") {
		c.String(200, "fail")
		return
	}
	if err = model.CompleteAccountRechargePayment(info.ServiceTradeNo, model.PaymentProviderEpay, "wxpay", params["trade_no"], amount); err != nil {
		c.String(200, "fail")
		return
	}
	c.String(200, "success")
}

// 提交时凭据只流经内存与加密服务，
// SQL 只能看到密文。
// 加密后事务再次核对归属、状态和版本，
// 不会覆盖已完成订单。
// 普通用户只能提交已付款且待资料或待补资料的本人订单。
// 邮箱与原始 session JSON共同认证加密，
// 数据库从不接触明文。
// 格式校验失败不向用户返回其提交的内容，
// 避免错误日志泄漏。
// 密钥缺失或损坏明确拒绝，
// 不启用明文存储作为降级路径。
// 提交与重新入队同一事务，
// 旧版本不能覆盖另一处理动作。
// 补交会保留原处理人，
// 用户无需再购买或再次付款。
// 返回成功只表示资料已入队，
// 不能宣称供应商已经受理。
func SubmitRechargeInfo(c *gin.Context) {
	o, ok := rechargeOrder(c, false)
	if !ok {
		return
	}
	var req struct {
		Version int `json:"version"`
		service.RechargeAccountInfo
	}
	if !rechargeBody(c, &req) {
		return
	}
	secret, mask, err := service.EncryptRechargeInfo(o.TradeNo, req.RechargeAccountInfo)
	if err != nil {
		// 只公开固定业务错误，既保留可操作原因，也不泄露用户资料或内部异常。
		// 未知故障继续走统一服务端错误响应，不能伪装成用户填写有误。
		switch {
		case errors.Is(err, service.ErrRechargeAccountInfo), errors.Is(err, service.ErrRechargeEmailMismatch), errors.Is(err, service.ErrRechargeEncryption):
			common.ApiErrorT(c, err.Error())
		default:
			rechargeError(c, err)
		}
		return
	}
	if err = model.SubmitAccountRechargeInfo(o.ID, c.GetInt("id"), req.Version, secret, mask); err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// 敏感证明由服务端重建上下文，
// 不允许客户端替换订单或动作。
// 证明摘要使用服务器定义的字段结构，
// 客户端不能随意扩展授权上下文。
// 动作区分读取、批准退款与确认退款，
// 不能拿一种证明做另一种动作。
// 订单版本绑定当前处理权，
// 转派及资料更新会使旧上下文失效。
// 统一验证服务负责会话绑定、有效期和单次消费，
// 不另造弱验证流程。
// 消费失败立即返回，
// 后续解密或资金操作不得继续。
func rechargeProof(c *gin.Context, id, version int, action string) bool {
	raw, err := common.Marshal(service.RechargeVerificationContext{OrderID: id, Version: version, Action: action})
	if err != nil {
		rechargeError(c, err)
		return false
	}
	return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeRecharge, Context: raw}) != nil
}

// 白名单动作之外一律拒绝；
// 转派需要独立能力，
// 不能靠管理页按钮隐藏。
// 普通办理权限不包含转派和退款审批，
// 相关动作额外检查独立能力。
// 调用者只能传动作、版本和必要说明，
// 不能任意修改订单结构。
// 业务员本人约束在模型事务内再次校验，
// 防止旧页面继续办理。
// 完成必须有说明，
// 表示人工核对结果而不是自动查询上游。
// 内部备注不修改用户可见结果，
// 转派原因也只作为业务记录。
// 此接口永不写入实际收款金额或手工把待付款订单变成已付款。
// 退款申请和拒绝申请都不直接改变用户资金，
// 资金由独立入口办理。
func ActRechargeOrder(c *gin.Context) {
	o, ok := rechargeOrder(c, true)
	if !ok {
		return
	}
	var req struct {
		Version    int    `json:"version"`
		Action     string `json:"action"`
		Note       string `json:"note"`
		AssigneeID int    `json:"assignee_id"`
	}
	if !rechargeBody(c, &req) {
		return
	}
	if req.Action == "reject_refund" && !authz.Can(c.GetInt("id"), c.GetInt("role"), authz.RechargeRefund) {
		rechargeError(c, model.ErrRechargeForbidden)
		return
	}
	if req.Action == "assign" && !authz.Can(c.GetInt("id"), c.GetInt("role"), authz.RechargeAssign) {
		rechargeError(c, model.ErrRechargeForbidden)
		return
	}
	if err := model.ActAccountRechargeOrder(o.ID, c.GetInt("id"), req.Version, c.GetInt("role") >= common.RoleAdminUser, req.Action, req.Note, req.AssigneeID); err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// 读取先消费会话单次证明，
// 再在事务内检查最新处理人并写审计。
// no-store响应不通过普通订单详情或列表返回，
// 也不缓存到客户端查询库。
// 路由先校验独立揭示能力，
// 此处再消费对应订单的单次证明。
// 取密文时重新检查已付款、待办理状态及处理人，
// 不能信任预读快照。
// 揭示事件与版本更新同事务，
// 审计写入失败就不返回资料。
// 解密使用持久订单号作为关联数据，
// 不能由用户替换。
// 完成、退款或要求补资料后的旧凭据不可继续通过此接口读取。
// 成功响应不经过普通订单序列化，
// 也不附带供应商内部信息。
// 管理审计中间件针对本路径禁用响应正文缓冲，
// 只保留动作元数据。
func RevealRechargeInfo(c *gin.Context) {
	o, ok := rechargeOrder(c, true)
	if !ok {
		return
	}
	var req struct {
		Version int `json:"version"`
	}
	if !rechargeBody(c, &req) || !rechargeProof(c, o.ID, req.Version, "reveal") {
		return
	}
	secret, err := model.ReadAccountRechargeSecret(o.ID, c.GetInt("id"), req.Version, c.GetInt("role") >= common.RoleAdminUser)
	if err != nil {
		rechargeError(c, err)
		return
	}
	info, err := service.DecryptRechargeInfo(o.TradeNo, secret)
	if err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, info)
}

// 批准与资金确认均需要退款能力及单次验证，
// 业务员申请不能替代审批。
// 微信凭证是人工核实记录；
// 接口不会声称已经调用支付方退款。
// 退款入口属于管理员独立能力，
// 业务员申请无法进入此处理器。
// 批准退款首先停止履约，
// 资金确认是另一个显式请求。
// 微信需要管理员先在网关完成真实原路退款，
// 再提交凭证号。
// 余额退款由服务器取原单扣除整数，
// 不接收任意退款金额。
// 每次请求都绑定当前版本和动作，
// 读取资料的证明不能用于退款。
// 数据库已返还但缓存未知时保留恢复标志，
// 可重复确认同单修复。
// 恢复操作不创建第二笔退款事件，
// 也不重新按当前汇率计算金额。
// 失败保持可核对状态，
// 不能用异常处理中断伪造已退款。
func RefundRechargeOrder(c *gin.Context) {
	o, ok := rechargeOrder(c, true)
	if !ok {
		return
	}
	var req struct {
		Version   int    `json:"version"`
		Confirm   bool   `json:"confirm"`
		Note      string `json:"note"`
		Reference string `json:"reference"`
	}
	if !rechargeBody(c, &req) {
		return
	}
	action := "refund"
	if req.Confirm {
		action = "refund_confirm"
	}
	if !rechargeProof(c, o.ID, req.Version, action) {
		return
	}
	if err := model.RefundAccountRecharge(o.ID, c.GetInt("id"), req.Version, req.Confirm, req.Note, req.Reference); err != nil {
		rechargeError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
