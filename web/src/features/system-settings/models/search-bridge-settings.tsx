import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

// 每个目标单独指定搜索模型，清空即可关闭，不用维护第二个启用状态。
// 模型名称与后端约束一致，不能保存不可用的供应商名称或空白字符。
// 这里不保存 API Key，真正凭据仍由现有渠道管理。
const backend = z // 模型值保持精确输入，禁止自动修改别名。
  .string()
  .refine(
    (value) =>
      value === '' ||
      (value.startsWith('gpt-') &&
        value.length > 4 &&
        value.length <= 128 &&
        !/\s/.test(value)),
    'Enter a GPT model name without spaces, or leave empty to disable'
  )
const schema = z.object({ kimi: backend, glm: backend }) // 两个目标共享同一后端模型格式约束。
type Values = z.infer<typeof schema> // 表单类型与校验器保持一致，避免保存不同结构。

// 旧配置坏数据必须展示错误，不能用空表单覆盖管理员已有设置。
// 未配置对应空对象，服务端默认保持关闭。
// 只读取当前正式支持的两个目标；未来扩展需同时更新后端能力范围。
function parseDefaults(raw: string): Values | null {
  try {
    const routes: unknown = JSON.parse(raw || '{}') // 配置来自接口，先当作不可信数据解析。
    if (!routes || typeof routes !== 'object' || Array.isArray(routes)) {
      return null
    }
    const entries = Object.entries(routes) // 拒绝未知目标，不能默默删掉未来或坏配置。
    if (
      entries.some(
        ([key, value]) =>
          !['kimi-k3', 'glm-5.3'].includes(key) ||
          typeof value !== 'string' ||
          value === ''
      )
    ) {
      return null
    }
    const map = routes as Record<string, string> // 键值约束已经在上方入口建立。
    const result = schema.safeParse({
      // 已保存值也使用当前表单合同检查。
      kimi: map['kimi-k3'] ?? '',
      glm: map['glm-5.3'] ?? '',
    })
    return result.success ? result.data : null // 解析失败与正常关闭保持可区分。
  } catch {
    return null
  }
}

