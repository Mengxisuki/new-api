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
import { RefreshCw } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'

type OfficialPaymentQrDialogProps = {
  open: boolean // 开关由发起付款的页面持有，组件不自行创建订单。
  onOpenChange: (open: boolean) => void // 关闭只通知调用方，不撤销或退款。
  onRefresh: () => void | Promise<void> // 钱包或订阅页分别查询自己的真实到账状态。
  qrCode: string // 服务端返回的支付内容原样编码，不拼接金额或密钥。
  tradeNo?: string // 展示订单号便于核对，不能用它作为支付成功凭据。
  paymentMethodName?: string // 渠道展示名称可覆盖通用弹窗标题。
  refreshLabel?: string // 订阅与充值共用组件，各自说明刷新目标。
}

// 充值与订阅共享支付展示，最终结果由各自页面刷新服务器数据确认。
// 组件没有轮询或余额加算，避免二维码已打开就误报付款成功。
export function OfficialPaymentQrDialog(props: OfficialPaymentQrDialogProps) {
  const { t } = useTranslation()

  return (
    <Dialog
      open={props.open} // 公共弹窗保留焦点管理与键盘关闭行为。
      onOpenChange={props.onOpenChange} // 关闭决定留给发起支付的页面处理。
      title={props.paymentMethodName || t('Payment QR code')}
      description={t('Scan the QR code with the corresponding payment app')}
      contentClassName='sm:max-w-sm' // 窄弹窗容纳二维码，手机仍限制在可用视口。
      showCloseButton // 用户可随时关闭展示，不把关闭解释为撤销付款。
      footer={
        <Button
          /* 刷新按钮不提交外层的订阅或充值表单。 */ type='button'
          className='w-full'
          onClick={props.onRefresh} // 刷新动作由调用方查询对应订单或订阅状态。
        >
          <RefreshCw
            /* 装饰图标不重复朗读按钮名称。 */ className='size-4'
            aria-hidden='true'
          />
          {props.refreshLabel || t('Payment completed, refresh balance')}
        </Button>
      }
    >
      <div className='flex flex-col items-center gap-4 py-3'>
        <div className='rounded-md border bg-white p-4'>
          <QRCodeSVG
            value={props.qrCode} // 使用项目现有二维码库，不另写编码算法。
            size={224} // 固定清晰像素尺寸，缩放交给响应式宽度。
            className='h-auto w-full max-w-56'
            level='M' // 中等纠错适合支付应用直接扫码。
          />
        </div>
        {props.tradeNo ? (
          <div className='text-muted-foreground max-w-full text-center text-xs'>
            <span>{t('Order number')}: </span>
            <code
              /* 长订单号允许换行，不撑破手机弹窗。 */ className='break-all'
            >
              {props.tradeNo}
            </code>
          </div>
        ) : null}
      </div>
    </Dialog>
  )
}
