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
import { InformationCircleIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

import { BeginnerCallChain } from './help-beginner-call-chain'
import { BeginnerIntegration } from './help-beginner-integration'
import { BeginnerPricing } from './help-beginner-pricing'

export function GettingStartedContent() {
  const { t } = useTranslation()

  return (
    <div className='flex flex-col gap-12'>
      <Alert className='border-primary/20 bg-primary/5'>
        <HugeiconsIcon
          icon={InformationCircleIcon}
          strokeWidth={2}
          aria-hidden='true'
        />
        <AlertTitle>{t('You only need to understand one path')}</AlertTitle>
        <AlertDescription>
          {t(
            'Choose a model, create a key, send a request, and check the usage log. The platform handles upstream routing and billing in between.'
          )}
        </AlertDescription>
      </Alert>

      <BeginnerCallChain />
      <BeginnerPricing />
      <BeginnerIntegration />
    </div>
  )
}
