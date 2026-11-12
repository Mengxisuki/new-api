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
import { cleanup, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { checkIsActive } from '@/components/layout/lib/url-utils'
import {
  parseSidebarModulesAdmin,
  serializeSidebarModulesAdmin,
} from '@/features/system-settings/maintenance/config'
import { useAuthStore } from '@/stores/auth-store'

import { useSidebarConfig } from '../use-sidebar-config'
import { useSidebarData } from '../use-sidebar-data'

beforeEach(() => {
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: () => undefined,
    removeItem: () => undefined,
  })
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  useAuthStore.getState().auth.reset()
})

function sidebarFor(admin?: object, user?: object, canConfigure = true, role = 1) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], {
    SidebarModulesAdmin: admin ? JSON.stringify(admin) : '',
  })
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'alice',
    role,
    permissions: { sidebar_settings: canConfigure },
    sidebar_modules: user ? JSON.stringify(user) : '',
  })
  function Wrapper(props: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        {props.children}
      </QueryClientProvider>
    )
  }
  const result = renderHook(
    () => useSidebarConfig(useSidebarData().navGroups),
    { wrapper: Wrapper }
  )
  return result
}

describe('security sidebar visibility', () => {
  it('old configurations show Security & Access immediately after Profile and keep API Keys', () => {
    const { result } = sidebarFor(
      { personal: { enabled: true, personal: true, topup: true } },
      { personal: { enabled: true, personal: true } }
    )
    expect(
      result.current
        .find((group) => group.id === 'personal')
        ?.items.map((item) => item.title)
    ).toEqual(['Account top-ups', 'Wallet', 'Profile', 'Security & Access'])
    expect(
      result.current
        .flatMap((group) => group.items)
        .some((item) => item.title === 'API Keys')
    ).toBe(true)
  })
  it.each([
    [{ personal: { enabled: true, security: false } }, undefined],
    [{ personal: { enabled: false } }, { personal: { security: true } }],
    [undefined, { personal: { enabled: true, security: false } }],
    [undefined, { personal: { enabled: false } }],
  ])(
    'admin or user disablement hides Security & Access (%j, %j)',
    (admin, user) => {
      const { result } = sidebarFor(admin, user)
      expect(
        result.current
          .flatMap((group) => group.items)
          .some((item) => item.title === 'Security & Access')
      ).toBe(false)
    }
  )
  it('users without sidebar configuration permission retain the admin view', () => {
    const { result } = sidebarFor(
      undefined,
      { personal: { security: false } },
      false
    )
    expect(
      result.current
        .flatMap((group) => group.items)
        .some((item) => item.title === 'Security & Access')
    ).toBe(true)
  })
})

describe('audit log sidebar entry', () => {
  it('admin settings default Audit Logs to visible and preserve its independent toggle when saved', () => {
    const config = parseSidebarModulesAdmin(
      '{"console":{"enabled":true,"log":true}}'
    )
    expect(config.console.audit).toBe(true)
    config.console.audit = false
    const saved = parseSidebarModulesAdmin(serializeSidebarModulesAdmin(config))
    const { result } = sidebarFor(saved)
    const titles = result.current
      .flatMap((group) => group.items)
      .map((item) => item.title)
    expect(titles).not.toContain('Audit Logs')
    expect(titles).toContain('Usage Logs')
  })

  it('legacy configurations show a separate Audit Logs link immediately after Usage Logs', () => {
    const { result } = sidebarFor(
      { console: { enabled: true, log: true } },
      { console: { enabled: true, log: true } }
    )
    const items =
      result.current.find((group) => group.id === 'general')?.items ?? []
    const usageIndex = items.findIndex((item) => item.title === 'Usage Logs')
    expect(items[usageIndex + 1]).toMatchObject({
      title: 'Audit Logs',
      url: '/usage-logs/audit',
    })
    const selected = items.filter((item) =>
      checkIsActive('/usage-logs/audit', item)
    )
    expect(selected.map((item) => item.title)).toEqual(['Audit Logs'])
  })

  it.each([
    [{ console: { enabled: true, audit: false } }, undefined],
    [{ console: { enabled: false } }, { console: { audit: true } }],
    [undefined, { console: { enabled: true, audit: false } }],
    [undefined, { console: { enabled: false } }],
  ])(
    'admin and personal visibility rules can hide Audit Logs (%j, %j)',
    (admin, user) => {
      const { result } = sidebarFor(admin, user)
      expect(
        result.current
          .flatMap((group) => group.items)
          .some((item) => item.title === 'Audit Logs')
      ).toBe(false)
    }
  )

  it('hiding Usage Logs does not hide the independently configured Audit Logs entry', () => {
    const { result } = sidebarFor({ console: { enabled: true, log: false } })
    const titles = result.current
      .flatMap((group) => group.items)
      .map((item) => item.title)
    expect(titles).not.toContain('Usage Logs')
    expect(titles).toContain('Audit Logs')
  })
})

