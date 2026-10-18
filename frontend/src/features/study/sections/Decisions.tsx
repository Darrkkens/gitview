import { useStudy } from '../context'
import {
  ActionLink,
  Actions,
  BasisBadge,
  Card,
  CodeBox,
  Empty,
  LocLink,
  MarkButton,
  Note,
  SectionTitle,
  Tag,
} from '../ui'
import type { Outcome } from '../types'
import { cx } from '../../../lib/format'

function coverageTone(c: string): string {
  switch (c) {
    case 'covered':
      return 'text-success'
    case 'uncovered':
      return 'text-error'
    case 'partial':
      return 'text-warning'
    default:
      return 'text-fg-2'
  }
}

function OutcomeRow({ o, hasCoverage }: { o: Outcome; hasCoverage: boolean }) {
  const then = o.first ? o.first : 'next block (B' + o.to + ')'
  const end = o.terminal ? o.terminal + (o.returnsError ? ' (error)' : '') : o.jump || 'continues'
  return (
    <tr className="align-top">
      <td className="font-mono whitespace-nowrap">
        <span className={cx('font-semibold', o.errorPath && 'text-error')}>{o.label.toUpperCase()}</span>
        {o.errorPath && <span className="text-error"> · error path</span>}
      </td>
      <td className="font-mono break-all">
        {then}
        {o.firstLine ? <span className="text-fg-3"> :{o.firstLine}</span> : null}
      </td>
      <td className={cx(o.returnsError && 'text-error')}>{end}</td>
      <td className="font-mono break-all">{(o.assigns ?? []).join(', ')}</td>
      <td>
        {o.coverage && o.coverage !== 'unknown' ? (
          <span className="flex items-center gap-1">
            <BasisBadge basis="COVERAGE" />
            <span className={coverageTone(o.coverage)}>{o.coverage}</span>
          </span>
        ) : (
          <span className="text-fg-3">{hasCoverage ? 'unknown' : 'no coverage data'}</span>
        )}
      </td>
    </tr>
  )
}

export function Decisions() {
  const { model, links } = useStudy()
  const funcs = model.decisions ?? []
  return (
    <>
      <Note className="mb-3">
        Every decision point of the control-flow graph (Branch Explorer) of the functions in the report: the condition
        as written, and for each outcome the statement that follows, how the block ends, whether it is an error path and
        what it assigns. This is structure, not intent: the code does not say why a rule exists.
      </Note>
      {!funcs.length && <Empty>No decision found in the functions of the report.</Empty>}
      {funcs.map((f) => (
        <section key={f.func}>
          <SectionTitle className="flex flex-wrap items-baseline gap-x-2">
            <span className="font-mono break-all">{f.funcName}</span>
            <span className="text-xs font-normal text-fg-3">
              cyclomatic complexity {f.cyclomatic}
              {f.coverage ? ' · coverage: ' + f.coverage : ''}
            </span>
          </SectionTitle>
          <div className="space-y-2">
            {f.decisions.map((d) => {
              const id = f.func + '|' + d.block
              return (
                <Card key={d.block}>
                  <div className="flex flex-wrap items-center gap-1.5">
                    <Tag>{d.kind}</Tag>
                    {d.errorCheck && <Tag tone="error">error check</Tag>}
                    <BasisBadge basis="STATIC" />
                    <LocLink file={d.file} line={d.line} href={links.diff(d.file, d.line)} />
                  </div>
                  <CodeBox className="mt-2 inline-block font-semibold">{d.cond || '(no condition)'}</CodeBox>
                  <div className="mt-2 overflow-x-auto">
                    <table className="table table-xs [&_tr]:text-xs">
                      <thead>
                        <tr>
                          <th>Outcome</th>
                          <th>Then</th>
                          <th>Ends with</th>
                          <th>Assigns</th>
                          <th>Coverage</th>
                        </tr>
                      </thead>
                      <tbody>
                        {d.outcomes.map((o, i) => (
                          <OutcomeRow key={i} o={o} hasCoverage={!!f.coverage} />
                        ))}
                      </tbody>
                    </table>
                  </div>
                  {!!d.preds?.length && (
                    <Note className="mt-1">
                      Reached from block{d.preds.length > 1 ? 's' : ''} B{d.preds.join(', B')} · decision block B
                      {d.block}
                    </Note>
                  )}
                  <Actions>
                    <ActionLink
                      label="Open Branch Explorer"
                      icon="microscope"
                      href={links.href(d.evidence.ref)}
                      area="branches"
                      id={id}
                    />
                    <MarkButton area="branches" id={id} label="Studied" done="Studied" />
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
