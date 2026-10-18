// Slots let the active view put controls in the shell: its actions in the
// header (search, filters…) and its summary in the status bar. They render
// through portals into elements the shell owns.

import { useEffect, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

function usePortalTarget(id: string): HTMLElement | null {
  const [el, setEl] = useState<HTMLElement | null>(null)
  useEffect(() => {
    // The shell's element exists only once it is committed, after the view renders.
    // oxlint-disable-next-line react/set-state-in-effect
    setEl(document.getElementById(id))
  }, [id])
  return el
}

/** HeaderSlot renders its children on the right side of the header. */
export function HeaderSlot({ children }: { children: ReactNode }) {
  const el = usePortalTarget('gv-header-slot')
  return el ? createPortal(children, el) : null
}

/** StatusSlot renders its children on the left side of the status bar. */
export function StatusSlot({ children }: { children: ReactNode }) {
  const el = usePortalTarget('gv-status-slot')
  return el ? createPortal(children, el) : null
}
