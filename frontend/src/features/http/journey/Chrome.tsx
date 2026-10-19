// The chrome around a journey canvas, shared by the HTTP and Security views:
// breadcrumbs, the toolbar, the legend, a stepper, the shortcut dialog and
// the keyboard shortcuts of the legacy explorer.

import { useEffect, useRef, type ReactNode } from 'react'
import { Icon } from '../../../components/Icon'
import { cx } from '../../../lib/format'
import { CONF_HELP, CONFS } from '../graph'

const GLYPHS = {
  fit: 'M2 6V2h4M10 2h4v4M14 10v4h-4M6 14H2v-4',
  minimap: 'M2 3h12v10H2zM8 8h4v3H8z',
  fullscreen: 'M2 5V2h3M11 2h3v3M14 11v3h-3M5 14H2v-3',
  keyboard: 'M1.5 4h13v8h-13zM4 6.5h.01M6.5 6.5h.01M9 6.5h.01M11.5 6.5h.01M4.5 9.5h7',
}

export function Glyph({ name, className }: { name: keyof typeof GLYPHS; className?: string }) {
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

export interface Crumb {
  label: string
  title?: string
  color?: string
  onClick?: () => void
}

export function Crumbs({ items }: { items: Crumb[] }) {
  return (
    <nav aria-label="Breadcrumb" className="flex min-w-0 flex-wrap items-center gap-0.5 text-xs">
      {items.map((c, k) => (
        <span key={k} className="flex min-w-0 items-center gap-0.5">
          {k > 0 && (
            <span className="px-0.5 text-fg-3" aria-hidden="true">
              ›
            </span>
          )}
          <button
            type="button"
            title={c.title ?? c.label}
            aria-current={k === items.length - 1 ? 'page' : undefined}
            className={cx(
              'flex max-w-72 min-w-0 items-center gap-1.5 rounded-field px-1.5 py-0.5 hover:bg-surface-hover',
              k === items.length - 1 ? 'font-medium text-fg' : 'text-fg-2',
            )}
            onClick={c.onClick}
          >
            {c.color && (
              <span className="size-2 shrink-0 rounded-[2px]" style={{ background: 'var(--' + c.color + ')' }} />
            )}
            <span className="truncate">{c.label}</span>
          </button>
        </span>
      ))}
    </nav>
  )
}

export function Chip({ children, className, title }: { children: ReactNode; className?: string; title?: string }) {
  return (
    <span
      title={title}
      className={cx(
        'inline-flex min-h-7 items-center gap-2 rounded-field border border-line bg-base-100/95 px-2 py-0.5 text-xs shadow-sm backdrop-blur',
        className,
      )}
    >
      {children}
    </span>
  )
}

export function Stepper({
  label,
  value,
  min,
  max,
  onChange,
  title,
}: {
  label: string
  value: number
  min: number
  max: number
  onChange: (v: number) => void
  title?: string
}) {
  return (
    <span className="inline-flex items-center gap-1" title={title ?? label}>
      <span className="text-fg-2">{label}</span>
      <button
        type="button"
        className="btn btn-ghost btn-xs btn-square"
        aria-label={'Less ' + label}
        disabled={value <= min}
        onClick={() => onChange(value - 1)}
      >
        −
      </button>
      <span className="w-3 text-center font-medium tabular-nums">{value}</span>
      <button
        type="button"
        className="btn btn-ghost btn-xs btn-square"
        aria-label={'More ' + label}
        disabled={value >= max}
        onClick={() => onChange(value + 1)}
      >
        +
      </button>
    </span>
  )
}

function ToolBtn({
  title,
  onClick,
  pressed,
  children,
}: {
  title: string
  onClick: () => void
  pressed?: boolean
  children: ReactNode
}) {
  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      aria-pressed={pressed}
      className={cx('btn btn-ghost btn-xs btn-square', pressed && 'bg-surface-3 text-fg')}
      onClick={onClick}
    >
      {children}
    </button>
  )
}

export function Toolbar({
  onZoomIn,
  onZoomOut,
  onFit,
  minimap,
  onMinimap,
  onFullscreen,
  onHelp,
  showLeft,
  onLeft,
  showRight,
  onRight,
  extra,
}: {
  onZoomIn: () => void
  onZoomOut: () => void
  onFit: () => void
  minimap: boolean
  onMinimap: () => void
  onFullscreen: () => void
  onHelp: () => void
  showLeft: boolean
  onLeft: () => void
  showRight: boolean
  onRight: () => void
  extra?: ReactNode
}) {
  return (
    <div
      role="toolbar"
      aria-label="Canvas"
      className="flex items-center gap-0.5 rounded-field border border-line bg-base-100/95 p-0.5 shadow-sm backdrop-blur print:hidden"
    >
      <ToolBtn title={(showLeft ? 'Hide' : 'Show') + ' the sidebar (b)'} onClick={onLeft} pressed={showLeft}>
        <Icon name="panelLeft" className="size-3.5" />
      </ToolBtn>
      <span className="mx-0.5 h-4 w-px bg-line" />
      <ToolBtn title="Zoom in (+)" onClick={onZoomIn}>
        <Icon name="plus" className="size-3.5" />
      </ToolBtn>
      <ToolBtn title="Zoom out (−)" onClick={onZoomOut}>
        <Icon name="minus" className="size-3.5" />
      </ToolBtn>
      <ToolBtn title="Fit view (f)" onClick={onFit}>
        <Glyph name="fit" className="size-3.5" />
      </ToolBtn>
      <span className="mx-0.5 h-4 w-px bg-line" />
      <ToolBtn title="Minimap (m)" onClick={onMinimap} pressed={minimap}>
        <Glyph name="minimap" className="size-3.5" />
      </ToolBtn>
      <ToolBtn title="Fullscreen (Shift+F)" onClick={onFullscreen}>
        <Glyph name="fullscreen" className="size-3.5" />
      </ToolBtn>
      {extra}
      <ToolBtn title="Keyboard shortcuts (?)" onClick={onHelp}>
        <Glyph name="keyboard" className="size-3.5" />
      </ToolBtn>
      <span className="mx-0.5 h-4 w-px bg-line" />
      <ToolBtn title={(showRight ? 'Hide' : 'Show') + ' the inspector (i)'} onClick={onRight} pressed={showRight}>
        <Icon name="panelRight" className="size-3.5" />
      </ToolBtn>
    </div>
  )
}

