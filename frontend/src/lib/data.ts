// Section data for the views. An offline page has its sections inline in
// the envelope; a served page fetches each one from /api/section/<name> the
// first time a view asks for it (the server analyses on demand, once, and
// caches), and can ask for a re-analysis after the repository changed.

import { useEffect, useSyncExternalStore } from 'react'
import { apiEvents, apiGet, apiPost } from './api'
import { envelope, isLive, type SectionName } from './envelope'
import { resetSources } from './sources'
import { clearHighlightCache } from './go'

export type SectionState<T> =
  | { status: 'absent' }
  | { status: 'loading' }
  | { status: 'ready'; data: T }
  | { status: 'error'; error: string }

/** Server-side state of one analysis (serve mode). */
export interface SectionStatus {
  state: 'idle' | 'running' | 'ready' | 'error'
  error?: string
  started?: string
  finished?: string
  stale?: boolean
}

export interface ServerStatus {
  repository: string
  root: string
  branch: string
  sections: Partial<Record<SectionName, SectionStatus>>
  ai?: { available: boolean; model?: string; error?: string }
}

interface Store {
  sections: Partial<Record<SectionName, SectionState<unknown>>>
  status: ServerStatus | null
  changedAt: string | null // last repository change seen by the server
  version: number
}

const store: Store = { sections: {}, status: null, changedAt: null, version: 0 }
const listeners = new Set<() => void>()
const inflight = new Map<SectionName, Promise<void>>()

function emit() {
  store.version++
  listeners.forEach((l) => l())
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

function set(name: SectionName, s: SectionState<unknown>) {
  store.sections = { ...store.sections, [name]: s }
  emit()
}

// Inline sections are ready from the start.
for (const [name, data] of Object.entries(envelope().sections)) {
  if (data !== undefined && data !== null) store.sections[name as SectionName] = { status: 'ready', data }
}

/** offered tells whether a section exists on this page (inline or on the server). */
export function offered(name: SectionName): boolean {
  const env = envelope()
  if (env.sections[name] !== undefined && env.sections[name] !== null) return true
  if (!isLive()) return false
  if (env.offered) return env.offered.includes(name)
  return !!store.status?.sections[name] || !store.status
}

function load(name: SectionName, force = false): Promise<void> {
  if (!isLive()) return Promise.resolve()
  const cur = inflight.get(name)
  if (cur && !force) return cur
  const p = (async () => {
    set(name, { status: 'loading' })
    try {
      if (force) await apiPost('/api/section/' + name + '/refresh', {})
      const data = await apiGet<unknown>('/api/section/' + name)
      set(name, { status: 'ready', data })
    } catch (e) {
      set(name, { status: 'error', error: (e as Error).message })
    } finally {
      inflight.delete(name)
    }
  })()
  inflight.set(name, p)
  return p
}

/**
 * useSection returns a section's data, loading it in serve mode. The data
 * object is stable until the section is reloaded.
 */
export function useSection<T>(name: SectionName): SectionState<T> {
  useSyncExternalStore(subscribe, () => store.version)
  const s = store.sections[name]
  useEffect(() => {
    if (!s && isLive()) void load(name)
  }, [name, s])
  if (s) return s as SectionState<T>
  return isLive() ? { status: 'loading' } : { status: 'absent' }
}

/** peekData returns a section's data when it is ready, outside React. */
export function peekData<T>(name: SectionName): T | undefined {
  const s = store.sections[name]
  return s?.status === 'ready' ? (s.data as T) : undefined
}

/** usePeek returns a section's state without loading it (for tabs). */
export function usePeek<T>(name: SectionName): SectionState<T> | undefined {
  useSyncExternalStore(subscribe, () => store.version)
  return store.sections[name] as SectionState<T> | undefined
}

const reloadHooks: (() => void)[] = []

/** onReload registers a cache to clear when an analysis is redone. */
export function onReload(fn: () => void) {
  reloadHooks.push(fn)
}

/** reload asks the server to analyse again (serve mode). */
export function reload(name: SectionName): Promise<void> {
  resetSources()
  clearHighlightCache()
  for (const fn of reloadHooks) fn()
  return load(name, true)
}

/** reloadLoaded re-analyses every section already opened. */
export async function reloadLoaded(): Promise<void> {
  const names = Object.keys(store.sections) as SectionName[]
  store.changedAt = null
  await Promise.all(names.map((n) => reload(n)))
}

export function useServerStatus(): { status: ServerStatus | null; changedAt: string | null } {
  useSyncExternalStore(subscribe, () => store.version)
  return { status: store.status, changedAt: store.changedAt }
}

let started = false

/** startLive polls the server status and listens to its events (serve mode). */
export function startLive() {
  if (started || !isLive()) return
  started = true
  const refresh = () =>
    apiGet<ServerStatus>('/api/status')
      .then((st) => {
        store.status = st
        emit()
      })
      .catch(() => {
        /* the next event or poll retries */
      })
  void refresh()
  apiEvents((type, data) => {
    if (type === 'changed') {
      store.changedAt = (data as { at?: string } | null)?.at ?? new Date().toISOString()
      emit()
    }
    void refresh()
  })
  setInterval(refresh, 15000)
}
