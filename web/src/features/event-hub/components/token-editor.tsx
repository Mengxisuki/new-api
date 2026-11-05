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
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import {
  EVENT_TOPICS,
  topicLabels,
  type EventConsumer,
  type TokenInput,
} from '../api'

// 每个 Token 的主题必须是消费者订阅的子集。
// 默认三十天有效，浏览器不隐式创建永久凭据。
// 到期时间在提交时生成，二次验证绑定确切时间戳。
const schema = z.object({
  name: z.string().trim().min(1).max(64), // 轮换时可以保留不同机器的用途名称。
  days: z.number().int().min(1).max(365), // 有效期在可操作范围内显式选择。
  topics: z.array(z.enum(EVENT_TOPICS)).min(1), // 不存在全权限通配符。
})
type Values = z.infer<typeof schema>

/**
 * 令牌创建与订阅配置分开，增加订阅不会扩展旧令牌权限。
 * 验证弹窗出现时隐藏当前表单，但保留输入内容。
 * 成功后退出创建页；明文由父级的单次展示弹窗承接。
 * 取消或失败不会产生伪造的成功结果。
 */
export function TokenEditor(props: {
  consumer: EventConsumer
  pending: boolean
  hidden: boolean
  onClose: () => void
  onCreate: (input: TokenInput) => Promise<boolean>
}) {
  const { t } = useTranslation()
  const available = EVENT_TOPICS.filter((topic) =>
    props.consumer.topics.includes(topic)
  )
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { name: '', days: 30, topics: available },
  })
  const submit = async (values: Values) => {
    const created = await props.onCreate({
      name: values.name,
      topics: [...values.topics].sort(),
      expires_at: Math.floor(Date.now() / 1000) + values.days * 86400,
    })
    if (created) props.onClose()
  }
  return (
    <Dialog
      open={!props.hidden}
      title={t('Create event token')}
      description={t(
        'This token can only receive and acknowledge events. It cannot read account credentials or change funds.'
      )}
      onOpenChange={(open) => {
        if (!open && !props.pending) props.onClose()
      }}
      footer={
        <>
          <Button
            variant='outline'
            onClick={props.onClose}
            disabled={props.pending}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form='event-token-form'
            disabled={props.pending}
          >
            {t('Create event token')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id='event-token-form'
          className='space-y-4'
          onSubmit={form.handleSubmit(submit)}
        >
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Token name')}</FormLabel>
                <FormControl>
                  <Input {...field} maxLength={64} autoComplete='off' />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='days'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Token validity (days)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    max={365}
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
          <FormField
            control={form.control}
            name='topics'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Allowed event topics')}</FormLabel>
                <div className='space-y-3'>
                  {available.map((topic) => (
                    <label
                      key={topic}
                      className='flex items-center gap-2 text-sm'
                    >
                      <Checkbox
                        checked={field.value.includes(topic)}
                        onCheckedChange={(checked) =>
                          field.onChange(
                            checked
                              ? [...field.value, topic]
                              : field.value.filter((item) => item !== topic)
                          )
                        }
                      />
                      {t(topicLabels[topic])}
                    </label>
                  ))}
                </div>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Dialog>
  )
}
