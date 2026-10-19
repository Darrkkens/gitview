// Filters popover: element kinds, relation kinds and confidence levels
// shown on the canvas, how far the selection's dependency path goes, and
// the theme. Filters only hide what is drawn; the inspector still lists
// every relation.

import { useEffect, useRef, type ReactNode } from 'react'
import { fmt } from '../../lib/format'
import { useTheme } from '../../lib/theme'
import { defaultFilters, type Explorer, type ExplorerState } from './controller'
import type { Model } from './model'
import type { Filters } from './types'
import { LineSample, Seg } from './ui'
import { CONFS, FILTER_KINDS, FILTER_RELS, KIND, REL } from './vocab'

function Check({
  label,
  on,
  set,
  count,
  lead,
}: {
  label: string
  on: boolean
  set: (v: boolean) => void
  count?: number
  lead?: ReactNode
}) {
  return (
    <label className="flex cursor-pointer items-center gap-1.5 text-xs text-fg-2 hover:text-fg">
      <input
        type="checkbox"
        className="checkbox checkbox-xs checkbox-primary"
        checked={on}
        onChange={(e) => set(e.target.checked)}
      />
      {lead}
      <span className="min-w-0 truncate">{label}</span>
      {count !== undefined && <span className="ml-auto text-[10.5px] text-fg-3 tabular-nums">{fmt(count)}</span>}
    </label>
  )
}

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="border-b border-line px-3 py-2.5 last:border-b-0">
      <h3 className="mb-2 text-[11px] font-semibold tracking-wide text-fg-3 uppercase">{title}</h3>
      {children}
    </section>
  )
}

export function FiltersPanel({
  D,
  ctrl,
  filters,
  pathDepth,
  S,
  onClose,
  anchor,
}: {
  D: Model
  ctrl: Explorer
  filters: Filters
  pathDepth: number
  S: ExplorerState
  onClose: () => void
  anchor: React.RefObject<HTMLElement | null>
}) {
  const [theme, setTheme] = useTheme()
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const down = (ev: MouseEvent) => {
      const t = ev.target as Node
      if (ref.current?.contains(t) || anchor.current?.contains(t)) return
      onClose()
    }
    document.addEventListener('mousedown', down)
    return () => document.removeEventListener('mousedown', down)
  }, [onClose, anchor])
  const f = filters
  const update = (patch: Partial<Filters>) => ctrl.setFilters({ ...f, ...patch })
  const present = D.presentKinds,
    presentRel = D.presentRels
  return (
    <div
      ref={ref}
      role="dialog"
      aria-label="Filters"
      className="scroll-thin absolute top-2.5 right-14 z-30 max-h-[calc(100%-20px)] w-[300px] overflow-y-auto rounded-box border border-line-strong bg-base-100 shadow-[var(--shadow)]"
    >
      <Group title="Elements">
        <div className="grid grid-cols-2 gap-x-3 gap-y-1">
          {FILTER_KINDS.filter((k) => present[k]).map((k) => (
            <Check
              key={k}
              label={KIND[k].label}
              on={f.kinds[k] !== false}
              count={present[k]}
              set={(v) => update({ kinds: { ...f.kinds, [k]: v } })}
            />
          ))}
          {(present.external || D.EXT >= 0) && (
            <Check
              label="external dependencies"
              on={f.ext}
              count={present.external || 0}
              set={(v) => update({ ext: v })}
            />
          )}
        </div>
      </Group>
      <Group title="Relations">
        <div className="grid grid-cols-2 gap-x-3 gap-y-1">
          {FILTER_RELS.filter((k) => presentRel[k]).map((k) => (
            <Check
              key={k}
              label={REL[k].label}
              on={f.rels[k] !== false}
              count={presentRel[k]}
              set={(v) => update({ rels: { ...f.rels, [k]: v } })}
            />
          ))}
        </div>
      </Group>
      <Group title="Confidence">
        <div className="flex flex-col gap-1">
          {CONFS.map((k) => (
            <Check
              key={k}
              label={k}
              on={f.confs[k] !== false}
              lead={<LineSample conf={k} />}
              set={(v) => update({ confs: { ...f.confs, [k]: v } })}
            />
          ))}
        </div>
      </Group>
      <Group title="Selection">
        <div className="flex flex-col gap-2">
          <Seg
            options={[
              [0, 'Full dependency path'],
              [1, 'Direct only'],
            ]}
            value={pathDepth ? 1 : 0}
            onChange={(v) => ctrl.setPathDepth(v)}
          />
          <Seg
            options={[
              ['both', 'Both ways'],
              ['out', 'Dependencies'],
              ['in', 'Dependents'],
            ]}
            value={S.direction}
            onChange={(v) => ctrl.setDirection(v)}
          />
          <Check
            label="Only what the selection relates to (O)"
            on={S.isolate}
            set={(v) => ctrl.setIsolate(v)}
          />
          <Check
            label="Hide secondary lines (only weak or type-use relations)"
            on={S.hideMinor}
            set={(v) => ctrl.setHideMinor(v)}
          />
        </div>
      </Group>
      <Group title="Theme">
        <Seg
          options={[
            ['dark', 'Dark'],
            ['light', 'Light'],
          ]}
          value={theme}
          onChange={setTheme}
        />
      </Group>
      <div className="px-3 py-2.5">
        <button type="button" className="btn btn-sm btn-soft" onClick={() => ctrl.setFilters(defaultFilters())}>
          Reset filters
        </button>
      </div>
    </div>
  )
}
