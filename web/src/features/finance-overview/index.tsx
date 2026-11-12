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
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DatePicker } from '@/components/date-picker'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { hasPermission } from '@/lib/admin-permissions'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { getFinanceOverview } from './api'
import { CreditPanel, TodoPanel } from './components/credit-panels'
import { ManualFinancePanel } from './components/manual-finance-panel'
import { OverviewMetrics } from './components/overview-metrics'
import { RevenueBreakdown, RevenueTrend } from './components/revenue-panels'
import { SubscriptionGantt } from './components/subscription-gantt'
import {
  MAX_RANGE_DAYS,
  customRange,
  presetRange,
  type RangePreset,
} from './lib/range'

// 预设时段的展示名称是 i18n 键，渲染时通过 t() 翻译。
const PRESETS: { value: RangePreset; labelKey: string }[] = [
  { value: 'today', labelKey: 'Today' },
  { value: '7d', labelKey: 'Last 7 days' },
  { value: '30d', labelKey: 'Last 30 days' },
  { value: 'month', labelKey: 'Month to date' },
  { value: 'last_month', labelKey: 'Last month' },
]

// 财务总览页面：只读汇总平台实收、退款、挂帐待收、钱包余额和待办。
// 收入数字共用顶部时段；订阅甘特图有独立的含未来日期窗口。
// 挂帐、钱包余额和待办是当前快照，各自的说明文字里标明。
// 访问由独立的财务总览权限控制，服务端每次请求都会再次校验。
export function FinanceOverviewPage() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const canRead = hasPermission(user, 'finance_overview', 'read')
  // 选择 'custom' 时用下面两个日期，否则用预设；默认看本月至今。
  const [preset, setPreset] = useState<RangePreset | 'custom'>('month')
  const [from, setFrom] = useState<Date | undefined>()
  const [to, setTo] = useState<Date | undefined>()

  // 自定义时段缺少日期或范围无效时为 null：不发请求，并提示原因。
  let range: { start: number; end: number } | null
  if (preset === 'custom') {
    range = from && to ? customRange(from, to) : null
  } else {
    range = presetRange(preset, Math.floor(Date.now() / 1000))
  }
  const invalidCustom = preset === 'custom' && Boolean(from && to) && !range

  const query = useQuery({
    queryKey: ['finance-overview', range?.start, range?.end],
    // range 为空时查询被禁用；这里仍显式报错，避免误用空范围发出请求。
    queryFn: () => {
      if (!range) throw new Error('finance overview range is not selected')
      return getFinanceOverview(range.start, range.end)
    },
    enabled: canRead && range !== null,
    // 切换时段时保留上一份数据并降低透明度，避免整页闪烁和布局跳动。
    placeholderData: keepPreviousData,
  })

  if (!canRead) return <ErrorState title={t('Access denied')} />
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Financial overview')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <div className='flex flex-wrap items-center gap-3'>
            <Tabs
              value={preset}
              onValueChange={(value) =>
                setPreset(value as RangePreset | 'custom')
              }
            >
              <TabsList aria-label={t('Date Range')}>
                {PRESETS.map((item) => (
                  <TabsTrigger
                    key={item.value}
                    value={item.value}
                    className='px-2.5 text-xs'
                  >
                    {t(item.labelKey)}
                  </TabsTrigger>
                ))}
                <TabsTrigger value='custom' className='px-2.5 text-xs'>
                  {t('Custom')}
                </TabsTrigger>
              </TabsList>
            </Tabs>
            {preset === 'custom' && (
              <>
                <DatePicker
                  selected={from}
                  onSelect={setFrom}
                  placeholder={t('Start date')}
                />
                <DatePicker
                  selected={to}
                  onSelect={setTo}
                  placeholder={t('End date')}
                />
              </>
            )}
            <Button
              variant='outline'
              disabled={query.isFetching || range === null}
              onClick={() => void query.refetch()}
            >
              {t('Refresh')}
            </Button>
          </div>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Amounts are in CNY and grouped by Asia/Shanghai calendar days. On-account orders count as revenue only when settled.'
            )}
          </p>
          {invalidCustom && (
            <p role='alert' className='text-destructive text-sm'>
              {t(
                'The end date must not be earlier than the start date, and the range cannot exceed {{days}} days.',
                { days: MAX_RANGE_DAYS }
              )}
            </p>
          )}
          {preset === 'custom' && !range && !invalidCustom && (
            <p className='text-muted-foreground text-sm'>
              {t('Select a start date and an end date.')}
            </p>
          )}
          {query.isError && <ErrorState onRetry={() => void query.refetch()} />}
          {query.isLoading && <LoadingState />}
          {query.data && !query.isError && range && (
            <div
              className={cn(
                'space-y-4 transition-opacity',
                query.isPlaceholderData && 'opacity-60'
              )}
            >
              <OverviewMetrics data={query.data} />
              <RevenueTrend data={query.data} />
              <RevenueBreakdown data={query.data} />
              <ManualFinancePanel
                key={`${query.data.start}:${query.data.end}`}
                data={query.data}
              />
              <div className='grid gap-3 xl:grid-cols-[2fr_1fr]'>
                <CreditPanel data={query.data} />
                <TodoPanel data={query.data} />
              </div>
            </div>
          )}
          {/* 周期独立查询，收入为空或失败时仍可管理未来号池订阅。 */}
          <SubscriptionGantt />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
