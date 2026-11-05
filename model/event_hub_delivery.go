package model

import (
	"fmt"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HubDelivery 是每个消费者独立的确认账本；同一事件可投递给多个消费者。
// 不使用全局最大事件编号作为游标，避免并发事务晚提交被永久遗漏。
// 未确认记录在断线后重发，消费者需用事件编号幂等处理。
type HubDelivery struct {
	ID         int64 `json:"id" gorm:"primaryKey;autoIncrement"`                               // ACK使用投递编号而非事件编号。
	ConsumerID int   `json:"consumer_id" gorm:"uniqueIndex:idx_hub_delivery,priority:1;index"` // 与事件构成唯一对。
	EventID    int64 `json:"event_id" gorm:"uniqueIndex:idx_hub_delivery,priority:2;index"`    // 引用白名单摘要。
	CreatedAt  int64 `json:"created_at"`                                                       // 默认保留期跟随原事件。
	LastSentAt int64 `json:"last_sent_at"`                                                     // 不等于客户端已处理。
	Attempts   int   `json:"attempts"`                                                         // 可观测网络重投次数。
	AckedAt    int64 `json:"acked_at" gorm:"index"`                                            // 仅客户端显式确认后写入。
}

// HubConnection 用短租约聚合多节点在线状态，进程退出后自动显示离线。
// 这里不保存客户端认证头、IP或原始令牌。
// 在线不代表已消费；业务进度始终由HubDelivery确认记录决定。
type HubConnection struct {
	ID         string `json:"-" gorm:"size:64;primaryKey"` // 每次连接使用随机编号。
	ConsumerID int    `json:"consumer_id" gorm:"index"`    // 用于在线数与连接限额。
	TokenID    int    `json:"-"`                           // 浏览器登录会话使用零。
	ExpiresAt  int64  `json:"-" gorm:"index"`              // 每次重鉴权后续租十秒。
}

// HubDeliveryView 将元数据与安全事件摘要组合返回。
// 原始数据库Payload不直接序列化，避免前端把JSON字符串误当对象。
// 对外载荷与生产者事件使用同一白名单契约。
// 此视图不暴露内部幂等键，客户端只需event_id去重。
// 投递编号与事件编号分开，以免一个消费者的确认影响其他消费者。
// 管理展示和SSE发送共用形状，避免同一字段出现不同含义。
type HubDeliveryView struct {
	HubDelivery
	Topic   string         `json:"topic"`   // 保持原事件类型。
	Payload map[string]any `json:"payload"` // 仅允许对象型白名单载荷。
}

// HubConsumerUsers 是关注范围的唯一解码入口；坏过滤器不能变成全量订阅。
// 数据库中的坏JSON同样属于不可信配置。
// 空数组表示不限用户，解析失败与空数组必须明确区分。
// 该函数只处理关注过滤，不决定消费者是否有主题权限。
// 已删除用户的历史事件仍按保存的用户编号过滤。
// 无效编号会返回错误，不自动从过滤器删除。
func HubConsumerUsers(c *HubConsumer) ([]int, error) {
	var ids []int
	if err := common.UnmarshalJsonStr(c.UserIDs, &ids); err != nil {
		return nil, err
	}
	return HubUserIDs(ids)
}

// MaterializeHubDeliveries 扫描尚未分配给本消费者的已提交事件。
// NOT EXISTS取代递增事件游标，事务编号先分配后提交也不会漏通知。
// 消费者行锁序列化同一消费者的分配，多实例仍只建一份投递记录。
// 单批最多一百条，限制长事务和积压恢复对主库的影响。
// 只读取本事务能够看见的已提交事件。
// 没有“分配到最大编号就永不回头”的隐含假设。
// 先前批次的投递唯一记录使重复扫描不会生成副本。
// 同消费者多个连接可能同时请求分配，数据库约束仍是最终防线。
// 当前令牌较窄时只分配它能消费的主题，后台可分配完整订阅。
// 不存在的事件不能通过客户端自报编号生成投递。
// 事件创建时间保留原值，不因客户端离线重连而刷新保留期。
// 返回的是本轮分配结果，实际消费需要另查未确认集合。
// 本函数不推进确认状态，也不会把未连接消费者伪装成在线。
// 用户过滤通过参数绑定进入查询，禁止拼接原始输入。
func MaterializeHubDeliveries(consumerID int, topics []string) ([]HubDelivery, error) {
	var deliveries []HubDelivery
	err := DB.Transaction(func(tx *gorm.DB) error {
		var c HubConsumer
		if err := lockForUpdate(tx).First(&c, consumerID).Error; err != nil {
			return err
		}
		if !c.Enabled {
			return ErrHubForbidden
		}
		subscribed, err := HubTopicList(c.Topics)
		if err != nil {
			return err
		}
		allowed := []string{}
		for _, topic := range topics {
			if slices.Contains(subscribed, topic) {
				allowed = append(allowed, topic)
			}
		}
		if len(allowed) == 0 {
			return ErrHubForbidden
		}
		ids, err := HubConsumerUsers(&c)
		if err != nil {
			return err
		}
		var settings HubSettings
		if err := tx.First(&settings, 1).Error; err != nil {
			return err
		}
		cutoff := common.GetTimestamp() - int64(settings.RetentionDays)*86400
		unseen := tx.Model(&HubDelivery{}).Select("1").Where("hub_deliveries.consumer_id = ? AND hub_deliveries.event_id = hub_events.id", consumerID)
		query := tx.Where("topic IN ? AND created_at >= ? AND created_at >= ?", allowed, c.CreatedAt, cutoff).Where("NOT EXISTS (?)", unseen)
		if len(ids) > 0 {
			query = query.Where("user_id IN ?", ids)
		}
		var events []HubEvent
		if err := query.Order("id ASC").Limit(100).Find(&events).Error; err != nil {
			return err
		}
		deliveries = make([]HubDelivery, 0, len(events))
		for _, event := range events {
			delivery := HubDelivery{ConsumerID: consumerID, EventID: event.ID, CreatedAt: event.CreatedAt}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&delivery).Error; err != nil {
				return err
			}
			deliveries = append(deliveries, delivery)
		}
		return nil
	})
	return deliveries, err
}

