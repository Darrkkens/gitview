// Route inspector, in tabs: Overview · Request · Response · Flow · Source ·
// Evidence. The request builder edits a synthetic request built from the
// static contract and shows it as cURL; nothing is ever sent.

import { useState, type ReactNode } from 'react'
import { CopyButton } from '../../components/CopyButton'
import { Icon } from '../../components/Icon'
import { download } from '../../lib/browser'
import { cx, plural } from '../../lib/format'
import { routeHref } from '../../lib/router'
import type { JourneyView } from './build'
import { CONF_RANK, LAYER_COLOR, layerColor, layerOf, nodeLoc, type Graph } from './graph'
import {
  curlOf,
  fileName,
  requestOf,
  requestURL,
  resetRequest,
  saveRequest,
  type KV as KVItem,
  type Request,
} from './requests'
import { NodeSource } from './source'
import type { Snippets } from './sourceData'
import { ROUTE_TABS, archHref } from './helpers'
import { NodeRow, type StepNav } from './StepInspector'
import type { PageEvidence, Route, Shape } from './types'
import { Btn, ConfBadge, Dot, InspectorHead, KV, Lock, Method, Note, Section, Tabs, Tag } from './ui'

const evLoc = (ev?: PageEvidence[]) => (ev && ev.length ? ev[0].file + ':' + ev[0].line : '')

export interface SecurityLink {
  /** findings of the audit reaching the route; null when not loaded */
  count: number | null
}

// ---------- overview ----------

function ChainList({ g, r, nav }: { g: Graph; r: Route; nav: StepNav }) {
  const mws = r.middlewares ?? []
  const all = mws.concat(r.handler ? [r.handler] : [])
  return (
    <ol className="space-y-0.5">
      {all.map((m, k) => {
        const i = m.id ? (g.byId.get(m.id) ?? -1) : -1
        const mw = k < mws.length
        const role = mw
          ? 'middleware · ' + (m.scope || '') + (m.addedFile ? ' · added at ' + m.addedFile + ':' + m.addedLine : '')
          : 'handler' + (m.file ? ' · ' + m.file + ':' + m.line : '')
        return (
          <li key={k}>
            <button
              type="button"
              className="flex w-full items-center gap-2 rounded-field px-1.5 py-1 text-left hover:bg-surface-hover disabled:hover:bg-transparent"
              disabled={i < 0}
              onClick={() => i >= 0 && !nav.select(i) && (location.hash = archHref(g.N[i].id))}
            >
              <span className="grid size-[18px] shrink-0 place-items-center rounded-full bg-surface-3 text-[10.5px] text-fg-2">
                {k + 1}
              </span>
              <Dot token={i >= 0 ? layerColor(g.N[i]) : 'k-unresolved'} round />
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="truncate font-mono text-xs">{m.name}</span>
                <span className="truncate text-[11px] text-fg-3">
                  {role}
                  {m.note ? ' · ' + m.note : ''}
                </span>
              </span>
              <ConfBadge conf={m.confidence} />
            </button>
          </li>
        )
      })}
    </ol>
  )
}

function authText(r: Route): string {
  const a = r.auth
  if (!a) return 'no middleware or handler of the chain answers 401 or reads a credential'
  return (
    (a.recognized ? 'recognised' : 'not recognised') +
    (a.scheme ? ' · ' + a.scheme : '') +
    (a.credential ? ' · ' + a.credential : '') +
    (a.by ? ' · by ' + a.by : '') +
    (a.note ? ' · ' + a.note : '')
  )
}

