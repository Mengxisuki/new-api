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
import {
  Activity01Icon,
  Analytics01Icon,
  Cancel01Icon,
  CheckmarkCircle01Icon,
  CloudDownloadIcon,
  CloudUploadIcon,
  Database01Icon,
  FilterHorizontalIcon,
  FlashIcon,
  Loading03Icon,
  Wallet01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon, type IconSvgElement } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import {
  lazy,
  Suspense,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'

import { MultiSelect, type Option } from '@/components/multi-select'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useTheme } from '@/context/theme-provider'
import { getKeyUsage } from '@/features/dashboard/api'
import {
  TIME_GRANULARITY_OPTIONS,
  TIME_RANGE_PRESETS,
} from '@/features/dashboard/constants'
import {
  formatCompactNumber,
  formatNumber,
  formatPercent,
  formatQuota,
} from '@/lib/format'
import { getRollingDateRange, type TimeGranularity } from '@/lib/time'
import { cn } from '@/lib/utils'

import { KeyComparisonCharts } from './key-usage-charts'

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

const LazyKeyActivityOffice = lazy(() => import('../key-office'))

type KeyUsageView = 'usage' | 'office'

interface KeyUsageDashboardProps {
  selectedKeyIds: number[]
  onSelectedKeyIdsChange: (keyIds: number[]) => void
}

type TileTone = 'blue' | 'green' | 'amber' | 'cyan' | 'violet' | 'emerald'

const TONE_CLASSES: Record<TileTone, { icon: string; iconShell: string }> = {
  blue: {
    icon: 'text-blue-600',
    iconShell: 'bg-blue-500/10 border-blue-500/20',
  },
  green: {
    icon: 'text-green-600',
    iconShell: 'bg-green-500/10 border-green-500/20',
  },
  amber: {
    icon: 'text-amber-600',
    iconShell: 'bg-amber-500/10 border-amber-500/20',
  },
  cyan: {
    icon: 'text-cyan-600',
    iconShell: 'bg-cyan-500/10 border-cyan-500/20',
  },
  violet: {
    icon: 'text-violet-600',
    iconShell: 'bg-violet-500/10 border-violet-500/20',
  },
  emerald: {
    icon: 'text-emerald-600',
    iconShell: 'bg-emerald-500/10 border-emerald-500/20',
  },
}

function MetricTile(props: {
  label: string
  value: string
  description: string
  icon: IconSvgElement
  tone: TileTone
}) {
  const tone = TONE_CLASSES[props.tone]
  return (
    <div className='bg-card flex min-h-32 items-start justify-between gap-3 rounded-lg border p-4 shadow-xs'>
      <div className='min-w-0'>
        <div className='text-muted-foreground text-xs font-medium'>
          {props.label}
        </div>
        <div className='mt-2 font-mono text-2xl font-semibold tracking-normal whitespace-nowrap tabular-nums'>
          {props.value}
        </div>
        <div className='text-muted-foreground mt-3 text-xs'>
          {props.description}
        </div>
      </div>
      <span
        className={cn(
          'flex size-11 shrink-0 items-center justify-center rounded-lg border',
          tone.iconShell
        )}
      >
        <HugeiconsIcon
          icon={props.icon}
          className={cn('size-5', tone.icon)}
          aria-hidden='true'
        />
      </span>
    </div>
  )
}

