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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { ResourcePolicyEditor } from '../components/resource-policy'
import type { HostingAccount, ResourcePolicy } from '../types'

// 只隔离 HTTP 边界，编辑器、排序手柄、查询缓存和提交解析均使用真实实现。
// 两个账号保留不同 ID，保护排序提交使用业务身份而非列表位置。
// 候选账号使用独立业务编号，不依赖数组下标生成来源身份。
// 两个不同备注方便用户视角定位各个添加和排序控件。
// 账号处于 connected 即可配置来源，实测成功不是编辑策略的前置条件。
// 暂停和耗尽由真实执行时检查，这里不以额度控制表单授权。
// fixture 不包含凭据，策略编辑不应要求读取明文 Key。
// 邮箱使用保留测试域名并脱敏，避免引入真实个人数据。
const accounts: HostingAccount[] = [1, 2].map((id) => ({
  id,
  provider: 'codex',
  alias: `Account ${id}`,
  email_mask: 'a***@example.test',
  status: 'connected',
  paused: false,
  verified_at: 0,
  last_error: '',
}))
// 测试边界包括组件、共享排序控件、查询缓存和 API 解析。
// 未覆盖真实浏览器触摸拖拽的像素位置，使用键盘路径验证可访问排序。
// 来源排序最终仍需后端执行器遵守，前端测试不替代路由选择测试。
// 支付费用由后端计算，本测试仅保护用户明确启停付费来源的授权。
// 授权范围变化通过请求体核对，不能用截图代替协议断言。
// 多账号候选只有本人摘要，越权账号拒绝由服务端测试负责。
// 并发版本冲突应保留后端错误，测试不会在 mock 中自动合并版本。
// fixtures 不包含随机值，因此失败可以稳定复现。
// 所有断言均围绕用户行为或公开 HTTP 契约，不读取内部私有变量。
let client: QueryClient
// 版本三模拟已经保存过的策略，保护提交携带读取时 revision。
// 首选挂靠、次选钱包能同时覆盖免费来源与付费回退提示。
// 初始钱包启用，使禁用开关产生可观察的用户文案变化。
// 不把包月加入该 fixture，避免无关来源增加断言负担。
// 空策略用独立交互产生，不能以初始缺少字段冒充。
// 继承场景覆盖 initial 的标志，保留相同有效来源以隔离模式变化。
const initial: ResourcePolicy = {
  revision: 3,
  inherited: false,
  legacy: false,
  sources: [
    { kind: 'hosting', account_id: 1, enabled: true },
    { kind: 'wallet', enabled: true },
  ],
}
// 每次挂载使用全新 QueryClient，防止前一个 Key 的策略污染当前测试。
// 预置查询缓存只隔离初始读取，后续保存仍走真实 HTTP 适配函数。
// 查询重试关闭，异常不会被测试运行时间掩盖。
// 无限新鲜度避免测试通过依赖不相关的后台自动刷新。
// mutation 重试关闭，测试不能因重复写入恰好成功而通过。
// 默认层和 Key 层查询键与真实组件一致。
// 这里只封装重复装配，不替换被测编辑器、排序控件或安全契约。
// 当前传入的 tokenId 决定真实请求路径，断言因此能保护作用域隔离。
// 清理 QueryClient 释放异步缓存，保证各场景可单独运行。
// 界面交互仍由 userEvent 产生，不直接调用组件内部 setter。
function mount(policy = initial, tokenId?: number) {
  client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  })
  client.setQueryData(['hosting', 'accounts'], accounts)
  client.setQueryData(['hosting', 'policy', tokenId ?? 'default'], policy)
  render(
    <QueryClientProvider client={client}>
      <ResourcePolicyEditor tokenId={tokenId} />
    </QueryClientProvider>
  )
}
afterEach(() => {
  client.clear()
})

