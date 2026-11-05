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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { EventHubSection } from '..'
import {
  EVENT_TOPICS,
  type EventConsumer,
  type EventDetail,
  type EventSettings,
} from '../api'
import { ConsumerDetails } from '../components/consumer-details'
import { ConsumerEditor } from '../components/consumer-editor'
import { ServiceSettings } from '../components/service-settings'
import { TokenEditor } from '../components/token-editor'

// 夹具只包含公开摘要，不构造真实凭据或付款信息。
// 明确区分消费者、Token 和投递编号，避免错误字段碰巧相等。
const consumer: EventConsumer = {
  id: 7,
  name: 'Finance desktop',
  topics: [...EVENT_TOPICS],
  user_ids: [],
  enabled: true,
  created_at: 100,
  last_seen_at: 0,
  backlog: 1,
  token_count: 1,
  online_connections: 0,
  auth_valid: true,
  expired_unacked_count: 0,
}
const settings: EventSettings = {
  enabled: true,
  retention_days: 7,
  anomaly_min_quota: 2500000,
  anomaly_multiplier: 5,
  anomaly_min_samples: 20,
  anomaly_cold_quota: 10000000,
}
const detail: EventDetail = {
  consumer,
  tokens: [
    {
      id: 9,
      name: 'Desktop credential',
      topics: [EVENT_TOPICS[0]],
      token_hint: 'abcd1234',
      expires_at: 2000000000,
      created_at: 100,
      last_used_at: 0,
      revoked_at: 0,
    },
  ],
  deliveries: [
    {
      id: 11,
      event_id: 42,
      topic: EVENT_TOPICS[0],
      payload: { user_id: 3 },
      created_at: 100,
      last_sent_at: 100,
      attempts: 1,
      acked_at: 0,
    },
  ],
}
afterEach(() => vi.restoreAllMocks())

