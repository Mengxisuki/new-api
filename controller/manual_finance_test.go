package controller

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// manualFinanceRequest 只序列化调用方明确给出的输入，不补任何合法默认值。
// 缺失分类、说明和请求键必须原样抵达生产控制器，避免测试夹具掩盖校验漏洞。
// 路径参数支持两个后台订阅入口；身份与业务参数分开，不能由请求体提权。
func manualFinanceRequest(t *testing.T, handler gin.HandlerFunc, role, userID int, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := common.Marshal(body) // 与正式 JSON 约定相同，测试不自建编码兼容层。
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder) // 只隔离 HTTP 传输，生产 handler 和数据库事务都真实执行。
	c.Request = httptest.NewRequest(http.MethodPost, "/api/manual-finance", bytes.NewReader(encoded))
	c.Request.Header.Set("Content-Type", "application/json")      // 必须经过生产输入绑定，而不是直接传内部结构。
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(userID)}} // 第二个订阅入口只能使用路由指定的用户。
	c.Set("id", 9999)
	c.Set("role", role)
	c.Set("username", "manual-operator")
	handler(c)
	return recorder
}

// manualFinanceUser 提供钱包、授信同时存在的用户，用于检查两种单位绝不混算。
// 配置走正式授信入口，不直接填 UsedCents 制造一笔看似成功的债务。
// 用户名由场景指定；每个场景独立记录，后续失败不能污染已有成功快照。
func manualFinanceUser(t *testing.T, db *gorm.DB, name string) model.User {
	t.Helper()
	user := model.User{Username: name, AffCode: name, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 500000, Group: "default", AuthVersion: 1}
	require.NoError(t, db.Create(&user).Error)
	account := model.CreditAccount{UserID: user.Id, Enabled: true, LimitCents: 10000, TopupEnabled: true, SubscriptionEnabled: true}
	require.NoError(t, model.SaveCreditAccount(account, 9999)) // 独立人民币分额度，不取钱包值作为授信。
	return user
}

// manualFinanceInput 固定逻辑操作 UUID，重试复用、下一笔操作显式换编号。
// 金额由场景自己填写，不能根据原因自动生成从而掩盖漏填或单位错误。
func manualFinanceInput(reason string, operation int) model.ManualFinanceInput {
	return model.ManualFinanceInput{Reason: reason, Note: "人工财务真实入口回归", RequestID: fmt.Sprintf("55555555-5555-4555-8555-%012d", operation)}
}

// 真实三库矩阵集中覆盖人工入口，不通过直接 Create 成功账本代替业务操作。
// 外部 DSN 缺失时明确跳过；不能用 SQLite 的通过宣称三库兼容。
// 复用既有数据库隔离器，不连接开发库或线上业务库。
func TestManualFinanceDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dialect != "sqlite" && dsn == "" {
				t.Skip("dedicated test database DSN is not configured")
			}
			db := modelManagementDB(t, dialect, dsn) // 初始化真实方言与主库句柄。
			require.NoError(t, db.AutoMigrate(&model.PaymentRefund{}, &model.QuotaLedger{}, &model.CreditAccount{}, &model.CreditEntry{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionOrder{}, &model.TopUp{}, &model.Log{}))
			payment := operation_setting.GetPaymentSetting()
			previous := *payment // 后台订阅保留正式合规门禁，夹具只设置已确认状态。
			payment.ComplianceConfirmed, payment.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
			t.Cleanup(func() { *payment = previous })
			t.Run("quota_classifications", func(t *testing.T) { checkManualQuotaClassifications(t, db) })
			t.Run("quota_only_reversals", func(t *testing.T) { checkManualQuotaReversals(t, db) })
			t.Run("invalid_and_permissions", func(t *testing.T) { checkManualFinanceInvalid(t, db) })
			t.Run("subscription_snapshots", func(t *testing.T) { checkManualSubscriptions(t, db) })
			t.Run("ledger_failure", func(t *testing.T) { checkManualFinanceRollback(t, db) })
		})
	}
}

