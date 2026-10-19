// Finding inspector, in tabs: Overview · Evidence · Source · Flow · Fix · AI ·
// Review. Static findings come from deterministic rules; AI text is shown
// apart and never changes a finding or its status. Asking the model and
// recording decisions need the local server.

import { useState, type ReactNode } from 'react'
import { CopyButton } from '../../components/CopyButton'
import { Icon } from '../../components/Icon'
import { toast } from '../../components/Toast'
import { cx } from '../../lib/format'
import { routeHref } from '../../lib/router'
import type { Graph } from '../http/graph'
import { Excerpts } from '../http/source'
import { archHref } from '../http/helpers'
import type { StepNav } from '../http/StepInspector'
import { Btn, InspectorHead, KV, Note, Section, Tabs, Tag } from '../http/ui'
import { AiResult } from './AiResult'
import { cancel, explainFinding, loadAiStatus, review, useAiReady, useAiStatus, usePending } from './live'
import { CATS, FINDING_TABS, NEEDS_NOTE, STATUSES, STATUS_LABEL, dateUTC, type Sec } from './model'
import { AiTag, Cmd, Role, SDot, Sev, StateBadge, Verdict } from './parts'
import type { Finding } from './types'

export interface SecNav extends StepNav {
  route: (id: string) => void
  finding: (id: string) => void
  tab: (t: string) => void
}

const fileLine = (file?: string, line?: number) => (file || '') + (file && line ? ':' + line : '')

function Bullets({ items }: { items?: string[] | null }) {
  if (!items || !items.length) return <Note>—</Note>
  return (
    <ul className="list-disc space-y-1 pl-4 text-xs leading-relaxed marker:text-fg-3">
      {items.map((x, k) => (
        <li key={k}>{x}</li>
      ))}
    </ul>
  )
}

// ---------- overview ----------

function Overview({ sec, g, f, nav }: { sec: Sec; g: Graph | null; f: Finding; nav: SecNav }) {
  const st = sec.stateOf(f)
  const status = sec.status(f)
  const rule = sec.ruleById.get(f.ruleId)
  const d = sec.diffOf.get(f.id)
  const lim = (f.limits ?? []).concat((f.unresolved ?? []).map((u) => 'unresolved: ' + u))
  const hasNode = !!(f.symbolId && g?.byId.has(f.symbolId))
  return (
    <>
      <Section>
        <Verdict st={st}>
          {f.verdictLabel}
          {st.label !== 'Vulnerable' && st.label !== 'Review required' && st.label !== 'Analysis incomplete'
            ? ' · ' + st.label
            : ''}
        </Verdict>
        <p className="mb-2.5 text-xs leading-relaxed">{f.message}</p>
        <KV
          rows={[
            ['Severity', f.severity],
            ['Confidence', f.confidence + ' (strength of the static evidence)'],
            ['Verdict', f.verdict + ' (from the analysis)'],
            ['Review', STATUS_LABEL[status] + (sec.revalidate[f.id] ? ' — revalidate: ' + sec.revalidate[f.id] : '')],
            ['Analysis', f.analysisStatus],
            ['Rule', f.ruleId + ' · ' + (rule?.basis || '')],
            ['Origin', 'deterministic rule (static analysis)'],
            f.symbol && [
              'Function',
              hasNode ? (
                <a
                  key="v"
                  className="link font-mono link-hover text-accent-text"
                  href={archHref(f.symbolId!)}
                  title="Show it in the Architecture view"
                >
                  {f.symbol.qualified}
                </a>
              ) : (
                <span key="v" className="font-mono">
                  {f.symbol.qualified}
                </span>
              ),
            ],
            d && ['Since the previous audit', d.kind + (d.changes ? ': ' + d.changes.join('; ') : '')],
          ]}
        />
      </Section>
      <Section title="Why it can be a problem">
        <Note>{f.why}</Note>
      </Section>
      {(f.checks ?? []).length > 0 && (
        <Section title="Checks identified" count={f.checks!.length}>
          {f.checks!.map((x, k) => (
            <div key={k} className="flex items-start gap-2 px-1.5 py-1">
              <SDot
                cls={x.kind === 'validation' ? 'review' : 'check'}
                className="mt-1"
                title={x.kind === 'validation' ? 'validation' : 'check'}
              />
              <span className="flex min-w-0 flex-col">
                <span className="text-xs">{x.label}</span>
                <span className="truncate font-mono text-[11px] text-fg-3">
                  {x.kind + (x.file ? ' · ' + fileLine(x.file, x.line) : '')}
                </span>
              </span>
            </div>
          ))}
        </Section>
      )}
      {(f.routes ?? []).length > 0 && (
        <Section title="Routes that may reach it" count={f.routes!.length}>
          {f.routes!.map((x) => (
            <div key={x.id} className="flex items-center gap-1">
              <button
                type="button"
                className="flex min-w-0 flex-1 flex-col rounded-field px-1.5 py-1 text-left hover:bg-surface-hover"
                onClick={() => nav.route(x.id)}
                title="Security posture of this route"
              >
                <span className="truncate font-mono text-xs">{x.key}</span>
                <span className="truncate text-[11px] text-fg-3">
                  {'authentication ' + x.auth + ' · ' + x.path.length + ' steps'}
                </span>
              </button>
              {g?.routeById.has(x.id) && (
                <a
                  className="btn btn-ghost btn-xs btn-square"
                  href={routeHref('http', { route: x.id })}
                  title="HTTP journey of this route"
                >
                  <Icon name="globe" className="size-3.5" />
                </a>
              )}
            </div>
          ))}
        </Section>
      )}
      {lim.length > 0 && (
        <Section title="Limits of the analysis" count={lim.length}>
          <div className="space-y-1">
            {lim.map((x, k) => (
              <Note key={k} warn>
                {x}
              </Note>
            ))}
          </div>
        </Section>
      )}
    </>
  )
}

