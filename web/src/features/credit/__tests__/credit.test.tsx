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

import { CreditAccounts } from '../accounts'
import { CreditManagement } from '../index'

// 只替换网络边界，真实表格、弹窗、表单和资源能力照常执行。
// 所有用户名和收款凭证均为不可用的测试值。
// 授信与流水的测试数据故意分开，防止混淆两种接口合同。
// 配置表中的占用是只读字段，保存断言不包含它。
// 新建账户的版本从零开始，修改账户保留已有版本。
// 用户选择必须经过真实下拉控件，不能只给组件传入编号。
// 输入小数金额覆盖元与分之间的转换边界。
// 模块开关覆盖编辑后关闭授权的路径。
// 预留充值和订阅不应呈现误导性的可用入口。
// 财务用例通过真实身份 store 提供资源能力。
// 页面不能因为角色值为财务就跳过细粒度权限。
// 只读场景刻意不给写能力，但仍提供同一条债务数据。
// 因此隐藏结清按钮必须来自权限判断，而非没有记录。
// 结清交互使用真实确认框，检查交易号和完整金额可见。
// 无收款凭证时最终确认按钮应保持禁用。
// 填写凭证后检查实际 POST 的记录编号和正文。
// 请求不能携带可以篡改的结清金额。
// 成功后观察新查询，避免界面停留在旧欠款快照。
// 测试不通过手动修改缓存来冒充刷新成功。
// 授信读取错误用例不提供任何成功响应。
// 错误时必须保留重试按钮，不能显示无用户。
// 所有网络请求使用虚构地址路径，不访问真实支付服务。
// 测试边界只替换 HTTP，不替换生产业务组件。
// 共享 Dialog 的焦点和可见性仍在真实 DOM 中执行。
// 等待条件绑定用户可观察结果，不依赖固定睡眠时间。
// 每个用例结束释放 QueryClient，避免异步查询跨场景运行。
// 身份清理在卸载之后执行，确保后续场景没有残余授权。
// 输入的收款凭证不是实际银行或商户交易号。
// 这些测试验证前端接线，不代替后端事务正确性验证。
// 并发额度、重复结清和退款原子性由真实三库测试覆盖。
// 业务端权限隔离另由实际认证中间件请求验证。
// 没有运行真实线下收款或向用户发送付款消息。
// 前端测试成功也不意味着已部署到生产站点。
const account = {
  user_id: 8, // 选中用户与财务流水归属对应。
  username: 'credit-user', // 虚构用户名避免真实业务信息入库。
  display_name: 'Credit User', // 选择器同时显示名称用于人工核对。
  enabled: true, // 授信总开关明确开启。
  limit_cents: 20000, // 总授信为人民币二百元。
  used_cents: 6000, // 六十元占用应从可用额度扣除。
  recharge_enabled: true, // 本期唯一已接入模块。
  topup_enabled: false, // 钱包充值不得提前开放。
  subscription_enabled: false, // 订阅仍为预留能力。
  version: 2, // 编辑应回传真实版本。
}
const entry = {
  id: 7, // 结清路径只能使用账本记录编号。
  user_id: 8,
  username: 'credit-user',
  display_name: 'Credit User',
  module: 'recharge', // 模块标识不能从页面猜测。
  trade_no: 'RCHUSR-credit-test', // 确认框展示冻结业务交易号。
  amount_cents: 6000, // 待结清金额来自服务器。
  status: 'outstanding', // 未结清时才出现收款登记入口。
  created_at: 1700000000, // 固定时间用于稳定的历史显示。
  settled_at: 0, // 尚未收款不能显示伪造到账时间。
  settled_by: 0, // 首次结清前没有收款登记人。
  settlement_ref: '', // 必须通过真实输入补齐凭证。
  settlement_note: '', // 备注可以为空。
  closed_at: 0, // 未退款时不应有关闭时间。
  closed_by: 0, // 当前尚未发生撤销动作。
  refund_ref: '', // 不存在实际退款凭证。
}
const clients: QueryClient[] = [] // 清理全部缓存与挂起请求。
// 每个测试独立缓存，失败用例不能继承之前的成功记录。
function mount(element: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client) // 记录当前实例供统一卸载。
  return render(
    <QueryClientProvider client={client}>{element}</QueryClientProvider>
  )
}
afterEach(() => {
  cleanup() // 组件先卸载，防止残余查询污染下个测试。
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks() // HTTP边界回到原始实现。
  useAuthStore.getState().auth.reset('complete') // 财务授权不能跨测试残留。
})

