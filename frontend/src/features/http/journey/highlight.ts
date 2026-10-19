// What a selection highlights in a drawn journey: what the selected box
// depends on (following the arrows) and what depends on it, directly
// (out/in) or further along the path (out2/in2, drawn lighter).

import type { Edge, Highlight, Scene } from './Canvas'

export interface Adjacency {
  out: Map<string, Edge[]>
  inn: Map<string, Edge[]>
}

export function adjacency(scene: Scene): Adjacency {
  const out = new Map<string, Edge[]>(),
    inn = new Map<string, Edge[]>()
  for (const e of scene.edges) {
    if (!out.has(e.s)) out.set(e.s, [])
    if (!inn.has(e.t)) inn.set(e.t, [])
    out.get(e.s)!.push(e)
    inn.get(e.t)!.push(e)
  }
  return { out, inn }
}

export interface PathHighlight extends Highlight {
  down: number
  up: number
}

export function highlightFrom(scene: Scene, adj: Adjacency, key: string | null, depth = 0): PathHighlight | null {
  if (!key || !scene.boxes.some((b) => b.key === key)) return null
  const sources = new Set([key])
  const max = depth || Infinity
  const walk = (m: Map<string, Edge[]>, end: 's' | 't') => {
    const seen = new Map<string, number>()
    let frontier = [...sources],
      d = 0
    while (frontier.length && d < max) {
      d++
      const next: string[] = []
      for (const k of frontier)
        for (const e of m.get(k) ?? []) {
          const o = e[end]
          if (sources.has(o) || seen.has(o)) continue
          seen.set(o, d)
          next.push(o)
        }
      frontier = next
    }
    return seen
  }
  const down = walk(adj.out, 't'),
    up = walk(adj.inn, 's')
  const boxes = new Map<string, string>(),
    edges = new Map<string, string>()
  sources.forEach((k) => boxes.set(k, 'sel'))
  down.forEach((d, k) => boxes.set(k, d === 1 ? 'out' : 'out2'))
  up.forEach((d, k) => {
    const r = d === 1 ? 'in' : 'in2'
    const cur = boxes.get(k)
    boxes.set(k, cur === 'out' || cur === 'out2' ? (cur === 'out' || r === 'in' ? 'both' : 'out2') : r)
  })
  const RANK: Record<string, number> = { out: 4, in: 3, out2: 2, in2: 1 }
  const mark = (k: string, role: string) => {
    if (!edges.has(k) || RANK[role] > RANK[edges.get(k)!]) edges.set(k, role)
  }
  for (const e of scene.edges) {
    if ((sources.has(e.s) || down.has(e.s)) && down.has(e.t) && !sources.has(e.t))
      mark(e.key, sources.has(e.s) ? 'out' : 'out2')
    else if ((sources.has(e.t) || up.has(e.t)) && up.has(e.s) && !sources.has(e.s))
      mark(e.key, sources.has(e.t) ? 'in' : 'in2')
  }
  return { boxes, edges, down: down.size, up: up.size }
}

/** pathKeys finds the drawn path between two boxes (following the arrows). */
export function pathKeys(adj: Adjacency, a: string, b: string): string[] | null {
  const prev = new Map<string, string | null>([[a, null]])
  const q = [a]
  while (q.length) {
    const x = q.shift()!
    if (x === b) break
    for (const e of adj.out.get(x) ?? [])
      if (!prev.has(e.t)) {
        prev.set(e.t, x)
        q.push(e.t)
      }
  }
  if (!prev.has(b)) return null
  const keys: string[] = []
  for (let x: string | null = b; x !== null; x = prev.get(x) ?? null) keys.unshift(x)
  return keys
}
