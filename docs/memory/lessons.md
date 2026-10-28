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
