// "Find function…": a combobox over the index's functions and routes.
// Functions sharing a name are never picked automatically: the list says
// how many there are and the reader selects the exact one.

import { useId, useMemo, useRef, useState, type RefObject } from 'react'
import { Icon } from '../../components/Icon'
import { cx } from '../../lib/format'
import { exactMatches, search, type FlowIndex } from './graph'
import type { FlowNode } from './types'

export function Search({
  ix,
  onPick,
  onSay,
  inputRef,
}: {
  ix: FlowIndex
  onPick: (n: FlowNode) => void
  onSay: (text: string, warn?: boolean) => void
  inputRef?: RefObject<HTMLInputElement | null>
}) {
  const [value, setValue] = useState('')
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const listId = useId()
  const listRef = useRef<HTMLUListElement>(null)
  const res = useMemo(() => search(ix, value), [ix, value])
  const raw = value.trim()

  const pick = (n: FlowNode) => {
    setOpen(false)
    setActive(-1)
    setValue(n.name)
    onPick(n)
  }
  const move = (i: number) => {
    setActive(i)
    listRef.current?.querySelector(`[data-i="${i}"]`)?.scrollIntoView({ block: 'nearest' })
  }

  return (
    <div className="relative w-64 max-lg:w-full">
      <label className="input input-sm w-full">
        <Icon name="search" className="size-3.5 text-fg-3" />
        <input
          ref={inputRef}
          type="search"
          placeholder="Find function…  (name, package, receiver or file)"
          autoComplete="off"
          spellCheck={false}
          role="combobox"
          aria-label="Find function"
          aria-expanded={open && !!raw}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={active >= 0 ? listId + '-' + active : undefined}
          value={value}
          onChange={(e) => {
            setValue(e.target.value)
            setOpen(true)
            setActive(-1)
          }}
          onFocus={() => value && setOpen(true)}
          onBlur={() => setTimeout(() => setOpen(false), 120)}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setOpen(true)
              move(Math.min(active + 1, res.hits.length - 1))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              move(Math.max(active - 1, 0))
            } else if (e.key === 'Enter') {
              e.preventDefault()
              if (active >= 0 && res.hits[active]) return pick(res.hits[active])
              const exact = exactMatches(ix, raw)
              if (exact.length === 1) pick(exact[0])
              else if (exact.length > 1) {
                onSay(`${exact.length} functions are named “${raw}”. Select one from the list.`, true)
                setOpen(true)
              } else if (res.hits.length === 1) pick(res.hits[0])
              else
                onSay(
                  res.hits.length
                    ? 'Select a function from the list.'
                    : `No function matching “${raw}” in this report’s index.`,
                  true,
                )
            } else if (e.key === 'Escape') {
              if (open) {
                e.stopPropagation()
                setOpen(false)
              } else {
                setValue('')
                e.currentTarget.blur()
              }
            }
          }}
        />
      </label>
      {open && raw && (
        <ul
          ref={listRef}
          id={listId}
          role="listbox"
          className="scroll-thin absolute top-[calc(100%+4px)] left-0 z-40 max-h-[360px] w-[520px] max-w-[90vw] overflow-auto rounded-box border border-line-strong bg-base-100 p-1 shadow-[var(--shadow)]"
        >
          {!res.hits.length && (
            <li className="px-2 py-1.5 text-xs text-fg-2">No function matching “{raw}” in this report’s index.</li>
          )}
          {res.exact > 1 && (
            <li className="px-2 py-1.5 text-xs text-warning">
              {res.exact} functions share this name — select the exact one.
            </li>
          )}
          {res.hits.map((n, i) => (
            <li
              key={n.id}
              id={listId + '-' + i}
              data-i={i}
              role="option"
              aria-selected={i === active}
              className={cx(
                'grid cursor-pointer gap-px rounded-field px-2 py-1',
                i === active ? 'bg-accent-weak' : 'hover:bg-surface-hover',
              )}
              onMouseDown={(e) => {
                e.preventDefault()
                pick(n)
              }}
            >
              <span className="font-mono text-[12.5px] font-semibold break-all text-fg">
                {n.name}
                {n.changed && <span className="badge ml-2 badge-soft badge-xs badge-info align-middle">modified</span>}
              </span>
              <span className="text-[11.5px] break-all text-fg-3">
                {n.pkg || n.router || ''}
                {n.file ? '  ·  ' + n.file + ':' + n.line : ''}
              </span>
            </li>
          ))}
          {res.total > res.hits.length && (
            <li className="px-2 py-1.5 text-xs text-fg-3">{res.total - res.hits.length} more — refine the search.</li>
          )}
        </ul>
      )}
    </div>
  )
}
