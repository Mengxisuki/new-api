package hosting

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// AuthorizationView 将本地归属 ID 与远端安全摘要组合起来。
// 浏览器后续操作始终使用 ID，而非可篡改的 Stage 或账号引用。
type AuthorizationView struct {
	ID               string `json:"id"`                          // 前端只使用本地 UUID 继续流程，不暴露 Stage。
	AccountID        int    `json:"account_id"`                  // 新接入未建账号时为零，不能据此查询任意资源。
	Status           string `json:"status"`                      // 消费中状态不能重新开放授权 URL。
	ExpiresAt        int64  `json:"expires_at"`                  // 使用秒级时间，界面不要按毫秒解释。
	AuthorizationURL string `json:"authorization_url,omitempty"` // 短期敏感内容只返回发起会话，不持久化。
	RedirectURI      string `json:"redirect_uri,omitempty"`      // 仅是回调说明，服务器绝不访问浏览器提供的地址。
}

// BeginAuthorization 先保存本地操作，再发出可恢复的远端授权请求。
// 返回的本地 ID 即使控制面超时也可用于轮询，不换 UUID 隐藏未知结果。
// 执行层地址和持久加密密钥必须先可用，才创建本地短期流程。
// 原厂凭据不会从浏览器直接进入本服务，授权由专用 Stage 管理。
// 新接入分配远端账号引用；重新授权沿用既有账号引用。
// 操作记录先于网络请求提交，进程退出不会丢失恢复身份。
// 首次远端请求使用同一个操作 UUID，不自动重试或换新 UUID。
// 返回 view 与 error 可以同时存在，调用者必须把恢复 ID 交给前端。
// 用户证明已经在控制器消费，此处仍保留真实会话版本用于后续绑定。
// 账号数量与未完成授权上限在模型事务里统一执行。
func BeginAuthorization(ctx context.Context, identity model.AuthSessionIdentity, accountID int) (*AuthorizationView, error) {
	client, err := NewClient()
	if err != nil {
		return nil, err
	}
	if _, err := cryptKey(); err != nil {
		return nil, err
	}
	authorization, err := model.BeginHostingAuthorization(identity, accountID)
	if err != nil {
		return nil, err
	}
	return loadAuthorization(ctx, client, authorization, true)
}

// LoadAuthorization 只允许本人恢复或查询未取消的授权。
// 仅尚未取得 Stage 的 starting 状态需要按原 UUID 继续最初的幂等创建。
// 授权必须同时匹配用户、会话、会话版本和用户认证版本。
// 拥有本地 UUID 不是访问许可，其他登录会话也不能接管。
// resume 只决定能否恢复尚未创建成功的 Stage，不改变授权归属。
// 读取数据库失败不能降级为新授权，否则可能重复建立登录任务。
// 执行层不可用时不删除本地记录，后续仍可查询同一流程。
// 远端 Stage ID 留在服务端，不从客户端接受替换。
// 此函数不消费 code，也不生成任何可调用模型的凭据。
func LoadAuthorization(ctx context.Context, identity model.AuthSessionIdentity, id string, resume bool) (*AuthorizationView, error) {
	var authorization model.HostingAuthorization
	if err := model.HostingAuthorizationQuery(model.DB, identity, id).First(&authorization).Error; err != nil {
		return nil, err
	}
	client, err := NewClient()
	if err != nil {
		return nil, err
	}
	return loadAuthorization(ctx, client, &authorization, resume)
}

