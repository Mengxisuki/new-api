package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 财务令牌的可授予范围来自真实权限目录，不复制前端角色判断。
// 目录只是可见范围；Normalize 还必须拒绝伪造请求中的其它管理 scope。
// 现有静态管理资源仍保留管理员门槛，财务不能授权用户或系统设置。
func TestFinanceAccessTokenScopesRemainWithinRole(t *testing.T) {
	oldMaster := common.IsMasterNode // 仅在本场景种入真实内置角色，随后恢复宿主状态。
	common.IsMasterNode = true       // 内置财务策略通过正式初始化生成。
	t.Cleanup(func() { common.IsMasterNode = oldMaster })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB() // 策略由真实SQLite持久化，不能用mock替代授权存储。
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, connection.Close()) }) // 不留下长期运行的测试数据库连接。
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(db)) // 复用正式角色种子与默认拒绝行为。
	// 两个财务动作可以授权，但不因 scope 中的 admin 分组获得其它权限。
	scopes, err := NormalizeAccessTokenScopes(701, common.RoleFinanceUser, []string{"finance:write", "finance:read"})
	require.NoError(t, err)
	assert.Equal(t, []string{"finance:read", "finance:write"}, scopes) // 明确允许两项，不借默认管理员scope集合授权。
	for _, scope := range []string{"channel:read", "user:read", "billing:write", "option:write"} {
		_, err := NormalizeAccessTokenScopes(701, common.RoleFinanceUser, []string{scope})
		assert.ErrorIs(t, err, ErrAccessTokenScopeForbidden) // 明确拒绝，不能过滤坏项后创建成功令牌。
	}
	_, err = NormalizeAccessTokenScopes(702, common.RoleCommonUser, []string{"finance:read"})
	assert.ErrorIs(t, err, ErrAccessTokenScopeForbidden) // 普通用户无法把猜出的财务scope写入令牌。
	// Root 撤销财务读权限后，新的令牌不能重新授予该动作。
	require.NoError(t, authz.SetUserPermissions(701, authz.PermissionsMap{authz.ResourceFinance: {authz.ActionRead: false}}))
	_, err = NormalizeAccessTokenScopes(701, common.RoleFinanceUser, []string{"finance:read"})
	assert.ErrorIs(t, err, ErrAccessTokenScopeForbidden)
	_, err = NormalizeAccessTokenScopes(701, common.RoleFinanceUser, []string{"finance:write"})
	require.NoError(t, err) // 单动作撤销不能意外删除另一项仍然有效的财务能力。
}
