// The compact dataset decoded once per section object: nodes and edges with
// their adjacency, the ID index and the HTTP catalog tied to its route
// nodes. Shared by the HTTP and Security views (the security page carries
// the same `graph` section).

import { baseName } from '../../lib/format'
import type { GEdge, GNode, HttpCatalog, Meta, RawGraph, Route, Shape } from './types'

export interface Graph {
  raw: RawGraph
  N: GNode[]
  E: GEdge[]
  byId: Map<string, number>
  http: HttpCatalog | null
  routes: Route[]
  defs: Record<string, Shape>
  /** route index → node index (-1 when the route has no node) */
  nodeOfRoute: number[]
  /** node index → route index */
  routeOfNode: Map<number, number>
  routeById: Map<string, number>
  modules: string[]
}

const cache = new WeakMap<object, Graph>()

export function decodeGraph(raw: RawGraph): Graph {
  const hit = cache.get(raw)
  if (hit) return hit
  const K = raw.kinds ?? [],
    ET = raw.etypes ?? [],
    CF = raw.confs ?? [],
    ES = raw.esrc ?? []
  const N: GNode[] = (raw.nodes ?? []).map((a, i) => ({
    i,
    id: String(a[0]),
    type: K[a[1] as number] ?? '',
    name: String(a[2] ?? ''),
    qual: String(a[3] ?? ''),
    pkg: String(a[4] ?? ''),
    file: String(a[5] ?? ''),
    line: Number(a[6] ?? 0),
    end: Number(a[7] ?? 0),
    sig: String(a[8] ?? ''),
    ext: !!a[9],
    parent: Number(a[10] ?? -1),
    group: Number(a[11] ?? -1),
    comm: Number(a[12] ?? -1),
    deg: Number(a[13] ?? 0),
    meta: ((a[14] as Meta | null) ?? {}) as Meta,
    out: [],
    inn: [],
  }))
  const E: GEdge[] = (raw.edges ?? []).map((a, i) => {
    const e: GEdge = {
      i,
      s: a[0] as number,
      t: a[1] as number,
      type: ET[a[2] as number] ?? '',
      conf: CF[a[3] as number] ?? 'unresolved',
      w: Number(a[4] || 1),
      file: String(a[5] ?? ''),
      line: Number(a[6] ?? 0),
      note: String(a[7] ?? ''),
      meta: ((a[8] as Meta | null) ?? {}) as Meta,
      src: ES[a[9] as number] ?? '',
    }
    N[e.s]?.out.push(i)
    N[e.t]?.inn.push(i)
    return e
  })
  const byId = new Map<string, number>()
  N.forEach((n) => byId.set(n.id, n.i))
  const http = raw.http ?? null
  const routes = http?.routes ?? []
  const routeOfNode = new Map<number, number>()
  const routeById = new Map<string, number>()
  const nodeOfRoute = routes.map((r, k) => {
    routeById.set(r.id, k)
    const i = byId.get(r.id) ?? -1
    if (i >= 0) routeOfNode.set(i, k)
    return i
  })
  const g: Graph = {
    raw,
    N,
    E,
    byId,
    http,
    routes,
    defs: http?.schemas ?? {},
    nodeOfRoute,
    routeOfNode,
    routeById,
    modules: [...(raw.modules ?? [])].sort(),
  }
  cache.set(raw, g)
  return g
}

// ---------- vocabulary ----------

export const KIND: Record<string, { label: string; color: string }> = {
  module: { label: 'module', color: 'k-module' },
  package: { label: 'package', color: 'k-package' },
  file: { label: 'file', color: 'k-file' },
  struct: { label: 'struct', color: 'k-type' },
  interface: { label: 'interface', color: 'k-interface' },
  type: { label: 'named type', color: 'k-type' },
  func: { label: 'function', color: 'k-func' },
  method: { label: 'method', color: 'k-func' },
  closure: { label: 'closure', color: 'k-func' },
  route: { label: 'HTTP route', color: 'k-route' },
  db: { label: 'database operation', color: 'k-db' },
  external: { label: 'external function', color: 'k-external' },
  unresolved: { label: 'unresolved call', color: 'k-unresolved' },
}

export const REL: Record<string, { label: string; inv: string; order: number; short: string }> = {
  imports: { label: 'imports', inv: 'imported by', order: 0, short: 'import' },
  calls: { label: 'calls', inv: 'called by', order: 1, short: 'call' },
  go: { label: 'starts goroutine', inv: 'goroutine started by', order: 2, short: 'goroutine' },
  defer: { label: 'defers', inv: 'deferred by', order: 3, short: 'defer' },
  handles: { label: 'handled by', inv: 'handles', order: 4, short: 'handler' },
  queries: { label: 'queries', inv: 'queried by', order: 5, short: 'query' },
  implements: { label: 'implements', inv: 'implemented by', order: 6, short: 'implements' },
  embeds: { label: 'embeds', inv: 'embedded by', order: 7, short: 'embeds' },
  references: { label: 'uses type', inv: 'type used by', order: 8, short: 'type ref' },
  method: { label: 'has method', inv: 'method of', order: 9, short: 'method' },
  contains: { label: 'contains', inv: 'contained in', order: 10, short: 'contains' },
}

