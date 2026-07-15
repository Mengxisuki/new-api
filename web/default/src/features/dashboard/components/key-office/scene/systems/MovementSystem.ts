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
import type { AgentEntity } from '@/features/dashboard/components/key-office/scene/entities/AgentEntity'
import { DESKS } from '@/features/dashboard/components/key-office/scene/layout/office-layout'
import {
  resolveWalkViewFacing,
  viewFacingToLR,
} from '@/features/dashboard/components/key-office/scene/systems/movementFacing'
import type { Agent } from '@/features/dashboard/components/key-office/types/agent'

const ARRIVE_THRESHOLD = 6
const WALK_SPEED = 90

function isHomeDeskSeat(
  deskId: string | undefined,
  x: number,
  y: number
): boolean {
  if (!deskId) return false
  const desk = DESKS.find((d) => d.id === deskId)
  if (!desk) return false
  return Math.hypot(x - desk.seatX, y - desk.seatY) < ARRIVE_THRESHOLD + 8
}

export class MovementSystem {
  update(entities: Map<string, AgentEntity>, dt: number): boolean {
    let anyMoving = false

    for (const entity of entities.values()) {
      const agent = entity.data
      if (
        agent.state !== 'walking' ||
        agent.targetX == null ||
        agent.targetY == null
      ) {
        continue
      }

      anyMoving = true
      const dx = agent.targetX - agent.x
      const dy = agent.targetY - agent.y
      const dist = Math.hypot(dx, dy)

      if (dist < ARRIVE_THRESHOLD) {
        const tx = agent.targetX
        const ty = agent.targetY
        const hasMore =
          agent.walkPath != null &&
          agent.walkPathIndex != null &&
          agent.walkPathIndex < agent.walkPath.length - 1

        if (hasMore) {
          const nextIndex = (agent.walkPathIndex ?? 0) + 1
          const next = agent.walkPath?.[nextIndex]
          if (!next) continue
          const ndx = next.x - tx
          const ndy = next.y - ty
          const viewFacing = resolveWalkViewFacing(ndx, ndy)
          entity.apply({
            x: tx,
            y: ty,
            walkPathIndex: nextIndex,
            targetX: next.x,
            targetY: next.y,
            viewFacing,
            facing: viewFacingToLR(viewFacing),
          })
          entity.setPosition(tx, ty)
          continue
        }

        const atHome = isHomeDeskSeat(agent.assignedDeskId, tx, ty)
        const shouldWork = atHome && agent.desiredState === 'working'
        const viewFacing = shouldWork ? 'back' : resolveWalkViewFacing(dx, dy)
        entity.apply({
          x: tx,
          y: ty,
          targetX: undefined,
          targetY: undefined,
          walkPath: undefined,
          walkPathIndex: undefined,
          state: shouldWork ? 'working' : 'idle',
          viewFacing,
          facing: viewFacingToLR(viewFacing),
          currentTask: shouldWork ? agent.currentTask : undefined,
          idleMoveAt: shouldWork
            ? undefined
            : Date.now() + 2500 + Math.random() * 4500,
        })
        entity.setPosition(tx, ty)
        continue
      }

      const step = Math.min(WALK_SPEED * dt, dist)
      const nx = agent.x + (dx / dist) * step
      const ny = agent.y + (dy / dist) * step
      const viewFacing = resolveWalkViewFacing(dx, dy)

      entity.apply({
        x: nx,
        y: ny,
        viewFacing,
        facing: viewFacingToLR(viewFacing),
      })
      entity.setPosition(nx, ny)
    }

    return anyMoving
  }

  static assignWalkPath(
    agent: Agent,
    points: { x: number; y: number }[]
  ): Agent {
    if (points.length === 0) return agent
    const first = points[0]
    const dx = first.x - agent.x
    const dy = first.y - agent.y
    const viewFacing = resolveWalkViewFacing(dx, dy)
    return {
      ...agent,
      state: 'walking',
      walkPath: points,
      walkPathIndex: 0,
      targetX: first.x,
      targetY: first.y,
      currentTask:
        agent.desiredState === 'working' ? agent.currentTask : undefined,
      bubbleText: undefined,
      viewFacing,
      facing: viewFacingToLR(viewFacing),
    }
  }
}
