// Branch Explorer: the function's control-flow graph (go/cfg), its metrics,
// the structural paths to each exit and, with a coverage profile, which
// blocks ran.

import { useMemo, useRef, useState } from 'react'
import { Icon } from '../../components/Icon'
import { cx } from '../../lib/format'
import { Graph, type GEdge, type GLine, type GNode, type GraphHandle } from './Graph'
import { useKeys } from './keys'
import type { BranchGraph, Bundle, CfgBlock } from './types'
import { Bar, Basis, Dl, FnCode, H4, Kv, LocLink, Msg, Note, Panel, Pre, Row } from './ui'
import { clip } from './util'

function blockLines(b: CfgBlock): GLine[] {
  const head =
    'B' +
    b.id +
    (b.entry ? '  entry' : '') +
    (b.loopHead ? '  loop head' : '') +
    (b.terminal && b.terminal !== 'return' ? '  ' + b.terminal : '')
  const lines: GLine[] = [{ t: head, sub: true }]
  const st = b.stmts ?? []
  st.slice(0, 6).forEach((x) => lines.push({ t: x.text }))
  if (st.length > 6) lines.push({ t: '… ' + (st.length - 6) + ' more', sub: true })
  if (b.decision) {
    const c =
      b.decision === 'if'
        ? 'if ' + b.cond
        : b.decision === 'for' || b.decision === 'range'
          ? 'for ' + b.cond
          : b.decision === 'select'
            ? 'select'
            : 'switch ' + (b.cond ?? '')
    const q = b.decision === 'select' || b.decision === 'switch' || b.decision === 'type switch' ? '' : ' ?'
    lines.push({ t: c + q, b: true })
  }
  if (!st.length && !b.decision) lines.push({ t: b.entry ? '(start)' : '(empty)', sub: true })
  return lines
}

function model(g: BranchGraph): { nodes: GNode[]; edges: GEdge[] } {
  const nodes = g.blocks.map((bl): GNode => {
    const bad = bl.terminal === 'panic' || bl.terminal === 'exit' || bl.returnsError
    let fill = bl.decision ? 'color-mix(in oklab, var(--color-info) 9%, var(--node-fill))' : undefined
    let stroke = bad ? 'var(--color-error)' : undefined
    if (bl.coverage === 'covered') {
      fill = 'color-mix(in oklab, var(--color-success) 14%, var(--node-fill))'
      stroke = 'var(--color-success)'
    } else if (bl.coverage === 'uncovered') {
      fill = 'color-mix(in oklab, var(--color-error) 14%, var(--node-fill))'
      stroke = 'var(--color-error)'
    } else if (bl.coverage === 'partial') {
      stroke = 'var(--color-warning)'
    }
    return {
      id: String(bl.id),
      lines: blockLines(bl),
      fill,
      stroke,
      strokeWidth: bl.terminal ? 2 : 1,
      dash: bl.reachable ? undefined : '4 3',
      opacity: bl.reachable ? 1 : 0.55,
      aria: 'Block ' + bl.id + ' line ' + bl.line,
      last: !bl.reachable,
    }
  })
  const edges = g.edges.map((e, i): GEdge => ({
    id: 'e' + i,
    from: String(e.from),
    to: String(e.to),
    label: e.label || '',
    back: e.kind === 'loop',
    tone: e.error ? 'error' : 'default',
    title: (e.label || e.kind) + (e.error ? ' (error branch)' : ''),
  }))
  return { nodes, edges }
}

function pathLabel(g: BranchGraph, i: number): string {
  const p = g.paths![i]
  const end = g.blocks[p.end]
  const last = end ? (end.stmts?.length ? end.stmts[end.stmts.length - 1].text : end.terminal) : ''
  return (
    i +
    1 +
    '. ' +
    (p.steps?.length ? p.steps.join(' · ') : '(no decisions)') +
    ' ⇒ ' +
    clip(last, 40) +
    (p.error ? '  [error]' : '')
  )
}

