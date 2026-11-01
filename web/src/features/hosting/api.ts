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
import { isAxiosError } from 'axios'
import { z } from 'zod'

import { api, type ApiRequestConfig } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  accountSchema,
  authorizationSchema,
  policySchema,
  observationSchema,
  resetOperationSchema,
  hostingTestSchema,
  type ResourceSource,
  type HostingAuthorization,
} from './types'

// 创建超时仍可携带可恢复会话；保留原始错误供统一错误处理展示。
export class HostingAuthorizationError extends Error {
  constructor(
    public authorization: HostingAuthorization,
    cause: unknown
  ) {
    super('Hosting authorization is pending', { cause })
  }
}
// 传输层只负责同源请求、一次性授权和 DTO 校验。
// 页面组件不得直接访问执行层的管理 API。
// 列表、详情和操作结果保留各自契约，不套用无业务意义的通用 CRUD。
// 接口的 success=false 与 HTTP 失败都必须抛出异常。
// 空数据与坏数据区别处理：合法空列表可以展示，坏结构不能静默兜底。
// 账号 ID、Key ID 和操作 UUID 属于不同命名空间，调用方不可互换。
// 查询键由页面管理，接口层不缓存凭据、回调或身份信息。
// 保留服务端错误来源，交由应用统一处理可读错误与登录状态。
// 服务端按当前会话检查所有权，浏览器参数不构成授权证明。
// 没有可用模型时返回真实空目录，不能替换成全站模型。
// 查询统一使用本人路由；用户、上游地址和管理凭据不能由浏览器指定。
const root = '/api/hosting'
// 敏感证明与 OAuth 回调是单次授权，不允许认证拦截器重放已发送请求。
const singleUse: ApiRequestConfig = { singleUseAuthorization: true }
// 复用应用的会话刷新流程，不在业务模块另建 token 或 Cookie 管理。
function proofOptions(proof: string): ApiRequestConfig {
  return { ...singleUse, headers: { 'X-Security-Proof': proof } }
}
// 只保存非敏感资源摘要；格式不符必须可见失败，不能返回假空列表。
// 列表接口的外层 items 不能误当成数组解析。
// 摘要可能包含缓存观测，无需为每个列表项访问原厂。
// 暂停账号仍保留，用户可以在工作区主动恢复。
export async function getHostingAccounts() {
  const response = await api.get(`${root}/accounts`, { disableDuplicate: true })
  return z
    .object({ items: z.array(accountSchema) })
    .parse(requireServerSuccess(response.data).data).items
}
// 用户默认和 Key 覆盖使用同一契约；tokenId 不代表切换所属用户。
// 未传 Key ID 时读取用户默认配置。
// 传入 Key ID 时服务端返回完整有效策略及继承标志。
// 旧 Key 的 legacy 状态由后端决定，前端不推断迁移。
export async function getResourcePolicy(tokenId?: number) {
  const path = tokenId ? `/tokens/${tokenId}/policy` : '/policy'
  const response = await api.get(root + path, { disableDuplicate: true })
  return policySchema.parse(requireServerSuccess(response.data).data)
}
// 提交完整顺序和读取时版本，冲突由服务端拒绝后提示重新加载。
// 空数组是用户明确禁用全部来源，不转换成默认回退。
// 来源排序与开关一起提交，不能拆成多次部分更新。
// 版本冲突时保存失败，让用户重新审查最新策略。
// 这里不插入默认钱包，也不自动启用被暂停账号。
export async function saveResourcePolicy(
  revision: number,
  sources: ResourceSource[],
  tokenId?: number
) {
  const path = tokenId ? `/tokens/${tokenId}/policy` : '/policy'
  const response = await api.put(root + path, { revision, sources })
  return policySchema.parse(requireServerSuccess(response.data).data)
}
// 恢复继承通过明确删除覆盖记录，不能以空策略伪装继承。
// 该操作仅删除 Key 的覆盖，不修改用户默认策略。
// 删除成功后由组件重新读取实际继承内容。
// 继承和禁用全部来源是不同业务动作。
export async function inheritResourcePolicy(tokenId: number) {
  const response = await api.delete(`${root}/tokens/${tokenId}/policy`)
  requireServerSuccess(response.data)
}
// 授权入口固定首期 Codex；已有账号重新授权只能由服务端核对身份。
// 证明绑定本次连接或重授权动作，不能跨账号复用。
// 首次请求超时仍可能已创建远端会话。
// 保留服务端返回的会话引用，用户可以恢复同一操作。
// 没有有效引用的异常保持原失败，不猜测成功。
export async function startHostingAuthorization(
  proof: string,
  accountId?: number
) {
  try {
    const response = await api.post(
      `${root}/authorizations`,
      { provider: 'codex', account_id: accountId },
      proofOptions(proof)
    )
    return authorizationSchema.parse(requireServerSuccess(response.data).data)
  } catch (error) {
    // 只恢复结构校验通过的会话引用，不把错误响应中的任意字段当凭据。
    if (isAxiosError(error)) {
      const parsed = authorizationSchema.safeParse(error.response?.data?.data)
      if (parsed.success) {
        throw new HostingAuthorizationError(parsed.data, error)
      }
    }
    throw error
  }
}
// 读取状态不隐式换码或刷新原厂凭据，轮询不会产生授权副作用。
// 轮询必须仍处于发起授权的站点会话中。
// 服务端会验证会话绑定，换浏览器登录不能接管旧流程。
// 读取状态与产生外部副作用的恢复请求保持分离。
export async function getHostingAuthorization(id: string) {
  const response = await api.get(
    `${root}/authorizations/${encodeURIComponent(id)}`,
    { disableDuplicate: true }
  )
  return authorizationSchema.parse(requireServerSuccess(response.data).data)
}
// 回调只作为字符串提交至已认证的同源 API，不访问用户粘贴的地址。
// 读取后立即由表单清空，接口错误也不得把含 code 的请求写入日志。
// 回调中的 OAuth code 是短期秘密，不属于可重试请求体。
// 会话刷新后拦截器也不能自动重发这个请求。
// 交换正在进行时仅返回状态，由后续只读查询确认。
export async function completeHostingAuthorization(
  id: string,
  callback: string
) {
  const response = await api.post(
    `${root}/authorizations/${encodeURIComponent(id)}/callback`,
    { callback_url: callback },
    singleUse
  )
  return authorizationSchema.parse(requireServerSuccess(response.data).data)
}
// 恢复只针对同一持久会话；不能重新提交一次性回调。
// 恢复请求复用原授权 ID，绝不重新生成 OAuth state。
// 该入口主要用于初始创建状态未决的情况。
// 服务端已持有 Stage 时恢复只同步其当前状态。
export async function resumeHostingAuthorization(id: string) {
  const response = await api.post(
    `${root}/authorizations/${encodeURIComponent(id)}/resume`,
    {},
    singleUse
  )
  return authorizationSchema.parse(requireServerSuccess(response.data).data)
}
// 服务端清除未完成会话；浏览器关闭不能被视为自动绑定成功。
// 取消结果失败仍向用户反馈，不静默关闭并假装解绑。
// 取消未完成授权与删除已保存账号不是同一个动作。
// 服务端决定已消费会话是否仍可取消。
export async function cancelHostingAuthorization(id: string) {
  const response = await api.delete(
    `${root}/authorizations/${encodeURIComponent(id)}`
  )
  requireServerSuccess(response.data)
}
// 保存账号只引用服务端授权会话，不把原厂 token 复制进 New API 表单。
// 授权完成后保存是独立一步，失败时可按原授权引用重试。
// 账号备注属于 Oak 展示信息，不参与身份验证。
// 重授权沿用原账号 ID，不插入重复业务账号。
export async function createHostingAccount(
  authorizationId: string,
  alias: string
) {
  const response = await api.post(`${root}/accounts`, {
    authorization_id: authorizationId,
    alias,
  })
  return accountSchema.parse(requireServerSuccess(response.data).data)
}
// 白名单仅允许备注和暂停；不能从普通用户页面更新 owner 或上游路由。
// 暂停仅影响新请求，不能保证中断已在上游执行的请求。
// 备注改名不改变策略中的稳定账号 ID。
// 接口不允许任意字段透传到执行层。
export async function updateHostingAccount(
  id: number,
  changes: { alias?: string; paused?: boolean }
) {
  const response = await api.patch(`${root}/accounts/${id}`, changes)
  return accountSchema.parse(requireServerSuccess(response.data).data)
}
// 解绑是敏感操作，服务器必须先停用执行资源再异步清理。
// 删除须二次证明，并先让新请求无法选中该资源。
// 解绑不会替用户撤销原厂网站的登录或订阅。
// 删除失败应保留详情供重试，不能伪造成功提示。
export async function deleteHostingAccount(id: number, proof: string) {
  const response = await api.delete(
    `${root}/accounts/${id}`,
    proofOptions(proof)
  )
  requireServerSuccess(response.data)
}