function Overview({
  g,
  r,
  view,
  nav,
  security,
}: {
  g: Graph
  r: Route
  view: JourneyView | null
  nav: StepNav
  security?: SecurityLink
}) {
  const hi = r.handler?.id ? (g.byId.get(r.handler.id) ?? -1) : -1
  const byLayer: Record<string, number[]> = {}
  view?.scene.boxes.forEach((b) => {
    if (b.layer && b.layer !== 'Route' && b.ref !== undefined) (byLayer[b.layer] ??= []).push(b.ref)
  })
  return (
    <>
      <Section title="Contract">
        <KV
          rows={[
            [
              'Handler',
              r.handler ? (
                <span key="v" className="inline-flex flex-wrap items-center gap-1.5">
                  {hi >= 0 ? (
                    <button
                      type="button"
                      className="link font-mono link-hover text-accent-text"
                      title="Select it in the journey"
                      onClick={() => !nav.select(hi) && (location.hash = archHref(g.N[hi].id))}
                    >
                      {r.handler.name}
                    </button>
                  ) : (
                    <span className="font-mono">{r.handler.name}</span>
                  )}
                  <ConfBadge conf={r.handler.confidence} />
                </span>
              ) : (
                'not resolved'
              ),
            ],
            ['Registered', r.file + ':' + r.line + ' (' + r.framework + ')', 'mono'],
            ['Group', r.group || '/', 'mono'],
            ['Status', r.status + (r.reasons && r.reasons.length ? ' — ' + r.reasons.join('; ') : '')],
            r.unresolvedCalls > 0 && [
              'Unresolved',
              r.unresolvedCalls + ' call(s) without a static target in the chain',
            ],
            ['Auth', authText(r)],
          ]}
        />
      </Section>
      {security && (
        <Section title="Security">
          <div className="flex items-center gap-2">
            <Note className="flex-1">
              {security.count === null
                ? 'The CodeAudit Security Journey of this page lists what its rules found on this route.'
                : security.count
                  ? plural(security.count, 'finding') + ' of the security audit may reach this route.'
                  : 'No finding of the security audit reaches this route.'}
            </Note>
            <Btn icon="shield" href={routeHref('security', { route: r.id })}>
              Security Journey
            </Btn>
          </div>
        </Section>
      )}
      <Section title="Handler chain" count={(r.middlewares ?? []).length + (r.handler ? 1 : 0)}>
        <ChainList g={g} r={r} nav={nav} />
      </Section>
      {view &&
        ['Middleware', 'Handler', 'Service', 'Repository', 'Database', 'External API', 'External', 'Unresolved'].map(
          (l) => {
            const list = byLayer[l]
            if (!list) return null
            return (
              <Section key={l} title={l === 'Database' ? 'SQL operations' : l} count={list.length}>
                {list.slice(0, 30).map((i) => {
                  const n = g.N[i]
                  const sub = n.meta.sql
                    ? String(n.meta.sql).replace(/\s+/g, ' ').trim().slice(0, 120)
                    : (n.meta.layerBasis ? n.meta.layerBasis + ' · ' : '') + nodeLoc(n)
                  return <NodeRow key={i} g={g} i={i} sub={sub} nav={nav} />
                })}
                {list.length > 30 && <Note>{list.length - 30} more</Note>}
              </Section>
            )
          },
        )}
      {view && (
        <Section>
          <Note>
            Layers come from the code's structure (STATIC) or, when it says nothing, from directory names (HEURISTIC).
            Only what the drawn graph reaches is listed: raise the depth or expand a step to follow more calls.
          </Note>
        </Section>
      )}
    </>
  )
}

// ---------- schemas ----------

interface ShapeRow {
  path: string
  type: string
  required?: string
  rules?: string
  src?: string
  span?: boolean
}

