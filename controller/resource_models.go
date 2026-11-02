package controller

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/hosting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// resourceModel 是配置导出的最小目录，不包含渠道和内部账号身份。
// Verified 只表示该模型曾在本人挂靠账号完成 Responses 测试，不承诺永久可用。
type resourceModel struct {
	ID                string   `json:"id"`                 // 使用真实目录ID，不生成可被误当模型的账号别名。
	Protocols         []string `json:"protocols"`          // 客户端按此选择接入协议，不从模型名称猜测。
	PlatformProtocols []string `json:"platform_protocols"` // 只描述当前平台渠道能力，避免混用同名挂靠协议。
	Verified          bool     `json:"verified"`           // 只记录已完成测试，不从授权成功推导。
}

// resourceModels 为浏览器导出与 API 模型列表提供同一个权限交集。
// 平台目录受价格/分组限制；免费挂靠目录不要求管理员配置平台价格。
// 目录表示能力与权限，不表示当前一定有足够额度完成生成。
// 因此余额暂时为零不会从客户端删掉用户已经配置的模型。
// policy=nil 是历史合同；非空但 sources=[] 是用户主动禁止全部来源。
// 二者必须保持区别，不能将空策略误当成允许所有平台模型。
// 同名模型通过模型 ID 去重，来源优先级仍由实际执行器决定。
// 一个账号测试通过不能替另一个账号证明身份或调用能力。
// 目录与调用统一要求测试通过且未暂停，verified 还限定到实测模型。
// 测试记录包含模型及协议，避免测过一个模型后给全部目录盖章。
// 用户暂停或删除账号会即时影响目录，不能继续使用旧页面缓存授权。
// 控制面故障返回错误而非空数组，区分不可观测与确定没有模型。
// 用户保存过但已删除的账号可以忽略，其余来源仍可列出。
// 平台模型继续要求站点定价，挂靠模型不需要平台为免费调用配置倍率。
// 价格查询不应成为用户能否使用原厂额度的隐含开关。
// 普通渠道目录与私人账号目录在最后统一应用 Key 的模型限制。
// 这一先合并再求交的顺序可以避免某一来源漏掉 Key 限制。
// 模型别名匹配复用现有 TokenModelLimitAllows，避免出现两套权限规则。
// 管理员固定渠道不会自动扩大控制台目录的模型权限。
// 排序固定为模型 ID 字典序，控制面返回顺序变化不影响客户端配置选择。
// 不缓存完整内部 Route；其中的受限 Key 只在当前服务端调用栈中存在。
// 返回 DTO 不携带 owner_ref、group_id、credential_id 或加密密钥。
// 此函数不发真实模型请求，不消耗用户原厂额度或平台余额。
// platform_protocols 单独保存平台能力，同名挂靠模型不能替平台扩充协议。
// 前端可据此导出未实测的平台配置，而 verified 仍只描述挂靠测试结果。
// 钱包和订阅共享同一渠道目录，只有启用其中之一才合并平台能力。
// 完全没有已知协议的任务模型仍可列出，但不会冒充可导出的文本客户端。
func resourceModels(c *gin.Context, policy *model.ResourcePolicySnapshot) ([]resourceModel, error) {
	items := map[string]resourceModel{}
	platform := policy == nil // 只有未配置策略的历史Key自动保留平台目录。
	if policy != nil {
		for _, source := range policy.Sources {
			if !source.Enabled { // 暂停某来源也撤销其新增模型可见性。
				continue
			}
			if source.Kind == "wallet" || source.Kind == "subscription" {
				platform = true // 两个付费来源共享平台能力目录，仍分别结算。
				continue
			}
			if source.Kind != "hosting" {
				return nil, errors.New("invalid_resource_source")
			}
			route, err := hosting.ResolveRoute(c.Request.Context(), c.GetInt("id"), source.AccountID)
			if errors.Is(err, gorm.ErrRecordNotFound) { // 已解绑账号不阻断其余合法来源。
				continue
			}
			if err != nil {
				return nil, err
			}
			if !model.HostingAccountCanRelay(route.State, route.Paused) { // 授权成功不能替代模型测试通过。
				continue
			}
			account, err := model.GetHostingAccount(c.GetInt("id"), source.AccountID)
			if err != nil {
				return nil, err
			}
			// 各账号同名模型合并，但验证状态仅来自该模型的实际测试记录。
			for _, name := range route.Models {
				previous := items[name] // 合并同名模型时保留已有真实测试记录。
				items[name] = resourceModel{ID: name, Protocols: []string{"responses", "chat_completions", "responses_compact"}, PlatformProtocols: []string{}, Verified: previous.Verified || (account.VerifiedAt > 0 && account.VerifiedModel == name && account.VerifiedProtocol == "openai-responses")}
			}
		}
	}
	if platform && service.ResourcePlatformAllowed(c) { // 排序不能绕过平台分组授权。
		groups, err := getModelListGroups(c)
		if err != nil {
			return nil, err
		}
		settings, err := model.GetUserSetting(c.GetInt("id"), false)
		if err != nil {
			return nil, err
		}
		platformModels, err := resourcePlatformModels(groups.ownerGroups)
		if err != nil {
			return nil, err
		}
		for name, protocols := range platformModels {
			if !operation_setting.SelfUseModeEnabled && !settings.AcceptUnsetRatioModel && !helper.HasModelBillingConfig(name) {
				continue
			}
			// 同名模型分别保留平台能力与挂靠验证，导出不能借用另一来源的协议。
			item := items[name]
			// 平台协议集合独立复制，合并总协议时不能把挂靠能力写回平台字段。
			item.ID, item.PlatformProtocols = name, slices.Clone(protocols)
			for _, protocol := range protocols {
				if !slices.Contains(item.Protocols, protocol) {
					item.Protocols = append(item.Protocols, protocol)
				}
			}
			if item.Protocols == nil {
				// JSON 空数组与未知对象保持区分，让客户端稳定处理原生任务模型。
				item.Protocols = []string{}
			}
			items[name] = item
		}
	}
	limits, _ := c.Get("token_model_limit") // 仅使用鉴权层解析后的限制集合。
	allowed, _ := limits.(map[string]bool)  // 坏类型按无权限处理，不能当作无限制。
	result := make([]resourceModel, 0, len(items))
	for name, item := range items {
		if c.GetBool("token_model_limit_enabled") && !middleware.TokenModelLimitAllows(allowed, name) {
			continue
		}
		result = append(result, item)
	}
	slices.SortFunc(result, func(a, b resourceModel) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return result, nil
}

// GetHostingTokenModels 以控制台会话查询本人指定 Key，不泄露完整 Key。
// 不覆盖原请求的会话身份；临时上下文仅用于复用 Key 分组和模型限制。
// 控制台身份来自 UserAuth，路径参数只接受正整数 Key ID。
// GetTokenByIds 同时限定 user_id，猜测 ID 不会返回他人的可用模型。
// 临时上下文继承用户组与语言，再应用该 Key 的分组和模型权限。
// 不使用查询者提交的 group、owner 或 source 参数重新构造权限。
// 此操作不要求展示 Key 明文，浏览器只用已有 Key ID 请求目录。
// 模型目录查看与实际 Key 使用是不同能力，过期 Key 仍可在管理页编辑。
// 真正的推理调用依旧由 TokenAuth 拒绝禁用、过期和 IP 不匹配的 Key。
// 读取策略失败必须返回错误，不能在故障时导出付费平台默认目录。
// 未配置策略时沿用原平台目录，避免破坏存量客户端配置导出。
// 已配置策略时只读取该 Key 的来源，不合并其他 Key 或历史全局来源。
// 返回采用控制台通用 success/data 信封，供前端统一处理错误。
// 前端可依据 verified 与协议信息限制 Codex 配置导出，但权限仍在后端。
func GetHostingTokenModels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("token_id")) // 不接受远端组或不透明账号引用代替KeyID。
	if err != nil || id <= 0 {
		hostingError(c, &hosting.Error{Status: 400, Code: "hosting_invalid_request"})
		return
	}
	token, err := model.GetTokenByIds(id, c.GetInt("id"))
	if err != nil {
		hostingError(c, err)
		return
	}
	user, err := model.GetUserCache(token.UserId)
	if err != nil {
		hostingError(c, err)
		return
	}
	view := c.Copy()        // 不把APIKey权限写回浏览器会话上下文。
	view.Writer = c.Writer  // 共用错误响应出口，但不继续执行原handler链。
	user.WriteContext(view) // 平台组与语言来自真实Key所有者。
	if err := middleware.SetupContextForToken(view, token); err != nil {
		hostingError(c, err)
		return
	}
	policy, err := model.GetEffectiveResourcePolicy(token.UserId, token.Id)
	if err != nil {
		hostingError(c, err)
		return
	}
	items, err := resourceModels(view, policy)
	if err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"models": items})
}

