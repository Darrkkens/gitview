// JourneyCanvas draws a small laid-out scene (boxes and orthogonal edges in
// world coordinates) as SVG with a pan/zoom camera, highlight/dim states,
// edge labels and a minimap. It knows nothing about routes or findings: the
// views build the scene and handle the clicks it reports. Colours are the
// theme's CSS tokens, so a theme change needs no redraw.

import {
  memo,
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type Ref,
} from 'react'
import { placeLabel } from '../../../lib/edgeLabel'
import { cx } from '../../../lib/format'
import { boundsOf, fitText, font, textWidth } from './layout'

export interface Box {
  key: string
  /** graph node index, when the box stands for a node of the dataset */
  ref?: number
  x: number
  y: number
  w: number
  h: number
  title: string
  sub?: string
  /** token of the category bar (l-handler, s-vuln…) */
  color: string
  chip?: string
  chipColor?: string
  /** position in the handler chain (drawn in a circle) */
  order?: number
  /** "+N" hidden callees */
  count?: string
  chev?: 'open' | 'closed' | null
  muted?: boolean
  tip?: string
  layer?: string
  findingId?: string
}

export interface Edge {
  key: string
  s: string
  t: string
  pts: number[]
  conf: string
  n?: number
  /** number of relation types the line stands for */
  kinds?: number
  label?: string
  /** token overriding the line colour */
  color?: string
}

export interface Bounds {
  x: number
  y: number
  w: number
  h: number
}

/** A band behind the steps of one stage of a flow (Route, Middleware, Handler…), with its name on top. */
export interface Lane {
  key: string
  label: string
  /** token of the name's square */
  color: string
  x: number
  y: number
  w: number
  h: number
}

export interface Scene {
  boxes: Box[]
  edges: Edge[]
  bounds: Bounds
  /** stages, left to right (the HTTP journey); none for other scenes */
  lanes?: Lane[]
}

/** Highlight roles: boxes sel | out | in, edges out | in. */
export interface Highlight {
  boxes: Map<string, string>
  edges: Map<string, string>
}

export interface FitOpts {
  anchor?: string | null
  align?: 'left' | 'topleft' | 'center'
  minZoom?: number
  /** screen pixels at the top covered by overlays */
  top?: number
}

export interface CanvasApi {
  fit(): void
  zoomBy(f: number): void
  reveal(keys: string[]): void
}

interface Cam {
  x: number
  y: number
  z: number
}

const TITLE = () => font(500, 12.5)
const SUB = () => font(400, 11)
const LABEL = () => font(500, 10.5)

const DASH: Record<string, string> = { likely: '6 4', possible: '2 4', unresolved: '5 4', chain: '3 3', finding: '1 3' }

const clampZ = (z: number) => Math.max(0.05, Math.min(z, 3))
const reduced = () => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches

/** path of a polyline with rounded corners, stopping short of the arrow. */
function edgePath(p: number[], cut: number): string {
  if (p.length < 4) return ''
  const n = p.length / 2
  const ex = p[p.length - 2],
    ey = p[p.length - 1],
    sx = p[p.length - 4],
    sy = p[p.length - 3]
  const len = Math.hypot(ex - sx, ey - sy) || 1
  const c = Math.min(cut, len * 0.6)
  let d = 'M' + p[0] + ' ' + p[1]
  for (let k = 1; k < n - 1; k++) {
    const x = p[2 * k],
      y = p[2 * k + 1]
    const px = p[2 * k - 2],
      py = p[2 * k - 1]
    const nx = p[2 * k + 2],
      ny = p[2 * k + 3]
    const l1 = Math.hypot(x - px, y - py) || 1,
      l2 = Math.hypot(nx - x, ny - y) || 1
    const r = Math.min(6, l1 / 2, l2 / 2)
    d += ' L' + (x - ((x - px) / l1) * r) + ' ' + (y - ((y - py) / l1) * r)
    d += ' Q' + x + ' ' + y + ' ' + (x + ((nx - x) / l2) * r) + ' ' + (y + ((ny - y) / l2) * r)
  }
  d += ' L' + (ex - ((ex - sx) / len) * c * 0.8) + ' ' + (ey - ((ey - sy) / len) * c * 0.8)
  return d
}

