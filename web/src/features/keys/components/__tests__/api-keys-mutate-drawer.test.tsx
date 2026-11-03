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
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import type { ApiKey } from '../../types'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { api } = await import('@/lib/api')
const { ApiKeysProvider } = await import('../api-keys-provider')
const { ApiKeysMutateDrawer } = await import('../api-keys-mutate-drawer')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = {
  get: ApiMethod
  post: ApiMethod
}
type RenderedDrawer = {
  queryClient: InstanceType<typeof QueryClient>
}

const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get
const originalPost = apiClient.post
let renderedDrawer: RenderedDrawer | null = null

function installApiFixtures(createdPayloads: Array<Record<string, unknown>>) {
  apiClient.get = async (url) => {
    switch (url) {
      case '/api/status':
        return { data: { data: { default_use_auto_group: true } } }
      case '/api/user/models':
        return { data: { success: true, data: [] } }
      case '/api/user/self/groups':
        return {
          data: {
            success: true,
            data: {
              auto: { desc: 'Automatic routing', ratio: 'auto' },
              default: { desc: 'Standard access', ratio: 1 },
              vip: { desc: 'Priority access', ratio: 2 },
            },
          },
        }
      case '/api/token/auto-groups':
        return {
          data: {
            success: true,
            data: { groups: ['vip', 'default'], max_count: 3 },
          },
        }
      default:
        throw new Error(`Unexpected GET ${url}`)
    }
  }
  apiClient.post = async (url, data) => {
    expect(url).toBe('/api/token/')
    expect(data && typeof data === 'object').toBeTruthy()
    createdPayloads.push(data as Record<string, unknown>)
    return { data: { success: true, data: {} } }
  }
}

async function renderCreateDrawer(currentRow?: ApiKey): Promise<void> {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const freshAt = Date.now() + 60_000
  queryClient.setQueryData(
    ['status'],
    { default_use_auto_group: true },
    { updatedAt: freshAt }
  )
  queryClient.setQueryData(
    ['user-models'],
    { success: true, data: [] },
    { updatedAt: freshAt }
  )
  queryClient.setQueryData(
    ['user-groups'],
    {
      success: true,
      data: {
        auto: { desc: 'Automatic routing', ratio: 'auto' },
        default: { desc: 'Standard access', ratio: 1 },
        vip: { desc: 'Priority access', ratio: 2 },
      },
    },
    { updatedAt: freshAt }
  )
  queryClient.setQueryData(
    ['token-auto-groups'],
    {
      success: true,
      data: { groups: ['vip', 'default'], max_count: 3 },
    },
    { updatedAt: freshAt }
  )
  renderedDrawer = { queryClient }

  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <ApiKeysProvider>
          <ApiKeysMutateDrawer
            open
            currentRow={currentRow}
            onOpenChange={() => undefined}
          />
        </ApiKeysProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
  await waitFor(
    () => {
      const saveButton = findButton('Save changes', false)
      expect(saveButton).toBeEnabled()
    },
    { timeout: 1500 }
  )
}

function findButton(text: string, required: true): HTMLButtonElement
function findButton(text: string, required: false): HTMLButtonElement | null
function findButton(text: string, required = true): HTMLButtonElement | null {
  const button = screen
    .queryAllByRole<HTMLButtonElement>('button')
    .find((candidate) => candidate.textContent?.includes(text))
  if (required && !button) {
    throw new Error(`Expected button containing "${text}"`)
  }
  return button ?? null
}

function getControlByLabel(labelText: 'Name' | 'Quantity'): HTMLInputElement
function getControlByLabel(labelText: 'Group'): HTMLButtonElement
function getControlByLabel(labelText: 'Auto group order'): HTMLElement
function getControlByLabel(labelText: string): HTMLElement {
  const label = [...document.querySelectorAll<HTMLLabelElement>('label')].find(
    (candidate) => candidate.textContent?.trim() === labelText
  )
  if (!label) {
    throw new Error(`Expected label "${labelText}"`)
  }

  const control =
    label.control ??
    label
      .closest('[data-slot="form-item"]')
      ?.querySelector<HTMLElement>(
        '[data-slot="form-control"], input, textarea, button[role="combobox"], [role="group"]'
      )
  if (!control) {
    throw new Error(`Expected control for label "${labelText}"`)
  }
  return control
}

function changeInput(input: HTMLInputElement, value: string): void {
  fireEvent.input(input, { target: { value } })
}

