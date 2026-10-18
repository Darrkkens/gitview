// The charts of the runtime report, drawn as SVG at the width of their
// container (text keeps its size): process metrics over time with the
// capture's phases, the requests' timeline, latency distributions and the
// A/B latency comparison. Colours are theme tokens (CSS variables).

import { useLayoutEffect, useMemo, useState, type ReactNode } from 'react'
import { cx } from '../../lib/format'
import { dur, niceStep, phaseAt } from './fmt'
import type { Latency, Phases, RequestRow } from './types'
import { Swatch } from './ui'

const ZONE: Record<string, string> = {
  baseline: 'color-mix(in oklab, var(--fg-3) 16%, transparent)',
  active: 'color-mix(in oklab, var(--color-success) 15%, transparent)',
  cooldown: 'color-mix(in oklab, var(--color-secondary) 14%, transparent)',
  paused: 'color-mix(in oklab, var(--color-warning) 24%, transparent)',
}

const AXIS = { fill: 'var(--fg-3)', fontSize: 10.5 }

/**
 * useWidth measures the width of an element (charts are drawn at that
 * width). It returns a callback ref, so an element mounted later is measured.
 */
function useWidth<T extends HTMLElement>(): [(el: T | null) => void, number] {
  const [el, setEl] = useState<T | null>(null)
  const [w, setW] = useState(0)
  useLayoutEffect(() => {
    if (!el) return
    // The observer reports the initial size too.
    const ro = new ResizeObserver(() => setW(el.clientWidth))
    ro.observe(el)
    return () => ro.disconnect()
  }, [el])
  return [setEl, w]
}

/** PhaseLegend explains the shaded phases of the time charts. */
export function PhaseLegend({ paused, extra }: { paused?: boolean; extra?: ReactNode }) {
  return (
    <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1">
      <Swatch color={ZONE.baseline} label="baseline" />
      <Swatch color={ZONE.active} label="active" />
      <Swatch color={ZONE.cooldown} label="cooldown" />
      {paused && <Swatch color={ZONE.paused} label="paused" />}
      {extra}
    </div>
  )
}

function zones(ph: Phases, tmax: number): [number, number, string][] {
  const out: [number, number, string][] = [
    [0, ph.activeStart / 1e9, ZONE.baseline],
    [ph.activeStart / 1e9, ph.activeEnd / 1e9, ZONE.active],
    [ph.activeEnd / 1e9, tmax, ZONE.cooldown],
  ]
  for (const w of ph.paused ?? []) out.push([w.start / 1e9, w.end / 1e9, ZONE.paused])
  return out
}

function timeTicks(tmax: number, width: number): number[] {
  const step = niceStep(tmax, Math.max(2, Math.floor(width / 90)))
  const out: number[] = []
  for (let t = 0; t <= tmax + 1e-9; t += step) out.push(t)
  return out
}

/** labelGroups joins labels closer than 28px: [["p50 · p90", x], …]. */
function labelGroups(items: [string, number][]): [string, number][] {
  const out: [string, number][] = []
  for (const [t, x] of items) {
    const last = out[out.length - 1]
    if (last && Math.abs(x - last[1]) < 28) last[0] += ' · ' + t
    else out.push([t, x])
  }
  return out
}

/** anchor keeps an axis label inside [lo, hi]. */
const anchor = (x: number, lo: number, hi: number) => (x - lo < 18 ? 'start' : hi - x < 18 ? 'end' : 'middle')

const tlabel = (t: number, tmax: number) => (tmax < 10 ? t.toFixed(1) : t.toFixed(0)) + 's'

/** At most this many requests are drawn (the table lists them all). */
const MAX_BARS = 5000

export interface Mark {
  t: number // seconds
  dt: number // seconds
  color: string
  title: string
}

/**
 * TimeChart draws one series over the capture's timeline, its phases
 * shaded and, optionally, marks along the top (the requests). Hovering
 * reads the nearest sample.
 */
