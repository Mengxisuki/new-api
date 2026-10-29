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
import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { ThemeProvider } from '@/context/theme-provider'
import zh from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'
import { STATUS_QUERY_KEY } from '@/lib/status-query'
import { useAuthStore } from '@/stores/auth-store'

import { Home } from '../index'

// 从真实首页入口渲染，防止新官网只有组件而没有被路由实际使用。
// 公共页头、页脚和默认页面保持真实实现，避免隐藏布局集成错误。
// 每个场景独立缓存，不依赖真实后台或其他测试的残留状态。
// 首页集成测试职责：
// 恢复历史页面需要同时恢复默认首页入口和相关依赖。
// 单独渲染 OakLanding 无法发现 Home 仍在展示上游默认页面。
// 因此这里从 Home 入口进入，并保留真实默认内容加载流程。
// 公开状态与通知不属于本次变更，用明确的新鲜缓存隔离网络。
// 首页内容接口返回空值，代表管理员未启用自定义内容。
// 这是默认官网应出现的明确触发条件。
// 没有模拟主题 Hook，也没有替换公共布局或页眉。
// 如果新组件需要额外 Provider，真实组合会直接暴露缺失依赖。
// 内存路由提供 Link 所需上下文，避免浏览器跳转污染测试环境。
// 测试只读取链接目标，不执行注册或控制台的认证流程。
// 匿名用户的开始入口应前往注册页面。
// 已登录用户应前往控制台，不能因为恢复旧布局倒退此约定。
// 测试登录态直接使用项目公开 store API，不伪造组件状态。
// 普通用户角色避免无关的管理员升级检查。
// 首屏与收尾是两个独立行动入口，均需保护跳转目标。
// 不会要求整份页面 DOM 与历史版本逐字节一致。
// 场景标签页必须通过可访问角色定位，不能依赖 CSS 类名。
// 切换后检查公开选中状态和可见标题，而非内部 state。
// 场景示例的完整长段文案不作为无关断言。
// 代码语言切换来自真实 Tabs 交互。
// 只有当前语言的代码应当被复制到系统剪贴板。
// 使用当前站点 origin 验证地址，避免恢复旧站点硬编码。
// 模型示例只用于说明，不应自动发起昂贵的模型请求。
// 因此 HTTP POST 作为不可触发的外部边界被观察。
// 剪贴板由 userEvent 提供可控边界，项目 CopyButton 仍走真实实现。
// 复制断言只比较当前可见内容，不在测试中重新生成示例字符串。
// 这可避免生产和测试各复制一份业务规则而同时出错。
// 所有异步步骤等待明确的标题、选中状态或用户交互结果。
// 不使用固定 sleep，也不依赖组件渲染耗时。
// 首页主题由真实 ThemeProvider 管理。
// 清理 document 根主题类，避免影响后续与官网无关的测试。
// 查询缓存和认证状态也必须恢复，不能把已登录用户泄漏给下一用例。
// 这些用例不覆盖注册、身份验证或模型计费实现。
// 这些流程仍由各自模块的专属测试负责。
// 页面视觉、浏览器滚动和响应式截图不由 jsdom 断言替代。
// 新增官网交互时只增加用户可观察的契约，不绑定内部组件拆分。
const clients: QueryClient[] = []

async function renderHome() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  // 已有公开状态作为缓存输入；页面无采购联系人仍须完整显示。
  client.setQueryData(STATUS_QUERY_KEY, { system_name: 'Oak' })
  client.setQueryData(['notice'], { success: true, data: '' })
  const router = createRouter({
    routeTree: createRootRoute({ component: Home }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <RouterProvider router={router} />
      </ThemeProvider>
    </QueryClientProvider>
  )
  // 等待真实内容加载分支结束，不以固定延迟猜测界面何时完成。
  return screen.findByRole('heading', { level: 1 })
}

beforeEach(() => {
  localStorage.clear()
  useAuthStore.getState().auth.reset('complete')
  // 仅替换 HTTP 边界；保留首页加载 Hook 的缓存清理与返回判断。
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data: '' } })
  vi.spyOn(api, 'post').mockResolvedValue({ data: { success: true } })
})

afterEach(async () => {
  // 完整恢复全局状态，保证登录态测试不会影响后续匿名场景。
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset('complete')
  localStorage.clear()
  document.documentElement.classList.remove('light', 'dark')
  await i18next.changeLanguage('en')
})

test('匿名访问默认首页时展示Oak官网且开始入口指向注册', async () => {
  const heading = await renderHome()
  expect(heading).toHaveTextContent('AI infrastructure,')
  // 首屏与收尾两处行动按钮都必须保留正确目标。
  const links = screen.getAllByRole('button', { name: 'Get Started' })
  expect(links).toHaveLength(2)
  for (const link of links) expect(link).toHaveAttribute('href', '/sign-up')
  // 后台自定义首页为空时才展示默认官网；请求仍经过现有入口。
  expect(api.get).toHaveBeenCalledWith(
    '/api/home_page_content',
    expect.any(Object)
  )
})