function selectComboboxOption(
  trigger: HTMLButtonElement,
  optionDescription: string
): void {
  fireEvent.click(trigger)
  const option = [
    ...document.querySelectorAll<HTMLElement>('[data-slot="command-item"]'),
  ].find((candidate) => candidate.textContent?.includes(optionDescription))
  if (!option) {
    throw new Error(`Expected option containing "${optionDescription}"`)
  }
  fireEvent.click(option)
}

afterEach(async () => {
  await i18n.changeLanguage('en')
  apiClient.get = originalGet
  apiClient.post = originalPost
  localStorage.clear()
  if (renderedDrawer) {
    renderedDrawer.queryClient.clear()
    renderedDrawer = null
  }
})

describe('API keys mutate drawer Auto group integration', () => {
  test('inherits the root Auto order and sends an empty override for every batch-created key', async () => {
    const createdPayloads: Array<Record<string, unknown>> = []
    installApiFixtures(createdPayloads)
    await renderCreateDrawer()

    const groupTrigger = getControlByLabel('Group')
    expect(groupTrigger.textContent?.includes('auto')).toBe(true)
    expect(
      document.body.textContent?.includes(
        'Using the complete global Auto order (2 groups)'
      )
    ).toBe(true)
    expect(
      [
        ...document.querySelectorAll('[data-slot="global-auto-order-name"]'),
      ].map((item) => item.textContent)
    ).toEqual(['vip', 'default'])
    expect(findButton('Restore global Auto', true).disabled).toBe(true)

    changeInput(getControlByLabel('Name'), 'batch')
    changeInput(getControlByLabel('Quantity'), '2')
    fireEvent.click(findButton('Save changes', true))
    await waitFor(() => expect(createdPayloads).toHaveLength(2))

    expect(createdPayloads.length).toBe(2)
    expect(createdPayloads[0]?.name).toBe('batch')
    for (const payload of createdPayloads) {
      expect(payload.group).toBe('auto')
      expect(payload.auto_groups).toEqual([])
      expect(payload.cross_group_retry).toBe(true)
      // 批量创建必须给每个 Key 同时保存独立的默认来源顺序。
      // 批量不是一个多 Key 事务，本例检查每个请求都带完整策略。
      // 默认钱包优先来自用户的明确选择，不由账户余额决定。
      // 账号池没有 account_id，后端在调用时读取当前本人账号。
      // 订阅没有具体套餐 ID，实际符合条件的套餐由计费服务选择。
      expect(payload.resource_policy).toEqual({
        revision: 0,
        sources: [
          { kind: 'wallet', enabled: true },
          { kind: 'subscription', enabled: true },
          { kind: 'hosting_pool', enabled: true },
        ],
      })
    }
  })

  test('preserves an unsaved custom order and mode after Auto to ordinary to Auto changes', async () => {
    const createdPayloads: Array<Record<string, unknown>> = []
    installApiFixtures(createdPayloads)
    await renderCreateDrawer()

    const autoOrderControl = getControlByLabel('Auto group order')
    const addGroupTrigger = autoOrderControl.querySelector<HTMLButtonElement>(
      'button[role="combobox"]'
    )
    if (!addGroupTrigger) {
      throw new Error('Expected Auto group order combobox')
    }
    selectComboboxOption(addGroupTrigger, 'Priority access')

    expect(
      document.querySelector('button[aria-label="Remove vip"]')
    ).toBeTruthy()
    expect(document.body.textContent?.includes('1 / 3 groups selected')).toBe(
      true
    )
    expect(findButton('Restore global Auto', true).disabled).toBe(false)

    const groupTrigger = getControlByLabel('Group')
    selectComboboxOption(groupTrigger, 'Standard access')
    expect(document.querySelector('button[aria-label="Remove vip"]')).toBe(null)
    selectComboboxOption(groupTrigger, 'Automatic routing')

    expect(
      document.querySelector('button[aria-label="Remove vip"]')
    ).toBeTruthy()
    expect(document.body.textContent?.includes('1 / 3 groups selected')).toBe(
      true
    )
    expect(findButton('Restore global Auto', true).disabled).toBe(false)

    changeInput(getControlByLabel('Name'), 'custom')
    fireEvent.click(findButton('Save changes', true))
    await waitFor(() => expect(createdPayloads).toHaveLength(1))
    expect(createdPayloads[0]?.auto_groups).toEqual(['vip'])
  })
})

