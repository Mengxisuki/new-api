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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type { InvoiceList, InvoiceProfile } from './types'

export const INVOICE_PROFILE_QUERY_KEY = ['invoice-profile'] as const // 个人资料与付款快捷配置共享内存缓存。

// 发票资料仅经过已有认证客户端读取，不持久化到浏览器存储。
// 显式关闭请求去重，使快捷保存后能立即重新确认服务器资料。
export async function getInvoiceProfile(): Promise<InvoiceProfile | null> {
  const response = await api.get('/api/user/invoice-profile', {
    disableDuplicate: true, // 主动查看必须访问服务器，不复用旧的在途读取。
  })
  return requireServerSuccess(response.data).data ?? null
}

// 只提交抬头字段，不允许表单覆盖服务端用户归属或更新时间。
// 业务失败继续使用统一错误解析，不能把失败保存当作配置完成。
export async function saveInvoiceProfile(
  profile: InvoiceProfile
): Promise<InvoiceProfile> {
  const response = await api.put('/api/user/invoice-profile', {
    title_type: profile.title_type, // 抬头类型决定税号的必填条件。
    title: profile.title, // 用户可编辑内容由服务端再次检查长度。
    tax_id: profile.tax_id, // 税号仍作为文本提交，保留原格式交给服务端规范化。
    email: profile.email, // 发票交付联系方式不改动用户登录身份。
    address: profile.address,
    phone: profile.phone,
    bank_name: profile.bank_name,
    bank_account: profile.bank_account, // 银行账号不经过数值化，避免精度及前导零损失。
  })
  return requireServerSuccess(response.data).data
}

// 本人申请由服务端按登录身份筛选，客户端不能传 user_id 切换账户。
export async function getUserInvoices(
  page = 1,
  size = 10
): Promise<InvoiceList> {
  const response = await api.get('/api/user/invoices', {
    params: { page, size }, // 分页在服务器执行，不把全部敏感申请下载到浏览器。
    disableDuplicate: true, // 主动查看必须访问服务器，不复用旧的在途读取。
  })
  return requireServerSuccess(response.data).data
}

// 文件读取保留会话与服务端归属校验，下载前不会拼接公开存储地址。
// 文件名只决定下载展示，不影响服务器读取哪一份申请附件。
// 返回附件的类型由服务端文件检查确定，不能让图片伪装成 PDF。
// 方法调用只携带申请 ID，所有者由认证会话解析。
// Blob 只留在本次下载生命周期，完成后释放浏览器对象地址。
export async function downloadUserInvoice(
  id: number,
  invoiceNumber: string
): Promise<void> {
  const response = await api.get(`/api/user/invoices/${id}/file`, {
    responseType: 'blob',
    disableDuplicate: true, // 主动查看必须访问服务器，不复用旧的在途读取。
  })
  const blob: Blob = response.data // 后端成功才返回附件，错误业务响应可能仍是 JSON Blob。
  if (!blob.size || blob.type.includes('json')) {
    throw new Error('Failed to download invoice')
  }
  let extension = 'pdf' // 支持的非图片附件只有 PDF，不使用用户抬头拼接扩展名。
  if (blob.type === 'image/png') extension = 'png'
  if (blob.type === 'image/jpeg') extension = 'jpg'
  const url = URL.createObjectURL(blob)
  try {
    const link = document.createElement('a')
    link.href = url // 仅浏览器本地临时地址，不暴露服务器文件路径。
    link.download = invoiceNumber
      ? `invoice-${invoiceNumber}.${extension}`
      : `invoice-${id}.${extension}`
    link.click() // 下载来自用户主动操作，列表加载不会获取文件内容。
  } finally {
    URL.revokeObjectURL(url) // 不论点击是否抛错都释放包含敏感附件的引用。
  }
}
