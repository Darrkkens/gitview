import { Fragment, useMemo } from 'react'
import { useStudy } from '../context'
import { BasisBadge, Card, Empty, EvidenceList, LayerBadge, LocLink, NodeName, Note, PathText, SectionTitle } from '../ui'
import { LayerGraph } from './LayerGraph'

export function Architecture() {
  const { model, links } = useStudy()
  const A = model.architecture ?? {}
  const chains = A.chains ?? []
  const edges = A.layerEdges ?? []
  const fns = useMemo(() => (A.functions ?? []).filter((f) => f.basis === 'STATIC'), [A.functions])

  return (
    <>
      <SectionTitle>Paths from the routes to the data</SectionTitle>
      <Note className="mb-3">
        Each path is the shortest chain of calls in the static call graph from a route's handler to a database operation
        (or, failing that, an outbound HTTP call). Layers come from structure (STATIC) or, when no structural signal
        exists, from names and paths (HEURISTIC).
      </Note>
      {!chains.length && <Empty>No HTTP route was found in the call graph.</Empty>}
      <div className="grid gap-2 lg:grid-cols-2">
        {chains.map((c, ci) => (
          <Card key={ci}>
            <h4 className="mb-2 font-mono text-[13px] font-semibold break-all">{c.title}</h4>
            <ol className="flex flex-col items-start">
              {c.steps.map((s, i) => (
                <Fragment key={i}>
                  {i > 0 && (
                    <li className="my-0.5 ml-4 flex items-baseline gap-1.5 border-l border-dashed border-line-strong py-0.5 pl-3 text-[11px] text-fg-3">
                      <span aria-hidden>↓</span>
                      {s.via && (
                        <>
                          <span>{s.via.text}</span>
                          <LocLink
                            file={s.via.file}
                            line={s.via.line}
                            href={links.href(s.via.ref)}
                            className="text-[11px]"
                          />
                        </>
                      )}
                    </li>
                  )}
                  <li className="flex max-w-full flex-wrap items-center gap-1.5 rounded-field border border-line bg-surface-2 px-2 py-1 text-xs">
                    <LayerBadge layer={s.layer} />
                    <BasisBadge basis={s.basis} />
                    <NodeName id={s.id} name={s.name} area="architecture" trackId={c.route} className="font-semibold" />
                    <LocLink file={s.file} line={s.line} />
                  </li>
                </Fragment>
              ))}
            </ol>
            {c.note && <Note className="mt-2">{c.note}</Note>}
          </Card>
        ))}
      </div>

      {edges.length > 0 && (
        <>
          <SectionTitle>Calls between layers</SectionTitle>
          <Note className="mb-2">
            Layers in their display order; arcs on the right are calls down the stack, arcs on the left calls up the
            stack. Hover a layer or an arc to highlight it; an arc opens an example call in Code Flow.
          </Note>
          <LayerGraph edges={edges} order={model.layers ?? []} />
        </>
      )}

      <SectionTitle>Packages</SectionTitle>
      {!(A.packages ?? []).length ? (
        <Empty>No package was classified.</Empty>
      ) : (
        <div className="overflow-x-auto rounded-box border border-line bg-base-100">
          <table className="table table-xs [&_tr]:text-xs">
            <thead>
              <tr>
                <th>Package</th>
                <th>Layer</th>
                <th>Why</th>
                <th className="text-right">Functions</th>
              </tr>
            </thead>
            <tbody>
              {(A.packages ?? []).map((p) => (
                <tr key={p.path} className="align-top">
                  <td className="min-w-40 font-mono break-words">
                    <PathText text={p.path} />
                  </td>
                  <td>
                    <div className="flex items-center gap-1 whitespace-nowrap">
                      <LayerBadge layer={p.layer} />
                      <BasisBadge basis={p.basis} />
                    </div>
                  </td>
                  <td className="min-w-64">
                    <EvidenceList items={p.evidence} />
                  </td>
                  <td className="text-right font-mono">{p.functions}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {fns.length > 0 && (
        <>
          <SectionTitle>Functions classified by their structure</SectionTitle>
          <div className="overflow-x-auto rounded-box border border-line bg-base-100">
            <table className="table table-xs [&_tr]:text-xs">
              <thead>
                <tr>
                  <th>Function</th>
                  <th>Layer</th>
                  <th>Evidence</th>
                </tr>
              </thead>
              <tbody>
                {fns.map((f) => (
                  <tr key={f.id} className="align-top">
                    <td className="min-w-56">
                      <NodeName id={f.id} name={f.name} area="architecture" />
                    </td>
                    <td>
                      <div className="flex items-center gap-1 whitespace-nowrap">
                        <LayerBadge layer={f.layer} />
                        <BasisBadge basis={f.basis} />
                      </div>
                    </td>
                    <td className="min-w-64">
                      <EvidenceList items={f.evidence} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      {(A.notes ?? []).map((n, i) => (
        <Note key={i} className="mt-2">
          {n}
        </Note>
      ))}
    </>
  )
}
