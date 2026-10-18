import { useState } from 'react'
import { copyText } from '../lib/browser'
import { cx } from '../lib/format'
import { Icon } from './Icon'
import { toast } from './Toast'

/** CopyButton copies text (computed on click) to the clipboard. */
export function CopyButton({
  text,
  label,
  title,
  className,
}: {
  text: string | (() => string)
  label?: string
  title?: string
  className?: string
}) {
  const [done, setDone] = useState(false)
  return (
    <button
      type="button"
      className={cx('btn btn-ghost btn-xs gap-1', className)}
      title={title ?? 'Copy to clipboard'}
      onClick={async () => {
        const ok = await copyText(typeof text === 'function' ? text() : text)
        if (ok) {
          setDone(true)
          setTimeout(() => setDone(false), 1200)
        } else {
          toast('Could not copy: the browser blocked the clipboard.', { tone: 'error' })
        }
      }}
    >
      <Icon name={done ? 'check' : 'copy'} className="size-3.5" />
      {label && <span>{done ? 'Copied' : label}</span>}
    </button>
  )
}
