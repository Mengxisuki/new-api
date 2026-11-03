package middleware

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

// 本模块处理 Key 的平台模型选择；渠道层仍负责供应商命名差异。
// 独立上下文键避免与渠道的 model_mapping 配置相互覆盖。
// 调用链约束：
//   - 身份认证完成后才能读取配置，客户端不能注入另一个 Key 的规则。
//   - 来源策略只能看到最终目标，因此免费账号与付费渠道选择保持一致。
//   - 价格查询沿用目标模型的既有定价，不在此计算或覆盖费用。
//   - 未开启映射时不解析正文，保持旧 Key 的协议兼容性与读取成本。
//   - 映射不修改 Authorization、Cookie、账号归属或资源回退授权。
//   - 上游返回的 model 继续按现有响应观测记录，不改写为源别名。
//   - 日志中的源名来自已解析请求，不能由用户伪造追踪字段覆盖。
//   - 所有规则失败都发生在上游执行之前，不产生需要退款的预扣。
//
// 协议边界：
//   - 普通 JSON 只替换顶层 model，嵌套的供应商字段完全保留。
//   - 表单的非模型字段可能重复；重复模型字段则不允许。
//   - 上传文件按原字节转发，不在映射层验证图片或音频内容。
//   - Gemini 映射仅替换模型段，流式动作和 query 参数保持原样。
//   - Responses WebSocket 先归一化每个事件，再经过此中间件。
//   - 历史任务查询不执行映射，异步结算读取提交时冻结的属性。
//   - 原生插件只在声明顶层模型合同的提交路由接入此入口。
//   - 专有的嵌套模型、动作推导模型仍由对应协议解释，不猜测改写。
//
// 正文缓存约束：
//   - 读取与重试共享 BodyStorage；直接替换 Request.Body 不能满足重放。
//   - 只改解析缓存而不改原始正文会让挂靠转发发送错误模型。
//   - 重建成功前不得释放旧存储，资源回收交给统一请求清理器。
//   - 请求级状态不写入全局对象，避免并行 Key 请求互相影响。
//
// TokenModelMapping 在限流、来源选择、插件认领和渠道选择之前改写模型。
// 认证中间件提供当前 Key 配置，后续计费自然读取改写后的平台模型。
// 查询任务及固定工具接口没有可替换的模型，不应重新解释为新提交。
// 各路由在 TokenAuth 之后注册，配置不来自请求头或客户端正文。
// 返回错误时终止整个链，不能失败后继续用原模型悄悄收费。
func TokenModelMapping() gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := applyTokenModelMapping(c); err != nil {
			abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
			return
		}
		c.Next()
	}
}