function shapeRows(
  defs: Record<string, Shape>,
  sh0: Shape | undefined,
  prefix: string,
  enforced: string[],
  depth: number,
  seen: Record<string, true>,
  out: ShapeRow[],
) {
  let sh = sh0
  if (!sh || depth > 6) return
  if (sh.ref) {
    if (seen[sh.ref]) {
      out.push({ path: prefix || '(root)', type: (sh.name ?? sh.ref) + ' (recursive)', span: true })
      return
    }
    seen = { ...seen, [sh.ref]: true }
    sh = defs[sh.ref] ?? sh
  }
  if (sh.type === 'array' && sh.items) {
    shapeRows(defs, sh.items, prefix + '[]', enforced, depth + 1, seen, out)
    return
  }
  for (const f of sh.fields ?? []) {
    const path = prefix ? prefix + '.' + f.name : f.name
    const fs = f.schema ?? { type: 'unknown' }
    const rs = (fs.ref && defs[fs.ref]) || fs
    const type = rs.type + (rs.format ? '/' + rs.format : '') + (fs.nullable ? ' | null' : '')
    const req = (f.rules ?? []).some((x) => x.name === 'required' && enforced.includes(x.source))
    const declared = (f.rules ?? []).some((x) => x.name === 'required')
    out.push({
      path,
      type: type + (rs.name && rs.type === 'object' ? ' ' + rs.name : ''),
      required: req ? 'yes' : declared ? 'declared, not applied' : 'no',
      rules: (f.rules ?? []).map((x) => x.name + (x.param ? '=' + x.param : '')).join(', '),
      src:
        (f.goName || '') +
        (f.embedded ? ' (from ' + f.embedded + ')' : '') +
        (f.conditional ? ' · some paths only' : ''),
    })
    if (rs.type === 'object' || rs.type === 'array') shapeRows(defs, fs, path, enforced, depth + 1, seen, out)
  }
  if (sh.values)
    out.push({ path: (prefix || '') + '{key}', type: (sh.values.type || '') + ' (map values)', span: true })
  if (sh.partial) out.push({ path: '…', type: sh.note || 'more keys may be present', span: true })
}

function ShapeTable({ g, shape, enforced }: { g: Graph; shape: Shape; enforced?: string[] }) {
  const rows: ShapeRow[] = []
  shapeRows(g.defs, shape, '', enforced ?? [], 0, {}, rows)
  if (!rows.length) return <Note>No field identified.</Note>
  return (
    <div className="scroll-thin overflow-x-auto rounded-box border border-line">
      <table className="table table-xs">
        <thead>
          <tr>
            <th>Field</th>
            <th>Type</th>
            <th>Required</th>
            <th>Rules</th>
            <th>Go field</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((x, k) =>
            x.span ? (
              <tr key={k}>
                <td className="font-mono">{x.path}</td>
                <td colSpan={4} className="text-fg-2">
                  {x.type}
                </td>
              </tr>
            ) : (
              <tr key={k}>
                <td className="font-mono whitespace-nowrap">{x.path}</td>
                <td>{x.type}</td>
                <td className={cx(x.required === 'yes' ? 'text-fg' : 'text-fg-3')}>{x.required}</td>
                <td className="font-mono">{x.rules}</td>
                <td className="text-fg-2">{x.src}</td>
              </tr>
            ),
          )}
        </tbody>
      </table>
    </div>
  )
}

// ---------- request ----------

function KVTable({
  title,
  list,
  kind,
  r,
  onChange,
}: {
  title: string
  list: KVItem[]
  kind: string
  r: Route
  onChange: (list: KVItem[]) => void
}) {
  const set = (k: number, p: Partial<KVItem>) => onChange(list.map((x, j) => (j === k ? { ...x, ...p } : x)))
  return (
    <Section
      title={title}
      count={list.length}
      extra={
        <button
          type="button"
          className="btn btn-ghost btn-xs"
          onClick={() => onChange([...list, { k: '', v: '', on: true }])}
        >
          <Icon name="plus" className="size-3" /> Add
        </button>
      }
    >
      {!list.length ? (
        <Note>none read by the code</Note>
      ) : (
        <div className="space-y-1.5">
          {list.map((x, k) => {
            const note = x.note
              ? x.note.source +
                (x.note.from && r.handler && x.note.from !== r.handler.name ? ' · read by ' + x.note.from : '') +
                ' · required: ' +
                x.note.required +
                (x.note.default ? ' · default ' + x.note.default : '')
              : ''
            return (
              <div key={k}>
                <div className="flex items-center gap-1.5">
                  <input
                    type="checkbox"
                    className="checkbox checkbox-xs"
                    checked={x.on}
                    aria-label={'Send ' + x.k}
                    onChange={(e) => set(k, { on: e.target.checked })}
                  />
                  <input
                    type="text"
                    className="input input-xs w-2/5 font-mono"
                    value={x.k}
                    aria-label={kind + ' name'}
                    onChange={(e) => set(k, { k: e.target.value })}
                  />
                  <input
                    type="text"
                    className="input input-xs min-w-0 flex-1 font-mono"
                    value={x.v}
                    aria-label={kind + ' value'}
                    onChange={(e) => set(k, { v: e.target.value })}
                  />
                </div>
                {note && <div className="mt-0.5 pl-6 text-[11px] text-fg-3">{note}</div>}
              </div>
            )
          })}
        </div>
      )}
    </Section>
  )
}

