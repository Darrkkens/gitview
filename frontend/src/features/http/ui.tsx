// Small parts of the HTTP and Security inspectors: sections, key/value
// lists, badges for confidences and HTTP methods, tabs.

import type { ReactNode } from 'react'
import { Icon, type IconName } from '../../components/Icon'
import { cx, fmt } from '../../lib/format'
import { CONF_HELP, CONFS } from './graph'
import { methodClass } from './helpers'

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
        <div className="mb-2 flex min-h-5 items-center gap-2">
          <h3 className="text-[11px] font-semibold tracking-wide text-fg-3 uppercase">{title}</h3>
          {count != null && <span className="badge badge-ghost badge-xs font-mono tabular-nums">{fmt(count)}</span>}
          {extra && <div className="ml-auto flex items-center gap-1">{extra}</div>}
        </div>
      )}
      {children}
    </section>
  )
}

/** A KV row: label, value and how the value is set (mono: code-like, break: long identifier). */
type KVEntry = [string, ReactNode] | [string, ReactNode, 'mono' | 'break']
export type KVRow = KVEntry | null | false | undefined | '' | 0

/** KV is a definition list; rows whose value is empty are skipped. */
export function KV({ rows, className }: { rows: KVRow[]; className?: string }) {
  const list = rows.filter((r): r is KVEntry => !!r && r[1] !== '' && r[1] !== null && r[1] !== undefined)
  return (
    <dl className={cx('grid grid-cols-[minmax(5.5rem,max-content)_1fr] gap-x-3 gap-y-1 text-xs', className)}>
      {list.map(([k, v, how], i) => (
        <div key={i} className="contents">
          <dt className="text-fg-3">{k}</dt>
          <dd className={cx('min-w-0 break-words text-fg', how && 'font-mono', how === 'break' && 'break-all')}>{v}</dd>
        </div>
      ))}
    </dl>
  )
}

export function Note({ children, warn, className }: { children: ReactNode; warn?: boolean; className?: string }) {
  return (
    <p className={cx('text-xs leading-relaxed', warn ? 'text-warning' : 'text-fg-2', className)}>
      {warn && <Icon name="warning" className="mr-1 inline size-3.5 align-[-2px]" />}
      {children}
    </p>
  )
}

const CONF_CLASS: Record<string, string> = {
  confirmed: 'text-c-confirmed border-c-confirmed/40 bg-c-confirmed/10',
  likely: 'text-c-likely border-c-likely/40 bg-c-likely/10',
  possible: 'text-c-possible border-c-possible/40 bg-c-possible/10',
  unresolved: 'text-c-unresolved border-c-unresolved/40 bg-c-unresolved/10',
}

export function ConfBadge({ conf, title, label }: { conf: string; title?: string; label?: string }) {
  return (
    <span
      className={cx('badge badge-xs shrink-0 border font-medium', CONF_CLASS[conf] ?? 'badge-ghost')}
      title={title ?? CONF_HELP[conf]}
    >
      {label ?? (conf || '—')}
    </span>
  )
}

/** Plain badge with a tone. */
export function Tag({
  children,
  tone,
  title,
  className,
}: {
  children: ReactNode
  tone?: 'ok' | 'warn' | 'error' | 'info' | 'ai'
  title?: string
  className?: string
}) {
  const t =
    tone === 'ok'
      ? 'badge-success badge-soft'
      : tone === 'warn'
        ? 'badge-warning badge-soft'
        : tone === 'error'
          ? 'badge-error badge-soft'
          : tone === 'info'
            ? 'badge-info badge-soft'
            : tone === 'ai'
              ? 'border-s-ai/40 bg-s-ai/12 text-s-ai'
              : 'badge-ghost'
  return (
    <span className={cx('badge badge-sm shrink-0', t, className)} title={title}>
      {children}
    </span>
  )
}

