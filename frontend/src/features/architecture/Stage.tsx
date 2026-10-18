// The canvas and what floats over it: the toolbar, the view's chips (scope,
// focus depths, dependency path), the legend, the minimap and the state of
// the layout (laying out, nothing to draw, failure).

import { useCallback, useEffect, useRef, type ReactNode } from 'react'
import { Icon } from '../../components/Icon'
import { debounce } from '../../lib/browser'
import { cx, plural } from '../../lib/format'
import { useTheme } from '../../lib/theme'
import { filtersChanged, type Explorer, type Snapshot } from './controller'
import { FiltersPanel } from './FiltersPanel'
import { PER_COL } from './focus'
import type { Model } from './model'
import { Renderer } from './renderer'
import { Glyph, LineSample, Seg, type GlyphName } from './ui'
import { CONFS, TYPE } from './vocab'

export interface StageProps {
  D: Model
  ctrl: Explorer
  snap: Snapshot
  minimap: boolean
  onMinimap: () => void
  onFullscreen: () => void
  filtersOpen: boolean
  setFiltersOpen: (v: boolean) => void
  sidebar: boolean
  inspector: boolean
  onSidebar: () => void
  onInspector: () => void
}

export function Stage(props: StageProps) {
  const { D, ctrl, snap, minimap, filtersOpen, setFiltersOpen } = props
  const canvas = useRef<HTMLCanvasElement>(null)
  const mini = useRef<HTMLCanvasElement>(null)
  const renderer = useRef<Renderer | null>(null)
  const filtersBtn = useRef<HTMLButtonElement>(null)
  const [theme] = useTheme()

  useEffect(() => {
    if (!canvas.current || !mini.current) return
    const r = new Renderer(canvas.current, mini.current, ctrl.events)
    renderer.current = r
    ctrl.attach(r)
    return () => {
      ctrl.detach(r)
      r.destroy()
      renderer.current = null
    }
  }, [ctrl])

  useEffect(() => {
    renderer.current?.redraw()
  }, [theme])

  useEffect(() => {
    renderer.current?.setMinimap(minimap)
  }, [minimap])

  // The layout follows the window's proportions: lay out again (and fit)
  // when the window is resized.
  useEffect(() => {
    const relayout = debounce(() => void ctrl.rebuild({ fit: true }), 300)
    window.addEventListener('resize', relayout)
    return () => window.removeEventListener('resize', relayout)
  }, [ctrl])

  const closeFilters = useCallback(() => setFiltersOpen(false), [setFiltersOpen])
  const hasBoxes = !!snap.view?.scene.boxes.length
  const changed = filtersChanged(snap.S.filters)

  return (
    <div className="relative min-h-0 flex-1 overflow-hidden bg-canvas">
      <canvas
        ref={canvas}
        tabIndex={0}
        className="absolute inset-0 block size-full touch-none outline-none"
        aria-label="Architecture diagram. Use the explorer, the search or the inspector to navigate it with the keyboard."
      />

      <div className="pointer-events-none absolute top-2.5 right-14 left-3 z-[5] flex flex-col gap-1.5">
        <div className="flex flex-wrap items-center gap-1.5 *:pointer-events-auto">
          {!props.sidebar && (
            <button
              type="button"
              className="btn btn-xs btn-square border-line bg-base-100 shadow-sm"
              title="Show the explorer (b)"
              aria-label="Show the explorer"
              onClick={props.onSidebar}
            >
              <Glyph name="sidebar" className="size-3.5" />
            </button>
          )}
          <ViewBar {...props} />
        </div>
      </div>

      <div
        role="toolbar"
        aria-label="Canvas"
        className="absolute top-2.5 right-3 z-[6] flex flex-col gap-0.5 rounded-box border border-line bg-base-100 p-[3px] shadow-sm"
      >
        {!props.inspector && (
          <>
            <ToolButton glyph="inspector" title="Show the inspector (i)" onClick={props.onInspector} />
            <Sep />
          </>
        )}
        <ToolButton icon="plus" title="Zoom in (+)" onClick={() => ctrl.zoomBy(1.25)} />
        <ToolButton icon="minus" title="Zoom out (−)" onClick={() => ctrl.zoomBy(0.8)} />
        <ToolButton glyph="fit" title="Fit view (f)" onClick={() => ctrl.fitView()} />
        <Sep />
        <ToolButton
          glyph="focus"
          title="Focus mode: only the selection and its dependencies (.)"
          pressed={!!snap.S.focus}
          onClick={() => ctrl.toggleFocus()}
        />
        <ToolButton glyph="minimap" title="Minimap (m)" pressed={minimap} onClick={props.onMinimap} />
        <ToolButton glyph="fullscreen" title="Fullscreen (Shift+F)" onClick={props.onFullscreen} />
        <Sep />
        <ToolButton
          ref={filtersBtn}
          icon="filter"
          title="Filters (element and relation types, confidence)"
          pressed={filtersOpen}
          onClick={() => setFiltersOpen(!filtersOpen)}
          dot={changed}
        />
      </div>

      {filtersOpen && (
        <FiltersPanel
          D={D}
          ctrl={ctrl}
          filters={snap.S.filters}
          pathDepth={snap.S.pathDepth}
          anchor={filtersBtn}
          onClose={closeFilters}
        />
      )}

      <div
        className={cx(
          'absolute right-3 bottom-3 z-[5] h-[140px] w-[200px] overflow-hidden rounded-field border border-line bg-base-100 shadow-sm max-[820px]:hidden',
          (!minimap || !hasBoxes) && 'hidden',
        )}
      >
        <canvas ref={mini} className="block size-full cursor-pointer" aria-label="Minimap" />
      </div>

      <Legend snap={snap} />
      <StageOverlay snap={snap} />
    </div>
  )
}

