// Small building blocks shared by the inspector's tabs.

import { useEffect, useMemo, useRef, type ReactNode } from 'react'
import { Icon } from '../../components/Icon'
import { cx } from '../../lib/format'
import { tokenize, tokenLines, type Token } from '../../lib/go'
import { useInspect } from './ctx'
import { loc } from './util'

/** Basis tells how a result was obtained: static analysis, an observation or an estimate. */
export function Basis({
  kind,
  children,
  title,
}: {
  kind: 'static' | 'observed' | 'estimated'
  children?: ReactNode
  title?: string
}) {
  const tone = kind === 'static' ? 'badge-info' : kind === 'observed' ? 'badge-success' : 'badge-warning'
  return (
    <span className={cx('badge badge-sm badge-outline whitespace-nowrap', tone)} title={title}>
      {children ?? kind}
    </span>
  )
}

/** Bar is the row under a tab's title: what the data is, and its controls. */
export function Bar({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cx('flex flex-wrap items-center gap-x-3 gap-y-1.5 text-xs text-fg-2', className)}>{children}</div>
  )
}

/** Msg is a missing-data message (never replaced by simulated data). */
export function Msg({ children, warn, icon }: { children: ReactNode; warn?: boolean; icon?: 'info' | 'warning' }) {
  if (warn) {
    return (
      <div role="status" className="alert alert-warning alert-soft items-start px-3 py-2 text-sm">
        <Icon name={icon ?? 'warning'} className="mt-0.5 size-4" />
        <div className="min-w-0 break-words">{children}</div>
      </div>
    )
  }
  return (
    <div className="flex items-start gap-2 rounded-box border border-dashed border-line-strong px-4 py-3 text-sm text-fg-2">
      <Icon name={icon ?? 'info'} className="mt-0.5 size-4 text-fg-3" />
      <div className="min-w-0 break-words">{children}</div>
    </div>
  )
}

export function Note({ children, className, mono }: { children: ReactNode; className?: string; mono?: boolean }) {
  return <div className={cx('text-xs leading-5 text-fg-2 break-words', mono && 'font-mono', className)}>{children}</div>
}

export function H4({ children, className, right }: { children: ReactNode; className?: string; right?: ReactNode }) {
  return (
    <div className={cx(!className?.includes('mt-') && 'mt-4', 'mb-1.5 flex items-center gap-2', className)}>
      <h4 className="text-[11px] font-semibold tracking-wide text-fg-3 uppercase">{children}</h4>
      {right && <div className="ml-auto flex items-center gap-2">{right}</div>}
    </div>
  )
}

/** Dl lists label/value rows (Row children). */
export function Dl({ children }: { children: ReactNode }) {
  return <dl className="my-2 grid grid-cols-[max-content_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">{children}</dl>
}

/** Row is one label/value pair of a Dl; it renders nothing without a value. */
export function Row({ label, children }: { label: string; children: ReactNode }) {
  if (children === null || children === undefined || children === '' || children === false) return null
  return (
    <>
      <dt className="text-fg-3">{label}</dt>
      <dd className="break-words text-fg">{children}</dd>
    </>
  )
}

/** Kv is an inline list of figures ("7 blocks · 6 edges…"). */
export function Kv({ items, className }: { items: [ReactNode, string, string?][]; className?: string }) {
  return (
    <div className={cx('flex flex-wrap gap-x-4 gap-y-1 text-xs text-fg-2', className)}>
      {items.map(([v, label, title], i) => (
        <span key={i} title={title}>
          <b className="font-semibold text-fg tabular-nums">{v}</b> {label}
        </span>
      ))}
    </div>
  )
}

/**
 * LocLink shows file:line, linked to the Changes view when that line is part
 * of the report's diff ("open in diff").
 */
export function LocLink({
  file,
  line,
  className,
  orFile,
}: {
  file?: string
  line?: number
  className?: string
  /** Link to the file's diff when the line itself is not part of it. */
  orFile?: boolean
}) {
  const { diffHref } = useInspect()
  const t = loc(file, line)
  if (!t) return null
  const href = diffHref(file, line) ?? (orFile ? diffHref(file) : null)
  if (href) {
    return (
      <a href={href} className={cx('link link-hover font-mono text-accent-text', className)} title="Open in diff">
        {t}
      </a>
    )
  }
  return <span className={cx('font-mono text-fg-3', className)}>{t}</span>
}

