import { useMemo, useState } from 'react'
import { Icon } from '../../../components/Icon'
import { cx, loc } from '../../../lib/format'
import { setParams } from '../../../lib/router'
import { useStudy } from '../context'
import { fmtNs } from '../fmt'
import { track } from '../progress'
import type { Candidate, ObservedCall, PredictEntry } from '../types'
import { BasisBadge, Card, Empty, FunctionActions, LayerBadge, LocLink, Note, SectionTitle } from '../ui'
import { ExerciseCard } from './Exercises'

/**
 * inOrder returns the longest subsequence of ids whose reference ranks never
 * decrease (ties in the reference are interchangeable): the items of the
 * prediction that are in the right order.
 */
function inOrder(ids: string[], rank: Map<string, number>): Set<string> {
  const n = ids.length
  const len: number[] = []
  const prev: number[] = []
  let best = -1
  for (let i = 0; i < n; i++) {
    len[i] = 1
    prev[i] = -1
    for (let j = 0; j < i; j++) {
      if (rank.get(ids[j])! <= rank.get(ids[i])! && len[j] + 1 > len[i]) {
        len[i] = len[j] + 1
        prev[i] = j
      }
    }
    if (best < 0 || len[i] > len[best]) best = i
  }
  const ok = new Set<string>()
  for (let k = best; k >= 0; k = prev[k]) ok.add(ids[k])
  return ok
}

interface Result {
  trace: boolean
  ref: string[]
  rank: Map<string, number>
  info: Map<string, ObservedCall>
  common: string[]
  unexpected: string[]
  missed: string[]
  ok: Set<string>
  nOk: number
}

function compare(en: PredictEntry, seq: string[], trace: boolean, byId: Map<string, Candidate>): Result {
  const ref: string[] = []
  const rank = new Map<string, number>()
  const info = new Map<string, ObservedCall>()
  if (trace) {
    for (const c of en.observed ?? []) {
      if (byId.has(c.id) && !rank.has(c.id)) {
        ref.push(c.id)
        rank.set(c.id, c.seq)
        info.set(c.id, c)
      }
    }
  } else {
    for (const id of en.staticOrder ?? []) {
      if (byId.has(id) && !rank.has(id)) {
        rank.set(id, ref.length)
        ref.push(id)
      }
    }
  }
  const common = seq.filter((id) => rank.has(id))
  const unexpected = seq.filter((id) => !rank.has(id))
  const missed = ref.filter((id) => !seq.includes(id))
  const ok = inOrder(common, rank)
  const nOk = common.filter((id) => ok.has(id)).length
  return { trace, ref, rank, info, common, unexpected, missed, ok, nOk }
}

function Stat({ label, value, tone }: { label: string; value: number; tone?: string }) {
  return (
    <div className="rounded-field border border-line bg-surface-2 px-2.5 py-1">
      <div className="text-[10px] tracking-wide text-fg-3 uppercase">{label}</div>
      <div className={cx('font-mono text-base font-semibold', tone)}>{value}</div>
    </div>
  )
}