describe('资源优先级', () => {
  // 重新加载回归来源于相同 revision 不触发表单重挂载的情况。
  // 先修改本地钱包开关，确保存在需要撤销的草稿。
  // HTTP 返回完全相同的服务端快照，故意不靠版本变化救场。
  // 期望开关恢复和保存按钮变灰，两者都对应用户可见状态。
  // 不检查组件内部 dirty 变量，允许未来实现更换表单库。
  // 该场景不保存策略，保护刷新动作没有写入副作用。
  it('服务端版本未变时重新加载仍撤销本地未保存的来源开关', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: initial },
    })
    mount()
    const wallet = screen.getByRole('switch', { name: 'Enable Wallet balance' })
    await user.click(wallet)
    expect(wallet).not.toBeChecked()
    await user.click(screen.getByRole('button', { name: 'Reload policy' }))
    expect(wallet).toBeChecked()
    expect(screen.getByRole('button', { name: 'Save priority' })).toBeDisabled()
  })
  // 键盘移动使用真实排序手柄，保护无需鼠标也能调整优先级。
  // 按下 ArrowDown 与关闭钱包是用户连续编辑同一草稿。
  // 请求断言核对完整数组，能捕获仅发送变化项导致的错误合并。
  // 钱包禁用后费用警告应消失，保证文案与当前草稿一致。
  // 原版本仍为三，前端不能自行加一来绕过后端并发判断。
  // 观察实际 HTTP PUT，而不是对保存函数使用 mock 自证调用。
  // waitFor 等待请求产生，不使用固定睡眠猜测 React 提交时机。
  // 账号身份与 enabled 一起保留，避免移动时丢失开关状态。
  it('键盘移动并关闭钱包后，保存完整顺序和原版本', async () => {
    const user = userEvent.setup()
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: initial } })
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: initial },
    })
    mount()
    fireEvent.keyDown(
      screen.getByRole('button', { name: 'Drag Account 1 to reorder' }),
      { key: 'ArrowDown' }
    )
    await user.click(
      screen.getByRole('switch', { name: 'Enable Wallet balance' })
    )
    expect(
      screen.queryByText(
        'Wallet is enabled. Falling back to it can charge your wallet at the published model prices.'
      )
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Save priority' }))
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith('/api/hosting/policy', {
        revision: 3,
        sources: [
          { kind: 'wallet', enabled: false },
          { kind: 'hosting', account_id: 1, enabled: true },
        ],
      })
    )
  })
  // Key 继承默认时必须真正只读，不能只显示一段继承文字。
  // 手柄的禁用属性是键盘用户可观察的约束。
  // 自定义操作复制完整有效来源，新增账号后仍保留原有项。
  // 请求 URL 必须指向该 Key，不能误改用户默认策略。
  // 新增账号放在末尾，保护显式排序的可预测性。
  // 断言完整三个来源，能捕获覆盖被实现成部分追加的错误。
  // 保持原读取版本，避免把继承行和默认行的版本混用。
  // 测试不读取 Key 明文，策略权限仅依赖管理会话和编号。
  it('继承策略禁用排序，进入自定义后才保存 Key 完整覆盖', async () => {
    const user = userEvent.setup()
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true, data: initial } })
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: initial },
    })
    mount({ ...initial, inherited: true }, 7)
    expect(
      screen.getByRole('button', { name: 'Drag Account 1 to reorder' })
    ).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Customize priority' }))
    await user.click(screen.getByRole('button', { name: 'Add Account 2' }))
    await user.click(screen.getByRole('button', { name: 'Save priority' }))
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith('/api/hosting/tokens/7/policy', {
        revision: 3,
        sources: [
          ...initial.sources,
          { kind: 'hosting', account_id: 2, enabled: true },
        ],
      })
    )
  })
  // 清空来源是合法的明确拒绝策略，不应自动恢复付费兜底。
  // 用户逐个移除两个来源，经过与真实页面相同的按钮路径。
  // 状态提示必须解释未来请求会被拒绝，避免用户误判服务故障。
  // 仍允许保存空策略，保护禁止全部来源这一产品能力。
  // 此处不提交 HTTP，重点是编辑阶段的即时反馈和可保存性。
  // 删除策略与保存空策略的区别由下一场景单独覆盖。
  it('移除全部来源显示拒绝提示，不自动恢复继承或钱包', async () => {
    const user = userEvent.setup()
    mount()
    await user.click(screen.getByRole('button', { name: 'Remove Account 1' }))
    await user.click(
      screen.getByRole('button', { name: 'Remove Wallet balance' })
    )
    expect(screen.getByRole('status')).toHaveTextContent(
      'All resource sources are disabled'
    )
    expect(screen.getByRole('button', { name: 'Save priority' })).toBeEnabled()
  })
  // 恢复继承必须删除覆盖记录，不能写入空来源数组。
  // 真实 DELETE 请求路径保护不会误删账号或默认策略。
  // 额外观察 PUT 未被调用，捕获空数组冒充继承的回归。
  // 服务器读回 inherited=true，让编辑器显示新的继承状态。
  // 请求成功仍由组件自己的 invalidation 触发后续查询。
  // 不模拟内部 mutation 成功回调，避免遗漏真实接线问题。
  // Key 编号七与账号编号不同，降低误用命名空间被 fixture 掩盖的机会。
  // 测试不依赖 Toast 文案，核心契约是实际授权记录发生的变更。
  it('恢复默认明确删除覆盖，绝不提交空数组冒充继承', async () => {
    const user = userEvent.setup()
    const remove = vi
      .spyOn(api, 'delete')
      .mockResolvedValue({ data: { success: true } })
    const put = vi.spyOn(api, 'put')
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { ...initial, inherited: true } },
    })
    mount(initial, 7)
    await user.click(
      screen.getByRole('button', { name: 'Restore default priority' })
    )
    await waitFor(() =>
      expect(remove).toHaveBeenCalledWith('/api/hosting/tokens/7/policy')
    )
    expect(put).not.toHaveBeenCalled()
  })
})
