// Hovers over Go identifiers in the code views: what an identifier refers
// to (signature, documentation, declaration), from GitView's Go analysis
// (internal/hover). A served page asks /api/hover; an offline page carries
// a table for the code it shows (report section "hovers").
//
// An identifier is addressed by its file, line, byte column (UTF-8, 1-based,
// as go/token counts) and name.

import { apiGet } from './api'
import { onReload, peekData } from './data'
import { isLive } from './envelope'


export interface HoverInfo {
  name: string
  kind: string // func | method | type | field | var | const | package | builtin | label
  sig: string
  decl?: string
  doc?: string
  pkg?: string
  file?: string
  line?: number
  local?: boolean
  symbol?: string
}

export type HoverStatus = 'ok' | 'stale' | 'none' | 'unknown-file' | 'pending'

export interface HoverReply {
  status: HoverStatus
  info?: HoverInfo
  message?: string
}

export interface HoverTable {
  items: (HoverInfo | null)[]
  files: Record<string, Record<string, number>>
  truncated?: boolean
}

interface WithHovers {
  hovers?: HoverTable | null
}

const NONE: HoverReply = { status: 'none' }

const cache = new Map<string, Promise<HoverReply>>()

export function clearHoverCache() {
  cache.clear()
}

onReload(clearHoverCache)

function fromTable(file: string, line: number, col: number, name: string): HoverReply {
  const t = peekData<WithHovers>('report')?.hovers
  const i = t?.files[file]?.[line + ':' + col + ':' + name]
  const info = i === undefined ? null : t!.items[i]
  return info ? { status: 'ok', info } : NONE
}

/** hoverAt describes the identifier name at file:line:col. */
export function hoverAt(file: string, line: number, col: number, name: string): Promise<HoverReply> {
  if (!isLive()) return Promise.resolve(fromTable(file, line, col, name))
  const key = file + ':' + line + ':' + col + ':' + name
  const hit = cache.get(key)
  if (hit) return hit
  const q = new URLSearchParams({ file, line: String(line), col: String(col), name })
  const p = apiGet<HoverReply>('/api/hover?' + q.toString()).catch(
    (e: Error): HoverReply => ({ status: 'none', message: e.message }),
  )
  cache.set(key, p)
  // An answer that depends on a running analysis is asked again next time.
  void p.then((r) => {
    if (r.status === 'pending' || r.message) cache.delete(key)
  })
  return p
}

/** inspectorHas tells whether the Inspector of this page holds a function. */
export function inspectorHas(symbol: string): boolean {
  const syms = peekData<{ inspect?: { symbols?: { symbol: { id: string } }[] } }>('report')?.inspect?.symbols
  return !!syms?.some((s) => s.symbol.id === symbol)
}
