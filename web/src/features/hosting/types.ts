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

// 所有外部数据在入口解析；界面只接受服务端确认的账号和来源。
// account_id 是 Oak 的用户资源标识，不接收 gpt-load 管理组或凭据 ID。
// 来源顺序是用户授权的一部分，不只是 UI 排列。
// 挂靠项引用具体账号，包月与钱包项不需要账号编号。
// 禁用项仍保留在数组中，让用户恢复时保持原位置。
// 实际所有权和重复来源限制由服务端再次校验。
export const sourceSchema = z.object({
  kind: z.enum(['hosting', 'hosting_pool', 'subscription', 'wallet']),
  account_id: z.number().int().positive().optional(),
  enabled: z.boolean(),
})
// revision 用于当前 Key 的并发编辑检测，sources 是该 Key 独立保存的顺序。
// legacy 提醒旧 Key 尚在原规则，不自动把账号或钱包加进授权范围。
export const policySchema = z.object({
  revision: z.number().int(),
  legacy: z.boolean(),
  sources: z.array(sourceSchema),
})
// 仅返回服务是否已配置及稳定错误码，浏览器不获取部署地址或管理凭据。
// 就绪检查不连接原厂、不生成授权，进入页面时可以安全读取。
export const hostingStatusSchema = z.object({
  configured: z.boolean(),
  code: z.string(),
})
// 授权地址只用于跳转；code、回调和原厂凭据均不持久化到浏览器。
// 一次授权会话只能绑定当前用户，expires_at 使用服务端 Unix 秒。
// 授权会话标识属于站点，不等于原厂 OAuth state。
// 启动异常时可能尚无授权 URL，因此允许字段缺省。
// ready 表示换码完成，consumed 表示已用于保存账号。
// starting 与 consuming 属于控制面中间状态。
// 具体状态可随执行层增加，界面只对已知可执行状态开放动作。
// 过期时间以秒表示，避免与额度快照的毫秒字段混淆。
export const authorizationSchema = z.object({
  id: z.string(),
  status: z.string(),
  authorization_url: z.string().optional(),
  redirect_uri: z.string().optional(),
  expires_at: z.number(),
  account_id: z.number(),
})
// 表单限制作为用户反馈，后端仍验证所属关系、长度和 OAuth state。
export const connectionSchema = z.object({
  alias: z.string().trim().min(1).max(64),
  callback_url: z.string().trim().max(8192),
})
// 类型从运行时 Schema 推导，避免界面类型与校验规则分叉。
// 新增协议字段时必须先修改入口 Schema，再调整展示和测试。
// 未知状态的处理不能靠强制类型转换假装已经受信任。
export type ResourceSource = z.infer<typeof sourceSchema>
export type ResourcePolicy = z.infer<typeof policySchema>
export type HostingAuthorization = z.infer<typeof authorizationSchema>
// 来源标识用于排序和去重，与可修改或重复的账号备注无关。
export function sourceKey(source: ResourceSource): string {
  return source.kind === 'hosting'
    ? `hosting:${source.account_id}`
    : source.kind
}

