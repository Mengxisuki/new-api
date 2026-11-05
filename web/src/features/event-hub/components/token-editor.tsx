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
 * 接入凭据输入契约：
 * 此表单只创建消费者的事件访问凭据，
 * 不创建模型调用密钥或管理访问令牌。
 * 使用者必须先有明确消费者，
 * 令牌自身不能定义另一个接收用途。
 * 可选主题始终取消费者订阅与公开主题目录的交集，
 * 不能选择尚未订阅的事件。
 * 主题至少选一个，
 * 空集合不能被后端当作全部主题。
 * 一个消费者允许多个令牌，
 * 适用于逐台设备授权和更换凭据，
 * 不表示每个令牌有独立确认进度。
 * 需要每个客户端分别收到一份事件时，
 * 应创建不同消费者。
 * 名称用于识别设备或用途，
 * 不能当作认证秘密，
 * 也不接受空名称或超长名称。
 * 默认有效期三十天，
 * 管理员可以明确调整一到三百六十五天。
 * 没有默认永久凭据选项，
 * 避免无意签发无法自然失效的秘密。
 * 有效期在提交瞬间换算成秒级绝对时间，
 * 再与具体主题及消费者一起进入二次验证。
 * 验证进行期间不能重新计算另一份到期时间，
 * 否则证明内容会与实际请求不一致。
 * 表单只传递已规范化的授权输入，
 * 令牌随机生成、哈希保存及连接校验都由后端处理。
 * 不能在浏览器生成可预测的凭据，
 * 不能把消费者编号编码成访问令牌。
 * 验证弹窗出现时隐藏本表单，
 * 防止两个弹层同时争夺焦点；
 * 隐藏期间保留输入，
 * 取消验证后可继续修改。
 * 表单提交不直接显示明文，
 * 创建成功后交给父页面的单次展示流程。
 * 创建失败或验证取消必须保持原输入，
 * 不提前关闭，
 * 也不显示假的成功提示。
 * 数字输入清空时不传入非法渲染值，
 * 提交时再校验完整范围。
 * 按钮禁用跟随真正的创建状态；
 * 正在签发期间不能连续点击造成多份不明确的授权。
 * 父页面切换消费者或登录身份后会撤销未完成请求，
 * 因此旧结果不会出现在新消费者的窗口里。
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
  // 可选项来自公开目录和当前订阅的交集。
  // 消费者中出现未知主题不会变成可授权范围。
  // 默认选择当前可用主题，后续可进一步缩小。
  // 新增订阅后必须重新签发凭据，旧令牌不会自动扩权。
  // 表单初始化只负责输入草稿，不生成访问秘密。
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { name: '', days: 30, topics: available },
  })
  // 签发只构造一次确定的请求对象。
  // 等待安全证明时不重新计算绝对到期时间。
  // 主题排序与证明校验采用同一规范顺序。
  // 名称先经过表单校验和去除首尾空白。
  // 有效期按完整天数换算，不依赖本地日期跨日算法。
  // 创建回调失败或验证取消时保留草稿。
  // 成功才关闭输入，明文展示由页面层单独承接。
  // 这里不把返回的秘密写回输入字段。
  const submit = async (values: Values) => {
    const created = await props.onCreate({
      name: values.name,
      topics: [...values.topics].sort(),
      expires_at: Math.floor(Date.now() / 1000) + values.days * 86400,
    })
    if (created) props.onClose()
  }
  // 隐藏与卸载含义不同，验证期间仍保留草稿。
  // 共享弹窗负责恢复焦点，避免自行实现多层遮罩。
  // 取消按钮在签发中禁用，防止用户误以为请求未发生。
  // 表单字段与页脚提交通过固定编号关联。
  // 数字控件清空只影响输入，不回退到默认天数。
  // 非法数字由校验器阻止，不创建意外的永久凭据。
  // 主题复选框与文本同属标签，点击名称即可切换。
  // 至少保留一个主题，避免空集合语义歧义。
  // 权限说明明确该凭据用于事件接入。
  // 界面不会给凭据附加资金修改或资料读取能力。
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
