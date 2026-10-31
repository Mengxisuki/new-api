package controller

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 全链路从真实抬头和报价进入订单，再走原支付完成函数和附件接口。
// 无网络付款、无真实税号，不复制收费公式来计算预期结果。
// 三库使用现有隔离测试创建器，不能往开发或生产库写发票。
func TestInvoiceLifecycleDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} { // 每个真实方言独立验证，不以编译通过代替事务正确性。
		for _, upgrade := range []bool{false, true} { // 同时验收空库安装与现有用户数据升级。
			t.Run(fmt.Sprintf("%s/upgrade=%v", dialect, upgrade), func(t *testing.T) {
				dsn := "" // 外部数据库只使用本轮专用测试DSN，不猜测线上连接。
				if dialect == "mysql" {
					dsn = os.Getenv("TEST_MYSQL_DSN")
				}
				if dialect == "postgres" {
					dsn = os.Getenv("TEST_POSTGRES_DSN")
				}
				if dialect != "sqlite" && dsn == "" {
					t.Skip("test database DSN is not configured")
				}
				db := modelManagementDB(t, dialect, dsn) // 复用既有独立数据库创建器，避免接触已有业务库。
				if upgrade {
					require.NoError(t, db.Table("top_ups").AutoMigrate(&topUpBeforeInvoice{}))
					require.NoError(t, db.Table("subscription_orders").AutoMigrate(&subscriptionOrderBeforeInvoice{}))
					require.NoError(t, db.Table("top_ups").Create(&topUpBeforeInvoice{UserId: 999, Amount: 100, CreditedQuota: 500000, Money: 100, TradeNo: "legacy-topup", Status: common.TopUpStatusPending}).Error)
					require.NoError(t, db.Table("subscription_orders").Create(&subscriptionOrderBeforeInvoice{UserId: 999, PlanId: 999, Money: 10, TradeNo: "legacy-subscription", Status: common.TopUpStatusSuccess}).Error)
				}
				require.NoError(t, db.AutoMigrate(&model.InvoiceProfile{}, &model.InvoiceRequest{}, &model.TopUp{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.Log{}))
				for range 2 { // 重复启动迁移必须保持旧数据及默认未开票状态。
					require.NoError(t, db.AutoMigrate(&model.InvoiceProfile{}, &model.InvoiceRequest{}, &model.TopUp{}, &model.SubscriptionOrder{}))
				}
				if upgrade {
					var old model.TopUp
					require.NoError(t, db.Where("trade_no = ?", "legacy-topup").First(&old).Error)
					assert.False(t, old.InvoiceRequired)             // 升级不能自动为历史充值申请开票或追加收费。
					assert.EqualValues(t, 500000, old.CreditedQuota) // 冻结权益是旧订单已购买的数据。
					var order model.SubscriptionOrder
					require.NoError(t, db.Where("trade_no = ?", "legacy-subscription").First(&order).Error)
					assert.False(t, order.InvoiceRequired) // 历史订阅继续采用不需要发票的默认值。
					assert.Equal(t, 10.0, order.Money)     // 新增开票功能不得追溯提高旧订单售价。
				}
				checkInvoiceLifecycle(t, db)
			})
		}
	}
}

