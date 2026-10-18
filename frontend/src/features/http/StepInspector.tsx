// StepInspector: a step of a journey (a node of the dataset) selected on the
// canvas, in tabs: Overview · Connections · Source · Metadata. Relations to
// a step drawn in the journey select it there; the others open in the
// Architecture view.

import { useState, type ReactNode } from 'react'
import { CodeInline } from '../../components/Code'
import { CopyButton } from '../../components/CopyButton'
import { cx, fmt, plural } from '../../lib/format'
import { routeHref } from '../../lib/router'
import {
  CONF_HELP,
  CONF_RANK,
  CONFS,
  FUNC,
  REL,
  STRUCTURAL,
  displayName,
  kindColor,
  kindLabel,
  layerOf,
  reference,
  type Graph,
} from './graph'
import { archHref } from './helpers'
import { NodeSource } from './source'
import type { Snippets } from './sourceData'
import type { GEdge, GNode } from './types'
import { Btn, ConfBadge, ConfBar, Dot, InspectorHead, KV, Note, Row, Section, Tabs, Tag, type KVRow } from './ui'

export function NodeBadges({ n }: { n: GNode }) {
  const lyr = layerOf(n)
  return (
    <>
      <Dot token={kindColor(n)} round={!!FUNC[n.type]} />
      <Tag>{kindLabel(n)}</Tag>
      {n.ext && <Tag>external</Tag>}
      {lyr && lyr !== 'External' && lyr !== 'Route' && lyr !== 'Database' && lyr !== 'Unresolved' && (
        <Tag title={n.meta.layerEvidence || 'role in the handler chain'}>
          {lyr + (n.meta.layerBasis ? ' · ' + n.meta.layerBasis.toLowerCase() : '')}
        </Tag>
      )}
      {n.meta.exported === false && <Tag>unexported</Tag>}
    </>
  )
}

function evidenceText(e: GEdge): string {
  return (
    (e.file ? e.file + ':' + e.line : '') + (e.w > 1 ? ' · ' + e.w + ' sites' : '') + (e.note ? ' · ' + e.note : '')
  )
}

export interface StepNav {
  /** selects a node drawn in the journey; false when it is not drawn */
  select: (i: number) => boolean
}

function relationsOf(g: Graph, i: number) {
  const n = g.N[i]
  return {
    out: n.out.filter((ei) => !STRUCTURAL[g.E[ei].type]),
    inn: n.inn.filter((ei) => !STRUCTURAL[g.E[ei].type]),
  }
}

/** NodeRow opens a node: in the journey when drawn, in Architecture otherwise. */
export function NodeRow({
  g,
  i,
  sub,
  right,
  nav,
}: {
  g: Graph
  i: number
  sub?: string
  right?: ReactNode
  nav: StepNav
}) {
  const n = g.N[i]
  const lead = <Dot token={kindColor(n)} round={!!FUNC[n.type]} />
  const props = { lead, name: displayName(n), sub, right, title: reference(n), mono: true }
  return (
    <Row
      {...props}
      onClick={() => {
        if (!nav.select(i)) location.hash = archHref(n.id)
      }}
    />
  )
}

