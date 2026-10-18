// The call graph canvas: an SVG of the root's neighbourhood with zoom and
// pan, expand/collapse toggles, the path from the root to the selection
// highlighted and keyboard navigation (Tab to a function, Enter to select,
// arrows to follow calls, r / Shift+Enter to set as root, + - 0 to zoom).

import {
  memo,
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  type KeyboardEvent,
  type PointerEvent,
  type Ref,
} from 'react'
import { cx } from '../../lib/format'
import {
  edgePath,
  edgeTitle,
  fitText,
  FONT1,
  FONT2,
  KIND_LABEL,
  sub,
  type Built,
  type FlowIndex,
  type Layout,
} from './graph'
import type { FlowEdge, View } from './types'

export interface CanvasApi {
  fit: () => void
  zoom: (f: number) => void
  center: (id: string) => void
  focusNode: (id: string) => void
}

interface Props {
  ix: FlowIndex
  g: Built
  L: Layout
  view: View
  selected: string
  path: Set<FlowEdge>
  find: string
  /** Changes when the graph should be fitted again (new root, depth…). */
  fitKey: string
  onSelect: (id: string) => void
  onSetRoot: (id: string) => void
  onToggle: (key: string) => void
  /** Whether fit had to keep a readable scale (the graph is larger than the view). */
  onLarge: (large: boolean) => void
  api: Ref<CanvasApi>
}

const MIN_K = 0.15
const MAX_K = 3
const FIT_MIN = 0.7

const cssEsc = (v: string) => CSS.escape(v)

