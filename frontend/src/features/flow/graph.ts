// The Code Flow navigator's model, ported from the legacy report (flowJS in
// internal/export/html_flow.go): an index of the call graph, the symbol
// search, the neighbourhood of a root within a depth (build), a layered
// layout (callers on the left, callees on the right) and the path from the
// root to a node. The browser only navigates the index the Go side built.

import type { Box, Confidence, FlowEdge, FlowNode, FlowSection, NodeKind, View } from './types'

/** Nodes per view: larger neighbourhoods stop growing and say so. */
export const LIMIT = 320

export const KIND_LABEL: Record<NodeKind, string> = {
  func: 'Function',
  closure: 'Anonymous function',
  route: 'HTTP route',
  external: 'External',
  db: 'Database operation',
  unresolved: 'Unresolved call',
}

export const CONF_LABEL: Record<Confidence, string> = {
  confirmed: 'confirmed',
  likely: 'likely',
  possible: 'possible',
  unresolved: 'unresolved',
}

export interface FlowIndex {
  nodes: Map<string, FlowNode>
  out: Map<string, FlowEdge[]>
  in: Map<string, FlowEdge[]>
  searchable: FlowNode[]
  funcs: number
  edges: number
}

const isSearchable = (n: FlowNode) => n.kind === 'func' || n.kind === 'closure' || n.kind === 'route'

export function indexGraph(d: FlowSection): FlowIndex {
  const nodes = new Map<string, FlowNode>()
  const out = new Map<string, FlowEdge[]>()
  const inn = new Map<string, FlowEdge[]>()
  for (const n of d.nodes ?? []) {
    nodes.set(n.id, n)
    out.set(n.id, [])
    inn.set(n.id, [])
  }
  let edges = 0
  for (const e of d.edges ?? []) {
    if (!nodes.has(e.from) || !nodes.has(e.to)) continue
    out.get(e.from)!.push(e)
    inn.get(e.to)!.push(e)
    edges++
  }
  for (const list of out.values()) if (list.length > 1) list.sort((a, b) => (a.order || 0) - (b.order || 0))
  const searchable = (d.nodes ?? []).filter(isSearchable)
  const funcs = (d.nodes ?? []).filter((n) => n.kind === 'func' || n.kind === 'closure').length
  return { nodes, out, in: inn, searchable, funcs, edges }
}

// ---------- symbol search ----------

/**
 * score ranks a node for a query (lower is better, -1 = no match). Matching
 * is case-insensitive, but only an exact, case-sensitive match of a Go
 * identifier (or qualified name) counts as "the same name" (0).
 */
export function score(n: FlowNode, q: string, raw: string): number {
  const name = n.name.toLowerCase()
  const bare = name.slice(name.lastIndexOf('.') + 1)
  const hay = (n.name + ' ' + (n.pkg || '') + ' ' + (n.file || '')).toLowerCase()
  for (const tok of q.split(/\s+/)) if (tok && !hay.includes(tok)) return -1
  const rawBare = n.name.slice(n.name.lastIndexOf('.') + 1)
  if (raw && (rawBare === raw || n.name === raw)) return 0
  if (bare === q || name === q) return 1
  if (bare.startsWith(q)) return 2
  if (name.includes(q)) return 3
  return 4
}

export interface SearchResult {
  hits: FlowNode[]
  total: number
  exact: number
}

export function search(ix: FlowIndex, raw: string, max = 14): SearchResult {
  raw = raw.trim()
  const q = raw.toLowerCase()
  if (!q) return { hits: [], total: 0, exact: 0 }
  const res: [number, FlowNode][] = []
  for (const n of ix.searchable) {
    const s = score(n, q, raw)
    if (s >= 0) res.push([s, n])
  }
  res.sort((a, b) => a[0] - b[0] || (a[1].name < b[1].name ? -1 : a[1].name > b[1].name ? 1 : 0))
  return { hits: res.slice(0, max).map((r) => r[1]), total: res.length, exact: res.filter((r) => r[0] === 0).length }
}

export function exactMatches(ix: FlowIndex, raw: string): FlowNode[] {
  raw = raw.trim()
  const q = raw.toLowerCase()
  return ix.searchable.filter((n) => score(n, q, raw) === 0)
}

// ---------- the neighbourhood of the root ----------

export interface Built {
  /** Column of each node: 0 root, +d callees, -d callers. */
  vis: Map<string, number>
  order: string[]
  edges: FlowEdge[]
  /** Edge that brought a node into the view, per side ("id|1", "id|-1"). */
  parent: Map<string, FlowEdge>
  /** Calls beyond the depth limit, per side. */
  more: Map<string, number>
  trunc: boolean
}

