// The inspector of a drawn line: the relations it stands for, with their
// evidence and confidence.

import { useMemo } from 'react'
import { fmt, plural } from '../../../lib/format'
import { realOf } from '../arch'
import type { InspectorProps } from '../Inspector'
import type { SceneEdge } from '../types'
import { ConfBadge, ConfBar, KV, type KVRow, Note, Section } from '../ui'
import { CONF_RANK, relLabel } from '../vocab'
import { Head, KindBadge, RelationRow } from './parts'

export function EdgeInspector({ D, ctrl, e }: InspectorProps & { e: SceneEdge }) {
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
