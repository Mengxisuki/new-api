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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { HostingWorkspace } from '../index'
import type { HostingAccount } from '../types'

// 从真实页面入口验证功能归属，而不是直接调用查询包装函数。
// 网络是唯一隔离点，路由、查询缓存、列表和共享控件均保留真实实现。
// 账号只包含公开摘要，不需要构造任何原厂凭据或管理员密钥。
// 使用待验证状态，确保普通列表读取不被误解成真实模型测试。
// 非空列表让服务不可用时仍能管理已有账号这一边界可被观察。
// 固定脱敏身份属于测试数据，不联系外部邮箱或上游服务。
// 账号备注仅用于定位公开内容，不充当资源授权依据。
// 此摘要刻意不提供 observation，页面应允许尚未产生额度快照的账号。
// verified_at=0 是服务端合同中的未验证值，不能格式化成成功时间。
// paused=false 只代表没有主动暂停，不代表账号已经可以调用模型。
// last_error 留空，确保服务配置错误由独立状态区域解释。
// 所有者检查属于 HTTP 后端，本测试只传已筛选的本人账号列表。
const account: HostingAccount = {
  id: 1,
  provider: 'codex',
  alias: 'Work account',
  email_mask: 'w***@example.test',
  status: 'connected',
  paused: false,
  verified_at: 0,
  last_error: '',
}

// 每个场景使用独立查询缓存，避免上次成功掩盖未配置状态。
// 关闭测试中的自动重试，使恢复必须由可见的重试按钮触发。
// 无限新鲜度只避免无关后台刷新，显式 refetch 仍执行真实请求。
// mutation 同样禁止自动重放，以便发现页面意外创建授权的行为。
// 清理缓存与全局组件清理配合，避免不同场景保留账号摘要。
let client: QueryClient
// 重试次数通过实际调用计数断言，不从 Query 私有状态读取。
// 没有启用持久化插件，测试不需要模拟本地数据库或浏览器存储。
// 页面查询失败是否显示错误由真实 useQuery 状态驱动。
// 这个装配不安装全局 toast，以避免把提示出现误当作页面恢复。
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  })
})
afterEach(() => client.clear())

