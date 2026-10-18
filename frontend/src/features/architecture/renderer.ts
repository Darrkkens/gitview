// Canvas renderer. Draws a scene (boxes and edges in world coordinates,
// computed by the views) with a pan/zoom camera, culling, level of detail
// and highlight/dim states, plus the minimap. It knows nothing about the
// dataset: views give it boxes and edges, it reports clicks.

import { token } from '../../lib/tokens'
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
}

export interface RendererEvents {
  box(box: Box, header: boolean, ev: PointerEvent): void
  edge(e: SceneEdge): void
  empty(): void
  dblbox(box: Box, header: boolean): void
}

const EMPTY: Scene = { boxes: [], edges: [], bounds: { x: 0, y: 0, w: 1, h: 1 } }
const DASH: Record<string, number[]> = { likely: [6, 4], possible: [2, 4], unresolved: [5, 4] }
const reduced = () => !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches

interface Tween {
  t0: number
  ms: number
  from: Map<string, { x: number; y: number; w: number; h: number }>
}
interface CamTween {
  t0: number
  ms: number
  a: { x: number; y: number; z: number }
  b: { x: number; y: number; z: number }
}

export class Renderer {
  private ctx: CanvasRenderingContext2D
  private mctx: CanvasRenderingContext2D
  private W = 0
  private H = 0
  private dpr = 1
  private scene: Scene = EMPTY
  private byKey = new Map<string, Box>()
  private edgeByKey = new Map<string, SceneEdge>()
  private cam = { x: 0, y: 0, z: 1 }
  private hl: Highlight | null = null
  private selected: string | null = null
  private hover: string | null = null
  private hoverEdge: string | null = null
  private tween: Tween | null = null
  private camTween: CamTween | null = null
  private dirty = true
  private miniDirty = true
  private showMini = true
  private raf = 0
  private miniT: { s: number; ox: number; oy: number } | null = null
  private drag: { x: number; y: number; cx: number; cy: number; moved: boolean } | null = null
  private ro: ResizeObserver | null = null
  private cleanup: (() => void)[] = []
  readonly stats = { draws: 0, last: 0, max: 0 }

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
   * their key from their previous place; opts.fit fits the camera;
   * opts.anchor keeps that box at the same place on screen.
   */
  setScene(next: Scene, opts: SceneOpts = {}) {
    const prev = this.byKey
    const anchorBox = opts.anchor ? prev.get(opts.anchor) : undefined
    const before = anchorBox ? this.screenOfBox(anchorBox) : null
    this.scene = next
    this.byKey = new Map()
    this.edgeByKey = new Map()
    const tf = TITLE_FONT(),
      sf = SUB_FONT(),
      hf = HEAD_FONT()
    for (const b of next.boxes) {
      this.byKey.set(b.key, b)
      const countW = b.count ? textWidth(b.count, sf) + 10 : 0
      if (b.kind === 'group') {
        // header: ■ title  subtitle ……… count
        b._title = fitText(b.title, hf, Math.max(10, b.w - 36 - countW))
        const rest = b.w - 36 - countW - textWidth(b._title, hf) - 10
        b._sub = b.sub && rest > 40 ? fitText(b.sub, sf, rest) : ''
      } else {
        const left = 22,
          right = 10 + (b.chev ? 16 : 0) + countW
        b._title = fitText(b.title, tf, Math.max(10, b.w - left - right))
        b._sub = b.sub ? fitText(b.sub, sf, Math.max(10, b.w - left - 10)) : ''
      }
    }
    for (const e of next.edges) {
      this.edgeByKey.set(e.key, e)
      let x0 = Infinity,
        y0 = Infinity,
        x1 = -Infinity,
        y1 = -Infinity
      for (let k = 0; k < e.pts.length; k += 2) {
        x0 = Math.min(x0, e.pts[k])
        x1 = Math.max(x1, e.pts[k])
        y0 = Math.min(y0, e.pts[k + 1])
        y1 = Math.max(y1, e.pts[k + 1])
      }
      e._bb = [x0, y0, x1, y1]
    }
    if (this.selected && !this.byKey.has(this.selected) && !this.edgeByKey.has(this.selected)) this.selected = null
    if (this.hover && !this.byKey.has(this.hover)) this.hover = null
    this.hoverEdge = null
    if (opts.animate && prev.size && !reduced()) {
      const from = new Map<string, { x: number; y: number; w: number; h: number }>()
      for (const b of next.boxes) {
        const p = prev.get(b.key)
        if (p) from.set(b.key, { x: p.x, y: p.y, w: p.w, h: p.h })
      }
      this.tween = { t0: performance.now(), ms: 260, from }
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

  // ---------- camera ----------
  private clampZ(z: number) {
    return Math.max(0.03, Math.min(z, 3))
  }
  private fitZoom(bb: { w: number; h: number }, pad = 48) {
    return this.clampZ(
      Math.min((this.W - pad * 2) / Math.max(bb.w, 1), (this.H - pad * 2 - 30) / Math.max(bb.h, 1), 1.25),
    )
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
  /** reveal brings boxes into view, zooming in only as far as needed. */
  reveal(keys: string[], animate?: boolean) {
    const bs = keys.map((k) => this.byKey.get(k)).filter((b): b is Box => !!b)
    if (!bs.length) return
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
    const bb = { x: x0, y: y0, w: x1 - x0, h: y1 - y0 }
    let z = this.cam.z
    const need = this.fitZoom(bb, 80)
    if (z > need) z = need
    if (z < 0.55 && need >= 0.55) z = Math.min(need, 0.9)
    this.animateTo(bb.x + bb.w / 2, bb.y + bb.h / 2, z, animate)
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

  // ---------- state from the view ----------
  setHighlight(h: Highlight | null) {
    this.hl = h
    this.dirty = this.miniDirty = true
    this.schedule()
  }
  setSelected(key: string | null) {
    this.selected = key
    this.dirty = this.miniDirty = true
    this.schedule()
  }
  setMinimap(on: boolean) {
    this.showMini = on
    this.miniDirty = true
    this.schedule()
  }
  /** redraw after a theme change. */
  redraw() {
    this.dirty = this.miniDirty = true
    this.schedule()
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
    if (this.miniDirty && this.showMini) {
      this.drawMini()
      this.miniDirty = false
    }
    if (this.tween && tp >= 1) this.tween = null
    if (this.camTween || this.tween) this.schedule()
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
    if (!hl) return 1
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

    // groups, outer first
    for (const b of scene.boxes) {
      if (b.kind !== 'group') continue
      const g = this.geom(b, tp)
      if (!inView(g.x, g.y, g.w, g.h)) continue
      ctx.globalAlpha = this.boxAlpha(b)
      this.rr(ctx, g.x, g.y, g.w, g.h, 8)
      ctx.fillStyle = C('group-fill')
      ctx.fill()
      ctx.lineWidth = (b.key === this.selected ? 2 : 1) * px
      ctx.strokeStyle = b.key === this.selected ? C('accent') : b.scope ? C('border-strong') : C('group-border')
      if (b.muted) ctx.setLineDash([4 * px, 3 * px])
      ctx.stroke()
      ctx.setLineDash([])
      // header
      ctx.save()
      this.rr(ctx, g.x, g.y, g.w, g.h, 8)
      ctx.clip()
      ctx.fillStyle = C('group-head')
      ctx.fillRect(g.x, g.y, g.w, 30)
      ctx.restore()
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
      }
      if (!showText && settled) late.push([b.title, g, 11, 600, true, g.w * g.h * 4])
      if (b.key === this.hover && b.key !== this.selected) {
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
          // branches (inside a box, towards a bundled trunk) only show on demand
          if (e.branch && !role && !isHover && e.key !== this.selected) continue
          const strong = !!role || e.key === this.selected || isHover
          if (strongPass !== strong) continue
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
      if (!inView(g.x, g.y, g.w, g.h)) continue
      ctx.globalAlpha = this.boxAlpha(b) * (this.tween && !this.tween.from.has(b.key) && tp < 1 ? tp : 1)
      this.drawNode(b, g, px, showText, showSub)
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
        if (!e.label || e.branch) return 0
        if (e.key === this.selected || this.hoverEdge === e.key) return 3
        const h = this.hover
        if (h && (e.s === h || e.t === h || e.units?.has(h))) return 2
        return (role === 'out' || role === 'in') && (e.n > 1 || Object.keys(e.agg.types).length > 1) ? 1 : 0
      }
      scene.edges
        .map((e) => [want(e), e] as const)
        .filter((x) => x[0])
        .sort((a, b) => b[0] - a[0])
        .forEach(([, e]) => this.drawEdgeLabel(e, px, taken))
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

  private drawNode(
    b: Box,
    g: { x: number; y: number; w: number; h: number },
    px: number,
    showText: boolean,
    showSub: boolean,
  ) {
    const ctx = this.ctx
    const role = this.hl?.boxes.get(b.key)
    const sel = b.key === this.selected
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
    const twoLines = showSub && !!b._sub && g.h >= 34
    const tx = g.x + 12
    ctx.textBaseline = 'middle'
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
      ctx.strokeStyle = C('text-3')
      ctx.lineWidth = 1.4 * px
      ctx.beginPath()
      const cx = rx - 4,
        cy = g.y + g.h / 2
      if (b.chev === 'closed') {
        ctx.moveTo(cx - 2, cy - 4)
        ctx.lineTo(cx + 2, cy)
        ctx.lineTo(cx - 2, cy + 4)
      } else {
        ctx.moveTo(cx - 4, cy - 2)
        ctx.lineTo(cx, cy + 2)
        ctx.lineTo(cx + 4, cy - 2)
      }
      ctx.stroke()
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

  private drawEdge(e: SceneEdge, role: EdgeRole | undefined, isHover: boolean, px: number, alpha: number) {
    const ctx = this.ctx
    const p = e.pts
    if (p.length < 4) return
    const far = role === 'out2' || role === 'in2'
    const color =
      role === 'out' || role === 'out2'
        ? C('hl-out')
        : role === 'in' || role === 'in2'
          ? C('hl-in')
          : e.key === this.selected
            ? C('accent')
            : e.conf === 'unresolved'
              ? C('c-unresolved')
              : isHover
                ? C('edge-strong')
                : C('edge')
    const width =
      (1 +
        Math.min(Math.log2(1 + (e.n || 1)), 3) * 0.45 +
        ((role && !far) || isHover || e.key === this.selected ? 0.7 : 0)) *
      px
    ctx.globalAlpha =
      alpha *
      (this.hl && !role && e.key !== this.selected ? Number(C('dim-alpha')) || 0.16 : far && !isHover ? 0.45 : 1)
    ctx.strokeStyle = color
    ctx.fillStyle = color
    ctx.lineWidth = width
    const dash = DASH[e.conf]
    ctx.setLineDash(dash ? dash.map((d) => d * px) : [])
    ctx.beginPath()
    ctx.moveTo(p[0], p[1])
    const r = 6
    for (let k = 2; k < p.length - 2; k += 2) ctx.arcTo(p[k], p[k + 1], p[k + 2], p[k + 3], r)
    // stop the line at the arrow's base
    const ex = p[p.length - 2],
      ey = p[p.length - 1],
      sx = p[p.length - 4],
      sy = p[p.length - 3]
    const len = Math.hypot(ex - sx, ey - sy) || 1
    const ah = Math.min(8 * px, len * 0.6),
      ux = (ex - sx) / len,
      uy = (ey - sy) / len
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

  private drawEdgeLabel(e: SceneEdge, px: number, taken: number[][]) {
    const ctx = this.ctx
    const [mx, my] = midpoint(e.pts)
    const f = font(500, 10.5)
    ctx.font = f
    const label = e.label ?? ''
    const w = textWidth(label, f) + 10
    const r = [mx - w / 2 - 2, my - 10, mx + w / 2 + 2, my + 10]
    if (taken.some((q) => r[0] < q[2] && r[2] > q[0] && r[1] < q[3] && r[3] > q[1])) return
    taken.push(r)
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
      if (b.kind !== 'group') continue
      mctx.strokeStyle = C('group-border')
      mctx.lineWidth = dpr
      mctx.strokeRect(b.x * s + ox, b.y * s + oy, Math.max(1, b.w * s), Math.max(1, b.h * s))
    }
    for (const b of scene.boxes) {
      if (b.kind === 'group') continue
      mctx.globalAlpha = this.hl && !this.hl.boxes.has(b.key) ? 0.25 : 0.85
      mctx.fillStyle = b.key === this.selected ? C('accent') : C(b.color)
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
      if (b.kind === 'group') continue
      if (p.x >= b.x && p.x <= b.x + b.w && p.y >= b.y && p.y <= b.y + b.h) return b
    }
    let best: Box | null = null
    for (const b of boxes) {
      if (b.kind !== 'group') continue
      if (p.x >= b.x && p.x <= b.x + b.w && p.y >= b.y && p.y <= b.y + b.h && (!best || b.depth >= best.depth)) best = b
    }
    return best
  }
  private inHeader(b: Box, sx: number, sy: number) {
    const p = this.toWorld(sx, sy)
    return b.kind === 'group' && p.y - b.y <= 30
  }
  private edgeAt(sx: number, sy: number): SceneEdge | null {
    const p = this.toWorld(sx, sy),
      tol = 5 / this.cam.z
    let best: SceneEdge | null = null,
      bd = tol
    for (const e of this.scene.edges) {
      // branches are hidden until asked for: they cannot be picked
      if (e.branch && !(this.hl?.edges.has(e.key) || e.key === this.selected)) continue
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

  // ---------- input ----------
  private local(ev: { clientX: number; clientY: number }) {
    const r = this.canvas.getBoundingClientRect()
    return [ev.clientX - r.left, ev.clientY - r.top] as const
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
      if (ev.button !== 0) return
      cv.focus({ preventScroll: true })
      this.drag = { x: ev.clientX, y: ev.clientY, cx: this.cam.x, cy: this.cam.y, moved: false }
      cv.setPointerCapture(ev.pointerId)
    })
    on(cv, 'pointermove', (ev) => {
      const [sx, sy] = this.local(ev)
      const d = this.drag
      if (d) {
        const dx = ev.clientX - d.x,
          dy = ev.clientY - d.y
        if (!d.moved && Math.hypot(dx, dy) > 4) {
          d.moved = true
          cv.style.cursor = 'grabbing'
        }
        if (d.moved) {
          this.cam.x = d.cx - dx / this.cam.z
          this.cam.y = d.cy - dy / this.cam.z
          this.camTween = null
          this.dirty = this.miniDirty = true
          this.schedule()
        }
        return
      }
      const b = this.boxAt(sx, sy)
      const e = b && b.kind !== 'group' ? null : this.edgeAt(sx, sy)
      const hk = b && b.kind !== 'group' ? b.key : b && this.inHeader(b, sx, sy) ? b.key : null
      const ek = e ? e.key : null
      if (hk !== this.hover || ek !== this.hoverEdge) {
        this.hover = hk
        this.hoverEdge = hk ? null : ek
        cv.style.cursor = this.hover || this.hoverEdge ? 'pointer' : ''
        const he = this.hoverEdge ? this.edgeByKey.get(this.hoverEdge) : undefined
        const hb = this.hover ? this.byKey.get(this.hover) : undefined
        cv.title = he?.label ? he.label : hb?.tip ? hb.tip : ''
        this.dirty = true
        this.schedule()
      }
    })
    on(cv, 'pointerup', (ev) => {
      const d = this.drag
      if (!d) return
      this.drag = null
      cv.style.cursor = this.hover || this.hoverEdge ? 'pointer' : ''
      if (d.moved) return
      const [sx, sy] = this.local(ev)
      const b = this.boxAt(sx, sy)
      const e = b && b.kind !== 'group' ? null : this.edgeAt(sx, sy)
      if (e && !(b && b.kind !== 'group')) this.events.edge(e)
      else if (b) this.events.box(b, this.inHeader(b, sx, sy), ev)
      else this.events.empty()
    })
    on(cv, 'pointercancel', () => {
      this.drag = null
      cv.style.cursor = ''
    })
    on(cv, 'pointerleave', () => {
      if (this.hover || this.hoverEdge) {
        this.hover = this.hoverEdge = null
        this.dirty = true
        this.schedule()
      }
    })
    on(cv, 'dblclick', (ev) => {
      const [sx, sy] = this.local(ev)
      const b = this.boxAt(sx, sy)
      if (b) this.events.dblbox(b, this.inHeader(b, sx, sy))
      else this.fit()
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
      if (m && !ev.altKey) {
        this.pan(m[0], m[1])
        ev.preventDefault()
      }
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

function midpoint(p: number[]): [number, number] {
  let total = 0
  for (let k = 0; k < p.length - 2; k += 2) total += Math.hypot(p[k + 2] - p[k], p[k + 3] - p[k + 1])
  let half = total / 2
  for (let k = 0; k < p.length - 2; k += 2) {
    const l = Math.hypot(p[k + 2] - p[k], p[k + 3] - p[k + 1])
    if (half <= l) {
      const f = l ? half / l : 0
      return [p[k] + (p[k + 2] - p[k]) * f, p[k + 1] + (p[k + 3] - p[k + 1]) * f]
    }
    half -= l
  }
  return [p[0], p[1]]
}

function segDist(px: number, py: number, x0: number, y0: number, x1: number, y1: number) {
  const dx = x1 - x0,
    dy = y1 - y0,
    l2 = dx * dx + dy * dy
  let t = l2 ? ((px - x0) * dx + (py - y0) * dy) / l2 : 0
  t = Math.max(0, Math.min(1, t))
  return Math.hypot(px - (x0 + t * dx), py - (y0 + t * dy))
}