function RequestTab({
  g,
  r,
  bodyView,
  setBodyView,
}: {
  g: Graph
  r: Route
  bodyView: string
  setBodyView: (v: string) => void
}) {
  const [q, setQ] = useState<Request>(() => requestOf(r))
  const update = (p: Partial<Request>) => {
    const next = { ...q, ...p }
    saveRequest(r, next)
    setQ(next)
  }
  const url = requestURL(r, q)
  const curl = curlOf(r, q)
  const pathParams = (r.params ?? []).filter((x) => x.in === 'path')
  return (
    <>
      <Section title="Request">
        <div className="mb-1.5 flex items-center gap-1.5 rounded-field border border-line bg-surface-2 p-1">
          <Method method={r.method} />
          <input
            readOnly
            className="min-w-0 flex-1 bg-transparent px-1 font-mono text-xs outline-none"
            value={url}
            aria-label="Request URL"
          />
          <CopyButton text={url} title="Copy the URL" />
        </div>
        <label className="input input-sm mb-1.5 w-full">
          <span className="label text-[11px]">Base URL</span>
          <input
            type="text"
            className="font-mono text-xs"
            value={q.base}
            aria-label="Base URL"
            onChange={(e) => update({ base: e.target.value })}
          />
        </label>
        <Note>
          Base URL: {r.example ? r.example.baseUrlSource : 'placeholder'}. Values are synthetic; edit them freely. This
          page never sends the request.
        </Note>
      </Section>
      {pathParams.length > 0 && (
        <Section title="Path parameters" count={pathParams.length}>
          <div className="space-y-1.5">
            {pathParams.map((x) => (
              <div key={x.name} className="flex items-center gap-1.5">
                <span className="w-24 shrink-0 truncate font-mono text-xs" title={x.name}>
                  {x.name}
                </span>
                <input
                  type="text"
                  className="input input-xs min-w-0 flex-1 font-mono"
                  value={q.path[x.name] ?? ''}
                  aria-label={x.name}
                  onChange={(e) => update({ path: { ...q.path, [x.name]: e.target.value } })}
                />
                <span className="max-w-[45%] truncate text-[11px] text-fg-3" title={x.note}>
                  {(x.schema?.type || 'string') + ' · ' + x.source + (x.note ? ' · ' + x.note : '')}
                </span>
              </div>
            ))}
          </div>
        </Section>
      )}
      <KVTable title="Query parameters" list={q.query} kind="Query" r={r} onChange={(query) => update({ query })} />
      <KVTable title="Headers" list={q.headers} kind="Header" r={r} onChange={(headers) => update({ headers })} />
      {r.auth?.recognized && (
        <Section>
          <Note>
            Authorization: placeholder only ({r.auth.scheme || 'unknown scheme'}); GitView never exports credentials.
          </Note>
        </Section>
      )}
      <Section
        title="Body"
        extra={
          <div className="join">
            {['example', 'schema'].map((v) => (
              <button
                key={v}
                type="button"
                aria-pressed={bodyView === v}
                className={cx('btn join-item btn-xs', bodyView === v ? 'btn-neutral' : 'btn-ghost border-line')}
                onClick={() => setBodyView(v)}
              >
                {v === 'example' ? 'Example' : 'Schema'}
              </button>
            ))}
          </div>
        }
      >
        {!r.body ? (
          <Note>The chain decodes no request body.</Note>
        ) : (
          <div className="space-y-1.5">
            <Note>
              {(r.body.contentTypes ?? []).join(', ') +
                ' · read with ' +
                r.body.source +
                ' at ' +
                evLoc(r.body.evidence) +
                (r.body.enforces && r.body.enforces.length
                  ? ' · applies ' + r.body.enforces.join(', ') + ' tags'
                  : ' · applies no validation tag') +
                (r.body.disallowUnknown ? ' · rejects unknown fields' : '') +
                ((r.body.notes ?? []).length ? ' · ' + r.body.notes!.join('; ') : '')}
            </Note>
            {bodyView === 'schema' ? (
              r.body.schema ? (
                <ShapeTable g={g} shape={r.body.schema} enforced={r.body.enforces} />
              ) : (
                <Note>No schema identified.</Note>
              )
            ) : (
              <>
                <textarea
                  className="textarea textarea-sm min-h-40 w-full bg-code font-mono text-xs leading-relaxed"
                  spellCheck={false}
                  aria-label="Request body"
                  value={q.body}
                  onChange={(e) => update({ body: e.target.value })}
                />
                {r.example && (
                  <Note warn={r.example.valid !== 'compatible'}>
                    {r.example.valid === 'compatible'
                      ? 'Synthetic example compatible with the recognised validation rules.'
                      : 'Synthetic example; it may not satisfy every rule: ' + (r.example.notes ?? []).join('; ')}
                  </Note>
                )}
              </>
            )}
          </div>
        )}
      </Section>
      <Section title="cURL">
        <pre className="scroll-thin overflow-x-auto rounded-box border border-line bg-code px-2.5 py-2 font-mono text-xs whitespace-pre">
          {curl}
        </pre>
        <div className="mt-2 flex flex-wrap gap-1">
          <CopyButton label="cURL" text={curl} className="border-line" />
          {q.body && <CopyButton label="JSON" text={q.body} className="border-line" />}
          <Btn
            icon="download"
            title="Save the command as a .sh file"
            onClick={() =>
              download(
                fileName(r, '.sh'),
                '#!/bin/sh\n# ' +
                  r.key +
                  ' — generated by GitView; synthetic values, review before running.\n' +
                  curl +
                  '\n',
                'text/x-shellscript',
              )
            }
          >
            Export cURL
          </Btn>
          {q.body && (
            <Btn icon="download" onClick={() => download(fileName(r, '.json'), q.body + '\n', 'application/json')}>
              Export JSON
            </Btn>
          )}
          <Btn icon="refresh" onClick={() => setQ(resetRequest(r))} title="Back to the request built from the contract">
            Reset
          </Btn>
        </div>
      </Section>
    </>
  )
}

