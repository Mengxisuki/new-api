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
import { act, render, renderHook, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { InvoiceProfileForm } from '@/features/invoices/components/invoice-profile-form'
import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { usePayment } from '../hooks/use-payment'

// 本文件覆盖用户端发票链路的公开行为，服务端税费算法不在浏览器重新实现。
// 充值测试使用一百元面额和六元附加费，确保总实付与到账明确不同。
// 付款返回二维码只表示拿到付款资料，不能视为余额已经到账。
// 缺抬头场景直接检查HTTP未发生，防止仅靠隐藏按钮掩盖可调用路径。
// 快捷配置测试读取真实Form字段，不伪造表单内部提交状态。
// 企业与个人条件由生产表单负责，测试不会复制校验公式。
// 保存成功断言包括服务器归属，确认提交不携带可替换所有者ID。
// 套餐测试独立使用订阅报价端点，不让充值报价误用于购买时长。
// 同一个开票布尔值必须同时进入报价和付款请求。
// 已配置资料通过真实查询进入选择器，不直接塞入购买组件的内部状态。
// 默认否场景保留Controlled组件的真实外层更新过程。
// 无配置时出现的Dialog继续使用同一份InvoiceProfileForm。
// 本人发票测试故意设置十一条总数，以要求真正请求第二页。
// 测试只提供当前页数据，无法通过前端过滤全部申请而侥幸通过。
// 发票交易号随页变化，可以发现界面仍保留上一页订单的问题。
// 下载边界使用本地虚构PDF，不访问实际税务文件或真实商户。
// Blob URL只替代浏览器不可控API，鉴权接口的选择仍由生产代码决定。
// 下载完成检查revoke，避免包含敏感资料的临时引用长期留存。
// 样例税号、邮箱、账号和二维码均为虚构测试数据。
// QueryClient独立建立，避免其他用户测试的抬头缓存混入本场景。
// 所有可见入口按角色或label查找，不依赖CSS类名或完整DOM快照。
// 异步操作等待对应文本或网络调用，不以固定延迟猜测渲染结束。
// 守护错误和重试行为的其他用例由钱包报价测试负责，不重复堆积。
// 后端负责付款真实性、附件所有者和审批权限，本文件不声称替代服务端验证。
const oldConfig = useSystemConfigStore.getState().config // 保存完整系统配置，币种改变后需要还原。

afterEach(() => {
  vi.restoreAllMocks() // HTTP与浏览器模拟不能泄漏到下一个用户场景。
  vi.unstubAllGlobals() // 用例中途失败也要恢复下载API，不能只依赖成功路径。
  useSystemConfigStore.getState().setConfig(oldConfig) // 其他钱包用例仍按自己的币种合同执行。
})

// 真实报价Hook必须把开票选择传给后端，不能只改变前端显示金额。
// 加价属于实付，服务端冻结到账额度不能跟着手续费增加。
test('选择开票后报价和下单都携带选择并保持原到账额度', async () => {
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType: 'CNY', // 本地人民币开票报价具有元面额合同。
      usdExchangeRate: 1, // 固定存储换算，测试聚焦开票附加费是否混入到账。
    },
  })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValueOnce({
      data: {
        message: 'success', // 旧充值报价保留message合同，不能只检查success布尔值。
        data: '106.00', // 总支付含六元附加费。
        credited_quota: 50000000, // 平台额度仍是一百元对应的整数单位。
        invoice_required: true, // 报价必须确认本次确实包含发票。
        invoice_base_amount: 100, // 基础实付与附加费分离，不由组件再推导。
        invoice_fee_percent: 6, // 后端选择分组费率，浏览器仅展示。
        invoice_fee_amount: 6, // 费用是付款组成，不是额外模型额度。
      },
    })
    .mockResolvedValueOnce({
      data: {
        success: true,
        data: {
          payment_kind: 'qr', // 本地微信付款走二维码而非旧易支付表单。
          qr_code: 'weixin://invoice', // 虚构二维码不会向实际商户发起扣款。
          trade_no: 'invoice-order', // 订单标识只用作本次支付资料展示。
        },
      },
    })
  const { result } = renderHook(
    () => usePayment({ invoiceRequired: true, invoiceReady: true }) // ready来自服务器已确认抬头。
  )
  await act(async () => {
    await result.current.calculatePaymentAmount(100, 'wxpay') // 不在前端给一百元面额预加六元。
  })
  expect(post).toHaveBeenNthCalledWith(
    1,
    '/api/user/amount',
    { amount: 100, invoice_required: true },
    expect.any(Object)
  )
  expect(result.current.quote).toMatchObject({
    amount: 106, // 消费者可见的应付以服务器报价为准。
    creditedQuota: 50000000, // 独立核对未把附加费兑换成额度。
    invoiceRequired: true, // 下单前能够识别报价所对应的发票选择。
    invoiceFeeAmount: 6, // 明细保存原值，不通过总额减面额猜测费用。
  })
  await act(async () => {
    await result.current.processPayment(100, 'wxpay') // 真实处理器提交原面额和开票标记。
  })
  expect(post).toHaveBeenNthCalledWith(
    2,
    '/api/user/pay',
    { amount: 100, payment_method: 'wxpay', invoice_required: true },
    expect.any(Object)
  )
})

