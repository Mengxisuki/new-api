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
 * 页面职责与访问边界：
 * 事件推送作为系统运维中的独立栏目，
 * 和邮件通知处于同一级别。
 * 邮件仍负责已有的邮件渠道，
 * 本页面管理持久事件和外部接收方，
 * 不把两套配置混到一起。
 * 访问权限由现有系统设置路由及后端真实登录校验共同决定，
 * 导航是否可见不是权限依据。
 * 页面自身不接受外部接入令牌进行管理，
 * 接入凭据只作用于订阅与确认接口。
 * 浏览器在管理页停留不会自动创建一个消费者，
 * 也不会默认为任何外部客户端确认事件。
 * 概况中的消费者数量来自服务端完整摘要，
 * 在线数按连接汇总，
 * 积压数按未确认投递汇总。
 * 这几类统计分别回答谁接收、谁连接和谁尚未确认，
 * 不能彼此替代。
 * 暂停状态要清楚显示为投递暂停，
 * 不能让管理员以为业务充值或资料接收也被关闭。
 * 服务配置和实时状态分开保存，
 * 状态拉取不能覆盖正在编辑的规则草稿。
 * 设置部分的费用是内部整数额度，
 * 展示货币只是辅助理解，
 * 不隐式改变账务规则。
 * 异常规则既有历史倍数也有绝对下限，
 * 样本不足采用独立阈值，
 * 不把新用户当作异常。
 * 账务观察的未决数量需要醒目展示，
 * 因为任务完成不一定意味着最终资金观察已得到确定结果。
 * 缓存确认未知也需要人工对账，
 * 不能给管理员一个可能再次扣款的盲目重试按钮。
 * 这两类警示只说明需要检查，
 * 页面不得自动修复钱包、重复结算或补造日志。
 * 消费者列表按稳定编号区分独立接收方，
 * 名称允许相似但不会合并进度。
 * 主题列表表达订阅过滤，
 * 用户列表表达关注范围，
 * 空筛选仍受服务端授权约束。
 * 列表中的启用状态只是投递开关，
 * 不能替代所有者授权是否有效的判断。
 * 所有者权限失效时显示醒目状态，
 * 不能靠暂停后再启用恢复旧权限。
 * 编辑名称与订阅会保留消费者身份和确认记录，
 * 不借机删除历史事件。
 * 暂停消费者不会撤销其令牌，
 * 恢复后可以继续从原确认位置接收，
 * 令牌撤销是另一种明确操作。
 * 新建消费者使用共享表单，
 * 输入失败保留草稿，
 * 成功后才关闭。
 * 列表管理按钮显式选择一个消费者，
 * 再加载它的凭据和投递，
 * 不能只凭名称猜测目标。
 * 切换管理目标时关闭旧的令牌创建流程，
 * 清空一次性明文，
 * 旧详情组件也按编号重新挂载。
 * 这样旧目标的撤销或补发确认不可能跟随到新消费者。
 * 详情查询的加载和失败状态分别呈现，
 * 不能在读取失败后继续显示另一消费者的数据。
 * 后台五秒刷新用于连接、积压和令牌状态观测，
 * 真实消费依然由客户端完成。
 * 事件历史保留独立展开区域，
 * 便于核对平台事实和消费者投递之间的差异。
 * 展开历史不会触发敏感资料读取，
 * 也不会重新执行业务动作。
 * 令牌创建从当前消费者进入，
 * 主题权限只在已有订阅范围内选择。
 * 安全证明绑定确切消费者、主题和到期时间，
 * 签发过程中切换身份会取消整个流程。
 * 凭据明文只留在短暂组件内存，
 * 不能被放进查询缓存、地址、存储或日志。
 * 单次展示弹窗关闭后立即清除明文，
 * 没有回看按钮；
 * 以后需要新凭据时再次生成。
 * 弹窗复用标准复制按钮，
 * 其成功反馈不等于秘密已被安全保存，
 * 用户仍须自行保管。
 * 真实明文不能嵌入接入示例，
 * 示例只引用环境变量。
 * 系统安全验证弹窗沿用既有交互，
 * 避免为事件服务重新发明密码或验证码校验。
 * 创建表单在二次验证时隐藏，
 * 减少多个弹层同时可操作的焦点风险。
 * 响应式布局只改变展示密度，
 * 不隐藏关键权限状态或资金未决警示。
 * 表格沿用共享横向滚动和空态结构，
 * 小屏可以查看所有订阅及动作。
 * 统计数字、用户编号和费用展示遵循全站语言与格式化规则，
 * 切换语言无需重新创建消费者。
 * 所有文案经过翻译工具，
 * 后台返回的主题键保持协议标识，
 * 不随界面语言改变。
 * 页面只发管理请求，
 * 生产业务发生时的事件创建、幂等和事务一致性全部由后端保证。
 */
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { StaticDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { SecureVerificationDialog } from '@/features/auth/secure-verification'
import { SettingsSection } from '@/features/system-settings/components/settings-section'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { topicLabels, type EventConsumer } from './api'
import { ConsumerDetails } from './components/consumer-details'
import { ConsumerEditor } from './components/consumer-editor'
import { EventHistory } from './components/event-history'
import { ServiceSettings } from './components/service-settings'
import { TokenEditor } from './components/token-editor'
import { useEventHub } from './hooks/use-event-hub'

/**
 * 运维设置中的独立服务工作区，沿用邮件设置的内容布局。
 * 页面只负责管理，不能把查看状态误当成消费事件。
 * 每个消费者有独立进度，同一消费者的多个 Token 用于轮换。
 * 新建与编辑在共用弹窗中完成，已有列表行为不另造实现。
 * 明文只展示一次，关闭弹窗立即从组件内存清除。
 * 服务暂停时仍能维护订阅和凭据，并查看持久事件。
 */
export function EventHubSection() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  // 选中的消费者编号是详情查询的唯一目标。
  // 编辑目标和详情目标独立，编辑弹窗不会改变订阅连接。
  // 令牌弹窗只记录是否打开，凭据本身由受控 Hook 管理。
  // 列表刷新时按编号重新查找对象，避免沿用过期订阅快照。
  const [selected, setSelected] = useState<number | null>(null)
  const [editor, setEditor] = useState<EventConsumer | 'new' | null>(null)
  const [tokenOpen, setTokenOpen] = useState(false)
  const hub = useEventHub(selected)
  const overview = hub.overview
  if (overview.isPending) return <LoadingState />
  if (overview.isError) {
    return <ErrorState onRetry={() => void overview.refetch()} />
  }
  // 概况必须先成功加载，下面的统计才能解释为真实零值。
  // 消费者不存在时不渲染令牌编辑器，避免给删除目标签发。
  // 连接数允许一个消费者有多个连接，所以不能用在线行数代替。
  // 待确认量可以同时属于多个消费者，不能当成去重后的订单数。
  const consumers = overview.data.consumers
  const consumer = consumers.find((item) => item.id === selected)
  const online = consumers.reduce(
    (count, item) => count + item.online_connections,
    0
  )
  const backlog = consumers.reduce((count, item) => count + item.backlog, 0)
  // 工作区按照状态、配置、订阅、投递、接入凭据排列。
  // 账务警示位于管理动作前，让操作人先核对未决结果。
  // 服务设置只接收服务端快照和保存回调，不承担全局缓存职责。
  // 凭据展示与验证弹窗放在页面层，关闭子表单不会丢失结果。
  // 公共弹窗负责焦点管理，页面只传明确的业务内容。
  // 订阅表使用行编号作为键，名称变化不会重建错误的行。
  // 各列表的空态说明可执行的下一步，不能用空白代替。
  // 加载和错误单独呈现，重试沿用当前查询键与身份。
  // 详情组件按消费者编号挂载，清理前一目标的确认状态。
  // 同级详情和两个编辑器必须使用不同类型前缀，避免同一编号造成重复键。
  // 重复键会让刷新保留旧详情快照，即使接口只返回一个消费者。
  // 历史组件维护自身页码，切换详情不影响事件核对位置。
  // 页面数字格式在渲染时计算，语言变化不会保留旧格式。
  // 主题显示使用翻译名称，协议中的主题键始终原样提交。
  // 管理动作通过真实忙碌状态禁用，避免同一订阅并发保存。
  // 编辑器由完成回调关闭，后端拒绝时管理员可继续修改。
  // 外部客户端接入示例位于详情中，编号来自当前服务端数据。
  // 明文弹窗只读取受会话保护的返回值，不保存第二份副本。
  // 关闭明文弹窗的任何入口都会调用同一个清理动作。
  // 验证器保持单实例，不能因开关表单多次请求安全证明。
  // 子表单只在验证交互阶段隐藏，避免同时出现可操作弹层。
  // 创建消费者与创建凭据是两个明确步骤，方便审核授权范围。
  // 任何统计卡片都不直接触发资金、补发或确认操作。
  // 当前列表没有浏览器消费者，不会占用客户端消费进度。
  // 管理员页面的轮询凭现有登录会话授权，不索取外部令牌。
  // 响应式网格的列数只改变布局，完整统计仍保留文本名称。
  // 状态列先检查授权，再检查启用开关，失效状态优先可见。
  // 授权失效后可查看历史资料，新的消费授权须建立新消费者。
  // 查询为空与查询失败使用不同组件，不能静默互相替代。
  // 所有写入通过 Hook 汇总失效范围，保持概况与详情一致。
  // 服务暂停时仍允许管理授权，因为配置动作不是网络投递。
  // 重建消费者不会在本页面自动复制旧凭据或确认记录。
  return (
    <SettingsSection title={t('Event delivery')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Receive platform events over SSE. Each consumer has independent subscriptions and acknowledgments.'
        )}
      </p>
      <div className='grid grid-cols-2 gap-3 lg:grid-cols-4'>
        {[
          {
            label: 'Service status',
            value: t(
              overview.data.settings.enabled ? 'Delivering' : 'Delivery paused'
            ),
          },
          { label: 'Consumers', value: formatNumber(consumers.length, locale) },
          { label: 'Online connections', value: formatNumber(online, locale) },
          {
            label: 'Awaiting acknowledgment',
            value: formatNumber(backlog, locale),
          },
        ].map((stat) => (
          <div key={stat.label} className='min-w-0 rounded-lg border px-4 py-3'>
            <p className='text-muted-foreground text-xs'>{t(stat.label)}</p>
            <p className='mt-2 text-lg font-semibold break-words tabular-nums'>
              {stat.value}
            </p>
          </div>
        ))}
      </div>
      <ServiceSettings
        settings={overview.data.settings}
        pending={hub.pending}
        onSave={hub.saveSettings}
      />
      {!!(
        (overview.data.unresolved_charge_count ?? 0) +
        (overview.data.sync_pending_count ?? 0)
      ) && (
        <p
          role='alert'
          className='border-destructive/30 bg-destructive/5 text-destructive rounded-md border p-3 text-sm'
        >
          {t(
            'Accounting needs review: {{unresolved}} unresolved task charges, {{pending}} uncertain cache confirmations. Reconcile before taking further action.',
            {
              unresolved: formatNumber(
                overview.data.unresolved_charge_count ?? 0,
                locale
              ),
              pending: formatNumber(
                overview.data.sync_pending_count ?? 0,
                locale
              ),
            }
          )}
        </p>
      )}
      <div className='space-y-3'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <h4 className='font-medium'>{t('Consumers')}</h4>
          <Button onClick={() => setEditor('new')} disabled={hub.pending}>
            {t('Create consumer')}
          </Button>
        </div>
        <StaticDataTable<EventConsumer>
          data={consumers}
          getRowKey={(row) => row.id}
          emptyContent={
            <EmptyState
              title={t('No consumers yet')}
              description={t(
                'Create one consumer for each independent receiving application.'
              )}
            />
          }
          columns={[
            {
              id: 'name',
              header: t('Consumer name'),
              cell: (row) => (
                <div>
                  <span className='font-medium'>{row.name}</span>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    #{row.id}
                  </p>
                </div>
              ),
            },
            {
              id: 'topics',
              header: t('Subscribed topics'),
              cell: (row) => (
                <div className='space-y-1'>
                  {row.topics.map((topic) => (
                    <p key={topic} className='text-xs'>
                      {t(
                        topicLabels[topic as keyof typeof topicLabels] ?? topic
                      )}
                    </p>
                  ))}
                </div>
              ),
            },
            {
              id: 'users',
              header: t('Follow user IDs'),
              cell: (row) =>
                row.user_ids.length
                  ? row.user_ids
                      .map((id) => formatNumber(id, locale))
                      .join(', ')
                  : t('All authorized users'),
            },
            {
              id: 'state',
              header: t('Status'),
              cell: (row) => {
                if (!row.auth_valid) {
                  return (
                    <Badge variant='destructive'>
                      {t('Authorization expired')}
                    </Badge>
                  )
                }
                return (
                  <Badge variant='outline'>
                    {t(row.enabled ? 'Enabled' : 'Paused')}
                  </Badge>
                )
              },
            },
            {
              id: 'online',
              header: t('Online / Pending'),
              cell: (row) =>
                `${formatNumber(row.online_connections, locale)} / ${formatNumber(row.backlog, locale)}`,
            },
            {
              id: 'actions',
              header: t('Actions'),
              cell: (row) => (
                <div className='flex gap-2'>
                  <Button
                    size='sm'
                    variant={selected === row.id ? 'secondary' : 'ghost'}
                    onClick={() => {
                      setSelected(row.id)
                      setTokenOpen(false)
                      hub.clearCreatedToken()
                    }}
                  >
                    {t('Manage')}
                  </Button>
                  <Button
                    size='sm'
                    variant='ghost'
                    onClick={() => setEditor(row)}
                  >
                    {t('Edit')}
                  </Button>
                  <Button
                    size='sm'
                    variant='outline'
                    disabled={hub.pending}
                    onClick={() =>
                      void hub.saveConsumer(
                        {
                          name: row.name,
                          topics: row.topics,
                          user_ids: row.user_ids,
                          enabled: !row.enabled,
                        },
                        row.id
                      )
                    }
                  >
                    {t(row.enabled ? 'Pause' : 'Enable')}
                  </Button>
                </div>
              ),
            },
          ]}
        />
      </div>
      {selected !== null && hub.detail.isPending && <LoadingState size='sm' />}
      {selected !== null && hub.detail.isError && (
        <ErrorState onRetry={() => void hub.detail.refetch()} />
      )}
      {selected !== null && hub.detail.data && (
        <ConsumerDetails
          key={`consumer-details-${selected}`}
          detail={hub.detail.data}
          pending={hub.pending}
          onCreateToken={() => setTokenOpen(true)}
          onRevoke={hub.revokeToken}
          onReplay={hub.replayEvent}
        />
      )}
      <details className='rounded-lg border p-4'>
        <summary className='cursor-pointer font-medium'>
          {t('Recorded events')}
        </summary>
        <div className='mt-4'>
          <EventHistory />
        </div>
      </details>
      {editor && (
        <ConsumerEditor
          key={
            editor === 'new'
              ? 'consumer-editor-new'
              : `consumer-editor-${editor.id}`
          }
          consumer={editor === 'new' ? undefined : editor}
          pending={hub.pending}
          onClose={() => setEditor(null)}
          onSave={hub.saveConsumer}
        />
      )}
      {tokenOpen && consumer && (
        <TokenEditor
          key={`token-editor-${consumer.id}`}
          consumer={consumer}
          pending={hub.pending}
          hidden={hub.verifying}
          onClose={() => setTokenOpen(false)}
          onCreate={hub.createToken}
        />
      )}
      <Dialog
        open={!!hub.createdToken}
        onOpenChange={(open) => {
          if (!open) hub.clearCreatedToken()
        }}
        title={t('Save your event token')}
        description={t(
          'This token is shown only once. Store it securely before closing this dialog.'
        )}
        footer={<Button onClick={hub.clearCreatedToken}>{t('Done')}</Button>}
      >
        <div className='bg-muted flex items-start gap-2 rounded-md p-3'>
          <code className='min-w-0 flex-1 text-sm break-all'>
            {hub.createdToken}
          </code>
          <CopyButton
            value={hub.createdToken}
            aria-label={t('Copy event token')}
          />
        </div>
      </Dialog>
      <SecureVerificationDialog {...hub.verificationDialogProps} />
    </SettingsSection>
  )
}
