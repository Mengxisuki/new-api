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
import { Crown, CalendarClock, Package } from 'lucide-react'
import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { GroupBadge } from '@/components/group-badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Separator } from '@/components/ui/separator'
import { OfficialPaymentQrDialog } from '@/features/wallet/components/dialogs/official-payment-qr-dialog'
import { submitPaymentForm } from '@/features/wallet/lib/payment'
import type {
  OfficialPaymentData,
  PaymentMethod as WalletPaymentMethod,
} from '@/features/wallet/types'
import { formatQuota } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  paySubscriptionStripe,
  paySubscriptionCreem,
  paySubscriptionEpay,
  paySubscriptionWaffoPancake,
  paySubscriptionOfficial,
} from '../../api'
import { formatDuration, formatResetPeriod } from '../../lib'
import type { PlanRecord } from '../../types'

interface PaymentMethod {
  type: string
  name?: string
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  plan: PlanRecord | null
  enableStripe?: boolean
  enableCreem?: boolean
  enableWaffoPancake?: boolean
  enableOnlineTopUp?: boolean
  epayMethods?: PaymentMethod[]
  purchaseLimit?: number
  purchaseCount?: number
  officialMethods?: WalletPaymentMethod[]
  onPurchaseSuccess?: () => void | Promise<void>
}

