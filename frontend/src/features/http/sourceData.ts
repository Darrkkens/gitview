// Source helpers: the excerpts a security report embeds, the decoded Go
// files of the page and the span of a declaration.

import { useEffect, useState } from 'react'
import { loadSources, type SourceFiles } from '../../lib/sources'
import { TYPE } from './graph'
import type { GNode } from './types'

/** An excerpt of a file embedded by the security report. */
export interface Excerpt {
  from: number
  to: number
  lines: string[]
}

export type Snippets = Record<string, Excerpt[]>

export function useSourceFiles(enabled: boolean): { files: SourceFiles | null; error: string; loading: boolean } {
  const [state, setState] = useState<{ files: SourceFiles | null; error: string; loading: boolean }>({
    files: null,
    error: '',
    loading: enabled,
  })
  useEffect(() => {
    if (!enabled) return
    let live = true
    loadSources().then(
      (files) => live && setState({ files, error: '', loading: false }),
      (e: Error) => live && setState({ files: null, error: e.message, loading: false }),
    )
    return () => {
      live = false
    }
  }, [enabled])
  return state
}

/** spanFor: the lines [from, to] of a declaration (1-based, inclusive). */
export function spanFor(n: GNode, text: string): [number, number] {
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

export function excerptParts(snippets: Snippets | undefined, file: string, from: number, to: number): Excerpt[] {
  const list = snippets?.[file]
  if (!list) return []
  return list.filter((x) => x.to >= from && x.from <= to)
}

export function hasExcerpt(snippets: Snippets | undefined, file: string, from: number, to: number): boolean {
  return excerptParts(snippets, file, from, to).length > 0
}
