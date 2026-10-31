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

import type { InvoiceList } from './types'

// 财务列表始终由后端分页与授权，不在浏览器拉取全部用户的敏感抬头。
// 手动刷新必须发起新请求，不能借请求去重缓存返回旧开票状态。
export async function getFinanceInvoices(
  page: number,
  status: string,
  keyword: string
): Promise<InvoiceList> {
  // 查询只含页码和筛选，不允许从浏览器替换财务接口的授权身份。
  const response = await api.get('/api/finance/invoices', {
    params: { page, size: 10, status, keyword }, // 页大小与管理表的翻页边界保持一致。
    disableDuplicate: true, // 主动刷新必须取新状态，不能返回上次完成开票前的响应。
  })
  return requireServerSuccess(response.data).data // HTTP成功不代表业务成功，统一解析服务端拒绝。
}

// 状态修改只提交管理意见，金额与抬头快照不允许被财务页面覆盖。
// issued 由受保护附件上传完成，不让浏览器伪造一个已开票状态。
export async function updateFinanceInvoice(
  id: number,
  status: 'processing' | 'rejected',
  remark: string,
  invoiceNumber: string
): Promise<void> {
  // 白名单字段只表达财务处理意见，历史抬头和金额快照保持不可编辑。
  const response = await api.put(`/api/finance/invoices/${id}/status`, {
    status,
    remark,
    invoice_number: invoiceNumber,
  })
  requireServerSuccess(response.data)
}

// 发票号码与文件一同上传，后端核对已付款状态和内容类型。
// 前端不发送用户 ID、实付金额或存储路径，避免越权替换开票申请。
export async function uploadFinanceInvoice(
  id: number,
  invoiceNumber: string,
  file: File,
  remark: string
): Promise<void> {
  const data = new FormData() // 复用认证客户端的multipart支持，不额外暴露附件存储入口。
  data.append('file', file) // 内容类型和字节上限最终仍由服务器检查。
  data.append('invoice_number', invoiceNumber) // 号码和文件一起提交才能成为完整已开票记录。
  data.append('remark', remark) // 保留上传前填写的处理备注，不能被空值无意覆盖。
  const response = await api.post(`/api/finance/invoices/${id}/file`, data, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  requireServerSuccess(response.data)
}

// 下载继续通过认证客户端，不能暴露可公开访问的发票附件地址。
// 下载名使用返回 MIME 类型，图片附件不能被误保存成 PDF。
export async function downloadFinanceInvoice(id: number): Promise<void> {
  const response = await api.get(`/api/finance/invoices/${id}/file`, {
    responseType: 'blob',
    disableDuplicate: true, // 主动刷新必须取新状态，不能返回上次完成开票前的响应。
  })
  const blob: Blob = response.data // 附件只存在本次下载内存，不写浏览器持久缓存。
  if (!blob.size || blob.type.includes('json')) {
    // 业务错误JSON不能伪装成一个可下载PDF。
    throw new Error('Failed to download invoice')
  }
  const extensions: Record<string, string> = {
    'application/pdf': 'pdf',
    'image/png': 'png',
    'image/jpeg': 'jpg',
  }
  const extension = extensions[blob.type] // 使用真实响应类型，图片不会被错误保存成PDF。
  if (!extension) {
    // 未知MIME是失败，不对可执行或HTML响应采用文件名回退。
    throw new Error('Failed to download invoice')
  }
  const url = URL.createObjectURL(blob) // 私有Blob地址不能作为永久公开链接保存。
  try {
    const link = document.createElement('a')
    link.href = url
    link.download = `invoice-${id}.${extension}` // 文件名不包含税号、抬头或真实邮箱。
    link.click()
  } finally {
    URL.revokeObjectURL(url) // 文件只在本次下载生命周期保留，不写 localStorage。
  }
}
