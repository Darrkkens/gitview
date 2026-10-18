// Shared state of the inspector's tabs: the section, the report's Code Flow
// index, the diff anchors and the selection of another function.

import { createContext, useContext } from 'react'
import type { Bundle, FlowIndex, InspectSection, TabId } from './types'

export interface InspectCtx {
  data: InspectSection
  bundles: Bundle[]
  /** The report's Code Flow index (report.flow), when it was exported. */
  flow: FlowIndex | null
  /** Hash of the Changes view for file:line, or null when not in the diff. */
  diffHref: (file?: string, line?: number) => string | null
  /** Index of a symbol in the report, -1 when absent. */
  indexOf: (id: string | undefined) => number
  /** Opens another function of the report (keeps the tab unless given). */
  select: (id: string, tab?: TabId) => void
}

export const Ctx = createContext<InspectCtx | null>(null)

export function useInspect(): InspectCtx {
  const c = useContext(Ctx)
  if (!c) throw new Error('inspector context missing')
  return c
}