// 四类正向原因分别覆盖 add 和 override；期望直接列出，不复制生产调额公式。
// 相同请求键重放必须保持原发生时间和余额快照，而非重算覆盖差额。
// 每笔先成功再提交同键异参，证明冲突不能重复增加钱包、现金或欠款。
func checkManualQuotaClassifications(t *testing.T, db *gorm.DB) {
	t.Helper()
	for i, reason := range []string{"compensation", "grant", "on_account", "offline_cash"} {
		for j, mode := range []string{"add", "override"} {
			t.Run(reason+"/"+mode, func(t *testing.T) {
				user := manualFinanceUser(t, db, fmt.Sprintf("class-%d-%d", i, j))
				input := manualFinanceInput(reason, 100+i*2+j) // 独立操作不能共用上一笔请求键。
				if reason == "offline_cash" {
					input.CashCents, input.CashRef = 1000, "offline-proof-1000"
				}
				if reason == "on_account" {
					input.ReceivableCents = 1300 // 明确与 500000 quota 不同，不依赖当前换算率。
				}
				value := 500000
				if mode == "override" {
					value = 1000000 // 覆盖值不是差额，更不能当作人民币分。
				}
				request := ManageRequest{Id: user.Id, Action: "add_quota", Mode: mode, Value: value, ManualFinanceInput: input}
				response := manualFinanceRequest(t, ManageUser, common.RoleRootUser, user.Id, request)
				require.Contains(t, response.Body.String(), `"success":true`)
				var before model.QuotaLedger
				require.NoError(t, db.Where("request_id = ?", input.RequestID).First(&before).Error)
				require.NotNil(t, before.QuotaBefore) // 钱包快照是持久余额，不是 Redis 预扣后的值。
				require.NotNil(t, before.QuotaAfter)
				assert.EqualValues(t, 500000, *before.QuotaBefore)             // 原始余额必须来自事务内锁定用户。
				assert.EqualValues(t, 1000000, *before.QuotaAfter)             // add 与 override 对相同结果保存一致快照。
				assert.EqualValues(t, 500000, before.Delta)                    // 分类统计使用净差额，不使用覆盖目标值。
				assert.Equal(t, reason, before.Reason)                         // 无现金赠送不能在持久化时被改写成收款。
				assert.Equal(t, model.ManualSourceQuota, before.Source)        // 来源约束决定后续能否被钱包更正引用。
				assert.Equal(t, user.Id, before.UserID)                        // 权益归属不能误存为发起操作的管理员。
				assert.Equal(t, 9999, before.OperatorID)                       // 操作者取上下文，不允许由业务请求指定。
				assert.Equal(t, input.CashCents, before.CashCents)             // 实收金额必须来自明确输入，不能从额度反推。
				assert.Equal(t, input.ReceivableCents, before.ReceivableCents) // 挂帐发生额不能同时进入现金列。
				assert.Equal(t, input.CashRef, before.CashRef)                 // 收款凭证原样保留，后续对账不依赖业务日志。
				assert.Empty(t, before.SubscriptionAfter)                      // 钱包操作不得伪造订阅权益快照。
				response = manualFinanceRequest(t, ManageUser, common.RoleRootUser, user.Id, request)
				require.Contains(t, response.Body.String(), `"success":true`)
				var after model.QuotaLedger
				require.NoError(t, db.First(&after, before.ID).Error)
				assert.Equal(t, before, after) // 包括原记账时间；重试不可覆盖证据。
				request.Value++                // 合法范围内只改金额，确保失败来自幂等冲突而不是金额边界。
				response = manualFinanceRequest(t, ManageUser, common.RoleRootUser, user.Id, request)
				assert.Contains(t, response.Body.String(), `"success":false`) // 冲突必须向调用者显式暴露，不能伪装成功。
				require.NoError(t, db.First(&user, user.Id).Error)
				assert.Equal(t, 1000000, user.Quota) // 成功、重试、冲突之后都只有一笔额度增加。
				var ledgers []model.QuotaLedger
				require.NoError(t, db.Where("user_id = ?", user.Id).Find(&ledgers).Error)
				require.Len(t, ledgers, 1) // 现金和应收计数由来源单决定，重复记录即重复财务发生额。
				account, err := model.GetCreditAccount(user.Id)
				require.NoError(t, err)
				assert.Equal(t, input.ReceivableCents, account.UsedCents) // 可用授信扣的是分，不是内部 quota。
				var debts []model.CreditEntry                             // 同键重试和冲突都不得重复占用授信。
				require.NoError(t, db.Where("user_id = ?", user.Id).Find(&debts).Error)
				if reason == "on_account" {
					require.Len(t, debts, 1)                                  // 幂等恢复不得生成第二笔可结清债务。
					assert.EqualValues(t, 1300, debts[0].AmountCents)         // 与钱包新增 500000 的数量级有意区分。
					assert.Equal(t, "outstanding", debts[0].Status)           // 授予额度不是已经收到款项。
					assert.Equal(t, model.CreditModuleTopup, debts[0].Module) // 财务结清按业务模块定位，不伪装代充值订单。
					assert.Equal(t, before.TradeNo, debts[0].TradeNo)         // 人工来源单与债务必须可相互追溯。
					assert.Equal(t, debts[0].ID, before.CreditEntryID)        // 外键引用只能指向本事务实际创建的债务。
				} else {
					assert.Empty(t, debts) // 赠送、补偿和实收均不产生应收债务。
				}
			})
		}
	}
}

