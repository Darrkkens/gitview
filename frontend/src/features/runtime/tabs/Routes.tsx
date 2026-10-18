import { useEffect } from 'react'
import { cx } from '../../../lib/format'
import { setParams } from '../../../lib/router'
import { LatencyHistogram } from '../charts'
import { Flame } from '../Flame'
import { bytes, dur, profileValue } from '../fmt'
import type { Analysis, Latency, RouteStats } from '../types'
import { BasisBadge, Card, H2, H3, KV, Muted, Swatch, Table } from '../ui'
import { FuncName, HotPath, Metrics, Statements } from './common'

const statusClass = (code: number) => (code >= 500 ? 'badge-error' : code >= 400 ? 'badge-warning' : 'badge-success')

function RouteCard({ r, highlight, index }: { r: RouteStats; highlight: boolean; index: number }) {
  const L = r.latency
  const lat: [string, Latency][] = [['server/primary', L]]
  if (r.proxy) lat.push(['at the proxy', r.proxy])
  if (r.ttfb) lat.push(['time to first byte', r.ttfb])
  const h = r.route?.handler
  return (
    <Card id={'rt-route-' + index} highlight={highlight} className="scroll-mt-4">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <h3 className="font-mono text-sm font-semibold">{r.key}</h3>
        <span className="text-xs text-fg-3">
          · {r.count} in the active phase
          {r.outside ? ' · ' + r.outside + ' outside (ignored)' : ''} · grouped by {r.keyBasis}
        </span>
        {!!r.errors && <span className="badge badge-error badge-soft badge-sm">{r.errors} errors</span>}
        <span className="ml-auto flex flex-wrap gap-1">
          {Object.entries(r.status ?? {}).map(([code, n]) => (
            <span
              key={code}
              className={cx('badge badge-soft badge-sm font-mono', statusClass(Number(code)))}
              title={n + ' × ' + code}
            >
              {code} × {n}
            </span>
          ))}
        </span>
      </div>
      {r.route && (
        <KV
          className="mt-2"
          rows={[
            [
              'handler',
              <span key="h">
                {h ? (
                  <>
                    <FuncName name={h.name} symbol={h.symbol} />
                    <span className="font-mono text-fg-3">
                      {' '}
                      ({h.file}:{h.line})
                    </span>
                  </>
                ) : (
                  '?'
                )}
                <span className="text-fg-3">
                  {' '}
                  — {r.route.conf}: {r.route.basis}
                </span>
              </span>,
            ],
            [
              'middleware',
              r.route.middleware?.length ? (
                <span key="m" className="flex flex-wrap gap-x-2">
                  {r.route.middleware.map((m, i) => (
                    <FuncName key={i} name={m.name} symbol={m.symbol} />
                  ))}
                </span>
              ) : (
                ''
              ),
            ],
            [
              'registered',
              <span key="r">
                {r.route.router} at <span className="font-mono">{r.route.file + ':' + r.route.line}</span>
              </span>,
            ],
          ]}
        />
      )}
      {!r.count ? (
        <Muted className="mt-2">No request of this route in the active phase.</Muted>
      ) : (
        <>
          <Table<[string, Latency]>
            className="mt-3"
            rows={lat}
            columns={[
              { head: 'latency', cell: ([k]) => <span className="whitespace-nowrap">{k}</span> },
              { head: 'n', num: true, cell: ([, l]) => l.n },
              { head: 'min', num: true, cell: ([, l]) => dur(l.min) },
              { head: 'p50', num: true, cell: ([, l]) => dur(l.p50) },
              { head: 'p90', num: true, cell: ([, l]) => dur(l.p90) },
              { head: 'p99', num: true, cell: ([, l]) => dur(l.p99) },
              { head: 'max', num: true, cell: ([, l]) => dur(l.max) },
              { head: 'mean', num: true, cell: ([, l]) => dur(l.mean) },
            ]}
          />
          <Muted className="mt-1.5">
            {r.latencyBasis} <BasisBadge basis="measured" className="align-middle" />
            {r.count < 100 && <> · With {r.count} requests, p99 is the slowest of them.</>}
          </Muted>
          <div className="mt-3 grid items-start gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)]">
            <KV
              rows={[
                ['rate', r.rate ? r.rate.toFixed(2) + ' req/s in the active phase' : ''],
                [
                  'in flight',
                  'max ' +
                    r.maxInFlight +
                    (r.meanInFlight ? ' · mean ' + r.meanInFlight.toFixed(2) : ' · mean not measured'),
                ],
                ['isolated', r.isolated + ' of ' + r.count + ' overlapped no other request'],
                ['bytes', 'in ' + bytes(r.bytesIn) + ' · out ' + bytes(r.bytesOut) + ' (mean per request)'],
                [
                  'sources',
                  Object.entries(r.sources ?? {})
                    .map(([k, n]) => k + ' ' + n)
                    .join(' · '),
                ],
              ]}
            />
            {(r.durations?.length ?? 0) > 1 && (
              <div className="rounded-box border border-line px-3 py-2">
                <div className="mb-1 flex flex-wrap items-center gap-2 text-xs">
                  <span className="font-semibold">Latency distribution</span>
                  <span className="text-fg-3">{r.durations!.length} requests, active phase</span>
                  <BasisBadge basis="measured" />
                </div>
                <LatencyHistogram durations={r.durations!} latency={L} />
              </div>
            )}
          </div>

          {!!r.metrics?.length && (
            <>
              <H3>Per-request metrics</H3>
              <Metrics rows={r.metrics} />
            </>
          )}

          {r.trace && (
            <>
              <H3>Execution trace (average per request, {r.trace.requests} requests)</H3>
              <KV
                rows={[
                  ['running', dur(r.trace.runningNs)],
                  ['runnable (waiting for a P)', dur(r.trace.runnableNs)],
                  ['waiting', dur(r.trace.waitingNs)],
                  ['syscall', dur(r.trace.syscallNs)],
                  ['GC overlapping', dur(r.trace.gcNs)],
                  ['stop-the-world overlapping', r.trace.stwNs ? dur(r.trace.stwNs) : ''],
                  [
                    'waits by reason',
                    Object.entries(r.trace.waits ?? {})
                      .map(([k, v]) => k + ' ' + dur(v))
                      .join(', '),
                  ],
                ]}
              />
            </>
          )}

          {!r.statements?.length && !r.sqlObserved && (
            <Muted className="mt-3 flex flex-wrap items-center gap-1.5">
              SQL not observed: only GitView's collector (or database spans from OpenTelemetry) sees the statements, so
              their count is unknown, not zero. <BasisBadge basis="unavailable" />
            </Muted>
          )}
          {!r.statements?.length && r.sqlObserved && (
            <Muted className="mt-3">No SQL statement was observed in these requests.</Muted>
          )}
          {!!r.statements?.length && (
            <>
              <H3>
                SQL — {(100 * (r.sqlShare || 0)).toFixed(0)}% of request time <BasisBadge basis="measured" />
              </H3>
              <Statements rows={r.statements} />
            </>
          )}

          {!!r.hotPath?.length && (
            <>
              <H3>
                Critical path (CPU samples of these requests) <BasisBadge basis="sampled" />
              </H3>
              <HotPath path={r.hotPath} />
            </>
          )}

          {r.flame && r.flame.value > 0 && (
            <>
              <H3>
                CPU flame graph of this route <BasisBadge basis="sampled" />
                <span className="flex gap-2 font-normal">
                  <Swatch
                    color="color-mix(in oklab, var(--k-unresolved) 50%, var(--color-base-100))"
                    label="module code"
                  />
                  <Swatch color="color-mix(in oklab, var(--fg-3) 30%, var(--color-base-100))" label="other code" />
                </span>
              </H3>
              <Flame root={r.flame} fmt={(v) => profileValue('nanoseconds', v)} />
            </>
          )}
        </>
      )}
    </Card>
  )
}

