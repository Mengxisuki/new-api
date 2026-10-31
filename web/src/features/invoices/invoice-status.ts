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
import type { InvoiceStatus } from './types'

// 状态只用于展示，推进规则始终由服务端付款及开票事务决定。
// pending_payment 与 pending 必须区分，防止财务提前为未付款订单开票。
export const INVOICE_STATUS_LABELS: Record<InvoiceStatus, string> = {
  pending_payment: 'Awaiting payment', // 未收款不可提前办理，即便抬头已经完整。
  pending: 'Awaiting invoice',
  processing: 'Invoice processing',
  issued: 'Invoice issued', // 只有号码和受保护附件俱全时服务端才会进入此状态。
  rejected: 'Invoice rejected',
}