describe('API key expiry across editing sessions', () => {
  test.each(['close', 'save'] as const)(
    'keeps an uncached key non-expiring after %s of a key with an expiry',
    async (finish) => {
      installApiFixtures([])
      const first: ApiKey = {
        id: 1,
        name: 'expiring',
        resource_policy: {
          revision: 0,
          legacy: true,
          sources: [{ kind: 'wallet', enabled: true }],
        },
        key: '',
        status: 1,
        remain_quota: 0,
        used_quota: 0,
        unlimited_quota: true,
        expired_time: 2000000000,
        created_time: 1,
        accessed_time: 0,
        group: 'default',
        auto_groups: [],
        cross_group_retry: false,
        model_limits_enabled: false,
        model_limits: '',
        allow_ips: '',
        allow_user_quota_query: true,
      }
      const second = { ...first, id: 2, name: 'non-expiring', expired_time: -1 }
      let resolveDetail!: (value: { data: unknown }) => void
      const detail = new Promise<{ data: unknown }>((resolve) => {
        resolveDetail = resolve
      })
      const getFixture = apiClient.get
      const get = vi.spyOn(apiClient, 'get').mockImplementation((url) => {
        if (url === '/api/token/1') {
          return Promise.resolve({ data: { success: true, data: first } })
        }
        if (url === '/api/token/2') return detail
        return getFixture(url)
      })
      const put = vi
        .spyOn(api, 'put')
        .mockResolvedValue({ data: { success: true } })
      const queryClient = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      })
      renderedDrawer = { queryClient }
      const close = vi.fn()
      const tree = (currentRow: ApiKey, open: boolean) => (
        <QueryClientProvider client={queryClient}>
          <I18nextProvider i18n={i18n}>
            <ApiKeysProvider>
              <ApiKeysMutateDrawer
                open={open}
                currentRow={currentRow}
                onOpenChange={close}
              />
            </ApiKeysProvider>
          </I18nextProvider>
        </QueryClientProvider>
      )
      const view = render(tree(first, true))
      await waitFor(() =>
        expect(findButton('Save changes', true)).toBeEnabled()
      )
      await waitFor(() => expect(screen.getByLabelText('Time')).toBeEnabled())

      if (finish === 'save') {
        fireEvent.click(findButton('Save changes', true))
        await waitFor(() => expect(put).toHaveBeenCalled())
        expect(put.mock.calls[0][1]).toMatchObject({
          id: 1,
          expired_time: 2000000000,
        })
      } else {
        fireEvent.click(screen.getAllByRole('button', { name: 'Close' })[0])
      }
      await waitFor(() => expect(close.mock.calls[0]?.[0]).toBe(false))
      view.rerender(tree(first, false))
      await waitFor(() =>
        expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      )
      view.rerender(tree(second, true))
      await waitFor(() => expect(get).toHaveBeenCalledWith('/api/token/2'))
      expect(findButton('Save changes', true)).toBeDisabled()
      await act(async () =>
        resolveDetail({ data: { success: true, data: second } })
      )
      await waitFor(() =>
        expect(findButton('Save changes', true)).toBeEnabled()
      )
      expect(
        screen.getByRole('button', { name: 'Never expires' })
      ).toBeVisible()
      expect(screen.getByLabelText('Time')).toBeDisabled()
      fireEvent.click(findButton('Save changes', true))
      await waitFor(() =>
        expect(put).toHaveBeenLastCalledWith(
          '/api/token/',
          expect.objectContaining({ id: 2, expired_time: -1 })
        )
      )
    }
  )

  test.each(['Never', 'Clear'])(
    'submits a non-expiring key when its date is cleared with %s',
    async (action) => {
      const payloads: Array<Record<string, unknown>> = []
      installApiFixtures(payloads)
      await renderCreateDrawer()
      changeInput(getControlByLabel('Name'), 'no-expiry')
      fireEvent.click(screen.getByRole('button', { name: '1 Month' }))
      expect(screen.getByLabelText('Time')).toBeEnabled()
      fireEvent.click(screen.getByRole('button', { name: action }))
      expect(
        screen.getByRole('button', { name: 'Never expires' })
      ).toBeVisible()
      expect(screen.getByLabelText('Time')).toBeDisabled()
      fireEvent.click(findButton('Save changes', true))
      await waitFor(() => expect(payloads).toHaveLength(1))
      expect(payloads[0]?.expired_time).toBe(-1)
    }
  )
})

