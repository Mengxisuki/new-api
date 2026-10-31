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
import i18next from 'i18next'
import { useState, useCallback, useRef, useEffect } from 'react'
import { toast } from 'sonner'

import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'
import { useSystemConfigStore } from '@/stores/system-config-store'

import {
  calculateAmount,
  calculateStripeAmount,
  calculateWaffoAmount,
  calculateWaffoPancakeAmount,
  requestPayment,
  requestStripePayment,
  isApiSuccess,
} from '../api'
import {
  isLocalTopupPayment,
  isStripePayment,
  isWaffoPayment,
  isWaffoPancakePayment,
  submitPaymentForm,
} from '../lib'
import type { AmountRequest, AmountResponse, PaymentQuote } from '../types'

// ============================================================================
// Payment Hook
// ============================================================================

type AmountCalculator = (request: AmountRequest) => Promise<AmountResponse>

export interface PaymentAmountCalculators {
  regular: AmountCalculator
  stripe: AmountCalculator
  waffo: AmountCalculator
  waffoPancake: AmountCalculator
}

const defaultPaymentAmountCalculators: PaymentAmountCalculators = {
  regular: calculateAmount,
  stripe: calculateStripeAmount,
  waffo: calculateWaffoAmount,
  waffoPancake: calculateWaffoPancakeAmount,
}

export async function requestPaymentQuote(
  topupAmount: number,
  paymentType: string,
  calculators: PaymentAmountCalculators = defaultPaymentAmountCalculators
): Promise<PaymentQuote> {
  let calculator = calculators.regular
  if (isStripePayment(paymentType)) {
    calculator = calculators.stripe
  } else if (isWaffoPayment(paymentType)) {
    calculator = calculators.waffo
  } else if (isWaffoPancakePayment(paymentType)) {
    calculator = calculators.waffoPancake
  }

  const response = await calculator({ amount: topupAmount })
  // 错误响应必须显式失败，不能将失败金额伪装成零元报价。
  if (!isApiSuccess(response) || !response.data) {
    throw createServerError(response, i18next.t('Payment request failed'))
  }
  const amount = Number(response.data)
  if (!Number.isFinite(amount) || amount <= 0) {
    throw new Error(i18next.t('Payment details unavailable. Please retry.'))
  }
  // 入账字段缺失可用于尚无冻结报价的国际渠道，人民币本地渠道随后严格要求。
  const creditedQuota = response.credited_quota ?? null
  if (
    creditedQuota !== null &&
    (!Number.isSafeInteger(creditedQuota) || creditedQuota <= 0)
  ) {
    throw new Error(i18next.t('Payment details unavailable. Please retry.'))
  }
  return { amount, creditedQuota, inputAmount: topupAmount, paymentType }
}

export function usePayment() {
  const [quote, setQuote] = useState<PaymentQuote | null>(null)
  const requestId = useRef(0) // 只有最新金额与渠道的报价可以更新界面。
  const currency = useSystemConfigStore((state) => state.config.currency)
  const lastRequest = useRef<{ amount: number; paymentType: string } | null>(
    null
  )
  const [calculating, setCalculating] = useState(false)
  const [processing, setProcessing] = useState(false)
  const [qrPayment, setQrPayment] = useState<{
    qrCode: string
    tradeNo?: string
  } | null>(null)

  // 报价中和失败时清空旧结果，不把上次金额留作可确认的付款资料。
  // 请求序号只保护浏览器展示顺序，最终金额仍由下单端点重新校验。
  const calculatePaymentAmount = useCallback(
    async (topupAmount: number, paymentType: string) => {
      const currentRequest = ++requestId.current
      lastRequest.current = { amount: topupAmount, paymentType }
      setQuote(null)
      if (topupAmount <= 0) {
        setCalculating(false) // 用户清空输入时，已失效旧请求的 finally 不会再释放加载状态。
        return null
      }
      setCalculating(true)
      try {
        const next = await requestPaymentQuote(topupAmount, paymentType)
        if (
          currency.quotaDisplayType === 'CNY' &&
          isLocalTopupPayment(paymentType) &&
          next.creditedQuota === null
        ) {
          throw new Error(
            i18next.t('Payment details unavailable. Please retry.')
          )
        }
        // 后返回的旧请求既不能覆盖当前报价，也不能打开其付款确认框。
        if (currentRequest !== requestId.current) return null
        setQuote(next)
        return next.amount
      } catch (error) {
        if (currentRequest === requestId.current) {
          setQuote(null)
          handleServerError(error, i18next.t('Payment request failed'))
        }
        return null
      } finally {
        if (currentRequest === requestId.current) setCalculating(false)
      }
    },
    [currency.quotaDisplayType]
  )

  // 币种或汇率保存后重新向服务器报价，不在浏览器按新汇率改旧冻结额度。
  useEffect(() => {
    requestId.current += 1
    setQuote(null)
    const last = lastRequest.current
    if (last) void calculatePaymentAmount(last.amount, last.paymentType)
  }, [
    currency.quotaDisplayType,
    currency.usdExchangeRate,
    currency.customCurrencyExchangeRate,
    calculatePaymentAmount,
  ])

  // Process payment
  const processPayment = useCallback(
    async (topupAmount: number, paymentType: string) => {
      try {
        setProcessing(true)

        const isStripe = isStripePayment(paymentType)
        const amount = Math.floor(topupAmount)

        const response = isStripe
          ? await requestStripePayment({
              amount,
              payment_method: 'stripe',
            })
          : await requestPayment({
              amount,
              payment_method: paymentType,
            })

        if (!isApiSuccess(response)) {
          handleServerError(response, i18next.t('Payment request failed'))
          return false
        }

        // Handle Stripe payment
        if (isStripe && response.data?.pay_link) {
          window.open(response.data.pay_link as string, '_blank')
          toast.success(i18next.t('Redirecting to payment page...'))
          return true
        }

        // Handle non-Stripe payment
        if (!isStripe && response.data) {
          const paymentData = response.data as Record<string, unknown>
          const redirectUrl = paymentData.redirect_url
          if (typeof redirectUrl === 'string' && redirectUrl) {
            toast.success(i18next.t('Redirecting to payment page...'))
            window.location.assign(redirectUrl)
            return true
          }

          const qrCode = paymentData.qr_code
          if (typeof qrCode === 'string' && qrCode) {
            setQrPayment({
              qrCode,
              tradeNo:
                typeof paymentData.trade_no === 'string'
                  ? paymentData.trade_no
                  : undefined,
            })
            return true
          }

          const url = (response as unknown as { url?: string }).url
          if (url) {
            submitPaymentForm(url, response.data)
            toast.success(i18next.t('Redirecting to payment page...'))
            return true
          }
        }

        return false
      } catch (error) {
        handleServerError(error, i18next.t('Payment request failed'))
        return false
      } finally {
        setProcessing(false)
      }
    },
    []
  )

  return {
    amount: quote?.amount ?? 0,
    creditedQuota: quote?.creditedQuota ?? null,
    quote,
    calculating,
    processing,
    qrPayment,
    calculatePaymentAmount,
    processPayment,
    clearQrPayment: () => setQrPayment(null),
  }
}
