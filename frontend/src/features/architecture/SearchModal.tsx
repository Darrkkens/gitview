// Global search (Ctrl+K or /): every tree item and every HTTP route, ranked
// by how the query matches the name, then the qualified name, then the
// path. Enter opens the result where it lives (a route in the HTTP view).

import { useEffect, useMemo, useRef, useState } from 'react'
import { Icon } from '../../components/Icon'
import { cx } from '../../lib/format'
import { navigate } from '../../lib/router'
import type { Explorer } from './controller'
import { MethodBadge } from './inspector/parts'
import { nodeLoc, type Model } from './model'
import type { TItem } from './types'
import { Kbd, KindDot } from './ui'
import { FUNC } from './vocab'

const KINDS: [string, string][] = [
  ['all', 'All'],
  ['package', 'Packages'],
  ['file', 'Files'],
  ['type', 'Types'],
  ['func', 'Functions'],
  ['route', 'Routes'],
  ['db', 'SQL'],
  ['external', 'External'],
]
const GROUP: Record<string, string> = {
  module: 'package',
  dir: 'package',
  package: 'package',
  file: 'file',
  struct: 'type',
  interface: 'type',
  type: 'type',
  func: 'func',
  method: 'func',
  closure: 'func',
  db: 'db',
  external: 'external',
  unresolved: 'func',
}

type Hit =
  { r: number; i: number; d: number; route?: undefined } | { r: number; route: number; d: number; i?: undefined }

interface Index {
  lower: string[][]
  rlower: string[][]
}

const indexes = new WeakMap<Model, Index>()
function indexOf(D: Model): Index {
  let x = indexes.get(D)
  if (!x) {
    x = {
      lower: D.T.map((t) => [
        t.label.toLowerCase(),
        (t.short || '').toLowerCase(),
        t.n && t.n.qual ? t.n.qual.toLowerCase() : '',
        t.n && t.n.file ? t.n.file.toLowerCase() : '',
        t.n && t.n.type === 'package' ? t.n.name.toLowerCase() : t.kind === 'dir' ? t.name.toLowerCase() : '',
      ]),
      rlower: D.ROUTES.map((r) => [
        (r.method + ' ' + r.path).toLowerCase(),
        r.path.toLowerCase(),
        (r.handler ? r.handler.name : '').toLowerCase(),
      ]),
    }
    indexes.set(D, x)
  }
  return x
}

function groupOf(t: TItem): string {
  if (t.n && t.n.ext) return 'external'
  if (t.kind === 'dir' && t.ext) return 'external'
  return GROUP[t.kind] || ''
}

function rank(L: string[], q: string): number {
  if (L[0] === q || L[1] === q) return 0
  if (L[0].startsWith(q) || L[1].startsWith(q)) return 1
  if (L[0].includes(q)) return 2
  if (L[2].includes(q) || L[4].includes(q)) return 3
  if (L[3].includes(q)) return 4
  return -1
}

function search(D: Model, text: string, kind: string): Hit[] {
  const q = text.trim().toLowerCase()
  if (!q) return []
  const { lower, rlower } = indexOf(D)
  const out: Hit[] = []
  const words = q.split(/\s+/)
  for (let i = 0; i < D.T.length; i++) {
    const t = D.T[i]
    if (i === D.ROOT || t.kind === 'route' || t.kind === 'extgroup') continue
    if (kind !== 'all' && groupOf(t) !== kind) continue
    let r = rank(lower[i], q)
    if (r < 0 && words.length > 1 && words.every((w) => lower[i].some((s) => s.includes(w)))) r = 5
    if (r >= 0) out.push({ r, i, d: t.n ? t.n.deg : 100 })
  }
  if (kind === 'all' || kind === 'route')
    D.ROUTES.forEach((_rt, k) => {
      const L = rlower[k]
      let r =
        L[0] === q || L[1] === q
          ? 0
          : L[0].startsWith(q) || L[1].startsWith(q)
            ? 1
            : L[0].includes(q)
              ? 2
              : L[2].includes(q)
                ? 3
                : -1
      if (r < 0 && words.every((w) => L[0].includes(w))) r = 2
      if (r >= 0) out.push({ r, route: k, d: 50 })
    })
  const labelOf = (x: Hit) => (x.route !== undefined ? D.ROUTES[x.route].path : D.T[x.i].label)
  out.sort(
    (a, b) => a.r - b.r || b.d - a.d || labelOf(a).length - labelOf(b).length || (labelOf(a) < labelOf(b) ? -1 : 1),
  )
  return out.slice(0, 60)
}

function Mark({ text, q }: { text: string; q: string }) {
  const k = q ? text.toLowerCase().indexOf(q) : -1
  if (k < 0) return <>{text}</>
  return (
    <>
      {text.slice(0, k)}
      <mark className="rounded-[2px] bg-primary/25 text-inherit">{text.slice(k, k + q.length)}</mark>
      {text.slice(k + q.length)}
    </>
  )
}

