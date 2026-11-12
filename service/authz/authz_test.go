package authz

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newAuthzTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() {
		common.IsMasterNode = wasMaster
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	return db
}

func TestInitSeedsBuiltInRolesAndPoliciesOnce(t *testing.T) {
	db := newAuthzTestDB(t)

	require.NoError(t, Init(db))
	require.NoError(t, Init(db))

	// root is a superuser role and is granted everything implicitly, so only the
	// admin baseline is written as explicit policy rows.
	var count int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Count(&count).Error)
	assert.Equal(t, int64(len(PermissionsForRole(BuiltInRoleAdmin))+len(PermissionsForRole(BuiltInRoleFinance))+len(PermissionsForRole(BuiltInRoleSales))), count)

	var roles []model.AuthzRole
	require.NoError(t, db.Order("sort asc").Find(&roles).Error)
	require.Len(t, roles, 4)
	assert.Equal(t, BuiltInRoleRoot, roles[0].Key)
	assert.Equal(t, BuiltInRoleAdmin, roles[1].Key)
	assert.Equal(t, BuiltInRoleFinance, roles[2].Key)
	assert.Equal(t, BuiltInRoleSales, roles[3].Key) // 业务员种子与策略都须幂等。

	assert.True(t, Can(1, common.RoleRootUser, ChannelSensitiveWrite))
	assert.True(t, Can(2, common.RoleAdminUser, ChannelRead))
	assert.True(t, Can(2, common.RoleAdminUser, ChannelOperate))
	assert.True(t, Can(2, common.RoleAdminUser, ChannelWrite))
	assert.False(t, Can(2, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.False(t, Can(3, common.RoleCommonUser, ChannelRead))
}

func TestInitOnSlaveOnlyLoadsPolicies(t *testing.T) {
	wasMaster := common.IsMasterNode
	common.IsMasterNode = false
	t.Cleanup(func() {
		common.IsMasterNode = wasMaster
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))

	require.NoError(t, Init(db))

	var roleCount int64
	require.NoError(t, db.Model(&model.AuthzRole{}).Count(&roleCount).Error)
	assert.Equal(t, int64(0), roleCount)
	var policyCount int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Count(&policyCount).Error)
	assert.Equal(t, int64(0), policyCount)
	assert.False(t, Can(2, common.RoleAdminUser, ChannelRead))
}

func TestLegacyScopedPoliciesDoNotExpandPermissions(t *testing.T) {
	for _, master := range []bool{true, false} {
		name := "master"
		if !master {
			name = "slave"
		}
		t.Run(name, func(t *testing.T) {
			db := newAuthzTestDB(t)
			common.IsMasterNode = master
			rules := []model.CasbinRule{
				{Ptype: "p", V0: "role:vendor", V1: "channel", V2: "read", V3: "allow", V4: "own"},
				{Ptype: "p", V0: UserSubject(42), V1: "channel", V2: "read", V3: "allow", V4: "own"},
				{Ptype: "p", V0: UserSubject(43), V1: "channel", V2: "sensitive_write", V3: "allow", V4: "all"},
				{Ptype: "p", V0: UserSubject(44), V1: "channel", V2: "secret_view", V3: "deny", V4: "all"},
				{Ptype: "p", V0: UserSubject(45), V1: "channel", V2: "read"},
				{Ptype: "p", V0: UserSubject(46), V1: "channel", V2: "read", V3: "allow", V4: "unknown-scope"},
				{Ptype: "p", V0: UserSubject(47), V1: "channel", V2: "read", V3: "allow", V5: "own"},
				{Ptype: "p", V0: UserSubject(48), V1: "channel", V2: "read", V3: "allow", V4: "own"},
				{Ptype: "p", V0: UserSubject(48), V1: "channel", V2: "read", V3: "allow", V4: "all"},
				{Ptype: "p", V0: RoleSubject(BuiltInRoleAdmin), V1: "channel", V2: "operate", V3: "allow", V4: "own"},
				{Ptype: "p", V0: UserSubject(50), V1: "channel", V2: "sensitive_write", V3: "allow"},
				{Ptype: "g", V0: UserSubject(99), V1: RoleSubject(BuiltInRoleAdmin)},
			}
			require.NoError(t, db.Create(&rules).Error)
			ids := make([]uint, len(rules))
			for i := range rules {
				ids[i] = rules[i].Id
			}
			for range 2 {
				require.NoError(t, Init(db))
				require.NoError(t, ReloadPolicy())
				assert.False(t, Can(42, common.RoleAdminUser, ChannelRead), "own must not fall back to the admin allow baseline")
				assert.True(t, Can(43, common.RoleAdminUser, ChannelSensitiveWrite))
				assert.False(t, Can(44, common.RoleAdminUser, ChannelSecretView))
				assert.True(t, Can(45, common.RoleAdminUser, ChannelRead))
				for _, userID := range []int{46, 47, 48} {
					assert.False(t, Can(userID, common.RoleAdminUser, ChannelRead))
				}
				assert.False(t, Can(51, common.RoleAdminUser, ChannelOperate), "reseed must not erase a scoped role restriction")
				assert.True(t, Can(50, common.RoleAdminUser, ChannelSensitiveWrite))
				assert.False(t, Can(99, common.RoleCommonUser, ChannelRead))
				var stored []model.CasbinRule
				require.NoError(t, db.Where("id IN ?", ids).Order("id").Find(&stored).Error)
				assert.Equal(t, rules, stored, "legacy rows must remain available for administrator review")
			}
		})
	}
}

