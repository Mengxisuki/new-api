package controller

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 同一交易合同在三种真实数据库验证，未配置显式跳过。
// 新装与带历史订单升级都执行两次迁移，不回填猜测的订阅权益。
// 测试只使用隔离本地数据库，不连接真实商户或生产钱包。
// 隔离数据库由既有夹具创建，
// 所有交易都使用虚构用户和订单。
// 每种数据库都执行相同的行为断言，
// 不能用SQLite通过替代另两种方言。
// 升级先移除新增权益关联列，
// 再插入没有这些字段的历史订阅单。
// 迁移不能据当前套餐回填历史权益，
// 否则已改价套餐会导致错误退款。
// 连续迁移之后再次读取原订单，
// 旧交易号和实付金额应仍可追溯。
// 钱包用固定冻结额度建立回收基线，
// 验证的是实际用户表余额变化。
// 重复准备使用不同输入原因，
// 仍应返回原退款号和原审批原因。
// 这能保护同键重试不会篡改审计信息，
// 也不会第二次扣掉钱包余额。
// 先写处理中再写成功，
// 随后用迟到处理中结果尝试覆盖终态。
// 实际状态必须保持成功，
// 不能让后台并发查询回退结果。
// 再次调用真实支付完成函数，
// 验证旧支付通知不重新补回退款额度。
// 余额不足用另一张独立订单测试，
// 拒绝后不应留下任何可提交微信的退款记录。
// 订阅通过真实付款完成路径购买两次，
// 不是直接手工填一个预期增量字段。
// 两笔订单应关联同一续期实例，
// 每笔仍保存各自新增的实际时长。
// 退款第二单后首单继续有效，
// 这个断言保护续费不会误删原权益。
// 从钱包入口尝试退款订阅镜像，
// 必须在任何资金动作前拒绝。
// 列表总数排除订阅镜像，
// 否则管理员会把同一收款看作两笔订单。
// 代充值使用已付且未完成的办理状态，
// 最终退款清除在线密文但保留原收款时间。
// 最后再次迁移验证终态不会被初始化覆盖。
func TestPaymentRefundDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		for _, upgrade := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/upgrade=%v", dialect, upgrade), func(t *testing.T) {
				dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
				if dialect != "sqlite" && dsn == "" {
					t.Skip("isolated DSN is not configured")
				}
				db := modelManagementDB(t, dialect, dsn)
				tables := []any{&model.TopUp{}, &model.SubscriptionOrder{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.PaymentRefund{}, &model.InvoiceRequest{}, &model.AccountRechargeOrder{}, &model.AccountRechargeEvent{}, &model.Log{}}
				require.NoError(t, db.AutoMigrate(tables...))
				if upgrade {
					// 删除新增列模拟上线前结构，历史金额和交易号必须原样保留。
					require.NoError(t, db.Migrator().DropColumn(&model.SubscriptionOrder{}, "FulfilledSubscriptionID"))
					require.NoError(t, db.Migrator().DropColumn(&model.SubscriptionOrder{}, "GrantedSeconds"))
					require.NoError(t, db.Table("subscription_orders").Create(map[string]any{"user_id": 1, "plan_id": 1, "money": 10, "trade_no": "legacy", "payment_provider": model.PaymentProviderWechat, "payment_method": "wx", "status": "success"}).Error)
				}
				for range 2 {
					require.NoError(t, db.AutoMigrate(tables...))
				}
				buyer := model.User{Id: 1, Username: "refund-buyer", Status: common.UserStatusEnabled, Quota: 1000, Group: "default"}
				require.NoError(t, db.Create(&buyer).Error)
				if upgrade {
					_, err := model.PreparePaymentRefund("subscription", "legacy", "历史退款", 100)
					require.ErrorIs(t, err, model.ErrRefundRights)
				}
				// 钱包必须回收冻结额度，重试不能重复扣减或生成新退款号。
				top := model.TopUp{UserId: 1, TradeNo: "wallet-refund", Money: 10, CreditedQuota: 400, PaymentProvider: model.PaymentProviderWechat, PaymentMethod: "wx", Status: common.TopUpStatusSuccess}
				require.NoError(t, db.Create(&top).Error)
				refund, err := model.PreparePaymentRefund("topup", top.TradeNo, "取消充值", 100)
				require.NoError(t, err)
				require.Equal(t, "prepared", refund.Status)
				require.NoError(t, db.First(&buyer, 1).Error)
				assert.Equal(t, 600, buyer.Quota)
				retry, err := model.PreparePaymentRefund("topup", top.TradeNo, "不同原因", 100)
				require.NoError(t, err)
				assert.Equal(t, refund.RefundNo, retry.RefundNo)
				assert.Equal(t, "取消充值", retry.Reason)
				require.NoError(t, model.ApplyPaymentRefundResult(refund.ID, "processing", "wx-refund", common.GetTimestamp()))
				require.NoError(t, model.ApplyPaymentRefundResult(refund.ID, "succeeded", "wx-refund", common.GetTimestamp()))
				require.NoError(t, model.ApplyPaymentRefundResult(refund.ID, "processing", "wx-refund", common.GetTimestamp()))
				retry, err = model.GetPaymentRefund("topup", top.TradeNo)
				require.NoError(t, err)
				assert.Equal(t, "succeeded", retry.Status) // 迟到的处理中响应不能回退终态。
				require.NoError(t, model.CompleteOfficialTopUp(top.TradeNo, model.PaymentProviderWechat, "wx", decimal.NewFromInt(10), ""))
				require.NoError(t, db.First(&buyer, 1).Error)
				assert.Equal(t, 600, buyer.Quota) // 迟到支付回调不重新补回已退款额度。
				tooLarge := model.TopUp{UserId: 1, TradeNo: "insufficient", Money: 20, CreditedQuota: 601, PaymentProvider: model.PaymentProviderWechat, PaymentMethod: "wx", Status: "success"}
				require.NoError(t, db.Create(&tooLarge).Error)
				_, err = model.PreparePaymentRefund("topup", tooLarge.TradeNo, "余额不足", 100)
				require.ErrorIs(t, err, model.ErrRechargeBalance)
				missing, err := model.GetPaymentRefund("topup", tooLarge.TradeNo)
				require.NoError(t, err)
				assert.Nil(t, missing) // 拒绝必须不留下可向微信退款的记录。
				// 真实付款完成两次，订单快照分别记录首次和续费实际增加的周期。
				plan := model.SubscriptionPlan{Title: "Refund plan", PriceAmount: 10, DurationUnit: "day", DurationValue: 10, Enabled: true, TotalAmount: 10000}
				require.NoError(t, db.Create(&plan).Error)
				for _, trade := range []string{"sub-one", "sub-two"} {
					order := model.SubscriptionOrder{UserId: 1, PlanId: plan.Id, Money: 10, TradeNo: trade, PaymentProvider: model.PaymentProviderWechat, PaymentMethod: "wx", Status: "pending"}
					require.NoError(t, order.Insert())
					require.NoError(t, model.CompleteOfficialSubscriptionOrder(trade, "", model.PaymentProviderWechat, "wx", decimal.NewFromInt(10)))
				}
				var first, second model.SubscriptionOrder
				require.NoError(t, db.Where("trade_no = ?", "sub-one").First(&first).Error)
				require.NoError(t, db.Where("trade_no = ?", "sub-two").First(&second).Error)
				assert.Equal(t, first.FulfilledSubscriptionID, second.FulfilledSubscriptionID)
				assert.EqualValues(t, 10*86400, second.GrantedSeconds)
				var sub model.UserSubscription
				require.NoError(t, db.First(&sub, second.FulfilledSubscriptionID).Error)
				end := sub.EndTime
				refund, err = model.PreparePaymentRefund("subscription", "sub-two", "取消续费", 100)
				require.NoError(t, err)
				require.NoError(t, db.First(&sub, second.FulfilledSubscriptionID).Error)
				assert.Equal(t, end-10*86400, sub.EndTime)
				assert.Equal(t, "active", sub.Status) // 原本仍有效的首购权益不能被删除。
				require.NoError(t, model.ApplyPaymentRefundResult(refund.ID, "succeeded", "wx-sub-refund", common.GetTimestamp()))
				_, err = model.PreparePaymentRefund("topup", "sub-two", "镜像绕过", 100)
				require.ErrorIs(t, err, model.ErrRefundInvalid)
				// 列表不泄露内部字段，并且不把两笔订阅镜像算作钱包订单。
				rows, total, err := model.ListPaymentOrders("topup", "", "", 1)
				require.NoError(t, err)
				assert.EqualValues(t, 2, total)
				assert.Len(t, rows, 2)
				// 代充值必须先停止履约，成功后清除在线资料并保留原收款时间。
				recharge := model.AccountRechargeOrder{UserID: 1, TradeNo: "recharge-refund", RequestID: "recharge-refund", MoneyCents: 1000, PaymentProvider: "wechat", PaymentMethod: "wx", PaymentStatus: "paid", Status: "pending", Secret: "encrypted-test", PaidAt: 100, Version: 1}
				require.NoError(t, db.Create(&recharge).Error)
				refund, err = model.PreparePaymentRefund("recharge", recharge.TradeNo, "取消办理", 100)
				require.NoError(t, err)
				require.NoError(t, model.ApplyPaymentRefundResult(refund.ID, "succeeded", "wx-recharge-refund", common.GetTimestamp()))
				require.NoError(t, db.First(&recharge, recharge.ID).Error)
				assert.Equal(t, "refunded", recharge.PaymentStatus)
				assert.Empty(t, recharge.Secret)
				assert.EqualValues(t, 100, recharge.PaidAt)
				require.NoError(t, db.AutoMigrate(tables...)) // 退款后再次迁移不能重置状态或索引。
			})
		}
	}
}

