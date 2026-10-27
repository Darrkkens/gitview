// Types of the runtime section (export.RuntimeSection): a capture analysed
// by internal/live/report (Analysis), optionally compared with an earlier
// one (Comparison, whose A side is `base`), and in serve mode the list of
// saved captures (capture.Entry). Durations are Go time.Duration values, in
// nanoseconds.

/** How a value was obtained: the honesty marker shown next to it. */
export type Basis = 'measured' | 'sampled' | 'estimated' | 'unavailable' | string

export interface Metric {
  name: string
  value: number
  unit: string // % | bytes | ns | count | bytes/s | ""
  basis: Basis
  scope: string
  note?: string
}

export interface PhaseStats {
  name: string
  from: number
  to: number
  samples: number
  hasCPU: boolean
  cpuAvg: number
  cpuMax: number
  cpuTime: number
  hasMem: boolean
  rssStart: number
  rssEnd: number
  rssMin: number
  rssMax: number
  threads: number
  hasCtx: boolean
  volCtx: number
  involCtx: number
  hasIO: boolean
  read: number
  write: number
  rchar: number
  wchar: number
  hasFDs?: boolean
  fds?: number
  hasNet?: boolean
  netRx?: number
  netTx?: number
}

export interface Point {
  t: number // seconds
  cpu: number // % of one core
  rss: number
  vsz?: number // virtual memory
  threads: number
  fds: number
}

export interface Process {
  phases?: PhaseStats[]
  series?: Point[]
  cpuResolution: string
  unavailable?: Record<string, string>
}

export interface Latency {
  n: number
  min: number
  p50: number
  p90: number
  p99: number
  max: number
  mean: number
}

export interface Location {
  symbol: string
  name: string
  file: string
  line: number
  conf: string
}

export interface SQLMatch {
  opId: string
  func: Location
  conf: string
  basis: string
}

export interface StatementStats {
  query: string
  count: number
  perRequest?: number
  total: number
  mean: number
  max: number
  errors?: number
  rows?: number
  site?: string
  match?: SQLMatch | null
  sources?: string[]
  unfinished?: number
}

export interface TraceStats {
  requests: number
  runningNs: number
  runnableNs: number
  waitingNs: number
  syscallNs: number
  gcNs: number
  stwNs: number
  waits?: Record<string, number>
}

export interface RouteMatch {
  method: string
  path: string
  router: string
  file: string
  line: number
  handler?: Location | null
  middleware?: Location[]
  conf: string
  basis: string
}

export interface PathStep {
  name: string
  symbol?: string
  value: number
  self: number
  pct: number
}

export interface FlameNode {
  name: string
  value: number
  self?: number
  symbol?: string
  children?: FlameNode[]
}

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

export interface RouteStats {
  key: string
  keyBasis: string
  count: number
  outside: number
  status?: Record<string, number>
  errors: number
  latency: Latency
  latencyBasis: string
  proxy?: Latency | null
  ttfb?: Latency | null
  bytesIn: number
  bytesOut: number
  maxInFlight: number
  meanInFlight: number
  rate: number
  isolated: number
  metrics?: Metric[]
  statements?: StatementStats[]
  sqlShare?: number
  route?: RouteMatch | null
  hotPath?: PathStep[]
  flame?: FlameNode | null
  topFuncs?: FuncStat[]
  trace?: TraceStats | null
  durations?: number[] // ms, active phase
  labelled?: boolean
  sqlObserved: boolean
  sources?: Record<string, number>
}

export interface RequestRow {
  id: string
  key: string
  method: string
  path: string
  status: number
  off: number
  dur: number
  proxy?: number
  phase: string
  sql: number // -1: no source of this request can see SQL
  sqlTime: number
  cpu: number // sampled ns, -1 when unknown
  perfSamples?: number // Linux perf samples of the whole process during the request; -1: no perf data
  inFlight?: number
  sources?: string[]
  error?: string
  panic?: boolean
}

export interface ProfileReport {
  kind: string
  sampleType: string
  sampleTypes?: string[]
  unit: string
  total: number
  samples: number
  durationMs?: number
  period?: string
  meaning: string
  funcs?: FuncStat[]
  funcCount?: number
  flame?: FlameNode | null
  notes?: string[]
}

export interface Profiles {
  cpu?: ProfileReport | null
  cpuCoverage?: number
  cpuByTrace?: Record<string, number>
  cpuLabelled?: number
  cpuUnlabelled?: number
  allocs?: ProfileReport | null
  objects?: number
  inuse?: ProfileReport | null
  mutex?: ProfileReport | null
  block?: ProfileReport | null
  goroutines?: ProfileReport | null
  goroutinesByTrace?: Record<string, number>
  notes?: string[]
}

