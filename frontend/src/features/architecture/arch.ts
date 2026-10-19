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

import { cardOf } from './cards'
import { PAD, SceneEditor } from './edit'
import { groupRect, placeAvoiding, removeOverlaps } from './geometry'
import {
  algoOptions,
  routesEdges,
  runLayout,
  runNested,
  type ContainerAlgo,
  type LayoutResult,
  type NestedNode,
  type NestedResult,
} from './layout'
import type { Algorithm, EdgeStyle, NodePos } from './layoutDoc'
import type { Model } from './model'
import { smoothRoute } from './route'
import { font, textWidth } from './text'
import type {
  Agg,
  ArchView,
  Box,
  BoxRole,
  Direction,
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
import { CONF_RANK, SHORT, STRUCTURAL } from './vocab'

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
  const c = cardOf(D, t, ctxNode, flat)
  return { w: c.w, h: c.h }
}

type Rect = { x: number; y: number; w: number; h: number }

/** What the viewer decided for this view: the algorithm, the lines, the positions. */
export interface LayoutInput {
  algorithm: Algorithm
  edges: EdgeStyle
  nodes: Record<string, NodePos>
}

/** Hierarchical layouts run over the whole drawing: above this many boxes, groups are laid out one by one. */
export const HIER_MAX = 900

const sameXY = (a: { x: number; y: number }, b: { x: number; y: number }) =>
  Math.abs(a.x - b.x) < 0.5 && Math.abs(a.y - b.y) < 0.5

/** minor: a line whose every relation is weak (possible, unresolved) or a type use. */
export function isMinor(a: LineAgg): boolean {
  if (!(a.confs.confirmed || a.confs.likely)) return true
  return Object.keys(a.types).every((k) => k === 'references')
}

/**
 * build lays out the view: the scene for the renderer, plus what the
 * inspector and the highlight need.
 *
 * Positions: each container's children are laid out by the algorithm
 * (bottom-up, so a group is laid out with the size of its content); a child
 * the viewer placed keeps its place, relative to its group's origin, and
 * the others avoid it. A group keeps at least the size of its automatic
 * layout and grows to hold children placed beyond it.
 */
