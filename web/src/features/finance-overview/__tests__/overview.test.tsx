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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { FinanceOverviewPage } from '../index'

// 只替换网络边界、图表画布和路由链接；页面、权限读取、表格和筛选都是真实实现。
// 图表库依赖浏览器画布，jsdom 无法绘制，因此记录它收到的数据来验证接线。
const chart = vi.hoisted(() =>
  vi.fn(
    (_props: { spec: { data: { values: Record<string, unknown>[] }[] } }) =>
      null
  )
)
vi.mock('@visactor/react-vchart', () => ({ VChart: chart }))
vi.mock('@visactor/vchart', () => ({
  ThemeManager: { setCurrentTheme: vi.fn() },
}))
// 页面单独渲染时没有路由上下文，链接替换为普通锚点以便断言跳转目标；
// 按钮合并进来的无障碍名称等属性原样透传，与真实链接一致。
vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  Link: ({ to, ...rest }: { to: string } & React.ComponentProps<'a'>) => (
    <a href={to} {...rest} />
  ),
}))

const DAY = 1793462400 // 上海 2026-11-01 00:00
// 金额均为虚构数据：两天内四条业务线的实收，合计 300.00 元。
const overview = {
  start: DAY,
  end: DAY + 2 * 86400,
  topup_cents: 15050,
  subscription_cents: 3000,
  recharge_cents: 6000,
  credit_settled_cents: 5950,
  received_cents: 30000,
  refund_cents: 4000,
  net_cents: 26000,
  invoice_fee_cents: 723,
  daily: [
    {
      day: DAY,
      topup_cents: 15050,
      subscription_cents: 0,
      recharge_cents: 6000,
      credit_settled_cents: 0,
    },
    {
      day: DAY + 86400,
      topup_cents: 0,
      subscription_cents: 3000,
      recharge_cents: 0,
      credit_settled_cents: 5950,
    },
  ],
  channels: [
    { channel: 'official_wechat', cents: 24050, count: 3 },
    { channel: 'credit_settlement', cents: 5950, count: 1 },
  ],
  other_gateways: [{ provider: 'stripe', money: 20, count: 1 }],
  credit: {
    outstanding_cents: 11000,
    outstanding_count: 3,
    limit_cents: 50000,
    used_cents: 11000,
    new_cents: 7000,
    aging30_cents: 8000,
    aging60_cents: 2000,
    aging_over_cents: 1000,
    overdue_count: 2,
    top_debtors: [
      {
        user_id: 8,
        username: 'debtor',
        display_name: 'Debtor A',
        cents: 8000,
        count: 1,
      },
      { user_id: 9, username: '', display_name: '', cents: 3000, count: 2 },
    ],
  },
  wallet_balance_quota: 3500,
  consumed_quota: 1000,
  consumption_available: true,
  todo: { pending_invoices: 2, pending_refunds: 1 },
}

let client: QueryClient
beforeEach(() => {
  chart.mockClear()
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset('complete')
})

// 以指定权限登录并挂载页面；返回被替换的 GET，用于断言请求参数。
function mount(
  permissions: Record<string, Record<string, boolean>>,
  data: unknown = overview
) {
  useAuthStore.getState().auth.setUser({
    id: 5,
    username: 'finance',
    role: 5,
    permissions: { admin_permissions: permissions },
  })
  const get = vi
    .spyOn(api, 'get')
    .mockResolvedValue({ data: { success: true, data } })
  render(
    <QueryClientProvider client={client}>
      <FinanceOverviewPage />
    </QueryClientProvider>
  )
  return get
}

// 取某个指标卡的容器：标题所在卡片内同时包含数值和口径说明。
function metric(label: string) {
  return within(screen.getByText(label).parentElement as HTMLElement)
}

