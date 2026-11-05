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

// 只接受服务端约定的三个主题；未知主题不能被静默当作全量订阅。
export const EVENT_TOPICS = [
  'wallet.topup_succeeded',
  'billing.charge_anomaly',
  'recharge.info_submitted',
] as const
export const topicLabels = {
  'wallet.topup_succeeded': 'Wallet top-up succeeded',
  'billing.charge_anomaly': 'Unusual charge detected',
  'recharge.info_submitted': 'Recharge information submitted',
} as const
// 消费者只保存订阅和进度，连接凭据独立管理。
// 金额字段保留内部整数额度，不把站点展示汇率写回业务配置。
const settingsSchema = z.object({
  enabled: z.boolean(), // 暂停仅影响投递，事件仍然持久保存。
  retention_days: z.number(), // 保留时间按天设置，过期事件不能补发。
  anomaly_min_quota: z.number(), // 有历史样本时仍需达到绝对费用下限。
  anomaly_multiplier: z.number(), // 当前费用相对历史中位数的倍数。
  anomaly_min_samples: z.number(), // 达到最小样本数后才启用历史比较。
  anomaly_cold_quota: z.number(), // 样本不足时使用独立的高额阈值。
})
const consumerSchema = z.object({
  id: z.number(), // 消费者标识不会因凭据轮换而变化。
  name: z.string(),
  topics: z.array(z.string()), // 主题只是过滤条件，权限仍由后端确定。
  user_ids: z.array(z.number()), // 空列表表示授权范围内的所有用户。
  enabled: z.boolean(),
  created_at: z.number(),
  last_seen_at: z.number(), // 连接活动不能冒充消息已经处理。
  backlog: z.number(), // 待确认消息独立于在线连接数量。
  token_count: z.number(),
  online_connections: z.number(),
  auth_valid: z.boolean(), // 创建者降权或认证版本失效时不能继续接收。
  expired_unacked_count: z.number(), // 超过保留期后必须通过业务接口人工对账。
})
const tokenSchema = z.object({
  id: z.number(),
  name: z.string(),
  topics: z.array(z.string()), // Token 授权只能进一步缩小消费者主题。
  token_hint: z.string(), // 列表只有尾号，不能重读完整凭据。
  expires_at: z.number(), // 秒级时间戳；零表示不设置到期时间。
  created_at: z.number(),
  last_used_at: z.number(),
  revoked_at: z.number(), // 撤销记录保留，便于追踪轮换过程。
})
const eventSchema = z.object({
  id: z.number(),
  topic: z.string(),
  payload: z.record(z.string(), z.unknown()), // 摘要由服务端白名单构造。
  created_at: z.number(),
})
const deliverySchema = eventSchema.omit({ id: true }).extend({
  id: z.number(), // ACK 确认投递标识，不使用订单标识。
  event_id: z.number(), // 业务事件标识用于客户端幂等去重。
  last_sent_at: z.number(), // 写入连接成功不等于业务处理成功。
  attempts: z.number(),
  acked_at: z.number(), // 只有客户端确认才算完成消费。
})
const overviewSchema = z.object({
  settings: settingsSchema,
  topics: z.array(z.object({ key: z.string(), label: z.string() })),
  consumers: z.array(consumerSchema),
  events_count: z.number(),
  unresolved_charge_count: z.number().optional(), // 任务终态与最终观察尚未一致。
  sync_pending_count: z.number().optional(), // 缓存确认未知，不能盲目重复扣费。
})
const detailSchema = z.object({
  consumer: consumerSchema,
  tokens: z.array(tokenSchema),
  deliveries: z.array(deliverySchema),
})
export type EventSettings = z.infer<typeof settingsSchema>
export type EventConsumer = z.infer<typeof consumerSchema>
export type EventToken = z.infer<typeof tokenSchema>
export type EventDelivery = z.infer<typeof deliverySchema>
export type EventOverview = z.infer<typeof overviewSchema>
export type EventDetail = z.infer<typeof detailSchema>
export type ConsumerInput = Pick<
  EventConsumer,
  'name' | 'topics' | 'user_ids'
> & {
  enabled?: boolean
}
export type TokenInput = { name: string; topics: string[]; expires_at: number }

// 所有请求复用统一认证和错误契约，业务失败不得变成空列表。
// Token 创建使用单次授权配置，认证刷新后不能自动重放写操作。
const root = '/api/event-hub'
export async function getEventHub(): Promise<EventOverview> {
  const response = await api.get(root)
  return overviewSchema.parse(requireServerSuccess(response.data).data)
}
export async function getEventConsumer(id: number): Promise<EventDetail> {
  const response = await api.get(`${root}/consumers/${id}`)
  return detailSchema.parse(requireServerSuccess(response.data).data)
}
export async function saveEventSettings(input: EventSettings): Promise<void> {
  requireServerSuccess((await api.put(`${root}/settings`, input)).data)
}
export async function saveEventConsumer(
  input: ConsumerInput,
  id?: number
): Promise<void> {
  const response = id
    ? await api.put(`${root}/consumers/${id}`, input)
    : await api.post(`${root}/consumers`, input)
  requireServerSuccess(response.data)
}
export async function createEventToken(
  id: number,
  input: TokenInput,
  proof: string,
  signal: AbortSignal
) {
  const response = await api.post(`${root}/consumers/${id}/tokens`, input, {
    headers: { 'X-Security-Proof': proof },
    singleUseAuthorization: true,
    signal,
  })
  // 明文只交给当前调用方，不放进列表查询或缓存。
  return z
    .object({ token: z.string(), item: tokenSchema })
    .parse(requireServerSuccess(response.data).data)
}
export async function revokeEventToken(
  consumer: number,
  token: number
): Promise<void> {
  requireServerSuccess(
    (await api.delete(`${root}/consumers/${consumer}/tokens/${token}`)).data
  )
}
export async function replayEvent(
  consumer: number,
  event: number
): Promise<void> {
  // 这里只请求补发事件，绝不重新执行事件对应的资金业务。
  requireServerSuccess(
    (
      await api.post(`${root}/consumers/${consumer}/replay`, {
        event_id: event,
      })
    ).data
  )
}
export async function getEventHistory(page: number) {
  const response = await api.get(`${root}/events`, { params: { page } })
  return z
    .object({ items: z.array(eventSchema), total: z.number() })
    .parse(requireServerSuccess(response.data).data)
}
