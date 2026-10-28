# oak.qingshukeji.top 部署记录

2026-10-28（北京时间）通过 `ssh wdm` 部署当前源码提交 `01da3a55f`，版本标识为 `oak-01da3a55f`。这是全新实例，未迁移本地数据库、渠道或密钥。

公网地址：https://oak.qingshukeji.top 。服务器 `172.96.16.93`，Ubuntu 26.04，应用以独立的 `new-api` 系统用户运行。Caddy 转发至 `127.0.0.1:3000`，PostgreSQL 18.6 和 Redis 8.0.5 仅监听回环地址。UFW 仅放行 TCP 22/80/443 和 UDP 443；应用本身监听 3000，由防火墙阻止公网直连。

## 服务器文件

| 内容 | 路径 |
| --- | --- |
| 当前程序 | `/opt/new-api/current/new-api` |
| 本次发布 | `/opt/new-api/releases/oak-01da3a55f/` |
| 发布压缩包 | `/opt/new-api/releases/oak-01da3a55f.tar.gz` |
| 应用配置及密钥 | `/etc/new-api/new-api.env`，权限 0600 |
| systemd 单元 | `/etc/systemd/system/new-api.service` |
| 应用运行数据 | `/var/lib/new-api/` |
| 应用日志 | `/var/log/new-api/` 及 systemd journal |
| 数据库 | PostgreSQL `new_api`，角色 `new_api` |
| Caddy 配置 | `/etc/caddy/Caddyfile` |
| Caddy 证书存储 | `/var/lib/caddy/.local/share/caddy/` |
| 初始管理员凭据 | `/root/new-api-initial-admin.json`，权限 0600 |
| 用户提供的备用证书 | `/root/oak.qingshukeji.top_nginx.zip`，权限 0600 |

管理员用户名为 `admin`，随机密码已保存到本地忽略目录 `build/oak-01da3a55f/admin-credentials.txt`，权限 0600。首次登录后更改密码；初始凭据文件不会随改密更新。公开站点前已完成管理员初始化，首次安装入口已关闭。

## HTTPS

Caddy 2.6.2 自动申请并续期 Let's Encrypt 证书，HTTP 自动返回 308 跳转至 HTTPS。本次证书有效期至 2027-01-26，由 Caddy 自动续期。用户提供的 TrustAsia 证书也有效至 2027-01-26，保留为备用，当前站点使用 Caddy 自动管理的证书。

已配置 HTTPS Secure/HttpOnly/SameSite=Strict 刷新 Cookie、精确可信 Origin、随机且持久保存的 session/crypto 密钥。流式反向代理配置 `flush_interval -1`；实际模型流式请求需配置上游渠道后再验证。

## 日常运维

```sh
# 查看服务状态；四个服务均已启用开机自启。
ssh wdm 'systemctl status new-api caddy postgresql redis-server --no-pager'

# 查看应用与代理日志。
ssh wdm 'journalctl -u new-api -u caddy -n 100 --no-pager'

# 修改环境配置后重启应用；修改 Caddyfile 后先验证再 reload。
ssh wdm 'systemctl restart new-api'
ssh wdm 'caddy validate --config /etc/caddy/Caddyfile && systemctl reload caddy'

# 手动备份数据库；同时妥善备份密钥配置和运行数据。
ssh wdm 'umask 077; runuser -u postgres -- pg_dump -Fc new_api > /root/new_api.dump'
```

更新时在本机重新构建前后端，保留 LICENSE、NOTICE 和 THIRD-PARTY-LICENSES.md，上传至新的 `/opt/new-api/releases/<version>/`。先备份数据库、`/etc/new-api/new-api.env` 和 `/var/lib/new-api/`，再切换 `current` 符号链接并重启。不要覆盖持久密钥；新版本启动可能迁移数据库，不能仅凭切换旧二进制保证回滚。

## GitHub Actions 手动发布

仓库的 `.github/workflows/oak-deploy.yml` 沿用以上原生部署，不安装服务器构建工具或自托管 Runner。只支持 `workflow_dispatch`：不勾选 `deploy` 时仅检查并生成发布包，勾选后才连接生产服务器。代码推送不会触发上线。

构建在 GitHub 托管 Runner 上完成：冻结依赖安装、前端类型检查及测试、实际前端构建、后端回归测试、静态 Linux amd64 构建。`package-release.sh` 只打包二进制、LICENSE、NOTICE、THIRD-PARTY-LICENSES.md、VERSION 和 REVISION；服务器环境文件、数据库、管理员信息和私钥均不上传到构建产物。

