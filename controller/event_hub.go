package controller

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// HubBrowserOnly 使管理入口仅接受真实网页登录，外部消费令牌不能管理服务。
// RootAuth必须先执行，身份与角色来自现有认证链而非请求参数。
// 消费凭据是对某个消费者的委托，不代表持有人获得发行者的后台权限。
// 网页登录访问令牌也必须带服务端仍有效的会话身份，不能只检查数字角色。
// 统一禁用响应缓存，防止一次性明文凭据进入浏览器或共享代理缓存。
// 失败后中止中间件链，不能仅写出错误而继续进入管理处理器。
// 本中间件不重复解析 Cookie，认证来源继续由现有 RootAuth 决定。
func HubBrowserOnly(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, ok := middleware.GetSessionAuthIdentity(c); !ok {
		hubError(c, model.ErrHubForbidden)
		c.Abort()
		return
	}
	c.Next()
}

// hubError 保留失败分类；内部错误写服务日志，响应不泄露SQL或凭证。
// 已建立流后不能再写普通JSON，流内错误由调用者发送固定错误事件。
// 此入口处理尚未升级为事件流的普通请求错误，统一返回控制台消息键。
// 输入错误表示请求需要修改，授权错误表示当前身份无权执行目标动作。
// 冲突通常来自并发状态变化，客户端应刷新而不是自动重放业务操作。
// 资源不存在返回独立状态，适用于已清理事件与不属于消费者的投递编号。
// 数据库错误保留服务端定位信息，客户端只能得到固定失败说明。
// 错误匹配使用 errors.Is，使模型包装的冲突和权限错误仍保留原分类。
// 消费凭据的初次拒绝不走这里，认证中间件统一隐藏具体凭据失败原因。
// 调用者必须在返回错误后结束当前业务分支，不能随后写成功响应。
func hubError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	key := "Event push operation failed"
	switch {
	case errors.Is(err, model.ErrHubInvalid):
		status = http.StatusBadRequest
		key = model.ErrHubInvalid.Error()
	case errors.Is(err, model.ErrHubForbidden):
		status = http.StatusForbidden
		key = model.ErrHubForbidden.Error()
	case errors.Is(err, model.ErrHubConflict):
		status = http.StatusConflict
		key = model.ErrHubConflict.Error()
	case errors.Is(err, gorm.ErrRecordNotFound):
		status = http.StatusNotFound
		key = "Event push resource not found"
	default:
		common.SysError(fmt.Sprintf("event hub operation failed: %v", err))
	}
	common.ApiErrorStatus(c, status, common.NewMessage(key), nil)
}

// 管理请求只接收有界JSON，防止订阅数组或资料载荷无界占用内存。
// 所有业务字段在模型边界继续校验，语法错误不能落成默认配置。
// 请求正文上限覆盖全部管理表单，数组字段另由模型限制元素数量。
// 截断或无效 JSON 都是显式请求失败，不保留部分解析出的字段继续执行。
// 只在系统入口解析外部数据，内部模型使用已确定的结构和类型。
// 公共 JSON 包保持项目统一的数字和编码行为，不在此引入第二套序列化。
// 处理器仍负责必填项和跨字段业务约束，语法有效不等于已获授权。
func hubBody(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	if err := common.DecodeJson(c.Request.Body, target); err != nil {
		hubError(c, model.ErrHubInvalid)
		return false
	}
	return true
}

// 消费者和令牌摘要把内部集合解码为数组，损坏配置显式报错。
// 原文凭证始终不在这些摘要里，避免Query缓存或审计捕获密钥。
// 管理响应使用显式白名单，不能直接展开消费者模型暴露发行者认证版本。
// 主题和关注用户在数据库内以文本保存，API 必须转换为可校验数组。
// 积压数量表示已物化且未确认的投递，不等同于最后一次网络发送成功。
// 后台定期补充分配，因此刚生成的事件可能稍后才计入该统计。
// 凭据数量仅统计当前可用项，已撤销及到期项仍可在详情审计中查看。
// 在线连接以数据库短租约为准，不依赖单个服务进程的内存连接表。
// 同一消费者的多枚 Token 共用确认进度，但每条连接单独占用租约。
// 发行者被禁用、降权或更新认证版本时，已有委托需要重新建立。
// 授权失效会展示为可操作状态，不能把真实查询故障解释为普通离线。
// 过期未确认累计值提醒先核对业务记录，不代表可无限保留或重放历史。
func hubConsumerView(consumer model.HubConsumer) (gin.H, error) {
	topics, err := model.HubTopicList(consumer.Topics)
	if err != nil {
		return nil, err
	}
	users, err := model.HubConsumerUsers(&consumer)
	if err != nil {
		return nil, err
	}
	var backlog, tokens, connections int64
	if err := model.DB.Model(&model.HubDelivery{}).Where("consumer_id = ? AND acked_at = 0", consumer.ID).Count(&backlog).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.HubToken{}).Where("consumer_id = ? AND revoked_at = 0 AND expires_at > ?", consumer.ID, common.GetTimestamp()).Count(&tokens).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.HubConnection{}).Where("consumer_id = ? AND expires_at > ?", consumer.ID, common.GetTimestamp()).Count(&connections).Error; err != nil {
		return nil, err
	}
	// 授权失效是可展示状态，数据库故障必须继续返回错误而非伪装失效。
	valid := true
	if err := model.ValidateHubOwner(&consumer); err != nil {
		if !errors.Is(err, model.ErrHubForbidden) {
			return nil, err
		}
		valid = false
	}
	return gin.H{"id": consumer.ID, "name": consumer.Name, "topics": topics, "user_ids": users, "enabled": consumer.Enabled, "created_at": consumer.CreatedAt, "last_seen_at": consumer.LastSeenAt, "backlog": backlog, "token_count": tokens, "online_connections": connections, "auth_valid": valid, "expired_unacked_count": consumer.ExpiredUnackedCount}, nil
}

