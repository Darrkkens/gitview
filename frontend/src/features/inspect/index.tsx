// Go Deep Inspector: six views of one Go function — Code Flow, Data Flow,
// Execution Trace, Branch Explorer, Performance and SQL Explorer — computed
// by GitView when the report was made. The page only displays them; every
// result says whether it is static, observed or estimated.

import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { CodeInline } from '../../components/Code'
import { CopyButton } from '../../components/CopyButton'
import { Icon } from '../../components/Icon'
import { PaneHeader, Panes } from '../../components/Panes'
import { HeaderSlot, StatusSlot } from '../../components/Slots'
import { EmptyState, SectionGate } from '../../components/States'
import { offered } from '../../lib/data'
import { cx, plural } from '../../lib/format'
import { routeHref, setParams } from '../../lib/router'
import type { ReportSection } from '../../lib/types'
import type { ViewProps } from '../../views'
import BranchesTab from './BranchesTab'
import CodeFlowTab from './CodeFlowTab'
import { Ctx, useInspect, type InspectCtx } from './ctx'
import DataFlowTab from './DataFlowTab'
import PerfTab from './PerfTab'
import { typing } from './keys'
import { Prefs, usePrefsState } from './prefs'
import SqlTab from './SqlTab'
import TraceTab from './TraceTab'
import type { Bundle, FlowIndex, InspectSection, TabId } from './types'
import { LocLink } from './ui'
import { loc } from './util'

const TABS: [TabId, string][] = [
  ['codeflow', 'Code Flow'],
  ['dataflow', 'Data Flow'],
  ['trace', 'Execution Trace'],
  ['branches', 'Branch Explorer'],
  ['perf', 'Performance'],
  ['sql', 'SQL Explorer'],
]

const DEFAULT_TAB: TabId = 'branches'

export default function InspectView({ params }: ViewProps) {
  return (
    <SectionGate<ReportSection> section="report">
      {(r) =>
        r.inspect ? (
          <Inspector report={r} data={r.inspect as InspectSection} params={params} />
        ) : (
          <EmptyState icon="microscope" title="No Go Deep Inspector data">
            This report was exported without the Go Deep Inspector.
          </EmptyState>
        )
      }
    </SectionGate>
  )
}

function hasData(t: TabId, b: Bundle, flow: FlowIndex | null): boolean {
  switch (t) {
    case 'codeflow':
      return !!flow?.nodes?.length
    case 'dataflow':
      return !!b.dataflow && Object.keys(b.dataflow).length > 0
    case 'trace':
      return !!b.trace?.loaded
    case 'branches':
      return !!b.branches
    case 'perf':
      return !!b.performance && !b.performance.message
    case 'sql':
      return !!b.sql?.ops?.length
  }
}

/** Lines of each changed file that the report's diff shows (new side). */
function diffLines(report: ReportSection): Map<string, Set<number>> {
  const m = new Map<string, Set<number>>()
  for (const f of report.files ?? []) {
    const s = new Set<number>()
    for (const h of f.hunks ?? []) for (const l of h.lines ?? []) if (l.new) s.add(l.new)
    m.set(f.path, s)
  }
  return m
}

