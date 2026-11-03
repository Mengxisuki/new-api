package authz

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/casbin/casbin/v2"
)

// Can reports whether the subject may perform the permission. A superuser role
// short-circuits to allow. Otherwise a per-user override wins, then the union of
// the subject's role baselines applies.
func Can(userID int, systemRole int, permission Permission) bool {
	// 财务角色的资源边界高于历史用户覆盖，降权后不能沿用旧管理员权限。
	// 逐请求检查角色，旧缓存的用户覆盖不能扩大到渠道、审计或插件资源。
	if systemRole == common.RoleFinanceUser && permission.Resource != ResourceFinance {
		return false
	}
	// 业务员的硬边界优先于历史覆盖，不能通过遗留管理员授权执行退款。
	if systemRole == common.RoleSalesUser && (permission.Resource != ResourceRechargeOrders || (permission.Action != ActionRead && permission.Action != ActionWrite && permission.Action != "reveal")) {
		return false
	}
	roles := resolveSubjectRoles(userID, systemRole)
	if len(roles) == 0 {
		return false
	}
	if slices.ContainsFunc(roles, isSuperuserRole) {
		return true
	}
	if !isKnownPermission(permission) {
		return false
	}

	e := currentEnforcer()
	if e == nil {
		return false
	}
	if effect, ok := explicitSubjectEffect(e, UserSubject(userID), permission); ok {
		return effect == EffectAllow
	}
	for _, role := range roles {
		if roleBaselineAllows(e, role, permission) {
			return true
		}
	}
	return false
}

// Capabilities returns the full resource/action matrix the subject is allowed.
func Capabilities(userID int, systemRole int) PermissionsMap {
	result := make(PermissionsMap, len(registry))
	for _, resource := range registry {
		actions := make(map[string]bool, len(resource.Actions))
		for _, action := range resource.Actions {
			actions[action.Action] = Can(userID, systemRole, Permission{
				Resource: resource.Resource,
				Action:   action.Action,
			})
		}
		result[resource.Resource] = actions
	}
	return result
}

func roleBaselineAllows(e *casbin.SyncedEnforcer, roleKey string, permission Permission) bool {
	effect, ok := explicitSubjectEffect(e, RoleSubject(roleKey), permission)
	return ok && effect == EffectAllow
}

func explicitSubjectEffect(e *casbin.SyncedEnforcer, subject string, permission Permission) (string, bool) {
	policies, err := e.GetFilteredPolicy(0, subject, permission.Resource, permission.Action)
	if err != nil {
		return "", false
	}
	hasAllow := false
	for _, policy := range policies {
		switch policyEffect(policy) {
		case EffectDeny:
			return EffectDeny, true
		case EffectAllow:
			hasAllow = true
		}
	}
	if hasAllow {
		return EffectAllow, true
	}
	return "", false
}

func policyEffect(policy []string) string {
	if len(policy) < 4 || policy[3] == "" {
		return EffectAllow
	}
	return policy[3]
}
