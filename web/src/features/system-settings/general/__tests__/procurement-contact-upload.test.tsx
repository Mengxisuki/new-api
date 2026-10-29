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
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { Form } from '@/components/ui/form'
import { api } from '@/lib/api'

import { ProcurementContactFields } from '../procurement-contact-fields'
import type { ProcurementContactValues } from '../procurement-contact-schema'

// 每个测试创建独立缓存，防止上传后刷新污染其他测试。
// 采购二维码表单测试边界：
// 该表单将公开联系人二维码与登录微信二维码分离。
// 图片上传和删除由独立接口立即保存，不能只更新浏览器预览。
// 因此测试保留真实 API 封装，仅替换共享 HTTP 客户端请求。
// 不能模拟 uploadSettingsImage 后再宣称载荷接线正确。
// POST 的 FormData 必须包含约定的 file 字段。
// 文件对象来自真实用户输入，不能在请求层替换成任意字符串。
// 成功结果中的 URL 是服务端返回值，不由前端猜测生成。
// 预览显示新地址是上传完成的可观察结果。
// 选择入口变为替换入口，表示用户可以继续管理已上传图片。
// 上传结束后的按钮必须恢复可操作状态。
// 旧配置以显式 fixture 提供，不读取真实系统状态。
// 表单实例由 React Hook Form 创建，保留真实 watch 与 setValue 行为。
// 页面必须跟随字段更新，不允许通过直接操作图片 DOM 假造成功。
// 查询客户端仅提供刷新缓存所需的真实上下文。
// 每个测试使用独立缓存，避免跨用例共享上传结果。
// 本组不挂载其他查询消费者，因此失效操作不触发无关网络。
// 不对 invalidateQueries 次数作无用户意义的断言。
// 缓存真正刷新后的首页展示由首页模块和接口测试配合保护。
// 失败场景必须保留上一次已保存的图片。
// HTTP 成功但 success 为 false 仍属于业务失败。
// 这样能够捕获遗漏 requireServerSuccess 检查的回归。
// 服务端具体错误应当通过现有错误处理器展示。
// 不能仅显示一个通用失败字符串而丢失可定位原因。
// 删除请求拒绝则模拟网络边界失败。
// 删除失败不能把旧预览移除，避免误导用户认为公开图片已删除。
// 失败后也必须解除忙碌状态，以便用户重试。
// 错误提示通过现有 Toast 观察，无需模拟组件内部状态。
// 删除成功应移除预览，并重新提供选择图片入口。
// 前端大小校验发生在发送请求前，节省不必要的上传。
// 超过一兆的 fixture 不必在内存中分配完整大图片。
// 这里只替换外部 File 的 size 元数据以触发明确边界。
// 后端另有真实图片数据测试，本组不重复解码规则。
// 文件选择控件的 accept 只是用户体验过滤，不能替代代码校验。
// 因此类型拒绝用例绕过 accept，直接提交不支持的 File。
// 该输入仍经过组件的真实 onChange 处理器。
// 不支持的类型不得到达共享 HTTP 客户端。
// 前端类型判断与后端二进制格式校验承担不同职责。
// 此处不声称文件的 MIME 字段能够证明内容安全。
// 所有 fixture 都是专用测试数据，不含真实二维码或联系人隐私。
// 异步成功和失败均等待明确界面或 Toast 结果。
// 不通过固定延迟或函数调用耗时推断完成状态。
// 用例结束时清理查询客户端和存储。
// Vitest 自动恢复 HTTP spy，防止影响其他模块请求。
// 后续新增图片管理能力应扩展可见行为用例，而非快照复杂 DOM。
// 本组不覆盖系统设置整页导航或通用表单库自身实现。
const clients: QueryClient[] = []

// 表单使用真实 React Hook Form；仅 HTTP 请求在边界处替换。
// 旧图片作为外部输入，保护替换失败时不能清空已保存配置。
function Fixture(props: { imageURL?: string }) {
  const form = useForm<ProcurementContactValues>({
    defaultValues: {
      ProcurementContactName: '',
      ProcurementWeChatID: '',
      ProcurementWeChatQRCodeURL: props.imageURL ?? '',
    },
  })
  return (
    <Form {...form}>
      <ProcurementContactFields />
    </Form>
  )
}

// 不模拟业务上传函数，确保 URL、表单载荷和业务失败检查均走真路径。
// 禁止重试使失败状态确定，不依赖网络或固定等待时间。
function renderFields(imageURL?: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <Fixture imageURL={imageURL} />
    </QueryClientProvider>
  )
  return screen.getByLabelText(
    'WeChat QR code for lab procurement'
  ) as HTMLInputElement
}

