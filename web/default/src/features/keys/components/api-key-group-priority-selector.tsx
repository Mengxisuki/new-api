import { ArrowDown, ArrowUp, GripVertical, X } from 'lucide-react'
import { Reorder, useDragControls, useReducedMotion } from 'motion/react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import {
  ApiKeyGroupCombobox,
  type ApiKeyGroupOption,
} from './api-key-group-combobox'

type ApiKeyGroupPrioritySelectorProps = {
  options: ApiKeyGroupOption[]
  value: string[]
  onValueChange: (value: string[]) => void
}

type SortableGroupItemProps = {
  group: string
  option?: ApiKeyGroupOption
  index: number
  total: number
  onMove: (from: number, to: number) => void
  onRemove: (group: string) => void
}

function SortableGroupItem(props: SortableGroupItemProps) {
  const { t } = useTranslation()
  const controls = useDragControls()
  const reducedMotion = useReducedMotion()
  const [isDragging, setIsDragging] = useState(false)

  return (
    <Reorder.Item
      as='div'
      value={props.group}
      dragControls={controls}
      dragListener={false}
      onDragStart={() => setIsDragging(true)}
      onDragEnd={() => setIsDragging(false)}
      whileDrag={reducedMotion ? undefined : { scale: 1.02 }}
      transition={
        reducedMotion
          ? { duration: 0 }
          : { type: 'spring', stiffness: 500, damping: 38 }
      }
      className={cn(
        'bg-muted/30 relative flex items-center gap-2 rounded-lg border p-2.5 transition-[border-color,background-color,box-shadow,opacity]',
        isDragging &&
          'bg-background border-ring/60 shadow-xl ring-2 ring-ring/25'
      )}
      role='listitem'
    >
      <Button
        type='button'
        variant='ghost'
        size='icon-sm'
        className='cursor-grab touch-none active:cursor-grabbing'
        aria-label={t('Drag to reorder')}
        onPointerDown={(event) => controls.start(event)}
      >
        <GripVertical className='text-muted-foreground size-4' />
      </Button>
      <Badge variant='secondary' className='shrink-0'>
        {t('Priority {{priority}}', { priority: props.index + 1 })}
      </Badge>
      <div className='min-w-0 flex-1'>
        <div className='truncate text-sm font-medium'>{props.group}</div>
        {props.option?.desc && (
          <div className='text-muted-foreground truncate text-xs'>
            {props.option.desc}
          </div>
        )}
      </div>
      {props.option?.ratio !== undefined && (
        <Badge variant='outline' className='hidden shrink-0 sm:inline-flex'>
          {props.option.ratio}x
        </Badge>
      )}
      <div className='flex shrink-0 gap-1'>
        <Button
          type='button'
          variant='ghost'
          size='icon-sm'
          disabled={props.index === 0}
          aria-label={t('Move {{group}} up', { group: props.group })}
          onClick={() => props.onMove(props.index, props.index - 1)}
        >
          <ArrowUp className='size-4' />
        </Button>
        <Button
          type='button'
          variant='ghost'
          size='icon-sm'
          disabled={props.index === props.total - 1}
          aria-label={t('Move {{group}} down', { group: props.group })}
          onClick={() => props.onMove(props.index, props.index + 1)}
        >
          <ArrowDown className='size-4' />
        </Button>
        <Button
          type='button'
          variant='ghost'
          size='icon-sm'
          disabled={props.total === 1}
          aria-label={t('Remove {{group}}', { group: props.group })}
          onClick={() => props.onRemove(props.group)}
        >
          <X className='size-4' />
        </Button>
      </div>
    </Reorder.Item>
  )
}

export function ApiKeyGroupPrioritySelector(
  props: ApiKeyGroupPrioritySelectorProps
) {
  const { t } = useTranslation()
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
      <Reorder.Group
        as='div'
        axis='y'
        values={props.value}
        onReorder={props.onValueChange}
        className='flex flex-col gap-2'
        role='list'
      >
        {props.value.map((group, index) => {
          const option = props.options.find((item) => item.value === group)
          return (
            <SortableGroupItem
              key={group}
              group={group}
              option={option}
              index={index}
              total={props.value.length}
              onMove={moveGroup}
              onRemove={(removedGroup) =>
                props.onValueChange(
                  props.value.filter((item) => item !== removedGroup)
                )
              }
            />
          )
        })}
      </Reorder.Group>
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
