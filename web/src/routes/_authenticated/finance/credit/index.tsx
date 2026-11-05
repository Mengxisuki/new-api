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
import { createFileRoute, redirect } from '@tanstack/react-router'

import { CreditManagement } from '@/features/credit'
import { hasPermission } from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

// 财务账号无需进入普通管理区，路由直接按资源读取能力授权。
// 隐藏侧栏不能作为权限校验；服务端接口仍逐次执行相同权限判断。
// 财务路径复用认证父布局，不另建登录机制。
// 资源检查必须在业务页面加载前执行。
// 只有读能力的财务用户也可以进入对账页面。
// 页面内部独立检查写能力，才允许登记整笔结清。
// 服务端会再次判断会话和权限，路由跳转不是授权依据。
// 未知或被撤销权限直接转入现有拒绝访问页。
export const Route = createFileRoute('/_authenticated/finance/credit/')({
  beforeLoad: () => {
    // 认证父路由先解析会话，这里只判断当前资源能力。
    if (!hasPermission(useAuthStore.getState().auth.user, 'finance', 'read')) {
      throw redirect({ to: '/403' }) // 财务入口拒绝时不能退回普通管理员工作区。
    }
  },
  component: CreditManagement, // 路由接入真实管理页，不能只有菜单没有业务组件。
})
