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
import { useQuery } from '@tanstack/react-query'
import { CreditCard } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import {
  CREDIT_MODULE_LABELS,
  getSelfCreditAccount,
} from '@/features/credit/api'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

// 额度卡是本人资料的只读财务摘要。
// 总授信、占用和可用额来自同一服务端响应，
// 不能拼接不同时间的三次请求。
// 可用额只表示总额减去未结清占用，
// 不等于平台已经收到相同数额的现金。
// 账户停用后仍可能有未结清欠款，
// 所以停用不会隐藏历史占用。
// 模块开关表示管理员允许的购买范围，
// 仍须和账户总开关共同决定支付资格。
// 显示正额度不意味着所有模块都已开放，
// 购买页还会核对各模块授权。
// 未配置授信时返回null，
// 页面提供联系管理员的提示而不是新建账户。
// 读取错误与未配置是不同业务状态，
// 失败时只能显示重试，不能显示空授信。
// 金额固定为人民币，
// 不继承钱包可能采用的美元或token显示方式。
// 界面语言转换为合法Intl语言码，
// 简繁中文切换不会导致数字格式化异常。
// 模块名使用已有共享映射，
// 避免和付款页出现另一套业务名称。
// 刷新只重取本人摘要，
// 不会保存个人资料的未提交表单。
// 卡片没有用户编号输入，
// 接口也不会接收客户端指定的目标用户。
// 卡片不提供授信编辑或付款按钮，
// 管理员配置入口继续使用现有权限和版本校验。
// 初次加载保留骨架占位，
// 避免闪现零值让用户误以为授权刚被清空。
// 金额与标签可以在窄容器内换行，
// 较长的本地化文案不会撑破右侧栏。
// 右侧布局由个人资料页面负责，
// 此组件不感知管理页或强制固定页面宽度。

// 本人授信与资料设置并排展示，所有数值均来自服务端账本。
// 页面不提供自增额度入口；总授信和模块授权仍由管理员调整。
// 授信按人民币计量，不能套用站点钱包的美元或 token 显示设置。
// 额度变更不会通过本组件调用其他用户的管理接口。
// 重新登录后沿用公共查询客户端的会话清理规则。
export function CreditSummaryCard() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const query = useQuery({
    queryKey: ['self-credit'],
    queryFn: getSelfCreditAccount,
  })
  const account = query.data
  // 停用账户仍展示历史欠款；授权状态与实际欠款是不同概念。
  const modules = account
    ? [
        account.topup_enabled ? t(CREDIT_MODULE_LABELS.topup) : null,
        account.subscription_enabled
          ? t(CREDIT_MODULE_LABELS.subscription)
          : null,
        account.recharge_enabled ? t(CREDIT_MODULE_LABELS.recharge) : null,
      ].filter(Boolean)
    : []
  return (
    <TitledCard
      title={t('On-account credit')}
      icon={<CreditCard className='size-4' />}
      disableHoverEffect
    >
      {/* 首次加载不显示零额度，避免误导用户授权已经关闭。 */}
      {query.isLoading && <Skeleton className='h-40 w-full' />}
      {/* 错误可独立重试，不影响个人资料表单的未保存内容。 */}
      {query.isError && <ErrorState onRetry={() => void query.refetch()} />}
      {!query.isLoading && !query.isError && (
        <div className='space-y-4'>
          {/* 未授权与额度用尽分开显示；此处不创建默认授信账户。 */}
          {!account && (
            <p className='text-muted-foreground text-sm'>
              {t(
                'On-account credit is not configured. Contact an administrator.'
              )}
            </p>
          )}
          {account && (
            <>
              <p className='text-sm'>
                {t(account.enabled ? 'Enabled' : 'Disabled')}
              </p>
              <dl className='space-y-3 text-sm'>
                {/* 三项金额共用两位小数，授信上限不等于当前可消费数。 */}
                {[
                  [t('Credit limit (CNY)'), account.limit_cents],
                  [t('Outstanding credit (CNY)'), account.used_cents],
                  [
                    t('Available credit (CNY)'),
                    account.limit_cents - account.used_cents,
                  ],
                ].map(([label, cents]) => (
                  <div
                    key={label}
                    className='flex flex-wrap justify-between gap-2'
                  >
                    <dt className='text-muted-foreground'>{label}</dt>
                    <dd className='font-medium tabular-nums'>
                      ¥{formatNumber(Number(cents) / 100, locale)}
                    </dd>
                  </div>
                ))}
              </dl>
              {/* 模块权限与总开关同时决定能否挂帐，余额正数不代表已授权。 */}
              <p className='text-muted-foreground text-sm'>
                {t('Enabled modules')}: {modules.join('、') || '—'}
              </p>
            </>
          )}
          {/* 主动刷新能查看付款或结清后的额度，不触发任何资金动作。 */}
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            {t('Refresh')}
          </Button>
        </div>
      )}
    </TitledCard>
  )
}
