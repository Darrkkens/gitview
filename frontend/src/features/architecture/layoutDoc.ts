// The layout document: where the viewer put the cards of the Architecture
// view, kept apart from the analysis (the model never changes) and from the
// automatic layout (recomputed on every rebuild).
//
//   {
//     "layout_version": 1,
//     "repository": "gitview",
//     "views": {
//       "repository": {                       // the exploration it belongs to
//         "algorithm": "grouped",
//         "edges": "orthogonal",
//         "expanded": ["module:…", "dir:…"],
//         "nodes": { "package:…": { "x": 320, "y": 180, "locked": true } }
//       }
//     }
//   }
//
// View keys: "repository" (the whole repository), "scope:<id>" (a module,
// directory or package opened as the scope), "focus:<id>" (focus mode).
// Node keys are the stable IDs of the tree items (graph node IDs such as
// "package:…", "file:…", "func:…", and the synthetic "repo:<name>",
// "dir:<prefix>:<path>", "ext:", "ext:std", "ext:third").
//
// Coordinates: x and y are the card's top-left corner in canvas units. A
// card drawn inside an expanded group is placed relative to that group's
// origin (the group's top-left as it was laid out), so moving a group moves
// everything inside it without rewriting their positions; a card at the top
// level of the view (the scope, the elements around it, every card of focus
// mode) has absolute coordinates.
//
// The document is data: it is parsed and validated, never executed. Unknown
// IDs are kept (they may belong to another expansion or a later analysis)
// but never drawn.

export const LAYOUT_VERSION = 1

export const ALGORITHMS = ['grouped', 'hierarchical', 'down', 'right', 'force', 'manual'] as const
export type Algorithm = (typeof ALGORITHMS)[number]

export const EDGE_STYLES = ['orthogonal', 'smooth', 'straight'] as const
export type EdgeStyle = (typeof EDGE_STYLES)[number]

export const ALGORITHM_INFO: Record<Algorithm, { label: string; help: string }> = {
  grouped: {
    label: 'Grouped',
    help: 'Nested by module, directory and package; each group lays out its own content, packed to the window',
  },
  hierarchical: {
    label: 'Hierarchical',
    help: 'Dependency layers across the whole drawing: a card sits above what it depends on, wherever it is nested',
  },
  down: { label: 'Top to bottom', help: 'Each group as one flow of dependency layers, from top to bottom' },
  right: { label: 'Left to right', help: 'Each group as one flow of dependency layers, from left to right' },
  force: { label: 'Force directed', help: 'Related cards attract, the others repel (deterministic force layout)' },
  manual: {
    label: 'Manual',
    help: 'Nothing is rearranged: every card stays where it is, new cards are placed in free space',
  },
}

export const EDGE_INFO: Record<EdgeStyle, { label: string; help: string }> = {
  orthogonal: { label: 'Orthogonal', help: 'Horizontal and vertical segments' },
  smooth: { label: 'Curved', help: 'Smooth curves' },
  straight: { label: 'Straight', help: 'Straight lines between the cards' },
}

export interface NodePos {
  x: number
  y: number
  locked?: boolean
}

export interface ViewLayout {
  algorithm: Algorithm
  edges: EdgeStyle
  expanded?: string[]
  nodes: Record<string, NodePos>
}

export interface LayoutDoc {
  layout_version: number
  repository: string
  views: Record<string, ViewLayout>
}

export const DEFAULT_ALGORITHM: Algorithm = 'grouped'
export const DEFAULT_EDGES: EdgeStyle = 'orthogonal'

/** Limits that keep a hostile or broken document harmless. */
const MAX_VIEWS = 2000
const MAX_NODES = 200000
const MAX_ID = 2000
const MAX_COORD = 1e7

export function emptyDoc(repository: string): LayoutDoc {
  return { layout_version: LAYOUT_VERSION, repository, views: {} }
}

export function emptyView(): ViewLayout {
  return { algorithm: DEFAULT_ALGORITHM, edges: DEFAULT_EDGES, nodes: {} }
}

/** isEmptyView: nothing in it differs from the automatic layout. */
export function isEmptyView(v: ViewLayout | undefined): boolean {
  return (
    !v ||
    (v.algorithm === DEFAULT_ALGORITHM &&
      v.edges === DEFAULT_EDGES &&
      !(v.expanded && v.expanded.length) &&
      !Object.keys(v.nodes).length)
  )
}

export function cloneView(v: ViewLayout): ViewLayout {
  const nodes: Record<string, NodePos> = {}
  for (const k in v.nodes) {
    const p = v.nodes[k]
    nodes[k] = p.locked ? { x: p.x, y: p.y, locked: true } : { x: p.x, y: p.y }
  }
  const out: ViewLayout = { algorithm: v.algorithm, edges: v.edges, nodes }
  if (v.expanded) out.expanded = v.expanded.slice()
  return out
}

export function cloneDoc(d: LayoutDoc): LayoutDoc {
  const views: Record<string, ViewLayout> = {}
  for (const k in d.views) views[k] = cloneView(d.views[k])
  return { layout_version: d.layout_version, repository: d.repository, views }
}

export interface ParseResult {
  doc: LayoutDoc
  /** The input was a single view ({"view": …, "nodes": …}). */
  single: string | null
  positions: number
  unknown: number
  warnings: string[]
}

export class LayoutError extends Error {}

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
// "__proto__" would set an object's prototype instead of a key: never a valid ID
const validKey = (k: string) => k.length > 0 && k.length <= MAX_ID && k !== '__proto__'

function num(v: unknown): number | null {
  if (typeof v !== 'number' || !Number.isFinite(v)) return null
  return Math.max(-MAX_COORD, Math.min(MAX_COORD, Math.round(v * 10) / 10))
}

