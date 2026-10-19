// Performance: pprof profiles (sampled estimates), benchmarks (measurements)
// and the compiler's escape-analysis diagnostics, related to the function.

import { useMemo, useState } from 'react'
import { Icon } from '../../components/Icon'
import { useTheme } from '../../lib/theme'
import { cx } from '../../lib/format'
import { useInspect } from './ctx'
import { FLAME_LEGEND, Flame } from './Flame'
import { useKeys } from './keys'
import { usePrefs } from './prefs'
import type { BenchRun, Bundle, Diagnostic, FlameNode, FuncStat, ProfileReport, SymbolPerf } from './types'
import { Bar, Basis, H4, LocLink, Msg, Note, Pre, Seg, TABLE } from './ui'
import { fmtVal, num, pct } from './util'

interface BenchRow {
  pkg?: string
  name: string
  procs?: number
  unit: string
  v: number[]
  n: number
  min: number
  max: number
  median: number
}

/** benchStats groups a run's results by benchmark and unit (median, min, max, n). */
function benchStats(run: BenchRun): BenchRow[] {
  const g = new Map<string, BenchRow>()
  for (const r of run.results ?? []) {
    for (const u of r.units ?? []) {
      const k = r.pkg + '|' + r.name + '|' + r.procs + '|' + u
      let x = g.get(k)
      if (!x) {
        x = { pkg: r.pkg, name: r.name, procs: r.procs, unit: u, v: [], n: 0, min: 0, max: 0, median: 0 }
        g.set(k, x)
      }
      x.v.push(r.metrics[u])
    }
  }
  return [...g.values()].map((x) => {
    const v = x.v.slice().sort((a, b) => a - b)
    const n = v.length
    return { ...x, n, min: v[0], max: v[n - 1], median: n % 2 ? v[(n - 1) / 2] : (v[n / 2 - 1] + v[n / 2]) / 2 }
  })
}

const MAX_ROWS = 200

export default function PerfTab({ b }: { b: Bundle }) {
  const { data } = useInspect()
  const { p, set } = usePrefs()
  const pv = b.performance ?? { escapeRun: false }
  const cpu = data.cpu
  const mem = data.mem
  const kind = p.perf === 'cpu' ? (cpu ? 'cpu' : 'mem') : mem ? 'mem' : 'cpu'
  const rep = kind === 'cpu' ? cpu : mem

  useKeys((k) => {
    if (k === 'm' && cpu && mem) set({ perf: kind === 'cpu' ? 'mem' : 'cpu' })
    else return false
    return true
  })

  const bar = (
    <Bar>
      <Basis kind="estimated">profiles: sampled estimates</Basis>
      <Basis kind="observed">benchmarks: measurements</Basis>
      <Basis kind="static">compiler diagnostics</Basis>
    </Bar>
  )
  if (!cpu && !mem && !data.benches?.length && !data.escape) {
    return (
      <div className="space-y-3">
        {bar}
        <Msg>No profiling data available</Msg>
        <Note className="text-fg-3">
          Import a CPU or memory profile, a benchmark output or compiler diagnostics when exporting (gitview inspect
          report --cpu, --memory, --bench-file, --escape-log), or run them from the TUI inspector.
        </Note>
      </div>
    )
  }

  return (
    <div className="space-y-2.5">
      {bar}
      {(pv.cpu || pv.mem) && (
        <div className="flex flex-wrap gap-x-5 gap-y-1 text-xs text-fg-2">
          {pv.cpu && <SymbolSummary label={'CPU (' + pv.cpuProfile + ')'} sp={pv.cpu} />}
          {pv.mem && <SymbolSummary label={'Memory (' + pv.memProfile + ', ' + pv.memType + ')'} sp={pv.mem} />}
        </div>
      )}
      {(cpu || mem) && (
        <Bar>
          {cpu && mem && (
            <Seg
              label="Profile"
              value={kind}
              onChange={(v) => set({ perf: v })}
              options={[
                ['cpu', 'CPU', 'CPU profile (m)'],
                ['mem', 'Memory', 'Memory profile (m)'],
              ]}
            />
          )}
          <label className="flex cursor-pointer items-center gap-1.5">
            <input
              type="checkbox"
              className="checkbox checkbox-xs"
              checked={p.perfMine}
              onChange={(e) => set({ perfMine: e.target.checked })}
            />
            Module functions only
          </label>
          <label className="input input-xs w-56">
            <Icon name="filter" className="size-3 text-fg-3" />
            <input
              type="search"
              placeholder="Filter functions"
              aria-label="Filter profile functions"
              value={p.perfFilter}
              onChange={(e) => set({ perfFilter: e.target.value })}
            />
          </label>
        </Bar>
      )}
      {rep && <Profile key={kind} rep={rep} b={b} sp={kind === 'cpu' ? pv.cpu : pv.mem} />}
      {data.benches?.map((run, i) => (
        <Benchmarks key={i} run={run} b={b} />
      ))}
      <Compare />
      <Escape b={b} />
    </div>
  )
}

