// Types of the Study Mode data (internal/study: study.Model), read from the
// report section's `study` field, plus the parts of the Code Flow index and
// the Deep Inspector data the Study links and code boxes look at.

/** Where a fact comes from: the badge shown next to it. */
export type Basis = 'STATIC' | 'RUNTIME' | 'COVERAGE' | 'SQL' | 'GIT' | 'HEURISTIC' | string

/**
 * What a fact rests on. ref is a navigation target: "codeflow:<node>",
 * "inspect:<symbol>:<tab>", "diff:<file>:<line>" (others are ignored).
 */
export interface Evidence {
  basis: Basis
  text: string
  file?: string
  line?: number
  ref?: string
}

export interface Summary {
  modules?: string[]
  packages: number
  functions: number
  routes: number
  changedFiles: number
  changedFunctions: number
  testFunctions: number
  hasCoverage: boolean
  coverageProfile?: string
  hasTrace: boolean
  hasProfile: boolean
  hasTestRun: boolean
}

export interface PackageInfo {
  path: string
  name: string
  dir: string
  layer: string
  basis: Basis
  evidence?: Evidence[]
  functions: number
  files?: string[]
  imports?: string[]
}

export interface FuncRole {
  id: string
  name: string
  pkg: string
  file: string
  line: number
  layer: string
  basis: Basis
  evidence?: Evidence[]
  changed?: boolean
}

export interface ChainStep {
  layer: string
  id?: string
  name: string
  file?: string
  line?: number
  basis: Basis
  via?: Evidence
}

export interface Chain {
  title: string
  route: string
  steps: ChainStep[]
  note?: string
}

export interface LayerEdge {
  from: string
  to: string
  count: number
  example: Evidence
}

export interface Architecture {
  packages?: PackageInfo[]
  functions?: FuncRole[]
  chains?: Chain[]
  layerEdges?: LayerEdge[]
  notes?: string[]
}

export interface ChangedSymbol {
  id: string
  name: string
  line: number
  layer: string
}

export interface Related {
  from: string
  to: string
  toName: string
  toFile?: string
  layer: string
  dir: string // calls | called by
  changed?: boolean
  evidence: Evidence
}

export interface ChangedFile {
  path: string
  status: string
  added: number
  deleted: number
  layer: string
  basis: Basis
  evidence?: Evidence[]
  symbols?: ChangedSymbol[]
  related?: Related[]
}

export interface ChangeGroup {
  layer: string
  files: ChangedFile[]
}

export interface Step {
  n: number
  kind: 'route' | 'function' | 'sql' | 'test' | 'file' | string
  title: string
  detail?: string
  id?: string
  file?: string
  line?: number
  layer: string
  changed?: boolean
  why?: Evidence[]
}

export interface JourneyStep {
  role: 'SOURCE' | 'TRANSFORMATION' | 'DECISION' | 'SINK' | string
  kind: string
  label: string
  var?: string
  type?: string
  func?: string
  funcName?: string
  file?: string
  line?: number
  code?: string
  via?: string
  viaText?: string
  conf?: string
}

export interface DataJourney {
  func: string
  funcName: string
  variable: string
  steps: JourneyStep[]
  complete: boolean
  partial?: string[]
  evidence: Evidence
}

export interface Outcome {
  label: string
  to: number
  first?: string
  firstLine?: number
  terminal?: string
  returnsError?: boolean
  jump?: string
  assigns?: string[]
  errorPath?: boolean
  coverage?: string // covered | uncovered | partial | unknown
}

export interface Decision {
  block: number
  kind: string
  cond: string
  file: string
  line: number
  errorCheck?: boolean
  preds?: number[]
  outcomes: Outcome[]
  evidence: Evidence
}

export interface FuncDecisions {
  func: string
  funcName: string
  file: string
  line: number
  cyclomatic: number
  coverage?: string
  decisions: Decision[]
}

export interface MatrixTest {
  name: string
  file: string
  line: number
  basis: Basis
  hops: number
  via: string
  status?: string
}

export interface MatrixRow {
  scenario: string
  cond: string
  outcome: string
  block: number
  to: number
  file: string
  line: number
  status: 'OBSERVED' | 'NOT OBSERVED' | 'NO DATA' | string
  count?: number
}

export interface UnobservedBranch {
  scenario: string
  line: number
  path: string[]
}

export interface MatrixFunc {
  func: string
  funcName: string
  file: string
  line: number
  coverage?: string
  stale?: boolean
  tests?: MatrixTest[]
  rows?: MatrixRow[]
  cyclomatic: number
  observed: number
  notObserved: number
  noData: number
  unobserved?: UnobservedBranch[]
  limited?: string
}

export interface Candidate {
  id: string
  name: string
  layer: string
  depth: number
  file?: string
  line?: number
}

export interface ObservedCall {
  id: string
  name: string
  firstNs: number
  how: string
  seq: number
}