### 首次接入

用户已授权本次远端接入、专用部署密钥和提交推送；GitHub 私钥由用户录入。以下步骤说明首次接入方式，日常发布无需重复配置。

1. 将已审查的工作流和 `deploy/oak/` 配置提交到 `oak`。GitHub 要求该工作流位于默认分支，本项目将 `oak` 设为默认分支。`upstream-main` 保持与官方 `upstream/main` 内容一致，不加入官网或部署文件，现有 `main` 保留。
2. 将处理器安装到固定位置，保持 root 拥有且应用用户不可写：

   ```sh
   ssh wdm 'install -o root -g root -m 0755 /dev/stdin /usr/local/sbin/oak-ci-deploy' < deploy/oak/ci-deploy.sh
   ssh wdm '/usr/local/sbin/oak-ci-deploy check'
   ```

3. 为本仓库生成独立 Ed25519 部署密钥；不要复用个人 `~/.ssh/id_ed25519`。公钥在服务器 `/root/.ssh/authorized_keys` 中必须限定为：

   ```text
   restrict,command="/usr/local/sbin/oak-ci-deploy" ssh-ed25519 <专用部署公钥> oak-github-actions
   ```

   `restrict` 禁止交互终端、端口转发和代理转发，固定处理器只接受 `check` 或严格校验参数的 `deploy` 命令。处理器本身不从发布包更新，应用二进制以既有 `new-api` 用户运行。
4. 在 GitHub 仓库创建环境 `oak-production`，将专用私钥保存为该环境的 `OAK_DEPLOY_SSH_KEY` secret。不要把私钥放入聊天、仓库、workflow 明文或普通变量；通过浏览器录入新凭据时由用户接手完成。
5. `known_hosts` 的 Ed25519 公钥已通过现有受信 `ssh wdm` 连接读取，并核对指纹 `SHA256:PEA/ySV7KRfOg+4Gkq2AeB1wGeVvvXucaGPt7rOa6bg`。如服务器重装导致密钥变化，先从可信管理通道核验新指纹再更新该文件，不能关闭主机校验或扫描后直接信任。

### 手动发布流程

进入 GitHub Actions →「Oak 手动构建与发布」→「Run workflow」，选择 `oak` 分支。工作流和生产环境均限定为该分支。先不勾选发布验证完整构建，再明确勾选 `deploy` 发布到现有域名。

发布处理器在 `flock` 锁内校验包摘要、固定文件清单、ELF 架构与二进制版本；禁止目录、软/硬链接、路径穿越及额外文件。预检完成后才停止旧应用，将 PostgreSQL 归档、环境配置和运行数据保存到 root 专用备份目录，再原子切换 `current` 并启动。数据库与 Caddy、Redis 配置保持原样，发布时应用会有短暂停机。

发布成功要求 systemd active、数据库连接可用，以及回环和公网 HTTPS `/api/status` 的版本与本次发布完全匹配。公开状态接口自身不执行数据库 Ping，脚本会另行检查 PostgreSQL。发布包和备份会保留，后续按容量与保留策略另行清理，本流程不自动删除历史数据。

### 失败与回滚边界

新程序尚未尝试启动时失败，可以恢复旧链接并启动旧服务。**只要已尝试启动新程序，就可能已执行数据库迁移**：此后失败会停止应用、返回错误并保留备份路径，不自动启动旧二进制或自动覆盖数据库。

此时先检查失败版本的迁移和日志，再决定修复前进，或在停机状态下成套恢复数据库、运行数据、配置和旧程序。`pg_restore --list` 仅验证归档目录可读，不代表已经完成恢复演练。

### 本地与服务器预检

```sh
# 只检查脚本和发布格式，不连接或改写线上服务。
bash -n deploy/oak/package-release.sh deploy/oak/ci-deploy.sh
python3 -B -m unittest discover -s deploy/oak/tests -v

# 不安装处理器也可通过已有管理连接执行只读预检。
ssh wdm 'bash -s -- check' < deploy/oak/ci-deploy.sh
```

配置文件存在不等于已启用 CI/CD：必须另外确认默认分支工作流、目标源码分支、服务器处理器、受限公钥和环境 secret 均已接通，并实际运行一次 GitHub workflow。

### 分支职责与官方更新

| 分支 | 职责 |
| --- | --- |
| `upstream-main` | 纯官方源码，跟踪 `upstream/main`；推送到个人仓库时使用 `origin`，不加入定制提交 |
| `oak` | 官网、采购配置及 CI/CD；作为 GitHub 默认分支，手动构建和发布只运行该分支 |
| `main` | 保留原有内容，不承担本次 CI/CD 入口 |