// 撤回与更正只改钱包，不等同退款或免除欠款。
// 原记录由真实入口产生；关联记录只追加，不能覆盖首次收款凭证。
// 最后一笔零差额覆盖也需留痕，不能因余额未变化跳过财务校验。
func checkManualQuotaReversals(t *testing.T, db *gorm.DB) {
	t.Helper()
	user := manualFinanceUser(t, db, "manual-reversals")
	var originals []model.QuotaLedger
	for i, reason := range []string{"grant", "compensation", "offline_cash", "on_account"} {
		input := manualFinanceInput(reason, 200+i)
		if reason == "offline_cash" {
			input.CashCents, input.CashRef = 1000, "retained-cash-proof"
		}
		if reason == "on_account" {
			input.ReceivableCents = 1300 // 欠款来源独立存在，扣额度不得将其冲掉。
		}
		request := ManageRequest{Id: user.Id, Action: "add_quota", Mode: "add", Value: 500000, ManualFinanceInput: input}
		require.Contains(t, manualFinanceRequest(t, ManageUser, common.RoleRootUser, user.Id, request).Body.String(), `"success":true`)
		var entry model.QuotaLedger
		require.NoError(t, db.Where("request_id = ?", input.RequestID).First(&entry).Error)
		originals = append(originals, entry) // 保存不可变原单，完成更正后逐字段比对。
	}
	for i, tc := range []struct {
		reason, mode           string
		value, after, original int
	}{
		{"revoke_grant", "subtract", 500000, 2000000, 0},
		{"revoke_compensation", "override", 1500000, 1500000, 1},
		{"correction", "subtract", 500000, 1000000, 2},
		{"correction", "override", 500000, 500000, 3},
		{"correction", "override", 500000, 500000, 3},
	} {
		input := manualFinanceInput(tc.reason, 210+i)
		input.OriginalID = originals[tc.original].ID // 撤回关联同用户原单，财务金额仍不自动逆转。
		request := ManageRequest{Id: user.Id, Action: "add_quota", Mode: tc.mode, Value: tc.value, ManualFinanceInput: input}
		require.Contains(t, manualFinanceRequest(t, ManageUser, common.RoleRootUser, user.Id, request).Body.String(), `"success":true`)
		require.NoError(t, db.First(&user, user.Id).Error)
		assert.Equal(t, tc.after, user.Quota) // 检查逐笔余额，避免互相抵消的错误被最终余额掩盖。
		var correction model.QuotaLedger
		require.NoError(t, db.Where("request_id = ?", input.RequestID).First(&correction).Error)
		assert.Zero(t, correction.CashCents)                     // 负向额度不是负向实收。
		assert.Zero(t, correction.ReceivableCents)               // 账务更正无权通过负应收抵消原债务。
		assert.Equal(t, input.OriginalID, correction.OriginalID) // 追溯字段保留，而不是将原单标记删除。
		if i == 4 {
			assert.Zero(t, correction.Delta) // 显式零差额更正也保存一笔证据。
		}
	}
	var stored []model.QuotaLedger
	require.NoError(t, db.Where("user_id = ?", user.Id).Order("id").Find(&stored).Error)
	require.Len(t, stored, 9)              // 四次发放、四次扣回、一次零差额更正，各有独立证据。
	assert.Equal(t, originals, stored[:4]) // 原现金、挂帐、赠送、补偿均不可被修改。
	var debt model.CreditEntry
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&debt).Error)
	assert.EqualValues(t, 1300, debt.AmountCents) // 原债权金额不因售出的额度被扣回而变化。
	assert.Equal(t, "outstanding", debt.Status)   // 未收款仍可进入财务结清流程。
	assert.Zero(t, debt.ClosedAt)                 // 扣回已发额度不是退款或债务撤销。
	assert.Empty(t, debt.RefundRef)               // 不允许伪造未实际发生的退款凭证。
	account, err := model.GetCreditAccount(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1300, account.UsedCents) // 钱包更正不能偷偷释放可再次借用的授信。
}

