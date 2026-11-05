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
 * 订阅表单契约：
 * 一个消费者代表一种独立接收用途，
 * 而不是一台瞬时连接。
 * 名称只是管理标签，
 * 不能承载登录身份或订单权限。
 * 创建时不接收客户端指定的编号，
 * 编号和所有者由后端确定。
 * 名称去除首尾空白并限制长度，
 * 空名称不能创建匿名接收方。
 * 主题必须来自平台支持的固定目录，
 * 并且至少选择一项。
 * 取消一个主题只改变未来接收过滤，
 * 不应该被解释成确认这个主题的全部历史消息。
 * 增加主题也不会隐式扩展旧令牌，
 * 令牌授权需要单独创建。
 * 复选框使用已有组件，
 * 文本和控件处于同一标签内，
 * 鼠标点击与键盘切换具有相同含义。
 * 用户筛选以逗号分隔的正整数表示，
 * 空白表示当前授权范围内所有用户，
 * 而不是所有数据库用户。
 * 输入中任何非法片段都会使整个字段失败，
 * 不能挑出有效数字继续提交。
 * 特别需要拒绝负号、小数、空段和超过安全整数范围的编号，
 * 避免错误筛选被静默扩大。
 * 重复编号在表单边界去重，
 * 最终按数字升序保存，
 * 使相同订阅配置稳定比较。
 * 筛选不能跨越权限边界：
 * 业务员权限、消费者所有者和事件归属仍由服务端判断。
 * 编辑时沿用消费者编号和启用状态，
 * 不能通过修改名称自动恢复暂停的接收方。
 * 新增和编辑共用一套验证，
 * 保证两个入口对用户筛选的解释一致。
 * 表单由外部消费者编号重新挂载，
 * 切换目标时不会继承另一消费者的草稿。
 * 请求未完成时不能关闭弹窗并让操作结果失去归属。
 * 只有保存返回明确成功后才关闭；
 * 业务拒绝、网络错误和取消均保留当前可见输入。
 * 提交只包含订阅字段，
 * 不携带令牌明文、消费游标或任何代充值资料。
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
  // 空白已单独处理，下面只接受实际编号列表。
  // 逗号后的空段属于输入错误，不能当成缺省筛选。
  // 先验证所有片段，再统一转换数字。
  // 这样不会只保留部分有效编号而悄悄改变关注范围。
  // 安全整数限制保证序列化前后编号精确一致。
  const pieces = value.split(',').map((part) => part.trim())
  if (
    pieces.some(
      (part) => !/^[1-9]\d*$/.test(part) || !Number.isSafeInteger(Number(part))
    )
  ) {
    return null
  }
  // 只有通过整体验证的片段才转换为数值。
  // 去重先于排序，重复关注不会生成多份相同筛选。
  // 数字排序避免字符串排序中十二排在二之前。
  // 返回独立数组，后续表单操作不会修改原始输入。
  // 空输入与非法输入分别返回空数组和空值，调用方必须区分。
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
  // 验证器和提交共用同一个解析入口。
  // 提交阶段不能重新实现另一套编号语法。
  // 只有完整合法的筛选才会送到服务器。
  // 主题排序保证安全审计中同一集合有稳定顺序。
  // 启用状态沿用原值，编辑名称不顺便恢复暂停订阅。
  // 新增时编号留空，后端决定消费者身份和所有者。
  // 提交没有令牌字段，避免把管理凭据错误地加入订阅配置。
  // 保存回调返回失败时不销毁表单。
  // 服务端具体错误由共享请求层展示。
  // 表单等待期间禁止取消，确保结果仍有明确归属。
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
  // 弹窗页脚按钮通过表单编号触发同一提交入口。
  // 按回车和点击保存都会经过真实表单校验。
  // 主题控件沿用共享复选框，不自行处理键盘状态。
  // 每项使用主题键作为稳定标识，翻译名称只负责展示。
  // 错误在对应字段下展示，保留正确的标签关联。
  // 编号说明强调空筛选的授权边界，避免误解为数据库全量。
  // 名称关闭自动完成，避免浏览器带入其他账号登录信息。
  // 用户编号仅为筛选信息，不在这里查询完整用户资料。
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
