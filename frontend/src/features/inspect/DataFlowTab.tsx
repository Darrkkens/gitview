// Data Flow: where a variable's value may come from and where it may go
// (SSA def-use chains, across calls up to a depth). Static: possible flows,
// not observed values; each edge carries its confidence.

import { useMemo, useRef, useState } from 'react'
import { cx } from '../../lib/format'
import { useInspect } from './ctx'
import { Graph, type GEdge, type GNode, type GraphHandle } from './Graph'
import { useKeys } from './keys'
import { usePrefs, type PrefValues } from './prefs'
import type { Bundle, DataFlowResult, DfEdge, DfNode } from './types'
import { Bar, Basis, Dl, FnCode, H4, LocLink, Msg, Note, Panel, Row, Seg } from './ui'
import { weaker } from './util'

/**
 * The variable whose trace reaches the most: an input source or a SQL
 * operation first, then a parameter (never the receiver or a context).
 */
function bestVariable(df: Record<string, DataFlowResult>, keys: string[]): string {
  let best = -Infinity
  let pick = keys[0]
  keys.forEach((k, i) => {
    const r = df[k]
    let sc = 0
    for (const n of r.nodes ?? []) {
      if (n.kind === 'source' && n.label.startsWith('HTTP')) sc = Math.max(sc, 4)
      if (n.kind === 'sql') sc = Math.max(sc, 3)
      if (n.kind === 'source') sc = Math.max(sc, 2)
    }
    const ctx = r.selection?.name === 'ctx' || r.selection?.type === 'context.Context'
    const param = r.selection?.kind === 'param' && i > 0 && !ctx
    const score = sc * 10 + (param ? 1 : 0) - (ctx ? 5 : 0)
    if (score > best) {
      best = score
      pick = k
    }
  })
  return pick
}

const EDGE_STYLE: Record<string, Pick<GEdge, 'tone' | 'dash'>> = {
  exact: { tone: 'default' },
  derived: { tone: 'default', dash: '6 3' },
  likely: { tone: 'faint', dash: '6 3' },
  possible: { tone: 'faint', dash: '3 3' },
  unresolved: { tone: 'warn', dash: '1.5 3' },
}

const GLYPH: Record<string, string> = { source: '⇢ ', sql: '⛁ ', check: '✓ ' }

function nodeStyle(n: DfNode, root: boolean): Partial<GNode> {
  const s: Partial<GNode> = {}
  switch (n.kind) {
    case 'source':
      s.fill = 'color-mix(in oklab, var(--color-success) 14%, var(--node-fill))'
      s.stroke = 'var(--color-success)'
      s.round = true
      break
    case 'sql':
      s.stroke = 'var(--k-db)'
      s.strokeWidth = 2
      s.round = true
      break
    case 'check':
      s.stroke = 'var(--color-warning)'
      break
    case 'unresolved':
      s.stroke = 'var(--color-warning)'
      s.dash = '3 3'
      break
    case 'external':
    case 'return':
      s.dash = '2 2'
      break
  }
  if (root) {
    s.stroke = 'var(--color-primary)'
    s.strokeWidth = 2
    s.dash = undefined
  }
  return s
}

type Filters = Pick<PrefValues, 'dfDir' | 'dfDepth' | 'dfHide'>