// 财务导航按独立资源能力展示，不依靠管理员角色等级顺带开放。
// 本测试只保护导航接线，服务端越权边界由真实权限测试验证。
it('财务账号具有读取能力时获得独立财务分类及开票入口', () => {
  useAuthStore.getState().auth.setUser({
    id: 5,
    username: 'finance',
    role: 5,
    permissions: {
      admin_permissions: { finance: { read: true, write: true } },
    },
  })
  const result = renderHook(() => useSidebarData())
  const finance = result.result.current.navGroups.find(
    (group) => group.id === 'finance'
  )
  expect(finance?.items).toEqual([
    expect.objectContaining({ url: '/finance/credit' }),
    expect.objectContaining({ url: '/finance/invoices' }),
  ])
})

// 被撤销读取权限后，菜单立即消失，不能只检查数字角色等于财务。
it('没有财务读取能力时不显示财务分类', () => {
  useAuthStore.getState().auth.setUser({
    id: 5,
    username: 'finance',
    role: 5,
    permissions: {
      admin_permissions: { finance: { read: false, write: false } },
    },
  })
  const result = renderHook(() => useSidebarData())
  expect(
    result.result.current.navGroups.some((group) => group.id === 'finance')
  ).toBe(false)
})

// 总览复用财务分类的可见性配置；旧配置缺少 overview 时保持默认可见。
it.each([
  [undefined, undefined, true],
  [{ finance: { enabled: true, overview: false } }, undefined, false],
  [{ finance: { enabled: false } }, undefined, false],
  [undefined, { finance: { overview: false } }, false],
])('财务总览遵循站点和个人的侧栏开关 (%j, %j)', (admin, user, visible) => {
  const { result } = sidebarFor(admin, user, true, 100)
  expect(result.current.flatMap((group) => group.items).some(
    (item) => 'url' in item && item.url === '/finance/overview'
  )).toBe(visible)
})

// 财务总览是独立权限：办理权限和总览权限各自决定自己的入口，互不带出。
it.each([
  {
    name: '只有总览权限时财务分类只出现总览入口',
    permissions: {
      finance: { read: false, write: false },
      finance_overview: { read: true },
    },
    urls: ['/finance/overview'],
  },
  {
    name: '同时具有两种权限时总览排在财务分类第一项',
    permissions: {
      finance: { read: true, write: true },
      finance_overview: { read: true },
    },
    urls: ['/finance/overview', '/finance/credit', '/finance/invoices'],
  },
])('$name', ({ permissions, urls }) => {
  useAuthStore.getState().auth.setUser({
    id: 5,
    username: 'finance',
    role: 5,
    permissions: { admin_permissions: permissions },
  })
  const result = renderHook(() => useSidebarData())
  const finance = result.result.current.navGroups.find(
    (group) => group.id === 'finance'
  )
  expect(finance?.items.map((item) => ('url' in item ? item.url : ''))).toEqual(
    urls
  )
})

// 业务员有独立工作区，不能因低于管理员等级而丢失入口。
// 商品维护与退款不属于业务员默认能力，导航不能顺带展示管理入口。
it('业务员只显示代充值订单管理入口', () => {
  useAuthStore
    .getState()
    .auth.setUser({
      id: 3,
      username: 'sales',
      role: 3,
      permissions: {
        admin_permissions: {
          recharge_orders: { read: true, write: true, reveal: true },
        },
      },
    })
  const { result } = renderHook(() => useSidebarData())
  const group = result.current.navGroups.find(
    (group) => group.id === 'recharge'
  )
  expect(group?.items).toEqual([
    expect.objectContaining({ url: '/recharge-orders' }),
  ])
  expect(result.current.navGroups.some((group) => group.id === 'finance')).toBe(
    false
  )
})
