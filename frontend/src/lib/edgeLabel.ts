// Where the label of a line goes: on the line, clear of the cards and of
// the labels already placed. The middle of the line first, then the middles
// of its longest segments (and quarters of the long ones). A label with no
// free spot is left out: the line's tooltip and the inspector still say it.
//
// Lines are polylines [x0, y0, x1, y1, …]; rectangles are [x0, y0, x1, y1],
// all in the same (world) coordinates.

export type Rect4 = number[]

const hits = (r: Rect4, q: Rect4) => r[0] < q[2] && r[2] > q[0] && r[1] < q[3] && r[3] > q[1]

/** midpoint: the point halfway along a polyline. */
export function midpoint(p: number[]): [number, number] {
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

/** spots: candidate centres along a polyline, best first. */
export function labelSpots(p: number[]): [number, number][] {
  if (p.length < 4) return []
  const out: [number, number][] = [midpoint(p)]
  const segs: [number, number][] = []
  for (let k = 0; k + 3 < p.length; k += 2) segs.push([k, Math.hypot(p[k + 2] - p[k], p[k + 3] - p[k + 1])])
  segs.sort((a, b) => b[1] - a[1])
  for (const [k, len] of segs.slice(0, 4)) {
    for (const f of len > 160 ? [0.5, 0.25, 0.75] : [0.5])
      out.push([p[k] + (p[k + 2] - p[k]) * f, p[k + 1] + (p[k + 3] - p[k + 1]) * f])
  }
  return out
}

/**
 * placeLabel finds the centre of a w×h label on line p that overlaps none
 * of cards nor taken, and records it in taken. Returns null when every spot
 * is covered.
 */
export function placeLabel(p: number[], w: number, h: number, cards: Rect4[], taken: Rect4[]): [number, number] | null {
  for (const [x, y] of labelSpots(p)) {
    const r = [x - w / 2 - 2, y - h / 2 - 2, x + w / 2 + 2, y + h / 2 + 2]
    if (taken.some((q) => hits(r, q)) || cards.some((q) => hits(r, q))) continue
    taken.push(r)
    return [x, y]
  }
  return null
}
