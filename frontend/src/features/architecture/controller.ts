// The explorer's state and navigation: scope, expansions, focus mode,
// selection and filters, the view built from them (async: ELK), and the
// canvas renderer it drives. React components read immutable snapshots
// (useSyncExternalStore) and call the actions.
//
// State lives in the URL too (#/architecture?scope=…&open=…&node=…), so a
// view can be linked and Back/Forward walk through scopes and focus targets
// (selection and expansion replace the current entry).

import { toast } from '../../components/Toast'
import { fmt } from '../../lib/format'
import { setParams } from '../../lib/router'
import { buildArch, expandable, highlight } from './arch'
import { buildFocus } from './focus'
import type { Model } from './model'
import type { FitOpts, Renderer, RendererEvents } from './renderer'
import type { Box, Filters, FocusState, Highlight, SceneEdge, Selection, View } from './types'
import { CONFS, FILTER_KINDS, FILTER_RELS, FUNC } from './vocab'

export interface ExplorerState {
  root: number
  open: Set<number>
  focus: FocusState | null
  sel: Selection | null
  filters: Filters
  /** 0: the selection's full dependency path; 1: direct neighbours only. */
  pathDepth: number
}

export type StageState = { kind: 'loading' } | { kind: 'error'; message: string } | { kind: 'empty' } | null

export interface Snapshot {
  S: ExplorerState
  view: View | null
  hl: Highlight | null
  stage: StageState
  /** Time of the last layout (ms). */
  layoutMs: number
  version: number
}

interface BuildOpts {
  fit?: boolean
  animate?: boolean
  anchor?: string
  reveal?: number
  /** After a restore: reveal the selection when it is not drawn. */
  revealHidden?: boolean
}

export interface SelectOpts {
  reveal?: boolean
  force?: boolean
  from?: 'tree' | 'search'
}

export function defaultFilters(): Filters {
  const f: Filters = { kinds: {}, rels: {}, confs: {}, ext: false }
  FILTER_KINDS.forEach((k) => (f.kinds[k] = true))
  FILTER_RELS.forEach((k) => (f.rels[k] = true))
  CONFS.forEach((k) => (f.confs[k] = true))
  return f
}

export function filtersChanged(f: Filters): boolean {
  const d = defaultFilters()
  return (
    f.ext !== d.ext ||
    FILTER_KINDS.some((k) => f.kinds[k] !== d.kinds[k]) ||
    FILTER_RELS.some((k) => f.rels[k] !== d.rels[k]) ||
    CONFS.some((k) => f.confs[k] !== d.confs[k])
  )
}

const PARAM_KEYS = ['scope', 'open', 'focus', 'up', 'down', 'refs', 'node'] as const
type Params = Record<(typeof PARAM_KEYS)[number], string | undefined>

/** canonical: the explorer's part of a route's parameters, as one comparable string. */
export function canonical(p: URLSearchParams | Partial<Params>): string {
  const get = (k: string) =>
    (p instanceof URLSearchParams ? p.get(k) : (p as Record<string, string | undefined>)[k]) ?? ''
  return PARAM_KEYS.map((k) => k + '=' + get(k)).join('&')
}

/** hasState tells whether route parameters carry any of the explorer's state. */
export const hasState = (p: URLSearchParams) => PARAM_KEYS.some((k) => p.has(k))

const clampDepth = (v: string | null) => {
  const n = Number(v)
  return v !== null && v !== '' && n >= 0 && n <= 6 ? n : 2
}

export class Explorer {
  readonly D: Model
  private S: ExplorerState
  private view: View | null = null
  private hl: Highlight | null = null
  private stage: StageState = null
  private layoutMs = 0
  private renderer: Renderer | null = null
  private boxByKey = new Map<string, Box>()
  private edgeByKey = new Map<string, SceneEdge>()
  private building = 0
  private pendingPush = false
  private lastWritten: string | null = null
  private disposed = false
  private listeners = new Set<() => void>()
  private snap: Snapshot
  private version = 0
  readonly maxNodes: number

  constructor(D: Model) {
    this.D = D
    this.maxNodes = D.raw.initial?.maxNodes || 5000
    this.S = { root: D.ROOT, open: this.initialOpen(), focus: null, sel: null, filters: defaultFilters(), pathDepth: 0 }
    this.snap = this.makeSnap()
  }

