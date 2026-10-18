// AiResult: an investigation of the local model, shown apart from the static
// findings and marked as an interpretation. Only what was validated against
// the context it received is listed as a reference.

import { KV, Note } from '../http/ui'
import { STATUS_LABEL, dateUTC } from './model'
import { AiTag } from './parts'
import type { Investigation } from './types'

function List({ title, items }: { title: string; items?: string[] | null }) {
  if (!items || !items.length) return null
  return (
    <div className="mt-2.5">
      <div className="mb-1 text-[11px] font-semibold tracking-wide text-fg-3 uppercase">{title}</div>
      <ul className="list-disc space-y-0.5 pl-4 text-xs text-fg-2 marker:text-fg-3">
        {items.map((x, k) => (
          <li key={k} className="[overflow-wrap:anywhere]">
            {x}
          </li>
        ))}
      </ul>
    </div>
  )
}

export function AiResult({ res }: { res: Investigation }) {
  const v = res.validation ?? { valid: false }
  const an = res.answer
  return (
    <div className="rounded-box border border-l-[3px] border-dashed border-s-ai/60 border-l-s-ai [border-left-style:solid] bg-s-ai/5 px-3 py-2.5 break-inside-avoid">
      <div className="mb-1.5 flex flex-wrap items-center gap-2">
        <AiTag>AI analysis</AiTag>
        <span className="text-[11px] text-fg-3">
          {res.model + ' · ' + dateUTC(res.generated, 16) + (res.cached ? ' · cached' : '')}
        </span>
      </div>
      <Note>
        Interpretation by a local model, not a finding. References were checked against the context it received:{' '}
        {(v.acceptedEvidenceIds ?? []).length} accepted, {(v.rejected ?? []).length} rejected.
      </Note>
      {!an ? (
        <Note warn className="mt-1.5">
          The model's answer was not valid JSON; nothing from it is shown.
        </Note>
      ) : (
        <>
          <p className="mt-2 text-xs font-semibold">{an.summary}</p>
          <KV
            className="mt-1.5"
            rows={[
              ['Possible problem', an.possible_problem],
              ['Why', an.why],
              ['Model confidence', an.confidence],
            ]}
          />
          <List title="Data path" items={an.data_path} />
          <List title="Transformations" items={an.transformations} />
          <List title="Validations" items={an.validations} />
          <List title="Security checks" items={an.security_checks} />
          <List title="Review points" items={an.review_points} />
          <List title="Undetermined relations" items={an.undetermined_relations} />
          <List title="Missing information" items={an.missing_information} />
          <List title="Recommendations" items={an.recommendations} />
          <List title="Tests" items={an.suggested_tests} />
          <List
            title="References (validated)"
            items={(an.references ?? []).map(
              (r) =>
                r.evidence_id + ' ' + r.file + ':' + r.start_line + '-' + r.end_line + (r.symbol ? ' ' + r.symbol : ''),
            )}
          />
        </>
      )}
      <List title="Rejected references" items={(v.rejected ?? []).map((r) => r.reference + ' — ' + r.reason)} />
      <List title="Warnings" items={v.warnings} />
      {an?.suggested_status && (
        <Note className="mt-2">
          Suggested review status (not applied): {STATUS_LABEL[an.suggested_status] ?? an.suggested_status}
          {an.status_rationale ? ' — ' + an.status_rationale : ''}
        </Note>
      )}
      {!v.valid && (
        <Note warn className="mt-2">
          The answer cites no valid evidence of its context: treat it with care.
        </Note>
      )}
    </div>
  )
}
