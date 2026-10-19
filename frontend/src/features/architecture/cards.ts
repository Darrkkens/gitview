// Cards: what a drawn element says about itself, compactly. The kind, the
// name, one line of statistics and one of dependencies:
//
//   PACKAGE
//   internal/modules/catalog
//   12 files · 48 functions
//   8 dependencies · 3 dependents
//
// Symbols have three lines (their callers and callees are their
// statistics). Everything else stays in the inspector. Counts come from the
// dataset (never invented); dependencies count the distinct packages (for a
// symbol: the distinct symbols) on the other side of its relations.

import { plural } from '../../lib/format'
import type { Model } from './model'
import { font, textWidth } from './text'
import type { TItem } from './types'
import { FLOW, FUNC, STRUCTURAL, SYMBOL, TYPE } from './vocab'

export interface Card {
  title: string
  kind: string
  stats: string
  deps: string
  w: number
  h: number
}

export const CARD_H = { container: 68, symbol: 54 }
export const KIND_FONT = () => font(600, 9.5)

interface Counts {
  funcs: Int32Array
  types: Int32Array
}

const countsCache = new WeakMap<Model, Counts>()
function countsOf(D: Model): Counts {
  let c = countsCache.get(D)
  if (c) return c
  const n = D.T.length
  c = { funcs: new Int32Array(n), types: new Int32Array(n) }
  for (let k = D.order.length - 1; k >= 0; k--) {
    const t = D.T[D.order[k]]
    if (t.n && (t.kind === 'func' || t.kind === 'method')) c.funcs[t.i]++
    if (t.n && TYPE[t.kind]) c.types[t.i]++
    if (t.parent >= 0) {
      c.funcs[t.parent] += c.funcs[t.i]
      c.types[t.parent] += c.types[t.i]
    }
  }
  countsCache.set(D, c)
  return c
}

const depsCache = new WeakMap<Model, Map<number, [number, number]>>()
/** depCounts: distinct packages (symbols: distinct symbols) it depends on, and that depend on it. */
export function depCounts(D: Model, i: number): [number, number] {
  let m = depsCache.get(D)
  if (!m) depsCache.set(D, (m = new Map()))
  const hit = m.get(i)
  if (hit) return hit
  const t = D.T[i]
  const sym = !!t.n && !!SYMBOL[t.kind] && !t.kids.length
  const r = D.relationsOf(i)
  const self = sym ? -1 : D.packageOf(i)
  const unit = (x: number) => (sym ? x : D.packageOf(x))
  const count = (list: number[], end: 's' | 't') => {
    const s = new Set<number>()
    for (const ei of list) {
      const e = D.E[ei]
      if (STRUCTURAL[e.type]) continue
      if (sym && FUNC[t.kind] && !FLOW[e.type]) continue
      const u = unit(e[end])
      if (u >= 0 && u !== self && u !== i) s.add(u)
    }
    return s.size
  }
  const res: [number, number] = [count(r.out, 't'), count(r.inn, 's')]
  m.set(i, res)
  return res
}

function statsOf(D: Model, t: TItem): string {
  const c = countsOf(D)
  const k = t.kind
  const n = t.n
  if (k === 'repo') return plural(D.modules.length, 'module') + ' · ' + plural(t.npkgs, 'package')
  if (k === 'module' || (k === 'dir' && !t.ext)) return plural(t.npkgs, 'package') + ' · ' + plural(t.nfiles, 'file')
  if (k === 'dir' || k === 'extgroup') return plural(t.npkgs, 'package')
  if (k === 'package') {
    if (n?.ext) return (n.meta.std ? 'standard library' : 'third-party') + ' · ' + plural(t.count, 'function') + ' used'
    return plural(t.nfiles, 'file') + ' · ' + plural(c.funcs[t.i], 'function') + (c.types[t.i] ? ' · ' + plural(c.types[t.i], 'type') : '')
  }
  if (k === 'file') {
    const f = c.funcs[t.i],
      ty = c.types[t.i]
    return f || ty ? [f ? plural(f, 'function') : '', ty ? plural(ty, 'type') : ''].filter(Boolean).join(' · ') : 'no declarations'
  }
  if (!n) return D.kindLabel(t)
  if (FUNC[k]) {
    const [out, inn] = depCounts(D, t.i)
    return plural(inn, 'caller') + ' · ' + plural(out, 'callee')
  }
  if (TYPE[k]) {
    const methods = t.kids.filter((x) => D.T[x].kind === 'method').length
    const [, inn] = depCounts(D, t.i)
    return (methods ? plural(methods, 'method') + ' · ' : '') + 'used by ' + plural(inn, 'symbol')
  }
  if (k === 'route') return (n.meta.method || 'ANY') + ' · ' + (n.meta.router || 'route')
  if (k === 'db') return ((n.meta.lib || 'db') + ' ' + (n.meta.op || '')).trim()
  if (k === 'external') return n.pkg || 'external'
  return n.line ? 'line ' + n.line : D.kindLabel(t)
}

function depsOf(D: Model, t: TItem): string {
  if (t.kind === 'repo') return ''
  if (t.n?.ext && t.kind === 'package') {
    const [, inn] = depCounts(D, t.i)
    return 'used by ' + plural(inn, 'package')
  }
  if (t.kind === 'extgroup' || (t.kind === 'dir' && t.ext)) return ''
  const [out, inn] = depCounts(D, t.i)
  return plural(out, 'dependency', 'dependencies') + ' · ' + plural(inn, 'dependent')
}

/** kindText: the card's first line ("PACKAGE", "OUTSIDE · PACKAGE"). */
export function kindText(D: Model, t: TItem, ctx?: boolean): string {
  const k = D.kindLabel(t) + (t.n?.ext && t.kind === 'package' ? ' · external' : '')
  return (ctx ? 'outside · ' + k : k).toUpperCase()
}

const cardCache = new WeakMap<Model, Map<string, Card>>()

/**
 * cardOf: the text and size of an element's card. ctx: drawn outside the
 * scope; flat: drawn without its parent around it (focus mode), so a
 * method keeps its receiver.
 */
export function cardOf(D: Model, t: TItem, ctx?: boolean, flat?: boolean): Card {
  let m = cardCache.get(D)
  if (!m) cardCache.set(D, (m = new Map()))
  const key = t.i + (ctx ? 'c' : '') + (flat ? 'f' : '')
  const hit = m.get(key)
  if (hit) return hit
  const symbol = !!t.n && !!SYMBOL[t.kind]
  const title = D.displayName(t, !ctx && !flat)
  const kind = kindText(D, t, ctx)
  const stats = statsOf(D, t)
  const deps = symbol ? '' : depsOf(D, t)
  const chev = !ctx && t.kids.length > 0 ? 18 : 0
  const tw = Math.max(
    textWidth(title, font(600, 12.5)) + chev,
    textWidth(kind, KIND_FONT()) + chev + 16,
    textWidth(stats, font(400, 11)),
    textWidth(deps, font(400, 11)),
  )
  const card: Card = {
    title,
    kind,
    stats,
    deps,
    w: Math.round(Math.max(symbol ? 150 : 168, Math.min(tw + 30, symbol ? 260 : 280))),
    h: symbol || ctx || !deps ? CARD_H.symbol : CARD_H.container,
  }
  m.set(key, card)
  return card
}
