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
import { ArrowUpRight } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { HOME_DOCS_URL } from '@/lib/constants'

// 接入指南提供可复制示例，不自动发起任何模型请求。
// 示例基址来自当前站点，适配不同部署域名及本地预览。
// 统一使用站点的 /v1 接入路径，不在示例中绑定旧环境域名。
// API_KEY 与 MODEL_ID 由用户运行环境提供，页面不读取真实密钥。
// Python 中通过 JSON 字符串转义写入基址，保证引号合法。
// cURL 保留 shell 环境变量与换行，复制后由用户自行执行。
// 示例文本只按代码块渲染，不使用 HTML 注入或执行脚本。
// 当前代码语言由 Tabs 控制，复制按钮始终使用同一选中状态。
// 示例对象的键与 TabsTrigger 的 value 必须一一对应。
// 复制反馈、剪贴板兼容和失败通知统一交给 CopyButton。
// 步骤导航复用既有账户、密钥和文档入口，不另建开户流程。
// 认证状态只改变首步入口，访问密钥页仍受应用现有路由守卫保护。
// 外部文档在新标签打开，隔离 opener 并避免传递 referrer。
// 代码区域保留键盘焦点，长代码由样式控制水平滚动。
// 文档地址沿用旧官网常量，避免在不同示例位置重复硬编码。
// 按钮和标签页均复用项目组件，保留既有键盘与可访问性行为。
// Python 示例从环境变量读取密钥和模型，不嵌入任何真实凭据。
// 示例中的平台地址随当前部署域名变化，复制后无需沿用旧域名。
// 选项卡只在当前页面维护选择，不持久化为全站语言偏好。
// 剪贴板内容直接使用当前示例，防止可见代码和复制内容分离。
// 文档按钮只打开已有接入文档，不在页面内尝试验证用户密钥。
export function OakQuickStart(props: { isAuthenticated: boolean }) {
  const { t } = useTranslation()
  const [language, setLanguage] = useState('python') // 初始示例与默认步骤中的 Python 接入一致
  const endpoint = `${window.location.origin}/v1` // 只取浏览器 origin，不混入当前路由查询参数
  const examples: Record<string, string> = {
    // 示例保留原始文本，剪贴板与代码面板使用同一来源
    python: `import os\nfrom openai import OpenAI\n\nclient = OpenAI(\n    base_url=${JSON.stringify(endpoint)},\n    api_key=os.environ["API_KEY"],\n)\n\nresponse = client.chat.completions.create(\n    model=os.environ["MODEL_ID"],\n    messages=[{"role": "user", "content": "Hello!"}],\n)\nprint(response.choices[0].message.content)`,
    curl: String.raw`curl "${endpoint}/chat/completions" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$MODEL_ID\",
    \"messages\": [
      {\"role\": \"user\", \"content\": \"Hello!\"}
    ]
  }"`,
  }
  const code = examples[language] // 复制目标由标签页当前值决定，避免复制到隐藏示例

  return (
    <section /* 接入区域使用独立标题作为可访问名称 */
      className='oak-start-section' // 接入章节局部背景与其余官网内容区分
      id='oak-quick-start' // 保留可直达接入示例的稳定锚点
      aria-labelledby='oak-start-title' // 区域名称来自可见接入标题
    >
      <div
        /* 步骤和代码在同一栅格内响应宽度 */ className='oak-container oak-start-grid'
      >
        <div /* 操作说明与示例面板的职责分离 */ className='oak-start-copy'>
          <p /* 章节顺序与官网前三节衔接 */ className='oak-eyebrow'>
            04 / {t('INTEGRATION')}
          </p>
          <h2 /* 标题同时命名此接入区域 */ id='oak-start-title'>
            {t('Connect your existing tools.')}
          </h2>
          <p
            /* 说明支持已有脚本，不承诺自动配置本机环境 */ className='oak-start-description'
          >
            {t(
              'From a Jupyter notebook to an automated experiment, start with a familiar API.'
            )}
          </p>
          <ol
            /* 开户、创建密钥、发出请求存在操作先后顺序 */ className='oak-steps'
          >
            <li>
              <span>01</span>
              <div>
                <h3>
                  <Link
                    /* 已有用户进入控制台，访客进入现有注册路由 */ to={
                      props.isAuthenticated ? '/dashboard' : '/sign-up'
                    }
                  >
                    {t('Create your account')}
                    <ArrowUpRight
                      /* 方向图标不加入步骤链接名称 */ size={15}
                      aria-hidden='true'
                    />
                  </Link>
                </h3>
                <p>{t('Your starting point for models, keys and usage.')}</p>
              </div>
            </li>
            <li>
              <span>02</span>
              <div>
                <h3>
                  <Link /* 密钥管理继续复用当前业务模块 */ to='/keys'>
                    {t('Create a key for your project')}
                    <ArrowUpRight size={15} aria-hidden='true' />
                  </Link>
                </h3>
                <p>{t('Choose a model and set an appropriate key quota.')}</p>
              </div>
            </li>
            <li>
              <span>03</span>
              <div>
                <h3>
                  <a /* 文档入口只导航，不代替执行示例 */
                    href={HOME_DOCS_URL} // 沿用旧官网文档入口的唯一常量
                    target='_blank'
                    rel='noopener noreferrer'
                  >
                    {t('Make your first request')}
                    <ArrowUpRight size={15} aria-hidden='true' />
                  </a>
                </h3>
                <p>
                  {t('Set your API key and model ID, then run the example.')}
                </p>
              </div>
            </li>
          </ol>
        </div>
        <div /* 代码窗口外观由官网局部样式管理 */ className='oak-code-window'>
          <div
            /* 窗口标题属于展示信息，不用于代码复制 */ className='oak-code-heading'
          >
            <span>
              <span
                /* 装饰圆点不绑定连接状态或服务健康含义 */ className='oak-ink-dot'
              />
              research / quickstart
            </span>
            <span /* 明确区分示例和已实际执行的请求 */>{t('Example')}</span>
          </div>
          <Tabs /* 公共标签页负责焦点、选中态与键盘切换 */
            value={language} // 选中语言同时驱动可见示例与复制内容
            onValueChange={(value) => setLanguage(String(value))}
          >
            <div
              /* 语言切换与复制操作共享工具栏 */ className='oak-code-toolbar'
            >
              <TabsList
                /* 标签组名称随当前语言更新 */ variant='line'
                aria-label={t('Code language')}
              >
                <TabsTrigger
                  /* 协议值对应 examples.python，不参与翻译 */ value='python'
                >
                  Python
                </TabsTrigger>
                <TabsTrigger
                  /* 协议值对应 examples.curl，品牌文本保持原样 */ value='curl'
                >
                  cURL
                </TabsTrigger>
              </TabsList>
              <CopyButton /* 复制选中代码，复用公共失败反馈 */
                value={code} // 复制当前语言代码，不读取隐藏面板 DOM
                size='sm' // 复制按钮在代码工具栏内保持紧凑
                className='oak-copy-button' // 首页只调整复制按钮外观，不重写交互
                aria-label={t('Copy code')} // 辅助技术能够明确复制操作的对象
              >
                {t('Copy')}
              </CopyButton>
            </div>
            {Object.entries(examples).map(([key, value]) => (
              <TabsContent
                /* 同一对象键绑定标签内容和选择值 */ value={key}
                key={key}
              >
                <pre /* 保留空白和换行，不把代码当富文本解析 */
                  className='oak-code' // 保持代码空白并通过局部样式处理溢出
                  tabIndex={0} // 键盘可聚焦长代码并浏览溢出内容
                  aria-label={t('API request example')} // 代码区域有明确用途名称
                >
                  <code /* React 默认文本转义保护展示边界 */>{value}</code>
                </pre>
              </TabsContent>
            ))}
          </Tabs>
          <p /* 提醒模型 ID 由账户真实可用目录确定 */ className='oak-code-note'>
            {t(
              'Use a model ID available to your account. Examples do not send a request.'
            )}
          </p>
        </div>
      </div>
    </section>
  )
}
