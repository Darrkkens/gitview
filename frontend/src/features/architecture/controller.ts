// The explorer's state and navigation: scope, expansions, focus mode,
// selection and filters, the view built from them (async: ELK), and the
// canvas renderer it drives. React components read immutable snapshots
// (useSyncExternalStore) and call the actions.
//
// State lives in the URL too (#/architecture?scope=…&open=…&node=…), so a
// view can be linked and Back/Forward walk through scopes and focus targets
// (selection and expansion replace the current entry).
//
// What the viewer arranges is kept apart (persist.ts, layoutDoc.ts): the
// algorithm, the line style and the positions of each view, by stable IDs.
// The states never mix:
//   automatic positions   computed by every build (arch.ts, focus.ts)
//   viewer's positions    the layout document (saved, undoable)
//   camera                the renderer's, never saved
//   selection             here (the primary element in the URL)
//   expansion             here and in the URL (and in the document per view)

import { toast } from '../../components/Toast'
import { download } from '../../lib/browser'
import { fmt } from '../../lib/format'
import { setParams } from '../../lib/router'
import { buildArch, expandable, highlight, HIER_MAX, type LayoutInput } from './arch'
import { relativePosition, SceneEditor } from './edit'
import { buildFocus, isSymbolFocus } from './focus'
import { align as alignRects, distribute as distributeRects, type AlignMode } from './geometry'
import {
  ALGORITHM_INFO,
  DEFAULT_ALGORITHM,
  EDGE_INFO,
  LayoutError,
  viewKey,
  type Algorithm,
  type EdgeStyle,
} from './layoutDoc'
import type { Model } from './model'
import { LayoutStore } from './persist'
import type { FitOpts, PointerMods, Renderer, RendererEvents } from './renderer'
import type { Box, Direction, Filters, FocusState, Highlight, SceneEdge, Selection, View } from './types'
import { CONFS, FILTER_KINDS, FILTER_RELS, FUNC, SYMBOL } from './vocab'

export interface ExplorerState {
  root: number
  open: Set<number>
  focus: FocusState | null
  sel: Selection | null
  /** Every selected element (the primary one, sel, included). */
  multi: ReadonlySet<number>
  filters: Filters
  /** 0: the selection's full dependency path; 1: direct neighbours only. */
  pathDepth: number
  /** Which relations of the selection are highlighted. */
  direction: Direction
  /** Draw only the selection and what it relates to. */
  isolate: boolean
  /** Hide lines that carry only weak relations or type uses. */
  hideMinor: boolean
}

export type StageState = { kind: 'loading' } | { kind: 'error'; message: string } | { kind: 'empty' } | null

/** The three scales of the explorer. */
export type Scale = 'overview' | 'module' | 'symbol'

export interface Snapshot {
  S: ExplorerState
  view: View | null
  hl: Highlight | null
  stage: StageState
  /** Time of the last layout (ms). */
  layoutMs: number
  /** The layout of the current view, as the viewer set it. */
  layout: { key: string; algorithm: Algorithm; edges: EdgeStyle; positions: number; locked: number }
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

/** The overview opens directories (and packages while it stays small), never thousands of cards. */
const OVERVIEW_BUDGET = 260

const EMPTY_MULTI: ReadonlySet<number> = new Set()

export class Explorer {
  readonly D: Model
  readonly store: LayoutStore
  private S: ExplorerState
  private view: View | null = null
  private editor: SceneEditor | null = null
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
  private warned = false
  /** The view last drawn with another algorithm than asked (warned once). */
  private fellBack: string | null = null
  readonly maxNodes: number

