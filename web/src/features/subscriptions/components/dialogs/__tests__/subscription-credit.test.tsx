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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { PlanRecord } from '../../../types'
import { SubscriptionPurchaseDialog } from '../subscription-purchase-dialog'
import { UserSubscriptionsDialog } from '../user-subscriptions-dialog'

// 真实订阅弹窗负责输入校验和按钮状态，HTTP 是唯一替换边界。
const plan: PlanRecord = {
  plan: {
    id: 7, // 自助和后台开通必须提交同一已存在套餐编号。
    title: 'Credit plan', // 真实选择器用标题定位，不模拟内部选中状态。
    price_amount: 12.5, // 应收以套餐价格计算，不能使用额度面值。
    currency: 'CNY', // 本地挂帐按人民币计债，与美元展示无关。
    duration_unit: 'month', // 一个自然月的权益，不经钱包先充值再兑换。
    duration_value: 1,
    quota_reset_period: 'never', // 本场景不混入配额重置操作。
    quota_reset_custom_seconds: 0,
    enabled: true, // 套餐开放，额度不足用例不会因下架而假通过。
    sort_order: 0,
    allow_wallet_overflow: true,
    max_purchase_per_user: 0, // 不限购，只测试授信可用性这一门禁。
    total_amount: 100, // 与售价故意不同，保护权益和应收分离。
  },
}
// 授信摘要只提供开关和剩余分；套餐应收仍由 price_amount 冻结计算。
// 测试故意关闭 wallet top-up，确认订阅模块开关独立生效。
const credit = {
  enabled: true, // 账户总授权不能被单个模块授权替代。
  limit_cents: 5000, // 五十元总上限并非本次剩余可用数。
  used_cents: 1000, // 已有十元债务，当前可用四十元。
  topup_enabled: false, // 禁用钱包不能阻断独立订阅挂帐。
  subscription_enabled: true, // 明确开放本次购买所属模块。
  recharge_enabled: false, // 不依赖人工代充值模块授权。
}

const clients: QueryClient[] = [] // 场景结束后统一释放缓存，避免成功结果跨用例泄漏。
// 沿用真实查询上下文；不开票时资料查询禁用，但仍需 Provider。
function mount(element: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } }, // 失败直接暴露，不能被自动重试掩盖。
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>{element}</QueryClientProvider>
  )
}

afterEach(() => {
  cleanup() // 先释放真实弹窗的焦点锁和订阅。
  clients.splice(0).forEach((client) => client.clear()) // 再丢弃本人数据，避免下一用例共享身份结果。
  vi.restoreAllMocks() // 网络桩不能影响后续钱包或发票测试。
})

// 未传任何微信或国际渠道开关：这是纯授信场景，不依赖现金付款桩。
// 返回成功表示权益事务已经完成，与打开支付页有不同语义。
// 本场景只验证首次请求字段，不宣称已经测试超时重试的幂等性。
test('纯授信订阅使用专用接口并发送确认金额与请求键', async () => {
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: {
        trade_no: 'credit-sub-1',
        status: 'success',
        payment_kind: 'credit',
      },
    },
  })
  const onPurchaseSuccess = vi.fn()
  mount(
    <SubscriptionPurchaseDialog
      open
      onOpenChange={vi.fn()}
      plan={plan}
      credit={credit}
      onPurchaseSuccess={onPurchaseSuccess}
    />
  )

  await user.click(screen.getByRole('button', { name: 'Pay on account' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/subscription/credit/pay',
      expect.objectContaining({
        plan_id: 7, // 不允许用充值数量代替订阅套餐。
        invoice_required: false, // 挂帐尚无现金实收，明确不开票。
        quoted_cents: 1250, // 十二元五角只转换一次，不乘钱包汇率。
        request_id: expect.any(String), // 请求必须携带服务端去重身份。
      }),
      expect.objectContaining({ singleUseAuthorization: true }) // 认证恢复不可自动重放未知资金结果。
    )
  )
  expect(post.mock.calls[0]?.[0]).not.toBe('/api/subscription/official/pay') // 无二维码、无表单重定向。
  expect(onPurchaseSuccess).toHaveBeenCalledOnce() // 已履约后通知上层刷新本人订阅。
})

// 不足时只展示原因，不把按钮回退到微信或钱包表单。
test('订阅剩余额度不足时禁用挂帐入口并显示原因', () => {
  mount(
    <SubscriptionPurchaseDialog
      open
      onOpenChange={vi.fn()}
      plan={plan}
      credit={{ ...credit, used_cents: 4950 }} // 只剩五角，不够购买十二元五角套餐。
    />
  )

  const button = screen.getByRole('button', { name: 'Pay on account' })
  expect(button).toBeDisabled() // 不允许提交已知额度不足的请求。
  // 原因必须可见，不能只有不可点击且无法解释的按钮。
  expect(
    screen.getByText('Insufficient remaining credit for this subscription.')
  ).toBeVisible()
})

// 后台现代 UI 只有一个开通按钮，分类、凭证和说明在同一请求内提交。
// 这条路径与旧的 bind 入口共享后端 ManualFinanceInput 合同。
test('后台订阅开通提交分类、线下收款凭证和说明', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/subscription/admin/plans') {
      return { data: { success: true, data: [plan] } } // 真实候选列表必须从管理 API 取得。
    }
    return { data: { success: true, data: [] } } // 没有旧订阅，开通结果不能借旧数据通过。
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { message: 'created' } },
  })
  mount(
    <UserSubscriptionsDialog
      open
      onOpenChange={vi.fn()}
      user={{ id: 8, username: 'credit-user' }}
    />
  )

  await user.click(
    await screen.findByPlaceholderText('Select subscription plan')
  )
  // Combobox 的候选项由真实组件渲染，选择后再填写四分类财务字段。
  await user.click(await screen.findByText('Credit plan ($12.50)'))
  await user.selectOptions(
    screen.getByLabelText('Adjustment category'),
    'offline_cash'
  )
  await user.type(screen.getByLabelText(/Note/), 'cash subscription') // 人工开通需要可追溯的说明。
  await user.type(screen.getByLabelText(/Cash received \(CNY\)/), '12.50') // 表单收元，接口传分。
  await user.type(screen.getByLabelText(/Cash reference/), 'wx-cash-1') // 虚构凭证只供测试对账。
  await user.click(screen.getByRole('button', { name: 'Add subscription' }))

  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/subscription/admin/users/8/subscriptions',
      expect.objectContaining({
        plan_id: 7, // 所选套餐通过原管理路径发放。
        reason: 'offline_cash', // 与赠送和补偿分开记实际收款。
        cash_cents: 1250, // 应与已填写的十二元五角收款一致。
        receivable_cents: 0, // 现金收款不能同时生成一笔挂帐。
        cash_ref: 'wx-cash-1', // 凭证随财务分类一起提交，不靠事后补写。
        note: 'cash subscription', // 说明不丢失在服务端消息处理分支。
        request_id: expect.any(String), // 后台开通同样需要幂等身份。
      })
    )
  )
})