// HubEventAllowed 复用当前订阅与关注用户过滤，ACK和补发均不能绕过。
// 修改订阅后，历史投递记录仍不赋予已撤销的读取权。
// 先检查令牌主题，再检查消费者当前主题。
// 两个集合都允许才能进入用户范围判断。
// 旧投递编号不是持久授权票据，修改订阅立即收缩其可操作范围。
// 管理员重放仍必须满足同样的主题与用户约束。
func HubEventAllowed(c *HubConsumer, event *HubEvent, topics []string) (bool, error) {
	if !slices.Contains(topics, event.Topic) {
		return false, nil
	}
	own, err := HubTopicList(c.Topics)
	if err != nil {
		return false, err
	}
	if !slices.Contains(own, event.Topic) {
		return false, nil
	}
	ids, err := HubConsumerUsers(c)
	if err != nil {
		return false, err
	}
	return len(ids) == 0 || slices.Contains(ids, event.UserID), nil
}

// ListHubDeliveries 关联事件以应用保留期；删除的事件不会伪装成待消费。
// 管理视图包含已确认记录，消费视图仅取当前允许的未确认消息。
// 消费查询先用数据库筛选主题、用户与保留期。
// 较窄Token不会在响应中看到其他主题的载荷。
// 重发间隔用于避免在线未确认消息持续占满连接。
// 客户端处理超过间隔可能收到重复消息，必须使用事件编号去重。
// 管理页最多展示五十条，消费连接每轮最多二十条。
// 排序只控制本轮发送顺序，不意味着较小编号已经全部确认。
// 载荷解析失败会终止本轮并报告存储错误，不能返回空对象。
// 查询期间事件可能到期，找不到关联记录需要重试而非伪造消息。
// 传输层把数据库错误转换为明确流错误后关闭连接。
func ListHubDeliveries(c *HubConsumer, topics []string, pending bool) ([]HubDeliveryView, error) {
	s, err := GetHubSettings()
	if err != nil {
		return nil, err
	}
	ids, err := HubConsumerUsers(c)
	if err != nil {
		return nil, err
	}
	query := DB.Table("hub_deliveries").Select("hub_deliveries.*").Joins("JOIN hub_events ON hub_events.id = hub_deliveries.event_id").Where("hub_deliveries.consumer_id = ? AND hub_events.created_at >= ? AND hub_events.topic IN ?", c.ID, common.GetTimestamp()-int64(s.RetentionDays)*86400, topics)
	if len(ids) > 0 {
		query = query.Where("hub_events.user_id IN ?", ids)
	}
	if pending {
		query = query.Where("acked_at = 0 AND (last_sent_at = 0 OR last_sent_at <= ?)", common.GetTimestamp()-30)
	}
	limit := 50
	order := "hub_deliveries.id DESC"
	if pending {
		limit = 20
		order = "hub_deliveries.id ASC"
	}
	var records []HubDelivery
	if err := query.Order(order).Limit(limit).Scan(&records).Error; err != nil {
		return nil, err
	}
	views := make([]HubDeliveryView, 0, len(records))
	for _, record := range records {
		var event HubEvent
		if err := DB.First(&event, record.EventID).Error; err != nil {
			return nil, err
		}
		var payload map[string]any
		if err := common.UnmarshalJsonStr(event.Payload, &payload); err != nil {
			return nil, err
		}
		views = append(views, HubDeliveryView{HubDelivery: record, Topic: event.Topic, Payload: payload})
	}
	return views, nil
}

