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
import type { TFunction } from 'i18next'
import { useEffect } from 'react'
import {
  type Control,
  type FieldPath,
  type Resolver,
  useForm,
} from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { useStatus } from '@/hooks/use-status'

import {
  createOfficialChannelDefault,
  type OfficialPaymentChannelData,
  type OfficialPaymentChannelType,
} from './official-payment-channel-types'

// 渠道草稿在提交边界统一验证；下方较长链式声明没有异步状态或额外业务分支。
function createChannelSchema(t: TFunction) {
  return z
    .object({
      id: z // ID 会进入后端通知路径，限制字符集避免不合法路由。
        .string()
        .regex(
          /^[A-Za-z0-9_-]{1,50}$/,
          t(
            'Channel ID can only contain letters, numbers, underscores, and hyphens'
          )
        ),
      name: z // 展示名称必填，不能使用仅空白的渠道。
        .string()
        .trim()
        .min(1, t('Channel name is required'))
        .max(64, t('Channel name must not exceed 64 characters')),
      icon: z.string().max(255, t('Icon must not exceed 255 characters')),
      channel_type: z.enum(['alipay', 'wechat']), // 仅接受后端已实现的两种直连商户。
      interaction_mode: z.enum(['qr', 'page', 'wap', 'redirect']), // 交互枚举不接受任意跳转方式。
      min_topup: z.string(), // 表单保留输入文本，数值检查集中在提交前。
      enabled: z.boolean(), // 关闭的草稿可以暂存尚不完整的商户字段。
      sort_order: z.string(), // 排序必须为整数，不能静默舍弃小数。
      config: z.record(z.string(), z.string()), // 商户字段值使用文本，禁止夹带对象。
      configured_secrets: z.array(z.string()), // 仅记录服务端已存字段名称，不含凭证内容。
    })
    .superRefine((value, ctx) => {
      const minTopUp = Number(value.min_topup) // 与输入控件分开，避免绕过 HTML min 限制。
      if (!Number.isFinite(minTopUp) || minTopUp < 0) {
        // 无穷与负值都不能进入最低充值配置。
        ctx.addIssue({
          code: 'custom',
          path: ['min_topup'],
          message: t('Minimum top-up must be zero or greater'),
        })
      }
      const sortOrder = Number(value.sort_order) // 排序只参与展示，不转换为扣费倍率。
      if (!Number.isInteger(sortOrder)) {
        // 非整数直接定位到排序字段。
        ctx.addIssue({
          code: 'custom',
          path: ['sort_order'],
          message: t('Sort order must be an integer'),
        })
      }
      const hasSecret = (key: string) =>
        // 脱敏编辑允许留空，以服务器已存凭证为准。
        !!value.config[key]?.trim() || value.configured_secrets.includes(key)
      if (!value.enabled) return // 关闭草稿不强制完成商户接入材料。

      const required = // 各协议必须的签名、解密及身份字段在启用时一次校验。
        value.channel_type === 'alipay'
          ? ['app_id', 'private_key', 'alipay_public_key']
          : [
              'appid',
              'mchid',
              'merchant_serial_no',
              'merchant_private_key',
              'api_v3_key',
              'wechatpay_public_key_id',
              'wechatpay_public_key',
            ]
      required.forEach((key) => {
        // 同时列出缺失字段，避免用户逐个提交才发现错误。
        const present = // 只有秘密字段允许使用 configured_secrets 代替明文值。
          key === 'private_key' ||
          key === 'alipay_public_key' ||
          key === 'merchant_private_key' ||
          key === 'api_v3_key' ||
          key === 'wechatpay_public_key'
            ? hasSecret(key)
            : !!value.config[key]?.trim()
        if (!present) {
          // 缺失项绑定原字段，错误显示在对应控件下方。
          ctx.addIssue({
            code: 'custom',
            path: ['config', key],
            message: t('Required when the channel is enabled'),
          })
        }
      })
      if (
        value.channel_type === 'wechat' &&
        value.config.api_v3_key?.trim() &&
        value.config.api_v3_key.trim().length !== 32
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['config', 'api_v3_key'],
          message: t('API v3 key must contain exactly 32 characters'),
        })
      }
      if (
        value.channel_type === 'wechat' &&
        value.config.wechatpay_public_key_id?.trim() &&
        !value.config.wechatpay_public_key_id.trim().startsWith('PUB_KEY_ID_')
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['config', 'wechatpay_public_key_id'],
          message: t('WeChat Pay public key ID must start with PUB_KEY_ID_'),
        })
      }
    })
}

