// Keyboard shortcuts of the journey views (the legacy explorer's keys),
// fullscreen and the per-viewer toggles kept in localStorage.

import { useEffect, useRef } from 'react'
import { isTyping } from '../../../lib/browser'
import { offered } from '../../../lib/data'
import { navigate } from '../../../lib/router'

export interface KeyHandlers {
  fit: () => void
  zoom: (f: number) => void
  minimap: () => void
  left: () => void
  right: () => void
  help: () => void
  escape: () => void
  search: () => void
  enter?: () => void
  /** an overlay of the view (the report) takes the keys: only Escape reaches escape() */
  blocked?: () => boolean
}

export function toggleFullscreen() {
  try {
    if (document.fullscreenElement) void document.exitFullscreen()
    else void document.documentElement.requestFullscreen?.()
  } catch {
    /* not available here */
  }
}

/** useJourneyKeys binds the explorer's keyboard shortcuts while the view is open. */
export function useJourneyKeys(h: KeyHandlers) {
  const ref = useRef(h)
  useEffect(() => {
    ref.current = h
  })
  useEffect(() => {
    const on = (ev: KeyboardEvent) => {
      const hs = ref.current
      if (document.querySelector('dialog[open]')) return
      if (hs.blocked?.()) {
        if (ev.key === 'Escape') hs.escape()
        return
      }
      const typing = isTyping(ev)
      if (ev.key === 'Escape') {
        if (typing) {
          ;(ev.target as HTMLElement).blur?.()
          return
        }
        hs.escape()
        return
      }
      if (typing) return
      if (ev.altKey && ev.key === 'ArrowLeft') {
        history.back()
        ev.preventDefault()
        return
      }
      if (ev.altKey && ev.key === 'ArrowRight') {
        history.forward()
        ev.preventDefault()
        return
      }
      if (ev.ctrlKey || ev.metaKey || ev.altKey) return
      const k = ev.key
      const views: Record<string, string> = { '1': 'architecture', '2': 'http', '3': 'security' }
      if (k === '/') hs.search()
      else if (views[k]) {
        const target = views[k]
        if (offered(target === 'security' ? 'security' : 'graph')) navigate(target)
      } else if (k === 'f') hs.fit()
      else if (k === 'F') toggleFullscreen()
      else if (k === '+' || k === '=') hs.zoom(1.25)
      else if (k === '-' || k === '_') hs.zoom(0.8)
      else if (k === 'm') hs.minimap()
      else if (k === 'b') hs.left()
      else if (k === 'i') hs.right()
      else if (k === '?') hs.help()
      else if (k === 'Enter' && hs.enter) {
        const t = ev.target as HTMLElement | null
        // a focused button or link handles its own Enter
        if (t && /^(BUTTON|A|SUMMARY)$/.test(t.tagName)) return
        hs.enter()
      } else return
      ev.preventDefault()
    }
    document.addEventListener('keydown', on)
    return () => document.removeEventListener('keydown', on)
  }, [])
}

/** readPref reads a per-viewer boolean preference (localStorage, best effort). */
export function readPref(key: string, def: boolean): boolean {
  try {
    const v = localStorage.getItem('gitview.' + key)
    return v === null ? def : v === 'true'
  } catch {
    return def
  }
}

export function writePref(key: string, v: boolean) {
  try {
    localStorage.setItem('gitview.' + key, String(v))
  } catch {
    /* per-viewer convenience only */
  }
}
