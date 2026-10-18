// Architecture view (#/architecture): the Architecture Explorer of the
// Repository Graph. The repository's tree drawn as nested boxes on a canvas
// (ELK layouts, every relation lifted to what is drawn), the explorer tree
// on the left, the inspector on the right, global search, filters and focus
// mode. Deep links: #/architecture?node=<id> selects (and reveals) an
// element; scope, open, focus, up, down and refs keep the rest of the view.

import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from 'react'
import { Icon } from '../../components/Icon'
import { PaneHeader, Panes } from '../../components/Panes'
import { HeaderSlot, StatusSlot } from '../../components/Slots'
import { SectionGate } from '../../components/States'
import { toast } from '../../components/Toast'
import { debounce, isTyping } from '../../lib/browser'
import { offered, usePeek } from '../../lib/data'
import { fmt, plural } from '../../lib/format'
import { navigate } from '../../lib/router'
import type { ViewProps } from '../../views'
import { Explorer, hasState } from './controller'
import { Crumbs } from './Crumbs'
import { HelpModal } from './HelpModal'
import { Inspector } from './Inspector'
import { layoutStats } from './layout'
import { modelOf, type Model } from './model'
import { SearchModal } from './SearchModal'
import { Stage } from './Stage'
import { Tree } from './Tree'
import type { GraphSection, SecuritySectionPeek } from './types'
import { Kbd } from './ui'

export default function ArchitectureView({ params }: ViewProps) {
  return (
    <SectionGate<GraphSection> section="graph">
      {(raw) => <ArchitectureExplorer raw={raw} params={params} />}
    </SectionGate>
  )
}

function pref(key: string, def: boolean): boolean {
  try {
    const v = localStorage.getItem('gitview.arch.' + key)
    return v === null ? def : v === '1'
  } catch {
    return def
  }
}
function savePref(key: string, v: boolean) {
  try {
    localStorage.setItem('gitview.arch.' + key, v ? '1' : '0')
  } catch {
    /* per-viewer convenience only */
  }
}

// The explorer of each dataset outlives the view, so its state survives a
// visit to another view.
const explorers = new WeakMap<Model, Explorer>()
function explorerOf(D: Model): Explorer {
  let x = explorers.get(D)
  if (!x) {
    x = new Explorer(D)
    explorers.set(D, x)
  }
  return x
}

// On a narrow screen the panes would leave no room to the canvas: they
// start closed, and opening one closes the other.
const narrow = () => window.innerWidth <= 820
const cramped = () => window.innerWidth < 960