export default function BranchesTab({ b }: { b: Bundle }) {
  const g = b.branches
  const bar = (
    <Bar>
      <Basis kind="static">static analysis (go/cfg)</Basis>
      <span>Structural paths: allowed by the code, not observed; some may be infeasible.</span>
    </Bar>
  )
  if (!g) {
    return (
      <div className="space-y-3">
        {bar}
        <Msg warn>{b.errors?.branches || 'No control-flow graph for this function.'}</Msg>
      </div>
    )
  }
  return <Cfg b={b} g={g} bar={bar} />
}

function Cfg({ b, g, bar }: { b: Bundle; g: BranchGraph; bar: React.ReactNode }) {
  const [sel, setSel] = useState<number>(g.entry ?? 0)
  const [path, setPath] = useState(-1)
  const gr = useRef<GraphHandle>(null)
  const m = g.metrics
  const paths = useMemo(() => g.paths ?? [], [g])
  const { nodes, edges } = useMemo(() => model(g), [g])
  const highlight = useMemo(() => {
    const p = path >= 0 ? paths[path] : null
    if (!p) return null
    return { nodes: new Set(p.blocks.map(String)), edges: new Set(p.edges.map((x) => 'e' + x)) }
  }, [path, paths])

  const go = (id: number) => {
    setSel(id)
    gr.current?.center(String(id))
  }
  const stepPath = (d: number) => {
    if (!paths.length) return
    setPath((p) => (p < 0 ? (d > 0 ? 0 : paths.length - 1) : (p + d + paths.length) % paths.length))
  }

  useKeys((k) => {
    if (k === 'p') stepPath(1)
    else if (k === 'P') stepPath(-1)
    else if (k === 'Escape' && path >= 0) setPath(-1)
    else if (k === 'f') gr.current?.fit()
    else if (k === '+' || k === '=') gr.current?.zoomBy(1.25)
    else if (k === '-') gr.current?.zoomBy(0.8)
    else return false
    return true
  })

  const bl = g.blocks[sel]
  const cur = path >= 0 ? paths[path] : null
  const c = g.coverage

  return (
    <div className="@container space-y-2.5">
      {bar}
      <Kv
        items={[
          [m.blocks, 'blocks'],
          [m.edges, 'edges'],
          [m.decisionPoints, 'decisions', 'decision points (if, for, range, switch, select)'],
          [m.cyclomatic, 'cyclomatic', 'cyclomatic complexity: decisions + 1'],
          [m.returns, 'returns'],
          [m.panics, 'panic/exit'],
          [m.loops, 'loops'],
          [m.unreachable, 'unreachable'],
          [m.paths + (m.pathsSaturated ? '+' : ''), 'paths', 'entry→exit paths, each loop repeated at most once'],
        ]}
      />
      {c ? (
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-fg-2">
          <Basis kind="observed">coverage {c.profile}</Basis>
          <span>
            <b className="text-success">{c.covered}</b> covered · <b className="text-error">{c.uncovered}</b> not
            covered · <b className="text-warning">{c.partial}</b> partial · <b className="text-fg">{c.unknown}</b>{' '}
            unknown
          </span>
          <span className="text-fg-3">— {c.note}</span>
        </div>
      ) : (
        <Note className="text-fg-3">No coverage data available.</Note>
      )}
      <Bar>
        <div className="join">
          <button
            type="button"
            className="btn btn-xs join-item border-line"
            title="Previous path (P)"
            aria-label="Previous path"
            onClick={() => stepPath(-1)}
            disabled={!paths.length}
          >
            <Icon name="chevronLeft" className="size-3.5" />
          </button>
          <select
            aria-label="Highlight path"
            className="select select-xs join-item w-auto min-w-48 max-w-[min(640px,60vw)]"
            value={path}
            onChange={(e) => setPath(+e.target.value)}
          >
            <option value={-1}>Highlight a path…</option>
            {paths.map((_, i) => (
              <option key={i} value={i}>
                {pathLabel(g, i)}
              </option>
            ))}
          </select>
          <button
            type="button"
            className="btn btn-xs join-item border-line"
            title="Next path (p)"
            aria-label="Next path"
            onClick={() => stepPath(1)}
            disabled={!paths.length}
          >
            <Icon name="chevronRight" className="size-3.5" />
          </button>
        </div>
        {path >= 0 && (
          <button type="button" className="btn btn-ghost btn-xs" onClick={() => setPath(-1)} title="Clear (Esc)">
            Clear
          </button>
        )}
        {m.pathsListed < m.paths && (
          <span className="text-fg-3">
            {m.pathsListed} of {m.paths}
            {m.pathsSaturated ? '+' : ''} paths listed
          </span>
        )}
      </Bar>
      {cur && (
        <div className="flex flex-wrap items-center gap-1.5 rounded-box border border-primary/40 bg-primary/5 px-3 py-1.5 text-xs">
          <span className="font-semibold text-fg-2">Path {path + 1}</span>
          {(cur.steps?.length ? cur.steps : ['(no decisions)']).map((s, i) => (
            <span key={i} className="flex items-center gap-1.5">
              {i > 0 && <Icon name="chevronRight" className="size-3 text-fg-3" />}
              <span className="font-mono">{s}</span>
            </span>
          ))}
          <span className="text-fg-3">⇒</span>
          <span className={cx('badge badge-sm', cur.error ? 'badge-error badge-soft' : 'badge-ghost')}>
            {cur.exit}
            {cur.error ? ' · error' : ''}
          </span>
          <span className="text-fg-3">· blocks {cur.blocks.map((x) => 'B' + x).join(' → ')}</span>
        </div>
      )}
      <div className="grid gap-3 @4xl:grid-cols-[minmax(0,1fr)_minmax(300px,380px)]">
        <div className="h-[clamp(360px,calc(100vh-330px),760px)] min-w-0">
          <Graph
            ref={gr}
            nodes={nodes}
            edges={edges}
            dir="DOWN"
            selected={String(sel)}
            onSelect={(id) => setSel(+id)}
            highlight={highlight}
            focus={String(g.entry ?? 0)}
            label="Control-flow graph"
          />
        </div>
        <Panel label="Block details" className="@4xl:h-[clamp(360px,calc(100vh-330px),760px)]">
          {bl ? <BlockDetail b={b} g={g} bl={bl} go={go} /> : <Note>Select a block.</Note>}
        </Panel>
      </div>
      <Legend />
      {g.notes?.map((n) => (
        <Note key={n}>{n}</Note>
      ))}
      {g.status?.status && g.status.status !== 'complete' && (
        <Note className="text-warning">
          Result {g.status.status}
          {g.status.reasons?.length ? ': ' + g.status.reasons.join('; ') : ''}
        </Note>
      )}
      {!!g.defers?.length && (
        <Note>
          Deferred calls (run on every exit after registration):{' '}
          {g.defers.map((d) => d.text + ' (line ' + d.line + ')').join('; ')}
        </Note>
      )}
    </div>
  )
}

