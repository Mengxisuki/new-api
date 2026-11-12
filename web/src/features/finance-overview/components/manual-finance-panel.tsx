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
import type { TFunction } from 'i18next'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { PanelWrapper } from '@/features/dashboard/components/ui/panel-wrapper'
import { ADJUSTMENT_REASON_LABELS } from '@/features/users/lib/adjustment-reasons'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatQuota, formatTimestamp } from '@/lib/format'

import { getFinanceAdjustments, type FinanceOverview } from '../api'
import { formatCNY } from '../constants'

// 分类比金额更重要，采用可读表格，不为不同权益量制造同一条比例尺。
// 钱包展示净额度，订阅展示操作笔数，现金和应收始终独立成列。
// 复用调额入口的原因文案，不能在总览另维护一套含义不同的分类。
//
// 阅读口径：
// - 人工线下现金已包含在上方钱包/订阅收入中，这里仅作拆分说明。
// - 挂帐发生额是本期新增应收，不等同当前未结清余额。
// - 核销按发生时段显示，不能直接从“实收减退款”再次扣除。
// - 正负 quota 是权益净变化，不按现价折算现金或预估成本。
// - 订阅赠送同样有操作笔数，但没有钱包 quota 的前后余额。
// - 单据和类别均来自后端，界面不按备注关键词猜分类。
// - 历史未分类显示数据边界，不用当前余额生成虚构的历史记录。
//
// 展开明细：
// - 汇总请求本身已包含所有分类，不因展开与否改变金额。
// - 明细使用相同的开始/结束时间，避免显示另一时段的数据。
// - 页码只属于流水列表，不影响上方指标或独立的订阅甘特图。
// - 页面切换时间范围时由父级 key 重置页码，不依赖 effect 补救。
// - 网络失败展示错误与重试，不把失败响应当作空表。
// - 没有展开时不请求凭证，减少默认页面的敏感数据暴露。
// - 未授权用户在父路由和真实接口被拒绝，不靠此组件隐藏实现授权。
//
// 明细关联：
// - 每笔保留用户编号，改名/删除后仍能按编号追溯。
// - 操作人编号来自服务器，不允许在本页面重写。
// - 现金凭证用于对账，挂帐显示人工单号以便在挂帐管理查找。
// - original_id 只是额度更正的来源，不能当作现金退款凭证。
// - source_id 对订阅表示权益实例，续期的原始快照仍由后端保存。
// - 备注按 React 文本渲染，不解释其中的 HTML 或链接指令。
// - 当前只读，不提供删除、改金额或重新执行调额按钮。

const reasonLabels: Record<string, string> = ADJUSTMENT_REASON_LABELS

// 两张只读财务表共用相同的文本列约定，金额先按业务单位格式化。
// 不在列渲染器里求和或反推现金，避免某列偷偷使用不同统计口径。
// 列键与翻译文案分离，切换界面语言不会改变行身份。
type FinanceTableRow = Record<string, string>
function financialTextColumns(fields: Record<string, string>, t: TFunction) {
  return Object.entries(fields).map(([id, label]) => ({
    // 保留业务列顺序。
    id,
    header: t(label), // 渲染时翻译，语言切换立即生效。
    cell: (row: FinanceTableRow) => row[id], // 只输出文本，不注入 HTML。
  }))
}

const summaryFields = {
  source: 'Business line', // 区分钱包和订阅，权益单位不可混加。
  reason: 'Adjustment classification', // 与操作入口共用分类键。
  quota: 'Net quota change', // 钱包额度净差，订阅不适用。
  cash: 'Offline collection', // 已包含在总实收中的线下现金。
  debt: 'On-account amount', // 本期发生应收，不是实时欠款余额。
  count: 'Count', // 操作次数，不是订阅剩余天数。
}
const recordFields = {
  id: 'ID', // 原记录编号，用于纠错时关联。
  date: 'Date', // 首次提交时间，不随重试变化。
  user: 'User', // 记账时的名称快照加固定编号。
  reason: 'Adjustment classification', // 不依备注推断类别。
  source: 'Business line', // 订阅同时展示实际权益实例。
  quota: 'Net quota change', // 不使用人民币格式化额度。
  cash: 'Offline collection', // 非现金类合法为零。
  debt: 'On-account amount', // 线下现金类必须为零。
  reference: 'Receipt reference', // 现金凭证或人工来源单号。
  original: 'Original record ID', // 纠错证据，不代表退款动作。
  operator: 'Operator', // 经服务端认证的操作者编号。
  note: 'Note', // 原始说明只作文本渲染。
}

