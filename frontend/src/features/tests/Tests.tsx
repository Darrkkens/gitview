// The Tests view: the session's last test run as evidence — command,
// duration, exit code, results per package, failures and the log.
// Package-level results (a build failure) are not shown as tests.

import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { CopyButton } from '../../components/CopyButton'
import { Icon } from '../../components/Icon'
import { StatusSlot } from '../../components/Slots'
import { download, isTyping } from '../../lib/browser'
import { cx } from '../../lib/format'
import { setParams } from '../../lib/router'
import { usePrintable } from '../changes/print'
import { buildTests, caseDuration } from './model'
import type { CaseView, PackageView, TestReport, TestsModel, Tone } from './types'

const nf = new Intl.NumberFormat('en-US')
const num = (n: number) => nf.format(n || 0)

const TONE_TEXT: Record<Tone, string> = {
  success: 'text-success',
  error: 'text-error',
  warning: 'text-warning',
  muted: 'text-fg-3',
}
const TONE_BADGE: Record<Tone, string> = {
  success: 'badge-success',
  error: 'badge-error',
  warning: 'badge-warning',
  muted: 'badge-neutral',
}

type Bucket = CaseView['bucket'] | 'all'
const BUCKETS: { id: Bucket; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'failed', label: 'Failed' },
  { id: 'passed', label: 'Passed' },
  { id: 'skipped', label: 'Skipped' },
  { id: 'other', label: 'Other' },
]

function Stat({ label, value, tone, hint }: { label: string; value: ReactNode; tone?: Tone; hint?: string }) {
  return (
    <div className="min-w-24 rounded-box border border-line bg-base-100 px-3 py-2" title={hint}>
      <div className="text-[11px] font-medium tracking-wide text-fg-3 uppercase">{label}</div>
      <div className={cx('mt-0.5 font-mono text-lg leading-tight font-semibold', tone ? TONE_TEXT[tone] : 'text-fg')}>
        {value}
      </div>
    </div>
  )
}

function Kv({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-fg-3">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </>
  )
}