export function Predict({ entryParam }: { entryParam?: string | null }) {
  const { model } = useStudy()
  const PR = model.predict ?? {}
  const entries = PR.entries ?? []
  const OB = PR.observed ?? null
  const traceChallenges = useMemo(() => (model.exercises ?? []).filter((e) => e.type === 'trace'), [model.exercises])
  const idx = Math.min(Math.max(Number(entryParam) || 0, 0), Math.max(entries.length - 1, 0))

  return (
    <>
      <Note className="mb-3">
        Choose an entry point and build the order in which you expect its functions to run. The candidates are the
        module functions the call graph says it may reach <BasisBadge basis="STATIC" /> (up to 5 calls deep). Then
        compare your order with the execution trace <BasisBadge basis="RUNTIME" /> or with the order of the calls in the
        code <BasisBadge basis="STATIC" /> — a possible order, not an observed one. The comparison only matches function
        IDs and positions.
      </Note>
      {!entries.length ? (
        <Empty>No entry point of the change reaches other functions of the module.</Empty>
      ) : (
        <>
          {OB ? (
            <Note className="mb-3">
              <BasisBadge basis="RUNTIME" className="mr-1.5" />
              Trace {OB.source}: {OB.functions} module functions observed. {OB.note}
            </Note>
          ) : (
            <Note className="mb-3">
              No execution trace in this report, so only the static call order can be compared. To add one:{' '}
              <code className="font-mono">gitview study --trace trace.out</code> (from{' '}
              <code className="font-mono">go test -trace</code>, or captured with{' '}
              <code className="font-mono">gitview inspect trace</code>).
            </Note>
          )}
          <label className="mb-3 flex flex-wrap items-center gap-2 text-xs text-fg-2">
            Entry point:
            <select
              className="select select-sm w-auto max-w-full min-w-64 pr-8 font-mono"
              value={idx}
              onChange={(e) => setParams({ entry: e.target.value === '0' ? undefined : e.target.value })}
            >
              {entries.map((e, i) => (
                <option key={e.id} value={i}>
                  {e.title}
                </option>
              ))}
            </select>
          </label>
          <EntryGame key={entries[idx].id} en={entries[idx]} hasTrace={!!OB} source={OB?.source ?? ''} />
        </>
      )}
      {traceChallenges.length > 0 && (
        <>
          <SectionTitle className="mt-8 flex items-center gap-2">
            Trace Challenge <BasisBadge basis="RUNTIME" />
          </SectionTitle>
          <div className="grid gap-2 xl:grid-cols-2">
            {traceChallenges.map((e) => (
              <ExerciseCard key={e.id} e={e} area="challenges" />
            ))}
          </div>
        </>
      )}
    </>
  )
}

