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

export type AgentState = 'idle' | 'walking' | 'working' | 'talking' | 'thinking'

export interface DeskVisitStop {
  hostRosterNo: number
  hostAgentId: string
  hostDeskId: string
  message: string
}

/** 工位拜访（可串联多站，最后一站后再回座） */
export interface DeskVisitMission {
  kind: 'desk_visit'
  phase: 'goto' | 'talk' | 'return'
  hostAgentId: string
  hostDeskId: string
  message: string
  resumeTask: string
  talkDuration: number
  talkRemaining?: number
  /** 当前站之后还要去的目标 */
  queue: DeskVisitStop[]
}

export interface Agent {
  id: string
  tokenId: number
  name: string
  officeCharacter: string
  color: number
  x: number
  y: number
  targetX?: number
  targetY?: number
  /** 过道路径点队列（逐段行走） */
  walkPath?: { x: number; y: number }[]
  walkPathIndex?: number
  state: AgentState
  currentTask?: string
  assignedDeskId?: string
  bubbleText?: string
  customAnimation?: string
  /** 简笔 fallback 左右翻转 */
  facing: 1 | -1
  /** Spine 四向：走路时按位移更新 */
  viewFacing?: ChibiFacing
  mission?: DeskVisitMission
  activeRequests: number
  requests24h: number
  lastUsedAt: number
  desiredState: 'working' | 'idle'
  idleMoveAt?: number
  /** 最近到达的闲逛节点。 */
  idleNodeId?: string
  /** 本次闲逛的目标节点及到达后的状态。 */
  idleTargetNodeId?: string
  idleTargetState?: Exclude<AgentState, 'walking' | 'working'>
  idleTargetStayMs?: number
  idleTargetFacing?: ChibiFacing
}

export interface Desk {
  id: string
  x: number
  y: number
  seatX: number
  seatY: number
  occupiedBy?: string
}
