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
/**
 * 事件历史展示契约：
 * 这里展示平台已经保存的业务事件，
 * 和某个消费者的投递队列分开。
 * 充值成功、异常扣费、资料提交都先形成持久摘要，
 * 是否已有客户端在线不影响历史可见性。
 * 服务暂停期间事件仍会增加，
 * 不能因为没有投递次数就隐藏事件。
 * 页面查询采用独立页码，
 * 服务端每页五十条，
 * 下一页是否可用按这一公开契约判断。
 * 事件清理后总数可能减少，
 * 因此空页仍保留返回上一页的能力，
 * 不凭空补造历史。
 * 读取失败与没有记录是两种状态，
 * 前者提供重试，
 * 后者展示空态。
 * 不能用异常捕获返回空数组，
 * 否则管理员会把损坏响应误认为没有业务发生。
 * 返回的数据通过接口结构校验后才渲染，
 * 未知字段不会被拿来拼装链接或执行动作。
 * 摘要使用文本形式展示并由框架转义，
 * 不把内容解释成页面标记。
 * 每条事件以稳定标识区分，
 * 展开详情只读取摘要，
 * 不请求代充值敏感资料。
 * 主题名称来自已知映射，
 * 遇到未知主题保留原始名称供定位，
 * 不能冒充其他业务。
 * 时间戳按秒转换，
 * 再使用合法的界面语言格式化；
 * 数字总量使用统一工具。
 * 翻页、展开和后台自动刷新都不会确认事件，
 * 也不会重新执行充值或扣款。
 * 查询缓存包含用户和会话标识，
 * 换账号后不能沿用另一身份的历史列表。
 * 五秒刷新只更新当前页面状态，
 * 不代替客户端持久确认和断线恢复协议。
 */
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import { getEventHistory, topicLabels } from '../api'

/**
 * 平台事件历史与消费者投递记录是两种不同视图。
 * 事件入库后就可查询，尚未投递也应可见。
 * 分页查询保留真实空态和失败态，不能用空数组掩盖错误。
 * 自动刷新只更新服务端数据，不推进任何消费进度。
 */
export function EventHistory() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const userId = useAuthStore((state) => state.auth.user?.id)
  const sessionId = useAuthStore((state) => state.auth.session?.sid)
  const [page, setPage] = useState(1)
  const history = useQuery({
    queryKey: ['event-hub', userId, sessionId, 'events', page],
    queryFn: () => getEventHistory(page),
    refetchInterval: 5000,
  })
  if (history.isPending) return <LoadingState size='sm' />
  if (history.isError) {
    return <ErrorState onRetry={() => void history.refetch()} />
  }
  // 总数取自当前请求，与本页条目属于同一快照。
  // 清理可能使总数缩小，不能据旧计数补造条目。
  // 日期格式化器跟随界面语言创建。
  // 事件摘要只做文本展示，不触发额外业务资料读取。
  // 历史查询无确认操作，展开和翻页都不推进消费进度。
  const total = history.data.total
  const time = new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
  // 表格使用业务事件编号作稳定键。
  // 摘要较长时局部滚动，不改变其他行的操作位置。
  // 上一页按钮只在第一页禁用，便于空页退出。
  // 下一页同时检查总数和当前条目，避免反复请求不存在的页。
  // 页码从一开始，与后端分页契约保持一致。
  // 历史与详情共享缓存前缀，管理变更后一起更新。
  // 查询失败展示重试，不用上一页内容冒充本页。
  // 未知主题保留协议名称，方便排查版本差异。
  // 数字总量统一格式化，编号仍保持稳定的事件身份。
  return (
    <div className='space-y-3'>
      <StaticDataTable
        data={history.data.items}
        getRowKey={(row) => row.id}
        emptyContent={<EmptyState title={t('No recorded events')} />}
        columns={[
          {
            id: 'id',
            header: t('Event'),
            cell: (row) => <span className='font-mono text-xs'>#{row.id}</span>,
          },
          {
            id: 'topic',
            header: t('Event topic'),
            cell: (row) =>
              t(
                topicLabels[row.topic as keyof typeof topicLabels] ?? row.topic
              ),
          },
          {
            id: 'time',
            header: t('Created at'),
            cell: (row) => time.format(row.created_at * 1000),
          },
          {
            id: 'data',
            header: t('Event summary'),
            cell: (row) => (
              <details>
                <summary className='cursor-pointer text-sm'>
                  {t('Details')}
                </summary>
                <pre className='mt-2 max-h-48 max-w-96 overflow-auto text-xs break-all whitespace-pre-wrap'>
                  {JSON.stringify(row.payload, null, 2)}
                </pre>
              </details>
            ),
          },
        ]}
      />
      <div className='flex flex-wrap items-center justify-between gap-3 text-sm'>
        <span className='text-muted-foreground'>
          {t('Recorded events')}: {formatNumber(total, locale)}
        </span>
        <div className='flex items-center gap-3'>
          <Button
            variant='outline'
            size='sm'
            onClick={() => setPage(page - 1)}
            disabled={page <= 1}
          >
            {t('Previous')}
          </Button>
          <span>{formatNumber(page, locale)}</span>
          <Button
            variant='outline'
            size='sm'
            onClick={() => setPage(page + 1)}
            disabled={history.data.items.length === 0 || page * 50 >= total}
          >
            {t('Next')}
          </Button>
        </div>
      </div>
    </div>
  )
}
