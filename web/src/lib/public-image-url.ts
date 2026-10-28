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
// 公开图片 URL 的共享入口校验，首页和设置表单使用同一规则。
// 调用方先 trim；这里不静默修改输入，以免显示地址与保存地址不同。
// 空值由表单当作主动清空处理，不是有效的图片地址。
// 允许 HTTPS 外部资源以及以单个斜杠开头的站内路径。
// 禁止双斜杠协议相对地址，避免浏览器把站内路径解释为外部主机。
// 禁止反斜杠和换行，减少 URL 与浏览器解析规则的歧义。
// 长度上限与后端采购二维码地址契约一致。
// 外部 URL 不得携带用户名或密码，防止公开图片请求暴露凭据。
// 这里只判断可安全展示的地址形式，不验证图片存在或内容格式。
// 网络失败由图片组件展示，上传内容校验由服务端处理。
// URL 构造失败返回 false，使表单能够给出明确校验反馈。
export function isPublicImageUrl(value: string): boolean {
  if (!value || value.length > 2048 || /[\\\r\n]/.test(value)) return false
  if (value.startsWith('/') && !value.startsWith('//')) return true
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && !url.username && !url.password
  } catch {
    return false
  }
}