// listResourceModels 接管统一策略的列表与单模型查询，返回相同授权集合。
// 原历史目录仅在未配置策略时使用，避免从静态模型表泄露不可用模型。
// 布尔返回值表示已接管响应，调用方不能再运行历史 ListModels 分支。
// 三种模型接口格式都来自同一个授权集合，不允许单模型查询绕过列表限制。
// 来源目录共用权限集合，Anthropic 与 Gemini 列表继续保留各自协议信封。
// 静态模型元数据只补展示信息，不能决定某个模型是否对当前 Key 可见。
// 不可用模型单查返回 404，与上游静态字典是否包含该 ID 无关。
// 空列表返回正常 list 对象，表示用户没有启用可列出的来源。
// 控制面错误采用 502，不能返回空列表让客户端误删本地配置。
// 目录不泄露账号别名和脱敏邮箱，这些仅在本人账号管理页展示。
// 统一策略上下文由 TokenAuth 写入，任何 HTTP 请求头都不能覆盖它。
// 断言失败代表内部接线故障，必须停止，不能静默启用历史收费行为。
// 模型列表的接口格式和能力目录是两层合同：来源变化不改变原客户端格式。
// 单查同样先过滤权限，不能通过模型路径绕过列表中的本人分组限制。
func listResourceModels(c *gin.Context, modelType int) bool {
	value, exists := c.Get("oak.resource_policy")
	if !exists { // 缺少策略时才允许调用方继续历史目录。
		return false
	}
	policy, ok := value.(*model.ResourcePolicySnapshot)
	if !ok || policy == nil {
		resourceRelayError(c, "invalid_resource_policy", 500)
		return true
	}
	items, err := resourceModels(c, policy)
	if err != nil {
		resourceRelayError(c, "resource_models_unavailable", 502)
		return true
	}
	models := make([]dto.OpenAIModels, 0, len(items)) // 保持空数组而不是null，兼容客户端目录缓存。
	for _, item := range items {
		entry := buildOpenAIModel(item.ID, nil)
		// 静态元数据会读取全站端点缓存，必须用当前用户目录的真实协议覆盖。
		entry.SupportedEndpointTypes = make([]constant.EndpointType, 0, len(item.Protocols))
		for _, protocol := range resourceProtocols {
			if slices.Contains(item.Protocols, protocol.name) {
				entry.SupportedEndpointTypes = append(entry.SupportedEndpointTypes, protocol.endpoint)
			}
		}
		if requested := c.Param("model"); requested != "" {
			if requested == item.ID {
				// 单模型查询保持 Anthropic 格式，权限仍来自同一目录。
				if modelType == constant.ChannelTypeAnthropic {
					c.JSON(http.StatusOK, dto.AnthropicModel{ID: entry.Id, DisplayName: entry.Id, Type: "model", CreatedAt: time.Unix(int64(entry.Created), 0).UTC().Format(time.RFC3339)})
				} else {
					c.JSON(http.StatusOK, entry)
				}
				return true
			}
			continue
		}
		models = append(models, entry)
	}
	if c.Param("model") != "" {
		resourceRelayError(c, "model_not_found", http.StatusNotFound)
		return true
	}
	writeModelList(c, modelType, models)
	return true
}

