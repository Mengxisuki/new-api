package model

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// HostingAccount 只保存 Oak 归属和执行层引用，不保存原厂 OAuth 凭据。
// 删除采用业务墓碑，历史策略和重置操作仍能定位原账号。
// 内部调用密钥仅以密文持久化，任何 JSON 响应都不能包含该字段。
type HostingAccount struct {
	ID                  int    `json:"id"`
	UserID              int    `json:"-" gorm:"index;not null"`                        // 所有读取和变更必须同时限定所有者。
	Provider            string `json:"provider" gorm:"type:varchar(32);not null"`      // 首期仅支持 codex，不能从请求猜测供应商。
	Alias               string `json:"alias" gorm:"type:varchar(80);not null"`         // 用户备注只用于显示，不承担身份判定。
	RemoteRef           string `json:"-" gorm:"type:varchar(64);uniqueIndex;not null"` // 重试与重授权复用此不可变引用。
	IdentityFingerprint string `json:"-" gorm:"type:varchar(128)"`
	EmailMask           string `json:"email_mask" gorm:"type:varchar(255)"`           // 仅保存执行层提供的掩码，不落原始邮箱。
	Status              string `json:"status" gorm:"type:varchar(32);index;not null"` // provisioning/connected/active/paused/deleting/deleted/error。
	Paused              bool   `json:"paused"`                                        // 独立拒绝标记，网络恢复不能擅自清除。
	GroupID             string `json:"-" gorm:"type:varchar(128)"`                    // 内部私有执行组引用，不复用公共渠道。
	CredentialID        string `json:"-" gorm:"type:varchar(128)"`                    // 原厂凭据由执行层保存，本地只有定位引用。
	AccessKeyID         string `json:"-" gorm:"type:varchar(128)"`                    // 用于受限执行权限追溯，不是用户 API Key。
	EncryptedKey        string `json:"-" gorm:"type:text"`                            // 密文绑定 RemoteRef，不能跨账号交换。
	ObservationJSON     string `json:"-" gorm:"type:text"`                            // 仅存 DTO 白名单后的观察结果。
	LastError           string `json:"last_error" gorm:"type:varchar(128)"`           // 仅保存可公开错误码，禁止原始上游正文。
	VerifiedAt          int64  `json:"verified_at"`                                   // 授权成功不能填写，只有模型实测可写。
	VerifiedModel       string `json:"verified_model" gorm:"type:varchar(255)"`       // 单次测试只证明这个模型，不授权整个目录。
	VerifiedProtocol    string `json:"verified_protocol" gorm:"type:varchar(32)"`     // 验证结论必须限定到实际测试协议。
	CreatedAt           int64  `json:"created_at"`
	UpdatedAt           int64  `json:"updated_at"`
}

// HostingAccountCanRelay 统一账号池、目录与执行前的生命周期资格判断。
// connected 已完成远端激活和内部 Key 持久化，可以承接用户主动发起的请求。
// active 额外表示通过诊断测试；未测试不是授权失败，也不能引发付费回退。
// 暂停和所有未完成/故障/删除状态一律拒绝，不从状态未知推断可调用。
// 此规则只判断本地状态，归属、模型、额度和执行层授权仍分别校验。
func HostingAccountCanRelay(status string, paused bool) bool {
	return !paused && (status == "connected" || status == "active")
}

// HostingAuthorization 将短期授权 Stage 绑定到本人及原账号。
// 授权网址留在执行层；这里只记录继续流程所需的非秘密引用。
type HostingAuthorization struct {
	SessionID       string `json:"-" gorm:"type:varchar(128)"` // 短期授权只能由发起会话继续。
	UserAuthVersion int64  `json:"-"`                          // 密码或认证版本变化使旧授权失效。
	SessionVersion  int64  `json:"-"`                          // 会话轮换后不复用旧敏感验证。
	ID              string `json:"id" gorm:"primaryKey;type:varchar(36)"`
	UserID          int    `json:"-" gorm:"index;not null"`
	AccountID       int    `json:"account_id" gorm:"index"`
	RemoteRef       string `json:"-" gorm:"type:varchar(64);not null"` // 新接入时预分配，重复创建不得换引用。
	StageID         string `json:"-" gorm:"type:varchar(128)"`
	Purpose         string `json:"-" gorm:"type:varchar(32)"` // 接入与重新授权不能互换同一 Stage。
	Status          string `json:"status" gorm:"type:varchar(32)"`
	ExpiresAt       int64  `json:"expires_at"`
	CreatedAt       int64  `json:"created_at"`
}

