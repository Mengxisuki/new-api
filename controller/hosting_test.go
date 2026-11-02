package controller

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/hosting"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// hostingTestDB 复用安全模块的真实会话与隔离数据库，外部数据库从测试环境选择。
// SQLite、MySQL、PostgreSQL 都执行同一组公共行为，不以模拟方言代替迁移验证。
// 运行时选择真实数据库方言，同一行为断言不得按方言跳过。
// 用户、会话和审计表来自安全测试的既有初始化流程。
// 挂靠表在每个隔离数据库中新建，避免依赖其他用例的迁移顺序。
// 测试私钥通过 t.Setenv 自动恢复，不修改工作区部署配置。
// 每个用例都有独立数据库，不能连接并清理开发者已有业务表。
// 旧模型 Token 表必须真实创建，策略归属校验不使用 mock。
// 本 helper 不开启原厂网络，控制面仅由具体用例的本地服务器承接。
// 测试结束恢复全局数据库句柄，不能污染安全模块其他回归。
func hostingTestDB(t *testing.T) (*model.User, service.AuthIdentity) {
	t.Helper()
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.HostingAccount{}, &model.HostingAuthorization{}, &model.HostingResetOperation{}, &model.ResourcePolicy{}))
	// 加密密钥仅为本测试进程的固定材料，不接触部署凭据。
	t.Setenv("CRYPTO_SECRET", strings.Repeat("test-secret-", 4))
	return user, identity
}

// hostingTestRequest 走真实 UserAuth，确保会话认证和处理器归属检查一起执行。
// 每次生成一条确切模板路由，敏感证明仍交给现有验证服务消费。
// 路径模板和实际 URL 分开传入，确保 Gin 参数行为与正式路由一致。
// 访问令牌由真实签发函数产生，不能直接手填用户上下文绕过认证。
// 证明通过正式请求头传入，真实验证代码负责消费和重放拒绝。
// 请求正文经过实际绑定和体积限制，测试不直接调用模型层替代 API。
// 审计中间件一起执行，能够验证失败操作也留下脱敏记录。
// 本地路由不引入服务启动或网络端口，响应由 httptest 收集。
// 此 helper 不模拟账号归属、会话版本或策略选择。
// 正式路由组的 PAT 路径覆盖另由路由契约测试验证。
func hostingTestRequest(t *testing.T, identity service.AuthIdentity, method, pattern, path, body, proof string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	// 为同一会话签发访问令牌，不绕过实际中间件的版本和吊销检查。
	token, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	engine := gin.New()
	engine.Handle(method, pattern, middleware.UserAuth(), AuditHostingRequests, handler)
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Security-Proof", proof)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

// hostingTestProof 为具体动作申请真实单次证明，不能跨账号或 UUID 通用。
// 证明上下文使用与生产相同的结构和规范化规则。
// 动作、账号与 UUID 分别传入，测试可以构造跨动作误用。
// 真实安全流程表记录证明状态，单次消费不靠内存计数器模拟。
// 证明绑定当前测试会话，另一会话不能仅凭用户编号接手。
// 密码验证方式沿用现有安全测试支持的可信签发 helper。
// 业务处理器仍需要执行验证消费，helper 不替它设置授权标记。
// 测试得到的原始证明只放在请求头，不记录在挂靠业务表。
// 无效证明的用例通过错误上下文或已消费证明构造，不篡改验证实现。
func hostingTestProof(t *testing.T, identity service.AuthIdentity, action string, accountID int, operationID string) string {
	t.Helper()
	data, err := common.Marshal(service.HostingManageContext{Action: action, AccountID: accountID, OperationID: operationID})
	require.NoError(t, err)
	return issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopeHostingManage, Context: data}, "password")
}