// 非法财务请求必须在真实入口失败，不能在夹具里补分类或默认金额。
// 涉及其他用户的原单、同键换用户以及同级管理员目标都不得越权。
// 全部拒绝后读取余额与账本数量，避免只断言错误文本却漏掉部分提交。
func checkManualFinanceInvalid(t *testing.T, db *gorm.DB) {
	t.Helper()
	user := manualFinanceUser(t, db, "manual-invalid")
	other := manualFinanceUser(t, db, "manual-other")
	input := manualFinanceInput("grant", 300)
	originalRequest := ManageRequest{Id: other.Id, Action: "add_quota", Mode: "add", Value: 500000, ManualFinanceInput: input}
	require.Contains(t, manualFinanceRequest(t, ManageUser, common.RoleRootUser, other.Id, originalRequest).Body.String(), `"success":true`)
	var original model.QuotaLedger
	require.NoError(t, db.Where("user_id = ?", other.Id).First(&original).Error)
	for i, tc := range []struct {
		name, reason, mode, note, key, reference string
		value                                    int
		cash, receivable                         int64
		original                                 int
	}{
		{name: "missing_reason", mode: "add", value: 1},                                                                                   // 旧客户端不填分类也必须拒绝，不能默认成赠送。
		{name: "historical_forbidden", reason: "unclassified", mode: "add", value: 1},                                                     // 历史展示分类不是新操作的逃生通道。
		{name: "unknown_reason", reason: "refund", mode: "subtract", value: 1},                                                            // 调额不能通过任意字符串扩展为退款操作。
		{name: "blank_note", reason: "grant", mode: "add", value: 1, note: " "},                                                           // 去除空白后必须仍有可审计说明。
		{name: "missing_key", reason: "grant", mode: "add", value: 1, key: "missing"},                                                     // 无法保证重试安全的请求不应先改变余额。
		{name: "invalid_uuid", reason: "grant", mode: "add", value: 1, key: "not-a-uuid"},                                                 // 任意业务描述不能替代明确请求身份。
		{name: "uuid_v1", reason: "grant", mode: "add", value: 1, key: "55555555-5555-1555-8555-555555555555"},                            // 可解析 UUID 不等于满足指定的 v4 合同。
		{name: "cash_missing_amount", reason: "offline_cash", mode: "add", value: 500000, reference: "proof"},                             // 有凭证也不能拿 quota 代替漏填的分金额。
		{name: "cash_missing_ref", reason: "offline_cash", mode: "add", value: 500000, cash: 1000},                                        // 金额正确仍需可追溯的实际收款证据。
		{name: "cash_negative", reason: "offline_cash", mode: "add", value: 500000, cash: -1, reference: "proof"},                         // 拒绝借负现金值混入退款。
		{name: "cash_over_limit", reason: "offline_cash", mode: "add", value: 500000, cash: model.MaxCreditCents + 1, reference: "proof"}, // 金额校验独立于合法的钱包增量。
		{name: "both_money_kinds", reason: "offline_cash", mode: "add", value: 500000, cash: 1000, receivable: 1300, reference: "proof"},  // 同一来源不能同时记实收和应收。
		{name: "receivable_missing", reason: "on_account", mode: "add", value: 500000},                                                    // 授信足够不代表可以省略本次债务金额。
		{name: "receivable_negative", reason: "on_account", mode: "add", value: 500000, receivable: -1},
		{name: "receivable_over_limit", reason: "on_account", mode: "add", value: 500000, receivable: model.MaxCreditCents + 1},
		{name: "grant_cannot_collect", reason: "grant", mode: "add", value: 500000, cash: 1000},
		{name: "grant_cannot_subtract", reason: "grant", mode: "subtract", value: 1},
		{name: "compensation_cannot_subtract", reason: "compensation", mode: "subtract", value: 1},
		{name: "cash_cannot_subtract", reason: "offline_cash", mode: "subtract", value: 1, cash: 1000, reference: "proof"},
		{name: "credit_cannot_subtract", reason: "on_account", mode: "subtract", value: 1, receivable: 1300},
		{name: "grant_zero_override", reason: "grant", mode: "override", value: 500000},
		{name: "compensation_zero_override", reason: "compensation", mode: "override", value: 500000},
		{name: "cash_zero_override", reason: "offline_cash", mode: "override", value: 500000, cash: 1000, reference: "proof"},
		{name: "credit_zero_override", reason: "on_account", mode: "override", value: 500000, receivable: 1300},
		{name: "revoke_positive", reason: "revoke_grant", mode: "add", value: 1},
		{name: "revoke_zero", reason: "revoke_compensation", mode: "override", value: 500000},
		{name: "foreign_original", reason: "correction", mode: "subtract", value: 1, original: original.ID},
		{name: "foreign_request_key", reason: "grant", mode: "add", value: 500000, key: input.RequestID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			finance := manualFinanceInput(tc.reason, 310+i)
			finance.CashCents, finance.ReceivableCents, finance.CashRef, finance.OriginalID = tc.cash, tc.receivable, tc.reference, tc.original
			if tc.note != "" {
				finance.Note = tc.note
			}
			if tc.key != "" {
				finance.RequestID = tc.key
			}
			if tc.key == "missing" {
				finance.RequestID = ""
			} // 显式空键原样提交，不能由请求封装恢复。
			request := ManageRequest{Id: user.Id, Action: "add_quota", Mode: tc.mode, Value: tc.value, ManualFinanceInput: finance}
			assert.Contains(t, manualFinanceRequest(t, ManageUser, common.RoleRootUser, user.Id, request).Body.String(), `"success":false`)
		})
	}
	// 提权判断必须是服务端约束，财务身份本身不等于钱包管理权限。
	for _, role := range []int{common.RoleCommonUser, common.RoleFinanceUser} {
		request := ManageRequest{Id: user.Id, Action: "add_quota", Mode: "add", Value: 1, ManualFinanceInput: manualFinanceInput("grant", 350+role)}
		assert.Contains(t, manualFinanceRequest(t, ManageUser, role, user.Id, request).Body.String(), `"success":false`)
	}
	require.NoError(t, db.Model(&user).Update("role", common.RoleAdminUser).Error)
	request := ManageRequest{Id: user.Id, Action: "add_quota", Mode: "add", Value: 1, ManualFinanceInput: manualFinanceInput("grant", 399)}
	assert.Contains(t, manualFinanceRequest(t, ManageUser, common.RoleAdminUser, user.Id, request).Body.String(), `"success":false`)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 500000, user.Quota)
	var count int64
	require.NoError(t, db.Model(&model.QuotaLedger{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count) // 所有拒绝都不能生成貌似成功的账本。
	account, err := model.GetCreditAccount(user.Id)
	require.NoError(t, err)
	assert.Zero(t, account.UsedCents)

	// 两个订阅入口都不能绕过必填分类，也不能把钱包撤回原因用于发放权益。
	// 用户是前面建立的同级管理员；Root 能走到财务校验，普通管理员必须被拒绝。
	plan := model.SubscriptionPlan{Title: "invalid-subscription", Enabled: true, DurationUnit: model.SubscriptionDurationCustom, CustomSeconds: 3600, TotalAmount: 700000}
	require.NoError(t, db.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })
	for h, handler := range []gin.HandlerFunc{AdminBindSubscription, AdminCreateUserSubscription} {
		for i, reason := range []string{"", "unclassified", "revoke_grant", "revoke_compensation", "correction", "offline_cash", "on_account"} {
			finance := manualFinanceInput(reason, 700+h*20+i) // 不同入口的独立操作也不共用请求身份。
			// 线下与挂帐故意不填金额；所有无效输入不得先续期再报错。
			request := AdminBindSubscriptionRequest{UserId: user.Id, PlanId: plan.Id, ManualFinanceInput: finance}
			assert.Contains(t, manualFinanceRequest(t, handler, common.RoleRootUser, user.Id, request).Body.String(), `"success":false`)
		}
		valid := manualFinanceInput("grant", 710+h*20)
		request := AdminBindSubscriptionRequest{UserId: user.Id, PlanId: plan.Id, ManualFinanceInput: valid}
		assert.Contains(t, manualFinanceRequest(t, handler, common.RoleAdminUser, user.Id, request).Body.String(), `"success":false`)
		request.Note, request.RequestID = " ", manualFinanceInput("grant", 711+h*20).RequestID // 赠送仍然必须附说明，不能因没有现金而放宽。
		assert.Contains(t, manualFinanceRequest(t, handler, common.RoleRootUser, user.Id, request).Body.String(), `"success":false`)
		request.Note, request.RequestID = valid.Note, ""
		assert.Contains(t, manualFinanceRequest(t, handler, common.RoleRootUser, user.Id, request).Body.String(), `"success":false`)
	}
	var subscriptions int64 // 失败开通不能留下隐形可用权益。
	require.NoError(t, db.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptions).Error)
	assert.Zero(t, subscriptions)
}