export function Routes({ a, route }: { a: Analysis; route: string | null }) {
  const RS = a.routes ?? []
  const idx = route ? RS.findIndex((r) => r.key === route) : -1

  useEffect(() => {
    if (idx < 0) return
    document.getElementById('rt-route-' + idx)?.scrollIntoView({ block: 'start' })
  }, [idx])

  if (!RS.length) return <Muted>No request was observed during this capture.</Muted>
  return (
    <>
      {RS.length > 1 && (
        <div className="mb-3 flex flex-wrap items-center gap-1 text-xs">
          <span className="mr-1 text-fg-3">Routes:</span>
          {RS.map((r, i) => (
            <button
              key={r.key}
              type="button"
              className={cx('btn btn-xs font-mono', i === idx ? 'btn-primary btn-soft' : 'btn-ghost')}
              onClick={() => setParams({ route: i === idx ? undefined : r.key })}
            >
              {r.key}
              <span className="text-fg-3">{r.count}</span>
            </button>
          ))}
        </div>
      )}
      <div className="space-y-3">
        {RS.map((r, i) => (
          <RouteCard key={r.key} r={r} index={i} highlight={i === idx} />
        ))}
      </div>
      {!!a.background?.length && (
        <>
          <H2>SQL outside requests (background work)</H2>
          <Statements rows={a.background} />
        </>
      )}
    </>
  )
}
