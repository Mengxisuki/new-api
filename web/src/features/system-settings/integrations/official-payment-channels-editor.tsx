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
import { Plus } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { ReactIconByName } from '@/components/react-icon-by-name'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

import { OfficialPaymentChannelDialog } from './official-payment-channel-dialog'
import type { OfficialPaymentChannelData } from './official-payment-channel-types'

type OfficialPaymentChannelsEditorProps = {
  value: string // 原始配置来自设置表单，只有用户保存才持久化。
  onChange: (value: string) => void // 输出整份有效数组，不独立写后台接口。
  callbackAddress?: string // 默认通知域名沿用付款设置的顶层回调域。
}

// 结构校验覆盖渲染与编辑所需字段，服务端继续承担商户业务校验。
function isOfficialPaymentChannel(
  value: unknown
): value is OfficialPaymentChannelData {
  if (!value || typeof value !== 'object') return false // null 与标量都不能成为渠道。
  const channel = value as Record<string, unknown> // 所有成员保持 unknown 到入口校验完成。
  return (
    typeof channel.id === 'string' && // 稳定主键被编辑和删除操作引用。
    typeof channel.name === 'string' && // 展示名称必须能按普通文本渲染。
    (channel.channel_type === 'alipay' || channel.channel_type === 'wechat') && // 未知商户不能冒用现有表单。
    ['qr', 'page', 'wap', 'redirect'].includes(
      String(channel.interaction_mode)
    ) &&
    typeof channel.enabled === 'boolean' && // 不能把字符串 false 当作已启用。
    !!channel.config && // 缺少配置对象时不能打开凭据表单。
    typeof channel.config === 'object' && // 商户字段必须是键值映射。
    !Array.isArray(channel.config) && // 数组不是合法商户配置。
    Object.values(channel.config).every((value) => typeof value === 'string') && // 配置输入只支持字符串。
    (channel.configured_secrets === undefined ||
      (Array.isArray(channel.configured_secrets) && // 已存凭据必须提供名称数组，不能误传明文对象。
        channel.configured_secrets.every((value) => typeof value === 'string')))
  )
}

