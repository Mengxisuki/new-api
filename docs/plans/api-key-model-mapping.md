# API Key 模型映射

## 使用与计费合同

入口：`/keys` → 创建或编辑 → **API Key 模型映射**。复用渠道的可视化/JSON 编辑器，随 Key 表单一起保存；空配置保持原行为，清空所有规则关闭映射。

例如 Key 保存 `{"my-gpt":"gpt-4o"}`，客户端仍提交 `model: "my-gpt"`。服务端先选择平台模型 `gpt-4o`，再按该模型选择资源、校验 Key 模型限制和计算费用。映射不授予额外模型、分组或资金来源权限。

有两级映射时：**请求 A → Key 目标 B → 渠道上游名称 C**。费用按平台模型 B 的原有定价与结算规则计算；管理员渠道映射仍承担上游命名转换。不会按源别名 A 或供应商响应声称的模型重新定价。钱包、订阅与免费挂靠各自沿用原资金合同。

规则精确区分大小写，支持链式映射，自映射不改变请求；循环、重复来源、空名称、空白/控制字符、非字符串值拒绝保存。最多 256 项、配置 65535 字节、单个名称 256 字节。不自动尝试 reasoning 后缀或名称前缀匹配。目标仍须支持当前请求协议和参数。

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

## 模块导航

- `common/token_model_mapping.go`：保存/运行共用的规则校验及链解析。
- `middleware/token_model_mapping.go`：认证后、资源与渠道选择前，改写真实正文或 URL；统一正文替换也供原插件改名复用。
- `controller/token.go`、`model/token.go`：配置保存及省略/清空语义。
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
