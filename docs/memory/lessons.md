# 排障经验

## 2026-11-01 05:58:28 +00:00 — 动态状态漏译与选择器原始值是两类问题

现象：开票列表、详情显示英文Awaiting invoice，状态筛选收起后显示all，尽管All statuses已有中文词条。

根因：pending映射使用动态翻译键，但七语言共同遗漏Awaiting invoice；Base UI选择器另缺items选值/标签映射，SelectValue因而显示原始值。原财务测试只使用空英文资源，不能发现这两类中文界面问题。

修复：七语言补齐缺失键，五个动态状态键登记static-keys.ts；下拉内容与触发器共用渲染时翻译的items。既有admin.test.tsx增加真实资源测试，覆盖中文列表与详情、筛选收起显示、API仍提交状态码，以及挂载后切换语言；七语五状态完整性回归通过。本地浏览器也验证中文页面，未访问真实支付或财务数据。

适用边界：使用Base UI SelectValue默认标签解析的选择器，以及通过常量或配置间接调用t的文案。查漏译时同时检查词条、动态键登记和控件标签映射，不能仅补一个看似缺失的翻译键。

## 2026-10-28 08:48:21 +00:00 — 迁入旧接口时同步个人令牌路由契约

现象：恢复采购图片接口后，定向控制器测试通过，但全量后端测试 `TestAccessTokenRouteRulesCoverEveryDashboardRoute` 失败；上传、删除、公开读取各缺少一个路由归类声明。

根因：当前分支的受保护控制台接口还需要在 `middleware/access_token_routes.go` 声明个人访问令牌范围。仅复用 `RootAuth` 并不满足这套新增契约。公开资源也必须在路由覆盖测试的匿名资源集合中明确标识。

修复：采购图片 POST/DELETE 复用 `option:write`，仍由现有 `RootAuth` 要求超级管理员；公开 GET 保留匿名资源语义。新增 `TestProcurementQRCodeRouteAccessContract`，直接通过真实路由验证公开读取与匿名写入拒绝。失败测试转通过，干净源码的全量 `make test` 通过。

适用边界：迁入任何旧分支控制台路由时核对该静态路由契约，而非只编译 router。参考 [OWASP 授权指南](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html) 中的默认拒绝、最小权限和逐请求校验；本次沿用既有认证与会话机制，没有宣称完整 ASVS 合规。

## 2026-10-28 08:48:21 +00:00 — CI 复核使用受版本管理的源码与冻结依赖

现象：本机 `make test` 扫描了被 Git 忽略的旧 `dujiao-next-main/.../upload` 源码并失败；原前端 node_modules 的 DOMPurify/Vitest 版本也与锁文件不一致，全量测试出现三个失败。

规避：复制受版本管理源码和明确新增文件，排除忽略目录；用 Bun 1.4.4 冻结安装，再运行与工作流一致的检查。干净快照前端 205 文件/2468 用例、根 Go 模块及两个子模块全部通过。限制前端最大四个 worker，避免重型表单测试争抢运行资源。

适用边界：不删除用户的被忽略文件，不把脏环境失败直接归因于 CI，也不通过跳过测试让发布继续。旧依赖/目录与并行压力对三个原前端失败的各自贡献未逐项隔离，不应断言单一原因。

## 2026-10-29 10:25:56 +00:00 — 即时上传的设置图片不要混入普通表单草稿

现象：徽标上传成功后，保存站点名称，预览重新显示旧图片。单独的上传控件测试通过，整页回归 `logo-upload.test.tsx` 能稳定复现。

根因：图片由独立接口即时保存，但原先仍写入普通表单字段；设置 Hook 的已保存基线及父级配置快照尚是旧地址，普通保存后的 reset 可将其恢复。`setValue(...shouldDirty: false)` 或 `resetField` 不能同步 Hook 自己维护的全部基线。

修复：共享 `SettingsImageField` 持有服务端确认的图片预览状态，父级只提供已保存快照；文件不注册为普通 URL 文本字段，徽标不进入整页默认草稿。上传后失效系统设置与公开状态缓存，既保留上传结果，也沿用后续服务端快照更新。整页测试覆盖先改名称、上传新图、保存名称，并确认仅名称发出普通 PUT，新图继续展示。

适用边界：有独立持久化时机的设置项，尤其与 `useSettingsForm` 同页使用时。参考 `web/src/features/system-settings/components/settings-image-field.tsx` 及 `general/__tests__/logo-upload.test.tsx`；不推导为所有表单都需要额外状态。

## 2026-10-30 09:55:17 +00:00 — 支付窗口到期不能否定迟到的成功通知

现象：迁入的官方充值实现按回调到达时间检查十分钟窗口；`TestCompleteOfficialTopUpAcceptsDelayedVerifiedCallback` 可复现已成功付款却被拒绝入账。订阅限购也可能在原权益到期后拒绝此前创建的续费单。

根因：把用户能否继续发起付款的本地时限与网关确认收款的事实混为一谈；订阅则仅按回调时的活跃状态重新判断限购。

修复：继续支付过期校验不改动待支付订单状态；验签、金额和渠道匹配后的通知仍可幂等到账。订阅付款事务按已保存的订单创建时间确认当时持有的自然到期权益，只在本次套餐副本中豁免实例限购，不放开已取消权益。验证见 `model/topup_official_test.go` 和三库 `model/subscription_renewal_test.go`。

适用边界：通知可能晚于界面倒计时的异步网关。不表示可恢复已关闭、已替换或明确失效订单，也不以浏览器返回页代替服务端验签收款。

## 2026-10-30 11:58:20 +00:00 — 修正充值单位时同时追踪报价和到账

现象：CNY 输入 1 时，旧 /api/user/amount 仍返回 7.30；改展示换算不会改变付款价格。旧链路既按输入乘 Price 报价，也按 Amount×QuotaPerUnit 结算；前端还把输入再乘汇率展示。

根因：上一轮支付迁移保留了美元数量输入合同，未接上人民币面额输入。仅修改报价会让用户支付一元却拿到一美元额度，单独调整标签或默认汇率也无法解决。

修复：CNY 本地支付按面额报价，同时换成内部额度并冻结；新易支付回调和人工补单只用该冻结值，真实旧零冻结订单保留原语义。前端使用服务端到账字段展示公式并防止旧报价继续付款。旧报价在隔离源码快照上确认红灯，修复后控制器报价/下单回归和 SQLite 3.50.4、MySQL 8.0.46、PostgreSQL 16.15 的二十四个结算矩阵场景通过。

适用边界：只改变用户确认的人民币本地支付；不把展示汇率等同于所有网关售价，也不追溯重算已有订单和账户余额。

## 2026-10-31 20:16:00 +00:00 — 可重开订单与发票附加费共用原快照

现象：官方充值继续支付会创建新交易号，如果只复制金额和额度，新单可能收取原开票费却没有可办理发票申请；付款弹窗长期挂载时也不会因再次打开而刷新数据。

根因：交易号既是支付幂等身份也是发票唯一关联，替代订单属于新身份；弹窗是否可见和Hook首次挂载则是不同生命周期。

修复：替代付款单在同事务延续抬头和加价快照，原申请不再成为可办理记录，新单实际到账后只有一份待开票申请。订单读取以open驱动并关闭请求去重，关闭停止读取并使旧请求失效；重开实际发GET。后台费率精度也在提交前与服务端统一，防止逐项设置先成功一项后精度拒绝另一项。

验证：三库 controller/invoice_test.go 覆盖费率修改后继续支付仍按原6%、到账额度不变；前端 invoice-payment.test.tsx 覆盖每次打开与快捷配置，invoice-rates.test.tsx 覆盖0费率及超精度拒绝。

补充验证：新增开票订单不能只信任易支付签名，签名证明来源但不证明足额收款。充值通知、订阅通知及订阅回跳均对冻结的含附加费总额作十进制精确匹配；真实SDK签名回归验证100.00、105.999、106.001不能兑付106.00订单，106.00及重复通知正确。非开票旧合同保持原样。

## 2026-11-01 12:38:07 +00:00 — 配置金额折扣时充值套餐不能被默认金额选项截断

