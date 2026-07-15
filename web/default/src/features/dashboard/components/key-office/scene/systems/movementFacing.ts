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
import type { ChibiFacing } from '@/features/dashboard/components/key-office/scene/characters/chibiAgentPresets'

/**
 * 根据「朝目标移动」的位移判断四向（Pixi：x 右为正，y 下为正）
 * - 横向分量 ≥ 纵向 → 左跑 / 右跑
 * - 否则 → 上跑背面 / 下跑正面
 */
export function resolveWalkViewFacing(dx: number, dy: number): ChibiFacing {
  const absDx = Math.abs(dx)
  const absDy = Math.abs(dy)
  if (absDx < 0.5 && absDy < 0.5) return 'front'

  if (absDx >= absDy) {
    return dx < 0 ? 'left' : 'right'
  }
  return dy < 0 ? 'back' : 'front'
}

export function viewFacingToLR(facing: ChibiFacing): 1 | -1 {
  return facing === 'left' ? -1 : 1
}

const TALK_FACE_DX = 6

/** 对话时朝向对方：目标在左则面向左，在右则面向右 */
export function resolveTalkViewFacing(
  fromX: number,
  fromY: number,
  targetX: number,
  targetY: number
): ChibiFacing {
  const dx = targetX - fromX
  if (dx < -TALK_FACE_DX) return 'left'
  if (dx > TALK_FACE_DX) return 'right'
  const dy = targetY - fromY
  return dy < 0 ? 'back' : 'front'
}

export function talkFacingToward(
  fromX: number,
  fromY: number,
  targetX: number,
  targetY: number
): { viewFacing: ChibiFacing; facing: 1 | -1 } {
  const viewFacing = resolveTalkViewFacing(fromX, fromY, targetX, targetY)
  return { viewFacing, facing: viewFacingToLR(viewFacing) }
}

export function isHorizontalWalk(dx: number, dy: number): boolean {
  return Math.abs(dx) >= Math.abs(dy)
}
