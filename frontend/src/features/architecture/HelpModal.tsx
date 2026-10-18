// Keyboard shortcuts of the Architecture view (?).

import { Fragment, useEffect, useRef } from 'react'
import { Icon } from '../../components/Icon'
import { Kbd } from './ui'

// Each shortcut: alternatives separated by "·", each a sequence of keys.
const ROWS: { keys: string[][]; text: string; plain?: boolean }[] = [
  { keys: [['Ctrl', 'K'], ['/']], text: 'Search everything' },
  { keys: [['2'], ['3']], text: 'HTTP view · Security view' },
  { keys: [['Enter']], text: 'Expand or collapse the selection' },
  { keys: [['Backspace']], text: 'Select the parent · leave the scope' },
  { keys: [['Esc']], text: 'Clear the selection · leave focus mode · close' },
  { keys: [['.']], text: 'Focus mode on the selection' },
  { keys: [['f'], ['+'], ['−']], text: 'Fit view · zoom' },
  { keys: [['Arrows']], text: 'Pan the canvas (when it has the focus)', plain: true },
  { keys: [['m'], ['Shift', 'F']], text: 'Minimap · fullscreen' },
  { keys: [['b'], ['i']], text: 'Explorer · inspector' },
  {
    keys: [
      ['Alt', '←'],
      ['Alt', '→'],
    ],
    text: 'Back · forward',
  },
  { keys: [['Double-click']], text: 'Expand or collapse a group · focus a symbol', plain: true },
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
      <div className="modal-box w-[min(480px,94vw)] border border-line p-0">
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
        <dl className="grid grid-cols-[max-content_1fr] items-center gap-x-5 gap-y-2 px-4 py-3 text-xs">
          {ROWS.map((r) => (
            <Fragment key={r.text}>
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
