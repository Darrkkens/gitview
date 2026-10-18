// Architecture view: the exploration tree drawn as nested boxes.
//
// What is drawn: the scope (a tree item, the repository by default) and its
// children; an expanded child shows its own children inside it, and so on.
// Every relation of the dataset is lifted to the drawn items: a call between
// two functions of collapsed packages is a line between the packages,
// counted with its kind and confidence. Elements outside the scope that
// relate to it are drawn around it as context, at the coarsest level that
// still tells them apart (siblings of the scope's ancestors).
//
// Layout runs per container, bottom-up: a container's children are laid out
// with the relations between them, then the container is a box of that size
// in its parent's layout. Each result is cached by content.

import { runLayout, type LayoutResult } from './layout'
import type { Model } from './model'
import { font, textWidth } from './text'
import type {
  Agg,
  ArchView,
  Box,
  BoxRole,
  EdgeRole,
  Filters,
  Highlight,
  LineAgg,
  LogicalEdge,
  SceneEdge,
  TItem,
  View,
  Visible,
} from './types'
import { CONF_RANK, SHORT, STRUCTURAL, SYMBOL } from './vocab'

const PAD = { top: 42, left: 14, right: 14, bottom: 14 }

export interface ArchState {
  root: number
  open: Set<number>
  filters: Filters
}

export function shown(t: TItem, f: Filters): boolean {
  if (t.kind === 'extgroup' || t.ext || (t.n && t.n.ext)) {
    if (!f.ext) return false
  }
  if (t.n && f.kinds[t.kind] === false) return false
  return true
}

export const expandable = (t: TItem) => t.kids.length > 0

/** visible computes which tree items are drawn and how every item maps to a drawn one. */
export function visible(D: Model, S: ArchState): Visible {
  const T = D.T
  const f = S.filters,
    root = S.root
  const vis = new Uint8Array(T.length)
  const groups: number[] = []
  vis[root] = 1
  const stack = [root]
  while (stack.length) {
    const c = stack.pop()!
    groups.push(c)
    for (const k of T[c].kids) {
      if (!shown(T[k], f)) continue
      vis[k] = 1
      if (expandable(T[k]) && S.open.has(k)) stack.push(k)
    }
  }
  // unit: the drawn item standing for each item inside the scope;
  // ctx: the context item for an item outside it.
  const chain = new Set(D.ancestors(root).concat([root]))
  const unit = new Int32Array(T.length).fill(-1),
    ctx = new Int32Array(T.length).fill(-1)
  const inside = new Uint8Array(T.length)
  for (const i of D.order) {
    const p = T[i].parent
    inside[i] = i === root || (p >= 0 && inside[p]) ? 1 : 0
    if (inside[i]) unit[i] = vis[i] ? i : p >= 0 ? unit[p] : -1
    else if (!chain.has(i)) ctx[i] = chain.has(p) ? (shown(T[i], f) ? i : -2) : p >= 0 ? ctx[p] : -1
  }
  return { vis, groups, unit, ctx, inside, root }
}

function newAgg(key: string, a: number, b: number): Agg {
  return { key, a, b, n: 0, w: 0, types: {}, confs: {}, best: 'unresolved', real: [] }
}

/** aggregate lifts the dataset's relations to drawn items. */
function aggregate(D: Model, S: ArchState, v: Visible): Map<string, Agg> {
  const f = S.filters
  const agg = new Map<string, Agg>()
  for (const e of D.E) {
    if (STRUCTURAL[e.type] || f.rels[e.type] === false || f.confs[e.conf] === false) continue
    let a = v.unit[e.s],
      b = v.unit[e.t],
      ca = false,
      cb = false
    if (a < 0) {
      a = v.ctx[e.s]
      ca = true
    }
    if (b < 0) {
      b = v.ctx[e.t]
      cb = true
    }
    if (a < 0 || b < 0 || a === b || (ca && cb)) continue
    if (!ca && !cb && (D.isAncestor(a, b) || D.isAncestor(b, a))) continue
    const key = (ca ? 'c' : 't') + a + '>' + (cb ? 'c' : 't') + b
    let g = agg.get(key)
    if (!g) {
      g = newAgg(key, a, b)
      g.ca = ca
      g.cb = cb
      agg.set(key, g)
    }
    addEdge(g, e)
  }
  return agg
}

