package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// hubRelayObservation 把已验证的计费上下文投影为最小结算事实。
// 不携带原请求、上游地址、Key 或日志正文。
// 请求编号由平台生成；同一请求重试不会形成多份费用样本。
// 总额由真实结算传入，不能从日志文字或当前价格反推。
// 平台钱包和订阅使用相同计费额度单位，但历史基线相互隔离。
// 异步任务在完成前保持 pending，提交价不能直接充当最终价。
func hubRelayObservation(info *relaycommon.RelayInfo, actualQuota int) model.HubChargeObservation {
	funding := info.BillingSource
	if funding == "" {
		funding = BillingSourceWallet
	}
	observation := model.HubChargeObservation{
		RequestKey: "request:" + info.RequestId, UserID: info.UserId,
		ModelName: info.GetBillingModelName(), Funding: funding,
		ActualQuota: actualQuota, State: model.HubChargeFinal,
	}
	// 任务身份在上游提交前冻结，轮询复用相同的平台任务编号。
	// 不能用上游私有任务编号替代它或把两者放进事件。
	if info.TaskRelayInfo != nil && info.PublicTaskID != "" {
		// 任务快照沿用OriginModelName定价合同，后续轮询必须匹配同一身份。
		observation.ModelName = info.OriginModelName
		observation.TaskID = info.PublicTaskID
		observation.RequestKey = "task:" + info.PublicTaskID
		observation.State = model.HubChargePending
	}
	return observation
}

// settleHubBillingFunding 在真正资金提交边界写最终观察。
// 观察失败与差额资金写入一起回滚；缓存确认失败继续向上返回。
// 已提交的幂等重复返回成功，不再次扣费。
// 返回是否实际应用，防止资金幂等后 Key 预算仍被二次扣减。
// 同一资金观察的缓存未确认会返回错误，调用方不得自动退款。
// 会话仍负责 Key 预算调整、来源冻结和退款生命周期。
func settleHubBillingFunding(info *relaycommon.RelayInfo, actualQuota, delta int) (bool, error) {
	if info.RequestId == "" {
		// 老的内部调用可能没有 HTTP 请求编号，首次生成后沿用。
		// 不能每次结算随机生成而使重投失去幂等保护。
		info.RequestId = common.NewRequestId()
	}
	return model.ApplyHubChargeSettlement(hubRelayObservation(info, actualQuota), delta, info.SubscriptionId, 0)
}

// settleHubTaskFunding 统一任务最终费用与失败退款的持久事实。
// 返回 false 表示相同结果此前已提交，调用方必须跳过 Key 二次调整。
// 退款状态从历史基线移除，补扣只更改整笔总额而不增加一个样本。
// 数据库任务费用、资金差额和观察由同一事务保护。
func settleHubTaskFunding(task *model.Task, actualQuota int, state string) (bool, error) {
	funding := task.PrivateData.BillingSource
	if funding == "" {
		funding = BillingSourceWallet
	}
	if task.TaskID == "" || task.ID <= 0 {
		return false, fmt.Errorf("task event settlement requires a persisted task")
	}
	observation := model.HubChargeObservation{
		RequestKey: "task:" + task.TaskID, TaskID: task.TaskID,
		UserID: task.UserId, ModelName: taskModelName(task), Funding: funding,
		ActualQuota: actualQuota, State: state,
	}
	return model.ApplyHubChargeSettlement(observation, actualQuota-task.Quota, task.PrivateData.SubscriptionId, task.ID)
}
