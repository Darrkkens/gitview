// Security inspector panels besides the finding: the audit summary (nothing
// selected), a route's security posture, and a path picked on the graph
// (Explain this path).

import { useState } from 'react'
import { Icon } from '../../components/Icon'
import { routeHref } from '../../lib/router'
import type { Graph } from '../http/graph'
import { Btn, InspectorHead, KV, Note, Section, Tag } from '../http/ui'
import { AiResult } from './AiResult'
import { ModelPicker } from './FindingInspector'
import { cancel, explainPath, useAiReady, usePending } from './live'
import { SEVS, commitText, dateUTC, pathKey, type Sec } from './model'
import { Cmd, SDot, Sev } from './parts'
import { FindingRow } from './Sidebar'
import type { Finding } from './types'

export function SecuritySummary({ sec, onReport }: { sec: Sec; onReport: () => void }) {
  const A = sec.page.audit,
    F = sec.F
  const commit = commitText(sec.page)
  const d = sec.diff
  const dep = A.dependencies
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <InspectorHead
        badges={
          <>
            <Tag>CodeAudit</Tag>
            <Tag tone={A.status.status === 'complete' ? 'ok' : 'warn'}>analysis {A.status.status}</Tag>
          </>
        }
        title="Security Journey"
        loc={A.repository + ' · commit ' + commit}
        actions={
          <Btn icon="file" primary onClick={onReport} title="The full report, printable">
            Full report
          </Btn>
        }
      />
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto">
        <Section title="Findings" count={F.length}>
          <div className="mb-2.5 grid grid-cols-5 gap-1.5">
            {SEVS.map((s) => (
              <div key={s} className="rounded-box border border-line bg-surface-2 px-2 py-1.5">
                <div className="text-base font-semibold tabular-nums">{F.filter((f) => f.severity === s).length}</div>
                <Sev sev={s} />
              </div>
            ))}
          </div>
          <KV
            rows={[
              ['Vulnerable', F.filter((f) => f.verdict === 'vulnerable').length + ' (evidence sufficient)'],
              ['Review required', String(F.filter((f) => f.verdict === 'review').length)],
              ['Incomplete', String(F.filter((f) => f.verdict === 'incomplete').length)],
              ['Reviewed', String(F.filter((f) => sec.status(f) !== 'open').length)],
            ]}
          />
        </Section>
        <Section title="Scope">
          <KV
            rows={[
              ['Repository', A.repository],
              ['Commit', commit, 'mono'],
              ['Analysed', dateUTC(A.generated) + ' UTC'],
              ['Modules', (A.modules ?? []).join(', '), 'mono'],
              ['Rules', A.rulesVersion + ' · ' + sec.rules.filter((r) => r.enabled).length + ' enabled'],
              ['Analyzer', 'GitView ' + (A.analyzerVersion || '')],
              [
                'Dependencies',
                dep.checked
                  ? 'checked against ' +
                    (dep.modified || '').slice(0, 10) +
                    ' database' +
                    (dep.stale ? ' (possibly out of date)' : '')
                  : 'not checked',
              ],
            ]}
          />
          <div className="mt-2 space-y-1">
            {(A.status.reasons ?? []).map((r, k) => (
              <Note key={k} warn>
                {r}
              </Note>
            ))}
            {dep.note && <Note>{dep.note}</Note>}
          </div>
        </Section>
        {(sec.page.omitted ?? []).length > 0 && (
          <Section title="Code not embedded" count={sec.page.omitted!.length}>
            <div className="space-y-0.5">
              {sec.page.omitted!.slice(0, 20).map((x, k) => (
                <div key={k} className="truncate font-mono text-[11px] text-fg-3" title={x}>
                  {x}
                </div>
              ))}
            </div>
          </Section>
        )}
        {d && (
          <Section title="Compared with the previous audit">
            <KV
              rows={[
                ['Previous commit', (d.baseCommit || '').slice(0, 12), 'mono'],
                ['New', String((d.new ?? []).length)],
                ['Resolved', String((d.resolved ?? []).length)],
                ['Unchanged', String((d.unchanged ?? []).length)],
                ['Changed', String((d.changed ?? []).length)],
                ['Not confirmed gone', String((d.unconfirmed ?? []).length)],
              ]}
            />
            <div className="mt-2 space-y-1">
              {(d.resolved ?? [])
                .concat(d.unconfirmed ?? [])
                .slice(0, 12)
                .map((x, k) => (
                  <Note key={k}>
                    {(x.reason ? 'not confirmed: ' : 'resolved: ') +
                      x.ruleId +
                      ' ' +
                      x.title +
                      ' (' +
                      x.file +
                      ':' +
                      x.line +
                      ')' +
                      (x.reason ? ' — ' + x.reason : '')}
                  </Note>
                ))}
            </div>
          </Section>
        )}
        <Section title="Colours of the graph">
          <div className="grid grid-cols-[max-content_1fr] items-center gap-x-2.5 gap-y-1 text-xs text-fg-2">
            {(
              [
                ['check', 'security check identified (authentication, authorization)'],
                ['review', 'possible risk: review required'],
                ['vulnerable', 'vulnerable by the evidence, or confirmed by a reviewer'],
                ['incomplete', 'analysis incomplete or relation not resolved'],
                ['struct', 'structural element without findings'],
              ] as const
            ).map(([c, t]) => (
              <div key={c} className="contents">
                <SDot cls={c} />
                <span>{t}</span>
              </div>
            ))}
          </div>
          <Note className="mt-2">
            Every coloured element also says its state in text. Severity, confidence, verdict and review status are
            separate: see each finding.
          </Note>
        </Section>
        <Section title="AI">
          <Note>
            {sec.live
              ? 'Live session: findings and paths can be explained by the local model, and decisions recorded.'
              : 'Offline report: it shows the AI analyses requested before the export (' +
                Object.keys(sec.page.ai ?? {}).length +
                '), marked as such. The static findings do not depend on them.'}
          </Note>
          {!sec.live && (
            <>
              <Note className="mt-1.5">
                To ask the model or record decisions in the browser, open the live Security Journey:
              </Note>
              <Cmd text="gitview serve --view security" />
              <Cmd text="gitview audit serve" />
            </>
          )}
        </Section>
      </div>
    </div>
  )
}

