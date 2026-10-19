// Where the layout document lives, and its history (undo/redo).
//
//   serve mode     GET /api/layout at start (204: none yet); every edit is
//                  saved with PUT /api/layout (the whole document, debounced)
//                  into GitView's cache, outside the repository.
//   offline page   the document embedded by `gitview graph export` (the
//                  graph section's "layout"), overridden by the viewer's own
//                  edits kept in this browser (localStorage: a convenience,
//                  never required; a page opened from file:// may not get it).
//
// The document is only ever parsed and validated (layoutDoc.ts).

import { ApiError, apiText } from '../../lib/api'
import { envelope, isLive } from '../../lib/envelope'
import {
  cloneDoc,
  cloneView,
  emptyDoc,
  emptyView,
  isEmptyView,
  LayoutError,
  parseLayout,
  serializeLayout,
  type LayoutDoc,
  type ViewLayout,
} from './layoutDoc'

export type SaveState =
  | { kind: 'clean' }
  | { kind: 'pending' }
  | { kind: 'saving' }
  | { kind: 'saved'; where: 'server' | 'browser' }
  | { kind: 'error'; message: string }
  /** offline, no browser storage: kept until the page closes (export to keep it) */
  | { kind: 'memory' }

/** Where the document in use came from. */
export type Source = 'server' | 'embedded' | 'browser' | 'none'

type Entry =
  | { kind: 'view'; key: string; label: string; before: ViewLayout | null; after: ViewLayout | null }
  | { kind: 'doc'; label: string; before: LayoutDoc; after: LayoutDoc }

export interface StoreSnapshot {
  state: SaveState
  source: Source
  live: boolean
  embedded: boolean
  undo: string | null
  redo: string | null
  version: number
}

const MAX_HISTORY = 100

/** apiPut sends a JSON document to the local server (with the page's token, like lib/api.ts). */
async function apiPut<T>(path: string, body: string, keepalive = false): Promise<T> {
  const h = new Headers({ 'Content-Type': 'application/json' })
  const live = envelope().live
  if (live) h.set('X-GitView-Token', live.token)
  const res = await fetch(path, { method: 'PUT', headers: h, body, keepalive })
  if (!res.ok) {
    let msg = res.statusText
    try {
      const text = await res.text()
      try {
        msg = (JSON.parse(text) as { error?: string }).error || text
      } catch {
        msg = text || msg
      }
    } catch {
      /* keep statusText */
    }
    throw new ApiError(res.status, msg.trim())
  }
  return (await res.json()) as T
}

function localGet(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}
function localSet(key: string, value: string | null): boolean {
  try {
    if (value === null) localStorage.removeItem(key)
    else localStorage.setItem(key, value)
    return true
  } catch {
    return false
  }
}

export class LayoutStore {
  readonly repository: string
  private doc: LayoutDoc
  private embeddedDoc: LayoutDoc | null = null
  private source: Source = 'none'
  private state: SaveState = { kind: 'clean' }
  private undoStack: Entry[] = []
  private redoStack: Entry[] = []
  private listeners = new Set<() => void>()
  private snap: StoreSnapshot
  private version = 0
  private timer: ReturnType<typeof setTimeout> | undefined
  private saving = false
  private again = false
  private known: (id: string) => boolean
  readonly live = isLive()
  readonly ready: Promise<void>
  /** Warnings of the document loaded at start (shown once by the view). */
  loadWarnings: string[] = []

  constructor(repository: string, embedded: unknown, known: (id: string) => boolean) {
    this.repository = repository
    this.known = known
    this.doc = emptyDoc(repository)
    if (embedded !== undefined && embedded !== null) {
      try {
        const r = parseLayout(embedded, known)
        this.embeddedDoc = r.doc
        this.loadWarnings.push(...r.warnings)
      } catch (e) {
        this.loadWarnings.push('The embedded layout was ignored: ' + (e as Error).message)
      }
    }
    this.snap = this.makeSnap()
    this.ready = this.load()
    if (this.live && typeof window !== 'undefined')
      window.addEventListener('pagehide', () => {
        if (this.state.kind === 'pending') void this.flush(true)
      })
  }

