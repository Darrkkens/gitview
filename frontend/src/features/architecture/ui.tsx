// Small presentational parts of the Architecture view: kind dots,
// confidence badges, sections, key-value lists, the confidence bar and the
// view's own icons. Repository strings are only ever rendered as text.

import type { CSSProperties, ReactNode } from 'react'
import { cx, fmt } from '../../lib/format'
import type { GEdge } from './types'
import { CONF_BG, CONF_HELP, CONF_TEXT, CONFS, cssVar } from './vocab'

export function KindDot({ color, round, className }: { color: string; round?: boolean; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cx('inline-block size-2 shrink-0', round ? 'rounded-full' : 'rounded-[2px]', className)}
      style={{ background: cssVar(color) }}
    />
  )
}

export function ConfBadge({ conf, children }: { conf: string; children?: ReactNode }) {
  return (
    <span
      className={cx(
        'badge badge-sm badge-outline shrink-0 gap-1 font-medium whitespace-nowrap',
        CONF_TEXT[conf] ?? 'text-fg-2',
      )}
      title={CONF_HELP[conf]}
    >
      {children ?? conf}
    </span>
  )
}

export function Section({
  title,
  count,
  extra,
  children,
  className,
}: {
  title?: ReactNode
  count?: number | null
  extra?: ReactNode
  children?: ReactNode
  className?: string
}) {
  return (
    <section className={cx('border-b border-line px-3 py-3 last:border-b-0', className)}>
      {title && (
        <h3 className="mb-2 flex items-center gap-2 text-[11px] font-semibold tracking-wide text-fg-3 uppercase">
          <span className="min-w-0 truncate">{title}</span>
          {count !== undefined && count !== null && (
            <span className="font-medium tracking-normal normal-case">{fmt(count)}</span>
          )}
          {extra && <span className="ml-auto flex items-center gap-1 tracking-normal normal-case">{extra}</span>}
        </h3>
      )}
      {children}
    </section>
  )
}

export type KVRow = [string, ReactNode] | null | false | undefined

