// Endpoints (sidebar): every route the analysis found, grouped by module and
// method (or path prefix, handler package, registration file), with a
// search, method chips and authentication / partial-contract filters.

import { useEffect, useMemo, useRef, type RefObject } from 'react'
import { Icon } from '../../components/Icon'
import { PaneHeader } from '../../components/Panes'
import { baseName, cx, fmt } from '../../lib/format'
import { handlerIndex, moduleOf, relPackage, type Graph } from './graph'
import type { Route } from './types'
import { METHODS, methodClass } from './helpers'
import { Lock, Method, PathText } from './ui'

export interface ListState {
  q: string
  methods: Record<string, boolean>
  group: 'module' | 'prefix' | 'package' | 'file'
  filter: '' | 'auth' | 'noauth' | 'partial'
}

const GROUPS: [ListState['group'], string][] = [
  ['module', 'Module · method'],
  ['prefix', 'Path prefix'],
  ['package', 'Handler package'],
  ['file', 'Registration file'],
]
const FILTERS: [ListState['filter'], string][] = [
  ['', 'All endpoints'],
  ['auth', 'Authentication recognised'],
  ['noauth', 'No authentication'],
  ['partial', 'Partial contracts'],
]

function matches(r: Route, s: ListState): boolean {
  const anyM = Object.values(s.methods).some(Boolean)
  if (anyM && !s.methods[r.method]) return false
  if (s.filter === 'auth' && !r.auth?.recognized) return false
  if (s.filter === 'noauth' && r.auth?.recognized) return false
  if (s.filter === 'partial' && r.status === 'complete') return false
  const t = s.q.trim().toLowerCase()
  if (!t) return true
  const hay = (
    r.method +
    ' ' +
    r.path +
    ' ' +
    r.template +
    ' ' +
    (r.handler ? r.handler.name : '') +
    ' ' +
    r.group
  ).toLowerCase()
  return t.split(/\s+/).every((w) => hay.includes(w))
}

const SEV_TEXT: Record<string, string> = {
  critical: 'text-sev-critical bg-sev-critical/10',
  high: 'text-sev-high bg-sev-high/10',
  medium: 'text-sev-medium bg-sev-medium/10',
  low: 'text-sev-low bg-sev-low/10',
  info: 'text-sev-info bg-sev-info/10',
}

const mOrder = (m: string) => {
  const k = METHODS.indexOf(m)
  return k < 0 ? 99 : k
}

