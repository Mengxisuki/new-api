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
import { BookOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { HOME_DOCS_URL } from '@/lib/constants'
import { cn } from '@/lib/utils'

type DocsButtonProps = {
  className?: string
  compact?: boolean
  href?: string
}

export function DocsButton(props: DocsButtonProps) {
  const { t } = useTranslation()

  return (
    <Button
      variant='outline'
      className={cn(
        'group border-border/50 hover:border-border hover:bg-muted/50',
        props.compact
          ? undefined
          : 'inline-flex h-11 items-center gap-1.5 rounded-lg px-5 text-sm font-medium',
        props.className
      )}
      render={
        <a
          href={props.href || HOME_DOCS_URL}
          target='_blank'
          rel='noopener noreferrer'
        />
      }
      nativeButton={false}
    >
      <BookOpen className='text-muted-foreground/80 group-hover:text-foreground size-4 transition-colors duration-200' />
      <span>{t('Docs')}</span>
    </Button>
  )
}