// loadAuthorization 统一生成投影，已取消或已消费结果不重新打开上游登录。
// GET 不产生外部创建副作用，恢复最初创建必须由显式 POST 发起。
// view 的本地 ID 是稳定恢复入口，远端字段只作为安全投影补充。
// 取消、消费中和已消费状态优先于远端轮询结果。
// 已过期流程只返回过期视图，不延长本地或原厂有效期。
// 尚无 Stage 的普通 GET 立即返回 starting，确保查询无创建副作用。
// 显式恢复沿用 purpose、account_ref 和 operation_id，不能改变授权目的。
// 取得 Stage 后只访问绑定资源，不允许浏览器指定控制路径。
// 缺少 Stage 身份或有效期的成功响应仍属于坏响应。
// 条件写入防止轮询把并发取消覆盖为 ready。
// 授权网址不保存到 Oak 表，因此不会通过数据库导出泄露。
// 网络结果未知保留 view，不能用 nil 抹去恢复线索。
func loadAuthorization(ctx context.Context, client *Client, authorization *model.HostingAuthorization, resume bool) (*AuthorizationView, error) {
	view := &AuthorizationView{ID: authorization.ID, AccountID: authorization.AccountID, Status: authorization.Status, ExpiresAt: authorization.ExpiresAt}
	if authorization.Status == "cancelled" || authorization.Status == "consumed" || authorization.Status == "consuming" {
		return view, nil
	}
	if authorization.ExpiresAt <= time.Now().Unix() {
		view.Status = "expired"
		return view, nil
	}
	var remote Authorization
	var err error
	if authorization.StageID == "" {
		if !resume {
			return view, nil
		}
		body := map[string]any{"owner_ref": OwnerRef(authorization.UserID), "account_ref": authorization.RemoteRef, "operation_id": authorization.ID, "purpose": authorization.Purpose}
		err = client.Call(ctx, http.MethodPost, "/authorizations", authorization.UserID, body, authorization.ID, &remote)
	} else {
		err = client.Call(ctx, http.MethodGet, "/authorizations/"+url.PathEscape(authorization.StageID), authorization.UserID, nil, "", &remote)
	}
	if err != nil {
		return view, err
	}
	if remote.StageID == "" || remote.ExpiresAtMS <= 0 {
		return view, &Error{502, "hosting_invalid_authorization"}
	}
	// 本地状态更新只接受尚未结束的记录，防止轮询覆盖并发取消结果。
	updates := map[string]any{"stage_id": remote.StageID, "status": remote.Status, "expires_at": remote.ExpiresAtMS / 1000}
	if err := model.DB.Model(authorization).Where("status NOT IN ?", []string{"consumed", "cancelled", "consuming"}).Updates(updates).Error; err != nil {
		return view, err
	}
	view.Status, view.ExpiresAt = remote.Status, remote.ExpiresAtMS/1000
	view.AuthorizationURL, view.RedirectURI = remote.AuthorizationURL, remote.RedirectURI
	return view, nil
}

// CompleteAuthorization 只转交已绑定到本人的 localhost 回调文本。
// 严格验证 URL 形状但绝不访问该 URL；state/PKCE 由 Stage 持有者验证。
// 解析回调仅检查语法，不执行任何用户提供的地址。
// 固定 HTTP 回环主机和端口对应原厂授权使用的本地回调。
// 禁止 userinfo、片段和重复 code/state，避免不同解析器解释不一致。
// 临时代码和 state 不进入 Oak 持久层、日志或成功响应。
// 真正的 state 和 PKCE 校验只能由原 Stage 持有者完成。
// 授权过期或已进入消费阶段时拒绝旧回调重放。
// 回调提交成功后只读取状态，不立即自动创建账号。
// 控制请求失败保留原 Stage，不能自动重新打开另一授权流程。
// 完成与取消竞争时，执行层仍负责 Stage 的终态互斥。
func CompleteAuthorization(ctx context.Context, identity model.AuthSessionIdentity, id, callback string) (*AuthorizationView, error) {
	userID := identity.UserID
	var authorization model.HostingAuthorization
	if err := model.HostingAuthorizationQuery(model.DB, identity, id).First(&authorization).Error; err != nil {
		return nil, err
	}
	if authorization.StageID == "" || authorization.ExpiresAt <= time.Now().Unix() || authorization.Status == "cancelled" || authorization.Status == "consumed" || authorization.Status == "consuming" {
		return nil, model.ErrHostingConflict
	}
	u, err := url.Parse(callback)
	if err != nil || len(callback) > 16384 || u.Scheme != "http" || u.User != nil || u.Fragment != "" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") || u.Port() != "1455" || u.Path != "/auth/callback" {
		return nil, &Error{400, "hosting_invalid_callback"}
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(values["state"]) != 1 || values.Get("state") == "" || len(values["code"]) != 1 || values.Get("code") == "" {
		return nil, &Error{400, "hosting_invalid_callback"}
	}
	client, err := NewClient()
	if err != nil {
		return nil, err
	}
	if err := client.Call(ctx, http.MethodPost, "/authorizations/"+url.PathEscape(authorization.StageID)+"/callback", userID, map[string]string{"callback_url": callback}, "", nil); err != nil {
		return nil, err
	}
	return loadAuthorization(ctx, client, &authorization, false)
}

