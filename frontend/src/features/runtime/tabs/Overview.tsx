import { useMemo } from 'react'
import { ChartBox, PhaseLegend, TimeChart, type Mark } from '../charts'
import { bytes, dur } from '../fmt'
import type { Analysis, Correlation, PhaseStats } from '../types'
import { BasisBadge, Card, H2, KV, Muted, Swatch, Table, Warn } from '../ui'
import { Metrics } from './common'

const dash = '–'

export function Overview({ a }: { a: Analysis }) {
  const P = a.processInfo ?? ({} as Analysis['processInfo'])
  const ph = a.phases
  const S = a.settings ?? ({} as Analysis['settings'])
  const series = a.process?.series
  const cpu = useMemo(() => (series ?? []).map((p) => [p.t, p.cpu] as [number, number]), [series])
  const rss = useMemo(() => (series ?? []).map((p) => [p.t, p.rss] as [number, number]), [series])
  const vsz = useMemo(() => (series ?? []).filter((p) => p.vsz).map((p) => [p.t, p.vsz!] as [number, number]), [series])
  const threads = useMemo(() => (series ?? []).map((p) => [p.t, p.threads] as [number, number]), [series])
  const fds = useMemo(() => (series ?? []).map((p) => [p.t, p.fds] as [number, number]), [series])
  // (each slice is drawn at its middle)
  const perfLine = a.perf?.timeline
  const half = (a.perf?.bucketSecs ?? 0) / 2
  const perfCPU = useMemo(
    () => (perfLine ?? []).map((b) => [b.t + half, b.cpu] as [number, number]),
    [perfLine, half],
  )
  const marks: Mark[] = useMemo(
    () =>
      (a.requests ?? []).map((r) => ({
        t: r.off / 1e9,
        dt: r.dur / 1e9,
        color: r.status >= 500 ? 'var(--color-error)' : 'var(--color-secondary)',
        title: r.method + ' ' + r.path + ' ' + r.status + ' ' + dur(r.dur),
      })),
    [a.requests],
  )
  const rt = a.runtime?.series
  const heap = useMemo(() => (rt ?? []).map((p) => [p.t, p.heapLive] as [number, number]), [rt])
  const gor = useMemo(() => (rt ?? []).map((p) => [p.t, p.goroutines] as [number, number]), [rt])
  const paused = !!ph.paused?.length

  return (
    <>
      {(a.warnings ?? []).map((w, i) => (
        <Warn key={i} className="mb-2">
          ⚠ {w}
        </Warn>
      ))}
      {a.statusNote && <Warn className="mb-2">{a.statusNote}</Warn>}
      <Card>
        <KV
          rows={[
            [
              'capture',
              <span key="c" className="font-mono">
                {a.id + (a.label ? ' · ' + a.label : '')}
              </span>,
            ],
            [
              'process',
              <span key="p">
                PID {P.pid} · <span className="font-mono">{P.mainModule || P.comm}</span>
                {P.vcsRevision && (
                  <>
                    {' @ '}
                    <span className="font-mono">{P.vcsRevision.slice(0, 12)}</span>
                    {P.vcsModified && <span className="text-warning"> (modified)</span>}
                  </>
                )}
              </span>,
            ],
            [
              'phases',
              'baseline 0–' +
                (ph.activeStart / 1e9).toFixed(1) +
                ' s · active ' +
                (ph.activeStart / 1e9).toFixed(1) +
                '–' +
                (ph.activeEnd / 1e9).toFixed(1) +
                ' s · end ' +
                (ph.end / 1e9).toFixed(1) +
                ' s' +
                (paused ? ' · ' + ph.paused!.length + ' pause' + (ph.paused!.length > 1 ? 's' : '') : ''),
            ],
            ['sampling', 'every ' + dur(S.interval) + ' · CPU resolution: ' + (a.process?.cpuResolution || '')],
            [
              'proxy',
              S.proxy ? (
                <span key="x" className="font-mono">
                  {S.proxy + ' → ' + S.target}
                </span>
              ) : (
                ''
              ),
            ],
            [
              'collector',
              S.collector ? (
                <span key="l" className="font-mono">
                  {S.collector}
                </span>
              ) : (
                ''
              ),
            ],
            [
              'OpenTelemetry',
              S.otlp ? (
                <span key="o" className="font-mono">
                  {S.otlp}
                </span>
              ) : (
                ''
              ),
            ],
            [
              'repository',
              a.repo ? (
                <span key="r" className="font-mono">
                  {a.repo}
                </span>
              ) : (
                ''
              ),
            ],
          ]}
        />
      </Card>

      <H2 aside={<BasisBadge basis="measured" />}>Process CPU (% of one core)</H2>
      <Muted className="mb-1.5">Whole process, from /proc. Bars along the top are the requests (red: 5xx).</Muted>
      <ChartBox>
        <TimeChart
          points={cpu}
          phases={ph}
          fmt={(v) => v.toFixed(0) + '%'}
          color="var(--color-primary)"
          marks={marks}
          label="Process CPU over time"
          unitLabel="of one core"
        />
        <PhaseLegend
          paused={paused}
          extra={
            marks.length > 0 && (
              <>
                <Swatch color="var(--color-secondary)" label="request" />
                <Swatch color="var(--color-error)" label="5xx" />
              </>
            )
          }
        />
      </ChartBox>

      {a.perf && perfCPU.length > 1 && (
        <>
          <H2 aside={<BasisBadge basis="sampled" />}>CPU sampled by Linux perf (% of one core)</H2>
          <Muted className="mb-1.5">
            Whole process, {a.perf.event} at {a.perf.freq} Hz per thread: samples × period in slices of{' '}
            {a.perf.bucketSecs} s. A statistical estimate of the curve above
            {a.perf.userOnly ? ', user space only (time in the kernel is missing)' : ''}; it cannot be split by
            request.
          </Muted>
          <ChartBox>
            <TimeChart
              points={perfCPU}
              phases={ph}
              fmt={(v) => v.toFixed(0) + '%'}
              color="var(--color-accent)"
              marks={marks}
              label="CPU sampled by perf over time"
              unitLabel="of one core"
            />
            <PhaseLegend paused={paused} />
          </ChartBox>
        </>
      )}

      {!!a.cpuViews?.length && (
        <>
          <H2>CPU: three different measures</H2>
          <Muted className="mb-1.5">
            The process total is measured; CPU by function is sampled; CPU of a request exists only when the collector
            labels the samples. They are not interchangeable.
          </Muted>
          <Metrics rows={a.cpuViews} />
        </>
      )}

      {a.correlation && <CorrelationCard c={a.correlation} />}

      <H2 aside={<BasisBadge basis="measured" />}>Resident memory (RSS)</H2>
      <ChartBox>
        <TimeChart
          points={rss}
          phases={ph}
          fmt={bytes}
          color="var(--color-secondary)"
          label="Resident memory over time"
        />
        <PhaseLegend paused={paused} />
      </ChartBox>

      <div className="mt-2 grid gap-2 lg:grid-cols-3">
        {vsz.length > 1 && (
          <ChartBox title="virtual memory" aside={<Muted>VmSize</Muted>}>
            <TimeChart
              points={vsz}
              phases={ph}
              fmt={bytes}
              color="var(--fg-2)"
              height={120}
              label="Virtual memory over time"
            />
          </ChartBox>
        )}
        <ChartBox title="threads">
          <TimeChart
            points={threads}
            phases={ph}
            fmt={(v) => v.toFixed(0)}
            color="var(--color-primary)"
            height={120}
            label="Threads over time"
          />
        </ChartBox>
        <ChartBox title="open file descriptors">
          <TimeChart
            points={fds}
            phases={ph}
            fmt={(v) => v.toFixed(0)}
            color="var(--color-secondary)"
            height={120}
            label="Open file descriptors over time"
          />
        </ChartBox>
      </div>

      <H2>Phases</H2>
      <Table<PhaseStats>
        rows={a.process?.phases ?? []}
        rowKey={(p) => p.name}
        columns={[
          { head: 'phase', cell: (p) => <span className="font-medium">{p.name}</span> },
          { head: 'samples', num: true, cell: (p) => p.samples },
          { head: 'CPU avg', num: true, cell: (p) => (p.hasCPU ? p.cpuAvg.toFixed(1) + ' %' : dash) },
          { head: 'CPU max', num: true, cell: (p) => (p.hasCPU ? p.cpuMax.toFixed(1) + ' %' : dash) },
          { head: 'CPU time', num: true, cell: (p) => (p.hasCPU ? dur(p.cpuTime) : dash) },
          { head: 'RSS max', num: true, cell: (p) => (p.hasMem ? bytes(p.rssMax) : dash) },
          { head: 'Δ RSS', num: true, cell: (p) => (p.hasMem ? bytes(p.rssEnd - p.rssStart) : dash) },
          { head: 'threads', num: true, cell: (p) => p.threads },
          {
            head: 'ctx vol/invol',
            num: true,
            cell: (p) => (p.hasCtx ? p.volCtx + ' / ' + p.involCtx : dash),
            title: 'voluntary / involuntary context switches',
          },
          {
            head: 'disk read/write',
            num: true,
            cell: (p) => (p.hasIO ? bytes(p.read) + ' / ' + bytes(p.write) : dash),
          },
          {
            head: 'read()/write()',
            num: true,
            cell: (p) => (p.hasIO ? bytes(p.rchar) + ' / ' + bytes(p.wchar) : dash),
            title: 'bytes through read() and write(), sockets included',
          },
        ]}
      />
      {Object.keys(a.process?.unavailable ?? {}).length > 0 && (
        <ul className="mt-2 space-y-0.5 text-xs text-fg-3">
          {Object.entries(a.process.unavailable ?? {}).map(([k, v]) => (
            <li key={k} className="flex items-start gap-1.5">
              <BasisBadge basis="unavailable" className="mt-0.5" />
              <span>
                <span className="font-medium text-fg-2">{k}</span>: {v}
              </span>
            </li>
          ))}
        </ul>
      )}

      {(rt?.length ?? 0) > 1 && (
        <>
          <H2 aside={<BasisBadge basis="measured" />}>Go runtime (collector)</H2>
          <div className="grid gap-2 lg:grid-cols-2">
            <ChartBox title="live heap" aside={<Muted>runtime/metrics</Muted>}>
              <TimeChart
                points={heap}
                phases={ph}
                fmt={bytes}
                color="var(--color-success)"
                height={140}
                label="Live heap over time"
              />
            </ChartBox>
            <ChartBox title="goroutines">
              <TimeChart
                points={gor}
                phases={ph}
                fmt={(v) => v.toFixed(0)}
                color="var(--color-primary)"
                height={140}
                label="Goroutines over time"
              />
            </ChartBox>
          </div>
        </>
      )}
    </>
  )
}

