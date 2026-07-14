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
import { useTranslation } from 'react-i18next'

import type { HelpTab } from '../types'
import { FaqContent } from './help-faq'
import { AppTutorialsContent, DeveloperGuideContent } from './help-guides'
import { GettingStartedContent, QuickStartContent } from './help-start'

type HelpContentProps = {
  activeTab: HelpTab
  docsLink?: string
}

export function HelpContent(props: HelpContentProps) {
  const { t } = useTranslation()
  const headings: Record<HelpTab, { title: string; description: string }> = {
    start: {
      title: t('Quick Start'),
      description: t('Go from a new account to a successful API request.'),
    },
    beginner: {
      title: t('Getting Started'),
      description: t('Learn the account, key, model, and usage workflow.'),
    },
    apps: {
      title: t('App Tutorials'),
      description: t('Connect SDKs, tools, and OpenAI-compatible clients.'),
    },
    faq: {
      title: t('FAQ'),
      description: t('Find practical answers to common integration issues.'),
    },
    developer: {
      title: t('Developer Guide'),
      description: t('Review the API conventions used by this deployment.'),
    },
  }
  const currentHeading = headings[props.activeTab]

  let content = <QuickStartContent />
  if (props.activeTab === 'beginner') content = <GettingStartedContent />
  if (props.activeTab === 'apps') content = <AppTutorialsContent />
  if (props.activeTab === 'faq') content = <FaqContent />
  if (props.activeTab === 'developer') {
    content = <DeveloperGuideContent docsLink={props.docsLink} />
  }

  return (
    <article className='min-w-0'>
      <header className='border-border/70 mb-8 border-b pb-6'>
        <p className='text-primary mb-2 text-xs font-semibold tracking-[0.18em] uppercase'>
          {t('Help Center')}
        </p>
        <h1 className='text-3xl font-semibold tracking-tight sm:text-4xl'>
          {currentHeading.title}
        </h1>
        <p className='text-muted-foreground mt-3 max-w-2xl text-sm leading-6 sm:text-base'>
          {currentHeading.description}
        </p>
      </header>
      {content}
    </article>
  )
}
