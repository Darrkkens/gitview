// Pieces shared by the Routes and Profiles tabs: SQL statements, the
// critical path, metrics and the top functions of a profile.

import { useState } from 'react'
import { Icon } from '../../../components/Icon'
import { cx } from '../../../lib/format'
import { dur, metric, profileValue } from '../fmt'
import { useCodeLink } from '../links'
import type { FuncStat, Metric, PathStep, ProfileReport, StatementStats } from '../types'
import { BasisBadge, Table } from '../ui'

export function FuncName({ name, symbol, muted }: { name: string; symbol?: string; muted?: boolean }) {
  const link = useCodeLink()
  const href = link(symbol)
  if (href)
    return (
      <a href={href} className="link link-hover font-mono break-all text-accent-text" title="Open in Code Flow">
        {name}
      </a>
    )
  return <span className={cx('font-mono break-all', muted && 'text-fg-3')}>{name}</span>
}

export function Statements({ rows }: { rows: StatementStats[] }) {
  return (
    <Table<StatementStats>
      rows={rows}
      columns={[
        { head: 'count', num: true, cell: (s) => s.count },
        { head: 'per req', num: true, cell: (s) => (s.perRequest ? +s.perRequest.toFixed(2) : '') },
        {
          head: 'statement',
          className: 'min-w-80',
          cell: (s) => (
            <code className="block font-mono text-[11px] leading-4 break-words whitespace-pre-wrap text-fg">
              {s.query}
              {!!s.errors && <span className="ml-1 badge badge-error badge-xs">{s.errors} errors</span>}
              {!!s.unfinished && <span className="ml-1 badge badge-warning badge-xs">{s.unfinished} unfinished</span>}
            </code>
          ),
        },
        { head: 'total', num: true, cell: (s) => dur(s.total) },
        { head: 'mean', num: true, cell: (s) => dur(s.mean) },
        { head: 'max', num: true, cell: (s) => dur(s.max) },
        { head: 'rows', num: true, cell: (s) => (s.rows ? +s.rows.toFixed(1) : '') },
        {
          head: 'code',
          className: 'min-w-48',
          cell: (s) =>
            s.match ? (
              <span>
                {s.match.func?.name ? <FuncName name={s.match.func.name} symbol={s.match.func.symbol} /> : null}
                {s.match.func?.file && (
                  <span className="font-mono text-fg-3">
                    {' '}
                    ({s.match.func.file}:{s.match.func.line})
                  </span>
                )}
                <span className="text-fg-3">
                  {' '}
                  [{s.match.conf}: {s.match.basis}]
                </span>
              </span>
            ) : s.site ? (
              <span className="text-fg-3">
                call site <span className="font-mono">{s.site}</span> (not matched)
              </span>
            ) : (
              <span className="text-fg-3">SQL query not statically resolved</span>
            ),
        },
      ]}
    />
  )
}

/** HotPath lists the heaviest chain of frames; leading frames outside the module are folded. */
export function HotPath({ path }: { path: PathStep[] }) {
  const [open, setOpen] = useState(false)
  let lead = 0
  while (lead < path.length && !path[lead].symbol) lead++
  if (lead === path.length) lead = 0
  const fold = lead > 1 && !open
  const shown = fold ? path.slice(lead) : path
  return (
    <ol className="space-y-0.5 rounded-box border border-line bg-base-100 px-3 py-2 font-mono text-xs">
      {lead > 1 && (
        <li>
          <button
            type="button"
            className="inline-flex items-center gap-1 text-fg-3 hover:text-fg"
            onClick={() => setOpen((o) => !o)}
          >
            <Icon name={open ? 'chevronDown' : 'chevronRight'} className="size-3" />
            {open
              ? 'hide the frames outside the module'
              : '… ' + lead + ' frames outside the module (down to ' + path[lead - 1].name + ')'}
          </button>
        </li>
      )}
      {shown.map((s, i) => (
        <li key={i} className="flex items-center gap-2">
          <span className="w-12 shrink-0 text-right text-fg-3 tabular-nums">{s.pct.toFixed(1)}%</span>
          <span className="h-1.5 w-16 shrink-0 overflow-hidden rounded-full bg-surface-3">
            <span className="block h-full bg-warning" style={{ width: Math.min(100, s.pct) + '%' }} />
          </span>
          <FuncName name={s.name} symbol={s.symbol} muted={!s.symbol} />
        </li>
      ))}
    </ol>
  )
}

export function Metrics({ rows }: { rows: Metric[] }) {
  return (
    <Table<Metric>
      rows={rows}
      columns={[
        { head: 'metric', cell: (m) => m.name },
        {
          head: 'value',
          num: true,
          cell: (m) =>
            m.basis === 'unavailable' ? (
              <span className="font-sans text-fg-3">unavailable</span>
            ) : (
              metric(m.value, m.unit)
            ),
        },
        { head: 'basis', cell: (m) => <BasisBadge basis={m.basis} /> },
        { head: 'scope', cell: (m) => <span className="text-fg-2">{m.scope}</span> },
        { head: 'how', className: 'min-w-64', cell: (m) => <span className="text-fg-3">{m.note || ''}</span> },
      ]}
    />
  )
}

/** TopFuncs lists the module functions first, then by cumulative cost. */
export function TopFuncs({ rep, n }: { rep: ProfileReport; n: number }) {
  const fs = (rep.funcs ?? [])
    .slice()
    .sort((a, b) => (!!a.symbol !== !!b.symbol ? (a.symbol ? -1 : 1) : b.cum - a.cum))
    .slice(0, n)
  if (!fs.length) return null
  return (
    <Table<FuncStat>
      className="mt-2"
      rows={fs}
      columns={[
        { head: 'function', cell: (f) => <FuncName name={f.name} symbol={f.symbol} muted={!f.symbol} /> },
        { head: 'flat', num: true, cell: (f) => profileValue(rep.unit, f.flat) },
        { head: 'cum', num: true, cell: (f) => profileValue(rep.unit, f.cum) },
        { head: 'cum %', num: true, cell: (f) => f.cumPct.toFixed(1) + ' %' },
        {
          head: 'file',
          cell: (f) => <span className="font-mono break-all text-fg-3">{f.file ? f.file + (f.line ? ':' + f.line : '') : ''}</span>,
        },
      ]}
    />
  )
}
