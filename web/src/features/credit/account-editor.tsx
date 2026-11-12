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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { useDebounce } from '@/hooks/use-debounce'
import { handleServerError } from '@/lib/handle-server-error'

import {
  saveCreditAccount,
  searchCreditUsers,
  type CreditAccountView,
} from './api'

// 表单保留十进制文本，到提交边界才转换为人民币分。
// 禁止超过两位小数、科学记数法和非法上限，避免悄悄舍入额度。
const schema = z.object({
  user_id: z.number().int().positive(), // 用户编号不能为零或非整数。
  amount: z
    .string()
    .regex(/^\d+(\.\d{1,2})?$/)
    .refine((value) => Number(value) <= 100000000),
  enabled: z.boolean(), // 关闭是合法配置值，不能当作缺省。
  recharge_enabled: z.boolean(), // 模块开关必须明确传递。
  topup_enabled: z.boolean(),
  subscription_enabled: z.boolean(),
})

// 编辑用既有 Dialog，不能在支付设置的大表单中嵌套第二个 form。
// 保存按钮显式调用表单校验，所有业务输入仍由 React Hook Form 管理。
// 该组件服务支付网关下的挂帐页签。
// 父组件决定新建或编辑，不在弹窗中推断管理员身份。
// 实际保存仍由后台超级管理员路由保护。
// 新建只选择已存在且启用的用户。
// 搜索关键词与已选择的用户编号独立保存。
// 输入搜索文字不会自动授信给同名账号。
// 列表展示编号、用户名和显示名，便于核对重名用户。
// 候选集合有服务器数量限制，更多用户通过搜索定位。
// 搜索失败提供重试，不能用空候选掩盖接口故障。
// 已有账户编辑时固定用户编号，防止错写另一人的授信。
// 额度输入采用元，接口采用分，转换只发生一次。
// 输入先通过十进制格式校验，最多保留两位小数。
// 不接受负数、科学记数法或超出存储合同的金额。
// 零额度可以保存，但实际付款仍会因额度不足被拒绝。
// 当前占用不进入编辑字段，管理员不能直接抹掉欠款。
// 服务端会拒绝低于当前占用的授信上限。
// 新建默认开放代充值，但直到点击保存才发生授权。
// 总开关与模块开关分别保存，便于保留配置后临时停用。
// 停用用户仍出现在财务面板，不删除历史消费。
// 充值和订阅仅展示预留说明，不生成可点但无效的开关。
// 新增模块接入时需同时扩展本表单和后端白名单。
// 保存不修改钱包余额，也不影响已有订单的归属。
// 表单内部不产生额外付款、退款或结清动作。
// 保存进行中禁止重复确认和关闭弹窗。
// 弹窗关闭后父组件卸载，用户搜索和草稿随之释放。
// 不把用户编号、额度草稿存入浏览器持久存储。
// 已发送请求失败时保留当前输入，方便核对或修正。
// 版本冲突不会自动改用新版本覆盖，用户需刷新重开。
// 界面错误统一通过项目处理器保留服务端原因。
// 金额校验失败使用明确提示，不能悄悄把无效输入改成零。
// 保存成功首先失效管理员及财务共享授信列表。
// 代充值目录也失效，下一次结账重新获得可用支付方式。
// 刷新这些摘要没有资金副作用，不会重复生成订单。
// 保存按钮使用普通按钮类型，避免触发外层网关保存。
// 弹层内容通过项目 FieldGroup 提供一致的表单间距。
// 每个控件与可见标签绑定，键盘与读屏用户可辨认用途。
// 两个授权开关由表单状态驱动，不直接写远端配置。
// 组件不读取完整邮箱、认证密钥或支付商户配置。
// 提交结构显式只包含白名单字段，未知响应字段不会被回传。
// 前端默认值仅用于新建草稿，编辑时保留服务器返回的关闭状态。
// 后台真实保存成功之后才调用关闭回调。
// 调用者不应同时在另一个表单重新实现这组授权逻辑。
export function CreditAccountEditor(props: {
  account?: CreditAccountView
  onClose: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient() // 保存后失效所有授信列表的共享前缀。
  const [keyword, setKeyword] = useState('') // 搜索用户与授信草稿独立。
  const search = useDebounce(keyword) // 后台搜索候选只使用稳定输入。
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema), // 结构和金额格式在输入边界确定。
    defaultValues: {
      user_id: props.account?.user_id ?? 0, // 新建需要用户主动选择。
      amount: String((props.account?.limit_cents ?? 0) / 100), // 编辑显示元，保持分金额合同。
      enabled: props.account?.enabled ?? true, // 新建仍须管理员显式保存。
      recharge_enabled: props.account?.recharge_enabled ?? true,
      topup_enabled: props.account?.topup_enabled ?? false,
      subscription_enabled: props.account?.subscription_enabled ?? false,
    },
  })
  const users = useQuery({
    queryKey: ['credit-users', search], // 不同搜索关键词隔离结果。
    queryFn: () => searchCreditUsers(search), // 仅访问专用安全用户投影。
    enabled: !props.account, // 编辑固定归属，不再请求候选用户。
  })
  const save = useMutation({
    mutationFn: saveCreditAccount, // 保存不混入网关整页配置提交。
    onSuccess: async () => {
      // 配置和财务账户摘要一起失效，旧页面不能继续显示旧授权。
      await client.invalidateQueries({ queryKey: ['credit-accounts'] })
      await client.invalidateQueries({ queryKey: ['recharge'] })
      props.onClose() // 已确认保存后才关闭并释放草稿。
    },
    onError: (error) => handleServerError(error), // 不能把请求变量误传为错误文案。
  })
  const submit = form.handleSubmit((value) =>
    save.mutate({
      user_id: value.user_id, // 只发送表单校验后的用户编号。
      limit_cents: Math.round(Number(value.amount) * 100), // 两位小数校验后一次转为整数分。
      enabled: value.enabled, // 总授权与模块授权分别保存。
      recharge_enabled: value.recharge_enabled,
      topup_enabled: value.topup_enabled,
      subscription_enabled: value.subscription_enabled,
      version: props.account?.version ?? 0,
    })
  )
  return (
    <Dialog
      open
      title={t('Configure on-account access')}
      onOpenChange={(open) => {
        if (!open && !save.isPending) props.onClose() // 保存中保持弹窗可核对。
      }}
      footer={
        <Button
          type='button'
          disabled={save.isPending}
          onClick={() => void submit()}
        >
          {t('Save')}
        </Button>
      }
    >
      <FieldGroup>
        {/* 用户不可在编辑时替换，避免将旧账户额度写到另一个人。 */}
        {props.account ? (
          <p>
            #{props.account.user_id} · {props.account.username}
          </p>
        ) : (
          <>
            <Input
              aria-label={t('Search users')}
              placeholder={t('Search users')}
              value={keyword}
              onChange={(event) => {
                setKeyword(event.target.value)
                // 新搜索重置已选编号，避免界面已无旧候选却保存到旧用户。
                form.setValue('user_id', 0)
              }}
            />
            {users.isError && (
              <ErrorState onRetry={() => void users.refetch()} />
            )}
            <Field>
              <FieldLabel htmlFor='credit-user'>{t('User')}</FieldLabel>
              <NativeSelect
                id='credit-user'
                value={form.watch('user_id')}
                onChange={(event) =>
                  form.setValue('user_id', Number(event.target.value))
                }
              >
                <NativeSelectOption value={0}>
                  {t('Select a user')}
                </NativeSelectOption>
                {users.data?.map((user) => (
                  <NativeSelectOption key={user.id} value={user.id}>
                    #{user.id} · {user.username} {user.display_name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </Field>
          </>
        )}
        {/* 额度单位固定人民币元，说明占用是未结清欠款。 */}
        <Field>
          <FieldLabel htmlFor='credit-limit'>
            {t('Credit limit (CNY)')}
          </FieldLabel>
          <Input
            id='credit-limit'
            inputMode='decimal'
            {...form.register('amount')}
          />
        </Field>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Outstanding balances consume credit. Full settlement restores the available limit.'
          )}
        </p>
        <Field orientation='horizontal'>
          <FieldLabel htmlFor='credit-enabled'>
            {t('Enable on-account access')}
          </FieldLabel>
          <Switch
            id='credit-enabled'
            checked={form.watch('enabled')}
            onCheckedChange={(value) => form.setValue('enabled', value)}
          />
        </Field>
        <Field orientation='horizontal'>
          <FieldLabel htmlFor='credit-recharge'>{t('Account recharge')}</FieldLabel>
          <Switch
            id='credit-recharge'
            checked={form.watch('recharge_enabled')}
            onCheckedChange={(value) => form.setValue('recharge_enabled', value)}
          />
        </Field>
        <Field orientation='horizontal'>
          <FieldLabel htmlFor='credit-topup'>{t('Wallet top-up')}</FieldLabel>
          <Switch
            id='credit-topup'
            checked={form.watch('topup_enabled')}
            onCheckedChange={(value) => form.setValue('topup_enabled', value)}
          />
        </Field>
        <Field orientation='horizontal'>
          <FieldLabel htmlFor='credit-subscription'>{t('Subscription')}</FieldLabel>
          <Switch
            id='credit-subscription'
            checked={form.watch('subscription_enabled')}
            onCheckedChange={(value) =>
              form.setValue('subscription_enabled', value)
            }
          />
        </Field>
        <p className='text-muted-foreground text-sm'>
          {t('Each enabled module shares this user credit limit.')}
        </p>
        {Object.keys(form.formState.errors).length > 0 && (
          <p role='alert'>
            {t(
              'Select a user and enter a valid CNY credit limit with up to two decimal places.'
            )}
          </p>
        )}
      </FieldGroup>
    </Dialog>
  )
}
