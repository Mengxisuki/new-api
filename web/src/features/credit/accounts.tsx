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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useDebounce } from '@/hooks/use-debounce'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { CreditAccountEditor } from './account-editor'
import { getCreditAccounts, type CreditAccountView } from './api'

// 支付网关页管理授权，财务页复用只读授信表并可筛选该用户账单。
// 不在公共组件引入业务逻辑；表格、错误态、输入和弹窗继续复用既有封装。
// 管理员模式嵌入支付网关，财务模式嵌入挂帐管理。
// 二者共享展示，但请求路径和允许的动作明确区分。
// 管理员模式允许添加及编辑授权，财务模式只能查看。
// 财务选择用户后由父页面筛选该用户的实际挂帐记录。
// 组件不替父页面实现权限提升或角色切换。
// 后端仍独立判断每一次读取和保存请求。
// 列表始终以用户编号作为稳定行键。
// 用户名和显示名用于识别，不能作为资金账本唯一键。
// 三个金额列分别是总授信、未结清占用和剩余可用额度。
// 人民币金额不受站点钱包显示单位影响。
// 格式化使用转换后的界面语言码，支持简繁中文切换。
// 金额显示不会参与后端付款或额度扣减计算。
// 状态列表示账户总授权开关，不代表债务是否结清。
// 模块列表示已配置范围，与总开关共同决定可用性。
// 关闭总开关后保留模块配置，便于管理员恢复原范围。
// 未配置用户不会自动拥有一行无限额度记录。
// 列表为空时明确显示尚无挂帐用户。
// 网络错误不能呈现正常空列表，否则管理员可能重复配置。
// 加载期间显示状态文本，不用旧页面数据制造成功印象。
// 刷新操作重取当前筛选与页码，不重置管理员正在查找的范围。
// 搜索采用共享防抖，减少逐字请求。
// 搜索文字变化回到第一页，防止旧页码掩盖匹配用户。
// 服务器负责分页，不在浏览器下载全部授信数据。
// 下一页按钮依据当前总数和每页二十条启用。
// 请求进行中暂停分页，避免连续点击错误跳过页码。
// 新建弹窗的状态与编辑对象明确区分。
// 编辑直接携带当前配置版本，用于服务端并发修改检查。
// 点击编辑不会保存任何东西，也不会恢复用户的授信。
// 添加用户同样需要后续明确保存。
// 弹窗置于真实内容区，兼容 SectionPageLayout 的命名插槽。
// 因此父页面不能把弹窗移到不会渲染的普通直接子节点。
// 按钮类型固定为普通按钮，避免提交支付网关的外层表单。
// 账户保存后的刷新由编辑器失效共享查询键完成。
// 这里不维护另一份占用或账户余额缓存。
// 该页没有删除账户入口，防止丢失历史欠款归属。
// 暂不支持批量授权，单笔编辑更便于核对用户和额度。
// 财务摘要可看到停用账户，便于继续处理旧欠款。
// 只读模式不展示用户搜索选择器，也不调用管理员保存接口。
// 父组件回调只传用户编号，不传可被修改的金额对象。
// 表格复用项目 StaticDataTable 的滚动与空状态能力。
// 列标题与按钮文案在渲染时翻译，不冻结首次语言。
// 列表获取失败后的重试只读取数据，不重放之前的管理操作。
// 用户没有剩余额度时仍可查看历史记录和授信配置。
// 停用操作不会通过此列表删除用户或撤销已完成服务。
export function CreditAccounts(props: {
  admin: boolean
  onSelectUser?: (id: number) => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [page, setPage] = useState(1) // 前端页码与服务器保持一致。
  const [keyword, setKeyword] = useState('') // 搜索仅保留当前页面内存草稿。
  const search = useDebounce(keyword) // 键入搜索不会触发逐字请求。
  const [editing, setEditing] = useState<CreditAccountView | null | undefined>(
    undefined
  )
  // undefined 是关闭，null 是添加，已存在对象是按版本编辑。
  const query = useQuery({
    queryKey: ['credit-accounts', props.admin, page, search], // 管理和财务结果不跨权限路径共用。
    queryFn: () => getCreditAccounts(props.admin, page, search),
  })
  return (
    <div className='space-y-4'>
      <h3 className='text-lg font-medium'>{t('On-account users')}</h3>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Credit is shared across enabled modules. Disabling access keeps existing debts.'
        )}
      </p>
      <div className='flex flex-wrap gap-3'>
        <Input
          className='max-w-sm'
          aria-label={t('Search credit users')}
          placeholder={t('Search credit users')}
          value={keyword}
          onChange={(event) => {
            setKeyword(event.target.value) // 修改搜索只影响读取条件。
            setPage(1) // 搜索更新后回到首批匹配用户。
          }}
        />
        {props.admin && (
          <Button type='button' onClick={() => setEditing(null)}>
            {t('Add user')}
          </Button>
        )}
        <Button
          type='button'
          variant='outline'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          {t('Refresh')}
        </Button>
      </div>
      {/* 错误与空数据分开，失败后提供原查询重试入口。 */}
      {query.isError && <ErrorState onRetry={() => void query.refetch()} />}
      {query.isLoading && <p role='status'>{t('Loading...')}</p>}
      {query.data && !query.isError && (
        <StaticDataTable
          data={query.data.items}
          getRowKey={(row) => row.user_id}
          emptyContent={t('No on-account users')}
          columns={[
            {
              id: 'user', // 用户编号稳定，名称只用于辅助核对。
              header: t('User'),
              cell: (row) =>
                `#${row.user_id} · ${row.username} ${row.display_name}`,
            },
            // 额度、欠款和可用额度并列，不能把总授信当作剩余额度。
            {
              id: 'limit', // 授信上限不是当前可用数。
              header: t('Credit limit (CNY)'),
              cell: (row) => formatNumber(row.limit_cents / 100, locale),
            },
            {
              id: 'used', // 未结清占用不能由表单覆盖。
              header: t('Outstanding credit (CNY)'),
              cell: (row) => formatNumber(row.used_cents / 100, locale),
            },
            {
              id: 'available', // 所有开放模块共用同一剩余额度。
              header: t('Available credit (CNY)'),
              cell: (row) =>
                formatNumber((row.limit_cents - row.used_cents) / 100, locale),
            },
            {
              id: 'enabled', // 停用账户仍保留在列表便于对账。
              header: t('Status'),
              cell: (row) => t(row.enabled ? 'Enabled' : 'Disabled'),
            },
            {
              id: 'modules', // 当前只有代充值实际接入。
              header: t('Enabled modules'),
              cell: (row) => {
                const modules = [
                  row.recharge_enabled ? t('Account recharge') : null,
                  row.topup_enabled ? t('Wallet top-up') : null,
                  row.subscription_enabled ? t('Subscription') : null,
                ].filter(Boolean)
                return modules.length > 0 ? modules.join(', ') : '—'
              },
            },
            {
              id: 'actions', // 管理员编辑和财务查看采用不同动作。
              header: t('Actions'),
              cell: (row) => (
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => {
                    if (props.admin) setEditing(row) // 携带列表快照的并发版本。
                    else props.onSelectUser?.(row.user_id) // 只读选择不会调用授权写入。
                  }}
                >
                  {t(props.admin ? 'Edit' : 'View credit records')}
                </Button>
              ),
            },
          ]}
        />
      )}
      {/* 分页只作用于服务端范围；新搜索回到第一页。 */}
      <div className='flex items-center justify-end gap-3'>
        <span>
          {t('Page {{page}}', { page })} · {query.data?.total ?? 0}
        </span>
        <Button
          type='button'
          variant='outline'
          disabled={page === 1 || query.isFetching}
          onClick={() => setPage(page - 1)}
        >
          {t('Previous')}
        </Button>
        <Button
          type='button'
          variant='outline'
          disabled={page * 20 >= (query.data?.total ?? 0) || query.isFetching}
          onClick={() => setPage(page + 1)}
        >
          {t('Next')}
        </Button>
      </div>
      {/* 弹窗必须位于布局实际渲染的内容区，不能放到被忽略的直接子级。 */}
      {editing !== undefined && (
        <CreditAccountEditor
          account={editing ?? undefined}
          onClose={() => setEditing(undefined)}
        />
      )}
    </div>
  )
}
