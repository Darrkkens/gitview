// Types of the Code Flow view: report.flow (export.FlowSection), the call
// index computed by the Go side (internal/flow: Graph, Node, Edge, Root)
// plus the anchors of the changed files, the analysed modules and compile
// errors — or only `error` when the analysis was unavailable.

import type { ReportSection } from '../../lib/types'

export type NodeKind = 'func' | 'closure' | 'route' | 'external' | 'db' | 'unresolved'
export type EdgeKind = 'call' | 'go' | 'defer' | 'handler'
export type Confidence = 'confirmed' | 'likely' | 'possible' | 'unresolved'

export interface FlowNode {
  id: string
  kind: NodeKind
  name: string
  pkg?: string
  sig?: string
  file?: string
  line?: number
  end?: number
  code?: string
  changed?: boolean
  recursive?: boolean
  truncated?: boolean
  tags?: string[]
  dbOp?: string
  dbLib?: string
  sql?: string
  sqlDynamic?: boolean
  method?: string
  path?: string
  router?: string
  partial?: boolean
  note?: string
  explain?: string
}

export interface FlowEdge {
  from: string
  to: string
  kind: EdgeKind
  conf: Confidence
  file?: string
  line?: number
  order?: number
  note?: string
}

export interface FlowRoot {
  id: string
  label: string
  kind: 'changed' | 'route' | 'selected' | string
}

export interface FlowSection {
  module?: string
  roots?: FlowRoot[] | null
  nodes?: FlowNode[] | null
  edges?: FlowEdge[] | null
  notes?: string[] | null
  truncated?: boolean
  maxDepth?: number
  anchors?: Record<string, string>
  modules?: string[] | null
  errors?: string[] | null
  error?: string
}

/** The inspector's symbols (report.inspect), only to know which exist. */
export interface InspectLite {
  symbols?: { symbol?: { id?: string } }[] | null
}

export type FlowReport = Omit<ReportSection, 'flow' | 'inspect'> & {
  flow?: FlowSection
  inspect?: InspectLite
}

export type Direction = 'callers' | 'both' | 'callees'

/** What the canvas shows: the neighbourhood of the root. */
export interface View {
  root: string
  dir: Direction
  depth: number
  external: boolean
  /** "id|1" (callees) or "id|-1" (callers) branches folded by the reader. */
  collapsed: Set<string>
}

export interface Box {
  x: number
  y: number
  w: number
  h: number
  col: number
}
