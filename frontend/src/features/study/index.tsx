// Study Mode: a learning layer over the analyses of the report (call graph,
// CFG, Data Flow, SQL, coverage, traces, profiles, diff). Nothing here is
// generated text: every item comes from the Study model (internal/study)
// with the evidence it rests on. Progress stays in this browser.
//
// URL: #/study?section=<key>&step=<n>&entry=<i>&fn=<i>

import { useEffect, useMemo, useRef, type ReactNode } from 'react'
import { Icon, type IconName } from '../../components/Icon'
import { PaneHeader, Panes } from '../../components/Panes'
import { StatusSlot } from '../../components/Slots'
import { EmptyState, SectionGate } from '../../components/States'
import { isTyping } from '../../lib/browser'
import { cx, plural } from '../../lib/format'
import { routeHref, setParams } from '../../lib/router'
import type { ReportSection } from '../../lib/types'
import type { ViewProps } from '../../views'
import { StudyContext, type StudyCtx } from './context'
import { BASIS_HELP } from './fmt'
import { makeLinks } from './links'
import { useProgress, visitedCount } from './progress'
import { Architecture } from './sections/Architecture'
import { Changes } from './sections/Changes'
import { DataJourneys } from './sections/DataJourneys'
import { Decisions } from './sections/Decisions'
import { Exercises } from './sections/Exercises'
import { Explain } from './sections/Explain'
import { Performance } from './sections/Performance'
import { Predict } from './sections/Predict'
import { ProgressView } from './sections/Progress'
import { Sql } from './sections/Sql'
import { StartHere } from './sections/StartHere'
import { Tests } from './sections/Tests'
import type { StudyModel } from './types'
import { BasisBadge } from './ui'

interface SectionDef {
  key: string
  name: string
  icon: IconName
  /** Count shown in the menu (items of the section). */
  count?: (m: StudyModel) => number
}

const SECTIONS: SectionDef[] = [
  { key: 'start', name: 'Start Here', icon: 'play', count: (m) => (m.readingOrder ?? []).length },
  { key: 'architecture', name: 'Architecture', icon: 'graph', count: (m) => (m.architecture?.chains ?? []).length },
  {
    key: 'changes',
    name: 'Change Journey',
    icon: 'diff',
    count: (m) => (m.changes ?? []).reduce((n, g) => n + g.files.length, 0),
  },
  { key: 'data', name: 'Data Journeys', icon: 'flow', count: (m) => (m.dataJourneys ?? []).length },
  {
    key: 'decisions',
    name: 'Decisions',
    icon: 'code',
    count: (m) => (m.decisions ?? []).reduce((n, f) => n + f.decisions.length, 0),
  },
  { key: 'tests', name: 'Tests', icon: 'beaker', count: (m) => (m.testMatrix ?? []).length },
  { key: 'predict', name: 'Predict Execution', icon: 'target', count: (m) => (m.predict?.entries ?? []).length },
  { key: 'sql', name: 'SQL', icon: 'file', count: (m) => (m.sql?.queries ?? []).length },
  { key: 'performance', name: 'Performance', icon: 'gauge', count: (m) => (m.performance?.static ?? []).length },
  { key: 'explain', name: 'Explain It Yourself', icon: 'book', count: (m) => (m.explain ?? []).length },
  {
    key: 'quiz',
    name: 'Quiz',
    icon: 'help',
    count: (m) => (m.exercises ?? []).filter((e) => e.kind === 'quiz').length,
  },
  {
    key: 'challenges',
    name: 'Challenges',
    icon: 'sparkles',
    count: (m) => (m.exercises ?? []).filter((e) => e.kind === 'challenge').length,
  },
  { key: 'progress', name: 'Progress', icon: 'check' },
]

export default function StudyView({ params }: ViewProps) {
  return (
    <SectionGate<ReportSection> section="report" loadingLabel="Analysing the repository for Study Mode…">
      {(r) =>
        r.study ? (
          <Study report={r} model={r.study as StudyModel} params={params} />
        ) : (
          <EmptyState icon="book" title="No Study Mode in this report">
            This report was exported without the Study tab. <code className="font-mono">gitview study</code> (or the
            Study option of the TUI export, <kbd className="kbd kbd-xs">x</kbd>) builds it.
          </EmptyState>
        )
      }
    </SectionGate>
  )
}

