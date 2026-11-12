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
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  StaticRowActions,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

import { formatShanghaiDay } from '../lib/range'
import type { SubscriptionPeriod } from '../subscriptions-api'

// 图形和文字共享同一行数据，避免图表排序与操作对象错位。
// 本组件只渲染传入的成功查询结果，不负责网络请求与错误恢复。
// start 与 end 必须是已验证的非空区间，父级在无效输入时不挂载。
// 所有行共用同一时间窗口，不按各自周期单独缩放条形。
// 这样可以直接比较用户权益覆盖与号池资源覆盖的重叠关系。
// 传入 items 已由服务端排序，组件不重新排序导致分页顺序改变。
// 未知名称仍有稳定的套餐编号可读，不影响时间条绘制。
// 用户编号用来区分同名用户，用户名变化不会改变行的唯一键。
// 人工号池不显示虚构的用户零号，也不会生成用户管理链接。
// 父级 canManage 只用于界面裁剪，不是后端权限验证的替代。
// 编辑和删除回调传完整记录，由宿主决定打开哪一个弹窗。
// 用户订阅即使在 Root 页面上也不出现人工配置操作。
// 名称和周期位于同一语义化表格，读屏器能关联标题与内容。
// 每条图形带文本等价信息，无需依赖 Canvas 或悬浮提示读取日期。
// 日期文字保留实际合同范围，图形才裁切到当前查询窗口。
// 结束日按排他边界减一秒展示，防止多显示一天。
// 同一自然日内的短订阅也至少有一像素条形，仍保留可读日期。
// 图形按秒线性缩放，日期标签并不改变条形的实际时间位置。
// 起始点在窗口之前时左侧贴边，不能把负百分比写入容器。
// 结束点在窗口之后时右侧贴边，不产生超出表格的长条。
// 查询接口只返回相交记录，零宽保护仅处理边界渲染。
// 未开始状态优先显示真实时间，不能把数据库默认 active 当已生效。
// 结束时刻已到则标记过期，避免等待后台任务更新状态。
// 取消状态覆盖时间推导结果，已经取消的合同不能显示生效中。
// 取消不改变原始时间范围，系统没有记录可用来重建精确取消日。
// 颜色区分来源，不用颜色单独表达状态或读写权限。
// 深色和浅色使用不同色阶，保证条形在卡片背景可辨。
// 取消条减弱透明度，同时保留明确的文字状态。
// 今天的位置使用当前时刻而非每天零点，更准确显示日内进度。
// 标线只在窗口内出现，历史月份不会把今天挤在边缘。
// 标线采用前景色虚线，不与任一来源的实心条混淆。
// 网格与刻度都按四等份计算，视图跨度变化后仍保持一致。
// 刻度省略年份以减少密度，完整年份由筛选与每行日期提供。
// 刻度末端减一秒，显示选择的最后一天而不是次日。
// 全年的窗口不绘制逐日三百多条网格，避免遮挡短周期。
// 时间轴最小宽度保障日期和状态可读，窄屏使用容器横向滚动。
// 名称允许换行，不把长名称直接扩大成无限宽的第一列。
// 编辑动作复用 StaticRowActions，删除操作保持二级菜单语义。
// 复用 StaticDataTable 的空态、边框和滚动布局，避免引入另一套表格。
// 组件自身订阅语言，独立使用时也能随切换重新计算标签。
// 这里没有到期执行逻辑，图形更新时间不会触发账户停用。
export function SubscriptionTimelineTable(props: {
  items: SubscriptionPeriod[]
  start: number
  end: number
  canManage: boolean
  onEdit: (row: SubscriptionPeriod) => void
  onDelete: (row: SubscriptionPeriod) => void
}) {
  const { t } = useTranslation()
  const { start, end } = props
  // 状态判断使用服务器传回的时间秒数，与浏览器毫秒时钟明确换算。
  // 这个时刻在一次渲染内共用，避免边界附近不同行出现不一致状态。
  // 刷新或切换筛选会重算状态，不单独建立常驻轮询计时器。
  // 图形不是交易确认依据，显示到期状态不会写回数据库。
  // 窗口长度由父级保证为正，比例计算不需要隐藏的零除回退。
  // 计算使用真实秒数，闰年月份不同天数自然反映到图形长度。
  // 跨上海零点时不借用浏览器本地年月日，日期展示函数保持统一。
  // 当前行标识不含名称，编辑名称不会让组件误认为另一条合同。
  // 取消记录仍保留身份和完整时间，方便管理者对照历史安排。
  // 没有对应付款订单也可显示管理员开通的真实用户订阅。
  // 空白套餐名称只影响标签，不修改其原有计划编号。
  // 时间尺的轴标签是展示辅助，筛选输入始终保留完整起止日期。
  // 条形容器自身隐藏越界部分，最小像素宽度不会突破表格边界。
  const now = Date.now() / 1000
  const todayPosition = ((now - start) / (end - start)) * 100
  // 列定义在渲染时生成，因为状态文案与当前时间都可能改变。
  // 把列固化为模块常量会让语言切换失效，也会冻结过期判断。
  // 数据量由服务端页长限制，无需为二十条记录引入虚拟滚动。
  // 每行的两个来源标签与筛选保持相同词条，避免用户误解分类。
  // 条形内不再重复绘制名称，短周期仍能完整读取旁边的身份信息。
  // 表格行默认没有点击跳转，避免用户点击时间条意外进入管理页面。
  // 渲染不修改原始 items，切换图形状态不会影响查询缓存。
  const columns: StaticDataTableColumn<SubscriptionPeriod>[] = [
    {
      id: 'name',
      header: t('Subscription'),
      className: 'w-60',
      cell: (row) => (
        <div className='w-52 break-words whitespace-normal'>
          {/* 关联名称被删除时仍可凭套餐编号定位，不能丢掉真实历史周期。 */}
          <div className='font-medium'>
            {row.name || `${t('Subscription plan')} #${row.plan_id}`}
          </div>
          <div className='text-muted-foreground mt-1 text-xs'>
            {row.category === 'user'
              ? `${t('Platform user subscriptions')} · #${row.user_id} ${row.username}`
              : t('Platform account pool')}
          </div>
        </div>
      ),
    },
    {
      id: 'timeline',
      header: (
        // 五个等距刻度适应短期和全年窗口；最末刻度显示所选结束日。
        <div className='flex min-w-96 justify-between text-xs tabular-nums'>
          {[0, 1, 2, 3, 4].map((i) => (
            <span key={i}>
              {formatShanghaiDay(start + ((end - start - 1) * i) / 4).slice(5)}
            </span>
          ))}
        </div>
      ),
      cell: (row) => {
        const left = Math.max(row.start_time, start)
        const right = Math.min(row.end_time, end)
        // 取消状态优先于日期；到期记录不能继续显示“生效中”。
        let status = t('Active')
        if (row.start_time > now) status = t('Upcoming')
        if (row.end_time <= now) status = t('Expired')
        if (row.status === 'cancelled') status = t('Cancelled')
        const datesLabel = `${formatShanghaiDay(row.start_time)} → ${formatShanghaiDay(row.end_time - 1)}`
        return (
          <div className='min-w-96 py-1'>
            {/* 文本是时间条的等价读法；颜色只区分来源，不承担唯一语义。 */}
            <div className='mb-2 flex items-center justify-between gap-3 text-xs'>
              <span className='tabular-nums'>{datesLabel}</span>
              <Badge variant='outline'>{status}</Badge>
            </div>
            <div
              role='img'
              aria-label={`${row.name}: ${datesLabel}, ${status}`}
              className='bg-muted relative h-5 overflow-hidden rounded'
            >
              {/* 等距网格与表头共用比例；今天标线只在所选窗口内出现。 */}
              {[25, 50, 75].map((x) => (
                <span
                  key={x}
                  className='border-background absolute inset-y-0 border-l'
                  style={{ left: `${x}%` }}
                />
              ))}
              <div
                className={cn(
                  'absolute inset-y-0 min-w-px rounded',
                  row.category === 'pool'
                    ? 'bg-amber-600 dark:bg-amber-400'
                    : 'bg-sky-600 dark:bg-sky-400',
                  row.status === 'cancelled' && 'opacity-40'
                )}
                style={{
                  left: `${((left - start) / (end - start)) * 100}%`,
                  width: `${(Math.max(0, right - left) / (end - start)) * 100}%`,
                }}
              />
              {todayPosition >= 0 && todayPosition < 100 && (
                <span
                  title={t('Today')}
                  className='border-foreground absolute inset-y-0 border-l-2 border-dashed'
                  style={{ left: `${todayPosition}%` }}
                />
              )}
            </div>
          </div>
        )
      },
    },
  ]
  // 只读访问者没有人工配置菜单，服务端仍独立限制三个写接口。
  if (props.canManage) {
    columns.push({
      id: 'actions',
      header: t('Actions'),
      className: 'w-20',
      cell: (row) =>
        row.category === 'pool' && (
          <StaticRowActions
            editLabel={t('Edit pool subscription')}
            deleteLabel={t('Delete')}
            menuLabel={t('Actions')}
            onEdit={() => props.onEdit(row)}
            onDelete={() => {
              props.onDelete(row)
            }}
          />
        ),
    })
  }
  return (
    <StaticDataTable
      columns={columns}
      data={props.items}
      getRowKey={(row) => `${row.category}-${row.id}`}
      tableProps={{ 'aria-label': t('Subscription timeline') }}
      emptyContent={t('No subscriptions overlap the selected dates.')}
    />
  )
}
