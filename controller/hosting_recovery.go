package controller

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/hosting"
)

// hostingRecoveryHandler 复用系统任务的数据库租约、续租和失锁取消。
// 任务仅记录脱敏结果；原厂不可逆重置不属于自动恢复范围。
type hostingRecoveryHandler struct{}

// 独立任务类型让恢复失败可在现有后台任务列表定位。
func (hostingRecoveryHandler) Type() string { return "hosting_recovery" }

// 本地地址与持久密钥完整时启用，不再依赖执行层管理员权限。
// 检查本身不联网，执行失败必须由任务结果显式记录。
// 禁用用户由验证端拒绝，后台恢复不能绕过当前用户状态。
func (hostingRecoveryHandler) Enabled() bool {
	_, err := hosting.NewConfiguredClient()
	return err == nil
}

// 分钟级检查只处理长期未完成记录，不对每次页面访问产生外部请求。
func (hostingRecoveryHandler) Interval() time.Duration { return time.Minute }

// 恢复依据持久账号状态，无需保存或序列化任何授权材料。
func (hostingRecoveryHandler) NewPayload() any { return nil }

// Run 由任务执行器持有租约后调用，最终状态必须写回已有任务表。
// 租约取消会传给控制面 HTTP 请求，不能继续处理后续账号。
func (hostingRecoveryHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	if err := hosting.ReconcilePendingAccounts(ctx); err != nil {
		// 数据库故障保留在服务器诊断，任务展示稳定错误，避免泄露内部字段。
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, &hosting.Error{Status: 500, Code: "hosting_recovery_failed"})
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, nil, nil)
}
