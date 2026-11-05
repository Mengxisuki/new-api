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