// MarkHubDeliverySent 在发送之前持久标记尝试；网络失败会在重投间隔后恢复。
// 不把发送次数作为成功处理依据，只有ACK会推进完成状态。
// 这里记录尝试而非网络对端收妥证明。
// 先标记再写网络，写失败会留下可再次发送的未确认记录。
// 不持数据库事务跨越网络写入，避免慢消费者锁住账本。
// 并发重复尝试是允许的至少一次语义，不承诺恰好一次传输。
// 已经确认的记录不会增加尝试次数。
func MarkHubDeliverySent(consumerID int, deliveryID int64) error {
	result := DB.Model(&HubDelivery{}).Where("id = ? AND consumer_id = ? AND acked_at = 0", deliveryID, consumerID).Updates(map[string]any{"last_sent_at": common.GetTimestamp(), "attempts": gorm.Expr("attempts + 1")})
	return result.Error
}

// AckHubDelivery 精确匹配消费者与已投递记录，重复确认保持幂等。
// 不能按最大编号一口气确认，避免并发客户端跳过未处理的消息。
// 客户端必须处理完单条通知后调用确认。
// 不能把事件编号直接当投递编号提交。
// 跨消费者编号不匹配会返回不存在，不泄露对方账本。
// HTTP层还会复查当前主题授权和关注用户。
// 同一记录多次确认保留第一次完成时间。
// 尚未尝试发送的记录不能通过猜测编号提前确认。
// 确认不触发退款、充值、接单或任何其他业务动作。
func AckHubDelivery(consumerID int, deliveryID int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// 与清理、分配共用消费者锁，确认结果不会与到期缺口统计交错。
		var consumer HubConsumer
		if err := lockForUpdate(tx).First(&consumer, consumerID).Error; err != nil {
			return err
		}
		var delivery HubDelivery
		if err := lockForUpdate(tx).Where("id = ? AND consumer_id = ?", deliveryID, consumerID).First(&delivery).Error; err != nil {
			return err
		}
		if delivery.LastSentAt == 0 {
			return ErrHubConflict
		}
		if delivery.AckedAt > 0 {
			return nil
		}
		return tx.Model(&delivery).Update("acked_at", common.GetTimestamp()).Error
	})
}

