// Runtime Performance: a capture of a running Go process (process metrics
// from /proc, HTTP requests from the proxy, the collector or OpenTelemetry,
// optional profiles and trace), and the comparison of two captures. Every
// value says how it was obtained (measured, sampled, estimated or
// unavailable) and whether it covers the whole process or one request.
//
// URL: #/runtime?tab=<tab>&capture=<id>&base=<id>&route=<key>
// (serve mode: capture opens a saved capture, base compares it with another)

import { useEffect, useLayoutEffect, useMemo, useRef, type ReactNode } from 'react'
import { Icon } from '../../components/Icon'
import { StatusSlot } from '../../components/Slots'
import { EmptyState, ErrorState, Loading, SectionGate } from '../../components/States'
import { isTyping } from '../../lib/browser'
import { reload } from '../../lib/data'
import { isLive } from '../../lib/envelope'
import { cx, dateTime, plural } from '../../lib/format'
import { routeHref, setParams } from '../../lib/router'
import type { ViewProps } from '../../views'
import { captureLabel, captureOption, useRuntimeData } from './data'
import { BASIS_HELP } from './fmt'
import { Comparison } from './tabs/Comparison'
import { Limitations } from './tabs/Limitations'
import { Overview } from './tabs/Overview'
import { Profiles } from './tabs/Profiles'
import { Requests } from './tabs/Requests'
import { Routes } from './tabs/Routes'
import type { Analysis, CaptureEntry, RuntimeSection } from './types'
import { BasisBadge } from './ui'

export default function RuntimeView({ params }: ViewProps) {
  return (
    <SectionGate<RuntimeSection> section="runtime" loadingLabel="Loading the runtime captures…">
      {(s) => <Runtime section={s} params={params} />}
    </SectionGate>
  )
}

interface Tab {
  key: string
  name: string
  count?: number
}