// 额度查询读取缓存投影，用户主动刷新才访问上游；两个动作绝不耗卡。
// 没有观测时合法返回 null，界面显示尚不可用。
// 已存在但结构异常的快照会解析失败。
// 查询缓存不等于原厂额度实时刷新。
export async function getHostingObservation(id: number) {
  const response = await api.get(`${root}/accounts/${id}/observation`, {
    disableDuplicate: true,
  })
  return observationSchema
    .nullable()
    .parse(requireServerSuccess(response.data).data)
}
// 刷新是显式只读同步，不调用重置机会消费接口。
// 部分子查询失败应保留服务端新鲜度状态。
// 快照各窗口不在此归一化为人民币余额。
export async function refreshHostingObservation(id: number) {
  const response = await api.post(`${root}/accounts/${id}/refresh-observation`)
  return observationSchema
    .nullable()
    .parse(requireServerSuccess(response.data).data)
}
// 模型发现只证明可列出模型，客户端协议必须经过单独的真实测试。
// 这个目录属于指定账号，并非当前用户所有资源的并集。
// 目录发现不能证明某个协议已经真实调用成功。
// 测试模型选择使用目录 ID，不按展示名模糊匹配。
export async function getHostingModels(id: number) {
  const response = await api.get(`${root}/accounts/${id}/models`)
  return z
    .object({ models: z.array(z.object({ id: z.string() })) })
    .parse(requireServerSuccess(response.data).data)
}
// 测试是显式动作，会消耗少量原厂额度；禁止 Query 自动重试调用。
// 测试成功与失败均由返回的 outcome 表达。
// HTTP 成功不代表模型调用通过，组件仍检查 outcome。
// 真实测试可能耗额，所以使用防自动重放配置。
export async function testHostingAccount(id: number, model: string) {
  const response = await api.post(
    `${root}/accounts/${id}/test`,
    { model },
    singleUse
  )
  return hostingTestSchema.parse(requireServerSuccess(response.data).data)
}
// 历史查询支撑刷新页面后的恢复，不把操作 ID 放进 localStorage。
// 历史只包含当前账号的持久操作，刷新页面后仍能恢复。
// 存在未决操作时界面禁止再生成新的消费请求。
// 不通过浏览器临时状态推断先前扣卡结果。
// 读取可能同步远端已完成的结果，但不得再次执行消费。
// 用户可以用它确认超时请求最后是否成功。
// 没有结果仍返回未知，不生成一个替代操作。
export async function getResetOperations(id: number) {
  const response = await api.get(`${root}/accounts/${id}/reset-operations`, {
    disableDuplicate: true,
  })
  return z
    .object({ items: z.array(resetOperationSchema) })
    .parse(requireServerSuccess(response.data).data)
}
// 新消费必须绑定持久 UUID 与单次证明；结果未知时保持原 UUID。
// UUID 对应一个业务消费操作，而不是一次 HTTP 尝试。
// 响应丢失后继续使用原 UUID，防止重复耗卡。
// 证明也绑定同一 UUID，不能挪用到另一个消费。
export async function consumeResetCredit(
  id: number,
  operationId: string,
  proof: string
) {
  const response = await api.post(
    `${root}/accounts/${id}/reset-credits/consume`,
    { operation_id: operationId },
    proofOptions(proof)
  )
  return resetOperationSchema.parse(requireServerSuccess(response.data).data)
}
// 只读同步单次操作可找回上次响应，不需要再消费重置机会。
export async function getResetOperation(operationId: string) {
  const response = await api.get(
    `${root}/reset-operations/${encodeURIComponent(operationId)}`,
    { disableDuplicate: true }
  )
  return resetOperationSchema.parse(requireServerSuccess(response.data).data)
}
// GET 永远只读；继续确认必须显式 POST 且仍指向原持久操作。
// 继续未决操作需要再次确认并重新取得单次证明。
// 请求体无需新 UUID，路径已经指定原操作身份。
// 后端负责确保恢复不会造成第二次原厂扣卡。
export async function resumeResetOperation(operationId: string, proof: string) {
  const response = await api.post(
    `${root}/reset-operations/${encodeURIComponent(operationId)}/resume`,
    {},
    proofOptions(proof)
  )
  return resetOperationSchema.parse(requireServerSuccess(response.data).data)
}

// 统一 Key 模型目录由服务端按来源、模型权限与验证记录交集生成。
// 缺少 verified 不代表通过；配置导出只能使用已验证 Responses 模型。
// 模型列表还要满足当前 Key 的模型限制。
// 协议数组按验证事实生成，不从模型名称猜测协议。
// 没有 verified 的记录只供发现，不能用于已验证导出。
export async function getTokenResourceModels(tokenId: number) {
  const response = await api.get(`${root}/tokens/${tokenId}/models`, {
    disableDuplicate: true,
  })
  return z
    .object({
      models: z.array(
        z.object({
          id: z.string(),
          protocols: z.array(z.string()).optional(),
          verified: z.boolean().optional(),
        })
      ),
    })
    .parse(requireServerSuccess(response.data).data)
}
