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
import { Loader2, UserRound } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import type { UpdateUserRequest, UserProfile } from '../types'

interface ProfileDisplayNameSectionProps {
  profile: UserProfile | null
  updating: boolean
  onSave: (data: UpdateUserRequest) => Promise<boolean>
}

const DISPLAY_NAME_MAX_LENGTH = 20

export function ProfileDisplayNameSection(
  props: ProfileDisplayNameSectionProps
) {
  const { t } = useTranslation()
  const [displayName, setDisplayName] = useState('')

  useEffect(() => {
    setDisplayName(props.profile?.display_name || props.profile?.username || '')
  }, [props.profile?.display_name, props.profile?.username])

  if (!props.profile) {
    return null
  }

  const normalizedDisplayName = displayName.trim() || props.profile.username
  const currentDisplayName =
    props.profile.display_name || props.profile.username || ''
  const isUnchanged = normalizedDisplayName === currentDisplayName

  const handleSave = async () => {
    if (normalizedDisplayName.length > DISPLAY_NAME_MAX_LENGTH) {
      toast.error(t('Display name must be at most 20 characters long'))
      return
    }
    const success = await props.onSave({
      display_name: normalizedDisplayName,
    })
    if (success) {
      setDisplayName(normalizedDisplayName)
    }
  }

  return (
    <div className='rounded-xl border p-3 sm:p-4 mb-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-end'>
        <div className='min-w-0 flex-1 space-y-2'>
          <div className='flex items-center gap-2 text-sm font-medium'>
            <UserRound className='h-4 w-4' />
            <span>{t('Display Name')}</span>
          </div>
          <Input
            value={displayName}
            maxLength={DISPLAY_NAME_MAX_LENGTH}
            placeholder={t('Enter display name')}
            onChange={(event) => setDisplayName(event.target.value)}
          />
          <p className='text-muted-foreground text-xs'>
            {t('Shown publicly. Does not change your login username.')}
          </p>
        </div>
        <Button
          className='sm:min-w-28'
          disabled={props.updating || isUnchanged}
          onClick={handleSave}
        >
          {props.updating ? <Loader2 className='h-4 w-4 animate-spin' /> : null}
          {t('Save changes')}
        </Button>
      </div>
    </div>
  )
}
