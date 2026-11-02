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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { HostingObservation } from '../types'

// 列表与详情共享额度语义，不把免费挂靠展示成可购买的余额。
// 只呈现窗口利用率、重置机会和时间，不渲染积分或任何金额。
// 百分比指已用比例，与进度条方向一致，不用剩余数值反推。
// 多个窗口彼此约束，不能相加，也不能按百分比排列消费顺序。
// 原厂窗口名称保留，模型专属窗口不能被缩写成普通周额度。
// 查询和消费由相邻动作组件处理，本展示组件不请求上游或消费权益。
// 组件输入来自已校验的 HostingObservation，不在每个元素重复解析。
// 快照可缺失，即使 observation 对象已经建立也不能默认额度完整。
// 账号状态与额度状态各有来源，额度刷新不会替代模型连接测试。
// 已连接、暂停、待验证等账号生命周期仍由外部账号列展示。
// 此处不根据余额或利用率决定账号是否进入 Key 的可用资源池。
// 任何刷新都不改变 Key 保存的资源顺序，也不触发平台扣费。
// 页面两处复用此组件，修改字段时需要同时考虑列表和详情。
// 服务端保留的 credits 字段用于接口兼容，不属于本组件展示范围。
// 不以隐藏数字、货币符号或缩写的方式重新展示相关积分数据。
// 窗口仍按照上游顺序呈现，以保留模型专属限制的相对关系。
// 已用百分比和机会次数是不同维度，不能合并为一个进度条。
// UI 仅描述观测时刻的事实，不保证下一次模型请求一定成功。
// 观测是否新鲜由执行层判定，浏览器不拿自己的时钟重定义 fresh。
// 浏览器时钟仅用于相对时间显示，不作为服务端路由或授权依据。
// 已到重置点但尚未观测的窗口继续显示历史百分比。
// 同一账号可能存在同时到期的窗口，各自保留原始重置点。
// 可用机会的消耗顺序由原厂负责，前端日期排序只是信息展示。
// 长错误码允许折行，不得通过省略号隐藏定位所需的错误类别。
// 原厂的重置窗口可能滚动变化，不能用固定周期推算下次时间。
// 缺少重置时间时只隐藏倒计时，不影响已确认的利用率显示。
// 原始剩余字段不用于补算利用率，避免不同单位的含义混用。
// 机会列表缺少总数时仍显示未知，不根据卡明细长度猜测次数。
// 状态或时间说明使用完整语义，辅助技术无需理解原厂枚举。
// 接口错误不会在这里触发重授权，用户应先查看详情确认原因。
export function AccountQuota(props: {
  observation?: HostingObservation | null
}) {
  const { t, i18n } = useTranslation()
  // 语言来自订阅式 hook，切换后数字和日期同时重新渲染。
  // 内部 zhCN、zhTW 必须先映射成 Intl 接受的语言标签。
  // 不把格式化后的文本持久化，避免换语言后残留旧展示。
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const observation = props.observation
  const snapshot = observation?.snapshot
  const fresh = observation?.state === 'fresh'
  // 主额度和重置机会的查询独立成功，不能借主状态展示旧卡数。
  // 明确零次必须显示零；缺失字段与过期快照均显示未知。
  // 此处的展示门槛不代替服务端的所有权和消费资格校验。
  const resetFresh =
    fresh && snapshot?.reset_credit_observation?.state === 'fresh'
  // 时间是组件唯一的本地状态，不复制服务端观测到本地 state。
  // 服务器刷新替换 props 后，利用率和机会次数直接反映新快照。
  const [now, setNow] = useState(Date.now)
  const hasDeadline = snapshot?.quota_windows?.some(
    (window) => window.reset_at_ms !== undefined
  )
  // 一个账号共用一个分钟时钟，不为每个窗口启动轮询。
  // 时钟只更新倒计时文案，绝不自动查询或恢复额度。
  // 切回浏览器时立即校准，避免后台节流造成过时提示。
  // 无重置时间时不启用时钟；卸载时同时清理定时器和监听。
  useEffect(() => {
    if (!hasDeadline) return
    const update = () => setNow(Date.now())
    const timer = window.setInterval(update, 60_000)
    document.addEventListener('visibilitychange', update)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', update)
    }
  }, [hasDeadline])
  // 所有完整日期使用浏览器时区，避免把服务端 UTC 当成本地钟点。
  // 时间戳为零也有含义，调用点一律用 undefined 判断缺失。
  const date = (value: number) => new Date(value).toLocaleString(locale)
  // 原始时间保留在 title，列表用短时间降低行高。
  // 倒计时到点只说明需要刷新，不能猜测原厂已经恢复额度。
  // 分钟向上取整，最后不足一分钟仍不提前显示已重置。
  // 天、小时、分钟模板均翻译；数字沿用项目统一格式化。
  const countdown = (deadline: number) => {
    // 先统一成整数分钟，再分解天与小时，避免四舍五入跨过重置点。
    // 到期提示没有修改快照，旧的耗尽百分比必须继续可见。
    const minutes = Math.ceil((deadline - now) / 60_000)
    if (minutes <= 0) return t('Awaiting quota refresh')
    let duration = t('{{minutes}}m', { minutes: formatNumber(minutes, locale) })
    if (minutes >= 1440) {
      duration = t('{{days}}d {{hours}}h', {
        days: formatNumber(Math.floor(minutes / 1440), locale),
        hours: formatNumber(Math.floor(minutes / 60) % 24, locale),
      })
    } else if (minutes >= 60) {
      duration = t('{{hours}}h {{minutes}}m', {
        hours: formatNumber(Math.floor(minutes / 60), locale),
        minutes: formatNumber(minutes % 60, locale),
      })
    }
    return t('Resets in {{duration}}', { duration })
  }
  // 相同到期点合并展示，不改变服务器提供的机会总数。
  // 最近到期日常驻一行，完整日期列表留在提示中供核对。
  // 过期主快照仍保留历史事实，但头部明确标注不是实时数据。
  // 排序只作用于独立的日期数组，不修改 props 内部明细。
  // 同一天的不同到期时刻仍保留在完整提示中，不能按日期字符串去重。
  // 过期快照中的到期日期保留为历史证据，不冒充当前可用权益。
  const expiries = [
    ...new Set(
      snapshot?.reset_credits?.flatMap((credit) =>
        credit.expires_at_ms === undefined ? [] : [credit.expires_at_ms]
      ) ?? []
    ),
  ].sort((a, b) => a - b)
  return (
    <div className='min-w-0 space-y-2 text-xs whitespace-normal'>
      {/* 套餐与观测状态压成一行，不把详情元数据逐段铺开。 */}
      {/* 无观测的说明与过期警告分开，便于用户判断是否需要查询。 */}
      {/* 成功观测只保留简短标签，完整观测时间放入原生提示。 */}
      {/* 原厂套餐名是身份信息，不增加费率、价格或预估消耗。 */}
      {/* 已用标题只在存在窗口时出现，避免空快照显示无意义列头。 */}
      <div className='text-muted-foreground flex flex-wrap items-center gap-x-2 gap-y-1'>
        {snapshot?.plan_summary?.name && (
          <Badge variant='outline'>{snapshot.plan_summary.name}</Badge>
        )}
        {!observation && <span>{t('Quota is not available yet')}</span>}
        {observation && (
          <span
            title={
              observation.observed_at_ms === undefined
                ? undefined
                : t('Observed at {{time}}', {
                    time: date(observation.observed_at_ms),
                  })
            }
          >
            {fresh
              ? t('Latest observation')
              : t('Quota may be stale or unavailable')}
          </span>
        )}
        {Boolean(snapshot?.quota_windows?.length) && (
          <span>{t('Quota used')}</span>
        )}
      </div>
      {/* 窗口间距小于账号间距，使同一账号的多个限制组成清晰整体。 */}
      {/* 每个窗口独占起始行，窄屏只在该窗口内部折行。 */}
      {/* 保持普通文档流，长名称或大字号不会被固定行高裁切。 */}
      <div className='space-y-1.5'>
        {snapshot?.quota_windows?.map((window) => {
          // 只限制进度条的视觉范围，百分比文本保留真实超额值。
          // 未提供利用率就不画空条，避免暗示未使用或无限额度。
          // 低余量用警示色，耗尽同时有文字，不能只靠颜色判断。
          const percent =
            window.utilization === undefined
              ? undefined
              : window.utilization * 100
          const exhausted = window.state === 'exhausted'
          return (
            <div
              key={window.id}
              className='flex flex-wrap items-center gap-x-2 gap-y-1'
            >
              {/* 长模型名可换行；短标签有统一最小宽度，方便纵向扫描。 */}
              <span className='bg-muted max-w-32 min-w-8 rounded px-1.5 py-0.5 text-center font-medium break-words'>
                {window.label}
              </span>
              {percent === undefined ? (
                <span className='text-muted-foreground'>
                  {t('Remaining quota unknown')}
                </span>
              ) : (
                <>
                  {/* 使用共享 Progress，保留数值和可访问名称的标准语义。 */}
                  {/* 条宽固定以便比较多个窗口，不能随百分比改变整个控件宽度。 */}
                  {/* 文本保留两位小数；0% 代表真实零用量，不代表缺省值。 */}
                  {/* 条填满后仍显示原始百分比，避免把超额值伪装成恰好耗尽。 */}
                  <Progress
                    value={Math.min(100, Math.max(0, percent))}
                    aria-label={window.label}
                    className={cn(
                      'w-16 shrink-0 [&_[data-slot=progress-track]]:h-1.5',
                      percent >= 75 &&
                        '[&_[data-slot=progress-indicator]]:bg-amber-500',
                      (percent >= 90 || exhausted) &&
                        '[&_[data-slot=progress-indicator]]:bg-destructive'
                    )}
                  />
                  <span className='w-11 shrink-0 text-right font-medium tabular-nums'>
                    {formatNumber(percent, locale)}%
                  </span>
                </>
              )}
              {exhausted && (
                <span className='text-destructive'>{t('Quota exhausted')}</span>
              )}
              {/* available 和 percent 是协议枚举，不作为界面文案原样显示。 */}
              {/* 非可用状态仍明确说明，不能让历史进度暗示当前可调用。 */}
              {window.state !== 'available' &&
                !exhausted &&
                percent !== undefined && (
                  <span className='text-muted-foreground'>
                    {t('Quota status unknown')}
                  </span>
                )}
              {/* 倒计时不使用 aria-live，避免每分钟打断屏幕阅读器。 */}
              {/* 完整重置点与相对时长共用同一字段，不能出现两套时间来源。 */}
              {window.reset_at_ms !== undefined && (
                <span
                  className='text-muted-foreground tabular-nums'
                  title={t('Resets at {{time}}', {
                    time: date(window.reset_at_ms),
                  })}
                >
                  {countdown(window.reset_at_ms)}
                </span>
              )}
            </div>
          )
        })}
      </div>
      {/* 重置机会是原厂权益，不与原厂积分余额混淆；列表和详情都隐藏金额。 */}
      {/* 次数与到期信息共享一行，在窄屏自然换行，不扩大页面宽度。 */}
      <div className='text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1'>
        <span>
          {t('Reset opportunities')}:{' '}
          {resetFresh && snapshot?.reset_credits_available !== undefined
            ? formatNumber(snapshot.reset_credits_available, locale)
            : t('Unknown')}
        </span>
        {/* 常驻最近到期日，让用户能在列表比较哪张权益需要先处理。 */}
        {/* title 保留年份和钟点；短日期不参与任何过期判断。 */}
        {/* 重置入口需要独立确认，查看到期提示不会启动不可撤销操作。 */}
        {expiries.length > 0 && (
          <span
            title={expiries
              .map((expires) =>
                t('Expires at {{time}}', { time: date(expires) })
              )
              .join('\n')}
          >
            {t('Expires at {{time}}', {
              time: new Date(expiries[0]).toLocaleDateString(locale, {
                month: 'short',
                day: 'numeric',
              }),
            })}
          </span>
        )}
      </div>
      {/* 错误码只用来定位失败，不能把原厂完整响应或凭据带到界面。 */}
      {observation?.last_error_code && (
        <p role='status' className='text-destructive break-words'>
          {observation.last_error_code}
        </p>
      )}
    </div>
  )
}
