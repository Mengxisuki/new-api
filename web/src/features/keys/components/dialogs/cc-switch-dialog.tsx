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
import { useQuery } from '@tanstack/react-query'
import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  getResourcePolicy,
  getTokenResourceModels,
} from '@/features/hosting/api'
import { getUserModels } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

const APP_CONFIGS = {
  claude: {
    label: 'Claude',
    defaultName: 'My Claude',
    modelFields: [
      { key: 'model', labelKey: 'Primary Model', required: true },
      { key: 'haikuModel', labelKey: 'Haiku Model', required: false },
      { key: 'sonnetModel', labelKey: 'Sonnet Model', required: false },
      { key: 'opusModel', labelKey: 'Opus Model', required: false },
    ],
  },
  codex: {
    label: 'Codex',
    defaultName: 'My Codex',
    modelFields: [{ key: 'model', labelKey: 'Primary Model', required: true }],
  },
  gemini: {
    label: 'Gemini',
    defaultName: 'My Gemini',
    modelFields: [{ key: 'model', labelKey: 'Primary Model', required: true }],
  },
} as const

type AppType = keyof typeof APP_CONFIGS

function getServerAddress(): string {
  try {
    const raw = localStorage.getItem('status')
    if (raw) {
      const status = JSON.parse(raw)
      if (status.server_address) return status.server_address
    }
  } catch {
    /* empty */
  }
  return window.location.origin
}

function buildCCSwitchURL(
  app: string,
  name: string,
  models: Record<string, string>,
  apiKey: string
): string {
  const serverAddress = getServerAddress()
  const endpoint = app === 'codex' ? `${serverAddress}/v1` : serverAddress
  const params = new URLSearchParams()
  params.set('resource', 'provider')
  params.set('app', app)
  params.set('name', name)
  params.set('endpoint', endpoint)
  params.set('apiKey', apiKey)
  for (const [k, v] of Object.entries(models)) {
    if (v) params.set(k, v)
  }
  params.set('homepage', serverAddress)
  params.set('enabled', 'true')
  return `ccswitch://v1/import?${params.toString()}`
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  tokenKey: string
  tokenId?: number
}

