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
import type { AuthBundle } from '@/stores/auth-store'

export type VerificationMethod =
  | '2fa'
  | 'passkey'
  | 'password'
  | 'oauth'
  | 'session'
export type SecurityProofScope =
  // 接入凭据只允许指定消费者及主题，不能复用为个人管理令牌证明。
  | 'event_hub.token'
  // 账号挂靠复用同一证明机制，证明不能跨账号或消费操作使用。
  | 'hosting.manage'
  | 'recharge.manage'
  | 'channel.key.read'
  | 'passkey.register'
  | 'passkey.delete'
  | '2fa.setup'
  | '2fa.disable'
  | '2fa.backup_codes.regenerate'
  | 'access_token.generate'
  | 'access_token.update'
  | 'access_token.revoke'
  | 'account.binding.bind'
  | 'account.binding.unbind'
  | 'account.password.set'
  | 'account.password.change'
  | 'account.delete'
  | 'admin.user.create'
  | 'admin.user.update'
  | 'admin.user.delete'
  | 'admin.user.manage'
  | 'admin.user.passkey.reset'
  | 'admin.user.2fa.disable'
  | 'admin.user.binding.clear'

/** ManageUser actions that change a user's status or role and need step-up. */
// 财务角色变更沿用单次管理验证，不能通过普通资料更新绕过。
export type AdminUserManageAction =
  | 'disable'
  | 'enable'
  | 'promote'
  | 'demote'
  | 'finance'
  | 'sales'

export type VerificationOperation =
  | {
      // 签发前固定消费者与授权；排序由调用端和服务端共同规范化。
      scope: 'event_hub.token'
      context: { consumer_id: number; topics: string[]; expires_at: number }
    }
  // 绑定订单与版本，转派后旧证明不可复用。
  | {
      scope: 'recharge.manage'
      context: {
        order_id: number
        version: number
        action: 'reveal' | 'refund' | 'refund_confirm'
      }
    }
  | {
      // 连接不指定账号；重授权、删除与重置都必须绑定明确目标。
      scope: 'hosting.manage'
      context: {
        action: 'connect' | 'reauthorize' | 'delete' | 'reset'
        account_id: number
        // 重置沿用持久 UUID，其他操作使用空字符串保持规范化一致。
        operation_id: string
      }
    }
  | { scope: 'channel.key.read'; context: { channel_id: number } }
  | {
      scope: 'account.binding.bind'
      context: { provider: string; email?: string; code?: string }
    }
  | { scope: 'account.binding.unbind'; context: { provider_id: number } }
  | { scope: 'admin.user.create'; context: { role: number } }
  | {
      scope:
        | 'admin.user.update'
        | 'admin.user.delete'
        | 'admin.user.passkey.reset'
        | 'admin.user.2fa.disable'
      context: { user_id: number }
    }
  | {
      scope: 'admin.user.manage'
      context: { user_id: number; action: AdminUserManageAction }
    }
  | {
      scope: 'admin.user.binding.clear'
      context:
        | { user_id: number; binding_type: string }
        | { user_id: number; provider_id: number }
    }
  | {
      scope: 'access_token.generate'
      context: { scopes: string[]; expires_at: number }
    }
  | {
      scope: 'access_token.update'
      context: { token_id: number; scopes: string[] }
    }
  | {
      scope: 'access_token.revoke'
      context: { token_id: number } | { legacy: true }
    }
  | {
      scope: Exclude<
        SecurityProofScope,
        | 'hosting.manage'
        | 'event_hub.token'
        | 'recharge.manage'
        | 'channel.key.read'
        | 'account.binding.bind'
        | 'account.binding.unbind'
        | 'access_token.generate'
        | 'access_token.update'
        | 'access_token.revoke'
        | `admin.user.${string}`
      >
      context?: Record<string, never>
    }

export interface SecurityProof {
  proof_token: string
  expires_at: number
  method: VerificationMethod
  scope: SecurityProofScope
}

export interface VerificationRequirements {
  scope: SecurityProofScope | 'auth.login'
  methods: { method: VerificationMethod; available: boolean; reason?: string }[]
  oauth_providers: { slug: string; name: string }[]
  password_encryption_enabled: boolean
}

export type VerificationInput =
  | { method: '2fa'; code: string }
  | { method: 'password'; password: string }
  | { method: 'passkey'; rpID?: string }
  | { method: 'oauth'; provider: string }
  | { method: 'session' }

export type RequestVerificationOptions = VerificationOperation & {
  title?: string
  description?: string
}

export interface LoginChallenge {
  require_verification: true
  flow_token: string
  expires_at: number
  methods: VerificationRequirements['methods']
}

export interface RequestLoginVerificationOptions {
  scope: 'auth.login'
  challenge: LoginChallenge
  title?: string
  description?: string
}

export type VerificationRequest =
  | RequestVerificationOptions
  | RequestLoginVerificationOptions
export type LoginResult = AuthBundle | LoginChallenge

export type SecureVerificationState =
  | { phase: 'idle' }
  | { phase: 'loading'; request: VerificationRequest }
  | { phase: 'error'; request: VerificationRequest; error: string }
  | {
      phase: 'ready' | 'verifying'
      request: VerificationRequest
      requirements: VerificationRequirements
      input: VerificationInput | null
      error?: string
    }
