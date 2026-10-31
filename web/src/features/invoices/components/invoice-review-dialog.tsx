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
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { toIntlLocale } from '@/i18n/languages'
import { handleServerError } from '@/lib/handle-server-error'

import {
  downloadFinanceInvoice,
  updateFinanceInvoice,
  uploadFinanceInvoice,
} from '../admin-api'
import { formatInvoiceAmount } from '../format'
import { INVOICE_STATUS_LABELS } from '../invoice-status'
import type { InvoiceRequest } from '../types'

type Props = {
  invoice: InvoiceRequest // 只读订单快照，不向抬头配置接口回写历史资料。
  canWrite: boolean // 页面能力与服务端权限共同约束操作。
  onClose: () => void // 关闭详情释放表单，父级不保留上传文件引用。
  onUpdated: () => void // 成功办理后刷新列表，不能只刷新当前状态标签。
}

// 财务详情集中展示冻结抬头、实付金额和处理动作。
// 当前较长部分是声明式表单，办理资格只有canProcess一个界面判断。
// 状态推进仍由服务端行锁与付款标记再次校验，页面按钮不能代表授权。
// 发票完成必须同时包含附件与号码，选择文件本身不意味着上传成功。
// 只读用户可以核对订单与下载已开票文件，但看不到写操作表单。
// 未付款申请保留查询价值，不能因为存在抬头就被提前标记处理中。
// 开具或拒绝是当前人工流程终态，不在详情提供隐式重开和自动退款。
// 拒绝前要求说明并使用项目确认弹窗，避免误触后静默结束申请。
// 附加费退还需要实际收款核对，拒绝按钮只修改申请状态。
// 名称、税号、邮箱和银行资料均来自下单快照，不读取现时个人资料替代。
// 本表单只允许填写财务处理意见，不能编辑冻结的抬头和收款金额。
// 详情中的发票号码对已开票记录也可读，不能只保留在隐藏的编辑框中。
// 文件仅保存在当前弹窗状态，关闭或切换申请后随组件释放。
// 文件扩展名只是选取提示，客户端检查MIME和大小，服务端检查真实内容。
// 上传失败保留表单供用户重试，成功后刷新列表并释放当前详情。
// 状态写入和附件下载使用独立忙状态，互不伪造对方的成功结果。
// 父组件按申请ID重新挂载本组件，草稿不会被另一份记录误用。
// 服务器并发拒绝仍展示具体错误，不采用静默覆盖最后一份办理结果。
// 交易金额使用固定人民币格式化，不能随全站余额显示币种重新标价。
// 备注长度受浏览器与服务端双重约束，不能把无限输入送进财务记录。
// 对账文件采用认证下载，不接受用户填入第三方URL或任意本地路径。
// 详情不发起商户退款、模型请求或自动税务开票等范围外操作。
export function InvoiceReviewDialog(props: Props) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language) // 格式语言变更不会改动交易币种。
  const [remark, setRemark] = useState(props.invoice.remark) // 备注局限在当前弹窗，不持久化敏感草稿。
  const [number, setNumber] = useState(props.invoice.invoice_number) // 原有号码进入只读详情及编辑草稿。
  const [confirmReject, setConfirmReject] = useState(false) // 拒绝会结束申请，复用项目确认流程避免误触。
  const [file, setFile] = useState<File | null>(null) // 不把文件写进用户资料或浏览器持久缓存。
  const canProcess = // 写能力与付款后的可办理状态共同决定是否显示表单。
    props.canWrite && ['pending', 'processing'].includes(props.invoice.status)
  const mutation = useMutation({
    // 只有服务端确认成功才通知父列表更新。
    mutationFn: async (action: 'processing' | 'rejected' | 'upload') => {
      // 内部动作枚举不开放任意服务端状态。
      if (action === 'upload') {
        // 附件与号码一并完成，不能只设置issued标签。
        if (!number.trim() || !file) {
          // 再次校验动作时的状态，不能只依赖按钮禁用。
          throw new Error(t('Enter the invoice number and choose a file.'))
        }
        // 快速拒绝超大或不支持的文件；服务端仍再次检查真实字节。
        if (
          file.size > 5 * 1024 * 1024 ||
          !['application/pdf', 'image/png', 'image/jpeg'].includes(file.type)
        ) {
          throw new Error(t('Choose a PDF, PNG or JPEG file up to 5 MiB.'))
        }
        return uploadFinanceInvoice(
          props.invoice.id,
          number.trim(),
          file,
          remark.trim()
        )
      }
      if (action === 'rejected' && !remark.trim()) {
        // 用户需要能读到拒绝的具体原因。
        throw new Error(t('Provide a reason before rejecting this invoice.'))
      }
      return updateFinanceInvoice(
        props.invoice.id,
        action,
        remark.trim(),
        number.trim()
      )
    },
    onSuccess: () => {
      toast.success(t('Invoice request updated')) // 成功提示依据真实响应，而不是选择了一个文件。
      props.onUpdated() // 失效服务端查询，让其它页也能读取最新办理状态。
      props.onClose() // 成功后关闭，避免对终态申请重复点击上传。
    },
    onError: (error) =>
      handleServerError(error, t('Failed to update invoice request')),
  })
  // 文件仅在用户主动下载时获取，不因打开详情预加载税务附件。
  const download = useMutation({
    // 读取附件不改变申请进度，也不触发二次开票。
    mutationFn: () => downloadFinanceInvoice(props.invoice.id), // 下载只传ID，服务器决定附件归属与路径。
    onError: (error) =>
      handleServerError(error, t('Failed to download invoice')),
  })
  const details = [
    [t('Order number'), props.invoice.trade_no], // 支付单号与开票号是不同身份，不能互相覆盖。
    [t('Status'), t(INVOICE_STATUS_LABELS[props.invoice.status])], // 服务端状态是唯一办理进度来源。
    [t('Invoice title'), props.invoice.profile.title], // 历史抬头不随用户后续配置变更。
    [t('Invoice number'), props.invoice.invoice_number], // 已开票后仍可核对录入的号码。
    [t('Tax ID'), props.invoice.profile.tax_id], // 企业税号仅在已授权详情中读取。
    [t('Email'), props.invoice.profile.email], // 邮箱用于人工开票资料核对，本页不自动发送邮件。
    [t('Address'), props.invoice.profile.address],
    [t('Phone'), props.invoice.profile.phone],
    [t('Bank name'), props.invoice.profile.bank_name],
    [t('Bank account'), props.invoice.profile.bank_account], // 银行资料不进入列表URL或下载文件名。
  ]
  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open) props.onClose() // 取消关闭不提交任何草稿。
        }}
        title={t('Review invoice')} // 详情名称同时用于辅助技术定位。
        description={t(
          'Review the paid order and the invoice details saved at checkout.'
        )}
      >
        <div className='space-y-5'>
          <dl className='grid gap-3 sm:grid-cols-2'>
            {/* 可选资料为空时不生成空白字段，不自行补充默认税号。 */}
            {details
              .filter(([, value]) => value)
              .map(([label, value]) => (
                <div key={label} className='min-w-0'>
                  {' '}
                  {/* 名称与值采用原生定义列表关联。 */}
                  <dt className='text-muted-foreground text-xs'>{label}</dt>
                  <dd className='text-sm break-all'>{value}</dd>{' '}
                  {/* 长订单号不会撑破窄屏详情。 */}
                </div>
              ))}
          </dl>
          <p className='rounded-md border p-3 text-sm'>
            {t('Base payment')}:{' '}
            {formatInvoiceAmount(props.invoice.base_amount, locale)}
            {' · '}
            {t('Invoice surcharge')}:{' '}
            {formatInvoiceAmount(props.invoice.fee_amount, locale)}
            {' · '}
            {t('Paid amount')}:{' '}
            {formatInvoiceAmount(props.invoice.pay_amount, locale)}
          </p>
          {props.invoice.remark && !canProcess && (
            <p className='text-muted-foreground text-sm whitespace-pre-wrap'>
              {props.invoice.remark}
            </p>
          )}
          {/* 已开票附件只在主动点击时请求，不在浏览详情时预取文件。 */}
          {props.invoice.has_file && (
            <Button
              variant='outline'
              onClick={() => download.mutate()}
              disabled={download.isPending} // 单次下载期间防止连续生成多个Blob地址。
            >
              {t('Download invoice')}
            </Button>
          )}
          {props.invoice.status === 'pending_payment' && (
            <p role='status' className='text-muted-foreground text-sm'>
              {t(
                'Payment must be completed before this invoice can be processed.'
              )}
            </p>
          )}
          {/* 写权限和可办理状态必须同时满足，不能只按角色显示表单。 */}
          {canProcess && (
            <div className='space-y-4 border-t pt-4'>
              <div className='space-y-2'>
                <Label htmlFor='invoice-number'>{t('Invoice number')}</Label>
                <Input
                  id='invoice-number'
                  value={number} // 号码属于财务填写信息，不与支付交易号混用。
                  maxLength={64} // 与服务端发票号长度边界一致。
                  onChange={(event) => setNumber(event.target.value)} // 只更新当前号码草稿，上传前不会写申请记录。
                />
              </div>
              <div className='space-y-2'>
                <Label htmlFor='invoice-file'>{t('Invoice file')}</Label>
                <Input
                  id='invoice-file' // 文件字段与标签关联，键盘用户可明确选择附件。
                  type='file' // 真实文件提交，不接受任意外部附件URL替代。
                  accept='.pdf,.png,.jpg,.jpeg' // 只提示允许格式，真正内容仍由服务器检查。
                  onChange={(event) => setFile(event.target.files?.[0] ?? null)} // 取消选择恢复无文件，不能误用上次文件。
                />
                <p className='text-muted-foreground text-xs'>
                  {t('Choose a PDF, PNG or JPEG file up to 5 MiB.')}
                </p>
              </div>
              <div className='space-y-2'>
                <Label htmlFor='invoice-remark'>{t('Finance remark')}</Label>
                <Textarea
                  id='invoice-remark'
                  value={remark} // 失败时保留备注供修正或重新提交。
                  maxLength={1000} // 限长与服务端人工备注约束保持一致。
                  onChange={(event) => setRemark(event.target.value)} // 草稿变化不会提前修改申请。
                />
              </div>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Rejecting a request does not refund the surcharge. Handle any refund separately.'
                )}
              </p>
              <div className='flex flex-wrap gap-2'>
                {props.invoice.status === 'pending' && (
                  <Button
                    variant='outline'
                    disabled={mutation.isPending}
                    onClick={() => mutation.mutate('processing')} // 此动作不代表已开具发票。
                  >
                    {t('Start processing')}
                  </Button>
                )}
                <Button
                  disabled={mutation.isPending || !file || !number.trim()} // 不完整发票资料和重复上传都应在界面阻止。
                  onClick={() => mutation.mutate('upload')} // 以multipart提交当前文件、号码和备注。
                >
                  {t('Upload invoice')}
                </Button>
                <Button
                  variant='destructive'
                  disabled={mutation.isPending || !remark.trim()} // 拒绝前必须能给用户可见的原因。
                  onClick={() => setConfirmReject(true)} // 先确认终止申请，不能误触立即写终态。
                >
                  {t('Reject invoice')}
                </Button>
              </div>
            </div>
          )}
        </div>
      </Dialog>
      <ConfirmDialog
        open={confirmReject} // 终止申请需要用户明确确认该动作。
        onOpenChange={setConfirmReject} // 取消确认只关闭提示，不修改申请状态。
        destructive
        title={t('Reject invoice')}
        confirmText={t('Reject invoice')}
        desc={t(
          'Rejecting a request does not refund the surcharge. Handle any refund separately.'
        )}
        isLoading={mutation.isPending} // 提交期间避免同一拒绝动作被反复触发。
        handleConfirm={() => mutation.mutate('rejected')} // 不调用任何余额或退款接口。
      />
    </>
  )
}