  // ---------- store ----------
  subscribe = (l: () => void) => {
    this.listeners.add(l)
    return () => {
      this.listeners.delete(l)
    }
  }
  getSnapshot = () => this.snap
  private makeSnap(): Snapshot {
    return {
      S: this.S,
      view: this.view,
      hl: this.hl,
      stage: this.stage,
      layoutMs: this.layoutMs,
      version: ++this.version,
    }
  }
  private emit() {
    this.snap = this.makeSnap()
    this.listeners.forEach((l) => l())
  }
  private set(patch: Partial<ExplorerState>) {
    this.S = { ...this.S, ...patch }
  }

  /** activate: the view is mounted (it may write the URL). */
  activate() {
    this.disposed = false
  }
  deactivate() {
    this.disposed = true
  }

  // ---------- renderer ----------
  attach(r: Renderer) {
    this.renderer = r
    if (this.view) {
      r.setScene(this.view.scene, { fit: true, fitOpts: this.fitOpts(this.view) })
      r.setSelected(this.selectedKey())
      r.setHighlight(this.hl)
    }
  }
  detach(r: Renderer) {
    if (this.renderer === r) this.renderer = null
  }
  readonly events: RendererEvents = {
    box: (box, _header, ev) => this.onBox(box, ev),
    edge: (e) => this.selectEdge(e.key),
    empty: () => this.clearSelection(),
    dblbox: (box) => this.onDblBox(box),
  }

  private aspect(): number {
    const s = this.renderer?.size()
    const w = s && s.w > 1 ? s.w : 1200,
      h = s && s.h > 1 ? s.h : 800
    return Math.max(0.5, Math.min(3, w / Math.max(1, h)))
  }

  initialOpen(): Set<number> {
    const { D } = this
    const open = new Set(D.modules.map((m) => m.i))
    const lvl = D.raw.initial?.level || ''
    if (lvl && lvl !== 'modules') {
      D.descendants(D.ROOT, (i) => {
        const t = D.T[i]
        if (t.ext || (t.n && t.n.ext) || t.kind === 'extgroup') return
        if (t.kind === 'dir') open.add(i)
        if (t.kind === 'package' && lvl !== 'packages') open.add(i)
      })
    }
    return open
  }

  // ---------- view ----------
  rebuild(opts: BuildOpts = {}) {
    return this.build(opts)
  }
  private async build(opts: BuildOpts) {
    const id = ++this.building
    const S = this.S
    const slow = setTimeout(() => {
      if (id === this.building) {
        this.stage = { kind: 'loading' }
        this.emit()
      }
    }, 150)
    let v: View
    const t0 = performance.now()
    try {
      v = S.focus
        ? await buildFocus(this.D, { focus: S.focus, filters: S.filters }, this.aspect())
        : await buildArch(this.D, S, this.aspect())
    } catch (err) {
      clearTimeout(slow)
      if (id !== this.building) return
      console.error(err)
      this.stage = { kind: 'error', message: String((err as Error)?.message || err) }
      this.emit()
      return
    }
    clearTimeout(slow)
    if (id !== this.building) return
    this.layoutMs = performance.now() - t0
    this.view = v
    this.boxByKey = new Map(v.scene.boxes.map((b) => [b.key, b]))
    this.edgeByKey = new Map(v.scene.edges.map((e) => [e.key, e]))
    this.stage = v.scene.boxes.length ? null : { kind: 'empty' }
    this.renderer?.setScene(v.scene, {
      fit: opts.fit,
      animate: opts.animate && !opts.fit,
      anchor: opts.anchor,
      fitOpts: this.fitOpts(v),
    })
    this.applySelection(false)
    if (opts.reveal !== undefined) {
      const k = v.keyOf(opts.reveal)
      if (k) this.renderer?.reveal([k])
    }
    if (opts.revealHidden && this.S.sel?.kind === 'item' && !this.S.focus) {
      const i = this.S.sel.i
      if (!v.keyOf(i) && !(v.kind === 'arch' && v.v.ctx[i] === i))
        void this.selectItem(i, { reveal: true, force: true })
    }
  }

