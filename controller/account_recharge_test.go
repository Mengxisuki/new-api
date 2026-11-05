package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
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

// 三库都运行真实迁移、事务、加密及订单状态流转，
// 不模拟 ORM行为。
// 升级先建立上一版本已有的钱包/用户数据，
// 再增加独立代充值表。
// 未配置方言必须明确跳过，
// 不能把单库通过解释为三库兼容。
// 每个数据库方言使用独立临时数据库，
// 清理只针对本次创建的测试资源。
// MySQL 和 PostgreSQL只允许回环地址，
// 测试不能指向现有生产库。
// 矩阵同时覆盖新建和升级，
// 旧钱包订单先入库再执行新增表迁移。
// 两次重复迁移后重新读取旧金额及冻结额度，
// 验证没有隐式重置。
// 用户钱包余额显式初始化，
// 不能依赖测试运行顺序或默认赠送额度。
// 商品先允许余额、再关闭、再恢复，
// 以真实行为校验管理员开关。
// 同一请求键恢复同单，
// 不同摘要复用请求键必须返回冲突。
// 重复余额支付最终余额只减少一份商品额度，
// 断言账本而非只看响应。
// 加密测试使用虚构会话，
// 并用另一订单号解密证明关联数据保护。
// 两个业务员并发领取同一版本，
// 必须恰好一人成功。
// 转派后验证旧处理人无法再读取，
// 避免仅测试前端隐藏按钮。
// 敏感读取后再获取最新版本，
// 验证读取审计与业务版本确实接线。
// 补资料动作清除旧密文，
// 用户重交仍保持原处理人。
// 完成后既不能重交资料也不能直接退款，
// 保护终态不可重开。
// 序列化摘要不得含原始 session或完整邮箱，
// 防止DTO 标签遗漏。
// 微信回调使用现有 SDK的真实签名，
// 错误金额不能通过验签后绕过。
// 同一成功通知重复发送，
// 平台钱包不得被外部账号充值增加。
// 网关退款必须提供非空参考号，
// 不能只凭管理员点击变成已退款。
// 退款确认重复请求保持幂等，
// 不创建第二次资金结果。
// 测试最后下架商品再迁移，
// 验证布尔false不被迁移默认值覆盖。
// 数据库版本由实际引擎查询输出，
// 不能把驱动名称当作验证版本。
// 没有调用真实商户或原厂账户，
// 测试通过不代表生产实付已验收。
// 升级前的真实表结构；不包含本轮新增展示字段。
// 保留原字段、索引与约束，迁移后核对旧订单不被改写。
type legacyAccountRechargeProduct struct {
	ID                 int    `json:"id"`
	Title              string `json:"title" gorm:"size:128;not null"` // 面向用户的套餐名称。
	Description        string `json:"description" gorm:"type:text"`   // 账户条件与售后等购买说明。
	PriceCents         int64  `json:"price_cents" gorm:"not null"`    // 始终为人民币分。
	Enabled            bool   `json:"enabled"`                        // 零值不上架，禁止迁移默认自动售卖。
	AllowWalletPayment bool   `json:"allow_wallet_payment"`           // 单商品控制余额付款。
	SortOrder          int    `json:"sort_order"`
	Version            int    `json:"version" gorm:"not null"` // 编辑和报价的乐观版本。
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
}

func (legacyAccountRechargeProduct) TableName() string { return "account_recharge_products" }

