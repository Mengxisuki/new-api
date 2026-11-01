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
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useEffect } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { Button } from '@/components/ui/button'
import { ApiKeysDialogs } from '@/features/keys/components/api-keys-dialogs'
import {
  ApiKeysProvider,
  useApiKeys,
} from '@/features/keys/components/api-keys-provider'
import { apiKeySchema } from '@/features/keys/types'
import { api } from '@/lib/api'

// 使用真实 Key 弹窗承载配置，避免在测试里自行复制 key 属性掩盖回归。
// 公开 Provider 是当前行选择的边界，模型与秘密仍由真实组件管理。
// Key fixture 仅为列表里的脱敏摘要，读取明文必须另走 HTTP 操作。
// 两个 Key 都有效，排除过期、额度等与配置隔离无关的条件。
// Schema 解析沿用真实输入合同，默认字段也由项目已有规则提供。
// 测试密钥字符串没有生产用途，不能用于真实服务鉴权。
// 列表 key 字段刻意使用 masked 前缀，与后续读取结果完全不同。
// 如果组件误把列表值当作可用秘密，实际剪贴板断言会直接失败。
// 额度设为正数只是排除表单的无关空值，不授予任何额外模型权限。
// 不提供自动分组配置，避免把分组顺序与资源来源顺序混为一谈。
// expired_time=-1 使用现有永久 Key 合同，不依赖当前测试机器时钟。
// fixture 没有原厂账号 ID，用户 Key 不应携带执行层管理标识。
const workKey = apiKeySchema.parse({
  id: 11,
  name: 'Work key',
  key: 'masked-work',
  status: 1,
  remain_quota: 100,
  used_quota: 0,
  unlimited_quota: false,
  expired_time: -1,
  created_time: 1,
  accessed_time: 1,
  model_limits_enabled: false,
})
// 第二个摘要复用正常属性，只改变资源身份与可见名称。
// 这样切换后的行为差异只能来自当前 Key，而不是其他权限变化。
// name 用于父 Dialog 描述，不能代替 id 作为配置的查询作用域。
const travelKey = {
  ...workKey,
  id: 22,
  name: 'Travel key',
  key: 'masked-travel',
}

// 同型号同时出现在两个目录中，专门揭露切 Key 后沿用旧选择的漏洞。
// 如果第二个 Key 只有完全不同的模型，supported=false 会掩盖秘密残留。
// 各自专属型号同时验证模型目录来自当前 Key，未混入另一目录。
// verified=true 与 Responses 支持共同构成可导出的必要条件。
// 非 Responses 模型即使验证过，也不能生成 Codex 配置。
// 模型名称是虚构目录数据，不声称任何原厂真实型号当前可用。
// 协议数组采用服务端 DTO 的 responses 值，不使用请求路径代替。
// 此记录只验证前端筛选；真正的模型测试与验证记录归后端负责。
// 目录 fixture 不含源账号、成本或策略，配置组件无需读取这些信息。
// 目录返回的 verified 记录不代表永久可用，本文件只确认配置导出门槛。
// 暂时余额耗尽是否隐藏模型是后端目录政策，不在前端另写规则。
// 不把模型名硬编码进配置组件；用户仍从真实下拉选择服务端型号。
const sharedModel = {
  id: 'shared-verified',
  verified: true,
  protocols: ['responses'],
}
const workOnlyModel = {
  id: 'work-only',
  verified: true,
  protocols: ['responses'],
}
const travelOnlyModel = {
  id: 'travel-only',
  verified: true,
  protocols: ['responses'],
}

// 只捕获公开的当前行 setter，不读写配置组件的内部 state 或 mutation。
// 这模拟父页面更新选中行，保护真实 Dialog 对 Key 编号的生命周期约束。
// 用 act 调整公开上下文可避免操作被模态框遮蔽的外部测试按钮。
// 第一份配置仍由真实用户点击打开，后续只改变已有弹窗的当前行。
// 捕获类型直接来自公开 hook，Provider 合同变化时由类型检查提醒。
// setter 在组件 effect 中取得，真实渲染完成后才允许切换测试资源。
// 不构造假的 context value，因此 setOpen 与 currentRow 保持项目行为。
// 也不手动重挂 HostingClientConfig；生命周期完全由生产父组件决定。
let selectCurrentKey: ReturnType<typeof useApiKeys>['setCurrentRow']
// 入口按钮只替代列表行菜单的外壳，不重写配置或密钥读取逻辑。
// 用户点击时同时设置行和弹窗类型，与真实列表选择动作一致。
// 实际弹窗的标题、门户节点和关闭逻辑仍由 ApiKeysDialogs 提供。
// 共享 Button 保留键盘与可访问行为，不引入测试专用 DOM 事件捷径。
function ConfigurationEntry() {
  const keys = useApiKeys()
  useEffect(() => {
    selectCurrentKey = keys.setCurrentRow
  }, [keys.setCurrentRow])
  return (
    <>
      <Button
        onClick={() => {
          keys.setCurrentRow(workKey)
          keys.setOpen('client-config')
        }}
      >
        Configure work key
      </Button>
      <ApiKeysDialogs />
    </>
  )
}

