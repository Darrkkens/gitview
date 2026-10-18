// Navigation targets of the Study evidence. A ref names where a fact can be
// seen ("codeflow:<node>", "inspect:<symbol>:<tab>", "diff:<file>:<line>");
// it becomes a link only when the report carries that view's data, so a
// link never opens an empty view.

import { routeHref } from '../../lib/router'
import type { ReportSection } from '../../lib/types'
import type { FlowLite, InspectLite } from './types'

export interface SourceSnippet {
  code: string
  line: number
  file?: string
}

export interface Links {
  /** href of an evidence ref, or null when its target is not in the report. */
  href(ref: string | undefined): string | null
  flow(id: string | undefined): string | null
  inspect(symbol: string | undefined, tab: string): string | null
  diff(file: string | undefined, line?: number): string | null
  /** The source of a function: the Inspector's copy, else the Code Flow snippet. */
  source(id: string | undefined): SourceSnippet | null
}

export function makeLinks(report: ReportSection): Links {
  const flow = (report.flow ?? null) as FlowLite | null
  const inspect = (report.inspect ?? null) as InspectLite | null
  const nodes = new Map((flow?.nodes ?? []).map((n) => [n.id, n]))
  const bundles = new Map((inspect?.symbols ?? []).map((b) => [b.symbol.id, b]))
  // Files the Changes view shows with content, and the new-side lines of
  // their hunks (a line outside them opens the file, not the line).
  const diffLines = new Map<string, Set<number>>()
  for (const f of report.files ?? []) {
    if (f.omitted || f.binary) continue
    const lines = new Set<number>()
    for (const h of f.hunks ?? []) for (const l of h.lines) if (l.new > 0) lines.add(l.new)
    diffLines.set(f.path, lines)
  }

  const flowHref = (id: string | undefined) => (id && nodes.has(id) ? routeHref('flow', { node: id }) : null)
  const inspectHref = (sym: string | undefined, tab: string) =>
    sym && bundles.has(sym) ? routeHref('inspect', { fn: sym, tab }) : null
  const diffHref = (file: string | undefined, line?: number) => {
    if (!file) return null
    const lines = diffLines.get(file)
    if (!lines) return null
    return routeHref('changes', { file, line: line && line > 0 && lines.has(line) ? line : undefined })
  }

  return {
    flow: flowHref,
    inspect: inspectHref,
    diff: diffHref,
    href(ref) {
      if (!ref) return null
      const i = ref.indexOf(':')
      if (i < 0) return null
      const kind = ref.slice(0, i)
      const rest = ref.slice(i + 1)
      switch (kind) {
        case 'codeflow':
          return flowHref(rest)
        case 'inspect': {
          const j = rest.lastIndexOf(':')
          return j < 0 ? inspectHref(rest, '') : inspectHref(rest.slice(0, j), rest.slice(j + 1))
        }
        case 'diff': {
          const j = rest.lastIndexOf(':')
          return j < 0 ? diffHref(rest) : diffHref(rest.slice(0, j), Number(rest.slice(j + 1)) || 0)
        }
      }
      return null
    },
    source(id) {
      if (!id) return null
      const b = bundles.get(id)
      if (b?.code) return { code: b.code, line: b.symbol.line ?? 1, file: b.symbol.file }
      const n = nodes.get(id)
      if (n?.code) return { code: n.code, line: n.line ?? 1, file: n.file }
      return null
    },
  }
}
