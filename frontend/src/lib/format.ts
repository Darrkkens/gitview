const numberFormat = new Intl.NumberFormat('en-US')

export const fmt = (n: number | null | undefined) => numberFormat.format(Number(n || 0))

export const plural = (n: number, one: string, many?: string) => fmt(n) + ' ' + (n === 1 ? one : many || one + 's')

export const baseName = (p: string) => {
  const i = p.lastIndexOf('/')
  return i >= 0 ? p.slice(i + 1) : p
}

export const dirName = (p: string) => {
  const i = p.lastIndexOf('/')
  return i >= 0 ? p.slice(0, i) : ''
}

/** loc formats "file:line" (empty without a file). */
export const loc = (n: { file?: string; line?: number } | null | undefined) =>
  n && n.file ? n.file + (n.line ? ':' + n.line : '') : ''

export function bytes(n: number): string {
  if (n < 1024) return n + ' B'
  const units = ['KiB', 'MiB', 'GiB']
  let v = n / 1024
  let u = 0
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024
    u++
  }
  return (v >= 10 ? v.toFixed(0) : v.toFixed(1)) + ' ' + units[u]
}

/** duration formats milliseconds compactly: 850ms, 1.2s, 3m 4s. */
export function duration(ms: number): string {
  if (!isFinite(ms)) return '—'
  if (ms < 1) return (ms * 1000).toFixed(0) + 'µs'
  if (ms < 1000) return ms.toFixed(ms < 10 ? 1 : 0) + 'ms'
  const s = ms / 1000
  if (s < 60) return s.toFixed(s < 10 ? 2 : 1) + 's'
  const m = Math.floor(s / 60)
  return m + 'm ' + Math.round(s - m * 60) + 's'
}

/** nanos formats a duration given in nanoseconds (Go's time.Duration). */
export const nanos = (ns: number) => duration(ns / 1e6)

export function dateTime(iso: string | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  return d.toLocaleString('en-US', { dateStyle: 'medium', timeStyle: 'short' })
}

export function pct(v: number, digits = 1): string {
  return (isFinite(v) ? v.toFixed(digits) : '0') + '%'
}

/** cx joins class names, skipping falsy values. */
export const cx = (...parts: (string | false | null | undefined)[]) => parts.filter(Boolean).join(' ')
