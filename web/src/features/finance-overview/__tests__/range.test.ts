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
import { describe, expect, it } from 'vitest'

import {
  MAX_RANGE_DAYS,
  customRange,
  formatShanghaiDay,
  presetRange,
} from '../lib/range'

// 时段计算必须按上海日历，与后端分日口径一致，且不受运行环境时区影响。
// 期望值是手工换算的固定时间戳，不复用被测函数的算法。
const NOV_1 = 1793462400 // 上海 2026-11-01 00:00
const NOV_12 = NOV_1 + 11 * 86400 // 上海 2026-11-12 00:00
const NOV_13 = NOV_12 + 86400
const OCT_1 = NOV_1 - 31 * 86400 // 上海 2026-10-01 00:00

describe('presetRange', () => {
  // UTC 2026-11-11 17:00 在上海已经是 11 月 12 日 01:00。
  const now = Date.UTC(2026, 10, 11, 17, 0, 0) / 1000

  it('上海已跨日而 UTC 尚未跨日时，今天取上海的自然日', () => {
    expect(presetRange('today', now)).toEqual({ start: NOV_12, end: NOV_13 })
  })

  it('近 7 天和近 30 天都包含今天，共 7 个和 30 个自然日', () => {
    expect(presetRange('7d', now)).toEqual({
      start: NOV_12 - 6 * 86400,
      end: NOV_13,
    })
    expect(presetRange('30d', now)).toEqual({
      start: NOV_12 - 29 * 86400,
      end: NOV_13,
    })
  })

  it('本月从上海当月一日零点到今天结束', () => {
    expect(presetRange('month', now)).toEqual({ start: NOV_1, end: NOV_13 })
  })

  it('上月是完整的上一个自然月，不包含本月任何一天', () => {
    expect(presetRange('last_month', now)).toEqual({ start: OCT_1, end: NOV_1 })
  })

  it('一月时上月回到上一年的十二月', () => {
    const january = Date.UTC(2027, 0, 15, 4, 0, 0) / 1000
    expect(presetRange('last_month', january)).toEqual({
      start: Date.UTC(2026, 11, 1) / 1000 - 8 * 3600,
      end: Date.UTC(2027, 0, 1) / 1000 - 8 * 3600,
    })
  })

  it('上海月末最后一秒仍属于当月', () => {
    // 上海 2026-10-31 23:59:59。
    expect(presetRange('month', NOV_1 - 1)).toEqual({
      start: OCT_1,
      end: NOV_1,
    })
  })
})

describe('customRange', () => {
  it('起止日期都包含在内，按所选日期的上海自然日计算', () => {
    expect(customRange(new Date(2026, 10, 1), new Date(2026, 10, 12))).toEqual({
      start: NOV_1,
      end: NOV_13,
    })
  })

  it('起止为同一天时得到恰好一个自然日', () => {
    expect(customRange(new Date(2026, 10, 12), new Date(2026, 10, 12))).toEqual(
      { start: NOV_12, end: NOV_13 }
    )
  })

  it('结束早于开始时返回 null', () => {
    expect(
      customRange(new Date(2026, 10, 12), new Date(2026, 10, 1))
    ).toBeNull()
  })

  it('跨度等于上限时有效，超过上限一天时返回 null', () => {
    const from = new Date(2026, 0, 1)
    const atLimit = new Date(2026, 0, MAX_RANGE_DAYS)
    const overLimit = new Date(2026, 0, MAX_RANGE_DAYS + 1)
    expect(customRange(from, atLimit)).not.toBeNull()
    expect(customRange(from, overLimit)).toBeNull()
  })
})

describe('formatShanghaiDay', () => {
  it('把上海自然日零点格式化为该日的日期', () => {
    expect(formatShanghaiDay(NOV_1)).toBe('2026-11-01')
    expect(formatShanghaiDay(NOV_12)).toBe('2026-11-12')
  })
})