// 真实父弹窗同时挂载其他关闭的弹窗，其公共站点状态查询保持真实执行。
// HTTP fixture 因此允许 /api/status，但不允许预取所有 Key 或全局策略。
// 每个场景创建独立缓存，不能把前一场景模型或秘密当作默认值。
// 禁用自动重试，让每次目录读取和明文读取都能对应具体用户动作。
// 配置所需 Router 使用内存历史，不替换 Link、导航或共享弹窗实现。
// 全局测试清理卸载组件，本地清理同时释放 Query 的所有数据。
let client: QueryClient
// staleTime 不控制模型是否验证成功，那是接口字段与真实过滤逻辑的职责。
// mutations 与 queries 的缓存生命周期不同，后面分别检查可见秘密与查询数据。
// 测试结束时不保存任何 fixture 到用户配置文件或真实剪贴板。
// localStorage 的公共站点快照由项目状态 hook 维护，不承载这些密钥。
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  })
})
afterEach(() => client.clear())
// 这是专门的 Key 弹窗装配，不提供任意页面渲染的通用测试框架。
// 根路由不会提前创建配置子组件，只有真实 setOpen 才打开目标 Dialog。
// 没有预置模型缓存，可观察到第一份和第二份 Key 目录的实际请求。
// 路由切换不参与当前场景，避免将卸载页面误当成清理 Key 状态。
function renderConfigurationDialogs() {
  // 没有装配真实 Key 表格，分页、搜索和批量取密属于其他测试范围。
  // 这一边界刻意让全部 Key 请求没有合法 fixture，避免重新引入跨页选择。
  // Provider 仍保存真实当前行，所以弹窗描述和配置目标应保持一致。
  const router = createRouter({
    routeTree: createRootRoute({
      component: () => (
        <ApiKeysProvider>
          <ConfigurationEntry />
        </ApiKeysProvider>
      ),
    }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

// 所有数据都进入真实查询函数和 DTO 校验，不 mock hook 或业务 API 包装。
// 剪贴板由 userEvent 的浏览器边界模拟提供，共享 CopyButton 保持真实。
// 复制出的文本比检查组件源码更直接地保护用户收到的配置和密钥。
// 每次打开下拉均使用可访问角色，避免依赖样式类或内部节点层级。
describe('具体 API Key 的客户端配置', () => {
  // 三个目录记录分别隔离缺省验证、失败验证和协议不匹配三种原因。
  // 若只提供空数组，组件漏掉过滤判断也能通过测试，无法保护合同。
  // 查询成功后仍无合格模型，与网络失败的错误状态是不同产品含义。
  // 本场景不触发实际模型测试，浏览配置不应产生原厂费用。
  it('未验证或没有 Responses 协议的模型不能导出配置或读取明文', async () => {
    const user = userEvent.setup()
    const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/status') return { data: { success: true, data: {} } }
      if (url === '/api/hosting/tokens/11/models') {
        return {
          data: {
            success: true,
            data: {
              models: [
                // 缺省验证状态不能被当作成功，协议匹配也不能绕过验证。
                { id: 'discovered-only', protocols: ['responses'] },
                {
                  id: 'failed-verification',
                  verified: false,
                  protocols: ['responses'],
                },
                // 其他协议的成功记录不能替代 Codex Responses 的实测记录。
                { id: 'chat-only', verified: true, protocols: ['chat'] },
              ],
            },
          },
        }
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    // 不仅隐藏复制按钮，还要保证没有先读取明文再决定是否展示。
    // spy 拒绝全部写请求，误读密钥或隐式测试都会留在调用记录中。
    // 拒绝的请求不能被空错误处理掩盖，最终还要检查调用次数为零。
    const post = vi
      .spyOn(api, 'post')
      .mockRejectedValue(new Error('Unexpected secret read'))
    renderConfigurationDialogs()
    await user.click(
      await screen.findByRole('button', { name: 'Configure work key' })
    )
    // 成功读取空的可导出目录需要明确说明，不用无反馈的空选择器代替。
    // 地址复制属于公开信息，不应因为尚未验证模型而一并隐藏。
    expect(
      await screen.findByText(
        'No model has a verified Responses connection for this key. Test the account before exporting configuration.'
      )
    ).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Copy API address' })
    ).toBeVisible()
    expect(
      screen.queryByText('Copy Codex configuration')
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Reveal selected API key' })
    ).not.toBeInTheDocument()
    // 打开真实下拉验证过滤结果，而非只检查没有默认选中模型。
    // 即使服务端返回了型号，也只有满足两项协议合同的选项才能出现。
    await user.click(
      screen.getByRole('combobox', { name: 'Verified Codex model' })
    )
    // 可访问选项为空说明不可导出的型号没有在交互层残留。
    // 目录中有数据却不可用时仍保留可读解释，不冒充加载中。
    // 此处不限制下拉空状态的具体文案，让共享控件保持独立演进。
    expect(screen.queryAllByRole('option')).toHaveLength(0)
    expect(get.mock.calls.map(([url]) => url).sort()).toEqual([
      '/api/hosting/tokens/11/models',
      '/api/status',
    ])
    expect(post).not.toHaveBeenCalled()
  })

  // 一次流程覆盖配置、显式取密与切换上下文之间的依赖关系。
  // 拆成两个独立挂载的场景会天然清空状态，无法复现跨 Key 残留。
  // 第一次明文不带前缀，第二次已带前缀，覆盖规范化的两条公共行为。
  // 两个 HTTP 地址都用数值 id，不按显示名或数组位置寻找 Key。
  // 返回同名模型的同时保留专属型号，既验证重置也验证目录隔离。
  it('按当前 Key 导出配置，切换 Key 后清除相同型号的旧选择与已读取秘密', async () => {
    const user = userEvent.setup()
    const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/status') return { data: { success: true, data: {} } }
      if (url === '/api/hosting/tokens/11/models') {
        return {
          data: {
            success: true,
            data: { models: [sharedModel, workOnlyModel] },
          },
        }
      }
      if (url === '/api/hosting/tokens/22/models') {
        return {
          data: {
            success: true,
            data: { models: [sharedModel, travelOnlyModel] },
          },
        }
      }
      throw new Error(`Unexpected GET ${url}`)
    })
    // 密钥读取接口使用原有 POST 合同，不伪造一个新的配置导出接口。
    // 每份响应只含当前 Key 的明文，没有批量列表可供前端误选。
    // 除两个预期编号外全部拒绝，不能把编号缺失默认为第一条记录。
    // 成功信封由 fetchTokenKey 与 requireServerSuccess 正常解包。
    const post = vi.spyOn(api, 'post').mockImplementation(async (url) => {
      if (url === '/api/token/11/key') {
        return { data: { success: true, data: { key: 'work-secret' } } }
      }
      if (url === '/api/token/22/key') {
        return { data: { success: true, data: { key: 'sk-travel-secret' } } }
      }
      throw new Error(`Unexpected POST ${url}`)
    })
    renderConfigurationDialogs()
    await user.click(
      await screen.findByRole('button', { name: 'Configure work key' })
    )
    const model = await screen.findByRole('combobox', {
      name: 'Verified Codex model',
    })
    await waitFor(() => expect(model).toBeEnabled())
    // 首次打开不会读取秘密，选择器也不能展示另一个 Key 的专属型号。
    // 只按传入编号查询目录，不需要请求分页 Key 列表再次选择。
    expect(post).not.toHaveBeenCalled()
    await user.click(model)
    expect(
      await screen.findByRole('option', { name: 'work-only' })
    ).toBeVisible()
    expect(
      screen.queryByRole('option', { name: 'travel-only' })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('option', { name: 'shared-verified' }))
    await user.click(screen.getByText('Copy Codex configuration'))
    // 读取真正复制结果，不直接调用组件内部的字符串生成逻辑。
    // 复制按钮具有共享的默认 aria-label，因此按可见内容触发其真实行为。
    // 导出的换行保留实际文本，断言关键协议项而非整份脆弱快照。
    // 环境变量方式允许用户审查配置时不必暴露 Key。
    // 测试不运行生成的配置，也不修改用户本机 Codex 设置。
    // 生成片段是否兼容真实客户端需由独立端到端验收确认。
    // 当前断言只保护模板使用正确公开地址、型号和密钥引用方式。
    const config = await navigator.clipboard.readText()
    // 导出只引用环境变量，既不包含脱敏列表值，也不包含真实密钥。
    // 公开入口取当前站点，不能泄露执行层地址或要求用户直连后台。
    expect(config).toContain('model = "shared-verified"')
    expect(config).toContain(`base_url = "${window.location.origin}/v1"`)
    expect(config).toContain('env_key = "OAK_API_KEY"')
    expect(config).not.toContain('masked-work')
    expect(config).not.toContain('work-secret')
    expect(post).not.toHaveBeenCalled()
    // 明文必须由用户明确触发；复制控件展示的值经过真实前缀规范化。
    // 查询缓存可以保存目录，却不能把秘密塞到配置或目录中。
    await user.click(
      screen.getByRole('button', { name: 'Reveal selected API key' })
    )
    await user.click(await screen.findByText('Copy selected API key'))
    // 这次复制发生在模型选中之后，证明读取结果接入实际共享控件。
    // 只检查按钮出现不足以发现控件复制了空值或脱敏摘要。
    // 第一份秘密的值随后成为切换隔离的观察基线。
    expect(await navigator.clipboard.readText()).toBe('sk-work-secret')
    expect(post.mock.calls.map(([url]) => url)).toEqual(['/api/token/11/key'])
    // 保持同一个真实父弹窗打开，只换当前 Key，不能靠关闭重开掩盖缺少 key。
    // 第二个目录含同型号，此时仍必须要求用户重新选择，不能继承旧秘密。
    // 如果真实父 Dialog 丢失 key 属性，旧 shared-verified 仍匹配新目录。
    // 因此下面的空选择断言会立即失败，不会被目录过滤偶然保护。
    // 不对 queryClient.clear 做手动调用，否则也可能掩盖缓存作用域错误。
    // 切换只影响当前行，open 保持 client-config，父组件没有卸载。
    await act(async () => {
      selectCurrentKey(travelKey)
    })
    await waitFor(() =>
      expect(get).toHaveBeenCalledWith('/api/hosting/tokens/22/models', {
        disableDuplicate: true,
      })
    )
    // 此时仍使用同一个 QueryClient，目录缓存必须按 tokenId 区分。
    // 第一份目录允许留在缓存中，关键是不能成为第二份配置的选项来源。
    const travelModel = screen.getByRole('combobox', {
      name: 'Verified Codex model',
    })
    await waitFor(() => expect(travelModel).toBeEnabled())
    // 等待第二个目录加载结束再观察状态，避免只检查到临时加载画面。
    // 模型控件本身仍存在，空值来自真实组件重新初始化。
    // 此时不能自动读取第二个 Key，仍需用户重新选择并明确取密。
    // 旧剪贴板属于用户设备，此测试检查界面可用秘密而非强制清空剪贴板。
    expect(travelModel).toHaveValue('')
    expect(
      screen.queryByText('Copy Codex configuration')
    ).not.toBeInTheDocument()
    expect(screen.queryByText('Copy selected API key')).not.toBeInTheDocument()
    // 再次选择同名型号后仍无秘密，证明不是暂时隐藏在未选模型后面。
    // 新目录只保留当前 Key 允许的型号，旧 Key 的专属型号必须消失。
    await user.click(travelModel)
    expect(
      await screen.findByRole('option', { name: 'travel-only' })
    ).toBeVisible()
    expect(
      screen.queryByRole('option', { name: 'work-only' })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('option', { name: 'shared-verified' }))
    expect(screen.queryByText('Copy selected API key')).not.toBeInTheDocument()
    expect(post.mock.calls.map(([url]) => url)).toEqual(['/api/token/11/key'])
    await user.click(
      screen.getByRole('button', { name: 'Reveal selected API key' })
    )
    await user.click(await screen.findByText('Copy selected API key'))
    // 已带前缀的第二份秘密不能重复补前缀，更不能复制第一份秘密。
    // 两次显式读取应分别绑定自己的编号，不通过所有 Key 列表寻找目标。
    // 新的复制动作应覆盖测试剪贴板中的旧值，不能沿用先前 mutation 数据。
    // 明文读取次数恰好等于两次点击，不由目录刷新额外触发。
    // 请求集合保留每次调用，不用去重后的结果掩盖重复读取。
    // 不断言两个查询先后顺序，React 可以合法地并行加载公共状态。
    expect(await navigator.clipboard.readText()).toBe('sk-travel-secret')
    expect(post.mock.calls.map(([url]) => url)).toEqual([
      '/api/token/11/key',
      '/api/token/22/key',
    ])
    expect(get.mock.calls.map(([url]) => url).sort()).toEqual([
      '/api/hosting/tokens/11/models',
      '/api/hosting/tokens/22/models',
      '/api/status',
    ])
    // 最后检查查询缓存中不存在秘密，避免未来把 reveal 改成可持久查询。
    // 这个检查只涉及公开缓存数据，不读取组件的私有状态或实现细节。
    // mutation 可在当前弹窗生命周期保留取密结果，关闭清理由实际组件负责。
    // 验证范围限于前端隔离，后端越权与密钥加密需另外的服务端测试。
    expect(
      JSON.stringify(
        client
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data)
      )
    ).not.toContain('secret')
  })
})
