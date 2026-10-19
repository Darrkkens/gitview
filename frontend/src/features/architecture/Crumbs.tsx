// Breadcrumbs: Repository › module › directory › package (the scope), then
// the focus target or the selection inside the scope.

import type { ReactNode } from 'react'
import { cx } from '../../lib/format'
import type { Explorer, ExplorerState } from './controller'
import type { Model } from './model'
import { KindDot } from './ui'

interface Crumb {
  key: string
  text: string
  title: string
  color: string
  rel?: boolean
  onClick?: () => void
}

export function Crumbs({ D, ctrl, S }: { D: Model; ctrl: Explorer; S: ExplorerState }) {
  const items: Crumb[] = []
  D.path(S.root).forEach((a) => {
    items.push({
      key: 's' + a,
      text: a === D.ROOT ? 'Repository' : D.displayName(D.T[a], true),
      title: a === D.ROOT ? D.raw.repository : D.reference(D.T[a]),
      color: D.kindColor(D.T[a]),
      onClick: () => (a !== S.root ? ctrl.enterAncestor(a, S.root) : ctrl.clearSelection()),
    })
  })
  if (S.focus) {
    const t = D.T[S.focus.i]
    items.push({
      key: 'f',
      text: 'Focus: ' + D.displayName(t),
      title: 'Leave focus mode',
      color: D.kindColor(t),
      onClick: () => ctrl.exitFocus(),
    })
  } else if (S.sel?.kind === 'item' && D.isAncestor(S.root, S.sel.i)) {
    D.path(S.sel.i)
      .filter((a) => D.isAncestor(S.root, a))
      .forEach((a) =>
        items.push({
          key: 'r' + a,
          text: D.displayName(D.T[a], true),
          title: D.reference(D.T[a]),
          color: D.kindColor(D.T[a]),
          rel: true,
          onClick: () => void ctrl.selectItem(a, { reveal: true }),
        }),
      )
  }
  const out: ReactNode[] = []
  items.forEach((c, k) => {
    if (k)
      out.push(
        <span key={'sep' + k} aria-hidden="true" className="shrink-0 text-[11px] text-fg-3">
          ›
        </span>,
      )
    const last = k === items.length - 1
    out.push(
      <button
        key={c.key}
        type="button"
        title={c.title}
        aria-current={last ? 'page' : undefined}
        onClick={c.onClick}
        className={cx(
          'flex min-w-7 shrink items-center gap-1.5 truncate rounded-field px-1.5 py-0.5 text-xs hover:bg-surface-3 hover:text-fg',
          last ? 'max-w-64 font-medium text-fg' : c.rel ? 'max-w-48 text-fg-3' : 'max-w-48 text-fg-2',
        )}
      >
        <KindDot color={c.color} className="size-[7px]" />
        <span className="truncate">{c.text}</span>
      </button>,
    )
  })
  return (
    <nav aria-label="Location" className="flex min-w-0 flex-1 items-center gap-0.5 overflow-hidden">
      {out}
    </nav>
  )
}
