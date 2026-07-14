import { PublicLayout } from '@/components/layout'
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
import { PageTransition } from '@/components/page-transition'
import { useStatus } from '@/hooks/use-status'

import { HelpContent } from './components/help-content'
import { HelpSidebar } from './components/help-sidebar'
import type { HelpTab } from './types'

type HelpCenterProps = {
  activeTab: HelpTab
  onTabChange: (tab: HelpTab) => void
}

export function HelpCenter(props: HelpCenterProps) {
  const { status } = useStatus()
  const docsLink = status?.docs_link as string | undefined

  return (
    <PublicLayout showMainContainer={false}>
      <div className='relative min-h-svh'>
        <div
          aria-hidden='true'
          className='from-primary/8 pointer-events-none absolute inset-x-0 top-0 h-96 bg-gradient-to-b to-transparent'
        />
        <PageTransition className='relative mx-auto w-full max-w-7xl px-4 pt-24 pb-16 sm:px-6 sm:pt-28 lg:px-8'>
          <div className='grid min-w-0 grid-cols-[minmax(0,1fr)] gap-8 lg:grid-cols-[15rem_minmax(0,1fr)] lg:gap-12'>
            <HelpSidebar
              activeTab={props.activeTab}
              onTabChange={props.onTabChange}
            />
            <HelpContent activeTab={props.activeTab} docsLink={docsLink} />
          </div>
        </PageTransition>
      </div>
    </PublicLayout>
  )
}

export type { HelpTab } from './types'