type OfficialChannelFormValues = z.infer<ReturnType<typeof createChannelSchema>>

type OfficialPaymentChannelDialogProps = {
  open: boolean // 弹窗开关由外层草稿编辑器持有。
  onOpenChange: (open: boolean) => void // 取消不保存，不主动发服务器请求。
  onSave: (channel: OfficialPaymentChannelData) => void // 只输出完整渠道，整页设置负责真正持久化。
  channel: OfficialPaymentChannelData | null // 空值明确表示新增，而不是编辑第一项。
  existingChannels: OfficialPaymentChannelData[] // 完整现有列表用于阻止 ID 冲突。
  callbackAddress?: string // 沿用现有支付回调域名，不另起配置项。
}

type ConfigFieldProps = {
  control: Control<OfficialChannelFormValues> // 配置字段订阅同一 React Hook Form 实例。
  name: keyof OfficialPaymentChannelData['config'] & string // 配置键原样映射后端商户协议。
  label: string // 传入已翻译标签，避免将动态字符串漏出国际化。
  placeholder?: string // 示例值仅提示，不会被自动当作真实配置保存。
  secret?: boolean // 密钥输入限制自动填充并显示留空保留的约定。
  multiline?: boolean // PEM 材料需要保留多行，不强行压成单行密码框。
  defaultUrl?: string // 展示可复制的后端默认通知地址。
}

const FORM_ID = 'official-payment-channel-form' // 公共弹窗页脚通过 form 属性提交真实表单。

// 统一凭证输入与默认回调提示，避免每个渠道复制不同的脱敏语义。
function ConfigField(props: ConfigFieldProps) {
  const { t } = useTranslation()

  return (
    <FormField
      control={props.control}
      name={`config.${props.name}` as FieldPath<OfficialChannelFormValues>} // 点路径让错误自动定位到商户字段。
      render={({ field }) => (
        <FormItem /* 商户字段共享标签与错误绑定，保留密码框语义。 */>
          <FormLabel /* 标签必须与真实输入关联，不能只靠占位符。 */>
            {props.label}
          </FormLabel>
          <FormControl>
            {props.multiline ? (
              <Textarea
                rows={5} // PEM 私钥与公钥均预留可读多行高度。
                placeholder={props.placeholder} // 提示字符串不作为已保存秘密的回显。
                autoComplete={props.secret ? 'new-password' : 'off'} // 避免浏览器把登录密码填进商户密钥。
                {...field}
                value={String(field.value ?? '')} // 脱敏缺失值仍保持受控文本输入。
              />
            ) : (
              <Input
                type={props.secret ? 'password' : 'text'} // 单行秘密以密码输入显示。
                placeholder={props.placeholder} // 提示字符串不作为已保存秘密的回显。
                autoComplete={props.secret ? 'new-password' : 'off'} // 避免浏览器把登录密码填进商户密钥。
                {...field}
                value={String(field.value ?? '')} // 脱敏缺失值仍保持受控文本输入。
              />
            )}
          </FormControl>
          {props.secret && ( // 提示保留语义，不能误导用户必须重新输入既有凭证。
            <FormDescription>
              {t('Leave blank to keep the stored value')}
            </FormDescription>
          )}
          {!props.secret && props.defaultUrl ? ( // 只复制公开通知地址，不提供秘密复制入口。
            <FormDescription className='flex min-w-0 items-center gap-2'>
              <span className='min-w-0 flex-1'>
                {t('Leave blank to use the default URL:')}{' '}
                <code className='break-all'>{props.defaultUrl}</code>
              </span>
              <CopyButton
                value={props.defaultUrl} // 复制源与用户看到的默认地址完全一致。
                className='size-7' // 紧凑复制控件沿用公共组件反馈。
                iconClassName='size-3.5' // 图标大小独立于点击区域。
              />
            </FormDescription>
          ) : null}
          <FormMessage /* 商户凭据不足时直接定位对应输入。 */ />
        </FormItem>
      )}
    />
  )
}

