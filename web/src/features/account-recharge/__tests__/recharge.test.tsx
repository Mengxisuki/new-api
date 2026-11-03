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

import type { Order, Product } from '../api'
import { RechargeCheckout } from '../checkout'
import { AccountRecharge } from '../index'
import { RechargeOrderDetail } from '../order-detail'
import { RechargeProducts } from '../products'
import { orderStatus } from '../status'

// 只替换 HTTP 边界，
// 保留真实表单、弹层、缓存和能力判断。
// 所有邮箱和 session都是无法登录的虚构值，
// 不访问ChatGPT 或支付商户。
const product: Product = {
  id: 1,
  title: 'Monthly plan',
  description: 'Eligible accounts only',
  summary: 'Plus membership benefits',
  provider: 'OpenAI',
  period: '1 month',
  delivery_time: 'Within 24 hours',
  icon: 'OpenAI',
  price_cents: 15000,
  enabled: true,
  allow_wallet_payment: false,
  sort_order: 0,
  version: 2,
}
const order: Order = {
  id: 1,
  user_id: 8,
  product_id: 1,
  title: product.title,
  description: product.description,
  summary: product.summary,
  provider: product.provider,
  period: product.period,
  delivery_time: product.delivery_time,
  icon: product.icon,
  base_cents: 15000,
  money_cents: 15000,
  trade_no: 'RCHUSR-test',
  payment_method: 'wxpay',
  payment_provider: 'epay',
  payment_status: 'paid',
  status: 'awaiting_info',
  assignee_id: 0,
  email_mask: '',
  public_note: '',
  refund_requested: false,
  wallet_sync_pending: false,
  version: 2,
  created_at: 1700000000,
  updated_at: 1700000000,
  paid_at: 1700000001,
  completed_at: 0,
  expires_at: 1900000000,
}
const clients: QueryClient[] = []
function mount(ui: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } }, // 失败测试不等待重试退避。
  })
  clients.push(client)
  render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
  return client
}
afterEach(() => {
  cleanup() // 先卸载组件，确保轮询和敏感计时器停止。
  clients.splice(0).forEach((client) => client.clear()) // 清除安全摘要缓存，测试间不共享已付款状态。
  vi.restoreAllMocks() // 请求代理复原，避免把成功响应带进失败场景。
  useAuthStore.getState().auth.reset('complete') // 身份与权限在测试间隔离，后续买家不能继承 Root。
})

// 余额开关关闭时不暴露该支付方式，
// 订单提交必须携带已确认的总价。
// 创建后交给原订单继续付款，
// 不在关闭确认框时重复下单。
// 测试选择真实的已配置微信选项，
// 确认不允许余额时没有隐藏可选值。
// 商品本身的余额开关是false，
// 而不是测试单独 mock支付菜单。
// 可信报价来自接口返回，
// 断言创建参数包含确认后的人民币分金额。
// 商品编号和版本一起进入请求，
// 防止用户确认的是旧价格但创建新套餐。
// 请求 UUID 只断言存在，
// 不固定生产随机数实现，
// 避免测试绑定内部细节。
// 创建成功只通知父页面打开原订单，
// 不把它当作已付款结果。
// 测试没有跳转真实网关，
// 网络边界返回不可支付的虚构订单。
// 价格断言不复写税费计算，
// 只检查服务端报价被前端原样绑定。
// 订单详情由其他用例覆盖，
// 本例聚焦购买选择与创建契约。
// QueryClient 独立创建，
// 不能继承其他订单的旧报价解锁按钮。
// 选择控件使用真实可访问名称，
// 确保用户能够用键盘识别支付方式。
// 测试也保护关闭余额后的显示文案，
// 不以禁用按钮代替移除方式。
test('checkout excludes disabled wallet payment and binds the confirmed quote', async () => {
  const user = userEvent.setup()
  const created = vi.fn()
  const post = vi.spyOn(api, 'post').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: url.endsWith('/quote')
        ? { money_cents: 15000, base_cents: 15000, wallet_quota: 0 }
        : order,
    },
  }))
  mount(
    <RechargeCheckout
      product={product}
      catalog={{
        items: [product],
        payment_methods: [{ id: 'epay:wxpay', name: 'WeChat Pay' }],
        configured: true,
      }}
      onClose={vi.fn()}
      onCreated={created}
    />
  )
  expect(
    screen.queryByRole('option', { name: 'Platform balance' })
  ).not.toBeInTheDocument()
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Create order and continue' })
    ).toBeEnabled()
  )
  await user.click(
    screen.getByRole('button', { name: 'Create order and continue' })
  )
  await waitFor(() => expect(created).toHaveBeenCalledWith(order))
  expect(post).toHaveBeenCalledWith(
    '/api/account-recharge/orders',
    expect.objectContaining({
      product_id: 1,
      product_version: 2,
      payment_method: 'epay:wxpay',
      quoted_cents: 15000,
      invoice_required: false,
      request_id: expect.any(String),
    }),
    expect.objectContaining({ singleUseAuthorization: true })
  )
})

