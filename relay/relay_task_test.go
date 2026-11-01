package relay

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	kittypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRelayChannelDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	previousType := common.MainDatabaseType()
	previousCache := common.MemoryCacheEnabled
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&model.Channel{}))
	model.DB = database
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
		common.MemoryCacheEnabled = previousCache
		require.NoError(t, sqlDB.Close())
	})
	return database
}

func TestRealtimeFetchAppliesChannelHeaderOverrides(t *testing.T) {
	database := setupRelayChannelDB(t)
	service.InitHttpClient()
	requests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		_, _ = w.Write([]byte(`{"name":"operations/task-1","done":false}`))
	}))
	t.Cleanup(server.Close)
	headerOverride := `{"X-API-Key":"{api_key}"}`
	channel := &model.Channel{
		Name: "realtime-header-override", Type: constant.ChannelTypeGemini,
		Key: "channel-key", BaseURL: &server.URL, HeaderOverride: &headerOverride,
	}
	require.NoError(t, database.Create(channel).Error)
	task := &model.Task{
		TaskID: "public-task", ChannelId: channel.Id,
		Status: model.TaskStatusInProgress, Progress: "50%",
		PrivateData: model.TaskPrivateData{
			Key: "saved-key", UpstreamTaskID: base64.RawURLEncoding.EncodeToString([]byte("operations/task-1")),
		},
	}

	tryRealtimeFetch(task, true)

	require.Len(t, requests, 1)
	request := <-requests
	assert.Equal(t, "/v1beta/operations/task-1", request.URL.Path)
	assert.Equal(t, "saved-key", request.Header.Get("X-API-Key"))
	assert.Equal(t, "saved-key", request.Header.Get("X-Goog-Api-Key"))
}

