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
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import {
  act,
  render,
  renderHook,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { SubscriptionPurchaseDialog } from '@/features/subscriptions/components/dialogs/subscription-purchase-dialog'
import { subscriptionPlanSchema } from '@/features/subscriptions/types'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { BillingHistoryDialog } from '../components/dialogs/billing-history-dialog'
import { SubscriptionPlansCard } from '../components/subscription-plans-card'
import { usePayment } from '../hooks/use-payment'
import type { TopupInfo } from '../types'

// 钱包弹窗常驻页面，关闭后再次查询必须重新读取服务端订单。
// 请求替换在 HTTP 边界，保留真实弹窗及账单 Hook 的生命周期。
test('订单查询每次打开时立即获取新数据且不复用旧请求', async () => {
  const get = vi
    .spyOn(api, 'get')
    .mockResolvedValueOnce({
      data: {
        success: true,
        data: {
          items: [
            {
              id: 1,
              trade_no: 'first-order',
              amount: 1,
              money: 1,
              status: 'pending',
              payment_method: 'wxpay',
              create_time: 1,
            },
          ],
          total: 1,
        },
      },
    })
    .mockResolvedValueOnce({
      data: {
        success: true,
        data: {
          items: [
            {
              id: 2,
              trade_no: 'new-order',
              amount: 1,
              money: 1,
              status: 'success',
              payment_method: 'wxpay',
              create_time: 1,
            },
          ],
          total: 1,
        },
      },
    })
  const view = render(
    <BillingHistoryDialog open={false} onOpenChange={vi.fn()} />
  )
  expect(get).not.toHaveBeenCalled()
  view.rerender(<BillingHistoryDialog open onOpenChange={vi.fn()} />)
  expect(await screen.findByText('first-order')).toBeVisible()
  view.rerender(<BillingHistoryDialog open={false} onOpenChange={vi.fn()} />)
  view.rerender(<BillingHistoryDialog open onOpenChange={vi.fn()} />)
  expect(await screen.findByText('new-order')).toBeVisible()
  expect(screen.queryByText('first-order')).not.toBeInTheDocument()
  expect(get).toHaveBeenCalledTimes(2)
  expect(get).toHaveBeenLastCalledWith(
    expect.stringContaining('/api/user/topup/self?'),
    expect.objectContaining({ disableDuplicate: true })
  )
})

// 本文件保护官方付款迁移与续费接线的用户契约。
// 下单成功表示获取付款资料，不代表支付到账或订阅已经开通。
// 模拟 HTTP 响应是为了隔离真实商户系统，测试不使用可支付二维码。
// 支付结果由后端验签，前端测试不伪造或绕过该校验。
// 微信二维码使用本地虚构协议内容，不会连接任何真实支付账户。
// 普通充值和订阅购买分属不同的服务器端点。
// 这一区分能阻止把“先充值余额再买订阅”作为隐式中转路径。
// 原有钱包余额仍服务于按量调用，测试不要求删除钱包本身。
// 用户余额充足也不会产生订阅余额购买按钮。
// 同套餐续期只延长服务时间，周额度刷新由后端专属测试负责。
// 前端负责保证历史限购不错误禁用活跃套餐的续费入口。
// 同时保留套餐到期后原有历史限购规则，避免扩大购买权限。
// 不读取 React 内部状态推断续期资格，直接检查用户可用按钮。
// 列表需要真实异步加载，断言等待明确按钮而非固定时长。
// 弹窗仍使用真实项目组件，缺少 Provider 等集成问题会直接暴露。
// 请求只允许携带套餐和渠道标识，不让浏览器计算续期价格。
// 服务端决定实际交易金额，测试没有复制后端金额计算算法。
// 账单可以展示多个用户，但普通用户不能为他人继续付款。
// 管理员的查看权限与超级管理员的人工补单权限相互独立。
// 重试不创建新订单，沿用原订单号才能匹配原付款通知。
// 过期按钮不显示只是界面约束，服务端还要重新校验订单状态。
// 倒计时使用远离边界的明确时间，避免运行耗时导致偶发失败。
// 重试后的二维码必须带回原订单标识，防止误展示上次订单。
// 成功返回不能让前端自行给钱包增加额度或给用户创建订阅。
// 这些账务约束由后端事务测试验证，本文件只覆盖界面接线。
// 测试直接观察 API 调用，因此 Epay/钱包误分流会造成失败。
// 不 mock 本次被测 Hook 或购买弹窗，避免测试只验证模拟实现。
// 每个场景独立安装 HTTP spy，不依赖上一用例缓存或网络结果。
// 查询缓存只存在于场景自己的 Provider 内，不共享支付对象。
// 认证用户通过公开 store API 建立，不能绕过实际角色读取逻辑。
// 完成后还原认证和 HTTP 边界，其他钱包用例不会继承身份。
// 支付二维码关闭后的状态清理防止下次购买展示旧订单。
// 响应式尺寸和真实商户跳转需浏览器检查，jsdom 断言不替代它们。
// 不使用完整 DOM 快照，迁移后的组件可继续演进布局。
// 公共按钮的可访问名称是查找入口，用户可读动作变化会被发现。
// 测试不自动进行真实付款，也不保存或接触商户密钥。

// 同一套餐 fixture 同时验证选购卡片和付款弹窗，避免两处测试输入产生差异。
// 数据经生产 schema 解析，新增必需字段会使测试真实失败。
// 测试售价与周额度分开，不能把周额度当作应付价格。
// 套餐限购一次用于复现“历史次数已满但应允许续费”的回归。
const plan = subscriptionPlanSchema.parse({
  id: 7, // 卡片与订单必须引用同一公开套餐。
  title: 'Monthly Research',
  price_amount: 10, // 售价独立于下方使用额度。
  duration_unit: 'month', // 月付不决定额度刷新间隔。
  duration_value: 1,
  quota_reset_period: 'weekly', // 本测试保护周额度套餐也走直购。
  enabled: true,
  sort_order: 0,
  max_purchase_per_user: 1, // 续期必须绕过历史实例数的限购提示。
  total_amount: 100,
})

// 在 HTTP 边界提供微信下单结果，保留真实 Hook 的分发和状态更新。
// 二维码订单不能误走 Epay 表单，也不能宣称钱包已到账。
// 关闭二维码仅清理展示状态，到账由服务端支付通知决定。
test('微信下单返回二维码时展示订单并可清理支付状态', async () => {
  // 隔离付款服务，只提供本次场景的交易资料。
  vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: {
        payment_kind: 'qr', // 显式交互类型避免按旧 Epay 表单处理。
        qr_code: 'weixin://wxpay/test',
        trade_no: 'wx-order-1', // 非订阅前缀明确是钱包充值单。
      },
    },
  })
  // 保留真实分发逻辑；未接二维码分支时会返回 false。
  const { result } = renderHook(() => usePayment())
  // 等待异步下单与 React 状态提交后再检查用户结果。
  await act(async () => {
    expect(await result.current.processPayment(10, 'official_wechat')).toBe(
      true
    )
  })
  // 原始金额只进入充值端点，不能误发到订阅购买接口。
  expect(api.post).toHaveBeenCalledWith(
    '/api/user/pay',
    { amount: 10, payment_method: 'official_wechat' },
    expect.any(Object)
  )
  // 订单号和二维码必须来自同一次响应。
  expect(result.current.qrPayment).toEqual({
    qrCode: 'weixin://wxpay/test',
    tradeNo: 'wx-order-1',
  })
  // 清理显示状态不发新付款请求，也不重新计算余额。
  act(() => result.current.clearQrPayment())
  expect(result.current.qrPayment).toBeNull()
})