beforeEach(() => {
  // 使用可观察 HTTP 返回值驱动表单成功状态。
  vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: { url: '/api/status/procurement-wechat-qr?v=new' },
    },
  })
  // 删除与上传是独立请求，不能共用成功结果推断。
  vi.spyOn(api, 'delete').mockResolvedValue({ data: { success: true } })
})

afterEach(() => {
  // 清除异步查询和测试存储，允许各用例单独运行。
  clients.splice(0).forEach((client) => client.clear())
  localStorage.clear()
})

test('上传有效图片后通过真实API封装发送文件并展示新预览', async () => {
  const user = userEvent.setup()
  const input = renderFields()
  const image = new File(['png-data'], 'wechat.png', { type: 'image/png' })
  // 文件选择是用户输入；图片内容校验由后端测试保护。
  await user.upload(input, image)
  const preview = await screen.findByAltText(
    'WeChat QR code for lab procurement'
  )
  expect(preview).toHaveAttribute(
    'src',
    '/api/status/procurement-wechat-qr?v=new'
  )
  // 不只检查调用次数，还检查真正发送的 multipart 字段。
  expect(api.post).toHaveBeenCalledWith(
    '/api/option/procurement-wechat-qr',
    expect.any(FormData)
  )
  expect((vi.mocked(api.post).mock.calls[0][1] as FormData).get('file')).toBe(
    image
  )
  // 预览更新后提供替换入口，不留下上传中的禁用状态。
  expect(screen.getByRole('button', { name: 'Replace' })).toBeEnabled()
})

test('删除已配置二维码成功后移除预览并恢复选择入口', async () => {
  const user = userEvent.setup()
  renderFields('/old-qr.png')
  // 删除必须实际经过管理 API，不能仅清理本地表单。
  await user.click(screen.getByRole('button', { name: 'Remove' }))
  await waitFor(() =>
    expect(
      screen.queryByAltText('WeChat QR code for lab procurement')
    ).not.toBeInTheDocument()
  )
  expect(api.delete).toHaveBeenCalledWith('/api/option/procurement-wechat-qr')
  expect(screen.getByRole('button', { name: 'Choose file' })).toBeEnabled()
})

test('上传返回业务失败时保留旧预览并展示服务端错误', async () => {
  const errorToast = vi.spyOn(toast, 'error')
  vi.mocked(api.post).mockResolvedValue({
    data: { success: false, message: 'Invalid image content' },
  })
  const user = userEvent.setup()
  const input = renderFields('/old-qr.png')
  // 业务失败不是网络异常，也必须被识别为失败。
  await user.upload(
    input,
    new File(['bad'], 'wechat.png', { type: 'image/png' })
  )
  await waitFor(() =>
    expect(errorToast).toHaveBeenCalledWith('Invalid image content')
  )
  // 确保失败不会把原配置替换成空值或假成功的 URL。
  expect(
    screen.getByAltText('WeChat QR code for lab procurement')
  ).toHaveAttribute('src', '/old-qr.png')
  expect(screen.getByRole('button', { name: 'Replace' })).toBeEnabled()
})

test('删除请求失败时保留旧预览且允许重新操作', async () => {
  const errorToast = vi.spyOn(toast, 'error')
  vi.mocked(api.delete).mockRejectedValue(new Error('Deletion unavailable'))
  const user = userEvent.setup()
  renderFields('/old-qr.png')
  // 网络失败与业务失败都不能被表现为已经删除。
  await user.click(screen.getByRole('button', { name: 'Remove' }))
  await waitFor(() =>
    expect(errorToast).toHaveBeenCalledWith('Deletion unavailable')
  )
  expect(
    screen.getByAltText('WeChat QR code for lab procurement')
  ).toHaveAttribute('src', '/old-qr.png')
  // finally 必须恢复可操作状态，否则用户无法重试。
  expect(screen.getByRole('button', { name: 'Remove' })).toBeEnabled()
})

test.each([
  ['超出一兆', 'image/png', 1024 * 1024 + 1, 'Image must be 1 MB or smaller.'],
  ['非支持类型', 'image/svg+xml', 10, 'Choose a PNG, JPEG or WebP image.'],
])('选择%s图片时在请求前拒绝文件', (_name, type, size, message) => {
  const errorToast = vi.spyOn(toast, 'error')
  const input = renderFields()
  const image = new File(['x'], 'image', { type })
  Object.defineProperty(image, 'size', { value: size })
  // 绕过选择器的 accept 过滤，验证应用自身仍执行类型校验。
  fireEvent.change(input, { target: { files: [image] } })
  expect(api.post).not.toHaveBeenCalled()
  expect(errorToast).toHaveBeenCalledWith(message)
})
