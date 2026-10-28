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
import { t } from 'i18next'
import { z } from 'zod'

import { isPublicImageUrl } from '@/lib/public-image-url'

// 系统信息表单内的采购字段规则；不单独维护另一套设置表单。
// 所有字段可选，以便与其他站点设置分步保存。
// 空字符串用于清除已有展示，不能被替换成旧值或静默默认值。
// 姓名与微信号先去除两端空白，避免公开页和复制结果不一致。
// 100 字符的上限同时在服务端检查，前端只负责及时反馈。
// 表单校验不替代服务端校验，公开内容仍需经过后端入口约束。
// 二维码字段只保存公开图片地址，不承载原图或 Base64 内容。
// 上传控件通过专用接口获取地址，再交回这组共享表单值。
// 地址规则复用公开图片校验，避免设置页接受而首页拒绝显示。
// 清空地址是合法操作，因此不把空字符串交给 URL 校验器。
// 错误文字在校验时调用翻译函数，避免冻结为模块首次加载语言。
// 其他字段可能在当前保存动作中缺省，不额外补出无关设置。
// 类型直接从规则推导，保证输入组件和校验结果保持同一契约。
// 新增采购字段时需要同时核对站点默认值、设置映射与状态输出。
export const procurementContactSchema = {
  // 姓名用于公开联系卡片，只保存文本，不允许 HTML 展示约定。
  ProcurementContactName: z
    .string()
    .trim()
    .max(100, { error: () => t('Use 100 characters or fewer.') })
    .optional(),
  // 微信号保持字符串，不转换数字，以免丢失前导零等标识。
  ProcurementWeChatID: z
    .string()
    .trim()
    .max(100, { error: () => t('Use 100 characters or fewer.') })
    .optional(),
  // 图片地址允许留空；非空输入必须满足统一公开资源约束。
  ProcurementWeChatQRCodeURL: z
    .string()
    .trim()
    .refine((value) => !value || isPublicImageUrl(value), {
      error: () =>
        t(
          'Use an HTTPS image URL or a site path starting with / (up to 2048 characters).'
        ),
    })
    .optional(),
}

// 消费方通过类型推导复用规则，不再手写一份可能漂移的字段类型。
export type ProcurementContactValues = z.infer<
  z.ZodObject<typeof procurementContactSchema>
>
