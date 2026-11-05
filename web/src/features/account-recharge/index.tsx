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
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { toIntlLocale } from '@/i18n/languages'
import { hasPermission } from '@/lib/admin-permissions'
import { formatNumber } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { getOrders, getProducts, type Product } from './api'
import { RechargeCheckout } from './checkout'
import { RechargeOrderDetail } from './order-detail'
import { RechargeProductInfo } from './product-info'
import { orderStatus, rechargePaymentLabel } from './status'

// 商品与本人历史放在同一页面，
// 付款后无需到其他模块寻找订单。
// 管理模式只加载运营队列，
// 不会额外读取用户私人购买记录。
// 服务端按权限过滤操作，
// 页面能力校验只负责准确呈现入口。
// 个人页面把商品选择和历史订单放在同一个上下文，
// 便于继续未完操作。
// 运营页面采用独立路由，
// 不能通过匿名参数获得后台数据。
// 权限不足时不渲染缓存订单，
// 也不发送新的列表查询。
// 后台默认进入待领取队列，
// 切换我的待办时清除旧状态筛选。
// 普通用户不需要理解领取与处理的差别，
// 两者统一显示订单处理中。
// 订单号可直接打开详情，
// 也保留明确操作按钮方便移动设备点击。
// 交易时间是服务器创建时间，
// 格式化使用规范化后的界面语言。
// 列表只能显示脱敏邮箱，
// 查看完整账户信息需要进入受保护详情。
// 金额以 CNY标记，
// 禁止把这类外部账户商品解释成TOKEN 额度。
// 翻页请求访问服务器，
// 不在前端持有全量订单或账户资料。
// 输入订单号后从第一页开始，
// 避免旧页码导致误认为没有记录。
// 加载中、请求失败和正常空列表分别显示，
// 接口故障不能伪装为无订单。
// 后台列表可见不代表可操作，
// 每笔订单的按钮还由处理人和状态决定。
// 商品卡片展示真实启用商品，
// 不为演示填入虚构套餐或售价。
// 商品余额开关只影响购买方式，
// 用户已经付款的历史方式保持不变。
// 结账弹层创建成功后直接打开该订单，
// 避免用户再次选择商品下单。
// 详情按订单编号重新挂载，
// 前一单的表单及凭据状态不会流入下一单。
// 所有弹层必须放在页面 Content插槽内，
// 布局不会渲染未知直接子节点。
// 订单查询刷新不会触发支付、领取或外部供应商动作。
// 刷新按钮和状态轮询只读取服务器持久结果，
// 不能本地猜测到账。
// 商品与订单两份查询独立，
// 商品配置不可用时仍能查询已有购买。
// 页面不提供供应商状态字段，
// 用户只需关注平台办理结果。
// 订单详情中的付款窗口不影响用户访问历史记录或联系客服。
// 统计总数与分页来自同一查询，
// 不能拼接不同筛选结果。
// 页面卸载后弹层内存自然销毁，
// 用户回来重新读取安全摘要。
// 无订单时提供明确空状态，
// 不能在后台伪造已处理示例。
// 管理员也可作为普通买家购买，
// 个人订单入口不显示全站业务数据。
// 首期不提供账户资料导出，
// 复制行为仅存在单笔敏感详情中。
// 用户页面只从上架商品开始购买，
// 未上架内容不会成为可选套餐。
// 商品卡片的余额提示由该商品开关决定，
// 不能沿用上一次选购状态。
// 后台当前没有队列记录时显示空状态，
// 不把历史已完成订单当待办。
// 状态筛选是数据库条件，
// 不根据中文标签或浏览器语言发给后端。
// 我的待办使用会话中的处理人，
// 前端不接受任意用户 ID 作为本人。
// 订单号搜索按完整编号匹配，
// 避免通过邮箱模糊搜索枚举购买者。
// 移动端通过公共表格容器水平查看长编号，
// 不撑破整个页面布局。
// 商品卡片在小屏按单列展示，
// 支付按钮不依赖鼠标悬停才能出现。
// 金额与订单状态各自成列，
// 便于运营核对付款方式及办理进度。
// 脱敏邮箱是识别提示，
// 不能代替资料提交时间或有效性验证。
// 详情按钮始终指向当前行的ID，
// 不能使用商品编号代替订单编号。
// 列表里的付款方式是原单冻结值，
// 不随商品开关后来修改而变化。
// 付款失败或未提交资料的历史记录也保留，
// 用户可以从原单继续。
// 前一页禁用条件使用当前页码，
// 后一页依赖服务器实际总数。
// 筛选变化重置分页，
// 避免留在不存在的页数而误报没有数据。
// 管理员和业务员共享队列组件，
// 但可用动作由详情的能力判断区分。
// 列表没有批量完成、批量揭示或导出按钮，
// 避免绕开逐单人工核实。
// 打开列表不要求安全重验证，
// 只有用户主动揭示资料才进入验证流程。
// 前台状态映射经过翻译函数，
// 不能让pending 等内部枚举漏给买家。
// 同一查询客户端刷新列表和详情，
// 避免回到列表仍显示旧处理人。
// 查询键的运营标志避免个人历史和后台队列相互污染。
// 选中商品与选中订单分别保存，
// 两种弹层不会同时处理同一次购买。
// 订单创建回调先关闭结账，
// 再打开订单详情，
// 避免重叠支付提示。
// 当前页刷新不会清空商品说明或替换买家已确认的历史规则快照。
// 错误重试只执行失败的数据读取，
// 不执行已经确认过的资金动作。
// 没有可用商品时仍保留我的订单标题和历史列表，
// 售后不依赖在售状态。
// 当加密未配置时提示联系管理员，
// 不能让用户把会话改发到普通备注。
// 页面不展示 GPTLoad内部地址，
// 后台办理仍在 NewAPI 控制台完成。
// 管理员商品配置入口与运营队列入口独立，
// 不通过同一页面切换角色。
export function AccountRecharge(props: { operations?: boolean }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const user = useAuthStore((state) => state.auth.user)
  const operations = props.operations === true // 模式由路由组件传入，不从 URL 自由参数提权。
  const allowed = !operations || hasPermission(user, 'recharge_orders', 'read')
  const [page, setPage] = useState(1) // 分页从一开始，与服务端 Offset 计算约定一致。
  const [status, setStatus] = useState(operations ? 'pending' : '') // 买家默认看全部历史，业务员优先领取新任务。
  const [mine, setMine] = useState(false)
  const [keyword, setKeyword] = useState('') // 订单号属于短时检索条件，不保存到用户偏好。
  const [product, setProduct] = useState<Product | null>(null)
  const [orderID, setOrderID] = useState<number | null>(null) // 使用 null 区分关闭弹层与有效数据库编号。
  // 两种模式使用独立查询键，
  // 切换后台时不会闪现个人列表。
  const catalog = useQuery({
    queryKey: ['recharge', 'products'],
    queryFn: () => getProducts(),
    enabled: !operations, // 运营队列不额外请求买家商品信息。
  })
  const orders = useQuery({
    queryKey: ['recharge', 'orders', operations, page, status, mine, keyword],
    queryFn: () => getOrders(operations, page, status, mine, keyword.trim()),
    enabled: allowed, // 权限撤销后停止列表查询。
  })
  if (!allowed) return <ErrorState title={t('Access denied')} />
  const statuses = [
    ['', 'All orders'], // 空值表示无状态约束，不能传译后的中文标签。
    ['pending', 'Awaiting claim'],
    ['processing', 'Order processing'],
    ['awaiting_info', 'Submit account information'],
    ['action_required', 'Additional information required'],
    ['completed', 'Completed'],
    ['exception', 'Order needs attention'],
    ['cancelled', 'Cancelled'],
  ]
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t(operations ? 'Recharge order management' : 'Account top-ups')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          disabled={orders.isFetching}
          onClick={() => void orders.refetch()}
        >
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-6'>
          {!operations && (
            <>
              <p className='text-muted-foreground'>
                {t(
                  'Choose a plan, pay, and submit your account information. Follow progress in My orders.'
                )}
              </p>
              {catalog.isLoading && <LoadingState />}
              {catalog.isError && (
                <ErrorState onRetry={() => void catalog.refetch()} />
              )}
              {catalog.data && !catalog.data.configured && (
                <p role='status'>
                  {t(
                    catalog.data.enabled
                      ? 'Account top-ups are not available yet. Please contact the administrator.'
                      : 'Account top-ups are paused'
                  )}
                </p>
              )}
              {catalog.data &&
                !catalog.data.configured &&
                user?.role === ROLE.SUPER_ADMIN && (
                  <Link
                    className='text-sm underline'
                    to='/system-settings/billing/$section'
                    params={{ section: 'recharge' }}
                  >
                    {t('Account top-up settings')}
                  </Link>
                )}
              {catalog.data?.items.length === 0 && (
                <EmptyState title={t('No recharge products available')} />
              )}
              {/* 商品说明承载账户条件、周期、时效和售后规则，购买前完整呈现。 */}
              <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-3'>
                {catalog.data?.items.map((item) => (
                  <Card
                    key={item.id}
                    className='hover:ring-foreground/20 h-full min-w-0 gap-4 transition-colors'
                  >
                    <CardContent className='flex-1'>
                      <RechargeProductInfo product={item} compact />
                    </CardContent>
                    <CardFooter className='flex-col items-stretch gap-3 border-t pt-4'>
                      <div className='flex flex-wrap items-end justify-between gap-2'>
                        <p className='text-2xl font-semibold tracking-tight'>
                          <span className='text-muted-foreground mr-1 text-xs font-medium'>
                            CNY
                          </span>
                          {formatNumber(item.price_cents / 100, locale)}
                        </p>
                        <p className='text-muted-foreground text-xs'>
                          {/* 按当前用户实际可用渠道展示，不再固定声称只有微信。 */}
                          {[
                            ...(catalog.data?.payment_methods ?? []).map(
                              (method) => t(method.name)
                            ),
                            ...(item.allow_wallet_payment
                              ? [t('Platform balance')]
                              : []),
                          ].join(' / ')}
                        </p>
                      </div>
                      <Button
                        disabled={
                          !catalog.data.configured ||
                          (!item.allow_wallet_payment &&
                            catalog.data.payment_methods.length === 0)
                        }
                        onClick={() => setProduct(item)}
                      >
                        {t('Choose plan')}
                      </Button>
                    </CardFooter>
                  </Card>
                ))}
              </div>
              <h2 className='text-lg font-semibold'>{t('My orders')}</h2>
            </>
          )}
          <div className='flex flex-wrap items-center gap-2'>
            <Input
              className='max-w-xs'
              aria-label={t('Order number')}
              placeholder={t('Order number')}
              value={keyword}
              onChange={(event) => {
                setKeyword(event.target.value)
                setPage(1)
              }}
            />
            {operations && (
              <>
                {/* 筛选只影响列表，不授予某个订单的处理或敏感读取权。 */}
                <NativeSelect
                  aria-label={t('Order status')}
                  value={status}
                  onChange={(event) => {
                    setStatus(event.target.value)
                    setPage(1)
                  }}
                >
                  {statuses.map(([value, label]) => (
                    <NativeSelectOption key={value} value={value}>
                      {t(label)}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                <Button
                  variant={mine ? 'default' : 'outline'}
                  onClick={() => {
                    setMine(!mine)
                    setStatus('')
                    setPage(1)
                  }}
                >
                  {t('Assigned to me')}
                </Button>
              </>
            )}
          </div>
          {orders.isLoading && <LoadingState />}
          {orders.isError && (
            <ErrorState onRetry={() => void orders.refetch()} />
          )}
          {orders.data && (
            <StaticDataTable
              data={orders.data.items}
              getRowKey={(order) => order.id}
              emptyContent={<EmptyState title={t('No orders')} />}
              columns={[
                {
                  id: 'order',
                  header: t('Order number'),
                  cell: (order) => (
                    <Button variant='link' onClick={() => setOrderID(order.id)}>
                      {order.trade_no}
                    </Button>
                  ),
                },
                {
                  id: 'product',
                  header: t('Product'),
                  cell: (order) => (
                    <div>
                      <p>{order.title}</p>
                      <p className='text-muted-foreground text-xs'>
                        {new Date(order.created_at * 1000).toLocaleString(
                          locale
                        )}
                      </p>
                    </div>
                  ),
                },
                {
                  id: 'amount',
                  header: t('Amount'),
                  cell: (order) => (
                    <span>
                      CNY {formatNumber(order.money_cents / 100, locale)}
                    </span>
                  ),
                },
                {
                  id: 'payment',
                  header: t('Payment method'),
                  cell: (order) => t(rechargePaymentLabel(order)), // 同一挂帐来源在列表和详情保持一致。
                },
                {
                  id: 'status',
                  header: t('Status'),
                  cell: (order) => t(orderStatus(order, operations)),
                },
                ...(operations
                  ? [
                      {
                        id: 'assignee',
                        header: t('Assignee'),
                        cell: (order: import('./api').Order) => (
                          <span>
                            {order.assignee_id || '—'}
                            <br />
                            {order.email_mask}
                          </span>
                        ),
                      },
                    ]
                  : []),
                {
                  id: 'action',
                  header: t('Actions'),
                  cell: (order) => (
                    <Button
                      size='sm'
                      variant='outline'
                      onClick={() => setOrderID(order.id)}
                    >
                      {t('View order')}
                    </Button>
                  ),
                },
              ]}
            />
          )}
          {/* 分页访问服务端，不下载全部订单或认证资料供浏览器筛选。 */}
          <div className='flex items-center justify-end gap-3'>
            <Button
              variant='outline'
              disabled={page === 1}
              onClick={() => setPage(page - 1)}
            >
              {t('Previous')}
            </Button>
            <span>{formatNumber(page, locale)}</span>
            <Button
              variant='outline'
              disabled={!orders.data || page * 20 >= orders.data.total}
              onClick={() => setPage(page + 1)}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
        {product && catalog.data && (
          <RechargeCheckout
            key={product.id}
            product={product}
            catalog={catalog.data}
            onClose={() => setProduct(null)}
            onCreated={(order) => {
              setProduct(null)
              setOrderID(order.id)
              void orders.refetch()
            }}
          />
        )}
        {orderID !== null && (
          <RechargeOrderDetail
            key={orderID}
            id={orderID}
            operations={operations}
            onClose={() => setOrderID(null)}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

// 路由组件不接受浏览器参数切换模式，
// 运营入口由权限路由单独挂载。
export function RechargeOperations() {
  return <AccountRecharge operations />
}