// 旧版本付款表没有开票标志；升级验证保留原金额和冻结权益。
// 这两个结构只重建历史schema，不进入生产模型注册或兼容接口。
type topUpBeforeInvoice struct {
	Id              int     // 旧表主键必须在升级后保留。
	UserId          int     // 旧订单所有者不随新字段迁移变化。
	Amount          int64   // 旧充值数量与最终实付不是同一个字段。
	CreditedQuota   int     `gorm:"type:bigint;not null;default:0"` // 已有冻结权益不得在迁移时归零。
	Money           float64 // 旧收款金额必须保持原值。
	TradeNo         string  `gorm:"unique;type:varchar(255);index"` // 订单唯一标识与索引继续有效。
	PaymentMethod   string  `gorm:"type:varchar(50)"`               // 原支付方式用于回调归属校验。
	PaymentProvider string  `gorm:"type:varchar(50);default:''"`    // 厂商标识不能随发票功能变更。
	CreateTime      int64   // 原下单时间不能被重置。
	CompleteTime    int64   // 已支付历史仍保留到账时间。
	Status          string  // 旧付款状态不因新增开票字段升级。
}
type subscriptionOrderBeforeInvoice struct {
	Id              int     // 旧表主键必须在升级后保留。
	UserId          int     // 旧订单所有者不随新字段迁移变化。
	PlanId          int     // 历史订阅关联的套餐身份保持不变。
	Money           float64 // 旧收款金额必须保持原值。
	TradeNo         string  `gorm:"unique;type:varchar(255);index"` // 订单唯一标识与索引继续有效。
	PaymentMethod   string  `gorm:"type:varchar(50)"`               // 原支付方式用于回调归属校验。
	PaymentProvider string  `gorm:"type:varchar(50);default:''"`    // 厂商标识不能随发票功能变更。
	Status          string  // 旧付款状态不因新增开票字段升级。
	CreateTime      int64   // 原下单时间不能被重置。
	CompleteTime    int64   // 已支付历史仍保留到账时间。
	ProviderPayload string  `gorm:"type:text"` // 历史网关引用不进入新的抬头快照。
}

