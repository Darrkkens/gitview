// Types of the Changes view: the session report's diff part (section
// "report", built by internal/export/section.go). The shared definitions
// live in lib/types.ts; this file only narrows what the view relies on.

import type { ChromaSpan } from '../../components/Code'
import type { Commit, DiffFile, DiffHunk, DiffLine, ReportSection, TestReport } from '../../lib/types'

export type { ChromaSpan, Commit, DiffFile, DiffHunk, DiffLine, ReportSection, TestReport }

/** Status letters of DiffFile.status. */
export type StatusLetter = 'A' | 'D' | 'R' | 'M'

/** One row of a rendered diff: a hunk header or a line of that hunk. */
export type DiffRow = { hunk: number; line: -1 } | { hunk: number; line: number }

/** A slice of a file's rows, mounted only when it comes near the viewport. */
export interface DiffChunk {
  rows: DiffRow[]
  /** Estimated height in pixels while it is not mounted. */
  height: number
}
