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
import {
  useIsMutating,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'

import { refreshHostingObservation } from '../api'

// 两种查询沿用执行层联合观测，一次请求同步额度和重置次数。
// 消费入口只交给已有详情确认流程，查询绝不生成消费 UUID。
// 桌面表格和移动卡片共用组件，查询结果同时更新列表与详情。
// 全局 mutation key 防止响应式双实例在请求期间重复点击。
// 部分观测属于真实结果，需要明确显示目标查询是否成功。
// 快照可能为空或过期，不能用旧次数冒充本次查询结果。
// accountId 只定位当前账号，归属与权限由同源 API 校验。
// disabled 由详情的消费流程传入，查询按钮不能抢在消费完成前刷新。
// canReset 是进入确认流程的前置提示，不能代替后端的消费校验。
// onReset 只打开确认界面，不允许在此直接取得站点证明。
// 列表打开确认后还需读取持久历史，阻止对未决操作重复扣卡。
// 组件挂载不查询原厂，避免列表有多个账号时产生批量网络请求。
// 查询使用 mutation，分页、窗口聚焦及重新挂载都不会隐式刷新。
// 不将目标查询种类加入请求体，后端仍只接受既有观测接口。
// 原厂对额度和卡详情分别观测，联合结果可能只完成其中一部分。
// 无论哪个查询入口，完整快照始终一起写入，避免人为拼接不同时间数据。
// 账号摘要通过失效重新读取，由服务端统一生成，不在本组件拼装。
// 详情缓存直接采用响应，已打开的详情无需等待第二次原厂请求。
// 此处失效只影响账号列表，不触发模型测试或重新读取授权回调。
// 本组件没有持久浏览器存储，刷新页面后仍从服务端读取事实。
// 请求错误不清空已有额度，界面相邻的观测标签负责标明历史时间。
// 手动再次查询使用原账号编号，不因错误自动创建或重新授权账号。
// 两个查询按钮共享忙碌状态，防止同一账号并发重复观测。
// 不同账号使用不同 mutation key，可以各自查询且不互相覆盖。
// 回应到达后才更新缓存，不能预先把进度条或重置次数改成目标值。
// 次数查询只关注卡详情完整性，不把“仍有额度”解释为“有重置次数”。
// 查询按钮不根据账号是否已验证推理而禁用，授权后即可获取额度。
// 后端观测节流保持有效，重复点击不能绕过执行层的频率限制。
export function AccountQuotaActions(props: {
  accountId: number
  disabled?: boolean
  canReset: boolean
  onReset: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const mutationKey = ['hosting', 'quota-refresh', props.accountId]
  const refreshing = useIsMutating({ mutationKey }) > 0
  // 两个按钮保持明确用途，但不另建重复的额度数据源。
  // 无论哪种查询失败，都保留原快照供用户辨认历史状态。
  // 不重试上游错误，用户可以主动再次发起只读查询。
  const refresh = useMutation({
    mutationKey,
    mutationFn: (_target: 'quota' | 'resets') =>
      refreshHostingObservation(props.accountId),
    retry: false,
    onSuccess: (value) => {
      client.setQueryData(['hosting', 'observation', props.accountId], value)
      void client.invalidateQueries({ queryKey: ['hosting', 'accounts'] })
    },
  })
  // 只展示本次响应的完整性，主额度成功不能掩盖次数查询失败。
  // 次数为零也是有效观测；是否可消费由外部统一门槛决定。
  // 网络失败由应用通用错误提示处理，同时留下可见失败状态。
  const result = refresh.data
  const incomplete =
    refresh.isSuccess &&
    (result?.state !== 'fresh' ||
      (refresh.variables === 'resets' &&
        result.snapshot?.reset_credit_observation?.state !== 'fresh'))
  const busy = Boolean(props.disabled) || refreshing
  return (
    <div className='space-y-1'>
      {/* 保持文字按钮，窄屏自然折行；键盘焦点沿用共享 Button。 */}
      <div className='flex flex-wrap gap-1' aria-busy={refreshing}>
        <Button
          size='sm'
          variant='ghost'
          disabled={busy}
          onClick={() => refresh.mutate('quota')}
        >
          {/* 加载图标只标明当前点击的查询，不改变按钮的可访问名称。 */}
          {refresh.isPending && refresh.variables === 'quota' && <Spinner />}
          {t('Refresh quota')}
        </Button>
        <Button
          size='sm'
          variant='ghost'
          disabled={busy}
          onClick={() => refresh.mutate('resets')}
        >
          {/* 次数和额度共用忙碌门禁，但保留各自正在请求的反馈。 */}
          {refresh.isPending && refresh.variables === 'resets' && <Spinner />}
          {t('Fetch reset opportunities')}
        </Button>
        {/* 缺少新鲜次数时保持禁用，不把“未知”当成零或可消费。 */}
        <Button
          size='sm'
          variant='ghost'
          disabled={busy || !props.canReset}
          onClick={props.onReset}
        >
          {t('Use one reset opportunity')}
        </Button>
      </div>
      {/* 不以 HTTP 成功覆盖独立的重置次数观测错误。 */}
      {(refresh.isError || incomplete) && (
        <p role='status' className='text-destructive text-xs'>
          {t('Quota query is incomplete. Please retry.')}
        </p>
      )}
    </div>
  )
}