// 业务配置与数据均显式建立，测试顺序不能决定加价费率。
// 本函数保留端到端串行账务验收，较长部分为显式断言与文件fixture，
// 不把同一付款过程拆成多个独立mock测试，以免丢失事务接线回归。
// 个人资料后来变更时，已创建申请仍保留当时的抬头和税号快照。
func checkInvoiceLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	general := operation_setting.GetGeneralSetting()
	oldDisplay := general.QuotaDisplayType                           // 结束后还原站点单位，后续测试不继承人民币模式。
	oldSettings := config.GlobalConfig.ExportAllConfigs()            // 保留原默认率和每组覆盖，不能让场景改价泄漏。
	general.QuotaDisplayType = operation_setting.QuotaDisplayTypeCNY // 本轮开票仅针对确认过的本地人民币渠道。
	t.Cleanup(func() {
		general.QuotaDisplayType = oldDisplay
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("invoice_setting"), map[string]string{"default_rate": oldSettings["invoice_setting.default_rate"], "group_rates": oldSettings["invoice_setting.group_rates"]}))
	})
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("invoice_setting"), map[string]string{"default_rate": "6", "group_rates": `{"vip":10}`}))
	user := model.User{Username: "invoice-user", AffCode: "invoice", Group: "default", Quota: 123, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	// 无抬头先拒绝选是，而选择否不应暗中生成申请或附加收费。
	_, _, err := model.NewInvoiceRequest(user.Id, "default", "topup", 100, true)
	require.EqualError(t, err, "Please configure your invoice title first")
	none, pay, err := model.NewInvoiceRequest(user.Id, "default", "topup", 100, false)
	require.NoError(t, err)
	assert.Nil(t, none)         // 默认否不需要抬头，也没有隐藏申请。
	assert.Equal(t, 100.0, pay) // 未选开票不能加收费用。
	profile := model.InvoiceProfile{TitleType: "business", Title: "科研实验室", TaxId: "TESTTAX000000000001", Email: "finance@example.test"}
	_, err = model.SaveInvoiceProfile(user.Id, profile)
	require.NoError(t, err)
	invoice, pay, err := model.NewInvoiceRequest(user.Id, "default", "topup", 100, true)
	require.NoError(t, err)
	assert.Equal(t, 106.0, pay)             // 精确预期直接来自六个百分点需求。
	assert.Equal(t, 6.0, invoice.FeeAmount) // 开票附加费独立于充值权益记录。
	vip, vipPay, err := model.NewInvoiceRequest(user.Id, "vip", "topup", 100, true)
	require.NoError(t, err)
	assert.Equal(t, 110.0, vipPay)     // 用户实际分组覆盖默认百分比。
	assert.Equal(t, 10.0, vip.FeeRate) // 快照保留选中的费率，不能只保存最终金额。
	order := model.TopUp{UserId: user.Id, Amount: 100, CreditedQuota: 500000, Money: pay, TradeNo: "USR-invoice-" + strconv.Itoa(user.Id), PaymentProvider: model.PaymentProviderWechat, PaymentMethod: "wechat-main", Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp(), Invoice: invoice}
	require.NoError(t, order.Insert()) // 订单及发票快照必须一起持久化成功。
	// 未付款不能靠财务状态更新变成已开票；订单未支付也不增加余额。
	_, err = model.UpdateInvoiceRequest(invoice.Id, model.InvoiceProcessing{Status: "processing"})
	require.EqualError(t, err, "Only paid orders can be invoiced")
	profile.Title = "修改后的实验室" // 模拟付款后用户改了个人资料中的当前抬头。
	_, err = model.SaveInvoiceProfile(user.Id, profile)
	require.NoError(t, err)
	require.NoError(t, model.CompleteOfficialTopUp(order.TradeNo, model.PaymentProviderWechat, "wechat-main", decimal.NewFromInt(106), "127.0.0.1"))
	require.NoError(t, model.CompleteOfficialTopUp(order.TradeNo, model.PaymentProviderWechat, "wechat-main", decimal.NewFromInt(106), "127.0.0.1"))
	var paid model.User
	require.NoError(t, db.First(&paid, user.Id).Error)
	assert.Equal(t, 500123, paid.Quota) // 附加费六元不赠送平台余额，重复回调不重复发放。
	stored, err := model.InvoiceByID(invoice.Id, user.Id)
	require.NoError(t, err)
	assert.Equal(t, "pending", stored.Status)      // 付款后才进入财务待处理队列。
	assert.Equal(t, "科研实验室", stored.Profile.Title) // 已有申请仍使用原下单快照。
	_, err = model.InvoiceByID(invoice.Id, user.Id+1)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound) // 他人记录与不存在记录使用一致边界。
	_, err = model.UpdateInvoiceRequest(invoice.Id, model.InvoiceProcessing{Status: "issued", InvoiceNumber: "INV-001"})
	require.EqualError(t, err, "Please upload the invoice file before marking it issued")
	// 文件落在本测试私有目录，API只返回has_file而不公开其磁盘路径。
	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(t.TempDir())) // 文件只落本次临时数据目录，测试退出自动清理。
	t.Cleanup(func() { require.NoError(t, os.Chdir(previous)) })
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "invoice.pdf") // 字段名遵从真实上传端点，不绕过multipart解析。
	require.NoError(t, err)
	_, err = part.Write([]byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n%%EOF"))
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("invoice_number", "INV-001"))
	require.NoError(t, writer.Close())
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(invoice.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/finance/invoices/file", &body)
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
	UploadInvoiceFile(ctx) // 真实控制器执行类型校验、私有存储和状态变更。
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.NotContains(t, recorder.Body.String(), `file_path`) // API不能泄露实际磁盘路径。
	stored, err = model.InvoiceByID(invoice.Id, user.Id)
	require.NoError(t, err)
	assert.Equal(t, "issued", stored.Status)            // 有附件且有号码才视为真正交付。
	assert.True(t, stored.HasFile)                      // 页面下载入口来自实际持久附件引用。
	require.FileExists(t, stored.FilePath)              // 不能用空文件记录冒充上传完成。
	for _, owner := range []int{user.Id, user.Id + 1} { // 同一附件分别验证本人和非本人请求。
		rec := httptest.NewRecorder()
		download, _ := gin.CreateTestContext(rec)
		download.Params = gin.Params{{Key: "id", Value: strconv.Itoa(invoice.Id)}}
		download.Set("id", owner)
		download.Request = httptest.NewRequest(http.MethodGet, "/api/user/invoices/file", nil)
		DownloadUserInvoiceFile(download) // 不mock文件或查询函数，真实归属条件必须生效。
		if owner == user.Id {
			assert.Equal(t, http.StatusOK, rec.Code) // 本人能下载已开具附件。
			assert.True(t, strings.HasPrefix(rec.Body.String(), "%PDF-"))
			assert.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
		} else {
			assert.Equal(t, http.StatusNotFound, rec.Code) // 不能通过递增申请ID读取他人发票。
		}
	}
	// 列表使用同一私有快照，分页和关键词均走参数化查询。
	rows, total, err := model.InvoiceRecords(user.Id, 1, 10, "issued", "USR-invoice")
	require.NoError(t, err)
	assert.EqualValues(t, 1, total) // 指定状态的列表只包含实际开具过的申请。
	require.Len(t, rows, 1)
	assert.Equal(t, "INV-001", rows[0].InvoiceNumber) // 财务填写的号码与用户看到的列表一致。
	// 继续支付复制原抬头和费率，不能按管理员后来调整的价格重新收费。
	retryInvoice, retryMoney, err := model.NewInvoiceRequest(user.Id, "default", "topup", 100, true)
	require.NoError(t, err)
	retryOrder := model.TopUp{UserId: user.Id, Amount: 100, CreditedQuota: 500000, Money: retryMoney, TradeNo: "USR-invoice-retry", PaymentProvider: model.PaymentProviderWechat, PaymentMethod: "wechat-main", Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp(), Invoice: retryInvoice}
	require.NoError(t, retryOrder.Insert()) // 原待付款单先有独立快照，重试不得重新读取当前资料。
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("invoice_setting"), map[string]string{"default_rate": "20"}))
	replacement, err := model.ReplacePendingOfficialTopUpForRetry(user.Id, retryOrder.TradeNo, "USR-invoice-retry-new", common.GetTimestamp())
	require.NoError(t, err)
	assert.True(t, replacement.InvoiceRequired)        // 替代交易继续承担已选择的开票义务。
	assert.Equal(t, 106.0, replacement.Money)          // 后台费率改为百分之二十也不能重算此单。
	assert.Equal(t, 500000, replacement.CreditedQuota) // 继续支付不能把附加费加入余额。
	var replacementInvoice model.InvoiceRequest
	require.NoError(t, db.Where("trade_no = ?", replacement.TradeNo).First(&replacementInvoice).Error)
	assert.Equal(t, 6.0, replacementInvoice.FeeRate) // 新订单沿用原冻结的六个百分点。
	assert.Equal(t, retryInvoice.ProfileJSON, replacementInvoice.ProfileJSON)
	require.NoError(t, model.CompleteOfficialTopUp(replacement.TradeNo, model.PaymentProviderWechat, "wechat-main", decimal.NewFromInt(106), "127.0.0.1"))
	var ready int64
	require.NoError(t, db.Model(&model.InvoiceRequest{}).Where("trade_no IN ? AND status = ?", []string{retryOrder.TradeNo, replacement.TradeNo}, "pending").Count(&ready).Error)
	assert.EqualValues(t, 1, ready) // 原待付款申请不会被重试误标为已支付，只可处理新单。
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("invoice_setting"), map[string]string{"default_rate": "6"}))
	// 订阅也使用同一加价规则，付106只开通100面额套餐而非钱包充值。
	plan := model.SubscriptionPlan{Title: "Invoice subscription", PriceAmount: 100, DurationUnit: "month", DurationValue: 1, TotalAmount: 1000, Enabled: true}
	require.NoError(t, db.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })
	subInvoice, gross, err := model.NewInvoiceRequest(user.Id, "default", "subscription", 100, true)
	require.NoError(t, err)
	subOrder := model.SubscriptionOrder{UserId: user.Id, PlanId: plan.Id, Money: gross, TradeNo: "SUBUSR-invoice", PaymentProvider: model.PaymentProviderWechat, PaymentMethod: "wechat-main", Status: common.TopUpStatusPending, Invoice: subInvoice}
	require.NoError(t, subOrder.Insert()) // 订阅单与对应抬头快照也使用同一事务。
	require.NoError(t, model.CompleteOfficialSubscriptionOrder(subOrder.TradeNo, "", model.PaymentProviderWechat, "wechat-main", decimal.NewFromInt(106)))
	require.NoError(t, model.CompleteOfficialSubscriptionOrder(subOrder.TradeNo, "", model.PaymentProviderWechat, "wechat-main", decimal.NewFromInt(106)))
	require.NoError(t, db.First(&paid, user.Id).Error)
	assert.Equal(t, 1000123, paid.Quota) // 购买订阅只发订阅权益，不增加钱包充值余额。
	subStored, err := model.InvoiceByID(subInvoice.Id, user.Id)
	require.NoError(t, err)
	assert.Equal(t, "pending", subStored.Status)        // 订阅支付完成同样进入统一开票队列。
	assert.Equal(t, "修改后的实验室", subStored.Profile.Title) // 新单可以使用新抬头，旧单快照不随之变化。
	var subs []model.UserSubscription
	require.NoError(t, db.Where("user_id = ?", user.Id).Find(&subs).Error)
	require.Len(t, subs, 1)
	assert.EqualValues(t, 1000, subs[0].AmountTotal) // 开票加价不能变相加送订阅额度。

	assert.True(t, filepath.IsAbs(stored.FilePath)) // 下载路径不依赖后续工作目录漂移。
}

