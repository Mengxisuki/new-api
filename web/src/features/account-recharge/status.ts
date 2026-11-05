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
import type { Order } from './api'

// 用户只看到平台订单进度；
// 待领取与正在办理统一显示处理中。
// 支付状态优先，
// 退款期间不能仍展示可提交资料或可继续办理。
// 此函数只决定文案，
// 服务器状态机才是权限和资金的依据。
// 状态映射只面向用户呈现，
// 不反向解析文案决定接口动作。
// 待付款优先于待资料，
// 创建订单后不能让用户误以为已经付费。
// 退款处理中优先于旧办理状态，
// 避免显示仍可办理的错误提示。
// 运营队列保留待领取语义，
// 买家看到统一处理中。
// 完成和异常来自人工确认，
// 不表示平台已查询供应商接口。
// 新增服务器状态时应扩展这里与七语言词条，
// 不能显示内部枚举。
export function orderStatus(order: Order, operations = false): string {
  if (order.payment_status === 'pending') return 'Awaiting payment'
  if (order.payment_status === 'refunding') return 'Refund in progress'
  if (order.payment_status === 'refunded') return 'Refunded'
  if (order.status === 'pending') {
    return operations ? 'Awaiting claim' : 'Order processing'
  }
  if (order.status === 'processing') return 'Order processing'
  if (order.status === 'awaiting_info') return 'Submit account information'
  if (order.status === 'action_required') {
    return 'Additional information required'
  }
  if (order.status === 'completed') return 'Completed'
  if (order.status === 'exception') return 'Order needs attention'
  return 'Cancelled'
}

// 事件动作保留稳定协议值，
// 展示时映射为已有七语言词条。
// 不将数据库的 claim、reveal 等内部枚举直接显示给办理人员。
// 用户资料与内部备注属于不同动作，
// 历史中不能混为办理结果。
// 退款批准和实际确认分开，
// 便于审查资金动作先后顺序。
// 未知新事件暂保留原动作名，
// 便于定位而不是伪造已完成描述。
export const rechargeActionLabels: Record<string, string> = {
  payment: 'Payment',
  credit_charge: 'On account', // 授信放行不能显示成已收到现金。
  submit_info: 'Submit account information',
  claim: 'Claim order',
  assign: 'Assign order',
  reveal: 'View account information',
  need_info: 'Request additional information',
  complete: 'Mark completed',
  exception: 'Report an issue',
  request_refund: 'Request refund',
  reject_refund: 'Reject refund request',
  note: 'Save internal note',
  refund_approve: 'Approve refund',
  refund: 'Confirm refund',
}

// 列表与详情共用付款来源名称，挂帐不能沿用旧的微信兜底文案。
// 返回翻译键，由调用组件按当前界面语言渲染。
// 官方及网关渠道仍保持现有微信付款描述。
export function rechargePaymentLabel(
  order: Pick<Order, 'payment_provider'>
): string {
  if (order.payment_provider === 'credit') return 'On account'
  if (order.payment_provider === 'wallet') return 'Platform balance'
  return 'WeChat Pay'
}
