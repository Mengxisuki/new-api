package hosting

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

// ReconcilePendingAccounts 由现有系统任务租约保护，跨副本只有一个执行者。
// 只恢复已接受的账号创建和删除，不自动启动授权、刷新额度或消耗重置卡。
// 一批最多二十个，先处理最久未更新的记录，避免控制面长时间占用任务。
// 调用方必须持有系统任务租约，不能由多个无锁定时器直接触发。
// 单个账号失败不会阻止处理本批其他账号，但最终任务仍标记失败。
// 调度上下文取消后必须结束，不能在租约丢失后继续写远端状态。
// 已接受的消费授权保存原 UUID，恢复不能因为过期另造新 Stage。
// 删除恢复只确认原资源删除，不能新建账号弥补状态不一致。
// 未找到原授权属于数据故障，需要记录而不是跳过并声称成功。
// 错误码和账号 ID 足以定位失败资源，错误正文不进入日志。
// 本函数不清除原厂重置未决记录，也不调用消费接口。
func ReconcilePendingAccounts(ctx context.Context) error {
	var accounts []model.HostingAccount
	// 给前台首次请求保留两分钟，减少后台与交互创建同时收敛的机会。
	// 时间窗口不是幂等保障；最终仍依赖远端稳定 operation_id 和本地状态条件。
	if err := model.DB.WithContext(ctx).Where("status IN ? AND updated_at < ?", []string{"provisioning", "deleting"}, time.Now().Add(-2*time.Minute).Unix()).Order("updated_at ASC").Limit(20).Find(&accounts).Error; err != nil {
		return err
	}
	var failures []error
	for _, account := range accounts {
		// 租约失效会取消上下文，下一项不能继续向执行层发送变更。
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		var err error
		if account.Status == "deleting" {
			// 原账号删除可重复确认，密钥直到执行层确认后才被清除。
			err = DeleteAccount(ctx, account.UserID, account.ID)
		} else {
			var authorization model.HostingAuthorization
			// 已有授权保存了稳定操作身份；后台不能自造新的 Stage 或 UUID。
			err = model.DB.Where("account_id = ? AND user_id = ? AND status = ?", account.ID, account.UserID, "consuming").Order("created_at DESC").First(&authorization).Error
			if err == nil {
				identity := model.AuthSessionIdentity{UserID: authorization.UserID, SessionID: authorization.SessionID, UserAuthVersion: authorization.UserAuthVersion, SessionVersion: authorization.SessionVersion}
				// 创建接口先查幂等结果；执行层必须保证不会重复消费已完成的 Stage。
				_, err = CreateAccount(ctx, identity, authorization.ID, account.Alias)
			}
		}
		if err != nil {
			// 用户只看稳定错误码，任务中也不写入管理密钥、原始授权或远端正文。
			// 下一轮仍可恢复同一操作，失败不会被改成成功或删除记录。
			code := "hosting_recovery_database_failed"
			var controlError *Error
			if errors.As(err, &controlError) {
				code = controlError.Code
			}
			if errors.Is(err, model.ErrHostingConflict) {
				code = "hosting_recovery_state_conflict"
			}
			// 诊断记录仅含本地账号、固定错误码和 Go 错误类型，不记录原始异常正文。
			logger.LogError(ctx, common.LogText("hosting recovery failed (account_id=%d, code=%s, error_type=%T)", account.ID, code, err))
			persistErr := model.DB.Model(&account).Where("status IN ?", []string{"provisioning", "deleting"}).Update("last_error", code).Error
			failures = append(failures, errors.Join(err, persistErr))
		}
	}
	return errors.Join(failures...)
}
