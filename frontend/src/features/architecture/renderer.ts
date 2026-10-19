// Canvas renderer. Draws a scene (boxes and edges in world coordinates,
// computed by the views) with a pan/zoom camera, culling, level of detail
// and highlight/dim states, plus the minimap. It knows nothing about the
// dataset: views give it boxes and edges, it reports what the viewer does.
//
// Pointer gestures (mouse, pen and touch, through pointer events):
//   press and release without moving   click (twice quickly: double-click)
//   drag a card, or a group by its header   move it (with the selection)
//   drag empty space or a group's body   pan; Shift+drag: select a rectangle
//   two fingers                          pinch to zoom (and pan)
//   wheel                                zoom at the pointer
// A press becomes a drag beyond a few pixels (geometry.ts); a drag never
// clicks. Moving cards edits the scene in place (edit.ts) and redraws only
// what changed; the controller is told when the move ends.

import { placeLabel } from '../../lib/edgeLabel'
import { token } from '../../lib/tokens'
import { CARD_H, KIND_FONT } from './cards'
import { SceneEditor } from './edit'
import { fitZoom as fitZoomOf, isDrag } from './geometry'
import { bboxOf } from './route'
import { fit as fitText, font, HEAD_FONT, SUB_FONT, TITLE_FONT, textWidth } from './text'
import type { Box, EdgeRole, Highlight, Scene, SceneEdge } from './types'

// Token names of the canvas, mapped onto the app's design tokens.
const ALIAS: Record<string, string> = {
  accent: 'color-primary',
  text: 'fg',
  'text-2': 'fg-2',
  'text-3': 'fg-3',
  'border-strong': 'line-strong',
}
const C = (name: string) => token(ALIAS[name] ?? name) || '#888'

export interface FitOpts {
  minZoom?: number
  anchor?: string | null
  align?: 'left' | 'topleft' | 'center'
}

export interface SceneOpts {
  fit?: boolean
  fitOpts?: FitOpts
  animate?: boolean
  anchor?: string
  /** The editor of the scene (the controller's), so both see the same nesting. */
  editor?: SceneEditor
}

export interface PointerMods {
  shift: boolean
  /** Ctrl or Cmd: toggles the element in the selection. */
  toggle: boolean
}

export interface RendererEvents {
  box(box: Box, header: boolean, mods: PointerMods): void
  edge(e: SceneEdge, mods: PointerMods): void
  empty(mods: PointerMods): void
  dblbox(box: Box, header: boolean): void
  /** A drag starts on a box: the keys of the boxes to move, or null to pan instead. */
  dragStart(box: Box, mods: PointerMods): string[] | null
  /** The moved boxes (topmost), by dx, dy in total. */
  dragEnd(moved: Box[], dx: number, dy: number): void
  /** A rectangle was drawn over these boxes. */
  marquee(keys: string[], additive: boolean): void
  /** Shift+arrows on the canvas: move the selection. */
  nudge(dx: number, dy: number): void
}

export interface DrawOptions {
  /** Draw only the selection and what it relates to. */
  isolate: boolean
  /** Hide lines that carry only weak relations or type uses. */
  hideMinor: boolean
}

const EMPTY: Scene = { boxes: [], edges: [], bounds: { x: 0, y: 0, w: 1, h: 1 }, style: 'orthogonal', prefer: 'v' }
const DASH: Record<string, number[]> = { likely: [6, 4], possible: [2, 4], unresolved: [5, 4] }
const reduced = () => !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
const HEADER = 30
const DBL_MS = 380

interface Tween {
  t0: number
  ms: number
  from: Map<string, { x: number; y: number; w: number; h: number }>
  /** Boxes that were not drawn before: they fade in. */
  fresh: Set<string>
}
interface CamTween {
  t0: number
  ms: number
  a: { x: number; y: number; z: number }
  b: { x: number; y: number; z: number }
}

type Gesture =
  | {
      kind: 'press'
      id: number
      type: string
      x: number
      y: number
      box: Box | null
      header: boolean
      edge: SceneEdge | null
      mods: PointerMods
    }
  | { kind: 'pan'; id: number; x: number; y: number; cx: number; cy: number }
  | { kind: 'move'; id: number; keys: string[]; wx: number; wy: number; dx: number; dy: number; moved: Box[] }
  | { kind: 'marquee'; id: number; x0: number; y0: number; x1: number; y1: number; additive: boolean }
  | { kind: 'pinch'; d0: number; z0: number; wx: number; wy: number }

export class Renderer {
  private ctx: CanvasRenderingContext2D
  private mctx: CanvasRenderingContext2D
  private W = 0
  private H = 0
  private dpr = 1
  private scene: Scene = EMPTY
  private editor: SceneEditor = new SceneEditor(EMPTY)
  private byKey = new Map<string, Box>()
  private edgeByKey = new Map<string, SceneEdge>()
  private cam = { x: 0, y: 0, z: 1 }
  private hl: Highlight | null = null
  private sel = new Set<string>()
  private hover: string | null = null
  private hoverEdge: string | null = null
  private tween: Tween | null = null
  private camTween: CamTween | null = null
  private dirty = true
  private miniDirty = true
  private miniAt = 0
  private showMini = true
  private opts: DrawOptions = { isolate: false, hideMinor: false }
  private raf = 0
  private miniT: { s: number; ox: number; oy: number } | null = null
  private gesture: Gesture | null = null
  private pointers = new Map<number, { x: number; y: number; type: string }>()
  private lastClick: { t: number; x: number; y: number; key: string } | null = null
  private ro: ResizeObserver | null = null
  private cleanup: (() => void)[] = []
  readonly stats = { draws: 0, last: 0, max: 0, moves: 0, moveMs: 0, moveMax: 0 }

  private canvas: HTMLCanvasElement
  private mini: HTMLCanvasElement
  private events: RendererEvents

  constructor(canvas: HTMLCanvasElement, mini: HTMLCanvasElement, events: RendererEvents) {
    this.canvas = canvas
    this.mini = mini
    this.events = events
    this.ctx = canvas.getContext('2d')!
    this.mctx = mini.getContext('2d')!
    this.bind()
    this.resize()
  }

  destroy() {
    cancelAnimationFrame(this.raf)
    this.raf = 0
    this.ro?.disconnect()
    this.cleanup.forEach((f) => f())
    this.cleanup = []
  }

  // ---------- size ----------
  resize = () => {
    const r = this.canvas.getBoundingClientRect()
    this.dpr = window.devicePixelRatio || 1
    this.W = Math.max(1, r.width)
    this.H = Math.max(1, r.height)
    this.canvas.width = Math.round(this.W * this.dpr)
    this.canvas.height = Math.round(this.H * this.dpr)
    const m = this.mini.getBoundingClientRect()
    this.mini.width = Math.round(Math.max(1, m.width) * this.dpr)
    this.mini.height = Math.round(Math.max(1, m.height) * this.dpr)
    this.dirty = this.miniDirty = true
    this.schedule()
  }

  /** size of the drawing area in CSS pixels */
  size() {
    return { w: this.W, h: this.H }
  }