func TestRealtimeFetchRefundsTaskItFindsFailed(t *testing.T) {
	database := setupRelayChannelDB(t)
	require.NoError(t, database.AutoMigrate(&model.Task{}, &model.User{}, &model.Log{}))
	previousLogDB := model.LOG_DB
	previousFactory := service.GetTaskAdaptorFunc
	previousRedis := common.RedisEnabled
	model.LOG_DB = database
	common.RedisEnabled = false
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor {
		if adaptor := GetTaskAdaptor(platform); adaptor != nil {
			return adaptor
		}
		return nil
	}
	t.Cleanup(func() {
		model.LOG_DB = previousLogDB
		service.GetTaskAdaptorFunc = previousFactory
		common.RedisEnabled = previousRedis
	})
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"operations/task-1","done":true,"error":{"code":3,"message":"blocked by safety"}}`))
	}))
	t.Cleanup(server.Close)
	channel := &model.Channel{Name: "realtime-refund", Type: constant.ChannelTypeGemini, Key: "channel-key", BaseURL: &server.URL}
	require.NoError(t, database.Create(channel).Error)
	require.NoError(t, database.Create(&model.User{Id: 7, Username: "realtime-owner", Quota: 100}).Error)
	task := &model.Task{
		TaskID: "public-task", Platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeGemini)),
		UserId: 7, ChannelId: channel.Id, Quota: 1000,
		Status: model.TaskStatusInProgress, Progress: "50%",
		PrivateData: model.TaskPrivateData{
			Key: "channel-key", UpstreamTaskID: base64.RawURLEncoding.EncodeToString([]byte("operations/task-1")),
		},
	}
	require.NoError(t, database.Create(task).Error)

	tryRealtimeFetch(task, true)

	var stored model.Task
	require.NoError(t, database.First(&stored, task.ID).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), stored.Status)
	assert.Equal(t, "blocked by safety", stored.FailReason)
	assert.Zero(t, stored.Quota, "the failed task is refunded")
	var user model.User
	require.NoError(t, database.First(&user, 7).Error)
	assert.Equal(t, 1100, user.Quota)
}

func TestApplyChannelPinPreservesOriginTasksAndRetryMode(t *testing.T) {
	database := setupRelayChannelDB(t)
	channel := &model.Channel{Name: "origin-channel", Key: "sk-test", Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeDoubaoVideo}
	require.NoError(t, database.Create(channel).Error)
	originTask := &model.Task{
		TaskID: "task-lock", ChannelId: channel.Id, Action: "text_to_video", Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-task-lock"},
		Data:        []byte(`{"id":"upstream-task-lock"}`),
	}

	for _, tc := range []struct {
		name     string
		tokenPin bool
		apply    func(*gin.Context, *relaycommon.RelayInfo) *dto.TaskError
	}{
		{name: "origin affinity", apply: ApplyOriginTaskAffinity},
		{name: "same channel retry", apply: ApplyChannelPin},
		{name: "token pin suppresses channel lock", tokenPin: true, apply: ApplyChannelPin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			common.SetContextKey(c, constant.ContextKeyOriginTasks, []*model.Task{originTask})
			constraints := service.GetChannelConstraints(c)
			constraints.AddPin(dto.ChannelPin{ChannelId: channel.Id, Source: dto.PinSourceOriginTask, Rank: dto.PinRankOriginTask, RetryMode: dto.PinRetrySameChannel})
			if tc.tokenPin {
				constraints.AddPin(dto.ChannelPin{ChannelId: channel.Id, Source: dto.PinSourceToken, Rank: dto.PinRankToken, RetryMode: dto.PinRetrySingleAttempt})
			}
			info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			require.Nil(t, tc.apply(c, info))
			if tc.tokenPin {
				assert.Nil(t, info.LockedChannel)
			} else {
				locked, ok := info.LockedChannel.(*model.Channel)
				require.True(t, ok)
				require.NotNil(t, locked)
				assert.Equal(t, channel.Id, locked.Id)
			}
			require.Len(t, info.OriginTasks, 1)
			assert.Equal(t, "task-lock", info.OriginTasks[0].TaskID)
			assert.Equal(t, "upstream-task-lock", info.OriginTasks[0].UpstreamTaskID)
			assert.Equal(t, "text_to_video", info.OriginTasks[0].Action)
			assert.Equal(t, string(model.TaskStatusSuccess), info.OriginTasks[0].Status)
			assert.Equal(t, []byte(originTask.Data), info.OriginTasks[0].Data)
		})
	}
}

func TestTaskModel2DtoNormalizesLegacyAction(t *testing.T) {
	task := &model.Task{Action: "firstTailGenerate"}

	dtoTask := TaskModel2Dto(task)

	assert.Equal(t, constant.TaskActionFirstTailToVideo, dtoTask.Action)
	assert.Equal(t, "firstTailGenerate", task.Action)
}

const mappingOrderSubmitPlugin = `
export const meta = {apiVersion:1,key:"maporder",name:"Map Order",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) {
  return {url: ctx.baseUrl+"/submit", method:"POST", body:{upstreamModel: ctx.upstreamModel, model: ctx.model}, action:"text_to_video"};
}
export function parseSubmitResponse(){return {taskId:"1"};}
export function buildQueryRequest(){return {url:"https://provider.example"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
`

const mappingOrderRewritePlugin = `
export const meta = {apiVersion:1,key:"maporder-rw",name:"Map Order RW",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) {
  return {url: ctx.baseUrl+"/submit", method:"POST", body:{upstreamModel: ctx.upstreamModel}, rewriteModel:"rewritten"};
}
export function parseSubmitResponse(){return {taskId:"1"};}
export function buildQueryRequest(){return {url:"https://provider.example"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
`

func pinMappingOrderPlugin(t *testing.T, c *gin.Context, source string) {
	t.Helper()
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Plugin: plugin})
}

func newTaskSubmitContext(t *testing.T, originalModel, mapping string) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, originalModel)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://provider.example")
	if mapping != "" {
		c.Set("model_mapping", mapping)
	}
	c.Set("task_request", map[string]any{"prompt": "p"})
	return c, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
}

func TestRelayTaskSubmitMapsBeforeValidateWhenOriginSet(t *testing.T) {
	const mapping = `{"alias-model":"mid-model","mid-model":"declared-model"}`

	c, info := newTaskSubmitContext(t, "alias-model", mapping)
	pinMappingOrderPlugin(t, c, mappingOrderSubmitPlugin)
	info.OriginModelName = "alias-model"

	_, taskErr := RelayTaskSubmit(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "model_price_error", taskErr.Code)
	assert.Equal(t, "alias-model", info.OriginModelName)
	assert.Equal(t, "declared-model", info.UpstreamModelName)
	assert.True(t, info.IsModelMapped)
}

func TestRelayTaskSubmitDeclaredNameWithoutMappingIsUnchanged(t *testing.T) {
	c, info := newTaskSubmitContext(t, "declared-model", "")
	pinMappingOrderPlugin(t, c, mappingOrderSubmitPlugin)
	info.OriginModelName = "declared-model"

	_, taskErr := RelayTaskSubmit(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "model_price_error", taskErr.Code)
	assert.Equal(t, "declared-model", info.OriginModelName)
	assert.Equal(t, "declared-model", info.UpstreamModelName)
	assert.False(t, info.IsModelMapped)
}

func TestRelayTaskSubmitDoesNotApplyMappingTwice(t *testing.T) {
	c, info := newTaskSubmitContext(t, "alias-model", `{"alias-model":"declared-model"}`)
	pinMappingOrderPlugin(t, c, mappingOrderRewritePlugin)
	info.OriginModelName = "alias-model"

	_, taskErr := RelayTaskSubmit(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "model_price_error", taskErr.Code)
	assert.Equal(t, "rewritten", info.UpstreamModelName, "late mapping would overwrite rewriteModel with the chain tail")
	assert.Equal(t, "alias-model", info.OriginModelName)
}

func TestRelayTaskSubmitEmptyOriginKeepsLateMapping(t *testing.T) {
	plugin, err := pluginruntime.NewRegistry().Register(mappingOrderSubmitPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	synthesized := service.CoverTaskActionToModelName(constant.TaskPlatform(plugin.Meta.Key), "text_to_video")
	c, info := newTaskSubmitContext(t, "pre-validate-upstream",
		`{"pre-validate-upstream":"should-not-apply-early","`+synthesized+`":"legacy-tail"}`)
	c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Plugin: plugin})
	info.OriginModelName = ""

	_, taskErr := RelayTaskSubmit(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "model_price_error", taskErr.Code)
	assert.Equal(t, synthesized, info.OriginModelName)
	assert.Equal(t, "legacy-tail", info.UpstreamModelName)
	assert.True(t, info.IsModelMapped)
}

const billingFallbackPlugin = `
export const meta = {apiVersion:1,key:"bill-fallback",name:"Bill Fallback",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) {
  return {url: ctx.baseUrl+"/submit", method:"POST", body:{upstreamModel: ctx.upstreamModel, model: ctx.model}, action:"text_to_video"};
}
export function parseSubmitResponse(){return {taskId:"1"};}
export function buildQueryRequest(){return {url:"https://provider.example"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
`

func saveBillingConfig(t *testing.T) {
	t.Helper()
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
}

func TestRelayTaskSubmitAliasBillingIdentityAndExprFallback(t *testing.T) {
	const mapping = `{"alias-model":"declared-model"}`
	const aliasExpr = `tier("alias", 2)`
	const tailExpr = `tier("tail", 3)`
	const legacyExpr = `tier("legacy", u("old_units") * 2)`

	tests := []struct {
		name       string
		modes      map[string]string
		exprs      map[string]string
		wantTiered bool
		wantExpr   string
		source     string
	}{
		{
			name:       "alias own tiered wins",
			modes:      map[string]string{"alias-model": "tiered_expr", "declared-model": "tiered_expr"},
			exprs:      map[string]string{"alias-model": aliasExpr, "declared-model": tailExpr},
			wantTiered: true,
			wantExpr:   aliasExpr,
		},
		{
			name:       "fallback uses tail expr",
			modes:      map[string]string{"declared-model": "tiered_expr"},
			exprs:      map[string]string{"declared-model": tailExpr},
			wantTiered: true,
			wantExpr:   tailExpr,
		},
		{
			name:       "stored expression keeps running after schema narrows",
			modes:      map[string]string{"declared-model": "tiered_expr"},
			exprs:      map[string]string{"declared-model": legacyExpr},
			wantTiered: true,
			wantExpr:   legacyExpr,
			source: strings.Replace(billingFallbackPlugin, `fetchMode:"per_task"`, `fetchMode:"per_task", usageSchema:{old_units:{type:"number",unit:"count"}}, usageProfiles:[{models:["declared-model"],schema:{seconds:{type:"number",unit:"second"}}}]`, 1) + `
export function extractUsage(){return {old_units:2};}
`,
		},
		{
			name:       "neither tiered uses ordinary pricing",
			wantTiered: false,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			saveBillingConfig(t)
			if len(testCase.modes) > 0 {
				modeJSON, marshalErr := common.Marshal(testCase.modes)
				require.NoError(t, marshalErr)
				exprJSON, marshalErr := common.Marshal(testCase.exprs)
				require.NoError(t, marshalErr)
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
					"billing_setting.billing_mode": string(modeJSON),
					"billing_setting.billing_expr": string(exprJSON),
				}))
				if testCase.wantExpr == aliasExpr {
					require.Equal(t, billing_setting.BillingModeTieredExpr, billing_setting.GetBillingMode("alias-model"))
				} else {
					require.Equal(t, billing_setting.BillingModeRatio, billing_setting.GetBillingMode("alias-model"))
					require.Equal(t, billing_setting.BillingModeTieredExpr, billing_setting.GetBillingMode("declared-model"))
				}
			}

			c, info := newTaskSubmitContext(t, "alias-model", mapping)
			c.Set("group", "default")
			info.UserGroup = "default"
			info.UsingGroup = "default"
			source := testCase.source
			if source == "" {
				source = billingFallbackPlugin
			}
			pinMappingOrderPlugin(t, c, source)
			info.OriginModelName = "alias-model"

			_, taskErr := RelayTaskSubmit(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, "alias-model", info.OriginModelName)
			assert.Equal(t, "declared-model", info.UpstreamModelName)
			assert.True(t, info.IsModelMapped)

			task := model.InitTask(constant.TaskPlatform("bill-fallback"), info)
			assert.Equal(t, "alias-model", task.Properties.OriginModelName)
			assert.Equal(t, "declared-model", task.Properties.UpstreamModelName)

			if testCase.wantTiered {
				require.NotNil(t, info.TieredBillingSnapshot, "submission error: %+v", taskErr)
				assert.Equal(t, "alias-model", info.TieredBillingSnapshot.ModelName)
				assert.Equal(t, testCase.wantExpr, info.TieredBillingSnapshot.ExprString)
				assert.Equal(t, billingexpr.ExprHashString(testCase.wantExpr), info.TieredBillingSnapshot.ExprHash)
				assert.NotEqual(t, "model_price_error", taskErr.Code)
				if testCase.wantExpr == legacyExpr {
					assert.Equal(t, 4*common.QuotaPerUnit, info.TieredBillingSnapshot.EstimatedQuotaBeforeGroup)
				}
			} else {
				assert.Nil(t, info.TieredBillingSnapshot)
				assert.Equal(t, "model_price_error", taskErr.Code)
			}
		})
	}
}

func TestSharedTaskBillingExpressionSelectionAndFrozenSettlement(t *testing.T) {
	const baseExpr = `tier("base", u("seconds") * 2)`
	const alphaExpr = `tier("alpha", u("seconds") * 3)`
	const betaExpr = `tier("beta", u("credits") * 5)`
	const aliasExpr = `tier("alias", u("credits") * 7)`
	for _, tc := range []struct {
		name, plugin, model, mapping, modelExpr, mode, wantExpr string
		variants                                                map[string]string
		wantPriceError, profiled                                bool
	}{
		{name: "executing plugin override", plugin: "billing-beta", model: "declared-model", modelExpr: baseExpr, mode: "tiered_expr", variants: map[string]string{"billing-alpha::declared-model": alphaExpr, "billing-beta::declared-model": betaExpr}, wantExpr: betaExpr},
		{name: "override ignores model mode", plugin: "billing-beta", model: "declared-model", modelExpr: baseExpr, mode: "ratio", variants: map[string]string{"billing-beta::declared-model": betaExpr}, wantExpr: betaExpr},
		{name: "model expression fallback", plugin: "billing-alpha", model: "declared-model", modelExpr: baseExpr, mode: "tiered_expr", wantExpr: baseExpr},
		{name: "alias override precedes mapped override", plugin: "billing-beta", model: "alias-model", mapping: `{"alias-model":"declared-model"}`, modelExpr: baseExpr, mode: "tiered_expr", variants: map[string]string{"billing-beta::declared-model": betaExpr, "billing-beta::alias-model": aliasExpr}, wantExpr: aliasExpr},
		{name: "mapped override precedes model fallback", plugin: "billing-beta", model: "alias-model", mapping: `{"alias-model":"declared-model"}`, modelExpr: baseExpr, mode: "tiered_expr", variants: map[string]string{"billing-beta::declared-model": betaExpr}, wantExpr: betaExpr},
		{name: "unconfigured plugin cannot use another schema", plugin: "billing-beta", model: "declared-model", modelExpr: baseExpr, mode: "tiered_expr", wantPriceError: true},
		{name: "missing usage in skipped branch remains incompatible", plugin: "billing-beta", model: "declared-model", modelExpr: `true ? tier("free", 0) : tier("missing", u("seconds"))`, mode: "tiered_expr", wantPriceError: true},
		{name: "fixed pricing is still rejected", plugin: "billing-beta", model: "declared-model", variants: map[string]string{"billing-beta::declared-model": `tier("fixed", fixed(1))`}, wantPriceError: true},
		{name: "endpoint mapping keeps the declared profile", plugin: "billing-beta", model: "declared-model", mapping: `{"declared-model":"ep-endpoint"}`, modelExpr: baseExpr, mode: "tiered_expr", variants: map[string]string{"billing-beta::declared-model": betaExpr}, wantExpr: betaExpr, profiled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saveBillingConfig(t)
			registry := pluginruntime.NewRegistry()
			for _, spec := range []struct{ key, field, unit string }{{"billing-alpha", "seconds", "second"}, {"billing-beta", "credits", "credit"}} {
				source := strings.ReplaceAll(billingFallbackPlugin, "bill-fallback", spec.key)
				schema := `usageSchema:{` + spec.field + `:{type:"number",unit:"` + spec.unit + `"}}`
				if tc.profiled && spec.key == "billing-beta" {
					// The endpoint ID is undeclared, so only the declared model's profile carries this schema.
					schema = `usageSchema:{seconds:{type:"number",unit:"second"}},usageProfiles:[{models:["declared-model"],schema:{` + spec.field + `:{type:"number",unit:"` + spec.unit + `"}}}]`
				}
				source = strings.Replace(source, `fetchMode:"per_task"`, `fetchMode:"per_task",`+schema, 1)
				source += `export function extractUsage(){return {` + spec.field + `:2};}`
				_, err := registry.Register(source, pluginruntime.Options{})
				require.NoError(t, err)
			}
			variants := tc.variants
			if variants == nil {
				variants = map[string]string{}
			}
			rawVariants, err := common.Marshal(variants)
			require.NoError(t, err)
			modes, err := common.Marshal(map[string]string{"declared-model": tc.mode})
			require.NoError(t, err)
			expressions, err := common.Marshal(map[string]string{"declared-model": tc.modelExpr})
			require.NoError(t, err)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				billing_setting.PluginBillingExprOption: string(rawVariants), "billing_setting.billing_mode": string(modes), "billing_setting.billing_expr": string(expressions),
			}))
			c, info := newTaskSubmitContext(t, tc.model, tc.mapping)
			c.Set("group", "default")
			c.Set("task_plugin_key", tc.plugin)
			info.UserGroup = "default"
			info.UsingGroup = "default"
			info.OriginModelName = tc.model
			plugin, ok := registry.Generation().Get(tc.plugin)
			require.True(t, ok)
			c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Generation: registry.Generation(), Plugin: plugin})
			_, taskErr := RelayTaskSubmit(c, info)
			require.NotNil(t, taskErr) // This fixture stops at reservation, before upstream submission.
			if tc.wantPriceError {
				assert.Equal(t, "model_price_error", taskErr.Code)
				assert.Nil(t, info.TieredBillingSnapshot)
				return
			}
			require.NotNil(t, info.TieredBillingSnapshot, "submission error: %+v", taskErr)
			assert.Equal(t, tc.wantExpr, info.TieredBillingSnapshot.ExprString)
			assert.NotEqual(t, "model_price_error", taskErr.Code)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{billing_setting.PluginBillingExprOption: `{}`, "billing_setting.billing_expr": `{}`}))
			field := "seconds"
			if tc.plugin == "billing-beta" {
				field = "credits"
			}
			result, usage, err := service.EvaluateTaskCompletionUsage(info.TieredBillingSnapshot, map[string]any{field: float64(4)})
			require.NoError(t, err)
			assert.Equal(t, float64(4), usage[field])
			assert.Equal(t, float64(2), info.TieredBillingSnapshot.UsageFacts[field])
			assert.Equal(t, 2*info.TieredBillingSnapshot.EstimatedQuotaAfterGroup, result.ActualQuotaAfterGroup)
			assert.Equal(t, tc.wantExpr, info.TieredBillingSnapshot.ExprString)
		})
	}
}

func TestSharedEndpointBillingKeepsOrdinaryAndPluginPricesSeparate(t *testing.T) {
	require.NoError(t, i18n.Init())
	const taskExpr = `tier("images", u("image_count") * 0.05)`
	const ordinaryExpr = `tier("images", fixed(0.04)) * image_count`
	savedPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices)) })
	for _, tc := range []struct {
		name, expression, override          string
		ordinaryQuota, taskQuota, taskFinal int
		wantError                           string
		alias, shared, legacy               bool
	}{
		{name: "task usage price is rejected by ordinary relay", expression: taskExpr, taskQuota: 50000, taskFinal: 75000, wantError: "task usage expression"},
		{name: "enum fallback cannot silently bill zero", expression: `u("output_image_type") == "2k" ? tier("2k", u("image_count") * 0.08) : tier("1k", 0.04)`, taskQuota: 20000, taskFinal: 20000, wantError: "task usage expression"},
		{name: "unselected usage branch is rejected", expression: `true ? tier("flat", 0.04) : tier("images", u("image_count"))`, taskQuota: 20000, taskFinal: 20000, wantError: "task usage expression"},
		{name: "dynamic usage key is rejected", expression: `tier("images", u(u("unit")) * 0.05)`, taskQuota: 50000, taskFinal: 75000, wantError: "task usage expression"},
		{name: "plain task USD price is ambiguous", expression: `tier("flat", 0.04)`, taskQuota: 20000, taskFinal: 20000, wantError: "shares its billing expression"},
		{name: "alias retains task USD price", expression: `tier("flat", 0.04)`, taskQuota: 20000, taskFinal: 20000, wantError: "shares its billing expression", alias: true},
		{name: "per image relay and plugin overrides", expression: ordinaryExpr, override: taskExpr, ordinaryQuota: 40000, taskQuota: 50000, taskFinal: 75000},
		{name: "mapped plugin override", expression: ordinaryExpr, override: taskExpr, ordinaryQuota: 40000, taskQuota: 50000, taskFinal: 75000, alias: true},
		{name: "token relay and plugin overrides", expression: `tier("tokens", p * 2)`, override: taskExpr, ordinaryQuota: 1000, taskQuota: 50000, taskFinal: 75000},
		{name: "ordinary constant has ordinary units with plugin override", expression: `tier("flat", 80000)`, override: taskExpr, ordinaryQuota: 40000, taskQuota: 50000, taskFinal: 75000},
		{name: "another plugin still inherits task USD units", expression: `tier("flat", 0.04)`, override: taskExpr, taskQuota: 50000, taskFinal: 75000, wantError: "shares its billing expression", shared: true},
		{name: "legacy per image pricing stays shared", ordinaryQuota: 40000, taskQuota: 40000, legacy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saveBillingConfig(t)
			name, mapping, mappedModel := "declared-model", "", ""
			if tc.alias {
				name, mapping, mappedModel = "alias-model", `{"alias-model":"declared-model"}`, "declared-model"
			}
			mode := billing_setting.BillingModeTieredExpr
			if tc.legacy {
				mode = billing_setting.BillingModeRatio
			}
			variants := map[string]string{}
			if tc.override != "" {
				variants["bill-fallback::declared-model"] = tc.override
			}
			modes, err := common.Marshal(map[string]string{name: mode})
			require.NoError(t, err)
			expressions, err := common.Marshal(map[string]string{name: tc.expression})
			require.NoError(t, err)
			overrides, err := common.Marshal(variants)
			require.NoError(t, err)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"billing_setting.billing_mode": string(modes), "billing_setting.billing_expr": string(expressions),
				billing_setting.PluginBillingExprOption: string(overrides), "group_ratio_setting.group_ratio": `{"default":1}`,
			}))
			prices, err := common.Marshal(map[string]float64{name: 0.04})
			require.NoError(t, err)
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(prices)))
			registry := pluginruntime.NewRegistry()
			source := strings.Replace(billingFallbackPlugin, `fetchMode:"per_task"`, `fetchMode:"per_task",usageSchema:{image_count:{type:"number",unit:"count",description:"Image generation unit price"},output_image_type:{enum:["1k","2k"]},unit:{enum:["image_count"]}}`, 1)
			source += `export function extractUsage(){return {image_count:2,output_image_type:"1k",unit:"image_count"};}`
			plugin, err := registry.Register(source, pluginruntime.Options{})
			require.NoError(t, err)
			candidates := []pluginruntime.ProtocolBinding{{Plugin: plugin}}
			if tc.shared {
				other, err := registry.Register(strings.ReplaceAll(source, "bill-fallback", "other-images"), pluginruntime.Options{})
				require.NoError(t, err)
				candidates = append(candidates, pluginruntime.ProtocolBinding{Plugin: other})
			}
			ordinaryContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			ordinaryContext.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			ordinaryContext.Request.Header.Set("Accept-Language", "en")
			ordinaryContext.Set(pluginruntime.ContextKeyRelayEndpoint, pluginruntime.PinnedEndpoint{Generation: registry.Generation(), Model: name, MappedModel: mappedModel, Candidates: candidates})
			ordinary := &relaycommon.RelayInfo{
				OriginModelName: name, UserGroup: "default", UsingGroup: "default", RelayFormat: kittypes.RelayFormatOpenAIImage,
				Request: &kitdto.ImageRequest{Model: name, N: common.GetPointer(uint(2))}, BillingRequestInput: &billingexpr.RequestInput{},
			}
			ordinaryPrice, err := helper.ModelPriceHelper(ordinaryContext, ordinary, 1000, &kittypes.TokenCountMeta{})
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				assert.Nil(t, ordinary.TieredBillingSnapshot)
				assert.Nil(t, ordinary.Billing, "pricing must fail before reservation")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.ordinaryQuota, ordinaryPrice.QuotaToPreConsume)
			}

			pluginContext, task := newTaskSubmitContext(t, name, mapping)
			pluginContext.Set("group", "default")
			pluginContext.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Generation: registry.Generation(), Plugin: plugin})
			task.OriginModelName, task.UserGroup, task.UsingGroup = name, "default", "default"
			_, taskErr := RelayTaskSubmit(pluginContext, task)
			require.NotNil(t, taskErr) // This fixture stops at reservation, before upstream submission.
			assert.NotEqual(t, "model_price_error", taskErr.Code)
			assert.Equal(t, tc.taskQuota, task.PriceData.Quota)
			if tc.legacy {
				assert.Nil(t, task.TieredBillingSnapshot)
				assert.Nil(t, ordinary.TieredBillingSnapshot)
				return
			}
			require.NotNil(t, task.TieredBillingSnapshot)
			assert.True(t, task.TieredBillingSnapshot.TaskUsageBilling)
			// Updating live prices must not change either in-flight meter.
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"billing_setting.billing_expr": `{}`, billing_setting.PluginBillingExprOption: `{}`,
			}))
			settlement, _, err := service.EvaluateTaskCompletionUsage(task.TieredBillingSnapshot, map[string]any{"image_count": float64(3)})
			require.NoError(t, err)
			assert.Equal(t, tc.taskFinal, settlement.ActualQuotaAfterGroup)
			if tc.wantError != "" {
				return
			}
			require.NotNil(t, ordinary.TieredBillingSnapshot)
			assert.False(t, ordinary.TieredBillingSnapshot.TaskUsageBilling)
			settlement, err = billingexpr.ComputeTieredQuotaWithRequest(ordinary.TieredBillingSnapshot, billingexpr.TokenParams{P: 1000, Len: 1000}, *ordinary.BillingRequestInput)
			require.NoError(t, err)
			assert.Equal(t, tc.ordinaryQuota, settlement.ActualQuotaAfterGroup)
		})
	}
}

// Issue #7478: task APIs answer 201 Created or 202 Accepted on submission.
// Every 2xx must reach parseSubmitResponse; only other statuses are upstream
// failures that keep the upstream status and body.
func TestRelayTaskSubmitAcceptsAnySuccessfulUpstreamStatus(t *testing.T) {
	service.InitHttpClient()
	const source = `
export const meta = {apiVersion:1,key:"status-echo",name:"Status Echo",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl+"/submit", method:"POST", body:{model: ctx.model}, action:"text_to_video"}; }
export function parseSubmitResponse(ctx, response) { return {taskId: response.body.id, taskData: {status: response.statusCode}}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl+"/query"}; }
export function parseTaskResult() { return {status:"SUCCESS"}; }
`
	for _, tc := range []struct {
		status   int
		wantCode string
	}{
		{status: http.StatusOK},
		{status: http.StatusCreated},
		{status: http.StatusAccepted},
		{status: http.StatusBadRequest, wantCode: "fail_to_fetch_task"},
		{status: http.StatusBadGateway, wantCode: "fail_to_fetch_task"},
	} {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			saveBillingConfig(t)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"billing_setting.billing_mode": `{"declared-model":"tiered_expr"}`,
				"billing_setting.billing_expr": `{"declared-model":"tier(\"flat\", 3)"}`,
			}))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"id":"job-42","message":"upstream body"}`))
			}))
			defer server.Close()

			c, info := newTaskSubmitContext(t, "declared-model", "")
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
			c.Set("group", "default")
			info.UserGroup, info.UsingGroup = "default", "default"
			info.OriginModelName = "declared-model"
			// An existing reservation stands in for the wallet so the fixture reaches the upstream call.
			info.Billing = &imageReservation{limit: 1 << 30}
			pinMappingOrderPlugin(t, c, source)

			result, taskErr := RelayTaskSubmit(c, info)
			if tc.wantCode != "" {
				require.NotNil(t, taskErr)
				assert.Equal(t, tc.wantCode, taskErr.Code)
				assert.Equal(t, tc.status, taskErr.StatusCode)
				assert.Contains(t, taskErr.Message, "upstream body")
				assert.Nil(t, result)
				return
			}
			require.Nil(t, taskErr, "submission error: %+v", taskErr)
			require.NotNil(t, result)
			assert.Equal(t, "job-42", result.UpstreamTaskID)
			assert.JSONEq(t, `{"status":`+strconv.Itoa(tc.status)+`}`, string(result.TaskData))
		})
	}
}

