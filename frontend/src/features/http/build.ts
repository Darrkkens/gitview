// HTTP journey: for the selected route, the code it may run, left to right:
// the route, its handler chain in registration order, then what the chain
// calls, by call depth. Every step is an edge of the dataset (static
// analysis): a possible path, not the trace of a request.

import { baseName } from '../../lib/format'
import type { Box, Edge, Scene } from './journey/Canvas'
import { boundsOf, font, layoutColumns, textWidth, type LEdge, type LNode } from './journey/layout'
import { CONF_RANK, FLOW, LAYER_COLOR, REL, kindLabel, layerColor, layerOf, reference, type Graph } from './graph'
import type { GEdge, GNode } from './types'

/** Columns keep at most this many steps. */
export const PER_COL = 60

export const JOURNEY_RELS = ['calls', 'go', 'defer', 'queries'] as const

export interface JourneyOpts {
  depth: number
  /** per node index: true opens its callees, false collapses them */
  exp: Record<number, boolean>
  ext: boolean
  /** relation types and confidences hidden (false) */
  rels: Record<string, boolean>
  confs: Record<string, boolean>
}

/** Agg is a drawn line: every relation between two drawn steps. */
export interface Agg {
  key: string
  a: number
  b: number
  n: number
  w: number
  types: Record<string, number>
  confs: Record<string, number>
  best: string
  real: number[]
}

export interface JourneyEdge extends Edge {
  agg: Agg
}

export interface JourneyView {
  route: number
  root: number
  scene: Scene
  edges: JourneyEdge[]
  more: Map<number, number>
  chainPos: Map<number, number>
  layers: Record<string, number>
  hidden: number
  keyOf: (i: number) => string | null
  ms: number
}

/** summary of a drawn line: "2 calls · 1 query". */
export function summary(g: Agg): string {
  return Object.keys(g.types)
    .sort((x, y) => g.types[y] - g.types[x])
    .slice(0, 3)
    .map((k) => {
      const s = REL[k]?.short ?? k
      return g.types[k] + ' ' + s + (g.types[k] > 1 && !/s$/.test(s) && k !== 'implements' && k !== 'embeds' ? 's' : '')
    })
    .join(' · ')
}

function subtitle(n: GNode): string {
  if (n.type === 'route') return 'route · ' + (n.meta.router || '') + (n.line ? ' · line ' + n.line : '')
  if (n.type === 'db') return (n.meta.lib || 'db') + ' ' + (n.meta.op || '') + (n.line ? ' · line ' + n.line : '')
  if (n.type === 'external') return 'external · ' + (n.pkg || '')
  if (n.type === 'unresolved') return 'unresolved' + (n.line ? ' · line ' + n.line : '')
  return kindLabel(n) + (n.line ? ' · line ' + n.line : '')
}

/** leafWidth: the width of a symbol drawn collapsed (Architecture's size). */
function leafWidth(n: GNode): number {
  const tw = textWidth(n.name, font(500, 12.5)) + 34
  const sw = textWidth(subtitle(n), font(400, 11)) + 34
  return Math.round(Math.max(140, Math.min(Math.max(tw, sw), 280)))
}

const relOk = (e: GEdge, o: JourneyOpts) => o.rels[e.type] !== false && o.confs[e.conf] !== false

