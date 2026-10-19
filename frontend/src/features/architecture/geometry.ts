// Geometry of the editor, free of the canvas and of the dataset: rectangles,
// alignment and distribution of cards, free space for new cards, the size of
// a group around what it holds, and the threshold between a click and a drag.

export interface Rect {
  x: number
  y: number
  w: number
  h: number
}

export const overlaps = (a: Rect, b: Rect, gap = 0) =>
  a.x < b.x + b.w + gap && a.x + a.w + gap > b.x && a.y < b.y + b.h + gap && a.y + a.h + gap > b.y

export function union(list: Rect[]): Rect | null {
  if (!list.length) return null
  let x0 = Infinity,
    y0 = Infinity,
    x1 = -Infinity,
    y1 = -Infinity
  for (const r of list) {
    if (r.x < x0) x0 = r.x
    if (r.y < y0) y0 = r.y
    if (r.x + r.w > x1) x1 = r.x + r.w
    if (r.y + r.h > y1) y1 = r.y + r.h
  }
  return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 }
}

export const contains = (outer: Rect, inner: Rect) =>
  inner.x >= outer.x && inner.y >= outer.y && inner.x + inner.w <= outer.x + outer.w && inner.y + inner.h <= outer.y + outer.h

/** Movement (CSS pixels) below which a press is a click: larger for a finger. */
export function dragThreshold(pointerType: string): number {
  return pointerType === 'touch' ? 8 : pointerType === 'pen' ? 6 : 4
}

export const isDrag = (dx: number, dy: number, pointerType: string) =>
  Math.hypot(dx, dy) > dragThreshold(pointerType)

export type AlignMode = 'left' | 'center' | 'right' | 'top' | 'middle' | 'bottom'

/**
 * align returns the new top-left of each rectangle: left/center/right line
 * them up on the leftmost edge, the centre of the selection, the rightmost
 * edge (top/middle/bottom likewise vertically).
 */
export function align(rects: Rect[], mode: AlignMode): { x: number; y: number }[] {
  const bb = union(rects)
  if (!bb) return []
  return rects.map((r) => {
    switch (mode) {
      case 'left':
        return { x: bb.x, y: r.y }
      case 'center':
        return { x: bb.x + bb.w / 2 - r.w / 2, y: r.y }
      case 'right':
        return { x: bb.x + bb.w - r.w, y: r.y }
      case 'top':
        return { x: r.x, y: bb.y }
      case 'middle':
        return { x: r.x, y: bb.y + bb.h / 2 - r.h / 2 }
      case 'bottom':
        return { x: r.x, y: bb.y + bb.h - r.h }
    }
  })
}

/**
 * distribute spaces rectangles evenly along an axis: the first and last
 * (by position) stay, the gaps between consecutive ones become equal. With
 * less room than the cards need, they are packed with a minimum gap.
 */
export function distribute(rects: Rect[], axis: 'h' | 'v', minGap = 12): { x: number; y: number }[] {
  const out = rects.map((r) => ({ x: r.x, y: r.y }))
  if (rects.length < 3) return out
  const idx = rects.map((_, i) => i)
  const pos = (r: Rect) => (axis === 'h' ? r.x : r.y)
  const len = (r: Rect) => (axis === 'h' ? r.w : r.h)
  idx.sort((a, b) => pos(rects[a]) - pos(rects[b]) || a - b)
  const first = rects[idx[0]],
    last = rects[idx[idx.length - 1]]
  const span = pos(last) + len(last) - pos(first)
  const total = idx.reduce((s, i) => s + len(rects[i]), 0)
  const gap = Math.max(minGap, (span - total) / (idx.length - 1))
  let at = pos(first)
  for (const i of idx) {
    if (axis === 'h') out[i].x = at
    else out[i].y = at
    at += len(rects[i]) + gap
  }
  return out
}

/**
 * placeAvoiding moves each free rectangle (in order) down until it overlaps
 * none of the fixed ones nor the free ones already placed. Returns the new
 * top-left of each free rectangle.
 */
export function placeAvoiding(fixed: Rect[], free: Rect[], gap = 16): { x: number; y: number }[] {
  const placed = fixed.slice()
  return free.map((r0) => {
    const r = { x: r0.x, y: r0.y, w: r0.w, h: r0.h }
    for (let guard = 0; guard < 400; guard++) {
      let hit: Rect | null = null
      for (const p of placed)
        if (overlaps(r, p, gap) && (!hit || p.y + p.h > hit.y + hit.h)) hit = p
      if (!hit) break
      r.y = hit.y + hit.h + gap
    }
    placed.push(r)
    return { x: r.x, y: r.y }
  })
}

/**
 * groupRect: the rectangle of a group around what it holds. A group keeps at
 * least the size its automatic layout gave it, anchored at its origin, and
 * grows (in any direction) to contain children dragged beyond its edges.
 * kids are in the same coordinates as the origin.
 */
export function groupRect(
  origin: { x: number; y: number },
  min: { w: number; h: number },
  kids: Rect[],
  pad: { top: number; left: number; right: number; bottom: number },
): Rect {
  let x0 = origin.x,
    y0 = origin.y,
    x1 = origin.x + min.w,
    y1 = origin.y + min.h
  for (const k of kids) {
    if (k.x - pad.left < x0) x0 = k.x - pad.left
    if (k.y - pad.top < y0) y0 = k.y - pad.top
    if (k.x + k.w + pad.right > x1) x1 = k.x + k.w + pad.right
    if (k.y + k.h + pad.bottom > y1) y1 = k.y + k.h + pad.bottom
  }
  return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 }
}

/** fitZoom: the zoom that shows a rectangle in a viewport, with a margin. */
export function fitZoom(bb: { w: number; h: number }, view: { w: number; h: number }, pad: number, max: number) {
  return Math.min((view.w - pad * 2) / Math.max(bb.w, 1), (view.h - pad * 2) / Math.max(bb.h, 1), max)
}

/**
 * removeOverlaps pushes overlapping rectangles apart (in place), along the
 * axis where they overlap least, until none overlap or the rounds run out.
 * Force layouts place cards by their centres and may overlap large ones.
 */
export function removeOverlaps(rects: Rect[], gap = 16, rounds = 60) {
  for (let r = 0; r < rounds; r++) {
    let moved = false
    for (let i = 0; i < rects.length; i++)
      for (let j = i + 1; j < rects.length; j++) {
        const a = rects[i],
          b = rects[j]
        if (!overlaps(a, b, gap - 0.01)) continue
        const ox = Math.min(a.x + a.w + gap - b.x, b.x + b.w + gap - a.x),
          oy = Math.min(a.y + a.h + gap - b.y, b.y + b.h + gap - a.y)
        const acx = a.x + a.w / 2,
          bcx = b.x + b.w / 2,
          acy = a.y + a.h / 2,
          bcy = b.y + b.h / 2
        if (ox <= oy) {
          const s = (acx < bcx || (acx === bcx && i < j) ? -1 : 1) * (ox / 2)
          a.x += s
          b.x -= s
        } else {
          const s = (acy < bcy || (acy === bcy && i < j) ? -1 : 1) * (oy / 2)
          a.y += s
          b.y -= s
        }
        moved = true
      }
    if (!moved) return
  }
}
