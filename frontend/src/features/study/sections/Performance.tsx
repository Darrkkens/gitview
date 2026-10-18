import type { ReactNode } from 'react'
import { useStudy } from '../context'
import { fmtNs, fmtVal } from '../fmt'
import type { PerfFunc, ProfileStudy } from '../types'
import { BasisBadge, Card, Note, TrackedLink } from '../ui'

function FuncName({ id, name }: { id?: string; name: string }) {
  const { links } = useStudy()
  const href = id ? links.inspect(id, 'perf') : null
  if (!href) return <span className="font-mono break-all">{name}</span>
  return (
    <TrackedLink
      href={href}
      area="performance"
      id={id}
      className="link link-hover font-mono break-all text-accent-text"
    >
      {name}
    </TrackedLink>
  )
}

function ProfileTable({ rows, unit, showFound }: { rows: PerfFunc[]; unit: string; showFound: boolean }) {
  return (
    <div className="overflow-x-auto">
      <table className="table table-xs [&_tr]:text-xs">
        <thead>
          <tr>
            <th>function</th>
            <th className="text-right">flat</th>
            <th className="text-right">flat %</th>
            <th className="text-right">cum %</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((f, i) => (
            <tr key={i}>
              <td>
                <FuncName id={f.id} name={f.name} />
              </td>
              {showFound && !f.found ? (
                <td colSpan={3} className="text-fg-3">
                  no samples in this profile (not measured)
                </td>
              ) : (
                <>
                  <td className="text-right font-mono whitespace-nowrap">{fmtVal(f.flat, unit)}</td>
                  <td className="text-right font-mono whitespace-nowrap">{f.flatPct.toFixed(1)}%</td>
                  <td className="text-right font-mono whitespace-nowrap">{f.cumPct.toFixed(1)}%</td>
                </>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Profile({ P, title }: { P: ProfileStudy; title: string }) {
  return (
    <Card>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <BasisBadge basis="RUNTIME" />
        <b className="text-[13px]">
          {title} · <span className="font-mono">{P.source}</span>
        </b>
        <span className="text-xs text-fg-3">
          {P.sampleType} · total {fmtVal(P.total, P.unit)} · {P.samples} samples
          {P.durationMs ? ' · ' + (P.durationMs / 1000).toFixed(1) + ' s' : ''}
        </span>
      </div>
      {P.meaning && <Note className="mt-1">{P.meaning}</Note>}
      {!P.samples && (
        <Note className="mt-1">
          The profile has no samples: nothing was measured (the run may have been shorter than the sampling interval).
        </Note>
      )}
      {!!P.top?.length && (
        <>
          <div className="mt-2 text-xs text-fg-3">Highest flat cost in the whole profile:</div>
          <ProfileTable rows={P.top} unit={P.unit} showFound={false} />
        </>
      )}
      {!!P.report?.length && (
        <>
          <div className="mt-2 text-xs text-fg-3">Functions of the reading order:</div>
          <ProfileTable rows={P.report} unit={P.unit} showFound />
        </>
      )}
    </Card>
  )
}

function Part({ tone, title, children }: { tone: 'obs' | 'sta'; title: ReactNode; children: ReactNode }) {
  return (
    <section className={'mt-4 border-l-2 pl-3 ' + (tone === 'obs' ? 'border-success' : 'border-k-package')}>
      <h3 className="mb-2 flex items-center gap-2 text-sm font-semibold">{title}</h3>
      <div className="space-y-2">{children}</div>
    </section>
  )
}

export function Performance() {
  const { model } = useStudy()
  const PF = model.performance ?? {}
  const O = PF.observed ?? {}
  const ST = PF.static ?? []
  const benchFmt = (unit: string, v: number) =>
    unit === 'ns/op' ? (v < 1000 ? +v.toPrecision(3) + 'ns' : fmtNs(v)) : String(+v.toPrecision(4))
  return (
    <>
      <Note>
        Two separate sections that are never mixed. <BasisBadge basis="RUNTIME" /> measurements: sampled profiles,
        benchmarks and trace regions, valid only for the run that produced them. <BasisBadge basis="STATIC" /> facts
        read from the code and the compiler: loops, decisions, database calls, escapes to the heap. A static fact is not
        a cost, and a function missing from a profile was not measured, which is not the same as cheap.
      </Note>
      <Part
        tone="obs"
        title={
          <>
            <BasisBadge basis="RUNTIME" /> Observed
          </>
        }
      >
        {O.cpu && <Profile P={O.cpu} title="CPU profile" />}
        {O.mem && <Profile P={O.mem} title="Memory profile" />}
        {!!O.benchmarks?.length && (
          <Card>
            <div className="flex flex-wrap items-center gap-2">
              <BasisBadge basis="RUNTIME" />
              <b className="text-[13px]">
                Benchmarks · <span className="font-mono">{O.benchFile ?? ''}</span>
              </b>
            </div>
            <div className="mt-2 overflow-x-auto">
              <table className="table table-xs [&_tr]:text-xs">
                <thead>
                  <tr>
                    <th>benchmark</th>
                    <th>unit</th>
                    <th className="text-right">runs</th>
                    <th className="text-right">median</th>
                    <th className="text-right">min</th>
                    <th className="text-right">max</th>
                  </tr>
                </thead>
                <tbody>
                  {O.benchmarks.map((b, i) => (
                    <tr key={i}>
                      <td className="font-mono break-all">
                        {b.name}
                        {b.procs ? '-' + b.procs : ''}
                      </td>
                      <td>{b.unit}</td>
                      <td className="text-right font-mono">{b.n}</td>
                      <td className="text-right font-mono">{benchFmt(b.unit, b.median)}</td>
                      <td className="text-right font-mono">{benchFmt(b.unit, b.min)}</td>
                      <td className="text-right font-mono">{benchFmt(b.unit, b.max)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Note className="mt-1">
              Values of the last benchmark run in the report; with fewer than about 6 runs (-count) differences between
              them are not reliable.
            </Note>
          </Card>
        )}
        {!!O.regions?.length && (
          <Card>
            <div className="flex flex-wrap items-center gap-2">
              <BasisBadge basis="RUNTIME" />
              <b className="text-[13px]">
                Trace regions · <span className="font-mono">{O.traceFile ?? ''}</span>
              </b>
            </div>
            <div className="mt-2 overflow-x-auto">
              <table className="table table-xs [&_tr]:text-xs">
                <thead>
                  <tr>
                    <th>region</th>
                    <th className="text-right">count</th>
                    <th className="text-right">total</th>
                    <th className="text-right">min</th>
                    <th className="text-right">max</th>
                  </tr>
                </thead>
                <tbody>
                  {O.regions.map((r, i) => (
                    <tr key={i} className="align-top">
                      <td className="font-mono">
                        {r.type}
                        {r.instrumented && (
                          <div className="font-sans text-fg-3">GitView instrumentation: includes its overhead</div>
                        )}
                        {!!r.unfinished && (
                          <div className="font-sans text-fg-3">
                            {r.unfinished} did not end in the trace (not counted)
                          </div>
                        )}
                      </td>
                      <td className="text-right font-mono">{r.count}</td>
                      <td className="text-right font-mono">{fmtNs(r.totalNs)}</td>
                      <td className="text-right font-mono">{fmtNs(r.minNs)}</td>
                      <td className="text-right font-mono">{fmtNs(r.maxNs)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>
        )}
        {!!O.missing?.length && (
          <div>
            <div className="text-xs text-fg-3">Not measured in this report:</div>
            <ul className="mt-1 list-disc space-y-0.5 pl-5 text-xs text-fg-2">
              {O.missing.map((m, i) => (
                <li key={i}>{m}</li>
              ))}
            </ul>
          </div>
        )}
      </Part>
      <Part
        tone="sta"
        title={
          <>
            <BasisBadge basis="STATIC" /> Static (no measurement)
          </>
        }
      >
        {(PF.notes ?? []).map((n, i) => (
          <Note key={i}>{n}</Note>
        ))}
        {!ST.length ? (
          <Note>No function in the reading order.</Note>
        ) : (
          <div className="overflow-x-auto rounded-box border border-line bg-base-100">
            <table className="table table-xs [&_tr]:text-xs">
              <thead>
                <tr>
                  <th>function</th>
                  <th className="text-right">loops</th>
                  <th className="text-right">cyclomatic</th>
                  <th className="text-right">database calls (direct / reachable)</th>
                  <th>compiler: escapes and inlining</th>
                </tr>
              </thead>
              <tbody>
                {ST.map((s) => (
                  <tr key={s.func} className="align-top">
                    <td className="min-w-56">
                      <FuncName id={s.func} name={s.funcName} />
                    </td>
                    <td className="text-right font-mono">{s.loops}</td>
                    <td className="text-right font-mono">{s.cyclomatic}</td>
                    <td className="text-right font-mono">
                      {s.dbOps} / {s.dbReach}
                    </td>
                    <td className="font-mono">
                      {!s.escapeRun ? (
                        <span className="font-sans text-fg-3">not loaded</span>
                      ) : (
                        <>
                          {s.inline && <div>{s.inline}</div>}
                          {(s.escapes ?? []).map((e, i) => (
                            <div key={i}>line {e}</div>
                          ))}
                          {!s.inline && !(s.escapes ?? []).length && (
                            <span className="font-sans text-fg-3">no escape or inlining diagnostic</span>
                          )}
                        </>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Part>
    </>
  )
}
