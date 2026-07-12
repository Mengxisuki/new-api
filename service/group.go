package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// GetUserSelectableGroups returns groups that may be shown in user-facing
// selectors. It strictly follows the global selectable list and per-user-group
// visibility rules, so a user's account group is not implicitly exposed.
func GetUserSelectableGroups(userGroup string) map[string]string {
	groupsCopy := setting.GetUserUsableGroupsCopy()
	if userGroup != "" {
		specialSettings, b := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		if b {
			// 处理特殊可用分组
			for specialGroup, desc := range specialSettings {
				if strings.HasPrefix(specialGroup, "-:") {
					// 移除分组
					groupToRemove := strings.TrimPrefix(specialGroup, "-:")
					delete(groupsCopy, groupToRemove)
				} else if strings.HasPrefix(specialGroup, "+:") {
					// 添加分组
					groupToAdd := strings.TrimPrefix(specialGroup, "+:")
					groupsCopy[groupToAdd] = desc
				} else {
					// 直接添加分组
					groupsCopy[specialGroup] = desc
				}
			}
		}
	}
	return groupsCopy
}

// GetUserUsableGroups returns groups available to internal routing and auth.
// The account group remains usable even when it is hidden from selectors.
func GetUserUsableGroups(userGroup string) map[string]string {
	groups := GetUserSelectableGroups(userGroup)
	if userGroup != "" {
		if _, ok := groups[userGroup]; !ok {
			groups[userGroup] = "用户分组"
		}
	}
	return groups
}

func GroupInUserSelectableGroups(userGroup, groupName string) bool {
	_, ok := GetUserSelectableGroups(userGroup)[groupName]
	return ok
}

// GetUserAutoGroup 根据用户分组获取自动分组设置
func GetUserAutoGroup(userGroup string) []string {
	groups := GetUserUsableGroups(userGroup)
	autoGroups := make([]string, 0)
	for _, group := range setting.GetAutoGroups() {
		if _, ok := groups[group]; ok {
			autoGroups = append(autoGroups, group)
		}
	}
	return autoGroups
}

func GetRequestGroupCandidates(ctx *gin.Context, userGroup string) []string {
	value, configured := common.GetContextKey(ctx, constant.ContextKeyTokenGroups)
	selectedGroups, ok := value.([]string)
	if !configured || !ok {
		return GetUserAutoGroup(userGroup)
	}

	usableGroups := GetUserUsableGroups(userGroup)
	candidates := make([]string, 0, len(selectedGroups))
	seen := make(map[string]struct{}, len(selectedGroups))
	for _, group := range selectedGroups {
		if _, usable := usableGroups[group]; !usable {
			continue
		}
		if _, exists := seen[group]; exists {
			continue
		}
		seen[group] = struct{}{}
		candidates = append(candidates, group)
	}

	if !common.GetContextKeyBool(ctx, constant.ContextKeyTokenAutoGroup) {
		return candidates
	}
	for _, group := range GetUserAutoGroup(userGroup) {
		if _, exists := seen[group]; exists {
			continue
		}
		seen[group] = struct{}{}
		candidates = append(candidates, group)
	}
	return candidates
}

// GetUserGroupRatio 获取用户使用某个分组的倍率
// userGroup 用户分组
// group 需要获取倍率的分组
func GetUserGroupRatio(userGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}
