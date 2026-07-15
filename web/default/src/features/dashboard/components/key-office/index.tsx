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
import { useQuery } from '@tanstack/react-query'
import { Activity, KeyRound, PersonStanding, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { getKeyActivity } from '@/features/dashboard/api'

import { KeyOfficeCanvas } from './key-office-canvas'

export default function KeyActivityOffice() {
  const { t } = useTranslation()
  const activityQuery = useQuery({
    queryKey: ['dashboard', 'key-activity'],
    queryFn: getKeyActivity,
    refetchInterval: () =>
      document.visibilityState === 'visible' ? 2000 : 10000,
  })
  const employees = activityQuery.data?.data?.tokens ?? []
  const workingCount = employees.filter(
    (employee) => employee.state === 'working'
  ).length
  const activeRequests = employees.reduce(
    (sum, employee) => sum + employee.active_requests,
    0
  )

  if (activityQuery.isLoading) {
    return <Skeleton className='h-[620px] w-full rounded-b-xl' />
  }

  return (
    <section className='relative overflow-hidden rounded-xl border bg-[#f7f3e8] shadow-xs dark:bg-[#171b18]'>
      <header className='relative flex flex-col gap-4 border-b border-black/5 px-4 py-4 sm:flex-row sm:items-center sm:justify-between sm:px-5 dark:border-white/10'>
        <div>
          <div className='flex items-center gap-2'>
            <span className='flex size-8 items-center justify-center rounded-lg bg-emerald-950 text-emerald-50 shadow-sm dark:bg-emerald-200 dark:text-emerald-950'>
              <KeyRound className='size-4' aria-hidden='true' />
            </span>
            <div>
              <h2 className='font-semibold tracking-tight'>
                {t('API Key Office')}
              </h2>
              <p className='text-muted-foreground text-xs'>
                {t('Showing your six most active API keys')}
              </p>
            </div>
          </div>
        </div>
        <div className='flex flex-wrap gap-2'>
          <Badge
            variant='outline'
            className='bg-background/70 gap-1.5 backdrop-blur'
          >
            <PersonStanding className='size-3.5' aria-hidden='true' />
            {t('{{count}} keys on shift', { count: employees.length })}
          </Badge>
          <Badge
            variant='outline'
            className='bg-background/70 gap-1.5 backdrop-blur'
          >
            <Activity
              className='size-3.5 text-emerald-600'
              aria-hidden='true'
            />
            {t('{{count}} working', { count: workingCount })}
          </Badge>
          <Badge variant='outline' className='bg-background/70 backdrop-blur'>
            {t('{{count}} active requests', { count: activeRequests })}
          </Badge>
        </div>
      </header>

      {employees.length > 0 ? (
        <>
          <KeyOfficeCanvas employees={employees} />
          <ul className='sr-only'>
            {employees.map((employee) => (
              <li key={employee.id}>
                {employee.name}:{' '}
                {t(employee.state === 'working' ? 'Working' : 'Idle')}
              </li>
            ))}
          </ul>
        </>
      ) : (
        <div className='relative flex min-h-[520px] flex-col items-center justify-center px-6 text-center'>
          <span className='bg-background/80 mb-4 flex size-14 items-center justify-center rounded-2xl border shadow-sm'>
            <KeyRound
              className='text-muted-foreground size-6'
              aria-hidden='true'
            />
          </span>
          <h3 className='font-medium'>{t('No API keys in the office yet')}</h3>
          <p className='text-muted-foreground mt-1 max-w-sm text-sm'>
            {t('Create an API key and its character will appear here.')}
          </p>
        </div>
      )}

      <footer className='border-t border-black/5 px-3 py-3 sm:px-5 dark:border-white/10'>
        <Alert
          role='note'
          className='border-emerald-900/10 bg-emerald-950/[0.035] px-3 py-2.5 text-emerald-950 dark:border-emerald-100/10 dark:bg-emerald-100/[0.045] dark:text-emerald-50'
        >
          <ShieldCheck className='mt-0.5 size-4' aria-hidden='true' />
          <AlertTitle className='text-xs font-semibold'>
            {t('Your request content stays private')}
          </AlertTitle>
          <AlertDescription className='text-xs leading-relaxed text-emerald-950/65 dark:text-emerald-50/65'>
            {t(
              'This page only retains request status. The platform does not store request content; only necessary token usage and user activity statistics are retained.'
            )}
          </AlertDescription>
        </Alert>
      </footer>
    </section>
  )
}
