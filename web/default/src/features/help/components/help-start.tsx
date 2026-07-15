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
  InformationCircleIcon,
  Key01Icon,
  PlayCircle02Icon,
  UserAccountIcon,
  Wallet03Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { DocsButton } from '@/components/docs-button'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

import { GuideCard, HelpSection } from './help-primitives'

export function QuickStartContent() {
  const { t } = useTranslation()

  return (
    <div className='flex flex-col gap-10'>
      <Alert className='bg-primary/5 border-primary/20'>
        <HugeiconsIcon icon={InformationCircleIcon} strokeWidth={2} />
        <AlertTitle>{t('Before you begin')}</AlertTitle>
        <AlertDescription>
          {t(
            'Keep your API key private. Store it on the server and never expose it in browser code or public repositories.'
          )}
        </AlertDescription>
      </Alert>

      <HelpSection
        title={t('Four steps to your first request')}
        description={t(
          'Follow this path once, then reuse the same endpoint and key in any OpenAI-compatible application.'
        )}
      >
        <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
          <GuideCard
            icon={UserAccountIcon}
            eyebrow={t('Step {{number}}', { number: 1 })}
            title={t('Create an account')}
            description={t(
              'Register and sign in so your balance, keys, and usage stay under one account.'
            )}
          >
            <Button
              variant='outline'
              render={<Link to='/sign-up' />}
              nativeButton={false}
            >
              {t('Register')}
            </Button>
          </GuideCard>
          <GuideCard
            icon={Wallet03Icon}
            eyebrow={t('Step {{number}}', { number: 2 })}
            title={t('Add balance')}
            description={t(
              'Recharge your wallet or redeem a code before sending billable requests.'
            )}
          >
            <Button
              variant='outline'
              render={<Link to='/wallet' />}
              nativeButton={false}
            >
              {t('Open Wallet')}
            </Button>
          </GuideCard>
          <GuideCard
            icon={Key01Icon}
            eyebrow={t('Step {{number}}', { number: 3 })}
            title={t('Create an API key')}
            description={t(
              'Create a key, set an optional budget, and copy it when it is shown.'
            )}
          >
            <div className='flex flex-wrap gap-2'>
              <Button
                variant='outline'
                render={<Link to='/keys' />}
                nativeButton={false}
              >
                {t('Open API Keys')}
              </Button>
              <DocsButton compact />
            </div>
          </GuideCard>
          <GuideCard
            icon={PlayCircle02Icon}
            eyebrow={t('Step {{number}}', { number: 4 })}
            title={t('Send a test request')}
            description={t(
              'Use the playground first to verify the key, model, and account balance.'
            )}
          >
            <Button
              variant='outline'
              render={<Link to='/playground' />}
              nativeButton={false}
            >
              {t('Open Playground')}
            </Button>
          </GuideCard>
        </div>
      </HelpSection>

      <HelpSection title={t('Minimal request example')}>
        <div className='bg-muted/60 overflow-x-auto rounded-xl border p-4 text-sm'>
          <pre className='font-mono leading-6'>
            <code>{`curl $API_BASE_URL/v1/chat/completions \\
  -H "Authorization: Bearer $API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "Hello"}]
  }'`}</code>
          </pre>
        </div>
      </HelpSection>
    </div>
  )
}