  private get localKey() {
    return 'gitview.arch.layout.' + this.repository
  }

  private async load() {
    if (this.live) {
      try {
        // an empty answer: nothing saved yet
        const text = await apiText('/api/layout')
        if (text.trim()) {
          const r = parseLayout(text, this.known)
          this.doc = r.doc
          this.doc.repository = this.repository
          this.source = 'server'
          this.loadWarnings.push(...r.warnings)
        }
      } catch (e) {
        if (!(e instanceof ApiError && e.status === 404)) {
          this.loadWarnings.push('The saved layout could not be read: ' + (e as Error).message)
          this.state = { kind: 'error', message: (e as Error).message }
        }
      }
    } else {
      const text = localGet(this.localKey)
      let local: LayoutDoc | null = null
      if (text)
        try {
          local = parseLayout(text, this.known).doc
        } catch {
          local = null
        }
      if (local) {
        this.doc = local
        this.source = 'browser'
      } else if (this.embeddedDoc) {
        this.doc = cloneDoc(this.embeddedDoc)
        this.source = 'embedded'
      }
    }
    this.emit()
  }

  // ---------- store ----------
  subscribe = (l: () => void) => {
    this.listeners.add(l)
    return () => {
      this.listeners.delete(l)
    }
  }
  getSnapshot = () => this.snap
  private makeSnap(): StoreSnapshot {
    const u = this.undoStack[this.undoStack.length - 1],
      r = this.redoStack[this.redoStack.length - 1]
    return {
      state: this.state,
      source: this.source,
      live: this.live,
      embedded: !!this.embeddedDoc,
      undo: u ? u.label : null,
      redo: r ? r.label : null,
      version: ++this.version,
    }
  }
  private emit() {
    this.snap = this.makeSnap()
    this.listeners.forEach((l) => l())
  }

  // ---------- reading ----------
  /** view: the layout of a view (a default one when it has none; not stored until edited). */
  view(key: string): ViewLayout {
    return this.doc.views[key] ?? emptyView()
  }
  has(key: string): boolean {
    return !isEmptyView(this.doc.views[key])
  }
  document(): LayoutDoc {
    return this.doc
  }
  serialize(pretty = true): string {
    return serializeLayout(this.doc, pretty)
  }
  /** serializeView: one view, in the single-view shape ({"layout_version": 1, "view": …, "nodes": …}). */
  serializeView(key: string): string {
    const one = serializeLayout({ layout_version: 1, repository: this.repository, views: { [key]: this.view(key) } }, false)
    const v = (JSON.parse(one) as { views: Record<string, Record<string, unknown>> }).views[key] ?? { nodes: {} }
    return JSON.stringify({ layout_version: 1, repository: this.repository, view: key, ...v }, null, 2) + '\n'
  }

  // ---------- editing ----------
  /**
   * edit changes one view's layout through fn and records it for undo. A
   * change that leaves the view as it was records nothing.
   */
  edit(key: string, label: string, fn: (v: ViewLayout) => void, record = true): boolean {
    const cur = this.doc.views[key]
    const before = cur ? cloneView(cur) : null
    const next = cur ? cloneView(cur) : emptyView()
    fn(next)
    const after = isEmptyView(next) ? null : next
    if (JSON.stringify(before) === JSON.stringify(after)) return false
    if (after) this.doc.views[key] = after
    else delete this.doc.views[key]
    if (record) this.push({ kind: 'view', key, label, before, after: after ? cloneView(after) : null })
    this.changed()
    return true
  }

  /** replace swaps the whole document (import, restore), recorded for undo. */
  replace(doc: LayoutDoc, label: string) {
    const before = cloneDoc(this.doc)
    this.doc = cloneDoc(doc)
    this.doc.repository = this.repository
    this.push({ kind: 'doc', label, before, after: cloneDoc(this.doc) })
    this.changed()
  }

  private push(e: Entry) {
    this.undoStack.push(e)
    if (this.undoStack.length > MAX_HISTORY) this.undoStack.shift()
    this.redoStack = []
  }