export function addEdge(g: Agg, e: { i: number; type: string; conf: string; w: number }) {
  g.n++
  g.w += e.w
  g.types[e.type] = (g.types[e.type] || 0) + 1
  g.confs[e.conf] = (g.confs[e.conf] || 0) + 1
  if (CONF_RANK[e.conf] > CONF_RANK[g.best]) g.best = e.conf as Agg['best']
  g.real.push(e.i)
}

/** summary: the line label, "3 calls · 1 type ref". */
export function summary(g: { types: Record<string, number> }): string {
  return Object.keys(g.types)
    .sort((x, y) => g.types[y] - g.types[x])
    .slice(0, 3)
    .map((k) => {
      const s = SHORT[k] ?? k
      return g.types[k] + ' ' + s + (g.types[k] > 1 && !/s$/.test(s) && k !== 'implements' && k !== 'embeds' ? 's' : '')
    })
    .join(' · ')
}

/** lineAgg wraps a single pair as the aggregate of a line. */
export function lineAggOf(g: Agg): LineAgg {
  return { n: g.n, w: g.w, types: g.types, confs: g.confs, best: g.best, members: [g] }
}

/** realOf lists the dataset edges a line stands for. */
export function realOf(a: LineAgg): number[] {
  if (a.members.length === 1) return a.members[0].real
  const out: number[] = []
  for (const m of a.members) for (const ei of m.real) out.push(ei)
  return out
}

// ---------- sizes ----------
const contextSub = (D: Model, t: TItem) => 'outside the scope · ' + D.kindLabel(t)

/**
 * leafSize: the box of an item drawn collapsed; flat when it is drawn
 * without its parent around it (focus mode).
 */
export function leafSize(D: Model, t: TItem, ctxNode?: boolean, flat?: boolean): { w: number; h: number } {
  const title = D.displayName(t, !ctxNode && !flat),
    sub = ctxNode ? contextSub(D, t) : D.subtitle(t)
  const tw = textWidth(title, font(500, 12.5)) + 22 + (expandable(t) && !ctxNode ? 20 : 0) + 12
  const sw = textWidth(sub, font(400, 11)) + 34
  const symbol = !!t.n && !!SYMBOL[t.kind]
  return {
    w: Math.round(Math.max(symbol ? 140 : 160, Math.min(Math.max(tw, sw), symbol ? 280 : 300))),
    h: symbol ? 42 : 46,
  }
}

type Rect = { x: number; y: number; w: number; h: number }

interface Trunk extends SceneEdge {
  pts: number[]
  dir?: 'v' | 'h'
  ends?: ([number, number] | null)[]
}

interface Branch extends SceneEdge {
  trunk: Trunk
  end: 0 | 1
}