// HostingResetOperation 是原厂不可逆动作的追踪记录，不是支付订单。
// 未知结果保留原 UUID，任何新 UUID 都不能绕过同账号未决操作。
type HostingResetOperation struct {
	ID         string `json:"operation_id" gorm:"primaryKey;type:varchar(36)"`
	UserID     int    `json:"-" gorm:"index;not null"`
	AccountID  int    `json:"account_id" gorm:"index;not null"`
	Status     string `json:"status" gorm:"type:varchar(32);not null"`
	ResultJSON string `json:"-" gorm:"type:text"` // 仅存脱敏业务结果，不存凭据或完整上游响应。
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

var ErrHostingConflict = errors.New("hosting operation conflicts with current state")

// GetHostingAccount 的 owner 条件也是不存在/越权时的统一响应边界。
// 管理员身份不会让普通用户 API 跳过此约束。
// 用户编号是强制过滤条件，不因账号主键全局唯一而省略。
// 删除墓碑仍可读取，历史操作需要稳定的本地关联。
// 调用者负责根据操作类型拒绝不允许的生命周期状态。
// 数据层不接收远端 URL，也不会尝试刷新原厂凭据。
func GetHostingAccount(userID, accountID int) (*HostingAccount, error) {
	var account HostingAccount
	err := DB.Where("id = ? AND user_id = ?", accountID, userID).First(&account).Error
	return &account, err
}

// BeginHostingAuthorization 在外部请求之前落下稳定的操作身份。
// 重新授权只沿用本人尚未删除的账号，后续仍由执行层验证原厂身份。
// 事务先锁用户再验证会话，沿用安全模块既有锁顺序。
// 重新授权必须指向本人账号，且不能覆盖删除中的资源。
// 同账号未结束的重新授权会阻止第二项流程同时开始。
// 短期流程最多五项，限制未完成登录任务的资源占用。
// 过期但已进入 consuming 的流程仍视为未完成，不能重复授权绕过。
// 新流程的远端引用与 UUID 都由服务器生成，不接受客户端覆盖。
// 会话和用户认证版本一起持久化，之后每一步都验证同一身份。
// 此事务只建立本地意图，网络创建必须在提交后执行。
func BeginHostingAuthorization(identity AuthSessionIdentity, accountID int) (*HostingAuthorization, error) {
	userID := identity.UserID
	var authorization *HostingAuthorization
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id").First(&user, userID).Error; err != nil {
			return err
		}
		if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		remoteRef, purpose := uuid.NewString(), "connect"
		if accountID != 0 {
			var account HostingAccount
			if err := tx.Where("id = ? AND user_id = ?", accountID, userID).First(&account).Error; err != nil {
				return err
			}
			if account.Status == "deleted" || account.Status == "deleting" {
				return ErrHostingConflict
			}
			var unfinished int64
			if err := tx.Model(&HostingAuthorization{}).Where("account_id = ? AND status NOT IN ? AND (expires_at > ? OR status = ?)", accountID, []string{"consumed", "cancelled"}, time.Now().Unix(), "consuming").Count(&unfinished).Error; err != nil {
				return err
			}
			if unfinished > 0 {
				return ErrHostingConflict
			}
			remoteRef, purpose = account.RemoteRef, "reauthorize"
		}
		// 在用户行锁内约束短期 Stage 数量，避免无限创建远端登录任务。
		var pending int64
		if err := tx.Model(&HostingAuthorization{}).Where("user_id = ? AND expires_at > ? AND status NOT IN ?", userID, time.Now().Unix(), []string{"consumed", "cancelled"}).Count(&pending).Error; err != nil {
			return err
		}
		if pending >= 5 {
			return ErrHostingConflict
		}
		authorization = &HostingAuthorization{ID: uuid.NewString(), UserID: userID, SessionID: identity.SessionID, UserAuthVersion: identity.UserAuthVersion, SessionVersion: identity.SessionVersion, AccountID: accountID, RemoteRef: remoteRef, Purpose: purpose, Status: "starting", CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(10 * time.Minute).Unix()}
		return tx.Create(authorization).Error
	})
	return authorization, err
}

