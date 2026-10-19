// SQL Explorer: the database operations of the function and of the module
// functions it may call (pgx, pgxpool, database/sql, sqlx), found
// statically, with the SQL, the $N parameters and their origin, Scan
// targets, transactions and, when one was requested, the PostgreSQL plan.

import { useMemo, type ReactNode } from 'react'
import { CodeInline } from '../../components/Code'
import { Icon } from '../../components/Icon'
import { cx } from '../../lib/format'
import { routeHref } from '../../lib/router'
import { useInspect } from './ctx'
import type { Bundle, Plan, PlanNode, SqlOp } from './types'
import { Bar, Basis, H4, LocLink, Msg, Note, Pre, TABLE } from './ui'

const SQL_KW = new Set(
  (
    'select from where and or not in is null like ilike between exists join left right inner outer full cross on using group by order ' +
    'having limit offset insert into values update set delete returning as distinct union all except intersect with recursive case when ' +
    'then else end asc desc nulls first last conflict do nothing for share nowait skip locked lateral create table index drop alter ' +
    'begin commit rollback merge matched true false count sum avg min max coalesce'
  ).split(' '),
)

/** SqlCode highlights SQL as text spans; ⟨…⟩ marks parts computed at runtime. */
function SqlCode({ text }: { text: string }) {
  const parts = useMemo(() => {
    const out: [string, string][] = []
    const re =
      /(--[^\n]*)|('(?:[^']|'')*'?)|(⟨[^⟩]*⟩?)|(\$\d+|\?)|(\b\d+(?:\.\d+)?\b)|([A-Za-z_][A-Za-z_0-9]*)|(\s+|[^\sA-Za-z_0-9'$?⟨-]+|-)/gu
    let m: RegExpExecArray | null
    while ((m = re.exec(text))) {
      if (m[1]) out.push(['tk-com', m[1]])
      else if (m[2]) out.push(['tk-str', m[2]])
      else if (m[3]) out.push(['dyn', m[3]])
      else if (m[4]) out.push(['tk-bi', m[4]])
      else if (m[5]) out.push(['tk-num', m[5]])
      else if (m[6]) out.push([SQL_KW.has(m[6].toLowerCase()) ? 'tk-kw' : '', m[6]])
      else out.push(['', m[0]])
    }
    return out
  }, [text])
  return (
    <Pre maxHeight="18rem">
      {parts.map(([c, t], i) =>
        c === 'dyn' ? (
          <span key={i} className="rounded-sm bg-warning/20 text-warning" title="computed at runtime">
            {t}
          </span>
        ) : c ? (
          <span key={i} className={c}>
            {t}
          </span>
        ) : (
          t
        ),
      )}
    </Pre>
  )
}

function PlanTree({ n, analyzed }: { n: PlanNode; analyzed: boolean }) {
  const title =
    n.nodeType +
    (n.joinType && !n.nodeType.includes(n.joinType) ? ' (' + n.joinType + ')' : '') +
    (n.strategy && n.nodeType === 'Aggregate' ? ' (' + n.strategy + ')' : '') +
    (n.relation ? ' on ' + n.relation + (n.alias && n.alias !== n.relation ? ' ' + n.alias : '') : '') +
    (n.index ? ' using ' + n.index : '')
  return (
    <li className="my-1">
      <div className="font-mono text-xs font-semibold text-fg">
        {n.parentRelationship && <span className="mr-1 font-normal text-fg-3">{n.parentRelationship}:</span>}
        {title}
      </div>
      <div className="font-mono text-[11px] text-fg-2">
        cost {n.startupCost.toFixed(2)}..{n.totalCost.toFixed(2)} · est. rows {n.planRows} · width {n.planWidth}
        {analyzed && n.actualRows !== undefined && (
          <span className="text-success">
            {' '}
            · actual rows {n.actualRows} × {n.actualLoops || 1} loops · {(n.actualTotalMs || 0).toFixed(3)} ms
          </span>
        )}
      </div>
      {[...(n.conditions ?? []), ...(n.keys ?? [])].map((c, i) => (
        <div key={i} className="font-mono text-[11px] text-fg-2">
          <span className="text-fg-3">{c.name}:</span> {c.value}
        </div>
      ))}
      {n.rowsRemovedByFilter !== undefined && (
        <div className="font-mono text-[11px] text-fg-2">rows removed by filter: {n.rowsRemovedByFilter}</div>
      )}
      {n.buffers && <div className="font-mono text-[11px] text-fg-2">buffers: {n.buffers}</div>}
      {!!n.plans?.length && (
        <ul className="ml-1.5 border-l border-line-strong pl-3.5">
          {n.plans.map((c, i) => (
            <PlanTree key={i} n={c} analyzed={analyzed} />
          ))}
        </ul>
      )}
    </li>
  )
}

