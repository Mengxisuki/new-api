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
import type {
  AgentState,
  Desk,
} from '@/features/dashboard/components/key-office/types/agent'

export const SCENE_WIDTH = 960
export const SCENE_HEIGHT = 640

export const COLORS = {
  floor: 0xffffff,
  wall: 0xe8e6e1,
  desk: 0xffffff,
  deskShadow: 0x00000014,
  monitor: 0x2a2a2a,
  chair: 0xd4d2cc,
  agentBody: 0x1a1a1a,
} as const

/** 2×3 工位区 */
const DESK_COLS = 2
const DESK_ROWS = 3
const DESK_COL_GAP = 150
const DESK_ROW_GAP = 140
const DESK_BLOCK_WIDTH = (DESK_COLS - 1) * DESK_COL_GAP
const DESK_BLOCK_HEIGHT = (DESK_ROWS - 1) * DESK_ROW_GAP
/** 工位阵列在场景内水平/垂直居中 */
const DESK_ORIGIN_X = (SCENE_WIDTH - DESK_BLOCK_WIDTH) / 2
const DESK_ORIGIN_Y = (SCENE_HEIGHT - DESK_BLOCK_HEIGHT) / 2
export const SEAT_OFFSET_Y = 45

function buildDesks(): Desk[] {
  const desks: Desk[] = []
  let n = 0
  for (let row = 0; row < DESK_ROWS; row++) {
    for (let col = 0; col < DESK_COLS; col++) {
      const x = DESK_ORIGIN_X + col * DESK_COL_GAP
      const y = DESK_ORIGIN_Y + row * DESK_ROW_GAP
      desks.push({
        id: `desk-${n}`,
        x,
        y,
        seatX: x,
        seatY: y + SEAT_OFFSET_Y,
      })
      n++
    }
  }
  return desks
}

export const DESKS: Desk[] = buildDesks()

/** 空闲角色只在办公室下半区活动，避免走到顶部的布景区域。 */
export type IdleRouteNodeState = Exclude<AgentState, 'walking' | 'working'>

export type IdleRouteNode = {
  id: string
  x: number
  y: number
  /** 到达节点后播放的角色状态。 */
  state: IdleRouteNodeState
  /** 在节点停留的随机时长范围，单位为毫秒。 */
  stayMs: readonly [min: number, max: number]
  /** 到达节点后角色朝向。 */
  facing: 'front' | 'back' | 'left' | 'right'
}

/**
 * 闲逛节点配置：修改 x/y 可调整活动区域，state 可切换节点动画。
 * 节点编号会显示在办公室中，便于直接对照这里调整。
 */
export const IDLE_ROUTE_NODES = [
  {
    id: 'L1',
    x: 300,
    y: 380,
    state: 'thinking',
    stayMs: [2600, 4800],
    facing: 'right',
  },
  {
    id: 'L2',
    x: 300,
    y: 435,
    state: 'idle',
    stayMs: [2200, 4200],
    facing: 'front',
  },
  {
    id: 'L3',
    x: 300,
    y: 500,
    state: 'talking',
    stayMs: [2000, 3600],
    facing: 'front',
  },
  {
    id: 'L4',
    x: 300,
    y: 560,
    state: 'idle',
    stayMs: [2400, 4400],
    facing: 'right',
  },
  {
    id: 'B1',
    x: 390,
    y: 585,
    state: 'thinking',
    stayMs: [2600, 4600],
    facing: 'front',
  },
  {
    id: 'B2',
    x: 480,
    y: 585,
    state: 'idle',
    stayMs: [2000, 4000],
    facing: 'front',
  },
  {
    id: 'B3',
    x: 570,
    y: 585,
    state: 'talking',
    stayMs: [2000, 3600],
    facing: 'front',
  },
  {
    id: 'R4',
    x: 660,
    y: 560,
    state: 'idle',
    stayMs: [2400, 4400],
    facing: 'left',
  },
  {
    id: 'R3',
    x: 660,
    y: 500,
    state: 'thinking',
    stayMs: [2600, 4800],
    facing: 'front',
  },
  {
    id: 'R2',
    x: 660,
    y: 435,
    state: 'idle',
    stayMs: [2200, 4200],
    facing: 'front',
  },
  {
    id: 'R1',
    x: 660,
    y: 380,
    state: 'talking',
    stayMs: [2000, 3600],
    facing: 'left',
  },
  {
    id: 'C1',
    x: 480,
    y: 435,
    state: 'thinking',
    stayMs: [2600, 4600],
    facing: 'front',
  },
] as const satisfies readonly IdleRouteNode[]

/**
 * 闲逛线路配置：每一项连接两个节点，线路双向可走。
 * 图中存在多个环路，因此相同起终点也可以随机走不同线路。
 */
export const IDLE_ROUTE_EDGES = [
  ['L1', 'L2'],
  ['L2', 'L3'],
  ['L3', 'L4'],
  ['L4', 'B1'],
  ['B1', 'B2'],
  ['B2', 'B3'],
  ['B3', 'R4'],
  ['R4', 'R3'],
  ['R3', 'R2'],
  ['R2', 'R1'],
  ['L2', 'C1'],
  ['C1', 'R2'],
  ['C1', 'B2'],
] as const satisfies readonly (readonly [string, string])[]

/** 调整完成后可改为 false，隐藏节点编号和线路。 */
export const SHOW_IDLE_ROUTE_GRAPH = true