// ---------- evidence ----------

function EvidenceTab({ f, nav }: { f: Finding; nav: SecNav }) {
  const ev = f.evidence ?? []
  return (
    <Section title="Evidence" count={ev.length}>
      <ul className="divide-y divide-line rounded-box border border-line">
        {ev.map((e) => (
          <li key={e.id}>
            <button
              type="button"
              disabled={!e.file}
              className="w-full px-2.5 py-1.5 text-left enabled:hover:bg-surface-hover"
              title={e.file ? 'Show it in the Source tab' : undefined}
              onClick={() => nav.tab('source')}
            >
              <span className="flex items-center gap-2">
                <span className="font-mono text-[11px] text-fg-3">{e.id}</span>
                <Role role={e.kind} />
                {e.basis === 'heuristic' && <span className="text-[10px] text-fg-3">heuristic</span>}
                <span className="flex-1" />
                <span className="truncate font-mono text-[11px] text-fg-3">
                  {e.file ? fileLine(e.file, e.line) : '—'}
                </span>
              </span>
              <span className="mt-0.5 block text-xs [overflow-wrap:anywhere]">{e.text}</span>
              {e.code && (
                <span className="mt-0.5 block font-mono text-[11px] text-fg-2 [overflow-wrap:anywhere]">{e.code}</span>
              )}
            </button>
          </li>
        ))}
      </ul>
      <Note className="mt-2">
        Static evidence: where the analysis read each fact (go/ssa, go/types, Data Flow, the SQL Explorer, the HTTP
        catalog). Heuristic evidence relies on names or paths.
      </Note>
    </Section>
  )
}

// ---------- source ----------

