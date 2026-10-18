// Layout of the journeys: ELK's layered algorithm with fixed columns
// (partitions), run in the page's thread through the bundle's built-in fake
// worker (no Worker, no network). Results are cached by content, so coming
// back to a route costs nothing. Text is measured with a 2D context.

import ELK, { type ElkNode } from 'elkjs/lib/elk.bundled.js'
import type { Bounds } from './Canvas'

export interface LNode {
  id: string
  width: number
  height: number
  /** column (partition), 0 = leftmost / topmost */
  c: number
}

export interface LEdge {
  id: string
  s: string
  t: string
}

export interface LayoutResult {
  pos: Record<string, { x: number; y: number }>
  edges: Record<string, number[]>
  width: number
  height: number
  ms: number
}

let elk: InstanceType<typeof ELK> | null = null
function engine() {
  if (!elk) {
    try {
      elk = new ELK()
    } catch {
      elk = null
    }
  }
  return elk
}

const cache = new Map<string, LayoutResult>()

/** columns places nodes column by column: the fallback when ELK fails. */
function columns(nodes: LNode[], dir: 'RIGHT' | 'DOWN'): LayoutResult {
  const cols = new Map<number, LNode[]>()
  nodes.forEach((n) => {
    const l = cols.get(n.c) ?? []
    l.push(n)
    cols.set(n.c, l)
  })
  const pos: LayoutResult['pos'] = {}
  let main = 0,
    cross = 0
  for (const c of [...cols.keys()].sort((a, b) => a - b)) {
    const list = cols.get(c)!
    let off = 0,
      thick = 0
    for (const n of list) {
      if (dir === 'RIGHT') {
        pos[n.id] = { x: main, y: off }
        off += n.height + 14
        thick = Math.max(thick, n.width)
      } else {
        pos[n.id] = { x: off, y: main }
        off += n.width + 14
        thick = Math.max(thick, n.height)
      }
    }
    cross = Math.max(cross, off)
    main += thick + 64
  }
  return dir === 'RIGHT'
    ? { pos, edges: {}, width: main, height: cross, ms: 0 }
    : { pos, edges: {}, width: cross, height: main, ms: 0 }
}

/**
 * layoutColumns lays out a flow whose columns are fixed (node.c), left to
 * right (RIGHT) or top to bottom (DOWN).
 */
export async function layoutColumns(
  key: string,
  nodes: LNode[],
  edges: LEdge[],
  dir: 'RIGHT' | 'DOWN',
  aspect: number,
): Promise<LayoutResult> {
  const hit = cache.get(key)
  if (hit) return hit
  const t0 = performance.now()
  const minCol = nodes.reduce((m, n) => Math.min(m, n.c), Infinity)
  const ids = new Set(nodes.map((n) => n.id))
  let res: LayoutResult
  const e = engine()
  if (!e || !nodes.length) res = columns(nodes, dir)
  else {
    const graph: ElkNode = {
      id: 'root',
      layoutOptions: {
        'elk.algorithm': 'layered',
        'elk.direction': dir,
        'elk.edgeRouting': 'ORTHOGONAL',
        'elk.aspectRatio': String(aspect),
        'elk.spacing.nodeNode': '14',
        'elk.layered.spacing.nodeNodeBetweenLayers': '64',
        'elk.spacing.edgeNode': '14',
        'elk.spacing.edgeEdge': '8',
        'elk.layered.spacing.edgeNodeBetweenLayers': '14',
        'elk.spacing.componentComponent': '30',
        'elk.layered.thoroughness': '4',
        'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES',
        'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
        'elk.separateConnectedComponents': 'false',
        'elk.partitioning.activate': 'true',
        'elk.padding': '[top=0,left=0,bottom=0,right=0]',
      },
      children: nodes.map((n) => ({
        id: n.id,
        width: n.width,
        height: n.height,
        layoutOptions: { 'elk.partitioning.partition': String(n.c - minCol) },
      })),
      edges: edges
        .filter((x) => x.s !== x.t && ids.has(x.s) && ids.has(x.t))
        .map((x) => ({ id: x.id, sources: [x.s], targets: [x.t] })),
    }
    try {
      const out = await e.layout(graph)
      const pos: LayoutResult['pos'] = {}
      const routes: LayoutResult['edges'] = {}
      for (const c of out.children ?? []) pos[c.id] = { x: c.x ?? 0, y: c.y ?? 0 }
      for (const ed of out.edges ?? []) {
        const s = ed.sections?.[0]
        if (!s) continue
        const pts = [s.startPoint.x, s.startPoint.y]
        for (const b of s.bendPoints ?? []) pts.push(b.x, b.y)
        pts.push(s.endPoint.x, s.endPoint.y)
        routes[ed.id] = pts
      }
      res = { pos, edges: routes, width: out.width ?? 0, height: out.height ?? 0, ms: 0 }
    } catch {
      res = columns(nodes, dir)
    }
  }
  res.ms = performance.now() - t0
  if (cache.size > 200) cache.delete(cache.keys().next().value!)
  cache.set(key, res)
  return res
}

// ---------- text ----------

let ctx: CanvasRenderingContext2D | null = null
let family = ''
const widths = new Map<string, number>()

export function font(weight: number, size: number): string {
  if (!family) {
    family = getComputedStyle(document.documentElement).fontFamily || 'sans-serif'
  }
  return weight + ' ' + size + 'px ' + family
}

export function textWidth(text: string, f: string): number {
  const key = f + '\u0000' + text
  let w = widths.get(key)
  if (w === undefined) {
    if (!ctx) ctx = document.createElement('canvas').getContext('2d')
    if (ctx) {
      ctx.font = f
      w = ctx.measureText(text).width
    } else w = text.length * 7
    if (widths.size > 20000) widths.clear()
    widths.set(key, w)
  }
  return w
}

/** fitText shortens text with an ellipsis to fit max pixels. */
export function fitText(text: string, f: string, max: number): string {
  if (!text) return ''
  if (textWidth(text, f) <= max) return text
  let lo = 0,
    hi = text.length
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1
    if (textWidth(text.slice(0, mid) + '…', f) <= max) lo = mid
    else hi = mid - 1
  }
  return text.slice(0, lo) + '…'
}

/** boundsOf: the rectangle around boxes. */
export function boundsOf(boxes: { x: number; y: number; w: number; h: number }[]): Bounds {
  let x0 = Infinity,
    y0 = Infinity,
    x1 = -Infinity,
    y1 = -Infinity
  for (const b of boxes) {
    x0 = Math.min(x0, b.x)
    y0 = Math.min(y0, b.y)
    x1 = Math.max(x1, b.x + b.w)
    y1 = Math.max(y1, b.y + b.h)
  }
  if (!boxes.length) return { x: 0, y: 0, w: 1, h: 1 }
  return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 }
}