// 缺少配置时阻止报价和付款，不让默认空字段创建开票请求。
test('选择开票但尚无有效抬头时不发送付款请求', async () => {
  const post = vi.spyOn(api, 'post') // 未就绪时连报价都不能向后端发送。
  const { result } = renderHook(
    () => usePayment({ invoiceRequired: true, invoiceReady: false }) // 模拟资料读取中或未配置，不能使用空默认抬头。
  )
  await act(async () => {
    await result.current.calculatePaymentAmount(1, 'wxpay') // 一元合法面额也不能绕过必需资料。
  })
  expect(result.current.quote).toBeNull() // 未取得报价时不给确认框伪造费用或到账。
  await act(async () => {
    expect(await result.current.processPayment(1, 'wxpay')).toBe(false) // 直接调用公共付款行为也要受同一约束。
  })
  expect(post).not.toHaveBeenCalled() // 禁止真实网络写入是该失败场景的关键契约。
})

// 常规配置与快捷配置共享表单，保存只包含允许编辑的字段。
// 客户端不发送 user_id，防止资料归属在浏览器被修改。
test('快捷配置抬头保存成功后返回服务端资料', async () => {
  const user = userEvent.setup() // 通过真实键盘和点击驱动公共控件，不直接调用内部事件。
  const saved = {
    title_type: 'business', // 企业类型要求税号，不能只靠名称通过保存。
    title: 'Research Lab', // 与登录用户名无关的明确开票抬头。
    tax_id: 'TESTTAX123', // 使用文本样例，保留税号原有字符。
    email: 'invoice@example.com', // 邮箱由用户主动填写，不复制身份资料。
    address: '', // 可选字段仍以空串提交，清空旧资料语义明确。
    phone: '',
    bank_name: '',
    bank_account: '', // 未配置银行资料也可以保存有效企业抬头。
  }
  const put = vi.spyOn(api, 'put').mockResolvedValue({
    data: { success: true, data: { ...saved, user_id: 7 } }, // 归属来自服务端返回，不能由表单指定。
  })
  const onSaved = vi.fn() // 观察父层解锁付款的公开保存成功通知。
  render(
    <QueryClientProvider client={new QueryClient()}>
      <InvoiceProfileForm profile={null} onSaved={onSaved} />
    </QueryClientProvider>
  )
  await user.type(screen.getByLabelText(/Invoice title \*/), saved.title)
  await user.type(
    screen.getByLabelText(/Taxpayer identification number/),
    saved.tax_id
  )
  await user.type(screen.getByLabelText(/Invoice email/), saved.email)
  await user.click(screen.getByRole('button', { name: 'Save invoice title' }))
  expect(put).toHaveBeenCalledWith('/api/user/invoice-profile', saved) // 精确核对白名单字段，不发送user_id。
  expect(onSaved).toHaveBeenCalledWith(
    expect.objectContaining({ title: saved.title, user_id: 7 })
  )
})

