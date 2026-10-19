// Execution Trace: events recorded by runtime/trace (goroutine states, GC,
// tasks, regions, logs) on a zoomable timeline, and what they show about
// the function. The trace does not record every call: per-function
// durations exist only for regions.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Icon } from '../../components/Icon'
import { cx } from '../../lib/format'
import { useTheme } from '../../lib/theme'
import { token } from '../../lib/tokens'
import { useInspect } from './ctx'
import { useKeys } from './keys'
import { usePrefs } from './prefs'
import type { Bundle, Goroutine, Region, Segment, Span, Timeline } from './types'
import { Bar, Basis, Dl, H4, LocLink, Msg, Note, Panel, Row, TABLE } from './ui'
import { clip, fmtNs, monoFont } from './util'

const LBL = 190
const RH = 20
const TOP = 22
const MAX_H = 620

type Sel = { gc: Span } | { g: Goroutine; seg?: Segment; region?: Region } | null

export default function TraceTab({ b }: { b: Bundle }) {
  const { data } = useInspect()
  const T = data.trace
  const bar = (
    <Bar>
      <Basis kind="observed">events recorded by runtime/trace</Basis>
      <span>The trace does not record every function call: per-function durations exist only for regions.</span>
    </Bar>
  )
  if (!T) {
    return (
      <div className="space-y-3">
        {bar}
        <Msg>No runtime trace available</Msg>
        <Note className="text-fg-3">
          Capture one from the TUI inspector (t, or T to measure the function with a temporary region) or import a trace
          when exporting (gitview inspect report --trace trace.out).
        </Note>
      </div>
    )
  }
  return <Trace b={b} T={T} bar={bar} />
}

