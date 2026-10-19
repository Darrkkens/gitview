// Types of the page's sections shared across views. Each feature refines
// the parts it owns in its own types.ts (features/<name>/types.ts).

import type { ChromaSpan } from '../components/Code'

/** The base commit the working tree is compared against. */
export interface Commit {
  hash: string
  short: string
  subject: string
  author: string
  date: string
}

/** One diff line: kind add | del | ctx; old/new are 0 when absent on that side. */
export interface DiffLine {
  kind: 'add' | 'del' | 'ctx'
  old: number
  new: number
  spans: ChromaSpan[]
}

export interface DiffHunk {
  range: string // "@@ -10,3 +10,4 @@"
  section?: string
  lines: DiffLine[]
}

/** A changed file of the working tree, highlighted by the Go side. */
export interface DiffFile {
  id: string // anchor id ("f0", "f1"…), targets of the "anchors" maps
  path: string
  oldPath?: string
  status: string // A | D | R | M
  statusName: string
  added: number
  deleted: number
  binary?: boolean
  omitted?: boolean // content withheld as potentially sensitive
  open: boolean // large diffs start collapsed
  lineCount: number
  hunks: DiffHunk[]
}

export interface TestCommand {
  command: string
  dir: string
}

export interface TestCase {
  kind: 'test' | 'package'
  package?: string
  name: string
  status: string // passed | failed | skipped | cancelled | error | running | notrun
  elapsedMs: number
  output?: string
}

/** The last test run of the session (ran=false: none was executed). */
export interface TestReport {
  ran: boolean
  target?: string
  status?: string
  structured: boolean
  started?: string
  finished?: string
  exitCode: number
  passed: number
  failed: number
  skipped: number
  other: number
  error?: string
  commands?: TestCommand[]
  tests?: TestCase[]
  log?: string[]
  logLines?: number
}

/**
 * The session report (`gitview study`, the TUI export, `gitview serve`):
 * the working tree's changes plus the analyses that were requested.
 * flow, inspect and study are owned by their features.
 */
export interface ReportSection {
  project: string
  branch: string
  root: string
  base?: Commit
  generated: string
  scope?: string
  query?: string
  added: number
  deleted: number
  files: DiffFile[]
  omitted?: string[]
  warnings?: string[]
  tests?: TestReport
  flow?: unknown
  inspect?: unknown
  study?: unknown
}
