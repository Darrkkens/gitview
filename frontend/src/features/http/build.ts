// HTTP journey: for the selected route, the code it may run, left to right
// in stage lanes: Route → Middleware → Handler → Service → Repository →
// Database. A step sits in the lane of its own layer (Study Mode's
// classification) or, without one (a helper, an external call), in the lane
// of the step that calls it; a lane is drawn only when a step is in it.
// Every step is an edge of the dataset (static analysis): a possible path,
// not the trace of a request. A middleware's own calls start collapsed: the
// path to the handler and beyond is the journey's subject.

import { baseName } from '../../lib/format'
import type { Box, Edge, Lane, Scene } from './journey/Canvas'
import { boundsOf, font, layoutColumns, textWidth, type LEdge, type LNode } from './journey/layout'
import { CONF_RANK, FLOW, LAYER_COLOR, REL, kindLabel, layerColor, layerOf, reference, type Graph } from './graph'
import type { GEdge, GNode } from './types'

/** Columns keep at most this many steps. */
export const PER_COL = 60

export const JOURNEY_RELS = ['calls', 'go', 'defer', 'queries'] as const

/** The lane of each layer: Repository and External API share the data access stage. */
const STAGE_OF: Record<string, number> = {
  Route: 0,
  Middleware: 1,
  Handler: 2,
  Service: 3,
  Repository: 4,
  'External API': 4,
  Database: 5,
}
const STAGE_LAYERS = [['Route'], ['Middleware'], ['Handler'], ['Service'], ['Repository', 'External API'], ['Database']]

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
    stage = new Map<number, number>(),
    depth = new Map<number, number>(),
    more = new Map<number, number>(),
    chainPos = new Map<number, number>(),
    order: number[] = []
  const place = (i: number, c: number) => {
    col.set(i, c)
    order.push(i)
  }
  place(root, 0)
  stage.set(root, 0)
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
    stage.set(e.t, layerOf(N[e.t]) === 'Middleware' ? 1 : 2)
    depth.set(e.t, 1)
    chainPos.set(e.t, j + 1)
    q.push(e.t)
  })
  perCol.set(1, chain.length)
  while (q.length) {
    const i = q.shift()!,
      d = depth.get(i)!
    const middleware = chainPos.has(i) && stage.get(i) === 1
    const open = o.exp[i] === true || (o.exp[i] !== false && d <= o.depth && !middleware)
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
      stage.set(e.t, STAGE_OF[layerOf(N[e.t])] ?? stage.get(i)!)
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
  // the lanes are the layout's partitions; inside one, ELK layers by calls
  // the middlewares keep the chain's order, top to bottom
  const lnodes: LNode[] = order.map((i) => {
    const n: LNode = { id: 'h' + i, width: sizes.get(i)!.w, height: 44, c: stage.get(i)! }
    if (chainPos.has(i) && n.c === 1) n.rank = chainPos.get(i)
    return n
  })
  const ledges: LEdge[] = [...agg.values()].map((a) => ({ id: a.key, s: 'h' + a.a, t: 'h' + a.b }))
  const key =
    'http|' +
    r.id +
    '|' +
    lnodes.map((n) => n.id + ':' + n.c + ':' + n.width + (n.rank ? '#' + n.rank : '')).join(',') +
    '|' +
    ledges.map((e) => e.id).join(',')
  const res = await layoutColumns(key, lnodes, ledges, 'RIGHT', 1.6, 'NETWORK_SIMPLEX')
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
  const lanes = lanesOf(order.map((i) => [stage.get(i)!, byKey.get('h' + i)!, layerOf(N[i])]))
  return {
    route: k,
    root,
    scene: { boxes, edges, lanes, bounds: boundsOf([...boxes, ...lanes]) },
    edges,
    more,
    chainPos,
    layers,
    hidden,
    keyOf: (i: number) => (col.has(i) ? 'h' + i : null),
    ms: res.ms,
  }
}

/** Room above the steps for a lane's name, and around them. */
const LANE_HEAD = 40,
  LANE_PAD = 22

/**
 * lanesOf: the band of each stage holding a step, from the steps' boxes
 * ([stage, box, own layer]). Neighbouring bands meet halfway between their
 * steps; every band spans the whole drawing's height. A stage's name is its
 * layer (the layers present, for the data access stage).
 */
function lanesOf(steps: [number, Box, string][]): Lane[] {
  if (!steps.length) return []
  const by = new Map<number, { x0: number; x1: number; layers: Set<string> }>()
  let y0 = Infinity,
    y1 = -Infinity
  for (const [s, b, layer] of steps) {
    let l = by.get(s)
    if (!l) by.set(s, (l = { x0: Infinity, x1: -Infinity, layers: new Set() }))
    l.x0 = Math.min(l.x0, b.x)
    l.x1 = Math.max(l.x1, b.x + b.w)
    if (STAGE_OF[layer] === s) l.layers.add(layer)
    y0 = Math.min(y0, b.y)
    y1 = Math.max(y1, b.y + b.h)
  }
  const keys = [...by.keys()].sort((a, b) => a - b)
  return keys.map((s, j) => {
    const l = by.get(s)!
    const prev = j > 0 ? by.get(keys[j - 1])! : null,
      next = j < keys.length - 1 ? by.get(keys[j + 1])! : null
    const x0 = prev ? (prev.x1 + l.x0) / 2 : l.x0 - LANE_PAD,
      x1 = next ? (l.x1 + next.x0) / 2 : l.x1 + LANE_PAD
    const names = STAGE_LAYERS[s].filter((x) => l.layers.has(x))
    const name = names.length ? names.join(' · ') : STAGE_LAYERS[s][0]
    return {
      key: 'lane' + s,
      label: name === 'Handler' ? 'Handler (controller)' : name,
      color: LAYER_COLOR[names[0] ?? STAGE_LAYERS[s][0]] ?? 'k-group',
      x: x0,
      y: y0 - LANE_HEAD,
      w: x1 - x0,
      h: y1 - y0 + LANE_HEAD + LANE_PAD,
    }
  })
}
