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
import { useEffect, useId, useRef, useState, type ChangeEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { deleteSettingsImage, uploadSettingsImage } from '../api'
import { SettingsFormGridItem } from './settings-form-layout'

const MAX_IMAGE_BYTES = 1024 * 1024 // 与服务端一致的单图上限；客户端只用于快速反馈。
const IMAGE_TYPES = new Set(['image/png', 'image/jpeg', 'image/webp']) // 实际内容由服务端解码验证，不能依赖文件名。

// 徽标与采购图片复用选择、预览、替换、删除和错误恢复。
// 上传成功前保留旧预览，不使用本地临时地址假装已经保存。
// 文件由专用接口即时保存，不依赖父表单的普通保存按钮。
// 服务端保存后的预览独立于普通表单，避免保存或重置时写回旧地址。
// 资源只接受项目已有两种图片，不能将任意 URL 或配置键传给接口。
export function SettingsImageField(props: {
  image: 'logo' | 'procurement-wechat-qr'
  value?: string // 父级提供已保存快照，不把文件上传绑定到普通设置草稿。
}) {
  const { t } = useTranslation() // 文案随界面语言变化，不冻结在模块初始化时。
  const inputId = useId() // 稳定关联文件输入与标签，两种图片同页不会重名。
  const [imageURL, setImageURL] = useState(props.value ?? '') // 成功响应驱动预览，不写入父表单草稿。
  useEffect(() => {
    setImageURL(props.value ?? '') // 服务端重新加载已保存值时同步，普通表单重渲染不覆盖上传结果。
  }, [props.value])
  const label =
    props.image === 'logo'
      ? t('Logo image')
      : t('WeChat QR code for lab procurement') // 标签同时描述文件输入与已保存图片。
  const queryClient = useQueryClient() // 共享查询缓存，让后台设置与公开首页读到同一配置版本。
  const fileInputRef = useRef<HTMLInputElement>(null) // 可见按钮转发到真实文件输入，保留浏览器原生选图流程。
  const [isUploading, setIsUploading] = useState(false) // 上传状态只反映当前图片请求，不占用父表单的普通保存状态。
  const [isRemoving, setIsRemoving] = useState(false) // 删除有独立生命周期，完成后重新开放上传与替换入口。
  const isBusy = isUploading || isRemoving // 图片操作期间统一禁用入口，避免上传与删除互相覆盖。
  let uploadButtonLabel = t('Choose file') // 尚无图片时明确提示选文件，而非暗示已经配置。
  if (imageURL) uploadButtonLabel = t('Replace') // 有图时仍复用同一输入，替换不需要先删除旧图。
  if (isUploading) uploadButtonLabel = t('Uploading...') // 进行中的标签优先于替换标签，用户能看到请求状态。

  const refreshStatus = async () => {
    // 图片接口即时保存，完成后同时刷新配置列表和公开状态。
    try {
      window.localStorage.removeItem('status') // 移除冷启动快照，避免下次刷新继续展示旧图片。
    } catch {
      /* 浏览器可能禁止存储；内存查询失效仍会执行，图片写入结果不因此丢失。 */
    }
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['system-options'] }), // 让系统信息重新取得服务端配置，避免旧地址随后被普通保存覆盖。
      queryClient.invalidateQueries({ queryKey: ['status'] }), // 通知已挂载的首页消费者重新读取公开站点图片。
    ])
  }

  const handleImageChange = async (event: ChangeEvent<HTMLInputElement>) => {
    // 仅处理本次选图事件，取消选择不会产生请求或清空旧预览。
    const file = event.target.files?.[0] // 一次仅上传一个图片，与后端单文件接口保持一致。
    event.target.value = '' // 清空原生输入，使失败后重新选择同一个文件仍能触发变化。
    if (!file) return // 关闭系统文件对话框不等于用户要求删除已上传图片。
    if (!IMAGE_TYPES.has(file.type)) {
      // 文件类型提示属于前端快拒绝，服务端仍会验证真实图片内容。
      toast.error(t('Choose a PNG, JPEG or WebP image.')) // 在发起请求前给出可执行提示，已有图片保持不变。
      return
    }
    if (file.size > MAX_IMAGE_BYTES) {
      // 大文件不进入网络上传，避免用户等待后才得知大小限制。
      toast.error(t('Image must be 1 MB or smaller.')) // 大小错误明确可见，不伪造上传成功或清除原预览。
      return
    }

    setIsUploading(true) // 通过校验才进入忙碌态，被拒绝的文件不会锁住入口。
    try {
      const response = requireServerSuccess(
        await uploadSettingsImage(props.image, file)
      ) // 业务失败与网络失败统一进入错误分支，不能把 HTTP 成功当保存成功。
      const url = response.data?.url // 公开预览使用服务端生成的地址，不在前端拼接存储路径。
      if (!url) throw new Error(t('Failed to update setting')) // 缺失必要返回值视为失败，避免以空地址覆盖旧图。
      setImageURL(url) // 服务端确认后更新预览，与父表单保存和重置相互独立。
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
      requireServerSuccess(await deleteSettingsImage(props.image)) // 沿用统一业务成功检查，删除失败不能提前清空界面。
      setImageURL('') // 删除已持久化，不能被父表单的旧地址重新显示。
      await refreshStatus() // 写入后同步公开读取路径，使首页与设置预览一致。
      toast.success(t('Setting updated successfully')) // 只有持久化和刷新流程完成才提示成功。
    } catch (error) {
      handleServerError(error, t('Failed to update setting')) // 统一展示可定位错误，不把失败降级成静默成功。
    } finally {
      setIsRemoving(false) // 删除失败同样恢复按钮状态，用户可以继续操作。
    }
  }

  return (
    <SettingsFormGridItem /* 图片预览与操作允许在窄屏自然换行。 */ span='full'>
      <div className='space-y-2'>
        <Label htmlFor={inputId}>{label}</Label>
        <Input
          id={inputId} // 原生文件输入无需注册成 URL 文本字段。
          aria-describedby={`${inputId}-description`}
          ref={fileInputRef} // 由可见按钮触发同一真实输入，避免另造文件选择状态。
          type='file' // 交给浏览器原生文件选择器处理本地文件授权。
          accept='image/png,image/jpeg,image/webp' // 仅筛选选择器候选；用户仍可能绕过，不能当作安全校验。
          className='sr-only' // 视觉隐藏但保持语义，使用可见按钮提供清楚的操作入口。
          disabled={isBusy} // 忙碌时原生输入也禁用，防止绕过可见按钮的互斥状态。
          onChange={handleImageChange} // 变更统一进入快拒绝、上传和结果同步流程。
        />
        {/* 预览与操作共享弹性布局，窄屏不靠固定横向尺寸挤压控件。 */}
        <div className='flex flex-wrap items-start gap-4'>
          {/* 尚未配置或删除成功后不创建空图片，避免破损图片占位。 */}
          {imageURL && (
            <div className='border-border bg-muted/30 flex size-36 items-center justify-center overflow-hidden rounded-lg border p-2'>
              {/* 直接预览服务端确认的地址，不使用本地临时文件冒充已保存。 */}
              <img
                src={imageURL} // 预览由已保存状态驱动，上传失败不会切换到未保存文件。
                alt={label} // 替代文本说明图片用途，避免与登录二维码混淆。
                className='max-h-full max-w-full object-contain' // 完整容纳图片，不能裁切边缘而破坏扫码识别。
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
            {/* 仅存在已配置图片时提供删除入口，不让用户删除空值。 */}
            {imageURL && (
              <Button
                type='button' // 删除图片不会连带提交其他设置草稿。
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
        <p
          id={`${inputId}-description`}
          className='text-muted-foreground text-sm'
        >
          {t('PNG, JPEG or WebP, up to 1 MB.')}{' '}
          {t('Uploads and removals are saved immediately.')}
        </p>
      </div>
    </SettingsFormGridItem>
  )
}