export function TimeChart({
  points,
  phases,
  fmt,
  color,
  height = 170,
  marks,
  label,
  unitLabel,
}: {
  points: [number, number][]
  phases: Phases
  fmt: (v: number) => string
  color: string
  height?: number
  marks?: Mark[]
  label: string
  unitLabel?: string
}) {
  const [ref, W] = useWidth<HTMLDivElement>()
  const [hover, setHover] = useState<number | null>(null)
  const L = 58
  const R = 12
  const T = marks?.length ? 14 : 8
  const B = 22
  const H = height

  const g = useMemo(() => {
    let tmax = phases.end / 1e9 || 0
    let vmax = 0
    for (const [t, v] of points) {
      if (t > tmax) tmax = t
      if (v > vmax) vmax = v
    }
    if (tmax <= 0) tmax = 1
    // Round ticks: a nice step, and the top at a multiple of it.
    const step = niceStep(vmax || 1, 4)
    vmax = Math.max(step, Math.ceil(vmax / step - 1e-9) * step)
    const ticks: number[] = []
    for (let v = 0; v <= vmax + step * 1e-6; v += step) ticks.push(v)
    return { tmax, vmax, ticks }
  }, [points, phases.end])

  if (!W) return <div ref={ref} style={{ height: H }} />
  const iw = W - L - R
  const x = (t: number) => L + (iw * t) / g.tmax
  const y = (v: number) => T + (H - T - B) * (1 - v / g.vmax)
  let d = ''
  points.forEach(([t, v], i) => (d += (i ? 'L' : 'M') + x(t).toFixed(1) + ' ' + y(v).toFixed(1)))
  const area =
    points.length > 1
      ? d + `L${x(points[points.length - 1][0]).toFixed(1)} ${y(0)}L${x(points[0][0]).toFixed(1)} ${y(0)}Z`
      : ''

  const onMove = (e: React.PointerEvent<SVGSVGElement>) => {
    if (!points.length) return
    const r = e.currentTarget.getBoundingClientRect()
    const t = ((e.clientX - r.left - L) / iw) * g.tmax
    let lo = 0
    let hi = points.length - 1
    while (lo < hi) {
      const mid = (lo + hi) >> 1
      if (points[mid][0] < t) lo = mid + 1
      else hi = mid
    }
    if (lo > 0 && Math.abs(points[lo - 1][0] - t) < Math.abs(points[lo][0] - t)) lo--
    setHover(lo)
  }
  const hp = hover !== null ? points[hover] : null

  return (
    <div ref={ref} className="relative">
      <svg
        width={W}
        height={H}
        role="img"
        aria-label={label}
        onPointerMove={onMove}
        onPointerLeave={() => setHover(null)}
        className="block"
      >
        {zones(phases, g.tmax).map(([a, b, fill], i) =>
          b > a ? (
            <rect key={i} x={x(a)} y={T} width={Math.max(x(b) - x(a), 0)} height={H - T - B} style={{ fill }} />
          ) : null,
        )}
        {g.ticks.map((v, k) => {
          return (
            <g key={k}>
              <line x1={L} x2={W - R} y1={y(v)} y2={y(v)} style={{ stroke: 'var(--line)' }} />
              <text x={L - 6} y={y(v) + 3.5} textAnchor="end" style={AXIS}>
                {fmt(v)}
              </text>
            </g>
          )
        })}
        {timeTicks(g.tmax, iw).map((t) => (
          <text key={t} x={x(t)} y={H - 6} textAnchor={anchor(x(t), 0, W)} style={AXIS}>
            {tlabel(t, g.tmax)}
          </text>
        ))}
        {area && <path d={area} style={{ fill: `color-mix(in oklab, ${color} 14%, transparent)` }} />}
        {points.length > 1 && (
          <path d={d} fill="none" style={{ stroke: color }} strokeWidth={1.6} strokeLinejoin="round" />
        )}
        {points.length === 1 && <circle cx={x(points[0][0])} cy={y(points[0][1])} r={3} style={{ fill: color }} />}
        {(marks ?? []).slice(0, MAX_BARS).map((m, i) => (
          <rect
            key={i}
            x={x(m.t)}
            y={2}
            width={Math.max(x(m.t + m.dt) - x(m.t), 1.5)}
            height={6}
            rx={1}
            style={{ fill: m.color }}
          >
            <title>{m.title}</title>
          </rect>
        ))}
        {hp && (
          <g pointerEvents="none">
            <line
              x1={x(hp[0])}
              x2={x(hp[0])}
              y1={T}
              y2={H - B}
              style={{ stroke: 'var(--fg-3)' }}
              strokeDasharray="3 3"
            />
            <circle
              cx={x(hp[0])}
              cy={y(hp[1])}
              r={3.5}
              style={{ fill: color, stroke: 'var(--color-base-100)' }}
              strokeWidth={1.5}
            />
          </g>
        )}
      </svg>
      {hp && (
        <div
          className="pointer-events-none absolute top-1 z-10 rounded-field border border-line bg-base-100 px-2 py-1 text-[11px] whitespace-nowrap shadow-md"
          style={x(hp[0]) > W / 2 ? { right: W - x(hp[0]) + 8 } : { left: x(hp[0]) + 8 }}
        >
          <div className="font-mono font-semibold text-fg">
            {fmt(hp[1])}
            {unitLabel && <span className="font-sans font-normal text-fg-3"> {unitLabel}</span>}
          </div>
          <div className="text-fg-3">
            {hp[0].toFixed(2)} s · {phaseAt(hp[0], phases)}
          </div>
        </div>
      )}
    </div>
  )
}