export function RoutePosture({
  sec,
  g,
  id,
  current,
  onFinding,
}: {
  sec: Sec
  g: Graph | null
  id: string
  current: string | null
  onFinding: (id: string) => void
}) {
  const post = sec.postureOf.get(id)
  if (!post) return null
  const k = g?.routeById.get(id)
  const rt = k !== undefined ? g!.routes[k] : undefined
  const fs = (post.findings ?? []).map((x) => sec.byId.get(x)).filter((f): f is Finding => !!f)
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <InspectorHead
        badges={<Tag>route</Tag>}
        title={<span className="font-mono">{post.key}</span>}
        loc={rt ? rt.file + ':' + rt.line : ''}
        actions={
          rt ? (
            <Btn
              icon="globe"
              href={routeHref('http', { route: id })}
              title="The code this route may run, and its contract"
            >
              HTTP journey
            </Btn>
          ) : undefined
        }
      />
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto">
        <Section title="Security posture">
          <KV
            rows={[
              ['Authentication', post.auth],
              ['Unresolved calls', String(post.unresolved || 0)],
              ['SQL built at runtime', String(post.dynamicSql || 0) + ' operation(s) reached'],
            ]}
          />
        </Section>
        <Section title="Checks identified" count={(post.checks ?? []).length}>
          {(post.checks ?? []).map((x, k) => (
            <div key={k} className="flex items-start gap-2 px-1.5 py-1">
              <SDot cls="check" className="mt-1" title="check" />
              <span className="flex min-w-0 flex-col">
                <span className="text-xs">{x.label}</span>
                <span className="truncate font-mono text-[11px] text-fg-3">
                  {x.kind + (x.file ? ' · ' + x.file + ':' + x.line : '')}
                </span>
              </span>
            </div>
          ))}
          {!(post.checks ?? []).length && <Note>None identified in the chain.</Note>}
        </Section>
        <Section title="Findings" count={fs.length}>
          {fs.map((f) => (
            <FindingRow key={f.id} sec={sec} f={f} current={current === f.id} onOpen={onFinding} />
          ))}
          {!fs.length && <Note>No finding reaches this route.</Note>}
        </Section>
      </div>
    </div>
  )
}

export interface PickedPath {
  from: string
  to: string
  keys: string[]
  names: string[]
}

let pathModelMemo = ''

export function PathInspector({ sec, path, onClear }: { sec: Sec; path: PickedPath; onClear: () => void }) {
  const key = pathKey(path.from, path.to)
  const res = sec.ai[key]
  const { pending, error } = usePending(key)
  const ready = useAiReady(sec.live)
  const [model, setModel0] = useState(pathModelMemo)
  const setModel = (m: string) => {
    pathModelMemo = m
    setModel0(m)
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <InspectorHead
        badges={<Tag>path</Tag>}
        title="Explain this path"
        loc={path.names.join(' → ')}
        actions={
          <Btn icon="x" onClick={onClear} title="Clear the path">
            Clear
          </Btn>
        }
      />
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto">
        <Section title="Steps" count={path.names.length}>
          {path.names.map((n, k) => (
            <div key={k} className="flex items-center gap-2 px-1.5 py-1">
              <span className="grid size-[18px] shrink-0 place-items-center rounded-full bg-surface-3 text-[10.5px] text-fg-2">
                {k + 1}
              </span>
              <span className="truncate font-mono text-xs">{n}</span>
            </div>
          ))}
        </Section>
        <Section title="Explain with Gemma">
          {res && <AiResult res={res} />}
          {sec.live ? (
            <>
              <div className="mt-2.5 flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  className="btn btn-primary btn-sm"
                  disabled={!!pending || !ready}
                  onClick={() => void explainPath(sec.page, key, path.from, path.to, model)}
                >
                  <Icon name="sparkles" className="size-3.5" />
                  {res ? 'Ask again' : 'Explain this path'}
                </button>
                {pending && (
                  <button type="button" className="btn btn-ghost btn-sm border-line" onClick={() => cancel(key)}>
                    Cancel
                  </button>
                )}
                {!pending && <ModelPicker live={sec.live} model={model} setModel={setModel} />}
              </div>
              {pending && (
                <div className="mt-2 flex items-center gap-2 text-xs text-fg-2">
                  <span className="loading loading-spinner loading-xs text-s-ai" />
                  asking the local model…
                </div>
              )}
              {error && !pending && (
                <Note warn className="mt-2">
                  {error}
                </Note>
              )}
            </>
          ) : (
            <>
              <Note className="mt-1.5">This report is offline. From the terminal:</Note>
              <Cmd text={"gitview audit explain-path --from '" + path.from + "' --to '" + path.to + "'"} />
            </>
          )}
          <Note className="mt-2.5">
            The relations between the steps are those the analysis found; a path is possible, not executed on every
            request.
          </Note>
        </Section>
      </div>
    </div>
  )
}
