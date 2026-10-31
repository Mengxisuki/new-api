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
import { useQuery } from '@tanstack/react-query'

import { getInvoiceProfile, INVOICE_PROFILE_QUERY_KEY } from '../api'

// 页面和快捷配置共享查询键，保存成功后统一刷新资料。
// 查询缓存仅存在内存，不把税号、银行信息等资料写入本地存储。
export function useInvoiceProfile(enabled = true) {
  return useQuery({
    queryKey: INVOICE_PROFILE_QUERY_KEY,
    queryFn: getInvoiceProfile,
    enabled, // 默认不开票时钱包无需预取敏感抬头资料。
    staleTime: 0, // 开始付款或重新访问资料时重新确认服务端配置。
  })
}
