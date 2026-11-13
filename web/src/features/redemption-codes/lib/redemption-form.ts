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
import type { TFunction } from 'i18next'
import { z } from 'zod'

import { parseQuotaFromDollars, quotaUnitsToEditableAmount } from '@/lib/format'

import {
  REDEMPTION_VALIDATION,
  getRedemptionFormErrorMessages,
} from '../constants'
import type { RedemptionFormData, Redemption } from '../types'

const PRICE_CENTS_MAX = 10_000_000_000

// ============================================================================
// Form Schema (use getRedemptionFormSchema(t) in components for i18n messages)
// ============================================================================

export function parsePriceCents(value: string): number | null {
  const normalized = value.trim()
  if (!/^\d+(?:\.\d{1,2})?$/.test(normalized)) return null
  const [yuan, fraction = ''] = normalized.split('.')
  const cents = Number(yuan) * 100 + Number(fraction.padEnd(2, '0'))
  if (!Number.isSafeInteger(cents) || cents > PRICE_CENTS_MAX) return null
  return cents
}

export function getRedemptionFormSchema(t: TFunction) {
  const msg = getRedemptionFormErrorMessages(t)
  return z
    .object({
      name: z
        .string()
        .min(REDEMPTION_VALIDATION.NAME_MIN_LENGTH, msg.NAME_LENGTH_INVALID)
        .max(REDEMPTION_VALIDATION.NAME_MAX_LENGTH, msg.NAME_LENGTH_INVALID),
      quota_dollars: z.number().min(0, t('Quota must be a positive number')),
      distribution_type: z.string().refine(
        (value) => value === 'sale' || value === 'gift',
        t('Select whether the redemption code is sold or gifted')
      ),
      price_yuan: z.string().refine(
        (value) => parsePriceCents(value) !== null,
        t('Enter a valid CNY price with up to two decimal places')
      ),
      expired_time: z.date().nullable(),
      count: z
        .number()
        .min(REDEMPTION_VALIDATION.COUNT_MIN, msg.COUNT_INVALID)
        .max(REDEMPTION_VALIDATION.COUNT_MAX, msg.COUNT_INVALID)
        .optional(),
    })
    .superRefine((data, context) => {
      const price = parsePriceCents(data.price_yuan)
      if (data.distribution_type === 'sale' && (!price || price <= 0)) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['price_yuan'],
          message: t('Sold redemption codes must have a positive price'),
        })
      }
      if (data.distribution_type === 'gift' && price !== 0) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['price_yuan'],
          message: t('Gift redemption codes must have a zero price'),
        })
      }
    })
}

export type RedemptionFormValues = {
  name: string
  quota_dollars: number
  distribution_type: string
  price_yuan: string
  expired_time: Date | null
  count?: number
}

// ============================================================================
// Form Defaults
// ============================================================================

export const REDEMPTION_FORM_DEFAULT_VALUES: RedemptionFormValues = {
  name: '',
  quota_dollars: 10,
  distribution_type: '',
  price_yuan: '0',
  expired_time: null,
  count: 1,
}

// ============================================================================
// Form Data Transformation
// ============================================================================

/**
 * Transform form data to API payload
 */
export function transformFormDataToPayload(
  data: RedemptionFormValues
): RedemptionFormData {
  const distributionType =
    data.distribution_type === 'sale' ? 'sale' : 'gift'
  return {
    name: data.name,
    quota: parseQuotaFromDollars(data.quota_dollars),
    expired_time: data.expired_time
      ? Math.floor(data.expired_time.getTime() / 1000)
      : 0,
    distribution_type: distributionType,
    price_cents:
      distributionType === 'gift' ? 0 : parsePriceCents(data.price_yuan) ?? 0,
    count: data.count || 1,
  }
}

/**
 * Transform redemption data to form defaults
 */
export function transformRedemptionToFormDefaults(
  redemption: Redemption
): RedemptionFormValues {
  return {
    name: redemption.name,
    quota_dollars: quotaUnitsToEditableAmount(redemption.quota),
    distribution_type:
      redemption.distribution_type === 'sale' ||
      redemption.distribution_type === 'gift'
        ? redemption.distribution_type
        : '',
    price_yuan: (redemption.price_cents / 100).toFixed(2),
    expired_time:
      redemption.expired_time > 0
        ? new Date(redemption.expired_time * 1000)
        : null,
    count: 1,
  }
}
