package controller

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// TestHostingLogAccountDisplay 通过真实日志读取入口验证历史编号的显示投影。
// fixture 使用无效测试凭据和示例域名，不需要真实用户或原厂网络。
// 接口数据断言复用生产 JSON 包装器，不手工构造预期补全结果。
// 各视图查询同一条存量记录，保护兼容性不是仅对新日志生效。
// 不检查内部 helper 的调用次数，允许后续优化批量读取方式。
// 此用例不验证计费数值，补充身份也不得进入钱包结算链。
// 主库和日志库可能分开，账号身份必须由主库按日志所有者匹配。
// 不模拟数据库查询，三种方言沿用现有隔离测试环境。
// 删除账号保留业务墓碑，历史日志仍能显示其备注与脱敏邮箱。
// 故意把他人编号放进本人的旧日志，验证不会跨所有者补充身份。
// 没有资源引用的旧日志不需要查询账号，也不应被补造来源。
// 超大整数必须原样保留，不能因为补身份而经过 float64 丢失精度。
func TestHostingLogAccountDisplay(t *testing.T) {
	user, _ := hostingTestDB(t)
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}))
	// 三个账号只在所有权与生命周期上不同，避免由无关字段触发过滤。
	// 私密字段刻意非空，让“不泄露凭据”的断言具有实际辨别力。
	accounts := []model.HostingAccount{
		{UserID: user.Id, Provider: "codex", Alias: "科研账号", EmailMask: "lab***@example.com", RemoteRef: "log-owned", Status: "active", EncryptedKey: "must-not-expose"},
		{UserID: user.Id, Provider: "codex", Alias: "已解绑账号", EmailMask: "old***@example.com", RemoteRef: "log-deleted", Status: "deleted"},
		{UserID: user.Id + 1, Provider: "codex", Alias: "他人的账号", EmailMask: "foreign***@example.com", RemoteRef: "log-foreign", Status: "active"},
	}
	require.NoError(t, model.DB.Create(&accounts).Error)
	// 第一条旧记录同时含实际使用账号与回退账号，均需正确解析。
	// 历史编号只用于匹配，响应不能带出执行层引用或加密密钥。
	original := fmt.Sprintf(`{"resource_source":"hosting","hosting_account_id":%d,"resource_skip_reasons":["hosting:%d:quota_exhausted","hosting:%d:account_unavailable"],"large":9007199254740993}`, accounts[0].ID, accounts[1].ID, accounts[2].ID)
	entry := model.Log{UserId: user.Id, Type: model.LogTypeConsume, TokenId: 44, Other: original}
	require.NoError(t, model.LOG_DB.Create(&entry).Error)
	// 管理员、自用和令牌读取是三个既有日志入口，不能只修某一种视图。
	// 令牌入口只按 token_id 过滤，其账号归属仍应使用日志的 UserId。
	// 管理员入口可查所有日志，但也不能向某条记录拼入不相关的身份。
	for _, view := range []string{"user", "admin", "token"} {
		t.Run(view, func(t *testing.T) {
			var logs []*model.Log
			var err error
			switch view {
			case "user":
				logs, _, err = model.GetUserLogs(user.Id, model.LogFilter{}, 0, 10)
			case "admin":
				logs, _, err = model.GetAllLogs(model.LogFilter{}, 0, 10)
			case "token":
				logs, err = model.GetLogByTokenId(44)
			}
			require.NoError(t, err)
			require.Len(t, logs, 1)
			// 仅检查新增显示投影，不要求重写历史来源及回退代码。
			var other struct {
				Accounts map[string]struct {
					Alias     string `json:"alias"`
					EmailMask string `json:"email_mask"`
				} `json:"hosting_accounts"`
			}
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
			// 删除墓碑必须可辨认，而外来账号引用必须被排除。
			// 邮箱只允许已存掩码，不从远端引用或凭据中还原完整身份。
			require.Len(t, other.Accounts, 2)
			require.Equal(t, "科研账号", other.Accounts[fmt.Sprint(accounts[0].ID)].Alias)
			require.Equal(t, "lab***@example.com", other.Accounts[fmt.Sprint(accounts[0].ID)].EmailMask)
			require.Equal(t, "已解绑账号", other.Accounts[fmt.Sprint(accounts[1].ID)].Alias)
			require.NotContains(t, logs[0].Other, "foreign***")
			require.NotContains(t, logs[0].Other, "must-not-expose")
			require.NotContains(t, logs[0].Other, "log-owned")
			// 超大整数超过 JavaScript 安全范围，可暴露 map[string]any 的精度损失。
			require.Contains(t, logs[0].Other, "9007199254740993")
		})
	}
	// 展示补充是只读操作，不能静默迁移或改写原始审计记录。
	var stored model.Log
	require.NoError(t, model.LOG_DB.First(&stored, entry.Id).Error)
	require.Equal(t, original, stored.Other)
	// 无挂靠字段的普通历史记录仍可读取，不能被新查询依赖影响。
	plain := model.Log{UserId: user.Id, Type: model.LogTypeConsume, TokenId: 55, Other: "{}"}
	require.NoError(t, model.LOG_DB.Create(&plain).Error)
	// 主库查询失败必须可见，不能伪装成账号已删除。
	require.NoError(t, model.DB.Migrator().DropTable(&model.HostingAccount{}))
	_, _, err := model.GetUserLogs(user.Id, model.LogFilter{}, 0, 10)
	require.Error(t, err)
	// 故障注入只删除本用例的隔离表；普通日志没有账号引用仍应成功。
	// 这区别于吞掉查询错误：有账号引用的前一次读取已明确失败。
	plainLogs, err := model.GetLogByTokenId(55)
	require.NoError(t, err)
	require.Len(t, plainLogs, 1)
	require.JSONEq(t, "{}", plainLogs[0].Other)
}
