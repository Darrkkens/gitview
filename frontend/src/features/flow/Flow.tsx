// The Code Flow view: who can call a function and what it can call, from
// the call index the Go side computed (static analysis: possible execution
// paths, not observed calls). A port of the legacy report's navigator:
// symbol search, start points, direction, depth, external calls, search in
// graph, zoom and pan, folding, the path to the selection, a detail panel
// and an accessible list view.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Icon } from '../../components/Icon'
import { PaneHeader, Panes } from '../../components/Panes'
import { StatusSlot } from '../../components/Slots'
import { toast } from '../../components/Toast'
import { isTyping } from '../../lib/browser'
import { offered } from '../../lib/data'
import { cx } from '../../lib/format'
import { routeHref, setParams } from '../../lib/router'
import { FlowCanvas, type CanvasApi } from './FlowCanvas'
import { build, indexGraph, layout, LIMIT, pathTo } from './graph'
import { ListView } from './ListView'
import { Panel, type Links } from './Panel'
import { Search } from './Search'
import type { Direction, FlowNode, FlowReport, FlowSection, View } from './types'

const nf = new Intl.NumberFormat('en-US')
const num = (n: number) => nf.format(n || 0)

const DIRS: { id: Direction; label: string }[] = [
  { id: 'callers', label: 'Callers' },
  { id: 'both', label: 'Both' },
  { id: 'callees', label: 'Callees' },
]

const ABOUT =
  'Static analysis of the Go code when the report was made (go/packages, go/types, SSA). Edges are possible execution paths, not observed calls.'

// The navigator's settings survive a trip to another view (which unmounts
// this one): per call index, in memory only.
interface Memory {
  flow: FlowSection
  root: string
  dir: Direction
  depth: number
  external: boolean
  mode: 'graph' | 'list'
  panelPref: boolean | null
}
let memory: Memory | null = null

function useWide(px: number): boolean {
  const [wide, setWide] = useState(() => window.innerWidth >= px)
  useEffect(() => {
    const on = () => setWide(window.innerWidth >= px)
    window.addEventListener('resize', on)
    return () => window.removeEventListener('resize', on)
  }, [px])
  return wide
}