function CorrelationCard({ c }: { c: Correlation }) {
  const perf = (v: number) => (c.hasPerf ? v.toFixed(1) + ' %' : dash)
  const rows = [
    { name: 'with requests in flight', secs: c.busySecs, cpu: c.busyCPU, perf: c.busyPerf, rss: c.busyRSS },
    { name: 'without requests', secs: c.idleSecs, cpu: c.idleCPU, perf: c.idlePerf, rss: c.idleRSS },
  ]
  return (
    <>
      <H2>Process with and without requests</H2>
      <Table<(typeof rows)[number]>
        rows={rows}
        rowKey={(r) => r.name}
        columns={[
          { head: 'active phase', cell: (r) => <span className="font-medium">{r.name}</span> },
          { head: 'time', num: true, cell: (r) => r.secs.toFixed(2) + ' s' },
          {
            head: (
              <span className="inline-flex items-center gap-1">
                CPU /proc <BasisBadge basis="measured" />
              </span>
            ),
            num: true,
            cell: (r) => r.cpu.toFixed(1) + ' %',
          },
          {
            head: (
              <span className="inline-flex items-center gap-1">
                CPU perf <BasisBadge basis="sampled" />
              </span>
            ),
            num: true,
            cell: (r) => perf(r.perf),
          },
          { head: 'Δ RSS', num: true, cell: (r) => bytes(r.rss) },
        ]}
      />
      <Muted className="mt-1.5">{c.note}</Muted>
    </>
  )
}