export function Method({ method, className }: { method: string; className?: string }) {
  return (
    <span
      className={cx(
        'inline-flex h-[18px] min-w-11 shrink-0 items-center justify-center rounded px-1 font-mono text-[10px] leading-none font-bold',
        methodClass(method),
        className,
      )}
    >
      {method}
    </span>
  )
}

/** PathText shows a route path with its variables marked; long paths wrap after a slash. */
export function PathText({ path, className }: { path: string; className?: string }) {
  const parts = path.split(/(:[A-Za-z0-9_]+|\{[^}]+\}|\*[A-Za-z0-9_]*)/)
  return (
    <span className={cx('font-mono', className)} title={path}>
      {parts.map((part, i) => {
        if (!part) return null
        if (/^(:|\{|\*)/.test(part))
          return (
            <span key={i} className="text-accent-text">
              {part}
            </span>
          )
        return part.split(/(?<=\/)/).map((seg, k) => (
          <span key={i + '.' + k}>
            {k > 0 && <wbr />}
            {seg}
          </span>
        ))
      })}
    </span>
  )
}

/** Dot in a token colour (kind, layer, state). */
export function Dot({
  token,
  round,
  className,
  title,
}: {
  token: string
  round?: boolean
  className?: string
  title?: string
}) {
  return (
    <span
      className={cx('inline-block size-2.5 shrink-0', round ? 'rounded-full' : 'rounded-[3px]', className)}
      style={{ background: 'var(--' + token + ')' }}
      title={title}
    />
  )
}

export function Metrics({ items, cols = 3 }: { items: [ReactNode, ReactNode][]; cols?: number }) {
  return (
    <div className={cx('grid gap-1.5', cols === 3 ? 'grid-cols-3' : cols === 5 ? 'grid-cols-5' : 'grid-cols-2')}>
      {items.map(([v, l], i) => (
        <div key={i} className="rounded-box border border-line bg-surface-2 px-2.5 py-1.5">
          <div className="text-base font-semibold tabular-nums">{v}</div>
          <div className="truncate text-[11px] text-fg-3">{l}</div>
        </div>
      ))}
    </div>
  )
}

/** ConfBar: the share of each confidence among relations. */
export function ConfBar({ confs }: { confs: string[] }) {
  const c: Record<string, number> = {}
  confs.forEach((x) => (c[x] = (c[x] || 0) + 1))
  const total = confs.length || 1
  return (
    <div>
      <div
        className="my-1.5 flex h-1.5 overflow-hidden rounded-full bg-surface-3"
        title={CONFS.map((k) => (c[k] || 0) + ' ' + k).join(' · ')}
      >
        {CONFS.map((k) =>
          c[k] ? (
            <span
              key={k}
              className="h-full"
              style={{ width: (100 * c[k]) / total + '%', background: 'var(--c-' + k + ')' }}
            />
          ) : null,
        )}
      </div>
      <div className="flex flex-wrap gap-1">
        {CONFS.filter((k) => c[k]).map((k) => (
          <span key={k} className={cx('badge badge-xs border', CONF_CLASS[k])} title={CONF_HELP[k]}>
            {fmt(c[k])} {k}
          </span>
        ))}
      </div>
    </div>
  )
}

export interface TabDef {
  id: string
  label: string
  count?: number
}

export function Tabs({ tabs, active, onChange }: { tabs: TabDef[]; active: string; onChange: (id: string) => void }) {
  return (
    <div
      role="tablist"
      className="tabs tabs-border tabs-sm shrink-0 flex-nowrap overflow-x-auto border-b border-line px-1 print:hidden"
    >
      {tabs.map((t) => (
        <button
          key={t.id}
          type="button"
          role="tab"
          aria-selected={t.id === active}
          className={cx('tab gap-1 px-2.5 text-xs', t.id === active && 'tab-active')}
          onClick={() => onChange(t.id)}
        >
          {t.label}
          {t.count != null && <span className="text-fg-3 tabular-nums">{fmt(t.count)}</span>}
        </button>
      ))}
    </div>
  )
}

