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

// 财务总览统一按上海自然日统计，与后端分日口径一致。
// 上海固定为 UTC+8 且没有夏令时，这里用固定偏移计算，
// 不依赖浏览器所在时区：在其他时区打开页面也得到同一组数字。
const SHANGHAI_OFFSET = 8 * 3600
const DAY = 86400

// 与后端 FinanceOverviewMaxDays 一致，超出会被接口拒绝。
export const MAX_RANGE_DAYS = 366

export type RangePreset = 'today' | '7d' | '30d' | 'month' | 'last_month'

// 查询时段为左闭右开的 Unix 秒区间。
export type FinanceRange = { start: number; end: number }

// 某个时刻所在上海自然日的零点。
function shanghaiDayStart(sec: number): number {
  return sec - ((sec + SHANGHAI_OFFSET) % DAY)
}

// 上海某年某月一日零点；月份可以越界（-1 表示上一年十二月），由 Date.UTC 归一化。
function shanghaiMonthStart(year: number, month: number): number {
  return Date.UTC(year, month, 1) / 1000 - SHANGHAI_OFFSET
}

// 预设时段都以“今天结束”为右边界，因此包含今天已发生的收入；
// 只有“上月”是完整的历史月份。
export function presetRange(preset: RangePreset, nowSec: number): FinanceRange {
  const today = shanghaiDayStart(nowSec)
  const tomorrow = today + DAY
  // 把时间平移到上海再按 UTC 读取年月，得到的是上海日历上的年月。
  const shifted = new Date((nowSec + SHANGHAI_OFFSET) * 1000)
  const year = shifted.getUTCFullYear()
  const month = shifted.getUTCMonth()
  switch (preset) {
    case 'today':
      return { start: today, end: tomorrow }
    case '7d':
      return { start: today - 6 * DAY, end: tomorrow }
    case '30d':
      return { start: today - 29 * DAY, end: tomorrow }
    case 'month':
      return { start: shanghaiMonthStart(year, month), end: tomorrow }
    case 'last_month':
      return {
        start: shanghaiMonthStart(year, month - 1),
        end: shanghaiMonthStart(year, month),
      }
  }
}

// 日期选择器返回的是浏览器本地日期，这里只取它的年月日，
// 再按上海日历解释，避免本地时区把所选日期偏移一天。
// 起止日期都包含在内；顺序颠倒或超过上限时返回 null，由界面提示。
export function customRange(from: Date, to: Date): FinanceRange | null {
  const start =
    Date.UTC(from.getFullYear(), from.getMonth(), from.getDate()) / 1000 -
    SHANGHAI_OFFSET
  const end =
    Date.UTC(to.getFullYear(), to.getMonth(), to.getDate()) / 1000 -
    SHANGHAI_OFFSET +
    DAY
  if (end <= start || end - start > MAX_RANGE_DAYS * DAY) return null
  return { start, end }
}

// 把上海自然日零点格式化为 YYYY-MM-DD；与语言无关，图表和表格共用。
export function formatShanghaiDay(daySec: number): string {
  return new Date((daySec + SHANGHAI_OFFSET) * 1000).toISOString().slice(0, 10)
}