function PlanBox({ p }: { p: Plan }) {
  const info: string[] = []
  if (p.indexes?.length) info.push('Indexes: ' + p.indexes.join(', '))
  if (p.planningMs !== undefined) info.push('planning ' + p.planningMs.toFixed(3) + ' ms')
  if (p.executionMs !== undefined) info.push('execution ' + p.executionMs.toFixed(3) + ' ms (one run)')
  if (p.paramsGiven) info.push(p.paramsGiven + ' parameter value(s) supplied, not recorded')
  return (
    <div className="mt-3 rounded-box border border-line bg-base-200/60 px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2 text-xs text-fg-2">
        <Basis kind={p.analyzed ? 'observed' : 'estimated'}>{p.analyzed ? 'EXPLAIN ANALYZE' : 'EXPLAIN'}</Basis>
        <span className="font-mono">{p.target}</span>
        <span>· PostgreSQL {p.server}</span>
      </div>
      <Note className="mt-1 text-fg-3">{p.mode}</Note>
      <ul className="mt-1">
        <PlanTree n={p.root} analyzed={p.analyzed} />
      </ul>
      {!!info.length && <Note className="mt-1">{info.join(' · ')}</Note>}
      {p.warnings?.map((w) => (
        <Note key={w} className="text-fg-3">
          {w}
        </Note>
      ))}
    </div>
  )
}

function Warn({ children }: { children: ReactNode }) {
  return (
    <div className="mt-2 flex items-start gap-2 rounded-field border border-warning/40 bg-warning/10 px-2.5 py-1.5 text-xs text-warning">
      <Icon name="warning" className="mt-px size-3.5" />
      <div className="min-w-0 break-words">{children}</div>
    </div>
  )
}

