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
import { Application } from 'pixi.js'
import { useEffect, useRef } from 'react'

import { loadSpineAssets } from '@/features/dashboard/components/key-office/scene/assets/loadSpineAssets'
import { SpineCharacter } from '@/features/dashboard/components/key-office/scene/characters/SpineCharacter'

type OfficeCharacterPreviewProps = {
  character: string
  tokenId: number
}

export function OfficeCharacterPreview(props: OfficeCharacterPreviewProps) {
  const hostRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    let disposed = false
    let initialized = false
    const app = new Application()

    void app
      .init({
        width: 260,
        height: 220,
        backgroundAlpha: 0,
        antialias: true,
        resolution: Math.min(window.devicePixelRatio || 1, 2),
        autoDensity: true,
      })
      .then(async () => {
        initialized = true
        if (disposed) {
          app.destroy(true, { children: true })
          return
        }
        host.appendChild(app.canvas)
        const ready = await loadSpineAssets()
        if (!ready || disposed) return
        const character = new SpineCharacter(
          `preview-${props.tokenId}`,
          0x4a90d9,
          props.character,
          props.tokenId
        )
        character.position.set(130, 168)
        character.scale.set(1.35)
        character.setViewFacing('front')
        character.playState('idle')
        app.stage.addChild(character)
      })

    return () => {
      disposed = true
      if (initialized) app.destroy(true, { children: true })
    }
  }, [props.character, props.tokenId])

  return (
    <div
      ref={hostRef}
      className='flex h-[220px] w-full items-center justify-center overflow-hidden rounded-xl border bg-[radial-gradient(circle_at_50%_70%,rgba(16,185,129,0.16),transparent_45%)]'
    />
  )
}
