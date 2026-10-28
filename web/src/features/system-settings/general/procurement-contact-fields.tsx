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
import { useQueryClient } from '@tanstack/react-query'
import { ImagePlus, Trash2 } from 'lucide-react'
import { useRef, useState, type ChangeEvent } from 'react'
import { useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { deleteProcurementQRCode, uploadProcurementQRCode } from '../api'
import { SettingsFormGridItem } from '../components/settings-form-layout'
import type { ProcurementContactValues } from './procurement-contact-schema'

const MAX_QR_IMAGE_BYTES = 1024 * 1024 // 前端按二进制 1 MB 快速拒绝，最终大小限制仍由服务端执行。
const QR_IMAGE_TYPES = new Set(['image/png', 'image/jpeg', 'image/webp']) // 仅声明接受可显示的静态图片类型，不能替代服务端解码校验。

/**
 * 系统信息中的采购联系配置区，和微信登录设置相互独立。
 * 联系人及微信号使用父表单的常规提交路径。
 * 图片上传与删除即时持久化，不要求再次点击父表单保存。
 * 图片成功写入后再更新预览，失败保留最后一次有效表单值。
 * 公开首页展示这些字段，表单中明确提示空值会隐藏对应信息。
 * 客户端类型和大小检查用于快速反馈，服务端负责权威校验。
 * 所有图片操作复用同一忙碌边界，防止相互竞争更新。
 */
export function ProcurementContactFields() {
  const { t } = useTranslation() // 订阅当前语言，上传状态和操作反馈随语言切换更新。
  const form = useFormContext<ProcurementContactValues>() // 复用系统信息表单；联系人和微信号仍随父表单统一提交。
  const queryClient = useQueryClient() // 共享查询缓存，让后台设置与公开首页读到同一配置版本。
  const fileInputRef = useRef<HTMLInputElement>(null) // 可见按钮转发到真实文件输入，保留浏览器原生选图流程。
  const [isUploading, setIsUploading] = useState(false) // 上传状态只反映当前图片请求，不占用父表单的普通保存状态。
  const [isRemoving, setIsRemoving] = useState(false) // 删除有独立生命周期，完成后重新开放上传与替换入口。
  const imageURL = form.watch('ProcurementWeChatQRCodeURL') ?? '' // 预览直接订阅表单值；上传成功后无需等待整页重新加载。
  const isBusy = isUploading || isRemoving // 图片操作期间统一禁用入口，避免上传与删除互相覆盖。
  let uploadButtonLabel = t('Choose file') // 尚无图片时明确提示选文件，而非暗示已经配置。
  if (imageURL) uploadButtonLabel = t('Replace') // 有图时仍复用同一输入，替换不需要先删除旧图。
  if (isUploading) uploadButtonLabel = t('Uploading...') // 进行中的标签优先于替换标签，用户能看到请求状态。

  const refreshStatus = async () => {
    // 图片接口即时保存，完成后同时刷新配置列表和公开状态。
    try {
      window.localStorage.removeItem('status') // 移除冷启动快照，避免下次刷新继续展示旧二维码。
    } catch {
      /* 浏览器可能禁止存储；内存查询失效仍会执行，图片写入结果不因此丢失。 */
    }
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['system-options'] }), // 让系统信息重新取得服务端配置，避免旧地址随后被普通保存覆盖。
      queryClient.invalidateQueries({ queryKey: ['status'] }), // 通知已挂载的首页消费者重新读取公开联系信息。
    ])
  }

  const handleImageChange = async (event: ChangeEvent<HTMLInputElement>) => {
    // 仅处理本次选图事件，取消选择不会产生请求或清空旧预览。
    const file = event.target.files?.[0] // 一次仅上传一个二维码，与后端单文件接口保持一致。
    event.target.value = '' // 清空原生输入，使失败后重新选择同一个文件仍能触发变化。
    if (!file) return // 关闭系统文件对话框不等于用户要求删除已上传图片。
    if (!QR_IMAGE_TYPES.has(file.type)) {
      // 文件类型提示属于前端快拒绝，服务端仍会验证真实图片内容。
      toast.error(t('Choose a PNG, JPEG or WebP image.')) // 在发起请求前给出可执行提示，已有图片保持不变。
      return
    }
    if (file.size > MAX_QR_IMAGE_BYTES) {
      // 大文件不进入网络上传，避免用户等待后才得知大小限制。
      toast.error(t('Image must be 1 MB or smaller.')) // 大小错误明确可见，不伪造上传成功或清除原预览。
      return
    }

    setIsUploading(true) // 通过校验才进入忙碌态，被拒绝的文件不会锁住入口。
    try {
      const response = requireServerSuccess(await uploadProcurementQRCode(file)) // 业务失败与网络失败统一进入错误分支，不能把 HTTP 成功当保存成功。
      const url = response.data?.url // 公开预览使用服务端生成的地址，不在前端拼接存储路径。
      if (!url) throw new Error(t('Failed to update setting')) // 缺失必要返回值视为失败，避免以空地址覆盖旧图。
      form.setValue('ProcurementWeChatQRCodeURL', url, {
        // 只有确认上传成功才替换表单值和预览。
        shouldDirty: false, // 图片接口已独立保存，不让父表单把它当作未提交改动。
        shouldTouch: false, // 更新来自服务端操作结果，不伪造一次手工编辑。
        shouldValidate: true, // 新地址或空值仍经过表单约束，及时清理旧校验提示。
      })
      await refreshStatus() // 写入后同步公开读取路径，使首页与设置预览一致。
      toast.success(t('Setting updated successfully')) // 只有持久化和刷新流程完成才提示成功。
    } catch (error) {
      handleServerError(error, t('Failed to update setting')) // 统一展示可定位错误，不把失败降级成静默成功。
    } finally {
      setIsUploading(false) // 无论成功还是失败都解锁图片入口，允许重试。
    }
  }

  const handleRemoveImage = async () => {
    setIsRemoving(true) // 请求开始即关闭图片入口，防止连续删除或同时替换。
    try {
      requireServerSuccess(await deleteProcurementQRCode()) // 沿用统一业务成功检查，删除失败不能提前清空界面。
      form.setValue('ProcurementWeChatQRCodeURL', '', {
        // 仅服务端删除成功后移除预览，失败时继续保留原图。
        shouldDirty: false, // 图片接口已独立保存，不让父表单把它当作未提交改动。
        shouldTouch: false, // 更新来自服务端操作结果，不伪造一次手工编辑。
        shouldValidate: true, // 新地址或空值仍经过表单约束，及时清理旧校验提示。
      })
      await refreshStatus() // 写入后同步公开读取路径，使首页与设置预览一致。
      toast.success(t('Setting updated successfully')) // 只有持久化和刷新流程完成才提示成功。
    } catch (error) {
      handleServerError(error, t('Failed to update setting')) // 统一展示可定位错误，不把失败降级成静默成功。
    } finally {
      setIsRemoving(false) // 删除失败同样恢复按钮状态，用户可以继续操作。
    }
  }

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
        name='ProcurementContactName'
        render={({ field }) => (
          <FormItem>
            {/* 标签通过表单组件与输入关联，为键盘和读屏提供字段名称。 */}
            <FormLabel>{t('Contact person')}</FormLabel>
            <FormControl>
              {/* 输入长度与采购配置契约一致，实际规则仍由表单和后端校验。 */}
              <Input maxLength={100} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      {/* 微信号只作为公开联系方式，不能与微信登录凭据混用。 */}
      <FormField
        control={form.control}
        name='ProcurementWeChatID'
        render={({ field }) => (
          <FormItem>
            {/* 微信号保留独立标签，避免与联系人姓名的值混淆。 */}
            <FormLabel>{t('WeChat ID')}</FormLabel>
            <FormControl>
              <Input maxLength={100} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      {/* 二维码跨整行展示，预览和操作在窄屏可以自然换行。 */}
      <SettingsFormGridItem span='full'>
        <FormField
          control={form.control}
          name='ProcurementWeChatQRCodeURL'
          render={() => (
            <FormItem>
              {/* 二维码字段具有稳定可访问名称，隐藏输入仍由此标签描述。 */}
              <FormLabel>{t('WeChat QR code for lab procurement')}</FormLabel>
              <FormControl>
                <Input
                  ref={fileInputRef} // 由可见按钮触发同一真实输入，避免另造文件选择状态。
                  type='file' // 交给浏览器原生文件选择器处理本地文件授权。
                  accept='image/png,image/jpeg,image/webp' // 仅筛选选择器候选；用户仍可能绕过，不能当作安全校验。
                  className='sr-only' // 视觉隐藏但保持语义，使用可见按钮提供清楚的操作入口。
                  disabled={isBusy} // 忙碌时原生输入也禁用，防止绕过可见按钮的互斥状态。
                  onChange={handleImageChange} // 变更统一进入快拒绝、上传和结果同步流程。
                />
              </FormControl>
              {/* 预览与操作共享弹性布局，窄屏不靠固定横向尺寸挤压控件。 */}
              <div className='flex flex-wrap items-start gap-4'>
                {/* 尚未配置或删除成功后不创建空图片，避免破损图片占位。 */}
                {imageURL && (
                  <div className='border-border bg-muted/30 flex size-36 items-center justify-center overflow-hidden rounded-lg border p-2'>
                    {/* 直接预览服务端确认的地址，不使用本地临时文件冒充已保存。 */}
                    <img
                      src={imageURL} // 预览由表单订阅值驱动，上传失败不会切换到未保存文件。
                      alt={t('WeChat QR code for lab procurement')} // 替代文本说明采购用途，避免与登录二维码混淆。
                      className='max-h-full max-w-full object-contain' // 完整容纳二维码，不能裁切边缘而破坏扫码识别。
                    />
                  </div>
                )}
                {/* 按钮允许换行，较长语言仍保留独立点击区域。 */}
                <div className='flex flex-wrap gap-2'>
                  {/* 使用项目按钮组件，复用焦点、禁用和键盘行为。 */}
                  <Button
                    type='button' // 选图不触发父表单提交，图片拥有独立保存时机。
                    variant='outline'
                    disabled={isBusy} // 同一忙碌边界同时覆盖上传、替换和删除。
                    onClick={() => fileInputRef.current?.click()} // 复用真实输入的选择流程，取消选择不影响已有图片。
                  >
                    {/* 图标为装饰信息，可访问名称由选择或替换文字提供。 */}
                    <ImagePlus aria-hidden='true' />
                    {uploadButtonLabel}
                  </Button>
                  {/* 仅存在已配置二维码时提供删除入口，不让用户删除空值。 */}
                  {imageURL && (
                    <Button
                      type='button' // 删除图片不会连带提交联系人和微信号的草稿。
                      variant='destructive'
                      disabled={isBusy} // 请求进行中禁止再次删除，避免重复覆盖状态。
                      onClick={() => void handleRemoveImage()} // 异步删除内部负责错误提示和恢复忙碌状态。
                    >
                      {/* 删除含明确文字标签，装饰图标无需被读屏重复朗读。 */}
                      <Trash2 aria-hidden='true' />
                      {t('Remove')}
                    </Button>
                  )}
                </div>
              </div>
              {/* 类型及大小限制在操作前可见，与上传时快拒绝提示一致。 */}
              <FormDescription>
                {t('PNG, JPEG or WebP, up to 1 MB.')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </SettingsFormGridItem>
    </>
  )
}