function EntryGame({ en, hasTrace, source }: { en: PredictEntry; hasTrace: boolean; source: string }) {
  const [seq, setSeq] = useState<string[]>([])
  const [mode, setMode] = useState<'trace' | 'static' | null>(null)
  const byId = useMemo(() => new Map(en.candidates.map((c) => [c.id, c])), [en])
  const result = useMemo(() => (mode ? compare(en, seq, mode === 'trace', byId) : null), [mode, en, seq, byId])

  const update = (next: string[]) => {
    setSeq(next)
    setMode(null)
  }
  const move = (i: number, d: number) => {
    const next = seq.slice()
    ;[next[i], next[i + d]] = [next[i + d], next[i]]
    update(next)
  }
  const run = (m: 'trace' | 'static') => {
    if (!seq.length) return
    track('codeflow', 'predict:' + en.id)
    setMode(m)
  }

  return (
    <>
      <Card>
        <div className="flex flex-wrap items-center gap-2">
          <LayerBadge layer={en.layer} />
          <span className="font-mono text-[13px] font-semibold break-all">{en.title}</span>
          <LocLink file={en.file} line={en.line} className="ml-auto" />
        </div>
        <FunctionActions id={en.id} file={en.file} line={en.line} area="codeflow" />
      </Card>
      <div className="mt-3 grid gap-3 lg:grid-cols-2">
        <div>
          <h4 className="mb-1.5 flex items-center gap-2 text-xs font-semibold text-fg-2">
            Candidates <BasisBadge basis="STATIC" />
            <span className="font-normal text-fg-3">click to add</span>
          </h4>
          <ul className="space-y-1">
            {en.candidates.map((c) => {
              const used = seq.includes(c.id)
              return (
                <li key={c.id}>
                  <button
                    type="button"
                    disabled={used}
                    onClick={() => update([...seq, c.id])}
                    title={used ? 'Already in your prediction' : 'Add to the prediction'}
                    className={cx(
                      'flex w-full items-center gap-2 rounded-field border border-line bg-base-100 px-2 py-1 text-left text-xs',
                      used ? 'opacity-40' : 'cursor-pointer hover:border-primary/60 hover:bg-surface-hover',
                    )}
                  >
                    <Icon name={used ? 'check' : 'plus'} className="size-3.5 text-fg-3" />
                    <span className="min-w-0 flex-1 font-mono break-all">{c.name}</span>
                    <LayerBadge layer={c.layer} />
                    <span className="text-[11px] whitespace-nowrap text-fg-3">depth {c.depth}</span>
                  </button>
                </li>
              )
            })}
          </ul>
        </div>
        <div>
          <h4 className="mb-1.5 text-xs font-semibold text-fg-2">
            Your prediction <span className="font-normal text-fg-3">(after {en.title})</span>
          </h4>
          <ol className="space-y-1">
            {!seq.length && (
              <li className="rounded-field border border-dashed border-line px-2 py-3 text-center text-xs text-fg-3">
                Add candidates in the order you expect them to run.
              </li>
            )}
            {seq.map((id, i) => (
              <li
                key={id}
                className="flex items-center gap-1.5 rounded-field border border-line bg-base-100 px-2 py-0.5 text-xs"
              >
                <span className="w-5 font-mono font-semibold text-fg-3">{i + 1}</span>
                <span className="min-w-0 flex-1 font-mono break-all">{byId.get(id)?.name ?? id}</span>
                <button
                  type="button"
                  className="btn btn-ghost btn-xs btn-square"
                  title="Move up"
                  aria-label="Move up"
                  disabled={i === 0}
                  onClick={() => move(i, -1)}
                >
                  <Icon name="chevronDown" className="size-3.5 rotate-180" />
                </button>
                <button
                  type="button"
                  className="btn btn-ghost btn-xs btn-square"
                  title="Move down"
                  aria-label="Move down"
                  disabled={i === seq.length - 1}
                  onClick={() => move(i, 1)}
                >
                  <Icon name="chevronDown" className="size-3.5" />
                </button>
                <button
                  type="button"
                  className="btn btn-ghost btn-xs btn-square"
                  title="Remove"
                  aria-label="Remove"
                  onClick={() => update(seq.filter((x) => x !== id))}
                >
                  <Icon name="x" className="size-3.5" />
                </button>
              </li>
            ))}
          </ol>
        </div>
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-1.5">
        <button
          type="button"
          className={cx('btn btn-sm', mode === 'trace' ? 'btn-primary' : 'btn-soft btn-primary')}
          disabled={!hasTrace || !seq.length}
          title={!hasTrace ? 'No execution trace in this report' : !seq.length ? 'Add candidates first' : undefined}
          onClick={() => run('trace')}
        >
          Compare With Trace
        </button>
        <button
          type="button"
          className={cx('btn btn-sm', mode === 'static' ? 'btn-primary' : 'btn-soft btn-primary')}
          disabled={!seq.length}
          title={!seq.length ? 'Add candidates first' : undefined}
          onClick={() => run('static')}
        >
          Compare With Static Order
        </button>
        <button type="button" className="btn btn-ghost btn-sm" disabled={!seq.length} onClick={() => update([])}>
          Clear
        </button>
      </div>
      {result && <Comparison r={result} seq={seq} byId={byId} source={source} />}
    </>
  )
}

