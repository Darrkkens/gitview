// Focus mode: one element and what it is connected to, as a left-to-right
// flow (what depends on it on the left, what it depends on on the right),
// with the depth set by the viewer.
//   symbol            callers and callees (types: implementations, embeds,
//                     type uses and methods), from the dataset's edges
//   package · folder  packages (directories collapse to their packages)
//   file              files
// Each column holds one distance; a column keeps its strongest relations
// first and says how many more there are.

import { addEdge, isMinor, lineAggOf, shown, summary, type LayoutInput } from './arch'
import { cardOf } from './cards'
import { SceneEditor } from './edit'
import { placeAvoiding, removeOverlaps } from './geometry'
import { algoOptions, runLayout, runPartitioned, type LayoutNode, type LayoutResult } from './layout'
import type { Algorithm } from './layoutDoc'
import type { Model } from './model'
import { smoothRoute } from './route'
import type { Agg, Box, Conf, Filters, FocusState, FocusView, GEdge, Scene, SceneEdge, TItem } from './types'
import { CONF_RANK, FLOW, STRUCTURAL, SYMBOL, TYPE } from './vocab'

export const PER_COL = 36
const TYPE_RELS: Record<string, 1> = { implements: 1, embeds: 1, references: 1, method: 1 }

export interface FocusInput {
  focus: FocusState
  filters: Filters
}

function relevant(e: GEdge, center: TItem, S: FocusInput): boolean {
  const f = S.filters
  if (f.rels[e.type] === false && !(e.type === 'method' && TYPE[center.kind])) return false
  if (f.confs[e.conf] === false) return false
  if (center.n && SYMBOL[center.kind]) {
    if (TYPE[center.kind]) return !!TYPE_RELS[e.type]
    return !!FLOW[e.type] || (e.type === 'references' && S.focus.refs)
  }
  return !STRUCTURAL[e.type]
}

/** unitFn: how a node is represented in the focus graph. */
function unitFn(D: Model, center: TItem): (x: number) => number {
  const T = D.T
  const c = center.i
  if (center.n && SYMBOL[center.kind]) return (x) => x
  const level = center.kind === 'file' ? 'file' : 'package'
  return (x) => {
    if (x === c || D.isAncestor(c, x)) return c
    for (let p = x; p >= 0; p = T[p].parent)
      if (T[p].kind === level || (level === 'file' && T[p].kind === 'package')) return p
    return -1
  }
}

export const isSymbolFocus = (t: TItem) => !!(t.n && SYMBOL[t.kind])

/**
 * buildFocus lays out focus mode. Algorithms: the columns flow left to right
 * (Grouped, Hierarchical, Left to right), top to bottom (Top to bottom), or
 * force directed; positions the viewer gave are absolute.
 */
