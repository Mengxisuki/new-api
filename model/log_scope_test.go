package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInviterLogScopeIncludesAdminAndInvitedUsers(t *testing.T) {
	truncateTables(t)

	admin := User{
		Username: "log-admin",
		Password: "password",
		AffCode:  "log-admin-code",
		Role:     common.RoleAdminUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&admin).Error)

	invitedUser := User{
		Username:  "log-invited",
		Password:  "password",
		AffCode:   "log-invited-code",
		InviterId: admin.Id,
		Status:    common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&invitedUser).Error)

	otherUser := User{
		Username: "log-other",
		Password: "password",
		AffCode:  "log-other-code",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&otherUser).Error)

	now := time.Now().Unix()
	createdLogs := []Log{
		{UserId: admin.Id, Username: admin.Username, Type: LogTypeConsume, Quota: 10, PromptTokens: 3, CompletionTokens: 2, CreatedAt: now - 2},
		{UserId: invitedUser.Id, Username: invitedUser.Username, Type: LogTypeConsume, Quota: 20, PromptTokens: 5, CompletionTokens: 4, CreatedAt: now - 1},
		{UserId: otherUser.Id, Username: otherUser.Username, Type: LogTypeConsume, Quota: 30, PromptTokens: 7, CompletionTokens: 6, CreatedAt: now},
	}
	require.NoError(t, LOG_DB.Create(&createdLogs).Error)

	logs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 20, 0, "", "", "", &admin.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, logs, 2)
	assert.Equal(t, []int{invitedUser.Id, admin.Id}, []int{logs[0].UserId, logs[1].UserId})

	stat, err := SumUsedQuota(LogTypeUnknown, 0, 0, "", "", "", 0, "", &admin.Id)
	require.NoError(t, err)
	assert.Equal(t, 30, stat.Quota)
	assert.Equal(t, 2, stat.Rpm)
	assert.Equal(t, 14, stat.Tpm)
}
