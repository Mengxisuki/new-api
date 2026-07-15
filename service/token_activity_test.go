package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenActivityCountsConcurrentRequests(t *testing.T) {
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
	})

	finishFirst := BeginTokenActivity(42, 7, "token-activity-first")
	finishSecond := BeginTokenActivity(42, 7, "token-activity-second")
	t.Cleanup(finishFirst)
	t.Cleanup(finishSecond)

	counts := GetActiveTokenCounts(42)
	require.Contains(t, counts, 7)
	assert.Equal(t, 2, counts[7])

	finishFirst()
	assert.Equal(t, 1, GetActiveTokenCounts(42)[7])

	finishSecond()
	assert.NotContains(t, GetActiveTokenCounts(42), 7)
}
