// Vocabulary of the dataset: element kinds, relation kinds, confidences and
// the architecture layers of Study Mode, with their labels and colours.

import type { Conf, GNode } from './types'

export const KIND: Record<string, { label: string; color: string; order: number }> = {
  repo: { label: 'repository', color: 'k-module', order: 0 },
  module: { label: 'module', color: 'k-module', order: 1 },
  dir: { label: 'directory', color: 'k-group', order: 2 },
  extgroup: { label: 'external dependencies', color: 'k-external', order: 90 },
  package: { label: 'package', color: 'k-package', order: 3 },
  file: { label: 'file', color: 'k-file', order: 4 },
  struct: { label: 'struct', color: 'k-type', order: 10 },
  interface: { label: 'interface', color: 'k-interface', order: 11 },
  type: { label: 'named type', color: 'k-type', order: 12 },
  func: { label: 'function', color: 'k-func', order: 13 },
  method: { label: 'method', color: 'k-func', order: 14 },
  closure: { label: 'closure', color: 'k-func', order: 15 },
  route: { label: 'HTTP route', color: 'k-route', order: 16 },
  db: { label: 'database operation', color: 'k-db', order: 17 },
  external: { label: 'external function', color: 'k-external', order: 18 },
  unresolved: { label: 'unresolved call', color: 'k-unresolved', order: 19 },
}

export const REL: Record<string, { label: string; inv: string; order: number }> = {
  imports: { label: 'imports', inv: 'imported by', order: 0 },
  calls: { label: 'calls', inv: 'called by', order: 1 },
  go: { label: 'starts goroutine', inv: 'goroutine started by', order: 2 },
  defer: { label: 'defers', inv: 'deferred by', order: 3 },
  handles: { label: 'handled by', inv: 'handles', order: 4 },
  queries: { label: 'queries', inv: 'queried by', order: 5 },
  implements: { label: 'implements', inv: 'implemented by', order: 6 },
  embeds: { label: 'embeds', inv: 'embedded by', order: 7 },
  references: { label: 'uses type', inv: 'type used by', order: 8 },
  method: { label: 'has method', inv: 'method of', order: 9 },
  contains: { label: 'contains', inv: 'contained in', order: 10 },
}

export const relLabel = (type: string) => REL[type]?.label ?? type

/** kindPlural: "3 directories", "2 HTTP routes". */
export function kindPlural(kind: string, n: number): string {
  const l = KIND[kind]?.label ?? kind
  if (n === 1 || l.endsWith('dependencies')) return l
  return l.endsWith('y') ? l.slice(0, -1) + 'ies' : l + 's'
}

/** cssVar turns a token name into a CSS value ("k-func" → var(--k-func)). */
export const cssVar = (token: string) => 'var(--' + token + ')'

export const CONFS: Conf[] = ['confirmed', 'likely', 'possible', 'unresolved']
export const CONF_RANK: Record<string, number> = { confirmed: 4, likely: 3, possible: 2, unresolved: 1 }
export const CONF_HELP: Record<string, string> = {
  confirmed: 'confirmed: a static call or a relation read from the types',
  likely: 'likely: the only implementation of an interface method in the analysed code',
  possible: 'possible: one of several implementations, or a function passed as a value',
  unresolved: 'unresolved: no static target (call through a function value or reflection)',
}
export const rank = (c: string) => CONF_RANK[c] ?? 0

export const FUNC: Record<string, 1> = { func: 1, method: 1, closure: 1 }
export const TYPE: Record<string, 1> = { struct: 1, interface: 1, type: 1 }
export const SYMBOL: Record<string, 1> = {
  struct: 1,
  interface: 1,
  type: 1,
  func: 1,
  method: 1,
  closure: 1,
  route: 1,
  db: 1,
  unresolved: 1,
  external: 1,
}
export const FLOW: Record<string, 1> = { calls: 1, go: 1, defer: 1, handles: 1, queries: 1 }
/** Structural relations are drawn as nesting, not as lines. */
export const STRUCTURAL: Record<string, 1> = { contains: 1, method: 1 }

/** Short relation names used in line labels ("3 calls · 1 type ref"). */
export const SHORT: Record<string, string> = {
  imports: 'import',
  calls: 'call',
  go: 'goroutine',
  defer: 'defer',
  handles: 'handler',
  queries: 'query',
  implements: 'implements',
  embeds: 'embeds',
  references: 'type ref',
}

/** Element kinds and relation kinds the filters offer, in order. */
export const FILTER_KINDS = ['struct', 'interface', 'type', 'func', 'method', 'closure', 'route', 'db', 'unresolved']
export const FILTER_RELS = [
  'imports',
  'calls',
  'go',
  'defer',
  'handles',
  'queries',
  'implements',
  'embeds',
  'references',
]

// ---------- architecture layers (Study Mode's classification) ----------

export const LAYER_COLOR: Record<string, string> = {
  Route: 'l-route',
  Middleware: 'l-middleware',
  Handler: 'l-handler',
  Service: 'l-service',
  Repository: 'l-repository',
  Database: 'l-database',
  'External API': 'l-external',
  Entrypoint: 'k-group',
  Config: 'k-group',
  Model: 'k-type',
  External: 'k-external',
  Unresolved: 'k-unresolved',
}

/** layerOf: the architecture layer of a node ("" when unclassified). */
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

// ---------- tokens ----------

/** Utility classes of the confidence colours (Tailwind needs them spelled out). */
export const CONF_TEXT: Record<string, string> = {
  confirmed: 'text-c-confirmed',
  likely: 'text-c-likely',
  possible: 'text-c-possible',
  unresolved: 'text-c-unresolved',
}
export const CONF_BG: Record<string, string> = {
  confirmed: 'bg-c-confirmed',
  likely: 'bg-c-likely',
  possible: 'bg-c-possible',
  unresolved: 'bg-c-unresolved',
}
export const METHOD_CLASS: Record<string, string> = {
  GET: 'text-m-get bg-m-get/15',
  POST: 'text-m-post bg-m-post/15',
  PUT: 'text-m-put bg-m-put/15',
  PATCH: 'text-m-patch bg-m-patch/15',
  DELETE: 'text-m-delete bg-m-delete/15',
  HEAD: 'text-m-other bg-m-other/15',
  OPTIONS: 'text-m-other bg-m-other/15',
}
export const methodClass = (m: string) => METHOD_CLASS[m] ?? 'text-m-any bg-m-any/15'

export const SEV_CLASS: Record<string, string> = {
  critical: 'text-sev-critical border-sev-critical/50 bg-sev-critical/10',
  high: 'text-sev-high border-sev-high/50 bg-sev-high/10',
  medium: 'text-sev-medium border-sev-medium/50 bg-sev-medium/10',
  low: 'text-sev-low border-sev-low/50 bg-sev-low/10',
  info: 'text-sev-info border-sev-info/50 bg-sev-info/10',
}
