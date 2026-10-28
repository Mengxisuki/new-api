# Oak 手动 CI/CD 接入交接

更新时间：2026-10-28 09:09:22 +00:00。

用户已授权创建专用部署密钥、分开提交并推送官网和 CI/CD，不再逐项确认；GitHub 私钥由用户亲自录入。仍采用手动发布，不执行推送自动上线。

## 当前代码与已完成验证

- 已建立 `upstream-main`，跟踪官方 `upstream/main`，当前均为 `b08ee86fe`；`oak` 从既有 `01da3a55f` 基线承载定制。旧 main 仍为 `246d62aa5`，未改内容。
- 官网、采购及个人令牌路由接线已提交为 `5edda9c46`（40 个功能/测试文件），CI/CD 和说明另行提交。
- 新增工作流、打包脚本、USTAR 受限部署处理器、11 个公共行为测试和固定主机公钥。已通过 actionlint、Bash 语法、11 个部署测试、真实 Linux 程序构建及打包校验。
- 冻结依赖的干净快照在 `build/oak-ci-clean/`，前端 205 文件/2468 用例通过；修复采购权限声明后根模块、relaykit、tokenkit 的 `make test` 全部通过。原工作目录仍保留旧忽略源码和旧 node_modules，不应直接将原目录全量检查当作 CI 环境。
- `build/oak-ci-review/` 只有本地审查用二进制、包和截图；版本的运行 ID 为 0，源工作区未提交，不应把它当作正式 GitHub 发布产物。该目录已被 Git 忽略，不能提交。

## 已接通的外部配置

- 服务器已安装 `/usr/local/sbin/oak-ci-deploy`，root:root、0755，SHA-256 为 `94ea9ab102c8c9fd6b35aa47beb5f3fd7e2b094e36b1647b17cf38b7910d3853`，与本地匹配，`check` 通过。
- 生产 `new-api` 仍 active，current 为 `/opt/new-api/releases/oak-01da3a55f`。没有执行发布、停机、备份恢复或改写生产配置/数据。
- GitHub 已创建环境 `oak-production`，ID `25552300409`，尚无 secret。浏览器已有仓库管理登录，可继续到该环境配置页；本机没有可用的 GitHub CLI/API 凭据。
- 专用私钥位于本机仓库外 `~/.ssh/oak_github_actions_ed25519`，权限 0600；不要读取并输出内容。公钥指纹 `SHA256:tytkQoqj5aip33NoS41+KqzqSjKoYV9/NfIw5/wx2sk`，已以 restrict+固定 command 追加到服务器，原 authorized_keys 有备份。
- 专用密钥 `check` 成功；`id` 命令和端口转发均被拒绝。GitHub 生产环境已设仅 oak 分支部署，Secret 由用户录入。

## 尚待收尾核对

1. 提交并推送 oak 与 upstream-main，确认远端纯官方分支和 upstream/main 一致、旧 main 未变；用户已经授权，无需再次确认。
2. 将 GitHub 默认分支设为 oak，以提供手动触发入口。定制及 CI 不进入 upstream-main，不再向旧 main 挑选 CI 文件。
3. 实际运行一次 deploy=false 的 GitHub workflow，处理本次流程相关失败，不触发生产发布。
4. 用户在 https://github.com/Mengxisuki/new-api/settings/environments/25552300409/edit 添加 OAK_DEPLOY_SSH_KEY，步骤见 deploy/oak/README.md。用户明确自己录入，不代填私钥、不要求粘贴到聊天。
5. 完成后滚动更新此记录，仅保留私钥录入和首次实际发布等真实未完成项。

新程序启动后可能迁移数据库，健康失败必须停服务、保留备份并人工决策；禁止自动启动旧程序或自动恢复数据库。完整操作与边界见 `deploy/oak/README.md`。
