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
import { DESKS } from '../layout/office-layout'
import {
  findNearestIdleRouteNode,
  findRandomIdleRoute,
  getIdleRouteNode,
  pickRandomIdleDestination,
} from '../navigation/idle-route'
import { MovementSystem } from '../systems/MovementSystem'

function pathFromDesk(
  agent: Agent,
  route: readonly { x: number; y: number }[]
) {
  const desk = DESKS.find((candidate) => candidate.id === agent.assignedDeskId)
  const first = route[0]
  if (!desk || !first) return [...route]
  const sideX = first.x < desk.seatX ? desk.seatX - 82 : desk.seatX + 82
  return [{ x: sideX, y: desk.seatY }, { x: sideX, y: first.y }, ...route]
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
            idleNodeId: undefined,
            idleTargetNodeId: undefined,
            idleTargetState: undefined,
            idleTargetStayMs: undefined,
            idleTargetFacing: undefined,
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
        return agent
      }

      const atDesk = Math.hypot(agent.x - desk.seatX, agent.y - desk.seatY) < 12
      const currentNode =
        getIdleRouteNode(agent.idleNodeId) ??
        findNearestIdleRouteNode(agent.x, agent.y)
      const destination = pickRandomIdleDestination(currentNode.id)
      const route = findRandomIdleRoute(currentNode.id, destination.id)
      const needsGraphEntry =
        Math.hypot(agent.x - currentNode.x, agent.y - currentNode.y) >= 12
      const graphRoute = needsGraphEntry ? [currentNode, ...route] : route
      const path = atDesk ? pathFromDesk(agent, graphRoute) : graphRoute
      const [minStayMs, maxStayMs] = destination.stayMs
      const idleTargetStayMs =
        minStayMs + Math.random() * (maxStayMs - minStayMs)
      return {
        ...MovementSystem.assignWalkPath(agent, path),
        idleTargetNodeId: destination.id,
        idleTargetState: destination.state,
        idleTargetStayMs,
        idleTargetFacing: destination.facing,
      }
    })
  }
}
