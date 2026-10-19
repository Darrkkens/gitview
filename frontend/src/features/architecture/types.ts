// Types of the Architecture view: the compact dataset of the `graph` section
// (internal/graph/export/htmldata.go), the model decoded from it, and the
// scenes the canvas draws.

import type { Algorithm, EdgeStyle } from './layoutDoc'
import type { Port } from './route'

// ---------- the `graph` section, as written by the Go side ----------

export type Conf = 'confirmed' | 'likely' | 'possible' | 'unresolved'

/** A finding or another analysis' result attached to a node or an edge. */
export interface Annotation {
  kind: string // "security:<rule id>" for the audit's findings
  ref?: string // the finding ID (pages exported before it existed: in details)
  severity?: string
  title: string
  details?: string // the audit writes "<verdict> · <finding id> · <message>"
  evidence?: { file?: string; line?: number; details?: string }[]
}

/** Node metadata: free-form, with the keys the analysers are known to write. */
export interface NodeMeta {
  lib?: string
  op?: string
  sql?: string
  sqlDynamic?: boolean
  router?: string
  method?: string
  path?: string
  partial?: boolean
  std?: boolean
  layer?: string
  layerBasis?: string
  layerEvidence?: string
  role?: string
  receiver?: string
  pointerReceiver?: boolean
  exported?: boolean
  note?: string
  flowId?: string
  annotations?: Annotation[]
  [key: string]: unknown
}

/**
 * A node: [id, kind, name, qualified, package, file, line, endLine,
 * signature, external (0|1), parent, group, community, degree, meta].
 * kind indexes `kinds`; parent and group index `nodes` (-1: none).
 */
export type NodeTuple = [
  string,
  number,
  string,
  string,
  string,
  string,
  number,
  number,
  string,
  number,
  number,
  number,
  number,
  number,
  NodeMeta | null,
]

/**
 * An edge: [source, target, type, confidence, weight, file, line, note,
 * meta, evidence source]; source and target index `nodes`, type `etypes`,
 * confidence `confs`, evidence source `esrc`.
 */
export type EdgeTuple = [
  number,
  number,
  number,
  number,
  number,
  string,
  number,
  string,
  Record<string, unknown> | null,
  number,
]

/** A community: [id, level, label, size, members]. */
export type CommunityTuple = [number, string, string, number, number]

export interface GraphStats {
  modules: number
  packages: number
  externalPackages: number
  files: number
  functions: number
  types: number
  nodes: number
  edges: number
  communities: number
  routes: number
  modularity?: number
  nodesByType?: Record<string, number>
  edgesByType?: Record<string, number>
  edgesByConfidence?: Record<string, number>
}

export interface Completeness {
  status: string // complete | partial | …
  reasons?: string[] | null
}

/** What the page embeds of the repository's Go files. */
export interface SourcesInfo {
  embedded: boolean
  files: number
  bytes: number
  skipped?: string[] | null // "path: reason"
  disabled: boolean
}

/** The parts of an HTTP route the Architecture view uses (the HTTP view owns the rest). */
export interface RouteRef {
  id: string
  key: string
  method: string
  path: string
  framework?: string
  file: string
  line: number
  group?: string
  status?: string
  handler?: { id?: string; name: string; file?: string; line?: number; confidence?: string } | null
  auth?: { recognized?: boolean; scheme?: string } | null
}

export interface GraphSection {
  schema: string
  repository: string
  version?: string
  generated: string
  modules: string[] | null
  stats: GraphStats
  completeness?: Completeness | null
  method?: string[] | null
  initial?: { level?: string; maxNodes?: number; maxEdges?: number } | null
  kinds: string[]
  etypes: string[]
  confs: string[]
  esrc: string[]
  communities: CommunityTuple[] | null
  nodes: NodeTuple[]
  edges: EdgeTuple[]
  http?: { routes?: RouteRef[] | null } | null
  sources?: SourcesInfo | null
  /** A layout document embedded by `gitview graph export` (layoutDoc.ts); data, validated before use. */
  layout?: unknown
}

/** Code excerpts a security report embeds instead of whole files. */
export interface SecuritySnippet {
  from: number
  to: number
  lines: string[]
}

/** The part of the `security` section the Architecture view reads. */
export interface SecuritySectionPeek {
  snippets?: Record<string, SecuritySnippet[]> | null
  audit?: { findings?: { id: string }[] | null } | null
}

// ---------- the decoded model ----------

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
  meta: NodeMeta
  out: number[]
  inn: number[]
}

export interface GEdge {
  i: number
  s: number
  t: number
  type: string
  conf: Conf
  w: number
  file: string
  line: number
  note: string
  meta: Record<string, unknown>
  src: string
}

/**
 * An item of the exploration tree: Repository → Module → directory →
 * Package → File → declaration. T[i] for i < N.length is node N[i];
 * synthetic items (repository, directories, external groups) follow.
 */
export interface TItem {
  i: number
  n: GNode | null
  id: string
  kind: string
  name: string
  label: string
  /** Name inside its parent (a method inside its type, a closure in its function). */
  short?: string
  parent: number
  kids: number[]
  depth: number
  count: number
  nfiles: number
  npkgs: number
  ext?: boolean
}

