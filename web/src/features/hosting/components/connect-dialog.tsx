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
import { useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import type { z } from 'zod'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { handleServerError } from '@/lib/handle-server-error'
import { AuthOperationError } from '@/lib/secure-verification'

import {
  cancelHostingAuthorization,
  completeHostingAuthorization,
  createHostingAccount,
  getHostingAuthorization,
  HostingAuthorizationError,
  resumeHostingAuthorization,
  startHostingAuthorization,
} from '../api'
import {
  connectionSchema,
  type HostingAccount,
  type HostingAuthorization,
} from '../types'

// 只借鉴参考项目的手动授权顺序，不复制管理员代理、批量导入或凭据输入。
// 业务弹窗复用项目 Dialog；仓库没有可复用的 Steps 或 FormDialog。
// 三步采用语义化有序列表，复制、字段、提示和加载仍使用已有组件。
// 账号备注先填写，让用户在申请授权之前知道自己正在添加哪个资源。
// 原厂身份授权和模型调用验证不同；本流程不发送真实模型请求。
// 完成按钮授权整条“交换回调→保存账号”流程，无需再次点击保存。
// 异步换码完成后只延续该用户动作，单纯读取 ready 不自动添加账号。
// 暂存的回调在发送前清除，不写入查询缓存、浏览器存储或导航地址。
// 用户进入官方页面自行登录，平台不收集原厂密码或 token 文件。
// disabledReason 由父页面服务状态查询产生，弹窗不重复探测控制面。
// 成功后由父页面导航，弹窗不绑定额外路由或创建另一套 Key 管理入口。
// 已有账号重授权仍传原 ID，服务端检查身份匹配和当前会话所属。
// 可用性提示沿用父页面已翻译文案，不把机器配置键展示给普通用户。
// 父页面在服务状态加载期阻止打开入口，弹窗处理后续状态改变。
// 后续即使接口再次失败，也保留真实错误而不生成假授权链接。
// 成功后的导航是可选回调，独立使用弹窗时仍可正常完成和关闭。
// 用户可以先关闭弹窗进入账号详情，不被强制跳转Key设置。
// 重授权保持相同业务账号，资源选择仍在原Key配置中独立管理。
// 这里不读取用户默认资源策略，也不在添加成功后自动修改Key。
// 账号可用额度、优先级和客户端协议导出不属于连接弹窗职责。
// 同一用户可以逐个添加多个账号，每次弹窗有独立会话引用。
// 不会将另一个弹窗中的原厂身份自动复用到当前备注。
// 原厂窗口的登录状态由用户浏览器管理，本组件不控制退出或切换账户。
// 长链接通过只读输入展示，避免撑宽移动端弹窗。
// 步骤中的普通文本由React转义，不把原厂响应当HTML插入。
// 所有按钮名称描述用户动作，不要求理解Stage、UUID或PKCE术语。
// 内部恢复机制仍保留稳定操作引用，但界面不把它当作需要填写的配置。
// 页面文案没有承诺免费无限额度，账号授权与额度观察是不同阶段。
// 用户取消后若服务端返回错误，弹窗不会静默消失并隐藏未决请求。
export function ConnectHostingDialog(props: {
  account?: HostingAccount
  disabledReason?: string
  onClose: () => void
  onManageKeys?: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const verification = useSecureVerification()
  const [authorization, setAuthorization] =
    useState<HostingAuthorization | null>(null)
  const [callbackSent, setCallbackSent] = useState(false)
  // 同一授权只自动保存一次；请求失败后由明确按钮按原引用重试。
  // 这个标记避免 ready 的轮询或 StrictMode 重复执行引发连续保存。
  // 它不替代服务端幂等，响应丢失时仍依赖 authorization_id 恢复。
  const saveStarted = useRef(false)
  // 备注与回调仍复用既有 Zod 契约，界面步骤不另建一份字段约束。
  // 生成链接时只触发备注校验，因为此时还没有可输入的回调。
  // 完成授权时才检查回调非空，避免第一步就出现无关错误。
  // 备注值在服务端保存前去掉首尾空白，不改变用户内部命名内容。
  // 重授权初始使用原备注，用户能认出正在操作的账号。
  // 回调发送后锁定备注，使后续自动保存对应用户提交时的含义。
  // 字段错误保留在各自输入旁，不只依赖短暂的 Toast。
  // 没有将回调注册为浏览器自动填充字段，减少共享设备遗留。
  // 表单状态只驻留当前弹窗，重新打开不能恢复上次的秘密输入。
  // 成功页不回显回调内容，也不展示原厂访问凭据。
  const form = useForm<z.infer<typeof connectionSchema>>({
    resolver: zodResolver(connectionSchema),
    defaultValues: { alias: props.account?.alias ?? '', callback_url: '' },
  })
  // 查询仅取得授权摘要，不交换 code、不消耗重置机会，也不做模型测试。
  // 查询键绑定会话 ID，重新开始之后不会读取上一次授权的缓存。
  // 中间状态持续同步，终态停止轮询，避免把过期状态伪装成处理中。
  // starting 是控制面创建未决，恢复必须走明确 POST，不能藏在 GET 内。
  // consuming 表示已开始账号创建，等待结果期间不能再提交回调。
  // outcome_unknown 保留原会话，用户可主动查询，不能自动创建新 UUID。
  // 这里不靠固定等待时长认定登录已经成功。
  // 原厂换码延迟与本地账号保存是两个可独立失败的异步边界。
  // pending_authorization 表示等待用户提交，不能据此开始保存。
  // exchanging 表示 code 已在处理，重复发送不能加速完成。
  // ready 说明已取得授权结果，但并不表示做过模型调用验证。
  // consumed 可用于恢复已保存但响应丢失的幂等结果。
  // 终态失败不会被自动替换成新会话，重新开始由用户明确触发。
  // 查询失败由统一错误链显示，不能伪造 ready 或账号已添加。
  // 刷新查询不需要重新证明，因为它不扩大已有授权的作用域。
  // 服务端仍验证原发起会话，换一个登录会话不能接管此流程。
  const state = useQuery({
    queryKey: ['hosting', 'authorization', authorization?.id],
    queryFn: () => {
      if (!authorization) throw new Error(t('Start authorization first'))
      return getHostingAuthorization(authorization.id)
    },
    enabled: Boolean(authorization),
    refetchInterval: (query) =>
      ['starting', 'pending_authorization', 'exchanging', 'consuming'].includes(
        query.state.data?.status ?? ''
      )
        ? 3000
        : false,
    gcTime: 0,
  })
  const current = state.data ?? authorization
  // 服务器摘要更新同时写入局部引用和当前查询键。
  // 否则轮询缓存中的旧状态可能在下一次渲染盖掉变更响应。
  // 这里只接受已经通过 API Schema 校验的数据。
  // 同步函数不包含隐式保存行为，开始和恢复也可以安全复用。
  // 敏感回调不在摘要中，不能把整个表单对象写入此缓存。
  // 异步保存的授权条件集中在下方 effect，避免多个入口各自添加。
  const sync = (value: HostingAuthorization) => {
    setAuthorization(value)
    client.setQueryData(['hosting', 'authorization', value.id], value)
  }
  // 一次性证明仍绑定连接或重授权动作，不能复用其他账号的证明。
  // 用户取消站点验证时不创建授权，也不展示业务失败提示。
  // 初次上游创建失败可能已经产生本地 ID，保留它供继续同一流程。
  // 错误仍可见，保存恢复引用不能被当成“生成链接成功”。
  // 生成链接前先验证站点用户身份，再申请原厂授权。
  // 初次连接的零账号编号是协议哨兵，不能解释成真实资源。
  // 重授权使用既有账号编号，防止另一个原厂身份覆盖错误账户。
  // 单次证明由 API 层标记不可自动重放，会话刷新也不能复制提交。
  // 只有用户再次点击生成或恢复才会执行相应变更。
  // 初始请求完全失败时没有授权引用，页面保持备注输入供重试。
  // 若服务器返回可恢复 ID，则下一步显示恢复入口而不是悄悄重建。
  // 站点验证取消不会清空已填备注，用户可以稍后继续。
  // 没有成功响应就不会伪造可点击的授权 URL。
  // 错误处理不直接输出 Axios 请求体，避免诊断日志携带秘密。
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
    onSuccess: sync,
    onError: (error) => {
      if (error instanceof HostingAuthorizationError) sync(error.authorization)
      if (!('code' in error && error.code === 'AUTH_CANCELLED')) {
        handleServerError(error)
      }
    },
  })
  // 保存仅发送授权引用和备注，不经过浏览器传递原厂凭据。
  // 连接成功保留成功页，避免弹窗骤然关闭后用户不知道下一步。
  // 重授权同样刷新列表，但不会更改该账号在任何 Key 中的来源位置。
  // 未调用模型测试 API，因此成功文案仅承诺“账号已添加”。
  // 浏览器只持有站点授权引用，账号身份从后端授权结果确定。
  // 备注不能替代原厂账号 ID，也不参与所属关系校验。
  // 同一引用保存可重试，这是后端持久幂等契约而非随机请求去重。
  // 自动保存完成后刷新列表，让用户在详情中看到真实后端状态。
  // 保存失败时保留授权上下文，不重新要求用户提交已消费的 code。
  // 重试按钮只重做本步骤，不再索取一份连接证明。
  // 服务端可能仍在配置执行资源，因此这里只宣告账号已添加。
  // 模型兼容性、可用额度与真实请求成功均不由本 mutation 推导。
  // 成功页关闭不会调用取消接口，避免取消已消费会话的冲突。
  // 导航到 Key 的动作不读取或复制任何明文 API Key。
  const save = useMutation({
    mutationFn: async () => {
      if (!authorization) throw new Error(t('Start authorization first'))
      return createHostingAccount(
        authorization.id,
        form.getValues('alias').trim()
      )
    },
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['hosting', 'accounts'] })
    },
  })
  // 发出回调后锁定输入，即使 HTTP 响应丢失也不重复发送一次性 code。
  // 失败改为查询结果，不能把同一秘密重新填回输入框让用户再试。
  // 后端仍校验 localhost 地址、OAuth state 和 PKCE，前端不推断身份。
  // mutation 参数不携带回调，避免它在 mutation 缓存中继续保留。
  // 读取回调发生在真正发请求的时刻，不复制到长期闭包或全局变量。
  // 清空后即使请求抛错也不重新填回原字符串。
  // callbackSent 表达单次凭证已经尝试发送，不等于授权成功。
  // 该标记决定隐藏输入，同时允许用户继续查询原会话结果。
  // 一次传输错误可能发生在原厂成功之后，不能保证 code 尚未消费。
  // 因此错误路径只触发只读状态同步，不自动再调用 callback。
  // 明确 ready 的响应会启动已授权的保存链，中间响应则继续等待。
  // 回调字符串从不作为 mutation variables 持久缓存。
  // 关闭后的服务器清理由显式取消负责，组件不伪造远端撤销结果。
  // 前端不从 callback 的 query 参数读取并保存账号身份。
  const complete = useMutation({
    mutationFn: async () => {
      if (!authorization) throw new Error(t('Start authorization first'))
      const callback = form.getValues('callback_url').trim()
      form.setValue('callback_url', '')
      setCallbackSent(true)
      return completeHostingAuthorization(authorization.id, callback)
    },
    onSuccess: sync,
    onError: (error) => {
      handleServerError(error)
      void state.refetch()
    },
  })
  // GET 的 ready 只有在用户已点击完成授权之后才能延续保存。
  // 标记先于请求设置，React 再渲染不能重复触发这条写入链。
  // 保存失败不自动重试，用户可检查错误后明确重试原授权引用。
  // consumed 可能是保存响应丢失后的状态，仍允许服务端幂等恢复。
  // 自动继续仅针对用户已经点击过“完成授权并添加”的会话。
  // 打开弹窗或加载缓存本身不代表用户批准新增账号。
  // 只读 GET 返回 ready 不单独构成写入触发条件。
  // 回调标记和原会话引用共同限定这条动作链。
  // saveStarted 在调用 mutation 前更新，阻止连续 ready 响应重复发送。
  // 保存失败保持该标记，因此失败不会形成无穷自动重试。
  // 显式重试仍使用同一个 mutation 和同一个授权引用。
  // 重新开始成功取消旧会话后才清除标记，下一会话有独立生命周期。
  // React Query 的 mutate 引用作为依赖，避免依赖整个易变的结果对象。
  // effect 不依赖备注输入内容，用户编辑文字不会触发账号创建。
  // 模型测试和原厂重置均没有接入此自动链路。
  // 保存成功后的列表失效不改变本会话的单次执行限制。
  const saveAccount = save.mutate
  useEffect(() => {
    if (
      !callbackSent ||
      saveStarted.current ||
      !['ready', 'consumed'].includes(current?.status ?? '')
    ) {
      return
    }
    saveStarted.current = true
    saveAccount()
  }, [callbackSent, current?.status, saveAccount])
  // starting 的恢复沿用原会话 ID，不生成第二份授权链接或新 state。
  // 查询结果与恢复创建分开，避免“刷新”按钮意外变成外部写入。
  // 恢复只用于已经存在的控制面操作，不能凭空构造授权 ID。
  // 请求路径沿用当前会话，不生成新的 OAuth state 或 PKCE 配置。
  // 它不接收回调字符串，也不从输入框提取新的 code。
  // 上游尚未创建时由服务端继续原幂等操作。
  // 上游已经存在时读取其状态，结果通过同一摘要同步函数更新。
  // 错误继续显示在当前弹窗，不假装用户已经打开原厂页面。
  const resume = useMutation({
    mutationFn: async () => {
      if (!authorization) throw new Error(t('Start authorization first'))
      return resumeHostingAuthorization(authorization.id)
    },
    onSuccess: sync,
  })
  // 重新开始必须先取消旧会话；取消失败继续保留旧引用，不能静默丢失。
  // 已经保存成功的关闭不再取消授权，避免对 consumed 会话请求冲突。
  // 用户重开时清掉回调锁与保存标记，备注保留便于重新尝试。
  // 取消授权和解绑已经保存的账号是两件不同业务动作。
  // 取消与重新开始共享清理流程，但退出目标不同。
  // 普通取消成功后交还父页面，不在后台再继续保存。
  // 重新开始成功后保留备注，仅销毁本次授权上下文。
  // 旧会话取消失败时不能清空本地引用，否则用户无法判断未决结果。
  // 服务端会阻止取消正在消费的授权，错误保留而不伪装撤销。
  // 用户重新开始前已经看到未知结果提示，动作不是静默重试。
  // 同一账号重新授权的取消不应删除原已绑定账号。
  // 重置局部 mutation 结果不会删除后台操作或改变账户余额。
  // 取消动作不访问原厂订阅设置，也不退出用户自己的浏览器登录。
  // 已添加账号只能通过详情中的解绑流程移除，不能用取消代替。
  const cancel = useMutation({
    mutationFn: async (restart: boolean) => {
      if (authorization && !save.isSuccess) {
        await cancelHostingAuthorization(authorization.id)
      }
      return restart
    },
    onSuccess: (restart) => {
      if (!restart) {
        props.onClose()
        return
      }
      setAuthorization(null)
      setCallbackSent(false)
      saveStarted.current = false
      form.setValue('callback_url', '')
      save.reset()
      complete.reset()
    },
  })
  // 网络写入期间统一禁用主要动作，避免连续点击创建两条流程。
  // 只读轮询不锁死整个弹窗，用户仍能退出等待中的授权。
  // 关闭时执行真实取消，取消失败继续保留可恢复上下文。
  // 等待状态用来解释进度，不用于猜测请求是否已经消耗额度。
  // failed、expired 与 cancelled 都是不能复用旧链接继续提交的终态。
  // 未知终态不假装失败，仍优先查询原操作结果。
  // 是否允许保存取决于明确的 ready 或 consumed，不按“不是失败”放行。
  const busy =
    start.isPending ||
    complete.isPending ||
    save.isPending ||
    resume.isPending ||
    cancel.isPending
  const waiting = ['exchanging', 'consuming'].includes(current?.status ?? '')
  const failed = ['failed', 'expired', 'cancelled'].includes(
    current?.status ?? ''
  )
  // 原厂跳转是新的信任边界，仅允许已知官方 HTTPS 域名。
  // 复制也使用经过同样验证的地址，不把异常服务响应提供给用户。
  // 回调地址是另一条协议，不能拿此处的官方域名规则去校验 localhost。
  // URL 先由标准解析器解析，不能使用字符串包含关系判断官方域名。
  // 类似域名或带 userinfo 的地址不能通过，避免视觉上伪装官方链接。
  // 只开放 HTTPS 页面，不能把原厂登录降级为明文传输。
  // 外部链接使用新窗口和隔离关系，原厂页面不能操作当前站点窗口。
  // 复制地址与打开地址来自同一个已校验值，避免两种入口信任不同。
  // 错误值不会被塞进 href，也不会回退到开发环境地址。
  // 模型请求的公开 API 地址不属于本弹窗，不混在原厂授权链接里。
  // 正常的 localhost 回调无法打开不代表身份授权失败。
  // 帮助文字解释复制完整地址，而不是让用户修复本机服务。
  let authorizationURL: string | undefined
  if (current?.authorization_url) {
    try {
      const url = new URL(current.authorization_url)
      if (
        url.protocol === 'https:' &&
        !url.username &&
        !url.password &&
        ['auth.openai.com', 'chatgpt.com'].includes(url.hostname)
      ) {
        authorizationURL = url.href
      }
    } catch {
      /* 非法地址显式显示不可用，不构造猜测的授权链接。 */
    }
  }
  // 展示文案描述用户下一步，而不是输出内部生命周期枚举。
  // 未决结果提示查询，不能暗示重新登录一定安全或必然必要。
  // 保存失败明确说明身份授权已完成，避免用户重复交付一次性 code。
  // 添加中与换码中区分，帮助定位卡在原厂还是控制面保存。
  // 成功状态由 save.isSuccess 单独渲染，不借 progress 的文字推断。
  // 加载指示是组件状态的表达，不设置虚构完成百分比。
  // 所有动态文案实时翻译，不能把语言固定在模块常量。
  let progress = t(
    'Authorization result is not confirmed. Check the result before starting again.'
  )
  if (waiting || complete.isPending) progress = t('Completing authorization…')
  if (save.isPending) progress = t('Adding account…')
  if (save.isError) {
    progress = t('Authorization is complete. Retry adding the account.')
  }
  const canSave = ['ready', 'consumed'].includes(current?.status ?? '')
  return (
    <>
      <Dialog
        open={!verification.isActive}
        onOpenChange={(open) => {
          if (!open && !busy) cancel.mutate(false)
        }}
        title={
          props.account ? t('Reauthorize account') : t('Connect Codex account')
        }
        description={t(
          'Authorize your own account in the browser, then return here to add it.'
        )}
        contentClassName='sm:max-w-xl'
        showCloseButton={!busy}
        footer={
          <Button
            variant='outline'
            disabled={busy}
            onClick={() => cancel.mutate(false)}
          >
            {save.isSuccess ? t('Done') : t('Cancel')}
          </Button>
        }
      >
        {save.isSuccess ? (
          <FieldGroup>
            {/* 身份连接不是模型测试，成功页解释剩余步骤而不显示“模型已验证”。 */}
            <Alert>
              <AlertDescription>
                <p className='font-medium'>{t('Account added')}</p>
                <p>
                  {t(
                    'Test the connection in account details, then select this account in an API key’s resource settings.'
                  )}
                </p>
              </AlertDescription>
            </Alert>
            {props.onManageKeys && (
              <Button onClick={props.onManageKeys}>
                {t('Go to API keys')}
              </Button>
            )}
          </FieldGroup>
        ) : (
          <FieldGroup>
            {props.disabledReason && (
              <Alert variant='destructive'>
                <AlertDescription>{props.disabledReason}</AlertDescription>
              </Alert>
            )}
            {/* 按用户任务顺序展开，未生成链接前不要求用户理解回调或服务器状态。 */}
            {/*
             * 步骤结构只是帮助用户完成当前任务，不是另建通用步骤组件。
             * 首屏只有备注和生成链接，减少尚未开始时的无关信息。
             * 取得会话后才出现原厂授权和回调步骤。
             * 有序列表提供天然阅读顺序，不需要用户理解后台服务部署。
             * 每个步骤标题说明动作，按钮名称说明实际产生的结果。
             * 不展示代理、倍率、令牌导入或管理员渠道选项。
             */}
            <ol className='list-inside list-decimal space-y-5 marker:font-semibold'>
              <li className='space-y-3'>
                <span className='font-medium'>{t('Name this account')}</span>
                {/*
                 * 备注首先校验，避免先申请一个最终因空备注无法保存的授权。
                 * 输入和错误说明通过稳定字段标识关联，支持辅助技术定位。
                 * 回调已发送后禁用编辑，保存期间不会偷偷改变账号名称。
                 * 重授权时展示原备注，让用户确认当前处理的是哪个账号。
                 */}
                <Field data-invalid={Boolean(form.formState.errors.alias)}>
                  <FieldLabel htmlFor='hosting-alias'>
                    {t('Account alias')}
                  </FieldLabel>
                  <Input
                    id='hosting-alias'
                    autoComplete='off'
                    maxLength={64}
                    disabled={busy || callbackSent}
                    aria-invalid={Boolean(form.formState.errors.alias)}
                    {...form.register('alias')}
                  />
                  {form.formState.errors.alias && (
                    <FieldDescription>
                      {t('Enter an account alias of 1 to 64 characters.')}
                    </FieldDescription>
                  )}
                </Field>
                {!authorization && (
                  <Button
                    disabled={busy || Boolean(props.disabledReason)}
                    onClick={async () => {
                      if (await form.trigger('alias')) start.mutate()
                    }}
                  >
                    {start.isPending && <Spinner />}
                    {t('Generate authorization link')}
                  </Button>
                )}
              </li>
              {current && (
                <>
                  <li className='space-y-3'>
                    <span className='font-medium'>
                      {t('Open the link and authorize')}
                    </span>
                    {/* 链接能打开也能复制，使用其他浏览器登录时不必手选长 URL。 */}
                    {authorizationURL && !failed ? (
                      <FieldGroup>
                        <Field>
                          <FieldLabel htmlFor='hosting-auth-url'>
                            {t('Authorization link')}
                          </FieldLabel>
                          <Input
                            id='hosting-auth-url'
                            value={authorizationURL}
                            readOnly
                            className='font-mono text-xs'
                          />
                        </Field>
                        <div className='flex flex-wrap gap-2'>
                          <Button
                            nativeButton={false}
                            render={
                              <a
                                href={authorizationURL}
                                target='_blank'
                                rel='noopener noreferrer'
                              />
                            }
                          >
                            {t('Open provider authorization')}
                          </Button>
                          {/*
                           * 复制链接复用站点的成功和失败反馈，不另写剪贴板状态。
                           * 用户可能需要换浏览器登录，保留复制入口比只提供打开按钮更可靠。
                           * 此按钮仅复制授权链接，不复制敏感回调、原厂凭据或平台 Key。
                           * 复制失败不会中止当前授权，用户仍可使用打开链接入口。
                           */}
                          <CopyButton
                            value={authorizationURL}
                            variant='outline'
                            size='default'
                            aria-label={t('Copy authorization link')}
                          >
                            {t('Copy authorization link')}
                          </CopyButton>
                        </div>
                      </FieldGroup>
                    ) : (
                      <Alert>
                        <AlertDescription>
                          {t(
                            'The authorization link is unavailable. Resume or start again.'
                          )}
                        </AlertDescription>
                      </Alert>
                    )}
                    {current.status === 'starting' && (
                      <Button
                        variant='outline'
                        disabled={busy}
                        onClick={() => resume.mutate()}
                      >
                        {t('Resume authorization')}
                      </Button>
                    )}
                  </li>
                  <li className='space-y-3'>
                    <span className='font-medium'>
                      {t('Paste the callback and add the account')}
                    </span>
                    {!callbackSent &&
                      !failed &&
                      current.status === 'pending_authorization' && (
                        <Field>
                          <FieldLabel htmlFor='hosting-callback'>
                            {t('Authorization callback URL')}
                          </FieldLabel>
                          {/*
                           * 回调以密码样式输入，避免公共屏幕直接暴露短期 code。
                           * 不要求用户手工拆解 code 或 state，降低复制遗漏导致的失败。
                           * 完整地址只交给同源 API，浏览器不会访问其 localhost 目标。
                           * 格式与身份校验仍在服务端完成，前端提示不代替安全边界。
                           * 字段错误不打印用户粘贴内容，只提供如何修正的说明。
                           * 输入只在等待回调时存在，发送后从界面撤下。
                           */}
                          <Input
                            id='hosting-callback'
                            type='password'
                            autoComplete='off'
                            disabled={busy}
                            aria-invalid={Boolean(
                              form.formState.errors.callback_url
                            )}
                            {...form.register('callback_url')}
                          />
                          <FieldDescription>
                            {t(
                              'After authorizing, copy the full localhost URL from the address bar, even if that page cannot open. Keep it private.'
                            )}
                          </FieldDescription>
                          {form.formState.errors.callback_url && (
                            <FieldDescription>
                              {t('Enter the complete callback URL.')}
                            </FieldDescription>
                          )}
                          <Button
                            disabled={busy || Boolean(props.disabledReason)}
                            onClick={form.handleSubmit((values) => {
                              if (!values.callback_url.trim()) {
                                form.setError('callback_url', {
                                  message: t(
                                    'Enter the complete callback URL.'
                                  ),
                                })
                                return
                              }
                              complete.mutate()
                            })}
                          >
                            {t('Complete authorization and add')}
                          </Button>
                        </Field>
                      )}
                    {/* 原始生命周期状态不直接展示；用户只看到当前等待或需要采取的动作。 */}
                    {callbackSent && (
                      <p
                        role='status'
                        className='flex items-center gap-2 text-sm'
                      >
                        {(waiting || save.isPending || complete.isPending) && (
                          <Spinner />
                        )}
                        {progress}
                      </p>
                    )}
                    {/*
                     * 只有保存阶段失败且授权已经完成，才提供重试添加。
                     * 这个恢复按钮不重新发 callback，也不会重新申请登录链接。
                     * 未知授权状态先查询结果，不能通过该按钮绕过状态门槛。
                     * 成功后的下一步引导仍需用户自己决定是否运行耗额测试。
                     */}
                    {save.isError && canSave && (
                      <Button disabled={busy} onClick={() => save.mutate()}>
                        {t('Retry adding account')}
                      </Button>
                    )}
                    {callbackSent && !waiting && !save.isPending && (
                      <Button
                        variant='outline'
                        disabled={busy || state.isFetching}
                        onClick={() => {
                          void state.refetch()
                        }}
                      >
                        {t('Check authorization result')}
                      </Button>
                    )}
                    {/*
                     * 终态提供重新开始入口，不让用户继续使用已经过期的链接。
                     * 后台未决时仍保留查询结果的路径，不把未知状态涂成成功。
                     * 新的授权只在旧会话明确取消之后创建，避免身份混用。
                     * 页面不会为加快流程自动切换到其他账号或付费来源。
                     */}
                    {failed && (
                      <Alert variant='destructive'>
                        <AlertDescription>
                          {t(
                            'Authorization ended. Start again to get a new link.'
                          )}
                        </AlertDescription>
                      </Alert>
                    )}
                    {!waiting &&
                      !busy &&
                      (failed ||
                        current.status === 'outcome_unknown' ||
                        !authorizationURL) && (
                        <Button
                          variant='ghost'
                          onClick={() => cancel.mutate(true)}
                        >
                          {t('Start again')}
                        </Button>
                      )}
                  </li>
                </>
              )}
            </ol>
          </FieldGroup>
        )}
      </Dialog>
      {/* 站点二次验证暂时接管模态焦点，不会索取原厂密码。 */}
      <SecureVerificationDialog {...verification.dialogProps} />
    </>
  )
}
