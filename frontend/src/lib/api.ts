// Calls to the local server of `gitview serve`. Every request carries the
// page's token; the server also checks Host and Origin. Offline pages never
// call this (their CSP has no connect-src).

import { envelope } from './envelope'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

function headers(extra?: HeadersInit): Headers {
  const h = new Headers(extra)
  const live = envelope().live
  if (live) h.set('X-GitView-Token', live.token)
  return h
}

async function check(res: Response): Promise<Response> {
  if (res.ok) return res
  let msg = res.statusText
  try {
    const text = await res.text()
    try {
      const j = JSON.parse(text)
      msg = j.error || text
    } catch {
      msg = text || msg
    }
  } catch {
    /* keep statusText */
  }
  throw new ApiError(res.status, msg.trim())
}

export async function apiGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await check(await fetch(path, { headers: headers(), signal }))
  return (await res.json()) as T
}

export async function apiText(path: string, signal?: AbortSignal): Promise<string> {
  const res = await check(await fetch(path, { headers: headers(), signal }))
  return res.text()
}

export async function apiPost<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> {
  const res = await check(
    await fetch(path, {
      method: 'POST',
      headers: headers({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(body ?? {}),
      signal,
    }),
  )
  return (await res.json()) as T
}

/** Server-sent events of the local server (repository changes, analyses). */
export function apiEvents(onEvent: (type: string, data: unknown) => void): () => void {
  const live = envelope().live
  if (!live || typeof EventSource === 'undefined') return () => {}
  const es = new EventSource('/api/events?t=' + encodeURIComponent(live.token))
  const types = ['changed', 'section']
  for (const t of types) {
    es.addEventListener(t, (e) => {
      let data: unknown = null
      try {
        data = JSON.parse((e as MessageEvent).data)
      } catch {
        /* ignore malformed event */
      }
      onEvent(t, data)
    })
  }
  return () => es.close()
}
