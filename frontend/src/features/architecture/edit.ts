// Scene editor: moves cards of a drawn scene in place and keeps the scene
// coherent while they move, without a new layout:
//   - a group moves with everything inside it;
//   - a group grows to contain a child moved beyond its edges (and so on up);
//   - lines touching what moved follow it: a line whose two ends moved
//     together is translated, any other is routed again from the cards'
//     rectangles (route.ts), with the ports of the sides it shares spread
//     again; the short branches inside groups follow their trunk.
// The renderer calls move() on every pointer move of a drag (only the
// touched lines are recomputed); the controller then saves the positions.

import { groupRect, type Rect } from './geometry'
import { bboxOf, branchRoute, routeLine, sides, spreadPorts, translate, type PortRequest, type Side } from './route'
import type { Box, Scene, SceneEdge } from './types'

/** Room inside a group around its content: the header on top. */
export const PAD = { top: 42, left: 14, right: 14, bottom: 14 }

interface End {
  e: SceneEdge
  end: 0 | 1
}

const rectOf = (b: Box): Rect => ({ x: b.x, y: b.y, w: b.w, h: b.h })

export class SceneEditor {
  readonly scene: Scene
  readonly byKey = new Map<string, Box>()
  private kids = new Map<string, Box[]>()
  private at = new Map<string, SceneEdge[]>()
  private branchesOf = new Map<SceneEdge, SceneEdge[]>()
  private lists = new Map<string, Set<End>>()
  private endOf = new Map<SceneEdge, [End, End]>()

  constructor(scene: Scene) {
    this.scene = scene
    for (const b of scene.boxes) this.byKey.set(b.key, b)
    for (const b of scene.boxes) {
      if (!b.parent || !this.byKey.has(b.parent)) continue
      let l = this.kids.get(b.parent)
      if (!l) this.kids.set(b.parent, (l = []))
      l.push(b)
    }
    for (const e of scene.edges) {
      for (const k of [e.s, e.t]) {
        let l = this.at.get(k)
        if (!l) this.at.set(k, (l = []))
        l.push(e)
      }
      if (e.branch && e.trunk) {
        let l = this.branchesOf.get(e.trunk)
        if (!l) this.branchesOf.set(e.trunk, (l = []))
        l.push(e)
      }
    }
  }

  box(key: string) {
    return this.byKey.get(key)
  }
  kidsOf(key: string): Box[] {
    return this.kids.get(key) ?? []
  }
  /** Lines with an end at a box. */
  edgesAt(key: string): SceneEdge[] {
    return this.at.get(key) ?? []
  }

  isAncestor(a: string, b: Box): boolean {
    for (let p = b.parent; p; p = this.byKey.get(p)?.parent) if (p === a) return true
    return false
  }

  /** descendants calls fn for every box inside a group (any depth). */
  descendants(key: string, fn: (b: Box) => void) {
    const st = this.kidsOf(key).slice()
    while (st.length) {
      const b = st.pop()!
      fn(b)
      for (const k of this.kidsOf(b.key)) st.push(k)
    }
  }

  movable(b: Box | undefined): b is Box {
    return !!b && !!b.id && !b.locked && b.kind !== 'more'
  }

  /** topmost: the movable boxes of a selection, without those inside another selected box. */
  topmost(keys: Iterable<string>): Box[] {
    const set = new Set(keys)
    const out: Box[] = []
    for (const k of set) {
      const b = this.byKey.get(k)
      if (!this.movable(b)) continue
      let inside = false
      for (let p = b.parent; p; p = this.byKey.get(p)?.parent)
        if (set.has(p) && this.movable(this.byKey.get(p))) {
          inside = true
          break
        }
      if (!inside) out.push(b)
    }
    return out
  }

  // ---------- routing ----------
  private listKey(box: string, side: Side) {
    return box + '|' + side
  }
  private register(e: SceneEdge, end: 0 | 1, side: Side, dirty: Set<string>) {
    let ends = this.endOf.get(e)
    if (!ends) {
      ends = [
        { e, end: 0 },
        { e, end: 1 },
      ]
      this.endOf.set(e, ends)
    }
    const box = end === 0 ? e.s : e.t
    const cur = e.ports?.[end]
    const nk = this.listKey(box, side)
    if (cur) {
      const ok = this.listKey(box, cur.side)
      const l = this.lists.get(ok)
      if (ok === nk && l?.has(ends[end])) {
        // same side: its ports move along with the card
        dirty.add(nk)
        return
      }
      if (l?.delete(ends[end])) dirty.add(ok)
    }
    let l = this.lists.get(nk)
    if (!l) this.lists.set(nk, (l = new Set()))
    l.add(ends[end])
    dirty.add(nk)
    // the port is placed by the spread; until then, a provisional one
    if (!e.ports) e.ports = [null, null]
    e.ports[end] = { x: 0, y: 0, side }
  }
  private place(e: SceneEdge, dirty: Set<string>) {
    const A = this.byKey.get(e.s),
      B = this.byKey.get(e.t)
    if (!A || !B) return
    const [sa, sb] = sides(A, B, this.scene.prefer)
    e.local = true
    this.register(e, 0, sa, dirty)
    this.register(e, 1, sb, dirty)
  }
  /** flush spreads the ports of dirty sides and routes their lines again (then their branches). */
  private flush(dirty: Set<string>, also: Set<SceneEdge>) {
    const touched = new Set<SceneEdge>(also)
    for (const k of dirty) {
      const l = this.lists.get(k)
      if (!l || !l.size) continue
      const req: PortRequest[] = []
      for (const en of l) {
        const box = this.byKey.get(en.end === 0 ? en.e.s : en.e.t)!
        const other = this.byKey.get(en.end === 0 ? en.e.t : en.e.s)!
        const side = en.e.ports![en.end]!.side
        const along = side === 't' || side === 'b' ? other.x + other.w / 2 : other.y + other.h / 2
        req.push({ rect: rectOf(box), side, along, set: (p) => (en.e.ports![en.end] = p) })
        touched.add(en.e)
      }
      spreadPorts(req)
    }
    const style = this.scene.style
    for (const e of touched) {
      if (e.branch) continue
      if (e.local && e.ports && e.ports[0] && e.ports[1]) {
        e.pts = routeLine(e.ports[0], e.ports[1], style)
        e._bb = bboxOf(e.pts)
      }
      for (const b of this.branchesOf.get(e) ?? []) this.routeBranch(b)
    }
    for (const e of touched) if (e.branch) this.routeBranch(e)
  }
  private routeBranch(b: SceneEdge) {
    const tr = b.trunk
    if (!tr || tr.pts.length < 4) {
      b.pts = []
      return
    }
    const tp = tr.pts
    const end = b.end ?? 0
    const P: [number, number] = end === 0 ? [tp[0], tp[1]] : [tp[tp.length - 2], tp[tp.length - 1]]
    const inner = this.byKey.get(end === 0 ? b.s : b.t),
      group = this.byKey.get(end === 0 ? b.t : b.s)
    if (!inner || !group) return
    b.pts = branchRoute(P, rectOf(inner), rectOf(group), end, this.scene.style)
    b._bb = bboxOf(b.pts)
  }

