// Formatting of the runtime report, as the legacy report wrote values.

/** dur formats nanoseconds: 1.25 s, 8.27 ms, 120 µs, 800 ns ("–" without a value). */
export function dur(ns: number | null | undefined): string {
  if (ns === null || ns === undefined || !isFinite(ns)) return '–'
  const a = Math.abs(ns)
  if (a >= 1e9) return (ns / 1e9).toFixed(2) + ' s'
  if (a >= 1e6) return (ns / 1e6).toFixed(2) + ' ms'
  if (a >= 1e3) return (ns / 1e3).toFixed(0) + ' µs'
  return Math.round(ns) + ' ns'
}

/** bytes formats a size in binary units: 15.4 MiB. */
export function bytes(n: number | null | undefined): string {
  if (n === null || n === undefined || !isFinite(n)) return '–'
  const a = Math.abs(n)
  if (a >= 1073741824) return (n / 1073741824).toFixed(2) + ' GiB'
  if (a >= 1048576) return (n / 1048576).toFixed(1) + ' MiB'
  if (a >= 1024) return (n / 1024).toFixed(1) + ' KiB'
  return Math.round(n) + ' B'
}

/** metric formats a value in its unit (% | bytes | ns | count | bytes/s). */
export function metric(v: number, unit: string): string {
  if (unit === 'ns') return dur(v)
  if (unit === 'bytes') return bytes(v)
  if (unit === 'bytes/s') return bytes(v) + '/s'
  if (unit === '%') return v.toFixed(1) + ' %'
  if (unit === 'count') return Math.round(v) === v ? String(v) : v.toFixed(2)
  return v + (unit ? ' ' + unit : '')
}

/** change formats a relative change in percent: +3.8 % (n/a without a base). */
export function change(v: number | null | undefined): string {
  if (v === null || v === undefined || !isFinite(v)) return 'n/a'
  return (v > 0 ? '+' : '') + v.toFixed(1) + ' %'
}

/** secs formats an offset in nanoseconds as seconds: 0.318 s. */
export const secs = (ns: number, digits = 3) => (ns / 1e9).toFixed(digits) + ' s'

/** Tone of an HTTP status code. */
export function statusTone(status: number, error?: string): 'error' | 'warning' | 'success' {
  if (status >= 500 || error) return 'error'
  if (status >= 400) return 'warning'
  return 'success'
}

export const BASIS_HELP: Record<string, string> = {
  measured: 'read from the kernel or the Go runtime',
  sampled: 'statistical profile',
  estimated: 'derived with a stated assumption',
  unavailable: 'not technically collectable here',
}

/** niceStep is a round step for about n ticks over span. */
export function niceStep(span: number, n = 8): number {
  if (!(span > 0)) return 1
  const raw = span / n
  const p = Math.pow(10, Math.floor(Math.log10(raw)))
  for (const m of [1, 2, 2.5, 5, 10]) if (m * p >= raw) return m * p
  return 10 * p
}

/** Phase of the capture at t seconds. */
export function phaseAt(
  t: number,
  ph: { activeStart: number; activeEnd: number; paused?: { start: number; end: number }[] },
): string {
  const ns = t * 1e9
  if (ns < ph.activeStart) return 'baseline'
  if (ns <= ph.activeEnd) return (ph.paused ?? []).some((w) => ns >= w.start && ns <= w.end) ? 'paused' : 'active'
  return 'cooldown'
}

/** profileValue formats a value of a profile in its unit. */
export function profileValue(unit: string, v: number): string {
  if (unit === 'nanoseconds') return dur(v)
  if (unit === 'bytes') return bytes(v)
  return String(v)
}
