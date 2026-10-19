// The full Security report: every finding with its evidence, path, code,
// fix and tests, the comparison and the rules, on one printable page. It
// covers the app while open; printing it prints only the report, in the
// light theme.

import { useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'
import { Icon } from '../../components/Icon'
import { cx } from '../../lib/format'
import { Excerpts } from '../http/source'
import { AiResult } from './AiResult'
import { CATS, SEVS, STATUS_LABEL, commitText, dateUTC, type Sec } from './model'
import { Sev, StateBadge } from './parts'

// While the report is open, printing hides the app (the report is a sibling
// of #root under <body>).
const PRINT_ONLY_REPORT = 'print:[&>#root]:hidden'

export function Report({ sec, onClose }: { sec: Sec; onClose: () => void }) {
  const A = sec.page.audit,
    F = sec.F
  const ref = useRef<HTMLDivElement>(null)
  const closeRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    document.body.classList.add(PRINT_ONLY_REPORT)
    closeRef.current?.focus()
    // print in the light theme whatever the screen uses
    let prev: string | undefined
    const before = () => {
      const el = ref.current
      if (!el) return
      prev = el.dataset.theme
      el.dataset.theme = 'gitview-light'
    }
    const after = () => {
      const el = ref.current
      if (!el) return
      if (prev) el.dataset.theme = prev
      else delete el.dataset.theme
    }
    window.addEventListener('beforeprint', before)
    window.addEventListener('afterprint', after)
    return () => {
      document.body.classList.remove(PRINT_ONLY_REPORT)
      window.removeEventListener('beforeprint', before)
      window.removeEventListener('afterprint', after)
    }
  }, [])

  const commit = commitText(sec.page)
  const d = sec.diff
  const cats = [...Object.keys(CATS), ...[...new Set(F.map((f) => f.category))].filter((c) => !CATS[c])]
  return createPortal(
    <div
      ref={ref}
      role="dialog"
      aria-modal="true"
      aria-label="Security report"
      className="fixed inset-0 z-[60] overflow-y-auto bg-base-200 text-base-content print:static print:overflow-visible print:bg-white"
    >
      <div className="mx-auto max-w-[980px] px-6 pb-16 print:max-w-none print:px-0 print:pb-0">
        <div className="sticky top-0 z-[1] -mx-6 mb-5 flex items-center gap-2 border-b border-line bg-base-200/95 px-6 py-2.5 backdrop-blur print:hidden">
          <Icon name="shield" className="size-4 text-primary" />
          <strong className="text-sm">Security report</strong>
          <span className="flex-1" />
          <button type="button" className="btn btn-sm" onClick={() => window.print()}>
            <Icon name="print" className="size-4" />
            Print
          </button>
          <button ref={closeRef} type="button" className="btn btn-ghost btn-sm" onClick={onClose}>
            <Icon name="x" className="size-4" />
            Close
          </button>
        </div>
        <h1 className="mb-1 text-xl font-semibold">Security audit — {A.repository}</h1>
        <p className="text-xs text-fg-2">
          Commit {commit} · analysed {dateUTC(A.generated)} UTC · rules {A.rulesVersion} · GitView{' '}
          {A.analyzerVersion || ''} · analysis {A.status.status}
        </p>
        <p className="mt-3 text-sm leading-relaxed">
          {F.length} findings: {SEVS.map((s) => F.filter((f) => f.severity === s).length + ' ' + s).join(', ')}.
          Findings come from deterministic static analysis rules: each one cites its evidence. A finding is a possible
          problem; its review status records the decision of a person.
        </p>
        <div className="mt-2 space-y-1">
          {(A.status.reasons ?? []).map((r, k) => (
            <p key={k} className="text-xs text-warning">
              Limit: {r}
            </p>
          ))}
          {A.dependencies.note && <p className="text-xs text-fg-2">{A.dependencies.note}</p>}
        </div>

        {cats.map((cat) => {
          const fs = F.filter((f) => f.category === cat)
          if (!fs.length) return null
          return (
            <section key={cat}>
              <h2 className="mt-7 mb-2 border-b border-line pb-1.5 text-base font-semibold break-after-avoid">
                {CATS[cat] ?? cat} ({fs.length})
              </h2>
              {fs.map((f) => {
                const st = sec.stateOf(f)
                const res = sec.ai[f.id]
                return (
                  <article
                    key={f.id}
                    className="my-2.5 rounded-box border border-line bg-base-100 px-3.5 py-3 text-xs leading-relaxed break-inside-avoid print:bg-white"
                  >
                    <h3 className="mb-1.5 flex flex-wrap items-center gap-2 text-sm font-semibold">
                      <Sev sev={f.severity} />
                      <span>{f.title}</span>
                      <span className="badge badge-ghost badge-sm font-mono">{f.ruleId}</span>
                    </h3>
                    <div className="flex flex-wrap items-center gap-2">
                      <StateBadge st={st} />
                      <span className="text-fg-2">
                        {f.verdictLabel} · confidence {f.confidence} · review:{' '}
                        {STATUS_LABEL[sec.status(f)] ?? sec.status(f)}
                      </span>
                    </div>
                    <p className="my-1.5">{f.message}</p>
                    <p className="font-mono text-[11px] text-fg-3">
                      {f.id} · {(f.file || '') + (f.line ? ':' + f.line : '')}
                    </p>
                    {(f.routes ?? []).length > 0 && (
                      <>
                        <p className="mt-1 text-fg-2">
                          <span className="text-fg-3">Routes: </span>
                          {f
                            .routes!.map((r) => r.key)
                            .slice(0, 8)
                            .join(', ') + (f.routes!.length > 8 ? ' …' : '')}
                        </p>
                        <p className="text-fg-2">
                          <span className="text-fg-3">Path: </span>
                          {f.routes![0].path.map((s) => s.name).join(' → ')}
                        </p>
                      </>
                    )}
                    {(f.flow ?? []).length > 0 && (
                      <p className="text-fg-2">
                        <span className="text-fg-3">Data: </span>
                        {f.flow!.map((s) => s.role + ' ' + s.label).join(' → ')}
                      </p>
                    )}
                    <p className="text-fg-2">
                      <span className="text-fg-3">Evidence: </span>
                      {(f.evidence ?? [])
                        .map((e) => e.id + ' ' + e.kind + (e.file ? ' ' + e.file + ':' + e.line : ''))
                        .join('; ')}
                    </p>
                    {f.file && f.line ? (
                      <Excerpts
                        snippets={sec.page.snippets ?? undefined}
                        file={f.file}
                        from={f.line - 4}
                        to={f.line + 4}
                        marks={new Set([f.line])}
                        className="my-2 print:overflow-visible"
                      />
                    ) : null}
                    <p className="mt-1.5">
                      <strong>Fix: </strong>
                      {(f.remediation ?? []).join(' ')}
                    </p>
                    <p>
                      <strong>Tests: </strong>
                      {(f.tests ?? []).join(' ')}
                    </p>
                    {(f.limits ?? []).map((l, k) => (
                      <p key={k} className="text-warning">
                        Limit: {l}
                      </p>
                    ))}
                    {res && (
                      <div className="mt-2">
                        <AiResult res={res} />
                      </div>
                    )}
                  </article>
                )
              })}
            </section>
          )
        })}
        {!F.length && (
          <p className="mt-6 text-sm text-fg-2">
            No findings. The enabled rules found nothing in the analysed code; that is not a proof of security.
          </p>
        )}

        {d && (
          <section>
            <h2 className="mt-7 mb-2 border-b border-line pb-1.5 text-base font-semibold break-after-avoid">
              Comparison with the previous audit
            </h2>
            <p className="text-sm">
              Previous commit {(d.baseCommit || '').slice(0, 12)}: {(d.new ?? []).length} new,{' '}
              {(d.resolved ?? []).length} resolved, {(d.unchanged ?? []).length} unchanged, {(d.changed ?? []).length}{' '}
              changed, {(d.unconfirmed ?? []).length} not confirmed gone.
            </p>
          </section>
        )}

        <section>
          <h2 className="mt-7 mb-2 border-b border-line pb-1.5 text-base font-semibold break-after-avoid">Rules</h2>
          <div className="space-y-1">
            {sec.rules.map((r) => (
              <p key={r.id} className={cx('text-xs', r.enabled ? 'text-fg-2' : 'text-fg-3')}>
                <span className="font-mono text-fg">{r.id}</span> · {r.title} · {r.severity} · {r.basis} ·{' '}
                {r.enabled ? r.findings + ' finding(s)' : 'not run'} — {r.limits}
              </p>
            ))}
          </div>
        </section>
      </div>
    </div>,
    document.body,
  )
}
