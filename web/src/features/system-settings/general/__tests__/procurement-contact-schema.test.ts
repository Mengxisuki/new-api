import { expect, test } from 'vitest'
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
import { z } from 'zod'

import { procurementContactSchema } from '../procurement-contact-schema'

// 使用实际字段 schema，避免在测试中复制 URL 或长度判断。
// 空字段允许管理员关闭单个公开信息，不强制收集联系人资料。
// 姓名、微信号与图片地址分别校验，错误应落在对应字段上。
// 配置 schema 的行为边界：
// 直接组合生产字段，不在测试中重新实现校验器。
// 姓名和微信号共用长度契约，但应分别定位错误字段。
// 使用中文字符验证长度，防止意外按传输字节计数。
// 一百字符允许保存，一百零一字符拒绝，保护真实边界。
// 空图片字段意味着关闭展示，不能被 URL 校验误拒绝。
// 站内地址需以单斜线开头，协议相对地址不属于站内路径。
// 外部图片仅允许 HTTPS，避免混合内容和降级传输。
// 地址中的凭据不是公开资源的一部分，必须拒绝。
// 脚本协议不能因为字段名是图片地址而自动受信任。
// 反斜线可能被浏览器规范化，必须在进入图片元素前拒绝。
// 字段 trim 在 URL 校验之前运行，因此末尾换行作为空白清除。
// 测试显式记录这一既有规则，不能误称原始输入被完整保留。
// 上传图片的二进制安全、大小和尺寸由后端测试另行负责。
// 这里的通过只证明表单配置契约，不代表远端资源可达。
const schema = z.object(procurementContactSchema)

test('采购资料允许留空并去除文本字段首尾空格', () => {
  // 只验证传入文本的规范化，不推断接口或存储行为。
  const result = schema.parse({
    ProcurementContactName: '  实验室采购  ',
    ProcurementWeChatID: '',
    ProcurementWeChatQRCodeURL: '',
  })
  expect(result.ProcurementContactName).toBe('实验室采购')
  expect(result.ProcurementWeChatQRCodeURL).toBe('')
})

test.each(['ProcurementContactName', 'ProcurementWeChatID'])(
  '%s超出一百字符时拒绝，边界长度仍可保存',
  (field) => {
    // 中文字符计数应按文本长度处理，不按 UTF-8 字节数误拒绝。
    expect(schema.safeParse({ [field]: '中'.repeat(100) }).success).toBe(true)
    expect(schema.safeParse({ [field]: '中'.repeat(101) }).success).toBe(false)
  }
)

test.each([
  ['https://example.com/qr.png', true],
  ['/api/status/procurement-wechat-qr?v=image', true],
  ['http://example.com/qr.png', false],
  ['//example.com/qr.png', false],
  ['javascript:alert(1)', false],
  ['https://name:password@example.com/qr.png', false],
  ['/qr.png\n', true],
  ['/qr\\image.png', false],
])('二维码地址%s的配置校验结果应为%s', (value, valid) => {
  // HTTPS 与站内路径是既有公开图片契约；协议相对地址不能绕过。
  // 表单先 trim，因此末尾换行被清理；内部控制字符由 URL 校验拒绝。
  expect(schema.safeParse({ ProcurementWeChatQRCodeURL: value }).success).toBe(
    valid
  )
})
