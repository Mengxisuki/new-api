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
import { z } from 'zod'

import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

// 接口边界负责校验服务端摘要的完整性。
// 页面不能把字段缺失解释为尚未退款。
// 退款记录为null才表示尚未建立退款意图，
// 空对象和未知状态都必须报解析错误。
// 原支付状态使用历史系统的开放字符串，
// 新增退款状态则采用明确的闭集枚举。
// 这样旧支付方式仍可查看，
// 但未知退款结果不会被错误展示为成功。
// 金额字段在这里不做汇率换算，
// 本地退款整数分与订单展示金额保持区分。
// 服务端返回失败时立即抛出业务错误，
// 仅有HTTP成功不能让页面关闭确认框。
// 提交只发送交易身份和管理员输入的原因，
// 不发送页面显示的用户编号或金额。
// 恢复通过已有退款对象确定，
// 调用方不能自行生成另一个商户退款号。
// 恢复时服务端继续沿用冻结原因，
// 表单输入不能改变已经提交过的微信参数。
// 请求禁用自动授权重放，
// 避免会话恢复机制盲目重复外部资金操作。
// 查询禁止重复请求合并，
// 管理员刷新必须取得当前退款状态。
// 全部接口沿用公共Axios客户端，
// 认证、统一错误通知和会话恢复遵循原项目合同。

// 仅接收订单安全投影，不包含支付载荷与代充值资料。
// 退款金额为人民币整数分，原单金额保留渠道记录值。
const refundSchema = z.object({
  refund_no: z.string(),
  amount_cents: z.number().int(),
  status: z.enum(['prepared', 'processing', 'succeeded', 'closed', 'abnormal']),
  reason: z.string(),
  actor_id: z.number().int(),
  provider_ref: z.string(),
})
const orderSchema = z.object({
  id: z.number().int(),
  module: z.enum(['topup', 'subscription', 'recharge']),
  user_id: z.number().int(),
  trade_no: z.string(),
  money: z.number(),
  payment_provider: z.string(),
  payment_method: z.string(),
  status: z.string(),
  created_at: z.number(),
  paid_at: z.number(),
  refund_block: z.string(),
  refund: refundSchema.nullable(),
})
export type PaymentOrder = z.infer<typeof orderSchema>
export type OrderModule = PaymentOrder['module']

// 三业务独立分页，过滤条件随请求传递，不下载全量账本。
// 业务失败抛错，不能用空列表掩盖服务端拒绝。
export async function getPaymentOrders(
  module: OrderModule,
  page: number,
  keyword: string,
  status: string
) {
  const response = await api.get('/api/orders', {
    params: { module, page, keyword, status },
    disableDuplicate: true,
  })
  return z
    .object({ items: z.array(orderSchema), total: z.number() })
    .parse(requireServerSuccess(response.data).data)
}

// 首次只提交原因；恢复明确resume，不重复创建退款意图。
// 金额、商户和退款号由原单确定，浏览器不能覆盖。
// 自动认证恢复不重放资金动作，失败后由管理员查看原单再恢复。
export async function refundPaymentOrder(order: PaymentOrder, reason: string) {
  const response = await api.post(
    '/api/orders/refund',
    {
      module: order.module,
      trade_no: order.trade_no,
      reason: reason.trim(),
      resume: order.refund !== null,
    },
    { singleUseAuthorization: true }
  )
  return refundSchema.parse(requireServerSuccess(response.data).data)
}