// 两个后台开通入口共享同一分类与幂等契约，不给旧入口留下无账本旁路。
// 首次开通和续期都通过控制器，订阅快照必须是权益对象而不是钱包字段。
// 满额已用的订阅继续保持用量，重复投递不能再续一个周期。
func checkManualSubscriptions(t *testing.T, db *gorm.DB) {
	t.Helper()
	for i, reason := range []string{"grant", "compensation", "offline_cash", "on_account"} {
		t.Run(reason, func(t *testing.T) {
			user := manualFinanceUser(t, db, "subscription-"+reason)
			plan := model.SubscriptionPlan{Title: reason, Enabled: true, DurationUnit: model.SubscriptionDurationCustom, CustomSeconds: 3600, TotalAmount: 700000}
			require.NoError(t, db.Create(&plan).Error)
			model.InvalidateSubscriptionPlanCache(plan.Id) // 不复用其他测试库中同 ID 的缓存套餐。
			t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })
			finance := manualFinanceInput(reason, 400+i*2)
			if reason == "offline_cash" {
				finance.CashCents, finance.CashRef = 1000, "subscription-cash"
			}
			if reason == "on_account" {
				finance.ReceivableCents = 1300
			}
			request := AdminBindSubscriptionRequest{UserId: user.Id, PlanId: plan.Id, ManualFinanceInput: finance}
			// 首次开通没有旧实例；重试不能生成第二个活跃实例。
			for range 2 {
				require.Contains(t, manualFinanceRequest(t, AdminBindSubscription, common.RoleRootUser, user.Id, request).Body.String(), `"success":true`)
			}
			var first model.QuotaLedger
			require.NoError(t, db.Where("request_id = ?", finance.RequestID).First(&first).Error)
			var empty []model.UserSubscription
			require.NoError(t, common.UnmarshalJsonStr(first.SubscriptionBefore, &empty))
			assert.Empty(t, empty) // 不允许用用户钱包数值充当首次权益快照。
			var initial model.UserSubscription
			require.NoError(t, common.UnmarshalJsonStr(first.SubscriptionAfter, &initial))
			assert.Equal(t, user.Id, initial.UserId)           // 权益快照归属真实受益人，不是管理员。
			assert.Equal(t, plan.Id, initial.PlanId)           // 快照绑定当次套餐，不读后续可能修改的模板。
			assert.EqualValues(t, 700000, initial.AmountTotal) // 套餐额度不等于 1000/1300 分的收款或应收。
			// 使用固定未来锚点与已耗尽额度，独立验证续期不重置用量。
			require.NoError(t, db.Model(&model.UserSubscription{}).Where("id = ?", initial.Id).Updates(map[string]any{"end_time": int64(4102444800), "amount_used": 700000}).Error)
			finance.RequestID = manualFinanceInput(reason, 401+i*2).RequestID
			create := AdminCreateUserSubscriptionRequest{PlanId: plan.Id, ManualFinanceInput: finance}
			for range 2 {
				require.Contains(t, manualFinanceRequest(t, AdminCreateUserSubscription, common.RoleRootUser, user.Id, create).Body.String(), `"success":true`)
			}
			var ledgers []model.QuotaLedger
			require.NoError(t, db.Where("user_id = ?", user.Id).Order("id").Find(&ledgers).Error)
			require.Len(t, ledgers, 2)          // 两个不同操作各记一笔，重复不会再收钱或增债。
			assert.Equal(t, first, ledgers[0])  // 再次续期不能覆盖首次开通时保存的时间和权益。
			var before []model.UserSubscription // 前快照允许空列表，首次开通与续期均有确定结构。
			var after model.UserSubscription
			require.NoError(t, common.UnmarshalJsonStr(ledgers[1].SubscriptionBefore, &before))
			require.NoError(t, common.UnmarshalJsonStr(ledgers[1].SubscriptionAfter, &after))
			require.Len(t, before, 1)                            // 只有一个活跃实例，不能把历史来源单混入权益列表。
			assert.EqualValues(t, 4102444800, before[0].EndTime) // 固定旧期限，不依赖当前时钟或复制续期算法。
			assert.EqualValues(t, 4102448400, after.EndTime)     // 购买一小时且重试一次，仍只能顺延一小时。
			assert.EqualValues(t, 700000, after.AmountUsed)      // 续期不恢复已经耗尽的本周期额度。
			assert.Equal(t, initial.StartTime, after.StartTime)  // 原始周期锚点不能被本次续费时间替换。
			assert.Equal(t, initial.Id, after.Id)                // 续期延长同一实例，不发第二份并行额度。
			for _, ledger := range ledgers {
				assert.Nil(t, ledger.QuotaBefore)                                // NULL 表示不适用，不能以零冒充钱包前余额。
				assert.Nil(t, ledger.QuotaAfter)                                 // 订阅权益不应污染钱包余额统计。
				assert.Zero(t, ledger.Delta)                                     // 购买时长不能加进钱包净发放额度。
				assert.Equal(t, model.ManualSourceSubscription, ledger.Source)   // 对账必须能区分钱包与订阅来源。
				assert.Equal(t, after.Id, ledger.SourceID)                       // 多次续期共同引用同一个权益实例。
				assert.Equal(t, finance.CashCents, ledger.CashCents)             // 独立操作各收一笔，重试不产生额外来源单。
				assert.Equal(t, finance.ReceivableCents, ledger.ReceivableCents) // 金额取人工输入，不取套餐额度或目录价格。
			}
			create.Note = "同请求换说明" // 同键异参必须拒绝，不能再次延长已有订阅。
			assert.Contains(t, manualFinanceRequest(t, AdminCreateUserSubscription, common.RoleRootUser, user.Id, create).Body.String(), `"success":false`)
			var subs []model.UserSubscription
			require.NoError(t, db.Where("user_id = ?", user.Id).Find(&subs).Error)
			require.Len(t, subs, 1)         // 幂等与冲突都不能额外创建一份并行权益。
			assert.Equal(t, after, subs[0]) // 真实落库对象与操作后快照一致，不能只生成漂亮账本。
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 500000, user.Quota) // 订阅财务入口不借钱包中转金额。
			account, err := model.GetCreditAccount(user.Id)
			require.NoError(t, err)
			wantDebt := int64(0)
			if reason == "on_account" {
				wantDebt = 2600
			}
			assert.Equal(t, wantDebt, account.UsedCents) // 两笔应收共占 2600 分，不按权益数量占额。
			var debts []model.CreditEntry
			require.NoError(t, db.Where("user_id = ?", user.Id).Find(&debts).Error)
			if reason == "on_account" {
				require.Len(t, debts, 2) // 每个独立财务请求各有一笔可追溯债务。
				for _, debt := range debts {
					assert.EqualValues(t, 1300, debt.AmountCents)                // 续期金额不因订阅原有用量而减少。
					assert.Equal(t, model.CreditModuleSubscription, debt.Module) // 不能写入钱包充值模块误导后续结清。
					assert.Equal(t, "outstanding", debt.Status)                  // 发放订阅没有收到现金，不能直接标为结清。
				}
			} else {
				assert.Empty(t, debts)
			}
			var orders int64 // 人工开通不得伪造成功的网关订单或 TopUp 镜像。
			require.NoError(t, db.Model(&model.TopUp{}).Where("user_id = ?", user.Id).Count(&orders).Error)
			assert.Zero(t, orders)
			require.NoError(t, db.Model(&model.SubscriptionOrder{}).Where("user_id = ?", user.Id).Count(&orders).Error)
			assert.Zero(t, orders)
		})
	}
}

