// Types of the Go Deep Inspector view: the `inspect` field of the report
// section (export.InspectView = InspectData + anchors), as serialised by the
// Go side (internal/inspect and its packages). The browser only displays it.

/** evidence.SymbolRef: the identity of a Go function. */
export interface SymbolRef {
  id: string
  name: string
  qualified: string
  pkg: string
  recv?: string
  sig?: string
  file?: string
  line?: number
  end?: number
  module?: string
  version?: string
}

/** evidence.Completeness */
export interface Completeness {
  status: 'complete' | 'partial' | 'inconclusive' | 'unavailable' | string
  reasons?: string[]
}

/** evidence.Artifact: a file a runtime result came from. */
export interface Artifact {
  kind: string
  name: string
  imported: boolean
  command?: string
  dir?: string
  capturedAt?: string
  codeVersion?: string
  stale?: boolean
  notes?: string[]
}

export type Confidence = 'exact' | 'derived' | 'likely' | 'possible' | 'unresolved' | string

// ---------- Branch Explorer (internal/inspect/branch) ----------

export interface CfgStmt {
  line: number
  text: string
  kind: string
}

export interface CfgBlock {
  id: number
  file: string
  line: number
  end: number
  stmts?: CfgStmt[]
  decision?: string // if | for | range | switch | type switch | select
  cond?: string
  condLine?: number
  terminal?: string
  entry?: boolean
  reachable: boolean
  loopHead?: boolean
  errorCheck?: boolean
  returnsError?: boolean
  defers?: number
  coverage?: 'covered' | 'uncovered' | 'partial' | 'unknown' | string
  coverCount?: number
}

export interface CfgEdge {
  from: number
  to: number
  label?: string
  kind: string // branch | loop | …
  error?: boolean
}

export interface CfgPath {
  blocks: number[]
  edges: number[] // indices into edges
  steps: string[]
  end: number
  exit: string
  error?: boolean
}

export interface CfgMetrics {
  blocks: number
  edges: number
  decisionPoints: number
  decisions: number
  cyclomatic: number
  returns: number
  panics: number
  loops: number
  unreachable: number
  defers: number
  paths: number
  pathsListed: number
  pathsSaturated?: boolean
}

export interface CoverageSummary {
  profile: string
  mode: string
  covered: number
  uncovered: number
  partial: number
  unknown: number
  stale?: boolean
  note: string
}

export interface BranchGraph {
  symbol: SymbolRef
  blocks: CfgBlock[]
  edges: CfgEdge[]
  paths: CfgPath[] | null
  metrics: CfgMetrics
  defers?: CfgStmt[]
  coverage?: CoverageSummary
  status: Completeness
  notes?: string[]
  entry: number
  truncated?: boolean
}

// ---------- Data Flow (internal/inspect/dataflow) ----------

export interface VarInfo {
  name: string
  kind: string // param | local | result
  type: string
  line: number
  refs: number
}

export interface DfNode {
  id: number
  kind: string // value | param | call | const | merge | field | source | sql | check | external | return | unresolved | global
  label: string
  var?: string
  type?: string
  func?: string
  funcName?: string
  file?: string
  line?: number
  code?: string
  detail?: string
  tags?: string[]
  dir: string // selected | origin | destination
  level: number
  hops: number
  conf: Confidence
  intermediate?: boolean
}

export interface DfEdge {
  from: number
  to: number
  kind: string
  conf: Confidence
  label?: string
  file?: string
  line?: number
  dir: string // origin | destination
}

export interface DfValidation {
  node: number
  text: string
  kind: string
  func: string
  file: string
  line: number
}

export interface DataFlowResult {
  symbol: SymbolRef
  selection: { kind: string; name: string; type?: string; line?: number }
  direction: string
  nodes: DfNode[]
  edges: DfEdge[]
  roots: number[] | null
  sources?: number[]
  sinks?: number[]
  validations?: DfValidation[]
  status: Completeness
  notes?: string[]
  maxDepth: number
}

// ---------- SQL Explorer (internal/inspect/sqlops) ----------

export interface SqlText {
  kind: string // none | literal | constant | concat | sprintf | variants | …
  text?: string
  formatted?: string
  template?: string
  variants?: string[]
  constant?: string
  constFile?: string
  constLine?: number
  statement: string
  multi?: boolean
  placeholders?: number
  questionMarks?: number
  note?: string
}

export interface ParamOrigin {
  label: string
  kind: string
  file?: string
  line?: number
  func?: string
  conf: Confidence
}

