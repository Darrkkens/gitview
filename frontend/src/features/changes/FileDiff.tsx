// One changed file: a sticky header (status, path, diffstat, actions) and,
// when expanded, its diff. Long diffs are split in chunks mounted when they
// come near the viewport; absurdly long lines are cut with a "show all".

import {
  memo,
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type MouseEvent,
  type ReactNode,
} from 'react'
import { ChromaSpans } from '../../components/Code'
import { CopyButton } from '../../components/CopyButton'
import { Icon } from '../../components/Icon'
import { toast } from '../../components/Toast'
import { cx } from '../../lib/format'
import { routeHref } from '../../lib/router'
import { LONG_LINE, chunks, num, findNewLine, hunkText, maxLineNumber, spansLength, truncateSpans } from './diff'
import { useNearViewport } from './lazy'
import { DiffStat, StatusLetterBadge } from './parts'
import type { DiffChunk, DiffFile, DiffHunk, DiffLine } from './types'

const SIGN = { add: '+', del: '-', ctx: ' ' } as const

export interface Target {
  line: number
  /** Scroll to it (deep link) or only mark it (line number clicked). */
  scroll: boolean
  nonce: number
}

interface FileDiffProps {
  file: DiffFile
  open: boolean
  current: boolean
  /** Mount every chunk now (printing). */
  mountAll: boolean
  /** Deep-link target in this file, if any. */
  target?: Target
  /** Scroll the header into view (file link without a line). */
  reveal?: number
  onToggle: (id: string, open?: boolean) => void
  onPickLine: (file: DiffFile, line: number) => void
}

export const FileDiff = memo(function FileDiff({
  file,
  open,
  current,
  mountAll,
  target,
  reveal,
  onToggle,
  onPickLine,
}: FileDiffProps) {
  const ref = useRef<HTMLElement>(null)

  // A file link without a line: show the header at the top.
  useLayoutEffect(() => {
    if (reveal) ref.current?.scrollIntoView({ block: 'start' })
  }, [reveal])

  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onToggle(file.id)
    }
  }
  const onHeadClick = (e: MouseEvent) => {
    if ((e.target as HTMLElement).closest('[data-nohead]')) return
    onToggle(file.id)
  }
  const name = file.path.slice(file.path.lastIndexOf('/') + 1)
  const dir = file.path.slice(0, file.path.length - name.length)

  return (
    <section
      ref={ref}
      id={'gd-' + file.id}
      data-file={file.id}
      data-hover-file={file.path.endsWith('.go') ? file.path : undefined}
      className={cx(
        'gd-file mb-3 scroll-mt-[var(--gd-top)] rounded-box border border-line bg-code print:break-inside-auto',
        current && 'gd-current',
      )}
    >
      <div
        className={cx(
          'gd-head sticky z-[5] flex min-h-9 cursor-pointer items-center gap-2 border-line bg-surface-2 px-2 py-1 text-sm',
          open ? 'rounded-t-box border-b' : 'rounded-box',
        )}
        onClick={onHeadClick}
      >
        <span
          role="button"
          tabIndex={0}
          aria-expanded={open}
          aria-label={(open ? 'Collapse ' : 'Expand ') + file.path}
          onKeyDown={onKey}
          data-gd-toggle={file.id}
          className="flex min-w-0 flex-1 items-center gap-2 rounded-field outline-offset-2"
        >
          <Icon
            name="chevronRight"
            className={cx('size-3.5 shrink-0 text-fg-3 transition-transform duration-100', open && 'rotate-90')}
          />
          <StatusLetterBadge status={file.status} name={file.statusName} />
          <span className="min-w-0 truncate font-mono text-[12.5px]" title={file.path}>
            <span className="text-fg-2">{dir}</span>
            <span className="font-semibold text-fg">{name}</span>
          </span>
          {file.oldPath && (
            <span className="badge shrink-0 badge-ghost badge-sm font-mono" title={'Renamed from ' + file.oldPath}>
              from {file.oldPath}
            </span>
          )}
          {file.omitted && <span className="badge shrink-0 badge-soft badge-sm badge-warning">content omitted</span>}
          {file.binary && <span className="badge shrink-0 badge-ghost badge-sm">binary</span>}
          {!open && !file.omitted && !file.binary && file.lineCount > 0 && (
            <span className="hidden shrink-0 text-xs text-fg-3 sm:inline">{num(file.lineCount)} lines</span>
          )}
        </span>
        <DiffStat added={file.added} deleted={file.deleted} className="shrink-0 max-sm:hidden" />
        <span className="flex shrink-0 items-center gap-0.5 print:hidden" data-nohead>
          <CopyButton
            text={file.path}
            label="Copy path"
            title="Copy file path"
            className="max-md:btn-square max-md:[&>span]:hidden"
          />
          <a
            className="btn btn-square btn-ghost btn-xs font-mono"
            href={routeHref('changes', { file: file.path })}
            title="Link to this file"
            aria-label={'Link to ' + file.path}
          >
            #
          </a>
        </span>
      </div>
      {open && <FileBody file={file} mountAll={mountAll} target={target} onPickLine={onPickLine} />}
    </section>
  )
})

