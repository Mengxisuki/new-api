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
import { z } from 'zod'

// 同一前端规则同时约束默认率、分组率与可视化行，不能提交后才发现精度冲突。
// 百分比用6表示6%，保留四位小数与服务端持久快照一致。
// coerce仅用于数字输入；分组JSON入口仍先检查原始值确为number。
export const invoiceRateSchema = z.coerce
  .number()
  .min(0, 'Invoice surcharge percentage must be between 0 and 100') // 零费率明确代表免加价。
  .max(100, 'Invoice surcharge percentage must be between 0 and 100') // 防止输入错误产生超过原价的加价百分比。
  .refine(
    (rate) => Number.isFinite(rate) && Number(rate.toFixed(4)) === rate,
    'Invoice surcharge percentage supports at most 4 decimal places'
  )
