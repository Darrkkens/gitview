// The inspector of several selected cards: a summary, the list, and the
// arrange, lock and fit actions.

import { useMemo } from 'react'
import { fmt, plural } from '../../../lib/format'
import type { InspectorProps } from '../Inspector'
import { Glyph, KV, type KVRow, Note, Section } from '../ui'
import { KIND, kindPlural } from '../vocab'
import { Head, KindBadge, Row } from './parts'

export function MultiInspector({ D, ctrl, snap }: InspectorProps) {
  const items = useMemo(() => Array.from(snap.S.multi), [snap.S.multi])
  const kinds = useMemo(() => {
    const c: Record<string, number> = {}
    items.forEach((i) => (c[D.T[i].kind] = (c[D.T[i].kind] || 0) + 1))
    return Object.keys(c)
      .sort((a, b) => (KIND[a]?.order ?? 50) - (KIND[b]?.order ?? 50))
      .map((k): KVRow => [kindPlural(k, c[k]), fmt(c[k])])
  }, [D, items])
  // relations of the dataset between the selected elements (and what they contain)
  const among = useMemo(() => {
    const set = new Set(items)
    const owner = (x: number) => {
      for (let p = x; p >= 0; p = D.T[p].parent) if (set.has(p)) return p
      return -1
    }
    let n = 0
    for (const i of items.slice(0, 300))
      for (const ei of D.relationsOf(i).out) {
        const o = owner(D.E[ei].t)
        if (o >= 0 && o !== i) n++
      }
    return n
  }, [D, items])
  const boxes = ctrl.selectedBoxes()
  const locked = boxes.filter((b) => b.locked).length
  const btn = 'btn btn-xs gap-1'
  return (
    <>
      <Head
        badges={<KindBadge>selection</KindBadge>}
        title={plural(items.length, 'element') + ' selected'}
        actions={
          <>
            <button type="button" className={btn} title="Align left" onClick={() => ctrl.align('left')}>
              <Glyph name="alignLeft" className="size-3.5" />
              Left
            </button>
            <button type="button" className={btn} title="Align centres" onClick={() => ctrl.align('center')}>
              <Glyph name="alignCenter" className="size-3.5" />
              Center
            </button>
            <button type="button" className={btn} title="Align right" onClick={() => ctrl.align('right')}>
              <Glyph name="alignRight" className="size-3.5" />
              Right
            </button>
            <button
              type="button"
              className={btn}
              title="Distribute horizontally"
              disabled={items.length < 3}
              onClick={() => ctrl.distribute('h')}
            >
              <Glyph name="distH" className="size-3.5" />
              Distribute ↔
            </button>
            <button
              type="button"
              className={btn}
              title="Distribute vertically"
              disabled={items.length < 3}
              onClick={() => ctrl.distribute('v')}
            >
              <Glyph name="distV" className="size-3.5" />
              Distribute ↕
            </button>
            <button type="button" className={btn} title="Lock in place (L)" onClick={() => ctrl.setLocked(true)}>
              <Glyph name="lock" className="size-3.5" />
              Lock
            </button>
            <button type="button" className={btn} title="Unlock" onClick={() => ctrl.setLocked(false)}>
              <Glyph name="unlock" className="size-3.5" />
              Unlock
            </button>
            <button type="button" className={btn} title="Fit the selection (C)" onClick={() => ctrl.fitSelection()}>
              <Glyph name="fitSel" className="size-3.5" />
              Fit
            </button>
            <button type="button" className={btn} title="Clear the selection (Esc)" onClick={() => ctrl.clearSelection()}>
              Clear
            </button>
          </>
        }
      />
      <div className="scroll-thin min-h-0 flex-1 overflow-y-auto">
        <Section title="Summary">
          <KV
            rows={[
              ...kinds,
              ['Relations among them', fmt(among)],
              ['Drawn', fmt(boxes.length) + (locked ? ' · ' + fmt(locked) + ' locked' : '')],
            ]}
          />
          <Note>Drag any of them to move them together; Shift+click adds or removes one.</Note>
        </Section>
        <Section title="Elements" count={items.length}>
          {items.slice(0, 200).map((i) => (
            <Row
              key={i}
              D={D}
              i={i}
              sub={D.kindLabel(D.T[i])}
              onClick={() => void ctrl.selectItem(i, { reveal: true })}
            />
          ))}
          {items.length > 200 && <Note>{items.length - 200} more.</Note>}
        </Section>
      </div>
    </>
  )
}