// 订阅开票与充值共用抬头选择，报价来自订阅端点，不能手算套餐附加费。
// 保存的抬头通过真实查询读取，付款 body 必须明确携带开票选择。
test('订阅选择开票后展示服务端加价并向官方渠道直接付款', async () => {
  const user = userEvent.setup() // 通过真实键盘和点击驱动公共控件，不直接调用内部事件。
  const { SubscriptionPurchaseDialog } =
    await import('@/features/subscriptions/components/dialogs/subscription-purchase-dialog')
  const { subscriptionPlanSchema } =
    await import('@/features/subscriptions/types')
  const profile = {
    title_type: 'business',
    title: 'Invoice Lab', // 数据通过真实GET进入下拉区，不用私有状态替换。
    tax_id: 'TESTTAX',
    email: 'invoice@example.com',
    address: '',
    phone: '',
    bank_name: '',
    bank_account: '',
  }
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: profile },
  })
  const post = vi.spyOn(api, 'post').mockImplementation(async (url) => ({
    data:
      url === '/api/subscription/invoice-quote'
        ? {
            message: 'success',
            data: '106.00', // 订阅应付由其专属报价接口返回。
            invoice_required: true, // 显式确认附加费属于本次开票报价。
            invoice_fee_percent: 6,
            invoice_fee_amount: 6,
          }
        : {
            success: true,
            data: {
              payment_kind: 'qr',
              qr_code: 'weixin://subscription-invoice',
              trade_no: 'SUBUSR-invoice', // 订阅交易前缀保证付款没有绕去充值钱包。
            },
          },
  }))
  const plan = subscriptionPlanSchema.parse({
    id: 9, // 卡片、报价与付款必须引用同一个套餐。
    title: 'Monthly',
    price_amount: 100, // 套餐价与额度分别设置，开票不应增加使用配额。
    duration_unit: 'month', // 月份权益仍由服务端处理，前端不增加时长。
    duration_value: 1,
    quota_reset_period: 'never', // 非周期额度套餐同样使用直购开票流程。
    enabled: true,
    total_amount: 1000, // 与售价故意不同，防止把费用当模型额度。
    sort_order: 0,
    max_purchase_per_user: 0, // 本用例不混入历史限购分支。
  })
  const client = new QueryClient({
    // 每个付款场景拥有独立服务端数据缓存。
    defaultOptions: { queries: { retry: false } }, // 失败测试不等待框架自动重试造成不确定行为。
  })
  render(
    <QueryClientProvider client={client}>
      <SubscriptionPurchaseDialog
        open
        onOpenChange={vi.fn()}
        plan={{ plan }}
        officialMethods={[
          {
            name: 'WeChat Pay',
            type: 'official_wechat', // 官方渠道不进入易支付表单分支。
            provider: 'official', // 用户可见名称不作为网关类型判断依据。
            channel_type: 'wechat',
          },
        ]}
      />
    </QueryClientProvider>
  )
  await user.click(
    screen.getByRole('combobox', { name: 'Do you need an invoice?' })
  )
  await user.click(screen.getByRole('option', { name: 'Yes' }))
  expect(await screen.findByText('Invoice title: Invoice Lab')).toBeVisible()
  await screen.findByText(/Invoicing surcharge/)
  await user.click(screen.getByRole('button', { name: 'WeChat Pay' }))
  expect(await screen.findByText('SUBUSR-invoice')).toBeVisible()
  expect(post).toHaveBeenCalledWith('/api/subscription/invoice-quote', {
    plan_id: 9,
    invoice_required: true,
  })
  expect(post).toHaveBeenCalledWith('/api/subscription/official/pay', {
    plan_id: 9,
    payment_method: 'official_wechat',
    invoice_required: true,
  })
  client.clear() // 清理本场景保存的发票抬头和订阅报价。 // 清理可能包含税务信息的内存测试缓存。
})

// 是否开票默认否；选择是且尚未配置时直接展示真实快捷配置表单。
// 通过公开下拉交互验证，不能只断言内部布尔状态。
test('开票默认否且选择是后无抬头可快捷配置', async () => {
  const user = userEvent.setup() // 通过真实键盘和点击驱动公共控件，不直接调用内部事件。
  const { InvoiceSelection } = await import('../components/invoice-selection')
  const changed = vi.fn() // 外层拥有选择，控件不能暗中持久化同意开票。
  const client = new QueryClient() // 快捷表单仍走项目既有缓存上下文。
  const selection = (required: boolean) => (
    <QueryClientProvider client={client}>
      <InvoiceSelection
        required={required}
        onRequiredChange={changed}
        profile={null} // 服务端已确认未配置，区别于加载中的undefined。
        loading={false} // 明确不是请求暂未完成导致的数据缺失。
        error={false} // 网络故障不能触发覆盖配置的空表单。
        onRetry={vi.fn()}
        onSaved={vi.fn()}
      />
    </QueryClientProvider>
  )
  const view = render(selection(false)) // 首次默认不开票，不能主动弹出敏感资料表单。
  expect(
    screen.getByRole('combobox', { name: 'Do you need an invoice?' })
  ).toHaveTextContent('No')
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  await user.click(
    screen.getByRole('combobox', { name: 'Do you need an invoice?' })
  )
  await user.click(screen.getByRole('option', { name: 'Yes' }))
  expect(changed).toHaveBeenCalledWith(true) // 下拉交互必须准确通知报价父层。
  view.rerender(selection(true)) // 模拟父层接受用户选择，保留受控组件合同。
  expect(
    await screen.findByRole('dialog', { name: 'Invoice title' })
  ).toBeVisible()
  expect(
    screen.getByRole('button', { name: 'Save invoice title' })
  ).toBeVisible()
  client.clear() // 清理可能包含税务信息的内存测试缓存。
})

