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
export interface KeyUsageKey {
  id: number
  name: string
  status: number
}

export interface KeyUsagePoint {
  token_id: number
  created_at: number
  successful_requests: number
  failed_requests: number
  prompt_tokens: number
  completion_tokens: number
  quota: number
  use_time_seconds: number
  cache_read_tokens: number
  cache_create_tokens: number
  latency_ms_sum: number
  latency_samples: number
}

export interface KeyUsageResponse {
  success: boolean
  message?: string
  data: {
    keys: KeyUsageKey[]
    points: KeyUsagePoint[]
  }
}