/** Seg is a segmented control (one choice among a few). */
export function Seg<T extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: T
  options: [T, string, string?][]
  onChange: (v: T) => void
  label: string
}) {
  return (
    <div role="group" aria-label={label} className="join">
      {options.map(([v, text, title]) => (
        <button
          key={v}
          type="button"
          title={title}
          aria-pressed={value === v}
          className={cx(
            'btn join-item btn-xs font-normal',
            value === v ? 'btn-primary btn-soft' : 'btn-ghost border-line',
          )}
          onClick={() => onChange(v)}
        >
          {text}
        </button>
      ))}
    </div>
  )
}

/** Panel is the detail pane next to a graph or timeline. */
export function Panel({ children, label, className }: { children: ReactNode; label: string; className?: string }) {
  return (
    <aside
      aria-label={label}
      className={cx(
        'scroll-thin min-w-0 overflow-auto rounded-box border border-line bg-base-100 px-3.5 py-3 text-sm',
        className,
      )}
    >
      {children}
    </aside>
  )
}

function Tokens({ tokens }: { tokens: Token[] }) {
  return (
    <>
      {tokens.map(([c, t], i) =>
        c ? (
          <span key={i} className={'tk-' + c}>
            {t}
          </span>
        ) : (
          t
        ),
      )}
    </>
  )
}

/**
 * FnCode shows Go source that starts at line `from` (the function of a
 * bundle, or one line of a data-flow node), with marked lines emphasised
 * and the first one scrolled into view inside the box.
 */
export function FnCode({
  code,
  from,
  mark,
  maxHeight = '22rem',
  className,
  file,
}: {
  code: string
  from: number
  mark?: number[]
  maxHeight?: string
  className?: string
  /** Repository-relative file of the code: its identifiers get hovers. */
  file?: string
}) {
  const rows = useMemo(() => tokenLines(tokenize(code.replace(/\n$/, ''))), [code])
  const marks = useMemo(() => new Set(mark ?? []), [mark])
  const ref = useRef<HTMLDivElement>(null)
  const first = mark && mark.length ? Math.min(...mark) : undefined
  useEffect(() => {
    const box = ref.current
    if (!box || first === undefined) return
    const tr = box.querySelector<HTMLElement>(`tr[data-line="${first}"]`)
    if (!tr) return
    // Scroll the box only, never the page.
    const top = tr.offsetTop - box.clientHeight / 3
    box.scrollTop = Math.max(0, top)
  }, [first, code])
  return (
    <div
      ref={ref}
      className={cx(
        'scroll-thin overflow-auto rounded-box border border-line bg-code font-mono text-xs leading-5',
        className,
      )}
      style={{ maxHeight }}
      data-hover-file={file}
    >
      <table className="min-w-full border-collapse">
        <tbody>
          {rows.map((r, i) => {
            const n = from + i
            const on = marks.has(n)
            return (
              <tr key={i} data-line={n} className={cx(on && 'bg-primary/15')}>
                <td
                  className={cx(
                    'sticky left-0 w-px bg-code pr-3 pl-2.5 text-right align-top text-[var(--code-gutter)] select-none',
                    on && 'text-primary',
                  )}
                >
                  {n}
                </td>
                <td className="pr-4 align-top whitespace-pre" style={{ tabSize: 4 }} data-hl={file ? n : undefined}>
                  {r.length ? <Tokens tokens={r} /> : ' '}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

/** Pre is a block of verbatim text (SQL, commands, compiler output). */
export function Pre({
  children,
  className,
  maxHeight = '22rem',
}: {
  children: ReactNode
  className?: string
  maxHeight?: string
}) {
  return (
    <pre
      className={cx(
        'scroll-thin overflow-auto rounded-box border border-line bg-code px-3 py-2 font-mono text-xs leading-5 whitespace-pre',
        className,
      )}
      style={{ maxHeight, tabSize: 4 }}
    >
      {children}
    </pre>
  )
}

/** Toolbar button used on graph canvases. */
export function ToolBtn({ onClick, title, children }: { onClick: () => void; title: string; children: ReactNode }) {
  return (
    <button
      type="button"
      className="btn btn-xs btn-square border-line bg-base-100/90 font-normal shadow-sm"
      title={title}
      aria-label={title}
      onClick={onClick}
    >
      {children}
    </button>
  )
}

/** Class of the dense data tables. */
export const TABLE = 'table table-xs [&_th]:text-[11px] [&_th]:font-semibold [&_th]:whitespace-nowrap [&_th]:text-fg-3'
