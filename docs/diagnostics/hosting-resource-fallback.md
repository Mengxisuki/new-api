# 挂靠额度耗尽后的顺延与失败日志

更新时间：2026-11-05 18:46:51 +00:00。修复已在本地验证，未提交、推送或部署。

## 线上证据与根因

wdm 核对版本：New API `oak-1ea2b201e07c-42083430069-1`、GPT-Load `oak-255abf88459d-42005704315-1`，与本地修复前 HEAD 一致。本轮只读检查生产日志、配置和数据库，没有重放用户任务。

- 2026-11-05 17:59:40 UTC，GPT-Load 请求 `5e1351ad-af38-4dcb-9939-c83eff989c79` 的 Codex 上游返回 429，摘要为 `The usage limit has been reached`。唯一尝试为 `maybe_sent`、`committed=0`，模型进入额度冷却；观测的主窗口为 `remaining=0/state=exhausted`。
- 同时刻 New API 请求 `202611051759393875088228268d9d6ATunl4xb` 返回 502；对应时间窗口的 New API 请求日志表没有该失败记录。
- 后续截至 18:06:33 UTC，挂靠内部 Key 的连续失败均为 `429/upstream_rate_limited`、`attempt_count=0`。例如 `2c5f57d0-47ad-4d94-8779-225794dfdb4d` 没有任何执行尝试。

`hostingQuotaExhausted` 只采纳一分钟内的明确耗尽观测，因此首次原厂拒绝、随后快照过期时，都要进入执行层。GPT-Load 已掌握“原厂明确拒绝处理”或“冷却导致根本未发送”的证据，但原数据面没有把该证据传给 New API。`relayHostingResource` 对所有非 2xx 一律返回终止错误，导致无法顺延；而失败返回发生在免费消费日志写入之前，也不经过平台 `ProcessChannelError`，所以 New API 看不到该失败日志。

## 修复合同

GPT-Load 在整个请求的每次尝试都满足以下条件时，才可能给最终未提交的 HTTP 429 增加 `X-GPTLoad-Replay-Safety: rejected_before_processing`：未发送，或执行器明确标记为处理前拒绝；任何已提交、可能处理且无拒绝证据的尝试都会永久撤销本请求的安全资格。全部候选冷却、零次发送也符合条件。入站及原厂同名头不能作为证据，原厂同名响应头由既有保留头过滤器剥离。普通 429 文案不授予资格。

New API 只在其固定回环挂靠执行端返回 HTTP 429、唯一且精确匹配上述标记、下游尚未输出时，跳过当前账号并继续当前 Key 已授权的后续来源。跳过分类为 `account_unavailable`，不把临时限流、模型容量拒绝一概谎报成账号额度耗尽。旧版 GPT-Load、缺失或重复标记、5xx、超时与断流仍明确终止；不自动添加钱包，也不消费原厂重置机会。

挂靠失败通过既有 `RecordErrorLog` 保存稳定错误码、HTTP 状态、账号引用、策略版本和安全顺延结果，并以原用户 request_id 关联。GPT-Load 响应的 `X-GPTLoad-Request-ID` 写入 `upstream_request_id`，下一来源使用独立上下文。日志不保存内部 Key、原厂错误正文或伪造 token 用量；是否写失败日志继续遵守现有 `ErrorLogEnabled`。流式请求的 JSON 拒绝也保留原 `is_stream`。

## 验证

新增回归在旧实现上先失败，再在修复后通过：

- New API `TestResourceRelaySafeRejectionFallbackAndLog`：下一账号、订阅、钱包、Responses 流式、未授权后续来源、未知/重复安全证据及 5xx；核对真实日志、请求关联与单次结算。
- GPT-Load `TestResourceFallbackReplayEvidence`：HTTP/SSE 明确拒绝、普通未知 429、前序未知后序拒绝，以及入站/原厂伪造标记；既有冷却用例补充零次发送标记断言。
- 真实数据库定向矩阵：SQLite 3.50.4、MySQL 8.4.2、PostgreSQL 14.17。使用独立本地测试实例和随机测试数据库，不连接生产数据库写入。
- `go test ./controller ./middleware ./service ./service/hosting -count=1`；GPT-Load `go test ./internal/gateway ./internal/execution/cpa -count=1`；两边受影响包 `go vet` 与根程序构建通过。

