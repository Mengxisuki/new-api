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
import { useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import { SettingsFormGridItem } from '../components/settings-form-layout'
import { SettingsImageField } from '../components/settings-image-field'
import type { ProcurementContactValues } from './procurement-contact-schema'

// 联系人和微信号随父表单保存；图片使用与徽标相同的即时上传控件。
// 采购图片仍独立于微信登录和站点徽标，原字段及公开地址保持不变。
// 空姓名和微信号在官网隐藏，不在上传操作中修改其草稿。
// 文字字段依赖父表单已有的长度与空白归一化规则。
// 对外公开的配置说明必须与填写区一起展示，避免误填内部资料。
// 姓名和微信号继续通过普通设置接口保存，不因图片操作提前提交。
// 图片控件只接收已保存地址快照，文件不注册为父表单文本字段。
// 控件内部处理成功后的新预览，失败时保留原有服务端图片。
// 后续系统配置重新加载时，watch 值向控件同步新的服务端快照。
// 这里不重复维护文件引用、忙碌状态、Toast 或 HTTP 请求。
// 上传限制在共享控件提示，权威格式和尺寸检查仍由后端执行。
// 固定资源名对应采购接口，不允许在此传入任意上传 URL。
// 原采购字段和服务器地址保持不变，既有图片无需重新上传。
// 联系人布局保留普通表单的错误关联，文件提示由共享控件关联。
// 图片跨列布局由共享控件决定，本层不再额外包裹重复网格。
export function ProcurementContactFields() {
  const { t } = useTranslation() // 切换语言后字段说明同步更新。
  const form = useFormContext<ProcurementContactValues>() // 复用父表单及既有校验。
  return (
    <>
      {/* 公开配置说明独占整行，避免被误认为仅管理员可见的内部备注。 */}
      <SettingsFormGridItem span='full'>
        <h3 className='font-medium'>{t('Lab procurement contact')}</h3>
        <p className='text-muted-foreground mt-2 text-sm'>
          {t(
            'These details are public on the default homepage. Leave a field empty to hide it. This QR code is separate from WeChat login.'
          )}
        </p>
      </SettingsFormGridItem>
      {/* 联系人名称接入父表单控制器，保持统一保存及校验行为。 */}
      <FormField
        control={form.control}
        name='ProcurementContactName' // 文本字段仍属于父表单，保存时参与变更检测。
        render={({ field }) => (
          <FormItem>
            {/* 标签通过表单组件与输入关联，为键盘和读屏提供字段名称。 */}
            <FormLabel>{t('Contact person')}</FormLabel>
            <FormControl>
              {/* 输入长度与采购配置契约一致，实际规则仍由表单和后端校验。 */}
              <Input maxLength={100} {...field} />
            </FormControl>
            {/* 沿用父表单校验错误，不因采用共享图片控件而隐藏文字错误。 */}
            <FormMessage />
          </FormItem>
        )}
      />
      {/* 微信号只作为公开联系方式，不能与微信登录凭据混用。 */}
      <FormField
        control={form.control}
        name='ProcurementWeChatID' // 保持字符串，避免账号标识的前导零丢失。
        render={({ field }) => (
          <FormItem>
            {/* 微信号保留独立标签，避免与联系人姓名的值混淆。 */}
            <FormLabel>{t('WeChat ID')}</FormLabel>
            <FormControl>
              {/* 长度限制与既有后端约束一致，避免只在请求后才提示超限。 */}
              <Input maxLength={100} {...field} />
            </FormControl>
            {/* 沿用父表单校验错误，不因采用共享图片控件而隐藏文字错误。 */}
            <FormMessage />
          </FormItem>
        )}
      />
      {/* 复用与徽标一致的即时上传行为，不在采购模块复制另一套实现。 */}
      <SettingsImageField
        image='procurement-wechat-qr' // 只选择采购资源，不能修改站点徽标或登录二维码。
        value={form.watch('ProcurementWeChatQRCodeURL')} // 值只用于同步已保存的采购图片快照。
      />
    </>
  )
}
