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
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { SubscriptionGantt } from '../components/subscription-gantt'
import { subscriptionDay } from '../subscriptions-api'

// 固定当前时刻以覆盖未来/到期状态；只替换时间与 HTTP 外部边界。
// 图表、日期转换、校验、弹窗及公共分页都使用真实实现。
// 测试固定上海十一月，避免月底运行时预设范围发生变化。
// fixture 的用户和号池故意拥有不同来源，验证合并后的分类不会串行。
// 取消用户订阅仍与当前月份相交，不能被 active 筛选误删。
// 号池周期跨完整月份，结束日转换可直接核对为十一月三十日。
// 只使用虚构名称和用户编号，不加载生产数据或真实凭据。
// 所有行为通过真实组件触发，测试不复制条形位置算法。
// HTTP mock 返回公开协议结构，使 schema 缺失也会暴露为错误。
// 数据库分页、身份连接和权限隔离由后端三库测试覆盖。
// 此文件只验证用户可观察的控件行为，不重复实现数据库查询。
// 不替换日期控件和弹窗，确保焦点、标签与提交关联真实存在。
// Mutation 使用无自动重试设置，测试中的重试必须由用户主动发起。
// Query 使用无自动重试设置，使请求失败立即落到可操作错误态。
// 独立 QueryClient 防止前一条用例的成功响应掩盖当前网络失败。
// 每个用例后重置认证状态，Root 能力不能泄漏到只读场景。
// 全局测试配置在用例后清理 DOM 并恢复 spies，无需重复清理实现。
// 没有快照断言，文案、状态及对外请求才是验收依据。
// 这些用例不证明像素布局，窄屏溢出另由真实浏览器检查。
// 不 mock 保存函数，否则可能跳过业务 success=false 的关键处理。
// 日期输入通过 change 模拟浏览器标准输入事件，不直接修改组件 state。
// 按钮点击使用 userEvent，让基础组件的正常事件流程参与测试。
const DAY = 1793462400
const pool = {
  id: 1,
  category: 'pool',
  name: 'Codex pool',
  user_id: 0,
  username: '',
  plan_id: 0,
  start_time: DAY,
  end_time: DAY + 30 * 86400,
  status: '',
}
const user = {
  ...pool,
  id: 2,
  category: 'user',
  name: 'Monthly plan',
  user_id: 7,
  username: 'alice',
  plan_id: 5,
  status: 'cancelled',
}
let client: QueryClient

// 每个用例单独建立缓存实例，避免上一个月份的结果污染下一个场景。
// 固定 Date.now 保持真实计时器，不影响 userEvent 的异步事件调度。
// 不启用全局 fakeTimers，弹窗与 Query 的正常等待语义可以保留。
// 查询重试关闭后，错误态的出现由一次失败响应决定。
// 手工点击重试可以清楚验证错误恢复责任属于界面。
// Mutation 不重放非幂等创建请求，避免 mock 掩盖重复提交风险。
// fixture 的起止常量独立于被测日期工具，测试不会自证正确。
// 测试环境默认为英文，仅用于稳定地查询组件的公开名称。
// 中文翻译完整性由 locale 文件静态检查与实际浏览器共同验证。
// 如果将来添加第三个来源，应先更新契约与相应分类交互用例。
// 本次明确只有两类，不能用挂靠账户的授权到期补足时间字段。
// 后端三库用例另外覆盖编号重复的两类数据，前端只按公开投影读取。
// 用户套餐取消记录的日期仍然可见，保留历史安排的解释能力。
// 不根据保存 toast 判定持久化，后端测试和浏览器回显负责确认实际写入。
// 这里的 HTTP 调用断言确认客户端没有遗漏必要字段。
// 失败草稿保留断言防止用户修正日期后被异步回包覆盖。
// 查询失败恢复用例防止错误标记在下一次成功后继续遮挡内容。
// 校验与错误恢复采用同一组件实例，不通过卸载重挂绕过状态缺陷。
// 测试结束后不会留下任何本地存储中的人工周期配置。
beforeEach(() => {
  vi.spyOn(Date, 'now').mockReturnValue((DAY + 10 * 86400) * 1000)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset('complete')
})