function Overview({ g, n }: { g: Graph; n: GNode }) {
  const m = n.meta
  const rel = relationsOf(g, n.i)
  const c = n.comm >= 0 ? (g.raw.communities ?? []).find((x) => x[0] === n.comm) : undefined
  const comm = c ? { label: String(c[2]), level: String(c[1]) } : null
  return (
    <>
      {n.sig && (
        <Section title="Signature">
          <CodeInline code={n.sig} block className="rounded-box border border-line bg-code px-2.5 py-2" />
        </Section>
      )}
      {m.sql && (
        <Section title={'SQL' + (m.sqlDynamic ? ' (built at runtime)' : '')}>
          <pre className="scroll-thin overflow-x-auto rounded-box border border-line bg-code px-2.5 py-2 font-mono text-xs whitespace-pre-wrap">
            {String(m.sql)
              .replace(/^\s*\n/, '')
              .replace(/\s+$/, '')}
          </pre>
        </Section>
      )}
      <Section title="Details">
        <KV
          rows={[
            ['Kind', kindLabel(n) + (n.ext ? ' (external)' : '')],
            n.qual && n.qual !== n.name && ['Qualified', n.qual, 'mono'],
            n.type !== 'package' && n.type !== 'module' && n.pkg && ['Package', n.pkg, 'mono'],
            n.file && ['File', n.file, 'mono'],
            n.line > 0 && ['Lines', n.end && n.end !== n.line ? n.line + '–' + n.end : String(n.line)],
            m.receiver && ['Receiver', m.receiver + (m.pointerReceiver ? ' (pointer)' : ' (value)')],
            m.role && ['Role', m.role + ' (registered in a handler chain)'],
            m.layer && [
              'Layer',
              m.layer + ' · ' + (m.layerBasis || '') + (m.layerEvidence ? ' — ' + m.layerEvidence : ''),
            ],
            n.type === 'route' && [
              'Route',
              (m.method || 'ANY') + ' ' + m.path + ' · ' + m.router + (m.partial ? ' · partially resolved' : ''),
            ],
            n.type === 'db' && ['Operation', (m.lib || '') + ' ' + (m.op || '')],
            m.std !== undefined && ['Standard library', m.std ? 'yes' : 'no'],
            m.note && ['Note', m.note],
            comm && ['Community', comm.label + ' (' + comm.level + ' level)'],
          ]}
        />
      </Section>
      <Section title="Relations" count={rel.out.length + rel.inn.length}>
        <KV
          rows={[
            ['Depends on', plural(rel.out.length, 'relation')],
            ['Used by', plural(rel.inn.length, 'relation')],
          ]}
        />
        {rel.out.length + rel.inn.length > 0 && <ConfBar confs={rel.out.concat(rel.inn).map((ei) => g.E[ei].conf)} />}
      </Section>
      {g.N[n.i].type === 'route' && g.routeOfNode.has(n.i) && (
        <Section>
          <Btn icon="globe" href={routeHref('http', { route: n.id })}>
            HTTP journey
          </Btn>
        </Section>
      )}
      {(m.annotations ?? []).length > 0 && (
        <Section title="Annotations" count={m.annotations!.length}>
          <div className="space-y-1">
            {m.annotations!.map((x, k) => (
              <Note key={k}>
                [{x.kind + (x.severity ? '/' + x.severity : '')}] {x.title}
                {x.details ? ' — ' + x.details : ''}
              </Note>
            ))}
          </div>
        </Section>
      )}
    </>
  )
}

function Connections({ g, n, nav }: { g: Graph; n: GNode; nav: StepNav }) {
  const [confs, setConfs] = useState<Record<string, boolean>>({})
  const [all, setAll] = useState<Record<string, boolean>>({})
  const rel = relationsOf(g, n.i)
  const groups: [string, number[], 1 | -1][] = [
    ['Outgoing · depends on', rel.out, 1],
    ['Incoming · used by', rel.inn, -1],
  ]
  return (
    <>
      <Section>
        <div className="mb-2 flex flex-wrap gap-1" role="group" aria-label="Confidence">
          {CONFS.map((c) => (
            <button
              key={c}
              type="button"
              aria-pressed={confs[c] !== false}
              title={CONF_HELP[c]}
              className={cx(
                'btn btn-xs rounded-full',
                confs[c] !== false ? 'btn-neutral' : 'btn-ghost border-line text-fg-3',
              )}
              onClick={() => setConfs({ ...confs, [c]: confs[c] === false })}
            >
              {c}
            </button>
          ))}
        </div>
        <Note>Relations of this element in the dataset, each with the place in the code it was read from.</Note>
      </Section>
      {groups.map(([title, list0, dir]) => {
        const list = list0.filter((ei) => confs[g.E[ei].conf] !== false)
        const by: Record<string, number[]> = {}
        list.forEach((ei) => (by[g.E[ei].type] ??= []).push(ei))
        const other = (e: GEdge) => (dir > 0 ? e.t : e.s)
        return (
          <Section key={title} title={title} count={list.length}>
            {!list.length && (
              <Note>None{rel.out.length + rel.inn.length ? ' with the selected confidences.' : '.'}</Note>
            )}
            {Object.keys(by)
              .sort((a, b) => (REL[a]?.order ?? 99) - (REL[b]?.order ?? 99))
              .map((type) => {
                const r = REL[type] ?? { label: type, inv: type }
                const items = by[type].sort(
                  (a, b) =>
                    CONF_RANK[g.E[b].conf] - CONF_RANK[g.E[a].conf] ||
                    (g.N[other(g.E[a])].name < g.N[other(g.E[b])].name ? -1 : 1),
                )
                const key = dir + ':' + type
                const max = all[key] ? items.length : 60
                return (
                  <div key={type} className="mb-2">
                    <div className="flex items-center gap-1.5 px-1 pb-1 text-[11px] text-fg-3">
                      <span className="font-medium text-fg-2">{dir > 0 ? r.label : r.inv}</span>
                      <span>{items.length}</span>
                    </div>
                    {items.slice(0, max).map((ei) => {
                      const e = g.E[ei]
                      return (
                        <NodeRow
                          key={ei}
                          g={g}
                          i={other(e)}
                          sub={evidenceText(e)}
                          right={<ConfBadge conf={e.conf} />}
                          nav={nav}
                        />
                      )
                    })}
                    {items.length > max && (
                      <button
                        type="button"
                        className="btn btn-ghost btn-xs mt-1"
                        onClick={() => setAll({ ...all, [key]: true })}
                      >
                        Show all {fmt(items.length)}
                      </button>
                    )}
                  </div>
                )
              })}
          </Section>
        )
      })}
    </>
  )
}

