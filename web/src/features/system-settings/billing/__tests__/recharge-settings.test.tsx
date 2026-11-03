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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { RechargeSettingsSection } from '../recharge-settings-section'

// 只替换 HTTP 请求，保留正式路由上下文、设置表单与保存按钮。
// 页面保存区通过 portal 挂载，不能直接调用内部提交函数。
// 密钥就绪仅是布尔值，测试不接收或保存真实密钥。
// 各用例拥有独立缓存，失败时不能继承另一次成功响应。
const clients: QueryClient[] = [] // 统一清理每次挂载创建的安全摘要缓存。
async function mount(
  props = {
    enabled: false,
    encryptionReady: true,
    paymentReady: true,
    wechatReady: true,
  }
) {
  function Fixture() {
    const [container, setContainer] = useState<HTMLDivElement | null>(null)
    return (
      <>
        <div ref={setContainer} />
        <SettingsPageProvider actionsContainer={container}>
          <RechargeSettingsSection {...props} />
        </SettingsPageProvider>
      </>
    )
  }
  const client = new QueryClient({ // 禁用自动重试，使首次失败可被用户观察并手动恢复。
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client) // 异步查询结束后也不会遗留到其他用例。
  const router = createRouter({
    routeTree: createRootRoute({ component: Fixture }), // 保留 Link 所需路由上下文，不替换产品导航。
    history: createMemoryHistory({ initialEntries: ['/'] }), // 测试不导航到用户实际运行的本地服务。
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return screen.findByRole('switch', { name: 'Enable account top-ups' })
}

// 卸载先于缓存清理，防止后台请求把测试数据写入下一用例。
// 恢复 HTTP 边界，避免失败响应污染其他设置测试。
afterEach(() => {
  cleanup() // 先拆除保存按钮 portal，再清理查询资源。
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks() // 还原请求边界，防止假响应流入其他功能测试。
})

// 对保存响应的 success=false 使用正式错误路径，不能只测试 HTTP 状态码。
// 第一次响应模拟配置在页面打开后失效，覆盖后端再次校验的场景。
// 第二次使用同一个表单重试，确认失败没有丢掉用户的操作意图。
// 保存按钮的可用状态是对用户的承诺，不能只看请求是否发出。
// 不直接断言 mutation 内部状态，避免测试绑定查询库实现细节。
// 不依赖 toast 动画的时间，失败是否可恢复通过真实按钮判断。
// 选择器使用可访问名称，同时保护标签与开关的关联。
// 开关操作使用用户事件，不能调用表单 setValue 绕过产品逻辑。
// 请求参数只包含接单配置，没有密钥、商户信息或就绪标记。
// 成功后的按钮禁用代表草稿与已保存值相同，不意味着全站所有渠道可用。
// 缺失配置回归单独覆盖禁用原因，避免成功路径掩盖管理员无法排障的问题。
// 从关闭到开启必须提交显式 true，不能靠省略字段推定启用。
// 保存只传业务开关，禁止将就绪状态作为可编辑配置发送。
// 请求失败保留草稿及保存入口，用户可按具体错误修复后重试。
// 成功重置脏状态，避免用户误以为还需再提交一次。
// 本用例先失败后成功，覆盖相同表单中的恢复路径。
test('settings enable switch persists only after server success and remains retryable on failure', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValueOnce({
      data: {
        success: false,
        message: 'Recharge encryption is not configured',
      },
    })
    .mockResolvedValue({ data: { success: true } })
  const toggle = await mount() // 等真实路由挂载后操作，避免抢跑加载阶段。
  expect(toggle).not.toBeChecked() // 初始状态须来自服务端参数，不能无条件开启。
  await userEvent.click(toggle)
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'recharge_setting.enabled',
      value: true,
    })
  )
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled()
  )
  expect(toggle).toBeChecked() // 保存失败保留草稿，不把失败回滚伪装成保存成功。
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeDisabled()
  )
})

// 支付配置已就绪而密钥缺失，复现钱包可用但代充值不能接单的差异。
// 微信缺失只提供独立提示，不能把两类配置错误混成同一原因。
// 管理员仍可打开支付配置，禁用范围只限制接单开关。
// 错误说明不展示密钥内容，本例仅检查固定环境变量名。
// 不向线上服务写任何配置，所有请求由本地 HTTP 边界隔离。
// 初始密钥缺失时应明确告知部署变量，不显示空白或统一停用文案。
// 关闭状态不能启用，避免保存之前就宣称业务可购买。
// 普通配置的支付入口仍可访问，管理员能继续补齐渠道设置。
// 就绪状态由 props 模拟网络摘要，组件必须只读展示。
test('missing encryption configuration is visible and prevents enabling', async () => {
  vi.spyOn(api, 'put') // 监视意外写请求，诊断页面加载应完全只读。
  const toggle = await mount({
    enabled: false,
    encryptionReady: false,
    paymentReady: true,
    wechatReady: false,
  })
  expect(toggle).toHaveAttribute('aria-disabled', 'true') // 公共开关是 span，禁用通过 ARIA 表达。
  expect(screen.getByRole('alert')).toHaveTextContent('OAK_RECHARGE_DATA_KEY')
  expect(screen.getByRole('link', { name: 'Payment Gateway' })).toHaveAttribute(
    'href',
    '/system-settings/billing/payment'
  )
  expect(api.put).not.toHaveBeenCalled() // 读取缺失配置不能顺便提交设置。
})

// 此场景先保留已有开启值，再模拟运行环境丢失密钥。
// 管理员应能明确保存暂停，而不是被启用前置检查一起阻断。
// false 必须真实发送，不能只改变当前页面的视觉状态。
// 测试结束清理组件，不让未完成的保存影响其他页面。
// 密钥丢失不能阻碍管理员暂停营业，false 也必须持久化。
// 本例同时验证只读诊断与启用意图不被混为一个状态。
// 不触发账号资料或支付接口，只操作配置边界。
test('an enabled service can be disabled even when encryption is unavailable', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const toggle = await mount({
    enabled: true,
    encryptionReady: false,
    paymentReady: true,
    wechatReady: true,
  })
  expect(toggle).toBeEnabled() // 环境故障不能阻止管理员关闭正在接单的业务。
  await userEvent.click(toggle)
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'recharge_setting.enabled',
      value: false,
    })
  )
})
