import { useMemo, useState } from 'react'
import { Icon } from '../../../components/Icon'
import { cx, loc } from '../../../lib/format'
import { useStudy } from '../context'
import { answer, track } from '../progress'
import type { Exercise } from '../types'
import { BasisBadge, Card, CodeBox, Empty, Note, SectionTitle } from '../ui'

const TYPE_NAMES: Record<string, string> = {
  signature: 'Signatures',
  callgraph: 'Call graph',
  cfg: 'Control flow',
  sql: 'SQL',
  dataflow: 'Data Flow',
  route: 'Routes',
  architecture: 'Architecture',
  branch: 'Branch challenge',
  trace: 'Trace challenge',
}

/**
 * ExerciseCard is a question with one right answer: the options are
 * compared by index with the answer the model recorded, and the evidence
 * of the answer is revealed once answered (or on demand).
 */
export function ExerciseCard({ e, area }: { e: Exercise; area: string }) {
  const { links, progress } = useStudy()
  const [before] = useState(() => progress.answers[e.id])
  const [chosen, setChosen] = useState<number | null>(null)
  const [showEv, setShowEv] = useState(false)
  const done = chosen !== null
  const ok = chosen === e.answer
  const href = links.href(e.evidence.ref)
  const where = loc({ file: e.evidence.file || e.sourceFile, line: e.evidence.line || e.sourceLine })

  const choose = (i: number) => {
    setChosen(i)
    setShowEv(true)
    answer(e.id, i === e.answer)
    track(area, e.id)
  }

  return (
    <Card>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
        <BasisBadge basis={e.evidence.basis} />
        <span className="text-fg-3">
          {e.type} · source: {e.sourceType}
        </span>
        {e.sourceFile && <span className="font-mono text-fg-3">{loc({ file: e.sourceFile, line: e.sourceLine })}</span>}
        {before !== undefined && !done && (
          <span className={cx('ml-auto badge badge-soft badge-sm', before ? 'badge-success' : 'badge-warning')}>
            answered before: {before ? 'correct' : 'not correct'}
          </span>
        )}
      </div>
      <p className="mt-1.5 text-[13px] leading-snug text-fg">{e.prompt}</p>
      {e.code && <CodeBox className="mt-2">{e.code}</CodeBox>}
      {!!e.given?.length && <CodeBox className="mt-2">given: {e.given.join(', ')}</CodeBox>}
      <div className="mt-2 flex flex-col gap-1" role="group" aria-label="Options">
        {e.options.map((o, i) => {
          const right = done && i === e.answer
          const wrong = done && i === chosen && !ok
          return (
            <button
              key={i}
              type="button"
              disabled={done}
              onClick={() => choose(i)}
              className={cx(
                'flex w-full items-start gap-2 rounded-field border px-2.5 py-1.5 text-left font-mono text-xs break-all transition-colors',
                right
                  ? 'border-success bg-success/10 text-fg'
                  : wrong
                    ? 'border-error bg-error/10 text-fg'
                    : done
                      ? 'border-line text-fg-3'
                      : 'cursor-pointer border-line bg-surface-2 text-fg hover:border-primary/60 hover:bg-surface-hover',
              )}
            >
              <span className="w-4 shrink-0 text-fg-3">{String.fromCharCode(65 + i)}</span>
              <span className="min-w-0 flex-1">{o}</span>
              {right && <Icon name="check" className="size-3.5 text-success" />}
              {wrong && <Icon name="x" className="size-3.5 text-error" />}
            </button>
          )
        })}
      </div>
      {done && (
        <p className="mt-2 text-xs">
          <span className={cx('font-semibold', ok ? 'text-success' : 'text-error')}>
            {ok ? 'Correct.' : 'Not this one.'}
          </span>
          {!ok && <span className="text-fg-2"> The answer is marked; see the evidence.</span>}
        </p>
      )}
      <div className="mt-2 flex flex-wrap items-center gap-1">
        <button
          type="button"
          className={cx('btn btn-xs', showEv ? 'btn-primary btn-soft' : 'btn-ghost')}
          aria-expanded={showEv}
          onClick={() => setShowEv((v) => !v)}
        >
          <Icon name={showEv ? 'chevronDown' : 'chevronRight'} className="size-3.5" />
          {showEv ? 'Hide Evidence' : 'Show Evidence'}
        </button>
        {done && (
          <button type="button" className="btn btn-ghost btn-xs" onClick={() => setChosen(null)}>
            <Icon name="refresh" className="size-3.5" />
            Try again
          </button>
        )}
      </div>
      {showEv && (
        <div className="mt-1.5 rounded-field bg-surface-2 px-2.5 py-1.5 text-xs leading-5 break-words">
          <BasisBadge basis={e.evidence.basis} className="mr-1.5" />
          <span className="mr-1.5 text-fg-2">{e.evidence.text}</span>
          {where && <span className="mr-1.5 font-mono text-fg-3">{where}</span>}
          {href && (
            <a href={href} className="link link-hover text-accent-text">
              open
            </a>
          )}
        </div>
      )}
    </Card>
  )
}

export function Exercises({ kind, intro, empty }: { kind: 'quiz' | 'challenge'; intro: string; empty: string }) {
  const { model, progress } = useStudy()
  const items = useMemo(() => (model.exercises ?? []).filter((e) => e.kind === kind), [model.exercises, kind])
  const groups = useMemo(() => {
    const by = new Map<string, Exercise[]>()
    for (const e of items) {
      const l = by.get(e.type)
      if (l) l.push(e)
      else by.set(e.type, [e])
    }
    return [...by.entries()]
  }, [items])
  const answered = items.filter((e) => progress.answers[e.id] !== undefined).length
  const correct = items.filter((e) => progress.answers[e.id] === 1).length
  const area = kind === 'quiz' ? 'quiz' : 'challenges'

  return (
    <>
      <Note className="mb-3">{intro}</Note>
      {!items.length ? (
        <Empty>{empty}</Empty>
      ) : (
        <div className="mb-2 flex flex-wrap items-center gap-3 text-xs text-fg-2">
          <span>
            {items.length} {kind === 'quiz' ? 'questions' : 'challenges'}
          </span>
          <span>· answered {answered}</span>
          <span>· correct {correct}</span>
          <progress className="progress progress-primary h-1.5 w-40" value={answered} max={items.length} />
        </div>
      )}
      {groups.map(([type, list]) => (
        <section key={type}>
          <SectionTitle>{TYPE_NAMES[type] ?? type}</SectionTitle>
          <div className="grid gap-2 xl:grid-cols-2">
            {list.map((e) => (
              <ExerciseCard key={e.id} e={e} area={area} />
            ))}
          </div>
        </section>
      ))}
    </>
  )
}
