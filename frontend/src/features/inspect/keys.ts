// Keyboard shortcuts of a tab: skipped while typing and with modifiers.

import { useEffect, useRef } from 'react'
import { isTyping } from '../../lib/browser'

/** typing is isTyping, except that a focused checkbox does not take letters. */
export function typing(e: KeyboardEvent): boolean {
  const t = e.target as HTMLInputElement | null
  if (t?.tagName === 'INPUT' && (t.type === 'checkbox' || t.type === 'radio')) return e.key === ' '
  return isTyping(e)
}

/** useKeys calls handler for unmodified keys; it returns true when it used the key. */
export function useKeys(handler: (key: string, e: KeyboardEvent) => boolean | void) {
  const ref = useRef(handler)
  useEffect(() => {
    ref.current = handler
  })
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented || typing(e)) return
      if (ref.current(e.key, e)) e.preventDefault()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
}
