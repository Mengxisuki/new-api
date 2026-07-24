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
import { ChartBarLineIcon, Key01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { VChart } from '@visactor/react-vchart'
import type { EventParamsDefinition } from '@visactor/vchart'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import type {
  KeyUsageKey,
  KeyUsagePoint,
} from '@/features/dashboard/components/key-usage/types'
import { getDashboardChartColors } from '@/features/dashboard/lib/charts'
import { formatQuota } from '@/lib/format'
import { formatChartTime, type TimeGranularity } from '@/lib/time'
import { VCHART_OPTION } from '@/lib/vchart'

// VChart accepts a broad discriminated union; dashboard charts construct specs dynamically.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type ChartSpec = Record<string, any>

function getKeyNameMap(keys: KeyUsageKey[]): Map<number, string> {
  return new Map(keys.map((key) => [key.id, key.name || `#${key.id}`]))
}

interface KeyComparisonChartsProps {
  keys: KeyUsageKey[]
  points: KeyUsagePoint[]
  filteredPoints: KeyUsagePoint[]
  selectedKeyIds: number[]
  granularity: TimeGranularity
  theme: 'light' | 'dark'
  onKeySelect: (keyId: number) => void
}

export function KeyComparisonCharts(props: KeyComparisonChartsProps) {
  const { t } = useTranslation()
  const keyNames = useMemo(() => getKeyNameMap(props.keys), [props.keys])
  const chartData = useMemo(() => {
    const selectedKeyIdSet = new Set(props.selectedKeyIds)
    const totals = new Map<number, number>()
    for (const point of props.points) {
      totals.set(
        point.token_id,
        (totals.get(point.token_id) ?? 0) + point.quota
      )
    }
    const rankedKeys = [...totals.entries()].sort((a, b) => b[1] - a[1])
    const names = rankedKeys.map(
      ([keyId]) => keyNames.get(keyId) || `#${keyId}`
    )
    const colors = getDashboardChartColors(names.length)
    const colorMap = Object.fromEntries(
      names.map((name, index) => [name, colors[index]])
    )
    const rankValues = rankedKeys.map(([keyId, quota]) => ({
      tokenId: keyId,
      key: keyNames.get(keyId) || `#${keyId}`,
      quota,
      selected:
        selectedKeyIdSet.size === 0 || selectedKeyIdSet.has(keyId) ? 1 : 0,
    }))
    const trendMap = new Map<string, Map<number, number>>()
    for (const point of props.filteredPoints) {
      const time = formatChartTime(point.created_at, props.granularity)
      const keyMap = trendMap.get(time) ?? new Map<number, number>()
      keyMap.set(
        point.token_id,
        (keyMap.get(point.token_id) ?? 0) + point.quota
      )
      trendMap.set(time, keyMap)
    }
    const trendValues: Array<{ time: string; key: string; quota: number }> = []
    for (const [time, keyMap] of [...trendMap.entries()].sort(([a], [b]) =>
      a.localeCompare(b)
    )) {
      for (const [keyId] of rankedKeys) {
        if (selectedKeyIdSet.size > 0 && !selectedKeyIdSet.has(keyId)) continue
        trendValues.push({
          time,
          key: keyNames.get(keyId) || `#${keyId}`,
          quota: keyMap.get(keyId) ?? 0,
        })
      }
    }
    return { colorMap, rankValues, trendValues }
  }, [
    keyNames,
    props.filteredPoints,
    props.granularity,
    props.points,
    props.selectedKeyIds,
  ])

  const handleRankClick = (event: EventParamsDefinition['click']) => {
    const tokenId = Number(event.datum?.tokenId)
    if (!Number.isInteger(tokenId) || tokenId <= 0) return
    props.onKeySelect(tokenId)
  }

  const rankSpec = useMemo<ChartSpec>(
    () => ({
      type: 'bar',
      data: [{ id: 'keyUsageRank', values: chartData.rankValues }],
      xField: 'quota',
      yField: 'key',
      seriesField: 'key',
      direction: 'horizontal',
      color: { specified: chartData.colorMap },
      legends: { visible: false },
      axes: [
        { orient: 'left', type: 'band' },
        { orient: 'bottom', type: 'linear', visible: false },
      ],
      label: {
        visible: true,
        position: 'outside',
        formatMethod: (value: number) => formatQuota(value),
        style: { fontSize: 11 },
      },
      bar: {
        style: {
          cornerRadius: [0, 4, 4, 0],
          cursor: 'pointer',
          fillOpacity: (datum: { selected?: number }) =>
            datum.selected === 0 ? 0.28 : 1,
        },
      },
      tooltip: {
        mark: {
          content: [
            {
              key: (datum: Record<string, unknown>) => datum.key,
              value: (datum: Record<string, unknown>) =>
                formatQuota(Number(datum.quota) || 0),
            },
          ],
        },
      },
      padding: { top: 12, right: 52, bottom: 8, left: 8 },
      background: { fill: 'transparent' },
      animation: true,
    }),
    [chartData.colorMap, chartData.rankValues]
  )

  const trendSpec = useMemo<ChartSpec>(
    () => ({
      type: 'area',
      data: [{ id: 'keyUsageByKeyTrend', values: chartData.trendValues }],
      xField: 'time',
      yField: 'quota',
      seriesField: 'key',
      stack: false,
      color: { specified: chartData.colorMap },
      legends: { visible: true, selectMode: 'single' },
      axes: [
        { orient: 'bottom', type: 'band' },
        {
          orient: 'left',
          type: 'linear',
          label: { formatMethod: (value: number) => formatQuota(value) },
        },
      ],
      area: { style: { fillOpacity: 0.12, curveType: 'monotone' } },
      line: { style: { lineWidth: 2, curveType: 'monotone' } },
      point: { visible: false },
      tooltip: {
        dimension: {
          content: [
            {
              key: (datum: Record<string, unknown>) => datum.key,
              value: (datum: Record<string, unknown>) =>
                formatQuota(Number(datum.quota) || 0),
            },
          ],
        },
      },
      padding: { top: 16, right: 20, bottom: 8, left: 8 },
      background: { fill: 'transparent' },
      animation: true,
    }),
    [chartData.colorMap, chartData.trendValues]
  )

  const charts = [
    {
      key: 'rank',
      title: t('API Key Consumption Ranking'),
      icon: ChartBarLineIcon,
      spec: rankSpec,
    },
    {
      key: 'trend',
      title: t('API Key Consumption Trend'),
      icon: Key01Icon,
      spec: trendSpec,
    },
  ]

  return (
    <div className='grid gap-3 xl:grid-cols-2'>
      {charts.map((chart) => (
        <section
          key={chart.key}
          className='bg-card overflow-hidden rounded-lg border shadow-xs'
        >
          <header className='flex items-center gap-2 border-b px-4 py-3 sm:px-5'>
            <HugeiconsIcon
              icon={chart.icon}
              className='text-muted-foreground size-4'
              aria-hidden='true'
            />
            <h2 className='text-sm font-semibold'>{chart.title}</h2>
          </header>
          <div className='h-[320px] p-2 sm:h-[380px]'>
            <VChart
              key={`${chart.key}-${props.granularity}-${props.theme}-${props.selectedKeyIds.join(',')}`}
              spec={{ ...chart.spec, theme: props.theme }}
              option={VCHART_OPTION}
              onClick={chart.key === 'rank' ? handleRankClick : undefined}
            />
          </div>
        </section>
      ))}
    </div>
  )
}