// 强制账本 Create 失败，验证异常不能被日志降级策略误吞。
// 钱包和订阅均走真实入口；债务占用必须与失败的来源单一起保持未发生。
// 回调只拦账本表，不模拟金额计算，也不替换任何生产资金函数。
func checkManualFinanceRollback(t *testing.T, db *gorm.DB) {
	t.Helper()
	user := manualFinanceUser(t, db, "manual-rollback")
	plan := model.SubscriptionPlan{Title: "rollback", Enabled: true, DurationUnit: model.SubscriptionDurationCustom, CustomSeconds: 3600, TotalAmount: 700000}
	require.NoError(t, db.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:manual_ledger_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "quota_ledgers" {
			tx.AddError(errors.New("injected ledger create failure"))
		}
	}))
	t.Cleanup(func() { assert.NoError(t, db.Callback().Create().Remove("test:manual_ledger_failure")) })
	input := manualFinanceInput("on_account", 500)
	input.ReceivableCents = 1300
	quota := ManageRequest{Id: user.Id, Action: "add_quota", Mode: "add", Value: 500000, ManualFinanceInput: input}
	assert.Contains(t, manualFinanceRequest(t, ManageUser, common.RoleRootUser, user.Id, quota).Body.String(), `"success":false`)
	input.RequestID = manualFinanceInput("on_account", 501).RequestID // 两个失败操作仍保持不同请求身份。
	subscription := AdminBindSubscriptionRequest{UserId: user.Id, PlanId: plan.Id, ManualFinanceInput: input}
	assert.Contains(t, manualFinanceRequest(t, AdminBindSubscription, common.RoleRootUser, user.Id, subscription).Body.String(), `"success":false`)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 500000, user.Quota)
	account, err := model.GetCreditAccount(user.Id)
	require.NoError(t, err)
	assert.Zero(t, account.UsedCents) // 未成功发放的权益不能先占授信。
	for _, table := range []any{&model.PaymentRefund{}, &model.QuotaLedger{}, &model.CreditEntry{}, &model.UserSubscription{}} {
		var count int64
		require.NoError(t, db.Model(table).Where("user_id = ?", user.Id).Count(&count).Error)
		assert.Zero(t, count) // 任一表留下半笔记录都是事务原子性回归。
	}
}

