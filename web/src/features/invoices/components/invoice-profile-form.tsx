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
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { handleServerError } from '@/lib/handle-server-error'

import { INVOICE_PROFILE_QUERY_KEY, saveInvoiceProfile } from '../api'
import type { InvoiceProfile } from '../types'

interface Props {
  profile: InvoiceProfile | null // null 是服务端已确认未配置，不是读取失败的替代值。
  onSaved?: (profile: InvoiceProfile) => void // 钱包快捷保存后立即重新报价，普通资料页无需跳转。
}

const emptyProfile: InvoiceProfile = {
  title_type: 'business', // 初次配置以企业抬头为默认，个人可主动切换。
  title: '', // 不以用户名自动生成税务抬头，避免未经确认的开票资料。
  tax_id: '', // 企业必须填写税号，个人抬头允许保持空白。
  email: '', // 发票投递邮箱单独确认，不隐式复制登录邮箱。
  address: '', // 可选资料留空仍提交空串，清除之前保存的旧值。
  phone: '', // 联系电话是税务资料，不改变认证手机号。
  bank_name: '', // 银行资料只有用户主动提供才保存。
  bank_account: '', // 保留字符串，前导零和长账号不得丢失。
}

// 常规设置和付款快捷配置共用一份表单，不维护两套抬头字段。
// 表单只处理服务器常用配置，历史申请仍保留创建时快照。
// 保存失败保留草稿并显示错误，不能把没有落库的抬头用于付款。
// 该组件同时用于页面卡片和模态窗口，因此不在内部包额外 Dialog。
// 外层只决定关闭行为，字段校验、提交和缓存刷新集中在此处。
// 用户ID和更新时间由服务器管理，不出现在表单元数据中。
// 发票申请保存独立快照，编辑常用资料不会追溯修改已付款订单。
// 各字段在渲染时翻译，切换语言后标签更新但字段身份不变。
// 星号只表达业务必填条件，实际校验仍由 schema 与后端执行。
// 注册地址、电话及银行资料是可选项，不因界面排版变成必填。
// 请求失败沿用统一解析，异常对象不会被输出到控制台。
// 成功反馈来自服务器返回，不以按钮点击或本地校验通过冒充保存。
// 私密草稿只存在组件内存，关闭弹窗后不会写进登录状态或浏览器存储。
// 当前实现没有批量抬头选择，保持与后端一份常用抬头的契约一致。
// 公共 Form 负责关联说明和错误，Input 不重复实现失焦与验证状态。
// 表单较长部分为字段及声明式布局，没有额外支付或权限分支。
// onSaved 是服务器成功通知，不作为开票审核或付款完成的证据。
// 提交方法只接收白名单值，不把 React Hook Form 的内部状态送入 API。
export function InvoiceProfileForm(props: Props) {
  const { t } = useTranslation() // 翻译订阅放在组件内部，错误和字段标签随语言切换。
  const client = useQueryClient() // 快捷配置与资料页只维护一份服务器确认的缓存。
  const [saving, setSaving] = useState(false) // 保存期间禁用提交，避免重复更新同一抬头。
  // 类型和必填条件在表单入口校验；服务端仍是最终有效性判断方。
  const schema = z
    .object({
      title_type: z.enum(['business', 'personal']), // 不允许任意类型绕过企业税号校验。
      title: z
        .string()
        .trim()
        .min(2, t('Please enter an invoice title'))
        .max(128), // 与服务端名称字符上限保持一致。
      tax_id: z.string().trim().max(32), // 文本税号先裁空白，最终规范化由服务器处理。
      email: z
        .string()
        .trim()
        .email(t('Please enter a valid email address'))
        .max(254), // 只接受纯邮箱，避免把显示名拼入实际投递地址。
      address: z.string().trim().max(255), // 可选地址仍约束长度，不让异常草稿突破接口边界。
      phone: z.string().trim().max(32), // 电话不是数值，区号和加号保持原样。
      bank_name: z.string().trim().max(128), // 银行名称上限匹配现有后端契约。
      bank_account: z.string().trim().max(64), // 账号不用于付款，只作为开票资料。
    })
    .superRefine((value, context) => {
      if (value.title_type === 'business' && !value.tax_id) {
        context.addIssue({
          code: 'custom',
          path: ['tax_id'], // 把企业条件错误定位到可编辑字段而不是只弹提示。
          message: t('Please enter the taxpayer identification number'),
        })
      }
    })
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema), // 用项目现有表单解析器，不重复维护另一份错误状态。
    defaultValues: props.profile ?? emptyProfile, // 已存在配置优先，不能被空模板覆盖。
  })
  useEffect(() => {
    form.reset(props.profile ?? emptyProfile) // 接收重新读取的服务端资料，避免跨配置保留旧草稿。
  }, [props.profile, form])
  // 显式列出允许编辑的资料，归属和更新时间不进入输入控件。
  const fields = [
    { name: 'title', label: t('Invoice title'), required: true },
    {
      name: 'tax_id',
      label: t('Taxpayer identification number'),
      required: form.watch('title_type') === 'business', // 类型改变时必填标识即时响应。
    },
    { name: 'email', label: t('Invoice email'), required: true },
    { name: 'address', label: t('Registered address'), required: false },
    { name: 'phone', label: t('Invoice phone'), required: false },
    { name: 'bank_name', label: t('Bank name'), required: false },
    { name: 'bank_account', label: t('Bank account'), required: false },
  ] as const
  const submit = form.handleSubmit(async (value) => {
    // 只在客户端合法后提交，服务端继续独立验证。
    setSaving(true)
    try {
      const profile = await saveInvoiceProfile(value) // 返回的规范化资料才是后续付款依据。
      client.setQueryData(INVOICE_PROFILE_QUERY_KEY, profile) // 共享的内存资料立即响应，不写入 localStorage。
      toast.success(t('Saved successfully'))
      props.onSaved?.(profile) // 只有服务器确认成功后，付款区才允许继续。
    } catch (error) {
      handleServerError(error, t('Failed to save invoice title'))
    } finally {
      setSaving(false) // 异常和成功都释放提交状态，允许用户修正后重试。
    }
  })
  return (
    <Form {...form}>
      <form onSubmit={submit} className='space-y-4'>
        <FormField
          control={form.control} // 类型字段与同一表单验证状态关联。
          name='title_type' // 使用实际字段名关联表单标签与错误消息。
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Invoice title type')}</FormLabel>
              <Select
                value={field.value} // 受控选择器与校验的企业/个人状态始终一致。
                onValueChange={(value) => value && field.onChange(value)} // 关闭列表产生的空值不清掉有效类型。
                items={[
                  { value: 'business', label: t('Business') },
                  { value: 'personal', label: t('Individual') },
                ]}
              >
                <FormControl>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  <SelectItem value='business'>{t('Business')}</SelectItem>
                  <SelectItem value='personal'>{t('Individual')}</SelectItem>
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          )}
        />
        {/* 手机保持单列，长抬头与银行字段允许自然换行。 */}
        <div className='grid gap-4 sm:grid-cols-2'>
          {fields.map((item) => (
            <FormField
              key={item.name} // 字段身份固定，语言切换不重建或丢失输入。
              control={form.control} // 复用公共表单的标签、失焦与错误关联。 // 类型字段与同一表单验证状态关联。
              name={item.name} // 仅元数据白名单中的抬头字段可出现在请求。
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {item.label}
                    {item.required ? ' *' : ''}
                  </FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      type={item.name === 'email' ? 'email' : 'text'} // 税号和银行账号保持文本，邮箱使用浏览器的邮箱键盘。
                      autoComplete='off' // 税号与银行资料不借浏览器自动填充误用其他账号信息。
                      aria-required={item.required} // 必填信息同时对辅助技术可见，不只依靠星号。
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
        </div>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Changes apply to future invoice requests. Existing requests keep their original title.'
          )}
        </p>
        {/* 通过真实表单提交保留键盘 Enter 行为，保存中阻止重复操作。 */}
        <Button type='submit' disabled={saving}>
          {saving ? t('Saving...') : t('Save invoice title')}
        </Button>
      </form>
    </Form>
  )
}