export interface SqlParam {
  index: number
  expr: string
  type?: string
  line?: number
  col?: number
  origins?: ParamOrigin[]
  originStatus?: string
}

export interface SqlScan {
  method: string
  file: string
  line: number
  targets: { expr: string; type?: string }[] | null
  inLoop?: boolean
  note?: string
}

export interface SqlTxRef {
  role: string
  var?: string
  beginLine?: number
  fromParam?: boolean
  deferred?: boolean
}

export interface SqlOp {
  id: string
  lib: string
  op: string
  method: string
  receiver?: string
  func: SymbolRef
  file: string
  line: number
  call: string
  conf: Confidence
  via?: string[]
  sql: SqlText
  params?: SqlParam[]
  paramsKnown: boolean
  scan?: SqlScan
  tx?: SqlTxRef
  notes?: string[]
  deferred?: boolean
}

export interface SqlTx {
  begin: string
  var?: string
  func: string
  line: number
  ops: string[]
  commits?: number[]
  rollbacks?: number[]
  deferredRollback?: boolean
}

export interface SqlResult {
  symbol: SymbolRef
  ops: SqlOp[] | null
  transactions?: SqlTx[]
  reachable: boolean
  depth?: number
  status: Completeness
  notes?: string[]
}

// ---------- PostgreSQL EXPLAIN (internal/inspect/pgexplain) ----------

export interface PlanCondition {
  name: string
  value: string
}

export interface PlanNode {
  nodeType: string
  relation?: string
  alias?: string
  index?: string
  joinType?: string
  strategy?: string
  parentRelationship?: string
  startupCost: number
  totalCost: number
  planRows: number
  planWidth: number
  actualStartupMs?: number
  actualTotalMs?: number
  actualRows?: number
  actualLoops?: number
  rowsRemovedByFilter?: number
  conditions?: PlanCondition[]
  keys?: PlanCondition[]
  buffers?: string
  plans?: PlanNode[]
}

export interface Plan {
  target: string
  server: string
  statement: string
  sql: string
  mode: string
  analyzed: boolean
  generic: boolean
  paramsGiven?: number
  root: PlanNode
  planningMs?: number
  executionMs?: number
  scans?: string[]
  joins?: string[]
  indexes?: string[]
  warnings?: string[]
  at: string
}

// ---------- Performance (internal/inspect/perf) ----------

export interface FuncStat {
  name: string
  file?: string
  line?: number
  flat: number
  cum: number
  flatPct: number
  cumPct: number
  symbol?: string
}

export interface LineStat {
  func: string
  file: string
  line: number
  flat: number
  cum: number
  symbol?: string
}

export interface CallEdge {
  from: string
  to: string
  weight: number
}

export interface FlameNode {
  name: string
  value: number
  self?: number
  symbol?: string
  children?: FlameNode[]
}

export interface ProfileReport {
  kind: string // cpu | heap | block | mutex | goroutine | other
  sampleType: string
  sampleTypes: string[]
  unit: string
  total: number
  samples: number
  durationMs?: number
  period?: string
  meaning: string
  funcs: FuncStat[] | null
  funcCount: number
  lines: LineStat[] | null
  edges: CallEdge[] | null
  flame: FlameNode | null
  notes?: string[]
  artifact: Artifact
  status: Completeness
}

export interface SymbolPerf {
  symbol: string
  found: boolean
  flat: number
  cum: number
  flatPct: number
  cumPct: number
  lines?: LineStat[]
  callers?: CallEdge[]
  callees?: CallEdge[]
}

export interface Diagnostic {
  file: string
  line: number
  col: number
  kind: string
  message: string
  flow?: string[]
  symbol?: string
  func?: string
}

export interface EscapeReport {
  packages: string[] | null
  diagnostics: Diagnostic[] | null
  errors?: string[]
  artifact: Artifact
}

export interface BenchResult {
  pkg?: string
  name: string
  procs?: number
  iterations: number
  metrics: Record<string, number>
  units: string[]
}

export interface BenchRun {
  config: Record<string, string> | null
  results: BenchResult[] | null
  failures?: string[]
  artifact: Artifact
}

export interface BenchStat {
  pkg?: string
  name: string
  procs?: number
  unit: string
  n: number
  mean: number
  median: number
  min: number
  max: number
  stddev: number
  values: number[]
  iterations: number
}

