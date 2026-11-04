import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { SearchBridgeSettings } from '../search-bridge-settings'

// 使用真实表单和变更 hook，只隔离网络边界。
// 保存按钮由设置页共享容器提供，不用测试专用按钮绕过表单。
// 每个测试独立 QueryClient，避免残留成功状态。
function Fixture(props: { value?: string }) {
  const [actions, setActions] = useState<HTMLDivElement | null>(null) // 使用正式页面顶部保存区域，覆盖真实交互入口。
  return (
    <>
      <div ref={setActions} />
      <SettingsPageProvider actionsContainer={actions}>
        <SearchBridgeSettings defaultValue={props.value ?? '{}'} />
      </SettingsPageProvider>
    </>
  )
}
// 渲染使用正常翻译回退，交互断言依据可访问名称而非样式类。
function mount(value?: string) {
  const client = new QueryClient({
    // 隔离缓存，不能依赖之前测试的选项刷新。
    defaultOptions: { mutations: { retry: false } },
  })
  return render(
    // 正式 QueryProvider 确保失败仍走统一错误处理。
    <QueryClientProvider client={client}>
      <Fixture value={value} />
    </QueryClientProvider>
  )
}
afterEach(() => vi.restoreAllMocks()) // 网络替身不能影响其他设置模块回归。

// 默认关闭且不自动保存，管理员选择的模型才进入配置。
// 清空字段必须删除路由，不能继续保留旧映射。
// 一次保存包含两个目标，避免半保存状态。
test('enables a target and removes an existing route', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  mount('{"glm-5.3":"gpt-6-luna"}') // 从已启用配置开始，验证关闭时真的删除键。
  const kimi = screen.getByRole('textbox', { name: /Kimi K3/ }) // 按可访问名称查找，避免耦合 DOM 层级。
  const glm = screen.getByRole('textbox', { name: /GLM 5.3/ }) // 两个目标必须有清晰独立的标签。
  expect(screen.getByRole('button', { name: 'Save Changes' })).toBeDisabled() // 未修改时不能意外再次提交配置。
  fireEvent.change(kimi, { target: { value: 'gpt-6-luna' } })
  fireEvent.change(glm, { target: { value: '' } }) // 留空是关闭语义，不是恢复示例值。
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/option/', {
      key: 'WebSearchBridgeRoutes',
      value: '{"kimi-k3":"gpt-6-luna"}',
    })
  )
})

// 非 GPT 模型不能误保存成搜索后端；服务端另有能力范围校验。
// 校验失败保留输入并展示错误，不能回填示例掩盖错误。
test('blocks invalid models and preserves the draft on save failure', async () => {
  const put = vi.spyOn(api, 'put').mockResolvedValue({
    data: { success: false, message: 'Rejected by server' },
  })
  mount()
  const input = screen.getByRole('textbox', { name: /Kimi K3/ }) // 用户输入错误模型时应保留可修正草稿。
  fireEvent.change(input, { target: { value: 'kimi-k3' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await screen.findByText(
    'Enter a GPT model name without spaces, or leave empty to disable'
  )
  expect(put).not.toHaveBeenCalled() // 字段错误发生时不能向后台提交。
  fireEvent.change(input, { target: { value: 'gpt-6-luna' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(put).toHaveBeenCalledTimes(1))
  await screen.findByRole('alert') // 服务端拒绝必须在页面上可见。
  expect(input).toHaveValue('gpt-6-luna') // 失败不能清掉用户已经输入的模型。
})

// 坏配置明确诊断，不展示可保存的空表单。
// 防止覆盖需要排查的原值。
test('reports malformed saved configuration', () => {
  mount('{bad-json') // 模拟持久化坏数据，而非空配置。
  expect(screen.getByRole('alert')).toHaveTextContent(
    'Invalid search bridge configuration'
  )
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument() // 不显示可以覆盖坏配置的空表单。
})
