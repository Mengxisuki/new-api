# API Key 模型映射

## 使用与计费合同

入口：`/keys` → 创建或编辑 → **API Key 模型映射**。复用渠道的可视化/JSON 编辑器，随 Key 表单一起保存；空配置保持原行为，清空所有规则关闭映射。

例如 Key 保存 `{"my-gpt":"gpt-4o"}`，客户端仍提交 `model: "my-gpt"`。服务端先选择平台模型 `gpt-4o`，再按该模型选择资源、校验 Key 模型限制和计算费用。映射不授予额外模型、分组或资金来源权限。

有两级映射时：**请求 A → Key 目标 B → 渠道上游名称 C**。费用按平台模型 B 的原有定价与结算规则计算；管理员渠道映射仍承担上游命名转换。不会按源别名 A 或供应商响应声称的模型重新定价。钱包、订阅与免费挂靠各自沿用原资金合同。

规则精确区分大小写，支持链式映射，自映射不改变请求；循环、重复来源、空名称、空白/控制字符、非字符串值拒绝保存。最多 256 项、配置 65535 字节、单个名称 256 字节。不自动尝试 reasoning 后缀或名称前缀匹配。

## Codex 无感切换验收（2026-11-04 06:40:30 +00:00 明确需求）

用户要求的平台能力不止模型名称替换：Codex 保持请求 `gpt-6.1-terra`，仅创建/编辑 Key 映射为 `kimi-k3` 即可完成任务，不要求用户修改客户端参数、删除工具或手动处理每次兼容报错。沿用此前新任务开始前选择目标的范围，要求同一目标下的后续工具回合连续可用；不把私有状态的任意跨供应商迁移当作已实现能力。

以下是验收标准，不代表当前均已实现：

| 环节 | 通过条件 | 当前证据与缺口 |
| --- | --- | --- |
| 配置与路由 | 可视化/JSON 保存；A→B→渠道 C，未命中不变 | 既有实现与回归覆盖 |
| 权限和计费 | 按平台 B 授权、报价、扣额；渠道 C 不改变计费身份 | 钱包/订阅/挂靠定向回归通过 |
| 日志 | 请求 A、目标 B、计费 B 分别可查，映射标记保留 | 既有持久快照与详情组件覆盖 |
| 参数兼容 | 平台处理源模型与目标的参数差异，用户无需改 Codex | 已知 OpenCode Kimi 拒绝 reasoning.context；未完成兼容验收 |
| 工具和搜索 | 函数、custom/exec、动态工具及实际搜索可执行，返回类型/引用符合客户端合同 | 本机透传测试通过；真实上游完整能力未验证 |
| 流式与续轮 | 工具只执行一次、正确终态、结果关联和下一轮继续工作 | 本机模拟覆盖，真实 Codex 多轮仍待验收 |

实施边界：New API 保持唯一的 A/B 业务身份、权限、资金和日志实现；最终供应商的协议/参数差异在执行适配处处理，不能两层各维护一份转换规则。优先使用上游 Responses 能力；必要的兼容操作应基于验证过的目标能力，并留下诊断。不得以静默删搜索、删 custom 声明、伪造完成事件、切到未配置的其他模型，或改变计费模型冒充无感切换。

验收使用合成任务覆盖参数、工具、搜索、流式和续轮，并核对真实计费/日志链路；HTTP 200、单次普通文本回复或模拟服务成功均不够。2026-11-04 本轮重新运行 `TestTokenModelMappingProtocolBodies` 与 `TestResourceRelayTokenModelMapping` 通过，但它们只证明已有路由/资金合同。两次经线上 GPT-Load 的最小 Kimi 对照（无 reasoning、仅 effort/summary）均被上游 `403 / error code: 1010` 拒绝，尚未进入有效的参数/工具验收；不能推断删除 context 已解决问题。本轮没有修改线上配置或实现未验证的参数过滤。 2026-11-04 06:45:51 +00:00 补充：按 Cloudflare 官方建议将测试脚本默认 Python UA 改为真实应用标识 `Oak-Compatibility-Check/1.0` 后，两类最小请求均返回 200/completed，说明该测试阻碍已排除；完整工具与搜索仍待验证。