describe('API keys mutate drawer user quota query switch', () => {
  test('sends false for a new key after the switch, which starts on, is turned off', async () => {
    const createdPayloads: Array<Record<string, unknown>> = []
    installApiFixtures(createdPayloads)
    await renderCreateDrawer()

    fireEvent.click(findButton('Advanced Settings', true))
    const quotaQuerySwitch = await screen.findByRole('switch', {
      name: 'Allow querying user quota',
    })
    expect(quotaQuerySwitch).toBeChecked()
    fireEvent.click(quotaQuerySwitch)
    expect(quotaQuerySwitch).not.toBeChecked()

    changeInput(getControlByLabel('Name'), 'no-user-quota')
    fireEvent.click(findButton('Save changes', true))
    await waitFor(() => expect(createdPayloads).toHaveLength(1))
    expect(createdPayloads[0]?.allow_user_quota_query).toBe(false)
  })
})

// 排序通过真实表单提交验证，避免只测试拖动组件内部状态。
// 首次新增 Key 不依赖后端已存在的策略行，版本必须从零开始。
// 默认三类来源都启用，排序不能改变 enabled 或隐式删除账号池。
// 创建与编辑共用同一个抽屉组件，下面分别验证两种保存语义。
// 键盘操作保护无法使用拖拽的用户，不直接调用内部 onMove。
// API mock 只接收最终提交，策略逻辑仍来自生产表单。
// 初始化仍等待分组和全局 Auto 配置，不能先用不完整详情提交。
// 横向溢出契约限制在排序栏，长来源名称不会撑宽整个抽屉。
// 不用拖动动画的像素作为断言，避免浏览器时间差影响顺序测试。
// 真实指针拖动另由本地浏览器检查，此处保护键盘替代路径。
// 断言 payload 是为了检查原子保存，不能出现第二次策略请求。
// 名称输入用现有辅助函数，不把其他字段变化混入排序动作。
describe('API key resource priority', () => {
  test('moves subscription before wallet with the keyboard and submits that order', async () => {
    const payloads: Array<Record<string, unknown>> = []
    installApiFixtures(payloads)
    await renderCreateDrawer()
    const handle = screen.getByRole('button', {
      name: 'Drag Subscription quota to reorder',
    })
    expect(screen.getByRole('list', { name: 'Resource priority' })).toHaveClass(
      'overflow-x-auto'
    )
    fireEvent.keyDown(handle, { key: 'ArrowLeft' })
    changeInput(getControlByLabel('Name'), 'subscription-first')
    fireEvent.click(findButton('Save changes', true))
    await waitFor(() => expect(payloads).toHaveLength(1))
    expect(payloads[0].resource_policy).toEqual({
      revision: 0,
      sources: [
        { kind: 'subscription', enabled: true },
        { kind: 'wallet', enabled: true },
        { kind: 'hosting_pool', enabled: true },
      ],
    })
  })
})

// 详情查询是网络边界，其他表单、排序和缓存均运行真实实现。
// 旧配置中的两个单账号不在相邻位置，专门防止按类型强行聚合。
// 钱包初始禁用，保护“普通调整不自动授权扣余额”的合同。
// 订阅初始启用，用于检查移动时只改变相邻位置。
// Key 本身无有效期和模型限制，排除与资源排序无关的校验失败。
// 测试值不是可调用的真实凭据，密钥明文接口不会在本流程触发。
// id 是服务端资源身份，name 改变不影响策略作用域。
// 来源里的账号编号同样只是测试数据库资源标识。
// 夹具包含交错的单账号来源，防止新版界面把旧 Key 静默折叠成账号池。
// revision 与来源属于同一份草稿，不由保存时的新缓存临时决定。
// 下列测试刻意刷新详情，模拟另一个窗口已经更新同一 Key。
const editingKey: ApiKey = {
  id: 41,
  name: 'existing',
  key: '',
  status: 1,
  remain_quota: 0,
  used_quota: 0,
  unlimited_quota: true,
  expired_time: -1,
  created_time: 1,
  accessed_time: 0,
  group: 'default',
  auto_groups: [],
  cross_group_retry: false,
  model_limits_enabled: false,
  model_limits: '',
  allow_ips: '',
  allow_user_quota_query: true,
  resource_policy: {
    revision: 3,
    legacy: false,
    sources: [
      { kind: 'hosting', account_id: 7, enabled: true },
      { kind: 'wallet', enabled: false },
      { kind: 'hosting', account_id: 9, enabled: true },
      { kind: 'subscription', enabled: true },
    ],
  },
}

