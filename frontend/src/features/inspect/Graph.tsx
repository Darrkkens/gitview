// A small layered graph (the control-flow graph of a function, the data
// flow of a variable): laid out by ELK, drawn as SVG, with zoom and pan.
// Labels are repository text and are only rendered as SVG text.

import ELK from 'elkjs/lib/elk.bundled.js'
import type { ElkExtendedEdge, ElkNode } from 'elkjs/lib/elk-api'
import { useCallback, useEffect, useId, useImperativeHandle, useMemo, useRef, useState, type Ref } from 'react'
import { Icon } from '../../components/Icon'
import { cx } from '../../lib/format'
import { ToolBtn } from './ui'
import { monoFont, textWidth } from './util'

export interface GLine {
  t: string
  b?: boolean // bold
  sub?: boolean // secondary
}

export interface GNode {
  id: string
  lines: GLine[]
  fill?: string
  stroke?: string
  strokeWidth?: number
  dash?: string
  opacity?: number
  round?: boolean
  title?: string
  aria?: string
  /** Place the node after every other layer (unreachable code). */
  last?: boolean
}

export type EdgeTone = 'default' | 'faint' | 'error' | 'warn'

export interface GEdge {
  id: string
  from: string
  to: string
  label?: string
  tone?: EdgeTone
  dash?: string
  /** A loop's back edge: laid out reversed so the graph keeps its order. */
  back?: boolean
  title?: string
}

export interface GraphHandle {
  fit: () => void
  center: (id: string) => void
  zoomBy: (f: number) => void
}

const FONT_PX = 12
const SUB_PX = 10.5
const LABEL_PX = 10
const LH = 16
const PADX = 10
const PADY = 7

const elk = new ELK()

/** Above LARGE nodes the fast placement is used; up to SMALL, ELK places edge labels. */
const LARGE = 60
const SMALL = 40

interface Laid {
  dir: 'DOWN' | 'RIGHT'
  nodes: Map<string, { x: number; y: number; w: number; h: number; lines: GLine[]; cut: boolean }>
  edges: Map<string, { d: string; lx?: number; ly?: number }>
  bounds: { x: number; y: number; w: number; h: number }
}

/** fit cuts text to a width (monospace: one measure gives the advance of every character). */
function fit(text: string, font: string, max: number): string {
  const w = textWidth(text, font)
  if (w <= max || !text.length) return text
  const n = Math.max(3, Math.floor(max / (w / text.length)) - 1)
  return text.slice(0, n) + '…'
}

/** roundedPath draws an orthogonal polyline with small rounded corners. */
function roundedPath(pts: { x: number; y: number }[]): string {
  if (pts.length < 2) return ''
  let d = `M${pts[0].x},${pts[0].y}`
  for (let i = 1; i < pts.length - 1; i++) {
    const p = pts[i - 1]
    const c = pts[i]
    const n = pts[i + 1]
    const r = Math.min(6, Math.hypot(c.x - p.x, c.y - p.y) / 2, Math.hypot(n.x - c.x, n.y - c.y) / 2)
    const a = { x: c.x - (Math.sign(c.x - p.x) * r || 0), y: c.y - (Math.sign(c.y - p.y) * r || 0) }
    const b = { x: c.x + (Math.sign(n.x - c.x) * r || 0), y: c.y + (Math.sign(n.y - c.y) * r || 0) }
    d += ` L${a.x},${a.y} Q${c.x},${c.y} ${b.x},${b.y}`
  }
  const last = pts[pts.length - 1]
  return d + ` L${last.x},${last.y}`
}

