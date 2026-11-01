package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type Midjourney struct {
	Id          int    `json:"id"`
	Code        int    `json:"code"`
	UserId      int    `json:"user_id" gorm:"index"`
	Action      string `json:"action" gorm:"type:varchar(40);index"`
	MjId        string `json:"mj_id" gorm:"index"`
	Prompt      string `json:"prompt"`
	PromptEn    string `json:"prompt_en"`
	Description string `json:"description"`
	State       string `json:"state"`
	SubmitTime  int64  `json:"submit_time" gorm:"index"`
	StartTime   int64  `json:"start_time" gorm:"index"`
	FinishTime  int64  `json:"finish_time" gorm:"index"`
	ImageUrl    string `json:"image_url"`
	VideoUrl    string `json:"video_url"`
	VideoUrls   string `json:"video_urls"`
	Status      string `json:"status" gorm:"type:varchar(20);index"`
	Progress    string `json:"progress" gorm:"type:varchar(30);index"`
	FailReason  string `json:"fail_reason"`
	ChannelId   int    `json:"channel_id"`
	Quota       int    `json:"quota"`
	Buttons     string `json:"buttons"`
	Properties  string `json:"properties"`

	TokenId          int `json:"-" gorm:"default:0"`
	BillingChannelId int `json:"-" gorm:"default:0"`
	// 来源只由发送前的真实预扣填写，空值保留历史钱包合同。
	// 原订阅编号必须随任务持久化，异步失败不能退到当前套餐或钱包。
	BillingSource  string `json:"-" gorm:"type:varchar(32)"`
	SubscriptionId int    `json:"-"`
	// 周期已经推进时，不得拿旧任务退款抵扣新周期其他请求的消费。
	SubscriptionResetTime int64 `json:"-"`
}

// TaskQueryParams 用于包含所有搜索条件的结构体，可以根据需求添加更多字段
type TaskQueryParams struct {
	ChannelID      string
	MjID           string
	StartTimestamp string
	EndTimestamp   string
}