// 微信HTTP边界模拟平台，真实SDK仍执行请求签名与响应RSA验签。
// 仅替换外部网络；权益回收、原单恢复、金额核对均走实际实现。
// 故意在受理后断网，证明重试沿用原退款号且只扣一次权益。
// 平台响应由测试RSA密钥签名，
// 生产SDK继续完成真正的验签过程。
// 网络替身只接受官方退款目标，
// 其他HTTP请求绝不会意外转发到真实商户。
// 请求中的交易号与金额来自实际数据库，
// 断言不调用生产代码计算预期退款额。
// 创建响应成功后故意中断查询，
// 模拟平台已受理而本地尚未确认的常见故障。
// 第一次调用应向管理员返回错误，
// 同时保留已回收权益和原准备记录。
// 恢复调用不能更换商户退款号，
// 重复提交参数应保持同一整单金额。
// 查询得到处理中后，
// 后续恢复只能查询已有退款。
// 少一分的退款响应仍有合法签名，
// 所以金额校验不能由签名校验替代。
// 篡改签名时保持原交易内容，
// 验证不是仅检查字段相同就认可成功。
// 两类坏响应都不得推进为成功，
// 本地权益也不能因此重复回收或补回。
// 最终合法成功响应提供实际退款时间，
// 时间早于查询时刻以保护财务日期合同。
// 最后检查钱包只减少一次冻结额度，
// 不是只断言接口返回了成功状态。
// 测试完成恢复全局HTTP传输和商户设置，
// 其他测试不能继承这套虚构密钥。
func TestPaymentRefundWechatRecovery(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.SubscriptionOrder{}, &model.PaymentRefund{}, &model.InvoiceRequest{}))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "wx-refund", Quota: 900, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.TopUp{UserId: 1, TradeNo: "wx-order-test", Money: 1.23, CreditedQuota: 400, PaymentMethod: "wx-refund", PaymentProvider: model.PaymentProviderWechat, Status: "success"}).Error)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	priv, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	settings := operation_setting.GetPaymentSetting()
	old := settings.OfficialChannels
	t.Cleanup(func() { settings.OfficialChannels = old })
	settings.OfficialChannels = []operation_setting.OfficialPaymentChannel{{ID: "wx-refund", Name: "测试退款", ChannelType: "wechat", InteractionMode: "qr", Enabled: true, Config: map[string]string{
		"appid": "wx-test", "mchid": "test-merchant", "merchant_serial_no": "test-serial", "api_v3_key": "12345678901234567890123456789012",
		"merchant_private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv})), "wechatpay_public_key": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})), "wechatpay_public_key_id": "PUB_KEY_ID_REFUND_TEST",
	}}}
	oldTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	var numbers []string
	status, badSignature, badAmount, failQuery := "PROCESSING", false, false, true
	var refundNo string
	successTime := time.Now().Add(-time.Hour).Truncate(time.Second)
	http.DefaultTransport = officialPaymentTestTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "api.mch.weixin.qq.com", req.URL.Host) // 未匹配请求不会透传真实网络。
		require.Contains(t, req.Header.Get("Authorization"), "WECHATPAY2-SHA256-RSA2048")
		if req.Method == "POST" {
			require.Equal(t, "/v3/refund/domestic/refunds", req.URL.Path)
			var body struct {
				Trade  string `json:"out_trade_no"`
				Refund string `json:"out_refund_no"`
				Amount struct {
					Refund   int64  `json:"refund"`
					Total    int64  `json:"total"`
					Currency string `json:"currency"`
				} `json:"amount"`
			}
			require.NoError(t, common.DecodeJson(req.Body, &body))
			assert.Equal(t, "wx-order-test", body.Trade)
			assert.EqualValues(t, 123, body.Amount.Total)
			assert.EqualValues(t, 123, body.Amount.Refund)
			assert.Equal(t, "CNY", body.Amount.Currency)
			numbers = append(numbers, body.Refund)
			refundNo = body.Refund
		} else {
			require.Equal(t, "GET", req.Method)
			require.Equal(t, "/v3/refund/domestic/refunds/"+refundNo, req.URL.Path)
			if failQuery {
				return nil, errors.New("test connection lost after acceptance")
			}
		}
		amount := 123
		if badAmount {
			amount = 122
		}
		body := fmt.Sprintf(`{"refund_id":"wx-refund-id","out_trade_no":"wx-order-test","out_refund_no":%q,"status":%q,"success_time":%q,"amount":{"total":123,"refund":%d,"currency":"CNY"}}`, refundNo, status, successTime.Format(time.RFC3339), amount)
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := "refund-response"
		digest := sha256.Sum256([]byte(timestamp + "\n" + nonce + "\n" + body + "\n"))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		require.NoError(t, err)
		if badSignature {
			signature[0] ^= 1
		}
		return &http.Response{StatusCode: 200, Request: req, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{
			"Content-Type": []string{"application/json"}, "Wechatpay-Serial": []string{"PUB_KEY_ID_REFUND_TEST"}, "Wechatpay-Timestamp": []string{timestamp}, "Wechatpay-Nonce": []string{nonce}, "Wechatpay-Signature": []string{base64.StdEncoding.EncodeToString(signature)},
		}}, nil
	})
	_, err = service.RefundPaymentOrder(context.Background(), "topup", "wx-order-test", "退款测试", 100, false)
	require.Error(t, err)
	pending, err := model.GetPaymentRefund("topup", "wx-order-test")
	require.NoError(t, err)
	assert.Equal(t, "prepared", pending.Status)
	// 查询恢复后仍使用同一申请身份，资金不因第一次断网重复扣回。
	failQuery = false
	result, err := service.RefundPaymentOrder(context.Background(), "topup", "wx-order-test", "", 100, true)
	require.NoError(t, err)
	assert.Equal(t, "processing", result.Status)
	require.Len(t, numbers, 2)
	assert.Equal(t, numbers[0], numbers[1])
	badAmount = true
	_, err = service.RefundPaymentOrder(context.Background(), "topup", "wx-order-test", "", 100, true)
	require.ErrorIs(t, err, service.ErrOfficialPaymentResponse)
	badAmount = false
	badSignature = true
	_, err = service.RefundPaymentOrder(context.Background(), "topup", "wx-order-test", "", 100, true)
	require.Error(t, err) // SDK必须拒绝损坏签名，不向账本写成功。
	badSignature = false
	status = "SUCCESS"
	result, err = service.RefundPaymentOrder(context.Background(), "topup", "wx-order-test", "", 100, true)
	require.NoError(t, err)
	assert.Equal(t, "succeeded", result.Status)
	assert.Equal(t, successTime.Unix(), result.CompletedAt) // 退款日以微信实际成功时间为准。
	require.Len(t, numbers, 2)                              // processing之后只查询，绝不再次提交新退款。
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, 500, user.Quota)
}