现象：管理员在支付网关配置金额折扣 `{"100": 0.98, "30000": 0.9}` 后，充值页只展示默认的 `10、20、50、100`，不在默认列表中的 `30000` 套餐丢失。

根因：`GetTopUpInfo` 始终返回默认 `AmountOptions`，而前端 `useTopupInfo` 与 `mergePresetAmounts` 仅遍历 `amount_options` 匹配折扣，忽略了仅在 `discount` 中配置的档位，且未配置折扣的默认档位仍被保留。

修复：当存在正数面额与正数折扣率的 `AmountDiscount` 配置时，后端 `controller/topup.go` 与前端 `web/src/features/wallet/lib/payment.ts`、`hooks/use-topup-info.ts` 统一按配置的折扣档位升序生成充值 Plan，不再返回默认的 `10/20/50/100`；仅当未配置任何有效充值折扣时才回退到 `amount_options` 或最低充值倍率档位。同时 `web/src/features/wallet/lib/format.ts` 消除折扣百分比计算的浮点尾差。验证见 `controller/topup_currency_test.go` 与 `web/src/features/wallet/__tests__/currency-payment.test.tsx`。

适用边界：仅影响用户端充值页预设套餐按钮与 `/api/user/topup/info` 的套餐输出；自定义金额输入、下单报价及后台设置持久化保持原契约。


## 2026-11-01 14:52:17 +00:00 — 来源回退必须辨别不足、陈旧观测与退款未知

现象：原订阅查询错误被转换为“没有套餐”；批量模式的旧退款返回成功时仅入队；补预留回滚响应丢失后再次全额退款可能重复返还。另有原厂重置后保留近期零额度快照但标记stale的真实执行层行为，仅按时间判断会跳过已恢复的免费账号并误用钱包。

根因：将业务不足、系统故障和持久化确认混为一谈，或只看额度数值忽略观测状态。统一来源需要更严格的跨尝试合同，不能直接套用旧单来源的异步回收方式。

修复：订阅不足使用明确哨兵；新策略预留、结算和释放同步落库并更新缓存；回滚未知冻结会话并停止，不能自动再次加额；耗尽预判同时要求fresh、近期、模型适用和未到重置时间。适用范围为已启用统一资源策略的请求，旧Key合同保留。回归覆盖真实数据库以及Redis/批量开关，详见 `service/resource_billing_test.go` 与 `controller/resource_relay_test.go`。

补充：结算已提交但确认返回失败，也必须冻结后续结算与退款；移除瞬时故障不能使未知结果自动变为未执行。`TestResourceBillingUnknownSettlementDoesNotRefund` 在真实数据库更新完成后注入确认错误，验证重复调用不会再次扣款或退还已消费金额。

## 2026-11-01 16:12:11 +00:00 — 挂靠配置错误不能靠补全权管理密钥解决

现象：旧线上 `POST /api/hosting/authorizations` 返回 `hosting_not_configured`。只读检查确认 New API 缺少旧实现要求的 gpt-load 管理凭据；回环服务地址和持久加密密钥不是该次故障根因。

根因：旧业务调用复用了执行层管理员认证，把用户挂靠权限与整个执行层管理权限耦合。直接补配置虽能越过配置检查，却扩大 New API 持有权限；用户明确拒绝这个方案，未执行生产写入或重启。

修复：移除管理凭据依赖，改成 New API 用户请求断言与执行层回查；页面增加本地配置状态和失败重试，在消耗敏感操作证明前检查配置。该状态只表示必需配置存在，不证明上游或执行层健康。普通管理Key或登录JWT必须在执行层本地拒绝，否则误调用会把本来不该共享的凭据传给核验服务。

适用边界：同机挂靠控制 API，不替代数据面每账号受限 Key，也不意味着执行层无法自行访问其持有的账号。上线必须同时升级两服务；不能再通过复制管理员凭据处理本故障。见 `controller/hosting_test.go` 请求断言/状态回归与 gpt-load `internal/control/hosting_test.go` 零核验转发测试。


## 2026-11-01 17:05:18 +00:00 — 翻译命名空间与 OAuth 状态摘要不能按字段存在推断

现象：挂靠页面大段英文，生成后的授权链接几秒内变成不可用。前一轮测试使用空英文资源，且模拟状态查询总带授权 URL，因而没有暴露真实行为。

根因：31 个新增词条误放在语言 JSON 根级，i18next 实际只读 `translation`；gpt-load 的浏览器 OAuth 初始响应返回链接，但状态 GET 不重复返回，前端直接用状态摘要替换初始投影，丢失了当前会话的可用链接。

修复：七语言词条归入实际命名空间，增加真实中文弹窗及语言切换回归。弹窗只在相同授权编号、等待授权且未提交回调期间复用首次已验证链接；提交、结束或重新开始后停用旧入口。初始页按 sub2api 固定展示生成链接、浏览器授权、输入链接/Code 三步，未生成不显示失败。纯 Code 只补充当前会话的 state/回调地址，后端仍验证会话、state、PKCE 与单次消费。

验证：`manual-authorization.test.tsx` 在旧资源上真实失败，`account-flow.test.tsx` 在旧组件和无 URL 状态响应下真实失败；修复后挂靠六文件 20 项测试通过，类型与 lint 通过。今后不能用“文件里存在词条”或比实际接口更完整的 mock 证明 UI 可用。当前只验证本地交互，不代表真实原厂授权成功。

## 2026-11-01 18:28:35 +00:00 — 资源策略不能替换原接口能力与草稿版本

现象：新 Key 如果默认启用旧统一调度，非文本请求会被白名单拒绝，平台模型也被客户端“必须挂靠实测”条件隐藏。根因是把挂靠执行器能力当成整支 Key 能力。修复：不支持挂靠的接口继续原平台适配器，资金工厂按 Key 明确授权预扣并冻结；目录单独提供 `platform_protocols`，不混用同名模型的其他来源协议。适用边界及定向三库、真实 HTTP/WS 和前端导出回归见实施记录第 15 节。

抽屉中的 revision 必须随 form.reset 冻结。若保存时直接取 query 最新 revision，后台刷新会让旧草稿携带新版本，导致乐观锁失效。测试覆盖版本三编辑期间缓存刷新到四，仍提交三；普通改名省略策略，空/全禁用授权不强制开启付费来源。见 `web/src/features/keys/components/__tests__/api-keys-mutate-drawer.test.tsx`。

## 2026-11-01 18:28:35 +00:00 — 微信订单号及跨周期异步退款

微信订阅 `PARAM_ERROR` 的具体消息为“商户订单号错误”；旧 `SUBUSR+用户ID+NO+随机+UnixNano` 至少34字符，超出微信32字符。修复为固定32字符，保留回调分流。真实入口/SDK验签/持久订单回归先失败后通过，三库通过；不能把本地通过写成真实商户付款成功。见 `controller/topup_official_test.go`。

新订阅 MJ 任务失败不能直接退当前钱包，也不能跨周减掉新周期的使用量。任务保存原来源和周期；失败资金、Key预算和退款标记同事务恢复，已经失败的本地补偿不再依赖上游可达。周期快照使用 nullable 预扣字段区分历史缺失与真实零值，幂等重放返回原快照。预扣退款原先在事务内调用全局 DB 再开事务，存在单连接等待且金额/标记不原子；现统一同一事务。真实三库升级/重迁移及系统轮询验证见 `controller/system_task_test.go`，故障注入、零周期推进和两个真实 MJ 入口见 `service/task_billing_test.go`、`relay/relay_task_test.go`。管理员不推进周期的清额操作保留原同周期语义。


## 2026-11-01 18:53:12 +00:00 — 挂靠测试拒绝需要追踪执行操作类型

现象：`accounts/:id/test` 返回 `hosting_control_rejected`。已核实执行层在普通凭据探测入口拒绝订阅组，并非 New API 缺少管理员权限。修复使用本人账号专用 Responses 执行，不能通过共享管理 Key 或删除通用探测门禁解决。详细根因、测试和适用边界保存在 [gpt-load 排障记忆](../../../gpt-load/docs/memory/lessons.md)；前端安全响应不透传原厂正文，排障应结合执行层的脱敏状态码核对。


