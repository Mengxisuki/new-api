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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { PanelWrapper } from '@/features/dashboard/components/ui/panel-wrapper'
import { toIntlLocale } from '@/i18n/languages'
import { hasPermission } from '@/lib/admin-permissions'
import { formatNumber } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import type { FinanceOverview } from '../api'
import { formatCNY } from '../constants'

// 挂帐面板：授信占用、账龄分布和欠款最多的用户。
// 除“本期新增”外都是当前时点快照，不随所选时段变化。
// 账龄是有先后次序的三档金额，用小表给出精确数字，不用颜色编码大小。
export function CreditPanel(props: { data: FinanceOverview }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const user = useAuthStore((state) => state.auth.user)
  // 总览权限与财务办理权限相互独立；没有挂帐读取能力时不提供跳转。
  const canOpenCredit = hasPermission(user, 'finance', 'read')
  const credit = props.data.credit
  const utilization =
    credit.limit_cents > 0
      ? `${formatNumber(Math.round((credit.used_cents / credit.limit_cents) * 1000) / 10, locale)}%`
      : '—'
  const aging = [
    { label: t('Within 30 days'), cents: credit.aging30_cents },
    { label: t('31 to 60 days'), cents: credit.aging60_cents },
    { label: t('Over 60 days'), cents: credit.aging_over_cents },
  ]
  const facts = [
    {
      label: t('Credit limit granted'),
      value: formatCNY(credit.limit_cents, locale),
    },
    { label: t('Credit in use'), value: formatCNY(credit.used_cents, locale) },
    { label: t('Utilization'), value: utilization },
    {
      label: t('New on-account in period'),
      value: formatCNY(credit.new_cents, locale),
    },
  ]

  return (
    <PanelWrapper
      title={t('On-account receivables')}
      description={t(
        'Current snapshot. Entries older than 30 days are treated as overdue.'
      )}
      headerActions={
        canOpenCredit ? (
          <Button
            size='sm'
            variant='outline'
            render={<Link to='/finance/credit' />}
          >
            {t('On-account management')}
          </Button>
        ) : undefined
      }
    >
      <dl className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
        {facts.map((fact) => (
          <div key={fact.label}>
            <dt className='text-muted-foreground text-xs'>{fact.label}</dt>
            <dd className='mt-1 text-sm font-semibold'>{fact.value}</dd>
          </div>
        ))}
      </dl>
      <div className='mt-4 grid gap-4 lg:grid-cols-2'>
        <StaticDataTable
          data={aging}
          getRowKey={(row) => row.label}
          columns={[
            { id: 'age', header: t('Age of debt'), cell: (row) => row.label },
            {
              id: 'amount',
              header: t('Amount (CNY)'),
              cellClassName: 'tabular-nums',
              cell: (row) => formatCNY(row.cents, locale),
            },
          ]}
        />
        <StaticDataTable
          data={credit.top_debtors}
          getRowKey={(row) => row.user_id}
          emptyContent={t('No outstanding on-account entries')}
          columns={[
            {
              id: 'user',
              header: t('Largest debtors'),
              // 用户删除后名称为空，编号始终保留以便在挂帐管理里查找。
              cell: (row) =>
                `#${row.user_id} ${row.display_name || row.username}`.trim(),
            },
            {
              id: 'amount',
              header: t('Amount (CNY)'),
              cellClassName: 'tabular-nums',
              cell: (row) => formatCNY(row.cents, locale),
            },
            {
              id: 'count',
              header: t('Entries'),
              cellClassName: 'tabular-nums',
              cell: (row) => formatNumber(row.count, locale),
            },
          ]}
        />
      </div>
    </PanelWrapper>
  )
}

// 待办面板：只给数量和入口，办理仍在各自页面完成。
// 每个入口按目标页面的真实权限显示，没有权限时只显示数量。
export function TodoPanel(props: { data: FinanceOverview }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const user = useAuthStore((state) => state.auth.user)
  const canFinance = hasPermission(user, 'finance', 'read')
  const canRecharge = hasPermission(user, 'recharge_orders', 'read')
  const items = [
    {
      label: t('Invoices awaiting processing'),
      count: props.data.todo.pending_invoices,
      to: canFinance ? ('/finance/invoices' as const) : undefined,
    },
    {
      label: t('On-account entries overdue'),
      count: props.data.credit.overdue_count,
      to: canFinance ? ('/finance/credit' as const) : undefined,
    },
    {
      label: t('Recharge refunds awaiting action'),
      count: props.data.todo.pending_refunds,
      to: canRecharge ? ('/recharge-orders' as const) : undefined,
    },
  ]

  return (
    <PanelWrapper
      title={t('Needs attention')}
      description={t(
        'Open items right now, regardless of the selected period.'
      )}
    >
      <ul className='divide-y'>
        {items.map((item) => (
          <li
            key={item.label}
            className='flex items-center justify-between gap-3 py-2.5 first:pt-0 last:pb-0'
          >
            <span className='text-sm'>{item.label}</span>
            <span className='flex items-center gap-3'>
              <span className='text-sm font-semibold tabular-nums'>
                {formatNumber(item.count, locale)}
              </span>
              {item.to && (
                <Button
                  size='sm'
                  variant='outline'
                  render={<Link to={item.to} />}
                  aria-label={`${t('View')}: ${item.label}`}
                >
                  {t('View')}
                </Button>
              )}
            </span>
          </li>
        ))}
      </ul>
    </PanelWrapper>
  )
}
