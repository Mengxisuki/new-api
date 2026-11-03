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
// 单份常用抬头保存在服务器，付款时另外冻结，不把敏感资料写入本地缓存。
export interface InvoiceProfile {
  user_id?: number // 归属由服务端会话决定，表单不会提交替代用户标识。
  title_type: 'business' | 'personal' // 企业抬头需税号，个人抬头不借用企业校验。
  title: string // 以创建申请时的名称开票，不从当前个人资料反查。
  tax_id: string // 企业税号由服务器规范化，不能从用户名推断。
  email: string // 发票联系邮箱与登录邮箱独立维护。
  address: string // 非必填资料以空串清空，避免保留上一份抬头的旧值。
  phone: string // 仅作为开票联络信息，不参与身份认证。
  bank_name: string // 银行资料属于敏感抬头内容，禁止进入公共状态接口。
  bank_account: string // 不转换为数字，必须保留前导零。
  updated_at?: number // 用于显示已保存资料，不作为浏览器授权依据。
}

// 状态来自后端付款与财务流程，用户界面不自行推进状态。
export type InvoiceStatus =
  | 'pending_payment'
  | 'pending'
  | 'processing'
  | 'issued'
  | 'rejected'

export interface InvoiceRequest {
  id: number
  user_id: number
  trade_no: string // 始终关联真实支付单，不能由用户替换后开票。
  business_type: 'topup' | 'subscription' | 'recharge' // 代充值收款独立开票，不增加平台权益。
  profile: InvoiceProfile // 申请时的抬头快照，修改常用配置不重写旧申请。
  base_amount: number // 原始实付基数已经包含普通充值优惠。
  fee_rate: number // 使用百分数，如六表示百分之六。
  fee_amount: number // 加价只计入收款，不转换成模型使用额度。
  pay_amount: number // 最终开票金额与这笔订单实收一致。
  currency: 'CNY' // 冻结交易币种，站点显示币种改变不能重标历史发票。
  status: InvoiceStatus
  invoice_number: string // 财务上传附件时填写，用户不允许自行修改。
  remark: string // 审核意见供本人查看，不包含后台原始异常或存储路径。
  has_file: boolean // 附件本身通过鉴权接口下载，不暴露公共文件 URL。
  created_at: number
  updated_at: number
  issued_at: number
}

export interface InvoiceList {
  items: InvoiceRequest[]
  total: number
  page: number
  size: number
}