  /** routeAll routes the local lines and the branches of a new scene. */
  routeAll() {
    const dirty = new Set<string>()
    for (const e of this.scene.edges) if (!e.branch && (e.local || e.pts.length < 4)) this.place(e, dirty)
    this.flush(dirty, new Set(this.scene.edges.filter((e) => e.branch)))
    for (const e of this.scene.edges) if (!e._bb) e._bb = bboxOf(e.pts)
  }

  // ---------- moving ----------
  /**
   * move moves boxes (with their content) by dx, dy and updates what
   * depends on them. Returns the boxes actually moved (the topmost movable
   * ones of keys).
   */
  move(keys: Iterable<string>, dx: number, dy: number): Box[] {
    const roots = this.topmost(keys)
    if (!roots.length || (!dx && !dy)) return roots
    const moved = new Set<string>()
    const shift = (b: Box) => {
      b.x += dx
      b.y += dy
      if (b.fx !== undefined) {
        b.fx += dx
        b.fy! += dy
      }
      moved.add(b.key)
    }
    for (const r of roots) {
      shift(r)
      this.descendants(r.key, shift)
    }
    // groups around what moved grow (or shrink back) to hold it
    const grown = new Set<string>()
    for (const r of roots) {
      for (let p = r.parent ? this.byKey.get(r.parent) : undefined; p; p = p.parent ? this.byKey.get(p.parent) : undefined) {
        if (moved.has(p.key)) break
        const nr = groupRect(
          { x: p.fx ?? p.x, y: p.fy ?? p.y },
          { w: p.minW ?? p.w, h: p.minH ?? p.h },
          this.kidsOf(p.key).map(rectOf),
          PAD,
        )
        if (nr.x === p.x && nr.y === p.y && nr.w === p.w && nr.h === p.h) break
        p.x = nr.x
        p.y = nr.y
        p.w = nr.w
        p.h = nr.h
        grown.add(p.key)
      }
    }
    // lines
    const seen = new Set<SceneEdge>(),
      dirty = new Set<string>(),
      also = new Set<SceneEdge>()
    const visit = (k: string) => {
      for (const e of this.edgesAt(k)) {
        if (seen.has(e)) continue
        seen.add(e)
        const together = moved.has(e.s) && moved.has(e.t) && !grown.has(e.s) && !grown.has(e.t)
        if (together) {
          translate(e.pts, dx, dy)
          if (e.ports)
            for (const p of e.ports)
              if (p) {
                p.x += dx
                p.y += dy
              }
          if (e._bb) e._bb = [e._bb[0] + dx, e._bb[1] + dy, e._bb[2] + dx, e._bb[3] + dy]
        } else if (e.branch) also.add(e)
        else {
          this.place(e, dirty)
          also.add(e)
        }
      }
    }
    moved.forEach(visit)
    grown.forEach(visit)
    this.flush(dirty, also)
    return roots
  }

  /** bounds recomputes the scene's bounds. */
  bounds() {
    let x0 = Infinity,
      y0 = Infinity,
      x1 = -Infinity,
      y1 = -Infinity
    for (const b of this.scene.boxes) {
      if (b.x < x0) x0 = b.x
      if (b.y < y0) y0 = b.y
      if (b.x + b.w > x1) x1 = b.x + b.w
      if (b.y + b.h > y1) y1 = b.y + b.h
    }
    if (!this.scene.boxes.length) x0 = y0 = x1 = y1 = 0
    this.scene.bounds = { x: x0, y: y0, w: Math.max(1, x1 - x0), h: Math.max(1, y1 - y0) }
    return this.scene.bounds
  }
}

/**
 * relativePosition: where a box is saved, relative to the origin of the
 * group drawn around it (absolute at the top level). A group is saved by
 * its origin, a card by its top-left corner.
 */
export function relativePosition(ed: SceneEditor, b: Box): { x: number; y: number } {
  const own = b.fx !== undefined ? { x: b.fx, y: b.fy! } : { x: b.x, y: b.y }
  const p = b.parent ? ed.box(b.parent) : undefined
  if (!p) return own
  return { x: own.x - (p.fx ?? p.x), y: own.y - (p.fy ?? p.y) }
}
