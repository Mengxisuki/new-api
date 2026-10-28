# 产品现状

更新时间：2026-10-28 09:31:00 +00:00。

New API 提供多模型 API 网关及控制台；本地 Oak 官网面向实验室与科研团队，介绍模型接入、项目用量管理、科研场景与采购开票支持。官网不会执行示例中的模型请求，也不代表包含模型厂商的官方客户端订阅。

## 官网来源与边界

本次按用户要求，将 `dev/v1.0.0`（`3d009416e`）的 Oak 官网及采购联系人配套功能恢复到当前分支。首页最近一次布局与词条修改为 `467e94092`。仅提取首页及采购功能，未合并整个旧分支；公共布局、上游认证逻辑、全局主题及其他后台功能沿用当前实现。

入口为 `web/src/features/home/index.tsx`，默认渲染 `components/oak-landing.tsx`；已配置的 Markdown、HTML、iframe 自定义首页保持优先。官网 CSS 局限于 `web/src/styles/oak-home.css`，七语言词条仅追加缺失键。

## 采购联系配置

配置入口、操作语义、上传限制与验证命令见 [中文说明](../../README.zh_CN.md#oak-官网与采购联系配置)。姓名、微信号、二维码与微信登录完全独立。上传与删除仅允许超级管理员，首页读取公开状态与图片接口；图片复用现有系统选项存储，无新增依赖或数据库结构。

验证依据：`controller/procurement_contact_test.go` 与前端首页、采购上传和校验测试。后端定向测试使用 SQLite；没有执行 MySQL/PostgreSQL 兼容矩阵，也未进行生产部署验证。

## 发布现状

`ssh wdm` 对应现有 Oak 站点，使用原生 Go 二进制、systemd、Caddy、PostgreSQL 和 Redis。`oak` 承载官网定制和手动 CI/CD，并作为 GitHub 默认分支；`upstream-main` 纯跟踪官方 `upstream/main`，定制不进入该分支，原 `main` 保留。官方最新六个提交已跟踪，尚未合入当前定制基线。

用户已授权本次提交推送和专用部署密钥接入，GitHub 私钥由用户自行录入环境 Secret。服务器处理器、受限公钥和生产环境已配置；默认仅构建，显式勾选才发布，没有推送即自动上线的触发器。

两个分支已推送、GitHub 默认分支已设为 oak，[首次真实 GitHub 构建](https://github.com/Mengxisuki/new-api/actions/runs/40663034653)成功，测试、构建和发布包上传通过，发布任务跳过。剩余私钥录入和首次发布见 [交接记录](../handoff.md)，操作说明见 [部署说明](../../deploy/oak/README.md)。生产版本尚未切换，CI 配置完成不代表已发布官网。
