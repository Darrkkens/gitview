// Edge router: the geometry of a line between two cards, computed from
// their rectangles alone, so lines follow cards that move. Used whenever
// the automatic layout's own routes no longer apply (a card was moved, the
// layout gives no routes, straight lines were asked for).
//
//   sides        which sides of the two cards a line leaves and enters
//   spreadPorts  ports along a side, ordered by where their lines go (fewer
//                crossings than sharing one point)
//   routeLine    the polyline in the chosen style: orthogonal (an elbow),
//                smooth (a sampled cubic curve), straight
//   smoothRoute  a smooth version of a routed polyline (the layout's routes)
//
// Lines are always polylines, so drawing, hit testing and labels treat
// every style alike.

import type { Rect } from './geometry'
import type { EdgeStyle } from './layoutDoc'

export type Side = 't' | 'b' | 'l' | 'r'

export interface Port {
  x: number
  y: number
  side: Side
}

const NORMAL: Record<Side, [number, number]> = { t: [0, -1], b: [0, 1], l: [-1, 0], r: [1, 0] }

/**
 * sides picks where a line from A to B leaves A and enters B: across the
 * widest gap between them, preferring the flow's axis (v: top to bottom,
 * h: left to right) when both gaps are clear.
 */
export function sides(A: Rect, B: Rect, prefer: 'v' | 'h' = 'v'): [Side, Side] {
  const gx = Math.max(B.x - (A.x + A.w), A.x - (B.x + B.w))
  const gy = Math.max(B.y - (A.y + A.h), A.y - (B.y + B.h))
  const vOK = gy >= 8,
    hOK = gx >= 8
  let vertical: boolean
  if (vOK && hOK) vertical = prefer === 'v' ? gx < gy * 3 : gy > gx * 3
  else if (vOK) vertical = true
  else if (hOK) vertical = false
  else {
    const dx = B.x + B.w / 2 - (A.x + A.w / 2),
      dy = B.y + B.h / 2 - (A.y + A.h / 2)
    vertical = Math.abs(dy) / Math.max(1, A.h + B.h) >= Math.abs(dx) / Math.max(1, A.w + B.w)
  }
  if (vertical) return B.y + B.h / 2 >= A.y + A.h / 2 ? ['b', 't'] : ['t', 'b']
  return B.x + B.w / 2 >= A.x + A.w / 2 ? ['r', 'l'] : ['l', 'r']
}

/** A port to place: on side of rect, ordered by `along` (where the line goes). */
export interface PortRequest {
  rect: Rect
  side: Side
  along: number
  set: (p: Port) => void
}

/**
 * spreadPorts places the ports of one side of one card evenly along the
 * middle of the side, in the order of where their lines go.
 */
export function spreadPorts(list: PortRequest[]) {
  if (!list.length) return
  const { rect: r, side } = list[0]
  list.sort((a, b) => a.along - b.along)
  const n = list.length
  list.forEach((p, j) => {
    const f = (j + 1) / (n + 1)
    const along = side === 't' || side === 'b' ? r.x + r.w * (0.15 + 0.7 * f) : r.y + r.h * (0.2 + 0.6 * f)
    p.set(
      side === 't'
        ? { x: along, y: r.y, side }
        : side === 'b'
          ? { x: along, y: r.y + r.h, side }
          : side === 'l'
            ? { x: r.x, y: along, side }
            : { x: r.x + r.w, y: along, side },
    )
  })
}

/** centerPort: the middle of a side. */
export function centerPort(r: Rect, side: Side): Port {
  return side === 't'
    ? { x: r.x + r.w / 2, y: r.y, side }
    : side === 'b'
      ? { x: r.x + r.w / 2, y: r.y + r.h, side }
      : side === 'l'
        ? { x: r.x, y: r.y + r.h / 2, side }
        : { x: r.x + r.w, y: r.y + r.h / 2, side }
}

/** elbow: an orthogonal line between two ports, turning halfway. */
export function elbow(a: Port, b: Port): number[] {
  const vertical = a.side === 't' || a.side === 'b'
  let pts: number[]
  if (vertical) {
    const my = (a.y + b.y) / 2
    pts = [a.x, a.y, a.x, my, b.x, my, b.x, b.y]
  } else {
    const mx = (a.x + b.x) / 2
    pts = [a.x, a.y, mx, a.y, mx, b.y, b.x, b.y]
  }
  return Math.abs(pts[0] - pts[6]) < 1 || Math.abs(pts[1] - pts[7]) < 1 ? [a.x, a.y, b.x, b.y] : pts
}