export async function buildArch(D: Model, S: ArchState, aspect: number, L: LayoutInput): Promise<ArchView> {
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
  const ctxList = Array.from(ctxUnits).sort((a, b) => T[a].depth - T[b].depth || a - b)

  // the algorithm (Manual: new cards are placed as Grouped places them)
  let algo: Algorithm = L.algorithm === 'manual' ? 'grouped' : L.algorithm
  let fellBack = false
  let drawnCount = 0
  for (let i = 0; i < v.vis.length; i++) drawnCount += v.vis[i]
  if (algo === 'hierarchical' && drawnCount + ctxList.length > HIER_MAX) {
    algo = 'grouped'
    fellBack = true
  }
  const calgo: ContainerAlgo = algo === 'hierarchical' ? 'grouped' : algo
  const manual = (k: number): NodePos | undefined => L.nodes[T[k].id]
  const headW = (c: number) => textWidth(D.displayName(T[c], true), font(600, 12)) + 60

  // Hierarchical: one run over the whole drawing
  let nested: NestedResult | null = null
  if (algo === 'hierarchical') {
    const node = (c: number): NestedNode => {
      if (!groupSet.has(c)) {
        const s = leafSize(D, T[c])
        return { id: 't' + c, width: s.w, height: s.h }
      }
      return { id: 't' + c, children: kids.get(c)!.map(node) }
    }
    const top: NestedNode[] = [node(v.root)]
    ctxList.forEach((c) => {
      const s = leafSize(D, T[c], true)
      top.push({ id: 'c' + c, width: s.w, height: s.h })
    })
    const edges: { id: string; s: string; t: string }[] = []
    agg.forEach((g) => edges.push({ id: g.key, s: (g.ca ? 'c' : 't') + g.a, t: (g.cb ? 'c' : 't') + g.b }))
    const sig = (n: NestedNode): string =>
      n.children ? n.id + '(' + n.children.map(sig).join(',') + ')' : n.id + ':' + n.width + 'x' + n.height
    nested = await runNested('hier|' + top.map(sig).join(',') + '|' + edges.map((e) => e.id).join(','), top, edges, PAD)
    if (!nested) {
      algo = 'grouped'
      fellBack = true
    }
  }

  // bottom-up: each container laid out with the sizes of its children
  const size = new Map<number, { w: number; h: number }>(),
    off = new Map<number, { x: number; y: number }>(),
    rel = new Map<number, { x: number; y: number }>(),
    autoSize = new Map<number, { w: number; h: number }>(),
    lay = new Map<number, LayoutResult>(),
    routed = new Set<number>(),
    atAuto = new Set<number>(),
    pinned = new Set<number>()
  const sizeOf = (k: number) => (groupSet.has(k) ? size.get(k)! : leafSize(D, T[k]))
  const offOf = (k: number) => (groupSet.has(k) ? off.get(k)! : { x: 0, y: 0 })
  // content beyond a group's top or left edge: its rectangle is not where the layout routed lines
  const noOverflow = (k: number) => {
    const o = offOf(k)
    return o.x === 0 && o.y === 0
  }
  const deepFirst = v.groups.slice().sort((a, b) => T[b].depth - T[a].depth)
  for (const c of deepFirst) {
    const ks = kids.get(c)!
    const autoRel = new Map<number, { x: number; y: number }>()
    let autoW: number, autoH: number
    if (nested) {
      for (const k of ks) autoRel.set(k, nested.pos['t' + k] || { x: PAD.left, y: PAD.top })
      const ns = nested.size['t' + c]
      autoW = Math.max(ns ? ns.w : 0, headW(c), 180)
      autoH = Math.max(ns ? ns.h : 0, PAD.top + PAD.bottom + (ks.length ? 0 : 24))
    } else {
      const nodes = ks.map((k) => {
        const s = sizeOf(k)
        return { id: 't' + k, width: s.w, height: s.h }
      })
      const edges = pairs.has(c) ? Array.from(pairs.get(c)!.values()) : []
      const asp = c === v.root ? Math.max(0.6, Math.min(3, Math.round(aspect * 10) / 10)) : 1.6
      const key =
        calgo +
        '|' +
        T[c].id +
        '|' +
        asp +
        '|' +
        nodes.map((n) => n.id + ':' + n.width + 'x' + n.height).join(',') +
        '|' +
        edges.map((e) => e.id).join(',')
      const res = await runLayout(key, nodes, edges, 'DOWN', asp, algoOptions(calgo))
      lay.set(c, res)
      if (routesEdges(calgo, nodes.length)) routed.add(c)
      // a group sits by its origin where the layout put its corner: the
      // content a viewer dragged beyond its top or left edge overflows
      // there, and the group does not shift on the next layout
      for (const k of ks) {
        const p = res.pos['t' + k] || { x: 0, y: 0 }
        autoRel.set(k, { x: PAD.left + p.x, y: PAD.top + p.y })
      }
      autoW = Math.max(res.width + PAD.left + PAD.right, headW(c), 180)
      autoH = Math.max(res.height, ks.length ? 0 : 24) + PAD.top + PAD.bottom
      if (calgo === 'force') {
        // force places cards by their centres: keep them apart
        const rects = ks.map((k) => {
          const p = autoRel.get(k)!,
            o = offOf(k),
            s = sizeOf(k)
          return { x: p.x - o.x, y: p.y - o.y, w: s.w, h: s.h }
        })
        removeOverlaps(rects, 24)
        ks.forEach((k, j) => {
          const o = offOf(k)
          autoRel.set(k, { x: rects[j].x + o.x, y: rects[j].y + o.y })
        })
      }
    }
    // the viewer's positions win; the other children avoid them
    const fixed: Rect[] = [],
      free: number[] = []
    for (const k of ks) {
      const m = manual(k)
      if (m) {
        const o = offOf(k),
          s = sizeOf(k)
        rel.set(k, { x: m.x, y: m.y })
        pinned.add(k)
        fixed.push({ x: m.x - o.x, y: m.y - o.y, w: s.w, h: s.h })
        if (sameXY(m, autoRel.get(k)!) && noOverflow(k)) atAuto.add(k)
      } else free.push(k)
    }
    if (fixed.length && free.length) {
      free.sort((a, b) => autoRel.get(a)!.y - autoRel.get(b)!.y || autoRel.get(a)!.x - autoRel.get(b)!.x)
      const rects = free.map((k) => {
        const p = autoRel.get(k)!,
          o = offOf(k),
          s = sizeOf(k)
        return { x: p.x - o.x, y: p.y - o.y, w: s.w, h: s.h }
      })
      placeAvoiding(fixed, rects).forEach((p, j) => {
        const k = free[j],
          o = offOf(k)
        const r = { x: p.x + o.x, y: p.y + o.y }
        rel.set(k, r)
        if (sameXY(r, autoRel.get(k)!) && noOverflow(k)) atAuto.add(k)
      })
    } else
      for (const k of free) {
        rel.set(k, autoRel.get(k)!)
        if (noOverflow(k)) atAuto.add(k)
      }
    const r = groupRect(
      { x: 0, y: 0 },
      { w: autoW, h: autoH },
      ks.map((k) => {
        const p = rel.get(k)!,
          o = offOf(k),
          s = sizeOf(k)
        return { x: p.x - o.x, y: p.y - o.y, w: s.w, h: s.h }
      }),
      PAD,
    )
    off.set(c, { x: -r.x, y: -r.y })
    size.set(c, { w: Math.round(r.w), h: Math.round(r.h) })
    autoSize.set(c, { w: Math.round(autoW), h: Math.round(autoH) })
  }

  // top level: the scope plus its context
  const rootSize = size.get(v.root)!,
    rootOff = off.get(v.root)!
  const top = [{ id: 't' + v.root, width: rootSize.w, height: rootSize.h }]
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
  const topAlgo = calgo
  let topRes: LayoutResult
  if (nested) {
    const pos: LayoutResult['pos'] = {}
    top.forEach((n) => (pos[n.id] = nested!.pos[n.id] || { x: 0, y: 0 }))
    topRes = { pos, edges: {}, width: 0, height: 0 }
  } else
    topRes = ctxList.length
      ? await runLayout(
          'top|' +
            topAlgo +
            '|' +
            T[v.root].id +
            '|' +
            top.map((n) => n.id + ':' + n.width + 'x' + n.height).join(',') +
            '|' +
            Array.from(topEdges.keys()).join(','),
          top,
          Array.from(topEdges.values()),
          'DOWN',
          Math.max(0.6, Math.min(3, aspect)),
          Object.assign(
            { 'elk.layered.spacing.nodeNodeBetweenLayers': '70', 'elk.spacing.nodeNode': '28' },
            algoOptions(topAlgo),
          ),
        )
      : { pos: { ['t' + v.root]: { x: 0, y: 0 } }, edges: {}, width: 0, height: 0 }
  const topRouted = !nested && ctxList.length > 0 && routesEdges(topAlgo, top.length)
  // top-level positions: the scope by its origin, context cards by their corner
  const topPos = new Map<string, { x: number; y: number }>(),
    topAuto = new Set<string>()
  {
    const autoOf = (id: string) => {
      const p = topRes.pos[id] || { x: 0, y: 0 }
      return { x: p.x, y: p.y }
    }
    const rootClean = rootOff.x === 0 && rootOff.y === 0
    const fixed: Rect[] = [],
      free: string[] = []
    for (const n of top) {
      const k = Number(n.id.slice(1))
      const m = manual(k)
      const a = autoOf(n.id)
      if (m) {
        topPos.set(n.id, { x: m.x, y: m.y })
        pinned.add(k)
        const o = n.id === 't' + v.root ? rootOff : { x: 0, y: 0 }
        fixed.push({ x: m.x - o.x, y: m.y - o.y, w: n.width, h: n.height })
        if (sameXY(m, a) && (rootClean || n.id !== 't' + v.root)) topAuto.add(n.id)
      } else free.push(n.id)
    }
    if (fixed.length && free.length) {
      const rects = free.map((id) => {
        const n = top.find((x) => x.id === id)!,
          a = autoOf(id),
          o = id === 't' + v.root ? rootOff : { x: 0, y: 0 }
        return { x: a.x - o.x, y: a.y - o.y, w: n.width, h: n.height }
      })
      placeAvoiding(fixed, rects, 28).forEach((p, j) => {
        const id = free[j],
          o = id === 't' + v.root ? rootOff : { x: 0, y: 0 }
        const r = { x: p.x + o.x, y: p.y + o.y }
        topPos.set(id, r)
        if (sameXY(r, autoOf(id)) && (rootClean || id !== 't' + v.root)) topAuto.add(id)
      })
    } else
      for (const id of free) {
        topPos.set(id, autoOf(id))
        if (rootClean || id !== 't' + v.root) topAuto.add(id)
      }
  }

  // absolute positions, top-down
  const boxes: Box[] = []
  const abs = new Map<number, Rect>(),
    ctxAbs = new Map<number, Rect>(),
    frame = new Map<number, { x: number; y: number }>()
  function placeGroup(c: number, fx: number, fy: number) {
    const s = size.get(c)!,
      o = off.get(c)!
    abs.set(c, { x: fx - o.x, y: fy - o.y, w: s.w, h: s.h })
    frame.set(c, { x: fx, y: fy })
    for (const k of kids.get(c)!) {
      const p = rel.get(k)!
      if (groupSet.has(k)) placeGroup(k, fx + p.x, fy + p.y)
      else {
        const ls = leafSize(D, T[k])
        abs.set(k, { x: fx + p.x, y: fy + p.y, w: ls.w, h: ls.h })
      }
    }
  }
  const rp = topPos.get('t' + v.root)!
  placeGroup(v.root, rp.x, rp.y)
  const isLocked = (k: number) => !!L.nodes[T[k].id]?.locked
  // boxes in drawing order: groups by depth, then leaves
  v.groups
    .slice()
    .sort((a, b) => T[a].depth - T[b].depth)
    .forEach((c) => {
      const r = abs.get(c)!,
        t = T[c],
        f = frame.get(c)!,
        a = autoSize.get(c)!
      boxes.push({
        key: 't' + c,
        ref: c,
        id: t.id,
        kind: 'group',
        x: r.x,
        y: r.y,
        w: r.w,
        h: r.h,
        depth: t.depth,
        title: D.displayName(t, true),
        sub: D.kindLabel(t) + ' · ' + cardOf(D, t).stats,
        color: D.kindColor(t),
        count: '',
        scope: c === v.root,
        tip: D.reference(t),
        parent: c === v.root ? undefined : 't' + t.parent,
        fx: f.x,
        fy: f.y,
        minW: a.w,
        minH: a.h,
        pinned: pinned.has(c),
        locked: isLocked(c),
      })
    })
  for (let i = 0; i < T.length; i++) {
    if (!v.vis[i] || groupSet.has(i)) continue
    const r = abs.get(i),
      t = T[i]
    if (!r) continue
    const card = cardOf(D, t)
    boxes.push({
      key: 't' + i,
      ref: i,
      id: t.id,
      kind: 'node',
      x: r.x,
      y: r.y,
      w: r.w,
      h: r.h,
      depth: t.depth,
      title: card.title,
      sub: D.subtitle(t),
      kindLabel: card.kind,
      stats: card.stats,
      deps: card.deps,
      color: D.kindColor(t),
      chev: expandable(t) ? 'closed' : null,
      tip: D.reference(t),
      parent: 't' + t.parent,
      pinned: pinned.has(i),
      locked: isLocked(i),
    })
  }
  ctxList.forEach((c) => {
    const p = topPos.get('c' + c)!,
      card = cardOf(D, T[c], true),
      t = T[c]
    ctxAbs.set(c, { x: p.x, y: p.y, w: card.w, h: card.h })
    boxes.push({
      key: 'c' + c,
      ref: c,
      id: t.id,
      kind: 'context',
      x: p.x,
      y: p.y,
      w: card.w,
      h: card.h,
      depth: t.depth,
      title: card.title,
      sub: contextSub(D, t),
      kindLabel: card.kind,
      stats: card.stats,
      color: D.kindColor(t),
      tip: D.reference(t),
      pinned: pinned.has(c),
      locked: isLocked(c),
    })
  })

  // Edges are bundled by the hierarchy. A relation between two drawn items
  // a and b belongs to the lowest group holding both; there it is one
  // "trunk" between the group's children on the way to a and b (sa, sb),
  // routed by the layout when both still are where it put them (from the
  // cards' rectangles otherwise). Every relation between items inside sa
  // and sb shares that trunk; the short "branches" from a to the trunk and
  // from the trunk to b are drawn only when one of their ends is selected
  // or hovered.
  const trunks = new Map<string, SceneEdge>(),
    branches = new Map<string, SceneEdge>(),
    sceneOf = new Map<string, string[]>()
  const logical: LogicalEdge[] = []
  const style = L.edges
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
      if (topRouted && topAuto.has(sa) && topAuto.has(sb)) {
        const r = topRes.edges[sa + '>' + sb]
        if (r) route = r.slice()
      }
    } else {
      const x = lowestGroup(g.a, g.b)
      if (x < 0) return
      c = x
      const ca = D.childOf(x, g.a),
        cb = D.childOf(x, g.b)
      sa = 't' + ca
      sb = 't' + cb
      if (routed.has(x) && atAuto.has(ca) && atAuto.has(cb)) {
        const res = lay.get(x)!,
          f = frame.get(x)!,
          r = res.edges['p' + ca + '>' + cb]
        if (r) route = r.map((q, k) => q + (k % 2 ? f.y + PAD.top : f.x + PAD.left))
      }
    }
    const tkey = 'T' + c + ':' + sa + '>' + sb
    let tr = trunks.get(tkey)
    if (!tr) {
      const useRoute = !!route && style !== 'straight'
      tr = {
        key: tkey,
        s: sa,
        t: sb,
        pts: useRoute ? (style === 'smooth' ? smoothRoute(route!) : route!) : [],
        local: !useRoute,
        conf: 'unresolved',
        n: 0,
        units: new Set(),
        agg: { n: 0, w: 0, types: {}, confs: {}, best: 'unresolved', members: [] },
      }
      trunks.set(tkey, tr)
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
  const edges: SceneEdge[] = []
  trunks.forEach((tr) => {
    tr.conf = tr.agg.best
    tr.n = tr.agg.n
    tr.label = summary(tr.agg)
    tr.minor = isMinor(tr.agg)
    edges.push(tr)
  })
  branches.forEach((b) => {
    b.conf = b.trunk!.agg.best
    b.minor = b.trunk!.minor
    edges.push(b)
  })
  const scene = {
    boxes,
    edges,
    bounds: { x: 0, y: 0, w: 1, h: 1 },
    style,
    prefer: algo === 'right' ? ('h' as const) : ('v' as const),
  }
  const ed = new SceneEditor(scene)
  ed.routeAll()
  ed.bounds()
  scene.edges = edges.filter((e) => e.pts.length >= 4)

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
    scene,
    counts: { boxes: leaves, groups: v.groups.length, context: ctxList.length, edges: trunks.size },
    keyOf: (i: number) => (v.vis[i] ? 't' + i : null),
    layout: { algorithm: L.algorithm, used: algo, fellBack },
  }
}

