// The inspector with nothing selected: the repository in numbers and how
// it was analysed.

import { useMemo } from 'react'
import { plural } from '../../../lib/format'
import type { InspectorProps } from '../Inspector'
import { ConfBar, Metrics, Note, Section } from '../ui'
import { CONF_HELP, CONFS, STRUCTURAL } from '../vocab'
import { statPairs } from './helpers'
import { Head, KindBadge } from './parts'

export function SummaryInspector({ D }: InspectorProps) {
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
          <Note>
            Drag a card (a group by its header) to move it; drag the background to pan; Shift+drag selects several.
            Positions are kept per view; Layout (in the toolbar) arranges, saves and imports them.
          </Note>
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
