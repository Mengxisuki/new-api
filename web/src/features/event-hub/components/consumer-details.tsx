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
 * 消费者详情的业务约束：
 * 消费者是独立消费进度的拥有者，
 * 令牌列表只是这个消费者的多份凭据。
 * 两个令牌同时连接时共享待确认记录，
 * 因此这里始终提示独立接收用途应创建不同消费者。
 * 服务端返回的在线连接数表示活动租约，
 * 不是消费者已经处理完所有消息的证明。
 * 投递记录分为尚未尝试、已尝试写入和客户端明确确认，
 * 三个状态不能互相冒充。
 * 最近投递时间实际是尝试写入时间，
 * 不承诺网络另一端确定收到了数据。
 * 投递次数用于诊断重试，
 * 只有确认时间不为零才显示已确认。
 * 事件编号对应一份业务事实，
 * 投递编号对应消费者的一次可确认记录；
 * 补发传事件编号，
 * 客户端确认传投递编号。
 * 两个编号即使偶然相同，
 * 也不能在接口中互换。
 * 详情只展示服务端白名单摘要，
 * 完整会话、账户令牌和支付凭据不应出现在此处。
 * 摘要弹窗使用文本渲染，
 * 未知结构只作为可检查的内容，
 * 不解析成执行指令。
 * 令牌列表只展示名称、尾号、主题和生命周期信息，
 * 不存在查看历史明文的按钮。
 * 有效期已到而尚未手工撤销的令牌必须显示过期，
 * 不能继续标记为可用。
 * 手工撤销与自然过期是不同状态：
 * 撤销保留操作记录，
 * 过期由当前时间判断。
 * 撤销前要求明确确认，
 * 因为该凭据上的在线连接会结束。
 * 撤销成功不删除消费者，
 * 也不会清空已经完成的确认记录。
 * 其他仍有效的凭据可以继续消费，
 * 便于在不中断服务的情况下轮换。
 * 生成新令牌是独立动作，
 * 不通过编辑订阅顺便扩权。
 * 消费者所有者授权失效时，
 * 现有订阅应停止；
 * 页面不提供绕过验证的恢复开关。
 * 管理员应先核对账号，
 * 再创建明确的新消费者；
 * 旧确认历史仍保留审计意义。
 * 保留期过后被清理且未确认的数量为累计历史损失，
 * 不能称为当前队列待处理数量。
 * 这个警示要求客户端从业务接口核对真实订单，
 * 不能通过补发按钮找回已清理事件。
 * 补发确认说明只重新发送通知，
 * 不执行原订单付款、退款或扣款。
 * 失败时保留确认窗口，
 * 服务端拒绝不能被当作补发成功。
 * 投递详情只列最近记录，
 * 缺少某条历史不代表这条事件从未存在。
 * 接入示例使用消费者编号构建真实接口地址，
 * 认证始终放在请求头。
 * 示例引用环境变量保存秘密，
 * 绝不能把刚生成的实际令牌拼进页面持久文本或查询参数。
 * 订阅流采用长连接输出，
 * 命令关闭输出缓冲便于即时查看事件。
 * 确认示例使用独立普通请求，
 * 并明确传递投递编号；
 * 收到消息后应先完成本地幂等处理，
 * 再发确认。
 * 断线后使用相同消费者重新连接，
 * 服务端保存的确认记录才是恢复依据。
 * 网络上的最后收到编号不能代替业务确认，
 * 客户端不能以游标跳过没有处理的事件。
 * 允许重复投递，
 * 所以接入方必须通过业务事件编号去重。
 * 复制地址和示例复用统一按钮，
 * 不另行实现剪贴板权限或成功提示。
 * 多个确认弹窗分别保存目标记录，
 * 切换消费者会卸载旧详情，
 * 防止确认错误接收方。
 * 所有破坏性凭据操作都受真实请求状态约束，
 * 页面不会在请求完成前提前显示成功。
 */
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { StaticDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import {
  topicLabels,
  type EventDetail,
  type EventDelivery,
  type EventToken,
} from '../api'

/**
 * 详情聚合消费者的凭据列表和最近投递。
 * 在线状态只表示连接活动，确认状态由 ACK 时间单独显示。
 * 撤销与补发必须确认，补发不会修改业务订单。
 * 历史 Token 永不回显明文；创建后的凭据在另一弹窗展示。
 * 多个 Token 共享同一消费者进度，独立用途应新建消费者。
 */
export function ConsumerDetails(props: {
  detail: EventDetail
  pending: boolean
  onCreateToken: () => void
  onRevoke: (id: number) => Promise<boolean>
  onReplay: (id: number) => Promise<boolean>
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  // 时间显示以服务器秒级协议为入口。
  // 缺少活动时间用明确的从未发生状态表示。
  // 界面语言先转换为合法区域码，再交给日期格式化器。
  // 撤销目标保存完整摘要，以便确认窗口稳定显示当前对象。
  // 补发目标与撤销目标分别保存，避免不同编号混淆。
  // 查看摘要不改变投递状态，也不会读取额外敏感数据。
  const time = (seconds: number) =>
    seconds
      ? new Intl.DateTimeFormat(locale, {
          dateStyle: 'medium',
          timeStyle: 'short',
        }).format(seconds * 1000)
      : t('Never')
  const [revoke, setRevoke] = useState<EventToken | null>(null)
  const [replay, setReplay] = useState<EventDelivery | null>(null)
  const [payload, setPayload] = useState<EventDelivery | null>(null)
  const streamPath = `/api/event-hub/stream?consumer_id=${props.detail.consumer.id}`
  const stream = new URL(streamPath, window.location.origin).href
  // 示例只引用环境变量，不把刚生成的真实凭据嵌进命令或查询参数。
  const example = [
    `curl -N '${stream}' -H "Authorization: Bearer $EVENT_TOKEN"`,
    '',
    `curl '${window.location.origin}/api/event-hub/ack' -H "Authorization: Bearer $EVENT_TOKEN" -H 'Content-Type: application/json' --data '{"delivery_id":123}'`,
  ].join('\n')

  // 凭据列表来自服务端当前消费者详情。
  // 列表刷新不重建明文，也不请求读取历史秘密。
  // 投递列表使用独立投递编号作为键。
  // 同一业务事件可以有多份消费者投递记录。
  // 下面的操作回调已绑定当前消费者，组件只传资源编号。
  // 回调返回成功后才关闭确认窗口。
  // 回调返回失败时保持目标，方便查看和再次明确操作。
  // 页面不会通过直接调用 ACK 来消除积压。
  // 保留期损失无法由重新连接恢复，提示需要业务对账。
  // 授权失效警示和保留期损失可以同时出现。
  // 两类警示分别解释原因，不合并成模糊的连接错误。
  // 创建按钮直接使用授权状态，无法靠弹窗入口绕过提示。
  const tokens = props.detail.tokens
  const deliveries = props.detail.deliveries
  // 详情中只有凭据撤销会使用危险操作样式。
  // 补发是通知动作，但仍需确认，避免误点触发重复消费。
  // 令牌尾号用于管理员定位机器，不代表秘密可以恢复。
  // 最后使用时间有助于轮换时确认旧凭据是否仍活跃。
  // 撤销状态比自然到期优先，保留管理员明确执行的结果。
  // 自然到期按当前时间判断，轮询后无需重新登录即可更新。
  // 允许撤销过期凭据，便于保留清楚的生命周期记录。
  // 已经撤销的行不允许再次撤销，避免无意义重复请求。
  // 投递尝试次数独立显示，不能据此推断财务执行次数。
  // 已确认样式只由确认时间决定，不能依据最后发送时间。
  // 详情展开使用文本序列化，不插入服务端提供的页面代码。
  // 长摘要在有限区域滚动，不能挤出整个设置页面。
  // 连接示例保留原始命令换行，便于客户端直接复制核对。
  // 连接地址使用当前站点来源，避免写死测试域名。
  // 复制流地址不包含凭据，地址可单独交给接入人员。
  // 接入人员需要自行设置环境变量，页面不替他们保存秘密。
  // 订阅命令关闭输出缓冲，调试时可立即看到事件帧。
  // 确认请求需要 JSON 类型，传入对应消息的投递编号。
  // 示例中的编号是结构说明，不能代表当前真实待确认消息。
  // 连接断开不等于消费成功，客户端重连仍以持久确认恢复。
  // 业务事件去重应持久保存，不能只依赖连接内存。
  // 确认前完成本地处理，避免崩溃后丢失业务动作。
  // 本模块不承诺消息只出现一次，接入方必须处理重复。
  // 撤销窗口与补发窗口使用共享确认组件，行为保持全站一致。
  // 窗口关闭只清理选择状态，不请求撤销或补发接口。
  // 确认中显示忙碌状态，避免连续点击同一资源。
  // 创建凭据按钮不要求消费者当前启用，支持先配置后上线。
  // 授权已经失效时则禁止新建，保留服务端权限边界。
  // 投递摘要和接入示例分别展开，避免过长内容干扰日常管理。
  // 消费状态描述的是通知，不描述订单是否支付成功。
  // 历史表没有删除按钮，避免误以为可通过清空记录完成消费。
  // 任何点击都不持有或改变消费者的服务器确认游标。
  return (
    <div className='space-y-5 rounded-lg border p-4 sm:p-5'>
      {!props.detail.consumer.auth_valid && (
        <p
          role='alert'
          className='bg-destructive/5 text-destructive rounded-md p-3 text-sm'
        >
          {t(
            'Consumer authorization is no longer valid. Create a new consumer after reviewing the owner account.'
          )}
        </p>
      )}
      {props.detail.consumer.expired_unacked_count > 0 && (
        <p
          role='alert'
          className='bg-destructive/5 text-destructive rounded-md p-3 text-sm'
        >
          {t(
            'Cumulative expired unacknowledged notifications: {{count}}. Reconcile from the business APIs.',
            { count: props.detail.consumer.expired_unacked_count }
          )}
        </p>
      )}
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div>
          <h4 className='font-medium'>{props.detail.consumer.name}</h4>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Use separate consumers when every client needs its own copy.')}
          </p>
        </div>
        <Button
          onClick={props.onCreateToken}
          disabled={props.pending || !props.detail.consumer.auth_valid}
        >
          {t('Create event token')}
        </Button>
      </div>
      <div className='bg-muted/50 flex items-center gap-2 rounded-md p-3 text-sm'>
        <code className='min-w-0 flex-1 break-all'>{streamPath}</code>
        <CopyButton value={stream} aria-label={t('Copy stream URL')} />
      </div>
      <StaticDataTable<EventToken>
        data={tokens}
        getRowKey={(row) => row.id}
        emptyContent={<EmptyState title={t('No event tokens')} />}
        columns={[
          {
            id: 'name',
            header: t('Token name'),
            cell: (row) => (
              <div className='space-y-1'>
                <span>{row.name}</span>
                <p className='text-muted-foreground text-xs'>
                  …{row.token_hint}
                </p>
              </div>
            ),
          },
          {
            id: 'topics',
            header: t('Allowed event topics'),
            cell: (row) => (
              <div className='space-y-1'>
                {row.topics.map((topic) => (
                  <p key={topic} className='text-xs'>
                    {t(topicLabels[topic as keyof typeof topicLabels] ?? topic)}
                  </p>
                ))}
              </div>
            ),
          },
          {
            id: 'expiry',
            header: t('Expiration'),
            cell: (row) =>
              row.expires_at ? time(row.expires_at) : t('Never expires'),
          },
          {
            id: 'used',
            header: t('Last used'),
            cell: (row) => time(row.last_used_at),
          },
          {
            id: 'state',
            header: t('Status'),
            cell: (row) => {
              if (row.revoked_at) {
                return <Badge variant='outline'>{t('Revoked')}</Badge>
              }
              if (row.expires_at && row.expires_at <= Date.now() / 1000) {
                return <Badge variant='outline'>{t('Expired')}</Badge>
              }
              return <Badge variant='outline'>{t('Active')}</Badge>
            },
          },
          {
            id: 'actions',
            header: t('Actions'),
            cell: (row) => (
              <Button
                size='sm'
                variant='outline'
                disabled={!!row.revoked_at || props.pending}
                onClick={() => setRevoke(row)}
              >
                {t('Revoke')}
              </Button>
            ),
          },
        ]}
      />
      <div>
        <h5 className='mb-3 text-sm font-medium'>{t('Recent deliveries')}</h5>
        <StaticDataTable<EventDelivery>
          data={deliveries}
          getRowKey={(row) => row.id}
          emptyContent={<EmptyState title={t('No deliveries yet')} />}
          columns={[
            {
              id: 'event',
              header: t('Event'),
              cell: (row) => (
                <div>
                  <span className='font-mono text-xs'>#{row.event_id}</span>
                  <p className='mt-1 text-xs'>
                    {t(
                      topicLabels[row.topic as keyof typeof topicLabels] ??
                        row.topic
                    )}
                  </p>
                </div>
              ),
            },
            {
              id: 'attempts',
              header: t('Delivery attempts'),
              cell: (row) => formatNumber(row.attempts, locale),
            },
            {
              id: 'state',
              header: t('Status'),
              cell: (row) => (
                <Badge variant={row.acked_at ? 'secondary' : 'outline'}>
                  {t(row.acked_at ? 'Acknowledged' : 'Awaiting acknowledgment')}
                </Badge>
              ),
            },
            {
              id: 'time',
              header: t('Last delivery attempt'),
              cell: (row) => time(row.last_sent_at),
            },
            {
              id: 'actions',
              header: t('Actions'),
              cell: (row) => (
                <div className='flex gap-2'>
                  <Button
                    size='sm'
                    variant='ghost'
                    onClick={() => setPayload(row)}
                  >
                    {t('Details')}
                  </Button>
                  <Button
                    size='sm'
                    variant='outline'
                    disabled={props.pending}
                    onClick={() => setReplay(row)}
                  >
                    {t('Replay event')}
                  </Button>
                </div>
              ),
            },
          ]}
        />
      </div>
      <details className='rounded-md border p-3'>
        <summary className='cursor-pointer text-sm font-medium'>
          {t('Client connection example')}
        </summary>
        <p className='text-muted-foreground my-3 text-sm'>
          {t(
            'Reconnect with the same consumer. Confirm only after processing; deduplicate by event_id.'
          )}
        </p>
        <div className='flex justify-end'>
          <CopyButton
            value={example}
            aria-label={t('Copy connection example')}
          />
        </div>
        <pre className='overflow-x-auto text-xs leading-6'>{example}</pre>
      </details>
      <ConfirmDialog
        open={!!revoke}
        onOpenChange={(open) => {
          if (!open) setRevoke(null)
        }}
        title={t('Revoke event token')}
        desc={t(
          'Revoking closes connections using this token. Consumer progress is preserved.'
        )}
        destructive
        isLoading={props.pending}
        handleConfirm={() => {
          if (revoke) {
            void props.onRevoke(revoke.id).then((ok) => {
              if (ok) setRevoke(null)
            })
          }
        }}
      />
      <ConfirmDialog
        open={!!replay}
        onOpenChange={(open) => {
          if (!open) setReplay(null)
        }}
        title={t('Replay event')}
        desc={t(
          'Only this notification will be delivered again. No payment or charge will be repeated.'
        )}
        isLoading={props.pending}
        handleConfirm={() => {
          if (replay) {
            void props.onReplay(replay.event_id).then((ok) => {
              if (ok) setReplay(null)
            })
          }
        }}
      />
      <Dialog
        open={!!payload}
        onOpenChange={(open) => {
          if (!open) setPayload(null)
        }}
        title={t('Event details')}
      >
        <pre className='bg-muted max-h-96 overflow-auto rounded-md p-3 text-xs break-all whitespace-pre-wrap'>
          {JSON.stringify(payload?.payload, null, 2)}
        </pre>
      </Dialog>
    </div>
  )
}
