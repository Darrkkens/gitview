// The envelope is the page's data: the Go side writes it into
// <script type="application/json" id="gitview-data"> (internal/webui).
// An offline export carries its sections inline; a page served by
// `gitview serve` carries none and a live token, and fetches each section
// from the local API on demand.

export type SectionName = 'graph' | 'security' | 'report' | 'runtime'

export const SECTION_NAMES: SectionName[] = ['graph', 'security', 'report', 'runtime']

export type EnvelopeKind = 'graph' | 'security' | 'report' | 'runtime' | 'serve'

export interface Live {
  token: string
}

export interface Envelope {
  kind: EnvelopeKind
  title: string
  repository: string
  generated: string // RFC 3339
  /** First view to open when the URL has none (e.g. "security"). */
  view?: string
  sections: Partial<Record<SectionName, unknown>>
  /** Sections the server can compute (serve mode). */
  offered?: SectionName[]
  live?: Live
}

function devEnvelope(): Envelope {
  // `npm run dev`: no Go-written document; the API behind the Vite proxy is
  // a `gitview serve`, and its token comes from the URL it printed.
  const token = new URLSearchParams(location.search).get('t') ?? ''
  return {
    kind: 'serve',
    title: 'GitView (dev)',
    repository: '',
    generated: new Date().toISOString(),
    sections: {},
    live: { token },
  }
}

let cached: Envelope | null = null

export function envelope(): Envelope {
  if (cached) return cached
  const el = document.getElementById('gitview-data')
  if (!el || !el.textContent) {
    cached = devEnvelope()
    return cached
  }
  const raw = JSON.parse(el.textContent) as Envelope
  raw.sections ??= {}
  cached = raw
  return cached
}

export const isLive = () => !!envelope().live