获取官方更新时先快进纯跟踪分支，评估差异后再合入定制分支：

```sh
git fetch upstream main
git switch upstream-main
git merge --ff-only upstream/main
git push origin upstream-main
git switch oak
git merge upstream-main
# 解决冲突并验证后再推送；推送本身不会发布。
git push origin oak
```

### GitHub 私钥录入（用户操作）

专用私钥保存在本机仓库外的 `~/.ssh/oak_github_actions_ed25519`，权限 0600。公钥已以受限命令方式安装在服务器，已验证 `check` 成功、任意 Shell 命令和端口转发被拒绝。

1. 打开 [oak-production 环境设置](https://github.com/Mengxisuki/new-api/settings/environments/25552300409/edit)，点击 **Add environment secret**。
2. Name 填 `OAK_DEPLOY_SSH_KEY`。在本机终端执行下方命令，将整个私钥复制到剪贴板；粘贴到 Value 并点击 **Add secret**。

   ```sh
   pbcopy < ~/.ssh/oak_github_actions_ed25519
   ```

3. 打开 [Oak 手动构建与发布](https://github.com/Mengxisuki/new-api/actions/workflows/oak-deploy.yml)，分支选 `oak`。先保持 `deploy` 不勾选验证仅构建，通过后再按需要手动勾选发布。不要把私钥填到普通 Variable 或聊天中。

### 当前接入状态（2026-10-28）

- 已在服务器安装 `/usr/local/sbin/oak-ci-deploy`，root:root、0755；服务器与本地 SHA-256 一致，只读 `check` 已通过。没有切换 `current`、重启线上应用或改写生产数据，线上仍为 `oak-01da3a55f`。
- GitHub `oak-production` 已创建，部署分支规则限定 `oak`；专用密钥已生成并完成服务器授权与限制验证，环境 Secret 等待用户录入。
- 官网/采购已单独提交为 `5edda9c46`，CI/CD 与说明单独提交到 `oak`。分支推送、默认分支切换和首次 GitHub 构建的最终状态见 [交接记录](../../docs/handoff.md)。
- 本地干净源码快照使用冻结依赖验证：前端 205 文件、2468 用例通过，根 Go 模块、relaykit 和 tokenkit 的 `make test` 通过；11 个部署契约测试、actionlint、Bash 语法检查、真实 Linux 发布包校验通过。已补齐采购图片接口在当前分支中的个人访问令牌权限声明。
- 本机原工作目录残留的被忽略旧源码和旧前端依赖曾造成全量检查失败；干净快照不包含这些内容，未通过删除用户文件或跳过失败测试来处理。
- 尚未执行实际停机发布和备份恢复演练。GitHub 私钥由用户录入，操作步骤见上文。

参考：[GitHub 手动运行工作流](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow)、[部署环境](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)、[OpenSSH 配置](https://man.openbsd.org/ssh_config)、[PostgreSQL pg_dump](https://www.postgresql.org/docs/current/app-pgdump.html)。

## 本次验证

- `bun run build`：生产前端构建成功；Go 1.26.10、`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off GOEXPERIMENT=greenteagc go build` 配合项目 PGO 配置及版本链接参数构建成功。
- 前后端资源和内置任务插件嵌入同一静态 Linux 二进制。服务器与本机 SHA-256 一致：`913451dec9d4fef09af6c9997dec85df2640611efcf48ebb323128e2c424e7e2`。
- PostgreSQL 新库迁移及应用重启通过；管理员和公开地址设置在重启后保留，Redis 正常连接。已验证服务启用状态，未执行整机重启。
- 公网 HTTPS 证书验证、HTTP 308 跳转、`/api/status` 和首页通过；已验证公网管理员登录、个人资料、刷新 Cookie 属性及退出登录。
- 初始 JavaScript/CSS 资源返回正确内容类型；3000/5432/6379 公网探测无服务响应，内核日志确认 UFW 拦截 3000 入站请求。
- `./node_modules/.bin/tsgo -b`：通过。仓库原有 `bun run typecheck` 使用 `bun check -b`，当前 Bun 报 `Script not found "check"`，因此使用已安装的 TypeScript 检查器验证。
- 仓库原有 `bun run lint` 及 `bun run format:check` 未通过，存在现有规则和格式错误；本次仅新增部署配置，未更改业务源码或修复无关检查错误。

模型上游渠道、支付、邮件和第三方登录均未配置。没有上游凭据，因此尚未验证真实模型请求。当前没有设置自动数据备份任务。