export function Tests({ tests, params }: { tests: TestReport; params: URLSearchParams }) {
  const m = useMemo(() => buildTests(tests), [tests])
  const t = m.report
  const [bucket, setBucket] = useState<Bucket>('all')
  const [query, setQuery] = useState('')
  const [closed, setClosed] = useState<Record<string, boolean>>({})
  const searchRef = useRef<HTMLInputElement>(null)
  usePrintable()

  // Deep link: #/tests?test=<package/name>.
  const want = params.get('test') ?? ''
  const selected = useMemo(() => {
    if (!want) return null
    for (const p of m.packages) for (const c of [...p.outcomes, ...p.tests]) if (c.key === want) return c
    for (const p of m.packages) for (const c of p.tests) if (c.name === want) return c
    return null
  }, [want, m])
  const q = query.trim().toLowerCase()
  const match = (c: CaseView) =>
    (bucket === 'all' || c.bucket === bucket) &&
    (!q ||
      c.label.toLowerCase().includes(q) ||
      c.name.toLowerCase().includes(q) ||
      (c.package ?? '').toLowerCase().includes(q))

  // Reveal a linked result hidden by a filter or a folded package. Adjusted
  // while rendering (not in an effect) when the link changes.
  const [revealed, setRevealed] = useState('')
  if (want !== revealed) {
    setRevealed(want)
    if (selected && (!match(selected) || closed[selected.package ?? ''])) {
      setClosed((c) => ({ ...c, [selected.package ?? '']: false }))
      setBucket((b) => (b === 'all' || b === selected.bucket ? b : 'all'))
      setQuery('')
    }
  }
  // Scroll to it, unless the link came from a click on the row itself.
  const selfNav = useRef('')
  useEffect(() => {
    if (!selected) return
    const self = selfNav.current === selected.key
    selfNav.current = ''
    if (self) return
    document
      .querySelector<HTMLElement>(`[data-test-key="${CSS.escape(selected.key)}"]`)
      ?.scrollIntoView({ block: 'center' })
  }, [selected])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || isTyping(e)) return
      if (e.key === '/' && searchRef.current) {
        e.preventDefault()
        searchRef.current.focus()
        searchRef.current.select()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const shown = m.packages
    .map((p) => ({ p, outcomes: p.outcomes.filter(match), tests: p.tests.filter(match) }))
    .filter((x) => x.outcomes.length + x.tests.length > 0)

  const select = (c: CaseView) => {
    selfNav.current = c.key
    setParams({ test: c.key })
  }
  const showOutput = (c: CaseView) => {
    const el = document.getElementById('failure-' + c.key)
    if (!el) return
    if (el instanceof HTMLDetailsElement) el.open = true
    el.scrollIntoView({ block: 'start', behavior: 'smooth' })
  }
  const allOpen = m.packages.every((p) => !closed[p.name])

  return (
    <div className="scroll-thin h-full overflow-y-auto print:h-auto print:overflow-visible">
      <div className="mx-auto max-w-6xl px-4 py-5 sm:px-6">
        <header>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <h1 className="text-lg font-semibold">Tests</h1>
            <span className={cx('badge badge-soft badge-sm font-medium', TONE_BADGE[m.tone])}>{m.status}</span>
            <span className="flex flex-wrap items-center gap-x-2 text-[13px]">
              {m.summary.map((s, i) => (
                <span key={i} className="flex items-center gap-2">
                  {i > 0 && <span className="text-fg-3">·</span>}
                  <span className={TONE_TEXT[s.tone]}>{s.text}</span>
                </span>
              ))}
            </span>
          </div>

          <div className="mt-4 flex flex-wrap gap-2">
            {t.structured ? (
              <>
                <Stat label="Passed" value={num(t.passed)} tone={t.passed ? 'success' : undefined} />
                <Stat label="Failed" value={num(t.failed)} tone={t.failed ? 'error' : undefined} />
                <Stat label="Skipped" value={num(t.skipped)} />
                {t.other > 0 && (
                  <Stat label="Other" value={num(t.other)} tone="warning" hint="Running, cancelled or did not finish" />
                )}
              </>
            ) : (
              <Stat label="Exit code" value={t.exitCode} tone={t.exitCode ? 'error' : 'success'} />
            )}
            {m.duration && <Stat label="Duration" value={m.duration} />}
          </div>

          <dl className="mt-4 grid grid-cols-[max-content_minmax(0,1fr)] gap-x-6 gap-y-1.5 text-[13px] max-sm:grid-cols-1 max-sm:gap-y-0">
            <Kv label="Status">
              <span className={TONE_TEXT[m.tone]}>{m.status}</span>
            </Kv>
            {t.target && <Kv label="Target">{t.target}</Kv>}
            {(t.commands ?? []).map((c, i) => (
              <Kv key={i} label="Command">
                <span className="inline-flex max-w-full flex-wrap items-center gap-x-2">
                  <code className="rounded-field border border-line bg-surface-2 px-1.5 py-px font-mono text-xs break-all">
                    {c.command}
                  </code>
                  {c.dir && c.dir !== '.' && <span className="text-fg-3">in {c.dir}</span>}
                  <CopyButton text={c.command} title="Copy the command" className="print:hidden" />
                </span>
              </Kv>
            ))}
            {m.duration && (
              <Kv label="Duration">
                <span className="font-mono">{m.duration}</span>
              </Kv>
            )}
            <Kv label="Exit code">
              <span className="font-mono">{t.exitCode}</span>
            </Kv>
            {m.started && <Kv label="Started">{m.started}</Kv>}
          </dl>
          {t.error && (
            <div role="alert" className="alert mt-4 alert-soft alert-warning py-2 text-[13px]">
              <Icon name="warning" className="size-4" />
              <span className="break-words whitespace-pre-wrap">{t.error}</span>
            </div>
          )}
        </header>

        {!t.structured ? (
          <p className="mt-6 rounded-box border border-line bg-base-100 px-4 py-3 text-[13px] text-fg-2">
            <Icon name="info" className="mr-2 size-4 align-[-3px] text-info" />
            The test command did not produce a structured report, so individual test results are not available. The
            outcome is based on the process exit code.
          </p>
        ) : !m.packages.length ? (
          <p className="mt-6 text-[13px] text-fg-2">No tests were reported.</p>
        ) : (
          <section className="mt-6" aria-label="Results by package">
            <div className="mb-3 flex flex-wrap items-center gap-2 print:hidden">
              <h2 className="mr-1 text-sm font-semibold">Results</h2>
              <div className="join" role="group" aria-label="Show tests">
                {BUCKETS.filter((b) => b.id === 'all' || m.counts[b.id as CaseView['bucket']] > 0).map((b) => {
                  const n = b.id === 'all' ? (t.tests?.length ?? 0) : m.counts[b.id as CaseView['bucket']]
                  return (
                    <button
                      key={b.id}
                      type="button"
                      aria-pressed={bucket === b.id}
                      className={cx('btn join-item btn-xs', bucket === b.id && 'btn-active btn-primary')}
                      onClick={() => setBucket(b.id)}
                    >
                      {b.label}
                      <span className="font-mono opacity-70">{num(n)}</span>
                    </button>
                  )
                })}
              </div>
              <label className="input input-sm ml-auto w-64 max-sm:ml-0 max-sm:w-full">
                <Icon name="search" className="size-3.5 text-fg-3" />
                <input
                  ref={searchRef}
                  type="search"
                  placeholder="Filter tests…"
                  aria-label="Filter tests"
                  autoComplete="off"
                  spellCheck={false}
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Escape') {
                      setQuery('')
                      e.currentTarget.blur()
                    }
                  }}
                />
                <kbd className="kbd kbd-xs">/</kbd>
              </label>
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                onClick={() => setClosed(allOpen ? Object.fromEntries(m.packages.map((p) => [p.name, true])) : {})}
              >
                {allOpen ? 'Collapse all' : 'Expand all'}
              </button>
            </div>
            {shown.length === 0 ? (
              <p className="rounded-box border border-line px-4 py-6 text-center text-[13px] text-fg-2">
                No tests match the filter.
              </p>
            ) : (
              <div className="overflow-hidden rounded-box border border-line bg-base-100">
                {shown.map(({ p, outcomes, tests }) => (
                  <Package
                    key={p.name}
                    pkg={p}
                    outcomes={outcomes}
                    tests={tests}
                    open={!closed[p.name] || !!q}
                    onToggle={() => setClosed((c) => ({ ...c, [p.name]: !c[p.name] }))}
                    selected={selected?.key}
                    onSelect={select}
                    onOutput={showOutput}
                  />
                ))}
              </div>
            )}
          </section>
        )}

        {m.failures.length > 0 && (
          <section className="mt-6" aria-label="Failures">
            <h2 className="mb-2 text-sm font-semibold">
              Failures <span className="badge ml-1 badge-soft badge-sm badge-error">{m.failures.length}</span>
            </h2>
            <div className="space-y-3">
              {m.failures.map((c, i) => (
                <Failure key={c.key + '#' + i} c={c} selected={selected?.key === c.key} />
              ))}
            </div>
          </section>
        )}

        {m.logShown > 0 && <Log m={m} openByDefault={!t.structured} />}
      </div>
      <StatusSlot>
        <span className={TONE_TEXT[m.tone]}>Tests {m.status.toLowerCase()}</span>
        {m.summary.map((s, i) => (
          <span key={i}>{s.text}</span>
        ))}
        {m.duration && <span>{m.duration}</span>}
      </StatusSlot>
    </div>
  )
}