export function KV({ rows }: { rows: KVRow[] }) {
  const list = rows.filter((r): r is [string, ReactNode] => !!r && r[1] !== '' && r[1] !== null && r[1] !== undefined)
  return (
    <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
      {list.map(([k, v], i) => (
        <div key={i} className="contents">
          <dt className="text-fg-3">{k}</dt>
          <dd className="min-w-0 break-words text-fg">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

export function Note({ children, warn, className }: { children: ReactNode; warn?: boolean; className?: string }) {
  return (
    <p className={cx('my-1.5 text-xs leading-relaxed', warn ? 'text-warning' : 'text-fg-3', className)}>{children}</p>
  )
}

export function Metrics({ items }: { items: [string, number][] }) {
  return (
    <div className="grid grid-cols-3 gap-1.5">
      {items.map(([l, v]) => (
        <div key={l} className="rounded-box border border-line bg-surface-2 px-2.5 py-1.5">
          <div className="text-base font-semibold tabular-nums">{fmt(v)}</div>
          <div className="truncate text-[11px] text-fg-3" title={l}>
            {l}
          </div>
        </div>
      ))}
    </div>
  )
}

/** ConfBar: the distribution of confidences of a list of relations. */
export function ConfBar({ list, E }: { list: number[]; E: GEdge[] }) {
  const c: Record<string, number> = {}
  list.forEach((ei) => (c[E[ei].conf] = (c[E[ei].conf] || 0) + 1))
  const total = list.length || 1
  return (
    <>
      <div
        className="mt-2 mb-1.5 flex h-1.5 overflow-hidden rounded-full bg-surface-3"
        title={CONFS.map((k) => (c[k] || 0) + ' ' + k).join(' · ')}
      >
        {CONFS.map((k) =>
          c[k] ? (
            <span key={k} className={cx('block h-full', CONF_BG[k])} style={{ width: (100 * c[k]) / total + '%' }} />
          ) : null,
        )}
      </div>
      <div className="flex flex-wrap gap-1">
        {CONFS.filter((k) => c[k]).map((k) => (
          <ConfBadge key={k} conf={k}>
            {fmt(c[k])} {k}
          </ConfBadge>
        ))}
      </div>
    </>
  )
}

/** LineSample: the line style of a confidence (legend, filters). */
export function LineSample({ conf, color }: { conf?: string; color?: string }) {
  const style: CSSProperties = {
    width: 18,
    height: 0,
    borderTopWidth: 2,
    borderTopStyle: conf === 'likely' || conf === 'unresolved' ? 'dashed' : conf === 'possible' ? 'dotted' : 'solid',
    borderTopColor: color ? cssVar(color) : conf === 'unresolved' ? cssVar('c-unresolved') : cssVar('edge-strong'),
  }
  return <span aria-hidden="true" className="inline-block shrink-0" style={style} />
}

const GLYPHS = {
  fit: 'M2 6V2h4M10 2h4v4M14 10v4h-4M6 14H2v-4',
  focus: 'M8 5.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5M8 1.5v2.5M8 12v2.5M1.5 8H4M12 8h2.5',
  minimap: 'M2 3h12v10H2zM8 8h4v3H8z',
  fullscreen: 'M2 5V2h3M11 2h3v3M14 11v3h-3M5 14H2v-3',
  expand: 'M3 6l5 5 5-5',
  collapse: 'M3 10l5-5 5 5',
  scope: 'M8 2.5v3M8 10.5v3M2.5 8h3M10.5 8h3M8 6.5a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3',
  http: 'M2 8h12M8 2c2 2 2.8 4 2.8 6S10 12 8 14M8 2C6 4 5.2 6 5.2 8S6 12 8 14M8 2a6 6 0 1 1 0 12A6 6 0 0 1 8 2',
  link: 'M7 9a3 3 0 0 0 4.2 0l2-2a3 3 0 0 0-4.2-4.2l-.6.6M9 7a3 3 0 0 0-4.2 0l-2 2a3 3 0 0 0 4.2 4.2l.6-.6',
  shield: 'M8 1.5 13.5 3.5v4c0 3.5-2.4 6-5.5 7-3.1-1-5.5-3.5-5.5-7v-4z',
  sidebar: 'M2 2.5h12v11H2zM6 2.5v11',
  inspector: 'M2 2.5h12v11H2zM10 2.5v11',
  layout: 'M2 2.5h5v4H2zM9 9.5h5v4H9zM4.5 6.5v5H9M11.5 9.5v-5H7',
  undo: 'M5.5 3 2.5 6l3 3M2.5 6h7a4 4 0 0 1 0 8h-3',
  redo: 'M10.5 3l3 3-3 3M13.5 6h-7a4 4 0 0 0 0 8h3',
  lock: 'M4 7.5h8v6H4zM5.5 7.5V5.5a2.5 2.5 0 0 1 5 0v2',
  unlock: 'M4 7.5h8v6H4zM5.5 7.5V5.5a2.5 2.5 0 0 1 4.8-1',
  alignLeft: 'M2.5 2v12M4.5 4.5h8v2.5h-8zM4.5 9h5v2.5h-5z',
  alignCenter: 'M8 2v12M3.5 4.5h9v2.5h-9zM5.5 9h5v2.5h-5z',
  alignRight: 'M13.5 2v12M3.5 4.5h8v2.5h-8zM6.5 9h5v2.5h-5z',
  distH: 'M2 2.5v11M14 2.5v11M5.5 5h1.5v6H5.5zM9 5h1.5v6H9z',
  distV: 'M2.5 2h11M2.5 14h11M5 5.5h6V7H5zM5 9h6v1.5H5z',
  isolate: 'M8 5.5a2.5 2.5 0 1 1 0 5 2.5 2.5 0 0 1 0-5M2 2h3M11 2h3M2 14h3M11 14h3M2 2v3M14 2v3M2 11v3M14 11v3',
  drill: 'M3 3h10v4H3zM6 10h4v3H6zM8 7v3',
  reset: 'M2.5 8a5.5 5.5 0 1 0 1.6-3.9M2.5 2.5V5H5',
  fitSel: 'M2 6V2h4M10 2h4v4M14 10v4h-4M6 14H2v-4M6 6h4v4H6z',
  upload: 'M8 13.5V5M4.5 8.5 8 5l3.5 3.5M2.5 2.5h11',
  download: 'M8 2v8.5M4.5 7 8 10.5 11.5 7M2.5 13.5h11',
  select: 'M3 2.5l8.5 5-3.8 1 2.3 4-1.5.8-2.3-4L3.5 12z',
}

export type GlyphName = keyof typeof GLYPHS

/** Glyph: an icon of this view, in the shared icon style (16×16 strokes). */
export function Glyph({ name, className }: { name: GlyphName; className?: string }) {
  return (
    <svg
      viewBox="0 0 16 16"
      className={'inline-block shrink-0 ' + (className ?? 'size-4')}
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={GLYPHS[name]} />
    </svg>
  )
}

export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="kbd kbd-xs">{children}</kbd>
}

/** Seg: a segmented control (one of a few options). */
export function Seg<T extends string | number>({
  options,
  value,
  onChange,
  className,
}: {
  options: [T, string][]
  value: T
  onChange: (v: T) => void
  className?: string
}) {
  return (
    <span className={cx('join', className)}>
      {options.map(([v, label]) => (
        <button
          key={String(v)}
          type="button"
          aria-pressed={v === value}
          className={cx(
            'btn btn-xs join-item',
            v === value ? 'btn-primary btn-soft' : 'btn-ghost border-line text-fg-2',
          )}
          onClick={() => onChange(v)}
        >
          {label}
        </button>
      ))}
    </span>
  )
}