// 使用真实登录会话和资源目录，确认普通用户、财务及只读管理员不能退款。
// 本人授信接口即使带他人user_id也只返回登录身份的数据。
// 用户角色从持久用户模型读取，
// 请求使用生产服务签发的真实登录凭据。
// 中间件决定是否允许进入处理器，
// 不是在测试里直接设置一个管理员标志。
// 普通用户与财务角色都尝试读取全平台订单，
// 两者必须被管理员门槛拒绝。
// 管理员默认有订单读取和退款能力，
// 随后单独撤销其退款能力。
// 撤销后仍可以保留读取权限，
// 写请求必须在访问业务订单之前被拒绝。
// 使用不存在的订单号作为拒绝用例，
// 避免把订单未找到误判为权限校验成功。
// 每个用户单独配置自己的授信，
// 本人接口返回值应始终来自当前会话身份。
// 请求故意附带另一个用户编号，
// 该参数不得改变授信归属。
// 测试不通过前端菜单隐藏证明安全性，
// 而是直接请求真实受保护的HTTP路由。
// 资源目录使用与应用启动一致的注册方式，
// 角色基线变化不能被硬编码mock掩盖。
// 所有账号存在于隔离数据库，
// 没有授予任何真实用户新的权限。
func TestPaymentOrderPermissions(t *testing.T) {
	_, _ = setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}, &model.TopUp{}, &model.SubscriptionOrder{}, &model.PaymentRefund{}, &model.CreditAccount{}))
	oldMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = oldMaster })
	require.NoError(t, authz.Init(model.DB))
	engine := gin.New()
	engine.GET("/api/orders", middleware.AdminAuth(), RechargeSessionOnly, middleware.RequirePermission(authz.OrdersRead), GetPaymentOrders)
	engine.POST("/api/orders/refund", middleware.AdminAuth(), RechargeSessionOnly, middleware.RequirePermission(authz.OrdersRefund), RefundPaymentOrder)
	engine.GET("/api/user/credit", middleware.UserAuth(), RechargeSessionOnly, GetSelfCreditAccount)
	for _, role := range []int{common.RoleCommonUser, common.RoleFinanceUser, common.RoleAdminUser} {
		user := model.User{Username: fmt.Sprintf("order-role-%d", role), AffCode: fmt.Sprintf("order-role-%d", role), Role: role, Status: common.UserStatusEnabled, AuthVersion: 1}
		require.NoError(t, model.DB.Create(&user).Error)
		bundle, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "orders-test")
		require.NoError(t, err)
		request := func(method, path, body string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			return response
		}
		response := request("GET", "/api/orders", "")
		if role == common.RoleAdminUser {
			require.Equal(t, 200, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), `"success":true`)
			require.NoError(t, authz.SetUserPermissions(user.Id, authz.PermissionsMap{authz.ResourceOrders: {"read": true, "refund": false}}))
		} else {
			require.NotEqual(t, 200, response.Code)
		}
		denied := request("POST", "/api/orders/refund", `{"module":"topup","trade_no":"missing","reason":"test"}`)
		require.NotEqual(t, 200, denied.Code) // 不具备退款权限时必须在业务调用前拒绝。
		require.NoError(t, model.SaveCreditAccount(model.CreditAccount{UserID: user.Id, Enabled: true, LimitCents: 1000, TopupEnabled: true}, 100))
		mine := request("GET", "/api/user/credit?user_id=99999", "")
		require.Equal(t, 200, mine.Code, mine.Body.String())
		assert.Contains(t, mine.Body.String(), fmt.Sprintf(`"user_id":%d`, user.Id))
	}
}