// hubTokenView仅返回不可用于认证的后缀和业务元数据。
// topic解析失败不能伪装成无权限令牌，以免掩盖持久数据损坏。
// 完整原文只允许在创建响应出现，后续列表和详情都无法恢复同一秘密。
// 指纹用于服务端查找和审计，不作为前端识别字段返回。
// 短后缀只帮助管理员区分凭据，不能用于鉴权或重置授权范围。
// 每枚凭据保留自己的主题上限，消费者新增主题不会自动扩展旧 Token。
// 有效期和撤销时间同时保留，便于解释失效原因和制定轮换安排。
// 数据库中的主题损坏属于故障，不允许用空集合掩盖配置问题。
func hubTokenView(token model.HubToken) (gin.H, error) {
	topics, err := model.HubTopicList(token.Topics)
	if err != nil {
		return nil, err
	}
	return gin.H{"id": token.ID, "name": token.Name, "token_hint": token.TokenHint, "topics": topics, "expires_at": token.ExpiresAt, "created_at": token.CreatedAt, "last_used_at": token.LastUsedAt, "revoked_at": token.RevokedAt}, nil
}

// HubOverview 汇总服务状态与独立消费者；只读管理页不会创建投递或确认消息。
// 未分配积压由后台生产worker及时物化，在线连接使用跨节点短租约。
// 入口仅供 Root 会话读取，外部消费者不读取整个平台的连接和账务概况。
// 部署级设置与消费者列表同时返回，页面可以用一份快照展示当前服务状态。
// 消费者数量有模型层上限，列表不开放任意跨系统检索或排序表达式。
// 各统计读取主库，Redis 暂时不可用不应导致事件管理失去真实状态。
// 统计是管理观察值，不构成消费端确认、对账或计费成功的证据。
// 某个消费者摘要解析失败会使整个请求失败，不能隐藏坏配置继续显示全绿。
// 事件总量与待确认投递是不同维度，一条事件可为多个消费者各分配一份。
// 异步任务已经终态而观察仍未决时，需要保留提交结果未知的事实。
// 未决记录不会通过管理页自动补扣，避免对跨系统不确定结果再次收费。
// 缓存待同步与资金待确认分别统计，已提交数据库的资金不能再次执行。
// 主题标签是前端翻译键，协议使用稳定主题标识而不随界面语言改变。
// 任一查询失败都会返回错误，页面不得把缺少任务表等故障显示成零积压。
func HubOverview(c *gin.Context) {
	settings, err := model.GetHubSettings()
	if err != nil {
		hubError(c, err)
		return
	}
	var consumers []model.HubConsumer
	if err := model.DB.Order("id DESC").Find(&consumers).Error; err != nil {
		hubError(c, err)
		return
	}
	items := make([]gin.H, 0, len(consumers))
	for _, consumer := range consumers {
		item, err := hubConsumerView(consumer)
		if err != nil {
			hubError(c, err)
			return
		}
		items = append(items, item)
	}
	var count int64
	if err := model.DB.Model(&model.HubEvent{}).Count(&count).Error; err != nil {
		hubError(c, err)
		return
	}
	// 未决跨系统状态保持可见，禁止用重新扣费按钮掩盖未知提交结果。
	var unresolved, syncPending int64
	terminal := model.DB.Model(&model.Task{}).Select("task_id").Where("status IN ?", []string{model.TaskStatusSuccess, model.TaskStatusFailure})
	if err := model.DB.Model(&model.HubChargeObservation{}).Where("state = ? AND task_id <> ? AND task_id IN (?)", model.HubChargePending, "", terminal).Count(&unresolved).Error; err != nil {
		hubError(c, err)
		return
	}
	if err := model.DB.Model(&model.HubChargeObservation{}).Where("cache_sync_pending = ?", true).Count(&syncPending).Error; err != nil {
		hubError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"settings": settings, "topics": []gin.H{{"key": model.HubTopicTopup, "label": "Wallet top-up succeeded"}, {"key": model.HubTopicCharge, "label": "Unusual charge detected"}, {"key": model.HubTopicRecharge, "label": "Recharge information submitted"}}, "consumers": items, "events_count": count, "unresolved_charge_count": unresolved, "sync_pending_count": syncPending})
}