// PrepareHostingAccount 让重复消费同一授权返回同一账号。
// 用户行锁串行处理账号上限；跨组身份唯一性由 gpt-load 的可信身份事务保证。
// 锁顺序与授权创建一致，避免同一用户同时操作时互相等待成环。
// 短期授权还必须匹配发起会话，用户 ID 相同不足以继续敏感流程。
// 首次消费把授权推进 consuming，取消操作此后不能再取得资格。
// 重复消费读取原账号，不重复计算账号上限或创建本地资源。
// 新账号只有待执行引用，内部 Key 尚未保存时不能参与调用。
// 重新授权先清除已有验证事实，避免旧成功结果掩盖新凭据未知状态。
// 过期新授权拒绝使用，已经接受的创建则允许沿原操作恢复。
// 账号上限在用户锁内统计，删除墓碑不再占据名额。
// 远端可信身份是否已经绑定由执行层事务保证，本地不猜测邮箱唯一性。
// 只有本地准备成功后才能请求执行层创建，避免无法追踪的孤儿资源。
func PrepareHostingAccount(identity AuthSessionIdentity, authorizationID, alias string) (*HostingAccount, *HostingAuthorization, error) {
	userID := identity.UserID
	var account HostingAccount
	var authorization HostingAuthorization
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id").First(&user, userID).Error; err != nil {
			return err
		}
		if err := HostingAuthorizationQuery(lockForUpdate(tx), identity, authorizationID).First(&authorization).Error; err != nil {
			return err
		}
		// 只有已完成原厂授权的 Stage 才能首次消费，避免永久占用账号名额。
		if (authorization.Status != "ready" && authorization.Status != "consuming" && authorization.Status != "consumed") || authorization.StageID == "" {
			return ErrHostingConflict
		}
		// 重新授权也受短期有效期限制；已开始的创建可以由后台继续收敛。
		if authorization.Purpose == "reauthorize" && authorization.Status != "consumed" && authorization.Status != "consuming" && authorization.ExpiresAt <= time.Now().Unix() {
			return ErrHostingConflict
		}
		// 已建记录允许按原操作继续确认，不因回包丢失而创建另一个账号。
		if authorization.AccountID != 0 {
			if authorization.Status != "consumed" {
				if err := tx.Model(&authorization).Update("status", "consuming").Error; err != nil {
					return err
				}
				authorization.Status = "consuming"
			}
			if err := tx.Where("id = ? AND user_id = ?", authorization.AccountID, userID).First(&account).Error; err != nil {
				return err
			}
			if account.Status == "deleted" || account.Status == "deleting" {
				return ErrHostingConflict
			}
			if authorization.Purpose == "reauthorize" && authorization.Status != "consumed" {
				return tx.Model(&account).Updates(map[string]any{"status": "provisioning", "verified_at": 0, "verified_model": "", "verified_protocol": "", "updated_at": time.Now().Unix()}).Error
			}
			return nil
		}
		if authorization.ExpiresAt <= time.Now().Unix() {
			return ErrHostingConflict
		}
		var count int64
		if err := tx.Model(&HostingAccount{}).Where("user_id = ? AND status <> ?", userID, "deleted").Count(&count).Error; err != nil {
			return err
		}
		if count >= 5 {
			return ErrHostingConflict
		}
		account = HostingAccount{UserID: userID, Provider: "codex", Alias: alias, RemoteRef: authorization.RemoteRef, Status: "provisioning", CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
		if err := tx.Create(&account).Error; err != nil {
			return err
		}
		authorization.AccountID = account.ID
		authorization.Status = "consuming"
		return tx.Model(&authorization).Updates(map[string]any{"account_id": account.ID, "status": "consuming"}).Error
	})
	return &account, &authorization, err
}

// PrepareHostingReset 原子取得同账号的消费资格。
// 相同 UUID 可以恢复原结果，不同 UUID 在 prepared/unknown 状态时拒绝。
// 账号行锁保证同一账号同时最多只有一个未决操作身份。
// 操作 UUID 是全表唯一的，跨用户或跨账号重用返回不存在。
// 原 UUID 在终态时仍可读取，幂等重试不能重新扣原厂次数。
// 删除中的账号允许查询原操作，却禁止创建新的消费意图。
// prepared 与 outcome_unknown 都阻止新 UUID，不能只防网络超时。
// 先写 prepared 再请求执行层，进程崩溃后仍有可恢复的业务记录。
// 此函数不验证敏感证明，必须由已消费对应证明的业务入口调用。
// 额度或可用卡数量不能在这里猜测，最终接受与否由执行层决定。
func PrepareHostingReset(userID, accountID int, operationID string) (*HostingResetOperation, error) {
	var operation HostingResetOperation
	err := DB.Transaction(func(tx *gorm.DB) error {
		var account HostingAccount
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", accountID, userID).First(&account).Error; err != nil {
			return err
		}
		if account.Status == "deleted" {
			return ErrHostingConflict
		}
		err := tx.Where("id = ?", operationID).First(&operation).Error
		if err == nil {
			if operation.UserID != userID || operation.AccountID != accountID {
				return gorm.ErrRecordNotFound
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if account.Status == "deleting" {
			return ErrHostingConflict
		}
		var pending int64
		if err := tx.Model(&HostingResetOperation{}).Where("account_id = ? AND status IN ?", accountID, []string{"prepared", "outcome_unknown"}).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return ErrHostingConflict
		}
		operation = HostingResetOperation{ID: operationID, UserID: userID, AccountID: accountID, Status: "prepared", CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
		return tx.Create(&operation).Error
	})
	return &operation, err
}

// HostingAuthorizationQuery 统一绑定授权所属用户、会话及双方版本。
// PAT 使用自己的虚拟会话身份，不能接手浏览器已通过验证的短期流程。
// 所有短期授权入口复用这个条件，避免某个恢复端点只校验用户编号。
// 身份版本变更后，旧流程不再能由浏览器继续提交回调。
// 查询返回不存在，不泄露另一会话正在接入哪个原厂账号。
// 后台只使用已接受操作保存的身份继续幂等收敛，不产生新授权。
func HostingAuthorizationQuery(tx *gorm.DB, identity AuthSessionIdentity, id string) *gorm.DB {
	return tx.Where("id = ? AND user_id = ? AND session_id = ? AND user_auth_version = ? AND session_version = ?", id, identity.UserID, identity.SessionID, identity.UserAuthVersion, identity.SessionVersion)
}
