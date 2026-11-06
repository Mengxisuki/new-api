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
import { CancelledError, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook } from '@testing-library/react'
import {
  AxiosError,
  AxiosHeaders,
  CanceledError,
  type AxiosAdapter,
} from 'axios'
import i18next from 'i18next'
import { createElement, type ReactNode } from 'react'
import { toast } from 'sonner'
import { afterEach, expect, it, vi } from 'vitest'

import { quoteOrder } from '@/features/account-recharge/api'
import { saveModelPricing } from '@/features/model-pricing/api'
import { useUpdateOption } from '@/features/system-settings/hooks/use-update-option'
import zh from '@/i18n/locales/zh.json'
import { handleServerError } from '@/lib/handle-server-error'
import { api } from '@/lib/http-client'
import { createAppQueryClient } from '@/lib/query-client'
import { AuthOperationError } from '@/lib/secure-verification'
import {
  createServerError,
  getServerErrorMessage,
} from '@/lib/server-error-message'
import { useAuthStore, type AuthBundle } from '@/stores/auth-store'

const originalAdapter = api.defaults.adapter

afterEach(() => {
  api.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
  window.history.replaceState({}, '', '/')
})

it('reports the pricing rejection once when the request error reaches multiple handlers', async () => {
  const message = 'model_pricing: unknown name fixed (1:16)'
  const notify = vi.spyOn(toast, 'error').mockReturnValue('pricing-error')
  const adapter: AxiosAdapter = async (config) => {
    throw new AxiosError(
      'Request failed with status code 400',
      'ERR_BAD_REQUEST',
      config,
      undefined,
      {
        data: { success: false, message },
        status: 400,
        statusText: 'Bad Request',
        headers: {},
        config,
      }
    )
  }
  api.defaults.adapter = adapter
  await saveModelPricing([
    { model_name: 'example', expected_version: 'v1', pricing: {}, reset: true },
  ]).catch((error) => {
    handleServerError(error)
    handleServerError(error)
  })
  expect(notify.mock.calls.map(([message]) => message)).toEqual([message])
})

it('uses an ordinary Error message and reports identical independent failures separately', () => {
  const notify = vi.spyOn(toast, 'error').mockReturnValue('error')
  handleServerError(new Error('The expression is invalid'))
  handleServerError(new Error('The expression is invalid'))
  expect(notify.mock.calls.map(([message]) => message)).toEqual([
    'The expression is invalid',
    'The expression is invalid',
  ])
})

it('keeps unsuccessful business responses resolved and silent until a caller handles them', async () => {
  const notify = vi.spyOn(toast, 'error').mockReturnValue('error')
  const payload = { success: false, message: 'Invalid expression' }
  api.defaults.adapter = async (config) => ({
    data: payload,
    status: 200,
    statusText: 'OK',
    headers: {},
    config,
  })
  const response = await api.patch('/api/option/model_pricing', {})
  expect(response.data).toEqual(payload)
  expect(notify).not.toHaveBeenCalled()
  handleServerError(response.data)
  handleServerError(createServerError(response.data))
  expect(notify.mock.calls.map(([message]) => message)).toEqual([
    'Invalid expression',
  ])
})

it('uses production mutation callbacks to report nested pricing failures only once per operation', async () => {
  const client = createAppQueryClient()
  const notify = vi.spyOn(toast, 'error').mockReturnValue('error')
  api.defaults.adapter = async (config) => ({
    data: { success: false, message: 'Invalid expression' },
    status: 200,
    statusText: 'OK',
    headers: {},
    config,
  })
  for (const name of ['first attempt', 'second attempt']) {
    const inner = client
      .getMutationCache()
      .build(client, { mutationFn: saveModelPricing })
    const outer = client.getMutationCache().build(client, {
      mutationFn: () =>
        inner.execute([
          {
            model_name: name,
            expected_version: 'v1',
            pricing: {},
            reset: true,
          },
        ]),
      onError: (error) => handleServerError(error),
    })
    await outer.execute(undefined).catch((error) => handleServerError(error))
  }
  expect(notify.mock.calls.map(([message]) => message)).toEqual([
    'Invalid expression',
    'Invalid expression',
  ])
  client.clear()
})

