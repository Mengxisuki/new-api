package authz

import "github.com/QuantumNous/new-api/common"

// resolveSubjectRoles returns the role keys assigned to a subject. The mapping
// is derived from the caller's system role.
var resolveSubjectRoles = func(userID int, systemRole int) []string {
	switch {
	case systemRole >= common.RoleRootUser:
		return []string{BuiltInRoleRoot}
	case systemRole >= common.RoleAdminUser:
		return []string{BuiltInRoleAdmin}
	case systemRole == common.RoleSalesUser:
		return []string{BuiltInRoleSales} // 不继承财务或管理员资源。
	case systemRole == common.RoleFinanceUser:
		return []string{BuiltInRoleFinance} // 财务不因数字较高而继承管理员基线。
	default:
		return nil
	}
}

// managedRoleKey is the role whose baseline per-user overrides are expressed
// relative to.
const managedRoleKey = BuiltInRoleAdmin
