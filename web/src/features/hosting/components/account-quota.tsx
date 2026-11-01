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

import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { HostingObservation } from '../types'

// 列表与详情共用额度投影，保持未知、零和过期数据的语义一致。
// 不计算跨账号总百分比，也不把原厂额度换算成平台人民币金额。
// 本组件只负责展示观测事实，不发起刷新或消费请求。
// 列表摘要和详情查询可以使用同一份数据契约。
// 没有观测与观测失败分别保留服务器提供的信息。
// 原厂窗口可能同时包含短周期、长周期或模型专属窗口。
// 这些窗口相互约束，不是能汇总成一个总余额的分账户。
// 剩余数量、利用率和重置时间独立存在，不能互相反推。
// 窗口没有百分比时不显示空进度条，避免暗示额度充足。
// 显示进度限制到合理视觉范围，但不修改原始剩余数值。
// 窗口名称保留原厂语义，不从标签推算固定七天周期。
// 用户切换界面语言后日期和数字立即重新格式化。
// 语言码先经过公共转换，兼容简体和繁体的内部名称。
// 所有日期按浏览器时区展示，不混淆服务端 UTC 时间。
// 重置机会是原厂一次性权益，不是平台钱包余额。
// 重置机会子查询失败时不能借主额度的新鲜度显示旧次数。
// 真实零次数应展示零，不能使用真假值判断丢失该事实。
// 有到期时间的机会逐一提示，重复时间合并不改变可用总数。
// 只返回卡子明细而没有总数时仍显示次数未知。
// 原厂积分余额保留小数字符串，不做人民币汇率换算。
// 缺少积分余额不能根据 unlimited 或 has_credits 编造数值。
// 额度过期不代表账号被解绑，用户仍可主动刷新或重新授权。
// 此处不以错误码直接触发任何补救操作。
// 错误码只用于排查，不展示原厂返回的完整响应。
// 时间戳为零仍是明确值，存在性检查不能写成真假判断。
// 重置成功但额度同步中时由详情页提供额外提示。
// 历史快照可继续显示，但必须同时保留过期状态标记。
// 展示历史值不意味着允许消费，操作权限由详情重新判断。
// 窗口顺序来自上游，避免前端重排让用户误解优先级。
// 进度条可访问名称使用窗口名，屏幕阅读器可区分多个窗口。
// 同一个账号的多个额度也不相加，避免双重计算可用量。
// 长窗口名称允许换行，不通过截断隐去额度约束。
export function AccountQuota(props: {
  observation?: HostingObservation | null
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const observation = props.observation
  // 快照可能不存在，即使顶层观测对象已经创建。
  // 该情况属于数据尚未取得，不应绘制一个空的正常窗口。
  // 所有可选属性都按存在性使用，保留零和空值的业务区别。
  // 数字格式化只用于显示，不回写服务端原始额度。
  const snapshot = observation?.snapshot
  const date = (value: number) => new Date(value).toLocaleString(locale)
  // 观测不是 fresh 时仍可显示历史值，但必须显式标注，不能伪装实时。
  const fresh = observation?.state === 'fresh'
  // 卡子状态独立于主快照，主额度查询成功不保证卡子查询成功。
  // 只有两层状态都新鲜，显示的次数才可以解释为当前可用次数。
  // 这与消费按钮的门槛保持同样语义，但本组件不授予操作权限。
  const resetFresh =
    fresh && snapshot?.reset_credit_observation?.state === 'fresh'
  return (
    <div className='min-w-0 space-y-3 text-sm'>
      {!observation && <p>{t('Quota is not available yet')}</p>}
      {observation && (
        <Badge variant={fresh ? 'secondary' : 'outline'}>
          {fresh
            ? t('Latest observation')
            : t('Quota may be stale or unavailable')}
        </Badge>
      )}
      {snapshot?.plan_summary?.name && <p>{snapshot.plan_summary.name}</p>}
      {snapshot?.quota_windows?.map((window) => (
        <div key={window.id} className='space-y-1'>
          <div className='flex flex-wrap justify-between gap-1'>
            <span>{window.label}</span>
            <span>{window.state}</span>
          </div>
          {/* utilization 仅在确有数值时绘制；绝不把缺少额度当作未使用。 */}
          {window.utilization !== undefined && (
            <Progress
              value={Math.min(100, Math.max(0, window.utilization * 100))}
              aria-label={window.label}
            />
          )}
          <p className='text-muted-foreground text-xs'>
            {window.remaining !== undefined
              ? t('Remaining: {{value}} {{unit}}', {
                  value: formatNumber(window.remaining, locale),
                  unit: window.unit,
                })
              : t('Remaining quota unknown')}
            {window.reset_at_ms !== undefined && (
              <>
                {' '}
                · {t('Resets at {{time}}', { time: date(window.reset_at_ms) })}
              </>
            )}
          </p>
        </div>
      ))}
      {/* 重置卡详情具有独立新鲜度，不借主额度成功掩盖子查询失败。 */}
      <p>
        {t('Reset opportunities')}:{' '}
        {resetFresh && snapshot?.reset_credits_available !== undefined
          ? formatNumber(snapshot.reset_credits_available, locale)
          : t('Unknown')}
      </p>
      {[
        ...new Set(
          snapshot?.reset_credits?.flatMap((credit) =>
            credit.expires_at_ms === undefined ? [] : [credit.expires_at_ms]
          ) ?? []
        ),
      ].map((expires) => (
        <p key={expires} className='text-muted-foreground text-xs'>
          {t('Expires at {{time}}', { time: date(expires) })}
        </p>
      ))}
      {/* 积分不是重置卡次数；未返回 balance 时显示未知而非零。 */}
      {/*
       * 积分区和重置机会区分开，避免把可用积分当作可重置次数。
       * 没有提供余额时明确展示未知，服务端后来补全后自然更新。
       * 数值字符串不经浮点数转换，以免损失原厂小数精度。
       */}
      <p>
        {t('Codex credits')}: {snapshot?.credits?.balance ?? t('Unknown')}
      </p>
      {observation?.observed_at_ms !== undefined && (
        <p className='text-muted-foreground text-xs'>
          {t('Observed at {{time}}', {
            time: date(observation.observed_at_ms),
          })}
        </p>
      )}
      {observation?.last_error_code && (
        <p role='status' className='text-destructive text-xs'>
          {observation.last_error_code}
        </p>
      )}
    </div>
  )
}
