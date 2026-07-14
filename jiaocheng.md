# CC Switch 配置使用API教程

CC Switch 是AI CLI 一体化管理平台，他可以统一管理 Claude Code、Codex 与 Gemini CLI 的供应商配置、模型切换、MCP 服务器、Skills 扩展和系统提示词。

使用 CC-Switch，您可以：

- 一键切换 API 配置 - 在多个 API 提供商之间快速切换
- 可视化配置管理 - 通过图形界面轻松管理所有配置
- MCP 服务器管理 - 管理 Model Context Protocol 服务器
- 系统托盘快捷操作 - 通过托盘菜单快速切换

项目地址：https://github.com/farion1231/cc-switch

## CC Switch 下载安装

前往项目官方 [GitHub Release](https://github.com/farion1231/cc-switch/releases) 页面，下载对应系统的最新版本安装程序，保证文件完整性与安全性。


![image.png](https://api.apifox.com/api/v1/projects/5076588/resources/666146/image-preview)

进去下载您电脑系统对应的最新版本安装程序即可。

下载完后点击安装，一路点击next就可以了

![image.png](https://api.apifox.com/api/v1/projects/5076588/resources/666147/image-preview)

## CC Switch 接入第三方API

这里我们选择用 Claude Code 作为演示，不知道如何安装claude code可以去我们编程教程文档查找，会一步一步教大家如何安装和部署。

### 一、进入设置页面

首次打开软件后，点击左上角的“齿轮”图标，进入 CC Switch 的全局设置页面。

![image.png](https://api.apifox.com/api/v1/projects/5076588/resources/666148/image-preview)

### 二、开启插件接管

在设置页面的“通用”选项卡中，找到并开启 “应用到 XXX 插件”（例如应用到 Claude Code 插件）开关，这样当你在 CC Switch 切换供应商时，对应的终端工具配置才会生效。你可以顺便开启“开机自启”。

![image.png](https://api.apifox.com/api/v1/projects/5076588/resources/666152/image-preview)

### 三、添加供应商

打开ccswitch程序后，选择Claude Code图标，并点击右上角的“+添加供应商”。

![image.png](https://api.apifox.com/api/v1/projects/5076588/resources/666148/image-preview)

进入后选择“自定义供应商” 页面滚动到底部添加供应商信息：

![image.png](https://api.apifox.com/api/v1/projects/5076588/resources/666154/image-preview)

但我们推荐选择“统一供应商”，一次配置适配更多CLI。

![image.png](https://api.apifox.com/api/v1/projects/5076588/resources/666155/image-preview)

选择预设类型（如 NewAPI 或 自定义网关）。
依次填入 名称、API 地址 (https://oik.qingshukeji.top:9030) 和 API Key。

|表单项|值|
|---|---|
|供应商名称|Oik|
|API地址|https://oik.qingshukeji.top:9030|
|API Key|sk-xxx 从我们网站API令牌页面复制过来|
|官网地址|https://oik.qingshukeji.top:9030|


![组合 1.webp](https://api.apifox.com/api/v1/projects/5076588/resources/666158/image-preview)

### 四、添加模型

配置都已完成，剩下的就是在刚添加的供应商卡片上 测试连通性和添加需要的模型即可。