### 2026-11-04 06:55:27 +00:00 真实供应商补测

通过现有 GPT-Load 数据面路径完成 19 次 Kimi 合成请求。普通函数及续轮、动态工具、custom/namespace、流式和真实 Codex exec 文法三回合成功；未见本批工具完成重复，连续 exec 的 call_id 不同。client tool_search 能返回发现调用，发现结果续轮尚未验收。reasoning.context 稳定返回 400；强制指定工具也存在目标差异。web_search 的 auto/required 均返回 200，却没有搜索事件或引用，不能计为通过。

本轮未经过 New API 用户 Key 入口，不能当作实际 A→B→C 计费/权限/日志端到端新证据；未改业务代码或生产配置。完整无感切换仍待参数兼容、真实搜索及客户端任务验收。详细矩阵与可追溯执行 ID 见 [GPT-Load 排障记录](../../../gpt-load/docs/memory/lessons.md)。


### 2026-11-04 09:09:16 +00:00 客户端配置与平台热更新的对照

用户实际成功路径是修改 Codex config.toml 的 model，平台目标是免去这一步，让 Key 映射变更对后续请求生效。Caddy 和两层日志确认，直接配置 DeepSeek/Kimi 的成功请求不带 reasoning.context，工具形状也与 GPT 配置不同；旧 GPT→Kimi 失败请求没有搜索声明，直接错误来自 context。服务端只能适配已收到的请求，必须保留客户端真实声明的工具合同；不能假装改变了客户端自身的工具配置，也不能保证已在途请求被热切换。

当前最小修复与验收应先聚焦目标参数兼容及后续请求选模，再独立验证真实搜索。搜索声明被接受不等于搜索已执行；现有新旧 OpenCode 推理入口未返回搜索事件，现有凭据调用官方搜索接口为 403，尚未解决。详细追溯见 [GPT-Load 排障记录](../../../gpt-load/docs/memory/lessons.md)。

### 2026-11-04 09:27:48 +00:00 本地参数修复与热更新回归

GPT-Load 已增加 kimi-k3 / glm-5.3 的 context=all_turns 归一化，不改变 Responses 工具和历史；DeepSeek V4 Pro 真实接受 context，保留原值。规则已接入普通/流式请求的公共准备路径并记录适配事件。本地尚未发布，不代表线上用户立即生效。

New API `TestTokenModelMappingResponsesHotUpdate` 使用相同 Key 和相同客户端 Responses 流式正文，更新目标 Kimi→GLM 后验证下一请求的新目标、继续应用渠道映射、按 B 的不同价格扣钱包及日志 A/B/计费 B。SQLite 3.50.4 及既有管理/权限/缓存定向回归通过。未改变 New API 业务实现、数据库或客户端配置；本轮没有重新执行完整三库矩阵。搜索、任意供应商私有状态迁移不在已完成范围。

## 协议范围

- HTTP 的 `application/json` 顶层 `model`：Chat、Responses、Claude Messages、Embedding、图像等现有入口。
- 表单及 multipart 的显式 `model`：保留原文本字段和二进制文件，重复模型字段拒绝。
- Gemini `/v1beta/models/{model}:{action}`：替换模型段，保留动作；目标不得含路径分隔符或动作冒号。
- Realtime 的 `?model=`；Responses WebSocket 的每个 `response.create`。后者每次重新认证、读取配置，已有连接的目标模型锁保持有效。
- 宿主任务协议在插件认领前应用；原生插件仅在声明顶层模型列表的 `submit` 路由应用。

不改写任务查询、remix 原任务身份、Midjourney 动作推导模型、固定 DeepSeek tools 价格模型及插件私有的嵌套/动作模型。省略 `model` 时保留原协议默认值，不隐式映射默认模型。不扩展模型目录来发布自定义别名。

