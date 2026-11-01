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
import { useTranslation } from 'react-i18next'

import type { LogOtherData } from '../types'

// 本组件只解释网关已经记录的事实，不根据当前策略重算历史路由。
// 旧日志没有资源来源字段时不补猜测值，避免误标为钱包付费。
// 挂靠日志的平台金额为零，但原厂消耗可能未知；两者不能混为一谈。
// 只展示脱敏账号 ID、策略版本和稳定原因码，不读取账号凭据。
// 展示字段来自当次请求日志，而不是当前用户配置。
// 用户后来调整优先级，不应改变旧日志的来源解释。
// 相同模型可能由不同来源执行，因此不能根据模型名推断来源。
// 平台零费用也可能是其他计费原因，不能仅凭 quota=0 判断挂靠。
// 未记录来源的旧日志完全省略本区块，保持历史数据边界。
// 来源名称随界面语言变化，稳定原因码保持可追溯。
// 账号 ID 是 Oak 本人的业务编号，不是原厂邮箱或上游组编号。
// 日志不会为显示账号名再发账号查询，避免解绑后丢失来源线索。
// 策略版本是带层级的字符串，不应转换成数字进行排序。
// 回退原因可能不止一个，顺序对应尝试经过。
// 已转发只说明请求进入上游，不能据此保证生成完整结束。
// 结果未知和用量未知是两种独立事实，可以同时出现。
// 未知用量提示不能把平台已确认费用也改成未知。
// 组件不计算任何账单，不修改既有费用显示和精度。
// 长原因码允许换行，手机详情不会因错误文本撑宽。
// 使用语义化 section 提供可访问名称，便于独立定位历史来源。
// 不把原厂响应内容、用户提示词或凭据附加到这块日志展示。
export function ResourceSourceDetails(props: { other: LogOtherData | null }) {
  const { t } = useTranslation()
  const other = props.other
  if (!other?.resource_source) return null
  // 枚举保留服务端值作为未知版本的可观测回退，不虚构已识别来源。
  const labels: Record<string, string> = {
    hosting: t('Account hosting'),
    subscription: t('Subscription quota'),
    wallet: t('Wallet balance'),
  }
  // 原因是按尝试顺序排列的历史记录；不做排序或去重改变含义。
  const reasons = other.resource_skip_reasons ?? []
  return (
    <section
      aria-label={t('Actual resource source')}
      className='space-y-1 rounded border p-3 text-sm'
    >
      <p>
        {t('Actual resource source')}:{' '}
        {labels[other.resource_source] ?? other.resource_source}
      </p>
      {/* 账号编号使用存在性判断，旧日志缺失时不补零号账号。 */}
      {other.hosting_account_id !== undefined && (
        <p>
          {t('Hosted account ID')}: {other.hosting_account_id}
        </p>
      )}
      {/* 版本零也有意义，不能用真假值过滤掉首个策略版本。 */}
      {other.resource_policy_revision !== undefined && (
        <p>
          {t('Resource policy revision')}: {other.resource_policy_revision}
        </p>
      )}
      {/* 回退原因来自服务端稳定业务码，React 转义避免注入页面。 */}
      {reasons.length > 0 && (
        <p className='break-words'>
          {t('Fallback reasons')}: {reasons.join(' → ')}
        </p>
      )}
      {/* 已转发不等于原厂完整完成，未知结果需要单独告知用户。 */}
      {other.execution_status === 'outcome_unknown' && (
        <p role='status'>{t('Request outcome is unknown')}</p>
      )}
      {/* 不借平台免费事实暗示原厂请求未耗额。 */}
      {other.usage_status === 'unknown' && (
        <p role='status'>{t('Provider token usage is unknown')}</p>
      )}
    </section>
  )
}
