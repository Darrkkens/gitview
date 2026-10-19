// HTTP Explorer (Architecture Journey): the endpoints the analysis found,
// the code each one may run (route → middlewares → handler → services →
// repositories → SQL) drawn as a journey, and the route's contract in the
// inspector with a request builder. Static analysis only: nothing is run or
// sent.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Icon } from '../../components/Icon'
import { Panes } from '../../components/Panes'
import { StatusSlot } from '../../components/Slots'
import { SectionGate } from '../../components/States'
import { offered, usePeek } from '../../lib/data'
import { cx, plural } from '../../lib/format'
import { navigate, setParams } from '../../lib/router'
import type { ViewProps } from '../../views'
import { buildJourney, JOURNEY_RELS, type JourneyEdge, type JourneyOpts, type JourneyView } from './build'
import { Endpoints, type ListState } from './Endpoints'
import { CONF_HELP, CONFS, LAYER_COLOR, SPINE, decodeGraph, displayName, kindColor, type Graph } from './graph'
import { JourneyCanvas, type Box, type CanvasApi, type Edge, type Highlight } from './journey/Canvas'
import { Chip, Crumbs, HelpDialog, Legend, StageState, Stepper, Toolbar, type Crumb } from './journey/Chrome'
import { readPref, toggleFullscreen, useJourneyKeys, writePref } from './journey/keys'
import { adjacency, highlightFrom } from './journey/highlight'
import { EdgeInspector, HttpSummary } from './Panels'
import { ROUTE_TABS } from './helpers'
import { RouteInspector } from './RouteInspector'
import type { Snippets } from './sourceData'
import { StepInspector, type StepNav } from './StepInspector'
import type { RawGraph } from './types'
import { Btn } from './ui'

/** The bits of the security section the HTTP view reads (when present). */
interface SecurityPeek {
  audit?: { routes?: { id: string; findings?: string[] }[]; findings?: { id: string; severity: string }[] }
  snippets?: Snippets
}

// Per-viewer conveniences kept while the page is open.
let listMemo: ListState = { q: '', methods: {}, group: 'module', filter: '' }
let stepTabMemo = 'overview'
let bodyViewMemo = 'example'

const narrow = () => window.innerWidth <= 820

export default function HttpView({ params }: ViewProps) {
  return (
    <SectionGate<RawGraph> section="graph" loadingLabel="Analysing the repository…">
      {(raw) => <Http raw={raw} params={params} />}
    </SectionGate>
  )
}

function LayerStrip({
  view,
  active,
  onPick,
}: {
  view: JourneyView
  active: string | null
  onPick: (l: string | null) => void
}) {
  const present = Object.keys(view.layers)
  const steps = ['Route', ...SPINE, ...present.filter((l) => l !== 'Route' && !SPINE.includes(l))]
  return (
    <div className="flex flex-wrap items-center gap-1 print:hidden" aria-label="Layers">
      {steps.map((l, k) => {
        const n = view.layers[l] || 0
        return (
          <span key={l} className="flex items-center gap-1">
            {k > 0 && <span className="text-[11px] text-fg-3">{k <= SPINE.length ? '→' : '·'}</span>}
            <button
              type="button"
              aria-pressed={active === l}
              disabled={!n}
              title={
                n
                  ? n + ' step(s) classified as ' + l + ' in the drawn graph; click to highlight them'
                  : 'No step classified as ' +
                    l +
                    ' is reachable in the drawn graph' +
                    (l === 'Route' ? '' : ' (raise the depth or expand a step)')
              }
              className={cx(
                'inline-flex h-6 items-center gap-1.5 rounded-full border bg-base-100/95 px-2 text-[11px] whitespace-nowrap shadow-sm',
                !n
                  ? 'cursor-default border-dashed border-line text-fg-3 opacity-60'
                  : active === l
                    ? 'border-line-strong bg-surface-3 text-fg'
                    : 'border-line text-fg-2 hover:border-line-strong hover:text-fg',
              )}
              onClick={() => onPick(active === l ? null : l)}
            >
              <span
                className="size-2 rounded-[2px]"
                style={{ background: 'var(--' + (LAYER_COLOR[l] ?? 'k-group') + ')' }}
              />
              {l === 'Handler' ? 'Handler (controller)' : l}
              <span className="text-fg-3 tabular-nums">{n}</span>
              {!n && <span className="sr-only">absent</span>}
            </button>
          </span>
        )
      })}
    </div>
  )
}

