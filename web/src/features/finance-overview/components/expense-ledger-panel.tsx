import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { PanelWrapper } from '@/features/dashboard/components/ui/panel-wrapper'
import { formatCNY } from '@/features/finance-overview/constants'
import { toIntlLocale } from '@/i18n/languages'
import { handleServerError } from '@/lib/handle-server-error'

import {
  consumeFinanceExpenseBalance,
  createFinanceExpense,
  getFinanceExpenses,
  type FinanceExpense,
} from '../api'

function expenseKindLabel(kind: FinanceExpense['kind']) {
  // 后端枚举是稳定合同；未知值按其他支出展示，而不是猜测业务分类。
  if (kind === 'subscription') return 'Subscription'
  if (kind === 'recharge') return 'Account top-up'
  return 'Other'
}

// 支出台账同时展示现金和成本；两者口径不同，禁止在组件内互相推导。
export function ExpenseLedgerPanel(props: {
  start: number
  end: number
  canWrite: boolean
}) {
  // 汇总沿用财务总览的同一日期窗口，切换窗口时 React Query 会自动丢弃旧结果。
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  // 表单只保存草稿；金额在提交时转换为整数分，避免浮点数进入账本。
  const [kind, setKind] = useState<FinanceExpense['kind']>('subscription')
  const [title, setTitle] = useState('')
  const [amount, setAmount] = useState('')
  const [occurred, setOccurred] = useState('')
  const [coverageStart, setCoverageStart] = useState('')
  const [coverageEnd, setCoverageEnd] = useState('')
  const [remaining, setRemaining] = useState('')
  const [note, setNote] = useState('')
  // 充值采购允许多次登记剩余余额，每一行独立保存输入，避免更新一项覆盖另一项。
  const [consumeValues, setConsumeValues] = useState<Record<number, string>>({})
  // 明细请求失败必须可见并可重试，不把错误当成没有支出。
  const query = useQuery({
    queryKey: ['finance-expenses', props.start, props.end],
    queryFn: () => getFinanceExpenses(props.start, props.end),
  })
  // 创建支出成功后只刷新支出查询，不刷新收入和订阅周期等无关面板。
  const mutation = useMutation({
    mutationFn: () => {
      // 日期输入按上海零点解释，与后端自然日统计保持一致。
      const toSeconds = (value: string) => Math.floor(new Date(`${value}T00:00:00+08:00`).getTime() / 1000)
      const occurredAt = toSeconds(occurred)
      return createFinanceExpense({
        // 订阅结束日期按日期输入的次日零点保存，形成左闭右开区间。
        kind,
        title,
        amount_cents: Math.round(Number(amount) * 100),
        occurred_at: occurredAt,
        coverage_start: kind === 'subscription' ? toSeconds(coverageStart) : 0,
        coverage_end: kind === 'subscription' ? toSeconds(coverageEnd) + 86400 : 0,
        remaining_balance_cents: kind === 'recharge' ? Math.round(Number(remaining || amount) * 100) : 0,
        note,
      })
    },
    onSuccess: () => {
      // 清空草稿，避免下一笔支出继承上一笔的覆盖期或余额。
      setTitle('')
      setAmount('')
      setOccurred('')
      setCoverageStart('')
      setCoverageEnd('')
      setRemaining('')
      setNote('')
      void client.invalidateQueries({ queryKey: ['finance-expenses'] })
    },
    onError: (error) => handleServerError(error, t('Failed to save expense')),
  })
  // 余额减少由服务端记录差额和发生时间，客户端不自行计算成本合计。
  const consumeMutation = useMutation({
    mutationFn: (input: { id: number; remaining: number }) =>
      consumeFinanceExpenseBalance(input.id, input.remaining, Math.floor(Date.now() / 1000)),
    onSuccess: () => {
      // 更新后重新取回成本汇总，显示服务端最终剩余余额。
      setConsumeValues({})
      void client.invalidateQueries({ queryKey: ['finance-expenses'] })
    },
    onError: (error) => handleServerError(error, t('Failed to save expense')),
  })
  return (
    <PanelWrapper
      title={t('Expense ledger')}
      description={t('Cash is recorded on purchase; cost is amortized or recognized when balance is consumed.')}
    >
      {/* 这个面板属于财务总览，现金与成本数字不参与收入合计。 */}
      {query.isLoading && <LoadingState />}
      {/* 读取状态独立于写入状态，提交失败不会隐藏已有账本。 */}
      {query.isError && <ErrorState onRetry={() => void query.refetch()} />}
      {query.data && !query.isError && (
        <>
          {/* 现金是采购日付款，成本是订阅摊销或充值余额实际消耗，两者必须并列显示。 */}
          <dl className='mb-4 grid gap-3 sm:grid-cols-2'>
            {/* 金额由接口以整数分返回，格式化只负责本地化显示。 */}
            <div><dt className='text-muted-foreground text-xs'>{t('Cash expenses')}</dt><dd className='text-lg font-semibold'>{formatCNY(query.data.cash_cents, locale)}</dd></div>
            <div><dt className='text-muted-foreground text-xs'>{t('Cost expenses')}</dt><dd className='text-lg font-semibold'>{formatCNY(query.data.cost_cents, locale)}</dd></div>
          </dl>
          {/* 明细保留剩余余额，便于后续按差额登记充值采购消耗。 */}
          <div className='overflow-x-auto'>
            <table className='w-full text-sm'>
              {/* 表格只展示事实字段，不在浏览器重新聚合成本。 */}
              <thead><tr className='border-b text-left'><th className='py-2'>{t('Title')}</th><th>{t('Type')}</th><th>{t('Amount')}</th><th>{t('Remaining balance')}</th>{props.canWrite && <th>{t('Actions')}</th>}</tr></thead>
              {/* 充值行提供余额输入，订阅和其他支出没有可消耗余额。 */}
              <tbody>{query.data.items.map((item) => <tr key={item.id} className='border-b'><td className='py-2'>{item.title}</td><td>{t(expenseKindLabel(item.kind))}</td><td>{formatCNY(item.amount_cents, locale)}</td><td>{item.kind === 'recharge' ? formatCNY(item.remaining_balance_cents, locale) : '—'}</td>{props.canWrite && <td>{item.kind === 'recharge' && <div className='flex items-center gap-2'><Input className='w-28' type='number' min='0' step='0.01' aria-label={`${t('Remaining balance')} ${item.title}`} value={consumeValues[item.id] ?? ''} onChange={(event) => setConsumeValues((current) => ({ ...current, [item.id]: event.target.value }))} placeholder={t('Remaining balance (CNY)')} /><Button type='button' variant='outline' disabled={consumeMutation.isPending || consumeValues[item.id] === undefined || consumeValues[item.id] === ''} onClick={() => consumeMutation.mutate({ id: item.id, remaining: Math.round(Number(consumeValues[item.id]) * 100) })}>{t('Update')}</Button></div>}</td>}</tr>)}</tbody>
            </table>
          </div>
          {/* 写权限只影响表单和余额动作，读取仍由后端财务读取权限保护。 */}
          {props.canWrite && (
            <form className='mt-5 grid gap-3 sm:grid-cols-2' onSubmit={(event) => { event.preventDefault(); mutation.mutate() }}>
              {/* 标题和金额是每笔支出的最小可追溯信息。 */}
              <Input aria-label={t('Title')} value={title} onChange={(event) => setTitle(event.target.value)} placeholder={t('Expense title')} required />
              <Select value={kind} onValueChange={(value) => setKind(value as FinanceExpense['kind'])}><SelectTrigger aria-label={t('Type')} className='w-full'><SelectValue /></SelectTrigger><SelectContent><SelectItem value='subscription'>{t('Subscription')}</SelectItem><SelectItem value='recharge'>{t('Account top-up')}</SelectItem><SelectItem value='other'>{t('Other')}</SelectItem></SelectContent></Select>
              <Input aria-label={t('Amount')} type='number' min='0.01' step='0.01' value={amount} onChange={(event) => setAmount(event.target.value)} placeholder={t('Amount (CNY)')} required />
              <Input aria-label={t('Occurred date')} type='date' value={occurred} onChange={(event) => setOccurred(event.target.value)} required />
              {/* 只有订阅需要覆盖期；成本会按完整覆盖日摊销。 */}
              {kind === 'subscription' && <><Input aria-label={t('Coverage start')} type='date' value={coverageStart} onChange={(event) => setCoverageStart(event.target.value)} required /><Input aria-label={t('Coverage end')} type='date' value={coverageEnd} onChange={(event) => setCoverageEnd(event.target.value)} required /></>}
              {/* 新建充值采购时可登记供应商侧仍未消耗的余额。 */}
              {kind === 'recharge' && <Input aria-label={t('Remaining balance')} type='number' min='0' step='0.01' value={remaining} onChange={(event) => setRemaining(event.target.value)} placeholder={t('Remaining balance (CNY)')} />}
              <Input className='sm:col-span-2' aria-label={t('Note')} value={note} onChange={(event) => setNote(event.target.value)} placeholder={t('Note')} />
              {/* 服务端失败时保留草稿，便于修正后重新提交。 */}
              <Button type='submit' disabled={mutation.isPending}>{t('Save expense')}</Button>
            </form>
          )}
        </>
      )}
    </PanelWrapper>
  )
}
