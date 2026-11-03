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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { downloadUserInvoice } from '@/features/invoices/api'
import { formatInvoiceAmount } from '@/features/invoices/format'
import { INVOICE_STATUS_LABELS } from '@/features/invoices/invoice-status'
import { toIntlLocale } from '@/i18n/languages'
import { handleServerError } from '@/lib/handle-server-error'

import type { OrderInvoice } from './api'

// 卡片只负责当前买家订单的开票摘要，数据与刷新由订单详情管理。
// 沿用详情的十秒轮询，不为同一申请叠加独立定时器。
// 关闭订单即卸载卡片，下次进入重新确认服务器状态。
// 已有钱包列表含独立分页和查询，不能直接嵌入单笔订单。
// 复用发票状态映射、人民币格式化和鉴权下载，避免另起开票规则。
// 申请在下单时冻结，这里不提供事后补申请入口。
// 展示不会改变付款或财务状态，也不重新计算附加费。
// 未付款的申请仍可存在，不能把存在申请解释为已收款。
// 不查询当前个人抬头，避免旧单显示后来修改的名称。
// 不在浏览器按订单状态猜测开票状态：代充值完成不等于已开票。
// 未申请显示明确说明，已申请却没有摘要则显示读取失败。
// 标题取自下单快照，修改个人抬头不会改写本卡片。
// 业务员界面不挂载本组件，服务端也不向运营读单返回税务摘要。
export function RechargeOrderInvoice(props: {
  required: boolean // 冻结选择，不能从发票对象是否存在反推。
  invoice: OrderInvoice | null | undefined // 缺失摘要不意味着正在开票。
}) {
  // 独立订阅语言，无须重新请求订单就能切换标签。
  // 界面语言码需转换，避免 zhCN/zhTW 触发 Intl 异常。
  const { t, i18n } = useTranslation()
  const [downloading, setDownloading] = useState(false) // 下载不阻塞右侧办理。
  const invoice = props.invoice
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  // 申请 ID 决定下载对象，发票号码仅决定保存的文件名。
  // 下载失败不会创建替代申请或改用其他订单附件。
  // 统一错误处理避免打印含税务内容的 HTTP 对象。
  // Blob 生命周期由公共函数管理，组件不保留副本。
  // 下载仅在用户点击后进行，不随轮询预取附件。
  // 错误使用统一提示，不回退到公开链接或伪造成功。
  // 同一次下载禁用按钮，结束后允许再次下载。
  async function download() {
    if (!invoice || downloading) return
    setDownloading(true)
    try {
      await downloadUserInvoice(invoice.id, invoice.invoice_number) // 不能传代充值订单 ID。
    } catch (error) {
      handleServerError(error, t('Failed to download invoice'))
    } finally {
      setDownloading(false)
    }
  }
  // 状态和附件必须同时满足；文件的最终访问权限由服务端确认。
  // 没有附件的已开票记录应可见报错，不能留下无响应的下载按钮。
  // 状态与财务模块共享解释，避免同状态在两个页面文案不同。
  // 只展示抬头名，不重复税号、地址及银行账号等私密字段。
  // 财务公开备注不包含代充值内部操作记录。
  // alert 让辅助技术区分异常与正常等待状态。
  // 卡片继承左栏滚动，长抬头和备注换行，不拉高整个弹窗。
  // 备注在开票区域独立标注，不能与补交账号资料的原因混淆。
  // 抬头和号码是纯文本，不能把后台内容解释成 HTML。
  // 发票金额使用实付快照，已含附加费，不再次叠加。
  // 未申请时不展示旧查询缓存中的发票内容。
  return (
    <section
      aria-label={t('Invoice information')}
      className='space-y-3 rounded-xl border p-4 text-sm'
    >
      <h3 className='font-semibold'>{t('Invoice information')}</h3>
      {!props.required && (
        <p className='text-muted-foreground'>{t('No invoice requested')}</p>
      )}
      {props.required && !invoice && (
        <p role='alert'>{t('Failed to load invoice requests')}</p>
      )}
      {props.required && invoice && (
        <>
          {/* 明确标注状态，颜色只辅助辨识；与订单办理状态分别展示。 */}
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <span className='text-muted-foreground'>{t('Invoice status')}</span>
            <Badge
              variant={
                invoice.status === 'rejected' ? 'destructive' : 'secondary'
              }
            >
              {t(INVOICE_STATUS_LABELS[invoice.status])}
            </Badge>
          </div>
          {/* 字段逐项有标签，手机和长内容场景均能对应名称与值。
              抬头单独占一行，企业长名称不会挤占金额空间。
              号码允许换行，连续数字不会撑宽手机视口。
              尚无号码不补零，也不借用支付单号冒充。 */}
          <dl className='grid grid-cols-2 gap-3'>
            <div className='col-span-2'>
              <dt className='text-muted-foreground text-xs'>
                {t('Invoice title')}
              </dt>
              <dd className='mt-1'>{invoice.title}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground text-xs'>
                {t('Invoice amount')}
              </dt>
              <dd className='mt-1 font-medium'>
                {formatInvoiceAmount(invoice.pay_amount, locale)}
              </dd>
            </div>
            {invoice.invoice_number && (
              <div>
                <dt className='text-muted-foreground text-xs'>
                  {t('Invoice number')}
                </dt>
                <dd className='mt-1 break-all'>{invoice.invoice_number}</dd>
              </div>
            )}
          </dl>
          {/* 财务备注包含拒绝原因；空备注不制造占位内容。
              保留财务输入的换行，使多项处理说明可读。
              拒绝时仍显示抬头及金额供用户核对。 */}
          {invoice.remark && (
            <div className='bg-muted/50 space-y-1 rounded-lg p-3'>
              <p className='text-muted-foreground text-xs'>
                {t('Invoice remarks')}
              </p>
              <p className='whitespace-pre-wrap'>{invoice.remark}</p>
            </div>
          )}
          {/* 未开票没有下载入口，已开票才检查附件是否齐全。
              下载不离开弹窗，避免丢失右侧未提交资料。
              缺文件是异常，不能重新描述成正在开票。
              不展示磁盘路径，也不创建可公开传播的附件链接。 */}
          {invoice.status === 'issued' && !invoice.has_file && (
            <p role='alert'>{t('Invoice file is unavailable')}</p>
          )}
          {invoice.status === 'issued' && invoice.has_file && (
            <Button
              variant='outline'
              size='sm'
              disabled={downloading}
              onClick={() => void download()}
            >
              {t('Download invoice')}
            </Button>
          )}
        </>
      )}
    </section>
  )
}