/** CPU sampled from the outside by Linux perf (whole process). */
export interface PerfReport {
  cpu?: ProfileReport | null
  event: string
  freq: number
  userOnly?: boolean
  recorded: number
  samples: number // in the active phase
  sampledCPU: number // ns, samples × period
  stats: { samples: number; unknownLeaf: number; broken: number; byPclntab: number; kernel: number; foreign: number }
  bucketSecs: number
  timeline?: { t: number; samples: number; cpu: number }[]
  notes?: string[]
  warnings?: string[]
}

/** The process with and without requests in flight (active phase). */
export interface Correlation {
  busySecs: number
  idleSecs: number
  busyCPU: number
  idleCPU: number
  hasPerf: boolean
  busyPerf: number
  idlePerf: number
  busyRSS: number
  idleRSS: number
  note: string
}

export interface RuntimeSeriesPoint {
  t: number
  heapLive: number
  goroutines: number
  gcCycles: number
}

export interface GoRuntime {
  metrics?: Metric[]
  series?: RuntimeSeriesPoint[]
}

export interface TraceAnalysis {
  requests?: { id: string }[]
  unmatched?: number
  startWall?: string
  wallFromRun?: boolean
  stats?: {
    events?: number
    goroutines?: number
    userGoroutines?: number
    gcCycles?: number
    gcNs?: number
    stwNs?: number
    blockedNs?: Record<string, number>
    procs?: number
  }
  durationNs: number
  notes?: string[]
}

export interface ProcessInfo {
  pid: number
  comm: string
  exe?: string
  exeDeleted?: boolean
  started?: string
  go?: boolean
  goVersion?: string
  mainModule?: string
  mainVersion?: string
  vcsRevision?: string
  vcsModified?: boolean
  ports?: { proto: string; addr: string; port: number; loopback?: boolean }[]
  unavailable?: Record<string, string>
  cpus?: number
}

export interface Settings {
  interval: number
  baseline: number
  duration?: number
  cooldown: number
  maxDuration: number
  proxy?: string
  target?: string
  collector?: string
  otlp?: string
  endpoint?: string
  profile?: Record<string, unknown>
}

export interface Window {
  start: number
  end: number
  file?: string
  err?: string
}

export interface Phases {
  activeStart: number
  activeEnd: number
  end: number
  paused?: Window[]
}

export interface Analysis {
  id: string
  label?: string
  created: string
  status: string
  statusNote?: string
  repo?: string
  processInfo: ProcessInfo
  settings: Settings
  phases: Phases
  sources?: string[]
  process: Process
  routes?: RouteStats[]
  requests?: RequestRow[]
  background?: StatementStats[]
  profiles?: Profiles | null
  perf?: PerfReport | null
  cpuViews?: Metric[]
  correlation?: Correlation | null
  hotPath?: PathStep[]
  runtime?: GoRuntime | null
  trace?: TraceAnalysis | null
  stale?: string[]
  limitations?: string[]
  warnings?: string[]
  notes?: string[]
}

export interface Condition {
  name: string
  ok: boolean
  detail: string
  blocking?: boolean
}

export interface Delta {
  name: string
  a: number
  b: number
  unit: string
  change: number | null
  basis: Basis
  scope: string
  note?: string
}

export interface RouteDelta {
  key: string
  na: number
  nb: number
  a: Latency
  b: Latency
  change: number | null
  p: number | null
  verdict: string // slower | faster | no significant change | inconclusive | …
  reasons?: string[]
  deltas?: Delta[]
  sqlAdded?: string[]
  sqlGone?: string[]
}

export interface FuncDelta {
  name: string
  symbol?: string
  a: number
  b: number
}

export interface Comparison {
  a: string
  b: string
  conditions?: Condition[]
  comparable: boolean
  routes?: RouteDelta[]
  onlyA?: string[]
  onlyB?: string[]
  process?: Delta[]
  functions?: FuncDelta[]
  regressions?: string[]
  notes?: string[]
}

/** A saved capture (serve mode). */
export interface CaptureEntry {
  id: string
  label?: string
  created: string
  status: string
  pid: number
  exe?: string
  active: number
  requests: number
  routes?: string[]
  bytes: number
}

export interface RuntimeSection {
  a: Analysis | null
  base?: Analysis | null
  comparison?: Comparison | null
  generated: string
  captures?: CaptureEntry[]
}
