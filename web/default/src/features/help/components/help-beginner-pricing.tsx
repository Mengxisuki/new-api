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
  Chart01Icon,
  InformationCircleIcon,
  Wallet03Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

import { GuideCard, HelpSection } from './help-primitives'

export function BeginnerPricing() {
  const { t } = useTranslation()
  const routingTerms = [
    [
      t('Group'),
      t(
        'A pricing and access lane attached to your API key. Different groups can expose different models and use different multipliers.'
      ),
    ],
    [
      t('Provider'),
      t(
        'The company or model family behind a service, such as the upstream organization shown in the Model Square.'
      ),
    ],
    [
      t('Channel'),
      t(
        'A real upstream connection configured by the platform. One model may have multiple channels for availability and failover.'
      ),
    ],
  ]

  return (
    <>
      <HelpSection
        title={t('Tokens and pricing, explained simply')}
        description={t(
          'A token is a small piece of text, not exactly one character or one word. Your prompt and the model reply are counted separately.'
        )}
      >
        <div className='grid gap-3 md:grid-cols-3'>
          <GuideCard
            icon={Wallet03Icon}
            eyebrow={t('You send')}
            title={t('Input tokens')}
            description={t(
              'System instructions, chat history, your new message, tool definitions, and readable file content can all count as input.'
            )}
          />
          <GuideCard
            icon={Chart01Icon}
            eyebrow={t('Model returns')}
            title={t('Output tokens')}
            description={t(
              'The answer and reasoning output are billed at the output rate, which is often higher than the input rate.'
            )}
          />
          <GuideCard
            icon={InformationCircleIcon}
            eyebrow={t('Reused context')}
            title={t('Cached input')}
            description={t(
              'Some providers discount repeated prompt content. A cached-input price only applies when the upstream cache is actually used.'
            )}
          />
        </div>

        <div className='bg-muted/45 rounded-xl border p-5'>
          <p className='text-xs font-semibold tracking-wide uppercase'>
            {t('Basic token cost formula')}
          </p>
          <p className='mt-3 font-mono text-sm leading-7'>
            {t(
              'cost = input tokens × input price + output tokens × output price + cached tokens × cached price'
            )}
          </p>
          <p className='text-muted-foreground mt-2 text-sm leading-6'>
            {t(
              'Divide each token count by the unit shown in the Model Square, usually 1K or 1M. The usage log shows the final token counts and charge.'
            )}
          </p>
        </div>

        <Alert>
          <HugeiconsIcon
            icon={InformationCircleIcon}
            strokeWidth={2}
            aria-hidden='true'
          />
          <AlertTitle>
            {t('Treat the displayed price as the current price')}
          </AlertTitle>
          <AlertDescription>
            {t(
              'Platform pricing is generally based on upstream official pricing or a model baseline, then adjusted by model rules, group multipliers, and dynamic controls. Prices can change, and some models use per-request or tiered billing, so check the Model Square before large jobs.'
            )}
          </AlertDescription>
        </Alert>
      </HelpSection>

      <HelpSection
        title={t('Group, provider, and channel are different things')}
        description={t(
          'These three words describe different parts of the same request. Understanding them makes price changes and failover behavior much easier to read.'
        )}
      >
        <div className='grid gap-3 md:grid-cols-3'>
          {routingTerms.map((term) => (
            <div key={term[0]} className='bg-card rounded-xl border p-5'>
              <h3 className='text-base font-semibold'>{term[0]}</h3>
              <p className='text-muted-foreground mt-2 text-sm leading-6'>
                {term[1]}
              </p>
            </div>
          ))}
        </div>
        <div className='border-primary/20 bg-primary/5 rounded-xl border p-5'>
          <p className='text-sm font-semibold'>{t('What usually happens')}</p>
          <p className='text-muted-foreground mt-1 text-sm leading-6'>
            {t(
              'Your API key selects an allowed group. The requested model and group narrow the eligible channels. The platform then routes to an available upstream channel; you normally do not choose a channel yourself. Billing uses the group that actually serves the request.'
            )}
          </p>
        </div>
      </HelpSection>
    </>
  )
}