/** contentOf: the boxes drawn inside a box (by the scene's nesting), cached per scene. */
const nestCache = new WeakMap<object, Map<string, string[]>>()
export function contentOf(scene: { boxes: Box[] }, key: string): string[] {
  let m = nestCache.get(scene)
  if (!m) {
    m = new Map()
    for (const b of scene.boxes) {
      if (!b.parent) continue
      let l = m.get(b.parent)
      if (!l) m.set(b.parent, (l = []))
      l.push(b.key)
    }
    nestCache.set(scene, m)
  }
  const out: string[] = []
  const st = (m.get(key) ?? []).slice()
  while (st.length) {
    const k = st.pop()!
    out.push(k)
    for (const c of m.get(k) ?? []) st.push(c)
  }
  return out
}

/**
 * highlight: the dependency path of drawn items (the selection). Downstream
 * are the items they depend on (following relations forward), upstream the
 * items that depend on them; dir keeps one side only. For an expanded
 * group, its drawn content counts as the selection.
 */
export function highlight(
  D: Model,
  view: View,
  keys: string[],
  depth: number,
  dir: Direction = 'both',
): Highlight | null {
  if (!keys.length || !view.scene) return null
  const sources = new Set<string>()
  const byKey = keys.length > 1 ? new Map(view.scene.boxes.map((b) => [b.key, b])) : null
  for (const key of keys) {
    const box = byKey ? byKey.get(key) : view.scene.boxes.find((b) => b.key === key)
    if (!box) continue
    sources.add(key)
    if (box.kind === 'group') contentOf(view.scene, key).forEach((k) => sources.add(k))
  }
  if (!sources.size) return null
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
  const down = dir === 'in' ? new Map<string, number>() : walk(view.out, 't'),
    up = dir === 'out' ? new Map<string, number>() : walk(view.inn, 's')
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
