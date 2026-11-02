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
import { Textarea } from '@/components/ui/textarea'
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
  getHostingModels,
  testHostingAccount,
  HostingAuthorizationError,
  resumeHostingAuthorization,
  refreshHostingObservation,
  startHostingAuthorization,
} from '../api'
import {
  connectionSchema,
  type HostingAccount,
  type HostingAuthorization,
} from '../types'
import { AccountQuota } from './account-quota'

// 按参考项目展示生成链接、浏览器授权、提交回调三步，不提供授权方式选择。
// 业务弹窗复用项目 Dialog；仓库没有可复用的 Steps 或 FormDialog。
// 三步始终展示为有序卡片，复制、字段、提示和加载仍使用已有组件。
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
  // 添加已经成功后才发起一次只读刷新，额度失败不能反转保存结果。
  // 独立 mutation 保留失败状态，用户可重试额度而不重新提交授权。
  // 禁止自动重试，避免把页面重渲染变为持续访问原厂。
  // 查询目标取创建响应的稳定 ID，不能使用新建前的占位账号编号。
  // 重授权也返回同一账号 ID，因此可以同步更新该账号的旧快照。
  // onSuccess 的单次触发依赖保存完成，不依赖 ready 状态轮询次数。
  // 首次查询尚在进行时允许关闭成功页，已经发出的只读请求仍会结束。
  // 请求状态与保存状态分离，额度错误不会开放再次提交回调的入口。
  // 重试按钮仅重做观测，不再次调用 createHostingAccount。
  // 成功返回部分观测时按原状态展示未知次数，不能改成查询全部成功。
  // 失败回调也刷新账号摘要，使服务端保存的观测错误能显示到列表。
  // 列表刷新读取缓存，不会为所有其他账号自动访问原厂。
  // 详情缓存按账号更新，避免重新打开详情还显示添加前的无额度状态。
  // 数据来源始终是标准观测 API，不能从授权身份字段推算套餐额度。
  // 不调用模型接口，首次查询本身不会产生推理耗额。
  // 不调用重置接口，即使观测发现窗口耗尽也只展示事实。
  const initialQuota = useMutation({
    mutationFn: refreshHostingObservation,
    retry: false,
    onSuccess: (value, id) => {
      client.setQueryData(['hosting', 'observation', id], value)
    },
    onSettled: () => {
      void client.invalidateQueries({ queryKey: ['hosting', 'accounts'] })
    },
  })
  // 保存成功后自动做一次真实测试，只有服务端确认 passed 才进入调度池。
  // 模型从该账号自己的目录选择，不硬编码型号，也不借平台目录补候选。
  // 首次连接和重授权共用此链路，旧账号不能沿用重授权前的验证事实。
  // 空目录、网络失败及未明结果均停在失败提示，不自动换模型重复耗额。
  // 重试关闭，同时沿用测试 API 的单次发送保护，认证恢复也不重放请求。
  // 测试与额度查询独立，额度查询失败不能覆盖已经完成的模型测试结果。
  // mutation 的目标只取保存响应编号，避免新建前的空账号引用串用权限。
  // 创建 mutation 仅成功触发一次，不由状态轮询或组件渲染重复发起。
  // 用户仍可在详情显式重试；自动链不消费原厂重置机会或平台余额。
  // 测试失败不会删除已经绑定的账号，便于查看状态和重新授权。
  const initialTest = useMutation({
    mutationFn: async (id: number) => {
      const catalog = await getHostingModels(id)
      const model = catalog.models[0]?.id
      if (!model) throw new Error('No models available')
      return testHostingAccount(id, model)
    },
    retry: false,
    meta: { errorToast: false }, // 页面保留失败说明，避免只弹一次提示后无法定位。
    onSettled: () => {
      void client.invalidateQueries({ queryKey: ['hosting', 'accounts'] })
      void client.invalidateQueries({
        queryKey: ['hosting', 'models', 'token'],
      })
    },
  })
  const save = useMutation({
    mutationFn: async () => {
      if (!authorization) throw new Error(t('Start authorization first'))
      return createHostingAccount(
        authorization.id,
        form.getValues('alias').trim()
      )
    },
    onSuccess: (account) => {
      void client.invalidateQueries({ queryKey: ['hosting', 'accounts'] })
      initialQuota.mutate(account.id)
      initialTest.mutate(account.id)
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
      let callback = form.getValues('callback_url').trim()
      // 完整回调原样交给后端校验，不替换其中的 state 或错误参数。
      // 仅输入 code 时，只使用当前授权链接中的 state 和服务端返回的回调地址。
      // 不猜测状态或回调端口；缺少上下文时仍要求完整回调。
      // 服务端继续校验所属会话、state、单次使用和原厂 PKCE，不放宽认证合同。
      // 解码仅用于复制出来的 code 参数值，避免百分号转义被二次编码。
      // URLSearchParams 负责参数转义，code 不能注入额外的查询字段。
      // 整个回调地址不经同样解码，保留用户实际返回的 state 与原始结构。
      // 非 URL 输入只接受单个紧凑 code，不把说明文字或多个参数当成授权码。
      // 校验异常发生在清空输入之前，用户可修正粘贴内容。
      // 原厂拒绝则保留已发送标记，不根据本地错误猜测 code 仍可复用。
      // 服务器响应不包含回调正文，因此同步状态不会重新写入敏感输入。
      // 仅复制 code 的便捷交互不新增后端接口，也不传递 PKCE verifier。
      if (!/^https?:\/\//i.test(callback)) {
        const stateValue = authorizationURL
          ? new URL(authorizationURL).searchParams.get('state')
          : null
        const redirect = current?.redirect_uri ?? authorization.redirect_uri
        if (
          !stateValue ||
          !redirect ||
          !/^[A-Za-z0-9._~%+-]+$/.test(callback)
        ) {
          throw new Error(t('Enter the complete callback URL.'))
        }
        const callbackURL = new URL(redirect)
        callbackURL.searchParams.set('state', stateValue)
        callbackURL.searchParams.set('code', decodeURIComponent(callback))
        callback = callbackURL.href
      }
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
  // 保存成功会自动测试模型，原厂重置不接入自动链路。
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
    initialTest.isPending ||
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
  // 浏览器 OAuth 的状态 GET 不返回初始 URL；省略字段不能抹掉已经取得的链接。
  // 只复用同一授权编号的首次响应，重开、过期和提交回调后不再开放旧链接。
  // 原始链接仅驻留当前弹窗，不能写入持久存储或另一会话的查询结果。
  // 查询返回显式的新链接时采用新值，只有省略字段才使用初始链接。
  // 初始响应与状态响应的编号必须相同，防止重新开始后串用旧登录页。
  // pending 是允许用户打开授权页的唯一阶段。
  // 进入 exchanging 后继续展示旧链接会诱导用户重复授权。
  // 本地 callbackSent 先于远端状态变化生效，关闭这段延迟窗口。
  // 过期与取消即使仍残留 URL，也不会产生可点击或可复制入口。
  // 链接可用性只负责浏览器导航，不代替后端原厂身份校验。
  // 服务端没有提供链接时不拼接 OAuth 参数，避免创建不匹配的 PKCE 流程。
  const initialURL =
    current?.id === authorization?.id
      ? authorization?.authorization_url
      : undefined
  const candidateURL = current?.authorization_url ?? initialURL
  if (
    current?.status === 'pending_authorization' &&
    !callbackSent &&
    candidateURL
  ) {
    try {
      const url = new URL(candidateURL)
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
  // 主操作固定在页脚，与参考手动授权窗口保持一致。
  // 页脚和字段读取同一份表单，不能让快捷提交绕过备注校验。
  // 完成后保留成功页，由用户决定返回账号详情或进入 Key 管理。
  // 关闭按钮沿用已有取消逻辑，异步写入期间不销毁操作上下文。
  // 按钮禁用只用于反馈；资源归属和单次消费仍由后端执行。
  // 回调尚未生成或已经提交时禁用，不让用户用空会话重复换码。
  // 输入校验先于提交，失败时保持草稿供修正。
  // 发送后的结果恢复沿用原会话与幂等编号，不重新申请授权。
  const submit = form.handleSubmit((values) => {
    if (!values.callback_url.trim()) {
      form.setError('callback_url', {
        message: t('Enter the authorization link or code.'),
      })
      return
    }
    complete.mutate()
  })
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
        description={`${t('Please follow the steps below to authorize your OpenAI account:')} ${t('After authorization, a short model test runs automatically and uses a small amount of provider quota.')}`}
        contentClassName='sm:max-w-3xl'
        showCloseButton={!busy}
        footer={
          <>
            <Button
              variant='outline'
              disabled={busy}
              onClick={() => cancel.mutate(false)}
            >
              {save.isSuccess ? t('Done') : t('Cancel')}
            </Button>
            {!save.isSuccess && (
              <Button
                disabled={
                  busy ||
                  Boolean(props.disabledReason) ||
                  callbackSent ||
                  current?.status !== 'pending_authorization'
                }
                onClick={() => {
                  void submit()
                }}
              >
                {(complete.isPending || save.isPending) && <Spinner />}
                {t('Complete authorization')}
              </Button>
            )}
          </>
        }
      >
        {save.isSuccess ? (
          <FieldGroup>
            {/* 保存和测试分别报告，只有真实测试通过才提示可调度。 */}
            <Alert>
              <AlertDescription>
                <p className='font-medium'>{t('Account added')}</p>
                {initialTest.isPending && (
                  <p role='status'>{t('Testing account connection…')}</p>
                )}
                {initialTest.isSuccess &&
                  initialTest.data.outcome === 'passed' && (
                    <p role='status'>
                      {t(
                        'Connection verified. This account can now be used by your API keys.'
                      )}
                    </p>
                  )}
                {(initialTest.isError ||
                  (initialTest.isSuccess &&
                    initialTest.data.outcome !== 'passed')) && (
                  <p role='alert'>
                    {t(
                      'Connection test did not pass. This account is excluded from routing. Retry in account details.'
                    )}
                  </p>
                )}
              </AlertDescription>
            </Alert>
            {/* 查询失败不隐藏已保存事实，也不要求用户重新授权。 */}
            {initialQuota.isPending && (
              <p role='status'>{t('Fetching account quota…')}</p>
            )}
            {initialQuota.isError && (
              <div role='alert' className='space-y-2'>
                <p>
                  {t(
                    'Account added, but quota could not be fetched. Please retry.'
                  )}
                </p>
                <Button
                  variant='outline'
                  onClick={() => initialQuota.mutate(save.data.id)}
                >
                  {t('Refresh quota')}
                </Button>
              </div>
            )}
            {initialQuota.isSuccess && (
              <AccountQuota observation={initialQuota.data} />
            )}
            {props.onManageKeys && (
              <Button
                disabled={initialTest.isPending}
                onClick={props.onManageKeys}
              >
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
            {/*
             * 备注不占授权步骤编号，用户先区分账号，再按三步完成手动授权。
             * 备注是平台显示名称，不作为原厂身份依据。
             * 已提交回调后锁定备注，自动保存继续使用用户确认的名称。
             * 重授权仍初始化已有名称，不把旧账号变为新账号。
             * 字段保留错误状态，生成链接前即可修正长度或空值。
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
            <section className='min-w-0 space-y-4 rounded-xl border border-blue-200 bg-blue-50/70 p-4 dark:border-blue-800 dark:bg-blue-950/20'>
              <h3 className='font-semibold text-blue-900 dark:text-blue-200'>
                {t('OpenAI account authorization')}
              </h3>
              {/*
               * 三步一直展示；尚未生成链接属于正常初始状态，不是假定的失败。
               * 有序列表给辅助技术提供步骤顺序，不依靠颜色表达先后。
               * 首次进入不渲染授权方式选择，也不预先申请 OAuth 会话。
               * 蓝色卡片承载说明，真正失败仍使用独立错误提示。
               * 长 URL 留在可滚动输入框内，不能撑开移动端窗口。
               */}
              <ol className='list-inside list-decimal space-y-4 marker:font-semibold marker:text-blue-600'>
                <li className='bg-background space-y-3 rounded-lg border border-blue-200 p-4 dark:border-blue-800'>
                  <span className='font-medium'>
                    {t(
                      'Click the button below to generate an authorization link'
                    )}
                  </span>
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
                  {/*
                   * 只读链接由首次授权响应保留；轮询只更新状态，不覆盖它。
                   * 链接展示和复制引用相同的已校验值。
                   * 复制失败沿用共享控件反馈，不把失败当成授权终止。
                   * 本步骤不读取原厂访问令牌或平台 API Key。
                   * 授权创建失败但带操作编号时保留恢复入口。
                   */}
                  {authorizationURL && (
                    <FieldGroup>
                      <Field>
                        <FieldLabel htmlFor='hosting-auth-url'>
                          {t('Authorization link')}
                        </FieldLabel>
                        <Input
                          id='hosting-auth-url'
                          value={authorizationURL}
                          readOnly
                          className='min-w-0 font-mono text-xs'
                        />
                      </Field>
                      <CopyButton
                        value={authorizationURL}
                        variant='outline'
                        size='default'
                        aria-label={t('Copy authorization link')}
                      >
                        {t('Copy authorization link')}
                      </CopyButton>
                    </FieldGroup>
                  )}
                  {/*
                   * starting 是尚未确认创建，不等于原厂授权失败；沿用原操作继续。
                   * 恢复请求使用原授权编号，不能重复创建供应商登录会话。
                   * 状态查询不消耗敏感证明，也不生成新授权链接。
                   * pending 且没有初始链接才说明无法继续打开登录页面。
                   * 已提交回调后不再显示链接缺失，避免正常换码被误报失败。
                   */}
                  {current?.status === 'starting' && (
                    <>
                      <p role='status' className='text-sm'>
                        {t('Generating authorization link...')}
                      </p>
                      <Button
                        variant='outline'
                        disabled={busy}
                        onClick={() => resume.mutate()}
                      >
                        {t('Resume authorization')}
                      </Button>
                    </>
                  )}
                  {current?.status === 'pending_authorization' &&
                    !callbackSent &&
                    !authorizationURL && (
                      <Alert variant='destructive'>
                        <AlertDescription>
                          {t(
                            'Unable to retrieve the authorization link. Start again to generate a new one.'
                          )}
                        </AlertDescription>
                      </Alert>
                    )}
                  {/* 终态才能提示重新授权；换码中和保存中不能以新会话掩盖结果。 */}
                  {failed && (
                    <Alert variant='destructive'>
                      <AlertDescription>
                        {t(
                          'Authorization ended. Start again to get a new link.'
                        )}
                      </AlertDescription>
                    </Alert>
                  )}
                  {current &&
                    !waiting &&
                    !busy &&
                    (failed ||
                      current.status === 'outcome_unknown' ||
                      (!callbackSent && !authorizationURL)) && (
                      <Button
                        variant='ghost'
                        onClick={() => cancel.mutate(true)}
                      >
                        {t('Start again')}
                      </Button>
                    )}
                </li>
                <li className='bg-background space-y-3 rounded-lg border border-blue-200 p-4 dark:border-blue-800'>
                  <span className='font-medium'>
                    {t(
                      'Open the link in your browser and complete authorization'
                    )}
                  </span>
                  <p className='text-sm'>
                    {t(
                      'Open the authorization link in a new tab, sign in to your OpenAI account, and authorize.'
                    )}
                  </p>
                  {/*
                   * 外链复用相同 URL 校验，不能从输入框或任意错误响应产生 href。
                   * 新标签保留当前弹窗，用户完成登录后可回到原步骤。
                   * noopener 隔离登录页面与当前控制台窗口。
                   * 生成前仍可阅读操作说明，打开按钮等真实链接到达后才出现。
                   * localhost 是手动复制回调的结束标志，不要求用户启动本地服务。
                   */}
                  {authorizationURL && (
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
                  )}
                  <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-200'>
                    <AlertDescription>
                      {t(
                        'Important: The page may take some time to load after authorization. Please wait. When the address bar starts with http://localhost, authorization is complete.'
                      )}
                    </AlertDescription>
                  </Alert>
                </li>
                <li className='bg-background space-y-3 rounded-lg border border-blue-200 p-4 dark:border-blue-800'>
                  <span className='font-medium'>
                    {t('Enter authorization link or code')}
                  </span>
                  <p className='text-sm'>
                    {t(
                      'After authorization, when the page address becomes http://localhost:xxx/auth/callback?code=...:'
                    )}
                  </p>
                  {/*
                   * 输入只在当前弹窗驻留；发送后撤下并清空，不存浏览器或查询缓存。
                   * 多行输入容纳参考说明与长回调，自动换行不修改实际值。
                   * 禁用拼写检查，避免浏览器对临时 code 提供文字修正。
                   * 用户可检查粘贴的内容，但成功页不会再次回显它。
                   * 纯 code 会补充当前会话上下文，完整 URL 的 state 原样提交。
                   * 地址只作为数据解析，浏览器和服务器都不抓取用户填入的 URL。
                   */}
                  {!callbackSent && (
                    <Field
                      data-invalid={Boolean(form.formState.errors.callback_url)}
                    >
                      <FieldLabel htmlFor='hosting-callback'>
                        {t('Authorization link or code')}
                      </FieldLabel>
                      <Textarea
                        id='hosting-callback'
                        autoComplete='off'
                        spellCheck={false}
                        rows={3}
                        disabled={
                          busy || current?.status !== 'pending_authorization'
                        }
                        placeholder={t(
                          'Option 1: Copy the full URL\n(http://localhost:xxx/auth/callback?code=...)\nOption 2: Copy only the code parameter value'
                        )}
                        aria-invalid={Boolean(
                          form.formState.errors.callback_url
                        )}
                        {...form.register('callback_url')}
                      />
                      <FieldDescription>
                        {t(
                          'You can paste the full URL or just the code parameter value. The system will recognize it automatically.'
                        )}
                      </FieldDescription>
                      {form.formState.errors.callback_url && (
                        <FieldDescription>
                          {t('Enter the authorization link or code.')}
                        </FieldDescription>
                      )}
                    </Field>
                  )}
                  {/*
                   * 结果未知时只查原操作，不能重新发送一次性 code。
                   * ready 后的保存错误只开放重试添加，不要求再次登录供应商。
                   * 校验阶段尚未发送的输入保留，用户可改成完整回调再提交。
                   * 等待中的换码与保存共用进度位置，避免跳转让用户误以为完成。
                   * 成功前不会自动测试模型，也不会改变任意 Key 的消费顺序。
                   */}
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
                  {complete.isError && !callbackSent && (
                    <Alert variant='destructive'>
                      <AlertDescription>
                        {t('Enter the complete callback URL.')}
                      </AlertDescription>
                    </Alert>
                  )}
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
                </li>
              </ol>
            </section>
          </FieldGroup>
        )}
      </Dialog>
      {/* 站点二次验证接管模态焦点，不与原厂授权方式混在一起。 */}
      <SecureVerificationDialog {...verification.dialogProps} />
    </>
  )
}