function BlockDetail({ b, g, bl, go }: { b: Bundle; g: BranchGraph; bl: CfgBlock; go: (id: number) => void }) {
  const outs = g.edges.filter((e) => e.from === bl.id)
  const ins = g.edges.filter((e) => e.to === bl.id)
  const lines = useMemo(() => {
    const out: number[] = []
    for (let l = bl.line; l <= (bl.end || bl.line); l++) out.push(l)
    return out
  }, [bl])
  return (
    <div>
      <div className="text-xs text-fg-3">
        {bl.decision ? bl.decision + ' decision' : 'block'}
        {bl.terminal ? ' · exits: ' + bl.terminal : ''}
        {bl.reachable ? '' : ' · unreachable'}
      </div>
      <h3 className="mt-0.5 font-mono text-[13px] font-semibold">
        B{bl.id}
        <span className="ml-2 font-normal text-fg-2">
          {bl.end && bl.end !== bl.line ? 'lines ' + bl.line + '–' + bl.end : 'line ' + bl.line}
        </span>
      </h3>
      <Dl>
        <Row label="Condition">{bl.cond && <span className="font-mono">{bl.cond}</span>}</Row>
        <Row label="Location">
          <LocLink file={bl.file} line={bl.line} />
        </Row>
        <Row label="Coverage">
          {bl.coverage ? (
            <span
              className={cx(
                bl.coverage === 'covered' && 'text-success',
                bl.coverage === 'uncovered' && 'text-error',
                bl.coverage === 'partial' && 'text-warning',
              )}
            >
              {bl.coverage + (bl.coverCount ? ' (' + bl.coverCount + ')' : '')}
            </span>
          ) : (
            <span className="text-fg-3">no coverage data</span>
          )}
        </Row>
        <Row label="Error check">{bl.errorCheck ? 'yes' : ''}</Row>
        <Row label="Returns an error">{bl.returnsError ? <span className="text-error">yes</span> : ''}</Row>
        <Row label="Defers">{bl.defers ? String(bl.defers) : ''}</Row>
      </Dl>
      {!!bl.stmts?.length && (
        <>
          <H4>Statements</H4>
          <Pre maxHeight="12rem">{bl.stmts.map((x) => String(x.line || '').padEnd(5) + ' ' + x.text).join('\n')}</Pre>
        </>
      )}
      {(!!outs.length || !!ins.length) && (
        <div className="mt-1 grid grid-cols-2 gap-3">
          {!!ins.length && (
            <div>
              <H4>Predecessors</H4>
              <ul className="space-y-0.5 text-xs">
                {ins.map((e, i) => (
                  <li key={i}>
                    <button
                      type="button"
                      className="link link-hover text-left font-mono text-accent-text"
                      onClick={() => go(e.from)}
                    >
                      B{e.from}
                    </button>
                    {e.label && <span className="text-fg-3"> ({e.label})</span>}
                  </li>
                ))}
              </ul>
            </div>
          )}
          {!!outs.length && (
            <div>
              <H4>Successors</H4>
              <ul className="space-y-0.5 text-xs">
                {outs.map((e, i) => (
                  <li key={i}>
                    <button
                      type="button"
                      className={cx('link link-hover font-mono', e.error ? 'text-error' : 'text-accent-text')}
                      onClick={() => go(e.to)}
                    >
                      {(e.label ? e.label + ' → ' : '') + 'B' + e.to}
                    </button>
                    {e.kind === 'loop' && <span className="text-fg-3"> (loop back)</span>}
                    {e.error && <span className="text-fg-3"> (error branch)</span>}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}
      {b.code && (
        <>
          <H4>Code</H4>
          <FnCode code={b.code} from={b.symbol.line ?? 1} mark={lines} maxHeight="20rem" />
        </>
      )}
    </div>
  )
}

function Legend() {
  const sw = (style: React.CSSProperties, label: string) => (
    <span className="flex items-center gap-1.5">
      <i className="inline-block h-2.5 w-3.5 rounded-[3px] border" style={style} />
      {label}
    </span>
  )
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-fg-3">
      {sw(
        {
          background: 'color-mix(in oklab, var(--color-info) 9%, var(--node-fill))',
          borderColor: 'var(--node-border)',
        },
        'decision',
      )}
      {sw({ background: 'var(--node-fill)', borderColor: 'var(--node-border)', borderWidth: 2 }, 'exit')}
      {sw({ background: 'var(--node-fill)', borderColor: 'var(--color-error)' }, 'panic / exit / returns an error')}
      {sw(
        { background: 'var(--node-fill)', borderColor: 'var(--node-border)', borderStyle: 'dashed', opacity: 0.6 },
        'unreachable',
      )}
      {sw(
        {
          background: 'color-mix(in oklab, var(--color-success) 14%, var(--node-fill))',
          borderColor: 'var(--color-success)',
        },
        'covered',
      )}
      {sw(
        {
          background: 'color-mix(in oklab, var(--color-error) 14%, var(--node-fill))',
          borderColor: 'var(--color-error)',
        },
        'not covered',
      )}
      <span className="flex items-center gap-1.5">
        <svg width="22" height="8" aria-hidden="true">
          <line x1="0" y1="4" x2="22" y2="4" stroke="var(--edge-strong)" strokeDasharray="5 4" strokeWidth="1.3" />
        </svg>
        loop back
      </span>
      <span className="flex items-center gap-1.5">
        <svg width="22" height="8" aria-hidden="true">
          <line x1="0" y1="4" x2="22" y2="4" stroke="var(--color-error)" strokeWidth="1.3" />
        </svg>
        error branch
      </span>
    </div>
  )
}
