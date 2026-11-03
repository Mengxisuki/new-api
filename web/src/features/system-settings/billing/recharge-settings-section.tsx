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
import { useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Button } from '@/components/ui/button'

import {
  SettingsForm,
  SettingsSwitchField,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

// 表单只保存业务开关，运行就绪标记属于服务端只读诊断。
// 密钥仍由部署环境持久保存，本页不接收也不回显密钥。
// 只有开关开启且加密与支付确认就绪时才可接收新订单。
// 微信状态单独列出，因为商品可能允许余额购买。
const schema = z.object({ enabled: z.boolean() })

// 系统设置沿用 Root 权限，不给业务员新增配置入口。
// 全局开关与商品上架、商品余额开关分别管理。
// 关闭只暂停新购买；已经收款的订单仍需继续履约。
// 已签发支付可能迟到，不能因开关关闭拒绝真实到账。
// 页面不把开关值当作服务健康状态，以免掩盖缺配置。
// 初始值来自系统选项，保存失败保留管理员当前草稿。
// 保存成功才重置脏状态，不用乐观成功覆盖服务端报错。
// 商品查询失效后返回用户页即可看到新接单状态。
// 就绪诊断刷新沿用系统选项查询，不另建轮询或缓存。
// 未配置时允许关闭，重新启用则必须先补齐必要配置。
// 此处的禁用仅提供交互提示，后端仍独立验证启用条件。
// 微信沿用支付网关配置，不在此重复保存商户或通道。
export function RechargeSettingsSection(props: {
  enabled: boolean // 兼容旧服务的接单意图，不代表密钥或渠道一定可用。
  encryptionReady: boolean // 来自当前服务进程，不能用管理员开关替代。
  paymentReady: boolean // 沿用当前版本的支付确认条件，不另建确认流程。
  wechatReady: boolean // 至少存在一个实际可选微信渠道时才为真。
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const updateOption = useUpdateOption() // 复用统一错误提示和系统设置缓存失效。
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: { enabled: props.enabled }, // 从已保存值建立草稿，初始读取不会发起修改。
  })
  const busy = form.formState.isSubmitting || updateOption.isPending
  const ready = props.encryptionReady && props.paymentReady
  // 三项诊断从同一次系统选项响应取得；开关草稿不改变这些值。
  // 标签在组件内翻译，语言切换后重新渲染，不冻结到模块常量。
  const diagnostics = [
    {
      label: t('Account data encryption'),
      value: t(props.encryptionReady ? 'Configured' : 'Not configured'),
    },
    {
      label: t('Payment compliance confirmation'),
      value: t(props.paymentReady ? 'Confirmed' : 'Not confirmed'),
    },
    {
      label: t('WeChat Pay'),
      value: t(props.wechatReady ? 'Available' : 'Not configured'),
    },
  ]
  const enabled = form.watch('enabled') // 监听单字段草稿，保存禁用状态与开关展示保持一致。

  // 单字段提交复用公共设置接口，不能将三个只读状态一并写入。
  // 异常交给公共 mutation 提示，保持表单可重试。
  // 重置前等待持久化响应，失败不宣称设置已生效。
  async function submit(values: z.infer<typeof schema>) {
    try {
      await updateOption.mutateAsync({
        key: 'recharge_setting.enabled', // 固定白名单键，不能提交任意后台字段。
        value: values.enabled, // 显式 false 同样提交，关闭不能依靠省略字段。
      })
      form.reset(values) // 成功后以此次保存值作为新基线，防止重复保存。
      await client.invalidateQueries({ queryKey: ['recharge', 'products'] })
    } catch {
      // 公共 mutation 已显示具体错误；失败保留草稿，不复位为保存成功。
    }
  }

  // 设置页标题由公共布局处理，直接挂载也保留可访问标题。
  // 保存按钮进入既有页头插槽，避免在表单底部再放重复入口。
  // 刷新按钮 type=button，读取诊断不能隐式提交开关草稿。
  // 输入控件复用 SettingsSwitchField，沿用标签、描述与禁用语义。
  // 初次密钥未就绪时，禁止从关闭切到开启，允许已开启状态转为关闭。
  // 保存按钮也执行同一就绪判断，阻止通过快捷提交绕过控件禁用。
  // 服务端接单门禁仍是最终边界，已打开的旧页面不能靠缓存继续支付。
  // 三项诊断在小屏纵向呈现，避免较长标签把状态挤出视口。
  // 金额和商户详情不进入本页，不以读取配置发起任何交易。
  // 部署提示仅针对缺失项，不要求已经配置的管理员重复设置。
  // 密钥配置后的生效需要服务重启，开关本身保存后即可生效。
  // 重启后点击刷新重新获取服务端状态，无须重新创建商品。
  // 配置故障时保留支付网关链接，管理员可以定位独立缺失项。
  // 余额开关仍逐商品控制，不能从缺少微信推定余额付款获准。
  // 本页没有订单取消功能，暂停营业不等于撤销已付款合同。
  // 管理员关闭后仍可从订单工作区继续履约，避免用户支付后无处提交。
  // 页面不会生成新密钥，避免无意轮换导致历史密文无法解开。
  // 前端只持有三个布尔诊断，截图或浏览器缓存不会包含加密材料。
  // 后台开关和密钥诊断不同源，无法通过修改就绪标记来解锁下单。
  // 无效配置的具体异常由服务端固定文案表达，不回显原始配置内容。
  return (
    <SettingsSection title={t('Account top-up settings')}>
      <SettingsForm onSubmit={form.handleSubmit(submit)}>
        <SettingsPageFormActions
          onSave={form.handleSubmit(submit)}
          isSaving={busy}
          isSaveDisabled={!form.formState.isDirty || (enabled && !ready)}
        />
        {/* 允许关闭未就绪服务；打开前检查环境，避免无效启用。 */}
        <SettingsSwitchField
          controlId='recharge-enabled'
          label={t('Enable account top-ups')}
          description={t(
            'Disabling stops new orders and unpaid order payments. Paid orders and payment notifications continue normally.'
          )}
          checked={enabled}
          onCheckedChange={(value) =>
            form.setValue('enabled', value, { shouldDirty: true })
          }
          disabled={busy || (!enabled && !ready)}
        />
        {/* 就绪指标始终显示，不随开关草稿变化而伪造通过状态。
              展示的是本次服务端读取结果，不是管理员填写的布尔值。
              标题与值明确关联，手机也无需靠颜色识别缺失项。
              刷新只读配置，不会执行下单或测试扣款。 */}
        <div className='space-y-4 rounded-xl border p-4'>
          <div className='flex items-center justify-between gap-3'>
            <h3 className='font-medium'>{t('Configuration readiness')}</h3>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() =>
                void client.invalidateQueries({
                  queryKey: ['system-options'],
                })
              }
            >
              {t('Refresh')}
            </Button>
          </div>
          <dl className='grid gap-4 sm:grid-cols-3'>
            {diagnostics.map((item) => (
              <div key={item.label}>
                <dt className='text-muted-foreground text-sm'>{item.label}</dt>
                <dd className='mt-1 font-medium'>{item.value}</dd>
              </div>
            ))}
          </dl>
        </div>
        {/* 只给管理员显示具体部署变量，用户页不暴露内部配置名称。
              解释密钥必须保留，避免把关闭营业误当作密钥轮换。
              不提供生成或复制真实密钥的浏览器操作。
              非敏感部署指引属于设置说明，不写入订单或用户备注。 */}
        {!props.encryptionReady && (
          <p
            role='alert'
            className='rounded-lg border border-amber-500/30 bg-amber-500/5 p-4 text-sm'
          >
            {t(
              'Set OAK_RECHARGE_DATA_KEY in the New API server environment to a Base64-encoded 32-byte random key, then restart the service. Keep the key backed up and do not replace it while order data still needs decryption.'
            )}
          </p>
        )}
        {!props.paymentReady && (
          <p role='alert' className='text-sm'>
            {t(
              'Complete payment compliance confirmation in Payment Gateway before enabling account top-ups.'
            )}
          </p>
        )}
        {!props.wechatReady && (
          <p className='text-muted-foreground text-sm'>
            {t(
              'Configure a WeChat channel in Payment Gateway. Platform balance is available only for products that explicitly allow it.'
            )}
          </p>
        )}
        {/* 配置入口使用既有设置路由，不自行再建一套商户表单。 */}
        <Link
          className='text-sm underline underline-offset-4'
          to='/system-settings/billing/$section'
          params={{ section: 'payment' }}
        >
          {t('Payment Gateway')}
        </Link>
      </SettingsForm>
    </SettingsSection>
  )
}
