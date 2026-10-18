import { useState } from 'react'
import { cx, loc } from '../../../lib/format'
import { useStudy } from '../context'
import { track } from '../progress'
import type { MatrixFunc } from '../types'
import { ActionLink, Actions, BasisBadge, Card, Empty, LocLink, Note, Tag } from '../ui'
import { Icon } from '../../../components/Icon'

function testTone(status: string): 'success' | 'error' | 'warning' | undefined {
  const s = status.toLowerCase()
  if (s.startsWith('pass')) return 'success'
  if (s.startsWith('fail')) return 'error'
  if (s.startsWith('skip')) return 'warning'
  return undefined
}

function MatrixCard({ f }: { f: MatrixFunc }) {
  const { links } = useStudy()
  const [open, setOpen] = useState(false)
  const tests = f.tests ?? []
  const rows = f.rows ?? []
  const unobserved = f.unobserved ?? []
  return (
    <Card>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span className="font-mono text-[13px] font-semibold break-all">{f.funcName}</span>
        <span className="text-xs text-fg-3">cyclomatic complexity {f.cyclomatic}</span>
        {f.coverage && (
          <span className="flex items-center gap-1 text-xs text-fg-2">
            <BasisBadge basis="COVERAGE" />
            {f.coverage}
            {f.stale && <span className="text-warning">(older than the code: not used)</span>}
          </span>
        )}
        <LocLink file={f.file} line={f.line} href={links.diff(f.file, f.line)} className="ml-auto" />
      </div>
      {!f.coverage && (
        <Note className="mt-1">
          No coverage profile in this report: run the package tests with coverage (Branch Explorer, key{' '}
          <kbd className="kbd kbd-xs">c</kbd>, in the TUI) or pass <code className="font-mono">--cover cover.out</code>{' '}
          to <code className="font-mono">gitview inspect report</code> /{' '}
          <code className="font-mono">gitview study</code>.
        </Note>
      )}
      <div className="mt-2 text-xs font-medium text-fg-3">Tests that reach this function</div>
      <ul className="mt-1 space-y-1 text-xs">
        {!tests.length && <li className="text-fg-3">No test reaches this function in the call graph.</li>}
        {tests.map((t, i) => (
          <li key={i} className="flex flex-wrap items-center gap-1.5">
            <BasisBadge basis={t.basis} />
            <span className="font-mono font-semibold">{t.name}</span>
            <span className="text-fg-2">
              {t.hops ? 'reaches it in ' + t.hops + ' call(s) from ' + t.via : 'calls it directly: ' + t.via}
            </span>
            <span className="font-mono text-fg-3">{loc(t)}</span>
            {t.status && (
              <span className="flex items-center gap-1">
                <BasisBadge basis="RUNTIME" />
                <span className="text-fg-3">last run:</span>
                <Tag tone={testTone(t.status)}>{t.status}</Tag>
              </span>
            )}
          </li>
        ))}
      </ul>
      {rows.length > 0 && (
        <>
          <div className="mt-2 overflow-x-auto">
            <table className="table table-xs [&_tr]:text-xs">
              <thead>
                <tr>
                  <th>Scenario</th>
                  <th>Status</th>
                  <th className="text-right">Executions</th>
                  <th>Line</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r, i) => (
                  <tr key={i} className="align-top">
                    <td className="font-mono break-all">{r.scenario}</td>
                    <td className="whitespace-nowrap">
                      {r.status === 'NO DATA' ? (
                        <span className="text-fg-3">no data</span>
                      ) : (
                        <span className="flex items-center gap-1">
                          <BasisBadge basis="COVERAGE" />
                          <span
                            className={cx('font-semibold', r.status === 'OBSERVED' ? 'text-success' : 'text-error')}
                          >
                            {r.status}
                          </span>
                        </span>
                      )}
                    </td>
                    <td className="text-right font-mono">{r.count ? String(r.count) : ''}</td>
                    <td>
                      <LocLink file={r.file} line={r.line} href={links.diff(r.file, r.line)} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Note className="mt-1">
            Branches: {rows.length} · observed: {f.observed} · not observed: {f.notObserved}
            {f.noData ? ' · no data: ' + f.noData : ''}
          </Note>
        </>
      )}
      {unobserved.length > 0 && (
        <>
          <Actions>
            <button
              type="button"
              className={cx('btn btn-xs', open ? 'btn-primary btn-soft' : 'btn-ghost')}
              aria-expanded={open}
              onClick={() => {
                setOpen((o) => !o)
                track('tests', f.func)
              }}
            >
              <Icon name={open ? 'chevronDown' : 'chevronRight'} className="size-3.5" />
              {open ? 'Hide' : 'Show'} unobserved branches ({unobserved.length})
            </button>
            <ActionLink
              label="Open Branch Explorer"
              icon="microscope"
              href={links.inspect(f.func, 'branches')}
              area="tests"
              id={f.func}
            />
          </Actions>
          {open && (
            <div className="mt-2 space-y-2">
              {unobserved.map((u, i) => (
                <div key={i}>
                  <div className="font-mono text-xs text-fg-2">
                    {u.scenario} <span className="text-fg-3">(line {u.line})</span>
                  </div>
                  <div className="mt-1 border-l-2 border-error/70 bg-surface-2 px-2.5 py-1.5 font-mono text-xs leading-5 whitespace-pre-wrap">
                    {u.path.join('\n ↓ ')}
                  </div>
                </div>
              ))}
            </div>
          )}
        </>
      )}
      {f.limited && <Note className="mt-1">{f.limited}</Note>}
    </Card>
  )
}

export function Tests() {
  const { model } = useStudy()
  const funcs = model.testMatrix ?? []
  return (
    <>
      <Note className="mb-3">
        Two different kinds of facts, never mixed: which tests <BasisBadge basis="STATIC" /> reach a function (read from
        the test source and the call graph; <BasisBadge basis="HEURISTIC" /> when only a method name matches), and which
        outcomes <BasisBadge basis="COVERAGE" /> a coverage profile shows ran. The profile covers a whole test run, so
        it does not say which test took which branch.
      </Note>
      {!funcs.length && <Empty>No function of the report has decisions or tests reaching it.</Empty>}
      <div className="space-y-2">
        {funcs.map((f) => (
          <MatrixCard key={f.func} f={f} />
        ))}
      </div>
    </>
  )
}