function arrowPath(p: number[], size: number): string {
  const ex = p[p.length - 2],
    ey = p[p.length - 1],
    sx = p[p.length - 4],
    sy = p[p.length - 3]
  const len = Math.hypot(ex - sx, ey - sy) || 1
  const ah = Math.min(size, len * 0.6),
    ux = (ex - sx) / len,
    uy = (ey - sy) / len
  return (
    'M' +
    ex +
    ' ' +
    ey +
    ' L' +
    (ex - ux * ah - uy * ah * 0.45) +
    ' ' +
    (ey - uy * ah + ux * ah * 0.45) +
    ' L' +
    (ex - ux * ah + uy * ah * 0.45) +
    ' ' +
    (ey - uy * ah - ux * ah * 0.45) +
    ' Z'
  )
}

const v = (token: string) => 'var(--' + token + ')'

interface Texts {
  title: string
  sub: string
}

/** texts fits each box's title and subtitle to its width. */
function fitTexts(scene: Scene): Map<string, Texts> {
  const tf = TITLE(),
    sf = SUB()
  const out = new Map<string, Texts>()
  for (const b of scene.boxes) {
    const countW = b.count ? textWidth(b.count, sf) + 10 : 0
    const left = 14 + (b.order ? 20 : 0),
      right = 10 + (b.chev ? 16 : 0) + countW
    out.set(b.key, {
      title: fitText(b.title, tf, Math.max(10, b.w - left - right)),
      sub: b.sub ? fitText(b.sub, sf, Math.max(10, b.w - left - 10 - (b.chip ? textWidth(b.chip, sf) + 18 : 0))) : '',
    })
  }
  return out
}

const BoxView = memo(function BoxView({
  b,
  t,
  role,
  selected,
  hover,
  dim,
}: {
  b: Box
  t: Texts
  role?: string
  selected: boolean
  hover: boolean
  dim: boolean
}) {
  const stroke =
    selected || role === 'sel'
      ? 'var(--color-primary)'
      : role === 'out' || role === 'both'
        ? v('hl-out')
        : role === 'in'
          ? v('hl-in')
          : hover
            ? v('edge-strong')
            : v('node-border')
  const twoLines = !!t.sub && b.h >= 34
  const cy = b.y + b.h / 2
  const tx = b.x + 14 + (b.order ? 20 : 0)
  let rx = b.x + b.w - 10
  const chev = b.chev ? rx - 4 : 0
  if (b.chev) rx -= 16
  const chipW = b.chip && twoLines ? textWidth(b.chip, SUB()) + 10 : 0
  return (
    <g data-key={b.key} className="cursor-pointer" style={dim ? { opacity: 'var(--dim-alpha)' } : undefined}>
      {b.tip && <title>{b.tip}</title>}
      <rect
        x={b.x}
        y={b.y}
        width={b.w}
        height={b.h}
        rx={6}
        fill={b.muted ? v('context-fill') : hover ? v('node-fill-hover') : v('node-fill')}
        stroke={stroke}
        strokeWidth={selected ? 2 : role ? 1.5 : 1}
        strokeDasharray={b.muted ? '4 3' : undefined}
        vectorEffect="non-scaling-stroke"
      />
      <rect x={b.x + 1.5} y={b.y + 4} width={3} height={b.h - 8} rx={1.5} fill={v(b.color)} />
      {b.order ? (
        <>
          <circle cx={b.x + 20} cy={cy} r={8} fill={v('surface-3')} />
          <text
            x={b.x + 20}
            y={cy + 0.5}
            textAnchor="middle"
            dominantBaseline="central"
            fontSize={10}
            fontWeight={600}
            fill={v('fg-2')}
          >
            {b.order}
          </text>
        </>
      ) : null}
      <text
        x={tx}
        y={twoLines ? cy - 7 : cy + 0.5}
        dominantBaseline="central"
        fontSize={12.5}
        fontWeight={500}
        fill={b.muted ? v('fg-2') : v('fg')}
      >
        {t.title}
      </text>
      {twoLines && (
        <text x={tx} y={cy + 8} dominantBaseline="central" fontSize={11} fill={v('fg-3')}>
          {t.sub}
        </text>
      )}
      {b.chev && (
        <path
          d={
            b.chev === 'closed'
              ? `M${chev - 2} ${cy - 4} L${chev + 2} ${cy} L${chev - 2} ${cy + 4}`
              : `M${chev - 4} ${cy - 2} L${chev} ${cy + 2} L${chev + 4} ${cy - 2}`
          }
          fill="none"
          stroke={v('fg-3')}
          strokeWidth={1.4}
          vectorEffect="non-scaling-stroke"
        />
      )}
      {b.count && (
        <text
          x={rx}
          y={twoLines ? cy - 7 : cy + 0.5}
          dominantBaseline="central"
          textAnchor="end"
          fontSize={11}
          fill={v('fg-3')}
        >
          {b.count}
        </text>
      )}
      {chipW > 0 && (
        <g>
          <rect x={b.x + b.w - chipW - 8} y={cy + 1} width={chipW} height={15} rx={7.5} fill={v('surface-3')} />
          <text
            x={b.x + b.w - chipW / 2 - 8}
            y={cy + 8.5}
            dominantBaseline="central"
            textAnchor="middle"
            fontSize={11}
            fill={v(b.chipColor || 'fg-2')}
          >
            {b.chip}
          </text>
        </g>
      )}
    </g>
  )
})

