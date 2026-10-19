// Building blocks of the Study sections: source badges, layer badges,
// evidence lists, location links, the per-function actions and the code
// snippet. Everything from the repository is rendered as text.

import { Fragment, memo, useMemo, useState, type ReactNode } from 'react'
import { Icon, type IconName } from '../../components/Icon'
import { cx, loc } from '../../lib/format'
import { tokenize, tokenLines } from '../../lib/go'
import { useStudy } from './context'
import { BASIS_HELP, layerColor } from './fmt'
import { track } from './progress'
import type { Basis, Evidence } from './types'

// ---------- badges ----------

const BASIS_COLOR: Record<string, string> = {
  STATIC: '[--badge-color:var(--k-package)]',
  RUNTIME: '[--badge-color:var(--color-success)]',
  COVERAGE: '[--badge-color:var(--k-func)]',
  SQL: '[--badge-color:var(--k-db)]',
  GIT: '[--badge-color:var(--k-type)]',
  HEURISTIC: '[--badge-color:var(--k-unresolved)]',
}

export function BasisBadge({ basis, className }: { basis: Basis | undefined; className?: string }) {
  if (!basis) return null
  return (
    <span
      className={cx(
        'badge badge-outline badge-xs h-4 shrink-0 px-1 font-mono text-[10px] font-semibold tracking-wide',
        BASIS_COLOR[basis] ?? '[--badge-color:var(--fg-3)]',
        className,
      )}
      title={BASIS_HELP[basis] ?? basis}
    >
      {basis}
    </span>
  )
}

export function LayerBadge({ layer, className }: { layer: string | undefined; className?: string }) {
  if (!layer) return null
  return (
    <span className={cx('badge badge-ghost badge-sm h-5 shrink-0 gap-1 px-1.5 text-[11px] font-medium', className)}>
      <span className="size-2 rounded-full" style={{ background: layerColor(layer) }} />
      {layer}
    </span>
  )
}

export function Tag({
  children,
  tone,
  title,
  className,
}: {
  children: ReactNode
  tone?: 'primary' | 'warning' | 'error' | 'success' | 'info'
  title?: string
  className?: string
}) {
  const tones = {
    primary: 'badge-primary',
    warning: 'badge-warning',
    error: 'badge-error',
    success: 'badge-success',
    info: 'badge-info',
  }
  return (
    <span
      className={cx('badge badge-soft badge-sm h-5 shrink-0 px-1.5 text-[11px]', tone && tones[tone], className)}
      title={title}
    >
      {children}
    </span>
  )
}

// ---------- text ----------

export function Note({ children, className }: { children: ReactNode; className?: string }) {
  return <p className={cx('text-xs leading-relaxed text-fg-2', className)}>{children}</p>
}

export function Meta({ children, className, mono }: { children: ReactNode; className?: string; mono?: boolean }) {
  return <span className={cx('text-xs text-fg-3', mono && 'font-mono', className)}>{children}</span>
}

export function SectionTitle({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <h3 className={cx(!/\bmt-/.test(className ?? '') && 'mt-5', 'mb-2 text-sm font-semibold text-fg', className)}>
      {children}
    </h3>
  )
}

export function Card({
  children,
  changed,
  className,
  highlight,
  id,
}: {
  children: ReactNode
  changed?: boolean
  className?: string
  highlight?: boolean
  id?: string
}) {
  return (
    <div
      id={id}
      className={cx(
        'rounded-box border border-line bg-base-100 px-3 py-2.5 [contain-intrinsic-size:auto_160px] [content-visibility:auto]',
        changed && 'border-l-2 border-l-primary',
        highlight && 'ring-2 ring-primary/60',
        className,
      )}
    >
      {children}
    </div>
  )
}

// ---------- links ----------

/** A link that records the visit of an item (the Progress section counts them). */
export function TrackedLink({
  href,
  area,
  id,
  className,
  children,
  title,
}: {
  href: string
  area?: string
  id?: string
  className?: string
  children: ReactNode
  title?: string
}) {
  return (
    <a href={href} className={className} title={title} onClick={() => area && track(area, id)}>
      {children}
    </a>
  )
}