function summaryParts(m: StudyModel): string[] {
  const s = m.summary
  if (!s) return []
  return [
    plural(s.functions || 0, 'function'),
    plural(s.routes || 0, 'route'),
    plural(s.changedFiles || 0, 'changed file'),
    plural(s.changedFunctions || 0, 'changed function'),
    s.hasCoverage ? 'coverage: ' + (s.coverageProfile || 'loaded') : 'no coverage profile',
    s.hasTrace ? 'execution trace loaded' : 'no execution trace',
  ]
}

function Study({ report, model, params }: { report: ReportSection; model: StudyModel; params: URLSearchParams }) {
  const links = useMemo(() => makeLinks(report), [report])
  const progress = useProgress(model.fingerprint || '')
  const ctx: StudyCtx = useMemo(() => ({ model, links, progress }), [model, links, progress])
  const stepParam = Number(params.get('step')) || undefined
  const key = params.get('section') || 'start'
  const current = SECTIONS.find((s) => s.key === key) ?? SECTIONS[0]
  const main = useRef<HTMLDivElement>(null)

  // A new section starts at the top (a step link scrolls to its step).
  const shownKey = useRef(current.key)
  useEffect(() => {
    if (shownKey.current === current.key) return
    shownKey.current = current.key
    if (main.current && !(current.key === 'start' && stepParam)) main.current.scrollTop = 0
  }, [current.key, stepParam])

  // Keyboard: [ ] previous/next section; j k next/previous step of Start Here.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (isTyping(e) || e.metaKey || e.ctrlKey || e.altKey) return
      const i = SECTIONS.indexOf(current)
      if (e.key === ']' || e.key === '[') {
        const next = SECTIONS[(i + (e.key === ']' ? 1 : SECTIONS.length - 1)) % SECTIONS.length]
        setParams({
          section: next.key === 'start' ? undefined : next.key,
          step: undefined,
          entry: undefined,
          fn: undefined,
        })
        e.preventDefault()
      } else if ((e.key === 'j' || e.key === 'k') && current.key === 'start') {
        const n = (model.readingOrder ?? []).length
        if (!n) return
        const cur = stepParam ?? 0
        const next = e.key === 'j' ? Math.min(cur + 1, n) : Math.max(cur - 1, 1)
        setParams({ step: next })
        e.preventDefault()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [current, stepParam, model.readingOrder])

  const counts = useMemo(() => new Map(SECTIONS.map((s) => [s.key, s.count?.(model)])), [model])
  const parts = summaryParts(model)

  let body: ReactNode
  switch (current.key) {
    case 'architecture':
      body = <Architecture />
      break
    case 'changes':
      body = <Changes />
      break
    case 'data':
      body = <DataJourneys />
      break
    case 'decisions':
      body = <Decisions />
      break
    case 'tests':
      body = <Tests />
      break
    case 'predict':
      body = <Predict entryParam={params.get('entry')} />
      break
    case 'sql':
      body = <Sql />
      break
    case 'performance':
      body = <Performance />
      break
    case 'explain':
      body = <Explain fnParam={params.get('fn')} />
      break
    case 'quiz':
      body = (
        <Exercises
          kind="quiz"
          intro="Questions built from fixed templates over the analysis: each has one right answer, alternatives that are certainly wrong, and the evidence of the answer. A question is only asked when such alternatives exist."
          empty="Not enough facts for questions with reliable alternatives."
        />
      )
      break
    case 'challenges':
      body = (
        <Exercises
          kind="challenge"
          intro="Follow the code: decide which way a branch goes for given values (the condition is evaluated, never guessed, and only simple integer and boolean conditions are used), where a value ends up, which expression a query receives, which function reaches the database, and in which order the trace shows functions."
          empty="No challenge could be built from this report."
        />
      )
      break
    case 'progress':
      body = <ProgressView />
      break
    default:
      body = <StartHere step={stepParam} />
  }

  const left = (
    <>
      <PaneHeader title="Study Mode" />
      <nav aria-label="Study sections" className="scroll-thin min-h-0 flex-1 overflow-y-auto p-1.5">
        <ul className="menu menu-sm w-full gap-px p-0">
          {SECTIONS.map((s) => {
            const n = counts.get(s.key)
            const answered =
              s.key === 'quiz' || s.key === 'challenges'
                ? (model.exercises ?? []).filter(
                    (e) => e.kind === (s.key === 'quiz' ? 'quiz' : 'challenge') && progress.answers[e.id] !== undefined,
                  ).length
                : undefined
            return (
              <li key={s.key}>
                <a
                  href={routeHref('study', s.key === 'start' ? undefined : { section: s.key })}
                  className={cx('gap-2 py-1.5', s.key === current.key && 'menu-active')}
                  aria-current={s.key === current.key ? 'page' : undefined}
                >
                  <Icon name={s.icon} className="size-3.5 opacity-80" />
                  <span className="flex-1 truncate">{s.name}</span>
                  {n !== undefined && (
                    <span
                      className={cx(
                        'font-mono text-[11px]',
                        s.key === current.key ? 'opacity-80' : n ? 'text-fg-3' : 'text-fg-3/50',
                      )}
                    >
                      {answered !== undefined && n ? answered + '/' + n : n}
                    </span>
                  )}
                </a>
              </li>
            )
          })}
        </ul>
      </nav>
      <div className="shrink-0 space-y-2 border-t border-line p-3 text-[11px] text-fg-3">
        <div className="grid grid-cols-2 gap-1.5">
          <Mini label="functions" value={model.summary?.functions ?? 0} />
          <Mini label="routes" value={model.summary?.routes ?? 0} />
          <Mini label="changed files" value={model.summary?.changedFiles ?? 0} />
          <Mini label="changed functions" value={model.summary?.changedFunctions ?? 0} />
        </div>
        <div className="space-y-0.5">
          <div className={cx(model.summary?.hasCoverage && 'text-success')}>{parts[4]}</div>
          <div className={cx(model.summary?.hasTrace && 'text-success')}>{parts[5]}</div>
        </div>
        <div className="flex flex-wrap gap-1" aria-label="Sources">
          {Object.keys(BASIS_HELP).map((b) => (
            <BasisBadge key={b} basis={b} />
          ))}
        </div>
        <div>
          <kbd className="kbd kbd-xs">[</kbd> <kbd className="kbd kbd-xs">]</kbd> sections
          {current.key === 'start' && (
            <>
              {' · '}
              <kbd className="kbd kbd-xs">j</kbd> <kbd className="kbd kbd-xs">k</kbd> steps
            </>
          )}
        </div>
      </div>
    </>
  )

  const visitedTotal = Object.keys(progress.visited).reduce((n, a) => n + visitedCount(progress, a), 0)

  return (
    <StudyContext.Provider value={ctx}>
      <Panes id="study" left={left} leftWidth={236}>
        <div ref={main} className="scroll-thin min-h-0 flex-1 overflow-y-auto">
          <div className="mx-auto max-w-6xl px-6 pt-4 pb-12">
            <header className="mb-3 flex flex-wrap items-baseline gap-x-3 gap-y-1 border-b border-line pb-2">
              <h2 className="flex items-center gap-2 text-lg font-semibold">
                <Icon name={current.icon} className="size-4 text-primary" />
                {current.name}
              </h2>
              <p className="text-xs text-fg-3">Every item shows where it comes from; nothing here is generated text.</p>
            </header>
            {(model.notes ?? []).map((n, i) => (
              <div key={i} role="alert" className="alert alert-warning alert-soft mb-3 py-2 text-xs">
                <Icon name="warning" className="size-4" />
                <span>{n}</span>
              </div>
            ))}
            {body}
          </div>
        </div>
      </Panes>
      <StatusSlot>
        <span className="truncate">Study · {parts.join(' · ')}</span>
        {visitedTotal > 0 && <span>{plural(visitedTotal, 'item')} visited</span>}
      </StatusSlot>
    </StudyContext.Provider>
  )
}

function Mini({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-field bg-surface-2 px-2 py-1">
      <div className="font-mono text-sm font-semibold text-fg">{value.toLocaleString('en-US')}</div>
      <div className="leading-tight">{label}</div>
    </div>
  )
}