  constructor(D: Model) {
    this.D = D
    this.maxNodes = D.raw.initial?.maxNodes || 5000
    this.store = new LayoutStore(D.raw.repository, D.raw.layout, (id) => D.byId.has(id))
    this.S = {
      root: D.ROOT,
      open: this.initialOpen(),
      focus: null,
      sel: null,
      multi: EMPTY_MULTI,
      filters: defaultFilters(),
      pathDepth: 0,
      direction: 'both',
      isolate: false,
      hideMinor: false,
    }
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
    const key = this.viewKey()
    const L = this.store.view(key)
    let locked = 0,
      positions = 0
    for (const k in L.nodes) {
      if (!this.D.byId.has(k)) continue
      positions++
      if (L.nodes[k].locked) locked++
    }
    return {
      S: this.S,
      view: this.view,
      hl: this.hl,
      stage: this.stage,
      layoutMs: this.layoutMs,
      layout: { key, algorithm: L.algorithm, edges: L.edges, positions, locked },
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
    r.setOptions({ isolate: this.S.isolate, hideMinor: this.S.hideMinor })
    if (this.view) {
      r.setScene(this.view.scene, { fit: true, fitOpts: this.fitOpts(this.view), editor: this.editor ?? undefined })
      r.setSelected(this.selectedKeys())
      r.setHighlight(this.hl)
    }
  }
  detach(r: Renderer) {
    if (this.renderer === r) this.renderer = null
  }
  readonly events: RendererEvents = {
    box: (box, _header, mods) => this.onBox(box, mods),
    edge: (e) => this.selectEdge(e.key),
    empty: (mods) => {
      if (!mods.shift && !mods.toggle) this.clearSelection()
    },
    dblbox: (box) => this.onDblBox(box),
    dragStart: (box, mods) => this.onDragStart(box, mods),
    dragEnd: (moved) => this.commitMoves(moved, 'Move'),
    marquee: (keys, additive) => this.selectKeys(keys, additive),
    nudge: (dx, dy) => this.nudge(dx, dy),
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
      let budget = OVERVIEW_BUDGET
      D.descendants(D.ROOT, (i) => {
        const t = D.T[i]
        if (t.ext || (t.n && t.n.ext) || t.kind === 'extgroup') return
        if (t.kind === 'dir') open.add(i)
        if (t.kind === 'package' && lvl !== 'packages' && budget - t.kids.length > 0) {
          budget -= t.kids.length
          open.add(i)
        }
      })
    }
    return open
  }

  // ---------- layout of the current view ----------
  viewKey(S: ExplorerState = this.S): string {
    const { D } = this
    return viewKey(S.root !== D.ROOT ? D.T[S.root].id : null, S.focus ? D.T[S.focus.i].id : null)
  }
  private layoutInput(): LayoutInput {
    const L = this.store.view(this.viewKey())
    return { algorithm: L.algorithm, edges: L.edges, nodes: L.nodes }
  }
  /** expandedOf: the expansions stored for a view, as tree items (null: none stored). */
  private expandedOf(key: string): Set<number> | null {
    const list = this.store.view(key).expanded
    if (!list) return null
    const out = new Set<number>()
    for (const id of list) {
      const i = this.D.byId.get(id)
      if (i !== undefined) out.add(i)
    }
    return out
  }
  private openInside(): string[] {
    const { D, S } = this
    if (S.focus) return []
    return Array.from(S.open)
      .filter((i) => i !== S.root && D.isAncestor(S.root, i))
      .sort((a, b) => a - b)
      .map((i) => D.T[i].id)
  }

