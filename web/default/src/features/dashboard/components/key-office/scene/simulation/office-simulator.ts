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
import type { Agent } from '../../types/agent'
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
import { DESKS, IDLE_WAYPOINTS } from '../layout/office-layout'
import { MovementSystem } from '../systems/MovementSystem'

function pickIdleWaypoint(agent: Agent) {
  const offset = Math.floor(Date.now() / 5000)
  const index = Math.abs(agent.tokenId + offset) % IDLE_WAYPOINTS.length
  return IDLE_WAYPOINTS[index]
}

function pathFromDesk(agent: Agent, target: { x: number; y: number }) {
  const desk = DESKS.find((candidate) => candidate.id === agent.assignedDeskId)
  if (!desk) return [target]
  const sideX = target.x < desk.seatX ? desk.seatX - 82 : desk.seatX + 82
  return [{ x: sideX, y: desk.seatY }, { x: sideX, y: target.y }, target]
}

function pathToDesk(agent: Agent) {
  const desk = DESKS.find((candidate) => candidate.id === agent.assignedDeskId)
  if (!desk) return []
  const sideX = agent.x < desk.seatX ? desk.seatX - 82 : desk.seatX + 82
  return [
    { x: sideX, y: agent.y },
    { x: sideX, y: desk.seatY },
    { x: desk.seatX, y: desk.seatY },
  ]
}

function pathBetweenIdlePoints(agent: Agent, target: { x: number; y: number }) {
  if (agent.x < 480 === target.x < 480) return [target]
  return [{ x: agent.x, y: 555 }, { x: target.x, y: 555 }, target]
}

export class OfficeSimulator {
  tick(agents: Agent[]): Agent[] {
    const now = Date.now()
    return agents.map((agent) => {
      const desk = DESKS.find(
        (candidate) => candidate.id === agent.assignedDeskId
      )
      if (!desk) return agent

      if (agent.desiredState === 'working') {
        const atDesk =
          Math.hypot(agent.x - desk.seatX, agent.y - desk.seatY) < 12
        if (atDesk && agent.state !== 'walking') {
          return {
            ...agent,
            x: desk.seatX,
            y: desk.seatY,
            state: 'working',
            currentTask: agent.currentTask,
            viewFacing: 'back',
            targetX: undefined,
            targetY: undefined,
            walkPath: undefined,
            walkPathIndex: undefined,
            idleMoveAt: undefined,
          }
        }
        const finalWaypoint = agent.walkPath?.at(-1)
        const isAlreadyReturning =
          finalWaypoint != null &&
          Math.hypot(
            finalWaypoint.x - desk.seatX,
            finalWaypoint.y - desk.seatY
          ) < 12
        if (agent.state === 'walking' && isAlreadyReturning) return agent
        return MovementSystem.assignWalkPath(agent, pathToDesk(agent))
      }

      if (agent.state === 'walking') return agent
      if ((agent.idleMoveAt ?? 0) > now) {
        return { ...agent, state: 'idle', currentTask: undefined }
      }

      const destination = pickIdleWaypoint(agent)
      const atDesk = Math.hypot(agent.x - desk.seatX, agent.y - desk.seatY) < 12
      const path = atDesk
        ? pathFromDesk(agent, destination)
        : pathBetweenIdlePoints(agent, destination)
      return MovementSystem.assignWalkPath(agent, path)
    })
  }
}