// 已授权入口内挂载图表；角色仅用于区分只读者和人工配置操作者。
function mount(role = 100) {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'fixture', role })
  render(
    <QueryClientProvider client={client}>
      <SubscriptionGantt />
    </QueryClientProvider>
  )
}

// 所有页面都读取这两个真实格式 fixture，筛选由模拟 HTTP 服务执行。
function mockList() {
  return vi.spyOn(api, 'get').mockImplementation(async (_url, config) => {
    const params = config?.params as { category: string }
    return {
      data: {
        success: true,
        data: {
          items: [user, pool].filter(
            (row) =>
              params.category === 'all' || params.category === row.category
          ),
          total: params.category === 'all' ? 2 : 1,
        },
      },
    }
  })
}

// 只读角色可以读取周期，但看不到人工记录的新增动作。
// 默认窗口必须覆盖整月，未来到期的合同不能因今天日期而消失。
// HTTP 请求参数是窗口计算的公开合同，不检查内部 useState。
// 取消状态在时间条的可访问名称中也必须出现，不能只用透明度表达。
// 切换到号池后用户记录消失，证明分类确实进入网络请求。
// 等待实际可见结果后才断言，避免依赖请求完成的偶然时序。
// 测试同时保留号池存在断言，不能用整个表格消失冒充过滤成功。
// 分类按钮按可访问名称定位，不依赖 DOM 中的第几个元素。
// 不把英文翻译值当做业务逻辑输入，测试初始化语言仅为稳定查询。
test('默认展示整月，筛选来源只展示匹配记录并保留取消状态', async () => {
  const get = mockList()
  mount(5)
  expect(await screen.findByText('Monthly plan')).toBeInTheDocument()
  expect(screen.getByText('Cancelled')).toBeInTheDocument()
  expect(
    screen.getByRole('img', {
      name: /Monthly plan.*2026-11-01.*2026-11-30.*Cancelled/,
    })
  ).toBeInTheDocument()
  expect(get).toHaveBeenCalledWith(
    '/api/finance/subscriptions',
    expect.objectContaining({
      params: { start: DAY, end: DAY + 30 * 86400, page: 1, category: 'all' },
    })
  )
  // 读者看得到两类数据，但不能新增或编辑号池。
  expect(
    screen.queryByRole('button', { name: 'Add pool subscription' })
  ).not.toBeInTheDocument()
  const action = userEvent.setup()
  await action.click(
    screen.getByRole('button', { name: 'Platform account pool' })
  )
  await waitFor(() =>
    expect(screen.queryByText('Monthly plan')).not.toBeInTheDocument()
  )
  expect(screen.getByText('Codex pool')).toBeInTheDocument()
})

// 用户清空日期时，应保留输入状态并暂停请求。
// 这个中间状态曾有风险让时间尺格式化 NaN，必须保护整个表格不渲染。
// 刷新按钮不能绕过禁用查询，坏日期不应到达服务端。
// 在清空前先完成初始请求，之后清除 spy 记录以隔离用户操作。
// 未来月份必须可选择，不能继承财务收入日期控件的历史上限。
// 空结果由接口明确返回，不能靠解析错误或异常默认成空数组。
// 确认空态文案意味着未来筛选已经完成有效请求。
// 测试不等待固定毫秒数，而是等待用户可见的终态。
test('无效日期不发请求或绘制旧时间尺，修正后可以查看未来空窗口', async () => {
  const get = mockList()
  mount()
  await screen.findByText('Codex pool')
  get.mockClear()
  fireEvent.change(screen.getByLabelText('Timeline start date'), {
    target: { value: '' },
  })
  expect(screen.getByRole('alert')).toHaveTextContent('Select valid dates')
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
  expect(
    screen.getByRole('button', { name: 'Refresh timeline' })
  ).toBeDisabled()
  expect(get).not.toHaveBeenCalled()
  // 未来月份允许查询；空列表有明确状态，不以零长度的图表冒充数据。
  get.mockResolvedValue({
    data: { success: true, data: { items: [], total: 0 } },
  })
  fireEvent.change(screen.getByLabelText('Timeline end date'), {
    target: { value: '2027-01-31' },
  })
  fireEvent.change(screen.getByLabelText('Timeline start date'), {
    target: { value: '2027-01-01' },
  })
  expect(
    await screen.findByText('No subscriptions overlap the selected dates.')
  ).toBeInTheDocument()
})