function Note({ children }: { children: ReactNode }) {
  return <p className="px-4 py-3 text-sm text-fg-2">{children}</p>
}

function FileBody({
  file,
  mountAll,
  target,
  onPickLine,
}: {
  file: DiffFile
  mountAll: boolean
  target?: Target
  onPickLine: (file: DiffFile, line: number) => void
}) {
  if (file.omitted)
    return (
      <Note>
        Content omitted: this file may contain secrets. Export again with sensitive content explicitly included to show
        it.
      </Note>
    )
  if (file.binary) return <Note>Binary file not shown.</Note>
  if (!file.hunks.length) return <Note>No content changes (mode change, rename or empty file).</Note>
  return <DiffTable file={file} mountAll={mountAll} target={target} onPickLine={onPickLine} />
}

function DiffTable({
  file,
  mountAll,
  target,
  onPickLine,
}: {
  file: DiffFile
  mountAll: boolean
  target?: Target
  onPickLine: (file: DiffFile, line: number) => void
}) {
  const parts = useMemo(() => chunks(file), [file])
  const digits = useMemo(() => Math.max(String(maxLineNumber(file)).length, 2), [file])
  const tableRef = useRef<HTMLTableElement>(null)

  // The chunk holding the target line is mounted at once.
  const targetChunk = useMemo(() => {
    if (!target) return -1
    const at = findNewLine(file, target.line)
    if (!at) return -1
    return parts.findIndex((c) => c.rows.some((r) => r.hunk === at.hunk && r.line === at.line))
  }, [file, parts, target])

  useLayoutEffect(() => {
    if (!target || !target.scroll) return
    const tr = tableRef.current?.querySelector<HTMLElement>(`tr[data-new="${target.line}"]`)
    if (!tr) {
      tableRef.current?.closest('section')?.scrollIntoView({ block: 'start' })
      toast(`Line ${target.line} of ${file.path} is not part of the diff.`, { tone: 'warning' })
      return
    }
    tr.scrollIntoView({ block: 'center' })
    tr.classList.remove('gd-flash')
    void tr.offsetWidth // restart the animation
    tr.classList.add('gd-flash')
  }, [target, file.path])

  const pick = useCallback((line: number) => onPickLine(file, line), [file, onPickLine])
  const longLines = useMemo(() => {
    let n = 0
    for (const h of file.hunks) for (const l of h.lines) if (spansLength(l.spans) > LONG_LINE) n++
    return n
  }, [file])
  const [full, setFull] = useState(false)
  return (
    <>
      {longLines > 0 && (
        <div className="flex flex-wrap items-center gap-x-2 border-b border-line bg-surface-2/60 px-3 py-1 text-xs text-fg-2 print:hidden">
          <Icon name="info" className="size-3.5 text-info" />
          <span>
            {longLines === 1 ? 'One line is' : num(longLines) + ' lines are'} longer than {num(LONG_LINE)} characters
            {full ? ' and shown wrapped.' : ' and cut at that length.'}
          </span>
          <button type="button" className="btn btn-ghost btn-xs text-accent-text" onClick={() => setFull(!full)}>
            {full ? 'Cut long lines' : 'Show in full'}
          </button>
        </div>
      )}
      <div className="gd-scroll scroll-thin overflow-x-auto rounded-b-box" aria-label={'Diff of ' + file.path}>
        <table ref={tableRef} className="gd-table font-mono" style={{ '--gd-d': digits } as CSSProperties}>
          <colgroup>
            <col />
            <col />
            <col />
            <col />
          </colgroup>
          {parts.map((c, i) => (
            <Chunk
              key={i}
              file={file}
              chunk={c}
              force={mountAll || i === targetChunk}
              targetLine={i === targetChunk ? target?.line : undefined}
              full={full}
              onPick={pick}
            />
          ))}
        </table>
      </div>
    </>
  )
}

