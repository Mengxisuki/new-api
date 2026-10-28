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

import { CopyButton } from '@/components/copy-button'
import { useStatus } from '@/hooks/use-status'
import { isPublicImageUrl } from '@/lib/public-image-url'

// 采购联系卡片只读取公开状态，不访问后台设置接口。
// 三个字段均可留空；无有效内容时不生成空卡片。
// 姓名、微信号和二维码分别控制显示，不要求三者同时存在。
// 采购字段独立于微信登录配置，不能取认证二维码作为联系人。
// 图片地址复用公开图片校验，拒绝危险协议和携带凭据的 URL。
// 这里使用现有状态查询，配置保存后的刷新由设置模块负责。
// 复制交互复用 CopyButton，错误提示和复制反馈不在本组件重复实现。
// 失败图片以地址为标识，管理员替换地址后新图片仍可显示。
// 加载失败明确展示替代联系提示，不静默切换到其他二维码。
// 图片只是联系渠道，加载失败不会隐藏已有姓名或微信号。
// 组件负责展示，不把公开的联系人字段当成认证或授权依据。
// 固定图片尺寸用于预留空间，实际排版由局部样式响应断点。
export function OakContact() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const [failedImage, setFailedImage] = useState('') // 仅记录已失败地址，不维护重复的图片加载状态机
  const name = status?.procurement_contact_name?.trim() // 裁掉展示端空白，空文本不占用联系信息行
  const wechat = status?.procurement_wechat_id?.trim() // 复制值与可见微信号使用同一归一化结果
  const image = status?.procurement_wechat_qr_url?.trim() ?? '' // 未配置图片仍使用明确的空字符串边界
  const hasImage = isPublicImageUrl(image) // 地址有效才挂载图片，避免浏览器处理危险输入
  if (!name && !wechat && !hasImage) return null // 沿用旧官网约定，空配置不显示无内容的卡片
  return (
    <aside
      /* 稳定联系锚点供定制方案入口引用 */ className='oak-contact'
      id='oak-procurement-contact'
    >
      <div /* 文字和二维码可以在窄屏独立重排 */ className='oak-contact-details'>
        <p /* 区分采购联系人与账户或登录支持 */ className='oak-eyebrow'>
          {t('Lab procurement contact')}
        </p>
        <p>
          {t(
            'Discuss your team’s usage, procurement and invoice requirements.'
          )}
        </p>
        <dl /* 姓名和微信号以名称值对表达 */>
          {name && (
            <div>
              <dt /* 字段标题随界面语言变化 */>{t('Contact person')}</dt>
              <dd /* 管理员输入按 React 文本转义后展示 */>{name}</dd>
            </div>
          )}
          {wechat && (
            <div>
              <dt /* 微信号字段保持可复制的文本形式 */>{t('WeChat ID')}</dt>
              <dd>
                {wechat}
                <CopyButton /* 复用公共剪贴板状态与失败处理 */
                  value={wechat} // 剪贴板值与可见微信号保持一致
                  size='sm' // 复制操作不抢占联系人正文空间
                  className='oak-contact-copy' // 样式在当前联系卡片范围内生效
                  aria-label={t('Copy WeChat ID')} // 复制目标明确为微信号
                >
                  {t('Copy')}
                </CopyButton>
              </dd>
            </div>
          )}
        </dl>
      </div>
      {hasImage && (
        <figure /* 二维码图与扫描说明保持语义分组 */ className='oak-contact-qr'>
          {failedImage === image ? (
            <p /* 图片失败提示可被辅助技术感知 */ role='status'>
              {t(
                'QR image unavailable. Please use the contact details provided.'
              )}
            </p>
          ) : (
            <img /* 保留原图片地址，不在浏览器下载或改写图像 */
              src={image} // 仅传入已通过公共 URL 校验的地址
              alt={t('WeChat QR code for lab procurement')} // 不可见图片仍有用途说明
              width={160} // 预留图片宽度以减少加载时布局变化
              height={160} // 预留正方形二维码显示区域
              loading='lazy' // 采购节位于首屏之后，按需加载图片
              referrerPolicy='no-referrer' // 外部图片不接收本站页面地址
              onError={() => setFailedImage(image)} // 记录失败地址，新配置地址仍可重新尝试
            />
          )}
          <figcaption /* 扫描用途与采购场景明确对应 */>
            {t('Scan to discuss lab procurement')}
          </figcaption>
        </figure>
      )}
    </aside>
  )
}
