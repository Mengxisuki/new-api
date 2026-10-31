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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useDebounce } from '@/hooks/use-debounce'
import { toIntlLocale } from '@/i18n/languages'
import { hasPermission } from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import { getFinanceInvoices } from './admin-api'
import { InvoiceReviewDialog } from './components/invoice-review-dialog'
import { formatInvoiceAmount } from './format'
import { INVOICE_STATUS_LABELS } from './invoice-status'
import type { InvoiceRequest } from './types'

// 财务列表只读取当前页，敏感抬头不得在浏览器缓存全部用户记录。
// 路由、页面能力和服务端逐请求权限分别约束导航、动作与真实数据访问。
// 角色数字只描述账号类型，本页使用资源能力，不把财务视为普通管理员。
// 默认同时列出付款中和已付款申请，状态筛选帮助财务排除尚未到账订单。
// 处理入口读取每笔订单冻结的抬头，修改个人常用资料不回写历史申请。
// 本页不生成税务发票，只负责已开票文件与平台申请的关联。
// 上传、拒绝与开始处理由详情组件执行，列表不保存第二份状态机。
// 服务端分页保持十条一页，筛选变化回到第一页，避免停留在空的后续页。
// 搜索采用已有防抖能力，不额外维护另一套定时器或请求状态。
// 查询键包含全部分页和筛选条件，迟到响应只能进入其自己的缓存项。
// 操作后失效财务列表前缀，让其它页下次访问时也能读取新状态。
// 金额显示固定交易币种人民币，不随站点余额显示单位切换。
// 本列表不合计全部用户资金，避免把当前页当成财务总账。
// 私有文件不在列表预取，只有详情中的明确下载动作才请求附件。
// 查询失败显示错误与重试，不能把失败结果伪装成没有申请。
// 已选记录按申请ID保持身份，切换申请时详情草稿必须重新初始化。
// 表格复用公共静态表格的溢出处理，不为单页实现另一套表格系统。
// 当前较长部分是列定义和声明式布局，写操作不混在表格渲染中。
export function FinanceInvoices() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language) // 界面语言码先转换，兼容简繁中文金额格式。
  const user = useAuthStore((state) => state.auth.user) // 权限变更实时订阅，不冻结首次打开的角色。
  const canRead = hasPermission(user, 'finance', 'read') // 无读权限时不发起敏感列表请求。
  const canWrite = hasPermission(user, 'finance', 'write') // 查看与办理能力独立，不能由列表可见推导可写。
  const [page, setPage] = useState(1) // 页码使用服务端的一基编号。
  const [status, setStatus] = useState('all') // all只存在于选择器，发送前转换为空筛选。
  const [keyword, setKeyword] = useState('') // 搜索只传订单或发票号码，不拼接完整抬头资料。
  const search = useDebounce(keyword) // 键入期间复用已有搜索防抖。
  const [selected, setSelected] = useState<InvoiceRequest | null>(null) // null明确表示没有正在查看的申请。
  const client = useQueryClient() // 操作后统一失效财务查询，不在每行维护重复刷新计数。
  const query = useQuery({
    queryKey: ['finance-invoices', page, status, search], // 不同页和筛选结果不会互相覆盖。
    queryFn: () =>
      getFinanceInvoices(page, status === 'all' ? '' : status, search.trim()),
    enabled: canRead, // 页面拒绝访问时不靠请求错误去试探数据是否存在。
  })
  if (!canRead) return <ErrorState title={t('Access denied')} /> // 缓存中即使有旧数据也不渲染。
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Invoice management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          <RefreshCw aria-hidden='true' />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <div className='flex flex-wrap gap-3'>
            <Input
              aria-label={t('Search invoice requests')}
              placeholder={`${t('Order number')} / ${t('Invoice number')}`}
              value={keyword} // 草稿由当前页面持有，关闭后不保留敏感搜索。
              onChange={(event) => {
                setKeyword(event.target.value)
                setPage(1) // 新筛选从第一页开始，避免被旧分页隐藏结果。
              }}
              className='max-w-sm'
            />
            <Select
              value={status} // 显示状态与服务端已知状态集合保持一致。
              onValueChange={(value) => {
                if (value) {
                  setStatus(value)
                  setPage(1) // 新筛选从第一页开始，避免被旧分页隐藏结果。
                }
              }}
            >
              <SelectTrigger className='w-48' aria-label={t('Invoice status')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {/* 待支付与待开票分别展示，不能将付款未完成的记录混为可办理。 */}
                <SelectItem value='all'>{t('All statuses')}</SelectItem>
                {Object.entries(INVOICE_STATUS_LABELS).map(([value, label]) => (
                  <SelectItem key={value} value={value}>
                    {t(label)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {query.isError && (
            <ErrorState
              title={t('Failed to load invoice requests')} // 错误保留明确恢复入口。
              onRetry={() => void query.refetch()} // 恢复读取失败只重取列表，不创建新申请。
            />
          )}
          {!query.isError && query.isLoading && (
            <p role='status'>{t('Loading...')}</p>
          )}
          {!query.isError && !query.isLoading && (
            <StaticDataTable
              data={query.data?.items ?? []} // 只渲染当前服务端页，不复制过滤或金额计算。
              getRowKey={(item) => item.id} // 稳定申请ID避免翻页后错误复用行状态。
              emptyContent={t('No invoice requests')} // 只有成功读取的空列表才显示此文案。
              columns={[
                {
                  id: 'order', // 支付订单号是用户账单与财务申请的对应依据。
                  header: t('Order number'), // 交易号可与用户提供的付款凭证对应核查。
                  cell: (item) => (
                    <div>
                      <p>{item.trade_no}</p>
                      <p className='text-muted-foreground text-xs'>
                        #{item.user_id}
                      </p>
                    </div>
                  ),
                },
                {
                  id: 'title', // 列表显示冻结抬头，税号等细节留在主动打开的详情。
                  header: t('Invoice title'), // 抬头展示属于受保护财务信息，不提供匿名列表。
                  cell: (item) => item.profile.title, // 直接使用申请快照，不能改读用户最新常用抬头。
                },
                {
                  id: 'paid', // 实付包含附加费，不能当成增加的平台额度。
                  header: t('Paid amount'), // 含附加费的总收款用于开票核对。
                  cell: (item) => formatInvoiceAmount(item.pay_amount, locale), // 冻结币种格式化防止站点切换后把人民币显示成美元。
                },
                {
                  id: 'fee', // 加价独立展示，便于财务核对基础收款与开票费用。
                  header: t('Invoice surcharge'), // 单独列出费用，避免财务需反推加价。
                  cell: (item) => formatInvoiceAmount(item.fee_amount, locale), // 历史费用从记录读取，不按当前费率重算。
                },
                {
                  id: 'status', // 可见状态完全使用服务端记录。
                  header: t('Status'), // 付款与开票进度都来自同一申请。
                  cell: (item) => t(INVOICE_STATUS_LABELS[item.status]), // 使用有限状态译文，不能把状态码直接当用户说明。
                },
                {
                  id: 'actions', // 写权限判断留在详情动作，不阻止只读用户查看。
                  header: t('Actions'),
                  cell: (item) => (
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => setSelected(item)} // 打开详情不产生服务端状态更新。
                    >
                      {t('Review invoice')}
                    </Button>
                  ),
                },
              ]}
            />
          )}
          <div className='flex items-center justify-end gap-3'>
            <span className='text-muted-foreground text-sm'>
              {t('Page {{page}}', { page })} · {query.data?.total ?? 0}
            </span>
            <Button
              variant='outline'
              disabled={page === 1 || query.isFetching} // 第一页不发第零页请求，加载中也不重复翻页。
              onClick={() => setPage((value) => value - 1)} // 仅改变服务端页码，不在本地截取全用户数据。
            >
              {t('Previous')}
            </Button>
            <Button
              variant='outline'
              disabled={
                page * 10 >= (query.data?.total ?? 0) || query.isFetching
              }
              onClick={() => setPage((value) => value + 1)} // 下一页继续执行同一权限和过滤条件。
            >
              {t('Next')}
            </Button>
          </div>
        </div>
        {selected && (
          <InvoiceReviewDialog
            key={selected.id} // 另一份申请不能沿用上次文件和发票号草稿。
            invoice={selected} // 详情只能读取当前用户主动选择的那份申请。
            canWrite={canWrite} // 权限撤销后当前详情也立即隐藏办理入口。
            onClose={() => setSelected(null)} // 关闭释放含抬头资料的页面草稿。
            onUpdated={() => {
              void client.invalidateQueries({ queryKey: ['finance-invoices'] }) // 全部财务页签失效，下次访问不能继续显示旧状态。
            }}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
