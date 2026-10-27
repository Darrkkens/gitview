import { useMemo, useState } from 'react'
import { Icon } from '../../../components/Icon'
import { cx } from '../../../lib/format'
import { setParams } from '../../../lib/router'
import { RequestTimeline } from '../charts'
import { dur, secs, statusTone } from '../fmt'
import type { Analysis, RequestRow } from '../types'
import { BasisBadge, H2, Muted } from '../ui'

type SortKey = 'off' | 'key' | 'status' | 'dur' | 'proxy' | 'sql' | 'sqlTime' | 'cpu' | 'perfSamples' | 'inFlight'

const PAGE = 100

const TONE = { error: 'text-error', warning: 'text-warning', success: 'text-success' }

export function Requests({ a, route }: { a: Analysis; route: string | null }) {
  const R = useMemo(() => a.requests ?? [], [a.requests])
  const [activeOnly, setActiveOnly] = useState(true)
  const [errorsOnly, setErrorsOnly] = useState(false)
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: 'off', desc: false })
  const [page, setPage] = useState(0)
  const keys = useMemo(() => [...new Set(R.map((r) => r.key))].sort(), [R])
  const k = route && keys.includes(route) ? route : null

  const rows = useMemo(
    () =>
      R.filter(
        (r) =>
          (!k || r.key === k) &&
          (!activeOnly || r.phase === 'active') &&
          (!errorsOnly || r.status >= 400 || !!r.error || !!r.panic),
      ),
    [R, k, activeOnly, errorsOnly],
  )
  const sorted = useMemo(() => {
    const out = rows.slice()
    const get = (r: RequestRow): number | string => {
      switch (sort.key) {
        case 'key':
          return r.key
        case 'proxy':
          return r.proxy ?? -1
        case 'inFlight':
          return r.inFlight ?? 0
        case 'perfSamples':
          return r.perfSamples ?? -1
        default:
          return r[sort.key]
      }
    }
    out.sort((x, y) => {
      const p = get(x)
      const q = get(y)
      const c = typeof p === 'string' ? p.localeCompare(q as string) : (p as number) - (q as number)
      return sort.desc ? -c : c
    })
    return out
  }, [rows, sort])

  if (!R.length) return <Muted>No request was observed during this capture.</Muted>
  const hasPerf = !!a.perf

  const pages = Math.max(1, Math.ceil(sorted.length / PAGE))
  const pg = Math.min(page, pages - 1)
  const shown = sorted.slice(pg * PAGE, pg * PAGE + PAGE)
  const th = (label: string, key: SortKey | null, num = false, title?: string) => (
    <th className={cx('font-medium whitespace-nowrap', num && 'text-right')} title={title}>
      {key ? (
        <button
          type="button"
          className={cx('inline-flex items-center gap-0.5 hover:text-fg', sort.key === key && 'text-fg')}
          onClick={() => {
            setSort((s) => ({ key, desc: s.key === key ? !s.desc : key !== 'off' && key !== 'key' }))
            setPage(0)
          }}
        >
          {label}
          {sort.key === key && <Icon name="chevronDown" className={cx('size-3', !sort.desc && 'rotate-180')} />}
        </button>
      ) : (
        label
      )}
    </th>
  )

  return (
    <>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-fg-2">
        <label className="flex items-center gap-2">
          Route
          <select
            className="select select-sm w-auto min-w-48 pr-8 font-mono"
            value={k ?? ''}
            onChange={(e) => {
              setParams({ route: e.target.value || undefined })
              setPage(0)
            }}
          >
            <option value="">all routes</option>
            {keys.map((x) => (
              <option key={x} value={x}>
                {x}
              </option>
            ))}
          </select>
        </label>
        <label className="flex cursor-pointer items-center gap-2">
          <input
            type="checkbox"
            className="toggle toggle-xs toggle-primary"
            checked={activeOnly}
            onChange={(e) => {
              setActiveOnly(e.target.checked)
              setPage(0)
            }}
          />
          active phase only
        </label>
        <label className="flex cursor-pointer items-center gap-2">
          <input
            type="checkbox"
            className="toggle toggle-xs toggle-error"
            checked={errorsOnly}
            onChange={(e) => {
              setErrorsOnly(e.target.checked)
              setPage(0)
            }}
          />
          errors only
        </label>
      </div>

      <H2 aside={<BasisBadge basis="measured" />}>Execution timeline</H2>
      {rows.length ? (
        <RequestTimeline rows={rows} phases={a.phases} onRoute={(key) => setParams({ route: key })} />
      ) : (
        <Muted>No request matches these filters.</Muted>
      )}

      <H2
        aside={
          <span className="text-xs text-fg-3">{rows.length !== R.length ? rows.length + ' of ' + R.length : ''}</span>
        }
      >
        Requests ({rows.length})
      </H2>
      {rows.length > 0 && (
        <div className="scroll-thin overflow-x-auto rounded-box border border-line bg-base-100">
          <table className="table table-xs [&_tr]:text-xs">
            <thead>
              <tr>
                {th('at', 'off', true, 'offset from the start of the capture')}
                {th('route', 'key')}
                {th('path', null)}
                {th('status', 'status', true)}
                {th('duration', 'dur', true)}
                {th('proxy', 'proxy', true, 'duration measured at the proxy')}
                {th('SQL', 'sql', true, 'statements (– when no source of the request sees SQL)')}
                {th('SQL time', 'sqlTime', true)}
                {th('CPU (sampled)', 'cpu', true, 'CPU samples attributed to the request (pprof labels)')}
                {hasPerf &&
                  th(
                    'perf (process)',
                    'perfSamples',
                    true,
                    'Linux perf samples of the whole process taken while the request ran: what the process was doing then, not the CPU of this request',
                  )}
                {th('in flight', 'inFlight', true, 'requests in flight when it started')}
                {th('phase', null)}
                {th('sources', null)}
                {th('id', null)}
              </tr>
            </thead>
            <tbody>
              {shown.map((r) => {
                const tone = statusTone(r.status, r.error)
                return (
                  <tr key={r.id} className={cx('hover:bg-surface-hover', r.phase !== 'active' && 'text-fg-3')}>
                    <td className="text-right font-mono whitespace-nowrap tabular-nums">{secs(r.off)}</td>
                    <td className="font-mono whitespace-nowrap">
                      <button
                        type="button"
                        className="hover:text-primary"
                        title="Show only this route"
                        onClick={() => setParams({ route: r.key })}
                      >
                        {r.key}
                      </button>
                    </td>
                    <td className="max-w-80 truncate font-mono whitespace-nowrap" title={r.path}>
                      {r.path}
                    </td>
                    <td className="text-right whitespace-nowrap">
                      <span className={cx('font-mono font-semibold', TONE[tone])} title={r.error}>
                        {r.status}
                      </span>
                      {r.panic && <span className="badge badge-error badge-xs ml-1">panic</span>}
                      {r.error && !r.panic && (
                        <span className="ml-1 cursor-help text-error" title={r.error}>
                          ⚠
                        </span>
                      )}
                    </td>
                    <td className="text-right font-mono whitespace-nowrap tabular-nums">{dur(r.dur)}</td>
                    <td className="text-right font-mono whitespace-nowrap tabular-nums">
                      {r.proxy ? dur(r.proxy) : ''}
                    </td>
                    <td className="text-right font-mono tabular-nums">{r.sql < 0 ? '–' : r.sql}</td>
                    <td className="text-right font-mono whitespace-nowrap tabular-nums">
                      {r.sql < 0 ? '–' : r.sqlTime ? dur(r.sqlTime) : ''}
                    </td>
                    <td className="text-right font-mono whitespace-nowrap tabular-nums">
                      {r.cpu >= 0 ? dur(r.cpu) : '–'}
                    </td>
                    {hasPerf && (
                      <td className="text-right font-mono tabular-nums">
                        {(r.perfSamples ?? -1) >= 0 ? r.perfSamples : '–'}
                      </td>
                    )}
                    <td className="text-right font-mono tabular-nums">{r.inFlight || ''}</td>
                    <td>{r.phase}</td>
                    <td className="whitespace-nowrap">{(r.sources ?? []).join('+')}</td>
                    <td className="whitespace-nowrap">
                      <code className="font-mono text-[10.5px] text-fg-3" title={r.id}>
                        {r.id.length > 12 ? r.id.slice(0, 12) + '…' : r.id}
                      </code>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      {pages > 1 && (
        <div className="mt-2 flex items-center justify-end gap-2 text-xs text-fg-3">
          <span>
            {pg * PAGE + 1}–{Math.min(sorted.length, pg * PAGE + PAGE)} of {sorted.length}
          </span>
          <div className="join">
            <button
              type="button"
              className="btn btn-xs join-item"
              disabled={pg === 0}
              onClick={() => setPage(pg - 1)}
              aria-label="Previous page"
            >
              <Icon name="chevronLeft" className="size-3.5" />
            </button>
            <span className="btn btn-xs join-item pointer-events-none">
              {pg + 1} / {pages}
            </span>
            <button
              type="button"
              className="btn btn-xs join-item"
              disabled={pg >= pages - 1}
              onClick={() => setPage(pg + 1)}
              aria-label="Next page"
            >
              <Icon name="chevronRight" className="size-3.5" />
            </button>
          </div>
        </div>
      )}
    </>
  )
}
