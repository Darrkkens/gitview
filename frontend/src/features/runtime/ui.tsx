// Building blocks of the runtime report: the basis badges (the honesty
// markers), key/value grids, sections and tables.

import type { ReactNode } from 'react'
import { cx } from '../../lib/format'
import { BASIS_HELP } from './fmt'
import type { Basis } from './types'

const BASIS_CLASS: Record<string, string> = {
  measured: 'badge-success',
  sampled: 'badge-warning',
  estimated: 'badge-warning',
  unavailable: 'badge-ghost text-fg-3',
}

/** BasisBadge says how a value was obtained: measured, sampled, estimated or unavailable. */
export function BasisBadge({ basis, className }: { basis: Basis; className?: string }) {
  return (
    <span
      className={cx(
        'badge badge-outline badge-xs h-4 shrink-0 rounded-full px-1.5 text-[10px] font-medium',
        BASIS_CLASS[basis] ?? 'badge-ghost',
        basis === 'estimated' && 'border-dashed',
        className,
      )}
      title={BASIS_HELP[basis] ? basis + ': ' + BASIS_HELP[basis] : basis}
    >
      {basis}
    </span>
  )
}

export function H2({ children, className, aside }: { children: ReactNode; className?: string; aside?: ReactNode }) {
  return (
    <div className={cx(!/\bmt-/.test(className ?? '') && 'mt-6', 'mb-2 flex flex-wrap items-center gap-2', className)}>
      <h2 className="text-sm font-semibold text-fg">{children}</h2>
      {aside}
    </div>
  )
}

export function H3({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <h3
      className={cx(
        !/\bmt-/.test(className ?? '') && 'mt-4',
        'mb-1.5 flex flex-wrap items-center gap-2 text-xs font-semibold text-fg',
        className,
      )}
    >
      {children}
    </h3>
  )
}

export function Muted({ children, className }: { children: ReactNode; className?: string }) {
  return <p className={cx('text-xs leading-relaxed text-fg-3', className)}>{children}</p>
}

export function Warn({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div role="alert" className={cx('alert alert-warning alert-soft items-start px-3 py-2 text-xs', className)}>
      <span>{children}</span>
    </div>
  )
}

export function Card({
  children,
  className,
  id,
  highlight,
}: {
  children: ReactNode
  className?: string
  id?: string
  highlight?: boolean
}) {
  return (
    <div
      id={id}
      className={cx(
        'rounded-box border border-line bg-base-100 px-4 py-3',
        highlight && 'ring-2 ring-primary/60',
        className,
      )}
    >
      {children}
    </div>
  )
}

/** KV is a grid of labelled values (empty values are skipped). */
export function KV({ rows, className }: { rows: [string, ReactNode][]; className?: string }) {
  const shown = rows.filter(([, v]) => v !== undefined && v !== null && v !== '')
  if (!shown.length) return null
  return (
    <dl className={cx('grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-xs', className)}>
      {shown.map(([k, v], i) => (
        <div key={i} className="contents">
          <dt className="text-fg-3">{k}</dt>
          <dd className="min-w-0 break-words text-fg">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

export interface Column<T> {
  head: ReactNode
  cell: (row: T, i: number) => ReactNode
  num?: boolean
  className?: string
  title?: string
}

/** Table renders rows with columns; numeric columns are right-aligned. */
export function Table<T>({
  columns,
  rows,
  rowKey,
  className,
  rowClass,
}: {
  columns: Column<T>[]
  rows: T[]
  rowKey?: (row: T, i: number) => string | number
  className?: string
  rowClass?: (row: T) => string | undefined
}) {
  return (
    <div className={cx('scroll-thin overflow-x-auto rounded-box border border-line bg-base-100', className)}>
      <table className="table table-xs [&_tr]:text-xs">
        <thead>
          <tr>
            {columns.map((c, i) => (
              <th key={i} className={cx('font-medium whitespace-nowrap', c.num && 'text-right')} title={c.title}>
                {c.head}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={rowKey ? rowKey(r, i) : i} className={cx('align-top', rowClass?.(r))}>
              {columns.map((c, j) => (
                <td key={j} className={cx(c.num && 'text-right font-mono whitespace-nowrap tabular-nums', c.className)}>
                  {c.cell(r, i)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/** Legend swatch of a chart. */
export function Swatch({ color, label, line }: { color: string; label: ReactNode; line?: boolean }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-[11px] text-fg-3">
      <span
        className={line ? 'h-0.5 w-3 rounded' : 'size-2.5 rounded-sm border border-line-strong'}
        style={{ background: color }}
      />
      {label}
    </span>
  )
}
