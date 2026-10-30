# 排障经验

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
