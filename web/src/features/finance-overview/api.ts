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

// 财务总览的接口合同：金额一律是人民币整数分，额度是平台内部单位。
// 在边界用 zod 完整解析，字段缺失直接报错，不用零值伪装成“没有收入”。

// 一个上海自然日内各业务线的实收；day 是该日零点的 Unix 秒。
const dailySchema = z.object({
  day: z.number().int(),
  topup_cents: z.number().int(),
  subscription_cents: z.number().int(),
  recharge_cents: z.number().int(),
  credit_settled_cents: z.number().int(),
})
export type FinanceDaily = z.infer<typeof dailySchema>

// 欠款用户的名称是当前值，用户被删除后可能为空串，展示时需同时显示编号。
const debtorSchema = z.object({
  user_id: z.number().int(),
  username: z.string(),
  display_name: z.string(),
  cents: z.number().int(),
  count: z.number().int(),
})

const overviewSchema = z.object({
  start: z.number().int(),
  end: z.number().int(),
  topup_cents: z.number().int(),
  subscription_cents: z.number().int(),
  recharge_cents: z.number().int(),
  credit_settled_cents: z.number().int(), // 挂帐在财务结清时才转入实收。
  received_cents: z.number().int(),
  refund_cents: z.number().int(),
  net_cents: z.number().int(),
  invoice_fee_cents: z.number().int(), // 已包含在实收里，仅作说明。
  daily: z.array(dailySchema),
  channels: z.array(
    z.object({
      channel: z.string(),
      cents: z.number().int(),
      count: z.number().int(),
    })
  ),
  // 其他网关单列订单存储值，可能是额度面额而非实收，不能与人民币相加。
  other_gateways: z.array(
    z.object({
      provider: z.string(),
      money: z.number(),
      count: z.number().int(),
    })
  ),
  // 除 new_cents 属于查询时段外，挂帐数字都是当前时点快照。
  credit: z.object({
    outstanding_cents: z.number().int(),
    outstanding_count: z.number().int(),
    limit_cents: z.number().int(),
    used_cents: z.number().int(),
    new_cents: z.number().int(),
    aging30_cents: z.number().int(),
    aging60_cents: z.number().int(),
    aging_over_cents: z.number().int(),
    overdue_count: z.number().int(),
    top_debtors: z.array(debtorSchema),
  }),
  wallet_balance_quota: z.number(),
  consumed_quota: z.number(),
  // 看板统计关闭时没有消耗数据，界面必须显示“不可用”而不是零。
  consumption_available: z.boolean(),
  todo: z.object({
    pending_invoices: z.number().int(),
    pending_refunds: z.number().int(),
  }),
})
export type FinanceOverview = z.infer<typeof overviewSchema>

// 查询 [start, end) 时段，时间为 Unix 秒；范围由调用方按上海自然日算好。
// 业务失败（包括无权限）由 requireServerSuccess 抛出，不能当成空数据。
export async function getFinanceOverview(start: number, end: number) {
  const response = await api.get('/api/finance/overview', {
    params: { start, end },
    disableDuplicate: true,
  })
  return overviewSchema.parse(requireServerSuccess(response.data).data)
}
