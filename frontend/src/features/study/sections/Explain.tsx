import { useState } from 'react'
import { Icon } from '../../../components/Icon'
import { cx } from '../../../lib/format'
import { setParams } from '../../../lib/router'
import { useStudy } from '../context'
import { setExplain } from '../progress'
import type { ExplainFunc, ExplainItem } from '../types'
import { BasisBadge, Card, Empty, FunctionActions, LayerBadge, LocLink, Note } from '../ui'

function Item({ f, it }: { f: ExplainFunc; it: ExplainItem }) {
  const { links, progress } = useStudy()
  const [open, setOpen] = useState(false)
  const id = f.func + '|' + it.key
  const checked = !!progress.explain[id]
  const href = links.href(it.ref)
  return (
    <div className={cx('rounded-field border bg-base-100 px-3 py-2', checked ? 'border-success/50' : 'border-line')}>
      <div className="flex items-start gap-2.5">
        <input
          type="checkbox"
          className="checkbox checkbox-sm checkbox-success mt-0.5"
          checked={checked}
          onChange={(e) => setExplain(id, e.target.checked ? 1 : undefined)}
          aria-label={it.prompt}
        />
        <span className={cx('min-w-0 flex-1 text-[13px] leading-snug', checked && 'text-fg-2')}>{it.prompt}</span>
        <button
          type="button"
          className={cx('btn btn-xs shrink-0', open ? 'btn-primary btn-soft' : 'btn-ghost')}
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
        >
          <Icon name={open ? 'chevronDown' : 'chevronRight'} className="size-3.5" />
          {open ? 'Hide' : 'Reveal'}
        </button>
      </div>
      {open && (
        <ul className="mt-2 ml-7 space-y-1 border-l border-line pl-3 font-mono text-xs">
          {!it.facts?.length && <li className="font-sans text-fg-3">Nothing found by the analysis.</li>}
          {(it.facts ?? []).map((x, i) => (
            <li key={i} className="flex items-start gap-1.5 break-words">
              <BasisBadge basis={it.basis} className="mt-0.5" />
              <span className="min-w-0">{x}</span>
            </li>
          ))}
          {href && (
            <li className="font-sans">
              <a href={href} className="link link-hover text-accent-text">
                open the evidence
              </a>
            </li>
          )}
        </ul>
      )}
    </div>
  )
}

export function Explain({ fnParam }: { fnParam?: string | null }) {
  const { model, progress } = useStudy()
  const F = model.explain ?? []
  const idx = Math.min(Math.max(Number(fnParam) || 0, 0), Math.max(F.length - 1, 0))
  const f = F[idx]
  const noteKey = f ? f.func + '|note' : ''
  const note = (progress.explain[noteKey] as string | undefined) ?? ''
  const done = f ? f.items.filter((it) => progress.explain[f.func + '|' + it.key]).length : 0
  return (
    <>
      <Note className="mb-3">
        Pick a function and explain it in your own words, out loud or in the box below, before revealing anything. Each
        point of the checklist has a Reveal button with the facts the analysis found, labelled with their source, so you
        can check yourself. GitView does not judge your explanation.
      </Note>
      {!f ? (
        <Empty>No function in the reading order.</Empty>
      ) : (
        <>
          <label className="mb-3 flex flex-wrap items-center gap-2 text-xs text-fg-2">
            Function:
            <select
              className="select select-sm w-auto max-w-full min-w-64 pr-8 font-mono"
              value={idx}
              onChange={(e) => setParams({ fn: e.target.value === '0' ? undefined : e.target.value })}
            >
              {F.map((x, i) => (
                <option key={x.func} value={i}>
                  {x.funcName}
                </option>
              ))}
            </select>
          </label>
          <Card>
            <div className="flex flex-wrap items-center gap-2">
              <LayerBadge layer={f.layer} />
              <span className="font-mono text-[13px] font-semibold break-all">{f.funcName}</span>
              <LocLink file={f.file} line={f.line} className="ml-auto" />
            </div>
            <FunctionActions key={f.func} id={f.func} file={f.file} line={f.line} area="codeflow" />
          </Card>
          <div className="mt-3 mb-1.5 flex items-center gap-2 text-xs text-fg-3">
            Checklist · {done} of {f.items.length} checked
            <progress className="progress progress-success h-1.5 w-32" value={done} max={f.items.length || 1} />
          </div>
          <div key={f.func} className="space-y-1.5">
            {f.items.map((it) => (
              <Item key={it.key} f={f} it={it} />
            ))}
          </div>
          <label className="mt-3 block">
            <span className="mb-1 block text-xs text-fg-3">Your explanation (kept only in this browser)</span>
            <textarea
              key={noteKey}
              className="textarea textarea-sm min-h-28 w-full font-mono text-xs"
              placeholder="Your explanation (kept only in this browser)"
              aria-label="Your explanation"
              value={note}
              onChange={(e) => setExplain(noteKey, e.target.value)}
            />
          </label>
        </>
      )}
    </>
  )
}