function SourceTab({ sec, g, f, nav }: { sec: Sec; g: Graph | null; f: Finding; nav: SecNav }) {
  const files = new Map<string, Set<number>>()
  const mark = (file?: string, line?: number) => {
    if (!file || !line) return
    if (!files.has(file)) files.set(file, new Set())
    files.get(file)!.add(line)
  }
  mark(f.file, f.line)
  for (const e of f.evidence ?? []) mark(e.file, e.line)
  for (const s of f.flow ?? []) mark(s.file, s.line)
  const snippets = sec.page.snippets ?? undefined
  const views: ReactNode[] = []
  files.forEach((lines, file) => {
    const ls = [...lines]
    const from = Math.min(...ls) - 40,
      to = Math.max(...ls) + 40
    if (!snippets?.[file]?.some((x) => x.to >= from && x.from <= to)) return
    views.push(
      <Section key={file}>
        <div className="mb-1.5 flex items-center gap-2 text-[11px]">
          <span className="min-w-0 flex-1 truncate font-mono text-fg-2" title={file}>
            {file}
          </span>
          <span className="text-fg-3">marked: lines with evidence</span>
        </div>
        <Excerpts snippets={snippets} file={file} from={from} to={to} marks={lines} maxHeight="24rem" />
      </Section>,
    )
  })
  const route = (f.routes ?? [])[0]
  return (
    <>
      {views.length ? (
        views
      ) : (
        <Section>
          <div className="rounded-box border border-dashed border-line px-3 py-4 text-center text-xs text-fg-2">
            <div className="mb-1 font-semibold text-fg">No code embedded</div>
            {f.file === 'go.mod' || !f.file || f.file.endsWith('/go.mod')
              ? 'This finding is about configuration or dependencies.'
              : 'The file was not available when the report was exported.'}
          </div>
        </Section>
      )}
      {route && (
        <Section title="Callers and callees on the path">
          {route.path.map((st, k) => {
            const i = st.id && g?.byId.has(st.id) ? g.byId.get(st.id)! : -1
            return (
              <button
                key={k}
                type="button"
                disabled={i < 0}
                className="flex w-full items-start gap-2 rounded-field px-1.5 py-1 text-left enabled:hover:bg-surface-hover"
                onClick={() => i >= 0 && !nav.select(i) && (location.hash = archHref(st.id))}
              >
                <Role role={st.role} className="mt-px w-20" />
                <span className="flex min-w-0 flex-col">
                  <span className="truncate font-mono text-xs">{st.name}</span>
                  <span className="truncate font-mono text-[11px] text-fg-3">
                    {fileLine(st.file, st.line)}
                    {st.check ? ' · ✓ ' + st.check : ''}
                  </span>
                </span>
              </button>
            )
          })}
        </Section>
      )}
    </>
  )
}

// ---------- flow ----------

const FLOW_DOT: Record<string, string> = {
  source: 'border-s-vuln',
  sink: 'border-s-vuln',
  sanitizer: 'border-s-check',
  validation: 'border-s-review',
}

function FlowTab({ f }: { f: Finding }) {
  const flow = f.flow ?? []
  return (
    <>
      {flow.length > 0 && (
        <Section title="Data path (Data Flow, static)" count={flow.length}>
          <ol className="ml-1.5">
            {flow.map((st, k) => (
              <li
                key={k}
                title={st.via || undefined}
                className={cx(
                  'relative border-l-2 pb-2.5 pl-4',
                  k === flow.length - 1 ? 'border-transparent' : 'border-line-strong',
                )}
              >
                <span
                  className={cx(
                    'absolute top-1 -left-[7px] size-3 rounded-full border-2 bg-base-100',
                    FLOW_DOT[st.role] ?? 'border-[var(--edge-strong)]',
                  )}
                />
                <div className="flex flex-wrap items-baseline gap-1.5">
                  <Role role={st.role} />
                  <span className="font-mono text-xs [overflow-wrap:anywhere]">{st.label}</span>
                </div>
                <div className="font-mono text-[11px] text-fg-3">
                  {(st.func ? st.func + ' · ' : '') + fileLine(st.file, st.line) + (st.conf ? ' · ' + st.conf : '')}
                </div>
                {st.code && (
                  <div className="mt-0.5 font-mono text-[11px] whitespace-pre-wrap text-fg-2 [overflow-wrap:anywhere]">
                    {st.code}
                  </div>
                )}
              </li>
            ))}
          </ol>
          <Note>
            A possible path found by static analysis, not a recorded execution. Sources are HTTP inputs; a sanitizer is
            listed only when its effect is known (a numeric conversion, filepath.Base).
          </Note>
        </Section>
      )}
      {(f.routes ?? []).slice(0, 4).map((r) => (
        <Section
          key={r.id}
          title={
            <>
              Execution path · <span className="font-mono normal-case">{r.key}</span>
            </>
          }
          count={r.path.length}
        >
          {r.path.map((st, k) => (
            <div key={k} className="flex items-center gap-2 px-1.5 py-1">
              <span className="grid size-[18px] shrink-0 place-items-center rounded-full bg-surface-3 text-[10.5px] text-fg-2">
                {k + 1}
              </span>
              <span className="flex min-w-0 flex-col">
                <span className="truncate font-mono text-xs">{st.name}</span>
                <span className="truncate text-[11px] text-fg-3">
                  {st.role + (st.conf ? ' · ' + st.conf : '') + (st.check ? ' · ✓ ' + st.check : '')}
                </span>
              </span>
            </div>
          ))}
        </Section>
      ))}
      {!flow.length && !(f.routes ?? []).length && (
        <Section>
          <Note>This finding has no data path or route: it is a declaration or a setting.</Note>
        </Section>
      )}
    </>
  )
}

