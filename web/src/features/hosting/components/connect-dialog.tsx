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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import type { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { handleServerError } from '@/lib/handle-server-error'
import { AuthOperationError } from '@/lib/secure-verification'

import {
  cancelHostingAuthorization,
  HostingAuthorizationError,
  completeHostingAuthorization,
  createHostingAccount,
  getHostingAuthorization,
  startHostingAuthorization,
  resumeHostingAuthorization,
} from '../api'
import {
  connectionSchema,
  type HostingAccount,
  type HostingAuthorization,
} from '../types'

// OAuth code 只留在本次表单内；关闭时销毁会话引用，不写入缓存或存储。
// 已有账号重授权保持同一绑定，服务器负责拒绝错误的上游身份。
// 授权弹窗只处理本人账号的连接或重新授权。
// 不收集原厂账号密码，不要求用户把账号密码交给平台。
// 登录操作在官方授权页面完成，平台接收短期回调。
// 官方登录页面的网络可达性与后续平台 API 调用分开说明。
// 同一个弹窗只保留一个服务端授权会话，避免混淆 OAuth state。
// 初次连接和重授权使用不同安全证明上下文。
// 重授权绑定原账号 ID，不能凭新的回调偷偷替换成其他身份。
// 账号备注只是 Oak 本地展示信息，不能替代原厂身份校验。
// 回调输入框使用密码样式，并关闭自动填充。
// 回调读取后立即清空，失败响应也不把秘密重新填回表单。
// 回调只提交到站点 API，浏览器不请求用户粘贴的 localhost 地址。
// 只有明确等待回调的状态允许提交一次性 code。
// 换码进行中只读轮询，不通过重按提交按钮重复发送 code。
// ready 表示凭据已取得，仍需保存账号才能完成接入。
// 保存失败可以重试同一授权引用，不重复换码。
// consumed 允许幂等保存重试，以恢复成功响应丢失的情况。
// starting 是本地创建未决，恢复沿用同一个授权操作 ID。
// consuming 是本地账号创建中间态，等待后台恢复结果。
// 终态失败、过期与取消都不允许继续提交旧回调。
// 没有有效授权 URL 时显示明确提示，不构造猜测的登录地址。
// 授权链接只允许官方 HTTPS 域名，拒绝服务端异常跳转值。
// 新窗口带隔离属性，防止官方页面接管当前窗口引用。
// 用户关闭弹窗时显式取消尚未完成的授权。
// 取消失败仍保留页面并显示错误，不能假装服务端已清理。
// 已保存账号的解绑操作不属于此弹窗，交给账号详情处理。
// 服务端错误消息继续由统一安全错误处理链筛选。
// 用户主动取消站点二次验证不弹出业务失败提示。
// 需要站点验证时让业务弹窗让出焦点，避免模态框互相阻塞。
// 所有 OAuth 状态仅存在内存，不写入 localStorage 或 URL 查询参数。
// 授权完成后失效账号列表，让新账号出现在工作区。
// 连接成功不代表真实模型已调用，详情测试仍是独立步骤。
// 授权数据被查询缓存短暂持有，卸载时立即回收。
export function ConnectHostingDialog(props: {
  account?: HostingAccount
  onClose: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const verification = useSecureVerification()
  const [authorization, setAuthorization] =
    useState<HostingAuthorization | null>(null)
  const form = useForm<z.infer<typeof connectionSchema>>({
    resolver: zodResolver(connectionSchema),
    defaultValues: { alias: props.account?.alias ?? '', callback_url: '' },
  })
  // 轮询读取状态，不创建账号、不提交回调，也不刷新原厂凭据。
  // 查询键包含授权会话 ID，不会把旧会话响应渲染到新会话。
  // 未开始授权时不执行查询，避免向空路径发请求。
  // 只有处理中状态继续轮询，终态停止无意义请求。
  // 换码延迟不能通过固定等待秒数认定成功，必须读取 ready。
  // 服务端会校验会话绑定和所属关系，前端不能自行复用别人 ID。
  // 查询失败会显式报错，不把上一次成功状态替换成假失败终态。
  // 临时查询关闭后不需要长时间驻留缓存。
  // 后台轮询不会持有原始回调字符串。
  // 轮询返回的链接仍要经过官方域名校验才能显示为可点击。
  const state = useQuery({
    queryKey: ['hosting', 'authorization', authorization?.id],
    queryFn: () => {
      if (!authorization) throw new Error(t('Start authorization first'))
      return getHostingAuthorization(authorization.id)
    },
    enabled: Boolean(authorization),
    // 轮询只读服务器授权状态；不在 GET 中重放 code 或创建账号。
    refetchInterval: (query) =>
      ['starting', 'pending_authorization', 'exchanging', 'consuming'].includes(
        query.state.data?.status ?? ''
      )
        ? 3000
        : false,
    gcTime: 0,
  })
  const current = state.data ?? authorization
  // 先完成站点安全验证，再创建原厂授权会话。
  // 证明上下文包含动作与目标账号，不能跨资源挪用。
  // 首次授权 account_id 为零，这是接口约定而非真实账号编号。
  // operation_id 在连接操作为空，不能复用重置卡的操作证明。
  // 证明为空表示用户取消验证，应终止后续请求。
  // 创建上游失败可能已产生本地会话，因此特殊错误保留恢复 ID。
  // 特殊错误仍经过统一错误提示，不能因可恢复就伪装成功。
  // 恢复状态不会自动再次提交证明，单次授权不能隐式重放。
  // 开始按钮在请求中禁用，避免双击创建多个会话。
  // 收到会话摘要后才展示原厂跳转和回调输入。
  const start = useMutation({
    mutationFn: async () => {
      const proof = await verification.requestVerification({
        scope: 'hosting.manage',
        context: {
          action: props.account ? 'reauthorize' : 'connect',
          account_id: props.account?.id ?? 0,
          operation_id: '',
        },
      })
      if (!proof) {
        throw new AuthOperationError('Verification cancelled', 'AUTH_CANCELLED')
      }
      return startHostingAuthorization(proof.proof_token, props.account?.id)
    },
    onSuccess: setAuthorization,
    onError: (error) => {
      if (error instanceof HostingAuthorizationError) {
        setAuthorization(error.authorization)
      }
      if (!('code' in error && error.code === 'AUTH_CANCELLED')) {
        handleServerError(error)
      }
    },
  })
  // 完成流程区分交换回调和保存账号两个阶段。
  // ready 或 consumed 不再需要回调，直接按原授权引用保存。
  // 交换前清理表单中的短期秘密，组件不保留第二份副本。
  // 交换仍在处理时返回未完成，不能关闭弹窗提示连接成功。
  // 交换返回的新状态同时更新局部引用与查询缓存。
  // 这样紧接着的渲染不会被缓存中的旧等待状态覆盖。
  // 保存账号请求只含授权引用和备注，不传原厂凭据。
  // 保存成功后才关闭弹窗并刷新账号摘要。
  // 变更失败保留授权引用，用户可继续同一持久流程。
  // 调用方无需构造账户身份，服务端从授权结果提取。
  // 重复保存依赖后端幂等契约，前端不新建授权规避冲突。
  // 回调内容不会出现在成功提示、错误文案或页面导航地址。
  const finish = useMutation({
    mutationFn: async (values: z.infer<typeof connectionSchema>) => {
      if (!authorization) throw new Error(t('Start authorization first'))
      // 一次性回调先清空，再调用接口；失败也不能把秘密保存在错误消息里。
      if (current?.status !== 'ready' && current?.status !== 'consumed') {
        form.setValue('callback_url', '')
        const result = await completeHostingAuthorization(
          authorization.id,
          values.callback_url
        )
        setAuthorization(result)
        client.setQueryData(['hosting', 'authorization', result.id], result)
        if (result.status !== 'ready') return false
      }
      await createHostingAccount(authorization.id, values.alias)
      return true
    },
    onSuccess: (connected) => {
      if (!connected) return
      void client.invalidateQueries({ queryKey: ['hosting', 'accounts'] })
      props.onClose()
    },
  })
  // 创建状态未决时只恢复原会话，不创建第二个登录流程。
  // 恢复是用户明确动作，不在 GET 轮询里偷偷创建远端 Stage。
  // 请求使用已有会话 ID，避免初次响应丢失后重复申请。
  // 恢复不采集新的 OAuth code，也不把旧回调从缓存拿回来。
  // 服务端仍检查原发起会话身份，重新登录后不能越权接管。
  // 响应同步到同一查询键，让当前页面进入真实远端状态。
  // 恢复失败保留原错误，不能自动启动第二个登录流程。
  const resume = useMutation({
    mutationFn: async () => {
      if (!authorization) throw new Error(t('Start authorization first'))
      return resumeHostingAuthorization(authorization.id)
    },
    onSuccess: (value) => {
      setAuthorization(value)
      client.setQueryData(['hosting', 'authorization', value.id], value)
    },
  })
  // 没有服务端会话时关闭只销毁本地表单。
  // 已有会话时先发取消请求，成功后才离开弹窗。
  // 取消请求与模型账号解绑不是同一接口。
  // 请求处理中阻止关闭，避免用户误以为消费已经被撤回。
  // 服务器已消费授权时可能拒绝取消，应展示真实冲突。
  // 弹窗退出不保存任何回调或明文凭据。
  const cancel = useMutation({
    mutationFn: async () => {
      if (authorization) await cancelHostingAuthorization(authorization.id)
    },
    onSuccess: props.onClose,
  })
  // 整个授权阶段只允许一个变更请求在途。
  // 开始、完成、恢复和取消共享忙碌门槛，防止相互覆盖结果。
  // 换码轮询不算变更忙碌，但提交按钮仍按状态严格禁用。
  // 禁止交换中重复发送回调，避免原厂单次 code 被重复消费。
  // 读取状态和接收用户输入分开，查询不会改变备注草稿。
  // 回调最长长度受表单和服务端双重约束，防止无界文本提交。
  // 备注在保存前会去掉首尾空白，空备注不能通过表单校验。
  // 官方授权窗口的异常不通过平台凭据代为登录。
  // 用户仍在自己的原厂页面完成原厂要求的验证。
  // 完成原厂登录后回到本平台粘贴一次性回调。
  // 回调中的 state 必须由执行层核验，前端没有对应秘密。
  // 服务器只解析回调文字，不访问用户设备的 localhost 服务。
  // 该手动方式不要求平台在用户电脑上启动本地代理。
  // 浏览器历史中的官方跳转由原厂控制，本页面不额外记录回调。
  // 取消安全验证不等于取消原厂会话，两种动作有独立生命周期。
  // 开始授权请求失败后若没有恢复 ID，保留明确错误让用户重新开始。
  // 有恢复 ID 时继续原会话，不自动重复申请登录地址。
  // 如果会话已经过期，旧回调不会被继续提交。
  // 过期后用户应关闭本次授权并发起新的连接。
  // 页面不会自行延长服务端会话有效期。
  // 服务端授权链接缺失不会改成用户可输入的任意跳转。
  // 域名校验防止状态响应成为意外开放重定向。
  // 当前会话切换后服务端可返回未找到，页面不绕过身份绑定。
  // 初次授权不保存账户，ready 后仍等待明确完成动作。
  // 用户名与邮箱不能由备注输入伪造，后端从授权身份确定。
  // OAuth code 不会成为 React Query 的查询键。
  // 错误对象可能包含传输信息，页面不把它直接输出到控制台。
  // 最终账号展示只来自服务端保存后的摘要。
  const busy =
    start.isPending || finish.isPending || cancel.isPending || resume.isPending
  // 只允许已知官方 HTTPS 授权域名，避免把服务端意外值变成任意跳转。
  // 官方 URL 由后端返回，仍需在浏览器跳转这个边界再次检查。
  // 只接受 HTTPS，禁止把异常响应变成明文授权地址。
  // 域名比较使用解析后的 hostname，不接受类似域名的子串匹配。
  // 解析失败只令链接不可用，不吞掉后续授权状态读取。
  // 可点击地址与回调字符串有不同信任边界，不能共用此校验。
  // 回调 URL 的 localhost、端口与 state 契约由服务端独立验证。
  let authorizationURL: string | undefined
  if (current?.authorization_url) {
    try {
      const url = new URL(current.authorization_url)
      if (
        url.protocol === 'https:' &&
        ['auth.openai.com', 'chatgpt.com'].includes(url.hostname)
      ) {
        authorizationURL = url.href
      }
    } catch {
      /* 非法地址保持不可点击，界面给出明确反馈。 */
    }
  }
  return (
    <>
      <Dialog
        open={!verification.isActive}
        onOpenChange={(open) => {
          if (!open && !busy) cancel.mutate()
        }}
        title={
          props.account ? t('Reauthorize account') : t('Connect Codex account')
        }
        description={t(
          'Use an existing Codex subscription. Opening the provider sign-in page may require your own network access.'
        )}
        showCloseButton={!busy}
      >
        {!authorization && (
          <Button disabled={busy} onClick={() => start.mutate()}>
            {t('Start browser authorization')}
          </Button>
        )}
        {current && (
          <form
            onSubmit={form.handleSubmit((values) => {
              if (
                !['ready', 'consumed'].includes(current.status) &&
                !values.callback_url
              ) {
                form.setError('callback_url', {
                  message: t('Enter the complete callback URL.'),
                })
                return
              }
              finish.mutate(values)
            })}
          >
            <FieldGroup>
              <p role='status'>
                {t('Authorization status')}: {current.status}
              </p>
              {current.status === 'pending_authorization' &&
                (authorizationURL ? (
                  <Button
                    render={
                      <a
                        href={authorizationURL}
                        target='_blank'
                        rel='noopener noreferrer'
                      />
                    }
                    nativeButton={false}
                  >
                    {t('Open provider authorization')}
                  </Button>
                ) : (
                  <p role='alert'>
                    {t(
                      'Authorization URL is unavailable. Start a new authorization.'
                    )}
                  </p>
                ))}
              {current.status === 'starting' && (
                <Button
                  type='button'
                  disabled={busy}
                  onClick={() => resume.mutate()}
                >
                  {t('Resume authorization')}
                </Button>
              )}
              {/*
               * 备注输入用于帮助用户区分多个账号，不要求输入完整邮箱。
               * 长度约束在表单反馈和服务端校验两端同时存在。
               * 重授权初始化原备注，避免意外把账号重命名为默认值。
               * 字段错误在对应输入下显示，不只依赖全局 Toast。
               * 提交中禁用输入，确保用户看到的备注与实际请求一致。
               */}
              <Field>
                <FieldLabel htmlFor='hosting-alias'>
                  {t('Account alias')}
                </FieldLabel>
                <Input
                  id='hosting-alias'
                  autoComplete='off'
                  maxLength={64}
                  disabled={busy}
                  aria-invalid={Boolean(form.formState.errors.alias)}
                  {...form.register('alias')}
                />
                {form.formState.errors.alias && (
                  <FieldDescription>
                    {t('Enter an account alias of 1 to 64 characters.')}
                  </FieldDescription>
                )}
              </Field>
              {/*
               * 只有尚未完成换码时需要回调输入。
               * 本地回调页面无法连接是正常的手动授权路径，不代表登录失败。
               * 帮助文字说明复制完整地址，避免只粘贴 code 遗失 state。
               * 回调属于秘密，用户不应把它发送到客服或聊天记录。
               * 界面不提供复制回调按钮，减少无意传播敏感信息。
               * 即使显示输入框，提交按钮仍按严格状态门槛控制。
               */}
              {!['ready', 'consumed'].includes(current.status) && (
                <Field>
                  <FieldLabel htmlFor='hosting-callback'>
                    {t('Authorization callback URL')}
                  </FieldLabel>
                  <Input
                    id='hosting-callback'
                    type='password'
                    autoComplete='off'
                    disabled={busy}
                    aria-invalid={Boolean(form.formState.errors.callback_url)}
                    {...form.register('callback_url')}
                  />
                  <FieldDescription>
                    {t(
                      'After signing in, paste the final localhost callback URL here, even if that page cannot connect. Never share it in chat or support tickets.'
                    )}
                  </FieldDescription>
                  {form.formState.errors.callback_url && (
                    <FieldDescription>
                      {t('Enter the complete callback URL.')}
                    </FieldDescription>
                  )}
                </Field>
              )}
              <Button
                type='submit'
                disabled={
                  busy ||
                  !['pending_authorization', 'ready', 'consumed'].includes(
                    current.status
                  )
                }
              >
                {t('Finish connecting')}
              </Button>
            </FieldGroup>
          </form>
        )}
        <Button
          variant='outline'
          className='mt-4'
          disabled={busy}
          onClick={() => cancel.mutate()}
        >
          {t('Cancel')}
        </Button>
      </Dialog>
      {/* 敏感确认复用站点已有的二次验证，不采集原厂密码。 */}
      <SecureVerificationDialog {...verification.dialogProps} />
    </>
  )
}
