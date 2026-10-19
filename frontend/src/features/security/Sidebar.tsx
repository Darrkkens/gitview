// Security sidebar: findings by category (filtered by severity, category and
// state), the routes with their security posture, and the rules.

import { useEffect, useMemo, useRef, type RefObject } from 'react'
import { Icon } from '../../components/Icon'
import { PaneHeader } from '../../components/Panes'
import { cx, fmt } from '../../lib/format'
import { Lock, Method, PathText } from '../http/ui'
import { CATS, SEVS, SEV_RANK, STATUSES, STATUS_LABEL, type Sec } from './model'
import { AiTag, SDot, Sev } from './parts'
import type { Finding } from './types'

export interface SideState {
  q: string
  sev: Record<string, boolean>
  cat: string
  /** "v:<verdict>" or "s:<review status>" */
  state: string
}

export type ListKind = 'findings' | 'routes' | 'rules'

function matches(sec: Sec, f: Finding, s: SideState): boolean {
  const anySev = SEVS.some((x) => s.sev[x])
  if (anySev && !s.sev[f.severity]) return false
  if (s.cat && f.category !== s.cat) return false
  if (s.state.startsWith('v:') && f.verdict !== s.state.slice(2)) return false
  if (s.state.startsWith('s:') && sec.status(f) !== s.state.slice(2)) return false
  const q = s.q.trim().toLowerCase()
  if (!q) return true
  const hay = (
    f.title +
    ' ' +
    f.ruleId +
    ' ' +
    f.message +
    ' ' +
    (f.file || '') +
    ' ' +
    (f.routes ?? []).map((r) => r.key).join(' ')
  ).toLowerCase()
  return q.split(/\s+/).every((w) => hay.includes(w))
}

const itemCls = (cur: boolean) =>
  cx(
    'flex w-full items-start gap-2 rounded-field px-2 py-1.5 text-left text-xs',
    cur ? 'bg-accent-weak' : 'hover:bg-surface-hover',
  )

export function FindingRow({
  sec,
  f,
  current,
  onOpen,
}: {
  sec: Sec
  f: Finding
  current: boolean
  onOpen: (id: string) => void
}) {
  const st = sec.stateOf(f)
  const status = sec.status(f)
  const d = sec.diffOf.get(f.id)
  return (
    <button
      type="button"
      role="option"
      aria-selected={current}
      aria-current={current}
      title={f.verdictLabel}
      className={itemCls(current)}
      onClick={() => onOpen(f.id)}
    >
      <SDot cls={st.cls} title={st.label} className="mt-1" />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-fg [overflow-wrap:anywhere]">{f.title}</span>
        <span className="truncate font-mono text-[11px] text-fg-3">
          {(f.file || '') + (f.line ? ':' + f.line : '')}
        </span>
        <span className="flex flex-wrap items-center gap-1">
          <Sev sev={f.severity} />
          <span className="badge badge-ghost badge-xs font-mono">{f.ruleId}</span>
          <span className="sr-only">{st.label}</span>
          {status !== 'open' && (
            <span className="badge badge-xs badge-info badge-soft">{STATUS_LABEL[status] ?? status}</span>
          )}
          {sec.revalidate[f.id] && (
            <span className="badge badge-xs badge-warning badge-soft" title={sec.revalidate[f.id]}>
              revalidate
            </span>
          )}
          {sec.ai[f.id] && <AiTag />}
          {d && <span className="badge badge-xs badge-ghost">{d.kind}</span>}
        </span>
      </span>
    </button>
  )
}

