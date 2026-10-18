// Inspector (right pane): what is selected, in tabs.
//   element   Overview · Connections · Source · Metadata
//   relation  the relations a drawn line stands for, with their evidence
//   nothing   the repository in numbers, and how it was analysed

import { useMemo, useState, type ReactNode } from 'react'
import { CodeInline } from '../../components/Code'
import { CopyButton } from '../../components/CopyButton'
import { Icon } from '../../components/Icon'
import { PaneHeader } from '../../components/Panes'
import { cx, fmt, plural } from '../../lib/format'
import { routeHref } from '../../lib/router'
import { expandable, realOf } from './arch'
import type { Explorer, Snapshot } from './controller'
import { findingIdOf, nodeLoc, type Model } from './model'
import { SourcePanel } from './SourcePanel'
import type { SceneEdge, SecuritySectionPeek, TItem } from './types'
import { ConfBadge, ConfBar, Glyph, KindDot, KV, Metrics, Note, Section, type KVRow } from './ui'
import {
  CONF_HELP,
  CONF_RANK,
  CONFS,
  FUNC,
  KIND,
  kindPlural,
  layerOf,
  methodClass,
  REL,
  relLabel,
  SEV_CLASS,
  STRUCTURAL,
  SYMBOL,
} from './vocab'

export interface InspectorProps {
  D: Model
  ctrl: Explorer
  snap: Snapshot
  security: SecuritySectionPeek | null
  securityOffered: boolean
  onHide: () => void
}

type ItemTab = 'overview' | 'connections' | 'source' | 'metadata'

export function Inspector(props: InspectorProps) {
  const { snap, ctrl, onHide } = props
  const sel = snap.S.sel
  let body: ReactNode
  if (sel?.kind === 'item') body = <ItemInspector key={'i' + sel.i} {...props} i={sel.i} />
  else if (sel?.kind === 'edge') {
    const e = ctrl.edge(sel.key)
    body = e ? <EdgeInspector key={'e' + sel.key} {...props} e={e} /> : <SummaryInspector {...props} />
  } else body = <SummaryInspector {...props} />
  return (
    <>
      <PaneHeader title="Inspector">
        <button
          type="button"
          className="btn btn-ghost btn-xs btn-square"
          title="Hide the inspector (i)"
          aria-label="Hide the inspector"
          onClick={onHide}
        >
          <Icon name="chevronRight" className="size-3.5" />
        </button>
      </PaneHeader>
      {body}
    </>
  )
}

// ---------- shared parts ----------

function Head({
  badges,
  title,
  loc,
  actions,
}: {
  badges: ReactNode
  title: ReactNode
  loc?: string
  actions?: ReactNode
}) {
  return (
    <div className="shrink-0 border-b border-line px-3 pt-2.5 pb-2">
      <div className="mb-1.5 flex flex-wrap items-center gap-1.5">{badges}</div>
      <div className="text-[15px] leading-snug font-semibold break-words [overflow-wrap:anywhere]">{title}</div>
      {loc && (
        <div className="mt-1 truncate font-mono text-[11.5px] text-fg-3 [direction:rtl]" title={loc}>
          {'‎' + loc + '‎'}
        </div>
      )}
      {actions && <div className="mt-2 flex flex-wrap gap-1">{actions}</div>}
    </div>
  )
}

function KindBadge({ children, title }: { children: ReactNode; title?: string }) {
  return (
    <span className="badge badge-sm badge-ghost rounded-[4px] lowercase" title={title}>
      {children}
    </span>
  )
}

function Tabs<T extends string>({ list, cur, set }: { list: [T, string, number?][]; cur: T; set: (t: T) => void }) {
  return (
    <div
      role="tablist"
      className="tabs tabs-border tabs-sm shrink-0 flex-nowrap overflow-x-auto border-b border-line px-1"
    >
      {list.map(([id, label, count]) => (
        <button
          key={id}
          type="button"
          role="tab"
          aria-selected={cur === id}
          className={cx('tab gap-1.5 text-xs', cur === id && 'tab-active')}
          onClick={() => set(id)}
        >
          {label}
          {count !== undefined && <span className="text-[10.5px] text-fg-3">{fmt(count)}</span>}
        </button>
      ))}
    </div>
  )
}