function parseView(
  raw: Record<string, unknown>,
  known: ((id: string) => boolean) | undefined,
  stats: { positions: number; unknown: number; warnings: string[] },
  name: string,
): ViewLayout {
  const v = emptyView()
  if (raw.algorithm !== undefined) {
    if ((ALGORITHMS as readonly unknown[]).includes(raw.algorithm)) v.algorithm = raw.algorithm as Algorithm
    else stats.warnings.push(name + ': unknown algorithm ' + JSON.stringify(raw.algorithm) + ', using grouped')
  }
  if (raw.edges !== undefined) {
    if ((EDGE_STYLES as readonly unknown[]).includes(raw.edges)) v.edges = raw.edges as EdgeStyle
    else stats.warnings.push(name + ': unknown edge style ' + JSON.stringify(raw.edges) + ', using orthogonal')
  }
  if (Array.isArray(raw.expanded)) {
    const list = raw.expanded.filter((x): x is string => typeof x === 'string' && validKey(x))
    if (list.length) v.expanded = Array.from(new Set(list)).slice(0, MAX_NODES)
  }
  const nodes = raw.nodes
  if (nodes !== undefined && !isObj(nodes)) throw new LayoutError(name + ': "nodes" must be an object')
  if (isObj(nodes)) {
    let bad = 0
    for (const id of Object.keys(nodes)) {
      if (stats.positions >= MAX_NODES) {
        stats.warnings.push('more than ' + MAX_NODES + ' positions: the rest is ignored')
        break
      }
      const p = nodes[id]
      if (!validKey(id) || !isObj(p)) {
        bad++
        continue
      }
      const x = num(p.x),
        y = num(p.y)
      if (x === null || y === null) {
        bad++
        continue
      }
      if (known && !known(id)) stats.unknown++
      v.nodes[id] = p.locked === true ? { x, y, locked: true } : { x, y }
      stats.positions++
    }
    if (bad) stats.warnings.push(name + ': ' + bad + ' invalid position' + (bad > 1 ? 's' : '') + ' ignored')
  }
  return v
}

/**
 * parseLayout validates a layout document (text or parsed JSON). It accepts
 * the whole document and the shape of a single view
 * ({"layout_version": 1, "view": "repository", "nodes": {…}}). known
 * tells which IDs exist in this dataset: the others are counted (and kept).
 * Throws LayoutError when the input is not a layout document.
 */
export function parseLayout(input: unknown, known?: (id: string) => boolean): ParseResult {
  let data = input
  if (typeof input === 'string') {
    try {
      data = JSON.parse(input)
    } catch (e) {
      throw new LayoutError('not JSON: ' + (e as Error).message)
    }
  }
  if (!isObj(data)) throw new LayoutError('a layout document is a JSON object')
  const ver = data.layout_version
  if (ver === undefined) throw new LayoutError('not a layout document: no "layout_version"')
  if (ver !== LAYOUT_VERSION)
    throw new LayoutError(
      'unsupported layout_version ' + JSON.stringify(ver) + ' (this page reads version ' + LAYOUT_VERSION + ')',
    )
  const stats = { positions: 0, unknown: 0, warnings: [] as string[] }
  const repository = typeof data.repository === 'string' ? data.repository.slice(0, 500) : ''
  const doc: LayoutDoc = { layout_version: LAYOUT_VERSION, repository, views: {} }
  let single: string | null = null
  if (data.views !== undefined) {
    if (!isObj(data.views)) throw new LayoutError('"views" must be an object')
    const keys = Object.keys(data.views)
    if (keys.length > MAX_VIEWS) stats.warnings.push('more than ' + MAX_VIEWS + ' views: the rest is ignored')
    for (const k of keys.slice(0, MAX_VIEWS)) {
      const v = data.views[k]
      if (!validKey(k) || !isObj(v)) {
        stats.warnings.push('view ' + JSON.stringify(k.slice(0, 80)) + ' ignored (not an object)')
        continue
      }
      doc.views[k] = parseView(v, known, stats, k)
    }
  } else if (data.view !== undefined || data.nodes !== undefined) {
    single = typeof data.view === 'string' && validKey(data.view) ? data.view : 'repository'
    doc.views[single] = parseView(data, known, stats, single)
  } else throw new LayoutError('a layout document has "views" (or "view" and "nodes")')
  return { doc, single, positions: stats.positions, unknown: stats.unknown, warnings: stats.warnings }
}

/** serializeLayout writes the document with a stable order (diff-friendly). */
export function serializeLayout(d: LayoutDoc, pretty = true): string {
  const views: Record<string, unknown> = {}
  for (const k of Object.keys(d.views).sort()) {
    const v = d.views[k]
    if (isEmptyView(v)) continue
    const nodes: Record<string, NodePos> = {}
    for (const id of Object.keys(v.nodes).sort()) {
      const p = v.nodes[id]
      nodes[id] = p.locked
        ? { x: Math.round(p.x), y: Math.round(p.y), locked: true }
        : { x: Math.round(p.x), y: Math.round(p.y) }
    }
    const out: Record<string, unknown> = { algorithm: v.algorithm, edges: v.edges }
    if (v.expanded && v.expanded.length) out.expanded = v.expanded.slice().sort()
    out.nodes = nodes
    views[k] = out
  }
  const doc = { layout_version: LAYOUT_VERSION, repository: d.repository, views }
  return pretty ? JSON.stringify(doc, null, 2) + '\n' : JSON.stringify(doc)
}

/** viewKey names the exploration a layout belongs to. */
export function viewKey(rootId: string | null, focusId: string | null): string {
  if (focusId) return 'focus:' + focusId
  if (rootId) return 'scope:' + rootId
  return 'repository'
}
