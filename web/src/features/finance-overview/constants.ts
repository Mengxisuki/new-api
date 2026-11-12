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
import { formatInvoiceAmount } from '@/features/invoices/format'

import type { FinanceDaily } from './api'

// 四条业务线的固定顺序与配色：颜色跟随业务线本身，不随金额排名变化，
// 这样切换时段后同一条业务线始终是同一种颜色。
// labelKey 是 i18n 键，组件内通过 t() 渲染。
// 配色已用图表校验脚本在明暗两种卡片底色上验证相邻色的色弱可辨性；
// 浅色模式下后两种颜色对比度低于 3:1，因此图表始终配图例和表格视图。
export const REVENUE_LINES: {
  key: keyof Omit<FinanceDaily, 'day'>
  labelKey: string
  light: string
  dark: string
}[] = [
  {
    key: 'topup_cents',
    labelKey: 'Wallet top-up',
    light: '#2a78d6',
    dark: '#3987e5',
  },
  {
    key: 'subscription_cents',
    labelKey: 'Subscription',
    light: '#eb6834',
    dark: '#d95926',
  },
  {
    key: 'recharge_cents',
    labelKey: 'Account top-up',
    light: '#1baf7a',
    dark: '#199e70',
  },
  {
    key: 'credit_settled_cents',
    labelKey: 'Credit settlement',
    light: '#eda100',
    dark: '#c98500',
  },
]

// 收款渠道标识到展示名称的 i18n 键；未知渠道原样显示标识，不猜测名称。
export const CHANNEL_LABEL_KEYS: Record<string, string> = {
  official_wechat: 'WeChat Pay',
  official_alipay: 'Alipay',
  epay: 'Epay',
  credit_settlement: 'Credit settlement',
  offline_cash: 'Offline collection',
}

// 总览金额固定为人民币实收，不跟随站点余额的展示币种切换。
// locale 必须先经 toIntlLocale 转换，不能直接传界面语言码。
export function formatCNY(
  cents: number,
  locale?: Intl.LocalesArgument
): string {
  return formatInvoiceAmount(cents / 100, locale)
}