test('有总览权限时按人民币展示各项指标并区分时段数字与当前快照', async () => {
  mount({ finance_overview: { read: true } })
  expect(await screen.findByText('Net revenue')).toBeInTheDocument()

  expect(metric('Net revenue').getByText('CN¥260.00')).toBeInTheDocument()
  expect(metric('Cash received').getByText('CN¥300.00')).toBeInTheDocument()
  // 开票附加费已包含在实收里，只作为说明出现。
  expect(
    metric('Cash received').getByText('Includes invoice surcharges of CN¥7.23.')
  ).toBeInTheDocument()
  expect(metric('Refunds').getByText('CN¥40.00')).toBeInTheDocument()
  // 挂帐待收单列，并明确说明结清前不算收入。
  expect(
    metric('Outstanding on-account').getByText('CN¥110.00')
  ).toBeInTheDocument()
  expect(
    metric('Outstanding on-account').getByText(/Not counted as revenue/)
  ).toBeInTheDocument()

  // 其他网关只列原始金额，并说明没有计入合计。
  expect(screen.getByText(/stripe: 20/)).toBeInTheDocument()
  expect(
    screen.getByText(/not verified cash receipts/)
  ).toBeInTheDocument()
  // 已知渠道显示名称，结清的挂帐作为独立收款来源出现。
  expect(screen.getByText('WeChat Pay')).toBeInTheDocument()
  // 用户被删除后名称为空，仍以编号标识欠款人。
  expect(screen.getByText('#8 Debtor A')).toBeInTheDocument()
  expect(screen.getByText('#9')).toBeInTheDocument()
})

test('没有总览权限时不请求数据并显示无访问权限', () => {
  // 即使具有财务办理权限，也不能查看经营汇总。
  const get = mount({ finance: { read: true, write: true } })
  expect(screen.getByText('Access denied')).toBeInTheDocument()
  expect(get).not.toHaveBeenCalled()
})

test('默认查询本月至今，切换到今天后按一个自然日重新请求', async () => {
  const get = mount({ finance_overview: { read: true } })
  await screen.findByText('Net revenue')
  const first = get.mock.calls[0][1]?.params as { start: number; end: number }
  expect(get.mock.calls[0][0]).toBe('/api/finance/overview')
  // 本月至今：起点是上海某月一日零点，终点是上海某日零点，跨度为整数天。
  expect((first.start + 8 * 3600) % 86400).toBe(0)
  expect((first.end - first.start) % 86400).toBe(0)
  expect(new Date((first.start + 8 * 3600) * 1000).getUTCDate()).toBe(1)

  await userEvent.click(screen.getByRole('tab', { name: 'Today' }))
  await waitFor(() => expect(get).toHaveBeenCalledTimes(2))
  const today = get.mock.calls[1][1]?.params as { start: number; end: number }
  expect(today.end - today.start).toBe(86400)
  expect(today.end).toBe(first.end) // 两个预设都以今天结束为右边界。
})

test('图表收到每天每条业务线的金额，表格视图给出相同数据的精确读法', async () => {
  mount({ finance_overview: { read: true } })
  await screen.findByText('Revenue trend')
  await waitFor(() => expect(chart).toHaveBeenCalled())
  const values = chart.mock.calls.at(-1)?.[0].spec.data[0].values
  // 两天乘四条业务线，金额以元传给图表。
  expect(values).toHaveLength(8)
  expect(values).toContainEqual({
    day: '2026-11-01',
    line: 'Wallet top-up',
    amount: 150.5,
  })
  expect(values).toContainEqual({
    day: '2026-11-02',
    line: 'Credit settlement',
    amount: 59.5,
  })

  await userEvent.click(screen.getByRole('tab', { name: 'Table view' }))
  const row = screen.getByRole('row', { name: /2026-11-01/ })
  expect(within(row).getByText('CN¥150.50')).toBeInTheDocument()
  expect(within(row).getByText('CN¥60.00')).toBeInTheDocument()
})