test('已登录访问首页时行动入口指向控制台', async () => {
  // 普通用户不会触发管理员更新检查，行为聚焦首页入口。
  useAuthStore
    .getState()
    .auth.setUser({ id: 7, username: 'researcher', role: 1 })
  await renderHome()
  const links = screen.getAllByRole('button', { name: 'Go to Dashboard' })
  expect(links).toHaveLength(2)
  for (const link of links) expect(link).toHaveAttribute('href', '/dashboard')
  expect(
    screen.queryByRole('button', { name: 'Get Started' })
  ).not.toBeInTheDocument()
})

// 从真实入口核对服务分组，防止说明组件未接入首页。
// 价格目录与采购说明是现有入口，不应生成尚未实现的购买路由。
// 周额度示例必须保留示例语义，避免被误读为现售套餐价格。
// 外部账户充值与平台余额分开说明，不能导向钱包兑换入口。
test('首页展示四类服务并将咨询入口定位到实际存在的采购说明', async () => {
  await renderHome()
  const services = screen.getByRole('region', {
    name: 'Four ways to access the models you need.',
  })
  expect(within(services).getAllByRole('article')).toHaveLength(4)
  for (const name of [
    'Token usage',
    'Monthly plans',
    'Account hosting',
    'Account top-ups',
  ]) {
    expect(
      within(services).getByRole('heading', { name, level: 3 })
    ).toBeVisible()
  }
  expect(
    within(services).getByRole('link', { name: 'Explore models and pricing' })
  ).toHaveAttribute('href', '/pricing')
  const consultations = within(services).getAllByRole('link', {
    name: /Discuss|Ask about/,
  })
  expect(consultations).toHaveLength(3)
  for (const link of consultations) {
    expect(link).toHaveAttribute('href', '#oak-procurement')
    expect(document.querySelector('#oak-procurement')).toBeVisible()
  }
  expect(services).toHaveTextContent(
    'Example allowance: CNY 150 per week. Actual limits and renewal rules depend on your plan.'
  )
  expect(services).toHaveTextContent(
    'The credit goes to your provider account, separately from your Oak balance. Check the supported service and redemption instructions before buying.'
  )
  // 阅读服务与导航链接不应触发下单或模型请求。
  expect(api.post).not.toHaveBeenCalled()
})

// 加载真实中文词条，覆盖上线后的展示而不是模拟 t() 返回值。
// 语言切换应即时更新服务说明，不能把翻译冻结在模块常量中。
// 示例额度和到账账户是本次说明的关键边界，按可见文案核对。
test('首页切换为中文时展示四类服务并明确额度示例与充值账户', async () => {
  await renderHome()
  i18next.addResourceBundle('zhCN', 'translation', zh.translation)
  await act(async () => {
    await i18next.changeLanguage('zhCN')
  })
  const services = screen.getByRole('region', {
    name: '四种方式，让模型为你所用。',
  })
  // 四种标题属于同一选型章节，不是四档订阅套餐。
  for (const name of ['量贩 Token', '包月订阅', '账号挂靠', '代充值']) {
    expect(
      within(services).getByRole('heading', { name, level: 3 })
    ).toBeVisible()
  }
  expect(services).toHaveTextContent('额度示例：每周 150 元。')
  expect(services).toHaveTextContent('与 Oak 平台余额独立。')
  // 首屏次按钮应能直接定位新增的服务选型区。
  expect(screen.getByRole('button', { name: '了解服务方式' })).toHaveAttribute(
    'href',
    '#oak-services'
  )
})

test('切换科研场景和代码语言后显示对应内容并复制当前示例', async () => {
  // userEvent 提供剪贴板边界，实现仍调用项目统一复制组件。
  const user = userEvent.setup()
  const writeText = vi.spyOn(navigator.clipboard, 'writeText')
  await renderHome()
  await user.click(screen.getByRole('tab', { name: /Research pipelines/ }))
  expect(
    screen.getByRole('tab', { name: /Research pipelines/ })
  ).toHaveAttribute('aria-selected', 'true')
  expect(
    screen.getByRole('heading', {
      name: 'Bring multiple models into the same experiment.',
    })
  ).toBeVisible()
  // 语言切换应只改变示例，不发起真实模型请求。
  await user.click(screen.getByRole('tab', { name: 'cURL' }))
  const code = screen.getByLabelText('API request example')
  expect(code).toHaveTextContent('curl')
  expect(code).toHaveTextContent(
    `${window.location.origin}/v1/chat/completions`
  )
  const integration = screen.getByRole('region', {
    name: 'Connect your existing tools.',
  })
  await user.click(
    within(integration).getByRole('button', { name: 'Copy code' })
  )
  // 复制文本必须等于当前可见代码，不能误复制默认 Python 示例。
  expect(writeText).toHaveBeenCalledWith(code.textContent)
  expect(api.post).not.toHaveBeenCalled()
})