// ReplayHubEvent 为单个消费者重新打开投递，不更改原业务状态和资金。
// 当前主题、用户关注与保留期都必须匹配，管理操作不越过资源边界。
// 补发以事件编号定位，ACK仍使用生成的投递编号。
// 原事件不变，其他消费者的确认进度不受影响。
// 重新打开投递时保留尝试次数供审计。
// 超出保留期的事件不能复活，需从业务源核对状态。
// 当前没有投递时可以为保留期内历史事件补建一份。
// 同消费者行锁与后台分配共享，避免补发产生重复记录。
// 此接口只影响通知账本，不接受任意外部载荷。
func ReplayHubEvent(consumerID int, eventID int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var c HubConsumer
		if err := lockForUpdate(tx).First(&c, consumerID).Error; err != nil {
			return err
		}
		var event HubEvent
		if err := tx.First(&event, eventID).Error; err != nil {
			return err
		}
		topics, err := HubTopicList(c.Topics)
		if err != nil {
			return err
		}
		allowed, err := HubEventAllowed(&c, &event, topics)
		if err != nil {
			return err
		}
		var settings HubSettings
		if err := tx.First(&settings, 1).Error; err != nil {
			return err
		}
		if !allowed || event.CreatedAt < common.GetTimestamp()-int64(settings.RetentionDays)*86400 {
			return ErrHubForbidden
		}
		var delivery HubDelivery
		result := tx.Where("consumer_id = ? AND event_id = ?", consumerID, eventID).First(&delivery)
		if result.Error == gorm.ErrRecordNotFound {
			return tx.Create(&HubDelivery{ConsumerID: consumerID, EventID: eventID, CreatedAt: event.CreatedAt}).Error
		}
		if result.Error != nil {
			return result.Error
		}
		return tx.Model(&delivery).Updates(map[string]any{"acked_at": 0, "last_sent_at": 0}).Error
	})
}

// OpenHubConnection 通过短租约限制每消费者并发连接，防止慢客户端无限占用资源。
// 行锁使多节点也遵守同一八连接上限；浏览器和外部客户端一并计数。
// 八连接上限作用于逻辑消费者，多个令牌共用限额。
// 换Token不能绕过限额，也不会修改持久消费进度。
// 过期连接允许新连接接替，但旧连接不能再次恢复租约。
// 连接编号来自密码学随机源，不使用客户端自报标识。
// 进程崩溃无需执行退出回调，租约到期即显示离线。
// 连接计数来自主库，因此负载均衡到其他节点仍遵守限制。
func OpenHubConnection(consumerID, tokenID int) (string, error) {
	id, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return "", err
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		var c HubConsumer
		if err := lockForUpdate(tx).First(&c, consumerID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&HubConnection{}).Where("consumer_id = ? AND expires_at > ?", consumerID, common.GetTimestamp()).Count(&count).Error; err != nil {
			return err
		}
		if count >= 8 {
			return fmt.Errorf("%w: connection limit", ErrHubConflict)
		}
		return tx.Create(&HubConnection{ID: id, ConsumerID: consumerID, TokenID: tokenID, ExpiresAt: common.GetTimestamp() + 10}).Error
	})
	return id, err
}

// TouchHubConnection 仅在重鉴权成功后续租；过期租约不表示仍能读取业务事件。
// 续租必须早于当前租约到期时间。
// 租约已经删除时不能把零行更新解释为成功。
// 失去租约的连接交由传输层终止，不自动创建另一连接。
// 最近使用时间只表达认证与连接活跃，不表示业务处理完成。
// 更新时间不包括IP或认证头，管理统计不保存额外敏感信息。
func TouchHubConnection(id string, consumerID, tokenID int) error {
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		// 与新建连接共用消费者行锁，失租连接不能绕过八连接上限复活。
		var consumer HubConsumer
		if err := lockForUpdate(tx).First(&consumer, consumerID).Error; err != nil {
			return err
		}
		if !consumer.Enabled {
			return ErrHubForbidden
		}
		result := tx.Model(&HubConnection{}).Where("id = ? AND consumer_id = ? AND expires_at > ?", id, consumerID, now).Update("expires_at", now+10)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrHubConflict
		}
		// 只在仍拥有活跃租约时更新最近活动，零行不能伪装成成功。
		if err := tx.Model(&HubConsumer{}).Where("id = ?", consumerID).Update("last_seen_at", now).Error; err != nil {
			return err
		}
		if tokenID > 0 {
			return tx.Model(&HubToken{}).Where("id = ? AND consumer_id = ?", tokenID, consumerID).Update("last_used_at", now).Error
		}
		return nil
	})
}