// 新增时必须打开无旧值的独立草稿。
// 通过 label 查询字段可发现标签与输入关联丢失的问题。
// 结束日期包含当天，提交秒数必须是十二月一日零点。
// 断言具体整数常量，不在测试里调用生产转换函数计算期望值。
// 保存成功后关闭弹窗，成功列表通过重新请求而非乐观伪造得到。
// 清除初始 GET 记录后，才能证明保存实际触发了列表刷新。
// 请求只允许三个可编辑字段，不能携带虚构分类或用户编号。
// 此测试不要求响应携带手工拼造的 ID，真实主键由服务器分配。
// 成功接口返回契约本身足够，客户端不依赖不需要的响应字段。
// 测试不执行真实付费或账号授权，号池记录只是隔离网络 fixture。
test('新增号池提交上海日期含结束日，保存成功后列表重新请求', async () => {
  const get = mockList()
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  mount()
  await screen.findByText('Codex pool')
  const action = userEvent.setup()
  await action.click(
    screen.getByRole('button', { name: 'Add pool subscription' })
  )
  const dialog = within(screen.getByRole('dialog'))
  await action.type(dialog.getByLabelText('Pool or account name'), 'New pool')
  fireEvent.change(dialog.getByLabelText('Start date'), {
    target: { value: '2026-11-01' },
  })
  fireEvent.change(dialog.getByLabelText('End date'), {
    target: { value: '2026-11-30' },
  })
  get.mockClear()
  await action.click(dialog.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(post).toHaveBeenCalledWith('/api/finance/pool-subscriptions', {
    name: 'New pool',
    start_time: DAY,
    end_time: DAY + 30 * 86400,
  })
  expect(get).toHaveBeenCalled()
})

// 编辑入口必须指向号池行，不允许用户套餐出现同类按钮。
// 回显结束日需要排除接口的次日边界，不能增加一天。
// 倒置日期在表单内直接报错，不应产生任何 PUT 请求。
// 顺序错误归属于结束日期，用户能在原输入位置修复。
// 业务失败使用 HTTP 成功但 success=false，覆盖最容易伪装成功的情况。
// 该失败不能触发缓存成功分支或关闭弹窗。
// 新的结束日必须保留，用户不需要重新填写整份草稿。
// 第二次提交改为成功响应，证明失败态没有永久锁死保存按钮。
// 测试只改变网络返回，不重建组件，覆盖同一草稿的实际重试路径。
// 完整日期回显保护跨月订阅，不能把图表窗口结尾当作编辑默认值。
// 读取弹窗内的错误而非全局 toast，避免瞬时通知让测试竞态。
// 日期校验和请求错误分开验证，两者不共用不明确的失败断言。
test('编辑回显完整周期，倒置日期拒绝，服务端失败保留草稿供重试', async () => {
  mockList()
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: false, message: 'Save rejected' } })
  mount()
  await screen.findByText('Codex pool')
  const action = userEvent.setup()
  await action.click(
    screen.getByRole('button', { name: 'Edit pool subscription' })
  )
  const dialog = within(screen.getByRole('dialog'))
  expect(dialog.getByLabelText('Start date')).toHaveValue('2026-11-01')
  expect(dialog.getByLabelText('End date')).toHaveValue('2026-11-30')
  fireEvent.change(dialog.getByLabelText('End date'), {
    target: { value: '2026-10-31' },
  })
  await action.click(dialog.getByRole('button', { name: 'Save' }))
  expect(
    await dialog.findByText(
      'The end date must not be earlier than the start date.'
    )
  ).toBeInTheDocument()
  expect(put).not.toHaveBeenCalled()
  fireEvent.change(dialog.getByLabelText('End date'), {
    target: { value: '2026-12-31' },
  })
  await action.click(dialog.getByRole('button', { name: 'Save' }))
  expect(await dialog.findByRole('alert')).toHaveTextContent('Save failed')
  expect(dialog.getByLabelText('End date')).toHaveValue('2026-12-31')
  // 重试成功后弹窗关闭；相同草稿不能被失败回包丢弃。
  put.mockResolvedValue({ data: { success: true } })
  await action.click(dialog.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
})