export function build(ix: FlowIndex, v: View): Built {
  const vis = new Map<string, number>([[v.root, 0]])
  const order = [v.root]
  const edges: FlowEdge[] = []
  const eset = new Set<string>()
  const parent = new Map<string, FlowEdge>()
  const more = new Map<string, number>()
  let trunc = false
  const keep = (id: string) => v.external || ix.nodes.get(id)?.kind !== 'external'
  const walk = (sign: 1 | -1, adj: Map<string, FlowEdge[]>, next: (e: FlowEdge) => string) => {
    let frontier = [v.root]
    for (let d = 1; d <= v.depth && frontier.length; d++) {
      const nf: string[] = []
      for (const id of frontier) {
        if (v.collapsed.has(id + '|' + sign)) continue
        for (const e of adj.get(id) ?? []) {
          const t = next(e)
          if (!keep(t)) continue
          if (!vis.has(t)) {
            if (order.length >= LIMIT) {
              trunc = true
              continue
            }
            vis.set(t, sign * d)
            order.push(t)
            nf.push(t)
            parent.set(t + '|' + sign, e)
          }
          const key = e.from + '>' + e.to + '>' + (e.line || 0) + '>' + (e.order || 0)
          if (!eset.has(key)) {
            eset.add(key)
            edges.push(e)
          }
        }
      }
      frontier = nf
    }
    for (const id of frontier) {
      const c = (adj.get(id) ?? []).filter((e) => keep(next(e))).length
      if (c) more.set(id + '|' + sign, c)
    }
  }
  if (v.dir !== 'callers') walk(1, ix.out, (e) => e.to)
  if (v.dir !== 'callees') walk(-1, ix.in, (e) => e.from)
  return { vis, order, edges, parent, more, trunc }
}

/** pathTo is the chain of edges from the root to a node of the view. */
export function pathTo(g: Built, root: string, id: string): Set<FlowEdge> {
  const es = new Set<FlowEdge>()
  const col = g.vis.get(id)
  if (col === undefined) return es
  const sign = col > 0 ? 1 : -1
  let cur = id
  for (let guard = 0; cur !== root && guard < 64; guard++) {
    const e = g.parent.get(cur + '|' + sign)
    if (!e) break
    es.add(e)
    cur = sign > 0 ? e.from : e.to
  }
  return es
}

// ---------- layout ----------

/** sub is a node's second line: where it is, or what it is. */
export function sub(n: FlowNode): string {
  switch (n.kind) {
    case 'route':
      return (n.router || '') + (n.partial ? ' · partial' : '') + (n.file ? ' · ' + n.file + ':' + n.line : '')
    case 'db':
      return (n.sql ? 'SQL literal' : n.sqlDynamic ? 'dynamic SQL' : '') + (n.file ? ' · ' + n.file + ':' + n.line : '')
    case 'external':
      return n.pkg || 'external'
    case 'unresolved':
      return n.file ? n.file + ':' + n.line : 'unresolved'
  }
  return (n.file || '') + (n.line ? ':' + n.line : '')
}

let measureCtx: CanvasRenderingContext2D | null = null
const widths = new Map<string, number>()

function monoFamily(): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue('--font-mono').trim()
  return v || 'ui-monospace, monospace'
}

let family = ''
export const FONT1 = () => '600 12px ' + (family ||= monoFamily())
export const FONT2 = () => '10.5px ' + (family ||= monoFamily())

export function textW(t: string, font: string): number {
  const key = font + '\u0000' + t
  let w = widths.get(key)
  if (w === undefined) {
    measureCtx ??= document.createElement('canvas').getContext('2d')
    if (measureCtx) {
      measureCtx.font = font
      w = measureCtx.measureText(t).width
    } else {
      w = t.length * 7.2
    }
    if (widths.size > 20000) widths.clear()
    widths.set(key, w)
  }
  return w
}

/** fitText keeps the end of a label that is too wide ("…pkg.Func"). */
export function fitText(t: string, max: number, font: string): string {
  if (textW(t, font) <= max) return t
  let lo = 0
  let hi = t.length
  while (lo < hi) {
    const m = (lo + hi + 1) >> 1
    if (textW('…' + t.slice(t.length - m), font) <= max) lo = m
    else hi = m - 1
  }
  return '…' + t.slice(t.length - lo)
}

export const NODE_H = 40
const V_GAP = 14
const H_GAP = 90

