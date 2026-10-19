// Explorer tree (sidebar). Virtualized: only the rows in view exist in the
// DOM, so the whole repository can be listed. Its expansion is its own;
// picking a row selects the item (and draws it on the canvas when it is
// there); the tree follows the selection made elsewhere.

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { Icon } from '../../components/Icon'
import { debounce } from '../../lib/browser'
import { cx, fmt } from '../../lib/format'
import type { Explorer } from './controller'
import type { Model } from './model'
import type { Selection } from './types'
import { KindDot } from './ui'
import { FUNC } from './vocab'

const ROW = 24

type Row = [number, number] // item, depth

function Match({ text, q }: { text: string; q: string }) {
  const k = q ? text.toLowerCase().indexOf(q.toLowerCase()) : -1
  if (k < 0) return <>{text}</>
  return (
    <>
      {text.slice(0, k)}
      <span className="text-accent-text font-semibold">{text.slice(k, k + q.length)}</span>
      {text.slice(k + q.length)}
    </>
  )
}

export function Tree({ D, ctrl, sel }: { D: Model; ctrl: Explorer; sel: Selection | null }) {
  const { T, ROOT } = D
  const [open, setOpen] = useState(() => new Set([ROOT, ...D.modules.map((m) => m.i)]))
  const [input, setInput] = useState('')
  const [filter, setFilter] = useState('')
  // the keyboard cursor, as a tree item (its row moves with expansions)
  const [cursorItem, setCursorItem] = useState(-1)
  const [scroll, setScroll] = useState({ top: 0, h: 600 })
  const el = useRef<HTMLDivElement>(null)
  const selected = sel?.kind === 'item' ? sel.i : -1
  // follow the selection made elsewhere: open its ancestors, then scroll to it
  const [seen, setSeen] = useState(-1)
  const [scrollTo, setScrollTo] = useState<{ i: number } | null>(null)
  const scrolled = useRef<{ i: number } | null>(null)
  if (selected !== seen) {
    setSeen(selected)
    if (selected >= 0) {
      setScrollTo({ i: selected })
      setCursorItem(selected)
      const anc = D.ancestors(selected)
      if (!filter && !anc.every((x) => open.has(x)))
        setOpen((o) => {
          const n = new Set(o)
          anc.forEach((x) => n.add(x))
          return n
        })
    }
  }

  const applyFilter = useMemo(() => debounce((v: string) => setFilter(v.trim()), 120), [])

  const rows = useMemo<Row[]>(() => {
    const out: Row[] = []
    if (filter) {
      const q = filter.toLowerCase()
      const keep = new Uint8Array(T.length)
      let hits = 0
      for (let i = 0; i < T.length && hits < 3000; i++) {
        const t = T[i]
        if (i === ROOT) continue
        if (t.label.toLowerCase().includes(q) || (t.n && t.n.qual && t.n.qual.toLowerCase().includes(q))) {
          keep[i] = 1
          hits++
          for (const a of D.ancestors(i)) if (!keep[a]) keep[a] = 2
        }
      }
      const stack: Row[] = [[ROOT, 0]]
      while (stack.length) {
        const [i, d] = stack.pop()!
        out.push([i, d])
        const kids = T[i].kids.filter((k) => keep[k])
        for (let k = kids.length - 1; k >= 0; k--) stack.push([kids[k], d + 1])
      }
      return out
    }
    const stack: Row[] = [[ROOT, 0]]
    while (stack.length) {
      const [i, d] = stack.pop()!
      out.push([i, d])
      if (!open.has(i)) continue
      const kids = T[i].kids
      for (let k = kids.length - 1; k >= 0; k--) stack.push([kids[k], d + 1])
    }
    return out
  }, [D, T, ROOT, open, filter])

  const toggle = useCallback((i: number) => {
    setOpen((o) => {
      const n = new Set(o)
      if (n.has(i)) n.delete(i)
      else n.add(i)
      return n
    })
  }, [])

  const pick = (i: number) => void ctrl.selectItem(i, { from: 'tree' })

  useLayoutEffect(() => {
    const box = el.current
    if (!scrollTo || scrolled.current === scrollTo || !box) return
    const k = rows.findIndex((r) => r[0] === scrollTo.i)
    if (k < 0) return
    scrolled.current = scrollTo
    const top = k * ROW,
      vh = box.clientHeight || 600
    if (top < box.scrollTop + ROW || top > box.scrollTop + vh - ROW * 2) box.scrollTop = Math.max(0, top - vh / 3)
  }, [rows, scrollTo])

  useEffect(() => {
    const box = el.current
    if (!box) return
    const ro = new ResizeObserver(() => setScroll({ top: box.scrollTop, h: box.clientHeight }))
    ro.observe(box)
    return () => ro.disconnect()
  }, [])

  const keepCursorVisible = (c: number) => {
    const box = el.current
    if (!box) return
    const top = c * ROW
    if (top < box.scrollTop) box.scrollTop = top
    else if (top + ROW > box.scrollTop + box.clientHeight) box.scrollTop = top + ROW - box.clientHeight
  }

  const cursor = cursorItem >= 0 ? rows.findIndex((r) => r[0] === cursorItem) : -1
  const onKeyDown = (ev: KeyboardEvent<HTMLDivElement>) => {
    if (!rows.length) return
    const c = cursor < 0 ? 0 : cursor
    const [i] = rows[c]
    const t = T[i]
    const isOpen = filter ? true : open.has(i)
    let next = c
    switch (ev.key) {
      case 'ArrowDown':
        next = Math.min(rows.length - 1, c + 1)
        break
      case 'ArrowUp':
        next = Math.max(0, c - 1)
        break
      case 'ArrowRight':
        if (t.kids.length && !isOpen) toggle(i)
        else if (t.kids.length) next = c + 1
        break
      case 'ArrowLeft':
        if (isOpen && t.kids.length && !filter) toggle(i)
        else {
          const k = rows.findIndex((r) => r[0] === t.parent)
          if (k >= 0) next = k
        }
        break
      case 'Enter':
      case ' ':
        pick(i)
        break
      case 'Home':
        next = 0
        break
      case 'End':
        next = rows.length - 1
        break
      default:
        return
    }
    ev.preventDefault()
    ev.stopPropagation()
    setCursorItem(rows[next][0])
    keepCursorVisible(next)
  }

  const a = Math.max(0, Math.floor(scroll.top / ROW) - 8),
    b = Math.min(rows.length, Math.ceil((scroll.top + scroll.h) / ROW) + 8)
  const visible: Row[] = rows.slice(a, b)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-2 pt-2 pb-1.5">
        <label className="input input-sm w-full">
          <Icon name="search" className="size-3.5 opacity-60" />
          <input
            type="search"
            value={input}
            placeholder="Filter…"
            autoComplete="off"
            spellCheck={false}
            aria-label="Filter the tree"
            onChange={(e) => {
              setInput(e.target.value)
              applyFilter(e.target.value)
              if (el.current) el.current.scrollTop = 0
            }}
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                setInput('')
                setFilter('')
                e.currentTarget.blur()
                e.stopPropagation()
              } else if (e.key === 'ArrowDown') {
                el.current?.focus()
                if (rows.length) setCursorItem(rows[0][0])
                e.preventDefault()
              }
            }}
          />
        </label>
      </div>
      <div
        ref={el}
        role="tree"
        aria-label="Repository structure"
        tabIndex={0}
        data-arch-tree=""
        className="scroll-thin group/tree relative min-h-0 flex-1 overflow-x-hidden overflow-y-auto pb-3 outline-none"
        onScroll={(e) => setScroll({ top: e.currentTarget.scrollTop, h: e.currentTarget.clientHeight })}
        onKeyDown={onKeyDown}
      >
        {filter && rows.length <= 1 ? (
          <p className="px-3 py-4 text-xs text-fg-3">Nothing matches “{filter}”.</p>
        ) : (
          <div className="relative" style={{ height: rows.length * ROW }}>
            {visible.map(([i, d], j) => {
              const k = a + j
              const t = T[i]
              const hasKids = t.kids.length > 0
              const isOpen = filter ? true : open.has(i)
              const ext = t.ext || (t.n && t.n.ext)
              return (
                <div
                  key={i}
                  role="treeitem"
                  aria-level={d + 1}
                  aria-selected={i === selected}
                  aria-expanded={hasKids ? isOpen : undefined}
                  title={D.reference(t) + ' · ' + D.kindLabel(t)}
                  className={cx(
                    'absolute right-0 left-0 flex h-6 cursor-pointer items-center gap-1.5 pr-2 text-xs whitespace-nowrap',
                    i === selected ? 'bg-accent-weak text-fg' : 'text-fg-2 hover:bg-surface-hover hover:text-fg',
                    k === cursor && 'group-focus-visible/tree:shadow-[inset_0_0_0_1px_var(--color-primary)]',
                  )}
                  style={{ top: k * ROW, paddingLeft: 6 + d * 14 }}
                  onClick={() => {
                    setCursorItem(i)
                    pick(i)
                  }}
                  onDoubleClick={() => hasKids && !filter && toggle(i)}
                >
                  <span
                    className={cx(
                      'grid size-4 shrink-0 place-items-center rounded-[3px] text-fg-3',
                      hasKids && 'hover:bg-surface-3 hover:text-fg',
                    )}
                    aria-hidden="true"
                    onClick={(e) => {
                      if (!hasKids || filter) return
                      e.stopPropagation()
                      toggle(i)
                    }}
                  >
                    {hasKids && (
                      <Icon name="chevronRight" className={cx('size-3 transition-transform', isOpen && 'rotate-90')} />
                    )}
                  </span>
                  <KindDot color={D.kindColor(t)} round={!!FUNC[t.kind]} />
                  <span className={cx('min-w-0 truncate', ext && 'text-fg-3')}>
                    <Match text={D.displayName(t, true)} q={filter} />
                  </span>
                  {hasKids && t.count > 0 && (
                    <span className="ml-auto pl-1.5 text-[10.5px] text-fg-3 tabular-nums">{fmt(t.count)}</span>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