/** Row: an element of the dataset, clickable (it navigates to it). */
function Row({
  D,
  i,
  sub,
  right,
  onClick,
}: {
  D: Model
  i: number
  sub?: ReactNode
  right?: ReactNode
  onClick: () => void
}) {
  const t = D.T[i]
  return (
    <button
      type="button"
      className="flex w-full min-w-0 cursor-pointer items-center gap-2 rounded-field px-1.5 py-1 text-left text-xs hover:bg-surface-hover focus-visible:bg-surface-hover"
      title={D.reference(t)}
      onClick={onClick}
    >
      <KindDot color={D.kindColor(t)} round={!!FUNC[t.kind]} />
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate">{D.displayName(t)}</span>
        {sub && <span className="truncate font-mono text-[11px] text-fg-3">{sub}</span>}
      </span>
      {right}
    </button>
  )
}

const evidenceText = (e: { file: string; line: number; w: number; note: string }) =>
  (e.file ? e.file + ':' + e.line : '') + (e.w > 1 ? ' · ' + e.w + ' sites' : '') + (e.note ? ' · ' + e.note : '')

const statPairs = (D: Model): [string, number][] => {
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

// ---------- element ----------

function ItemInspector({ D, ctrl, snap, security, securityOffered, i }: InspectorProps & { i: number }) {
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

// The element tab and the confidences shown in Connections are kept while
// the selection changes (per viewer, in memory).
const memory: { tab: ItemTab; confs: Record<string, boolean> } = { tab: 'overview', confs: {} }
function useTabMemory(): [ItemTab, (t: ItemTab) => void] {
  const [tab, setTab] = useState<ItemTab>(memory.tab)
  return [
    tab,
    (t) => {
      memory.tab = t
      setTab(t)
    },
  ]
}

function useConfMemory(): [Record<string, boolean>, (c: Record<string, boolean>) => void] {
  const [confs, setConfs] = useState(memory.confs)
  return [
    confs,
    (c) => {
      memory.confs = c
      setConfs(c)
    },
  ]
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

export function MethodBadge({ method }: { method: string }) {
  return (
    <span
      className={cx(
        'inline-block min-w-11 shrink-0 rounded-[4px] text-center font-mono text-[10px] leading-[18px] font-bold',
        methodClass(method),
      )}
    >
      {method}
    </span>
  )
}

function ItemConnections({ D, ctrl, i }: { D: Model; ctrl: Explorer; i: number }) {
  const t = D.T[i]
  const rel = D.relationsOf(i)
  const container = t.kids.length > 0
  const [confs, setConfs] = useConfMemory()
  const go = (x: number) => void ctrl.selectItem(x, { reveal: true })
  return (
    <>
      <Section>
        <div className="flex flex-wrap gap-1" role="group" aria-label="Confidence">
          {CONFS.map((c) => (
            <button
              key={c}
              type="button"
              aria-pressed={confs[c] !== false}
              title={CONF_HELP[c]}
              className={cx(
                'btn btn-xs rounded-full',
                confs[c] !== false ? 'btn-soft' : 'btn-ghost text-fg-3 line-through',
              )}
              onClick={() => setConfs({ ...confs, [c]: confs[c] === false })}
            >
              {c}
            </button>
          ))}
        </div>
        <Note>
          {container
            ? 'Relations between what this ' +
              D.kindLabel(t) +
              ' contains and the rest, grouped by the package (or file) on the other side.'
            : 'Relations of this element in the dataset, each with the place in the code it was read from.'}
        </Note>
      </Section>
      {(
        [
          ['Outgoing · depends on', rel.out, 1],
          ['Incoming · used by', rel.inn, -1],
        ] as const
      ).map(([title, all, dir]) => {
        const list = all.filter((ei) => confs[D.E[ei].conf] !== false)
        return (
          <Section key={title} title={title} count={list.length}>
            {!list.length ? (
              <Note>{'None' + (rel.out.length + rel.inn.length ? ' with the selected confidences.' : '.')}</Note>
            ) : container ? (
              <GroupedByUnit D={D} list={list} dir={dir} self={i} go={go} />
            ) : (
              <GroupedByType D={D} list={list} dir={dir} go={go} />
            )}
          </Section>
        )
      })}
    </>
  )
}

const otherEnd = (e: { s: number; t: number }, dir: number) => (dir > 0 ? e.t : e.s)

function GroupedByType({ D, list, dir, go }: { D: Model; list: number[]; dir: number; go: (i: number) => void }) {
  const [all, setAll] = useState<Record<string, boolean>>({})
  const by: Record<string, number[]> = {}
  list.forEach((ei) => (by[D.E[ei].type] = by[D.E[ei].type] || []).push(ei))
  return (
    <>
      {Object.keys(by)
        .sort((a, b) => (REL[a]?.order ?? 99) - (REL[b]?.order ?? 99))
        .map((type) => {
          const r = REL[type] ?? { label: type, inv: type }
          const items = by[type].sort(
            (a, b) =>
              CONF_RANK[D.E[b].conf] - CONF_RANK[D.E[a].conf] ||
              (D.T[otherEnd(D.E[a], dir)].label < D.T[otherEnd(D.E[b], dir)].label ? -1 : 1),
          )
          const max = all[type] ? items.length : 60
          return (
            <div key={type} className="mb-3 last:mb-0">
              <div className="flex items-center gap-1.5 px-1 pb-1 text-[11px] text-fg-3">
                <span className="font-medium text-fg-2">{dir > 0 ? r.label : r.inv}</span>
                <span>{items.length}</span>
              </div>
              {items.slice(0, max).map((ei) => {
                const e = D.E[ei],
                  o = otherEnd(e, dir)
                return (
                  <Row
                    key={ei}
                    D={D}
                    i={o}
                    sub={evidenceText(e)}
                    right={<ConfBadge conf={e.conf} />}
                    onClick={() => go(o)}
                  />
                )
              })}
              {items.length > max && (
                <button
                  type="button"
                  className="btn btn-ghost btn-xs mt-1"
                  onClick={() => setAll({ ...all, [type]: true })}
                >
                  Show all {items.length}
                </button>
              )}
            </div>
          )
        })}
    </>
  )
}

function GroupedByUnit({
  D,
  list,
  dir,
  self,
  go,
}: {
  D: Model
  list: number[]
  dir: number
  self: number
  go: (i: number) => void
}) {
  const [all, setAll] = useState(false)
  const units = useMemo(() => {
    const level = D.T[self].kind === 'file' ? 'file' : 'package'
    const by = new Map<number, number[]>()
    list.forEach((ei) => {
      const o = otherEnd(D.E[ei], dir)
      let u = -1
      for (let p = o; p >= 0; p = D.T[p].parent)
        if (D.T[p].kind === level || D.T[p].kind === 'package') {
          u = p
          break
        }
      if (u < 0) u = o
      if (!by.has(u)) by.set(u, [])
      by.get(u)!.push(ei)
    })
    return Array.from(by.entries()).sort(
      (a, b) => b[1].length - a[1].length || (D.T[a[0]].label < D.T[b[0]].label ? -1 : 1),
    )
  }, [D, list, dir, self])
  const max = all ? units.length : 40
  return (
    <>
      {units.slice(0, max).map(([u, items]) => (
        <UnitGroup key={u} D={D} u={u} items={items} dir={dir} go={go} />
      ))}
      {units.length > max && (
        <button type="button" className="btn btn-ghost btn-xs mt-1" onClick={() => setAll(true)}>
          Show all {units.length}
        </button>
      )}
    </>
  )
}

function UnitGroup({
  D,
  u,
  items,
  dir,
  go,
}: {
  D: Model
  u: number
  items: number[]
  dir: number
  go: (i: number) => void
}) {
  const [open, setOpen] = useState(false)
  const types: Record<string, number> = {}
  let best = 'unresolved'
  items.forEach((ei) => {
    const e = D.E[ei]
    types[e.type] = (types[e.type] || 0) + 1
    if (CONF_RANK[e.conf] > CONF_RANK[best]) best = e.conf
  })
  const sum = Object.keys(types)
    .sort((a, b) => types[b] - types[a])
    .map((k) => types[k] + ' ' + relLabel(k))
    .join(' · ')
  const t = D.T[u]
  return (
    <div className="rounded-field">
      <div className="flex items-center gap-0.5">
        <button
          type="button"
          className="btn btn-ghost btn-xs btn-square shrink-0"
          aria-expanded={open}
          aria-label={(open ? 'Hide' : 'Show') + ' the relations with ' + D.displayName(t)}
          onClick={() => setOpen(!open)}
        >
          <Icon name={open ? 'chevronDown' : 'chevronRight'} className="size-3" />
        </button>
        <div className="min-w-0 flex-1">
          <Row D={D} i={u} sub={sum} right={<ConfBadge conf={best} />} onClick={() => go(u)} />
        </div>
      </div>
      {open && (
        <div className="mb-1 ml-6 border-l border-line pl-1.5">
          {items
            .slice()
            .sort((a, b) => CONF_RANK[D.E[b].conf] - CONF_RANK[D.E[a].conf])
            .slice(0, 120)
            .map((ei) => {
              const e = D.E[ei]
              const target = dir > 0 ? e.t : e.s
              return <RelationRow key={ei} D={D} ei={ei} onClick={() => go(target)} />
            })}
          {items.length > 120 && <Note>{items.length - 120} more (all in the JSON export).</Note>}
        </div>
      )}
    </div>
  )
}

/** RelationRow: "from → to" with the kind, evidence and confidence of a relation. */
function RelationRow({ D, ei, onClick }: { D: Model; ei: number; onClick: () => void }) {
  const e = D.E[ei]
  return (
    <button
      type="button"
      className="flex w-full min-w-0 items-center gap-2 rounded-field px-1.5 py-1 text-left text-xs hover:bg-surface-hover"
      title={D.reference(D.T[e.s]) + ' → ' + D.reference(D.T[e.t])}
      onClick={onClick}
    >
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate font-mono">{D.displayName(D.T[e.s]) + ' → ' + D.displayName(D.T[e.t])}</span>
        <span className="truncate font-mono text-[11px] text-fg-3">{relLabel(e.type) + ' · ' + evidenceText(e)}</span>
      </span>
      <ConfBadge conf={e.conf} />
    </button>
  )
}

function ItemMetadata({ D, ctrl, i }: { D: Model; ctrl: Explorer; i: number }) {
  const t = D.T[i],
    n = t.n
  const comm = n && n.comm >= 0 ? D.COMM.get(n.comm) : undefined
  const keys = n
    ? Object.keys(n.meta)
        .filter((k) => k !== 'annotations' && k !== 'sql')
        .sort()
    : []
  return (
    <>
      <Section title="Identity">
        <KV
          rows={[
            [
              'ID',
              <span key="v" className="font-mono break-all">
                {t.id}
              </span>,
            ],
            [
              'Reference',
              <span key="v" className="font-mono break-all">
                {D.reference(t)}
              </span>,
            ],
            n?.meta.flowId
              ? [
                  'Code Flow ID',
                  <span key="v" className="font-mono break-all">
                    {n.meta.flowId}
                  </span>,
                ]
              : null,
          ]}
        />
        <div className="mt-2 flex flex-wrap gap-1">
          <CopyButton label="ID" title="Copy the stable ID" text={t.id} className="btn-soft" />
          <CopyButton
            label="Link"
            title="Copy a link to this element in this page"
            text={() => ctrl.linkTo(i)}
            className="btn-soft"
          />
        </div>
      </Section>
      {n && (
        <Section title="Graph">
          <KV
            rows={[
              ['Degree', String(n.deg)],
              ['Out', String(n.out.length)],
              ['In', String(n.inn.length)],
              [
                'Community',
                comm
                  ? comm.label + ' · #' + n.comm + ' · ' + comm.level + ' level · ' + comm.size + ' members'
                  : 'not clustered',
              ],
              [
                'Tree parent',
                D.T[t.parent] ? (
                  <span key="v" className="font-mono break-all">
                    {D.reference(D.T[t.parent])}
                  </span>
                ) : (
                  ''
                ),
              ],
            ]}
          />
        </Section>
      )}
      {n && keys.length > 0 && (
        <Section title="Attributes" count={keys.length}>
          <KV
            rows={keys.map((k): KVRow => {
              const v = n.meta[k]
              return [
                k,
                <span key="v" className="font-mono break-all">
                  {typeof v === 'object' ? JSON.stringify(v) : String(v)}
                </span>,
              ]
            })}
          />
        </Section>
      )}
      <div className="px-3 pb-3">
        <Note>Everything here comes from static analysis (go/packages, go/types, SSA); nothing was executed.</Note>
      </div>
    </>
  )
}

// ---------- relation (drawn line) ----------

function EdgeInspector({ D, ctrl, e }: InspectorProps & { e: SceneEdge }) {
  const g = e.agg
  const refOf = (k: string) => ctrl.box(k)?.ref ?? -1
  const from = refOf(e.s),
    to = refOf(e.t)
  const real = useMemo(() => realOf(g), [g])
  const list = useMemo(
    () => real.slice().sort((a, b) => CONF_RANK[D.E[b].conf] - CONF_RANK[D.E[a].conf] || a - b),
    [D, real],
  )
  const name = (i: number) => (i >= 0 ? D.displayName(D.T[i]) : '?')
  return (
    <>
      <Head
        badges={
          <>
            <KindBadge>relation</KindBadge>
            <ConfBadge conf={g.best} />
            <KindBadge>{plural(g.n, 'relation')}</KindBadge>
          </>
        }
        title={name(from) + ' → ' + name(to)}
        actions={
          <>
            {from >= 0 && (
              <button type="button" className="btn btn-xs" onClick={() => void ctrl.selectItem(from, { reveal: true })}>
                Select source
              </button>
            )}
            {to >= 0 && (
              <button type="button" className="btn btn-xs" onClick={() => void ctrl.selectItem(to, { reveal: true })}>
                Select target
              </button>
            )}
          </>
        }
      />
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto">
        <Section title="Summary">
          <KV rows={Object.keys(g.types).map((k): KVRow => [relLabel(k), fmt(g.types[k])])} />
          <ConfBar list={real} E={D.E} />
          <Note>
            The line stands for every relation between what the two boxes contain; its style is the strongest confidence
            among them.
          </Note>
        </Section>
        <Section title="Relations" count={real.length}>
          {list.slice(0, 200).map((ei) => (
            <RelationRow key={ei} D={D} ei={ei} onClick={() => void ctrl.selectItem(D.E[ei].t, { reveal: true })} />
          ))}
          {list.length > 200 && <Note>{list.length - 200} more (all in the JSON export).</Note>}
        </Section>
      </div>
    </>
  )
}

// ---------- nothing selected ----------

function SummaryInspector({ D }: InspectorProps) {
  const r = D.raw
  const all = useMemo(() => D.E.filter((e) => !STRUCTURAL[e.type]).map((e) => e.i), [D])
  const src = r.sources
  const comp = r.completeness
  return (
    <>
      <Head
        badges={<KindBadge>repository</KindBadge>}
        title={r.repository}
        loc={D.modules.map((m) => m.name).join(', ')}
      />
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto">
        <Section title="Repository">
          <Metrics items={statPairs(D)} />
        </Section>
        <Section title="Confidence of the relations" count={all.length}>
          <ConfBar list={all} E={D.E} />
          <div className="mt-2">
            {CONFS.map((k) => (
              <Note key={k}>{CONF_HELP[k]}</Note>
            ))}
          </div>
        </Section>
        {comp && comp.status !== 'complete' && (
          <Section title="Completeness">
            <Note warn>The analysis is {comp.status}:</Note>
            <ul className="list-disc pl-4 text-xs text-fg-3">
              {(comp.reasons ?? []).slice(0, 30).map((x, k) => (
                <li key={k} className="my-0.5">
                  {x}
                </li>
              ))}
            </ul>
          </Section>
        )}
        <Section title="How it was computed">
          <ul className="list-disc pl-4 text-xs leading-relaxed text-fg-3">
            {(r.method ?? []).map((x, k) => (
              <li key={k} className="my-1">
                {x}
              </li>
            ))}
          </ul>
          <Note>
            {src?.embedded
              ? 'Source: ' +
                plural(src.files, 'Go file') +
                ' embedded for the Source tab' +
                ((src.skipped ?? []).length ? ' (' + src.skipped!.length + ' left out)' : '') +
                '.'
              : src?.disabled
                ? 'Source not embedded (--include-source=false).'
                : 'No source embedded.'}
          </Note>
        </Section>
        <Section title="Navigate">
          <Note>Click to select, double-click a box to expand it or a symbol to focus on it.</Note>
          <Note>Ctrl+K searches everything; the explorer on the left lists the whole tree.</Note>
          <Note>
            Selecting shows what the element depends on (<span style={{ color: 'var(--hl-out)' }}>blue</span>) and what
            depends on it (<span style={{ color: 'var(--hl-in)' }}>amber</span>).
          </Note>
        </Section>
      </div>
    </>
  )
}