export const CONFS = ['confirmed', 'likely', 'possible', 'unresolved'] as const
export const CONF_RANK: Record<string, number> = { confirmed: 4, likely: 3, possible: 2, unresolved: 1 }
export const CONF_HELP: Record<string, string> = {
  confirmed: 'confirmed: a static call or a relation read from the types',
  likely: 'likely: the only implementation of an interface method in the analysed code',
  possible: 'possible: one of several implementations, or a function passed as a value',
  unresolved: 'unresolved: no static target (call through a function value or reflection)',
}
export const FLOW: Record<string, true> = { calls: true, go: true, defer: true, handles: true, queries: true }
export const STRUCTURAL: Record<string, true> = { contains: true, method: true }
export const FUNC: Record<string, true> = { func: true, method: true, closure: true }
export const TYPE: Record<string, true> = { struct: true, interface: true, type: true }

export const kindLabel = (n: GNode) => KIND[n.type]?.label ?? n.type

export function kindColor(n: GNode): string {
  if (n.ext && n.type === 'package') return 'k-external'
  return KIND[n.type]?.color ?? 'k-group'
}

/** displayName: the name a node is shown with (a package by its path). */
export function displayName(n: GNode): string {
  if (n.type === 'package') return n.ext ? n.name : baseName(n.name) || n.name
  return n.name
}

/** reference: the fully qualified Go name of a node. */
export function reference(n: GNode): string {
  if (n.type === 'package' || n.type === 'module') return n.name
  if (n.type === 'file') return n.file
  if (n.type === 'route') return (n.meta.method || 'ANY') + ' ' + (n.meta.path || n.name)
  if (n.type === 'db' || n.type === 'unresolved') return n.file ? n.file + (n.line ? ':' + n.line : '') : n.name
  if (n.pkg && n.qual) {
    const i = n.qual.indexOf('.')
    return i >= 0 ? n.pkg + n.qual.slice(i) : n.pkg + '.' + n.qual
  }
  return n.qual || n.name
}

/** moduleOf returns the module (path) declaring an import path. */
export function moduleOf(g: Graph, pkg: string): string {
  let best = ''
  for (const m of g.modules) if ((pkg === m || pkg.startsWith(m + '/')) && m.length > best.length) best = m
  return best
}

/** relPackage shows an import path relative to its module. */
export function relPackage(g: Graph, pkg: string): string {
  const m = moduleOf(g, pkg)
  if (!m) return pkg
  if (pkg === m) return baseName(m) || m
  return pkg.slice(m.length + 1)
}

// ---------- architecture layers ----------

export const LAYERS: [string, string][] = [
  ['Route', 'l-route'],
  ['Middleware', 'l-middleware'],
  ['Handler', 'l-handler'],
  ['Service', 'l-service'],
  ['Repository', 'l-repository'],
  ['Database', 'l-database'],
  ['External API', 'l-external'],
  ['Entrypoint', 'k-group'],
  ['Config', 'k-group'],
  ['Model', 'k-type'],
  ['External', 'k-external'],
  ['Unresolved', 'k-unresolved'],
]
export const LAYER_COLOR: Record<string, string> = Object.fromEntries(LAYERS)
/** The canonical path the layer strip always shows; absent steps are marked. */
export const SPINE = ['Middleware', 'Handler', 'Service', 'Repository', 'Database']

/** layerOf: the architecture layer of a node (Study Mode's classification). */
export function layerOf(n: GNode): string {
  if (n.type === 'route') return 'Route'
  if (n.type === 'db') return 'Database'
  if (n.type === 'unresolved') return 'Unresolved'
  if (n.ext) return 'External'
  if (n.meta.role === 'middleware') return 'Middleware'
  if (n.meta.layer && n.meta.layer !== 'HTTP Route') return n.meta.layer
  if (n.meta.role === 'http handler') return 'Handler'
  return ''
}

export const layerColor = (n: GNode) => LAYER_COLOR[layerOf(n)] ?? kindColor(n)

/** loc of a node: file:line, or the package of an external one. */
export function nodeLoc(n: GNode): string {
  return n.file ? n.file + (n.line ? ':' + n.line : '') : n.ext ? n.pkg : ''
}

/** routesOfNode: the routes whose chain registers this node (handler or middleware). */
export function handlerIndex(g: Graph, r: Route): number {
  return r.handler?.id ? (g.byId.get(r.handler.id) ?? -1) : -1
}
