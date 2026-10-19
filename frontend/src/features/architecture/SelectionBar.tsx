// Over the canvas: the scale the explorer is at (Repository overview ›
// Module explorer › Symbol explorer, moved between only by explicit
// actions), and the tools for the selected cards (arrange, lock, fit, drill
// down, only related).

import { cx, plural } from '../../lib/format'
import type { Explorer, Scale, Snapshot } from './controller'
import type { Model } from './model'
import { Glyph, type GlyphName } from './ui'

const SCALES: [Scale, string, string][] = [
  ['overview', 'Overview', 'Repository overview: modules and packages, with their aggregated dependencies'],
  ['module', 'Module', "Module explorer: the files, types and functions of the selection's package"],
  ['symbol', 'Symbol', 'Symbol explorer: a function or type with its callers and callees'],
]

export function ScaleBar({ ctrl }: { ctrl: Explorer; snap: Snapshot }) {
  const cur = ctrl.scale()
  return (
    <nav
      aria-label="Scale"
      className="inline-flex h-[26px] items-center rounded-full border border-line bg-base-100 p-0.5 text-xs shadow-sm"
    >
      {SCALES.map(([s, label, title], k) => (
        <span key={s} className="inline-flex items-center">
          {k > 0 && (
            <span aria-hidden="true" className="px-0.5 text-[10px] text-fg-3">
              ›
            </span>
          )}
          <button
            type="button"
            title={title}
            aria-current={cur === s ? 'step' : undefined}
            className={cx(
              'h-[20px] rounded-full px-2 whitespace-nowrap',
              cur === s ? 'bg-accent-weak font-medium text-accent-text' : 'text-fg-2 hover:bg-surface-hover hover:text-fg',
            )}
            onClick={() => cur !== s && ctrl.goScale(s)}
          >
            {label}
          </button>
        </span>
      ))}
    </nav>
  )
}

function Tool({
  glyph,
  title,
  onClick,
  pressed,
  label,
  disabled,
}: {
  glyph: GlyphName
  title: string
  onClick: () => void
  pressed?: boolean
  label?: string
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      title={title}
      aria-label={title.replace(/ \(.*\)$/, '')}
      aria-pressed={pressed}
      disabled={disabled}
      className={cx(
        'btn btn-ghost btn-xs h-6 min-h-0 gap-1 px-1.5 font-normal',
        pressed && 'bg-accent-weak text-accent-text',
      )}
      onClick={onClick}
    >
      <Glyph name={glyph} className="size-3.5" />
      {label && <span className="max-[1100px]:hidden">{label}</span>}
    </button>
  )
}

const Sep = () => <span aria-hidden="true" className="mx-0.5 h-4 w-px bg-line" />

export function SelectionBar({ D, ctrl, snap }: { D: Model; ctrl: Explorer; snap: Snapshot }) {
  const { S } = snap
  if (S.sel?.kind !== 'item') return null
  const boxes = ctrl.selectedBoxes()
  const n = Math.max(S.multi.size, 1)
  const locked = boxes.length > 0 && boxes.every((b) => b.locked)
  const drill = ctrl.drillTarget()
  const t = D.T[S.sel.i]
  return (
    <div
      role="toolbar"
      aria-label="Selection"
      className="inline-flex h-[30px] max-w-full items-center gap-0.5 overflow-x-auto rounded-box border border-line bg-base-100 px-1 shadow-sm"
    >
      <span className="max-w-[220px] truncate px-1.5 text-xs text-fg-2" title={D.reference(t)}>
        {n > 1 ? plural(n, 'card') + ' selected' : D.displayName(t)}
      </span>
      <Sep />
      {n > 1 && (
        <>
          <Tool glyph="alignLeft" title="Align left" onClick={() => ctrl.align('left')} />
          <Tool glyph="alignCenter" title="Align centres" onClick={() => ctrl.align('center')} />
          <Tool glyph="alignRight" title="Align right" onClick={() => ctrl.align('right')} />
          <Tool
            glyph="distH"
            title="Distribute horizontally"
            onClick={() => ctrl.distribute('h')}
            disabled={n < 3}
          />
          <Tool glyph="distV" title="Distribute vertically" onClick={() => ctrl.distribute('v')} disabled={n < 3} />
          <Sep />
        </>
      )}
      <Tool
        glyph={locked ? 'unlock' : 'lock'}
        title={(locked ? 'Unlock' : 'Lock in place') + ' (L)'}
        label={locked ? 'Unlock' : 'Lock'}
        onClick={() => ctrl.setLocked(!locked)}
      />
      <Tool glyph="fitSel" title="Fit the selection (C)" onClick={() => ctrl.fitSelection()} />
      <Tool
        glyph="isolate"
        title="Only related: hide what the selection is not connected to (O)"
        label="Only related"
        pressed={S.isolate}
        onClick={() => ctrl.setIsolate(!S.isolate)}
      />
      {drill && (
        <Tool
          glyph="drill"
          title={
            (drill.kind === 'symbol'
              ? 'Symbol explorer: its callers and callees'
              : S.focus
                ? 'Focus on it'
                : 'Module explorer: open it as the scope') + ' (D)'
          }
          label="Drill down"
          onClick={() => ctrl.drillDown()}
        />
      )}
    </div>
  )
}