  /**
   * Long focus graphs are fitted only while their text stays readable,
   * starting from the focused element; the architecture from its top left.
   */
  private fitOpts(v: View): FitOpts {
    if (v.kind === 'focus') return { minZoom: 0.62, anchor: 'f' + v.center, align: 'center' }
    return { minZoom: 0.4, anchor: v.scene.boxes.length ? v.scene.boxes[0].key : null, align: 'topleft' }
  }
  fitView() {
    if (this.view) this.renderer?.fit(true, this.fitOpts(this.view))
    else this.renderer?.fit()
  }
  zoomBy(f: number) {
    this.renderer?.zoomBy(f)
  }

  box(key: string) {
    return this.boxByKey.get(key)
  }
  edge(key: string) {
    return this.edgeByKey.get(key)
  }
  isDrawn(i: number) {
    return !!this.view?.keyOf(i)
  }
  visibleCount() {
    return this.view ? this.view.scene.boxes.length : 0
  }

  // ---------- selection ----------
  selectedKey(): string | null {
    const { S, view } = this
    if (!S.sel || !view) return null
    if (S.sel.kind === 'edge') return S.sel.key
    const i = S.sel.i
    return view.keyOf(i) || (view.kind === 'arch' && view.v.ctx[i] === i ? 'c' + i : null)
  }

  private keepOf(keys: string[]) {
    const keep = new Set<string>()
    keys.forEach((k) => {
      if (k.charAt(0) === 't') this.D.ancestors(Number(k.slice(1))).forEach((a) => keep.add('t' + a))
    })
    return keep
  }

  private applySelection(scroll: boolean) {
    const { S, view } = this
    const key = this.selectedKey()
    this.renderer?.setSelected(key)
    let hl: Highlight | null = null
    if (view && key && S.sel?.kind === 'item') hl = highlight(this.D, view, key, S.pathDepth || 0)
    else if (view && key && S.sel?.kind === 'edge') {
      const e = this.edgeByKey.get(key)
      if (e) {
        const ends = e.units ? Array.from(e.units) : [e.s, e.t]
        hl = {
          boxes: new Map(ends.map((k) => [k, 'sel'])),
          edges: new Map([[key, 'out']]),
          keep: this.keepOf(ends),
          down: 0,
          up: 0,
        }
        if (view.sceneOf)
          e.agg.members.forEach((g) => (view.sceneOf!.get(g.key) || []).forEach((sk) => hl!.edges.set(sk, 'out')))
      }
    }
    this.hl = hl
    this.renderer?.setHighlight(hl)
    this.emit()
    this.writeURL()
    if (scroll && key) this.renderer?.reveal([key])
  }

  /**
   * selectItem selects a tree item. opts.reveal makes it visible on the
   * canvas: it expands the item's ancestors (widening the scope when the
   * item is outside it) and leaves focus mode when asked from the tree or
   * the search.
   */
  async selectItem(i: number, opts: SelectOpts = {}) {
    const { D } = this
    if (i < 0 || !D.T[i]) return
    const view = this.view
    // the drawn view is behind the state (focus left): draw it again
    const stale = !!view && (view.kind === 'focus') !== !!this.S.focus
    this.set({ sel: { kind: 'item', i } })
    if (!stale && view && view.keyOf(i)) {
      this.applySelection(!!opts.reveal)
      return
    }
    if (!stale && view && view.kind === 'arch' && view.v.ctx[i] === i && !opts.force) {
      this.applySelection(true)
      return
    }
    if (!opts.reveal && !stale) {
      this.applySelection(false)
      return
    }
    if (this.S.focus) {
      if (opts.from || opts.force) {
        this.pushHistory()
        this.set({ focus: null })
      } else {
        this.applySelection(false)
        return
      }
    }
    // make it drawable
    const t = D.T[i]
    let filters = this.S.filters
    if ((t.n && t.n.ext) || t.ext || t.kind === 'extgroup') {
      if (!filters.ext) {
        filters = { ...filters, ext: true }
        toast('External dependencies are now shown (Filters).')
      }
    } else if (t.n && filters.kinds[t.kind] === false) {
      filters = { ...filters, kinds: { ...filters.kinds, [t.kind]: true } }
      toast(D.kindLabel(t) + ' elements are now shown (Filters).')
    }
    for (const a of D.ancestors(i))
      if (D.T[a].n && filters.kinds[D.T[a].kind] === false)
        filters = { ...filters, kinds: { ...filters.kinds, [D.T[a].kind]: true } }
    this.set({ filters })
    let root = this.S.root
    if (i !== root && !D.isAncestor(root, i)) {
      while (root !== D.ROOT && !D.isAncestor(root, i)) root = D.T[root].parent
      if (root !== this.S.root) {
        this.pushHistory()
        this.set({ root })
      }
    }
    if (i === this.S.root) {
      await this.rebuild({ fit: true })
      return
    }
    const open = new Set(this.S.open)
    for (const a of D.ancestors(i)) {
      if (a === this.S.root) break
      open.add(a)
    }
    this.set({ open })
    await this.rebuild({ animate: !stale, fit: stale, reveal: i })
  }

