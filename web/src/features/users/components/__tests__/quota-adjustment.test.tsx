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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { UserQuotaDialog } from '../user-quota-dialog'

// 只替换网络边界，真实 Dialog、NativeSelect 和表单状态继续运行。
// 金额输入使用元，断言请求体中的人民币字段已经转换为整数分。
afterEach(() => {
  cleanup() // 先卸载弹窗，释放焦点锁，防止影响下一场景。
  vi.restoreAllMocks() // HTTP 桩不能让下一次真实请求误判为成功。
})

// 本场景验证首次提交的单位和 UUID 形状，不宣称覆盖失败重试生命周期。
// 输入面值与应收故意不同，防止回归成按额度面值生成债务。
// 未挂载真正付款服务，测试不会创建真实债务或增加账户余额。
test('挂帐调额要求说明和应收金额并提交UUID请求键', async () => {
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true },
  })
  const onSuccess = vi.fn()
  // 挂帐只传应收分，现金字段必须显式保持零，防止两种财务口径叠加。
  // request_id 由真实组件生成，测试只验证 UUIDv4 形状而不替换随机数。
  render(
    <UserQuotaDialog
      open
      onOpenChange={vi.fn()}
      userId={8} // 虚构目标与管理接口正文一致，不接受其他用户归属。
      currentQuota={1000} // 余额快照只供预览，不能当成本次人民币应收。
      onSuccess={onSuccess} // 刷新回调只能在服务端确认成功后执行。
    />
  )

  const dialog = screen.getByRole('dialog')
  await user.selectOptions(
    within(dialog).getByLabelText('Adjustment category'),
    'on_account'
  )
  await user.type(within(dialog).getByLabelText(/Amount/), '12.5') // 原面值仍走站点额度换算。
  await user.type(within(dialog).getByLabelText(/Note/), 'approved credit') // 原因说明与分类同时提交。
  await user.type(within(dialog).getByLabelText(/Receivable \(CNY\)/), '88.80') // 两位小数应精确落到分。
  await user.click(within(dialog).getByRole('button', { name: 'Confirm' })) // 通过真实确认动作到达 API。

  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user/manage',
      expect.objectContaining({
        id: 8, // 用户编号不能因选择挂帐而丢失。
        action: 'add_quota', // 沿用管理接口，不伪造支付网关到账。
        mode: 'add', // 新增不能误传为覆盖当前余额。
        value: 6250000, // 默认每美元五十万额度，独立于人民币应收。
        reason: 'on_account', // 债务分类由用户主动选择。
        note: 'approved credit', // 保存管理员填写的业务依据。
        cash_cents: 0, // 挂帐没有当期实收，不能计现金收入。
        receivable_cents: 8880, // 八十八元八角与额度面值无直接等式。
        cash_ref: '', // 没有真实收款，不应生成外部收款凭证。
      }),
      { singleUseAuthorization: true } // 未知资金结果不能被认证恢复拦截器自动重放。
    )
  )
  // 从网络正文读取公开合同，避免断言组件内部 ref。
  const payload = post.mock.calls[0]?.[1] as { request_id?: string }
  // 除长度外检查版本与变体，不能把任意随机字符串当 UUIDv4。
  expect(payload.request_id).toMatch(
    /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
  )
  expect(onSuccess).toHaveBeenCalledOnce()
})

test('线下现金缺少凭证时保持确认按钮禁用', async () => {
  // 线下收款必须同时具备金额和外部凭证；缺一项都不能进入网络边界。
  // 该断言保护 double-click 前的前端门禁，但不替代后端的财务校验。
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post')
  render(
    <UserQuotaDialog
      open
      onOpenChange={vi.fn()}
      userId={8}
      currentQuota={1000}
      onSuccess={vi.fn()}
    />
  )

  const dialog = screen.getByRole('dialog')
  await user.selectOptions(
    within(dialog).getByLabelText('Adjustment category'),
    'offline_cash'
  )
  await user.type(within(dialog).getByLabelText(/Amount/), '1') // 额度输入有效，排除因零面值禁用的假阳性。
  await user.type(within(dialog).getByLabelText(/Note/), 'cash fixture') // 说明有效，只留下缺凭证这一失败条件。
  await user.type(within(dialog).getByLabelText(/Cash received \(CNY\)/), '20') // 收款金额也有效，不能替代凭证。

  // 不只检查 DOM 禁用，还检查没有发送会修改余额的网络请求。
  expect(within(dialog).getByRole('button', { name: 'Confirm' })).toBeDisabled()
  expect(post).not.toHaveBeenCalled()
})
