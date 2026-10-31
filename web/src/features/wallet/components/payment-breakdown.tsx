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
import {
  formatLocalCurrencyAmount,
  formatQuotaWithCurrency,
} from '@/lib/currency'

interface PaymentBreakdownProps {
  localCnyInput?: boolean // 只有人民币本地渠道采用元输入，国际渠道不展示该公式。
  inputAmount: number // 用户当前请求充值的人民币金额，不是美元数量。
  paymentAmount: number // 来自同一次服务端报价的实际应付金额。
  creditedQuota?: number | null // 缺失时明确未知，禁止自行推导入账结果。
  calculating: boolean
  quoteValid?: boolean // 父级核对当前输入和渠道后才允许展示此报价。
}

// 本地人民币渠道使用服务端应入账额度说明充值、付款和到账的关系。
// 组件只格式化，不按输入金额、折扣或显示汇率自行生成入账额度。
// 国际网关仍保持其原有美元输入合同，不展示此人民币说明。
// 报价失败或字段缺失时显示未知，不能把上一份数据当作当前结果。
export function PaymentBreakdown(props: PaymentBreakdownProps) {
  const { t, i18n } = useTranslation() // 语言切换后公式与可访问名称同步更新。
  if (!props.localCnyInput) return null
  // 最多两位只用于货币展示，内部额度精度仍保留服务器返回值。
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const options = { digitsLarge: 2, digitsSmall: 2, abbreviate: false, locale }
  // 语言订阅用于触发格式化更新，公共工具维护实际站点币种规则。
  const valid = props.quoteValid && typeof props.creditedQuota === 'number'
  let message = t('Payment details unavailable. Please retry.')
  if (props.calculating) {
    message = t('Calculating payment details…')
  } else if (valid) {
    message = t(
      'Recharge {{input}} → Pay {{payment}} → Receive {{credit}} in platform credit',
      {
        input: formatLocalCurrencyAmount(props.inputAmount, options), // 输入已经是元，不再次换算。
        payment: formatLocalCurrencyAmount(props.paymentAmount, options), // 应付金额来自本次服务端报价。
        credit: formatQuotaWithCurrency(props.creditedQuota, options), // 到账始终采用服务器冻结单位。
      }
    )
  }
  return (
    <p
      className='text-muted-foreground bg-muted/30 rounded-md border px-3 py-2 text-xs leading-relaxed'
      role='status' // 报价刷新与失败提示不抢夺焦点，朗读可感知新结果。
      aria-label={t('Payment breakdown')}
      lang={locale}
    >
      {message}
    </p>
  )
}
