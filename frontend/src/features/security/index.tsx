// Security Journey (CodeAudit): the findings of the deterministic security
// rules, the path from a route to each one drawn and coloured by state
// (always with the state in text), and in the inspector the evidence, code,
// data path, fixes, the AI investigations that were asked for and the
// review decisions. Asking the model and recording decisions need
// `gitview serve`; an exported report is read-only and offline.

import { useEffect, useMemo, useRef, useState } from 'react'
import { Panes } from '../../components/Panes'
import { StatusSlot } from '../../components/Slots'
import { SectionGate } from '../../components/States'
import { toast } from '../../components/Toast'
import { useSection } from '../../lib/data'
import { plural } from '../../lib/format'
import { navigate, setParams } from '../../lib/router'
import type { ViewProps } from '../../views'
import { decodeGraph, displayName, kindColor, type Graph } from '../http/graph'
import { JourneyCanvas, type Box, type CanvasApi, type Edge, type Highlight } from '../http/journey/Canvas'
import { Chip, Crumbs, HelpDialog, Legend, StageState, Toolbar, type Crumb } from '../http/journey/Chrome'
import { readPref, toggleFullscreen, useJourneyKeys, writePref } from '../http/journey/keys'
import { adjacency, highlightFrom, pathKeys } from '../http/journey/highlight'
import { StepInspector } from '../http/StepInspector'
import type { RawGraph } from '../http/types'
import { buildSecurity, type SecView } from './build'
import { FindingInspector, type SecNav } from './FindingInspector'
import { FINDING_TABS, useSec } from './model'
import { PathInspector, RoutePosture, SecuritySummary, type PickedPath } from './Panels'
import { SDot } from './parts'
import { Report } from './Report'
import { Sidebar, type ListKind, type SideState } from './Sidebar'
import type { SecurityPage } from './types'

let sideMemo: SideState = { q: '', sev: {}, cat: '', state: '' }
let stepTabMemo = 'overview'

const narrow = () => window.innerWidth <= 820
const LISTS: ListKind[] = ['findings', 'routes', 'rules']

export default function SecurityView({ params }: ViewProps) {
  return (
    <SectionGate<SecurityPage> section="security" loadingLabel="Running the security audit…">
      {(page) => <Security page={page} params={params} />}
    </SectionGate>
  )
}