function Comparison({
  r,
  seq,
  byId,
  source,
}: {
  r: Result
  seq: string[]
  byId: Map<string, Candidate>
  source: string
}) {
  const [showRef, setShowRef] = useState(false)
  const name = (id: string) => byId.get(id)?.name ?? id
  const pos = (id: string) => r.ref.indexOf(id) + 1
  const sameInstant = (id: string) => r.ref.some((o) => o !== id && r.rank.get(o) === r.rank.get(id))
  const seen = (id: string) => {
    const c = r.info.get(id)
    return c ? fmtNs(c.firstNs) + ' (' + c.how + ')' : ''
  }
  return (
    <Card className="mt-3">
      <div className="flex flex-wrap items-center gap-2">
        <BasisBadge basis={r.trace ? 'RUNTIME' : 'STATIC'} />
        <b className="text-sm">
          {r.trace ? 'Compared with the trace ' + source : 'Compared with the static call order'}
        </b>
      </div>
      {r.trace && !r.ref.length ? (
        <Note className="mt-2">
          None of these candidates appears in the trace under this entry point. That does not mean they did not run: the
          trace has no stack for them.
        </Note>
      ) : (
        <>
          <div className="mt-2 flex flex-wrap gap-2">
            <Stat label="Correct" value={r.common.length} tone="text-success" />
            <Stat label="Missed" value={r.missed.length} tone={r.missed.length ? 'text-warning' : undefined} />
            <Stat
              label="Unexpected"
              value={r.unexpected.length}
              tone={r.unexpected.length ? 'text-error' : undefined}
            />
            <Stat label="Correct order" value={r.nOk} tone="text-success" />
            <Stat
              label="Incorrect order"
              value={r.common.length - r.nOk}
              tone={r.common.length - r.nOk ? 'text-warning' : undefined}
            />
          </div>
          <div className="mt-2 overflow-x-auto">
            <table className="table table-xs [&_tr]:text-xs">
              <thead>
                <tr>
                  <th>#</th>
                  <th>Your prediction</th>
                  <th>Result</th>
                  <th>{r.trace ? 'First seen in the trace' : 'Position in the code order'}</th>
                </tr>
              </thead>
              <tbody>
                {seq.map((id, i) => (
                  <tr key={id} className="align-top">
                    <td className="font-mono">{i + 1}</td>
                    <td className="font-mono break-all">{name(id)}</td>
                    <td>
                      {!r.rank.has(id) ? (
                        <>
                          <span className="font-semibold text-error">Unexpected</span>
                          <span className="text-fg-2">
                            {r.trace
                              ? ' — not observed in the trace under this entry (absence is not proof it did not run)'
                              : ' — not in the static call order'}
                          </span>
                        </>
                      ) : (
                        <>
                          <span className="font-semibold text-success">Correct</span>
                          <span className="text-fg-3"> · </span>
                          {r.ok.has(id) ? (
                            <span className="font-semibold text-success">correct order</span>
                          ) : (
                            <span className="font-semibold text-warning">incorrect order</span>
                          )}
                        </>
                      )}
                    </td>
                    <td className="font-mono text-fg-3">
                      {r.rank.has(id) ? '#' + pos(id) + (r.trace ? ' at ' + seen(id) : '') : ''}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {r.missed.length > 0 && (
            <>
              <Note className="mt-2">
                {r.trace
                  ? 'Observed under this entry point but not in your prediction:'
                  : 'In the static order but not in your prediction:'}
              </Note>
              <ul className="mt-1 space-y-0.5 text-xs">
                {r.missed.map((id) => (
                  <li key={id}>
                    <span className="font-semibold text-warning">Missed </span>
                    <span className="font-mono">{name(id)}</span>
                    <span className="text-fg-3">
                      {r.trace ? ' — observed at ' + seen(id) : ' — position #' + pos(id) + ' in the code order'}
                    </span>
                  </li>
                ))}
              </ul>
            </>
          )}
          <div className="mt-2">
            <button
              type="button"
              className={cx('btn btn-xs', showRef ? 'btn-primary btn-soft' : 'btn-ghost')}
              aria-expanded={showRef}
              onClick={() => setShowRef((v) => !v)}
            >
              <Icon name={showRef ? 'chevronDown' : 'chevronRight'} className="size-3.5" />
              {r.trace ? 'Show the observed order' : 'Show the static order'}
            </button>
          </div>
          {showRef && (
            <ol className="mt-1 list-decimal space-y-0.5 pl-6 text-xs">
              {r.ref.map((id) => {
                const c = byId.get(id)
                return (
                  <li key={id}>
                    <span className="font-mono">{name(id)}</span>
                    <span className="text-fg-3">
                      {r.trace
                        ? '  ' +
                          fmtNs(r.info.get(id)?.firstNs) +
                          ' · ' +
                          (r.info.get(id)?.how ?? '') +
                          (sameInstant(id) ? ' · same instant as another (order unknown)' : '')
                        : '  depth ' + (c?.depth ?? '') + ' · ' + loc({ file: c?.file, line: c?.line })}
                    </span>
                  </li>
                )
              })}
            </ol>
          )}
          <Note className="mt-2">
            {r.trace
              ? 'Order = first appearance in the trace (time, then outer frames before inner ones); a function that runs several times counts once.'
              : 'The static order lists calls depth first in the order they appear in the code. Branches, loops and early returns decide the real order, so this is a possible order, not an observed one.'}
          </Note>
        </>
      )}
    </Card>
  )
}