  // ---------- scene ----------
  /**
   * setScene replaces what is drawn. opts.animate tweens boxes that keep
   * their key from their previous place (never the ones the viewer placed);
   * opts.fit fits the camera; opts.anchor keeps that box at the same place
   * on screen.
   */
  setScene(next: Scene, opts: SceneOpts = {}) {
    if (this.gesture && this.gesture.kind === 'move') this.gesture = null
    const prev = this.byKey
    const anchorBox = opts.anchor ? prev.get(opts.anchor) : undefined
    const before = anchorBox ? this.screenOfBox(anchorBox) : null
    this.scene = next
    this.editor = opts.editor && opts.editor.scene === next ? opts.editor : new SceneEditor(next)
    this.byKey = new Map()
    this.edgeByKey = new Map()
    for (const b of next.boxes) {
      this.byKey.set(b.key, b)
      this.fitBox(b)
    }
    for (const e of next.edges) {
      this.edgeByKey.set(e.key, e)
      if (!e._bb) e._bb = bboxOf(e.pts)
    }
    for (const k of Array.from(this.sel)) if (!this.byKey.has(k) && !this.edgeByKey.has(k)) this.sel.delete(k)
    if (this.hover && !this.byKey.has(this.hover)) this.hover = null
    this.hoverEdge = null
    if (opts.animate && prev.size && !reduced()) {
      // boxes the viewer placed do not travel: they are where they were put
      const from = new Map<string, { x: number; y: number; w: number; h: number }>(),
        fresh = new Set<string>()
      for (const b of next.boxes) {
        const p = prev.get(b.key)
        if (!p) fresh.add(b.key)
        else if (!b.pinned && (p.x !== b.x || p.y !== b.y || p.w !== b.w || p.h !== b.h))
          from.set(b.key, { x: p.x, y: p.y, w: p.w, h: p.h })
      }
      this.tween = from.size || fresh.size ? { t0: performance.now(), ms: 260, from, fresh } : null
    } else this.tween = null
    if (opts.fit) this.fit(false, opts.fitOpts)
    else if (before && opts.anchor) {
      const b = this.byKey.get(opts.anchor)
      if (b) {
        this.cam.x = b.x + b.w / 2 - (before.x - this.W / 2) / this.cam.z
        this.cam.y = b.y + b.h / 2 - (before.y - this.H / 2) / this.cam.z
      }
    }
    this.dirty = this.miniDirty = true
    this.schedule()
  }

  /** fitBox fits the text of a box to its width. */
  private fitBox(b: Box) {
    const sf = SUB_FONT()
    const countW = b.count ? textWidth(b.count, sf) + 10 : 0
    if (b.kind === 'group') {
      const hf = HEAD_FONT()
      const lock = b.locked ? 16 : 0
      // header: ■ title  subtitle ……… count
      b._title = fitText(b.title, hf, Math.max(10, b.w - 36 - countW - lock))
      const rest = b.w - 36 - countW - lock - textWidth(b._title, hf) - 10
      b._sub = b.sub && rest > 40 ? fitText(b.sub, sf, rest) : ''
      return
    }
    const left = 14,
      right = 10 + (b.chev ? 16 : 0) + countW
    b._title = fitText(b.title, TITLE_FONT(), Math.max(10, b.w - left - right))
    b._sub = b.sub ? fitText(b.sub, sf, Math.max(10, b.w - left - 10)) : ''
    if (b.kindLabel) b._kind = fitText(b.kindLabel, KIND_FONT(), Math.max(10, b.w - left - 10 - (b.locked ? 16 : 0)))
    if (b.stats) b._stats = fitText(b.stats, sf, Math.max(10, b.w - left - 10))
    if (b.deps) b._deps = fitText(b.deps, sf, Math.max(10, b.w - left - 10))
  }

  private screenOfBox(b: Box) {
    return {
      x: (b.x + b.w / 2 - this.cam.x) * this.cam.z + this.W / 2,
      y: (b.y + b.h / 2 - this.cam.y) * this.cam.z + this.H / 2,
    }
  }

  box(key: string) {
    return this.byKey.get(key)
  }
  edge(key: string) {
    return this.edgeByKey.get(key)
  }
  sceneEditor() {
    return this.editor
  }

  // ---------- editing ----------
  /**
   * moveBoxes moves boxes by their own deltas (align, distribute, nudge)
   * and returns the boxes moved.
   */
  moveBoxes(list: { key: string; dx: number; dy: number }[]): Box[] {
    const out: Box[] = []
    for (const m of list) out.push(...this.editor.move([m.key], m.dx, m.dy))
    this.afterEdit()
    return out
  }
  private afterEdit() {
    for (const b of this.scene.boxes) if (b.kind === 'group') this.fitBox(b)
    this.editor.bounds()
    this.dirty = this.miniDirty = true
    this.schedule()
  }
  /** refresh redraws after the boxes' flags changed (locked, pinned). */
  refresh() {
    for (const b of this.scene.boxes) this.fitBox(b)
    this.dirty = this.miniDirty = true
    this.schedule()
  }