// 订阅卡片现在导航到真实 Key 路由，不能再脱离 Router 单独挂载。
// 只替换 HTTP 边界，购买弹窗和订阅卡片的状态关联仍使用生产实现。
// 内存历史隔离浏览器导航，不 mock Link 或把路由组件替换成普通标签。
// 查询缓存属于当前场景，禁止失败自动重试导致额外付款或读请求。
// topupInfo 只提供公开渠道配置，测试不需要商户密钥。
// 辅助函数只装配本文件反复使用的订阅卡片，不建立通用渲染框架。
// 页面初始仍会读取公开套餐和本人订阅，不能用预填缓存绕过请求接线。
// 根路由不包含真实用户数据，身份隔离由服务端测试另行保护。
function renderSubscriptionCard(topupInfo: TopupInfo | null) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const router = createRouter({
    routeTree: createRootRoute({
      component: () => <SubscriptionPlansCard topupInfo={topupInfo} />,
    }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

// 活跃同套餐允许续期，历史限购计数仍用于到期后的新购买。
// 用可见按钮核对规则，不读取组件内部购买计数。
// 相同历史实例分别覆盖生效及过期两种购买边界。
test.each([
  { active: true, label: 'Renew' },
  { active: false, label: 'Limit Reached' },
])(
  '历史限购一次的套餐在活跃状态 $active 时展示 $label',
  async ({ active, label }) => {
    // 两种状态只改变有效期和状态，其他套餐输入保持一致。
    const now = Math.floor(Date.now() / 1000)
    const subscription = {
      id: 1,
      user_id: 1,
      plan_id: 7, // 订阅请求不携带充值数量或钱包扣款。
      status: active ? 'active' : 'expired',
      start_time: now - 100,
      end_time: active ? now + 3600 : now - 1,
      amount_total: 100,
      amount_used: 30, // 已消耗额度说明续期不是重新购买满额实例。
    }
    // 公开套餐与个人订阅分别通过真实接口读取。
    vi.spyOn(api, 'get').mockImplementation(async (url) => ({
      data: {
        success: true,
        data: String(url).endsWith('/plans')
          ? [
              {
                plan,
              },
            ]
          : {
              subscriptions: active ? [{ subscription }] : [], // 活跃列表与历史列表职责分开。
              all_subscriptions: [{ subscription }], // 历史记录不可因到期从限购计数里消失。
            },
      },
    }))
    renderSubscriptionCard(null)
    // 等待真实异步列表载入，而不是仅断言组件没有抛错。
    const button = await screen.findByRole('button', {
      name: label, // 用户读到的动作决定可否续期，不依赖 CSS 选择器。
    })
    expect(button).toBeVisible()
    // 活跃套餐不能因为历史购买次数已满而丢失续费。
    if (active) expect(button).toBeEnabled()
    // 到期后仍遵守原套餐限购，不能扩大为无限次重新购买。
    else expect(button).toBeDisabled()
  }
)

// 本场景保护旧入口退场后的完整用户路径，不只检查删除了几个文案。
// 有活跃订阅时仍能看到余量，无订阅时仍能选购公开套餐。
// 测试选择 subscription_only 作为旧响应值，能暴露残留的拒绝请求警告。
// 若仅返回旧默认值，残留状态可能刚好不显示警告而漏过检查。
// 全局写入 spy 与可见控件断言互补，避免保留隐藏副作用。
// 测试不要求后端立刻移除历史读取字段，只要求本页面停止消费它。
// 钱包订阅区只展示套餐和额度，不再保存影响所有 Key 的全局偏好。
// 同时覆盖首次购买与同套餐续费，防止删除选择器时误删支付或刷新入口。
// 两种场景都经过真实卡片打开购买弹窗，而不是直接渲染一个孤立弹窗。
// 旧偏好字段即使仍由历史后端返回，界面也不能据它生成钱包回退说明。
// 一个服务端响应即可证明显示层忽略旧字段，无需模拟后端迁移过程。
// 网络响应均为测试数据，不调用真实微信商户，也不创建可支付订单。
test.each([
  { renewal: false, action: 'Subscribe Now' },
  { renewal: true, action: 'Renew' },
])(
  '订阅卡片 $action 保留刷新和付款，额度顺序仅指向 Key',
  async ({ renewal, action }) => {
    const user = userEvent.setup()
    const now = Math.floor(Date.now() / 1000)
    // 生效订阅使用明确未到期时间；未购买场景返回真正空列表。
    // plan_id 与公开套餐一致，确保续费资格来自真实关联。
    // 历史限购一次仍保留，续费不得因为已有一份实例而变成禁用。
    // amount_used 后续会变化，用可见进度证明手动刷新实际更新了列表。
    const subscription = {
      id: 1,
      user_id: 1,
      plan_id: plan.id,
      status: 'active',
      start_time: now - 100,
      end_time: now + 3600,
      amount_total: 100,
      amount_used: 30,
    }
    // 读取计数只决定模拟外部响应，不修改页面中的订阅状态。
    // 首次挂载与手动刷新必须各有一次真实调用，缺失任一都会失败。
    // 使用较大的到期间隔，避免测试时钟刚好跨到套餐到期点。
    // 费用与额度分别来自套餐和订阅记录，不能把使用进度当应付金额。
    let selfReads = 0
    // 请求逐个匹配，未声明接口直接失败，不把任意地址都包装成订阅响应。
    // 公开套餐只读取一次；刷新本人订阅不应顺便改变在售套餐。
    // 当前订阅与全部历史使用同份记录，保留原卡片的状态和限购逻辑。
    // 第一次读 30%、第二次读 40%，无需定时器即可观察刷新生效。
    const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/subscription/plans') {
        return { data: { success: true, data: [{ plan }] } }
      }
      if (url === '/api/subscription/self') {
        selfReads += 1
        const items = renewal
          ? [
              {
                subscription: {
                  ...subscription,
                  amount_used: selfReads === 1 ? 30 : 40,
                },
              },
            ]
          : []
        return {
          data: {
            success: true,
            data: {
              billing_preference: 'subscription_only', // 历史字段不能恢复被移除的全局设置。
              subscriptions: items,
              all_subscriptions: items,
            },
          },
        }
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    // 写偏好走 PUT；即使错误被组件捕获，最终调用次数仍能揭露意外写入。
    // 支付走独立 POST，不能用禁掉所有写操作来让测试轻易通过。
    // 唯一允许的下单接口始终是订阅支付，不是钱包充值或余额兑换。
    const put = vi
      .spyOn(api, 'put')
      .mockRejectedValue(new Error('Unexpected preference write'))
    const post = vi.spyOn(api, 'post').mockImplementation(async (url) => {
      if (url !== '/api/subscription/official/pay') {
        throw new Error(`Unexpected POST ${url}`)
      }
      return {
        data: {
          success: true,
          data: {
            payment_kind: 'qr',
            qr_code: 'weixin://wxpay/subscription-card',
            trade_no: 'SUBUSR-card',
          },
        },
      }
    })
    // 官方渠道通过现有 provider 字段识别，沿用原来的订阅支付分发方式。
    // 在线充值能力关闭不影响独立官方订阅渠道，避免隐式先充值再买套餐。
    // 金额预设和最低充值字段只是合法公开配置，本场景没有充值动作。
    renderSubscriptionCard({
      enable_online_topup: false,
      enable_stripe_topup: false,
      pay_methods: [
        {
          type: 'official_wechat',
          name: 'WeChat Pay',
          provider: 'official',
          channel_type: 'wechat',
        },
      ],
      min_topup: 1,
      stripe_min_topup: 1,
      amount_options: [],
      discount: {},
    })
    // 卡片加载有异步状态，用真实可操作按钮作为首屏完成的标志。
    // 两个参数场景都保留套餐限购，新增订阅与续费的入口互不混淆。
    // 返回的按钮之后会用于打开弹窗，验证刷新没有销毁购买动作。
    const purchase = await screen.findByRole('button', { name: action })
    // 等待真实加载完成后断言，不能利用骨架阶段暂时没有选择器来假通过。
    // 链接只做导航，不会自动把套餐、钱包或挂靠账号加进某个 Key。
    // role=link 与 href 共同保护用户能发现并到达唯一策略入口。
    expect(
      screen.getByRole('link', {
        name: 'Configure resource priority in each API key.',
      })
    ).toHaveAttribute('href', '/keys')
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    expect(screen.queryByText('Subscription First')).not.toBeInTheDocument()
    expect(screen.queryByText('Wallet First')).not.toBeInTheDocument()
    expect(screen.queryByText(/Preference saved as/)).not.toBeInTheDocument()
    // 刷新保持原有只读语义，不因删除偏好状态而失去按钮事件绑定。
    // 按钮现在有可访问名称，读屏和测试都不需要通过图标路径识别它。
    // 额度进度来自第二次 HTTP 响应，不由测试直接操纵 React state。
    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    await waitFor(() =>
      expect(
        get.mock.calls.filter(([url]) => url === '/api/subscription/self')
      ).toHaveLength(2)
    )
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Refresh' })).toBeEnabled()
    )
    // 无订阅场景没有使用进度，不能为方便断言伪造一份历史记录。
    // 有订阅场景必须显示更新后的消耗比例，单看请求发生不足以证明刷新有效。
    // 不断言日期格式或金额换算细节，它们不属于本轮入口迁移范围。
    if (renewal) {
      expect(screen.getByText('Used 40%')).toBeVisible()
    }
    expect(
      get.mock.calls.filter(([url]) => url === '/api/subscription/plans')
    ).toHaveLength(1)
    expect(put).not.toHaveBeenCalled()
    expect(post).not.toHaveBeenCalled()
    // 原购买和续费按钮仍打开同一个真实弹窗，不能换成无行为占位入口。
    // 付款渠道通过卡片 props 传递，缺少接线会导致下面的渠道按钮不可见。
    // 弹窗打开本身不能创建订单，必须等待用户明确选择支付渠道。
    // 不 mock SubscriptionPurchaseDialog，以便 props 丢失或菜单误接线能被发现。
    // 关闭重开与其他支付渠道沿用原有测试，本次不改变这些实现。
    // 点击套餐按钮不应被链接导航截获，两个用户动作具有独立职责。
    await user.click(purchase)
    expect(
      await screen.findByRole('dialog', { name: 'Purchase Subscription' })
    ).toBeVisible()
    expect(post).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'WeChat Pay' }))
    // 二维码是订单创建结果，不代表支付已到账或套餐已经续期。
    // 检查真实订单号，确保父卡片没有丢失子弹窗的付款结果状态。
    // 两种购买类型都使用同一套餐 ID，实际续期由服务端决定。
    // 订单响应保持原二维码交互合同，测试不绕过支付 Hook 的结果解析。
    // 同一真实弹窗负责展示结果，避免卡片仅弹出标题却无法下单。
    // 这里不模拟支付成功回调，因此不能把二维码出现写成订阅已开通。
    // 已有独立付款测试继续覆盖无余额购买入口与原订单继续支付。
    expect(await screen.findByText('SUBUSR-card')).toBeVisible()
    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith('/api/subscription/official/pay', {
      plan_id: plan.id,
      payment_method: 'official_wechat',
    })
    // 从列表刷新到下单完整过程都没有写入全局偏好。
    // 此断言不会约束 Key 抽屉的独立策略保存，那是另一页面的职责。
    // 浏览器层保护入口归属，后端仍须自行拒绝过时接口与越权请求。
    expect(put).not.toHaveBeenCalled()
  }
)