// A retry prices the plugin it lands on from scratch and holds that price
// before the upstream sees the request. Plugin A fails upstream on the first
// attempt; the retry runs on plugin B, pinned the way
// SetupContextForSelectedChannel pins another candidate plugin's channel.
func TestRelayTaskSubmitRetryRepricesAndReservesBeforeUpstream(t *testing.T) {
	service.InitHttpClient()
	const source = `
export const meta = {apiVersion:1,key:"KEY",name:"KEY",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task",usageSchema:{seconds:{type:"number",unit:"second",description:"Video generation unit price"}}};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl+"/submit", method:"POST", body:{model: ctx.model}, action:"text_to_video"}; }
export function parseSubmitResponse(ctx, response) { return {taskId: response.body.id}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl+"/query"}; }
export function parseTaskResult() { return {status:"SUCCESS"}; }
export function extractUsage() { return {seconds:2}; }
`
	pricier := map[string]string{"retry-a::declared-model": `tier("a", u("seconds") * 0.05)`, "retry-b::declared-model": `tier("b", u("seconds") * 0.4)`}
	for _, tc := range []struct {
		name, perCallPrice, wantCode string
		expressions                  map[string]string
		wallet, wantQuota            int
		wantSubmitted                []string
	}{
		{name: "per-call plugin drops the previous expression", expressions: map[string]string{"retry-a::declared-model": `tier("a", u("seconds") * 0.02)`}, perCallPrice: `{"declared-model":0.04}`, wallet: 1 << 30, wantQuota: 40000, wantSubmitted: []string{"a", "b"}},
		{name: "pricier plugin is held before submission", expressions: pricier, perCallPrice: `{}`, wallet: 1 << 30, wantQuota: 400000, wantSubmitted: []string{"a", "b"}},
		{name: "unaffordable plugin is never submitted", expressions: pricier, perCallPrice: `{}`, wallet: 100000, wantCode: "insufficient_user_quota", wantSubmitted: []string{"a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saveBillingConfig(t)
			savedPrices := ratio_setting.ModelPrice2JSONString()
			t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices)) })
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(tc.perCallPrice))
			expressions, err := common.Marshal(tc.expressions)
			require.NoError(t, err)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				billing_setting.PluginBillingExprOption: string(expressions),
				"billing_setting.billing_mode":          `{}`,
				"billing_setting.billing_expr":          `{}`,
				"group_ratio_setting.group_ratio":       `{"default":1}`,
			}))
			registry := pluginruntime.NewRegistry()
			for _, key := range []string{"retry-a", "retry-b"} {
				_, err := registry.Register(strings.ReplaceAll(source, "KEY", key), pluginruntime.Options{})
				require.NoError(t, err)
			}
			// The fake stands in for the session the first attempt created; its
			// limit is the most the wallet can hold.
			billing := &imageReservation{limit: tc.wallet}
			var submitted []string
			var heldAtSubmit []int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
				submitted = append(submitted, attempt)
				heldAtSubmit = append(heldAtSubmit, billing.held)
				w.Header().Set("Content-Type", "application/json")
				if attempt == "a" {
					w.WriteHeader(http.StatusInternalServerError)
				}
				_, _ = w.Write([]byte(`{"id":"job-` + attempt + `"}`))
			}))
			defer server.Close()

			c, info := newTaskSubmitContext(t, "declared-model", "")
			c.Set("group", "default")
			info.UserGroup, info.UsingGroup, info.OriginModelName = "default", "default", "declared-model"
			info.Billing = billing
			var result *TaskSubmitResult
			var taskErr *dto.TaskError
			for _, attempt := range []string{"a", "b"} {
				plugin, ok := registry.Generation().Get("retry-" + attempt)
				require.True(t, ok)
				c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Generation: registry.Generation(), Plugin: plugin})
				c.Set("task_plugin_key", plugin.Meta.Key)
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL+"/"+attempt)
				result, taskErr = RelayTaskSubmit(c, info)
				if attempt == "a" {
					require.NotNil(t, taskErr)
					require.Equal(t, http.StatusInternalServerError, taskErr.StatusCode, "the first attempt fails upstream and is retried")
				}
			}

			require.Equal(t, tc.wantSubmitted, submitted)
			if tc.wantCode != "" {
				require.NotNil(t, taskErr)
				assert.Equal(t, tc.wantCode, taskErr.Code)
				assert.Equal(t, http.StatusForbidden, taskErr.StatusCode)
				return
			}
			require.Nil(t, taskErr, "submission error: %+v", taskErr)
			assert.Equal(t, tc.wantQuota, result.Quota)
			assert.Equal(t, tc.wantQuota, heldAtSubmit[1], "plugin B must not see the request before its price is held")
			if tc.perCallPrice != `{}` {
				assert.Nil(t, info.TieredBillingSnapshot, "a per-call plugin must not settle with the previous plugin's expression")
				return
			}
			require.NotNil(t, info.TieredBillingSnapshot)
			assert.Equal(t, tc.expressions["retry-b::declared-model"], info.TieredBillingSnapshot.ExprString)
		})
	}
}

