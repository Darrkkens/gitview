// Keyboard shortcuts of the Architecture view (?).

import { Fragment, useEffect, useRef } from 'react'
import { Icon } from '../../components/Icon'
import { Kbd } from './ui'

// Each shortcut: alternatives separated by "·", each a sequence of keys.
const ROWS: { keys: string[][]; text: string; plain?: boolean; section?: string }[] = [
  { section: 'Navigate', keys: [['Ctrl', 'K'], ['/']], text: 'Search everything' },
  { keys: [['2'], ['3']], text: 'HTTP view · Security view' },
  { keys: [['Enter']], text: 'Expand or collapse the selection' },
  { keys: [['d']], text: 'Drill down: open the selection as the scope, or its callers and callees' },
  { keys: [['Backspace']], text: 'Select the parent · leave the scope' },
  { keys: [['Esc']], text: 'Leave “only related” · clear the selection · leave focus mode · close' },
  { keys: [['.']], text: 'Focus mode on the selection' },
  {
    keys: [
      ['Alt', '←'],
      ['Alt', '→'],
    ],
    text: 'Back · forward',
  },
  { keys: [['Double-click']], text: 'Expand or collapse a group · focus a symbol', plain: true },
  { section: 'View', keys: [['f'], ['0']], text: 'Zoom to fit everything · reset the view' },
  { keys: [['c']], text: 'Fit the selection' },
  { keys: [['+'], ['−'], ['Wheel']], text: 'Zoom (two fingers: pinch)' },
  { keys: [['Drag the background'], ['Arrows']], text: 'Pan (one finger on touch screens)', plain: true },
  { keys: [['o']], text: 'Only what the selection relates to' },
  { keys: [['m'], ['Shift', 'F']], text: 'Minimap · fullscreen' },
  { keys: [['b'], ['i']], text: 'Explorer · inspector' },
  { section: 'Arrange', keys: [['Drag a card']], text: 'Move it (a group by its header, with what it holds)', plain: true },
  { keys: [['Shift', 'click'], ['Ctrl', 'click']], text: 'Add to the selection, or take out' },
  { keys: [['Shift', 'drag']], text: 'Select the cards in a rectangle' },
  { keys: [['Ctrl', 'A']], text: 'Select every card' },
  { keys: [['Shift', 'Arrows']], text: 'Move the selection by 10 (canvas focused)' },
  { keys: [['l']], text: 'Lock or unlock the selection' },
  {
    keys: [
      ['Ctrl', 'Z'],
      ['Ctrl', 'Shift', 'Z'],
    ],
    text: 'Undo · redo a layout change',
  },
]

export function HelpModal({ onClose }: { onClose: () => void }) {
  const close = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null
    close.current?.focus()
    return () => prev?.focus?.({ preventScroll: true })
  }, [])
  return (
    <div className="modal modal-open" role="dialog" aria-modal="true" aria-label="Keyboard shortcuts">
      <div className="modal-box w-[min(540px,94vw)] border border-line p-0">
        <div className="flex h-10 items-center border-b border-line px-4">
          <h2 className="flex-1 text-sm font-semibold">Keyboard shortcuts</h2>
          <button
            ref={close}
            type="button"
            className="btn btn-ghost btn-sm btn-square"
            aria-label="Close"
            onClick={onClose}
          >
            <Icon name="x" />
          </button>
        </div>
        <dl className="scroll-thin grid max-h-[70vh] grid-cols-[max-content_1fr] items-center gap-x-5 gap-y-2 overflow-y-auto px-4 py-3 text-xs">
          {ROWS.map((r) => (
            <Fragment key={r.text}>
              {r.section && (
                <dt className="col-span-2 mt-1 text-[11px] font-semibold tracking-wide text-fg-3 uppercase first:mt-0">
                  {r.section}
                </dt>
              )}
              <dt className="flex items-center gap-1 whitespace-nowrap text-fg-2">
                {r.keys.map((alt, k) => (
                  <Fragment key={k}>
                    {k > 0 && <span className="text-fg-3">·</span>}
                    {alt.map((key) => (r.plain ? <span key={key}>{key}</span> : <Kbd key={key}>{key}</Kbd>))}
                  </Fragment>
                ))}
              </dt>
              <dd className="text-fg">{r.text}</dd>
            </Fragment>
          ))}
        </dl>
      </div>
      <div className="modal-backdrop" onMouseDown={onClose} />
    </div>
  )
}