function model(res: DataFlowResult, p: Filters): { nodes: GNode[]; edges: GEdge[] } {
  const roots = new Set(res.roots ?? [])
  const keep = new Map<number, boolean>()
  for (const n of res.nodes) keep.set(n.id, Math.abs(n.level || 0) <= p.dfDepth)
  let edges: DfEdge[] = res.edges.filter((e) => keep.get(e.from) && keep.get(e.to))
  if (p.dfHide) {
    for (const n of res.nodes) {
      if (!n.intermediate || roots.has(n.id)) continue
      const ins = edges.filter((e) => e.to === n.id && e.from !== n.id)
      const outs = edges.filter((e) => e.from === n.id && e.to !== n.id)
      edges = edges.filter((e) => e.from !== n.id && e.to !== n.id)
      for (const a of ins) {
        for (const b of outs) {
          edges.push({
            from: a.from,
            to: b.to,
            kind: b.kind === 'assign' ? a.kind : b.kind,
            conf: weaker(a.conf, b.conf),
            label: [a.label, b.label].filter(Boolean).join(' · '),
            file: b.file,
            line: b.line,
            dir: b.dir,
          })
        }
      }
      keep.set(n.id, false)
    }
  }
  edges = edges.filter((e) => p.dfDir === 'both' || e.dir === p.dfDir)
  // Nodes connected to the selection in the edges' own direction.
  const reach = new Set<number>(roots)
  let changed = true
  for (let guard = 0; changed && guard < 400; guard++) {
    changed = false
    for (const e of edges) {
      if (e.dir === 'origin' && reach.has(e.to) && !reach.has(e.from)) {
        reach.add(e.from)
        changed = true
      }
      if (e.dir === 'destination' && reach.has(e.from) && !reach.has(e.to)) {
        reach.add(e.to)
        changed = true
      }
    }
  }
  const nodes: GNode[] = []
  for (const n of res.nodes) {
    if (!keep.get(n.id) || !reach.has(n.id)) continue
    const lines: GNode['lines'] = [
      { t: (GLYPH[n.kind] ?? '') + n.label, b: true },
      { t: (n.funcName || '') + (n.line ? '  :' + n.line : ''), sub: true },
    ]
    if (n.var && !n.label.includes(n.var)) lines.splice(1, 0, { t: 'var ' + n.var, sub: true })
    nodes.push({
      id: String(n.id),
      lines,
      title: (n.code || '') + (n.detail ? '\n' + n.detail : ''),
      aria: n.kind + ' ' + n.label,
      ...nodeStyle(n, roots.has(n.id)),
    })
  }
  const gedges = edges
    .filter((e) => reach.has(e.from) && reach.has(e.to))
    .map((e, i): GEdge => ({
      id: 'd' + i,
      from: String(e.from),
      to: String(e.to),
      label: e.kind,
      ...(EDGE_STYLE[e.conf] ?? EDGE_STYLE.possible),
      title: e.kind + ' [' + e.conf + '] ' + (e.label || '') + (e.file ? ' at ' + e.file + ':' + e.line : ''),
    }))
  return { nodes, edges: gedges }
}

export default function DataFlowTab({ b }: { b: Bundle }) {
  const df = b.dataflow ?? {}
  const keys = Object.keys(df)
  const errs = Object.entries(b.errors ?? {}).filter(([k]) => k === 'dataflow' || k.startsWith('dataflow:'))
  const bar = (
    <Bar>
      <Basis kind="static">static analysis (SSA)</Basis>
      <span>Possible flows on some execution path, not observed values. Edge style shows the confidence.</span>
    </Bar>
  )
  if (!keys.length) {
    return (
      <div className="space-y-3">
        {bar}
        <Msg>
          {b.variables?.length
            ? 'No data flow traces were computed for this function in this report.'
            : 'This function has no variables to trace.'}
        </Msg>
        {errs.map(([k, v]) => (
          <Note key={k} className="text-warning">
            {v}
          </Note>
        ))}
      </div>
    )
  }
  return <Flow fn={b.symbol.id} df={df} keys={keys} bar={bar} errs={errs} />
}

