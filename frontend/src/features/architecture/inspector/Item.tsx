// The inspector of one element: its head and actions, and the tabs
// Overview · Connections · Source · Metadata (Overview is here).

import { useMemo } from 'react'
import { CodeInline } from '../../../components/Code'
import { CopyButton } from '../../../components/CopyButton'
import { Icon } from '../../../components/Icon'
import { cx, plural } from '../../../lib/format'
import { routeHref } from '../../../lib/router'
import { expandable } from '../arch'
import type { Explorer, Snapshot } from '../controller'
import type { InspectorProps } from '../Inspector'
import { findingIdOf, type Model, nodeLoc } from '../model'
import { SourcePanel } from '../SourcePanel'
import type { SecuritySectionPeek, TItem } from '../types'
import { ConfBar, Glyph, KindDot, KV, type KVRow, Metrics, Note, Section } from '../ui'
import { FUNC, KIND, kindPlural, layerOf, SEV_CLASS, SYMBOL } from '../vocab'
import { ItemConnections } from './Connections'
import { type ItemTab, useTabMemory } from './helpers'
import { ItemMetadata } from './Metadata'
import { Head, KindBadge, MethodBadge, Tabs } from './parts'

export function ItemInspector({ D, ctrl, snap, security, securityOffered, i }: InspectorProps & { i: number }) {
  const t = D.T[i],
    n = t.n
  const [tab, setTab] = useTabMemory()
  const rel = D.relationsOf(i)
  const hasSource = !!n && (!!n.file || n.ext) && n.type !== 'package' && n.type !== 'module'
  const cur: ItemTab = tab === 'source' && !hasSource ? 'overview' : tab
  const loc =
    n?.type === 'module' || n?.type === 'package'
      ? n.name
      : n?.file
        ? n.file + (n.line ? ':' + n.line + (n.end && n.end !== n.line ? '–' + n.end : '') : '')
        : t.kind === 'dir'
          ? t.name
          : ''
  return (
    <>
      <Head
        badges={<ItemBadges D={D} t={t} />}
        title={D.displayName(t)}
        loc={loc}
        actions={<ItemActions D={D} ctrl={ctrl} snap={snap} i={i} />}
      />
      <Tabs<ItemTab>
        list={[
          ['overview', 'Overview'],
          ['connections', 'Connections', rel.out.length + rel.inn.length],
          ...(hasSource ? ([['source', 'Source']] as [ItemTab, string][]) : []),
          ['metadata', 'Metadata'],
        ]}
        cur={cur}
        set={setTab}
      />
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto" role="tabpanel">
        {cur === 'overview' && <ItemOverview D={D} i={i} security={security} securityOffered={securityOffered} />}
        {cur === 'connections' && <ItemConnections D={D} ctrl={ctrl} i={i} />}
        {cur === 'source' && n && (
          <div className="p-3">
            <SourcePanel n={n} info={D.raw.sources} snippets={security?.snippets ?? null} />
          </div>
        )}
        {cur === 'metadata' && <ItemMetadata D={D} ctrl={ctrl} i={i} />}
      </div>
    </>
  )
}

function ItemBadges({ D, t }: { D: Model; t: TItem }) {
  const n = t.n
  const lyr = n ? layerOf(n) : ''
  return (
    <>
      <KindDot color={D.kindColor(t)} round={!!FUNC[t.kind]} />
      <KindBadge>{D.kindLabel(t)}</KindBadge>
      {n?.ext && <KindBadge>external</KindBadge>}
      {lyr && lyr !== 'External' && lyr !== 'Route' && lyr !== 'Database' && lyr !== 'Unresolved' && (
        <span className="badge badge-sm badge-outline" title={n?.meta.layerEvidence || 'role in the handler chain'}>
          {lyr + (n?.meta.layerBasis ? ' · ' + n.meta.layerBasis.toLowerCase() : '')}
        </span>
      )}
      {n?.meta.exported === false && <KindBadge>unexported</KindBadge>}
    </>
  )
}