function edgeColor(e: Edge, role: string | undefined, selected: boolean, hover: boolean): string {
  if (role === 'out' || role === 'out2') return v('hl-out')
  if (role === 'in' || role === 'in2') return v('hl-in')
  if (selected) return 'var(--color-primary)'
  if (e.color) return v(e.color)
  if (e.conf === 'unresolved') return v('c-unresolved')
  if (hover) return v('edge-strong')
  return v('edge')
}

const SceneLayer = memo(function SceneLayer({
  scene,
  texts,
  selected,
  highlight,
  hover,
  hoverEdge,
}: {
  scene: Scene
  texts: Map<string, Texts>
  selected: string | null
  highlight: Highlight | null
  hover: string | null
  hoverEdge: string | null
}) {
  const strongOf = (e: Edge) => {
    const role = highlight?.edges.get(e.key)
    const isHover = hoverEdge === e.key || (!!hover && (e.s === hover || e.t === hover))
    return { role, isHover, strong: !!role || e.key === selected || isHover }
  }
  const edges = scene.edges.map((e) => ({ e, ...strongOf(e) }))
  // weak lines first, the highlighted ones on top
  edges.sort((a, b) => Number(a.strong) - Number(b.strong))
  // labels of the strong lines, never over each other nor over a step
  const labels: { e: Edge; x: number; y: number; w: number }[] = []
  const taken: number[][] = []
  const covered = scene.boxes.map((b) => [b.x, b.y, b.x + b.w, b.y + b.h])
  const want = (x: (typeof edges)[number]) => {
    if (!x.e.label) return 0
    if (x.e.key === selected || hoverEdge === x.e.key) return 3
    if (hover && (x.e.s === hover || x.e.t === hover)) return 2
    return (x.role === 'out' || x.role === 'in') && ((x.e.n ?? 1) > 1 || (x.e.kinds ?? 1) > 1) ? 1 : 0
  }
  edges
    .map((x) => [want(x), x] as const)
    .filter((x) => x[0])
    .sort((a, b) => b[0] - a[0])
    .forEach(([, x]) => {
      const w = textWidth(x.e.label!, LABEL()) + 10
      const at = placeLabel(x.e.pts, w, 16, covered, taken)
      if (at) labels.push({ e: x.e, x: at[0], y: at[1], w })
    })
  return (
    <>
      {scene.lanes && <LaneBands lanes={scene.lanes} />}
      <g>
        {edges.map(({ e, role, isHover, strong }) => {
          const color = edgeColor(e, role, e.key === selected, isHover)
          const far = role === 'out2' || role === 'in2'
          const width = 1 + Math.min(Math.log2(1 + (e.n || 1)), 3) * 0.45 + ((strong && !far) || isHover ? 0.7 : 0)
          const dim = !!highlight && !role && e.key !== selected
          const style = dim ? { opacity: 'var(--dim-alpha)' } : far && !isHover ? { opacity: 0.45 } : undefined
          return (
            <g key={e.key} data-edge={e.key} className="cursor-pointer" style={style}>
              {e.label && <title>{e.label}</title>}
              <path
                d={edgePath(e.pts, 8)}
                fill="none"
                stroke="transparent"
                strokeWidth={12}
                vectorEffect="non-scaling-stroke"
              />
              <path
                d={edgePath(e.pts, 8)}
                fill="none"
                stroke={color}
                strokeWidth={width}
                strokeDasharray={DASH[e.conf]}
                vectorEffect="non-scaling-stroke"
              />
              <path d={arrowPath(e.pts, 8)} fill={color} />
            </g>
          )
        })}
      </g>
      <g>
        {scene.boxes.map((b) => (
          <BoxView
            key={b.key}
            b={b}
            t={texts.get(b.key)!}
            role={highlight?.boxes.get(b.key)}
            selected={b.key === selected}
            hover={b.key === hover}
            dim={!!highlight && !highlight.boxes.has(b.key)}
          />
        ))}
      </g>
      <g pointerEvents="none">
        {labels.map((l) => (
          <g key={l.e.key}>
            <rect
              x={l.x - l.w / 2}
              y={l.y - 8}
              width={l.w}
              height={16}
              rx={8}
              fill={v('surface-1')}
              stroke={v('line-strong')}
              vectorEffect="non-scaling-stroke"
            />
            <text
              x={l.x}
              y={l.y + 0.5}
              textAnchor="middle"
              dominantBaseline="central"
              fontSize={10.5}
              fontWeight={500}
              fill={v('fg-2')}
            >
              {l.e.label}
            </text>
          </g>
        ))}
      </g>
    </>
  )
})

