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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, act, within } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { expect, it } from 'vitest'

import en from '@/i18n/locales/en.json'
import fr from '@/i18n/locales/fr.json'
import ja from '@/i18n/locales/ja.json'
import ru from '@/i18n/locales/ru.json'
import vi from '@/i18n/locales/vi.json'
import zhTW from '@/i18n/locales/zh-TW.json'
import zh from '@/i18n/locales/zh.json'

import { ConnectHostingDialog } from '../components/connect-dialog'

// 挂靠新增词条必须放在 i18next 实际读取的 translation 命名空间。
// 顶层键虽然存在于 JSON，却不会被 useTranslation 找到。
// 七语言共同检查，防止只修中文而让其他语言继续英文回退。
// 这里只保护本模块的结构与核心入口，不枚举无关全站文案。
// en 也必须在同一命名空间，英文回退不能掩盖资源结构错误。
// 简繁体分别加载各自文件，不能用语言映射代替实际词条。
// 菜单标题与授权按钮来自不同组件，保证本次搬迁覆盖整个功能入口。
// 不把测试字典手工补成正确结构，真实文件放错层级时必须失败。
// 词条值的语言正确性由后续中文界面断言检查。
// 空值与缺失都不应成为可发布的挂靠文案。
// 未来添加命名空间应同步显式初始化，不能只在 JSON 顶层追加键。
it('七语言挂靠词条位于实际翻译命名空间', () => {
  for (const resource of [en, zh, zhTW, fr, ja, ru, vi]) {
    expect(Object.keys(resource)).toEqual(['translation']) // 文件存在不等于翻译器实际能读取。
    expect(resource.translation).toHaveProperty('Usage order') // 每 Key 菜单标题也属于漏译修复范围。
    expect(resource.translation).toHaveProperty('Generate authorization link') // 首要操作的翻译缺失会直接阻断用户理解。
    // 额度词条同样需要进入真实命名空间，避免紧凑布局继续混杂英文。
    // 所有七语言都检查，不能仅依赖中文回退掩盖漏译。
    // 自动测试的进度与失败是用户接入结论，不能依赖英文回退。
    // 使用真实七语言资源检查，防止保存成功提示掩盖未翻译的失败信息。
    for (const key of [
      'After authorization, a short model test runs automatically and uses a small amount of provider quota.',
      'Testing account connection…',
      'Connection verified. This account can now be used by your API keys.',
      'Connection test did not pass. This account is excluded from routing. Retry in account details.',
    ]) {
      expect(
        resource.translation[key as keyof typeof resource.translation]
      ).toBeTruthy()
    }
    expect(resource.translation).toHaveProperty('Quota used')
    expect(resource.translation).toHaveProperty('Resets in {{duration}}')
    expect(resource.translation).toHaveProperty('Awaiting quota refresh')
    expect(resource.translation).toHaveProperty('Codex configuration') // 配置入口不能继续显示英文键。
  }
})

// 使用真实中文资源和独立实例，空英文测试资源发现不了命名空间错位。
// 未开始时三步说明均应可读，但不应出现“链接不可用”的失败提示。
// 当前只开放手动流程，不展示授权方式选择或令牌导入。
// 在同一个挂载实例切换语言，验证词条由翻译器实时解析。
// 初始界面没有请求授权的副作用，因此无需伪造后端响应。
// 缓存只服务本用例，测试结束清理，防止影响其他授权会话。
it('中文弹窗显示手动授权三步说明，并可切换语言', async () => {
  // 独立实例避免切换语言影响已有英文交互回归。
  // zhCN 与真实应用语言代码一致，资源对象也保持实际嵌套。
  // URL 文案包含冒号，不能被解析成另一个命名空间。
  // 不禁用英文回退，中文缺失时应通过可见文本断言发现。
  const i18n = createInstance()
  await i18n.use(initReactI18next).init({
    lng: 'zhCN',
    fallbackLng: 'en',
    nsSeparator: false,
    resources: { en, zhCN: zh },
  })
  // 这个场景只打开说明页，不验证安全证明或原厂网络。
  // 重试关闭使意外增加网络调用不会在测试后台持续执行。
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  // 本地打开弹窗不读取用户凭据，不触发实际原厂授权。
  const view = render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <ConnectHostingDialog onClose={() => {}} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  try {
    expect(screen.getByRole('button', { name: '生成授权链接' })).toBeVisible()
    // 三个编号表示实际用户动作，备注不再单独占一个授权步骤。
    // 通过语义节点识别步骤，不依赖蓝色背景或截图像素。
    const steps = screen.getAllByRole('listitem')
    expect(steps).toHaveLength(3)
    expect(steps[0]).toHaveTextContent('点击下方按钮生成授权链接')
    expect(steps[1]).toHaveTextContent('在浏览器中打开链接并完成授权')
    expect(steps[2]).toHaveTextContent('输入授权链接或 Code')
    // 打开前就能阅读下一步指引，但空会话不能提交回调。
    expect(screen.getByLabelText('授权链接或 Code')).toBeDisabled()
    expect(screen.getByRole('button', { name: '完成授权' })).toBeDisabled()
    // 底部完成按钮不能藏在第三步内部，便于长页面滚动后稳定操作。
    expect(
      within(steps[2]).queryByRole('button', { name: '完成授权' })
    ).not.toBeInTheDocument()
    // 参考窗口的具体提示必须真实翻译，不能只检查按钮中文。
    expect(steps[1]).toHaveTextContent(
      '请在新标签页中打开授权链接，登录您的 OpenAI 账户并授权。'
    )
    expect(steps[1]).toHaveTextContent(
      '重要提示：授权后页面可能会加载较长时间，请耐心等待。'
    )
    expect(steps[2]).toHaveTextContent(
      '您可以直接复制整个链接或仅复制 code 参数值，系统会自动识别'
    )
    expect(screen.getByLabelText('授权链接或 Code')).toHaveAttribute(
      'placeholder',
      expect.stringContaining('方式1：复制完整的链接')
    )
    // 没有导入或其他授权方式，选择器不能增加用户操作分支。
    expect(screen.queryByRole('radio')).not.toBeInTheDocument()
    expect(screen.queryByText('Authorization Method')).not.toBeInTheDocument()
    // 未生成不等于失效；不能把正常初始状态渲染成错误。
    expect(screen.queryByText(/授权链接暂不可用/)).not.toBeInTheDocument()
    // 不重新挂载组件，语言切换必须更新当前已打开的弹窗。
    await act(async () => {
      await i18n.changeLanguage('en')
    })
    expect(
      screen.getByRole('button', { name: 'Generate authorization link' })
    ).toBeVisible()
  } finally {
    // 即使断言失败也清理独立实例，保留其他测试的英文环境。
    view.unmount() // 先卸载观察者，再释放请求缓存，避免残留异步更新。
    client.clear()
  }
})