// CancelAuthorization 先关闭本地消费入口，再尽力取消远端 Stage。
// 远端失败会返回错误，本地取消状态继续阻止授权被另一请求使用。
// 先原子取得取消资格，已经进入 consuming 的记录不能取消。
// 取消成功的本地记录继续阻止创建，即使远端清理暂时失败。
// 没有取得 Stage 的流程无需发送虚构的远端删除。
// 取消只接受发起会话的身份，不允许其他用户凭 UUID 操作。
// 对同一取消结果可以重复清理，不能反向改成待授权。
// 控制面失败必须返回给调用者，不能声称临时凭据已清除。
// 数据库更新错误会阻断外部请求，避免本地状态与远端操作失配。
// 取消不会删除已经持久化的账号或更改任何资源策略。
func CancelAuthorization(ctx context.Context, identity model.AuthSessionIdentity, id string) error {
	userID := identity.UserID
	var authorization model.HostingAuthorization
	if err := model.HostingAuthorizationQuery(model.DB, identity, id).First(&authorization).Error; err != nil {
		return err
	}
	if authorization.Status == "consumed" || authorization.Status == "consuming" {
		return model.ErrHostingConflict
	}
	query := model.DB.Model(&authorization).Where("status NOT IN ?", []string{"consumed", "consuming"}).Update("status", "cancelled")
	if query.Error != nil {
		return query.Error
	}
	if query.RowsAffected != 1 {
		return model.ErrHostingConflict
	}
	if authorization.StageID == "" {
		return nil
	}
	client, err := NewClient()
	if err != nil {
		return err
	}
	return client.Call(ctx, http.MethodDelete, "/authorizations/"+url.PathEscape(authorization.StageID), userID, nil, "", nil)
}

// CreateAccount 复用授权 UUID，执行 pending → 保存密钥 → activate 两阶段。
// 任何中断都保留同一个本地账号，重复请求不会新增另一组凭据。
// 模型事务先保留账号名额和授权消费身份，后续网络请求不持有行锁。
// 已消费授权直接返回原账号，重复按钮不会再次创建远端凭据。
// 远端响应必须匹配预分配的账号引用、用户归属和首期 provider。
// 执行组、凭据和访问 Key 引用均必须非零，不能接受不完整资源。
// 返回的受限 Key 在本地可靠加密保存之前，远端账号保持 pending。
// 激活失败保留 provisioning 及加密密钥，后台使用同一操作恢复。
// 重新授权清除旧验证事实，不能把旧模型测试结果套到新凭据。
// 并发删除通过状态条件阻止再次激活，删除墓碑不能被覆盖。
// 最终授权 consumed 与账号 connected 都可在重试中收敛。
// 这里不创建平台 Token，不向用户展示内部执行 Key。
// 此函数不会把账号自动加入用户默认策略，也不会触发钱包回退。
func CreateAccount(ctx context.Context, identity model.AuthSessionIdentity, authorizationID, alias string) (*model.HostingAccount, error) {
	userID := identity.UserID
	client, err := NewClient()
	if err != nil {
		return nil, err
	}
	if _, err := cryptKey(); err != nil {
		return nil, err
	}
	account, authorization, err := model.PrepareHostingAccount(identity, authorizationID, alias)
	if err != nil {
		return nil, err
	}
	if authorization.Status == "consumed" {
		return account, nil
	}
	if account.Status == "deleted" || account.Status == "deleting" {
		return nil, model.ErrHostingConflict
	}
	var remote RemoteAccount
	body := map[string]any{"owner_ref": OwnerRef(userID), "account_ref": account.RemoteRef, "stage_id": authorization.StageID, "operation_id": authorization.ID, "alias": alias}
	if err := client.Call(ctx, http.MethodPost, "/accounts", userID, body, authorization.ID, &remote); err != nil {
		return account, err
	}
	if remote.AccountRef != account.RemoteRef || remote.OwnerRef != OwnerRef(userID) || remote.Provider != "codex" || remote.GroupID == 0 || remote.CredentialID == 0 || remote.AccessKeyID == 0 || remote.IdentityFingerprint == "" {
		return account, &Error{502, "hosting_invalid_account"}
	}
	key, err := EncryptKey(account.RemoteRef, remote.InternalKey)
	if err != nil {
		return account, err
	}
	// 先可靠持久化受限 Key，才允许远端对外执行，避免启用不可管理的孤儿资源。
	updates := map[string]any{"alias": alias, "group_id": strconv.FormatUint(remote.GroupID, 10), "credential_id": strconv.FormatUint(remote.CredentialID, 10), "access_key_id": strconv.FormatUint(remote.AccessKeyID, 10), "identity_fingerprint": remote.IdentityFingerprint, "email_mask": remote.EmailMask, "encrypted_key": key, "status": "provisioning", "verified_at": 0, "verified_model": "", "verified_protocol": "", "updated_at": time.Now().Unix()}
	query := model.DB.Model(account).Where("status NOT IN ?", []string{"deleted", "deleting"}).Updates(updates)
	if query.Error != nil {
		return account, query.Error
	}
	if query.RowsAffected != 1 {
		return account, model.ErrHostingConflict
	}
	if err := client.Call(ctx, http.MethodPost, "/accounts/"+url.PathEscape(account.RemoteRef)+"/activate", userID, map[string]any{}, authorization.ID, &remote); err != nil {
		return account, err
	}
	// 最近验证状态不由授权成功推导，必须经过用户主动测试。
	query = model.DB.Model(account).Where("status = ?", "provisioning").Updates(map[string]any{"status": "connected", "paused": false, "last_error": "", "updated_at": time.Now().Unix()})
	if query.Error != nil {
		return account, query.Error
	}
	if query.RowsAffected != 1 {
		return account, model.ErrHostingConflict
	}
	if err := model.DB.Model(authorization).Update("status", "consumed").Error; err != nil {
		return account, err
	}
	return model.GetHostingAccount(userID, account.ID)
}

