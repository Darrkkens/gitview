// Formatting helpers of the inspector (the same rules as the legacy report).

export function fmtNs(ns: number | null | undefined): string {
  if (ns === null || ns === undefined || !isFinite(ns)) return ''
  const a = Math.abs(ns)
  if (a >= 1e9) return (ns / 1e9).toFixed(2) + 's'
  if (a >= 1e6) return (ns / 1e6).toFixed(2) + 'ms'
  if (a >= 1e3) return (ns / 1e3).toFixed(1) + 'µs'
  return Math.round(ns) + 'ns'
}

/** fmtVal formats a profile value in its unit (nanoseconds, bytes or a count). */
export function fmtVal(v: number, unit: string): string {
  if (unit === 'nanoseconds') return fmtNs(v)
  if (unit === 'bytes') {
    if (v >= 1073741824) return (v / 1073741824).toFixed(2) + 'GB'
    if (v >= 1048576) return (v / 1048576).toFixed(2) + 'MB'
    if (v >= 1024) return (v / 1024).toFixed(1) + 'kB'
    return v + 'B'
  }
  return String(v)
}

export const pct = (x: number | null | undefined) => (x || 0).toFixed(2) + '%'

export const loc = (file?: string, line?: number) => (file ? (line ? file + ':' + line : file) : '')

export function clip(t: string | null | undefined, n: number): string {
  const s = String(t ?? '')
  return s.length > n ? s.slice(0, n - 1) + '…' : s
}

/** num formats a benchmark value compactly. */
export function num(v: number | null | undefined): string {
  if (v === null || v === undefined || !isFinite(v)) return '-'
  return v >= 1e6 || (v < 0.01 && v !== 0) ? v.toExponential(3) : String(Math.round(v * 1000) / 1000)
}

const CONF_RANK: Record<string, number> = { exact: 4, derived: 3, likely: 2, possible: 1, unresolved: 0 }

/** weaker returns the weaker of two confidences. */
export const weaker = (a: string, b: string) => ((CONF_RANK[a] ?? 0) <= (CONF_RANK[b] ?? 0) ? a : b)

let measureCtx: CanvasRenderingContext2D | null = null
let monoFamily = ''

/** monoFont is the page's monospace stack (Tailwind's --font-mono). */
export function monoFont(): string {
  if (!monoFamily) {
    monoFamily = getComputedStyle(document.documentElement).getPropertyValue('--font-mono').trim() || 'monospace'
  }
  return monoFamily
}

/** textWidth measures text in a CSS font (used to size graph nodes). */
export function textWidth(text: string, font: string): number {
  if (!measureCtx) measureCtx = document.createElement('canvas').getContext('2d')
  if (!measureCtx) return text.length * 7
  measureCtx.font = font
  return measureCtx.measureText(text).width
}