function ItemActions({ D, ctrl, snap, i }: { D: Model; ctrl: Explorer; snap: Snapshot; i: number }) {
  const t = D.T[i],
    n = t.n
  const S = snap.S
  const drawn = ctrl.isDrawn(i)
  const routeK = D.routeOfNode.get(i)
  const btn = 'btn btn-xs gap-1'
  return (
    <>
      <CopyButton label="Name" title="Copy the name" text={() => D.displayName(t)} className="btn-soft" />
      {n?.file && n.type !== 'module' && (
        <CopyButton label="Path" title="Copy file:line" text={() => nodeLoc(n)} className="btn-soft" />
      )}
      <CopyButton
        label="Reference"
        title="Copy the fully qualified reference"
        text={() => D.reference(t)}
        className="btn-soft"
      />
      {!S.focus && expandable(t) && drawn && i !== S.root && (
        <button
          type="button"
          className={btn}
          title={(S.open.has(i) ? 'Collapse' : 'Expand') + ' on the canvas (Enter)'}
          onClick={() => ctrl.toggle(i)}
        >
          <Glyph name={S.open.has(i) ? 'collapse' : 'expand'} className="size-3.5" />
          {S.open.has(i) ? 'Collapse' : 'Expand'}
        </button>
      )}
      {!S.focus && expandable(t) && i !== S.root && !(t.n && SYMBOL[t.kind]) && (
        <button type="button" className={btn} title="Make it the scope of the canvas" onClick={() => ctrl.enter(i)}>
          <Glyph name="scope" className="size-3.5" />
          Open as scope
        </button>
      )}
      {i !== D.ROOT && t.kind !== 'extgroup' && !(S.focus && S.focus.i === i) && (
        <button
          type="button"
          className={btn}
          title="Focus mode: only this element and what it is connected to (.)"
          onClick={() => ctrl.focusOn(i)}
        >
          <Glyph name="focus" className="size-3.5" />
          Focus
        </button>
      )}
      {n?.type === 'route' && routeK !== undefined && (
        <a
          className={cx(btn, 'btn-primary')}
          href={routeHref('http', { route: D.ROUTES[routeK].id })}
          title="Open the route in the HTTP view"
        >
          <Glyph name="http" className="size-3.5" />
          HTTP journey
        </a>
      )}
      {!drawn && (
        <button
          type="button"
          className={btn}
          title="Show it on the canvas"
          onClick={() => void ctrl.selectItem(i, { reveal: true, force: true })}
        >
          <Icon name="target" className="size-3.5" />
          Show on canvas
        </button>
      )}
    </>
  )
}

