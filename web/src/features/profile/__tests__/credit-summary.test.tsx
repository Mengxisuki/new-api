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
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { CreditSummaryCard } from '../components/credit-summary-card'

// 读取真实本人API与卡片，不模拟业务组件或复制额度计算。
// 每个场景独立缓存，失败时不继承旧的额度快照。
// 额度展示必须读取本人接口，
// 不能借用管理员用户列表再前端筛选。
// 测试替换HTTP返回值，
// 保留生产数据解析与组件渲染。
// 固定总额和占用选择不同数值，
// 以便直接识别错把总额当可用额的问题。
// 所有金额断言来自明确fixture，
// 测试不复制生产计算函数自证正确。
// 每个场景独立缓存，
// 错误场景不会意外看到旧成功结果。
// 组件卸载后清空查询资源，
// 避免刷新轮询跨测试持续运行。
// 总额和占用即使都为正，也不能推出当前授权一定启用。
// 本测试不替代后端支付时的授信校验。
// 个人资料仅展示，实际支付仍在购买页面发生。
// 错误断言等待真实查询最终失败状态。
// 不使用固定延迟掩盖异步读取未完成。
// 测试结束释放缓存，防止身份数据跨场景残留。
const clients: QueryClient[] = []
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>
      <CreditSummaryCard />
    </QueryClientProvider>
  )
}
afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})

// 正常授信同时提供三个模块的独立授权。
// 总额、占用和可用额应分别可见，
// 且不出现允许用户编辑授信的输入控件。
// 接口请求不能包含目标用户编号，
// 归属必须由服务端登录身份确定。
// 这组断言保护只读展示的权限边界。
test('本人授信展示总额已用与可用金额，并且没有额度编辑入口', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        user_id: 2,
        enabled: true,
        limit_cents: 20000,
        used_cents: 6500,
        topup_enabled: true,
        subscription_enabled: true,
        recharge_enabled: false,
        version: 1,
      },
    },
  })
  mount()
  expect(await screen.findByText('¥135')).toBeVisible()
  expect(screen.getByText('¥200')).toBeVisible()
  expect(screen.getByText('¥65')).toBeVisible()
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  expect(get).toHaveBeenCalledWith('/api/user/credit', {
    disableDuplicate: true,
  })
})

// 服务端null表示尚未配置授信。
// 卡片应提示联系管理员，
// 不能制造一个额度为零的账户对象。
// 没有授信与授信刚好用尽含义不同，
// 用户不能被错误提示为已有可消费资格。
test('未配置授信时提示联系管理员而不是虚构零额度账户', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: null },
  })
  mount()
  expect(
    await screen.findByText(
      'On-account credit is not configured. Contact an administrator.'
    )
  ).toBeVisible()
  expect(screen.queryByText('¥0')).not.toBeInTheDocument()
})

// 网络失败必须表现为读取错误。
// 测试显式拒绝HTTP请求，
// 不能用null模拟故障。
// 错误状态应允许重新读取，
// 也不应显示尚未配置的业务提示。
// 这防止短暂故障被当成管理员撤销授权。
test('读取失败时提供重试且不显示未配置授信', async () => {
  vi.spyOn(api, 'get').mockRejectedValue(new Error('offline'))
  mount()
  expect(await screen.findByRole('button', { name: /Retry/i })).toBeVisible()
  expect(
    screen.queryByText(
      'On-account credit is not configured. Contact an administrator.'
    )
  ).not.toBeInTheDocument()
})