// hostingTestControlAuthorization 使用实际请求字节核验真实控制面断言。
// 模拟执行层只隔离网络，不能用固定管理密钥跳过新授权机制。
// 读取后恢复正文，后续业务响应仍然解析同一份请求字节。
// 摘要包括空正文；查询串和幂等键均直接取自收到的 HTTP 请求。
// 返回的归属已经过签名和当前用户状态检查，可供具体测试断言。
func hostingTestControlAuthorization(t *testing.T, request *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	request.Body = io.NopCloser(bytes.NewReader(body))
	digest := sha256.Sum256(body)
	binding := hosting.RequestBinding{Method: request.Method, RequestURI: request.URL.RequestURI(), BodySHA256: hex.EncodeToString(digest[:]), IdempotencyKey: request.Header.Get("Idempotency-Key")}
	owner, err := hosting.VerifyRequestAuthorization(strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "), binding)
	require.NoError(t, err)
	return owner
}

// TestHostingPolicyContracts 验证每个 Key 独立维护来源，不存在用户全局继承。
// 无记录沿用平台合同，显式空数组则禁用全部来源，二者不能互换。
// 本人多个 Key 互不影响，旧全局保留编号不再接受读写。
// 版本冲突必须显式拒绝，不能覆盖用户刚保存的付费授权。
// 他人 Key、他人账号与重复来源不能进入策略。
// 删除只恢复当前 Key 的既有平台合同，不修改其他 Key。
// 损坏 JSON 必须失败，不能静默恢复平台收费。
func TestHostingPolicyContracts(t *testing.T) {
	user, identity := hostingTestDB(t)
	tokens := []model.Token{{UserId: user.Id, Key: "hosting-policy-a"}, {UserId: user.Id, Key: "hosting-policy-b"}, {UserId: user.Id + 1, Key: "hosting-policy-foreign"}}
	require.NoError(t, model.DB.Create(&tokens).Error)
	account := model.HostingAccount{UserID: user.Id, Provider: "codex", Alias: "本人", RemoteRef: uuid.NewString(), Status: "connected"}
	require.NoError(t, model.DB.Create(&account).Error)
	snapshot, err := model.GetEffectiveResourcePolicy(user.Id, tokens[0].Id)
	require.NoError(t, err)
	assert.Nil(t, snapshot) // 缺行保留既有合同，不能被解释为禁用全部。
	// 全局编号不能通过内部 API 重新引入，迁移只允许直接处理历史行。
	_, err = model.SaveResourcePolicy(user.Id, 0, 0, nil)
	require.ErrorIs(t, err, model.ErrResourcePolicyInvalid)
	_, err = model.GetEffectiveResourcePolicy(user.Id, 0)
	require.ErrorIs(t, err, model.ErrResourcePolicyInvalid)
	sources := []model.ResourceSource{{Kind: "hosting", AccountID: account.ID, Enabled: true}, {Kind: "wallet", Enabled: true}}
	_, err = model.SaveResourcePolicy(user.Id, tokens[0].Id, 0, sources)
	require.NoError(t, err)
	snapshot, err = model.GetEffectiveResourcePolicy(user.Id, tokens[0].Id)
	require.NoError(t, err)
	assert.Equal(t, sources, snapshot.Sources)                                        // 挂靠在钱包前的显式顺序必须原样返回。
	assert.Equal(t, fmt.Sprintf("%d:%d:1", user.Id, tokens[0].Id), snapshot.Revision) // 日志版本应绑定具体 Key。
	// 编辑 A 后，B 依然没有策略，避免一个设置页改变其他客户端的费用。
	snapshot, err = model.GetEffectiveResourcePolicy(user.Id, tokens[1].Id)
	require.NoError(t, err)
	assert.Nil(t, snapshot)
	_, err = model.SaveResourcePolicy(user.Id, tokens[1].Id, 0, nil)
	require.NoError(t, err)
	snapshot, err = model.GetEffectiveResourcePolicy(user.Id, tokens[1].Id)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	assert.Empty(t, snapshot.Sources) // 非 nil 快照与空列表共同表达用户主动禁用。
	// 保存空数组也产生版本，旧页面不能再以首次创建版本覆盖它。
	_, err = model.SaveResourcePolicy(user.Id, tokens[1].Id, 0, nil)
	require.ErrorIs(t, err, model.ErrResourcePolicyConflict) // 这是并发编辑冲突，不能归为输入损坏。
	_, err = model.SaveResourcePolicy(user.Id, tokens[0].Id, 1, []model.ResourceSource{{Kind: "wallet"}, {Kind: "wallet"}})
	require.ErrorIs(t, err, model.ErrResourcePolicyInvalid)
	// 用户本人存在也不能替他人的 Key 建立策略；不能只检查用户记录。
	_, err = model.SaveResourcePolicy(user.Id, tokens[2].Id, 0, sources)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	// 来源归属在保存时验证，不能把他人的账号写入本人合法 Key。
	require.NoError(t, model.DB.Model(&account).Update("user_id", user.Id+1).Error)
	_, err = model.SaveResourcePolicy(user.Id, tokens[0].Id, 1, sources)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	// 走实际 DELETE 响应验证新版 DTO，不接受旧 inherited/effective_revision 字段残留。
	path := fmt.Sprintf("/api/hosting/tokens/%d/policy", tokens[1].Id)
	response := hostingTestRequest(t, identity, "DELETE", "/api/hosting/tokens/:token_id/policy", path, "", "", DeleteHostingResourcePolicy)
	assert.Equal(t, 200, response.Code)
	assert.JSONEq(t, `{"success":true,"message":"","data":{"revision":0,"legacy":true,"sources":[{"kind":"subscription","enabled":true},{"kind":"wallet","enabled":true}]}}`, response.Body.String())
	// A 保留原来的策略，即使 B 删除，也不会重新查询任何全局来源。
	snapshot, err = model.GetEffectiveResourcePolicy(user.Id, tokens[0].Id)
	require.NoError(t, err)
	assert.Equal(t, sources, snapshot.Sources)
	// 人为破坏持久 JSON，区分数据库内容损坏和合法的无策略状态。
	require.NoError(t, model.DB.Model(&model.ResourcePolicy{}).Where("user_id = ? AND token_id = ?", user.Id, tokens[0].Id).Update("sources_json", "broken").Error)
	_, err = model.GetEffectiveResourcePolicy(user.Id, tokens[0].Id)
	require.Error(t, err) // 损坏不能降级为钱包可用，从而改变费用授权。
}

// TestTokenResourcePolicyAtomicEditing 通过真实会话验证 Key 字段与策略同事务保存。
// 新建默认授权顺序固定，但旧 Key 普通编辑不得悄悄获得新收费来源。
// 明确空策略可以保存，省略和 null 不能混淆。
// 版本冲突同时回滚名称，避免界面提示失败但普通字段已经变更。
// 创建失败也不能留下没有策略的半成品 Key。
// GET 详情和独立策略接口必须返回相同编辑投影。
// 所有断言重新读取真实数据库，测试本身不拼接执行时策略。
// 版本错误采用冲突状态，输入错误采用校验状态，两者都不能残留新 Key。
// 控制台请求采用真实会话身份，不直接伪造用户上下文来替代鉴权。
func TestTokenResourcePolicyAtomicEditing(t *testing.T) {
	user, identity := hostingTestDB(t)
	create := func(extra string) *httptest.ResponseRecorder {
		return hostingTestRequest(t, identity, "POST", "/api/token/", "/api/token/", `{"name":"atomic-key","expired_time":-1,"unlimited_quota":true`+extra+`}`, "", AddToken)
	}
	response := create("")
	require.Equal(t, 200, response.Code, response.Body.String())
	var created struct {
		Data struct {
			ID             int                      `json:"id"`
			ResourcePolicy model.ResourcePolicyView `json:"resource_policy"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &created))
	require.Positive(t, created.Data.ID)
	require.Equal(t, int64(1), created.Data.ResourcePolicy.Revision)
	require.False(t, created.Data.ResourcePolicy.Legacy)
	require.Equal(t, []model.ResourceSource{{Kind: "wallet", Enabled: true}, {Kind: "subscription", Enabled: true}, {Kind: "hosting_pool", Enabled: true}}, created.Data.ResourcePolicy.Sources)
	// 不暴露完整 Key；创建响应只用于刷新界面，明文仍走已有单独接口。
	saved, err := model.GetTokenByIds(created.Data.ID, user.Id)
	require.NoError(t, err)
	require.NotContains(t, response.Body.String(), saved.Key)
	// 无效新策略必须回滚已创建的令牌行，而非仅拒绝来源子表。
	for _, extra := range []string{`,"resource_policy":null`, `,"resource_policy":{"revision":0}`, `,"resource_policy":{"revision":0,"sources":[{"kind":"invalid","enabled":true}]}`} {
		invalid := create(extra)
		require.Equal(t, 400, invalid.Code, invalid.Body.String())
	}
	count, err := model.CountUserTokens(user.Id)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	// 旧版本保存名称与策略必须整体失败，不能造成部分更新。
	body := fmt.Sprintf(`{"id":%d,"name":"must-rollback","expired_time":-1,"unlimited_quota":true,"resource_policy":{"revision":0,"sources":[]}}`, saved.Id)
	conflict := hostingTestRequest(t, identity, "PUT", "/api/token/", "/api/token/", body, "", UpdateToken)
	require.Equal(t, 409, conflict.Code, conflict.Body.String())
	saved, err = model.GetTokenByIds(saved.Id, user.Id)
	require.NoError(t, err)
	require.Equal(t, "atomic-key", saved.Name)
	// 明确关闭全部来源是合法策略；普通改名随后省略策略应保留该拒绝授权。
	body = strings.ReplaceAll(body, `"revision":0`, `"revision":1`)
	updated := hostingTestRequest(t, identity, "PUT", "/api/token/", "/api/token/", body, "", UpdateToken)
	require.Equal(t, 200, updated.Code, updated.Body.String())
	view, err := model.GetResourcePolicyView(user.Id, saved.Id)
	require.NoError(t, err)
	require.Equal(t, int64(2), view.Revision)
	require.Empty(t, view.Sources)
	// 详情采用未展开的可编辑投影，不能把动态池快照写回用户设置。
	path := fmt.Sprintf("/api/token/%d", saved.Id)
	detail := hostingTestRequest(t, identity, "GET", "/api/token/:id", path, "", "", GetToken)
	require.Equal(t, 200, detail.Code, detail.Body.String())
	require.NoError(t, common.Unmarshal(detail.Body.Bytes(), &created))
	require.Equal(t, *view, created.Data.ResourcePolicy)
	// 仅状态开关不能暗中携带费用授权变更。
	status := hostingTestRequest(t, identity, "PUT", "/api/token/", "/api/token/?status_only=true", body, "", UpdateToken)
	require.Equal(t, 400, status.Code, status.Body.String())
}

// TestTokenResourcePolicyLegacyAndPool 验证旧合同只读投影与动态本人账号池。
// 默认钱包顺序仅适用于新建，四类旧偏好都不自动迁移。
// 池展开只取本人测试通过且未暂停账号，授权成功不等于可用。
// 新接入可用账号影响下一请求，但不会改变持久策略版本。
// 显式账号策略不自动变池，池和显式账号也不能混合扩大授权。
// 三库复用同一隔离夹具，不能按方言跳过归属和回滚断言。
func TestTokenResourcePolicyLegacyAndPool(t *testing.T) {
	user, identity := hostingTestDB(t)
	token := &model.Token{UserId: user.Id, Key: "legacy-pool", Name: "legacy", ExpiredTime: -1}
	require.NoError(t, model.DB.Create(token).Error)
	for preference, kinds := range map[string][]string{"wallet_only": {"wallet"}, "subscription_only": {"subscription"}, "wallet_first": {"wallet", "subscription"}, "subscription_first": {"subscription", "wallet"}} {
		setting := user.GetSetting()
		setting.BillingPreference = preference
		require.NoError(t, model.UpdateUserSetting(user.Id, setting))
		view, err := model.GetResourcePolicyView(user.Id, token.Id)
		require.NoError(t, err)
		require.True(t, view.Legacy)
		var actual []string
		for _, source := range view.Sources {
			actual = append(actual, source.Kind)
		}
		require.Equal(t, kinds, actual)
		// 读取不落库；普通改名仍保留旧偏好的执行分支及溢出限制。
		body := fmt.Sprintf(`{"id":%d,"name":"legacy-renamed","expired_time":-1}`, token.Id)
		response := hostingTestRequest(t, identity, "PUT", "/api/token/", "/api/token/", body, "", UpdateToken)
		require.Equal(t, 200, response.Code, response.Body.String())
		snapshot, err := model.GetEffectiveResourcePolicy(user.Id, token.Id)
		require.NoError(t, err)
		require.Nil(t, snapshot)
	}
	accounts := []model.HostingAccount{
		{UserID: user.Id, RemoteRef: "pool-active", Status: "active"},
		{UserID: user.Id, RemoteRef: "pool-paused", Status: "active", Paused: true},
		{UserID: user.Id, RemoteRef: "pool-pending", Status: "connected"},
		{UserID: user.Id + 1, RemoteRef: "pool-foreign", Status: "active"},
		{UserID: user.Id, RemoteRef: "pool-provisioning", Status: "provisioning"},
		{UserID: user.Id, RemoteRef: "pool-error", Status: "error"},
		{UserID: user.Id, RemoteRef: "pool-deleted", Status: "deleted"},
		{UserID: user.Id, RemoteRef: "pool-deleting", Status: "deleting"},
		{UserID: user.Id, RemoteRef: "pool-connected-paused", Status: "connected", Paused: true},
		{UserID: user.Id + 1, RemoteRef: "pool-foreign-connected", Status: "connected"},
	}
	require.NoError(t, model.DB.Create(&accounts).Error)
	_, err := model.SaveResourcePolicy(user.Id, token.Id, 0, []model.ResourceSource{{Kind: "hosting_pool", Enabled: true}})
	require.NoError(t, err)
	snapshot, err := model.GetEffectiveResourcePolicy(user.Id, token.Id)
	require.NoError(t, err)
	require.Equal(t, []model.ResourceSource{{Kind: "hosting", AccountID: accounts[0].ID, Enabled: true}}, snapshot.Sources)
	// 解除暂停只改变动态可用集合，不要求修改每一把 Key。
	require.NoError(t, model.DB.Model(&accounts[1]).Update("paused", false).Error)
	next, err := model.GetEffectiveResourcePolicy(user.Id, token.Id)
	require.NoError(t, err)
	require.Len(t, next.Sources, 2)
	require.Equal(t, snapshot.Revision, next.Revision)
	require.Equal(t, accounts[1].ID, next.Sources[1].AccountID)
	// 编辑仍返回池标识，不能将请求快照误作为新的显式账号授权。
	view, err := model.GetResourcePolicyView(user.Id, token.Id)
	require.NoError(t, err)
	require.Equal(t, []model.ResourceSource{{Kind: "hosting_pool", Enabled: true}}, view.Sources)
	_, err = model.SaveResourcePolicy(user.Id, token.Id, 1, []model.ResourceSource{{Kind: "hosting_pool", Enabled: true}, {Kind: "hosting", AccountID: accounts[0].ID, Enabled: true}})
	require.ErrorIs(t, err, model.ErrResourcePolicyInvalid)
}

// TestTokenResourcePolicyRegistrationDefault 验证注册赠送 Key 不绕过统一新建合同。
// 走真实匿名注册处理器，用户与 Key 都由生产逻辑创建。
// 创建来源不依赖旧默认 BillingPreference，也无需用户先打开来源编辑器。
// 测试暂时开启本地注册和默认 Key，不修改部署配置。
// 空邮箱不触发邮件验证或外部通知，验证只依赖隔离数据库。
// 余额赠送设为零，避免测试引入与来源保存无关的消费日志。
// 所有全局开关在结束时恢复，不能影响后续鉴权与注册用例。
// 最终读取新用户的实际 Token 策略，不能只依据注册成功信封推断。
func TestTokenResourcePolicyRegistrationDefault(t *testing.T) {
	hostingTestDB(t)
	register, password, email, generate, quota := common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled, constant.GenerateDefaultToken, common.QuotaForNewUser
	common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled, constant.GenerateDefaultToken, common.QuotaForNewUser = true, true, false, true, 0
	t.Cleanup(func() {
		common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled, constant.GenerateDefaultToken, common.QuotaForNewUser = register, password, email, generate, quota
	})
	engine := gin.New()
	engine.POST("/api/user/register", Register)
	request := httptest.NewRequest("POST", "/api/user/register", strings.NewReader(`{"username":"registration-policy","password":"RegistrationPassword123!"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	// 注册成功必须同时拥有已保存的默认 Key，不接受只创建用户后假报完成。
	require.True(t, decodeAPIResponse(t, response).Success, response.Body.String())
	var owner model.User
	require.NoError(t, model.DB.Where("username = ?", "registration-policy").First(&owner).Error)
	var token model.Token
	require.NoError(t, model.DB.Where("user_id = ?", owner.Id).First(&token).Error)
	// 无存量迁移：这里只检查本次新增用户的 Key，并保留默认来源的精确顺序。
	view, err := model.GetResourcePolicyView(owner.Id, token.Id)
	require.NoError(t, err)
	require.False(t, view.Legacy)
	require.Equal(t, []model.ResourceSource{{Kind: "wallet", Enabled: true}, {Kind: "subscription", Enabled: true}, {Kind: "hosting_pool", Enabled: true}}, view.Sources)
}

// TestHostingFreshUpgradeRepeatMigration 先保留真实业务行，再模拟尚无验证字段的旧表。
// 重复迁移后数据、唯一约束和默认版本必须仍可正常读取。
// 首次建表之后写入真实业务记录，确保迁移不是只验证空表。
// 移除验证字段模拟尚未具备测试标记的旧版本结构。
// 再次 AutoMigrate 必须补回字段并保留原账号备注与归属。
// 随后立即重复迁移，验证启动多次不会破坏已升级表。
// 验证字段默认未知，迁移不能凭空把旧账号变成已实测。
// 每个方言执行相同列变更，覆盖真实 DDL 而非字符串快照。
// 隔离数据库由既有 helper 创建，测试不会删除环境变量指向的数据库。
// 重复迁移的验收依据是保留的数据和可用查询，不只检查命令退出码。
func TestHostingFreshUpgradeRepeatMigration(t *testing.T) {
	user, _ := hostingTestDB(t)
	account := model.HostingAccount{UserID: user.Id, Provider: "codex", Alias: "升级保留", RemoteRef: uuid.NewString(), Status: "connected"}
	require.NoError(t, model.DB.Create(&account).Error)
	for _, column := range []string{"verified_at", "verified_model", "verified_protocol"} {
		require.NoError(t, model.DB.Migrator().DropColumn(&model.HostingAccount{}, column))
	}
	for pass := 0; pass < 2; pass++ {
		require.NoError(t, model.DB.AutoMigrate(&model.HostingAccount{}, &model.HostingAuthorization{}, &model.HostingResetOperation{}, &model.ResourcePolicy{}))
	}
	// 新增字段不能把既有账号误标成可用；真实测试仍是启用条件。
	stored, err := model.GetHostingAccount(user.Id, account.ID)
	require.NoError(t, err)
	require.Equal(t, account.Alias, stored.Alias) // 升级不能把用户已有的区分备注丢掉。
	require.Zero(t, stored.VerifiedAt)
	require.Empty(t, stored.VerifiedModel) // 迁移没有资格宣称某个模型已经真实验证。
}

// TestHostingAuthorizationAndRecovery 使用本机测试服务器隔离唯一外部依赖。
// 数据库、密文、授权状态、控制请求及后台恢复均执行真实实现。
// 本地控制服务器模拟执行层协议，真实业务服务负责所有状态推进。
// 服务器检查管理 Authorization 与 owner_ref，防止调用漏传归属。
// 创建请求的幂等键必须等于授权操作 UUID，不能产生第二个意图。
// 故意让第一次 activate 失败，形成可观察的半完成状态。
// 失败后检查密文已持久化且 JSON 不泄露密文或远端引用。
// 修改更新时间模拟进程退出后的恢复窗口，无需等待真实分钟数。
// 后台恢复只依赖数据库记录，不能依赖原请求的内存对象。
// 恢复后仍是 connected，只有真实模型测试才能进一步激活。
// 再次创建同授权不增加远端调用次数，验证业务级幂等。
// 最终删除必须清除密钥并保留墓碑，避免用户历史丢失。
func TestHostingAuthorizationAndRecovery(t *testing.T) {
	user, identity := hostingTestDB(t)
	var remoteRef, operationID string
	var createCalls, activateCalls, deleteCalls int
	failActivate := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 控制面归属和请求断言必须由 Oak 生成，不能来自浏览器正文。
		require.Equal(t, hosting.OwnerRef(user.Id), hostingTestControlAuthorization(t, r))
		require.Equal(t, hosting.OwnerRef(user.Id), r.URL.Query().Get("owner_ref"))
		var data any
		switch {
		case r.URL.Path == "/api/hosting/v1/authorizations":
			var input map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			remoteRef, operationID = input["account_ref"], input["operation_id"]
			data = gin.H{"stage_id": "stage-test", "status": "ready", "authorization_url": "https://auth.example/authorize", "redirect_uri": "http://localhost:1455/auth/callback", "expires_at_ms": time.Now().Add(time.Minute).UnixMilli()}
		case r.URL.Path == "/api/hosting/v1/accounts":
			createCalls++
			require.Equal(t, operationID, r.Header.Get("Idempotency-Key"))
			// 相同操作重放相同受限 Key，模拟执行层已验证的幂等边界。
			data = gin.H{"account_ref": remoteRef, "owner_ref": hosting.OwnerRef(user.Id), "provider": "codex", "group_id": 1, "credential_id": 2, "access_key_id": 3, "identity_fingerprint": "identity-test", "internal_key": "restricted-secret", "status": "pending"}
		case strings.HasSuffix(r.URL.Path, "/activate"):
			activateCalls++
			if failActivate {
				w.WriteHeader(503)
				return
			}
			data = gin.H{"status": "active"}
		case r.Method == http.MethodPatch:
			data = gin.H{"status": "paused"}
		case r.Method == http.MethodDelete:
			deleteCalls++
			data = gin.H{"status": "deleted"}
		default:
			t.Errorf("unexpected control request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(gin.H{"code": 0, "data": data}))
	}))
	defer server.Close()
	t.Setenv("OAK_GPT_LOAD_URL", server.URL)
	authorization, err := hosting.BeginAuthorization(context.Background(), identity, 0)
	require.NoError(t, err)
	// 同一用户另一会话也不能接手已经通过敏感验证的授权。
	other := identity
	other.SessionID = "other-session" // 用户相同但敏感验证所属会话不同。
	_, err = hosting.LoadAuthorization(context.Background(), other, authorization.ID, false)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = hosting.CompleteAuthorization(context.Background(), identity, authorization.ID, "https://evil.example/auth/callback?code=x&state=y")
	require.Error(t, err)
	// 未完成授权不能抢占一个永远无法激活的本地账号。
	require.NoError(t, model.DB.Model(&model.HostingAuthorization{}).Where("id = ?", authorization.ID).Update("status", "pending_authorization").Error)
	_, err = hosting.CreateAccount(context.Background(), identity, authorization.ID, "提前消费")
	require.ErrorIs(t, err, model.ErrHostingConflict)
	require.Zero(t, createCalls)
	require.NoError(t, model.DB.Model(&model.HostingAuthorization{}).Where("id = ?", authorization.ID).Update("status", "ready").Error)
	account, err := hosting.CreateAccount(context.Background(), identity, authorization.ID, "本人 Codex")
	require.Error(t, err)
	require.NotNil(t, account) // 半完成资源必须有持久本地身份供恢复。
	// 激活失败时，密钥已加密保存但账号不可调用，授权身份仍可恢复。
	stored, err := model.GetHostingAccount(user.Id, account.ID)
	require.NoError(t, err)
	require.Equal(t, "provisioning", stored.Status)                  // 远端尚未确认激活时禁止数据面使用。
	require.NotContains(t, stored.EncryptedKey, "restricted-secret") // 持久凭据不能以可直接使用的明文出现。
	encoded, err := json.Marshal(stored)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), stored.EncryptedKey)
	require.NotContains(t, string(encoded), remoteRef) // 公开投影不能泄露执行层资源定位信息。
	// 模拟进程退出后的下一轮任务：只依赖数据库，无内存恢复状态。
	require.NoError(t, model.DB.Model(stored).Update("updated_at", time.Now().Add(-3*time.Minute).Unix()).Error)
	failActivate = false
	require.NoError(t, hosting.ReconcilePendingAccounts(context.Background()))
	stored, err = model.GetHostingAccount(user.Id, account.ID)
	require.NoError(t, err)
	require.Equal(t, "connected", stored.Status) // 授权接通不等于模型测试通过。
	require.Zero(t, stored.VerifiedAt)
	require.Equal(t, 2, createCalls) // 恢复只能重放原操作，不能由读取额外触发创建。
	require.Equal(t, 2, activateCalls)
	// 已消费创建重放读取原账号，不再次调用执行层或覆盖验证状态。
	replay, err := hosting.CreateAccount(context.Background(), identity, authorization.ID, "不同备注")
	require.NoError(t, err)
	require.Equal(t, account.ID, replay.ID)
	require.Equal(t, 2, createCalls) // 恢复只能重放原操作，不能由读取额外触发创建。
	// 重复暂停和相同备注是幂等操作，MySQL 零变更行数不能误报冲突。
	paused := true
	for attempt := 0; attempt < 2; attempt++ {
		_, err = hosting.ChangeAccount(context.Background(), user.Id, account.ID, nil, &paused)
		require.NoError(t, err)
	}
	require.NoError(t, hosting.DeleteAccount(context.Background(), user.Id, account.ID))
	require.Equal(t, 1, deleteCalls)
	stored, err = model.GetHostingAccount(user.Id, account.ID)
	require.NoError(t, err)
	require.Equal(t, "deleted", stored.Status)
	require.Empty(t, stored.EncryptedKey) // 执行层确认删除后必须消除本地可用凭据。
}

// TestHostingProofOwnershipAndReset 覆盖实际认证入口和不可逆动作的单次证明。
// 只模拟执行层传输结果，证明、账户归属、操作互斥和历史查询都用真实代码。
// 此用例真实通过中间件验证用户，并由处理器消费安全证明。
// 外部服务器只统计写请求，读状态不得增加消费次数。
// 缺失证明和错误动作证明都必须在请求执行层之前失败。
// 第一次合法消费故意丢失明确结果，数据库应保存未知状态。
// 相同证明重放不能再次调用执行层，避免双击触发重复动作。
// 新 UUID 也被未决记录阻止，说明保护并非只依赖请求幂等头。
// 查询原 UUID 可以读取未知结果，但不能偷偷恢复消费。
// 他人账号详情统一返回不存在，避免通过本地编号枚举信息。
// 审计必须存在，但不包含证明或控制面请求断言。
// 这些断言覆盖真实业务失败条件，不把 mock 的返回值当作最终结论。
func TestHostingProofOwnershipAndReset(t *testing.T) {
	user, identity := hostingTestDB(t)
	account := model.HostingAccount{UserID: user.Id, Provider: "codex", Alias: "本人", RemoteRef: uuid.NewString(), Status: "active"}
	require.NoError(t, model.DB.Create(&account).Error)
	calls := 0
	var assertions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, hosting.OwnerRef(user.Id), hostingTestControlAuthorization(t, r))
		assertions = append(assertions, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if r.Method == http.MethodPost {
			calls++
			w.WriteHeader(502)
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(gin.H{"code": 0, "data": gin.H{"status": "outcome_unknown"}}))
	}))
	defer server.Close()
	t.Setenv("OAK_GPT_LOAD_URL", server.URL)
	path := fmt.Sprintf("/api/hosting/accounts/%d/reset-credits/consume", account.ID)
	operationID := uuid.NewString()
	body := fmt.Sprintf(`{"operation_id":%q}`, operationID)
	request := func(proof string) *httptest.ResponseRecorder {
		return hostingTestRequest(t, identity, "POST", "/api/hosting/accounts/:id/reset-credits/consume", path, body, proof, ConsumeHostingResetCredit)
	}
	// 未证明或证明属于另一动作时，外部消费计数必须保持零。
	require.NotEqual(t, 200, request("").Code)
	wrong := hostingTestProof(t, identity, "delete", account.ID, "")
	require.NotEqual(t, 200, request(wrong).Code)
	require.Zero(t, calls) // 证明拒绝必须发生在不可逆调用之前。
	proof := hostingTestProof(t, identity, "reset", account.ID, operationID)
	response := request(proof)
	require.Equal(t, 502, response.Code, response.Body.String())
	require.Equal(t, 1, calls) // 读取或证明重放都不得产生第二次消费。
	// 一次性证明重放不可再执行；新 UUID 也不能绕开未知状态。
	require.NotEqual(t, 200, request(proof).Code)
	require.Equal(t, 1, calls) // 读取或证明重放都不得产生第二次消费。
	_, err := model.PrepareHostingReset(user.Id, account.ID, uuid.NewString())
	require.ErrorIs(t, err, model.ErrHostingConflict)
	response = hostingTestRequest(t, identity, "GET", "/api/hosting/reset-operations/:id", "/api/hosting/reset-operations/"+operationID, "", "", GetHostingResetOperation)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, 1, calls) // 读取或证明重放都不得产生第二次消费。
	// GET 仅查询未知结果；重载页面绝不成为再次消费的触发条件。
	var operation model.HostingResetOperation
	require.NoError(t, model.DB.First(&operation, "id = ?", operationID).Error)
	require.Equal(t, "outcome_unknown", operation.Status) // 超时不能伪装成明确失败或成功。
	foreign := account
	foreign.ID = 0
	foreign.UserID = user.Id + 100
	foreign.RemoteRef = uuid.NewString()
	require.NoError(t, model.DB.Create(&foreign).Error)
	response = hostingTestRequest(t, identity, "GET", "/api/hosting/accounts/:id", "/api/hosting/accounts/"+strconv.Itoa(foreign.ID), "", "", GetHostingAccount)
	require.Equal(t, 404, response.Code)
	// 审计仅含请求模板，禁止原始操作正文或证明落入日志。
	var logs []model.AuditLog
	require.NoError(t, model.LOG_DB.Find(&logs).Error)
	require.NotEmpty(t, logs) // 失败操作也需要可追溯证据。
	data, err := json.Marshal(logs)
	require.NoError(t, err)
	require.NotContains(t, string(data), proof) // 审计必须避免保存可重放的认证材料。
	for _, assertion := range assertions {
		require.NotContains(t, string(data), assertion) // 短期票据也属于有效认证材料，不因期限短而允许记日志。
	}
}

