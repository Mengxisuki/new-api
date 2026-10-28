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
import { ArrowRight, ReceiptText } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'

import { OakContact } from './oak-contact'

// 采购节组合方案说明、开票准备清单和公开联系卡片。
// 内容说明现有流程，不在官网直接创建订单或调用付款接口。
// 按量使用与定制方案分别展示，不能把待沟通方案标成已可购买。
// 实际模型价格仍从既有定价页查询，不在此复制价格表。
// 开票清单只有准备项，不收集税号、邮箱或项目敏感信息。
// 可见清单与剪贴板内容使用同一数组，避免两份规则逐渐分叉。
// 清单随当前语言构造，复制结果与用户当时看到的文字一致。
// 复制行为交给公共 CopyButton，组件不重复维护成功与失败状态。
// 采购联系方式从公开配置显示，具体字段为空时由子组件处理。
// 采购支持区布局由 CSS 负责，保持旧分支平板和手机断点。
// 采购、开票与联系锚点相互独立，供页面不同入口准确定位。
// 此处链接只做导航，不替代目标路由的认证与权限校验。
export function OakProcurement() {
  const { t } = useTranslation()
  const invoiceItems = [
    // 唯一清单数据源，同时生成 DOM 和剪贴板文本
    t('Invoice title and taxpayer identification number'), // 只提示需准备的税务资料，不在官网存储
    t('Purchase amount and project reference'), // 金额与项目引用由实际采购沟通确认
    t('Invoice delivery email and institutional requirements'), // 交付渠道与机构要求作为采购前准备项
  ]
  const checklist = `${t('Lab procurement and invoicing')}\n\n${invoiceItems.map((item) => `- ${item}`).join('\n')}` // 纯文本换行适合粘贴到采购沟通工具
  return (
    <section /* 采购总锚点承接首屏和收尾的采购入口 */
      className='oak-procurement oak-container oak-section' // 沿用官网宽度和采购节局部排版
      id='oak-procurement' // 主采购入口定位目标，改名需同步首页链接
      aria-labelledby='oak-procurement-title' // 区域名称直接引用当前可见标题
    >
      <div /* 章节标题与采购范围说明保持分组 */ className='oak-section-heading'>
        <div>
          <p /* 编号与官网内容顺序对应 */ className='oak-eyebrow'>
            03 / {t('LAB PROCUREMENT')}
          </p>
          <h2 /* 可见标题作为采购节可访问名称 */ id='oak-procurement-title'>
            {t('Plan for the lab. Account for the funding.')}
          </h2>
        </div>
        <p>
          {t(
            'We focus on research-group procurement, with payment options planned around different team sizes and usage patterns.'
          )}
        </p>
      </div>
      <div
        /* 两类方案在宽屏比较，窄屏纵向排列 */ className='oak-payment-options'
      >
        <article /* 按量方案只指向现有模型价格目录 */>
          <div className='oak-plan-heading'>
            <h3 /* 方案标题与实际开通状态分开表达 */>{t('Pay as you go')}</h3>
            <span /* 现有可用能力与待沟通方案状态区分 */ className='oak-status'>
              {t('Available now')}
            </span>
          </div>
          <p>
            {t(
              'Top up your platform balance and pay for actual model usage. Suitable for pilot projects, variable workloads and teams that want to understand costs before scaling.'
            )}
          </p>
          <Link
            /* 价格查询复用当前应用的定价路由 */ className='oak-text-link'
            to='/pricing'
          >
            {t('Explore models and pricing')}
            <ArrowRight
              /* 装饰图标不改变链接的朗读名称 */ size={14}
              aria-hidden='true'
            />
          </Link>
        </article>
        <article>
          <div className='oak-plan-heading'>
            <h3 /* 保留需要另行沟通的采购方案范围 */>
              {t('Subscriptions and other plans')}
            </h3>
            <span
              /* 定制状态不能被误读为可立即购买 */ className='oak-status oak-status-contact'
            >
              {t('Contact for a tailored plan')}
            </span>
          </div>
          <p>
            {t(
              'Pay-as-you-go is available now. For subscriptions and other procurement arrangements, contact us to discuss a tailored plan based on your team size, expected usage and institutional requirements.'
            )}
          </p>
          <a /* 联系入口使用原生锚点，不触发订单请求 */
            className='oak-text-link oak-plan-contact-link' // 定制方案引导至联系卡片
            href='#oak-procurement-contact' // 联系锚点由 OakContact 提供
          >
            {t('Discuss a tailored plan')}
            <ArrowRight size={14} aria-hidden='true' />
          </a>
        </article>
      </div>
      <div
        /* 开票与联系方式共享断点重排容器 */ className='oak-procurement-support'
      >
        <div
          /* 开票锚点与首屏票据链接保持一致 */ className='oak-invoice-panel'
          id='oak-invoicing'
        >
          <div
            /* 说明区与准备清单在语义上各自独立 */ className='oak-invoice-intro'
          >
            <ReceiptText
              /* 票据图形不替代开票说明 */ size={29}
              strokeWidth={1.35}
              aria-hidden='true'
            />
            <p className='oak-eyebrow'>{t('INVOICING SUPPORT')}</p>
            <h3 /* 保留机构报销要求需提前确认的上下文 */>
              {t('Invoices for your research-group purchases.')}
            </h3>
            <p>
              {t(
                'We can coordinate invoicing for lab purchases to support research-expense reimbursement. Confirm your institution’s requirements and invoice details before purchasing.'
              )}
            </p>
          </div>
          <div
            /* 可见清单与复制按钮相邻，便于核对 */ className='oak-invoice-checklist'
          >
            <p>{t('Prepare these details for your purchase')}</p>
            <ul /* 准备项无强制完成顺序，使用无序列表 */>
              {invoiceItems.map((item) => (
                <li /* 翻译后的清单项同时用于显示与复制 */ key={item}>
                  {item}
                </li>
              ))}
            </ul>
            <CopyButton /* 复制整份准备项，复用公共交互反馈 */
              size='default' // 清单复制作为采购节主要实用操作
              value={checklist} // 复制源复用可见准备项生成的纯文本
              variant='outline' // 公共按钮变体与旧官网外观保持一致
              className='oak-button oak-invoice-copy' // 只覆盖清单按钮的首页外观
            >
              {t('Copy procurement checklist')}
            </CopyButton>
          </div>
        </div>
        <OakContact /* 联系卡片独立负责空配置与图片失败显示 */ />
      </div>
    </section>
  )
}
