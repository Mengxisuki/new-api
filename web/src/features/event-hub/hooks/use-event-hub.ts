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
/**
 * 状态管理与秘密生命周期：
 * 概况、消费者详情和历史查询共享事件服务命名空间，
 * 但包含当前用户及会话标识。
 * 不同登录会话不能通过相同查询键复用管理数据。
 * 当前消费者为空时禁止发起详情查询；
 * 后续变更也再次检查编号，
 * 避免错误动作落到零编号资源。
 * 普通配置、订阅、撤销和补发采用变更接口，
 * 成功后使当前身份的查询失效。
 * 读取刷新只更新摘要，
 * 不将写入视为消费确认。
 * 令牌签发不使用变更缓存，
 * 因为返回值包含一次性秘密；
 * 秘密仅保存在组件本地状态。
 * 保存秘密时同时记录用户、会话和消费者编号，
 * 渲染时逐项比较，
 * 身份刚变化而清理尚未运行时也立即隐藏旧明文。
 * 页面离开、切换账号、刷新会话或切换消费者时会取消当前请求和安全验证，
 * 并清空秘密。
 * 请求取消信号传给真实网络操作，
 * 迟到响应必须再次检查当前控制器是否仍然匹配。
 * 这个检查防止旧请求完成后把凭据展示到新目标。
 * 任何时刻只允许一份令牌创建请求，
 * 避免多个安全证明和结果竞争。
 * 创建输入的主题先排序，
 * 证明与实际请求采用同一份规范化数组。
 * 有效期在表单提交时冻结，
 * 不能在等待验证后再次计算，
 * 否则证明上下文会不一致。
 * 二次验证返回取消时直接结束，
 * 不制造失败提示，
 * 也不能继续调用创建接口。
 * 返回明确证明后，
 * 客户端通过专用请求头提交，
 * 认证刷新不重放这次单次授权。
 * 生成成功先记录一次性明文再刷新列表，
 * 列表本身永远只有尾号和业务元数据。
 * 刷新不会把明文写回查询数据，
 * 关闭单次展示后也不会通过重新拉取找回。
 * 创建失败保留具体服务端原因，
 * 不能返回空字符串假装成功。
 * 普通保存的布尔结果只控制弹窗是否关闭，
 * 异常仍由统一提示层处理。
 * 尚未完成的请求保留忙碌状态，
 * 结束时只有当前控制器可以清理，
 * 旧响应不能覆盖新操作状态。
 * 取消与真实失败分开判断，
 * 离开页面后的请求不再向用户弹出无归属的错误。
 * 五秒查询刷新只是运营状态观测，
 * 不是后台消费者或生产者，
 * 不应持有业务令牌。
 */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { useSecureVerification } from '@/features/auth/secure-verification'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import {
  createEventToken,
  getEventConsumer,
  getEventHub,
  replayEvent,
  revokeEventToken,
  saveEventConsumer,
  saveEventSettings,
  type ConsumerInput,
  type EventSettings,
  type TokenInput,
} from '../api'

/**
 * 服务数据只在当前登录身份内缓存。
 * 明文凭据留在会话绑定的组件内存，不进入 Query 的变更缓存。
 * 身份变化或页面离开会取消未完成的安全验证和请求。
 * 五秒刷新只用于状态展示，不代表浏览器参与业务消费。
 */
