// Code Flow: callers and callees of the function in the report's call index.

import { useMemo } from 'react'
import { Icon } from '../../components/Icon'
import { routeHref } from '../../lib/router'
import { useInspect } from './ctx'
import type { Bundle, FlowEdge, FlowNode } from './types'
import { Bar, Basis, H4, LocLink, Msg, Note, TABLE } from './ui'

const CONF_TONE: Record<string, string> = {
  confirmed: 'text-c-confirmed',
  exact: 'text-c-confirmed',
  likely: 'text-c-likely',
  possible: 'text-c-possible',
  unresolved: 'text-c-unresolved',
}

function Calls({
  title,
  edges,
  other,
  nodes,
}: {
  title: string
  edges: FlowEdge[]
  other: (e: FlowEdge) => string
  nodes: Map<string, FlowNode>
}) {
  const { indexOf, select } = useInspect()
  return (
    <div className="min-w-0 rounded-box border border-line bg-base-100 px-3.5 py-2.5">
      <H4 className="mt-0">
        {title} ({edges.length})
      </H4>
      {!edges.length ? (
        <Note>None found.</Note>
      ) : (
        <table className={TABLE}>
          <thead>
            <tr>
              <th>Function</th>
              <th>Confidence</th>
              <th>Call site</th>
            </tr>
          </thead>
          <tbody>
            {edges.map((e, i) => {
              const n = nodes.get(other(e))
              if (!n) return null
              const inReport = indexOf(n.id) >= 0
              return (
                <tr key={i} className="hover:bg-surface-hover">
                  <td className="min-w-48 font-mono break-words">
                    {inReport ? (
                      <button
                        type="button"
                        className="link link-hover text-left text-fg"
                        title="Inspect this function"
                        onClick={() => select(n.id)}
                      >
                        {n.name}
                      </button>
                    ) : (
                      <a
                        className="link link-hover text-fg-2"
                        href={routeHref('flow', { node: n.id })}
                        title="Open in the Code Flow graph"
                      >
                        {n.name}
                      </a>
                    )}
                    {n.kind !== 'func' && n.kind !== 'method' && (
                      <span className="ml-1.5 text-[11px] text-fg-3">{n.kind}</span>
                    )}
                  </td>
                  <td className="whitespace-nowrap">
                    <span className={CONF_TONE[e.conf] ?? 'text-fg-2'}>{e.conf}</span>
                    {e.kind !== 'call' && <span className="text-fg-3"> · {e.kind}</span>}
                  </td>
                  <td className="whitespace-nowrap">
                    <LocLink file={e.file} line={e.line} />
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
    </div>
  )
}

export default function CodeFlowTab({ b }: { b: Bundle }) {
  const { flow } = useInspect()
  const id = b.symbol.id
  const idx = useMemo(() => {
    const nodes = new Map<string, FlowNode>()
    for (const n of flow?.nodes ?? []) nodes.set(n.id, n)
    const edges = flow?.edges ?? []
    return {
      nodes,
      callers: edges.filter((e) => e.to === id),
      callees: edges.filter((e) => e.from === id),
    }
  }, [flow, id])

  const bar = (
    <Bar>
      <Basis kind="static">static analysis</Basis>
      <span>Callers and callees from the Code Flow index of this report: possible calls, not observed ones.</span>
    </Bar>
  )
  if (!flow?.nodes?.length) {
    return (
      <div className="space-y-3">
        {bar}
        <Msg>
          This report has no Code Flow index (export it with the Code Flow option).
          {flow?.error && <div className="mt-1 text-fg-3">{flow.error}</div>}
        </Msg>
      </div>
    )
  }
  const self = idx.nodes.get(id)
  if (!self) {
    return (
      <div className="space-y-3">
        {bar}
        <Msg>No matching symbol found in the Code Flow index.</Msg>
      </div>
    )
  }
  return (
    <div className="space-y-3">
      {bar}
      <div className="flex flex-wrap items-center gap-2">
        <a className="btn btn-sm btn-soft btn-primary" href={routeHref('flow', { node: id })}>
          <Icon name="flow" className="size-4" />
          Open in Code Flow graph
        </a>
        {self.tags?.map((t) => (
          <span key={t} className="badge badge-ghost badge-sm">
            {t}
          </span>
        ))}
      </div>
      {self.explain && <Note className="max-w-4xl text-sm">{self.explain}</Note>}
      <div className="grid gap-3 xl:grid-cols-2">
        <Calls title="Called by" edges={idx.callers} other={(e) => e.from} nodes={idx.nodes} />
        <Calls title="Calls" edges={idx.callees} other={(e) => e.to} nodes={idx.nodes} />
      </div>
    </div>
  )
}