function Inspector({ report, data, params }: { report: ReportSection; data: InspectSection; params: URLSearchParams }) {
  const bundles = useMemo(() => data.symbols ?? [], [data])
  const flow = (report.flow ?? null) as FlowIndex | null
  const prefs = usePrefsState()

  const index = useMemo(() => {
    const m = new Map<string, number>()
    bundles.forEach((b, i) => m.set(b.symbol.id, i))
    return m
  }, [bundles])

  const want = params.get('fn')
  const sel = want !== null && index.has(want) ? index.get(want)! : 0
  const missing = want !== null && !index.has(want) ? want : null
  const tabParam = params.get('tab') as TabId | null
  const tab: TabId = tabParam && TABS.some(([t]) => t === tabParam) ? tabParam : DEFAULT_TAB

  const ctx = useMemo<InspectCtx>(() => {
    const lines = diffLines(report)
    const anchors = data.anchors ?? {}
    return {
      data,
      bundles,
      flow,
      diffHref: (file, line) => {
        if (!file || !anchors[file]) return null
        if (line && lines.get(file)?.has(line)) return routeHref('changes', { file, line })
        return line ? null : routeHref('changes', { file })
      },
      indexOf: (id) => (id !== undefined && index.has(id) ? index.get(id)! : -1),
      select: (id, t) => setParams(t ? { fn: id, tab: t } : { fn: id }),
    }
  }, [report, data, bundles, flow, index])

  if (!bundles.length) {
    return (
      <EmptyState icon="microscope" title="No function to inspect">
        {data.notes?.[0] ?? 'No function was included in this report.'}
        {data.errors?.map((e) => (
          <div key={e} className="mt-2 text-error">
            {e}
          </div>
        ))}
      </EmptyState>
    )
  }

  return (
    <Ctx value={ctx}>
      <Prefs value={prefs}>
        <Layout b={bundles[sel]} sel={sel} tab={tab} missing={missing} />
      </Prefs>
    </Ctx>
  )
}

function useNarrow(px: number): boolean {
  const [narrow, setNarrow] = useState(() => window.innerWidth < px)
  useEffect(() => {
    const on = () => setNarrow(window.innerWidth < px)
    window.addEventListener('resize', on)
    return () => window.removeEventListener('resize', on)
  }, [px])
  return narrow
}