export interface CompareRow {
  pkg?: string
  name: string
  unit: string
  old?: BenchStat
  new?: BenchStat
  deltaPct: number
  deltaUndefined?: boolean
  p: number
  significant: boolean
  note?: string
}

export interface Comparison {
  rows: CompareRow[] | null
  configDiffs?: string[]
  method: string
  benchstat?: string
  alpha: number
}

/** inspect.PerfView: what the loaded performance data says about a symbol. */
export interface PerfView {
  cpu?: SymbolPerf
  mem?: SymbolPerf
  cpuProfile?: string
  memProfile?: string
  memType?: string
  escape?: Diagnostic[]
  escapeRun: boolean
  benchmarks?: BenchStat[]
  message?: string
}

// ---------- Execution Trace (internal/inspect/xtrace) ----------

export interface Frame {
  func: string
  file?: string
  line?: number
  symbol?: string
}

export interface Segment {
  s: number
  e: number
  st: string // running | runnable | waiting | syscall
  r?: string
  k: number
  p?: number
}

export interface Goroutine {
  id: number
  name: string
  symbol?: string
  created: number
  ended: number
  creator?: number
  createStack: number
  segments: Segment[] | null
  runningNs: number
  runnableNs: number
  waitingNs: number
  syscallNs: number
  system?: boolean
}

export interface Span {
  name: string
  s: number
  e: number
  g?: number
}

export interface TraceTask {
  id: number
  parent?: number
  type: string
  s: number
  e: number
  goroutines?: number[]
}

export interface Region {
  g: number
  task?: number
  type: string
  s: number
  e: number
  k: number
  instrumented?: boolean
  symbol?: string
}

export interface TraceLog {
  t: number
  g: number
  task?: number
  category?: string
  message: string
  k: number
}

export interface TraceEvent {
  t: number
  g: number
  kind: string // block | unblock | create | syscall | end
  detail?: string
  dur?: number
  k: number
  other?: number
}

export interface TraceStats {
  events: number
  goroutines: number
  userGoroutines: number
  gcCycles: number
  gcNs: number
  stwNs: number
  blockedNs: Record<string, number> | null
  procs: number
  stackSamples?: number
}

export interface Timeline {
  durationNs: number
  startWall?: string
  goroutines: Goroutine[] | null
  gc: Span[] | null
  ranges?: Span[]
  tasks?: TraceTask[]
  regions?: Region[]
  logs?: TraceLog[]
  events?: TraceEvent[]
  stacks: Frame[][] | null
  stats: TraceStats
  truncated?: boolean
  notes?: string[]
  artifact: Artifact
  status: Completeness
}

export interface SymbolTrace {
  symbol: string
  goroutines?: number[]
  spans?: Region[]
  spanCount: number
  spanTotalNs: number
  spanMinNs: number
  spanMaxNs: number
  blocks?: TraceEvent[]
  logs?: TraceLog[]
  found: boolean
}

/** inspect.TraceView: what the loaded trace shows about a symbol. */
export interface TraceView {
  loaded: boolean
  trace?: string
  symbol?: SymbolTrace
  message?: string
}

// ---------- the bundle and the section ----------

/** inspect.Bundle: everything known about one symbol. */
export interface Bundle {
  symbol: SymbolRef
  code?: string
  branches?: BranchGraph
  variables: VarInfo[] | null
  dataflow?: Record<string, DataFlowResult>
  sql: SqlResult | null
  performance: PerfView | null
  trace: TraceView | null
  errors?: Record<string, string>
}

/** export.InspectView */
export interface InspectSection {
  symbols: Bundle[] | null
  cpu?: ProfileReport
  mem?: ProfileReport
  benches?: BenchRun[]
  compare?: Comparison
  escape?: EscapeReport
  trace?: Timeline
  plans?: Plan[]
  notes?: string[]
  errors?: string[]
  /** Changed file path → diff anchor id. */
  anchors: Record<string, string> | null
}

// ---------- the Code Flow index of the report (report.flow) ----------

export interface FlowNode {
  id: string
  kind: string
  name: string
  pkg?: string
  sig?: string
  file?: string
  line?: number
  explain?: string
  tags?: string[]
}

export interface FlowEdge {
  from: string
  to: string
  kind: string
  conf: string
  file?: string
  line?: number
  note?: string
}

export interface FlowIndex {
  nodes?: FlowNode[] | null
  edges?: FlowEdge[] | null
  error?: string
}

export type TabId = 'codeflow' | 'dataflow' | 'trace' | 'branches' | 'perf' | 'sql'