/**
 * RequestTimeline places each request on the capture's timeline, one lane
 * per route; the active phase is shaded. A lane's name filters the route.
 */
export function RequestTimeline({
  rows,
  phases,
  onRoute,
}: {
  rows: RequestRow[]
  phases: Phases
  onRoute?: (key: string) => void
}) {
  const [ref, W] = useWidth<HTMLDivElement>()
  const lanes = useMemo(() => {
    const m = new Map<string, number>()
    for (const r of rows) if (!m.has(r.key)) m.set(r.key, m.size)
    return m
  }, [rows])
  const tmax = useMemo(() => {
    let t = phases.end / 1e9
    for (const r of rows) t = Math.max(t, (r.off + r.dur) / 1e9)
    return t || 0.001
  }, [rows, phases.end])
  const LANE = 22
  const H = Math.max(44, lanes.size * LANE + 30)
  // Lane names sit in a gutter on the left, bars to their right.
  const longest = Math.max(0, ...[...lanes.keys()].map((k) => k.length))
  const L = Math.min(Math.max(80, longest * 6.4 + 20), Math.max(80, W * 0.3))
  const R = 12
  if (!W) return <div ref={ref} style={{ height: H }} />
  const iw = W - L - R
  const x = (t: number) => L + (iw * t) / tmax
  return (
    <div ref={ref} className="relative rounded-box border border-line bg-base-100">
      <svg width={W} height={H} role="img" aria-label="Execution timeline of the requests" className="block">
        <rect x={L} y={0} width={iw} height={H - 20} style={{ fill: 'var(--canvas)' }} />
        <rect
          x={x(phases.activeStart / 1e9)}
          y={0}
          width={Math.max(x(phases.activeEnd / 1e9) - x(phases.activeStart / 1e9), 0)}
          height={H - 20}
          style={{ fill: ZONE.active }}
        />
        {(phases.paused ?? []).map((w, i) => (
          <rect
            key={i}
            x={x(w.start / 1e9)}
            y={0}
            width={Math.max(x(w.end / 1e9) - x(w.start / 1e9), 0)}
            height={H - 20}
            style={{ fill: ZONE.paused }}
          />
        ))}
        {[...lanes.keys()].map((_, i) => (
          <line
            key={i}
            x1={0}
            x2={W}
            y1={(i + 1) * LANE + 2}
            y2={(i + 1) * LANE + 2}
            style={{ stroke: 'var(--line)' }}
            strokeOpacity={0.6}
          />
        ))}
        {(rows.length > MAX_BARS ? rows.slice(0, MAX_BARS) : rows).map((r, i) => {
          const lane = lanes.get(r.key) ?? 0
          const bad = r.status >= 500 || !!r.error
          return (
            <rect
              key={i}
              x={x(r.off / 1e9)}
              y={6 + lane * LANE}
              width={Math.max(x((r.off + r.dur) / 1e9) - x(r.off / 1e9), 2)}
              height={13}
              rx={2}
              style={{
                fill: bad ? 'var(--color-error)' : r.status >= 400 ? 'var(--color-warning)' : 'var(--color-primary)',
              }}
              fillOpacity={0.85}
            >
              <title>
                {r.key} · {r.path} · {r.status} · {dur(r.dur)} · at {(r.off / 1e9).toFixed(3)} s
                {r.error ? ' · ' + r.error : ''}
                {r.panic ? ' · panic' : ''}
              </title>
            </rect>
          )
        })}
        {timeTicks(tmax, iw).map((t) => (
          <text key={t} x={x(t)} y={H - 5} textAnchor={anchor(x(t), L, W)} style={AXIS}>
            {tlabel(t, tmax)}
          </text>
        ))}
      </svg>
      {[...lanes.entries()].map(([k, i]) => (
        <button
          key={k}
          type="button"
          onClick={() => onRoute?.(k)}
          title={onRoute ? 'Show only ' + k : k}
          className="absolute left-2 truncate text-left font-mono text-[10.5px] leading-4 text-fg-2 hover:text-primary"
          style={{ top: 5 + i * LANE, width: L - 14 }}
        >
          {k}
        </button>
      ))}
      {rows.length > MAX_BARS && (
        <div className="border-t border-line px-2 py-1 text-[11px] text-fg-3">
          The timeline draws the first {MAX_BARS.toLocaleString('en-US')} of {rows.length.toLocaleString('en-US')} requests.
        </div>
      )}
    </div>
  )
}