// 订阅与续期必须通过支付网关扣款，不能再次消费钱包已付费的余额。
// HTTP spy 同时保护直购端点和“没有先充值/余额兑换”的调用边界。
// 二维码复用钱包组件，但本次资金用途必须始终是订阅订单。
test('订阅通过微信直接下单且没有余额购买入口', async () => {
  const user = userEvent.setup()
  // 即使账户已有充足钱包余额，订阅页面也只能选择真实支付网关。
  useAuthStore
    .getState()
    .auth.setUser({ id: 8, username: 'subscriber', role: 1, quota: 100000000 })
  // 隔离付款服务，只提供本次场景的交易资料。
  vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: {
        payment_kind: 'qr', // 显式交互类型避免按旧 Epay 表单处理。
        qr_code: 'weixin://wxpay/subscription',
        trade_no: 'SUBUSR-test', // 服务器按前缀匹配订阅到账业务。
      },
    },
  })
  render(
    <QueryClientProvider client={new QueryClient()}>
      <SubscriptionPurchaseDialog
        open
        onOpenChange={vi.fn()}
        plan={{ plan }}
        officialMethods={[
          {
            type: 'official_wechat',
            name: 'WeChat Pay',
            provider: 'official', // 此标记阻止渠道被误归类为易支付。
            channel_type: 'wechat', // 使用真实支持的商户协议类型。
          },
        ]}
      />
    </QueryClientProvider>
  )
  // 不仅检查直购可用，还检查旧余额入口已经真正移除。
  expect(
    screen.queryByRole('button', { name: 'Pay with Balance' })
  ).not.toBeInTheDocument()
  // 从用户可见渠道按钮启动，避免仅测试未接线的 API 函数。
  await user.click(screen.getByRole('button', { name: 'WeChat Pay' }))
  // 二维码结果继续保留订阅订单标识，不能变成钱包充值单。
  expect(await screen.findByText('SUBUSR-test')).toBeVisible()
  // 唯一一次请求排除了余额兑换或提前充值的隐藏绕道。
  expect(api.post).toHaveBeenCalledTimes(1)
  expect(api.post).toHaveBeenCalledWith('/api/subscription/official/pay', {
    plan_id: 7, // 订阅请求不携带充值数量或钱包扣款。
    payment_method: 'official_wechat',
  })
})

