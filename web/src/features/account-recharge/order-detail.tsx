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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { CREDIT_STATUS } from '@/features/credit/api'
import { OfficialPaymentQrDialog } from '@/features/wallet/components/dialogs/official-payment-qr-dialog'
import { submitPaymentForm } from '@/features/wallet/lib/payment'
import { toIntlLocale } from '@/i18n/languages'
import { hasPermission } from '@/lib/admin-permissions'
import { formatNumber } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import {
  actOrder,
  getOrder,
  payOrder,
  refundOrder,
  revealInfo,
  submitInfo,
} from './api'
import { RechargeOrderInvoice } from './order-invoice'
import { RechargeProductInfo } from './product-info'
import {
  orderStatus,
  rechargeActionLabels,
  rechargePaymentLabel,
} from './status'

// 用户与运营详情共用安全摘要，
// 权限来自服务端能力快照。
// 凭据只保存在当前弹层内存，
// 关闭、切换订单或离开页面即卸载。
// 表单和敏感读取不用mutation，
// 避免 ReactQuery 保存 session正文。
// 订单详情是购买者与运营人员共享的摘要容器，
// 两个路由仍具有独立授权。
// 操作权限不由页面标题、订单编号或管理员侧栏可见性推导。
// 浏览器持有的角色只是按钮呈现依据，
// 服务端每次请求重新核对身份。
// 买家模式不会渲染内部记录、转派输入或资金处理按钮。
// 业务员模式仅在取得处理权后开放资料揭示及后续办理。
// 管理员可按能力接管或转派，
// 但不能手工伪造付款成功。
// 用户看见的是平台人工办理状态，
// 不展示上游受理、接口结果或订单引用。
// 完成只表示工作人员确认了实际办理结果，
// 界面不能模拟供应商确认。
// 所有状态更新完成后重新读取安全摘要，
// 不在本地拼接下一状态。
// 订单版本是写入和敏感证明的必要条件，
// 不能用陈旧列表版本覆盖新分配。
// 已经完成、取消或进入退款的订单不能继续查看凭据及办理。
// 首期账户信息限定为邮箱与完整JSON，
// 不提供任意文件导入或批量导出。
// 浏览器凭据正文只存在当前表单及短时揭示状态，
// 不进入持久存储。
// 不使用 query 或mutation 保存session，
// 以免其变量和结果被缓存。
// 关闭弹层清空用户输入；
// 不同订单按编号重新挂载，
// 避免跨单资料混用。
// 复制操作复用公共组件，
// 其内部状态随敏感弹层关闭一起销毁。
// 清空内存不能撤回用户已经粘贴到外部系统的内容，
// 平台不作此承诺。
// 敏感内容短时可见并在窗口失焦后消失，
// 截图或剪贴板由操作人员自行管理。
// 审核退款与实际退款确认分别执行，
// 业务员申请不触发资金返还。
// 退款确认结果未知时保留原订单，
// 不能通过重新购买或新退款键恢复。
// 余额缓存恢复只同步已经退款的原单，
// 不允许输入任意返还金额。
// 微信退款凭证由管理员核实后填写，
// 本页不调用供应商充值或退款API。
// 用户资料提交只进入待领取队列，
// 不要求用户暴露浏览器密码。
// 链接固定指向 ChatGPT官网与会话页面，
// 不能由商品说明指定登录地址。
// 邮箱与 JSON的格式和一致性最终由服务端验证，
// 原厂有效性由人工核对。
// 业务说明与内部备注共用编辑区但采用不同动作保存，
// 提示明确可见范围。
// 任何说明都不得填入凭据，
// 正式敏感字段也不会进入处理历史。
// 页面轮询只读取当前摘要，
// 不能因轮询触发付款、领取或再充值。
// 加载失败保留明确错误状态，
// 不能显示空凭据或假完成结果。
// 订单金额始终展示人民币，
// 不受站点当前钱包币种切换影响。
// 辅助技术通过字段标签区分邮箱、session、处理说明及退款凭证。
// 繁体及简体界面语言在进入日期格式化前统一转换为有效locale。
// 付款二维码复用项目组件，
// 二维码关闭不表示收款失败或订单取消。
// 共享安全验证弹窗与确认弹窗依次出现，
// 避免两个模态焦点互相争抢。
// 请求忙碌期间禁用重复动作，
// 后端幂等仍负责网络重试与跨窗口竞争。
// 详情并不保存永久处理权，
// 转派和撤权后后续请求必须立即失败。
export function RechargeOrderDetail(props: {
  id: number
  operations: boolean
  onClose: () => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language) // 语言码先规范化，避免中文切换时日期格式化抛错。
  const client = useQueryClient() // 共享缓存只保存订单摘要，凭据另存短时组件状态。
  const user = useAuthStore((state) => state.auth.user)
  const verification = useSecureVerification() // 复用绑定当前登录会话的验证，不另收管理员密码。
  // 详情键包含订单编号和用户/运营模式，
  // 不能跨权限范围复用一份响应。
  // 定时读取可以发现处理人或版本变化，
  // 并帮助清理过时的凭据显示。
  // 前端轮询间隔不代表授权有效期；
  // 服务器对每次敏感请求重新验证。
  // 普通查询结果只含摘要，
  // 关闭后即使内存仍保留也没有原始session。
  // 操作后的刷新由同一查询客户端完成，
  // 列表与详情不会各自猜测状态。
  const detail = useQuery({
    queryKey: ['recharge', 'order', props.id, props.operations], // 个人和运营响应不能共用缓存项。
    queryFn: () => getOrder(props.id, props.operations), // 后端路径决定身份范围，前端筛选不能扩大权限。
    refetchInterval: 10000, // 轮询能发现转派和退款；不触发任何外部办理。
  })
  const order = detail.data?.order // 未取得摘要之前禁止绑定任何资金或资料操作。
  const [busy, setBusy] = useState(false) // 只管理本窗口交互，不能当作跨进程订单锁。
  const [email, setEmail] = useState('') // 用户输入与历史掩码分开，不自动补全到真实邮箱。
  const [session, setSession] = useState('') // 保留原始字符串，不能 trim 后意外改动用户提供的 JSON。
  const [note, setNote] = useState('') // 每次打开详情都要求人工填写当前处理依据。
  const [assignee, setAssignee] = useState('') // 空输入不默认为当前账号，转派需要明确目标。
  const [reference, setReference] = useState('') // 支付交易号不能被自动用作退款凭证号。
  const [action, setAction] = useState<string | null>(null) // 确认框只保存待执行动作，不保存认证证明。
  const [qr, setQR] = useState<{ value: string; tradeNo: string } | null>(null)
  const [secret, setSecret] = useState<{
    email: string
    session: string
    version: number
  } | null>(null)
  // 凭据窗口短时展示，
  // 页面失焦或版本改变立即清空。
  // 转派不会撤回剪贴板内容，
  // 但平台不再提供后续读取权限。
  useEffect(() => {
    if (!secret) return
    const clear = () => setSecret(null) // 计时器与窗口失焦共用同一清理动作。
    const timeout = window.setTimeout(clear, 60000) // 超时从本次揭示开始，不因普通轮询续期。
    window.addEventListener('blur', clear) // 离开操作窗口时收起凭据，避免留在后台屏幕。
    return () => {
      window.clearTimeout(timeout) // 窗口关闭后不保留上一单的计时回调。
      window.removeEventListener('blur', clear) // 卸载时释放敏感状态闭包，避免长期事件引用。
    }
  }, [secret])
  useEffect(() => {
    if (secret && order && secret.version !== order.version) setSecret(null)
  }, [order, secret])
  // 读权限不隐含写权限，
  // 管理员也可能被撤销某个动作能力。
  // 处理人关系与操作能力同时满足才展示办理按钮，
  // 不能仅比较数字角色。
  // 财务角色不因能够处理发票就得到代充值账户访问权。
  // 退款能力独立计算，
  // 避免业务员提交退款申请时顺带执行资金动作。
  // 管理员转派能力不能让未经安全验证的查看动作绕过敏感证明。
  const canWrite = hasPermission(user, 'recharge_orders', 'write') // 办理能力不包含退款审批。
  const canAssign = hasPermission(user, 'recharge_orders', 'assign') // 管理员可接管，业务员没有跨人处理能力。
  const canRefund = hasPermission(user, 'recharge_orders', 'refund') // 资金操作单独授权，不能从写权限推导。
  const ownsOrder = order && (order.assignee_id === user?.id || canAssign)
  const canReveal =
    !detail.isError && // 刷新失败说明当前权限无法确认，不能继续使用旧敏感入口。
    props.operations &&
    ownsOrder &&
    hasPermission(user, 'recharge_orders', 'reveal') &&
    order?.payment_status === 'paid' && // 批准退款即停止敏感访问，即使办理状态尚未清空。
    ['processing', 'exception'].includes(order.status)
  // 权限撤销后的旧摘要不应保留敏感内容，
  // 服务端仍逐请求拒绝。
  useEffect(() => {
    if (!canReveal) setSecret(null)
  }, [canReveal])
  // 刷新只使本业务的安全查询失效，
  // 不影响其他钱包或模型调用状态。
  // 不能用刷新触发付款，
  // 因此 GET 和invalidateQueries都没有资金副作用。
  // 支付窗口返回后仍需依赖服务器收到的真实签名通知确认是否成功。
  // 调用者等待刷新以便使用新版本，
  // 不手工递增本地版本去猜服务器结果。
  async function refresh() {
    await client.invalidateQueries({ queryKey: ['recharge'] }) // 操作后同步列表和详情，避免不同页面显示矛盾状态。
  }
  // 继续付款固定原订单编号，
  // 不重新调用商品创建接口。
  // 网关返回的表单交给现有支付工具提交，
  // 避免另写浏览器兼容逻辑。
  // 官方二维码内容原样传入公共二维码组件，
  // 不在前端拼接收款金额。
  // 跳转与二维码是不同交互，
  // 不能把二维码字符串交给location 打开。
  // 余额成功也只刷新原订单，
  // 不在页面自行减去或增加展示余额。
  // 请求失败交给统一错误处理，
  // 当前订单仍保留供用户核对。
  // 忙碌状态在 finally恢复，
  // 关闭外部付款窗口不会伪造成功回调。
  // 没有自动切换付款方式的补偿逻辑，
  // 避免一个有效微信单再扣平台余额。
  async function pay() {
    if (busy) return
    setBusy(true) // 同窗口避免双击；跨进程幂等仍由服务端保证。
    try {
      const result = await payOrder(props.id) // 失败不重新创建单据，用户可以从同一 ID 核对。
      if (result.payment_kind === 'qr') {
        setQR({ value: result.qr_code, tradeNo: result.trade_no }) // 二维码绑定原订单，不创建新的支付身份。
      }
      if (result.payment_kind === 'redirect') {
        window.location.assign(result.redirect_url) // 目标来自经验证的支付响应，不读取商品描述链接。
      }
      if (result.payment_kind === 'form') {
        submitPaymentForm(result.url, result.params) // 沿用公共工具处理浏览器差异，不拼接可执行 HTML。
      }
      await refresh()
    } catch (error) {
      handleServerError(error) // 统一处理器不会打印含 session 的 Axios 请求对象。
    } finally {
      setBusy(false) // 失败后保留原单重试入口，不锁死整个页面。
    }
  }
  // 提交来自用户明确的表单事件，
  // 页面加载或输入变化不会自动发送资料。
  // 版本来自当前详情，
  // 服务端会拒绝已完成、退款或已替换的旧订单。
  // 成功后先清空邮箱和原始JSON，
  // 再刷新服务器状态。
  // 失败时保留当前输入便于核对，
  // 但错误提示不能回显正文。
  // 提交不用 React Querymutation，
  // 防止凭据保留在操作历史缓存。
  // 原订单补交与首次提交共用接口，
  // 前端不生成第二笔付费购买。
  // 表单的邮箱类型和长度限制只是输入辅助，
  // 不能代替服务端校验。
  // 实际 JSON必须来自用户自己的账户，
  // 本页不尝试登录或抓取原厂会话。
  async function submit(event: React.FormEvent) {
    event.preventDefault() // 阻止浏览器原生表单提交把资料带到页面导航。
    if (!order || busy) return
    setBusy(true)
    try {
      await submitInfo(order.id, order.version, email, session) // 邮箱与会话一次提交，不形成不完整账户资料。
      setEmail('') // 清理输入状态不会修改账户本人保存的登录邮箱。
      setSession('') // 服务端成功后立即移除页面持有的原厂凭据。
      await refresh()
    } catch (error) {
      handleServerError(error)
    } finally {
      setBusy(false)
    }
  }
  // 确认弹层先关闭，
  // 再请求敏感验证，
  // 避免验证框被另一模态挡住。
  // 任何办理动作开始前清除当前揭示内容，
  // 减少后续状态变化留下旧凭据。
  // 读取和退款分别传明确动作，
  // 单次证明不能跨动作复用。
  // 证明上下文包含订单编号和当前版本，
  // 转派或补交后必须重新验证。
  // 用户取消安全验证时不发送业务请求，
  // 也不声称动作已完成。
  // 敏感读取推进版本，
  // 先刷新摘要再显示对应版本的凭据。
  // 读取失败保持隐藏状态，
  // 不能用上一次成功资料作为降级结果。
  // 批准退款不自动视为退款已到账，
  // 资金确认由另一个动作完成。
  // 余额退款恢复仍发送原订单和当前版本，
  // 服务器负责幂等返还。
  // 普通办理仅提交动作、说明和必要处理人编号，
  // 不提交整个订单对象。
  // 标记完成前展示确认说明，
  // 防止用户将填写内部备注误当作履约确认。
  // 服务端冲突错误需要刷新核对，
  // 不能自动重试外部充值来解决。
  // 操作成功后让共享安全查询失效，
  // 后台列表能看到同一持久结果。
  // finally只恢复按钮状态，
  // 不把捕获到的错误改写为成功结果。
  async function execute(next: string) {
    if (!order || busy) return
    setAction(null) // 不保留已消费确认，下一次操作须重新选择。
    setBusy(true)
    setSecret(null) // 业务动作期间撤下旧资料，防止边读边变更状态。
    try {
      if (['reveal', 'refund', 'refund_confirm'].includes(next)) {
        const verifiedAction = next as 'reveal' | 'refund' | 'refund_confirm'
        const proof = await verification.requestVerification({
          scope: 'recharge.manage',
          context: {
            order_id: order.id, // 证明只能用于当前订单，不能拿去读取另一单。
            version: order.version, // 转派或重新提交资料后原证明上下文失效。
            action: verifiedAction, // 查看资料的证明不能用于确认退款。
          },
        })
        if (!proof) return // 取消验证不消耗订单状态，也不执行后续动作。
        if (next === 'reveal') {
          const result = await revealInfo(
            order.id,
            order.version,
            proof.proof_token // 证明仅用于本次请求，不写入查询缓存或持久存储。
          )
          // 审计读取推进版本，
          // 先刷新安全摘要再展示当前凭据。
          const latest = await detail.refetch()
          // 重新鉴权失败或订单已被转派时丢弃本次响应，
          // 不能把旧凭据重新放回页面。
          if (
            latest.isError ||
            latest.data?.order.version !== order.version + 1
          ) {
            return
          }
          setSecret({ ...result, version: order.version + 1 })
        } else {
          await refundOrder(
            order.id,
            order.version,
            next === 'refund_confirm', // 审批冻结办理，确认才执行或登记资金结果。
            note,
            reference,
            proof.proof_token
          )
          await refresh() // 退款结果以服务端重新读取为准，不本地猜测已退款。
        }
      } else {
        await actOrder(order.id, order.version, next, note, Number(assignee)) // 服务器校验状态版本及领取人，旧页面不可覆盖新操作。
        await refresh()
      }
    } catch (error) {
      handleServerError(error)
    } finally {
      setBusy(false)
    }
  }
  // 业务员动作集合在此显式列举，
  // 不能根据用户输入生成任意后端动作。
  // 要求补资料必须提供用户可见原因，
  // 用户才能在原单继续处理。
  // 完成与退款申请互斥，
  // 申请未审核时不能直接宣布订单履约完成。
  // 内部备注只更新内部记录，
  // 不覆盖买家已经看到的处理说明。
  // 管理员拒绝退款后可继续核实办理，
  // 拒绝动作本身不会改动资金。
  // 后端仍有独立白名单，
  // 隐藏或修改浏览器按钮不能扩展权限。
  const actions = [
    ['need_info', 'Request additional information'],
    ['complete', 'Mark completed'],
    ['exception', 'Report an issue'],
    ['request_refund', 'Request refund'],
    ['note', 'Save internal note'],
  ]
  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open) {
            setSecret(null)
            setEmail('')
            setSession('')
            props.onClose()
          }
        }}
        title={t(
          props.operations ? 'Recharge order management' : 'Recharge order'
        )}
        description={order?.trade_no}
        descriptionClassName='break-all'
        contentClassName='sm:max-w-5xl'
        bodyClassName='lg:h-full'
        contentHeight='min(76dvh, 780px)'
      >
        {detail.isLoading && <LoadingState />}
        {detail.isError && <ErrorState onRetry={() => void detail.refetch()} />}
        {order && (
          <div className='grid min-h-0 gap-6 [overflow-wrap:anywhere] lg:h-full lg:grid-cols-2'>
            {/* 信息与操作各自滚动；手机使用外层单一滚动容器。 */}
            <section
              aria-label={t('Order information')}
              className='min-w-0 space-y-4 lg:min-h-0 lg:overflow-y-auto lg:pr-3'
            >
              <div className='rounded-xl border p-4'>
                <RechargeProductInfo product={order} />
              </div>
              {/* 金额、支付与办理状态分开标注，付款不等于已完成。 */}
              <div className='rounded-xl border p-4'>
                <h3 className='mb-3 text-sm font-semibold'>
                  {t('Order information')}
                </h3>
                <dl className='grid grid-cols-2 gap-4 text-sm'>
                  <div>
                    <dt className='text-muted-foreground text-xs'>
                      {t('Amount')}
                    </dt>
                    <dd className='mt-1 font-semibold'>
                      CNY {formatNumber(order.money_cents / 100, locale)}
                    </dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground text-xs'>
                      {t('Payment method')}
                    </dt>
                    <dd className='mt-1'>{t(rechargePaymentLabel(order))}</dd>
                  </div>
                  <div className='col-span-2'>
                    <dt className='text-muted-foreground text-xs'>
                      {t('Order status')}
                    </dt>
                    <dd className='mt-1 font-medium'>
                      {t(orderStatus(order, props.operations))}
                    </dd>
                  </div>
                </dl>
              </div>
              {/* 发票跟随订单详情刷新，独立于套餐说明和办理反馈。
                  仅买家详情展示，业务员不借订单读权限查看税务资料。
                  查询失败时收起旧下载入口，不能将缓存当作当前授权。 */}
              {!props.operations && !detail.isError && (
                <RechargeOrderInvoice
                  required={order.invoice_required}
                  invoice={detail.data?.invoice}
                />
              )}
              {/* 管理员退回原因独立展示，不能误读为商品条款。 */}
              {order.public_note && (
                <section
                  aria-label={t(
                    order.status === 'action_required'
                      ? 'Reason for additional information'
                      : 'Processing result'
                  )}
                  className='rounded-xl border border-amber-500/30 bg-amber-500/5 p-4'
                >
                  <h3 className='text-sm font-semibold'>
                    {t(
                      order.status === 'action_required'
                        ? 'Reason for additional information'
                        : 'Processing result'
                    )}
                  </h3>
                  <p className='mt-2 text-sm [overflow-wrap:anywhere] whitespace-pre-wrap'>
                    {order.public_note}
                  </p>
                </section>
              )}
              {props.operations && (
                <div className='space-y-4'>
                  {detail.data?.internal_note && (
                    <p className='whitespace-pre-wrap'>
                      {t('Internal note')}: {detail.data.internal_note}
                    </p>
                  )}
                  {/* 历史只包含动作元数据和说明，没有完整邮箱或凭据。 */}
                  {detail.data?.events && (
                    <div className='space-y-2'>
                      <h3 className='font-medium'>{t('Processing history')}</h3>
                      {detail.data.events.map((event) => (
                        <div
                          key={event.id}
                          className='rounded-md border p-2 text-sm'
                        >
                          <p>
                            {t(
                              rechargeActionLabels[event.action] ?? event.action
                            )}{' '}
                            · #{event.actor_id} ·{' '}
                            {new Date(event.created_at * 1000).toLocaleString(
                              locale
                            )}
                          </p>
                          <p className='whitespace-pre-wrap'>{event.note}</p>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </section>
            <section
              aria-label={t('Order actions')}
              className='min-w-0 space-y-4 lg:min-h-0 lg:overflow-y-auto lg:border-l lg:pr-2 lg:pl-6'
            >
              <div className='flex items-center justify-between gap-3'>
                <h3 className='font-semibold'>{t('Order actions')}</h3>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => void refresh()}
                  disabled={detail.isFetching}
                >
                  {t('Refresh order')}
                </Button>
              </div>
              {!props.operations &&
                !['awaiting_info', 'action_required'].includes(
                  order.status
                ) && (
                  <p className='text-muted-foreground text-sm'>
                    {t(
                      'Follow the order status and processing result in Order information.'
                    )}
                  </p>
                )}
              {detail.data?.credit_status && (
                <p role='status'>
                  {t('On account')}:{' '}
                  {t(CREDIT_STATUS[detail.data.credit_status])}
                </p>
              )}
              {!props.operations && order.payment_status === 'pending' && (
                <Button onClick={() => void pay()} disabled={busy}>
                  {t(
                    order.payment_provider === 'credit'
                      ? 'Confirm on-account payment'
                      : 'Continue payment'
                  )}
                </Button>
              )}
              {!props.operations &&
                order.payment_status === 'paid' &&
                ['awaiting_info', 'action_required'].includes(order.status) && (
                  <form
                    onSubmit={(event) => void submit(event)}
                    className='space-y-4'
                  >
                    {/* 用户按官网操作收集资料；本页不访问或登录其原厂账户。 */}
                    <h4 className='text-sm font-medium'>
                      {t('How to prepare account information')}
                    </h4>
                    <ol className='bg-muted/40 list-decimal space-y-2 rounded-lg py-3 pr-3 pl-8 text-sm'>
                      <li>
                        {t('Sign in to ChatGPT.com in your browser.')}{' '}
                        <a
                          className='underline'
                          href='https://chatgpt.com'
                          target='_blank'
                          rel='noreferrer'
                        >
                          {t('Open ChatGPT')}
                        </a>
                      </li>
                      <li>
                        {t(
                          'In the same browser, open the account session page.'
                        )}{' '}
                        <a
                          className='break-all underline'
                          href='https://chatgpt.com/api/auth/session'
                          target='_blank'
                          rel='noreferrer'
                        >
                          https://chatgpt.com/api/auth/session
                        </a>
                      </li>
                      <li>
                        {t(
                          'Copy the complete JSON and enter the email of the account to recharge.'
                        )}
                      </li>
                    </ol>
                    <Field>
                      <FieldLabel htmlFor='recharge-email'>
                        {t('Account email')}
                      </FieldLabel>
                      <Input
                        id='recharge-email'
                        type='email'
                        autoComplete='off'
                        required
                        maxLength={254}
                        value={email}
                        onChange={(event) => setEmail(event.target.value)}
                      />
                    </Field>
                    <Field>
                      <FieldLabel htmlFor='recharge-session'>
                        {t('Complete session JSON')}
                      </FieldLabel>
                      <Textarea
                        id='recharge-session'
                        autoComplete='off'
                        spellCheck={false}
                        required
                        maxLength={65536}
                        rows={5}
                        className='max-h-48 resize-y font-mono text-xs'
                        value={session}
                        onChange={(event) => setSession(event.target.value)}
                      />
                    </Field>
                    <p className='text-muted-foreground text-sm'>
                      {t(
                        'This information grants account access. Only submit it for an order you authorized. It is encrypted and removed from the active order when processing ends.'
                      )}
                    </p>
                    <Button type='submit' disabled={busy}>
                      {t('Submit for processing')}
                    </Button>
                  </form>
                )}
              {props.operations && (
                <div className='space-y-4'>
                  <p>
                    {t('Account email')}: {order.email_mask || '—'} ·{' '}
                    {t('Assignee')}: {order.assignee_id || '—'}
                  </p>
                  {/* 未领取订单可领取，其他人员订单只显示摘要。 */}
                  {canWrite &&
                    order.payment_status === 'paid' &&
                    order.status === 'pending' &&
                    !order.assignee_id && (
                      <Button
                        disabled={busy}
                        onClick={() => void execute('claim')}
                      >
                        {t('Claim order')}
                      </Button>
                    )}
                  {canReveal && (
                    <Button
                      variant='outline'
                      disabled={busy}
                      onClick={() => void execute('reveal')}
                    >
                      {t('View account information')}
                    </Button>
                  )}
                  {(ownsOrder || canRefund) && (
                    <>
                      <Field>
                        <FieldLabel htmlFor='recharge-note'>
                          {t('Processing note')}
                        </FieldLabel>
                        <Textarea
                          id='recharge-note'
                          value={note}
                          maxLength={1000}
                          onChange={(event) => setNote(event.target.value)}
                        />
                      </Field>
                      <p className='text-muted-foreground text-sm'>
                        {t(
                          'Processing results are visible to the customer. Internal notes and assignment reasons are staff-only. Never enter session credentials in notes.'
                        )}
                      </p>
                    </>
                  )}
                  {ownsOrder &&
                    canWrite &&
                    order.payment_status === 'paid' &&
                    ['processing', 'exception'].includes(order.status) && (
                      <div className='flex flex-wrap gap-2'>
                        {actions.map(([key, label]) => (
                          <Button
                            key={key}
                            variant='outline'
                            disabled={
                              busy ||
                              !note.trim() ||
                              (key === 'complete' && order.refund_requested)
                            }
                            onClick={() => setAction(key)}
                          >
                            {t(label)}
                          </Button>
                        ))}
                      </div>
                    )}
                  {/* 转派需明确原因及目标编号，不能把打开详情当作自动接管。 */}
                  {canAssign &&
                    order.payment_status === 'paid' &&
                    !['completed', 'awaiting_info', 'cancelled'].includes(
                      order.status
                    ) && (
                      <div className='flex flex-wrap items-end gap-2'>
                        <Field className='max-w-52'>
                          <FieldLabel htmlFor='recharge-assignee'>
                            {t('Assignee user ID')}
                          </FieldLabel>
                          <Input
                            id='recharge-assignee'
                            type='number'
                            min={1}
                            value={assignee}
                            onChange={(event) =>
                              setAssignee(event.target.value)
                            }
                          />
                        </Field>
                        <Button
                          variant='outline'
                          disabled={
                            busy || !note.trim() || Number(assignee) <= 0
                          }
                          onClick={() => setAction('assign')}
                        >
                          {t('Assign order')}
                        </Button>
                      </div>
                    )}
                  {canRefund &&
                    order.status !== 'completed' &&
                    (['paid', 'refunding'].includes(order.payment_status) ||
                      order.wallet_sync_pending) && (
                      <div className='space-y-3 rounded-lg border p-3'>
                        <p className='text-sm'>
                          {order.payment_provider === 'credit' && (
                            <span>
                              {t(
                                'Unsettled credit is cancelled on refund. Settled credit requires an actual refund reference.'
                              )}
                            </span>
                          )}
                          {t(
                            'Approve a refund only after confirming that external processing has stopped. For WeChat, complete the original-channel refund in the gateway before recording confirmation.'
                          )}
                        </p>
                        {order.payment_status === 'refunding' &&
                          order.payment_provider !== 'wallet' &&
                          (order.payment_provider !== 'credit' ||
                            detail.data?.credit_status === 'settled') && (
                            <Field>
                              <FieldLabel htmlFor='recharge-refund-ref'>
                                {t('Gateway refund reference')}
                              </FieldLabel>
                              <Input
                                id='recharge-refund-ref'
                                maxLength={128}
                                value={reference}
                                onChange={(event) =>
                                  setReference(event.target.value)
                                }
                              />
                            </Field>
                          )}
                        <Button
                          variant='destructive'
                          disabled={
                            busy ||
                            !note.trim() ||
                            (order.payment_status === 'refunding' &&
                              order.payment_provider !== 'wallet' &&
                              (order.payment_provider !== 'credit' ||
                                detail.data?.credit_status === 'settled') &&
                              !reference.trim())
                          }
                          onClick={() =>
                            setAction(
                              order.payment_status === 'paid'
                                ? 'refund'
                                : 'refund_confirm'
                            )
                          }
                        >
                          {t(
                            order.payment_status === 'paid'
                              ? 'Approve refund'
                              : 'Confirm refund'
                          )}
                        </Button>
                      </div>
                    )}
                  {canRefund &&
                    order.refund_requested &&
                    order.payment_status === 'paid' && (
                      <Button
                        variant='outline'
                        disabled={busy || !note.trim()}
                        onClick={() => setAction('reject_refund')}
                      >
                        {t('Reject refund request')}
                      </Button>
                    )}
                </div>
              )}
            </section>
          </div>
        )}
      </Dialog>
      {/* 公共确认框承担不可逆办理结果确认，安全证明随后独立完成。 */}
      <ConfirmDialog
        open={!!action}
        onOpenChange={(open) => {
          if (!open) setAction(null)
        }}
        title={t('Confirm order action')}
        desc={t(
          'Confirm the actual processing result and account before continuing. A completed order cannot be reopened.'
        )}
        handleConfirm={() => {
          if (action) void execute(action)
        }}
        isLoading={busy}
      />
      <SecureVerificationDialog {...verification.dialogProps} />
      {secret && canReveal && (
        <Dialog
          open
          onOpenChange={() => setSecret(null)}
          title={t('Recharge account information')}
          description={t(
            'The information closes after one minute or when this window loses focus.'
          )}
        >
          <div className='space-y-3'>
            <p className='break-all'>
              {secret.email}
              <CopyButton value={secret.email} />
            </p>
            <Textarea
              readOnly
              value={secret.session}
              rows={8}
              aria-label={t('Complete session JSON')}
            />
            <CopyButton value={secret.session} size='default'>
              {t('Copy session JSON')}
            </CopyButton>
          </div>
        </Dialog>
      )}
      {qr && (
        <OfficialPaymentQrDialog
          open
          onOpenChange={() => setQR(null)}
          qrCode={qr.value}
          tradeNo={qr.tradeNo}
          refreshLabel={t('Refresh order')}
          onRefresh={async () => {
            const result = await detail.refetch()
            if (result.data?.order.payment_status !== 'pending') setQR(null)
            await refresh()
          }}
        />
      )}
    </>
  )
}
