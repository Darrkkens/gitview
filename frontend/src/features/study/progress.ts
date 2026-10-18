// Study progress of this viewer: what was opened or marked, the answers to
// the exercises, and the Explain It Yourself checklist and notes. It lives
// in localStorage under the same keys as the legacy report
// ("gitview-study:<fingerprint>", ":answers", ":explain"), so it never
// leaves the browser; when storage is unavailable it lasts for the page.

import { useSyncExternalStore } from 'react'

/** Visited items by area (architecture, codeflow, files, dataflow…). */
export type Visited = Record<string, Record<string, 1>>
/** Last answer of each exercise: 1 correct, 0 not correct. */
export type Answers = Record<string, 0 | 1>
/** Checklist points ("<func>|<key>": 1) and notes ("<func>|note": text). */
export type ExplainState = Record<string, 1 | string>

export interface Progress {
  visited: Visited
  answers: Answers
  explain: ExplainState
}

let fingerprint: string | null = null
let state: Progress = { visited: {}, answers: {}, explain: {} }
const listeners = new Set<() => void>()

const key = () => 'gitview-study:' + (fingerprint ?? '')

function read<T extends object>(k: string): T {
  try {
    const v = JSON.parse(localStorage.getItem(k) || '{}') as unknown
    return (v && typeof v === 'object' && !Array.isArray(v) ? v : {}) as T
  } catch {
    return {} as T
  }
}

function write(k: string, v: unknown) {
  try {
    localStorage.setItem(k, JSON.stringify(v))
  } catch {
    /* per-viewer convenience only */
  }
}

function load(fp: string) {
  if (fp === fingerprint) return
  fingerprint = fp
  state = { visited: read(key()), answers: read(key() + ':answers'), explain: read(key() + ':explain') }
}

function emit(next: Progress) {
  state = next
  listeners.forEach((l) => l())
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

/** useProgress returns the progress of the report with this fingerprint. */
export function useProgress(fp: string): Progress {
  load(fp)
  return useSyncExternalStore(subscribe, () => state)
}

/** track records that an item of an area was opened or marked. */
export function track(area: string, id: string | undefined) {
  if (!area || !id || state.visited[area]?.[id]) return
  const visited = { ...state.visited, [area]: { ...state.visited[area], [id]: 1 as const } }
  write(key(), visited)
  emit({ ...state, visited })
}

export function answer(id: string, ok: boolean) {
  const answers = { ...state.answers, [id]: ok ? (1 as const) : (0 as const) }
  write(key() + ':answers', answers)
  emit({ ...state, answers })
}

/** setExplain sets (or, with undefined, clears) a checklist point or a note. */
export function setExplain(k: string, v: 1 | string | undefined) {
  const explain = { ...state.explain }
  if (v === undefined || v === '') delete explain[k]
  else explain[k] = v
  write(key() + ':explain', explain)
  emit({ ...state, explain })
}

export function resetProgress() {
  try {
    localStorage.removeItem(key())
    localStorage.removeItem(key() + ':answers')
    localStorage.removeItem(key() + ':explain')
  } catch {
    /* nothing stored */
  }
  emit({ visited: {}, answers: {}, explain: {} })
}

export const visitedCount = (p: Progress, area: string) => Object.keys(p.visited[area] ?? {}).length
