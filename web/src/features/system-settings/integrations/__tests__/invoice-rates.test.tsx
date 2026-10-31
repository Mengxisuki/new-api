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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { expect, test } from 'vitest'

import { InvoiceRateEditor } from '../invoice-rate-editor'

// 真实受控草稿验证可视化分组编辑，不能仅断言静态输入能够渲染。
// 零费率必须保留，不能被当作空值落回默认百分比。
function GroupRates() {
  const [value, setValue] = useState('{"default":6}')
  return (
    <>
      <InvoiceRateEditor value={value} onChange={setValue} />
      <output aria-label='rates'>{value}</output>
    </>
  )
}

// 修改分组名称时行身份保持稳定，连续输入不会因为React key变化丢字。
test('管理员可连续编辑分组名称并设置零开票加价', async () => {
  const user = userEvent.setup() // 连续真实输入保护可编辑分组名的焦点与字符顺序。
  render(<GroupRates />)
  const group = screen.getByRole('textbox', { name: 'User group' }) // 通过可访问名称定位真实输入，不读取私有state。
  await user.clear(group) // 新名称允许中间空草稿，不能立刻删除整行。
  await user.type(group, 'vip') // 每个字符都触发受控更新，能暴露不稳定key的问题。
  const rate = screen.getByRole('spinbutton', { name: 'Surcharge (%)' }) // 百分比编辑使用公共Input，清空行为必须真实触发。
  await user.clear(rate) // 清空已有费率后允许设零，不能残留原6拼成60。
  await user.type(rate, '0') // 零是有效覆盖，并不表示删除此组配置。
  expect(screen.getByLabelText('rates')).toHaveTextContent('{"vip":0}') // 同时保护分组名和零费率的真实对外草稿。
})

// 无效外部JSON不能被清空为默认零费率，必须禁止覆盖原始资料。
test('已有分组开票配置损坏时显示错误且不生成替代空配置', () => {
  render(
    <InvoiceRateEditor
      value='broken' // 加载失败不能自动变为会免收所有加价的空配置。
      onChange={() => {
        throw new Error('must not overwrite') // 如生产组件错误写回配置，此测试会立即失败。
      }}
    />
  )
  expect(screen.getByRole('alert')).toHaveTextContent(
    'Invoice group surcharge rates must be a JSON object'
  )
  expect(
    screen.queryByRole('button', { name: 'Add group surcharge' })
  ).not.toBeInTheDocument()
})

// 精度规则与服务端订单快照一致，超出四位时不能把坏草稿交给整页保存。
test('分组开票费率超过四位小数时保留有效草稿并显示错误', async () => {
  const user = userEvent.setup()
  render(<GroupRates />)
  const rate = screen.getByRole('spinbutton', { name: 'Surcharge (%)' })
  await user.clear(rate) // 旧数值应可完整替换，不能将新数字追加成另一费率。
  await user.type(rate, '6.00001') // 超出四位小数的明确反例，不靠随机输入覆盖边界。
  expect(screen.getByRole('alert')).toHaveTextContent(
    'Invoice surcharge percentage supports at most 4 decimal places'
  )
  expect(screen.getByLabelText('rates')).not.toHaveTextContent('6.00001') // 坏精度不能污染后续整页提交的草稿。
})
