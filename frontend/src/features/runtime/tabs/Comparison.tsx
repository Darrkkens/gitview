import { Icon } from '../../../components/Icon'
import { cx } from '../../../lib/format'
import { LatencyCompare } from '../charts'
import { change, dur, metric } from '../fmt'
import type { Analysis, Comparison as Cmp, Condition, Delta, FuncDelta, Latency } from '../types'
import { BasisBadge, Card, H2, Muted, Swatch, Table, Warn } from '../ui'
import { FuncName } from './common'

function verdictClass(v: string): string {
  if (v === 'slower') return 'badge-error'
  if (v === 'faster') return 'badge-success'
  if (v === 'inconclusive') return 'badge-warning'
  return 'badge-ghost'
}

function DeltaTable({ rows }: { rows: Delta[] }) {
  return (
    <Table<Delta>
      rows={rows}
      columns={[
        { head: 'metric', className: 'min-w-44', cell: (x) => <span title={x.note}>{x.name}</span> },
        { head: 'A', num: true, cell: (x) => metric(x.a, x.unit) },
        { head: 'B', num: true, cell: (x) => metric(x.b, x.unit) },
        {
          head: 'change',
          num: true,
          cell: (x) => (
            <span
              className={cx(
                x.change !== null && x.change > 0
                  ? 'text-warning'
                  : x.change !== null && x.change < 0
                    ? 'text-success'
                    : 'text-fg-3',
              )}
            >
              {change(x.change)}
            </span>
          ),
        },
        { head: 'basis', cell: (x) => <BasisBadge basis={x.basis} /> },
        { head: 'scope', cell: (x) => <span className="text-fg-2">{x.scope}</span> },
        { head: 'note', className: 'min-w-48', cell: (x) => <span className="text-fg-3">{x.note ?? ''}</span> },
      ]}
    />
  )
}