/**
 * LatencyHistogram shows the distribution of a route's latencies (ms) with
 * its percentiles; a logarithmic axis when they span more than 20×.
 */
export function LatencyHistogram({ durations, latency }: { durations: number[]; latency: Latency }) {
  const [ref, W] = useWidth<HTMLDivElement>()
  const H = 104
  const L = 8
  const R = 8
  const T = 22
  const B = 18
  const data = useMemo(() => {
    const vs = durations.filter((v) => v > 0 && isFinite(v))
    if (!vs.length) return null
    const min = Math.min(...vs)
    const max = Math.max(...vs)
    const log = max / min > 20
    const f = (v: number) => (log ? Math.log10(v) : v)
    const lo = f(min)
    const hi = f(max) === lo ? lo + (log ? 0.1 : Math.max(lo * 0.1, 0.01)) : f(max)
    const nb = Math.min(40, Math.max(8, Math.round(Math.sqrt(vs.length) * 2)))
    const bins = new Array<number>(nb).fill(0)
    for (const v of vs) bins[Math.min(nb - 1, Math.floor(((f(v) - lo) / (hi - lo)) * nb))]++
    return { bins, lo, hi, log, f, min, max, top: Math.max(...bins) }
  }, [durations])
  if (!data) return null
  if (!W) return <div ref={ref} style={{ height: H }} />
  const iw = W - L - R
  const px = (ms: number) => L + (iw * (data.f(ms) - data.lo)) / (data.hi - data.lo)
  const bw = iw / data.bins.length
  const marks: [string, number][] = [
    ['p50', latency.p50 / 1e6],
    ['p90', latency.p90 / 1e6],
    ['p99', latency.p99 / 1e6],
  ]
  return (
    <div ref={ref}>
      <svg width={W} height={H} role="img" aria-label="Latency distribution" className="block">
        {data.bins.map((n, i) =>
          n ? (
            <rect
              key={i}
              x={L + i * bw + 0.5}
              y={T + (H - T - B) * (1 - n / data.top)}
              width={Math.max(bw - 1, 1)}
              height={(H - T - B) * (n / data.top)}
              rx={1}
              style={{ fill: 'color-mix(in oklab, var(--color-primary) 70%, transparent)' }}
            >
              <title>{n === 1 ? '1 request' : n + ' requests'}</title>
            </rect>
          ) : null,
        )}
        <line x1={L} x2={W - R} y1={H - B} y2={H - B} style={{ stroke: 'var(--line-strong)' }} />
        {marks.map(([name, ms]) => {
          const xx = Math.min(Math.max(px(ms), L), W - R)
          return (
            <line
              key={name}
              x1={xx}
              x2={xx}
              y1={T - 2}
              y2={H - B}
              style={{ stroke: 'var(--color-warning)' }}
              strokeDasharray="2 2"
            >
              <title>
                {name} {dur(ms * 1e6)}
              </title>
            </line>
          )
        })}
        {labelGroups(marks.map(([name, ms]) => [name, Math.min(Math.max(px(ms), L), W - R)])).map(([text, xx]) => (
          <text
            key={text}
            x={xx}
            y={10}
            textAnchor={anchor(xx, 0, W)}
            style={{ ...AXIS, fontSize: 9.5, fill: 'var(--color-warning)' }}
          >
            {text}
          </text>
        ))}
        <text x={L} y={H - 4} style={AXIS}>
          {dur(data.min * 1e6)}
        </text>
        <text x={W - R} y={H - 4} textAnchor="end" style={AXIS}>
          {dur(data.max * 1e6)}
          {data.log ? ' (log scale)' : ''}
        </text>
      </svg>
    </div>
  )
}

