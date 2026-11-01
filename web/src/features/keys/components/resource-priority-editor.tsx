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
import { Reorder } from 'motion/react'
import { useTranslation } from 'react-i18next'

import { AutoGroupOrderItem } from '@/components/auto-group-order-item'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { sourceKey, type ResourceSource } from '@/features/hosting/types'

import { API_KEY_FORM_DEFAULT_VALUES } from '../lib/api-key-form'

type ResourcePriorityEditorProps = {
  // value 由详情 DTO 或新建默认值提供，不接受原厂凭据。
  // 数组顺序就是尝试顺序，不能先按类型排序再显示。
  // 值中不携带余额，未知额度不会被错误转换为零。
  // 新连接账号是否入池由后端执行时决定，界面不固化账号列表。
  // hosting 可重复出现，但必须以不同 account_id 区分。
  // hosting_pool 与逐账号来源不能混用，由服务端入口校验。
  value: ResourceSource[]
  // 加载详情及保存请求期间禁用编辑，避免草稿和提交快照分叉。
  // 来源自己的 enabled 只是业务开关，不代表控件能否编辑。
  disabled?: boolean
  // 回调只更新父表单，排序动作不会单独发请求。
  // 关闭抽屉即可放弃这些改动，不需要恢复另一份远端策略。
  // 原子保存和版本冲突由 Key 抽屉及后端共同负责。
  onChange: (sources: ResourceSource[]) => void
}