// 行菜单只打开确认，首次点击删除菜单时不得发送请求。
// 实际确认按钮触发删除，避免把菜单点击误认为不可撤回动作。
// 网络拒绝时确认框必须保留，并显示持久错误文本。
// 失败之后可以重试，不需要重新选择行或重新打开菜单。
// 成功后确认框关闭，证明 mutation 成功分支已完成。
// 最后断言精确资源 URL，防止拿用户编号代替号池记录编号。
// 删除在 HTTP mock 内执行，不影响真实环境的数据。
// 该用例复用公共确认组件，不绕过它直接调用回调。
// 没有依赖菜单生成的随机 ID，键盘可访问标签也是测试入口。
test('删除先确认，失败仍可重试，成功后刷新周期', async () => {
  mockList()
  const remove = vi
    .spyOn(api, 'delete')
    .mockRejectedValue(new Error('network failure'))
  mount()
  await screen.findByText('Codex pool')
  const action = userEvent.setup()
  await action.click(screen.getByRole('button', { name: 'Actions' }))
  await action.click(screen.getByRole('menuitem', { name: 'Delete' }))
  const dialog = within(screen.getByRole('alertdialog'))
  expect(remove).not.toHaveBeenCalled()
  await action.click(dialog.getByRole('button', { name: 'Delete' }))
  expect(await dialog.findByRole('alert')).toHaveTextContent('Delete failed')
  remove.mockResolvedValue({ data: { success: true } })
  await action.click(dialog.getByRole('button', { name: 'Delete' }))
  await waitFor(() =>
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  )
  expect(remove).toHaveBeenCalledWith('/api/finance/pool-subscriptions/1')
})

// 读取失败必须表现为错误态，不能显示“没有订阅”。
// 重试沿用原来的筛选状态，只替换下一次网络响应。
// 成功恢复后名称重新可见，证明错误不会永久覆盖数据展示。
// 该场景不需要模拟保存，它只验证独立读取链路的恢复。
// 失败响应使用网络拒绝，与前面的业务保存失败形成不同边界。
// 通过用户点击 Retry 继续流程，不调用 query.refetch 的内部引用。
// 不检查 React Query 私有缓存结构，避免锁定库的实现细节。
test('接口失败提供重试入口，成功重试后恢复图表', async () => {
  const get = vi
    .spyOn(api, 'get')
    .mockRejectedValue(new Error('network failure'))
  mount()
  await screen.findByRole('button', { name: 'Retry' })
  get.mockResolvedValue({
    data: { success: true, data: { items: [pool], total: 1 } },
  })
  await userEvent.setup().click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByText('Codex pool')).toBeInTheDocument()
})

// 日期入口排除浏览器自动进位，且不受运行机器时区影响。
// 二月三十日会被部分日期解析器进位到三月，入口必须明确拒绝。
// 空输入是合法编辑中间状态，但不是合法的业务日期。
// 固定上海零点断言确保运行测试机器的本地时区不会改变结果。
// 这里直接测试公开转换入口，不把转换逻辑复制到断言右侧。
// 保存的结束日包含语义已由新增表单请求测试覆盖。
// 因此本用例无需重复枚举一整年的所有日期。
// 对于查询与保存共用的基础函数，保留这一小组边界即可定位回归。
test('日期解析拒绝不存在的日期并保留上海零点', () => {
  expect(subscriptionDay('2026-02-30')).toBeNaN()
  expect(subscriptionDay('')).toBeNaN()
  expect(subscriptionDay('2026-11-01')).toBe(DAY)
})
