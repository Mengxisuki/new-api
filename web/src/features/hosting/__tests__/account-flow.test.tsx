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

// 场景覆盖首次录入、不可用服务、异步成功和两种不同失败阶段。
// 原有重置场景继续保留，确保连接流程重做没有影响共享安全证明行为。
// UI中的“完成授权”不等同于模型测试，测试明确禁止隐式调用测试接口。
// 每种恢复方式都绑定原授权，不依赖用户重新提交一次性回调。
// 这些验证只使用测试网络边界，不能代替生产原厂OAuth连通性验收。
// 页面显示的真实错误由统一处理链负责，测试关注可继续操作的行为。
// 复制能力通过共享组件实际执行，避免只断言按钮存在而漏接剪贴板。
// 安全证明依然通过真实hook发起，没有为了简化UI测试而绕过验证。
// 用例不读取任何开发者本地账号或真实环境变量中的凭据。
// 日志和失败断言使用固定测试文案，避免将回调内容打印到输出。
// 成功后的资源配置仍属于父页面导航任务，本测试不修改默认策略。
// 新流程不会把账号接入自动扩展为对任意API Key的调用授权。
describe('账号授权和原厂重置', () => {
  // 父页面探测服务未就绪时传入可读原因，不让用户走到无法完成的OAuth。
  // 输入备注仍可操作，但不会因为输入完整就绕过服务可用性门槛。
  // 观察真实HTTP调用次数，防止禁用样式有了但仍发起授权。
  // 该用例不需要任何成功响应，避免服务不可用被其他fixture意外掩盖。
  // 可读原因来自父页面，组件无需认识部署配置键或内部服务地址。
  // 填入有效备注后按钮仍禁用，说明不是字段验证恰好阻止了请求。
  // 接口spy未被调用包含站点证明入口，不能先打证明再发现服务不可用。
  // 静态错误原因不应改变账号备注的输入值。
  // 这个门槛用于连接和重授权，父页不需要另建两套可用性提示。
  // 不可用时没有授权ID，也就不应存在任何恢复请求。
  it('服务未就绪时解释原因并禁止生成授权链接', async () => {
    const user = userEvent.setup()
    const post = vi.spyOn(api, 'post')
    render(
      <QueryClientProvider client={client}>
        <ConnectHostingDialog
          disabledReason='Hosting service is unavailable'
          onClose={vi.fn()}
        />
      </QueryClientProvider>
    )
    await user.type(screen.getByLabelText('Account alias'), 'Work')
    expect(screen.getByText('Hosting service is unavailable')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Generate authorization link' })
    ).toBeDisabled()
    expect(post).not.toHaveBeenCalled()
  })

  // 此场景保护最危险的阶段边界：已提交 code 但还未取得凭据。
  // HTTP 回调返回 exchanging，故意不立即返回 ready。
  // 页面不能因此保存账号或关闭弹窗提示成功。
  // 首先验证回调输入撤下，确保秘密不因异步等待继续展示。
  // 然后推进服务端状态为 ready，模拟后台换码完成。
  // 推进只改外部状态，通过真实查询重新渲染，不直接操作组件 state。
  // ready 到达后自动延续用户已授权的添加动作，不要求用户再点一次完成。
  // 保存请求只携带授权引用与备注，不能包含原厂 code。
  // 检查回调请求数量是一次性授权契约，不是组件私有调用细节。
  // 添加成功保留下一步引导，只有用户点击完成才关闭弹窗。
  // 模型实际可调用尚未在此证明，后续账号测试仍是独立动作。
  // 测试内容中的 private-code 是固定无效字符串，不来自真实登录。
  it('换码仍在处理时等待 ready，保存时不再次发送已消费的回调', async () => {
    const user = userEvent.setup()
    const close = vi.fn()
    // 导航回调属于父页面职责，这里只观察成功页是否提供明确入口。
    // 前往API Key不会改变这次授权结果，也不要求再次读取原厂身份。
    // Done和导航按钮分别可用，用户可以先返回详情做模型测试。
    // 成功提示明确先测试连接，避免身份授权被误解成模型兼容性已验收。
    // 额外检查没有test请求，保护自动添加流程不产生用户未点击的模型耗额。
    // 等待成功文本后再检查关闭回调，避免异步渲染时序带来误判。
    // 点击Done才退出，用户有机会阅读接入后的两步引导。
    // 公共回调spy不会驱动组件内部状态，因此仍测试真实交互路径。
    const manageKeys = vi.fn()
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
        <ConnectHostingDialog onClose={close} onManageKeys={manageKeys} />
      </QueryClientProvider>
    )
    // 备注先校验，再取得授权链接；首次展示不直接索要回调。
    // 未填写备注时不应调用安全证明或创建授权会话。
    // 第一步点击空备注时应停留在表单，不申请无用OAuth会话。
    // 随后用同一组件填入备注并继续，保护校验错误可以正常恢复。
    // 授权按钮只有在取得有效官方链接后才出现。
    // 检查href内容能防止按钮意外指向站点自身或占位链接。
    // 复制动作走真实CopyButton，剪贴板属于userEvent提供的浏览器边界。
    // 复制成功的内容必须是官方授权链接，不能混入回调地址。
    // 链接打开和复制是两种用户选择，不互相触发额外网络操作。
    await user.click(
      screen.getByRole('button', { name: 'Generate authorization link' })
    )
    expect(post).not.toHaveBeenCalled()
    await user.type(screen.getByLabelText('Account alias'), 'Work')
    await user.click(
      screen.getByRole('button', { name: 'Generate authorization link' })
    )
    expect(
      await screen.findByRole('button', { name: 'Copy authorization link' })
    ).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Open provider authorization' })
    ).toHaveAttribute('href', authorization.authorization_url)
    await user.click(
      screen.getByRole('button', { name: 'Copy authorization link' })
    )
    expect(await navigator.clipboard.readText()).toBe(
      authorization.authorization_url
    )
    await user.type(
      screen.getByLabelText('Authorization callback URL'),
      'http://localhost:1455/auth/callback?state=example&code=private-code'
    )
    await user.click(
      screen.getByRole('button', { name: 'Complete authorization and add' })
    )
    await waitFor(() =>
      expect(
        screen.queryByLabelText('Authorization callback URL')
      ).not.toBeInTheDocument()
    )
    expect(close).not.toHaveBeenCalled()
    expect(
      post.mock.calls.filter(([url]) => url === '/api/hosting/accounts')
    ).toHaveLength(0)
    // 此时应仍停留在同一弹窗和同一授权会话。
    // 只读轮询能发现换码完成，不能隐式重新发送 code。
    // 等待回调输入消失，是 ready 状态已被界面接收的可见证据。
    // 无需再点击提交即可进入保存阶段，避免多余确认割裂完整用户动作。
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
    await screen.findByText('Account added')
    expect(post.mock.calls.some(([url]) => url.endsWith('/test'))).toBe(false)
    expect(close).not.toHaveBeenCalled()
    expect(
      screen.getByText(
        'Test the connection in account details, then select this account in an API key’s resource settings.'
      )
    ).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Go to API keys' }))
    expect(manageKeys).toHaveBeenCalledOnce()
    await user.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(close).toHaveBeenCalledOnce())
    expect(
      post.mock.calls.filter(([url]) => url.endsWith('/callback'))
    ).toHaveLength(1)
    expect(post).toHaveBeenCalledWith('/api/hosting/accounts', {
      authorization_id: 'a-flow',
      alias: 'Work',
    })
  })
  // 保存账号失败和原厂换码失败是不同阶段，不能要求用户重新授权。
  // 此场景让回调直接 ready，以便单独验证保存响应失败后的恢复行为。
  // 首次保存拒绝只代表控制面写入未完成，不应自动申请另一会话。
  // 第二次保存由用户明确点击，同一个 authorization_id 保持幂等身份。
  // 安全证明、OAuth创建和回调请求都不应随着保存重试而重复。
  // 成功页不自动关闭，保留用户需要的后续测试与Key配置提示。
  // 网络错误没有包含回调内容，避免把测试秘密写入控制台。
  // 保存重试场景使用同一个页面实例，保护当前会话上下文能被保留。
  // 首次网络错误刻意发生在账号保存阶段，而非OAuth exchange阶段。
  // 错误类型只表达传输失败，避免测试依赖某个服务端文案。
  // 回调响应先返回ready，因此用户无需再填写授权信息。
  // 页面应给出“重试添加”，而不是回到最初的生成链接按钮。
  // 重试后保存请求参数必须完全沿用原授权引用。
  // 备注仍来自用户在首步骤输入的Work，不能被错误重置为空。
  // 重复保存数量为二是显式重试契约，不允许后台自动多次提交。
  // 测试没有调用mutation私有方法，重试仍通过用户可见按钮。
  // 成功页是添加结果的证据，不能把按钮恢复可点击当成已成功。
  // 没有把错误响应解析成账号摘要，以免假成功覆盖失败状态。
  // 原厂登录会话创建数量保持一，防止重试扩展为全流程重来。
  // 回调POST数量保持一，保护一次性凭证不被保存重试污染。
  // 模拟服务器返回的是同一个业务账号，前端不生成额外账号ID。
  it('保存失败后按原授权重试添加，不重新生成链接或重发回调', async () => {
    const user = userEvent.setup()
    let status = authorization.status
    let saves = 0
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
      if (url.endsWith('/callback')) {
        status = 'ready'
        return { data: { success: true, data: { ...authorization, status } } }
      }
      if (url === '/api/hosting/accounts') {
        saves += 1
        if (saves === 1) throw new Error('Account save failed')
        return { data: { success: true, data: account } }
      }
      return { data: { success: true, data: authorization } }
    })
    render(
      <QueryClientProvider client={client}>
        <ConnectHostingDialog onClose={vi.fn()} />
      </QueryClientProvider>
    )
    await user.type(screen.getByLabelText('Account alias'), 'Work')
    await user.click(
      screen.getByRole('button', { name: 'Generate authorization link' })
    )
    await user.type(
      await screen.findByLabelText('Authorization callback URL'),
      'http://localhost:1455/auth/callback?state=example&code=private-code'
    )
    await user.click(
      screen.getByRole('button', { name: 'Complete authorization and add' })
    )
    await user.click(
      await screen.findByRole('button', { name: 'Retry adding account' })
    )
    await screen.findByText('Account added')
    expect(
      post.mock.calls.filter(([url]) => url === '/api/hosting/authorizations')
    ).toHaveLength(1)
    expect(
      post.mock.calls.filter(([url]) => url.endsWith('/callback'))
    ).toHaveLength(1)
    expect(
      post.mock.calls.filter(([url]) => url === '/api/hosting/accounts')
    ).toHaveLength(2)
    expect(post).toHaveBeenLastCalledWith('/api/hosting/accounts', {
      authorization_id: 'a-flow',
      alias: 'Work',
    })
  })
  // 换码HTTP失败可能只是响应丢失，不证明原厂没有消费code。
  // 本场景保留同一会话，用只读结果查询发现稍后完成的授权。
  // 回调输入发送后撤下，用户不能通过重复粘贴造成第二次交换。
  // 只读查询得到ready后，继续此前明确授权的保存动作链。
  // 检查回调POST总数，保护恢复入口不偷偷重放单次凭证。
  // 不使用固定延时，通过服务器fixture的状态推进控制异步边界。
  // 未确认状态向用户解释检查结果，而不宣称授权失败或已保存。
  // 回调错误只表示浏览器没有取得结果，不表示服务端拒绝该code。
  // 因此fixture仍保留原授权会话，后续GET能发现ready。
  // 读取结果按钮在回调响应失败后出现，用户无需重新开始。
  // 输入框被卸载而不是重新填回秘密，保护共享屏幕的敏感信息。
  // 服务器状态由测试显式推进，不依赖三秒轮询恰好触发。
  // 等待查询按钮可用能排除同时进行的自动错误恢复读取。
  // 点击查询之后的保存属于先前完成按钮批准的动作链。
  // 此时没有再次提交回调的可见入口，也不应产生新的callback请求。
  // 保存仅执行一次，防止用户主动查询与后台轮询分别触发添加。
  // 同一个query key承接原状态，错误恢复不能切换到别人的授权。
  // fixture不模拟原厂明确拒绝，因此不要求用户重新生成链接。
  // 真正失败终态由服务端状态决定，不能用HTTP异常反推。
  // 测试里的秘密字符串是无效固定值，不从浏览器历史读取真实回调。
  // 断言请求总数并非为了覆盖内部细节，而是保护外部单次消费合同。
  // 测试继续使用真实Schema解析，缺失授权字段不会被mock隐藏。
  // 模型真实调用没有接入该恢复流程，避免查询结果产生额外耗额。
  it('回调响应丢失时查询原会话结果，恢复后不重发一次性回调', async () => {
    const user = userEvent.setup()
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
      if (url.endsWith('/callback')) {
        throw new Error('Authorization response lost')
      }
      return {
        data: {
          success: true,
          data: url === '/api/hosting/accounts' ? account : authorization,
        },
      }
    })
    render(
      <QueryClientProvider client={client}>
        <ConnectHostingDialog onClose={vi.fn()} />
      </QueryClientProvider>
    )
    await user.type(screen.getByLabelText('Account alias'), 'Work')
    await user.click(
      screen.getByRole('button', { name: 'Generate authorization link' })
    )
    await user.type(
      await screen.findByLabelText('Authorization callback URL'),
      'http://localhost:1455/auth/callback?state=example&code=private-code'
    )
    await user.click(
      screen.getByRole('button', { name: 'Complete authorization and add' })
    )
    const check = await screen.findByRole('button', {
      name: 'Check authorization result',
    })
    expect(
      screen.queryByLabelText('Authorization callback URL')
    ).not.toBeInTheDocument()
    status = 'ready'
    await waitFor(() => expect(check).toBeEnabled())
    await user.click(check)
    await screen.findByText('Account added')
    expect(
      post.mock.calls.filter(([url]) => url.endsWith('/callback'))
    ).toHaveLength(1)
    expect(
      post.mock.calls.filter(([url]) => url === '/api/hosting/accounts')
    ).toHaveLength(1)
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
