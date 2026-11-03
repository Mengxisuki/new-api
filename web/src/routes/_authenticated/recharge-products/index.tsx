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

import { RechargeProducts } from '@/features/account-recharge/products'
import { hasPermission } from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'
// 认证父路由先建立真实会话，
// 本路由不依赖侧栏隐藏提供授权。
// 页面只使用 New API控制台会话，
// 不暴露执行层管理地址。
// 访问权限由认证父路由和实际 API双重约束。
// 路由存在不代表已配置商品、支付渠道或资料加密。
// 页面组件负责加载安全摘要，
// 敏感资料只在明确操作后获取。
// 首次进入不会创建订单、领取任务或执行资金变更。
// 商品写权限在页面中另行检查。
// 只有读取能力的管理员仍可查看配置。
// 下架只影响新购买，不删除历史订单。
export const Route = createFileRoute('/_authenticated/recharge-products/')({
  // 路由能力与后端资源一致，
  // 业务员不能进入商品管理。
  beforeLoad: () => {
    if (
      !hasPermission(
        useAuthStore.getState().auth.user,
        'recharge_products',
        'read'
      )
    ) {
      throw redirect({ to: '/403' })
    }
  },
  component: RechargeProducts,
})