export interface PredictEntry {
  id: string
  title: string
  layer: string
  file?: string
  line?: number
  candidates: Candidate[]
  staticOrder?: string[]
  observed?: ObservedCall[]
}

export interface ObservedRun {
  source: string
  functions: number
  note: string
}

export interface Predict {
  entries?: PredictEntry[]
  observed?: ObservedRun | null
}

export interface TableRef {
  name: string
  alias?: string
  role: string
  depth?: number
}

export interface Join {
  kind: string
  table: string
  alias?: string
  on?: string
  depth?: number
}

export interface SQLStructure {
  statement: string
  tables?: TableRef[]
  joins?: Join[]
  ctes?: string[]
  columns?: string[]
  filters?: string[]
  groupBy?: string[]
  having?: string
  orderBy?: string[]
  limit?: string
  offset?: string
  returning?: string[]
  conflict?: string
  lock?: string
  partial?: boolean
  notes?: string[]
}

export interface IndexMatch {
  name?: string
  table: string
  columns?: string[]
  unique?: boolean
  primary?: boolean
  method?: string
  include?: string[]
  where?: string
  implicit?: boolean
  file: string
  line: number
  referenced?: string[]
  inPlan?: boolean
  dropped?: string
}

export interface PlanSummary {
  mode: string
  analyzed: boolean
  generic?: boolean
  scans?: string[]
  joins?: string[]
  indexes?: string[]
  warnings?: string[]
  planningMs?: number | null
  executionMs?: number | null
  evidence: Evidence
}

export interface CallerRef {
  id: string
  name: string
  file: string
  line: number
}

export interface SQLQuery {
  id: string
  func: string
  funcName: string
  file: string
  line: number
  lib: string
  method: string
  textKind: string
  sql?: string
  variants?: number
  statement: string
  inReport: boolean
  structure?: SQLStructure | null
  params?: string[]
  scan?: string[]
  tx?: string
  callers?: CallerRef[]
  indexes?: IndexMatch[]
  plan?: PlanSummary | null
  evidence?: Evidence[]
  notes?: string[]
}

export interface SQLStudy {
  queries?: SQLQuery[]
  schemaFiles?: string[]
  tables?: string[]
  indexes: number
  notes?: string[]
}

export interface PerfFunc {
  id?: string
  name: string
  file?: string
  line?: number
  found: boolean
  flat: number
  cum: number
  flatPct: number
  cumPct: number
}

export interface ProfileStudy {
  source: string
  kind: string
  sampleType: string
  unit: string
  total: number
  samples: number
  durationMs?: number
  meaning: string
  top?: PerfFunc[]
  report?: PerfFunc[]
  evidence: Evidence
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
  values?: number[]
  iterations?: number
}

export interface RegionStat {
  type: string
  symbol?: string
  instrumented?: boolean
  count: number
  unfinished?: number
  totalNs: number
  minNs: number
  maxNs: number
}

export interface PerfObserved {
  cpu?: ProfileStudy | null
  mem?: ProfileStudy | null
  benchmarks?: BenchStat[]
  benchFile?: string
  regions?: RegionStat[]
  traceFile?: string
  missing?: string[]
}

export interface StaticPerf {
  func: string
  funcName: string
  file: string
  line: number
  loops: number
  cyclomatic: number
  dbOps: number
  dbReach: number
  escapes?: string[]
  inline?: string
  escapeRun: boolean
}

export interface PerfStudy {
  observed?: PerfObserved
  static?: StaticPerf[]
  notes?: string[]
}

export interface Exercise {
  id: string
  kind: 'quiz' | 'challenge' | string
  type: string
  prompt: string
  code?: string
  given?: string[]
  options: string[]
  answer: number
  sourceFile?: string
  sourceLine?: number
  sourceType: string
  evidence: Evidence
}

export interface ExplainItem {
  key: string
  prompt: string
  facts?: string[]
  basis: Basis
  ref?: string
}

export interface ExplainFunc {
  func: string
  funcName: string
  file: string
  line: number
  layer: string
  items: ExplainItem[]
}

export interface StudyModel {
  version: number
  fingerprint: string
  summary?: Summary
  layers?: string[]
  architecture?: Architecture
  changes?: ChangeGroup[]
  readingOrder?: Step[]
  dataJourneys?: DataJourney[]
  decisions?: FuncDecisions[]
  testMatrix?: MatrixFunc[]
  predict?: Predict
  sql?: SQLStudy
  performance?: PerfStudy
  exercises?: Exercise[]
  explain?: ExplainFunc[]
  notes?: string[]
}

/** The parts of a Code Flow node the Study reads. */
export interface FlowNodeLite {
  id: string
  kind: string
  name: string
  file?: string
  line?: number
  code?: string
}

/** The parts of a Deep Inspector bundle the Study reads. */
export interface InspectBundleLite {
  symbol: { id: string; name: string; file?: string; line?: number }
  code?: string
}

export interface FlowLite {
  nodes?: FlowNodeLite[]
  error?: string
}

export interface InspectLite {
  symbols?: InspectBundleLite[]
}
