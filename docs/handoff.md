# API Key 映射与 GPT 搜索：本地完成，待发布

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