// 将可选字段规范为受控输入；复制配置避免未保存编辑污染外层快照。
function toFormValues(
  channel: OfficialPaymentChannelData
): OfficialChannelFormValues {
  return {
    id: channel.id, // 编辑使用原有稳定 ID。
    name: channel.name, // 展示名不参与商户验签身份。
    icon: channel.icon ?? '', // 未设置图标保持空输入，展示端按商户类型选择默认图标。
    channel_type: channel.channel_type, // 旧渠道类型只读，不允许换协议后沿用原订单。
    interaction_mode: channel.interaction_mode, // 交互方式沿用服务端已保存值。
    min_topup: String(channel.min_topup ?? 0), // 文本草稿与数值协议在边界转换。
    enabled: channel.enabled, // 保留已有启用状态，不因打开弹窗而改变。
    sort_order: String(channel.sort_order ?? 0), // 未设置排序时使用与后端一致的零值。
    config: { ...channel.config }, // 每个表单使用独立凭据草稿。
    configured_secrets: [...(channel.configured_secrets ?? [])], // 保留秘密存在标记，不向用户还原秘密值。
  }
}

// 弹窗负责商户表单，不直接请求支付接口，也不把保存配置等同于渠道可用。
// 声明式字段与渲染较长，但状态只有表单；无需额外状态管理或动态注册层。
export function OfficialPaymentChannelDialog(
  props: OfficialPaymentChannelDialogProps
) {
  const { t } = useTranslation()
  const { status } = useStatus() // 只读取站点公开地址，商户凭据仍来自脱敏设置。
  const channelSchema = createChannelSchema(t) // 当前语言决定用户可见的校验错误。
  const form = useForm<OfficialChannelFormValues>({
    resolver: zodResolver(channelSchema) as Resolver<OfficialChannelFormValues>, // 保存只能经过 schema 校验。
    defaultValues: toFormValues(createOfficialChannelDefault('alipay')),
  })
  const channelType = form.watch('channel_type') // 协议变化后展示相应凭据字段。
  const channelID = form.watch('id') // 通知路径示例随新建草稿 ID 即时更新。
  const statusServerAddress = // 沿用公开状态当前支持的地址形状。
    status?.server_address ?? status?.data?.server_address
  const callbackBaseAddress = // 支付专用回调域优先，随后站点地址与当前来源。
    (
      props.callbackAddress?.trim() ||
      (typeof statusServerAddress === 'string' ? statusServerAddress : '') ||
      window.location.origin
    ) // 本地预览也能显示实际来源，无硬编码部署域名。
      .replace(/\/$/, '')
  const defaultNotifyUrl = `${callbackBaseAddress}/api/user/${
    channelType === 'alipay' ? 'alipay' : 'wechat-pay'
  }/notify/${channelID || 'channel-id'}`

  useEffect(() => {
    let next = props.channel // 每次打开从外层最新保存的草稿重新开始。
    if (!next) {
      let existingCount = props.existingChannels.length // 后缀只作候选，仍需检查真正重复 ID。
      next = createOfficialChannelDefault('alipay', existingCount) // 新建采用原默认商户，不自动启用。
      while (
        props.existingChannels.some((channel) => channel.id === next?.id)
      ) {
        existingCount++ // 删除过渠道后也不能生成与存量相同的 ID。
        next = createOfficialChannelDefault('alipay', existingCount) // 新建采用原默认商户，不自动启用。
      }
    }
    form.reset(toFormValues(next)) // 取消后重开不残留未保存的秘密输入。
  }, [form, props.channel, props.existingChannels, props.open])

  const handleChannelTypeChange = (nextType: OfficialPaymentChannelType) => {
    const current = form.getValues() // 切换协议保留用户已经填写的展示资料。
    const next = toFormValues(
      createOfficialChannelDefault(nextType, props.existingChannels.length)
    )
    form.reset({
      // 清空旧协议凭证，避免把支付宝密钥提交成微信商户材料。
      ...next,
      id: current.id, // 创建中的自定义 ID 不随协议选择被改写。
      name: current.name, // 用户为渠道命名的文字继续保留。
      icon: current.icon, // 已选图标不被默认模板强制覆盖。
      min_topup: current.min_topup, // 充值门槛不属于协议凭证，切换时保留。
      enabled: current.enabled, // 新协议若缺材料仍会在提交时失败。
      sort_order: current.sort_order, // 改协议不影响用户排定的展示位置。
    })
  }

  // 通过校验后仅输出规范化草稿；实际秘密合并与业务验证由服务端完成。
  const handleSubmit = (values: OfficialChannelFormValues) => {
    if (
      !props.channel &&
      props.existingChannels.some((channel) => channel.id === values.id.trim())
    ) {
      form.setError('id', { message: t('Channel ID must be unique') }) // 新建冲突定位 ID 字段，不覆盖存量渠道。
      return
    }
    const channel: OfficialPaymentChannelData = {
      id: values.id.trim(), // 保存前去除输入两端空白，与唯一性比较一致。
      name: values.name.trim(), // 展示文本不允许以空白伪造必填项。
      channel_type: values.channel_type, // 类型已由枚举约束，不再建立第二套转换。
      interaction_mode: values.interaction_mode, // 保持后端原有交互枚举。
      min_topup: Number(values.min_topup) || 0, // 非负校验已完成，协议字段以数值提交。
      enabled: values.enabled, // 提交草稿不改变当前表单之外的其他渠道。
      sort_order: Number(values.sort_order) || 0, // 整数校验已通过，不在这里静默取整。
      config: Object.fromEntries(
        Object.entries(values.config).map(([key, value]) => [key, value.trim()])
      ),
      configured_secrets: values.configured_secrets, // 留空秘密依据原已配置标记保持存在。
    }
    if (values.icon.trim()) channel.icon = values.icon.trim() // 空图标省略，展示端采用安全默认。
    props.onSave(channel) // 外层草稿更新与网络保存保持分离。
    props.onOpenChange(false) // 保存草稿成功后关闭，付款仍不会在此触发。
  }

  // 每种商户的字段独立列出，切换渠道时不会混入另一方的签名凭据。
  // 标签在当前渲染内翻译，语言切换后名称与校验提示一起更新。
  const configFields: Omit<ConfigFieldProps, 'control'>[] =
    channelType === 'alipay'
      ? [
          { name: 'app_id', label: t('App ID') }, // 应用身份必须与付款通知中的 app_id 相符。
          { name: 'sign_type', label: t('Sign type') }, // 原有 RSA/RSA2 选择由后端再次核验。
          {
            name: 'private_key',
            label: t('Application private key'),
            secret: true,
            multiline: true,
          }, // 私钥输入不回显已有值。
          {
            name: 'alipay_public_key',
            label: t('Alipay public key'),
            secret: true,
            multiline: true,
          }, // 验签材料按原脱敏约定保留。
          { name: 'gateway_url', label: t('Gateway URL') }, // 沿用商户配置的正式或沙盒网关。
          { name: 'notify_url', label: t('Notify URL') }, // 异步通知承接实际到账，不以浏览器返回判定成功。
          { name: 'return_url', label: t('Return URL') }, // 返回页仅用于支付后的用户导航。
          { name: 'app_cert_sn', label: t('App certificate SN') }, // 证书模式保留旧配置字段。
          {
            name: 'alipay_root_cert_sn',
            label: t('Alipay root certificate SN'),
          }, // 根证书标识与应用证书分开保存。
        ]
      : [
          { name: 'appid', label: t('App ID') }, // 微信应用标识与商户号配对。
          { name: 'mchid', label: t('Merchant ID') }, // 商户身份由服务端付款接口校验。
          {
            name: 'merchant_serial_no',
            label: t('Merchant certificate serial number'),
          }, // 请求签名使用商户证书序列号。
          { name: 'api_v3_key', label: t('API v3 key'), secret: true }, // 解密密钥必须保留三十二字符约束。
          {
            name: 'wechatpay_public_key_id',
            label: t('WeChat Pay public key ID'),
            placeholder: 'PUB_KEY_ID_...',
          }, // 公钥模式通过 ID 匹配微信签名。
          {
            name: 'merchant_private_key',
            label: t('Merchant private key'),
            secret: true,
            multiline: true,
          }, // 商户私钥只在用户主动更新时提交新值。
          {
            name: 'wechatpay_public_key',
            label: t('WeChat Pay public key'),
            secret: true,
            multiline: true,
          }, // 微信公钥用于通知验签，不自动下载替代。
          { name: 'notify_url', label: t('Notify URL') }, // 充值与订阅共用后端通知入口。
          { name: 'h5_redirect_url', label: t('H5 redirect URL') }, // H5 支付完成后可返回当前站点。
          { name: 'h5_type', label: t('H5 scene type'), placeholder: 'WAP' }, // 保留微信商户的 H5 场景配置。
          { name: 'h5_wap_url', label: t('H5 website URL') }, // 网站地址作为网关请求信息提交。
          { name: 'h5_wap_name', label: t('H5 website name') }, // 网站名称与地址分别配置。
        ]

  return (
    <Dialog
      open={props.open} // 复用公共弹窗的焦点与关闭语义。
      onOpenChange={props.onOpenChange} // 遮罩关闭与取消均交由同一外层状态处理。
      title={
        props.channel ? t('Edit official channel') : t('Add official channel')
      }
      contentClassName='sm:max-w-3xl' // 双列字段有足够宽度，手机仍由公共弹窗限宽。
      contentHeight='min(70vh, 720px)' // 长凭据使用弹窗正文滚动，页脚始终可操作。
      footer={
        <>
          <Button
            type='button' // 取消不能提交表单或改变商户状态。
            variant='outline' // 取消维持次级操作层级。
            onClick={() => props.onOpenChange(false)} // 丢弃本次未保存草稿。
          >
            {t('Cancel')}
          </Button>
          <Button
            /* 页脚通过原生 form 关联触发唯一校验入口。 */ type='submit'
            form={FORM_ID} // 外置按钮提交当前表单，保留键盘 Enter 行为。
          >
            {props.channel ? t('Update') : t('Add')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={FORM_ID} // 与公共弹窗外置页脚按钮精确对应。
          onSubmit={form.handleSubmit(handleSubmit)} // 无效凭据不能进入外层配置数组。
          className='space-y-6' // 身份字段、启用状态和商户凭据保持视觉分组。
        >
          <div className='grid gap-4 sm:grid-cols-2'>
            {/* 文本字段共用表单关联，名称和渠道 ID 保留独立校验。 */}
            {(
              [
                { name: 'name', label: t('Name') }, // 展示名可修改，不改变订单网关关联。
                { name: 'id', label: t('Channel ID') }, // ID 在新建时确定，并在编辑时只读。
                { name: 'icon', label: t('Icon') }, // 图标字段复用现有安全名称与 URL 约定。
                {
                  name: 'min_topup', // 充值最低数量与订阅售价无关。
                  label: t('Minimum top-up'), // 明确这是渠道充值门槛，避免误读为订单总额。
                  type: 'number',
                  min: 0, // 控件层提前阻止负数，schema 仍再次验证。
                },
                { name: 'sort_order', label: t('Sort order'), type: 'number' },
              ] as const
            ).map((input) => (
              <FormField
                key={input.name} // 稳定字段标识避免多语言切换重建输入。
                control={form.control}
                name={input.name} // 错误提示仍关联原始业务字段名。
                render={({ field }) => (
                  <FormItem /* 通用身份字段统一处理必填与格式错误。 */>
                    <FormLabel>{input.label}</FormLabel>
                    <FormControl>
                      <Input
                        {...field}
                        // 既有渠道 ID 被付款通知路径引用，编辑时不允许改名。
                        disabled={input.name === 'id' && !!props.channel}
                        type={'type' in input ? input.type : 'text'} // 数值与文字输入共用布局，类型保留差异。
                        min={'min' in input ? input.min : undefined} // 充值数量在控件层也限制负数。
                        step={'type' in input ? 1 : undefined} // 排序和充值门槛均以整数输入。
                      />
                    </FormControl>
                    <FormMessage /* 身份或数值错误在当前字段下展示。 */ />
                  </FormItem>
                )}
              />
            ))}
            <FormField
              control={form.control}
              name='channel_type' // 协议决定下面出现哪些商户专属字段。
              render={({ field }) => (
                <FormItem /* 协议字段只允许新建时选择，存量商户不能换类型。 */>
                  <FormLabel>{t('Channel type')}</FormLabel>
                  <FormControl>
                    <NativeSelect
                      className='w-full'
                      value={field.value}
                      disabled={!!props.channel} // 旧订单继续引用原商户，不允许编辑时切换协议。
                      onChange={(event) =>
                        handleChannelTypeChange(
                          event.target.value as OfficialPaymentChannelType
                        )
                      }
                    >
                      <NativeSelectOption
                        /* 支付宝直连使用自身签名配置。 */ value='alipay'
                      >
                        {t('Alipay')}
                      </NativeSelectOption>
                      <NativeSelectOption
                        /* 微信支付不复用微信登录凭证。 */ value='wechat'
                      >
                        {t('WeChat Pay')}
                      </NativeSelectOption>
                    </NativeSelect>
                  </FormControl>
                  <FormMessage /* 不支持的商户类型由枚举校验拒绝。 */ />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='interaction_mode' // 选择付款交互，不改变商户身份与密钥。
              render={({ field }) => (
                <FormItem /* 交互方式单独受控，不影响现有凭据输入。 */>
                  <FormLabel>{t('Interaction mode')}</FormLabel>
                  <FormControl>
                    <NativeSelect
                      className='w-full'
                      value={field.value}
                      onChange={field.onChange} // 模式变更只更新当前表单。
                    >
                      <NativeSelectOption
                        /* 两种商户均提供扫码模式。 */ value='qr'
                      >
                        {t('QR code')}
                      </NativeSelectOption>
                      {channelType === 'alipay' ? (
                        <>
                          <NativeSelectOption
                            /* 支付宝桌面浏览器结算页面。 */ value='page'
                          >
                            {t('Desktop web')}
                          </NativeSelectOption>
                          <NativeSelectOption
                            /* 支付宝移动网页结算页面。 */ value='wap'
                          >
                            {t('Mobile web')}
                          </NativeSelectOption>
                        </>
                      ) : (
                        <NativeSelectOption
                          /* 微信 H5 调起页跳转模式。 */ value='redirect'
                        >
                          {t('H5 redirect')}
                        </NativeSelectOption>
                      )}
                    </NativeSelect>
                  </FormControl>
                  <FormMessage /* 不合法交互模式不能离开当前表单。 */ />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='enabled' // 停用保留配置，启用才要求完整商户材料。
            render={({ field }) => (
              <FormItem className='flex items-center justify-between rounded-md border px-3 py-2.5'>
                <FormLabel>{t('Enabled')}</FormLabel>
                <FormControl>
                  <Switch
                    checked={field.value} // 显示与表单持有的布尔值一致。
                    onCheckedChange={field.onChange} // 不额外写数据库或请求付款。
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <Separator /* 身份与开关之后单独呈现商户凭据，避免把公私钥与普通字段混读。 */
          />

          {/* 配置元数据只决定控件排列，保存协议仍使用原商户字段名。 */}
          <div className='grid gap-4 sm:grid-cols-2'>
            {configFields.map((config) => (
              <div
                key={config.name} // 商户协议字段名是控件稳定身份。
                className={config.multiline ? 'sm:col-span-2' : undefined} // 长 PEM 凭据独占整行，避免横向裁切。
              >
                <ConfigField
                  control={form.control} // 各凭据共用父表单提交与错误映射。
                  {...config}
                  // 回调地址留空时显示后端默认路径，避免误填到站点根路径。
                  defaultUrl={
                    config.name === 'notify_url' ? defaultNotifyUrl : undefined
                  }
                  placeholder={
                    config.name === 'notify_url'
                      ? defaultNotifyUrl
                      : config.placeholder
                  }
                />
              </div>
            ))}
          </div>
        </form>
      </Form>
    </Dialog>
  )
}
