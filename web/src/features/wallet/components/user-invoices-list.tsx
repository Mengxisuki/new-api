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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { getUserInvoices, downloadUserInvoice } from '@/features/invoices/api'
import { formatInvoiceAmount } from '@/features/invoices/format'
import { INVOICE_STATUS_LABELS } from '@/features/invoices/invoice-status'
import { toIntlLocale } from '@/i18n/languages'
import { handleServerError } from '@/lib/handle-server-error'

interface Props {
  open: boolean // 外层必须确认用户正在查看发票页签才启用查询。
}

// 用户发票独立分页，不能用前一百份申请去猜测旧账单的开票状态。
// 列表和下载始终走本人鉴权接口，关闭弹窗时不提前抓取敏感资料。
// 付款订单号随每份申请显示，用户可以与充值账单核对。
// 抬头来自下单快照，编辑个人资料不会让旧列表发生追溯变化。
// 金额固定使用发票人民币币种，不跟随全站余额展示配置切换。
// 每条申请可处于待付款、待开票、处理中、已开票或拒绝状态。
// 状态标签复用财务管理的可信映射，不建立两套业务解释。
// 只有已开票且确有附件时显示下载，状态变更由后端管理。
// 下载失败由统一错误解析显示，不回退为公开文件地址。
// 文件不会因列表可见就预加载，避免无意下载税务附件。
// 空列表与读取失败分别显示，不以空数组掩盖服务端错误。
// 初次加载使用独立状态，不把未读取的资料描述成没有申请。
// 手动刷新保留当前页，翻页则由查询键重新取得对应页资料。
// 组件不提供用户ID筛选，也不复用管理员的跨账户列表接口。
// 页面卸载由查询框架清理订阅，不在组件外保存税号或银行信息。
// 下一页依据服务器total，能够访问超过一百份的历史记录。
// 用户备注和发票号码按React文本转义展示，不把后端内容当HTML执行。
// 附件下载完成后由API释放对象地址，不保留浏览器临时文件引用。
// 该列表是读取与下载入口，不执行退款、开票审批或订单补单。
export function UserInvoicesList(props: Props) {
  const { t, i18n } = useTranslation() // 标签和冻结货币数字随当前语言同步格式化。
  const [page, setPage] = useState(1) // 每次重新进入列表从第一页确认最新申请。
  const invoices = useQuery({
    queryKey: ['user-invoices', page], // 不同页缓存分离，不用前页记录冒充新页。
    queryFn: () => getUserInvoices(page, 10), // 页大小与下一页判定一致，完整分页由服务器执行。
    enabled: props.open, // 关闭订单弹窗时不读取或刷新敏感发票资料。
    staleTime: 0, // 重进页签应重新读取财务已经更新的状态。
  })
  const download = async (id: number, number: string) => {
    try {
      await downloadUserInvoice(id, number) // 只提交申请ID，不提供可改写的文件路径。
    } catch (error) {
      handleServerError(error, t('Failed to download invoice'))
    }
  }
  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between gap-2'>
        <h3 className='font-medium'>{t('My invoices')}</h3>
        <Button
          variant='outline'
          size='sm'
          onClick={() => void invoices.refetch()} // 主动刷新忽略读取去重，直接获取当前状态。
          disabled={invoices.isFetching} // 同一刷新尚未结束时不重复触发按钮操作。
        >
          {t('Refresh')}
        </Button>
      </div>
      {/* 错误保持可见，用户可用上方刷新重试。 */}
      {invoices.isError ? (
        <p role='alert'>{t('Failed to load invoice requests')}</p>
      ) : null}
      {/* 初次未完成请求时不显示错误的空列表结论。 */}
      {invoices.isLoading ? (
        <p role='status'>{t('Loading invoice requests...')}</p>
      ) : null}
      {!invoices.isLoading &&
      !invoices.isError &&
      !invoices.data?.items.length ? (
        <p className='text-muted-foreground text-sm'>
          {t('No invoice requests yet')}
        </p>
      ) : null}
      {/* 服务端返回当前页的申请，不与充值列表的另一套分页强行合并。 */}
      {invoices.data?.items.map((invoice) => (
        <article
          key={invoice.id} // 用申请主键保留行身份，不以页内索引混淆下载对象。
          className='space-y-2 rounded-lg border p-3 text-sm'
        >
          <div className='flex flex-wrap justify-between gap-2'>
            <span className='font-mono break-all'>{invoice.trade_no}</span>
            <span>{t(INVOICE_STATUS_LABELS[invoice.status])}</span>
          </div>
          <p>{invoice.profile.title}</p>
          <p>
            {t('Invoice amount')}:{' '}
            {formatInvoiceAmount(
              invoice.pay_amount, // 开票金额用冻结实付，不能显示未含附加费的基础金额。
              toIntlLocale(i18n.resolvedLanguage || i18n.language) // 界面码转换为合法Intl标记。
            )}
          </p>
          {invoice.remark ? (
            <p className='text-muted-foreground'>{invoice.remark}</p>
          ) : null}
          {/* 尚未开票时号码可以为空，不显示虚构号码占位。 */}
          {invoice.invoice_number ? (
            <p>
              {t('Invoice number')}: {invoice.invoice_number}
            </p>
          ) : null}
          {/* 状态和附件同时确认后才暴露下载动作，服务端还会鉴权。 */}
          {invoice.status === 'issued' && invoice.has_file ? (
            <Button
              variant='outline'
              size='sm'
              onClick={() => void download(invoice.id, invoice.invoice_number)} // 下载标识必须属于该行申请。
            >
              {t('Download invoice')}
            </Button>
          ) : null}
        </article>
      ))}
      {/* 分页控件仅改变读取页码，不会修改申请或开票资料。 */}
      <div className='flex items-center justify-between'>
        <Button
          variant='outline'
          size='sm'
          disabled={page === 1 || invoices.isFetching} // 第一页不构造零或负页码。
          onClick={() => setPage((value) => value - 1)} // 分页改变查询键，保持真实服务端分页行为。
        >
          {t('Previous')}
        </Button>
        <span className='text-muted-foreground text-xs'>
          {t('Page {{page}}', { page })}
        </span>
        <Button
          variant='outline'
          size='sm'
          disabled={
            !invoices.data ||
            page * 10 >= invoices.data.total || // 依赖服务端总数，不以当前页条目数猜测是否结束。
            invoices.isFetching
          }
          onClick={() => setPage((value) => value + 1)} // 不把未知页数据预加载成敏感客户端全集。
        >
          {/* 未获取 total 或正在请求时禁止跳页，避免显示前一页冒充新页。 */}
          {t('Next')}
        </Button>
      </div>
    </div>
  )
}
