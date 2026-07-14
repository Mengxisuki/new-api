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
import { Zap, ExternalLink, Gauge } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  getLatencyColorClass,
  openExternalSpeedTest,
} from '@/features/dashboard/lib/api-info'
import type { ApiInfoItem, PingStatus } from '@/features/dashboard/types'
import { getBgColorClass } from '@/lib/colors'
import { cn } from '@/lib/utils'

interface ApiInfoItemProps {
  item: ApiInfoItem
  status: PingStatus
  onTest: (url: string) => void
}

export function ApiInfoItemComponent(props: ApiInfoItemProps) {
  const { t } = useTranslation()
  const item = props.item
  const status = props.status
  const routeLabel =
    item.source === 'server_address' ? t('Base URL') : item.route
  const description =
    item.source === 'server_address' ? t('Server Address') : item.description

  return (
    <div className='group hover:bg-muted/40 flex flex-col gap-2 px-2.5 py-2 transition-colors sm:px-3'>
      <div className='flex min-w-0 flex-col gap-1'>
        <div className='flex min-w-0 items-baseline gap-2'>
          <span
            className={cn(
              'inline-block size-2 shrink-0 rounded-full',
              getBgColorClass(item.color)
            )}
            aria-hidden='true'
          />
          <span className='shrink-0 font-mono text-sm font-semibold'>
            {routeLabel}
          </span>
          <span className='text-muted-foreground/60 truncate text-xs'>
            {description}
          </span>
        </div>
        <div className='flex min-w-0 items-center gap-1.5 pl-4'>
          <span className='text-muted-foreground min-w-0 truncate font-mono text-xs'>
            {item.url}
          </span>
          {status.testing && (
            <Badge variant='secondary' className='animate-pulse font-mono'>
              {t('Testing...')}
            </Badge>
          )}
          {status.latency !== null && !status.testing && (
            <Badge
              variant='secondary'
              className={cn(
                'font-mono font-medium',
                getLatencyColorClass(status.latency)
              )}
            >
              {status.latency}
              {t('ms')}
            </Badge>
          )}
          {status.error && (
            <Badge variant='outline' className='text-muted-foreground'>
              {t('N/A')}
            </Badge>
          )}
        </div>
      </div>

      <div className='border-border/50 flex flex-wrap items-center justify-end gap-1 border-t pt-1.5'>
        <Button
          variant='outline'
          size='xs'
          onClick={() => props.onTest(item.url)}
          disabled={status.testing}
          title={t('Test Latency')}
          aria-label={t('Test Latency')}
        >
          <Zap
            data-icon='inline-start'
            className={cn(status.testing && 'animate-pulse')}
          />
          {t('Test')}
        </Button>

        <Button
          variant='ghost'
          size='icon-xs'
          onClick={() => openExternalSpeedTest(item.url)}
          className='hidden sm:inline-flex'
          title={t('External Speed Test')}
          aria-label={t('External Speed Test')}
        >
          <Gauge />
        </Button>

        <CopyButton
          value={item.url}
          variant='ghost'
          size='sm'
          className='size-6 p-0'
          tooltip={t('Copy URL')}
          aria-label={t('Copy URL')}
        />

        <Button
          variant='ghost'
          size='icon-xs'
          className='hidden sm:inline-flex'
          title={t('Open in New Tab')}
          aria-label={t('Open in New Tab')}
          render={<a href={item.url} target='_blank' rel='noreferrer' />}
        >
          <ExternalLink />
        </Button>
      </div>
    </div>
  )
}
