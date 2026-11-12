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
  act,
  render,
  renderHook,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import zh from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { BillingHistoryDialog } from '../components/dialogs/billing-history-dialog'
import { PaymentConfirmDialog } from '../components/dialogs/payment-confirm-dialog'
import { RechargeFormCard } from '../components/recharge-form-card'
import { usePayment } from '../hooks/use-payment'
import { useTopupInfo } from '../hooks/use-topup-info'

// 覆盖人民币输入、服务端冻结额度和付款展示的真实接线。
// HTTP 是唯一替换边界，不模拟 Hook、货币工具或付款弹窗。
// CNY 输入不是美元数量，预设与确认框均不能再乘汇率。
// 到账来自服务器 credited_quota，不能仅依据输入假定成功。
// 汇率变更改变内部单位，但以人民币显示的到账金额应保持不变。
// 折扣只减少付款，不减少原请求应到账的额度。
// 失败与并发报价必须清空旧结果，不能为另一金额沿用旧付款数据。
// 本测试不发起真实付款，不接触真实商户或账户余额。
// 服务端报价金额与冻结额度是两个字段，不能彼此替代。
// 内部额度单位保留整数，展示工具负责映射到站点人民币。
// 该映射只解释金额，不意味着浏览器已完成支付。
// 所有交易标识和账户均是测试 fixture，不指向真实支付订单。
// 预设与自定义输入共用父级报价；单独修文字不能修复下单金额。
// 首个网络场景保留七点三的站点换算，才能稳定复现旧问题。
// 第二种换算证明新逻辑没有把七点三作为前端常量。
// 折扣场景保护请求一元额度和实际支付八角能独立展示。
// 中文资源来自真实语言文件，防止新增公式只在英文界面可读。
// 通过区域可访问名称查询公式，不绑定视觉布局或完整类名。
// 账单复用原有读接口，验证新的冻结字段不破坏已付款历史。
// 缺少冻结字段只对人民币本地报价失败，不扩大国际网关约束。
// 报价时提交的 amount 保持原输入，不能前端先换算后后端再换算。
// 金额变化与清空分别验证不同生命周期，不能合并为简单渲染冒烟。
// Promise 屏障明确控制响应顺序，不用随机延迟模拟并发。
// 测试采用实际公共组件，确认按钮的禁用属性也属于可见契约。
// 付款处理器是否调用由其他官方支付测试保护，本文件聚焦报价到展示。
// 每个 Hook 都在自己的 React 渲染范围运行，不共享异步报价缓存。
// 本文件不对后端汇率算法自证正确，只使用独立固定的网络结果。
// 将配置还原与 HTTP 清理放在收尾，使其他钱包测试仍可使用美元。
const i18n = createInstance()
const originalConfig = useSystemConfigStore.getState().config // 完整保存站点币种快照，不能只还原汇率。

beforeEach(async () => {
  // 每项用例显式配置 CNY，避免继承其他测试的系统币种。
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType: 'CNY',
      usdExchangeRate: 7.3, // 保留用户报告中的旧倍率作为回归条件。
    },
  })
  await i18n.use(initReactI18next).init({
    lng: 'en',
    fallbackLng: 'en',
    resources: { en, zh }, // 使用真实译文，漏登记公式会在中文断言中失败。
    nsSeparator: false, // 公式中的标点不能被语言库解释为命名空间。
    interpolation: { escapeValue: false },
  })
})

afterEach(() => {
  // 恢复全局配置与外部请求，测试顺序不应影响实际币种。
  useSystemConfigStore.getState().setConfig(originalConfig)
  vi.restoreAllMocks() // 恢复请求边界，后续场景不得复用上次失败响应。
})

// 使用真实报价 Hook，不以组件 props 伪造服务端到账数据。
// 两个金额故意不同，防止将实付折扣错误应用到充值额度。
test('人民币报价保留服务器入账额度且折扣只影响支付', async () => {
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { message: 'success', data: '0.80', credited_quota: 68493 }, // 八角支付仍购买完整一元额度。
  })
  const { result } = renderHook(() => usePayment())
  await act(async () => {
    await result.current.calculatePaymentAmount(1, 'official_wechat') // 官方微信也走普通本地报价合同。
  })
  expect(result.current.amount).toBe(0.8) // 应付不再乘默认七点三，优惠由服务器决定。
  expect(result.current.creditedQuota).toBe(68493) // 保留整数单位，不能用付款额替代入账额。
  expect(api.post).toHaveBeenCalledWith(
    '/api/user/amount',
    { amount: 1 }, // 充值输入按元原样提交，不预先折成美元小数。
    expect.any(Object)
  )
})

