import { Fragment, useMemo } from 'react'
import { cx } from '../../../lib/format'
import { useStudy } from '../context'
import type { DataJourney } from '../types'
import { ActionLink, Actions, BasisBadge, Card, Empty, LocLink, MarkButton, Note, SectionTitle, Tag } from '../ui'

const ROLE: Record<string, string> = {
  SOURCE: 'badge-success',
  TRANSFORMATION: 'badge-neutral',
  DECISION: 'badge-warning',
  SINK: 'badge-secondary',
}

export function RoleBadge({ role }: { role: string }) {
  return (
    <span
      className={cx(
        'badge badge-xs h-4 shrink-0 px-1 font-mono text-[10px] font-semibold',
        ROLE[role] ?? 'badge-ghost',
      )}
    >
      {role}
    </span>
  )
}

export function DataJourneys() {
  const { model, links } = useStudy()
  const groups = useMemo(() => {
    const out: { name: string; items: DataJourney[] }[] = []
    for (const j of model.dataJourneys ?? []) {
      const last = out[out.length - 1]
      if (last && last.name === j.funcName) last.items.push(j)
      else out.push({ name: j.funcName, items: [j] })
    }
    return out
  }, [model.dataJourneys])

  return (
    <>
      <Note className="mb-3">
        Each journey follows one variable of a function through the Data Flow analysis (SSA def-use, across calls), from
        where the value enters to where it ends. <RoleBadge role="SOURCE" /> input (HTTP request, database row,
        parameter, constant). <RoleBadge role="TRANSFORMATION" /> assignments, calls, fields.{' '}
        <RoleBadge role="DECISION" /> a condition that tests it. <RoleBadge role="SINK" /> SQL, a call outside the
        module, a return. Only relations the analysis proved are shown.
      </Note>
      {!groups.length && (
        <Empty>No data journey: the report has no Data Flow results for the functions of the reading order.</Empty>
      )}
      {groups.map((g, gi) => (
        <section key={gi}>
          <SectionTitle className="font-mono break-all">{g.name}</SectionTitle>
          <div className="grid gap-2 xl:grid-cols-2">
            {g.items.map((j) => {
              const id = j.func + '|' + j.variable
              return (
                <Card key={id}>
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-mono text-[13px] font-semibold break-all">{j.variable}</span>
                    <BasisBadge basis="STATIC" />
                    <Tag tone={j.complete ? 'success' : 'warning'}>{j.complete ? 'source → sink' : 'partial path'}</Tag>
                  </div>
                  <ol className="mt-2 flex flex-col items-start">
                    {j.steps.map((st, i) => {
                      const meta: string[] = []
                      if (st.var && st.var !== st.label) meta.push(st.var)
                      if (st.type) meta.push(st.type)
                      if (st.funcName) meta.push('in ' + st.funcName)
                      return (
                        <Fragment key={i}>
                          {i > 0 && (
                            <li className="my-0.5 ml-4 border-l border-dashed border-line-strong py-0.5 pl-3 text-[11px] text-fg-3">
                              ↓ {st.via}
                              {st.viaText ? ' (' + st.viaText + ')' : ''}
                              {st.conf && st.conf !== 'exact' ? ' · ' + st.conf : ''}
                            </li>
                          )}
                          <li className="max-w-full rounded-field border border-line bg-surface-2 px-2 py-1 text-xs">
                            <div className="flex flex-wrap items-center gap-1.5">
                              <RoleBadge role={st.role} />
                              <b className="font-mono font-semibold break-all">{st.label}</b>
                              {meta.length > 0 && <span className="text-fg-3">· {meta.join(' · ')}</span>}
                              <LocLink file={st.file} line={st.line} href={links.diff(st.file, st.line)} />
                            </div>
                            {st.code && (
                              <div className="mt-0.5 font-mono text-[11px] break-all text-fg-3">{st.code.trim()}</div>
                            )}
                          </li>
                        </Fragment>
                      )
                    })}
                  </ol>
                  {(j.partial ?? []).map((p, i) => (
                    <Note key={i} className="mt-1">
                      Partial: {p}
                    </Note>
                  ))}
                  <Actions>
                    <ActionLink
                      label="Open Data Flow"
                      icon="microscope"
                      href={links.href(j.evidence.ref)}
                      area="dataflow"
                      id={id}
                    />
                    <MarkButton area="dataflow" id={id} label="I followed this journey" done="Followed" />
                  </Actions>
                </Card>
              )
            })}
          </div>
        </section>
      ))}
    </>
  )
}