  selectEdge(key: string) {
    this.set({ sel: { kind: 'edge', key } })
    this.applySelection(false)
  }

  clearSelection() {
    if (!this.S.sel) return
    this.set({ sel: null })
    this.applySelection(false)
  }

  // ---------- navigation ----------
  toggle(i: number) {
    const { D, S } = this
    if (S.focus) return
    const t = D.T[i]
    if (!t || !expandable(t) || i === S.root) return
    const open = new Set(S.open)
    if (open.has(i)) open.delete(i)
    else {
      if (this.visibleCount() + t.kids.length > this.maxNodes) {
        toast('Expanding would draw more than ' + fmt(this.maxNodes) + ' elements. Open it as the scope instead.', {
          tone: 'warning',
          ms: 6000,
          action: { label: 'Open as scope', run: () => this.enter(i) },
        })
        return
      }
      if (t.kids.length > 400) toast('Large group: ' + fmt(t.kids.length) + ' elements, packed without layers.')
      open.add(i)
    }
    this.set({ open, sel: { kind: 'item', i } })
    void this.rebuild({ animate: true, anchor: 't' + i })
  }

  enter(i: number) {
    const t = this.D.T[i]
    if (!t || !expandable(t)) return
    this.pushHistory()
    const open = new Set(this.S.open)
    open.add(i)
    this.set({ focus: null, root: i, open, sel: null })
    void this.rebuild({ fit: true })
  }

  focusOn(i: number) {
    if (i < 0 || i === this.D.ROOT) return
    this.pushHistory()
    const f = this.S.focus
    this.set({
      focus: { i, up: f ? f.up : 2, down: f ? f.down : 2, refs: f ? f.refs : false },
      sel: { kind: 'item', i },
    })
    void this.rebuild({ fit: true })
  }

  setFocus(patch: Partial<FocusState>) {
    if (!this.S.focus) return
    this.set({ focus: { ...this.S.focus, ...patch } })
    void this.rebuild({ fit: true })
  }

  exitFocus() {
    const f = this.S.focus
    if (!f) return
    this.pushHistory()
    this.set({ focus: null })
    void this.selectItem(f.i, { reveal: true })
  }

  toggleFocus() {
    if (this.S.focus) {
      this.exitFocus()
      return
    }
    if (this.S.sel?.kind === 'item') this.focusOn(this.S.sel.i)
    else toast('Select an element first: focus mode draws it with what it is connected to.')
  }

  setFilters(f: Filters) {
    this.set({ filters: f })
    void this.rebuild({ animate: true })
  }

  setPathDepth(d: number) {
    this.set({ pathDepth: d })
    this.applySelection(false)
  }

  /** up: select the parent, leave focus mode, or widen the scope. */
  up() {
    const { D, S } = this
    if (S.sel?.kind === 'item') {
      const p = D.T[S.sel.i].parent
      if (!S.focus && p >= 0 && (p === S.root || D.isAncestor(S.root, p))) {
        void this.selectItem(p)
        return
      }
    }
    if (S.focus) {
      this.exitFocus()
      return
    }
    if (S.root !== D.ROOT) this.enterAncestor(D.T[S.root].parent, S.root)
  }

  enterAncestor(a: number, sel: number) {
    this.pushHistory()
    const open = new Set(this.S.open)
    open.add(a)
    this.set({ root: a, open, sel: sel >= 0 ? { kind: 'item', i: sel } : null })
    void this.rebuild({ fit: true })
  }

  /** Enter on the selection: expand or collapse it. */
  toggleSelection() {
    if (this.S.sel?.kind === 'item') this.toggle(this.S.sel.i)
  }

