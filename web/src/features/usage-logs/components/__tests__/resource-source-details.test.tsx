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

import { ResourceSourceDetails } from '../resource-source-details'

// 直接渲染用户详情组件，输入脱敏日志事实，不模拟组件内部状态。
// 回退到钱包的日志必须解释实际扣费来源，而非只显示首选挂靠。
// 第一条 fixture 选择钱包而非挂靠，覆盖实际发生回退的用户困惑。
// 回退原因仍是挂靠耗尽，来源和原因必须同时表达完整经过。
// 日志策略版本包含默认层级前缀，保护不会被误转成数字。
// 版本零也是合法历史版本，界面不能用真假值判断隐藏它。
// 未知用量与未知执行结果同时出现，保护两者没有互相覆盖。
// 测试不传账号编号，因为钱包执行不能伪造挂靠账号 ID。
// 使用真实翻译 hook，页面内容遵循当前测试语言资源。
// 断言可见文本而非 CSS class，允许日志详情布局继续演进。
// 组件只读取日志，不应发起当前账户或策略查询。
// 因此测试无网络 mock，任何新增网络依赖都需要重新审查职责。
// 第二条输入空对象模拟上线前已有日志。
// 旧记录没有来源字段，不应因为通常是钱包而补上钱包标签。
// 按语义区域查找，保护整块未知历史来源不被伪造展示。
// 没有把零费用直接加入 fixture，组件不应依赖费用推导来源。
// 用量未知与平台账单零额是不同事实，费用测试在原模块保持独立。
// 原因码使用固定测试字符串，不携带用户提示词或真实原厂错误正文。
// 该测试不检查后台来源决策是否正确，只验证事实被准确呈现。
// 路由选择和费用结算必须由后端行为测试另行保护。
// 测试使用公共组件 props，未引用内部字段计算或复制格式化逻辑。
describe('历史资源来源', () => {
  it('钱包回退显示实际来源、策略版本和未决结果', () => {
    render(
      <ResourceSourceDetails
        other={{
          resource_source: 'wallet',
          resource_policy_revision: 'user:0',
          resource_skip_reasons: ['hosting_exhausted'],
          usage_status: 'unknown',
          execution_status: 'outcome_unknown',
        }}
      />
    )
    expect(
      screen.getByText('Actual resource source: Wallet balance')
    ).toBeInTheDocument()
    expect(
      screen.getByText('Resource policy revision: user:0')
    ).toBeInTheDocument()
    expect(
      screen.getByText('Fallback reasons: hosting_exhausted')
    ).toBeInTheDocument()
    expect(
      screen.getByText('Provider token usage is unknown')
    ).toBeInTheDocument()
    expect(screen.getByText('Request outcome is unknown')).toBeInTheDocument()
  })
  // 没有来源字段的历史数据不能自动推断成钱包或挂靠。
  it('旧日志不凭零费用猜测资源来源', () => {
    render(<ResourceSourceDetails other={{}} />)
    expect(screen.queryByRole('region')).not.toBeInTheDocument()
  })
})
