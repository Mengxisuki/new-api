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
  // 默认 Key 已使用统一来源，平台能力不能再受挂靠主动验证门槛限制。
  // 同一目录同时包含平台、挂靠和混合来源，避免只测试单一成功路径。
  // 平台协议由后端能力目录给出，不能从模型名称猜测客户端兼容性。
  // verified=false 仅表示没有挂靠实测，不能否定真实平台渠道的协议能力。
  // 当前 Key 模型目录已经应用来源和模型限制，不得退回全站模型补选项。
  // 每个客户端都从真实弹窗点击、选择模型并生成实际导出 URL。
  // 外部协议打开仅隔离浏览器边界，防止测试启动用户电脑上的应用。
  // URL 参数读取使用标准 URL 类，不复制产品的构造或编码逻辑。
  // 同名模型可以在不同来源支持不同协议，platform_protocols 保留这一差异。
  // 混合模型的 Claude 能力不能为未验证的挂靠 Responses 提供证明。
  // 没有启用平台来源的挂靠模型仍需真实 Responses 验证才可导出。
  // 此测试证明导出按目录约束，不代替真实客户端与上游联调。
  // 参数化三种应用避免只解锁按钮，却遗漏某一个客户端的模型过滤。
  // 各场景使用独立 QueryClient，不能从前一客户端继承已加载目录或模型选择。
  // 主模型与可选备用模型共享同一个选项集合，权限入口保持单一。
  // URL 不携带 Key 编号或内部账号标识，只包含用户明确选择的公开模型。
  // 测试只提供无效秘密，不读取当前登录用户真实 API Key。
  // HTTP spy 没有返回完整渠道数据，浏览器不需要知道内部执行层配置。
  it.each([
    { app: 'Claude', model: 'platform-claude', protocol: 'claude' },
    { app: 'Codex', model: 'platform-codex', protocol: 'responses' },
    { app: 'Gemini', model: 'platform-gemini', protocol: 'gemini' },
  ])(
    '统一 Key 的 $app 按平台协议导出，不受挂靠验证状态阻断',
    async ({ app, model, protocol }) => {
      const user = userEvent.setup()
      // 确切匹配两条本人目录接口；其他请求直接报错，禁止隐式全站回退。
      // 策略启用钱包和挂靠池，符合新 Key 默认共用多类来源的使用方式。
      // 不提供所有 Key 列表，组件只接收当前行编号和已授权读取的秘密。
      const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
        if (url === '/api/hosting/tokens/7/policy') {
          return {
            data: {
              success: true,
              data: {
                revision: 1,
                legacy: false,
                sources: [
                  { kind: 'wallet', enabled: true },
                  { kind: 'hosting_pool', enabled: true },
                ],
              },
            },
          }
        }
        if (url !== '/api/hosting/tokens/7/models') {
          throw new Error(`Unexpected GET ${url}`)
        }
        return {
          data: {
            success: true,
            data: {
              models: [
                // 平台三个客户端各有真实协议，但全部未进行本人挂靠主动测试。
                {
                  id: 'platform-claude',
                  verified: false,
                  protocols: ['claude'],
                  platform_protocols: ['claude'],
                },
                {
                  id: 'platform-codex',
                  verified: false,
                  protocols: ['responses'],
                  platform_protocols: ['responses'],
                },
                {
                  id: 'platform-gemini',
                  verified: false,
                  protocols: ['gemini'],
                  platform_protocols: ['gemini'],
                },
                // 私有账号的已验证记录只开放 Codex，不扩大为任意客户端能力。
                {
                  id: 'verified-hosting',
                  verified: true,
                  protocols: ['responses'],
                },
                {
                  id: 'unverified-hosting',
                  verified: false,
                  protocols: ['responses'],
                },
                // 总协议并集含 Responses，但平台仅承担 Claude，不能把二者混用。
                {
                  id: 'mixed-unverified',
                  verified: false,
                  protocols: ['responses', 'claude'],
                  platform_protocols: ['claude'],
                },
              ],
            },
          },
        }
      })
      // window.open 是外部应用边界，其余 URL 构造和表单处理均为真实实现。
      // 返回 null 模拟浏览器没有可访问窗口对象，不影响协议调用参数检查。
      // 关闭回调只观察弹窗完成，不作为模型或授权查询的替代实现。
      const open = vi.spyOn(window, 'open').mockReturnValue(null)
      const onOpenChange = vi.fn()
      render(
        <QueryClientProvider client={queryClient}>
          <CCSwitchDialog
            open
            onOpenChange={onOpenChange}
            tokenKey='test-only'
            tokenId={7}
          />
        </QueryClientProvider>
      )
      // 策略加载后保留原统一 Key 的 Codex 默认，但用户可自由切换三种客户端。
      // 等待默认选择稳定，避免策略到达时覆盖测试刚做的选择。
      await waitFor(() =>
        expect(screen.getByRole('radio', { name: 'Codex' })).toBeChecked()
      )
      // Base UI 用 aria-disabled 表达单选项禁用，原生 disabled 查询不适用。
      // 三个选项都可切换不等于三个都保证有模型，目录过滤继续独立生效。
      // 当前测试为每种协议准备能力记录，因此分别有明确可选结果。
      for (const application of ['Claude', 'Codex', 'Gemini']) {
        expect(
          screen.getByRole('radio', { name: application })
        ).not.toHaveAttribute('aria-disabled', 'true')
      }
      // 只通过用户可见的单选按钮改变应用，不手动注入组件内部 state。
      // 切应用时原有模型草稿会清空，不能自动沿用上个应用的主模型。
      // 下拉由真实共享 Combobox 渲染，协议过滤必须落到实际可见选项。
      await user.click(screen.getByRole('radio', { name: app }))
      await user.click(screen.getByRole('combobox', { name: 'Primary Model' }))
      expect(await screen.findByRole('option', { name: model })).toBeVisible()
      // 平台协议不匹配的型号不出现在当前客户端，避免导出可选但不可调用的模型。
      // 不按 verified 统一过滤，否则三个平台成功选项都会消失。
      // 模型名故意直观区分来源，只用于测试定位，不参与生产协议推断。
      // 同一 Key 的权限交集可以同时支持多个协议，但每个客户端只取对应项。
      // 不使用全站定价目录补足选项，免得禁用来源的模型重新出现。
      for (const other of [
        'platform-claude',
        'platform-codex',
        'platform-gemini',
      ].filter((id) => id !== model)) {
        expect(
          screen.queryByRole('option', { name: other })
        ).not.toBeInTheDocument()
      }
      // 私有 Responses 能力不要求先测试，但只能用于 Codex 客户端。
      // Claude/Gemini 仍按各自平台协议过滤，不能借挂靠能力扩权。
      // 混合目录可同时具有平台 Claude 与私有 Responses，分别导出。
      if (app === 'Codex') {
        expect(
          screen.getByRole('option', { name: 'unverified-hosting' })
        ).toBeVisible()
      } else {
        expect(
          screen.queryByRole('option', { name: 'unverified-hosting' })
        ).not.toBeInTheDocument()
      }
      if (app === 'Claude' || app === 'Codex') {
        expect(
          screen.getByRole('option', { name: 'mixed-unverified' })
        ).toBeVisible()
      } else {
        expect(
          screen.queryByRole('option', { name: 'mixed-unverified' })
        ).not.toBeInTheDocument()
      }
      // 已验证挂靠继续可以导出 Codex，修复平台回归不能删掉既有挂靠入口。
      if (app === 'Codex') {
        expect(
          screen.getByRole('option', { name: 'verified-hosting' })
        ).toBeVisible()
      } else {
        expect(
          screen.queryByRole('option', { name: 'verified-hosting' })
        ).not.toBeInTheDocument()
      }
      // 选择的目标是未做挂靠验证的平台型号，能直接揭露旧 verified 总开关。
      // 已验证挂靠型号只用于验证兼容性保留，不用于绕过本次平台回归。
      // 选定型号后才点击导出，弹窗不能在目录加载时自动唤起外部应用。
      await user.click(screen.getByRole('option', { name: model }))
      await user.click(screen.getByRole('button', { name: 'Open CC Switch' }))
      // 检查真实生成参数，证明按钮接通现有导出协议而非只恢复模型列表。
      // 密钥沿用原有规范化逻辑，模型目录查询本身不会额外请求任何秘密。
      expect(open).toHaveBeenCalledTimes(1)
      // 标准 URL 解析会解码查询参数，不依赖产品实现的参数排列顺序。
      // 检查应用、模型和秘密分别绑定正确，不能只验证 URL 以协议名开头。
      // 一次用户点击仅允许一次外部打开，避免重复导入同一配置。
      const exported = new URL(String(open.mock.calls[0][0]))
      expect(exported.protocol).toBe('ccswitch:')
      expect(exported.searchParams.get('app')).toBe(app.toLowerCase())
      expect(exported.searchParams.get('model')).toBe(model)
      expect(exported.searchParams.get('apiKey')).toBe('sk-test-only')
      // Codex 继续使用 /v1 地址，Claude/Gemini 沿用原外部集成的根地址约定。
      expect(exported.searchParams.get('endpoint')).toBe(
        `${window.location.origin}${protocol === 'responses' ? '/v1' : ''}`
      )
      // 导入动作后延续原弹窗关闭行为，不将成功解释为模型调用已通过。
      // CC Switch 的安装状态与客户端最终配置由外部应用负责。
      // 本地测试不声称已在真实 Claude、Codex 或 Gemini 客户端完成连接。
      expect(onOpenChange).toHaveBeenCalledWith(false)
      // 请求集合包含 Key 作用域，无 user-models 或其他 Key 的目录混入。
      // 保留重复项以便发现意外重复读取，不使用 Set 将其掩盖。
      expect(get.mock.calls.map(([url]) => url).sort()).toEqual([
        '/api/hosting/tokens/7/models',
        '/api/hosting/tokens/7/policy',
      ])
    }
  )
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
