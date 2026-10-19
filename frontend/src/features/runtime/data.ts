// Which capture the view shows. Offline pages show the capture (or the
// comparison) they embed. A page served by `gitview serve` starts with the
// latest saved capture and can open another one (?capture=<id>) or compare
// two (?base=<A>&capture=<B>), fetched from the local server.

import { useEffect, useState } from 'react'
import { apiGet } from '../../lib/api'
import { isLive } from '../../lib/envelope'
import type { CaptureEntry, RuntimeSection } from './types'

export type Loaded =
  | { status: 'ready'; data: RuntimeSection; loading: boolean; error?: undefined }
  | { status: 'error'; data: RuntimeSection; loading: false; error: string }

// Responses by URL, for the life of the section object they came with.
let cacheFor: RuntimeSection | null = null
const cache = new Map<string, RuntimeSection>()

function cacheOf(section: RuntimeSection): Map<string, RuntimeSection> {
  if (cacheFor !== section) {
    cache.clear()
    cacheFor = section
  }
  return cache
}

/** url of the request that gives the wanted view, or null for the section itself. */
export function wantedURL(section: RuntimeSection, capture: string | null, base: string | null): string | null {
  if (!isLive()) return null
  const b = capture || section.a?.id || ''
  if (base && b) return '/api/runtime/compare?a=' + encodeURIComponent(base) + '&b=' + encodeURIComponent(b)
  if (capture && capture !== section.a?.id) return '/api/runtime/capture/' + encodeURIComponent(capture)
  return null
}

interface Fetched {
  url: string
  data?: RuntimeSection
  error?: string
}

export function useRuntimeData(section: RuntimeSection, capture: string | null, base: string | null): Loaded {
  const url = wantedURL(section, capture, base)
  const hit = url ? cacheOf(section).get(url) : undefined
  const [got, setGot] = useState<Fetched | null>(null)
  // The last capture loaded stays on screen while the next one loads.
  const [last, setLast] = useState<RuntimeSection | null>(null)

  useEffect(() => {
    if (!url || hit) return
    const ctl = new AbortController()
    apiGet<RuntimeSection>(url, ctl.signal)
      .then((d) => {
        cacheOf(section).set(url, d)
        setGot({ url, data: d })
        setLast(d)
      })
      .catch((e: Error) => {
        if (!ctl.signal.aborted) setGot({ url, error: e.message || String(e) })
      })
    return () => ctl.abort()
  }, [url, hit, section])

  if (!url) return { status: 'ready', data: section, loading: false }
  if (hit) return { status: 'ready', data: hit, loading: false }
  if (got?.url === url && got.data) return { status: 'ready', data: got.data, loading: false }
  if (got?.url === url && got.error) return { status: 'error', data: last ?? section, loading: false, error: got.error }
  return { status: 'ready', data: last ?? section, loading: true }
}

/** captureLabel names a saved capture in the pickers. */
export function captureLabel(c: CaptureEntry): string {
  const when = new Date(c.created)
  const date = isNaN(when.getTime())
    ? c.created
    : when.toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  return (
    (c.label ? c.label + ' · ' : '') +
    date +
    ' · ' +
    (c.requests === 1 ? '1 request' : c.requests + ' requests') +
    (c.status && c.status !== 'complete' ? ' · ' + c.status : '')
  )
}

/** captureOption names a capture in a picker, briefly: its label (or ID) and time. */
export function captureOption(c: CaptureEntry): string {
  const when = new Date(c.created)
  const date = isNaN(when.getTime())
    ? ''
    : ' · ' + when.toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false })
  return (c.label || c.id) + date
}