// 升级前的真实表结构；不包含本轮新增展示字段。
// 保留原字段、索引与约束，迁移后核对旧订单不被改写。
type legacyAccountRechargeOrder struct {
	ID                int    `json:"id"`
	UserID            int    `json:"user_id" gorm:"index;uniqueIndex:idx_recharge_request,priority:1"`
	RequestID         string `json:"-" gorm:"size:36;uniqueIndex:idx_recharge_request,priority:2"`
	RequestHash       string `json:"-" gorm:"size:64"` // 同一幂等键不能换商品或付款方式。
	ProductID         int    `json:"product_id" gorm:"index"`
	Title             string `json:"title" gorm:"size:128"`        // 购买时的商品名称。
	Description       string `json:"description" gorm:"type:text"` // 购买时的规则快照。
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
	RefundRef         string `json:"refund_ref,omitempty" gorm:"size:128"` // 人工核实微信原路退款后的凭证号。
	WalletSyncPending bool   `json:"wallet_sync_pending"`                  // 退款已入库但缓存尚未同步时，管理员可恢复原单。
	RefundRequested   bool   `json:"refund_requested"`
	Version           int    `json:"version" gorm:"not null"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
	PaidAt            int64  `json:"paid_at"`
	CompletedAt       int64  `json:"completed_at"`
	ExpiresAt         int64  `json:"expires_at"`
}

func (legacyAccountRechargeOrder) TableName() string { return "account_recharge_orders" }

func TestRechargeDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		for _, upgrade := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/upgrade=%v", dialect, upgrade), func(t *testing.T) {
				dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
				if dialect != "sqlite" && dsn == "" {
					t.Skip("test database DSN is not configured")
				}
				db := modelManagementDB(t, dialect, dsn)
				require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.InvoiceRequest{}))
				if upgrade {
					// 旧表升级保留说明、关闭状态、唯一订单号以及历史金额。
					// 新字段留空，不回填当前商品配置冒充购买快照。
					require.NoError(t, db.AutoMigrate(&legacyAccountRechargeProduct{}, &legacyAccountRechargeOrder{}))
					require.NoError(t, db.Create(&legacyAccountRechargeProduct{ID: 900, Title: "Legacy plan", Description: "Legacy terms", PriceCents: 100, Version: 1}).Error)
					require.NoError(t, db.Create(&legacyAccountRechargeOrder{ID: 900, UserID: 999, RequestID: "legacy", Title: "Legacy plan", Description: "Legacy purchase terms", MoneyCents: 100, TradeNo: "RCHUSR-legacy", Version: 1}).Error)
					require.NoError(t, db.Create(&model.TopUp{UserId: 999, TradeNo: "legacy-recharge-upgrade", Money: 12, CreditedQuota: 700, Status: common.TopUpStatusSuccess}).Error)
				}
				tables := []any{&model.CreditAccount{}, &model.CreditEntry{}, &model.AccountRechargeProduct{}, &model.AccountRechargeOrder{}, &model.AccountRechargeEvent{}}
				for range 2 {
					require.NoError(t, db.AutoMigrate(tables...))
				}
				if upgrade {
					var legacy model.TopUp
					require.NoError(t, db.Where("trade_no = ?", "legacy-recharge-upgrade").First(&legacy).Error)
					assert.Equal(t, 700, legacy.CreditedQuota)
					assert.Equal(t, 12.0, legacy.Money)
					var oldProduct model.AccountRechargeProduct
					var oldOrder model.AccountRechargeOrder
					require.NoError(t, db.First(&oldProduct, 900).Error)
					require.NoError(t, db.First(&oldOrder, 900).Error)
					assert.Equal(t, "Legacy terms", oldProduct.Description)
					assert.False(t, oldProduct.Enabled)
					assert.Empty(t, oldProduct.Summary)
					assert.Equal(t, "Legacy purchase terms", oldOrder.Description)
					assert.Empty(t, oldOrder.Icon)
					assert.Equal(t, int64(100), oldOrder.MoneyCents)
					duplicate := oldOrder
					duplicate.ID, duplicate.RequestID = 0, "legacy-duplicate"
					require.Error(t, db.Create(&duplicate).Error) // 迁移不能破坏订单号唯一约束。
				}
				// 独立测试密钥只存于本进程环境，
				// 资料使用不可登录的fixture。
				t.Setenv("OAK_RECHARGE_DATA_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
				buyer := model.User{Username: "recharge-buyer", AffCode: "rb", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1, Quota: 2000}
				sales := model.User{Username: "recharge-sales", AffCode: "rs", Role: common.RoleSalesUser, Status: common.UserStatusEnabled, AuthVersion: 1}
				other := model.User{Username: "recharge-other", AffCode: "ro", Role: common.RoleSalesUser, Status: common.UserStatusEnabled, AuthVersion: 1}
				require.NoError(t, db.Create(&buyer).Error)
				require.NoError(t, db.Create(&sales).Error)
				require.NoError(t, db.Create(&other).Error)
				p := model.AccountRechargeProduct{Title: "ChatGPT monthly", Description: "Test account requirements", Summary: " Plus benefits ", Provider: " OpenAI ", Period: "1 month", DeliveryTime: "24 hours", Icon: "OpenAI", PriceCents: 15000, Enabled: true, AllowWalletPayment: true}
				require.NoError(t, model.SaveAccountRechargeProduct(&p))
				order := &model.AccountRechargeOrder{UserID: buyer.Id, RequestID: "request-wallet", RequestHash: "same", ProductID: p.ID, BaseCents: p.PriceCents, MoneyCents: p.PriceCents, TradeNo: "RCHUSR-wallet", PaymentProvider: "wallet", PaymentMethod: "wallet", WalletQuota: 1000, WalletRate: "7.3"}
				order, err := model.CreateAccountRechargeOrder(order, p.Version, nil)
				require.NoError(t, err)
				duplicate, err := model.CreateAccountRechargeOrder(&model.AccountRechargeOrder{UserID: buyer.Id, RequestID: "request-wallet", RequestHash: "same"}, p.Version, nil)
				require.NoError(t, err)
				assert.Equal(t, order.ID, duplicate.ID)
				_, err = model.CreateAccountRechargeOrder(&model.AccountRechargeOrder{UserID: buyer.Id, RequestID: "request-wallet", RequestHash: "changed"}, p.Version, nil)
				require.ErrorIs(t, err, model.ErrRechargeConflict)
				// 商品编辑与清空必须真正落库，原订单仍保持创建时内容。
				// 查询数据库断言，避免只验证函数修改了内存对象。
				var stored model.AccountRechargeProduct
				require.NoError(t, db.First(&stored, p.ID).Error)
				assert.Equal(t, "Plus benefits", stored.Summary)
				assert.Equal(t, "OpenAI", stored.Provider)
				p.Summary, p.Provider, p.Period, p.DeliveryTime, p.Icon = "", "", "", "", ""
				require.NoError(t, model.SaveAccountRechargeProduct(&p))
				require.NoError(t, db.First(&stored, p.ID).Error)
				assert.Empty(t, stored.Summary)
				assert.Empty(t, stored.Icon)
				snapshot, err := model.GetAccountRechargeOrder(order.ID, buyer.Id)
				require.NoError(t, err)
				assert.Equal(t, "Plus benefits", snapshot.Summary)
				assert.Equal(t, "OpenAI", snapshot.Provider)
				assert.Equal(t, "1 month", snapshot.Period)
				assert.Equal(t, "24 hours", snapshot.DeliveryTime)
				assert.Equal(t, "OpenAI", snapshot.Icon)
				// 入口拒绝超长展示字段，不能依赖某个数据库的截断行为。
				invalid := p
				invalid.Summary = strings.Repeat("中", 501)
				require.ErrorIs(t, model.SaveAccountRechargeProduct(&invalid), model.ErrRechargeInvalid)
				// 余额开关在实际扣款时再验证，
				// 不只相信下单前的商品页面。
				p.AllowWalletPayment = false
				require.NoError(t, model.SaveAccountRechargeProduct(&p))
				require.ErrorIs(t, model.PayAccountRechargeWallet(order.ID, buyer.Id), model.ErrRechargeUnavailable)
				p.AllowWalletPayment = true
				require.NoError(t, model.SaveAccountRechargeProduct(&p))
				require.NoError(t, model.PayAccountRechargeWallet(order.ID, buyer.Id))
				require.NoError(t, model.PayAccountRechargeWallet(order.ID, buyer.Id))
				quota, err := model.GetUserQuota(buyer.Id, true)
				require.NoError(t, err)
				assert.Equal(t, 1000, quota)
				order, err = model.GetAccountRechargeOrder(order.ID, buyer.Id)
				require.NoError(t, err)
				info := service.RechargeAccountInfo{Email: "owner@example.com", Session: `{"user":{"email":"owner@example.com"},"accessToken":"fixture-not-a-credential"}`}
				secret, mask, err := service.EncryptRechargeInfo(order.TradeNo, info)
				require.NoError(t, err)
				require.NotContains(t, secret, "fixture-not-a-credential")
				_, err = service.DecryptRechargeInfo("different-order", secret)
				require.Error(t, err)
				require.NoError(t, model.SubmitAccountRechargeInfo(order.ID, buyer.Id, order.Version, secret, mask))
				order, err = model.GetAccountRechargeOrder(order.ID, buyer.Id)
				require.NoError(t, err)
				// 并发领取必须只有一个获胜；
				// SQLite的锁升级失败同样不能放行第二人。
				results := make(chan error, 2)
				var wg sync.WaitGroup
				for _, actor := range []int{sales.Id, other.Id} {
					wg.Go(func() {
						results <- model.ActAccountRechargeOrder(order.ID, actor, order.Version, false, "claim", "", 0)
					})
				}
				wg.Wait()
				close(results)
				success := 0
				for err := range results {
					if err == nil {
						success++
					}
				}
				assert.Equal(t, 1, success)
				order, err = model.GetAccountRechargeOrder(order.ID, buyer.Id)
				require.NoError(t, err)
				oldAssignee := order.AssigneeID
				// 转派后旧处理人无权读取；
				// 旧版本也不能再次领取或完成。
				require.NoError(t, model.ActAccountRechargeOrder(order.ID, 1, order.Version, true, "assign", "handover confirmed", other.Id))
				order, err = model.GetAccountRechargeOrder(order.ID, buyer.Id)
				require.NoError(t, err)
				_, err = model.ReadAccountRechargeSecret(order.ID, buyer.Id, order.Version, false)
				require.ErrorIs(t, err, model.ErrRechargeForbidden)
				if oldAssignee != other.Id {
					_, err = model.ReadAccountRechargeSecret(order.ID, oldAssignee, order.Version, false)
					require.ErrorIs(t, err, model.ErrRechargeForbidden)
				}
				ciphertext, err := model.ReadAccountRechargeSecret(order.ID, other.Id, order.Version, false)
				require.NoError(t, err)
				decoded, err := service.DecryptRechargeInfo(order.TradeNo, ciphertext)
				require.NoError(t, err)
				assert.Equal(t, info, decoded)
				order, err = model.GetAccountRechargeOrder(order.ID, buyer.Id)
				require.NoError(t, err)
				require.NoError(t, model.ActAccountRechargeOrder(order.ID, other.Id, order.Version, false, "need_info", "Please update the session", 0))
				order, err = model.GetAccountRechargeOrder(order.ID, buyer.Id)
				require.NoError(t, err)
				assert.Empty(t, order.Secret)
				require.NoError(t, model.SubmitAccountRechargeInfo(order.ID, buyer.Id, order.Version, secret, mask))
				order, err = model.GetAccountRechargeOrder(order.ID, buyer.Id)
				require.NoError(t, err)
				assert.Equal(t, other.Id, order.AssigneeID)
				require.NoError(t, model.ActAccountRechargeOrder(order.ID, other.Id, order.Version, false, "complete", "Subscription renewed", 0))
				order, err = model.GetAccountRechargeOrder(order.ID, buyer.Id)
				require.NoError(t, err)
				assert.Empty(t, order.Secret)
				require.Error(t, model.SubmitAccountRechargeInfo(order.ID, buyer.Id, order.Version, secret, mask))
				require.Error(t, model.RefundAccountRecharge(order.ID, 1, order.Version, false, "completed", ""))
				serialized, err := common.Marshal(order)
				require.NoError(t, err)
				assert.NotContains(t, string(serialized), "fixture-not-a-credential")
				assert.NotContains(t, string(serialized), "owner@example.com")
				// 微信真实签名通知独立入账，
				// 错误金额不改变订单或平台钱包。
				wx := &model.AccountRechargeOrder{UserID: buyer.Id, RequestID: "request-wechat", RequestHash: "wx", ProductID: p.ID, BaseCents: p.PriceCents, MoneyCents: p.PriceCents, TradeNo: "RCHUSR-wechat", PaymentProvider: model.PaymentProviderEpay, PaymentMethod: "wxpay"}
				wx, err = model.CreateAccountRechargeOrder(wx, p.Version, nil)
				require.NoError(t, err)
				oldAddress, oldID, oldKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
				operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://gateway.example", "test-merchant", "test-signing-key"
				t.Cleanup(func() {
					operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = oldAddress, oldID, oldKey
				})
				for _, amount := range []string{"149.99", "150.00", "150.00"} {
					params, err := epay.GenerateParamsWithError(map[string]string{"pid": "test-merchant", "type": "wxpay", "out_trade_no": wx.TradeNo, "trade_no": "gateway-reference", "trade_status": epay.StatusTradeSuccess, "money": amount, "sign_type": "MD5"}, operation_setting.EpayKey)
					require.NoError(t, err)
					values := url.Values{}
					for k, v := range params {
						values.Set(k, v)
					}
					rec := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(rec)
					ctx.Request = httptest.NewRequest("GET", "/notify?"+values.Encode(), nil)
					RechargeEpayNotify(ctx)
					expected := "success"
					if amount == "149.99" {
						expected = "fail"
					}
					assert.Equal(t, expected, rec.Body.String())
				}
				quota, err = model.GetUserQuota(buyer.Id, true)
				require.NoError(t, err)
				assert.Equal(t, 1000, quota)
				wx, err = model.GetAccountRechargeOrder(wx.ID, buyer.Id)
				require.NoError(t, err)
				assert.Equal(t, "paid", wx.PaymentStatus)
				require.NoError(t, model.RefundAccountRecharge(wx.ID, 1, wx.Version, false, "Unable to fulfill", ""))
				wx, err = model.GetAccountRechargeOrder(wx.ID, buyer.Id)
				require.NoError(t, err)
				require.Error(t, model.RefundAccountRecharge(wx.ID, 1, wx.Version, true, "Refund confirmed", ""))
				require.NoError(t, model.RefundAccountRecharge(wx.ID, 1, wx.Version, true, "Refund confirmed", "actual-refund-reference"))
				require.NoError(t, model.RefundAccountRecharge(wx.ID, 1, wx.Version, true, "Refund confirmed", "actual-refund-reference"))
				// 重复迁移要保留商品 false和终态订单，
				// 不重新上架或恢复凭据。
				p.Enabled = false
				require.NoError(t, model.SaveAccountRechargeProduct(&p))
				require.NoError(t, db.AutoMigrate(tables...))
				var saved model.AccountRechargeProduct
				require.NoError(t, db.First(&saved, p.ID).Error)
				assert.False(t, saved.Enabled)
			})
		}
	}
}

// Redis故障只隔离基础设施；
// 余额支付与退款仍执行真实数据库事务。
// 同一缓存中已预留的模型额度不可再被购买消费，
// 重复退款不放大余额。
// 使用真实用户缓存结构，
// 不在生产代码注入测试专用余额逻辑。
// 缓存中先扣除一笔尚未落库额度，
// 构造模型预留领先数据库的边界。
// 数据库仍有足够余额但缓存不足时必须拒绝购买，
// 避免双重消费。
// 显式恢复该预留后再测试付款，
// 两个场景不能混成随机并发故障。
// 模拟 Redis服务报错时必须停止付款，
// 不允许静默回落数据库扣款。
// 恢复 Redis后用原订单支付，
// 不能重新创建交易号掩盖未知状态。
// 连续两次付款后缓存与数据库都只减少一笔冻结额度。
// 退款批准使用实际状态机，
// 批准后订单进入禁止继续办理的状态。
// 故障注入点位于退款事件写入后，
// 确保数据库提交与缓存确认之间被覆盖。
// 该故障不是普通退款失败：
// 数据库已经返还，
// 缓存尚未恢复。
// 第一次确认返回待同步错误，
// 调用者不得因此重新执行数据库加额。
// 恢复基础设施后重试同单，
// 再次确认也不能把钱包额度翻倍。
// 最终同时检查缓存与数据库数值，
// 避免只验证其中一个存储成功。
// 测试用 Redis只隔离基础设施，
// 订单事务和资金函数保持生产实现。
// 回调注册在用例内移除，
// 不污染后续测试的持久化行为。
// 用例结束恢复全局 Redis客户端和开关，
// 不修改项目部署配置。
// 实际网络付款不在此用例范围，
// 微信通道另有签名与回调测试。
func TestRechargeWalletCacheAndRefund(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	require.NoError(t, db.AutoMigrate(&model.CreditAccount{}, &model.CreditEntry{}, &model.AccountRechargeProduct{}, &model.AccountRechargeOrder{}, &model.AccountRechargeEvent{}))
	server := miniredis.RunT(t)
	oldRedis, oldClient := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, common.RDB.Close()); common.RedisEnabled, common.RDB = oldRedis, oldClient })
	user := model.User{Username: "cache-recharge", AffCode: "cache-recharge", Role: 1, Status: 1, AuthVersion: 1, Quota: 1000}
	require.NoError(t, db.Create(&user).Error)
	_, err := model.GetUserCache(user.Id)
	require.NoError(t, err)
	p := model.AccountRechargeProduct{Title: "Wallet test", PriceCents: 100, Enabled: true, AllowWalletPayment: true}
	require.NoError(t, model.SaveAccountRechargeProduct(&p))
	o := &model.AccountRechargeOrder{UserID: user.Id, RequestID: "cache", RequestHash: "cache", ProductID: p.ID, BaseCents: 100, MoneyCents: 100, TradeNo: "RCHUSR-cache", PaymentProvider: "wallet", PaymentMethod: "wallet", WalletQuota: 600}
	o, err = model.CreateAccountRechargeOrder(o, p.Version, nil)
	require.NoError(t, err)
	key := fmt.Sprintf("user:%d", user.Id)
	// 建立缓存已扣、数据库批量落库尚未执行的真实边界状态。
	require.NoError(t, common.RDB.HIncrBy(context.Background(), key, "Quota", -500).Err())
	require.ErrorIs(t, model.PayAccountRechargeWallet(o.ID, user.Id), model.ErrRechargeBalance)
	require.NoError(t, common.RDB.HIncrBy(context.Background(), key, "Quota", 500).Err())
	server.SetError("unavailable")
	require.ErrorIs(t, model.PayAccountRechargeWallet(o.ID, user.Id), model.ErrRechargeCache)
	server.SetError("")
	require.NoError(t, model.PayAccountRechargeWallet(o.ID, user.Id))
	require.NoError(t, model.PayAccountRechargeWallet(o.ID, user.Id))
	assert.Equal(t, "400", server.HGet(key, "Quota"))
	o, err = model.GetAccountRechargeOrder(o.ID, user.Id)
	require.NoError(t, err)
	require.NoError(t, model.RefundAccountRecharge(o.ID, 1, o.Version, false, "Cancelled by support", ""))
	o, err = model.GetAccountRechargeOrder(o.ID, user.Id)
	require.NoError(t, err)
	// 在退款事务提交前注入缓存故障，
	// 模拟已退款但缓存同步失败。
	require.NoError(t, db.Callback().Create().After("gorm:create").Register("test:recharge_refund", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*model.AccountRechargeEvent); ok && event.Action == "refund" {
			server.SetError("unavailable")
		}
	}))
	require.ErrorIs(t, model.RefundAccountRecharge(o.ID, 1, o.Version, true, "Refunded", ""), model.ErrRechargeCache)
	require.NoError(t, db.Callback().Create().Remove("test:recharge_refund"))
	server.SetError("")
	require.NoError(t, model.RefundAccountRecharge(o.ID, 1, o.Version, true, "Refunded", ""))
	require.NoError(t, model.RefundAccountRecharge(o.ID, 1, o.Version, true, "Refunded", ""))
	quota, err := model.GetUserQuota(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 1000, quota)
	assert.Equal(t, "1000", server.HGet(key, "Quota"))
}

// 真正的会话认证、权限及证明消费串联验证，
// 不使用伪造角色上下文。
// 已撤销或其他会话的证明不能读取同单凭据，
// 敏感内容不进入审计表。
// 使用正式登录会话和访问令牌，
// 不能伪造上下文角色跳过认证。
// 用户角色升级后发布认证缓存，
// 使中间件读取真实业务员身份。
// 订单归属买家与当前业务员不同，
// 证明后台能力不是本人购买权限。
// 敏感读取路由串联角色门槛、网页登录限制和独立揭示能力。
// 没有证明的请求必须拒绝，
// 即使业务员就是当前处理人。
// 业务员请求资金退款同样拒绝，
// 不能以读取订单能力冒充管理员。
// 单次证明使用与生产一致的动作、订单编号和版本结构。
// 成功揭示只出现于独立响应，
// 响应必须带no-store。
// 同一证明第二次消费必须失败，
// 防止重复抓取账户资料。
// 用户授权覆盖故意设置管理员资源，
// 检验业务员硬边界优先。
// 读取完成后查询真实审计表，
// 断言没有邮箱和 session正文。
// 撤销会话后旧访问令牌必须立即失效，
// 不能等自然过期才拒绝。
// 错误提供商和渠道不能通过支付完成函数修改同一订单。
// 测试密钥仅用于临时数据库中的不可登录fixture。
// 权限矩阵由真实种子初始化，
// 不mock Can 返回值。
// 测试不宣称实现了对剪贴板或已发送外部资料的撤回能力。
// 证明消费的有效期及其他认证方式继续由既有安全回归覆盖。
func TestRechargeSensitivePermissions(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.CreditAccount{}, &model.CreditEntry{}, &model.AccountRechargeProduct{}, &model.AccountRechargeOrder{}, &model.AccountRechargeEvent{}, &model.CasbinRule{}, &model.AuthzRole{}))
	oldMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = oldMaster })
	require.NoError(t, authz.Init(model.DB))
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleSalesUser).Error)
	require.NoError(t, model.PublishUserAuthCache(user.Id))
	t.Setenv("OAK_RECHARGE_DATA_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)))
	secret, mask, err := service.EncryptRechargeInfo("RCHUSR-secret", service.RechargeAccountInfo{Email: "secret@example.com", Session: `{"token":"private-fixture"}`})
	require.NoError(t, err)
	o := model.AccountRechargeOrder{UserID: 999, RequestID: "secret", TradeNo: "RCHUSR-secret", PaymentStatus: "paid", Status: "processing", AssigneeID: user.Id, Version: 1, Secret: secret, EmailMask: mask}
	require.NoError(t, model.DB.Create(&o).Error)
	token, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	engine := gin.New()
	engine.POST("/api/account-recharge/operations/orders/:id/reveal-account-info", middleware.RechargeAuth(), RechargeSessionOnly, middleware.RequirePermission(authz.RechargeReveal), RevealRechargeInfo)
	engine.POST("/refund", middleware.RechargeAuth(), RechargeSessionOnly, middleware.RequirePermission(authz.RechargeRefund), RefundRechargeOrder)
	engine.GET("/products", middleware.AdminAuth(), GetRechargeProducts)
	request := func(path, proof string, version int) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", path, strings.NewReader(fmt.Sprintf(`{"version":%d}`, version)))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Security-Proof", proof)
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(rec, req)
		return rec
	}
	path := fmt.Sprintf("/api/account-recharge/operations/orders/%d/reveal-account-info", o.ID)
	assert.Equal(t, 403, request(path, "", 1).Code)
	assert.Equal(t, 403, request("/refund", "", 1).Code)
	raw, err := common.Marshal(service.RechargeVerificationContext{OrderID: o.ID, Version: 1, Action: "reveal"})
	require.NoError(t, err)
	proof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopeRecharge, Context: raw}, "password")
	rec := request(path, proof, 1)
	assert.Equal(t, 200, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "private-fixture")
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, 403, request(path, proof, 1).Code)
	// 业务员的旧管理员覆盖不能扩大到商品和资金动作。
	require.NoError(t, authz.SetUserPermissions(user.Id, authz.PermissionsMap{authz.ResourceRechargeOrders: {"refund": true}, authz.ResourceChannel: {"sensitive_write": true}}))
	assert.False(t, authz.Can(user.Id, common.RoleSalesUser, authz.RechargeRefund))
	assert.False(t, authz.Can(user.Id, common.RoleSalesUser, authz.ChannelSensitiveWrite))
	var audits []model.AuditLog
	require.NoError(t, model.LOG_DB.Find(&audits).Error)
	encoded, err := common.Marshal(audits)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private-fixture")
	assert.NotContains(t, string(encoded), "secret@example.com")
	// 撤销会话后的访问令牌即使尚未到期也必须立即失效。
	_, err = model.RevokeAllUserSessions(user.Id, "test-revocation")
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, request(path, "", 2).Code)
	// 币种与金额由官方回调适配器和完成事务分层核对。
	require.Error(t, model.CompleteAccountRechargePayment(o.TradeNo, model.PaymentProviderWechat, "unknown", "", decimal.NewFromInt(1)))
}

// 从真实用户会话走报价、创建和继续付款，
// 确保前后端字段接线一致。
// 网关表单由现有 SDK生成，
// 本地测试不产生真实资金交易。
// 从 HTTP 路由开始执行报价与创建，
// 保护客户端字段契约。
// 测试开启既有支付前置配置，
// 不能绕过正式服务端开关。
// 商品价格和用户确认价格故意先不一致，
// 创建必须返回冲突。
// 报价返回当前可信总价，
// 修正后同一未创建请求才允许继续。
// 订单创建结果检查真实买家归属，
// 客户端不能指定其他用户。
// 网关表单使用固定冻结金额，
// 不能错误套用钱包充值折扣。
// 表单异步通知地址必须属于代充值业务，
// 不得回落钱包入口。
// 继续付款重复调用后 JSON相同，
// 确保页面恢复的是原支付单。
// 订单表计数保持一条，
// 防止只复用展示订单号却创建重复记录。
// 普通详情仍显示待付款，
// 生成表单不是到账证据。
// 详情响应不能包含网关签名表单，
// 付款凭据只由付款接口提供。
// 调用已有签名 SDK 而非复写签名算法，
// 能发现参数接线错误。
// 所有通知和回跳地址使用测试配置，
// 不与真实商户服务联网。
// 环境设置在用例结束时恢复，
// 后续支付测试不继承临时渠道。
// 付款窗口与随机订单号由生产代码生成，
// 不在测试中伪造成功结果。
// 该用例补足 UI 只模拟 HTTP时无法覆盖的创建金额校验。
func TestRechargeCheckoutAndGatewayForm(t *testing.T) {
	// 初始化正式方言列名，单独运行也不依赖前面的矩阵测试。
	modelManagementDB(t, "sqlite", "")
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.CreditAccount{}, &model.CreditEntry{}, &model.AccountRechargeProduct{}, &model.AccountRechargeOrder{}, &model.AccountRechargeEvent{}, &model.InvoiceProfile{}, &model.InvoiceRequest{}))
	t.Setenv("OAK_RECHARGE_DATA_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{6}, 32)))
	previousSetting := *operation_setting.GetPaymentSetting()
	previousAddress, previousID, previousKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://gateway.example", "checkout-test", "checkout-signature"
	t.Cleanup(func() {
		*operation_setting.GetPaymentSetting() = previousSetting
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = previousAddress, previousID, previousKey
	})
	p := model.AccountRechargeProduct{Title: "Checkout plan", PriceCents: 12900, Enabled: true}
	require.NoError(t, model.SaveAccountRechargeProduct(&p))
	token, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	engine := gin.New()
	route := engine.Group("/api/account-recharge", middleware.UserAuth(), RechargeSessionOnly)
	route.POST("/quote", QuoteRechargeOrder)
	route.POST("/orders", CreateRechargeOrder)
	route.POST("/orders/:id/pay", PayRechargeOrder)
	route.GET("/orders/:id", GetRechargeOrder)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		data, err := common.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}
	purchase := rechargePurchase{ProductID: p.ID, ProductVersion: p.Version, PaymentMethod: "epay:wxpay", RequestID: "7cb148cb-7686-4d44-9349-1d8b9ce07121", QuotedCents: 12800}
	quote := request("POST", "/api/account-recharge/quote", purchase)
	assert.Equal(t, 200, quote.Code)
	assert.Contains(t, quote.Body.String(), `"money_cents":12900`)
	assert.Equal(t, 409, request("POST", "/api/account-recharge/orders", purchase).Code)
	purchase.QuotedCents = 12900
	result := request("POST", "/api/account-recharge/orders", purchase)
	require.Equal(t, 200, result.Code, result.Body.String())
	var envelope struct {
		Success bool                       `json:"success"`
		Data    model.AccountRechargeOrder `json:"data"`
	}
	require.NoError(t, common.Unmarshal(result.Body.Bytes(), &envelope))
	require.True(t, envelope.Success)
	assert.Equal(t, user.Id, envelope.Data.UserID)
	result = request("POST", fmt.Sprintf("/api/account-recharge/orders/%d/pay", envelope.Data.ID), gin.H{})
	require.Equal(t, 200, result.Code, result.Body.String())
	assert.Contains(t, result.Body.String(), `"payment_kind":"form"`)
	assert.Contains(t, result.Body.String(), `"money":"129.00"`)
	assert.Contains(t, result.Body.String(), `/api/account-recharge/epay/notify`)
	// 重复继续付款必须返回原交易号和同一表单，
	// 不创建第二笔订单。
	repeat := request("POST", fmt.Sprintf("/api/account-recharge/orders/%d/pay", envelope.Data.ID), gin.H{})
	assert.JSONEq(t, result.Body.String(), repeat.Body.String())
	var count int64
	require.NoError(t, model.DB.Model(&model.AccountRechargeOrder{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	// 从详情读取不会把创建付款入口当成已付款，
	// 也不会返回商户签名表单。
	result = request("GET", fmt.Sprintf("/api/account-recharge/orders/%d", envelope.Data.ID), nil)
	assert.Contains(t, result.Body.String(), `"payment_status":"pending"`)
	assert.NotContains(t, result.Body.String(), `"params"`)
	// 暂停业务只阻止继续收新款；已签发的支付通知仍须兑现。
	// 开关使用真实持久设置接口，不能只隐藏页面按钮。
	// 旧订单通过原幂等键恢复，不能因暂停丢失已有订单。
	previousEnabled := operation_setting.GetRechargeSetting().Enabled
	t.Cleanup(func() { operation_setting.GetRechargeSetting().Enabled = previousEnabled })
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	require.NoError(t, model.UpdateOption("recharge_setting.enabled", "false"))
	for _, path := range []string{"/quote", fmt.Sprintf("/orders/%d/pay", envelope.Data.ID)} {
		paused := request("POST", "/api/account-recharge"+path, purchase)
		assert.Contains(t, paused.Body.String(), "Account top-ups are paused")
	}
	assert.Contains(t, request("POST", "/api/account-recharge/orders", purchase).Body.String(), envelope.Data.TradeNo)
	purchase.RequestID = "643bf922-3f9f-4da9-ad8d-35111c9458aa"
	assert.Contains(t, request("POST", "/api/account-recharge/orders", purchase).Body.String(), "Account top-ups are paused")
	// 模拟合法回调完成原微信订单；不向任何真实商户请求付款。
	require.NoError(t, model.CompleteAccountRechargePayment(envelope.Data.TradeNo, model.PaymentProviderEpay, "wxpay", "paused-paid", decimal.NewFromInt(129)))
	result = request("GET", fmt.Sprintf("/api/account-recharge/orders/%d", envelope.Data.ID), nil)
	assert.Contains(t, result.Body.String(), `"payment_status":"paid"`)
	// 已付订单暂停期间仍可提交资料，用户不应被锁在已付款阶段。
	route.POST("/orders/:id/account-info", SubmitRechargeInfo)
	paid, err := model.GetAccountRechargeOrder(envelope.Data.ID, user.Id)
	require.NoError(t, err)
	result = request("POST", fmt.Sprintf("/api/account-recharge/orders/%d/account-info", paid.ID), gin.H{"version": paid.Version, "email": "owner@example.com", "session": `{"user":{"email":"owner@example.com"},"accessToken":"fixture"}`})
	assert.Contains(t, result.Body.String(), `"success":true`)

}

// 业务员授权沿用正式用户管理入口，不能仅测试权限常量。
// Root 需要绑定目标与动作的单次证明；普通管理员不能授予此角色。
// 降为业务员时撤销旧管理员会话并清除覆盖，避免旧管理能力残留。
func TestRechargeSalesRoleAssignment(t *testing.T) {
	db := setupManageUserTestDB(t)
	previousMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = previousMaster })
	require.NoError(t, authz.Init(db))
	user := model.User{Username: "managed-sales", Password: "password", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, authz.SetUserPermissions(user.Id, authz.PermissionsMap{authz.ResourceChannel: {authz.ActionSensitiveWrite: true}}))
	// 会话使用虚构测试凭据，验证真实持久状态而非只看响应文案。
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.UserSession{SID: "sales-old-admin", UserID: user.Id, Version: 1, UserAuthVersion: 1, Status: model.UserSessionStatusActive, RefreshHash: "fixture-refresh", LoginMethod: "password", LastActiveAt: now, ExpiresAt: now + 3600}).Error)
	body := fmt.Sprintf(`{"id":%d,"action":"sales"}`, user.Id)
	assert.NotContains(t, performManageUserRequest(t, body).Body.String(), `"success":true`)
	identity, proof := manageUserProof(t, db, service.VerificationOperation{Scope: service.VerificationScopeAdminUserManage, Context: []byte(fmt.Sprintf(`{"user_id":%d,"action":"sales"}`, user.Id))})
	response := performVerifiedManageUserRequest(t, body, identity, proof)
	require.Contains(t, response.Body.String(), `"success":true`)
	var updated model.User
	require.NoError(t, db.First(&updated, user.Id).Error)
	assert.Equal(t, common.RoleSalesUser, updated.Role)
	assert.EqualValues(t, 2, updated.AuthVersion)
	assert.True(t, authz.Can(user.Id, updated.Role, authz.RechargeWrite))
	assert.False(t, authz.Can(user.Id, updated.Role, authz.RechargeRefund))
	assert.Empty(t, authz.ExplicitUserOverrides(user.Id))
	var session model.UserSession
	require.NoError(t, db.First(&session, "sid = ?", "sales-old-admin").Error)
	assert.Equal(t, model.UserSessionStatusRevoked, session.Status)
	assert.Equal(t, "admin_sales_role", session.RevokedReason)
	// 创建业务员也不能利用低于管理员的角色数值绕过 Root 门槛。
	createBody := `{"username":"new-sales","password":"testpassword123","role":3}`
	denied := adminUserRequest(http.MethodPost, "/api/user/", createBody, "", service.AuthIdentity{UserID: 9999}, common.RoleAdminUser, nil, CreateUser)
	assert.Contains(t, denied.Body.String(), "Only root can assign the sales role")
}

// 错误提示必须区分用户输入与部署配置，且不能回显会话或邮箱。
// 使用虚构资料调用真实提交处理器，避免将用户粘贴的认证数据写入测试。
func TestRechargeAccountInfoErrorMessages(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")                                                         // 只使用隔离数据库，不连接运行中的预览订单。
	require.NoError(t, db.AutoMigrate(&model.AccountRechargeOrder{}, &model.AccountRechargeEvent{})) // 该回归只需要订单和事件表。
	order := model.AccountRechargeOrder{UserID: 7, RequestID: "info-errors", TradeNo: "RCHUSR-info-errors", PaymentStatus: "paid", Status: "awaiting_info", Version: 1}
	require.NoError(t, db.Create(&order).Error)                                // 已付款待资料才是本次故障的入口条件。
	validKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32)) // 测试密钥仅用于本进程，不能用于运行配置。
	session := `{"user":{"email":"owner@example.com"},"accessToken":"invalid-fixture"}`
	// 各失败分支均保持原单未提交，用户修改资料后无需重新付款。
	for _, tc := range []struct{ name, email, session, key, message string }{
		{"email_mismatch", "other@example.com", session, validKey, "Account email does not match the session"},
		{"invalid_json", "owner@example.com", "invalid-fixture", validKey, "Invalid recharge account information"},
		{"invalid_email", "invalid-email", session, validKey, "Invalid recharge account information"},
		{"missing_key", "owner@example.com", session, "", "Recharge encryption is not configured"},
	} {
		t.Run(tc.name, func(t *testing.T) { // 逐个区分输入错误和配置错误，不接受同一笼统提示。
			t.Setenv("OAK_RECHARGE_DATA_KEY", tc.key) // 子用例自动恢复密钥，避免缺失配置污染后续测试。
			body, err := common.Marshal(gin.H{"version": 1, "email": tc.email, "session": tc.session})
			require.NoError(t, err) // 请求仍通过正式 JSON 编码器，保护字段绑定契约。
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Set("id", 7) // 固定为订单本人；本例聚焦提示，鉴权由独立测试覆盖。
			c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(order.ID)}}
			c.Request = httptest.NewRequest("POST", "/account-info", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			SubmitRechargeInfo(c) // 调用真实处理器，不能仅断言服务层返回的错误。
			var result struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &result))
			assert.False(t, result.Success)                             // 校验失败不得返回成功或把订单加入待办。
			assert.Equal(t, tc.message, result.Message)                 // 明确错误键让前端能够呈现对应中文文案。
			assert.NotContains(t, rec.Body.String(), "invalid-fixture") // 错误响应不得携带会话值。
			assert.NotContains(t, rec.Body.String(), tc.email)          // 不通过错误文案回显充值账号。
			saved, err := model.GetAccountRechargeOrder(order.ID, 7)
			require.NoError(t, err)
			assert.Empty(t, saved.Secret)     // 失败不产生半份加密资料。
			assert.Equal(t, 1, saved.Version) // 保留版本允许买家修改邮箱后直接重交。
		})
	}
	// 邮箱大小写与两端空白不构成账号不匹配，原始 JSON 必须仍可完整还原。
	t.Setenv("OAK_RECHARGE_DATA_KEY", validKey) // 成功边界与失败边界采用相同加密配置。
	encrypted, _, err := service.EncryptRechargeInfo(order.TradeNo, service.RechargeAccountInfo{Email: " OWNER@example.com ", Session: session})
	require.NoError(t, err)
	decoded, err := service.DecryptRechargeInfo(order.TradeNo, encrypted)
	require.NoError(t, err)
	assert.Equal(t, session, decoded.Session) // 仅规范邮箱，原始 JSON 不应被重写。
}

// 设置状态使用真实数据库及正式选项控制器，不模拟保存成功。
// 每个方言验证开关落库、只读状态和坏配置拒绝。
// 测试只使用固定虚构密钥，响应不得包含密钥正文。
// 故障注入仅影响本用例隔离库，确保失败不会改变运行状态。
func TestRechargeConfiguration(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dialect != "sqlite" && dsn == "" {
				t.Skip("test database DSN is not configured")
			}
			db := modelManagementDB(t, dialect, dsn)
			previousEnabled := operation_setting.GetRechargeSetting().Enabled
			previousPayment := *operation_setting.GetPaymentSetting()
			t.Cleanup(func() {
				operation_setting.GetRechargeSetting().Enabled = previousEnabled
				*operation_setting.GetPaymentSetting() = previousPayment
			})
			operation_setting.GetPaymentSetting().ComplianceConfirmed = true
			operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
			t.Setenv("OAK_RECHARGE_DATA_KEY", "")
			// 缺少密钥时拒绝启用，管理员得到明确原因。
			response := modelManagementRequest(t, UpdateOption, "PUT", "/api/option/", gin.H{"key": "recharge_setting.enabled", "value": true}, nil)
			assert.Contains(t, response.Body.String(), "Recharge encryption is not configured")
			// 关闭无须密钥，保存 false 后再次读取数据库仍为 false。
			response = modelManagementRequest(t, UpdateOption, "PUT", "/api/option/", gin.H{"key": "recharge_setting.enabled", "value": false}, nil)
			assert.Contains(t, response.Body.String(), `"success":true`)
			var saved model.Option
			require.NoError(t, db.Where(&model.Option{Key: "recharge_setting.enabled"}).First(&saved).Error)
			assert.Equal(t, "false", saved.Value)
			assert.False(t, operation_setting.GetRechargeSetting().Enabled)
			require.NoError(t, db.AutoMigrate(&model.Option{})) // 现有选项表无须新增列，重复迁移保留开关。
			require.NoError(t, db.Where(&model.Option{Key: "recharge_setting.enabled"}).First(&saved).Error)
			assert.Equal(t, "false", saved.Value)
			// 正常运行时就绪信息由服务器计算，不接受浏览器伪造。
			for _, key := range []string{"RechargeEncryptionReady", "RechargePaymentReady", "RechargeWeChatReady"} {
				require.Error(t, model.UpdateOption(key, "true"))
			}
			require.Error(t, model.UpdateOption("recharge_setting.enabled", "yes"))
			key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
			t.Setenv("OAK_RECHARGE_DATA_KEY", key)
			response = modelManagementRequest(t, UpdateOption, "PUT", "/api/option/", gin.H{"key": "recharge_setting.enabled", "value": true}, nil)
			assert.Contains(t, response.Body.String(), `"success":true`)
			assert.True(t, operation_setting.GetRechargeSetting().Enabled)
			response = modelManagementRequest(t, GetOptions, "GET", "/api/option/", nil, nil)
			assert.Contains(t, response.Body.String(), `"key":"RechargeEncryptionReady","value":"true"`)
			assert.NotContains(t, response.Body.String(), key)
			// 已有商品接口同步反映开关，不需要前端猜测服务是否暂停。
			require.NoError(t, db.AutoMigrate(&model.CreditAccount{}, &model.CreditEntry{}, &model.AccountRechargeProduct{}))
			require.NoError(t, model.UpdateOption("recharge_setting.enabled", "false"))
			response = modelManagementRequest(t, GetRechargeProducts, "GET", "/api/account-recharge/products", nil, nil)
			assert.Contains(t, response.Body.String(), `"enabled":false`)
			assert.Contains(t, response.Body.String(), `"configured":false`)
			// 数据库写入失败不能假装启用；复用批量选项事务的错误传播。
			require.NoError(t, db.Migrator().DropTable(&model.Option{}))
			require.Error(t, model.UpdateOption("recharge_setting.enabled", "true"))
			assert.False(t, operation_setting.GetRechargeSetting().Enabled)
		})
	}
}

// 真实三库读取订单关联的发票，不以页面快照代替数据库契约。
// 每个方言创建独立测试库，不读写本地预览或线上订单。
// 订单所有者来自认证上下文，接口参数不能切换用户身份。
// 测试只使用虚构抬头和附件路径，不生成真实税务文件。
// 摘要不得携带银行资料、税号、完整抬头快照或文件存储路径。
func TestRechargeOrderInvoice(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dialect != "sqlite" && dsn == "" {
				t.Skip("test database DSN is not configured")
			}
			db := modelManagementDB(t, dialect, dsn) // 正式方言初始化保证查询与线上相同。
			require.NoError(t, db.AutoMigrate(&model.AccountRechargeOrder{}, &model.InvoiceRequest{}))
			order := model.AccountRechargeOrder{UserID: 8, RequestID: "invoice-detail", TradeNo: "RCH-invoice-detail", InvoiceRequired: true}
			require.NoError(t, db.Create(&order).Error) // 不调用付款接口，避免本测试涉及资金变更。
			profile, err := common.Marshal(model.InvoiceProfile{Title: "Frozen invoice title", TaxId: "private-tax", BankAccount: "private-bank"})
			require.NoError(t, err)
			invoice := model.InvoiceRequest{UserId: 8, TradeNo: order.TradeNo, BusinessType: "recharge", ProfileJSON: string(profile), PayAmount: 1.15, Currency: "CNY"}
			require.NoError(t, db.Create(&invoice).Error) // 发票主键由数据库生成，下载引用必须使用这个编号。
			// 正式路由模板决定买家与运营分支，不能直接调用而丢失 FullPath。
			owner := 8 // 认证上下文固定买家，客户端不能提交所有者。
			engine := gin.New()
			engine.Use(func(c *gin.Context) { c.Set("id", owner); c.Set("role", common.RoleCommonUser) })
			engine.GET("/orders/:id", GetRechargeOrder)
			engine.GET("/operations/orders/:id", GetRechargeOrder)
			read := func(prefix string) *httptest.ResponseRecorder {
				r := httptest.NewRecorder() // 每次读取单独保存响应，不继承上一状态。
				engine.ServeHTTP(r, httptest.NewRequest("GET", fmt.Sprintf("%s/orders/%d", prefix, order.ID), nil))
				return r
			}
			// 五种状态原样读取，订单是否办理完成不能替代财务开票状态。
			// 附件存在性来自正式 Expand，文件路径永远不进入摘要。
			for _, status := range []string{"pending_payment", "pending", "processing", "issued", "rejected"} {
				require.NoError(t, db.Model(&invoice).Updates(map[string]any{"status": status, "remark": "Invoice review note", "file_path": "private/invoice.pdf", "invoice_number": "INV-test"}).Error)
				r := read("")
				var response struct {
					Success bool
					Data    struct{ Invoice map[string]any }
				}
				require.NoError(t, common.Unmarshal(r.Body.Bytes(), &response)) // 解析真实 JSON 响应，检验序列化契约。
				require.True(t, response.Success, r.Body.String())
				assert.Equal(t, status, response.Data.Invoice["status"])
				assert.Equal(t, "Frozen invoice title", response.Data.Invoice["title"])
				assert.Equal(t, 1.15, response.Data.Invoice["pay_amount"])
				assert.Equal(t, true, response.Data.Invoice["has_file"])          // 服务端按文件引用派生公开标记。
				assert.Equal(t, float64(invoice.Id), response.Data.Invoice["id"]) // JSON 数字解码为浮点数，不能误用订单主键。
				for _, private := range []string{"private-tax", "private-bank", "private/invoice.pdf", "profile_json"} {
					assert.NotContains(t, r.Body.String(), private)
				}
			}
			// 业务员可读订单不表示可读税务信息；响应不出现发票对象。
			assert.NotContains(t, read("/operations").Body.String(), `"invoice":`)
			// 另一用户不能先读取订单再借其交易号取得发票。
			owner = 9
			assert.Equal(t, http.StatusNotFound, read("").Code)
			owner = 8 // 恢复本人后验证申请自身的关联约束。
			// 跨用户或跨业务的错误关联必须失败，不用同订单号掩盖坏数据。
			for _, changed := range []map[string]any{{"user_id": 9}, {"user_id": 8, "business_type": "topup"}, {"business_type": "recharge", "profile_json": "invalid-json"}} {
				require.NoError(t, db.Model(&invoice).Updates(changed).Error)
				assert.Contains(t, read("").Body.String(), `"success":false`)
			}
			// 已申请却丢失申请时明确报错，不能显示为未申请或待开票。
			require.NoError(t, db.Delete(&invoice).Error) // 仅删除当前隔离库的测试记录。
			assert.Contains(t, read("").Body.String(), "Failed to load invoice requests")
			// 未申请开票无须存在发票行，返回 null 表达真实选择。
			require.NoError(t, db.Model(&order).Update("invoice_required", false).Error)
			assert.Contains(t, read("").Body.String(), `"invoice":null`)
		})
	}
}