// 顺序本身就是授权，组件不根据余额或套餐状态自动修改来源。
// 无套餐或无账号时保留对应项，新增资源后自然可用。
// 旧单账号来源按原位置显示，不合并成池，避免扩大授权。
// 切换默认顺序是明确操作，会启用三类来源并纳入本人账号池。
// 该编辑器只处理资源级顺序，渠道组的 Auto 顺序由下方控件管理。
// 钱包来源对应按量计费，不代表开通或购买订阅的支付方式。
// 订阅来源引用已有套餐，组件不自动购买或续费。
// 账号池引用当前用户已连接的账号，不是平台公共上游账号。
// 运行时是否跳过一个来源取决于接口能力和可确认的额度状态。
// 前端不轮询额度来重排，否则刷新会意外改变收费授权。
// 结果未知的请求如何停止属于后端调度合同，排序栏不执行重试。
// 账号备注可能泄露身份；旧单账号项仅显示 Oak 内部编号。
export function ResourcePriorityEditor(props: ResourcePriorityEditorProps) {
  const { t } = useTranslation()
  const keys = props.value.map(sourceKey)
  // 文案直接通过 t 字面量登记，避免动态键遗漏七语言提取。
  // 语言切换只更新标签，资源稳定标识仍保持不变。
  // 单账号编号由服务端提供，不能用数组序号假装账号身份。
  // 四种输入类型来自共享 Schema，不以未知类型默认映射钱包。
  const label = (source: ResourceSource): string => {
    if (source.kind === 'wallet') return t('Pay as you go')
    if (source.kind === 'subscription') return t('Subscription quota')
    if (source.kind === 'hosting_pool') return t('Personal accounts')
    return t('Personal account #{{id}}', { id: source.account_id })
  }

  // 拖动和键盘共用同一个顺序提交入口，不改变启停值。
  // 使用稳定来源标识，不依赖翻译后的名称或可重复的账号备注。
  // React 的 key 跟随来源，键盘移动后焦点仍留在原手柄。
  // disabled 同时拦截程序回调与控件，提交期间保持草稿不变。
  const reorder = (orderedKeys: string[]) => {
    if (props.disabled) return
    // Reorder 只返回本次 values 的排列，不接受外部账号编号。
    // 本组件是受控输入，父级完成更新后才形成下一次拖动快照。
    // 创建新数组保留源对象，避免直接修改 React Hook Form 的默认值。
    // 不在这里丢弃 disabled 项，否则恢复来源时位置会丢失。
    props.onChange(
      [...props.value].sort(
        (left, right) =>
          orderedKeys.indexOf(sourceKey(left)) -
          orderedKeys.indexOf(sourceKey(right))
      )
    )
  }

  return (
    <div
      className='min-w-0 space-y-2'
      role='group'
      aria-label={t('Resource priority')}
    >
      {/*
       * 原有 AutoGroupOrderItem 承担手柄、键盘和焦点行为。
       * 新增横向布局仅改变排列方向，不复制另一套拖动控制器。
       * 列表保留全部来源，长标签和多个旧账号可横向查看。
       * 箭头是装饰，不额外进入键盘焦点顺序。
       * 切换与拖动均不触发保存，因此可连续调整后统一提交。
       */}
      {/* 水平溢出限制在排序栏内，移动端可滑动，不撑宽抽屉。 */}
      <Reorder.Group
        // 滚动容器自身参与布局测量，横滑后拖动仍使用正确的项位置。
        layoutScroll
        axis='x'
        values={keys}
        onReorder={reorder}
        aria-label={t('Resource priority')}
        className='flex max-w-full items-center gap-6 overflow-x-auto px-1 py-1'
      >
        {props.value.map((source, index) => (
          <AutoGroupOrderItem
            key={sourceKey(source)}
            group={sourceKey(source)}
            label={label(source)}
            orientation='horizontal'
            index={index}
            count={props.value.length}
            disabled={props.disabled}
            onMove={(from, direction) => {
              // 边界处保持原序，不能把首项移动到末尾。
              // 禁用项同样可以排序，重新启用时恢复用户选定位置。
              const to = from + (direction === 'up' ? -1 : 1)
              if (to < 0 || to >= keys.length) return
              const next = [...keys]
              ;[next[from], next[to]] = [next[to], next[from]]
              reorder(next)
            }}
          >
            {/*
             * 开关与拖动手柄分离，触摸开关不会触发排序。
             * 完整来源名称是开关的可访问名称，读屏不只听到“启用”。
             * 关闭来源不等于删除它，不能重置账号或取消套餐。
             * 全部关闭是合法的明确拒绝配置，不偷偷保留钱包兜底。
             * 服务端请求时仍校验授权，开关不能替代安全校验。
             */}
            <Switch
              size='sm'
              checked={source.enabled}
              disabled={props.disabled}
              aria-label={t('Enable {{source}}', { source: label(source) })}
              onCheckedChange={(enabled) =>
                props.onChange(
                  props.value.map((item, at) =>
                    at === index ? { ...item, enabled } : item
                  )
                )
              }
            />
          </AutoGroupOrderItem>
        ))}
      </Reorder.Group>
      {/*
       * 空策略和全关闭策略给出同一明确反馈，不能显示为空白加载态。
       * 此提示只解释本 Key 的授权，不暗示用户账户没有余额。
       * 已有旧合同仍允许普通改名，不要求先开启付费来源。
       * 恢复按钮下方的保存操作才会真正改变服务端授权。
       */}
      {props.value.every((source) => !source.enabled) && (
        <p role='status' className='text-muted-foreground text-xs'>
          {t(
            'All resource sources are disabled. Requests with this key will be rejected.'
          )}
        </p>
      )}
      {/* 文案把键盘操作、启停与自动跳过区别开，避免误认为三类必须付费。 */}
      <p className='text-muted-foreground text-xs'>
        {t(
          'Drag to reorder, or focus a handle and use the left and right arrow keys.'
        )}
      </p>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Personal accounts use your connected accounts for free. Disabled resources are never used.'
        )}
      </p>
      {/*
       * 恢复动作显式说明会启用三类资源，旧 Key 不会因普通保存而升级。
       * 使用表单默认值这一唯一来源，不维护另一份容易分叉的顺序。
       * 克隆每个来源对象，后续开关操作不能污染其他新建 Key。
       * 按钮不会创建账号、扣余额或调整订阅额度。
       * 点击后仍可撤销编辑；没有独立远端写入需要回滚。
       */}
      <Button
        type='button'
        variant='link'
        size='sm'
        className='h-auto px-0'
        disabled={props.disabled}
        onClick={() =>
          props.onChange(
            API_KEY_FORM_DEFAULT_VALUES.resource_sources.map((source) => ({
              ...source,
            }))
          )
        }
      >
        {t('Enable all three resources in the default order')}
      </Button>
    </div>
  )
}
