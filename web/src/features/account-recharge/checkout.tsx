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
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  getInvoiceProfile,
  INVOICE_PROFILE_QUERY_KEY,
} from '@/features/invoices/api'
import { InvoiceSelection } from '@/features/wallet/components/invoice-selection'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  createOrder,
  quoteOrder,
  type Catalog,
  type Order,
  type Product,
  type Purchase,
} from './api'
import { RechargeProductInfo } from './product-info'

// 结账先获取可信报价，
// 用户确认后才创建订单；
// 付款在订单详情继续。
// 余额开关关闭时根本不呈现余额选项，
// 后端仍会再次验证。
// UUID绑定一次结账选择，
// 结果未知时保持选择和请求键，
// 避免双单。
// 本弹层只负责确认商品及支付选择，
// 不承担到账判定。
// 目录中的官方渠道和网关微信都保持服务器分配的稳定标识。
// 默认选第一个实际可用微信渠道，
// 不能构造未配置的假支付按钮。
// 只有商品明确允许余额时才添加余额选项，
// 默认保持关闭。
// 是否开票的选择与当前支付方式一起进入报价及创建请求。
// 余额付款不重复申请发票，
// 原钱包充值开票记录保持原有规则。
// 发票资料通过现有公共组件编辑，
// 不再维护一份不同的抬头表单。
// 报价失败时保留用户选择并显示原因，
// 不用商品原价忽略附加费。
// 订单创建前展示固定人民币总价，
// 不受站点显示币种变化影响。
// 创建发生后冻结结账选择，
// 防止同一幂等键被拿去买另一商品。
// 请求不确定时再次点击仍使用原UUID，
// 关闭后可去我的订单核对。
// 关闭弹层不意味着取消已经创建的支付订单，
// 不能自动退款。
// onCreated只在服务器确认订单保存后执行，
// 随后进入原单详情。
// 没有配置加密或支付前置条件时禁止继续，
// 避免付费后无法提交。
// 商品说明完整展示，
// 处理时效和账户条件不能仅藏在后台配置。
// 表单输入不会自动写入localStorage，
// 刷新后以服务器订单为准。
// 忙碌标志只防止重复点击，
// 不代替数据库的幂等与金额核对。
// 回调成功前不得跳出已完成文案，
// 商品购买与人工办理是两步。
// 初始支付选择可以为空，
// 此时不能发起无渠道报价或创建。
// 开票资料加载中不能把undefined当成已经保存的抬头。
// 用户主动启用开票后才读取其抬头，
// 默认页面不预读税号等资料。
// 支付方式改成余额后，
// 开票布尔值在最终请求中强制为 false。
// 用户编辑抬头成功会更新共享缓存，
// 并重新计算含附加费的报价。
// 报价 query key 包含全部购买选择，
// 过期报价不能复用到另一方式。
// 请求发出后保留原购买参数，
// 发票资料后来变化也不能偷偷改原单。
// 重试按钮只恢复订单创建，
// 不在创建过程中同时发起资金扣除。
// 原订单返回已付款状态时仍直接打开原单，
// 不能生成新的支付交易。
// 界面中 CNY 是商品计价单位，
// 展示数值不能用于模型调用额度。
// 价税总额来自服务器，
// 界面不自行计算舍入或附加费比例。
// 商品说明为纯文本，
// 里面的 URL不会变成自动提交或执行脚本。
// 调用方提供关闭回调仅控制当前窗口，
// 不触发取消和退款 API。
// 创建成功由父页面负责失效订单列表，
// 结账组件不持有另一份购买历史。
// 支付配置读取失败时禁用实际购买，
// 但原有订单仍可在列表继续核对。
export function RechargeCheckout(props: {
  product: Product
  catalog: Catalog
  onClose: () => void
  onCreated: (order: Order) => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const [method, setMethod] = useState(
    props.catalog.payment_methods[0]?.id ?? ''
  )
  const [invoice, setInvoice] = useState(false)
  const [busy, setBusy] = useState(false)
  const locked = useRef<Purchase | null>(null)
  const [attempted, setAttempted] = useState(false)
  const invoiceProfile = useQuery({
    queryKey: INVOICE_PROFILE_QUERY_KEY,
    queryFn: getInvoiceProfile,
    enabled: invoice && method !== 'wallet',
  })
  // 选择变更自动使用独立报价键，
  // 不复用上一支付方式的价格。
  const purchase: Purchase = {
    product_id: props.product.id,
    product_version: props.product.version,
    payment_method: method,
    invoice_required: invoice && method !== 'wallet',
    request_id: '',
  }
  const quote = useQuery({
    queryKey: ['recharge', 'quote', purchase, invoiceProfile.data],
    queryFn: () => quoteOrder(purchase),
    enabled: !!method && (!purchase.invoice_required || !!invoiceProfile.data),
    retry: false,
  })
  async function checkout() {
    if (busy || !quote.data) return
    setBusy(true)
    // 一旦请求发出，
    // 重试只能恢复原请求；
    // 返回列表也能查到服务端已创建订单。
    locked.current ??= {
      ...purchase,
      request_id: crypto.randomUUID(),
      quoted_cents: quote.data.money_cents,
    }
    setAttempted(true)
    try {
      props.onCreated(await createOrder(locked.current))
    } catch (error) {
      handleServerError(error)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) props.onClose()
      }}
      title={t('Confirm recharge order')}
      description={props.product.title}
      footer={
        <Button
          disabled={
            busy || !quote.data || quote.isFetching || !props.catalog.configured
          }
          onClick={() => void checkout()}
        >
          {t('Create order and continue')}
        </Button>
      }
    >
      <div className='space-y-4'>
        <RechargeProductInfo product={props.product} />
        {/* 原生选择具备键盘语义；冻结后避免改变已发送的幂等请求内容。 */}
        <Field>
          <FieldLabel htmlFor='recharge-method'>
            {t('Payment method')}
          </FieldLabel>
          <NativeSelect
            id='recharge-method'
            value={method}
            disabled={attempted}
            onChange={(e) => setMethod(e.target.value)}
          >
            <NativeSelectOption value='' disabled>
              {t('Select payment method')}
            </NativeSelectOption>
            {props.catalog.payment_methods.map((item) => (
              <NativeSelectOption key={item.id} value={item.id}>
                {t(item.name)}
              </NativeSelectOption>
            ))}
            {props.product.allow_wallet_payment && (
              <NativeSelectOption value='wallet'>
                {t('Platform balance')}
              </NativeSelectOption>
            )}
          </NativeSelect>
        </Field>
        {method === 'wallet' ? (
          <p className='text-muted-foreground text-sm'>
            {t(
              'Balance payments produce a spending record and do not issue another invoice.'
            )}
          </p>
        ) : (
          <InvoiceSelection
            required={invoice}
            onRequiredChange={(value) => {
              if (!attempted) setInvoice(value)
            }}
            profile={invoiceProfile.data}
            loading={invoiceProfile.isLoading}
            error={invoiceProfile.isError}
            onRetry={() => void invoiceProfile.refetch()}
            onSaved={(profile) =>
              client.setQueryData(INVOICE_PROFILE_QUERY_KEY, profile)
            }
          />
        )}
        {/* 金额固定人民币，不套用平台美元或 TOKEN 的显示配置。 */}
        {quote.data && (
          <p className='text-xl font-semibold'>
            {t('Total')}: CNY{' '}
            {formatNumber(quote.data.money_cents / 100, locale)}
          </p>
        )}
        {quote.isError && (
          <p role='alert'>
            {t(
              'Recharge quote is unavailable. Check the product, payment method and invoice profile.'
            )}
          </p>
        )}
        {attempted && (
          <p role='status'>
            {t(
              'Retry continues the same order. Check My orders if the result is uncertain.'
            )}
          </p>
        )}
      </div>
    </Dialog>
  )
}