## 2026-11-02 11:26:25 +00:00 — 测试准入状态与 MySQL 空更新

现象：自有账号优先仍使用钱包；只读排查发现账号完成授权但尚未测试。用户确认测试门槛符合产品预期，缺的是首次连接自动测试，不能自行把 connected 等同于 active。

修复：保存后自动测试一次；重测先清除历史验证，只有 passed 重新激活，失败与未知退出池。三库回归进一步发现 MySQL 相同值 UPDATE 默认返回零修改行，同秒重试 connected 账号被误判冲突；零行时核对当前凭据、未暂停与验证已清除状态，保留真实冲突拒绝。SQLite、MySQL 8.4.2、PostgreSQL 14.17 的 `TestHostingCredentialAndProbeBoundaries` 覆盖重复测试、故障撤销及迟到结果遇暂停，不以单一方言结果推断兼容。

适用边界：免费账号准入，不改变 Key 的付费回退授权。自动测试不重试、不耗原厂重置机会，测试网络使用本地执行层替身，未证明真实原厂账号可用。


## 2026-11-03 03:39:28 +00:00 — 使用日志的业务引用与用户说明分离

现象：日志详情将挂靠账号编号和 `hosting:2:quota_exhausted` 原样展示，用量未知又挤在来源区。旧日志仅保存业务引用，不能只修新日志写入，否则用户现有记录依然不可读。

修复：日志列表从主库批量读取引用账号的当前备注与已存脱敏邮箱，并逐条匹配日志所有者；删除墓碑也可识别，缺失则明确提示信息不可用。显示投影只修改返回对象，原始日志与原始导出保持不变，不声称当前备注是历史快照。元数据采用 RawMessage 保留大整数精度，查询错误显式返回。前端解释现有回退原因码，未知码给出可读的缺失说明；用量未知移入既有 Token 明细，不伪造零统计。

验证：`controller/log_hosting_test.go` 在真实 SQLite 3.50.4、MySQL 8.4.2、PostgreSQL 14.17 覆盖用户/管理员/令牌入口、归属隔离、删除墓碑、原始记录不改写、大整数与查询失败；前端日志目录 25 文件 458 项通过，含真实中文资源切换与七语言词条。没有改动计费或调度，没有生产部署。源码入口为 `model/log_hosting.go`、`web/src/features/usage-logs/components/resource-source-details.tsx` 和详情弹窗 TokenBreakdown。

## 2026-11-03 05:46:32 +00:00 — 页面布局插槽可能丢弃业务弹层

现象：商品编辑和结账组件本身可渲染，放到页面后点击无弹层。根因：`SectionPageLayout` 只组合识别的插槽，未放在 `Content` 内的业务弹层不会渲染。

修复：将代充值的编辑、结账和订单详情放入 `SectionPageLayout.Content`；回归从真实页面点击商品/编辑按钮并断言可见弹层。单独挂载弹窗的组件测试不能覆盖父布局接线，适用于使用同一布局的新增工作区。见 `web/src/features/account-recharge/__tests__/recharge.test.tsx`。

## 2026-11-03 06:11:35 +00:00 — 代充值资料错误需保留可操作原因

现象：提交资料提示“账户资料格式或加密配置无效”，实际是填写邮箱与 session 中账号邮箱不同。根因：服务层已经识别邮箱冲突，但控制器将所有加密前校验错误替换成同一文案。

修复：输入无效、邮箱不匹配、加密未配置使用固定错误类型，控制器仅公开这些安全文案，其他故障保留服务端错误。七语言补齐提示，不回显邮箱或会话。保留账号一致性校验，大小写及邮箱两端空白不构成冲突。

验证：`TestRechargeAccountInfoErrorMessages` 覆盖四类失败、订单不变、错误脱敏及原 JSON 还原；代充值后端回归、前端六项测试通过。本地原请求复现确认现在返回邮箱冲突错误。排障使用的真实凭据未写入源码、测试或记忆。

## 2026-11-03 09:47:19 +00:00 — Realtime 改写 URL 前避免初始化 Gin 查询缓存

现象：Key 映射已把 `Request.URL.RawQuery` 改为目标模型，后续 `c.Query("model")` 仍返回源名。根因：首次通过 `c.Query` 读取会初始化 Gin 的查询缓存；之后只改 URL 不会刷新该缓存。

修复：映射入口直接使用 `c.Request.URL.Query()` 读取及修改，避免提前创建旧缓存；在分发前接入。`middleware/distributor_test.go` 的 `TestTokenModelMappingProtocolBodies/realtime` 用后续处理器的 `c.Query` 验证目标值。适用于本链路中所有前置处理器尚未读取查询缓存的情况，未来增加前置查询读取时需重新检查这一约束。

## 2026-11-03 14:57:22 +00:00 — Key 跨模型映射不等于 Responses 参数兼容

现象：线上任务把 `gpt-6-luna` 映射到 `glm-5.3` 后失败。通过 SSH 只读核对 New API 与 gpt-load 日志及日志表，确认 Key 目标已生效；gpt-load 的 `opencode_go` 路径返回 400，提示 `reasoning.context: this reasoning option requires a native Responses target`，New API 对客户端返回 502。同期订阅预扣不足后已尝试钱包，不是最终失败原因。

根因与边界：当前映射只改变模型身份，保留其他请求参数；目标链路不支持此次 Responses 推理选项。源码 `middleware/token_model_mapping.go` 与 `gpt-load/internal/execution/bifrost/opencode_presets_test.go` 分别说明改名和原生协议保留边界。未读取完整用户任务正文，未确认 `reasoning.context` 的具体值，也不能据此断言其他所有模型切换都会失败。

规避与后续：这类任务先使用兼容原请求参数的目标；若要跨供应商继续任务，须另行验证参数与历史工具调用的兼容策略，不能承诺仅删除一个字段即可无损继续。本轮仅诊断，未改生产配置、重启服务或向供应商重放任务；兼容修复尚未实施。

## 2026-11-03 15:26:33 +00:00 — 新任务兼容处理已在 GPT-Load 本地落地

承接 2026-11-03 14:57:22 条目：用户再次在新任务映射 Kimi 时遇到相同拒绝，并授权先实现新任务范围。GPT-Load 已对 OpenCode Go/Zen 的 GLM、Kimi、DeepSeek 模型族接入现有 Responses→Chat→Responses 转换；两类 New API 映射都在其前完成，无需在 New API 再写一份参数规则。

