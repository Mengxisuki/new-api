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
  ApiIcon,
  ArrowRight01Icon,
  CodeIcon,
  ComputerTerminal01Icon,
  FileCodeIcon,
  PlayCircle02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { GuideCard, HelpSection } from './help-primitives'

export function AppTutorialsContent() {
  const { t } = useTranslation()

  return (
    <div className='flex flex-col gap-10'>
      <HelpSection
        title={t('Choose how you want to connect')}
        description={t(
          'Most clients only need two values: the API base URL from this site and an API key from your account.'
        )}
      >
        <div className='grid gap-3 md:grid-cols-3'>
          <GuideCard
            icon={PlayCircle02Icon}
            title={t('Built-in Playground')}
            description={t(
              'Test models in the browser before changing code or third-party application settings.'
            )}
          >
            <Button render={<Link to='/playground' />} nativeButton={false}>
              {t('Try in Playground')}
              <HugeiconsIcon icon={ArrowRight01Icon} data-icon='inline-end' />
            </Button>
          </GuideCard>
          <GuideCard
            icon={CodeIcon}
            title={t('OpenAI SDKs')}
            description={t(
              'Set the SDK base URL to this site, provide your key, and keep the standard request format.'
            )}
          />
          <GuideCard
            icon={ComputerTerminal01Icon}
            title={t('OpenAI-compatible apps')}
            description={t(
              'In apps that support custom providers, enter the API base URL, key, and an available model name.'
            )}
          />
        </div>
      </HelpSection>

      <HelpSection title={t('Connection checklist')}>
        <div className='grid gap-3 sm:grid-cols-2'>
          {[
            [
              t('API base URL'),
              t(
                'Use the address of this site and keep the /v1 path required by your client.'
              ),
            ],
            [
              t('API key'),
              t(
                'Use an active key with enough remaining quota and access to the selected model.'
              ),
            ],
            [
              t('Model name'),
              t(
                'Copy an enabled model identifier exactly as shown in the Model Square.'
              ),
            ],
            [
              t('Streaming'),
              t(
                'Enable streaming only when the selected client and upstream model support it.'
              ),
            ],
          ].map((item) => (
            <div key={item[0]} className='bg-card rounded-xl border p-4'>
              <h3 className='text-sm font-medium'>{item[0]}</h3>
              <p className='text-muted-foreground mt-1 text-sm leading-6'>
                {item[1]}
              </p>
            </div>
          ))}
        </div>
      </HelpSection>
    </div>
  )
}

type DeveloperGuideContentProps = {
  docsLink?: string
}

export function DeveloperGuideContent(props: DeveloperGuideContentProps) {
  const { t } = useTranslation()

  return (
    <div className='flex flex-col gap-10'>
      <HelpSection
        title={t('OpenAI-compatible API')}
        description={t(
          'Use Bearer authentication and JSON request bodies. Available models and endpoint support can vary by deployment.'
        )}
      >
        <div className='grid gap-3 sm:grid-cols-3'>
          <GuideCard icon={ApiIcon} title={t('Base URL')} description='/v1' />
          <GuideCard
            icon={FileCodeIcon}
            title={t('Content type')}
            description='application/json'
          />
          <GuideCard
            icon={ComputerTerminal01Icon}
            title={t('Authentication')}
            description='Authorization: Bearer $API_KEY'
          />
        </div>
      </HelpSection>

      <HelpSection title={t('Common endpoints')}>
        <div className='overflow-hidden rounded-xl border'>
          <div className='grid grid-cols-[5rem_1fr] gap-3 border-b p-3 text-sm last:border-b-0 sm:grid-cols-[6rem_15rem_1fr]'>
            <code className='font-semibold'>POST</code>
            <code className='break-all'>/v1/chat/completions</code>
            <span className='text-muted-foreground col-span-2 sm:col-span-1'>
              {t('Create a chat completion')}
            </span>
          </div>
          <div className='grid grid-cols-[5rem_1fr] gap-3 border-b p-3 text-sm last:border-b-0 sm:grid-cols-[6rem_15rem_1fr]'>
            <code className='font-semibold'>POST</code>
            <code className='break-all'>/v1/responses</code>
            <span className='text-muted-foreground col-span-2 sm:col-span-1'>
              {t('Create a response')}
            </span>
          </div>
          <div className='grid grid-cols-[5rem_1fr] gap-3 border-b p-3 text-sm last:border-b-0 sm:grid-cols-[6rem_15rem_1fr]'>
            <code className='font-semibold'>GET</code>
            <code className='break-all'>/v1/models</code>
            <span className='text-muted-foreground col-span-2 sm:col-span-1'>
              {t('List available models')}
            </span>
          </div>
        </div>
      </HelpSection>

      <div className='bg-muted/50 flex flex-col items-start justify-between gap-4 rounded-xl border p-5 sm:flex-row sm:items-center'>
        <div>
          <h2 className='font-medium'>{t('Need the full API reference?')}</h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t(
              'Open the deployment documentation for complete schemas and examples.'
            )}
          </p>
        </div>
        {props.docsLink ? (
          <Button
            variant='outline'
            render={
              <a
                href={props.docsLink}
                target='_blank'
                rel='noopener noreferrer'
              />
            }
            nativeButton={false}
          >
            {t('Open Docs')}
            <HugeiconsIcon icon={ArrowRight01Icon} data-icon='inline-end' />
          </Button>
        ) : null}
      </div>
    </div>
  )
}
