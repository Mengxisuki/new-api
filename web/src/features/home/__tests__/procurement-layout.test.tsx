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
import { readFileSync } from 'node:fs'
import path from 'node:path'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, test } from 'vitest'

import { STATUS_QUERY_KEY, type StatusData } from '@/lib/status-query'

import { OakContact } from '../components/oak-contact'

// 采购展示测试约束：
// 采购信息是公开系统状态的一部分，不代表登录微信的配置。
// 不能通过登录二维码或默认联系人值掩盖采购资料为空的情况。
// 无配置时整个区域消失，符合后台字段留空隐藏的约定。
// 姓名和微信号可以独立配置，图片也可以单独存在。
// 这里选取组合资料、全空资料和危险图片三条关键输入路径。
// 外部状态通过真实 QueryClient 注入，保留 useStatus 的订阅路径。
// 新建缓存默认为新鲜数据，因此不会误访问真实后台。
// 每个用例结束后清除缓存，防止其他场景读到旧联系人。
// 不模拟 OakContact 的局部状态或返回结构。
// 图片失败由真实元素接收 error 事件，等价于资源加载失败。
// 失败反馈必须可见，不能只在控制台留下信息。
// 文字资料仍然可用，不应跟随图片一起被隐藏。
// 危险 URL 是外部输入回归，独立于浏览器是否会执行图片脚本。
// 即使浏览器自身拦截协议，组件也不应渲染不受支持的图片。
// 平板断点来自旧版官网已验证的布局修正。
// 这条样式断言只保护列方向和联系面板分隔边界。
// 它不承诺 jsdom 可以计算真实网格、宽度或滚动结果。
// 完整 CSS 快照会把无关颜色和排版细节绑在测试上。
// 窄屏视觉仍需由实际浏览器验证；源码规则只能提供回归保护。
// 如果后续断点整体调整，应先确认真实设备行为再更新此测试。
// 接口缓存输入不包含凭据或用户隐私，均为专用测试资料。
// 新增字段时应根据公开显示契约增加场景，不按字段数量重复用例。
const clients: QueryClient[] = []

afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
})

// 使用共享 status 缓存作为外部配置入口，不模拟被测组件。
// 页面与其他消费者共享查询，测试不允许额外发起网络请求。
function renderContact(status: StatusData) {
  const client = new QueryClient()
  clients.push(client)
  client.setQueryData(STATUS_QUERY_KEY, status)

  render(
    <QueryClientProvider client={client}>
      <OakContact />
    </QueryClientProvider>
  )
}

// 姓名和微信号同时存在时必须显示两者，不能只展示二维码。
test('已配置采购资料时展示联系人姓名与微信号', () => {
  renderContact({
    procurement_contact_name: 'Research purchasing',
    procurement_wechat_id: 'lab_support',
  })
  expect(screen.getByText('Research purchasing')).toBeVisible()
  expect(screen.getByText('lab_support')).toBeVisible()
})

// 平板端必须先变为纵向布局，避免左侧开票说明挤压联系资料。
// 此处断言稳定的布局契约，不比较完整样式或浏览器像素。
test('到达平板断点时采购支持区域纵向堆叠', () => {
  const css = readFileSync(path.resolve('src/styles/oak-home.css'), 'utf8')
  const tabletRules = css.slice(
    css.indexOf('@media (max-width: 1050px)'),
    css.indexOf('@media (max-width: 760px)')
  )

  expect(tabletRules).toMatch(
    /\.oak-procurement-support\s*{[^}]*flex-direction:\s*column;/s
  )
  expect(tabletRules).toMatch(
    /\.oak-contact\s*{[^}]*border-left:\s*1px solid var\(--oak-line\);[^}]*border-top:\s*0;/s
  )
})

// 联系资料可全部关闭，不能留下空白的采购联系卡片。
test('未配置采购资料时隐藏联系区域', () => {
  renderContact({})
  expect(screen.queryByText('Lab procurement contact')).not.toBeInTheDocument()
})

// 图片加载失败必须给出可见反馈，同时保留可用的文字联系方式。
// 使用 DOM error 事件覆盖真实图片元素的失败分支，不模拟组件内部状态。
test('二维码加载失败时显示提示并保留微信号', () => {
  renderContact({
    procurement_wechat_id: 'lab_support',
    procurement_wechat_qr_url: '/qr.png',
  })
  fireEvent.error(screen.getByAltText('WeChat QR code for lab procurement'))
  expect(screen.getByRole('status')).toHaveTextContent('QR image unavailable')
  expect(screen.getByText('lab_support')).toBeVisible()
})

// 状态接口属于外部输入，不能把脚本协议交给图片元素。
// 无其他资料时，危险图片地址也不能撑出一张空联系卡。
test('状态返回危险二维码地址时不渲染图片或空联系区域', () => {
  renderContact({ procurement_wechat_qr_url: 'javascript:alert(1)' })
  expect(screen.queryByRole('img')).not.toBeInTheDocument()
  expect(screen.queryByText('Lab procurement contact')).not.toBeInTheDocument()
})