// 提交成功后用户看到统一处理中，
// 原始凭据不留在 ReactQuery 缓存。
// 下次补交由服务端状态决定，
// 重复提交不能从页面自行重新入队。
// 测试使用真实输入框和提交事件，
// 不通过直接调用 submitInfo绕过页面。
// 服务端状态在提交后变为待领取，
// 用于观察用户是否看到统一处理中。
// JSON 使用无法登录的固定 fixture，
// 原始字符串不包含任何真实凭据。
// 邮箱与完整 session 都必须进入同一资料接口，
// 不能写到订单备注。
// 提交请求携带当前订单版本，
// 以便后端拒绝旧页面覆盖。
// 成功后原输入框消失，
// 前端不继续展示已提交的凭据原文。
// 查询缓存只保留安全订单摘要，
// 测试扫描缓存值确认没有 session。
// 操作缓存必须为空，
// 防止useMutation的变量历史保存认证正文。
// 待领取和办理中两种状态得到相同买家文案，
// 避免泄漏内部队列阶段。
// 操作失败的服务端行为由后端测试覆盖，
// 此处不复制订单状态机。
// 表单采用页面实际绑定的处理器，
// 能够发现忘记接提交按钮的问题。
// 测试结束清理挂载及缓存，
// 不能让虚构凭据流入其他用例。
test('account submission clears the form and never caches session JSON', async () => {
  const user = userEvent.setup()
  let submitted = false
  vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: {
      success: true,
      data: {
        order: { ...order, status: submitted ? 'pending' : 'awaiting_info' },
      },
    },
  }))
  const post = vi.spyOn(api, 'post').mockImplementation(async () => {
    submitted = true
    return { data: { success: true, data: null } }
  })
  const client = mount(
    <RechargeOrderDetail id={1} operations={false} onClose={vi.fn()} />
  )
  await user.type(
    await screen.findByLabelText('Account email'),
    'owner@example.com'
  )
  const session = '{"accessToken":"private-fixture"}'
  await user.click(screen.getByLabelText('Complete session JSON'))
  await user.paste(session)
  await user.click(
    screen.getByRole('button', { name: 'Submit for processing' })
  )
  expect(await screen.findByText('Order processing')).toBeVisible()
  expect(
    screen.queryByLabelText('Complete session JSON')
  ).not.toBeInTheDocument()
  expect(post).toHaveBeenCalledWith(
    '/api/account-recharge/orders/1/account-info',
    { version: 2, email: 'owner@example.com', session },
    expect.objectContaining({ singleUseAuthorization: true })
  )
  expect(
    JSON.stringify(
      client
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data)
    )
  ).not.toContain('private-fixture')
  expect(client.getMutationCache().getAll()).toHaveLength(0)
  expect(orderStatus({ ...order, status: 'processing' })).toBe(
    orderStatus({ ...order, status: 'pending' })
  )
})