  // ---------- camera ----------
  private clampZ(z: number) {
    return Math.max(0.03, Math.min(z, 3))
  }
  private fitZoom(bb: { w: number; h: number }, pad = 48) {
    return this.clampZ(fitZoomOf(bb, { w: this.W, h: this.H - 30 }, pad, 1.25))
  }
  private animateTo(x: number, y: number, z: number, animate?: boolean) {
    if (animate === false || reduced()) {
      this.cam = { x, y, z }
      this.camTween = null
      this.dirty = this.miniDirty = true
      this.schedule()
      return
    }
    this.camTween = { t0: performance.now(), ms: 280, a: { ...this.cam }, b: { x, y, z } }
    this.schedule()
  }
  /**
   * fit shows the whole scene. With opts.minZoom, a scene too large to be
   * read at once is shown at that zoom instead, around opts.anchor (kept at
   * the left edge with align "left", centred otherwise).
   */
  fit(animate?: boolean, opts?: FitOpts) {
    const bb = this.scene.bounds
    const z = this.fitZoom(bb)
    const b = opts?.anchor ? this.byKey.get(opts.anchor) : undefined
    if (opts?.minZoom && z < opts.minZoom && b) {
      const m = opts.minZoom
      const x = opts.align === 'left' || opts.align === 'topleft' ? b.x - 60 + this.W / 2 / m : b.x + b.w / 2
      const y = opts.align === 'topleft' ? b.y - 70 + this.H / 2 / m : b.y + b.h / 2
      this.animateTo(x, y, m, animate)
      return
    }
    // the view's chips cover the top of the stage: leave them some room
    this.animateTo(bb.x + bb.w / 2, bb.y + bb.h / 2 - 12 / z, z, animate)
  }
  private boundsOf(keys: string[]) {
    const bs = keys.map((k) => this.byKey.get(k)).filter((b): b is Box => !!b)
    if (!bs.length) return null
    let x0 = Infinity,
      y0 = Infinity,
      x1 = -Infinity,
      y1 = -Infinity
    bs.forEach((b) => {
      x0 = Math.min(x0, b.x)
      y0 = Math.min(y0, b.y)
      x1 = Math.max(x1, b.x + b.w)
      y1 = Math.max(y1, b.y + b.h)
    })
    return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 }
  }
  /** reveal brings boxes into view, zooming in only as far as needed. */
  reveal(keys: string[], animate?: boolean) {
    const bb = this.boundsOf(keys)
    if (!bb) return
    let z = this.cam.z
    const need = this.fitZoom(bb, 80)
    if (z > need) z = need
    if (z < 0.55 && need >= 0.55) z = Math.min(need, 0.9)
    this.animateTo(bb.x + bb.w / 2, bb.y + bb.h / 2, z, animate)
  }
  /** fitKeys fits boxes in the view (the selection). */
  fitKeys(keys: string[], animate?: boolean) {
    const bb = this.boundsOf(keys)
    if (!bb) return
    this.animateTo(bb.x + bb.w / 2, bb.y + bb.h / 2, this.fitZoom(bb, 80), animate)
  }
  zoomBy(f: number, sx?: number, sy?: number) {
    const z = this.clampZ(this.cam.z * f)
    if (sx === undefined || sy === undefined) {
      sx = this.W / 2
      sy = this.H / 2
    }
    const wx = (sx - this.W / 2) / this.cam.z + this.cam.x,
      wy = (sy - this.H / 2) / this.cam.z + this.cam.y
    this.cam.z = z
    this.cam.x = wx - (sx - this.W / 2) / z
    this.cam.y = wy - (sy - this.H / 2) / z
    this.camTween = null
    this.dirty = this.miniDirty = true
    this.schedule()
  }
  pan(dx: number, dy: number) {
    this.cam.x += dx / this.cam.z
    this.cam.y += dy / this.cam.z
    this.camTween = null
    this.dirty = this.miniDirty = true
    this.schedule()
  }
  zoom() {
    return this.cam.z
  }
  /** screenRect: where a box is on the page (client coordinates), for tests and tooltips. */
  screenRect(key: string): { x: number; y: number; w: number; h: number } | null {
    const b = this.byKey.get(key)
    if (!b) return null
    const r = this.canvas.getBoundingClientRect()
    return {
      x: r.left + (b.x - this.cam.x) * this.cam.z + this.W / 2,
      y: r.top + (b.y - this.cam.y) * this.cam.z + this.H / 2,
      w: b.w * this.cam.z,
      h: b.h * this.cam.z,
    }
  }
  camera() {
    return { ...this.cam }
  }

  // ---------- state from the view ----------
  setHighlight(h: Highlight | null) {
    this.hl = h
    this.dirty = this.miniDirty = true
    this.schedule()
  }
  /** setSelected: the selected boxes (and/or the selected line). */
  setSelected(keys: Iterable<string>) {
    this.sel = new Set(keys)
    this.dirty = this.miniDirty = true
    this.schedule()
  }
  setMinimap(on: boolean) {
    this.showMini = on
    this.miniDirty = true
    this.schedule()
  }
  setOptions(o: Partial<DrawOptions>) {
    this.opts = { ...this.opts, ...o }
    this.dirty = this.miniDirty = true
    this.schedule()
  }
  /** redraw after a theme change. */
  redraw() {
    this.dirty = this.miniDirty = true
    this.schedule()
  }

  // ---------- visibility ----------
  /** isolated: hidden by "only related" (not drawn, not picked). */
  private hiddenBox(b: Box) {
    const hl = this.hl
    if (!this.opts.isolate || !hl) return false
    return !hl.boxes.has(b.key) && !(b.kind === 'group' && hl.keep.has(b.key)) && !this.sel.has(b.key)
  }
  private hiddenEdge(e: SceneEdge) {
    const role = this.hl ? this.hl.edges.get(e.key) : undefined
    if (this.opts.isolate && this.hl && !role && !this.sel.has(e.key)) return true
    if (this.opts.hideMinor && e.minor && !role && !this.sel.has(e.key) && this.hoverEdge !== e.key) return true
    return false
  }

  // ---------- drawing ----------
  private schedule() {
    if (!this.raf) this.raf = requestAnimationFrame(this.frame)
  }
  private frame = (now: number) => {
    this.raf = 0
    const ct = this.camTween
    if (ct) {
      const p = Math.max(0, Math.min(1, (now - ct.t0) / ct.ms)),
        e = 1 - Math.pow(1 - p, 3)
      this.cam.x = ct.a.x + (ct.b.x - ct.a.x) * e
      this.cam.y = ct.a.y + (ct.b.y - ct.a.y) * e
      this.cam.z = ct.a.z + (ct.b.z - ct.a.z) * e
      this.dirty = this.miniDirty = true
      if (p >= 1) this.camTween = null
    }
    let tp = 1
    // a frame's timestamp can precede the tween's start
    if (this.tween) {
      tp = Math.max(0, Math.min(1, (now - this.tween.t0) / this.tween.ms))
      this.dirty = true
    }
    if (this.dirty) {
      const t0 = performance.now()
      this.draw(tp)
      this.dirty = false
      this.stats.draws++
      this.stats.last = performance.now() - t0
      this.stats.max = Math.max(this.stats.max, this.stats.last)
    }
    // while cards move, the minimap follows a few times a second
    const moving = this.gesture?.kind === 'move'
    if (this.miniDirty && this.showMini && (!moving || now - this.miniAt > 120)) {
      this.drawMini()
      this.miniDirty = false
      this.miniAt = now
    }
    if (this.tween && tp >= 1) this.tween = null
    if (this.camTween || this.tween || (moving && this.miniDirty)) this.schedule()
  }

  private geom(b: Box, tp: number) {
    if (!this.tween || tp >= 1) return b
    const f = this.tween.from.get(b.key)
    const e = 1 - Math.pow(1 - tp, 3)
    if (!f) return b
    return { x: f.x + (b.x - f.x) * e, y: f.y + (b.y - f.y) * e, w: f.w + (b.w - f.w) * e, h: f.h + (b.h - f.h) * e }
  }
  private rr(c: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number) {
    w = Math.max(w, 1)
    h = Math.max(h, 1)
    r = Math.min(r, w / 2, h / 2)
    c.beginPath()
    c.moveTo(x + r, y)
    c.arcTo(x + w, y, x + w, y + h, r)
    c.arcTo(x + w, y + h, x, y + h, r)
    c.arcTo(x, y + h, x, y, r)
    c.arcTo(x, y, x + w, y, r)
    c.closePath()
  }
  private boxAlpha(b: Box) {
    const hl = this.hl
    if (!hl || this.sel.has(b.key)) return 1
    if (hl.boxes.has(b.key)) return 1
    if (b.kind === 'group' && hl.keep.has(b.key)) return 1
    return Number(C('dim-alpha')) || 0.16
  }

  private draw(tp: number) {
    const { ctx, W, H, dpr } = this
    const z = this.cam.z
    ctx.setTransform(1, 0, 0, 1, 0, 0)
    ctx.fillStyle = C('canvas')
    ctx.fillRect(0, 0, this.canvas.width, this.canvas.height)
    ctx.setTransform(dpr * z, 0, 0, dpr * z, dpr * (W / 2 - this.cam.x * z), dpr * (H / 2 - this.cam.y * z))
    const vx0 = this.cam.x - W / 2 / z,
      vy0 = this.cam.y - H / 2 / z,
      vx1 = this.cam.x + W / 2 / z,
      vy1 = this.cam.y + H / 2 / z
    const inView = (x: number, y: number, w: number, h: number) => x + w >= vx0 && x <= vx1 && y + h >= vy0 && y <= vy1
    const px = 1 / z // one screen pixel in world units
    const showText = 12.5 * z >= 5.5,
      showSub = 11 * z >= 7.5
    const settled = !this.tween || tp >= 1
    const scene = this.scene
    const late: [string, { x: number; y: number; w: number; h: number }, number, number, boolean, number][] = []
    // what edge labels must not cover: the cards and the groups' headers in view
    const covered: number[][] = []

    // groups, outer first
    for (const b of scene.boxes) {
      if (b.kind !== 'group') continue
      const g = this.geom(b, tp)
      if (!inView(g.x, g.y, g.w, g.h) || this.hiddenBox(b)) continue
      const sel = this.sel.has(b.key)
      ctx.globalAlpha = this.boxAlpha(b)
      this.rr(ctx, g.x, g.y, g.w, g.h, 8)
      ctx.fillStyle = C('group-fill')
      ctx.fill()
      ctx.lineWidth = (sel ? 2 : 1) * px
      ctx.strokeStyle = sel ? C('accent') : b.scope ? C('border-strong') : C('group-border')
      if (b.muted) ctx.setLineDash([4 * px, 3 * px])
      ctx.stroke()
      ctx.setLineDash([])
      // header
      ctx.save()
      this.rr(ctx, g.x, g.y, g.w, g.h, 8)
      ctx.clip()
      ctx.fillStyle = C('group-head')
      ctx.fillRect(g.x, g.y, g.w, HEADER)
      ctx.restore()
      covered.push([g.x, g.y, g.x + g.w, g.y + HEADER])
      ctx.fillStyle = C(b.color)
      ctx.fillRect(g.x + 10, g.y + 11, 8, 8)
      if (showText && settled) {
        ctx.font = HEAD_FONT()
        ctx.textBaseline = 'middle'
        ctx.fillStyle = C('text')
        ctx.fillText(b._title ?? b.title, g.x + 24, g.y + 15.5)
        if (showSub && b._sub) {
          const tw = textWidth(b._title ?? '', HEAD_FONT())
          ctx.font = SUB_FONT()
          ctx.fillStyle = C('text-3')
          ctx.fillText(b._sub, g.x + 24 + tw + 10, g.y + 15.5)
        }
        if (b.count) {
          ctx.font = SUB_FONT()
          ctx.fillStyle = C('text-3')
          ctx.textAlign = 'right'
          ctx.fillText(b.count, g.x + g.w - 12, g.y + 15.5)
          ctx.textAlign = 'left'
        }
        if (b.locked && showSub) this.lockIcon(g.x + g.w - 18, g.y + 9, px)
      }
      if (!showText && settled) late.push([b.title, g, 11, 600, true, g.w * g.h * 4])
      if (b.key === this.hover && !sel) {
        this.rr(ctx, g.x, g.y, g.w, g.h, 8)
        ctx.lineWidth = px
        ctx.strokeStyle = C('edge-strong')
        ctx.stroke()
      }
    }
    ctx.globalAlpha = 1

    // edges
    const edgeAlpha = this.tween && tp < 1 ? Math.max(0, (tp - 0.6) / 0.4) : 1
    const hovering = (e: SceneEdge) => {
      const h = this.hover
      return this.hoverEdge === e.key || (!!h && (e.s === h || e.t === h || !!e.units?.has(h)))
    }
    if (edgeAlpha > 0) {
      const pass = (strongPass: boolean) => {
        for (const e of scene.edges) {
          const role = this.hl ? this.hl.edges.get(e.key) : undefined
          const isHover = hovering(e)
          const isSel = this.sel.has(e.key)
          // branches (inside a box, towards a bundled trunk) only show on demand
          if (e.branch && !role && !isHover && !isSel) continue
          const strong = !!role || isSel || isHover
          if (strongPass !== strong) continue
          if (this.hiddenEdge(e)) continue
          const bb = e._bb!
          if (!inView(bb[0], bb[1], bb[2] - bb[0] + 1, bb[3] - bb[1] + 1)) continue
          this.drawEdge(e, role, isHover, px, edgeAlpha)
        }
      }
      pass(false)
      pass(true)
    }

    // nodes
    for (const b of scene.boxes) {
      if (b.kind === 'group') continue
      const g = this.geom(b, tp)
      if (!inView(g.x, g.y, g.w, g.h) || this.hiddenBox(b)) continue
      ctx.globalAlpha = this.boxAlpha(b) * (this.tween && this.tween.fresh.has(b.key) && tp < 1 ? tp : 1)
      this.drawNode(b, g, px, showText, showSub)
      covered.push([g.x, g.y, g.x + g.w, g.y + g.h])
      if (!showText && settled) late.push([b.title, g, 10, 500, false, g.w * g.h])
    }
    // zoomed-out names last, larger boxes first, never over each other
    if (late.length) {
      const taken: number[][] = []
      late.sort((a, b) => b[5] - a[5]).forEach((l) => this.zoomedOutLabel(l[0], l[1], l[2], l[3], l[4], taken))
    }
    ctx.globalAlpha = 1

    // labels of the strong edges, on top, never over each other
    if (edgeAlpha >= 1 && showText) {
      const taken: number[][] = []
      const want = (e: SceneEdge) => {
        const role = this.hl ? this.hl.edges.get(e.key) : undefined
        if (!e.label || e.branch || this.hiddenEdge(e)) return 0
        if (this.sel.has(e.key) || this.hoverEdge === e.key) return 3
        const h = this.hover
        if (h && (e.s === h || e.t === h || e.units?.has(h))) return 2
        return (role === 'out' || role === 'in') && (e.n > 1 || Object.keys(e.agg.types).length > 1) ? 1 : 0
      }
      scene.edges
        .map((e) => [want(e), e] as const)
        .filter((x) => x[0])
        .sort((a, b) => b[0] - a[0])
        .forEach(([, e]) => this.drawEdgeLabel(e, px, covered, taken))
    }

    // the rectangle being drawn
    const g = this.gesture
    if (g && g.kind === 'marquee') {
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      const x = Math.min(g.x0, g.x1),
        y = Math.min(g.y0, g.y1)
      ctx.fillStyle = C('accent-weak')
      ctx.fillRect(x, y, Math.abs(g.x1 - g.x0), Math.abs(g.y1 - g.y0))
      ctx.strokeStyle = C('accent')
      ctx.lineWidth = 1
      ctx.setLineDash([4, 3])
      ctx.strokeRect(x + 0.5, y + 0.5, Math.abs(g.x1 - g.x0), Math.abs(g.y1 - g.y0))
      ctx.setLineDash([])
    }
  }

  /**
   * zoomedOutLabel: when the scene is too small for its text, boxes that are
   * still large on screen keep their name at a fixed screen size (semantic
   * zoom), a group's as a tag on its corner.
   */
  private zoomedOutLabel(
    text: string,
    g: { x: number; y: number; w: number; h: number },
    screenPx: number,
    weight: number,
    tag: boolean,
    taken: number[][],
  ) {
    const ctx = this.ctx
    const z = this.cam.z
    if (g.w * z < (tag ? 70 : 46) || g.h * z < 9) return
    const fs = Math.max(1, Math.round(screenPx / z))
    const f = font(weight, fs)
    const pad = 6 / z
    const t = fitText(text, f, g.w - pad * 3)
    if (!t || t === '…') return
    const w = textWidth(t, f) + pad * 2,
      hh = (screenPx + 8) / z
    const y0 = tag ? g.y + pad / 2 : g.y + g.h / 2 - hh / 2
    const r = [g.x, y0, g.x + w + pad, y0 + hh]
    if (taken.some((q) => r[0] < q[2] && r[2] > q[0] && r[1] < q[3] && r[3] > q[1])) return
    taken.push(r)
    ctx.font = f
    ctx.textBaseline = 'middle'
    if (tag) {
      this.rr(ctx, g.x + pad / 2, g.y + pad / 2, w, hh, 4 / z)
      ctx.fillStyle = C('surface-1')
      ctx.fill()
      ctx.lineWidth = 1 / z
      ctx.strokeStyle = C('border-strong')
      ctx.stroke()
      ctx.fillStyle = C('text')
      ctx.fillText(t, g.x + pad * 1.5, g.y + pad / 2 + hh / 2)
    } else {
      ctx.fillStyle = C('text-2')
      ctx.fillText(t, g.x + pad * 1.4, g.y + g.h / 2)
    }
  }

  /** lockIcon: a small padlock (top-left corner at x, y; 10×12). */
  private lockIcon(x: number, y: number, px: number) {
    const ctx = this.ctx
    ctx.strokeStyle = C('text-3')
    ctx.fillStyle = C('text-3')
    ctx.lineWidth = 1.3 * px
    ctx.beginPath()
    ctx.moveTo(x + 2.5, y + 5)
    ctx.lineTo(x + 2.5, y + 3.2)
    ctx.arc(x + 5, y + 3.2, 2.5, Math.PI, 0)
    ctx.lineTo(x + 7.5, y + 5)
    ctx.stroke()
    ctx.fillRect(x + 1, y + 5, 8, 6)
  }

  private drawNode(
    b: Box,
    g: { x: number; y: number; w: number; h: number },
    px: number,
    showText: boolean,
    showSub: boolean,
  ) {
    const ctx = this.ctx
    const role = this.hl?.boxes.get(b.key)
    const sel = this.sel.has(b.key)
    const ghost = b.kind === 'context' || b.kind === 'more'
    this.rr(ctx, g.x, g.y, g.w, g.h, 6)
    ctx.fillStyle = ghost ? C('context-fill') : b.key === this.hover ? C('node-fill-hover') : C('node-fill')
    ctx.fill()
    ctx.lineWidth = (sel ? 2 : role ? 1.5 : 1) * px
    ctx.strokeStyle = sel
      ? C('accent')
      : role === 'out' || role === 'both'
        ? C('hl-out')
        : role === 'in'
          ? C('hl-in')
          : b.key === this.hover
            ? C('edge-strong')
            : C('node-border')
    if (role === 'sel') ctx.strokeStyle = C('accent')
    if (ghost || b.muted) ctx.setLineDash([4 * px, 3 * px])
    ctx.stroke()
    ctx.setLineDash([])
    // category bar
    if (b.kind !== 'more') {
      ctx.save()
      this.rr(ctx, g.x, g.y, g.w, g.h, 6)
      ctx.clip()
      ctx.fillStyle = C(b.color)
      ctx.globalAlpha *= b.kind === 'context' ? 0.5 : 1
      ctx.fillRect(g.x, g.y, 3, g.h)
      ctx.restore()
    }
    if (!showText) return
    const tx = g.x + 12
    ctx.textBaseline = 'middle'
    if (b.kindLabel && showSub && g.h >= CARD_H.symbol - 1) {
      // KIND / name / statistics / dependencies
      ctx.font = KIND_FONT()
      ctx.fillStyle = C('text-3')
      ctx.fillText(b._kind ?? b.kindLabel, tx, g.y + 14)
      if (b.locked) this.lockIcon(g.x + g.w - 18, g.y + 8, px)
      ctx.font = TITLE_FONT()
      ctx.fillStyle = ghost ? C('text-2') : C('text')
      ctx.fillText(b._title ?? b.title, tx, g.y + 30)
      ctx.font = SUB_FONT()
      ctx.fillStyle = C('text-2')
      if (b._stats) ctx.fillText(b._stats, tx, g.y + 45.5)
      if (b._deps && g.h >= CARD_H.container - 1) {
        ctx.fillStyle = C('text-3')
        ctx.fillText(b._deps, tx, g.y + 59.5)
      }
      if (b.chev) this.chevron(b.chev, g.x + g.w - 14, g.y + 30, px)
      return
    }
    const twoLines = showSub && !!b._sub && g.h >= 34 && !b.kindLabel
    ctx.font = TITLE_FONT()
    ctx.fillStyle = ghost ? C('text-2') : C('text')
    ctx.fillText(b._title ?? b.title, tx, twoLines ? g.y + g.h / 2 - 7 : g.y + g.h / 2 + 0.5)
    if (twoLines) {
      ctx.font = SUB_FONT()
      ctx.fillStyle = C('text-3')
      ctx.fillText(b._sub!, tx, g.y + g.h / 2 + 8)
    }
    let rx = g.x + g.w - 10
    if (b.chev) {
      this.chevron(b.chev, rx - 4, g.y + g.h / 2, px)
      rx -= 16
    }
    if (b.count) {
      ctx.font = SUB_FONT()
      ctx.fillStyle = C('text-3')
      ctx.textAlign = 'right'
      ctx.fillText(b.count, rx, twoLines ? g.y + g.h / 2 - 7 : g.y + g.h / 2 + 0.5)
      ctx.textAlign = 'left'
    }
  }

  private chevron(dir: 'open' | 'closed', cx: number, cy: number, px: number) {
    const ctx = this.ctx
    ctx.strokeStyle = C('text-3')
    ctx.lineWidth = 1.4 * px
    ctx.beginPath()
    if (dir === 'closed') {
      ctx.moveTo(cx - 2, cy - 4)
      ctx.lineTo(cx + 2, cy)
      ctx.lineTo(cx - 2, cy + 4)
    } else {
      ctx.moveTo(cx - 4, cy - 2)
      ctx.lineTo(cx, cy + 2)
      ctx.lineTo(cx + 4, cy - 2)
    }
    ctx.stroke()
  }

  private drawEdge(e: SceneEdge, role: EdgeRole | undefined, isHover: boolean, px: number, alpha: number) {
    const ctx = this.ctx
    const p = e.pts
    if (p.length < 4) return
    const far = role === 'out2' || role === 'in2'
    const isSel = this.sel.has(e.key)
    const color =
      role === 'out' || role === 'out2'
        ? C('hl-out')
        : role === 'in' || role === 'in2'
          ? C('hl-in')
          : isSel
            ? C('accent')
            : e.conf === 'unresolved'
              ? C('c-unresolved')
              : isHover
                ? C('edge-strong')
                : C('edge')
    const width =
      (1 + Math.min(Math.log2(1 + (e.n || 1)), 3) * 0.45 + ((role && !far) || isHover || isSel ? 0.7 : 0)) * px
    ctx.globalAlpha =
      alpha * (this.hl && !role && !isSel ? Number(C('dim-alpha')) || 0.16 : far && !isHover ? 0.45 : 1)
    ctx.strokeStyle = color
    ctx.fillStyle = color
    ctx.lineWidth = width
    const dash = DASH[e.conf]
    ctx.setLineDash(dash ? dash.map((d) => d * px) : [])
    // the arrow points along the end of the line (over a few pixels: curves are sampled)
    const n = p.length
    const ex = p[n - 2],
      ey = p[n - 1]
    let k = n - 4,
      sx = p[k],
      sy = p[k + 1]
    while (k > 0 && Math.hypot(ex - sx, ey - sy) < 8 * px) {
      k -= 2
      sx = p[k]
      sy = p[k + 1]
    }
    const len = Math.hypot(ex - sx, ey - sy) || 1
    const ah = Math.min(8 * px, len * 0.6),
      ux = (ex - sx) / len,
      uy = (ey - sy) / len
    ctx.beginPath()
    ctx.moveTo(p[0], p[1])
    if (this.scene.style === 'orthogonal') {
      const r = 6
      for (let j = 2; j < n - 2; j += 2) ctx.arcTo(p[j], p[j + 1], p[j + 2], p[j + 3], r)
    } else for (let j = 2; j < n - 2; j += 2) ctx.lineTo(p[j], p[j + 1])
    // stop the line at the arrow's base
    ctx.lineTo(ex - ux * ah * 0.8, ey - uy * ah * 0.8)
    ctx.stroke()
    ctx.setLineDash([])
    ctx.beginPath()
    ctx.moveTo(ex, ey)
    ctx.lineTo(ex - ux * ah - uy * ah * 0.45, ey - uy * ah + ux * ah * 0.45)
    ctx.lineTo(ex - ux * ah + uy * ah * 0.45, ey - uy * ah - ux * ah * 0.45)
    ctx.closePath()
    ctx.fill()
    ctx.globalAlpha = 1
  }

  private drawEdgeLabel(e: SceneEdge, px: number, covered: number[][], taken: number[][]) {
    const ctx = this.ctx
    const f = font(500, 10.5)
    ctx.font = f
    const label = e.label ?? ''
    const w = textWidth(label, f) + 10
    const at = placeLabel(e.pts, w, 16, covered, taken)
    if (!at) return
    const [mx, my] = at
    this.rr(ctx, mx - w / 2, my - 8, w, 16, 8)
    ctx.fillStyle = C('surface-1')
    ctx.fill()
    ctx.lineWidth = px
    ctx.strokeStyle = C('border-strong')
    ctx.stroke()
    ctx.fillStyle = C('text-2')
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    ctx.fillText(label, mx, my + 0.5)
    ctx.textAlign = 'left'
  }

  // ---------- minimap ----------
  private drawMini() {
    const { mctx, mini, scene, dpr, cam, W, H } = this
    const mw = mini.width,
      mh = mini.height
    mctx.setTransform(1, 0, 0, 1, 0, 0)
    mctx.fillStyle = C('surface-1')
    mctx.fillRect(0, 0, mw, mh)
    const bb = scene.bounds
    if (!scene.boxes.length) return
    const s = Math.min((mw - 12 * dpr) / Math.max(bb.w, 1), (mh - 12 * dpr) / Math.max(bb.h, 1))
    const ox = (mw - bb.w * s) / 2 - bb.x * s,
      oy = (mh - bb.h * s) / 2 - bb.y * s
    this.miniT = { s, ox, oy }
    for (const b of scene.boxes) {
      if (b.kind !== 'group' || this.hiddenBox(b)) continue
      mctx.strokeStyle = this.sel.has(b.key) ? C('accent') : C('group-border')
      mctx.lineWidth = dpr
      mctx.strokeRect(b.x * s + ox, b.y * s + oy, Math.max(1, b.w * s), Math.max(1, b.h * s))
    }
    for (const b of scene.boxes) {
      if (b.kind === 'group' || this.hiddenBox(b)) continue
      mctx.globalAlpha = this.hl && !this.hl.boxes.has(b.key) && !this.sel.has(b.key) ? 0.25 : 0.85
      mctx.fillStyle = this.sel.has(b.key) ? C('accent') : C(b.color)
      mctx.fillRect(b.x * s + ox, b.y * s + oy, Math.max(1.5, b.w * s), Math.max(1.5, b.h * s))
    }
    mctx.globalAlpha = 1
    const vx = (cam.x - W / 2 / cam.z) * s + ox,
      vy = (cam.y - H / 2 / cam.z) * s + oy
    mctx.fillStyle = C('accent-weak')
    mctx.fillRect(vx, vy, (W / cam.z) * s, (H / cam.z) * s)
    mctx.strokeStyle = C('accent')
    mctx.lineWidth = 1.5 * dpr
    mctx.strokeRect(vx, vy, (W / cam.z) * s, (H / cam.z) * s)
  }
  private miniMove(ev: PointerEvent) {
    if (!this.miniT) return
    const r = this.mini.getBoundingClientRect()
    const mx = (ev.clientX - r.left) * this.dpr,
      my = (ev.clientY - r.top) * this.dpr
    this.cam.x = (mx - this.miniT.ox) / this.miniT.s
    this.cam.y = (my - this.miniT.oy) / this.miniT.s
    this.camTween = null
    this.dirty = this.miniDirty = true
    this.schedule()
  }

  // ---------- hit testing ----------
  private toWorld(sx: number, sy: number) {
    return { x: (sx - this.W / 2) / this.cam.z + this.cam.x, y: (sy - this.H / 2) / this.cam.z + this.cam.y }
  }
  private boxAt(sx: number, sy: number): Box | null {
    const p = this.toWorld(sx, sy)
    const boxes = this.scene.boxes
    for (let k = boxes.length - 1; k >= 0; k--) {
      const b = boxes[k]
      if (b.kind === 'group' || this.hiddenBox(b)) continue
      if (p.x >= b.x && p.x <= b.x + b.w && p.y >= b.y && p.y <= b.y + b.h) return b
    }
    let best: Box | null = null
    for (const b of boxes) {
      if (b.kind !== 'group' || this.hiddenBox(b)) continue
      if (p.x >= b.x && p.x <= b.x + b.w && p.y >= b.y && p.y <= b.y + b.h && (!best || b.depth >= best.depth)) best = b
    }
    return best
  }
  /** inHeader: on a group's header, which keeps a grabbable height when zoomed out (its name tag). */
  private inHeader(b: Box, sx: number, sy: number) {
    const p = this.toWorld(sx, sy)
    return b.kind === 'group' && p.y - b.y <= Math.min(Math.max(HEADER, 22 / this.cam.z), b.h / 2)
  }
  private edgeAt(sx: number, sy: number): SceneEdge | null {
    const p = this.toWorld(sx, sy),
      tol = 5 / this.cam.z
    let best: SceneEdge | null = null,
      bd = tol
    for (const e of this.scene.edges) {
      // branches are hidden until asked for: they cannot be picked
      if (e.branch && !(this.hl?.edges.has(e.key) || this.sel.has(e.key))) continue
      if (this.hiddenEdge(e)) continue
      const bb = e._bb!
      if (p.x < bb[0] - tol || p.x > bb[2] + tol || p.y < bb[1] - tol || p.y > bb[3] + tol) continue
      for (let k = 0; k < e.pts.length - 2; k += 2) {
        const d = segDist(p.x, p.y, e.pts[k], e.pts[k + 1], e.pts[k + 2], e.pts[k + 3])
        if (d < bd) {
          bd = d
          best = e
        }
      }
    }
    return best
  }
  /** boxesIn: the topmost boxes drawn entirely inside a screen rectangle. */
  private boxesIn(x0: number, y0: number, x1: number, y1: number): string[] {
    const a = this.toWorld(Math.min(x0, x1), Math.min(y0, y1)),
      b = this.toWorld(Math.max(x0, x1), Math.max(y0, y1))
    const inside = this.scene.boxes.filter(
      (q) => !this.hiddenBox(q) && q.x >= a.x && q.y >= a.y && q.x + q.w <= b.x && q.y + q.h <= b.y,
    )
    const set = new Set(inside.map((q) => q.key))
    const nested = (q: Box) => {
      for (let p = q.parent; p; p = this.byKey.get(p)?.parent) if (set.has(p)) return true
      return false
    }
    return inside.filter((q) => !nested(q)).map((q) => q.key)
  }

  // ---------- input ----------
  private local(ev: { clientX: number; clientY: number }) {
    const r = this.canvas.getBoundingClientRect()
    return [ev.clientX - r.left, ev.clientY - r.top] as const
  }
  private modsOf(ev: { shiftKey: boolean; ctrlKey: boolean; metaKey: boolean }): PointerMods {
    return { shift: ev.shiftKey, toggle: ev.ctrlKey || ev.metaKey }
  }
  private setCursor(c: string) {
    if (this.canvas.style.cursor !== c) this.canvas.style.cursor = c
  }
  private hoverAt(sx: number, sy: number) {
    const b = this.boxAt(sx, sy)
    const e = b && b.kind !== 'group' ? null : this.edgeAt(sx, sy)
    const hk = b && b.kind !== 'group' ? b.key : b && this.inHeader(b, sx, sy) ? b.key : null
    const ek = e ? e.key : null
    if (hk !== this.hover || ek !== this.hoverEdge) {
      this.hover = hk
      this.hoverEdge = hk ? null : ek
      const he = this.hoverEdge ? this.edgeByKey.get(this.hoverEdge) : undefined
      const hb = this.hover ? this.byKey.get(this.hover) : undefined
      this.canvas.title = he?.label ? he.label : hb?.tip ? hb.tip + (hb.locked ? ' · locked' : '') : ''
      this.dirty = true
      this.schedule()
    }
    this.setCursor(this.hover || this.hoverEdge ? 'pointer' : '')
  }
  private startPinch() {
    const ps = Array.from(this.pointers.values())
    if (ps.length < 2) return
    const [a, b] = ps
    const r = this.canvas.getBoundingClientRect()
    const mx = (a.x + b.x) / 2 - r.left,
      my = (a.y + b.y) / 2 - r.top
    const w = this.toWorld(mx, my)
    this.gesture = { kind: 'pinch', d0: Math.max(10, Math.hypot(a.x - b.x, a.y - b.y)), z0: this.cam.z, wx: w.x, wy: w.y }
  }
  private endMove(g: Extract<Gesture, { kind: 'move' }>) {
    this.afterEdit()
    if (g.moved.length) this.events.dragEnd(g.moved, g.dx, g.dy)
  }

  private bind() {
    const cv = this.canvas
    const on = <K extends keyof HTMLElementEventMap>(
      el: HTMLElement,
      type: K,
      fn: (ev: HTMLElementEventMap[K]) => void,
      opts?: AddEventListenerOptions,
    ) => {
      el.addEventListener(type, fn, opts)
      this.cleanup.push(() => el.removeEventListener(type, fn, opts))
    }
    on(cv, 'pointerdown', (ev) => {
      const mouse = ev.pointerType === 'mouse'
      if (mouse && ev.button !== 0 && ev.button !== 1) return
      cv.focus({ preventScroll: true })
      // a mouse is pressed once at a time: a pointer left over (a release missed) is dropped
      for (const [id, p] of this.pointers) if (p.type === ev.pointerType && ev.pointerType !== 'touch') this.pointers.delete(id)
      this.pointers.set(ev.pointerId, { x: ev.clientX, y: ev.clientY, type: ev.pointerType })
      try {
        cv.setPointerCapture(ev.pointerId)
      } catch {
        /* the pointer may already be gone */
      }
      if (this.pointers.size >= 2) {
        // a second finger: whatever the first one was doing becomes a pinch
        const g = this.gesture
        if (g && g.kind === 'move') this.endMove(g)
        if (g && g.kind === 'marquee') this.dirty = true
        this.startPinch()
        return
      }
      this.camTween = null
      const [sx, sy] = this.local(ev)
      if (mouse && ev.button === 1) {
        ev.preventDefault()
        this.gesture = { kind: 'pan', id: ev.pointerId, x: ev.clientX, y: ev.clientY, cx: this.cam.x, cy: this.cam.y }
        return
      }
      const box = this.boxAt(sx, sy)
      const header = !!box && this.inHeader(box, sx, sy)
      const edge = box && box.kind !== 'group' ? null : this.edgeAt(sx, sy)
      this.gesture = {
        kind: 'press',
        id: ev.pointerId,
        type: ev.pointerType,
        x: ev.clientX,
        y: ev.clientY,
        box,
        header,
        edge,
        mods: this.modsOf(ev),
      }
    })
    on(cv, 'pointermove', (ev) => {
      const pt = this.pointers.get(ev.pointerId)
      if (pt) {
        pt.x = ev.clientX
        pt.y = ev.clientY
      }
      const g = this.gesture
      if (!g) {
        const [sx, sy] = this.local(ev)
        this.hoverAt(sx, sy)
        return
      }
      if (g.kind === 'pinch') {
        const ps = Array.from(this.pointers.values())
        if (ps.length < 2) return
        const [a, b] = ps
        const d = Math.max(10, Math.hypot(a.x - b.x, a.y - b.y))
        const r = this.canvas.getBoundingClientRect()
        const mx = (a.x + b.x) / 2 - r.left,
          my = (a.y + b.y) / 2 - r.top
        const z = this.clampZ((g.z0 * d) / g.d0)
        // the world point under the fingers' middle stays under it
        this.cam.z = z
        this.cam.x = g.wx - (mx - this.W / 2) / z
        this.cam.y = g.wy - (my - this.H / 2) / z
        this.dirty = this.miniDirty = true
        this.schedule()
        return
      }
      if (g.id !== ev.pointerId) return
      if (g.kind === 'press') {
        const dx = ev.clientX - g.x,
          dy = ev.clientY - g.y
        if (!isDrag(dx, dy, g.type)) return
        const b = g.box
        const draggable = !!b && (b.kind !== 'group' || g.header)
        if (draggable && b) {
          const keys = this.events.dragStart(b, g.mods)
          if (keys && keys.length) {
            const w = this.toWorld(...this.local({ clientX: g.x, clientY: g.y }))
            this.gesture = { kind: 'move', id: g.id, keys, wx: w.x, wy: w.y, dx: 0, dy: 0, moved: [] }
            this.hover = null
            this.hoverEdge = null
            this.setCursor('grabbing')
          }
        }
        if (this.gesture === g) {
          if (g.mods.shift && !draggable) {
            const [x0, y0] = this.local({ clientX: g.x, clientY: g.y })
            this.gesture = { kind: 'marquee', id: g.id, x0, y0, x1: x0, y1: y0, additive: true }
            this.setCursor('crosshair')
          } else {
            this.gesture = { kind: 'pan', id: g.id, x: g.x, y: g.y, cx: this.cam.x, cy: this.cam.y }
            this.setCursor('grabbing')
          }
        }
      }
      const cur = this.gesture
      if (!cur) return
      if (cur.kind === 'pan') {
        this.cam.x = cur.cx - (ev.clientX - cur.x) / this.cam.z
        this.cam.y = cur.cy - (ev.clientY - cur.y) / this.cam.z
        this.camTween = null
        this.dirty = this.miniDirty = true
        this.schedule()
      } else if (cur.kind === 'move') {
        const [sx, sy] = this.local(ev)
        const w = this.toWorld(sx, sy)
        const ddx = w.x - cur.wx,
          ddy = w.y - cur.wy
        if (!ddx && !ddy) return
        const t0 = performance.now()
        const moved = this.editor.move(cur.keys, ddx, ddy)
        const ms = performance.now() - t0
        this.stats.moves++
        this.stats.moveMs += ms
        this.stats.moveMax = Math.max(this.stats.moveMax, ms)
        if (moved.length) cur.moved = moved
        cur.wx = w.x
        cur.wy = w.y
        cur.dx += ddx
        cur.dy += ddy
        this.dirty = this.miniDirty = true
        this.schedule()
      } else if (cur.kind === 'marquee') {
        const [sx, sy] = this.local(ev)
        cur.x1 = sx
        cur.y1 = sy
        this.dirty = true
        this.schedule()
      }
    })
    const release = (ev: PointerEvent, cancelled: boolean) => {
      this.pointers.delete(ev.pointerId)
      const g = this.gesture
      if (!g) return
      if (g.kind === 'pinch') {
        if (this.pointers.size < 2) this.gesture = null
        return
      }
      if (g.id !== ev.pointerId) return
      this.gesture = null
      this.setCursor(this.hover || this.hoverEdge ? 'pointer' : '')
      if (g.kind === 'move') {
        this.endMove(g)
        return
      }
      if (g.kind === 'marquee') {
        this.dirty = true
        this.schedule()
        if (!cancelled) this.events.marquee(this.boxesIn(g.x0, g.y0, g.x1, g.y1), g.additive)
        return
      }
      if (g.kind !== 'press' || cancelled) return
      // a click; twice at the same place: a double-click
      const now = performance.now()
      const key = g.box ? g.box.key : g.edge ? g.edge.key : ''
      const last = this.lastClick
      if (last && now - last.t < DBL_MS && Math.hypot(ev.clientX - last.x, ev.clientY - last.y) < 10 && last.key === key) {
        this.lastClick = null
        if (g.box) this.events.dblbox(g.box, g.header)
        else if (!g.edge) this.fit()
        return
      }
      this.lastClick = { t: now, x: ev.clientX, y: ev.clientY, key }
      if (g.edge && !(g.box && g.box.kind !== 'group')) this.events.edge(g.edge, g.mods)
      else if (g.box) this.events.box(g.box, g.header, g.mods)
      else this.events.empty(g.mods)
    }
    on(cv, 'pointerup', (ev) => release(ev, false))
    on(cv, 'pointercancel', (ev) => release(ev, true))
    on(cv, 'lostpointercapture', (ev) => {
      if (this.pointers.has(ev.pointerId)) release(ev, true)
    })
    on(cv, 'pointerleave', () => {
      if (!this.gesture && (this.hover || this.hoverEdge)) {
        this.hover = this.hoverEdge = null
        this.dirty = true
        this.schedule()
      }
    })
    on(
      cv,
      'wheel',
      (ev) => {
        ev.preventDefault()
        const [sx, sy] = this.local(ev)
        const k = ev.ctrlKey ? 0.012 : 0.0018
        const dl = ev.deltaMode === 1 ? ev.deltaY * 16 : ev.deltaY
        this.zoomBy(Math.exp(-dl * k), sx, sy)
      },
      { passive: false },
    )
    on(cv, 'keydown', (ev) => {
      const step = 60
      const moves: Record<string, [number, number]> = {
        ArrowLeft: [-step, 0],
        ArrowRight: [step, 0],
        ArrowUp: [0, -step],
        ArrowDown: [0, step],
      }
      const m = moves[ev.key]
      if (!m || ev.altKey) return
      if (ev.shiftKey) this.events.nudge(Math.sign(m[0]) * 10, Math.sign(m[1]) * 10)
      else this.pan(m[0], m[1])
      ev.preventDefault()
    })
    const mini = this.mini
    on(mini, 'pointerdown', (ev) => {
      mini.setPointerCapture(ev.pointerId)
      this.miniMove(ev)
      const move = (e: PointerEvent) => this.miniMove(e)
      const up = () => {
        mini.removeEventListener('pointermove', move)
        mini.removeEventListener('pointerup', up)
      }
      mini.addEventListener('pointermove', move)
      mini.addEventListener('pointerup', up)
    })
    if (typeof ResizeObserver !== 'undefined') {
      this.ro = new ResizeObserver(() => this.resize())
      this.ro.observe(cv)
      this.ro.observe(mini)
    }
  }
}

function segDist(px: number, py: number, x0: number, y0: number, x1: number, y1: number) {
  const dx = x1 - x0,
    dy = y1 - y0,
    l2 = dx * dx + dy * dy
  let t = l2 ? ((px - x0) * dx + (py - y0) * dy) / l2 : 0
  t = Math.max(0, Math.min(1, t))
  return Math.hypot(px - (x0 + t * dx), py - (y0 + t * dy))
}
