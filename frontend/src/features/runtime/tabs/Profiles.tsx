import type { ReactNode } from 'react'
import { Flame } from '../Flame'
import { bytes, dur, profileValue } from '../fmt'
import type { Analysis, ProfileReport } from '../types'
import { BasisBadge, Card, H2, H3, KV, Muted, Swatch } from '../ui'
import { HotPath, Metrics, TopFuncs } from './common'

function FlameLegend() {
  return (
    <span className="flex gap-2 font-normal">
      <Swatch color="color-mix(in oklab, var(--k-unresolved) 50%, var(--color-base-100))" label="module code" />
      <Swatch color="color-mix(in oklab, var(--fg-3) 30%, var(--color-base-100))" label="other code" />
    </span>
  )
}

function Profile({
  title,
  rep,
  n,
  children,
  flame,
}: {
  title: ReactNode
  rep: ProfileReport
  n: number
  children?: ReactNode
  flame?: boolean
}) {
  return (
    <section className="mt-6 first:mt-0">
      <H2 className="mt-0" aside={<BasisBadge basis="sampled" />}>
        {title}
      </H2>
      <Card>
        {children}
        {rep.meaning && <Muted>{rep.meaning}</Muted>}
        {flame && rep.flame && rep.flame.value > 0 && (
          <>
            <H3 className="mt-2">
              Flame graph <FlameLegend />
            </H3>
            <Flame root={rep.flame} fmt={(v) => profileValue(rep.unit, v)} />
          </>
        )}
        <TopFuncs rep={rep} n={n} />
        {(rep.notes ?? []).map((x, i) => (
          <Muted key={i} className="mt-1.5">
            {x}
          </Muted>
        ))}
      </Card>
    </section>
  )
}

function PerfCPU({ a }: { a: Analysis }) {
  const P = a.perf!
  const goCPU = !!a.profiles?.cpu
  if (!P.cpu) return <Muted className="mt-2">Linux perf recorded no CPU sample in the active phase.</Muted>
  return (
    <Profile title={'CPU sampled by Linux perf (' + dur(P.sampledCPU) + ')'} rep={P.cpu} n={15} flame>
      <Muted className="mb-1">
        {P.event} at {P.freq} Hz per thread · {P.samples} samples in the active phase ({P.recorded} recorded) · whole
        process, from outside: no endpoint, no instrumentation
        {P.stats.foreign ? ' · ' + P.stats.foreign + ' samples in C or system code' : ''}
      </Muted>
      {!goCPU && !!a.hotPath?.length && (
        <div className="mb-2">
          <H3 className="mt-1">Critical path of the process</H3>
          <HotPath path={a.hotPath} />
        </div>
      )}
    </Profile>
  )
}

export function Profiles({ a }: { a: Analysis }) {
  const PR = a.profiles
  return (
    <>
      {a.perf && <PerfCPU a={a} />}
      {(a.perf?.notes ?? []).map((x, i) => (
        <Muted key={i} className="mt-1.5">
          {x}
        </Muted>
      ))}
      {!PR ? (
        <Muted className={a.perf ? 'mt-4' : ''}>
          {a.perf
            ? 'No Go profile (net/http/pprof or the collector): Go heap, goroutines, contention and the execution trace are not available; Linux perf samples CPU only.'
            : 'No profiling data available.'}
        </Muted>
      ) : (
        <>
          {PR.cpu ? (
            <Profile title={a.perf ? 'CPU (Go profile)' : 'CPU'} rep={PR.cpu} n={15} flame>
              <Muted className="mb-1">
                Total {dur(PR.cpu.total)} in {PR.cpu.samples} samples · {(100 * (PR.cpuCoverage || 0)).toFixed(0)}% of
                the active phase covered
                {PR.cpuLabelled
                  ? ' · attributed to requests ' + dur(PR.cpuLabelled) + ', other work ' + dur(PR.cpuUnlabelled || 0)
                  : ''}
                {PR.cpu.period ? ' · ' + PR.cpu.period : ''}
              </Muted>
              {!!a.hotPath?.length && (
                <div className="mb-2">
                  <H3 className="mt-1">Critical path of the process</H3>
                  <HotPath path={a.hotPath} />
                </div>
              )}
            </Profile>
          ) : (
            <Muted>No CPU profile was collected.</Muted>
          )}
          {PR.allocs && (
            <Profile
              title={
                <>
                  Allocations during the capture ({bytes(PR.allocs.total)}
                  {PR.objects ? ', ' + PR.objects.toLocaleString('en-US') + ' objects' : ''})
                </>
              }
              rep={PR.allocs}
              n={10}
              flame
            />
          )}
          {PR.inuse && (
            <Profile title={'Heap in use at the end (' + bytes(PR.inuse.total) + ')'} rep={PR.inuse} n={10} />
          )}
          {PR.mutex && <Profile title={'Mutex contention (' + dur(PR.mutex.total) + ')'} rep={PR.mutex} n={10} />}
          {PR.block && <Profile title={'Blocking (' + dur(PR.block.total) + ')'} rep={PR.block} n={10} />}
          {PR.goroutines && (
            <Profile
              title={'Goroutines at the end of the active phase (' + PR.goroutines.total + ')'}
              rep={PR.goroutines}
              n={10}
            />
          )}
          {(PR.notes ?? []).map((x, i) => (
            <Muted key={i} className="mt-2">
              {x}
            </Muted>
          ))}
        </>
      )}
      {!!a.runtime?.metrics?.length && (
        <>
          <H2>Go runtime metrics (collector)</H2>
          <Metrics rows={a.runtime.metrics} />
        </>
      )}
      {a.trace && (
        <>
          <H2>Execution trace</H2>
          <Card>
            <KV
              rows={[
                ['duration', dur(a.trace.durationNs)],
                ['requests found in the trace', String((a.trace.requests ?? []).length)],
                ['GC cycles', a.trace.stats ? String(a.trace.stats.gcCycles ?? '') : ''],
                ['stop-the-world', a.trace.stats ? dur(a.trace.stats.stwNs ?? 0) : ''],
                ['goroutines', a.trace.stats?.goroutines ? String(a.trace.stats.goroutines) : ''],
                ['unmatched tasks', a.trace.unmatched ? String(a.trace.unmatched) : ''],
              ]}
            />
            {(a.trace.notes ?? []).map((x, i) => (
              <Muted key={i} className="mt-1.5">
                {x}
              </Muted>
            ))}
          </Card>
        </>
      )}
    </>
  )
}
