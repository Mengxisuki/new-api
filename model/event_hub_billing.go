package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// 观察状态与资金事务一起保存，不依赖可关闭的消费日志。
// pending 只表示异步任务尚未达到可比较的最终费用。
const (
	HubChargePending  = "pending"
	HubChargeFinal    = "final"
	HubChargeRefunded = "refunded"
)

// HubChargeObservation 保存一笔真实扣费的最新结算事实。
// RequestKey 是请求或任务的稳定身份，不是原厂响应正文。
// 同一任务补扣、退差价及退款只修订一行，不能增加多份历史样本。
// 未知缓存确认不会被当成未扣费，也不会触发资金自动回滚。
// 事件生成与 EvaluatedRevision 同事务，重启不会重复产生同版告警。
// 只有 final 且费用为正的记录参与历史基线。
// 独立日志库失败或禁用不影响本表，主库迁移是启动前提。
// 费用快照只包含归属、身份和额度，不接收任意用户载荷。
// CreatedAt固定为首次结算时刻，修订只更新UpdatedAt。
// 基线筛选按实际业务身份分组，不以令牌备注区分价格。
// 单笔中位数样本与退款版本共享主键，避免冲销变成额外消费。
// 未检测记录不得因通知保留期到达而清理。
// 缓存未知结果单独标记，后台与人工都能定位到原计费身份。
type HubChargeObservation struct {
	ID                int    `json:"id"`
	RequestKey        string `json:"request_key" gorm:"size:191;uniqueIndex"` // 服务端请求或任务身份。
	UserID            int    `json:"user_id" gorm:"index:idx_hub_charge_baseline,priority:1"`
	ModelName         string `json:"model_name" gorm:"size:128"` // 使用平台实际计费模型，不用客户端别名。
	Funding           string `json:"funding" gorm:"size:24"`     // 钱包和订阅不混合比较。
	TaskID            string `json:"task_id" gorm:"size:191"`    // 仅存平台公开任务编号。
	ActualQuota       int    `json:"actual_quota" gorm:"type:bigint"`
	State             string `json:"state" gorm:"size:16;index"` // 异步预估与最终费用必须明确区分。
	CacheSyncPending  bool   `json:"cache_sync_pending"`         // 资金已提交但缓存未确认时必须显式对账。
	Revision          int    `json:"revision"`
	EvaluatedRevision int    `json:"evaluated_revision"` // 已完成检测的版本，落后则需要恢复。
	CreatedAt         int64  `json:"created_at" gorm:"index:idx_hub_charge_baseline,priority:2"`
	UpdatedAt         int64  `json:"updated_at"`
}

// ErrHubChargeConflict 表示同一计费身份的合同或预期费用已变化。
// 该错误不可当作额度不足回退，否则可能在另一来源再次扣费。
var ErrHubChargeConflict = errors.New("event charge settlement conflict")