// 重试按钮只能属于当前用户且尚未过期的官方订单。
// 真实账单 Hook 在 HTTP 边界获取数据，避免模拟内部状态绕过归属逻辑。
// 重试原订单必须复用订单号，不能重新创建充值或更改金额。
test('账单仅允许本人未过期的官方订单继续付款并展示原订单二维码', async () => {
  const user = userEvent.setup()
  useAuthStore.getState().auth.setUser({ id: 8, username: 'payer', role: 1 })
  // 时间选在有效期内部和过期之后，不依赖秒级竞态。
  const now = Math.floor(Date.now() / 1000)
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        total: 3,
        items: [
          {
            id: 1,
            user_id: 8,
            status: 'pending',
            expires_at: now + 600,
            trade_no: 'wx-self',
          },
          {
            id: 2,
            user_id: 9,
            status: 'pending',
            expires_at: now + 600,
            trade_no: 'wx-other',
          },
          {
            id: 3,
            user_id: 8,
            status: 'pending',
            expires_at: now - 1,
            trade_no: 'wx-expired',
          },
        ].map((item) => ({
          ...item,
          amount: 10,
          money: 10,
          payment_method: 'official_wechat',
          payment_provider: 'official_wechat',
          create_time: now - 100,
        })),
      },
    },
  })
  // 隔离付款服务，只提供本次场景的交易资料。
  vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: { qr_code: 'weixin://wxpay/retry', trade_no: 'wx-self' },
    },
  })
  render(<BillingHistoryDialog open onOpenChange={vi.fn()} />)
  // 本人有效订单、他人订单、本人过期订单中只允许第一种重试。
  const retry = await screen.findByRole('button', { name: 'Retry payment' })
  expect(screen.getAllByRole('button', { name: 'Retry payment' })).toHaveLength(
    1
  )
  expect(
    screen.queryByRole('button', { name: 'Complete Order' })
  ).not.toBeInTheDocument()
  // 用户操作经过真实账单 Hook，不能跳过订单归属筛选。
  await user.click(retry)
  expect(
    await screen.findByRole('dialog', { name: 'Payment QR code' })
  ).toBeVisible()
  expect(api.post).toHaveBeenCalledWith(
    '/api/user/topup/retry',
    { trade_no: 'wx-self' }, // 重试不能更换原订单的业务归属。
    expect.any(Object)
  )
})

// 恢复请求边界，避免其他钱包用例继承模拟响应。
afterEach(() => {
  vi.restoreAllMocks() // 请求模拟不泄漏到其他支付或语言测试。
  useAuthStore.getState().auth.reset('complete') // 用公开重置流程撤销场景身份。
})
