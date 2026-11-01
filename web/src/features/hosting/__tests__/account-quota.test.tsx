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
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { AccountQuota } from '../components/account-quota'

// 只测用户看到的金额与未知状态，避免把上游缺失字段变成假零余额。
// 这些测试保护额度展示语义，不测 CSS 像素或私有 state。
// 直接渲染真实组件，不 mock 格式化或翻译函数。
// 观测快照是接口已校验后的最小业务输入。
// 每个场景只设置决定该语义的字段，避免无关完整账号 fixture。
// 测试运行环境使用英文翻译键，断言关注稳定的语义标签。
// 不发网络请求，也不构造真正原厂凭据。
// 主快照成功和子查询失败可以同时发生，不能合并成一个布尔值。
// 第一场景故意保留非零历史卡数，保护旧数据不会伪装当前可用。
// 独立积分余额应继续显示，卡子失败不应清空所有额度。
// 余额字符串的小数保真是原厂积分展示的一部分。
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
    expect(screen.getByText('Codex credits: 0.50')).toBeInTheDocument()
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