export function Flow({ report, flow, params }: { report: FlowReport; flow: FlowSection; params: URLSearchParams }) {
  const ix = useMemo(() => indexGraph(flow), [flow])
  const roots = useMemo(() => (flow.roots ?? []).filter((r) => ix.nodes.has(r.id)), [flow, ix])
  const routes = useMemo(
    () =>
      [...ix.nodes.values()]
        .filter((n) => n.kind === 'route')
        .sort((a, b) => ((a.path || '') < (b.path || '') ? -1 : (a.path || '') > (b.path || '') ? 1 : 0)),
    [ix],
  )

  const [mem] = useState(() => (memory?.flow === flow ? memory : null))

  // The root comes from the URL (#/flow?node=<id>), else the last one seen
  // here, else the first modified function.
  const asked = params.get('node') ?? ''
  const root =
    asked && ix.nodes.has(asked) ? asked : mem?.root && ix.nodes.has(mem.root) ? mem.root : (roots[0]?.id ?? '')
  useEffect(() => {
    if (asked && !ix.nodes.has(asked)) toast('This function is not in the report’s call index.', { tone: 'warning' })
  }, [asked, ix])

  const [dir, setDir] = useState<Direction>(mem?.dir ?? 'both')
  const [depth, setDepth] = useState(mem?.depth ?? 3)
  const [external, setExternal] = useState(mem?.external ?? true)
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set())
  const [selected, setSelected] = useState(root)
  const [find, setFind] = useState('')
  const [mode, setMode] = useState<'graph' | 'list'>(mem?.mode ?? 'graph')
  const [large, setLarge] = useState(false)
  const [note, setNote] = useState<{ text: string; warn: boolean } | null>(null)
  const wide = useWide(1000)
  const [panelPref, setPanelPref] = useState<boolean | null>(mem?.panelPref ?? null)
  const showPanel = panelPref ?? wide
  useEffect(() => {
    memory = { flow, root, dir, depth, external, mode, panelPref }
  })

  // A new root starts unfolded with itself selected.
  const [prevRoot, setPrevRoot] = useState(root)
  if (prevRoot !== root) {
    setPrevRoot(root)
    setSelected(root)
    setCollapsed(new Set())
    setNote(null)
  }

  const view: View = useMemo(() => ({ root, dir, depth, external, collapsed }), [root, dir, depth, external, collapsed])
  const g = useMemo(() => (root ? build(ix, view) : null), [ix, view, root])
  const L = useMemo(() => (g ? layout(ix, g, root) : null), [ix, g, root])
  const sel = g && g.vis.has(selected) ? selected : root
  const path = useMemo(() => (g ? pathTo(g, root, sel) : new Set<never>()), [g, root, sel])
  const q = find.trim().toLowerCase()
  const canvas = useRef<CanvasApi>(null)
  const searchRef = useRef<HTMLInputElement>(null)

  const setRoot = useCallback(
    (id: string) => {
      if (!ix.nodes.has(id)) return
      if (id === root) {
        setSelected(id)
        return
      }
      // A new history entry: Back returns to the previous root.
      setParams({ node: id }, { replace: false })
    },
    [ix, root],
  )
  const go = useCallback(
    (id: string) => {
      if (g?.vis.has(id)) {
        setSelected(id)
        canvas.current?.center(id)
        canvas.current?.focusNode(id)
      } else setRoot(id)
    },
    [g, setRoot],
  )
  const toggleBranch = useCallback(
    (key: string) =>
      setCollapsed((c) => {
        const n = new Set(c)
        if (n.has(key)) n.delete(key)
        else n.add(key)
        return n
      }),
    [],
  )
  const say = useCallback((text: string, warn?: boolean) => setNote({ text, warn: !!warn }), [])
  const pick = useCallback(
    (n: FlowNode) => {
      setNote(null)
      setRoot(n.id)
    },
    [setRoot],
  )

  // Links to the other views.
  const links: Links = useMemo(() => {
    const anchors = flow.anchors ?? {}
    const lines = new Map<string, Set<number>>()
    const newLines = (path: string) => {
      let s = lines.get(path)
      if (!s) {
        s = new Set()
        const f = report.files.find((x) => x.path === path)
        for (const h of f?.hunks ?? []) for (const l of h.lines) if (l.new) s.add(l.new)
        lines.set(path, s)
      }
      return s
    }
    const inspectable = new Set((report.inspect?.symbols ?? []).map((s) => s.symbol?.id).filter(Boolean) as string[])
    const graph = offered('graph')
    return {
      diff: (n) => {
        if (!n.file || !anchors[n.file] || !n.line) return null
        const s = newLines(n.file)
        for (let l = n.line; l <= (n.end || n.line); l++)
          if (s.has(l)) return routeHref('changes', { file: n.file, line: l })
        return n.changed ? routeHref('changes', { file: n.file }) : null
      },
      inspect: (n) => (inspectable.has(n.id) ? routeHref('inspect', { fn: n.id }) : null),
      // The Repository Graph names functions "func:<Code Flow id>"; routes,
      // database operations and externals share the Code Flow id.
      architecture: (n) =>
        !graph || n.kind === 'unresolved'
          ? null
          : routeHref('architecture', { node: n.kind === 'func' || n.kind === 'closure' ? 'func:' + n.id : n.id }),
    }
  }, [flow, report])

  // "/" finds a function, as in the other views.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || isTyping(e)) return
      if (e.key === '/') {
        e.preventDefault()
        searchRef.current?.focus()
        searchRef.current?.select()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  // The status line under the toolbar.
  let info = ''
  let warn = false
  if (g) {
    info = num(g.order.length) + (g.order.length === 1 ? ' function shown' : ' functions shown')
    if (g.trunc) info += ` — view limited to ${LIMIT} nodes; narrow the depth or set another root`
    if (flow.truncated) info += " — the report's index itself was limited, some relations are missing"
    if (large && mode === 'graph') info += ' — larger than the view: drag to pan, scroll to zoom'
    warn = g.trunc || !!flow.truncated
  }
  const errors = flow.errors ?? []
  const rootInList = roots.some((r) => r.id === root) || routes.some((r) => r.id === root)
  const changedRoots = roots.filter((r) => r.kind !== 'selected' && r.kind !== 'route')
  const selectedRoots = roots.filter((r) => r.kind === 'selected')

  const panel = (
    <>
      <PaneHeader title="Details">
        <button
          type="button"
          className="btn btn-square btn-ghost btn-xs"
          title="Hide the details"
          onClick={() => setPanelPref(false)}
        >
          <Icon name="x" className="size-3.5" />
        </button>
      </PaneHeader>
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto">
        {g && sel ? (
          <Panel ix={ix} id={sel} root={root} links={links} onGo={go} onSetRoot={setRoot} />
        ) : (
          <p className="p-4 text-[13px] text-fg-3">Select a function to see its details.</p>
        )}
      </div>
    </>
  )

  return (
    <div className="h-full">
      <Panes id="flow" right={panel} rightWidth={380} showRight={showPanel}>
        <div className="flex flex-wrap items-center gap-2 border-b border-line bg-base-100 px-3 py-2">
          <Search ix={ix} onPick={pick} onSay={say} inputRef={searchRef} />
          <select
            className="select w-48 min-w-40 select-sm max-lg:w-auto max-lg:flex-1"
            aria-label="Start from"
            value={rootInList ? root : ''}
            onChange={(e) => e.target.value && setRoot(e.target.value)}
          >
            <option value="">Start from…</option>
            {changedRoots.length > 0 && (
              <optgroup label="Modified functions">
                {changedRoots.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.label}
                  </option>
                ))}
              </optgroup>
            )}
            {selectedRoots.length > 0 && (
              <optgroup label="Selected functions">
                {selectedRoots.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.label}
                  </option>
                ))}
              </optgroup>
            )}
            {routes.length > 0 && (
              <optgroup label="HTTP routes">
                {routes.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.name + '  (' + (r.router || '') + ')'}
                  </option>
                ))}
              </optgroup>
            )}
          </select>
          <div className="join" role="group" aria-label="Direction">
            {DIRS.map((d) => (
              <button
                key={d.id}
                type="button"
                aria-pressed={dir === d.id}
                className={cx('btn join-item btn-sm', dir === d.id && 'btn-active btn-primary')}
                onClick={() => setDir(d.id)}
              >
                {d.label}
              </button>
            ))}
          </div>
          <label className="flex items-center gap-1.5 text-xs text-fg-2">
            Depth
            <select
              className="select w-14 select-sm"
              aria-label="Depth"
              value={depth}
              onChange={(e) => setDepth(Number(e.target.value))}
            >
              {[1, 2, 3, 4, 5].map((i) => (
                <option key={i} value={i}>
                  {i}
                </option>
              ))}
            </select>
          </label>
          <label
            className="flex cursor-pointer items-center gap-1.5 text-xs text-fg-2"
            title="Show calls leaving the module"
          >
            <input
              type="checkbox"
              className="toggle toggle-xs toggle-primary"
              checked={external}
              onChange={(e) => setExternal(e.target.checked)}
            />
            External
          </label>
          <label className="input input-sm w-40">
            <Icon name="filter" className="size-3.5 text-fg-3" />
            <input
              type="search"
              placeholder="Search in graph"
              aria-label="Search in graph"
              autoComplete="off"
              spellCheck={false}
              value={find}
              onChange={(e) => setFind(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && q && g) {
                  e.preventDefault()
                  const hit = g.order.find((id) => {
                    const n = ix.nodes.get(id)!
                    return (n.name + ' ' + (n.file || '')).toLowerCase().includes(q)
                  })
                  if (hit) {
                    setSelected(hit)
                    canvas.current?.center(hit)
                    canvas.current?.focusNode(hit)
                  }
                } else if (e.key === 'Escape') {
                  setFind('')
                }
              }}
            />
          </label>
          <div className="join" role="group" aria-label="View">
            <button
              type="button"
              aria-pressed={mode === 'graph'}
              className={cx('btn join-item btn-sm', mode === 'graph' && 'btn-active')}
              onClick={() => setMode('graph')}
              title="Graph view"
              aria-label="Graph view"
            >
              <Icon name="flow" className="size-3.5" />
            </button>
            <button
              type="button"
              aria-pressed={mode === 'list'}
              className={cx('btn join-item btn-sm', mode === 'list' && 'btn-active')}
              onClick={() => setMode('list')}
              title="List view (accessible)"
              aria-label="List view"
            >
              <Icon name="menu" className="size-3.5" />
            </button>
          </div>
          <span className="flex-1" />
          <span className="tooltip tooltip-left before:max-w-72 before:text-left" data-tip={ABOUT}>
            <Icon name="info" className="size-4 text-fg-3" />
          </span>
          {!showPanel && (
            <button
              type="button"
              className="btn btn-square btn-ghost btn-sm"
              title="Show the details"
              onClick={() => setPanelPref(true)}
            >
              <Icon name="panelRight" />
            </button>
          )}
        </div>
        <div
          className={cx(
            'flex min-h-6 items-center gap-2 border-b border-line bg-base-100 px-3 py-0.5 text-xs',
            !note && !info && !errors.length && !flow.notes?.length && 'hidden',
            note?.warn || (!note && warn) ? 'text-warning' : 'text-fg-2',
          )}
          role="status"
          aria-live="polite"
        >
          <span className="min-w-0 flex-1 truncate" title={note?.text ?? info}>
            {note?.text ?? info}
          </span>
          {errors.length > 0 && (
            <details className="dropdown dropdown-end">
              <summary className="btn btn-xs btn-warning btn-soft">
                <Icon name="warning" className="size-3.5" />
                {errors.length} compile error{errors.length === 1 ? '' : 's'}
              </summary>
              <div className="dropdown-content z-30 mt-1 w-[560px] max-w-[90vw] rounded-box border border-line-strong bg-base-100 p-3 shadow-[var(--shadow)]">
                <p className="mb-2 text-xs text-fg-2">
                  The Go code has {errors.length} compile error(s); results for the affected packages may be partial.
                </p>
                <ul className="scroll-thin max-h-72 space-y-1 overflow-auto font-mono text-[11.5px] break-all text-fg">
                  {errors.map((e, i) => (
                    <li key={i}>{e}</li>
                  ))}
                </ul>
              </div>
            </details>
          )}
          {!!flow.notes?.length && (
            <details className="dropdown dropdown-end">
              <summary className="btn btn-ghost btn-xs">Notes ({flow.notes.length})</summary>
              <ul className="dropdown-content z-30 mt-1 w-[480px] max-w-[90vw] space-y-1 rounded-box border border-line-strong bg-base-100 p-3 text-xs text-fg-2 shadow-[var(--shadow)]">
                {flow.notes.map((n, i) => (
                  <li key={i}>{n}</li>
                ))}
              </ul>
            </details>
          )}
        </div>
        <div className="relative min-h-0 flex-1">
          {!g || !L ? (
            <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 bg-canvas p-6 text-center">
              <Icon name="flow" className="size-8 text-fg-3" />
              <p className="max-w-md text-sm text-fg-2">
                {ix.searchable.length
                  ? 'Search for a function or pick a starting point to see who calls it and what it calls.'
                  : 'No Go functions were indexed for this report.'}
              </p>
              <p className="max-w-md text-xs text-fg-3">{ABOUT}</p>
            </div>
          ) : mode === 'list' ? (
            <ListView ix={ix} g={g} view={view} selected={sel} onSelect={go} />
          ) : (
            <>
              <FlowCanvas
                api={canvas}
                ix={ix}
                g={g}
                L={L}
                view={view}
                selected={sel}
                path={path}
                find={q}
                fitKey={[root, dir, depth, external].join('|')}
                onSelect={setSelected}
                onSetRoot={setRoot}
                onToggle={toggleBranch}
                onLarge={setLarge}
              />
              <div className="absolute top-2 right-2 z-10 join shadow-sm">
                <button
                  type="button"
                  className="btn join-item btn-xs"
                  onClick={() => canvas.current?.fit()}
                  title="Fit graph (0)"
                >
                  Fit
                </button>
                <button
                  type="button"
                  className="btn join-item btn-xs"
                  onClick={() => canvas.current?.zoom(1.25)}
                  aria-label="Zoom in"
                  title="Zoom in (+)"
                >
                  <Icon name="plus" className="size-3.5" />
                </button>
                <button
                  type="button"
                  className="btn join-item btn-xs"
                  onClick={() => canvas.current?.zoom(0.8)}
                  aria-label="Zoom out"
                  title="Zoom out (−)"
                >
                  <Icon name="minus" className="size-3.5" />
                </button>
              </div>
              <div
                className="pointer-events-none absolute bottom-2 left-2 z-10 flex max-w-[calc(100%-16px)] flex-wrap gap-x-3 gap-y-1 rounded-field border border-line bg-base-100/90 px-2 py-1 text-[11.5px] text-fg-2"
                aria-hidden="true"
              >
                <span>
                  <i className="gf-swatch" />
                  confirmed
                </span>
                <span>
                  <i className="gf-swatch gf-likely" />
                  likely (single implementation)
                </span>
                <span>
                  <i className="gf-swatch gf-possible" />
                  possible (interface / callback)
                </span>
                <span>
                  <i className="gf-swatch gf-unresolved" />
                  unresolved
                </span>
                <span className="flex items-center gap-1">
                  <span className="inline-block size-2 rounded-full bg-primary" />
                  modified
                </span>
              </div>
            </>
          )}
        </div>
      </Panes>
      <StatusSlot>
        <span>
          {num(ix.funcs)} functions · {num(ix.edges)} calls indexed
        </span>
        {(flow.modules?.length ?? 0) > 0 && <span className="truncate font-mono">{flow.modules!.join(', ')}</span>}
      </StatusSlot>
    </div>
  )
}
