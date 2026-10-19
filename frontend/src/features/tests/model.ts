// The Tests view's model: a port of buildTests (internal/export/html.go),
// which turned a TestReport into the report's summary, packages, failures
// and log tail. Nothing is invented: a run that was not structured only has
// its exit code and log.

import type { CaseView, PackageView, TestCase, TestReport, TestsModel, Tone } from './types'

/** Package-level results, named in Portuguese by the test runner. */
const PACKAGE_LABELS: Record<string, string> = {
  '(falha de compilação)': 'Build failed',
  '(pacote)': 'Package failed',
  '(arquivo de teste)': 'Test file failed to run',
}

const STATUS: Record<string, { glyph: string; tone: Tone; label: string; bucket: CaseView['bucket'] }> = {
  passed: { glyph: '✓', tone: 'success', label: 'passed', bucket: 'passed' },
  failed: { glyph: '✗', tone: 'error', label: 'failed', bucket: 'failed' },
  skipped: { glyph: '○', tone: 'muted', label: 'skipped', bucket: 'skipped' },
  cancelled: { glyph: '○', tone: 'warning', label: 'cancelled', bucket: 'other' },
  error: { glyph: '!', tone: 'warning', label: 'did not finish', bucket: 'other' },
  running: { glyph: '…', tone: 'warning', label: 'running', bucket: 'other' },
  notrun: { glyph: '·', tone: 'muted', label: 'not run', bucket: 'other' },
}

export const caseKey = (c: Pick<TestCase, 'package' | 'name'>) => (c.package ? c.package + '/' : '') + c.name

/** runStatus is the run's overall status and tone ("Failed", error). */
export function runStatus(t: TestReport): { text: string; tone: Tone } {
  switch (t.status) {
    case 'passed':
      return { text: 'Passed', tone: 'success' }
    case 'failed':
      return { text: 'Failed', tone: 'error' }
    case 'cancelled':
      return { text: 'Cancelled', tone: 'warning' }
    case 'running':
      return { text: 'Running', tone: 'warning' }
  }
  return { text: 'Did not complete', tone: 'warning' }
}

/**
 * goDuration formats milliseconds like Go's time.Duration.String() after
 * Round(time.Millisecond): "850ms", "1.204s", "2m3.5s", "1h0m2s".
 */
export function goDuration(ms: number): string {
  ms = Math.round(ms)
  if (ms <= 0) return '0s'
  if (ms < 1000) return ms + 'ms'
  const h = Math.floor(ms / 3_600_000)
  const m = Math.floor((ms % 3_600_000) / 60_000)
  const s = (ms % 60_000) / 1000
  const sec = s.toFixed(3).replace(/\.?0+$/, '') + 's'
  if (h) return h + 'h' + m + 'm' + sec
  if (m) return m + 'm' + sec
  return sec
}

/** caseDuration is a test's elapsed time ("42 ms"), empty when unknown. */
export function caseDuration(ms: number): string {
  if (!(ms > 0)) return ''
  if (ms < 1000) return ms + ' ms'
  return goDuration(ms)
}

/** stamp formats a time like "Oct 18, 2026 15:58:01" (local time). */
export function stamp(iso: string | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const date = d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
  const time = d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })
  return date + ' ' + time
}

const zero = () => ({ passed: 0, failed: 0, skipped: 0, other: 0 })

export function buildTests(t: TestReport): TestsModel {
  const st = runStatus(t)
  const summary: TestsModel['summary'] = []
  if (t.structured) {
    summary.push({ text: t.passed + ' passed', tone: t.passed > 0 ? 'success' : 'muted' })
    summary.push({ text: t.failed + ' failed', tone: t.failed > 0 ? 'error' : 'muted' })
    if (t.skipped > 0) summary.push({ text: t.skipped + ' skipped', tone: 'muted' })
    if (t.other > 0) summary.push({ text: t.other + ' other', tone: 'warning' })
  } else {
    summary.push({ text: 'exit code ' + t.exitCode, tone: t.exitCode !== 0 ? 'error' : 'muted' })
  }
  let duration = ''
  if (t.finished && t.started) {
    const ms = new Date(t.finished).getTime() - new Date(t.started).getTime()
    if (isFinite(ms)) duration = goDuration(ms)
  }

  const index = new Map<string, number>()
  const packages: PackageView[] = []
  const failures: CaseView[] = []
  const counts = zero()
  for (const c of t.tests ?? []) {
    const s = STATUS[c.status] ?? STATUS.notrun
    const cv: CaseView = {
      ...c,
      key: caseKey(c),
      label: c.kind === 'package' ? (PACKAGE_LABELS[c.name] ?? c.name) : c.name,
      glyph: s.glyph,
      tone: s.tone,
      statusLabel: s.label,
      bucket: s.bucket,
    }
    const pkg = c.package ?? ''
    let i = index.get(pkg)
    if (i === undefined) {
      i = packages.length
      index.set(pkg, i)
      packages.push({ name: pkg, outcomes: [], tests: [], counts: zero(), elapsedMs: 0 })
    }
    const p = packages[i]
    if (c.kind === 'package') p.outcomes.push(cv)
    else p.tests.push(cv)
    p.counts[cv.bucket]++
    counts[cv.bucket]++
    // Subtests run inside their parent: only top-level times add up.
    if (c.kind !== 'package' && !c.name.includes('/')) p.elapsedMs += c.elapsedMs || 0
    if ((c.status === 'failed' || c.status === 'error') && c.output) failures.push(cv)
  }
  const log = t.log ?? []
  return {
    report: t,
    status: st.text,
    tone: st.tone,
    summary,
    duration,
    started: stamp(t.started),
    packages,
    failures,
    logText: log.join('\n'),
    logShown: log.length,
    logHidden: Math.max((t.logLines ?? 0) - log.length, 0),
    logTotal: t.logLines ?? log.length,
    counts,
  }
}