// 挂靠页真实使用 Link 和 useNavigate，不能把 Router 替换成空实现。
// 内存历史只隔离浏览器导航，不改变页面与路由的实际契约。
// 根路由直接承载被测工作区，认证本身由独立路由测试负责。
// 本文件不声称覆盖生产登录或后端账号归属检查。
// 工作区只需要一个有效的路由上下文，无需挂载整个站点导航。
// 默认历史起点不携带账号编号，排除地址参数控制选中资源的影响。
// 渲染后所有数据仍从页面自己的查询函数取得，没有预填成功缓存。
function renderWorkspace() {
  const router = createRouter({
    routeTree: createRootRoute({ component: HostingWorkspace }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

// 就绪判断与页面职责分别测试，失败时能定位到产品边界。
// 所有等待都观察可访问元素或请求结果，不依赖固定动画时长。
// HTTP spy 会拒绝未声明请求，防止测试偷偷连接真实服务。
describe('挂靠工作区', () => {
  it('服务未配置时说明原因并禁用连接，重试恢复后保留账号管理', async () => {
    const user = userEvent.setup()
    // 第一次返回明确配置错误，第二次才表示管理员已修好环境。
    // 不需要在浏览器填写内部地址或敏感配置才能恢复入口。
    // 错误码使用持久加密配置缺失，符合当前服务状态接口。
    // 不再使用旧管理密钥缺失码，避免把已移除的部署条件固定进测试。
    // 恢复响应沿用同一成功信封，只有业务可用性发生变化。
    // 计数器属于外部响应剧本，不驱动组件按钮或修改缓存。
    let statusReads = 0
    const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/hosting/accounts') {
        return { data: { success: true, data: { items: [account] } } }
      }
      if (url === '/api/hosting/status') {
        statusReads += 1
        return {
          data: {
            success: true,
            data: {
              configured: statusReads > 1,
              code: statusReads > 1 ? '' : 'hosting_encryption_not_configured',
            },
          },
        }
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    const post = vi
      .spyOn(api, 'post')
      .mockRejectedValue(new Error('Unexpected mutation'))
    renderWorkspace()
    // 错误说明必须来自服务状态，不能用空账号列表冒充不可用原因。
    // 连接入口关闭之前不触发敏感验证或原厂授权。
    expect(
      await screen.findByText(
        'Account hosting encryption is not configured. Please contact an administrator.'
      )
    ).toBeVisible()
    const connect = screen.getByRole('button', {
      name: 'Connect Codex account',
    })
    // 禁用断言针对真实按钮属性，不能只检查提示文字存在。
    // 若代码误把 configured 对象本身当作真值，这条断言会失败。
    expect(connect).toBeDisabled()
    // 桌面表格和移动卡片在 jsdom 中同时存在，不把重复摘要当作错误。
    // 仍能看到已有账号，证明不可用状态没有覆盖整个工作区。
    expect(await screen.findAllByText(account.alias)).not.toHaveLength(0)
    expect(
      screen.getAllByRole('button', { name: 'Manage account' })
    ).not.toHaveLength(0)
    await user.click(
      screen.getByRole('button', { name: 'Retry availability check' })
    )
    // 恢复后同一页面立即开放连接，不要求重新登录或刷新浏览器。
    // 查询恢复只改变按钮可用性，不应擅自开始授权流程。
    // 按钮引用仍是原页面节点，恢复不是换挂一个假成功组件。
    // 单次显式重试必须足够，不通过额外点击补偿丢失的状态更新。
    await waitFor(() => expect(connect).toBeEnabled())
    expect(
      screen.queryByText(
        'Account hosting encryption is not configured. Please contact an administrator.'
      )
    ).not.toBeInTheDocument()
    expect(
      get.mock.calls.filter(([url]) => url === '/api/hosting/status')
    ).toHaveLength(2)
    expect(post).not.toHaveBeenCalled()
  })

  // 本场景先满足就绪与列表条件，避免错误页隐藏旧控件导致假通过。
  // 返回一个真实账号也避免只验证到初次接入空状态分支。
  // 禁止全局策略的约束来自新的信息归属，不只是减少一次请求。
  // 排序和导出从具体 Key 进入，账号页仍保留可发现的导航入口。
  it('只管理本人账号并提供 Key 管理入口，不再读取全局顺序或展示配置导出', async () => {
    const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/hosting/accounts') {
        return { data: { success: true, data: { items: [account] } } }
      }
      if (url === '/api/hosting/status') {
        return { data: { success: true, data: { configured: true, code: '' } } }
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    const post = vi
      .spyOn(api, 'post')
      .mockRejectedValue(new Error('Unexpected mutation'))
    renderWorkspace()
    await screen.findAllByText(account.alias)
    // 等待就绪查询完成后再检查请求集合，避免提前断言漏掉子组件请求。
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Connect Codex account' })
      ).toBeEnabled()
    )
    // 共享 Button 使用 Link 渲染，真实导航目标比假定元素 role 更稳定。
    // 这里保留用户到具体 Key 选择来源的路径，不在账号页重复管理。
    expect(screen.getByText('Manage API keys').closest('a')).toHaveAttribute(
      'href',
      '/keys'
    )
    // 这几个入口分别保护来源排序、配置导出和模型选择的页面归属。
    // 账号列表不应为了展示页面而请求所有 Key 或所有账号的模型。
    // 同时检查文案和带标签的控件，防止仅改标题就遗留可操作区域。
    // 没有对整页做 DOM 快照，允许布局、列宽和视觉样式正常调整。
    // 这条职责约束不禁止用户主动打开账号详情查看模型。
    expect(screen.queryByText('Usage order')).not.toBeInTheDocument()
    expect(
      screen.queryByText('Copy Codex configuration')
    ).not.toBeInTheDocument()
    expect(
      screen.queryByLabelText('Verified Codex model')
    ).not.toBeInTheDocument()
    // 明确允许的两个读接口也使旧全局策略请求重新出现时测试失败。
    // 读取账号详情与额度属于用户后续动作，本场景没有打开详情。
    // 请求顺序由 React 调度决定，因此排序后比较集合。
    // 集合仍保留重复项，意外重复加载不会被 Set 去重掩盖。
    // 这里不依赖 Axios 的内部拦截器或去重实现细节。
    // 没有模拟全局策略响应，误恢复该查询会出现明确失败。
    // 无 POST 也证明浏览列表不会暗中测试模型或开始授权。
    expect(get.mock.calls.map(([url]) => url).sort()).toEqual([
      '/api/hosting/accounts',
      '/api/hosting/status',
    ])
    expect(post).not.toHaveBeenCalled()
  })
})
