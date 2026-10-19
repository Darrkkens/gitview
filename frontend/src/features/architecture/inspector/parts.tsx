// Parts shared by the inspector's panels: the head (badges, title, location,
// actions), tabs, clickable rows of elements and of relations.

import type { ReactNode } from 'react'
import { cx, fmt } from '../../../lib/format'
import type { Model } from '../model'
import { ConfBadge, KindDot } from '../ui'
import { FUNC, methodClass, relLabel } from '../vocab'
import { evidenceText } from './helpers'

export function Head({
  badges,
  title,
  loc,
  actions,
}: {
  badges: ReactNode
  title: ReactNode
  loc?: string
  actions?: ReactNode
}) {
  return (
    <div className="shrink-0 border-b border-line px-3 pt-2.5 pb-2">
      <div className="mb-1.5 flex flex-wrap items-center gap-1.5">{badges}</div>
      <div className="text-[15px] leading-snug font-semibold break-words [overflow-wrap:anywhere]">{title}</div>
      {loc && (
        <div className="mt-1 truncate font-mono text-[11.5px] text-fg-3 [direction:rtl]" title={loc}>
          {'‎' + loc + '‎'}
        </div>
      )}
      {actions && <div className="mt-2 flex flex-wrap gap-1">{actions}</div>}
    </div>
  )
}

export function KindBadge({ children, title }: { children: ReactNode; title?: string }) {
  return (
    <span className="badge badge-sm badge-ghost rounded-[4px] lowercase" title={title}>
      {children}
    </span>
  )
}

export function Tabs<T extends string>({ list, cur, set }: { list: [T, string, number?][]; cur: T; set: (t: T) => void }) {
  return (
    <div
      role="tablist"
      className="tabs tabs-border tabs-sm shrink-0 flex-nowrap overflow-x-auto border-b border-line px-1"
    >
      {list.map(([id, label, count]) => (
        <button
          key={id}
          type="button"
          role="tab"
          aria-selected={cur === id}
          className={cx('tab gap-1.5 text-xs', cur === id && 'tab-active')}
          onClick={() => set(id)}
        >
          {label}
          {count !== undefined && <span className="text-[10.5px] text-fg-3">{fmt(count)}</span>}
        </button>
      ))}
    </div>
  )
}

/** Row: an element of the dataset, clickable (it navigates to it). */
export function Row({
  D,
  i,
  sub,
  right,
  onClick,
}: {
  D: Model
  i: number
  sub?: ReactNode
  right?: ReactNode
  onClick: () => void
}) {
  const t = D.T[i]
  return (
    <button
      type="button"
      className="flex w-full min-w-0 cursor-pointer items-center gap-2 rounded-field px-1.5 py-1 text-left text-xs hover:bg-surface-hover focus-visible:bg-surface-hover"
      title={D.reference(t)}
      onClick={onClick}
    >
      <KindDot color={D.kindColor(t)} round={!!FUNC[t.kind]} />
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate">{D.displayName(t)}</span>
        {sub && <span className="truncate font-mono text-[11px] text-fg-3">{sub}</span>}
      </span>
      {right}
    </button>
  )
}

export function MethodBadge({ method }: { method: string }) {
  return (
    <span
      className={cx(
        'inline-block min-w-11 shrink-0 rounded-[4px] text-center font-mono text-[10px] leading-[18px] font-bold',
        methodClass(method),
      )}
    >
      {method}
    </span>
  )
}

/** RelationRow: "from → to" with the kind, evidence and confidence of a relation. */
export function RelationRow({ D, ei, onClick }: { D: Model; ei: number; onClick: () => void }) {
  const e = D.E[ei]
  return (
    <button
      type="button"
      className="flex w-full min-w-0 items-center gap-2 rounded-field px-1.5 py-1 text-left text-xs hover:bg-surface-hover"
      title={D.reference(D.T[e.s]) + ' → ' + D.reference(D.T[e.t])}
      onClick={onClick}
    >
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate font-mono">{D.displayName(D.T[e.s]) + ' → ' + D.displayName(D.T[e.t])}</span>
        <span className="truncate font-mono text-[11px] text-fg-3">{relLabel(e.type) + ' · ' + evidenceText(e)}</span>
      </span>
      <ConfBadge conf={e.conf} />
    </button>
  )
}
