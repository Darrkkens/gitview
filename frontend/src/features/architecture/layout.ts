// Layout: ELK (run in the page's thread through its built-in fake worker)
// lays out one container at a time. Results are cached by the container's
// content, so expanding a package re-lays only that package and its
// ancestors, and coming back to a view costs nothing.

import ELK from 'elkjs/lib/elk.bundled.js'
import type { ElkNode } from 'elkjs/lib/elk-api'

/** Children beyond which a container is packed instead of layered. */
const BIG = 260

export interface LayoutNode {
  id: string
  width: number
  height: number
  /** Column of a partitioned flow (focus mode). */
  c?: number
  opts?: Record<string, string>
}

export interface LayoutEdge {
  id: string
  s: string
  t: string
}

export interface LayoutResult {
  pos: Record<string, { x: number; y: number }>
  edges: Record<string, number[]>
  width: number
  height: number
}

let elk: InstanceType<typeof ELK> | null = null
let elkFailed = false
function engine() {
  if (!elk && !elkFailed) {
    try {
      elk = new ELK()
    } catch {
      elkFailed = true
    }
  }
  return elk
}

const cache = new Map<string, LayoutResult>()
export const layoutStats = { runs: 0, hits: 0, ms: 0, last: 0, failures: 0 }

const packed = (n: number, extra?: Record<string, string>) => n > BIG && !(extra && extra['elk.partitioning.activate'])

function options(dir: string, aspect: number, n: number, extra?: Record<string, string>): Record<string, string> {
  const o: Record<string, string> = packed(n, extra)
    ? {
        'elk.algorithm': 'rectpacking',
        'elk.aspectRatio': String(aspect),
        'elk.spacing.nodeNode': '16',
        'elk.padding': '[top=0,left=0,bottom=0,right=0]',
      }
    : {
        'elk.algorithm': 'layered',
        'elk.direction': dir,
        'elk.edgeRouting': 'ORTHOGONAL',
        'elk.aspectRatio': String(aspect),
        'elk.spacing.nodeNode': '22',
        'elk.layered.spacing.nodeNodeBetweenLayers': '46',
        'elk.spacing.edgeNode': '14',
        'elk.spacing.edgeEdge': '8',
        'elk.layered.spacing.edgeNodeBetweenLayers': '14',
        'elk.spacing.componentComponent': '30',
        'elk.layered.thoroughness': '4',
        'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES',
        'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
        'elk.separateConnectedComponents': 'true',
        'elk.padding': '[top=0,left=0,bottom=0,right=0]',
      }
  return Object.assign(o, extra || {})
}

/** grid places nodes in rows, the fallback when ELK is missing or fails. */
function grid(nodes: LayoutNode[], aspect: number): LayoutResult {
  const area = nodes.reduce((s, n) => s + (n.width + 20) * (n.height + 20), 0)
  const rowW = Math.max(Math.sqrt(area * aspect), 200)
  let x = 0,
    y = 0,
    rowH = 0,
    W = 0
  const pos: LayoutResult['pos'] = {}
  nodes.forEach((n) => {
    if (x > 0 && x + n.width > rowW) {
      x = 0
      y += rowH + 20
      rowH = 0
    }
    pos[n.id] = { x, y }
    x += n.width + 20
    rowH = Math.max(rowH, n.height)
    W = Math.max(W, x - 20)
  })
  return { pos, edges: {}, width: W, height: y + rowH }
}

/**
 * run lays out a flat graph. Resolves to positions per node id, routed
 * points per edge id ([x0, y0, x1, y1, …]) and the size.
 */
export async function runLayout(
  key: string,
  nodes: LayoutNode[],
  edges: LayoutEdge[],
  dir: string,
  aspect: number,
  extra?: Record<string, string>,
): Promise<LayoutResult> {
  const hit = cache.get(key)
  if (hit) {
    layoutStats.hits++
    return hit
  }
  let res: LayoutResult
  const t0 = performance.now()
  const e = engine()
  if (!e || !nodes.length) res = grid(nodes, aspect)
  else {
    const ids = new Set(nodes.map((n) => n.id))
    const g: ElkNode = {
      id: 'root',
      layoutOptions: options(dir, aspect, nodes.length, extra),
      children: nodes.map((n) =>
        n.opts
          ? { id: n.id, width: n.width, height: n.height, layoutOptions: n.opts }
          : { id: n.id, width: n.width, height: n.height },
      ),
      edges: packed(nodes.length, extra)
        ? []
        : edges
            .filter((x) => x.s !== x.t && ids.has(x.s) && ids.has(x.t))
            .map((x) => ({ id: x.id, sources: [x.s], targets: [x.t] })),
    }
    try {
      const out = await e.layout(g)
      const pos: LayoutResult['pos'] = {},
        routes: LayoutResult['edges'] = {}
      ;(out.children ?? []).forEach((c) => {
        pos[c.id] = { x: c.x || 0, y: c.y || 0 }
      })
      ;(out.edges ?? []).forEach((ed) => {
        const s = ed.sections?.[0]
        if (!s) return
        const pts = [s.startPoint.x, s.startPoint.y]
        ;(s.bendPoints ?? []).forEach((b) => pts.push(b.x, b.y))
        pts.push(s.endPoint.x, s.endPoint.y)
        routes[ed.id] = pts
      })
      res = { pos, edges: routes, width: out.width || 0, height: out.height || 0 }
    } catch {
      layoutStats.failures++
      res = grid(nodes, aspect)
    }
  }
  const ms = performance.now() - t0
  layoutStats.runs++
  layoutStats.ms += ms
  layoutStats.last = ms
  if (cache.size > 600) cache.delete(cache.keys().next().value as string)
  cache.set(key, res)
  return res
}

/**
 * runPartitioned lays out a left-to-right flow whose columns are fixed:
 * node.c is the column (minCol the leftmost).
 */
export function runPartitioned(key: string, nodes: LayoutNode[], edges: LayoutEdge[], minCol: number, aspect: number) {
  nodes.forEach((n) => {
    n.opts = { 'elk.partitioning.partition': String((n.c ?? 0) - minCol) }
  })
  return runLayout(key, nodes, edges, 'RIGHT', aspect, {
    'elk.partitioning.activate': 'true',
    'elk.separateConnectedComponents': 'false',
    'elk.layered.spacing.nodeNodeBetweenLayers': '64',
    'elk.spacing.nodeNode': '14',
  })
}

