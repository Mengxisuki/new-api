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
import { Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Combobox } from '@/components/ui/combobox'
import { cn } from '@/lib/utils'

import { getHostingAccounts, getHostingStatus } from './api'
import { HostingAccountDetails } from './components/account-details'
import { AccountQuota } from './components/account-quota'
import { ConnectHostingDialog } from './components/connect-dialog'
import type { HostingAccount } from './types'

// 列表只请求 Oak 摘要，选择详情后才加载该账号的上游观测与模型目录。
// 工作区是用户管理本人挂靠资源的统一入口。
// 导航进入后先取得账号摘要，不在加载页面时产生原厂耗额。
// 用户可以连接多个账号，并用自己的备注区分用途。
// 邮箱只作脱敏身份辅助，不使用邮箱作为账号主键。
// 同一备注可以对应不同账号，所有操作仍依据稳定编号。
// 列表状态解释连接和真实模型验证的区别。
// 账号已连接不代表所有客户端协议都可用。
// 暂停状态优先显示，避免已验证标签掩盖禁止新请求的事实。
// 详情由当前列表查找对象，缓存刷新后可显示最新暂停状态。
// 列表移除已解绑账号后，不继续保留悬空的详情引用。
// 连接弹窗以新建标记或已有账号对象区分初次授权与重授权。
// 重授权之前关闭详情，避免两个业务弹窗争抢焦点。
// 筛选只决定列表展示，不会改变任何资源策略。
// 筛选出空列表不代表账号已经被删除或暂停。
// 所有账号中的空状态与筛选结果为空需要区别理解。
// 查询失败展示可重试错误，而不是首次接入营销空状态。
// 用户点击列表刷新只同步摘要，不逐个强制刷新原厂额度。
// 钱包回退的费用提示始终可见，避免把挂靠免费推广到所有来源。
// 用户仍使用同一个 Key 和入口地址，来源选择在平台端管理。
// Key 管理链接沿用已有令牌页面，不创建第二套令牌系统。
// 移动端使用卡片展示，保留与桌面相同的管理入口。
// 两种视图共用状态函数和额度组件，避免事实展示分叉。
// 长备注与脱敏身份允许换行，不能把页面撑出横向滚动。
// 表格操作按钮保持明确文字，避免用户靠图标猜测动作。
// 当前仅接入 Codex，不展示未实现供应商的可点击入口。
// 后续供应商扩展需要同步 DTO、授权和协议验证，而非只加菜单。
export function HostingWorkspace() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  // 界面状态与持久业务状态的边界：
  // 筛选值仅决定当前列表内容，不写入服务端。
  // 账号编号选择用于定位详情，不保存凭据。
  // 授权选择保留完整摘要以初始化原备注。
  // 关闭详情后清除选择，重新打开读取最新对象。
  // 路由鉴权由认证父路由负责，不在工作区再实现登录跳转。
  // 账号所有权由每次 API 校验，不能仅凭路由已登录判断。
  // 列表没有全局加载全部模型的副作用。
  // 查看多个账号时只有当前详情读取其额外数据。
  // 原厂窗口的具体展示由 AccountQuota 维护，页面不重复计算。
  // 状态标签由一个函数提供给桌面和移动端。
  // 页面不在客户端对账号可用性进行轮询推理。
  // 用户在其他会话改了账号，重新读取列表后显示服务端事实。
  // 已经删除的账号不继续提供可操作详情。
  // 需要处理错误的账号仍可进入详情，以便重授权或解绑。
  // 首期供应商固定，因此入口写明 Codex，避免让用户误接其他账号。
  // 列表不展示 gpt-load 管理组、凭据类型或内部路由。
  // 普通用户不需要理解后台管理服务才能完成接入。
  // 挂靠页只提供账号操作；来源顺序和客户端配置从具体 Key 菜单进入。
  // 页面不自动把已验证账号设为第一优先级。
  // 同一 Key 使用多账号时优先级仍由用户完整保存。
  // 挂靠免费说明只指平台费用，不承诺原厂无限使用。
  // 钱包付费风险直接显示，不藏在仅管理员可见的设置里。
  // 包月来源会遵守平台额度规则，不借原厂窗口解释人民币余额。
  // 回退发生的真实原因从使用日志查询，不在列表预先承诺。
  // 接口失败保留重试入口，用户不需要通过重新登录排查。
  // 视觉响应式切换不会修改业务状态或重新连接账号。
  // 移动端管理按钮与桌面触发同一个详情组件。
  // 列表项采用稳定 ID，避免备注变化导致错误复用组件。
  // 静态表的 key 同样不使用数组下标。
  // 错误码与状态不会被放入页面标题或浏览器地址。
  // 没有用户账号信息写入全局状态库，缩小敏感信息驻留范围。
  // 账号备注由 React 文本渲染，不允许注入 HTML。
  // 列表内容可为空，但必须等待成功响应才展示首次引导。
  // 只提供产品支持的真实动作，不显示尚未接入的占位按钮。
  // 页面刷新不会自动继续敏感操作，持久恢复由详情显式发起。
  // 用户取消授权后可以重新连接，不保留已取消回调输入。
  // 账号重新授权后仍沿用已有策略中的编号。
  // 当前页面是用户视角的资源工作区，不复制管理员渠道配置。
  const [filter, setFilter] = useState('all') // 筛选只影响当前列表，不改变 Key 的来源配置。
  const [selected, setSelected] = useState<number | null>(null) // 以稳定编号查详情，备注可以重复。
  const [connecting, setConnecting] = useState<HostingAccount | 'new' | null>(
    null
  )
  // 账号列表查询键被详情变更共同失效，备注与暂停会同步回列表。
  // 查询函数已在 API 边界完成数据校验，组件不再次吞错解析。
  // 组件只保留界面选择状态，不把账号摘要复制进持久浏览器存储。
  // 选中账号与授权流程是不同状态，关闭任一不会改变另一资源。
  // 账号创建成功后刷新列表，用户可以看到新的稳定编号对应项。
  const accounts = useQuery({
    queryKey: ['hosting', 'accounts'], // 账号变更失效同一摘要缓存，避免列表状态落后。
    queryFn: getHostingAccounts,
  })
  // 页面进入时先读本地服务就绪状态，避免用户完成敏感验证后才遇到配置错误。
  // 配置查询不创建 OAuth 会话，也不请求原厂额度。
  // 普通用户只看安全错误说明，部署细节与签名材料留在服务器。
  const availability = useQuery({
    queryKey: ['hosting', 'status'], // 就绪状态独立于是否已经存在挂靠账号。
    queryFn: getHostingStatus, // 只读配置摘要，不向模型商发起连接。
  })
  // 读取失败不能沿用上一次成功状态开放授权入口。
  // 这里只检查本地必需配置；实际执行层连通失败仍由授权请求明确报告。
  const canConnect =
    availability.data?.configured === true && !availability.isError
  // 即使账号列表为空，也必须区别“尚未添加”和“服务尚未配置”。
  // 重新授权复用相同门禁，不通过已有账号绕过就绪检查。
  const unavailableMessages: Record<string, string> = {
    hosting_invalid_control_url: t(
      'Account hosting control service URL is invalid. Please contact an administrator.'
    ),
    hosting_encryption_not_configured: t(
      'Account hosting encryption is not configured. Please contact an administrator.'
    ),
  }
  // 错误只说明配置类别，不把部署地址、签名材料或原厂响应传给用户。
  let disabledReason: string | undefined
  if (!canConnect) {
    disabledReason =
      unavailableMessages[availability.data?.code ?? ''] ??
      t('Account hosting is not configured. Please contact an administrator.')
    if (availability.isPending) {
      disabledReason = t('Checking account hosting availability...')
    } else if (availability.isError) {
      disabledReason = t(
        'Unable to check account hosting availability. Please retry.'
      )
    }
  }
  const data = accounts.data ?? [] // 等待期的空值不能触发首次接入提示。
  const active = data.find((account) => account.id === selected) // 已解绑账号自动退出详情。
  const filtered = data.filter(
    (account) =>
      filter === 'all' ||
      (filter === 'paused' ? account.paused : !account.paused)
  )
  // 生命周期状态来自受校验枚举，不能把任意坏字符串当成正常状态。
  // connected 指已经绑定，active 指至少一次模型调用经过真实验证。
  // error 需要用户进入详情查看脱敏错误，不能自动替其重新授权。
  // deleting 仍可能有异步清理工作，因此不显示删除完成。
  // 暂停开关独立于生命周期，恢复后仍要尊重原来的验证状态。
  // 页面标签描述当前摘要，不保证后续调用一定成功。
  const status = (account: HostingAccount) => {
    if (account.paused) return t('Paused') // 暂停优先显示，不让旧测试成功掩盖停用状态。
    if (account.status === 'connected') {
      return t('Connected, awaiting verification') // OAuth 成功尚不等于模型调用成功。
    }
    if (account.status === 'active') return t('Model call verified')
    if (account.status === 'error') return t('Account needs attention')
    if (account.status === 'deleting') return t('Removing account')
    return t('Connecting account')
  }
  // 只有真实验证通过且未暂停才使用可用色，授权成功不能伪装可调用。
  // 异常、暂停仍保留文字标签，颜色只是辅助，不替代状态语义。
  // 桌面和手机复用同一徽标，避免响应式切换时出现不同结论。
  const statusBadge = (account: HostingAccount) => (
    <Badge
      variant={account.status === 'error' ? 'destructive' : 'secondary'}
      className={cn(
        'h-auto min-h-5 max-w-full whitespace-normal',
        account.status === 'active' &&
          !account.paused &&
          'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400'
      )}
    >
      {status(account)}
    </Badge>
  )
  const quota = (account: HostingAccount) => (
    <AccountQuota observation={account.observation} />
  )
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Account hosting')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          disabled={!canConnect}
          onClick={() => setConnecting('new')} // 所有新建入口进入同一个授权状态机。
        >
          {t('Connect Codex account')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-6'>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Manage your connected accounts and provider quotas here. Choose which account, subscription or wallet each API key uses from the API Keys menu.'
            )}
          </p>
          {/* 服务不可用时仍能管理已有账号；只有新授权入口受就绪状态限制。 */}
          {disabledReason && (
            <Alert role='status'>
              <AlertDescription>{disabledReason}</AlertDescription>
              {!availability.isPending && (
                <Button
                  variant='outline'
                  size='sm'
                  disabled={availability.isFetching}
                  onClick={() => {
                    void availability.refetch() // 服务恢复后原入口可直接继续，不要求重复登录。
                  }}
                >
                  {t('Retry availability check')}
                </Button>
              )}
            </Alert>
          )}
          <div className='flex flex-wrap items-center gap-2'>
            <Combobox
              options={[
                { value: 'all', label: t('All accounts') },
                { value: 'active', label: t('Unpaused accounts') },
                { value: 'paused', label: t('Paused accounts') },
              ]}
              value={filter}
              onValueChange={(value) => setFilter(value ?? 'all')}
            />
            <Button
              variant='outline'
              disabled={accounts.isFetching}
              onClick={() => {
                void accounts.refetch() // 只更新摘要，不替用户触发原厂刷新或重置。
              }}
            >
              {t('Refresh account list')}
            </Button>
            <Button
              variant='outline'
              render={<Link to='/keys' />}
              nativeButton={false}
            >
              {t('Manage API keys')}
            </Button>
          </div>
          {/*
           * 加载、失败、真实空列表分别有不同入口。
           * 错误重试调用同一查询，不触发授权或新增账号。
           * 仅成功响应的空列表才展示首次连接引导。
           * 已有账号但筛选结果为空，不应误导用户重复授权。
           */}
          {accounts.isPending && <LoadingState />}
          {accounts.isError && (
            <ErrorState
              onRetry={() => {
                void accounts.refetch() // 只更新摘要，不替用户触发原厂刷新或重置。
              }}
            />
          )}
          {accounts.isSuccess && !data.length && (
            <EmptyState
              title={t('Connect your first Codex account')}
              description={t(
                'Add your ChatGPT account, test a model connection, then select this account in an API key’s usage order.'
              )}
              action={
                <Button
                  disabled={!canConnect}
                  onClick={() => setConnecting('new')} // 所有新建入口进入同一个授权状态机。
                >
                  {t('Connect Codex account')}
                </Button>
              }
            />
          )}
          {Boolean(data.length) && (
            <>
              {/* 桌面沿用静态表封装，移动端折成账号卡，长身份不撑宽页面。 */}
              <div className='hidden md:block'>
                <StaticDataTable
                  // 固定列宽把空间留给窗口摘要，长备注不会挤压额度区。
                  // 单元格顶部对齐，短操作不再漂浮在高行中央。
                  // 小屏继续使用下方卡片，不把桌面表格缩成横向滚动。
                  tableClassName='table-fixed [&_td]:px-4 [&_td]:py-4 [&_td]:align-top [&_th]:px-4'
                  headerRowClassName='bg-muted/40 hover:bg-muted/40'
                  data={filtered}
                  getRowKey={(account) => account.id} // 稳定身份使备注修改不会重建错误行。
                  columns={[
                    {
                      id: 'account',
                      className: 'w-[23%]',
                      header: t('Account'),
                      cell: (account) => (
                        <div className='space-y-1 break-words whitespace-normal'>
                          <p className='font-medium'>{account.alias}</p>
                          <p className='text-muted-foreground text-xs'>
                            {account.email_mask}
                          </p>
                        </div>
                      ),
                    },
                    {
                      id: 'status',
                      className: 'w-[19%]',
                      header: t('Status'),
                      cell: statusBadge,
                    },
                    { id: 'quota', header: t('Provider quota'), cell: quota },
                    {
                      id: 'actions',
                      className: 'w-28 text-right',
                      cellClassName: 'text-right',
                      header: t('Actions'),
                      cell: (account) => (
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => setSelected(account.id)} // 以账号编号打开同一份详情。
                        >
                          {t('Manage account')}
                        </Button>
                      ),
                    },
                  ]}
                />
              </div>
              <div className='grid gap-3 md:hidden'>
                {filtered.map((account) => (
                  <Card key={account.id}>
                    <CardHeader>
                      <CardTitle className='break-words'>
                        {account.alias}
                      </CardTitle>
                      <p className='text-muted-foreground text-xs break-all'>
                        {account.email_mask}
                      </p>
                      {statusBadge(account)}
                    </CardHeader>
                    <CardContent className='space-y-3'>
                      {quota(account)}
                      <Button
                        variant='outline'
                        onClick={() => setSelected(account.id)} // 以账号编号打开同一份详情。
                      >
                        {t('Manage account')}
                      </Button>
                    </CardContent>
                  </Card>
                ))}
              </div>
            </>
          )}
        </div>
        {/* 以账号 ID 重新挂载详情，防止分页或选择变化复用前一账号的操作状态。 */}
        {active && (
          <HostingAccountDetails
            key={active.id}
            account={active}
            onClose={() => setSelected(null)}
            onReauthorize={() => {
              setSelected(null) // 重授权只保留一个弹窗，避免焦点被账号详情遮挡。
              setConnecting(active)
            }}
          />
        )}
        {connecting && (
          <ConnectHostingDialog
            account={connecting === 'new' ? undefined : connecting}
            disabledReason={disabledReason} // 弹窗保持打开时也同步服务配置门禁。
            onManageKeys={() => {
              setConnecting(null)
              void navigate({ to: '/keys' }) // 成功后进入唯一的 Key 配置入口。
            }}
            onClose={() => setConnecting(null)}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
