package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPerfMetricsQueriesScopeUserIDs(t *testing.T) {
	truncateTables(t)

	metrics := []PerfMetric{
		{UserId: 1, ModelName: "gpt-visible", Group: "default", BucketTs: 1000, RequestCount: 2, SuccessCount: 2},
		{UserId: 2, ModelName: "gpt-visible", Group: "default", BucketTs: 1000, RequestCount: 3, SuccessCount: 2},
		{UserId: 3, ModelName: "gpt-hidden", Group: "default", BucketTs: 1000, RequestCount: 5, SuccessCount: 5},
	}
	for i := range metrics {
		require.NoError(t, UpsertPerfMetric(&metrics[i]))
	}

	rows, err := GetPerfMetrics("gpt-visible", "", 900, 1100, []int{1, 2})
	require.NoError(t, err)
	require.Len(t, rows, 2)

	summaries, err := GetPerfMetricsSummaryAll(900, 1100, nil, []int{1, 2})
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, "gpt-visible", summaries[0].ModelName)
	assert.Equal(t, int64(5), summaries[0].RequestCount)
}