test('待办入口按目标页面权限显示：只有总览权限时只显示数量', async () => {
  mount({ finance_overview: { read: true } })
  await screen.findByText('Needs attention')
  expect(screen.getByText('Invoices awaiting processing')).toBeInTheDocument()
  // 项目按钮组件渲染为链接时在无障碍树中仍是按钮角色，因此按按钮查询。
  expect(
    screen.queryByRole('button', { name: /^View: / })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'On-account management' })
  ).not.toBeInTheDocument()
})

test('同时具有财务办理权限时待办和挂帐面板提供跳转', async () => {
  mount({
    finance_overview: { read: true },
    finance: { read: true },
    recharge_orders: { read: true },
  })
  await screen.findByText('Needs attention')
  expect(
    screen.getByRole('button', { name: 'View: Invoices awaiting processing' })
  ).toHaveAttribute('href', '/finance/invoices')
  expect(
    screen.getByRole('button', { name: 'View: On-account entries overdue' })
  ).toHaveAttribute('href', '/finance/credit')
  expect(
    screen.getByRole('button', {
      name: 'View: Recharge refunds awaiting action',
    })
  ).toHaveAttribute('href', '/recharge-orders')
  expect(
    screen.getByRole('button', { name: 'On-account management' })
  ).toHaveAttribute('href', '/finance/credit')
})

test('用量统计关闭时消耗显示为不可用而不是零', async () => {
  mount(
    { finance_overview: { read: true } },
    { ...overview, consumption_available: false, consumed_quota: 0 }
  )
  await screen.findByText('Quota consumed')
  expect(
    metric('Quota consumed').getByText('Not available')
  ).toBeInTheDocument()
  expect(
    metric('Quota consumed').getByText(/Usage statistics are turned off/)
  ).toBeInTheDocument()
})

test('所选时段没有收入时趋势面板显示空状态而不是空图表', async () => {
  mount(
    { finance_overview: { read: true } },
    {
      ...overview,
      received_cents: 0,
      topup_cents: 0,
      subscription_cents: 0,
      recharge_cents: 0,
      credit_settled_cents: 0,
      channels: [],
      daily: overview.daily.map((day) => ({
        day: day.day,
        topup_cents: 0,
        subscription_cents: 0,
        recharge_cents: 0,
        credit_settled_cents: 0,
      })),
    }
  )
  await screen.findByText('Revenue trend')
  expect(
    screen.getAllByText('No revenue in the selected period').length
  ).toBeGreaterThan(0)
  expect(chart).not.toHaveBeenCalled()
})

test('接口失败时显示可重试的错误状态，不显示零收入', async () => {
  useAuthStore.getState().auth.setUser({
    id: 5,
    username: 'finance',
    role: 5,
    permissions: { admin_permissions: { finance_overview: { read: true } } },
  })
  const get = vi
    .spyOn(api, 'get')
    .mockResolvedValueOnce({
      data: {
        success: false,
        message: 'Unauthorized, insufficient privileges',
      },
    })
    .mockResolvedValue({ data: { success: true, data: overview } })
  render(
    <QueryClientProvider client={client}>
      <FinanceOverviewPage />
    </QueryClientProvider>
  )
  const retry = await screen.findByRole('button', { name: 'Retry' })
  expect(screen.queryByText('Net revenue')).not.toBeInTheDocument()
  await userEvent.click(retry)
  expect(await screen.findByText('Net revenue')).toBeInTheDocument()
  expect(get).toHaveBeenCalledTimes(2)
})

test('自定义时段在选齐日期前不发请求并提示需要选择起止日期', async () => {
  const get = mount({ finance_overview: { read: true } })
  await screen.findByText('Net revenue')
  await userEvent.click(screen.getByRole('tab', { name: 'Custom' }))
  expect(
    await screen.findByText('Select a start date and an end date.')
  ).toBeInTheDocument()
  expect(get).toHaveBeenCalledTimes(1) // 只有默认时段那一次请求。
  expect(screen.getByRole('button', { name: 'Refresh' })).toBeDisabled()
})
