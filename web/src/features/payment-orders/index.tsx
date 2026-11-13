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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { CREDIT_MODULE_LABELS } from '@/features/credit/api'
import { CHANNEL_LABEL_KEYS } from '@/features/finance-overview/constants'
import { useDebounce } from '@/hooks/use-debounce'
import { toIntlLocale } from '@/i18n/languages'
import { hasPermission } from '@/lib/admin-permissions'
import { formatNumber } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import {
  getPaymentOrders,
  refundPaymentOrder,
  type PaymentOrder,
  type OrderModule,
} from './api'

// 页面分为业务筛选、订单列表和退款确认三个部分。
// 进入页面只读取交易摘要，
// 不会因打开订单列表而请求微信。
// 三个业务分别分页展示，
// 用户可以明确选择钱包、订阅或代充值。
// 切换业务会重置页码和支付状态，
// 避免把另一业务的筛选带入导致假空列表。
// 交易号搜索使用项目共享防抖，
// 不在业务组件里维护另一套定时器。
// 状态筛选针对原支付状态，
// 退款状态在独立列展示。
// 原单已支付而退款已成功可以同时成立，
// 这不是需要前端修正的矛盾数据。
// 列表不会把退款订单改标为未付款，
// 财务仍需要追溯其原收款事实。
// 订单金额使用服务端记录值，
// 不从当前套餐价格或钱包余额推算。
// 国际网关记录值不自动标为人民币，
// 同一列不承担跨币种汇总职责。
// 退款确认金额仍由后端冻结，
// 页面金额只是供管理员核对交易。
// 用户以稳定编号标识，
// 同名显示名不作为退款归属依据。
// 订阅的账单镜像在服务端排除，
// 前端不按金额零值自行去重。
// 创建时间只是交易建立时间，
// 不拿它代替支付或退款的实际到账时间。
// 时间格式遵循当前界面语言，
// 内部语言别名先经公共转换函数处理。
// 订单号允许在窄列中折行，
// 不会因为一个长交易号扩大整个页面。
// 通用表格负责列宽和水平滚动，
// 本页不复制一套独立的表格滚动实现。
// 当前每页二十条与服务端约定一致，
// 下一页按钮由总数决定是否开放。
// 请求进行中暂停分页动作，
// 减少连续点击造成的页码跳跃。
// 没有订单和读取失败分开呈现，
// 错误不能被空数组或零总数掩盖。
// 手动刷新重新读取当前过滤条件，
// 不会把筛选回到默认钱包业务。
// 页面定期刷新读取结果，
// 已受理微信退款由后台查询推进状态。
// 浏览器轮询只发送列表GET，
// 不由页面定时重放资金POST。
// 退款权限和读取权限分别判断，
// 只读管理员仍能查看处理中和历史结果。
// 无读取权限时不预取列表，
// 也不显示另一个会话留下的交易缓存。
// 前端权限只控制体验，
// 后端对每次读取和退款仍独立授权。
// 首次退款必须显式打开确认框，
// 点击行内按钮本身不回收任何权益。
// 确认框显示完整交易号及原单金额，
// 避免管理员误退相邻行。
// 钱包提示说明必须扣回冻结充值额度，
// 不足时服务器会拒绝而不是部分退款。
// 订阅提示说明回收的是本单增加的时长，
// 不会暗示无条件删除整个订阅实例。
// 代充值提示说明已完成订单不能退款，
// 管理员还须先确认外部办理已停止。
// 原因按UTF八编码后的字节数限制，
// 中文字数不能直接当作微信接口字节长度。
// 纯空白原因不允许确认，
// 真正的业务校验仍由后端执行。
// 提交时禁用确认与取消，
// 防止请求进行中关闭对话框造成结果丢失。
// 服务端成功返回后关闭确认框，
// 但列表状态仍以重新读取的记录为准。
// 网络失败可能发生在本地权益回收之后，
// 失败回调同样必须刷新订单。
// 这可以让管理员看到原退款已经存在，
// 而不是继续误以为首次请求完全未执行。
// 已有退款只提供恢复动作，
// 恢复不会重新要求填写另一份原因。
// 原原因由后端冻结并继续使用，
// 浏览器不允许换退款号或改退款金额。
// 准备态表示需要核实提交结果，
// 不能把它直接当作微信退款失败。
// 处理中表示微信已经受理，
// 页面不会提前显示现金已退回。
// 成功状态没有再次退款按钮，
// 避免重复操作给管理员造成误解。
// 关闭和异常状态保留原单查询入口，
// 同时提示到微信商户平台处理。
// 这些状态不会自动恢复已经回收的权益，
// 界面必须明确说明这一资金边界。
// 退款号、原因和原操作人一起展示，
// 为客服与财务核对提供最小必要信息。
// 无退款记录使用短占位符，
// 不显示虚构的退款号或默认金额。
// 服务端禁止原因直接翻译为业务提示，
// 不把内部错误对象或商户配置输出到页面。
// 历史订阅缺少权益关联时保留明确原因，
// 不能只隐藏按钮而让管理员无法理解限制。
// 确认组件复用项目危险操作封装，
// 因此键盘焦点和弹窗关闭行为遵循全站约定。
// 列表和财务总览在操作结束后一起失效，
// 避免退款成功但汇总仍显示旧净收款。
// 页面不维护第二份本地退款状态机，
// 持久账本始终是结果的唯一来源。