export async function buildJourney(g: Graph, k: number, o: JourneyOpts): Promise<JourneyView | null> {
  const r = g.routes[k],
    root = g.nodeOfRoute[k]
  if (!r || root === undefined || root < 0) return null
  const { N, E } = g
  const col = new Map<number, number>(),
    depth = new Map<number, number>(),
    more = new Map<number, number>(),
    chainPos = new Map<number, number>(),
    order: number[] = []
  const place = (i: number, c: number) => {
    col.set(i, c)
    order.push(i)
  }
  place(root, 0)
  depth.set(root, 0)
  const chain = N[root].out
    .map((ei) => E[ei])
    .filter((e) => e.type === 'handles')
    .sort((a, b) => (Number(a.meta.order) || 0) - (Number(b.meta.order) || 0))
  const q: number[] = []
  const perCol = new Map<number, number>()
  chain.forEach((e, j) => {
    if (col.has(e.t)) return
    place(e.t, 1)
    depth.set(e.t, 1)
    chainPos.set(e.t, j + 1)
    q.push(e.t)
  })
  perCol.set(1, chain.length)
  while (q.length) {
    const i = q.shift()!,
      d = depth.get(i)!
    const open = o.exp[i] === true || (o.exp[i] !== false && d <= o.depth)
    const callees = N[i].out
      .map((ei) => E[ei])
      .filter((e) => FLOW[e.type] && e.type !== 'handles' && relOk(e, o) && !(N[e.t].ext && !o.ext))
    callees.sort((a, b) => CONF_RANK[b.conf] - CONF_RANK[a.conf] || (N[a.t].name < N[b.t].name ? -1 : 1))
    let hidden = 0
    for (const e of callees) {
      if (col.has(e.t)) continue
      const c = col.get(i)! + 1
      if (!open || (perCol.get(c) || 0) >= PER_COL) {
        hidden++
        continue
      }
      place(e.t, c)
      depth.set(e.t, d + 1)
      q.push(e.t)
      perCol.set(c, (perCol.get(c) || 0) + 1)
    }
    if (hidden) more.set(i, hidden)
  }
  // relations between drawn steps
  const agg = new Map<string, Agg>()
  for (const i of order) {
    for (const ei of N[i].out) {
      const e = E[ei]
      if (!col.has(e.t) || e.t === i || !FLOW[e.type] || !relOk(e, o)) continue
      const key = 'h' + i + '>h' + e.t
      let a = agg.get(key)
      if (!a) {
        a = { key, a: i, b: e.t, n: 0, w: 0, types: {}, confs: {}, best: 'unresolved', real: [] }
        agg.set(key, a)
      }
      a.n++
      a.w += e.w
      a.types[e.type] = (a.types[e.type] || 0) + 1
      a.confs[e.conf] = (a.confs[e.conf] || 0) + 1
      if (CONF_RANK[e.conf] > CONF_RANK[a.best]) a.best = e.conf
      a.real.push(e.i)
    }
  }
  const subOf = (i: number) => {
    const n = N[i]
    const lyr = layerOf(n)
    const where = n.file ? baseName(n.file) + (n.line ? ':' + n.line : '') : n.ext ? n.pkg : ''
    const sub =
      n.type === 'route'
        ? r.framework + ' · ' + where
        : (chainPos.has(i) ? (lyr === 'Middleware' ? 'middleware' : 'handler') + ' · ' : '') + where
    return { lyr, sub }
  }
  const sizeOf = (i: number) => {
    const n = N[i]
    const title = n.type === 'route' ? r.key : n.name
    const { lyr, sub } = subOf(i)
    const lead = 30 + (chainPos.has(i) ? 20 : 0)
    const tw = textWidth(title, font(500, 12.5)) + lead + (more.has(i) ? 34 : 0)
    // the subtitle and the layer chip share the second line
    const sw =
      textWidth(sub, font(400, 11)) + lead + (lyr && n.type !== 'route' ? textWidth(lyr, font(400, 11)) + 22 : 0)
    return { w: Math.round(Math.max(leafWidth(n), Math.min(Math.max(tw, sw), 320))), h: 44 }
  }
  const sizes = new Map(order.map((i) => [i, sizeOf(i)]))
  const lnodes: LNode[] = order.map((i) => ({ id: 'h' + i, width: sizes.get(i)!.w, height: 44, c: col.get(i)! }))
  const ledges: LEdge[] = [...agg.values()].map((a) => ({ id: a.key, s: 'h' + a.a, t: 'h' + a.b }))
  const key =
    'http|' +
    r.id +
    '|' +
    lnodes.map((n) => n.id + ':' + n.c + ':' + n.width).join(',') +
    '|' +
    ledges.map((e) => e.id).join(',')
  const res = await layoutColumns(key, lnodes, ledges, 'RIGHT', 1.6)
  const layers: Record<string, number> = {}
  const boxes: Box[] = order.map((i) => {
    const n = N[i],
      p = res.pos['h' + i] ?? { x: 0, y: 0 },
      s = sizes.get(i)!
    const { lyr, sub } = subOf(i)
    if (lyr) layers[lyr] = (layers[lyr] || 0) + 1
    return {
      key: 'h' + i,
      ref: i,
      x: p.x,
      y: p.y,
      w: s.w,
      h: s.h,
      title: n.type === 'route' ? r.key : n.name,
      sub,
      color: layerColor(n),
      chip: lyr && n.type !== 'route' ? lyr : '',
      chipColor: LAYER_COLOR[lyr] ?? 'fg-2',
      order: chainPos.get(i) || 0,
      count: more.has(i) ? '+' + more.get(i) : '',
      chev: more.has(i) ? 'closed' : null,
      tip: reference(n),
      layer: lyr,
    }
  })
  const byKey = new Map(boxes.map((b) => [b.key, b]))
  const edges: JourneyEdge[] = [...agg.values()].map((a) => {
    let pts = res.edges[a.key]
    if (!pts) {
      const A = byKey.get('h' + a.a)!,
        B = byKey.get('h' + a.b)!
      const ax = A.x + A.w,
        ay = A.y + A.h / 2,
        bx = B.x,
        by = B.y + B.h / 2,
        mx = (ax + bx) / 2
      pts = [ax, ay, mx, ay, mx, by, bx, by]
    }
    return {
      key: a.key,
      s: 'h' + a.a,
      t: 'h' + a.b,
      conf: a.best,
      n: a.n,
      kinds: Object.keys(a.types).length,
      label: summary(a),
      pts,
      agg: a,
    }
  })
  let hidden = 0
  more.forEach((n) => (hidden += n))
  return {
    route: k,
    root,
    scene: { boxes, edges, bounds: boundsOf(boxes) },
    edges,
    more,
    chainPos,
    layers,
    hidden,
    keyOf: (i: number) => (col.has(i) ? 'h' + i : null),
    ms: res.ms,
  }
}
