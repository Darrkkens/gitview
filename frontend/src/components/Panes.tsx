// Pane layout of the explorers: an optional left sidebar, the main area and
// an optional right inspector, with draggable widths remembered per viewer.

import { useCallback, useRef, useState, type ReactNode } from 'react'
import { cx } from '../lib/format'

function readWidth(key: string, fallback: number): number {
  try {
    const v = Number(localStorage.getItem('gitview.pane.' + key))
    return v > 0 ? v : fallback
  } catch {
    return fallback
  }
}

function saveWidth(key: string, v: number) {
  try {
    localStorage.setItem('gitview.pane.' + key, String(Math.round(v)))
  } catch {
    /* per-viewer convenience only */
  }
}

function Resizer({ onDrag, side }: { onDrag: (dx: number) => void; side: 'left' | 'right' }) {
  const last = useRef(0)
  const onPointerDown = useCallback(
    (e: React.PointerEvent) => {
      last.current = e.clientX
      const el = e.currentTarget as HTMLElement
      el.setPointerCapture(e.pointerId)
      const move = (ev: PointerEvent) => {
        onDrag(ev.clientX - last.current)
        last.current = ev.clientX
      }
      const up = () => {
        el.removeEventListener('pointermove', move)
        el.removeEventListener('pointerup', up)
      }
      el.addEventListener('pointermove', move)
      el.addEventListener('pointerup', up)
    },
    [onDrag],
  )
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      className={cx(
        'relative z-10 w-1 shrink-0 cursor-col-resize bg-transparent transition-colors hover:bg-primary/40',
        side === 'left' ? '-ml-0.5' : '-mr-0.5',
      )}
      onPointerDown={onPointerDown}
    />
  )
}

export function Panes({
  id,
  left,
  right,
  children,
  leftWidth = 288,
  rightWidth = 400,
  showLeft = true,
  showRight = true,
}: {
  /** Key under which the widths are remembered. */
  id: string
  left?: ReactNode
  right?: ReactNode
  children: ReactNode
  leftWidth?: number
  rightWidth?: number
  showLeft?: boolean
  showRight?: boolean
}) {
  const [lw, setLw] = useState(() => readWidth(id + '.left', leftWidth))
  const [rw, setRw] = useState(() => readWidth(id + '.right', rightWidth))
  const dragLeft = useCallback(
    (dx: number) =>
      setLw((w) => {
        const v = Math.min(640, Math.max(180, w + dx))
        saveWidth(id + '.left', v)
        return v
      }),
    [id],
  )
  const dragRight = useCallback(
    (dx: number) =>
      setRw((w) => {
        const v = Math.min(900, Math.max(260, w - dx))
        saveWidth(id + '.right', v)
        return v
      }),
    [id],
  )
  return (
    <div className="flex h-full min-h-0 w-full">
      {left && showLeft && (
        <>
          <aside className="flex min-h-0 shrink-0 flex-col border-r border-line bg-base-100" style={{ width: lw }}>
            {left}
          </aside>
          <Resizer side="left" onDrag={dragLeft} />
        </>
      )}
      <main className="relative flex min-h-0 min-w-0 flex-1 flex-col">{children}</main>
      {right && showRight && (
        <>
          <Resizer side="right" onDrag={dragRight} />
          <aside className="flex min-h-0 shrink-0 flex-col border-l border-line bg-base-100" style={{ width: rw }}>
            {right}
          </aside>
        </>
      )}
    </div>
  )
}

/** PaneHeader is the title row of a sidebar or inspector. */
export function PaneHeader({ title, children }: { title: ReactNode; children?: ReactNode }) {
  return (
    <div className="flex h-9 shrink-0 items-center gap-2 border-b border-line px-3">
      <h2 className="min-w-0 flex-1 truncate text-xs font-semibold tracking-wide text-fg-2 uppercase">{title}</h2>
      {children}
    </div>
  )
}
