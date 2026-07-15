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
import { HugeiconsIcon, type IconSvgElement } from '@hugeicons/react'
import type { ReactNode } from 'react'

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

type HelpSectionProps = {
  title: string
  description?: string
  children: ReactNode
}

export function HelpSection(props: HelpSectionProps) {
  return (
    <section className='flex flex-col gap-4'>
      <header className='flex flex-col gap-1'>
        <h2 className='text-xl font-semibold tracking-tight'>{props.title}</h2>
        {props.description && (
          <p className='text-muted-foreground max-w-3xl text-sm leading-6'>
            {props.description}
          </p>
        )}
      </header>
      {props.children}
    </section>
  )
}

type GuideCardProps = {
  icon: IconSvgElement
  eyebrow?: string
  title: string
  description: string
  children?: ReactNode
}

export function GuideCard(props: GuideCardProps) {
  return (
    <Card className='h-full'>
      <CardHeader>
        <div className='bg-muted text-foreground mb-2 flex size-9 items-center justify-center rounded-lg'>
          <HugeiconsIcon
            icon={props.icon}
            strokeWidth={2}
            className='size-4'
            aria-hidden='true'
          />
        </div>
        {props.eyebrow && (
          <p className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
            {props.eyebrow}
          </p>
        )}
        <CardTitle>{props.title}</CardTitle>
        <CardDescription className='leading-6'>
          {props.description}
        </CardDescription>
      </CardHeader>
      {props.children && (
        <CardContent className='mt-auto'>{props.children}</CardContent>
      )}
    </Card>
  )
}