function ItemOverview({
  D,
  i,
  security,
  securityOffered,
}: {
  D: Model
  i: number
  security: SecuritySectionPeek | null
  securityOffered: boolean
}) {
  const t = D.T[i],
    n = t.n
  const m = n ? n.meta : {}
  const rel = D.relationsOf(i)
  const contains = useMemo(() => {
    if (!t.kids.length) return null
    const c: Record<string, number> = {}
    D.descendants(i, (d) => (c[D.T[d].kind] = (c[D.T[d].kind] || 0) + 1))
    return Object.keys(c)
      .sort((a, b) => (KIND[a]?.order ?? 50) - (KIND[b]?.order ?? 50))
      .slice(0, 9)
      .map((k): [string, number] => [kindPlural(k, c[k]), c[k]])
  }, [D, i, t])
  const container = t.kind === 'package' || t.kind === 'file' || t.kind === 'dir' || t.kind === 'module'
  const routes = useMemo(() => (container && D.ROUTES.length ? D.routesOf(i) : []), [D, i, container])
  const rows: KVRow[] = [['Kind', D.kindLabel(t) + (n?.ext ? ' (external)' : '')]]
  if (n) {
    if (n.qual && n.qual !== n.name)
      rows.push([
        'Qualified',
        <span key="v" className="font-mono">
          {n.qual}
        </span>,
      ])
    if (n.type !== 'package' && n.type !== 'module' && n.pkg)
      rows.push([
        'Package',
        <span key="v" className="font-mono">
          {n.pkg}
        </span>,
      ])
    if (n.type === 'package')
      rows.push([
        'Import path',
        <span key="v" className="font-mono">
          {n.name}
        </span>,
      ])
    if (n.type === 'module' && n.file)
      rows.push([
        'Directory',
        <span key="v" className="font-mono">
          {n.file}
        </span>,
      ])
    else if (n.file && n.type !== 'package')
      rows.push([
        'File',
        <span key="v" className="font-mono">
          {n.file}
        </span>,
      ])
    if (n.line) rows.push(['Lines', n.end && n.end !== n.line ? n.line + '–' + n.end : String(n.line)])
    if (m.receiver) rows.push(['Receiver', m.receiver + (m.pointerReceiver ? ' (pointer)' : ' (value)')])
    if (m.role) rows.push(['Role', m.role + ' (registered in a handler chain)'])
    if (m.layer)
      rows.push(['Layer', m.layer + ' · ' + (m.layerBasis || '') + (m.layerEvidence ? ' — ' + m.layerEvidence : '')])
    if (n.type === 'route')
      rows.push([
        'Route',
        (m.method || 'ANY') +
          ' ' +
          (m.path ?? '') +
          ' · ' +
          (m.router ?? '') +
          (m.partial ? ' · partially resolved' : ''),
      ])
    if (n.type === 'db') rows.push(['Operation', (m.lib || '') + ' ' + (m.op || '')])
    if (m.std !== undefined) rows.push(['Standard library', m.std ? 'yes' : 'no'])
    if (m.note) rows.push(['Note', m.note])
    const comm = n.comm >= 0 ? D.COMM.get(n.comm) : undefined
    if (comm) rows.push(['Community', comm.label + ' (' + comm.level + ' level)'])
  } else if (t.kind === 'dir') rows.push(['Path', t.name])
  const anns = m.annotations ?? []
  return (
    <>
      {n?.sig && (
        <Section title="Signature">
          <CodeInline
            block
            code={n.sig}
            className="rounded-box border border-line bg-code px-2.5 py-2 leading-relaxed"
          />
        </Section>
      )}
      {n && m.sql && (
        <Section title={'SQL' + (m.sqlDynamic ? ' (built at runtime)' : '')}>
          <pre className="scroll-thin overflow-x-auto rounded-box border border-line bg-code px-2.5 py-2 font-mono text-xs leading-relaxed whitespace-pre-wrap">
            {m.sql.replace(/^\s*\n/, '').replace(/\s+$/, '')}
          </pre>
        </Section>
      )}
      <Section title="Details">
        <KV rows={rows} />
      </Section>
      {contains && (
        <Section title="Contains">
          <Metrics items={contains} />
        </Section>
      )}
      <Section title="Relations" count={rel.out.length + rel.inn.length}>
        <KV
          rows={[
            ['Depends on', plural(rel.out.length, 'relation') + (t.kids.length ? ' leaving it' : '')],
            ['Used by', plural(rel.inn.length, 'relation') + (t.kids.length ? ' reaching it' : '')],
          ]}
        />
        {rel.out.length + rel.inn.length > 0 && <ConfBar list={rel.out.concat(rel.inn)} E={D.E} />}
      </Section>
      {routes.length > 0 && (
        <Section title="HTTP routes" count={routes.length}>
          <div className="flex flex-col">
            {routes.slice(0, 40).map((k) => {
              const r = D.ROUTES[k]
              return (
                <a
                  key={r.id}
                  href={routeHref('http', { route: r.id })}
                  className="flex items-center gap-2 rounded-field px-1.5 py-1 text-xs hover:bg-surface-hover"
                  title={'Open ' + r.key + ' in the HTTP view'}
                >
                  <MethodBadge method={r.method} />
                  <span className="min-w-0 flex-1 truncate font-mono">{r.path}</span>
                </a>
              )
            })}
          </div>
          {routes.length > 40 && <Note>{routes.length - 40} more in the HTTP view.</Note>}
        </Section>
      )}
      {anns.length > 0 && (
        <Section title="Annotations" count={anns.length}>
          <ul className="flex flex-col gap-2">
            {anns.map((a, k) => {
              const fid = findingIdOf(a)
              const known =
                !!fid &&
                securityOffered &&
                (!security?.audit?.findings || security.audit.findings.some((f) => f.id === fid))
              return (
                <li key={k} className="rounded-box border border-line bg-surface-2 px-2.5 py-2 text-xs">
                  <div className="flex flex-wrap items-center gap-1.5">
                    {a.severity && (
                      <span className={cx('badge badge-sm border', SEV_CLASS[a.severity] ?? 'badge-ghost')}>
                        {a.severity}
                      </span>
                    )}
                    <span className="badge badge-sm badge-ghost font-mono">{a.kind}</span>
                    {known && (
                      <a
                        className="btn btn-ghost btn-xs ml-auto gap-1"
                        href={routeHref('security', { finding: fid })}
                        title="Open the finding in the Security view"
                      >
                        <Glyph name="shield" className="size-3.5" />
                        Open finding
                      </a>
                    )}
                  </div>
                  <div className="mt-1 font-medium">{a.title}</div>
                  {a.details && <div className="mt-0.5 text-fg-3">{a.details}</div>}
                </li>
              )
            })}
          </ul>
        </Section>
      )}
    </>
  )
}