// CleanupHubEvents 有界清理到期事件及其投递，未到保留期的数据不受影响。
// 连接租约只用于统计，清理失效租约不会删除消费者的消费进度。
// 各节点都可运行清理，固定设置行锁避免重复处理同一批。
// 消费者锁与分配、ACK采用一致顺序，防止孤立投递或错误缺口。
// 单次最多删除五百事件，剩余由后续周期继续处理。
// 保留期按事件时间计算，重投不会让旧事件永久留下。
// 未确认缺口包含禁用或离线期间尚未物化的事件。
// 缺口累计只作历史事实提示，不能自动当作业务已对账。
// 已经确认的消息到期后不累计缺口。
// 关注用户过滤仍适用于缺口统计，不能泄露其他用户事件数量。
// 连接租约清理与事件清理独立，不删除消费者本身。
// 无外部依赖参与本事务，连接断开不会影响清理正确性。
// 删除前先累计缺口，任何步骤失败会一起回滚。
// 新产生事件不在本批编号中，下一轮才重新选择过期集合。
// 管理员调整保留期时通过设置行锁与本次清理串行。
// 清理不会修改订单、钱包、订阅或已结算费用。
func CleanupHubEvents() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// 设置单行锁序列化各节点清理，避免相同缺口被重复累计。
		// 再按消费者编号持锁，与分配和ACK统一资源顺序。
		var settings HubSettings
		if err := lockForUpdate(tx).First(&settings, 1).Error; err != nil {
			return err
		}
		var consumers []HubConsumer
		if err := lockForUpdate(tx).Order("id ASC").Limit(100).Find(&consumers).Error; err != nil {
			return err
		}
		var ids []int64
		if err := tx.Model(&HubEvent{}).Where("created_at < ?", common.GetTimestamp()-int64(settings.RetentionDays)*86400).Order("id ASC").Limit(500).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) > 0 {
			for _, consumer := range consumers {
				topics, err := HubTopicList(consumer.Topics)
				if err != nil {
					return err
				}
				users, err := HubConsumerUsers(&consumer)
				if err != nil {
					return err
				}
				// 包含离线或禁用期间尚未分配的事件，过期不能假装全部消费完成。
				query := tx.Table("hub_events").Joins("LEFT JOIN hub_deliveries ON hub_deliveries.event_id = hub_events.id AND hub_deliveries.consumer_id = ?", consumer.ID).Where("hub_events.id IN ? AND hub_events.topic IN ? AND hub_events.created_at >= ? AND (hub_deliveries.id IS NULL OR hub_deliveries.acked_at = 0)", ids, topics, consumer.CreatedAt)
				if len(users) > 0 {
					query = query.Where("hub_events.user_id IN ?", users)
				}
				var gap int64
				if err := query.Count(&gap).Error; err != nil {
					return err
				}
				if gap > 0 {
					if err := tx.Model(&HubConsumer{}).Where("id = ?", consumer.ID).Update("expired_unacked_count", gorm.Expr("expired_unacked_count + ?", gap)).Error; err != nil {
						return err
					}
				}
			}
			if err := tx.Where("event_id IN ?", ids).Delete(&HubDelivery{}).Error; err != nil {
				return err
			}
			if err := tx.Where("id IN ?", ids).Delete(&HubEvent{}).Error; err != nil {
				return err
			}
		}
		return tx.Where("expires_at < ?", common.GetTimestamp()-60).Delete(&HubConnection{}).Error
	})
}
