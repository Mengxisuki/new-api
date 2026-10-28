# Oak 手动 CI/CD 接入交接

更新时间：2026-10-28 09:31:00 +00:00。

配置和提交推送已获用户授权并完成；用户明确自己录入 GitHub 私钥，不代填、不输出私钥、不再次要求授权。生产发布仍由用户手动触发。

## 已完成与验证

- `oak` 是定制及 CI/CD 分支，已推送并设为 GitHub 默认分支；`upstream-main` 已推送，严格等于官方 `upstream/main` 的 `b08ee86fe`。旧 `main` 仍为 `246d62aa5`，内容未改。
- 功能提交 `5edda9c46` 恢复官网、采购和个人令牌路由接线；CI 提交 `cd94d9a8c` 提供仅手动触发的检查、构建和发布。后续文档提交不改变运行代码。
- 首次真实 GitHub 运行成功：https://github.com/Mengxisuki/new-api/actions/runs/40663034653 。检查、全量测试、生产构建、Linux 二进制、包校验及产物上传通过，deploy=false，发布任务跳过。运行源码为 `cd94d9a8c14526d6f9c44906ffc2ec6e1aed2712`。
- 服务器 `/usr/local/sbin/oak-ci-deploy` 已安装，摘要 `94ea9ab102c8c9fd6b35aa47beb5f3fd7e2b094e36b1647b17cf38b7910d3853` 与本地一致。线上仍 active，current 为 `/opt/new-api/releases/oak-01da3a55f`，没有执行发布或停机。
- 本机专用私钥 `~/.ssh/oak_github_actions_ed25519` 在仓库外、权限 0600，不输出内容。公钥指纹 `SHA256:tytkQoqj5aip33NoS41+KqzqSjKoYV9/NfIw5/wx2sk` 已以 restrict+固定 command 授权；原 authorized_keys 已备份。专用密钥 check 成功，任意 Shell 命令和端口转发被拒绝。
- GitHub 环境 `oak-production` 限定仅 `oak` 分支部署。用户自行录入 Secret，不读取其输入中的值；用户若已完成，只核对 Secret 名称。
- 本地冻结依赖验证此前通过前端 2468 用例、三个 Go 模块、类型检查和生产构建；本轮分支守卫、Bash 语法与 11 个部署测试通过，真实 GitHub 运行进一步验证了发布流程。

## 剩余用户操作

1. 在 https://github.com/Mengxisuki/new-api/settings/environments/25552300409/edit 点击 Add environment secret，Name 为 OAK_DEPLOY_SSH_KEY；本机执行 `pbcopy < ~/.ssh/oak_github_actions_ed25519`，将完整剪贴板内容粘贴到 Value 后保存。用户不需把私钥发送到聊天。
2. 在 Oak 手动构建与发布中选择 oak，显式勾选 deploy 才会发布。未获实际发布指令时不擅自切换线上程序。
3. 如用户要求继续上线，先核对环境与提交，再运行手动发布并验证实际站点版本。新程序启动可能迁移数据库，失败后不得自动启动旧二进制或自动覆盖数据库；备份恢复演练尚未执行。

官方更新先快进 upstream-main，再评估合入 oak；工作流和定制文件不得加入官方跟踪分支。操作步骤见 deploy/oak/README.md。