/** LocLink shows file:line, linked when its target is in the report. */
export function LocLink({
  file,
  line,
  href,
  className,
  area,
  id,
}: {
  file?: string
  line?: number
  href?: string | null
  className?: string
  area?: string
  id?: string
}) {
  const text = loc({ file, line })
  if (!text) return null
  if (href)
    return (
      <TrackedLink
        href={href}
        area={area}
        id={id}
        className={cx('link link-hover font-mono text-xs break-words text-accent-text', className)}
      >
        <PathText text={text} />
      </TrackedLink>
    )
  return (
    <span className={cx('font-mono text-xs break-words text-fg-3', className)}>
      <PathText text={text} />
    </span>
  )
}

/** PathText lets a path wrap after its slashes rather than inside a name. */
export function PathText({ text }: { text: string }) {
  const parts = text.split('/')
  return (
    <>
      {parts.map((p, i) => (
        <Fragment key={i}>
          {p}
          {i < parts.length - 1 && (
            <>
              /<wbr />
            </>
          )}
        </Fragment>
      ))}
    </>
  )
}

/** Name of a function or node, linked to Code Flow when it is there. */
export function NodeName({
  id,
  name,
  area,
  trackId,
  className,
}: {
  id?: string
  name: string
  area?: string
  trackId?: string
  className?: string
}) {
  const { links } = useStudy()
  const href = links.flow(id)
  if (!href) return <span className={cx('font-mono break-all', className)}>{name}</span>
  return (
    <TrackedLink
      href={href}
      area={area}
      id={trackId ?? id}
      className={cx('link link-hover font-mono break-all text-accent-text', className)}
      title="Open in Code Flow"
    >
      {name}
    </TrackedLink>
  )
}

export function EvidenceList({ items, className }: { items: Evidence[] | undefined; className?: string }) {
  const { links } = useStudy()
  if (!items?.length) return null
  return (
    <ul className={cx('space-y-1 text-xs', className)}>
      {items.map((ev, i) => {
        const href = links.href(ev.ref)
        const where = loc(ev)
        return (
          <li key={i} className="leading-5 break-words">
            <BasisBadge basis={ev.basis} className="mr-1.5" />
            <span className="mr-1.5 text-fg-2">{ev.text}</span>
            {where ? (
              <LocLink file={ev.file} line={ev.line} href={href} />
            ) : (
              href && (
                <a href={href} className="link link-hover text-accent-text">
                  open
                </a>
              )
            )}
          </li>
        )
      })}
    </ul>
  )
}

export function ActionLink({
  label,
  href,
  area,
  id,
  icon,
}: {
  label: string
  href: string | null
  area?: string
  id?: string
  icon?: IconName
}) {
  if (!href)
    return (
      <span className="btn btn-xs btn-ghost btn-disabled" title="Not in this report" aria-disabled="true">
        {icon && <Icon name={icon} className="size-3.5" />}
        {label}
      </span>
    )
  return (
    <TrackedLink href={href} area={area} id={id} className="btn btn-xs btn-ghost">
      {icon && <Icon name={icon} className="size-3.5" />}
      {label}
    </TrackedLink>
  )
}

/** MarkButton marks an item as studied (kept in the progress). */
export function MarkButton({ area, id, label, done }: { area: string; id: string; label: string; done: string }) {
  const { progress } = useStudy()
  const on = !!progress.visited[area]?.[id]
  return (
    <button
      type="button"
      className={cx('btn btn-xs', on ? 'btn-success btn-soft' : 'btn-ghost')}
      onClick={() => track(area, id)}
      disabled={on}
    >
      <Icon name={on ? 'check' : 'dot'} className="size-3.5" />
      {on ? done : label}
    </button>
  )
}

export function Actions({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cx('mt-2 flex flex-wrap items-center gap-1', className)}>{children}</div>
}

/**
 * FunctionActions: Open Source (the function's code, inline), Code Flow,
 * Data Flow and Branch Explorer. Targets missing from the report are shown
 * disabled.
 */
