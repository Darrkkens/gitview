// Types of the `security` section: audit.Page (internal/audit/report.go)
// with the audit (model.go), the review records (store.go), the comparison
// with the previous audit (diff.go) and the AI investigations
// (internal/audit/ai/investigate.go).

import type { Excerpt } from '../http/sourceData'

export interface Completeness {
  status: string // complete | partial | …
  reasons?: string[]
}

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

export interface Evidence {
  id: string
  kind: string // source | propagation | transformation | sanitizer | validation | sink | check | absence | config | registration | dependency
  basis: string // static | heuristic
  text: string
  file?: string
  line?: number
  endLine?: number
  symbol?: string
  code?: string
  confidence?: string
  source: string
}

export interface FlowStep {
  role: string // source | propagation | transformation | sanitizer | validation | sink
  label: string
  func?: string
  file?: string
  line?: number
  code?: string
  conf?: string
  via?: string
}

export interface PathStep {
  id: string
  name: string
  role: string // route | middleware | handler | function | sink
  file?: string
  line?: number
  conf?: string
  check?: string
}

export interface RouteRef {
  id: string
  key: string
  auth: string
  path: PathStep[]
}

export interface Check {
  kind: string // authentication | authorization | tenant-filter | parameterized | sanitizer | validation
  label: string
  file?: string
  line?: number
  symbol?: string
  basis: string
}

export interface Finding {
  id: string
  ruleId: string
  title: string
  category: string
  severity: string
  confidence: string
  verdict: string // vulnerable | review | incomplete
  verdictLabel: string
  origin: string
  message: string
  why: string
  remediation: string[] | null
  tests: string[] | null
  symbol?: SymbolRef
  symbolId?: string
  file?: string
  line?: number
  endLine?: number
  routes?: RouteRef[]
  evidence: Evidence[] | null
  flow?: FlowStep[]
  checks?: Check[]
  unresolved?: string[]
  analysisStatus: string
  limits?: string[]
  anchor: string
  codeHash: string
}

export interface RuleInfo {
  id: string
  category: string
  title: string
  severity: string
  basis: string // static | heuristic | database
  detects: string
  why: string
  remediation: string[]
  tests: string[]
  limits: string
  enabled: boolean
  findings: number
}

export interface RoutePosture {
  id: string
  key: string
  auth: string
  checks?: Check[]
  findings?: string[]
  unresolved?: number
  dynamicSql?: number
}

export interface DependencyStatus {
  checked: boolean
  database?: string
  modified?: string
  stale?: boolean
  modules: number
  note?: string
}

export interface Audit {
  schema: string
  rulesVersion: string
  analyzerVersion: string
  repository: string
  commit?: string
  dirty?: boolean
  generated: string
  modules: string[] | null
  status: Completeness
  rules: RuleInfo[] | null
  findings: Finding[] | null
  routes: RoutePosture[] | null
  dependencies: DependencyStatus
  durationMs: number
}

export interface Decision {
  time: string
  from: string
  to: string
  justification: string
  commit?: string
  codeHash: string
  by: string
}

export interface ReviewRecord {
  findingId: string
  ruleId: string
  title: string
  status: string
  history: Decision[] | null
}

export interface DiffItem {
  id: string
  ruleId: string
  title: string
  severity: string
  file?: string
  line?: number
  changes?: string[]
  reason?: string
}

export interface Diff {
  baseCommit: string
  headCommit: string
  baseStatus: string
  headStatus: string
  new: DiffItem[] | null
  resolved: DiffItem[] | null
  unchanged: DiffItem[] | null
  changed: DiffItem[] | null
  unconfirmed: DiffItem[] | null
}

// ---------- AI ----------

export interface AiReference {
  evidence_id: string
  file: string
  start_line: number
  end_line: number
  symbol?: string
}

export interface AiAnswer {
  summary: string
  possible_problem: string
  why: string
  data_path?: string[]
  confidence: string
  evidence_ids?: string[]
  references?: AiReference[]
  missing_information?: string[]
  recommendations?: string[]
  suggested_tests?: string[]
  transformations?: string[]
  validations?: string[]
  security_checks?: string[]
  review_points?: string[]
  undetermined_relations?: string[]
  suggested_status?: string
  status_rationale?: string
}

export interface AiValidation {
  valid: boolean
  acceptedEvidenceIds?: string[] | null
  rejected?: { reference: string; reason: string }[] | null
  warnings?: string[] | null
}

export interface Investigation {
  kind: string // finding | path
  subject: string
  model: string
  modelDigest?: string
  endpoint?: string
  promptVersion?: string
  contextHash?: string
  answer?: AiAnswer | null
  raw?: string
  validation: AiValidation
  cached: boolean
  generated: string
  durationMs?: number
}

/** GET /api/ai/status */
export interface AiStatus {
  endpoint?: string
  reachable: boolean
  models?: { name: string; digest?: string; size?: number; family?: string; parameters?: string }[] | null
  selected?: string
  error?: string
}

/** POST /api/review */
export interface ReviewResult {
  record: ReviewRecord
  revalidate: string
}

/** The `security` section (audit.Page). */
export interface SecurityPage {
  audit: Audit
  reviews: Record<string, ReviewRecord> | null
  revalidate?: Record<string, string> | null
  ai?: Record<string, Investigation> | null
  diff?: Diff | null
  snippets: Record<string, Excerpt[]> | null
  omitted?: string[] | null
  live: boolean
}
