// Requests built locally from the static contract and never sent: the page
// has no HTTP client for the analysed API. defaultRequest mirrors
// httpapi.DefaultRequest (Go), so the first cURL shown is the one
// `gitview graph journey` prints. Values are synthetic; credentials are
// placeholders.

import type { Param, Route } from './types'

export interface KV {
  k: string
  v: string
  on: boolean
  note?: Param
}

export interface Request {
  base: string
  path: Record<string, string>
  query: KV[]
  headers: KV[]
  body: string
  ct: string
}

const hex = (c: string) => '%' + c.charCodeAt(0).toString(16).toUpperCase()

/** pathEscape follows Go's url.PathEscape. */
export function pathEscape(s: string): string {
  return encodeURIComponent(s)
    .replace(/[!'()*]/g, hex)
    .replace(/%(24|26|2B|2C|3A|3B|3D|40)/g, (_m, x: string) => String.fromCharCode(parseInt(x, 16)))
}

/** queryEscape follows Go's url.QueryEscape. */
export function queryEscape(s: string): string {
  return encodeURIComponent(s)
    .replace(/[!'()*]/g, hex)
    .replace(/%20/g, '+')
}

export const shellQuote = (s: string) => "'" + String(s).replace(/'/g, "'\\''") + "'"

export function exampleParam(p: Param): string {
  if (p.default) return p.default
  const sh = p.schema ?? { type: '' }
  if (sh.type === 'integer' || sh.type === 'number') return '1'
  if (sh.type === 'boolean') return 'true'
  if (sh.format === 'uuid') return '00000000-0000-4000-8000-000000000000'
  return 'example'
}

function compactJSON(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s))
  } catch {
    return s
  }
}

export function defaultRequest(r: Route): Request {
  const q: Request = {
    base: r.example ? r.example.baseUrl : 'http://localhost:8080',
    path: {},
    query: [],
    headers: [],
    body: '',
    ct: '',
  }
  for (const p of r.params ?? []) {
    if (p.in === 'path') q.path[p.name] = exampleParam(p)
    else if (p.in === 'query')
      q.query.push({ k: p.name, v: exampleParam(p), on: p.required === 'yes' || !!p.default, note: p })
    else if (p.in === 'header' && !/^(authorization|content-type)$/i.test(p.name))
      q.headers.push({ k: p.name, v: '<' + p.name.toLowerCase() + '>', on: p.required === 'yes', note: p })
  }
  const a = r.auth
  if (a && a.recognized) {
    if (a.scheme === 'bearer') q.headers.push({ k: 'Authorization', v: 'Bearer <token>', on: true })
    else if (a.scheme === 'basic') q.headers.push({ k: 'Authorization', v: 'Basic <credentials>', on: true })
    else if (a.scheme === 'cookie') {
      let name = (a.credential || '').replace(/^cookie /, '')
      if (name.includes(' ')) name = '<cookie>'
      q.headers.push({ k: 'Cookie', v: name + '=<value>', on: true })
    }
  }
  if (r.body && r.example && r.example.body) {
    q.ct = r.body.contentTypes?.[0] || 'application/json'
    q.headers.push({ k: 'Content-Type', v: q.ct, on: true })
    q.body = r.example.body
  }
  return q
}

export function requestURL(r: Route, q: Request): string {
  let path = r.template || r.path
  for (const k of Object.keys(q.path).sort()) path = path.split('{' + k + '}').join(pathEscape(q.path[k]))
  const u = q.base.replace(/\/$/, '') + path
  const qs = q.query.filter((x) => x.on).map((x) => queryEscape(x.k) + '=' + queryEscape(x.v))
  return qs.length ? u + '?' + qs.join('&') : u
}

export function curlOf(r: Route, q: Request): string {
  const m = r.method === 'ANY' || r.method === '?' ? 'GET' : r.method
  let out = 'curl' + (m !== 'GET' ? ' -X ' + m : '') + ' ' + shellQuote(requestURL(r, q))
  for (const x of q.headers) if (x.on) out += ' \\\n  -H ' + shellQuote(x.k + ': ' + x.v)
  if (q.body) out += ' \\\n  --data-raw ' + shellQuote(compactJSON(q.body))
  return out
}

export const fileName = (r: Route, ext: string) =>
  (r.method + '_' + r.path).replace(/[^A-Za-z0-9_.-]+/g, '_').replace(/_+/g, '_') + ext

// The requests being edited in this page, by route ID (kept while the page
// is open, like the legacy page).
const edited = new Map<string, Request>()

export function requestOf(r: Route): Request {
  let q = edited.get(r.id)
  if (!q) {
    q = defaultRequest(r)
    edited.set(r.id, q)
  }
  return q
}

export function saveRequest(r: Route, q: Request) {
  edited.set(r.id, q)
}

export function resetRequest(r: Route): Request {
  edited.delete(r.id)
  return requestOf(r)
}