function Runtime({ section, params }: { section: RuntimeSection; params: URLSearchParams }) {
  const live = isLive()
  const capture = params.get('capture')
  const base = live ? params.get('base') : null
  const route = params.get('route')
  const loaded = useRuntimeData(section, capture, base)
  const data = loaded.data ?? section
  const A = data.a
  const C = data.comparison ?? null
  const captures = data.captures ?? section.captures ?? []

  const tabs: Tab[] = useMemo(() => {
    if (!A) return []
    const t: Tab[] = [
      { key: 'overview', name: 'Overview' },
      { key: 'requests', name: 'Requests', count: (A.requests ?? []).length },
      { key: 'routes', name: 'Routes', count: (A.routes ?? []).length },
      { key: 'profiles', name: 'Profiles' },
    ]
    if (C) t.push({ key: 'comparison', name: 'Comparison' })
    t.push({ key: 'limitations', name: 'Limitations', count: (A.limitations ?? []).length })
    return t
  }, [A, C])
  const tabKey = params.get('tab')
  const tab =
    tabs.find((t) => t.key === tabKey) ?? tabs.find((t) => t.key === (C ? 'comparison' : 'overview')) ?? tabs[0]

  // Another tab (or capture) starts at the top.
  const scroller = useRef<HTMLDivElement>(null)
  const tabShown = tab?.key + '|' + (A?.id ?? '') + '|' + (C?.a ?? '')
  // (a layout effect: it runs before a tab's own effects, which may scroll to a route)
  useLayoutEffect(() => {
    if (scroller.current) scroller.current.scrollTop = 0
  }, [tabShown])

  // Keyboard: [ ] previous/next tab, 1–6 a tab.
  useEffect(() => {
    if (!tabs.length) return
    const onKey = (e: KeyboardEvent) => {
      if (isTyping(e) || e.metaKey || e.ctrlKey || e.altKey) return
      const i = tabs.indexOf(tab!)
      let next: Tab | undefined
      if (e.key === ']') next = tabs[(i + 1) % tabs.length]
      else if (e.key === '[') next = tabs[(i + tabs.length - 1) % tabs.length]
      else if (/^[1-9]$/.test(e.key)) next = tabs[Number(e.key) - 1]
      if (!next) return
      e.preventDefault()
      setParams({ tab: next.key })
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [tabs, tab])

  if (!A) {
    if (loaded.loading) return <Loading label="Loading the capture…" />
    if (loaded.status === 'error') return <LoadError message={loaded.error} />
    return live ? <NoCapture captures={captures} /> : <EmptyState icon="gauge" title="No capture in this page" />
  }

  const P = A.processInfo ?? ({} as Analysis['processInfo'])
  const sub = [
    (C ? 'B: ' : '') + 'PID ' + P.pid,
    P.exe || P.comm || '',
    P.goVersion || '',
    dateTime(A.created),
    'status: ' + A.status,
    'sources: ' + (A.sources ?? []).join(', '),
  ].filter(Boolean)

  let body: ReactNode
  switch (tab?.key) {
    case 'requests':
      body = <Requests a={A} route={route} />
      break
    case 'routes':
      body = <Routes a={A} route={route} />
      break
    case 'profiles':
      body = <Profiles a={A} />
      break
    case 'comparison':
      body = C ? <Comparison c={C} a={A} base={data.base} /> : null
      break
    case 'limitations':
      body = <Limitations a={A} />
      break
    default:
      body = <Overview a={A} />
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="shrink-0 border-b border-line bg-base-100 px-4 pt-3">
        <div className="flex flex-wrap items-start gap-x-4 gap-y-2">
          <div className="min-w-0 flex-1">
            <h1 className="flex flex-wrap items-center gap-x-2 gap-y-1 text-base font-semibold">
              <Icon name="gauge" className="size-4 text-primary" />
              {C ? 'Runtime comparison' : 'Runtime Performance'}
              <span className="font-mono text-[13px] font-medium text-fg-2">
                {C ? C.a + ' → ' + C.b : A.label ? A.label : A.id}
              </span>
              {!C && A.label && <span className="font-mono text-xs font-normal text-fg-3">{A.id}</span>}
              <span
                className={cx('badge badge-soft badge-sm', A.status === 'complete' ? 'badge-success' : 'badge-warning')}
              >
                {A.status}
              </span>
            </h1>
            <p className="mt-0.5 truncate text-xs text-fg-3" title={sub.join(' · ')}>
              {sub.join(' · ')}
            </p>
          </div>
          {live && <Pickers section={section} data={data} captures={captures} base={base} loading={loaded.loading} />}
        </div>
        {!live && capture && capture !== A.id && (
          <div role="alert" className="alert alert-info alert-soft mt-2 px-3 py-1.5 text-xs">
            <Icon name="info" className="size-4" />
            <span>
              Capture <span className="font-mono">{capture}</span> is not in this page: it shows {A.id}. Open the
              capture with <code className="font-mono">gitview runtime show {capture} -o report.html</code> or with{' '}
              <code className="font-mono">gitview serve</code>.
            </span>
          </div>
        )}
        {loaded.status === 'error' && (
          <div role="alert" className="alert alert-error alert-soft mt-2 px-3 py-1.5 text-xs">
            <Icon name="error" className="size-4" />
            <span className="flex-1">Could not load: {loaded.error}</span>
            <a className="btn btn-ghost btn-xs" href={routeHref('runtime')}>
              Latest capture
            </a>
          </div>
        )}
        <div
          role="tablist"
          aria-label="Runtime report"
          className="tabs tabs-border tabs-sm -mb-px mt-1 flex-nowrap overflow-x-auto"
        >
          {tabs.map((t, i) => (
            <a
              key={t.key}
              role="tab"
              aria-selected={t === tab}
              href={routeHref('runtime', { tab: t.key, capture, base, route })}
              className={cx('tab gap-1.5 whitespace-nowrap', t === tab && 'tab-active')}
              title={'Key ' + (i + 1)}
            >
              {t.name}
              {t.count !== undefined && <span className="font-mono text-[11px] text-fg-3">{t.count}</span>}
            </a>
          ))}
        </div>
      </div>
      <div
        ref={scroller}
        className={cx('scroll-thin min-h-0 flex-1 overflow-y-auto transition-opacity', loaded.loading && 'opacity-60')}
      >
        <div className="mx-auto max-w-6xl px-4 pt-4 pb-12 [&>:first-child]:mt-0">{body}</div>
      </div>
      <StatusSlot>
        <span className="truncate">
          Runtime · {plural((A.requests ?? []).length, 'request')} · {plural((A.routes ?? []).length, 'route')}
        </span>
        <span className="hidden items-center gap-1 md:flex">
          {Object.keys(BASIS_HELP).map((b) => (
            <BasisBadge key={b} basis={b} />
          ))}
        </span>
      </StatusSlot>
    </div>
  )
}

function LoadError({ message }: { message: string }) {
  return (
    <ErrorState
      title="Could not load the capture"
      message={message}
      action={
        <a className="btn btn-sm" href={routeHref('runtime')}>
          Latest capture
        </a>
      }
    />
  )
}

/** Pickers choose the capture shown and another one to compare it with (serve mode). */
function Pickers({
  section,
  data,
  captures,
  base,
  loading,
}: {
  section: RuntimeSection
  data: RuntimeSection
  captures: CaptureEntry[]
  base: string | null
  loading: boolean
}) {
  const shownB = data.comparison ? data.comparison.b : (data.a?.id ?? '')
  const shownA = data.comparison ? data.comparison.a : (base ?? '')
  const byId = new Map(captures.map((c) => [c.id, c]))
  const open = (id: string) =>
    setParams({ capture: id === section.a?.id ? undefined : id, base: undefined, route: undefined })
  const compare = (id: string) => {
    if (!id) return setParams({ base: undefined })
    // The older capture is the reference (A), the newer one B.
    const x = byId.get(id)
    const y = byId.get(shownB)
    const older = x && y ? new Date(x.created) <= new Date(y.created) : true
    setParams({ base: older ? id : shownB, capture: older ? shownB : id, tab: 'comparison' })
  }
  const options = (skip?: string) =>
    captures
      .filter((c) => c.id !== skip)
      .map((c) => (
        <option key={c.id} value={c.id} title={c.id + ' · ' + captureLabel(c)}>
          {captureOption(c)}
        </option>
      ))
  const select = 'select select-sm w-64 text-xs font-normal text-fg'
  return (
    <div className="flex flex-wrap items-center justify-end gap-1.5 text-xs">
      {loading && <span className="loading loading-spinner loading-xs text-primary" aria-label="Loading" />}
      {data.comparison ? (
        <>
          <label className="flex items-center gap-1.5 font-semibold text-primary">
            A
            <select
              className={select}
              value={shownA}
              onChange={(e) => setParams({ base: e.target.value })}
              aria-label="Capture A, the reference"
            >
              {!byId.has(shownA) && <option value={shownA}>{shownA}</option>}
              {options(shownB)}
            </select>
          </label>
          <Icon name="chevronRight" className="size-3.5 text-fg-3" />
          <label className="flex items-center gap-1.5 font-semibold text-secondary">
            B
            <select
              className={select}
              value={shownB}
              onChange={(e) => setParams({ capture: e.target.value })}
              aria-label="Capture B, the new capture"
            >
              {!byId.has(shownB) && <option value={shownB}>{shownB}</option>}
              {options(shownA)}
            </select>
          </label>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            title="Swap A and B"
            onClick={() => setParams({ base: shownB, capture: shownA })}
          >
            Swap
          </button>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            title="Show B alone"
            onClick={() => setParams({ base: undefined, tab: undefined })}
          >
            <Icon name="x" className="size-3.5" />
            Stop comparing
          </button>
        </>
      ) : (
        <>
          <label className="flex items-center gap-1.5 text-fg-3">
            Capture
            <select className={select} value={shownB} onChange={(e) => open(e.target.value)} aria-label="Capture">
              {!byId.has(shownB) && <option value={shownB}>{shownB}</option>}
              {options()}
            </select>
          </label>
          <select
            className={select + ' w-48'}
            value=""
            onChange={(e) => compare(e.target.value)}
            aria-label="Compare with"
            disabled={captures.length < 2}
            title={captures.length < 2 ? 'Comparing needs two saved captures' : 'Compare with another capture'}
          >
            <option value="">Compare with…</option>
            {options(shownB)}
          </select>
        </>
      )}
      <button
        type="button"
        className="btn btn-ghost btn-sm btn-square"
        title="List the saved captures again"
        aria-label="Refresh the list of captures"
        onClick={() => void reload('runtime')}
      >
        <Icon name="refresh" className="size-3.5" />
      </button>
    </div>
  )
}

function NoCapture({ captures }: { captures: CaptureEntry[] }) {
  return (
    <EmptyState
      icon="gauge"
      title={captures.length ? 'Choose a capture' : 'No runtime capture yet'}
      action={
        <button type="button" className="btn btn-sm" onClick={() => void reload('runtime')}>
          <Icon name="refresh" className="size-3.5" />
          Refresh
        </button>
      }
    >
      {captures.length ? (
        <ul className="mt-2 space-y-1 text-left">
          {captures.map((c) => (
            <li key={c.id}>
              <a className="link link-hover font-mono" href={routeHref('runtime', { capture: c.id })}>
                {c.id}
              </a>{' '}
              <span className="text-fg-3">{captureLabel(c)}</span>
            </li>
          ))}
        </ul>
      ) : (
        <div className="space-y-2 text-left">
          <p>
            Captures measure a running Go process of this repository: CPU and memory from /proc, every HTTP request
            through a local proxy, and optional profiles. They are saved for this repository and listed here.
          </p>
          <pre className="rounded-box border border-line bg-code px-3 py-2 font-mono text-xs leading-5 text-fg">
            {'gitview runtime ps              # the Go processes of this user\n' +
              'gitview runtime capture <pid>   # proxy + collector; Enter ends\n' +
              'gitview runtime capture <pid> --profile cpu,heap --yes'}
          </pre>
          <p>
            In the TUI, <kbd className="kbd kbd-xs">m</kbd> opens the Runtime Performance Inspector. Then refresh this
            page.
          </p>
        </div>
      )}
    </EmptyState>
  )
}
