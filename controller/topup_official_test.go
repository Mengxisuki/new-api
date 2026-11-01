package controller

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 验签算法由 service 真实加密回归覆盖，本用例验证通知进入正确账务路径。
// 充值和订阅共用官方回调，不能因为共享接口就将订阅款记入钱包。
// 只隔离数据库句柄；模型订单完成、续期和幂等均调用真实实现。
func TestOfficialPaymentCallbackSeparatesSubscriptionFromWallet(t *testing.T) {
	gin.SetMode(gin.TestMode) // 真实请求上下文沿用 Gin 测试模式，不启动 HTTP 服务。
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false // 隔离未启动的 Redis，不模拟充值或日志业务本身。
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	db, err := gorm.Open(sqlite.Open("file:official_callback?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)                // 付款事务内查询数据库时间需要共享内存库的第二个连接。
	oldDB, oldLog := model.DB, model.LOG_DB // 保存宿主句柄，避免测试退出后污染其他控制器。
	oldMain, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB = db, db // 日志也使用隔离数据库，不能产生真实业务日志。
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLog // 先还原包状态，再关闭临时连接。
		common.SetDatabaseTypes(oldMain, oldLogType)
		assert.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionOrder{}, &model.TopUp{}, &model.Log{}))
	user := model.User{Username: "official-callback", AffCode: "callback", Quota: 700, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	plan := model.SubscriptionPlan{Title: "Monthly", DurationUnit: "month", DurationValue: 1, TotalAmount: 1000, Enabled: true}
	require.NoError(t, db.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id) // 不允许另一个数据库的同 ID 套餐缓存改变本次权益。
	t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/api/user/wechat-pay/notify/wechat-main", nil)
	channel := operation_setting.OfficialPaymentChannel{ID: "wechat-main", ChannelType: "wechat"}
	// 一笔待支付订阅和一笔充值金额相同，必须按业务前缀及实际订单区分。
	order := model.SubscriptionOrder{UserId: user.Id, PlanId: plan.Id, Money: 10, TradeNo: "SUBUSR-test", PaymentProvider: model.PaymentProviderWechat, PaymentMethod: channel.ID, Status: common.TopUpStatusPending}
	require.NoError(t, order.Insert()) // 本地待支付记录存在仍不代表已获得订阅权益。

	topup := model.TopUp{UserId: user.Id, Amount: 1, CreditedQuota: 1234, Money: 10, TradeNo: "USR-test", PaymentProvider: model.PaymentProviderWechat, PaymentMethod: channel.ID, Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp()}
	require.NoError(t, topup.Insert()) // 使用冻结额度而非重算当前汇率，守住充值计费契约。

	callback := service.OfficialPaymentCallback{TradeNo: order.TradeNo, Amount: decimal.NewFromInt(10)}
	// 签名有效也不能给不存在的订阅号充值，业务之间禁止失败回退。
	callback.TradeNo = "SUBUSR-missing"
	require.Error(t, completeOfficialPaymentCallback(ctx, channel, &callback, model.PaymentProviderWechat))
	callback.TradeNo = order.TradeNo        // 切回存在的订阅单，使少付金额断言不受订单缺失影响。
	callback.Amount = decimal.NewFromInt(9) // 金额由订单核对，少付不可开通。
	require.Error(t, completeOfficialPaymentCallback(ctx, channel, &callback, model.PaymentProviderWechat))
	callback.Amount = decimal.NewFromInt(10) // 恢复正确金额后再单独验证商户渠道隔离。
	wrongChannel := operation_setting.OfficialPaymentChannel{ID: "another-merchant"}
	require.ErrorIs(t, completeOfficialPaymentCallback(ctx, wrongChannel, &callback, model.PaymentProviderWechat), model.ErrPaymentMethodMismatch)
	for range 2 { // 同一订阅付款重复回调只创建一次权益。
		require.NoError(t, completeOfficialPaymentCallback(ctx, channel, &callback, model.PaymentProviderWechat))
	}
	// 通知重复处理后的余额必须原样保留，不能把订阅价赠送成可消费余额。
	var paidUser model.User
	require.NoError(t, db.First(&paidUser, user.Id).Error)
	assert.Equal(t, 700, paidUser.Quota) // 订阅支付不赠送钱包额度，也不消费已有余额。
	// 查询实际权益表，避免只检查回调返回成功而漏掉重复开通。
	var subscriptions []model.UserSubscription
	require.NoError(t, db.Where("user_id = ?", user.Id).Find(&subscriptions).Error)
	require.Len(t, subscriptions, 1)
	assert.EqualValues(t, 1000, subscriptions[0].AmountTotal) // 权益额度来自套餐，和付款金额分别保存。

	// 同一个回调入口换用另一笔订单号后，应准确切换到账业务。
	callback.TradeNo = topup.TradeNo // 切换到真实充值订单，只更新钱包。
	for range 2 {
		require.NoError(t, completeOfficialPaymentCallback(ctx, channel, &callback, model.PaymentProviderWechat))
	}
	require.NoError(t, db.First(&paidUser, user.Id).Error)
	assert.Equal(t, 1934, paidUser.Quota) // 冻结额度只加一次，不能按重复通知翻倍。
	// 充值通知不得借相同金额命中已有订阅，实例数量仍应保持一份。
	var count int64
	require.NoError(t, db.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count) // 钱包充值不能新建或延长订阅。
}

// 支付入口回归只隔离微信网络，SDK请求签名与响应验签仍执行真实代码。
// 本地订单号、用户归属及金额由生产控制器生成，测试不复制生成算法。
// 不允许未匹配的HTTP目标出网，避免测试配置错误触达真实商户。
// 这里实现的是标准库RoundTripper测试边界，不给生产服务增加可替换钩子。
// 失败也沿用生产错误处理和本地订单失效逻辑，不手工改造控制器响应。
type officialPaymentTestTransport func(*http.Request) (*http.Response, error)

// RoundTrip提供标准HTTP传输边界，不能替换支付服务或业务入口。
// 返回响应继续交给微信SDK验签，不能通过禁用验证模拟成功。
func (transport officialPaymentTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// 微信Native文档要求商户订单号6–32字符，并在商户范围内唯一。
// 旧SUBUSR+用户ID+随机串+UnixNano最少34字符，真实入口因此下单失败。
// 本用例覆盖一位和十位用户ID，防止修复依赖当前用户ID恰好较短。
// 通过外部HTTP边界检查实际发送字段，不以内部生成辅助函数自证。
// 模拟平台返回真实RSA签名二维码响应，SDK必须验签后控制器才报成功。
// 每次调用仍创建真实待支付订单，不提前开通订阅或增加钱包。
// 同一用户连续请求应产生不同交易号，不能用截断用户ID或固定时间凑长度。
// 用户归属来自本地订单字段，交易号保留SUBUSR只用于回调业务分流。
// 测试不会创建真实微信订单，不访问生产数据库或商户私钥。
// 原有回调回归继续验证此前缀进入订阅结算而非钱包充值。
// 字符集和长度来自厂商合同，不要求测试了解随机串怎样生成。
// 支付渠道使用公开DTO结构，所有配置通过现有Validate入口检查。
// 价格固定为十元，线上请求应为一千分，不在本次改造金额计算。
// 合规确认在测试中显式开启，避免认证前失败掩盖交易号回归。
// 未申请发票使本用例只覆盖订阅下单，不引入额外收费配置。
// 套餐缓存按实际ID失效，避免其他测试同ID的套餐影响报价。
// 数据库helper同时隔离Redis和进程配置，不接触外部用户余额。
// 请求通过JSON提交套餐及渠道，商户信息仍由服务端配置决定。
// 响应必须success=true并含二维码，不能只断言接口返回HTTP200。
// 测试结束还原默认Transport，后续其他支付测试不会使用本桩。
// 请求数来自两名用户各两次新单，不通过随机压力循环证明唯一性。
// 数据库查询核对原用户与套餐，防止单号压缩顺便丢失归属关系。
// 所有订单创建成功后仍检查权益表为空，阻止未付款先开通的回归。
func TestOfficialSubscriptionPaymentTradeNumberFitsWechat(t *testing.T) {
	// 默认离线SQLite，显式指定方言后使用既有独立数据库创建器。
	// DSN只能由测试环境提供，不推断开发或生产库的连接信息。
	// 要求执行外部方言却缺DSN时直接失败，不能跳过后声称三库通过。
	// 每个方言复用同一真实入口断言，避免数据库差异被独立假实现隐藏。
	// 隔离库的创建与清理沿用已有测试工具，不向生产模型增加测试分支。
	// 本次未改订单表结构，三库复验关注相同单号在实际保存与查询中一致。
	dialect := os.Getenv("TEST_OFFICIAL_PAYMENT_DB_DIALECT")
	dsn := ""
	switch dialect {
	case "", "sqlite":
		dialect = "sqlite"
	case "mysql":
		dsn = os.Getenv("TEST_MYSQL_DSN")
	case "postgres":
		dsn = os.Getenv("TEST_POSTGRES_DSN")
	default:
		t.Fatal("unsupported payment test database dialect")
	}
	if dialect != "sqlite" {
		require.NotEmpty(t, dsn, "explicit database dialect requires its isolated test DSN")
	}
	db := modelManagementDB(t, dialect, dsn)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}))
	settings := operation_setting.GetPaymentSetting()
	previous := *settings
	t.Cleanup(func() { *settings = previous })
	settings.ComplianceConfirmed = true
	settings.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	// 临时RSA密钥同时提供请求签名和模拟平台响应签名，绝不从磁盘读凭据。
	// 两端使用标准签名串，验证的是生产SDK，而不是绕过SDK的假响应。
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	const publicID = "PUB_KEY_ID_SUBSCRIPTION_TEST"
	settings.OfficialChannels = []operation_setting.OfficialPaymentChannel{{
		ID: "wechat-subscription-test", Name: "微信测试", ChannelType: operation_setting.PaymentChannelWechat,
		InteractionMode: operation_setting.PaymentInteractionQR, Enabled: true,
		Config: map[string]string{
			"appid": "wx-test-app", "mchid": "test-merchant", "merchant_serial_no": "test-serial",
			"merchant_private_key":    string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
			"wechatpay_public_key":    string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})),
			"wechatpay_public_key_id": publicID, "api_v3_key": "12345678901234567890123456789012",
		},
	}}
	// HTTP传输仅接受Native下单目标；其他请求直接失败而不是透传出网。
	// 订单号不合厂商约束时模拟上游拒绝，因此旧代码会让真实入口回归红灯。
	// 外部错误只包含长度，测试日志不打印私钥或完整带签名请求。
	// JSON从SDK实际发送的Body读取，不从本地订单倒推上游参数。
	// 真实SDK会使用商户私钥生成Authorization，桩只检查签名头已存在。
	// SDK的密码学验签继续运行，没有使用跳过验证的测试选项。
	// 平台响应时间戳使用当前时刻，避免SDK时间窗造成无关失败。
	// 平台公钥ID、随机串、时间戳、签名四个头按官方协议共同提供。
	// 仅替换网络出口，不替换CreateOfficialPayment或订单Insert函数。
	oldTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	observed := make([]string, 0, 4)
	validTrade := regexp.MustCompile(`^[A-Za-z0-9_\-|*]{6,32}$`)
	http.DefaultTransport = officialPaymentTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method != "POST" || request.URL.String() != "https://api.mch.weixin.qq.com/v3/pay/transactions/native" {
			return nil, fmt.Errorf("unexpected payment target")
		}
		var payload struct {
			TradeNo string `json:"out_trade_no"`
			Amount  struct {
				Total int64 `json:"total"`
			} `json:"amount"`
		}
		if err := common.DecodeJson(request.Body, &payload); err != nil {
			return nil, err
		}
		observed = append(observed, payload.TradeNo)
		if !validTrade.MatchString(payload.TradeNo) {
			return nil, fmt.Errorf("PARAM_ERROR: trade number length %d exceeds WeChat contract", len(payload.TradeNo))
		}
		assert.EqualValues(t, 1000, payload.Amount.Total)
		assert.NotEmpty(t, request.Header.Get("Authorization"))
		// 响应签名绑定原始正文，不能先签名再改变二维码字段。
		// 固定测试二维码只是付款交互，不能当作支付到账凭据。
		body := `{"code_url":"weixin://wxpay/bizpayurl?pr=local-test"}`
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := "subscription-response"
		digest := sha256.Sum256([]byte(timestamp + "\n" + nonce + "\n" + body + "\n"))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if err != nil {
			return nil, err
		}
		response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}
		response.Header.Set("Content-Type", "application/json")
		response.Header.Set("Wechatpay-Serial", publicID)
		response.Header.Set("Wechatpay-Timestamp", timestamp)
		response.Header.Set("Wechatpay-Nonce", nonce)
		response.Header.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(signature))
		return response, nil
	})
	plan := model.SubscriptionPlan{Title: "订阅订单号回归", PriceAmount: 10, Enabled: true, DurationUnit: "month", DurationValue: 1}
	require.NoError(t, db.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })
	// 账号只使用隔离测试ID，当前钱包余额不会被订阅购买预扣。
	// 两种ID宽度都各请求两次，覆盖用户增长与同用户重复下单。
	seen := map[string]bool{}
	for _, userID := range []int{7, 2147483647} {
		user := model.User{Id: userID, Username: fmt.Sprintf("subscription-%d", userID), AffCode: fmt.Sprintf("sub%d", userID), Group: "default", Quota: 700, Status: common.UserStatusEnabled}
		require.NoError(t, db.Create(&user).Error)
		for range 2 {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest("POST", "/api/subscription/official/pay", strings.NewReader(fmt.Sprintf(`{"plan_id":%d,"payment_method":"wechat-subscription-test"}`, plan.Id)))
			ctx.Request.Header.Set("Content-Type", "application/json")
			ctx.Set("id", userID)
			SubscriptionRequestOfficialPay(ctx)
			var reply struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
				Data    struct {
					TradeNo     string `json:"trade_no"`
					PaymentKind string `json:"payment_kind"`
					QRCode      string `json:"qr_code"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &reply))
			require.True(t, reply.Success, "payment did not start: %s", reply.Message)
			require.Equal(t, "qr", reply.Data.PaymentKind)
			require.NotEmpty(t, reply.Data.QRCode)
			tradeNo := reply.Data.TradeNo
			require.True(t, strings.HasPrefix(tradeNo, "SUBUSR"))
			require.True(t, validTrade.MatchString(tradeNo))
			require.False(t, seen[tradeNo], "each new order needs its own merchant trade number")
			seen[tradeNo] = true
			// 回传、数据库和上游必须使用同一单号，否则成功通知无法认领原单。
			// 完整交易号参与唯一索引和通知定位，不从其中截取用户ID。
			// 两种ID宽度使用同一渠道，避免通过换商户掩盖交易号重复。
			// 查询绑定参数沿用生产数据库方式，不拼接刚生成的交易号。
			// 订单依然pending，获得二维码不应开通套餐或提前记成功。
			var order model.SubscriptionOrder
			require.NoError(t, db.Where("trade_no = ?", tradeNo).First(&order).Error)
			assert.Equal(t, userID, order.UserId)
			assert.Equal(t, plan.Id, order.PlanId)
			assert.Equal(t, common.TopUpStatusPending, order.Status)
			assert.Equal(t, 10.0, order.Money)
			assert.Equal(t, tradeNo, observed[len(observed)-1])
		}
		// 本入口从不扣钱包；付款成功必须留给原有已验签回调事务。
		require.NoError(t, db.First(&user, userID).Error)
		assert.Equal(t, 700, user.Quota)
	}
	require.Len(t, observed, 4)
	var count int64
	require.NoError(t, db.Model(&model.UserSubscription{}).Count(&count).Error)
	assert.Zero(t, count)
}