// 导出入口必须遵守当前 Key 的资源策略，而不是仅看用户能访问哪些模型。
// 此处保留旧 Key 的原有导出合同，不全量迁移用户现有配置。
// 只有服务端明确 legacy=true 才使用原用户模型目录。
// 策略尚未读取或读取失败时不能假定旧合同。
// 启用统一策略后只读取该 Key 的有效模型交集。
// 该交集已经应用来源权限和 Key 模型限制，前端不再扩大范围。
// 挂靠已连接不等于可导出，需要模型与协议有真实验证记录。
// 首期统一资源只验证 Codex 的 Responses 配置。
// Claude 和 Gemini 仍对旧合同开放，对统一策略暂时禁用。
// 禁用用可访问状态表达，避免只在点击后才发现不支持。
// 界面切换 Key 时清空模型和应用选择，避免旧权限状态残留。
// 模型目录变化后提交前重新核对所有已选字段。
// 可选备用模型同样不能越过当前 Key 的目录。
// 导出 URL 构造沿用既有规范，不改变外部应用集成协议。
// 明文 Key 由既有受权入口取得，此组件不自动额外请求秘密。
// Key 编号只用于获取策略，不会暴露到公开配置中的模型名。
// 查询键包含 Key 编号，避免不同令牌共享一份资源目录。
// 打开弹窗才查询，不让后台未使用的导出入口持续请求服务。
// 查询错误显示重试，不把失败当成空目录再推荐默认模型。
// 模型缺少 verified 时按未验证处理，不能以协议存在代替。
// 原有应用名称与默认配置名不参与授权判断。
// 改变应用时清空原模型，避免 Claude 的备用字段带入 Codex。
// 配置中的模型名必须是实际目录 ID，不从显示标签生成。
// 导出动作由用户明确点击，不在模型选择后自动打开外部协议。
// 源策略变化可能使原模型失效，按钮与提交检查共同防止旧配置流出。
// 旧 Key 不要求增加新验证流程，现有三个客户端测试继续保护兼容性。
// 目录权限与真实调用权限仍由后端逐请求校验，导出不授予额外能力。
// 本组件不根据钱包余额猜测模型是否可用。
// 原厂额度未知不等于模型协议未验证，两种状态应由各自数据说明。
// 服务器控制面的异常不能成为跳过 Key 权限过滤的理由。
export function CCSwitchDialog(props: Props) {
  const { t } = useTranslation()
  const [app, setApp] = useState<AppType>('claude')
  const [name, setName] = useState<string>(APP_CONFIGS.claude.defaultName)
  const [models, setModels] = useState<Record<string, string>>({})

  // 旧 Key 保持原目录；已启用统一策略的 Key 必须按自己的权限发现模型。
  // 查询失败不能回退全站目录，否则会推荐该 Key 无权调用的模型。
  // 策略查询是目录选择的前置依赖。
  // 只读取当前 Key，不能把用户默认请求当成 Key 请求的替代。
  // 查询失败保留错误状态，不自动退回原 user-models。
  // 返回的 legacy 标志由后端根据默认与覆盖两层共同决定。
  // 空 sources 仍是一份有效策略，不等价于旧合同。
  // 因此不能通过 sources.length 判断是否走旧目录。
  // 未打开弹窗时保持惰性读取，避免无关请求。
  const policy = useQuery({
    queryKey: ['hosting', 'policy', props.tokenId],
    queryFn: () => getResourcePolicy(props.tokenId),
    enabled: props.open && props.tokenId !== undefined,
  })
  const unified = policy.data?.legacy === false
  // 当前 Key 的模型目录与工作区配置导出共享查询键。
  // 策略保存失效该前缀后，两处入口都会重新读取权限交集。
  // 只有统一策略才启用此查询，旧合同不被新验证门槛误伤。
  // 路径中的编号来自选中行，不从用户输入的模型文本读取。
  // 查询函数仅在有编号的启用状态执行。
  // 没有目录响应时选项为空，不能先展示全站缓存然后再过滤。
  const keyModels = useQuery({
    queryKey: ['hosting', 'models', 'token', props.tokenId],
    queryFn: () => getTokenResourceModels(props.tokenId as number),
    enabled: props.open && unified && props.tokenId !== undefined,
  })
  // 首期只验证 Responses 客户端，不从 OpenAI 风格模型名推断其他协议。
  // 此限制只作用于统一策略，原有 Claude/Gemini 导出继续沿用既有行为。
  const { data: modelsData } = useQuery({
    queryKey: ['user-models-ccswitch'],
    queryFn: async () => requireServerSuccess(await getUserModels()),
    enabled:
      props.open &&
      (props.tokenId === undefined || policy.data?.legacy === true),
    staleTime: 5 * 60 * 1000,
  })

  // 选项从当前应用协议与服务端验证事实共同过滤。
  // 统一策略首期不支持的应用直接没有可导出选项。
  // 模型名即使看起来像 GPT，也不能推断它经过 Responses 验证。
  // 协议存在而 verified=false 的发现项仍被排除。
  // 旧目录缓存可能同时存在，但 unified 分支不能读取它。
  // 所有选项使用稳定 ID，显示文字不改写请求模型名。
  // 依赖包含 Key 策略和目录，变化后重新计算，不冻结首次加载结果。
  // 待定策略返回空选项，避免权限读取过程中的瞬时越权推荐。
  const modelOptions = useMemo(() => {
    if (unified) {
      if (app !== 'codex') return []
      return (keyModels.data?.models ?? [])
        .filter(
          (model) =>
            model.verified === true && model.protocols?.includes('responses')
        )
        .map((model) => ({ value: model.id, label: model.id }))
    }
    if (props.tokenId !== undefined && !policy.data?.legacy) return []
    const items = modelsData?.data ?? []
    return items.map((m) => ({ value: m, label: m }))
  }, [
    modelsData?.data,
    unified,
    app,
    keyModels.data,
    props.tokenId,
    policy.data?.legacy,
  ])

  // 打开或更换 Key 后重置选择，避免导出上一个令牌的模型。
  // 策略从待定转为统一后默认选择已验证的 Codex 应用。
  // 旧合同继续使用原 Claude 默认，不改变存量用户习惯。
  // 该效果只更新界面草稿，不写入 Key 配置或资源策略。
  // 每次打开恢复默认名称，延续原弹窗行为。
  // 模型目录刷新本身不会清空选择，提交时还会确认它仍有效。
  useEffect(() => {
    if (props.open) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setModels({})

      setApp(unified ? 'codex' : 'claude')

      setName(
        unified ? APP_CONFIGS.codex.defaultName : APP_CONFIGS.claude.defaultName
      )
    }
  }, [props.open, props.tokenId, unified])

  // 字段集合沿用客户端配置表，新增协议需同时维护验证目录与字段契约。
  // 展示应用名称本身不代表平台已经完成该客户端的真实兼容性验收。
  const currentConfig = APP_CONFIGS[app]

  // 应用切换会改变需要填写的模型字段集合。
  // 清空字段避免上一个客户端的备用模型被带到新客户端。
  // 默认名称与应用同步，用户之后仍可手动改名。
  // 这里不进行模型请求，也不改变平台账号的绑定。
  const handleAppChange = (val: string) => {
    const appVal = val as AppType
    setApp(appVal)
    setName(APP_CONFIGS[appVal].defaultName)
    setModels({})
  }

  // 提交是最终导出边界，不能仅依赖下拉曾经提供过这个选项。
  // 所有非空模型字段必须仍属于当前目录。
  // 空主模型保留既有提示，用户需要明确选择而非使用猜测默认值。
  // 有效性失败不会调用 window.open，避免输出不受支持的配置。
  // 模型验证与密钥前缀规范化是不同职责，不能互相替代。
  // 一次点击只生成一个外部协议请求，关闭弹窗不意味着模型已运行。
  // 外部 CC Switch 是否启动由用户设备处理，不伪造安装成功状态。
  // 接口地址由既有站点地址逻辑处理，当前变更不另建域名配置。
  const handleSubmit = () => {
    // 目录更新后重新核对所有已选模型，不能导出失效的旧选择。
    // 可选模型字段也属于权限范围，不能只验证主模型。
    if (
      !models.model ||
      Object.values(models).some(
        (model) =>
          model && !modelOptions.some((option) => option.value === model)
      )
    ) {
      toast.warning(t('Please select a primary model'))
      return
    }
    const key = props.tokenKey.startsWith('sk-')
      ? props.tokenKey
      : `sk-${props.tokenKey}`
    const url = buildCCSwitchURL(app, name, models, key)
    window.open(url, '_blank')
    props.onOpenChange(false)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Import to CC Switch')}
      contentClassName='sm:max-w-md'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            disabled={
              Boolean(props.tokenId) &&
              (policy.isPending ||
                policy.isError ||
                (unified &&
                  (keyModels.isPending || keyModels.isError || !models.model)))
            }
            onClick={handleSubmit}
          >
            {t('Open CC Switch')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        {/* 错误留在当前导出弹窗，用户可重试，绝不扩大到全站模型。 */}
        {(policy.isError || keyModels.isError) && (
          <ErrorState
            onRetry={() => {
              void policy.refetch()
              if (unified) void keyModels.refetch()
            }}
          />
        )}
        {unified && keyModels.isSuccess && !modelOptions.length && (
          <p role='status'>
            {t(
              'No model has a verified Responses connection for this key. Test the account before exporting configuration.'
            )}
          </p>
        )}
        <div className='space-y-2'>
          <Label>{t('Application')}</Label>
          <RadioGroup
            value={app}
            onValueChange={handleAppChange}
            className='flex gap-4'
          >
            {(
              Object.entries(APP_CONFIGS) as [
                AppType,
                (typeof APP_CONFIGS)[AppType],
              ][]
            ).map(([key, cfg]) => (
              <div key={key} className='flex items-center gap-2'>
                <RadioGroupItem
                  value={key}
                  id={`app-${key}`}
                  disabled={unified && key !== 'codex'}
                />
                <Label htmlFor={`app-${key}`} className='cursor-pointer'>
                  {cfg.label}
                </Label>
              </div>
            ))}
          </RadioGroup>
        </div>

        <div className='space-y-2'>
          <Label htmlFor='cc-switch-name'>{t('Name')}</Label>
          <Input
            id='cc-switch-name'
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={currentConfig.defaultName}
          />
        </div>

        {currentConfig.modelFields.map((field) => (
          <div key={field.key} className='space-y-2'>
            <Label htmlFor={`cc-switch-${field.key}`} required={field.required}>
              {t(field.labelKey)}
            </Label>
            <Combobox
              id={`cc-switch-${field.key}`}
              aria-label={t(field.labelKey)}
              options={modelOptions}
              value={models[field.key] || ''}
              onValueChange={(v) =>
                setModels((prev) => ({ ...prev, [field.key]: v ?? '' }))
              }
              placeholder={t('Select or enter model name')}
              emptyText={t('No models found')}
            />
          </div>
        ))}
      </div>
    </Dialog>
  )
}
