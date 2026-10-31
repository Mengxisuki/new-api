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
import { ReceiptText } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import { InvoiceProfileForm } from '@/features/invoices/components/invoice-profile-form'
import { useInvoiceProfile } from '@/features/invoices/hooks/use-invoice-profile'

// 发票抬头是用户的常规资料，在个人资料页面提供固定配置入口。
// 与付款快捷配置使用同一查询和表单，敏感资料不另存本地。
// 组件只负责常规入口，不把用户资料复制进认证 store。
// 编辑成功由表单更新共享缓存，当前页面无需整页刷新。
export function InvoiceProfileCard() {
  const { t } = useTranslation()
  const profile = useInvoiceProfile() // 页面复用付款区相同的用户资料来源。
  return (
    <TitledCard
      title={t('Invoice title')} // 固定入口使用户无需先发起支付也能配置。
      description={t('Save your usual invoice details for future payments.')}
      icon={<ReceiptText className='size-4' />} // 发票图标属于普通资料配置，避免混同管理员开票入口。
      disableHoverEffect // 编辑表单保持稳定，不以卡片动效暗示跳转。
    >
      {/* 初次读取时不显示空表单，防止把尚未加载的已有资料覆盖掉。 */}
      {profile.isLoading ? <Skeleton className='h-40 w-full' /> : null}
      {/* 查询失败和未配置是不同状态；错误时只提供重试而不伪造空配置。 */}
      {profile.isError ? (
        <div role='alert' className='space-y-2'>
          <p>{t('Failed to load invoice title')}</p>
          <Button variant='outline' onClick={() => void profile.refetch()}>
            {/* 恢复读取而不是清空已有税务资料。 */}
            {t('Retry')}
          </Button>
        </div>
      ) : null}
      {/* 服务端明确返回空值时，才由公共表单显示可创建的默认值。 */}
      {!profile.isLoading && !profile.isError ? (
        <InvoiceProfileForm profile={profile.data ?? null} />
      ) : null}
    </TitledCard>
  )
}