/** Lane bands, every other one tinted, with a rule between them. */
function LaneBands({ lanes }: { lanes: Lane[] }) {
  return (
    <g pointerEvents="none">
      {lanes.map((l, k) => (
        <g key={l.key}>
          {k % 2 === 1 && <rect x={l.x} y={l.y} width={l.w} height={l.h} fill={v('group-fill')} />}
          {k > 0 && (
            <line
              x1={l.x}
              y1={l.y}
              x2={l.x}
              y2={l.y + l.h}
              stroke={v('group-border')}
              strokeDasharray="3 5"
              vectorEffect="non-scaling-stroke"
            />
          )}
        </g>
      ))}
    </g>
  )
}

/**
 * LaneHeads: the lanes' names in screen space, at a fixed size: on top of
 * their lane, kept in sight below the overlays (top) while the view scrolls
 * down the lanes.
 */
function LaneHeads({
  lanes,
  cam,
  size,
  top,
}: {
  lanes: Lane[]
  cam: Cam
  size: { w: number; h: number }
  top: number
}) {
  const sx = (x: number) => (x - cam.x) * cam.z + size.w / 2
  const sy = (y: number) => (y - cam.y) * cam.z + size.h / 2
  const f = font(600, 10.5)
  return (
    <g pointerEvents="none" aria-hidden="true">
      {lanes.map((l) => {
        const x0 = Math.max(sx(l.x), 0),
          x1 = Math.min(sx(l.x + l.w), size.w)
        const y = Math.max(sy(l.y) + 6, top)
        if (x1 - x0 < 48 || y > sy(l.y + l.h) - 30) return null
        const text = fitText(l.label.toUpperCase(), f, x1 - x0 - 46)
        if (!text || text === '…') return null
        // the letter spacing widens the text by 0.4px a character
        const w = textWidth(text, f) + text.length * 0.4 + 30
        return (
          <g key={l.key} transform={`translate(${x0 + 8} ${y})`}>
            <rect width={w} height={20} rx={4} fill={v('surface-1')} stroke={v('line')} />
            <rect x={8} y={6.5} width={7} height={7} rx={1.5} fill={v(l.color)} />
            <text
              x={21}
              y={10.5}
              dominantBaseline="central"
              fontSize={10.5}
              fontWeight={600}
              letterSpacing={0.4}
              fill={v('fg-2')}
            >
              {text}
            </text>
          </g>
        )
      })}
    </g>
  )
}

