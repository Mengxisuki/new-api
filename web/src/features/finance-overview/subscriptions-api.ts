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

// 两类来源在接口边界明确区分；号池不携带用户身份。
// 用严格投影解析列表，字段缺失属于接口失败，不能伪装为空数据。
// 时间均为 Unix 秒，统一使用左闭右开区间。
// 公开投影不包含用户邮箱、账户密钥或渠道连接参数。
// 订阅名称为空表示关联套餐已不存在，展示层会回退为套餐编号。
// 号池名称由人工输入，和用户套餐标题没有跨来源唯一性要求。
// 用户订阅的 status 可能含历史值，不能用严格枚举使整页不可读。
// 取消状态保留原值；生效、未开始和过期由展示时刻判定。
// 时间字段为整数，禁止把毫秒当秒导致跨数万年的日期。
// 分类进入类型系统后，组件无需在每个分支重复检查来源。
const periodSchema = z.object({
  id: z.number().int().positive(),
  category: z.enum(['user', 'pool']),
  name: z.string(),
  user_id: z.number().int(),
  username: z.string(),
  plan_id: z.number().int(),
  start_time: z.number().int(),
  end_time: z.number().int(),
  status: z.string(),
})
export type SubscriptionPeriod = z.infer<typeof periodSchema>
export type SubscriptionCategory = 'all' | 'user' | 'pool'

// 页大小由服务端固定为二十条；总数用于公共分页控件。
const pageSchema = z.object({
  items: z.array(periodSchema),
  total: z.number().int().nonnegative(),
})

// 甘特图有自己的日期窗口，不继承收入的“本月至今”。
// 这样月底续订和未来计划也可以在查询范围中显示。
// 每次刷新禁用 axios 合并重复 GET，确保保存后的刷新触及服务端。
// React Query 仍负责请求缓存、重试及同查询订阅协调。
// 先检查业务 success，再解析数据；权限失败不会进入空态分支。
// 响应新增未使用字段可被丢弃，已声明字段缺失则必须失败。
// 接口页码从一开始，组件内部页索引到此入口前完成转换。
// 这里不限制周期长度，查询窗口上限属于控制器与界面筛选契约。
export async function getSubscriptionPeriods(
  start: number,
  end: number,
  category: SubscriptionCategory,
  page: number
) {
  const response = await api.get('/api/finance/subscriptions', {
    params: { start, end, category, page },
    disableDuplicate: true,
  })
  return pageSchema.parse(requireServerSuccess(response.data).data)
}

// 保存只提交人工周期的三个可编辑字段，不允许覆盖来源或用户归属。
// id 缺省表示新增；非空时只能编辑号池表中的同号记录。
// 业务失败必须抛出，交给公共 Mutation 错误处理并保持表单打开。
export async function savePoolSubscription(
  id: number | undefined,
  input: { name: string; start_time: number; end_time: number }
) {
  const response = id
    ? await api.put(`/api/finance/pool-subscriptions/${id}`, input)
    : await api.post('/api/finance/pool-subscriptions', input)
  requireServerSuccess(response.data)
}

// 删除仅针对人工图表记录，用户套餐没有对应的前端写入口。
// 服务端仍独立检查 Root 权限，不以按钮隐藏作为授权依据。
export async function deletePoolSubscription(id: number) {
  const response = await api.delete(`/api/finance/pool-subscriptions/${id}`)
  requireServerSuccess(response.data)
}

// 日期字符串来自 date 输入；显式使用上海时区，不受浏览器时区影响。
// round-trip 排除不存在的日期，例如二月三十日，避免 Date 自动进位。
// 空字符串是编辑日期时的正常中间状态，返回无效数字交由表单提示。
// 不在此处抛异常，避免清空输入导致整个财务页面崩溃。
// 不接收带时间或时区的任意 ISO 文本，防止同字段出现两种语义。
// 该函数只返回当天零点，包含结束日的加一天由提交调用方负责。
// 年份固定四位，使表单与后端可表达范围一致。
// 前后转换比较使用平移后的 UTC 日期，保证非上海机器结果相同。
// 查询窗口和保存表单都复用这个入口，避免日期解释不一致。
export function subscriptionDay(value: string): number {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return Number.NaN
  const time = Date.parse(`${value}T00:00:00+08:00`) / 1000
  if (!Number.isFinite(time)) return Number.NaN
  const roundTrip = new Date((time + 8 * 3600) * 1000)
    .toISOString()
    .slice(0, 10)
  return roundTrip === value ? time : Number.NaN
}