// resourceProtocols 是配置导出与模型列表共用的平台协议映射。
// 请求路径由既有端点定义取得，避免目录与渠道路径过滤各维护一份规则。
// 名称供配置导出使用，实际转发仍由原路由和适配器选择。
// 一个模型可以有多个协议，不根据模型名称把所有协议归成 OpenAI。
var resourceProtocols = []struct {
	name     string
	endpoint constant.EndpointType
}{
	{"responses", constant.EndpointTypeOpenAIResponse},
	{"chat_completions", constant.EndpointTypeOpenAI},
	{"responses_compact", constant.EndpointTypeOpenAIResponseCompact},
	{"claude", constant.EndpointTypeAnthropic},
	{"gemini", constant.EndpointTypeGemini},
	{"images", constant.EndpointTypeImageGeneration},
	{"embeddings", constant.EndpointTypeEmbeddings},
	{"rerank", constant.EndpointTypeJinaRerank},
	{"video", constant.EndpointTypeOpenAIVideo},
	{"alpha_search", constant.EndpointTypeOpenAIAlphaSearch},
	{"tools_search", constant.EndpointTypeDeepSeekToolsSearch},
	{"tools_fetch", constant.EndpointTypeDeepSeekToolsFetch},
}

// resourcePlatformModels 只从当前用户可用分组收集真实启用渠道能力。
// 不读取全站模型端点并集，同名模型在其他分组的能力不能借给当前用户。
// 只查询非秘密路由字段，模型目录无需加载平台渠道密钥。
// 同名模型按可用协议求并集，真实请求仍按用户策略和渠道调度执行。
// 数据库失败必须可见，不能把读取故障伪装成没有可用模型。
// 分组集合已由用户及 Key 的既有分组规则求交，本函数不自行增加 auto 或默认组。
// 能力行启用和渠道状态启用必须同时成立，防止旧能力记录复活停用渠道。
// 同一渠道跨多个允许组出现时，协议合并去重而不增加模型执行权。
// 任务插件注册表只用于识别任务模型，不作为用户获得某个模型的授权依据。
// 模型是否已定价继续由上层检查；此处保留全部本人平台模型及已知能力。
// 专用图片与向量模型需要纠正通用 Chat 标签，避免导出错误的文本客户端配置。
// 高级自定义配置缺失时不能按通用 OpenAI 能力补齐，缺路由意味着没有可列协议。
// 命名兼容渠道的预设由 GetOtherSettings 统一取得，避免复制预设实现。
// 解析配置错误必须停止目录生成，不能一边写库修复一边声称能力正常。
// 请求路径过滤使用当前模型名，因此同渠道其他模型的路由不会被借用。
// 未映射的原生任务模型保留空协议数组；可见性不等于文本配置导出能力。
// 平台与免费挂靠能力在调用方合并，本函数不能修改挂靠验证记录。
// 未加载的插件仍可保留模型可见性，但没有声明就不声称具备某个宿主协议。
// 插件声明的 models 子集限制仍需生效，不能把一个模型的 Responses 能力给同插件全部模型。
// 绑定插件时优先使用该插件声明，避免从默认模型注册表借用其他供应商能力。
// 同名渠道的协议按并集合并，而渠道健康和实际预算仍由请求时的调度决定。
func resourcePlatformModels(groups []string) (map[string][]string, error) {
	result := map[string][]string{}
	if len(groups) == 0 {
		return result, nil
	}
	var abilities []struct {
		model.Channel
		ModelName string
	}
	// 使用 GORM 字段映射处理保留字 group，兼容三种数据库的引用方式。
	err := model.DB.Table("abilities").Select("channels.id, channels.type, channels.status, channels.setting, channels.settings, abilities.model AS model_name").Joins("JOIN channels ON channels.id = abilities.channel_id").Where(map[string]any{"abilities.group": groups, "abilities.enabled": true, "channels.status": common.ChannelStatusEnabled}).Scan(&abilities).Error
	if err != nil {
		return nil, err
	}
	generation := jsplugin.DefaultRegistry.Generation()
	for _, ability := range abilities {
		channel, name := &ability.Channel, ability.ModelName
		// 先读取显式插件绑定，平台协议与原生任务能力不能混淆。
		var setting dto.ChannelSettings
		// 原有 GetSetting 会在坏 JSON 时写库自修复；目录读取先严格解析，禁止此副作用。
		if channel.Setting != nil && *channel.Setting != "" {
			if err := common.UnmarshalJsonStr(*channel.Setting, &setting); err != nil {
				return nil, errors.New("invalid_platform_channel_settings")
			}
		}
		// 所有本人平台模型保留在目录；未知协议用空数组，不能假冒 Chat 能力。
		if _, exists := result[name]; !exists {
			result[name] = []string{}
		}
		plugin, task := generation.GetByModel(name)
		if setting.TaskPluginKey != "" {
			// 显式单插件绑定覆盖模型注册表的默认拥有者，不偷偷换成另一插件。
			plugin, task = generation.Get(setting.TaskPluginKey)
		}
		if channel.Type == constant.ChannelTypeTaskPlugin || setting.TaskPluginKey != "" || task {
			// 宿主协议来自已加载插件声明，不将插件原生任务接口猜成同步文本。
			if task {
				for _, claim := range plugin.Meta.Protocols {
					// 声明未覆盖当前模型时不能在其配置导出中暴露该协议。
					if len(claim.Models) > 0 && !slices.Contains(claim.Models, name) {
						continue
					}
					protocol := map[string]string{"openai_responses": "responses", "openai_image": "images", "openai_video": "video"}[claim.Name]
					if protocol != "" && !slices.Contains(result[name], protocol) {
						result[name] = append(result[name], protocol)
					}
				}
			}
			continue
		}
		endpoints := common.GetEndpointTypesByChannelType(channel.Type, name)
		// 通用渠道的默认 Chat 标签不能让专用模型在配置导出中冒充文本模型。
		lower := strings.ToLower(name)
		switch {
		case common.IsImageGenerationModel(name), channel.Type == constant.ChannelTypeVolcEngine && strings.Contains(lower, "seedream"):
			endpoints = []constant.EndpointType{constant.EndpointTypeImageGeneration}
		case strings.Contains(lower, "embed"), strings.HasPrefix(name, "m3e"), strings.Contains(name, "bge-"):
			endpoints = []constant.EndpointType{constant.EndpointTypeEmbeddings}
		case strings.Contains(lower, "rerank"), channel.Type == constant.ChannelTypeMokaAI:
			endpoints = []constant.EndpointType{constant.EndpointTypeJinaRerank}
		}

		if constant.IsAdvancedCustomChannel(channel.Type) {
			// 自定义与内置兼容渠道以实际配置路由为准，不使用全站能力缓存补齐。
			var settings dto.ChannelOtherSettings
			// 同样阻止坏路由配置触发渠道自修复，失败时不能用默认全协议兜底。
			if channel.OtherSettings != "" {
				if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
					return nil, errors.New("invalid_platform_channel_routes")
				}
			}
			config := channel.GetOtherSettings().AdvancedCustom
			if config == nil {
				// 缺路由配置保留模型但不伪造默认同步协议，真正请求会报告配置问题。
				continue
			}
			endpoints = config.SupportedEndpointTypesForModel(name)
		}
		for _, protocol := range resourceProtocols {
			if !slices.Contains(endpoints, protocol.endpoint) {
				continue
			}
			endpoint, ok := common.GetDefaultEndpointInfo(protocol.endpoint)
			if !ok {
				// Video 使用宿主固定路径；其他未知端点不能凭名字猜测上游 URL。
				if protocol.endpoint != constant.EndpointTypeOpenAIVideo {
					continue
				}
				endpoint.Path = "/v1/videos"
			}
			endpoint.Path = strings.ReplaceAll(endpoint.Path, "{model}", name)
			// Gemini 的模型路径必须先展开，路径过滤才能与实际执行一致。
			// 复用执行时的请求路径过滤，同一自定义渠道可以为不同模型配置不同协议。
			supported, _ := model.ChannelSatisfiesFilters(channel, name, []hostdto.ChannelFilter{{Kind: hostdto.FilterRequestPath, RequestPath: endpoint.Path}})
			if supported && !slices.Contains(result[name], protocol.name) {
				result[name] = append(result[name], protocol.name)
			}
		}
	}
	return result, nil
}
