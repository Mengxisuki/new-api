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
import { z } from 'zod'

import { creditAccountSchema } from '@/features/credit/api'
import { api, type ApiRequestConfig } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

// 接口边界验证持久摘要，
// 不将认证凭据放入订单 DTO。
// 所有金额均为人民币分，
// 与站点钱包展示币种无关。
// 商品返回值只承载购买决策信息，
// 不包含商户配置或供应商凭据。
// 价格以人民币分传递，
// 展示时才除以一百，
// 不能套用钱包汇率显示。
// 布尔字段必须明确存在，
// 缺少余额开关不能推定为允许付款。
// 版本是服务器冻结报价的条件，
// 编辑后的旧页面必须重新取得商品。
// 描述由后台维护账户要求、期限、办理时效及售后规则。
// 商品接口读取失败与正常空列表不同，
// 必须由页面显示真实错误。
export const productSchema = z.object({
  id: z.number(),
  title: z.string(),
  description: z.string(),
  // 展示字段随商品和订单一同返回，历史空值不推断为当前套餐。
  summary: z.string(),
  provider: z.string(),
  period: z.string(),
  delivery_time: z.string(),
  icon: z.string(),
  price_cents: z.number(),
  enabled: z.boolean(),
  allow_wallet_payment: z.boolean(),
  sort_order: z.number(),
  version: z.number(),
})
// 支付状态与办理状态分开，
// 领取不代表收款或完成。
// 订单摘要严格排除session、完整邮箱、支付凭据和内部说明。
// 处理状态与付款状态并存，
// 客户端不能根据领取动作推断已付款。
// 金额是下单时的快照，
// 商品价格变动不能改写历史展示。
// 处理人编号只用于操作提示，
// 后端每次请求仍重新核对授权。
// 公开说明可展示给买家，
// 内部说明只在有权限的运营详情另行返回。
// 资料替换、领取、转派和读取都会改变版本，
// 写入携带当前版本。
// 退款申请不等于退款成功，
// 不能仅凭布尔标志显示已退款。
// 同步标志表示余额已在数据库返还但缓存恢复尚待确认。
// 完成时间来自人工结果确认，
// 不来自浏览器按钮点击时间。
// 付款期限只限制展示付款入口，
// 不能据此前端断言未收款。
// 认证信息不会通过兼容字段或未知字段透传到查询缓存。
// 服务端返回错误结构时 Zod会明确抛错，
// 不造空订单继续流程。
export const orderSchema = z.object({
  id: z.number(),
  user_id: z.number(),
  product_id: z.number(),
  title: z.string(),
  description: z.string(),
  // 展示字段随商品和订单一同返回，历史空值不推断为当前套餐。
  summary: z.string(),
  provider: z.string(),
  period: z.string(),
  delivery_time: z.string(),
  icon: z.string(),
  base_cents: z.number(),
  money_cents: z.number(),
  trade_no: z.string(),
  payment_method: z.string(),
  payment_provider: z.string(),
  payment_status: z.string(),
  status: z.string(),
  assignee_id: z.number(),
  email_mask: z.string(),
  public_note: z.string(),
  invoice_required: z.boolean(), // 下单时冻结的开票选择，不从当前抬头配置推断。
  refund_requested: z.boolean(),
  wallet_sync_pending: z.boolean(), // 同步失败提供恢复按钮，不能再次执行数据库加额。
  version: z.number(),
  created_at: z.number(),
  updated_at: z.number(),
  paid_at: z.number(),
  completed_at: z.number(),
  expires_at: z.number(),
})
export type Product = z.infer<typeof productSchema>
export type Order = z.infer<typeof orderSchema>
export type Purchase = {
  product_id: number
  product_version: number
  payment_method: string
  invoice_required: boolean
  quoted_cents?: number // 创建时绑定用户确认的报价，不能静默改变金额。
  request_id: string
}
const root = '/api/account-recharge'
// 请求身份由既有客户端管理，
// 不把cookie、session或证明写入本地存储。
const once: ApiRequestConfig = { singleUseAuthorization: true }
const catalogSchema = z.object({
  credit: creditAccountSchema.nullable().optional(), // 本人可用额度，历史响应可不含此新增字段。
  items: z.array(productSchema),
  payment_methods: z.array(z.object({ id: z.string(), name: z.string() })),
  configured: z.boolean(),
  enabled: z.boolean(),
})
export type Catalog = z.infer<typeof catalogSchema>
// 用户与管理员使用不同接口，
// 后者允许查看下架商品。
// 返回支付方式仅有公开标识和显示名称，
// 不包含完整渠道配置。
// 配置就绪标志用于阻止无法收集资料的购买，
// 不替代服务端检查。
// 请求不触发支付或外部账户操作，
// 刷新商品不会产生订单。
// 显式关闭 GET 去重，
// 以便上下架后及时重新读取服务器结果。
export async function getProducts(admin = false) {
  const response = await api.get(`${root}${admin ? '/admin' : ''}/products`, {
    disableDuplicate: true,
  })
  return catalogSchema.parse(requireServerSuccess(response.data).data)
}
// 保存只传商品字段，
// 版本由服务端乐观锁校验。
// 管理员保存后仍解析服务器返回的版本，
// 不能在客户端自行累加。
// 只提交商品白名单字段，
// 不接收订单状态或上游自动化选项。
// 上下架使用明确布尔值；
// 服务端会保存false而不是忽略它。
// 保存失败保留表单，
// 不能提前关闭后向用户表示修改成功。
// 该接口只管理未来购买，
// 不执行退款或回溯修改已付订单。
export async function saveProduct(product: Product) {
  const response = await api.put(`${root}/admin/products`, product)
  return productSchema.parse(requireServerSuccess(response.data).data)
}
// 报价和创建使用同样的选择，
// 页面不得自行计算发票附加费。
// 报价可以重复读取，
// 不能消耗钱包或生成支付交易。
// 发票附加费以服务器规则为准，
// 浏览器不能复制税费计算。
// 余额换算同样返回服务器确定的整数额度，
// 不能使用显示值扣款。
// 报价变更后要重新确认总价，
// 创建请求携带用户确认过的分金额。
// 接口失败必须停止结账，
// 禁止使用上一份旧价格偷偷继续。
export async function quoteOrder(purchase: Purchase) {
  const response = await api.post(`${root}/quote`, purchase)
  return z
    .object({
      money_cents: z.number(),
      base_cents: z.number(),
      wallet_quota: z.number(),
    })
    .parse(requireServerSuccess(response.data).data)
}
// 创建请求 UUID来自当前结账实例，
// 超时重试保持不变。
// 同一 UUID 的商品、方式或报价变化由服务器返回冲突。
// 成功只代表订单已保存，
// 页面随后打开原订单付款入口。
// 不开启自动重放，
// 避免认证恢复把不可知的创建请求再次发送。
// 用户可从本人订单列表恢复，
// 不依赖浏览器保存交易正文。
export async function createOrder(purchase: Purchase) {
  const response = await api.post(`${root}/orders`, purchase, once)
  return orderSchema.parse(requireServerSuccess(response.data).data)
}
// 继续支付必须用已持久化ID，
// 不生成新订单或切换付款方式。
// 付款返回值按交互类型验证，
// 不能把二维码地址当成跳转链接。
// 网关表单与官方微信二维码使用不同展示方式，
// 共用原订单编号。
// 收到 paid 仅触发重新查询摘要，
// 界面不自行增加钱包余额。
// 未确认付款成功时订单仍待付款，
// 外部窗口关闭不能代表完成。
// 付款请求不接受新的支付方式，
// 已生成微信凭据不能改成余额扣款。
// 接口超时仍保留订单，
// 用户应刷新原单而不是重复购买。
// 网关响应不包含商户密钥，
// 签名表单仅供完成当前收款。
export async function payOrder(id: number) {
  const response = await api.post(`${root}/orders/${id}/pay`, {}, once)
  return z
    .discriminatedUnion('payment_kind', [
      z.object({ payment_kind: z.literal('paid') }),
      z.object({
        payment_kind: z.literal('qr'),
        qr_code: z.string(),
        trade_no: z.string(),
      }),
      z.object({
        payment_kind: z.literal('redirect'),
        redirect_url: z.string(),
      }),
      z.object({
        payment_kind: z.literal('form'),
        url: z.string(),
        params: z.record(z.string(), z.unknown()),
      }),
    ])
    .parse(requireServerSuccess(response.data).data)
}
// 列表查询只缓存安全摘要，
// 业务员筛选由服务端处理。
// 查询参数只有分页、状态、本人待办和完整订单号。
// 购买者身份由会话确定，
// 浏览器不能传另一个 user_id。
// 运营查询必须经过业务员能力检查，
// 不因页面参数为 true就授权。
// 请求摘要可进入内存缓存，
// 但不持久化到浏览器存储。
// 总数与当前过滤条件保持一致，
// 不取全量订单后在浏览器筛选。
// 详情与列表采用相同订单schema，
// 避免某个入口意外透传凭据。
// 事件记录只在服务器允许的运营详情返回，
// 买家没有内部记录。
// 操作记录与当前摘要一起刷新，
// 读取成功不等于获得处理权。
// 404不区分资源不存在与不是本人订单，
// 避免枚举其他用户购买。
// 客户端不能通过传空所有者或改管理标志绕过后端路由权限。
export async function getOrders(
  operations: boolean,
  page: number,
  status: string,
  mine: boolean,
  keyword: string
) {
  const response = await api.get(
    `${root}/${operations ? 'operations/orders' : 'orders/self'}`,
    {
      params: { page, size: 20, status, mine, keyword },
      disableDuplicate: true,
    }
  )
  return z
    .object({ items: z.array(orderSchema), total: z.number() })
    .parse(requireServerSuccess(response.data).data)
}
// 订单只读取精简的本人发票摘要，不在详情缓存税号或银行资料。
// 状态枚举与既有开票流程一致；未知状态应暴露协议错误。
// 金额来自冻结申请，不能从当前套餐价格重新计算。
// 下载只需申请 ID，附件路径始终留在后端。
const orderInvoiceSchema = z.object({
  id: z.number(),
  status: z.enum([
    'pending_payment',
    'pending',
    'processing',
    'issued',
    'rejected',
  ]),
  title: z.string(),
  pay_amount: z.number(), // 发票以元为单位，不能与订单分金额混用。
  invoice_number: z.string(), // 未开票允许空串，不用交易号替代。
  remark: z.string(),
  has_file: z.boolean(), // 仅表示持久文件引用存在，不携带下载 URL。
})
export type OrderInvoice = z.infer<typeof orderInvoiceSchema>

