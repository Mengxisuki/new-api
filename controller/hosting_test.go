package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/hosting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

// TestHostingPolicyContracts 验证两层策略及空覆盖的语义，防止意外恢复收费。
// 首先证明没有配置的旧 Key 仍返回 legacy 所需的 nil 快照。
// 随后保存用户默认并验证具体来源顺序，避免后台自动重排。
// Key 空覆盖必须优先于默认，即使默认仍允许钱包使用。
// 旧 revision 再次提交应冲突，不能覆盖新的付费授权。
// 重复钱包来源是输入错误，不能被静默去重后保存。
// 他人账号或不存在的用户不能借 account_id 建立策略。
// 删除覆盖后再破坏默认 JSON，验证损坏不能退回旧收费行为。
// 该用例只验证公共模型行为，不复制一份资源选择算法。
func TestHostingPolicyContracts(t *testing.T) {
	user, _ := hostingTestDB(t)
	token := model.Token{UserId: user.Id, Key: "hosting-policy-key", Name: "owned"}
	require.NoError(t, model.DB.Create(&token).Error)
	account := model.HostingAccount{UserID: user.Id, Provider: "codex", Alias: "本人", RemoteRef: uuid.NewString(), Status: "connected"}
	require.NoError(t, model.DB.Create(&account).Error)
	snapshot, err := model.GetEffectiveResourcePolicy(user.Id, token.Id)
	require.NoError(t, err)
	require.Nil(t, snapshot) // 旧用户不能因为迁移存在空表就改变原合同。
	// 未设置保持旧合同；保存空数组后则明确不允许使用任何来源。
	_, err = model.SaveResourcePolicy(user.Id, 0, 0, []model.ResourceSource{{Kind: "hosting", AccountID: account.ID, Enabled: true}, {Kind: "wallet", Enabled: true}})
	require.NoError(t, err)
	snapshot, err = model.GetEffectiveResourcePolicy(user.Id, token.Id)
	require.NoError(t, err)
	require.Equal(t, "hosting", snapshot.Sources[0].Kind)
	require.Equal(t, fmt.Sprintf("%d:0:1", user.Id), snapshot.Revision)
	_, err = model.SaveResourcePolicy(user.Id, token.Id, 0, nil)
	require.NoError(t, err)
	snapshot, err = model.GetEffectiveResourcePolicy(user.Id, token.Id)
	require.NoError(t, err)
	require.Empty(t, snapshot.Sources) // 显式关闭所有来源绝不能回退默认钱包。
	// 并发设置页用版本冲突显式拒绝，不能覆盖用户刚保存的付费授权。
	_, err = model.SaveResourcePolicy(user.Id, 0, 0, nil)
	require.ErrorIs(t, err, model.ErrResourcePolicyConflict) // 旧页面保存必须显式提醒刷新，不能静默覆盖。
	_, err = model.SaveResourcePolicy(user.Id, 0, 1, []model.ResourceSource{{Kind: "wallet"}, {Kind: "wallet"}})
	require.ErrorIs(t, err, model.ErrResourcePolicyInvalid) // 输入问题要与数据库损坏保持不同错误分类。
	_, err = model.SaveResourcePolicy(user.Id+1, 0, 0, []model.ResourceSource{{Kind: "hosting", AccountID: account.ID}})
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	// 删除覆盖立即继承最新默认，数据库故障绝不能返回 nil 伪装旧合同。
	require.NoError(t, model.DB.Where("user_id = ? AND token_id = ?", user.Id, token.Id).Delete(&model.ResourcePolicy{}).Error)
	require.NoError(t, model.DB.Model(&model.ResourcePolicy{}).Where("user_id = ?", user.Id).Update("sources_json", "broken").Error)
	_, err = model.GetEffectiveResourcePolicy(user.Id, token.Id)
	require.Error(t, err)
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
		// 控制面归属和管理凭据必须由 Oak 生成，不能来自浏览器正文。
		require.Equal(t, "Bearer test-control-key", r.Header.Get("Authorization"))
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
	t.Setenv("OAK_GPT_LOAD_MANAGEMENT_KEY", "test-control-key")
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
// 审计必须存在，但不包含证明或控制面管理密钥。
// 这些断言覆盖真实业务失败条件，不把 mock 的返回值当作最终结论。
func TestHostingProofOwnershipAndReset(t *testing.T) {
	user, identity := hostingTestDB(t)
	account := model.HostingAccount{UserID: user.Id, Provider: "codex", Alias: "本人", RemoteRef: uuid.NewString(), Status: "active"}
	require.NoError(t, model.DB.Create(&account).Error)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			calls++
			w.WriteHeader(502)
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(gin.H{"code": 0, "data": gin.H{"status": "outcome_unknown"}}))
	}))
	defer server.Close()
	t.Setenv("OAK_GPT_LOAD_URL", server.URL)
	t.Setenv("OAK_GPT_LOAD_MANAGEMENT_KEY", "test-control-key")
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
	require.NotContains(t, string(data), proof)              // 审计必须避免保存可重放的认证材料。
	require.NotContains(t, string(data), "test-control-key") // 部署级管理凭据不能进入用户日志。
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch {
		case strings.HasSuffix(r.URL.Path, "/models"):
			data = gin.H{"models": []gin.H{{"id": "codex-test"}}}
		case strings.HasSuffix(r.URL.Path, "/observation"):
			data = gin.H{"state": "unavailable", "snapshot": nil}
		case strings.HasSuffix(r.URL.Path, "/test"):
			name := "codex-test"
			if wrongModel {
				name = "different-model"
			}
			data = gin.H{"outcome": "passed", "model": name, "protocol": "openai-responses"}
		default:
			t.Errorf("unexpected control path: %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(gin.H{"code": 0, "data": data}))
	}))
	defer server.Close()
	t.Setenv("OAK_GPT_LOAD_URL", server.URL)
	t.Setenv("OAK_GPT_LOAD_MANAGEMENT_KEY", "test-control-key")
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
	stored, err = model.GetHostingAccount(user.Id, account.ID)
	require.NoError(t, err)
	require.Equal(t, "active", stored.Status)
	require.Equal(t, "codex-test", stored.VerifiedModel)
	require.Equal(t, "openai-responses", stored.VerifiedProtocol)
	require.Positive(t, stored.VerifiedAt) // 只有成功探测才能产生可展示的验证事实。
	// 一份晚到的 unknown 观察不能回退成功消费，防止用户再次扣原厂次数。
	operation := model.HostingResetOperation{ID: uuid.NewString(), UserID: user.Id, AccountID: account.ID, Status: "prepared"}
	require.NoError(t, model.DB.Create(&operation).Error)
	require.NoError(t, hosting.PersistResetResult(&operation, &hosting.ResetResult{Status: "succeeded"}))
	require.NoError(t, hosting.PersistResetResult(&operation, &hosting.ResetResult{Status: "outcome_unknown"}))
	require.Equal(t, "succeeded", operation.Status) // 晚到回包不能让已扣次数重新进入可消费状态。
}
