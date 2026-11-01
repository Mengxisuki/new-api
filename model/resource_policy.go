package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ResourceSource 表示一个候选资源，顺序就是明确的用户回退授权。
// AccountID 只对 hosting 有意义；平台来源不得夹带账号引用。
type ResourceSource struct {
	Kind      string `json:"kind"`                 // 类型决定计费与执行来源，不能接受未知枚举。
	AccountID int    `json:"account_id,omitempty"` // 只对本人挂靠账号有效，平台来源必须为零。
	Enabled   bool   `json:"enabled"`              // 禁用保留顺序，但执行时不得选择该项。
}

// ResourcePolicy 仅保存具体 API Key 的来源顺序，不提供用户全局优先级。
// TokenID=0 只用于启动迁移识别旧默认行；新写入必须绑定正数 Key。
type ResourcePolicy struct {
	ID          int    `json:"-"`                                                   // 数据库内部主键，不作为策略版本使用。
	UserID      int    `json:"-" gorm:"uniqueIndex:resource_policy_owner;not null"` // 读取和写入同时限定用户及具体 Key。
	TokenID     int    `json:"-" gorm:"uniqueIndex:resource_policy_owner;not null"` // 正数标识模型 API Key，零不能成为新策略。
	Revision    int64  `json:"revision"`                                            // 乐观版本防止旧页面覆盖新的付费授权。
	SourcesJSON string `json:"-" gorm:"type:text;not null"`                         // 空数组有效，缺行才表示未配置。
	UpdatedAt   int64  `json:"updated_at"`
}

// ResourcePolicySnapshot 是一次请求使用的不可变策略投影。
// Revision 同时绑定用户、Key 与版本，供请求日志追踪来源决策。
type ResourcePolicySnapshot struct {
	Revision string           `json:"revision"`
	Sources  []ResourceSource `json:"sources"`
}

var ErrResourcePolicyConflict = errors.New("resource policy revision conflict")

// ErrResourcePolicyInvalid 只描述客户端契约错误，与数据库故障分开。
var ErrResourcePolicyInvalid = errors.New("invalid resource policy")

// GetEffectiveResourcePolicy 仅查询该 Key 自己的策略，没有记录才返回旧平台合同。
// 旧全局行仅由启动迁移处理，运行时不会读取或继承它。
// 空 sources 是明确禁用全部，不能等价于没有配置。
// 数据库故障或损坏 JSON 直接返回错误，不能退回可能收费的路径。
// 来源可用性在执行时检查，此处不因额度变化改写用户选择。
// 返回快照与数据库行分离，临时跳过候选不会修改持久顺序。
// 保留历史版本格式，使现有请求日志仍能关联具体用户与 Key。
// Key 必须是正数，禁止内部调用意外重新启用全局策略语义。
func GetEffectiveResourcePolicy(userID, tokenID int) (*ResourcePolicySnapshot, error) {
	if tokenID <= 0 {
		return nil, ErrResourcePolicyInvalid
	}
	var row ResourcePolicy
	err := DB.Where("user_id = ? AND token_id = ?", userID, tokenID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sources []ResourceSource
	if err := common.UnmarshalJsonStr(row.SourcesJSON, &sources); err != nil {
		return nil, fmt.Errorf("invalid stored resource policy: %w", err)
	}
	if sources == nil {
		sources = []ResourceSource{}
	}
	return &ResourcePolicySnapshot{Revision: fmt.Sprintf("%d:%d:%d", userID, row.TokenID, row.Revision), Sources: sources}, nil
}

// SaveResourcePolicy 使用用户行锁与版本号保护同时打开的设置页。
// 校验发生在事务中，不能通过改 owner 或 token_id 更新他人策略。
// 上限七项对应五个挂靠账号和两种平台来源，不预留隐含兜底。
// 来源顺序本身就是用户授权，保存过程不能自动排序。
// 用户行锁与启动迁移保持相同顺序，版本号防止旧页面覆盖。
// TokenID 必须是本人模型 API Key，不接受零或 PAT 编号替代。
// 挂靠账号必须属于本人且未删除，暂停与待测试账号可以提前编排。
// 平台来源不允许带 account_id，防止混淆账号归属与收费来源。
// 重复项按来源身份拒绝，即使其中一项 enabled=false 也不能重复。
// 不支持的类型按客户端契约错误返回，不能自动忽略。
// 空切片序列化成数组，避免 JSON null 被误读成“尚未设置”。
// 首次创建只接受 revision=0；更新必须与当前记录版本完全一致。
// 所有校验和最终写入共享事务，失败不能保留半个策略。
// 数据库故障不包装成输入错误，便于调用者给出正确失败类别。
// 策略保存不会改变旧计费偏好，未配置策略的历史 Key 保持原行为。
// 调度只读取 enabled 项，禁用来源保留顺序便于用户再次开启。
// 账号额度、订阅资格和钱包余额不是保存条件，执行时再查真实状态。
// 该事务不会调用执行层网络，避免长时间持有用户锁。
func SaveResourcePolicy(userID, tokenID int, revision int64, sources []ResourceSource) (*ResourcePolicy, error) {
	if tokenID <= 0 || revision < 0 {
		return nil, ErrResourcePolicyInvalid
	}
	if len(sources) > 7 {
		return nil, fmt.Errorf("%w: too many resource sources", ErrResourcePolicyInvalid)
	}
	var saved ResourcePolicy
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id").First(&user, userID).Error; err != nil {
			return err
		}
		var token Token
		if err := tx.Where("id = ? AND user_id = ?", tokenID, userID).First(&token).Error; err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, source := range sources {
			key := source.Kind
			switch source.Kind {
			case "hosting":
				var account HostingAccount
				if source.AccountID <= 0 {
					return fmt.Errorf("%w: hosting account is required", ErrResourcePolicyInvalid)
				}
				if err := tx.Where("id = ? AND user_id = ? AND status <> ?", source.AccountID, userID, "deleted").First(&account).Error; err != nil {
					return err
				}
				key = fmt.Sprintf("hosting:%d", source.AccountID)
			case "subscription", "wallet":
				if source.AccountID != 0 {
					return fmt.Errorf("%w: platform source cannot bind an account", ErrResourcePolicyInvalid)
				}
			default:
				return fmt.Errorf("%w: unsupported resource source", ErrResourcePolicyInvalid)
			}
			if seen[key] {
				return fmt.Errorf("%w: duplicate resource source", ErrResourcePolicyInvalid)
			}
			seen[key] = true
		}
		// 空切片必须存成 []，保持“禁用全部”与“未设置”的区别。
		if sources == nil {
			sources = []ResourceSource{}
		}
		encoded, err := common.Marshal(sources)
		if err != nil {
			return err
		}
		err = tx.Where("user_id = ? AND token_id = ?", userID, tokenID).First(&saved).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if revision != 0 {
				return ErrResourcePolicyConflict
			}
			saved = ResourcePolicy{UserID: userID, TokenID: tokenID, Revision: 1, SourcesJSON: string(encoded), UpdatedAt: time.Now().Unix()}
			return tx.Create(&saved).Error
		}
		if err != nil {
			return err
		}
		if saved.Revision != revision {
			return ErrResourcePolicyConflict
		}
		saved.Revision++
		saved.SourcesJSON, saved.UpdatedAt = string(encoded), time.Now().Unix()
		return tx.Save(&saved).Error
	})
	return &saved, err
}

