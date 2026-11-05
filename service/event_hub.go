package service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// 事件令牌生成独立于个人管理令牌，单次证明不能跨消费者复用。
// 上下文将主题与有效期冻结，避免验证后替换授权范围。
const VerificationScopeHubToken = "event_hub.token"

type HubTokenVerificationContext struct {
	ConsumerID int      `json:"consumer_id"` // 必须明确目标消费者。
	Topics     []string `json:"topics"`      // 规范化排序的主题集合。
	ExpiresAt  int64    `json:"expires_at"`  // 服务端校验绝对到期时间。
}

// RunEventHub 定期恢复检测与投递，不依赖内存通知或Redis可用性。
// 各节点都可执行：观察行锁和投递唯一约束负责幂等，节点不共享游标。
// 暂停仅影响网络发送，后台仍物化通知以供恢复与积压统计。
// 批次有界；错误显式记录，下轮从持久状态继续。
// 告警检测先于本轮分配，新产生的告警可以直接进入消费者账本。
// 消费者数量受创建入口限制，扫描不会无限增长。
// 每个消费者的订阅使用持久快照，不取客户端连接中的临时参数。
// 无客户端在线时仍生成投递记录，管理页可观察积压。
// 网络发送仅在HTTP连接中执行，工作器不会持有客户端连接。
// 禁用投递设置不会阻止检测或资料提交事件保存。
// 未决任务不凭状态猜测最终费用，保留给账务对账。
// 数据库读取失败要记录并等待下个周期，不能用空列表宣称完成。
// 每六十秒执行有界清理，不在每次业务付款中触发维护。
// 任务循环响应取消信号，服务退出时不会再开始新的扫描。
// 不选主也不依赖Redis锁，数据库事务提供跨节点幂等边界。
// 这里不调用邮件通知通道，避免混用发送限额吞掉业务事件。
// 投递延迟通常为一个扫描周期，积压恢复时间取决于有界批次。
// 持久账本负责恢复，进程重启不需要重建内存中的历史队列。
func RunEventHub(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	rounds := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := model.ProcessHubChargeObservations(ctx); err != nil {
			common.SysError(fmt.Sprintf("event hub charge recovery failed: %v", err))
		}
		var consumers []model.HubConsumer
		if err := model.DB.Where("enabled = ?", true).Order("id ASC").Limit(100).Find(&consumers).Error; err != nil {
			common.SysError(fmt.Sprintf("event hub consumers scan failed: %v", err))
			continue
		}
		for _, consumer := range consumers {
			if err := model.ValidateHubOwner(&consumer); err != nil {
				// 授权失效应停投递；数据库失败同样失败关闭并留下定位线索。
				if err != model.ErrHubForbidden {
					common.SysError(fmt.Sprintf("event hub owner validation failed: %v", err))
				}
				continue
			}
			topics, err := model.HubTopicList(consumer.Topics)
			if err == nil {
				_, err = model.MaterializeHubDeliveries(consumer.ID, topics)
			}
			if err != nil {
				common.SysError(fmt.Sprintf("event hub delivery allocation failed: %v", err))
			}
		}
		rounds++
		if rounds%30 == 0 {
			if err := model.CleanupHubEvents(); err != nil {
				common.SysError(fmt.Sprintf("event hub cleanup failed: %v", err))
			}
		}
	}
}