// ChangeAccount 暂停先本地拒绝，恢复先等待远端成功再放行。
// 网络失败不会因为静默回滚而重新开放已暂停的账号。
// 备注与暂停是独立字段，仅更新用户明确提供的部分。
// 暂停采用先拒绝本地调用再同步远端的顺序，失败时保持保守状态。
// 恢复采用相反顺序，只有远端确认后才允许本地重新选择。
// 旧验证时间保留用于解释历史，但从未验证的账号只能回到 connected。
// 远端暂时失败时不恢复之前的本地允许状态。
// 创建或删除尚未完成时，普通编辑不能跨越生命周期边界。
// 最终条件更新避免慢请求把并发删除覆盖成 active。
// 更新不改变远端身份指纹、归属或绑定的 Key 权限。
// 策略中的来源顺序不随暂停状态自动变化，由调度按已保存顺序处理。
func ChangeAccount(ctx context.Context, userID, accountID int, alias *string, paused *bool) (*model.HostingAccount, error) {
	account, err := model.GetHostingAccount(userID, accountID)
	if err != nil {
		return nil, err
	}
	if account.Status == "deleted" || account.Status == "deleting" || account.Status == "provisioning" {
		return nil, model.ErrHostingConflict
	}
	client, err := NewClient()
	if err != nil {
		return nil, err
	}
	updates := map[string]any{"updated_at": time.Now().Unix()}
	body := map[string]any{}
	if alias != nil {
		updates["alias"], body["alias"] = *alias, *alias
	}
	if paused != nil {
		body["paused"] = *paused
		if *paused {
			// 暂停请求不得把并发删除的墓碑覆盖回可编辑账号。
			if err := updateEditableAccount(account, map[string]any{"paused": true, "status": "paused", "updated_at": time.Now().Unix()}); err != nil {
				return nil, err
			}
		}
	}
	var remote RemoteAccount
	if err := client.Call(ctx, http.MethodPatch, "/accounts/"+url.PathEscape(account.RemoteRef), userID, body, "", &remote); err != nil {
		return nil, err
	}
	if paused != nil {
		updates["paused"] = *paused
		updates["status"] = "connected"
		if account.VerifiedAt > 0 {
			updates["status"] = "active"
		}
		if *paused {
			updates["status"] = "paused"
		}
	}
	if err := updateEditableAccount(account, updates); err != nil {
		return nil, err
	}
	return model.GetHostingAccount(userID, accountID)
}

