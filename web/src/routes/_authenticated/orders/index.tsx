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

import { PaymentOrders } from '@/features/payment-orders'
import { hasPermission } from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

// 认证父路由先恢复当前登录会话。
// 此路由进一步要求管理员角色，
// 并检查订单读取能力。
// 直接访问地址也必须经过这两道门禁，
// 不能因为侧栏隐藏就省略权限判断。
// 页面查询与退款接口仍会独立校验，
// 路由放行不是服务端授权依据。
// 路由不解析退款金额或交易意图，
// 首次加载绝不发起外部资金动作。
// 认证信息丢失时进入拒绝页面，
// 不沿用前一个用户留下的管理权限。

// 角色门禁与后端一致，资源能力还需单独检查。
// 财务和业务员不会继承全平台交易管理权限。
// 路由加载仅查询，退款必须在页面明确确认。
// 实际退款能力在页面内独立判断，读取权限不会顺带开放按钮。
export const Route = createFileRoute('/_authenticated/orders/')({
  beforeLoad: () => {
    const user = useAuthStore.getState().auth.user
    if (
      !user ||
      user.role < ROLE.ADMIN ||
      !hasPermission(user, 'orders', 'read')
    ) {
      throw redirect({ to: '/403' })
    }
  },
  component: PaymentOrders,
})
