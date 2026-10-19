// Metadata tab: what the dataset says about the element, as written.

import { CopyButton } from '../../../components/CopyButton'
import type { Explorer } from '../controller'
import type { Model } from '../model'
import { KV, type KVRow, Note, Section } from '../ui'

export function ItemMetadata({ D, ctrl, i }: { D: Model; ctrl: Explorer; i: number }) {
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
