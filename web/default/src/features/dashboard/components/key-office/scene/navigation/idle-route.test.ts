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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { IDLE_ROUTE_EDGES, IDLE_ROUTE_NODES } from '../layout/office-layout'
import { findRandomIdleRoute, pickRandomIdleDestination } from './idle-route'

const edgeKeys = new Set(
  IDLE_ROUTE_EDGES.flatMap(([fromId, toId]) => [
    `${fromId}:${toId}`,
    `${toId}:${fromId}`,
  ])
)

describe('key office idle route graph', () => {
  test('keeps every configured edge connected to existing nodes', () => {
    const nodeIds = new Set(IDLE_ROUTE_NODES.map((node) => node.id))

    for (const [fromId, toId] of IDLE_ROUTE_EDGES) {
      assert.equal(nodeIds.has(fromId), true)
      assert.equal(nodeIds.has(toId), true)
      assert.notEqual(fromId, toId)
    }
  })

  test('builds a route using only visible graph edges', () => {
    const route = findRandomIdleRoute('L1', 'R1', () => 0.5)
    const routeIds = ['L1', ...route.map((node) => node.id)]

    assert.equal(routeIds.at(-1), 'R1')
    for (let index = 1; index < routeIds.length; index++) {
      assert.equal(
        edgeKeys.has(`${routeIds[index - 1]}:${routeIds[index]}`),
        true
      )
    }
  })

  test('never selects the node where the character is already waiting', () => {
    const destination = pickRandomIdleDestination('L1', () => 0)

    assert.notEqual(destination.id, 'L1')
  })
})