// TestHostingCredentialAndProbeBoundaries 验证加密归属和真实测试结果的绑定。
// 一次通过只证明指定模型与协议，远端返回其他模型不能激活账号。
// 先用真实 AES-GCM 保存受限 Key，再通过路由准备函数解密使用。
// 复制密文到另一账号引用应失败，验证账号 AAD 绑定实际生效。
// 删除显式加密配置后拒绝加密，防止使用进程随机默认值。
// 执行层返回 passed 但模型不一致时，账号必须仍未验证。
// 改为匹配模型和协议后才允许保存 active 与验证时间。
// 模型目录包含候选能力，不代表全部模型都继承这次验证结果。
// 用真实重置记录先写成功，再写晚到未知，终态必须保持成功。
// 控制面只有本地 httptest 服务，测试不消费任何真实原厂额度。
func TestHostingCredentialAndProbeBoundaries(t *testing.T) {
	user, identity := hostingTestDB(t)
	ref := uuid.NewString()
	encrypted, err := hosting.EncryptKey(ref, "private-test-key")
	require.NoError(t, err)
	account := model.HostingAccount{UserID: user.Id, Provider: "codex", Alias: "测试账号", RemoteRef: ref, Status: "connected", EncryptedKey: encrypted}
	require.NoError(t, model.DB.Create(&account).Error)
	wrongModel := true
	outcome := "passed"
	rejectTest := false
	pauseDuringTest := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, hosting.OwnerRef(user.Id), hostingTestControlAuthorization(t, r))
		var data any
		switch {
		case strings.HasSuffix(r.URL.Path, "/models"):
			data = gin.H{"models": []gin.H{{"id": "codex-test"}}}
		case strings.HasSuffix(r.URL.Path, "/observation"):
			data = gin.H{"state": "unavailable", "snapshot": nil}
		case strings.HasSuffix(r.URL.Path, "/test"):
			// 模拟模型返回前用户暂停，成功回包不能覆盖已确认的暂停操作。
			if pauseDuringTest {
				require.NoError(t, model.DB.Model(&account).Update("paused", true).Error)
			}
			if rejectTest {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			name := "codex-test"
			if wrongModel {
				name = "different-model"
			}
			// 执行层的专用文本字段必须保留，完整响应中的额外字段不能泄露。
			data = gin.H{"outcome": outcome, "model": name, "protocol": "openai-responses", "prompt": "Reply only OK.", "response_text": "OK from test model", "private_metadata": "must-not-leak"}
		default:
			t.Errorf("unexpected control path: %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(gin.H{"code": 0, "data": data}))
	}))
	defer server.Close()
	t.Setenv("OAK_GPT_LOAD_URL", server.URL)
	route, err := hosting.ResolveRoute(context.Background(), user.Id, account.ID)
	require.NoError(t, err)
	require.Equal(t, "private-test-key", route.AccessKey) // 真实解密必须得到内部受限 Key 才可准备调用。
	// 攻击者交换两行密文时，账号引用的附加认证数据会拒绝解密。
	swapped := account
	swapped.ID = 0
	swapped.RemoteRef = uuid.NewString() // 只替换归属引用，测试不能顺带重新加密掩盖问题。
	require.NoError(t, model.DB.Create(&swapped).Error)
	_, err = hosting.ResolveRoute(context.Background(), user.Id, swapped.ID)
	require.Error(t, err)
	originalSecret := strings.Repeat("test-secret-", 4)
	t.Setenv("CRYPTO_SECRET", "")
	_, err = hosting.EncryptKey(ref, "private-test-key")
	require.Error(t, err)
	t.Setenv("CRYPTO_SECRET", originalSecret)
	path := fmt.Sprintf("/api/hosting/accounts/%d/test", account.ID)
	request := func() *httptest.ResponseRecorder {
		return hostingTestRequest(t, identity, "POST", "/api/hosting/accounts/:id/test", path, `{"model":"codex-test"}`, "", TestHostingAccount)
	}
	require.Equal(t, 502, request().Code)
	stored, err := model.GetHostingAccount(user.Id, account.ID)
	require.NoError(t, err)
	require.Zero(t, stored.VerifiedAt)
	// 只有匹配真实请求的 passed 才能写入 active，其他目录模型仍未验证。
	wrongModel = false // 同一路径仅改变可信结果字段，隔离模型绑定条件。
	response := request()
	require.Equal(t, 200, response.Code, response.Body.String())
	// 浏览器只收到固定 DTO 的文本结果，不能收到未知控制面字段。
	// 使用生产 DTO 解码，遗漏字段或错误 JSON 名称都将导致明确失败。
	// 此处只验证投影，模型回复是否合法仍由执行层的协议测试负责。
	var transcript struct {
		Data hosting.ProbeResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &transcript))
	assert.Equal(t, "Reply only OK.", transcript.Data.Prompt)
	assert.Equal(t, "OK from test model", transcript.Data.ResponseText)
	assert.NotContains(t, response.Body.String(), "must-not-leak")
	stored, err = model.GetHostingAccount(user.Id, account.ID)
	require.NoError(t, err)
	require.Equal(t, "active", stored.Status)
	require.Equal(t, "codex-test", stored.VerifiedModel)
	require.Equal(t, "openai-responses", stored.VerifiedProtocol)
	require.Positive(t, stored.VerifiedAt) // 只有成功探测才能产生可展示的验证事实。
	// 历史通过不能掩盖最新测试失败；失败或结果未明都应立即退出调度。
	// 先恢复成功再逐个验证失败，防止测试因初始状态本来不可用而假通过。
	// 控制面错误与业务失败同样不能保留旧 active 和模型验证事实。
	// 不删除账号或凭据，用户仍能在详情重试并查看失败。
	// failed 是明确业务拒绝，inconclusive 是拿到结果但无法确认模型成功。
	// transport 模拟控制面没有可用响应；mismatch 模拟响应属于别的模型。
	// 四类结果走不同返回码，但准入状态必须保持同一个安全结论。
	// 循环不等待下一秒，顺带覆盖 MySQL 对同值 UPDATE 返回零修改行。
	// 每轮通过真实 HTTP 重新激活，不直接写 active 来绕过验证入口。
	for _, failure := range []string{"failed", "inconclusive", "transport", "mismatch"} {
		outcome, rejectTest, wrongModel = "passed", false, false
		require.Equal(t, http.StatusOK, request().Code)
		outcome = failure
		rejectTest = failure == "transport"
		wrongModel = failure == "mismatch"
		if wrongModel {
			outcome = "passed"
		}
		result := request()
		if rejectTest || wrongModel {
			require.Equal(t, http.StatusBadGateway, result.Code)
		} else {
			require.Equal(t, http.StatusOK, result.Code)
		}
		stored, err = model.GetHostingAccount(user.Id, account.ID)
		require.NoError(t, err)
		require.False(t, model.HostingAccountCanRelay(stored.Status, stored.Paused), failure)
		require.Zero(t, stored.VerifiedAt)
		require.Empty(t, stored.VerifiedModel)
	}
	// 条件更新未激活也必须返回冲突，不能仅凭原厂 passed 向界面报告可用。
	// 此处通过真实数据库写入暂停，再检查调度门槛仍保持关闭。
	outcome, rejectTest, wrongModel, pauseDuringTest = "passed", false, false, true
	require.Equal(t, http.StatusConflict, request().Code)
	stored, err = model.GetHostingAccount(user.Id, account.ID)
	require.NoError(t, err)
	require.True(t, stored.Paused)
	require.False(t, model.HostingAccountCanRelay(stored.Status, stored.Paused))
	// 一份晚到的 unknown 观察不能回退成功消费，防止用户再次扣原厂次数。
	operation := model.HostingResetOperation{ID: uuid.NewString(), UserID: user.Id, AccountID: account.ID, Status: "prepared"}
	require.NoError(t, model.DB.Create(&operation).Error)
	require.NoError(t, hosting.PersistResetResult(&operation, &hosting.ResetResult{Status: "succeeded"}))
	require.NoError(t, hosting.PersistResetResult(&operation, &hosting.ResetResult{Status: "outcome_unknown"}))
	require.Equal(t, "succeeded", operation.Status) // 晚到回包不能让已扣次数重新进入可消费状态。
}