## 日志与数据

使用日志列表显示客户端源名及现有映射标记，详情新增独立的 Key 映射区域，包含原请求、目标与计费模型。渠道映射、上游响应观测独立保留。`log.model_name` 继续是平台计费模型，统计和模型筛选按该字段执行。

公开元数据使用 `other.token_model_mapping = {requested_model, target_model}`。集中消费/错误日志入口附加；任务 `Properties` 冻结同一源/目标，后台结算和退款不查询后来修改的 Key。历史日志没有此字段则维持原展示。

Token 新增普通 TEXT 列 `model_mapping`，启动自动迁移；旧记录为空。管理 API 复用本人 Token 的创建/更新接口，输入为 JSON 对象的字符串。更新省略或 JSON null 保留，显式 `""` 清空；`status_only` 不修改映射。原事务、缓存失效栅栏及审计字段变更列表继续复用，不新建缓存。

Redis 令牌哈希必须保存 `ModelMapping`（包括空字符串）。旧哈希缺少该字段时回源读取数据库，并在原哈希中仅补齐缺失映射，保留实时额度；编辑栅栏期间禁止补缺。测试必须包含栅栏到期后的热缓存请求，仅验证保存后立即请求不足以覆盖该路径。相关本地修复与验证见[排障记录](../memory/lessons.md#2026-11-03-162813-0000--key-映射必须覆盖-redis-热缓存字段表)。

## 模块导航

- `common/token_model_mapping.go`：保存/运行共用的规则校验及链解析。
- `middleware/token_model_mapping.go`：认证后、资源与渠道选择前，改写真实正文或 URL；统一正文替换也供原插件改名复用。
- `controller/token.go`、`model/token.go`：配置保存及省略/清空语义。
- `model/token_cache.go`：热缓存映射字段、旧哈希补缺与编辑栅栏。
- `relay/responses_websocket.go`：归一化事件正文与 typed DTO 同步。
- `model/log*.go`、`model/task.go`、`service/task_billing.go`：同步和异步日志追踪。
- `web/src/features/keys/`、`web/src/features/usage-logs/`：Key 表单与日志展示。

## 验证与发布边界

本地已验证：管理 API 创建/更新/清除/省略保留/归属拒绝/坏规则拒绝；真实模拟上游 A→B→C、B 单价收费、免费账号零收费、目标权限拒绝；JSON、表单、二进制 multipart、Gemini、Realtime 参数；真实 Responses WebSocket 两次不同规则、Redis 刷新、连接复用、逐请求账单及日志；任务属性与后台日志快照。

真实 SQLite 3.50.4、MySQL 8.4.2、PostgreSQL 14.17 的新表、代表性旧表升级、重复迁移、原 Key 唯一索引、配置保留及任务属性读写通过。MySQL/PostgreSQL 另执行钱包和免费挂靠实际请求测试。未使用生产数据库或真实供应商额度。

复现命令（外部 DSN 必须是隔离的本机测试实例）：

```sh
go test ./common ./middleware ./controller ./model ./relay ./router ./service
go test ./controller -run '^TestTokenModelMappingDatabaseMatrix$' -v -count=1
TEST_RESOURCE_DB_DIALECT=mysql go test ./controller -run '^TestResourceRelayTokenModelMapping$' -v -count=1
TEST_RESOURCE_DB_DIALECT=postgres go test ./controller -run '^TestResourceRelayTokenModelMapping$' -v -count=1
cd web
bun x --no-install tsgo -b
bun run build
```

三库矩阵读取 `TEST_MYSQL_DSN` / `TEST_POSTGRES_DSN`；未设置时对应测试显式跳过。当前本机 Bun 不支持仓库脚本中的 `bun check -b`，改用已安装的 `tsgo -b` 完成类型检查，未改动依赖或全局工具版本。

参考 OWASP [Authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html) 与 [Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html) 的凭据及访问控制边界：本人归属、服务端目标授权、编辑后缓存失效、审计不记录凭据均已定向验证；本次没有更换认证机制，不作整站 ASVS 合规认证声明。

本地后端在 `build/local-preview/` 独立环境启动，预览库升级前备份为 `preview-before-key-model-mapping.db`。前端 `http://localhost:5173/keys` 已可查看。前后端需配套发布；本次未提交、推送或部署线上。

前端验证：Key 编辑、共享映射编辑器、日志详情/徽标/移动卡等 12 文件共 326 项，325 项通过；原有 `task log prices use localized unit labels from pricing metadata` 在整文件/整组运行时出现 20 秒超时，单独运行通过，未将这项计为整组通过。本次新增的映射编辑与日志展示用例通过；没有为规避超时而删测试或改生产逻辑。所改文件 Lint、`tsgo -b` 和生产构建通过。

## GPT 搜索编排（2026-11-04 10:20:19 +00:00，本地已实现）

用户批准在目标模型需要联网时复用 GPT 的真实 `web_search`。本节更新前述“搜索尚未实现”的现状，保留之前排障记录；不承诺任意供应商私有状态迁移或已在途请求切换。

### 配置及使用

- 入口：**系统设置 → 模型设置 → 联网搜索适配**。分别给 Kimi K3、GLM 5.3 填写搜索执行模型，留空关闭。默认全关闭。
- 系统选项：`WebSearchBridgeRoutes`，示例 `{"kimi-k3":"gpt-6.1-sol","glm-5.3":"gpt-6.1-sol"}`。模型名为平台模型，渠道映射仍照常执行；搜索模型不再次经过用户 Key 模型映射。
- 搜索模型必须只路由到真正支持托管搜索的上游。现场 `gpt-6-luna` 同时存在 Codex/OpenCode Go 两个组，实际发生过随机落入 Go 返回 400；当前 `gpt-6.1-sol` 仅在 Codex 组，Kimi 合成链路已实际验证。不能仅凭 GPT 名称判断能力。
- Key 需同时允许目标模型和搜索模型，沿用它的分组、资金来源、钱包/订阅额度与模型限流。没有钱包授权不追加钱包；权限不足明确失败，不隐含扩权。
- 搜索调用单价继续由**计费设置 → 工具价格**中的 `web_search` / `web_search_preview` 配置，单位沿用现有 `$/1000 次`，模型覆盖规则按搜索执行模型生效；零价仍是显式免费。
- OpenCode Go 仍需要会话头。当前线上 New API 渠道已有 header override `{"*":true}`；部署或新建渠道时需要保持必要会话头可传递。平台为搜索子请求生成独立会话，主任务会话保持原样。

### 实际请求与账单

1. 客户端请求 A，Key 映射到 B；只对配置目标且声明搜索工具的 HTTP `POST /v1/responses` 请求接管。
2. B 收到内部普通函数并决定是否搜索。没有搜索需求只执行这一轮，不无条件调用 GPT。
3. B 发起查询后，New API 用同一个用户 Key 重新走正常鉴权、路由、原子预扣与结算，以配置的 GPT 执行真实 `web_search`。仅传查询及搜索选项，不传整个用户会话。
4. 必须获得真实完成的 `web_search_call`。资料摘要和引用作为工具结果交还 B；客户端看到真实搜索项及标注 `commentary` 的 GPT 资料摘要，最终由 B 回答。普通/custom/动态发现工具仍交给客户端执行。
5. 同一轮同时出现搜索和客户端工具时，完成搜索后归还客户端；不替用户执行命令。下一请求从公开历史恢复资料，不依赖服务进程缓存。

每次 B 调用按 B 收费，GPT Tokens 按 GPT 收费，真实搜索调用附加费只出现在 GPT 账单；外层编排不重复结算。使用日志详情新增父请求 ID 和执行阶段，目标阶段保留 A→B 映射标签，GPT 阶段不冒用该标签。HTTP 总响应 `usage` 为 B 各轮的标准令牌统计，`oak_search_usage` 单独列出各 GPT 用量；这些输出字段不再进入扣费链。真实工具计数仍由原有 Responses 累计器处理，避免终态和工具完成事件重复计费。

### 输出与边界

- 第一版采用**阶段缓冲 SSE**：每阶段完成后发送真实事件，等待期间每 10 秒发送心跳；不是逐 token 实时透传。外层响应 ID、输出索引和序号统一，只有一个终态。JSON 客户端获得同一份合并结果。
- 公开事件隐藏内部函数。真实 item/call ID、普通工具参数、引用和输出内容保留；供应商只发终态时补齐协议封装，不编造工具成功。
- 每个客户端请求最多自动发起 4 次 GPT 搜索请求；查询最多 8 KiB，子响应最多 8 MiB，内部历史最多 32 MiB。断连取消后续阶段。已执行用量仍由原链路结算；失败不伪装成未执行或全额退款。
- 平台不能保证供应商内部搜索次数或 Tokens 的绝对上限。搜索请求携带 `max_output_tokens=2048`，但当前 Codex 上游可能忽略该参数；钱包及 Key 额度继续使用平台原有结算规则。
- 不默认注入 `max_tool_calls`：现场 Codex 上游返回 `Unsupported parameter: max_tool_calls`。客户端明确传入时保留约束并按实际兼容性返回错误，不静默删除。
- 保留域名过滤、用户位置、离线搜索等工具选项并交给 GPT；上游不支持时明确失败。
- 同步显式历史为当前支持范围；`store=true`、`background=true`、`previous_response_id` 和复杂 `allowed_tools` 选择明确拒绝。WebSocket 不接入本次搜索编排。任意第三方工具、加密历史和跨供应商私有状态不在本轮保证范围。

### 实现、验证与发布

入口 `router/relay-router.go`；编排及私有子路由 `controller/search_bridge.go`；工具循环、历史与公开输出 `controller/search_bridge_loop.go`；配置校验 `setting/operation_setting/search_bridge.go`。复用原始 TokenAuth / ResourceRelay / Distribute / Relay；不新增数据库表、依赖或第二套价格计算。

测试集中于 `controller/search_bridge_test.go`：真实 SQLite 钱包与 Key、无资源策略旧入口、权限/余额/空来源、假成功/供应商失败、循环上限、并行客户端工具、JSON/SSE、取消和索引合同。外部只以本地 HTTP 服务替代供应商。既有 Key 热更新、渠道映射和价格回归一起验证。

真实验收通过临时仅回环代理，让本地 New API 测试数据库与 Key 调用线上 GPT-Load；只使用合成 Bun 版本查询，未重放用户任务。Kimi→GPT 搜索→Kimi 和公开历史下一轮通过，GPT 返回真实引用，正常子账单产生；GLM 改用固定 Codex 的 `gpt-6.1-sol` 后，同类链路与公开历史下一轮再次通过。生产配置和服务未替换，未提交或推送。真实 Codex 应用完整操作仍需发布后验收，不把合成链路当作客户端全功能保证。

安全边界参考 OWASP [Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html)、[Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)、[Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)：每个子请求重验权限、默认不扩大来源、只记录非敏感关联身份；不改登录或会话认证机制，不作完整 ASVS 合规声明。Responses 搜索输出依据 [OpenAI 官方文档](https://platform.openai.com/docs/guides/tools-web-search)。

最终本地验证（2026-11-04 10:28:44 +00:00）：映射/权限/缓存定向后端回归、搜索集成与取消场景的 race 检查、前端配置与日志 35 项回归、`bun x tsgo -b`、改动文件 oxlint、前端生产构建通过。Go 构建依赖 `web/dist`，必须先完成前端构建再构建后端；不能并行让前端清空嵌入目录。当前本机 Bun 1.3.14 没有 `bun check`，使用项目已安装的 tsgo，未升级工具链。数据库沿用 SQLite 3.50.4 实测，无新增表或查询语义修改，未重新执行 MySQL/PostgreSQL 矩阵。临时回环探测代理及 SSH 隧道已停止。