// 仅替换 HTTP 边界，后端的归属和冲突检测由对应 Go 公共入口回归覆盖。
// 这里关注抽屉是否保存用户看到的合同，而非构造另一个策略实现。
// HTTP 查询只按传入 Key 的 ID 返回详情，不能借另一个 Key 填充表单。
// 创建接口的 fixture 不会被编辑调用，编辑保存使用已有 PUT 合同。
// 保存参数仍含完整 Key 表单字段，来源字段只是可选的原子扩展。
// 成功响应不返回明文密钥，测试不需要为了排序揭示秘密。
// 新增策略创建和旧策略修改都由后端版本规则最终仲裁。
// 原有分组及站点配置继续经过公共 fixture，初始化流程不会被绕过。
// 保存响应模拟单次原子成功，不在测试里补发独立 policy 请求。
// 外层清理恢复 api mock 和 QueryClient，防止跨用例泄漏版本状态。
// 同一 helper 用于有来源和空策略，保证不是两种不同的表单实现。
async function renderEditingKey(key: ApiKey) {
  installApiFixtures([])
  const getFixture = apiClient.get
  apiClient.get = async (url) =>
    url === `/api/token/${key.id}`
      ? { data: { success: true, data: key } }
      : getFixture(url)
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  await renderCreateDrawer(key)
  return put
}

describe('Existing API key resource authorization', () => {
  const initialPolicy = editingKey.resource_policy
  if (!initialPolicy) throw new Error('Expected editing fixture policy')
  // 普通改名不升级授权，包括明确拒绝所有资源的旧合同。
  // 不能为了修表单校验而要求用户启用钱包。
  // 非空分支也保护禁用钱包以及交错账号的完整旧配置。
  // 空列表表示明确拒绝所有来源，不等于未加载完成。
  // 两个分支都只改名称，提交必须省略整个 resource_policy 字段。
  // 不仅检查 sources 相等，因为重写同一策略也会改变并发版本。
  // 此处不会触发“恢复三类资源”，它是用户主动扩大授权的独立动作。
  // 普通编辑路径不把 legacy 标志作为需要自动迁移的信号。
  // 服务端是否仍采用历史计费合同由策略行存在性决定。
  // 前端省略字段保护这一边界，不发送 revision=0 的空壳替代。
  // 若无改动保存也创建了账号池，用户的付费选择将无法保持。
  test.each([{ sources: initialPolicy.sources }, { sources: [] }])(
    'preserves an unchanged existing policy when only the name changes',
    async ({ sources }) => {
      const put = await renderEditingKey({
        ...editingKey,
        resource_policy: { revision: 3, legacy: false, sources },
      })
      changeInput(getControlByLabel('Name'), 'renamed')
      fireEvent.click(findButton('Save changes', true))
      await waitFor(() => expect(put).toHaveBeenCalled())
      expect(put.mock.calls[0][1]).not.toHaveProperty('resource_policy')
    }
  )

  // 只改变位置，禁用钱包和单账号身份必须原样提交。
  // 焦点应跟随稳定资源标识，不能排序一次就落到另一账号。
  // 后台 refetch 只更新缓存，不授予旧草稿覆盖新版本的权限。
  // 用户编辑版本三，后台拿到版本四后仍应提交版本三。
  // 服务端因此能返回冲突，而不是把旧草稿冒充成最新授权。
  // 手柄 focus 在真实 DOM 上验证，排序不能靠重新挂载清掉焦点。
  // 预期数组手工写明结果，不复用生产排序函数计算期望。
  // 钱包的 disabled 与两个账号的 ID 均应保留，不能只比较种类。
  // 测试保存仍通过正常按钮提交，以覆盖 dirtyFields 的订阅时机。
  test('saves a reordered draft with its original revision after a background refresh', async () => {
    const put = await renderEditingKey(editingKey)
    const handle = screen.getByRole('button', {
      name: 'Drag Subscription quota to reorder',
    })
    handle.focus()
    fireEvent.keyDown(handle, { key: 'ArrowLeft' })
    expect(handle).toHaveFocus()
    await act(async () => {
      if (!renderedDrawer) throw new Error('Expected rendered drawer')
      renderedDrawer.queryClient.setQueryData(['api-key', editingKey.id], {
        success: true,
        data: {
          ...editingKey,
          resource_policy: { ...editingKey.resource_policy, revision: 4 },
        },
      })
    })
    fireEvent.click(findButton('Save changes', true))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][1]).toMatchObject({
      resource_policy: {
        revision: 3,
        sources: [
          { kind: 'hosting', account_id: 7, enabled: true },
          { kind: 'wallet', enabled: false },
          { kind: 'subscription', enabled: true },
          { kind: 'hosting', account_id: 9, enabled: true },
        ],
      },
    })
  })

  // 关闭最后一种资源是有效的拒绝全部配置，不自动启用收费来源。
  // 单次原子请求应包含禁用值，不能只更新页面上的开关。
  // 这也是编辑模式脏字段检测的独立路径，不依赖创建模式兜底。
  // 禁用与移除语义不同，提交仍包含同一来源及 enabled=false。
  // 显式版本三保持不变，空资金授权仍参与并发冲突检测。
  // 若前端自动增加订阅或账号池，精确 sources 断言会失败。
  test('saves all resources disabled without enabling a paid fallback', async () => {
    const put = await renderEditingKey({
      ...editingKey,
      resource_policy: {
        revision: 3,
        legacy: false,
        sources: [{ kind: 'wallet', enabled: true }],
      },
    })
    fireEvent.click(
      screen.getByRole('switch', { name: 'Enable Pay as you go' })
    )
    fireEvent.click(findButton('Save changes', true))
    await waitFor(() => expect(put).toHaveBeenCalled())
    expect(put.mock.calls[0][1]).toMatchObject({
      resource_policy: {
        revision: 3,
        sources: [{ kind: 'wallet', enabled: false }],
      },
    })
  })
})

