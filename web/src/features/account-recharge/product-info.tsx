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
import { useTranslation } from 'react-i18next'

import { getLobeIcon } from '@/lib/lobe-icon'

import type { Product } from './api'

// 商品页、结账和订单详情共享同一信息层级。
// 这里只接收展示字段，不接触金额或付款能力。
// 订单调用方传入冻结快照，不能回查当前商品补值。
// 图标沿用模型广场的本地图标集合与默认占位。
// 不支持远程图片，因此不会在订单中请求第三方资源。
// 简介说明权益，购买说明解释条件，两者不得合并。
// 空字段不虚构承诺，旧套餐仍可仅展示原购买说明。
// 周期和时效采用文字标签，避免仅靠位置辨认。
// 长词允许换行，多行描述保留管理员编排。
// 不解析 HTML，所有后台文案通过 React 转义。
// 卡片收起长说明，用户可在购买前完整展开。
// 详情默认完整展示，供核对已买商品的具体条款。
// 折叠状态仅影响阅读，不丢弃或截断正文。
// 标题使用语义化层级，图标作为装饰不重复朗读。
// 服务商来自独立配置，不从图标名推断品牌。
// 单列结构可嵌入两列弹窗及不同宽度的商品网格。
// 此组件不显示管理员反馈，反馈属于订单履约区域。
export function RechargeProductInfo(props: {
  // Pick 约束卡片只依赖套餐展示信息。
  // 商品管理的余额开关及价格不是这里的输入契约。
  // 订单对象结构兼容，无须复制映射另一份展示对象。
  product: Pick<
    Product,
    | 'title'
    | 'summary'
    | 'description'
    | 'provider'
    | 'period'
    | 'delivery_time'
    | 'icon'
  >
  // compact 仅控制购买说明的初始展开状态。
  // 卡片价格与按钮由父级负责，说明展开不挤掉购买入口。
  // 此标志不参与数据提交，读页面不会修改商品状态。
  compact?: boolean
}) {
  // 独立订阅语言变化，避免父页面翻译但标签仍停留旧语言。
  // 商品正文保持管理员原文，不把业务数据当作翻译键。
  const { t } = useTranslation()
  return (
    <div className='min-w-0 space-y-4 [overflow-wrap:anywhere]'>
      {/* 图标固定占位宽度，长标题不会把图标压扁。
          标题允许自然换行，完整套餐名称始终可读。
          共享加载器负责异步图标，未知键保持可识别的占位。 */}
      <div className='flex items-start gap-3'>
        <div
          aria-hidden
          className='bg-muted/50 flex size-11 shrink-0 items-center justify-center rounded-xl'
        >
          {getLobeIcon(props.product.icon, 28)}
        </div>
        <div className='min-w-0'>
          <h3 className='text-base font-semibold'>{props.product.title}</h3>
          {props.product.provider && (
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Service provider')}: {props.product.provider}
            </p>
          )}
        </div>
      </div>
      {/* 简介不采用行数截断，避免隐藏重要权益限定。
          管理员输入受长度上限约束，展示层不重复裁剪。
          老商品为空时不占空间，不将购买说明复制成简介。 */}
      {props.product.summary && (
        <div className='space-y-1.5'>
          <h4 className='text-muted-foreground text-xs font-medium'>
            {t('Plan summary')}
          </h4>
          <p className='text-sm whitespace-pre-wrap'>{props.product.summary}</p>
        </div>
      )}
      {/* 结构化属性使用定义列表，标签和值在语义上关联。
          时效只有一项时仍保持同一栅格，不改变阅读顺序。
          周期仅为销售文案，不推算到期日或承诺自动续费。
          人工时效不转成倒计时，避免误导用户为确定完成期限。 */}
      {(props.product.period || props.product.delivery_time) && (
        <dl className='bg-muted/40 grid grid-cols-2 gap-3 rounded-lg p-3 text-sm'>
          {props.product.period && (
            <div>
              <dt className='text-muted-foreground text-xs'>
                {t('Plan period')}
              </dt>
              <dd className='mt-1 font-medium'>{props.product.period}</dd>
            </div>
          )}
          {props.product.delivery_time && (
            <div>
              <dt className='text-muted-foreground text-xs'>
                {t('Estimated processing time')}
              </dt>
              <dd className='mt-1 font-medium'>
                {props.product.delivery_time}
              </dd>
            </div>
          )}
        </dl>
      )}
      {/* 原生 details 支持键盘展开，避免再维护开关状态。
          详情初始展开便于核对，用户仍可收起长条款。
          展开只影响当前卡片，不同步到其他套餐。
          说明属于商品快照，订单留言由外层独立显示。
          保留换行但不解释富文本链接，防止管理员正文插入交互。
          空说明不渲染空折叠项，避免用户反复点击无内容区域。 */}
      {props.product.description && (
        <details
          open={props.compact ? undefined : true}
          className='border-t pt-3 text-sm'
        >
          <summary className='cursor-pointer font-medium'>
            {t('Purchase instructions')}
          </summary>
          <p className='text-muted-foreground mt-2 whitespace-pre-wrap'>
            {props.product.description}
          </p>
        </details>
      )}
    </div>
  )
}