// 正式 InitDB 负责迁移接入，不能只直接 AutoMigrate 账本后宣称启动会创建它。
// 升级基线保存此前版本真实存在的钱包、TopUp 和日志，无账本不回填虚构现金。
// 两次重启后再重放真实人工请求，验证旧数据、唯一索引和幂等记录同时保留。
func TestManualFinanceStartupMigrationDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		for _, upgrade := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/upgrade=%v", dialect, upgrade), func(t *testing.T) {
				dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
				if dialect != "sqlite" && dsn == "" {
					t.Skip("dedicated test database DSN is not configured")
				}
				db := modelManagementDB(t, dialect, dsn)
				assert.False(t, db.Migrator().HasTable(&model.QuotaLedger{})) // 夹具未偷建账本，才能证明正式启动确实接入迁移。
				user := model.User{Username: "legacy-finance", AffCode: "legacy-finance", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 500000, AuthVersion: 1}
				require.NoError(t, db.Create(&user).Error)
				if upgrade {
					require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.Log{}))
					require.NoError(t, db.Create(&model.TopUp{UserId: user.Id, TradeNo: "legacy-finance-cash", Money: 10, Amount: 500000, Status: common.TopUpStatusSuccess}).Error)
					require.NoError(t, db.Create(&model.Log{UserId: user.Id, Type: model.LogTypeTopup, Content: "legacy manual adjustment"}).Error)
				}
				for iteration := range 2 {
					// 每次都从正式启动迁移走到 schema，临时连接关闭后恢复夹具句柄。
					common.IsMasterNode = true // 从节点跳过迁移，必须以正式主节点分支验证接线。
					err := model.InitDB()      // 沿用隔离器设置的测试 DSN，不接触环境中的业务连接。
					migratedDB := model.DB
					model.DB, model.LOG_DB = db, db // 迁移连接和业务夹具指向同一隔离库，后续入口保持既有连接。
					connection, connectionErr := migratedDB.DB()
					require.NoError(t, connectionErr)
					require.NoError(t, connection.Close())                                                     // 避免重复启动留下连接妨碍外部库清理。
					require.NoError(t, err)                                                                    // 启动遇到 schema 错误必须使测试失败，不能容忍半迁移。
					assert.True(t, db.Migrator().HasTable(&model.QuotaLedger{}))                               // 以原连接读取，确认迁移已经真实提交。
					assert.True(t, db.Migrator().HasIndex(&model.QuotaLedger{}, "idx_manual_finance_request")) // 重启不得丢失幂等索引。
					if iteration == 0 {
						var count int64
						require.NoError(t, db.Model(&model.QuotaLedger{}).Count(&count).Error)
						assert.Zero(t, count) // 历史日志不能推算为现金来源单。
					}
					input := manualFinanceInput("grant", 600)
					request := ManageRequest{Id: user.Id, Action: "add_quota", Mode: "add", Value: 500000, ManualFinanceInput: input}
					require.Contains(t, manualFinanceRequest(t, ManageUser, common.RoleRootUser, user.Id, request).Body.String(), `"success":true`)
				}
				require.NoError(t, db.First(&user, user.Id).Error)
				assert.Equal(t, 1000000, user.Quota) // 再次启动后的同请求仍只能增加一次。
				var entries []model.QuotaLedger
				require.NoError(t, db.Find(&entries).Error)
				require.Len(t, entries, 1) // 第二次启动不能回填出另一张相同人工来源单。
				duplicate := entries[0]
				duplicate.ID, duplicate.TradeNo = 0, "different-manual-trade" // 排除主键和单号冲突，只检测操作者加请求键的唯一性。
				require.Error(t, db.Create(&duplicate).Error)                 // 同操作者请求键的数据库唯一约束不能在重迁移后消失。
				if upgrade {
					var topup model.TopUp
					require.NoError(t, db.Where("trade_no = ?", "legacy-finance-cash").First(&topup).Error)
					assert.Equal(t, 10.0, topup.Money)          // 已有订单的元金额不因新表统一按分记录而乘除百倍。
					assert.EqualValues(t, 500000, topup.Amount) // 旧支付额度保持原整数值，不随新增账本换算。
					var logs int64
					require.NoError(t, db.Model(&model.Log{}).Where("content = ?", "legacy manual adjustment").Count(&logs).Error)
					assert.EqualValues(t, 1, logs) // 旧审计记录保留，但不转为新分类现金。
				}
			})
		}
	}
}