function ArchitectureExplorer({ raw, params }: { raw: GraphSection; params: URLSearchParams }) {
  const D = useMemo(() => modelOf(raw), [raw])
  const ctrl = useMemo(() => explorerOf(D), [D])
  const snap = useSyncExternalStore(ctrl.subscribe, ctrl.getSnapshot)
  const sec = usePeek<SecuritySectionPeek>('security')
  const security = sec?.status === 'ready' ? sec.data : null
  const securityOffered = offered('security')

  const [sidebar, setSidebar] = useState(() => !narrow() && pref('sidebar', true))
  const [inspector, setInspector] = useState(() => !narrow() && pref('inspector', true))
  const [minimap, setMinimap] = useState(() => pref('minimap', true))
  const [wide] = useState(() => window.innerWidth > 1100)
  const [searchOpen, setSearchOpen] = useState(false)
  const [helpOpen, setHelpOpen] = useState(false)
  const [filtersOpen, setFiltersOpen] = useState(false)

  const showSidebar = useCallback((on: boolean) => {
    setSidebar(on)
    savePref('sidebar', on)
    if (on && cramped()) setInspector(false)
  }, [])
  const showInspector = useCallback((on: boolean) => {
    setInspector(on)
    savePref('inspector', on)
    if (on && cramped()) setSidebar(false)
  }, [])
  const toggleMinimap = useCallback(() => {
    setMinimap((m) => {
      savePref('minimap', !m)
      return !m
    })
  }, [])
  const toggleFullscreen = useCallback(() => {
    try {
      if (document.fullscreenElement) void document.exitFullscreen()
      else if (document.documentElement.requestFullscreen) void document.documentElement.requestFullscreen()
      else toast('Fullscreen is not available here.')
    } catch {
      toast('Fullscreen is not available here.')
    }
  }, [])

  useEffect(() => {
    ctrl.activate()
    return () => ctrl.deactivate()
  }, [ctrl])

  // The window got narrow: keep one pane at most, so the canvas stays usable.
  useEffect(() => {
    const onResize = debounce(() => {
      if (cramped()) setSidebar((s) => (s && inspector ? false : s))
    }, 200)
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [inspector])

  // The URL drives the view: restore whenever it differs from what the
  // explorer last wrote (first load, Back/Forward, links from other views).
  // #/architecture without parameters (the view's tab) comes back to the
  // state the explorer was left in.
  useEffect(() => {
    if (ctrl.isCurrent(params)) return
    if (!hasState(params) && ctrl.hasView()) ctrl.resume()
    else ctrl.restore(params)
  }, [ctrl, params])

  // keyboard shortcuts of the explorer
  useEffect(() => {
    const m = { searchOpen, helpOpen, filtersOpen }
    const onKey = (ev: KeyboardEvent) => {
      if ((ev.ctrlKey || ev.metaKey) && (ev.key === 'k' || ev.key === 'K')) {
        setHelpOpen(false)
        setSearchOpen(true)
        ev.preventDefault()
        return
      }
      if (ev.key === 'Escape') {
        if (m.searchOpen) return setSearchOpen(false)
        if (m.helpOpen) return setHelpOpen(false)
        if (m.filtersOpen) return setFiltersOpen(false)
        if (isTyping(ev)) return
        const S = ctrl.getSnapshot().S
        if (S.sel) ctrl.clearSelection()
        else if (S.focus) ctrl.exitFocus()
        return
      }
      if (isTyping(ev) || m.searchOpen || m.helpOpen) return
      if (ev.altKey && ev.key === 'ArrowLeft') {
        history.back()
        ev.preventDefault()
        return
      }
      if (ev.altKey && ev.key === 'ArrowRight') {
        history.forward()
        ev.preventDefault()
        return
      }
      if (ev.ctrlKey || ev.metaKey || ev.altKey) return
      const target = ev.target as HTMLElement | null
      const inTree = !!target?.closest?.('[data-arch-tree]')
      const onControl = !!target?.closest?.('button, a, summary, [role="tab"]')
      const k = ev.key
      if (k === '/') {
        setSearchOpen(true)
        ev.preventDefault()
      } else if (k === '2') navigate('http')
      else if (k === '3') {
        if (offered('security')) navigate('security')
        else toast('This page has no security audit: export it with gitview audit export.')
      } else if (k === 'f') ctrl.fitView()
      else if (k === 'F') toggleFullscreen()
      else if (k === '+' || k === '=') ctrl.zoomBy(1.25)
      else if (k === '-' || k === '_') ctrl.zoomBy(0.8)
      else if (k === 'm') toggleMinimap()
      else if (k === '.') ctrl.toggleFocus()
      else if (k === 'b') showSidebar(!sidebar)
      else if (k === 'i') showInspector(!inspector)
      else if (k === '?') setHelpOpen(true)
      else if (k === 'Backspace') {
        ctrl.up()
        ev.preventDefault()
      } else if (k === 'Enter' && !inTree && !onControl) ctrl.toggleSelection()
      else return
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [
    ctrl,
    searchOpen,
    helpOpen,
    filtersOpen,
    sidebar,
    inspector,
    showSidebar,
    showInspector,
    toggleMinimap,
    toggleFullscreen,
  ])

  // Dev and automated checks only: a read-only hook on the explorer.
  useEffect(() => {
    if (!import.meta.env.DEV) return
    ;(window as unknown as { __arch?: unknown }).__arch = { ctrl, D, layoutStats }
  }, [ctrl, D])

  return (
    <>
      <HeaderSlot>
        {/* the header's room varies with the views offered: adapt to it */}
        <div className="@container flex min-w-0 flex-1 items-center justify-end gap-1.5">
          <div className="hidden min-w-0 flex-1 @[300px]:flex">
            <Crumbs D={D} ctrl={ctrl} S={snap.S} />
          </div>
          <button
            type="button"
            className="btn btn-sm btn-ghost shrink-0 gap-2 border-line bg-base-200 px-2 font-normal text-fg-3 hover:text-fg-2"
            title="Search everything (Ctrl+K or /)"
            aria-label="Search"
            onClick={() => setSearchOpen(true)}
          >
            <Icon name="search" className="size-3.5" />
            <span className="hidden @[420px]:inline">Search</span>
            <span className="hidden gap-0.5 @[420px]:flex">
              <Kbd>Ctrl</Kbd>
              <Kbd>K</Kbd>
            </span>
          </button>
          <button
            type="button"
            className="btn btn-sm btn-ghost btn-square shrink-0"
            title="Keyboard shortcuts (?)"
            aria-label="Keyboard shortcuts"
            onClick={() => setHelpOpen(true)}
          >
            <Icon name="help" />
          </button>
        </div>
      </HeaderSlot>
      <StatusSlot>
        <Status D={D} snap={snap} />
      </StatusSlot>
      <Panes
        id="architecture"
        leftWidth={wide ? 288 : 240}
        rightWidth={wide ? 400 : 340}
        showLeft={sidebar}
        showRight={inspector}
        left={
          <>
            <PaneHeader title="Explorer">
              <button
                type="button"
                className="btn btn-ghost btn-xs btn-square"
                title="Hide the explorer (b)"
                aria-label="Hide the explorer"
                onClick={() => showSidebar(false)}
              >
                <Icon name="chevronLeft" className="size-3.5" />
              </button>
            </PaneHeader>
            <Tree D={D} ctrl={ctrl} sel={snap.S.sel} />
          </>
        }
        right={
          <Inspector
            D={D}
            ctrl={ctrl}
            snap={snap}
            security={security}
            securityOffered={securityOffered}
            onHide={() => showInspector(false)}
          />
        }
      >
        <Stage
          D={D}
          ctrl={ctrl}
          snap={snap}
          minimap={minimap}
          onMinimap={toggleMinimap}
          onFullscreen={toggleFullscreen}
          filtersOpen={filtersOpen}
          setFiltersOpen={setFiltersOpen}
          sidebar={sidebar}
          inspector={inspector}
          onSidebar={() => showSidebar(true)}
          onInspector={() => showInspector(true)}
        />
      </Panes>
      {searchOpen && <SearchModal D={D} ctrl={ctrl} onClose={() => setSearchOpen(false)} />}
      {helpOpen && <HelpModal onClose={() => setHelpOpen(false)} />}
    </>
  )
}

function Status({ D, snap }: { D: Model; snap: ReturnType<Explorer['getSnapshot']> }) {
  const v = snap.view
  const s = D.raw.stats
  const comp = D.raw.completeness
  return (
    <>
      {v && (
        <span className="truncate">
          {(v.kind === 'focus' ? 'Focus' : 'Architecture') +
            ' · ' +
            plural(v.counts.boxes, 'element') +
            ' · ' +
            plural(v.counts.edges, 'line') +
            (v.kind === 'arch' && v.counts.context ? ' · ' + v.counts.context + ' outside' : '') +
            ' · layout ' +
            Math.round(snap.layoutMs) +
            ' ms'}
        </span>
      )}
      {comp && comp.status !== 'complete' && (
        <span className="text-warning" title={(comp.reasons ?? []).slice(0, 10).join('\n')}>
          analysis {comp.status}
        </span>
      )}
      <span
        className="truncate max-[1100px]:hidden"
        title="Relations come from static analysis of the code (go/packages, go/types, SSA), not from observing it run."
      >
        static analysis · {fmt(s.nodes)} nodes · {fmt(s.edges)} relations
        {s.routes ? ' · ' + fmt(s.routes) + ' routes' : ''}
      </span>
    </>
  )
}
