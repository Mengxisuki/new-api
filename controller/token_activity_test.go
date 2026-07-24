package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tokenActivityResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Tokens []model.TokenActivityItem `json:"tokens"`
	} `json:"data"`
}

type tokenUsageResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Keys   []model.TokenUsageKey   `json:"keys"`
		Points []model.TokenUsagePoint `json:"points"`
	} `json:"data"`
}

func TestGetUserTokenActivityPrioritizesWorkingAndScopesToOwner(t *testing.T) {
	setupFlowControllerTestDB(t)
	now := time.Now().Unix()

	for id := 30; id < 37; id++ {
		require.NoError(t, model.DB.Create(&model.Token{
			Id:           id,
			UserId:       1,
			Key:          "activity-key-" + strconv.Itoa(id),
			Name:         "key",
			Status:       common.TokenStatusEnabled,
			AccessedTime: int64(id),
			CreatedTime:  int64(id),
		}).Error)
		require.NoError(t, model.DB.Create(&model.QuotaData{
			UserID:    1,
			TokenID:   id,
			ModelName: "gpt-test",
			CreatedAt: now,
			Count:     id - 20,
		}).Error)
	}

	finish := service.BeginTokenActivity(1, 30, "activity-test-request")
	t.Cleanup(finish)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/key-activity/self", nil)
	GetUserTokenActivity(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	payload := tokenActivityResponse{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data.Tokens, model.TokenActivityOfficeLimit)
	assert.Equal(t, 30, payload.Data.Tokens[0].Id)
	assert.Equal(t, "working", payload.Data.Tokens[0].ResolvedActivity)
	assert.Equal(t, 1, payload.Data.Tokens[0].ActiveRequests)
	for _, item := range payload.Data.Tokens {
		assert.NotEqual(t, 22, item.Id)
	}
}

func TestGetUserTokenUsageAggregatesMetricsAndScopesToOwner(t *testing.T) {
	setupFlowControllerTestDB(t)
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}))
	startTimestamp := int64(1_700_000_000)
	logs := []model.Log{
		{
			UserId: 1, TokenId: 11, TokenName: "primary", CreatedAt: startTimestamp + 10,
			Type: model.LogTypeConsume, Quota: 120, PromptTokens: 80, CompletionTokens: 20, UseTime: 4,
			Other: `{"cache_tokens":30,"cache_creation_tokens_5m":12,"frt":250.8}`,
		},
		{
			UserId: 1, TokenId: 11, TokenName: "primary", CreatedAt: startTimestamp + 20,
			Type: model.LogTypeError, Other: `{}`,
		},
		{
			UserId: 2, TokenId: 22, TokenName: "backup", CreatedAt: startTimestamp + 30,
			Type: model.LogTypeConsume, Quota: 999, PromptTokens: 999, CompletionTokens: 999,
		},
	}
	require.NoError(t, model.LOG_DB.Create(&logs).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet,
		"/api/data/key-usage/self?start_timestamp=1700000000&end_timestamp=1700003600", nil)
	GetUserTokenUsage(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	payload := tokenUsageResponse{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data.Points, 1)
	point := payload.Data.Points[0]
	assert.EqualValues(t, 1, point.SuccessfulRequests)
	assert.EqualValues(t, 1, point.FailedRequests)
	assert.EqualValues(t, 100, point.PromptTokens+point.CompletionTokens)
	assert.EqualValues(t, 120, point.Quota)
	assert.EqualValues(t, 30, point.CacheReadTokens)
	assert.EqualValues(t, 12, point.CacheCreateTokens)
	assert.EqualValues(t, 250, point.LatencyMsSum)
	assert.EqualValues(t, 1, point.LatencySamples)
}

func TestGetUserTokenUsageRejectsOversizedRange(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet,
		"/api/data/key-usage/self?start_timestamp=1&end_timestamp=99999999", nil)

	GetUserTokenUsage(ctx)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}
