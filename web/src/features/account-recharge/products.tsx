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
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { StaticDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { LobeIconField } from '@/components/lobe-icon-field'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { toIntlLocale } from '@/i18n/languages'
import { hasPermission } from '@/lib/admin-permissions'
import { formatNumber } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { getProducts, saveProduct, type Product } from './api'

// 商品金额编辑采用分作为精确输入单位，
// 避免浮点乘法保存意外尾差。
// 初始余额支付关闭，
// 管理员必须按商品主动启用。
// 草稿不上架也不开放余额，
// 避免管理员尚未填写规则就开始售卖。
// 售价保存整数分而非浮点元，
// 输入单位在标签里明确标示。
// 描述支持多行，
// 用于列出账户条件及售后说明，周期和时效有独立字段。
// 校验规则覆盖数据库允许的长度，
// 减少保存时发生截断。
// 商品编号和版本不作为可编辑字段，
// 来自选中的数据库记录。
// 排序只影响卡片展示，
// 不参与订单身份或收款金额计算。
const draft: Product = {
  id: 0,
  title: '',
  description: '',
  summary: '',
  provider: '',
  period: '',
  delivery_time: '',
  icon: '',
  price_cents: 100,
  enabled: false, // 新商品先保存草稿，由管理员明确上架。
  allow_wallet_payment: false, // 默认只允许微信，余额须逐商品授权。
  sort_order: 0,
  version: 0,
}
const schema = z.object({
  title: z.string().trim().min(1).max(128),
  description: z.string().max(4000),
  summary: z.string().max(500),
  provider: z.string().max(80),
  period: z.string().max(80),
  delivery_time: z.string().max(160),
  icon: z.string().max(128),
  price_cents: z.number().int().min(1).max(10000000),
  enabled: z.boolean(),
  allow_wallet_payment: z.boolean(),
  sort_order: z.number().int(),
})

// 表单仅编辑商品白名单字段，
// 不提供供应商配置或任意动态账户表单。
// 版本来自打开时快照，
// 冲突时服务端拒绝覆盖最新商品。
// 管理员打开已有商品时保留原版本，
// 提交失败不更新本地版本。
// 表单解析只保留可编辑字段，
// 不能夹带其他用户或订单属性。
// 服务端成功后失效商品查询，
// 用户页和后台页下次读取均得到新配置。
// 价格和上下架修改不回写历史订单，
// 订单拥有自己的购买快照。
// 关闭编辑窗只丢弃本地草稿，
// 不假定服务器已经保存。
// 提交过程中按钮不可重复点击，
// 数据库仍负责跨窗口版本竞争。
// 余额开关与商品上架开关独立，
// 开启余额不隐式上架商品。
// 微信支付配置继续由既有系统设置管理，
// 商品页不重复存商户密钥。
// 业务员不具备此表单入口，
// 普通管理员还须拥有商品维护能力。
// 校验错误保留表单输入并显示提示，
// 不用无提示的默认值覆盖。
// 商品说明以纯文本保存和展示，
// 不将管理员输入直接注入HTML。
// 没有额外自动化充值配置，
// 人工订单不需要上游 SKU或回调密钥。
// 支付方式的实际可用性由服务端渠道配置决定，
// 不由商品文案决定。
// 已有商品关闭余额支付仍保留其先前已付订单，
// 退款按原扣除值执行。
// 商品标题需要非空且有限长度，
// 避免空标题订单难以核对。
// 金额必须为正整数分，
// 不能通过零价绕过付款及资料提交约束。
// 排序值只用于展示，
// 不允许借排序字段拼接数据库排序表达式。
// 表单错误采用公共样式提示，
// 字段输入在失败后不会自动重置。
// 默认保存草稿不上架，
// 配置商户与资料密钥完成后再公开售卖。
// 没有开放商品硬删除，
// 避免历史订单失去关联而无法审计。
// 管理员可保存多种套餐，
// 实际套餐周期和履约规则以说明快照为准。
// 编辑页不提供上游自动充值开关，
// 减少与当前人工流程不一致的误导。
// 保存成功之后只刷新商品缓存，
// 不以配置成功推断任何订单已完成。
function ProductEditor(props: { product: Product; onClose: () => void }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: props.product, // 打开时的版本留到提交，防止覆盖他人编辑。
  })
  async function submit(values: z.infer<typeof schema>) {
    try {
      await saveProduct({ ...props.product, ...values }) // 保留商品编号，不能把编辑误作新增。
      await client.invalidateQueries({ queryKey: ['recharge', 'products'] })
      props.onClose()
    } catch (error) {
      handleServerError(error)
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Edit recharge product')}
      contentClassName='sm:max-w-3xl'
    >
      <form className='space-y-4' onSubmit={form.handleSubmit(submit)}>
        <Field>
          <FieldLabel htmlFor='product-title'>{t('Product name')}</FieldLabel>
          <Input id='product-title' {...form.register('title')} />
        </Field>
        {/* 简介回答购买什么，购买说明承载账户限制和售后约定。 */}
        <Field>
          <FieldLabel htmlFor='product-summary'>{t('Plan summary')}</FieldLabel>
          <Textarea
            id='product-summary'
            rows={2}
            maxLength={500}
            {...form.register('summary')}
          />
        </Field>
        {/* 沿用模型广场图标键，管理员可搜索或直接填写。 */}
        <Field>
          <FieldLabel htmlFor='product-icon'>{t('Plan icon')}</FieldLabel>
          <Controller
            name='icon'
            control={form.control}
            render={({ field }) => (
              <LobeIconField
                id='product-icon'
                value={field.value}
                onChange={field.onChange}
              />
            )}
          />
        </Field>
        {/* 结构信息各自保存，空值不会自动补出未经承诺的时效。 */}
        <div className='grid gap-4 sm:grid-cols-2'>
          <Field>
            <FieldLabel htmlFor='product-provider'>
              {t('Service provider')}
            </FieldLabel>
            <Input
              id='product-provider'
              maxLength={80}
              {...form.register('provider')}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor='product-period'>{t('Plan period')}</FieldLabel>
            <Input
              id='product-period'
              maxLength={80}
              {...form.register('period')}
            />
          </Field>
          <Field className='sm:col-span-2'>
            <FieldLabel htmlFor='product-delivery'>
              {t('Estimated processing time')}
            </FieldLabel>
            <Input
              id='product-delivery'
              maxLength={160}
              {...form.register('delivery_time')}
            />
          </Field>
        </div>
        {/* 旧描述原样保留；新增字段不会从旧文案猜测拆分。 */}
        <Field>
          <FieldLabel htmlFor='product-description'>
            {t('Purchase instructions')}
          </FieldLabel>
          <Textarea
            id='product-description'
            rows={4}
            maxLength={4000}
            {...form.register('description')}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor='product-price'>
            {t('Price in CNY cents')}
          </FieldLabel>
          <Input
            id='product-price'
            type='number'
            min={1}
            step={1}
            {...form.register('price_cents', { valueAsNumber: true })}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor='product-sort'>{t('Sort order')}</FieldLabel>
          <Input
            id='product-sort'
            type='number'
            step={1}
            {...form.register('sort_order', { valueAsNumber: true })}
          />
        </Field>
        {/* 布尔 false 必须提交，不能依靠省略字段来执行下架。 */}
        <label className='flex items-center gap-2'>
          <Controller
            name='enabled'
            control={form.control}
            render={({ field }) => (
              <Checkbox
                checked={field.value}
                onCheckedChange={field.onChange}
              />
            )}
          />
          {t('Available for purchase')}
        </label>
        <label className='flex items-center gap-2'>
          <Controller
            name='allow_wallet_payment'
            control={form.control}
            render={({ field }) => (
              <Checkbox
                checked={field.value}
                onCheckedChange={field.onChange}
              />
            )}
          />
          {t('Allow platform balance payment')}
        </label>
        <p className='text-muted-foreground text-sm'>
          {t(
            'WeChat payment uses the configured payment gateway or official WeChat channel. Balance payment is optional for each product.'
          )}
        </p>
        {Object.keys(form.formState.errors).length > 0 && (
          <p role='alert' className='text-destructive'>
            {t('Check the product name, price and description.')}
          </p>
        )}
        <Button type='submit' disabled={form.formState.isSubmitting}>
          {t('Save')}
        </Button>
      </form>
    </Dialog>
  )
}