// 上游窗口单位与名称原样保留；未知数值不转换为零或无限额度。
// 不同账号、不同模型窗口不可相加，reset_at_ms 只代表该窗口。
// 额度契约保留上游的单位，不能统一当作可消费金额。
// 观测状态描述整个快照的新鲜度，窗口自身还有单独状态。
// 部分失败允许带历史快照，界面必须继续展示过期标记。
// 观测时间与窗口重置时间均使用毫秒，不能当 Unix 秒格式化。
// 可选数值不存在时表示未知；数字零是明确的真实观测。
// 卡子列表的过期时间描述机会有效期，不是额度窗口到期。
// 积分余额可能具有小数精度，因此保留服务端原始字符串。
// 无限积分标志不等于无限请求，也不改写原厂窗口状态。
// 字段解析失败会暴露错误，不尝试以空对象继续展示。
// 快照的 null 是合法未观测状态，与格式错误分开处理。
export const observationSchema = z.object({
  state: z.string(),
  observed_at_ms: z.number().optional(),
  last_error_code: z.string().optional(),
  snapshot: z
    .object({
      plan_summary: z
        .object({ name: z.string().optional(), level: z.string().optional() })
        .optional(),
      quota_windows: z
        .array(
          z.object({
            id: z.string(),
            label: z.string(),
            unit: z.string(),
            state: z.string(),
            used: z.number().optional(),
            limit: z.number().optional(),
            remaining: z.number().optional(),
            utilization: z.number().optional(),
            reset_at_ms: z.number().optional(),
          })
        )
        .optional(),
      // 重置卡子查询独立失败时，即使主额度 fresh，也不得放行消费。
      reset_credit_observation: z
        .object({
          state: z.string(),
          observed_at_ms: z.number().optional(),
          error_code: z.string().optional(),
        })
        .optional(),
      reset_credits_available: z.number().int().nonnegative().optional(),
      reset_credits: z
        .array(z.object({ expires_at_ms: z.number().optional() }))
        .optional(),
      // 积分余额保留原始小数字符串，与平台钱包及重置次数分开。
      credits: z
        .object({
          balance: z.string().nullable().optional(),
          unlimited: z.boolean().optional(),
          has_credits: z.boolean().optional(),
        })
        .optional(),
    })
    .nullable()
    .optional(),
})
// 账号状态与暂停开关分别展示；授权成功不代表真实调用已验证。
// observation 由独立额度契约处理，列表摘要不把缺少观测误判为零。
// 账号 DTO 不包含 access token、refresh token 或上游内部编号。
// 暂停开关与生命周期分离，连接完成仍可以被用户暂停。
// connected 只证明绑定完成；active 来自实际模型测试成功。
// provisioning 表示执行资源仍在配置，不能提前宣传可调用。
// deleting 阶段仍可展示，供用户理解异步解绑进度。
// 服务端列表正常排除 deleted，但解析保留完整状态契约。
// 原厂邮箱只接收脱敏值，页面不自行获取完整身份信息。
// 观察摘要在同一入口校验，避免列表吞掉坏快照。
// 未返回快照时保持 undefined，不能构造一个 fresh 零值。
// 最后错误是脱敏业务码，可以帮助定位待处理原因。
export const accountSchema = z
  .object({
    id: z.number().int().positive(),
    provider: z.literal('codex'),
    alias: z.string(),
    email_mask: z.string(),
    status: z.enum([
      'provisioning',
      'connected',
      'active',
      'paused',
      'deleting',
      'deleted',
      'error',
    ]),
    paused: z.boolean(),
    verified_at: z.number(),
    last_error: z.string(),
    observation: observationSchema.nullable().optional(),
  })
  .passthrough()
export type HostingAccount = z.infer<typeof accountSchema>
// 持久操作状态可在页面重载后查询；result 只接收脱敏业务结果。
// prepared/outcome_unknown 都阻止生成新的消费 ID，不能把超时解释成未扣卡。
// 操作结果允许为空，传输成功不代表已有原厂结论。
// succeeded 与 rejected 是已明确的终态。
// outcome_unknown 可能已耗卡，不能当成未执行重试。
// windows_reset 只表达上游确认的窗口数量。
// observation_pending 提醒额度视图仍未同步。
// 错误码不包含上游响应体或凭据。
export const resetOperationSchema = z.object({
  operation_id: z.string(),
  account_id: z.number(),
  status: z.string(),
  created_at: z.number().optional(),
  updated_at: z.number().optional(),
  result: z
    .object({
      windows_reset: z.number().optional(),
      observation_pending: z.boolean().optional(),
      error_code: z.string().optional(),
    })
    .nullable()
    .optional(),
})
// 真实测试结果不同于模型发现；只有 passed 才展示调用已验证。
// 测试记录同时绑定模型和协议，不能推广到所有模型。
// inconclusive 表示未能证实结果，不应显示绿色已验证。
// 延迟仅在服务端确有记录时可用。
// 测试时间用于区分历史验证与当前查询时间。
export const hostingTestSchema = z.object({
  outcome: z.enum(['passed', 'failed', 'inconclusive']),
  model: z.string(),
  protocol: z.string(),
  latency_ms: z.number().optional(),
  reason: z.string().optional(),
  tested_at_ms: z.number(),
})
export type HostingObservation = z.infer<typeof observationSchema>
export type ResetOperation = z.infer<typeof resetOperationSchema>