describe('event subscription management', () => {
  it('creates a consumer with normalized user filters and retains no shared cursor', async () => {
    const save = vi.fn().mockResolvedValue(true)
    const close = vi.fn()
    render(<ConsumerEditor pending={false} onSave={save} onClose={close} />)
    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Consumer name'), 'Finance worker')
    await user.type(screen.getByLabelText('Follow user IDs'), '34, 12, 34')
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(save).toHaveBeenCalledWith(
        {
          name: 'Finance worker',
          topics: [...EVENT_TOPICS].sort(),
          user_ids: [12, 34],
          enabled: undefined,
        },
        undefined
      )
    )
    expect(close).toHaveBeenCalledOnce()
  })

  it('rejects malformed user filters rather than subscribing to everyone', async () => {
    const save = vi.fn()
    render(<ConsumerEditor pending={false} onSave={save} onClose={vi.fn()} />)
    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Consumer name'), 'Finance')
    await user.type(screen.getByLabelText('Follow user IDs'), '3, invalid')
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    expect(
      await screen.findByText('Enter positive user IDs separated by commas')
    ).toBeVisible()
    expect(save).not.toHaveBeenCalled()
  })

  it('keeps consumer drafts visible when saving is rejected', async () => {
    const close = vi.fn()
    render(
      <ConsumerEditor
        consumer={consumer}
        pending={false}
        onSave={vi.fn().mockResolvedValue(false)}
        onClose={close}
      />
    )
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(screen.getByLabelText('Consumer name')).toHaveValue(
        'Finance desktop'
      )
    )
    expect(close).not.toHaveBeenCalled()
  })

  it('limits a new token to subscribed topics and passes a concrete expiry', async () => {
    const create = vi.fn().mockResolvedValue(true)
    render(
      <TokenEditor
        consumer={{ ...consumer, topics: [EVENT_TOPICS[2]] }}
        hidden={false}
        pending={false}
        onClose={vi.fn()}
        onCreate={create}
      />
    )
    const user = userEvent.setup()
    expect(
      screen.queryByRole('checkbox', { name: 'Wallet top-up succeeded' })
    ).not.toBeInTheDocument()
    await user.type(screen.getByLabelText('Token name'), 'Operations')
    await user.click(screen.getByRole('button', { name: 'Create event token' }))
    await waitFor(() =>
      expect(create).toHaveBeenCalledWith({
        name: 'Operations',
        topics: [EVENT_TOPICS[2]],
        expires_at: expect.any(Number),
      })
    )
    expect(create.mock.calls[0][0].expires_at).toBeGreaterThan(
      Math.floor(Date.now() / 1000)
    )
  })

  it('preserves edited settings during background refresh and submits pause explicitly', async () => {
    const save = vi.fn().mockResolvedValue(true)
    const view = render(
      <ServiceSettings settings={settings} pending={false} onSave={save} />
    )
    const user = userEvent.setup()
    const days = screen.getByRole('spinbutton', {
      name: 'Event retention (days)',
    })
    await user.clear(days)
    await user.type(days, '14')
    view.rerender(
      <ServiceSettings
        settings={{ ...settings, retention_days: 9 }}
        pending={false}
        onSave={save}
      />
    )
    expect(days).toHaveValue(14)
    await user.click(
      screen.getByRole('switch', { name: 'Enable event delivery' })
    )
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() =>
      expect(save).toHaveBeenCalledWith({
        ...settings,
        enabled: false,
        retention_days: 14,
      })
    )
  })

  it('requires confirmation before revoking a token or replaying a notification', async () => {
    const revoke = vi.fn().mockResolvedValue(true)
    const replay = vi.fn().mockResolvedValue(true)
    render(
      <ConsumerDetails
        detail={detail}
        pending={false}
        onCreateToken={vi.fn()}
        onRevoke={revoke}
        onReplay={replay}
      />
    )
    const user = userEvent.setup()
    expect(screen.getByText('Awaiting acknowledgment')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Revoke' }))
    expect(revoke).not.toHaveBeenCalled()
    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Continue',
      })
    )
    await waitFor(() => expect(revoke).toHaveBeenCalledWith(9))
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    await user.click(screen.getByRole('button', { name: 'Replay event' }))
    expect(replay).not.toHaveBeenCalled()
    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Continue',
      })
    )
    await waitFor(() => expect(replay).toHaveBeenCalledWith(42))
  })

  it('shows empty consumers from a validated response and reports an API rejection', async () => {
    const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/event-hub') {
        return {
          data: {
            success: true,
            data: { settings, topics: [], consumers: [], events_count: 0 },
          },
        }
      }
      return { data: { success: true, data: { items: [], total: 0 } } }
    })
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = render(
      <QueryClientProvider client={client}>
        <EventHubSection />
      </QueryClientProvider>
    )
    expect(await screen.findByText('No consumers yet')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Create consumer' })
    ).toBeEnabled()
    get.mockResolvedValue({
      data: { success: false, message: 'Event access rejected' },
    })
    await client.invalidateQueries({ queryKey: ['event-hub'] })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Retry' })).toBeVisible()
    )
    view.unmount()
    client.clear()
  })

  it('creates a token through bound verification and removes the one-time secret on close', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/event-hub')
        return {
          data: {
            success: true,
            data: {
              settings,
              topics: [],
              consumers: [consumer],
              events_count: 1,
            },
          },
        }
      if (url === '/api/event-hub/consumers/7')
        return { data: { success: true, data: detail } }
      if (url === '/api/verify/methods')
        return {
          data: {
            success: true,
            data: {
              scope: 'event_hub.token',
              methods: [{ method: 'session', available: true }],
              oauth_providers: [],
              password_encryption_enabled: false,
            },
          },
        }
      return { data: { success: true, data: { items: [], total: 0 } } }
    })
    const post = vi.spyOn(api, 'post').mockImplementation(async (url) => {
      if (url === '/api/verify')
        return {
          data: {
            success: true,
            data: {
              proof_token: 'proof-fixture',
              scope: 'event_hub.token',
              method: 'session',
              expires_at: 2000000000,
            },
          },
        }
      return {
        data: {
          success: true,
          data: { token: 'evt_one_time_fixture', item: detail.tokens[0] },
        },
      }
    })
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = render(
      <QueryClientProvider client={client}>
        <EventHubSection />
      </QueryClientProvider>
    )
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Manage' }))
    await user.click(
      await screen.findByRole('button', { name: 'Create event token' })
    )
    await user.type(screen.getByLabelText('Token name'), 'Second desktop')
    await user.click(
      within(screen.getByRole('dialog')).getByRole('button', {
        name: 'Create event token',
      })
    )
    expect(await screen.findByText('evt_one_time_fixture')).toBeVisible()
    expect(post).toHaveBeenCalledWith(
      '/api/verify',
      expect.objectContaining({
        scope: 'event_hub.token',
        context: expect.objectContaining({
          consumer_id: 7,
          topics: [...EVENT_TOPICS].sort(),
        }),
      }),
      expect.any(Object)
    )
    expect(post).toHaveBeenCalledWith(
      '/api/event-hub/consumers/7/tokens',
      expect.objectContaining({ name: 'Second desktop' }),
      expect.objectContaining({
        singleUseAuthorization: true,
        headers: { 'X-Security-Proof': 'proof-fixture' },
      })
    )
    await user.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() =>
      expect(screen.queryByText('evt_one_time_fixture')).not.toBeInTheDocument()
    )
    view.unmount()
    client.clear()
  })

  it('shows expired authorization and expired unacknowledged totals without a funds retry action', () => {
    render(
      <ConsumerDetails
        detail={{
          ...detail,
          consumer: {
            ...consumer,
            auth_valid: false,
            expired_unacked_count: 2,
          },
        }}
        pending={false}
        onCreateToken={vi.fn()}
        onRevoke={vi.fn()}
        onReplay={vi.fn()}
      />
    )
    expect(screen.getAllByRole('alert')).toHaveLength(2)
    expect(
      screen.getByRole('button', { name: 'Create event token' })
    ).toBeDisabled()
    expect(
      screen.queryByRole('button', { name: /charge|payment|funds/i })
    ).not.toBeInTheDocument()
  })
})
