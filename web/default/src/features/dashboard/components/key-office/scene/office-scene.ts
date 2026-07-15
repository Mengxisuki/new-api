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
import { Application, Container, Graphics, Sprite } from 'pixi.js'

import type { KeyOfficeEmployee } from '../types'
import type { Agent } from '../types/agent'
import {
  getOfficeBackgroundTexture,
  loadOfficeAssets,
} from './assets/loadOfficeAssets'
import { loadSpineAssets } from './assets/loadSpineAssets'
import { AgentEntity } from './entities/AgentEntity'
import { DeskEntity } from './entities/DeskEntity'
import {
  COLORS,
  DESKS,
  SCENE_HEIGHT,
  SCENE_WIDTH,
} from './layout/office-layout'
import { OfficeSimulator } from './simulation/office-simulator'
import { AnimationSystem } from './systems/AnimationSystem'
import { MovementSystem } from './systems/MovementSystem'

const AGENT_COLORS = [
  0xe85d4a, 0x4a90d9, 0x9b6dd7, 0xf5c542, 0xf97316, 0x4ecdc4,
]

export class KeyOfficeScene {
  private app: Application | null = null
  private world: Container | null = null
  private officeLayer: Container | null = null
  private agents: Agent[] = []
  private agentEntities = new Map<string, AgentEntity>()
  private deskEntities = new Map<string, DeskEntity>()
  private movement = new MovementSystem()
  private animation = new AnimationSystem()
  private simulator = new OfficeSimulator()

  async init(container: HTMLElement, width: number, height: number) {
    const app = new Application()
    await app.init({
      width,
      height,
      backgroundColor: COLORS.floor,
      antialias: true,
      resolution: Math.min(window.devicePixelRatio || 1, 2),
      autoDensity: true,
      preference: 'webgl',
      powerPreference: 'high-performance',
    })
    this.app = app
    container.appendChild(app.canvas)
    this.world = new Container()
    app.stage.addChild(this.world)
    this.fitStage(width, height)

    await Promise.all([loadSpineAssets(), loadOfficeAssets()])
    this.drawMap()
    this.spawnDesks()
    this.syncEntities()
    app.ticker.add(this.onTick)
  }

  setEmployees(
    employees: KeyOfficeEmployee[],
    workingLabel: (count: number) => string
  ) {
    const previousById = new Map(
      this.agents.map((agent) => [agent.tokenId, agent])
    )
    this.agents = employees.map((employee, index) => {
      const previous = previousById.get(employee.id)
      const desk = DESKS[index]
      const desiredState = employee.state
      if (previous) {
        return {
          ...previous,
          name: employee.name,
          officeCharacter: employee.office_character,
          assignedDeskId: desk.id,
          desiredState,
          activeRequests: employee.active_requests,
          requests24h: employee.requests_24h,
          lastUsedAt: employee.last_used_at,
          currentTask:
            desiredState === 'working'
              ? workingLabel(employee.active_requests)
              : undefined,
        }
      }

      return {
        id: `token-${employee.id}`,
        tokenId: employee.id,
        name: employee.name,
        officeCharacter: employee.office_character,
        color: AGENT_COLORS[index % AGENT_COLORS.length],
        x: desk.seatX,
        y: desk.seatY,
        state: desiredState === 'working' ? 'working' : 'idle',
        desiredState,
        currentTask:
          desiredState === 'working'
            ? workingLabel(employee.active_requests)
            : undefined,
        assignedDeskId: desk.id,
        facing: 1,
        viewFacing: desiredState === 'working' ? 'back' : 'front',
        activeRequests: employee.active_requests,
        requests24h: employee.requests_24h,
        lastUsedAt: employee.last_used_at,
        idleMoveAt:
          desiredState === 'idle' ? Date.now() + index * 350 : undefined,
      }
    })
    this.syncEntities()
  }

  resize(width: number, height: number) {
    if (!this.app) return
    this.app.renderer.resize(width, height)
    this.fitStage(width, height)
  }

  destroy() {
    this.app?.ticker.remove(this.onTick)
    this.app?.destroy(true, { children: true })
    this.app = null
    this.world = null
    this.officeLayer = null
    this.agentEntities.clear()
    this.deskEntities.clear()
  }

