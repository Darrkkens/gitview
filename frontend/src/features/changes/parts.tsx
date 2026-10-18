// Small pieces shared by the Changes view's list, headers and summary.

import { cx } from '../../lib/format'
import { diffstatBar, num } from './diff'

const STATUS_CLASS: Record<string, string> = {
  A: 'text-success',
  D: 'text-error',
  M: 'text-primary',
  R: 'text-fg-2',
}

/** StatusLetterBadge is the A / M / D / R letter of a changed file. */
export function StatusLetterBadge({ status, name }: { status: string; name: string }) {
  return (
    <span
      className={cx(
        'w-4 shrink-0 text-center font-mono text-[11.5px] leading-[18px] font-bold',
        STATUS_CLASS[status] ?? 'text-fg-2',
      )}
      title={name}
      aria-label={name}
    >
      {status}
    </span>
  )
}

/** DiffStat shows "+a −d" and a five-block bar. */
export function DiffStat({
  added,
  deleted,
  className,
  bar = true,
}: {
  added: number
  deleted: number
  className?: string
  bar?: boolean
}) {
  return (
    <span
      className={cx('inline-flex items-center gap-1.5 font-mono text-xs whitespace-nowrap', className)}
      title={`${num(added)} addition${added === 1 ? '' : 's'}, ${num(deleted)} deletion${deleted === 1 ? '' : 's'}`}
    >
      <span className="text-success">+{num(added)}</span>
      <span className="text-error">−{num(deleted)}</span>
      {bar && (
        <span className="inline-flex gap-px" aria-hidden="true">
          {diffstatBar(added, deleted).map((b, i) => (
            <i
              key={i}
              className={cx(
                'block size-[7px] rounded-[1px]',
                b === 'add' ? 'bg-success' : b === 'del' ? 'bg-error' : 'bg-line-strong',
              )}
            />
          ))}
        </span>
      )}
    </span>
  )
}
