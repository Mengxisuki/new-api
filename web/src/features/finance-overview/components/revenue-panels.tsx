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
import { VChart } from '@visactor/react-vchart'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { PanelWrapper } from '@/features/dashboard/components/ui/panel-wrapper'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { useThemeColor } from '@/lib/theme-color'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

import type { FinanceDaily, FinanceOverview } from '../api'
import { CHANNEL_LABEL_KEYS, REVENUE_LINES, formatCNY } from '../constants'
import { formatShanghaiDay } from '../lib/range'

// 收入趋势：按上海自然日堆叠四条业务线的实收。
// 图表用于看走势，表格视图给出每一天的精确金额，两者数据完全相同；
// 浅色模式下部分颜色对比度不足，因此表格视图是必备的等价读法，不是可选项。
export function RevenueTrend(props: { data: FinanceOverview }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const { resolvedTheme, themeReady } = useChartTheme()
  // 图表画在卡片上，段间间隙取卡片底色，随主题和配色预设变化。
  const surface = useThemeColor('--card')
  const [view, setView] = useState('chart')
  const daily = props.data.daily
  const dark = resolvedTheme === 'dark'

  const spec = useMemo(() => {
    // 颜色按业务线名称指定而不是按出现顺序，没有收入的业务线也不会让其他颜色错位。
    const colors: Record<string, string> = {}
    const values = daily.flatMap((day) =>
      REVENUE_LINES.map((line) => {
        const label = t(line.labelKey)
        colors[label] = dark ? line.dark : line.light
        return {
          day: formatShanghaiDay(day.day),
          line: label,
          amount: day[line.key] / 100,
        }
      })
    )
    return {
      type: 'bar',
      data: [{ id: 'revenue', values }],
      xField: 'day',
      yField: 'amount',
      seriesField: 'line',
      stack: true,
      color: { specified: colors },
      // 柱宽封顶；各段用卡片底色描边形成 2px 间隙来分隔，不画有色边框。
      // 圆角只加在整根柱子的顶端，底部贴着基线保持平直。
      barMaxWidth: 24,
      stackCornerRadius: [4, 4, 0, 0],
      bar: { style: { stroke: surface, lineWidth: 2 } },
      legends: { visible: true, orient: 'top', position: 'start' },
      axes: [
        { orient: 'bottom', type: 'band', label: { autoHide: true } },
        {
          orient: 'left',
          type: 'linear',
          label: {
            formatMethod: (value: number) => formatNumber(value, locale),
          },
        },
      ],
      tooltip: {
        mark: {
          content: [
            {
              key: (datum: Record<string, unknown>) => datum?.line,
              value: (datum: Record<string, unknown>) =>
                formatCNY(Number(datum?.amount) * 100, locale),
            },
          ],
        },
        dimension: {
          content: [
            {
              key: (datum: Record<string, unknown>) => datum?.line,
              value: (datum: Record<string, unknown>) =>
                formatCNY(Number(datum?.amount) * 100, locale),
            },
          ],
        },
      },
      background: 'transparent',
      theme: dark ? 'dark' : 'light',
    }
  }, [daily, dark, locale, surface, t])

  return (
    <PanelWrapper
      title={t('Revenue trend')}
      description={t(
        'Cash received per day (Asia/Shanghai), stacked by business line.'
      )}
      empty={props.data.received_cents === 0}
      emptyMessage={t('No revenue in the selected period')}
      height='h-72'
      headerActions={
        <Tabs value={view} onValueChange={setView}>
          <TabsList>
            <TabsTrigger value='chart' className='px-2.5 text-xs'>
              {t('Chart')}
            </TabsTrigger>
            <TabsTrigger value='table' className='px-2.5 text-xs'>
              {t('Table view')}
            </TabsTrigger>
          </TabsList>
        </Tabs>
      }
    >
      {view === 'chart' ? (
        <div className='h-72'>
          {themeReady && (
            <VChart
              key={`revenue-${resolvedTheme}`}
              spec={spec}
              option={VCHART_OPTION}
            />
          )}
        </div>
      ) : (
        <StaticDataTable<FinanceDaily>
          className='max-h-72 overflow-y-auto'
          data={daily}
          getRowKey={(row) => row.day}
          columns={[
            {
              id: 'day',
              header: t('Date'),
              cell: (row) => formatShanghaiDay(row.day),
            },
            ...REVENUE_LINES.map((line) => ({
              id: line.key,
              header: t(line.labelKey),
              cellClassName: 'tabular-nums',
              cell: (row: FinanceDaily) => formatCNY(row[line.key], locale),
            })),
          ]}
        />
      )}
    </PanelWrapper>
  )
}

// 收入构成：业务线和收款渠道两张小表。
// 构成只有三四项且都要看精确金额，用表格比饼图更直接，占比列提供相对大小。
export function RevenueBreakdown(props: { data: FinanceOverview }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const data = props.data
  const share = (cents: number) =>
    data.received_cents > 0
      ? `${formatNumber(Math.round((cents / data.received_cents) * 1000) / 10, locale)}%`
      : '—'
  const lines = REVENUE_LINES.map((line) => ({
    label: t(line.labelKey),
    cents: data[line.key],
  }))

  return (
    <div className='grid gap-3 lg:grid-cols-2'>
      <PanelWrapper
        title={t('Revenue by business line')}
        description={t(
          'Credit settlement is money collected for earlier on-account orders.'
        )}
      >
        <StaticDataTable
          data={lines}
          getRowKey={(row) => row.label}
          columns={[
            {
              id: 'line',
              header: t('Business line'),
              cell: (row) => row.label,
            },
            {
              id: 'amount',
              header: t('Amount (CNY)'),
              cellClassName: 'tabular-nums',
              cell: (row) => formatCNY(row.cents, locale),
            },
            {
              id: 'share',
              header: t('Share'),
              cellClassName: 'tabular-nums',
              cell: (row) => share(row.cents),
            },
          ]}
        />
      </PanelWrapper>
      <PanelWrapper
        title={t('Revenue by payment channel')}
        description={t('Only CNY channels are added into cash received.')}
      >
        <StaticDataTable
          data={data.channels}
          getRowKey={(row) => row.channel}
          emptyContent={t('No revenue in the selected period')}
          columns={[
            {
              id: 'channel',
              header: t('Channel'),
              cell: (row) =>
                CHANNEL_LABEL_KEYS[row.channel]
                  ? t(CHANNEL_LABEL_KEYS[row.channel])
                  : row.channel,
            },
            {
              id: 'amount',
              header: t('Amount (CNY)'),
              cellClassName: 'tabular-nums',
              cell: (row) => formatCNY(row.cents, locale),
            },
            {
              id: 'count',
              header: t('Orders'),
              cellClassName: 'tabular-nums',
              cell: (row) => formatNumber(row.count, locale),
            },
          ]}
        />
        {/* 其他网关缺少统一的真实收款快照，存储值可能是额度面额，不能当作现金。 */}
        {data.other_gateways.length > 0 && (
          <div className='mt-4 space-y-1 text-xs'>
            <p className='text-muted-foreground'>
              {t(
                'Other gateway figures are stored order values, not verified cash receipts. They are excluded from the totals.'
              )}
            </p>
            {data.other_gateways.map((gateway) => (
              <p key={gateway.provider} className='tabular-nums'>
                {gateway.provider}: {formatNumber(gateway.money, locale)} ·{' '}
                {t('{{count}} orders', { count: gateway.count })}
              </p>
            ))}
          </div>
        )}
      </PanelWrapper>
    </div>
  )
}
