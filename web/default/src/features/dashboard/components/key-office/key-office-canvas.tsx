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
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { KeyOfficeScene } from './scene/office-scene'
import type { KeyOfficeEmployee } from './types'

type KeyOfficeCanvasProps = {
  employees: KeyOfficeEmployee[]
}

export function KeyOfficeCanvas(props: KeyOfficeCanvasProps) {
  const { t } = useTranslation()
  const hostRef = useRef<HTMLDivElement>(null)
  const sceneRef = useRef<KeyOfficeScene | null>(null)
  const employeesRef = useRef(props.employees)
  const workingLabelRef = useRef((count: number) =>
    t('{{count}} active requests', { count })
  )

  employeesRef.current = props.employees
  workingLabelRef.current = (count: number) =>
    t('{{count}} active requests', { count })

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const scene = new KeyOfficeScene()
    sceneRef.current = scene
    let initialized = false

    const observer = new ResizeObserver((entries) => {
      const entry = entries[0]
      if (!entry) return
      const { width, height } = entry.contentRect
      if (width <= 0 || height <= 0) return
      if (!initialized) {
        initialized = true
        void scene.init(host, width, height).then(() => {
          scene.setEmployees(employeesRef.current, workingLabelRef.current)
        })
        return
      }
      scene.resize(width, height)
    })
    observer.observe(host)

    return () => {
      observer.disconnect()
      scene.destroy()
      sceneRef.current = null
    }
  }, [])

  useEffect(() => {
    sceneRef.current?.setEmployees(props.employees, workingLabelRef.current)
  }, [props.employees])

  return (
    <div
      ref={hostRef}
      className='h-full min-h-[420px] w-full overflow-hidden sm:min-h-[520px]'
      aria-label={t('API Key Office')}
      role='img'
    />
  )
}