export function Comparison({ c, a, base }: { c: Cmp; a: Analysis; base?: Analysis | null }) {
  const conds = c.conditions ?? []
  const blocking = conds.filter((x) => !x.ok && x.blocking).length
  return (
    <>
      <Card className="flex flex-wrap items-start gap-x-8 gap-y-2">
        <div className="min-w-0">
          <div className="flex items-center gap-1.5 text-xs text-fg-3">
            <Swatch color="var(--color-primary)" label="A, the reference" />
          </div>
          <div className="font-mono text-sm font-semibold">{c.a}</div>
          {base?.label && <div className="text-xs text-fg-2">{base.label}</div>}
        </div>
        <Icon name="chevronRight" className="mt-4 size-4 text-fg-3" />
        <div className="min-w-0">
          <div className="flex items-center gap-1.5 text-xs text-fg-3">
            <Swatch color="var(--color-secondary)" label="B, the new capture" />
          </div>
          <div className="font-mono text-sm font-semibold">{c.b}</div>
          {a.label && <div className="text-xs text-fg-2">{a.label}</div>}
        </div>
        <div className="ml-auto self-center">
          {c.comparable ? (
            <span className="badge badge-success badge-soft">comparable conditions</span>
          ) : (
            <span
              className="badge badge-warning badge-soft"
              title="Verdicts are inconclusive while a blocking condition fails"
            >
              not comparable
              {blocking ? ': ' + blocking + ' blocking condition' + (blocking > 1 ? 's' : '') + ' failed' : ''}
            </span>
          )}
        </div>
        <Muted className="basis-full">Only routes present in both captures are compared.</Muted>
      </Card>

      <H2>Conditions</H2>
      <Table<Condition>
        rows={conds}
        columns={[
          {
            head: '',
            cell: (x) =>
              x.ok ? (
                <Icon name="check" className="size-4 text-success" />
              ) : (
                <Icon name="x" className={cx('size-4', x.blocking ? 'text-error' : 'text-warning')} />
              ),
          },
          {
            head: 'condition',
            cell: (x) => (
              <span className="whitespace-nowrap">
                {x.name}
                {x.blocking && (
                  <span
                    className="ml-1.5 badge badge-ghost badge-xs"
                    title="When it fails, every verdict is inconclusive"
                  >
                    blocking
                  </span>
                )}
              </span>
            ),
          },
          { head: 'detail', className: 'min-w-64', cell: (x) => <span className="text-fg-2">{x.detail}</span> },
        ]}
      />
      {(c.notes ?? []).map((n, i) => (
        <Warn key={i} className="mt-2">
          {n}
        </Warn>
      ))}

      <H2>Routes</H2>
      {!(c.routes ?? []).length && <Muted>No route is present in both captures.</Muted>}
      <div className="space-y-3">
        {(c.routes ?? []).map((r) => (
          <Card key={r.key}>
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="font-mono text-sm font-semibold">{r.key}</h3>
              <span className={cx('badge badge-soft badge-sm', verdictClass(r.verdict))}>{r.verdict}</span>
              <span className="ml-auto text-xs text-fg-2">
                median change{' '}
                <b
                  className={cx(
                    'font-mono',
                    r.change !== null && r.change > 0
                      ? 'text-warning'
                      : r.change !== null && r.change < 0
                        ? 'text-success'
                        : '',
                  )}
                >
                  {change(r.change)}
                </b>{' '}
                · Mann–Whitney p ={' '}
                <span className="font-mono">{r.p === null || r.p === undefined ? 'n/a' : r.p.toPrecision(3)}</span>
              </span>
            </div>
            <div className="mt-2 grid items-start gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)]">
              <Table<[string, number, Latency]>
                rows={[
                  ['A', r.na, r.a],
                  ['B', r.nb, r.b],
                ]}
                columns={[
                  {
                    head: '',
                    cell: ([k]) => <b className={k === 'A' ? 'text-primary' : 'text-secondary'}>{k}</b>,
                  },
                  { head: 'n', num: true, cell: ([, n]) => n },
                  { head: 'p50', num: true, cell: ([, , l]) => dur(l.p50) },
                  { head: 'p90', num: true, cell: ([, , l]) => dur(l.p90) },
                  { head: 'p99', num: true, cell: ([, , l]) => dur(l.p99) },
                  { head: 'max', num: true, cell: ([, , l]) => dur(l.max) },
                  { head: 'mean', num: true, cell: ([, , l]) => dur(l.mean) },
                ]}
              />
              <div className="rounded-box border border-line px-2 pt-1">
                <LatencyCompare a={r.a} b={r.b} />
                <Muted className="px-1 pb-1">bar: p50–p90 · line: min–max · dashed tick: p99</Muted>
              </div>
            </div>
            {(r.reasons ?? []).map((x, i) => (
              <p key={i} className="mt-1 text-xs text-warning">
                · {x}
              </p>
            ))}
            {!!r.deltas?.length && (
              <div className="mt-2">
                <DeltaTable rows={r.deltas} />
              </div>
            )}
            {(r.sqlAdded ?? []).map((q, i) => (
              <p key={'a' + i} className="mt-1 flex items-start gap-2 text-xs">
                <span className="shrink-0 font-semibold text-warning">+ new SQL</span>
                <code className="font-mono break-words whitespace-pre-wrap">{q}</code>
              </p>
            ))}
            {(r.sqlGone ?? []).map((q, i) => (
              <p key={'g' + i} className="mt-1 flex items-start gap-2 text-xs">
                <span className="shrink-0 text-fg-3">− removed SQL</span>
                <code className="font-mono break-words whitespace-pre-wrap text-fg-2">{q}</code>
              </p>
            ))}
          </Card>
        ))}
      </div>
      {!!c.onlyA?.length && (
        <Muted className="mt-2">
          only in A: <span className="font-mono">{c.onlyA.join(', ')}</span>
        </Muted>
      )}
      {!!c.onlyB?.length && (
        <Muted className="mt-1">
          only in B: <span className="font-mono">{c.onlyB.join(', ')}</span>
        </Muted>
      )}

      {!!c.process?.length && (
        <>
          <H2>Process</H2>
          <DeltaTable rows={c.process} />
        </>
      )}
      {!!c.functions?.length && (
        <>
          <H2 aside={<BasisBadge basis="sampled" />}>Module functions whose share of CPU changed most</H2>
          <Table<FuncDelta>
            rows={c.functions}
            columns={[
              { head: 'function', cell: (f) => <FuncName name={f.name} symbol={f.symbol} /> },
              { head: 'A', num: true, cell: (f) => f.a.toFixed(1) + ' %' },
              { head: 'B', num: true, cell: (f) => f.b.toFixed(1) + ' %' },
              {
                head: 'change',
                num: true,
                cell: (f) => (
                  <span className={f.b > f.a ? 'text-warning' : 'text-success'}>
                    {(f.b - f.a > 0 ? '+' : '') + (f.b - f.a).toFixed(1)} pt
                  </span>
                ),
              },
            ]}
          />
        </>
      )}
      <H2>Possible regressions</H2>
      {c.regressions?.length ? (
        <ul className="space-y-1">
          {c.regressions.map((x, i) => (
            <li key={i} className="alert alert-error alert-soft px-3 py-1.5 text-xs">
              {x}
            </li>
          ))}
        </ul>
      ) : (
        <Muted>No statistically significant regression.</Muted>
      )}
    </>
  )
}
