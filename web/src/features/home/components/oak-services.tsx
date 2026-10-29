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
import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

// 服务说明只负责选型与导航，不创建订阅、绑定账号或发起充值。
// 量贩价格复用实时目录，其余服务先通过现有采购渠道确认。
// 周额度是方案示例，不是后台套餐配置，也不是月费报价。
// 账号挂靠使用用户已有账号，不能暗示随服务赠送厂商账号。
// 代充值进入用户自己的厂商账户，区别于平台钱包余额。
export function OakServices() {
  const { t } = useTranslation()
  // 文案在渲染时翻译，切换语言后无需重新载入首页。
  // 本组件是静态说明，较长部分为四类必要文案与语义标记，无状态分支拆分需求。
  const services = [
    {
      id: 'tokens', // 稳定标识只用于渲染，不参与计费或模型路由。
      title: t('Token usage'), // 按量服务名称与控制台订阅产品分开，避免混淆计费周期。
      label: t('For flexible workloads'), // 先提示适用负载，不承诺固定月费或包量。
      // 按量说明以平台余额为起点，避免与自有账号余额混淆。
      description: t(
        'Top up your Oak balance and pay for the tokens you use. Choose available models as your coding, analysis and research needs change.'
      ),
      // 倍率由管理侧维护，对用户只公开最终计费规则和查询入口。
      detail: t(
        'Charges follow each model’s published rates and billing multipliers. Check model pricing before you start.'
      ),
      action: t('Explore models and pricing'), // 量贩用户可直接核对现有模型的计费信息。
    },
    {
      id: 'monthly', // 月付周期与周额度分别解释，避免把额度当售价。
      title: t('Monthly plans'), // 月付购买周期不代表额度也按月重置。
      label: t('For regular use'), // 稳定用量用户优先关注周期预算，而非单次请求价格。
      // 包月覆盖使用节奏，具体重置和续费限制留给所选套餐。
      description: t(
        'Subscribe monthly with a weekly usage allowance. A predictable rhythm for everyday coding, writing and research.'
      ),
      // 示例金额始终带示例标记，防止后台套餐变更后留下错误报价。
      detail: t(
        'Example allowance: CNY 150 per week. Actual limits and renewal rules depend on your plan.'
      ),
      action: t('Discuss a monthly plan'), // 套餐额度需先确认，不能将示例包装为立即购买。
    },
    {
      id: 'hosting', // 接入范围需咨询确认，不提供尚不存在的绑定入口。
      title: t('Account hosting'), // 挂靠仅描述已有账号接入，不作为账号销售入口。
      label: t('For accounts you already own'), // 明确账号由用户自行持有，避免误解为开通附赠。
      // 接入描述止于平台 API，不宣称支持厂商全部客户端功能。
      description: t(
        'Bring your existing provider account to Oak. Access it through a platform API endpoint without setting up an overseas network connection, and switch between supported providers’ models.'
      ),
      // 已有账号的上游限制仍然有效，不承诺通过挂靠扩大额度。
      detail: t(
        'Ask about supported account types and models before connecting. Usage remains subject to your account’s access and limits.'
      ),
      action: t('Ask about account hosting'), // 通过人工渠道确认接入资格，不暗示已有自助授权。
    },
    {
      id: 'top-ups', // 卡密适用范围以兑换说明为准，不接入平台余额兑换。
      title: t('Account top-ups'), // 外部账户充值与按量服务的余额充值独立。
      label: t('For your own provider balance'), // 先标明到账账户，用户无需到详情页才辨别服务。
      // 卡密作为交付方式，首页不披露内部供货和充值链路。
      description: t(
        'Buy a recharge card for your own provider account and follow the redemption guide to add credit. Keep using the account and tools you already know.'
      ),
      // 兑换说明在购买前确认，避免误购不适用的账号或服务卡密。
      detail: t(
        'The credit goes to your provider account, separately from your Oak balance. Check the supported service and redemption instructions before buying.'
      ),
      action: t('Ask about recharge cards'), // 卡密先核对适用范围，不调用平台钱包兑换。
    },
  ]
  return (
    <section
      // 服务区置于科研场景之前，先帮助访客选择使用方式。
      className='oak-container oak-section oak-services'
      id='oak-services' // 首屏了解服务入口的稳定定位目标。
      aria-labelledby='oak-services-title' // 区域名称随当前语言引用可见标题。
    >
      <div className='oak-section-heading'>
        <div>
          <p className='oak-eyebrow'>01 / {t('WAYS TO USE OAK')}</p>
          <h2 id='oak-services-title'>
            {t('Four ways to access the models you need.')}
          </h2>
        </div>
        <p>
          {t(
            'Pay by token, choose a monthly allowance, connect an existing account or top up your own. Start with the option that fits how you work.'
          )}
        </p>
      </div>
      <div
        /* 复用原采购方案栅格及手机断点，不增加另一套卡片系统。 */ className='oak-payment-options'
      >
        {services.map((service) => (
          // 四类服务使用同一阅读层级，避免混同为四档价格套餐。
          <article
            /* 服务可独立阅读，标题层级保持在章节标题之下。 */ key={service.id}
          >
            <p className='oak-service-label'>{service.label}</p>
            <div className='oak-plan-heading'>
              <h3>{service.title}</h3>
            </div>
            <p>{service.description}</p>
            <p
              /* 额度示例和账户限制始终可见，不藏在悬浮提示中。 */ className='oak-service-detail'
            >
              {service.detail}
            </p>
            {service.id === 'tokens' ? (
              // 价格由现有目录维护，首页不复制模型报价。
              <Link className='oak-text-link' to='/pricing'>
                {service.action}
                {/* 装饰箭头不加入链接可访问名称，朗读保留服务动作。 */}
                <ArrowRight size={14} aria-hidden='true' />
              </Link>
            ) : (
              // 采购总锚点始终存在，联系方式留空也不会产生悬空链接。
              <a className='oak-text-link' href='#oak-procurement'>
                {service.action}
                {/* 装饰箭头不加入链接可访问名称，朗读保留服务动作。 */}
                <ArrowRight size={14} aria-hidden='true' />
              </a>
            )}
          </article>
        ))}
      </div>
    </section>
  )
}