// 先给合法报价再给失败响应，验证第二次失败不能留下上次金额。
// 错误在 HTTP 边界给出，不通过修改 Hook 内部状态制造条件。
test('人民币报价失败或缺少入账字段时不可沿用旧金额', async () => {
  vi.spyOn(api, 'post')
    .mockResolvedValueOnce({
      data: { message: 'success', data: '1.00', credited_quota: 68493 },
    })
    .mockResolvedValueOnce({ data: { message: 'error', data: 'quote failed' } }) // 业务失败也必须清空报价。
    .mockResolvedValueOnce({ data: { message: 'success', data: '2.00' } }) // 缺少入账字段不能假装完整成功。
  const { result } = renderHook(() => usePayment())
  await act(async () => {
    await result.current.calculatePaymentAmount(1, 'wxpay')
  })
  await act(async () => {
    expect(await result.current.calculatePaymentAmount(2, 'wxpay')).toBeNull()
  })
  expect(result.current.amount).toBe(0) // 请求失败后不再保留旧应付金额。
  expect(result.current.creditedQuota).toBeNull()
  await act(async () => {
    expect(await result.current.calculatePaymentAmount(2, 'wxpay')).toBeNull()
  })
  expect(result.current.creditedQuota).toBeNull()
})

// 同样一元到账在两种汇率下有不同内部单位，展示不可重新乘汇率。
// 服务端四舍五入到内部单位，所以使用可显示为一元的显式 fixture。
test.each([
  { rate: 7.3, quota: 68493, paid: 1 }, // 原站点换算下的一元充值。
  { rate: 5, quota: 100000, paid: 1 }, // 换算改变内部单位，用户仍看到一元。
  { rate: 7.3, quota: 68493, paid: 0.8 }, // 优惠不缩减入账额度。
])(
  '汇率 $rate 下人民币确认显示一元并随语言切换',
  async ({ rate, quota, paid }) => {
    useSystemConfigStore.getState().setConfig({
      currency: {
        ...DEFAULT_CURRENCY_CONFIG,
        quotaDisplayType: 'CNY',
        usdExchangeRate: rate,
      },
    })
    render(
      <I18nextProvider i18n={i18n}>
        <PaymentConfirmDialog
          open
          onOpenChange={vi.fn()}
          onConfirm={vi.fn()}
          topupAmount={1}
          paymentAmount={paid} // 使用表中实付值，避免折扣场景误沿用原价。
          paymentMethod={{ name: 'WeChat', type: 'wxpay' }}
          calculating={false}
          processing={false}
          localCnyInput // 显式区分本地人民币与原美元输入语义。
          creditedQuota={quota} // 不让组件根据输入或实付重新推算冻结单位。
          quoteValid // 模拟当前金额与渠道已取得完整服务端报价。
        />
      </I18nextProvider>
    )
    // 用完整公式核对三个独立概念，不能只断言任意一处出现数字一。
    expect(
      screen.getByRole('status', { name: 'Payment breakdown' })
    ).toHaveTextContent(
      `Recharge ¥1 → Pay ¥${paid} → Receive ¥1 in platform credit`
    )
    await act(async () => {
      await i18n.changeLanguage('zh') // 在同一已挂载弹窗中切换，保护语言订阅。
    })
    expect(
      screen.getByRole('status', { name: '充值换算说明' })
    ).toHaveTextContent(`充值 ¥1 → 支付 ¥${paid} → 到账平台额度 ¥1`)
  }
)

