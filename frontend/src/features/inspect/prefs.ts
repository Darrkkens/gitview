// Choices that persist while moving between functions (the legacy report
// kept them across selections): data-flow direction and depth, profile
// kind and filters, trace filters.

import { createContext, useContext, useMemo, useState } from 'react'

export interface PrefValues {
  dfDir: 'origin' | 'both' | 'destination'
  dfDepth: number
  dfHide: boolean
  /** Data Flow variable chosen per function (symbol ID → "name@line"). */
  dfVar: Record<string, string>
  perf: 'cpu' | 'mem'
  perfMine: boolean
  perfFilter: string
  perfSort: 'flat' | 'cum'
  flameFind: string
  trAll: boolean
  trFilter: string
  trTask: string
  trView: [number, number] | null
}

const DEFAULTS: PrefValues = {
  dfDir: 'both',
  dfDepth: 3,
  dfHide: false,
  dfVar: {},
  perf: 'cpu',
  perfMine: false,
  perfFilter: '',
  perfSort: 'flat',
  flameFind: '',
  trAll: false,
  trFilter: '',
  trTask: '',
  trView: null,
}

export interface PrefsState {
  p: PrefValues
  set: (patch: Partial<PrefValues>) => void
}

export function usePrefsState(): PrefsState {
  const [p, setP] = useState(DEFAULTS)
  return useMemo(() => ({ p, set: (patch: Partial<PrefValues>) => setP((cur) => ({ ...cur, ...patch })) }), [p])
}

const PrefsCtx = createContext<PrefsState | null>(null)

export const Prefs = PrefsCtx

export function usePrefs(): PrefsState {
  const c = useContext(PrefsCtx)
  if (!c) throw new Error('inspector prefs missing')
  return c
}