function OpCard({ op, i, plans }: { op: SqlOp; i: number; plans: Plan[] }) {
  const { flow, indexOf, select } = useInspect()
  const q = op.sql
  const inFlow = !!flow?.nodes?.some((n) => n.id === op.id)
  const mine = plans.filter((p) => q.text && p.sql.trim() === q.text.trim())
  return (
    <section className="rounded-box border border-line bg-base-100 px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="font-mono text-[13px] font-semibold">
          {i + 1}. {op.lib} {op.op}
        </h3>
        {q.statement && q.kind !== 'none' && (
          <span className="badge badge-ghost badge-sm uppercase">{q.statement}</span>
        )}
        {op.conf !== 'exact' && (
          <Basis kind="estimated" title="Reached through a module interface: one of its possible implementations">
            {op.conf}
          </Basis>
        )}
        {op.deferred && <Basis kind="static">deferred</Basis>}
        {inFlow && (
          <a
            className="btn btn-ghost btn-xs ml-auto"
            href={routeHref('flow', { node: op.id })}
            title="Open this operation in the Code Flow graph"
          >
            <Icon name="flow" className="size-3.5" />
            Code Flow
          </a>
        )}
      </div>
      <div className="mt-1 flex flex-wrap items-center gap-x-1.5 text-xs text-fg-2">
        <span className="font-mono">{op.method}</span>
        {op.receiver && (
          <>
            on <span className="font-mono">{op.receiver}</span>
          </>
        )}
        <span>· in</span>
        {indexOf(op.func.id) >= 0 && op.func.id !== undefined ? (
          <button
            type="button"
            className="link link-hover text-left font-mono"
            title="Inspect this function"
            onClick={() => select(op.func.id)}
          >
            {op.func.qualified}
          </button>
        ) : (
          <span className="font-mono">{op.func.qualified}</span>
        )}
        <span>·</span>
        <LocLink file={op.file} line={op.line} />
      </div>
      {!!op.via?.length && <Note className="mt-1 text-fg-3">Reached via {op.via.join(' → ')}</Note>}
      {op.call && (
        <div className="mt-2 rounded-field border border-line bg-code px-2.5 py-1.5">
          <CodeInline code={op.call} block className="whitespace-pre-wrap" />
        </div>
      )}
      {q.kind !== 'none' && (
        <>
          {q.text ? (
            <>
              <Note className="mt-2 text-fg-3">
                SQL ({q.kind}
                {q.constant ? (
                  <>
                    {' '}
                    <span className="font-mono">{q.constant}</span> at <LocLink file={q.constFile} line={q.constLine} />
                  </>
                ) : null}
                , {q.statement}
                {q.placeholders ? ', ' + q.placeholders + ' placeholder' + (q.placeholders === 1 ? '' : 's') : ''})
              </Note>
              <SqlCode text={q.formatted || q.text} />
            </>
          ) : q.variants?.length ? (
            <>
              <Note className="mt-2 text-fg-3">
                One of {q.variants.length} literal statements ({q.kind})
              </Note>
              <div className="space-y-1.5">
                {q.variants.map((v, k) => (
                  <SqlCode key={k} text={v} />
                ))}
              </div>
            </>
          ) : q.template ? (
            <>
              <Warn>SQL query not statically resolved — {q.kind}: ⟨…⟩ parts are computed at runtime</Warn>
              <div className="mt-1.5">
                <SqlCode text={q.formatted || q.template} />
              </div>
            </>
          ) : (
            <Warn>
              SQL query not statically resolved ({q.kind}){q.note ? ': ' + q.note : ''}
            </Warn>
          )}
          {q.note && (q.text || q.variants) && <Note className="mt-1">{q.note}</Note>}
          {q.multi && <Note className="mt-1 text-warning">Several statements in one string.</Note>}
        </>
      )}
      {!!op.params?.length && (
        <div className="mt-2 overflow-x-auto">
          <table className={TABLE}>
            <thead>
              <tr>
                <th>param</th>
                <th>Go expression</th>
                <th>type</th>
                <th>origin (Data Flow)</th>
              </tr>
            </thead>
            <tbody>
              {op.params.map((p) => (
                <tr key={p.index}>
                  <td className="font-mono text-k-db">${p.index}</td>
                  <td className="font-mono">{p.expr}</td>
                  <td className="font-mono text-fg-2">{p.type || ''}</td>
                  <td>
                    {p.origins?.map((o, k) => (
                      <div key={k} className="flex flex-wrap items-center gap-x-1.5">
                        <span
                          className={cx(
                            o.kind === 'source' && 'text-success',
                            o.kind === 'unresolved' && 'text-warning',
                          )}
                        >
                          {o.label}
                        </span>
                        <span className="text-[11px] text-fg-3">[{o.conf}]</span>
                        <LocLink file={o.file} line={o.line} />
                      </div>
                    ))}
                    {!p.origins?.length && <span className="text-fg-3">—</span>}
                    {p.originStatus === 'partial' && (
                      <Note className="text-fg-3">origin search stopped at its limits</Note>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {op.paramsKnown === false && <Warn>Parameters passed as a slice: not known statically.</Warn>}
      {op.scan && (
        <Note className="mt-2">
          {op.scan.note ? (
            op.scan.method + ': ' + op.scan.note
          ) : (
            <>
              {op.scan.inLoop ? 'rows.Scan (each row) into ' : 'Scan into '}
              <span className="font-mono">
                {(op.scan.targets ?? []).map((x) => x.expr + (x.type ? ' (' + x.type + ')' : '')).join(', ')}
              </span>
            </>
          )}{' '}
          <LocLink file={op.scan.file} line={op.scan.line} />
        </Note>
      )}
      {op.tx && (
        <Note className="mt-1">
          Transaction: {op.tx.role}
          {op.tx.var ? ' (' + op.tx.var + ')' : ''}
          {op.tx.fromParam
            ? ' — received as parameter'
            : op.tx.beginLine && op.tx.role !== 'begin'
              ? ', begun at line ' + op.tx.beginLine
              : ''}
          {op.tx.deferred ? ', deferred' : ''}
        </Note>
      )}
      {op.notes?.map((n) => (
        <Warn key={n}>{n}</Warn>
      ))}
      {mine.map((p, k) => (
        <PlanBox key={k} p={p} />
      ))}
    </section>
  )
}

export default function SqlTab({ b }: { b: Bundle }) {
  const { data } = useInspect()
  const r = b.sql
  const ops = r?.ops ?? []
  const plans = data.plans ?? []
  return (
    <div className="space-y-3">
      <Bar>
        <Basis kind="static">static discovery</Basis>
        <span>
          pgx, pgxpool, database/sql, sqlx. Operations of the function
          {r?.reachable ? ' and of the module functions it may call' : ''}; nothing was executed.
        </span>
      </Bar>
      {!ops.length ? (
        <Msg>No database operations found</Msg>
      ) : (
        <>
          {r?.notes?.map((n) => (
            <Note key={n}>{n}</Note>
          ))}
          {r?.status && r.status.status !== 'complete' && (
            <Note className="text-warning">
              Result {r.status.status}
              {r.status.reasons?.length ? ': ' + r.status.reasons.join('; ') : ''}
            </Note>
          )}
          {ops.map((op, i) => (
            <OpCard key={op.id + i} op={op} i={i} plans={plans} />
          ))}
          {!!r?.transactions?.length && (
            <div>
              <H4>Transactions</H4>
              <div className="space-y-1">
                {r.transactions.map((t, i) => (
                  <Note key={i}>
                    <span className="font-mono">{t.var || 'tx'}</span> begun at line {t.line} in{' '}
                    <span className="font-mono">{t.func}</span>: {t.ops.length} operation(s); commit at{' '}
                    {(t.commits ?? []).join(', ') || '—'}; rollback at {(t.rollbacks ?? []).join(', ') || '—'}
                    {t.deferredRollback ? ' (deferred)' : ''}
                  </Note>
                ))}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}
