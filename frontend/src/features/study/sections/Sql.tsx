import { useMemo, useState, type ReactNode } from 'react'
import { useStudy } from '../context'
import type { SQLQuery } from '../types'
import { ActionLink, Actions, BasisBadge, Card, CodeBox, Empty, KV, LocLink, MarkButton, NodeName, Note } from '../ui'

function QueryCard({ q }: { q: SQLQuery }) {
  const { links } = useStudy()
  const s = q.structure
  const rows: [string, ReactNode | ReactNode[] | undefined][] = []
  if (s) {
    rows.push([
      'Tables',
      (s.tables ?? []).map(
        (x) => x.name + (x.alias ? ' AS ' + x.alias : '') + '  · ' + x.role + (x.depth ? ' (in a subquery)' : ''),
      ),
    ])
    rows.push(['CTEs', s.ctes])
    rows.push([
      'JOINs',
      (s.joins ?? []).map(
        (j) =>
          j.kind +
          ' ' +
          j.table +
          (j.alias ? ' ' + j.alias : '') +
          (j.on ? (j.on.indexOf('USING') === 0 ? ' ' : ' ON ') + j.on : ''),
      ),
    ])
    rows.push([
      s.statement === 'insert' ? 'Inserted columns' : s.statement === 'update' ? 'Updated columns' : 'Columns',
      s.columns,
    ])
    rows.push([
      'Filters (WHERE)',
      (s.filters ?? []).length
        ? s.filters
        : s.statement === 'select' || s.statement === 'update' || s.statement === 'delete'
          ? [
              <span key="w" className="text-warning">
                none — no WHERE clause
              </span>,
            ]
          : undefined,
    ])
    rows.push(
      ['GROUP BY', s.groupBy],
      ['HAVING', s.having],
      ['ORDER BY', s.orderBy],
      ['LIMIT', s.limit],
      ['OFFSET', s.offset],
    )
    rows.push(['ON CONFLICT', s.conflict], ['RETURNING', s.returning], ['Row lock', s.lock])
    for (const n of s.notes ?? [])
      rows.push([
        'Note',
        <span key="n" className="font-sans text-fg-2">
          {n}
        </span>,
      ])
  }
  rows.push(['Parameters', q.params], ['Scanned into', q.scan], ['Transaction', q.tx])
  for (const n of q.notes ?? [])
    rows.push([
      'Note',
      <span key="n" className="font-sans text-fg-2">
        {n}
      </span>,
    ])
  if (q.callers?.length) {
    rows.push([
      'Callers of ' + q.funcName.split('.').pop(),
      q.callers.map((r) => (
        <span key={r.id + r.line} className="flex flex-wrap items-center gap-1.5">
          <BasisBadge basis="STATIC" />
          <NodeName id={r.id} name={r.name} />
          <LocLink file={r.file} line={r.line} href={links.diff(r.file, r.line)} />
        </span>
      )),
    ])
  }

  const idx = q.indexes ?? []
  return (
    <Card changed={q.inReport}>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <BasisBadge basis="SQL" />
        <span className="font-mono text-[13px] font-semibold">{(q.statement || '?').toUpperCase()}</span>
        <span className="text-xs text-fg-3">
          {q.lib} {q.method} in
        </span>
        <NodeName id={q.func} name={q.funcName} className="text-xs" />
        <LocLink file={q.file} line={q.line} href={links.diff(q.file, q.line)} />
        {q.inReport && <span className="badge badge-soft badge-primary badge-sm ml-auto">reading order</span>}
      </div>
      {q.sql && <CodeBox className="mt-2">{q.sql}</CodeBox>}
      <KV rows={rows} className="mt-2" />
      {idx.length > 0 ? (
        <>
          <div className="mt-3 mb-1 text-xs text-fg-3">Indexes on the tables of this query:</div>
          <div className="overflow-x-auto">
            <table className="table table-xs [&_tr]:text-xs">
              <thead>
                <tr>
                  <th>Index</th>
                  <th>Columns (in order)</th>
                  <th>Defined at</th>
                  <th>Columns that appear in this query (text match)</th>
                  <th>EXPLAIN</th>
                </tr>
              </thead>
              <tbody>
                {idx.map((ix, i) => {
                  const name = ix.name || (ix.primary ? 'PRIMARY KEY' : 'UNIQUE constraint')
                  const kind: string[] = []
                  if (ix.primary) kind.push('primary key')
                  else if (ix.unique) kind.push('unique')
                  if (ix.method) kind.push(ix.method)
                  if (ix.implicit) kind.push('implicit index of a constraint')
                  return (
                    <tr key={i} className="align-top">
                      <td>
                        <div className="flex flex-wrap items-center gap-1.5">
                          <BasisBadge basis="SQL" />
                          <span className="font-mono">
                            {name} on {ix.table}
                          </span>
                        </div>
                        {kind.length > 0 && <div className="text-fg-3">{kind.join(' · ')}</div>}
                        {ix.where && <div className="font-mono text-fg-3">partial: WHERE {ix.where}</div>}
                        {ix.dropped && (
                          <div className="font-semibold text-warning">
                            a DROP INDEX with this name exists at {ix.dropped}
                          </div>
                        )}
                      </td>
                      <td className="font-mono">
                        {(ix.columns ?? []).join(', ')}
                        {(ix.include ?? []).length ? ' INCLUDE (' + ix.include!.join(', ') + ')' : ''}
                      </td>
                      <td>
                        <LocLink file={ix.file} line={ix.line} href={links.diff(ix.file, ix.line)} />
                      </td>
                      <td className="font-mono">{(ix.referenced ?? []).join(', ') || '—'}</td>
                      <td>
                        {q.plan ? (
                          ix.inPlan ? (
                            <span className="flex items-center gap-1">
                              <BasisBadge basis={q.plan.evidence.basis} /> the plan uses it
                            </span>
                          ) : (
                            <span className="text-fg-3">not in the plan</span>
                          )
                        ) : (
                          <span className="text-fg-3">no plan</span>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </>
      ) : (
        s &&
        (s.tables ?? []).length > 0 && (
          <Note className="mt-2">No index on these tables was found in the project's .sql files.</Note>
        )
      )}
      {q.plan ? (
        <div className="mt-3 rounded-field border border-line bg-surface-2 px-2.5 py-2">
          <div className="flex flex-wrap items-center gap-2">
            <BasisBadge basis={q.plan.evidence.basis} />
            <b className="text-xs">
              {q.plan.mode}
              {q.plan.generic ? ' (generic plan)' : ''}
            </b>
            <span className="text-xs text-fg-3">{q.plan.evidence.text}</span>
          </div>
          <KV
            className="mt-1"
            rows={[
              ['Scans', q.plan.scans],
              ['Joins', q.plan.joins],
              ['Indexes', q.plan.indexes],
              [
                'Warnings',
                q.plan.warnings?.map((w, i) => (
                  <span key={i} className="text-warning">
                    {w}
                  </span>
                )),
              ],
              [
                'Planning',
                q.plan.analyzed && q.plan.planningMs != null ? q.plan.planningMs.toFixed(3) + ' ms' : undefined,
              ],
              [
                'Execution',
                q.plan.analyzed && q.plan.executionMs != null ? q.plan.executionMs.toFixed(3) + ' ms' : undefined,
              ],
            ]}
          />
        </div>
      ) : (
        <Note className="mt-2">
          No EXPLAIN plan in this report.{' '}
          <code className="font-mono">
            gitview inspect sql --function {q.funcName.split('.').pop()} --explain --dsn &lt;development database&gt;
          </code>{' '}
          asks PostgreSQL for the plan (EXPLAIN without ANALYZE: the statement is not executed).
        </Note>
      )}
      <Actions>
        <ActionLink
          label="Open SQL Explorer"
          icon="microscope"
          href={links.inspect(q.func, 'sql')}
          area="sql"
          id={q.id}
        />
        <ActionLink
          label="Open Code Flow"
          icon="flow"
          href={links.flow(q.id) ?? links.flow(q.func)}
          area="sql"
          id={q.id}
        />
        <MarkButton area="sql" id={q.id} label="Studied" done="Studied" />
      </Actions>
    </Card>
  )
}

export function Sql() {
  const { model } = useStudy()
  const S = model.sql ?? { indexes: 0 }
  const Q = useMemo(() => S.queries ?? [], [S.queries])
  const nIn = Q.filter((q) => q.inReport).length
  const [only, setOnly] = useState(false)
  const shown = only ? Q.filter((q) => q.inReport) : Q
  return (
    <>
      <Note className="mb-2">
        Every query the code sends, as read from the source <BasisBadge basis="SQL" />: the tables it names, its joins,
        filters and clauses. Indexes come from CREATE INDEX, PRIMARY KEY and UNIQUE in the project's .sql files{' '}
        <BasisBadge basis="SQL" />; a column that appears in a query only says the text matches, not that PostgreSQL
        uses the index or that the index is good or bad. Only an EXPLAIN plan shows which index the planner chooses.
      </Note>
      <div className="mb-2 text-xs text-fg-3">
        {Q.length} queries ·{' '}
        {(S.schemaFiles ?? []).length ? (
          <>
            {S.indexes || 0} indexes in <span className="font-mono">{S.schemaFiles!.join(', ')}</span>
          </>
        ) : (
          'no .sql file in the project'
        )}
      </div>
      {(S.notes ?? []).map((n, i) => (
        <Note key={i} className="mb-2">
          {n}
        </Note>
      ))}
      {!Q.length ? (
        <Empty>No database operation with SQL was found in the module (pgx, pgxpool, database/sql, sqlx).</Empty>
      ) : (
        <>
          {nIn > 0 && nIn < Q.length && (
            <label className="mb-2 flex cursor-pointer items-center gap-2 text-xs text-fg-2">
              <input
                type="checkbox"
                className="toggle toggle-xs toggle-primary"
                checked={only}
                onChange={(e) => setOnly(e.target.checked)}
              />
              Only queries of the change's reading order ({nIn})
            </label>
          )}
          <div className="space-y-2">
            {shown.map((q) => (
              <QueryCard key={q.id + '|' + q.line} q={q} />
            ))}
          </div>
        </>
      )}
    </>
  )
}
