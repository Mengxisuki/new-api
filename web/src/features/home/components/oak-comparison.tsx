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

// 从科研团队视角比较直接接入与共享网关，不查询外部供应商数据。
// 本表是能力解释，实际账号权限和模型可用性以现有目录为准。
// 行数据按关注点、直接接入、平台接入三个位置组成。
// 列顺序必须与表头一致，不能只调整数据却不调整表格语义。
// 翻译在组件渲染时执行，避免把首次加载语言冻结为模块常量。
// 长文案保留在单元格内，由横向滚动容器处理窄屏宽度。
// 不把表格重做成互相分离的卡片，保持逐行比较关系。
// 区域可聚焦，使键盘用户也能进入溢出的比较内容。
// 列标题与行标题使用原生表格关联，避免手工维护重复 ARIA 映射。
// 比较后的说明明确官方订阅与平台模型调用的关系。
// 保留历史首页的描述边界，不将支持接入写成包含官方会员。
// 该区没有表单、购买动作或服务端状态变更。
export function OakComparison() {
  const { t } = useTranslation()
  const rows = [
    // 使用显式行数据，调整内容时直接检查列对应关系
    [
      t('Model access'), // 模型接入比较配置入口，不宣称实时模型数量
      t('Access and configure each provider separately.'),
      t(
        'Access compatible models through one gateway and a shared platform account.'
      ),
    ],
    [
      t('Usage and budgets'), // 账务比较围绕平台已存在的密钥和配额能力
      t('Usage records and balances are managed on each provider platform.'),
      t(
        'Track platform usage centrally and separate projects with API keys and quotas.'
      ),
    ],
    [
      t('Client relationship'), // 客户端兼容与官方产品订阅分开说明
      t(
        'Use the provider APIs or its official product subscription, according to its terms.'
      ),
      t(
        'Keep compatible clients such as Claude Code and Codex; pay here for the model API calls routed through this platform.'
      ),
    ],
    [
      t('Lab procurement'), // 采购差异只说明协调流程，不自动发起订单
      t('Procurement and invoicing follow each provider’s processes.'),
      t(
        'Coordinate research-group purchases and invoice requirements with one platform.'
      ),
    ],
  ]
  return (
    <section /* 比较节使用独立标题提供区域名称 */
      className='oak-comparison-section' // 比较区背景只作用于当前章节
      aria-labelledby='oak-comparison-title' // 名称来自用户可见的章节标题
    >
      <div /* 沿用官网内容宽度与节间距 */ className='oak-container oak-section'>
        <div
          /* 标题和解释段落由 CSS 在窄屏重排 */ className='oak-section-heading'
        >
          <div>
            <p /* 章节编号与完整页面顺序对应 */ className='oak-eyebrow'>
              03 / {t('WHY A SHARED GATEWAY')}
            </p>
            <h2 /* 可见标题同时作为区域名称 */ id='oak-comparison-title'>
              {t('What changes compared with going direct?')}
            </h2>
          </div>
          <p>
            {t(
              'The difference is how your team accesses, manages and purchases model usage.'
            )}
          </p>
        </div>
        <div /* 滚动边界包裹整张表，不截断单列 */
          className='oak-comparison-scroll' // 窄屏保留横向溢出浏览能力
          tabIndex={0} // 键盘用户可将焦点移到表格滚动区域
          role='region' // 辅助技术可将滚动内容识别为独立区域
          aria-label={t('Provider access comparison')} // 滚动区域具有明确且可翻译的名称
        >
          <table
            /* 使用原生表格表达跨行列的比较关系 */ className='oak-comparison-table'
          >
            <thead /* 表头只描述比较维度，不参与业务数据映射 */>
              <tr>
                <th /* 第一列名称说明每一行关注的能力 */ scope='col'>
                  {t('What matters')}
                </th>
                <th /* 第二列固定对应直接接入说明 */ scope='col'>
                  {t('Direct provider access')}
                </th>
                <th /* 第三列固定对应平台接入说明 */ scope='col'>
                  {t('Through this platform')}
                </th>
              </tr>
            </thead>
            <tbody /* 所有比较记录共享同一组表头 */>
              {rows.map(([topic, direct, gateway]) => (
                <tr /* 当前语言下的关注点作为行标识 */ key={topic}>
                  <th /* 行标题帮助读屏器关联两列说明 */ scope='row'>
                    {topic}
                  </th>
                  <td /* 直接接入描述保持独立，便于逐行阅读 */>{direct}</td>
                  <td /* 平台描述不混入不可验证的价格或 SLA 承诺 */>
                    {gateway}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p
          /* 订阅边界说明紧跟表格，避免与比较内容分离 */ className='oak-comparison-note'
        >
          {t(
            'This is an independent model API service. ChatGPT, Claude and other official subscriptions are not included. Model availability and client features depend on the selected integration.'
          )}
        </p>
      </div>
    </section>
  )
}