// ---------- fix ----------

function FixTab({ sec, f }: { sec: Sec; f: Finding }) {
  const res = sec.ai[f.id]
  return (
    <>
      <Section title="How to fix (from the rule)">
        <Bullets items={f.remediation} />
      </Section>
      <Section title="How to test the fix">
        <Bullets items={f.tests} />
      </Section>
      {res && res.answer && res.validation?.valid && (
        <Section>
          <div className="rounded-box border border-l-[3px] border-dashed border-s-ai/60 border-l-s-ai [border-left-style:solid] bg-s-ai/5 px-3 py-2.5">
            <AiTag>{'AI · ' + res.model}</AiTag>
            <Note className="mt-1.5 mb-1">Suggestions of the model, from the evidence it was given:</Note>
            <Bullets
              items={(res.answer.recommendations ?? []).concat(
                (res.answer.suggested_tests ?? []).map((x) => 'test: ' + x),
              )}
            />
          </div>
        </Section>
      )}
    </>
  )
}

// ---------- AI ----------

export function ModelPicker({
  live,
  model,
  setModel,
}: {
  live: boolean
  model: string
  setModel: (m: string) => void
}) {
  const { status, error, loading } = useAiStatus(live)
  if (!live) return null
  if (loading) return <span className="loading loading-dots loading-xs text-fg-3" />
  if (error) return <span className="text-[11px] text-error">AI status unavailable: {error}</span>
  if (!status) return <span className="loading loading-dots loading-xs text-fg-3" />
  if (!status.reachable)
    return (
      <span className="text-[11px] text-warning" title={status.endpoint}>
        Ollama not reachable{status.error ? ': ' + status.error : ''}
      </span>
    )
  const models = status.models ?? []
  if (!status.selected && !models.length)
    return <span className="text-[11px] text-warning">No model installed{status.error ? ': ' + status.error : ''}</span>
  return (
    <label className="flex items-center gap-1.5 text-[11px] text-fg-2">
      model
      <select
        className="select select-xs w-auto font-mono"
        value={model || status.selected || ''}
        onChange={(e) => setModel(e.target.value)}
      >
        {status.selected && !models.some((m) => m.name === status.selected) && (
          <option value={status.selected}>{status.selected}</option>
        )}
        {models.map((m) => (
          <option key={m.name} value={m.name}>
            {m.name}
            {m.name === status.selected ? ' (default)' : ''}
          </option>
        ))}
      </select>
    </label>
  )
}

let modelMemo = ''