  // ---------- view ----------
  rebuild(opts: BuildOpts = {}) {
    return this.build(opts)
  }
  private async build(opts: BuildOpts) {
    const id = ++this.building
    await this.store.ready
    if (id !== this.building) return
    if (!this.warned && this.store.loadWarnings.length) {
      this.warned = true
      toast(this.store.loadWarnings[0], { tone: 'warning', ms: 7000 })
    }
    const S = this.S
    const slow = setTimeout(() => {
      if (id === this.building) {
        this.stage = { kind: 'loading' }
        this.emit()
      }
    }, 150)
    let v: View
    const t0 = performance.now()
    const L = this.layoutInput()
    try {
      v = S.focus
        ? await buildFocus(this.D, { focus: S.focus, filters: S.filters }, this.aspect(), L)
        : await buildArch(this.D, S, this.aspect(), L)
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
    this.editor = new SceneEditor(v.scene)
    this.boxByKey = this.editor.byKey
    this.edgeByKey = new Map(v.scene.edges.map((e) => [e.key, e]))
    this.stage = v.scene.boxes.length ? null : { kind: 'empty' }
    const fell = v.layout.fellBack ? this.viewKey() : null
    if (fell && fell !== this.fellBack && v.layout.algorithm === 'hierarchical')
      toast('Hierarchical layout runs on up to ' + fmt(HIER_MAX) + ' cards: this view is laid out as Grouped.', {
        tone: 'warning',
      })
    this.fellBack = fell
    this.renderer?.setScene(v.scene, {
      fit: opts.fit,
      animate: opts.animate && !opts.fit,
      anchor: opts.anchor,
      fitOpts: this.fitOpts(v),
      editor: this.editor,
    })
    // the selection keeps what is still drawn
    if (S.multi.size > 1) {
      const multi = new Set(Array.from(S.multi).filter((i) => this.keyOfItem(i)))
      if (multi.size !== S.multi.size) this.set({ multi })
    }
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
  /** fitView: the view's own framing (readable, from its top left): the reset view. */
  fitView() {
    if (this.view) this.renderer?.fit(true, this.fitOpts(this.view))
    else this.renderer?.fit()
  }
  /** zoomToFit: everything in view, however small. */
  zoomToFit() {
    this.renderer?.fit(true)
  }
  fitSelection() {
    const keys = this.selectedKeys()
    if (!keys.length) {
      toast('Nothing is selected.')
      return
    }
    this.renderer?.fitKeys(keys, true)
  }
  zoomBy(f: number) {
    this.renderer?.zoomBy(f)
  }

  box(key: string) {
    return this.boxByKey.get(key)
  }
  /** screenOf: where a drawn box is on the page (client coordinates). */
  screenOf(key: string) {
    return this.renderer?.screenRect(key) ?? null
  }
  /** rendererStats: draw and move timings (checks and the status bar). */
  rendererStats() {
    return this.renderer?.stats ?? null
  }
  camera() {
    return this.renderer?.camera() ?? null
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
  /** keyOfItem: the box drawing an item (inside the scope or around it). */
  keyOfItem(i: number): string | null {
    const view = this.view
    if (!view) return null
    return view.keyOf(i) || (view.kind === 'arch' && view.v.ctx[i] === i ? 'c' + i : null)
  }
  selectedKey(): string | null {
    const { S } = this
    if (!S.sel || !this.view) return null
    if (S.sel.kind === 'edge') return S.sel.key
    return this.keyOfItem(S.sel.i)
  }
  /** selectedKeys: the boxes (or the line) selected. */
  selectedKeys(): string[] {
    const { S } = this
    if (S.sel?.kind === 'edge') return [S.sel.key]
    const out: string[] = []
    const items = S.multi.size ? Array.from(S.multi) : S.sel ? [S.sel.i] : []
    for (const i of items) {
      const k = this.keyOfItem(i)
      if (k) out.push(k)
    }
    return out
  }
  /** selectedBoxes: the drawn boxes of the selection. */
  selectedBoxes(): Box[] {
    return this.selectedKeys()
      .map((k) => this.boxByKey.get(k))
      .filter((b): b is Box => !!b)
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
    const keys = this.selectedKeys()
    const key = this.selectedKey()
    this.renderer?.setSelected(keys)
    let hl: Highlight | null = null
    if (view && keys.length && S.sel?.kind !== 'edge') hl = highlight(this.D, view, keys, S.pathDepth || 0, S.direction)
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
    this.set({ sel: { kind: 'item', i }, multi: new Set([i]) })
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
    this.set({ sel: { kind: 'edge', key }, multi: EMPTY_MULTI })
    this.applySelection(false)
  }

  clearSelection() {
    if (!this.S.sel && !this.S.multi.size) return
    this.set({ sel: null, multi: EMPTY_MULTI })
    this.applySelection(false)
  }

  /** selectKeys selects drawn boxes (a rectangle, select all); additive keeps the selection. */
  selectKeys(keys: string[], additive: boolean) {
    const refs = keys.map((k) => this.boxByKey.get(k)?.ref).filter((r): r is number => r !== undefined)
    const multi = new Set(additive && this.S.sel?.kind !== 'edge' ? this.S.multi : [])
    refs.forEach((r) => multi.add(r))
    if (!multi.size) {
      if (!additive) this.clearSelection()
      return
    }
    const cur = this.S.sel?.kind === 'item' && multi.has(this.S.sel.i) ? this.S.sel : null
    this.set({ multi, sel: cur ?? { kind: 'item', i: refs[0] ?? Array.from(multi)[0] } })
    this.applySelection(false)
  }

  /** selectAll selects every card drawn (not the groups around them). */
  selectAll() {
    if (!this.view) return
    this.selectKeys(
      this.view.scene.boxes.filter((b) => b.kind !== 'group' && b.ref !== undefined).map((b) => b.key),
      false,
    )
  }

  /** toggleInSelection adds an item to the selection, or takes it out. */
  private toggleInSelection(i: number) {
    const multi = new Set(this.S.multi.size ? this.S.multi : this.S.sel?.kind === 'item' ? [this.S.sel.i] : [])
    if (multi.has(i)) {
      multi.delete(i)
      const sel = this.S.sel?.kind === 'item' && this.S.sel.i === i ? null : this.S.sel
      const first = multi.values().next()
      this.set({ multi, sel: sel ?? (first.done ? null : { kind: 'item', i: first.value }) })
    } else {
      multi.add(i)
      this.set({ multi, sel: { kind: 'item', i } })
    }
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
    this.set({ open, sel: { kind: 'item', i }, multi: new Set([i]) })
    this.store.edit(this.viewKey(), 'Expand', (v) => (v.expanded = this.openInside()), false)
    void this.rebuild({ animate: true, anchor: 't' + i })
  }

  /** viewOpen: the expansions to use entering a view (stored for it, or the current ones). */
  private viewOpen(S: ExplorerState, base: Set<number>): Set<number> {
    const stored = this.expandedOf(this.viewKey(S))
    if (!stored) return base
    stored.add(S.root)
    return stored
  }

  enter(i: number) {
    const t = this.D.T[i]
    if (!t || !expandable(t)) return
    this.pushHistory()
    const open = new Set(this.S.open)
    open.add(i)
    // a package opened as the scope shows its files' declarations when they are few
    if (t.kind === 'package' || t.kind === 'file') {
      let budget = 120
      for (const k of t.kids)
        if (expandable(this.D.T[k]) && budget - this.D.T[k].kids.length >= 0) {
          budget -= this.D.T[k].kids.length
          open.add(k)
        }
    }
    const next: ExplorerState = { ...this.S, focus: null, root: i, open, sel: null, multi: EMPTY_MULTI }
    this.set({ focus: null, root: i, open: this.viewOpen(next, open), sel: null, multi: EMPTY_MULTI })
    void this.rebuild({ fit: true })
  }

  focusOn(i: number) {
    if (i < 0 || i === this.D.ROOT) return
    this.pushHistory()
    const f = this.S.focus
    this.set({
      focus: { i, up: f ? f.up : 2, down: f ? f.down : 2, refs: f ? f.refs : false },
      sel: { kind: 'item', i },
      multi: new Set([i]),
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

  setDirection(d: Direction) {
    this.set({ direction: d })
    this.applySelection(false)
  }

  setIsolate(on: boolean) {
    if (on && !this.S.sel) toast('Select an element first: only it and what it relates to stay drawn.')
    this.set({ isolate: on })
    this.renderer?.setOptions({ isolate: on })
    this.emit()
  }

  setHideMinor(on: boolean) {
    this.set({ hideMinor: on })
    this.renderer?.setOptions({ hideMinor: on })
    this.emit()
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
    const selection = sel >= 0 ? ({ kind: 'item', i: sel } as const) : null
    const next: ExplorerState = { ...this.S, root: a, open, focus: null }
    const o = this.viewOpen(next, open)
    this.set({ root: a, open: o, focus: null, sel: selection, multi: sel >= 0 ? new Set([sel]) : EMPTY_MULTI })
    void this.rebuild({ fit: true })
  }

  /** Enter on the selection: expand or collapse it. */
  toggleSelection() {
    if (this.S.sel?.kind === 'item') this.toggle(this.S.sel.i)
  }

  // ---------- scales ----------
  scale(): Scale {
    const { S, D } = this
    if (S.focus) return isSymbolFocus(D.T[S.focus.i]) ? 'symbol' : 'module'
    return S.root === D.ROOT ? 'overview' : 'module'
  }

  /** drillTarget: where "drill down" goes from the selection (null: nowhere). */
  drillTarget(): { kind: 'module' | 'symbol'; i: number } | null {
    const { S, D } = this
    if (S.sel?.kind !== 'item') return null
    const i = S.sel.i
    const t = D.T[i]
    if (S.focus && S.focus.i === i) return null
    if (t.n && SYMBOL[t.kind]) return { kind: 'symbol', i }
    if (expandable(t) && i !== S.root && !S.focus) return { kind: 'module', i }
    if (S.focus) return { kind: t.n && SYMBOL[t.kind] ? 'symbol' : 'module', i }
    return null
  }

  drillDown() {
    const d = this.drillTarget()
    if (!d) {
      toast('Select a package, file or symbol to drill down into.')
      return
    }
    if (d.kind === 'symbol') this.focusOn(d.i)
    else if (this.S.focus) this.focusOn(d.i)
    else this.enter(d.i)
  }

  /** goScale moves to a scale explicitly (the scale bar). */
  goScale(s: Scale) {
    const { S, D } = this
    if (s === 'overview') {
      if (S.focus) {
        this.pushHistory()
        this.set({ focus: null })
      }
      // the overview selects the package of what was selected (it draws no symbols)
      const from = S.sel?.kind === 'item' ? S.sel.i : S.focus ? S.focus.i : -1
      const p = from >= 0 ? D.packageOf(from) : -1
      if (this.S.root !== D.ROOT || S.focus) this.enterAncestor(D.ROOT, p >= 0 ? p : from)
      return
    }
    const i = S.sel?.kind === 'item' ? S.sel.i : S.focus ? S.focus.i : -1
    if (s === 'symbol') {
      if (i >= 0 && D.T[i].n && SYMBOL[D.T[i].kind]) this.focusOn(i)
      else toast('Select a function, method or type first: the Symbol explorer shows its callers and callees.')
      return
    }
    // module: the package (or module) of the selection
    let p = i >= 0 ? D.packageOf(i) : -1
    if (p < 0 && i >= 0 && expandable(D.T[i]) && D.T[i].kind !== 'repo') p = i
    if (p < 0) {
      toast('Select an element first: the Module explorer opens its package.')
      return
    }
    if (S.focus) {
      this.pushHistory()
      this.set({ focus: null })
    }
    this.enter(p)
  }

  // ---------- canvas events ----------
  private onBox(box: Box, mods: PointerMods) {
    if (box.kind === 'more') {
      toast(box.tip || 'More elements at this distance')
      return
    }
    if (box.ref === undefined) return
    if (mods.shift || mods.toggle) {
      this.toggleInSelection(box.ref)
      return
    }
    const sel = this.S.sel
    if (sel?.kind === 'item' && sel.i === box.ref && box.kind !== 'group' && this.S.multi.size <= 1) {
      this.applySelection(false)
      return
    }
    this.set({ sel: { kind: 'item', i: box.ref }, multi: new Set([box.ref]) })
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

  /** onDragStart: what a drag on a box moves (the selection when the box is in it). */
  private onDragStart(box: Box, mods: PointerMods): string[] | null {
    if (!box.id || box.ref === undefined || box.kind === 'more') return null
    let keys: string[]
    if (this.S.multi.has(box.ref) && this.S.multi.size > 1) keys = this.selectedKeys()
    else {
      if (mods.shift || mods.toggle) {
        const multi = new Set(this.S.multi)
        multi.add(box.ref)
        this.set({ multi, sel: { kind: 'item', i: box.ref } })
      } else this.set({ sel: { kind: 'item', i: box.ref }, multi: new Set([box.ref]) })
      this.applySelection(false)
      keys = this.selectedKeys()
    }
    const movable = keys.filter((k) => {
      const b = this.boxByKey.get(k)
      return !!b && !!b.id && !b.locked
    })
    if (!movable.length) {
      toast(box.locked ? 'This card is locked: unlock it to move it (L).' : 'Nothing here can be moved.')
      return null
    }
    return movable
  }

  // ---------- editing positions ----------
  /** commitMoves saves where moved boxes are now (one undoable edit). */
  private commitMoves(moved: Box[], label: string) {
    const ed = this.editor
    if (!ed || !moved.length) return
    const pos = moved
      .filter((b) => b.id)
      .map((b) => ({ id: b.id!, p: relativePosition(ed, b), b }))
    this.store.edit(this.viewKey(), label, (v) => {
      for (const { id, p } of pos) {
        const cur = v.nodes[id]
        v.nodes[id] = cur?.locked ? { x: p.x, y: p.y, locked: true } : { x: p.x, y: p.y }
      }
      if (!v.expanded) v.expanded = this.openInside()
    })
    for (const { b } of pos) b.pinned = true
    this.emit()
  }

  private nudge(dx: number, dy: number) {
    const r = this.renderer
    if (!r) return
    const boxes = this.editor?.topmost(this.selectedKeys()) ?? []
    if (!boxes.length) return
    this.commitMoves(r.moveBoxes(boxes.map((b) => ({ key: b.key, dx, dy }))), 'Nudge')
  }

  /** arrangeable: the selected boxes that can move, at least n of them. */
  private arrangeable(n: number, what: string): Box[] | null {
    const boxes = this.editor?.topmost(this.selectedKeys()) ?? []
    if (boxes.length < n) {
      toast('Select at least ' + n + ' unlocked cards to ' + what + ' (Shift+click or Shift+drag).')
      return null
    }
    return boxes
  }

  align(mode: AlignMode) {
    const boxes = this.arrangeable(2, 'align them')
    if (!boxes || !this.renderer) return
    const to = alignRects(boxes, mode)
    const moved = this.renderer.moveBoxes(boxes.map((b, k) => ({ key: b.key, dx: to[k].x - b.x, dy: to[k].y - b.y })))
    this.commitMoves(moved, 'Align ' + mode)
  }

  distribute(axis: 'h' | 'v') {
    const boxes = this.arrangeable(3, 'distribute them')
    if (!boxes || !this.renderer) return
    const to = distributeRects(boxes, axis)
    const moved = this.renderer.moveBoxes(boxes.map((b, k) => ({ key: b.key, dx: to[k].x - b.x, dy: to[k].y - b.y })))
    this.commitMoves(moved, axis === 'h' ? 'Distribute horizontally' : 'Distribute vertically')
  }

  /** setLocked locks the selected cards where they are (or unlocks them). */
  setLocked(lock: boolean) {
    const ed = this.editor
    const boxes = this.selectedBoxes().filter((b) => b.id)
    if (!ed || !boxes.length) {
      toast('Select the cards to ' + (lock ? 'lock' : 'unlock') + ' first.')
      return
    }
    const changed = this.store.edit(this.viewKey(), lock ? 'Lock' : 'Unlock', (v) => {
      for (const b of boxes) {
        const cur = v.nodes[b.id!]
        if (lock) {
          const p = cur ?? relativePosition(ed, b)
          v.nodes[b.id!] = { x: p.x, y: p.y, locked: true }
        } else if (cur) v.nodes[b.id!] = { x: cur.x, y: cur.y }
      }
    })
    for (const b of boxes) {
      b.locked = lock
      if (lock) b.pinned = true
    }
    this.renderer?.refresh()
    this.emit()
    if (!changed) toast(lock ? 'Already locked.' : 'Nothing to unlock.')
  }

  toggleLock() {
    const boxes = this.selectedBoxes()
    this.setLocked(!boxes.length || !boxes.every((b) => b.locked))
  }

  /** autoLayout arranges every unlocked card of the view with its algorithm. */
  autoLayout() {
    const key = this.viewKey()
    const L = this.store.view(key)
    const manual = L.algorithm === 'manual'
    const changed = this.store.edit(key, 'Auto layout', (v) => {
      for (const id of Object.keys(v.nodes)) if (!v.nodes[id].locked) delete v.nodes[id]
      if (manual) v.algorithm = DEFAULT_ALGORITHM
    })
    if (manual) toast('Arranged with the Grouped layout (Manual never rearranges by itself).')
    if (changed) void this.rebuild({ animate: true })
    else toast('Already arranged automatically.')
  }

  /** setAlgorithm: lay the view out again with an algorithm (locked cards stay). */
  setAlgorithm(a: Algorithm) {
    const ed = this.editor
    const key = this.viewKey()
    if (this.store.view(key).algorithm === a) return
    this.store.edit(key, 'Layout: ' + ALGORITHM_INFO[a].label, (v) => {
      v.algorithm = a
      if (a === 'manual') {
        // Manual keeps every card where it is now
        if (ed)
          for (const b of this.view?.scene.boxes ?? []) {
            if (!b.id || v.nodes[b.id]) continue
            const p = relativePosition(ed, b)
            v.nodes[b.id] = { x: p.x, y: p.y }
          }
      } else for (const id of Object.keys(v.nodes)) if (!v.nodes[id].locked) delete v.nodes[id]
      if (!v.expanded) v.expanded = this.openInside()
    })
    if (a === 'manual') this.emit()
    else void this.rebuild({ animate: true })
  }

  setEdgeStyle(s: EdgeStyle) {
    const key = this.viewKey()
    if (this.store.view(key).edges === s) return
    this.store.edit(key, 'Lines: ' + EDGE_INFO[s].label, (v) => (v.edges = s))
    void this.rebuild({})
  }

  /** resetLayout: this view back to the automatic layout (locked cards too). */
  resetLayout() {
    const changed = this.store.edit(this.viewKey(), 'Reset layout', (v) => {
      v.nodes = {}
      v.algorithm = DEFAULT_ALGORITHM
    })
    if (changed) void this.rebuild({ animate: true })
    else toast('This view already has the automatic layout.')
  }

  undo() {
    const label = this.store.getSnapshot().undo
    const k = this.store.undo()
    if (k === null) {
      toast('Nothing to undo.')
      return
    }
    if (label) toast('Undone: ' + label, { ms: 1800 })
    void this.rebuild({})
  }
  redo() {
    const label = this.store.getSnapshot().redo
    const k = this.store.redo()
    if (k === null) {
      toast('Nothing to redo.')
      return
    }
    if (label) toast('Redone: ' + label, { ms: 1800 })
    void this.rebuild({})
  }

  // ---------- the layout document ----------
  /** exportLayout downloads the layout document (every view), or the current view only. */
  exportLayout(viewOnly = false) {
    const base = (this.D.raw.repository || 'repository').replace(/[^\w.-]+/g, '_')
    if (viewOnly) {
      const key = this.viewKey()
      download(
        base + '.' + key.replace(/[^\w.-]+/g, '_').slice(0, 80) + '.gitview-layout.json',
        this.store.serializeView(key),
        'application/json',
      )
    } else download(base + '.gitview-layout.json', this.store.serialize(), 'application/json')
  }

  importLayout(text: string) {
    try {
      const r = this.store.importText(text)
      toast('Imported ' + r.summary + '.', { tone: r.warnings.length ? 'warning' : undefined, ms: 5000 })
      if (r.warnings.length) setTimeout(() => toast(r.warnings[0], { tone: 'warning', ms: 7000 }), 400)
      void this.rebuild({ animate: true })
    } catch (e) {
      toast('Not imported: ' + (e instanceof LayoutError ? e.message : String(e)), { tone: 'error', ms: 7000 })
    }
  }

  restoreEmbedded() {
    this.store.restoreEmbedded()
    toast('The layout embedded in this page is restored.')
    void this.rebuild({ animate: true })
  }

  discardAll() {
    this.store.discardAll()
    toast('Every view is back to the automatic layout (Ctrl+Z undoes it).')
    void this.rebuild({ animate: true })
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
    this.pendingPush = false
    this.lastWritten = canonical(p)
    void this.store.ready.then(() => this.restoreNow(p))
  }
  private restoreNow(p: URLSearchParams) {
    const { D } = this
    const idx = (id: string | null) => (id ? (D.byId.get(id) ?? -1) : -1)
    const r = idx(p.get('scope'))
    const root = r >= 0 && expandable(D.T[r]) ? r : D.ROOT
    const o = p.get('open')
    const f = idx(p.get('focus'))
    const focus =
      f >= 0
        ? { i: f, up: clampDepth(p.get('up')), down: clampDepth(p.get('down')), refs: p.get('refs') === '1' }
        : null
    const s = idx(p.get('node'))
    const base: ExplorerState = { ...this.S, root, focus: null }
    const open = o
      ? new Set(
          o === '-'
            ? []
            : o
                .split('|')
                .map(idx)
                .filter((i) => i >= 0),
        )
      : (this.expandedOf(this.viewKey(base)) ?? this.initialOpen())
    open.add(root)
    this.set({
      root,
      open,
      focus,
      sel: s >= 0 ? { kind: 'item', i: s } : null,
      multi: s >= 0 ? new Set([s]) : EMPTY_MULTI,
    })
    void this.rebuild({ fit: true, revealHidden: true })
  }

  /** linkTo: a link to an element in this page. */
  linkTo(i: number): string {
    const base = location.href.replace(/#.*$/, '')
    return base + '#/architecture?node=' + encodeURIComponent(this.D.T[i].id)
  }
}