// TestHostingKeyPolicyMigration 固化历史默认，不修改任何已经明确的 Key 选择。
// 原始 JSON 包括顺序、关闭项与空白格式都逐字复制，不重新解释旧账号状态。
// 禁用与过期 Key 仍属于用户现存资产，软删除 Key 则不会重新取得来源。
// 显式空策略与显式平台策略均优先保留，迁移不得添加钱包授权。
// 新库、升级后重复运行和升级后新增 Key 分别覆盖三个启动边界。
// 所有检查使用真实数据库；三种方言共用此用例而不跳过事务断言。
func TestHostingKeyPolicyMigration(t *testing.T) {
	user, _ := hostingTestDB(t)
	// 空库可以重复启动，不为用户凭空生成默认来源。
	for range 2 {
		copied, removed, err := model.MigrateResourcePoliciesToTokens()
		require.NoError(t, err)
		assert.Zero(t, copied)  // 空库或重复启动不能生成额外策略行。
		assert.Zero(t, removed) // 幂等运行不应再宣称删除旧默认。
	}
	tokens := []model.Token{
		{UserId: user.Id, Key: "migration-normal"},                                       // 正常 Key 是默认复制基线。
		{UserId: user.Id, Key: "migration-disabled", Status: common.TokenStatusDisabled}, // 停用只是暂停身份，不表示丢弃来源。
		{UserId: user.Id, Key: "migration-expired", ExpiredTime: 1},                      // 过期后续期仍应保留用户原有配置。
		{UserId: user.Id, Key: "migration-explicit"},                                     // 独立配置不能被旧全局顺序覆盖。
		{UserId: user.Id, Key: "migration-empty"},                                        // 空数组是明确授权状态，不是缺失数据。
		{UserId: user.Id, Key: "migration-deleted"},                                      // 软删除由 GORM 范围过滤。
		{UserId: user.Id + 1, Key: "migration-foreign"},                                  // 用户边界不能由连续 Key 编号推断。
	}
	require.NoError(t, model.DB.Create(&tokens).Error)
	require.NoError(t, model.DB.Delete(&tokens[5]).Error)
	// 旧来源可能引用已删除账号，迁移负责保留意图而不冒充一次用户编辑。
	legacy := model.ResourcePolicy{UserID: user.Id, TokenID: 0, Revision: 7, UpdatedAt: 1234567890, SourcesJSON: `[ {"kind":"hosting","account_id":999,"enabled":false}, {"kind":"hosting","account_id":998,"enabled":true} ]`}
	explicit := model.ResourcePolicy{UserID: user.Id, TokenID: tokens[3].Id, Revision: 3, UpdatedAt: 1234567891, SourcesJSON: `[{"kind":"subscription","enabled":true}]`}
	empty := model.ResourcePolicy{UserID: user.Id, TokenID: tokens[4].Id, Revision: 4, UpdatedAt: 1234567892, SourcesJSON: `[]`}
	for _, row := range []*model.ResourcePolicy{&legacy, &explicit, &empty} {
		require.NoError(t, model.DB.Create(row).Error)
	}
	copied, removed, err := model.MigrateResourcePoliciesToTokens()
	require.NoError(t, err)
	assert.EqualValues(t, 3, copied)  // 仅复制普通、禁用、过期三个无策略 Key。
	assert.EqualValues(t, 1, removed) // 每个用户只有一条历史默认待移除。
	// 复制前后的原始字段一致，证明没有加收费来源或重排用户偏好。
	for _, token := range tokens[:3] {
		var row model.ResourcePolicy
		require.NoError(t, model.DB.Where("user_id = ? AND token_id = ?", user.Id, token.Id).First(&row).Error)
		assert.Equal(t, legacy.SourcesJSON, row.SourcesJSON) // 包括空白与关闭项，防止迁移重新序列化。
		assert.Equal(t, legacy.Revision, row.Revision)       // 迁移不能伪装成用户的一次新编辑。
		assert.Equal(t, legacy.UpdatedAt, row.UpdatedAt)     // 保留历史编辑时间便于排障追溯。
	}
	for _, original := range []model.ResourcePolicy{explicit, empty} {
		var row model.ResourcePolicy
		require.NoError(t, model.DB.First(&row, original.ID).Error)
		assert.Equal(t, original, row) // 连内部主键也不变，证明显式策略未重建。
	}
	// 删除与他人 Key 没有新增记录；用户自己的全局行已经移除。
	for _, token := range tokens[5:] {
		snapshot, err := model.GetEffectiveResourcePolicy(token.UserId, token.Id)
		require.NoError(t, err)
		assert.Nil(t, snapshot) // 没有跨用户复制，也没有复活已删除 Key。
	}
	var defaults int64
	require.NoError(t, model.DB.Model(&model.ResourcePolicy{}).Where("token_id = ?", 0).Count(&defaults).Error)
	assert.Zero(t, defaults) // 新运行时不得保留仍可能被旧页面读取的全局行。
	copied, removed, err = model.MigrateResourcePoliciesToTokens()
	require.NoError(t, err)
	assert.Zero(t, copied)  // 空库或重复启动不能生成额外策略行。
	assert.Zero(t, removed) // 幂等运行不应再宣称删除旧默认。
	// 迁移仅处理当时已有的 Key，后来创建的 Key 不继承已移除的用户设置。
	newToken := model.Token{UserId: user.Id, Key: "migration-new"}
	require.NoError(t, model.DB.Create(&newToken).Error)
	snapshot, err := model.GetEffectiveResourcePolicy(user.Id, newToken.Id)
	require.NoError(t, err)
	assert.Nil(t, snapshot) // 后续 Key 独立开始，迁移不充当持久模板。
}