function AiTab({ sec, f }: { sec: Sec; f: Finding }) {
  const [model, setModel0] = useState(modelMemo)
  const setModel = (m: string) => {
    modelMemo = m
    setModel0(m)
  }
  const { pending, error } = usePending(f.id)
  const ready = useAiReady(sec.live)
  const res = sec.ai[f.id]
  return (
    <Section title="Explain with Gemma">
      {res && <AiResult res={res} />}
      {sec.live ? (
        <>
          <div className="mt-2.5 flex flex-wrap items-center gap-2">
            <button
              type="button"
              className="btn btn-primary btn-sm"
              disabled={!!pending || !ready}
              onClick={() => void explainFinding(sec.page, f.id, model)}
            >
              <Icon name="sparkles" className="size-3.5" />
              {res ? 'Ask again' : 'Explain with Gemma'}
            </button>
            {pending && (
              <button type="button" className="btn btn-ghost btn-sm border-line" onClick={() => cancel(f.id)}>
                Cancel
              </button>
            )}
            {!pending && <ModelPicker live={sec.live} model={model} setModel={setModel} />}
            {!pending && !ready && (
              <button
                type="button"
                className="btn btn-ghost btn-xs btn-square"
                title="Check the model again"
                aria-label="Check the model again"
                onClick={loadAiStatus}
              >
                <Icon name="refresh" className="size-3" />
              </button>
            )}
          </div>
          {pending && (
            <div className="mt-2 flex items-center gap-2 text-xs text-fg-2">
              <span className="loading loading-spinner loading-xs text-s-ai" />
              asking the local model… the evidence and snippets of this finding are sent to it
            </div>
          )}
          {error && !pending && (
            <Note warn className="mt-2">
              {error}
            </Note>
          )}
          <Note className="mt-2.5">
            Only the evidence of this finding and short snippets (secrets redacted) go to the model at the local Ollama;
            the answer is validated before it is shown. The model never changes a finding or its status.
          </Note>
        </>
      ) : (
        <>
          {!res && (
            <Note className="mb-1.5">
              No AI analysis was requested for this finding before the report was exported.
            </Note>
          )}
          <Note className="mt-2">
            This report is an offline file: it cannot reach a model. Ask from the terminal (the answer is embedded in
            the next export), or open the live Security Journey:
          </Note>
          <Cmd text={'gitview audit explain ' + f.id} />
          <Cmd text="gitview serve --view security" />
          <Cmd text="gitview audit serve" />
        </>
      )}
    </Section>
  )
}

// ---------- review ----------

