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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import { getEventHistory, topicLabels } from '../api'

/**
 * 平台事件历史与消费者投递记录是两种不同视图。
 * 事件入库后就可查询，尚未投递也应可见。
 * 分页查询保留真实空态和失败态，不能用空数组掩盖错误。
 * 自动刷新只更新服务端数据，不推进任何消费进度。
 */
export function EventHistory() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const userId = useAuthStore((state) => state.auth.user?.id)
  const sessionId = useAuthStore((state) => state.auth.session?.sid)
  const [page, setPage] = useState(1)
  const history = useQuery({
    queryKey: ['event-hub', userId, sessionId, 'events', page],
    queryFn: () => getEventHistory(page),
    refetchInterval: 5000,
  })
  if (history.isPending) return <LoadingState size='sm' />
  if (history.isError) {
    return <ErrorState onRetry={() => void history.refetch()} />
  }
  const total = history.data.total
  const time = new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
  return (
    <div className='space-y-3'>
      <StaticDataTable
        data={history.data.items}
        getRowKey={(row) => row.id}
        emptyContent={<EmptyState title={t('No recorded events')} />}
        columns={[
          {
            id: 'id',
            header: t('Event'),
            cell: (row) => <span className='font-mono text-xs'>#{row.id}</span>,
          },
          {
            id: 'topic',
            header: t('Event topic'),
            cell: (row) =>
              t(
                topicLabels[row.topic as keyof typeof topicLabels] ?? row.topic
              ),
          },
          {
            id: 'time',
            header: t('Created at'),
            cell: (row) => time.format(row.created_at * 1000),
          },
          {
            id: 'data',
            header: t('Event summary'),
            cell: (row) => (
              <details>
                <summary className='cursor-pointer text-sm'>
                  {t('Details')}
                </summary>
                <pre className='mt-2 max-h-48 max-w-96 overflow-auto text-xs break-all whitespace-pre-wrap'>
                  {JSON.stringify(row.payload, null, 2)}
                </pre>
              </details>
            ),
          },
        ]}
      />
      <div className='flex flex-wrap items-center justify-between gap-3 text-sm'>
        <span className='text-muted-foreground'>
          {t('Recorded events')}: {formatNumber(total, locale)}
        </span>
        <div className='flex items-center gap-3'>
          <Button
            variant='outline'
            size='sm'
            onClick={() => setPage(page - 1)}
            disabled={page <= 1}
          >
            {t('Previous')}
          </Button>
          <span>{formatNumber(page, locale)}</span>
          <Button
            variant='outline'
            size='sm'
            onClick={() => setPage(page + 1)}
            disabled={history.data.items.length === 0 || page * 50 >= total}
          >
            {t('Next')}
          </Button>
        </div>
      </div>
    </div>
  )
}