/** curve: a cubic curve leaving and entering the cards square to their sides, sampled. */
export function curve(a: Port, b: Port, samples = 18): number[] {
  const d = Math.hypot(b.x - a.x, b.y - a.y)
  const k = Math.max(18, Math.min(140, d * 0.45))
  const na = NORMAL[a.side],
    nb = NORMAL[b.side]
  const c1x = a.x + na[0] * k,
    c1y = a.y + na[1] * k,
    c2x = b.x + nb[0] * k,
    c2y = b.y + nb[1] * k
  const pts: number[] = []
  for (let i = 0; i <= samples; i++) {
    const t = i / samples,
      u = 1 - t
    const w0 = u * u * u,
      w1 = 3 * u * u * t,
      w2 = 3 * u * t * t,
      w3 = t * t * t
    pts.push(w0 * a.x + w1 * c1x + w2 * c2x + w3 * b.x, w0 * a.y + w1 * c1y + w2 * c2y + w3 * b.y)
  }
  return pts
}

/** routeLine: the polyline between two ports in a style. */
export function routeLine(a: Port, b: Port, style: EdgeStyle): number[] {
  if (style === 'straight') return [a.x, a.y, b.x, b.y]
  if (style === 'smooth') return curve(a, b)
  return elbow(a, b)
}

/**
 * smoothRoute turns a routed polyline into a smooth one: each bend becomes a
 * quadratic curve between the middles of its segments (the route keeps its
 * way around the cards).
 */
export function smoothRoute(p: number[], samples = 8): number[] {
  const n = p.length / 2
  if (n <= 2) return p.slice()
  const out = [p[0], p[1]]
  let sx = p[0],
    sy = p[1]
  for (let i = 1; i < n - 1; i++) {
    const bx = p[2 * i],
      by = p[2 * i + 1]
    const last = i === n - 2
    const ex = last ? p[2 * i + 2] : (bx + p[2 * i + 2]) / 2,
      ey = last ? p[2 * i + 3] : (by + p[2 * i + 3]) / 2
    for (let s = 1; s <= samples; s++) {
      const t = s / samples,
        u = 1 - t
      out.push(u * u * sx + 2 * u * t * bx + t * t * ex, u * u * sy + 2 * u * t * by + t * t * ey)
    }
    sx = ex
    sy = ey
  }
  return out
}

/** translate moves a polyline in place. */
export function translate(p: number[], dx: number, dy: number) {
  for (let k = 0; k < p.length; k += 2) {
    p[k] += dx
    p[k + 1] += dy
  }
}

/**
 * branchRoute: the short line inside a group from an inner card R to the
 * point P where the group's trunk leaves (end 0) or enters (end 1) the
 * group's rectangle G.
 */
export function branchRoute(P: [number, number], R: Rect, G: Rect, end: 0 | 1, style: EdgeStyle): number[] {
  const onTopOrBottom = Math.abs(P[1] - G.y) < 2 || Math.abs(P[1] - (G.y + G.h)) < 2
  let q: Port, p: Port
  if (onTopOrBottom) {
    const below = P[1] > R.y + R.h / 2
    q = centerPort(R, below ? 'b' : 't')
    p = { x: P[0], y: P[1], side: below ? 't' : 'b' }
  } else {
    const right = P[0] > R.x + R.w / 2
    q = centerPort(R, right ? 'r' : 'l')
    p = { x: P[0], y: P[1], side: right ? 'l' : 'r' }
  }
  return end === 0 ? routeLine(q, p, style) : routeLine(p, q, style)
}

/** bbox of a polyline: [x0, y0, x1, y1]. */
export function bboxOf(p: number[]): [number, number, number, number] {
  let x0 = Infinity,
    y0 = Infinity,
    x1 = -Infinity,
    y1 = -Infinity
  for (let k = 0; k < p.length; k += 2) {
    if (p[k] < x0) x0 = p[k]
    if (p[k] > x1) x1 = p[k]
    if (p[k + 1] < y0) y0 = p[k + 1]
    if (p[k + 1] > y1) y1 = p[k + 1]
  }
  return [x0, y0, x1, y1]
}