// ---------- response ----------

const STATUS_CLASS: Record<string, string> = {
  '2': 'text-c-confirmed',
  '3': 'text-c-likely',
  '4': 'text-c-possible',
  '5': 'text-c-unresolved',
}

function ResponseTab({ g, r }: { g: Graph; r: Route }) {
  const [open, setOpen] = useState<Record<number, boolean>>({})
  if (!(r.responses ?? []).length)
    return (
      <Section>
        <Note warn>No response identified statically.</Note>
      </Section>
    )
  return (
    <Section>
      <Note className="mb-2">
        Statuses the chain may answer with, found in the code. static: status and body identified · partial: one of them
        is not · unknown: neither. None of them was observed at runtime.
      </Note>
      <div className="space-y-2">
        {r.responses!.map((x, k) => {
          const name = x.schema ? x.schema.name || x.schema.goType || x.schema.type : 'no body'
          const hasSchema = !!(x.schema && (x.schema.fields || x.schema.ref || x.schema.items))
          return (
            <div key={k} className="rounded-box border border-line bg-surface-2">
              <div className="flex flex-wrap items-center gap-2 px-2.5 py-1.5">
                <span
                  className={cx(
                    'rounded bg-surface-3 px-1.5 font-mono text-[11px] leading-[18px] font-bold',
                    STATUS_CLASS[String(x.status || 0).charAt(0)] ?? 'text-fg-3',
                  )}
                >
                  {x.status ? String(x.status) : '???'}
                </span>
                <span className="text-xs">{x.status ? x.statusText : 'status not determinable statically'}</span>
                {x.contentType && <span className="font-mono text-[11px] text-fg-3">{x.contentType}</span>}
                <span className="flex-1" />
                <ConfBadge
                  conf={x.basis === 'static' ? 'confirmed' : x.basis === 'partial' ? 'possible' : 'unresolved'}
                  label={x.basis}
                  title={
                    'Basis: ' +
                    x.basis +
                    ' (static: status and body identified · partial: one of them is not · unknown: neither)'
                  }
                />
                <ConfBadge conf={x.confidence} />
              </div>
              <div className="space-y-0.5 px-2.5 pb-2">
                <div className="text-[11px] text-fg-2">
                  {name +
                    ' · ' +
                    x.source +
                    ' · from ' +
                    x.from +
                    (x.via && x.via.length ? ' → ' + x.via.join(' → ') : '')}
                </div>
                <div className="font-mono text-[11px] text-fg-3">
                  {evLoc(x.evidence) +
                    ((x.evidenceTotal ?? 0) > (x.evidence ?? []).length
                      ? ' · ' + x.evidenceTotal + ' evidence entries (all in the JSON export)'
                      : '') +
                    (x.implicit ? ' · implicit status' : '') +
                    (x.note ? ' · ' + x.note : '')}
                </div>
                {hasSchema && (
                  <details
                    className="group mt-1"
                    open={!!open[k]}
                    onToggle={(e) => {
                      const o = (e.target as HTMLDetailsElement).open
                      if (o !== !!open[k]) setOpen({ ...open, [k]: o })
                    }}
                  >
                    <summary className="cursor-pointer text-xs text-accent-text select-none">
                      Schema and synthetic example
                    </summary>
                    <div className="mt-1.5 space-y-1.5">
                      <ShapeTable g={g} shape={x.schema!} />
                      {x.example && (
                        <pre className="scroll-thin overflow-x-auto rounded-box border border-line bg-code px-2.5 py-2 font-mono text-xs">
                          {x.example}
                        </pre>
                      )}
                    </div>
                  </details>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </Section>
  )
}

// ---------- flow ----------

interface FlowLine {
  i: number
  text: string
  meta: string
  site: string
}

function flowLines(g: Graph, view: JourneyView): FlowLine[] {
  const out: FlowLine[] = []
  const drawnOut = new Map<number, JourneyView['edges']>()
  view.edges.forEach((e) => {
    const a = e.agg.a
    if (!drawnOut.has(a)) drawnOut.set(a, [])
    drawnOut.get(a)!.push(e)
  })
  const printed = new Set<number>()
  const line = (i: number, e: JourneyView['edges'][number] | null, indent: string, mark: string) => {
    const n = g.N[i],
      l = layerOf(n)
    const re = e ? g.E[e.agg.real[0]] : undefined
    const site = re ? re.file + ':' + re.line : nodeLoc(n)
    out.push({
      i,
      text: indent + (e ? '→ ' : '') + n.name,
      meta: (l ? '  [' + l + ']' : '') + (e ? '  ' + e.conf : '') + mark,
      site,
    })
  }
  const walk = (i: number, indent: string, path: Set<number>, d: number) => {
    for (const e of (drawnOut.get(i) ?? []).filter((x) => !x.agg.types.handles)) {
      const t = e.agg.b
      if (path.has(t)) {
        line(t, e, indent, '  ↻')
        continue
      }
      if (printed.has(t)) {
        line(t, e, indent, '  (above)')
        continue
      }
      printed.add(t)
      line(t, e, indent, view.more.has(t) ? '  ⋯' : '')
      path.add(t)
      if (d < 40) walk(t, indent + '   ', path, d + 1)
      path.delete(t)
    }
  }
  ;(drawnOut.get(view.root) ?? [])
    .filter((e) => e.agg.types.handles)
    .sort((a, b) => (view.chainPos.get(a.agg.b) || 0) - (view.chainPos.get(b.agg.b) || 0))
    .forEach((e) => {
      const t = e.agg.b
      printed.add(t)
      line(t, null, '', '')
      walk(t, '   ', new Set([t]), 0)
    })
  return out
}

function FlowTab({
  g,
  view,
  depth,
  collapseAll,
  nav,
}: {
  g: Graph
  view: JourneyView | null
  depth: number
  collapseAll: () => void
  nav: StepNav
}) {
  const lines = view ? flowLines(g, view) : []
  return (
    <Section
      title="Call tree"
      extra={
        <button type="button" className="btn btn-ghost btn-xs border-line" onClick={collapseAll}>
          Collapse all
        </button>
      }
    >
      <Note className="mb-2">
        The handler chain and what it calls, as drawn (depth {depth}). ↻ marks recursion, ⋯ collapsed calls. Every line
        is a possible path found by static analysis.
      </Note>
      <div className="scroll-thin overflow-x-auto rounded-box border border-line bg-code px-1 py-1.5 font-mono text-xs leading-[1.7] whitespace-pre">
        {lines.map((l, k) => (
          <button
            key={k}
            type="button"
            title={l.site}
            className="block w-full rounded px-1 text-left hover:bg-surface-hover"
            onClick={() => !nav.select(l.i) && (location.hash = archHref(g.N[l.i].id))}
          >
            {l.text}
            <span className="text-fg-3">{l.meta}</span>
          </button>
        ))}
        {!lines.length && <span className="px-1 text-fg-3">The chain has no resolved handler or middleware.</span>}
      </div>
    </Section>
  )
}

// ---------- evidence ----------

function EvidenceTab({ r }: { r: Route }) {
  const rows: [string, string, string, string, string][] = [
    ['Registration', r.key, r.file + ':' + r.line, r.framework, 'confirmed'],
  ]
  for (const m of r.middlewares ?? [])
    rows.push([
      'Middleware (' + (m.scope || '') + ')',
      m.name,
      m.addedFile ? m.addedFile + ':' + m.addedLine : '',
      m.note || '',
      m.confidence,
    ])
  if (r.handler)
    rows.push([
      'Handler',
      r.handler.name,
      r.handler.file ? r.handler.file + ':' + r.handler.line : '',
      r.handler.note || '',
      r.handler.confidence,
    ])
  for (const x of r.params ?? [])
    for (const ev of (x.evidence ?? []).slice(0, 3))
      rows.push(['Param ' + x.in, x.name, ev.file + ':' + ev.line, ev.details || x.source, x.confidence])
  if (r.body)
    for (const ev of (r.body.evidence ?? []).slice(0, 4))
      rows.push(['Body', r.body.source, ev.file + ':' + ev.line, ev.details || '', r.body.confidence])
  for (const x of r.responses ?? [])
    for (const ev of (x.evidence ?? []).slice(0, 4))
      rows.push(['Response ' + (x.status || '?'), x.source, ev.file + ':' + ev.line, ev.details || '', x.confidence])
  if (r.auth)
    for (const ev of r.auth.evidence ?? [])
      rows.push([
        'Auth',
        r.auth.credential || '',
        ev.file + ':' + ev.line,
        ev.details || '',
        r.auth.recognized ? 'recognised' : '—',
      ])
  return (
    <Section title="Evidence" count={rows.length}>
      <ul className="divide-y divide-line rounded-box border border-line">
        {rows.map((rw, k) => (
          <li key={k} className="px-2.5 py-1.5">
            <div className="flex items-center gap-2">
              <span className="w-28 shrink-0 text-[11px] text-fg-3">{rw[0]}</span>
              <span className="min-w-0 flex-1 truncate font-mono text-xs" title={rw[1]}>
                {rw[1]}
              </span>
              {CONF_RANK[rw[4]] ? <ConfBadge conf={rw[4]} /> : <span className="text-[11px] text-fg-3">{rw[4]}</span>}
            </div>
            {(rw[2] || rw[3]) && (
              <div className="mt-0.5 flex gap-2 pl-30 text-[11px]">
                <span className="min-w-0 truncate font-mono text-fg-2" title={rw[2]}>
                  {rw[2]}
                </span>
                {rw[3] && (
                  <span className="min-w-0 truncate text-fg-3" title={rw[3]}>
                    {rw[3]}
                  </span>
                )}
              </div>
            )}
          </li>
        ))}
      </ul>
      <Note className="mt-2">
        Every row is static evidence (go/ssa, go/types): where the relation was read in the code.
      </Note>
    </Section>
  )
}

// ---------- source ----------

function SourceTab({ g, r, k, snippets }: { g: Graph; r: Route; k: number; snippets?: Snippets }) {
  const ri = g.nodeOfRoute[k]
  const hi = r.handler?.id ? (g.byId.get(r.handler.id) ?? -1) : -1
  return (
    <>
      {ri >= 0 && (
        <Section title="Registration">
          <NodeSource n={g.N[ri]} info={g.raw.sources} snippets={snippets} />
        </Section>
      )}
      {hi >= 0 && (
        <Section
          title={
            <>
              Handler · <span className="font-mono normal-case">{r.handler!.name}</span>
            </>
          }
        >
          <NodeSource n={g.N[hi]} info={g.raw.sources} snippets={snippets} />
        </Section>
      )}
      {ri < 0 && hi < 0 && (
        <Section>
          <Note>Neither the registration nor the handler is a node of the dataset.</Note>
        </Section>
      )}
    </>
  )
}

// ---------- inspector ----------

export function RouteInspector({
  g,
  k,
  view,
  tab,
  setTab,
  nav,
  depth,
  collapseAll,
  snippets,
  security,
  bodyView,
  setBodyView,
}: {
  g: Graph
  k: number
  view: JourneyView | null
  tab: string
  setTab: (t: string) => void
  nav: StepNav
  depth: number
  collapseAll: () => void
  snippets?: Snippets
  security?: SecurityLink
  bodyView: string
  setBodyView: (v: string) => void
}) {
  const r = g.routes[k]
  const ri = g.nodeOfRoute[k]
  const cur = ROUTE_TABS.includes(tab) ? tab : 'overview'
  let body: ReactNode
  if (cur === 'request') body = <RequestTab key={r.id} g={g} r={r} bodyView={bodyView} setBodyView={setBodyView} />
  else if (cur === 'response') body = <ResponseTab key={r.id} g={g} r={r} />
  else if (cur === 'flow') body = <FlowTab g={g} view={view} depth={depth} collapseAll={collapseAll} nav={nav} />
  else if (cur === 'source') body = <SourceTab g={g} r={r} k={k} snippets={snippets} />
  else if (cur === 'evidence') body = <EvidenceTab r={r} />
  else body = <Overview g={g} r={r} view={view} nav={nav} security={security} />
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <InspectorHead
        prefix={<Method method={r.method} className="relative -top-px" />}
        title={r.path}
        loc={r.file + ':' + r.line}
        badges={
          <>
            <Dot token={LAYER_COLOR.Route} round />
            <Tag>HTTP route</Tag>
            <Tag>{r.framework}</Tag>
            {r.status === 'complete' ? (
              <Tag tone="ok">contract complete</Tag>
            ) : (
              <Tag tone="warn" title={(r.reasons ?? []).join('; ')}>
                contract {r.status}
              </Tag>
            )}
            {r.auth?.recognized && (
              <Tag tone="ok">
                <Lock className="size-3" />
                auth: {r.auth.scheme || 'recognised'}
              </Tag>
            )}
          </>
        }
        actions={
          <>
            <CopyButton label="Path" text={r.path} className="border-line" />
            <CopyButton label="cURL" text={() => curlOf(r, requestOf(r))} className="border-line" />
            {ri >= 0 && (
              <Btn icon="target" href={archHref(g.N[ri].id)} title="Show the registration in the architecture">
                In architecture
              </Btn>
            )}
            {security && (
              <Btn icon="shield" href={routeHref('security', { route: r.id })} title="Security posture of this route">
                Security{security.count ? ' · ' + security.count : ''}
              </Btn>
            )}
          </>
        }
      />
      <Tabs
        tabs={[
          { id: 'overview', label: 'Overview' },
          { id: 'request', label: 'Request' },
          { id: 'response', label: 'Response', count: (r.responses ?? []).length },
          { id: 'flow', label: 'Flow' },
          { id: 'source', label: 'Source' },
          { id: 'evidence', label: 'Evidence' },
        ]}
        active={cur}
        onChange={setTab}
      />
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto">{body}</div>
    </div>
  )
}
