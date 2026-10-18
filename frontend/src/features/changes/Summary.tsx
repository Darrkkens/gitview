// Overview of the session report: project, branch, base commit, scope,
// totals, test status, warnings and files whose content was omitted.

import type { ReactNode } from 'react'
import { Icon } from '../../components/Icon'
import { cx, dateTime } from '../../lib/format'
import { routeHref } from '../../lib/router'
import { num, testsLine } from './diff'
import type { ReportSection } from './types'

const TONE_TEXT = { success: 'text-success', error: 'text-error', warning: 'text-warning', muted: 'text-fg-2' } as const

function Meta({
  label,
  children,
  title,
  optional,
}: {
  label: string
  children: ReactNode
  title?: string
  optional?: boolean
}) {
  return (
    <div className={cx('flex min-w-0 gap-1.5', optional && 'max-md:hidden')} title={title}>
      <dt className="shrink-0 text-fg-3">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </div>
  )
}

export function Summary({ report, onOpenFile }: { report: ReportSection; onOpenFile: (path: string) => void }) {
  const r = report
  const tl = testsLine(r.tests)
  const n = r.files.length
  const base = r.base
  return (
    <header className="px-4 pt-4 pb-3 sm:px-6" id="overview">
      <div className="text-xs text-fg-3">Changes in the working tree</div>
      <h1 className="mt-0.5 text-lg font-semibold tracking-tight break-words" title={r.root}>
        {r.project}
      </h1>
      <dl className="mt-1.5 flex flex-wrap gap-x-5 gap-y-1 text-[12.5px]">
        {r.branch && (
          <Meta label="Branch">
            <span className="font-mono">{r.branch}</span>
          </Meta>
        )}
        {base && (
          <Meta label="Base" title={`${base.hash}\n${base.author}${base.date ? ' · ' + dateTime(base.date) : ''}`}>
            <span className="font-mono">{base.short}</span>{' '}
            <span className="text-fg-2 max-md:hidden">{base.subject}</span>
          </Meta>
        )}
        <Meta label="Compared">{base ? 'working tree (uncommitted)' : 'working tree · no commits yet'}</Meta>
        <Meta label="Generated">
          <time dateTime={r.generated}>{dateTime(r.generated)}</time>
        </Meta>
        {r.scope && (
          <Meta label="Scope" optional>
            {r.scope}
          </Meta>
        )}
        {r.query && (
          <Meta label="Search" optional>
            <span className="font-mono">{r.query}</span>
          </Meta>
        )}
      </dl>
      <p className="mt-3 flex flex-wrap items-center gap-x-2 text-[13px] text-fg-2">
        <span>
          <b className="font-semibold text-fg">{num(n)}</b> {n === 1 ? 'file' : 'files'} changed
        </span>
        <span className="text-fg-3">·</span>
        <span className="text-success">
          <b className="font-semibold">{num(r.added)}</b> {r.added === 1 ? 'addition' : 'additions'}
        </span>
        <span className="text-fg-3">·</span>
        <span className="text-error">
          <b className="font-semibold">{num(r.deleted)}</b> {r.deleted === 1 ? 'deletion' : 'deletions'}
        </span>
        <span className="text-fg-3">·</span>
        {r.tests ? (
          <a href={routeHref('tests')} className={cx('link link-hover', TONE_TEXT[tl.tone])}>
            {tl.text}
          </a>
        ) : (
          <span className={TONE_TEXT[tl.tone]}>{tl.text}</span>
        )}
      </p>
      {((r.warnings?.length ?? 0) > 0 || (r.omitted?.length ?? 0) > 0) && (
        <div role="alert" className="alert mt-3 alert-soft alert-warning items-start py-2 text-[12.5px]">
          <Icon name="warning" className="mt-0.5 size-4" />
          <div className="min-w-0">
            <h3 className="font-semibold">Warnings</h3>
            <ul className="mt-0.5 space-y-0.5">
              {r.warnings?.map((w, i) => (
                <li key={'w' + i} className="break-words">
                  {w}
                </li>
              ))}
              {r.omitted?.map((p) => (
                <li key={'o' + p} className="break-words">
                  Content of{' '}
                  <button type="button" className="link font-mono" onClick={() => onOpenFile(p)}>
                    {p}
                  </button>{' '}
                  was omitted because it may contain secrets.
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}
    </header>
  )
}
