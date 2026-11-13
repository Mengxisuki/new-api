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
import { useTranslation } from 'react-i18next'

import { toIntlLocale } from '@/i18n/languages'
import { formatQuota } from '@/lib/format'

import type { FinanceOverview } from '../api'
import { formatCNY } from '../constants'

// 一个指标格：标题、主数字和一行口径说明。
// 这里的数字都是单一值，直接用数字展示，不为单个值画图表。
function Metric(props: { label: string; value: string; hint: string }) {
  return (
    <div className='bg-card rounded-xl border p-4'>
      <div className='text-muted-foreground text-xs font-medium'>
        {props.label}
      </div>
      <div className='mt-2 text-xl font-semibold break-all sm:text-2xl'>
        {props.value}
      </div>
      <p className='text-muted-foreground mt-1 text-xs'>{props.hint}</p>
    </div>
  )
}

// 顶部指标卡：实收、退款、净收入属于所选时段；
// 挂帐待收和钱包余额是当前时点快照，说明文字里明确区分，避免误读为时段数字。
export function OverviewMetrics(props: { data: FinanceOverview }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const data = props.data
  return (
    <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-3'>
      <Metric
        label={t('Net revenue')}
        value={formatCNY(data.net_cents, locale)}
        hint={t('Cash received minus refunds in the selected period.')}
      />
      <Metric
        label={t('Cash received')}
        value={formatCNY(data.received_cents, locale)}
        hint={t('Includes invoice surcharges of {{amount}}.', {
          amount: formatCNY(data.invoice_fee_cents, locale),
        })}
      />
      <Metric
        label={t('Refunds')}
        value={formatCNY(data.refund_cents, locale)}
        hint={t(
          'Refunds recorded on the platform. Refunds issued directly in the payment provider console are not included.'
        )}
      />
      <Metric
        label={t('Outstanding on-account')}
        value={formatCNY(data.credit.outstanding_cents, locale)}
        hint={t(
          'Current balance across {{count}} unsettled entries. Not counted as revenue until settled.',
          { count: data.credit.outstanding_count }
        )}
      />
      <Metric
        label={t('Unredeemed sold codes')}
        value={formatCNY(data.redemption.unredeemed_sale_cents, locale)}
        hint={t(
          'Current registered value of {{count}} sold codes awaiting redemption. Gifted: {{gifted}}, unclassified: {{unknown}}. Not cash received.',
          {
            count: data.redemption.unredeemed_sale_count,
            gifted: data.redemption.unredeemed_gift_count,
            unknown: data.redemption.unredeemed_unknown_count,
          }
        )}
      />
      <Metric
        label={t('Wallet balance held')}
        value={formatQuota(data.wallet_balance_quota)}
        hint={t('Current wallet quota, including bonuses and manual adjustments. Not a cash balance.')}
      />
      <Metric
        label={t('Quota consumed')}
        value={
          data.consumption_available
            ? formatQuota(data.consumed_quota)
            : t('Not available')
        }
        hint={
          data.consumption_available
            ? t('Usage billed in the selected period.')
            : t(
                'Usage statistics are turned off, so consumption is not recorded.'
              )
        }
      />
    </div>
  )
}