function Minimap({
  scene,
  cam,
  size,
  highlight,
  selected,
  onMove,
}: {
  scene: Scene
  cam: Cam
  size: { w: number; h: number }
  highlight: Highlight | null
  selected: string | null
  onMove: (x: number, y: number) => void
}) {
  const W = 168,
    H = 108,
    pad = 6
  const bb = scene.bounds
  const s = Math.min((W - pad * 2) / Math.max(bb.w, 1), (H - pad * 2) / Math.max(bb.h, 1))
  const ox = (W - bb.w * s) / 2 - bb.x * s,
    oy = (H - bb.h * s) / 2 - bb.y * s
  const ref = useRef<SVGSVGElement>(null)
  const move = (ev: { clientX: number; clientY: number }) => {
    const r = ref.current!.getBoundingClientRect()
    onMove((ev.clientX - r.left - ox) / s, (ev.clientY - r.top - oy) / s)
  }
  const vw = (size.w / cam.z) * s,
    vh = (size.h / cam.z) * s
  return (
    <svg
      ref={ref}
      width={W}
      height={H}
      className="absolute right-3 bottom-3 cursor-crosshair rounded-box border border-line bg-base-100 shadow-sm print:hidden"
      aria-label="Minimap"
      onPointerDown={(e) => {
        e.stopPropagation()
        const el = e.currentTarget
        el.setPointerCapture(e.pointerId)
        move(e)
        const mv = (ev: PointerEvent) => move(ev)
        const up = () => {
          el.removeEventListener('pointermove', mv)
          el.removeEventListener('pointerup', up)
        }
        el.addEventListener('pointermove', mv)
        el.addEventListener('pointerup', up)
      }}
    >
      {scene.boxes.map((b) => (
        <rect
          key={b.key}
          x={b.x * s + ox}
          y={b.y * s + oy}
          width={Math.max(1.5, b.w * s)}
          height={Math.max(1.5, b.h * s)}
          fill={b.key === selected ? 'var(--color-primary)' : v(b.color)}
          opacity={highlight && !highlight.boxes.has(b.key) ? 0.25 : 0.85}
        />
      ))}
      <rect
        x={(cam.x - size.w / 2 / cam.z) * s + ox}
        y={(cam.y - size.h / 2 / cam.z) * s + oy}
        width={vw}
        height={vh}
        fill={v('accent-weak')}
        stroke="var(--color-primary)"
        strokeWidth={1.5}
      />
    </svg>
  )
}

export interface JourneyCanvasProps {
  scene: Scene | null
  selected?: string | null
  highlight?: Highlight | null
  fitOpts?: FitOpts
  /** a new value fits the camera to the scene (a new route or finding) */
  fitKey: string
  /** box kept in place when the scene changes without a new fitKey */
  anchor?: string | null
  minimap?: boolean
  onBox?: (b: Box, ev: { shiftKey: boolean }) => void
  onBoxDouble?: (b: Box) => void
  onEdge?: (e: Edge) => void
  onEmpty?: () => void
  apiRef?: Ref<CanvasApi>
  label: string
  children?: ReactNode
}

