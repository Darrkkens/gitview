// Layout popover: how the current view is arranged (the algorithm and the
// lines), the automatic layout tools, and the layout document: where it is
// saved, download, import, restore.

import { useEffect, useRef } from 'react'
import { toast } from '../../components/Toast'
import { cx } from '../../lib/format'
import type { Explorer, Snapshot } from './controller'
import { ALGORITHM_INFO, ALGORITHMS, EDGE_INFO, EDGE_STYLES } from './layoutDoc'
import type { StoreSnapshot } from './persist'
import { saveText, useStore, viewName } from './saveState'
import { Glyph, Seg } from './ui'

function Group({ title, children, extra }: { title: string; children: React.ReactNode; extra?: React.ReactNode }) {
  return (
    <section className="border-b border-line px-3 py-2.5 last:border-b-0">
      <h3 className="mb-2 flex items-center text-[11px] font-semibold tracking-wide text-fg-3 uppercase">
        <span className="flex-1">{title}</span>
        {extra && <span className="font-normal tracking-normal normal-case">{extra}</span>}
      </h3>
      {children}
    </section>
  )
}

export function SaveBadge({ st, onRetry }: { st: StoreSnapshot; onRetry?: () => void }) {
  const s = saveText(st)
  return (
    <span
      className={cx(
        'inline-flex items-center gap-1.5 text-[11px] whitespace-nowrap',
        s.tone === 'error' ? 'text-error' : s.tone === 'warn' ? 'text-warning' : 'text-fg-3',
      )}
      title={s.title}
      role="status"
      aria-live="polite"
    >
      <span
        aria-hidden="true"
        className={cx(
          'inline-block size-1.5 rounded-full',
          s.tone === 'ok'
            ? 'bg-success'
            : s.tone === 'busy'
              ? 'bg-primary'
              : s.tone === 'error'
                ? 'bg-error'
                : s.tone === 'warn'
                  ? 'bg-warning'
                  : 'bg-fg-3',
        )}
      />
      {s.text}
      {s.tone === 'error' && onRetry && (
        <button type="button" className="link link-hover text-[11px]" onClick={onRetry}>
          Retry
        </button>
      )}
    </span>
  )
}

export function LayoutPanel({
  ctrl,
  snap,
  onClose,
  anchor,
}: {
  ctrl: Explorer
  snap: Snapshot
  onClose: () => void
  anchor: React.RefObject<HTMLElement | null>
}) {
  const ref = useRef<HTMLDivElement>(null)
  const file = useRef<HTMLInputElement>(null)
  const st = useStore(ctrl.store)
  useEffect(() => {
    const down = (ev: MouseEvent) => {
      const t = ev.target as Node
      if (ref.current?.contains(t) || anchor.current?.contains(t)) return
      onClose()
    }
    document.addEventListener('mousedown', down)
    return () => document.removeEventListener('mousedown', down)
  }, [onClose, anchor])
  const L = snap.layout
  const focus = !!snap.S.focus
  const btn = 'btn btn-xs btn-soft gap-1.5'
  return (
    <div
      ref={ref}
      role="dialog"
      aria-label="Layout"
      className="scroll-thin absolute top-2.5 right-14 z-30 max-h-[calc(100%-20px)] w-[310px] overflow-y-auto rounded-box border border-line-strong bg-base-100 shadow-[var(--shadow)]"
    >
      <Group title="Layout" extra={viewName(L.key)}>
        <div className="flex flex-col gap-0.5" role="radiogroup" aria-label="Layout algorithm">
          {ALGORITHMS.map((a) => (
            <label
              key={a}
              className={cx(
                'flex cursor-pointer items-start gap-2 rounded-field px-1.5 py-1 hover:bg-surface-hover',
                L.algorithm === a && 'bg-accent-weak',
              )}
              title={ALGORITHM_INFO[a].help}
            >
              <input
                type="radio"
                name="arch-layout"
                className="radio radio-xs radio-primary mt-0.5"
                checked={L.algorithm === a}
                onChange={() => ctrl.setAlgorithm(a)}
              />
              <span className="flex min-w-0 flex-col">
                <span className="text-xs text-fg">{ALGORITHM_INFO[a].label}</span>
                <span className="text-[11px] leading-snug text-fg-3">
                  {focus && (a === 'grouped' || a === 'hierarchical' || a === 'right')
                    ? 'Callers on the left, callees on the right'
                    : ALGORITHM_INFO[a].help}
                </span>
              </span>
            </label>
          ))}
        </div>
        <div className="mt-2 flex flex-wrap gap-1">
          <button
            type="button"
            className={btn}
            title="Arrange every unlocked card with the algorithm"
            onClick={() => ctrl.autoLayout()}
          >
            <Glyph name="layout" className="size-3.5" />
            Auto layout
          </button>
          <button
            type="button"
            className={btn}
            title="Forget every position of this view, locked ones too"
            onClick={() => ctrl.resetLayout()}
          >
            <Glyph name="reset" className="size-3.5" />
            Reset this view
          </button>
        </div>
        {(L.positions > 0 || L.locked > 0) && (
          <p className="mt-1.5 text-[11px] text-fg-3">
            {L.positions} card{L.positions === 1 ? '' : 's'} placed by hand
            {L.locked ? ' · ' + L.locked + ' locked' : ''}
          </p>
        )}
      </Group>
      <Group title="Lines">
        <Seg
          options={EDGE_STYLES.map((e) => [e, EDGE_INFO[e].label] as [typeof e, string])}
          value={L.edges}
          onChange={(v) => ctrl.setEdgeStyle(v)}
        />
      </Group>
      <Group title="Saved layout" extra={<SaveBadge st={st} onRetry={() => void ctrl.store.flush()} />}>
        <p className="mb-2 text-[11px] leading-snug text-fg-3">
          {st.live
            ? 'Positions are saved by gitview serve (outside the repository) and embedded by gitview graph export.'
            : 'Positions are kept in this browser; download them to share them or to export them with the page (gitview graph export --layout).'}
        </p>
        <div className="flex flex-wrap gap-1">
          <button
            type="button"
            className={btn}
            title="Save the layout of every view to a JSON file (download)"
            onClick={() => ctrl.exportLayout()}
          >
            <Glyph name="download" className="size-3.5" />
            Save to JSON
          </button>
          <button
            type="button"
            className={btn}
            title="Export only this view's layout (JSON)"
            onClick={() => ctrl.exportLayout(true)}
          >
            Export this view
          </button>
          <button type="button" className={btn} title="Read a layout JSON file" onClick={() => file.current?.click()}>
            <Glyph name="upload" className="size-3.5" />
            Import…
          </button>
          <input
            ref={file}
            type="file"
            accept=".json,application/json"
            className="hidden"
            aria-label="Layout file to import"
            onChange={(e) => {
              const f = e.target.files?.[0]
              e.target.value = ''
              if (!f) return
              if (f.size > 8 << 20) {
                toast('Not imported: a layout file is at most 8 MiB.', { tone: 'error' })
                return
              }
              void f.text().then((t) => ctrl.importLayout(t))
            }}
          />
          {st.embedded && !st.live && (
            <button
              type="button"
              className={btn}
              title="Use the layout this page was exported with"
              onClick={() => ctrl.restoreEmbedded()}
            >
              Restore embedded
            </button>
          )}
          <button
            type="button"
            className={cx(btn, 'text-fg-2')}
            title="Every view back to the automatic layout (undo with Ctrl+Z)"
            onClick={() => ctrl.discardAll()}
          >
            Restore automatic layout
          </button>
        </div>
      </Group>
    </div>
  )
}
