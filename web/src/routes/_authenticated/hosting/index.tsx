/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { createFileRoute } from '@tanstack/react-router'

import { HostingWorkspace } from '@/features/hosting'

// 复用认证父路由，未登录用户完成登录后回到挂靠工作区。
// 页面可见性不替代服务端按当前用户逐次执行的账号所属校验。
// 路由只负责声明入口，业务组件位于 feature 目录。
// 文件名遵循自动路由生成约定，不手工修改生成树。
// 登录恢复路径由父路由统一处理，避免重复鉴权逻辑。
// 挂靠账号和 API Key 的所有权仍由各接口验证。
// 不通过路由参数接收原厂 code 或账号秘密。
// OAuth 回调由弹窗内存表单提交，不保存到导航历史。
// 页面加载不执行模型测试或重置机会消费。
export const Route = createFileRoute('/_authenticated/hosting/')({
  component: HostingWorkspace,
})