// 配置中的目标名称是平台模型，不是渠道改写后的供应商模型。
// 搜索模型也使用平台名称，权限、价格和渠道仍按该名称解析。
// 当前 UI 只呈现已验收的 Kimi K3 与 GLM 5.3，不暗示任意模型可转换。
// 保存接口不校验供应商实时可用性，不能把保存成功等同于模型探测成功。
// 搜索供应商能力或同名组配置变化时，管理员需要重新验证所选路由。
// 参数示例不包含凭据，也不会从浏览器直接调用模型数据面。
// 只在请求确实声明 web_search 时接管，不影响普通文本请求。
// 后端失败会留在用量日志中，设置页不引入自动回退模型。
// 不在浏览器发起试搜索，避免保存设置时产生额外费用。
// 占位模型只是示例，不参与校验或作为空值的后备选择。
// 管理员仍需确保示例名称在自己的渠道目录中可用。
// 用户资金来源由 Key 管理，系统设置不能替用户扩大来源范围。
// 平台计费配置和模型路由配置分开保存，不修改已存在的工具价格。
// 两个目标可以使用不同搜索模型，不强制共享同一个付费渠道。
// 现有配置本身表达启停状态，因此无需重复布尔开关造成状态冲突。
// 运行时只处理同步 Responses；不会把此设置误用到图片或音频接口。
// 来源引用由真实搜索服务返回，页面不能用配置状态宣称搜索已成功。
// 复用现有设置表单、保存按钮与服务端错误处理，避免另建配置接口。
// 两个字段写入同一选项，成功之前不重置草稿或显示启用成功。
// 请求开始时冻结路由，保存只影响之后的新请求。
// 明确说明三类费用，让管理员知道搜索并非仅收一项调用费。
// Key 必须有搜索后端权限，配置不能为用户隐含扩权。
// 价格入口仍为计费设置中的工具价格，不在此复制价格规则。
// 使用输入框方便选择已建渠道的精确模型名，不猜测可用的供应商别名。
// 清空字段会删除对应键，服务端能立即关闭该目标的桥接。
// 页面加载失败不发起任何修改，后台校验仍是最终边界。
// 外层组件提供已保存数据，查询刷新不会覆盖尚未保存的表单。
export function SearchBridgeSettings(props: { defaultValue: string }) {
  const { t } = useTranslation() // 标签在渲染时翻译，切换语言不冻结文案。
  const update = useUpdateOption() // 沿用 Root 配置权限和统一失败提示。
  const defaults = parseDefaults(props.defaultValue) // 只从已保存配置建立初始草稿。
  const form = useForm<Values>({
    resolver: zodResolver(schema), // 提交前的校验不能替代后端验证。
    defaultValues: defaults ?? { kimi: '', glm: '' }, // 坏数据仅用于初始化 hook，后续会显示错误而非表单。
  })
  const observed = useRef(props.defaultValue) // 记录查询版本，避免保存后被旧响应重置。
  useEffect(() => {
    // 只有真正的新配置才重置，保存完成不能被旧查询值覆盖。
    if (observed.current === props.defaultValue) return
    observed.current = props.defaultValue // 草稿编辑中也记住已观察版本，防止延迟覆盖。
    const next = parseDefaults(props.defaultValue) // 后台刷新仍需校验，不能把坏值注入受控输入。
    if (next && !form.formState.isDirty) form.reset(next)
  }, [props.defaultValue, form, form.formState.isDirty])
  const submit = form.handleSubmit(async (values) => {
    // 保存入口与顶部操作按钮共用同一校验函数。
    const routes: Record<string, string> = {} // 重建完整映射，清空字段确实删除旧路由。
    if (values.kimi) routes['kimi-k3'] = values.kimi
    if (values.glm) routes['glm-5.3'] = values.glm
    try {
      await update.mutateAsync({
        // 等待服务端确认，失败时保留当前编辑内容。
        key: 'WebSearchBridgeRoutes', // 配置与工具价格是两项独立的管理员选项。
        value: JSON.stringify(routes), // 使用标准 JSON 编码，模型名不能注入额外配置键。
      })
      form.reset(values) // 只在成功后清除脏状态。
    } catch (error) {
      // hook 已负责统一提示；保留草稿并展示错误，不留下未处理 Promise。
      form.setError('root', { message: getServerErrorMessage(error) }) // 错误在页面可见，同时避免未处理的异步拒绝。
    }
  })
  // 参数异常时保留诊断，允许管理员通过选项 API 修复原始配置。
  // 不使用默认模型偷偷开启付费能力。
  if (!defaults) {
    return <p role='alert'>{t('Invalid search bridge configuration')}</p>
  }
  return (
    <SettingsSection title={t('Web search bridge')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'The target model decides when to search. GPT executes web_search and returns cited evidence. Target tokens, GPT tokens and search calls are billed separately using existing prices.'
        )}
      </p>
      <p className='text-muted-foreground text-sm'>
        {t(
          'The API key must allow the GPT search model and a valid funding source. Search tool prices are configured in Billing settings. Responses are streamed by stage with keep-alive events.'
        )}
      </p>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Choose a model routed exclusively to a search-capable upstream. A shared model name across Codex and OpenCode Go can route searches to an unsupported provider.'
        )}
      </p>
      <Form {...form}>
        <SettingsForm onSubmit={submit}>
          <SettingsPageFormActions
            onSave={submit}
            isSaving={update.isPending}
            isSaveDisabled={!form.formState.isDirty}
          />
          {form.formState.errors.root && (
            <p role='alert'>{form.formState.errors.root.message}</p>
          )}
          {(['kimi', 'glm'] as const).map((name) => (
            <FormField
              key={name}
              control={form.control}
              name={name}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {name === 'kimi' ? 'Kimi K3' : 'GLM 5.3'} ·{' '}
                    {t('GPT search model')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      placeholder='gpt-6.1-sol'
                      disabled={update.isPending}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Enter an available GPT model; leave empty to disable this target.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