function Flow({
  fn,
  df,
  keys,
  bar,
  errs,
}: {
  fn: string
  df: Record<string, DataFlowResult>
  keys: string[]
  bar: React.ReactNode
  errs: [string, string][]
}) {
  const { p, set } = usePrefs()
  // The variable chosen for this function survives tab switches.
  const chosen = p.dfVar[fn]
  const v = chosen && df[chosen] ? chosen : bestVariable(df, keys)
  const setV = (k: string) => set({ dfVar: { ...p.dfVar, [fn]: k } })
  const res = df[v] ?? df[keys[0]]
  const maxDepth = res.maxDepth || 3
  const depth = Math.min(p.dfDepth, maxDepth)
  const { nodes, edges } = useMemo(
    () => model(res, { dfDir: p.dfDir, dfDepth: depth, dfHide: p.dfHide }),
    [res, p.dfDir, depth, p.dfHide],
  )
  const byId = useMemo(() => new Map(res.nodes.map((n) => [String(n.id), n])), [res])
  const root = String((res.roots ?? [0])[0] ?? 0)
  const [picked, setPicked] = useState<{ root: string; id: string } | null>(null)
  const sel = picked && picked.root === root ? picked.id : root
  const setSel = (id: string) => setPicked({ root, id })
  const gr = useRef<GraphHandle>(null)
  const varRef = useRef<HTMLSelectElement>(null)

  const go = (id: string) => {
    setSel(id)
    gr.current?.center(id)
  }

  useKeys((k) => {
    if (k === 'o') set({ dfDir: 'origin' })
    else if (k === 'b') set({ dfDir: 'both' })
    else if (k === 'd') set({ dfDir: 'destination' })
    else if (k === 'h') set({ dfHide: !p.dfHide })
    else if (k === '>' || k === '.') set({ dfDepth: Math.min(depth + 1, maxDepth) })
    else if (k === '<' || k === ',') set({ dfDepth: Math.max(depth - 1, 0) })
    else if (k === 'v') varRef.current?.focus()
    else if (k === 'f') gr.current?.fit()
    else if (k === '+' || k === '=') gr.current?.zoomBy(1.25)
    else if (k === '-') gr.current?.zoomBy(0.8)
    else return false
    return true
  })

  const sources = (res.sources ?? []).map((id) => byId.get(String(id))).filter(Boolean) as DfNode[]
  const n = byId.get(sel)

  return (
    <div className="@container space-y-2.5">
      {bar}
      <Bar>
        <label className="flex items-center gap-1.5">
          Variable
          <select
            ref={varRef}
            aria-label="Variable"
            className="select select-xs w-auto max-w-80 font-mono"
            value={v}
            onChange={(e) => setV(e.target.value)}
          >
            {keys.map((k) => {
              const s = df[k].selection
              return (
                <option key={k} value={k}>
                  {(s.kind === 'param' ? 'param ' : s.kind === 'return' ? 'result ' : '') +
                    s.name +
                    ' ' +
                    (s.type || '') +
                    '  (line ' +
                    s.line +
                    ')'}
                </option>
              )
            })}
          </select>
        </label>
        <Seg
          label="Direction"
          value={p.dfDir}
          onChange={(d) => set({ dfDir: d })}
          options={[
            ['origin', 'Trace Origin', 'Where the value comes from (o)'],
            ['both', 'Trace Both', 'Origin and destination (b)'],
            ['destination', 'Trace Destination', 'Where the value may go (d)'],
          ]}
        />
        <label className="flex items-center gap-1.5" title="Function boundaries crossed (< >)">
          Call depth
          <select
            aria-label="Depth"
            className="select select-xs w-14"
            value={depth}
            onChange={(e) => set({ dfDepth: +e.target.value })}
          >
            {Array.from({ length: maxDepth + 1 }, (_, i) => (
              <option key={i} value={i}>
                {i}
              </option>
            ))}
          </select>
        </label>
        <label className="flex cursor-pointer items-center gap-1.5" title="Hide intermediate SSA values (h)">
          <input
            type="checkbox"
            className="checkbox checkbox-xs"
            checked={p.dfHide}
            onChange={(e) => set({ dfHide: e.target.checked })}
          />
          Hide intermediate values
        </label>
        <span className="text-fg-3">
          {nodes.length} of {res.nodes.length} nodes shown
        </span>
      </Bar>
      <div className="grid gap-3 @4xl:grid-cols-[minmax(0,1fr)_minmax(300px,380px)]">
        <div className="h-[clamp(360px,calc(100vh-320px),760px)] min-w-0">
          {nodes.length ? (
            <Graph
              ref={gr}
              nodes={nodes}
              edges={edges}
              dir="AUTO"
              maxWidth={260}
              selected={sel}
              onSelect={setSel}
              focus={root}
              label="Data flow graph"
            />
          ) : (
            <Msg>Nothing to show with these filters: try another direction or a larger call depth.</Msg>
          )}
        </div>
        <Panel label="Details" className="@4xl:h-[clamp(360px,calc(100vh-320px),760px)]">
          {n ? <NodeDetail n={n} res={res} go={go} /> : <Note>Select a node.</Note>}
        </Panel>
      </div>
      <ConfLegend />
      <div className="space-y-1">
        {!!res.validations?.length && (
          <Note>
            <b className="font-semibold text-fg">
              {res.validations.length} validation{res.validations.length === 1 ? '' : 's'}:
            </b>{' '}
            {res.validations.map((x, i) => (
              <span key={i}>
                {i > 0 && '; '}
                <button
                  type="button"
                  className="link link-hover text-left font-mono"
                  onClick={() => go(String(x.node))}
                >
                  {x.text}
                </button>{' '}
                <span className="text-fg-3">({x.func})</span>
              </span>
            ))}
          </Note>
        )}
        {!!sources.length && (
          <Note>
            <b className="font-semibold text-fg">Sources:</b>{' '}
            {sources.map((s, i) => (
              <span key={s.id}>
                {i > 0 && '; '}
                <button type="button" className="link link-hover text-left" onClick={() => go(String(s.id))}>
                  {s.label}
                </button>
              </span>
            ))}
          </Note>
        )}
        {res.status && res.status.status !== 'complete' && (
          <Note className="text-warning">
            Result {res.status.status}
            {res.status.reasons?.length ? ': ' + res.status.reasons.join('; ') : ''}.
          </Note>
        )}
        {res.notes?.map((x) => (
          <Note key={x}>{x}</Note>
        ))}
        {errs.map(([k, x]) => (
          <Note key={k} className="text-warning">
            {x}
          </Note>
        ))}
      </div>
    </div>
  )
}