function FiltersMenu({
  rels,
  confs,
  setRels,
  setConfs,
  counts,
}: {
  rels: Record<string, boolean>
  confs: Record<string, boolean>
  setRels: (r: Record<string, boolean>) => void
  setConfs: (c: Record<string, boolean>) => void
  counts: Record<string, number>
}) {
  const changed = Object.values(rels).some((x) => x === false) || Object.values(confs).some((x) => x === false)
  return (
    <div className="dropdown dropdown-end">
      <button
        type="button"
        tabIndex={0}
        className={cx('btn btn-ghost btn-xs btn-square relative', changed && 'text-primary')}
        title="Filters (relation types, confidence)"
        aria-label="Filters"
      >
        <Icon name="filter" className="size-3.5" />
        {changed && <span className="absolute top-0.5 right-0.5 size-1.5 rounded-full bg-primary" />}
      </button>
      <div
        tabIndex={0}
        className="dropdown-content z-20 mt-1 w-60 rounded-box border border-line bg-base-100 p-3 text-xs shadow-lg"
      >
        <div className="mb-1.5 text-[11px] font-semibold tracking-wide text-fg-3 uppercase">Relations followed</div>
        {JOURNEY_RELS.map((r) => (
          <label key={r} className="flex cursor-pointer items-center gap-2 py-0.5">
            <input
              type="checkbox"
              className="checkbox checkbox-xs"
              checked={rels[r] !== false}
              onChange={(e) => setRels({ ...rels, [r]: e.target.checked })}
            />
            <span className="flex-1">{r === 'go' ? 'goroutines' : r}</span>
            <span className="text-fg-3 tabular-nums">{counts[r] ?? 0}</span>
          </label>
        ))}
        <div className="mt-2.5 mb-1.5 text-[11px] font-semibold tracking-wide text-fg-3 uppercase">Confidence</div>
        {CONFS.map((c) => (
          <label key={c} className="flex cursor-pointer items-center gap-2 py-0.5" title={CONF_HELP[c]}>
            <input
              type="checkbox"
              className="checkbox checkbox-xs"
              checked={confs[c] !== false}
              onChange={(e) => setConfs({ ...confs, [c]: e.target.checked })}
            />
            <span className="size-2 rounded-full" style={{ background: 'var(--c-' + c + ')' }} />
            <span className="flex-1">{c}</span>
          </label>
        ))}
        {changed && (
          <button
            type="button"
            className="btn btn-ghost btn-xs mt-2 w-full"
            onClick={() => {
              setRels({})
              setConfs({})
            }}
          >
            Reset filters
          </button>
        )}
      </div>
    </div>
  )
}