function Layout({ b, sel, tab, missing }: { b: Bundle; sel: number; tab: TabId; missing: string | null }) {
  const { bundles, data, flow, diffHref, select } = useInspect()
  const narrow = useNarrow(1000)
  const [listOpen, setListOpen] = useState<boolean | null>(null)
  const showList = listOpen ?? !narrow
  const filterRef = useRef<HTMLInputElement>(null)
  const [help, setHelp] = useState(false)

  const setTab = useCallback((t: TabId) => setParams({ fn: b.symbol.id, tab: t }), [b])
  const step = useCallback(
    (d: number) => {
      const i = (sel + d + bundles.length) % bundles.length
      select(bundles[i].symbol.id)
    },
    [sel, bundles, select],
  )

  // View-wide keys; each tab adds its own.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || typing(e) || e.defaultPrevented) return
      const i = TABS.findIndex(([t]) => t === tab)
      if (e.key >= '1' && e.key <= '6') setTab(TABS[+e.key - 1][0])
      else if (e.key === ']') setTab(TABS[(i + 1) % TABS.length][0])
      else if (e.key === '[') setTab(TABS[(i + TABS.length - 1) % TABS.length][0])
      else if (e.key === 'j') step(1)
      else if (e.key === 'k') step(-1)
      else if (e.key === '/') {
        setListOpen(true)
        setTimeout(() => filterRef.current?.focus(), 0)
      } else if (e.key === 'g') {
        const href = diffHref(b.symbol.file, b.symbol.line) ?? diffHref(b.symbol.file)
        if (href) location.hash = href
        else return
      } else if (e.key === '?') setHelp((h) => !h)
      else if (e.key === 'Escape' && help) setHelp(false)
      else return
      e.preventDefault()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [tab, setTab, step, b, diffHref, help])

  const r = b.symbol
  const inFlow = !!flow?.nodes?.some((n) => n.id === r.id)
  const changed = !!(r.file && data.anchors?.[r.file])

  return (
    <>
      <HeaderSlot>
        <KeysHelp open={help} setOpen={setHelp} />
      </HeaderSlot>
      <StatusSlot>
        <StatusLine />
      </StatusSlot>
      <Panes
        id="inspect"
        leftWidth={300}
        showLeft={showList}
        left={<FunctionList sel={sel} filterRef={filterRef} onPicked={() => narrow && setListOpen(false)} />}
      >
        <div className="flex h-full min-h-0 flex-col">
          <div className="shrink-0 border-b border-line bg-base-100 px-4 pt-2.5">
            {missing && (
              <div role="alert" className="alert alert-warning alert-soft mb-2 px-3 py-1.5 text-xs">
                <Icon name="warning" className="size-4" />
                <span className="min-w-0 break-all">
                  No matching symbol found in this report: <span className="font-mono">{missing}</span>
                </span>
              </div>
            )}
            <div className="flex min-w-0 items-start gap-2">
              <button
                type="button"
                className="btn btn-ghost btn-xs btn-square mt-0.5"
                title={showList ? 'Hide the function list' : 'Show the function list (/)'}
                aria-label="Toggle the function list"
                aria-pressed={showList}
                onClick={() => setListOpen(!showList)}
              >
                <Icon name="panelLeft" className="size-4" />
              </button>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                  <h1 className="font-mono text-sm font-semibold break-all text-fg">{r.qualified}</h1>
                  {changed && (
                    <span
                      className="badge badge-warning badge-soft badge-sm"
                      title="The function's file is changed in the working tree"
                    >
                      changed file
                    </span>
                  )}
                </div>
                {r.sig && <CodeInline code={r.sig} className="mt-0.5 block truncate text-fg-2" />}
              </div>
              <div className="flex shrink-0 items-center gap-1">
                {flow && inFlow && (
                  <a
                    className="btn btn-ghost btn-xs"
                    href={routeHref('flow', { node: r.id })}
                    title="Open the call graph centred on this function"
                  >
                    <Icon name="flow" className="size-3.5" />
                    <span className="max-[1100px]:hidden">Call graph</span>
                  </a>
                )}
                {offered('graph') && (
                  <a
                    className="btn btn-ghost btn-xs"
                    href={routeHref('architecture', { node: 'func:' + r.id })}
                    title="Show this function in the Architecture Explorer"
                  >
                    <Icon name="graph" className="size-3.5" />
                    <span className="max-[1100px]:hidden">Architecture</span>
                  </a>
                )}
              </div>
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 pl-8 text-[11px] text-fg-3">
              <span className="flex min-w-0 items-center gap-0.5">
                <span className="font-mono break-all" title="Symbol ID (accepted by gitview inspect --function)">
                  {r.id}
                </span>
                <CopyButton text={r.id} title="Copy the symbol ID" className="h-5 min-h-0 px-1 text-fg-3" />
              </span>
              <span>·</span>
              <span>
                <LocLink file={r.file} line={r.line} orFile />
                {r.end ? <span className="font-mono">–{r.end}</span> : null}
              </span>
              {r.module && (
                <>
                  <span>·</span>
                  <span>
                    module <span className="font-mono">{r.module}</span>
                  </span>
                </>
              )}
              {r.version && (
                <>
                  <span>·</span>
                  <span title="Fingerprint of the analysed sources">
                    code version <span className="font-mono">{r.version}</span>
                  </span>
                </>
              )}
            </div>
            <Tabs b={b} tab={tab} setTab={setTab} flow={flow} />
          </div>
          <div
            role="tabpanel"
            aria-label={TABS.find(([t]) => t === tab)?.[1]}
            className="scroll-thin min-h-0 flex-1 overflow-auto bg-base-200 px-4 py-3"
          >
            <TabBody key={r.id + ':' + tab} b={b} tab={tab} />
          </div>
        </div>
      </Panes>
    </>
  )
}

function TabBody({ b, tab }: { b: Bundle; tab: TabId }): ReactNode {
  switch (tab) {
    case 'codeflow':
      return <CodeFlowTab b={b} />
    case 'dataflow':
      return <DataFlowTab b={b} />
    case 'trace':
      return <TraceTab b={b} />
    case 'branches':
      return <BranchesTab b={b} />
    case 'perf':
      return <PerfTab b={b} />
    case 'sql':
      return <SqlTab b={b} />
  }
}

