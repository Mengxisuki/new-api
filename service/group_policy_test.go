package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectableGroupsDoNotExposeHiddenAccountGroup(t *testing.T) {
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	originalSpecialGroups := specialGroups.ReadAll()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		specialGroups.Clear()
		specialGroups.AddAll(originalSpecialGroups)
	})

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","premium":"Premium"}`))
	specialGroups.Clear()
	specialGroups.AddAll(map[string]map[string]string{
		"private": {
			"-:premium": "",
			"+:backup":  "Backup",
		},
	})

	selectable := GetUserSelectableGroups("private")
	assert.Equal(t, map[string]string{
		"default": "Default",
		"backup":  "Backup",
	}, selectable)
	assert.NotContains(t, selectable, "private")
	assert.NotContains(t, selectable, "premium")

	usable := GetUserUsableGroups("private")
	assert.Contains(t, usable, "private")
	assert.NotContains(t, usable, "premium")
}

func TestGetRequestGroupCandidatesPreservesTokenPriorityAndAppendsAutoGroups(t *testing.T) {
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
	})

	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["premium","default","backup"]`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","premium":"Premium","backup":"Backup"}`))

	testCases := []struct {
		name           string
		selected       []string
		autoGroup      bool
		setPolicy      bool
		expectedGroups []string
	}{
		{
			name:           "selected groups keep token order",
			selected:       []string{"default", "premium"},
			setPolicy:      true,
			expectedGroups: []string{"default", "premium"},
		},
		{
			name:           "auto fallback appends remaining global groups",
			selected:       []string{"default", "premium"},
			autoGroup:      true,
			setPolicy:      true,
			expectedGroups: []string{"default", "premium", "backup"},
		},
		{
			name:           "legacy auto token uses global order",
			expectedGroups: []string{"premium", "default", "backup"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(nil)
			if testCase.setPolicy {
				common.SetContextKey(ctx, constant.ContextKeyTokenGroups, testCase.selected)
				common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroup, testCase.autoGroup)
			}

			assert.Equal(t, testCase.expectedGroups, GetRequestGroupCandidates(ctx, "default"))
		})
	}
}
