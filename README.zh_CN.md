<div align="center">

![new-api](/web/public/logo.png)

# New API

**连接模型、应用与 Agent 的 AI 网关**

<p align="center">
  <strong>简体中文</strong> |
  <a href="./README.zh_TW.md">繁體中文</a> |
  <a href="./README.md">English</a> |
  <a href="./README.fr.md">Français</a> |
  <a href="./README.ja.md">日本語</a>
</p>

<p align="center">
  <a href="https://raw.githubusercontent.com/Calcium-Ion/new-api/main/LICENSE">
    <img src="https://img.shields.io/github/license/Calcium-Ion/new-api?color=brightgreen" alt="license">
  </a><!--
  --><a href="https://github.com/Calcium-Ion/new-api/releases/latest">
    <img src="https://img.shields.io/github/v/release/Calcium-Ion/new-api?color=brightgreen&include_prereleases" alt="release">
  </a><!--
  --><a href="https://hub.docker.com/r/CalciumIon/new-api">
    <img src="https://img.shields.io/badge/docker-dockerHub-blue" alt="docker">
  </a>
  <a href="https://atomgit.com/QuantumNous/new-api" target="_blank">
    <img alt="AtomGit G-Star" src="https://atomgit.com/QuantumNous/new-api/star/badge.svg"/>
  </a>
</p>

<p align="center">
  <a href="https://trendshift.io/repositories/20180" target="_blank">
    <img src="https://trendshift.io/api/badge/repositories/20180" alt="QuantumNous%2Fnew-api | Trendshift" style="width: 250px; height: 55px;" width="250" height="55"/>
  </a>
  <br>
  <a href="https://hellogithub.com/repository/QuantumNous/new-api" target="_blank">
    <img src="https://api.hellogithub.com/v1/widgets/recommend.svg?rid=539ac4217e69431684ad4a0bab768811&claim_uid=tbFPfKIDHpc4TzR" alt="Featured｜HelloGitHub" style="width: 250px; height: 54px;" width="250" height="54" />
  </a><!--
  -->
  <a href="https://atomgit.com/QuantumNous/new-api" target="_blank">
    <img alt="AtomGit G-Star" src="https://atomgit.com/QuantumNous/new-api/star/new_badge.svg" width="250" height="55" />
  </a>
</p>

<p align="center">
  <a href="#screenshots">项目截图</a> •
  <a href="#capabilities">核心能力</a> •
  <a href="#quick-start">快速开始</a> •
  <a href="#deployment">部署运维</a> •
  <a href="#development">开发扩展</a> •
  <a href="#documentation">文档社区</a>
</p>

</div>

---

## 📝 项目说明

New API 是面向应用、Agent 和团队的自托管 AI 网关。将不同厂商的模型服务接入统一入口，在同一套控制台中管理渠道、访问权限、用量与成本。

你可以用它为团队分配已授权的模型资源，在切换上游时减少客户端改动，或搭建自己的多模型服务。支持接入 OpenAI、Anthropic、Google Gemini、Azure OpenAI、AWS Bedrock、Vertex AI、DeepSeek、通义千问及其他兼容服务。

