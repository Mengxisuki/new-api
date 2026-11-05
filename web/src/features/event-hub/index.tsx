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
  const [selected, setSelected] = useState<number | null>(null)
  const [editor, setEditor] = useState<EventConsumer | 'new' | null>(null)
  const [tokenOpen, setTokenOpen] = useState(false)
  const hub = useEventHub(selected)
  const overview = hub.overview
  if (overview.isPending) return <LoadingState />
  if (overview.isError) {
    return <ErrorState onRetry={() => void overview.refetch()} />
  }
  const consumers = overview.data.consumers
  const consumer = consumers.find((item) => item.id === selected)
  const online = consumers.reduce(
    (count, item) => count + item.online_connections,
    0
  )
  const backlog = consumers.reduce((count, item) => count + item.backlog, 0)
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
          key={selected}
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
          key={editor === 'new' ? 'new' : editor.id}
          consumer={editor === 'new' ? undefined : editor}
          pending={hub.pending}
          onClose={() => setEditor(null)}
          onSave={hub.saveConsumer}
        />
      )}
      {tokenOpen && consumer && (
        <TokenEditor
          key={consumer.id}
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
