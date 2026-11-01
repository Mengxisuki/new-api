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

// ResourcePolicy 独立保存默认策略与 Key 完整覆盖，避免改写旧计费设置。
// TokenID=0 是用户默认；记录不存在才表示继承/旧合同，空数组表示不允许任何来源。
type ResourcePolicy struct {
	ID          int    `json:"-"`                                                   // 数据库内部主键，不作为策略版本使用。
	UserID      int    `json:"-" gorm:"uniqueIndex:resource_policy_owner;not null"` // 所有层级共享同一用户归属边界。
	TokenID     int    `json:"-" gorm:"uniqueIndex:resource_policy_owner;not null"` // 零表示默认，正数表示模型 API Key 完整覆盖。
	Revision    int64  `json:"revision"`                                            // 乐观版本防止旧页面覆盖新的付费授权。
	SourcesJSON string `json:"-" gorm:"type:text;not null"`                         // 空数组有效，缺行才表示未配置。
	UpdatedAt   int64  `json:"updated_at"`
}

// ResourcePolicySnapshot 是一次请求使用的不可变策略投影。
// Revision 包含归属层级，默认与 Key 覆盖的同一版本号不会混淆。
type ResourcePolicySnapshot struct {
	Revision string           `json:"revision"`
	Sources  []ResourceSource `json:"sources"`
}

var ErrResourcePolicyConflict = errors.New("resource policy revision conflict")

// ErrResourcePolicyInvalid 只描述客户端契约错误，与数据库故障分开。
var ErrResourcePolicyInvalid = errors.New("invalid resource policy")

// GetEffectiveResourcePolicy 只在两层均未配置时返回 nil。
// 表不存在、坏 JSON 和数据库错误都向上传播，绝不降级到可收费的旧链路。
// 只在该用户的两个候选层级中查询，不读取其他 Key 的覆盖。
// 正数 TokenID 优先于保留值零，完整覆盖不与默认策略做逐项合并。
// Revision 将用户、层级和版本编码在一起，供一次请求记录决策依据。
// 没有任何记录才表示旧合同；显式空数组绝不能被当作继承。
// 解析损坏的数据会中止调度，避免故障触发意外收费。
// 此读取不校验实时额度，额度和账号状态由执行时的候选检查决定。
// 模型不存在或来源不可用也不能修改持久策略去“修好”用户选择。
// 数据库缺表是部署错误，需要可见失败而不是默认走钱包。
// 返回的是新建快照，调用方不应修改数据库行来表示临时跳过。
func GetEffectiveResourcePolicy(userID, tokenID int) (*ResourcePolicySnapshot, error) {
	var row ResourcePolicy
	err := DB.Where("user_id = ? AND token_id IN ?", userID, []int{0, tokenID}).Order("token_id DESC").First(&row).Error
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
// 用户行锁把同一用户的默认与覆盖编辑串行化，版本号防止旧页面覆盖。
// TokenID 非零时先核对模型 API Key 归属，不接受 PAT 编号替代。
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
	if len(sources) > 7 {
		return nil, fmt.Errorf("%w: too many resource sources", ErrResourcePolicyInvalid)
	}
	var saved ResourcePolicy
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id").First(&user, userID).Error; err != nil {
			return err
		}
		if tokenID != 0 {
			var token Token
			if err := tx.Where("id = ? AND user_id = ?", tokenID, userID).First(&token).Error; err != nil {
				return err
			}
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
