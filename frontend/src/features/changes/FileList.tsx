// The list of changed files (the report's overview list): status, name,
// directory and diffstat; a click opens the file's diff.

import { memo, useEffect, useRef } from 'react'
import { PaneHeader } from '../../components/Panes'
import { cx } from '../../lib/format'
import { num } from './diff'
import { DiffStat, StatusLetterBadge } from './parts'
import type { DiffFile } from './types'

export const FileList = memo(function FileList({
  files,
  total,
  current,
  onOpen,
}: {
  files: DiffFile[]
  total: number
  current: string | null
  onOpen: (f: DiffFile) => void
}) {
  const navRef = useRef<HTMLElement>(null)
  // Keep the current file visible in the list as the diff scrolls.
  useEffect(() => {
    navRef.current?.querySelector('[aria-current]')?.scrollIntoView({ block: 'nearest' })
  }, [current])
  return (
    <>
      <PaneHeader
        title={
          <>
            Files{' '}
            <span className="ml-1 font-normal text-fg-3 normal-case">
              {files.length === total ? num(total) : `${num(files.length)} of ${num(total)}`}
            </span>
          </>
        }
      />
      <nav ref={navRef} className="scroll-thin min-h-0 flex-1 overflow-y-auto py-1" aria-label="Changed files">
        {files.length === 0 && (
          <p className="px-3 py-2 text-xs text-fg-3">{total ? 'No files match the filter.' : 'No changes.'}</p>
        )}
        <ul>
          {files.map((f) => {
            const i = f.path.lastIndexOf('/')
            const name = f.path.slice(i + 1)
            const dir = i >= 0 ? f.path.slice(0, i) : ''
            const active = f.id === current
            return (
              <li key={f.id}>
                <button
                  type="button"
                  onClick={() => onOpen(f)}
                  aria-current={active || undefined}
                  title={f.path + (f.oldPath ? '\nrenamed from ' + f.oldPath : '')}
                  className={cx(
                    'grid w-full grid-cols-[16px_minmax(0,1fr)_auto] items-center gap-x-2 px-3 py-1 text-left hover:bg-surface-hover',
                    active && 'bg-accent-weak shadow-[inset_2px_0_0_var(--color-primary)]',
                  )}
                >
                  <StatusLetterBadge status={f.status} name={f.statusName} />
                  <span className="min-w-0">
                    <span className="block truncate text-[12.5px] font-semibold text-fg">{name}</span>
                    {(dir || f.oldPath) && (
                      <span className="block truncate text-left text-[11.5px] text-fg-3 [direction:rtl]">
                        <bdi>{f.oldPath ? (dir ? dir + ' · ' : '') + 'from ' + f.oldPath : dir}</bdi>
                      </span>
                    )}
                  </span>
                  <DiffStat added={f.added} deleted={f.deleted} bar={false} className="text-[11px]" />
                </button>
              </li>
            )
          })}
        </ul>
      </nav>
    </>
  )
})