export function Sidebar({
  sec,
  list,
  setList,
  side,
  setSide,
  finding,
  route,
  onFinding,
  onRoute,
  inputRef,
}: {
  sec: Sec
  list: ListKind
  setList: (l: ListKind) => void
  side: SideState
  setSide: (s: SideState) => void
  finding: string | null
  route: string | null
  onFinding: (id: string) => void
  onRoute: (id: string) => void
  inputRef: RefObject<HTMLInputElement | null>
}) {
  const F = sec.F
  const sevCount = useMemo(() => {
    const c: Record<string, number> = {}
    F.forEach((f) => (c[f.severity] = (c[f.severity] || 0) + 1))
    return c
  }, [F])
  const cats = useMemo(() => Object.keys(CATS).filter((c) => F.some((f) => f.category === c)), [F])
  const shown = useMemo(() => F.filter((f) => matches(sec, f, side)), [sec, F, side])
  const listRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    listRef.current?.querySelector('[aria-current="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [finding, route, list])

  const q = side.q.trim().toLowerCase()
  const counts: Record<ListKind, number> = { findings: F.length, routes: sec.postures.length, rules: sec.rules.length }
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PaneHeader title="Security">
        <span className="text-[11px] text-fg-3 tabular-nums">
          {list === 'findings' && shown.length !== F.length
            ? fmt(shown.length) + ' / ' + fmt(F.length)
            : fmt(counts[list])}
        </span>
      </PaneHeader>
      <div className="flex shrink-0 flex-col gap-1.5 border-b border-line p-2">
        <div role="tablist" className="tabs tabs-box tabs-xs w-full bg-base-200">
          {(['findings', 'routes', 'rules'] as ListKind[]).map((k) => (
            <button
              key={k}
              type="button"
              role="tab"
              aria-selected={list === k}
              className={cx('tab flex-1 gap-1 text-xs', list === k && 'tab-active')}
              onClick={() => setList(k)}
            >
              {k[0].toUpperCase() + k.slice(1)}
              <span className="text-fg-3 tabular-nums">{counts[k]}</span>
            </button>
          ))}
        </div>
        {list !== 'rules' && (
          <label className="input input-sm w-full">
            <Icon name="search" className="size-3.5 text-fg-3" />
            <input
              ref={inputRef}
              type="search"
              placeholder={list === 'routes' ? 'Filter routes…' : 'Filter findings…'}
              aria-label={list === 'routes' ? 'Filter routes' : 'Filter findings'}
              autoComplete="off"
              spellCheck={false}
              value={side.q}
              onChange={(e) => setSide({ ...side, q: e.target.value })}
            />
            <kbd className="kbd kbd-xs">/</kbd>
          </label>
        )}
        {list === 'findings' && (
          <>
            <div className="flex flex-wrap gap-1" role="group" aria-label="Severity">
              {SEVS.filter((s) => sevCount[s]).map((s) => (
                <button
                  key={s}
                  type="button"
                  aria-pressed={!!side.sev[s]}
                  title={'Only ' + s + ' findings (combine several)'}
                  className={cx(
                    'inline-flex h-6 items-center gap-1 rounded-field border px-1 text-[11px]',
                    side.sev[s] ? 'border-line-strong bg-surface-3 text-fg' : 'border-line text-fg-3 hover:text-fg-2',
                  )}
                  onClick={() => setSide({ ...side, sev: { ...side.sev, [s]: !side.sev[s] } })}
                >
                  <Sev sev={s} />
                  <span className="pr-0.5 tabular-nums">{sevCount[s]}</span>
                </button>
              ))}
            </div>
            <div className="flex gap-1.5">
              <select
                className="select select-xs min-w-0 flex-1"
                aria-label="Category"
                value={side.cat}
                onChange={(e) => setSide({ ...side, cat: e.target.value })}
              >
                <option value="">All categories</option>
                {cats.map((c) => (
                  <option key={c} value={c}>
                    {CATS[c]}
                  </option>
                ))}
              </select>
              <select
                className="select select-xs min-w-0 flex-1"
                aria-label="Verdict or review status"
                value={side.state}
                onChange={(e) => setSide({ ...side, state: e.target.value })}
              >
                <option value="">Any state</option>
                <option value="v:vulnerable">Vulnerable</option>
                <option value="v:review">Review required</option>
                <option value="v:incomplete">Incomplete</option>
                {STATUSES.map((s) => (
                  <option key={s} value={'s:' + s}>
                    Review: {STATUS_LABEL[s]}
                  </option>
                ))}
              </select>
            </div>
          </>
        )}
      </div>
      <div
        ref={listRef}
        className="scroll-thin min-h-0 flex-1 overflow-y-auto px-1 pb-3"
        role="listbox"
        aria-label={list[0].toUpperCase() + list.slice(1)}
      >
        {list === 'rules' ? (
          sec.rules.map((r) => (
            <div key={r.id} className="flex items-start gap-2 px-2 py-1.5 text-xs" title={r.detects}>
              <SDot
                cls={r.findings ? 'review' : 'struct'}
                className="mt-1"
                title={r.findings ? 'found something' : 'nothing found'}
              />
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="text-fg">{r.title}</span>
                <span className="truncate font-mono text-[11px] text-fg-3">
                  {r.id + ' · ' + r.category + ' · ' + r.basis}
                </span>
                <span className="flex flex-wrap items-center gap-1">
                  <Sev sev={r.severity} />
                  <span className={cx('badge badge-xs', r.enabled ? 'badge-ghost' : 'badge-warning badge-soft')}>
                    {r.enabled ? r.findings + ' finding(s)' : 'not run'}
                  </span>
                </span>
              </span>
            </div>
          ))
        ) : list === 'routes' ? (
          sec.postures
            .filter((r) => !q || r.key.toLowerCase().includes(q))
            .map((r) => {
              const fs = (r.findings ?? []).map((id) => sec.byId.get(id)).filter((f): f is Finding => !!f)
              let worst: Finding | null = null
              for (const f of fs) if (!worst || SEV_RANK[f.severity] > SEV_RANK[worst.severity]) worst = f
              const i = r.key.indexOf(' ')
              const method = r.key.slice(0, i),
                path = r.key.slice(i + 1)
              const cur = route === r.id
              return (
                <button
                  key={r.id}
                  type="button"
                  role="option"
                  aria-selected={cur}
                  aria-current={cur}
                  title={r.key + ' · authentication ' + r.auth}
                  className={cx(itemCls(cur), 'items-center py-1')}
                  onClick={() => onRoute(r.id)}
                >
                  <Method method={method} />
                  <PathText path={path} className="min-w-0 flex-1 text-[11.5px] [overflow-wrap:anywhere]" />
                  {worst && <Sev sev={worst.severity} />}
                  {fs.length > 0 && <span className="badge badge-ghost badge-xs tabular-nums">{fs.length}</span>}
                  {r.auth !== 'none identified' && (
                    <span className="text-s-check" title={'authentication ' + r.auth}>
                      <Lock />
                      <span className="sr-only">authentication</span>
                    </span>
                  )}
                  {!!r.unresolved && (
                    <span className="text-warning" title={r.unresolved + ' call(s) without a static target'}>
                      <Icon name="warning" className="size-3.5" />
                      <span className="sr-only">unresolved calls</span>
                    </span>
                  )}
                </button>
              )
            })
        ) : !F.length ? (
          <div className="px-3 py-6 text-center text-xs text-fg-2">
            <div className="mb-1 text-sm font-semibold text-fg">No findings</div>
            The enabled rules found nothing in the analysed code. That is not a proof of security: see the analysis
            status and limits in the inspector.
          </div>
        ) : !shown.length ? (
          <div className="px-3 py-6 text-center text-xs text-fg-2">No finding matches the filters.</div>
        ) : (
          [...Object.keys(CATS), ...[...new Set(F.map((f) => f.category))].filter((c) => !CATS[c])].map((cat) => {
            const items = shown.filter((f) => f.category === cat)
            if (!items.length) return null
            return (
              <div key={cat}>
                <div className="sticky top-0 z-[1] flex items-center gap-2 bg-base-100 px-2 pt-2.5 pb-1 text-[11px] font-semibold text-fg-2">
                  <span className="flex-1">{CATS[cat] ?? cat}</span>
                  <span className="font-normal text-fg-3 tabular-nums">{items.length}</span>
                </div>
                {items.map((f) => (
                  <FindingRow key={f.id} sec={sec} f={f} current={finding === f.id} onOpen={onFinding} />
                ))}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