export function Endpoints({
  g,
  current,
  state,
  setState,
  onOpen,
  inputRef,
  findingsOf,
}: {
  g: Graph
  current: number
  state: ListState
  setState: (s: ListState) => void
  onOpen: (k: number) => void
  inputRef: RefObject<HTMLInputElement | null>
  /** findings of the security audit reaching a route (when the page has one) */
  findingsOf?: (routeId: string) => { n: number; worst: string } | undefined
}) {
  const routes = g.routes
  const present = useMemo(() => {
    const p: Record<string, number> = {}
    routes.forEach((r) => (p[r.method] = (p[r.method] || 0) + 1))
    return p
  }, [routes])
  const chips = METHODS.concat(Object.keys(present).filter((m) => !METHODS.includes(m))).filter((m) => present[m])

  const groupKeys = useMemo(() => {
    const repo = g.raw.repository
    return (r: Route, k: number): [string, string] => {
      if (state.group === 'prefix') return [r.group || '/', '']
      if (state.group === 'file') return [r.file, '']
      if (state.group === 'package') {
        const h = handlerIndex(g, r)
        return [h >= 0 && g.N[h].pkg ? relPackage(g, g.N[h].pkg) : '(unresolved handler)', '']
      }
      const n = g.nodeOfRoute[k]
      const m = n >= 0 ? moduleOf(g, g.N[n].pkg) : ''
      return [m ? baseName(m) || m : repo, r.method]
    }
  }, [g, state.group])

  const tree = useMemo(() => {
    const t = new Map<string, Map<string, number[]>>()
    let shown = 0
    routes.forEach((r, k) => {
      if (!matches(r, state)) return
      shown++
      const [g1, g2] = groupKeys(r, k)
      if (!t.has(g1)) t.set(g1, new Map())
      const sub = t.get(g1)!
      if (!sub.has(g2)) sub.set(g2, [])
      sub.get(g2)!.push(k)
    })
    return { t, shown }
  }, [routes, state, groupKeys])

  const listRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    listRef.current?.querySelector('[aria-current="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [current])

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PaneHeader title="Endpoints">
        <span className="text-[11px] text-fg-3 tabular-nums">
          {tree.shown === routes.length ? fmt(routes.length) : fmt(tree.shown) + ' / ' + fmt(routes.length)}
        </span>
      </PaneHeader>
      <div className="flex shrink-0 flex-col gap-1.5 border-b border-line p-2">
        <label className="input input-sm w-full">
          <Icon name="search" className="size-3.5 text-fg-3" />
          <input
            ref={inputRef}
            type="search"
            placeholder="Filter endpoints…"
            aria-label="Filter endpoints"
            autoComplete="off"
            spellCheck={false}
            value={state.q}
            onChange={(e) => setState({ ...state, q: e.target.value })}
          />
          <kbd className="kbd kbd-xs">/</kbd>
        </label>
        {chips.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {chips.map((m) => (
              <button
                key={m}
                type="button"
                aria-pressed={!!state.methods[m]}
                title={'Only ' + m + ' endpoints (combine several)'}
                className={cx(
                  'inline-flex h-5 items-center gap-1 rounded border px-1.5 font-mono text-[10px] font-bold',
                  state.methods[m] ? cx(methodClass(m), 'border-current') : 'border-line text-fg-3 hover:text-fg-2',
                )}
                onClick={() => setState({ ...state, methods: { ...state.methods, [m]: !state.methods[m] } })}
              >
                {m}
                <span className="font-normal opacity-80">{present[m]}</span>
              </button>
            ))}
          </div>
        )}
        <div className="flex gap-1.5">
          <select
            className="select select-xs min-w-0 flex-1"
            aria-label="Group endpoints by"
            value={state.group}
            onChange={(e) => setState({ ...state, group: e.target.value as ListState['group'] })}
          >
            {GROUPS.map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </select>
          <select
            className="select select-xs min-w-0 flex-1"
            aria-label="Show"
            value={state.filter}
            onChange={(e) => setState({ ...state, filter: e.target.value as ListState['filter'] })}
          >
            {FILTERS.map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div
        ref={listRef}
        className="scroll-thin min-h-0 flex-1 overflow-y-auto px-1 pb-3"
        role="listbox"
        aria-label="Endpoints"
      >
        {!routes.length ? (
          <div className="px-3 py-6 text-center text-xs text-fg-2">
            <div className="mb-1 text-sm font-semibold text-fg">No HTTP endpoint</div>
            The analysis found no route registration (gin, echo, chi or net/http).
          </div>
        ) : !tree.shown ? (
          <div className="px-3 py-6 text-center text-xs text-fg-2">No endpoint matches the filter.</div>
        ) : (
          [...tree.t.keys()].sort().map((g1) => {
            const sub = tree.t.get(g1)!
            let total = 0
            sub.forEach((l) => (total += l.length))
            return (
              <div key={g1}>
                <div className="sticky top-0 z-[1] flex items-center gap-2 bg-base-100 px-2 pt-2.5 pb-1 text-[11px] font-semibold text-fg-2">
                  <span className="min-w-0 flex-1 truncate" title={g1}>
                    {g1}
                  </span>
                  <span className="font-normal text-fg-3 tabular-nums">{total}</span>
                </div>
                {[...sub.keys()]
                  .sort((a, b) => mOrder(a) - mOrder(b) || (a < b ? -1 : 1))
                  .map((g2) => (
                    <div key={g2}>
                      {g2 && (
                        <div className="flex items-center gap-2 px-2 pt-1 pb-0.5 text-[11px] text-fg-3">
                          <Method method={g2} className="min-w-0 bg-transparent! px-0" />
                          <span className="flex-1" />
                          <span className="tabular-nums">{sub.get(g2)!.length}</span>
                        </div>
                      )}
                      {sub
                        .get(g2)!
                        .sort((a, b) =>
                          routes[a].path < routes[b].path ? -1 : routes[a].path > routes[b].path ? 1 : a - b,
                        )
                        .map((k) => {
                          const r = routes[k]
                          const sf = findingsOf?.(r.id)
                          const nf = sf?.n ?? 0
                          return (
                            <button
                              key={k}
                              type="button"
                              role="option"
                              aria-selected={current === k}
                              aria-current={current === k}
                              title={r.key + (r.handler ? ' → ' + r.handler.name : '')}
                              className={cx(
                                'flex w-full items-start gap-2 rounded-field px-2 py-1 text-left text-xs',
                                current === k
                                  ? 'bg-accent-weak text-fg'
                                  : 'text-fg-2 hover:bg-surface-hover hover:text-fg',
                              )}
                              onClick={() => onOpen(k)}
                            >
                              <Method method={r.method} className="mt-px" />
                              <PathText
                                path={r.path}
                                className="min-w-0 flex-1 text-[11.5px] leading-[18px] [overflow-wrap:anywhere]"
                              />
                              <span className="mt-0.5 flex shrink-0 items-center gap-1">
                                {nf > 0 && (
                                  <span
                                    className={cx('badge badge-xs gap-0.5 border-current/40', SEV_TEXT[sf!.worst] ?? 'text-fg-2')}
                                    title={nf + ' security finding(s), worst ' + (sf!.worst || 'unknown') + ': open the route in the Security view'}
                                  >
                                    <Icon name="shield" className="size-3" />
                                    {nf}
                                  </span>
                                )}
                                {r.auth?.recognized && (
                                  <span
                                    className="text-s-check"
                                    title={'Authentication recognised: ' + (r.auth.by || r.auth.scheme || '')}
                                  >
                                    <Lock />
                                    <span className="sr-only">authentication</span>
                                  </span>
                                )}
                                {r.status !== 'complete' && (
                                  <span
                                    className="text-warning"
                                    title={'Contract ' + r.status + ': ' + (r.reasons ?? []).join('; ')}
                                  >
                                    <Icon name="warning" className="size-3.5" />
                                    <span className="sr-only">partial contract</span>
                                  </span>
                                )}
                              </span>
                            </button>
                          )
                        })}
                    </div>
                  ))}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
