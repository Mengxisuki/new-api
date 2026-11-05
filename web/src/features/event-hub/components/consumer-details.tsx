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

  const tokens = props.detail.tokens
  const deliveries = props.detail.deliveries
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
