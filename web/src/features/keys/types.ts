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

import { policySchema, type ResourceSource } from '@/features/hosting/types'

// ============================================================================
// API Key Schema & Types
// ============================================================================

export const apiKeySchema = z.object({
  // 列表不携带策略；详情返回本人 Key 的完整授权顺序。
  resource_policy: policySchema.optional(),
  id: z.number(),
  name: z.string(),
  key: z.string(),
  status: z.number(), // 1: enabled, 2: disabled, 3: expired, 4: exhausted
  remain_quota: z.number(),
  used_quota: z.number(),
  unlimited_quota: z.boolean(),
  expired_time: z.number(), // -1 for never expires
  created_time: z.number(),
  accessed_time: z.number(),
  group: z.string().nullish().default(''),
  auto_groups: z.array(z.string()).nullish().default(null),
  cross_group_retry: z
    .preprocess((v) => {
      if (v === 1) return true
      if (v === 0) return false
      return v
    }, z.boolean())
    .optional()
    .default(false),
  // 新字段缺失表示旧服务端响应，编辑时按空映射展示。
  model_mapping: z.string().nullish(),
  model_limits_enabled: z.boolean(),
  model_limits: z.string().nullish().default(''),
  allow_ips: z.string().nullish().default(''),
  allow_user_quota_query: z.boolean().optional().default(false),
})

export type ApiKey = z.infer<typeof apiKeySchema>

// ============================================================================
// API Request/Response Types
// ============================================================================

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface GetApiKeysParams {
  p?: number
  size?: number
}

export interface GetApiKeysResponse {
  success: boolean
  message?: string
  data?: {
    items: ApiKey[]
    total: number
    page: number
    page_size: number
  }
}

export interface SearchApiKeysParams {
  keyword?: string
  token?: string
  p?: number
  size?: number
}

export interface ApiKeyFormData {
  // 与 Key 同事务保存；更新省略时保持旧合同。
  resource_policy?: { revision: number; sources: ResourceSource[] }
  name: string
  remain_quota: number
  expired_time: number
  unlimited_quota: boolean
  model_limits_enabled: boolean
  model_limits: string
  model_mapping?: string
  allow_ips: string
  allow_user_quota_query: boolean
  group: string
  auto_groups: string[]
  cross_group_retry: boolean
}

export interface TokenAutoGroupsConfig {
  groups: string[]
  max_count: number
}

// ============================================================================
// Dialog Types
// ============================================================================

export type ApiKeysDialogType =
  | 'create'
  | 'update'
  | 'delete'
  | 'batch-delete'
  | 'client-config'
  | 'cc-switch'
