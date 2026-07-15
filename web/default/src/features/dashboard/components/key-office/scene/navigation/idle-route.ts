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
  IDLE_ROUTE_EDGES,
  IDLE_ROUTE_NODES,
  type IdleRouteNode,
} from '../layout/office-layout'

const nodesById = new Map<string, IdleRouteNode>(
  IDLE_ROUTE_NODES.map((node) => [node.id, node] as const)
)
const neighborsById = new Map<string, string[]>()

for (const [fromId, toId] of IDLE_ROUTE_EDGES) {
  const fromNeighbors = neighborsById.get(fromId) ?? []
  fromNeighbors.push(toId)
  neighborsById.set(fromId, fromNeighbors)

  const toNeighbors = neighborsById.get(toId) ?? []
  toNeighbors.push(fromId)
  neighborsById.set(toId, toNeighbors)
}

export function getIdleRouteNode(id: string | undefined) {
  return id ? nodesById.get(id) : undefined
}

export function findNearestIdleRouteNode(x: number, y: number) {
  let nearest: IdleRouteNode = IDLE_ROUTE_NODES[0]
  let nearestDistance = Number.POSITIVE_INFINITY
  for (const node of IDLE_ROUTE_NODES) {
    const distance = Math.hypot(node.x - x, node.y - y)
    if (distance >= nearestDistance) continue
    nearest = node
    nearestDistance = distance
  }
  return nearest
}

export function pickRandomIdleDestination(
  currentNodeId: string,
  random: () => number = Math.random
) {
  const candidates = IDLE_ROUTE_NODES.filter(
    (node) => node.id !== currentNodeId
  )
  if (candidates.length === 0) return IDLE_ROUTE_NODES[0]
  const index = Math.min(
    Math.floor(random() * candidates.length),
    candidates.length - 1
  )
  return candidates[index]
}

/**
 * 随机深度优先寻路。邻接点每次都会重新洗牌，因此有环图中的线路会变化。
 * 返回结果不包含起点，调用方可以直接作为后续行走队列。
 */
export function findRandomIdleRoute(
  fromId: string,
  toId: string,
  random: () => number = Math.random
): IdleRouteNode[] {
  if (fromId === toId) return []

  const visited = new Set<string>()
  const route: string[] = []

  const visit = (nodeId: string): boolean => {
    visited.add(nodeId)
    route.push(nodeId)
    if (nodeId === toId) return true

    const neighbors = [...(neighborsById.get(nodeId) ?? [])]
    for (let index = neighbors.length - 1; index > 0; index--) {
      const swapIndex = Math.min(Math.floor(random() * (index + 1)), index)
      const current = neighbors[index]
      neighbors[index] = neighbors[swapIndex]
      neighbors[swapIndex] = current
    }

    for (const neighborId of neighbors) {
      if (visited.has(neighborId)) continue
      if (visit(neighborId)) return true
    }

    route.pop()
    return false
  }

  if (!visit(fromId)) return []
  const nodes: IdleRouteNode[] = []
  for (const nodeId of route.slice(1)) {
    const node = nodesById.get(nodeId)
    if (node) nodes.push(node)
  }
  return nodes
}
