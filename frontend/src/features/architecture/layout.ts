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

/**
 * How one container is laid out (layoutDoc.ts names them for the viewer):
 *   grouped  layered top-down, the components packed to the window (default)
 *   down     one flow of dependency layers, top to bottom
 *   right    one flow of dependency layers, left to right
 *   force    force directed (seeded: the same input gives the same result)
 * Hierarchical runs once over the whole drawing (runNested).
 */
export type ContainerAlgo = 'grouped' | 'down' | 'right' | 'force'

/** algoOptions: the ELK options of an algorithm, given to runLayout as extra options. */
export function algoOptions(algo: ContainerAlgo): Record<string, string> {
  switch (algo) {
    case 'down':
    case 'right':
      return {
        'elk.direction': algo === 'down' ? 'DOWN' : 'RIGHT',
        'elk.separateConnectedComponents': 'false',
        'elk.layered.considerModelOrder.strategy': 'NONE',
        'elk.layered.spacing.nodeNodeBetweenLayers': algo === 'down' ? '54' : '64',
      }
    case 'force':
      return {
        'elk.algorithm': 'force',
        'elk.force.model': 'EADES',
        'elk.force.repulsion': '1',
        'elk.randomSeed': '7',
        'elk.spacing.nodeNode': '20',
      }
    default:
      return {}
  }
}

/** routes: the algorithm routes its edges (orthogonally), so they can be drawn as they are. */
export const routesEdges = (algo: ContainerAlgo, n: number) => algo !== 'force' && n <= BIG

function options(dir: string, aspect: number, n: number, extra?: Record<string, string>): Record<string, string> {
  if (extra && extra['elk.algorithm'] === 'force' && !packed(n, extra))
    return Object.assign({ 'elk.padding': '[top=0,left=0,bottom=0,right=0]', 'elk.aspectRatio': String(aspect) }, extra)
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
export function runPartitioned(
  key: string,
  nodes: LayoutNode[],
  edges: LayoutEdge[],
  minCol: number,
  aspect: number,
  dir: 'RIGHT' | 'DOWN' = 'RIGHT',
) {
  nodes.forEach((n) => {
    n.opts = { 'elk.partitioning.partition': String((n.c ?? 0) - minCol) }
  })
  return runLayout(key, nodes, edges, dir, aspect, {
    'elk.partitioning.activate': 'true',
    'elk.separateConnectedComponents': 'false',
    'elk.layered.spacing.nodeNodeBetweenLayers': '64',
    'elk.spacing.nodeNode': '14',
  })
}


/** A node of a nested layout: a leaf (size) or a container (children). */
export interface NestedNode {
  id: string
  width?: number
  height?: number
  children?: NestedNode[]
}

export interface NestedResult {
  /** Position of each node relative to its container's top-left (padding included). */
  pos: Record<string, { x: number; y: number }>
  /** Size of each container as laid out. */
  size: Record<string, { w: number; h: number }>
}

/**
 * runNested lays out a whole nested drawing in one run (ELK layered with
 * hierarchy handling): the layers follow the relations across containers.
 * pad is the room inside a container (its header on top). Resolves to null
 * when ELK is missing or fails.
 */
export async function runNested(
  key: string,
  top: NestedNode[],
  edges: LayoutEdge[],
  pad: { top: number; left: number; right: number; bottom: number },
): Promise<NestedResult | null> {
  const hit = nestedCache.get(key)
  if (hit) {
    layoutStats.hits++
    return hit
  }
  const e = engine()
  if (!e) return null
  const padding = '[top=' + pad.top + ',left=' + pad.left + ',bottom=' + pad.bottom + ',right=' + pad.right + ']'
  const ids = new Set<string>()
  const conv = (n: NestedNode): ElkNode => {
    ids.add(n.id)
    if (n.children)
      return {
        id: n.id,
        layoutOptions: { 'elk.padding': padding },
        children: n.children.map(conv),
      }
    return { id: n.id, width: n.width, height: n.height }
  }
  const g: ElkNode = {
    id: 'root',
    layoutOptions: {
      'elk.algorithm': 'layered',
      'elk.direction': 'DOWN',
      'elk.hierarchyHandling': 'INCLUDE_CHILDREN',
      'elk.edgeRouting': 'ORTHOGONAL',
      'elk.spacing.nodeNode': '22',
      'elk.layered.spacing.nodeNodeBetweenLayers': '46',
      'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES',
      'elk.padding': '[top=0,left=0,bottom=0,right=0]',
    },
    children: top.map(conv),
  }
  g.edges = edges
    .filter((x) => x.s !== x.t && ids.has(x.s) && ids.has(x.t))
    .map((x) => ({ id: x.id, sources: [x.s], targets: [x.t] }))
  const t0 = performance.now()
  try {
    const out = await e.layout(g)
    const res: NestedResult = { pos: {}, size: {} }
    const walk = (n: ElkNode) => {
      for (const c of n.children ?? []) {
        res.pos[c.id] = { x: c.x || 0, y: c.y || 0 }
        if (c.children && c.children.length) res.size[c.id] = { w: c.width || 0, h: c.height || 0 }
        walk(c)
      }
    }
    walk(out)
    layoutStats.runs++
    layoutStats.last = performance.now() - t0
    layoutStats.ms += layoutStats.last
    if (nestedCache.size > 40) nestedCache.delete(nestedCache.keys().next().value as string)
    nestedCache.set(key, res)
    return res
  } catch {
    layoutStats.failures++
    return null
  }
}

const nestedCache = new Map<string, NestedResult>()
