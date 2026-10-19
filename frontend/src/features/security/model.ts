// The Security Journey's vocabulary and state: severities, categories,
// review statuses, the state shown for a finding (a reviewer's decision
// overrides the analysis' verdict), and what this page learned since it
// was loaded (decisions recorded and AI answers received in serve mode).

import { useMemo, useSyncExternalStore } from 'react'
import { isLive } from '../../lib/envelope'
import type { Diff, Finding, Investigation, ReviewRecord, RoutePosture, RuleInfo, SecurityPage } from './types'

export const SEVS = ['critical', 'high', 'medium', 'low', 'info'] as const
export const SEV_RANK: Record<string, number> = { critical: 5, high: 4, medium: 3, low: 2, info: 1 }
export const CATS: Record<string, string> = {
  authentication: 'Authentication',
  authorization: 'Authorization',
  injection: 'Injection',
  'sensitive-data': 'Sensitive data',
  configuration: 'Configuration',
  dependencies: 'Dependencies',
}
export const STATUSES = [
  'open',
  'investigating',
  'confirmed',
  'false_positive',
  'accepted_risk',
  'fixed',
  'needs_revalidation',
] as const
export const STATUS_LABEL: Record<string, string> = {
  open: 'Open',
  investigating: 'Investigating',
  confirmed: 'Confirmed',
  false_positive: 'False positive',
  accepted_risk: 'Accepted risk',
  fixed: 'Fixed',
  needs_revalidation: 'Needs revalidation',
}
export const FINDING_TABS = ['overview', 'evidence', 'source', 'flow', 'fix', 'ai', 'review']

/** Statuses that need a justification (the server checks it too). */
export const NEEDS_NOTE = new Set(['confirmed', 'false_positive', 'accepted_risk', 'fixed'])

export type StateClass = 'vulnerable' | 'review' | 'incomplete' | 'check' | 'struct' | 'dismissed'

export interface FState {
  cls: StateClass
  label: string
  token: string
}

export const STATE_TOKEN: Record<StateClass, string> = {
  vulnerable: 's-vuln',
  review: 's-review',
  incomplete: 's-unres',
  check: 's-check',
  struct: 's-struct',
  dismissed: 's-unres',
}

// ---------- what the page learned (serve mode) ----------

interface Overlay {
  reviews: Record<string, ReviewRecord>
  revalidate: Record<string, string | null>
  ai: Record<string, Investigation>
}

// Overlays are replaced, never changed, so a new one is a new snapshot.
const overlays = new WeakMap<object, Overlay>()
const EMPTY: Overlay = { reviews: {}, revalidate: {}, ai: {} }
const listeners = new Set<() => void>()

const overlay = (p: SecurityPage): Overlay => overlays.get(p) ?? EMPTY

function update(p: SecurityPage, o: Overlay) {
  overlays.set(p, o)
  listeners.forEach((l) => l())
}

export function recordReview(p: SecurityPage, id: string, rec: ReviewRecord, revalidate: string) {
  const o = overlay(p)
  update(p, { ...o, reviews: { ...o.reviews, [id]: rec }, revalidate: { ...o.revalidate, [id]: revalidate || null } })
}

export function recordAI(p: SecurityPage, key: string, res: Investigation) {
  const o = overlay(p)
  update(p, { ...o, ai: { ...o.ai, [key]: res } })
}

/** pathKey: the key of an AI investigation of a path. */
export const pathKey = (from: string, to: string) => from + '→' + to

// ---------- the audit, indexed ----------

export interface Sec {
  page: SecurityPage
  F: Finding[]
  byId: Map<string, Finding>
  rules: RuleInfo[]
  ruleById: Map<string, RuleInfo>
  postures: RoutePosture[]
  postureOf: Map<string, RoutePosture>
  reviews: Record<string, ReviewRecord>
  revalidate: Record<string, string>
  ai: Record<string, Investigation>
  diff: Diff | null
  diffOf: Map<string, { kind: string; changes?: string[] }>
  /** asking the model and recording decisions are possible */
  live: boolean
  status: (f: Finding) => string
  stateOf: (f: Finding) => FState
}

export function stateFor(f: Finding, status: string): FState {
  switch (status) {
    case 'confirmed':
      return { cls: 'vulnerable', label: 'Confirmed by review', token: 's-vuln' }
    case 'false_positive':
      return { cls: 'dismissed', label: 'False positive (review)', token: 's-unres' }
    case 'accepted_risk':
      return { cls: 'review', label: 'Accepted risk (review)', token: 's-review' }
    case 'fixed':
      return { cls: 'check', label: 'Marked fixed (review)', token: 's-check' }
  }
  if (f.verdict === 'vulnerable') return { cls: 'vulnerable', label: 'Vulnerable', token: 's-vuln' }
  if (f.verdict === 'incomplete') return { cls: 'incomplete', label: 'Analysis incomplete', token: 's-unres' }
  return { cls: 'review', label: 'Review required', token: 's-review' }
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

/** useSec indexes the audit and merges what this page recorded since it loaded. */
export function useSec(page: SecurityPage): Sec {
  const o = useSyncExternalStore(subscribe, () => overlay(page))
  const base = useMemo(() => {
    const F = page.audit.findings ?? []
    const rules = page.audit.rules ?? []
    const postures = page.audit.routes ?? []
    const diffOf = new Map<string, { kind: string; changes?: string[] }>()
    if (page.diff) {
      for (const x of page.diff.new ?? []) diffOf.set(x.id, { kind: 'new' })
      for (const x of page.diff.changed ?? []) diffOf.set(x.id, { kind: 'changed', changes: x.changes })
    }
    return {
      F,
      byId: new Map(F.map((f) => [f.id, f])),
      rules,
      ruleById: new Map(rules.map((r) => [r.id, r])),
      postures,
      postureOf: new Map(postures.map((r) => [r.id, r])),
      diffOf,
    }
  }, [page])
  return useMemo(() => {
    const reviews = { ...(page.reviews ?? {}), ...o.reviews }
    const revalidate: Record<string, string> = { ...(page.revalidate ?? {}) }
    for (const [k, x] of Object.entries(o.revalidate)) {
      if (x) revalidate[k] = x
      else delete revalidate[k]
    }
    const ai = { ...(page.ai ?? {}), ...o.ai }
    const status = (f: Finding) => reviews[f.id]?.status || 'open'
    return {
      page,
      ...base,
      reviews,
      revalidate,
      ai,
      diff: page.diff ?? null,
      live: !!page.live && isLive(),
      status,
      stateOf: (f: Finding) => stateFor(f, status(f)),
    }
  }, [page, base, o])
}

export const commitText = (p: SecurityPage) =>
  (p.audit.commit || '').slice(0, 12) + (p.audit.dirty ? ' (uncommitted changes)' : '')

/** dateUTC: "2026-10-18 15:04:05" from an RFC 3339 time. */
export const dateUTC = (s?: string, n = 19) => (s || '').replace('T', ' ').slice(0, n)
