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
import { useEffect, useState, useId } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { InvoiceProfileForm } from '@/features/invoices/components/invoice-profile-form'
import type { InvoiceProfile } from '@/features/invoices/types'

interface Props {
  required: boolean // 此布尔值同时进入报价和下单，不能只用来显示附加费。
  onRequiredChange: (required: boolean) => void // 父级负责立即失效旧报价并重新请求。
  profile: InvoiceProfile | null | undefined // undefined表示尚未确定，null才是明确未配置。
  loading: boolean // 加载时不能把暂时没有数据解释为无抬头。
  error: boolean // 查询错误必须可见，不能自动用空配置覆盖已有资料。
  onRetry: () => void // 原有资料读取失败时保留明确的恢复入口。
  onSaved: (profile: InvoiceProfile) => void // 仅服务端确认后通知外层重新报价。
}

// 先选择是否开票，再选择付款渠道；默认否由父级明确初始化。
// 选择是后发现未配置抬头时允许当场配置，避免离开付款流程。
// 此组件不决定加价，报价和付款仍由服务器根据分组计算。
// 常规资料页和本弹窗使用相同字段、校验与保存API。
// 税号、邮箱等资料只显示当前用户配置，不接受替代所有者ID。
// 页面重进时默认值由业务父级恢复，不读取本地持久化偏好。
// 加载、失败和未配置分别表达，避免把接口故障当作需要创建抬头。
// 表单不向此组件返回未验证草稿，失败保存不能解锁付款。
// 开票提示说明费用用途，不能把附加费误写成新增模型额度。
// 个人和企业抬头的细节留在公共表单，选择器不复制第二份条件。
// Dialog使用项目公共封装，保留键盘关闭、焦点恢复及手机尺寸。
// 布尔选择与对话框开关只存在内存，不写入认证状态或localStorage。
// 再次配置修改的是今后订单，已有发票快照不受此处影响。
export function InvoiceSelection(props: Props) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(false) // 快捷对话框与是否开票是独立状态，取消不改变付款选择。
  const controlId = useId() // 钱包和订阅可能同时挂载，标签ID不能互相冲突。
  useEffect(() => {
    if (
      props.required &&
      props.profile === null && // 只有确认未配置才主动弹窗，不抢占已有配置的阅读。
      !props.loading &&
      !props.error // 读失败时先修复读取，不向服务器盲写空默认值。
    ) {
      setEditing(true) // 选择是后的快捷路径复用同一资料表单。
    }
  }, [props.required, props.profile, props.loading, props.error])
  return (
    <div className='space-y-3 rounded-lg border p-3'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <Label htmlFor={controlId}>{t('Do you need an invoice?')}</Label>
        <Select
          value={props.required ? 'yes' : 'no'} // 使用显式否值，不把未选择当成同意开票。
          onValueChange={
            (value) => value && props.onRequiredChange(value === 'yes') // 下拉关闭的空值不覆盖真实选择。
          }
          items={[
            { value: 'no', label: t('No') },
            { value: 'yes', label: t('Yes') },
          ]}
        >
          <SelectTrigger id={controlId} className='w-28'>
            {/* 标签与当前选值同属真实选择控件，支持键盘及辅助技术。 */}
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='no'>{t('No')}</SelectItem>
            <SelectItem value='yes'>{t('Yes')}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      {/* 默认不开票时不展示或预读常用抬头资料。 */}
      {props.required ? (
        <div className='text-sm'>
          {props.loading ? <p>{t('Loading invoice title...')}</p> : null}
          {props.error ? (
            <p role='alert'>
              {t('Failed to load invoice title')}{' '}
              <Button variant='link' onClick={props.onRetry}>
                {/* 重试仍从服务端获取，不恢复上一份失败响应。 */}
                {t('Retry')}
              </Button>
            </p>
          ) : null}
          {/* 已配置时显示服务器保存的名称，不以草稿代替有效抬头。 */}
          {props.profile ? (
            <p>
              {t('Invoice title')}: {props.profile.title}
            </p>
          ) : null}
          {/* 读取正常后才提供编辑入口，避免错误状态覆盖已有资料。 */}
          {!props.loading && !props.error ? (
            <Button
              variant='outline'
              size='sm'
              onClick={() => setEditing(true)} // 已配置抬头也可从付款区快速修正。
            >
              {props.profile
                ? t('Edit invoice title')
                : t('Configure invoice title')}
            </Button>
          ) : null}
          <p className='text-muted-foreground mt-2 text-xs'>
            {t(
              'Any invoicing surcharge is included in the payment quote and does not increase platform credit.'
            )}
          </p>
        </div>
      ) : null}
      <Dialog
        open={editing} // 对话框开关不代表资料已保存或付款已成功。
        onOpenChange={setEditing} // 取消关闭保留父级资料，表单草稿不会提交。
        title={t('Invoice title')} // 快捷弹窗和常规页面使用相同字段名称。
        description={t('Save your invoice title to continue payment.')}
        contentClassName='sm:max-w-xl' // 小屏保留项目默认弹窗宽度和内部滚动。
      >
        <InvoiceProfileForm
          profile={props.profile ?? null} // 只有用户主动配置后才使用空表单模板。
          onSaved={(profile) => {
            props.onSaved(profile) // 共享缓存已更新，外层会重新取得含附加费的新报价。
            setEditing(false) // 保存失败由公共表单保留对话框，不走这里的关闭逻辑。
          }}
        />
      </Dialog>
    </div>
  )
}
