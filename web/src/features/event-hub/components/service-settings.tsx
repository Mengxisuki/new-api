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
  const dirty = form.formState.isDirty
  useEffect(() => {
    if (!dirty) form.reset(props.settings)
  }, [props.settings, form, dirty])
  const submit = async (input: EventSettings) => {
    if (await props.onSave(input)) form.reset(input)
  }
  const minimum = form.watch('anomaly_min_quota')
  const cold = form.watch('anomaly_cold_quota')
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