export function ManualFinancePanel(props: { data: FinanceOverview }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [expanded, setExpanded] = useState(false) // 默认不取回凭证明细。
  const [page, setPage] = useState(1) // 与后端的一基页码一致。
  // 原始流水只在展开后请求，金额概览不依赖额外网络请求才能显示。
  // 父组件按查询区间设置 key，切换区间后不会留在旧区间的高页码。
  const records = useQuery({
    queryKey: ['finance-adjustments', props.data.start, props.data.end, page],
    queryFn: () =>
      getFinanceAdjustments(props.data.start, props.data.end, page),
    enabled: expanded, // 概览与明细的请求生命周期独立。
  })
  return (
    <PanelWrapper
      title={t('Manual financial activity')}
      description={t(
        'Manual cash is already included in revenue. Quota corrections and write-offs do not create cash refunds.'
      )}
      headerActions={
        <Button
          variant='outline'
          onClick={() => setExpanded(!expanded)}
          aria-expanded={expanded}
        >
          {t('View adjustment records')}
        </Button>
      }
    >
      {/* 核销和现金相邻但不相减，避免误解为净利润或已发生现金支出。 */}
      <dl className='mb-4 grid gap-3 sm:grid-cols-2'>
        <div>
          <dt className='text-muted-foreground text-xs'>
            {t('Offline collection')}
          </dt>
          <dd className='text-lg font-semibold'>
            {formatCNY(props.data.offline_cash_cents, locale)}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-xs'>
            {t('Bad debt written off')}
          </dt>
          <dd className='text-lg font-semibold'>
            {formatCNY(props.data.write_off_cents, locale)} ·{' '}
            {formatNumber(props.data.write_off_count, locale)}
          </dd>
        </div>
      </dl>
      <StaticDataTable<FinanceTableRow>
        data={props.data.manual_adjustments.map((row) => ({
          id: `${row.source}:${row.reason}`, // 类别同名也按权益来源区分。
          source:
            row.source === 'quota_adjust'
              ? t('Wallet top-up')
              : t('Subscription'),
          reason: t(reasonLabels[row.reason] ?? row.reason),
          quota: row.source === 'quota_adjust' ? formatQuota(row.delta) : '—',
          cash: formatCNY(row.cash_cents, locale),
          debt: formatCNY(row.receivable_cents, locale),
          count: formatNumber(row.count, locale), // 笔数不使用额度格式。
        }))}
        getRowKey={(row) => row.id}
        columns={financialTextColumns(summaryFields, t)}
      />
      {/* 没有可靠账本的历史不能从当前余额反推，显示未知而不是伪造一组零值。 */}
      <p className='text-muted-foreground mt-3 text-xs'>
        {t(
          'Historical unclassified adjustments are not reconstructed from balances or counted as cash.'
        )}
      </p>
      {expanded && records.isLoading && <LoadingState />}
      {expanded && records.isError && (
        <ErrorState onRetry={() => void records.refetch()} />
      )}
      {expanded && records.data && !records.isError && (
        <div className='mt-4 space-y-3'>
          {/* 主库快照包含操作人和原始凭证，客户名称为空仍保留编号以便追溯。 */}
          <StaticDataTable<FinanceTableRow>
            data={records.data.items.map((row) => ({
              id: `#${row.id}`, // 这是账本编号，不是用户或债务编号。
              date: formatTimestamp(row.created_at), // 沿用站点时间展示约定。
              user: `#${row.user_id} ${row.username}`, // 名称失效仍保留身份编号。
              reason: t(reasonLabels[row.reason] ?? row.reason),
              source:
                row.source === 'subscription_bind'
                  ? `${t('Subscription')} #${row.source_id}`
                  : t('Wallet top-up'),
              quota:
                row.source === 'quota_adjust' ? formatQuota(row.delta) : '—',
              cash: formatCNY(row.cash_cents, locale),
              debt: formatCNY(row.receivable_cents, locale),
              reference: row.cash_ref || row.trade_no, // 无现金时展示人工原单。
              original: row.original_id > 0 ? `#${row.original_id}` : '—', // 无关联不显示零号单。
              operator: `#${row.operator_id}`, // 不使用当前查看者冒充操作人。
              note: row.note, // 用户说明作为文本，不能改变分类或金额。
            }))}
            getRowKey={(row) => row.id}
            columns={financialTextColumns(recordFields, t)}
          />
          {/* 服务端分页，不把当前页的小计冒充总额；按钮只控制这张明细表。 */}
          <div className='flex items-center justify-end gap-3'>
            <span className='text-muted-foreground text-xs'>
              {formatNumber(records.data.total, locale)} ·{' '}
              {formatNumber(page, locale)}
            </span>
            <Button
              variant='outline'
              disabled={page <= 1 || records.isFetching}
              onClick={() => setPage(page - 1)}
            >
              {t('Previous')}
            </Button>
            <Button
              variant='outline'
              disabled={
                page * records.data.size >= records.data.total ||
                records.isFetching
              }
              onClick={() => setPage(page + 1)}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
      )}
    </PanelWrapper>
  )
}
