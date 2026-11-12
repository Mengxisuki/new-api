# cn 反向代理与 Codex TLS 连接故障

更新时间：2026-11-06 08:00:08 +00:00。

## 已验证结论

`https://cn.oak.qingshukeji.top/v1` 的 URL 格式和 Responses 路径正确。本机 Codex 在建立连接时失败，未进入 wdm 的模型请求处理；故障由 DMIT Xray 443 入站的 `tlsSettings.minVersion = "1.3"` 与本机 Codex 传输实现的兼容问题触发。此结论来自隔离前后对照，不推断所有平台或所有 Codex 版本均不支持 TLS 1.3。

当前链路：客户端 → DMIT Xray TLS/443 → 按 cn 域名回落到 `127.0.0.1:8081` → DMIT Caddy → wdm `oak.qingshukeji.top:443` → Caddy → New API `127.0.0.1:3000`。

## 证据与验证

- 本机客户端日志数据库 `/Users/mengxi/.codex/logs_2.sqlite` 中，2026-11-06 07:40–07:41 UTC 的 `codex_http_client::client` 记录：`POST https://cn.oak.qingshukeji.top/v1/responses`，`is_timeout=false is_connect=true`；重试日志为 `Connection failed: error sending request`。
- 检查 wdm 现存 Caddy 活动文件及 20 个归档时，按 `remote_ip=DMIT` 且 `X-Forwarded-Host=cn.oak.qingshukeji.top` 过滤，未发现用户 Codex Responses 请求；仅有本轮合成无效令牌请求到达。
- cn 的 DNS 指向 DMIT；证书域名匹配、链完整、有效期为 2026-11-03 至 2027-02-01。本机 Python TLS 1.3 与 curl 默认握手成功；首页 200、无令牌模型列表 401。
- `curl --tls-max 1.2 https://cn.oak.qingshukeji.top/v1/models` 握手失败，明确为 `tlsv1 alert protocol version`；同一命令访问原域名返回预期 401。
- 独立临时 CODEX_HOME、合成无效令牌、本机 `codex-cli 0.155.1` 复现生产 cn 入口连接失败。未使用用户令牌，不消耗模型额度。
- 在 DMIT 回环 `127.0.0.1:18443` 启动临时 Xray，沿用证书和同一个 Caddy 回落；唯一相关差异为最低 TLS 版本 1.2。Python 分别强制 TLS 1.2、1.3 均得到 401。
- 经临时 SSH 回环转发与限定目标域名的本地 CONNECT 代理，让同一 Codex CLI 连接隔离入口；证书仍由客户端校验。客户端得到 `401 Unauthorized: 无效的令牌`，关联 New API ID：`202611060754159297385618268d9d6YQ9DDDLd`。证明真实 Codex 请求已到 New API 认证层；未验证真实模型生成。临时进程、转发、候选测试文件均已清理。

## 修复候选与发布边界

候选：DMIT `/root/oak-tls12-fix-20261106T075454Z/candidate.json`，仅将 443 入站 `minVersion` 从 `1.3` 改为 `1.2`；仍允许 TLS 1.3。`xray run -test -config` 已返回 `Configuration OK`。

生产源文件：`/usr/local/etc/xray/config.json`；准备候选时 SHA-256 为 `69f0ed93c555593ace797716125828b28e69a35b1f3d8136e04546ea58eb230f`，摘要保存在候选目录 `source.sha256`。用户明确授权后，已核对原配置摘要、确认候选仅有该一处语义变化，保留 0640 与 root:xray 权限及属组，备份、原子替换并于 2026-11-06 07:57:41 UTC 重启 Xray。服务 active/running，PID 2900902，NRestarts=0。原配置备份为同目录 `config.before.json`；如需恢复，应先检查后续改动，不能直接覆盖已变化的生产配置。当前文件 SHA-256 为 `e3f178e25d0eb25a8f9b715a707e5a6e6f1acf10ecc3670821f43d8e8a3a2c63`。

本轮只修改 DMIT Xray 的最低 TLS 版本；Base URL、New API 业务代码、wdm 配置与客户端永久配置均未修改。

## 日志判读边界

DMIT 同时作为网络代理出口，wdm 日志的远端地址等于 DMIT 并不能证明访问的是 cn 反向代理。此前由 DMIT 出口直连原域名的 77 次 200、63 次 502、1 次 499 Codex 请求均缺少 cn 的转发 Host，不应用来判断本次 cn 故障。

2026-11-06 07:01 UTC 的 cn Python Chat 502 已进入 New API，执行层上游为 OpenCode Go DeepSeek Flash 的 429 `overloaded`，部分后续请求为本地冷却零发送；这是独立的容量故障，不是本次 Codex 的 TLS 连接失败。

## 正式修复验收（2026-11-06 08:00:08 +00:00）

- 正式 cn 443 入口分别强制 TLS 1.2、TLS 1.3，均完成证书校验并返回预期的未认证 401；cn 与原域名首页均为 200。
- 桌面应用自带 `codex-cli 0.167.0-alpha.3.2`，独立临时配置直指正式 cn 地址；合成无效令牌返回正常 401，New API 请求 ID 为 `202611060758305222448188268d9d6W8tgf752`，未再出现连接失败。
- 同一桌面应用自带 Codex CLI 使用用户既有 API Key、当前模型 `gpt-6.1-sol`，经 cn 正式入口执行一次仅要求回复 OK 的合成请求。客户端退出码 0，输出 `OK`，收到 `turn.completed`。使用量为输入 11235、输出 5 Token；此请求实际消耗额度，未重放用户任务或调用模型工具，密钥仅在子进程环境中使用。
- wdm Caddy 对应 `POST /v1/responses` 返回 200，耗时约 2.56 秒；`remote_ip=179.253.238.122` 且 `X-Forwarded-Host=cn.oak.qingshukeji.top`，New API 请求 ID：`202611060759041417139878268d9d6fELnFHld`。
- 两站服务正常；wdm New API、GPT-Load、Caddy 均 active、NRestarts=0。临时 CODEX_HOME、隔离监听和 SSH 转发均已清理。

故障已修复并完成真实 Codex 生成验收；无需变更现有客户端 Base URL。该验收覆盖连接、认证与一次短文本生成，不扩大为完整工具、多模态或其他上游容量问题均已解决。