验证包括真实 HTTP 网关、普通/流式响应、函数工具和结果回传、日志实际协议、原生 GPT 保留、旧状态/不支持工具拒绝、探测协议，以及相关包回归、定向 race 和构建。未部署或调用真实厂商，不能据此承诺所有客户端可无损切换；GPT 推理选项采用目标默认行为，托管/自定义语法工具仍需原生目标。具体模块与命令见 [GPT-Load 说明](../../../gpt-load/README_CN.md#oak新任务的跨模型兼容)。

## 2026-11-03 16:28:13 +00:00 — Key 映射必须覆盖 Redis 热缓存字段表

现象：用户已经保存 GPT→Kimi 的 Key 映射，请求仍调用原 GPT 模型且没有 Key 映射日志。SSH 只读核对发现数据库有规则，审计证明请求前已经保存，而 Redis 令牌哈希缺少 `ModelMapping`。这与此前 GPT-Load 的跨协议参数兼容失败是不同问题。

根因：`model/token_cache.go` 的 Lua 初始化显式列举字段，新增 Token 属性时遗漏了映射；`RedisHGetObj` 对缺失字符串返回空值。编辑后的栅栏期从数据库读取，因此仅测试保存后立即请求会掩盖热缓存失效。

本地修复：初始化写入映射；同一次 HGETALL 要求映射字段存在，缺字段按既有流程回源。旧哈希仅用 HSETNX 补缺，保留实时额度和已有规则；显式空值合法，变更栅栏优先于补缺。无需清空 Redis 或迁移数据库，必须发布 New API 才会在线上生效。

验证：新增回归在修复前复现规则丢失，修复后 `go test ./common ./model ./middleware ./controller -count=1` 全部通过。`model/token_auto_groups_cache_test.go` 覆盖热缓存往返、旧哈希回源补缺、额度不覆盖、编辑/清空在栅栏到期后的热缓存一致性；既有额度与映射请求链路测试一并通过。Redis 使用 miniredis，未向真实厂商重放任务，未修改生产配置、提交或部署。

构建边界：实际 New API 主程序 `go build -o <临时目录>/new-api .` 通过。全目录 `go build ./...` 被本机未纳入 Git 的 `dujiao-next-main` 目录触发的 `gomonkey` 依赖校验缺失阻断；未为此修改依赖或删除该目录。


## 2026-11-04 01:20:55 +00:00 — Caddy 正文采集需验证字段内容和服务用户权限

现象一：Caddy 2.11.7 使用 `log_append request_body {http.request.body}` 后，上游收到完整请求，但 JSON 日志解析得到空正文。源码在特殊正文占位符时提前添加字段，又因 Early 未显式开启在回程添加同名空字段。修复显式使用 `<request_body`。隔离 HTTP 回归先失败后通过，说明配置通过和转发成功都不能替代正文落盘断言。

现象二：候选配置以 root:caddy 0640 验证通过，但首次切换复制到原 root:root 文件时仅同步了模式，没有同步所属组，服务用户无法读取配置。自动回滚恢复旧服务，之后用 install 明确 root:caddy 0640 并先检查服务用户可读，再次切换成功。后续部署不能用 root 验证结果替代运行身份校验。

验证：deploy/oak/tests/test_caddy_body_log.py 用真实候选二进制验证 6 类 HTTP 行为、SSE 首帧、日志过滤与文件权限；生产启动后 Oak 与访谈均 200，合成无效 Key 的请求 401 且正文与关联 ID 正确落盘。官方二进制版本、资产 SHA-256 和部署边界见 deploy/oak/README.md。只存合成测试数据到源码，实际请求正文始终留在服务器受限目录。

## 2026-11-04 10:28:44 +00:00 — 搜索后端必须避免同名模型跨能力组分流

现象：同样的 gpt-6-luna 搜索请求有时返回真实搜索，有时 400，提示 OpenCode 模型没有 hosted web_search。根因已由 GPT-Load request_logs 证实：模型同时存在 Codex 组和 OpenCode Go 组，成功请求落入 Codex，失败请求落入 Go。修复是选择仅路由到支持搜索上游的模型或专用别名；当前 gpt-6.1-sol 仅在 Codex 组，Kimi 和 GLM 均完成真实搜索及公开历史续轮。不能按 GPT 名称或单次成功推断所有候选渠道的搜索能力。配置提示已加入模型设置。

同次真实验收发现 Codex 上游拒绝 `max_tool_calls`。编排不得默认注入这个选项；客户端显式要求时保留并报告不支持。`max_output_tokens` 也不能当作可靠的绝对成本上限。平台限制自动发起的 GPT 请求轮数，实际工具费用仍从既有用量累计器和上游事实结算。代码及边界见 `controller/search_bridge_loop.go` 和[方案](../plans/api-key-model-mapping.md)。

搜索测试的无资源策略分支会运行原 Relay 的异步性能指标任务，若 fixture 紧接着恢复 Redis/DB 全局配置，会触发测试清理竞态。仅在搜索 fixture 关闭非验收范围内的性能聚合并恢复，不改生产指标逻辑；请求取消的子执行器必须先结束结算再释放外层请求。HTTP 在途取消与完整搜索回归通过 race。

## 2026-11-05 08:53:04 +00:00 — 持久事件不能用最大自增编号认定已完整消费

现象：事务先取得较小 ID、较晚提交时，按最大已读 ID 推进游标会永久跳过它；SSE 的 Last-Event-ID 也只代表传输位置，不证明业务已处理。修复为每消费者独立唯一投递记录，用 NOT EXISTS 分配可见未分配事件，单条显式 ACK 推进确认；过期清理、分配和 ACK 共用消费者锁，缺口按真实未确认事实累计。

适用边界：本期至多一百消费者的数据库事件服务，不承诺高吞吐 MQ 或恰好一次网络传输。三库验证见 `controller/event_hub_test.go`，客户端仍须按 event_id 幂等。

## 2026-11-05 08:53:04 +00:00 — 同级详情与弹窗不能共享数字 React key

现象：事件页面签发 Token 后，后台刷新使同一消费者详情累积多份旧 DOM，单独组件测试未发现。根因：ConsumerDetails、TokenEditor 和 ConsumerEditor 同属一个父节点，用同一消费者 ID 当 key，异步渲染发生键冲突。

修复：按组件角色加 key 前缀，保留切换消费者时的重挂载语义。真实父页面签发、关闭、刷新回归先失败（应一份、实际两份），修复后通过；编译页面再次签发及编辑后 DOM 详情计数均为一。测试见 `web/src/features/event-hub/__tests__/event-hub.test.tsx`；所有同级异构组件共享业务 ID 时都需检查此约束。

## 2026-11-05 17:09:56 +00:00 — 订阅候选预扣失败需结合最终请求与本期余额判断

现象：订阅优先的请求反复打印 `resource billing preparation failed`，但同一请求随后从钱包预扣、结算并返回 HTTP 200。线上只读核对 journal、消费日志及订阅实例，确认订阅仍有效，但本期只剩不足以支付预扣金额的零头。

根因：`model/subscription.go` 的 `PreConsumeUserSubscription` 要求单个订阅实例的本期剩余额度足够支付整个预扣；有效期和正数余额不能单独证明可以承接请求。`controller/resource_relay.go` 的 `relayPlatformResource` 在返回可回退的 `quota_exhausted` 前也会打印 ERR，因此该行不能单独认定整笔请求失败。

排查方式：先确认实际 Key 的 `resource_policies.sources_json`，显式策略优先于历史用户 `billing_preference`；再比较 `user_subscriptions.amount_total - amount_used` 与日志中的预扣金额，核对 `next_reset_time`，最后按同一 request_id 检查最终资金来源、结算和响应状态。额度与重置以用户订阅实例为准，不能用当前套餐模板替代。仅完成诊断，未修改生产数据、配置或日志级别。

## 2026-11-05 17:52:08 +00:00 — GORM 联表查询在聚合与分页之间隔离会话

现象：挂帐财务列表先执行 Count、SUM，再 Scan 当前页，三种数据库都报重复联表或歧义字段。根因：同一个携带 Joins 的 GORM 查询对象被终结操作继续修改，后续 Scan 累积相同关联。

修复：保留公共过滤条件，各次 Count、聚合和分页 Select 使用 `Session(&gorm.Session{})` 独立查询状态；不改写 SQL 或吞掉错误。`model/credit_query.go` 已应用，`controller/credit_account_test.go` 的真实 SQLite、MySQL、PostgreSQL 列表及金额断言验证。适用于同一个联表查询被多次聚合、投影或分页复用的路径。

## 2026-11-05 18:38:05 +00:00 — 控制台错误已有词条仍需显式翻译元数据

现象：代充值挂帐报价返回 `Insufficient available credit`，七语言词条已存在，中文 Toast 仍展示英文。根因：`rechargeError` 直接构造只有 `message` 的 JSON；统一前端 `getServerMessage` 仅对 `message_key` 调用 i18next，普通 `message` 按原文展示，以保护上游或用户自定义文本。

修复：共享错误出口使用 `common.ApiErrorStatus(c, code, common.NewMessage(message))`，保留原状态码与英文说明，并附加显式翻译键。继续仅将固定白名单错误传入，未知异常保持脱敏，不全局猜测翻译任意错误文本。

验证：`TestRechargeErrorTranslationContract` 修复前失败、修复后通过，覆盖挂帐四类错误、余额不足、权限、未找到、缓存与内部故障；`web/src/lib/__tests__/server-error-notifications.test.ts` 经真实报价 API、全局 QueryClient 与中文资源断言 Toast 为「可用挂帐额度不足。」且仅一次。相关后端定向测试与前端 28 项错误链路测试通过；未部署。


## 2026-11-05 18:46:51 +00:00 — 执行层冷却不能被当成结果未知，免费失败也须独立落日志

现象：挂靠优先时，原厂明确额度拒绝后的请求仍返回502，New API请求日志没有对应失败。只读现场确认GPT-Load首次429后进入模型额度冷却，后续连续失败均attempt_count=0。

根因：New API仅采纳一分钟内额度快照，执行层虽有未处理/未发送证据却没有数据面合同传递；免费链路非2xx提前返回，又绕开平台渠道错误日志。不能把所有429都放行，也不能通过永久信任旧零额度快照处理。

修复：GPT-Load以全请求尝试状态生成保留的安全拒绝头，任一未知结果永久撤销；New API只接受回环执行端唯一精确标记的429，按原Key授权继续。挂靠失败独立写现有错误日志，关联用户和执行层请求身份，不留凭据/原厂正文，不将未知用量当零。HTTP/SSE、无隐含钱包及单次扣费回归和真实SQLite3.50.4/MySQL8.4.2/PostgreSQL14.17矩阵通过，两服务回归、vet/build通过；未发布。细节与已验证边界见[排障记录](../diagnostics/hosting-resource-fallback.md)。


## 2026-11-06 07:54:30 +00:00 — cn 入口仅允许 TLS 1.3 可使本机 Codex 在 HTTP 之前失败

现象：cn 官网与普通 curl 可访问，但本机 Codex 发送 Responses 时报 `is_connect=true`，wdm 无对应请求。DMIT Xray 443 入站显式配置 `minVersion=1.3`；强制 TLS 1.2 握手收到 protocol version alert，原域名可用。隔离回环 Xray 仅将最低版本改为 1.2 后，同一 Codex CLI 成功进入 New API 并得到合成无效令牌的 401；TLS 1.3 也保持可用。证明是该客户端与入口 TLS 策略的兼容问题，不能把网站可访问等同于 Codex 传输可用，也不推广为所有 Codex 均不支持 TLS 1.3。

排查时必须用 `X-Forwarded-Host=cn.oak.qingshukeji.top` 区分 cn 反向代理与 DMIT 网络代理出口；仅依据远端 IP 会混入原域名成功/失败请求。候选配置已通过 Xray 校验，生产未修改，等待用户明确应用授权。完整证据、关联 ID、候选位置与验证边界见[诊断记录](../diagnostics/cn-proxy-codex-tls.md)。


## 2026-11-06 08:00:08 +00:00 — cn TLS 最低版本修复已上线并经真实 Codex 验收

承接 07:54:30 条目：用户明确授权后，DMIT Xray 已将最低 TLS 版本改为 1.2，保留 TLS 1.3、原证书、路由及配置权限；生产已备份并正常重启。正式入口双 TLS 版本检查通过。桌面应用自带 `codex-cli 0.167.0-alpha.3.2` 使用既有 API Key 与 `gpt-6.1-sol` 经 cn 完成一次真实请求，退出码 0、输出 OK；wdm Caddy 确认 cn 转发 Host 与 Responses 200，请求 ID `202611060759041417139878268d9d6fELnFHld`。客户端永久配置与 wdm 配置未改。此前“待授权、生产未修改”是当时状态，现已完成；备份、SHA-256 和验收边界见[诊断记录](../diagnostics/cn-proxy-codex-tls.md)。

## 2026-11-10 03:30:44 +00:00 — 托管模型跳过后仍受 Key 的 Auto 分组范围约束

现象：渠道模型测试成功，实际 Responses 请求却返回 `resource_sources_exhausted`。只读核对 wdm 的 Caddy 精确请求、Key 映射、Auto 分组和 abilities，并确认线上 New API 与本地均为 `34e0fea2777a`：客户端请求 `gpt-6-astra`，Key 映射目标为 `claude-fable-5-1`；本人 Codex 私有组目录不含目标模型，而 Key 的显式 Auto 列表只有 GPT、GLM、Gemini、Deepseek、Kimi。目标模型的启用渠道仅属于 default 或 Cursor_claude，两者均不在该 Key 列表中，因此平台候选也无法选出渠道。

调用链：`controller/resource_relay.go` 查询 GPT-Load 私有账号模型目录，发送生成请求前对不支持模型返回 `model_unavailable`，随后遍历已授权的订阅/钱包来源；`service/channel_select.go` / `service/group.go` 继续按当前 Key 的显式 Auto 列表选渠道，不自动扩大到新分组。渠道测试由 `controller/channel-test.go` 按指定渠道 ID 执行，不能证明这个 Key 的自动路由覆盖该渠道。修正配置应明确将目标渠道所属分组纳入该 Key，保持用户授权边界；当时无 MaxTokenAutoGroups 覆盖，默认上限为 5。

排障边界：`credit_accounts` 是挂帐授信账户，`subscription_enabled` 是预留的挂帐购买开关，不是模型调用订阅资源开关；托管快照的 credits=0 也不等同于 Plus 计划窗口耗尽。不能用这两项解释本例 503。相邻 `hosting_route_unavailable` 是另一请求的 502，需独立归因。本轮仅核对源码、线上只读数据及访问日志，未重放用户任务、执行模型测试或修改生产配置。

## 2026-11-10 08:04:57 +00:00 — platform_execution_failed 需要继续追溯实际渠道与工具续轮

现象：Codex 工具加载后重连并显示 HTTP 502 / `platform_execution_failed`。按 Caddy 中确切任务内容核对，2026-11-10 06:45:08 UTC 的首轮请求 `202611100645088784200438268d9d6BU4ANxmb` 成功；06:45:17–06:45:48 UTC 的 24 次工具续轮均为 502。首个失败 ID 为 `202611100645177792296038268d9d6sMkVscuS`，入站 JSON 可解析，新增 reasoning、custom_tool_call、custom_tool_call_output 历史。

已验证调用链：Key 将 `gpt-6.1-terra` 映射为 `deepseek-v4.1-flash`；New API 已记录 `hosting:2:model_unavailable` 并选择 subscription，实际渠道为 #9“兔子”，直连第三方 tuzai.icu。New API 将 Responses 转成 Chat，记录 additional_tools 展平、namespace 展平、custom exec 转函数三类诊断；上游返回 HTTP 400 / `invalid_request_body`，外层统一表现为 502。初始成功轮也有上述转换诊断，因此诊断警告本身不足以确定拒绝字段。Caddy 同期无服务警告，GPT-Load 数据面在 06:43–06:47 UTC 没有该请求；不能把所有平台渠道都当成经过 GPT-Load。

适用边界：简单模型测试或首次文本响应成功，不能证明 Codex 工具历史在 Responses→Chat→第三方链路上可继续。当前证据把故障定位到带工具历史的转换后请求被第三方拒绝，具体字段仍需受控最小对照或供应商详细错误；不能仅凭通用 400 指定某个参数。相邻 Claude 的 HTML 502 属于其他会话，不能混作本例证据。本轮仅查询三个服务的日志、只读数据库与实际渠道配置，未重放任务、调用模型测试或修改生产。

## 2026-11-10 08:30:31 +00:00 — Responses 推理历史曾被误转成空用户轮，本地已修复

承接 08:04:57 条目：只读核对留存正文结构，确认失败续轮为 assistant 文字 → reasoning（content=null、summary 非空）→ custom_tool_call → custom_tool_call_output。`relaykit/relayconvert/internal/oai_responses/to_oai_chat_req.go` 原先没有 reasoning 分支，缺省按 user 处理，既插入空用户消息又丢失摘要；合成回归在修复前复现失败。

本地修复将可读推理写入同轮 assistant.reasoning_content，与正文及工具调用合并；复用响应侧正文优先、摘要兜底规则。纯密文不冒充明文，省略时给出 encrypted_reasoning_omitted 诊断。既有测试文件覆盖文字与推理顺序、直接工具调用、正文优先、多段、纯密文与空推理；RelayKit 全量测试、GOWORK=off 独立构建及 New API 服务入口 go build -o /dev/null . 通过。

边界：已证实的是转换缺陷，不是第三方通用 400 的唯一拒绝字段；没有向第三方重放真实任务或执行付费模型探测，生产未发布。全仓 go build ./... 被无关 dujiao-next-main 的既有依赖校验缺失阻断，不能写成全仓构建通过。发布后仍需在渠道 #9 用合成的工具调用与结果回传验证实际兼容性。

## 2026-11-11 06:47:42 +00:00 — Flash 正文推理需区分上游输出与转换方向

现象：用户报告 deepseek-v4.1-flash 的 Chat 正文包含规划，询问是否由 Responses→Chat 配置导致。经 SSH 只读核对，New API 线上为 `117f513a7227`，GPT-Load 为 `ba2522dcea0c`；报告时段 2026-11-10 08:50–12:45 UTC 的 Flash 日志全部指向 #9「兔子」直连 tuzai.icu，GPT-Load 同时段无该模型记录。相关渠道 `thinking_to_content=false`。#9 Chat 路由为 converter=none，只有 Responses 入站启用 Responses→Chat；Caddy 留存的 20:36 两次请求均为 cn 的 `/v1/chat/completions`、stream=true，因此没有经过该转换。

合成验证：按报告提示词、400 token 上限，直达 #9 上游及公开 cn Chat 的普通/流式四次请求均为 200，content 保留完整 `<thinking>…</thinking>`，reasoning_content 另有文本。公开 cn Responses 经配置的 Chat 上游路径，普通/流式也保留标签正文，并独立输出 reasoning；最终普通请求 ID `202611110647274982886628268d9d6FnE79fXb`，流式 `202611110645534086706458268d9d6Z4elnpeL`。直连 Chat 的两次公开请求 ID 分别为 `202611110641308803287488268d9d6JD3BNS1H`、`202611110641379594635238268d9d6qlqfXFsZ`，数据库再次确认均为渠道 #9。只发送合成提示词，没有重放用户任务或修改生产配置。

源码与本地验证：`relay/channel/openai/relay-openai.go` 的 sendStreamData 在未开启格式化/推理转正文时原样发送 SSE，OpenaiHandler 普通响应也默认复制原始正文。`relaykit/relayconvert/internal/oai_chat/to_oai_responses_{resp,stream_resp}.go` 分别把 reasoning_content 转为 reasoning、content 转为 output_text，没有标签整块清理或相同文本去重。临时 Go 程序验证正常分离、标签正文、两字段相同的普通/流式转换均保持输入字段。当前链路的清理应在 New API 接收 Chat 上游响应、对外发送或协议转换之前统一执行；只改 GPT-Load 无法覆盖 #9。修复流式标签需要跨 chunk 状态，不能逐 chunk 正则替换；全部为推理时也不能伪造正文成功。此为建议，尚未实现。

另一个独立缺陷已本地复现：反方向“上游 Responses→客户端 Chat”的非流式 `relaykit/relayconvert/internal/oai_responses/to_oai_chat_resp.go::ExtractOutputTextFromResponses` 在没有正式正文时遍历所有 output.content 文本，包含 reasoning.content。只有 `reasoning.content=[{type:reasoning_text,text:内部规划}]` 时，ResponsesResponseToChatCompletionsResponse 同时输出 content=内部规划、reasoning_content=内部规划。需要限制正文提取的项目类型，不能在缺少正文时把推理当回退正文；它不是 #9 当前配置的返回转换方向，不能混为本次报告的已确认根因。既有推理/响应转换定向测试通过，但未覆盖这个边界。

验证边界：诱导输出标签的最小复现证明上游会返回标签块、平台未清理，不能单独证明无诱导时的自发泄漏成因。Caddy 不记录响应正文，历史四次的完整输出和完全相同双写未能从原始响应重新核验；没有发现本地这条 Chat 路径执行“只删 thinking 标签”的逻辑，不能仅凭客户端结果断言由本平台删标签。此次仅完成排查与记忆记录，未修改业务源码、提交、推送或部署。

## 2026-11-11 07:11:08 +00:00 — DeepSeek 返回推理分离已在本地修复

承接 06:47:42 条目，用户明确要求修复。`relay/channel/openai/reasoning_output.go` 在真实上游模型 `deepseek-` 前缀的 Chat 响应上统一处理标签块、缺少起始标签、独立推理重复前缀及完全相同双写，普通/流式 Chat 和 Chat→Responses 已接入；原有 Chat→Claude/Gemini 返回同样经过普通 Chat 处理器。正文与推理只在 choice 终态后释放，以识别迟到推理和跨事件标签；工具、角色、扩展字段与用量保持原路径。缓冲采用现有 SSE 扫描大小上限，缺少终态、超限或清理后只剩推理但上游声称 stop 时显式失败，禁止自动重试。代价是 DeepSeek 文本显示延后，其他模型不启用该策略；无标签、无推理字段对照的规划无法可靠识别，不使用语言或关键词猜测。

反方向 Responses→Chat 已删除 ExtractOutputTextFromResponses 遍历所有 output.content 的回退，只接受 assistant message.output_text；reasoning.content 不再因缺少正文被复制到 content。新回归集中在 `relay/channel/openai/reasoning_output_test.go`，先复现失败再通过，覆盖四条返回入口、标签变体/嵌套/错配/未闭合、孤立闭合、整段重复、工具轮、迟到别名、断流、扩展字段及有/无供应商用量。RelayKit 全量测试和独立构建、OpenAI/高级自定义/DeepSeek/helper 包回归、相关定向 race、vet、服务入口构建均通过。实现与命令见 [中文说明](../../README.zh_CN.md#deepseek-chat-推理分离)。未提交、推送或部署，生产是否恢复仍需发布后用合成请求验证。

额外接入教训：将清理后的 SSE JSON 反序列化回同一个非空 DTO 时，JSON 中已删除的字段不会自动清空，会让旧 content 再次进入转换器。`responses_via_chat.go` 现在先清零 chunk 再解析，避免“清理器正确但转换后仍泄漏”；定向四入口回归已覆盖此故障。用量处理在清理前保留原始文本或快照，不能用清理后的短回答偷偷降低生成量。

## 2026-11-11 07:48:47 +00:00 — 挂靠顺延的一次请求不应写成两个顶层使用记录

现象：挂靠优先遇到拒绝后订阅成功，使用面板显示一条 `hosting_execution_rejected` 与一条消费记录。只读核对请求 `202611110712299387973438268d9d6oRfNN6BA`，两行客户端 ID 相同，执行层 `attempt_count=0`；失败零收费，最终只有订阅消费。根因是安全拒绝立即记错误、后续成功再记消费，日志条目被用户理解成请求次数；429 本身不足以确认额度耗尽。

本地修复：`controller/resource_relay.go` 将有可信未处理证据的挂靠 429 归入最终日志 `resource_attempts`，保留账号、状态与执行层 ID；终止错误及所有来源不可用仍留最终失败，后续平台预检错误也不丢诊断。`service/log_info_generate.go` 统一附加尝试快照；现有来源详情展示归属及关联身份，公共内容渲染器解释旧错误码。原顺延权限和计费链未改变，历史日志不改写。本轮只需 New API 发布，GPT-Load 已提供安全证据。

验证：SQLite 3.50.4、MySQL 8.4.2、PostgreSQL 14.17 的真实库回归覆盖单条最终记录、尝试累积、账号/钱包/订阅回退、平台单次计数与结算、终止及预检错误；相关后端回归、定向 race、vet、服务构建，前端 27 项回归、类型检查、lint 与构建通过。详细证据与命令见[排障记录](../diagnostics/hosting-resource-fallback.md#2026-11-11-074847-0000--安全顺延尝试并入最终请求日志本地完成)。未提交、推送或部署，线上效果仍待发布验收。

## 2026-11-11 08:15:32 +00:00 — Claude 最终模型 404 会掩盖前一路推理参数 400

现象：请求 `202611110757224789363638268d9d63QBqx5hH` 于 07:57:22–07:57:27 UTC 返回 404。只读核对 wdm 的 journal、PostgreSQL、GPT-Load SQLite 和 Caddy 精确 request_id：New API #10 `OakGL_Claude` 将 `/v1/messages` 转成 `/v1/responses`，再经本机 GPT-Load 访问 Aiwanwu。执行层 ID 为 `db3188f2-33e6-47ac-b0cb-740996dbd631`，不是一次单纯模型拒绝。

两次尝试依次为：组 #8 `Aiwanwu Claude` 返回上游 400，拒绝 enabled 推理并要求 adaptive 与 `output_config.effort`；组 #6 `Aiwanwu` 返回上游 404，提示分组无账号支持 `claude-fable-5-1`。相邻请求 `202611110757296880353938268d9d6U8WsCfAB` 重复同样两步。两组本地模型目录都声明该模型，因此本地声明不能代替对应上游 Key 的实际模型支持；最终错误摘要也不能代替完整 attempts。New API 的 `claude cli trace` 会话规则记录 `strict_session` 停止，未再切其他 New API 渠道。

入口已带 `thinking.type=adaptive`、`output_config.effort=high`，不能建议客户端重复改成已经使用的参数。#10 的转换配置和 `relaykit/relayconvert/internal/claude_messages/to_oai_responses_req.go` 证明存在 Claude→Responses 转换；具体哪一层重新产生 enabled 尚无出站正文或供应商内部日志证据。后续可优先验证端到端原生 Messages 路径，并独立核对组 #6 对应上游 Key 的模型授权；这只是待验证方向，不能承诺改路由即可恢复。当前 New API 为 `344682784654`，GPT-Load 为 `ba2522dcea0c`；本轮未重放任务、执行付费模型测试、修改生产配置或重启服务。

## 2026-11-11 08:48:47 +00:00 — Claude 排障需区分候选 Key 错配、跨服务推理转换与等待超时

补充上一条：用户在 Aiwanwu 面板未见报错。固定窗口 2026-11-10 08:41:30 至 2026-11-11 08:41:30 UTC，GPT-Load 的 Fable/Opus 共 96 次成功、70 次最终失败、3 次取消；最终失败为 47 次 404、23 次 504。尝试级另有组 #6 的 58 次模型 404、组 #8 的 8 次推理参数 400、32 次普通 `Not Found` 和 7 次 502；尝试次数不能当成失败请求数。所有这些模型的 New API 日志均走 #10。本轮运行版本已为 New API `3e8977c53862`、GPT-Load `ba2522dcea0c`，不可沿用上一条的 New API 版本。

已验证的配置错配：使用各组实际凭据仅在服务器内存中请求 Aiwanwu 的 `GET /v1/models`，两个 Key 均为 200，但组 #6 / 凭据 #6 的 7 个模型没有任何 Claude，组 #8 / 凭据 #11 的 7 个模型包含 Fable、Opus、Haiku、Sonnet。本地两组都声明 Fable/Opus，导致 #6 被反复选中后由 Aiwanwu 模型选择层返回不支持。当前目录与历史拒绝相互印证，不能描述成 Claude 原厂执行失败，也不能从管理面板无消费记录推断没有入口拒绝。

已验证的本地转换行为：用合成 adaptive/high 参数调用真实 `relayconvert.ConvertRequestByID`，三个模型序列化后均只保留 `reasoning={effort:high,summary:detailed}`，原生 adaptive 模式只存在于不参与 JSON 的 `ReasoningConversion`。将该结果交给本地 Sub2API 的 `apicompat.ResponsesToAnthropicRequest`，Fable 5 / 5.1 生成 enabled/10240，Opus 5.5 生成 adaptive，均保留 effort=high。两个临时 Go 程序退出码 0，依赖检索关闭，临时文件已清理；未调用模型。该复现解释了协议链的兼容风险及 Fable/Opus 差异，但 Aiwanwu 的实际代码版本与内部出站正文未知，不能把本地源码行为等同于其线上根因证明。

超时边界：23 次 504 的执行尝试状态码为 0，约 120 秒后 `upstream_timeout`、response_started=false、dispatch_state=maybe_sent；这是本地等待终止生成的 504，不是收到 Aiwanwu 的 HTTP 504。两组无超时覆盖、全局无对应持久覆盖，默认 first_byte_timeout 为 120 秒。不能仅据此区分网络、排队或推理慢。32 次普通 404 和 7 次 502 则有真实上游 HTTP 状态；普通 `Not Found` 缺少内部细节，不能断定是哪条上游路由或资源缺失，亦不能用模型列表存在证明该次生成路径正常。

成功对照：Fable 5 请求 `202611110825140932904348268d9d6vDmKzra6`、Opus 5.5 请求 `202611110800516926417198268d9d68m1XLf5f` 均为原始 Messages adaptive/high，经相同 #10 和组 #8 返回 200，因此不应概括成两系列整体不可用。本轮仅查询日志、配置、模型目录并执行本地合成转换，未重放任务、做付费模型测试、改生产或重启服务；修复方向为清理无 Claude 授权的候选、受控验证原生 Messages 链路，尚未实施。

## 2026-11-12 05:02:03 +00:00 — Claude 搜索流失败需区分工具限制警告与输出块冲突

现象：wdm 的 New API 错误日志 #13837–13839 依次为请求 `202611120422101360704818268d9d61pZwQTzk`、`202611120422099260106558268d9d6AVlCNLEb`、`202611120422212381042988268d9d6R6WdDcsx`，于 04:22:30、04:22:35、04:22:37 UTC 失败。三条均走 #10 OakGL_Claude、claude-fable-5，直接错误为 `Responses output item changed from tool_use to text`；journal 显示 received=5、handler_stop，返还预扣费。外层 HTTP 200 是已经开始发送流，不能当成生成成功。线上版本为 `3e8977c53862`。

已核对 Caddy 留存请求的结构：三条均为 stream=true，声明 `web_search_20250305`、name=web_search、max_uses=8，tool_choice 强制 web_search；因此不能归因为客户端未声明搜索工具。`unsupported_search_limit` 只说明 Claude→Responses 无法保留 max_uses；相邻非流式成功请求 `202611120422308954386858268d9d66gvQkoXk` 也有同一诊断，不能把它直接等同于终止原因。#10 将 Messages 转为 Responses，`relaykit/relayconvert/internal/oai_responses/to_claude_messages_stream_resp.go:219` 在返回转换时按 output_index/item_id 查块，既有 tool_use 块随后被识别为 text 就抛出本次错误；这两个源码文件与线上 revision 无差异。

证据边界：GPT-Load 同时刻三条记录为 client_canceled，时长与 New API 中断吻合，不能据此认定是终端用户主动取消。现有已查日志没有出错前的完整上游 SSE，尚不能确定是上游复用索引/身份，还是返回转换关联错误，也不能证明上游实际完成搜索。此次只读生产日志、数据库与请求结构，未重放任务、调用付费模型、修改配置或重启服务；仅追加本地排障记忆。

## 2026-11-12 05:19:00 +00:00 — 受控复现确认 Aiwanwu 搜索流转换产生无归属工具增量与索引冲突

本条收敛上一条尚未确定的来源。继续经 ssh wdm 查询 GPT-Load：线上仍为 `ba2522dcea0c`。三条失败按开始/结束时间与时长对应执行 ID `e58c5001-4d13-4d1a-ab9b-eb02abcec318`、`f87c713d-2864-47b5-b944-237665124cc2`、`abe7eadf-2b62-4815-a6b0-b6559f61f24e`；各只有一次尝试，均为组 #8 Aiwanwu Claude、凭据 #11、sub2api/native/openai-responses。上游 HTTP 200、response_started=true、committed=true，最终 failure_origin=downstream、downstream_cancel/client_canceled；取消是 New API 中断连接的后果。这三条未记录请求正文，运行时 StoreRawRequestResponse=false，历史完整 SSE 无法恢复。

关键复现：保留首条原搜索子请求的查询、通用说明与元数据，通过已核对的 Claude→Responses 字段映射直接调用 Aiwanwu `/v1/responses`，仅把输出上限降为 2048。绕过 New API/GPT-Load 后，上游请求 ID `e2e4e6f2-3233-414c-968d-d539521be224` 在 20.562 秒内返回 completed，但开头依次为：① response.created；② response.function_call_arguments.delta，output_index=0，缺少 item_id/name/call_id，之前也没有工具 output_item.added；③ response.output_item.added，output_index=0，item.type=message。因此工具参数先占用了索引 0，随后文本又占用同一索引。仅保存类型、索引与 ID 的脱敏事件，交给 New API 真实公开入口 ConvertStreamResponseChunk，在第 3 个事件精确复现 `Responses output item changed from tool_use to text`。成功对照的 6 个合法工具事件全部通过；相关转换器与线上 revision 无差异。

同一原请求再直接调用 Aiwanwu 原生 `/v1/messages`，输出同限 2048，上游 ID `7efceaea-9f7f-46b8-b703-cf19e3f63304`，21.289 秒完成：server_tool_use(web_search,index=0) → web_search_tool_result(index=1，含 5 条 web_search_result) → text(index=2) → end_turn/message_stop。原生搜索能够执行；问题位于上游 Claude 搜索事件转 Responses 的兼容路径，不能归为客户端没声明搜索或 GPT-Load 缺工具。前面 4 次公开合成查询曾仅返回普通 function_call/tool_use，未触发故障，不能据此泛化上游全部请求都没有搜索能力。

源码印证与边界：本地 `sub2api/backend/internal/pkg/apicompat/anthropic_to_responses_response.go` 只在普通 tool_use 开始时创建 function_call，未处理 server_tool_use/web_search_tool_result，却会无条件把非空 input_json_delta 转成函数参数增量；该结构与实测孤立增量一致，但供应商实际源码版本未知，不能指定其部署提交。GPT-Load 的 `internal/execution/bifrost/passthrough.go` 与 `model_alias.go` 对该 native 路由不改事件类型/索引，客户端模型与上游模型也相同；与部署版本无差异。

建议后续在 New API #10 将 `/v1/messages` 改为 `/v1/messages`、converter=none，沿 GPT-Load 已有 Anthropic native 路由保留原生搜索事件，并做整链验收；也可由供应商修复 Responses 搜索流转换。不能通过静默删除孤立工具事件、放宽类型冲突或只移除 max_uses 冒充修复。本轮完成 4 次合成调用、原搜索的 Responses/原生 Messages 两次有界调用及本地转换/回放；未更改生产配置、业务源码或重启服务。临时诊断程序与捕获文件已清理，查询正文、用户元数据及凭据未写入仓库。

## 2026-11-12 06:24:18 +00:00 — Gemini 的通用 502 可追溯到上游 Cloudflare 网关

只读核对 wdm 请求 `202611120616105036296358268d9d6WzaBVpEc`：06:16:10–06:16:20 UTC，Caddy 精确 request_id 对应非流式 `/v1/chat/completions`、`gemini-3.8-flash`，约 9.785 秒后返回 502。New API 错误日志 #13948 走 #4 OakGL_Gemini、回环 GPT-Load；journal 保留的上游 HTML 标题为 `aiwanwu.cc | 502: Bad gateway`。这是实际收到的上游 HTTP 502，现有证据不支持归为本机 Caddy 超时或请求参数校验失败；供应商内部失败原因未知。

GPT-Load 执行 ID `1e2493a0-20a7-4ddc-89e7-2785ca173d0b`，仅一次 native/openai-completions 尝试，组 #6 Aiwanwu Gemini，模型名未映射；failure_origin=upstream、failure_category=upstream_host_error、effect=skip_group、will_retry=false。当前仅该组声明此模型，不能从 next_candidate 指令推断实际发生了重试。06:21:35 UTC 同模型另一次请求也返回 502；所查 06:00–06:22 UTC 窗口仅这两条，不能据此断言全天不可用。线上 revision 分别为 New API `3e8977c53862`、GPT-Load `ba2522dcea0c`。

检索注意：请求 ID 可能被后续诊断对话正文引用，全文匹配只用于缩小候选，必须再按 Caddy 顶层 `.request_id` 精确相等筛选，避免把当前排障请求误算为原请求。正文仅在服务器内存核对，未写入仓库；本轮未重放请求、调用付费模型、修改生产配置或重启服务。

## 2026-11-13 06:51:47 +00:00 — 事件投递次数不证明客户端收妥，同消费者连接共享重发间隔

现象：wdm 事件 #7 累计两千余次尝试仍待确认。线上与本地均为 `95375cf9aa74`，消费者有两个使用同一事件 Token 的活跃连接；前五条投递已经确认，本条投递编号为 6、事件编号为 7。所查应用日志在该事件产生后未见 ACK 请求。数据库只读检查和真实 SSE 接收均已完成，未人工 ACK、补发、修改生产配置或重启服务。

实现依据：`model/event_hub_delivery.go` 的 `MarkHubDeliverySent` 在网络写入前累计尝试；`ListHubDeliveries` 按消费者账本的 `last_sent_at` 限制约 30 秒重发。同消费者多连接不是每连接独立广播：某连接更新发送时间后，其余连接可能连续只收到心跳。独立接收方应使用不同消费者；同一业务客户端需检查重复实例、旧连接和业务处理失败，不能只凭在线状态或尝试数认定终端已处理。ACK 必须使用信封 `id`，不能使用 `event_id`；本次没有客户端源码和处理日志，尚未确定原客户端失败点。

实测：用用户提供的 Token，经应用回环、主域名和 cn 域名分别读取约 48 秒，连接提示及心跳均实时到达，期间三条诊断连接未取得业务事件。随后在下一次重发窗口建立 cn 连接，约 0.114 秒收到 `wallet.topup_succeeded`，`id=6,event_id=7,attempts=2138,acked_at=0`，白名单载荷字段齐全。说明本次事件能够经 cn 公网入口实际传出；不等于原客户端已收到或业务已执行。诊断连接全部关闭，Token 和业务载荷未写入文件。

另发现部署配置差异：仓库 `deploy/oak/Caddyfile` 已排除事件流压缩，wdm 运行配置仍全局启用压缩。复用现有 `deploy/oak/tests/test_caddy_body_log.py`、生产 Caddy 二进制及临时回环服务验证，线上配置副本在“事件流不得压缩”的断言失败，仓库配置通过。但真实 cn gzip 连接可以立即读到提示、心跳和本次事件，不能把配置差异直接认定为本次客户端故障根因。隔离验证目录和进程已清理，生产配置保持原样。

2026-11-13 06:57:09 +00:00 补充：用户再次手动补发后，重新通过 cn 持续监听 158.139 秒，实际收到 14 个业务帧，覆盖事件 #2、#6、#7（投递编号分别为 1、5、6），未确认的同一事件约每 30 秒重复到达。用户同时报告自己的连接只有心跳，与共享重试间隔下其他连接取得事件的表现相符，但尚无其客户端日志，不能据此断定其全部故障原因。诊断连接正常结束且未 ACK；没有留下长期后台监听器。后续排查需客户端运行位置及代码，先核对重复实例/残留连接，再核对命名事件解析和处理后 ACK；独立业务接收方应使用不同消费者。

2026-11-13 07:00:21 +00:00 补充：使用用户新提供的另一枚 Token 建立 cn SSE 连接 192.115 秒，仍绑定消费者 1；HTTP 200、gzip 流可解析，收到事件 #7（投递编号 6）7 次及心跳 13 次，未发送 ACK。监听期间数据库最新事件仍为 #7，没有出现更高编号的新事件，因此不能把这次观察解释为新事件未传输。新 Token 没有改变同一消费者的广播/确认语义。

## 2026-11-13 10:39:41 +00:00 — 财务聚合新增表后测试夹具必须同步迁移

现象：财务总览新增兑换码聚合查询后，原有 SQLite 权限/聚合测试在业务数据为空时返回 `no such table: redemptions`，不是聚合逻辑失败。

根因：测试使用隔离数据库并按查询前的业务表清单手工 `AutoMigrate`；生产启动会统一迁移 `Redemption`，但测试不会自动继承 `model/main.go` 的完整迁移列表。

规避：每次财务总览读取新增业务表，都要在现有总览/权限测试夹具中加入对应模型迁移，并补一条真实业务记录验证收入、当前快照和软删除口径。不要在生产查询中用“表不存在则返回零”静默降级，否则会把迁移遗漏伪装成没有财务数据。