function SymbolSummary({ label, sp }: { label: string; sp: SymbolPerf }) {
  return (
    <span>
      {label}:{' '}
      <b className="font-semibold text-fg">
        {sp.found ? 'flat ' + pct(sp.flatPct) + ' · cum ' + pct(sp.cumPct) : 'no samples in this function'}
      </b>
    </span>
  )
}

function Profile({ rep, b, sp }: { rep: ProfileReport; b: Bundle; sp?: SymbolPerf }) {
  const { p, set } = usePrefs()
  const [zoom, setZoom] = useState<FlameNode | null>(null)
  const [theme] = useTheme()
  const pal = FLAME_LEGEND[theme]
  const a = rep.artifact
  return (
    <>
      <div className="rounded-box border border-line bg-base-100 px-3.5 py-2.5">
        <Note>
          <b className="font-semibold text-fg">
            {rep.kind} profile {a.name}
          </b>{' '}
          · {rep.sampleType} · total {fmtVal(rep.total, rep.unit)} · {rep.samples} samples
          {rep.period ? ' · ' + rep.period : ''}
          {rep.durationMs ? ' · ' + (rep.durationMs / 1000).toFixed(2) + 's' : ''} — {rep.meaning}
        </Note>
        {a.command && (
          <Note mono className="text-fg-3">
            $ {a.command}
          </Note>
        )}
        {a.stale && (
          <Note className="text-warning">
            The sources changed after this profile was captured: lines may not correspond.
          </Note>
        )}
        {rep.notes?.map((n) => (
          <Note key={n} className="text-fg-3">
            {n}
          </Note>
        ))}
        {rep.status && rep.status.status !== 'complete' && (
          <Note className="text-warning">
            {rep.status.status}
            {rep.status.reasons?.length ? ': ' + rep.status.reasons.join('; ') : ''}
          </Note>
        )}
      </div>
      <H4
        right={
          <>
            <label className="input input-xs w-48">
              <Icon name="search" className="size-3 text-fg-3" />
              <input
                type="search"
                placeholder="Highlight frames"
                aria-label="Highlight frames"
                value={p.flameFind}
                onChange={(e) => set({ flameFind: e.target.value })}
              />
            </label>
            <button type="button" className="btn btn-ghost btn-xs" disabled={!zoom} onClick={() => setZoom(null)}>
              Reset zoom
            </button>
          </>
        }
      >
        Flame graph
      </H4>
      {rep.flame ? (
        <>
          {zoom && (
            <Note className="text-fg-3">
              Zoomed into <span className="font-mono text-fg">{zoom.name}</span> — click the top frame or Reset zoom to
              go back.
            </Note>
          )}
          <Flame
            root={zoom ?? rep.flame}
            total={rep.total}
            unit={rep.unit}
            find={p.flameFind}
            self={b.symbol.id}
            onZoom={setZoom}
            zoomed={!!zoom}
          />
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-fg-3">
            {(
              [
                [pal.mine, 'module function'],
                [pal.other, 'other code'],
                [pal.rt, 'Go runtime'],
                ['var(--color-primary)', 'matches the search'],
              ] as const
            ).map(([c, l]) => (
              <span key={l} className="flex items-center gap-1.5">
                <i className="inline-block h-2.5 w-3.5 rounded-[2px]" style={{ background: c }} />
                {l}
              </span>
            ))}
            <span className="flex items-center gap-1.5">
              <i className="inline-block h-2.5 w-3.5 rounded-[2px] border-[1.5px] border-fg" />
              this function
            </span>
          </div>
        </>
      ) : (
        <Note className="text-fg-3">The profile has no call stacks to draw.</Note>
      )}
      <H4>Functions</H4>
      <FuncTable rep={rep} b={b} />
      {sp?.found && (
        <>
          <H4>This function in the profile</H4>
          <div className="space-y-0.5 rounded-box border border-line bg-base-100 px-3.5 py-2.5 text-xs">
            {sp.lines?.map((l, i) => (
              <div key={i} className="flex flex-wrap gap-x-2">
                <span className="text-fg-2">
                  line {l.line}: flat <b className="font-mono text-fg">{fmtVal(l.flat, rep.unit)}</b> · cum{' '}
                  <b className="font-mono text-fg">{fmtVal(l.cum, rep.unit)}</b>
                </span>
                <LocLink file={l.file} line={l.line} />
              </div>
            ))}
            {sp.callers?.map((e, i) => (
              <div key={'c' + i} className="text-fg-2">
                called by <span className="font-mono text-fg">{e.from}</span> — {fmtVal(e.weight, rep.unit)}
              </div>
            ))}
            {sp.callees?.map((e, i) => (
              <div key={'e' + i} className="text-fg-2">
                calls <span className="font-mono text-fg">{e.to}</span> — {fmtVal(e.weight, rep.unit)}
              </div>
            ))}
          </div>
        </>
      )}
    </>
  )
}

