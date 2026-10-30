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

import { SubscriptionPurchaseDialog } from '@/features/subscriptions/components/dialogs/subscription-purchase-dialog'
import { subscriptionPlanSchema } from '@/features/subscriptions/types'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { BillingHistoryDialog } from '../components/dialogs/billing-history-dialog'
import { SubscriptionPlansCard } from '../components/subscription-plans-card'
import { usePayment } from '../hooks/use-payment'

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
              billing_preference: 'subscription_first',
              subscriptions: active ? [{ subscription }] : [], // 活跃列表与历史列表职责分开。
              all_subscriptions: [{ subscription }], // 历史记录不可因到期从限购计数里消失。
            },
      },
    }))
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <SubscriptionPlansCard topupInfo={null} />
      </QueryClientProvider>
    )
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
