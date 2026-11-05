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
/**
 * 测试范围与证据解释：
 * 本文件保护事件管理页的公开交互和接口契约，
 * 不模拟生产充值或模型推理。
 * 后端事务、数据库兼容、事件生成和长连接投递由对应后端测试验证，
 * 本文件不把界面通过当作账务可靠性证明。
 * 所有数据都是明确的虚构夹具，
 * 用户编号、消费者编号、令牌编号、事件编号和投递编号刻意不同，
 * 防止错误字段因为数字碰巧相同而通过。
 * 夹具只含安全摘要，
 * 不能加入真实会话凭据或商户信息。
 * 测试运行期间的浏览器时间只用于确认有效期处于未来，
 * 不依赖执行耗时。
 * 默认有效期由生产表单生成，
 * 测试不复制生产算法计算另一个所谓期望值。
 * 创建消费者用例从真实输入框和保存按钮操作，
 * 验证逗号输入会去重排序。
 * 空关注范围和指定用户不是同一含义，
 * 非法片段不得被过滤后变成全量订阅。
 * 非法筛选用例断言表单可见错误和未调用保存，
 * 而不是只查看私有校验函数返回值。
 * 验证必须经过真实组件与校验器，
 * 不能把被测解析函数复制到测试里自证正确。
 * 编辑保存失败用例要求输入继续可见，
 * 防止后端拒绝后弹窗仍关闭。
 * 这个断言不要求具体内部状态结构，
 * 只保护用户草稿不会消失。
 * 令牌主题用例只提供一个已订阅主题，
 * 再从可访问复选框验证其他主题不可选。
 * 测试不依赖私有数组常量长度推断权限，
 * 而观察实际表单可选内容。
 * 到期时间断言只要求明确的未来秒级时间，
 * 不固定当前时钟或制造随机等待。
 * 服务设置用例模拟后台刷新时管理员正在编辑，
 * 要求草稿保持不变。
 * 这保护五秒轮询不会覆盖尚未保存的配置，
 * 也同时保证暂停以明确布尔值提交。
 * 表单保存通过真实提交事件，
 * 浏览器原生数值约束仍有效，
 * 可以发现步长和最小值不匹配导致提交被阻止的问题。
 * 临时清空数字输入不应产生非法渲染值，
 * 最终合法输入仍能保存。
 * 凭据撤销用例先点击操作，
 * 再确认弹窗，
 * 确认之前不能调用真实行为回调。
 * 弹窗使用共享组件，
 * 测试通过可访问角色定位，
 * 不依赖内部层级或样式类。
 * 撤销传令牌编号，
 * 不允许误传消费者或事件编号。
 * 补发用例使用另一事件编号，
 * 确认后只回调补发事件；
 * 不调用任何资金接口。
 * 投递状态要求没有确认时间时仍显示待确认，
 * 即使已有投递尝试时间。
 * 这保护网络写入与业务处理的区别，
 * 不能把连接收到了数据当作已经处理。
 * 管理页概况测试只模拟外部网络边界，
 * 内部查询、结构校验、错误传播和页面渲染都是真实路径。
 * 成功响应为空列表时展示空态，
 * 不能因为没有消费者就显示服务器故障。
 * 随后接口明确返回业务失败时必须展示错误和重试入口，
 * 不能保留看似正常的空列表。
 * 测试不拦截被测组件或内部业务函数，
 * 避免测试仅验证手工搭建的替代实现。
 * 令牌完整流程同样只替换网络，
 * 真实安全验证协调器仍然运行。
 * 服务端声明可以使用当前会话完成验证时，
 * 由既有验证流程请求一次证明，
 * 再将证明绑定到接入令牌创建请求。
 * 测试检查证明上下文包含消费者和排序后的主题，
 * 不能请求不相关的个人令牌授权。
 * 创建请求必须带专用证明头并声明单次授权，
 * 避免认证刷新自动重放签发。
 * 网络返回的一次性秘密只允许出现在当前弹窗中，
 * 关闭之后应从可见文档消失。
 * 列表查询永远不返回这份明文，
 * 重新拉取不会恢复秘密。
 * 测试不访问组件私有状态，
 * 而通过用户能够看到的文本判断一次性展示契约。
 * 授权失效用例要求管理员看见明确警示，
 * 并且不能继续创建接入凭据。
 * 过期未确认数量是累计历史损失，
 * 界面提示对账而不是把它混入当前待确认总量。
 * 资金未知结果不得提供盲目重新扣费按钮，
 * 测试检查这一行为边界。
 * 所有异步步骤等待明确的请求结果或界面状态，
 * 不用固定睡眠，
 * 也不比较执行速度。
 * 每次用例后恢复外部方法，
 * 查询客户端在对应集成用例结束时清理，
 * 避免定时刷新影响后续测试。
 * 测试查询使用标签、按钮名称、对话框角色和可见错误，
 * 不使用脆弱的大段样式快照。
 * 中文和其他语言由真实翻译资源维护，
 * 这些交互测试采用稳定英文键，
 * 不把本地化差异误判为业务错误。
 * 新增主题或改变公开字段时应先更新最小夹具，
 * 再根据实际行为新增必要断言；
 * 不能为覆盖率机械添加相同场景。
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { EventHubSection } from '..'
import {
  EVENT_TOPICS,
  type EventConsumer,
  type EventDetail,
  type EventSettings,
} from '../api'
import { ConsumerDetails } from '../components/consumer-details'
import { ConsumerEditor } from '../components/consumer-editor'
import { ServiceSettings } from '../components/service-settings'
import { TokenEditor } from '../components/token-editor'

// 夹具只包含公开摘要，不构造真实凭据或付款信息。
// 明确区分消费者、Token 和投递编号，避免错误字段碰巧相等。
const consumer: EventConsumer = {
  id: 7,
  name: 'Finance desktop',
  topics: [...EVENT_TOPICS],
  user_ids: [],
  enabled: true,
  created_at: 100,
  last_seen_at: 0,
  backlog: 1,
  token_count: 1,
  online_connections: 0,
  auth_valid: true,
  expired_unacked_count: 0,
}
const settings: EventSettings = {
  enabled: true,
  retention_days: 7,
  anomaly_min_quota: 2500000,
  anomaly_multiplier: 5,
  anomaly_min_samples: 20,
  anomaly_cold_quota: 10000000,
}
const detail: EventDetail = {
  consumer,
  tokens: [
    {
      id: 9,
      name: 'Desktop credential',
      topics: [EVENT_TOPICS[0]],
      token_hint: 'abcd1234',
      expires_at: 2000000000,
      created_at: 100,
      last_used_at: 0,
      revoked_at: 0,
    },
  ],
  deliveries: [
    {
      id: 11,
      event_id: 42,
      topic: EVENT_TOPICS[0],
      payload: { user_id: 3 },
      created_at: 100,
      last_sent_at: 100,
      attempts: 1,
      acked_at: 0,
    },
  ],
}
afterEach(() => vi.restoreAllMocks())

describe('event subscription management', () => {
  // 这组输入包含重复且乱序的真实编号。
  // 从界面输入开始，避免绕过字段解析。
  // 保存断言保留明确排序结果，不复制生产解析函数。
  // 新增消费者不应携带现有资源编号。
  // 回调成功后应退出创建弹窗。
  // 这里验证订阅配置，不创建任何接入令牌。
  it('creates a consumer with normalized user filters and retains no shared cursor', async () => {
    const save = vi.fn().mockResolvedValue(true)
    const close = vi.fn()
    render(<ConsumerEditor pending={false} onSave={save} onClose={close} />)
    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Consumer name'), 'Finance worker')
    await user.type(screen.getByLabelText('Follow user IDs'), '34, 12, 34')
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(save).toHaveBeenCalledWith(
        {
          name: 'Finance worker',
          topics: [...EVENT_TOPICS].sort(),
          user_ids: [12, 34],
          enabled: undefined,
        },
        undefined
      )
    )
    expect(close).toHaveBeenCalledOnce()
  })

  // 非法片段与一个合法编号混合，覆盖最危险的部分成功路径。
  // 任何片段失败都必须阻止整个保存动作。
  // 可见错误由真实表单校验器产生。
  // 不能把坏输入过滤为空列表再提交。
  // 空列表表示所有授权用户，所以静默回退会扩大关注范围。
  // 此处不直接调用解析函数，保护真实输入绑定。
  it('rejects malformed user filters rather than subscribing to everyone', async () => {
    const save = vi.fn()
    render(<ConsumerEditor pending={false} onSave={save} onClose={vi.fn()} />)
    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Consumer name'), 'Finance')
    await user.type(screen.getByLabelText('Follow user IDs'), '3, invalid')
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    expect(
      await screen.findByText('Enter positive user IDs separated by commas')
    ).toBeVisible()
    expect(save).not.toHaveBeenCalled()
  })

  // 现有消费者从夹具加载当前草稿。
  // 保存回调明确拒绝，模拟业务失败结果。
  // 表单应继续显示原有名称。
  // 关闭回调没有执行才能保留修正机会。
  // 等待可见字段状态，不用固定延迟等待提交。
  // 该场景不要求组件内部采用哪种状态变量。
  it('keeps consumer drafts visible when saving is rejected', async () => {
    const close = vi.fn()
    render(
      <ConsumerEditor
        consumer={consumer}
        pending={false}
        onSave={vi.fn().mockResolvedValue(false)}
        onClose={close}
      />
    )
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(screen.getByLabelText('Consumer name')).toHaveValue(
        'Finance desktop'
      )
    )
    expect(close).not.toHaveBeenCalled()
  })

  // 消费者只订阅资料提交主题。
  // 钱包主题必须从可操作控件中消失。
  // 测试检查用户能选到的选项，而不检查内部数组。
  // 提交经过真实表单和点击事件。
  // 到期时间必须是未来的秒级数值。
  // 不固定运行时间，因此无需通过睡眠制造时间差。
  // 创建输入不能夹带未订阅的主题。
  // 表单不接触真实凭据生成算法。
  it('limits a new token to subscribed topics and passes a concrete expiry', async () => {
    const create = vi.fn().mockResolvedValue(true)
    render(
      <TokenEditor
        consumer={{ ...consumer, topics: [EVENT_TOPICS[2]] }}
        hidden={false}
        pending={false}
        onClose={vi.fn()}
        onCreate={create}
      />
    )
    const user = userEvent.setup()
    expect(
      screen.queryByRole('checkbox', { name: 'Wallet top-up succeeded' })
    ).not.toBeInTheDocument()
    await user.type(screen.getByLabelText('Token name'), 'Operations')
    await user.click(screen.getByRole('button', { name: 'Create event token' }))
    await waitFor(() =>
      expect(create).toHaveBeenCalledWith({
        name: 'Operations',
        topics: [EVENT_TOPICS[2]],
        expires_at: expect.any(Number),
      })
    )
    expect(create.mock.calls[0][0].expires_at).toBeGreaterThan(
      Math.floor(Date.now() / 1000)
    )
  })

  // 先清空数值输入，覆盖编辑中的暂态。
  // 新输入与后台新快照故意使用不同天数。
  // 重新渲染模拟真实轮询送入更新配置。
  // 本地草稿必须保持，不能被后台快照覆盖。
  // 暂停通过可访问的开关操作。
  // 完整提交应包含显式的关闭状态。
  // 其他规则沿用原有草稿，不能混入刷新快照的部分值。
  // 真实点击提交能发现浏览器原生数值约束问题。
  it('preserves edited settings during background refresh and submits pause explicitly', async () => {
    const save = vi.fn().mockResolvedValue(true)
    const view = render(
      <ServiceSettings settings={settings} pending={false} onSave={save} />
    )
    const user = userEvent.setup()
    const days = screen.getByRole('spinbutton', {
      name: 'Event retention (days)',
    })
    await user.clear(days)
    await user.type(days, '14')
    view.rerender(
      <ServiceSettings
        settings={{ ...settings, retention_days: 9 }}
        pending={false}
        onSave={save}
      />
    )
    expect(days).toHaveValue(14)
    await user.click(
      screen.getByRole('switch', { name: 'Enable event delivery' })
    )
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(save).toHaveBeenCalledWith({
        ...settings,
        enabled: false,
        retention_days: 14,
      })
    )
  })

  // 夹具中发送时间已存在，但确认时间为零。
  // 界面仍必须显示待确认。
  // 初次点击撤销只打开确认窗口。
  // 确认前不能调用撤销回调。
  // 确认后断言令牌编号，防止混用消费者编号。
  // 等撤销窗口真正关闭后再触发补发。
  // 补发同样要求确认前没有业务调用。
  // 补发参数是事件编号，而不是投递编号。
  // 两次动作都来自用户可访问按钮。
  // 不读取弹窗内部状态或依赖样式层级。
  it('requires confirmation before revoking a token or replaying a notification', async () => {
    const revoke = vi.fn().mockResolvedValue(true)
    const replay = vi.fn().mockResolvedValue(true)
    render(
      <ConsumerDetails
        detail={detail}
        pending={false}
        onCreateToken={vi.fn()}
        onRevoke={revoke}
        onReplay={replay}
      />
    )
    const user = userEvent.setup()
    expect(screen.getByText('Awaiting acknowledgment')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Revoke' }))
    expect(revoke).not.toHaveBeenCalled()
    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Continue',
      })
    )
    await waitFor(() => expect(revoke).toHaveBeenCalledWith(9))
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    await user.click(screen.getByRole('button', { name: 'Replay event' }))
    expect(replay).not.toHaveBeenCalled()
    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Continue',
      })
    )
    await waitFor(() => expect(replay).toHaveBeenCalledWith(42))
  })

  // 只替换网络边界，查询和响应解析继续运行。
  // 首次返回明确成功的空消费者列表。
  // 空态仍允许管理员创建消费者。
  // 第二次响应明确返回业务失败。
  // 统一失效查询模拟管理员刷新当前数据。
  // 失败必须出现重试入口，不能伪装为正常空态。
  // 关闭查询重试，测试等待确定的业务结果。
  // 卸载页面后清理查询客户端，避免轮询残留。
  it('shows empty consumers from a validated response and reports an API rejection', async () => {
    const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/event-hub') {
        return {
          data: {
            success: true,
            data: { settings, topics: [], consumers: [], events_count: 0 },
          },
        }
      }
      return { data: { success: true, data: { items: [], total: 0 } } }
    })
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = render(
      <QueryClientProvider client={client}>
        <EventHubSection />
      </QueryClientProvider>
    )
    expect(await screen.findByText('No consumers yet')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Create consumer' })
    ).toBeEnabled()
    get.mockResolvedValue({
      data: { success: false, message: 'Event access rejected' },
    })
    await client.invalidateQueries({ queryKey: ['event-hub'] })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Retry' })).toBeVisible()
    )
    view.unmount()
    client.clear()
  })

  // 方法列表使用会话验证，沿用真实安全验证协调器。
  // 模拟证明由外部验证接口返回。
  // 模拟签发响应包含虚构的一次性秘密。
  // 管理详情和历史都返回真实契约结构。
  // 从管理按钮进入指定消费者，再提交凭据表单。
  // 证明请求必须绑定事件签发作用域。
  // 消费者编号和主题集合必须进入证明上下文。
  // 签发调用必须携带专用证明请求头。
  // 单次授权标记阻止认证刷新自动重放签发。
  // 一次性秘密只能在成功结果中显示。
  // 关闭后应从可见页面消失。
  // 这里不检查组件私有内存，只保护用户可见生命周期。
  // 真实系统不会使用这份固定测试凭据。
  // 清理独立查询客户端，保证用例互不依赖。
  it('keeps one consumer detail after verified token creation, secret dismissal and refresh', async () => {
    // 使用首个消费者编号覆盖真实新环境创建路径。
    // 详情与令牌编辑器同时挂载时，资源编号相同不能成为重复 React 键。
    const pageConsumer = { ...consumer, id: 1 }
    const pageDetail = { ...detail, consumer: pageConsumer }
    const errors = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/event-hub') {
        return {
          data: {
            success: true,
            data: {
              settings,
              topics: [],
              consumers: [pageConsumer],
              events_count: 1,
            },
          },
        }
      }
      if (url === '/api/event-hub/consumers/1') {
        return { data: { success: true, data: pageDetail } }
      }
      if (url === '/api/verify/methods') {
        return {
          data: {
            success: true,
            data: {
              scope: 'event_hub.token',
              methods: [{ method: 'session', available: true }],
              oauth_providers: [],
              password_encryption_enabled: false,
            },
          },
        }
      }
      return { data: { success: true, data: { items: [], total: 0 } } }
    })
    const post = vi.spyOn(api, 'post').mockImplementation(async (url) => {
      if (url === '/api/verify') {
        return {
          data: {
            success: true,
            data: {
              proof_token: 'proof-fixture',
              scope: 'event_hub.token',
              method: 'session',
              expires_at: 2000000000,
            },
          },
        }
      }
      return {
        data: {
          success: true,
          data: { token: 'evt_one_time_fixture', item: detail.tokens[0] },
        },
      }
    })
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = render(
      <QueryClientProvider client={client}>
        <EventHubSection />
      </QueryClientProvider>
    )
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Manage' }))
    await user.click(
      await screen.findByRole('button', { name: 'Create event token' })
    )
    await user.type(screen.getByLabelText('Token name'), 'Second desktop')
    await user.click(
      within(screen.getByRole('dialog')).getByRole('button', {
        name: 'Create event token',
      })
    )
    expect(await screen.findByText('evt_one_time_fixture')).toBeVisible()
    expect(post).toHaveBeenCalledWith(
      '/api/verify',
      expect.objectContaining({
        scope: 'event_hub.token',
        context: expect.objectContaining({
          consumer_id: 1,
          topics: [...EVENT_TOPICS].sort(),
        }),
      }),
      expect.any(Object)
    )
    expect(post).toHaveBeenCalledWith(
      '/api/event-hub/consumers/1/tokens',
      expect.objectContaining({ name: 'Second desktop' }),
      expect.objectContaining({
        singleUseAuthorization: true,
        headers: { 'X-Security-Proof': 'proof-fixture' },
      })
    )
    await user.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() =>
      expect(screen.queryByText('evt_one_time_fixture')).not.toBeInTheDocument()
    )
    // 成功刷新不能保留旧消费者详情，数量必须始终为一。
    // 同时检查 React 的重复键警告，定位协调过程中的真实根因。
    await client.invalidateQueries({ queryKey: ['event-hub'] })
    expect(
      screen.getAllByRole('heading', { name: consumer.name })
    ).toHaveLength(1)
    expect(
      errors.mock.calls.filter(([message]) =>
        String(message).includes('same key')
      )
    ).toHaveLength(0)
    view.unmount()
    client.clear()
  })

  // 授权失效和历史过期损失同时存在。
  // 两个警示需要分别可见，保留各自原因。
  // 签发按钮必须明确禁用。
  // 不能仅有提示而继续暴露有效签发入口。
  // 页面不能出现重新扣款或支付按钮。
  // 事件管理不承担资金恢复职责。
  // 此处只验证界面边界，服务端还会独立检查授权。
  // 所有数字都来自虚构夹具，不包含实际业务资料。
  it('shows expired authorization and expired unacknowledged totals without a funds retry action', () => {
    render(
      <ConsumerDetails
        detail={{
          ...detail,
          consumer: {
            ...consumer,
            auth_valid: false,
            expired_unacked_count: 2,
          },
        }}
        pending={false}
        onCreateToken={vi.fn()}
        onRevoke={vi.fn()}
        onReplay={vi.fn()}
      />
    )
    expect(screen.getAllByRole('alert')).toHaveLength(2)
    expect(
      screen.getByRole('button', { name: 'Create event token' })
    ).toBeDisabled()
    expect(
      screen.queryByRole('button', { name: /charge|payment|funds/i })
    ).not.toBeInTheDocument()
  })
})