it('only reports a query failure after retries are exhausted and stays silent when a retry succeeds', async () => {
  const client = createAppQueryClient()
  const notify = vi.spyOn(toast, 'error').mockReturnValue('error')
  let attempts = 0
  api.defaults.adapter = async (config) => {
    attempts++
    if (attempts <= 2) throw new AxiosError('Offline', 'ERR_NETWORK', config)
    return {
      data: { success: true },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  await client.fetchQuery({
    queryKey: ['retry success'],
    queryFn: () => api.get('/retry-success'),
    retry: 2,
    retryDelay: 0,
  })
  expect(attempts).toBe(3)
  expect(notify).not.toHaveBeenCalled()
  api.defaults.adapter = async (config) => {
    throw new AxiosError('Offline', 'ERR_NETWORK', config)
  }
  await client
    .fetchQuery({
      queryKey: ['retry failure'],
      queryFn: () => api.get('/retry-failure'),
      retry: 2,
      retryDelay: 0,
    })
    .catch(handleServerError)
  expect(notify.mock.calls.map(([message]) => message)).toEqual(['Offline'])
  client.clear()
})

it('lets a local handler own notifications when automatic error toasts are disabled', async () => {
  const client = createAppQueryClient()
  const notify = vi.spyOn(toast, 'error').mockReturnValue('error')
  const failure = new Error('Inline error')
  const mutation = client.getMutationCache().build(client, {
    mutationFn: async () => {
      throw failure
    },
    meta: { errorToast: false },
  })
  await expect(mutation.execute(undefined)).rejects.toBe(failure)
  expect(notify).not.toHaveBeenCalled()
  handleServerError(failure)
  expect(notify.mock.calls.map(([message]) => message)).toEqual([
    'Inline error',
  ])
  const queryError = new Error('Silent query')
  await expect(
    client.fetchQuery({
      queryKey: ['silent'],
      queryFn: async () => {
        throw queryError
      },
      retry: false,
      meta: { errorToast: false },
    })
  ).rejects.toBe(queryError)
  expect(notify).toHaveBeenCalledTimes(1)
  client.clear()
})

it('keeps cancellations silent even after wrapping them in another error', () => {
  const notify = vi.spyOn(toast, 'error').mockReturnValue('error')
  for (const error of [
    new CanceledError(),
    new DOMException('Aborted', 'AbortError'),
    new CancelledError(),
  ]) {
    handleServerError(new Error('Request failed', { cause: error }))
  }
  expect(notify).not.toHaveBeenCalled()
})

it.each([
  [
    { response: { data: { message: 'Backend detail' } }, message: 'HTTP 400' },
    'Backend detail',
  ],
  [
    { response: { data: { error: { message: 'Nested detail' } } } },
    'Nested detail',
  ],
  [{ error: 'String error' }, 'String error'],
  [{ title: 'Response title' }, 'Response title'],
  [{ response: { status: 304, data: {} } }, 'Content not modified!'],
  [{ message: ' ' }, 'Operation failed'],
  [null, 'Operation failed'],
  [{ response: { data: '<html>proxy error</html>' } }, 'Operation failed'],
])('extracts a useful message or fallback from %j', (error, expected) => {
  expect(getServerErrorMessage(error, 'Operation failed')).toBe(expected)
})

it('preserves safe authentication messages and recognizes their original failure without exposing its payload', () => {
  const notify = vi.spyOn(toast, 'error').mockReturnValue('error')
  const original = new AxiosError(
    'HTTP 500',
    'ERR_BAD_RESPONSE',
    undefined,
    undefined,
    {
      data: { message: 'private server detail' },
      status: 500,
      statusText: 'Error',
      headers: {},
      config: { headers: new AxiosHeaders() },
    }
  )
  const safe = AuthOperationError.from(original)
  const wrapper = new Error('Operation failed', { cause: safe })
  expect(getServerErrorMessage(wrapper)).toBe('Please try again later.')
  handleServerError(wrapper)
  handleServerError(original)
  expect(notify.mock.calls.map(([message]) => message)).toEqual([
    'Please try again later.',
  ])
})

it('handles circular error causes and prioritizes translated stable error codes', () => {
  const error = new Error('Raw error')
  error.cause = { code: 'AUTH_INTERNAL_ERROR', cause: error }
  expect(getServerErrorMessage(error)).toBe('Please try again later.')
  const notify = vi.spyOn(toast, 'error').mockReturnValue('error')
  handleServerError(error)
  handleServerError(error.cause)
  expect(notify).toHaveBeenCalledTimes(1)
})

it.each([200, 401])(
  'refreshes a 401 through the real auth client and only reports a terminal failure (refresh HTTP %i)',
  async (refreshStatus) => {
    window.history.replaceState({}, '', '/sign-in')
    const original: AuthBundle = {
      access_token: 'expired-access',
      token_type: 'Bearer',
      access_expires_at: 1,
      user: { id: 1, username: 'test-user', role: 1 },
      session: {
        sid: 'test-session',
        current: true,
        login_method: 'password',
        ip: '',
        user_agent: '',
        created_at: 1,
        last_active_at: 1,
        expires_at: 2_000_000_000,
      },
    }
    useAuthStore.getState().auth.setBundle(original)
    const fresh = {
      ...original,
      access_token: 'fresh-access',
      access_expires_at: 2_000_000_000,
    }
    const open = vi.spyOn(XMLHttpRequest.prototype, 'open')
    vi.spyOn(XMLHttpRequest.prototype, 'send').mockImplementation(
      function (this: XMLHttpRequest) {
        Object.defineProperties(this, {
          status: { value: refreshStatus, configurable: true },
          statusText: { value: 'Refresh response', configurable: true },
          responseText: {
            value: JSON.stringify({
              success: refreshStatus === 200,
              data: fresh,
            }),
            configurable: true,
          },
          readyState: { value: 4, configurable: true },
        })
        this.onloadend?.(new ProgressEvent('loadend'))
      }
    )
    let requests = 0
    api.defaults.adapter = async (config) => {
      requests++
      if (!config.authRetry) {
        throw new AxiosError('HTTP 401', 'ERR_BAD_REQUEST', config, undefined, {
          data: { message: 'Access token expired' },
          status: 401,
          statusText: 'Unauthorized',
          headers: {},
          config,
        })
      }
      expect(config.headers.get('Authorization')).toBe('Bearer fresh-access')
      return {
        data: { success: true },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    const notify = vi.spyOn(toast, 'error').mockReturnValue('error')
    const client = createAppQueryClient()
    await client
      .fetchQuery({
        queryKey: ['refresh', refreshStatus],
        queryFn: () => api.get('/protected'),
        retry: false,
      })
      .catch(handleServerError)
    expect(open).toHaveBeenCalledTimes(1)
    expect(open.mock.calls[0][1]).toBe('/api/user/auth/refresh')
    if (refreshStatus === 200) {
      expect(requests).toBe(2)
      expect(notify).not.toHaveBeenCalled()
      expect(useAuthStore.getState().auth.accessToken).toBe('fresh-access')
    } else {
      expect(requests).toBe(1)
      expect(notify.mock.calls.map(([message]) => message)).toEqual([
        'Session expired!',
      ])
      expect(useAuthStore.getState().auth.accessToken).toBeNull()
    }
    client.clear()
  }
)

it('never refreshes or replays a failed single-use authorization request', async () => {
  useAuthStore.getState().auth.setBundle({
    access_token: 'access',
    token_type: 'Bearer',
    access_expires_at: 2_000_000_000,
    user: { id: 1, username: 'test-user', role: 1 },
    session: {
      sid: 'proof-session',
      current: true,
      login_method: 'password',
      ip: '',
      user_agent: '',
      created_at: 1,
      last_active_at: 1,
      expires_at: 2_000_000_000,
    },
  })
  const send = vi.spyOn(XMLHttpRequest.prototype, 'send')
  const adapter = vi.fn<AxiosAdapter>(async (config) => {
    throw new AxiosError('Unauthorized', 'ERR_BAD_REQUEST', config, undefined, {
      data: { message: 'Proof rejected' },
      status: 401,
      statusText: 'Unauthorized',
      headers: {},
      config,
    })
  })
  api.defaults.adapter = adapter
  const notify = vi.spyOn(toast, 'error').mockReturnValue('error')
  await api
    .post(
      '/sensitive',
      {},
      {
        singleUseAuthorization: true,
        skipErrorHandler: true,
        headers: { 'X-Security-Proof': 'test-proof' },
      }
    )
    .catch(handleServerError)
  expect(adapter).toHaveBeenCalledTimes(1)
  expect(send).not.toHaveBeenCalled()
  expect(notify.mock.calls.map(([message]) => message)).toEqual([
    'Proof rejected',
  ])
})

it('rejects an unsuccessful setting update so callers cannot proceed as if it saved, with one useful notification', async () => {
  const client = createAppQueryClient()
  const notify = vi.spyOn(toast, 'error').mockReturnValue('setting-error')
  const success = vi.spyOn(toast, 'success').mockReturnValue('saved')
  api.defaults.adapter = async (config) => ({
    data: { success: false, message: 'Setting value is invalid' },
    status: 200,
    statusText: 'OK',
    headers: {},
    config,
  })
  const { result, unmount } = renderHook(() => useUpdateOption(), {
    wrapper: (props: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client }, props.children),
  })
  await act(async () => {
    await expect(
      result.current.mutateAsync({ key: 'QuotaPerUnit', value: 'invalid' })
    ).rejects.toThrow('Setting value is invalid')
  })
  expect(notify.mock.calls.map(([message]) => message)).toEqual([
    'Setting value is invalid',
  ])
  expect(success).not.toHaveBeenCalled()
  unmount()
  client.clear()
})

// 使用真实中文资源和全局查询错误处理，覆盖报价请求最终弹出的文案。
// 只替换 HTTP 边界；业务 API、响应解析和 Toast 翻译均执行真实实现。
// 后端显式提供 message_key，不能靠猜测所有外部错误文本来翻译。
it('挂帐额度不足的报价错误以中文提示且只通知一次', async () => {
  const previousLanguage = i18next.language // 记住测试前的语言，结束后完整恢复。
  i18next.addResourceBundle('zhCN', 'translation', zh.translation, true, true) // 加载项目已有中文词条，不能在测试里编造译文。
  await i18next.changeLanguage('zhCN') // 复现中文界面收到英文业务错误的场景。
  const client = createAppQueryClient() // 页面实际使用的统一错误通知链路。
  const notify = vi.spyOn(toast, 'error').mockReturnValue('credit-error') // 观察用户最终看到的提示，而非只测词条存在。
  api.defaults.adapter = async (config) => {
    // 模拟后端修复后的 400 响应，保留英文供非网页调用者读取。
    throw new AxiosError(
      'Request failed with status code 400',
      'ERR_BAD_REQUEST',
      config,
      undefined,
      {
        data: {
          success: false, // 报价失败不得继续创建订单。
          message: 'Insufficient available credit', // 外部调用者保留兼容英文原文。
          message_key: 'Insufficient available credit', // 显式元数据才触发统一前端翻译。
        },
        status: 400, // 保持真实业务拒绝状态。
        statusText: 'Bad Request',
        headers: {},
        config,
      }
    )
  }
  try {
    await expect(
      client.fetchQuery({
        queryKey: ['recharge', 'quote', 'credit-i18n'], // 使用独立缓存键，避免继承成功报价。
        queryFn: () =>
          quoteOrder({
            product_id: 4, // 复现请求参数，不读取或修改线上商品。
            product_version: 5, // 版本仅用于构造报价，不会发起真实订单。
            payment_method: 'credit', // 挂帐错误经真实业务 API 传播。
            invoice_required: false, // 排除开票校验对测试错误的干扰。
            request_id: '', // 报价没有写入动作，不需要付款幂等键。
          }),
        retry: false, // 业务拒绝无需等待网络退避才能验收文案。
      })
    ).rejects.toThrow()
    expect(notify.mock.calls.map(([message]) => message)).toEqual([
      '可用挂帐额度不足。',
    ])
  } finally {
    client.clear() // 释放查询状态，避免错误通知影响后续测试。
    await i18next.changeLanguage(previousLanguage) // 恢复共享语言，不污染其他场景。
  }
})
