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
import { createInstance } from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { describe, expect, it } from 'vitest'

import en from '@/i18n/locales/en.json'
import fr from '@/i18n/locales/fr.json'
import ja from '@/i18n/locales/ja.json'
import ru from '@/i18n/locales/ru.json'
import vi from '@/i18n/locales/vi.json'
import zhTW from '@/i18n/locales/zh-TW.json'
import zh from '@/i18n/locales/zh.json'

import { ResourceSourceDetails } from '../resource-source-details'

// 直接渲染用户详情组件，输入脱敏日志事实，不模拟组件内部状态。
// 回退到钱包的日志必须解释实际扣费来源，而非只显示首选挂靠。
// 第一条 fixture 选择钱包而非挂靠，覆盖实际发生回退的用户困惑。
// 回退原因仍是挂靠耗尽，来源和原因必须同时表达完整经过。
// 原始编号和代号留在日志数据中，界面只展示可识别账号和原因。
// 日志策略版本包含默认层级前缀，保护不会被误转成数字。
// 版本零也是合法历史版本，界面不能用真假值判断隐藏它。
// 未知用量与未知执行结果同时出现，来源区只保留执行结果提示。
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
// 原因码使用后端真实格式，不携带用户提示词或真实原厂错误正文。
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
          resource_skip_reasons: ['hosting:2:quota_exhausted'],
          hosting_accounts: {
            '2': { alias: 'Work', email_mask: 'w***@example.com' },
          },
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
      screen.getByText(
        'Fallback reasons: Work (w***@example.com): Account quota is exhausted'
      )
    ).toBeInTheDocument()
    expect(
      screen.queryByText('Provider token usage is unknown')
    ).not.toBeInTheDocument()
    expect(screen.getByText('Request outcome is unknown')).toBeInTheDocument()
  })
  // 没有来源字段的历史数据不能自动推断成钱包或挂靠。
  it('旧日志不凭零费用猜测资源来源', () => {
    render(<ResourceSourceDetails other={{}} />)
    expect(screen.queryByRole('region')).not.toBeInTheDocument()
  })
})

// 身份展示不依赖账号是否仍在调度池，后端提供墓碑信息也必须正常显示。
// 缺少身份时明确提示，不能退回只有用户无法识别的内部编号。
// 备注按普通文本渲染，不能把用户输入当作网页标记。
it.each([
  [
    { alias: 'Research', email_mask: 'r***@example.com' },
    'Research (r***@example.com)',
  ],
  [{ alias: '', email_mask: 'r***@example.com' }, 'r***@example.com'],
  [undefined, 'Account information unavailable'],
])('挂靠来源显示可识别身份 %s', (account, expected) => {
  render(
    <ResourceSourceDetails
      other={{
        resource_source: 'hosting',
        hosting_account_id: 42,
        hosting_accounts: account ? { '42': account } : undefined,
      }}
    />
  )
  expect(screen.getByText(`Hosted account: ${expected}`)).toBeVisible()
  expect(screen.queryByText(/Hosted account ID|42/)).toBeNull()
})

// 后端当前会生成四类原因，额度不足还须分别解释钱包和订阅。
// 未识别码也必须有可读说明，不能泄露未加工代号或编造耗尽原因。
// 按可见文本验证，翻译文案与原因顺序同属公共展示合同。
it.each([
  ['wallet:0:quota_exhausted', 'Wallet balance is insufficient'],
  ['subscription:0:quota_exhausted', 'No available subscription quota'],
  [
    'hosting:2:account_unavailable',
    'Account is not currently available for requests',
  ],
  [
    'hosting:2:model_unavailable',
    'This resource does not support the requested model',
  ],
  ['wallet:0:group_forbidden', 'The current API key cannot use this group'],
  [
    'hosting:2:new_reason',
    'This resource was skipped; the specific reason is unavailable',
  ],
  [
    'legacy_code',
    'This resource was skipped; the specific reason is unavailable',
  ],
])('回退原因 %s 显示自然语言', (reason, expected) => {
  render(
    <ResourceSourceDetails
      other={{ resource_source: 'wallet', resource_skip_reasons: [reason] }}
    />
  )
  expect(screen.getByText(new RegExp(expected))).toBeVisible()
  expect(screen.queryByText(new RegExp(reason))).toBeNull()
})

// 真正加载语言资源，避免仅用英文 key 断言掩盖中文缺失。
// 同一组件切换语言仍保留账号身份，只重新翻译原因和标签。
// 这条回归对应用户截图中的挂靠耗尽后回退钱包路径。
it('中文回退原因可读且响应语言切换', async () => {
  const locale = createInstance()
  await locale
    .use(initReactI18next)
    .init({
      lng: 'zhCN',
      fallbackLng: 'en',
      resources: { en, zhCN: zh },
      nsSeparator: false,
    })
  render(
    <I18nextProvider i18n={locale}>
      <ResourceSourceDetails
        other={{
          resource_source: 'wallet',
          resource_skip_reasons: ['hosting:2:quota_exhausted'],
          hosting_accounts: {
            '2': { alias: '科研账号', email_mask: 'lab***@example.com' },
          },
        }}
      />
    </I18nextProvider>
  )
  expect(
    screen.getByText('回退原因: 科研账号 (lab***@example.com): 账号额度已用完')
  ).toBeVisible()
  await act(() => locale.changeLanguage('en'))
  expect(
    screen.getByText(
      'Fallback reasons: 科研账号 (lab***@example.com): Account quota is exhausted'
    )
  ).toBeVisible()
})

// 七语言都在 translation 命名空间中维护，不用页面英文回退冒充翻译。
// 稳定英文键也是接口旧码与界面文案之间的维护边界。
it('七语言均包含账号与回退说明', () => {
  for (const resource of [en, zh, zhTW, fr, ja, ru, vi]) {
    for (const key of [
      'Hosted account',
      'Account information unavailable',
      'Account quota is exhausted',
      'No available subscription quota',
      'Wallet balance is insufficient',
      'Account is not currently available for requests',
      'This resource does not support the requested model',
      'The current API key cannot use this group',
      'This resource was skipped; the specific reason is unavailable',
      'Unknown resource source',
    ]) {
      expect(
        resource.translation[key as keyof typeof resource.translation]
      ).toBeTruthy()
    }
  }
})