function Security({ page, params }: { page: SecurityPage; params: URLSearchParams }) {
  const sec = useSec(page)
  const graphState = useSection<RawGraph>('graph')
  const g: Graph | null = useMemo(
    () => (graphState.status === 'ready' ? decodeGraph(graphState.data) : null),
    [graphState],
  )

  const findingParam = params.get('finding')
  const routeParam = params.get('route')
  const finding = findingParam && sec.byId.has(findingParam) ? findingParam : null
  const route = !finding && routeParam && sec.postureOf.has(routeParam) ? routeParam : null
  const tab = FINDING_TABS.includes(params.get('tab') ?? '') ? params.get('tab')! : 'overview'
  const list: ListKind = LISTS.includes(params.get('list') as ListKind)
    ? (params.get('list') as ListKind)
    : route
      ? 'routes'
      : 'findings'
  const selParam = params.get('sel')
  const selNode = selParam && g ? (g.byId.get(selParam) ?? -1) : -1

  const [side, setSide0] = useState<SideState>(sideMemo)
  const setSide = (s: SideState) => {
    sideMemo = s
    setSide0(s)
  }
  const [stepTab, setStepTab0] = useState(stepTabMemo)
  const setStepTab = (t: string) => {
    stepTabMemo = t
    setStepTab0(t)
  }
  const [pick, setPick] = useState<string | null>(null)
  const [path, setPath] = useState<PickedPath | null>(null)
  const [selEdge, setSelEdge] = useState<string | null>(null)
  const [report, setReport] = useState(false)
  const [showLeft, setShowLeft] = useState(() => !narrow() && readPref('sidebar', true))
  const [showRight, setShowRight] = useState(() => !narrow() && readPref('inspector', true))
  const [minimap, setMinimap] = useState(() => readPref('minimap', true))
  const [help, setHelp] = useState(false)
  const [view, setView] = useState<{ subject: string; v: SecView | null } | null>(null)
  const [building, setBuilding] = useState(false)
  const [error, setError] = useState('')
  const canvas = useRef<CanvasApi>(null)
  const search = useRef<HTMLInputElement>(null)

  // a new subject starts with nothing picked
  const subject = finding ? 'f:' + finding : route ? 'r:' + route : ''
  const [prevSubject, setPrevSubject] = useState(subject)
  if (prevSubject !== subject) {
    setPrevSubject(subject)
    setPick(null)
    setPath(null)
    setSelEdge(null)
  }

  // ---------- graph ----------
  useEffect(() => {
    if (!subject) return
    let live = true
    const slow = setTimeout(() => live && setBuilding(true), 150)
    buildSecurity(sec, g, finding, route).then(
      (v) => {
        if (!live) return
        clearTimeout(slow)
        setBuilding(false)
        setError('')
        setView({ subject, v })
      },
      (e: Error) => {
        if (!live) return
        clearTimeout(slow)
        setBuilding(false)
        setError(e.message || String(e))
      },
    )
    return () => {
      live = false
      clearTimeout(slow)
    }
  }, [sec, g, finding, route, subject])
  const shown = subject && view && view.subject === subject ? view.v : null
  const adj = useMemo(() => (shown ? adjacency(shown.scene) : null), [shown])
  const boxOf = (k: string) => shown?.scene.boxes.find((b) => b.key === k)

  // ---------- navigation ----------
  const listParam = list === 'findings' ? undefined : list
  const openFinding = (id: string) =>
    navigate('security', { finding: id, tab: tab === 'overview' ? undefined : tab, list: listParam })
  const openRoute = (id: string) => navigate('security', { route: id, list: listParam })
  const selKey = shown && selNode >= 0 ? shown.keyOf(selNode) : null
  const nav: SecNav = {
    select: (i: number) => {
      const key = shown?.keyOf(i)
      if (!key || !g) return false
      setPath(null)
      setPick(key)
      setSelEdge(null)
      setParams({ sel: g.N[i].id })
      canvas.current?.reveal([key])
      return true
    },
    route: openRoute,
    finding: openFinding,
    tab: (t: string) => setParams({ tab: t === 'overview' ? undefined : t }),
  }
  const clearSel = () => {
    setSelEdge(null)
    if (selParam) setParams({ sel: undefined })
  }

  // ---------- canvas events ----------
  const onBox = (b: Box, ev: { shiftKey: boolean }) => {
    if (b.findingId) {
      setPath(null)
      setPick(null)
      setSelEdge(null)
      if (finding !== b.findingId) openFinding(b.findingId)
      else if (selParam) setParams({ sel: undefined })
      return
    }
    if (ev.shiftKey && pick && pick !== b.key && adj) {
      const keys = pathKeys(adj, pick, b.key) ?? pathKeys(adj, b.key, pick)
      const ends = keys ? [boxOf(keys[0]), boxOf(keys[keys.length - 1])] : []
      if (!keys || !g || ends.some((x) => !x || x.ref === undefined)) {
        toast('No path between these steps in the drawn graph.')
        return
      }
      setPath({
        from: g.N[ends[0]!.ref!].id,
        to: g.N[ends[1]!.ref!].id,
        keys,
        names: keys.map((k) => boxOf(k)?.title ?? k),
      })
      clearSel()
      return
    }
    setPath(null)
    setPick(b.key)
    setSelEdge(null)
    if (b.ref !== undefined && g) setParams({ sel: g.N[b.ref].id })
    else if (selParam) setParams({ sel: undefined })
  }
  const onEdge = (e: Edge) => setSelEdge(e.key)
  const onEmpty = () => clearSel()

  const highlight: Highlight | null = useMemo(() => {
    if (!shown) return null
    if (path) {
      const boxes = new Map(path.keys.map((k) => [k, 'sel']))
      const edges = new Map<string, string>()
      for (let k = 1; k < path.keys.length; k++)
        for (const e of adj?.out.get(path.keys[k - 1]) ?? []) if (e.t === path.keys[k]) edges.set(e.key, 'out')
      return { boxes, edges }
    }
    const e = selEdge ? shown.scene.edges.find((x) => x.key === selEdge) : undefined
    if (e)
      return {
        boxes: new Map([
          [e.s, 'sel'],
          [e.t, 'sel'],
        ]),
        edges: new Map([[e.key, 'out']]),
      }
    if (selKey && adj) return highlightFrom(shown.scene, adj, selKey)
    return null
  }, [shown, adj, path, selEdge, selKey])

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
      if (report) setReport(false)
      else if (selParam || selEdge) clearSel()
      else if (path || pick) {
        setPath(null)
        setPick(null)
      }
    },
    search: () => {
      if (!showLeft) setShowLeft(true)
      if (list === 'rules') setParams({ list: undefined })
      setTimeout(() => search.current?.focus(), 0)
    },
    blocked: () => report,
  })

  // ---------- inspector ----------
  const f = finding ? sec.byId.get(finding)! : null
  let inspector
  if (g && selNode >= 0 && selKey)
    inspector = (
      <StepInspector
        g={g}
        i={selNode}
        nav={nav}
        tab={stepTab}
        setTab={setStepTab}
        snippets={page.snippets ?? undefined}
      />
    )
  else if (path) inspector = <PathInspector sec={sec} path={path} onClear={() => (setPath(null), setPick(null))} />
  else if (f)
    inspector = <FindingInspector sec={sec} g={g} f={f} tab={tab} nav={nav} onReport={() => setReport(true)} />
  else if (route) inspector = <RoutePosture sec={sec} g={g} id={route} current={finding} onFinding={openFinding} />
  else inspector = <SecuritySummary sec={sec} onReport={() => setReport(true)} />

  // ---------- chrome ----------
  const crumbs: Crumb[] = [
    { label: 'CodeAudit', title: 'Security Journey', onClick: () => navigate('security', { list: listParam }) },
  ]
  if (f)
    crumbs.push({
      label: f.title,
      title: f.id,
      color: sec.stateOf(f).token,
      onClick: () => {
        setPath(null)
        clearSel()
      },
    })
  if (route)
    crumbs.push({
      label: route.replace(/^route:/, '').replace(/@.*$/, ''),
      title: route,
      color: 'l-route',
      onClick: clearSel,
    })
  if (g && selNode >= 0 && selKey) crumbs.push({ label: displayName(g.N[selNode]), color: kindColor(g.N[selNode]) })

  const confs = useMemo(() => [...new Set((shown?.scene.edges ?? []).map((e) => e.conf))], [shown])
  const extra = [
    ...(confs.includes('chain') ? [{ label: 'next in chain', color: 'var(--edge-strong)', dash: 'chain' }] : []),
    ...(confs.includes('finding') ? [{ label: 'finding', color: 'var(--s-review)', dash: 'finding' }] : []),
  ]
  const A = page.audit

  return (
    <>
      <Panes
        id="security"
        leftWidth={272}
        rightWidth={390}
        showLeft={showLeft}
        showRight={showRight}
        left={
          <Sidebar
            sec={sec}
            list={list}
            setList={(l) => setParams({ list: l === 'findings' ? undefined : l })}
            side={side}
            setSide={setSide}
            finding={finding}
            route={route}
            onFinding={openFinding}
            onRoute={openRoute}
            inputRef={search}
          />
        }
        right={inspector}
      >
        <JourneyCanvas
          scene={shown?.scene ?? null}
          selected={selEdge ?? selKey ?? pick}
          highlight={highlight}
          fitKey={subject + '|' + (g ? 'g' : '')}
          fitOpts={shown ? { minZoom: 0.62, anchor: shown.root, align: 'topleft', top: 64 } : undefined}
          minimap={minimap}
          onBox={onBox}
          onEdge={onEdge}
          onEmpty={onEmpty}
          apiRef={canvas}
          label="Security Journey graph. Use the list and the inspector to navigate it with the keyboard."
        >
          <div className="pointer-events-none absolute inset-x-3 top-3 flex items-start gap-3 @max-2xl:flex-col-reverse @max-2xl:gap-2">
            <div className="pointer-events-auto flex min-w-0 flex-1 flex-col items-start gap-2 @max-2xl:w-full">
              <div className="max-w-full rounded-field border border-line bg-base-100/95 px-1 py-0.5 shadow-sm backdrop-blur">
                <Crumbs items={crumbs} />
              </div>
              {shown &&
                (path ? (
                  <Chip className="border-primary/50">
                    <strong className="font-medium">
                      Path: {path.names[0]} → {path.names[path.names.length - 1]}
                    </strong>
                    <button
                      type="button"
                      className="btn btn-ghost btn-xs btn-square"
                      title="Clear the path"
                      aria-label="Clear the path"
                      onClick={() => {
                        setPath(null)
                        setPick(null)
                      }}
                    >
                      ×
                    </button>
                  </Chip>
                ) : pick ? (
                  <Chip>Shift+click another step to explain the path from the selected one</Chip>
                ) : (
                  <Chip className="text-fg-3">
                    Click a step for its code; Shift+click a second step to explain the path between them
                  </Chip>
                ))}
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
              />
            </div>
          </div>
          {shown && (
            <div className="pointer-events-auto absolute bottom-3 left-3 flex max-w-[calc(100%-13rem)] flex-col items-start gap-1.5 @max-[35rem]:max-w-[calc(100%-1.5rem)]">
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-field border border-line bg-base-100/90 px-2 py-1 text-[11px] text-fg-2 shadow-sm backdrop-blur">
                {(
                  [
                    ['check', 'check'],
                    ['review', 'review required'],
                    ['vulnerable', 'vulnerable'],
                    ['incomplete', 'incomplete'],
                    ['struct', 'structural'],
                  ] as const
                ).map(([c, t]) => (
                  <span key={c} className="inline-flex items-center gap-1.5">
                    <SDot cls={c} className="size-2" />
                    {t}
                  </span>
                ))}
              </div>
              <Legend confs={confs} extra={extra} />
            </div>
          )}
          {!subject && !findingParam && !routeParam && (
            <StageState title={sec.F.length ? 'Pick a finding or a route' : 'No findings'}>
              {sec.F.length
                ? 'The list on the left has what the security rules found. The path from the route to the code is drawn here, with the evidence in the inspector.'
                : 'The enabled rules found nothing. The inspector shows the scope and the limits of the analysis.'}
            </StageState>
          )}
          {subject && error && <StageState title="This view could not be drawn">{error}</StageState>}
          {subject && building && !error && <StageState loading>Laying out…</StageState>}
          {!subject && (findingParam || routeParam) && (
            <StageState title="Not in this audit">
              {findingParam
                ? 'No finding has the ID “' + findingParam + '”.'
                : 'No route of the audit has the ID “' + routeParam + '”.'}{' '}
              Pick one on the left.
            </StageState>
          )}
        </JourneyCanvas>
      </Panes>
      <HelpDialog open={help} onClose={() => setHelp(false)} />
      {report && <Report sec={sec} onClose={() => setReport(false)} />}
      <StatusSlot>
        <span>
          {shown
            ? 'Security Journey · ' +
              plural(shown.scene.boxes.length, 'element') +
              ' · ' +
              plural(shown.scene.edges.length, 'line') +
              ' · layout ' +
              Math.round(shown.ms) +
              ' ms'
            : 'Security · ' + plural(sec.F.length, 'finding')}
        </span>
        {A.status.status !== 'complete' && (
          <span className="text-warning" title={(A.status.reasons ?? []).slice(0, 10).join('\n')}>
            analysis {A.status.status}
          </span>
        )}
        {sec.live && <span className="text-s-ai">live · AI and reviews available</span>}
      </StatusSlot>
    </>
  )
}
