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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { PaymentOrders } from '../index'

// 只隔离HTTP边界，真实表格、危险确认和权限判断照常执行。
// 订单fixture无商户凭据与付款人身份，金额由服务端给出。
// 测试真实页面组件与共享确认弹窗。
// 网络层是唯一被替换的业务边界，
// 表格渲染、权限判断及请求正文均使用生产实现。
// 订单金额和用户编号是固定fixture，
// 不从被测函数计算预期结果。
// 所有测试使用独立QueryClient，
// 失败用例不能继承上一项的成功缓存。
// 清理先卸载组件再清空查询，
// 避免异步请求污染后续身份。
// 账号数据只用于内存中的测试store，
// 结束时恢复未登录状态。
const order = {
  id: 1,
  module: 'topup',
  user_id: 2,
  trade_no: 'wx-test-order',
  money: 12.3,
  payment_provider: 'official_wechat',
  payment_method: 'wx',
  status: 'success',
  created_at: 1700000000,
  paid_at: 1700000001,
  refund: null,
  refund_block: '',
}
const refunded = {
  refund_no: 'RFtest',
  amount_cents: 1230,
  status: 'processing',
  reason: '取消充值',
  actor_id: 100,
  provider_ref: 'wx-refund',
}
// 确认测试只检查传到HTTP边界的安全字段。
// 即使测试fixture含有金额，
// 请求正文也不应出现可由浏览器操控的退款额。
// 用于身份模拟的是本地auth store，
// 不替换hasPermission生产实现。
// Root测试验证默认管理能力，
// 只读管理员测试验证显式退款拒绝。
// 所有操作由可访问角色或label定位，
// 不会直接修改组件内部状态。
// 弹窗采用真实AlertDialog，
// 能观察最终确认按钮的禁用状态。
// 原因输入通过真实键盘事件完成，
// 而不是直接给mutation传参数。
// 恢复测试同时验证原因输入不存在，
// 避免UI暗示管理员可以改写原请求。
// 成功后等待API真实调用完成，
// 不用固定睡眠猜测请求已经结束。
// 错误用例关闭查询自动重试，
// 用于立即观察最终错误状态。
// 网络mock在每项结束恢复，
// 没有后续测试会继续使用虚构订单。
// 这些测试不证明微信已经实际退款，
// 签名与事务行为由后端独立验证。
// 页面交互测试只保护前端接线合同。
const clients: QueryClient[] = []
// 每项测试使用独立查询缓存，避免前一项成功响应掩盖读取失败。
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>
      <PaymentOrders />
    </QueryClientProvider>
  )
}
afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset('complete')
})

// 首次退款必须通过用户点击打开确认框。
// 断言显示完整交易号，
// 防止按钮行与确认对象错位。
// 原因为空时确认按钮不可用，
// 不通过直接调用回调跳过真实输入。
// 填写原因后检查真实API正文，
// 金额、商户和用户归属都不应由浏览器提交。
// 请求标记禁止授权自动重放，
// 保护资金动作不会随登录刷新盲目重复。
test('管理员确认整单退款时必须填写原因，提交不携带金额或商户配置', async () => {
  useAuthStore.getState().auth.setUser({ id: 100, username: 'root', role: 100 })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { items: [order], total: 1 } },
  })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: refunded } })
  const user = userEvent.setup()
  mount()
  await user.click(await screen.findByRole('button', { name: 'Full refund' }))
  const dialog = screen.getByRole('alertdialog')
  expect(within(dialog).getByText(/wx-test-order/)).toBeVisible()
  expect(within(dialog).getByRole('button', { name: 'Confirm' })).toBeDisabled()
  await user.type(
    within(dialog).getByLabelText('Refund reason (up to 80 bytes)'),
    '取消充值'
  )
  await user.click(within(dialog).getByRole('button', { name: 'Confirm' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/orders/refund',
      {
        module: 'topup',
        trade_no: 'wx-test-order',
        reason: '取消充值',
        resume: false,
      },
      { singleUseAuthorization: true }
    )
  )
})

// 恢复场景从服务端已有退款记录开始。
// 页面必须显示恢复按钮，
// 不能把同一订单再次视为首次退款。
// 确认框不再提供原因编辑，
// 以保证重试不会改变微信幂等参数。
// 实际POST必须携带恢复标记，
// 不能在测试里模拟成功后自行改状态。
// 成功响应仍通过生产边界解析，
// 未知退款状态不会被组件忽略。
test('已受理退款只提供恢复操作并沿用服务端退款身份', async () => {
  useAuthStore.getState().auth.setUser({ id: 100, username: 'root', role: 100 })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { items: [{ ...order, refund: refunded }], total: 1 },
    },
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { ...refunded, status: 'succeeded' } },
  })
  const user = userEvent.setup()
  mount()
  await user.click(
    await screen.findByRole('button', { name: 'Recover refund' })
  )
  const dialog = screen.getByRole('alertdialog')
  expect(within(dialog).queryByRole('textbox')).not.toBeInTheDocument()
  await user.click(within(dialog).getByRole('button', { name: 'Confirm' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/orders/refund',
      expect.objectContaining({ resume: true, reason: '' }),
      expect.anything()
    )
  )
})

// 只读管理员具有真实订单数据可看。
// 如果没有数据也没有按钮，
// 无法证明退款能力真的受限。
// 测试因此先等待订单号出现，
// 再检查整单退款入口不存在。
// 管理员角色本身不能覆盖显式禁止，
// 细粒度权限仍应控制页面动作。
test('只有读取权限时展示订单但不展示退款按钮', async () => {
  useAuthStore.getState().auth.setUser({
    id: 10,
    username: 'reader',
    role: 10,
    permissions: {
      admin_permissions: { orders: { read: true, refund: false } },
    },
  })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { items: [order], total: 1 } },
  })
  mount()
  expect(await screen.findByText(order.trade_no)).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'Full refund' })
  ).not.toBeInTheDocument()
})

// 业务失败使用HTTP成功信封返回，
// 用于防止页面只判断HTTP状态码。
// 失败之后应展示可重试状态，
// 不能把失败数据归一化为空数组。
// 没有订单文案不应同时出现，
// 否则管理员可能错误认定没有待处理交易。
test('服务端拒绝读取时显示重试而不是无订单', async () => {
  useAuthStore.getState().auth.setUser({ id: 100, username: 'root', role: 100 })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: false, message: 'database unavailable' },
  })
  mount()
  expect(await screen.findByRole('button', { name: /Retry/i })).toBeVisible()
  expect(screen.queryByText('No orders')).not.toBeInTheDocument()
})
