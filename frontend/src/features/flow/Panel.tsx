// Details of the selected function: signature, location, route or database
// facts, actions (set as root, open in diff, inspector), the explanation,
// SQL, calls and callers, and its code.

import { memo, useMemo, type ReactNode } from 'react'
import { CodeInline } from '../../components/Code'
import { CopyButton } from '../../components/CopyButton'
import { Icon } from '../../components/Icon'
import { tokenize, tokenLines } from '../../lib/go'
import { cx } from '../../lib/format'
import { CONF_LABEL, KIND_LABEL, type FlowIndex } from './graph'
import type { FlowEdge, FlowNode } from './types'

const MAX_REL = 60

export interface Links {
  /** "Open in diff" target, or null when the definition is not in the diff. */
  diff: (n: FlowNode) => string | null
  inspect: (n: FlowNode) => string | null
  architecture: (n: FlowNode) => string | null
}

function Row({ k, children }: { k: string; children: ReactNode }) {
  if (!children) return null
  return (
    <>
      <dt className="text-fg-3">{k}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </>
  )
}

function Snippet({ code, first }: { code: string; first: number }) {
  const rows = useMemo(() => tokenLines(tokenize(code)), [code])
  return (
    <div className="scroll-thin max-h-[420px] overflow-auto rounded-field border border-line bg-code font-mono text-[11.5px] leading-[18px]">
      <table className="min-w-full border-collapse [tab-size:4]">
        <tbody>
          {rows.map((toks, i) => (
            <tr key={i}>
              <td className="sticky left-0 w-px bg-code pr-2.5 pl-2 text-right text-[var(--code-gutter)] select-none">
                {first + i}
              </td>
              <td className="pr-3 whitespace-pre">
                {toks.length
                  ? toks.map(([c, t], j) =>
                      c ? (
                        <span key={j} className={'tk-' + c}>
                          {t}
                        </span>
                      ) : (
                        t
                      ),
                    )
                  : ' '}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Relations({
  title,
  edges,
  other,
  ix,
  onGo,
}: {
  title: string
  edges: FlowEdge[]
  other: (e: FlowEdge) => string
  ix: FlowIndex
  onGo: (id: string) => void
}) {
  return (
    <section className="mt-4">
      <h4 className="mb-1 text-xs font-semibold text-fg-2">
        {title} <span className="font-normal text-fg-3">({edges.length})</span>
      </h4>
      {!edges.length ? (
        <p className="text-xs text-fg-3">None found.</p>
      ) : (
        <ul className="space-y-px">
          {edges.slice(0, MAX_REL).map((e, i) => {
            const t = ix.nodes.get(other(e))
            if (!t) return null
            return (
              <li key={i} className="flex min-w-0 items-baseline gap-2">
                <button
                  type="button"
                  className={cx(
                    'min-w-0 cursor-pointer text-left font-mono text-xs break-all hover:underline',
                    t.kind === 'external' ? 'text-fg-2' : 'text-accent-text',
                  )}
                  title={t.id}
                  onClick={() => onGo(t.id)}
                >
                  {t.name}
                </button>
                <span className="shrink-0 text-[11px] text-fg-3">
                  {CONF_LABEL[e.conf]}
                  {e.kind !== 'call' ? ' · ' + e.kind : ''}
                  {e.line ? ' · line ' + e.line : ''}
                </span>
              </li>
            )
          })}
          {edges.length > MAX_REL && <li className="text-xs text-fg-3">{edges.length - MAX_REL} more</li>}
        </ul>
      )}
    </section>
  )
}

export const Panel = memo(function Panel({
  ix,
  id,
  root,
  links,
  onGo,
  onSetRoot,
}: {
  ix: FlowIndex
  id: string
  root: string
  links: Links
  onGo: (id: string) => void
  onSetRoot: (id: string) => void
}) {
  const n = ix.nodes.get(id)
  if (!n) return null
  const diff = links.diff(n)
  const inspect = links.inspect(n)
  const arch = links.architecture(n)
  const settable = id !== root && (n.kind === 'func' || n.kind === 'closure' || n.kind === 'route')
  const loc = n.file ? n.file + ':' + n.line + (n.end && n.end !== n.line ? '–' + n.end : '') : ''
  return (
    <div className="p-3 text-[13px]">
      <div className="flex flex-wrap items-center gap-1.5 text-[11.5px] text-fg-3">
        <span>{KIND_LABEL[n.kind]}</span>
        {id === root && <span className="badge badge-soft badge-xs badge-primary">root</span>}
        {n.changed && <span className="badge badge-soft badge-xs badge-info">modified</span>}
        {n.recursive && <span className="badge badge-ghost badge-xs">recursive ↻</span>}
        {n.truncated && (
          <span className="badge badge-soft badge-xs badge-warning" title="Callees not explored (depth or size limit)">
            truncated
          </span>
        )}
        {n.partial && (
          <span className="badge badge-soft badge-xs badge-warning" title="Path, method or handler computed at runtime">
            partial
          </span>
        )}
      </div>
      <div className="mt-1 flex items-start gap-1">
        <h3 className="min-w-0 flex-1 font-mono text-[13px] font-semibold break-all">{n.name}</h3>
        <CopyButton text={n.id} title="Copy the full identifier" />
      </div>
      {n.sig && (
        <CodeInline
          code={n.sig}
          block
          className="mt-1.5 block rounded-field border border-line bg-surface-2 px-2 py-1.5 break-all"
        />
      )}
      <dl className="mt-2.5 grid grid-cols-[max-content_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
        <Row k="Package">{n.pkg && <span className="font-mono">{n.pkg}</span>}</Row>
        <Row k="Location">{loc && <span className="font-mono">{loc}</span>}</Row>
        {n.kind === 'route' && (
          <>
            <Row k="Method">{n.method && <span className="font-mono">{n.method}</span>}</Row>
            <Row k="Path">{n.path && <span className="font-mono">{n.path}</span>}</Row>
            <Row k="Router">{n.router}</Row>
          </>
        )}
        {n.kind === 'db' && (
          <>
            <Row k="Library">{n.dbLib}</Row>
            <Row k="Operation">{n.dbOp && <span className="font-mono">{n.dbOp}</span>}</Row>
            <Row k="SQL">{n.sql ? 'literal (below)' : n.sqlDynamic ? 'built at runtime (dynamic)' : ''}</Row>
          </>
        )}
      </dl>
      {!!n.tags?.length && (
        <div className="mt-2 flex flex-wrap gap-1">
          {n.tags.map((t) => (
            <span key={t} className="badge badge-outline badge-sm text-fg-2">
              {t}
            </span>
          ))}
        </div>
      )}
      <div className="mt-3 flex flex-wrap items-center gap-1.5">
        {settable && (
          <button type="button" className="btn btn-xs btn-primary btn-soft" onClick={() => onSetRoot(id)}>
            <Icon name="target" className="size-3.5" />
            Set as root
          </button>
        )}
        {diff ? (
          <a className="btn btn-xs" href={diff}>
            <Icon name="diff" className="size-3.5" />
            Open in diff
          </a>
        ) : (
          n.file &&
          (n.kind === 'func' || n.kind === 'closure') && (
            <span className="text-xs text-fg-3">Not modified in this report</span>
          )
        )}
        {inspect && (
          <a className="btn btn-xs" href={inspect} title="Open this function in the Go Deep Inspector">
            <Icon name="microscope" className="size-3.5" />
            Inspect
          </a>
        )}
        {arch && (
          <a className="btn btn-xs" href={arch} title="Show this function in the Architecture Explorer">
            <Icon name="graph" className="size-3.5" />
            Architecture
          </a>
        )}
      </div>
      {n.explain && <p className="mt-3 text-[12.5px] leading-relaxed text-fg">{n.explain}</p>}
      {n.sql && (
        <section className="mt-3">
          <h4 className="mb-1 text-xs font-semibold text-fg-2">SQL</h4>
          <pre className="scroll-thin max-h-60 overflow-auto rounded-field border border-line bg-code p-2 font-mono text-[11.5px] leading-[18px] whitespace-pre-wrap">
            {n.sql}
          </pre>
        </section>
      )}
      {n.note && n.kind !== 'route' && <p className="mt-2 text-xs text-fg-3">{n.note}</p>}
      <Relations title="Calls" edges={ix.out.get(id) ?? []} other={(e) => e.to} ix={ix} onGo={onGo} />
      <Relations title="Called by" edges={ix.in.get(id) ?? []} other={(e) => e.from} ix={ix} onGo={onGo} />
      {n.code && (
        <section className="mt-4">
          <h4 className="mb-1 flex items-center text-xs font-semibold text-fg-2">
            <span className="flex-1">Code</span>
            <CopyButton text={n.code} label="Copy" title="Copy the code" />
          </h4>
          <Snippet code={n.code} first={n.line || 1} />
        </section>
      )}
    </div>
  )
})