// 编辑器复用项目静态表格与统一行操作，不另起列表或删除交互。
// 组件较长部分是声明式表格列定义，状态仅包含弹窗与编辑索引。
export function OfficialPaymentChannelsEditor(
  props: OfficialPaymentChannelsEditorProps
) {
  const { t } = useTranslation()
  const [dialogOpen, setDialogOpen] = useState(false) // 弹窗与外层保存按钮各自管理生命周期。
  const [editIndex, setEditIndex] = useState<number | null>(null) // null 明确表示新增，不与第零项混淆。

  // 此处是配置文本进入结构化编辑器的唯一入口；整份配置必须有效。
  // 任一渠道无效都阻止编辑，不能过滤坏项后保存而丢失其他商户配置。
  const parsedChannels = useMemo(() => {
    try {
      const parsed: unknown = JSON.parse(props.value || '[]') // 未配置时可创建第一项；非空坏文本不能回退。
      if (!Array.isArray(parsed) || !parsed.every(isOfficialPaymentChannel)) {
        return null // 任一坏项保留原文本，不自动删除或修复商户配置。
      }
      return parsed // every 验证整份数组后才允许更新草稿。
    } catch {
      // 格式错误必须可见，原始配置保持不变，不能伪装为空列表。
      return null
    }
  }, [props.value])
  const channels = parsedChannels ?? [] // 空数组只用于后续 Hook 计算，错误分支不会显示编辑入口。

  // 可读 JSON 便于外层变更比较；密钥留空保留的语义由后端统一实现。
  const saveChannels = (next: OfficialPaymentChannelData[]) => {
    props.onChange(JSON.stringify(next, null, 2)) // 不在编辑器里绕过整页保存执行网络写入。
  }

  // 子弹窗已完成表单校验，此处仅替换当前项或附加一个新商户。
  const handleSave = (channel: OfficialPaymentChannelData) => {
    const next = [...channels] // 不原地修改来自外层设置快照的数组。
    if (editIndex === null) {
      // 新建不会覆盖首个已有商户。
      next.push(channel) // 新渠道追加，其他商户的凭据和启用状态不改动。
    } else {
      next[editIndex] = channel // 编辑保持数组次序，排序由原字段控制。
    }
    saveChannels(next) // 原始 props 快照保留，便于用户取消整页编辑。
  }

  // 错误分支不挂载新增/删除入口，避免以空数组覆盖损坏的输入。
  if (parsedChannels === null) {
    return (
      <Alert variant='destructive'>
        <AlertDescription>
          {t('Official payment channels must be a valid JSON array')}
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <div
      /* 列表与弹窗共享同一草稿，没有额外持久化副本。 */ className='space-y-4'
    >
      <div className='flex justify-end'>
        <Button
          type='button' // 打开编辑器不会提交外层整页设置。
          onClick={() => {
            // 用户主动新增时才打开商户材料表单。
            setEditIndex(null) // 避免沿用上次编辑的渠道索引。
            setDialogOpen(true) // 先确定目标，再显示相应表单草稿。
          }}
        >
          <Plus
            /* 图标只提示新增动作，按钮名称来自文字。 */ className='size-4'
            aria-hidden='true'
          />
          {t('Add official channel')}
        </Button>
      </div>

      {channels.length === 0 ? (
        <div className='text-muted-foreground rounded-md border border-dashed p-8 text-center text-sm'>
          {t('No official payment channels configured')}
        </div>
      ) : (
        <StaticDataTable
          data={channels} // 表格只接收已经完整验证的配置。
          getRowKey={(channel) => channel.id} // 稳定 ID 防止修改顺序后错误复用行状态。
          columns={[
            {
              id: 'channel', // 第一列展示商户名称和通知使用的渠道 ID。
              header: t('Channel'), // 列头随当前语言更新。
              cell: (channel) => (
                <div className='flex min-w-0 items-center gap-2'>
                  <ReactIconByName
                    name={
                      channel.icon ||
                      (channel.channel_type === 'alipay'
                        ? 'SiAlipay'
                        : 'SiWechat')
                    }
                    className='size-5 shrink-0' // 图标不压缩，长名称由后方文本截断。
                    title={channel.name} // 图标的可访问说明与商户展示名一致。
                  />
                  <div className='min-w-0'>
                    <div className='truncate font-medium'>{channel.name}</div>
                    <code
                      /* ID 使用等宽文字，便于核对通知配置。 */ className='text-muted-foreground text-xs'
                    >
                      {channel.id}
                    </code>
                  </div>
                </div>
              ),
            },
            {
              id: 'type', // 业务协议名称与自定义渠道名分开显示。
              header: t('Channel type'),
              cell: (channel) =>
                channel.channel_type === 'alipay'
                  ? t('Alipay')
                  : t('WeChat Pay'),
            },
            {
              id: 'mode', // 扫码或跳转方式在不打开编辑器时也可核对。
              header: t('Interaction mode'),
              cell: (channel) => {
                const labels = {
                  // 切换语言时重新构造显示标签。
                  qr: t('QR code'), // 二维码模式支持在桌面使用手机支付。
                  page: t('Desktop web'), // 支付宝桌面页面模式。
                  wap: t('Mobile web'), // 支付宝移动网页模式。
                  redirect: t('H5 redirect'), // 微信 H5 通过浏览器跳转调起付款。
                }
                return (
                  <Badge variant='outline'>
                    {labels[channel.interaction_mode]}
                  </Badge>
                )
              },
            },
            {
              id: 'status', // 启用标记只表示渠道可供下单，不代表支付成功。
              header: t('Status'),
              cell: (channel) => (
                <Badge variant={channel.enabled ? 'default' : 'secondary'}>
                  {channel.enabled ? t('Enabled') : t('Disabled')}{' '}
                  {/* 颜色之外保留明确文字。 */}
                </Badge>
              ),
            },
            {
              id: 'actions', // 行操作复用共享菜单的键盘与焦点行为。
              header: t('Actions'),
              className: 'text-right',
              cellClassName: 'text-right', // 行操作与表头统一对齐，防止宽表错位。
              cell: (channel) => {
                const index = channels.findIndex(
                  // 行重排后按稳定 ID 重新定位。
                  (item) => item.id === channel.id
                )
                return (
                  <StaticRowActions
                    editLabel={t('Edit')}
                    deleteLabel={t('Delete')}
                    menuLabel={t('Open menu')} // 菜单入口具有独立可访问名称。
                    onEdit={() => {
                      setEditIndex(index) // 编辑只作用于当前稳定 ID 对应的配置。
                      setDialogOpen(true) // 先确定目标，再显示相应表单草稿。
                    }}
                    onDelete={() =>
                      saveChannels(
                        channels.filter((_, itemIndex) => itemIndex !== index) // 删除只更新草稿，尚未写服务器。
                      )
                    }
                  />
                )
              },
            },
          ]}
        />
      )}

      <OfficialPaymentChannelDialog
        open={dialogOpen} // 当前列表唯一编辑弹窗，避免同一渠道多份草稿。
        onOpenChange={setDialogOpen}
        onSave={handleSave} // 子表单仅提交本次编辑项，由父级替换草稿。
        channel={editIndex === null ? null : channels[editIndex]} // 新建明确传空值，禁止残留旧凭据草稿。
        existingChannels={channels} // 子表单使用完整列表检查 ID 唯一性。
        callbackAddress={props.callbackAddress} // 只传回调域名，不从公开状态获取商户密钥。
      />
    </div>
  )
}
