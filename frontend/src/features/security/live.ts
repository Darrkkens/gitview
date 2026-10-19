// The local server's CodeAudit API (serve mode only): the AI status, asking
// the model about a finding or a path, and recording review decisions.
// Requests in flight are kept here so they survive switching tabs.

import { useEffect, useSyncExternalStore } from 'react'
import { ApiError, apiGet, apiPost } from '../../lib/api'
import { recordAI, recordReview } from './model'
import type { AiStatus, Investigation, ReviewResult, SecurityPage } from './types'

// ---------- AI status ----------

interface AiState {
  status: AiStatus | null
  error: string
  loading: boolean
}

let ai: AiState = { status: null, error: '', loading: false }
let aiAsked = false
const aiListeners = new Set<() => void>()
const setAi = (s: AiState) => {
  ai = s
  aiListeners.forEach((l) => l())
}

/** loadAiStatus asks the server whether a model can be reached. */
export function loadAiStatus() {
  aiAsked = true
  setAi({ ...ai, loading: true })
  apiGet<AiStatus>('/api/ai/status').then(
    (status) => setAi({ status, error: '', loading: false }),
    (e: Error) => setAi({ status: null, error: e.message, loading: false }),
  )
}

export function useAiStatus(enabled: boolean): AiState {
  const s = useSyncExternalStore(
    (l) => {
      aiListeners.add(l)
      return () => aiListeners.delete(l)
    },
    () => ai,
  )
  useEffect(() => {
    if (enabled && !aiAsked) loadAiStatus()
  }, [enabled])
  return s
}

// ---------- requests in flight ----------

export interface Pending {
  ctrl: AbortController
  started: number
  error?: string
}

const pending = new Map<string, Pending>()
const errors = new Map<string, string>()
const listeners = new Set<() => void>()
let version = 0
const emit = () => {
  version++
  listeners.forEach((l) => l())
}

export function usePending(key: string): { pending: Pending | undefined; error: string } {
  useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => version,
  )
  return { pending: pending.get(key), error: errors.get(key) ?? '' }
}

export function cancel(key: string) {
  pending.get(key)?.ctrl.abort()
}

async function ask(page: SecurityPage, key: string, path: string, body: unknown) {
  if (pending.has(key)) return
  const ctrl = new AbortController()
  pending.set(key, { ctrl, started: Date.now() })
  errors.delete(key)
  emit()
  try {
    const res = await apiPost<Investigation>(path, body, ctrl.signal)
    recordAI(page, key, res)
  } catch (e) {
    const err = e as Error
    errors.set(key, err.name === 'AbortError' ? 'cancelled' : 'failed: ' + err.message)
  } finally {
    pending.delete(key)
    emit()
  }
}

export function explainFinding(page: SecurityPage, finding: string, model: string) {
  return ask(page, finding, '/api/explain', { finding, model })
}

export function explainPath(page: SecurityPage, key: string, from: string, to: string, model: string) {
  return ask(page, key, '/api/explain-path', { from, to, model })
}

// ---------- reviews ----------

export async function review(page: SecurityPage, finding: string, status: string, note: string): Promise<void> {
  try {
    const res = await apiPost<ReviewResult>('/api/review', { finding, status, note })
    recordReview(page, finding, res.record, res.revalidate)
  } catch (e) {
    if (e instanceof ApiError) throw new Error(e.message || 'the server refused the decision (' + e.status + ')')
    throw e
  }
}

/** useAiReady: the server can reach a model. */
export function useAiReady(live: boolean) {
  const { status } = useAiStatus(live)
  return !!status && status.reachable && (!!status.selected || (status.models ?? []).length > 0)
}
