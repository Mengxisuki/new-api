# API Key 映射与 GPT 搜索：本地完成，待发布

## 2026-11-12 10:48:08 +00:00 — 财务总览：第 1 期本地实现，后续范围保留

分支 `oak`（基于 `3e8977c53`）。用户已确认开始第 1 期：`/finance/overview`、只读聚合接口、独立的 `finance_overview:read` 权限及 Root 对管理员/财务角色的逐用户授权已实现。不新增业务表或修改支付事务，未提交、推送或部署。与下方其他任务相互独立。

目标：在「财务」分组新增 `/finance/overview`，让老板看到平台收入、待收、非现金发放和支出，并与挂帐、手动调额、兑换码、订阅等功能打通。

用户已确认的口径：

- 只做人民币，线上仅开放微信支付；国际渠道若有订单单列不合计。
- 实收按付款日一次性计入，客户订阅不按周期摊销。
- 挂帐发生时进待收，财务结清时（`credit_entries.settled_at`）转入实收；付款方式为 `credit` 的订单不得计入现金实收。
- 余额支付（`wallet`）的代充值订单不计收入，只算内部消耗。
- 手动调额必填分类：补偿、赠送、挂帐、线下收款；扣减和覆盖同样必填，覆盖按差额记账。线下收款录入即计入实收，金额与凭证号必填。
- 管理员开通订阅、人工补单复用同一套分类。
- 挂帐扩展到钱包充值和订阅，包含用户在付款页自助选择；三个模块共用授信额度，挂帐不开票的规则不变。
- 新增坏账核销：财务填原因后释放授信，不计实收，总览单列。
- 兑换码创建批次时选售出或赠送，售出填单码售价；按兑换时间计入，未兑换的售出码单列。
- 支出为手工台账，只能作废不能删除。订阅费用按「订阅类型的覆盖周期」逐日摊销，覆盖周期可配置；充值费用维护剩余余额，已消耗等于金额减剩余；其他类别按付款日计入。提供现金与成本两种视图。
- 上线前的历史调额显示为「未分类（历史）」，不进任何一类。
- 权限：新增独立资源（查看总览、管理支出台账），默认仅 Root，Root 可按用户授予管理员和财务角色；普通用户不支持。

已核实的现状与改动点：

- 手动调额只写日志库（`controller/user_quota.go`、`model/user_quota_adjustment.go`），没有账本和原因字段，需新增主库账本并与余额变更同事务。
- 挂帐三处写死代充值：`model/credit_account.go` 的 `SaveCreditAccount`、`CheckCreditAvailable`、`SettleCreditEntry`；结清必须能按模块定位单据。
- 充值到账经 `model/topup_official.go` 的 `CompleteOfficialTopUp`，订阅到账经 `model/subscription.go` 的 `completeSubscriptionOrder`；自助挂帐需要各加一条不经网关的记账路径，并保持订单、用户、订阅的锁序。
- 充值到账会经 `creditHubTopUpQuota` 写「充值到账」事件并带实付金额；挂帐充值不是现金到账，事件内容需要区分。
- 充值和订阅订单没有退款状态，挂帐后无法撤销债务，因此才需要坏账核销。
- 第 1 期已放开财务角色的 `finance_overview:read` 覆盖，且只更新总览资源，不连带改变其他财务授权；其他资源输入不会被财务角色采纳。财务路由仍保留 `middleware.FinanceAuth` 门槛，接口限真实登录会话。
- 管理员开通订阅时来源备注传空（`controller/subscription.go` 的 `AdminBindSubscription`）。
- 前端已有 `recharts` 和 `@visactor/react-vchart`，现有仪表盘在 `web/src/features/dashboard`，挂帐页在 `web/src/features/credit`。

建议分期（每期独立可交付）：

1. 总览页和独立权限，仅用现有数据：微信实收、代充值、现有挂帐、发票附加费、钱包余额与消耗、待办。
2. 调额账本与四分类；管理员开通订阅和补单分类；挂帐扩展到充值和订阅（含自助付款页）；坏账核销。
3. 兑换码售出与赠送。
4. 支出台账、订阅类型覆盖周期、毛利。

第 2 至 4 期新增表或字段，须按 `AGENTS.md` 在 SQLite、MySQL、PostgreSQL 上完成新建、升级和重复迁移验证。开始实现前须读 `web/AGENTS.md`；本任务不改计费表达式和预扣结算，是否触及 `.agents/rules/billing.md` 的范围在动手时再核对。

未核实：人工补单的订单是否有区别于日志的持久标记；各国际渠道订单金额的币种。

第 1 期验证与限制：SQLite 3.50.4、MySQL 8.4.2、PostgreSQL 14.17 的查询及会话权限均已实际运行；后端六个相关包回归、前端类型检查、定向 lint（零错误）及构建通过。最后一轮受影响前端 23 文件 224 项全部通过，总览和权限竞态检测通过；390 像素手机视口无页面横向溢出，运行时无异常。此前一次前端全量 2650 项中 2649 项通过，插件市场源码高亮测试一项失败，已在未改动的 HEAD 临时工作树中复现。最终新增回归与复核结果见 README 的「财务总览（第 1 期）」；临时服务、测试数据库和虚构数据均已清理。

