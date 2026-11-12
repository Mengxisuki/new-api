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

import { FinanceOverviewPage } from '@/features/finance-overview'
import { hasPermission } from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

// 财务总览使用独立的 finance_overview 权限，与开票、挂帐的办理权限互不包含：
// 能办理单笔业务的账号不一定能看全平台收入，反之亦然。
// 这里的检查只决定是否进入页面，服务端接口每次请求都会再次校验同一权限。
export const Route = createFileRoute('/_authenticated/finance/overview/')({
  beforeLoad: () => {
    if (
      !hasPermission(
        useAuthStore.getState().auth.user,
        'finance_overview',
        'read'
      )
    ) {
      throw redirect({ to: '/403' })
    }
  },
  component: FinanceOverviewPage,
})