// ApplyHubChargeSettlement 把本次资金差额和最终观察放进一个主库事务。
// quotaDelta 为正代表补扣；ActualQuota 始终是整笔总额。
// taskID 非零时还原子回写任务费用，消除先扣款后丢失任务回写的窗口。
// 返回 applied=false 表示相同结果已经提交，调用者不能再次调整 Key 预算。
// 旧批量预扣仍按原队列执行；本次差额同步提交且增量运算保持可交换。
// 缓存只在数据库提交后按差额更新，失败保留已提交事实而不重扣。
// 该函数不修改模型价格、预扣估算或资金来源选择。
// 原订阅上下限继续由共享差额规则验证，不能通过通知旁路。
// 钱包差额允许原有欠费，退款仍必须满足钱包容量上限。
// request_key的唯一约束配合同用户行锁，跨节点重复结算只认领一次。
// 同步最终费用不允许改价重投，异步任务修订必须有持久任务编号。
// 参数只来自已经完成用量计算的内部调用，不直接解析HTTP正文。
// 任一数据库写入失败都会回滚费用、任务总额与观察记录。
// 返回applied和error两个结果区分已提交但缓存未知与完全未执行。
func ApplyHubChargeSettlement(observation HubChargeObservation, quotaDelta int, subscriptionID int, taskID int64) (applied bool, err error) {
	if observation.RequestKey == "" || len(observation.RequestKey) > 191 || observation.UserID <= 0 || observation.ActualQuota < 0 || observation.ActualQuota > math.MaxInt32 || quotaDelta < -math.MaxInt32 || quotaDelta > math.MaxInt32 ||
		(observation.Funding != "wallet" && observation.Funding != "subscription") {
		return false, ErrHubChargeConflict
	}
	if observation.State != HubChargeFinal && observation.State != HubChargePending && observation.State != HubChargeRefunded {
		return false, ErrHubChargeConflict
	}
	// 用户行在观察之前持锁，让同一用户的幂等身份认领在多实例间一致。
	// 没有跨库事务，也不在本事务中调用 Redis 或上游接口。
	err = DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id", "quota").First(&user, observation.UserID).Error; err != nil {
			return err
		}
		var stored HubChargeObservation
		lookupErr := lockForUpdate(tx).Where("request_key = ?", observation.RequestKey).First(&stored).Error
		if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return lookupErr
		}
		if lookupErr == nil {
			// 相同请求不能借重投更换用户、模型或资金来源。
			// 已提交的同版结果是明确成功，不重复执行钱包加减。
			if stored.UserID != observation.UserID || stored.ModelName != observation.ModelName || stored.Funding != observation.Funding || stored.TaskID != observation.TaskID {
				return ErrHubChargeConflict
			}
			if stored.CacheSyncPending {
				return fmt.Errorf("event charge cache synchronization remains unconfirmed: %s", stored.RequestKey)
			}
			if stored.ActualQuota == observation.ActualQuota && stored.State == observation.State {
				return nil
			}
			// 同步请求只有一次最终结算；任务和流内累计观察允许修订。
			if stored.State == HubChargeFinal && observation.TaskID == "" {
				return ErrHubChargeConflict
			}
		}
		// 先读取任务现有费用，陈旧轮询结果不得覆盖较新的资金提交。
		// 任务行不锁上游网络，避免长事务阻塞其他消费者。
		if taskID > 0 {
			var task Task
			if err := lockForUpdate(tx).Select("id", "user_id", "quota").First(&task, taskID).Error; err != nil {
				return err
			}
			// 后台并发结算必须匹配真正的旧总额，不能盲用陈旧内存快照。
			if task.UserId != observation.UserID || task.Quota != observation.ActualQuota-quotaDelta {
				return ErrHubChargeConflict
			}
		}
		// 只有订阅需要订阅编号；钱包不能借编号变更资金归属。
		// 引用原订阅行也让额度不足以明确业务错误返回。
		if observation.Funding == "subscription" {
			if subscriptionID <= 0 {
				return ErrHubChargeConflict
			}
			if err := postConsumeUserSubscriptionDeltaTx(tx, subscriptionID, int64(quotaDelta)); err != nil {
				return err
			}
		} else if quotaDelta != 0 {
			// 退款仍受同一钱包容量上限保护；实际补扣保留原欠费合同。
			if quotaDelta < 0 && (common.ValidateWalletQuota(-quotaDelta) != nil || user.Quota > common.MaxWalletQuota+quotaDelta) {
				return ErrWalletQuotaLimitExceeded
			}
			if err := tx.Model(&User{}).Where("id = ?", observation.UserID).Update("quota", gorm.Expr("quota - ?", quotaDelta)).Error; err != nil {
				return err
			}
		}
		// 资金总额已经确定时同步保存任务费用，后续重投可校验旧值。
		// 零差额不执行无意义更新，兼容MySQL受影响行数语义。
		if taskID > 0 && quotaDelta != 0 {
			if err := tx.Model(&Task{}).Where("id = ?", taskID).Update("quota", observation.ActualQuota).Error; err != nil {
				return err
			}
		}
		// 保存版本与资金增量同事务；任何失败回滚整笔结算。
		now := common.GetTimestamp()
		observation.UpdatedAt = now
		// 先持久标记跨系统确认窗口，进程在Redis前退出也不会伪装完成。
		observation.CacheSyncPending = quotaDelta != 0 && observation.Funding == "wallet" && common.RedisEnabled
		observation.Revision = stored.Revision + 1
		if lookupErr == nil {
			observation.ID, observation.CreatedAt = stored.ID, stored.CreatedAt
			observation.EvaluatedRevision = stored.EvaluatedRevision
		} else {
			observation.CreatedAt = now
		}
		if err := tx.Save(&observation).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err == nil && applied && quotaDelta != 0 && observation.Funding == "wallet" && common.RedisEnabled {
		result, cacheErr := cacheApplyUserQuotaDelta(observation.UserID, -int64(quotaDelta))
		err = cacheErr
		// miss表示完整可用缓存不存在，沿用同步资金路径的冷缓存合同。
		// 不创建残缺哈希，也不全量回写覆盖其他请求的在途原子预留。
		// 本次增量只对存在且schema一致的缓存执行一次。
		if err == nil && result != cacheQuotaOK && result != cacheQuotaMiss {
			err = fmt.Errorf("unexpected event charge cache result: %d", result)
		}
		if err == nil {
			err = DB.Model(&HubChargeObservation{}).Where("id = ? AND revision = ?", observation.ID, observation.Revision).Update("cache_sync_pending", false).Error
		}
	}
	return applied, err
}

