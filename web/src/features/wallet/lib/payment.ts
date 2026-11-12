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
import { getServerMessage } from '@/lib/server-error-message'

import {
  PAYMENT_TYPES,
  DEFAULT_PRESET_MULTIPLIERS,
  DEFAULT_PAYMENT_TYPE,
  DEFAULT_MIN_TOPUP,
} from '../constants'
import type { PaymentMethod, PresetAmount, TopupInfo } from '../types'

// ============================================================================
// Payment Processing Functions
// ============================================================================

/**
 * Payment endpoints answer errors as {"message": "error", "data": text}. When
 * the response also carries a message_key, this is its translation; callers
 * fall back to the text they already read.
 */
export function getPaymentErrorTranslation(
  response: unknown
): string | undefined {
  if (!response || typeof response !== 'object') return undefined
  const body = response as Record<string, unknown>
  if (typeof body.message_key !== 'string' || !body.message_key) {
    return undefined
  }
  return getServerMessage(body)
}

/**
 * Check if browser is Safari
 */
function isSafariBrowser(): boolean {
  return (
    navigator.userAgent.includes('Safari') &&
    !navigator.userAgent.includes('Chrome')
  )
}

/**
 * Submit payment form (for non-Stripe payments)
 */
export function submitPaymentForm(
  url: string,
  params: Record<string, unknown>
): void {
  const form = document.createElement('form')
  form.action = url
  form.method = 'POST'

  // Don't open in new tab for Safari
  if (!isSafariBrowser()) {
    form.target = '_blank'
  }

  // Add form parameters
  Object.entries(params).forEach(([key, value]) => {
    const input = document.createElement('input')
    input.type = 'hidden'
    input.name = key
    input.value = String(value)
    form.appendChild(input)
  })

  document.body.appendChild(form)
  form.submit()
  document.body.removeChild(form)
}

/**
 * Check if payment method is Stripe
 */
export function isStripePayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.STRIPE
}

/**
 * Check if payment method is Waffo
 */
export function isWaffoPayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.WAFFO
}

/**
 * Check if payment method is Waffo Pancake
 *
 * Pancake is a metered-style payment that goes through a dedicated checkout
 * URL flow rather than the generic epay form submission, so it must be
 * special-cased in payment dispatch logic.
 */
export function isWaffoPancakePayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.WAFFO_PANCAKE
}

// 普通人民币渠道和国际美元渠道具有不同输入合同。
// 已有国际渠道继续使用其专属报价和结算，不能套用一元充值一元公式。
export function isLocalTopupPayment(paymentType: string): boolean {
  return (
    !isStripePayment(paymentType) &&
    !isWaffoPayment(paymentType) &&
    !isWaffoPancakePayment(paymentType) &&
    paymentType !== PAYMENT_TYPES.CREEM
  )
}

export interface PaymentProcessors {
  regular: (topupAmount: number, paymentType: string) => Promise<boolean>
  waffo: (topupAmount: number, payMethodIndex: number) => Promise<boolean>
  waffoPancake: (topupAmount: number) => Promise<boolean>
  credit?: (topupAmount: number) => Promise<boolean>
}

export async function dispatchSelectedPayment(
  paymentMethod: PaymentMethod,
  topupAmount: number,
  waffoMethodIndex: number | null,
  processors: PaymentProcessors
): Promise<boolean> {
  if (isWaffoPayment(paymentMethod.type)) {
    if (waffoMethodIndex === null) {
      return false
    }
    return processors.waffo(topupAmount, waffoMethodIndex)
  }

  if (isWaffoPancakePayment(paymentMethod.type)) {
    return processors.waffoPancake(topupAmount)
  }

  if (paymentMethod.type === PAYMENT_TYPES.CREDIT) {
    return processors.credit ? processors.credit(topupAmount) : false
  }

  return processors.regular(topupAmount, paymentMethod.type)
}

/**
 * Get default payment type from topup info
 */
export function getDefaultPaymentType(topupInfo: TopupInfo | null): string {
  if (!topupInfo) {
    return DEFAULT_PAYMENT_TYPE
  }

  // Return first available payment method or default
  if (topupInfo.pay_methods?.length > 0) {
    return topupInfo.pay_methods[0].type
  }

  if (topupInfo.enable_stripe_topup) {
    return PAYMENT_TYPES.STRIPE
  }

  if (topupInfo.enable_waffo_topup) {
    return PAYMENT_TYPES.WAFFO
  }

  if (topupInfo.enable_waffo_pancake_topup) {
    return PAYMENT_TYPES.WAFFO_PANCAKE
  }

  return DEFAULT_PAYMENT_TYPE
}

/**
 * Get minimum topup amount from topup info
 */
export function getMinTopupAmount(topupInfo: TopupInfo | null): number {
  if (!topupInfo) {
    return DEFAULT_MIN_TOPUP
  }

  if (topupInfo.enable_online_topup) {
    return topupInfo.min_topup
  }

  if (topupInfo.enable_stripe_topup) {
    return topupInfo.stripe_min_topup
  }

  if (topupInfo.enable_waffo_topup) {
    return topupInfo.waffo_min_topup || DEFAULT_MIN_TOPUP
  }

  if (topupInfo.enable_waffo_pancake_topup) {
    return topupInfo.waffo_pancake_min_topup || DEFAULT_MIN_TOPUP
  }

  return DEFAULT_MIN_TOPUP
}

/**
 * Generate preset amounts based on minimum topup
 */
export function generatePresetAmounts(minAmount: number): PresetAmount[] {
  return DEFAULT_PRESET_MULTIPLIERS.map((multiplier) => ({
    value: minAmount * multiplier,
  }))
}

/**
 * 生成充值页预设套餐列表（PresetAmount[]）。
 * 输入约束：amountOptions 为候选充值金额数组，discounts 为充值面额到折扣率的映射（如 { 100: 0.98, 30000: 0.9 }）。
 * 关键流程：先提取 discounts 中正数面额与正数折扣率并按金额升序排列；若存在有效折扣配置则直接作为充值 Plan 返回。
 * 设计取舍：管理员配置金额折扣后，用户端应展示全部已配置折扣套餐（如 30000），不再混入默认的 10/20/50/100 档位。
 * 回退规则：仅在未配置任何有效金额折扣时，才使用 amountOptions 生成无折扣预设。
 */
export function mergePresetAmounts(
  amountOptions: number[],
  discounts: Record<number, number>
): PresetAmount[] {
  // 从折扣映射中提取合法的正数面额与正数折扣，过滤掉 0、负数或 NaN 等异常项。
  const discountPresets = Object.entries(discounts || {})
    .map(([amount, discount]) => ({
      value: Number(amount),
      discount: Number(discount),
    }))
    .filter(
      (item) =>
        Number.isFinite(item.value) &&
        item.value > 0 &&
        Number.isFinite(item.discount) &&
        item.discount > 0
    )
    // 按充值面额从小到大排序，确保大额套餐（如 30000）排在小额套餐（如 100）之后稳定展示。
    .sort((a, b) => a.value - b.value)

  // 只要配置了有效的充值折扣档位，就直接使用折扣档位作为充值页套餐。
  if (discountPresets.length > 0) {
    return discountPresets
  }

  // 未配置任何折扣且无自定义金额选项时返回空数组，由上层按最低充值额生成默认档位。
  if (!amountOptions || amountOptions.length === 0) {
    return []
  }

  // 无折扣配置时按原充值金额选项顺序生成原价套餐。
  return amountOptions.map((amount) => ({
    value: amount,
    discount: 1.0,
  }))
}
