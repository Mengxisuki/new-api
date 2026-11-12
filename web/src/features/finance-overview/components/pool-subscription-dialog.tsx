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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'

import { formatShanghaiDay } from '../lib/range'
import {
  savePoolSubscription,
  subscriptionDay,
  type SubscriptionPeriod,
} from '../subscriptions-api'

// 人工记录采用自然日输入，结束日期包含当天。
// 只在提交边界换成左闭右开 Unix 秒，编辑时做逆转换。
// 不复用现有 DatePicker：该组件禁止选择未来日期，无法维护续订周期。
// 输入和弹窗仍复用项目基础控件，不新增日期选择器实现。
// 表单只管理人工记录，不能传入平台用户订阅作为编辑目标。
// 父组件在行操作中限制来源，后端又以独立表和 Root 权限限制写入。
// null 输入表示新建，不通过一个伪造 id 来表达新增模式。
// onClose 只通知宿主关闭，组件自身不操作路由或浏览器历史。
// 日期是经营者维护的合同范围，不代表上游服务器已验证订阅。
// 名称允许中文与空格，提交前只去除首尾空白，不改写内部格式。
// 名称不是账号凭据，也不要求与平台渠道名称一致。
// 新建草稿不预填今天，防止未核对真实日期就误保存默认周期。
// 编辑保留原起止日，窗口外的日期也必须完整回显。
// 保存对象不含 created_at 和 updated_at，时间戳由后端维护。
// 取消尚未提交的草稿不会发出写请求。
// 等待中的关闭请求被忽略，避免保存结果未知时重复创建。
// 表单卸载后不保留本地草稿，重新打开以服务器最新数据为准。
// 接口错误不会覆盖字段内容，用户可以在原草稿上修正。
// 成功提示在缓存失效之后发出，避免提示成功但列表仍使用旧缓存。
// 即使刷新仍失败，保存已经成功的事实也不会被转换成重复提交。
// 编辑周期移出当前窗口后可能消失，这是筛选结果而非删除。
// 中文日期文案通过 i18next 生成，校验错误跟随界面语言。
// 校验器在组件内创建，避免切换语言后保留模块级旧文案。
// 字段错误的关联由 FieldError 与无效标记共同表达。
// 基础 Dialog 提供标题、焦点约束及窄屏滚动，不重复实现弹层。
// 页脚位于 form 外，用 form 属性关联提交，维持共享弹窗布局。
// 保存按钮的禁用状态取 Mutation，不能只依赖表单验证状态。
// 同一天开始结束允许保存，实际覆盖上海当天二十四小时。
// 跨年周期合法，图表将按查看窗口截取可见部分。
// 结束日加一天只在 mutationFn 中发生，不能在表单默认值里预先加。
// DatePicker 的历史查询限制仅属于原组件，此处没有修改它的契约。
// 输入 max 限制四位年份，与服务器日期上限一致。
// 原生日期输入保留浏览器键盘支持，不拦截方向键或 Tab。
// 表单没有自动提交或失焦保存，明确点击保存后才写入。
export function PoolSubscriptionDialog(props: {
  period: SubscriptionPeriod | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  // 名称最多一百二十八个字符，与服务端的存储约束一致。
  // 日期校验绑定具体字段，失败后焦点可定位到相应输入。
  // 名称长度使用表单字符串约束，不按字节限制中文名称。
  // 浏览器 maxLength 是即时辅助约束，Zod 负责提交时的正式前端验证。
  // 服务端按 Unicode 字符验证最大长度，非浏览器客户端也不能超限。
  // 空白名称在 trim 后无字符，按必填字段报告错误。
  // 两个日期各自验证，保证结束顺序校验之前已有清晰的字段反馈。
  // 顺序错误挂在结束日期上，避免两个输入同时收到笼统错误。
  // 保存不限三百六十六天：这是记录合同，而非执行列表查询。
  // 包含结束日意味着开始和结束相同不是零长度区间。
  // 无效日期返回 NaN，大小比较会拒绝提交而不是悄悄选今天。
  // 提交解析结果只包含 schema 定义的三个字段。
  // 后端拒绝消息不写入合同名称，草稿内容保持用户原意。
  // 表单不从 URL 读取默认内容，避免分享链接带入非预期写入值。
  // 该组件不接收付款金额，因此不能被误当作支出登记表。
  const schema = z
    .object({
      name: z
        .string()
        .trim()
        .min(1, t('Required'))
        .max(128, t('Maximum 128 characters')),
      from: z.string().refine((v) => subscriptionDay(v) > 0, t('Invalid date')),
      to: z.string().refine((v) => subscriptionDay(v) > 0, t('Invalid date')),
    })
    .refine((v) => subscriptionDay(v.to) >= subscriptionDay(v.from), {
      path: ['to'],
      message: t('The end date must not be earlier than the start date.'),
    })
  // 父级在打开编辑时挂载表单，初始值不会残留上一条记录。
  // 默认值直接取被选中行，不能把当前图表的裁切边界当成合同边界。
  // end_time 是排他边界，减一秒后才能定位真正包含的最后一天。
  // 如果以后支持分钟级合同，必须同时修改此逆转换与输入控件。
  // 当前业务只允许人工按自然日维护，不截断用户自动订阅的原始数据。
  // 表单数据类型由校验 schema 推导，避免单独接口与校验规则漂移。
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: props.period?.name ?? '',
      from: props.period ? formatShanghaiDay(props.period.start_time) : '',
      to: props.period ? formatShanghaiDay(props.period.end_time - 1) : '',
    },
  })
  // 保存完成后刷新所有图表筛选缓存，切回旧月份也能读到最新数据。
  // 失败由全局 QueryClient 提示；保留草稿，用户可以直接修正后重试。
  // 写入复用统一 API，认证头和会话续期仍由公共网络层处理。
  // 普通业务失败由 requireServerSuccess 转成拒绝，onSuccess 不会误运行。
  // 界面没有乐观插入：服务端生成主键后重新查询，避免临时身份冲突。
  // 新增和编辑共用同一个失效前缀，包含所有月份、分类与页码。
  // 表单不根据当前筛选猜测应将新条目插到哪一页。
  // 序列由服务器按开始日期、来源、编号稳定排列。
  // 保存成功只关闭本弹窗，不改变收入区间或其他财务数据。
  // 网络失败后保留一个持久 alert，用户无需捕捉瞬时 toast。
  // 错误详情仍由公共错误处理保留，不把服务端原因改写成成功。
  const save = useMutation({
    mutationFn: (values: z.infer<typeof schema>) =>
      savePoolSubscription(props.period?.id, {
        name: values.name,
        start_time: subscriptionDay(values.from),
        end_time: subscriptionDay(values.to) + 86400,
      }),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ['finance-subscriptions'] })
      toast.success(t('Saved successfully'))
      props.onClose()
    },
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !save.isPending) props.onClose()
      }}
      title={t(
        props.period ? 'Edit pool subscription' : 'Add pool subscription'
      )}
      description={t(
        'Dates include the end day (Asia/Shanghai). This record does not change revenue or account access.'
      )}
      footer={
        <>
          <Button
            variant='outline'
            disabled={save.isPending}
            onClick={props.onClose}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form='pool-subscription-form'
            disabled={save.isPending}
          >
            {t('Save')}
          </Button>
        </>
      }
    >
      {/* 表单提交同时支持回车和页脚按钮，等待期间禁止修改以保持保存对象一致。 */}
      <form
        id='pool-subscription-form'
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
      >
        <FieldGroup>
          {/* 名称可以描述整个号池或单个账号，无需填写任何原厂凭据。 */}
          <Field data-invalid={Boolean(form.formState.errors.name)}>
            <FieldLabel htmlFor='pool-name'>
              {t('Pool or account name')}
            </FieldLabel>
            <Input
              id='pool-name'
              maxLength={128}
              disabled={save.isPending}
              aria-invalid={Boolean(form.formState.errors.name)}
              {...form.register('name')}
            />
            <FieldError errors={[form.formState.errors.name]} />
          </Field>
          {/* 日期允许未来值；月份跨度与图表查询上限无关，长订阅可以分窗查看。 */}
          {(['from', 'to'] as const).map((field) => (
            <Field
              key={field}
              data-invalid={Boolean(form.formState.errors[field])}
            >
              <FieldLabel htmlFor={`pool-${field}`}>
                {t(field === 'from' ? 'Start date' : 'End date')}
              </FieldLabel>
              <Input
                id={`pool-${field}`}
                type='date'
                max='9999-12-31'
                disabled={save.isPending}
                aria-invalid={Boolean(form.formState.errors[field])}
                {...form.register(field)}
              />
              <FieldError errors={[form.formState.errors[field]]} />
            </Field>
          ))}
          {/* 表单内保留失败状态，除了瞬时提示外还提供可持续读取的反馈。 */}
          {save.isError && (
            <p role='alert' className='text-destructive text-sm'>
              {t('Save failed. Your changes are kept; please retry.')}
            </p>
          )}
        </FieldGroup>
      </form>
    </Dialog>
  )
}