function Sep() {
  return <span className="mx-1 my-0.5 h-px bg-line" />
}

function ToolButton({
  icon,
  glyph,
  title,
  onClick,
  pressed,
  dot,
  ref,
}: {
  icon?: 'plus' | 'minus' | 'filter'
  glyph?: GlyphName
  title: string
  onClick: () => void
  pressed?: boolean
  dot?: boolean
  ref?: React.Ref<HTMLButtonElement>
}) {
  return (
    <button
      ref={ref}
      type="button"
      title={title}
      aria-label={title.replace(/ \(.*\)$/, '')}
      aria-pressed={pressed}
      className={cx(
        'btn btn-ghost btn-sm btn-square relative size-7 min-h-0',
        pressed && 'bg-accent-weak text-accent-text',
      )}
      onClick={onClick}
    >
      {icon ? <Icon name={icon} className="size-4" /> : glyph ? <Glyph name={glyph} className="size-4" /> : null}
      {dot && <span className="absolute top-1 right-1 size-1.5 rounded-full bg-primary" />}
    </button>
  )
}

function Chip({
  children,
  accent,
  warn,
  title,
}: {
  children: ReactNode
  accent?: boolean
  warn?: boolean
  title?: string
}) {
  return (
    <span
      title={title}
      className={cx(
        'inline-flex h-[26px] items-center gap-1.5 rounded-full border bg-base-100 px-2.5 text-xs whitespace-nowrap text-fg-2 shadow-sm',
        accent ? 'border-primary/45' : warn ? 'border-warning/45 text-warning' : 'border-line',
      )}
    >
      {children}
    </span>
  )
}

function Stepper({
  label,
  value,
  lo,
  hi,
  set,
  title,
}: {
  label: string
  value: number
  lo: number
  hi: number
  set: (v: number) => void
  title: string
}) {
  return (
    <Chip title={title}>
      {label}
      <span className="-mr-1.5 inline-flex items-center">
        <button
          type="button"
          className="btn btn-ghost btn-xs btn-square size-5 min-h-0"
          aria-label={'Less ' + label}
          disabled={value <= lo}
          onClick={() => set(value - 1)}
        >
          −
        </button>
        <span className="min-w-3.5 text-center text-fg tabular-nums">{value}</span>
        <button
          type="button"
          className="btn btn-ghost btn-xs btn-square size-5 min-h-0"
          aria-label={'More ' + label}
          disabled={value >= hi}
          onClick={() => set(value + 1)}
        >
          +
        </button>
      </span>
    </Chip>
  )
}

