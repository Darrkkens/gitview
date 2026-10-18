import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './styles/global.css'

// Development only: `?sample=<name>` opens a page exported by the Go side
// (npm run sample -- <page.html> <name> extracts its data to .samples/), so
// offline pages can be worked on without a build. Removed from production
// builds, whose pages never fetch anything.
async function devSample() {
  if (!import.meta.env.DEV) return
  const name = new URLSearchParams(location.search).get('sample')
  if (!name) return
  const res = await fetch('/.samples/' + encodeURIComponent(name) + '.json')
  if (!res.ok) throw new Error('sample ' + name + ': ' + res.status)
  const s = (await res.json()) as { envelope: unknown; sources?: string }
  const add = (id: string, type: string, text: string) => {
    const el = document.createElement('script')
    el.type = type
    el.id = id
    el.textContent = text
    document.body.appendChild(el)
  }
  add('gitview-data', 'application/json', JSON.stringify(s.envelope))
  if (s.sources) add('gitview-sources', 'application/gzip+base64', s.sources)
}

// The app reads the page's data when its modules load: import it after the
// sample (if any) is in place.
devSample().finally(async () => {
  const { default: App } = await import('./App.tsx')
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
})