export function FlowCanvas({
  ix,
  g,
  L,
  view,
  selected,
  path,
  find,
  fitKey,
  onSelect,
  onSetRoot,
  onToggle,
  onLarge,
  api,
}: Props) {
  const wrapRef = useRef<HTMLDivElement>(null)
  const svgRef = useRef<SVGSVGElement>(null)
  const vpRef = useRef<SVGGElement>(null)
  const T = useRef({ k: 1, tx: 0, ty: 0 })

  const apply = useCallback(() => {
    const t = T.current
    vpRef.current?.setAttribute('transform', `translate(${t.tx},${t.ty}) scale(${t.k})`)
  }, [])

  const size = () => ({ W: wrapRef.current?.clientWidth || 800, H: wrapRef.current?.clientHeight || 500 })

  const zoomAt = useCallback(
    (f: number, x: number, y: number) => {
      const t = T.current
      const k = Math.max(MIN_K, Math.min(MAX_K, t.k * f))
      t.tx = x - (x - t.tx) * (k / t.k)
      t.ty = y - (y - t.ty) * (k / t.k)
      t.k = k
      apply()
    },
    [apply],
  )

  // fit shows the whole graph when it stays readable; otherwise it keeps a
  // readable scale and centres the root, leaving the rest to pan and zoom.
  const fit = useCallback(() => {
    const { minX, minY, maxX, maxY } = L.bounds
    if (!isFinite(minX)) return
    const { W, H } = size()
    const pad = 40
    const k = Math.min(1.2, Math.min((W - 2 * pad) / (maxX - minX || 1), (H - 2 * pad) / (maxY - minY || 1)))
    const t = T.current
    if (k >= FIT_MIN) {
      t.k = k
      t.tx = W / 2 - ((minX + maxX) / 2) * k
      t.ty = H / 2 - ((minY + maxY) / 2) * k
      apply()
      onLarge(false)
      return
    }
    t.k = FIT_MIN
    const r = L.box.get(view.root) ?? { x: 0, y: 0, w: 0, h: 0 }
    t.tx = W / 2 - (r.x + r.w / 2) * t.k
    t.ty = H / 2 - (r.y + r.h / 2) * t.k
    apply()
    onLarge(true)
  }, [L, view.root, apply, onLarge])

  const center = useCallback(
    (id: string) => {
      const b = L.box.get(id)
      if (!b) return
      const { W, H } = size()
      const t = T.current
      const cx = t.tx + (b.x + b.w / 2) * t.k
      const cy = t.ty + (b.y + b.h / 2) * t.k
      if (cx < 40 || cx > W - 40 || cy < 40 || cy > H - 40) {
        t.tx = W / 2 - (b.x + b.w / 2) * t.k
        t.ty = H / 2 - (b.y + b.h / 2) * t.k
        apply()
      }
    },
    [L, apply],
  )

  const focusNode = useCallback((id: string) => {
    svgRef.current?.querySelector<SVGGElement>(`.gf-node[data-id="${cssEsc(id)}"]`)?.focus({ preventScroll: true })
  }, [])

  useImperativeHandle(
    api,
    () => ({
      fit,
      zoom: (f: number) => {
        const { W, H } = size()
        zoomAt(f, W / 2, H / 2)
      },
      center,
      focusNode,
    }),
    [fit, zoomAt, center, focusNode],
  )

  // Fit when the view changes (not when a branch is folded), and keep the
  // transform across renders otherwise.
  useLayoutEffect(() => {
    fit()
    // fit depends on the layout; only fitKey asks for a new fit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fitKey])

  // A canvas that was hidden (0×0) when the graph came: fit once it has a size.
  useEffect(() => {
    const el = wrapRef.current
    if (!el || typeof ResizeObserver === 'undefined') return
    let had = el.clientWidth > 0
    const ro = new ResizeObserver(() => {
      if (!had && el.clientWidth > 0) {
        had = true
        fit()
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [fit])

  // Wheel zoom (a native listener: React's is passive and cannot prevent scrolling).
  useEffect(() => {
    const svg = svgRef.current
    if (!svg) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const r = svg.getBoundingClientRect()
      zoomAt(Math.exp(-e.deltaY * 0.0015), e.clientX - r.left, e.clientY - r.top)
    }
    svg.addEventListener('wheel', onWheel, { passive: false })
    return () => svg.removeEventListener('wheel', onWheel)
  }, [zoomAt])

  const drag = useRef<{ x: number; y: number; tx: number; ty: number } | null>(null)
  const onPointerDown = (e: PointerEvent<SVGSVGElement>) => {
    if ((e.target as Element).closest('.gf-node') || e.button !== 0) return
    drag.current = { x: e.clientX, y: e.clientY, tx: T.current.tx, ty: T.current.ty }
    e.currentTarget.setPointerCapture(e.pointerId)
    e.currentTarget.classList.add('gf-panning')
  }
  const onPointerMove = (e: PointerEvent<SVGSVGElement>) => {
    const d = drag.current
    if (!d) return
    T.current.tx = d.tx + e.clientX - d.x
    T.current.ty = d.ty + e.clientY - d.y
    apply()
  }
  const onPointerUp = (e: PointerEvent<SVGSVGElement>) => {
    drag.current = null
    e.currentTarget.classList.remove('gf-panning')
  }

  const onKeyDown = (e: KeyboardEvent<SVGGElement>) => {
    const el = (e.target as Element).closest<SVGGElement>('.gf-node')
    const id = el?.dataset.id
    if (!id) return
    let next: string | undefined
    if ((e.key === 'Enter' && e.shiftKey) || e.key === 'r') {
      e.preventDefault()
      onSetRoot(id)
      return
    }
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onSelect(id)
      return
    }
    if (e.key === 'ArrowRight') next = g.edges.find((x) => x.from === id && g.vis.has(x.to))?.to
    else if (e.key === 'ArrowLeft') next = g.edges.find((x) => x.to === id && g.vis.has(x.from))?.from
    else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      const col = [...(L.cols.get(g.vis.get(id)!) ?? [])].sort((a, b) => L.box.get(a)!.y - L.box.get(b)!.y)
      next = col[col.indexOf(id) + (e.key === 'ArrowDown' ? 1 : -1)]
    } else if (e.key === '+' || e.key === '=') {
      const { W, H } = size()
      zoomAt(1.25, W / 2, H / 2)
      return
    } else if (e.key === '-') {
      const { W, H } = size()
      zoomAt(0.8, W / 2, H / 2)
      return
    } else if (e.key === '0') {
      fit()
      return
    }
    if (next) {
      e.preventDefault()
      onSelect(next)
      center(next)
      focusNode(next)
    }
  }

  return (
    <div ref={wrapRef} className="absolute inset-0 overflow-hidden bg-canvas">
      <svg
        ref={svgRef}
        className="gf-svg font-mono"
        role="application"
        aria-label="Call graph. Tab to a function, Enter to select, arrow keys to follow calls."
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
      >
        <defs>
          {(
            [
              ['gf-a-def', 'gf-m-def'],
              ['gf-a-poss', 'gf-m-poss'],
              ['gf-a-unres', 'gf-m-unres'],
              ['gf-a-path', 'gf-m-path'],
            ] as const
          ).map(([id, cls]) => (
            <marker
              key={id}
              id={id}
              viewBox="0 0 10 10"
              refX="9"
              refY="5"
              markerWidth="7"
              markerHeight="7"
              orient="auto-start-reverse"
            >
              <path d="M0,1 L9,5 L0,9 z" className={cls} />
            </marker>
          ))}
        </defs>
        <g ref={vpRef} onKeyDown={onKeyDown}>
          <Edges ix={ix} g={g} L={L} path={path} dim={!!find} />
          <Nodes
            ix={ix}
            g={g}
            L={L}
            view={view}
            selected={selected}
            find={find}
            onSelect={onSelect}
            onSetRoot={onSetRoot}
            onToggle={onToggle}
          />
        </g>
      </svg>
    </div>
  )
}

const markerOf = (e: FlowEdge, on: boolean) =>
  'url(#' +
  (on ? 'gf-a-path' : e.conf === 'unresolved' ? 'gf-a-unres' : e.conf === 'possible' ? 'gf-a-poss' : 'gf-a-def') +
  ')'

const Edges = memo(function Edges({
  ix,
  g,
  L,
  path,
  dim,
}: {
  ix: FlowIndex
  g: Built
  L: Layout
  path: Set<FlowEdge>
  dim: boolean
}) {
  // Edges on the path are drawn last, above the others.
  const sorted = useMemo(() => [...g.edges].sort((a, b) => Number(path.has(a)) - Number(path.has(b))), [g, path])
  return (
    <g>
      {sorted.map((e, i) => {
        const a = L.box.get(e.from)
        const b = L.box.get(e.to)
        if (!a || !b) return null
        const on = path.has(e)
        const p = edgePath(a, b)
        const label =
          e.kind === 'go' || e.kind === 'defer' ? e.kind : e.kind === 'handler' && e.order ? String(e.order) : ''
        return (
          <g key={e.from + '>' + e.to + '>' + (e.line || 0) + '>' + (e.order || 0) + '>' + i}>
            <path
              d={p.d}
              className={cx('gf-edge', 'gf-c-' + e.conf, on && 'gf-on', dim && !on && 'gf-dim')}
              markerEnd={markerOf(e, on)}
            >
              <title>{edgeTitle(ix, e)}</title>
            </path>
            {label && (
              <text className="gf-elabel" x={p.mx} y={p.my - 4} textAnchor="middle">
                {label}
              </text>
            )}
          </g>
        )
      })}
    </g>
  )
})

const Nodes = memo(function Nodes({
  ix,
  g,
  L,
  view,
  selected,
  find,
  onSelect,
  onSetRoot,
  onToggle,
}: {
  ix: FlowIndex
  g: Built
  L: Layout
  view: View
  selected: string
  find: string
  onSelect: (id: string) => void
  onSetRoot: (id: string) => void
  onToggle: (key: string) => void
}) {
  // Which nodes already show a branch on their right (callees) or left (callers).
  const shown = useMemo(() => {
    const right = new Set<string>()
    const left = new Set<string>()
    for (const e of g.edges) {
      const a = g.vis.get(e.from)
      const b = g.vis.get(e.to)
      if (a === undefined || b === undefined) continue
      if (b > a) right.add(e.from)
      if (a < b) left.add(e.to)
    }
    return { right, left }
  }, [g])
  const f1 = FONT1()
  const f2 = FONT2()
  return (
    <g>
      {g.order.map((id) => {
        const n = ix.nodes.get(id)
        const b = L.box.get(id)
        if (!n || !b) return null
        const isRoot = id === view.root
        const hit = !!find && (n.name + ' ' + (n.file || '')).toLowerCase().includes(find)
        const toggles = ([1, -1] as const).map((sign) => {
          const col = b.col
          if (!isRoot && ((sign > 0 && col < 0) || (sign < 0 && col > 0))) return null
          if ((sign > 0 && view.dir === 'callers') || (sign < 0 && view.dir === 'callees')) return null
          const isShown = sign > 0 ? shown.right.has(id) : shown.left.has(id)
          const key = id + '|' + sign
          const collapsed = view.collapsed.has(key)
          const more = g.more.get(key)
          if (!isShown && !collapsed && !more) return null
          const title = collapsed
            ? 'Expand'
            : isShown
              ? 'Collapse'
              : `${more} more call(s) beyond the depth limit — set as root to explore`
          return (
            <g
              key={sign}
              className="gf-tog"
              transform={`translate(${sign > 0 ? b.w : 0},${b.h / 2})`}
              onClick={(ev) => {
                ev.stopPropagation()
                if (!isShown && !collapsed) onSetRoot(id)
                else onToggle(key)
              }}
              onDoubleClick={(ev) => ev.stopPropagation()}
            >
              <circle r={7} />
              <text y={3.5}>{collapsed ? '+' : isShown ? '−' : '…'}</text>
              <title>{title}</title>
            </g>
          )
        })
        return (
          <g
            key={id}
            className={cx(
              'gf-node',
              'gf-k-' + n.kind,
              isRoot && 'gf-root',
              id === selected && 'gf-sel',
              !!find && !hit && 'gf-dim',
              hit && 'gf-hit',
            )}
            transform={`translate(${b.x},${b.y})`}
            tabIndex={0}
            role="button"
            data-id={id}
            aria-label={KIND_LABEL[n.kind] + ' ' + n.name + (n.file ? ', ' + n.file + ' line ' + n.line : '')}
            aria-pressed={id === selected}
            onClick={() => onSelect(id)}
            onDoubleClick={() => onSetRoot(id)}
          >
            <rect className="gf-box" width={b.w} height={b.h} rx={6} />
            <rect className="gf-bar" width={3} height={b.h - 12} x={6} y={6} rx={1.5} />
            <text className="gf-t1" x={16} y={17}>
              {fitText(n.name + (n.recursive ? ' ↻' : ''), b.w - 30, f1)}
            </text>
            <text className="gf-t2" x={16} y={32}>
              {fitText(sub(n), b.w - 30, f2)}
            </text>
            {n.changed && <circle className="gf-chg" cx={b.w - 9} cy={9} r={3.5} />}
            <title>{n.name + (n.changed ? ' (modified)' : '') + (n.recursive ? ' (recursive)' : '')}</title>
            {toggles}
          </g>
        )
      })}
    </g>
  )
})
