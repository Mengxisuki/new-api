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
*/
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'

import { adjustUserQuota } from '../api'
import { ADJUSTMENT_REASON_LABELS } from '../lib/adjustment-reasons'
import type { QuotaAdjustMode, QuotaAdjustReason } from '../types'

interface UserQuotaDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  userId: number
  currentQuota: number
  onSuccess: () => void
}

const CREDIT_REASONS: QuotaAdjustReason[] = [
  'compensation',
  'grant',
  'on_account',
  'offline_cash',
]
const REVOCATION_REASONS: QuotaAdjustReason[] = [
  'revoke_grant',
  'revoke_compensation',
  'correction',
]
const MODE_LABELS: Record<QuotaAdjustMode, string> = {
  add: 'Add',
  subtract: 'Subtract',
  override: 'Override',
}

// 人民币输入只接受最多两位小数，并在提交边界转换为整数分。
function parseCents(value: string): number {
  if (!/^\d+(\.\d{1,2})?$/.test(value.trim())) return 0
  const cents = Math.round(Number(value) * 100)
  return Number.isSafeInteger(cents) ? cents : 0
}

export function UserQuotaDialog(props: UserQuotaDialogProps) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<QuotaAdjustMode>('add')
  const [reason, setReason] = useState<QuotaAdjustReason>('grant')
  const [amount, setAmount] = useState('')
  const [note, setNote] = useState('')
  const [cashAmount, setCashAmount] = useState('')
  const [cashRef, setCashRef] = useState('')
  const [receivableAmount, setReceivableAmount] = useState('')
  const [originalId, setOriginalId] = useState('')
  const [loading, setLoading] = useState(false)
  const requestId = useRef<string | null>(null)
  const requestFingerprint = useRef<string | null>(null)

  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'
  const amountValue = Number.parseFloat(amount) || 0
  const quotaValue = parseQuotaFromDollars(Math.abs(amountValue))
  const overrideValue = parseQuotaFromDollars(amountValue)
  const reasonOptions = useMemo(() => {
    if (mode === 'subtract') return REVOCATION_REASONS
    if (mode === 'override' && overrideValue < props.currentQuota) {
      return REVOCATION_REASONS
    }
    return CREDIT_REASONS
  }, [mode, overrideValue, props.currentQuota])
  const isCash = reason === 'offline_cash'
  const isOnAccount = reason === 'on_account'
  const amountRequired = mode !== 'override' || amount.length > 0
  const validAmount = mode === 'override' ? amount.length > 0 : quotaValue > 0
  const validNote = note.trim().length > 0
  const validCash = !isCash || (parseCents(cashAmount) > 0 && cashRef.trim())
  const validReceivable = !isOnAccount || parseCents(receivableAmount) > 0
  const parsedOriginalId = Number(originalId)
  const validOriginal =
    !originalId.trim() ||
    (Number.isInteger(parsedOriginalId) && parsedOriginalId > 0)
  const canSubmit =
    amountRequired &&
    validAmount &&
    validNote &&
    validCash &&
    validReceivable &&
    validOriginal

  // 操作类型或向上/向下覆盖变化时，不能保留另一类原因造成后端拒绝。
  useEffect(() => {
    if (!reasonOptions.includes(reason)) setReason(reasonOptions[0])
  }, [reason, reasonOptions])

  // 每次关闭都清理草稿；同一弹窗内失败重试则保留 request_id 和输入。
  useEffect(() => {
    if (props.open) return
    setAmount('')
    setNote('')
    setCashAmount('')
    setCashRef('')
    setReceivableAmount('')
    setOriginalId('')
    setMode('add')
    setReason('grant')
    requestId.current = null
    requestFingerprint.current = null
  }, [props.open])

  const preview = () => {
    const current = props.currentQuota
    if (mode === 'override') {
      return `${t('Current quota')}: ${formatQuota(current)} → ${formatQuota(overrideValue)}`
    }
    const next = mode === 'add' ? current + quotaValue : current - quotaValue
    return `${t('Current quota')}: ${formatQuota(current)} ${mode === 'add' ? '+' : '-'}${formatQuota(quotaValue)} = ${formatQuota(next)}`
  }

  const handleConfirm = async () => {
    if (loading || !canSubmit) return
    const basePayload = {
      id: props.userId,
      action: 'add_quota' as const,
      mode,
      value: mode === 'override' ? overrideValue : quotaValue,
      reason,
      note: note.trim(),
      cash_cents: isCash ? parseCents(cashAmount) : 0,
      receivable_cents: isOnAccount ? parseCents(receivableAmount) : 0,
      cash_ref: isCash ? cashRef.trim() : '',
      ...(originalId.trim() ? { original_id: Number(originalId) } : {}),
    }
    const fingerprint = JSON.stringify(basePayload)
    if (requestFingerprint.current !== fingerprint) {
      requestFingerprint.current = fingerprint
      requestId.current = crypto.randomUUID()
    }

    setLoading(true)
    try {
      const result = await adjustUserQuota({
        ...basePayload,
        request_id: requestId.current ?? crypto.randomUUID(),
      })
      if (result.success) {
        toast.success(t('Quota adjusted successfully'))
        props.onOpenChange(false)
        props.onSuccess()
      } else {
        handleServerError(result, t('Failed to adjust quota'))
      }
    } catch (error: unknown) {
      handleServerError(error, t('Failed to adjust quota'))
    } finally {
      setLoading(false)
    }
  }

  const handleModeChange = (nextMode: QuotaAdjustMode) => {
    setMode(nextMode)
    setAmount('')
    setReason(nextMode === 'add' ? 'grant' : REVOCATION_REASONS[0])
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Adjust Quota')}
      description={t('Record a classified quota adjustment with an explanation')}
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)} disabled={loading}>
            {t('Cancel')}
          </Button>
          <Button onClick={() => void handleConfirm()} disabled={loading || !canSubmit}>
            {loading ? t('Processing...') : t('Confirm')}
          </Button>
        </>
      }
    >
      <FieldGroup>
        <p className='text-muted-foreground text-sm'>{preview()}</p>
        <Field>
          <FieldLabel>{t('Mode')}</FieldLabel>
          <div className='flex flex-wrap gap-1'>
            {(['add', 'subtract', 'override'] as const).map((value) => (
              <Button
                key={value}
                type='button'
                variant='outline'
                size='sm'
                className={cn(mode === value && 'bg-primary text-primary-foreground')}
                onClick={() => handleModeChange(value)}
              >
                {t(MODE_LABELS[value])}
              </Button>
            ))}
          </div>
        </Field>
        <Field>
          <FieldLabel htmlFor='quota-adjust-reason'>{t('Adjustment category')}</FieldLabel>
          <NativeSelect
            id='quota-adjust-reason'
            value={reason}
            onChange={(event) => setReason(event.target.value as QuotaAdjustReason)}
          >
            {reasonOptions.map((value) => (
              <NativeSelectOption key={value} value={value}>
                {t(ADJUSTMENT_REASON_LABELS[value])}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Field>
          <FieldLabel htmlFor='quota-adjust-amount'>
            {t('Amount')} ({tokensOnly ? t('tokens') : currencyLabel})
          </FieldLabel>
          <Input
            id='quota-adjust-amount'
            type='number'
            step={tokensOnly ? 1 : 0.000001}
            min={mode === 'override' ? undefined : 0}
            placeholder={t('Enter amount in {{currency}}', { currency: currencyLabel })}
            value={amount}
            onChange={(event) => setAmount(event.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor='quota-adjust-note'>{t('Note')} *</FieldLabel>
          <Input
            id='quota-adjust-note'
            value={note}
            maxLength={500}
            onChange={(event) => setNote(event.target.value)}
            placeholder={t('Explain this adjustment')}
          />
        </Field>
        {isCash && (
          <div className='grid gap-3 sm:grid-cols-2'>
            <Field>
              <FieldLabel htmlFor='quota-adjust-cash'>{t('Cash received (CNY)')} *</FieldLabel>
              <Input
                id='quota-adjust-cash'
                inputMode='decimal'
                value={cashAmount}
                onChange={(event) => setCashAmount(event.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor='quota-adjust-cash-ref'>{t('Cash reference')} *</FieldLabel>
              <Input
                id='quota-adjust-cash-ref'
                value={cashRef}
                maxLength={128}
                onChange={(event) => setCashRef(event.target.value)}
              />
            </Field>
          </div>
        )}
        {isOnAccount && (
          <Field>
            <FieldLabel htmlFor='quota-adjust-receivable'>{t('Receivable (CNY)')} *</FieldLabel>
            <Input
              id='quota-adjust-receivable'
              inputMode='decimal'
              value={receivableAmount}
              onChange={(event) => setReceivableAmount(event.target.value)}
            />
          </Field>
        )}
        <Field>
          <FieldLabel htmlFor='quota-adjust-original'>{t('Original entry ID')}</FieldLabel>
          <Input
            id='quota-adjust-original'
            inputMode='numeric'
            value={originalId}
            onChange={(event) => setOriginalId(event.target.value)}
            placeholder={t('Optional')}
          />
        </Field>
        {mode !== 'add' && (
          <p className='text-muted-foreground text-xs'>
            {t('Revoke or correct quota only; this does not refund cash or cancel debt.')}
          </p>
        )}
      </FieldGroup>
    </Dialog>
  )
}
