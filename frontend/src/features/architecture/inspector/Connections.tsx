// Connections tab: the element's relations, outgoing (depends on) and
// incoming (used by) apart, filtered by confidence; each row navigates.

import { useMemo, useState } from 'react'
import { Icon } from '../../../components/Icon'
import { cx } from '../../../lib/format'
import type { Explorer } from '../controller'
import type { Model } from '../model'
import { ConfBadge, Note, Section } from '../ui'
import { CONF_HELP, CONF_RANK, CONFS, REL, relLabel } from '../vocab'
import { evidenceText, otherEnd, useConfMemory } from './helpers'
import { RelationRow, Row } from './parts'

export function ItemConnections({ D, ctrl, i }: { D: Model; ctrl: Explorer; i: number }) {
  const t = D.T[i]
  const rel = D.relationsOf(i)
  const container = t.kids.length > 0
  const [confs, setConfs] = useConfMemory()
  const go = (x: number) => void ctrl.selectItem(x, { reveal: true })
  return (
    <>
      <Section>
        <div className="flex flex-wrap gap-1" role="group" aria-label="Confidence">
          {CONFS.map((c) => (
            <button
              key={c}
              type="button"
              aria-pressed={confs[c] !== false}
              title={CONF_HELP[c]}
              className={cx(
                'btn btn-xs rounded-full',
                confs[c] !== false ? 'btn-soft' : 'btn-ghost text-fg-3 line-through',
              )}
              onClick={() => setConfs({ ...confs, [c]: confs[c] === false })}
            >
              {c}
            </button>
          ))}
        </div>
        <Note>
          {container
            ? 'Relations between what this ' +
              D.kindLabel(t) +
              ' contains and the rest, grouped by the package (or file) on the other side.'
            : 'Relations of this element in the dataset, each with the place in the code it was read from.'}
        </Note>
      </Section>
      {(
        [
          ['Outgoing · depends on', rel.out, 1],
          ['Incoming · used by', rel.inn, -1],
        ] as const
      ).map(([title, all, dir]) => {
        const list = all.filter((ei) => confs[D.E[ei].conf] !== false)
        return (
          <Section key={title} title={title} count={list.length}>
            {!list.length ? (
              <Note>{'None' + (rel.out.length + rel.inn.length ? ' with the selected confidences.' : '.')}</Note>
            ) : container ? (
              <GroupedByUnit D={D} list={list} dir={dir} self={i} go={go} />
            ) : (
              <GroupedByType D={D} list={list} dir={dir} go={go} />
            )}
          </Section>
        )
      })}
    </>
  )
}

function GroupedByType({ D, list, dir, go }: { D: Model; list: number[]; dir: number; go: (i: number) => void }) {
  const [all, setAll] = useState<Record<string, boolean>>({})
  const by: Record<string, number[]> = {}
  list.forEach((ei) => (by[D.E[ei].type] = by[D.E[ei].type] || []).push(ei))
  return (
    <>
      {Object.keys(by)
        .sort((a, b) => (REL[a]?.order ?? 99) - (REL[b]?.order ?? 99))
        .map((type) => {
          const r = REL[type] ?? { label: type, inv: type }
          const items = by[type].sort(
            (a, b) =>
              CONF_RANK[D.E[b].conf] - CONF_RANK[D.E[a].conf] ||
              (D.T[otherEnd(D.E[a], dir)].label < D.T[otherEnd(D.E[b], dir)].label ? -1 : 1),
          )
          const max = all[type] ? items.length : 60
          return (
            <div key={type} className="mb-3 last:mb-0">
              <div className="flex items-center gap-1.5 px-1 pb-1 text-[11px] text-fg-3">
                <span className="font-medium text-fg-2">{dir > 0 ? r.label : r.inv}</span>
                <span>{items.length}</span>
              </div>
              {items.slice(0, max).map((ei) => {
                const e = D.E[ei],
                  o = otherEnd(e, dir)
                return (
                  <Row
                    key={ei}
                    D={D}
                    i={o}
                    sub={evidenceText(e)}
                    right={<ConfBadge conf={e.conf} />}
                    onClick={() => go(o)}
                  />
                )
              })}
              {items.length > max && (
                <button
                  type="button"
                  className="btn btn-ghost btn-xs mt-1"
                  onClick={() => setAll({ ...all, [type]: true })}
                >
                  Show all {items.length}
                </button>
              )}
            </div>
          )
        })}
    </>
  )
}