function Http({ raw, params }: { raw: RawGraph; params: URLSearchParams }) {
  const g: Graph = useMemo(() => decodeGraph(raw), [raw])
  const routeParam = params.get('route') ?? ''
  const k = useMemo(() => {
    if (!routeParam) return -1
    const byId = g.routeById.get(routeParam)
    if (byId !== undefined) return byId
    // a route key ("POST /api/clients") also opens it
    return g.routes.findIndex((r) => r.key === routeParam)
  }, [g, routeParam])
  const r = k >= 0 ? g.routes[k] : undefined
  const root = k >= 0 ? g.nodeOfRoute[k] : -1
  const dp = Number(params.get('depth') ?? 3)
  const depth = Number.isFinite(dp) ? Math.max(0, Math.min(8, Math.round(dp))) : 3
  const tab = ROUTE_TABS.includes(params.get('tab') ?? '') ? params.get('tab')! : 'overview'
  const selParam = params.get('sel')

  // ---------- local state ----------
  const [list, setList0] = useState<ListState>(listMemo)
  const setList = (s: ListState) => {
    listMemo = s
    setList0(s)
  }
  const [stepTab, setStepTab0] = useState(stepTabMemo)
  const setStepTab = (t: string) => {
    stepTabMemo = t
    setStepTab0(t)
  }
  const [bodyView, setBodyView0] = useState(bodyViewMemo)
  const setBodyView = (v: string) => {
    bodyViewMemo = v
    setBodyView0(v)
  }
  const [exp, setExp] = useState<Record<number, boolean>>({})
  const [ext, setExt] = useState(false)
  const [rels, setRels] = useState<Record<string, boolean>>({})
  const [confs, setConfs] = useState<Record<string, boolean>>({})
  const [layerPick, setLayerPick] = useState<string | null>(null)
  const [selEdge, setSelEdge] = useState<string | null>(null)
  const [cleared, setCleared] = useState(false)
  const [anchor, setAnchor] = useState<string | null>(null)
  const [showLeft, setShowLeft] = useState(() => !narrow() && readPref('sidebar', true))
  const [showRight, setShowRight] = useState(() => !narrow() && readPref('inspector', true))
  const [minimap, setMinimap] = useState(() => readPref('minimap', true))
  const [help, setHelp] = useState(false)
  const [view, setView] = useState<JourneyView | null>(null)
  const [building, setBuilding] = useState(false)
  const [error, setError] = useState<{ route: number; msg: string } | null>(null)
  const canvas = useRef<CanvasApi>(null)
  const search = useRef<HTMLInputElement>(null)

  // a new route starts collapsed to the chosen depth, with nothing picked
  const routeId = r?.id ?? ''
  const [prevRoute, setPrevRoute] = useState(routeId)
  if (prevRoute !== routeId) {
    setPrevRoute(routeId)
    setExp({})
    setLayerPick(null)
    setSelEdge(null)
    setCleared(false)
    setAnchor(null)
  }

  // ---------- journey ----------
  const opts: JourneyOpts = useMemo(() => ({ depth, exp, ext, rels, confs }), [depth, exp, ext, rels, confs])
  useEffect(() => {
    if (k < 0) return
    let live = true
    const slow = setTimeout(() => live && setBuilding(true), 150)
    buildJourney(g, k, opts).then(
      (v) => {
        if (!live) return
        clearTimeout(slow)
        setBuilding(false)
        setError(null)
        setView(v)
      },
      (e: Error) => {
        if (!live) return
        clearTimeout(slow)
        setBuilding(false)
        setError({ route: k, msg: e.message || String(e) })
      },
    )
    return () => {
      live = false
      clearTimeout(slow)
    }
  }, [g, k, opts])
  const shown = view && view.route === k ? view : null
  const adj = useMemo(() => (shown ? adjacency(shown.scene) : null), [shown])

  // ---------- selection ----------
  const selNode = useMemo(() => {
    if (k < 0) return -1
    if (selParam) return g.byId.get(selParam) ?? -1
    return cleared ? -1 : root
  }, [g, k, selParam, cleared, root])
  const edge: JourneyEdge | undefined = selEdge && shown ? shown.edges.find((e) => e.key === selEdge) : undefined
  const selKey = edge ? edge.key : shown && selNode >= 0 ? shown.keyOf(selNode) : null

  const selectNode = useCallback(
    (i: number) => {
      setSelEdge(null)
      setLayerPick(null)
      setCleared(false)
      setParams({ sel: i === root ? undefined : g.N[i].id })
    },
    [g, root],
  )
  const nav: StepNav = useMemo(
    () => ({
      select: (i: number) => {
        const key = shown?.keyOf(i)
        if (!key) return false
        selectNode(i)
        canvas.current?.reveal([key])
        return true
      },
    }),
    [shown, selectNode],
  )
  const clearSelection = () => {
    setSelEdge(null)
    setLayerPick(null)
    setCleared(true)
    if (selParam) setParams({ sel: undefined })
  }
  const toggleStep = (i: number) => {
    if (!shown || i === shown.root) return
    // a step with hidden callees opens; an open one collapses
    setExp({ ...exp, [i]: shown.more.has(i) })
    setAnchor('h' + i)
    selectNode(i)
  }

  const openRoute = (j: number) => {
    const p: Record<string, string | number | undefined> = { route: g.routes[j].id }
    if (depth !== 3) p.depth = depth
    if (tab !== 'overview') p.tab = tab
    navigate('http', p)
  }

  const highlight: Highlight | null = useMemo(() => {
    if (!shown) return null
    if (layerPick) {
      const boxes = new Map<string, string>()
      shown.scene.boxes.forEach((b) => b.layer === layerPick && boxes.set(b.key, 'sel'))
      return { boxes, edges: new Map() }
    }
    if (edge)
      return {
        boxes: new Map([
          [edge.s, 'sel'],
          [edge.t, 'sel'],
        ]),
        edges: new Map([[edge.key, 'out']]),
      }
    if (!selKey || !adj) return null
    return highlightFrom(shown.scene, adj, selKey)
  }, [shown, adj, layerPick, edge, selKey])

  // ---------- security of the page (when it has one) ----------
  const secPeek = usePeek<SecurityPeek>('security')
  const sec = secPeek?.status === 'ready' ? secPeek.data : null
  const hasSecurity = offered('security')
  const findingsByRoute = useMemo(() => {
    const sev = new Map((sec?.audit?.findings ?? []).map((f) => [f.id, f.severity]))
    const rank: Record<string, number> = { critical: 5, high: 4, medium: 3, low: 2, info: 1 }
    const m = new Map<string, { n: number; worst: string }>()
    for (const p of sec?.audit?.routes ?? []) {
      let worst = ''
      for (const id of p.findings ?? []) {
        const s = sev.get(id) ?? ''
        if ((rank[s] ?? 0) > (rank[worst] ?? 0)) worst = s
      }
      m.set(p.id, { n: (p.findings ?? []).length, worst })
    }
    return m
  }, [sec])
  const snippets = sec?.snippets

  // ---------- keys ----------
  const togglePane = (side: 'left' | 'right') => {
    // on a narrow screen one panel at a time leaves room for the canvas
    if (side === 'left') {
      writePref('sidebar', !showLeft)
      setShowLeft(!showLeft)
      if (!showLeft && narrow()) setShowRight(false)
    } else {
      writePref('inspector', !showRight)
      setShowRight(!showRight)
      if (!showRight && narrow()) setShowLeft(false)
    }
  }
  const toggleMinimap = () => {
    writePref('minimap', !minimap)
    setMinimap(!minimap)
  }
  useJourneyKeys({
    fit: () => canvas.current?.fit(),
    zoom: (f) => canvas.current?.zoomBy(f),
    minimap: toggleMinimap,
    left: () => togglePane('left'),
    right: () => togglePane('right'),
    help: () => setHelp(true),
    escape: () => {
      if (selKey || layerPick) clearSelection()
    },
    search: () => {
      if (!showLeft) setShowLeft(true)
      setTimeout(() => search.current?.focus(), 0)
    },
    enter: () => {
      if (!edge && selNode >= 0 && selNode !== root) toggleStep(selNode)
    },
  })

  // ---------- canvas events ----------
  const onBox = (b: Box) => {
    if (b.ref === undefined) return
    if (b.ref === root) {
      setSelEdge(null)
      setLayerPick(null)
      setCleared(false)
      if (selParam) setParams({ sel: undefined })
      return
    }
    selectNode(b.ref)
  }
  const onBoxDouble = (b: Box) => {
    if (b.ref !== undefined) toggleStep(b.ref)
  }
  const onEdge = (e: Edge) => {
    setLayerPick(null)
    setSelEdge(e.key)
  }

  // ---------- inspector ----------
  const relCounts = useMemo(() => {
    const c: Record<string, number> = {}
    for (const e of g.E)
      if (JOURNEY_RELS.includes(e.type as (typeof JOURNEY_RELS)[number])) c[e.type] = (c[e.type] || 0) + 1
    return c
  }, [g])
  const security = hasSecurity && r ? { count: sec ? (findingsByRoute.get(r.id)?.n ?? 0) : null } : undefined
  let inspector
  if (edge) inspector = <EdgeInspector g={g} e={edge} nav={nav} />
  else if (!r) inspector = <HttpSummary g={g} />
  else if (selNode >= 0 && selNode !== root)
    inspector = (
      <StepInspector
        g={g}
        i={selNode}
        nav={nav}
        tab={stepTab}
        setTab={setStepTab}
        snippets={snippets}
        actions={
          shown && shown.keyOf(selNode) && selNode !== shown.root ? (
            <Btn
              icon={shown.more.has(selNode) ? 'chevronDown' : 'chevronRight'}
              title="Open or collapse its calls on the canvas (Enter)"
              onClick={() => toggleStep(selNode)}
            >
              {shown.more.has(selNode) ? 'Show calls' : 'Collapse calls'}
            </Btn>
          ) : null
        }
      />
    )
  else
    inspector = (
      <RouteInspector
        g={g}
        k={k}
        view={shown}
        tab={tab}
        setTab={(t) => setParams({ tab: t === 'overview' ? undefined : t })}
        nav={nav}
        depth={depth}
        collapseAll={() => {
          setExp({})
          setParams({ depth: 0 })
        }}
        snippets={snippets}
        security={security}
        bodyView={bodyView}
        setBodyView={setBodyView}
      />
    )

  // ---------- chrome ----------
  const crumbs: Crumb[] = [
    {
      label: 'HTTP',
      title: 'All endpoints',
      onClick: () => navigate('http'),
    },
  ]
  if (r)
    crumbs.push({
      label: r.key,
      title: r.id,
      color: 'l-route',
      onClick: () => {
        setSelEdge(null)
        setLayerPick(null)
        setCleared(false)
        setParams({ sel: undefined })
      },
    })
  if (r && selNode >= 0 && selNode !== root)
    crumbs.push({ label: displayName(g.N[selNode]), color: kindColor(g.N[selNode]) })

  const lineConfs = useMemo(() => [...new Set((shown?.scene.edges ?? []).map((e) => e.conf))], [shown])
  const hl = highlight as (Highlight & { down?: number; up?: number }) | null
  const comp = g.raw.completeness

  return (
    <>
      <Panes
        id="http"
        leftWidth={264}
        rightWidth={380}
        showLeft={showLeft}
        showRight={showRight}
        left={
          <Endpoints
            g={g}
            current={k}
            state={list}
            setState={setList}
            onOpen={openRoute}
            inputRef={search}
            findingsOf={sec ? (id) => findingsByRoute.get(id) : undefined}
          />
        }
        right={inspector}
      >
        <JourneyCanvas
          scene={shown?.scene ?? null}
          selected={selKey}
          highlight={highlight}
          fitKey={routeId + '|' + depth + '|' + ext + '|' + JSON.stringify(rels) + JSON.stringify(confs)}
          fitOpts={shown ? { minZoom: 0.62, anchor: 'h' + shown.root, align: 'left', top: 110 } : undefined}
          anchor={anchor}
          minimap={minimap}
          onBox={onBox}
          onBoxDouble={onBoxDouble}
          onEdge={onEdge}
          onEmpty={clearSelection}
          apiRef={canvas}
          label="HTTP journey. Use the endpoint list and the inspector to navigate it with the keyboard."
        >
          <div className="pointer-events-none absolute inset-x-3 top-3 flex items-start gap-3 @max-2xl:flex-col-reverse @max-2xl:gap-2">
            <div className="pointer-events-auto flex min-w-0 flex-1 flex-col items-start gap-2 @max-2xl:w-full">
              <div className="max-w-full rounded-field border border-line bg-base-100/95 px-1 py-0.5 shadow-sm backdrop-blur">
                <Crumbs items={crumbs} />
              </div>
              {r && (
                <div className="flex flex-wrap items-center gap-1.5">
                  <Chip>
                    <Stepper
                      label="Depth"
                      value={depth}
                      min={0}
                      max={8}
                      title="Calls followed from the handler chain"
                      onChange={(v) => {
                        setExp({})
                        setParams({ depth: v === 3 ? undefined : v })
                      }}
                    />
                  </Chip>
                  <Chip>
                    <label className="flex cursor-pointer items-center gap-1.5">
                      <input
                        type="checkbox"
                        className="checkbox checkbox-xs"
                        checked={ext}
                        onChange={(e) => setExt(e.target.checked)}
                      />
                      external calls
                    </label>
                  </Chip>
                  {shown && shown.hidden > 0 && (
                    <Chip title="Double-click a step with +N to open it">
                      <span className="text-fg-3">{plural(shown.hidden, 'call')} collapsed</span>
                    </Chip>
                  )}
                </div>
              )}
              {shown && <LayerStrip view={shown} active={layerPick} onPick={(l) => setLayerPick(l)} />}
            </div>
            <div className="pointer-events-auto @max-2xl:self-end">
              <Toolbar
                onZoomIn={() => canvas.current?.zoomBy(1.25)}
                onZoomOut={() => canvas.current?.zoomBy(0.8)}
                onFit={() => canvas.current?.fit()}
                minimap={minimap}
                onMinimap={toggleMinimap}
                onFullscreen={toggleFullscreen}
                onHelp={() => setHelp(true)}
                showLeft={showLeft}
                onLeft={() => togglePane('left')}
                showRight={showRight}
                onRight={() => togglePane('right')}
                extra={
                  <FiltersMenu rels={rels} confs={confs} setRels={setRels} setConfs={setConfs} counts={relCounts} />
                }
              />
            </div>
          </div>
          {shown && shown.scene.edges.length > 0 && (
            <div className="pointer-events-auto absolute bottom-3 left-3 max-w-[calc(100%-13rem)] @max-[35rem]:max-w-[calc(100%-1.5rem)]">
              <Legend
                confs={lineConfs}
                extra={
                  hl && (hl.down || hl.up)
                    ? [
                        { label: 'depends on', color: 'var(--hl-out)' },
                        { label: 'used by', color: 'var(--hl-in)' },
                      ]
                    : undefined
                }
              />
            </div>
          )}
          {k < 0 &&
            (routeParam ? (
              <StageState title="Route not found">
                No route of this repository has the ID or key “{routeParam}”. Pick an endpoint on the left.
              </StageState>
            ) : (
              <StageState title={g.routes.length ? 'Pick an endpoint' : 'No HTTP endpoint'}>
                {g.routes.length
                  ? 'The list on the left has every route the analysis found. Its journey through the code is drawn here.'
                  : 'The analysis found no route registration in this repository.'}
              </StageState>
            ))}
          {k >= 0 && root < 0 && (
            <StageState title="No journey">
              This route is not a node of the dataset, so its calls cannot be followed.
            </StageState>
          )}
          {k >= 0 && error?.route === k && <StageState title="This view could not be drawn">{error.msg}</StageState>}
          {k >= 0 && building && error?.route !== k && <StageState loading>Laying out…</StageState>}
        </JourneyCanvas>
      </Panes>
      <HelpDialog open={help} onClose={() => setHelp(false)} />
      <StatusSlot>
        <span>
          {shown
            ? 'HTTP journey · ' +
              plural(shown.scene.boxes.length, 'element') +
              ' · ' +
              plural(shown.scene.edges.length, 'line') +
              ' · layout ' +
              Math.round(shown.ms) +
              ' ms'
            : 'HTTP · ' + plural(g.routes.length, 'route')}
        </span>
        {comp && comp.status !== 'complete' && (
          <span className="text-warning" title={(comp.reasons ?? []).slice(0, 10).join('\n')}>
            analysis {comp.status}
          </span>
        )}
      </StatusSlot>
    </>
  )
}