// HubUpdateSettings 保存完整设置快照；禁用只停止网络投递。
// 设置以完整快照提交，缺失的关键阈值不能被静默补成宽松默认值。
// 金额单位是内部额度整数，不是人民币分，也不与界面币种自动混用。
// 暂停只停止网络投递，业务事务继续保存事件供恢复后处理。
// 保留天数是消息恢复的明确边界，超期后通过缺口提示要求业务对账。
// 历史样本门槛和倍率在模型统一校验，后台检测器不另推断配置含义。
// 返回成功仅代表设置已保存，不意味着所有已有连接已完成下一轮重检。
func HubUpdateSettings(c *gin.Context) {
	var settings model.HubSettings
	if !hubBody(c, &settings) {
		return
	}
	if err := model.UpdateHubSettings(settings); err != nil {
		hubError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// HubCreateConsumer 将当前Root作为发行者，客户端不能指定委托所有者。
// 可选用户过滤只能减少订阅范围，不扩展主题授权。
// 消费者代表逻辑接收用途，其生命周期独立于某一条网络连接。
// 发行者由已认证上下文确定，外部 JSON 不能指定其他用户为授权来源。
// 空关注用户集合表示所有已授权用户；非空集合只能缩小接收范围。
// 消费主题必须来自服务端固定目录，不能借名称注册任意生产入口。
// 创建时冻结发行者认证版本，之后密码重置等事件会撤销旧委托。
// 模型持锁检查数量，避免同一发行者的并发请求绕过服务消费者上限。
// 新建消费者还没有凭据，生成可用 Token 必须再通过单次安全证明。
// 响应摘要包含独立消费状态，名称本身不参与鉴权或幂等判断。
func HubCreateConsumer(c *gin.Context) {
	var req struct {
		Name    string   `json:"name"`
		Topics  []string `json:"topics"`
		UserIDs []int    `json:"user_ids"`
	}
	if !hubBody(c, &req) {
		return
	}
	consumer, err := model.CreateHubConsumer(c.GetInt("id"), req.Name, req.Topics, req.UserIDs)
	if err != nil {
		hubError(c, err)
		return
	}
	item, err := hubConsumerView(*consumer)
	if err != nil {
		hubError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

// HubUpdateConsumer 仅接收显示和订阅字段，禁止任意模型字段更新。
// 既有令牌不会自动获得新增主题，连接逐次重查订阅交集。
// 路径编号只定位消费者，权限仍来自管理会话，不相信请求声明的所有者。
// 可编辑字段使用专门请求结构，阻止认证版本、累计缺口等内部字段被覆盖。
// 订阅修改立即影响后续重检，已有投递不赋予被移除主题的永久读取权。
// 缩小关注用户同样约束补发与确认，不能靠历史投递绕过新范围。
// 新增主题只扩大消费者上限，旧 Token 仍保持创建时的独立主题限制。
// 禁用不删除事件与确认记录，重新启用后仍可按保留期继续处理。
// 普通改名不重新生成凭据，不打断同一消费者已保存的确认进度。
// 更新失败保留模型返回分类，管理员据此区分无效配置与状态冲突。
func HubUpdateConsumer(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		hubError(c, model.ErrHubInvalid)
		return
	}
	var req struct {
		Name    string   `json:"name"`
		Topics  []string `json:"topics"`
		UserIDs []int    `json:"user_ids"`
		Enabled bool     `json:"enabled"`
	}
	if !hubBody(c, &req) {
		return
	}
	if err := model.UpdateHubConsumer(id, req.Name, req.Topics, req.UserIDs, req.Enabled); err != nil {
		hubError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// HubConsumerDetail 仅返回最近五十条投递；历史全部事件通过独立分页入口查询。
// 此操作不会把已发送标成已处理，管理查看与消费ACK分离。
// 详情用于管理观察，不建立消费连接，也不计作某条消息已发送。
// 消费者摘要与概览使用同一转换入口，在线和积压定义保持一致。
// 所有凭据保留创建及撤销历史，管理员可以判断哪次轮换仍未完成。
// Token 原文和指纹不进入响应，查询缓存只能保存不可认证的摘要。
// 投递展示当前消费者的一份记录，不能把同事件的其他消费者状态合并。
// 投递载荷来自已保存的安全摘要，读取详情不会解密代充值账户资料。
// 历史记录受当前订阅和关注范围过滤，撤销范围后不再暴露旧消息内容。
// 最近记录限制控制单次响应体积，更多事件通过单独分页入口查看。
// 坏主题或坏载荷显式失败，不能返回空列表让管理员误以为没有待办。
func HubConsumerDetail(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		hubError(c, model.ErrHubInvalid)
		return
	}
	consumer, err := model.GetHubConsumer(id)
	if err != nil {
		hubError(c, err)
		return
	}
	item, err := hubConsumerView(*consumer)
	if err != nil {
		hubError(c, err)
		return
	}
	var tokens []model.HubToken
	if err := model.DB.Where("consumer_id = ?", id).Order("id DESC").Find(&tokens).Error; err != nil {
		hubError(c, err)
		return
	}
	tokenItems := make([]gin.H, 0, len(tokens))
	for _, token := range tokens {
		view, err := hubTokenView(token)
		if err != nil {
			hubError(c, err)
			return
		}
		tokenItems = append(tokenItems, view)
	}
	topics, err := model.HubTopicList(consumer.Topics)
	if err != nil {
		hubError(c, err)
		return
	}
	deliveries, err := model.ListHubDeliveries(consumer, topics, false)
	if err != nil {
		hubError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"consumer": item, "tokens": tokenItems, "deliveries": deliveries})
}

// HubCreateToken 的单次证明绑定消费者、规范化主题和有效期。
// 证明消费前先校验请求，返回的明文只存在于本次no-store响应。
// 这是签发长期委托凭据的敏感操作，Root 登录本身不替代本次单次证明。
// 请求先验证主题和有效期，避免无效表单提前消耗用户安全验证结果。
// 主题经过去重排序后进入证明，等价顺序不能产生不同授权上下文。
// 目标消费者编号也属于证明内容，其他消费者的验证结果不能挪用。
// 证明中的有效期与请求完全一致，验证后不能偷偷延长委托时长。
// 名称用于管理识别，不改变消费者归属或授予任何后台业务权限。
// 消费者主题是上界，Token 可以进一步收窄，不能反过来扩大其订阅。
// 安全证明仅可消费一次，网络重试失败时应先核对是否已经生成过凭据。
// 凭据随机材料来自项目安全随机源，数据库只保存不可直接使用的指纹。
// 创建模型再次检查持久消费者状态和数量，避免仅依赖处理器的预检查。
// 明文只在本次 no-store 响应返回，刷新管理页不会恢复同一份秘密。
// 无法保存或转换新记录时不能宣称签发成功，错误继续向客户端传播。
// 安全审计记录消费者、令牌编号和主题，不记录明文、证明或认证请求头。
// 手动审计先于成功响应标记，管理兜底无需再次捕获包含明文的响应内容。
// 消费端只允许事件读取和确认，持有该 Token 不能退款、办理订单或调用模型。
func HubCreateToken(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		hubError(c, model.ErrHubInvalid)
		return
	}
	var req struct {
		Name      string   `json:"name"`
		Topics    []string `json:"topics"`
		ExpiresAt int64    `json:"expires_at"`
	}
	if !hubBody(c, &req) {
		return
	}
	topics, err := model.HubTopics(req.Topics)
	if err != nil {
		hubError(c, err)
		return
	}
	consumer, err := model.GetHubConsumer(id)
	if err != nil {
		hubError(c, err)
		return
	}
	own, err := model.HubTopicList(consumer.Topics)
	if err != nil {
		hubError(c, err)
		return
	}
	for _, topic := range topics {
		if !slices.Contains(own, topic) {
			hubError(c, model.ErrHubForbidden)
			return
		}
	}
	if req.ExpiresAt < common.GetTimestamp()+3600 || req.ExpiresAt > common.GetTimestamp()+366*86400 || strings.TrimSpace(req.Name) == "" {
		hubError(c, model.ErrHubInvalid)
		return
	}
	binding, err := common.Marshal(service.HubTokenVerificationContext{ConsumerID: id, Topics: topics, ExpiresAt: req.ExpiresAt})
	if err != nil {
		hubError(c, err)
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeHubToken, Context: binding}) == nil {
		return
	}
	raw, token, err := model.CreateHubToken(id, req.Name, topics, req.ExpiresAt)
	if err != nil {
		hubError(c, err)
		return
	}
	item, err := hubTokenView(*token)
	if err != nil {
		hubError(c, err)
		return
	}
	// 显式写脱敏安全审计，使管理审计兜底不截获带明文的响应。
	recordManageAudit(c, "event_hub.token.generate", map[string]any{"consumer_id": id, "token_id": token.ID, "topics": topics})
	common.ApiSuccess(c, gin.H{"token": raw, "item": item})
}

// HubRevokeToken 撤销只影响目标消费者的目标凭证，不删除消费进度。
// 现有长连接会在两秒内的重检中关闭，其他令牌保持可用。
// 撤销必须同时匹配消费者和凭据编号，不能只凭全局编号改动其他接收方。
// 记录保留撤销时间而不是物理删除，方便接手者追溯授权生命周期。
// 同消费者其他凭据继续有效，可先创建替代凭据再安全完成轮换。
// 确认进度属于消费者，所以撤销、到期或断线都不会重置已确认消息。
// 已有流在后续身份检查失败后停止发送，客户端会得到固定撤销错误事件。
// 慢客户端仍受写入期限约束，控制面不等待远端主动关闭连接。
// 审计只保存目标编号，不能回显原有秘密以证明撤销对象。
func HubRevokeToken(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		hubError(c, model.ErrHubInvalid)
		return
	}
	tokenID, err := strconv.Atoi(c.Param("token_id"))
	if err != nil || tokenID <= 0 {
		hubError(c, model.ErrHubInvalid)
		return
	}
	if err := model.RevokeHubToken(id, tokenID); err != nil {
		hubError(c, err)
		return
	}
	recordManageAudit(c, "event_hub.token.revoke", map[string]any{"consumer_id": id, "token_id": tokenID})
	common.ApiSuccess(c, nil)
}

