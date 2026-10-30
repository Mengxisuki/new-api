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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { STATUS_QUERY_KEY } from '@/lib/status-query'

import { OfficialPaymentChannelDialog } from '../official-payment-channel-dialog'
import { createOfficialChannelDefault } from '../official-payment-channel-types'
import { OfficialPaymentChannelsEditor } from '../official-payment-channels-editor'

// 坏配置不能先过滤出合法项后再允许保存，否则一次普通编辑会删除原始商户。
// 测试同时覆盖 JSON 语法错误和语法正确但渠道内容不完整的边界。
// 使用真实编辑器检查可见错误及操作入口，不依赖解析器内部函数。
// 这里只保护浏览器草稿，后端仍要对真正保存的商户数据再次校验。
// 失败时不创建替代空数组，也不发送配置更新。
test.each(['{broken', '[{"id":"missing-config"}]'])(
  '无效官方支付配置 %s 显示错误且不允许覆盖',
  (value) => {
    const onChange = vi.fn() // 观察外层草稿契约，不能仅检查错误文字。
    render(<OfficialPaymentChannelsEditor value={value} onChange={onChange} />)
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Official payment channels must be a valid JSON array'
    )
    // 原始输入仍被保留，用户需要修正后才能继续可视化编辑。
    expect(
      screen.queryByRole('button', { name: 'Add official channel' })
    ).not.toBeInTheDocument()
    expect(onChange).not.toHaveBeenCalled() // 加载失败不会静默写回删减后的配置。
  }
)

// 脱敏响应只带凭证名称，编辑已有商户时不能要求再次输入真实私钥。
// 下方全部身份资料为虚构 fixture，不包含任何可用商户凭证。
// 输入框保持空值意味着“沿用服务器已存值”，不能变成清空私钥。
// 保存渠道草稿只触发 onSave，整页付款设置负责真正提交。
// 原有渠道 ID 不可编辑，避免已创建订单的回调无法匹配商户。
// 公钥 ID 属于公开验签配置，与私钥输入的保留规则区分。
// Provider 使用真实状态缓存，组件仍走项目统一 useStatus 链路。
// 断言等待明确保存结果，不使用定时延迟猜测表单校验完成。
test('编辑已保存微信渠道时凭证不回显且空白提交保留已配置凭证', async () => {
  const user = userEvent.setup()
  const onSave = vi.fn() // 服务端配置保存不属于表单局部职责。
  const channel = createOfficialChannelDefault('wechat') // 默认模板必须与正式新建渠道使用同一字段集合。
  channel.enabled = true // 启用状态要求全部必要商户材料通过实际表单校验。
  channel.config.appid = 'test-app' // 公开应用身份不作为秘密隐藏。
  channel.config.mchid = 'test-merchant' // 商户身份字段仍需存在，脱敏不豁免公开必填项。
  channel.config.merchant_serial_no = 'test-serial' // 商户证书序列号与平台公钥 ID 分开配置。
  channel.config.wechatpay_public_key_id = 'PUB_KEY_ID_test' // 后端公钥验签需要指定对应 ID。
  channel.configured_secrets = [
    'merchant_private_key',
    'api_v3_key',
    'wechatpay_public_key',
  ]
  const client = new QueryClient() // 每个表单测试独立缓存，不访问实际站点配置。
  client.setQueryData(STATUS_QUERY_KEY, {
    server_address: 'https://oak.example',
  })
  render(
    <QueryClientProvider client={client}>
      <OfficialPaymentChannelDialog
        open
        onOpenChange={vi.fn()}
        onSave={onSave}
        channel={channel}
        existingChannels={[channel]}
      />
    </QueryClientProvider>
  )
  expect(screen.getByRole('textbox', { name: 'Channel ID' })).toBeDisabled()
  expect(screen.getByLabelText('Merchant private key')).toHaveValue('')
  // API v3 密钥是单行密码输入，仍然必须为空而非占位密钥内容。
  expect(screen.getByLabelText('API v3 key')).toHaveValue('')
  await user.clear(screen.getByRole('textbox', { name: 'Name' }))
  await user.type(
    screen.getByRole('textbox', { name: 'Name' }),
    'WeChat Research'
  )
  await user.click(screen.getByRole('button', { name: 'Update' })) // 走实际表单提交与启用校验。
  await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
  // 对外提交维持已存秘密名和空值，私钥不会在界面被重新拼接。
  expect(onSave).toHaveBeenCalledWith(
    expect.objectContaining({
      name: 'WeChat Research', // 仅展示名称改变，原凭证材料不应被替换。
      configured_secrets: channel.configured_secrets, // 名称数组是保留凭证约定，不包含可用密钥。
      config: expect.objectContaining({
        merchant_private_key: '',
        api_v3_key: '',
        wechatpay_public_key: '',
      }),
    })
  )
  client.clear() // 不把站点状态或商户草稿带到其他设置测试。
})
