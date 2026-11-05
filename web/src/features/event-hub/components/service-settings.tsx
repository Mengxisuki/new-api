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
/**
 * 配置输入契约：
 * 启用开关只控制网络投递，
 * 关闭期间仍然保存平台产生的事件。
 * 重新启用后消费者从已有确认状态继续接收，
 * 不通过开关重置消费进度。
 * 保留天数决定事件能被恢复的最长周期；
 * 调整时不承诺重新找回已经清理的通知。
 * 费用比较有两个入口：
 * 样本足够时同时比较历史倍数与绝对额度，
 * 样本不足时只采用独立高额阈值。
 * 最小样本数不能超过后台基线保留数量；
 * 配置一百以上会让历史检测永远无法启用，
 * 因此在表单边界直接拒绝。
 * 历史倍数必须大于一，
 * 不能用正常费用的一倍定义异常；
 * 小数步长与最小值对齐，
 * 避免浏览器原生校验阻止正常提交。
 * 额度字段保存原始内部整数，
 * 不能把展示用人民币或美元直接写回。
 * 下方换算只帮助管理员理解当前站点的显示金额，
 * 汇率变动不会自动改写设置。
 * 货币和语言遵循全站格式化工具，
 * 切换界面语言后立即重新计算展示。
 * 数值输入允许短暂清空，
 * 清空时渲染空串；
 * 不能把非法数字写到输入属性造成浏览器警告。
 * 最终提交仍由校验器要求完整有效的数值，
 * 不给缺失字段静默补默认值。
 * 后台状态每五秒拉取一次，
 * 但表单正在编辑时不能被远端快照覆盖。
 * 未修改时可接受新的服务端配置，
 * 保存成功后才把当前输入设为新的基线。
 * 保存失败保留草稿和修改标记，
 * 管理员可以修正后重试。
 * 保存按钮根据修改状态和真实请求状态禁用，
 * 不能因为查询刷新而宣称配置已保存。
 * 多个数值字段共用标准表单组件及错误提示，
 * 输入标签与控件关联，
 * 键盘提交与鼠标提交经过相同校验。
 */
import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
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
import { Switch } from '@/components/ui/switch'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'

import type { EventSettings } from '../api'

// 下限和倍数采用独立条件，防止零星微额费用频繁触发告警。
// 输入始终保留整数额度，货币文本仅作辅助展示。
// 浏览器校验服务于操作提示，服务端仍校验所有边界。
const schema = z.object({
  enabled: z.boolean(), // 关闭不等于停止保存事件。
  retention_days: z.number().int().min(1).max(365), // 保存周期有明确容量边界。
  anomaly_min_quota: z.number().int().min(1).max(2147483647), // 不舍入配置额度。
  anomaly_multiplier: z.number().gt(1).max(1000), // 一倍没有异常判定意义。
  anomaly_min_samples: z.number().int().min(1).max(100), // 基线只保留最近一百笔。
  anomaly_cold_quota: z.number().int().min(1).max(2147483647), // 不推测缺失历史的正常值。
})
type NumericSetting = Exclude<keyof EventSettings, 'enabled'>
// 五个字段共用一套真实表单绑定，避免分别实现数值转换。
const fields: {
  name: NumericSetting
  label: string
  max: number
  min: number
  step: number
}[] = [
  {
    name: 'retention_days',
    label: 'Event retention (days)',
    min: 1,
    max: 365,
    step: 1,
  },
  {
    name: 'anomaly_min_quota',
    label: 'Minimum unusual charge (quota)',
    min: 1,
    max: 2147483647,
    step: 1,
  },
  {
    name: 'anomaly_multiplier',
    label: 'Historical median multiplier',
    min: 1.01,
    max: 1000,
    step: 0.01,
  },
  {
    name: 'anomaly_min_samples',
    label: 'Minimum historical samples',
    min: 1,
    max: 100,
    step: 1,
  },
  {
    name: 'anomaly_cold_quota',
    label: 'Charge threshold without history (quota)',
    min: 1,
    max: 2147483647,
    step: 1,
  },
]

