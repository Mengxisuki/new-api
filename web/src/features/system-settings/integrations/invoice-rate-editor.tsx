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
import { Plus, Trash2 } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { invoiceRateSchema } from './invoice-rate-schema'

interface Props {
  value: string // 外层表单仍是JSON草稿，真正提交由整页保存统一处理。
  onChange: (value: string) => void // 只输出草稿，真正写入仍受整页校验及Root接口限制。
}

// 配置仅含分组与百分比，不能借普通草稿提交用户或商户资料。
// 外层整页表单统一保存，本编辑器只生成可校验的JSON草稿。
// 空分组是新增时的暂态，真正保存前必须补齐名称。
// 分组对象不能接受重复键，否则输入一次会无声覆盖另一项费率。
// 显式零费率用于免加价，不得通过真值判断落回默认百分比。
// 数字范围与精度复用同一个前端schema，服务端仍执行独立可信验证。
// 用户输入超过四位小数时保留原有效草稿并提示错误，不发部分配置。
// 清空百分比进入零草稿，避免受控数字框无法从原值改成免加价。
// 未配置分组使用默认费率，编辑器不复制默认率到每个分组。
// 删除组覆盖代表恢复默认，而不是停用该用户组的充值能力。
// 原始坏数据显式报错，不过滤坏项后把剩余对象保存回服务器。
// 行身份与可编辑分组名分离，连续输入不会因React重新挂载丢字。
// 数据来自受保护系统选项，不持久化到独立浏览器缓存。
// 当前较长部分为声明式输入布局，只有更新草稿一个数据出口。
export function InvoiceRateEditor(props: Props) {
  const { t } = useTranslation() // 错误与字段名都随当前界面语言更新。
  const [error, setError] = useState<'duplicate' | 'rate' | 'precision' | null>(
    null
  )
  const errorMessages = {
    // 错误按具体失败原因说明，不把精度问题伪装成JSON损坏。
    duplicate: t('Each invoice surcharge group can only appear once.'),
    rate: t('Invoice surcharge percentage must be between 0 and 100'),
    precision: t(
      'Invoice surcharge percentage supports at most 4 decimal places'
    ),
  }
  const rowKeys = useRef<string[]>([]) // 草稿名称可编辑，行身份不能随分组名称变化而失焦。
  let entries: [string, number][] // 保留对象原有分组顺序，不按费率重新排序。
  try {
    const data: unknown = JSON.parse(props.value || '{}') // 仅空配置等价于无覆盖，非空坏JSON不能回退。
    if (!data || typeof data !== 'object' || Array.isArray(data)) {
      throw new Error('Invalid group rates') // 完整配置无效时阻止可视化覆盖，保留原始数据待修正。
    }
    entries = Object.entries(data) // 先确认整体对象再遍历，数组不是分组费率表。
    if (
      entries.some(
        ([, value]) =>
          typeof value !== 'number' || // null和字符串不能被coerce误当成合法零费率。
          !invoiceRateSchema.safeParse(value).success
      )
    ) {
      throw new Error('Invalid group rates')
    }
  } catch {
    return (
      <p role='alert'>
        {t('Invoice group surcharge rates must be a JSON object')}
      </p>
    )
  }
  while (rowKeys.current.length < entries.length) {
    rowKeys.current.push(crypto.randomUUID()) // 身份只在增加一行时产生，不随每个字符变化。
  }
  const update = (index: number, group: string, rate: number) => {
    // 每次只更新当前行，其他组覆盖原样保留。
    // 在局部副本中验证，失败不会损坏外层已保存草稿。
    const next = entries.map((entry, position) =>
      position === index ? [group, rate] : entry
    ) as [string, number][]
    // 重复名字会使对象键覆盖，因此编辑期间拒绝这个名称而不删除旧行。
    if (next.some(([key], position) => position !== index && key === group)) {
      setError('duplicate') // 明确提示命名冲突，不能静默用后一个覆盖前一个。
      return
    }
    if (!Number.isFinite(rate) || rate < 0 || rate > 100) {
      setError('rate') // 负数或超过百分之百不进入真正保存的数据。
      return
    }
    if (!invoiceRateSchema.safeParse(rate).success) {
      setError('precision') // 提前阻断后端会拒绝的精度，避免整页逐项保存部分成功。
      return
    }
    setError(null) // 合法修改后清除上一项错误，允许继续编辑。
    props.onChange(JSON.stringify(Object.fromEntries(next))) // 对象结构是服务端唯一接受的分组配置合同。
  }
  return (
    <div className='space-y-3'>
      {error && <p role='alert'>{errorMessages[error]}</p>}
      {entries.map(([group, rate], index) => (
        <div
          key={rowKeys.current[index]} // 名称改变后仍保留该行输入焦点和选择区间。
          className='grid grid-cols-[minmax(0,1fr)_7rem_auto] items-end gap-3'
        >
          <div className='space-y-2'>
            <Label htmlFor={`invoice-group-${index}`}>{t('User group')}</Label>
            <Input
              id={`invoice-group-${index}`}
              value={group} // 当前组名保持受控，重名修改失败时原有效值仍在。
              maxLength={64} // 组标识长度与后端一致，不能依赖保存时才截断。
              onChange={(event) => update(index, event.target.value, rate)} // 组名修改经过唯一性检查，不静默覆盖另一行。
            />
          </div>
          <div className='space-y-2'>
            <Label htmlFor={`invoice-rate-${index}`}>
              {t('Surcharge (%)')}
            </Label>
            <Input
              id={`invoice-rate-${index}`}
              type='number' // 费率是百分数，不能接受任意文本对象。
              min={0} // 零费率代表显式免加价，保留该有效覆盖。
              max={100} // 界面边界与模型费率范围一致。
              step='0.0001' // 允许服务端支持的四位小数精度。
              value={rate} // 显示原百分数6，不能把它转成0.06后再提交。
              onChange={(event) => {
                const value = Number(event.target.value) // 清空时按零草稿处理，允许将已有百分比改为免加价。
                if (Number.isFinite(value)) update(index, group, value) // 非法数字不进入可保存草稿。
              }}
            />
          </div>
          <Button
            type='button'
            variant='outline' // 添加和移除仅编辑覆盖项，不直接触发付款或系统保存。
            size='icon'
            aria-label={t('Remove group surcharge')} // 删除含义是恢复该组默认费率，不是删除用户分组。
            onClick={() => {
              rowKeys.current.splice(index, 1) // 删除时同步行身份，避免下一行被错误复用。
              props.onChange(
                // 仅移除当前覆盖，其余分组及默认率不受影响。
                JSON.stringify(
                  Object.fromEntries(
                    entries.filter((_, position) => position !== index)
                  )
                )
              )
            }}
          >
            <Trash2 aria-hidden='true' />
          </Button>
        </div>
      ))}
      <Button
        type='button'
        variant='outline'
        disabled={entries.some(([group]) => !group)} // 未命名行先完成，避免多个空键互相覆盖。
        onClick={() =>
          props.onChange(
            JSON.stringify(Object.fromEntries([...entries, ['', 0]]))
          )
        }
      >
        <Plus aria-hidden='true' />
        {t('Add group surcharge')}
      </Button>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Groups without an override use the default percentage. Set 0 to waive the surcharge for a group.'
        )}
      </p>
    </div>
  )
}