// TestHostingKeyPolicyMigrationRollback 在第二次复制时制造真实事务内写失败。
// 第一条已经写入的副本必须回滚，旧默认必须保留以便下次启动恢复。
// 返回计数也要归零，避免日志把未提交的工作宣称为迁移成功。
// 故障只注入 GORM 写入回调，真实事务、查询与回滚仍由数据库执行。
// 移除注入后立即重新迁移，验证失败不会留下无法恢复的中间状态。
func TestHostingKeyPolicyMigrationRollback(t *testing.T) {
	user, _ := hostingTestDB(t)
	tokens := []model.Token{{UserId: user.Id, Key: "rollback-a"}, {UserId: user.Id, Key: "rollback-b"}}
	require.NoError(t, model.DB.Create(&tokens).Error)
	legacy := model.ResourcePolicy{UserID: user.Id, Revision: 2, SourcesJSON: `[]`, UpdatedAt: 1234567890}
	require.NoError(t, model.DB.Create(&legacy).Error)
	// 在第二条副本写入前报错，确保第一条已经进入事务而非全部尚未执行。
	writes := 0
	injected := errors.New("injected policy copy failure")
	const callback = "test:hosting_policy_migration_failure"
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		// 只拦截策略表，避免把其他模型的初始化错误误当作迁移回滚。
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "resource_policies" {
			writes++
			if writes == 2 {
				tx.AddError(injected)
			}
		}
	}))
	// 清理在数据库句柄还有效时执行，即使中途断言失败也不污染后续测试。
	t.Cleanup(func() { require.NoError(t, model.DB.Callback().Create().Remove(callback)) })
	copied, removed, err := model.MigrateResourcePoliciesToTokens()
	require.ErrorIs(t, err, injected)
	assert.Equal(t, 2, writes) // 故障确实发生在部分写入之后。
	assert.Zero(t, copied)     // 未提交副本不计入成功日志。
	assert.Zero(t, removed)    // 失败不能对外报告默认已移除。
	var rows []model.ResourcePolicy
	require.NoError(t, model.DB.Find(&rows).Error)
	assert.Equal(t, []model.ResourcePolicy{legacy}, rows) // 默认完整保留，第一条副本已真实回滚。
	require.NoError(t, model.DB.Callback().Create().Remove(callback))
	copied, removed, err = model.MigrateResourcePoliciesToTokens()
	require.NoError(t, err)
	assert.EqualValues(t, 2, copied)  // 重试重新复制两条，证明没有半完成残留。
	assert.EqualValues(t, 1, removed) // 只有成功提交才允许移除历史默认。
}

