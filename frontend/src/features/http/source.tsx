// Source of a graph node: the embedded Go files (offline pages exported with
// their source, or `gitview serve`), or, on a security report that embeds
// only the excerpts its findings point to, those excerpts.

import { useMemo, useState } from 'react'
import { CodeView } from '../../components/Code'
import { CopyButton } from '../../components/CopyButton'
import { isLive } from '../../lib/envelope'
import { cx } from '../../lib/format'
import { tokenLines, tokenize, type Token } from '../../lib/go'
import { excerptParts, hasExcerpt, spanFor, useSourceFiles, type Snippets } from './sourceData'
import type { GNode, SourcesInfo } from './types'

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

/**
 * Excerpts renders the embedded excerpts of a file overlapping [from, to],
 * marking the given lines. Returns null when none is embedded.
 */
export function Excerpts({
  snippets,
  file,
  from,
  to,
  marks,
  className,
  maxHeight,
}: {
  snippets: Snippets | undefined
  file: string
  from: number
  to: number
  marks?: Set<number>
  className?: string
  maxHeight?: string
}) {
  const parts = useMemo(() => excerptParts(snippets, file, from, to), [snippets, file, from, to])
  const rows = useMemo(() => parts.map((x) => tokenLines(tokenize(x.lines.join('\n')))), [parts])
  if (!parts.length) return null
  const first = marks && marks.size ? Math.min(...marks) : undefined
  return (
    <div
      className={cx(
        'scroll-thin overflow-auto rounded-box border border-line bg-code font-mono text-xs leading-5',
        className,
      )}
      style={maxHeight ? { maxHeight } : undefined}
      ref={(el) => {
        if (!el || first === undefined) return
        const tr = el.querySelector<HTMLElement>(`tr[data-line="${first}"]`)
        if (tr) el.scrollTop = Math.max(0, tr.offsetTop - 60)
      }}
    >
      <table className="min-w-full border-collapse">
        <tbody>
          {parts.map((x, k) => {
            const out = []
            if (k > 0)
              out.push(
                <tr key={'gap' + k}>
                  <td colSpan={2} className="bg-surface-2 text-center text-fg-3">
                    ⋯
                  </td>
                </tr>,
              )
            rows[k].forEach((toks, i) => {
              const n = x.from + i
              if (n < from || n > to) return
              const mark = !!marks?.has(n)
              out.push(
                <tr key={k + ':' + n} data-line={n} className={mark ? 'bg-s-review/14' : undefined}>
                  <td
                    className={cx(
                      'sticky left-0 w-px bg-code pr-3 pl-3 text-right align-top text-[var(--code-gutter)] select-none',
                      mark && 'text-fg shadow-[inset_2px_0_0_var(--s-review)]',
                    )}
                  >
                    {n}
                  </td>
                  <td className="pr-4 align-top whitespace-pre">{toks.length ? <Tokens tokens={toks} /> : ' '}</td>
                </tr>,
              )
            })
            return out
          })}
        </tbody>
      </table>
    </div>
  )
}

function Unavailable({ children }: { children: string }) {
  return (
    <div className="rounded-box border border-dashed border-line px-3 py-4 text-center text-xs text-fg-2">
      <div className="mb-1 font-semibold text-fg">Source not available</div>
      {children}
    </div>
  )
}

/** NodeSource shows the declaration of a node (or the code around a call site). */
export function NodeSource({
  n,
  info,
  snippets,
  maxHeight = '28rem',
}: {
  n: GNode
  info?: SourcesInfo
  snippets?: Snippets
  maxHeight?: string
}) {
  const canLoad = !!info?.embedded || isLive()
  const { files, error, loading } = useSourceFiles(canLoad && !n.ext && !!n.file)
  const [whole, setWhole] = useState(false)
  if (n.ext)
    return (
      <Unavailable>Declared outside the analysed modules; GitView only embeds the repository's own files.</Unavailable>
    )
  if (!n.file) return <Unavailable>This element has no source location.</Unavailable>
  const text = files?.[n.file]
  if (canLoad && loading)
    return (
      <div className="space-y-2">
        <div className="skeleton h-4 w-full" />
        <div className="skeleton h-4 w-2/3" />
      </div>
    )
  if (text == null) {
    const from = n.line || 1,
      to = n.end || (n.line ? n.line + 20 : 40)
    if (hasExcerpt(snippets, n.file, from, to))
      return (
        <div className="space-y-1.5">
          <div className="flex items-center gap-2 text-[11px]">
            <span className="min-w-0 flex-1 truncate font-mono text-fg-2" title={n.file}>
              {n.file}
            </span>
            <span className="text-fg-3">excerpts embedded by the security report</span>
          </div>
          <Excerpts
            snippets={snippets}
            file={n.file}
            from={from}
            to={to}
            marks={new Set(n.line ? [n.line] : [])}
            maxHeight={maxHeight}
          />
        </div>
      )
    if (error) return <Unavailable>{error}</Unavailable>
    if (files) {
      const skipped = (info?.skipped ?? []).find((s) => s.startsWith(n.file + ':'))
      return (
        <Unavailable>
          {skipped
            ? 'Left out of the page: ' + skipped.slice(n.file.length + 2) + '.'
            : 'This file is not in the embedded source.'}
        </Unavailable>
      )
    }
    return (
      <Unavailable>
        {info?.disabled
          ? 'The page was exported without whole source files' +
            (snippets ? "; this element is not near any finding's evidence." : ' (--include-source=false).')
          : 'No source file could be embedded for this repository.'}
      </Unavailable>
    )
  }
  let [from, to] = spanFor(n, text)
  const site = n.type === 'db' || n.type === 'unresolved' || n.type === 'route'
  if (whole) {
    from = 1
    to = text.split('\n').length
  }
  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-1">
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-fg-2" title={n.file}>
          {n.file}:{from}
          {to > from ? '–' + to : ''}
        </span>
        {n.type !== 'file' && (
          <button type="button" className="btn btn-ghost btn-xs" onClick={() => setWhole(!whole)}>
            {whole ? 'Show declaration' : 'Whole file'}
          </button>
        )}
        <CopyButton
          label="Code"
          title="Copy these lines"
          text={() =>
            text
              .split('\n')
              .slice(from - 1, to)
              .join('\n')
          }
        />
      </div>
      <CodeView
        path={n.file}
        hoverFile={n.file}
        text={text}
        from={from}
        to={to}
        context={site && !whole ? 4 : 0}
        target={n.line || undefined}
        maxHeight={maxHeight}
      />
    </div>
  )
}
