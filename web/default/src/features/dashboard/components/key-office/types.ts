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
  CHIBI_CHARACTER_SKINS,
  type ChibiCharacterSkin,
} from './scene/characters/chibiStickerSkins'

export type KeyOfficeEmployee = {
  id: number
  name: string
  status: number
  office_character: string
  active_requests: number
  requests_24h: number
  last_used_at: number
  state: 'working' | 'idle'
}

export type KeyActivityResponse = {
  success: boolean
  message?: string
  data?: {
    server_time: number
    tokens: KeyOfficeEmployee[]
  }
}

export function resolveOfficeCharacter(
  tokenId: number,
  configured?: string
): ChibiCharacterSkin {
  if (
    configured &&
    CHIBI_CHARACTER_SKINS.includes(configured as ChibiCharacterSkin)
  ) {
    return configured as ChibiCharacterSkin
  }
  return CHIBI_CHARACTER_SKINS[Math.abs(tokenId) % CHIBI_CHARACTER_SKINS.length]
}