function InsightPanel(props: {
  title: string
  description: string
  icon: IconSvgElement
  items: Array<{
    label: string
    value: string
    valueClassName?: string
  }>
}) {
  return (
    <section className='bg-card rounded-lg border p-4 shadow-xs sm:p-5'>
      <div className='flex items-start gap-2'>
        <HugeiconsIcon
          icon={props.icon}
          className='text-muted-foreground mt-0.5 size-4'
          aria-hidden='true'
        />
        <div>
          <h2 className='text-sm font-semibold'>{props.title}</h2>
          <p className='text-muted-foreground mt-1 text-xs'>
            {props.description}
          </p>
        </div>
      </div>
      <dl className='mt-4 space-y-2'>
        {props.items.map((item) => (
          <div
            key={item.label}
            className='bg-muted/35 flex min-h-12 items-center justify-between gap-3 rounded-lg border px-3 py-2'
          >
            <dt className='text-muted-foreground text-sm'>{item.label}</dt>
            <dd
              className={cn(
                'font-mono text-sm font-semibold tabular-nums',
                item.valueClassName
              )}
            >
              {item.value}
            </dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

export default function KeyUsageDashboard(props: KeyUsageDashboardProps) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const [activeView, setActiveView] = useState<KeyUsageView>('usage')
  const [selectedRange, setSelectedRange] = useState(7)
  const [granularity, setGranularity] = useState<TimeGranularity>('day')
  const [themeReady, setThemeReady] = useState(false)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)

  const timeRange = useMemo(() => {
    const range = getRollingDateRange(selectedRange)
    return {
      start_timestamp: Math.floor(range.start.getTime() / 1000),
      end_timestamp: Math.floor(range.end.getTime() / 1000),
    }
  }, [selectedRange])

  const usageQuery = useQuery({
    queryKey: ['dashboard', 'key-usage', timeRange],
    queryFn: () => getKeyUsage(timeRange),
    enabled: activeView === 'usage',
    staleTime: 60_000,
  })

  useEffect(() => {
    const updateTheme = async () => {
      if (activeView !== 'usage') return
      setThemeReady(false)
      if (!themeManagerPromise) {
        themeManagerPromise = import('@visactor/vchart').then(
          (module) => module.ThemeManager
        )
      }
      const ThemeManager = await themeManagerPromise
      themeManagerRef.current = ThemeManager
      ThemeManager.setCurrentTheme(resolvedTheme === 'dark' ? 'dark' : 'light')
      setThemeReady(true)
    }
    void updateTheme()
  }, [activeView, resolvedTheme])

  const points = useMemo(
    () => usageQuery.data?.data.points ?? [],
    [usageQuery.data]
  )
  const keys = useMemo(
    () => usageQuery.data?.data.keys ?? [],
    [usageQuery.data]
  )
  const selectedKeyIdSet = useMemo(
    () => new Set(props.selectedKeyIds),
    [props.selectedKeyIds]
  )
  const filteredPoints = useMemo(() => {
    if (selectedKeyIdSet.size === 0) return points
    return points.filter((point) => selectedKeyIdSet.has(point.token_id))
  }, [points, selectedKeyIdSet])
  const keyOptions = useMemo<Option[]>(
    () =>
      keys.map((key) => ({
        label: key.name || `#${key.id}`,
        value: String(key.id),
      })),
    [keys]
  )
  const keyNameById = useMemo(
    () => new Map(keys.map((key) => [key.id, key.name || `#${key.id}`])),
    [keys]
  )
  const totals = useMemo(() => {
    let successfulRequests = 0
    let failedRequests = 0
    let promptTokens = 0
    let completionTokens = 0
    let quota = 0
    let cacheReadTokens = 0
    let cacheCreateTokens = 0
    let useTimeSeconds = 0
    let latencyMsSum = 0
    let latencySamples = 0
    for (const point of filteredPoints) {
      successfulRequests += point.successful_requests
      failedRequests += point.failed_requests
      promptTokens += point.prompt_tokens
      completionTokens += point.completion_tokens
      quota += point.quota
      cacheReadTokens += point.cache_read_tokens
      cacheCreateTokens += point.cache_create_tokens
      useTimeSeconds += point.use_time_seconds
      latencyMsSum += point.latency_ms_sum
      latencySamples += point.latency_samples
    }
    const totalRequests = successfulRequests + failedRequests
    const totalTokens = promptTokens + completionTokens
    return {
      successfulRequests,
      failedRequests,
      totalRequests,
      totalTokens,
      promptTokens,
      completionTokens,
      quota,
      cacheReadTokens,
      cacheCreateTokens,
      successRate:
        totalRequests > 0 ? (successfulRequests / totalRequests) * 100 : 0,
      averageLatencyMs: latencySamples > 0 ? latencyMsSum / latencySamples : 0,
      outputTokensPerSecond:
        useTimeSeconds > 0 ? completionTokens / useTimeSeconds : 0,
      averageTokensPerRequest:
        successfulRequests > 0 ? totalTokens / successfulRequests : 0,
      cacheReadRatio:
        promptTokens > 0 ? (cacheReadTokens / promptTokens) * 100 : 0,
      cacheCreateRatio:
        promptTokens > 0 ? (cacheCreateTokens / promptTokens) * 100 : 0,
    }
  }, [filteredPoints])

  let filterSummary = ''
  if (props.selectedKeyIds.length === 1) {
    const selectedKeyId = props.selectedKeyIds[0]
    filterSummary = t('Filtered to {{name}}', {
      name: keyNameById.get(selectedKeyId) || `#${selectedKeyId}`,
    })
  } else if (props.selectedKeyIds.length > 1) {
    filterSummary = t('{{count}} API keys selected', {
      count: props.selectedKeyIds.length,
    })
  }

  const rangeDescription = t('Across selected range')
  const summaryTiles = [
    {
      label: t('Total Requests'),
      value: formatCompactNumber(totals.totalRequests, 'en-US'),
      description: t('{{count}} failed', {
        count: formatCompactNumber(totals.failedRequests, 'en-US'),
      }),
      icon: Activity01Icon,
      tone: 'blue' as const,
    },
    {
      label: t('Success Rate'),
      value: formatPercent(totals.successRate),
      description: t('{{count}} successful', {
        count: formatCompactNumber(totals.successfulRequests, 'en-US'),
      }),
      icon: CheckmarkCircle01Icon,
      tone: 'green' as const,
    },
    {
      label: t('Total Tokens'),
      value: formatCompactNumber(totals.totalTokens, 'en-US'),
      description: t('{{count}} per request', {
        count: formatCompactNumber(totals.averageTokensPerRequest, 'en-US'),
      }),
      icon: FlashIcon,
      tone: 'amber' as const,
    },
    {
      label: t('Cache Read'),
      value: formatCompactNumber(totals.cacheReadTokens, 'en-US'),
      description: formatPercent(totals.cacheReadRatio),
      icon: CloudDownloadIcon,
      tone: 'cyan' as const,
    },
    {
      label: t('Cache Creation'),
      value: formatCompactNumber(totals.cacheCreateTokens, 'en-US'),
      description: formatPercent(totals.cacheCreateRatio),
      icon: CloudUploadIcon,
      tone: 'violet' as const,
    },
    {
      label: t('Total Cost'),
      value: formatQuota(totals.quota),
      description: rangeDescription,
      icon: Wallet01Icon,
      tone: 'emerald' as const,
    },
  ]

  const theme = resolvedTheme === 'dark' ? 'dark' : 'light'

  let content: ReactNode
  if (activeView === 'office') {
    content = (
      <Suspense fallback={<Skeleton className='h-[620px] rounded-xl' />}>
        <LazyKeyActivityOffice />
      </Suspense>
    )
  } else if (usageQuery.isLoading) {
    content = (
      <>
        <Skeleton className='h-9 w-full max-w-md' />
        <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6'>
          {Array.from({ length: 6 }, (_, index) => (
            <Skeleton key={index} className='h-32 rounded-lg' />
          ))}
        </div>
        <div className='grid gap-3 xl:grid-cols-2'>
          <Skeleton className='h-[380px] rounded-lg' />
          <Skeleton className='h-[380px] rounded-lg' />
        </div>
      </>
    )
  } else {
    content = (
      <>
        <div className='flex flex-col gap-2 lg:flex-row lg:items-end lg:justify-between'>
          <div className='flex items-center gap-1.5 overflow-x-auto pb-1 sm:gap-2'>
            <Tabs
              value={String(selectedRange)}
              onValueChange={(value) => setSelectedRange(Number(value))}
              className='shrink-0'
            >
              <TabsList>
                {TIME_RANGE_PRESETS.map((preset) => (
                  <TabsTrigger
                    key={preset.days}
                    value={String(preset.days)}
                    className='px-2.5 text-xs'
                  >
                    {t(preset.label)}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
            <Tabs
              value={granularity}
              onValueChange={(value) =>
                setGranularity(value as TimeGranularity)
              }
              className='shrink-0'
            >
              <TabsList>
                {TIME_GRANULARITY_OPTIONS.map((option) => (
                  <TabsTrigger
                    key={option.value}
                    value={option.value}
                    className='px-2.5 text-xs'
                  >
                    {t(option.label)}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
            {usageQuery.isFetching && (
              <HugeiconsIcon
                icon={Loading03Icon}
                className='text-muted-foreground size-4 animate-spin'
                aria-label={t('Loading')}
              />
            )}
          </div>

          <div className='flex min-w-0 flex-col gap-1.5 sm:flex-row sm:items-center'>
            <label
              htmlFor='api-key-usage-filter'
              className='text-muted-foreground flex shrink-0 items-center gap-1.5 text-xs font-medium'
            >
              <HugeiconsIcon
                icon={FilterHorizontalIcon}
                className='size-4'
                aria-hidden='true'
              />
              {t('API Key Filter')}
            </label>
            <MultiSelect
              id='api-key-usage-filter'
              options={keyOptions}
              selected={props.selectedKeyIds.map(String)}
              onChange={(values) =>
                props.onSelectedKeyIdsChange(
                  values
                    .map(Number)
                    .filter((value) => Number.isInteger(value) && value > 0)
                )
              }
              placeholder={t('All API Keys')}
              emptyText={t('No matching items')}
              maxVisibleChips={2}
              className='min-h-8 sm:w-80'
            />
          </div>
        </div>

        {props.selectedKeyIds.length > 0 && (
          <div
            className='border-primary/20 bg-primary/5 flex min-h-9 flex-wrap items-center gap-2 rounded-lg border px-3 py-1.5'
            role='status'
          >
            <HugeiconsIcon
              icon={FilterHorizontalIcon}
              className='text-primary size-4'
              aria-hidden='true'
            />
            <Badge variant='secondary' className='max-w-full truncate'>
              {filterSummary}
            </Badge>
            <Button
              variant='ghost'
              size='xs'
              className='ml-auto'
              onClick={() => props.onSelectedKeyIdsChange([])}
            >
              <HugeiconsIcon icon={Cancel01Icon} aria-hidden='true' />
              {t('Clear filters')}
            </Button>
          </div>
        )}

        <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6'>
          {summaryTiles.map((tile) => (
            <MetricTile key={tile.label} {...tile} />
          ))}
        </div>

        <div className='grid gap-3 lg:grid-cols-2'>
          <InsightPanel
            title={t('Quality Signals')}
            description={t('Evaluate the stability of your API keys.')}
            icon={Analytics01Icon}
            items={[
              {
                label: t('Average Latency'),
                value: `${formatNumber(totals.averageLatencyMs)} ms`,
                valueClassName: 'text-blue-600',
              },
              {
                label: t('Average Output Speed'),
                value: `${formatNumber(totals.outputTokensPerSecond)} tok/s`,
                valueClassName: 'text-blue-600',
              },
              {
                label: t('Failed Requests'),
                value: formatCompactNumber(totals.failedRequests, 'en-US'),
                valueClassName:
                  totals.failedRequests > 0
                    ? 'text-amber-600'
                    : 'text-green-600',
              },
            ]}
          />
          <InsightPanel
            title={t('Cache Insights')}
            description={t('Understand the benefit of prompt caching.')}
            icon={Database01Icon}
            items={[
              {
                label: t('Cache Read Ratio'),
                value: formatPercent(totals.cacheReadRatio),
                valueClassName: 'text-blue-600',
              },
              {
                label: t('Cache Creation Ratio'),
                value: formatPercent(totals.cacheCreateRatio),
                valueClassName: 'text-violet-600',
              },
              {
                label: t('Average Tokens per Request'),
                value: formatCompactNumber(
                  totals.averageTokensPerRequest,
                  'en-US'
                ),
                valueClassName: 'text-green-600',
              },
            ]}
          />
        </div>

        {themeReady ? (
          <KeyComparisonCharts
            keys={keys}
            points={points}
            filteredPoints={filteredPoints}
            selectedKeyIds={props.selectedKeyIds}
            granularity={granularity}
            theme={theme}
            onKeySelect={(keyId) => props.onSelectedKeyIdsChange([keyId])}
          />
        ) : (
          <div className='grid gap-3 xl:grid-cols-2'>
            <Skeleton className='h-[380px] rounded-lg' />
            <Skeleton className='h-[380px] rounded-lg' />
          </div>
        )}
      </>
    )
  }

  return (
    <div className='space-y-3'>
      <Tabs
        value={activeView}
        onValueChange={(value) => setActiveView(value as KeyUsageView)}
      >
        <TabsList aria-label={t('API Key Usage')}>
          <TabsTrigger value='usage'>{t('API Key Usage')}</TabsTrigger>
          <TabsTrigger value='office'>{t('API Key Office')}</TabsTrigger>
        </TabsList>
      </Tabs>

      {content}
    </div>
  )
}