/** InspectorHead: badges, title, location and actions of the selection. */
export function InspectorHead({
  badges,
  title,
  prefix,
  loc,
  actions,
}: {
  badges?: ReactNode
  title: ReactNode
  prefix?: ReactNode
  loc?: string
  actions?: ReactNode
}) {
  return (
    <div className="shrink-0 border-b border-line px-3 pt-3 pb-2">
      {badges && <div className="mb-1.5 flex flex-wrap items-center gap-1">{badges}</div>}
      <div className="flex items-baseline gap-2 text-[15px] leading-snug font-semibold break-words">
        {prefix}
        <span className="min-w-0 [overflow-wrap:anywhere]">{title}</span>
      </div>
      {loc && (
        <div className="mt-1 truncate font-mono text-[11px] text-fg-3" title={loc} dir="rtl">
          <bdi>{loc}</bdi>
        </div>
      )}
      {actions && <div className="mt-2 flex flex-wrap items-center gap-1">{actions}</div>}
    </div>
  )
}

/** A row that opens something: dot/marker, two lines, right part. */
export function Row({
  lead,
  name,
  sub,
  right,
  onClick,
  href,
  title,
  mono,
}: {
  lead?: ReactNode
  name: ReactNode
  sub?: ReactNode
  right?: ReactNode
  onClick?: () => void
  href?: string
  title?: string
  mono?: boolean
}) {
  const body = (
    <>
      {lead}
      <span className="flex min-w-0 flex-1 flex-col">
        <span className={cx('truncate text-xs text-fg', mono && 'font-mono')}>{name}</span>
        {sub && <span className="truncate font-mono text-[11px] text-fg-3">{sub}</span>}
      </span>
      {right}
    </>
  )
  const cls = 'flex w-full items-center gap-2 rounded-field px-1.5 py-1 text-left hover:bg-surface-hover'
  if (href)
    return (
      <a href={href} className={cls} title={title}>
        {body}
      </a>
    )
  if (onClick)
    return (
      <button type="button" className={cls} onClick={onClick} title={title}>
        {body}
      </button>
    )
  return (
    <div className={cx(cls, 'hover:bg-transparent')} title={title}>
      {body}
    </div>
  )
}

/** Small button with an icon. */
export function Btn({
  icon,
  children,
  onClick,
  title,
  primary,
  disabled,
  href,
  className,
}: {
  icon?: IconName
  children?: ReactNode
  onClick?: () => void
  title?: string
  primary?: boolean
  disabled?: boolean
  href?: string
  className?: string
}) {
  const cls = cx('btn btn-xs gap-1', primary ? 'btn-primary' : 'btn-ghost border-line', className)
  const body = (
    <>
      {icon && <Icon name={icon} className="size-3.5" />}
      {children}
    </>
  )
  if (href)
    return (
      <a className={cls} href={href} title={title}>
        {body}
      </a>
    )
  return (
    <button type="button" className={cls} onClick={onClick} title={title} disabled={disabled}>
      {body}
    </button>
  )
}

/** Empty: a quiet message inside a pane. */
export function Empty({ title, children }: { title?: string; children?: ReactNode }) {
  return (
    <div className="px-3 py-6 text-center text-xs text-fg-2">
      {title && <div className="mb-1 text-sm font-semibold text-fg">{title}</div>}
      {children}
    </div>
  )
}

/** Lock: authentication recognised (the shared icon set has none). */
export function Lock({ className, title }: { className?: string; title?: string }) {
  return (
    <svg
      viewBox="0 0 16 16"
      className={'inline-block shrink-0 ' + (className ?? 'size-3.5')}
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden={title ? undefined : true}
      role={title ? 'img' : undefined}
    >
      {title && <title>{title}</title>}
      <path d="M4.5 7V5a3.5 3.5 0 0 1 7 0v2M3.5 7h9v6.5h-9z" />
    </svg>
  )
}
