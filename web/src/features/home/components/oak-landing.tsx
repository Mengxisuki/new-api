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
import { ArrowDown, ArrowRight, ReceiptText } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Footer } from '@/components/layout/components/footer'
import { PublicLayout } from '@/components/layout/components/public-layout'
import { Button } from '@/components/ui/button'

import { OakComparison } from './oak-comparison'
import { OakProcurement } from './oak-procurement'
import { OakQuickStart } from './oak-quick-start'
import { OakResearchSketch } from './oak-research-sketch'
import { OakUseCases } from './oak-use-cases'

import '@/styles/oak-home.css'

const modelFamilies = [
  // 静态品牌家族不用于请求路由或计费选择
  'OpenAI',
  'Claude',
  'Gemini',
  'DeepSeek',
  'Qwen',
  'Llama',
]

// 默认官网的组合入口；自定义首页内容仍由 Home 决定是否接管。
// 此组件只承载科研团队官网，不负责账户或采购业务的提交。
// 认证状态由父级传入，主按钮只据此选择现有站内路由。
// 真假认证、注册开放与账户权限仍由目标路由和服务端判断。
// 页眉和页脚复用公共布局，保留当前项目的配置和归属信息。
// 不在首页另起一套导航、主题切换或登录状态管理。
// 局部 oak-home 样式提供纸张色与暗色变量，不改控制台全局主题。
// 各节锚点被首屏和采购链接引用，改名时须同步所有入口。
// 移动端布局由 oak-home.css 接管，不在渲染中判断屏幕宽度。
// 模型品牌仅表示兼容生态，实时可用模型以定价目录为准。
// 所有非品牌文案订阅当前语言，切换语言后重新渲染。
// 官网保持旧分支的内容顺序，以恢复既有页面而非重新设计。
// 章节组件各自维护数据和交互；此处只决定页面组合关系。
// 不请求模型服务，也不在营销页面生成账户密钥。
// 首屏和收尾提供相同的账户入口，修改时应保持目标一致。
// 按钮通过 Link 的 render 插槽组合，保留客户端路由导航。
// 采购次按钮使用页内锚点，访客无需登录即可阅读采购说明。
// 开票提示定位到采购节内部，目标必须在同一默认页面存在。
// 模型目录链接继续使用现有定价页，不维护另一份价格列表。
// 装饰树图是本地 SVG，页面无新增外部图片加载依赖。
// 科研场景章节保持在对比表之前，先展示用法再解释平台关系。
// 采购章节复用公开状态，不能在组合入口读取管理接口。
// 接入章节由当前浏览器来源构造地址，避免照搬旧部署域名。
// 收尾入口只负责导航，不在此预创建用户或触发购买。
// 页脚仍接收项目已有法律链接及自定义页脚设置。
// 只有网站默认内容被替换，HTML 和 iframe 等分支不属于此组件。
export function OakLanding(props: { isAuthenticated: boolean }) {
  const { t } = useTranslation()
  return (
    <div /* 局部主题变量边界，避免泄漏到控制台 */ className='oak-home'>
      <PublicLayout
        /* 页面自身提供主内容地标，关闭默认容器 */ showMainContainer={false}
      >
        <main /* 主内容唯一地标，章节都属于此页面 */ id='oak-main'>
          <section
            /* 首屏名称关联可见主标题 */ className='oak-hero'
            aria-labelledby='oak-title'
          >
            <div
              /* 统一内容宽度并承接首屏栅格 */ className='oak-container oak-hero-inner'
            >
              <div /* 文案列与装饰插画独立排列 */ className='oak-hero-copy'>
                <p
                  /* 保留既有品牌标记，后半段随语言切换 */ className='oak-eyebrow'
                >
                  OAK / {t('RESEARCH INFRASTRUCTURE')}
                </p>
                <p /* 受众说明不挤占唯一主标题 */ className='oak-audience'>
                  {t('Built for laboratories and research groups')}
                </p>
                <h1 /* 页面唯一一级标题，同时供区域命名 */ id='oak-title'>
                  {t('AI infrastructure,')}
                  <br /* 保留旧首页标题的显式换行 */ />
                  <span /* 强调色只覆盖标题第二行 */>
                    {t('built for research teams.')}
                  </span>
                </h1>
                <p
                  /* 描述模型接入与团队管理的范围 */ className='oak-hero-description'
                >
                  {t(
                    'Connect Claude Code, Codex and research scripts to a shared model gateway. Manage project quotas and usage, support sustained coding work, and simplify procurement for your lab.'
                  )}
                </p>
                <div
                  /* 主要转化与采购入口由同一响应式动作区承载 */ className='oak-actions'
                >
                  <Button /* 复用按钮外观，render 保留链接语义 */
                    className='oak-button oak-button-primary' // 主动作外观使用首页局部按钮样式
                    render={
                      <Link /* 登录态仅决定导航目的地，不执行权限判断 */
                        to={props.isAuthenticated ? '/dashboard' : '/sign-up'} // 导航目标由父级认证状态选择
                      />
                    }
                  >
                    {props.isAuthenticated
                      ? t('Go to Dashboard')
                      : t('Get Started')}
                    <ArrowRight
                      /* 装饰箭头不加入按钮的朗读名称 */ aria-hidden='true'
                    />
                  </Button>
                  <Button
                    variant='outline' // 采购次动作保留共享按钮的轮廓变体
                    className='oak-button oak-button-secondary' // 次动作与主转化维持视觉层级
                    render={<a href='#oak-procurement' />}
                  >
                    {t('Lab procurement and invoicing')}
                  </Button>
                </div>
                <a
                  /* 开票说明定位到采购节内的稳定锚点 */ className='oak-invoice-callout'
                  href='#oak-invoicing'
                >
                  <ReceiptText
                    /* 票据图形仅辅助识别，不代替文案 */ size={16}
                    strokeWidth={1.5}
                    aria-hidden='true'
                  />
                  {t('Invoice support for research-group purchases')}
                  <ArrowRight
                    /* 保持开票链接的可访问名称来自正文 */ size={14}
                    aria-hidden='true'
                  />
                </a>
              </div>
              <OakResearchSketch /* 插画自包含且无外部图片请求 */ />
              <div
                /* 首屏底线在窄屏允许自然折行 */ className='oak-hero-baseline'
              >
                <span>
                  {t('Model access. Project accounting. Research continuity.')}
                </span>
                <a
                  /* 使用原生锚点保留键盘和浏览器定位行为 */ href='#oak-use-cases'
                >
                  {t('See practical use cases')}
                  <ArrowDown
                    /* 方向图标只提示下方还有内容 */ size={14}
                    aria-hidden='true'
                  />
                </a>
              </div>
            </div>
          </section>
          <section /* 生态区单独命名，方便辅助技术定位 */
            className='oak-ecosystem' // 生态区域样式不影响动态定价目录
            aria-label={t('Compatible model ecosystem')} // 区域名称随当前语言更新
          >
            <div
              /* 品牌列表与定价入口共享内容网格 */ className='oak-container oak-ecosystem-inner'
            >
              <p /* 模型家族展示不等于实时可用性承诺 */>
                {t('Compatible model ecosystem')}
              </p>
              <div>
                {modelFamilies.map((name) => (
                  <span /* 品牌名是稳定标识，不作为翻译键 */ key={name}>
                    {name}
                  </span>
                ))}
              </div>
              <Link
                /* 实时价格复用现有目录路由 */ to='/pricing'
                className='oak-text-link'
              >
                {t('Explore models and pricing')}
                <ArrowRight size={14} aria-hidden='true' />
              </Link>
            </div>
          </section>
          <OakUseCases /* 使用场景先说明科研任务与客户端边界 */ />
          <OakComparison /* 比较区解释共享网关的实际差异 */ />
          <OakProcurement /* 采购说明包含开票清单与可配置联系信息 */ />
          <OakQuickStart
            /* 接入示例沿用同一认证状态，不自行取会话 */ isAuthenticated={
              props.isAuthenticated
            }
          />
          <section
            className='oak-closing oak-container' // 收尾沿用全页内容宽度
            aria-labelledby='oak-closing-title' // 收尾区域名称引用可见标题
          >
            <div>
              <p className='oak-eyebrow'>{t('BUILT FOR THE WORK AHEAD')}</p>
              <h2 /* 收尾区域名称引用可见标题 */ id='oak-closing-title'>
                {t('Give your research a dependable foundation.')}
              </h2>
              <p>
                {t(
                  'Start with a project, understand its usage, then plan access for the whole team.'
                )}
              </p>
            </div>
            <div className='oak-actions'>
              <Button
                className='oak-button oak-button-primary' // 主动作外观使用首页局部按钮样式
                render={
                  <Link
                    to={props.isAuthenticated ? '/dashboard' : '/sign-up'} // 导航目标由父级认证状态选择
                  />
                }
              >
                {props.isAuthenticated
                  ? t('Go to Dashboard')
                  : t('Get Started')}
                <ArrowRight aria-hidden='true' />
              </Button>
              <a
                /* 收尾采购入口回到同一采购说明 */ href='#oak-procurement'
                className='oak-text-link'
              >
                {t('Plan a lab purchase')}
                <ArrowRight size={14} aria-hidden='true' />
              </a>
            </div>
          </section>
        </main>
        <Footer /* 使用现有页脚，仅应用首页局部外观 */ className='oak-footer' />
      </PublicLayout>
    </div>
  )
}