// 使用真实语言资源验证新词条的位置，而不是用英文键作为中文假翻译。
// 切换不重新创建 QueryClient，不以重挂载掩盖语言订阅问题。
// 已存在的来源身份和顺序不会因为标签翻译而改变。
// 中文手柄和开关都要有完整可访问名称，读屏不依赖可见标签猜测。
// translation 是实际加载的命名空间，根级同名键不能让本例通过。
// 页面语言变化不会修改来源 kind，保存协议始终使用稳定英文标识。
// 开关在切换后仍启用，避免翻译导致控件被错误重新初始化。
// 校验同时覆盖钱包名称和其余两个来源，不能只修第一项中文。
// 生产构建不加载这些 fixture，测试数据只存在于 Vitest 进程。
// 最后回到英语由统一清理执行，不能污染后续测试的查询标签。
test('updates resource labels and accessible controls when switching to Chinese', async () => {
  installApiFixtures([])
  await renderCreateDrawer()
  const zh = await import('@/i18n/locales/zh.json')
  i18n.addResourceBundle('zh', 'translation', zh.default.translation)
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(
    screen.getByRole('button', { name: '拖动 按量计费 以重新排序' })
  ).toBeVisible()
  expect(screen.getByRole('switch', { name: '启用订阅额度' })).toBeChecked()
  expect(screen.getByRole('switch', { name: '启用自有账号' })).toBeChecked()
})

// 从用户视角填写可视化映射，提交必须与 Key 的其余配置一起发送。
// 源别名允许自定义，目标字段使用 Key 语义而不是渠道上游语义。
// 两个输入都保留可访问名称，以便键盘和辅助设备定位。
// 测试从新增行按钮开始，覆盖编辑器与表单字段的真实接线。
// 不 mock ModelMappingEditor，否则无法发现 onChange 没有写入表单。
// 目录候选为空，仍应允许用户输入自定义别名与目标。
// 请求捕获只在 API 边界，后端校验另由控制器测试验证。
// 用户输入的是普通字符串，不依赖 JSON 模式才能保存。
// 点击保存后等待真实表单提交，而不是读取内部 React state。
// 断言解析后的规则对象，避免无意义绑定 JSON 缩进风格。
test('creates a key with a custom model mapping from the visual editor', async () => {
  const payloads: Array<Record<string, unknown>> = []
  installApiFixtures(payloads)
  await renderCreateDrawer()
  changeInput(getControlByLabel('Name'), 'mapped-key')
  fireEvent.click(screen.getByRole('button', { name: 'Add Mapping' }))
  fireEvent.change(
    screen.getByRole('combobox', { name: 'Request Model Name' }),
    { target: { value: 'my-alias' } }
  )
  fireEvent.change(screen.getByRole('combobox', { name: 'Target Model' }), {
    target: { value: 'gpt-4' },
  })
  fireEvent.click(findButton('Save changes', true))
  await waitFor(() => expect(payloads).toHaveLength(1))
  expect(JSON.parse(payloads[0].model_mapping as string)).toEqual({
    'my-alias': 'gpt-4',
  })
})
