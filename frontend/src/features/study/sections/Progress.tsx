import { useRef } from 'react'
import { Icon } from '../../../components/Icon'
import { toast } from '../../../components/Toast'
import { useStudy } from '../context'
import { resetProgress, visitedCount } from '../progress'
import { Note } from '../ui'

export function ProgressView() {
  const { model, progress } = useStudy()
  const dialog = useRef<HTMLDialogElement>(null)
  const A = model.architecture ?? {}
  const nDec = (model.decisions ?? []).reduce((n, f) => n + (f.decisions ?? []).length, 0)
  const rows: [string, string, number, number][] = [
    [
      'Architecture',
      'architecture',
      (A.chains ?? []).length + (A.functions ?? []).filter((f) => f.basis === 'STATIC').length,
      0,
    ],
    [
      'Code Flow',
      'codeflow',
      (model.readingOrder ?? []).length + (model.predict?.entries ?? []).length,
      visitedCount(progress, 'files'),
    ],
    ['Data Flow', 'dataflow', (model.dataJourneys ?? []).length, 0],
    ['Branches', 'branches', nDec, 0],
    ['SQL', 'sql', (model.sql?.queries ?? []).length, 0],
    ['Performance', 'performance', (model.performance?.static ?? []).length, 0],
    ['Tests', 'tests', (model.testMatrix ?? []).length, 0],
  ]
  const X = model.exercises ?? []
  const nItems = (model.explain ?? []).reduce((n, f) => n + (f.items ?? []).length, 0)
  const checked = Object.keys(progress.explain).filter((k) => !k.endsWith('|note')).length

  return (
    <>
      <Note className="mb-3">
        What you opened, marked or answered in this browser for this report. It counts activity; it is not a measure of
        what you know. Nothing leaves this browser.
      </Note>
      <div className="overflow-x-auto rounded-box border border-line bg-base-100">
        <table className="table table-sm [&_thead]:text-xs">
          <thead>
            <tr>
              <th>Area</th>
              <th className="text-right">Visited</th>
              <th className="w-1/2" />
            </tr>
          </thead>
          <tbody>
            {rows.map(([label, area, total, extra]) => {
              const n = Math.min(visitedCount(progress, area) + extra, total)
              return (
                <tr key={area}>
                  <td className="font-medium">{label}</td>
                  <td className="text-right font-mono text-xs whitespace-nowrap">
                    {total ? n + ' of ' + total : 'nothing to visit'}
                  </td>
                  <td>
                    <progress
                      className={
                        'progress h-1.5 w-full ' + (total && n === total ? 'progress-success' : 'progress-primary')
                      }
                      value={total ? n : 0}
                      max={total || 1}
                    />
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      <ul className="mt-3 space-y-1 text-xs text-fg-2">
        {(['quiz', 'challenge'] as const).map((k) => {
          const xs = X.filter((e) => e.kind === k)
          if (!xs.length) return null
          const d = xs.filter((e) => progress.answers[e.id] !== undefined).length
          const o = xs.filter((e) => progress.answers[e.id] === 1).length
          return (
            <li key={k}>
              {k === 'quiz' ? 'Quiz' : 'Challenges'}: answered {d} of {xs.length}, correct on the last answer {o}.
            </li>
          )
        })}
        {nItems > 0 && (
          <li>
            Explain It Yourself: {Math.min(checked, nItems)} of {nItems} points checked.
          </li>
        )}
      </ul>
      <div className="mt-4">
        <button type="button" className="btn btn-sm btn-error btn-soft" onClick={() => dialog.current?.showModal()}>
          <Icon name="refresh" className="size-3.5" />
          Reset Progress
        </button>
      </div>
      <dialog ref={dialog} className="modal">
        <div className="modal-box max-w-md">
          <h3 className="text-base font-semibold">Reset Progress?</h3>
          <p className="mt-2 text-sm text-fg-2">
            Reset the progress of this report in this browser (visited items, answers, checklist and notes)?
          </p>
          <div className="modal-action">
            <form method="dialog">
              <button className="btn btn-sm btn-ghost">Cancel</button>
            </form>
            <button
              type="button"
              className="btn btn-sm btn-error"
              onClick={() => {
                resetProgress()
                dialog.current?.close()
                toast('Progress reset', { tone: 'success' })
              }}
            >
              Reset
            </button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button aria-label="Close">close</button>
        </form>
      </dialog>
    </>
  )
}