async function layout(nodes: GNode[], edges: GEdge[], dir: 'DOWN' | 'RIGHT', maxW: number): Promise<Laid> {
  const mono = monoFont()
  const fMain = `${FONT_PX}px ${mono}`
  const fBold = `600 ${FONT_PX}px ${mono}`
  const fSub = `${SUB_PX}px ${mono}`
  const fLabel = `${LABEL_PX}px ${mono}`
  const sized = nodes.map((n) => {
    let w = 0
    for (const l of n.lines) w = Math.max(w, textWidth(l.t, l.b ? fBold : l.sub ? fSub : fMain))
    w = Math.min(Math.max(w + 2 * PADX, 64), maxW)
    const lines = n.lines.map((l) => ({ ...l, t: fit(l.t, l.b ? fBold : l.sub ? fSub : fMain, w - 2 * PADX) }))
    return { n, w: Math.ceil(w), h: n.lines.length * LH + 2 * PADY, lines }
  })
  const ids = new Set(nodes.map((n) => n.id))
  // ELK's best placement and its label layers cost seconds on graphs of a
  // hundred nodes: large graphs get the fast placement, and their edge
  // labels are placed here, on the longest segment of each edge.
  const big = nodes.length > LARGE
  const elkLabels = nodes.length <= SMALL
  const graph: ElkNode = {
    id: 'root',
    layoutOptions: {
      'elk.algorithm': 'layered',
      'elk.direction': dir,
      'elk.edgeRouting': 'ORTHOGONAL',
      'elk.spacing.nodeNode': dir === 'DOWN' ? '26' : '16',
      'elk.layered.spacing.nodeNodeBetweenLayers': dir === 'DOWN' ? '34' : '56',
      'elk.spacing.edgeNode': '14',
      'elk.spacing.edgeEdge': '10',
      'elk.layered.spacing.edgeNodeBetweenLayers': '14',
      'elk.spacing.edgeLabel': '3',
      'elk.layered.nodePlacement.strategy': big ? 'BRANDES_KOEPF' : 'NETWORK_SIMPLEX',
      'elk.layered.thoroughness': big ? '3' : '7',
      'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES',
      'elk.layered.cycleBreaking.strategy': 'DEPTH_FIRST',
      'elk.edgeLabels.placement': 'CENTER',
      'elk.layered.mergeEdges': 'false',
      'elk.separateConnectedComponents': 'false',
      'elk.padding': '[top=8,left=8,bottom=8,right=8]',
    },
    children: sized.map(({ n, w, h }) => ({
      id: n.id,
      width: w,
      height: h,
      layoutOptions: n.last ? { 'elk.layered.layering.layerConstraint': 'LAST' } : undefined,
    })),
    edges: edges
      .filter((e) => ids.has(e.from) && ids.has(e.to))
      .map((e): ElkExtendedEdge => ({
        id: e.id,
        sources: [e.back ? e.to : e.from],
        targets: [e.back ? e.from : e.to],
        labels: e.label && elkLabels ? [{ text: e.label, width: Math.ceil(textWidth(e.label, fLabel)) + 4, height: 12 }] : undefined,
      })),
  }
  const out = await elk.layout(graph)
  const laid: Laid = {
    dir,
    nodes: new Map(),
    edges: new Map(),
    bounds: { x: 0, y: 0, w: out.width ?? 0, h: out.height ?? 0 },
  }
  const byId = new Map(sized.map((s) => [s.n.id, s]))
  for (const c of out.children ?? []) {
    const s = byId.get(c.id)
    if (!s) continue
    const cut = s.lines.some((l, i) => l.t !== s.n.lines[i].t)
    laid.nodes.set(c.id, { x: c.x ?? 0, y: c.y ?? 0, w: c.width ?? s.w, h: c.height ?? s.h, lines: s.lines, cut })
  }
  const back = new Map(edges.map((e) => [e.id, !!e.back]))
  for (const e of (out.edges ?? []) as ElkExtendedEdge[]) {
    const sec = e.sections?.[0]
    if (!sec) continue
    let pts = [sec.startPoint, ...(sec.bendPoints ?? []), sec.endPoint]
    if (back.get(e.id)) pts = pts.slice().reverse()
    const lb = e.labels?.[0]
    if (lb) {
      laid.edges.set(e.id, { d: roundedPath(pts), lx: lb.x, ly: (lb.y ?? 0) + (lb.height ?? 12) - 2 })
      continue
    }
    // Midpoint of the longest segment: beside it when vertical, above when horizontal.
    let best = 0
    let lx = pts[0].x
    let ly = pts[0].y
    for (let i = 1; i < pts.length; i++) {
      const a = pts[i - 1]
      const b = pts[i]
      const len = Math.abs(b.x - a.x) + Math.abs(b.y - a.y)
      if (len <= best) continue
      best = len
      const vertical = Math.abs(b.x - a.x) < Math.abs(b.y - a.y)
      lx = vertical ? a.x + 2 : (a.x + b.x) / 2 - 12
      ly = vertical ? (a.y + b.y) / 2 + 3 : a.y - 3
    }
    laid.edges.set(e.id, { d: roundedPath(pts), lx, ly })
  }
  return laid
}