定向复验命令：

```sh
# new-api；外部数据库使用 TEST_RESOURCE_DB_DIALECT 及既有 TEST_MYSQL_DSN / TEST_POSTGRES_DSN。
go test ./controller -run '^TestResourceRelay(SafeRejectionFallbackAndLog|ExhaustionAndNoReplay|PlatformFallbackAndNoHiddenWallet|PaidFailureBoundary|StreamAndOwnership|StaleObservationDoesNotSkip)$' -count=1
# gpt-load
go test ./internal/gateway -run 'TestResourceFallbackReplayEvidence|TestOnlyModelCooledCandidatesReturn429WithoutDispatch' -count=1
```

## 发布与验收

两服务均需发布：先更新 GPT-Load 生成证据，再更新 New API 识别证据；中间窗口旧 New API 仍按原规则停止。无数据库结构、依赖或生产配置变更。按用户规则，未经要求不提交/推送，生产写入需明确授权。

发布后需用专用合成请求验收当前已耗尽账号在前、订阅或钱包在后：确认最终成功、挂靠失败及后续成功按同一用户请求关联、执行层冷却请求为零次发送、平台只结算一次。若挂靠额度已恢复，不人为耗尽账号或修改真实余额来制造场景。只有本地模拟和数据库验证，尚不能声称线上已修复。

## 2026-11-11 07:48:47 +00:00 — 安全顺延尝试并入最终请求日志（本地完成）

本节替代前文“安全拒绝也单独写失败日志”的记录规则；可信顺延条件保持不变。只读核对 wdm 的 New API 版本为 `oak-117f513a7227-42784492939-1`，生产查询没有写入、重放任务或调用原厂测试。

请求 `202611110712299387973438268d9d6oRfNN6BA` 在北京时间 15:12:30 产生错误日志 13004，挂靠账号 2 返回 429，携带安全顺延资格；执行层关联 ID 为 `b4f3ae6e-cbbe-476e-8ecb-8fe3e09d930f`。15:13:32 的日志 13005 为订阅来源、渠道 2 成功。两条记录的客户端 request_id 相同；GPT-Load 此次记录为 `upstream_rate_limited`、`attempt_count=0`，没有执行尝试。该证据只能确认未发送及限流/冷却拒绝，不能单凭 429 指定具体额度窗口耗尽。

根因是 New API 在安全拒绝时立即调用 `RecordErrorLog`，最终成功又写消费日志。日志条目数并不等于客户端请求数，失败记录无费用，也不调用累计请求数更新。本地改为将安全拒绝按顺序写入最终日志 `resource_attempts`，字段为本地账号 ID、HTTP 状态及执行层 ID；后续来源的主 `upstream_request_id` 不被覆盖。全部来源不可用、后续平台预检失败或终止拒绝仍保留最终失败。普通 429、重复/未知安全头、5xx 等仍不获重放资格。

使用面板复用 `ResourceSourceDetails` 展示“前序资源尝试（属于本次请求）”；旧 `hosting_execution_rejected` 根据状态及已保存的安全证据解释，所有七种语言已补齐。历史日志不迁移、删除或隐藏；新规则仅对发布后的请求生效。账号身份继续复用按日志所有者过滤的现有投影，原厂正文和凭据不进入日志。

验证：原回归先稳定复现成功时两行日志，再转为通过；定向用例覆盖账号、钱包、订阅、Responses 流式、全部账号拒绝、平台预检失败及安全证据边界，校验单条最终日志、同一客户端 ID、尝试累积、平台单次计数与结算。SQLite 3.50.4、MySQL 8.4.2、PostgreSQL 14.17 的真实隔离库通过。命令仍为上方 `TestResourceRelaySafeRejectionFallbackAndLog`，外部分别配置 `TEST_RESOURCE_DB_DIALECT=mysql|postgres` 和回环测试 DSN。相关资源回归、定向 race、controller/service 的 vet、服务构建、前端 27 项回归、类型检查、定向 lint 与生产构建通过。

此轮只需发布 New API（含控制台）；GPT-Load 线上已具备顺延证据，本次无需修改。没有数据库结构、依赖或生产配置变更。未提交、推送或部署；发布后需用合成请求验收一条最终日志及可读尝试详情，不人为耗尽账号或修改真实余额。
