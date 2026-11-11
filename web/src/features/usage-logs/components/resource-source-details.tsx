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

// 本组件只解释当次来源与回退记录，不用当前策略重算历史调度。
// 账单可能已从首选挂靠回退钱包，实际来源不能从第一项原因推断。
// 用户切换语言时重新生成标签，日志数据本身保持不变。
// 平台来源中的编号为零属于旧编码格式，不能把它解释成“零号账号”。
// 回退说明只覆盖发送前跳过的事实，不暗示已经发送的请求被重新执行。
// 不提供一键重试，查看日志不能产生新的模型请求或消费。
// 账号信息不可用不等于已删除，缺失时不对生命周期作未经证实的判断。
// 字典查找只接受自身键，未知代码不能命中 JavaScript 原型属性。
// 来源名和原因文案分别翻译，避免用字符串替换去猜测业务含义。
// 保留策略版本便于排障，它不参与账号显示名称的构造。
// 不展示原厂错误正文，避免把凭据、请求内容或内部地址带入原因说明。
// 当前版本不认识的原因保留可见提示，不直接输出代号或假称额度耗尽。
// 缺少来源字段的旧日志省略此区域，不凭零费用猜测执行来源。
// 账号备注仅用于辨认，不作为身份验证、路由选择或权限判断依据。
// 账号编号仅用于关联服务端返回的显示投影，不直接展示给用户。
// 投影包含当前备注与已脱敏邮箱，不能冒充历史名称快照。
// 身份由后端按日志所有者过滤，页面不再请求账号管理接口。
// 已解绑账号仍可通过服务端墓碑辨认；找不到时明确说明信息不可用。
// 原始原因码保留在日志中，界面以当前语言解释，未知码不伪造具体原因。
// Token 用量归属详情的 Token 明细，本区只说明资源来源和执行结果。
// 平台零费用不意味着原厂未耗额，本组件不计算或更改账单。
export function ResourceSourceDetails(props: { other: LogOtherData | null }) {
  const { t } = useTranslation()
  const other = props.other
  if (!other?.resource_source) return null
  // 显式覆盖三类来源，名称与其他日志信息保持当前语言一致。
  // 未知来源不会把后端枚举直接展示给普通用户。
  const labels: Record<string, string> = {
    hosting: t('Account hosting'),
    subscription: t('Subscription quota'),
    wallet: t('Wallet balance'),
  }
  // 两个位置共用账号显示规则，备注和邮箱至少一项存在才算可识别。
  // React 文本插值保持转义，用户自定义备注不能作为 HTML 渲染。
  // 空备注时仍可用邮箱辨认，两个字段均缺失则明确显示信息不可用。
  // 不把 ID 拼到缺失提示中，避免再次退回只有编号的界面。
  // 使用服务端返回的掩码，不尝试从日志其他字段推导完整邮箱。
  const accountName = (id: string) => {
    const account = other.hosting_accounts?.[id]
    if (account?.alias && account.email_mask) {
      return `${account.alias} (${account.email_mask})`
    }
    return (
      account?.alias ||
      account?.email_mask ||
      t('Account information unavailable')
    )
  }
  // 原因顺序与历史尝试一致，不排序、不去重，也不展示编号或裸业务码。
  // 耗尽对三种来源含义不同，订阅无剩余额度不应写成钱包余额不足。
  const quotaReasons: Record<string, string> = {
    hosting: t('Account quota is exhausted'),
    subscription: t('No available subscription quota'),
    wallet: t('Wallet balance is insufficient'),
  }
  // 分组权限不足不能用调整资源余额解决，须与额度耗尽分开说明。
  // 来源不支持模型时只解释此次跳过，不向用户承诺后续来源必然成功。
  const reasonLabels: Record<string, string> = {
    account_unavailable: t('Account is not currently available for requests'),
    // 限流与额度耗尽分开表达；执行层可能没有向原厂发送。
    rate_limited: t(
      'Hosted account was rate-limited or cooling down; the request was not processed'
    ),
    model_unavailable: t('This resource does not support the requested model'),
    group_forbidden: t('The current API key cannot use this group'),
  }
  // 保留按尝试顺序连接的说明，多个失败来源不会互相覆盖。
  // 模型不可用只是该来源不支持，不代表整个平台没有这个模型。
  // 账号暂不可用也不猜测具体是暂停还是验证失败，旧码没有更细信息。
  const reasons = (other.resource_skip_reasons ?? []).map((reason) => {
    const [source, id, code] = reason.split(':')
    const sourceLabel = Object.hasOwn(labels, source)
      ? labels[source]
      : undefined
    const label = source === 'hosting' ? accountName(id) : sourceLabel
    const description =
      code === 'quota_exhausted' && Object.hasOwn(quotaReasons, source)
        ? quotaReasons[source]
        : Object.hasOwn(reasonLabels, code) && reasonLabels[code]
    // 新版本或损坏的原因记录仍可见，但不将不可读代号当作问题说明。
    const message =
      description ||
      t('This resource was skipped; the specific reason is unavailable')
    return label ? `${label}: ${message}` : message
  })
  // section 的可访问名称供读屏定位；长账号和原因允许在窄屏折行。
  // 详情中的 Token 明细继续负责已知统计和未知提示，避免重复解释。
  return (
    <section
      aria-label={t('Actual resource source')}
      className='space-y-1 rounded border p-3 text-sm break-words'
    >
      <p>
        {t('Actual resource source')}:{' '}
        {Object.hasOwn(labels, other.resource_source)
          ? labels[other.resource_source]
          : t('Unknown resource source')}
      </p>
      {/* 实际使用挂靠时展示账号身份，缺少旧记录也不回退成编号。 */}
      {other.resource_source === 'hosting' && (
        <p>
          {t('Hosted account')}:{' '}
          {accountName(String(other.hosting_account_id ?? ''))}
        </p>
      )}
      {/* 策略版本是诊断元信息，仍保持原始精度与格式。 */}
      {other.resource_policy_revision !== undefined && (
        <p>
          {t('Resource policy revision')}: {other.resource_policy_revision}
        </p>
      )}
      {reasons.length > 0 && (
        <p>
          {t('Fallback reasons')}: {reasons.join(' → ')}
        </p>
      )}
      {/* 尝试不是额外请求；身份用于关联执行层日志，保留原顺序。 */}
      {/* 不声称已经耗尽额度，也不从历史拒绝推断最终消费金额。 */}
      {(other.resource_attempts?.length ?? 0) > 0 && (
        <div>
          <p>{t('Earlier resource attempts (part of this request)')}</p>
          {other.resource_attempts?.map((attempt) => (
            // 策略校验禁止账号重复，每个账号在该请求中最多尝试一次。
            <p key={attempt.account_id}>
              {accountName(String(attempt.account_id))}:{' '}
              {t(
                'Hosted account was rate-limited or cooling down; the request was not processed'
              )}
              {` (HTTP ${attempt.status_code})`}
              {/* 执行层 ID 只是诊断元信息，不触发网络查询或重试。 */}
              {attempt.upstream_request_id && (
                <>
                  {' '}
                  · {t('Upstream Request ID')}: {attempt.upstream_request_id}
                </>
              )}
            </p>
          ))}
        </div>
      )}
      {/* 执行结果未知与 Token 用量未知独立，不能相互替代。 */}
      {other.execution_status === 'outcome_unknown' && (
        <p role='status'>{t('Request outcome is unknown')}</p>
      )}
    </section>
  )
}
