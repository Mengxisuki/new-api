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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Reorder } from 'motion/react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { AutoGroupOrderItem } from '@/components/auto-group-order-item'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'

import {
  getHostingAccounts,
  getResourcePolicy,
  saveResourcePolicy,
} from '../api'
import {
  sourceKey,
  type HostingAccount,
  type ResourcePolicy,
  type ResourceSource,
} from '../types'

// key 中包含作用域，切换 Key 时不会把另一个 Key 的草稿写入当前资源。
// 编辑器只管理来源授权顺序，不在保存时发出模型请求。
// 未配置任何策略的旧 Key 仍保留原计费合同。
// 打开编辑器不能自动把旧 Key 迁移到统一策略。
// 第一次保存属于用户明确建立策略，页面提前解释这一变化。
// 空列表是有效的禁止全部来源策略，而非缺省值。
// 禁用开关保留来源位置，不通过删除和重加实现禁用。
// 新连接账号只出现在候选列表，不自动进入实际调用顺序。
// 来源名称用于显示，稳定身份仍由种类与账号编号组成。
// 账号备注修改不会改变原有优先级或造成重复项。
// 同名账号的拖动、删除和启停仍按各自稳定标识处理。
// 钱包是明确付费来源，启用时始终显示可能扣款提示。
// 来源回退改变资源身份，不允许改写客户端请求的模型。
// API Key 的现有限额、IP 和模型权限不因策略保存而放宽。
// 页面无法判断上游此刻是否有额度，因此不把观测当授权条件。
// 账号已暂停仍可保留在策略中，运行时按状态跳过。
// 移除来源只改调用策略，不等于解绑原厂账号。
// 删除账号只影响资源可用性，历史策略引用不会被错误指向别人。
// 保存时禁用所有编辑入口，避免在提交中继续改变草稿。
// 保存后的提示来自成功响应，不能把发出请求当成生效。
// 发生版本冲突时保留草稿供用户审查，不自动再次提交。
// 重新加载明确撤销当前草稿，恢复最新读取内容。
// 账号列表更新不应擅自重排当前编辑中的来源。
// 表单通过版本重新挂载，避免成功保存后仍停留在脏状态。
// 本编辑器只能从具体 API Key 菜单进入，编号是必填作用域。
// 账号摘要共享读取，草稿、版本与保存地址始终属于当前 Key。
// 切换 Key 重新挂载表单，不能把 A 的未保存顺序带给 B。
// 新增账号只进入候选列表，任何 Key 都不会自动选用它。
// 未设置策略仍走既有平台计费，第一次保存才建立独立顺序。
export function ResourcePolicyEditor(props: { tokenId: number }) {
  // 账号候选列表属于当前用户，与策略接口分开读取。
  // 两个查询都成功后才允许编辑，避免缺失账号被误删。
  // 重试只重新读数据，不改变用户授权，也不请求明文 Key。
  // 即使候选列表中的备注变化，已经保存的来源标识仍有效。
  const policy = useQuery({
    queryKey: ['hosting', 'policy', props.tokenId], // 两个 Key 的缓存与草稿严格分开。
    queryFn: () => getResourcePolicy(props.tokenId), // 保存和读取始终指向同一个 Key。
  })
  const accounts = useQuery({
    queryKey: ['hosting', 'accounts'],
    queryFn: getHostingAccounts,
  })
  if (policy.isPending || accounts.isPending) return <LoadingState />
  if (policy.isError || accounts.isError) {
    return (
      <ErrorState
        onRetry={() => {
          void policy.refetch()
          void accounts.refetch()
        }}
      />
    )
  }
  // 服务端版本变化后新建表单，成功保存或重新读取才替换当前草稿。
  // 列表刷新不会擅自插入新账号；添加资源始终是显式操作。
  return (
    <ResourcePolicyForm
      key={`${props.tokenId}:${policy.data.revision}`}
      policy={policy.data}
      accounts={accounts.data}
      tokenId={props.tokenId}
    />
  )
}