/** build lays out the view: the scene for the renderer, plus what the inspector and the highlight need. */
export async function buildArch(D: Model, S: ArchState, aspect: number): Promise<ArchView> {
  const T = D.T
  const v = visible(D, S)
  const agg = aggregate(D, S, v)
  const groupSet = new Set(v.groups)
  // children of each group that are drawn
  const kids = new Map<number, number[]>()
  v.groups.forEach((c) =>
    kids.set(
      c,
      T[c].kids.filter((k) => v.vis[k]),
    ),
  )
  // the lowest drawn group holding both a and b
  const lowestGroup = (a: number, b: number) => {
    let c = T[a].parent
    while (c >= 0 && !(groupSet.has(c) && (c === b || D.isAncestor(c, b)))) c = T[c].parent
    return c
  }
  // layout pairs per container: relations lifted to direct children
  const pairs = new Map<number, Map<string, { id: string; s: string; t: string }>>()
  const ctxUnits = new Set<number>()
  agg.forEach((g) => {
    if (g.ca || g.cb) {
      ctxUnits.add(g.ca ? g.a : g.b)
      return
    }
    const c = lowestGroup(g.a, g.b)
    if (c < 0) return
    const sa = D.childOf(c, g.a),
      sb = D.childOf(c, g.b)
    if (sa < 0 || sb < 0 || sa === sb) return
    if (!pairs.has(c)) pairs.set(c, new Map())
    const pk = 'p' + sa + '>' + sb
    pairs.get(c)!.set(pk, { id: pk, s: 't' + sa, t: 't' + sb })
  })
  // bottom-up layouts
  const size = new Map<number, { w: number; h: number }>(),
    lay = new Map<number, LayoutResult>()
  const deepFirst = v.groups.slice().sort((a, b) => T[b].depth - T[a].depth)
  for (const c of deepFirst) {
    const ks = kids.get(c)!
    const nodes = ks.map((k) => {
      const s = groupSet.has(k) ? size.get(k)! : leafSize(D, T[k])
      return { id: 't' + k, width: s.w, height: s.h }
    })
    const edges = pairs.has(c) ? Array.from(pairs.get(c)!.values()) : []
    const asp = c === v.root ? Math.max(0.6, Math.min(3, Math.round(aspect * 10) / 10)) : 1.6
    const key =
      T[c].id +
      '|' +
      asp +
      '|' +
      nodes.map((n) => n.id + ':' + n.width + 'x' + n.height).join(',') +
      '|' +
      edges.map((e) => e.id).join(',')
    const res = await runLayout(key, nodes, edges, 'DOWN', asp)
    lay.set(c, res)
    const headW = textWidth(D.displayName(T[c], true), font(600, 12)) + 60
    const w = Math.max(res.width + PAD.left + PAD.right, headW, 180)
    const h = Math.max(res.height, ks.length ? 0 : 24) + PAD.top + PAD.bottom
    size.set(c, { w: Math.round(w), h: Math.round(h) })
  }
  // top level: the scope plus its context
  const top = [{ id: 't' + v.root, width: size.get(v.root)!.w, height: size.get(v.root)!.h }]
  const ctxList = Array.from(ctxUnits).sort((a, b) => T[a].depth - T[b].depth || a - b)
  ctxList.forEach((c) => {
    const s = leafSize(D, T[c], true)
    top.push({ id: 'c' + c, width: s.w, height: s.h })
  })
  const topEdges = new Map<string, { id: string; s: string; t: string }>()
  agg.forEach((g) => {
    if (!(g.ca || g.cb)) return
    const s = g.ca ? 'c' + g.a : 't' + v.root,
      t = g.cb ? 'c' + g.b : 't' + v.root
    topEdges.set(s + '>' + t, { id: s + '>' + t, s, t })
  })
  const topRes: LayoutResult = ctxList.length
    ? await runLayout(
        'top|' +
          T[v.root].id +
          '|' +
          top.map((n) => n.id + ':' + n.width + 'x' + n.height).join(',') +
          '|' +
          Array.from(topEdges.keys()).join(','),
        top,
        Array.from(topEdges.values()),
        'DOWN',
        Math.max(0.6, Math.min(3, aspect)),
        { 'elk.layered.spacing.nodeNodeBetweenLayers': '70', 'elk.spacing.nodeNode': '28' },
      )
    : { pos: { ['t' + v.root]: { x: 0, y: 0 } }, edges: {}, width: 0, height: 0 }

  // absolute positions, top-down
  const boxes: Box[] = []
  const abs = new Map<number, Rect>(),
    ctxAbs = new Map<number, Rect>(),
    origin = new Map<number, { x: number; y: number }>()
  function placeGroup(c: number, x: number, y: number) {
    const s = size.get(c)!
    abs.set(c, { x, y, w: s.w, h: s.h })
    const res = lay.get(c)!
    const ox = x + PAD.left + Math.max(0, (s.w - PAD.left - PAD.right - res.width) / 2),
      oy = y + PAD.top
    origin.set(c, { x: ox, y: oy })
    for (const k of kids.get(c)!) {
      const p = res.pos['t' + k] || { x: 0, y: 0 }
      if (groupSet.has(k)) placeGroup(k, ox + p.x, oy + p.y)
      else {
        const ls = leafSize(D, T[k])
        abs.set(k, { x: ox + p.x, y: oy + p.y, w: ls.w, h: ls.h })
      }
    }
  }
  const rp = topRes.pos['t' + v.root] || { x: 0, y: 0 }
  placeGroup(v.root, rp.x, rp.y)
  // boxes in drawing order: groups by depth, then leaves
  v.groups
    .slice()
    .sort((a, b) => T[a].depth - T[b].depth)
    .forEach((c) => {
      const r = abs.get(c)!,
        t = T[c]
      boxes.push({
        key: 't' + c,
        ref: c,
        kind: 'group',
        x: r.x,
        y: r.y,
        w: r.w,
        h: r.h,
        depth: t.depth,
        title: D.displayName(t, true),
        sub: D.subtitle(t),
        color: D.kindColor(t),
        count: '',
        scope: c === v.root,
        tip: D.reference(t),
      })
    })
  for (let i = 0; i < T.length; i++) {
    if (!v.vis[i] || groupSet.has(i)) continue
    const r = abs.get(i),
      t = T[i]
    if (!r) continue
    boxes.push({
      key: 't' + i,
      ref: i,
      kind: 'node',
      x: r.x,
      y: r.y,
      w: r.w,
      h: r.h,
      depth: t.depth,
      title: D.displayName(t, true),
      sub: D.subtitle(t),
      color: D.kindColor(t),
      chev: expandable(t) ? 'closed' : null,
      tip: D.reference(t),
    })
  }
  ctxList.forEach((c) => {
    const p = topRes.pos['c' + c] || { x: 0, y: 0 },
      s = leafSize(D, T[c], true),
      t = T[c]
    ctxAbs.set(c, { x: p.x, y: p.y, w: s.w, h: s.h })
    boxes.push({
      key: 'c' + c,
      ref: c,
      kind: 'context',
      x: p.x,
      y: p.y,
      w: s.w,
      h: s.h,
      depth: t.depth,
      title: D.displayName(t),
      sub: contextSub(D, t),
      color: D.kindColor(t),
      tip: D.reference(t),
    })
  })
  const rectOf = (key: string): Rect =>
    (key.charAt(0) === 'c' ? ctxAbs.get(Number(key.slice(1))) : abs.get(Number(key.slice(1)))) ?? {
      x: 0,
      y: 0,
      w: 1,
      h: 1,
    }

  // Edges are bundled by the hierarchy. A relation between two drawn items
  // a and b belongs to the lowest group holding both; there it is one
  // "trunk" between the group's children on the way to a and b (sa, sb),
  // routed by ELK with the group's layout. Every relation between items
  // inside sa and sb shares that trunk; the short "branches" from a to the
  // trunk and from the trunk to b are drawn only when one of their ends is
  // selected or hovered.
  const trunks = new Map<string, Trunk>(),
    branches = new Map<string, Branch>(),
    sceneOf = new Map<string, string[]>()
  const logical: LogicalEdge[] = []
  const ports = new Map<string, { e: Trunk; end: 0 | 1; other: number }[]>()
  const wantPort = (key: string, side: string, e: Trunk, end: 0 | 1, other: number) => {
    const k = key + side
    if (!ports.has(k)) ports.set(k, [])
    ports.get(k)!.push({ e, end, other })
  }
  function connector(e: Trunk, A: Rect, B: Rect, sk: string, tk: string) {
    if (B.y >= A.y + A.h + 8) {
      wantPort(sk, 'b', e, 0, B.x + B.w / 2)
      wantPort(tk, 't', e, 1, A.x + A.w / 2)
      e.dir = 'v'
    } else if (A.y >= B.y + B.h + 8) {
      wantPort(sk, 't', e, 0, B.x + B.w / 2)
      wantPort(tk, 'b', e, 1, A.x + A.w / 2)
      e.dir = 'v'
    } else if (B.x >= A.x + A.w) {
      wantPort(sk, 'r', e, 0, B.y + B.h / 2)
      wantPort(tk, 'l', e, 1, A.y + A.h / 2)
      e.dir = 'h'
    } else {
      wantPort(sk, 'l', e, 0, B.y + B.h / 2)
      wantPort(tk, 'r', e, 1, A.y + A.h / 2)
      e.dir = 'h'
    }
    e.ends = [null, null]
  }
  function merge(into: LineAgg, g: Agg) {
    into.n += g.n
    into.w += g.w
    for (const k in g.types) into.types[k] = (into.types[k] || 0) + g.types[k]
    for (const k in g.confs) into.confs[k] = (into.confs[k] || 0) + g.confs[k]
    if (CONF_RANK[g.best] > CONF_RANK[into.best]) into.best = g.best
    into.members.push(g)
  }
  agg.forEach((g) => {
    const sk = (g.ca ? 'c' : 't') + g.a,
      tk = (g.cb ? 'c' : 't') + g.b
    logical.push({ key: g.key, s: sk, t: tk, agg: g })
    let c: number | 'top', sa: string, sb: string
    let route: number[] | null = null
    if (g.ca || g.cb) {
      c = 'top'
      sa = g.ca ? sk : 't' + v.root
      sb = g.cb ? tk : 't' + v.root
      route = topRes.edges[sa + '>' + sb] || null
    } else {
      const x = lowestGroup(g.a, g.b)
      if (x < 0) return
      c = x
      sa = 't' + D.childOf(x, g.a)
      sb = 't' + D.childOf(x, g.b)
      const res = lay.get(x)!,
        o = origin.get(x)!,
        r = res.edges['p' + sa.slice(1) + '>' + sb.slice(1)]
      if (r) route = r.map((q, k) => q + (k % 2 ? o.y : o.x))
    }
    const tkey = 'T' + c + ':' + sa + '>' + sb
    let tr = trunks.get(tkey)
    if (!tr) {
      tr = {
        key: tkey,
        s: sa,
        t: sb,
        pts: route ?? [],
        conf: 'unresolved',
        n: 0,
        units: new Set(),
        agg: { n: 0, w: 0, types: {}, confs: {}, best: 'unresolved', members: [] },
      }
      trunks.set(tkey, tr)
      if (!route) connector(tr, rectOf(sa), rectOf(sb), sa, sb)
    }
    merge(tr.agg, g)
    tr.units!.add(sk)
    tr.units!.add(tk)
    const list = [tkey]
    if (sk !== sa) {
      const bk = 'B' + sk + '@' + tkey
      if (!branches.has(bk))
        branches.set(bk, {
          key: bk,
          s: sk,
          t: sa,
          trunk: tr,
          end: 0,
          branch: true,
          units: new Set([sk]),
          agg: tr.agg,
          pts: [],
          conf: 'unresolved',
          n: 1,
        })
      list.push(bk)
    }
    if (tk !== sb) {
      const bk = 'B' + tkey + '@' + tk
      if (!branches.has(bk))
        branches.set(bk, {
          key: bk,
          s: sb,
          t: tk,
          trunk: tr,
          end: 1,
          branch: true,
          units: new Set([tk]),
          agg: tr.agg,
          pts: [],
          conf: 'unresolved',
          n: 1,
        })
      list.push(bk)
    }
    sceneOf.set(g.key, list)
  })
  // spread connector ends along each side, ordered by where they go
  ports.forEach((list, k) => {
    const side = k.slice(-1),
      r = rectOf(k.slice(0, -1))
    list.sort((x, y) => x.other - y.other)
    const n = list.length
    list.forEach((p, j) => {
      const f = (j + 1) / (n + 1)
      const along = side === 't' || side === 'b' ? r.x + r.w * (0.15 + 0.7 * f) : r.y + r.h * (0.2 + 0.6 * f)
      p.e.ends![p.end] =
        side === 't'
          ? [along, r.y]
          : side === 'b'
            ? [along, r.y + r.h]
            : side === 'l'
              ? [r.x, along]
              : [r.x + r.w, along]
    })
  })
  const elbow = (a: number[], b: number[], dir: 'v' | 'h') => {
    let pts: number[]
    if (dir === 'v') {
      const my = (a[1] + b[1]) / 2
      pts = [a[0], a[1], a[0], my, b[0], my, b[0], b[1]]
    } else {
      const mx = (a[0] + b[0]) / 2
      pts = [a[0], a[1], mx, a[1], mx, b[1], b[0], b[1]]
    }
    return Math.abs(pts[0] - pts[6]) < 1 || Math.abs(pts[1] - pts[7]) < 1 ? [a[0], a[1], b[0], b[1]] : pts
  }
  trunks.forEach((tr) => {
    if (!tr.pts.length && tr.ends && tr.ends[0] && tr.ends[1]) tr.pts = elbow(tr.ends[0], tr.ends[1], tr.dir!)
  })
  // a branch runs inside the trunk's end box, from the inner item to the
  // point where the trunk leaves (or enters) the box
  branches.forEach((b) => {
    const tp = b.trunk.pts
    if (tp.length < 4) return
    const P = b.end === 0 ? [tp[0], tp[1]] : [tp[tp.length - 2], tp[tp.length - 1]]
    const R = rectOf(b.end === 0 ? b.s : b.t),
      box = b.end === 0 ? rectOf(b.t) : rectOf(b.s)
    const onTopOrBottom = Math.abs(P[1] - box.y) < 2 || Math.abs(P[1] - (box.y + box.h)) < 2
    if (onTopOrBottom) {
      const below = P[1] > R.y + R.h / 2
      const q = [R.x + R.w / 2, below ? R.y + R.h : R.y]
      b.pts = b.end === 0 ? elbow(q, P, 'v') : elbow(P, q, 'v')
    } else {
      const right = P[0] > R.x + R.w / 2
      const q = [right ? R.x + R.w : R.x, R.y + R.h / 2]
      b.pts = b.end === 0 ? elbow(q, P, 'h') : elbow(P, q, 'h')
    }
    b.conf = b.trunk.agg.best
    b.n = 1
  })
  const edges: SceneEdge[] = []
  trunks.forEach((tr) => {
    tr.conf = tr.agg.best
    tr.n = tr.agg.n
    tr.label = summary(tr.agg)
    if (tr.pts.length >= 4) edges.push(tr)
  })
  branches.forEach((b) => {
    if (b.pts.length >= 4) edges.push(b)
  })

  let x0 = Infinity,
    y0 = Infinity,
    x1 = -Infinity,
    y1 = -Infinity
  boxes.forEach((b) => {
    x0 = Math.min(x0, b.x)
    y0 = Math.min(y0, b.y)
    x1 = Math.max(x1, b.x + b.w)
    y1 = Math.max(y1, b.y + b.h)
  })
  if (!boxes.length) {
    x0 = y0 = 0
    x1 = y1 = 1
  }
  // adjacency between drawn items (logical relations) for the highlight
  const out = new Map<string, LogicalEdge[]>(),
    inn = new Map<string, LogicalEdge[]>()
  logical.forEach((e) => {
    if (!out.has(e.s)) out.set(e.s, [])
    if (!inn.has(e.t)) inn.set(e.t, [])
    out.get(e.s)!.push(e)
    inn.get(e.t)!.push(e)
  })
  const leaves = boxes.filter((b) => b.kind !== 'group').length
  return {
    kind: 'arch',
    v,
    edges: logical,
    out,
    inn,
    sceneOf,
    scene: { boxes, edges, bounds: { x: x0, y: y0, w: x1 - x0, h: y1 - y0 } },
    counts: { boxes: leaves, groups: v.groups.length, context: ctxList.length, edges: trunks.size },
    keyOf: (i: number) => (v.vis[i] ? 't' + i : null),
  }
}

