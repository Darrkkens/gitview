// Links from the runtime report to the code views.

import { offered, usePeek } from '../../lib/data'
import { isLive } from '../../lib/envelope'
import { routeHref } from '../../lib/router'
import type { ReportSection } from '../../lib/types'

const ids = new WeakMap<object, Set<string>>()

function nodeIDs(flow: { nodes?: { id: string }[] }): Set<string> {
  let s = ids.get(flow)
  if (!s) {
    s = new Set((flow.nodes ?? []).map((n) => n.id))
    ids.set(flow, s)
  }
  return s
}

/**
 * useCodeLink links a module function (its flow symbol ID) to Code Flow when
 * the page can show it: served pages that offer the session report (once
 * that report is loaded, only functions of its call index). Offline runtime
 * reports carry no code views, so they have no such links.
 */
export function useCodeLink(): (symbol: string | undefined) => string | null {
  const report = usePeek<ReportSection>('report')
  const live = isLive() && offered('report')
  const flow = report?.status === 'ready' ? (report.data.flow as { nodes?: { id: string }[] } | undefined) : undefined
  const nodes = flow ? nodeIDs(flow) : null
  return (symbol) => {
    if (!symbol || !live) return null
    if (nodes && !nodes.has(symbol)) return null
    return routeHref('flow', { node: symbol })
  }
}
