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
  type ConsumerInput,
  type EventConsumer,
} from '../api'

// 用户筛选只缩小订阅范围；空输入必须保持为空数组。
// 拒绝负数、小数和空段，不能将坏输入转换成“所有用户”。
// 去重后保持稳定数字顺序，使编辑和服务端比较一致。
function parseUserFilter(value: string): number[] | null {
  if (!value.trim()) return []
  const pieces = value.split(',').map((part) => part.trim())
  if (
    pieces.some(
      (part) => !/^[1-9]\d*$/.test(part) || !Number.isSafeInteger(Number(part))
    )
  ) {
    return null
  }
  return [...new Set(pieces.map(Number))].sort((a, b) => a - b)
}
// 表单边界保留原输入，错误显示在对应字段下。
// 消费者必须订阅至少一个主题，避免创建无法接收消息的配置。
// 编辑时不改 Token，也不重置已有确认记录。
const schema = z.object({
  name: z.string().trim().min(1).max(64), // 名称用于区分独立接收用途。
  topics: z.array(z.enum(EVENT_TOPICS)).min(1), // 主题来自固定公开目录。
  users: z
    .string()
    .refine(
      (value) => parseUserFilter(value) !== null,
      'Enter positive user IDs separated by commas'
    ),
})
type Values = z.infer<typeof schema>

/**
 * 新建与编辑共用订阅表单，消费者标识由服务端生成。
 * 外部以消费者编号作为 key，切换目标时不继承其他草稿。
 * 成功保存后才关闭；失败保留管理员输入。
 * 禁用状态由列表单独控制，不因编辑订阅被意外恢复。
 */
export function ConsumerEditor(props: {
  consumer?: EventConsumer
  pending: boolean
  onClose: () => void
  onSave: (input: ConsumerInput, id?: number) => Promise<boolean>
}) {
  const { t } = useTranslation()
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: props.consumer?.name ?? '',
      topics: (props.consumer?.topics as Values['topics']) ?? [...EVENT_TOPICS],
      users: props.consumer?.user_ids.join(', ') ?? '',
    },
  })
  const submit = async (values: Values) => {
    const users = parseUserFilter(values.users)
    if (users === null) return
    const saved = await props.onSave(
      {
        name: values.name,
        topics: [...values.topics].sort(),
        user_ids: users,
        enabled: props.consumer?.enabled,
      },
      props.consumer?.id
    )
    if (saved) props.onClose()
  }
  return (
    <Dialog
      open
      title={t(props.consumer ? 'Edit consumer' : 'Create consumer')}
      onOpenChange={(open) => {
        if (!open && !props.pending) props.onClose()
      }}
      footer={
        <>
          <Button
            variant='outline'
            disabled={props.pending}
            onClick={props.onClose}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form='event-consumer-form'
            disabled={props.pending}
          >
            {t('Save Changes')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id='event-consumer-form'
          className='space-y-4'
          onSubmit={form.handleSubmit(submit)}
        >
          <FormField
            control={form.control}
            name='name'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Consumer name')}</FormLabel>
                <FormControl>
                  <Input {...field} maxLength={64} autoComplete='off' />
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
                <FormLabel>{t('Subscribed topics')}</FormLabel>
                <div className='space-y-3'>
                  {EVENT_TOPICS.map((topic) => (
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
          <FormField
            control={form.control}
            name='users'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Follow user IDs')}</FormLabel>
                <FormControl>
                  <Input {...field} placeholder='12, 34' autoComplete='off' />
                </FormControl>
                <p className='text-muted-foreground text-sm'>
                  {t(
                    'Leave empty to follow every user within the authorized scope.'
                  )}
                </p>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Dialog>
  )
}
