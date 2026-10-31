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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { FinanceInvoices } from '../admin-index'

// 发票管理页通过真实QueryClient、表格及详情组件执行，不替换生产UI。
// 网络边界仅返回虚构交易，不连接商户、税务服务或真实用户文件。
// 财务数字角色并不自动代表可写，fixture显式给出资源能力。
// 已付款、未付款、只读和上传分别保护不同的业务约束。
// 列表金额是实付，发票附加费不能被误认为平台余额赠送。
// 只检查服务端调用与用户可见结果，不复写后端收费公式。
// 回归文件不模拟hasPermission，因此角色读取错误会真实暴露。
// 每个场景都初始化缓存与身份，不能继承上一项的办理权限。
// 上传的PDF内容仅是表单fixture，不作为真实税务文件或安全扫描结论。
// 查询过滤和后端授权仍由各自集成测试覆盖，本文件聚焦页面接线。
// 下方显式金额允许发现将基础支付和附加费混为一项的展示错误。
// 文件号与订单号故意不同，避免两个标识被客户端误用。
// 测试真实页面、权限读取和操作，HTTP 边界不连接实际税务或商户服务。
// 已付款、未付款与只读权限是管理入口必须保护的不同边界。
const clients: QueryClient[] = []
const invoice = {
  id: 12, // 详情、状态和文件请求必须使用同一申请ID。
  user_id: 8, // 申请属于另一个用户，财务查看依据专用权限而非本人身份。
  trade_no: 'invoice-order', // 交易号不能被当作上传文件路径。
  business_type: 'topup', // 人民币充值与订阅都可开票，此例使用充值。
  profile: {
    title_type: 'business', // 企业抬头需要保留税号快照。
    title: 'Example Lab', // 所有抬头均为虚构，不引用真实科研机构。
    tax_id: 'TEST-TAX', // 详情公开给财务，而非出现在匿名首页。
    email: 'lab@example.com', // 不触发真实邮件发送。
    address: '',
    phone: '',
    bank_name: '',
    bank_account: '',
  },
  base_amount: 100, // 优惠后的基础收款与最终实收分别保存。
  fee_rate: 6, // 百分数6表示6%，不是小数0.06。
  fee_amount: 6, // 附加费明细用于财务核对。
  pay_amount: 106, // 页面不能把实付106解释成平台额度106。
  currency: 'CNY', // 站点以后切换余额单位也不能重标这笔收款币种。
  status: 'pending', // 待开票表示支付已确认，与pending_payment明确区分。
  invoice_number: '',
  remark: '',
  has_file: false, // 没有文件时不能出现虚假的已开票下载。
  created_at: 1700000000,
  updated_at: 1700000000,
  issued_at: 0,
}

function renderFinance(write: boolean, status = 'pending') {
  useAuthStore.getState().auth.setUser({
    // 通过公开store建立真实页面读取的身份。
    id: 5,
    username: 'finance',
    role: 5, // 财务低于普通管理员，不能依赖管理员等级获得权限。
    permissions: { admin_permissions: { finance: { read: true, write } } },
  })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { items: [{ ...invoice, status }], total: 1, page: 1, size: 10 },
    },
  })
  const client = new QueryClient({
    // 每个场景拥有独立缓存，不共享敏感抬头资料。
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client) // 清理阶段统一释放后台查询，避免测试互相影响。
  render(
    <QueryClientProvider client={client}>
      <FinanceInvoices />
    </QueryClientProvider>
  )
}

afterEach(() => {
  clients.splice(0).forEach((client) => client.clear()) // 每项场景完成后释放缓存中的虚构抬头资料。
  vi.restoreAllMocks() // 后续场景不能继承上传成功或固定列表响应。
  useAuthStore.getState().auth.reset('complete') // 不把财务能力泄漏给之后的普通用户测试。
})

// 待开票申请经真实 HTTP 操作推进，不能仅修改浏览器中的状态文字。
test('财务打开待开票申请后可开始处理并刷新列表', async () => {
  const user = userEvent.setup()
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  renderFinance(true) // 显式允许写操作，仍需申请处于可办理状态。
  await user.click(
    await screen.findByRole('button', { name: 'Review invoice' })
  )
  expect(screen.getByText('TEST-TAX')).toBeVisible() // 读取的是下单冻结的税号。
  await user.click(screen.getByRole('button', { name: 'Start processing' }))
  await waitFor(() =>
    expect(put).toHaveBeenCalledWith('/api/finance/invoices/12/status', {
      status: 'processing',
      remark: '',
      invoice_number: '',
    })
  )
  expect(api.get).toHaveBeenCalledWith(
    '/api/finance/invoices',
    expect.objectContaining({
      params: expect.objectContaining({ page: 1, size: 10 }),
      disableDuplicate: true,
    })
  )
  await waitFor(() => expect(api.get).toHaveBeenCalledTimes(2)) // 状态提交后重读，不能只验证初次打开的GET。
})

// 只读角色仍能查资料，但不能通过页面得到开票和状态写入入口。
test('只读财务查看申请时不提供开票写入按钮', async () => {
  const user = userEvent.setup()
  renderFinance(false) // 只有读取权限的财务仍然可核对抬头。
  await user.click(
    await screen.findByRole('button', { name: 'Review invoice' })
  )
  expect(
    screen.queryByRole('button', { name: 'Start processing' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Upload invoice' })
  ).not.toBeInTheDocument()
})

// 尚未支付的申请只能查询，不能提前处理或上传文件。
test('未付款申请只能查看且不能上传发票', async () => {
  const user = userEvent.setup()
  renderFinance(true, 'pending_payment') // 即使具备写权限也不能提前开未支付订单。
  await user.click(
    await screen.findByRole('button', { name: 'Review invoice' })
  )
  expect(
    screen.queryByRole('button', { name: 'Start processing' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Upload invoice' })
  ).not.toBeInTheDocument()
})

// 上传动作必须携带真实文件与发票号码，不能只调用状态接口冒充开票。
// 虚构PDF仅用于浏览器表单边界，不发送给任何税务或商户系统。
test('财务填写号码并选择文件后上传发票且保留处理备注', async () => {
  const user = userEvent.setup()
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  renderFinance(true) // 显式允许写操作，仍需申请处于可办理状态。
  await user.click(
    await screen.findByRole('button', { name: 'Review invoice' })
  )
  expect(screen.getByRole('button', { name: 'Upload invoice' })).toBeDisabled()
  await user.type(
    screen.getByRole('textbox', { name: 'Invoice number' }),
    'INV-2026-1'
  )
  await user.type(
    screen.getByRole('textbox', { name: 'Finance remark' }),
    'Checked order'
  )
  await user.upload(
    screen.getByLabelText('Invoice file'),
    new File(['%PDF-1.4 test'], 'invoice.pdf', { type: 'application/pdf' })
  )
  await user.click(screen.getByRole('button', { name: 'Upload invoice' }))
  await waitFor(() => expect(post).toHaveBeenCalledTimes(1))
  expect(post.mock.calls[0][0]).toBe('/api/finance/invoices/12/file')
  const data = post.mock.calls[0][1] as FormData // 检查真实multipart合同，不能仅凭成功提示证明附件已提交。
  expect(data.get('invoice_number')).toBe('INV-2026-1') // 号码与附件必须一并交给后端。
  expect(data.get('remark')).toBe('Checked order') // 上传不能丢掉已填写的财务处理意见。
  expect(data.get('file')).toBeInstanceOf(File) // 真实文件不是可被伪造的外部URL字符串。
})