// HubListEvents 是管理审计入口，分页大小固定以限制响应和解析成本。
// 事件只含安全摘要，不需要调用敏感资料读取接口。
// 这是全平台安全摘要目录，普通事件消费令牌不获得该管理读取权限。
// 分页编号有上界，阻止客户端构造无界 OFFSET 和超大响应。
// 固定每页五十条，同时限制数据库返回量和浏览器 JSON 解析成本。
// 总数反映保留期清理后的现存事件，不能当作永久审计归档承诺。
// 按标识倒序便于管理浏览，传输恢复不以这里的最大编号推进 ACK。
// 持久载荷必须是合法 JSON 对象，解码失败作为数据故障显式暴露。
// 返回值只包含展示需要的字段，不暴露内部幂等键或来源认证材料。
func HubListEvents(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > 100000 {
		hubError(c, model.ErrHubInvalid)
		return
	}
	var total int64
	if err := model.DB.Model(&model.HubEvent{}).Count(&total).Error; err != nil {
		hubError(c, err)
		return
	}
	var events []model.HubEvent
	if err := model.DB.Order("id DESC").Offset((page - 1) * 50).Limit(50).Find(&events).Error; err != nil {
		hubError(c, err)
		return
	}
	items := make([]gin.H, 0, len(events))
	for _, event := range events {
		var payload map[string]any
		if err := common.UnmarshalJsonStr(event.Payload, &payload); err != nil {
			hubError(c, err)
			return
		}
		items = append(items, gin.H{"id": event.ID, "topic": event.Topic, "payload": payload, "created_at": event.CreatedAt})
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total})
}

