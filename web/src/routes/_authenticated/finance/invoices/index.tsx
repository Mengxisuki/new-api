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

import { FinanceInvoices } from '@/features/invoices/admin-index'
import { hasPermission } from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

// 财务账号无需进入普通管理区，路由直接按资源读取能力授权。
// 隐藏侧栏不能作为权限校验；服务端接口仍逐次执行相同权限判断。
export const Route = createFileRoute('/_authenticated/finance/invoices/')({
  beforeLoad: () => {
    // 认证父路由先解析会话，这里只判断当前资源能力。
    if (!hasPermission(useAuthStore.getState().auth.user, 'finance', 'read')) {
      throw redirect({ to: '/403' }) // 财务入口拒绝时不能退回普通管理员工作区。
    }
  },
  component: FinanceInvoices, // 路由接入真实管理页，不能只有菜单没有业务组件。
})
