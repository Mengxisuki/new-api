package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// InsertWithResourcePolicy 将新 Key 与默认或显式来源放在同一个事务中。
// 缺省顺序只用于新建，不会借此修改已有 Key 的费用授权。
// 默认池代表用户明确新建的合同，不扫描或改写其历史 Key。
// 调用者已完成请求字段解析；缺省与显式空数组在此仍保持区别。
func (token *Token) InsertWithResourcePolicy(policy *ResourcePolicyInput) (*ResourcePolicyView, error) {
	if policy == nil {
		policy = &ResourcePolicyInput{Sources: []ResourceSource{{Kind: "wallet", Enabled: true}, {Kind: "subscription", Enabled: true}, {Kind: "hosting_pool", Enabled: true}}}
	}
	return token.saveWithResourcePolicy(policy, true)
}

// UpdateWithResourcePolicy 省略策略表示保留，不能把普通改名变成重新授权。
// 显式策略和 Token 字段一起提交，冲突或写失败都回滚整个编辑。
// 空来源与 nil 输入不同，前者保存拒绝全部，后者保留当前合同。
// 普通改名也返回当前策略，界面无需发第二次查询拼接保存结果。
func (token *Token) UpdateWithResourcePolicy(policy *ResourcePolicyInput) (*ResourcePolicyView, error) {
	return token.saveWithResourcePolicy(policy, false)
}

// saveWithResourcePolicy 共用用户行锁，锁顺序与独立策略编辑和启动迁移一致。
// 返回的编辑投影在同一事务读取，避免响应把另一版本策略拼到当前 Key 上。
// 未提交的失败不会改变已保存字段；缓存失效只会增加一次数据库读取。
// 不将钱包或订阅余额作为保存条件，真实可用性在每次请求的预扣阶段判断。
// 内部 Key 与来源账号都通过本人归属检查，前端字段不能扩大所有者范围。
// 不调用执行层网络，因此保存不受挂靠控制面可达性或原厂余额影响。
// 同一用户内串行保存与独立策略接口共享锁，避免并发首次创建重复版本。
// 不复用已展开的执行快照，池始终按用户提交的标识保存。
// 数据库错误原样返回上层分类，不降级成仅保存令牌的部分成功。
// 缓存 fence 复用既有令牌更新规则，不能另建资源策略的令牌副本。
// 事务不执行删除、迁移或额度调整，账户预算不属于编辑权限的一部分。
func (token *Token) saveWithResourcePolicy(policy *ResourcePolicyInput, creating bool) (*ResourcePolicyView, error) {
	var view *ResourcePolicyView
	if !creating {
		// 保留既有缓存 fence 行为，防止更新期间的旧快照重新写回缓存。
		if err := invalidateTokenCacheForMutation(token.Key); err != nil {
			common.SysLog(common.LogText("failed to invalidate token cache before update: %s", err.Error()))
		}
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var owner User
		// 所有者行也提供旧合同只读投影，避免事务中另走缓存读取旧偏好。
		if err := lockForUpdate(tx).First(&owner, token.UserId).Error; err != nil {
			return err
		}
		if creating {
			// 新 Key 不可能已有编辑版本，非零版本不能套用其他 Key 的授权。
			if policy.Revision != 0 {
				return ErrResourcePolicyConflict
			}
			if err := tx.Create(token).Error; err != nil {
				return err
			}
		} else {
			// 再次核对当前数据库归属，不能使用控制器读取后已经删除的 Key。
			var existing Token
			if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", token.Id, token.UserId).First(&existing).Error; err != nil {
				return err
			}
		}
		// 先通过共享版本和归属校验，再更新普通字段；两步仍受同一事务保护。
		if policy != nil {
			if _, err := saveResourcePolicy(tx, token.UserId, token.Id, policy.Revision, policy.Sources); err != nil {
				return err
			}
		}
		if !creating {
			// 沿用模型层唯一的更新白名单，来源保存不扩大可编辑 Token 字段。
			if err := token.updateFields(tx); err != nil {
				return err
			}
		}
		row, sources, err := readResourcePolicy(tx, token.UserId, token.Id)
		if err != nil {
			return err
		}
		if row == nil {
			// 返回投影不插入资源行，原订阅溢出规则仍由存量执行链处理。
			view = &ResourcePolicyView{Legacy: true, Sources: legacyResourceSources(owner.GetSetting().BillingPreference)}
		} else {
			// 保存后的版本和响应内容来自同一事务，不能误报告并发编辑结果。
			view = &ResourcePolicyView{Revision: row.Revision, Sources: sources}
		}
		return nil
	})
	if err != nil {
		// Create 可能已填入结构体 ID，但事务失败时调用方必须丢弃该结果。
		return nil, err
	}
	return view, nil
}