// 业务员可领取队列订单，
// 不能看到执行资金退款的按钮。
// 领取成功刷新后才允许继续办理，
// 未领取时不暴露完整资料。
// 业务员身份通过真实认证 store的公开接口设置，
// 而不是 mock权限判断。
// 权限矩阵只有读取、处理与揭示，
// 明确缺少转派及资金退款能力。
// 未领取订单有领取按钮，
// 但不能提前看到查看账户信息的入口。
// 点击领取后先等待服务器摘要刷新，
// 再检查后续办理能力是否出现。
// 接口请求中的处理人由服务端决定，
// 前端领取参数不指定自己或他人。
// 请求版本仍为领取前的版本，
// 刷新后才获取新版本用于下一动作。
// 退款审批按钮在业务员页面必须不存在，
// 不能只是样式隐藏。
// 转派按钮同样不出现，
// 防止把业务员误识别为普通管理员。
// 测试不模拟真实安全证明，
// 敏感读取的证明校验由后端回归负责。
// 网络 mock 只改变摘要，
// UI的角色、状态及按钮条件保持生产实现。
// 界面成功不能由按钮点击直接推导，
// 测试必须等到真实查询刷新。
// 保留同一订单编号验证领取后的页面继续操作，
// 不重新挂载伪造成功页。
test('sales claims an order without receiving administrator money controls', async () => {
  useAuthStore.getState().auth.setUser({
    id: 5,
    username: 'sales',
    role: 3,
    permissions: {
      admin_permissions: {
        recharge_orders: { read: true, write: true, reveal: true },
      },
    },
  })
  let claimed = false
  vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: {
      success: true,
      data: {
        order: {
          ...order,
          status: claimed ? 'processing' : 'pending',
          assignee_id: claimed ? 5 : 0,
          version: claimed ? 3 : 2,
        },
        events: [],
      },
    },
  }))
  const post = vi.spyOn(api, 'post').mockImplementation(async () => {
    claimed = true
    return { data: { success: true, data: null } }
  })
  mount(<RechargeOrderDetail id={1} operations onClose={vi.fn()} />)
  expect(
    await screen.findByRole('button', { name: 'Claim order' })
  ).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'View account information' })
  ).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'Claim order' }))
  expect(
    await screen.findByRole('button', { name: 'View account information' })
  ).toBeVisible()
  expect(post).toHaveBeenCalledWith(
    '/api/account-recharge/operations/orders/1/action',
    { version: 2, action: 'claim', note: '', assignee_id: 0 },
    expect.any(Object)
  )
  expect(
    screen.queryByRole('button', { name: 'Approve refund' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Assign order' })
  ).not.toBeInTheDocument()
})

// 商品管理默认关闭余额，
// 编辑保存必须真实传递 false而非遗漏字段。
// 使用真实表单校验验证用户可操作入口，
// 不能只断言配置对象常量。
// 管理员通过真实表格编辑入口打开表单，
// 能发现布局插槽未接线的问题。
// 原商品余额开关初始为true，
// 点击后检查保存请求确实发送false。
// 测试不依赖字段是否省略，
// 而是断言最终接口参数中的明确布尔值。
// 版本必须保持打开表单时的值，
// 客户端不能自行累加绕开乐观锁。
// 表单使用真实校验与提交，
// 测试不会直接调用 saveProduct替代用户操作。
// 商品 ID 必须保留，
// 避免编辑被误当成创建另一份套餐。
// 真实公共 Dialog 和Field 参与渲染，
// 辅助技术能找到对应开关标签。
// 保存成功才关闭表单，
// 不把点击保存按钮本身作为成功依据。
// 网络边界仅返回服务器认可的商品摘要，
// 不涉及任何付款操作。
// QueryClient 失效行为使用生产实现，
// 能发现保存后列表仍被旧缓存覆盖。
// 本例只检查单商品编辑，
// 不制造没有业务价值的大量随机套餐。
// 用例结束恢复 API spies和用户状态，
// 避免后续普通用户测试继承Root。
test('product editor persists the balance switch and version', async () => {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        items: [{ ...product, allow_wallet_payment: true }],
        payment_methods: [],
        configured: true,
      },
    },
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true, data: product } })
  mount(<RechargeProducts />)
  await userEvent.click(await screen.findByRole('button', { name: 'Edit' }))
  const dialog = screen.getByRole('dialog') // 限定编辑弹层，避免误点列表的其他操作按钮。
  await userEvent.click(
    within(dialog).getByRole('checkbox', {
      name: 'Allow platform balance payment',
    })
  )
  // 分别修改简介和周期，图标清空必须提交空串而非沿用旧值。
  await userEvent.clear(within(dialog).getByLabelText('Plan summary'))
  await userEvent.type(
    within(dialog).getByLabelText('Plan summary'),
    'Updated benefits'
  )
  await userEvent.clear(within(dialog).getByLabelText('Plan period'))
  await userEvent.type(
    within(dialog).getByLabelText('Plan period'),
    '12 months'
  )
  await userEvent.click(within(dialog).getByRole('button', { name: 'Clear' }))
  await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/account-recharge/admin/products',
      expect.objectContaining({
        id: 1,
        version: 2, // 客户端保留原版本，不能自行累加掩盖更新冲突。
        summary: 'Updated benefits',
        period: '12 months',
        description: product.description, // 其他展示字段保持原值，不把简介写回说明。
        icon: '',
        allow_wallet_payment: false, // 明确提交 false，遗漏字段不能代表关闭余额付款。
      })
    )
  )
})

