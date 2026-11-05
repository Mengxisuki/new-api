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

import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

// 所有金额以人民币整数分传输，与钱包显示币种无关。
// 边界解析拒绝字段缺失，不用空账本伪装读取成功。
// 账户只表达授信，不承担钱包充值或订阅权益的发放。
// 余额字段全部使用整数，不能把价格格式化文本带回付款接口。
// 当前两个预留模块字段仍由服务端返回，便于界面说明开放范围。
// 字段存在不代表该模块已提供真实付款实现。
// 授信版本只描述配置修改，不随消费次数变化。
// 因此付款引起的占用变化不会无故阻止管理员编辑配置。
// 保存时的剩余额度检查始终由后端完成。
// 历史接口允许目录没有此新增对象，但对象存在时必须完整验证。
// 用户目录的授权字段来自本人会话，不从 URL 读取所有者。
// 财务账户列表另外扩展用户名和显示名，不扩散用户完整模型。
export const creditAccountSchema = z.object({
  user_id: z.number().int(), // 已有用户编号，不建立额外身份体系。
  enabled: z.boolean(),
  limit_cents: z.number().int(),
  used_cents: z.number().int(), // 占用只读，不能从配置表单提交。
  recharge_enabled: z.boolean(),
  topup_enabled: z.boolean(), // 预留能力，首期始终关闭。
  subscription_enabled: z.boolean(),
  version: z.number().int(), // 管理配置并发修改依据。
})
export type CreditAccount = z.infer<typeof creditAccountSchema>
const accountView = creditAccountSchema.extend({
  username: z.string(),
  display_name: z.string(),
})
export type CreditAccountView = z.infer<typeof accountView>

// 内部状态只在渲染时翻译；所有页面共享同一套债务语义。
export const CREDIT_STATUS = {
  outstanding: 'Outstanding credit',
  settled: 'Credit settled',
  cancelled: 'Credit cancelled',
  refunded: 'Refunded',
} as const
// 流水金额在下单后冻结，财务不能通过结清编辑金额。
// 挂帐状态独立于代充值的人工办理进度。
// 未结清表示已占额度、尚未登记实际收款。
// 已结清表示已核实全额收款、占用已经恢复。
// 已撤销表示未收款的订单退款，不存在现金返还。
// 已退款表示此前已结清，之后登记了真实退款。
// 收款记录与退款记录同时保留，不能用后者覆盖前者。
// 支付重试不会改变账本交易号。
// 模块和交易号在后端具有联合唯一约束。
// 这里对白名单之外状态报解析错误，避免未知状态误显示成功。
const entrySchema = z.object({
  id: z.number().int(),
  user_id: z.number().int(),
  username: z.string(),
  display_name: z.string(),
  module: z.literal('recharge'), // 未接入模块不得伪装成可处理记录。
  trade_no: z.string(),
  amount_cents: z.number().int(),
  status: z.enum(['outstanding', 'settled', 'cancelled', 'refunded']),
  created_at: z.number(),
  settled_at: z.number(),
  settled_by: z.number().int(), // 对账显示真实操作人编号。
  settlement_ref: z.string(),
  settlement_note: z.string(),
  closed_at: z.number(),
  closed_by: z.number().int(),
  refund_ref: z.string(), // 退还已收款不抹掉原收款凭证。
})
export type CreditEntry = z.infer<typeof entrySchema>

// 管理员与财务使用不同路径，后端独立校验权限。
// 搜索和分页均在服务端执行，不下载全用户授信数据。
export async function getCreditAccounts(
  admin: boolean,
  page: number,
  keyword: string
) {
  const response = await api.get(
    `/api/${admin ? 'option' : 'finance'}/credit/accounts`,
    {
      params: { page, size: 20, keyword },
      disableDuplicate: true,
    }
  )
  return z
    .object({ items: z.array(accountView), total: z.number() })
    .parse(requireServerSuccess(response.data).data)
}

// 编辑仅提交白名单配置，使用显式版本避免覆盖别人的新修改。
// 金额在表单入口转换为分，这里不再二次换算。
export async function saveCreditAccount(
  account: Pick<
    CreditAccount,
    'user_id' | 'enabled' | 'limit_cents' | 'recharge_enabled' | 'version'
  >
) {
  const response = await api.put('/api/option/credit/accounts', account, {
    singleUseAuthorization: true,
  })
  requireServerSuccess(response.data)
}

// 用户搜索只请求编号和名称，不把整份用户档案放入选择器。
// 输入为空时展示最近用户，更多候选通过关键字查找。
export async function searchCreditUsers(keyword: string) {
  const response = await api.get('/api/option/credit/users', {
    params: { keyword },
    disableDuplicate: true,
  })
  return z
    .array(
      z.object({
        id: z.number(),
        username: z.string(),
        display_name: z.string(),
      })
    )
    .parse(requireServerSuccess(response.data).data)
}

// 总金额与列表使用相同筛选条件，不在前端只累加当前页。
// 默认状态由调用者明确传入，不能把全量流水解释成未结清。
// 接口返回金额为整数分，显示层再格式化。
// 查询参数的用户编号只用于财务筛选，不授予读取权限。
// 服务端统一限制页大小，浏览器不能改成全量下载。
// 未结清合计需显式传入对应状态。
// 已撤销记录仍保留原金额，便于追溯业务取消。
// 未知状态在边界报错，不自动转换成已结清。
export async function getCreditEntries(
  page: number,
  status: string,
  keyword: string,
  userID: number
) {
  const response = await api.get('/api/finance/credit/entries', {
    params: { page, status, keyword, user_id: userID },
    disableDuplicate: true,
  })
  return z
    .object({
      items: z.array(entrySchema),
      total: z.number(),
      amount_cents: z.number(),
    })
    .parse(requireServerSuccess(response.data).data)
}

// 整笔结清金额来自服务端冻结值，浏览器只能填写凭证和备注。
// 自动认证恢复不能盲目重放资金确认；用户可在原记录核对后重试。
// 提交参数不包含所有者，不能通过改请求体替别人结清。
// 传入编号来自用户主动打开的财务行。
// 凭证号是外部收款证据，不是网关调用参数。
// 备注只供内部对账，不发给代充值买家。
// 成功后调用者必须同时失效账本及授信查询。
// 请求失败保留原记录和表单，允许人工核对后重试。
// 接口返回业务失败时必须抛错，不能只看 HTTP 状态码。
export async function settleCreditEntry(
  id: number,
  reference: string,
  note: string
) {
  const response = await api.post(
    `/api/finance/credit/entries/${id}/settle`,
    { reference, note },
    { singleUseAuthorization: true }
  )
  requireServerSuccess(response.data)
}
