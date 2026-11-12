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
import type { QuotaAdjustReason } from '../types'

// 原因键保持英文，组件和财务总览分别用 t() 订阅当前语言。
// 该映射是财务口径的唯一展示入口，避免列表和表单各自拼写标签。
// compensation/grant 是纯发放，offline_cash 代表已收到现金，on_account 代表待收。
// revoke_* 与 correction 只用于扣减或下调覆盖，不应被新增发放默认选中。
// 增加新原因时必须同步后端校验、总览汇总和测试，不能只扩展下拉选项。
export const ADJUSTMENT_REASON_LABELS: Record<QuotaAdjustReason, string> = {
  compensation: 'Compensation', // 服务问题补偿，不计入现金实收。
  grant: 'Grant', // 主动赠送权益，与履约补偿分列审计。
  on_account: 'On account', // 应收单独按人民币分登记，不从发放额度推算。
  offline_cash: 'Offline cash', // 已核实线下收款，必须附金额和外部凭证。
  revoke_grant: 'Revoke grant', // 撤回赠送只更正额度，不触发现金退款。
  revoke_compensation: 'Revoke compensation', // 撤回补偿不删除原始补偿记录。
  correction: 'Correction', // 其他向下更正保留旧账关联，不消除欠款。
}