// 权限可能在详情打开后被撤销，
// 后台刷新失败时不能继续展示旧的敏感入口。
// 仅替换网络响应，
// 保留真实查询缓存与能力判断来复现旧摘要仍存在的场景。
test('failed order refresh disables access to account information', async () => {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { order: { ...order, status: 'processing', assignee_id: 1 } },
    },
  })
  const client = mount(
    <RechargeOrderDetail id={1} operations onClose={vi.fn()} />
  )
  expect(
    await screen.findByRole('button', { name: 'View account information' })
  ).toBeVisible()
  get.mockRejectedValue(new Error('Access denied')) // 模拟打开详情之后权限被撤销。
  await client.invalidateQueries({ queryKey: ['recharge'] }) // 真实刷新保留旧 data，用于复现失败后仍显示敏感入口。
  await waitFor(() =>
    expect(
      screen.queryByRole('button', { name: 'View account information' })
    ).not.toBeInTheDocument()
  )
})

// 从真实页面的商品卡打开结账，保护 SectionPageLayout 的弹层插槽接线。
// 直接测试结账组件无法发现父布局丢弃未识别子节点的回归。
test('choosing a product opens checkout inside the account recharge page', async () => {
  useAuthStore.getState().auth.setUser({ id: 8, username: 'buyer', role: 1 })
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: url.endsWith('/products')
        ? {
            items: [product],
            payment_methods: [{ id: 'epay:wxpay', name: 'WeChat Pay' }],
            configured: true, // 验证业务已启用时的可购买路径，不绕开部署门禁。
          }
        : { items: [], total: 0 }, // 本人订单正常为空，不能把查询失败伪装为空列表。
    },
  }))
  vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: { money_cents: 15000, base_cents: 15000, wallet_quota: 0 },
    },
  })
  mount(<AccountRecharge />) // 必须通过父布局打开结账，直接挂载弹层发现不了插槽错误。
  // 卡片先展示结构信息，长购买说明由用户主动展开。
  expect(await screen.findByText(product.summary)).toBeVisible()
  expect(screen.getByText(product.period)).toBeVisible()
  expect(screen.getByText(product.delivery_time)).toBeVisible()
  expect(screen.getByText(product.description)).not.toBeVisible()
  await userEvent.click(screen.getByText('Purchase instructions'))
  expect(screen.getByText(product.description)).toBeVisible()

  await userEvent.click(
    await screen.findByRole('button', { name: 'Choose plan' })
  )
  expect(
    await screen.findByRole('dialog', { name: 'Confirm recharge order' })
  ).toBeVisible()
  expect(screen.getByRole('combobox', { name: 'Payment method' })).toHaveValue(
    'epay:wxpay' // 已配置微信是默认支付方式，不依赖余额开关。
  )
})

// 退回原因属于履约反馈，不能混入购买时的套餐说明。
// 使用区域名称验证信息归属，避免仅靠颜色区分。
// 超长正文仍须可读，布局要有明确的滚动边界。
// 表单保持真实可访问标签，补交操作仍位于右侧区域。
// 小屏采用单列，桌面才展开两列，不固定设备像素值。
// 测试使用非敏感说明，不读取用户真实会话。
test('returned orders separate purchase information, staff feedback and responsive actions', async () => {
  const longDescription = 'Long purchase instructions '.repeat(100)
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        order: {
          ...order,
          status: 'action_required',
          description: longDescription,
          public_note: 'Session expired; please resubmit',
        },
      },
    },
  })
  mount(<RechargeOrderDetail id={1} operations={false} onClose={vi.fn()} />)
  const information = await screen.findByRole('region', {
    name: 'Order information',
  })
  const actions = screen.getByRole('region', { name: 'Order actions' })
  expect(within(information).getByText(product.summary)).toBeVisible()
  expect(within(information).getByText('Purchase instructions')).toBeVisible()
  expect(within(information).getByText(longDescription.trim())).toBeVisible()
  const feedback = within(information).getByRole('region', {
    name: 'Reason for additional information',
  })
  expect(
    within(feedback).getByText('Session expired; please resubmit')
  ).toBeVisible()
  expect(
    within(actions).queryByText(product.description)
  ).not.toBeInTheDocument()
  expect(within(actions).getByLabelText('Complete session JSON')).toBeVisible()
  expect(information.parentElement).toHaveClass('grid', 'lg:grid-cols-2')
  expect(information).toHaveClass('lg:overflow-y-auto')
  expect(actions).toHaveClass('lg:overflow-y-auto')
  expect(screen.getByRole('dialog')).toHaveClass('sm:max-w-5xl')
})