export function SearchModal({ D, ctrl, onClose }: { D: Model; ctrl: Explorer; onClose: () => void }) {
  const [text, setText] = useState('')
  const [kind, setKind] = useState('all')
  const [cur, setCur] = useState(0)
  const input = useRef<HTMLInputElement>(null)
  const list = useRef<HTMLUListElement>(null)
  const hits = useMemo(() => search(D, text, kind), [D, text, kind])
  const kinds = KINDS.filter(([k]) => k !== 'route' || D.ROUTES.length)
  const q = text.trim().toLowerCase()

  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null
    input.current?.focus()
    return () => prev?.focus?.({ preventScroll: true })
  }, [])
  useEffect(() => {
    list.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [cur])

  const pick = (j: number) => {
    const x = hits[j]
    if (!x) return
    onClose()
    if (x.route !== undefined) navigate('http', { route: D.ROUTES[x.route].id })
    else void ctrl.selectItem(x.i, { reveal: true, force: true, from: 'search' })
  }

  return (
    <div className="modal modal-open" role="dialog" aria-modal="true" aria-label="Search">
      <div className="modal-box mt-[12vh] flex self-start max-h-[70vh] w-[min(640px,94vw)] max-w-none flex-col gap-0 overflow-hidden border border-line p-0">
        <label className="flex items-center gap-2 border-b border-line px-3 py-2.5">
          <Icon name="search" className="size-4 text-fg-3" />
          <input
            ref={input}
            type="text"
            value={text}
            placeholder="Search packages, files, types, functions, routes…"
            autoComplete="off"
            spellCheck={false}
            aria-label="Search"
            role="combobox"
            aria-expanded="true"
            aria-controls="arch-search-results"
            aria-activedescendant={hits.length ? 'arch-hit-' + cur : undefined}
            className="min-w-0 flex-1 bg-transparent text-sm outline-none"
            onChange={(e) => {
              setText(e.target.value)
              setCur(0)
            }}
            onKeyDown={(e) => {
              if (e.key === 'ArrowDown') {
                setCur((c) => Math.min(c + 1, hits.length - 1))
                e.preventDefault()
              } else if (e.key === 'ArrowUp') {
                setCur((c) => Math.max(c - 1, 0))
                e.preventDefault()
              } else if (e.key === 'Enter') {
                pick(cur)
                e.preventDefault()
              } else if (e.key === 'Escape') {
                onClose()
                e.preventDefault()
                e.stopPropagation()
              } else if (e.key === 'Tab') {
                const ks = kinds.map(([k]) => k)
                setKind(ks[(ks.indexOf(kind) + (e.shiftKey ? ks.length - 1 : 1)) % ks.length])
                setCur(0)
                e.preventDefault()
              }
            }}
          />
          <Kbd>Esc</Kbd>
        </label>
        <div className="flex flex-wrap gap-1 border-b border-line px-3 py-2" role="group" aria-label="Kinds">
          {kinds.map(([k, label]) => (
            <button
              key={k}
              type="button"
              aria-pressed={kind === k}
              className={cx('btn btn-xs', kind === k ? 'btn-primary btn-soft' : 'btn-ghost')}
              onMouseDown={(e) => {
                e.preventDefault()
                setKind(k)
                setCur(0)
              }}
            >
              {label}
            </button>
          ))}
        </div>
        <ul
          ref={list}
          id="arch-search-results"
          role="listbox"
          className="scroll-thin min-h-0 flex-1 overflow-y-auto p-1.5"
        >
          {!q ? (
            <li className="px-3 py-6 text-center text-xs text-fg-3">
              <div className="mb-1 text-sm font-medium text-fg-2">Search the repository</div>
              Packages, files, types, functions, routes and SQL operations. Use Tab to narrow by kind.
            </li>
          ) : !hits.length ? (
            <li className="px-3 py-6 text-center text-xs text-fg-3">Nothing matches “{text.trim()}”.</li>
          ) : (
            hits.map((x, j) => {
              const active = j === cur
              const cls = cx(
                'flex cursor-pointer items-center gap-2.5 rounded-field px-2.5 py-1.5',
                active && 'bg-accent-weak',
              )
              if (x.route !== undefined) {
                const r = D.ROUTES[x.route]
                return (
                  <li
                    key={'r' + x.route}
                    id={'arch-hit-' + j}
                    role="option"
                    aria-selected={active}
                    className={cls}
                    onMouseDown={(e) => {
                      e.preventDefault()
                      pick(j)
                    }}
                    onMouseMove={() => !active && setCur(j)}
                  >
                    <MethodBadge method={r.method} />
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate font-mono text-xs">
                        <Mark text={r.path} q={q} />
                      </span>
                      <span className="truncate font-mono text-[11px] text-fg-3">
                        {(r.handler ? r.handler.name : 'handler not resolved') + ' · ' + r.file + ':' + r.line}
                      </span>
                    </span>
                    <span className="badge badge-sm badge-ghost">route</span>
                  </li>
                )
              }
              const t = D.T[x.i],
                n = t.n
              const where = n ? (n.file ? nodeLoc(n) : n.pkg || n.name) : t.name
              return (
                <li
                  key={x.i}
                  id={'arch-hit-' + j}
                  role="option"
                  aria-selected={active}
                  title={D.reference(t)}
                  className={cls}
                  onMouseDown={(e) => {
                    e.preventDefault()
                    pick(j)
                  }}
                  onMouseMove={() => !active && setCur(j)}
                >
                  <KindDot color={D.kindColor(t)} round={!!FUNC[t.kind]} />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-sm">
                      <Mark text={D.displayName(t)} q={q} />
                    </span>
                    <span className="truncate font-mono text-[11px] text-fg-3">{where}</span>
                  </span>
                  <span className="badge badge-sm badge-ghost shrink-0">{D.kindLabel(t)}</span>
                </li>
              )
            })
          )}
        </ul>
        <div className="flex flex-wrap gap-4 border-t border-line px-3 py-1.5 text-[11px] text-fg-3">
          <span className="flex items-center gap-1">
            <Kbd>↑</Kbd>
            <Kbd>↓</Kbd> move
          </span>
          <span className="flex items-center gap-1">
            <Kbd>Enter</Kbd> open
          </span>
          <span className="flex items-center gap-1">
            <Kbd>Tab</Kbd> kind
          </span>
          <span className="flex items-center gap-1">
            <Kbd>Esc</Kbd> close
          </span>
        </div>
      </div>
      <div className="modal-backdrop" onMouseDown={onClose} />
    </div>
  )
}