// 草稿只代表当前 Key，加载完成即可直接排序与启停。
// 表单草稿以服务端完整快照初始化。
// 不在渲染时直接修改响应对象，避免污染共享查询缓存。
// dirty 只描述本地改动，不表示后端版本已经变化。
// 保存按钮要求确有改动，防止单纯浏览触发迁移。
// 读取时版本随提交发送，后端负责乐观并发控制。
function ResourcePolicyForm(props: {
  policy: ResourcePolicy
  accounts: HostingAccount[]
  tokenId: number
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [sources, setSources] = useState(props.policy.sources) // 初始快照不因账号刷新而自动追加来源。
  const [dirty, setDirty] = useState(false) // 浏览旧 Key 不会触发付费授权变更。
  // 保存请求只传允许的来源字段和原始版本。
  // 成功后失效策略和模型目录，因为来源改变可能收窄模型权限。
  // 未成功之前不刷新成猜测的新状态，也不弹保存成功。
  // 错误交给统一 Query 错误处理，保留服务端冲突提示。
  const mutation = useMutation({
    mutationFn: () =>
      saveResourcePolicy(props.policy.revision, sources, props.tokenId),
    onSuccess: () => {
      toast.success(t('Resource priority saved'))
      void client.invalidateQueries({
        queryKey: ['hosting', 'policy', props.tokenId],
      })
      void client.invalidateQueries({
        queryKey: ['hosting', 'models', 'token', props.tokenId],
      })
    },
  })
  // 未保存的自定义只存在本地，取消弹窗不会写入后端。
  // 刷新按钮撤销本地草稿，即使服务端 revision 没变化。
  // 服务端 revision 改变时重挂载，避免把旧草稿混入新版本。
  // 版本冲突不自动合并来源，避免意外启用付费钱包。
  // 服务器决定账户是否仍归当前用户，候选列表不是永久授权。
  // 挂靠账号引用失效时显示不可用，不自动替换成另一个账号。
  // 用户移除不可用项后可以保存新的完整顺序。
  // 空数组和全禁用数组都能明确阻止调用，但保留顺序的体验不同。
  // 禁用所有来源的提示应立即响应草稿，而不是等保存后才发现。
  // 钱包既可以排最后，也可以禁用或移除。
  // 包月耗尽时是否回退，由同一列表中的后续来源决定。
  // 来源顺序不会改变请求中的 model 字段。
  // 前端不按模型供应商自动插入另一种来源。
  // 切换界面语言仅改变显示名称，不能改变来源稳定键。
  // 拖动库使用纵向排序，移动端仍提供按钮和键盘替代路径。
  // 操作可访问名称使用当前备注，方便区分多个账号。
  // 保存完成失效模型目录，导出入口不继续使用旧权限集合。
  // 页面中的费用提示跟随启停状态，不把移除钱包误说成全平台免费。
  // 显式刷新只读策略，不重新购买套餐或加载明文 Key。
  // 保存期间冻结当前草稿，后续响应只刷新本 Key 的缓存。
  const readonly = mutation.isPending
  // 候选项包括本人账号及两种平台来源；未加入的资源不会参与调用。
  // 旧 Key 保持原授权范围，不因为页面初始化就添加钱包或新账号。
  // 候选账号剔除正在删除和已经删除的资源。
  // 暂停账号仍可选，便于用户先安排顺序再恢复账号。
  // 平台包月和钱包各只有一个逻辑来源，不按套餐实例扩展 UI。
  // 未点击添加的来源不会因为候选项存在而参与调用。
  // 按钮通过稳定标识排除已添加项，不能仅比较显示名称。
  const candidates: ResourceSource[] = [
    ...props.accounts
      .filter((account) => !['deleting', 'deleted'].includes(account.status))
      .map(
        (account): ResourceSource => ({
          kind: 'hosting',
          account_id: account.id,
          enabled: true,
        })
      ),
    { kind: 'subscription', enabled: true },
    { kind: 'wallet', enabled: true },
  ]
  // 账号备注可能为空或资源已不存在，使用明确不可用标签。
  // 标签不可用不代表自动删除来源，避免改变保存时授权意图。
  // 钱包和包月名称通过实时翻译生成，语言切换后同步更新。
  // 资源编号只用于身份，不作为普通用户必须理解的配置概念。
  const label = (source: ResourceSource) => {
    if (source.kind === 'wallet') return t('Wallet balance')
    if (source.kind === 'subscription') return t('Subscription quota')
    return (
      props.accounts.find((account) => account.id === source.account_id)
        ?.alias ?? t('Unavailable account')
    )
  }
  // 所有草稿改动统一经过只读门槛。
  // 除了禁用按钮，还拦截拖动结束等可能晚到的回调。
  // 保存期间的拖动惯性不能在后台偷偷修改即将提交的顺序。
  // 草稿变动才置脏，重新加载会显式清除这个标记。
  const change = (next: ResourceSource[]) => {
    if (readonly) return // 提交中的拖拽惯性不能覆盖已冻结的请求体。
    setSources(next) // 完整顺序作为一次授权保存，不逐项发送网络请求。
    setDirty(true) // 仅修改草稿；用户保存前不改变当前 Key 的消费授权。
  }
  // 拖动按稳定 ID 排列；备注相同的账号仍是两个不同来源。
  // 拖动库回传稳定 ID 数组，这里映射回原始来源对象。
  // 不能按新的视觉下标重建账号，否则同名资源会被错换。
  // 映射保持每个来源原来的启停状态。
  // 未知 ID 不生成资源对象，合法来源仍由服务端提交校验。
  // 排序不触碰账号配置、余额或原厂额度。
  const reorder = (keys: string[]) =>
    change(
      keys.flatMap((key) =>
        sources.filter((source) => sourceKey(source) === key)
      )
    )
  // 键盘和上下按钮使用同一移动函数，行为与拖动一致。
  // 首项不能再上移，末项不能再下移，边界直接返回。
  // 移动前复制数组，避免改变 props 中的服务端快照。
  // 交换的是完整来源对象，不遗漏 enabled 或 account_id。
  const move = (index: number, direction: 'up' | 'down') => {
    // 同一移动约束用于键盘和按钮，避免两种操作得到不同顺序。
    const target = direction === 'up' ? index - 1 : index + 1
    if (readonly || target < 0 || target >= sources.length) return
    const next = [...sources] // 不修改 Query 缓存提供的服务器快照。
    ;[next[index], next[target]] = [next[target], next[index]]
    change(next)
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Usage order')}</CardTitle>
        <CardDescription>
          {t(
            'Drag to set which quota this API key uses first. When a source is unavailable before execution, the next enabled source is tried. Other keys are unaffected.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-4'>
        {props.policy.legacy && (
          <Alert>
            <AlertDescription>
              {t(
                'This key still uses its previous billing rules. Saving creates an explicit resource policy.'
              )}
            </AlertDescription>
          </Alert>
        )}
        {/*
         * 添加按钮列出未加入的来源，用户不必输入内部编号。
         * 账号备注变化只影响按钮文字，不影响去重规则。
         * 添加一个来源不会顺便启用其他已禁用的来源。
         * 新增项排在末尾，后续可以显式拖到需要的位置。
         */}
        <div className='flex flex-wrap gap-2' aria-label={t('Add resource')}>
          {candidates
            .filter(
              (item) =>
                !sources.some((source) => sourceKey(source) === sourceKey(item))
            )
            .map((item) => (
              <Button
                key={sourceKey(item)}
                variant='outline'
                size='sm'
                disabled={readonly}
                onClick={() => change([...sources, item])}
              >
                {t('Add {{resource}}', { resource: label(item) })}
              </Button>
            ))}
        </div>
        {/* 共用 Auto 的手柄、上下键与按钮；保存期间所有交互都禁用。 */}
        <Reorder.Group
          axis='y'
          values={sources.map(sourceKey)}
          onReorder={reorder}
          className='space-y-2'
          aria-label={t('Usage order')}
        >
          {sources.map((source, index) => (
            <AutoGroupOrderItem
              key={sourceKey(source)}
              group={sourceKey(source)}
              label={label(source)}
              index={index}
              count={sources.length}
              disabled={readonly}
              onMove={move}
              onRemove={(key) =>
                change(sources.filter((item) => sourceKey(item) !== key))
              }
            >
              <Switch
                aria-label={t('Enable {{resource}}', {
                  resource: label(source),
                })}
                checked={source.enabled}
                disabled={readonly}
                onCheckedChange={(enabled) =>
                  change(
                    sources.map((item) =>
                      sourceKey(item) === sourceKey(source)
                        ? { ...item, enabled }
                        : item
                    )
                  )
                }
              />
              <span className='text-muted-foreground text-xs'>
                {source.kind === 'hosting'
                  ? t('Provider quota, free on Oak')
                  : t('Platform billing applies')}
              </span>
            </AutoGroupOrderItem>
          ))}
        </Reorder.Group>
        {/* 空顺序具有明确的拒绝语义，绝不当作全部可用或自动继承。 */}
        {!sources.some((source) => source.enabled) && (
          <p role='status'>
            {t(
              'All resource sources are disabled. New requests will be rejected.'
            )}
          </p>
        )}
        {sources.some(
          (source) => source.kind === 'wallet' && source.enabled
        ) && (
          <Alert>
            <AlertDescription>
              {t(
                'Wallet is enabled. Falling back to it can charge your wallet at the published model prices.'
              )}
            </AlertDescription>
          </Alert>
        )}
        {/*
         * 策略只对新的请求生效，已有流式请求不被前端强制中断。
         * 保存成功不扩大 Key 自身权限，也不取消付费使用上限。
         * 用户看到的钱包警告与当前本地草稿对应，保存前即可审查。
         * 如果关闭所有来源，拒绝请求是预期配置结果而非服务故障。
         */}
        <p className='text-muted-foreground text-xs'>
          {t(
            'Changes apply to new requests. Token permissions and paid spending limits still apply.'
          )}
        </p>
        <div className='flex flex-wrap gap-2'>
          <Button
            disabled={readonly || !dirty}
            onClick={() => mutation.mutate()}
          >
            {t('Save priority')}
          </Button>
          <Button
            variant='outline'
            disabled={mutation.isPending}
            onClick={() => {
              setSources(props.policy.sources) // 相同版本也要撤销本地未保存的修改。
              setDirty(false) // 撤销后的草稿不应仍提示可保存。
              void client.invalidateQueries({
                queryKey: ['hosting', 'policy', props.tokenId],
              })
            }}
          >
            {t('Reload policy')}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
