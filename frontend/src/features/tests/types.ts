// Types of the Tests view: the session's last test run (report.tests,
// export.TestReport on the Go side). TestCase.status holds the keys of
// testrunner.Status.Key(): notrun | running | passed | failed | skipped |
// cancelled | error.

import type { ReportSection, TestCase, TestCommand, TestReport } from '../../lib/types'

export type { ReportSection, TestCase, TestCommand, TestReport }

export type CaseStatus = 'notrun' | 'running' | 'passed' | 'failed' | 'skipped' | 'cancelled' | 'error'

export type Tone = 'success' | 'error' | 'warning' | 'muted'

/** A test case ready to display. */
export interface CaseView extends TestCase {
  /** Stable key: "<package>/<name>" (the ?test= deep link). */
  key: string
  label: string
  glyph: string
  tone: Tone
  statusLabel: string
  /** Bucket of the status filter. */
  bucket: 'passed' | 'failed' | 'skipped' | 'other'
}

export interface PackageView {
  name: string
  /** Package-level outcomes (build failure, crash) that are not tests. */
  outcomes: CaseView[]
  tests: CaseView[]
  counts: Record<CaseView['bucket'], number>
  elapsedMs: number
}

export interface TestsModel {
  report: TestReport
  status: string
  tone: Tone
  summary: { text: string; tone: Tone }[]
  duration: string
  started: string
  packages: PackageView[]
  failures: CaseView[]
  logText: string
  logShown: number
  logHidden: number
  logTotal: number
  counts: Record<CaseView['bucket'], number>
}
