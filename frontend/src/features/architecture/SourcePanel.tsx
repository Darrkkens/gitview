// The Source tab: the declaration of an element in the Go files the page
// embeds (decoded on first use), or the whole file; a security report that
// embeds only excerpts shows the ones near the element.

import { useEffect, useMemo, useState } from 'react'
import { CodeView } from '../../components/Code'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/States'
import { cx } from '../../lib/format'
import { tokenize, tokenLines } from '../../lib/go'
import { loadSources, type SourceFiles } from '../../lib/sources'
import type { GNode, SecuritySnippet, SourcesInfo } from './types'
import { TYPE } from './vocab'

type Load =
  { status: 'loading' } | { status: 'ready'; files: SourceFiles | null } | { status: 'error'; message: string }

/** useSources decodes the page's Go files on first use (lib/sources caches them). */
function useSources(): Load {
  const [s, setS] = useState<Load>({ status: 'loading' })
  useEffect(() => {
    let live = true
    loadSources().then(
      (files) => {
        if (live) setS({ status: 'ready', files })
      },
      (err: Error) => live && setS({ status: 'error', message: err.message }),
    )
    return () => {
      live = false
    }
  }, [])
  return s
}

/** spanFor: the lines [from, to] of a declaration (1-based, inclusive). */
function spanFor(n: GNode, text: string): [number, number] {
  const rows = text.split('\n')
  const total = rows.length
  if (n.type === 'file') return [1, total]
  if (!n.line) return [1, Math.min(total, 40)]
  if (n.end && n.end >= n.line) return [n.line, n.end]
  if (TYPE[n.type]) {
    // types carry only their first line: follow brackets to the end.
    let depth = 0,
      seen = false
    for (let k = n.line - 1; k < rows.length && k < n.line + 400; k++) {
      for (const ch of rows[k].replace(/\/\/.*$/, '').replace(/"(?:[^"\\]|\\.)*"|`[^`]*`/g, '')) {
        if (ch === '{' || ch === '(') {
          depth++
          seen = true
        } else if (ch === '}' || ch === ')') depth--
      }
      if (depth <= 0 && (seen || k > n.line - 1 || !/[{(]\s*$/.test(rows[k]))) return [n.line, k + 1]
    }
    return [n.line, n.line]
  }
  return [n.line, n.line]
}

function Unavailable({ children }: { children: string }) {
  return (
    <EmptyState icon="code" title="Source not available">
      {children}
    </EmptyState>
  )
}

export function SourcePanel({
  n,
  info,
  snippets,
}: {
  n: GNode
  info: SourcesInfo | null | undefined
  snippets: Record<string, SecuritySnippet[]> | null
}) {
  if (n.ext)
    return (
      <Unavailable>Declared outside the analysed modules; GitView only embeds the repository's own files.</Unavailable>
    )
  if (!n.file) return <Unavailable>This element has no source location.</Unavailable>
  if (!info?.embedded) {
    // A security report embeds only the excerpts its findings point to.
    const from = n.line || 1,
      to = n.end || (n.line ? n.line + 20 : 40)
    const parts = (snippets?.[n.file] ?? []).filter((x) => x.to >= from && x.from <= to)
    if (parts.length) return <Excerpts file={n.file} parts={parts} from={from} to={to} mark={n.line} />
    return (
      <Unavailable>
        {info?.disabled
          ? 'The page was exported without whole source files' +
            (snippets ? "; this element is not near any finding's evidence." : ' (--include-source=false).')
          : 'No source file could be embedded for this repository.'}
      </Unavailable>
    )
  }
  return <EmbeddedSource n={n} info={info} />
}

function EmbeddedSource({ n, info }: { n: GNode; info: SourcesInfo }) {
  const s = useSources()
  const [whole, setWhole] = useState(false)
  if (s.status === 'loading')
    return (
      <div className="flex flex-col gap-2 p-1">
        <div className="skeleton h-4 w-full" />
        <div className="skeleton h-4 w-3/4" />
        <div className="skeleton h-4 w-5/6" />
      </div>
    )
  if (s.status === 'error') return <Unavailable>{s.message}</Unavailable>
  const text = s.files?.[n.file]
  if (text === undefined || text === null) {
    const skipped = (info.skipped ?? []).find((x) => x.startsWith(n.file + ':'))
    return (
      <Unavailable>
        {skipped
          ? 'Left out of the page: ' + skipped.slice(n.file.length + 2) + '.'
          : 'This file is not in the embedded source.'}
      </Unavailable>
    )
  }
  let [from, to] = spanFor(n, text)
  const site = n.type === 'db' || n.type === 'unresolved' || n.type === 'route'
  if (whole) {
    from = 1
    to = text.split('\n').length
  }
  const ctx = site && !whole ? 4 : 0
  return (
    <div className="flex min-h-0 flex-col gap-2">
      <div className="flex items-center gap-1">
        <span className="min-w-0 flex-1 truncate font-mono text-xs text-fg-2" title={n.file}>
          {n.file + ':' + from + (to > from ? '–' + to : '')}
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
        key={n.id + (whole ? ':whole' : '')}
        path={n.file}
        text={text}
        from={from}
        to={to}
        context={ctx}
        target={whole || site ? n.line || undefined : undefined}
        maxHeight={whole ? '70vh' : undefined}
      />
    </div>
  )
}

/** Excerpts: the code a security report embeds around its evidence. */
function Excerpts({
  file,
  parts,
  from,
  to,
  mark,
}: {
  file: string
  parts: SecuritySnippet[]
  from: number
  to: number
  mark: number
}) {
  const rows = useMemo(
    () =>
      parts.map((x) => {
        const toks = tokenLines(tokenize(x.lines.join('\n')))
        return toks.map((t, i) => ({ n: x.from + i, toks: t })).filter((r) => r.n >= from && r.n <= to)
      }),
    [parts, from, to],
  )
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate font-mono text-xs text-fg-2">{file}</span>
        <span className="text-xs text-fg-3">excerpts embedded by the security report</span>
      </div>
      <div className="scroll-thin overflow-auto rounded-box border border-line bg-code font-mono text-xs leading-5">
        <table className="min-w-full border-collapse">
          <tbody>
            {rows.map((part, k) => [
              k > 0 && (
                <tr key={'gap' + k}>
                  <td colSpan={2} className="px-3 text-center text-fg-3 select-none">
                    ⋯
                  </td>
                </tr>
              ),
              ...part.map((r) => (
                <tr key={k + ':' + r.n} className={cx(r.n === mark && 'bg-primary/15')}>
                  <td className="w-px pr-3 pl-3 text-right align-top text-[var(--code-gutter)] select-none">{r.n}</td>
                  <td className="pr-4 align-top whitespace-pre">
                    {r.toks.length
                      ? r.toks.map(([c, t], i) =>
                          c ? (
                            <span key={i} className={'tk-' + c}>
                              {t}
                            </span>
                          ) : (
                            t
                          ),
                        )
                      : ' '}
                  </td>
                </tr>
              )),
            ])}
          </tbody>
        </table>
      </div>
    </div>
  )
}