// 预设按钮沿用真实钱包组件，防止只修确认框却保留七点三倍预设。
// 旧版价格配置故意保持 7.3，CNY 普通渠道仍应标记支付一元。
test('人民币预设一元不会再次乘显示汇率或旧美元单价', () => {
  render(
    <RechargeFormCard
      topupInfo={{
        enable_online_topup: true, // 只开启本次人民币普通渠道。
        enable_stripe_topup: false, // 国际网关不应参与这份人民币预设断言。
        pay_methods: [{ name: 'WeChat', type: 'wxpay' }],
        min_topup: 1,
        stripe_min_topup: 1,
        amount_options: [1], // 一元预设必须仍展示一而非七点三。
        discount: {},
      }}
      presetAmounts={[{ value: 1 }]}
      selectedPreset={null}
      topupAmount={1}
      paymentAmount={1}
      priceRatio={7.3} // 保持旧配置不变，修复不能要求用户改成特殊售价。
      usdExchangeRate={7.3} // 输入已是元，预设标题不能再次乘率。
      localCnyInput // 显式区分本地人民币与原美元输入语义。
      calculating={false}
      paymentLoading={null}
      redemptionCode=''
      redeeming={false}
      onSelectPreset={vi.fn()}
      onTopupAmountChange={vi.fn()}
      onPaymentMethodSelect={vi.fn()}
      onRedemptionCodeChange={vi.fn()}
      onRedeem={vi.fn()}
    />
  )
  expect(screen.getByRole('button', { name: /^1 Pay 1$/ })).toBeVisible()
})

// 授信付款不依赖微信商户；全部现金渠道关闭时，金额区仍须可操作。
// 直接渲染真实充值卡片，避免钱包 Hook 的网络桩意外开启现金网关。
// 该回归只保护入口显示与交互，付款接口由其他网络边界测试验证。
test('仅开放钱包挂帐且未配置现金渠道时仍可输入金额并选择挂帐', async () => {
  const user = userEvent.setup()
  // 三个开关与真实授信摘要一致，不把 credit 塞进 pay_methods。
  const credit = {
    enabled: true,
    limit_cents: 10000,
    used_cents: 1000,
    topup_enabled: true,
    subscription_enabled: true,
    recharge_enabled: true,
  }
  const changeAmount = vi.fn()
  const selectCredit = vi.fn()
  render(
    <RechargeFormCard
      topupInfo={{
        enable_online_topup: false,
        enable_stripe_topup: false,
        pay_methods: [],
        min_topup: 1,
        stripe_min_topup: 1,
        amount_options: [],
        discount: {},
        credit,
      }}
      credit={credit}
      onCreditSelect={selectCredit}
      presetAmounts={[]}
      selectedPreset={null}
      topupAmount={1}
      paymentAmount={1}
      localCnyInput
      calculating={false}
      paymentLoading={null}
      redemptionCode=''
      redeeming={false}
      onSelectPreset={vi.fn()}
      onTopupAmountChange={changeAmount}
      onPaymentMethodSelect={vi.fn()}
      onRedemptionCodeChange={vi.fn()}
      onRedeem={vi.fn()}
    />
  )
  // 必须存在可访问金额输入，不能仅在隐藏 DOM 中保留入口。
  const amount = screen.getByRole('spinbutton', { name: 'Custom Amount' })
  expect(amount).toBeVisible()
  await user.clear(amount)
  await user.type(amount, '25')
  expect(changeAmount).toHaveBeenLastCalledWith(25)
  // 挂帐按钮实际执行选择回调，而不是用普通网关按钮冒充可用。
  const pay = screen.getByRole('button', { name: 'On account' })
  expect(pay).toBeEnabled()
  await user.click(pay)
  expect(selectCredit).toHaveBeenCalledOnce()
  expect(screen.queryByText(/Online topup is not enabled/)).toBeNull()
})

// 旧请求晚到不能覆盖当前金额；序号只对前端报价有效，不替代后端验价。
// 用显式 Promise 控制网络完成顺序，测试不依赖固定延迟。
test('人民币金额变化后只保留最新服务端报价', async () => {
  let finishFirst: (response: unknown) => void = () => {
    throw new Error('first request not started')
  }
  const first = new Promise((resolve) => {
    // 第一笔报价故意延迟到第二笔完成后。
    finishFirst = resolve
  })
  vi.spyOn(api, 'post')
    .mockImplementationOnce(async () => first) // 模拟网络乱序，不操作 Hook 的私有状态。
    .mockResolvedValueOnce({
      data: { message: 'success', data: '2.00', credited_quota: 136986 },
    })
  const { result } = renderHook(() => usePayment())
  let oldRequest: Promise<number | null> | undefined // 保存未完成调用，确保测试结束前真正释放。
  await act(async () => {
    oldRequest = result.current.calculatePaymentAmount(1, 'wxpay')
  })
  expect(result.current.creditedQuota).toBeNull()
  await act(async () => {
    await result.current.calculatePaymentAmount(2, 'wxpay') // 当前输入已变为两元，后续只能接受这份报价。
  })
  await act(async () => {
    finishFirst({
      data: { message: 'success', data: '1.00', credited_quota: 68493 },
    })
    await oldRequest // 等待旧网络完成，才能证明其未覆盖当前数据。
  })
  expect(result.current.amount).toBe(2) // 不能在旧请求晚到后退回一元应付。
  expect(result.current.creditedQuota).toBe(136986) // 金额与额度必须同属第二笔报价。
})

