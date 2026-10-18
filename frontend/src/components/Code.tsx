// Code views. CodeView shows lines of a Go file with numbers, a highlighted
// range and a target line; CodeInline highlights a short snippet (a
// signature); ChromaSpans renders spans already classified by the Go side
// (internal/highlight, any language). Code is always rendered as text.

import { memo, useEffect, useMemo, useRef } from 'react'
import { highlightFile, tokenize, type Token } from '../lib/go'
import { cx } from '../lib/format'

function Tokens({ tokens }: { tokens: Token[] }) {
  return (
    <>
      {tokens.map(([c, t], i) =>
        c ? (
          <span key={i} className={'tk-' + c}>
            {t}
          </span>
        ) : (
          t
        ),
      )}
    </>
  )
}

export interface CodeViewProps {
  /** Path of the file (the highlighting cache key). */
  path: string
  /** Whole file content. */
  text: string
  /** First and last line shown (1-based, inclusive); the whole file by default. */
  from?: number
  to?: number
  /** Extra lines shown around [from, to], which is then highlighted. */
  context?: number
  /** Lines marked as targets (scrolled into view, emphasised). */
  target?: number | number[]
  /** Extra per-line class (e.g. coverage). */
  lineClass?: (line: number) => string | undefined
  /** Called when a line number is clicked. */
  onLineClick?: (line: number) => void
  className?: string
  /** Max height of the scroll area (CSS value); none by default. */
  maxHeight?: string
}

export const CodeView = memo(function CodeView({
  path,
  text,
  from,
  to,
  context = 0,
  target,
  lineClass,
  onLineClick,
  className,
  maxHeight,
}: CodeViewProps) {
  const rows = useMemo(() => highlightFile(path, text), [path, text])
  const lo = Math.max(1, (from ?? 1) - context)
  const hi = Math.min(rows.length, (to ?? rows.length) + context)
  const targets = useMemo(() => new Set(target === undefined ? [] : Array.isArray(target) ? target : [target]), [target])
  const ref = useRef<HTMLDivElement>(null)
  const first = targets.size ? Math.min(...targets) : undefined

  useEffect(() => {
    const box = ref.current
    if (first === undefined || !box) return
    const tr = box.querySelector<HTMLElement>(`tr[data-line="${first}"]`)
    if (!tr) return
    if (box.scrollHeight > box.clientHeight) {
      // Scroll the code itself, never the panes around it.
      box.scrollTop = tr.offsetTop - box.clientHeight / 2 + tr.offsetHeight / 2
    } else {
      tr.scrollIntoView({ block: 'nearest' })
    }
  }, [first, path])

  const lines = []
  for (let k = lo; k <= hi; k++) {
    const inRange = context > 0 && from !== undefined && k >= from && k <= (to ?? from)
    lines.push(
      <tr
        key={k}
        data-line={k}
        className={cx(inRange && 'bg-[var(--code-line-hl)]', targets.has(k) && 'bg-primary/15', lineClass?.(k))}
      >
        <td
          className={cx(
            'sticky left-0 w-px bg-code pr-3 pl-3 text-right align-top text-[var(--code-gutter)] select-none',
            onLineClick && 'cursor-pointer hover:text-fg',
            targets.has(k) && 'text-primary',
          )}
          onClick={onLineClick ? () => onLineClick(k) : undefined}
        >
          {k}
        </td>
        <td className="pr-4 align-top whitespace-pre">
          {rows[k - 1]?.length ? <Tokens tokens={rows[k - 1]} /> : ' '}
        </td>
      </tr>,
    )
  }
  return (
    <div
      ref={ref}
      className={cx('scroll-thin overflow-auto rounded-box border border-line bg-code font-mono text-xs leading-5', className)}
      style={maxHeight ? { maxHeight } : undefined}
    >
      <table className="min-w-full border-collapse">
        <tbody>{lines}</tbody>
      </table>
    </div>
  )
})

/** CodeInline highlights a short Go snippet, such as a signature. */
export function CodeInline({ code, className, block }: { code: string; className?: string; block?: boolean }) {
  const tokens = useMemo(() => tokenize(code || ''), [code])
  const Tag = block ? 'pre' : 'code'
  return (
    <Tag className={cx('font-mono text-xs', block && 'scroll-thin overflow-x-auto whitespace-pre-wrap', className)}>
      <Tokens tokens={tokens} />
    </Tag>
  )
}

/** A span classified by Chroma: [text] or [text, class]. */
export type ChromaSpan = [string] | [string, string]

export function ChromaSpans({ spans }: { spans: ChromaSpan[] }) {
  return (
    <span className="chroma">
      {spans.map(([t, c], i) =>
        c ? (
          <span key={i} className={c}>
            {t}
          </span>
        ) : (
          t
        ),
      )}
    </span>
  )
}
