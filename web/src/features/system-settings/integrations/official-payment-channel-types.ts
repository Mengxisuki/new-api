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
// 官方直连仅覆盖现有两种商户协议，新增渠道须同步服务端校验。
export type OfficialPaymentChannelType = 'alipay' | 'wechat'

export type OfficialPaymentChannelData = {
  id: string // ID 同时进入支付方式选择与通知路由，创建后不可更改。
  name: string // 展示名允许独立修改，不承担订单网关身份判断。
  icon?: string // 图标沿用项目安全图标渲染器，不作为可执行内容。
  channel_type: OfficialPaymentChannelType // 商户协议决定必填凭证与交互方式。
  interaction_mode: 'qr' | 'page' | 'wap' | 'redirect' // 微信仅二维码与 H5，支付宝另有页面模式。
  min_topup?: number // 充值最低数量仍由服务端再次验证。
  enabled: boolean // 草稿可不完整，启用前必须具备所选商户的凭证。
  sort_order?: number // 只影响渠道展示次序，不改变扣费优先级。
  config: Record<string, string> // 服务端脱敏后秘密值为空，空值保存表示保留原值。
  configured_secrets?: string[] // 仅说明已保存哪些凭证，不能推导或回显秘密本身。
}

// 模板仅用于新建草稿，不会自动启用商户或持久化支付配置。
export const OFFICIAL_CHANNEL_DEFAULTS: Record<
  OfficialPaymentChannelType,
  OfficialPaymentChannelData
> = {
  alipay: {
    id: 'official_alipay', // 初始名称与服务端官方渠道命名保持一致。
    name: 'Alipay',
    icon: 'SiAlipay',
    channel_type: 'alipay',
    interaction_mode: 'page',
    min_topup: 0,
    enabled: false, // 新商户配置验证完成前不能进入用户付款列表。
    sort_order: 10,
    config: {
      app_id: '',
      private_key: '', // 应用签名密钥绝不从模板填入真实值。
      alipay_public_key: '', // 验签公钥与应用私钥必须分别配置。
      gateway_url: 'https://openapi.alipay.com/gateway.do',
      notify_url: '', // 留空采用当前站点后端通知路径。
      return_url: '', // 用户导航返回地址不承担异步到账判定。
      sign_type: 'RSA2',
      app_cert_sn: '',
      alipay_root_cert_sn: '',
    },
  },
  wechat: {
    id: 'official_wechat', // 微信商户身份独立于微信登录应用配置。
    name: 'WeChat Pay',
    icon: 'SiWechat',
    channel_type: 'wechat',
    interaction_mode: 'redirect',
    min_topup: 0,
    enabled: false, // 新商户配置验证完成前不能进入用户付款列表。
    sort_order: 20,
    config: {
      appid: '',
      mchid: '',
      merchant_serial_no: '', // 商户证书序列号用于请求签名标识。
      merchant_private_key: '', // 私钥由管理员主动输入，不从公开状态读取。
      api_v3_key: '', // 微信通知解密使用单独的 API v3 密钥。
      wechatpay_public_key_id: '', // 公钥模式保留 PUB_KEY_ID 前缀约束。
      wechatpay_public_key: '', // 显式配置微信公钥，避免依赖运行时证书下载。
      notify_url: '', // 留空采用当前站点后端通知路径。
      h5_redirect_url: '', // H5 完成后返回位置独立于服务器通知地址。
      h5_type: 'WAP', // 普通网页默认使用微信规定的 WAP 场景。
      h5_wap_url: '',
      h5_wap_name: '',
    },
  },
}

// 每个新草稿复制 config，编辑一个渠道不会污染后续创建的模板。
// 序号只生成候选 ID，调用方还要检查实际列表中的 ID 冲突。
export function createOfficialChannelDefault(
  channelType: OfficialPaymentChannelType,
  existingCount = 0
): OfficialPaymentChannelData {
  const template = OFFICIAL_CHANNEL_DEFAULTS[channelType]
  const suffix = existingCount > 0 ? `_${existingCount + 1}` : ''
  return {
    ...template,
    id: `${template.id}${suffix}`,
    config: { ...template.config }, // 凭据字段始终使用独立对象，避免共享可变状态。
  }
}
