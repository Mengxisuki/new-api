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
import { Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { CHIBI_CHARACTER_SKINS } from '@/features/dashboard/components/key-office/scene/characters/chibiStickerSkins'
import { resolveOfficeCharacter } from '@/features/dashboard/components/key-office/types'
import { cn } from '@/lib/utils'

import { updateApiKeyOfficeCharacter } from '../../api'
import { useApiKeys } from '../api-keys-provider'
import { OfficeCharacterPreview } from './office-character-preview'

export function OfficeCharacterDialog() {
  const { t } = useTranslation()
  const { open, setOpen, currentRow, triggerRefresh } = useApiKeys()
  const isOpen = open === 'office-character'
  const [selected, setSelected] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!isOpen || !currentRow) return
    setSelected(
      resolveOfficeCharacter(currentRow.id, currentRow.office_character)
    )
  }, [currentRow, isOpen])

  if (!currentRow) return null

  const handleSave = async () => {
    if (!selected) return
    setSaving(true)
    try {
      const result = await updateApiKeyOfficeCharacter(currentRow.id, selected)
      if (!result.success) {
        toast.error(result.message || t('Failed to update office character'))
        return
      }
      toast.success(t('Office character updated'))
      triggerRefresh()
      setOpen(null)
    } catch {
      toast.error(t('Failed to update office character'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open={isOpen}
      onOpenChange={(nextOpen) => !nextOpen && setOpen(null)}
    >
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('Edit office character')}</DialogTitle>
          <DialogDescription>
            {t('Choose how {{name}} appears in the API key office.', {
              name: currentRow.name,
            })}
          </DialogDescription>
        </DialogHeader>

        <OfficeCharacterPreview character={selected} tokenId={currentRow.id} />

        <div className='grid grid-cols-3 gap-2' role='radiogroup'>
          {CHIBI_CHARACTER_SKINS.map((character, index) => (
            <button
              key={character}
              type='button'
              role='radio'
              aria-checked={selected === character}
              onClick={() => setSelected(character)}
              className={cn(
                'focus-visible:ring-ring rounded-lg border px-3 py-2 text-left text-sm outline-none transition-colors focus-visible:ring-2',
                selected === character
                  ? 'border-emerald-600 bg-emerald-50 text-emerald-950 dark:border-emerald-400 dark:bg-emerald-950 dark:text-emerald-50'
                  : 'hover:bg-muted'
              )}
            >
              <span className='text-muted-foreground block text-[10px] font-semibold tracking-widest'>
                {String(index + 1).padStart(2, '0')}
              </span>
              <span>{t('Character {{number}}', { number: index + 1 })}</span>
            </button>
          ))}
        </div>

        <DialogFooter>
          <Button variant='outline' onClick={() => setOpen(null)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSave} disabled={saving || !selected}>
            {saving && <Loader2 className='animate-spin' aria-hidden='true' />}
            {t('Save character')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