/**
 * highlight: the dependency path of a drawn item. Downstream are the items
 * it depends on (following relations forward), upstream the items that
 * depend on it. For an expanded group, its drawn content counts as the
 * selection.
 */
export function highlight(D: Model, view: View, key: string | null, depth: number): Highlight | null {
  if (!key || !view.scene) return null
  const box = view.scene.boxes.find((b) => b.key === key)
  if (!box) return null
  const sources = new Set([key])
  if (box.kind === 'group')
    view.scene.boxes.forEach((b) => {
      if (b.key !== key && b.x >= box.x && b.y >= box.y && b.x + b.w <= box.x + box.w && b.y + b.h <= box.y + box.h)
        sources.add(b.key)
    })
  const max = depth || Infinity
  function walk(adj: View['out'], end: 's' | 't') {
    const seen = new Map<string, number>()
    let frontier = Array.from(sources),
      d = 0
    while (frontier.length && d < max) {
      d++
      const next: string[] = []
      for (const k of frontier)
        for (const e of adj.get(k) || []) {
          const o = e[end]
          if (sources.has(o) || seen.has(o)) continue
          seen.set(o, d)
          next.push(o)
        }
      frontier = next
    }
    return seen
  }
  const down = walk(view.out, 't'),
    up = walk(view.inn, 's')
  // roles: out/in for direct neighbours and their relations, out2/in2
  // further along the path (drawn lighter)
  const boxes = new Map<string, BoxRole>(),
    edges = new Map<string, EdgeRole>()
  sources.forEach((k) => boxes.set(k, 'sel'))
  down.forEach((d, k) => boxes.set(k, d === 1 ? 'out' : 'out2'))
  up.forEach((d, k) => {
    const r = d === 1 ? 'in' : 'in2'
    const cur = boxes.get(k)
    boxes.set(k, cur === 'out' || cur === 'out2' ? (cur === 'out' || r === 'in' ? 'both' : 'out2') : r)
  })
  const RANK: Record<EdgeRole, number> = { out: 4, in: 3, out2: 2, in2: 1 }
  const mark = (k: string, role: EdgeRole) => {
    for (const sk of view.sceneOf ? view.sceneOf.get(k) || [] : [k])
      if (!edges.has(sk) || RANK[role] > RANK[edges.get(sk)!]) edges.set(sk, role)
  }
  view.edges.forEach((e) => {
    if ((sources.has(e.s) || down.has(e.s)) && down.has(e.t) && !sources.has(e.t))
      mark(e.key, sources.has(e.s) ? 'out' : 'out2')
    else if ((sources.has(e.t) || up.has(e.t)) && up.has(e.s) && !sources.has(e.s))
      mark(e.key, sources.has(e.t) ? 'in' : 'in2')
  })
  // groups that hold a highlighted item stay readable
  const keep = new Set<string>()
  boxes.forEach((_r, k) => {
    if (k.charAt(0) !== 't') return
    for (const a of D.ancestors(Number(k.slice(1)))) keep.add('t' + a)
  })
  return { boxes, edges, keep, down: down.size, up: up.size }
}
