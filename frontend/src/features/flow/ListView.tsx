// The accessible list view of the current graph: the root's callers and
// callees as nested lists, following the same depth and filters.

import { memo, type ReactNode } from 'react'
import { CONF_LABEL, type Built, type FlowIndex } from './graph'
import type { View } from './types'

export const ListView = memo(function ListView({
  ix,
  g,
  view,
  selected,
  onSelect,
}: {
  ix: FlowIndex
  g: Built
  view: View
  selected: string
  onSelect: (id: string) => void
}) {
  const tree = (id: string, sign: 1 | -1, depth: number, seen: Set<string>): ReactNode => {
    const adj = (sign > 0 ? ix.out.get(id) : ix.in.get(id)) ?? []
    const items = adj.filter((e) => g.vis.get(sign > 0 ? e.to : e.from) === sign * depth)
    if (!items.length) return null
    return (
      <ul className="ml-4 border-l border-line pl-3">
        {items.map((e, i) => {
          const t = sign > 0 ? e.to : e.from
          const n = ix.nodes.get(t)!
          const first = !seen.has(t)
          seen.add(t)
          return (
            <li key={t + i} className="py-px">
              <button
                type="button"
                className={
                  'cursor-pointer text-left font-mono text-[12.5px] hover:text-accent-text ' +
                  (t === selected ? 'font-semibold text-accent-text' : 'text-fg')
                }
                onClick={() => onSelect(t)}
              >
                {(sign > 0 ? '→ ' : '← ') + n.name}
              </button>
              <span className="ml-2 text-[11.5px] text-fg-3">
                {CONF_LABEL[e.conf]}
                {e.kind !== 'call' ? ' · ' + e.kind : ''}
                {n.file ? ' · ' + n.file + ':' + n.line : ''}
              </span>
              {first && tree(t, sign, depth + 1, seen)}
            </li>
          )
        })}
      </ul>
    )
  }
  const root = ix.nodes.get(view.root)
  if (!root) return null
  return (
    <div className="scroll-thin absolute inset-0 overflow-auto bg-base-100 px-4 py-3 text-[13px]">
      <h3 className="font-mono font-semibold">{root.name}</h3>
      {view.dir !== 'callees' && (
        <section className="mt-3">
          <h4 className="text-xs font-semibold text-fg-2">Callers</h4>
          {tree(view.root, -1, 1, new Set()) ?? <p className="ml-4 text-xs text-fg-3">None in this view.</p>}
        </section>
      )}
      {view.dir !== 'callers' && (
        <section className="mt-3">
          <h4 className="text-xs font-semibold text-fg-2">Callees</h4>
          {tree(view.root, 1, 1, new Set()) ?? <p className="ml-4 text-xs text-fg-3">None in this view.</p>}
        </section>
      )}
    </div>
  )
})