export function JourneyCanvas({
  scene,
  selected = null,
  highlight = null,
  fitOpts,
  fitKey,
  anchor,
  minimap = true,
  onBox,
  onBoxDouble,
  onEdge,
  onEmpty,
  apiRef,
  label,
  children,
}: JourneyCanvasProps) {
  const wrap = useRef<HTMLDivElement>(null)
  const svg = useRef<SVGSVGElement>(null)
  const [size, setSize] = useState({ w: 0, h: 0 })
  const [cam, setCamState] = useState<Cam>({ x: 0, y: 0, z: 1 })
  const camRef = useRef(cam)
  const tween = useRef(0)
  const moved = useRef(false) // the viewer moved the camera since the last fit
  const fitted = useRef<string | null>(null)
  const prevScene = useRef<Scene | null>(null)
  const [hover, setHover] = useState<string | null>(null)
  const [grabbing, setGrabbing] = useState(false)
  const [hoverEdge, setHoverEdge] = useState<string | null>(null)

  const setCam = useCallback((c: Cam) => {
    cancelAnimationFrame(tween.current)
    camRef.current = c
    setCamState(c)
  }, [])
  const animateTo = useCallback(
    (to: Cam, animate: boolean) => {
      cancelAnimationFrame(tween.current)
      if (!animate || reduced()) {
        setCam(to)
        return
      }
      const from = camRef.current,
        t0 = performance.now(),
        ms = 280
      const step = (now: number) => {
        const p = Math.max(0, Math.min(1, (now - t0) / ms)),
          e = 1 - Math.pow(1 - p, 3)
        const c = { x: from.x + (to.x - from.x) * e, y: from.y + (to.y - from.y) * e, z: from.z + (to.z - from.z) * e }
        camRef.current = c
        setCamState(c)
        if (p < 1) tween.current = requestAnimationFrame(step)
      }
      tween.current = requestAnimationFrame(step)
    },
    [setCam],
  )

  useLayoutEffect(() => {
    const el = wrap.current
    if (!el) return
    const ro = new ResizeObserver(() => {
      const r = el.getBoundingClientRect()
      setSize((s) => (Math.abs(s.w - r.width) < 1 && Math.abs(s.h - r.height) < 1 ? s : { w: r.width, h: r.height }))
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const texts = useMemo(() => (scene ? fitTexts(scene) : new Map<string, Texts>()), [scene])
  const byKey = useMemo(() => new Map((scene?.boxes ?? []).map((b) => [b.key, b])), [scene])
  const edgeByKey = useMemo(() => new Map((scene?.edges ?? []).map((e) => [e.key, e])), [scene])

  const fitZoom = useCallback(
    (bb: Bounds, pad = 48, top = 0) =>
      clampZ(Math.min((size.w - pad * 2) / Math.max(bb.w, 1), (size.h - pad * 2 - 30 - top) / Math.max(bb.h, 1), 1.25)),
    [size],
  )
  const fit = useCallback(
    (animate: boolean) => {
      if (!scene || !size.w) return
      moved.current = false
      const bb = scene.bounds
      const top = fitOpts?.top ?? 0
      const z = fitZoom(bb, 48, top)
      const b = fitOpts?.anchor ? byKey.get(fitOpts.anchor) : undefined
      if (fitOpts?.minZoom && z < fitOpts.minZoom && b) {
        // too large to be read at once: show it at a readable zoom, from the anchor
        const m = fitOpts.minZoom
        const x =
          fitOpts.align === 'left' || fitOpts.align === 'topleft' ? b.x - 60 / m + size.w / 2 / m : b.x + b.w / 2
        let y = fitOpts.align === 'topleft' ? b.y - (top + 24) / m + size.h / 2 / m : b.y + b.h / 2 - top / 2 / m
        if (fitOpts.align === 'left') {
          // from the drawing's top when the anchor stays in sight, else centred on the anchor
          const fromTop = bb.y - (top + 8) / m + size.h / 2 / m
          if (b.y + b.h + 40 / m <= fromTop + size.h / 2 / m) y = fromTop
        }
        animateTo({ x, y, z: m }, animate)
        return
      }
      animateTo({ x: bb.x + bb.w / 2, y: bb.y + bb.h / 2 - (top / 2 - 12) / z, z }, animate)
    },
    [scene, size, fitZoom, fitOpts, byKey, animateTo],
  )
  const zoomBy = useCallback(
    (f: number, sx?: number, sy?: number) => {
      const c = camRef.current
      const z = clampZ(c.z * f)
      if (sx === undefined || sy === undefined) {
        sx = size.w / 2
        sy = size.h / 2
      }
      const wx = (sx - size.w / 2) / c.z + c.x,
        wy = (sy - size.h / 2) / c.z + c.y
      moved.current = true
      setCam({ x: wx - (sx - size.w / 2) / z, y: wy - (sy - size.h / 2) / z, z })
    },
    [size, setCam],
  )
  const reveal = useCallback(
    (keys: string[]) => {
      const bs = keys.map((k) => byKey.get(k)).filter((b): b is Box => !!b)
      if (!bs.length || !size.w) return
      const bb = boundsOf(bs)
      const c = camRef.current
      // already fully visible: keep the camera
      const vx0 = c.x - size.w / 2 / c.z,
        vy0 = c.y - size.h / 2 / c.z,
        vx1 = c.x + size.w / 2 / c.z,
        vy1 = c.y + size.h / 2 / c.z
      if (bb.x >= vx0 && bb.y >= vy0 && bb.x + bb.w <= vx1 && bb.y + bb.h <= vy1) return
      let z = c.z
      const need = fitZoom(bb, 80)
      if (z > need) z = need
      if (z < 0.55 && need >= 0.55) z = Math.min(need, 0.9)
      animateTo({ x: bb.x + bb.w / 2, y: bb.y + bb.h / 2, z }, true)
    },
    [byKey, size, fitZoom, animateTo],
  )
  useImperativeHandle(apiRef, () => ({ fit: () => fit(true), zoomBy: (f: number) => zoomBy(f), reveal }), [
    fit,
    zoomBy,
    reveal,
  ])

  // Fit when the subject changes; otherwise keep the anchor box in place.
  // fit and the anchor are read when the scene changes, not followed.
  const latest = useRef({ fit, anchor })
  // a layout effect, declared first: it runs before the one below
  useLayoutEffect(() => {
    latest.current = { fit, anchor }
  })
  const ready = size.w > 0
  useLayoutEffect(() => {
    if (!scene || !ready) return
    const prev = prevScene.current
    prevScene.current = scene
    if (fitted.current !== fitKey) {
      fitted.current = fitKey
      latest.current.fit(false)
      return
    }
    const anchor = latest.current.anchor
    if (prev && prev !== scene && anchor) {
      const a = prev.boxes.find((b) => b.key === anchor),
        b = scene.boxes.find((x) => x.key === anchor)
      if (a && b) {
        const c = camRef.current
        setCam({ x: c.x + (b.x - a.x), y: c.y + (b.y - a.y), z: c.z })
      }
    }
  }, [scene, fitKey, ready, setCam])

  // A resized stage fits again unless the viewer moved the camera.
  const lastSize = useRef(size)
  useEffect(() => {
    const p = lastSize.current
    lastSize.current = size
    if (!p.w || (Math.abs(p.w - size.w) < 2 && Math.abs(p.h - size.h) < 2)) return
    if (!moved.current) fit(false)
  }, [size, fit])

  // wheel zoom (non-passive)
  useEffect(() => {
    const el = svg.current
    if (!el) return
    const onWheel = (ev: WheelEvent) => {
      ev.preventDefault()
      const r = el.getBoundingClientRect()
      const k = ev.ctrlKey ? 0.012 : 0.0018
      const d = ev.deltaMode === 1 ? ev.deltaY * 16 : ev.deltaY
      zoomBy(Math.exp(-d * k), ev.clientX - r.left, ev.clientY - r.top)
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [zoomBy])

  const drag = useRef<{
    x: number
    y: number
    cx: number
    cy: number
    moved: boolean
    box?: string
    edge?: string
  } | null>(null)
  const lastClick = useRef<{ key: string; t: number } | null>(null)
  const hit = (target: EventTarget | null) => {
    const el = target as Element | null
    const b = el?.closest?.('[data-key]')?.getAttribute('data-key') ?? undefined
    const e = b ? undefined : (el?.closest?.('[data-edge]')?.getAttribute('data-edge') ?? undefined)
    return { box: b, edge: e }
  }

  const onPointerDown = (ev: React.PointerEvent<SVGSVGElement>) => {
    if (ev.button !== 0) return
    wrap.current?.focus({ preventScroll: true })
    const h = hit(ev.target)
    const c = camRef.current
    drag.current = { x: ev.clientX, y: ev.clientY, cx: c.x, cy: c.y, moved: false, ...h }
    ev.currentTarget.setPointerCapture(ev.pointerId)
  }
  const onPointerMove = (ev: React.PointerEvent<SVGSVGElement>) => {
    const d = drag.current
    if (d) {
      const dx = ev.clientX - d.x,
        dy = ev.clientY - d.y
      if (!d.moved && Math.hypot(dx, dy) > 4) {
        d.moved = true
        setGrabbing(true)
      }
      if (d.moved) {
        const c = camRef.current
        moved.current = true
        setCam({ x: d.cx - dx / c.z, y: d.cy - dy / c.z, z: c.z })
      }
      return
    }
    const h = hit(ev.target)
    if ((h.box ?? null) !== hover) setHover(h.box ?? null)
    if ((h.edge ?? null) !== hoverEdge) setHoverEdge(h.edge ?? null)
  }
  const onPointerUp = (ev: React.PointerEvent<SVGSVGElement>) => {
    const d = drag.current
    drag.current = null
    if (d?.moved) setGrabbing(false)
    if (!d || d.moved) return
    const now = performance.now()
    if (d.box) {
      const b = byKey.get(d.box)
      if (!b) return
      onBox?.(b, { shiftKey: ev.shiftKey })
      const lc = lastClick.current
      if (lc && lc.key === d.box && now - lc.t < 380) {
        lastClick.current = null
        onBoxDouble?.(b)
      } else lastClick.current = { key: d.box, t: now }
      return
    }
    if (d.edge) {
      const e = edgeByKey.get(d.edge)
      if (e) onEdge?.(e)
      lastClick.current = null
      return
    }
    const lc = lastClick.current
    if (lc && lc.key === '' && now - lc.t < 380) {
      lastClick.current = null
      fit(true)
      return
    }
    lastClick.current = { key: '', t: now }
    onEmpty?.()
  }
  const onKeyDown = (ev: React.KeyboardEvent) => {
    const c = camRef.current
    const step = 60 / c.z
    const moves: Record<string, [number, number]> = {
      ArrowLeft: [-step, 0],
      ArrowRight: [step, 0],
      ArrowUp: [0, -step],
      ArrowDown: [0, step],
    }
    const m = moves[ev.key]
    if (m && !ev.altKey) {
      moved.current = true
      setCam({ x: c.x + m[0], y: c.y + m[1], z: c.z })
      ev.preventDefault()
    }
  }

  const transform = `translate(${size.w / 2 - cam.x * cam.z} ${size.h / 2 - cam.y * cam.z}) scale(${cam.z})`
  return (
    <div
      ref={wrap}
      tabIndex={0}
      onKeyDown={onKeyDown}
      aria-label={label}
      className="@container relative h-full w-full overflow-hidden bg-canvas outline-none [background-image:radial-gradient(var(--canvas-grid)_1px,transparent_1px)] [background-size:22px_22px] focus-visible:outline-none"
    >
      <svg
        ref={svg}
        width={size.w}
        height={size.h}
        className={cx('absolute inset-0 touch-none select-none', grabbing && 'cursor-grabbing')}
        fontFamily="var(--font-sans)"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerLeave={() => {
          if (hover) setHover(null)
          if (hoverEdge) setHoverEdge(null)
        }}
      >
        {scene && (
          <g transform={transform}>
            <SceneLayer
              scene={scene}
              texts={texts}
              selected={selected}
              highlight={highlight}
              hover={hover}
              hoverEdge={hoverEdge}
            />
          </g>
        )}
        {scene?.lanes && <LaneHeads lanes={scene.lanes} cam={cam} size={size} top={(fitOpts?.top ?? 0) + 4} />}
      </svg>
      {children}
      {minimap && scene && scene.boxes.length > 0 && size.w >= 560 && size.h >= 360 && (
        <Minimap
          scene={scene}
          cam={cam}
          size={size}
          highlight={highlight}
          selected={selected}
          onMove={(x, y) => {
            moved.current = true
            setCam({ x, y, z: camRef.current.z })
          }}
        />
      )}
    </div>
  )
}
