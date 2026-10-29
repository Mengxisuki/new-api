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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { SystemInfoSection } from '../system-info-section'

// 本文件验证整页接线和保存边界，通用上传失败恢复由采购上传用例保护。
// 预览相对路径来自上传响应，不依赖浏览器对象 URL。
// 父级始终保留旧配置 fixture，用于检验组件不会依赖父级立即回传新地址。
// 因此能发现单独组件测试漏掉的父表单 reset 回归。
// Logo 的接口调用是文件上传；普通系统设置仍是键值更新。
// 两种保存语义不能互相替代，也不能通过只修改 UI 文案掩盖。
// 文件输入的可访问名称与图片替代文本一致，便于用户识别操作对象。
// 预览只有在请求成功后才更新，测试显式等待新 src。
// 保存名称后再次检查预览，避免只覆盖上传那一刻的短暂正确状态。
// 请求数量断言针对禁止重复写 Logo 的接口契约，不针对组件内部调用。
// 其他站点字段保持最小空配置，减少无关校验对目标流程的干扰。
// Portal 容器由测试显式创建，清理不能仅依赖组件卸载。
// 使用公开角色查找保存入口，内部组件拆分不影响该行为契约。
// 两类图片使用同一控件，因此无需再复制一套类型、大小和失败用例。
// 后端独立验证图片内容、尺寸、持久化和删除；这里保留真实前端组合。
// 只隔离 HTTP 请求，不模拟上传 API 封装、图片控件或表单 Hook。
// 初始 Logo 为旧地址，可以发现上传后被父级草稿覆盖的回归。
// 服务端返回站内相对地址，可以发现遗留 URL 校验阻塞整页保存。
// 用户先编辑名称再上传，覆盖已有普通草稿时的实际使用顺序。
// 最后从页头保存，确认上传既不提交旧 Logo，也不丢失名称改动。
// 操作按钮通过真实 Portal 挂载，不绕过系统设置的操作入口。
// 内存路由保留导航保护所需上下文，不访问真实用户会话。
// 请求无重试，避免网络错误被延迟或自动重发掩盖。
// 所有异步断言等待具体可见结果，不依赖固定延迟。
// 测试不创建账户、不运行模型请求，也不写真实站点配置。
const clients: QueryClient[] = []

// 查询缓存与 Portal 都由本模块创建，结束时完整释放。
// HTTP spy 由 Vitest 的 restoreMocks 自动恢复，避免影响其他模块。
afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  document.querySelector('[data-logo-test-actions]')?.remove()
  localStorage.clear()
})

// 从真实系统信息区进入，保护文件控件接线与父表单保存。
// 其他设置仍走普通 PUT，徽标不能再被父表单当 URL 写回。
// 上传后的相对路径必须通过整页校验，不能阻塞站点名称保存。
test('系统信息使用上传徽标且上传后仍可保存其他字段', async () => {
  const user = userEvent.setup()
  // 只替换持久化边界，普通设置仍走真实 API 函数。
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
  // 上传接口返回的 URL 是唯一成功预览来源。
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { url: '/api/status/logo?v=new' } },
  })
  // 独立缓存保证用例不读取前一个测试的站点配置。
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  // 正式页面的操作按钮通过 Portal 放到页头，这里保留真实组合。
  const actions = document.createElement('div')
  actions.setAttribute('data-logo-test-actions', '')
  document.body.append(actions)
  // 保留真实路由，FormNavigationGuard 可以正常订阅离站状态。
  const router = createRouter({
    history: createMemoryHistory({ initialEntries: ['/'] }),
    routeTree: createRootRoute({
      component: () => (
        <SettingsPageProvider actionsContainer={actions}>
          <SystemInfoSection
            defaultValues={{
              SystemName: 'Oak', // 名称草稿变化是上传后仍应允许保存的公共行为。
              ServerAddress: '',
              TaskPublicAddress: '',
              Logo: '/old-logo.png', // 旧配置快照不会因 HTTP spy 自动更新。
              Footer: '',
              About: '',
              HomePageContent: '',
              general_setting: { docs_link: '' },
              legal: {},
            }}
          />
        </SettingsPageProvider>
      ),
    }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  // 文件控件必须从真实系统信息区渲染出来，而非单独挂载未接线组件。
  const input = await screen.findByLabelText('Logo image')
  // 用户选择本地文件，页面不再提供手工 URL 配置。
  expect(input).toHaveAttribute('type', 'file')
  expect(
    screen.queryByRole('textbox', { name: 'Logo URL' })
  ).not.toBeInTheDocument()
  // 先编辑普通字段，再上传，不能只在初始空闲表单中验证上传。
  await user.clear(screen.getByRole('textbox', { name: 'System Name' }))
  await user.type(
    screen.getByRole('textbox', { name: 'System Name' }),
    'Oak Lab'
  )
  // 文件正文不需要在浏览器测试中解码，真实内容约束由后端覆盖。
  await user.upload(input, new File(['png'], 'logo.png', { type: 'image/png' }))
  // 先确认上传结果，再保存名称，避免请求未完成导致假阳性。
  await waitFor(() =>
    expect(screen.getByAltText('Logo image')).toHaveAttribute(
      'src',
      '/api/status/logo?v=new'
    )
  )
  // 上传应使用专用文件接口，而非将文件或地址塞进通用设置 PUT。
  expect(api.post).toHaveBeenCalledWith(
    '/api/option/logo',
    expect.any(FormData)
  )
  // 保存按钮执行真实父表单解析、变更收集和请求流程。
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'SystemName',
      value: 'Oak Lab',
    })
  )
  // 唯一 PUT 只能是名称；徽标已经由上传接口持久化。
  expect(api.put).toHaveBeenCalledTimes(1)
  // 普通保存完成后仍显示新图，保护已复现的旧地址回写问题。
  expect(screen.getByAltText('Logo image')).toHaveAttribute(
    'src',
    '/api/status/logo?v=new'
  )
})