// 商品读取和维护分别检查能力；
// 只读管理员看得到商品但不能修改。
// 业务员不具备商品权限，
// 路由与后端同时拒绝此工作区。
// 列表读取和写入能力分别判断，
// 只读权限不显示新增或编辑按钮。
// 下架商品仍可见并可编辑，
// 不能因用户页隐藏而丢失管理入口。
// 页面显示数据库实际保存的两个开关，
// 不能用存在性推定true。
// 选择商品后按编号及版本挂载表单，
// 防止上一商品草稿污染下一商品。
// 服务端版本冲突通过统一错误提示报告，
// 需要用户重新读取后核对。
// 表格是既有公共封装，
// 避免另写一套复制、截断和手机滚动行为。
// 商品状态不能被误读为订单处理状态，
// 两者在不同管理页维护。
// 页面中的金额保持人民币计价，
// 不把站点钱包单位套用到商品售价。
// 编辑弹层放在 Content插槽里，
// 避免布局丢弃普通子节点。
// 查询失败提供重试入口，
// 不能在失败时允许基于空数据覆盖已有商品。
// 编辑保存只改变当前商品，
// 不能批量重写其他套餐的余额开关。
export function RechargeProducts() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const user = useAuthStore((state) => state.auth.user)
  const canRead = hasPermission(user, 'recharge_products', 'read')
  const canWrite = hasPermission(user, 'recharge_products', 'write')
  const [editing, setEditing] = useState<Product | null>(null)
  const query = useQuery({
    queryKey: ['recharge', 'products', 'admin'],
    queryFn: () => getProducts(true),
    enabled: canRead, // 无读取能力时不请求后台商品。
  })
  if (!canRead) return <ErrorState title={t('Access denied')} />
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Recharge product management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {canWrite && (
          <Button onClick={() => setEditing(draft)}>{t('Add product')}</Button>
        )}
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        {query.isLoading && <LoadingState />}
        {query.isError && <ErrorState onRetry={() => void query.refetch()} />}
        {query.data && (
          <StaticDataTable
            data={query.data.items}
            getRowKey={(item) => item.id}
            columns={[
              { id: 'title', header: t('Product'), cell: (item) => item.title },
              {
                id: 'price',
                header: t('Price'),
                cell: (item) =>
                  `CNY ${formatNumber(item.price_cents / 100, locale)}`,
              },
              {
                id: 'enabled',
                header: t('Status'),
                cell: (item) =>
                  t(item.enabled ? 'Available for purchase' : 'Disabled'),
              },
              {
                id: 'wallet',
                header: t('Platform balance'),
                cell: (item) =>
                  t(item.allow_wallet_payment ? 'Allowed' : 'Not allowed'),
              },
              {
                id: 'action',
                header: t('Actions'),
                cell: (item) =>
                  canWrite ? (
                    <Button variant='outline' onClick={() => setEditing(item)}>
                      {t('Edit')}
                    </Button>
                  ) : null,
              },
            ]}
          />
        )}
        {editing && (
          <ProductEditor
            key={`${editing.id}-${editing.version}`}
            product={editing}
            onClose={() => setEditing(null)}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
