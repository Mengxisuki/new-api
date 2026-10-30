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
import i18next from 'i18next'
import { useState, useEffect, useCallback, useRef } from 'react'
import { toast } from 'sonner'

import { useIsAdmin, useIsSuperAdmin } from '@/hooks/use-admin'
import { useDebounce } from '@/hooks/use-debounce'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import {
  getUserBillingHistory,
  getAllBillingHistory,
  completeOrder,
  retryOfficialPayment,
  isApiSuccess,
} from '../api'
import type { TopupRecord } from '../types'

// ============================================================================
// Billing History Hook
// ============================================================================

interface UseBillingHistoryOptions {
  /** Initial page number */
  initialPage?: number
  /** Initial page size */
  initialPageSize?: number
}

export function useBillingHistory(options: UseBillingHistoryOptions = {}) {
  const { initialPage = 1, initialPageSize = 10 } = options
  const isAdmin = useIsAdmin()
  const isSuperAdmin = useIsSuperAdmin() // 补单权限与账单查看权限分别判断。
  const currentUserId = useAuthStore((state) => state.auth.user?.id ?? 0)

  const [records, setRecords] = useState<TopupRecord[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(initialPage)
  const [pageSize, setPageSize] = useState(initialPageSize)
  const [keyword, setKeyword] = useState('')
  const debouncedKeyword = useDebounce(keyword)
  const requestIdRef = useRef(0)
  const [loading, setLoading] = useState(false)
  const [completing, setCompleting] = useState(false)
  const [retryingTradeNo, setRetryingTradeNo] = useState<string | null>(null)

  /**
   * Fetch billing history
   */
  const fetchBillingHistory = useCallback(async () => {
    const requestId = ++requestIdRef.current
    setLoading(true)
    try {
      const response = isAdmin
        ? await getAllBillingHistory(page, pageSize, debouncedKeyword)
        : await getUserBillingHistory(page, pageSize, debouncedKeyword)

      if (requestId !== requestIdRef.current) return

      if (isApiSuccess(response) && response.data) {
        setRecords(response.data.items || [])
        setTotal(response.data.total || 0)
      } else {
        handleServerError(response, i18next.t('Failed to load billing history'))
        setRecords([])
        setTotal(0)
      }
    } catch (error) {
      if (requestId !== requestIdRef.current) return
      handleServerError(error, i18next.t('Failed to load billing history'))
      setRecords([])
      setTotal(0)
    } finally {
      if (requestId === requestIdRef.current) {
        setLoading(false)
      }
    }
  }, [debouncedKeyword, isAdmin, page, pageSize])

  /**
   * Complete a pending order (admin only)
   */
  const handleCompleteOrder = useCallback(
    async (tradeNo: string) => {
      if (!isSuperAdmin) {
        toast.error(i18next.t('Super admin access required'))
        return false
      }

      setCompleting(true)
      try {
        const response = await completeOrder({ trade_no: tradeNo })
        if (isApiSuccess(response)) {
          toast.success(i18next.t('Order completed successfully'))
          // Refresh the list
          await fetchBillingHistory()
          return true
        } else {
          handleServerError(response, i18next.t('Failed to complete order'))
          return false
        }
      } catch (error) {
        handleServerError(error, i18next.t('Failed to complete order'))
        return false
      } finally {
        setCompleting(false)
      }
    },
    [isSuperAdmin, fetchBillingHistory]
  )

  // 重试请求只传订单号，服务端核对归属、有效期和冻结金额。
  // 成功与失败后都刷新账单，让支付回调或到期状态及时可见。
  // 错误仍走统一解析，避免把请求对象或商户资料写入浏览器日志。
  const handleRetryPayment = useCallback(
    async (tradeNo: string) => {
      setRetryingTradeNo(tradeNo)
      try {
        const response = await retryOfficialPayment({ trade_no: tradeNo })
        if (!isApiSuccess(response) || !response.data) {
          handleServerError(response, i18next.t('Failed to retry payment'))
          await fetchBillingHistory()
          return null
        }
        await fetchBillingHistory()
        return response.data
      } catch (error) {
        handleServerError(error, i18next.t('Failed to retry payment'))
        return null
      } finally {
        setRetryingTradeNo(null)
      }
    },
    [fetchBillingHistory]
  )

  /**
   * Change page
   */
  const handlePageChange = useCallback((newPage: number) => {
    setPage(newPage)
  }, [])

  /**
   * Change page size
   */
  const handlePageSizeChange = useCallback((newPageSize: number) => {
    setPageSize(newPageSize)
    setPage(1) // Reset to first page when changing page size
  }, [])

  /**
   * Search by keyword
   */
  const handleSearch = useCallback((newKeyword: string) => {
    requestIdRef.current += 1
    setKeyword(newKeyword)
    setPage(1) // Reset to first page when searching
  }, [])

  // Fetch data after the search draft has settled.
  useEffect(() => {
    if (keyword !== debouncedKeyword) return

    fetchBillingHistory()
  }, [debouncedKeyword, fetchBillingHistory, keyword])

  return {
    records,
    total,
    page,
    pageSize,
    keyword,
    loading,
    completing,
    isAdmin,
    isSuperAdmin,
    currentUserId,
    retryingTradeNo,
    handleRetryPayment,
    handlePageChange,
    handlePageSizeChange,
    handleSearch,
    handleCompleteOrder,
    refresh: fetchBillingHistory,
  }
}
