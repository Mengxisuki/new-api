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
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Combobox } from '@/components/ui/combobox'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { fetchTokenKey, getApiKeys } from '@/features/keys/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import { getTokenResourceModels } from '../api'

// 用户选定统一 Key 后再加载可用模型，不用“全站模型”生成越权配置。
// 明文 Key 只在用户要求生成配置时读取，切换 Key 时组件卸载清除。
// 配置入口复用已有 Key，不额外申请平台身份或原厂凭据。
// Key 列表只读取名称和编号，首次打开不读取明文秘密。
// 分页使用既有 API 的页码与总数，不把第一页当作全部 Key。
// 切换页码清除选择，避免显示已不在当前页的旧 Key 配置。
// 切换 Key 会卸载配置子组件，销毁已读明文和模型选择。
// Key 的显示名称可重复，选项值必须使用稳定数字 ID。
// Key 列表失败时显示错误并允许重试，不伪造没有 Key。
// 用户没有 Key 时可以跳转既有管理页创建。
// 导出并不改变 Key 的资源策略、限额或模型权限。
// 同一个 Key 的有效目录由服务端授权和资源状态共同决定。
// 平台地址使用当前站点的同源入口，不泄露执行层内部地址。
// 复制基础地址不需要读取 API Key。
// 用户须先选择已验证模型，之后才开放配置和明文读取动作。
// 配置文本本身不含 API Key，使用环境变量名称引用秘密。
// 读取密钥是显式按钮操作，避免预加载导致不必要的凭据暴露。
// 复制交互复用共享 CopyButton，保留站点既有失败提示和可访问性。
// 生成的文本由用户合并进现有配置，不自动覆盖本地文件。
// 不会调用外部工具或把密钥写入浏览器下载历史。
// 模型名称可能包含特殊字符，输出时必须按字符串格式转义。
// 目录变化后检查旧模型是否仍可导出，而不是只检查曾经选中过。
// 目录中未验证的发现项不能出现在“已验证模型”选项里。
// 首期只导出 Responses 的 Codex 配置，不猜测其他客户端兼容性。
// 空的验证目录明确提示先测试账号，不能默认推荐某个型号。
// Key 查询成功与模型可调用是两件事，前端不能混成一个已接入状态。
export function HostingClientConfig() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [tokenId, setTokenId] = useState<number | null>(null)
  const keys = useQuery({
    queryKey: ['hosting', 'keys', page],
    queryFn: async () =>
      requireServerSuccess(await getApiKeys({ p: page, size: 20 })).data,
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Client configuration')}</CardTitle>
        <CardDescription>
          {t(
            'Keep the same Oak address and API key when changing resource priority.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-4'>
        {keys.isError && (
          <ErrorState
            onRetry={() => {
              void keys.refetch()
            }}
          />
        )}
        <Field>
          <FieldLabel htmlFor='hosting-config-key'>{t('API Key')}</FieldLabel>
          <Combobox
            id='hosting-config-key'
            options={(keys.data?.items ?? []).map((key) => ({
              value: String(key.id),
              label: key.name,
            }))}
            value={tokenId ? String(tokenId) : null}
            onValueChange={(value) => setTokenId(value ? Number(value) : null)}
            placeholder={t('Select an API key')}
            disabled={keys.isPending}
          />
        </Field>
        <div className='flex flex-wrap gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={page === 1 || keys.isFetching}
            onClick={() => {
              setTokenId(null)
              setPage(page - 1)
            }}
          >
            {t('Previous')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            disabled={keys.isFetching || page * 20 >= (keys.data?.total ?? 0)}
            onClick={() => {
              setTokenId(null)
              setPage(page + 1)
            }}
          >
            {t('Next')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            render={<Link to='/keys' />}
            nativeButton={false}
          >
            {t('Manage API keys')}
          </Button>
        </div>
        {/* 重挂载使前一 Key 的明文、模型选择和查询失败都不会污染下一项。 */}
        {tokenId !== null && <TokenConfig key={tokenId} tokenId={tokenId} />}
      </CardContent>
    </Card>
  )
}

// 仅输出通过真实验证记录的 Responses 配置，不把模型发现当作客户端兼容验收。
// TOML 字符串用 JSON 转义表达，防止模型名或站点地址破坏配置结构。
// 配置子组件只接收非敏感 Key 编号。
// 明文密钥保存在当前 mutation 结果内，不放进 query 持久缓存。
// 父组件通过稳定 Key 编号重新挂载，确保不同 Key 不共享秘密。
// 显式重新选择模型也清理已展示密钥，避免复制旧上下文。
// 接口返回空密钥视为失败，不生成只有前缀的伪密钥。
// 前缀规范化保留已经带前缀的真实值，避免重复拼接。
// 读取失败由应用统一错误链反馈，不返回空字符串冒充成功。
// 模型查询不使用全站定价目录，定价可见性不等于调用权限。
// 服务端可能返回不同协议，导出必须核对 Responses 支持事实。
// verified 缺省为未验证，不能用真假默认值放宽条件。
// 模型确实通过验证仍可能受到额度与状态限制，配置不是可用性承诺。
// 生成配置不发出模型请求，不在浏览或复制时消耗原厂额度。
// 环境变量命名固定为产品提供的客户端约定，不改变站点会话。
// 复制密钥之后剪贴板由用户设备管理，界面不持久化副本。
// 部署在不同域名时导出使用当前域名，避免交付测试环境地址。
// 模型列表错误时保持错误状态，不退回之前缓存的全站模型。
function TokenConfig(props: { tokenId: number }) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState('')
  const models = useQuery({
    queryKey: ['hosting', 'models', 'token', props.tokenId],
    queryFn: () => getTokenResourceModels(props.tokenId),
  })
  // 明文读取与模型目录请求分开，防止目录预取附带泄密。
  // 请求不会在页面加载时执行，也不由查询自动刷新触发。
  // 保存的值只提供给复制控件，不把密钥插入可见配置代码块。
  // 如果用户关闭页面，组件状态随之释放，不写入 localStorage。
  // 服务器现有密钥读取权限仍生效，前端没有绕过二次验证流程。
  // 读取成功并不修改后端 Key 权限，仍需请求时逐次校验。
  const reveal = useMutation({
    mutationFn: async () => {
      const result = requireServerSuccess(await fetchTokenKey(props.tokenId))
      if (!result.data?.key) throw new Error(t('API key is unavailable'))
      return result.data.key.startsWith('sk-')
        ? result.data.key
        : `sk-${result.data.key}`
    },
  })
  const verified =
    models.data?.models.filter(
      (item) => item.verified === true && item.protocols?.includes('responses')
    ) ?? []
  // 每次目录变化重新确认选中模型仍已验证，策略改变不会沿用旧可导出状态。
  const supported = verified.some((item) => item.id === selected)
  // 地址与秘密分开呈现，用户可以安全复制基础入口。
  // 基础地址后缀与 Codex Responses 协议相匹配。
  // 配置中的提供方名称是客户端本地标识，不是原厂账户类型。
  // 所有挂靠账号共享公开入口，账号切换通过平台策略完成。
  // 客户端不需要知道当前账号 ID 或执行层端口。
  // 配置不包含来源优先级副本，避免本地和平台出现两套规则。
  // 用户修改策略后无需重新生成 Key。
  // 模型名由客户端请求固定，模型来源可以按授权顺序回退。
  // 已验证状态表示完成过实测，不保证未来额度永远充足。
  // 选择其他模型时必须重新满足当前目录过滤条件。
  // 模型目录可能因为账号暂停或解绑而收窄。
  // 即使旧选择还在本地 state，supported 也会关闭导出区域。
  // 接口出错时显示重试，不把错误当成全站所有模型可选。
  // 重试读取只针对选中 Key，不增加它的权限。
  // Key 列表的加载状态不触发秘密读取。
  // 翻页清空 Key 选择，也随之卸载已读秘密。
  // 复制完成不向服务器提交密钥内容。
  // 明文仅进入共享复制控件，界面不打印到调试日志。
  // 用户可不读取秘密而先审查 TOML 配置内容。
  // 配置引导提醒不要分享 Key，帮助用户区分公开地址与秘密。
  // 不在导出时写入 localStorage，避免共用电脑留下持久副本。
  // 没有自动下载密钥文件，用户通过明确复制操作掌控传递。
  // 失败状态必须可见，空密钥不会显示为可复制的 sk- 字符串。
  // 模型验证来源由后端提供，浏览器不能自行把 verified 改成 true。
  // Key 本身失效时后端真实调用仍会拒绝，配置导出不豁免令牌校验。
  // 用户已有模型限制继续生效，不因挂靠免费而开放全部型号。
  // 配置所需环境变量值应在客户端进程环境内设置。
  // 这里只生成文档片段，不操作用户电脑上的 shell 或配置文件。
  // 没有真实验证过的协议不生成占位配置。
  // 后续支持更多客户端需增加各自验证和协议目录，而非复制此模板。
  const address = `${window.location.origin}/v1`
  // 配置只描述已验证的调用协议和选中模型。
  // 模型 ID 与 URL 使用字符串转义，避免换行破坏 TOML 结构。
  // 环境变量值由用户在客户端环境中配置，不嵌入此文本。
  // 生产入口固定使用公开 /v1 路径，不包含管理 API 或私人账号 ID。
  // 资源优先级以后改变时地址与 Key 都保持不变。
  // 请求的模型名仍由客户端决定，来源回退不会偷偷换模型。
  const config = [
    'model_provider = "oak"',
    `model = ${JSON.stringify(selected)}`,
    '',
    '[model_providers.oak]',
    'name = "Oak"',
    `base_url = ${JSON.stringify(address)}`,
    'wire_api = "responses"',
    'env_key = "OAK_API_KEY"',
  ].join('\n')
  return (
    <FieldGroup>
      <div className='flex min-w-0 items-center gap-2'>
        <code className='min-w-0 text-xs break-all'>{address}</code>
        <CopyButton value={address} aria-label={t('Copy API address')} />
      </div>
      {models.isError && (
        <ErrorState
          onRetry={() => {
            void models.refetch()
          }}
        />
      )}
      <Field>
        <FieldLabel htmlFor='hosting-config-model'>
          {t('Verified Codex model')}
        </FieldLabel>
        <Combobox
          id='hosting-config-model'
          options={verified.map((model) => ({
            label: model.id,
            value: model.id,
          }))}
          value={selected}
          onValueChange={(value) => {
            setSelected(value ?? '')
            reveal.reset()
          }}
          placeholder={t('Select a model')}
          disabled={models.isPending}
        />
      </Field>
      {models.isSuccess && !verified.length && (
        <p role='status'>
          {t(
            'No model has a verified Responses connection for this key. Test the account before exporting configuration.'
          )}
        </p>
      )}
      {/*
       * 只在当前目录仍验证通过时展示导出结果。
       * 策略变化移除模型后旧选择不再产生可复制配置。
       * 配置说明与密钥读取分开，用户不必为查看参数而暴露秘密。
       * 没有使用内联复制实现，继续沿用项目的共享状态反馈。
       */}
      {supported && (
        <>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Merge this configuration into Codex config.toml and set OAK_API_KEY in your client environment. Do not share your key.'
            )}
          </p>
          <pre className='max-h-64 overflow-auto rounded border p-3 text-xs'>
            {config}
          </pre>
          <CopyButton value={config} size='sm' variant='outline'>
            {t('Copy Codex configuration')}
          </CopyButton>
          <Button
            variant='outline'
            disabled={reveal.isPending}
            onClick={() => reveal.mutate()}
          >
            {t('Reveal selected API key')}
          </Button>
          {reveal.data && (
            <CopyButton value={reveal.data} size='sm' variant='outline'>
              {t('Copy selected API key')}
            </CopyButton>
          )}
        </>
      )}
    </FieldGroup>
  )
}