function Tabs({ b, tab, setTab, flow }: { b: Bundle; tab: TabId; setTab: (t: TabId) => void; flow: FlowIndex | null }) {
  const refs = useRef<Record<string, HTMLButtonElement | null>>({})
  return (
    <div
      role="tablist"
      aria-label="Inspector views"
      className="tabs tabs-border tabs-sm mt-1.5 -mb-px flex-nowrap overflow-x-auto"
    >
      {TABS.map(([id, label], i) => {
        const on = hasData(id, b, flow)
        return (
          <button
            key={id}
            ref={(el) => {
              refs.current[id] = el
            }}
            type="button"
            role="tab"
            aria-selected={tab === id}
            tabIndex={tab === id ? 0 : -1}
            className={cx('tab gap-1.5 px-3 text-[13px] whitespace-nowrap', tab === id && 'tab-active')}
            title={`${label} (${i + 1}) · ${on ? 'data available' : 'no data'}`}
            onClick={() => setTab(id)}
            onKeyDown={(e) => {
              if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
              e.preventDefault()
              const k = (i + (e.key === 'ArrowRight' ? 1 : TABS.length - 1)) % TABS.length
              setTab(TABS[k][0])
              refs.current[TABS[k][0]]?.focus()
            }}
          >
            {label}
            <span
              className={cx('status', on ? 'status-success' : 'opacity-40')}
              aria-label={on ? 'data available' : 'no data'}
            />
          </button>
        )
      })}
    </div>
  )
}

function FunctionList({
  sel,
  filterRef,
  onPicked,
}: {
  sel: number
  filterRef: React.RefObject<HTMLInputElement | null>
  onPicked: () => void
}) {
  const { bundles, data, select } = useInspect()
  const [q, setQ] = useState('')
  const shown = useMemo(() => {
    const s = q.trim().toLowerCase()
    return bundles
      .map((b, i) => ({ b, i }))
      .filter(({ b }) => !s || (b.symbol.qualified + ' ' + (b.symbol.file ?? '')).toLowerCase().includes(s))
  }, [bundles, q])
  const listRef = useRef<HTMLUListElement>(null)
  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>('[aria-current="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [sel])

  return (
    <>
      <PaneHeader title={`Functions · ${bundles.length}`} />
      <div className="shrink-0 border-b border-line p-2">
        <label className="input input-xs w-full">
          <Icon name="search" className="size-3.5 text-fg-3" />
          <input
            ref={filterRef}
            type="search"
            placeholder="Filter functions…"
            aria-label="Filter functions"
            autoComplete="off"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                setQ('')
                e.currentTarget.blur()
              } else if (e.key === 'Enter' && shown.length) {
                select(shown[0].b.symbol.id)
                onPicked()
                e.currentTarget.blur()
              }
            }}
          />
          <kbd className="kbd kbd-xs">/</kbd>
        </label>
      </div>
      <ul ref={listRef} aria-label="Functions" className="scroll-thin min-h-0 flex-1 overflow-y-auto py-1">
        {shown.map(({ b, i }) => {
          const r = b.symbol
          const ops = b.sql?.ops?.length ?? 0
          const changed = !!(r.file && data.anchors?.[r.file])
          return (
            <li key={r.id}>
              <button
                type="button"
                aria-current={i === sel}
                className={cx(
                  'flex w-full min-w-0 flex-col items-start gap-0.5 border-l-2 px-3 py-1.5 text-left hover:bg-surface-hover',
                  i === sel ? 'border-primary bg-accent-weak' : 'border-transparent',
                )}
                onClick={() => {
                  select(r.id)
                  onPicked()
                }}
              >
                <span
                  className={cx('w-full truncate font-mono text-xs', i === sel ? 'text-fg' : 'text-fg-2')}
                  title={r.qualified}
                >
                  {r.qualified}
                </span>
                <span className="flex w-full min-w-0 items-center gap-1.5 text-[11px] text-fg-3">
                  <span className="truncate font-mono" title={loc(r.file, r.line)}>
                    {loc(r.file, r.line)}
                  </span>
                  <span className="ml-auto flex shrink-0 gap-1">
                    {changed && <span className="size-1.5 rounded-full bg-warning" title="changed file" />}
                    {ops > 0 && (
                      <span className="badge badge-ghost badge-xs font-mono" title={plural(ops, 'database operation')}>
                        SQL {ops}
                      </span>
                    )}
                  </span>
                </span>
              </button>
            </li>
          )
        })}
        {!shown.length && <li className="px-3 py-4 text-center text-xs text-fg-3">No function matches “{q}”.</li>}
      </ul>
      {(data.notes?.length || data.errors?.length) && (
        <div className="shrink-0 space-y-1 border-t border-line px-3 py-2 text-[11px] text-fg-3">
          {data.notes?.map((n) => (
            <div key={n}>{n}</div>
          ))}
          {data.errors?.map((e) => (
            <div key={e} className="text-warning" title="Packages that failed to compile: results may be partial">
              {e}
            </div>
          ))}
        </div>
      )}
    </>
  )
}