/** LatencyCompare draws A and B: a line from min to max, a box from p50 to p90, a tick at p99. */
export function LatencyCompare({ a, b }: { a: Latency; b: Latency }) {
  const [ref, W] = useWidth<HTMLDivElement>()
  const H = 74
  const L = 24
  const R = 12
  if (!W) return <div ref={ref} style={{ height: H }} />
  // The axis starts at 0 unless both distributions sit far from it.
  const hiV = Math.max(a.max, b.max, 1)
  const loV = Math.min(a.min, b.min)
  const iw = W - L - R
  let step = niceStep(hiV, Math.max(2, Math.floor(iw / 110)))
  let lo = 0
  if (loV > hiV * 0.4) {
    step = niceStep(hiV - loV, Math.max(2, Math.floor(iw / 110)))
    lo = Math.floor((loV - step * 0.5) / step) * step
  }
  const hi = Math.ceil((hiV + step * 0.25) / step) * step
  const x = (v: number) => L + (iw * (v - lo)) / (hi - lo)
  const ticks: number[] = []
  for (let v = lo; v <= hi + step * 1e-6; v += step) ticks.push(v)
  const row = (name: string, l: Latency, y: number, color: string) => (
    <g key={name}>
      <title>
        {name}: min {dur(l.min)} · p50 {dur(l.p50)} · p90 {dur(l.p90)} · p99 {dur(l.p99)} · max {dur(l.max)}
      </title>
      <text x={4} y={y + 10} style={{ ...AXIS, fontWeight: 600, fill: color }}>
        {name}
      </text>
      <line x1={x(l.min)} x2={x(l.max)} y1={y + 6} y2={y + 6} style={{ stroke: color }} strokeWidth={1.5} />
      <line x1={x(l.min)} x2={x(l.min)} y1={y + 2} y2={y + 10} style={{ stroke: color }} />
      <line x1={x(l.max)} x2={x(l.max)} y1={y + 2} y2={y + 10} style={{ stroke: color }} />
      <rect
        x={x(l.p90) - x(l.p50) < 3 ? x(l.p50) - 1.5 : x(l.p50)}
        y={y}
        width={Math.max(x(l.p90) - x(l.p50), 3)}
        height={12}
        rx={2}
        style={{ fill: color }}
        fillOpacity={0.85}
      />
      <line
        x1={x(l.p99)}
        x2={x(l.p99)}
        y1={y - 1}
        y2={y + 13}
        style={{ stroke: color }}
        strokeWidth={2}
        strokeDasharray="2 1"
      />
    </g>
  )
  return (
    <div ref={ref}>
      <svg width={W} height={H} role="img" aria-label="Latency of A and B" className="block">
        {ticks.map((v) => (
          <g key={v}>
            <line x1={x(v)} x2={x(v)} y1={4} y2={H - 18} style={{ stroke: 'var(--line)' }} />
            <text x={x(v)} y={H - 5} textAnchor={anchor(x(v), 0, W)} style={AXIS}>
              {dur(v)}
            </text>
          </g>
        ))}
        {row('A', a, 8, 'var(--color-primary)')}
        {row('B', b, 32, 'var(--color-secondary)')}
      </svg>
    </div>
  )
}

/** ChartBox frames a chart with its title and notes. */
export function ChartBox({
  title,
  aside,
  children,
  className,
}: {
  title?: ReactNode
  aside?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <div className={cx('rounded-box border border-line bg-base-100 px-3 pt-2 pb-2', className)}>
      {(title || aside) && (
        <div className="mb-1 flex flex-wrap items-center gap-2 text-xs">
          {title && <span className="font-semibold text-fg">{title}</span>}
          {aside}
        </div>
      )}
      {children}
    </div>
  )
}