/** LineSample: a short line in the style of a confidence. */
function LineSample({ conf, color }: { conf?: string; color?: string }) {
  const dash: Record<string, string> = {
    likely: '6 4',
    possible: '2 4',
    unresolved: '5 4',
    chain: '3 3',
    finding: '1 3',
  }
  return (
    <svg width="22" height="8" aria-hidden="true">
      <line
        x1="1"
        y1="4"
        x2="21"
        y2="4"
        stroke={color ?? (conf === 'unresolved' ? 'var(--c-unresolved)' : 'var(--edge-strong)')}
        strokeWidth="1.6"
        strokeDasharray={conf ? dash[conf] : undefined}
      />
    </svg>
  )
}

export function Legend({
  confs,
  extra,
}: {
  confs: string[]
  extra?: { label: string; color: string; dash?: string }[]
}) {
  const list = CONFS.filter((c) => confs.includes(c))
  if (!list.length && !extra?.length) return null
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-field border border-line bg-base-100/90 px-2 py-1 text-[11px] text-fg-2 shadow-sm backdrop-blur print:hidden">
      {list.map((c) => (
        <span key={c} className="inline-flex items-center gap-1.5" title={'Line style: ' + CONF_HELP[c]}>
          <LineSample conf={c} />
          {c}
        </span>
      ))}
      {extra?.map((x) => (
        <span key={x.label} className="inline-flex items-center gap-1.5">
          <LineSample conf={x.dash} color={x.color} />
          {x.label}
        </span>
      ))}
    </div>
  )
}

/** Overlay: a message in the middle of the canvas. */
export function StageState({ title, children, loading }: { title?: string; children?: ReactNode; loading?: boolean }) {
  return (
    <div className="pointer-events-none absolute inset-0 flex items-center justify-center p-6">
      <div className="pointer-events-auto max-w-md rounded-box border border-line bg-base-100 px-4 py-3 text-center shadow-sm">
        {loading && <span className="loading loading-spinner loading-sm mb-1 text-primary" />}
        {title && <div className="text-sm font-semibold">{title}</div>}
        {children && <div className="mt-1 text-xs text-fg-2">{children}</div>}
      </div>
    </div>
  )
}

const SHORTCUTS: [string[], string][] = [
  [['/'], 'Filter the list'],
  [['1', '2', '3'], 'Architecture · HTTP · Security'],
  [['Shift', 'click'], 'Security: pick the end of a path to explain'],
  [['Enter'], 'HTTP: open or collapse the calls of the selected step'],
  [['Esc'], 'Clear the selection · close'],
  [['f', '+', '−'], 'Fit view · zoom'],
  [['m', 'Shift F'], 'Minimap · fullscreen'],
  [['b', 'i'], 'Sidebar · inspector'],
  [['Alt ←', 'Alt →'], 'Back · forward'],
  [['←', '↑', '→', '↓'], 'Pan the focused canvas'],
  [['Double-click'], 'Open or collapse a step · fit (on the background)'],
]

export function HelpDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const d = ref.current
    if (!d) return
    if (open && !d.open) d.showModal()
    if (!open && d.open) d.close()
  }, [open])
  return (
    <dialog ref={ref} className="modal" onClose={onClose} aria-label="Keyboard shortcuts">
      <div className="modal-box max-w-md p-4">
        <h3 className="mb-3 text-sm font-semibold">Keyboard shortcuts</h3>
        <dl className="grid grid-cols-[max-content_1fr] items-center gap-x-4 gap-y-1.5 text-xs">
          {SHORTCUTS.map(([keys, what]) => (
            <div key={what} className="contents">
              <dt className="flex flex-wrap gap-1">
                {keys.map((k) => (
                  <kbd key={k} className="kbd kbd-xs">
                    {k}
                  </kbd>
                ))}
              </dt>
              <dd className="text-fg-2">{what}</dd>
            </div>
          ))}
        </dl>
        <div className="modal-action mt-4">
          <button type="button" className="btn btn-sm" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
      <form method="dialog" className="modal-backdrop">
        <button type="submit" aria-label="Close">
          close
        </button>
      </form>
    </dialog>
  )
}
