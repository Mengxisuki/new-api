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