// ProcessHubChargeObservations 批量检测已提交的最终费用并清理过期样本。
// 每次工作有固定上限，后台循环负责继续，不能用无限事务拖住资金写入。
// 服务暂停只暂停投递，检测和业务事实仍持续保存。
// 两个节点可以竞争同一条记录，行锁与版本检查让告警至多产生一份。
// 未完成异步任务只占据pending记录，不从SUCCESS字段猜测最终费用。
// 检测失败直接向工作器返回，保留未完成版本供下一轮恢复。
// 已检测记录按保留窗口有界删除，不永久存储每次请求。
// 没有邮件或Webhook副作用，通知投递继续交给统一事件服务。
func ProcessHubChargeObservations(ctx context.Context) error {
	if !HubCaptureEnabled() {
		return nil
	}
	settings, err := GetHubSettings()
	if err != nil {
		return err
	}
	var pending []HubChargeObservation
	if err := DB.WithContext(ctx).Where("state <> ? AND evaluated_revision < revision", HubChargePending).Order("id ASC").Limit(100).Find(&pending).Error; err != nil {
		return err
	}
	for _, candidate := range pending {
		if err := evaluateHubCharge(ctx, candidate.ID, settings); err != nil {
			return err
		}
	}
	// 未决任务和缓存未知结果不能因保留期到达而静默消失。
	// 已处理样本至少保留七天，避免短通知保留期改变费用基线。
	var expired []int
	if err := DB.WithContext(ctx).Model(&HubChargeObservation{}).
		Where("state <> ? AND cache_sync_pending = ? AND evaluated_revision >= revision AND updated_at < ?", HubChargePending, false, common.GetTimestamp()-int64(max(7, settings.RetentionDays))*24*60*60).
		Order("id ASC").Limit(100).Pluck("id", &expired).Error; err != nil {
		return err
	}
	if len(expired) > 0 {
		return DB.WithContext(ctx).Where("id IN ?", expired).Delete(&HubChargeObservation{}).Error
	}
	return nil
}

// evaluateHubCharge 比较同用户、实际计费模型和资金来源的近期中位数。
// 当前记录不会参与自己的基线，退款、零费用和未完成任务均排除。
// 上限一百条、窗口七天，避免每次消费扫描全部历史。
// 金额使用内部额度；没有足够历史时只触发独立的高额规则。
// 当前样本只与先前记录比较，后发生的用量不能回写过去的基线。
// 偶数样本中位数使用浮点平均，避免两个整数相加溢出。
// 冷启动和历史倍率使用不同原因码，消费者可以明确解释触发依据。
// 已告警任务即便费用再修订也保留首次快照，不重复通知相同业务。
// 对账后的退款不撤回已经收到的通知，但不再参与后续费用基线。
func evaluateHubCharge(ctx context.Context, id int, settings HubSettings) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var charge HubChargeObservation
		if err := lockForUpdate(tx).First(&charge, id).Error; err != nil {
			return err
		}
		if charge.State == HubChargePending || charge.EvaluatedRevision >= charge.Revision {
			return nil
		}
		if charge.State == HubChargeFinal && charge.ActualQuota > 0 {
			var history []int
			if err := tx.Model(&HubChargeObservation{}).
				Where("user_id = ? AND model_name = ? AND funding = ? AND state = ? AND actual_quota > ? AND created_at >= ? AND id < ?", charge.UserID, charge.ModelName, charge.Funding, HubChargeFinal, 0, charge.CreatedAt-7*24*60*60, charge.ID).
				Order("id DESC").Limit(100).Pluck("actual_quota", &history).Error; err != nil {
				return err
			}
			// 冷启动规则不声称已有历史基线，载荷明确给出触发原因。
			threshold, baseline, reason := float64(settings.AnomalyColdQuota), float64(0), "absolute_limit"
			if len(history) >= settings.AnomalyMinSamples {
				slices.Sort(history)
				middle := len(history) / 2
				baseline = float64(history[middle])
				if len(history)%2 == 0 {
					baseline = (float64(history[middle-1]) + baseline) / 2
				}
				threshold = max(float64(settings.AnomalyMinQuota), baseline*settings.AnomalyMultiplier)
				reason = "historical_multiple"
			}
			// 仅比较已经冻结的费用，不在通知检测中重新计算价格。
			var previousAlerts int64
			if err := tx.Model(&HubEvent{}).Where("event_key = ?", fmt.Sprintf("charge:%d", charge.ID)).Count(&previousAlerts).Error; err != nil {
				return err
			}
			// 同笔任务后续修订不制造第二份异常通知，退款会从基线移除。
			// 首次告警载荷是触发时快照，不能改写已交付消费者的历史。
			if float64(charge.ActualQuota) >= threshold && previousAlerts == 0 {
				if err := AppendHubEventTx(tx, fmt.Sprintf("charge:%d", charge.ID), HubTopicCharge, charge.UserID, map[string]any{
					"user_id": charge.UserID, "request_key": charge.RequestKey, "model_name": charge.ModelName,
					"funding": charge.Funding, "actual_quota": charge.ActualQuota, "baseline_quota": baseline,
					"threshold_quota": threshold, "sample_count": len(history), "reason": reason,
				}); err != nil {
					return err
				}
			}
		}
		// 标记和事件一起提交，失败会留下待检测版本供下次恢复。
		return tx.Model(&charge).Update("evaluated_revision", charge.Revision).Error
	})
}
