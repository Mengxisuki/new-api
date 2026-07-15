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
  ArrowRight01Icon,
  CheckmarkCircle02Icon,
  CodeIcon,
  ComputerTerminal01Icon,
  Key01Icon,
  PlayCircle02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { GuideCard, HelpSection } from './help-primitives'

export function BeginnerIntegration() {
  const { t } = useTranslation()
  const setupSteps = [
    t('Open the Model Square and copy a model name that fits your task.'),
    t(
      'Create an API key, choose an allowed group, and set a quota limit for testing.'
    ),
    t(
      'Send one short prompt in the Playground. If it succeeds, the key, model, and balance are ready.'
    ),
    t(
      'Put the same base URL, API key, and model name into your app or coding client.'
    ),
    t(
      'Open Usage Logs and verify the endpoint, group, token counts, latency, and final charge.'
    ),
  ]
  const errors = [
    [
      '401',
      t(
        'The API key is missing, invalid, disabled, expired, or copied incorrectly.'
      ),
    ],
    ['403', t('The key or group is not allowed to use the requested model.')],
    [
      '404',
      t(
        'The base URL, API path, or model name is wrong. Copy the model identifier again.'
      ),
    ],
    [
      '429',
      t(
        'The request hit a rate limit, concurrency limit, or temporary upstream capacity limit. Retry with backoff.'
      ),
    ],
    [
      '5xx',
      t(
        'The upstream channel or platform is temporarily unavailable. Check the log and retry later.'
      ),
    ],
  ]

  return (
    <>
      <HelpSection
        title={t('Your first complete setup')}
        description={t(
          'Follow these steps in order. Do not start by editing application code; first prove that the account, key, and model work in the Playground.'
        )}
      >
        <ol className='border-border/70 divide-border/70 overflow-hidden rounded-xl border'>
          {setupSteps.map((step, index) => (
            <li key={step} className='flex items-start gap-3 p-4'>
              <span className='bg-foreground text-background flex size-7 shrink-0 items-center justify-center rounded-full text-xs font-semibold'>
                {index + 1}
              </span>
              <span className='text-sm leading-6'>{step}</span>
            </li>
          ))}
        </ol>
        <div className='flex flex-wrap gap-2'>
          <Button
            variant='outline'
            render={<Link to='/keys' />}
            nativeButton={false}
          >
            <HugeiconsIcon
              icon={Key01Icon}
              data-icon='inline-start'
              aria-hidden='true'
            />
            {t('Open API Keys')}
          </Button>
          <Button
            variant='outline'
            render={<Link to='/playground' />}
            nativeButton={false}
          >
            <HugeiconsIcon
              icon={PlayCircle02Icon}
              data-icon='inline-start'
              aria-hidden='true'
            />
            {t('Open Playground')}
          </Button>
          <Button
            variant='outline'
            render={
              <Link to='/usage-logs/$section' params={{ section: 'common' }} />
            }
            nativeButton={false}
          >
            <HugeiconsIcon
              icon={CheckmarkCircle02Icon}
              data-icon='inline-start'
              aria-hidden='true'
            />
            {t('Open Usage Logs')}
          </Button>
        </div>
      </HelpSection>

      <HelpSection
        title={t('Connect an AI app or your own code')}
        description={t(
          'Most OpenAI-compatible clients need only three values. Keep the key on the server, not in browser code.'
        )}
      >
        <div className='grid gap-3 md:grid-cols-3'>
          <GuideCard
            icon={ComputerTerminal01Icon}
            title={t('API base URL')}
            description={t(
              'Use this site address and the /v1 path expected by the client.'
            )}
          />
          <GuideCard
            icon={Key01Icon}
            title={t('API key')}
            description={t(
              'Use the key you created, including its full prefix. Never use an upstream provider key here.'
            )}
          />
          <GuideCard
            icon={CodeIcon}
            title={t('Model name')}
            description={t(
              'Paste the exact identifier from the Model Square. A friendly display name may not work.'
            )}
          />
        </div>
      </HelpSection>

      <HelpSection
        title={t('Connect Codex through the platform')}
        description={t(
          'Codex is a coding client, but the connection idea is the same: platform base URL, platform API key, and a compatible coding model.'
        )}
      >
        <div className='grid gap-3 lg:grid-cols-[1.1fr_0.9fr]'>
          <div className='bg-card rounded-xl border p-5'>
            <p className='text-xs font-semibold tracking-wide uppercase'>
              {t('Recommended: import with CC Switch')}
            </p>
            <ol className='text-muted-foreground mt-4 flex flex-col gap-3 text-sm leading-6'>
              <li>{t('1. Create or open an API key in API Keys.')}</li>
              <li>{t('2. Open the key actions and choose CC Switch.')}</li>
              <li>
                {t(
                  '3. Select Codex, choose an available coding model, and import the generated provider configuration.'
                )}
              </li>
              <li>
                {t(
                  '4. Start with a small repository task, then confirm the request in Usage Logs.'
                )}
              </li>
            </ol>
          </div>
          <div className='bg-muted/45 rounded-xl border p-5'>
            <p className='text-xs font-semibold tracking-wide uppercase'>
              {t('Manual configuration checklist')}
            </p>
            <dl className='mt-4 flex flex-col gap-4 text-sm'>
              <div>
                <dt className='font-medium'>{t('Base URL')}</dt>
                <dd className='text-muted-foreground mt-1'>
                  {t('This site address ending in /v1')}
                </dd>
              </div>
              <div>
                <dt className='font-medium'>{t('Authentication')}</dt>
                <dd className='text-muted-foreground mt-1'>
                  {t(
                    'Your platform API key, not a ChatGPT password or session'
                  )}
                </dd>
              </div>
              <div>
                <dt className='font-medium'>{t('API and model')}</dt>
                <dd className='text-muted-foreground mt-1'>
                  {t(
                    'Use the Responses API and a model that the Model Square shows as available to your group.'
                  )}
                </dd>
              </div>
            </dl>
          </div>
        </div>
      </HelpSection>

      <HelpSection
        title={t('If the first request fails')}
        description={t(
          'Read the HTTP status first, then open Usage Logs. The log is more useful than repeatedly changing random settings.'
        )}
      >
        <div className='overflow-hidden rounded-xl border'>
          {errors.map((error) => (
            <div
              key={error[0]}
              className='grid grid-cols-[4rem_1fr] gap-3 border-b p-4 text-sm last:border-b-0'
            >
              <code className='font-semibold'>{error[0]}</code>
              <p className='text-muted-foreground leading-6'>{error[1]}</p>
            </div>
          ))}
        </div>
      </HelpSection>

      <div className='bg-foreground text-background flex flex-col items-start justify-between gap-4 rounded-xl p-5 sm:flex-row sm:items-center'>
        <div>
          <h2 className='font-semibold'>{t('Ready for the first request?')}</h2>
          <p className='text-background/65 mt-1 text-sm'>
            {t(
              'Choose a model, create a limited test key, and prove the full path in the Playground.'
            )}
          </p>
        </div>
        <Button
          variant='secondary'
          render={<Link to='/pricing' />}
          nativeButton={false}
        >
          {t('Choose a model')}
          <HugeiconsIcon
            icon={ArrowRight01Icon}
            data-icon='inline-end'
            aria-hidden='true'
          />
        </Button>
      </div>
    </>
  )
}