// 配置页须可真实选人、编辑额度和模块，不能只验证组件挂载。
// 挂帐保存独立于支付网关整页表单，金额需从元转为整数分。
test('添加用户授信并按人民币分保存', async () => {
  const user = userEvent.setup() // 通过真实控件输入，不直接改内部状态。
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: String(url).endsWith('/users')
        ? [{ id: 8, username: 'credit-user', display_name: 'Credit User' }]
        : { items: [], total: 0 },
    },
  }))
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  mount(<CreditAccounts admin />) // 验证实际支付配置业务组件。
  await user.click(screen.getByRole('button', { name: 'Add user' }))
  const dialog = await screen.findByRole('dialog') // 确认弹层确实渲染到可访问树。
  await within(dialog).findByRole('option', {
    name: '#8 · credit-user Credit User',
  })
  await user.selectOptions(within(dialog).getByLabelText('User'), '8') // 真实用户选择以编号保存。
  await user.clear(within(dialog).getByLabelText('Credit limit (CNY)'))
  await user.type(within(dialog).getByLabelText('Credit limit (CNY)'), '123.45') // 输入元金额应精确转换为分。
  expect(
    within(dialog).getByText(
      'Wallet top-up and subscriptions: not available yet.'
    )
  ).toBeVisible()
  await user.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/option/credit/accounts',
      {
        user_id: 8,
        enabled: true,
        limit_cents: 12345,
        recharge_enabled: true,
        version: 0,
      },
      { singleUseAuthorization: true } // 写入不允许认证恢复自动重放。
    )
  )
})

// 编辑必须保留读到的版本，不能上传服务端只读占用或审计字段。
test('编辑授信可关闭代充值授权并保留并发版本', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { items: [account], total: 1 } },
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  mount(<CreditAccounts admin />)
  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  const dialog = screen.getByRole('dialog') // 编辑使用统一弹层组件。
  await user.click(
    within(dialog).getByRole('switch', { name: 'Account top-up' })
  )
  await user.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/option/credit/accounts',
      {
        user_id: 8,
        enabled: true,
        limit_cents: 20000,
        recharge_enabled: false,
        version: 2,
      },
      { singleUseAuthorization: true }
    )
  )
})

// 财务整笔结清必须经过可核对金额的确认框，并要求非空收款凭证。
// 失败后保留原记录与凭证，不能在请求未成功时假装恢复授信。
test('财务登记整笔结清并刷新账本与授信', async () => {
  const user = userEvent.setup()
  useAuthStore.getState().auth.setUser({
    id: 5,
    username: 'finance',
    role: 5,
    permissions: {
      admin_permissions: { finance: { read: true, write: true } },
    },
  })
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { items: [entry], total: 1, amount_cents: 6000 },
    },
  })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  mount(<CreditManagement />) // 整个财务布局接入真实表格与弹窗。
  await user.click(
    await screen.findByRole('button', { name: 'Record full settlement' })
  )
  const dialog = screen.getByRole('alertdialog') // 资金动作采用显式确认层。
  expect(within(dialog).getByText(/RCHUSR-credit-test · CNY 60/)).toBeVisible()
  expect(
    within(dialog).getByRole('button', { name: 'Confirm settlement' })
  ).toBeDisabled()
  await user.type(
    within(dialog).getByLabelText('Settlement reference'),
    'offline-fixture'
  )
  await user.click(
    within(dialog).getByRole('button', { name: 'Confirm settlement' })
  )
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/finance/credit/entries/7/settle',
      { reference: 'offline-fixture', note: '' },
      { singleUseAuthorization: true }
    )
  )
  await waitFor(() => expect(get.mock.calls.length).toBeGreaterThan(1))
})

// 只读用户可对账，不能因看见记录就获得结清按钮。
test('只读财务隐藏结清，读取错误不呈现空账本', async () => {
  useAuthStore.getState().auth.setUser({
    id: 5,
    username: 'finance',
    role: 5,
    permissions: {
      admin_permissions: { finance: { read: true, write: false } },
    },
  })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { items: [entry], total: 1, amount_cents: 6000 },
    },
  })
  mount(<CreditManagement />)
  await screen.findByText('RCHUSR-credit-test') // 只读用户仍能看到真实记录。
  expect(
    screen.queryByRole('button', { name: 'Record full settlement' })
  ).toBeNull()
})

// 服务端故障必须有重试入口，不能把 undefined 当作正常空数组。
test('授信读取失败提供重试且不显示无用户', async () => {
  vi.spyOn(api, 'get').mockRejectedValue(new Error('network unavailable'))
  mount(<CreditAccounts admin />)
  await screen.findByRole('button', { name: /Try again|Retry/i })
  expect(screen.queryByText('No on-account users')).toBeNull() // 故障不能伪装成未配置授信。
})