// 钱包的缓存预扣保护尚未批量落库的模型消费，不能只看DB余额。
// 注入退款记录写入失败，验证事务和Redis预留一起安全回滚。
// 模型消费可能已经在Redis扣费，
// 数据库批量写入此时还没有执行。
// 测试直接建立这一缓存领先于数据库的状态，
// 验证退款不会只看持久余额就放行。
// 缓存不足时不应产生退款意图，
// 更不能发出任何微信请求。
// 随后恢复足额缓存并注入持久写入故障，
// 故障发生在真实退款事务的创建阶段。
// 钱包扣减与退款记录创建必须共同回滚，
// 不能留下有扣款却无退款身份的半次操作。
// 缓存预留需重新核对原单后才能释放，
// 测试同时检查Redis和数据库恢复到基线。
// 移除故障后使用相同订单重新执行，
// 应能够完成一次合法回收。
// 再次重复准备调用只恢复原记录，
// 缓存和数据库不能累计第二次扣款。
// Redis替身只隔离外部存储边界，
// 生产Lua脚本和用户缓存水合照常运行。
// 每次测试都恢复原Redis客户端与启用标志，
// 避免后续用例读取错误环境。
// 缓存内容不被作为退款成功凭证，
// 现金状态仍必须由微信签名查询确认。
func TestPaymentRefundWalletCacheRollback(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.SubscriptionOrder{}, &model.PaymentRefund{}))
	server := miniredis.RunT(t)
	oldRedis, oldClient := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, common.RDB.Close()); common.RedisEnabled, common.RDB = oldRedis, oldClient })
	user := model.User{Id: 1, Username: "refund-cache", Quota: 1000, Status: 1, AuthVersion: 1}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.TopUp{UserId: 1, TradeNo: "refund-cache", Money: 10, CreditedQuota: 600, PaymentProvider: model.PaymentProviderWechat, Status: "success"}).Error)
	_, err := model.GetUserCache(1)
	require.NoError(t, err)
	require.NoError(t, common.RDB.HIncrBy(context.Background(), "user:1", "Quota", -500).Err())
	_, err = model.PreparePaymentRefund("topup", "refund-cache", "取消充值", 100)
	require.ErrorIs(t, err, model.ErrRechargeBalance)
	require.NoError(t, common.RDB.HIncrBy(context.Background(), "user:1", "Quota", 500).Err())
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("refund:fail", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.PaymentRefund); ok {
			tx.AddError(errors.New("test refund persistence failed"))
		}
	}))
	_, err = model.PreparePaymentRefund("topup", "refund-cache", "取消充值", 100)
	require.Error(t, err)
	assert.Equal(t, "1000", server.HGet("user:1", "Quota")) // 明确回滚后才能释放预留。
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, 1000, user.Quota)
	require.NoError(t, db.Callback().Create().Remove("refund:fail"))
	_, err = model.PreparePaymentRefund("topup", "refund-cache", "取消充值", 100)
	require.NoError(t, err)
	_, err = model.PreparePaymentRefund("topup", "refund-cache", "取消充值", 100)
	require.NoError(t, err)
	assert.Equal(t, "400", server.HGet("user:1", "Quota")) // 同单重试不会第二次扣缓存。
}