// 统一查看三个业务线原始付款与退款，不合并不同网关金额。
// 通用表格和危险确认沿用项目封装，业务层只处理筛选和退款意图。
// 权限控制数据读取，按钮可见性不能代替后端鉴权。
// 原付款状态先映射到现有业务文案，不能直接展示数据库枚举。
// 未知历史状态仍保留原标识供核对，不猜测为支付成功。
const PAYMENT_STATUS_LABELS: Record<string, string> = {
  pending: 'Awaiting payment',
  success: 'Paid',
  paid: 'Paid',
  refunding: 'Refunding',
  refunded: 'Refunded',
  failed: 'Failed',
  expired: 'Expired',
}

export function PaymentOrders() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const user = useAuthStore((s) => s.auth.user)
  const canRead = hasPermission(user, 'orders', 'read')
  const canRefund = hasPermission(user, 'orders', 'refund')
  const client = useQueryClient()
  // 初始业务为钱包充值，保持管理员首先看到最常见的现金订单。
  const [module, setModule] = useState<OrderModule>('topup')
  // 页码属于当前过滤范围，不用于选择或标识交易。
  const [page, setPage] = useState(1)
  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState('')
  // 确认对象保留打开弹窗时的完整交易身份，不能只保存当前行索引。
  const [selected, setSelected] = useState<PaymentOrder | null>(null)
  // 原因只存在当前页面内存，不写入浏览器持久存储。
  const [reason, setReason] = useState('')
  // 搜索只针对原交易号，不能把退款编号当作另一笔付款。
  // 防抖后的关键词参与缓存键，确保结果归属当前查询。
  const search = useDebounce(keyword)
  // 筛选维度全部进入查询键，切换业务不展示上一类订单。
  const query = useQuery({
    queryKey: ['payment-orders', module, page, search, status],
    queryFn: () => getPaymentOrders(module, page, search, status),
    enabled: canRead,
    refetchInterval: 30000, // 只轮询列表读取，外部退款由后台处理。
  })
  const refund = useMutation({
    mutationFn: (order: PaymentOrder) => refundPaymentOrder(order, reason),
    onSuccess: () => setSelected(null), // API确认后才关闭，失败保留核对上下文。
    // 网络失败仍可能已回收权益，必须重读原单而非显示尚未退款。
    onSettled: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: ['payment-orders'] }),
        client.invalidateQueries({ queryKey: ['finance-overview'] }),
      ])
    },
  })
  if (!canRead) return <ErrorState title={t('Access denied')} />
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Order management')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          {/* 异步退款先撤权益，受理中不能显示为已退现金。 */}
          <p className='text-muted-foreground text-sm'>
            {t(
              'Full WeChat refunds recover the original entitlement first. Pending or failed requests must be recovered using the same refund.'
            )}
          </p>
          <div className='flex flex-wrap gap-3'>
            <NativeSelect
              aria-label={t('Order type')}
              value={module}
              onChange={(e) => {
                setModule(e.target.value as OrderModule)
                setPage(1)
                setStatus('')
              }}
            >
              {Object.entries(CREDIT_MODULE_LABELS).map(([value, label]) => (
                <NativeSelectOption key={value} value={value}>
                  {t(label)}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <NativeSelect
              aria-label={t('Payment status')}
              value={status}
              onChange={(e) => {
                setStatus(e.target.value)
                setPage(1)
              }}
            >
              <NativeSelectOption value=''>{t('All')}</NativeSelectOption>
              <NativeSelectOption value='pending'>
                {t('Pending')}
              </NativeSelectOption>
              <NativeSelectOption
                value={module === 'recharge' ? 'paid' : 'success'}
              >
                {t('Paid')}
              </NativeSelectOption>
              {module === 'recharge' && (
                <NativeSelectOption value='refunding'>
                  {t('Refunding')}
                </NativeSelectOption>
              )}
            </NativeSelect>
            <Input
              className='max-w-sm'
              aria-label={t('Order number')}
              placeholder={t('Order number')}
              value={keyword}
              onChange={(e) => {
                setKeyword(e.target.value)
                setPage(1)
              }}
            />
            <Button
              variant='outline'
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              {t('Refresh')}
            </Button>
          </div>
          {/* 错误与空列表分开，重试只执行读取。 */}
          {query.isError && <ErrorState onRetry={() => void query.refetch()} />}
          {query.isLoading && <p role='status'>{t('Loading...')}</p>}
          {query.data && !query.isError && (
            <StaticDataTable
              data={query.data.items}
              getRowKey={(row) => row.trade_no}
              emptyContent={t('No orders')}
              columns={[
                {
                  id: 'trade', // 商户订单号供管理员定位原交易。
                  header: t('Order number'),
                  cell: (row) => (
                    <span className='break-all'>{row.trade_no}</span>
                  ),
                },
                {
                  id: 'user',
                  header: t('User'),
                  cell: (row) => `#${row.user_id}`,
                },
                // 国际渠道保留原记录值，不擅自标记成人民币实收。
                {
                  id: 'money', // 原订单金额不能由退款状态反推。
                  header: t('Order amount'),
                  cell: (row) => formatNumber(row.money, locale),
                },
                {
                  id: 'provider', // 渠道标识保留，便于定位实际商户配置。
                  header: t('Payment method'),
                  cell: (row) =>
                    `${t(CHANNEL_LABEL_KEYS[row.payment_provider] || ({ wechat: 'WeChat Pay', credit: 'Pay on account', wallet: 'Wallet' } as Record<string, string>)[row.payment_provider] || row.payment_provider)} · ${row.payment_method}`,
                },
                {
                  id: 'paid',
                  header: t('Payment status'),
                  cell: (row) =>
                    t(PAYMENT_STATUS_LABELS[row.status] || row.status),
                },
                {
                  id: 'date', // 创建时刻不会被退款查询覆盖。
                  header: t('Created at'),
                  cell: (row) =>
                    new Date(row.created_at * 1000).toLocaleString(locale),
                },
                {
                  id: 'refund', // 独立状态列避免混淆收款与退款。
                  header: t('Refund status'),
                  cell: (row) =>
                    row.refund ? (
                      <div className='space-y-1'>
                        <p>{t(`Refund ${row.refund.status}`)}</p>
                        <p className='text-muted-foreground text-xs'>
                          {row.refund.refund_no}
                        </p>
                        <p className='text-xs'>
                          {row.refund.reason} · #{row.refund.actor_id}
                        </p>
                      </div>
                    ) : (
                      '—'
                    ),
                },
                {
                  id: 'actions', // 操作始终以当前行的业务归属发起。
                  header: t('Actions'),
                  cell: (row) => (
                    <div className='max-w-xs space-y-2'>
                      {/* 成功后不再显示按钮；异常只允许原单查询恢复。 */}
                      {canRefund &&
                        row.refund?.status !== 'succeeded' &&
                        !row.refund_block && (
                          <Button
                            size='sm'
                            variant='outline'
                            disabled={refund.isPending}
                            onClick={() => {
                              setSelected(row) // 固定本次确认对象，不依赖表格排序。
                              setReason('') // 每次首次退款重新填写，不沿用上一单原因。
                            }}
                          >
                            {t(row.refund ? 'Recover refund' : 'Full refund')}
                          </Button>
                        )}
                      {row.refund_block && (
                        <p className='text-muted-foreground text-xs whitespace-normal'>
                          {t(row.refund_block)}
                        </p>
                      )}
                      {row.refund &&
                        ['closed', 'abnormal'].includes(row.refund.status) && (
                          <p className='text-destructive text-xs whitespace-normal'>
                            {t(
                              'Resolve this refund in the WeChat merchant platform, then query its result. Entitlements remain recovered.'
                            )}
                          </p>
                        )}
                    </div>
                  ),
                },
              ]}
            />
          )}
          {/* 页大小固定二十，筛选变化必须回到第一页。 */}
          <div className='flex items-center justify-end gap-3'>
            <span>
              {t('Page {{page}}', { page })} · {query.data?.total ?? 0}
            </span>
            <Button
              variant='outline'
              disabled={page === 1 || query.isFetching}
              onClick={() => setPage(page - 1)}
            >
              {t('Previous')}
            </Button>
            <Button
              variant='outline'
              disabled={
                page * 20 >= (query.data?.total ?? 0) || query.isFetching
              }
              onClick={() => setPage(page + 1)}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
        {/* 确认包含交易号和金额，原因不作为微信成功凭证。 */}
        <ConfirmDialog
          open={selected !== null}
          onOpenChange={(open) => {
            if (!open && !refund.isPending) setSelected(null)
          }}
          title={t(selected?.refund ? 'Recover refund' : 'Full refund')}
          desc={
            <>
              <p>
                {selected?.trade_no} · {formatNumber(selected?.money, locale)}
              </p>
              <p>
                {t(
                  'Wallet refunds require sufficient balance. Subscription refunds remove the duration granted by this order. Completed recharge orders cannot be refunded.'
                )}
              </p>
            </>
          }
          confirmText={t('Confirm')}
          destructive
          isLoading={refund.isPending}
          disabled={
            !selected?.refund &&
            (!reason.trim() ||
              new TextEncoder().encode(reason.trim()).length > 80)
          }
          handleConfirm={() => {
            if (selected) refund.mutate(selected)
          }}
        >
          {/* 恢复使用原原因，不允许更改原退款参数。 */}
          {!selected?.refund && (
            <Field>
              <FieldLabel htmlFor='refund-reason'>
                {t('Refund reason (up to 80 bytes)')}
              </FieldLabel>
              <Input
                id='refund-reason'
                value={reason}
                onChange={(e) => setReason(e.target.value)}
              />
            </Field>
          )}
        </ConfirmDialog>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
