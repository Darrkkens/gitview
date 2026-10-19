// Pure helpers of the Changes view: diffstat bars, the unified-diff text of
// a hunk, chunking of long diffs and truncation of absurdly long lines.

import type { ChromaSpan, DiffChunk, DiffFile, DiffHunk, DiffRow, ReportSection, TestReport } from './types'

/** Height of a diff line and of a hunk header row, in pixels (diff.css). */
export const ROW_H = 20
export const HUNK_H = 26
/** Rows per lazily mounted chunk. */
export const CHUNK_ROWS = 100
/** Lines longer than this are cut, with a "show all" affordance. */
export const LONG_LINE = 1500

// lib/format's fmt builds a formatter per call (toLocaleString), which shows
// up when hundreds of files are listed: keep one.
const nf = new Intl.NumberFormat('en-US')
export const num = (n: number | null | undefined) => nf.format(n || 0)

/**
 * diffstatBar distributes five blocks proportionally between additions and
 * deletions, like the compact bars of code review tools (the Go side's
 * diffstatBar in internal/export/html.go).
 */
export function diffstatBar(add: number, del: number): ('add' | 'del' | 'none')[] {
  const bar: ('add' | 'del' | 'none')[] = ['none', 'none', 'none', 'none', 'none']
  const total = add + del
  if (total === 0) return bar
  const filled = Math.min(total, 5)
  let adds = Math.floor((add * filled + Math.floor(total / 2)) / total)
  if (add > 0 && adds === 0) adds = 1
  if (del > 0 && adds === filled) adds = filled - 1
  for (let i = 0; i < filled; i++) bar[i] = i < adds ? 'add' : 'del'
  return bar
}

export const spansText = (spans: ChromaSpan[] | null | undefined) => {
  if (!spans) return ''
  let s = ''
  for (const sp of spans) s += sp[0]
  return s
}

export const spansLength = (spans: ChromaSpan[] | null | undefined) => {
  if (!spans) return 0
  let n = 0
  for (const sp of spans) n += sp[0].length
  return n
}

/** truncateSpans keeps the first max characters of a highlighted line. */
export function truncateSpans(spans: ChromaSpan[], max: number): ChromaSpan[] {
  const out: ChromaSpan[] = []
  let n = 0
  for (const sp of spans) {
    if (n >= max) break
    const t = sp[0]
    if (n + t.length <= max) {
      out.push(sp)
      n += t.length
    } else {
      const cut = t.slice(0, max - n)
      out.push(sp.length > 1 ? [cut, sp[1] as string] : [cut])
      n = max
    }
  }
  return out
}

const SIGN = { add: '+', del: '-', ctx: ' ' } as const

/** hunkText is a hunk in unified diff format (what "Copy hunk" copies). */
export function hunkText(h: DiffHunk): string {
  const out = [h.range + (h.section ? ' ' + h.section : '')]
  for (const l of h.lines) out.push(SIGN[l.kind] + spansText(l.spans))
  return out.join('\n') + '\n'
}

/** maxLineNumber is the widest line number of a file (gutter width). */
export function maxLineNumber(f: DiffFile): number {
  let m = 0
  for (const h of f.hunks) for (const l of h.lines) m = Math.max(m, l.old, l.new)
  return m
}

/** chunks splits a file's rows into slices of at most CHUNK_ROWS rows. */
export function chunks(f: DiffFile): DiffChunk[] {
  const out: DiffChunk[] = []
  let cur: DiffChunk = { rows: [], height: 0 }
  const push = (r: DiffRow, h: number) => {
    if (cur.rows.length >= CHUNK_ROWS) {
      out.push(cur)
      cur = { rows: [], height: 0 }
    }
    cur.rows.push(r)
    cur.height += h
  }
  f.hunks.forEach((h, hi) => {
    push({ hunk: hi, line: -1 }, HUNK_H)
    h.lines.forEach((_, li) => push({ hunk: hi, line: li }, ROW_H))
  })
  if (cur.rows.length) out.push(cur)
  return out
}

/** findNewLine locates a new-side line number in a file's diff. */
export function findNewLine(f: DiffFile, line: number): { hunk: number; line: number } | null {
  for (let hi = 0; hi < f.hunks.length; hi++) {
    const ls = f.hunks[hi].lines
    for (let li = 0; li < ls.length; li++) if (ls[li].new === line) return { hunk: hi, line: li }
  }
  return null
}

/** testsLine is the overview's test status, e.g. "Tests failed". */
export function testsLine(t: TestReport | undefined): {
  text: string
  tone: 'success' | 'error' | 'warning' | 'muted'
} {
  if (!t || !t.ran) return { text: 'Tests not run', tone: 'muted' }
  switch (t.status) {
    case 'passed':
      return { text: 'Tests passed', tone: 'success' }
    case 'failed':
      return { text: 'Tests failed', tone: 'error' }
    case 'cancelled':
      return { text: 'Tests cancelled', tone: 'warning' }
    case 'running':
      return { text: 'Tests running', tone: 'warning' }
  }
  return { text: 'Tests did not complete', tone: 'warning' }
}

/** fileByPath finds a changed file by path, old path or anchor id. */
export function fileByPath(r: ReportSection, key: string): DiffFile | undefined {
  return r.files.find((f) => f.path === key) ?? r.files.find((f) => f.oldPath === key || f.id === key)
}
