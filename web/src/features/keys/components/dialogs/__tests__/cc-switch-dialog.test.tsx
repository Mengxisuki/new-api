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

import { CCSwitchDialog } from '../cc-switch-dialog'

let queryClient: QueryClient

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
})

afterEach(() => {
  queryClient.clear()
})

function renderDialog(models = ['gpt-5.4', 'claude-sonnet-4-6']) {
  queryClient.setQueryData(['user-models-ccswitch'], {
    success: true,
    data: models,
  })
  render(
    <QueryClientProvider client={queryClient}>
      <CCSwitchDialog open onOpenChange={vi.fn()} tokenKey='test-only' />
    </QueryClientProvider>
  )
}

describe('CC Switch model selection', () => {
  // 真实组件仅隔离 HTTP，保护统一 Key 不被旧全站目录扩权。
  // 新场景提供明确 Key 编号，原有场景省略编号继续验证旧行为。
  // 策略响应 legacy=false 是进入统一资源目录的真实契约。
  // 目录中同时放已验证和未验证模型，防止实现只检查协议字段。
  // 两项都声明 Responses，使 verified 的过滤作用可以被独立观察。
  // Key 编号七只出现在本人目录路径，不能请求全站模型后再前端猜权限。
  // 真实组件的异步策略查询决定默认应用，测试等待 Codex 被选中。
  // 不直接修改组件 state，也不 mock 挂靠 API 包装函数。
  // Base UI 单选项使用 aria-disabled，因此不误用原生 disabled 断言。
  // Claude 与 Gemini 在统一策略首期未被验证，必须不能导出占位配置。
  // 模型选择使用真实下拉，能同时保护接口解析和选项过滤接线。
  // 未验证模型不可见，避免用户得到必然无法保证兼容的配置。
  // 请求集合只允许当前 Key 的 hosting 路径，防止隐式全站回退。
  // 测试不真正调用外部 CC Switch 协议，避免启动用户电脑上的应用。
  // 导出 URL 构造沿用原实现，本场景聚焦可导出模型的授权边界。
  // 固定 test-only 密钥是无效 fixture，不是可用于平台请求的凭据。
  // 既有三个应用的测试继续保留，保护旧 Key 不被统一策略限制误伤。
  // 策略与模型查询都保留 success/data 信封，实际入口 Schema 会参与验证。
  // 没有浏览器延时假设，findByRole 等待真实选项加载完成。
  // 断言用户可见状态和公开 HTTP 路径，不依赖私有缓存变量。
  // 上游实测记录真假由后端负责，前端只遵守服务端 verified 事实。
  // 该测试不证明原厂模型实时可用，只保护导出不会夸大能力。
  // 查询失败不能以旧全站目录兜底，这个路径范围断言能捕获回归。
  // 如果后续增加供应商协议，需增加相应验证 fixture 后再放开应用。
  it('统一策略只显示该Key已验证Responses模型，其他客户端不可导出', async () => {
    const user = userEvent.setup()
    const get = vi.spyOn(api, 'get').mockImplementation(async (url) => ({
      data: {
        success: true,
        data: url.endsWith('/policy')
          ? { revision: 1, inherited: false, legacy: false, sources: [] }
          : {
              models: [
                {
                  id: 'verified-codex',
                  verified: true,
                  protocols: ['responses'],
                },
                { id: 'unverified', verified: false, protocols: ['responses'] },
              ],
            },
      },
    }))
    render(
      <QueryClientProvider client={queryClient}>
        <CCSwitchDialog
          open
          onOpenChange={vi.fn()}
          tokenKey='test-only'
          tokenId={7}
        />
      </QueryClientProvider>
    )
    await waitFor(() =>
      expect(screen.getByRole('radio', { name: 'Codex' })).toBeChecked()
    )
    expect(screen.getByRole('radio', { name: 'Claude' })).toHaveAttribute(
      'aria-disabled',
      'true'
    )
    expect(screen.getByRole('radio', { name: 'Gemini' })).toHaveAttribute(
      'aria-disabled',
      'true'
    )
    await user.click(screen.getByRole('combobox', { name: 'Primary Model' }))
    expect(
      await screen.findByRole('option', { name: 'verified-codex' })
    ).toBeVisible()
    expect(
      screen.queryByRole('option', { name: 'unverified' })
    ).not.toBeInTheDocument()
    expect(
      get.mock.calls.every(([url]) => url.startsWith('/api/hosting/tokens/7/'))
    ).toBe(true)
  })
  it.each(['Claude', 'Codex', 'Gemini'])(
    'opens %s models outside the clipping dialog and keeps the dialog open after selection',
    async (app) => {
      renderDialog()
      const user = userEvent.setup()
      await user.click(screen.getByRole('radio', { name: app }))
      const input = screen.getByRole('combobox', { name: 'Primary Model' })

      await user.click(input)

      expect(input).toHaveAttribute('aria-expanded', 'true')
      const list = await screen.findByRole('listbox')
      const dialog = screen.getByRole('dialog', { name: 'Import to CC Switch' })
      // The dialog is translated and clips overflow. Its popup must escape
      // that containing block to remain aligned and fully visible.
      expect(dialog).not.toContainElement(list)
      await user.click(screen.getByRole('option', { name: 'gpt-5.4' }))
      await waitFor(() => expect(input).toHaveValue('gpt-5.4'))
      expect(input).toHaveAttribute('aria-expanded', 'false')
      expect(dialog).toBeVisible()
    }
  )

  it('filters model names and supports keyboard selection and Escape without closing the dialog', async () => {
    renderDialog()
    const user = userEvent.setup()
    await user.click(screen.getByRole('radio', { name: 'Codex' }))
    const input = screen.getByRole('combobox', { name: 'Primary Model' })
    await user.click(input)
    await user.type(input, 'sonnet')

    expect(
      screen.queryByRole('option', { name: 'gpt-5.4' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('option', { name: 'claude-sonnet-4-6' })
    ).toBeVisible()
    await user.keyboard('{ArrowDown}{Enter}')
    await waitFor(() => expect(input).toHaveValue('claude-sonnet-4-6'))
    await user.click(input)
    await user.keyboard('{Escape}')

    expect(input).toHaveValue('claude-sonnet-4-6')
    expect(input).toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByRole('dialog')).toBeVisible()
  })

  it('shows an empty result when no models are available and updates an open dropdown when models arrive', async () => {
    renderDialog([])
    const user = userEvent.setup()
    await user.click(screen.getByRole('radio', { name: 'Codex' }))
    const input = screen.getByRole('combobox', { name: 'Primary Model' })
    await user.click(screen.getByRole('button', { name: 'Primary Model' }))

    expect(await screen.findByText('No models found')).toBeVisible()
    await act(async () => {
      queryClient.setQueryData(['user-models-ccswitch'], {
        success: true,
        data: ['gpt-5.4'],
      })
    })
    await user.click(await screen.findByRole('option', { name: 'gpt-5.4' }))
    await waitFor(() => expect(input).toHaveValue('gpt-5.4'))
  })

  it('lets users edit the provider name without opening a model dropdown', async () => {
    renderDialog()
    const user = userEvent.setup()
    const input = screen.getByRole('textbox', { name: 'Name' })

    await user.clear(input)
    await user.type(input, 'Development')

    expect(input).toHaveValue('Development')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })
})