// 本人发票采用服务端分页，不能只加载前一百份申请后过滤账单。
// 文件下载使用有会话的API，不把敏感附件发布成公共链接。
test('我的发票下一页重新查询并通过鉴权接口下载已开票附件', async () => {
  const user = userEvent.setup() // 通过真实键盘和点击驱动公共控件，不直接调用内部事件。
  const { UserInvoicesList } = await import('../components/user-invoices-list')
  const invoice = {
    id: 2, // 申请主键是下载接口的唯一对象标识。
    user_id: 7, // 列表所有者由服务器确定，读取API不接收可替换ID。
    trade_no: 'invoice-file-order', // 确保申请与真实付款单能在界面关联。
    business_type: 'topup', // 钱包和订阅申请可以共用本人发票列表。
    profile: {
      title_type: 'personal', // 本人发票也支持没有企业税号的个人抬头。
      title: 'Researcher',
      tax_id: '',
      email: 'invoice@example.com',
      address: '',
      phone: '',
      bank_name: '',
      bank_account: '',
    },
    base_amount: 100, // 原充值实付与附加费分别冻结。
    fee_rate: 6, // 这是下单时费率，不依据当前分组设置重算。
    fee_amount: 6, // 用户可区分附加费与模型额度。
    pay_amount: 106, // 下载记录对应的发票金额是实际总支付。
    currency: 'CNY', // 历史发票不随站点显示币种被重标。
    status: 'issued', // 只有已开票记录才可以展示附件下载按钮。
    invoice_number: 'INV-2', // 文件名展示用号码，服务器仍按申请ID读取。
    remark: '',
    has_file: true, // 状态和确有文件必须共同满足下载条件。
    created_at: 1,
    updated_at: 1,
    issued_at: 1,
  }
  const get = vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    if (String(url).endsWith('/file')) {
      return { data: new Blob(['%PDF-test'], { type: 'application/pdf' }) }
    }
    const page = (config?.params as { page?: number })?.page ?? 1
    return {
      data: {
        success: true,
        data: {
          items: [{ ...invoice, id: page, trade_no: `invoice-page-${page}` }],
          total: 11, // 大于一页，用明确总数要求再次访问服务器。
          page,
          size: 10, // 列表与后端分页使用同一固定页大小。
        },
      },
    }
  })
  const create = vi.fn(() => 'blob:invoice-test') // 不在测试文件中生成真实公开附件地址。
  const revoke = vi.fn() // 校验下载结束确实释放敏感Blob引用。
  vi.stubGlobal(
    'URL',
    class extends URL {
      static createObjectURL = create // 仅替换浏览器实现，不替换业务下载函数。
      static revokeObjectURL = revoke // 与创建时使用的对象地址完全一致。
    }
  )
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  const client = new QueryClient({
    // 每个付款场景拥有独立服务端数据缓存。
    defaultOptions: { queries: { retry: false } }, // 失败测试不等待框架自动重试造成不确定行为。
  })
  render(
    <QueryClientProvider client={client}>
      <UserInvoicesList open />
    </QueryClientProvider>
  )
  expect(await screen.findByText('invoice-page-1')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Next' }))
  expect(await screen.findByText('invoice-page-2')).toBeVisible()
  expect(get).toHaveBeenCalledWith(
    '/api/user/invoices',
    expect.objectContaining({
      params: { page: 2, size: 10 },
      disableDuplicate: true,
    })
  )
  await user.click(screen.getByRole('button', { name: 'Download invoice' }))
  expect(get).toHaveBeenCalledWith(
    '/api/user/invoices/2/file',
    expect.objectContaining({ responseType: 'blob', disableDuplicate: true })
  )
  expect(create).toHaveBeenCalled()
  expect(revoke).toHaveBeenCalledWith('blob:invoice-test')
  client.clear() // 清理税务记录缓存，其他测试不能继承本人发票。 // 清理可能包含税务信息的内存测试缓存。
  vi.unstubAllGlobals() // 提前恢复浏览器全局；afterEach另有失败路径兜底清理。
})