function FuncTable({ rep, b }: { rep: ProfileReport; b: Bundle }) {
  const { p, set } = usePrefs()
  const { indexOf, select } = useInspect()
  const rows = useMemo(() => {
    const all: FuncStat[] = (rep.funcs ?? []).slice()
    if (p.perfSort === 'cum') all.sort((x, y) => y.cum - x.cum)
    const q = p.perfFilter.trim().toLowerCase()
    return all.filter((f) => (!p.perfMine || f.symbol) && (!q || f.name.toLowerCase().includes(q)))
  }, [rep, p.perfSort, p.perfFilter, p.perfMine])
  const th = (label: string, key?: 'flat' | 'cum') =>
    key ? (
      <th
        className="cursor-pointer text-right select-none hover:text-fg"
        aria-sort={p.perfSort === key ? 'descending' : undefined}
        onClick={() => set({ perfSort: key })}
      >
        {label}
        {p.perfSort === key ? ' ▾' : ''}
      </th>
    ) : (
      <th className="text-right">{label}</th>
    )
  return (
    <div className="rounded-box border border-line bg-base-100">
      <div className="scroll-thin max-h-[26rem] overflow-auto">
        <table className={cx(TABLE, 'table-pin-rows')}>
          <thead>
            <tr>
              {th('flat', 'flat')}
              {th('flat%')}
              {th('cum', 'cum')}
              {th('cum%')}
              <th>function</th>
              <th>location</th>
            </tr>
          </thead>
          <tbody>
            {rows.slice(0, MAX_ROWS).map((f, i) => {
              const inReport = !!f.symbol && indexOf(f.symbol) >= 0
              const cur = !!f.symbol && f.symbol === b.symbol.id
              return (
                <tr
                  key={i}
                  className={cx(
                    cur && 'bg-primary/15',
                    inReport && !cur && 'cursor-pointer hover:bg-surface-hover',
                    f.symbol ? 'text-fg' : 'text-fg-3',
                  )}
                  title={inReport && !cur ? 'Inspect this function' : undefined}
                  onClick={inReport && !cur ? () => select(f.symbol!) : undefined}
                >
                  <td className="text-right font-mono whitespace-nowrap">{fmtVal(f.flat, rep.unit)}</td>
                  <td className="text-right font-mono whitespace-nowrap">{pct(f.flatPct)}</td>
                  <td className="text-right font-mono whitespace-nowrap">{fmtVal(f.cum, rep.unit)}</td>
                  <td className="text-right font-mono whitespace-nowrap">{pct(f.cumPct)}</td>
                  <td className="font-mono break-all">
                    {f.name}
                    {inReport && !cur && <Icon name="chevronRight" className="ml-1 size-3 text-fg-3" />}
                  </td>
                  <td>
                    <LocLink file={f.file} line={f.line} />
                  </td>
                </tr>
              )
            })}
            {!rows.length && (
              <tr>
                <td colSpan={6} className="py-3 text-center text-fg-3">
                  No function matches the filters.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {(rows.length > MAX_ROWS || rep.funcCount > (rep.funcs?.length ?? 0)) && (
        <div className="border-t border-line px-3 py-1.5 text-[11px] text-fg-3">
          {rows.length > MAX_ROWS ? rows.length - MAX_ROWS + ' more — refine the filter. ' : ''}
          {rep.funcCount > (rep.funcs?.length ?? 0)
            ? `The report keeps the top ${rep.funcs?.length ?? 0} of ${rep.funcCount} functions.`
            : ''}
        </div>
      )}
    </div>
  )
}

function Benchmarks({ run, b }: { run: BenchRun; b: Bundle }) {
  const stats = useMemo(() => benchStats(run), [run])
  const cfg = run.config ?? {}
  const meta = ['goos', 'goarch', 'pkg', 'cpu'].filter((k) => cfg[k]).map((k) => k + '=' + cfg[k])
  return (
    <>
      <H4>
        Benchmarks{run.artifact?.name ? <span className="font-mono normal-case"> · {run.artifact.name}</span> : null}
      </H4>
      <div className="rounded-box border border-line bg-base-100">
        {(meta.length > 0 || run.artifact?.command) && (
          <div className="border-b border-line px-3 py-1.5">
            {meta.length > 0 && <Note className="text-fg-3">{meta.join(' · ')}</Note>}
            {run.artifact?.command && (
              <Note mono className="text-fg-3">
                $ {run.artifact.command}
              </Note>
            )}
          </div>
        )}
        <div className="scroll-thin max-h-[22rem] overflow-auto">
          <table className={cx(TABLE, 'table-pin-rows')}>
            <thead>
              <tr>
                <th>benchmark</th>
                <th>unit</th>
                <th className="text-right">median</th>
                <th className="text-right">min</th>
                <th className="text-right">max</th>
                <th className="text-right">n</th>
              </tr>
            </thead>
            <tbody>
              {stats.map((x, i) => (
                <tr key={i} className={x.pkg === b.symbol.pkg ? 'text-fg' : 'text-fg-3'}>
                  <td className="font-mono">{x.name + (x.procs ? '-' + x.procs : '')}</td>
                  <td className="whitespace-nowrap">{x.unit}</td>
                  <td className="text-right font-mono">{num(x.median)}</td>
                  <td className="text-right font-mono">{num(x.min)}</td>
                  <td className="text-right font-mono">{num(x.max)}</td>
                  <td className="text-right font-mono">{x.n}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {!!run.failures?.length && (
          <div className="border-t border-line px-3 py-1.5">
            {run.failures.map((f) => (
              <Note key={f} className="text-error">
                {f}
              </Note>
            ))}
          </div>
        )}
      </div>
    </>
  )
}

function Compare() {
  const { data } = useInspect()
  const c = data.compare
  if (!c) return null
  return (
    <>
      <H4>Before / after comparison</H4>
      <div className="space-y-2">
        <Note>{c.method}</Note>
        {c.configDiffs?.map((d) => (
          <Msg key={d} warn>
            Conditions differ: {d}
          </Msg>
        ))}
        <div className="scroll-thin max-h-[22rem] overflow-auto rounded-box border border-line bg-base-100">
          <table className={cx(TABLE, 'table-pin-rows')}>
            <thead>
              <tr>
                <th>benchmark</th>
                <th>unit</th>
                <th className="text-right">old median</th>
                <th className="text-right">new median</th>
                <th className="text-right">Δ</th>
                <th className="text-right">p</th>
                <th>note</th>
              </tr>
            </thead>
            <tbody>
              {(c.rows ?? []).map((r, i) => (
                <tr key={i}>
                  <td className="font-mono">{r.name}</td>
                  <td className="whitespace-nowrap">{r.unit}</td>
                  <td className="text-right font-mono">{r.old ? num(r.old.median) : '-'}</td>
                  <td className="text-right font-mono">{r.new ? num(r.new.median) : '-'}</td>
                  <td
                    className={cx(
                      'text-right font-mono whitespace-nowrap',
                      r.significant && (r.deltaPct > 0 ? 'text-error' : r.deltaPct < 0 ? 'text-success' : ''),
                    )}
                  >
                    {r.old && r.new
                      ? r.deltaUndefined
                        ? 'n/a'
                        : (r.deltaPct >= 0 ? '+' : '') + r.deltaPct.toFixed(2) + '%'
                      : ''}
                  </td>
                  <td className="text-right font-mono">{r.p >= 0 ? r.p.toFixed(3) : ''}</td>
                  <td className={cx(r.significant ? 'text-fg' : 'text-fg-3')}>
                    {r.significant && <span className="badge badge-soft badge-warning badge-xs mr-1">significant</span>}
                    {r.note || ''}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {c.benchstat && <Pre>{c.benchstat}</Pre>}
      </div>
    </>
  )
}

function diagTone(kind: string): string {
  if (kind.includes('does not escape')) return 'badge-success'
  if (kind.includes('escape') || kind.includes('heap') || kind.includes('leak')) return 'badge-warning'
  if (kind.includes('cannot')) return 'badge-neutral'
  return 'badge-info'
}

function Escape({ b }: { b: Bundle }) {
  const { data } = useInspect()
  const e = data.escape
  if (!e) return null
  const list: Diagnostic[] = b.performance?.escape ?? []
  return (
    <>
      <H4>
        Compiler diagnostics <span className="font-mono normal-case">(go build -gcflags=-m=2)</span>
      </H4>
      <Note>Verbatim compiler output for {(e.packages ?? []).join(', ')}; no interpretation added.</Note>
      {e.artifact?.command && (
        <Note mono className="text-fg-3">
          $ {e.artifact.command}
        </Note>
      )}
      {e.errors?.map((x) => (
        <Note key={x} className="text-error">
          {x}
        </Note>
      ))}
      {!list.length ? (
        <Note className="text-fg-3">No diagnostics located in this function.</Note>
      ) : (
        <div className="mt-1.5 rounded-box border border-line bg-base-100">
          <table className={TABLE}>
            <thead>
              <tr>
                <th>kind</th>
                <th>message</th>
                <th>location</th>
              </tr>
            </thead>
            <tbody>
              {list.map((d, i) => (
                <tr key={i}>
                  <td className="whitespace-nowrap">
                    <span className={cx('badge badge-xs badge-soft', diagTone(d.kind))}>{d.kind}</span>
                  </td>
                  <td className="font-mono break-words">
                    {d.message}
                    {!!d.flow?.length && (
                      <details className="mt-1">
                        <summary className="cursor-pointer font-sans text-[11px] text-fg-3 hover:text-fg">
                          compiler explanation
                        </summary>
                        <Pre className="mt-1" maxHeight="14rem">
                          {d.flow.join('\n')}
                        </Pre>
                      </details>
                    )}
                  </td>
                  <td className="whitespace-nowrap">
                    <LocLink file={d.file} line={d.line} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}