const Chunk = memo(function Chunk({
  file,
  chunk,
  force,
  targetLine,
  full,
  onPick,
}: {
  file: DiffFile
  chunk: DiffChunk
  force: boolean
  targetLine?: number
  full: boolean
  onPick: (line: number) => void
}) {
  const ref = useRef<HTMLTableSectionElement>(null)
  const shown = useNearViewport(ref, force)
  if (!shown) {
    return (
      <tbody ref={ref}>
        <tr>
          <td colSpan={4} className="gd-ph" style={{ height: chunk.height }} />
        </tr>
      </tbody>
    )
  }
  const inChunk = targetLine ?? 0
  return (
    <tbody>
      {chunk.rows.map((r) => {
        const h = file.hunks[r.hunk]
        if (r.line < 0) return <HunkRow key={'h' + r.hunk} hunk={h} />
        const l = h.lines[r.line]
        return (
          <LineRow
            key={r.hunk + ':' + r.line}
            line={l}
            target={inChunk !== 0 && l.new === inChunk}
            full={full}
            onPick={onPick}
          />
        )
      })}
    </tbody>
  )
})

function HunkRow({ hunk }: { hunk: DiffHunk }) {
  return (
    <tr className="gd-hh">
      <td className="gd-ln gd-o" />
      <td className="gd-ln gd-n" />
      <td colSpan={2} className="gd-hc">
        <span className="gd-range">{hunk.range}</span>
        {hunk.section && <span className="gd-sec">{hunk.section}</span>}
        <span className="gd-copy inline-block">
          <CopyButton
            text={() => hunkText(hunk)}
            label="Copy"
            title="Copy this hunk (unified diff)"
            className="h-5 min-h-5 px-1.5 font-sans"
          />
        </span>
      </td>
    </tr>
  )
}

const LineRow = memo(function LineRow({
  line,
  target,
  full,
  onPick,
}: {
  line: DiffLine
  target: boolean
  full: boolean
  onPick: (line: number) => void
}) {
  const spans = line.spans ?? []
  const len = spansLength(spans)
  return (
    <tr className={cx('gd-' + line.kind, target && 'gd-target')} data-new={line.new || undefined}>
      <td className="gd-ln gd-o">{line.old || ''}</td>
      <td
        className={cx('gd-ln gd-n', line.new > 0 && 'gd-link')}
        onClick={line.new > 0 ? () => onPick(line.new) : undefined}
        title={line.new > 0 ? 'Link to line ' + line.new : undefined}
      >
        {line.new || ''}
      </td>
      <td className="gd-sg">{SIGN[line.kind]}</td>
      {len > LONG_LINE ? (
        <LongLine key={String(full)} spans={spans} len={len} full={full} line={line.new} />
      ) : (
        <td className="gd-cd" data-hl={line.new || undefined}>
          {spans.length ? <ChromaSpans spans={spans} /> : ''}
        </td>
      )}
    </tr>
  )
})

function LongLine({ spans, len, full, line }: { spans: DiffLine['spans']; len: number; full: boolean; line: number }) {
  const [all, setAll] = useState(full)
  const cut = useMemo(() => (all ? spans : truncateSpans(spans, LONG_LINE)), [all, spans])
  return (
    <td className={cx('gd-cd', all && 'gd-wrap')} data-hl={line || undefined}>
      <ChromaSpans spans={cut} />
      <button
        type="button"
        className="gd-more"
        onClick={() => setAll(!all)}
        title={all ? 'Show the beginning of the line only' : 'Show the whole line (wrapped)'}
      >
        {all ? 'show less' : `… ${num(len - LONG_LINE)} more characters`}
      </button>
    </td>
  )
}