// applyTokenModelMapping 只处理协议显式提供的模型，不猜测省略模型的默认值。
// 无配置或未命中时不改变请求；同一请求已经完成映射则不重复执行链。
// 源名独立记录，OriginalModel 继续由现有分发器设置为计费目标。
func applyTokenModelMapping(c *gin.Context) error {
	// 配置字符串来自认证缓存；更新 Key 时由既有缓存栅栏使旧值失效。
	// 不另建映射缓存，以免不同进程或 WebSocket 事件读到过期规则。
	raw := common.GetContextKeyString(c, constant.ContextKeyTokenModelMapping)
	if raw == "" || common.GetContextKeyString(c, constant.ContextKeyTokenTargetModel) != "" {
		return nil
	}
	// 固定工具的 model 是价格身份，不是供应商可选参数。
	// remix 的模型来自原任务，改写正文不能更换已提交任务的模型。
	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/v1/tools/") || strings.Contains(path, "/mj/") || strings.HasSuffix(path, "/remix") {
		return nil
	}
	// Realtime 握手是唯一通过 GET 提交模型的入口。
	// GET 查询任务和模型目录继续使用原路由，不能因配置改变历史资源定位。
	if c.Request.Method != http.MethodPost && path != "/v1/realtime" {
		return nil
	}
	// 请求时重验使历史坏配置和数据库直写可见失败，不用空映射兜底。
	// 链解析只改变平台模型身份，不跨协议转换消息正文。
	mapping, err := common.ParseTokenModelMapping(raw)
	if err != nil {
		return err
	}
	if len(mapping) == 0 {
		return nil
	}

	// 路径与查询参数模型不依赖正文；Gemini 的动作后缀须原样保留。
	// 其他协议在同一份 BodyStorage 中读取，避免分发和上游看到不同名称。
	var source string
	var body []byte
	var mediaType string
	var params map[string]string
	switch {
	case path == "/v1/realtime":
		// 查询参数可能重复；必须在取单值前拒绝，避免代理与 Gin 取值不同。
		// 保留其他参数，包括 Realtime 客户端自己的协议配置。
		values := c.Request.URL.Query()["model"]
		if len(values) > 1 {
			return errors.New("model must be provided once")
		}
		// 避免提前初始化 Gin 的 queryCache，后续 c.Query 需要读到改写后的 URL。
		source = c.Request.URL.Query().Get("model")
	case strings.HasPrefix(path, "/v1beta/models/") || strings.HasPrefix(path, "/v1/models/"):
		// 共用分发器的路径提取规则，不把 generateContent 当作模型后缀。
		// 若协议未给出模型，原处理器继续负责生成相应错误。
		source = extractModelNameFromGeminiPath(path)
	default:
		// Content-Type 参数可能包含引号，因此不能手写字符串分割 boundary。
		// 未识别协议不尝试把二进制文件解释成 JSON。
		mediaType, params, err = mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil {
			return nil
		} // 其他协议由原处理器验证 Content-Type。
		if mediaType != gin.MIMEJSON && mediaType != gin.MIMEPOSTForm && mediaType != gin.MIMEMultipartPOSTForm {
			return nil
		}
		// 沿用统一请求大小限制和磁盘溢写，不能直接无界读取 Request.Body。
		// 存储读取失败会中止请求，不能改用已经被消费的原流。
		storage, readErr := common.GetBodyStorage(c)
		if readErr != nil {
			return readErr
		}
		body, err = storage.Bytes()
		if err != nil {
			return err
		}
		// 读取 storage 后重置游标，未命中也须保留后续处理器的完整正文。
		if _, err = storage.Seek(0, io.SeekStart); err != nil {
			return err
		}
		c.Request.Body = io.NopCloser(storage)
		switch mediaType {
		case gin.MIMEPOSTForm:
			// ParseQuery 会验证百分号编码；坏表单不能以部分解析结果路由。
			// URL 编码解码后再做名称比较，避免编码形式影响规则命中。
			values, parseErr := url.ParseQuery(string(body))
			if parseErr != nil {
				return errors.New("invalid form body")
			}
			if len(values["model"]) > 1 {
				return errors.New("model must be provided once")
			}
			source = values.Get("model")
		case gin.MIMEMultipartPOSTForm:
			// 直接读分段，不创建 MultipartForm 临时文件，也不改变文件字段顺序。
			// 重复字段或把文件命名为 model 都拒绝，防止两套解码器解释不一致。
			// 同一 boundary 用于读写，文件的内容和文件名不是模型候选。
			// 分段读取只用于寻找显式 model，其他字段由协议适配器验证。
			reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
			found := false
			for {
				part, partErr := reader.NextPart()
				if partErr == io.EOF {
					break
				}
				if partErr != nil {
					return errors.New("invalid multipart body")
				}
				if part.FormName() == "model" {
					if found || part.FileName() != "" {
						return errors.New("model must be provided once as text")
					}
					found = true
					// 多读一个字节区分正好上限与超限，超长名称不得被截断后命中。
					// 这里只对模型小字段设限，图片和音频仍按总请求上限处理。
					value, readErr := io.ReadAll(io.LimitReader(part, 257))
					if readErr != nil || len(value) > 256 {
						return errors.New("invalid model name")
					}
					source = string(value)
				}
				// Close 排空当前分段，使下一字段在正确边界开始读取。
				// 读取失败不掩盖为“未命中”，否则客户端可能按另一价格执行。
				if err = part.Close(); err != nil {
					return err
				}
			}
		default:
			// 复用已有 JSON 校验，保留重复键拒绝与显式类型约束。
			request, parseErr := getModelFromJSONBody(c)
			if parseErr != nil {
				return parseErr
			}
			source = request.Model
		}
	}
	// 省略模型保留现有协议默认值，不给默认值额外添加隐式映射。
	// 明确自映射也保持原正文，避免无意义重新编码上传文件。
	if source == "" {
		return nil
	}
	target, err := mapping.Resolve(source)
	if err != nil || target == source {
		return err
	}
	// 权限按最终目标检查，即使管理员固定渠道也不能借别名绕过 Key 限制。
	// 分组、渠道可用性和来源额度继续交给原调度器，不在这里扩大授权。
	if c.GetBool("token_model_limit_enabled") {
		// 模型限制沿用平台既有的标准化匹配；空限制集合按禁止处理。
		// 源别名不需要出现在平台目录，但最终目标必须属于该 Key 的权限。
		limits, _ := c.Get("token_model_limit")
		allowed, _ := limits.(map[string]bool)
		if !TokenModelLimitAllows(allowed, target) {
			abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorTokenModelForbidden, map[string]any{"Model": target}))
			return nil
		}
	}
	switch {
	case path == "/v1/realtime":
		// 用 URL 编码器替换单一模型值，保留其余查询字段及正确转义。
		// 后续分发与上游连接均从同一请求 URL 取得目标名称。
		query := c.Request.URL.Query()
		query.Set("model", target)
		c.Request.URL.RawQuery = query.Encode()
	case strings.HasPrefix(path, "/v1beta/models/") || strings.HasPrefix(path, "/v1/models/"):
		// 路径模型不能改变动作或层级；常规模型中的斜杠仍可用于 JSON 协议。
		if strings.ContainsAny(target, "/:") {
			return errors.New("invalid Gemini target model")
		}
		c.Request.URL.Path = strings.Replace(path, "/models/"+source+":", "/models/"+target+":", 1)
		// 清除旧编码路径，防止 URL.String 在 Path 与 RawPath 间选中旧模型。
		// Gin 的路径参数同步更新，兼容按参数读取而非按 URL 读取的处理器。
		c.Request.URL.RawPath = ""
		for i := range c.Params {
			if c.Params[i].Key == "path" {
				c.Params[i].Value = strings.Replace(c.Params[i].Value, source+":", target+":", 1)
			}
		}
	default:
		switch mediaType {
		case gin.MIMEPOSTForm:
			// 原表单已验证成功，Set 只更换模型，不覆盖 prompt 等业务参数。
			// Encode 可能调整字段顺序，但保留重复的非模型字段值。
			values, _ := url.ParseQuery(string(body))
			values.Set("model", target)
			body = []byte(values.Encode())
		case gin.MIMEMultipartPOSTForm:
			// 重建保持原 boundary、字段头和二进制内容；只有模型字段被替换。
			// 原正文已有请求大小上限，输出仅增加两个模型名之间的长度差。
			var output bytes.Buffer
			writer := multipart.NewWriter(&output)
			if err = writer.SetBoundary(params["boundary"]); err != nil {
				return err
			}
			// 同一 boundary 用于读写，文件的内容和文件名不是模型候选。
			// 分段读取只用于寻找显式 model，其他字段由协议适配器验证。
			reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
			for {
				part, partErr := reader.NextPart()
				if partErr == io.EOF {
					break
				}
				if partErr != nil {
					return partErr
				}
				// 复制分段头而非重造 FormFile，保留文件 MIME 类型和重复文件列表。
				// 对模型分段只写目标文本，不复制原值，防止出现拼接的模型名称。
				dest, writeErr := writer.CreatePart(part.Header)
				if writeErr != nil {
					return writeErr
				}
				if part.FormName() == "model" {
					_, err = io.WriteString(dest, target)
				} else {
					_, err = io.Copy(dest, part)
				}
				if err != nil {
					return err
				}
			}
			// 必须写出最后的结束边界，适配器和上游才能完整解析文件。
			// 完成前不替换原存储，构造失败不会留下部分可读正文。
			if err = writer.Close(); err != nil {
				return err
			}
			body = output.Bytes()
		default:
			// sjson 只替换 model，不重新编码数值、消息或供应商扩展参数。
			body, err = sjson.SetBytes(body, "model", target)
			if err != nil {
				return err
			}
		}
		// 所有正文协议通过同一替换点提交，避免 JSON 和表单缓存行为不同。
		// 失败仍未设置映射日志字段，只有成功提交后才能记录目标。
		if err = replaceRequestBody(c, body); err != nil {
			return err
		}
	}
	// 仅在正文/URL 成功同步后发布映射，失败请求不会伪称已完成改写。
	common.SetContextKey(c, constant.ContextKeyTokenRequestedModel, source)
	common.SetContextKey(c, constant.ContextKeyTokenTargetModel, target)
	return nil
}