export async function getOrder(id: number, operations: boolean) {
  const response = await api.get(
    `${root}/${operations ? 'operations/' : ''}orders/${id}`,
    { disableDuplicate: true }
  )
  return z
    .object({
      order: orderSchema,
      credit_status: z
        .enum(['outstanding', 'settled', 'cancelled', 'refunded'])
        .optional(), // 原单精确关联的债务状态。
      invoice: orderInvoiceSchema.nullable().optional(), // 运营响应不含本人税务资料。
      internal_note: z.string().optional(),
      events: z
        .array(
          z.object({
            id: z.number(),
            actor_id: z.number(),
            action: z.string(),
            note: z.string(),
            created_at: z.number(),
          })
        )
        .optional(),
    })
    .parse(requireServerSuccess(response.data).data)
}
// 提交不用 useMutation保存variables；
// 调用者负责成功或关闭时清空内存。
// singleUseAuthorization禁止认证恢复拦截器自动重放凭据正文。
// session 只通过 POST正文传输，
// 不放入路径、查询串或订单备注。
// 资料成功提交后调用者清空表单，
// 重交必须由服务器状态明确允许。
// 错误显示统一文案，
// 不把 JSON解析器原文或认证字段复制到提示。
// 用户不通过此接口回读原始资料，
// 忘记提交内容可按要求重新获取。
// 替换原资料的状态和密文保存由服务器同一事务执行。
export async function submitInfo(
  id: number,
  version: number,
  email: string,
  session: string
) {
  const response = await api.post(
    `${root}/orders/${id}/account-info`,
    { version, email, session },
    once
  )
  requireServerSuccess(response.data)
}
// 处理动作不允许传入金额、收款状态或完整订单更新。
// 领取、补资料、完成及内部备注都显式传动作，
// 不能提交任意状态。
// 转派目标仅表示建议接手人，
// 服务器还检查其角色及启用状态。
// 旧版本失败后需要重新读取详情，
// 不自动重复外部办理。
// 人工完成只表示业务员已核实结果，
// 不模拟上游接口确认。
// 退款申请使用办理动作，
// 但资金退还必须调用管理员独立接口。
export async function actOrder(
  id: number,
  version: number,
  action: string,
  note: string,
  assignee_id: number
) {
  const response = await api.post(
    `${root}/operations/orders/${id}/action`,
    { version, action, note, assignee_id },
    once
  )
  requireServerSuccess(response.data)
}
// 敏感读取只能显式触发且不进入ReactQuery；
// 证明限定本次版本和动作。
// 单次证明放在专用请求头，
// 禁止放在URL 或持久表单字段。
// 服务端校验当前处理人和版本，
// 转派后旧证明无法继续读取。
// 返回值只交给当前弹层，
// 不能保存到订单摘要或 mutation记录。
// 前端关闭弹层只能清理自身状态，
// 无法撤回已复制到剪贴板的内容。
// 读取成功会生成审计事件并推进版本，
// 调用方应重新读取安全摘要。
export async function revealInfo(id: number, version: number, proof: string) {
  const response = await api.post(
    `${root}/operations/orders/${id}/reveal-account-info`,
    { version },
    { ...once, headers: { 'X-Security-Proof': proof } }
  )
  return z
    .object({ email: z.string(), session: z.string() })
    .parse(requireServerSuccess(response.data).data)
}
// 管理员批准与确认是两次独立动作，
// 真实网关退款凭证仅在确认时传入。
// 批准退款首先停止履约，
// 确认退款才记录真实资金结果。
// 微信的参考号代表管理员已核实的网关退款，
// 不触发供应商调用。
// 余额退款使用服务器冻结的原扣除值，
// 前端不提供退款金额输入。
// 重复确认已退款原单只恢复缓存同步，
// 不能再发一次资金返还。
// 退款证明同样绑定动作、订单和版本，
// 不能借读取证明执行退款。
export async function refundOrder(
  id: number,
  version: number,
  confirm: boolean,
  note: string,
  reference: string,
  proof: string
) {
  const response = await api.post(
    `${root}/admin/orders/${id}/refund`,
    { version, confirm, note, reference },
    { ...once, headers: { 'X-Security-Proof': proof } }
  )
  requireServerSuccess(response.data)
}