function ViewBar({ D, ctrl, snap }: StageProps) {
  const { S, view, hl } = snap
  const f = S.focus
  if (f) {
    const sym = view?.kind === 'focus' && view.symbolMode
    const more = view?.kind === 'focus' && Object.keys(view.more).length > 0
    const t = D.T[f.i]
    return (
      <>
        <Chip accent>
          <Glyph name="focus" className="size-3.5 text-accent-text" />
          <strong className="max-w-[340px] truncate font-medium text-fg">{D.displayName(t)}</strong>
          <button
            type="button"
            className="btn btn-ghost btn-xs btn-square -mr-1.5 size-5 min-h-0"
            title="Leave focus mode (Esc)"
            aria-label="Leave focus mode"
            onClick={() => ctrl.exitFocus()}
          >
            <Icon name="x" className="size-3" />
          </button>
        </Chip>
        <Stepper
          label={sym ? '← callers' : '← dependents'}
          value={f.up}
          lo={0}
          hi={6}
          set={(v) => ctrl.setFocus({ up: v })}
          title="Distance followed backwards"
        />
        <Stepper
          label={sym ? 'callees →' : 'dependencies →'}
          value={f.down}
          lo={0}
          hi={6}
          set={(v) => ctrl.setFocus({ down: v })}
          title="Distance followed forwards"
        />
        {sym && !TYPE[t.kind] && (
          <Chip>
            <label className="flex cursor-pointer items-center gap-1.5">
              <input
                type="checkbox"
                className="checkbox checkbox-xs checkbox-primary"
                checked={f.refs}
                onChange={(e) => ctrl.setFocus({ refs: e.target.checked })}
              />
              type uses
            </label>
          </Chip>
        )}
        {more && <Chip warn>Columns keep the {PER_COL} strongest relations</Chip>}
      </>
    )
  }
  return (
    <>
      {S.root !== D.ROOT && (
        <Chip>
          Scope
          <strong className="max-w-[340px] truncate font-medium text-fg">{D.displayName(D.T[S.root], true)}</strong>
          <button
            type="button"
            className="btn btn-ghost btn-xs btn-square -mr-1.5 size-5 min-h-0"
            title="Back to the whole repository"
            aria-label="Leave the scope"
            onClick={() => ctrl.enterAncestor(D.ROOT, S.root)}
          >
            <Icon name="x" className="size-3" />
          </button>
        </Chip>
      )}
      {hl && S.sel?.kind === 'item' && (
        <Chip title={'Following relations ' + (S.pathDepth ? 'one step' : 'transitively') + ' from the selection'}>
          <span style={{ color: 'var(--hl-out)' }}>↓ {plural(hl.down, 'dependency', 'dependencies')}</span>
          <span style={{ color: 'var(--hl-in)' }}>↑ {plural(hl.up, 'dependent')}</span>
          <Seg
            className="-mr-1.5 [&_.btn]:h-5 [&_.btn]:min-h-0 [&_.btn]:px-2"
            options={[
              [0, 'Full path'],
              [1, 'Direct'],
            ]}
            value={S.pathDepth ? 1 : 0}
            onChange={(v) => ctrl.setPathDepth(v)}
          />
        </Chip>
      )}
      {!S.filters.ext && D.EXT >= 0 && (
        <Chip title="External dependencies are hidden; show them in Filters">
          <span className="text-fg-3">External hidden</span>
          <button
            type="button"
            className="btn btn-ghost btn-xs -mr-1.5 h-5 min-h-0"
            onClick={() => ctrl.setFilters({ ...S.filters, ext: true })}
          >
            Show
          </button>
        </Chip>
      )}
    </>
  )
}

function Legend({ snap }: { snap: Snapshot }) {
  const { view, hl } = snap
  if (!view || !view.scene.edges.length) return null
  const confs: Record<string, 1> = {}
  view.scene.edges.forEach((e) => (confs[e.conf] = 1))
  return (
    <div
      aria-label="Legend"
      className="absolute bottom-3 left-3 z-[5] flex max-w-[calc(100%-240px)] flex-wrap items-center gap-x-3 gap-y-1 rounded-field border border-line bg-base-100/90 px-2.5 py-1 text-[11px] text-fg-3 max-[820px]:max-w-[calc(100%-24px)]"
    >
      {CONFS.filter((c) => confs[c]).map((c) => (
        <span key={c} className="inline-flex items-center gap-1.5 whitespace-nowrap" title={'Line style: ' + c}>
          <LineSample conf={c} />
          {c}
        </span>
      ))}
      {hl && (hl.down > 0 || hl.up > 0) && (
        <>
          <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
            <LineSample color="hl-out" />
            depends on
          </span>
          <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
            <LineSample color="hl-in" />
            used by
          </span>
        </>
      )}
    </div>
  )
}

function StageOverlay({ snap }: { snap: Snapshot }) {
  const st = snap.stage
  if (!st) return null
  return (
    <div
      className="pointer-events-none absolute inset-0 z-[4] grid place-items-center p-6"
      role="status"
      aria-live="polite"
    >
      <div className="pointer-events-auto flex max-w-md items-center gap-2.5 rounded-box border border-line bg-base-100 px-4 py-3 text-sm text-fg-2 shadow-[var(--shadow)]">
        {st.kind === 'loading' && (
          <>
            <span className="loading loading-spinner loading-sm text-primary" />
            Laying out…
          </>
        )}
        {st.kind === 'empty' && (
          <div className="flex flex-col items-center gap-1 text-center">
            <strong className="font-medium text-fg">Nothing to draw</strong>
            <span>The filters hide every element of this scope.</span>
          </div>
        )}
        {st.kind === 'error' && (
          <div className="flex flex-col items-center gap-1 text-center">
            <strong className="font-medium text-error">This view could not be drawn</strong>
            <span className="break-words">{st.message}</span>
          </div>
        )}
      </div>
    </div>
  )
}
