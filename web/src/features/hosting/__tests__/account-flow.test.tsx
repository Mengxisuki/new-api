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
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { HostingAccountDetails } from '../components/account-details'
import { ConnectHostingDialog } from '../components/connect-dialog'
import type { HostingAccount } from '../types'

// HTTP 是唯一隔离边界，实际站点验证 hook 会执行 session 证明并传递上下文。
// 账号 fixture 仅包含公开摘要，任何测试都不需要原厂凭据。
// connected 状态刻意不冒充 active，以免把授权成功等同于模型实测通过。
// 脱敏测试邮箱不会指向真实用户。
// 账号编号一贯穿详情和安全证明，保护动作绑定目标资源。
// 备注 Work 用于可见弹窗标题，不参与身份验证。
// verified_at 为零符合尚未测试的真实流程。
// 最后错误为空表示当前没有已知账户错误，不模拟隐式错误回退。
const account: HostingAccount = {
  id: 1,
  provider: 'codex',
  alias: 'Work',
  email_mask: 'w***@example.test',
  status: 'connected',
  paused: false,
  verified_at: 0,
  last_error: '',
}
// 授权状态使用执行层真实的 pending_authorization 字面值。
// 不能用看似合理的 authorizing 替代，否则实际页面可能禁止用户提交。
// 官方域名 fixture 经过真实 URL 白名单逻辑。
// 回调地址使用服务端约定的本机端口和路径。
// 固定有效期只用于 DTO 解析，不通过真实时间等待触发状态变更。
// 授权 ID 是测试业务引用，不是真实 OAuth code 或 state。
// account_id 为零表示首次连接，与已有账号重授权区分。
// fixture 只提供一次会话，测试应证明不会为了继续而创建第二份。
const authorization = {
  id: 'a-flow',
  account_id: 0,
  status: 'pending_authorization',
  authorization_url: 'https://auth.openai.com/oauth/authorize?state=example',
  redirect_uri: 'http://localhost:1455/auth/callback',
  expires_at: 2000000000,
}
// 明确返回两次机会，确保消费入口因业务条件可用。
// 主额度和卡子子查询都标为新鲜，排除无关禁用原因。
// 未知结果之后即使仍有两次机会，也必须禁止新消费。
// 这个设定保护互斥门槛来自未决历史，而不是恰好次数变为零。
// 不配置具体窗口百分比，因为该场景只关注消费生命周期。
// 重置是否恢复所有窗口属于上游结果，不在前端测试中编造。
const observation = {
  state: 'fresh',
  snapshot: {
    reset_credits_available: 2,
    reset_credit_observation: { state: 'fresh' },
  },
}
// 使用真实 useSecureVerification 的 session 方式完成站点证明。
// 只 mock 其 HTTP 响应，不能直接把 requestVerification 替换成成功函数。
// 这样动作 scope 与 context 会经过真实组装和发送路径。
// 方法列表不包含原厂密码，避免混淆站点验证和 OAuth 登录。
// 测试不会尝试真实安全硬件或外部身份提供方。
// 证明有效期使用固定测试数据，不依赖当前机器时间。
const requirements = {
  scope: 'hosting.manage',
  methods: [{ method: 'session', available: true }],
  oauth_providers: [],
  password_encryption_enabled: false,
}
// 此文件验证账号操作生命周期，不承担页面列表布局测试。
// 两个业务流程均从真实组件入口发起，避免只测 API 包装函数。
// OAuth 与重置共享安全证明服务，但它们的动作上下文不能互换。
// 网络响应只在 HTTP 边界控制，不模拟组件自身的成功状态。
// 没有连接真实原厂账户，测试通过不能被描述成真实 OAuth 验收。
// 原厂回调的严格 state 校验属于服务端，其规则不能在测试中重新实现。
// 原厂消费幂等由执行层和后端共同保证，本测试保护前端不制造新操作。
// UI 单次请求门槛不能替代服务端并发锁，两个层次需分别测试。
// 各异步阶段等待可观察状态，避免测试机器快慢决定结果。
// 所有响应都有最小合法字段，DTO 校验失败会让组件流程实际失败。
// 清空回调输入是用户隐私要求，不只是视觉优化。
// 浏览器刷新后的恢复来自后台历史，不把秘密放进测试用 localStorage。
// 测试中的证明字符串仅为网络 fixture，不是可复用授权凭据。
// 用户关闭动作的 spy 只观察公开回调，不干预业务执行。
let client: QueryClient
// 每个场景独立创建缓存，授权状态不会从上一次测试残留。
// 关闭请求重试，防止一次性授权因测试框架自动重放。
// 查询无限新鲜度使状态推进由测试显式控制，消除网络时序偶然性。
// 状态变化仍通过真实 Query invalidation 进入组件。
// 清理缓存会销毁临时授权引用，不在 localStorage 保存跨场景状态。
// 公共测试清理恢复 HTTP spy，场景没有共享传输结果。
// 不用固定 sleep 等待模态框动画，以可访问 DOM 状态作为同步点。
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  })
})
afterEach(() => {
  client.clear()
})