// TestHostingConfigurationStatusAndProofPreservation 证明本地预检没有网络与授权副作用。
// 配置状态只向已认证用户开放，不返回管理地址或持久加密材料。
// 配置错误在消费单次证明之前失败，管理员补齐后同一证明仍可使用一次。
// 真正创建授权仍经过原有安全验证，不因预检成功而绕过重放检查。
// 执行层由本地 HTTP 服务替代，业务会话、证明与授权表均使用真实实现。
func TestHostingConfigurationStatusAndProofPreservation(t *testing.T) {
	_, identity := hostingTestDB(t)
	// 计数跨状态查询与授权创建共用，任何意外探测请求都会暴露。
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "/api/hosting/v1/authorizations", r.URL.Path) // 配置检查无权调用其他执行层端点。
		body, err := common.Marshal(gin.H{"code": 0, "data": gin.H{"stage_id": "configured-stage", "status": "ready", "authorization_url": "https://auth.example/authorize", "redirect_uri": "http://localhost:1455/auth/callback", "expires_at_ms": time.Now().Add(time.Minute).UnixMilli()}})
		require.NoError(t, err)
		_, err = w.Write(body)
		require.NoError(t, err)
	}))
	defer server.Close()
	// 特征字符串便于检查响应脱敏，与任何部署真实密钥无关。
	secret := strings.Repeat("private-config-secret", 2)
	// 每种失败只返回稳定错误代码，不能把原始配置值混入可展示信息。
	cases := []struct{ name, address, secret, code string }{
		{"invalid-url", "https://private-internal.example", secret, "hosting_invalid_control_url"},
		{"missing-secret", server.URL, "", "hosting_encryption_not_configured"},
		{"short-secret", server.URL, "short-private-secret", "hosting_encryption_not_configured"},
		{"configured", server.URL, secret, ""},
		{"offline", "http://127.0.0.1:1", secret, ""}, // 配置完整不能被等同于服务在线。
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 子用例退出自动恢复环境，缺失密钥不会污染后续正常配置断言。
			t.Setenv("OAK_GPT_LOAD_URL", tc.address)
			t.Setenv("CRYPTO_SECRET", tc.secret)
			response := hostingTestRequest(t, identity, "GET", "/api/hosting/status", "/api/hosting/status", "", "", GetHostingStatus)
			assert.Equal(t, 200, response.Code) // 未配置是可展示的状态，查询本身仍成功。
			var envelope struct {
				Success bool `json:"success"`
				Data    struct {
					Configured bool   `json:"configured"`
					Code       string `json:"code"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
			assert.True(t, envelope.Success)
			assert.Equal(t, tc.code == "", envelope.Data.Configured) // 只有全部本地条件满足才允许继续授权。
			assert.Equal(t, tc.code, envelope.Data.Code)
			assert.NotContains(t, response.Body.String(), tc.address) // 管理网络布局不能进入用户 DTO。
			assert.NotContains(t, response.Body.String(), "private-") // 所有测试密钥都有此特征，防止部分泄漏。
		})
	}
	assert.Zero(t, calls) // 预检与缺配置失败都不得访问执行层。
	// 相同处理器必须经过认证中间件，匿名用户不能读取部署配置状态。
	engine := gin.New()
	engine.GET("/api/hosting/status", middleware.UserAuth(), GetHostingStatus)
	anonymous := httptest.NewRecorder()
	engine.ServeHTTP(anonymous, httptest.NewRequest("GET", "/api/hosting/status", nil))
	assert.Equal(t, http.StatusUnauthorized, anonymous.Code)
	t.Setenv("OAK_GPT_LOAD_URL", server.URL)
	t.Setenv("OAK_GPT_LOAD_MANAGEMENT_KEY", "old-admin-secret-must-not-be-used")
	t.Setenv("CRYPTO_SECRET", "")
	proof := hostingTestProof(t, identity, "connect", 0, "")
	// 缺持久密钥的请求不能创建本地流程，更不能消费已经完成的用户验证。
	response := hostingTestRequest(t, identity, "POST", "/api/hosting/authorizations", "/api/hosting/authorizations", `{"provider":"codex"}`, proof, BeginHostingAuthorization)
	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.Contains(t, response.Body.String(), "hosting_encryption_not_configured")
	assert.Contains(t, response.Body.String(), "Account hosting encryption is not configured. Please contact an administrator.")
	var authorizations int64
	require.NoError(t, model.DB.Model(&model.HostingAuthorization{}).Count(&authorizations).Error)
	assert.Zero(t, authorizations) // 没有创建不可恢复的本地接入流程。
	assert.Zero(t, calls)          // 预检与缺配置失败都不得访问执行层。
	// 管理员补配置后重用原证明成功，随后重放必须被安全层拒绝。
	t.Setenv("CRYPTO_SECRET", secret)
	response = hostingTestRequest(t, identity, "POST", "/api/hosting/authorizations", "/api/hosting/authorizations", `{"provider":"codex"}`, proof, BeginHostingAuthorization)
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String()) // 原证明仍有效，修复配置无需用户重新验证。
	assert.Equal(t, 1, calls)
	response = hostingTestRequest(t, identity, "POST", "/api/hosting/authorizations", "/api/hosting/authorizations", `{"provider":"codex"}`, proof, BeginHostingAuthorization)
	assert.Equal(t, http.StatusForbidden, response.Code) // 成功创建后证明已经单次消费。
	assert.Equal(t, 1, calls)                            // 重放拒绝发生在远端创建之前。
}

// TestHostingControlRequestAuthorization 用真实 Client 签发并走真实验证处理器。
// 执行层只拿到单次请求的短期断言，不获得登录密钥或长期管理员凭据。
// 验证入口必须区分服务用途、受众、算法、有效期以及精确请求绑定。
// 同一有效票据可重复只读核验，但不能改变用户、动作、正文或幂等 UUID。
// 停用用户即时拒绝，内部数据库故障保持可观察的失败而非授予默认身份。
// 负面声明由测试专用签名材料构造，验证真实拒绝路径而不复制验证算法。
func TestHostingControlRequestAuthorization(t *testing.T) {
	user, identity := hostingTestDB(t)
	secret := strings.Repeat("request-authorization-test", 2) // 专用测试主密钥，不复用会话签名配置。
	t.Setenv("CRYPTO_SECRET", secret)
	var raw string
	var binding hosting.RequestBinding // 模拟执行层按真实收到的字节重建绑定。
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") // 只截取 Client 真正发送的票据。
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		digest := sha256.Sum256(body)
		binding = hosting.RequestBinding{Method: r.Method, RequestURI: r.URL.RequestURI(), BodySHA256: hex.EncodeToString(digest[:]), IdempotencyKey: r.Header.Get("Idempotency-Key")}
		_, err = w.Write([]byte(`{"code":0,"data":{}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	t.Setenv("OAK_GPT_LOAD_URL", server.URL)
	t.Setenv("OAK_GPT_LOAD_MANAGEMENT_KEY", "obsolete-admin-key")
	client, err := hosting.NewConfiguredClient()
	require.NoError(t, err)
	operationID := uuid.NewString()                                                       // 后续篡改此值应导致请求授权失败。
	payload := gin.H{"owner_ref": hosting.OwnerRef(user.Id), "operation_id": operationID} // 正文和查询归属同时由服务端生成。
	require.NoError(t, client.Call(context.Background(), "POST", "/authorizations", user.Id, payload, operationID, nil))
	assert.NotEqual(t, "obsolete-admin-key", raw) // 旧配置即使还在环境里，也不能被发送给执行层。
	assert.Equal(t, 1, calls)                     // 控制客户端不会为签名过程额外访问执行层。

	engine := gin.New()
	engine.POST("/api/internal/hosting/verify-request", VerifyHostingControlRequest)
	// 每次调用真实只读处理器，不用直接检查私有 claims 作为授权成功依据。
	verify := func(assertion string, actual hosting.RequestBinding) *httptest.ResponseRecorder {
		body, err := common.Marshal(actual)
		require.NoError(t, err)
		request := httptest.NewRequest("POST", "/api/internal/hosting/verify-request", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+assertion) // 核验使用独立票据，而非浏览器会话。
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response
	}
	// 核验可以重复执行；它不会创建授权业务行或消费用户安全证明。
	for range 2 {
		response := verify(raw, binding)
		assert.Equal(t, 200, response.Code) // 只读核验不会因之前核验过而误报票据已消费。
		assert.JSONEq(t, fmt.Sprintf(`{"success":true,"data":{"owner_ref":%q}}`, hosting.OwnerRef(user.Id)), response.Body.String())
	}
	var rows int64
	require.NoError(t, model.DB.Model(&model.HostingAuthorization{}).Count(&rows).Error)
	assert.Zero(t, rows) // 验证接口不能暗中变成一个新接入入口。

	// 精确请求绑定阻止改变动作、路径、归属、字节内容或原业务幂等键。
	for _, field := range []string{"method", "path", "owner", "body", "idempotency"} {
		t.Run(field, func(t *testing.T) {
			changed := binding
			switch field {
			case "method":
				changed.Method = "DELETE" // 既有读写权限不能被客户端转换为删除权限。
			case "path":
				changed.RequestURI = strings.Replace(changed.RequestURI, "/authorizations", "/accounts", 1) // 同一命名空间内也不能换动作。
			case "owner":
				changed.RequestURI = "/api/hosting/v1/authorizations?owner_ref=oak%3A999" // 查询串也是签名对象。
			case "body":
				changed.BodySHA256 = strings.Repeat("0", 64) // 合法格式的不同摘要仍必须拒绝。
			case "idempotency":
				changed.IdempotencyKey = uuid.NewString() // 不允许拿旧授权另开一次消费操作。
			}
			response := verify(raw, changed)
			assert.Equal(t, 401, response.Code) // 字段篡改与坏签名使用相同对外失败类别。
			assert.JSONEq(t, `{"success":false,"message":"Hosting request authorization failed"}`, response.Body.String())
		})
	}
	login, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	for _, rejected := range []string{login, "obsolete-admin-key", "", "not-a-jwt"} {
		assert.Equal(t, 401, verify(rejected, binding).Code) // 用户登录或管理员身份不能替代请求范围授权。
	}

	// 仅在测试内解析已捕获的自有票据，重新签名产生有合法 MAC 的错误声明。
	// 这样错误受众或超长期限不会被“签名本来就错”掩盖，仍走真实解析校验。
	claims := jwt.MapClaims{}
	_, _, err = jwt.NewParser().ParseUnverified(raw, claims)
	require.NoError(t, err)
	mac := hmac.New(sha256.New, []byte(secret))
	_, err = mac.Write([]byte("oak-hosting-request-authorization-v1"))
	require.NoError(t, err)
	key := mac.Sum(nil)
	for _, invalid := range []string{"algorithm", "signature", "issuer", "audience", "use", "type", "expired", "future", "long-lived", "missing-expiry", "owner"} {
		t.Run(invalid, func(t *testing.T) {
			changed := jwt.MapClaims{}
			for k, v := range claims {
				changed[k] = v
			}
			method := jwt.SigningMethodHS256
			signingKey := key
			tokenType := "oak-hosting-request+jwt"
			switch invalid {
			case "algorithm":
				method = jwt.SigningMethodHS512 // 即便是安全算法，也不是本协议允许的算法。
			case "signature":
				signingKey = []byte("different-test-key") // 形状相同不能替代真实性校验。
			case "issuer":
				changed["iss"] = "another-service" // 受信任发行者必须精确匹配。
			case "audience":
				changed["aud"] = "oak-dashboard" // 面向控制台的授权不能横向用于执行层。
			case "use":
				changed["use"] = "access" // 共同使用 JWT 格式不代表共同用途。
			case "type":
				tokenType = "JWT" // 普通令牌类型也不能被作为专用请求断言接收。
			case "expired":
				changed["iat"], changed["nbf"], changed["exp"] = time.Now().Add(-time.Minute).Unix(), time.Now().Add(-time.Minute).Unix(), time.Now().Add(-30*time.Second).Unix()
			case "future":
				changed["iat"], changed["nbf"], changed["exp"] = time.Now().Add(time.Minute).Unix(), time.Now().Add(time.Minute).Unix(), time.Now().Add(90*time.Second).Unix()
			case "long-lived":
				changed["exp"] = time.Now().Add(time.Hour).Unix() // 当前有效但超出短期授权窗口也要拒绝。
			case "missing-expiry":
				delete(changed, "exp") // 缺少期限不能按永不过期处理。
			case "owner":
				changed["owner_ref"] = "oak:999" // subject 与业务归属引用必须共同一致。
			}
			token := jwt.NewWithClaims(method, changed)
			token.Header["typ"] = tokenType
			assertion, err := token.SignedString(signingKey)
			require.NoError(t, err)
			assert.Equal(t, 401, verify(assertion, binding).Code)
		})
	}
	// 当前用户状态独立于短期签名，停用后无需等待票据自然过期。
	require.NoError(t, model.DB.Model(user).Update("status", common.UserStatusDisabled).Error)
	assert.Equal(t, 401, verify(raw, binding).Code) // 签名尚未过期仍不能访问已停用用户资源。
	require.NoError(t, model.DB.Model(user).Update("status", common.UserStatusEnabled).Error)
	assert.Equal(t, 200, verify(raw, binding).Code)
	// 数据库故障不能被当成不存在，也不能因为票据有效而直接放行。
	const callback = "test:hosting_authorization_query_failure"
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "users" {
			tx.AddError(errors.New("injected user query failure"))
		}
	}))
	response := verify(raw, binding)
	require.NoError(t, model.DB.Callback().Query().Remove(callback))
	assert.Equal(t, 503, response.Code) // 基础设施失败必须可定位，不能被伪装成普通认证失败。
	assert.NotContains(t, response.Body.String(), "injected")

	// 内部调用方也不能取得任意接口、点段逃逸或他人正文的签名能力。
	for _, target := range []string{"/../accounts", "/authorizations?owner_ref=oak:999", "/authorizations#fragment"} {
		require.Error(t, client.Call(context.Background(), "POST", target, user.Id, payload, operationID, nil))
	}
	require.Error(t, client.Call(context.Background(), "POST", "/authorizations", user.Id, gin.H{"owner_ref": "oak:999"}, operationID, nil))
	assert.Equal(t, 1, calls) // 所有错误都发生在签发或发包之前，不能先执行再拒绝。
}