function NodeDetail({ n, res, go }: { n: DfNode; res: DataFlowResult; go: (id: string) => void }) {
  const byId = useMemo(() => new Map(res.nodes.map((x) => [x.id, x])), [res])
  const ins = res.edges.filter((e) => e.to === n.id)
  const outs = res.edges.filter((e) => e.from === n.id)
  const { indexOf, select } = useInspect()
  return (
    <div>
      <div className="text-xs text-fg-3">
        {n.kind}
        {n.dir ? ' · ' + n.dir : ''} · confidence <span className={confClass(n.conf)}>{n.conf}</span>
      </div>
      <h3 className="mt-0.5 font-mono text-[13px] font-semibold break-all">{n.label}</h3>
      <Dl>
        <Row label="Variable">{n.var && <span className="font-mono">{n.var}</span>}</Row>
        <Row label="Type">{n.type && <span className="font-mono">{n.type}</span>}</Row>
        <Row label="Function">
          {n.funcName &&
            (n.func && indexOf(n.func) >= 0 ? (
              <button
                type="button"
                className="link link-hover text-left font-mono"
                title="Inspect this function"
                onClick={() => select(n.func!)}
              >
                {n.funcName}
              </button>
            ) : (
              <span className="font-mono">{n.funcName}</span>
            ))}
        </Row>
        <Row label="Location">{n.file ? <LocLink file={n.file} line={n.line} /> : null}</Row>
        <Row label="Call level">{String(n.level || 0)}</Row>
        <Row label="Detail">{n.detail}</Row>
        <Row label="Tags">{n.tags?.join(', ')}</Row>
      </Dl>
      {n.code && (
        <>
          <H4>Go code responsible</H4>
          <FnCode code={n.code} from={n.line || 1} mark={n.line ? [n.line] : undefined} maxHeight="10rem" />
        </>
      )}
      {(
        [
          ['Comes from', ins, (e: DfEdge) => e.from],
          ['Flows to', outs, (e: DfEdge) => e.to],
        ] as const
      ).map(([title, list, other]) =>
        list.length ? (
          <div key={title}>
            <H4>{title}</H4>
            <ul className="space-y-1 text-xs">
              {list.map((e, i) => {
                const o = byId.get(other(e))
                if (!o) return null
                return (
                  <li key={i} className="break-words">
                    <button
                      type="button"
                      className="link link-hover text-left font-mono text-accent-text"
                      onClick={() => go(String(o.id))}
                    >
                      {o.label}
                    </button>{' '}
                    <span className="text-fg-2">{e.kind}</span>{' '}
                    <span className={cx('text-[11px]', confClass(e.conf))}>[{e.conf}]</span>{' '}
                    <span className="text-fg-3">{e.label}</span>
                  </li>
                )
              })}
            </ul>
          </div>
        ) : null,
      )}
    </div>
  )
}

function confClass(c: string): string {
  switch (c) {
    case 'exact':
      return 'text-c-confirmed'
    case 'derived':
      return 'text-c-likely'
    case 'likely':
      return 'text-c-likely'
    case 'possible':
      return 'text-c-possible'
    case 'unresolved':
      return 'text-c-unresolved'
  }
  return 'text-fg-2'
}

function ConfLegend() {
  const line = (stroke: string, dash: string | undefined, label: string, title: string) => (
    <span className="flex items-center gap-1.5" title={title}>
      <svg width="24" height="8" aria-hidden="true">
        <line x1="0" y1="4" x2="24" y2="4" style={{ stroke }} strokeDasharray={dash} strokeWidth="1.5" />
      </svg>
      {label}
    </span>
  )
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-fg-3">
      <span className="font-semibold">Confidence</span>
      {line('var(--edge-strong)', undefined, 'exact', 'SSA def-use, parameter binding, return')}
      {line('var(--edge-strong)', '6 3', 'derived', 'computed from the origin (conversion, arithmetic, library call)')}
      {line('var(--edge)', '6 3', 'likely', 'interface with one implementation')}
      {line('var(--edge)', '3 3', 'possible', 'one of several implementations, or memory that may alias')}
      {line('var(--color-warning)', '1.5 3', 'unresolved', 'the analysis cannot follow the value')}
      <span className="ml-2 flex items-center gap-1.5">
        <i
          className="inline-block h-2.5 w-3.5 rounded-full border"
          style={{
            background: 'color-mix(in oklab, var(--color-success) 14%, var(--node-fill))',
            borderColor: 'var(--color-success)',
          }}
        />
        source
      </span>
      <span className="flex items-center gap-1.5">
        <i className="inline-block h-2.5 w-3.5 rounded-full border-2" style={{ borderColor: 'var(--k-db)' }} />
        SQL
      </span>
      <span className="flex items-center gap-1.5">
        <i className="inline-block h-2.5 w-3.5 rounded-[3px] border" style={{ borderColor: 'var(--color-warning)' }} />
        check
      </span>
      <span className="flex items-center gap-1.5">
        <i
          className="inline-block h-2.5 w-3.5 rounded-[3px] border-2"
          style={{ borderColor: 'var(--color-primary)' }}
        />
        traced variable
      </span>
    </div>
  )
}
