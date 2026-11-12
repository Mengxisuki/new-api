# New API 平台事件推送（SSE）接口文档

文档版本：1.1  
核对日期：2026-11-05（UTC）  
适用对象：开发外部通知客户端、业务服务或控制台接入的开发者。  
传输方式：HTTPS + Server-Sent Events（SSE）；处理确认使用 HTTP POST。无需 MQ SDK。

本文对应当前已实现的接口合同。上线地址、是否启用、消费者编号、授权 Token、订阅范围和保留期由平台管理员提供；不能仅凭本文假设某个线上环境已发布。本文件可单独交付，不要求接入方阅读项目源码。

快速导航：[连接与帧格式](#3-建立-sse-连接) · [业务字段](#4-业务消息字段) · [心跳与控制事件](#5-连接提示心跳与控制事件) · [ACK](#6-确认处理完成ack) · [错误与重试](#7-http-错误与重试) · [联调请求](#10-可复制联调请求)

## 1. 接入前准备

平台管理员在「系统设置 → 运维 → 事件推送」完成以下操作：

1. 为接收方创建消费者，选择事件主题和关注用户；用户过滤留空表示不限用户。
2. 为该消费者生成事件 Token，并设置授权主题及有效期。明文仅生成时显示一次。
3. 启用事件投递；新安装默认暂停投递。
4. 向接入方安全交付 `BASE_URL`、`EVENT_TOKEN`，并告知消费者编号、订阅范围和当前保留期。

`BASE_URL` 示例为 `https://api.example.com`，不带末尾 `/`。本文的编号、时间和业务值均为虚构示例。

### 1.1 消费者与 Token 的关系

- **不同消费者各收一份**匹配的事件，拥有独立确认进度。
- **同一消费者的多枚 Token 共享确认进度**。轮换 Token 不会生成一套新进度；不能把多 Token 当作多个独立订阅者。
- 同一消费者同时连接多个实例时，可能重复收到相同事件；当前没有竞争消费者的独占分配、消费租约或恰好一次执行保证。
- 实际可消费主题为「消费者当前主题 ∩ Token 授权主题」，再应用消费者当前关注用户范围。连接参数不能扩大这些范围。
- 当前上限：全平台 100 个消费者、每消费者 20 枚有效 Token、每消费者 8 个在线连接（所有 Token 及浏览器连接合计）。

### 1.2 鉴权

两个消费接口均使用相同请求头，前缀按下例书写：

```http
Authorization: Bearer evt_由平台提供的完整Token
```

事件 Token 当前为 `evt_` 前缀加 43 个随机字符，客户端应将它视为不可解析的完整字符串。它只能接收事件和 ACK，不能读取账户凭据、操作资金或管理消费者，也不能替代模型 API Key、个人访问令牌或网页登录凭据。

不得把 Token 放入 URL。请求中非空的 `token`、`access_token` 查询参数会被拒绝。过期、撤销、消费者停用、发行者账号停用/降权/认证版本失效，都会使授权不可用；没有供外部客户端自行续签的接口。

浏览器必须使用可携带 `Authorization` 的 Fetch SSE 实现；原生 `EventSource` 不支持自定义该请求头。当前事件路由没有单独开放跨域 CORS，浏览器接入采用同源部署或自有后端中转；不要假设其他域名的页面能直接携带此请求头访问。服务端程序及原生客户端不受浏览器 CORS 限制。

## 2. 接口总览与公共类型

| 方法 | 路径 | 作用 | 成功响应 |
| --- | --- | --- | --- |
| GET | `/api/event-hub/stream` | 持续接收匹配的事件 | HTTP 200，`text/event-stream` |
| POST | `/api/event-hub/ack` | 确认单条投递已经处理完成 | HTTP 200，JSON |

后台管理、Token 签发、订阅修改和人工补发由平台管理员操作，事件 Token 没有这些权限。客户端不需要调用管理接口。

公共类型约定：

| 类型 | 线上表示与含义 |
| --- | --- |
| `integer` | JSON 数字，整数值；除另有说明外为正数 |
| `int64` | JSON 数字，后端 64 位有符号整数；不是 JSON 字符串 |
| `number` | JSON 数字，可带小数 |
| `string` | UTF-8 字符串 |
| Unix 时间 | 秒级 UTC 时间戳，不是毫秒；`0` 的含义按具体字段说明 |
| quota | 平台内部计费额度；不是人民币分，也不是模型 Token 数 |

ID 在服务端按整数处理。JavaScript 客户端若需要处理超过 `Number.MAX_SAFE_INTEGER` 的 ID，应使用无损 JSON 整数解析；先 `JSON.parse` 再转 `BigInt` 无法恢复已经丢失的精度。时间戳显示时再乘 1000 转成 JavaScript 毫秒值。

客户端应允许响应增加未知字段，并对未知事件类型告警或进入人工处理队列；不能把未知类型默认为已成功处理。

## 3. 建立 SSE 连接

### 3.1 请求

```http
GET /api/event-hub/stream HTTP/1.1
Authorization: Bearer evt_由平台提供的完整Token
Accept: text/event-stream
```

无请求体。

| 位置 | 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- | --- |
| Header | `Authorization` | string | 是 | `Bearer ` 加事件 Token |
| Header | `Accept` | string | 建议 | `text/event-stream` |
| Query | `consumer_id` | 正整数的十进制文本 | 否 | 专用 Token 已绑定消费者，推荐省略；传入时必须与绑定编号完全一致，不带前导零，否则鉴权失败 |
| Header | `Last-Event-ID` | string | 否 | 可以不发送；服务端不会依据它跳过未 ACK 的投递 |

不支持通过 URL 的 `topics`、`user_id`、起始游标等参数修改订阅或跳过消息。不要依赖未在表中列出的查询参数产生业务效果。

### 3.2 成功响应头

```http
HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-cache, no-store
X-Accel-Buffering: no
```

HTTP 200 只代表流已建立，不代表已收到业务消息或完成消费。连接保持打开，不能等待整个 HTTP 响应结束后再解析。此响应不使用普通 API 的 `success/data` 外层封装。

### 3.3 SSE 帧格式

服务端使用 UTF-8 SSE 文本，每一帧以空行结束。一个网络读取块可能只有半帧，也可能包含多帧；必须按 SSE 帧边界解析，不能对每个 TCP/Fetch chunk 直接执行 `JSON.parse`。

首次连接发送：

```text
: connected
retry: 3000

```

`:` 开头的行是注释；`retry: 3000` 是建议重连间隔 **3000 毫秒**，不会替客户端执行重连。

正常业务帧：

```text
id: 123
event: wallet.topup_succeeded
data: {"id":123,"consumer_id":2,"event_id":89,"created_at":1793865600,"last_sent_at":1793865602,"attempts":1,"acked_at":0,"topic":"wallet.topup_succeeded","payload":{"user_id":7,"user_name":"example_user","trade_no":"EXAMPLE_TOPUP_001","paid_amount":"10.00","credited_quota":1000000,"payment_provider":"epay","payment_method":"wxpay"}}

```

上例的 `data:` 在线上是一行 JSON。后文为便于阅读采用缩进 JSON，不改变线上帧格式。

| SSE 字段 | 类型 | 业务帧必有 | 说明 |
| --- | --- | --- | --- |
| `id` | 文本形式的正整数 | 是 | 投递编号，等于 `data.id`；用它提交 ACK |
| `event` | string | 是 | 事件主题，等于 `data.topic`；也可为第 5 节的控制事件 |
| `data` | JSON 文本 | 是 | 完整业务信封或控制事件内容；按 `event` 判断形状 |
| `retry` | 十进制整数文本 | 否 | 重连建议，单位毫秒；当前出现在首次连接帧 |
| `:` 注释 | 文本 | 否 | 连接提示或心跳，不属于业务事件，不需要 ACK |

使用 SSE 库时要接收**命名事件**，不能仅监听默认 `message/onmessage` 而漏掉上述 `event` 名称。遵循标准 SSE 解析规则；若同一帧出现多行 `data:`，先按换行连接后再解析 JSON。

## 4. 业务消息字段

### 4.1 公共信封

三类业务消息的 `data` 均为以下对象；表中字段当前都会返回，不为 `null`。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | int64 | **投递编号**；确认时填入 `delivery_id` |
| `consumer_id` | integer | 当前消费者编号；可与本地预期配置核对 |
| `event_id` | int64 | **事件编号**；同一业务事件在不同消费者中相同，用于业务幂等去重 |
| `created_at` | int64，Unix 秒 | 原事件创建时间；正常分配/补发均沿用事件时间，不代表这次网络发送时间 |
| `last_sent_at` | int64，Unix 秒 | 当前发送尝试时间；不代表客户端已经成功收到或处理 |
| `attempts` | integer | 发送尝试次数，首发通常为 1；可能包含失败发送，并发连接下不能用它推断精确收到次数 |
| `acked_at` | int64，Unix 秒 | SSE 中待投递记录通常为 `0`（未确认）；这是发送时快照，ACK 后原帧不会更新，不能用它判断另一连接当前状态 |
| `topic` | string | 下表三个主题之一，与 SSE `event` 相同 |
| `payload` | object | 按主题定义的业务摘要，字段见 4.2–4.4 |

| `topic` | 含义 |
| --- | --- |
| `wallet.topup_succeeded` | 钱包充值订单实际到账 |
| `billing.charge_anomaly` | 一笔最终费用达到异常规则阈值 |
| `recharge.info_submitted` | 已付款代充值订单提交或补交资料 |

区分三个编号：`id` 用来确认投递，`event_id` 用来去重，`payload.order_id`（若存在）才是代充值业务订单编号。它们不能互换。公共信封**没有 `user_id` 字段**。

**用户名约定（文档 1.1 新增）：**三类事件的 `payload.user_name` 均为业务所属用户的账号名（平台 `username`，不是显示昵称 `display_name`）。它在事件创建时保存为快照，账号后续改名、断线重发或人工补发都不会改变旧事件的名称；异常费用事件取检测创建事件时可见的账号名。该字段只用于展示，不用于鉴权或幂等。新版生成的事件必有此字符串；升级前已保存的事件可能缺少，客户端应允许缺省，不能把缺省解释成空账号，也不能要求历史补发自动补齐。

### 4.2 钱包充值到账：`wallet.topup_succeeded`

触发于钱包充值订单真正增加额度的业务事务，包括支付渠道到账、已核实收款的人工补单及自助挂帐充值。**额度到账不总等于现金到账**：挂帐订单的 `payment_provider/payment_method` 都为 `credit`，`paid_amount` 固定为 `"0.00"`，另有 `receivable_amount` 表示尚未收取的人民币应收元。不是支付跳转成功提示，也不表示订阅购买、代充值订单付款或所有形式的钱包余额变更。

`payload` 字段：

| 字段 | 类型 | 必有 | 说明 |
| --- | --- | --- | --- |
| `user_id` | integer | 是 | 钱包所属平台用户编号 |
| `user_name` | string | 新事件是 | 钱包所属用户的账号名快照；历史兼容规则见 4.1 |
| `trade_no` | string | 是 | 平台充值交易号，便于与原账单核对 |
| `paid_amount` | string | 是 | 原订单金额的十进制文本，保留两位小数；挂帐固定 `"0.00"`，其他渠道语义见金额单位边界 |
| `receivable_amount` | string | 挂帐是 | `credit` 充值的应收人民币元，如 `"10.00"`；不计实收，其他渠道不返回此字段 |
| `credited_quota` | integer | 是 | 实际增加的钱包内部额度，不等于实付金额或模型 Token 数 |
| `payment_provider` | string | 是 | 支付提供方标识；历史数据可能为空，不用于鉴权 |
| `payment_method` | string | 是 | 原订单支付方式，如 `wxpay`、`alipay`、`stripe`；按开放字符串处理，历史数据可能为空 |

当前支付提供方包括 `epay`、`stripe`、`creem`、`waffo`、`waffo_pancake`、`official_alipay`、`official_wechat`。客户端应允许未知值展示为原始标识，不把该列表当作永不扩展的枚举。

```json
{
  "user_id": 7,
  "user_name": "example_user",
  "trade_no": "EXAMPLE_TOPUP_001",
  "paid_amount": "10.00",
  "credited_quota": 1000000,
  "payment_provider": "epay",
  "payment_method": "wxpay"
}
```

**金额单位边界：**载荷目前没有 `currency` 或汇率字段。不得将所有 `paid_amount` 一律显示为人民币；币种须由平台提供该支付配置的业务约定。额度与金额也可能受汇率、优惠、附加费影响，不能用一个字段反推另一个。未取得币种约定时，展示原始金额和渠道或展示额度，不擅自标注货币符号。

### 4.3 异常扣费：`billing.charge_anomaly`

基于已确认的整笔最终费用生成，不基于预扣额度、单次补扣差额或消费日志文字。同步请求在最终结算后检测；异步任务的提交估价保持待定，完成后才进入最终费用检测。

`payload` 字段：

| 字段 | 类型 | 必有 | 说明 |
| --- | --- | --- | --- |
| `user_id` | integer | 是 | 计费用户编号 |
| `user_name` | string | 新事件是 | 计费用户的账号名快照；历史兼容规则见 4.1 |
| `request_key` | string | 是 | 平台计费关联身份，当前形如 `request:<请求编号>` 或 `task:<平台任务编号>`；作为关联字符串保存，不是凭证，也不是 ACK 编号 |
| `model_name` | string | 是 | 实际计费模型名，可能与客户端请求别名不同 |
| `funding` | string | 是 | `wallet`：钱包；`subscription`：订阅 |
| `actual_quota` | integer | 是 | 触发告警时整笔最终费用，内部额度，正数 |
| `baseline_quota` | number | 是 | 使用历史规则时的中位数，可为小数；使用无历史高额规则时固定为 `0`，不表示历史费用真为零 |
| `threshold_quota` | number | 是 | 本次检测实际采用的阈值，内部额度，可为小数 |
| `sample_count` | integer | 是 | 本次可用历史样本数量，范围 0–100；不足最少样本数时仍返回实际数量 |
| `reason` | string | 是 | `historical_multiple` 或 `absolute_limit`，含义见下表 |

| `reason` | 判定 |
| --- | --- |
| `historical_multiple` | 样本数达到平台门槛，`threshold_quota = max(最低异常额度, 历史中位数 × 倍率)` |
| `absolute_limit` | 样本数不足门槛，`threshold_quota = 历史不足时的高额阈值`，`baseline_quota = 0` |

两类规则均在 `actual_quota >= threshold_quota` 时告警。历史按同一用户、实际计费模型和资金来源区分，取当前笔费用观察创建时间向前 7 天、观察编号早于当前笔的最多 100 笔已知正向最终费用；排除当前笔、待定任务和退款记录。具体阈值由管理员配置，客户端直接使用消息给出的基线、阈值和原因，不自行用实时配置重算旧消息。

```json
{
  "user_id": 7,
  "user_name": "example_user",
  "request_key": "request:EXAMPLE_REQUEST_001",
  "model_name": "example-billing-model",
  "funding": "wallet",
  "actual_quota": 3000000,
  "baseline_quota": 400000,
  "threshold_quota": 2500000,
  "sample_count": 25,
  "reason": "historical_multiple"
}
```

以下也是合法载荷中的规则字段组合：样本不足时 `sample_count = 3`、`baseline_quota = 0`、`reason = "absolute_limit"`。不能根据 `baseline_quota = 0` 做除法或展示「无限倍异常」。

告警保存触发时快照，同笔任务后续费用修订或退款不会撤回已经收到的告警；本接口不是实时余额账本，也不提供退款事件。

### 4.4 代充值资料提交：`recharge.info_submitted`

用户首次交资料，或按业务员要求补交资料，且原订单资料保存事务成功后生成。

新版 `payload` 包含以下六个字段：

| 字段 | 类型 | 必有 | 说明 |
| --- | --- | --- | --- |
| `order_id` | integer | 是 | 平台代充值订单编号 |
| `user_name` | string | 新事件是 | 订单买家的账号名快照，不是处理人名称；历史兼容规则见 4.1 |
| `order_version` | integer | 是 | 本次资料保存后递增的订单版本，不是投递尝试次数 |
| `status` | string | 是 | `pending`：资料已收、待领取；`processing`：已有处理人，继续办理 |
| `submission` | string | 是 | `initial`：首次提交；`resubmitted`：按要求补交 |
| `assignee_id` | integer | 是 | 当前处理人用户编号；`0` 表示尚未分配，正数表示已有处理人 |

```json
{
  "order_id": 301,
  "user_name": "example_user",
  "order_version": 4,
  "status": "processing",
  "submission": "resubmitted",
  "assignee_id": 18
}
```

此主题的载荷与公共信封均**不返回订单买家 `user_id`**，但新版载荷提供买家账号名 `user_name`。关注用户的筛选由服务端完成，不能从 `assignee_id` 推断买家。载荷不包含代充值资料中的邮箱、账户明文/密文、session JSON 或登录 Token。需要办理订单或读取敏感资料时，使用独立的已授权业务流程；事件 Token 不提供此能力。事件是当时快照，办理前须重新读取订单当前版本。

## 5. 连接提示、心跳与控制事件

以下内容不使用第 4 节业务信封，不携带可 ACK 的投递编号，**均不要调用 ACK**。

### 5.1 心跳

```text
: heartbeat

```

当前实现约每 15–16 秒检查并发送一次心跳，受调度及数据库耗时影响，不是严格秒表。客户端不要因短于该周期的空闲误判断线；读取超时可从 60 秒起按网络条件调整。心跳仅证明连接活动，不表示消费成功。

### 5.2 流内错误

流已返回 HTTP 200 后，故障无法再修改 HTTP 状态码，可能发送下列帧后断开：

```text
event: error
data: {"code":"delivery_paused"}

```

| 字段 | 类型 | 必有 | 说明 |
| --- | --- | --- | --- |
| `code` | string | 是 | 下表中的错误代码 |

| `code` | 含义与建议 |
| --- | --- |
| `authorization_revoked` | 后续鉴权检查未通过；可能是 Token/消费者/发行者失效。停止密集重连，核对授权；当前实现的鉴权链存储故障也可能表现为此代码，不能仅凭它断言一定有人撤销了 Token |
| `delivery_paused` | 平台暂停投递；保留本地处理状态，低频重试或等待管理员恢复 |
| `storage_unavailable` | 后续存储、投递分配或连接续租失败；关闭旧连接并退避重连，持续发生时联系平台 |

网络异常、进程退出或写入失败时，可能直接 EOF/断连而收不到 `error` 帧。客户端必须同时处理正常 EOF、网络错误和流内错误。

### 5.3 保留期缺口

```text
event: retention_gap
data: {"expired_unacked_count":3,"action":"reconcile_business_state"}

```

| 字段 | 类型 | 必有 | 说明 |
| --- | --- | --- | --- |
| `expired_unacked_count` | int64 | 是 | 此消费者历史累计「已过期且未确认」数量，包含过期前尚未物化为投递的匹配事件；不是本次新增数量，也不是当前剩余待处理数量 |
| `action` | string | 是 | 当前固定为 `reconcile_business_state`，要求从原业务状态核对缺失信息 |

累计值大于零时，每条新连接提示一次；同一连接中累计值变化时再次提示。重复收到同一累计值是预期行为，不能反复累加。当前没有用 ACK 清零该计数的接口。

此帧不会自动关闭流，随后仍可能发送业务事件。客户端应将消费者标记为「需要对账」，按接入业务约定处理后续通知，并通过原业务接口/平台管理员核对状态；仅靠事件 Token 无法重建已清理的消息。

## 6. 确认处理完成：ACK

### 6.1 请求

```http
POST /api/event-hub/ack HTTP/1.1
Authorization: Bearer evt_由平台提供的完整Token
Content-Type: application/json

{"delivery_id":123}
```

Query 的 `consumer_id` 规则与 GET 相同：专用 Token 推荐省略；网页登录方式见第 9 节。JSON 请求体读取上限为 16 KiB，客户端只发送下表字段。

| 字段 | 类型 | 必填 | 校验与含义 |
| --- | --- | --- | --- |
| `delivery_id` | int64 | 是 | 大于 0 的 JSON 整数；填写 SSE `id` / JSON `data.id`，不能填 `event_id`、订单编号、数组或最大已读编号 |

只有属于当前消费者、已经尝试发送且当前主题/关注用户范围仍允许的消息才可确认。不要求发起 ACK 的 Token 与接收该帧的 Token 相同，但它们必须属于同一消费者且当前 Token 有该主题权限。

### 6.2 成功响应

```json
{
  "success": true,
  "message": "",
  "data": null
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `success` | boolean | 成功时为 `true` |
| `message` | string | 成功时为空字符串 |
| `data` | null | 没有额外返回数据 |

HTTP 200 且 `success = true` 才视为确认成功。ACK 只记录客户端处理声明，不执行充值、扣费或代充值业务。

同一投递在**记录仍保留且当前权限仍允许**的条件下，重复 ACK 幂等成功。ACK 成功后不再主动重发，但其他连接已经取出的在途帧仍可能到达；客户端仍需去重。平台暂停投递时 ACK 接口仍可使用。记录已清理后可能返回 404，权限收缩后可能返回 401/403，不能假定任何时候重复 ACK 都成功。

## 7. HTTP 错误与重试

本节适用于 GET 建立流之前及 POST ACK。流已建立后的错误见第 5 节。

业务处理器的错误 JSON 示例：

```json
{
  "success": false,
  "message": "Event push access denied",
  "message_key": "Event push access denied"
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `success` | boolean | 失败为 `false` |
| `message` | string | 错误说明；不应靠翻译后的展示文本判断业务状态 |
| `message_key` | string | 当前业务处理器返回的英文消息键，见下表 |
| `message_params` | object，可选 | 公共错误结构可能携带参数；当前下表错误没有参数，字段缺省 |

| HTTP | 当前 `message_key` | 常见原因 | 客户端处理 |
| --- | --- | --- | --- |
| 400 | `Invalid event push configuration` | ACK JSON 无效、缺少编号、编号非正整数或无法解码 | 修正请求，不原样无限重试 |
| 401 | `Event push access denied` | 初始鉴权失败，包括 Token 不存在/过期/撤销、消费者失效、`consumer_id` 不匹配、URL 携带凭据；鉴权存储故障也可能统一返回此状态 | 核对地址和授权，持续失败联系平台；不要通过轮询猜测 Token |
| 403 | `Event push access denied` | 处理器再次鉴权失败，或 ACK 超出当前主题/用户范围 | 核对授权变更，不强行跳过确认 |
| 404 | `Event push resource not found` | ACK 编号不存在、不属于当前消费者，或事件已清理 | 核对编号；过期/缺失情况进入对账 |
| 409 | `Event push state changed. Refresh and try again.` | GET 达到消费者连接上限，或 ACK 指向从未尝试发送的投递 | 连接冲突先减少重复连接；ACK 冲突核对消息来源，不盲目 ACK |
| 429 | 无固定 JSON 体 | 平台全局 API 限流 | 优先遵守 `Retry-After` 秒数，再加退避；GET 与 ACK 都计入限流 |
| 500 | `Event push operation failed`（若由业务处理器返回） | 服务端内部或存储故障 | 保留未确认状态，退避重试并报告持续故障 |
| 503 | `Event push delivery is paused` | 建立 SSE 时平台投递开关关闭 | 等待恢复或低频重连 |

429 当前通常为空响应体；限流存储故障的 500 也可能没有 JSON，反向代理还可能返回自己的 502/503/504 页面。先检查 HTTP 状态与 `Content-Type`，不要对所有错误响应强行解析 JSON。不要把 HTTP 200 的 SSE 流内 `error` 当作业务成功。

## 8. 幂等、重连与保留期

### 8.1 推荐消费顺序

```text
收到完整业务帧
  → 校验信封、topic 和 payload
  → 以「部署标识 + consumer_id + event_id」查询本地持久处理记录
  → 未处理：完成业务处理，并持久提交处理结果/幂等记录
  → 已处理：跳过重复业务副作用
  → 使用本次帧的 id 调用 ACK
  → ACK 成功后继续；失败保留处理记录，按状态分类处理
```

业务副作用与本地幂等标记应尽量在同一事务完成；若调用第三方服务，应使用该服务的幂等能力。不能只在内存维护已处理集合，也不能先 ACK 再处理业务。多实例共享消费者时，还需共享持久幂等记录或等价的并发去重能力。

客户端业务失败时不要 ACK。没有单独 NACK、可见性延期或死信接口；服务端会按未确认状态继续尝试。应记录失败、限制本地重试并报警，不能为了清空积压而伪造成功。

### 8.2 重发与重连

- 未 ACK 的投递在上次**发送尝试**约 30 秒后重新具备投递资格；这不是业务处理超时，也不表示 30 秒内只会收到一份。
- 重连使用原消费者的有效 Token。服务端从持久未确认账本恢复，`Last-Event-ID` 不能推进确认，也不能代替 ACK。
- 不保证严格全局顺序。并发事务、重发和人工补发可能使较小编号晚到；不要丢弃「编号小于已见最大值」的事件。
- 普通网络故障可从约 3 秒开始指数退避并加抖动，例如上限 60 秒；这是客户端建议，不是服务端 SLA。429 优先遵守 `Retry-After`；授权失败按第 7 节处理。
- 当前后台及连接循环约每 2 秒推进有界批次，每条连接每轮最多发送 20 条业务消息；正常投递通常有数秒延迟，存在积压时可能更久。这不是固定吞吐保证。
- Token 轮换时使用同消费者的新 Token，保持幂等记录，验证新连接后由管理员撤销旧 Token。更换消费者会建立独立进度。

### 8.3 保留与人工补发

默认事件保留 7 天，平台可配置 1–365 天；以事件创建时间为准，重发或补发不会重新延长保留期。新消费者默认接收创建时间之后匹配的事件；历史通知需要管理员在保留期内明确补发。

投递是保留期内可恢复的**至少一次**机制，不提供无限离线恢复。服务暂停仅停止网络投递，事件仍会保存/检测/到期清理。到期清理未确认事件后会形成 `retention_gap`；已清理事件无法再补发。

人工补发只重新开放通知记录，同一事件的 `event_id` 和业务摘要保持不变；已有投递的 `id` 也会沿用。客户端看到已经处理的事件，应跳过重复副作用并再次 ACK。补发不表示要求再次充值、扣款或强制重复办理订单。

## 9. 平台控制台的网页登录方式

外部开发优先使用前文专用事件 Token。平台自己的网页登录接入也可使用已有短期访问 JWT：

```http
GET /api/event-hub/stream?consumer_id=2 HTTP/1.1
Authorization: Bearer 当前登录的访问JWT
```

ACK 同样必须携带 `?consumer_id=2`。服务端要求真实且仍有效的登录会话、当前启用的 Root 身份及匹配的认证版本；仅带 Cookie、个人访问令牌、模型 Key、普通管理员或业务员 JWT 都不能替代该条件。消费者发行者授权也必须仍有效。

JWT 到期或会话失效需要走现有登录/刷新流程后重新连接；本事件接口不会签发或刷新登录 JWT。不要把 Root 登录凭据交给第三方开发者。

## 10. 可复制联调请求

在客户端安全环境中准备 `EVENT_BASE_URL` 与 `EVENT_TOKEN`，不要把真实值提交到代码仓库或写入请求日志。

接收事件（`-N` 禁止 curl 输出缓冲）：

```sh
curl -N "$EVENT_BASE_URL/api/event-hub/stream" \
  -H "Authorization: Bearer $EVENT_TOKEN" \
  -H 'Accept: text/event-stream'
```

在另一个终端，对**已经成功处理**的帧逐条确认（将 `123` 替换成该帧的投递 `id`）：

```sh
curl --fail-with-body "$EVENT_BASE_URL/api/event-hub/ack" \
  -H "Authorization: Bearer $EVENT_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"delivery_id":123}'
```

浏览器或 Node.js Fetch 接入时，先验证 HTTP 200 和 `text/event-stream`，再使用 UTF-8 流式解码器与标准 SSE 解析器逐帧读取；不要调用 `response.json()` 或 `response.text()` 等待流结束。解析、重连、幂等处理、ACK 重试分别按本文实现，不需要引入 MQ 客户端。

## 11. 联调验收清单

使用独立的测试消费者，避免与正式业务共享确认进度。

| 场景 | 预期 |
| --- | --- |
| 有效 Token 连接 | HTTP 200，立即收到连接提示帧；无业务事件时能收到心跳 |
| 三种业务分别触发 | 主题、信封及对应 payload 字段符合第 4 节；不包含账户秘密 |
| 用户改名后重发旧事件 | `user_name` 保持原快照；新事件使用新账号名，代充值始终取买家名称 |
| 接收升级前的历史事件 | 允许缺少 `user_name`，其余解析、幂等及 ACK 规则不变 |
| 不 ACK 后重连 | 保留期内再次收到同一事件；不因 `Last-Event-ID` 被跳过 |
| 成功处理后 ACK，再次 ACK | 在记录仍存在且权限有效时均成功；业务副作用只执行一次 |
| 两个独立消费者订阅同一事件 | 各收到一份，确认其中一个不确认另一个 |
| 同消费者轮换 Token | 已确认进度保留，未确认事件可继续消费 |
| 传其他消费者编号或 ACK 他人投递 | 请求被拒绝，不能改变对方进度 |
| 撤销正在使用的 Token | 后续鉴权检查失败，旧连接结束，新连接被拒绝；具体断开时间受调度影响 |
| 平台暂停投递 | 新连接 503，已有连接以 `delivery_paused` 结束；ACK 仍可用 |
| 消费者暂停 | 授权检查失败；与平台总开关暂停的响应不同 |
| 过期消息形成缺口 | 收到 `retention_gap`，标记需要对账，不对此帧 ACK |
| 人工补发已处理事件 | 去重后再次 ACK，不重复执行资金或订单操作 |
| 429/网络中断/EOF | 保留本地处理记录并按规则退避，不创建无限并行连接 |

## 12. 当前业务与恢复边界

本接口传输业务摘要，不代替支付账单、账户余额或订单办理接口。事件不包含代充值账户资料；事件 Token 也没有敏感资料读取权限。

最终费用告警依赖请求抵达真实最终结算：进程在此之前退出时，不能保证已有最终费用观察。异步任务在写终态后、结算前退出，以及钱包数据库已提交但 Redis 确认未知时，平台保留未决标记供对账；模型 Key 预算更新失败仍需平台检查原错误日志。接入方不能用「没有收到告警」推断没有扣费，也不能仅凭告警或补发执行自动资金冲正。

当前载荷没有协议版本字段、币种字段或独立的退款/告警撤回主题；不提供外部消费者自行修改订阅、拉取已清理历史、批量 ACK、NACK 或死信队列的接口。本文文档版本不表示线上路径包含 `/v1`。

## 13. 维护核对位置

维护者更新接口时应同步本文。字段与行为依据以下现有实现核对：

- `router/event_hub.go`：对外路径与路由权限。
- `controller/event_hub.go`：鉴权、HTTP 状态、SSE 帧及 ACK。
- `model/event_hub_delivery.go`：信封字段、确认账本、重发、连接数及保留期。
- `model/event_hub.go`：统一写入 `payload.user_name` 快照，保留历史事件及幂等约束。
- `model/topup.go`：钱包到账载荷。
- `model/event_hub_billing.go`：异常费用载荷与规则。
- `model/account_recharge.go`：代充值资料提交载荷。
- `common/gin.go`、`middleware/rate-limit.go`：普通 JSON 响应、429 及 `Retry-After`。
