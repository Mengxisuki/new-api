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
import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AccountQuota } from '../components/account-quota'

// 验证用户只看到额度和重置权益，不出现积分、费用或货币余额。
// 这些测试保护额度展示语义，不测 CSS 像素或私有 state。
// 直接渲染真实组件，不 mock 格式化或翻译函数。
// 观测快照是接口已校验后的最小业务输入。
// 每个场景只设置决定该语义的字段，避免无关完整账号 fixture。
// 测试运行环境使用英文翻译键，断言关注稳定的语义标签。
// 不发网络请求，也不构造真正原厂凭据。
// 主快照成功和子查询失败可以同时发生，不能合并成一个布尔值。
// 第一场景故意保留非零历史卡数，保护旧数据不会伪装当前可用。
// 即使接口仍返回积分余额，挂靠界面也不展示它。
// 金额过滤属于展示合同，不改变上游数据契约。
// 第二场景的零是已确认数据，必须区别于缺少次数字段。
// 如果实现改用真假值判断，这个断言会捕获零被错误隐藏。
// 第三场景不传快照，用于保护首次接入尚未观测状态。
// 无观测时不能绘制零百分比，因为零百分比暗示额度完整。
// 使用 progressbar 角色而非 class 定位，允许视觉样式正常演进。
// 未知说明是用户决策依据，不能仅留一个空白组件。
// 测试没有等待异步完成，因为被测组件是纯展示。
// 上游是否可访问与采样准确性由执行层测试负责。
// 原厂额度不能换算成人民币，此处只断言原始显示事实。
// 跨账号聚合不属于此组件，fixture 始终只表示一个账号。
// 测试清理由公共测试环境完成，场景之间不共享 DOM。
// 不把测试中的历史卡数当成真实可消费权益。
// 后续新增窗口种类应继续保持未知和零的区分。
// 这些展示断言不能作为真实原厂额度查询已经验收的证据。
// 第一条沿用上游积分字段，防止后续组件改版重新带回余额区域。
// 不通过从 fixture 删除敏感展示字段来让隐藏金额测试假通过。
// 同时保留历史机会数，验证子查询失败不会泄露旧权益为当前值。
// 不要求主查询失败，才能复现真实的部分成功结果。
// 零机会使用新鲜子状态，排除因失败而显示未知的正确行为。
// 没有快照时也不显示默认套餐，不替用户猜测账号级别。
// 断言使用 DOM 可见文字，与组件实际产品展示保持一致。
describe('逐账号额度', () => {
  it('主额度新鲜但重置卡子查询失败时仍显示未知', () => {
    render(
      <AccountQuota
        observation={{
          state: 'fresh',
          snapshot: {
            reset_credits_available: 4,
            reset_credit_observation: { state: 'unavailable' },
            credits: { balance: '0.50' },
          },
        }}
      />
    )
    expect(screen.getByText('Reset opportunities: Unknown')).toBeInTheDocument()
    expect(screen.queryByText(/Codex credits|0\.50/)).not.toBeInTheDocument()
  })
  it('已确认的零次机会保留为零，而不是未知或无限', () => {
    render(
      <AccountQuota
        observation={{
          state: 'fresh',
          snapshot: {
            reset_credits_available: 0,
            reset_credit_observation: { state: 'fresh' },
          },
        }}
      />
    )
    expect(screen.getByText('Reset opportunities: 0')).toBeInTheDocument()
  })
  it('额度缺失时没有伪造进度条且保留未知说明', () => {
    render(<AccountQuota />)
    expect(screen.getByText('Quota is not available yet')).toBeInTheDocument()
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  })
})

// 固定外部时间，验证倒计时变化而不等待真实一分钟。
// 第一场景选用非零已用量，避免剩余值与已用值偶然相等。
// 一分钟后跨过重置边界，进度仍应保持旧值而不归零。
// 查询节点仍是原组件，未重新渲染 props 来假造刷新成功。
// 当前状态已明确 available，因此不应另外显示未知状态警告。
// aria-valuenow 与可见百分比同时断言，防止辅助技术读到相反含义。
// 最初的时间值来自同一受控时钟，不依赖测试运行机器的时区。
// 倒计时结束后只检查文案，不假设原厂重置点的真实精度。
// 原厂网络和权限校验属于执行层，不在本文件模拟消费动作。
// 历史窗口使用缺失利用率的输入，避免仅检查警告但忽略假进度。
// 长标签是公开产品名称，不包含邮箱或用户备注等隐私。
// 额度百分比使用原厂 utilization，不能把 remaining 当成已用。
// 过了重置点仍保留旧进度，等待用户查询真实状态。
// 定时器只刷新文案，不能替用户发起原厂请求。
// 多窗口按服务端顺序显示，不能暗示额外的路由顺序。
// 当前可用状态不再泄露英文 available 或 percent 枚举。
// 未知状态保留说明，不绘制伪造的零进度。
// 清理时恢复时钟，避免影响同一进程中的其他测试。
describe('紧凑额度窗口', () => {
  afterEach(() => vi.useRealTimers())
  it('同一行展示已用百分比及倒计时，到期后提示待刷新', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-11-02T00:00:00Z'))
    render(
      <AccountQuota
        observation={{
          state: 'fresh',
          snapshot: {
            quota_windows: [
              {
                id: '5h',
                label: '5h',
                unit: 'percent',
                state: 'available',
                utilization: 0.72,
                remaining: 28,
                reset_at_ms: Date.now() + 60_000,
              },
            ],
          },
        }}
      />
    )
    expect(screen.getByRole('progressbar', { name: '5h' })).toHaveAttribute(
      'aria-valuenow',
      '72'
    )
    // 短条使用固定宽度，避免重新拉伸成占满整列的详情进度条。
    // 窗口允许内部折行，窄屏不会挤压出横向滚动。
    expect(screen.getByRole('progressbar')).toHaveClass('w-16')
    expect(screen.getByText('5h').parentElement).toHaveClass('flex-wrap')
    expect(screen.getByText('72%')).toBeInTheDocument()
    expect(screen.getByText('Resets in 1m')).toBeInTheDocument()
    expect(screen.queryByText(/available|percent|28/)).not.toBeInTheDocument()
    act(() => vi.advanceTimersByTime(60_000))
    expect(screen.getByText('Awaiting quota refresh')).toBeInTheDocument()
    expect(screen.getByRole('progressbar')).toHaveAttribute(
      'aria-valuenow',
      '72'
    )
  })
  // 主观测过期与窗口未知应同时保留，不能用颜色代替事实。
  // 长窗口标签允许换行，固定宽度进度条不挤占整行。
  it('历史快照和未知窗口显示明确说明，不猜测可用额度', () => {
    render(
      <AccountQuota
        observation={{
          state: 'stale',
          snapshot: {
            quota_windows: [
              {
                id: 'unknown',
                label: 'Special model window',
                unit: 'percent',
                state: 'unknown',
              },
            ],
          },
        }}
      />
    )
    expect(
      screen.getByText('Quota may be stale or unavailable')
    ).toBeInTheDocument()
    expect(screen.getByText('Remaining quota unknown')).toBeInTheDocument()
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  })
})