// HubReplayEvent 只重新开放一份已授权通知，不触发支付或资金结算。
// 补发针对消费者与事件这一对关系，不会给所有接收方重开同一通知。
// 参数使用事件编号而非最近一次投递编号，方便从事件管理列表发起。
// 模型重新检查当前订阅与关注用户，不允许补发绕过已经收窄的授权。
// 事件超过保留期或已被清理时返回失败，不能伪造空消息表示已补发。
// 已有确认会被重新打开，但事件原始内容和生成时间保持不变。
// 消费者收到重复事件仍需按事件编号幂等，补发不代表应重复办理业务。
// 充值、退款和订单状态更新都不在此调用链，防止重发变成资金重试。
// 成功响应只代表投递已可再次发送，不能提前标记客户端处理成功。
func HubReplayEvent(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		hubError(c, model.ErrHubInvalid)
		return
	}
	var req struct {
		EventID int64 `json:"event_id"`
	}
	if !hubBody(c, &req) {
		return
	}
	if req.EventID <= 0 {
		hubError(c, model.ErrHubInvalid)
		return
	}
	if err := model.ReplayHubEvent(id, req.EventID); err != nil {
		hubError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// hubClientIdentity 在消费入口与每轮长连接循环重复执行，拒绝已撤销授权。
// 外部Token仅从Authorization读取；网页登录仍验证JWT及服务端会话。
// 消费者所属账号从数据库读取，不能由URL指定用户角色。
// 外部凭据与浏览器登录令牌在同一协议入口区分，不能互相回退解释。
// 明确拒绝查询字符串中的凭据，避免代理访问日志和复制链接泄露秘密。
// 专用前缀触发事件 Token 验证，其指纹、过期、撤销和发行者由主库确认。
// 查询指定的消费者只能与 Token 固定归属一致，参数不授予跨消费者权限。
// 外部 Token 的实际主题是凭据和当前消费者订阅交集，不能信任客户端选项。
// 未知格式不会被当作匿名浏览器继续处理，所有消费请求都必须认证。
// 网页登录分支先校验签名和令牌用途，再检查服务端会话是否仍存活。
// 个人访问令牌不是浏览器会话，不能借普通管理 PAT 进入此分支。
// 不能只读取缓存中的历史角色，当前 Root 身份与启用状态再由主库确认。
// 认证版本匹配防止密码重置后旧浏览器委托继续读取资金与订单事件。
// 浏览器不持有固定事件消费者，因此必须显式传入消费者编号。
// 编号来自 URL 只决定定位，发行者有效性仍从持久消费者关系验证。
// 停用消费者会令验证失败，不允许只停止新连接而保留旧连接继续消费。
// 发行者降权或停用会撤销其全部委托，不要求逐枚清理已有 Token。
// 返回 nil Token 仅表示经过真实浏览器会话认证，不能推断为匿名。
// 返回的消费者对象是当次校验快照，下一轮长连接循环必须重新获取。
// 流存活期间不缓存授权决定，避免长时间连接绕过后续撤销或订阅变化。
// 数据库不可用时失败关闭，不能把认证读取失败视为继续沿用旧权限。
// 初次握手统一隐藏拒绝细节，已有连接只收到固定错误码而非驱动原文。
// 此函数不注入平台管理角色，消费确认与后台管理继续使用独立信任边界。
func hubClientIdentity(c *gin.Context) (*model.HubConsumer, *model.HubToken, []string, error) {
	header := c.GetHeader("Authorization")
	raw, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || raw == "" || c.Query("token") != "" || c.Query("access_token") != "" {
		return nil, nil, nil, model.ErrHubForbidden
	}
	if strings.HasPrefix(raw, model.HubTokenPrefix) {
		consumer, token, topics, err := model.AuthenticateHubToken(raw)
		if err != nil {
			return nil, nil, nil, err
		}
		if wanted := c.Query("consumer_id"); wanted != "" && wanted != strconv.Itoa(consumer.ID) {
			return nil, nil, nil, model.ErrHubForbidden
		}
		return consumer, token, topics, nil
	}
	identity, err := service.ParseAccessToken(raw)
	if err != nil {
		return nil, nil, nil, model.ErrHubForbidden
	}
	if _, _, err := service.ValidateLoginSession(identity); err != nil {
		return nil, nil, nil, err
	}
	var user model.User
	if err := model.DB.Select("id", "role", "status", "auth_version").First(&user, identity.UserID).Error; err != nil {
		return nil, nil, nil, err
	}
	if user.Role != common.RoleRootUser || user.Status != common.UserStatusEnabled || user.AuthVersion != identity.UserAuthVersion {
		return nil, nil, nil, model.ErrHubForbidden
	}
	id, err := strconv.Atoi(c.Query("consumer_id"))
	if err != nil || id <= 0 {
		return nil, nil, nil, model.ErrHubInvalid
	}
	consumer, err := model.GetHubConsumer(id)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := model.ValidateHubOwner(consumer); err != nil {
		return nil, nil, nil, err
	}
	topics, err := model.HubTopicList(consumer.Topics)
	return consumer, nil, topics, err
}

// HubClientAuth 只服务消费和ACK两个接口，不向通用认证链注入管理身份。
// 拒绝响应不区分令牌不存在、过期或撤销，避免凭证枚举。
// 只在消费路由使用该中间件，不能把专用凭据身份接入通用管理 API。
// Cache-Control 同时覆盖成功和拒绝响应，认证结果不进入共享缓存。
// 初次认证失败不区分未知、过期或已撤销 Token，减少凭据枚举信息。
// 失败必须 Abort，避免后续 SSE 处理器写出已连接帧造成伪成功。
// 上下文中的消费者和主题用于路由内传递，不接受请求自报的权限字段。
// 处理器对长操作再次检查身份，握手成功不是整个连接生命周期的永久授权。
// 网页 JWT 与外部 Token 最终遵循相同消费者边界，确认契约保持一致。
// 本入口不写原始凭据审计；使用时间和连接状态由模型保存安全元数据。
func HubClientAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		consumer, token, topics, err := hubClientIdentity(c)
		if err != nil {
			common.ApiErrorStatus(c, http.StatusUnauthorized, common.NewMessage("Event push access denied"), nil)
			c.Abort()
			return
		}
		c.Set("hub.consumer", consumer)
		c.Set("hub.token", token)
		c.Set("hub.topics", topics)
		c.Next()
	}
}