function ReviewTab({ sec, f }: { sec: Sec; f: Finding }) {
  const rec = sec.reviews[f.id]
  const cur = sec.status(f)
  const [to, setTo] = useState(cur)
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const history = (rec?.history ?? []).slice().reverse()
  const save = async () => {
    if (NEEDS_NOTE.has(to) && !note.trim()) {
      setMsg('A justification is required for ' + STATUS_LABEL[to].toLowerCase() + '.')
      return
    }
    setBusy(true)
    setMsg('')
    try {
      await review(sec.page, f.id, to, note)
      toast('Decision recorded: ' + (STATUS_LABEL[to] ?? to), { tone: 'success' })
    } catch (e) {
      setMsg((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <Section title="Review status">
        <KV
          rows={[['Status', STATUS_LABEL[cur] ?? cur], sec.revalidate[f.id] && ['Revalidate', sec.revalidate[f.id]]]}
        />
        {sec.live ? (
          <div className="mt-2.5 grid gap-1.5">
            <select
              className="select select-sm w-full"
              aria-label="New status"
              value={to}
              onChange={(e) => setTo(e.target.value)}
            >
              {STATUSES.map((s) => (
                <option key={s} value={s}>
                  {STATUS_LABEL[s]}
                </option>
              ))}
            </select>
            <textarea
              className="textarea textarea-sm min-h-20 w-full"
              placeholder="Justification (required for confirmed, false positive, accepted risk and fixed)"
              aria-label="Justification"
              value={note}
              onChange={(e) => setNote(e.target.value)}
            />
            <div className="flex items-center gap-2">
              <button type="button" className="btn btn-primary btn-sm" disabled={busy} onClick={() => void save()}>
                {busy && <span className="loading loading-spinner loading-xs" />}
                Record decision
              </button>
              {msg && <span className="text-xs text-error">{msg}</span>}
            </div>
            <Note>Decisions are kept outside the repository with the commit analysed. The AI never records one.</Note>
          </div>
        ) : (
          <>
            <Note className="mt-2">
              This report is read-only. Record a decision from the terminal, or open the live Security Journey:
            </Note>
            <Cmd text={'gitview audit review ' + f.id + ' --status confirmed --note "…"'} />
            <Cmd text="gitview serve --view security" />
          </>
        )}
      </Section>
      <Section title="History" count={rec ? (rec.history ?? []).length : 0}>
        {!history.length ? (
          <Note>No decision recorded.</Note>
        ) : (
          <ul className="divide-y divide-line rounded-box border border-line">
            {history.map((d, k) => (
              <li key={k} className="px-2.5 py-1.5 text-xs">
                <div className="flex flex-wrap items-center gap-1.5">
                  <span className="font-mono text-[11px] text-fg-3">{dateUTC(d.time, 16)}</span>
                  <span>{STATUS_LABEL[d.from] ?? d.from}</span>
                  <span className="text-fg-3">→</span>
                  <span className="font-medium">{STATUS_LABEL[d.to] ?? d.to}</span>
                  <span className="flex-1" />
                  <span className="font-mono text-[11px] text-fg-3" title="Commit analysed">
                    {(d.commit || '').slice(0, 12)}
                  </span>
                </div>
                <div className="mt-0.5 text-fg-2">{d.justification || '—'}</div>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </>
  )
}

// ---------- inspector ----------

export function FindingInspector({
  sec,
  g,
  f,
  tab,
  nav,
  onReport,
}: {
  sec: Sec
  g: Graph | null
  f: Finding
  tab: string
  nav: SecNav
  onReport: () => void
}) {
  const cur = FINDING_TABS.includes(tab) ? tab : 'overview'
  const st = sec.stateOf(f)
  let body: ReactNode
  if (cur === 'evidence') body = <EvidenceTab f={f} nav={nav} />
  else if (cur === 'source') body = <SourceTab sec={sec} g={g} f={f} nav={nav} />
  else if (cur === 'flow') body = <FlowTab f={f} />
  else if (cur === 'fix') body = <FixTab sec={sec} f={f} />
  else if (cur === 'ai') body = <AiTab key={f.id} sec={sec} f={f} />
  else if (cur === 'review') body = <ReviewTab key={f.id + ':' + sec.status(f)} sec={sec} f={f} />
  else body = <Overview sec={sec} g={g} f={f} nav={nav} />
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <InspectorHead
        badges={
          <>
            <Sev sev={f.severity} />
            <Tag className="font-mono">{f.ruleId}</Tag>
            <Tag>{CATS[f.category] ?? f.category}</Tag>
            <StateBadge st={st} />
            {sec.ai[f.id] && <AiTag />}
          </>
        }
        title={f.title}
        loc={fileLine(f.file, f.line)}
        actions={
          <>
            <CopyButton label="ID" text={f.id} className="border-line" />
            {f.file && (
              <CopyButton label="Path" title="Copy file:line" text={fileLine(f.file, f.line)} className="border-line" />
            )}
            <Btn icon="file" title="Open the full report" onClick={onReport}>
              Report
            </Btn>
          </>
        }
      />
      <Tabs
        tabs={[
          { id: 'overview', label: 'Overview' },
          { id: 'evidence', label: 'Evidence', count: (f.evidence ?? []).length },
          { id: 'source', label: 'Source' },
          { id: 'flow', label: 'Flow' },
          { id: 'fix', label: 'Fix' },
          { id: 'ai', label: 'AI' },
          { id: 'review', label: 'Review' },
        ]}
        active={cur}
        onChange={nav.tab}
      />
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto">{body}</div>
    </div>
  )
}