  // ---------- canvas events ----------
  private onBox(box: Box, _ev: PointerEvent) {
    if (box.kind === 'more') {
      toast(box.tip || 'More elements at this distance')
      return
    }
    if (box.ref === undefined) return
    const sel = this.S.sel
    if (sel?.kind === 'item' && sel.i === box.ref && box.kind !== 'group') {
      this.applySelection(false)
      return
    }
    this.set({ sel: { kind: 'item', i: box.ref } })
    this.applySelection(false)
  }

  private onDblBox(box: Box) {
    if (box.ref === undefined) return
    const t = this.D.T[box.ref]
    if (this.S.focus) {
      if (box.ref !== this.S.focus.i) this.focusOn(box.ref)
      return
    }
    if (box.kind === 'context') {
      void this.selectItem(box.ref, { reveal: true, force: true })
      return
    }
    // functions open in focus mode; containers and types expand in place
    if (expandable(t) && !FUNC[t.kind]) this.toggle(box.ref)
    else this.focusOn(box.ref)
  }

  // ---------- history and links ----------
  private pushHistory() {
    this.pendingPush = true
  }

  private encodeOpen(): string | undefined {
    const { D, S } = this
    const inside = (i: number) => i !== S.root && D.isAncestor(S.root, i)
    const list = Array.from(S.open)
      .filter(inside)
      .sort((a, b) => a - b)
    const init = Array.from(this.initialOpen())
      .filter(inside)
      .sort((a, b) => a - b)
    if (list.length === init.length && list.every((x, k) => x === init[k])) return undefined
    return list.length ? list.map((i) => D.T[i].id).join('|') : '-'
  }

  params(): Params {
    const { D, S } = this
    return {
      scope: S.root !== D.ROOT ? D.T[S.root].id : undefined,
      open: this.encodeOpen(),
      focus: S.focus ? D.T[S.focus.i].id : undefined,
      up: S.focus ? String(S.focus.up) : undefined,
      down: S.focus ? String(S.focus.down) : undefined,
      refs: S.focus?.refs ? '1' : undefined,
      node: S.sel?.kind === 'item' ? D.T[S.sel.i].id : undefined,
    }
  }

  private writeURL() {
    if (this.disposed || !/^#\/architecture(\?|$)/.test(location.hash || '#/architecture')) return
    const p = this.params()
    const key = canonical(p)
    const push = this.pendingPush
    this.pendingPush = false
    if (key === this.lastWritten) return
    this.lastWritten = key
    setParams(p, { replace: !push })
  }

  /** hasView: a view was drawn (the explorer has a state to come back to). */
  hasView() {
    return !!this.view
  }

  /** resume shows the current state again (coming back to the view) and writes it to the URL. */
  resume() {
    this.lastWritten = null
    this.pendingPush = false
    this.applySelection(false)
  }

  /** isCurrent tells whether route parameters are what the explorer last wrote. */
  isCurrent(p: URLSearchParams) {
    return canonical(p) === this.lastWritten
  }

  /** restore reads the state from the route's parameters and draws it. */
  restore(p: URLSearchParams) {
    const { D } = this
    const idx = (id: string | null) => (id ? (D.byId.get(id) ?? -1) : -1)
    const r = idx(p.get('scope'))
    const root = r >= 0 && expandable(D.T[r]) ? r : D.ROOT
    const o = p.get('open')
    const open = o
      ? new Set(
          o === '-'
            ? []
            : o
                .split('|')
                .map(idx)
                .filter((i) => i >= 0),
        )
      : this.initialOpen()
    open.add(root)
    const f = idx(p.get('focus'))
    const focus =
      f >= 0
        ? { i: f, up: clampDepth(p.get('up')), down: clampDepth(p.get('down')), refs: p.get('refs') === '1' }
        : null
    const s = idx(p.get('node'))
    this.set({ root, open, focus, sel: s >= 0 ? { kind: 'item', i: s } : null })
    this.pendingPush = false
    this.lastWritten = canonical(p)
    void this.rebuild({ fit: true, revealHidden: true })
  }

  /** linkTo: a link to an element in this page. */
  linkTo(i: number): string {
    const base = location.href.replace(/#.*$/, '')
    return base + '#/architecture?node=' + encodeURIComponent(this.D.T[i].id)
  }
}
