// Theme: GitView's dark and light daisyUI themes. The choice is a per-viewer
// convenience (localStorage, which may be unavailable), defaulting to the
// system preference.

import { useSyncExternalStore } from 'react'

export type Theme = 'dark' | 'light'

const KEY = 'gitview.theme'
const listeners = new Set<() => void>()

function stored(): Theme | null {
  try {
    const v = localStorage.getItem(KEY)
    return v === 'dark' || v === 'light' ? v : null
  } catch {
    return null
  }
}

function system(): Theme {
  return window.matchMedia?.('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
}

let theme: Theme = stored() ?? system()

function apply() {
  document.documentElement.dataset.theme = theme === 'light' ? 'gitview-light' : 'gitview-dark'
}
apply()

export function setTheme(t: Theme) {
  theme = t
  try {
    localStorage.setItem(KEY, t)
  } catch {
    /* per-viewer convenience only */
  }
  apply()
  listeners.forEach((l) => l())
}

export function useTheme(): [Theme, (t: Theme) => void] {
  const t = useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => theme,
  )
  return [t, setTheme]
}

export const currentTheme = () => theme
