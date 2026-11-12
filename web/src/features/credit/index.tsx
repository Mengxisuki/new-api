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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useDebounce } from '@/hooks/use-debounce'
import { toIntlLocale } from '@/i18n/languages'
import { hasPermission } from '@/lib/admin-permissions'
import { formatNumber } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { CreditAccounts } from './accounts'
import {
  CREDIT_MODULE_LABELS,
  CREDIT_STATUS,
  getCreditEntries,
  settleCreditEntry,
  writeOffCreditEntry,
  type CreditEntry,
} from './api'

// 财务工作区按真实资源能力控制读取和结清，角色数字不替代授权。
// 授信只读页与债务流水共用一个入口；金额汇总来自服务端全部筛选结果。
// 本页面是财务分类下的挂帐管理入口。
// 左侧导航可见不代表具有实际服务端权限。
// 渲染时读取当前会话资源能力，撤销读取后不继续显示缓存内容。
// 读取和写入能力分开，只有读权限时仍可完成日常对账。
// 未获得写权限不会展示登记结清动作。
// 页面本身不允许调整用户授信或模块范围。
// 授信配置应在支付网关页由有权限的管理员完成。
// 页签将逐笔债务和用户授信摘要分开。
// 用户授信页可以直接筛选对应用户的流水。
// 该跳转只传编号，不修改已有债务或付款状态。
// 流水默认选择未结清，避免历史已收款被误算为当前欠款。
// 切换到全部状态时合计仅代表查询结果，不代表待收金额。
// 金额汇总由服务器计算全部匹配记录，不局限于当前页。
// 界面不使用当前商品价格重算历史债务。
// 每一笔金额都来自下单时冻结的人民币分值。
// 金额格式化仅用于展示，不回传为结清金额。
// 同一用户不同模块未来将共用授信，但目前只接受代充值流水。
// 模块标签明确显示代充值，不把它当成钱包充值。
// 订单号帮助财务关联实际业务，不作为下载或付款链接。
// 用户编号和名称一起展示，方便区分同名用户。
// 搜索支持订单号和用户名称，由服务器执行过滤。
// 搜索字符串是查询参数，不是客户端 SQL 或自由排序表达式。
// 防抖只减少请求，不改变用户最终搜索条件。
// 状态、搜索或用户范围变化时回到第一页。
// 清除用户筛选后保留当前搜索和状态条件。
// 查询键包含所有筛选维度，避免不同范围错误复用缓存。
// 手动刷新只执行当前查询，不重新发起任何结清。
// 加载状态与读取失败有独立呈现。
// 读取失败提供重试，不显示无记录或零欠款作为成功结果。
// 当服务端返回合法空数组时才显示暂无挂帐记录。
// 分页按每页二十条与服务端总数决定下一页可用性。
// 日期使用合法的语言标签，而不是项目内部语言别名。
// 创建时间表示记账发生时间，不冒充现金收款时间。
// 结清时间只在财务确认全额收款后出现。
// 结清人编号与凭证号一起保留，支持事后追踪。
// 备注用于核对收款过程，不在代充值用户详情展示。
// 若结清后又退款，原收款凭证不会被新的退款凭证覆盖。
// 因此历史流水能区分未收款撤销和收款后退回。
// 未结清行才允许打开整笔结清确认框。
// 该按钮不直接写入服务器，必须先完成凭证核对。
// 确认框明确列出交易号与完整应收金额。
// 文案说明必须先在线下实际收到全款。
// 界面不会代为调用支付商户或从钱包自动扣费。
// 目前没有部分结清输入，避免产生用户未要求的分期规则。
// 收款凭证必填，空白字符串不能启用最终确认。
// 凭证号长度遵守服务端存储限制。
// 备注可选，长度限制与后台一致。
// 表单不接收收款人、债务金额或授信余额字段。
// 目标记录来自用户主动打开的行，避免跨行复用选择。
// 每次打开另一行都会清空前一行的凭证和备注。
// 关闭确认框不会取消原业务订单，也不会撤销欠款。
// 提交中禁止关闭与重复点击，减少结果不明确的重复操作。
// 后端仍需幂等处理，因为前端锁不能约束多标签页并发。
// 网络失败时保留凭证输入，用户可以核对原记录后恢复。
// 错误使用项目统一处理器，不吞掉额度或状态冲突原因。
// 成功后同时刷新债务列表、授信摘要和代充值目录。
// 这三者共享一次资金状态变更，不能只隐藏按钮而保留旧额度。
// 失效查询不在客户端手动增加可用额度。
// 不同页面仍以服务器账本作为唯一资金依据。
// 确认完成后关闭弹窗，旧选择不留到下一次操作。
// 若管理员正在批准退款，结清会由后端状态锁拒绝。
// 页面不得根据旧的未结清快照推断当前操作必然成功。
// 用户授信已停用也不影响财务对旧债务登记实际收款。
// 已结清记录不允许通过界面反复修改凭证或覆盖首次操作人。
// 未结清撤销后也不出现重新结清按钮。
// 页面没有手工清零额度、删除流水或伪造付款成功的入口。
// 账本完整性错误保持可见，不能自动补一笔虚构记录。
// 这里仅复用财务权限，不授予读取代充值账号敏感资料的能力。
// 公共组件仍负责弹窗焦点、表格滚动和按钮可访问语义。
// 业务组件只编排数据与动作，不复制另一套基础交互。
// 所有弹层都放在布局内容区，避免命名插槽丢弃子组件。
// 窄屏筛选栏允许换行，表格使用既有横向滚动处理。
// 原生选择器保留键盘导航，状态码经统一映射翻译。
// 状态翻译在当前渲染读取，切换语言后不能残留旧文案。
// 本页面不会将财务记录写入本地持久存储。
// 用户离开页面后查询缓存遵循项目的统一生命周期。
// 后续模块接入应首先扩展后端账本合同，再扩展状态展示。
// 不能仅增加一个模块名称就让该模块具有实际挂帐能力。
// 业务验收覆盖真实表格、确认框、请求参数及只读授权。
// 数据库事务和跨请求竞争由独立后端集成测试验证。
export function CreditManagement() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient() // 沿用页面共享缓存，结清后可刷新其他页签。
  const user = useAuthStore((state) => state.auth.user) // 权限变更通过订阅反映到当前页面。
  const canRead = hasPermission(user, 'finance', 'read') // 读取能力决定是否请求账本。
  const canWrite = hasPermission(user, 'finance', 'write') // 结清能力独立于列表读取。
  const [tab, setTab] = useState('entries') // 默认直接呈现欠款明细。
  const [page, setPage] = useState(1) // 服务端采用一基页码。
  const [status, setStatus] = useState('outstanding') // 默认聚焦当前欠款，避免历史流水混入。
  const [keyword, setKeyword] = useState('')
  const [userID, setUserID] = useState(0) // 零表示未指定某个债务人。
  const search = useDebounce(keyword) // 使用统一延迟，不自建定时器。
  const [selected, setSelected] = useState<CreditEntry | null>(null)
  const [reference, setReference] = useState('')
  const [note, setNote] = useState('')
  const [writeOff, setWriteOff] = useState<CreditEntry | null>(null)
  const [writeOffReason, setWriteOffReason] = useState('')
  const query = useQuery({
    queryKey: ['credit-entries', page, status, search, userID], // 筛选范围不同必须隔离缓存。
    queryFn: () => getCreditEntries(page, status, search, userID),
    enabled: canRead, // 无权限时禁止后台预读。
  })
  const invalidateCreditViews = async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: ['credit-entries'] }),
      client.invalidateQueries({ queryKey: ['credit-accounts'] }),
      client.invalidateQueries({ queryKey: ['recharge'] }),
      client.invalidateQueries({ queryKey: ['finance-overview'] }),
    ])
  }
  const settle = useMutation({
    mutationFn: (entry: CreditEntry) =>
      settleCreditEntry(entry.id, reference.trim(), note.trim()),
    onError: (error) => handleServerError(error),
    onSuccess: async () => {
      await invalidateCreditViews()
      setSelected(null)
    },
  })
  const writeOffMutation = useMutation({
    mutationFn: (entry: CreditEntry) =>
      writeOffCreditEntry(entry.id, writeOffReason.trim()),
    onError: (error) => handleServerError(error),
    onSuccess: async () => {
      await invalidateCreditViews()
      setWriteOff(null)
      setWriteOffReason('')
    },
  })
  if (!canRead) return <ErrorState title={t('Access denied')} />
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('On-account management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <Tabs value={tab} onValueChange={setTab}>
          <TabsList>
            <TabsTrigger value='entries'>{t('Credit records')}</TabsTrigger>
            <TabsTrigger value='accounts'>{t('On-account users')}</TabsTrigger>
          </TabsList>
          <TabsContent value='accounts' className='mt-4'>
            <CreditAccounts
              admin={false}
              onSelectUser={(id) => {
                setUserID(id) // 从授信摘要选择真实用户编号。
                setPage(1) // 更换用户范围不能沿用旧页码。
                setTab('entries') // 只切换查看范围，不产生结清。
              }}
            />
          </TabsContent>
          <TabsContent value='entries' className='mt-4 space-y-4'>
            <div className='flex flex-wrap items-center gap-3'>
              <Input
                className='max-w-sm'
                aria-label={t('Search credit records')}
                placeholder={t('Order number or username')}
                value={keyword}
                onChange={(event) => {
                  setKeyword(event.target.value)
                  setPage(1)
                }}
              />
              <NativeSelect
                aria-label={t('Credit status')}
                value={status}
                onChange={(event) => {
                  setStatus(event.target.value)
                  setPage(1)
                }}
              >
                <NativeSelectOption value=''>
                  {t('All statuses')}
                </NativeSelectOption>
                {Object.entries(CREDIT_STATUS).map(([value, label]) => (
                  <NativeSelectOption key={value} value={value}>
                    {t(label)}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
              {userID > 0 && (
                <Button
                  variant='outline'
                  onClick={() => {
                    setUserID(0)
                    setPage(1)
                  }}
                >
                  {t('Clear user filter')} #{userID}
                </Button>
              )}
              <Button
                variant='outline'
                disabled={query.isFetching}
                onClick={() => void query.refetch()}
              >
                {t('Refresh')}
              </Button>
            </div>
            {/* 汇总明确限定当前筛选范围，避免当作已实际收到的现金。 */}
            {query.data && !query.isError && (
              <p>
                {t('Filtered amount (CNY)')}:{' '}
                {formatNumber(query.data.amount_cents / 100, locale)} ·{' '}
                {query.data.total} {t('Credit records')}
              </p>
            )}
            {query.isError && (
              <ErrorState onRetry={() => void query.refetch()} />
            )}
            {query.isLoading && <p role='status'>{t('Loading...')}</p>}
            {query.data && !query.isError && (
              <StaticDataTable
                data={query.data.items}
                getRowKey={(row) => row.id}
                emptyContent={t('No credit records')}
                columns={[
                  {
                    id: 'user', // 名称可能变化，展示同时保留编号。
                    header: t('User'),
                    cell: (row) =>
                      `#${row.user_id} · ${row.username} ${row.display_name}`,
                  },
                  {
                    id: 'order', // 业务交易号与债务一一关联。
                    header: t('Order number'),
                    cell: (row) => (
                      <div>
                        {row.trade_no}
                        <p className='text-muted-foreground text-xs'>
                          {t(CREDIT_MODULE_LABELS[row.module])}
                        </p>
                      </div>
                    ),
                  },
                  {
                    id: 'amount', // 金额来自历史冻结值。
                    header: t('Amount (CNY)'),
                    cell: (row) => formatNumber(row.amount_cents / 100, locale),
                  },
                  {
                    id: 'status', // 债务终态不等于人工办理状态。
                    header: t('Credit status'),
                    cell: (row) => (
                      <div>
                        {t(CREDIT_STATUS[row.status])}
                        {/* 核销不是收款，原因与操作人放在终态下方，不冒充收款凭证。 */}
                        {row.status === 'write_off' && (
                          <div className='text-muted-foreground text-xs'>
                            <p>{row.write_off_note}</p>
                            <p>#{row.closed_by} · {new Date(row.closed_at * 1000).toLocaleString(locale)}</p>
                          </div>
                        )}
                      </div>
                    ),
                  },
                  // 日期使用合法界面语言标签，零时间表示尚未发生。
                  {
                    id: 'created', // 记账时间不代表现金入账。
                    header: t('Created time'),
                    cell: (row) =>
                      new Date(row.created_at * 1000).toLocaleString(locale),
                  },
                  {
                    id: 'settlement', // 收款与退款凭证共同保留。
                    header: t('Settlement reference'),
                    cell: (row) => (
                      <div>
                        {row.settlement_ref || '—'}
                        {row.settled_at > 0 && (
                          <p className='text-muted-foreground text-xs'>
                            #{row.settled_by} ·{' '}
                            {new Date(row.settled_at * 1000).toLocaleString(
                              locale
                            )}
                          </p>
                        )}
                        <p>{row.settlement_note}</p>
                        {row.refund_ref && (
                          <p>
                            {t('Refund reference')}: {row.refund_ref}
                          </p>
                        )}
                      </div>
                    ),
                  },
                  {
                    id: 'actions',
                    header: t('Actions'),
                    cell: (row) =>
                      canWrite && row.status === 'outstanding' ? (
                        <div className='flex flex-wrap justify-end gap-2'>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => {
                              setReference('')
                              setNote('')
                              setSelected(row)
                            }}
                          >
                            {t('Record full settlement')}
                          </Button>
                          <Button
                            size='sm'
                            variant='destructive'
                            onClick={() => {
                              setWriteOffReason('')
                              setWriteOff(row)
                            }}
                          >
                            {t('Write off')}
                          </Button>
                        </div>
                      ) : null,
                  },
                ]}
              />
            )}
            {/* 页码只影响当前筛选列表，切换筛选会回到第一页。 */}
            <div className='flex items-center justify-end gap-3'>
              <span>{t('Page {{page}}', { page })}</span>
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
          </TabsContent>
        </Tabs>
        {/* 使用共享确认弹窗，整笔金额和凭证在实际写入前可核对。 */}
        {selected && canWrite && (
          <ConfirmDialog
            open
            title={t('Record full settlement')}
            desc={`${selected.trade_no} · CNY ${formatNumber(selected.amount_cents / 100, locale)}. ${t('Confirm only after the full offline payment has been received. This restores the credit limit.')}`}
            onOpenChange={(open) => {
              if (!open && !settle.isPending) setSelected(null) // 提交未结束不能丢弃结果核对入口。
            }}
            isLoading={settle.isPending}
            disabled={!reference.trim()}
            confirmText={t('Confirm settlement')}
            handleConfirm={() => settle.mutate(selected)}
          >
            <Field>
              <FieldLabel htmlFor='credit-reference'>
                {t('Settlement reference')}
              </FieldLabel>
              <Input
                id='credit-reference'
                value={reference}
                maxLength={128}
                onChange={(event) => setReference(event.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor='credit-note'>{t('Remark')}</FieldLabel>
              <Input
                id='credit-note'
                value={note}
                maxLength={500}
                onChange={(event) => setNote(event.target.value)}
              />
            </Field>
          </ConfirmDialog>
        )}
        {writeOff && canWrite && (
          <ConfirmDialog
            open
            destructive
            title={t('Write off credit entry')}
            desc={`${writeOff.trade_no} · CNY ${formatNumber(writeOff.amount_cents / 100, locale)}. ${t('Write-off releases the credit hold only. It is not a cash refund and does not grant benefits.')}`}
            onOpenChange={(open) => {
              if (!open && !writeOffMutation.isPending) {
                setWriteOff(null)
                setWriteOffReason('')
              }
            }}
            isLoading={writeOffMutation.isPending}
            disabled={!writeOffReason.trim()}
            confirmText={t('Confirm write-off')}
            handleConfirm={() => writeOffMutation.mutate(writeOff)}
          >
            <Field>
              <FieldLabel htmlFor='credit-write-off-reason'>
                {t('Write-off reason')} *
              </FieldLabel>
              <Input
                id='credit-write-off-reason'
                value={writeOffReason}
                maxLength={500}
                onChange={(event) => setWriteOffReason(event.target.value)}
              />
            </Field>
          </ConfirmDialog>
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