export function SubscriptionPurchaseDialog(props: Props) {
  const { t } = useTranslation()
  const [paying, setPaying] = useState(false)
  const [officialPayment, setOfficialPayment] =
    useState<OfficialPaymentData | null>(null)
  const [selectedEpayMethod, setSelectedEpayMethod] = useState('')

  useEffect(() => {
    if (props.open && props.epayMethods && props.epayMethods.length > 0) {
      setSelectedEpayMethod(props.epayMethods[0].type)
    } else if (!props.open) {
      setSelectedEpayMethod('')
    }
  }, [props.open, props.epayMethods])

  const plan = props.plan?.plan
  if (!plan) return null

  const hasStripe = props.enableStripe && !!plan.stripe_price_id
  const hasCreem = props.enableCreem && !!plan.creem_product_id
  const hasWaffoPancake =
    props.enableWaffoPancake && !!plan.waffo_pancake_product_id
  const hasEpay =
    props.enableOnlineTopUp && (props.epayMethods || []).length > 0
  const hasOfficial = (props.officialMethods?.length ?? 0) > 0
  const hasAnyPayment =
    hasStripe || hasCreem || hasWaffoPancake || hasEpay || hasOfficial
  const totalAmount = Number(plan.total_amount || 0)
  const price = Number(plan.price_amount || 0).toFixed(2)
  const limitReached =
    (props.purchaseLimit || 0) > 0 &&
    (props.purchaseCount || 0) >= (props.purchaseLimit || 0)

  const handlePayStripe = async () => {
    setPaying(true)
    try {
      const res = await paySubscriptionStripe({ plan_id: plan.id })
      if (res.message === 'success' && res.data?.pay_link) {
        window.open(res.data.pay_link, '_blank')
        toast.success(t('Payment page opened'))
        props.onOpenChange(false)
      } else {
        handleServerError(res, t('Payment request failed'))
      }
    } catch (error) {
      handleServerError(error, t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  const handlePayCreem = async () => {
    setPaying(true)
    try {
      const res = await paySubscriptionCreem({ plan_id: plan.id })
      if (res.message === 'success' && res.data?.checkout_url) {
        window.open(res.data.checkout_url, '_blank')
        toast.success(t('Payment page opened'))
        props.onOpenChange(false)
      } else {
        handleServerError(res, t('Payment request failed'))
      }
    } catch (error) {
      handleServerError(error, t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  // In-tab redirect (not window.open) — user-gesture context is lost
  // across the await, so a popup would be blocked. Same as the wallet hook.
  const handlePayWaffoPancake = async () => {
    setPaying(true)
    try {
      const res = await paySubscriptionWaffoPancake({ plan_id: plan.id })
      if (res.message === 'success' && res.data?.checkout_url) {
        toast.success(t('Redirecting to payment page...'))
        window.location.href = res.data.checkout_url
      } else {
        handleServerError(res, t('Payment request failed'))
      }
    } catch (error) {
      handleServerError(error, t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  const handlePayEpay = async () => {
    if (!selectedEpayMethod) {
      toast.error(t('Please select a payment method'))
      return
    }
    setPaying(true)
    try {
      const res = await paySubscriptionEpay({
        plan_id: plan.id,
        payment_method: selectedEpayMethod,
      })
      if (res.message === 'success' && res.url) {
        // 沿用钱包现有表单提交，Safari 行为和隐藏字段清理由公共函数负责。
        submitPaymentForm(res.url, res.data || {})
        toast.success(t('Payment initiated'))
        props.onOpenChange(false)
      } else {
        handleServerError(res, t('Payment request failed'))
      }
    } catch (error) {
      handleServerError(error, t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  // 直接向所选官方渠道支付订阅价格；不消费已有钱包余额。
  // 微信 Native 返回二维码，H5/支付宝页面返回跳转地址。
  // 仅服务端支付通知可以开通/续期，打开付款页不触发本地成功回调。
  const handlePayOfficial = async (paymentMethod: string) => {
    setPaying(true)
    try {
      // 只发送套餐和渠道标识，客户端不能指定应付金额或续期时长。
      const res = await paySubscriptionOfficial({
        plan_id: plan.id, // 与当前购买卡片一致，不允许替换为充值数量。
        payment_method: paymentMethod, // 后端验证此官方渠道启用且支持付款。
      })
      // 保存二维码与订单号即可，避免把其他网关的响应字段带入展示状态。
      if (res.success && res.data?.qr_code) {
        setOfficialPayment({
          qr_code: res.data.qr_code,
          trade_no: res.data.trade_no,
        })
      } else if (res.success && res.data?.redirect_url) {
        // H5 在当前窗口跳转，避免 await 后新窗口被浏览器阻止。
        window.location.assign(res.data.redirect_url)
      } else {
        handleServerError(res, t('Payment request failed'))
      }
    } catch (error) {
      // 失败必须可见，不能退回余额购买或把请求当作已付款。
      handleServerError(error, t('Payment request failed'))
    } finally {
      setPaying(false) // 异常和取消都释放按钮状态，允许用户重新选择。
    }
  }

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={props.onOpenChange}
        title={
          <>
            <Crown className='h-5 w-5' />
            {t('Purchase Subscription')}
          </>
        }
        contentClassName='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-md'
        titleClassName='flex items-center gap-2'
        contentHeight='auto'
        bodyClassName='space-y-4'
      >
        <div className='space-y-3 sm:space-y-4'>
          <div className='bg-muted/50 space-y-2.5 rounded-lg border p-3 sm:space-y-3 sm:p-4'>
            <div className='flex justify-between'>
              <span className='text-muted-foreground text-sm'>
                {t('Plan Name')}
              </span>
              <span className='max-w-[200px] truncate text-sm font-medium'>
                {plan.title}
              </span>
            </div>
            <div className='flex items-center justify-between'>
              <span className='text-muted-foreground text-sm'>
                {t('Validity Period')}
              </span>
              <span className='flex items-center gap-1 text-sm'>
                <CalendarClock className='h-3.5 w-3.5' />
                {formatDuration(plan, t)}
              </span>
            </div>
            {formatResetPeriod(plan, t) !== t('No Reset') && (
              <div className='flex justify-between'>
                <span className='text-muted-foreground text-sm'>
                  {t('Reset Period')}
                </span>
                <span className='text-sm'>{formatResetPeriod(plan, t)}</span>
              </div>
            )}
            <div className='flex items-center justify-between'>
              <span className='text-muted-foreground text-sm'>
                {t('Plan Quota')}
              </span>
              <span className='flex items-center gap-1 text-sm'>
                <Package className='h-3.5 w-3.5' />
                {totalAmount > 0 ? formatQuota(totalAmount) : t('Unlimited')}
              </span>
            </div>
            {plan.upgrade_group && (
              <div className='flex items-center justify-between'>
                <span className='text-muted-foreground text-sm'>
                  {t('Upgrade Group')}
                </span>
                <GroupBadge group={plan.upgrade_group} />
              </div>
            )}
            <Separator />
            <div className='flex items-center justify-between'>
              <span className='text-sm font-medium'>{t('Amount Due')}</span>
              <span className='text-primary text-lg font-bold'>${price}</span>
            </div>
          </div>

          {limitReached && (
            <Alert variant='destructive'>
              <AlertDescription>
                {t('Purchase limit reached')} ({props.purchaseCount}/
                {props.purchaseLimit})
              </AlertDescription>
            </Alert>
          )}

          {hasAnyPayment && (
            <div className='space-y-3'>
              <p className='text-muted-foreground text-xs'>
                {t('Select payment method')}
              </p>
              {(hasStripe || hasCreem || hasWaffoPancake) && (
                <div className='grid grid-cols-2 gap-2 sm:flex'>
                  {hasStripe && (
                    <Button
                      variant='outline'
                      className='flex-1'
                      onClick={handlePayStripe}
                      disabled={paying || limitReached}
                    >
                      Stripe
                    </Button>
                  )}
                  {hasCreem && (
                    <Button
                      variant='outline'
                      className='flex-1'
                      onClick={handlePayCreem}
                      disabled={paying || limitReached}
                    >
                      Creem
                    </Button>
                  )}
                  {hasWaffoPancake && (
                    <Button
                      variant='outline'
                      className='flex-1'
                      onClick={handlePayWaffoPancake}
                      disabled={paying || limitReached}
                    >
                      Waffo Pancake
                    </Button>
                  )}
                </div>
              )}
              {/* 官方支付使用真实渠道名称，余额充足也不出现钱包购买按钮。 */}
              {hasOfficial && (
                <div className='flex flex-wrap gap-2'>
                  {props.officialMethods?.map((method) => (
                    <Button
                      key={method.type}
                      variant='outline'
                      disabled={paying || limitReached}
                      onClick={() => handlePayOfficial(method.type)}
                    >
                      {method.name || method.type}
                    </Button>
                  ))}
                </div>
              )}
              {hasEpay && (
                <div className='grid grid-cols-[minmax(0,1fr)_auto] gap-2'>
                  <Combobox
                    options={(props.epayMethods || []).map((m) => ({
                      value: m.type,
                      label: m.name || m.type,
                    }))}
                    value={selectedEpayMethod}
                    onValueChange={(v) =>
                      v !== null && setSelectedEpayMethod(v)
                    }
                    disabled={limitReached}
                    className='flex-1'
                  />
                  <Button
                    onClick={handlePayEpay}
                    disabled={paying || !selectedEpayMethod || limitReached}
                  >
                    {t('Pay')}
                  </Button>
                </div>
              )}
            </div>
          )}
        </div>
      </Dialog>
      {/* 关闭二维码后只刷新订阅数据，是否生效以服务端查询结果为准。 */}
      <OfficialPaymentQrDialog
        open={!!officialPayment}
        onOpenChange={(open) => {
          if (!open) setOfficialPayment(null)
        }}
        onRefresh={async () => {
          setOfficialPayment(null)
          await props.onPurchaseSuccess?.()
          props.onOpenChange(false)
        }}
        refreshLabel={t('Payment completed, refresh subscription')}
        qrCode={officialPayment?.qr_code ?? ''}
        tradeNo={officialPayment?.trade_no}
      />
    </>
  )
}
