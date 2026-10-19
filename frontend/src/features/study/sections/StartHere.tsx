import { useEffect } from 'react'
import { routeHref } from '../../../lib/router'
import { cx } from '../../../lib/format'
import { useStudy } from '../context'
import { ActionLink, Actions, Card, Empty, EvidenceList, FunctionActions, LayerBadge, LocLink, Note, Tag } from '../ui'

/** Start Here: the recommended reading order. step is the step to bring into view. */
export function StartHere({ step }: { step?: number }) {
  const { model, links } = useStudy()
  const steps = model.readingOrder ?? []

  useEffect(() => {
    if (!step) return
    const el = document.getElementById('study-step-' + step)
    el?.scrollIntoView({ block: 'center' })
  }, [step])

  return (
    <>
      <Note className="mb-3">
        Entry points that lead to the change first, then the changed functions, their data access, changed SQL, the
        tests that reach the change and the other changed files. The order is computed from the call graph and the diff:
        the same change always gives the same order.
      </Note>
      {!steps.length ? (
        <Empty>No reading order: no changed Go code was found in this report.</Empty>
      ) : (
        <ol className="relative space-y-2">
          {steps.map((s) => {
            const on = step === s.n
            const isCode = s.kind === 'function' || s.kind === 'route'
            return (
              <li key={s.n} id={'study-step-' + s.n} className="flex scroll-mt-4 gap-2.5">
                <a
                  href={routeHref('study', { step: s.n })}
                  className={cx(
                    'mt-2 flex size-6 shrink-0 items-center justify-center rounded-full border font-mono text-[11px] font-semibold',
                    on
                      ? 'border-primary bg-primary text-primary-content'
                      : s.changed
                        ? 'border-primary/60 bg-primary/10 text-accent-text'
                        : 'border-line bg-base-200 text-fg-2',
                  )}
                  title={'Link to step ' + s.n}
                  aria-current={on ? 'step' : undefined}
                >
                  {s.n}
                </a>
                <Card changed={s.changed} highlight={on} className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                    <LayerBadge layer={s.layer} />
                    <span className="font-mono text-[13px] font-semibold break-all">{s.title}</span>
                    {s.changed && <Tag tone="primary">changed</Tag>}
                    <Tag className="text-fg-3">{s.kind}</Tag>
                    <LocLink file={s.file} line={s.line} href={links.diff(s.file, s.line)} className="ml-auto" />
                  </div>
                  {s.detail && s.detail !== s.file && (
                    <div className="mt-1 font-mono text-xs break-all text-fg-3">{s.detail}</div>
                  )}
                  <EvidenceList items={s.why} className="mt-2" />
                  {isCode ? (
                    <FunctionActions id={s.id} file={s.file} line={s.line} area="codeflow" />
                  ) : (
                    <Actions>
                      <ActionLink
                        label="Open in diff"
                        icon="diff"
                        href={links.diff(s.file, s.line)}
                        area="files"
                        id={s.file}
                      />
                    </Actions>
                  )}
                </Card>
              </li>
            )
          })}
        </ol>
      )}
    </>
  )
}