  /** undo reverts the last edit; returns the view it touched ("*" for the whole document). */
  undo(): string | null {
    const e = this.undoStack.pop()
    if (!e) return null
    this.redoStack.push(e)
    this.apply(e, 'before')
    return e.kind === 'view' ? e.key : '*'
  }
  redo(): string | null {
    const e = this.redoStack.pop()
    if (!e) return null
    this.undoStack.push(e)
    this.apply(e, 'after')
    return e.kind === 'view' ? e.key : '*'
  }
  private apply(e: Entry, side: 'before' | 'after') {
    if (e.kind === 'view') {
      const v = e[side]
      if (v) this.doc.views[e.key] = cloneView(v)
      else delete this.doc.views[e.key]
    } else this.doc = cloneDoc(e[side])
    this.changed()
  }

  // ---------- import, restore ----------
  /**
   * importText reads a layout file: a whole document replaces the current
   * one, a single view replaces that view. Returns a summary for the viewer.
   * Throws LayoutError on anything that is not a layout document.
   */
  importText(text: string): { summary: string; warnings: string[]; views: string[] } {
    const r = parseLayout(text, this.known)
    const warnings = r.warnings.slice()
    if (r.doc.repository && r.doc.repository !== this.repository)
      warnings.unshift('This layout was saved for "' + r.doc.repository + '", not "' + this.repository + '".')
    const known = r.positions - r.unknown
    let summary = known + ' position' + (known === 1 ? '' : 's')
    if (r.unknown) summary += ' (' + r.unknown + ' unknown ID' + (r.unknown === 1 ? '' : 's') + ' ignored)'
    if (r.single) {
      const v = r.doc.views[r.single]
      this.edit(r.single, 'Import', (cur) => {
        cur.algorithm = v.algorithm
        cur.edges = v.edges
        cur.nodes = v.nodes
        if (v.expanded) cur.expanded = v.expanded
      })
      return { summary, warnings, views: [r.single] }
    }
    this.replace(r.doc, 'Import')
    return { summary, warnings, views: Object.keys(r.doc.views) }
  }

  /** restoreEmbedded drops the viewer's edits for the document the page was exported with. */
  restoreEmbedded() {
    if (!this.embeddedDoc) return
    this.replace(this.embeddedDoc, 'Restore embedded layout')
    if (!this.live) {
      localSet(this.localKey, null)
      clearTimeout(this.timer)
      this.state = { kind: 'clean' }
      this.source = 'embedded'
      this.emit()
    }
  }

  /** discardAll: every view back to the automatic layout. */
  discardAll() {
    this.replace(emptyDoc(this.repository), 'Restore automatic layout')
  }

  // ---------- saving ----------
  private changed() {
    this.state = { kind: 'pending' }
    this.emit()
    clearTimeout(this.timer)
    this.timer = setTimeout(() => void this.flush(), this.live ? 700 : 300)
  }

  /** flush saves now (serve mode: to the server; offline: to this browser). */
  async flush(keepalive = false): Promise<void> {
    clearTimeout(this.timer)
    if (!this.live) {
      const ok = localSet(this.localKey, serializeLayout(this.doc, false))
      this.state = ok ? { kind: 'saved', where: 'browser' } : { kind: 'memory' }
      if (ok) this.source = 'browser'
      this.emit()
      return
    }
    if (this.saving) {
      this.again = true
      return
    }
    this.saving = true
    this.state = { kind: 'saving' }
    this.emit()
    try {
      await apiPut<{ saved?: boolean }>('/api/layout', serializeLayout(this.doc, false), keepalive)
      this.state = { kind: 'saved', where: 'server' }
      this.source = 'server'
    } catch (e) {
      const status = e instanceof ApiError ? e.status : 0
      const message =
        status === 404 || status === 405
          ? 'this server does not save layouts (update GitView); download the layout to keep it'
          : (e as Error).message || 'network error'
      this.state = { kind: 'error', message }
    } finally {
      this.saving = false
    }
    this.emit()
    if (this.again) {
      this.again = false
      void this.flush()
    }
  }
}

export { LayoutError }
