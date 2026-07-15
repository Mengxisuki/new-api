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
  Chart01Icon,
  ComputerTerminal01Icon,
  Key01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { HelpSection } from './help-primitives'

export function BeginnerCallChain() {
  const { t } = useTranslation()
  const callChain = [
    {
      icon: ComputerTerminal01Icon,
      title: t('Your app or Codex'),
      description: t('Sends the prompt, model name, and request parameters.'),
    },
    {
      icon: Key01Icon,
      title: t('API key and group'),
      description: t(
        'Identifies your account and decides which groups and budgets may be used.'
      ),
    },
    {
      icon: ApiIcon,
      title: t('Platform gateway'),
      description: t(
        'Checks access, finds an available channel, and forwards the request upstream.'
      ),
    },
    {
      icon: Chart01Icon,
      title: t('Response and billing'),
      description: t(
        'Returns the answer, records tokens and latency, then deducts the actual cost.'
      ),
    },
  ]
  const modelFacts = [
    [
      t('Model name'),
      t(
        'Copy the identifier exactly. It is the value sent in the model field and is case-sensitive.'
      ),
    ],
    [
      t('Endpoints'),
      t(
        'Confirm the model supports the API your client uses, such as Chat Completions or Responses.'
      ),
    ],
    [
      t('Input and output'),
      t(
        'Check whether the model accepts text, images, audio, files, or video, and what it can generate.'
      ),
    ],
    [
      t('Context and output limit'),
      t(
        'Context is the total room for instructions, history, files, and the reply. The output limit caps the reply length.'
      ),
    ],
    [
      t('Capabilities'),
      t(
        'Look for tools, reasoning, vision, structured output, streaming, or caching when your task needs them.'
      ),
    ],
    [
      t('Price and billing unit'),
      t(
        'Check input, output, and cached-input prices. Some models instead charge per request, image, second, or another unit.'
      ),
    ],
    [
      t('Groups and provider'),
      t(
        'Groups affect availability and price. Provider information tells you the model family or upstream source.'
      ),
    ],
    [
      t('Performance and stability'),
      t(
        'Use latency and success-rate information when available; the cheapest model is not always the best production choice.'
      ),
    ],
  ]

  return (
    <>
      <HelpSection
        title={t('How one AI request travels through the platform')}
        description={t(
          'You call one stable API. The platform chooses an eligible upstream channel, returns the result, and writes a usage record.'
        )}
      >
        <ol className='grid gap-3 lg:grid-cols-4'>
          {callChain.map((step, index) => (
            <li key={step.title} className='relative min-w-0'>
              <div className='bg-card h-full rounded-xl border p-4'>
                <div className='mb-4 flex items-center justify-between'>
                  <span className='bg-primary/10 text-primary flex size-9 items-center justify-center rounded-lg'>
                    <HugeiconsIcon
                      icon={step.icon}
                      strokeWidth={2}
                      className='size-4'
                      aria-hidden='true'
                    />
                  </span>
                  <span className='text-muted-foreground text-xs font-semibold'>
                    {index + 1}
                  </span>
                </div>
                <h3 className='text-sm font-semibold'>{step.title}</h3>
                <p className='text-muted-foreground mt-1 text-sm leading-6'>
                  {step.description}
                </p>
              </div>
              {index < callChain.length - 1 && (
                <HugeiconsIcon
                  icon={ArrowRight01Icon}
                  strokeWidth={2}
                  aria-hidden='true'
                  className='text-muted-foreground/40 absolute top-1/2 -right-5 z-10 hidden size-4 -translate-y-1/2 lg:block'
                />
              )}
            </li>
          ))}
        </ol>
      </HelpSection>

      <HelpSection
        title={t('How to read the Model Square')}
        description={t(
          'Do not choose by name alone. Open a model and check these items before you copy its identifier.'
        )}
      >
        <div className='grid overflow-hidden rounded-xl border sm:grid-cols-2'>
          {modelFacts.map((fact) => (
            <div
              key={fact[0]}
              className='border-border/70 border-b p-4 last:border-b-0 sm:border-r sm:even:border-r-0 sm:[&:nth-last-child(-n+2)]:border-b-0'
            >
              <h3 className='text-sm font-semibold'>{fact[0]}</h3>
              <p className='text-muted-foreground mt-1 text-sm leading-6'>
                {fact[1]}
              </p>
            </div>
          ))}
        </div>
        <div className='mt-1'>
          <Button render={<Link to='/pricing' />} nativeButton={false}>
            {t('Open Model Square')}
            <HugeiconsIcon
              icon={ArrowRight01Icon}
              data-icon='inline-end'
              aria-hidden='true'
            />
          </Button>
        </div>
      </HelpSection>
    </>
  )
}