export function FunctionActions({
  id,
  file,
  line,
  area,
  className,
}: {
  id?: string
  file?: string
  line?: number
  area: string
  className?: string
}) {
  const { links } = useStudy()
  const [open, setOpen] = useState(false)
  const src = useMemo(() => links.source(id), [links, id])
  const diff = links.diff(file, line)
  return (
    <>
      <Actions className={className}>
        {src ? (
          <button
            type="button"
            className={cx('btn btn-xs', open ? 'btn-primary btn-soft' : 'btn-ghost')}
            aria-expanded={open}
            onClick={() => {
              track(area, id)
              setOpen((o) => !o)
            }}
          >
            <Icon name="code" className="size-3.5" />
            {open ? 'Hide Source' : 'Open Source'}
          </button>
        ) : (
          <ActionLink label="Open Source" icon="code" href={diff} area={area} id={id ?? file} />
        )}
        <ActionLink label="Open Code Flow" icon="flow" href={links.flow(id)} area={area} id={id} />
        <ActionLink label="Open Data Flow" icon="microscope" href={links.inspect(id, 'dataflow')} area={area} id={id} />
        <ActionLink
          label="Open Branch Explorer"
          icon="microscope"
          href={links.inspect(id, 'branches')}
          area={area}
          id={id}
        />
      </Actions>
      {open && src && <Snippet code={src.code} line={src.line} mark={line} className="mt-2" />}
    </>
  )
}

// ---------- code ----------

/** Snippet shows a piece of Go source whose first line is `line`. */
export const Snippet = memo(function Snippet({
  code,
  line,
  mark,
  className,
}: {
  code: string
  line: number
  mark?: number
  className?: string
}) {
  const rows = useMemo(() => tokenLines(tokenize(code)), [code])
  return (
    <div
      className={cx(
        'scroll-thin max-h-96 overflow-auto rounded-box border border-line bg-code font-mono text-xs leading-5',
        className,
      )}
    >
      <table className="min-w-full border-collapse">
        <tbody>
          {rows.map((toks, i) => {
            const n = line + i
            return (
              <tr key={i} className={cx(n === mark && 'bg-primary/15')}>
                <td
                  className={cx(
                    'sticky left-0 w-px bg-code pr-3 pl-3 text-right align-top text-[var(--code-gutter)] select-none',
                    n === mark && 'text-primary',
                  )}
                >
                  {n}
                </td>
                <td className="pr-4 align-top whitespace-pre">
                  {toks.length
                    ? toks.map(([c, t], k) =>
                        c ? (
                          <span key={k} className={'tk-' + c}>
                            {t}
                          </span>
                        ) : (
                          t
                        ),
                      )
                    : ' '}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
})

/** CodeBox is a block of literal text (a condition, a query). */
export function CodeBox({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={cx(
        'rounded-field border border-line bg-code px-2.5 py-1.5 font-mono text-xs leading-5 break-words whitespace-pre-wrap text-fg',
        className,
      )}
    >
      {children}
    </div>
  )
}

/** KV is a two-column table of labelled values (empty values are skipped). */
export function KV({
  rows,
  className,
}: {
  rows: [string, ReactNode | ReactNode[] | undefined | null][]
  className?: string
}) {
  const shown = rows.filter(([, v]) => v !== undefined && v !== null && v !== '' && !(Array.isArray(v) && !v.length))
  if (!shown.length) return null
  return (
    <table className={cx('w-full text-xs', className)}>
      <tbody>
        {shown.map(([k, v], i) => (
          <tr key={i} className="align-top">
            <th className="w-36 py-0.5 pr-3 text-left font-medium whitespace-nowrap text-fg-3">{k}</th>
            <td className="py-0.5 font-mono break-words text-fg">
              {Array.isArray(v) ? v.map((x, j) => <div key={j}>{x}</div>) : v}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-box border border-dashed border-line px-4 py-6 text-center text-xs text-fg-3">
      {children}
    </div>
  )
}