// replaceRequestBody 统一替换可重复读取的正文，并使已解析表单失效。
// 新存储创建成功后才释放旧文件；最终由请求清理中间件关闭新存储。
// 此函数与任务插件的 JSON 改写共用，避免两条路径的缓存规则漂移。
// ContentLength 与真实字节数一致，避免上游按旧长度截断正文。
// 移除旧 Content-Length 头，由 Go 传输层按新长度重新生成。
// Request.GetBody 在此链路未被使用；重试依赖统一 BodyStorage。
func replaceRequestBody(c *gin.Context, body []byte) error {
	storage, err := common.CreateBodyStorage(body)
	if err != nil {
		return err
	}
	common.CleanupBodyStorage(c)
	c.Set(common.KeyBodyStorage, storage)
	c.Set(common.KeyRequestBody, nil)
	c.Request.Body = io.NopCloser(storage)
	c.Request.ContentLength = int64(len(body))
	c.Request.Header.Del("Content-Length")
	// 在解析缓存存在时同步清理磁盘文件，后续解析将读取新正文。
	if c.Request.MultipartForm != nil {
		_ = c.Request.MultipartForm.RemoveAll()
	}
	c.Request.MultipartForm, c.Request.Form, c.Request.PostForm = nil, nil, nil
	return nil
}