// HubAck 以持久确认作为恢复依据，不信任Last-Event-ID自动跳过消息。
// ACK必须同时匹配消费者、当前主题和当前关注用户范围。
// 确认粒度是单个投递编号，不提供“截至最大编号全部完成”的跳跃操作。
// Last-Event-ID 只是客户端传输提示，不足以证明应用已经处理过消息。
// 处理器重鉴权，避免在中间件检查后长期使用已撤销的身份快照。
// 路径不允许指定别人的消费者，实际消费者来自当前凭据固定归属。
// 查询同时匹配投递编号和消费者，不能靠猜测连续 ID 确认他人待办。
// 事件正文不由 ACK 请求提供，客户端不能替换主题后获得不对应的授权。
// 当前订阅和 Token 主题交集重新应用，历史投递不代表永久可确认权限。
// 关注用户缩小后，同主题的其他用户历史消息也不能继续确认。
// 模型只接受已标记过发送的投递，防止客户端直接跳过从未接收的积压。
// 消费者和投递在事务中按统一顺序持锁，与过期清理协调确认结果。
// 已经确认的同一投递再次 ACK 成功，支持客户端在网络响应丢失后重试。
// 确认只记录接收方处理声明，不改变充值、扣费或代充值订单业务状态。
// 资源过期或被清理时明确报错，客户端需要从业务接口对账恢复事实。
// 成功不返回事件载荷，防止 ACK 被误当作另一条不受控制的数据读取接口。
func HubAck(c *gin.Context) {
	consumer, _, topics, err := hubClientIdentity(c)
	if err != nil {
		hubError(c, model.ErrHubForbidden)
		return
	}
	var req struct {
		DeliveryID int64 `json:"delivery_id"`
	}
	if !hubBody(c, &req) {
		return
	}
	if req.DeliveryID <= 0 {
		hubError(c, model.ErrHubInvalid)
		return
	}
	var delivery model.HubDelivery
	if err := model.DB.Where("id = ? AND consumer_id = ?", req.DeliveryID, consumer.ID).First(&delivery).Error; err != nil {
		hubError(c, err)
		return
	}
	var event model.HubEvent
	if err := model.DB.First(&event, delivery.EventID).Error; err != nil {
		hubError(c, err)
		return
	}
	allowed, err := model.HubEventAllowed(consumer, &event, topics)
	if err != nil {
		hubError(c, err)
		return
	}
	if !allowed {
		hubError(c, model.ErrHubForbidden)
		return
	}
	if err := model.AckHubDelivery(consumer.ID, req.DeliveryID); err != nil {
		hubError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// HubStream 使用有界数据库批次和显式ACK提供至少一次投递。
// 每轮重鉴权并续租；慢连接有写入期限，不能阻塞生产事务。
// 心跳不代表消费成功；撤销或服务器故障使用固定错误事件后关闭。
// 连接开始前先验证服务设置，暂停时返回普通 503 而不建立空成功流。
// 初次身份校验与循环内校验分开，前者决定是否允许握手，后者维持授权。
// 消费者连接数由主库短租约控制，多个服务节点共用同一并发上限。
// 浏览器连接没有事件 Token 编号，但仍计入所属消费者的连接配额。
// 连接随机标识只用于租约管理，不可用于认证、补发或确认消息。
// 退出时清理租约；进程异常退出则依靠到期机制恢复真实在线统计。
// Content-Type 与明确刷新让普通 HTTP 代理识别为持续事件流。
// X-Accel-Buffering 仅提示兼容代理，正式路由仍需排除应用层 gzip。
// 每次写入设置有界期限，慢读客户端不能无限持有数据库和服务资源。
// 刷新同样检查错误，不能只检查写入内存缓冲区成功就认为网络正常。
// 首次注释帧让客户端立即观察连接，retry 值只是建议重连间隔。
// 两秒循环兼顾撤销响应与数据库开销，心跳频率独立于授权检查频率。
// 每次循环重新读取当前订阅，长期连接不能固定初始允许的全部主题。
// 保留期缺口在新连接告知一次，提醒离线客户端先从业务状态恢复。
// 同连接累计缺口增加时再次提示，不用 ACK 消除曾经丢失的历史事实。
// 运行中暂停以固定流内错误结束，避免已经发出 SSE 头后混入普通 JSON。
// 失去租约必须停止发送，不能复活过期连接绕过并发上限。
// 物化步骤分配每消费者独立记录，不依赖全局最大事件游标。
// 读取待发送批次受当前 Token 主题和关注用户限制，每轮批次有界。
// 先持久记录发送尝试再写网络，写失败会在重投间隔后恢复。
// 该顺序提供至少一次投递，不承诺网络上的恰好一次；客户端应按 event_id 去重。
// 发送时间和次数只是尝试元数据，只有显式 ACK 才表示应用处理完成。
// SSE 的 id 使用投递编号，业务幂等仍使用载荷里的稳定事件编号。
// 心跳使用注释帧，不产生业务待办，也不需要客户端确认。
// 请求取消及时退出并释放租约，客户端关闭窗口不会隐式确认最后一条消息。
func HubStream(c *gin.Context) {
	consumer, token, _, err := hubClientIdentity(c)
	if err != nil {
		hubError(c, model.ErrHubForbidden)
		return
	}
	settings, err := model.GetHubSettings()
	if err != nil {
		hubError(c, err)
		return
	}
	if !settings.Enabled {
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Event push delivery is paused"), nil)
		return
	}
	tokenID := 0
	if token != nil {
		tokenID = token.ID
	}
	connectionID, err := model.OpenHubConnection(consumer.ID, tokenID)
	if err != nil {
		hubError(c, err)
		return
	}
	defer func() {
		if err := model.DB.Where("id = ?", connectionID).Delete(&model.HubConnection{}).Error; err != nil {
			common.SysError(fmt.Sprintf("event hub connection cleanup failed: %v", err))
		}
	}()
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-store")
	c.Header("X-Accel-Buffering", "no")
	controller := http.NewResponseController(c.Writer)
	write := func(frame string) bool {
		if err := controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return false
		}
		if _, err := fmt.Fprint(c.Writer, frame); err != nil {
			return false
		}
		if err := controller.Flush(); err != nil {
			return false
		}
		return true
	}
	if !write(": connected\nretry: 3000\n\n") {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	heartbeat := time.Now()
	var reportedGap int64 = -1
	for {
		consumer, token, topics, err := hubClientIdentity(c)
		if err != nil {
			write("event: error\ndata: {\"code\":\"authorization_revoked\"}\n\n")
			return
		}
		// 过期未确认记录已经无法重放，客户端必须从业务接口重新核对。
		// 累计缺口保留审计，每次新连接通知一次，不用伪ACK清除历史事实。
		if consumer.ExpiredUnackedCount > 0 && consumer.ExpiredUnackedCount != reportedGap {
			gap, err := common.Marshal(gin.H{"expired_unacked_count": consumer.ExpiredUnackedCount, "action": "reconcile_business_state"})
			if err != nil || !write(fmt.Sprintf("event: retention_gap\ndata: %s\n\n", gap)) {
				return
			}
			reportedGap = consumer.ExpiredUnackedCount
		}
		settings, err := model.GetHubSettings()
		if err != nil {
			write("event: error\ndata: {\"code\":\"storage_unavailable\"}\n\n")
			return
		}
		if !settings.Enabled {
			write("event: error\ndata: {\"code\":\"delivery_paused\"}\n\n")
			return
		}
		tokenID = 0
		if token != nil {
			tokenID = token.ID
		}
		if err := model.TouchHubConnection(connectionID, consumer.ID, tokenID); err != nil {
			write("event: error\ndata: {\"code\":\"storage_unavailable\"}\n\n")
			return
		}
		if _, err := model.MaterializeHubDeliveries(consumer.ID, topics); err != nil {
			write("event: error\ndata: {\"code\":\"storage_unavailable\"}\n\n")
			return
		}
		deliveries, err := model.ListHubDeliveries(consumer, topics, true)
		if err != nil {
			write("event: error\ndata: {\"code\":\"storage_unavailable\"}\n\n")
			return
		}
		for _, delivery := range deliveries {
			delivery.LastSentAt = common.GetTimestamp()
			delivery.Attempts++
			data, err := common.Marshal(delivery)
			if err != nil {
				return
			}
			if err := model.MarkHubDeliverySent(consumer.ID, delivery.ID); err != nil {
				write("event: error\ndata: {\"code\":\"storage_unavailable\"}\n\n")
				return
			}
			if !write(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", delivery.ID, delivery.Topic, data)) {
				return
			}
		}
		if time.Since(heartbeat) >= 15*time.Second {
			if !write(": heartbeat\n\n") {
				return
			}
			heartbeat = time.Now()
		}
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