function StatusLine() {
  const { bundles, data } = useInspect()
  const rt: string[] = []
  if (data.cpu) rt.push('CPU profile')
  if (data.mem) rt.push('memory profile')
  if (data.trace) rt.push('trace')
  if (data.benches?.length) rt.push(plural(data.benches.length, 'benchmark run'))
  if (data.escape) rt.push('compiler diagnostics')
  if (data.plans?.length) rt.push(plural(data.plans.length, 'query plan'))
  return (
    <>
      <span>{plural(bundles.length, 'function')}</span>
      <span title="Static analyses are computed from the source; runtime data was imported or captured in the session">
        {rt.length ? 'runtime data: ' + rt.join(', ') : 'static analysis only · no runtime data'}
      </span>
    </>
  )
}

const KEYS: [string, string][] = [
  ['1 – 6', 'switch view'],
  ['[  ]', 'previous / next view'],
  ['j  k', 'next / previous function'],
  ['/', 'filter functions'],
  ['g', 'open the function in the diff'],
  ['f  +  −', 'fit / zoom graph or timeline'],
  ['o  b  d', 'Data Flow: origin / both / destination'],
  ['< >', 'Data Flow: call depth'],
  ['h', 'Data Flow: hide intermediate values'],
  ['v', 'Data Flow: choose the variable'],
  ['p  P', 'Branch Explorer: next / previous path'],
  ['Esc', 'clear the highlighted path'],
  ['m', 'Performance: CPU / memory'],
  ['a', 'Trace: runtime goroutines'],
  ['0  ←  →', 'Trace: whole trace / pan'],
  ['?', 'this help'],
]

function KeysHelp({ open, setOpen }: { open: boolean; setOpen: (v: boolean) => void }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const onDown = (e: PointerEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', onDown)
    return () => document.removeEventListener('pointerdown', onDown)
  }, [open, setOpen])
  return (
    <div ref={ref} className={cx('dropdown dropdown-end', open && 'dropdown-open')}>
      <button
        type="button"
        className="btn btn-ghost btn-sm btn-square"
        title="Keyboard shortcuts (?)"
        aria-label="Keyboard shortcuts"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        <Icon name="help" />
      </button>
      {open && (
        <div
          tabIndex={-1}
          className="dropdown-content z-50 mt-1 w-80 rounded-box border border-line bg-base-100 p-3 shadow-lg"
        >
          <div className="mb-2 text-xs font-semibold text-fg-2">Inspector shortcuts</div>
          <table className="w-full text-xs">
            <tbody>
              {KEYS.map(([k, v]) => (
                <tr key={k}>
                  <td className="py-0.5 pr-3 align-top whitespace-nowrap">
                    {k.split(/\s+/).map((x) => (
                      <kbd key={x} className="kbd kbd-xs mr-0.5">
                        {x}
                      </kbd>
                    ))}
                  </td>
                  <td className="py-0.5 text-fg-2">{v}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
