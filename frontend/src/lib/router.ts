// Hash routing: #/<view>?key=value. The hash keeps the state shareable and
// works from file://, where there is no server to route paths.

import { useSyncExternalStore } from 'react'

export interface Route {
  view: string
  params: URLSearchParams
}

function parse(hash: string): Route {
  const h = hash.replace(/^#\/?/, '')
  const q = h.indexOf('?')
  const view = decodeURIComponent(q >= 0 ? h.slice(0, q) : h)
  const params = new URLSearchParams(q >= 0 ? h.slice(q + 1) : '')
  return { view, params }
}

let current = parse(location.hash)
const listeners = new Set<() => void>()

window.addEventListener('hashchange', () => {
  current = parse(location.hash)
  listeners.forEach((l) => l())
})

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

export function useRoute(): Route {
  return useSyncExternalStore(subscribe, () => current)
}

export function routeHref(view: string, params?: Record<string, string | number | undefined | null>): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params ?? {})) {
    if (v !== undefined && v !== null && v !== '') q.set(k, String(v))
  }
  const s = q.toString()
  return '#/' + encodeURIComponent(view) + (s ? '?' + s : '')
}

/**
 * navigate opens a view. replace keeps the history entry (for state that
 * changes often, such as a selection), so Back returns to the previous view.
 */
export function navigate(
  view: string,
  params?: Record<string, string | number | undefined | null>,
  opts?: { replace?: boolean },
) {
  const href = routeHref(view, params)
  if (href === location.hash) return
  if (opts?.replace) {
    history.replaceState(null, '', location.pathname + location.search + href)
    current = parse(href)
    listeners.forEach((l) => l())
  } else {
    location.hash = href
  }
}

/** setParams updates the current view's parameters in place. */
export function setParams(params: Record<string, string | number | undefined | null>, opts?: { replace?: boolean }) {
  const next: Record<string, string> = {}
  current.params.forEach((v, k) => (next[k] = v))
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '') delete next[k]
    else next[k] = String(v)
  }
  navigate(current.view, next, { replace: opts?.replace ?? true })
}