func GetAllUserTask(userId int, startIdx int, num int, queryParams TaskQueryParams) []*Midjourney {
	var tasks []*Midjourney
	var err error

	// 初始化查询构建器
	query := DB.Where("user_id = ?", userId)

	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetAllTasks(startIdx int, num int, queryParams TaskQueryParams) []*Midjourney {
	var tasks []*Midjourney
	var err error

	// 初始化查询构建器
	query := DB

	// 添加过滤条件
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetAllUnFinishTasks() []*Midjourney {
	var tasks []*Midjourney
	var err error
	// get all tasks progress is not 100%
	// 已失败但退款未完成的策略任务仍需轮询，不能因进度结束丢失补偿。
	err = DB.Where("progress != ? OR (status = ? AND quota > 0 AND billing_source <> ?)", "100%", "FAILURE", "").Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// HasUnfinishedMidjourneyTasks reports whether at least one Midjourney task is
// still in progress. It is a cheap existence check (LIMIT 1) used to decide
// whether the midjourney_poll system task needs to run; when no task is pending
// the scheduler skips creating a row entirely.
func HasUnfinishedMidjourneyTasks() bool {
	var id int
	err := DB.Model(&Midjourney{}).
		Where("progress != ? OR (status = ? AND quota > 0 AND billing_source <> ?)", "100%", "FAILURE", "").
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

func GetByOnlyMJId(mjId string) *Midjourney {
	var mj *Midjourney
	var err error
	err = DB.Where("mj_id = ?", mjId).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func GetByMJId(userId int, mjId string) *Midjourney {
	var mj *Midjourney
	var err error
	err = DB.Where("user_id = ? and mj_id = ?", userId, mjId).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func GetByMJIds(userId int, mjIds []string) []*Midjourney {
	var mj []*Midjourney
	var err error
	err = DB.Where("user_id = ? and mj_id in (?)", userId, mjIds).Find(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func GetMjByuId(id int) *Midjourney {
	var mj *Midjourney
	var err error
	err = DB.Where("id = ?", id).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func UpdateProgress(id int, progress string) error {
	return DB.Model(&Midjourney{}).Where("id = ?", id).Update("progress", progress).Error
}

func (midjourney *Midjourney) Insert() error {
	var err error
	err = DB.Create(midjourney).Error
	return err
}

func (midjourney *Midjourney) Update() error {
	// 轮询只更新任务进度，旧快照不得把已清除的退款标记写回来。
	// 资金字段仅通过准备、结算与原子退款入口维护。
	return DB.Model(midjourney).Select("*").Omit("quota", "token_id", "billing_channel_id", "billing_source", "subscription_id", "subscription_reset_time").Updates(midjourney).Error
}

func (midjourney *Midjourney) UpdateBillingState() error {
	return DB.Model(midjourney).
		Select("quota", "token_id", "billing_channel_id", "billing_source", "subscription_id", "subscription_reset_time").
		Updates(midjourney).Error
}

func (midjourney *Midjourney) GetBillingChannelId() int {
	if midjourney.BillingChannelId > 0 {
		return midjourney.BillingChannelId
	}
	return midjourney.ChannelId
}

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus.
// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Uses Model().Select("*").Updates() to avoid GORM Save()'s INSERT fallback.
func (midjourney *Midjourney) UpdateWithStatus(fromStatus string) (bool, error) {
	// 状态 CAS 不拥有资金字段，防止并发轮询重建已完成的退款。
	result := DB.Model(midjourney).Where("status = ?", fromStatus).Select("*").Omit("quota", "token_id", "billing_channel_id", "billing_source", "subscription_id", "subscription_reset_time").Updates(midjourney)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// RefundMidjourneyFunding 将原来源、Key 预算和退款标记放在同一事务。
// task 可能来自旧轮询快照，实际退款金额始终重新读取并锁定数据库行。
// 用户锁在任务与订阅锁之前，与订阅管理的锁顺序一致。
// 返回本次真正退还的额度；重复调用返回零，不能重复记退款统计。
// 任一数据库步骤失败整体回滚，下一次轮询可用同一任务安全重试。
// 缓存失败发生在提交之后，只报告错误，绝不重新执行资金加额。
func RefundMidjourneyFunding(task *Midjourney) (int, error) {
	if task == nil || task.Id <= 0 {
		return 0, errors.New("persisted Midjourney task required")
	}
	var current Midjourney // 持久行是唯一退款事实，不能信任调用者缓存。
	var token Token        // 事务后只使用其非公开 Key 同步预算缓存。
	refunded := 0          // 仅在事务成功后向调用层报告有效加额。
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 先锁所属用户，不能让传入对象伪造另一人的资金归属。
		var user User // 同用户退款与订阅管理先在归属锁上串行。
		if err := lockForUpdate(tx).First(&user, task.UserId).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", task.Id, user.Id).First(&current).Error; err != nil {
			return err
		}
		if current.Quota == 0 { // 持久幂等边界覆盖重启和并发轮询。
			return nil
		}
		if current.Quota < 0 { // 坏数据必须显式阻止反向扣款。
			return errors.New("invalid Midjourney refund quota")
		}
		// 只接收明确的新来源，旧任务由历史退款路径处理。
		switch current.BillingSource {
		case "wallet": // 钱包不经过可能延迟执行的批量队列。
			// 正向退款仍受钱包容量约束，不允许溢出成为负数。
			if err := common.ValidateWalletQuota(current.Quota); err != nil {
				return err
			}
			if user.Quota > common.MaxWalletQuota-current.Quota {
				return ErrWalletQuotaLimitExceeded
			}
			if err := tx.Model(&user).Update("quota", gorm.Expr("quota + ?", current.Quota)).Error; err != nil {
				return err
			}
		case "subscription": // 订阅退款永远不会增加钱包余额。
			// 即使套餐已经到期，也只释放原订阅，不查当前活跃套餐。
			var sub UserSubscription // 仅锁确切订阅，不重新选择活跃套餐。
			if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", current.SubscriptionId, user.Id).First(&sub).Error; err != nil {
				return err
			}
			// 原周期额度已被重置时无需再退订阅，但 Key 预算仍须恢复。
			if sub.LastResetTime == current.SubscriptionResetTime {
				if err := tx.Model(&sub).Update("amount_used", max(sub.AmountUsed-int64(current.Quota), 0)).Error; err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid Midjourney billing source")
		}
		if current.TokenId > 0 { // 只有实际被扣预算的令牌参与恢复。
			// 软删除 Key 仍保留账目；归属不一致或记录缺失必须回滚。
			if err := lockForUpdate(tx.Unscoped()).Where("id = ? AND user_id = ?", current.TokenId, user.Id).First(&token).Error; err != nil {
				return err
			}
			if err := tx.Unscoped().Model(&token).Updates(map[string]any{"remain_quota": gorm.Expr("remain_quota + ?", current.Quota), "used_quota": gorm.Expr("used_quota - ?", current.Quota)}).Error; err != nil {
				return err
			}
		}
		// 金额和标记同事务提交，进程重启后也不会再次增加余额。
		refunded = current.Quota // GORM 更新会修改对象，先保留待返回金额。
		if err := tx.Model(&current).Update("quota", 0).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil { // 未确认提交不能让调用方记作退款成功。
		return 0, err
	}
	task.Quota = 0
	if refunded == 0 || !common.RedisEnabled { // 无真实加额时缓存也不能再增量一次。
		return refunded, nil
	}
	// 增量缓存不会覆盖其他并发请求的预算，冷缓存由下次读取水合。
	// 两份缓存都尝试同步，任一失败可见且不反向重试数据库事务。
	var cacheErr error
	if current.BillingSource == "wallet" {
		_, cacheErr = cacheApplyUserQuotaDelta(current.UserId, int64(refunded))
	}
	if token.Id > 0 {
		_, tokenErr := cacheApplyTokenQuotaDelta(token.Id, token.Key, int64(refunded))
		cacheErr = errors.Join(cacheErr, tokenErr)
	}
	return refunded, cacheErr
}

func MjBulkUpdate(mjIds []string, params map[string]any) error {
	return DB.Model(&Midjourney{}).
		Where("mj_id in (?)", mjIds).
		Updates(params).Error
}

func MjBulkUpdateByTaskIds(taskIDs []int, params map[string]any) error {
	return DB.Model(&Midjourney{}).
		Where("id in (?)", taskIDs).
		Updates(params).Error
}

// CountAllTasks returns total midjourney tasks for admin query
func CountAllTasks(queryParams TaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Midjourney{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// CountAllUserTask returns total midjourney tasks for user
func CountAllUserTask(userId int, queryParams TaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Midjourney{}).Where("user_id = ?", userId)
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}