// 新易支付订单的 amount 已是人民币输入，历史展示必须使用冻结入账额度。
// 不因渠道名称不是 official 就再次把 amount 当成美元放大。
test('人民币易支付账单使用冻结额度展示到账一元', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        total: 1,
        items: [
          {
            id: 1,
            user_id: 8,
            amount: 1, // 新订单请求单位是元，不能再当一美元读取。
            credited_quota: 68493, // 账单以冻结单位为准，避免再次乘显示汇率。
            money: 1,
            trade_no: 'cny-epay-order',
            payment_method: 'wxpay',
            payment_provider: 'epay', // 特意使用非官方标识，保护易支付也采用冻结额度。
            create_time: 1,
            status: 'success', // 已成功历史记录不允许继续创建付款。
          },
        ],
      },
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } }, // 不让外部请求重试掩盖 fixture 或接线错误。
  })
  render(
    <QueryClientProvider client={client}>
      <BillingHistoryDialog open onOpenChange={vi.fn()} />
    </QueryClientProvider>
  )
  expect(await screen.findByText('cny-epay-order')).toBeVisible()
  await waitFor(() => expect(screen.getByText('¥1')).toBeVisible())
  expect(screen.queryByText('¥7.3')).not.toBeInTheDocument() // 旧倍率产生的错误历史金额必须消失。
  client.clear() // 本场景查询缓存不泄漏给之后的账单用例。
})

// 清空输入会使正在运行的旧报价失效，也必须立即释放加载状态。
// 旧请求最终完成不能重新恢复已清空的金额和付款额度。
test('报价中清空金额后加载状态结束且不接受旧响应', async () => {
  let finish: (value: unknown) => void = () => {
    throw new Error('request not started')
  }
  const response = new Promise((resolve) => {
    // 清空时仍保留一笔未完成网络请求。
    finish = resolve
  })
  vi.spyOn(api, 'post').mockImplementationOnce(async () => response)
  const { result } = renderHook(() => usePayment())
  let pending: Promise<number | null> | undefined
  await act(async () => {
    pending = result.current.calculatePaymentAmount(1, 'wxpay')
  })
  expect(result.current.calculating).toBe(true) // 确认清空动作发生于加载中，而不是完成后。
  await act(async () => {
    await result.current.calculatePaymentAmount(0, 'wxpay') // 零是输入清空状态，不发起零元付款报价。
  })
  expect(result.current.calculating).toBe(false) // 旧请求被作废后不能继续让界面永久加载。
  await act(async () => {
    finish({
      data: { message: 'success', data: '1.00', credited_quota: 68493 },
    })
    await pending // 不遗留悬挂 Promise，避免后续测试收到异步状态更新。
  })
  expect(result.current.quote).toBeNull() // 空输入不能被迟到的一元网络响应重新填上。
})

// 单独保护付款按钮，隐藏旧公式不代表用户已无法提交旧金额。
// 未取得权威入账额度时必须明确提示并拒绝确认。
test('无当前报价时人民币付款确认按钮禁用', () => {
  render(
    <I18nextProvider i18n={i18n}>
      <PaymentConfirmDialog
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
        topupAmount={1}
        paymentAmount={0} // 失败状态只是空展示，不意味着存在零元交易。
        paymentMethod={{ name: 'WeChat', type: 'wxpay' }}
        calculating={false}
        processing={false}
        localCnyInput // 显式区分本地人民币与原美元输入语义。
        creditedQuota={null} // 不为缺失服务端字段生成默认到账值。
        quoteValid={false} // 当前金额没有有效报价，确认动作必须被禁止。
      />
    </I18nextProvider>
  )
  expect(screen.getByRole('button', { name: 'Confirm Payment' })).toBeDisabled()
  expect(
    screen.getByRole('status', { name: 'Payment breakdown' })
  ).toHaveTextContent('Payment details unavailable. Please retry.')
})

