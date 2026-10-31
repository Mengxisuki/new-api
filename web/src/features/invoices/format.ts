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
// 发票金额冻结为实际人民币收款，不跟随站点余额展示单位切换。
// 公共余额格式化工具不能表达历史交易币种，因此此处显式指定 CNY。
// locale 由组件用 toIntlLocale 转换，不能将 zhCN 等界面码直接传入 Intl。
export function formatInvoiceAmount(
  amount: number,
  locale?: Intl.LocalesArgument
): string {
  return new Intl.NumberFormat(locale, {
    style: 'currency', // 金额与币种一起格式化，避免手工拼符号遗漏本地小数规则。
    currency: 'CNY', // 已开票实付不能随全站 USD/CUSTOM 配置重标币种。
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(amount)
}