const TONE: Record<EdgeTone | 'path', string> = {
  default: 'var(--edge-strong)',
  faint: 'var(--edge)',
  error: 'var(--color-error)',
  warn: 'var(--color-warning)',
  path: 'var(--color-primary)',
}

export function Graph({
  nodes,
  edges,
  dir,
  maxWidth = 440,
  selected,
  onSelect,
  highlight,
  focus,
  label,
  className,
  ref,
}: {
  nodes: GNode[]
  edges: GEdge[]
  /** Layer direction; AUTO picks the one that fits the view best. */
  dir: 'DOWN' | 'RIGHT' | 'AUTO'
  /** Widest node (longer lines are cut with …; the tooltip keeps them). */
  maxWidth?: number
  selected?: string | null
  onSelect?: (id: string) => void
  /** Nodes and edges on a highlighted path; everything else is dimmed. */
  highlight?: { nodes: Set<string>; edges: Set<string> } | null
  /** Node the initial view is centred on when the graph is too large to fit. */
  focus?: string
  label: string
  className?: string
  ref?: Ref<GraphHandle>
}) {
  const uid = useId().replace(/:/g, '')
  // The layout of the current inputs (a stale one is ignored while ELK runs).
  const inputs = useMemo(() => ({ nodes, edges, dir, maxWidth }), [nodes, edges, dir, maxWidth])
  const [result, setResult] = useState<{ inputs: object; laid: Laid | null; error: string | null } | null>(null)
  const laid = result?.inputs === inputs ? result.laid : null
  const error = result?.inputs === inputs ? result.error : null
  const wrap = useRef<HTMLDivElement>(null)
  const svg = useRef<SVGSVGElement>(null)
  const vp = useRef<SVGGElement>(null)
  const view = useRef({ k: 1, tx: 0, ty: 0 })
  const [panning, setPanning] = useState(false)
  const [partial, setPartial] = useState(false)

  useEffect(() => {
    let live = true
    const run = async (): Promise<Laid> => {
      const { nodes: n, edges: e, dir: d, maxWidth: mw } = inputs
      if (d !== 'AUTO') return layout(n, e, d, mw)
      if (n.length > LARGE) return layout(n, e, 'RIGHT', mw)
      const [lr, tb] = await Promise.all([layout(n, e, 'RIGHT', mw), layout(n, e, 'DOWN', mw)])
      const W = (wrap.current?.clientWidth || 800) - 48
      const H = (wrap.current?.clientHeight || 500) - 48
      const k = (l: Laid) => Math.min(W / Math.max(l.bounds.w, 1), H / Math.max(l.bounds.h, 1))
      // Left to right reads best (origins left, destinations right); top to
      // bottom when it fits clearly better.
      return k(tb) > k(lr) * 1.25 ? tb : lr
    }
    run().then(
      (l) => live && setResult({ inputs, laid: l, error: null }),
      (e) => live && setResult({ inputs, laid: null, error: String((e as Error)?.message ?? e) }),
    )
    return () => {
      live = false
    }
  }, [inputs])

  const apply = useCallback(() => {
    const v = view.current
    vp.current?.setAttribute('transform', `translate(${v.tx},${v.ty}) scale(${v.k})`)
  }, [])

  const zoom = useCallback(
    (f: number, x: number, y: number) => {
      const v = view.current
      const k = Math.max(0.12, Math.min(3, v.k * f))
      v.tx = x - (x - v.tx) * (k / v.k)
      v.ty = y - (y - v.ty) * (k / v.k)
      v.k = k
      apply()
      setPartial(false)
    },
    [apply],
  )

  const doFit = useCallback(() => {
    if (!laid || !wrap.current) return
    const b = laid.bounds
    const W = wrap.current.clientWidth || 800
    const H = wrap.current.clientHeight || 500
    const pad = 24
    const v = view.current
    let k = Math.min(1, Math.min((W - 2 * pad) / Math.max(b.w, 1), (H - 2 * pad) / Math.max(b.h, 1)))
    if (k < 0.45) {
      // Too large to read when fitted: keep a readable scale and start at
      // the focus (the entry block, the traced variable).
      k = 0.8
      const f = (focus && laid.nodes.get(focus)) || { x: b.x, y: b.y, w: b.w, h: 0 }
      v.k = k
      v.tx = W / 2 - (f.x + f.w / 2) * k
      v.ty = laid.dir === 'RIGHT' ? H / 2 - (f.y + f.h / 2) * k : pad - f.y * k
      if (laid.dir === 'DOWN') v.tx = Math.min(pad, Math.max(W - pad - (b.x + b.w) * k, v.tx))
      setPartial(true)
    } else {
      setPartial(false)
      v.k = k
      v.tx = W / 2 - (b.x + b.w / 2) * k
      v.ty = H / 2 - (b.y + b.h / 2) * k
    }
    apply()
  }, [laid, focus, apply])

  const center = useCallback(
    (id: string) => {
      const p = laid?.nodes.get(id)
      if (!p || !wrap.current) return
      const v = view.current
      v.tx = wrap.current.clientWidth / 2 - (p.x + p.w / 2) * v.k
      v.ty = wrap.current.clientHeight / 2 - (p.y + p.h / 2) * v.k
      apply()
    },
    [laid, apply],
  )

  const zoomBy = useCallback(
    (f: number) => zoom(f, (wrap.current?.clientWidth ?? 0) / 2, (wrap.current?.clientHeight ?? 0) / 2),
    [zoom],
  )

  useImperativeHandle(ref, () => ({ fit: doFit, center, zoomBy }), [doFit, center, zoomBy])

  // Fit once per layout.
  useEffect(() => {
    if (laid) doFit()
  }, [laid, doFit])

  // Wheel zoom needs a non-passive listener.
  useEffect(() => {
    const el = svg.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const r = el.getBoundingClientRect()
      zoom(Math.exp(-e.deltaY * 0.0015), e.clientX - r.left, e.clientY - r.top)
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [zoom, laid])

  const drag = useRef<{ x: number; y: number; tx: number; ty: number } | null>(null)
  const onPointerDown = (e: React.PointerEvent<SVGSVGElement>) => {
    if ((e.target as Element).closest('[data-node]')) return
    drag.current = { x: e.clientX, y: e.clientY, tx: view.current.tx, ty: view.current.ty }
    e.currentTarget.setPointerCapture(e.pointerId)
    setPanning(true)
  }
  const onPointerMove = (e: React.PointerEvent<SVGSVGElement>) => {
    const d = drag.current
    if (!d) return
    view.current.tx = d.tx + e.clientX - d.x
    view.current.ty = d.ty + e.clientY - d.y
    apply()
  }
  const endDrag = () => {
    drag.current = null
    setPanning(false)
  }

  const markers = useMemo(() => Object.entries(TONE), [])

  return (
    <div
      ref={wrap}
      className={cx('relative h-full min-h-72 overflow-hidden rounded-box border border-line bg-canvas', className)}
      style={{ touchAction: 'none' }}
    >
      {!laid && !error && (
        <div className="absolute inset-0 flex items-center justify-center text-fg-3">
          <span className="loading loading-spinner loading-sm" />
        </div>
      )}
      {error && (
        <div className="absolute inset-0 flex items-center justify-center p-6 text-sm text-error">
          Layout failed: {error}
        </div>
      )}
      <svg
        ref={svg}
        role="application"
        aria-label={label}
        className={cx('block h-full w-full select-none', panning ? 'cursor-grabbing' : 'cursor-grab')}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
      >
        <defs>
          {markers.map(([tone, color]) => (
            <marker
              key={tone}
              id={`${uid}-m-${tone}`}
              viewBox="0 0 10 10"
              refX="9"
              refY="5"
              markerWidth="7"
              markerHeight="7"
              orient="auto-start-reverse"
            >
              <path d="M0,1 L9,5 L0,9 z" style={{ fill: color }} />
            </marker>
          ))}
        </defs>
        <g ref={vp}>
          {laid && (
            <>
              <g>
                {edges.map((e) => {
                  const g = laid.edges.get(e.id)
                  if (!g) return null
                  const on = highlight?.edges.has(e.id)
                  const dim = !!highlight && !on
                  const tone = on ? 'path' : (e.tone ?? 'default')
                  return (
                    <g key={e.id} style={{ opacity: dim ? 0.2 : 1 }}>
                      <path
                        d={g.d}
                        fill="none"
                        style={{ stroke: TONE[tone] }}
                        strokeWidth={on ? 2.4 : 1.3}
                        strokeDasharray={e.back && !e.dash ? '5 4' : e.dash}
                        markerEnd={`url(#${uid}-m-${tone})`}
                      >
                        {e.title && <title>{e.title}</title>}
                      </path>
                      {e.label && g.lx !== undefined && (
                        <text
                          x={g.lx + 2}
                          y={g.ly}
                          fontSize={LABEL_PX}
                          style={{
                            fill: e.tone === 'error' ? 'var(--color-error)' : 'var(--fg-3)',
                            fontFamily: 'var(--font-mono)',
                            paintOrder: 'stroke',
                            stroke: 'var(--canvas)',
                            strokeWidth: 3,
                          }}
                          className="pointer-events-none"
                        >
                          {e.label}
                        </text>
                      )}
                    </g>
                  )
                })}
              </g>
              <g>
                {nodes.map((n) => {
                  const p = laid.nodes.get(n.id)
                  if (!p) return null
                  const sel = selected === n.id
                  const on = highlight?.nodes.has(n.id)
                  const dim = !!highlight && !on
                  const stroke = sel || on ? 'var(--color-primary)' : (n.stroke ?? 'var(--node-border)')
                  const sw = on ? 2.5 : sel ? 2 : (n.strokeWidth ?? 1)
                  return (
                    <g
                      key={n.id}
                      data-node={n.id}
                      transform={`translate(${p.x},${p.y})`}
                      tabIndex={0}
                      role="button"
                      aria-pressed={sel}
                      aria-label={n.aria ?? n.lines.map((l) => l.t).join(' ')}
                      className="cursor-pointer outline-none [&:focus-visible>rect]:stroke-[var(--color-primary)]"
                      style={{ opacity: dim ? 0.25 : (n.opacity ?? 1) }}
                      onClick={() => onSelect?.(n.id)}
                      onKeyDown={(ev) => {
                        if (ev.key === 'Enter' || ev.key === ' ') {
                          ev.preventDefault()
                          onSelect?.(n.id)
                        }
                      }}
                    >
                      <rect
                        width={p.w}
                        height={p.h}
                        rx={n.round ? 12 : 5}
                        style={{ fill: n.fill ?? 'var(--node-fill)', stroke }}
                        strokeWidth={sw}
                        strokeDasharray={sel || on ? undefined : n.dash}
                      />
                      {p.lines.map((l, i) => (
                        <text
                          key={i}
                          x={PADX}
                          y={PADY + 11 + i * LH}
                          fontSize={l.sub ? SUB_PX : FONT_PX}
                          fontWeight={l.b ? 600 : undefined}
                          style={{ fill: l.sub ? 'var(--fg-3)' : 'var(--fg)', fontFamily: 'var(--font-mono)' }}
                          className="pointer-events-none"
                        >
                          {l.t}
                        </text>
                      ))}
                      {(n.title || p.cut) && (
                        <title>
                          {(p.cut ? n.lines.map((l) => l.t).join('\n') + (n.title ? '\n\n' : '') : '') +
                            (n.title ?? '')}
                        </title>
                      )}
                    </g>
                  )
                })}
              </g>
            </>
          )}
        </g>
      </svg>
      {laid && partial && (
        <div className="pointer-events-none absolute bottom-2 left-2 z-10 rounded-field border border-line bg-base-100/90 px-2 py-0.5 text-[11px] text-fg-3">
          Part of the graph · drag to pan, wheel to zoom
        </div>
      )}
      <div className="absolute top-2 right-2 z-10 flex gap-1">
        <ToolBtn title="Fit graph (f)" onClick={doFit}>
          <Icon name="target" className="size-3.5" />
        </ToolBtn>
        <ToolBtn title="Zoom in (+)" onClick={() => zoomBy(1.25)}>
          <Icon name="plus" className="size-3.5" />
        </ToolBtn>
        <ToolBtn title="Zoom out (−)" onClick={() => zoomBy(0.8)}>
          <Icon name="minus" className="size-3.5" />
        </ToolBtn>
      </div>
    </div>
  )
}
