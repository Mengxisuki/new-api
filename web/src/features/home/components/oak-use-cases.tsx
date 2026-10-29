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

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

// 使用场景说明科研任务如何使用现有客户端与模型 API。
// 本组件不负责启动 agent、实验任务或持续运行的后台作业。
// 每个场景包含标签、工具、任务示例、能力边界和阶段说明。
// 场景 ID 是 Tabs 的稳定协议值，不随界面语言变化。
// 所有说明文字在渲染时翻译，语言切换不会丢失当前场景选择。
// 品牌和协议名保留原样，不能把工具名称作为实际可用模型列表。
// 示例任务只用于解释用途，不向服务端发送提示词。
// 连续任务中的执行、重试和检查点仍由调用方客户端负责。
// API 平台提供模型接入和用量核算，不能被描述为本机任务执行器。
// 官方产品订阅和客户端可用特性由场景备注明确区分。
// 公共 Tabs 维护键盘交互、焦点和可访问选中状态。
// 每个面板的数据来自同一个场景对象，避免标签和内容错配。
// 阶段流仅作视觉辅助，完整含义已由正文给出。
// 宽窄屏排列交给 oak-home.css，不基于设备检测切换内容。
// 场景是官网说明数据，不作为后台任务模板或自动运行指令。
// 编程客户端场景说明接入关系，不替代客户端自身的配置文档。
// 持续任务场景将重试与检查点责任保留在用户的客户端或执行器。
// 科研流水线场景说明项目隔离，不在官网读取真实数据集或用量。
// 每个场景的标题、任务示例和流程标签来自同一个数据项。
// 稳定 id 同时连接标签页触发器和面板，不能改用翻译文案作值。
// 流程标签是装饰性的概括，可访问内容保留在标题与说明中。
// 场景切换无需后台请求，真实模型支持范围仍以可用模型目录为准。
export function OakUseCases() {
  const { t } = useTranslation()
  const scenarios = [
    // 场景内容内聚于单个对象数组，标签与面板共同遍历
    {
      id: 'agents', // 稳定选择值对应编码助手场景
      number: '01',
      label: t('Coding Agents'),
      tools: 'Claude Code / Codex', // 工具名沿用历史官网，官方订阅另行说明
      title: t('Keep the coding client. Connect your research budget.'),
      description: t(
        'Use Claude Code to explore a paper repository and adapt its training scripts, or use Codex to refactor a data pipeline and add tests. Connect through a compatible API endpoint and platform key; model calls are billed to your platform balance.'
      ),
      example: t(
        'Reproduce this paper: inspect the repository, prepare the environment, adapt the training script to our dataset, and add a minimal validation run.'
      ),
      note: t(
        'Client setup and available features depend on the selected model and protocol. Official client subscriptions are separate.'
      ),
      stages: [t('Repository'), t('Implementation'), t('Validation')],
    },
    {
      id: 'continuous', // 持续任务场景只描述模型接入和费用跟踪
      number: '02',
      label: t('Long-running code tasks'),
      tools: 'Agent loop / Evaluation / CI', // 执行循环由外部 runner 承担，不由官网启动
      title: t('Support the iterations behind a finished experiment.'),
      description: t(
        'Run repeated code-test-fix cycles, maintain evaluation scripts, or process a series of experiment configurations. Assign a project key and quota, then track token usage and costs as the work progresses.'
      ),
      example: t(
        'Run the evaluation suite, analyze failing cases, revise the implementation, and repeat until the tests pass. Save intermediate results at each stage.'
      ),
      note: t(
        'Your client or job runner executes tasks, handles retries and saves checkpoints. The platform provides model access and usage accounting.'
      ),
      stages: [t('Code'), t('Test'), t('Revise')],
    },
    {
      id: 'pipelines', // 研究流水线示例与编码任务使用独立选择值
      number: '03',
      label: t('Research pipelines'),
      tools: 'Python / Jupyter / API', // 工具示例不限制实际用户使用的兼容语言
      title: t('Bring multiple models into the same experiment.'),
      description: t(
        'Connect notebooks and scripts for literature processing, structured extraction and model evaluation. Use separate keys for each project so model experiments remain visible in usage records.'
      ),
      example: t(
        'Extract methods and datasets from a collection of paper abstracts, save structured results, and compare outputs from two models on the same evaluation set.'
      ),
      note: t(
        'Choose models from the live catalog and confirm their capabilities before running your workload.'
      ),
      stages: [t('Dataset'), t('Model API'), t('Results')],
    },
  ]
  return (
    <section /* 使用场景锚点承接首屏向下浏览入口 */
      className='oak-section oak-container' // 保留统一内容宽度及章节留白
      id='oak-use-cases' // 首屏向下浏览链接的定位目标
      aria-labelledby='oak-use-cases-title' // 区域标题与可见文案保持同源
    >
      <div /* 标题与用途说明构成同一区域头部 */ className='oak-section-heading'>
        <div>
          <p /* 编号表示默认官网第一内容节 */ className='oak-eyebrow'>
            02 / {t('RESEARCH IN PRACTICE')}
          </p>
          <h2 /* 可见标题用于此节可访问名称 */ id='oak-use-cases-title'>
            {t('From coding assistance to sustained research work.')}
          </h2>
        </div>
        <p>
          {t(
            'Use the tools your team already knows, with a shared layer for model access and spending.'
          )}
        </p>
      </div>
      <Tabs
        /* 默认先展示既有编码客户端场景 */ defaultValue='agents'
        className='oak-scenarios'
      >
        <TabsList /* 标签组使用项目组件实现完整键盘行为 */
          variant='line' // 复用公共标签页提供的线条变体
          className='oak-scenario-tabs' // 场景标签在窄屏的排布交给局部样式
          aria-label={t('Research use cases')} // 整个标签组提供可翻译的用途名称
        >
          {scenarios.map((item) => (
            <TabsTrigger
              /* 标签绑定稳定场景 ID，避免翻译影响选择 */ value={item.id}
              key={item.id}
            >
              <span /* 序号帮助视觉定位，不参与选择值计算 */>
                {item.number}
              </span>
              {item.label}
            </TabsTrigger>
          ))}
        </TabsList>
        {scenarios.map((item) => (
          <TabsContent /* 面板 value 与标签使用同一个场景 ID */
            value={item.id} // 面板值必须与对应触发器完全一致
            key={item.id} // 语言切换后仍沿用同一场景标识
            className='oak-scenario-panel' // 面板布局与场景数据结构保持独立
          >
            <div
              /* 描述与任务示例分列，窄屏保留阅读顺序 */ className='oak-scenario-copy'
            >
              <p
                /* 工具清单只是适用示例，不读取本机安装情况 */ className='oak-tool-label'
              >
                {item.tools}
              </p>
              <h3 /* 每个场景具有自己的可见标题 */>{item.title}</h3>
              <p /* 场景能力说明与客户端执行边界共同阅读 */>
                {item.description}
              </p>
              <p
                /* 备注明确避免把客户端能力误当平台承诺 */ className='oak-scenario-note'
              >
                {item.note}
              </p>
            </div>
            <div
              /* 任务示例与产品能力说明保持分区 */ className='oak-task-example'
            >
              <p /* 标明以下文本只是任务示例 */ className='oak-eyebrow'>
                {t('EXAMPLE TASK')}
              </p>
              <blockquote /* 示例作为引用文本展示，不作为输入框执行 */>
                {item.example}
              </blockquote>
              <div
                /* 阶段图只作视觉提示，不重复朗读正文 */ className='oak-task-flow'
                aria-hidden='true'
              >
                {item.stages.map((stage) => (
                  <span
                    /* 阶段来自当前场景数据，不使用跨场景共享状态 */ key={stage}
                  >
                    {stage}
                  </span>
                ))}
              </div>
            </div>
          </TabsContent>
        ))}
      </Tabs>
    </section>
  )
}
