// Types of what the HTTP view reads: the `graph` section (the compact
// dataset written by internal/graph/export/htmldata.go) and its HTTP catalog
// (internal/graph/model/http.go, slimmed by slimHTTP).

/** Evidence as embedded by the page (at most 3 per element). */
export interface PageEvidence {
  file: string
  line: number
  details?: string
}

/** Full evidence entry (servers keep the model's form). */
export interface Evidence {
  kind?: string
  basis?: string
  file: string
  line: number
  source?: string
  details?: string
}

export interface Rule {
  name: string
  param?: string
  source: string // binding | validate
  recognized?: boolean
}

export interface Field {
  name: string
  goName?: string
  schema?: Shape
  omitEmpty?: boolean
  rules?: Rule[]
  embedded?: string
  conditional?: boolean
  file?: string
  line?: number
}

export interface Shape {
  type: string
  format?: string
  goType?: string
  ref?: string
  name?: string
  fields?: Field[]
  items?: Shape
  values?: Shape
  nullable?: boolean
  enum?: string[]
  partial?: boolean
  note?: string
  file?: string
  line?: number
}

export interface ChainEntry {
  id?: string
  name: string
  file?: string
  line?: number
  confidence: string
  scope?: string
  addedFile?: string
  addedLine?: number
  note?: string
}

export interface Param {
  name: string
  in: string // path | query | header | cookie | form
  required: string // yes | no | unknown
  schema?: Shape
  default?: string
  source: string
  from?: string
  inPattern?: boolean
  confidence: string
  evidence?: PageEvidence[]
  note?: string
}

export interface Body {
  contentTypes?: string[]
  schema?: Shape
  source: string
  enforces?: string[]
  disallowUnknown?: boolean
  confidence: string
  evidence?: PageEvidence[]
  notes?: string[]
}

export interface Response {
  status: number
  statusText?: string
  implicit?: boolean
  contentType?: string
  schema?: Shape
  source: string
  from: string
  fromId?: string
  via?: string[]
  basis: string // static | partial | unknown | observed
  example?: string
  confidence: string
  evidence?: PageEvidence[]
  evidenceTotal?: number
  note?: string
}

export interface Auth {
  recognized: boolean
  scheme?: string
  credential?: string
  by?: string
  evidence?: PageEvidence[]
  note?: string
}

export interface Example {
  body?: string
  curl: string
  baseUrl: string
  baseUrlSource: string
  valid: string // compatible | unverified
  notes?: string[]
}

export interface Annotation {
  kind: string
  severity?: string
  title: string
  details?: string
}

export interface Route {
  id: string
  key: string
  method: string
  path: string
  template: string
  framework: string
  file: string
  line: number
  group: string
  handler?: ChainEntry
  middlewares?: ChainEntry[]
  params?: Param[]
  body?: Body
  responses?: Response[]
  auth?: Auth
  example?: Example
  status: string // complete | partial | unknown
  reasons?: string[]
  unresolvedCalls: number
  annotations?: Annotation[]
}

export interface Server {
  addr: string
  url?: string
  dynamic?: boolean
  evidence: Evidence
}

export interface HttpCatalog {
  routes: Route[] | null
  schemas?: Record<string, Shape>
  servers?: Server[]
  notes?: string[]
}

export interface Completeness {
  status: string
  reasons?: string[]
}

export interface SourcesInfo {
  bytes?: number
  disabled?: boolean
  embedded?: boolean
  files?: number
  skipped?: string[] | null
}

export interface GraphStats {
  modules: number
  packages: number
  externalPackages: number
  files: number
  functions: number
  types: number
  nodes: number
  edges: number
  routes: number
}

/** The `graph` section: the compact dataset. */
export interface RawGraph {
  schema: string
  repository: string
  version?: string
  generated: string
  modules: string[] | null
  stats: GraphStats
  completeness?: Completeness
  method?: string[]
  kinds: string[]
  etypes: string[]
  confs: string[]
  esrc: string[]
  communities: unknown[][]
  /** [id, kind, name, qualified, pkg, file, line, end, sig, ext, parent, group, community, degree, meta] */
  nodes: unknown[][]
  /** [source, target, type, confidence, weight, file, line, note, meta, evidenceSource] */
  edges: unknown[][]
  http?: HttpCatalog | null
  sources?: SourcesInfo
}

export type Meta = Record<string, unknown> & {
  role?: string
  layer?: string
  layerBasis?: string
  layerEvidence?: string
  sql?: string
  sqlDynamic?: boolean
  lib?: string
  op?: string
  method?: string
  path?: string
  router?: string
  partial?: boolean
  receiver?: string
  pointerReceiver?: boolean
  std?: boolean
  note?: string
  exported?: boolean
  flowId?: string
  order?: number
  annotations?: Annotation[]
}

/** A decoded node. */
export interface GNode {
  i: number
  id: string
  type: string
  name: string
  qual: string
  pkg: string
  file: string
  line: number
  end: number
  sig: string
  ext: boolean
  parent: number
  group: number
  comm: number
  deg: number
  meta: Meta
  out: number[]
  inn: number[]
}

/** A decoded edge. */
export interface GEdge {
  i: number
  s: number
  t: number
  type: string
  conf: string
  w: number
  file: string
  line: number
  note: string
  meta: Meta
  src: string
}