describe('账号授权和原厂重置', () => {
  // 此场景保护最危险的阶段边界：已提交 code 但还未取得凭据。
  // HTTP 回调返回 exchanging，故意不立即返回 ready。
  // 页面不能因此保存账号或关闭弹窗提示成功。
  // 首先验证回调输入清空，确保秘密不因异步等待留在输入框。
  // 然后推进服务端状态为 ready，模拟后台换码完成。
  // 推进只改外部状态，通过真实查询重新渲染，不直接操作组件 state。
  // 第二次点击完成只允许创建账号，不能再次提交已消费回调。
  // 保存请求只携带授权引用与备注，不能包含原厂 code。
  // 检查回调请求数量是一次性授权契约，不是组件私有调用细节。
  // 关闭回调代表用户看到连接流程完成，仅在保存响应后发生。
  // 模型实际可调用尚未在此证明，后续账号测试仍是独立动作。
  // 测试内容中的 private-code 是固定无效字符串，不来自真实登录。
  it('换码仍在处理时等待 ready，保存时不再次发送已消费的回调', async () => {
    const user = userEvent.setup()
    const close = vi.fn()
    // 服务端状态是可控网络边界，允许精确安排交换前后的响应。
    // GET 安全验证方法与 GET 授权状态使用不同响应，避免泛化 mock 误通过。
    // POST 证明、创建授权、提交回调和保存账号各有真实 DTO。
    // 没有把页面子组件 mock 掉，确认表单与缓存接线一并执行。
    // 对 HTTP 响应保留 success/data 信封，让入口校验真正参与测试。
    // 固定响应使测试结果不依赖原厂网络或账户额度。
    let status = authorization.status
    vi.spyOn(api, 'get').mockImplementation(async (url) => ({
      data: {
        success: true,
        data:
          url === '/api/verify/methods'
            ? requirements
            : { ...authorization, status },
      },
    }))
    const post = vi.spyOn(api, 'post').mockImplementation(async (url) => {
      if (url === '/api/verify') {
        return {
          data: {
            success: true,
            data: {
              proof_token: 'test-proof',
              scope: 'hosting.manage',
              method: 'session',
              expires_at: 2000000000,
            },
          },
        }
      }
      if (url === '/api/hosting/authorizations') {
        return { data: { success: true, data: authorization } }
      }
      if (url.endsWith('/callback')) {
        return {
          data: {
            success: true,
            data: { ...authorization, status: 'exchanging' },
          },
        }
      }
      return { data: { success: true, data: account } }
    })
    render(
      <QueryClientProvider client={client}>
        <ConnectHostingDialog onClose={close} />
      </QueryClientProvider>
    )
    await user.click(
      screen.getByRole('button', { name: 'Start browser authorization' })
    )
    await user.type(await screen.findByLabelText('Account alias'), 'Work')
    await user.type(
      screen.getByLabelText('Authorization callback URL'),
      'http://localhost:1455/auth/callback?state=example&code=private-code'
    )
    await user.click(screen.getByRole('button', { name: 'Finish connecting' }))
    await waitFor(() =>
      expect(screen.getByLabelText('Authorization callback URL')).toHaveValue(
        ''
      )
    )
    expect(close).not.toHaveBeenCalled()
    expect(
      post.mock.calls.filter(([url]) => url === '/api/hosting/accounts')
    ).toHaveLength(0)
    // 此时应仍停留在同一弹窗和同一授权会话。
    // 只读轮询能发现换码完成，不能隐式重新发送 code。
    // 等待回调输入消失，是 ready 状态已被界面接收的可见证据。
    // 再提交备注会走保存阶段，不依赖测试直接调用 API。
    // 若实现错误复用旧回调，这里的请求数量断言会失败。
    // 不使用定时器快进替代真实 Query 更新，避免掩盖缓存键错误。
    status = 'ready'
    await act(async () => {
      await client.invalidateQueries({ queryKey: ['hosting', 'authorization'] })
    })
    await waitFor(() =>
      expect(
        screen.queryByLabelText('Authorization callback URL')
      ).not.toBeInTheDocument()
    )
    await user.click(screen.getByRole('button', { name: 'Finish connecting' }))
    await waitFor(() => expect(close).toHaveBeenCalledOnce())
    expect(
      post.mock.calls.filter(([url]) => url.endsWith('/callback'))
    ).toHaveLength(1)
    expect(post).toHaveBeenCalledWith('/api/hosting/accounts', {
      authorization_id: 'a-flow',
      alias: 'Work',
    })
  })
  // 第二场景保护外部不可撤销消费的确认与恢复边界。
  // 打开详情已有新鲜卡数，但未点击确认前不允许发送 consume。
  // 先执行额度刷新，证明刷新按钮不会偷偷消费权益。
  // 确认后模拟上游结果未知，而不是网络明确未发送。
  // 即使观测仍显示可用次数，历史中的未决记录必须关闭新消费按钮。
  // 用户可刷新原操作结果，验证这个动作只走 GET。
  // 只读刷新前后 consume 数量保持一，防止刷新等价于再次扣卡。
  // 安全证明的 context 必须包含当前账号和 reset 动作。
  // 测试不假设原厂真的未扣卡，未知结果是需要保留的业务状态。
  // 返回的 result 为 null，保护空结果不能被当作没有操作。
  // 操作 ID 从真实请求体取出，避免 mock 另造身份掩盖 UUID 错误。
  // 后台历史随后返回同一记录，模拟刷新页面也可恢复的持久契约。
  it('确认后才消费且未知结果阻止新消费，刷新额度不会调用消费接口', async () => {
    const user = userEvent.setup()
    // 操作历史属于网络边界的持久状态模拟，不是组件内部状态。
    // 首次读取为空，消费后才加入未决记录。
    // 预置观察和模型缓存使按钮可用性只取决于消费历史。
    // 模型目录为空不影响重置卡操作，避免引入无关模型验证步骤。
    // 刷新观察始终返回同一新鲜快照，强调未知结果优先于剩余次数。
    // GET 单次操作与 GET 历史列表分别满足实际接口形状。
    // 不把 POST 消费响应直接注入组件 state，而由真实 invalidation 重新读取。
    let operations: object[] = []
    client.setQueryData(['hosting', 'observation', 1], observation)
    client.setQueryData(['hosting', 'models', 1], { models: [] })
    client.setQueryData(['hosting', 'operations', 1], { items: [] })
    const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
      let data: unknown = { items: operations }
      if (url === '/api/verify/methods') data = requirements
      if (url.includes('/reset-operations/')) data = operations[0]
      if (url.endsWith('/observation')) data = observation
      if (url.endsWith('/models')) data = { models: [] }
      return { data: { success: true, data } }
    })
    const post = vi.spyOn(api, 'post').mockImplementation(async (url, body) => {
      if (url === '/api/verify') {
        return {
          data: {
            success: true,
            data: {
              proof_token: 'test-proof',
              scope: 'hosting.manage',
              method: 'session',
              expires_at: 2000000000,
            },
          },
        }
      }
      if (url.endsWith('/refresh-observation')) {
        return { data: { success: true, data: observation } }
      }
      const operation = {
        operation_id: (body as { operation_id: string }).operation_id,
        account_id: 1,
        status: 'outcome_unknown',
        result: null,
      }
      operations = [operation]
      return { data: { success: true, data: operation } }
    })
    render(
      <QueryClientProvider client={client}>
        <HostingAccountDetails
          account={account}
          onClose={vi.fn()}
          onReauthorize={vi.fn()}
        />
      </QueryClientProvider>
    )
    // 从用户可见按钮发起动作，包含真实确认框和站点验证流程。
    // 首次点击重置只打开确认框，HTTP consume 应仍为零。
    // 确认 Continue 使用精确名称，避免误点继续未决操作按钮。
    // 未决提示出现是历史查询已完成的同步点。
    // 随后检查禁用状态，保护键盘用户也不能触发新的消费。
    // 只读刷新按钮应仍可用，用户不会被未知结果永久困在死胡同。
    // 请求路径证明读取的是原操作，而非新建一次恢复消费。
    // 最终证明上下文断言防止误把连接或删除证明用于重置。
    // 测试不检查 toast 或 DOM 层级，避免视觉调整造成无关失败。
    await user.click(screen.getByRole('button', { name: 'Refresh quota' }))
    await user.click(
      screen.getByRole('button', { name: 'Use one reset opportunity' })
    )
    expect(
      post.mock.calls.filter(([url]) => url.endsWith('/consume'))
    ).toHaveLength(0)
    await user.click(screen.getByRole('button', { name: /^Continue$/ }))
    await screen.findByText(
      'The previous reset result is not confirmed. Do not start another reset.'
    )
    expect(
      screen.getByRole('button', { name: 'Use one reset opportunity' })
    ).toBeDisabled()
    expect(
      post.mock.calls.filter(([url]) => url.endsWith('/consume'))
    ).toHaveLength(1)
    await user.click(
      screen.getByRole('button', { name: 'Refresh reset result' })
    )
    await waitFor(() =>
      expect(
        get.mock.calls.some(([url]) => url.includes('/reset-operations/'))
      ).toBe(true)
    )
    expect(
      post.mock.calls.filter(([url]) => url.endsWith('/consume'))
    ).toHaveLength(1)
    expect(post).toHaveBeenCalledWith(
      '/api/verify',
      expect.objectContaining({
        context: expect.objectContaining({ account_id: 1, action: 'reset' }),
      }),
      expect.any(Object)
    )
  })
})