function Metadata({ g, n }: { g: Graph; n: GNode }) {
  const keys = Object.keys(n.meta)
    .filter((k) => k !== 'annotations' && k !== 'sql')
    .sort()
  const comm = (g.raw.communities ?? []).find((c) => c[0] === n.comm)
  const parent = n.parent >= 0 ? g.N[n.parent] : undefined
  const link = location.href.replace(/#.*$/, '') + archHref(n.id)
  return (
    <>
      <Section title="Identity">
        <KV
          rows={[
            ['ID', n.id, 'break'],
            ['Reference', reference(n), 'break'],
            n.meta.flowId && ['Code Flow ID', n.meta.flowId, 'break'],
          ]}
        />
        <div className="mt-2 flex gap-1">
          <CopyButton label="ID" text={n.id} className="border-line" />
          <CopyButton
            label="Link"
            title="Copy a link to this element in this page"
            text={link}
            className="border-line"
          />
        </div>
      </Section>
      <Section title="Graph">
        <KV
          rows={[
            ['Degree', String(n.deg)],
            ['Out', String(n.out.length)],
            ['In', String(n.inn.length)],
            ['Community', comm ? `${comm[2]} · #${comm[0]} · ${comm[1]} level · ${comm[3]} members` : 'not clustered'],
            ['Tree parent', parent ? reference(parent) : '', 'break'],
          ]}
        />
      </Section>
      {keys.length > 0 && (
        <Section title="Attributes" count={keys.length}>
          <KV
            rows={keys.map((k) => {
              const val = n.meta[k]
              return [k, typeof val === 'object' ? JSON.stringify(val) : String(val), 'break'] as KVRow
            })}
          />
        </Section>
      )}
      <Section>
        <Note>Everything here comes from static analysis (go/packages, go/types, SSA); nothing was executed.</Note>
      </Section>
    </>
  )
}

export function StepInspector({
  g,
  i,
  nav,
  tab,
  setTab,
  snippets,
  actions,
}: {
  g: Graph
  i: number
  nav: StepNav
  tab: string
  setTab: (t: string) => void
  snippets?: Snippets
  actions?: ReactNode
}) {
  const n = g.N[i]
  const rel = relationsOf(g, i)
  const hasSource = !!(n.file || n.ext) && n.type !== 'package' && n.type !== 'module'
  const cur = tab === 'source' && !hasSource ? 'overview' : tab
  const loc = n.file
    ? n.file + (n.line ? ':' + n.line + (n.end && n.end !== n.line ? '–' + n.end : '') : '')
    : n.type === 'package'
      ? n.name
      : ''
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <InspectorHead
        badges={<NodeBadges n={n} />}
        title={displayName(n)}
        loc={loc}
        actions={
          <>
            <CopyButton label="Name" text={displayName(n)} className="border-line" />
            {n.file && (
              <CopyButton
                label="Path"
                title="Copy file:line"
                text={n.file + (n.line ? ':' + n.line : '')}
                className="border-line"
              />
            )}
            <CopyButton
              label="Reference"
              title="Copy the fully qualified reference"
              text={reference(n)}
              className="border-line"
            />
            {actions}
            <Btn icon="target" href={archHref(n.id)} title="Show it in the Architecture view">
              In architecture
            </Btn>
          </>
        }
      />
      <Tabs
        tabs={[
          { id: 'overview', label: 'Overview' },
          { id: 'connections', label: 'Connections', count: rel.out.length + rel.inn.length },
          ...(hasSource ? [{ id: 'source', label: 'Source' }] : []),
          { id: 'metadata', label: 'Metadata' },
        ]}
        active={cur}
        onChange={setTab}
      />
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto">
        {cur === 'connections' ? (
          <Connections key={i} g={g} n={n} nav={nav} />
        ) : cur === 'source' ? (
          <Section>
            <NodeSource n={n} info={g.raw.sources} snippets={snippets} />
          </Section>
        ) : cur === 'metadata' ? (
          <Metadata g={g} n={n} />
        ) : (
          <Overview g={g} n={n} />
        )}
      </div>
    </div>
  )
}