// 人民币站点不意味着所有收款网关都改为人民币数量。
// 国际网关的既有报价缺少冻结字段仍可使用，不能误套本地强校验。
// 逐个验证现有专属端点，防止按站点币种把它们送入普通报价。
test.each([
  ['stripe', '/api/user/stripe/amount'], // Stripe 仍用已有美元报价。
  ['waffo', '/api/user/waffo/amount'], // Waffo 的独立定价不使用普通人民币规则。
  ['waffo_pancake', '/api/user/waffo-pancake/amount'], // Pancake 保留自己的金额端点。
])('人民币展示下国际渠道 %s 保持原报价合同', async (method, endpoint) => {
  // 国际报价沿用旧响应结构，没有人民币本地冻结额度字段。
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: '2.00' },
  })
  const { result } = renderHook(() => usePayment())
  await act(async () => {
    await result.current.calculatePaymentAmount(1, method)
  })
  expect(result.current.amount).toBe(2) // 不因显示CNY就重新把美元报价换成一元。
  expect(result.current.creditedQuota).toBeNull() // 未提供的额度保持未知，不猜测填值。
  expect(api.post).toHaveBeenCalledWith(
    endpoint,
    { amount: 1 },
    expect.any(Object)
  )
})

// 配置金额折扣（如 100 享 0.98、30000 享 0.9）时，充值页必须展示所有配置的折扣套餐，不再展示默认的 10/20/50。
// 同时验证即使后端仍返回旧的默认 amount_options，前端 Hook 也会以折扣配置为准生成升序充值 Plan。
test('配置金额折扣时充值页展示全部折扣套餐并过滤默认档位', async () => {
  await i18n.changeLanguage('zh') // 切换到中文界面，验证折扣与实付节省金额的中文文案。
  // 模拟后端返回默认 10/20/50/100 选项以及管理员配置的 100 和 30000 两个折扣档位。
  vi.spyOn(api, 'get').mockResolvedValueOnce({
    data: {
      success: true,
      data: {
        enable_online_topup: true,
        enable_stripe_topup: false,
        pay_methods: [{ name: 'WeChat', type: 'wxpay' }],
        min_topup: 1,
        stripe_min_topup: 1,
        amount_options: [10, 20, 50, 100], // 旧默认档位不应覆盖或截断折扣套餐。
        discount: {
          '100': 0.98, // 100 元享 98 折（优惠 2%，实付 98，节省 2）。
          '30000': 0.9, // 30000 元享 9 折（优惠 10%，实付 27,000，节省 3,000）。
        },
      },
    },
  })

  // 通过真实 Hook 拉取并解析充值配置，验证生成的 presetAmounts 只含配置的折扣档位。
  const { result } = renderHook(() => useTopupInfo())
  await waitFor(() => expect(result.current.loading).toBe(false))
  expect(result.current.presetAmounts).toEqual([
    { value: 100, discount: 0.98 },
    { value: 30000, discount: 0.9 },
  ])

  // 将 Hook 产出的套餐传入真实充值卡片组件，验证用户可见的套餐按钮与折扣金额。
  render(
    <I18nextProvider i18n={i18n}>
      <RechargeFormCard
        topupInfo={result.current.topupInfo}
        presetAmounts={result.current.presetAmounts}
        selectedPreset={null}
        topupAmount={100}
        paymentAmount={98}
        priceRatio={7.3}
        usdExchangeRate={7.3}
        localCnyInput // 本地人民币渠道按元面额计算实付與优惠额。
        calculating={false}
        paymentLoading={null}
        redemptionCode=''
        redeeming={false}
        onSelectPreset={vi.fn()}
        onTopupAmountChange={vi.fn()}
        onPaymentMethodSelect={vi.fn()}
        onRedemptionCodeChange={vi.fn()}
        onRedeem={vi.fn()}
      />
    </I18nextProvider>
  )

  // 断言 100 与 30000 两个充值 Plan 完整展示，且未配置折扣的默认 10/20/50 不再出现。
  expect(screen.getByRole('button', { name: /^100 / })).toHaveTextContent(
    '优惠 2%支付 98 • 节省 2'
  )
  expect(screen.getByRole('button', { name: /^30,000 / })).toHaveTextContent(
    '优惠 10%支付 27,000 • 节省 3,000'
  )
  expect(screen.queryByRole('button', { name: /^10 / })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /^20 / })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /^50 / })).not.toBeInTheDocument()
})