export function useEventHub(consumerId: number | null) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const sessionId = useAuthStore((state) => state.auth.session?.sid)
  // 会话标识属于查询身份的一部分。
  // 同一个用户重新登录也不能复用旧会话缓存。
  // 服务端摘要和详情保留不同后缀，避免结构混用。
  // 统一前缀失效会刷新两者，不要求每个按钮知道全部查询。
  const key = ['event-hub', userId, sessionId]
  const overview = useQuery({
    queryKey: [...key, 'overview'],
    queryFn: getEventHub,
    refetchInterval: 5000,
  })
  const detail = useQuery({
    queryKey: [...key, 'consumer', consumerId],
    queryFn: () => {
      if (consumerId === null) throw new Error('Select a consumer')
      return getEventConsumer(consumerId)
    },
    enabled: consumerId !== null,
    refetchInterval: 5000,
  })
  const refresh = () => client.invalidateQueries({ queryKey: key })
  const settings = useMutation({
    mutationFn: saveEventSettings,
    onSuccess: refresh,
  })
  const consumer = useMutation({
    mutationFn: (input: { values: ConsumerInput; id?: number }) =>
      saveEventConsumer(input.values, input.id),
    onSuccess: refresh,
  })
  const revoke = useMutation({
    mutationFn: (tokenId: number) => {
      if (consumerId === null) throw new Error('Select a consumer')
      return revokeEventToken(consumerId, tokenId)
    },
    onSuccess: refresh,
  })
  const replay = useMutation({
    mutationFn: (eventId: number) => {
      if (consumerId === null) throw new Error('Select a consumer')
      return replayEvent(consumerId, eventId)
    },
    onSuccess: refresh,
  })
  // 以下状态不进入可持久化 Store。
  // 控制器引用只标识当前尚未完成的签发操作。
  // 旧操作的 finally 必须检查控制器身份。
  // 否则取消后的旧响应可能清掉新操作的忙碌状态。
  // 秘密同时保存签发时的登录与消费者信息。
  // 渲染时比较身份可以覆盖 Effect 清理之前的一帧。
  const verification = useSecureVerification()
  const cancelVerification = verification.cancel
  const current = useRef<AbortController | null>(null)
  const [creating, setCreating] = useState(false)
  const [secret, setSecret] = useState<{
    value: string
    userId?: number
    sessionId?: string
    consumer: number
  } | null>(null)
  useEffect(
    () => () => {
      // 同一页面切换消费者也必须清除旧凭据，避免展示到另一个接收方。
      current.current?.abort()
      current.current = null
      cancelVerification()
      setSecret(null)
      setCreating(false)
    },
    [cancelVerification, userId, sessionId, consumerId]
  )

  /**
   * 二次验证绑定消费者、主题及到期时间，旧证明不能扩展授权。
   * 创建结果只在请求与身份仍然匹配时回显一次。
   * 请求异常保留具体原因，取消验证不会出现错误提示。
   */
  const createToken = async (input: TokenInput): Promise<boolean> => {
    if (current.current || consumerId === null) return false
    const controller = new AbortController()
    current.current = controller
    setCreating(true)
    const topics = [...input.topics].sort()
    try {
      const proof = await verification.requestVerification({
        scope: 'event_hub.token',
        context: {
          consumer_id: consumerId,
          topics,
          expires_at: input.expires_at,
        },
      })
      if (!proof || current.current !== controller) return false
      const result = await createEventToken(
        consumerId,
        { ...input, topics },
        proof.proof_token,
        controller.signal
      )
      if (current.current !== controller) return false
      setSecret({
        value: result.token,
        userId,
        sessionId,
        consumer: consumerId,
      })
      await refresh()
      return true
    } catch (error) {
      if (!controller.signal.aborted) handleServerError(error)
      return false
    } finally {
      if (current.current === controller) {
        current.current = null
        setCreating(false)
      }
    }
  }
  // 普通写入不吞掉异常；只有完成刷新后才通知调用方关闭弹窗。
  const save = async (action: () => Promise<unknown>): Promise<boolean> => {
    try {
      await action()
      toast.success(t('Saved successfully'))
      return true
    } catch (error) {
      handleServerError(error)
      return false
    }
  }
  // 仅向页面暴露完成业务所需的动作和状态。
  // 统一 pending 包含所有写入与令牌签发。
  // 页面轮询不阻塞配置草稿，写入忙碌才禁用动作。
  // 验证加载阶段没有交互弹窗，仍可保留原表单。
  // 验证交互阶段隐藏原表单，焦点交由安全验证器。
  // 关闭一次性结果只清内存，不向服务端发送凭据撤销。
  // 需要停用已签发凭据时必须执行明确撤销操作。
  // 秘密访问器不会把旧身份的值返回给组件。
  // 查询失效承诺只涉及摘要刷新，不复制签发结果。
  // 返回布尔值供表单决定是否关闭，不代替具体错误提示。
  const phase = verification.dialogProps.state.phase
  return {
    overview,
    detail,
    saveSettings: (values: EventSettings) =>
      save(() => settings.mutateAsync(values)),
    saveConsumer: (values: ConsumerInput, id?: number) =>
      save(() => consumer.mutateAsync({ values, id })),
    revokeToken: (id: number) => save(() => revoke.mutateAsync(id)),
    replayEvent: (id: number) => save(() => replay.mutateAsync(id)),
    pending:
      settings.isPending ||
      consumer.isPending ||
      revoke.isPending ||
      replay.isPending ||
      creating,
    createToken,
    // 登录身份比较在渲染时生效，不能等待清理 Effect 才隐藏旧凭据。
    createdToken:
      secret?.userId === userId &&
      secret?.sessionId === sessionId &&
      secret?.consumer === consumerId
        ? secret.value
        : '',
    clearCreatedToken: () => setSecret(null),
    verificationDialogProps: verification.dialogProps,
    verifying: phase !== 'idle' && phase !== 'loading',
  }
}