  private drawMap() {
    if (!this.world) return
    const map = new Container()
    const floor = new Graphics()
    floor.rect(0, 0, SCENE_WIDTH, SCENE_HEIGHT)
    floor.fill(COLORS.floor)
    map.addChild(floor)

    const texture = getOfficeBackgroundTexture()
    if (texture) {
      const background = new Sprite(texture)
      const scale = Math.min(
        SCENE_WIDTH / texture.width,
        SCENE_HEIGHT / texture.height
      )
      background.scale.set(scale)
      background.position.set(
        (SCENE_WIDTH - texture.width * scale) / 2,
        (SCENE_HEIGHT - texture.height * scale) / 2
      )
      map.addChild(background)
    }
    this.world.addChild(map)
  }

  private spawnDesks() {
    if (!this.world) return
    const layer = new Container()
    layer.sortableChildren = true
    this.officeLayer = layer
    for (const desk of DESKS) {
      const entity = new DeskEntity(desk)
      this.deskEntities.set(desk.id, entity)
      layer.addChild(
        entity.shadowGfx,
        entity.deskLayer,
        entity.chairLayer,
        entity.occupiedIndicator
      )
    }
    this.world.addChild(layer)
  }

  private syncEntities() {
    if (!this.officeLayer) return
    const activeIds = new Set(this.agents.map((agent) => agent.id))
    for (const [id, entity] of this.agentEntities) {
      if (activeIds.has(id)) continue
      entity.destroy({ children: true })
      this.agentEntities.delete(id)
    }

    for (const agent of this.agents) {
      const existing = this.agentEntities.get(agent.id)
      const needsReplacement =
        existing && existing.data.officeCharacter !== agent.officeCharacter
      if (needsReplacement) {
        existing.destroy({ children: true })
        this.agentEntities.delete(agent.id)
      }
      let entity = this.agentEntities.get(agent.id)
      if (!entity) {
        entity = new AgentEntity(agent)
        this.agentEntities.set(agent.id, entity)
        this.officeLayer.addChild(entity)
      }
      entity.apply(agent)
      entity.setPosition(agent.x, agent.y)
    }
  }

  private onTick = (ticker: { deltaTime: number }) => {
    const dt = Math.min(ticker.deltaTime / 60, 0.05)
    this.agents = this.simulator.tick(this.agents)
    this.pushAgentsToEntities()
    this.movement.update(this.agentEntities, dt)
    this.pullAgentsFromEntities()
    this.animation.update(this.agentEntities, dt)
    this.sortDepth()
    this.syncDeskOccupancy()
  }

  private pushAgentsToEntities() {
    for (const agent of this.agents) {
      const entity = this.agentEntities.get(agent.id)
      if (!entity) continue
      const previous = entity.data
      entity.apply(agent)
      if (
        previous.x !== agent.x ||
        previous.y !== agent.y ||
        agent.state !== 'walking'
      ) {
        entity.setPosition(agent.x, agent.y)
      }
    }
  }

  private pullAgentsFromEntities() {
    this.agents = this.agents.map((agent) => {
      const entity = this.agentEntities.get(agent.id)
      return entity ? { ...agent, ...entity.data } : agent
    })
  }

  private sortDepth() {
    if (!this.officeLayer) return
    const positions = [...this.agentEntities.values()].map((entity) => ({
      x: entity.position.x,
      y: entity.position.y,
    }))
    for (const entity of this.agentEntities.values()) {
      entity.zIndex = entity.position.y
    }
    for (const desk of this.deskEntities.values()) {
      desk.updateDepthZ(positions)
    }
    this.officeLayer.sortChildren()
  }

  private syncDeskOccupancy() {
    const occupied = new Set(
      this.agents
        .filter((agent) => agent.state === 'working')
        .map((agent) => agent.assignedDeskId)
    )
    for (const desk of this.deskEntities.values()) {
      desk.setOccupied(occupied.has(desk.deskId))
    }
  }

  private fitStage(width: number, height: number) {
    if (!this.world) return
    const scale = Math.min(width / SCENE_WIDTH, height / SCENE_HEIGHT)
    this.world.scale.set(scale)
    this.world.position.set(
      (width - SCENE_WIDTH * scale) / 2,
      (height - SCENE_HEIGHT * scale) / 2
    )
    if (this.app?.canvas) {
      this.app.canvas.style.width = '100%'
      this.app.canvas.style.height = '100%'
      this.app.canvas.style.display = 'block'
    }
  }
}
