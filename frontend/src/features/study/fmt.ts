// Formatting helpers of the Study sections (values as the legacy report
// wrote them) and the colours of the layers.

export const BASIS_HELP: Record<string, string> = {
  STATIC: 'Static analysis (SSA call graph, CFG, types): a possible path, not an observed one',
  RUNTIME: 'Observed when the program ran (trace, profile, test run)',
  COVERAGE: 'Observed by a coverage profile',
  SQL: 'Read from SQL text',
  GIT: 'From the working-tree diff',
  HEURISTIC: 'Inferred from names, paths or extensions',
}

/** CSS colour of a layer (badges and the layer graph). */
export function layerColor(layer: string): string {
  switch (layer) {
    case 'HTTP Route':
      return 'var(--l-route)'
    case 'Middleware':
      return 'var(--l-middleware)'
    case 'Handler':
      return 'var(--l-handler)'
    case 'Service':
      return 'var(--l-service)'
    case 'Repository':
      return 'var(--l-repository)'
    case 'Database':
      return 'var(--l-database)'
    case 'External API':
      return 'var(--l-external)'
    case 'Model':
      return 'var(--k-type)'
    case 'Entrypoint':
      return 'var(--k-module)'
    case 'Worker':
      return 'var(--k-interface)'
    case 'Tests':
      return 'var(--color-success)'
    default:
      return 'var(--fg-3)'
  }
}

/** fmtNs formats nanoseconds like the legacy report. */
export function fmtNs(ns: number | null | undefined): string {
  if (ns === null || ns === undefined) return ''
  const a = Math.abs(ns)
  if (a >= 1e9) return (ns / 1e9).toFixed(2) + 's'
  if (a >= 1e6) return (ns / 1e6).toFixed(2) + 'ms'
  if (a >= 1e3) return (ns / 1e3).toFixed(1) + 'µs'
  return Math.round(ns) + 'ns'
}

export function fmtBytes(v: number): string {
  if (v >= 1073741824) return (v / 1073741824).toFixed(2) + ' GB'
  if (v >= 1048576) return (v / 1048576).toFixed(2) + ' MB'
  if (v >= 1024) return (v / 1024).toFixed(1) + ' kB'
  return v + ' B'
}

export function fmtVal(v: number, unit: string): string {
  if (unit === 'nanoseconds') return fmtNs(v)
  if (unit === 'bytes') return fmtBytes(v)
  return String(v) + (unit && unit !== 'count' ? ' ' + unit : '')
}
