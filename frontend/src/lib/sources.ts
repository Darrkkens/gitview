// The repository's Go source shown by the code views. Offline pages embed
// it compressed (<script type="application/gzip+base64" id="gitview-sources">:
// base64 of the gzip of a JSON object path → content); `gitview serve`
// returns the same encoding from /api/sources. Decoded once, on first use.

import { apiText } from './api'
import { isLive } from './envelope'

export type SourceFiles = Record<string, string>

let loading: Promise<SourceFiles | null> | null = null

async function decode(b64: string): Promise<SourceFiles | null> {
  b64 = b64.trim()
  if (!b64) return null
  if (typeof DecompressionStream === 'undefined') {
    throw new Error('This browser cannot decompress the embedded source (no DecompressionStream).')
  }
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
  const text = await new Response(new Blob([bytes]).stream().pipeThrough(new DecompressionStream('gzip'))).text()
  return JSON.parse(text) as SourceFiles
}

/** loadSources resolves to the files, or null when the page has none. */
export function loadSources(): Promise<SourceFiles | null> {
  if (loading) return loading
  const el = document.getElementById('gitview-sources')
  if (el && el.textContent && el.textContent.trim()) {
    loading = decode(el.textContent)
  } else if (isLive()) {
    loading = apiText('/api/sources').then(decode)
  } else {
    loading = Promise.resolve(null)
  }
  return loading
}

/** Forget the decoded files (serve mode, after a re-analysis). */
export function resetSources() {
  loading = null
}