export async function buildFocus(D: Model, S: FocusInput, aspect: number, L: LayoutInput): Promise<FocusView> {
  const { T, N, E } = D
  const center = T[S.focus.i]
  const unit = unitFn(D, center)
  const f = S.filters
  const symbolMode = isSymbolFocus(center)
  const memberCache = new Map<number, number[]>()
  function memberNodes(u: number): number[] {
    const hit = memberCache.get(u)
    if (hit) return hit
    const out: number[] = []
    if (T[u].n) out.push(u)
    if (!symbolMode)
      D.descendants(u, (d) => {
        if (T[d].n) out.push(d)
      })
    memberCache.set(u, out)
    return out
  }
  // neighbours of a unit (lazily, only around the center)
  function neighbours(u: number, dir: 1 | -1): Map<number, Conf> {
    const list = new Map<number, Conf>()
    for (const x of memberNodes(u)) {
      for (const ei of dir > 0 ? N[x].out : N[x].inn) {
        const e = E[ei]
        if (!relevant(e, center, S)) continue
        const o = unit(dir > 0 ? e.t : e.s)
        if (o < 0 || o === u) continue
        if (o !== center.i && !shown(T[o], f)) continue
        const cur = list.get(o)
        if (!cur || CONF_RANK[e.conf] > CONF_RANK[cur]) list.set(o, e.conf)
      }
    }
    return list
  }
  const col = new Map<number, number>([[center.i, 0]])
  const cols: Record<number, number[]> = { 0: [center.i] },
    more: Record<number, number> = {}
  function walk(dir: 1 | -1, depth: number) {
    let frontier = [center.i]
    for (let d = 1; d <= depth && frontier.length; d++) {
      const next = new Map<number, Conf>()
      for (const u of frontier)
        neighbours(u, dir).forEach((conf, o) => {
          if (col.has(o)) return
          const c = next.get(o)
          if (!c || CONF_RANK[conf] > CONF_RANK[c]) next.set(o, conf)
        })
      let list = Array.from(next.keys()).sort(
        (a, b) => CONF_RANK[next.get(b)!] - CONF_RANK[next.get(a)!] || (T[a].label < T[b].label ? -1 : 1),
      )
      if (list.length > PER_COL) {
        more[dir * d] = list.length - PER_COL
        list = list.slice(0, PER_COL)
      }
      list.forEach((o) => col.set(o, dir * d))
      cols[dir * d] = list
      frontier = list
    }
  }
  walk(-1, S.focus.up)
  walk(1, S.focus.down)

  // every relevant relation between drawn units, aggregated per pair
  const agg = new Map<string, Agg>()
  col.forEach((_c, u) => {
    for (const x of memberNodes(u))
      for (const ei of N[x].out) {
        const e = E[ei]
        if (!relevant(e, center, S)) continue
        const o = unit(e.t)
        if (o < 0 || o === u || !col.has(o)) continue
        const key = 'f' + u + '>f' + o
        let g = agg.get(key)
        if (!g) {
          g = { key, a: u, b: o, n: 0, w: 0, types: {}, confs: {}, best: 'unresolved', real: [] }
          agg.set(key, g)
        }
        addEdge(g, e)
      }
  })

  const minCol = -S.focus.up
  const nodes: LayoutNode[] = [],
    sizes = new Map<string, { w: number; h: number }>()
  Object.keys(cols)
    .map(Number)
    .sort((a, b) => a - b)
    .forEach((c) => {
      cols[c].forEach((u) => {
        const s = cardOf(D, T[u], false, true)
        sizes.set('f' + u, { w: s.w, h: s.h })
        nodes.push({ id: 'f' + u, width: s.w, height: s.h, c })
      })
      if (more[c]) {
        sizes.set('m' + c, { w: 150, h: 34 })
        nodes.push({ id: 'm' + c, width: 150, height: 34, c })
      }
    })
  const edges = Array.from(agg.values()).map((g) => ({ id: g.key, s: 'f' + g.a, t: 'f' + g.b }))
  const algo: Algorithm = L.algorithm === 'manual' ? 'grouped' : L.algorithm
  const key =
    'focus|' +
    algo +
    '|' +
    T[S.focus.i].id +
    '|' +
    nodes.map((n) => n.id + ':' + n.c).join(',') +
    '|' +
    edges.map((e) => e.id).join(',')
  let res: LayoutResult
  if (algo === 'force') {
    nodes.forEach((n) => delete n.opts)
    res = await runLayout(key, nodes, edges, 'RIGHT', aspect, algoOptions('force'))
    const rects = nodes.map((n) => ({ ...(res.pos[n.id] || { x: 0, y: 0 }), w: n.width, h: n.height }))
    removeOverlaps(rects, 24)
    const pos: LayoutResult['pos'] = {}
    nodes.forEach((n, j) => (pos[n.id] = { x: rects[j].x, y: rects[j].y }))
    res = { ...res, pos }
  } else res = await runPartitioned(key, nodes, edges, minCol, aspect, algo === 'down' ? 'DOWN' : 'RIGHT')
  const routed = algo !== 'force'

  // the viewer's positions win; the others avoid them
  const pos = new Map<string, { x: number; y: number }>(),
    atAuto = new Set<string>(),
    pinned = new Set<string>()
  const fixed: { x: number; y: number; w: number; h: number }[] = [],
    free: string[] = []
  for (const nd of nodes) {
    const u = nd.id.charAt(0) === 'f' ? Number(nd.id.slice(1)) : -1
    const m = u >= 0 ? L.nodes[T[u].id] : undefined
    const a = res.pos[nd.id] || { x: 0, y: 0 }
    if (m) {
      pos.set(nd.id, { x: m.x, y: m.y })
      pinned.add(nd.id)
      fixed.push({ x: m.x, y: m.y, w: nd.width, h: nd.height })
      if (Math.abs(m.x - a.x) < 0.5 && Math.abs(m.y - a.y) < 0.5) atAuto.add(nd.id)
    } else free.push(nd.id)
  }
  if (fixed.length && free.length) {
    const rects = free.map((id) => {
      const nd = nodes.find((n) => n.id === id)!,
        a = res.pos[id] || { x: 0, y: 0 }
      return { x: a.x, y: a.y, w: nd.width, h: nd.height }
    })
    placeAvoiding(fixed, rects).forEach((p, j) => {
      const id = free[j],
        a = res.pos[id] || { x: 0, y: 0 }
      pos.set(id, p)
      if (Math.abs(p.x - a.x) < 0.5 && Math.abs(p.y - a.y) < 0.5) atAuto.add(id)
    })
  } else
    for (const id of free) {
      pos.set(id, res.pos[id] || { x: 0, y: 0 })
      atAuto.add(id)
    }

  const boxes: Box[] = nodes.map((nd) => {
    const p = pos.get(nd.id)!,
      s = sizes.get(nd.id)!
    if (nd.id.charAt(0) === 'm') {
      const d = Number(nd.id.slice(1))
      return {
        key: nd.id,
        kind: 'more',
        x: p.x,
        y: p.y,
        w: s.w,
        h: s.h,
        depth: 0,
        title: '+' + more[d] + ' more',
        sub: '',
        color: 'k-group',
        tip: more[d] + ' more at distance ' + Math.abs(d) + ' (strongest relations are drawn)',
      }
    }
    const u = Number(nd.id.slice(1)),
      t = T[u]
    const card = cardOf(D, t, false, true)
    return {
      key: nd.id,
      ref: u,
      id: t.id,
      kind: 'node',
      x: p.x,
      y: p.y,
      w: s.w,
      h: s.h,
      depth: t.depth,
      title: card.title,
      sub: D.subtitle(t),
      kindLabel: card.kind,
      stats: card.stats,
      deps: card.deps,
      color: D.kindColor(t),
      tip: D.reference(t),
      focusCol: nd.c,
      pinned: pinned.has(nd.id),
      locked: !!L.nodes[t.id]?.locked,
    }
  })
  const style = L.edges
  const sceneEdges: SceneEdge[] = Array.from(agg.values()).map((g) => {
    const sk = 'f' + g.a,
      tk = 'f' + g.b
    const route = routed && atAuto.has(sk) && atAuto.has(tk) && style !== 'straight' ? res.edges[g.key] : undefined
    const a = lineAggOf(g)
    return {
      key: g.key,
      s: sk,
      t: tk,
      conf: g.best,
      n: g.n,
      label: summary(g),
      pts: route ? (style === 'smooth' ? smoothRoute(route) : route.slice()) : [],
      local: !route,
      agg: a,
      minor: isMinor(a),
    }
  })
  const scene: Scene = {
    boxes,
    edges: sceneEdges,
    bounds: { x: 0, y: 0, w: 1, h: 1 },
    style,
    prefer: algo === 'down' ? 'v' : 'h',
  }
  const ed = new SceneEditor(scene)
  ed.routeAll()
  ed.bounds()
  const out = new Map<string, SceneEdge[]>(),
    inn = new Map<string, SceneEdge[]>()
  sceneEdges.forEach((e) => {
    if (!out.has(e.s)) out.set(e.s, [])
    if (!inn.has(e.t)) inn.set(e.t, [])
    out.get(e.s)!.push(e)
    inn.get(e.t)!.push(e)
  })
  let callers = 0,
    callees = 0
  col.forEach((c) => {
    if (c < 0) callers++
    else if (c > 0) callees++
  })
  return {
    kind: 'focus',
    center: center.i,
    edges: sceneEdges,
    out,
    inn,
    more,
    scene,
    counts: { boxes: boxes.length, callers, callees, edges: sceneEdges.length },
    keyOf: (i: number) => (col.has(i) ? 'f' + i : null),
    symbolMode,
    layout: { algorithm: L.algorithm, used: algo, fellBack: false },
  }
}
