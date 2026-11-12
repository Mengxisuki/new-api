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
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTablePagination } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { PanelWrapper } from '@/features/dashboard/components/ui/panel-wrapper'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { formatShanghaiDay, MAX_RANGE_DAYS } from '../lib/range'
import {
  deletePoolSubscription,
  getSubscriptionPeriods,
  subscriptionDay,
  type SubscriptionCategory,
  type SubscriptionPeriod,
} from '../subscriptions-api'
import { PoolSubscriptionDialog } from './pool-subscription-dialog'
import { SubscriptionTimelineTable } from './subscription-timeline-table'

// 每页固定二十条，与服务端保持一致；公共分页组件负责键盘和禁用状态。
const EMPTY: SubscriptionPeriod[] = []
const TABLE_COLUMNS = [{ accessorKey: 'id' }]

// 使用同一条时间尺对齐全部记录，表格同时提供真实起止日期及行操作。
// 现有 Canvas 收入图无法容纳逐行编辑与可访问日期，故在公共表格内绘制时间条。
// 条的位置仅表达相交部分；完整日期仍显示，跨月订阅不会被误认为本月才开始。
// 本组件管理筛选、分页和人工号池操作，时间条绘制交给专用表格。
// 父页面先校验 finance_overview:read 后才挂载，未授权时不会发请求。
// Root 身份仅决定人工配置入口，读取仍遵循整个总览的授权。
// 不同于收入图，该窗口包含未来周期，因此默认选择完整月份。
// 窗口只影响展示，不修改任何订阅的真实起止时间。
// 用户订阅来自本地购买或管理员开通记录，不查询原厂账单。
// 号池来自手工配置表，不把公共渠道的创建时间推算为订阅开始。
// 用户已明确排除挂靠账号，本组件没有第三类空白占位筛选。
// 全部表示两个来源的合并查询，不是在浏览器合并两份分页。
// 两类独立编号可能相同，行身份由下层表格使用联合键维护。
// 列表服务器分页，客户端不下载整个平台的全部订阅。
// 查询范围最长三百六十六天，长合同可切换年份分段查看。
// 开始和结束日期在 UI 中都是包含当天，接口右边界则是次日零点。
// 筛选输入保持字符串，未填完整时可以暂停查询而不抛日期异常。
// 默认月份只在挂载时确定，不会随着重渲染重置用户选择。
// 当前时间变化不会自动把用户正在编辑的查询切换到新月份。
// 操作员可以主动刷新，以获取其他管理员维护的最新周期。
// 刷新只针对当前周期查询，顶部收入刷新保持原行为。
// 查询键包含每个服务端筛选项，避免页码或分类之间共用错误结果。
// 页码使用 TanStack 的零起始索引，仅在 API 调用前加一。
// 日期变化重置页码，防止短结果沿用旧筛选中的末页。
// 更换分类也重置页码，不用空列表误导用户认为该分类没有数据。
// 加载时使用共享 LoadingState，不保留不属于新条件的旧条形。
// 接口失败使用共享 ErrorState，重试保持当前输入和分类。
// 手动输入无效日期属于本地校验，不触发无意义的服务器错误。
// 错误态不显示成功空表格，二者的业务含义不同。
// 总数取服务端过滤后的 total，而非当前页数组长度。
// 公共分页使用 compact 模式，固定页长与后端一致。
// 不开放页长切换，因为当前接口没有该参数。
// React Table 仅承载分页状态，视觉行仍复用已有静态表格。
// 模块级空数组防止无数据时不断替换表格 data 引用。
// 单个组件只会打开一份编辑表单，不允许多份草稿共享提交状态。
// 删除对象保存完整行，使确认文字可以明确显示记录名称。
// 打开新的删除确认前清除旧错误，避免上一条错误污染下一条。
// 取消删除只清理选中对象，不先发请求再尝试撤销。
// 删除期间保留确认框，完成后刷新服务端真实结果。
// 写入失败不会清空图表缓存，用户仍可理解操作前的上下文。
// 图表数据重取失败时给出错误反馈，不悄悄退回旧数据。
// 该面板复用现有 PanelWrapper，空数据时也保留新增和筛选入口。
// 不能使用 PanelWrapper 的 empty 分支，否则号池首次配置入口会消失。
// 英文标签通过 t 动态翻译，不把语言选择写入数据请求。
// 表格滚动与页面滚动分离，时间轴的最小宽度不能撑开整页。
// 操作和筛选按钮保留共享组件的焦点样式及键盘行为。
// 确认弹窗复用通用危险操作布局，不自行管理焦点恢复。
// 当前没有自动续订、通知或到期停用功能，时间图只用于经营查看。
export function SubscriptionGantt() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const canManage = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  const [category, setCategory] = useState<SubscriptionCategory>('all')
  // 默认完整上海自然月，区别于收入统计的“本月至今”，直接看见月底到期项。
  const [dates, setDates] = useState(() => {
    const month = formatShanghaiDay(Date.now() / 1000).slice(0, 7)
    const first = new Date(`${month}-01T00:00:00Z`)
    const last = new Date(
      Date.UTC(first.getUTCFullYear(), first.getUTCMonth() + 1, 0)
    )
    return { from: `${month}-01`, to: last.toISOString().slice(0, 10) }
  })
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  // undefined 表示关闭，null 表示新增；已有记录对象表示编辑。
  const [editing, setEditing] = useState<
    SubscriptionPeriod | null | undefined
  >()
  const [deleting, setDeleting] = useState<SubscriptionPeriod | null>(null)
  const start = subscriptionDay(dates.from)
  const end = subscriptionDay(dates.to) + 86400
  const valid =
    start > 0 && end > start && end - start <= MAX_RANGE_DAYS * 86400
  // 不保留不同筛选的旧行，避免新时间尺绘制旧数据而误导读数。
  // 所有请求都由 QueryClient 管理，组件卸载无需手写异步状态开关。
  // 无效窗口对应的 query 被禁用，之前的成功数据也不会被渲染。
  // 手工刷新按钮同样检查 valid，不能绕过 enabled 发出坏日期。
  // 服务端再次校验日期与分类，前端输入约束不构成安全边界。
  // 页面每次挂载重新取得真实会话的能力，未在本地持久化写权限。
  // 这份查询不依赖收入接口成功，因此收入故障不阻断周期维护。
  // 未配置号池时返回空结果，不能自动构造示例或占位条形。
  const query = useQuery({
    queryKey: [
      'finance-subscriptions',
      start,
      end,
      category,
      pagination.pageIndex,
    ],
    queryFn: () =>
      getSubscriptionPeriods(start, end, category, pagination.pageIndex + 1),
    enabled: valid,
  })
  const items = query.data?.items ?? EMPTY
  // 手动分页模式关闭客户端二次分页，否则第二页可能被再次截成空数组。
  // rowCount 是所有匹配行的总量，公共分页据此判断下一页是否可用。
  // 筛选改变后旧查询仍在缓存中，但不会覆盖新筛选的独立查询键。
  // 表格不启用选择或批量动作，避免引入当前需求之外的操作语义。
  // 列配置只提供分页状态所需的身份字段，不重复维护视觉列。
  // 视觉列由 SubscriptionTimelineTable 管理，可访问标题在那里统一生成。
  // 分页组件只在成功结果下出现，不展示失败请求的陈旧总数。
  // 无数据时仍传空数组，表格实例的 data 类型保持稳定。
  // 查询返回总数为零时，公共控件禁用前后页，不允许负页码。
  // 数据删除可能改变排序和总数，成功后重查是唯一可信来源。
  // 没有在客户端按名称过滤，因为这会让服务端总数与可见行不一致。
  // 日期输入改动可能产生中间无效状态，分页重置不依赖查询成功。
  // 每次手动刷新保留当前页，便于核对其他人刚编辑的记录。
  // 号池新增不会擅自切换分类，避免打断用户原本查看平台用户的上下文。
  // 如果当前只看用户订阅，新号池可通过全部或号池筛选查看。
  // 界面始终显示当前分类的选中状态，空列表也保留该上下文。
  // 一次只选一个分类，全部作为单独选项而非两个多选值的隐式组合。
  // 清空分类的动作被忽略，避免前端无类别而后端默认全部。
  // 分页不参与收入汇总请求，收入数字不随二十条列表换页而改变。
  // 完整历史周期不会在此裁剪，日期裁剪只发生在图形层。
  const table = useReactTable({
    data: items,
    columns: TABLE_COLUMNS,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    rowCount: query.data?.total ?? 0,
    state: { pagination },
    onPaginationChange: setPagination,
  })
  // 删除后回第一页，避免最后一页只剩一行时停留在空页。
  // 删除失败保留确认状态，由公共 QueryClient 报告服务端原因。
  // 删除成功后第一页一定存在，即使最后一条记录刚被移除。
  // 不以本地数组过滤代替数据库刷新，确保 total 与行列表一起更新。
  // 后端删除只作用于人工表，真实用户订阅无对应的删除动作。
  // 确认文本明确操作对象，不能把行标题截断后再用于确认。
  // 失败时记录对象保持不变，重试仍然提交同一个编号。
  // 成功失效所有分类缓存，避免切到全部时又看到已删记录。
  // 删除操作不会使收入查询失效，因为周期表不参与财务合计。
  // 通用确认组件负责防止等待期间重复点击确认按钮。
  const remove = useMutation({
    mutationFn: deletePoolSubscription,
    onSuccess: async () => {
      setDeleting(null)
      setPagination({ pageIndex: 0, pageSize: 20 })
      await client.invalidateQueries({ queryKey: ['finance-subscriptions'] })
    },
  })
  return (
    <PanelWrapper
      title={t('Subscription timeline')}
      description={t(
        'Subscription periods by Asia/Shanghai date. The dashed line marks today; dates are independent of revenue filters.'
      )}
      headerActions={
        canManage && (
          <Button size='sm' onClick={() => setEditing(null)}>
            {t('Add pool subscription')}
          </Button>
        )
      }
    >
      {/* 筛选只影响周期视图，切换后回第一页，其他财务指标保持原时段。 */}
      <div className='mb-4 flex flex-wrap items-end gap-3'>
        <ToggleGroup
          value={[category]}
          onValueChange={(values) => {
            if (!values[0]) return
            setCategory(values[0] as SubscriptionCategory)
            setPagination({ pageIndex: 0, pageSize: 20 })
          }}
          aria-label={t('Subscription category')}
        >
          <ToggleGroupItem value='all'>{t('All')}</ToggleGroupItem>
          <ToggleGroupItem value='user'>
            {t('Platform user subscriptions')}
          </ToggleGroupItem>
          <ToggleGroupItem value='pool'>
            {t('Platform account pool')}
          </ToggleGroupItem>
        </ToggleGroup>
        {/* 原生 date 语义由已有 Input 承载，支持未来日期和键盘输入。 */}
        {(['from', 'to'] as const).map((field) => (
          <div key={field} className='space-y-1'>
            <Label htmlFor={`timeline-${field}`}>
              {t(
                field === 'from' ? 'Timeline start date' : 'Timeline end date'
              )}
            </Label>
            <Input
              id={`timeline-${field}`}
              className='w-40'
              type='date'
              max='9999-12-31'
              value={dates[field]}
              onChange={(event) => {
                setDates({ ...dates, [field]: event.target.value })
                setPagination({ pageIndex: 0, pageSize: 20 })
              }}
            />
          </div>
        ))}
        <Button
          variant='outline'
          disabled={!valid || query.isFetching}
          onClick={() => void query.refetch()}
        >
          {t('Refresh timeline')}
        </Button>
      </div>
      {/* 无效日期时隐藏整个表格，包括时间尺，避免格式化无效日期。 */}
      {!valid && (
        <p role='alert' className='text-destructive text-sm'>
          {t('Select valid dates spanning no more than {{days}} days.', {
            days: MAX_RANGE_DAYS,
          })}
        </p>
      )}
      {valid && query.isLoading && <LoadingState />}
      {valid && query.isError && (
        <ErrorState onRetry={() => void query.refetch()} />
      )}
      {valid && query.data && !query.isError && (
        <div className='space-y-3'>
          {/* 横向滚动限制在公共表格容器，手机页面本身不被宽时间轴撑开。 */}
          <SubscriptionTimelineTable
            items={items}
            start={start}
            end={end}
            canManage={canManage}
            onEdit={setEditing}
            onDelete={(row) => {
              remove.reset()
              setDeleting(row)
            }}
          />
          <DataTablePagination table={table} compact />
        </div>
      )}
      {/* 条件挂载确保新增和编辑草稿隔离，关闭后释放表单。 */}
      {editing !== undefined && (
        <PoolSubscriptionDialog
          period={editing}
          onClose={() => setEditing(undefined)}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => {
          if (!open && !remove.isPending) setDeleting(null)
        }}
        title={t('Delete pool subscription')}
        desc={t(
          'Delete the manual period for {{name}}? Account access and revenue will not change.',
          { name: deleting?.name }
        )}
        destructive
        confirmText={t('Delete')}
        isLoading={remove.isPending}
        handleConfirm={() => {
          if (deleting) remove.mutate(deleting.id)
        }}
      >
        {remove.isError && (
          <p role='alert' className='text-destructive text-sm'>
            {t('Delete failed. Please retry.')}
          </p>
        )}
      </ConfirmDialog>
    </PanelWrapper>
  )
}
