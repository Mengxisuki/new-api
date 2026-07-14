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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Button } from '@/components/ui/button'

import { HelpSection } from './help-primitives'

export function FaqContent() {
  const { t } = useTranslation()
  const questions = [
    {
      question: t('Why does my request return 401?'),
      answer: t(
        'Confirm that the Authorization header uses Bearer followed by an active API key. If the key was copied incorrectly or disabled, create a new one.'
      ),
    },
    {
      question: t('Why does my request return 429?'),
      answer: t(
        'The account, key, user group, or upstream channel may have reached a rate limit. Reduce concurrency and review the response message and usage logs.'
      ),
    },
    {
      question: t('Why is a model unavailable?'),
      answer: t(
        'Model availability depends on your group, key permissions, and current channels. Check the Model Square and the model restrictions on your key.'
      ),
    },
    {
      question: t('How is usage charged?'),
      answer: t(
        'Charges depend on the model, user group, input and output usage, and any model-specific billing factors. The final deduction is recorded in Usage Logs.'
      ),
    },
    {
      question: t('What should I provide when reporting a problem?'),
      answer: t(
        'Provide the request time, model, endpoint, status code, and request ID from Usage Logs. Never send your complete API key.'
      ),
    },
  ]

  return (
    <div className='flex flex-col gap-10'>
      <HelpSection
        title={t('Frequently asked questions')}
        description={t(
          'Start with the response status and Usage Logs. They usually identify whether the issue is authentication, quota, rate limiting, or model access.'
        )}
      >
        <Accordion className='rounded-xl border px-4'>
          {questions.map((item, index) => (
            <AccordionItem key={item.question} value={`question-${index}`}>
              <AccordionTrigger className='text-left'>
                {item.question}
              </AccordionTrigger>
              <AccordionContent className='text-muted-foreground leading-6'>
                {item.answer}
              </AccordionContent>
            </AccordionItem>
          ))}
        </Accordion>
      </HelpSection>

      <div className='bg-muted/50 flex flex-col items-start gap-3 rounded-xl border p-5'>
        <h2 className='font-medium'>{t('Still investigating?')}</h2>
        <p className='text-muted-foreground max-w-2xl text-sm leading-6'>
          {t(
            'Open Usage Logs to inspect the request details before contacting the support channel configured by your site operator.'
          )}
        </p>
        <Button
          variant='outline'
          render={
            <Link to='/usage-logs/$section' params={{ section: 'common' }} />
          }
          nativeButton={false}
        >
          {t('Open Usage Logs')}
        </Button>
      </div>
    </div>
  )
}