export interface Layout {
  box: Map<string, Box>
  cols: Map<number, string[]>
  keys: number[]
  bounds: { minX: number; minY: number; maxX: number; maxY: number }
}

export function layout(ix: FlowIndex, g: Built, root: string): Layout {
  const cols = new Map<number, string[]>()
  for (const id of g.order) {
    const l = g.vis.get(id)!
    if (!cols.has(l)) cols.set(l, [])
    cols.get(l)!.push(id)
  }
  const keys = [...cols.keys()].sort((a, b) => a - b)
  const pos = new Map<string, number>()
  for (const l of keys) cols.get(l)!.forEach((id, i) => pos.set(id, i))
  const nb = new Map<string, string[]>()
  for (const e of g.edges) {
    if (!nb.has(e.from)) nb.set(e.from, [])
    if (!nb.has(e.to)) nb.set(e.to, [])
    nb.get(e.from)!.push(e.to)
    nb.get(e.to)!.push(e.from)
  }
  // Barycentre ordering, a few sweeps outwards from the root.
  for (let it = 0; it < 4; it++) {
    for (const l of keys) {
      if (l === 0) continue
      const toward = l > 0 ? l - 1 : l + 1
      const bary = (id: string) => {
        const ns = (nb.get(id) ?? []).filter((x) => g.vis.get(x) === toward)
        if (!ns.length) return pos.get(id)!
        return ns.reduce((s, x) => s + pos.get(x)!, 0) / ns.length
      }
      const col = cols.get(l)!
      const b = new Map(col.map((id) => [id, bary(id)]))
      col.sort((a, z) => b.get(a)! - b.get(z)!)
      col.forEach((id, i) => pos.set(id, i))
    }
  }
  const box = new Map<string, Box>()
  const f1 = FONT1()
  const f2 = FONT2()
  let x = 0
  for (const l of keys) {
    let w = 0
    for (const id of cols.get(l)!) {
      const n = ix.nodes.get(id)!
      w = Math.max(w, Math.min(360, Math.max(textW(n.name + (n.recursive ? ' ↻' : ''), f1), textW(sub(n), f2)) + 34))
    }
    const list = cols.get(l)!
    const total = list.length * (NODE_H + V_GAP) - V_GAP
    let y = -total / 2
    for (const id of list) {
      box.set(id, { x, y, w, h: NODE_H, col: l })
      y += NODE_H + V_GAP
    }
    x += w + H_GAP
  }
  const r = box.get(root)
  const shift = r ? r.x + r.w / 2 : 0
  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity
  for (const b of box.values()) {
    b.x -= shift
    minX = Math.min(minX, b.x)
    minY = Math.min(minY, b.y)
    maxX = Math.max(maxX, b.x + b.w)
    maxY = Math.max(maxY, b.y + b.h)
  }
  return { box, cols, keys, bounds: { minX, minY, maxX, maxY } }
}

/** edgePath is the curve of an edge between two boxes. */
export function edgePath(a: Box, b: Box): { d: string; mx: number; my: number } {
  const x1 = a.x + a.w
  const y1 = a.y + a.h / 2
  const x2 = b.x
  const y2 = b.y + b.h / 2
  if (x2 > x1 + 10) {
    const dx = Math.max(30, (x2 - x1) / 2)
    return { d: `M${x1},${y1} C${x1 + dx},${y1} ${x2 - dx},${y2} ${x2},${y2}`, mx: (x1 + x2) / 2, my: (y1 + y2) / 2 }
  }
  // Back edges (recursion, calls to an earlier column) loop above.
  const lift = Math.max(a.h, Math.abs(y2 - y1) / 2) + 30
  return {
    d: `M${x1},${y1} C${x1 + 60},${y1 - lift} ${x2 - 60},${y2 - lift} ${x2},${y2}`,
    mx: (x1 + x2) / 2,
    my: (y1 + y2) / 2 - lift * 0.75,
  }
}

/** edgeTitle describes an edge for its tooltip. */
export function edgeTitle(ix: FlowIndex, e: FlowEdge): string {
  const a = ix.nodes.get(e.from)?.name ?? e.from
  const b = ix.nodes.get(e.to)?.name ?? e.to
  return (
    a +
    ' → ' +
    b +
    ' (' +
    CONF_LABEL[e.conf] +
    (e.kind !== 'call' ? ', ' + e.kind : '') +
    ')' +
    (e.file ? ' at ' + e.file + ':' + e.line : '') +
    (e.note ? ' — ' + e.note : '')
  )
}