function GroupedByUnit({
  D,
  list,
  dir,
  self,
  go,
}: {
  D: Model
  list: number[]
  dir: number
  self: number
  go: (i: number) => void
}) {
  const [all, setAll] = useState(false)
  const units = useMemo(() => {
    const level = D.T[self].kind === 'file' ? 'file' : 'package'
    const by = new Map<number, number[]>()
    list.forEach((ei) => {
      const o = otherEnd(D.E[ei], dir)
      let u = -1
      for (let p = o; p >= 0; p = D.T[p].parent)
        if (D.T[p].kind === level || D.T[p].kind === 'package') {
          u = p
          break
        }
      if (u < 0) u = o
      if (!by.has(u)) by.set(u, [])
      by.get(u)!.push(ei)
    })
    return Array.from(by.entries()).sort(
      (a, b) => b[1].length - a[1].length || (D.T[a[0]].label < D.T[b[0]].label ? -1 : 1),
    )
  }, [D, list, dir, self])
  const max = all ? units.length : 40
  return (
    <>
      {units.slice(0, max).map(([u, items]) => (
        <UnitGroup key={u} D={D} u={u} items={items} dir={dir} go={go} />
      ))}
      {units.length > max && (
        <button type="button" className="btn btn-ghost btn-xs mt-1" onClick={() => setAll(true)}>
          Show all {units.length}
        </button>
      )}
    </>
  )
}

function UnitGroup({
  D,
  u,
  items,
  dir,
  go,
}: {
  D: Model
  u: number
  items: number[]
  dir: number
  go: (i: number) => void
}) {
  const [open, setOpen] = useState(false)
  const types: Record<string, number> = {}
  let best = 'unresolved'
  items.forEach((ei) => {
    const e = D.E[ei]
    types[e.type] = (types[e.type] || 0) + 1
    if (CONF_RANK[e.conf] > CONF_RANK[best]) best = e.conf
  })
  const sum = Object.keys(types)
    .sort((a, b) => types[b] - types[a])
    .map((k) => types[k] + ' ' + relLabel(k))
    .join(' · ')
  const t = D.T[u]
  return (
    <div className="rounded-field">
      <div className="flex items-center gap-0.5">
        <button
          type="button"
          className="btn btn-ghost btn-xs btn-square shrink-0"
          aria-expanded={open}
          aria-label={(open ? 'Hide' : 'Show') + ' the relations with ' + D.displayName(t)}
          onClick={() => setOpen(!open)}
        >
          <Icon name={open ? 'chevronDown' : 'chevronRight'} className="size-3" />
        </button>
        <div className="min-w-0 flex-1">
          <Row D={D} i={u} sub={sum} right={<ConfBadge conf={best} />} onClick={() => go(u)} />
        </div>
      </div>
      {open && (
        <div className="mb-1 ml-6 border-l border-line pl-1.5">
          {items
            .slice()
            .sort((a, b) => CONF_RANK[D.E[b].conf] - CONF_RANK[D.E[a].conf])
            .slice(0, 120)
            .map((ei) => {
              const e = D.E[ei]
              const target = dir > 0 ? e.t : e.s
              return <RelationRow key={ei} D={D} ei={ei} onClick={() => go(target)} />
            })}
          {items.length > 120 && <Note>{items.length - 120} more (all in the JSON export).</Note>}
        </div>
      )}
    </div>
  )
}