// DeleteAccount 保留墓碑与未决重置记录，删除失败仍禁止新调用。
// 只有执行层确认删除才清除加密密钥，未知结果可继续原删除动作。
// 业务墓碑保留账号 ID，历史操作和策略仍能解释原来源。
// 第一次删除先本地停用，执行层不可用也不能继续选择该账号。
// 远端确认之前保留加密 Key，便于恢复清理并定位孤儿资源。
// 已经完成删除的重试无需再次访问执行层。
// 未决重置可能让远端拒绝清理，错误必须保留而不是忽略。
// 同一删除可由系统任务继续确认，不创建新的授权或凭据。
// 成功后清除密文和额度缓存，防止已解绑账号继续暴露旧状态。
// 删除挂靠不删除原厂账户，也不归还或扣减平台余额。
func DeleteAccount(ctx context.Context, userID, accountID int) error {
	account, err := model.GetHostingAccount(userID, accountID)
	if err != nil {
		return err
	}
	if account.Status == "deleted" {
		return nil
	}
	if err := model.DB.Model(account).Updates(map[string]any{"status": "deleting", "paused": true, "updated_at": time.Now().Unix()}).Error; err != nil {
		return err
	}
	client, err := NewClient()
	if err != nil {
		return err
	}
	if err := client.Call(ctx, http.MethodDelete, "/accounts/"+url.PathEscape(account.RemoteRef), userID, nil, "", nil); err != nil {
		return err
	}
	return model.DB.Model(account).Updates(map[string]any{"status": "deleted", "encrypted_key": "", "observation_json": "", "updated_at": time.Now().Unix()}).Error
}

// SaveObservation 只保存经过类型白名单过滤的额度视图。
// 删除中账号不通过后台观测覆盖停用状态。
// 固定 DTO 限制可持久化字段，不接受任意原厂响应直接存库。
// 没有观测时允许 null，保持未知与零额度的语义差异。
// 仅更新观测和更新时间，不把账号自动改成 active。
// 已经删除的记录不能因晚到刷新重新保留额度缓存。
// 错误向上传播，调用者不能用持久化失败的结果宣称刷新完成。
// 时间戳代表本地投影更新，不替代原厂观察时间。
// 主额度与重置卡独立的新鲜度由各自字段保留。
func SaveObservation(account *model.HostingAccount, observation *Observation) error {
	encoded, err := common.Marshal(observation)
	if err != nil {
		return err
	}
	return model.DB.Model(account).Where("status <> ?", "deleted").Updates(map[string]any{"observation_json": string(encoded), "updated_at": time.Now().Unix()}).Error
}

// ResetCredits 所有重试使用原 UUID；GET 查询不会调用此函数。
// 超时记为 outcome_unknown，新 UUID 被模型层的账号行锁阻止。
// 先持久化操作 UUID，再执行不可逆的原厂消费。
// 同一账号的其他未决 UUID 会在模型事务中被拒绝。
// 已经成功或明确拒绝的结果直接返回，不再次发送消费请求。
// 所有控制面写入沿用同一个 Idempotency-Key。
// 网络失败不能证明原厂未执行，因此状态必须保持 outcome_unknown。
// 未知状态持久化失败也会返回错误，不能隐瞒保护记录丢失。
// 最终消费结论先保存，随后观察失败不会把成功改回可重试。
// 此函数不尝试其他账号、包月或钱包，重置永远只作用于指定账号。
// 前台必须显式重新验证后调用恢复，后台任务禁止调用本函数。
func ResetCredits(ctx context.Context, userID, accountID int, operationID string) (*model.HostingResetOperation, error) {
	operation, err := model.PrepareHostingReset(userID, accountID, operationID)
	if err != nil {
		return nil, err
	}
	if operation.Status == "succeeded" || operation.Status == "rejected" {
		return operation, nil
	}
	account, err := model.GetHostingAccount(userID, accountID)
	if err != nil {
		return operation, err
	}
	client, err := NewClient()
	if err != nil {
		return operation, err
	}
	var result ResetResult
	err = client.Call(ctx, http.MethodPost, "/accounts/"+url.PathEscape(account.RemoteRef)+"/reset-credits/consume", userID, map[string]any{}, operation.ID, &result)
	if err != nil {
		// 未知是持久事实，不能把“客户端没收到结果”解释成未消费。
		persistErr := model.DB.Model(operation).Where("status IN ?", []string{"prepared", "outcome_unknown"}).Updates(map[string]any{"status": "outcome_unknown", "updated_at": time.Now().Unix()}).Error
		operation.Status = "outcome_unknown"
		return operation, errors.Join(err, persistErr)
	}
	if err := PersistResetResult(operation, &result); err != nil {
		return operation, err
	}
	if result.Observation != nil {
		if err := SaveObservation(account, result.Observation); err != nil {
			return operation, err
		}
	}
	return operation, nil
}

