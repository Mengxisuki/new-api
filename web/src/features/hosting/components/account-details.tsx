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
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { handleServerError } from '@/lib/handle-server-error'
import { AuthOperationError } from '@/lib/secure-verification'

import {
  consumeResetCredit,
  deleteHostingAccount,
  getHostingModels,
  getHostingObservation,
  getResetOperations,
  getResetOperation,
  refreshHostingObservation,
  resumeResetOperation,
  testHostingAccount,
  updateHostingAccount,
} from '../api'
import type { HostingAccount, ResetOperation } from '../types'
import { AccountQuota } from './account-quota'

type Action = 'pause' | 'delete' | 'reset' | 'test' | 'resume'
// 详情按账号挂载，不为列表中的全部账号并发查询上游。
// 观测、模型发现和消费历史分别读取，任一失败不伪造完整可用状态。
// 详情围绕一个稳定账号编号组织，不允许在弹窗内切换所属账号。
// 父组件按编号重新挂载，防止前一个账号的消费 UUID 污染下一个。
// 用户备注可编辑，脱敏原厂身份只读。
// 连接成功与真实模型调用通过是不同状态，测试是单独显式动作。
// 暂停只影响新请求，不能承诺中止已到达原厂的执行。
// 恢复账号也不跳过后端凭据与状态校验。
// 重新授权复用原账号绑定，界面把它交给独立 OAuth 流程。
// 解绑前告知原厂授权不自动撤销，避免用户误认为已退出原厂账户。
// 账号免费使用不代表后续回退一定免费，暂停与解绑都提示来源回退。
// 额度查询、原厂刷新、重置机会消费是三种不同操作。
// 打开详情只查询缓存投影和模型目录，不自动消费额度或卡子。
// 模型测试可能消耗少量原厂额度，必须经过确认。
// 原厂重置机会是不可撤销的外部消费，必须绑定持久 UUID。
// HTTP 超时不能证明未扣卡，未知结果禁止再生成新操作。
// 后台重置历史用于页面重载后的恢复，不依赖浏览器存储。
// 一次成功消费不保证所有窗口都重置，确认文案明确这个边界。
// 重置结果和额度观察可以不同步，成功后仍可能显示同步中。
// 原厂卡子查询不新鲜时，即使曾观察到正次数也不能开放消费。
// 历史接口失败时不能断言没有未决操作，因此关闭新消费入口。
// 重置结果的只读刷新与继续未决操作分开按钮和接口。
// 继续未决操作再次执行站点证明，但沿用原 UUID。
// 用户取消站点验证不产生消费，也不弹出误导的失败提示。
// 业务确认与安全验证互斥打开，避免焦点在两个模态框间来回跳。
// 敏感动作中的证明只发给站点 API，执行层身份由后端处理。
// 备注修改、额度刷新和账号测试分别保留自己的请求状态。
// 任何一次失败都不能把其他查询的事实改写成零或已成功。
// 账号最后错误来自脱敏摘要，页面不展示完整上游响应或秘密。
// 状态更新后失效共享账号查询，让列表与详情保持一致。
// 解绑成功关闭详情，后续清理状态由服务端继续处理。
// 页面不操作原厂订阅付款、不购买重置卡，也不代替用户充值。
// 所有操作目标来自 props.account.id，不能通过表单覆盖 owner。
// 原厂额度与平台钱包始终分开显示和解释。
// 请求忙碌期间阻止危险动作并发，避免用户误解请求先后。
// 详情关闭只改变界面状态，不等价于撤销已经发送的外部操作。
export function HostingAccountDetails(props: {
  account: HostingAccount
  onClose: () => void
  onReauthorize: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const verification = useSecureVerification()
  const [action, setAction] = useState<Action | null>(null)
  const [alias, setAlias] = useState(props.account.alias)
  const [model, setModel] = useState('')
  // UUID 在结果明确前保留，HTTP 超时或同页重试绝不生成第二个消费操作。
  const resetId = useRef<string | null>(null)
  // 缓存额度查询与用户主动刷新分开，避免反复打开弹窗打满上游。
  // 查询键包含账号 ID，不能复用其他账号的快照。
  // 合法 null 表示尚无观测，界面不补造默认窗口。
  // 查询错误展示重试控件，用户能区分未观测与请求失败。
  // 模型目录只用于选择测试目标，不代表目录内模型全部经过验证。
  // 重置历史是持久事实，进入详情立即读取以阻止重复消费。
  // 三个查询相互独立，某一个失败不把另外两个结果改成假成功。
  // 刷新账号列表不触发所有账号的原厂额度刷新。
  const observation = useQuery({
    queryKey: ['hosting', 'observation', props.account.id],
    queryFn: () => getHostingObservation(props.account.id),
  })
  const models = useQuery({
    queryKey: ['hosting', 'models', props.account.id],
    queryFn: () => getHostingModels(props.account.id),
  })
  const operations = useQuery({
    queryKey: ['hosting', 'operations', props.account.id],
    queryFn: () => getResetOperations(props.account.id),
  })
  // prepared 和 outcome_unknown 都表示尚不能开始另一次消费。
  // 从服务端历史恢复未决身份，不凭前端最近一次请求状态判断。
  // 未决记录即使没有 result 也仍需保护，不能当作不存在。
  // 服务端确保同账号未决互斥，前端只选择用于恢复的记录。
  // 历史为空只有在成功查询之后才可解释为没有未决操作。
  const unresolved = operations.data?.items.find((item) =>
    ['prepared', 'outcome_unknown'].includes(item.status)
  )
  // 刷新额度不调用 consume，也不生成操作 UUID。
  // 成功快照直接写入当前账号的观测查询，保持列表详情共享契约。
  // 刷新返回部分观测仍保留它的新鲜度，不强行改成 fresh。
  // 失败继续展示原有历史快照及错误反馈，不把原额度归零。
  // 再次刷新是独立只读同步，不意味着撤回上次重置。
  const refresh = useMutation({
    mutationFn: () => refreshHostingObservation(props.account.id),
    onSuccess: (value) =>
      client.setQueryData(['hosting', 'observation', props.account.id], value),
  })
  // 改名只提交去掉首尾空白的备注，不提交整个账号 DTO。
  // 后端更新成功后重新读取账号列表，避免本地猜测新状态。
  // 空备注在按钮层阻止，服务端仍有最终长度约束。
  // 备注不参与策略身份，因此重命名不会打断来源排序。
  const rename = useMutation({
    mutationFn: () =>
      updateHostingAccount(props.account.id, { alias: alias.trim() }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['hosting', 'accounts'] })
    },
  })
  // 测试必须使用用户明确选中的真实模型 ID。
  // HTTP 成功仍需检查 outcome，失败和结果未明不能显示调用已验证。
  // 通过测试后刷新账号摘要，后端记录的精确模型与协议才是依据。
  // 客户端不把一次验证推广到其他型号或协议。
  // 测试结果提示保留协议和脱敏原因，便于用户区分兼容性问题。
  // 加载详情不触发本 mutation，避免无感消耗原厂额度。
  const test = useMutation({
    mutationFn: () => testHostingAccount(props.account.id, model),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['hosting', 'accounts'] })
    },
  })
  // 统一调度确认后的账号动作，避免弹窗路径绕过安全验证。
  // 暂停不消耗重置机会，因此无需生成重置操作 ID。
  // 测试交给独立 mutation 保留结果，仍由此处的确认入口触发。
  // 删除和重置都要取得动作绑定的站点安全证明。
  // 首次重置先生成 UUID，再请求绑定该 UUID 的证明。
  // 证明取消时保留 UUID 无害，真正消费仍未开始。
  // 同页网络重试保持原 UUID，不能把 HTTP 尝试当成新业务操作。
  // 恢复路径只接受当前服务端未决记录，不选择新的账号或新 UUID。
  // 删除的 operation_id 为空，不复用重置的证明上下文。
  // 发出消费请求后不因超时清空 UUID，先查询持久结果再决定。
  // 终态 rejected 同样是明确结果，允许用户后来创建新的独立操作。
  // succeeded 清理本地 UUID，但后台历史仍永久保留业务记录。
  const mutate = useMutation({
    mutationFn: async (next: Action) => {
      if (next === 'pause') {
        return updateHostingAccount(props.account.id, {
          paused: !props.account.paused,
        })
      }
      if (next === 'test') return test.mutateAsync()
      const operationId =
        next === 'reset'
          ? (resetId.current ??= crypto.randomUUID())
          : (unresolved?.operation_id ?? '')
      const proof = await verification.requestVerification({
        scope: 'hosting.manage',
        context: {
          action: next === 'delete' ? 'delete' : 'reset',
          account_id: props.account.id,
          operation_id: next === 'delete' ? '' : operationId,
        },
      })
      if (!proof) {
        throw new AuthOperationError('Verification cancelled', 'AUTH_CANCELLED')
      }
      if (next === 'delete') {
        return deleteHostingAccount(props.account.id, proof.proof_token)
      }
      // 继续确认只允许服务端已有未决操作，不用新 ID 掩盖上次未知结果。
      if (next === 'resume') {
        return resumeResetOperation(operationId, proof.proof_token)
      }
      return consumeResetCredit(
        props.account.id,
        operationId,
        proof.proof_token
      )
    },
    // 普通账号更新与重置操作返回不同 DTO，按动作分支解释。
    // 只有重置或继续确认才检查消费终态。
    // 未决结果保持原本地 ID，并立即刷新后台历史供恢复。
    // 任何成功动作都会失效相关查询，不手工构造新的账户状态。
    // 确认框在响应成功后退出，不抢先展示操作完成。
    // 解绑成功才能关闭详情，失败时仍允许用户排查或重试。
    onSuccess: (result, next) => {
      if (next === 'reset' || next === 'resume') {
        const operation = result as ResetOperation
        if (['succeeded', 'rejected'].includes(operation.status)) {
          resetId.current = null
        }
      }
      void client.invalidateQueries({ queryKey: ['hosting'] })
      setAction(null)
      if (next === 'delete') props.onClose()
    },
    // 主动取消安全验证不属于业务故障，避免多余错误提示。
    // 其他异常沿用统一错误处理，保留服务端可定位原因。
    // 消费响应丢失可能已经扣卡，因此错误处理只重新读取历史。
    // 这里不自动再调用消费接口，也不生成替代 UUID。
    // 历史重读失败仍保留错误状态，不能宣称未发生消费。
    onError: (error) => {
      if (!('code' in error && error.code === 'AUTH_CANCELLED')) {
        handleServerError(error)
      }
      // 消费响应丢失时重新读操作记录，不能提示“失败，未扣次数”。
      void operations.refetch()
    },
  })
  // 同步未知结果使用只读接口，不以刷新按钮发起新的外部消费。
  // 只读同步按原操作编号查询远端结果。
  // 没有未决记录时不构造一个空编号请求。
  // 成功读取后刷新历史和额度，用户能看到最终状态与窗口变化。
  // 同步结果仍未知时保留原操作，不开放新的消费入口。
  // 该按钮没有消费证明，因为 GET 不应该引发外部扣卡。
  // 真正继续执行的路径仍需要独立确认和单次证明。
  const syncReset = useMutation({
    mutationFn: async () => {
      if (!unresolved) return
      return getResetOperation(unresolved.operation_id)
    },
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ['hosting'] })
    },
  })
  const snapshot = observation.data?.snapshot
  // 新消费门槛同时要求主观测和卡子观测新鲜。
  // 可用次数必须明确大于零，缺省值只用于关闭操作入口。
  // 这里的缺省零不进入展示组件，用户仍看到次数未知。
  // 重置历史必须已成功取得，避免在未知历史上并发消费。
  // 存在任何未决操作都禁止新消费，与次数多少无关。
  // 后端仍会在消费时检查额度、所属和并发，UI 门槛不是授权。
  // 窗口有剩余额度不阻止用户主动重置，因为原厂规则由后端决定。
  // 前端不承诺重置会恢复所有窗口，操作结果另行展示。
  const canReset =
    observation.data?.state === 'fresh' &&
    snapshot?.reset_credit_observation?.state === 'fresh' &&
    (snapshot.reset_credits_available ?? 0) > 0 &&
    operations.isSuccess &&
    !unresolved
  // 忙碌门槛覆盖可能改变账号或消费判断的动作。
  // 同步未决结果期间不开放新的重置，避免用户同时操作。
  // 备注保存有独立禁用状态，因为它不会改变消费历史。
  // 测试模型选择在执行中保持固定，结果对应的模型才可解释。
  // 观察刷新不改变原操作 UUID，即使刷新拿到更多可用次数。
  // 如果正在消费的账号被另一会话暂停，后端仍会拒绝不合法请求。
  // 这里不通过 UI 顺序假设能规避服务端并发校验。
  // 安全验证打开期间保持当前 action，确认完成后回到对应流程。
  // 验证取消保留业务确认框，用户可以明确返回而非被强制关闭。
  // 新消费成功后清空本地 UUID，让未来消费拥有独立身份。
  // 成功后的窗口同步由后端处理，前端不自行扣减卡次数。
  // 操作拒绝也不直接恢复原次数，仍读取原厂真实观测。
  // 未知结果不能展示成功 Toast，也不能展示未扣卡保证。
  // 卡次显示与按钮门槛都依据新鲜度，不依据缓存更新时间猜测。
  // 如果观察子查询失败，主额度正常也不能开放消费。
  // 服务端可能返回多个原厂窗口，前端不选择一个代表全部。
  // 已测试模型失效时用户可再次主动测试，界面不自动发耗额请求。
  // 不能把接口响应成功当成原厂测试通过。
  // 重授权改变凭据后，是否需要重新测试由后端状态决定。
  // 模型目录查询错误不能回退到全站模型目录。
  // 每次操作的目标 ID 都来自当前账号摘要。
  // 按钮文案不包含原厂密钥或完整身份。
  // 备注的保存与账号删除不会共享请求体。
  // 解绑确认特别说明其他来源可能继续收费。
  // 暂停确认不承诺当前流式请求立即中断。
  // 恢复按钮只恢复本账号，不自动启用策略里禁用的来源。
  // 关闭详情不取消已经完成的原厂消费。
  // 未决记录保存在服务器，因此重新打开仍会阻止重复消费。
  // 只读结果刷新可用于确认失去响应的历史请求。
  // 敏感恢复始终使用原操作编号，不以新的编号规避冲突。
  // 历史的稳定编号用于支持排障，不应当作用户可修改字段。
  const busy = mutate.isPending || refresh.isPending || syncReset.isPending
  // 确认文案描述动作真实影响，不把平台行为说成原厂行为。
  // 暂停提醒后续来源仍可能付费，防止用户误认为停用整个 Key。
  // 解绑只删除平台持有凭据，不替用户注销原厂订阅。
  // 消费明确不可撤销，并说明窗口效果可能不同。
  // 继续未决请求承诺沿用同一操作，不能暗示重新申请一张卡。
  // 测试说明原厂耗额与平台免费分别是什么。
  // 所有确认文字通过翻译，语言切换后不保留旧语言常量。
  const confirmations: Record<Action, string> = {
    pause: t(
      'Pausing this account skips it for new requests. Other sources, including paid sources, may still be used.'
    ),
    delete: t(
      'Removing this account deletes its Oak credentials, not the provider authorization. Other sources may still charge your wallet.'
    ),
    reset: t(
      'Consume one native reset opportunity for this account. This cannot be undone and does not guarantee every window is reset.'
    ),
    resume: t(
      'Continue checking the same reset operation. A new reset opportunity will not be requested.'
    ),
    test: t(
      'Send one small model request using this account. It consumes provider quota and is free on Oak.'
    ),
  }
  return (
    <>
      <Dialog
        open={!verification.isActive && !action}
        onOpenChange={(open) => {
          if (!open && !busy) props.onClose()
        }}
        title={props.account.alias}
        description={props.account.email_mask}
      >
        <div className='space-y-5'>
          <Field>
            <FieldLabel htmlFor='hosting-rename'>
              {t('Account alias')}
            </FieldLabel>
            <Input
              id='hosting-rename'
              value={alias}
              maxLength={64}
              onChange={(event) => setAlias(event.target.value)}
            />
            <Button
              variant='outline'
              disabled={
                rename.isPending ||
                !alias.trim() ||
                alias.trim() === props.account.alias
              }
              onClick={() => rename.mutate()}
            >
              {t('Save alias')}
            </Button>
          </Field>
          {props.account.last_error && (
            <p role='alert' className='text-destructive text-sm'>
              {props.account.last_error}
            </p>
          )}
          {/*
           * 额度显示不做额外账户状态推断。
           * 查询未结束时展示加载，失败则提供重试。
           * 服务端暂时没有快照时保持未观测状态。
           * 主动刷新按钮独立于消费按钮，刷新不会用掉重置机会。
           * 账号错误信息显示在相邻位置，便于理解授权问题与额度问题。
           */}
          {observation.isPending && <LoadingState />}
          {observation.isError && (
            <ErrorState
              onRetry={() => {
                void observation.refetch()
              }}
            />
          )}
          {observation.data && <AccountQuota observation={observation.data} />}
          <Button
            variant='outline'
            disabled={busy}
            onClick={() => refresh.mutate()}
          >
            {t('Refresh quota')}
          </Button>
          {/*
           * 所有账号操作使用明确文字，避免危险操作只靠图标区分。
           * 新消费按钮严格遵守观测与历史的组合门槛。
           * 未决操作同时提供只读同步和继续确认，不合并为模糊的重试。
           * 暂停与恢复共用一个动作，根据当前账号摘要决定目标状态。
           * 重授权先离开详情，由父工作区打开独立登录弹窗。
           * 删除使用危险按钮和二次确认，不能由列表普通点击直接执行。
           */}
          <div className='flex flex-wrap gap-2'>
            <Button
              variant='outline'
              disabled={busy || !canReset}
              onClick={() => setAction('reset')}
            >
              {t('Use one reset opportunity')}
            </Button>
            {unresolved && (
              <Button
                variant='outline'
                disabled={busy}
                onClick={() => syncReset.mutate()}
              >
                {t('Refresh reset result')}
              </Button>
            )}
            {unresolved && (
              <Button
                variant='outline'
                disabled={busy}
                onClick={() => setAction('resume')}
              >
                {t('Continue pending reset')}
              </Button>
            )}
            <Button
              variant='outline'
              disabled={busy}
              onClick={() => setAction('pause')}
            >
              {props.account.paused ? t('Resume account') : t('Pause account')}
            </Button>
            <Button
              variant='outline'
              disabled={busy}
              onClick={props.onReauthorize}
            >
              {t('Reauthorize account')}
            </Button>
            <Button
              variant='destructive'
              disabled={busy}
              onClick={() => setAction('delete')}
            >
              {t('Remove account')}
            </Button>
          </div>
          {/* 测试和消费都由用户点击，加载详情永远不触发付费或原厂耗额。 */}
          <Field>
            <FieldLabel>{t('Available models')}</FieldLabel>
            {models.isError && (
              <ErrorState
                onRetry={() => {
                  void models.refetch()
                }}
              />
            )}
            <Combobox
              options={(models.data?.models ?? []).map((item) => ({
                label: item.id,
                value: item.id,
              }))}
              value={model}
              onValueChange={(value) => setModel(value ?? '')}
              placeholder={t('Select a model')}
              disabled={busy || models.isPending}
            />
            <Button
              disabled={
                busy ||
                !model ||
                props.account.paused ||
                !['connected', 'active'].includes(props.account.status)
              }
              onClick={() => setAction('test')}
            >
              {t('Test connection')}
            </Button>
          </Field>
          {test.data && (
            <p role='status'>
              {test.data.outcome === 'passed'
                ? t('Model call verified')
                : t('Model call not verified')}{' '}
              · {test.data.protocol} · {test.data.reason}
            </p>
          )}
          {/*
           * 历史按服务端返回的持久操作显示，不仅展示本次页面点击。
           * 操作 UUID 可用于定位故障，但不是密钥或上游管理编号。
           * 成功且观察未同步时明确说明等待，避免诱导用户重复重置。
           * 未知结果提示禁止新消费，与顶部按钮禁用保持一致。
           * 历史查询失败可独立重试，不触发原厂消费或自动补偿。
           * 服务端状态原样保留，未识别状态不会被渲染成成功。
           * 结果为空不能跳过记录，未决身份本身就是保护依据。
           * 操作数量不等于剩余重置机会，历史与当前观测分开展示。
           */}
          <section aria-label={t('Reset history')} className='space-y-2'>
            <h3 className='font-medium'>{t('Reset history')}</h3>
            {operations.isError && (
              <ErrorState
                onRetry={() => {
                  void operations.refetch()
                }}
              />
            )}
            {operations.data?.items.map((item) => (
              <div
                key={item.operation_id}
                className='rounded border p-2 text-sm'
              >
                <p>
                  {item.status === 'succeeded'
                    ? t('Reset confirmed')
                    : t('Reset status: {{status}}', { status: item.status })}
                </p>
                {item.result?.observation_pending && (
                  <p>{t('Reset succeeded. Quota is still synchronizing.')}</p>
                )}
                <p className='text-muted-foreground text-xs break-all'>
                  {item.operation_id}
                </p>
              </div>
            ))}
            {unresolved && (
              <p role='status'>
                {t(
                  'The previous reset result is not confirmed. Do not start another reset.'
                )}
              </p>
            )}
          </section>
        </div>
      </Dialog>
      {/* 二次确认让出焦点给站点验证弹窗，避免两个模态框争抢焦点。 */}
      <ConfirmDialog
        open={Boolean(action) && !verification.isActive}
        onOpenChange={(open) => {
          if (!open && !mutate.isPending) setAction(null)
        }}
        title={props.account.alias}
        desc={action ? confirmations[action] : ''}
        destructive={action === 'delete' || action === 'reset'}
        isLoading={mutate.isPending}
        handleConfirm={() => {
          if (action) mutate.mutate(action)
        }}
      />
      <SecureVerificationDialog {...verification.dialogProps} />
    </>
  )
}