export interface Community {
  id: number
  level: string
  label: string
  size: number
  members: number
}

// ---------- state ----------

export interface Filters {
  kinds: Record<string, boolean>
  rels: Record<string, boolean>
  confs: Record<string, boolean>
  ext: boolean
}

export interface FocusState {
  i: number
  up: number
  down: number
  refs: boolean
}

export type Selection = { kind: 'item'; i: number } | { kind: 'edge'; key: string }

// ---------- scenes ----------

export type BoxKind = 'group' | 'node' | 'context' | 'more'

export interface Box {
  key: string
  ref?: number
  /** Stable ID of the tree item (positions are saved under it); none for "more" boxes. */
  id?: string
  kind: BoxKind
  x: number
  y: number
  w: number
  h: number
  depth: number
  title: string
  sub: string
  color: string // token name
  count?: string
  chev?: 'open' | 'closed' | null
  scope?: boolean
  muted?: boolean
  tip?: string
  focusCol?: number
  /** Card text: kind label, statistics and dependencies lines (sub is the fallback). */
  kindLabel?: string
  stats?: string
  deps?: string
  /** Key of the group drawn around this box. */
  parent?: string
  /** Group: the origin its content is placed from, and the size of its automatic layout from there. */
  fx?: number
  fy?: number
  minW?: number
  minH?: number
  /** Placed by the viewer (kept by automatic layouts); locked: not movable. */
  pinned?: boolean
  locked?: boolean
  // fitted text, computed by the renderer
  _title?: string
  _sub?: string
  _stats?: string
  _deps?: string
  _kind?: string
}

/** Relations of the dataset lifted to one pair of drawn items. */
export interface Agg {
  key: string
  a: number
  b: number
  ca?: boolean
  cb?: boolean
  n: number
  w: number
  types: Record<string, number>
  confs: Record<string, number>
  best: Conf
  real: number[]
}

/** The relations a drawn line stands for (a trunk merges several pairs). */
export interface LineAgg {
  n: number
  w: number
  types: Record<string, number>
  confs: Record<string, number>
  best: Conf
  members: Agg[]
}

export interface SceneEdge {
  key: string
  s: string
  t: string
  pts: number[]
  conf: Conf
  n: number
  label?: string
  /** A branch runs from an inner item to its trunk; drawn on demand. */
  branch?: boolean
  /** Branch: the trunk it joins, and the trunk's end it joins (0: start, 1: end). */
  trunk?: SceneEdge
  end?: 0 | 1
  /** Routed from the cards' rectangles (route.ts) rather than by the layout. */
  local?: boolean
  /** Local routes: the ports at each end. */
  ports?: [Port | null, Port | null]
  /** Drawn items whose relations the line carries. */
  units?: Set<string>
  agg: LineAgg
  /** Secondary: every relation it carries is weak (possible, unresolved) or a type use. */
  minor?: boolean
  _bb?: [number, number, number, number]
}

/** A relation between two drawn items, before bundling. */
export interface LogicalEdge {
  key: string
  s: string
  t: string
  agg: Agg
}

export interface Scene {
  boxes: Box[]
  edges: SceneEdge[]
  bounds: { x: number; y: number; w: number; h: number }
  /** How lines are drawn, and the flow's main axis (where local routes prefer to leave). */
  style: EdgeStyle
  prefer: 'v' | 'h'
}

export interface Visible {
  vis: Uint8Array
  groups: number[]
  unit: Int32Array
  ctx: Int32Array
  inside: Uint8Array
  root: number
}

interface ViewBase {
  scene: Scene
  out: Map<string, { key: string; s: string; t: string }[]>
  inn: Map<string, { key: string; s: string; t: string }[]>
  edges: { key: string; s: string; t: string }[]
  sceneOf?: Map<string, string[]>
  keyOf: (i: number) => string | null
}

export interface ArchView extends ViewBase {
  kind: 'arch'
  v: Visible
  counts: { boxes: number; groups: number; context: number; edges: number }
  /** The algorithm asked for, the one used (Hierarchical falls back on large drawings). */
  layout: { algorithm: Algorithm; used: Algorithm; fellBack: boolean }
}

export interface FocusView extends ViewBase {
  kind: 'focus'
  center: number
  more: Record<number, number>
  symbolMode: boolean
  counts: { boxes: number; callers: number; callees: number; edges: number }
  layout: { algorithm: Algorithm; used: Algorithm; fellBack: boolean }
}

export type View = ArchView | FocusView

export type BoxRole = 'sel' | 'out' | 'in' | 'out2' | 'in2' | 'both'
export type EdgeRole = 'out' | 'in' | 'out2' | 'in2'

export interface Highlight {
  boxes: Map<string, BoxRole>
  edges: Map<string, EdgeRole>
  keep: Set<string>
  down: number
  up: number
}

/** Which relations of the selection are highlighted. */
export type Direction = 'both' | 'out' | 'in'