// 两个真实 MJ 提交入口必须遵守同一 Key 资金顺序，不能只测试计费工厂。
// 外部边界仅用 HTTP 上游替代，价格、预扣、任务保存、退款都使用真实实现。
// 钱包为零仍可走订阅，禁止来源时则必须在发出 HTTP 前拒绝。
// 同时覆盖明确拒绝后的释放，防止发送前预扣成为永久扣款。
func TestMidjourneyResourceBillingEntries(t *testing.T) {
	for _, entry := range []string{"imagine", "swap_face"} { // 两个入口以前各有独立钱包检查。
		for _, scenario := range []string{"wallet", "subscription", "wallet_empty", "rejected", "failed_properties", "no_sources", "hosting_only", "free_forbidden", "insert_failure", "refund_failure", "new_period"} {
			t.Run(entry+"/"+scenario, func(t *testing.T) {
				db := setupRelayChannelDB(t) // 每例隔离数据库，失败不能污染下一种来源。
				require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Midjourney{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}, &model.Log{}))
				// 测试数据库与日志库同源；关闭异步批量以核对已提交的实际金额。
				oldLog, oldRedis, oldBatch := model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled
				oldPrices := ratio_setting.ModelPrice2JSONString() // 恢复全局价格，避免让其他协议测试免费。
				model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled = db, false, false
				t.Cleanup(func() {
					model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled = oldLog, oldRedis, oldBatch
					require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
				})
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":0.0002,"swap_face":0.0002}`))
				require.NoError(t, i18n.Init())
				wallet := 1000                  // 充足钱包用于证明订阅优先并非钱包不足的偶然结果。
				if scenario == "wallet_empty" { // 原钱包预检会错误拦截这个有效订阅请求。
					wallet = 0
				}
				require.NoError(t, db.Create(&model.User{Id: 7101, Username: "mj-policy", Quota: wallet, Status: common.UserStatusEnabled}).Error)
				require.NoError(t, db.Create(&model.Token{Id: 7101, UserId: 7101, Key: "mj-policy", Status: common.TokenStatusEnabled, RemainQuota: 1000}).Error)
				// 套餐真实关联到订阅；原周期标识与上游任务时间彼此独立。
				plan := &model.SubscriptionPlan{Title: "mj-policy", Enabled: true, TotalAmount: 1000, QuotaResetPeriod: "never"}
				require.NoError(t, db.Create(plan).Error)
				sub := &model.UserSubscription{Id: 7101, UserId: 7101, PlanId: plan.Id, AmountTotal: 1000, Status: "active", EndTime: time.Now().Add(time.Hour).Unix(), LastResetTime: 100}
				require.NoError(t, db.Create(sub).Error)
				calls := 0 // 禁用来源必须保证调用次数为零，而不只返回错误。
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					// 上游收到请求前，预扣必须已经实际持久化。
					var token model.Token
					if !assert.NoError(t, db.First(&token, 7101).Error) {
						http.Error(w, "database unavailable", 500)
						return
					}
					assert.Equal(t, 900, token.RemainQuota) // 上游执行前预算已经减少，不能事后才收费。
					if scenario == "rejected" {             // HTTP成功不代表MJ业务接受，必须识别响应code。
						_, _ = w.Write([]byte(`{"code":24,"description":"rejected"}`))
						return
					}
					if scenario == "failed_properties" { // 已声明失败的任务不能因code1被误认为可计费。
						_, _ = w.Write([]byte(`{"code":1,"result":"mj-policy-task","properties":{"status":"FAILURE"}}`))
						return
					}
					_, _ = w.Write([]byte(`{"code":1,"result":"mj-policy-task"}`))
				}))
				t.Cleanup(upstream.Close)
				service.InitHttpClient() // 走实际HTTP客户端，不替换提交函数或资金方法。
				channel := &model.Channel{Id: 7101, Name: "mj-policy", Key: "test", Status: common.ChannelStatusEnabled, BaseURL: &upstream.URL}
				require.NoError(t, db.Create(channel).Error)
				// 同一策略还可关闭所有平台来源，挂靠不能冒充 MJ 执行器。
				sources := []model.ResourceSource{{Kind: "wallet", Enabled: true}, {Kind: "subscription", Enabled: true}}
				if scenario != "wallet" && scenario != "wallet_empty" { // 同样的两个来源仅调整顺序。
					sources[0], sources[1] = sources[1], sources[0]
				}
				if scenario == "no_sources" { // 空策略是禁止全部，不是恢复旧钱包合同。
					sources = nil
				}
				if scenario == "hosting_only" || scenario == "free_forbidden" {
					sources = []model.ResourceSource{{Kind: "hosting", AccountID: 1, Enabled: true}, {Kind: "wallet", Enabled: false}}
				}
				if scenario == "free_forbidden" { // 零价只免资金，不能免平台访问授权。
					require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":0,"swap_face":0}`))
				}
				recorder := httptest.NewRecorder() // 观察真实handler响应写入而非手动构造返回值。
				c, _ := gin.CreateTestContext(recorder)
				body, path, modelName := `{"prompt":"test"}`, "/mj/submit/imagine", "mj_imagine"
				if entry == "swap_face" {
					body, path, modelName = `{"sourceBase64":"source","targetBase64":"target"}`, "/mj/insight-face/swap", "swap_face"
				}
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Set("base_url", upstream.URL) // 目标固定为本地测试上游，绝不访问生产。
				c.Set("channel_id", 7101)       // 统计和退款须关联真实执行渠道。
				c.Set("oak.resource_policy", &model.ResourcePolicySnapshot{Revision: "mj-test", Sources: sources})
				info := &relaycommon.RelayInfo{UserId: 7101, TokenId: 7101, TokenKey: "mj-policy", RequestId: "mj-policy-request", OriginModelName: modelName, UsingGroup: "default", UserGroup: "default", StartTime: time.Now(), RelayMode: relayconstant.RelayModeMidjourneyImagine}
				// 任务持久化失败必须释放预扣，不向客户端返回伪造的提交成功。
				if scenario == "insert_failure" { // 资金已预扣，但没有可交付任务时必须释放。
					require.NoError(t, db.Callback().Create().Before("gorm:create").Register("mj-insert-failure", func(tx *gorm.DB) {
						if tx.Statement.Table == "midjourneys" { // 只阻断任务保存，保留真实资金事务。
							tx.AddError(errors.New("task insert failed"))
						}
					}))
					t.Cleanup(func() { db.Callback().Create().Remove("mj-insert-failure") })
				}
				var apiErr *dto.MidjourneyResponse // 外部错误保持MJ现有响应格式。
				if entry == "swap_face" {
					apiErr = RelaySwapFace(c, info)
				} else {
					apiErr = RelayMidjourneySubmit(c, info)
				}
				forbidden := scenario == "no_sources" || scenario == "hosting_only" || scenario == "free_forbidden"
				if forbidden || scenario == "insert_failure" { // 权限和持久化错误必须让调用方看见。
					require.NotNil(t, apiErr)
				} else {
					require.Nil(t, apiErr)
				}
				if forbidden {
					require.Zero(t, calls) // 不得先访问上游再发现账号池不支持MJ。
				} else {
					require.Equal(t, 1, calls) // 任何业务结果都不能跨来源重复提交同一任务。
				}
				charged := !forbidden && scenario != "rejected" && scenario != "failed_properties" && scenario != "insert_failure"
				var user model.User // 钱包金额与订阅用量独立核对，不能相互替代。
				var token model.Token
				require.NoError(t, db.First(&user, 7101).Error)
				require.NoError(t, db.First(&token, 7101).Error)
				require.NoError(t, db.First(sub, 7101).Error)
				// 成功只扣一次，拒绝和保存失败恢复原来源及 Key 预算。
				if !charged { // 拒绝类场景不会进入异步退款阶段。
					assert.Equal(t, wallet, user.Quota)
					assert.EqualValues(t, 0, sub.AmountUsed)
					assert.Equal(t, 1000, token.RemainQuota) // 请求预算应恢复初值，不能因重放多退。
					return
				}
				expectedSource := "subscription" // 成功来源需同时反映在余额和持久任务中。
				if scenario == "wallet" {
					expectedSource = "wallet"
					assert.Equal(t, 900, user.Quota)
					assert.Zero(t, sub.AmountUsed)
				} else {
					assert.Equal(t, wallet, user.Quota)
					assert.EqualValues(t, 100, sub.AmountUsed) // 真正订阅账目必须保留预扣，供恢复再退。
				}
				assert.Equal(t, 900, token.RemainQuota) // 上游执行前预算已经减少，不能事后才收费。
				var task model.Midjourney
				require.NoError(t, db.Where("mj_id = ?", "mj-policy-task").First(&task).Error)
				require.Equal(t, expectedSource, task.BillingSource) // 后台不得依据用户以后修改的Key重新选择来源。
				require.Equal(t, 100, task.Quota)                    // 固定价格只记录一次实际扣费。
				stale := task                                        // 模拟另一轮已读到旧退款标记的轮询进程。
				if expectedSource == "subscription" {
					require.Equal(t, 7101, task.SubscriptionId)
					require.EqualValues(t, 100, task.SubscriptionResetTime) // 周期快照必须来自预扣时，不能用上游提交时间。
				}
				// 错误注入发生在真正的资金更新之后，整个事务仍必须回滚。
				if scenario == "refund_failure" { // 后半段失败也必须撤销已执行的资金返还。
					require.NoError(t, db.Callback().Update().Before("gorm:update").Register("mj-refund-failure", func(tx *gorm.DB) {
						if tx.Statement.Table == "tokens" { // 定位事务第二种资金约束，验证不是部分退款。
							tx.AddError(errors.New("token update failed"))
						}
					}))
					assert.False(t, service.RefundMidjourneyQuota(c, &task, "failed")) // 失败不得吞掉或伪报成功。
					db.Callback().Update().Remove("mj-refund-failure")
					require.NoError(t, db.First(sub, 7101).Error)
					assert.EqualValues(t, 100, sub.AmountUsed) // 真正订阅账目必须保留预扣，供恢复再退。
					require.NoError(t, db.First(&task, task.Id).Error)
					assert.Equal(t, 100, task.Quota) // 未完成退款必须保留持久恢复标记。
				}
				// 新周期已有其他消费时，旧任务退款只恢复 Key，不能释放新额度。
				if scenario == "new_period" {
					require.NoError(t, db.Model(sub).Updates(map[string]any{"last_reset_time": 200, "amount_used": 70}).Error)
				}
				require.True(t, service.RefundMidjourneyQuota(c, &task, "failed"))
				require.True(t, service.RefundMidjourneyQuota(c, &stale, "duplicate stale poll"))
				// 旧轮询快照保存也不能重新创建退款标记。
				stale.Quota = 100                  // 主动模拟过期金额重新进入普通保存路径。
				require.NoError(t, stale.Update()) // 进度保存不能重建已经退清的资金状态。
				require.NoError(t, db.First(&task, task.Id).Error)
				assert.Zero(t, task.Quota) // 数据库中的幂等标记最终只有一个终态。
				require.NoError(t, db.First(&user, 7101).Error)
				require.NoError(t, db.First(&token, 7101).Error)
				require.NoError(t, db.First(sub, 7101).Error)
				assert.Equal(t, wallet, user.Quota)
				assert.Equal(t, 1000, token.RemainQuota) // 请求预算应恢复初值，不能因重放多退。
				if scenario == "new_period" {
					assert.EqualValues(t, 70, sub.AmountUsed) // 另一周期的真实消费应完整保留。
				} else {
					assert.Zero(t, sub.AmountUsed)
				}
			})
		}
	}
}