> [!IMPORTANT]
> - 本项目仅面向合法授权的 AI API 网关、组织内部鉴权、多模型管理、用量统计、成本核算和私有化部署场景。
> - 使用者必须合法取得上游 API Key、账号、模型服务或接口权限，并遵守上游服务条款及适用法律法规。
> - 使用者应确保其使用方式符合上游服务条款及适用法律法规。
> - 面向公众提供生成式人工智能服务时，使用者应遵守[《生成式人工智能服务管理暂行办法》](http://www.cac.gov.cn/2023-07/13/c_1690898327029107.htm)等监管要求，自行完成所在司法辖区要求的备案、许可、内容安全、实名、日志留存、税务和上游授权等合规义务。

<!-- -->

> [!WARNING]
> 将本项目作为面向公众的生成式 AI 服务或 API 转售服务运营时，使用者应先完成备案、内容安全、实名、日志留存、税务、支付和上游授权等合规义务。

---

<a id="screenshots"></a>

## 项目截图

以下账号、渠道、插件安装状态、用量、价格和费用均为模拟数据，插件市场展示官方目录。点击图片可查看原图。

| 数据看板 | 插件市场 |
| --- | --- |
| [![数据看板：模拟请求量、费用和性能指标](assets/screenshots/dashboard.zh-CN.jpg)](assets/screenshots/dashboard.zh-CN.jpg) | [![插件市场：官方插件目录与模拟安装状态](assets/screenshots/plugin-marketplace.zh-CN.jpg)](assets/screenshots/plugin-marketplace.zh-CN.jpg) |
| **模型广场** | **使用日志** |
| [![模型广场：模拟模型价格与可用状态](assets/screenshots/models.zh-CN.jpg)](assets/screenshots/models.zh-CN.jpg) | [![使用日志：模拟 Token 用量、费用和响应耗时](assets/screenshots/usage-logs.zh-CN.jpg)](assets/screenshots/usage-logs.zh-CN.jpg) |

---

## 🤝 我们信任的合作伙伴

<p align="center">
  <em>排名不分先后</em>
</p>

<p align="center">
  <a href="https://www.cherry-ai.com/" target="_blank">
    <img src="./docs/images/cherry-studio.png" alt="Cherry Studio" height="80" />
  </a><!--
  --><a href="https://github.com/iOfficeAI/AionUi/" target="_blank">
    <img src="./docs/images/aionui.png" alt="Aion UI" height="80" />
  </a><!--
  --><a href="https://bda.pku.edu.cn/" target="_blank">
    <img src="./docs/images/pku.png" alt="北京大学" height="80" />
  </a><!--
  --><a href="https://www.aliyun.com/" target="_blank">
    <img src="./docs/images/aliyun.png" alt="阿里云" height="80" />
  </a><!--
  --><a href="https://io.net/" target="_blank">
    <img src="./docs/images/io-net.png" alt="IO.NET" height="80" />
  </a>
</p>

---

## 🙏 特别鸣谢

<p align="center">
  <a href="https://www.jetbrains.com/?from=new-api" target="_blank">
    <img src="https://resources.jetbrains.com/storage/products/company/brand/logos/jb_beam.png" alt="JetBrains Logo" width="120" />
  </a>
</p>

<p align="center">
  <strong>感谢 <a href="https://www.jetbrains.com/?from=new-api">JetBrains</a> 为本项目提供免费的开源开发许可证</strong>
</p>

---

<a id="capabilities"></a>

## 核心能力

| 方向 | 可以做什么 |
| --- | --- |
| 模型接入 | 支持 OpenAI Chat Completions、Responses、Anthropic Messages 和 Gemini 协议，以及上游支持的流式输出、工具调用、推理与多模态输入 |
| 渠道调度 | 配置模型映射、渠道优先级与权重、失败重试、渠道亲和性和多密钥管理 |
| 用量与成本 | 管理额度、订阅套餐、用量日志、缓存计费，以及基于表达式的阶梯定价 |
| 访问控制 | 管理用户、分组、细粒度权限和 API Key 限制；支持 OAuth/OIDC、通行密钥、两步验证与登录会话管理 |
| 异步任务 | 通过 JavaScript 插件扩展图片、视频等任务 API，统一查询任务状态和获取产物 |
| Web 控制台 | 配置渠道与模型、查看用量和审计日志、在 Playground 中调试模型；支持简体中文、繁体中文、英语、法语、日语、俄语和越南语 |

### 协议与接口

| 接口类型 | 常用入口 |
| --- | --- |
| OpenAI Chat / Responses | `POST /v1/chat/completions`、`POST /v1/responses` |
| Anthropic Messages | `POST /v1/messages` |
| Gemini | `POST /v1beta/models/{model}:generateContent`、`POST /v1beta/models/{model}:streamGenerateContent` |
| Realtime / Responses WebSocket | `GET /v1/realtime`、`GET /v1/responses`（WebSocket 升级） |
| 图片 / 音频 | `/v1/images/generations`、`/v1/images/edits`、`/v1/audio/speech`、`/v1/audio/transcriptions`、`/v1/audio/translations` |
| 向量 / 重排 | `POST /v1/embeddings`、`POST /v1/rerank` |
| 任务插件 | `POST /v1/tasks/{pluginKey}`、`GET /v1/tasks/{taskId}`，以及各插件声明的协议路由 |

[RelayKit](./relaykit/README.md) 提供上述四种文本协议之间的请求、响应和流式转换。实际可用能力取决于渠道、上游模型和转换路径；协议特有的工具与字段可能无法完整映射。WebSocket 同样需要上游与渠道配置支持。

本 README 描述当前源码的能力，部署时请同时查看所选版本的发布说明。

<a id="quick-start"></a>

## 快速开始

### 使用 Docker 本地体验

以下命令使用 SQLite 启动单实例，仅监听本机端口：

```bash
mkdir -p data
docker run --name new-api -d --restart unless-stopped \
  -p 127.0.0.1:3000:3000 \
  -e TZ=Asia/Shanghai \
  -v "$(pwd)/data:/data" \
  calciumion/new-api:latest
```

打开 [http://localhost:3000](http://localhost:3000)，按初始化向导创建管理员账号。SQLite 数据库存放在挂载的 `data` 目录中，更换容器后仍会保留。

### 发起第一次请求

1. 新建渠道，填写上游 API Key、可用模型和所属分组，执行渠道测试。
2. 配置模型价格，确保调用用户有可用额度或有效订阅。
3. 在控制台创建 API Key，确保它能访问对应分组和模型。
4. 对于 OpenAI 兼容客户端，将 Base URL 设为 `http://localhost:3000/v1`，密钥使用 **New API 签发的 Key**。

在终端中将 `NEW_API_KEY` 环境变量设为该密钥，查询它可以访问的模型：

```bash
curl --fail-with-body http://localhost:3000/v1/models \
  -H "Authorization: Bearer ${NEW_API_KEY}"
```

然后调用 Responses，将 `your-enabled-model` 替换为已启用且支持该接口的模型名称：

```bash
curl --fail-with-body http://localhost:3000/v1/responses \
  -H "Authorization: Bearer ${NEW_API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"model":"your-enabled-model","input":"Hello!"}'
```

<a id="deployment"></a>

## 部署与运维

### Docker Compose

仓库的 [Compose 配置](./docker-compose.yml) 默认启动 **New API + PostgreSQL + Redis**，并提供 MySQL 和独立 ClickHouse 日志库的配置示例。

```bash
git clone https://github.com/QuantumNous/new-api.git
cd new-api
```

启动前编辑 `docker-compose.yml`：同时替换数据库、Redis 服务及对应连接串中的示例密码，并设置固定的随机 `SESSION_SECRET`（可用 `openssl rand -hex 32` 生成）。通过 HTTPS 访问控制台时，设置 `SESSION_COOKIE_SECURE=true`，并在 `SESSION_COOKIE_TRUSTED_URL` 中填写控制台对外的精确 HTTPS Origin。

```bash
docker compose up -d
docker compose logs -f new-api
```

### 存储与配置

| 组件 | 可选方案 |
| --- | --- |
| 主数据库 | SQLite、MySQL ≥ 5.7.8、PostgreSQL ≥ 9.6 |
| 独立日志库 | 通过 `LOG_SQL_DSN` 配置，额外支持 ClickHouse |
| 缓存 | 可选 Redis 与内存缓存；多节点需要共享限流额度时使用共享 Redis |
| 容器平台 | Linux amd64 / arm64 |

| 环境变量 | 用途 |
| --- | --- |
| `SQL_DSN` | 主数据库连接串；未设置时使用 SQLite |
| `LOG_SQL_DSN` | 可选的独立日志数据库连接串 |
| `REDIS_CONN_STRING` | Redis 连接串 |
| `SESSION_SECRET` | 持久保存的鉴权密钥，所有节点必须一致 |
| `CRYPTO_SECRET` | 默认使用 `SESSION_SECRET`；共享 Redis 的节点必须使用相同的有效值 |
| `SESSION_COOKIE_SECURE` | HTTPS 控制台设为 `true`，启用 Secure 刷新 Cookie 和严格的刷新／退出来源校验 |
| `SESSION_COOKIE_TRUSTED_URL` | Secure 模式必填：以逗号分隔的精确 HTTPS Origin，不含路径或通配符；本地 HTTP 模式不设置 |
| `TRUSTED_PROXIES` | 可信反向代理 IP/CIDR，或 `none`；按实际网络显式配置 |

完整配置见[环境变量示例](./.env.example)、[环境变量文档](https://docs.newapi.ai/zh/docs/installation/config-maintenance/environment-variables)和[鉴权与登录会话说明](./docs/authentication.md)。容器变量应通过 Compose 的 `environment` 或 `env_file` 注入；只复制 `.env.example` 不会自动将变量传入容器。

正式部署时使用 HTTPS，并配置反向代理支持流式响应与 WebSocket 升级。持久化并备份数据库和挂载数据。多节点必须共用主数据库和鉴权密钥；独立 Redis 或内存限流器会按节点分别计数。不同拓扑下的会话传播行为见鉴权文档。

从[发布页](https://github.com/QuantumNous/new-api/releases)选择明确的镜像版本，阅读升级说明并先备份再升级。`latest` 会随发布构建变化；已有实例应按实际版本评估迁移与兼容性。

<a id="development"></a>

## 开发与扩展

后端使用 Go 和 Gin；控制台使用 React 19、TypeScript、Rsbuild、TanStack 与 Tailwind CSS 4。前端依赖和脚本使用 Bun；Go 语言基线见 [go.mod](./go.mod)，容器构建工具链见 [Dockerfile](./Dockerfile)。

### DeepSeek Chat 推理分离

`relay/channel/openai/reasoning_output.go` 在客户端协议转换之前统一处理真实上游模型名以 `deepseek-` 开头的 Chat 返回。`<thinking>`、`<think>`、`<reasoning>`、`<thought>` 区块及可确定的重复推理从正文移到 `reasoning_content`；孤立闭合标签之前的整段也归入推理。原生 Chat 与转换成 Responses、Claude、Gemini 的普通/流式返回共用该处理。没有标签或独立推理对照的自然语言不做语义猜测。

为识别跨事件标签、迟到推理及整段重复，DeepSeek 的 SSE 文本在对应 choice 结束后释放，正文和推理的显示会延后；角色、工具及其他事件继续转发。这条路径不执行“推理转正文”。缓冲沿用 `STREAM_SCANNER_MAX_BUFFER_MB` 的大小限制，默认 128 MiB；被清理后只剩推理且上游声称正常结束、缓冲超限或缺少文本终态时明确失败，禁止自动重试。成功响应保留上游用量；缺失用量时仍使用清理前的生成文本估算。

反方向 Responses→Chat 的正文提取位于 `relaykit/relayconvert/internal/oai_responses/to_oai_chat_resp.go`，只接受 assistant message 的 `output_text`；无正文时不从 reasoning 或工具项目兜底。

定向验证（仓库根目录）：

```bash
go test ./relay/channel/openai -run 'TestDeepSeekReasoning|TestResponsesReasoningOnly' -count=1
```

后端会嵌入 `web/dist`，首次启动前先构建前端：

```bash
# 仓库根目录
cd web
bun install --frozen-lockfile
bun run build
cd ..
go run .
```

在另一个终端启动前端开发服务器：

```bash
cd web
bun run dev -- --port 5173
```

访问 [http://localhost:5173](http://localhost:5173)，开发服务器会将 API 请求代理到 3000 端口的后端。需要容器化开发后端时，参阅 [docker-compose.dev.yml](./docker-compose.dev.yml) 和 [makefile](./makefile) 中的 `make dev`。

| 目录 | 职责 |
| --- | --- |
| `router/`、`middleware/`、`controller/` | HTTP 路由、访问校验与 API 处理 |
| `relay/` | 上游适配与请求调度 |
| `service/`、`model/` | 业务逻辑与持久化 |
| [relaykit/](./relaykit/README.md) | 可独立构建的协议 DTO 与转换 Go 模块 |
| [plugins/tasks/](./plugins/tasks/) | JavaScript 任务插件；编写方式与宿主边界见 [Task Plugin API v1](./docs/plugin-api/v1.md) |
| `web/` | Web 控制台，参阅[前端开发约定](./web/AGENTS.md) |
| [electron/](./electron/README.md) | 桌面封装与打包 |

### Oak 官网与采购联系配置

默认首页位于 `web/src/features/home/components/oak-landing.tsx`，局部主题和响应式样式位于 `web/src/styles/oak-home.css`。后台配置的自定义首页内容仍优先显示。首屏“文档”按钮与接入指南均使用「系统设置 → 站点设置 → 系统信息 → 文档链接」的配置；未配置时首屏文档按钮禁用。

服务选型区位于 `web/src/features/home/components/oak-services.tsx`，依次介绍量贩 Token、包月订阅、账号挂靠与卡密代充值。量贩入口沿用 `/pricing`，挂靠入口为 `/hosting`，其余未自助开通的服务定位到采购咨询区；每周 150 元仅为额度示例，不代表已配置的套餐或月费。挂靠已提供本地实现，真实账号与生产部署仍需验收；外部代充值尚未实施，卡密不等同于平台余额兑换码。

### Oak 免费账号挂靠与统一资源顺序

入口 `/hosting` 管理多个 ChatGPT/Codex 账号、逐账号额度和手动原厂重置机会。额度采用紧凑已用比例和重置倒计时展示，不显示积分或费用；连接测试使用该账号真实 Responses 请求，会少量消耗原厂额度。连接流程为备注 → 生成授权链接 → 浏览器授权 → 粘贴完整回调或 Code → 完成授权并添加；保存后自动执行一次短模型测试，通过后才进入调度池；失败或结果未知时保留账号但不参与调度，用户可从详情重试。重新授权同样自动测试，重测会先撤销旧验证，测试不自动重试或消费重置机会。在 `/keys` 的创建/更新抽屉中、分组上方设置资源使用顺序，可拖拽、键盘排序或关闭来源。新 Key 默认“按量计费 → 订阅额度 → 自有账号”；旧 Key 普通编辑保留原授权。例如 A 先自有账号、B 先订阅额度，两者互不影响；订阅页不再提供全局偏好。具体 Key 菜单仍提供 Codex 接入配置。挂靠调用免费，平台来源按实际选择结算。

New API 配置 `OAK_GPT_LOAD_URL`（默认 `http://127.0.0.1:3001`）与持久、显式不少于 32 字节的 `CRYPTO_SECRET`；gpt-load 配置 `OAK_NEW_API_URL`（默认 `http://127.0.0.1:3000`）。不再需要 `OAK_GPT_LOAD_MANAGEMENT_KEY`，也不向 New API 提供 gpt-load `AUTH_KEY`。New API 为具体用户请求签发 30 秒授权，执行层回查 New API 核验用户和请求绑定。两个服务需成套升级；旧全局顺序会原样迁移到现存无独立策略的 Key，新建 Key 不继承，未配置策略的 Key 保持既有平台规则。

添加账号或重新授权保存成功后，页面自动查询一次原厂额度；失败可单独重试，不重复添加账号。额度下方提供查询额度、查询重置次数及使用重置次数三个入口；两种查询复用联合观测，使用重置次数继续要求确认与安全验证。连接测试展示实际发送的固定提示词和模型文本，空回复不会算验证通过。实现与验证见[额度快捷操作与测试内容](docs/plans/account-hosting.md#17-额度快捷操作与测试内容)。

源码导航、实际接口、测试命令、回退边界与上线验收见 [Oak 账号挂靠实施记录](docs/plans/account-hosting.md#14-本地实施结果与上线交接)。自有账号首期只执行同步 Chat/Responses/compact；其他接口跳过自有账号，继续使用原平台适配器并遵守当前 Key 的按量/订阅顺序，未启用平台来源则明确拒绝。执行层确认处理前拒绝或冷却未发送时，可凭可信标记按当前 Key 顺序继续后续来源；普通 429、超时或已输出后仍不跨来源重放，不自动耗重置机会。安全顺延的挂靠拒绝归入最终请求日志的 `resource_attempts`，使用面板只产生一条最终记录，详情保留账号、HTTP 状态与执行层关联 ID。全部授权来源不可用或发生终止错误时仍记录最终失败；普通 429 没有安全证据时不会重放。旧 `hosting_execution_rejected` 日志在列表、详情和导出中解释为可读原因，历史记录不迁移或删除。限流/冷却不等于已经确认额度耗尽。本次修复的证据、验证及配套发布要求见[挂靠顺延排障记录](docs/diagnostics/hosting-resource-fallback.md)。平台 Claude/Gemini 等协议不受挂靠首期范围限制；Claude 自有账号、文件导入及真实原厂验收仍不在已验证范围。接口契约和本轮验证见[抽屉与全接口兼容](docs/plans/account-hosting.md#15-api-key-抽屉资源顺序与平台接口兼容)。

超级管理员可在「系统设置 → 站点设置 → 系统信息」维护采购联系人、微信号和二维码。联系文字通过页面保存按钮提交；二维码上传或删除立即保存。图片支持 PNG、JPEG、WebP，最大 1 MiB，宽高均不超过 4096 像素；空字段在首页隐藏，此配置与微信登录二维码独立。

“徽标图片”也使用上传、预览、替换和删除控件，不再填写图片 URL，限制与采购二维码相同；上传和删除立即保存，普通设置保存不会回写旧图片。徽标写入接口为 `POST/DELETE /api/option/logo`（仅超级管理员），公开图片通过 `GET /api/status/logo` 读取。现有页头、页脚及 favicon 继续从公开状态获取徽标；删除后恢复默认展示。

首页通过共享 `/api/status` 获取联系信息，图片由 `/api/status/procurement-wechat-qr` 公开读取，后端资源入口位于 `controller/procurement_contact.go`、`controller/logo.go`，共用 `controller/option_image.go` 校验与图片存储，复用现有系统选项。接手背景见 [项目记忆](./docs/memory/product.md)。

定向验证：在仓库根目录运行 `go test ./controller ./router -run TestProcurementContactOptions -count=1`；在 `web/` 下运行 `bun run test src/features/home/__tests__ src/features/system-settings/general/__tests__/procurement-contact-upload.test.tsx src/features/system-settings/general/__tests__/procurement-contact-schema.test.ts src/features/system-settings/general/__tests__/logo-upload.test.tsx`。

### Oak 订阅续期与官方微信支付

同一套餐仍有效时再次付款会从原到期时间延长一个套餐周期，不新增并行订阅，不补满当前周期额度，也不改变原重置时间。已到期或取消后重新购买按新开通处理；套餐原有历史限购规则仍有效。到期前已创建的续费订单若在自然到期后才到账，仍能兑现付款。管理员授权入口保留；用户购买和续费仅能走支付网关，已删除 `/api/subscription/balance/pay`、前端余额兑换和套餐余额支付开关。钱包余额继续用于原有按量模型调用。

人民币（CNY）模式下，微信、支付宝及易支付的充值输入直接表示元：无分组倍率或优惠时，输入 1 即支付 1 元、到账 1 元平台额度，不再乘 `Price` 的美元充值售价。已有分组倍率与面额优惠仍作用于应付金额；`USDExchangeRate` 仅用于把这份人民币面额换成内部额度。Stripe、Waffo、Pancake 等保留原计价合同。预设、当前付款方式与确认框统一显示“充值面额 → 实际应付 → 到账平台额度”，到账取 `/api/user/amount` 新增的 `credited_quota` 字段，不在浏览器自行猜测。切换金额、渠道或汇率会重新报价，报价失败或未完成时不允许确认旧金额。

新易支付订单与官方订单都冻结到账额度，回调与人工补单共用冻结值，不受下单后汇率变化影响；历史零冻结值易支付订单仍按其原美元数量结算，不把旧单追溯解释为人民币。相关回归：`go test ./controller -run 'TestCNYTopUp|TestLocalTopUpKeeps' -count=1`，三库 `go test ./model -run '^TestEpayFrozenQuotaDatabaseMatrix$' -count=1`，前端 `bun run test src/features/wallet/__tests__/currency-payment.test.tsx`。

官方支付功能从 `dev/v1.0.0` 的 `504c53567`、`c2a94666d` 相关实现迁入，复用其支付宝/微信渠道框架，仅新增原功能所需 `wechatpay-go v0.2.21`。配置入口为「系统设置 → 计费 → 付款设置」的官方支付渠道：微信 Native 二维码或 H5 跳转需要商户号、AppID、商户证书序列号、商户私钥、API v3 密钥、微信支付公钥 ID 和公钥；H5 还需匹配微信商户端已配置的域名及场景资料。私钥和密钥不会通过用户接口公开，配置页返回脱敏占位，留空保存保留已存凭据。此功能与微信登录相互独立。

官方网关订阅通过 `POST /api/subscription/official/pay` 创建独立订单；充值仍用 `POST /api/user/pay`。微信通知地址为 `/api/user/wechat-pay/notify/<渠道ID>`，支付宝为 `/api/user/alipay/notify/<渠道ID>`，基址沿用站点的支付回调地址配置。未覆盖通知地址时自动生成上述路径；填写自定义通知地址时须准确指向对应渠道路径。服务端执行平台公钥验签、商户/AppID、渠道及金额匹配；订阅收款只开通或延长订阅，充值收款只增加下单冻结的额度。成功订单重复通知不会重复发放。

官方充值订单的十分钟窗口用于限制继续付款；已验签成功的迟到通知不会仅因本地窗口结束被拒绝。账单可继续支付本人未过期的官方充值单，服务端先关闭原上游订单，再创建冻结金额和额度不变的新单。`top_ups.credited_quota` 保存冻结额度，由现有 AutoMigrate 新增；旧订单及历史 `allow_balance_pay` 列不删除，不据旧列恢复余额购买。

验证命令：`go test ./model ./controller ./router ./service ./setting/operation_setting -run 'Official|Subscription|AccessTokenRouteRules|TopUp|Topup|Epay|Payment' -count=1`；配置本地专用 `TEST_MYSQL_DSN`、`TEST_POSTGRES_DSN` 后，同一命令执行三库迁移、到账和续期矩阵。在 `web/` 下运行 `bun run test src/features/wallet/__tests__ src/features/wallet/hooks/use-payment.test.ts src/features/wallet/lib/payment.test.ts src/features/system-settings/integrations/__tests__/official-payment.test.tsx`、`bunx tsgo -b` 和 `bun run build`。当前未执行真实商户付款或生产发布，收款前仍需通过真实商户配置完成端到端联调。

### Oak 人工代充值

用户入口 `/recharge`，后台入口 `/recharge-orders`（代充值订单管理）与 `/recharge-products`（商品管理）。商品配置名称、套餐简介、服务商、套餐周期、预计处理时间、图标、购买说明、人民币售价（后台以分输入，15000 表示 150 元）、排序、上下架及“允许平台余额支付”，默认关闭余额支付。简介填写套餐权益，购买说明填写账号条件和退款规则；图标复用模型广场选择器，可搜索或填写 OpenAI、Claude.Color 等已安装图标键并预览，不是图片 URL。不会自动创建示例商品或定价。

订单详情在桌面端左侧显示购买信息与独立的补充资料原因/处理结果，右侧显示步骤和表单；小屏上下排列，弹窗限高并可滚动。展示字段随下单保存快照，后续商品编辑不改写已购信息；升级前的描述保留为购买说明，新增字段为空，在商品管理中补填。

买家订单详情在“订单信息”下方独立显示“发票信息”：未申请时明确提示；已申请时显示冻结抬头、开票金额、状态、发票号码及开票备注。财务开具并上传附件后可直接下载，刷新订单会同步开票状态。文件继续走本人鉴权接口，业务员订单详情不扩大为财务资料访问入口。

超级管理员可在“系统设置 → 计费与支付 → 代充值设置”（`/system-settings/billing/recharge`）保存“开启代充值接单”。关闭会阻止新报价、新订单和未付订单继续支付；已付订单资料提交、办理、退款及已签发支付的通知照常处理。页面分别显示账户资料加密、支付确认和微信渠道是否就绪，缺失配置有具体指引。开关默认保持升级前接单行为；只有密钥与支付前置配置有效才实际开放购买。

启用前在 New API 服务环境中设置 `OAK_RECHARGE_DATA_KEY`：标准 Base64 编码的 32 字节随机密钥，可用 `openssl rand -base64 32` 生成并直接存入服务器机密配置。此密钥独立于登录密钥，必须持久备份；有待办订单时不能直接更换，否则旧资料无法解密。缺失或格式错误时停止新购及资料接收。沿用现有支付配置确认、微信商户/网关配置和 HTTPS 回调地址；不需要 GPTLoad 或供应商接口改造。

微信支持易支付的 `wxpay` 和既有官方微信 Native/H5 渠道。易支付异步通知为 `/api/account-recharge/epay/notify`，官方微信继续使用 `/api/user/wechat-pay/notify/<渠道ID>`；订单号前缀 `RCHUSR` 用于区分代充值。回跳只回到 `/recharge`，不能代表付款成功。付款和退款只影响本业务订单，不增加钱包额度或开通平台包月；允许余额的商品按原单冻结额度扣款、退款。

用户选择套餐并确认支付方式后创建订单，从详情继续支付。付款成功后，按页面指引在同一浏览器登录 ChatGPT、打开 session 地址，将完整 JSON 和充值账号邮箱提交到订单。资料提交后显示“订单处理中”；未填写或需更新资料时可继续原订单。详情不显示上游受理或进度，也不发送上游 API 请求。

Root 在用户管理中“授予业务员角色”（角色值 3），经过既有单次验证后生效；旧会话失效，需重新登录。业务员只能进入订单队列，领取订单、验证后查看/复制本人订单资料、要求补交、填写内部备注、标记异常、申请退款及确认完成。管理员可转派、维护商品和审核退款。财务角色不会自动获得代充值资料权限。

办理时先领取再查看资料；结果不确定时保持处理中并填写内部备注。补资料会撤下旧凭据，用户重交仍交给原业务员。完成必须确认实际结果并填写说明，完成及退款终态清除在线密文。敏感弹层一分钟、失焦、订单版本变更或权限查询失败时关闭；资料不进入普通列表、日志或浏览器持久缓存。已复制到外部的内容不能撤回，历史备份按既有保留政策管理。

退款先由管理员批准，停止继续办理和读取资料；随后确认退款。余额按原单实际扣除整数返还，缓存同步失败可在原单重复确认恢复，不重复加额。微信须先在支付网关完成原路退款，再输入真实退款凭证号登记确认，此按钮不自动调用网关退款。微信开票沿用常用抬头和收费快照；余额支付只提供本次订单消费记录，避免重复开票。

代码、数据库验证和上线边界见[代充值实施记录](docs/plans/account-recharge.md#12-本地实施结果与验证)。本轮为本地实现，尚未生产部署或完成真实商户付款验收。

### 发票与财务工作区

钱包的「订单查询」每次打开都重新请求当前订单；关闭时停止读取，不复用上次弹窗打开前的订单响应。订单弹窗另有「我的发票」分页列表，可查看对应付款订单、处理进度和已开票文件。

常用抬头在「个人资料」配置，支持个人/企业抬头；企业税号和接收邮箱会在服务端校验。付款方式前的「是否需要发票」默认否，选择是且未配置抬头时提供同一表单的快捷配置。下单时保存独立抬头快照，后续编辑常用抬头不会修改已付款申请。税务和银行资料不写浏览器持久存储。

超级管理员在「系统设置 → 计费 → 付款设置 → 开票」设置默认加价百分比及分组覆盖。`invoice_setting.default_rate` 用 `6` 表示 6%，`invoice_setting.group_rates` 为分组到百分比的映射；显式 `0` 表示该组免加价，没有覆盖时使用默认率。范围 0–100，最多四位小数。费用按优惠后的原应付金额计算，四舍五入到分，并计入本笔实付；不增加平台到账额度。例如充值 100 元、无优惠、开票加价 6%，支付 106 元、到账 100 元额度。报价、订单、支付回调和继续支付均使用服务端冻结金额与资料。

开票支持人民币本地微信、支付宝、易支付充值，以及官方商户/易支付订阅购买与续费；Stripe、Creem、Waffo 等尚未接通开票的支付方式会明确拒绝开票请求。选择否继续原付款流程，不创建发票申请或附加收费。新增报价字段为 `invoice_required`、`invoice_base_amount`、`invoice_fee_percent`、`invoice_fee_amount`，原应付 `data` 与到账 `credited_quota` 仍保留。

财务人员进入「财务 → 开票管理」，只处理实际支付成功的申请。可以开始处理、填写发票号并上传 PDF/PNG/JPEG（最大 5 MiB），或填写原因后拒绝；上传成功且号码完整才显示已开具。已开具/拒绝为当前流程终态，不提供自动重开或自动退款，拒绝不会自动退还附加费。用户和财务分别通过受保护接口下载，附件不公开托管。附件目录为运行工作目录下 `data/invoices/`，原生部署对应 `/var/lib/new-api/data/invoices/`，备份时需保留此目录及数据库。

Root 可在用户管理中创建财务账号或通过原有单次验证将现有账号设为财务。系统角色值 `5` 低于普通管理员 `10`，默认只有 `finance:read/write`，不授予渠道、用户管理、系统设置或人工补余额能力；角色变更会撤销旧会话与遗留管理覆盖。普通管理员默认可办理财务事项，Root 可通过现有权限编辑器调整；财务账号的个人令牌也只能授权其真实财务能力。

核心位置：`model/invoice.go` 保存抬头、申请和状态规则，`controller/invoice*.go` 提供资料/报价/文件接口，`setting/operation_setting/invoice_setting.go` 保存费率，`service/authz/resources_finance.go` 定义权限，前端 `web/src/features/invoices/` 组织公共资料表单与财务页面。新表 `invoice_profiles`、`invoice_requests` 和订单的 `invoice_required` 字段由既有迁移流程注册；旧订单默认不开票，不追溯加价。

验证：配置隔离 `TEST_MYSQL_DSN`、`TEST_POSTGRES_DSN` 后执行 `go test ./controller -run '^TestInvoice' -count=1`；权限回归执行 `go test ./service/authz ./middleware -count=1`。在 `web/` 下执行 `bun run test src/features/wallet/__tests__/invoice-payment.test.tsx src/features/invoices/__tests__/admin.test.tsx src/features/system-settings/integrations/__tests__/invoice-rates.test.tsx src/features/users/components/__tests__`、`bunx tsgo -b` 和 `bun run build`。本实现记录人工开票与文件交付，不对接税务开票服务商；发布前应完成真实商户付款、财务上传和用户下载联调。

### 财务总览（第 1 期）

入口为「财务 → 财务总览」（`/finance/overview`）。默认只有 Root 可见。Root 在用户管理中编辑管理员或财务账号，在权限区勾选「财务总览 → 查看财务总览」，经原有单次身份验证保存后生效；普通用户和业务员不能获得此能力。`finance_overview:read` 与既有 `finance:read/write` 相互独立，能开票或结清挂帐不代表能查看经营汇总。接口只接受网页登录会话，不对个人访问令牌开放；侧栏隐藏设置不授予或撤销接口权限。

总览包括实收、退款、实收减退款、开票附加费说明、钱包额度合计、时段消耗，以及按业务线的每日收入、收款渠道构成、当前挂帐账龄、欠款最多的用户和待办。支持今天、近 7/30 天、本月、上月、自定义日期（最长 366 天），统一按上海自然日统计；趋势图提供等价的逐日表格视图。挂帐、钱包额度与待办是当前快照，不随查询时段消失。

口径与边界：

- 钱包充值和客户订阅按成功订单到账时间计入，订阅不按周期摊销。订阅写入 `top_ups` 的同交易号账单镜像会被排除，避免重复统计。
- 代充值只计外部收款；钱包支付不重复计收入，挂帐订单直到财务登记结清时才计入。结清后退款保留原收款历史，在登记退款时冲减。
- 退款只包括平台内登记的代充值现金退款和已结清挂帐退款。充值/订阅在商户后台直接退款，目前缺少平台登记来源，不在统计中。
- 微信、支付宝官方渠道和易支付按本地人民币合同合计。其他网关单列订单存储值，不当作已核实现金；例如 Stripe 的记录金额可能是额度面额。历史钱包订单的渠道不明时也不猜币种。
- 开票附加费已经包含在实收里，说明金额不重复相加。钱包余额包含赠送、兑换和手动调整，不能当作现金余额或实收；额度显示沿用站点既有单位。消耗依赖消费日志与看板统计均开启，按小时汇总并存在既有缓存落库延迟，不是实时财务账本。
- 本页不是与微信商户账单完成核对后的正式财务报表。第一期未覆盖的手动调额分类、充值/订阅自助挂帐、坏账核销和人工补单凭证现已在下方「第 2 期」接入；历史无凭证的成功补单不自动回填。兑换码售赠分类、支出台账及订阅成本覆盖周期仍属于后续期。

代码导航：`model/finance_overview.go` 负责只读聚合，`controller/finance_overview.go` 校验时间范围，`GET /api/finance/overview?start=<Unix秒>&end=<Unix秒>` 返回左闭右开时段；`service/authz/resources_finance.go` 定义总览独立权限，前端在 `web/src/features/finance-overview/`。本期不新增业务表、迁移或依赖。

验证命令：`go test ./controller -run '^TestFinanceOverview' -count=1 -v`，设置回环隔离 `TEST_MYSQL_DSN`、`TEST_POSTGRES_DSN` 执行三库查询矩阵；另以 `TEST_SECURITY_DIALECT=mysql` / `postgres` 运行 `TestFinanceOverviewPermissions`。前端在 `web/` 执行 `bun run test src/features/finance-overview src/features/users/components/__tests__/permissions.test.tsx src/hooks/__tests__/sidebar-config.test.tsx`、`bun x tsgo -b` 和 `bun run build`。已实际验证 SQLite 3.50.4、MySQL 8.4.2、PostgreSQL 14.17，使用临时实例和虚构数据完成 Root 登录、接口金额核对、页面明暗模式/表格切换、Root 授予财务角色并完成二次验证；没有执行真实收款或生产发布。

受影响前端 23 文件 224 项测试、类型检查、定向 lint、前后端构建和后端总览/权限竞态检查通过；390 像素手机视口无页面横向溢出。前端全量测试另有一项既有失败：`src/features/task-plugins/__tests__/marketplace-install-dialog.test.tsx` 的源码高亮断言，已在未修改的 HEAD 代码上复现；未扩改该模块。授权设计参考 [OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html) 的默认拒绝、服务端逐请求校验和最小权限，不宣称完成全项目 ASVS 合规认证。

#### 订阅周期甘特图

财务总览底部新增「订阅周期甘特图」，仅包含「平台用户订阅」和「平台号池」，不维护挂靠账户周期。可选全部或单类，独立选择展示日期（默认完整上海自然月，最长 366 天），每页 20 条。显示与窗口相交的订阅、完整起止日期、未开始/生效/过期/取消状态及今天虚线；长周期在图上裁切，原日期保留，手机在表格内横向滚动。

平台用户周期自动读取 `user_subscriptions`，包括购买和管理员开通的订阅；同一用户的多份订阅分别显示。号池由 Root 点击「新增号池周期」，填写号池/账号名称、开始日、结束日，结束日包含当天。行末编辑与删除只维护人工展示记录，不影响上游账号、用户权益或收入统计。续订可新增下一条周期以保留历史。已有总览查看权限的管理员和财务可以读取，写入仅允许 Root 登录会话，个人访问令牌不支持。

实现导航：`model/finance_subscription.go` 的 `FinancePoolSubscription` 新增 `finance_pool_subscriptions` 表，经 `model/main.go` 启动迁移；`controller/finance_subscription.go` 与 `router/api-router.go` 提供读取、创建、修改和删除入口，`middleware/access_token_routes.go` 声明会话边界。`GET /api/finance/subscriptions` 接受 `start/end`（左闭右开 Unix 秒）、`category=all|user|pool`、`page`；`POST /api/finance/pool-subscriptions` 和 `PUT/DELETE /api/finance/pool-subscriptions/:id` 维护人工配置。前端组件在 `web/src/features/finance-overview/components/`，网络与日期入口在 `subscriptions-api.ts`。复用公共静态表格、分页、弹窗与行操作；现有 DatePicker 禁止未来日期，因此组合已有 Input 的 date 模式，没有新增依赖。

本地验证（2026-11-12）：SQLite 3.50.4、MySQL 8.4.2、PostgreSQL 14.17 的空表创建、已有用户/套餐/订阅表升级、新表连续迁移、索引与记录保留、两类筛选分页及 CRUD 通过；编译后的服务在三个全新隔离库分别启动两次，健康检查与人工记录保留通过。后端 `model/controller/router/middleware/service/authz` 回归通过；前端财务模块 28 项测试、`bun x tsgo -b`、定向 oxlint 和生产构建通过。隔离实例实际登录、创建/修改后的日期回显、两类筛选及 390px 宽度检查通过，页面无横向溢出，浏览器无运行错误。仓库 `bun run typecheck` 当前指向本机不可用的 `bun check`，验证使用已安装的 `tsgo`，未改动依赖。

复验命令：`TEST_MYSQL_DSN='<回环测试库 DSN>' TEST_POSTGRES_DSN='<回环 PostgreSQL URL>' go test ./controller -run '^TestFinance(Overview|Subscriptions)' -count=1 -v`；`go test ./model ./controller ./router ./middleware ./service/authz`；前端 `bun run test src/features/finance-overview/__tests__`、`bun x tsgo -b`、`bun x oxlint -c .oxlintrc.json src/features/finance-overview src/i18n/static-keys.ts`、`bun run build`。本次未提交、推送或生产部署，首次发布须正常执行新表迁移。

### 分类记账与全销售挂帐（第 2 期）

第二期在现有财务总览上关联人工发放、实际线下收款及欠款，不将日志库作为财务账本，也不改变既有订阅甘特图/号池周期。

- **人工调额**：用户管理的调额弹窗需填写分类、说明和稳定请求身份。正向发放/向上覆盖可选补偿、赠送、挂帐、线下收款；线下收款必填人民币分金额及收款凭证，录入即计实收；挂帐独立填写人民币应收分，不由额度面额反推。
- **扣减与下调覆盖**：仅更正额度，区分撤回赠送、撤回补偿、账务更正，可关联原记录。不会自动退现金、取消欠款或恢复授信；零差额覆盖也不能制造收款/欠款。
- **后台开订阅**：两个现有管理员入口共用上述四分类和既有续期规则。保存实际订阅实例及前后权益快照，不伪造钱包余额变化或微信订单。
- **自助挂帐**：Root 在支付设置的用户授信中分别开放钱包充值、订阅、代充值。三类共用人民币授信上限；用户在钱包/订阅付款页选择挂帐，确认本笔应收后获得权益。挂帐不开票、不生成二维码、不调用商户；待付订单有效期为十分钟，支付时再次核对用户状态、模块授权、有效期、额度及套餐状态。
- **人工补单**：保留“原渠道已经收款但回调漏单”的专用操作，必须核实收款凭证。原订单的渠道、金额、冻结额度不变，成功状态与到账事务正常更新；它不转成四分类。已成功的重复补单/迟到回调不再发权益、记实收、生成到账事件或覆盖首次凭证。仅支持已明确本地金额合同的官方微信/支付宝和易支付，国际渠道不猜测现金金额。
- **核销**：财务在挂帐管理对未结清单填写核销原因并确认。只释放授信占用，保留欠款金额、原单、操作者和原因；不记实收、不再次发权益、不回收已消费额度或取消订阅。核销、结清与退款竞争只允许一个资金终态。
- **总览关联**：线下收款归入对应钱包/订阅实收及每日序列，渠道单列；挂帐权益单不计实收，财务结清后才计入。新增人工分类汇总、流水明细和本期坏账核销。额度单位、人民币现金与应收分列，核销不当作现金退款扣减。

主库新增 `quota_ledgers`，保存人工请求唯一身份、摘要、用户/操作者、分类、额度快照、订阅快照及现金/应收。`credit_entries` 增加独立核销说明；`top_ups` 保存人工补单凭证/操作者及自助请求摘要，`subscription_orders` 保存自助请求摘要与冻结套餐快照。启动按 `model/main.go` 自动迁移；旧记录不回填虚构现金或来源，不扫描可清理日志来“还原”已无可靠依据的历史账目。前后端需配套发布，旧客户端缺少新必填字段时明确报错。

调用契约：

| 入口 | 主要参数与限制 |
| --- | --- |
| `POST /api/user/manage`（`action=add_quota`） | 原 `id/mode/value` 加 `reason/note/request_id`；现金用 `cash_cents/cash_ref`，挂帐用 `receivable_cents`，可选 `original_id`。用户与额度权限仍由后端验证。 |
| 两个管理员订阅开通入口 | 原用户/套餐参数加相同分类字段；实际操作人取鉴权身份，不能由请求伪造。 |
| `POST /api/user/credit/pay` | `amount/request_id/quoted_cents`，`invoice_required=false`；报价沿用 `/api/user/amount`，支付方式为 `credit`。 |
| `POST /api/subscription/credit/pay` | `plan_id/request_id/quoted_cents`，`invoice_required=false`。 |
| `POST /api/user/topup/complete` | `trade_no/receipt_ref/note`，不得覆盖原单金额和渠道。 |
| `POST /api/finance/credit/entries/:id/write-off` | `reason` 必填，要求真实登录会话和财务写权限。 |
| `GET /api/finance/adjustments` | `start/end` 与总览相同，支持 `p/page_size/user_id/reason/keyword`；要求独立总览查看权限。 |

`request_id` 为 UUIDv4，同次操作重试必须沿用。相同键不同内容拒绝；建单响应丢失可恢复原单，同单付款、回调及补单各自幂等。事务失败不保留半份权益/债务；PostgreSQL 唯一冲突后在事务外恢复原结果。已有消费预扣仍使用原钱包缓存合同，首次提交只同步差额，不以总余额覆盖预扣。

代码导航：`model/quota_ledger.go`、`user_quota_adjustment.go`、`manual_subscription.go` 为人工来源；`model/credit_checkout.go`、`controller/credit_checkout.go` 为自助挂帐；公共占额与结清/核销在 `model/credit_account.go`。前端按 `users`、`subscriptions`、`wallet`、`credit`、`finance-overview` 职责接入。

三库复验：在隔离回环实例配置 `TEST_MYSQL_DSN/TEST_POSTGRES_DSN`，运行 `go test ./controller ./model -run 'TestManualFinance|TestCredit|TestFinanceOverview|TestManageUserQuota|TestSubscriptionRenewal' -count=1 -v`；另用 `TEST_MANAGE_USER_DIALECT=sqlite|mysql|postgres`、`TEST_MANAGE_USER_SEPARATE_LOG_DB=1` 验证独立日志库。定向并发可加 `-race`。前端类型使用本地 `web/node_modules/.bin/tsgo -b`，相关测试、lint、构建后再构建 Go 服务。

第二期本地验证：SQLite **3.50.4**、MySQL **8.4.2**、PostgreSQL **14.17** 新建/旧表升级/重复迁移、唯一约束、资金闭环及竞态检测通过；相关6个后端包完整回归和 `go vet` 通过。全量测试 DSN 应使用新建空库（既有 `TestDeleteRedemptionBatch` 不接受已含用户/兑换码表的库），不在生产库执行测试。

前端受影响 **19 文件134项**、真实引用类型检查、**35个改动文件零lint错误**及前后端生产构建通过。前端全量 **2671项中2670项通过**，剩余一项仍是既有插件市场源码高亮断言，未扩改该模块。临时实例通过真实HTTP完成分类调额、后台订阅、用户充值/订阅挂帐、结清、核销及重试，并以手算核对实收/待收/核销。浏览器实际操作了没有微信配置、只有授信的账户：钱包付款、订阅续费、分类调额及核销历史显示均通过；390px页面无横向溢出和运行时异常。测试未调用真实商户，未部署。

第二期不包括兑换码售赠分类、上游成本/支出台账、订阅成本摊销、部分还款、批量结清或自动坏账追回；生产发布仍需单独授权。

服务器原生部署及 GitHub 手动构建/发布的配置、接入状态与失败处理，见 [Oak 部署说明](./deploy/oak/README.md)。

贡献前请阅读 [AGENTS.md](./AGENTS.md)。按改动范围执行检查：Go 模块使用 `make test`；前端在 `web/` 下运行 `bun check -b`、`bun run lint`、`bun run test` 和 `bun run build`。修改 RelayKit 后还必须在 `relaykit/` 下执行 `GOWORK=off go build ./...`。

<a id="documentation"></a>

## 文档与社区

| 资源 | 入口 |
| --- | --- |
| 官方文档 | [使用指南](https://docs.newapi.ai/zh/docs) · [安装部署](https://docs.newapi.ai/zh/docs/installation) · [API 参考](https://docs.newapi.ai/zh/docs/api) |
| 项目导读 | [DeepWiki](https://deepwiki.com/QuantumNous/new-api) |
| 使用问题与交流 | [常见问题](https://docs.newapi.ai/zh/docs/support/faq) · [社区渠道](https://docs.newapi.ai/zh/docs/support/community-interaction) |
| 缺陷与功能建议 | [GitHub Issues](https://github.com/QuantumNous/new-api/issues) |
| 安全漏洞 | 按[安全政策](./.github/SECURITY.md)进行私下报告 |

反馈问题时请附上版本、部署方式、复现步骤及脱敏日志。欢迎贡献文档、翻译、渠道适配和有针对性的回归测试。

---

## 🔗 相关项目

### 上游项目

| 项目 | 说明 |
|------|------|
| [One API](https://github.com/songquanpeng/one-api) | 原版项目基础 |
| [Midjourney-Proxy](https://github.com/novicezk/midjourney-proxy) | Midjourney 接口支持 |

### 配套工具

| 项目 | 说明 |
|------|------|
| [new-api-key-tool](https://github.com/Calcium-Ion/new-api-key-tool) | Key 额度查询工具 |
| [new-api-horizon](https://github.com/Calcium-Ion/new-api-horizon) | New API 高性能优化版 |

---

## 📜 许可证

本项目采用 [GNU Affero 通用公共许可证 v3.0 (AGPLv3)](./LICENSE) 授权。

根据 AGPLv3 第 7 条，本项目还适用[附加条款](./NOTICE)。修改版本必须在适当法律声明及界面中显著的关于、法律、页脚或署名位置保留作者署名 `Frontend design and development by New API contributors.`，并保留指向原项目 <https://github.com/QuantumNous/new-api> 的可见链接。

本项目为开源项目，在 [One API](https://github.com/songquanpeng/one-api)（MIT 许可证）的基础上进行二次开发。

如果您所在的组织政策不允许使用 AGPLv3 许可的软件，或您希望规避 AGPLv3 的开源义务，请发送邮件至：[support@quantumnous.com](mailto:support@quantumnous.com)

署名及依赖声明见 [NOTICE](./NOTICE) 和[第三方许可证](./THIRD-PARTY-LICENSES.md)。

---

## 🌟 Star History

<div align="center">

[![Star History Chart](https://api.star-history.com/svg?repos=Calcium-Ion/new-api&type=Date)](https://star-history.com/#Calcium-Ion/new-api&Date)

</div>

---

<div align="center">

### 💖 感谢使用 New API

如果这个项目对你有帮助，欢迎给我们一个 ⭐️ Star！

**[官方文档](https://docs.newapi.ai/zh/docs)** • **[问题反馈](https://github.com/Calcium-Ion/new-api/issues)** • **[最新发布](https://github.com/Calcium-Ion/new-api/releases)**

<sub>Built with ❤️ by QuantumNous</sub>

</div>

### Oak API Key 模型映射

在 API 密钥的创建/编辑抽屉中配置“API Key 模型映射”，支持可视化或 JSON。请求 A 会先路由到 Key 配置的目标 B，权限、资源选择和费用按 B；之后继续应用管理员的渠道映射。使用日志展示原请求、Key 目标及计费模型，历史记录不受后续改配置影响。

保存映射后对后续请求生效，客户端可保持原 model；已在途请求使用开始时冻结的目标。跨模型的参数差异由最终执行层处理，不能仅凭改名保证所有模型能力一致。配套 GPT-Load 本地已补齐 OpenCode Kimi K3 / GLM 5.3 的 `reasoning.context=all_turns` 兼容，发布该修复后才在线上生效；保留客户端工具和完整历史，不代表已解决联网搜索或任意跨供应商私有状态迁移。

映射为空时保持原行为；链式规则可用，循环与重复来源拒绝。支持标准 HTTP、表单上传、Gemini 与 WebSocket 显式模型。特殊动作/历史任务边界、数据升级及验证命令见[实现说明](docs/plans/api-key-model-mapping.md)。

### API Key 模型映射后的联网搜索

本地实现支持为 Kimi K3 / GLM 5.3 配置 GPT 搜索后端，入口为**系统设置 → 模型设置 → 联网搜索适配**。填写当前仅路由到支持搜索上游的 GPT 模型（现场已验证 `gpt-6.1-sol`）；留空关闭。Key 必须同时拥有目标和搜索模型权限及可用资金来源。目标模型决定是否搜索，GPT 提供真实资料及引用，目标模型继续回答。

目标模型 Tokens、GPT Tokens 和真实 `web_search` 调用分别沿用现有价格，工具价格仍在计费设置中调整。日志详情以父请求 ID 关联所有子账单。当前采用阶段缓冲 SSE，等待时发送心跳；同步 HTTP 显式历史可续轮，尚不支持搜索编排的 WebSocket、持久响应或后台任务。部署前还需发布 GPT-Load 已有的 Kimi/GLM `reasoning.context` 参数兼容改动。

配置示例、限制、验证方法和相关模块见[模型映射实施说明：GPT 搜索编排](docs/plans/api-key-model-mapping.md#gpt-搜索编排2026-11-04-102019-0000本地已实现)。定向回归：`go test ./controller ./middleware ./model -run 'TestSearchBridge|TestTokenModelMapping|TestResourceRelayTokenModelMapping' -count=1`。前端配置与日志测试位于 `web/src/features/system-settings/models/__tests__/search-bridge-settings.test.tsx` 和 `web/src/features/usage-logs/components/__tests__/detail-preview.test.tsx`。本轮代码未发布。

## Oak 事件推送（SSE）

超级管理员进入「系统设置 → 运维 → 事件推送」（`/system-settings/operations/events`），可管理服务开关、保留期、异常扣费规则、消费者、授权 Token、在线连接和确认进度。外部客户端使用普通 HTTP SSE 接收事件，再用 HTTP POST 确认，无需 MQ SDK 或额外消息中间件。

**客户端开发与交付文档：[平台事件推送接口文档](docs/event-hub-api.md)。** 独立说明鉴权、请求参数、SSE 帧、全部字段及单位、三类业务载荷、ACK、错误处理、重连幂等和联调验收；接入方无需阅读项目源码。

| 主题 | 产生时机 | 摘要范围 |
| --- | --- | --- |
| `wallet.topup_succeeded` | 钱包充值真正入账，包括人工补单 | 用户编号及账号名、交易号、实付文本、到账额度、支付渠道 |
| `billing.charge_anomaly` | 真实最终费用达到异常阈值 | 用户编号及账号名、请求身份、计费模型、来源、费用、基线和触发原因 |
| `recharge.info_submitted` | 已付款代充值订单提交或更新账户资料 | 订单编号/版本、买家账号名、业务状态、提交类型与处理人编号；不含买家编号或账户资料 |

每个需要独立收到消息的客户端应创建自己的**消费者**，可以按主题及用户 ID 关注。一个消费者可以创建多枚 Token，用于轮换或访问不同主题；这些 Token **共享确认进度**，不提供各自独立的一份消息，也不承诺同消费者多连接之间的独占分配。平台最多 100 个消费者，每个消费者最多 20 枚有效 Token、8 个在线连接。

Token 以 `evt_` 开头，仅可读事件和 ACK，不能调用管理、模型或代充值办理接口。创建需要当前网页登录及一次安全验证，明文只展示一次，数据库仅存哈希。Token 具有独立主题范围、到期时间及撤销状态；消费者创建者被禁用、降权或认证版本失效后，委托也失效，需要重新建立消费者。只允许通过 `Authorization: Bearer` 传递，URL 中的密钥会被拒绝，正式接入使用 HTTPS。

### 接入与确认

使用专用 Token 调用 `GET /api/event-hub/stream`，按 `event_id` 幂等处理并持久保存结果，再将投递 `id` 提交至 `POST /api/event-hub/ack`。仅收到消息或传递 `Last-Event-ID` 都不能代替确认。完整参数、字段表、错误与可复制请求统一维护在[独立接口文档](docs/event-hub-api.md)；网页管理页的状态刷新不代表消费或确认消息。

### 保留期、检测与运行边界

默认暂停网络投递、保留 7 天。进程启动完成迁移后始终捕获业务事件和最终费用，暂停投递也会继续保存与检测；这会增加主数据库的同步写入。消费者默认接收创建后匹配的事件，管理页可按事件编号补发保留期内历史通知，补发不会重新执行充值或扣费。多节点共享主数据库，先升级主节点完成迁移，再启动从节点；缺少事件表的从节点拒绝启动。

扣费基线按「用户 + 实际计费模型 + 钱包/订阅来源」分别统计，取最近 7 天最多 100 笔正向最终费用的中位数，排除当前请求、未完成任务和退款。样本达到门槛时，阈值为 `max(最低异常额度, 中位数 × 倍率)`；不足时使用独立的无历史高额阈值。额度采用平台内部单位，页面显示对应货币金额。异步任务提交价暂不告警，完成后才确认整笔最终费用；同任务补扣和退款更新原记录，不新增历史样本。

超过保留期的未确认消息会清理，并累加消费者的缺口计数。新连接或计数增加时发送 `event: retention_gap`，数据为 `{"expired_unacked_count":数量,"action":"reconcile_business_state"}`，管理页同时提示。收到后应通过原业务接口核对状态，不能依赖事件服务无限重放历史。

现有异步任务在写入终态后、费用结算前退出，可能留下待核对记录；数据库已提交而 Redis 增量确认未知时也会保留标记。管理页分别显示未决任务数和缓存待确认数；观察表 `hub_charge_observations` 的 `request_key`、`task_id`、`state`、`cache_sync_pending` 可用于定位。此类记录不会被保留期清理，不自动猜测终价、退款或重复应用缓存增量。资金与模型 Key 预算沿用既有两阶段更新，Key 更新失败仍需结合错误日志核对，不计入上述两个页面计数。同步请求或 Realtime 连接尚未到最终结算就进程退出时，也无法生成最终费用观察；本次没有新增通用账务恢复系统。

核心目录：`model/event_hub*.go` 保存主库事件、订阅、投递账本和费用观察；`controller/event_hub.go` 提供管理、SSE 和 ACK；`service/event_hub.go` 执行后台检测与恢复；`web/src/features/event-hub/` 为管理页面。Gin 路由避开 gzip，`deploy/oak/Caddyfile` 排除事件流压缩并实时刷新。

验证入口：`go test ./router ./middleware ./controller ./model ./service -count=1`；核心 `go test -race ./controller -run '^TestEventHub' -count=1`；三库行为 `go test ./controller ./service -run 'TestEventHub|TestHubBilling' -count=1 -v`，外部库通过隔离实例的 `TEST_MYSQL_DSN`、`TEST_POSTGRES_DSN` 提供。已验证 SQLite 3.50.4、MySQL 8.4.2、PostgreSQL 14.17，包括重复迁移、保留既有业务数据、独立日志库、事务回滚、权限收缩、在线撤销及真实 HTTP SSE。代理验证使用官方 Caddy 2.11.7：`CADDY_TEST_BINARY=/绝对路径/caddy python3 deploy/oak/tests/test_caddy_body_log.py`。

前端验证：在 `web/` 执行 `bun x tsgo -b`、`bun run test src/features/event-hub/__tests__/event-hub.test.tsx` 及相关文件 oxlint，9 项交互测试通过；先 `bun run build` 再在仓库根执行 `go build`，避免清理前端产物时破坏 Go embed。隔离编译实例已实际完成消费者创建/编辑、Token 二次验证签发/撤销、SSE 首帧/合成事件接收及持久 ACK；页面刷新不重复详情。本轮未部署，也未进行真实支付或上游模型调用。

鉴权复核参考 ASVS 5.0.0 的 7.2.1（后端验证）与 7.4.1（会话终止后失效），以及 [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html) 与 [Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)，落实高熵凭据、限权与到期、敏感操作再验证、服务器端失效检查及凭据不入审计。定向测试覆盖证明绑定/重用拒绝、消费者与主题越权、凭据撤销和会话失效；这些验证不等同于全站 ASVS 认证。

### 挂帐支付与财务结清

支付网关增加「挂帐」页签，由现有支付设置权限（超级管理员）添加用户、设置人民币授信额度，并分别控制总开关和代充值模块授权。未配置用户不能选择挂帐；关闭授权保留历史欠款。额度按未结清欠款占用，所有未来开放模块共用总额度。本期仅实际接入代充值，充值与订阅保留字段和未开放说明，服务端拒绝提前启用。

用户在代充值结账选择「挂帐」，确认订单后点击「确认挂帐」才实际占额，之后提交账号资料并沿用人工办理流程。报价、下单与付款入口均受原接单与支付配置门禁保护；付款再次验证当前授权及额度。挂帐不增减钱包余额，不向商户发起收款，本期不支持该支付方式申请发票。

财务分类的「挂帐管理」提供用户授信摘要及逐笔挂帐记录，可按用户、订单号和状态筛选，并查看筛选范围金额。具有 `finance:write` 能力的用户核实线下收到整笔款项后，填写收款凭证登记结清；恢复额度与结清记录同事务提交。没有部分结清、自动扣款或无凭证清零功能。授信配置与结清均保留操作者，个人访问令牌不能调用新增挂帐接口。

未结清订单退款时撤销债务并恢复额度；已结清订单退款必须登记实际退款凭证，不再次恢复额度。退款仍沿用代充值现有管理员审批、确认与单次验证流程。已经完成履约的订单仍遵守原不可直接退款规则。

实现导航：`model/credit_account.go` 管理授信和结清，`model/credit_recharge.go` 处理挂帐付款与退款事务，`model/credit_query.go` 提供分页对账；`controller/credit_account.go` 提供受保护接口，`web/src/features/credit/` 提供配置和财务界面。迁移新增 `credit_accounts`、`credit_entries`，不回填历史订单为挂帐。金额以人民币整数分保存，界面显示元；订单内部 `paid` 仅用于沿用履约资格，挂帐时 `PaidAt` 为零，实际收款以挂帐流水的 `settled_at` 为准。

验证：`go test ./controller ./model ./middleware ./router ./service/authz ./setting/operation_setting`；三库定向执行 `go test ./controller -run 'TestCredit|TestRecharge' -count=1 -v`，设置回环专用 `TEST_MYSQL_DSN` / `TEST_POSTGRES_DSN`，测试创建独立数据库。前端在 `web/` 使用 `bun x tsgo -b`、受影响文件的 `bun x oxlint -c .oxlintrc.json ...`、`bun run test -- src/features/credit src/features/account-recharge src/hooks/__tests__/sidebar-config.test.tsx src/features/system-settings/integrations/__tests__ src/features/invoices/__tests__` 和 `bun run build`，最后在根目录执行 `go build`。

本地验证使用 SQLite 3.50.4、MySQL 8.4.2、PostgreSQL 14.17，覆盖空库、含原代充值结构和现金订单的升级、重复迁移、并发限额、失败回滚、幂等结清和两类退款。没有生产部署或真实收退款。现有通用审计把财务角色 5 记为未知角色（仅 Root 可见），属于既有问题；本功能的挂帐流水独立保存真实结清人、凭证与时间，未改动全局审计规则。

安全检查参考 OWASP 的 [Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html)、[Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html) 与 [Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)：采用默认拒绝、逐请求能力检查、订单所有者约束及原有会话校验，不新增登录或会话机制。测试验证普通用户拒绝财务入口、财务可结清但不可授信、PAT 即使有财务范围仍被拒绝；不宣称完成整套 ASVS 合规认证。
