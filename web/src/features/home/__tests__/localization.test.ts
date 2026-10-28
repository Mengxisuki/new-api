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
import { expect, test } from 'vitest'

import zhCN from '@/i18n/locales/zh.json'

// 只校验官网主体承诺的栏目，避免将无关上游翻译绑入此次同步。
// 键缺失和直接显示英文均意味着中文首页没有完整接入。
// 表中覆盖首屏、目标受众、场景、采购和收尾入口。
// 翻译回归的边界：
// 这里直接读取已提交的中文资源，不依赖浏览器当前语言。
// 这样能够捕获恢复组件却漏同步词条的接线问题。
// 英文测试环境不能自动证明中文用户看见了翻译。
// 不要求所有上游语言一次性达到中文内容的相同程度。
// 也不检查标点、排版或广告措辞，避免文案微调导致无关失败。
// 首屏与底部标题是网站识别的关键内容，因此进入保护集合。
// 科研场景和采购栏目的键分别来自独立组件，不能只检查首屏。
// 新增栏目时应更新这个语义集合，而非快照整份语言包。
// 缺键与英文原文都视为此次官网恢复不完整。
test.each([
  'AI infrastructure,',
  'Built for laboratories and research groups',
  'Research use cases',
  'Lab procurement and invoicing',
  'Give your research a dependable foundation.',
])('简体中文环境下首页文案 %s 应提供中文翻译', (key) => {
  expect(zhCN.translation[key as keyof typeof zhCN.translation]).toBeDefined()
  expect(zhCN.translation[key as keyof typeof zhCN.translation]).not.toBe(key)
})