// 设置校验必须拒绝非法百分比及坏映射，不能在收款时静默免收。
func TestInvoiceSettingValidation(t *testing.T) {
	for _, value := range []string{"-1", "101", "null", `"6"`, "6.00001"} {
		assert.Error(t, operation_setting.ValidateInvoiceSettingUpdate("invoice_setting.default_rate", value), fmt.Sprintf("value=%s", value))
	}
	assert.NoError(t, operation_setting.ValidateInvoiceSettingUpdate("invoice_setting.default_rate", "6"))
	assert.NoError(t, operation_setting.ValidateInvoiceSettingUpdate("invoice_setting.group_rates", `{"default":0,"vip":6}`))
	assert.Error(t, operation_setting.ValidateInvoiceSettingUpdate("invoice_setting.group_rates", `[]`))
}

// SDK生成有效通知签名，证明验签成功不能代替应付总金额核对。
// 三条入口分别保护充值通知、订阅通知与浏览器返回，不模拟生产校验。
// 少付和非整分金额均不能到账；足额重复通知只能完成一次。
func TestInvoiceEpaySignedCallbacksRequireGrossAmount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := modelManagementDB(t, "sqlite", "") // 付款通知只触及本场景隔离数据库。
	require.NoError(t, db.AutoMigrate(&model.InvoiceProfile{}, &model.InvoiceRequest{}, &model.TopUp{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.Log{}))
	oldPayment := *operation_setting.GetPaymentSetting() // 既有启用状态与商户测试配置结束后完整还原。
	oldAddress, oldID, oldKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	oldMethods := operation_setting.PayMethods // 避免测试的微信方法覆盖后续支付配置。
	oldDisplay := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldInvoice := config.GlobalConfig.ExportAllConfigs() // 费率固定为六，但不泄漏到其他用例。
	t.Cleanup(func() {
		*operation_setting.GetPaymentSetting() = oldPayment
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = oldAddress, oldID, oldKey
		operation_setting.PayMethods = oldMethods
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplay
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("invoice_setting"), map[string]string{"default_rate": oldInvoice["invoice_setting.default_rate"], "group_rates": oldInvoice["invoice_setting.group_rates"]}))
	})
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://payment.example", "invoice-merchant", "invoice-fixture-key"
	operation_setting.PayMethods = []map[string]string{{"type": "wxpay", "name": "test"}} // 仅作为启用入口的虚构方法，不连接实际商户。
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeCNY // 复现本轮本地人民币开票收费。
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("invoice_setting"), map[string]string{"default_rate": "6", "group_rates": "{}"}))
	for _, scenario := range []struct {
		name, path   string
		handler      gin.HandlerFunc
		subscription bool
	}{
		{"topup", "/api/user/epay/notify", EpayNotify, false},
		{"subscription", "/api/subscription/epay/notify", SubscriptionEpayNotify, true},
		{"return", "/api/subscription/epay/return", SubscriptionEpayReturn, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			user := model.User{Username: "invoice-sign-" + scenario.name, AffCode: scenario.name, Group: "default", Quota: 123, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			_, err := model.SaveInvoiceProfile(user.Id, model.InvoiceProfile{TitleType: "personal", Title: "测试用户", Email: "invoice@example.test"})
			require.NoError(t, err)
			business := "topup"        // 申请归属业务与真实订单类型保持一致。
			if scenario.subscription { // 通知及返回两条订阅路径都要经过同一金额门槛。
				business = "subscription"
			}
			invoice, gross, err := model.NewInvoiceRequest(user.Id, "default", business, 100, true)
			require.NoError(t, err)
			trade := fmt.Sprintf("invoice-signed-%s-%d", scenario.name, user.Id)
			if scenario.subscription { // 通知及返回两条订阅路径都要经过同一金额门槛。
				plan := model.SubscriptionPlan{Title: trade, PriceAmount: 100, DurationUnit: "month", DurationValue: 1, TotalAmount: 1000, Enabled: true}
				require.NoError(t, db.Create(&plan).Error)
				model.InvalidateSubscriptionPlanCache(plan.Id)
				t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })
				require.NoError(t, (&model.SubscriptionOrder{UserId: user.Id, PlanId: plan.Id, Money: gross, TradeNo: trade, PaymentProvider: model.PaymentProviderEpay, PaymentMethod: "wxpay", Status: common.TopUpStatusPending, Invoice: invoice}).Insert())
			} else {
				require.NoError(t, (&model.TopUp{UserId: user.Id, Amount: 100, CreditedQuota: 500000, Money: gross, TradeNo: trade, PaymentProvider: model.PaymentProviderEpay, PaymentMethod: "wxpay", Status: common.TopUpStatusPending, Invoice: invoice}).Insert())
			}
			for _, paid := range []string{"100.00", "105.999", "106.001", "106.00", "106.00"} { // 少付、非整分和重复成功分别保护不同边界。
				params, err := epay.GenerateParamsWithError(map[string]string{"pid": "invoice-merchant", "type": "wxpay", "out_trade_no": trade, "trade_no": "upstream-fixture", "trade_status": epay.StatusTradeSuccess, "money": paid, "sign_type": "MD5"}, operation_setting.EpayKey)
				require.NoError(t, err)
				values := url.Values{}           // 表单编码复用真实回调协议，不直接伪造VerifyRes。
				for key, value := range params { // SDK已签名的字段原样进入HTTP请求。
					values.Set(key, value)
				}
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodGet, scenario.path+"?"+values.Encode(), nil)
				scenario.handler(ctx) // 真实控制器执行SDK验签、金额核对与原模型到账。
				if paid != "106.00" { // 即便签名有效，未足额付款也不能推进开票状态。
					if scenario.name == "return" {
						assert.Contains(t, recorder.Header().Get("Location"), "pay=fail")
					} else {
						assert.Equal(t, "fail", recorder.Body.String())
					}
					stored, err := model.InvoiceByID(invoice.Id, user.Id)
					require.NoError(t, err)
					assert.Equal(t, "pending_payment", stored.Status) // 财务队列不能出现已收齐附加费的假象。
					var unchanged model.User
					require.NoError(t, db.First(&unchanged, user.Id).Error)
					assert.Equal(t, 123, unchanged.Quota) // 少付时用户余额完全保持原样。
					var subs int64
					require.NoError(t, db.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subs).Error)
					assert.Zero(t, subs) // 不能先送订阅再在财务页面补收费用。
				} else {
					if scenario.name == "return" {
						assert.Contains(t, recorder.Header().Get("Location"), "pay=success")
					} else {
						assert.Equal(t, "success", recorder.Body.String())
					}
					stored, err := model.InvoiceByID(invoice.Id, user.Id)
					require.NoError(t, err)
					assert.Equal(t, "pending", stored.Status) // 只有足额网关确认才允许财务开始处理。
					var credited model.User
					require.NoError(t, db.First(&credited, user.Id).Error)
					if scenario.subscription { // 通知及返回两条订阅路径都要经过同一金额门槛。
						assert.Equal(t, 123, credited.Quota)
						var subs int64
						require.NoError(t, db.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subs).Error)
						assert.EqualValues(t, 1, subs) // 完成同一付款单不会重复创建或续长订阅。
					} else {
						assert.Equal(t, 500123, credited.Quota) // 附加费不增加冻结权益，重复通知也不重复加款。
					}
				}
			}
		})
	}
}

// 不需要发票的既有单仍由原网关合同处理，缺少金额字段不新增拒绝分支。
func TestInvoiceCallbackAmountKeepsLegacyNonInvoiceContract(t *testing.T) {
	require.NoError(t, validateInvoiceCallbackAmount(false, 106, ""))
	require.NoError(t, validateInvoiceCallbackAmount(false, 106, "100.00"))
	require.Error(t, validateInvoiceCallbackAmount(true, 106, ""))
}