// MigrateResourcePoliciesToTokens 将旧默认顺序固化到升级前已存在的 Key。
// 整次迁移使用事务；任一复制失败都回滚，不能删除尚未复制的来源授权。
// 默认内容、版本和顺序原样保留，不重新校验额度、不添加钱包兜底。
// 包括失效账号引用也不清洗；运行时负责判断可用性，迁移不能替用户重新授权。
// 复制不调用 SaveResourcePolicy，避免新写入校验改变历史策略的有效内容。
// 显式 Key 策略优先保留，包括用户主动保存的空数组。
// 软删除 Key 不再迁移，禁用或过期但仍存在的 Key 则保留原顺序。
// 删除旧全局行后，新建 Key 不会再继承任何用户级来源。
// 与保存操作按用户行锁串行，多个启动实例会在锁后复查旧行。
// 重复执行没有旧默认行时直接无操作，不依赖额外迁移标记表。
// 返回值只含数量，日志不能输出策略内容、账号引用或 Key 凭据。
func MigrateResourcePoliciesToTokens() (int64, int64, error) {
	var copied, removed int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 按所有者顺序取锁，避免多个实例以相反顺序锁定不同用户。
		var defaults []ResourcePolicy
		if err := tx.Where("token_id = ?", 0).Order("user_id ASC").Find(&defaults).Error; err != nil {
			return err
		}
		for _, legacy := range defaults {
			// 包括已软删除的用户记录，避免历史归属仍存在却无法完成结构迁移。
			var user User
			if err := lockForUpdate(tx.Unscoped()).Select("id").First(&user, legacy.UserID).Error; err != nil {
				return err
			}
			// 另一个启动进程可能已迁移此用户；锁后不存在属于正常幂等结果。
			err := lockForUpdate(tx).Where("id = ? AND token_id = ?", legacy.ID, 0).First(&legacy).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			// GORM 自动排除软删除 Key；不按 status 或过期时间过滤现存资产。
			var tokens []Token
			if err := tx.Select("id").Where("user_id = ?", legacy.UserID).Find(&tokens).Error; err != nil {
				return err
			}
			for _, token := range tokens {
				// 当前读避免 MySQL 事务旧快照遗漏等待用户锁期间刚保存的显式策略。
				// 与 SaveResourcePolicy 同一锁顺序，不让迁移抢占用户最新选择。
				var existing ResourcePolicy
				err := lockForUpdate(tx).Select("id").Where("user_id = ? AND token_id = ?", legacy.UserID, token.Id).First(&existing).Error
				if err == nil {
					continue // 显式空数组也是完整策略，不能被默认行覆盖。
				}
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err // 查询故障不能被当成缺行，从而创建错误授权。
				}
				// 新副本由数据库分配独立主键，不复用默认行的内部身份。
				row := ResourcePolicy{UserID: legacy.UserID, TokenID: token.Id, Revision: legacy.Revision, SourcesJSON: legacy.SourcesJSON, UpdatedAt: legacy.UpdatedAt}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
				copied++ // 计数只有整笔事务提交后才对外报告。
			}
			// 只有该用户全部现存 Key 已保留各自策略后，才删除旧全局行。
			result := tx.Delete(&legacy)
			if result.Error != nil {
				return result.Error
			}
			removed += result.RowsAffected // 用数据库实际结果统计，不按候选行数猜测。
		}
		return nil
	})
	if err != nil {
		return 0, 0, err // 回滚后的计数不能误报已经迁移成功。
	}
	// 默认行已经删除，后续启动与新建 Key 均不会重复继承。
	return copied, removed, nil
}
