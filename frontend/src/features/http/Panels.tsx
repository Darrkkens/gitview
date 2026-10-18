// Inspector panels of the HTTP view besides the route: the catalog summary
// (no route selected) and a drawn line (the relations it stands for).

import { CopyButton } from '../../components/CopyButton'
import { fmt, plural } from '../../lib/format'
import type { JourneyEdge } from './build'
import { CONF_RANK, REL, displayName, reference, type Graph } from './graph'
import { archHref } from './helpers'
import type { StepNav } from './StepInspector'
import { Btn, ConfBadge, ConfBar, InspectorHead, KV, Metrics, Note, Row, Section, Tag } from './ui'

export function HttpSummary({ g }: { g: Graph }) {
  const R = g.routes,
    H = g.http
  const auth = R.filter((r) => r.auth?.recognized).length,
    partial = R.filter((r) => r.status !== 'complete').length
  const cmds = [
    'gitview graph export --format openapi --output openapi.json',
    'gitview graph export --format postman --output collection.json',
  ]
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <InspectorHead badges={<Tag>HTTP</Tag>} title="HTTP endpoints" />
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto">
        <Section title="Catalog">
          <Metrics
            items={[
              [fmt(R.length), 'Routes'],
              [fmt(auth), 'With auth'],
              [fmt(partial), 'Partial'],
            ]}
          />
        </Section>
        {(H?.servers ?? []).length > 0 && (
          <Section title="Listen addresses" count={H!.servers!.length}>
            <div className="space-y-1">
              {H!.servers!.map((x, k) => (
                <div key={k} className="text-xs">
                  <span className="font-mono">{x.addr}</span>
                  {x.url && <span className="font-mono text-accent-text"> → {x.url}</span>}
                  <div className="font-mono text-[11px] text-fg-3">
                    {(x.evidence.details || '') + ' (' + x.evidence.file + ':' + x.evidence.line + ')'}
                    {x.dynamic ? ' · computed at runtime' : ''}
                  </div>
                </div>
              ))}
            </div>
          </Section>
        )}
        {(H?.notes ?? []).length > 0 && (
          <Section title="Notes">
            <div className="space-y-1">
              {H!.notes!.map((x, k) => (
                <Note key={k} warn>
                  {x}
                </Note>
              ))}
            </div>
          </Section>
        )}
        <Section>
          <Note>
            Pick an endpoint on the left: the canvas draws the code it may run, the inspector its contract and a request
            you can copy as cURL. Nothing is sent.
          </Note>
        </Section>
        <Section title="OpenAPI and Postman">
          <Note className="mb-1.5">The contracts can be exported from the terminal:</Note>
          <div className="space-y-1">
            {cmds.map((c) => (
              <div key={c} className="flex items-center gap-1">
                <code className="scroll-thin min-w-0 flex-1 overflow-x-auto rounded-field border border-line bg-code px-2 py-1 font-mono text-[11px] whitespace-nowrap">
                  {c}
                </code>
                <CopyButton text={c} title="Copy the command" />
              </div>
            ))}
          </div>
        </Section>
      </div>
    </div>
  )
}

function evidenceText(g: Graph, ei: number) {
  const e = g.E[ei]
  return (
    (e.file ? e.file + ':' + e.line : '') + (e.w > 1 ? ' · ' + e.w + ' sites' : '') + (e.note ? ' · ' + e.note : '')
  )
}

export function EdgeInspector({ g, e, nav }: { g: Graph; e: JourneyEdge; nav: StepNav }) {
  const a = e.agg
  const from = g.N[a.a],
    to = g.N[a.b]
  const list = a.real.slice().sort((x, y) => CONF_RANK[g.E[y].conf] - CONF_RANK[g.E[x].conf] || x - y)
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <InspectorHead
        badges={
          <>
            <Tag>relation</Tag>
            <ConfBadge conf={a.best} />
            <Tag>{plural(a.n, 'relation')}</Tag>
          </>
        }
        title={displayName(from) + ' → ' + displayName(to)}
        actions={
          <>
            <Btn onClick={() => nav.select(a.a)}>Select source</Btn>
            <Btn onClick={() => nav.select(a.b)}>Select target</Btn>
          </>
        }
      />
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto">
        <Section title="Summary">
          <KV rows={Object.keys(a.types).map((k) => [REL[k]?.label ?? k, fmt(a.types[k])] as [string, string])} />
          <ConfBar confs={a.real.map((ei) => g.E[ei].conf)} />
          <Note className="mt-2">
            The line stands for every relation between the two steps; its style is the strongest confidence among them.
          </Note>
        </Section>
        <Section title="Relations" count={list.length}>
          {list.slice(0, 200).map((ei) => {
            const x = g.E[ei]
            return (
              <Row
                key={ei}
                name={displayName(g.N[x.s]) + ' → ' + displayName(g.N[x.t])}
                sub={(REL[x.type]?.label ?? x.type) + ' · ' + evidenceText(g, ei)}
                right={<ConfBadge conf={x.conf} />}
                title={reference(g.N[x.t])}
                mono
                onClick={() => {
                  if (!nav.select(x.t)) location.hash = archHref(g.N[x.t].id)
                }}
              />
            )
          })}
          {list.length > 200 && <Note>{list.length - 200} more (all in the JSON export).</Note>}
        </Section>
      </div>
    </div>
  )
}
