import { ArrowDown, ArrowUp, GripVertical, X } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

import {
  ApiKeyGroupCombobox,
  type ApiKeyGroupOption,
} from './api-key-group-combobox'

type ApiKeyGroupPrioritySelectorProps = {
  options: ApiKeyGroupOption[]
  value: string[]
  onValueChange: (value: string[]) => void
}

export function ApiKeyGroupPrioritySelector(
  props: ApiKeyGroupPrioritySelectorProps
) {
  const { t } = useTranslation()
  const [dragIndex, setDragIndex] = useState<number | null>(null)
  const candidates = props.options.filter(
    (option) => option.value !== 'auto' && !props.value.includes(option.value)
  )

  const moveGroup = (from: number, to: number) => {
    if (to < 0 || to >= props.value.length || from === to) return
    const next = [...props.value]
    const [group] = next.splice(from, 1)
    next.splice(to, 0, group)
    props.onValueChange(next)
  }

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex flex-col gap-2'>
        {props.value.map((group, index) => {
          const option = props.options.find((item) => item.value === group)
          return (
            <div
              key={group}
              draggable
              onDragStart={() => setDragIndex(index)}
              onDragEnd={() => setDragIndex(null)}
              onDragOver={(event) => event.preventDefault()}
              onDrop={() => {
                if (dragIndex !== null) moveGroup(dragIndex, index)
                setDragIndex(null)
              }}
              className='bg-muted/30 flex items-center gap-2 rounded-lg border p-2.5'
            >
              <GripVertical
                className='text-muted-foreground size-4 shrink-0 cursor-grab'
                aria-label={t('Drag to reorder')}
              />
              <Badge variant='secondary' className='shrink-0'>
                {t('Priority {{priority}}', { priority: index + 1 })}
              </Badge>
              <div className='min-w-0 flex-1'>
                <div className='truncate text-sm font-medium'>{group}</div>
                {option?.desc && (
                  <div className='text-muted-foreground truncate text-xs'>
                    {option.desc}
                  </div>
                )}
              </div>
              {option?.ratio !== undefined && (
                <Badge
                  variant='outline'
                  className='hidden shrink-0 sm:inline-flex'
                >
                  {option.ratio}x
                </Badge>
              )}
              <div className='flex shrink-0 gap-1'>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  disabled={index === 0}
                  aria-label={t('Move {{group}} up', { group })}
                  onClick={() => moveGroup(index, index - 1)}
                >
                  <ArrowUp className='size-4' />
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  disabled={index === props.value.length - 1}
                  aria-label={t('Move {{group}} down', { group })}
                  onClick={() => moveGroup(index, index + 1)}
                >
                  <ArrowDown className='size-4' />
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  disabled={props.value.length === 1}
                  aria-label={t('Remove {{group}}', { group })}
                  onClick={() =>
                    props.onValueChange(
                      props.value.filter((item) => item !== group)
                    )
                  }
                >
                  <X className='size-4' />
                </Button>
              </div>
            </div>
          )
        })}
      </div>
      {candidates.length > 0 && (
        <ApiKeyGroupCombobox
          options={candidates}
          onValueChange={(group) =>
            props.onValueChange([...props.value, group])
          }
          placeholder={t('Add API key group')}
        />
      )}
      <p className='text-muted-foreground text-xs'>
        {t(
          'Groups are tried from top to bottom. Billing uses the group that serves the request.'
        )}
      </p>
    </div>
  )
}