function Trace({ b, T, bar }: { b: Bundle; T: Timeline; bar: React.ReactNode }) {
  const { p, set } = usePrefs()
  const tv = b.trace ?? { loaded: false }
  const sy = tv.symbol
  const st = T.stats
  const dur = Math.max(T.durationNs || 1, 1)
  const view = useMemo<[number, number]>(() => p.trView ?? [0, dur], [p.trView, dur])
  const setView = useCallback((v: [number, number] | null) => set({ trView: v }), [set])
  const [sel, setSel] = useState<Sel>(null)
  const [mark, setMark] = useState<{ g: number; s: number; e: number } | null>(null)

  const tasks = useMemo(() => T.tasks ?? [], [T])
  const rows = useMemo(() => {
    const q = p.trFilter.trim().toLowerCase()
    const task = p.trTask ? tasks.find((t) => String(t.id) === p.trTask) : undefined
    return (T.goroutines ?? []).filter((g) => {
      if (!p.trAll && g.system) return false
      if (q && !('g' + g.id + ' ' + g.id + ' ' + g.name).toLowerCase().includes(q)) return false
      if (task && !(task.goroutines ?? []).includes(g.id)) return false
      return true
    })
  }, [T, tasks, p.trAll, p.trFilter, p.trTask])

  const zoomAt = useCallback(
    (f: number, t?: number) => {
      const [a, z] = view
      const c = t ?? (a + z) / 2
      const na = Math.max(0, c - (c - a) * f)
      const nz = Math.min(dur, c + (z - c) * f)
      if (nz - na > 1000) setView([na, nz])
    },
    [view, dur, setView],
  )
  const pan = useCallback(
    (frac: number) => {
      const [a, z] = view
      const w = z - a
      let na = a + w * frac
      na = Math.max(0, Math.min(dur - w, na))
      setView([na, na + w])
    },
    [view, dur, setView],
  )

  useKeys((k) => {
    if (k === 'a') set({ trAll: !p.trAll })
    else if (k === '0') setView(null)
    else if (k === '+' || k === '=') zoomAt(0.6)
    else if (k === '-') zoomAt(1 / 0.6)
    else if (k === 'ArrowLeft') pan(-0.2)
    else if (k === 'ArrowRight') pan(0.2)
    else return false
    return true
  })

  const blocks = useMemo(() => {
    const sys = new Map((T.goroutines ?? []).map((g) => [g.id, !!g.system]))
    return (T.events ?? [])
      .filter((e) => e.kind === 'block' && (e.dur ?? 0) > 0 && !sys.get(e.g))
      .sort((x, y) => (y.dur ?? 0) - (x.dur ?? 0))
      .slice(0, 15)
  }, [T])

  return (
    <div className="@container space-y-2.5">
      {bar}
      <div className="text-xs text-fg-2">
        <b className="font-semibold text-fg">{T.artifact?.name || 'trace'}</b> · duration {fmtNs(T.durationNs)} ·{' '}
        {st.goroutines} goroutines ({st.userGoroutines} non-runtime) · {st.procs} Ps · {st.gcCycles} GC cycles ·{' '}
        {tasks.length} tasks · {(T.regions ?? []).length} regions · {(T.logs ?? []).length} logs
      </div>
      {T.artifact?.command && (
        <Note mono className="text-fg-3">
          $ {T.artifact.command}
        </Note>
      )}
      {T.artifact?.stale && (
        <Note className="text-warning">
          The sources changed after this trace was captured: lines may not correspond.
        </Note>
      )}
      {sy && (
        <div
          className={cx(
            'rounded-box border px-3 py-1.5 text-xs',
            sy.found ? 'border-primary/40 bg-primary/5' : 'border-line bg-base-100 text-fg-2',
          )}
        >
          {!sy.found ? (
            tv.message || 'No event attributed to this function.'
          ) : (
            <>
              <b className="font-semibold">This function:</b> {(sy.goroutines ?? []).length} goroutine(s) started here ·{' '}
              {sy.spanCount} measured span(s)
              {sy.spanCount
                ? ' (total ' +
                  fmtNs(sy.spanTotalNs) +
                  ', min ' +
                  fmtNs(sy.spanMinNs) +
                  ', max ' +
                  fmtNs(sy.spanMaxNs) +
                  ')'
                : ''}{' '}
              · {(sy.blocks ?? []).length} blocking event(s) with it on the stack
            </>
          )}
        </div>
      )}
      <Bar>
        <label
          className="flex cursor-pointer items-center gap-1.5"
          title="Include goroutines started by the runtime (a)"
        >
          <input
            type="checkbox"
            className="checkbox checkbox-xs"
            checked={p.trAll}
            onChange={(e) => set({ trAll: e.target.checked })}
          />
          Runtime goroutines
        </label>
        <label className="input input-xs w-52">
          <Icon name="filter" className="size-3 text-fg-3" />
          <input
            type="search"
            placeholder="Goroutine id or function"
            aria-label="Filter goroutines"
            value={p.trFilter}
            onChange={(e) => set({ trFilter: e.target.value })}
          />
        </label>
        <select
          aria-label="Task"
          className="select select-xs w-auto"
          value={p.trTask}
          onChange={(e) => {
            const id = e.target.value
            const t = tasks.find((x) => String(x.id) === id)
            set({ trTask: id, trView: t ? [Math.max(t.s - 1, 0), (t.e >= 0 ? t.e : dur) + 1] : p.trView })
          }}
        >
          <option value="">All tasks</option>
          {tasks.map((t) => (
            <option key={t.id} value={String(t.id)}>
              {t.type} #{t.id}
            </option>
          ))}
        </select>
        <RangeInputs view={view} dur={dur} onChange={setView} />
        <button type="button" className="btn btn-ghost btn-xs" onClick={() => setView(null)} title="Whole trace (0)">
          Whole trace
        </button>
      </Bar>
      <Legend />
      <div className="grid gap-3 @4xl:grid-cols-[minmax(0,1fr)_minmax(280px,360px)]">
        <TimelineCanvas
          T={T}
          rows={rows}
          view={view}
          dur={dur}
          mark={mark}
          self={b.symbol.id}
          selfGs={sy?.goroutines}
          onView={setView}
          onPick={(x) => {
            setSel(x)
            if (x && 'g' in x && (x.seg || x.region)) {
              const q = (x.seg ?? x.region)!
              setMark({ g: x.g.id, s: q.s, e: q.e >= 0 ? q.e : view[1] })
            } else setMark(null)
          }}
        />
        <Panel label="Event details" className="@4xl:max-h-[620px]">
          <Detail T={T} sel={sel} />
        </Panel>
      </div>
      {blocks.length > 0 && (
        <>
          <H4>Longest observed waits</H4>
          <div className="rounded-box border border-line bg-base-100">
            <table className={TABLE}>
              <thead>
                <tr>
                  <th className="text-right">wait</th>
                  <th>goroutine · reason</th>
                  <th>where</th>
                </tr>
              </thead>
              <tbody>
                {blocks.map((e, i) => {
                  const stk = T.stacks?.[e.k] ?? []
                  const fr = stk.find((f) => f.symbol) ?? stk[0] ?? { func: '' }
                  const d = e.dur ?? 0
                  return (
                    <tr
                      key={i}
                      className="cursor-pointer hover:bg-surface-hover"
                      title="Show on the timeline"
                      onClick={() => {
                        setView([Math.max(e.t - d * 0.2, 0), Math.min(e.t + d * 1.2, dur)])
                        setMark({ g: e.g, s: e.t, e: e.t + d })
                        const g = (T.goroutines ?? []).find((x) => x.id === e.g)
                        if (g)
                          setSel({
                            g,
                            seg: (g.segments ?? []).find(
                              (s) => s.s <= e.t + 1 && s.e >= e.t + d - 1 && s.st === 'waiting',
                            ),
                          })
                      }}
                    >
                      <td className="text-right font-mono whitespace-nowrap">{fmtNs(d)}</td>
                      <td>
                        <span className="font-mono">G{e.g}</span> {e.detail}
                      </td>
                      <td className="break-all">
                        <span className="font-mono text-fg-2">{fr.func}</span> <LocLink file={fr.file} line={fr.line} />
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </>
      )}
      {(T.logs ?? []).length > 0 && (
        <>
          <H4>Logs</H4>
          <div className="space-y-0.5 rounded-box border border-line bg-base-100 px-3 py-2">
            {(T.logs ?? []).slice(0, 30).map((l, i) => (
              <Note key={i} mono>
                <span className="text-fg-3">{fmtNs(l.t)}</span> G{l.g}{' '}
                <span className="text-accent-text">[{l.category}]</span> {l.message}
              </Note>
            ))}
          </div>
        </>
      )}
      <div className="space-y-1">
        {T.notes?.map((n) => (
          <Note key={n}>{n}</Note>
        ))}
        {T.status && T.status.status !== 'complete' && (
          <Note className="text-warning">
            {T.status.status}
            {T.status.reasons?.length ? ': ' + T.status.reasons.join('; ') : ''}
          </Note>
        )}
      </div>
    </div>
  )
}

function RangeInputs({
  view,
  dur,
  onChange,
}: {
  view: [number, number]
  dur: number
  onChange: (v: [number, number] | null) => void
}) {
  const fmt = (ns: number) => (ns / 1e6).toFixed(3)
  const [from, setFrom] = useState(fmt(view[0]))
  const [to, setTo] = useState(fmt(view[1]))
  const [shown, setShown] = useState(view)
  if (shown[0] !== view[0] || shown[1] !== view[1]) {
    setShown(view)
    setFrom(fmt(view[0]))
    setTo(fmt(view[1]))
  }
  const commit = () => {
    const a = parseFloat(from)
    const z = parseFloat(to)
    if (!isNaN(a) && !isNaN(z) && z > a) onChange([Math.max(0, a * 1e6), Math.min(dur, z * 1e6)])
    else {
      setFrom(fmt(view[0]))
      setTo(fmt(view[1]))
    }
  }
  const input = (v: string, set: (s: string) => void, label: string) => (
    <input
      type="number"
      step="0.001"
      min="0"
      aria-label={label}
      className="input input-xs w-24 font-mono"
      value={v}
      onChange={(e) => set(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') commit()
      }}
    />
  )
  return (
    <span className="flex items-center gap-1.5">
      From (ms) {input(from, setFrom, 'From (ms)')} to {input(to, setTo, 'To (ms)')}
    </span>
  )
}

const LEGEND: [string, string][] = [
  ['running', 'color-success'],
  ['runnable', 'color-warning'],
  ['waiting', 'fg-3'],
  ['syscall', 'color-primary'],
  ['GC', 'color-error'],
  ['region / span', 'color-accent'],
]

function Legend() {
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-fg-3">
      {LEGEND.map(([l, v]) => (
        <span key={l} className="flex items-center gap-1.5">
          <i className="inline-block h-2.5 w-3.5 rounded-[2px]" style={{ background: `var(--${v})` }} />
          {l}
        </span>
      ))}
      <span className="flex items-center gap-1.5">
        <i className="inline-block h-1 w-3.5 rounded-[1px] bg-primary" />
        region measuring this function
      </span>
      <span className="text-fg-3">· wheel to zoom, drag to pan, click a block for details</span>
    </div>
  )
}

function TimelineCanvas({
  T,
  rows,
  view,
  dur,
  mark,
  self,
  selfGs,
  onView,
  onPick,
}: {
  T: Timeline
  rows: Goroutine[]
  view: [number, number]
  dur: number
  mark: { g: number; s: number; e: number } | null
  self: string
  selfGs?: number[]
  onView: (v: [number, number]) => void
  onPick: (s: Sel) => void
}) {
  const [theme] = useTheme()
  const wrap = useRef<HTMLDivElement>(null)
  const cv = useRef<HTMLCanvasElement>(null)
  const [W, setW] = useState(900)
  useEffect(() => {
    const el = wrap.current
    if (!el) return
    const ro = new ResizeObserver(() => setW(Math.max(el.clientWidth, 320)))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const regionsBy = useMemo(() => {
    const m = new Map<number, Region[]>()
    for (const r of T.regions ?? []) {
      const l = m.get(r.g)
      if (l) l.push(r)
      else m.set(r.g, [r])
    }
    return m
  }, [T])
  const mineGs = useMemo(() => new Set(selfGs ?? []), [selfGs])
  const H = Math.max(TOP + (rows.length + 1) * RH + 10, 200)

  useEffect(() => {
    const c = cv.current?.getContext('2d')
    if (!c || !cv.current) return
    const dpr = window.devicePixelRatio || 1
    cv.current.width = W * dpr
    cv.current.height = H * dpr
    c.setTransform(dpr, 0, 0, dpr, 0, 0)
    const [a, z] = view
    const sc = (W - LBL - 8) / Math.max(z - a, 1)
    const col: Record<string, string> = {
      running: token('color-success'),
      runnable: token('color-warning'),
      waiting: token('fg-3'),
      syscall: token('color-primary'),
      text: token('fg'),
      muted: token('fg-2'),
      faint: token('fg-3'),
      gc: token('color-error'),
      region: token('color-accent'),
      line: token('line'),
      sel: token('color-primary'),
      hl: token('accent-weak'),
    }
    const mono = monoFont()
    c.clearRect(0, 0, W, H)
    c.font = '11px ' + mono
    // time axis: grid lines, and the labels that fit (first and last always)
    const ticks = [0, 1, 2, 3, 4, 5].map((i) => {
      const t = a + ((z - a) * i) / 5
      return { x: LBL + (t - a) * sc, label: fmtNs(t), w: c.measureText(fmtNs(t)).width }
    })
    c.fillStyle = col.line
    for (const tk of ticks) c.fillRect(tk.x, TOP - 4, 1, H)
    c.fillStyle = col.muted
    const lastX = ticks[5].x - 2 - ticks[5].w
    let end = -Infinity
    ticks.forEach((tk, i) => {
      if (i === 5) {
        c.fillText(tk.label, lastX, 12)
        return
      }
      const x = tk.x + 2
      if (i > 0 && (x < end + 8 || x + tk.w > lastX - 8)) return
      c.fillText(tk.label, x, 12)
      end = x + tk.w
    })
    rows.forEach((g, ri) => {
      const y = TOP + ri * RH
      if (mineGs.has(g.id)) {
        c.fillStyle = col.hl
        c.fillRect(0, y, W, RH)
      }
      c.fillStyle = mineGs.has(g.id) ? col.sel : g.symbol ? col.text : col.faint
      c.fillText(clip('G' + g.id + ' ' + g.name.slice(g.name.lastIndexOf('/') + 1), 26), 4, y + 13)
      for (const s of g.segments ?? []) {
        if (s.e < a || s.s > z) continue
        const x1 = LBL + (Math.max(s.s, a) - a) * sc
        const x2 = LBL + (Math.min(s.e, z) - a) * sc
        c.fillStyle = col[s.st] || col.muted
        c.fillRect(x1, y + (s.st === 'waiting' ? 6 : 3), Math.max(x2 - x1, 1), s.st === 'waiting' ? 6 : 12)
      }
      // Regions measuring the inspected function last, over enclosing ones.
      for (const pass of [false, true]) {
        for (const r of regionsBy.get(g.id) ?? []) {
          const mine = !!r.symbol && r.symbol === self
          if (mine !== pass) continue
          const e = r.e >= 0 ? r.e : z
          if (e < a || r.s > z) continue
          const x1 = LBL + (Math.max(r.s, a) - a) * sc
          const x2 = LBL + (Math.min(e, z) - a) * sc
          c.fillStyle = mine ? col.sel : col.region
          c.fillRect(x1, y + 16, Math.max(x2 - x1, mine ? 2 : 1), 3)
        }
      }
      if (mark && mark.g === g.id) {
        c.strokeStyle = col.sel
        c.lineWidth = 2
        const sx1 = LBL + (Math.max(mark.s, a) - a) * sc
        const sx2 = LBL + (Math.min(mark.e, z) - a) * sc
        c.strokeRect(sx1 - 1, y + 1, Math.max(sx2 - sx1, 2) + 2, 17)
      }
    })
    const gy = TOP + rows.length * RH
    c.fillStyle = col.muted
    c.fillText('GC / STW', 4, gy + 13)
    for (const sp of T.gc ?? []) {
      if (sp.e < a || sp.s > z) continue
      const x1 = LBL + (Math.max(sp.s, a) - a) * sc
      const x2 = LBL + (Math.min(sp.e, z) - a) * sc
      c.fillStyle = col.gc
      c.globalAlpha = sp.name.startsWith('stop-the-world') ? 1 : 0.55
      c.fillRect(x1, gy + 4, Math.max(x2 - x1, 1), 10)
      c.globalAlpha = 1
    }
    if (!rows.length) {
      c.fillStyle = col.muted
      c.fillText('No goroutine matches the filters.', LBL, TOP + 14)
    }
  }, [W, H, view, rows, T, regionsBy, mark, theme, self, mineGs])

  const hit = (ev: { clientX: number; clientY: number }): Sel => {
    const r = cv.current!.getBoundingClientRect()
    const x = ev.clientX - r.left
    const y = ev.clientY - r.top
    const ri = Math.floor((y - TOP) / RH)
    const sc = (W - LBL - 8) / Math.max(view[1] - view[0], 1)
    const t = view[0] + (x - LBL) / sc
    if (x < LBL) return null
    if (ri === rows.length) {
      const gc = (T.gc ?? []).find((sp) => t >= sp.s && t <= sp.e)
      return gc ? { gc } : null
    }
    const g = rows[ri]
    if (!g) return null
    if (y - TOP - ri * RH >= 15) {
      const rg = (regionsBy.get(g.id) ?? []).filter((q) => t >= q.s && t <= (q.e >= 0 ? q.e : view[1])).pop()
      if (rg) return { g, region: rg }
    }
    const seg = (g.segments ?? []).find((q) => t >= q.s && t <= q.e)
    return seg ? { g, seg } : { g }
  }

  // Wheel zoom around the pointer (non-passive listener).
  const viewRef = useRef(view)
  useEffect(() => {
    viewRef.current = view
  })
  useEffect(() => {
    const el = cv.current
    if (!el) return
    const onWheel = (ev: WheelEvent) => {
      const r = el.getBoundingClientRect()
      const x = ev.clientX - r.left
      if (x < LBL) return
      ev.preventDefault()
      const [a, z] = viewRef.current
      const sc = (el.clientWidth - LBL - 8) / Math.max(z - a, 1)
      const t = a + (x - LBL) / sc
      const f = Math.exp(ev.deltaY * 0.0015)
      const na = Math.max(0, t - (t - a) * f)
      const nz = Math.min(dur, t + (z - t) * f)
      if (nz - na > 1000) onView([na, nz])
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [dur, onView])

  const drag = useRef<{ x: number; v: [number, number]; moved: boolean } | null>(null)
  return (
    <div
      ref={wrap}
      className="scroll-thin relative overflow-x-hidden overflow-y-auto rounded-box border border-line bg-canvas"
      style={{ maxHeight: MAX_H, height: Math.min(H, MAX_H) + 2 }}
    >
      <canvas
        ref={cv}
        role="img"
        aria-label="Execution timeline: wheel to zoom, drag to pan, click a block for details"
        className="block cursor-crosshair"
        style={{ width: W, height: H, touchAction: 'none' }}
        onPointerDown={(e) => {
          drag.current = { x: e.clientX, v: [view[0], view[1]], moved: false }
          e.currentTarget.setPointerCapture(e.pointerId)
        }}
        onPointerMove={(e) => {
          const d = drag.current
          if (!d) return
          const dx = e.clientX - d.x
          if (!d.moved && Math.abs(dx) < 3) return
          d.moved = true
          const sc = (W - LBL - 8) / Math.max(d.v[1] - d.v[0], 1)
          const dt = -dx / sc
          const w = d.v[1] - d.v[0]
          const na = Math.max(0, Math.min(dur - w, d.v[0] + dt))
          onView([na, na + w])
        }}
        onPointerUp={(e) => {
          const d = drag.current
          drag.current = null
          if (d && !d.moved) onPick(hit(e))
        }}
        onPointerCancel={() => {
          drag.current = null
        }}
      />
    </div>
  )
}

function Stack({ T, k }: { T: Timeline; k: number }) {
  const st = k >= 0 ? T.stacks?.[k] : undefined
  if (!st?.length) return <Note className="text-fg-3">No stack recorded for this event.</Note>
  return (
    <ol className="scroll-thin max-h-72 space-y-1 overflow-auto rounded-box border border-line bg-code px-2.5 py-2 text-[11px]">
      {st.map((f, i) => (
        <li key={i} className="leading-4">
          <div className={cx('font-mono break-all', f.symbol ? 'text-fg' : 'text-fg-2')}>
            {f.func}
            {f.symbol && <span className="badge badge-ghost badge-xs ml-1.5 align-middle">module</span>}
          </div>
          <div className="pl-3">
            <LocLink file={f.file} line={f.line} />
          </div>
        </li>
      ))}
    </ol>
  )
}

function Detail({ T, sel }: { T: Timeline; sel: Sel }) {
  if (!sel) return <Note className="text-fg-3">Click a block of the timeline to see the event.</Note>
  if ('gc' in sel) {
    return (
      <div>
        <h3 className="font-mono text-[13px] font-semibold">{sel.gc.name}</h3>
        <Dl>
          <Row label="Start">{fmtNs(sel.gc.s)}</Row>
          <Row label="Duration">{fmtNs(sel.gc.e - sel.gc.s)}</Row>
        </Dl>
      </div>
    )
  }
  const g = sel.g
  return (
    <div>
      <div className="text-xs text-fg-3">
        goroutine G{g.id}
        {g.system ? ' · runtime' : ''}
      </div>
      <h3 className="mt-0.5 font-mono text-[13px] font-semibold break-all">{g.name}</h3>
      <Dl>
        <Row label="Created">
          {g.created >= 0 ? fmtNs(g.created) + (g.creator ? ' by G' + g.creator : '') : 'before the trace started'}
        </Row>
        <Row label="Ended">{g.ended >= 0 ? fmtNs(g.ended) : 'alive at the end'}</Row>
        <Row label="Running">{fmtNs(g.runningNs)}</Row>
        <Row label="Runnable">{fmtNs(g.runnableNs)}</Row>
        <Row label="Waiting">{fmtNs(g.waitingNs)}</Row>
        <Row label="Syscall">{fmtNs(g.syscallNs)}</Row>
      </Dl>
      {sel.region && (
        <>
          <H4>Region</H4>
          <Dl>
            <Row label="Type">{sel.region.type}</Row>
            <Row label="Measures">
              {sel.region.instrumented
                ? 'function ' + sel.region.symbol + ' (GitView instrumentation; includes its overhead)'
                : ''}
            </Row>
            <Row label="Task">{sel.region.task ? String(sel.region.task) : ''}</Row>
            <Row label="Start">{fmtNs(sel.region.s)}</Row>
            <Row label="Duration">{sel.region.e >= 0 ? fmtNs(sel.region.e - sel.region.s) : 'did not end'}</Row>
          </Dl>
          <Stack T={T} k={sel.region.k} />
        </>
      )}
      {sel.seg && (
        <>
          <H4>State</H4>
          <Dl>
            <Row label="State">{sel.seg.st}</Row>
            <Row label="Reason">{sel.seg.r}</Row>
            <Row label="Start">{fmtNs(sel.seg.s)}</Row>
            <Row label="Duration">{fmtNs(sel.seg.e - sel.seg.s)}</Row>
            <Row label="P">{sel.seg.st === 'running' && sel.seg.p !== undefined ? String(sel.seg.p) : ''}</Row>
          </Dl>
          {sel.seg.k >= 0 && (
            <>
              <H4>Stack</H4>
              <Stack T={T} k={sel.seg.k} />
            </>
          )}
        </>
      )}
      {g.createStack >= 0 && (
        <>
          <H4>Created at</H4>
          <Stack T={T} k={g.createStack} />
        </>
      )}
    </div>
  )
}
