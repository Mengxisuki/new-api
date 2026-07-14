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
  BookOpen02Icon,
  CodeIcon,
  Compass01Icon,
  PlayCircle02Icon,
  QuestionIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import type { HelpTab } from '../types'

type HelpSidebarProps = {
  activeTab: HelpTab
  onTabChange: (tab: HelpTab) => void
}

export function HelpSidebar(props: HelpSidebarProps) {
  const { t } = useTranslation()
  const items = [
    {
      value: 'start' as const,
      title: t('Quick Start'),
      description: t('Complete the first API call'),
      icon: Compass01Icon,
    },
    {
      value: 'beginner' as const,
      title: t('Getting Started'),
      description: t('Learn the core workflow'),
      icon: BookOpen02Icon,
    },
    {
      value: 'apps' as const,
      title: t('App Tutorials'),
      description: t('Connect popular tools'),
      icon: PlayCircle02Icon,
    },
    {
      value: 'faq' as const,
      title: t('FAQ'),
      description: t('Solve common problems'),
      icon: QuestionIcon,
    },
    {
      value: 'developer' as const,
      title: t('Developer Guide'),
      description: t('Review endpoints and authentication'),
      icon: CodeIcon,
    },
  ]

  return (
    <nav
      aria-label={t('Help topics')}
      className='min-w-0 overflow-hidden lg:sticky lg:top-24'
    >
      <div className='border-border/70 bg-card/60 flex w-full max-w-full gap-1 overflow-x-auto rounded-xl border p-1.5 backdrop-blur-sm lg:flex-col lg:overflow-visible'>
        {items.map((item) => {
          const isActive = item.value === props.activeTab
          return (
            <button
              key={item.value}
              type='button'
              aria-current={isActive ? 'page' : undefined}
              onClick={() => props.onTabChange(item.value)}
              className={cn(
                'focus-visible:ring-ring flex min-w-44 items-center gap-3 rounded-lg px-3 py-2.5 text-left outline-none transition-colors focus-visible:ring-2 lg:min-w-0',
                isActive
                  ? 'bg-foreground text-background shadow-sm'
                  : 'text-muted-foreground hover:bg-muted hover:text-foreground'
              )}
            >
              <HugeiconsIcon
                icon={item.icon}
                strokeWidth={2}
                className='size-4 shrink-0'
              />
              <span className='min-w-0'>
                <span className='block text-sm font-medium'>{item.title}</span>
                <span
                  className={cn(
                    'mt-0.5 hidden text-xs lg:block',
                    isActive ? 'text-background/65' : 'text-muted-foreground'
                  )}
                >
                  {item.description}
                </span>
              </span>
            </button>
          )
        })}
      </div>
    </nav>
  )
}