// PersistResetResult 仅接受执行层已定义状态，坏数据不能变成成功。
// 成功且 observation_pending 时保留成功，不再次调用消费动作。
// 只接受执行层协议定义的四种业务状态，未知枚举视为坏响应。
// 准备中和结果未知可以收敛到最终状态，终态不能被晚到响应回退。
// 状态与脱敏结果在同一数据库更新中落下，避免界面读取互相矛盾的值。
// 相同 UUID 的并发查询只能更新仍未决的记录。
// 返回前重新读取真实持久结果，调用者不能使用未提交的内存状态。
// 成功但观测待更新仍是成功，不能因观察未完成重新消费。
// 结果字段不包括凭据、授权地址或原厂生成文本。
// 条件更新而非“先读后无条件保存”是终态不可回退的关键。
// 查询与状态投影都必须使用数据库最终记录，内存中的旧状态不能决定重试。
// 这里没有自动刷新额度，消费结果与观察结果的更新可以独立收敛。
// 未知状态并不等价于原厂未执行，调用方不得因此生成新 UUID。
// 终态重复结果允许查询，不能因为 result_json 未变化而误报业务冲突。
func PersistResetResult(operation *model.HostingResetOperation, result *ResetResult) error {
	switch result.Status {
	case "prepared", "succeeded", "rejected", "outcome_unknown":
	default:
		return &Error{502, "hosting_invalid_reset_result"}
	}
	encoded, err := common.Marshal(result)
	if err != nil {
		return err
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var stored model.HostingResetOperation
		if err := tx.Where("id = ? AND user_id = ? AND account_id = ?", operation.ID, operation.UserID, operation.AccountID).First(&stored).Error; err != nil {
			return err
		}
		// 最终结果不可被晚到的 unknown 响应回退，避免界面再次提示消费。
		query := tx.Model(&stored).Where("status IN ?", []string{"prepared", "outcome_unknown"}).Updates(map[string]any{"status": result.Status, "result_json": string(encoded), "updated_at": time.Now().Unix()})
		if query.Error != nil {
			return query.Error
		}
		return tx.First(operation, "id = ?", operation.ID).Error
	})
}

// updateEditableAccount 统一保护普通编辑不能覆盖创建或删除状态。
// MySQL 默认返回实际变更行数，同秒重复暂停可能合法命中却返回零。
// 因此零行时检查条件是否仍匹配，不能把幂等无变化误判成状态冲突。
// 查询仍限定原用户和账号，不能通过额外确认扩大写入权限。
// 该函数只用于可逆备注/暂停操作，不替代授权与重置的状态事务。
func updateEditableAccount(account *model.HostingAccount, updates map[string]any) error {
	query := model.DB.Model(account).Where("status NOT IN ?", []string{"deleted", "deleting", "provisioning"}).Updates(updates)
	if query.Error != nil {
		return query.Error
	}
	if query.RowsAffected > 0 {
		return nil
	}
	// 使用新的查询，避免继承 GORM 更新语句状态后错误地统计整个表。
	var matched int64
	err := model.DB.Model(&model.HostingAccount{}).Where("id = ? AND user_id = ? AND status NOT IN ?", account.ID, account.UserID, []string{"deleted", "deleting", "provisioning"}).Count(&matched).Error
	if err != nil {
		return err
	}
	if matched != 1 {
		return model.ErrHostingConflict
	}
	return nil
}