只读复核补充：真实订阅到账会创建同交易号的 TopUp 镜像，聚合须按订阅交易号排除，不能按空 provider 全丢；Stripe 的 Money 可能是额度面额，非人民币网关只能标为记录值，不能声称现金实收；钱包合计包含赠送和手动调额，不等于现金；消费日志关闭时 QuotaData 也不再采集，消耗不可用状态需同时检查两个采集开关。

后续第 2 至 4 期范围已确认但未实现，不得将本次交付表述成完整财务系统。发布仍需明确授权；不要运行 `git add -A` 或推送 `oak`，它会触发生产自动部署。

## 2026-11-10 08:30:31 +00:00 — Responses 工具续轮推理历史修复待发布验收

目标：修复线上渠道 #9 工具续轮排查中发现的 reasoning 被误转为空 user、推理摘要丢失的问题，并验证实际第三方渠道能接收工具结果续轮。本地仅修改 `relaykit/relayconvert/internal/oai_responses/to_oai_chat_req.go`、既有对应测试及 `relaykit/README.md`；未提交、推送或生产写入。用户仅询问处理方案，尚未授权发布。

已验证：修复前合成测试失败，修复后 7 类消息历史通过；RelayKit 全量测试、`GOWORK=off go build ./...` 和 New API 服务入口 `go build -o /dev/null .` 通过。全仓构建被无关 `dujiao-next-main` 的既有 `gomonkey` go.sum 缺失阻断，未扩大修改依赖。证据与限制见 [排障记忆](memory/lessons.md#2026-11-10-083031-0000--responses-推理历史曾被误转成空用户轮本地已修复)。

下一步：先核对本地 diff 与线上版本，取得生产发布授权后只发布这项 New API 转换修复，再用合成提示词完成一次工具调用及结果续轮，核对三层日志。不要重放用户原任务或把本地回归当作第三方 400 已解决；也不要混入下方其他历史任务或工作区已有 Caddy/记忆改动。

## 2026-11-05 18:46:51 +00:00 — 挂靠额度顺延修复待配套发布

本轮只读核对wdm：New API为`oak-1ea2b201e07c-42083430069-1`，GPT-Load为`oak-255abf88459d-42005704315-1`。下面更早任务的发布状态不能覆盖本次实测版本。

本地已完成`controller/resource_relay.go`的可信429顺延与免费失败日志、既有测试文件的来源/资金/日志回归；GPT-Load配套修改`internal/gateway/handler.go`和`model_cooldown_test.go`。需求与验收证据见[排障记录](diagnostics/hosting-resource-fallback.md)。三库定向矩阵、两服务相关后端回归、vet及构建通过。未提交、推送或部署，用户规则要求发布前明确授权。

下一步：获得发布授权后先核对两个仓库diff/暂存区，只提交本任务相关修改；New API存在本轮之外的代充值、前端错误提示及记忆修改，GPT-Load已有未跟踪诊断目录，必须保留。先发布GPT-Load，再发布New API，最后用专用合成请求验证耗尽账号顺延与一次扣费；不修改真实余额或人为耗尽账号。无配置、依赖或迁移变更。

更新时间：2026-11-04 10:28:44 +00:00。

用户批准实现 Codex 保持源模型、Key 热更新选择 Kimi/GLM，并让 GPT 执行真实搜索。New API 已完成可配置搜索编排、分模型结算及父请求日志详情；GPT-Load 已有精确 reasoning.context 兼容改动。均未提交、推送或部署；推送 oak 会自动部署，不能在未获发布要求时执行。

- 实现与验收范围：[方案](plans/api-key-model-mapping.md) 的 GPT 搜索编排章节。
- 配置入口：系统设置 → 模型设置 → 联网搜索适配；默认关闭。现场验证 gpt-6.1-sol 为独占 Codex 路由。gpt-6-luna 会同时命中不支持搜索的 Go 组，不能直接照搬。
- Kimi/GLM → GPT 搜索 → 目标续轮，以及公开搜索历史的下一请求已真实通过。普通/流式、权限/资金/上游失败、调用上限和取消回归通过；UI 配置/日志测试、类型检查、lint、前端构建通过。最终输出采用阶段缓冲 SSE。
- 私有子请求复用 TokenAuth / ResourceRelay / Distribute / Relay；使用日志按父请求 ID 关联，GPT Tokens 与搜索调用费单独结算。没有新数据库结构或依赖。
- 后续若用户要求发布：先检查本地各仓库最新 diff 和暂存区，发布 GPT-Load 参数兼容及 New API 编排代码，再设置搜索模型、检查 Key 权限/价格/来源，最后用真实 Codex 客户端验收。当前真实联调用的是本地测试 DB + 临时回环代理 + 线上 GPT-Load；临时代理和 SSH 隧道已清理。
- 非本轮保证范围：WebSocket 搜索编排、持久响应、后台任务、任意供应商私有历史迁移、动态发现工具完整客户端流程。不要把合成测试表述为全客户端无缝保证。
- 保护此前已经暂存的 Caddy 正文日志、模型映射和文档修改；本轮未覆盖这些内容。

常用验证：`go test ./controller ./middleware ./model -run 'TestSearchBridge|TestTokenModelMapping|TestResourceRelayTokenModelMapping|TestTokenLegacyCacheRecoversMapping' -count=1`；搜索 `-race`；前端 `bun x tsgo -b`（本机 Bun 无 check 子命令）及两个功能测试；先 `bun run build` 再 `go build`，不能并行清空 Go 嵌入的 web/dist。
