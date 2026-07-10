package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type flowQuotaResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Data    []model.FlowQuotaData `json:"data"`
}

type quotaDataResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Data    []model.QuotaData `json:"data"`
}

func setupFlowControllerTestDB(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.QuotaData{}))
	require.NoError(t, model.DB.Create([]model.User{
		{Id: 1, Username: "alice", Password: "password", AffCode: "alice-code", Role: common.RoleAdminUser, Status: common.UserStatusEnabled},
		{Id: 2, Username: "bob", Password: "password", AffCode: "bob-code", InviterId: 1, Status: common.UserStatusEnabled},
		{Id: 3, Username: "carol", Password: "password", AffCode: "carol-code", Status: common.UserStatusEnabled},
	}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 22, UserId: 2, Key: "sk-backup", Name: "backup"}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "default",
		ChannelID: 1,
		ModelName: "gpt-a",
		CreatedAt: 1100,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    2,
		Username:  "bob",
		NodeName:  "node-b",
		TokenID:   22,
		UseGroup:  "vip",
		ChannelID: 1,
		ModelName: "gpt-b",
		CreatedAt: 1200,
		Count:     1,
		Quota:     70,
		TokenUsed: 30,
	}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    3,
		Username:  "carol",
		NodeName:  "node-c",
		UseGroup:  "default",
		ChannelID: 1,
		ModelName: "gpt-c",
		CreatedAt: 1300,
		Count:     4,
		Quota:     90,
		TokenUsed: 50,
	}).Error)
}

func decodeFlowQuotaResponse(t *testing.T, recorder *httptest.ResponseRecorder) flowQuotaResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	return payload
}

func TestGetAllFlowQuotaDatesUsesAdminDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=bob", nil)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "bob", payload.Data[0].Username)
	require.Equal(t, "vip", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
	require.Empty(t, payload.Data[0].TokenName)
	require.Empty(t, payload.Data[0].NodeName)
}

func TestAdminQuotaDataEndpointsExcludeUninvitedUsers(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/?start_timestamp=1000&end_timestamp=2000", nil)

	GetAllQuotaDates(ctx)

	var quotaPayload quotaDataResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &quotaPayload))
	require.True(t, quotaPayload.Success, quotaPayload.Message)
	require.Len(t, quotaPayload.Data, 2)
	modelNames := map[string]struct{}{}
	for _, row := range quotaPayload.Data {
		modelNames[row.ModelName] = struct{}{}
	}
	require.Contains(t, modelNames, "gpt-a")
	require.Contains(t, modelNames, "gpt-b")
	require.NotContains(t, modelNames, "gpt-c")

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/?start_timestamp=1000&end_timestamp=2000&username=carol", nil)

	GetAllQuotaDates(ctx)

	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &quotaPayload))
	require.True(t, quotaPayload.Success, quotaPayload.Message)
	require.Empty(t, quotaPayload.Data)

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/users?start_timestamp=1000&end_timestamp=2000", nil)

	GetQuotaDatesByUser(ctx)

	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &quotaPayload))
	require.True(t, quotaPayload.Success, quotaPayload.Message)
	require.Len(t, quotaPayload.Data, 2)
	usernames := map[string]struct{}{}
	for _, row := range quotaPayload.Data {
		usernames[row.Username] = struct{}{}
	}
	require.Contains(t, usernames, "alice")
	require.Contains(t, usernames, "bob")
	require.NotContains(t, usernames, "carol")

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000", nil)

	GetAllFlowQuotaDates(ctx)

	flowPayload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, flowPayload.Data, 2)
	flowUsernames := map[string]struct{}{}
	for _, row := range flowPayload.Data {
		flowUsernames[row.Username] = struct{}{}
	}
	require.Contains(t, flowUsernames, "alice")
	require.Contains(t, flowUsernames, "bob")
	require.NotContains(t, flowUsernames, "carol")
}

func TestGetAllFlowQuotaDatesUsesRootDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=alice", nil)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "alice", payload.Data[0].Username)
	require.Equal(t, "node-a", payload.Data[0].NodeName)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "default", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
}

func TestGetUserFlowQuotaDatesRestrictsToAuthenticatedUser(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=1000&end_timestamp=2000", nil)

	GetUserFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Empty(t, payload.Data[0].Username)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "default", payload.Data[0].UseGroup)
	require.Empty(t, payload.Data[0].ChannelName)
}

func TestGetUserFlowQuotaDatesRejectsInvalidTimeRange(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=bad&end_timestamp=2000", nil)

	GetUserFlowQuotaDates(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.False(t, payload.Success)
	require.Equal(t, "invalid start_timestamp", payload.Message)
}
