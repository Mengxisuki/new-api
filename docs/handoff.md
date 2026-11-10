# API Key 映射与 GPT 搜索：本地完成，待发布

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