/**
 * 服务配置与五秒状态刷新分离。
 * 管理员正在编辑时保留草稿，不能被自动拉取覆盖。
 * 保存失败不重置表单，成功后才更新本地基线。
 * 费用展示遵循站点货币设置和当前界面语言。
 */
export function ServiceSettings(props: {
  settings: EventSettings
  pending: boolean
  onSave: (input: EventSettings) => Promise<boolean>
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const form = useForm<EventSettings>({
    resolver: zodResolver(schema),
    defaultValues: props.settings,
  })
  // 修改标记由表单真实字段状态决定。
  // 后台刷新只在没有本地修改时重置基线。
  // 保存失败不会清除修改标记。
  // 保存成功采用已提交输入作为新基线。
  // 两个额度字段单独订阅，辅助金额随输入即时更新。
  // 金额辅助展示不能参与保存请求的数字转换。
  const dirty = form.formState.isDirty
  useEffect(() => {
    if (!dirty) form.reset(props.settings)
  }, [props.settings, form, dirty])
  const submit = async (input: EventSettings) => {
    if (await props.onSave(input)) form.reset(input)
  }
  const minimum = form.watch('anomaly_min_quota')
  const cold = form.watch('anomaly_cold_quota')
  // 整数额度使用统一数值输入，保留内部计费精度。
  // 历史倍数允许小数，步长和起点必须兼容。
  // 所有数值都有上限，界面与服务端边界保持一致。
  // 浏览器原生约束和表单校验共同保护普通输入路径。
  // 最终校验仍以服务端为准，前端不能替代授权和数据验证。
  // 输入变成空值时显示空串，不把非数字渲染到属性。
  // 当用户修正后继续使用原草稿，无需重新填写其他字段。
  // 币种与站点汇率来自共享格式工具。
  // 语言切换会触发重新渲染，因此辅助金额同步更新。
  // 暂停开关是显式布尔字段，不以缺失字段推断状态。
  // 保存动作提交完整规则，服务端不需要猜测局部变更。
  // 没有本地修改时禁用保存，避免轮询造成无意义写入。
  // 请求中禁用保存，防止先后结果覆盖运营人员判断。
  // 配置区域与统计区域分离，异步拉取不干扰输入焦点。
  // 字段定义只保存标签键，展示时由当前语言翻译。
  // 同一映射负责数值边界和控件参数，减少手工复制。
  // 每个字段继续使用标准 FormMessage，保持错误关联。
  // 整个配置只改变异常检测规则和通知保留，不修改既有账单。
  return (
    <Form {...form}>
      <form
        className='space-y-5 rounded-lg border p-4 sm:p-5'
        onSubmit={form.handleSubmit(submit)}
      >
        <div className='flex flex-wrap items-start justify-between gap-3'>
          <div>
            <h4 className='font-medium'>{t('Event service')}</h4>
            <p className='text-muted-foreground mt-1 text-sm'>
              {t('Pausing delivery keeps events for later delivery.')}
            </p>
          </div>
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <FormItem className='flex items-center gap-2'>
                <FormLabel>{t('Enable event delivery')}</FormLabel>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={props.pending}
                  />
                </FormControl>
              </FormItem>
            )}
          />
        </div>
        <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-3'>
          {fields.map((definition) => (
            <FormField
              key={definition.name}
              control={form.control}
              name={definition.name}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t(definition.label)}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={definition.min}
                      max={definition.max}
                      step={definition.step}
                      disabled={props.pending}
                      {...field}
                      value={Number.isNaN(field.value) ? '' : field.value}
                      onChange={(event) =>
                        field.onChange(event.currentTarget.valueAsNumber)
                      }
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
        </div>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <p className='text-muted-foreground text-sm'>
            {t('Charge limits in display currency: {{minimum}} / {{cold}}', {
              minimum: formatQuotaWithCurrency(minimum || 0, { locale }),
              cold: formatQuotaWithCurrency(cold || 0, { locale }),
            })}
          </p>
          <Button type='submit' disabled={props.pending || !dirty}>
            {t('Save Changes')}
          </Button>
        </div>
      </form>
    </Form>
  )
}
