// What the inspector's panels share besides components: the text of a
// relation's evidence, the repository's numbers, and the tab and
// confidences kept while the selection changes.

import { useState } from 'react'
import type { Model } from '../model'

export type ItemTab = 'overview' | 'connections' | 'source' | 'metadata'

export const evidenceText = (e: { file: string; line: number; w: number; note: string }) =>
  (e.file ? e.file + ':' + e.line : '') + (e.w > 1 ? ' · ' + e.w + ' sites' : '') + (e.note ? ' · ' + e.note : '')

export const statPairs = (D: Model): [string, number][] => {
  const s = D.raw.stats
  return [
    ['Modules', s.modules],
    ['Packages', s.packages],
    ['Files', s.files],
    ['Functions', s.functions],
    ['Types', s.types],
    ['Routes', s.routes || 0],
    ['External packages', s.externalPackages],
    ['Nodes', s.nodes],
    ['Relations', s.edges],
  ]
}

export const otherEnd = (e: { s: number; t: number }, dir: number) => (dir > 0 ? e.t : e.s)

// The element tab and the confidences shown in Connections are kept while
// the selection changes (per viewer, in memory).
const memory: { tab: ItemTab; confs: Record<string, boolean> } = { tab: 'overview', confs: {} }

export function useTabMemory(): [ItemTab, (t: ItemTab) => void] {
  const [tab, setTab] = useState<ItemTab>(memory.tab)
  return [
    tab,
    (t) => {
      memory.tab = t
      setTab(t)
    },
  ]
}

export function useConfMemory(): [Record<string, boolean>, (c: Record<string, boolean>) => void] {
  const [confs, setConfs] = useState(memory.confs)
  return [
    confs,
    (c) => {
      memory.confs = c
      setConfs(c)
    },
  ]
}