/** Name of a Go subtest with its parent dimmed ("TestX/" + "case"). */
function CaseName({ c }: { c: CaseView }) {
  if (c.kind === 'package') return <span>{c.label}</span>
  const i = c.name.lastIndexOf('/')
  if (i <= 0 || !/^Test|^Benchmark|^Example|^Fuzz/.test(c.name))
    return <span className="font-mono text-[12.5px]">{c.label}</span>
  return (
    <span className="font-mono text-[12.5px]">
      <span className="text-fg-3">{c.name.slice(0, i + 1)}</span>
      {c.name.slice(i + 1)}
    </span>
  )
}

function Package({
  pkg,
  outcomes,
  tests,
  open,
  onToggle,
  selected,
  onSelect,
  onOutput,
}: {
  pkg: PackageView
  outcomes: CaseView[]
  tests: CaseView[]
  open: boolean
  onToggle: () => void
  selected?: string
  onSelect: (c: CaseView) => void
  onOutput: (c: CaseView) => void
}) {
  const failed = pkg.counts.failed > 0
  return (
    <div className="border-line not-first:border-t">
      <button
        type="button"
        className="flex w-full items-center gap-2 bg-surface-2 px-3 py-1.5 text-left hover:bg-surface-hover"
        aria-expanded={open}
        onClick={onToggle}
      >
        <Icon
          name="chevronRight"
          className={cx('size-3.5 shrink-0 text-fg-3 transition-transform', open && 'rotate-90')}
        />
        <span
          className={cx('min-w-0 flex-1 truncate font-mono text-xs', failed ? 'text-fg' : 'text-fg-2')}
          title={pkg.name}
        >
          {pkg.name || '(no package)'}
        </span>
        <span className="flex shrink-0 items-center gap-2 font-mono text-[11.5px]">
          {pkg.counts.passed > 0 && <span className="text-success">✓ {pkg.counts.passed}</span>}
          {pkg.counts.failed > 0 && <span className="text-error">✗ {pkg.counts.failed}</span>}
          {pkg.counts.skipped > 0 && <span className="text-fg-3">○ {pkg.counts.skipped}</span>}
          {pkg.counts.other > 0 && <span className="text-warning">! {pkg.counts.other}</span>}
          {pkg.elapsedMs > 0 && <span className="w-16 text-right text-fg-3">{caseDuration(pkg.elapsedMs)}</span>}
        </span>
      </button>
      {open && (
        <ul>
          {[...outcomes, ...tests].map((c, i) => {
            const sel = c.key === selected
            const hasOutput = (c.status === 'failed' || c.status === 'error') && !!c.output
            return (
              <li
                key={c.key + '#' + i}
                data-test-key={c.key}
                className={cx(
                  'group grid grid-cols-[18px_minmax(0,1fr)_auto_auto] items-baseline gap-x-2 border-t border-line/60 px-3 py-0.5 text-[13px]',
                  sel && 'bg-accent-weak shadow-[inset_2px_0_0_var(--color-primary)]',
                )}
              >
                <span className={cx('text-center font-bold', TONE_TEXT[c.tone])} title={c.statusLabel}>
                  {c.glyph}
                </span>
                <button
                  type="button"
                  className="min-w-0 cursor-pointer text-left break-words hover:text-accent-text"
                  title={'Link to this result (' + c.statusLabel + ')'}
                  onClick={() => onSelect(c)}
                >
                  <CaseName c={c} />
                  {c.kind === 'package' && <span className="badge ml-2 badge-ghost badge-xs">package result</span>}
                  {c.status === 'error' && (
                    <span className="badge ml-2 badge-soft badge-xs badge-warning">did not finish</span>
                  )}
                  {c.status === 'cancelled' && (
                    <span className="badge ml-2 badge-soft badge-xs badge-warning">cancelled</span>
                  )}
                </button>
                <span>
                  {hasOutput && (
                    <button
                      type="button"
                      className="btn btn-ghost btn-xs h-5 min-h-5 text-error print:hidden"
                      onClick={() => onOutput(c)}
                    >
                      Output
                    </button>
                  )}
                </span>
                <span className="w-16 text-right font-mono text-xs text-fg-3">{caseDuration(c.elapsedMs)}</span>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

function Failure({ c, selected }: { c: CaseView; selected: boolean }) {
  return (
    <details
      id={'failure-' + c.key}
      open
      className={cx(
        'group scroll-mt-4 overflow-hidden rounded-box border bg-base-100',
        selected ? 'border-error' : 'border-error/40',
      )}
    >
      <summary className="flex cursor-pointer list-none items-center gap-2 bg-surface-2 px-3 py-1.5 text-[13px] [&::-webkit-details-marker]:hidden">
        <Icon name="chevronRight" className="size-3.5 shrink-0 text-fg-3 transition-transform group-open:rotate-90" />
        <span className={cx('font-medium', c.tone === 'error' ? 'text-error' : 'text-warning')}>
          {c.status === 'error' ? 'Did not finish' : 'Failure'}
        </span>
        <span className={cx('min-w-0 truncate', c.kind === 'package' ? '' : 'font-mono')}>{c.label}</span>
        {c.package && <span className="min-w-0 truncate font-mono text-xs text-fg-3">{c.package}</span>}
        <span className="flex-1" />
        <span className="print:hidden" onClick={(e) => e.preventDefault()}>
          <CopyButton text={c.output ?? ''} label="Copy" title="Copy the output" />
        </span>
      </summary>
      <pre className="scroll-thin max-h-[520px] overflow-auto border-t border-line bg-code p-3 font-mono text-xs leading-[19px] [tab-size:4] print:max-h-none print:break-all print:whitespace-pre-wrap">
        {c.output}
      </pre>
    </details>
  )
}

function Log({ m, openByDefault }: { m: TestsModel; openByDefault: boolean }) {
  const [open, setOpen] = useState(openByDefault)
  const name =
    'gitview-tests-' + (m.report.started ? m.report.started.slice(0, 19).replace(/[:T]/g, '-') : 'log') + '.log'
  return (
    <details
      className="group mt-6 overflow-hidden rounded-box border border-line bg-base-100"
      open={open}
      onToggle={(e) => setOpen(e.currentTarget.open)}
    >
      <summary className="flex cursor-pointer list-none items-center gap-2 bg-surface-2 px-3 py-1.5 text-[13px] [&::-webkit-details-marker]:hidden">
        <Icon name="chevronRight" className="size-3.5 shrink-0 text-fg-3 transition-transform group-open:rotate-90" />
        <span className="font-medium">Full log</span>
        <span className="text-fg-3">
          {num(m.logShown)} {m.logShown === 1 ? 'line' : 'lines'}
          {m.logHidden > 0 && <>, last of {num(m.logTotal)}</>}
        </span>
        <span className="flex-1" />
        <span className="flex items-center print:hidden" onClick={(e) => e.preventDefault()}>
          <CopyButton text={() => m.logText} label="Copy" title="Copy the log" />
          <button
            type="button"
            className="btn btn-ghost btn-xs gap-1"
            title="Save the log as a file"
            onClick={() => download(name, m.logText + '\n')}
          >
            <Icon name="download" className="size-3.5" />
            Download
          </button>
        </span>
      </summary>
      {m.logHidden > 0 && (
        <p className="border-t border-line bg-warning/10 px-3 py-1 text-xs text-fg-2">
          The run produced {num(m.logTotal)} lines; the report keeps the last {num(m.logShown)}.
        </p>
      )}
      {open && (
        <pre className="scroll-thin max-h-[520px] overflow-auto border-t border-line bg-code p-3 font-mono text-xs leading-[19px] [tab-size:4] print:max-h-none print:break-all print:whitespace-pre-wrap">
          {m.logText}
        </pre>
      )}
    </details>
  )
}