func TestSetUserPermissionsStoresOnlyOverrides(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	require.NoError(t, SetUserPermissions(42, PermissionsMap{
		ResourceChannel: {
			ActionRead:           true,
			ActionOperate:        true,
			ActionWrite:          false,
			ActionSensitiveWrite: true,
			ActionSecretView:     false,
			"unknown":            true,
		},
		"unknown": {
			ActionRead: true,
		},
	}))

	assert.True(t, Can(42, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.False(t, Can(42, common.RoleAdminUser, ChannelWrite))
	assert.Equal(t, PermissionsMap{
		ResourceChannel: {
			ActionRead:           true,
			ActionOperate:        true,
			ActionWrite:          false,
			ActionSensitiveWrite: true,
			ActionSecretView:     false,
		},
		ResourceTaskPlugin: {
			ActionBind: false,
		},
		ResourceAudit:            {ActionRead: false},
		ResourceFinance:          {ActionRead: true, ActionWrite: true},
		ResourceFinanceOverview:  {ActionRead: false},
		ResourceRechargeOrders:   {ActionRead: true, ActionWrite: true, "reveal": true, "assign": true, "refund": true},
		ResourceRechargeProducts: {ActionRead: true, ActionWrite: true},
	}, ExplicitUserPermissions(42))
	assert.Equal(t, PermissionsMap{
		ResourceChannel: {
			ActionSensitiveWrite: true,
			ActionWrite:          false,
		},
	}, ExplicitUserOverrides(42))

	var userPolicyCount int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Where("v0 = ?", UserSubject(42)).Count(&userPolicyCount).Error)
	assert.Equal(t, int64(2), userPolicyCount)

	require.NoError(t, SetUserPermissions(42, PermissionsMap{ResourceChannel: {
		ActionRead:           true,
		ActionOperate:        true,
		ActionWrite:          true,
		ActionSensitiveWrite: false,
		ActionSecretView:     false,
	}}))
	assert.False(t, Can(42, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.Equal(t, PermissionsMap{
		ResourceChannel: {
			ActionRead:           true,
			ActionOperate:        true,
			ActionWrite:          true,
			ActionSensitiveWrite: false,
			ActionSecretView:     false,
		},
		ResourceTaskPlugin: {
			ActionBind: false,
		},
		ResourceAudit:            {ActionRead: false},
		ResourceFinance:          {ActionRead: true, ActionWrite: true},
		ResourceFinanceOverview:  {ActionRead: false},
		ResourceRechargeOrders:   {ActionRead: true, ActionWrite: true, "reveal": true, "assign": true, "refund": true},
		ResourceRechargeProducts: {ActionRead: true, ActionWrite: true},
	}, ExplicitUserPermissions(42))
	assert.Empty(t, ExplicitUserOverrides(42))
}

func TestClearUserAuthorizationRemovesOverrides(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	require.NoError(t, SetUserPermissions(90, PermissionsMap{ResourceChannel: {
		ActionWrite:          false,
		ActionSensitiveWrite: true,
	}}))

	assert.True(t, Can(90, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.False(t, Can(90, common.RoleAdminUser, ChannelWrite))

	require.NoError(t, ClearUserAuthorization(90))

	assert.Empty(t, ExplicitUserOverrides(90))
	assert.True(t, Can(90, common.RoleAdminUser, ChannelRead))
	assert.True(t, Can(90, common.RoleAdminUser, ChannelWrite))
	assert.False(t, Can(90, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.False(t, Can(90, common.RoleCommonUser, ChannelRead))
}

func TestSetUserPermissionsInTxDoesNotMutateEnforcerBeforeReload(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return SetUserPermissionsInTx(tx, 42, PermissionsMap{ResourceChannel: {
			ActionRead:           true,
			ActionOperate:        true,
			ActionWrite:          true,
			ActionSensitiveWrite: true,
			ActionSecretView:     false,
		}})
	}))

	assert.False(t, Can(42, common.RoleAdminUser, ChannelSensitiveWrite))
	require.NoError(t, ReloadPolicy())
	assert.True(t, Can(42, common.RoleAdminUser, ChannelSensitiveWrite))
}

func TestSetUserPermissionsInTxRollbackLeavesNoPolicy(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	tx := db.Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, SetUserPermissionsInTx(tx, 43, PermissionsMap{ResourceChannel: {
		ActionSensitiveWrite: true,
	}}))
	require.NoError(t, tx.Rollback().Error)
	require.NoError(t, ReloadPolicy())

	assert.False(t, Can(43, common.RoleAdminUser, ChannelSensitiveWrite))
	var count int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Where("v0 = ?", UserSubject(43)).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestAdapterAddPolicyIsIdempotent(t *testing.T) {
	db := newAuthzTestDB(t)
	adapter := newGormAdapter(db)
	rule := []string{UserSubject(55), ResourceChannel, ActionSensitiveWrite, EffectAllow}

	require.NoError(t, adapter.AddPolicy("p", "p", rule))
	require.NoError(t, adapter.AddPolicy("p", "p", rule))

	var count int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Where(
		"ptype = ? AND v0 = ? AND v1 = ? AND v2 = ? AND v3 = ?",
		"p",
		UserSubject(55),
		ResourceChannel,
		ActionSensitiveWrite,
		EffectAllow,
	).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestCapabilitiesUseCatalogShape(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	capabilities := Capabilities(7, common.RoleAdminUser)

	assert.True(t, capabilities[ResourceChannel][ActionRead])
	assert.True(t, capabilities[ResourceChannel][ActionOperate])
	assert.True(t, capabilities[ResourceChannel][ActionWrite])
	assert.False(t, capabilities[ResourceChannel][ActionSensitiveWrite])
	assert.False(t, capabilities[ResourceChannel][ActionSecretView])
	assert.False(t, capabilities[ResourceTaskPlugin][ActionBind])
}

func TestTaskPluginBindIsRootOnlyUntilGranted(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	var bindAction *ActionDefinition
	for _, resource := range Catalog() {
		if resource.Resource != ResourceTaskPlugin {
			continue
		}
		assert.Equal(t, "Task Plugin", resource.LabelKey)
		for i := range resource.Actions {
			if resource.Actions[i].Action == ActionBind {
				bindAction = &resource.Actions[i]
			}
		}
	}
	require.NotNil(t, bindAction)
	assert.Equal(t, "Bind task plugins", bindAction.LabelKey)
	assert.Equal(t, "List registered task plugins and bind them when creating or editing task plugin channels.", bindAction.DescriptionKey)
	assert.Empty(t, bindAction.DefaultRoles)

	assert.False(t, Can(2, common.RoleAdminUser, TaskPluginBind))
	assert.True(t, Can(1, common.RoleRootUser, TaskPluginBind))

	enforcer := currentEnforcer()
	require.NotNil(t, enforcer)
	_, err := enforcer.AddPolicy(RoleSubject(BuiltInRoleAdmin), ResourceTaskPlugin, ActionBind, EffectAllow)
	require.NoError(t, err)
	assert.True(t, Can(2, common.RoleAdminUser, TaskPluginBind))

	_, err = enforcer.RemovePolicy(RoleSubject(BuiltInRoleAdmin), ResourceTaskPlugin, ActionBind, EffectAllow)
	require.NoError(t, err)
	assert.False(t, Can(2, common.RoleAdminUser, TaskPluginBind))
}

// 财务是独立的受限系统角色，不能凭数字排序进入既有管理员功能。
// 真实策略种子、角色解析和权限快照共同验证，而非模拟 Can 返回值。
// 历史管理员覆盖即使残留，也不能在降为财务后恢复渠道权限。
func TestFinanceRoleOnlyGrantsFinanceCapabilities(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))
	assert.True(t, common.IsValidateRole(5)) // 财务角色必须能被登录身份和用户 API 接受。
	for _, action := range []string{"read", "write"} {
		permission := Permission{Resource: "finance", Action: action}
		assert.True(t, Can(501, 5, permission))                      // 财务人员获得开票工作区的默认权限。
		assert.True(t, Can(502, common.RoleAdminUser, permission))   // 管理员仍可办理财务工作。
		assert.False(t, Can(503, common.RoleCommonUser, permission)) // 用户不能读取他人的财务数据。
	}
	require.NoError(t, SetUserPermissions(501, PermissionsMap{ResourceChannel: {ActionSensitiveWrite: true}}))
	assert.False(t, Can(501, 5, ChannelSensitiveWrite)) // 旧覆盖不能跨越新角色的资源边界。
	assert.False(t, Can(501, 5, ChannelRead))           // 只读渠道访问也不属于开票工作区。
	assert.False(t, Can(501, 5, AuditRead))             // 财务不能默认读取其它账号的通用审计。
	assert.False(t, Can(501, 5, TaskPluginBind))
	assert.True(t, Can(504, common.RoleRootUser, Permission{Resource: "finance", Action: "write"}))
	// 财务总览没有默认角色：管理员和财务都要由 Root 逐用户授予，Root 自身始终可见。
	assert.False(t, Can(501, 5, FinanceOverviewRead))
	assert.False(t, Can(502, common.RoleAdminUser, FinanceOverviewRead))
	assert.True(t, Can(504, common.RoleRootUser, FinanceOverviewRead))
	for _, userID := range []int{501, 502} {
		require.NoError(t, SetUserPermissions(userID, PermissionsMap{ResourceFinanceOverview: {ActionRead: true}}))
	}
	assert.True(t, Can(501, 5, FinanceOverviewRead)) // 财务角色的硬边界为这一项资源放行。
	assert.True(t, Can(502, common.RoleAdminUser, FinanceOverviewRead))
	assert.False(t, Can(501, 5, ChannelRead))                             // 放行总览不能连带打开其他资源。
	assert.False(t, Can(501, common.RoleSalesUser, FinanceOverviewRead))  // 改任业务员后旧授权立即失效。
	assert.False(t, Can(501, common.RoleCommonUser, FinanceOverviewRead)) // 普通用户即使残留授权也不可见。
	require.NoError(t, SetUserPermissions(501, PermissionsMap{ResourceFinanceOverview: {ActionRead: false}}))
	assert.False(t, Can(501, 5, FinanceOverviewRead)) // Root 可以单独收回。
}
