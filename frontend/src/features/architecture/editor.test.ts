// Unit tests of the editor's pure parts: the layout document, the geometry
// of arranging cards, the edge router and the scene editor.

import { describe, expect, it } from 'vitest'
import { PAD, relativePosition, SceneEditor } from './edit'
import {
  align,
  distribute,
  dragThreshold,
  groupRect,
  isDrag,
  overlaps,
  placeAvoiding,
  removeOverlaps,
  type Rect,
} from './geometry'
import { isEmptyView, LayoutError, parseLayout, serializeLayout, viewKey, emptyView } from './layoutDoc'
import { branchRoute, curve, elbow, routeLine, sides, smoothRoute, spreadPorts, type Port } from './route'
import type { Box, Scene, SceneEdge } from './types'

// ---------- layout document ----------

describe('parseLayout', () => {
  const doc = {
    layout_version: 1,
    repository: 'gitview',
    views: {
      repository: {
        algorithm: 'grouped',
        edges: 'orthogonal',
        expanded: ['module:a', 'module:a', 42],
        nodes: { 'package:a': { x: 320, y: 180, locked: true }, 'package:b': { x: 1.234, y: -5 } },
      },
      'scope:package:a': { algorithm: 'force', edges: 'smooth', nodes: {} },
    },
  }

  it('reads a whole document', () => {
    const r = parseLayout(JSON.stringify(doc))
    expect(r.single).toBeNull()
    expect(r.positions).toBe(2)
    expect(r.doc.views.repository.nodes['package:a']).toEqual({ x: 320, y: 180, locked: true })
    expect(r.doc.views.repository.nodes['package:b']).toEqual({ x: 1.2, y: -5 })
    expect(r.doc.views.repository.expanded).toEqual(['module:a'])
    expect(r.doc.views['scope:package:a'].algorithm).toBe('force')
    expect(r.doc.views['scope:package:a'].edges).toBe('smooth')
  })

  it('reads the shape of a single view', () => {
    const r = parseLayout({ layout_version: 1, view: 'focus:func:x', nodes: { 'func:x': { x: 1, y: 2 } } })
    expect(r.single).toBe('focus:func:x')
    expect(r.doc.views['focus:func:x'].nodes['func:x']).toEqual({ x: 1, y: 2 })
    expect(r.doc.views['focus:func:x'].algorithm).toBe('grouped')
  })

  it('counts unknown IDs and keeps them', () => {
    const r = parseLayout(doc, (id) => id === 'package:a')
    expect(r.unknown).toBe(1)
    expect(Object.keys(r.doc.views.repository.nodes)).toHaveLength(2)
  })

  it('ignores invalid positions and unknown options with warnings', () => {
    const r = parseLayout({
      layout_version: 1,
      views: { repository: { algorithm: 'magic', edges: 'zigzag', nodes: { a: { x: 'no' }, b: null, c: { x: 1, y: 2 } } } },
    })
    expect(r.positions).toBe(1)
    expect(r.doc.views.repository.algorithm).toBe('grouped')
    expect(r.doc.views.repository.edges).toBe('orthogonal')
    expect(r.warnings.length).toBe(3)
  })

  it('clamps huge coordinates', () => {
    const r = parseLayout({ layout_version: 1, view: 'repository', nodes: { a: { x: 1e300, y: -1e300 } } })
    expect(r.doc.views.repository.nodes.a).toEqual({ x: 1e7, y: -1e7 })
  })

  it('rejects what is not a layout document', () => {
    expect(() => parseLayout('not json')).toThrow(LayoutError)
    expect(() => parseLayout('[]')).toThrow(LayoutError)
    expect(() => parseLayout({ hello: 1 })).toThrow(/layout_version/)
    expect(() => parseLayout({ layout_version: 2, views: {} })).toThrow(/unsupported/)
    expect(() => parseLayout({ layout_version: 1 })).toThrow(LayoutError)
    expect(() => parseLayout({ layout_version: 1, views: { r: { nodes: [] } } })).toThrow(/nodes/)
  })

  it('never takes code: a document is data', () => {
    const r = parseLayout('{"layout_version":1,"view":"repository","nodes":{"__proto__":{"x":1,"y":2},"constructor":{"x":3,"y":4}}}')
    const nodes = r.doc.views.repository.nodes as unknown as Record<string, unknown>
    expect(Object.getPrototypeOf(nodes)).toBe(Object.prototype)
    expect(nodes.x).toBeUndefined()
    expect(r.positions).toBe(1)
    expect(r.doc.views.repository.nodes.constructor).toEqual({ x: 3, y: 4 })
  })
})

describe('serializeLayout', () => {
  it('writes a stable, rounded document without empty views', () => {
    const r = parseLayout({
      layout_version: 1,
      repository: 'r',
      views: {
        z: { nodes: { b: { x: 1.6, y: 2.4 }, a: { x: 0, y: 0, locked: true } } },
        a: { algorithm: 'grouped', edges: 'orthogonal', nodes: {} },
      },
    })
    const text = serializeLayout(r.doc, false)
    expect(text).toBe(
      '{"layout_version":1,"repository":"r","views":{"z":{"algorithm":"grouped","edges":"orthogonal","nodes":{"a":{"x":0,"y":0,"locked":true},"b":{"x":2,"y":2}}}}}',
    )
    // a round trip gives the same document
    expect(serializeLayout(parseLayout(text).doc, false)).toBe(text)
  })

  it('knows an empty view', () => {
    expect(isEmptyView(undefined)).toBe(true)
    expect(isEmptyView(emptyView())).toBe(true)
    expect(isEmptyView({ ...emptyView(), algorithm: 'force' })).toBe(false)
  })

  it('names views', () => {
    expect(viewKey(null, null)).toBe('repository')
    expect(viewKey('package:a', null)).toBe('scope:package:a')
    expect(viewKey('package:a', 'func:f')).toBe('focus:func:f')
  })
})

// ---------- geometry ----------

describe('click or drag', () => {
  it('needs more movement from a finger than from a mouse', () => {
    expect(dragThreshold('mouse')).toBeLessThan(dragThreshold('touch'))
    expect(isDrag(3, 0, 'mouse')).toBe(false)
    expect(isDrag(5, 0, 'mouse')).toBe(true)
    expect(isDrag(6, 0, 'touch')).toBe(false)
    expect(isDrag(9, 0, 'touch')).toBe(true)
  })
})

describe('align and distribute', () => {
  const rs: Rect[] = [
    { x: 10, y: 0, w: 100, h: 40 },
    { x: 50, y: 100, w: 60, h: 40 },
    { x: 300, y: 200, w: 20, h: 40 },
  ]
  it('aligns left, centre and right', () => {
    expect(align(rs, 'left').map((p) => p.x)).toEqual([10, 10, 10])
    expect(align(rs, 'right').map((p) => p.x + 0)).toEqual([220, 260, 300])
    const c = align(rs, 'center')
    rs.forEach((r, k) => expect(c[k].x + r.w / 2).toBe(165))
    // y never changes when aligning horizontally
    expect(align(rs, 'left').map((p) => p.y)).toEqual([0, 100, 200])
  })
  it('distributes with equal gaps, the ends staying', () => {
    const d = distribute(rs, 'h')
    expect(d[0].x).toBe(10)
    expect(d[2].x).toBe(300)
    const gap1 = d[1].x - (d[0].x + 100),
      gap2 = d[2].x - (d[1].x + 60)
    expect(gap1).toBeCloseTo(gap2)
    const v = distribute(rs, 'v')
    expect(v[1].y - (v[0].y + 40)).toBeCloseTo(v[2].y - (v[1].y + 40))
    expect(distribute(rs.slice(0, 2), 'h')).toEqual([
      { x: 10, y: 0 },
      { x: 50, y: 100 },
    ])
  })
})

describe('free space and overlaps', () => {
  it('places new cards clear of the fixed ones', () => {
    const fixed = [{ x: 0, y: 0, w: 100, h: 100 }]
    const free = [
      { x: 50, y: 50, w: 80, h: 40 },
      { x: 60, y: 60, w: 80, h: 40 },
    ]
    const out = placeAvoiding(fixed, free, 10)
    const placed = out.map((p, k) => ({ ...p, w: free[k].w, h: free[k].h }))
    expect(overlaps(placed[0], fixed[0])).toBe(false)
    expect(overlaps(placed[1], fixed[0])).toBe(false)
    expect(overlaps(placed[0], placed[1])).toBe(false)
  })
  it('separates overlapping rectangles', () => {
    const rs = [
      { x: 0, y: 0, w: 100, h: 50 },
      { x: 10, y: 10, w: 100, h: 50 },
      { x: 20, y: 5, w: 100, h: 50 },
    ]
    removeOverlaps(rs, 8)
    for (let i = 0; i < rs.length; i++) for (let j = i + 1; j < rs.length; j++) expect(overlaps(rs[i], rs[j])).toBe(false)
  })
  it('grows a group around children moved out, never below its own size', () => {
    const g = groupRect({ x: 0, y: 0 }, { w: 200, h: 100 }, [{ x: 20, y: 50, w: 50, h: 30 }], PAD)
    expect(g).toEqual({ x: 0, y: 0, w: 200, h: 100 })
    const g2 = groupRect({ x: 0, y: 0 }, { w: 200, h: 100 }, [{ x: 300, y: -80, w: 50, h: 30 }], PAD)
    expect(g2.x).toBe(0)
    expect(g2.y).toBe(-80 - PAD.top)
    expect(g2.x + g2.w).toBe(350 + PAD.right)
    expect(g2.y + g2.h).toBe(100)
  })
})

// ---------- routing ----------

describe('edge router', () => {
  const A = { x: 0, y: 0, w: 100, h: 50 }
  it('leaves across the widest gap', () => {
    expect(sides(A, { x: 0, y: 200, w: 100, h: 50 })).toEqual(['b', 't'])
    expect(sides(A, { x: 0, y: -200, w: 100, h: 50 })).toEqual(['t', 'b'])
    expect(sides(A, { x: 300, y: 0, w: 100, h: 50 })).toEqual(['r', 'l'])
    expect(sides(A, { x: -300, y: 10, w: 100, h: 50 })).toEqual(['l', 'r'])
    // both gaps clear: the flow's axis wins unless the other gap is far larger
    expect(sides(A, { x: 200, y: 150, w: 100, h: 50 }, 'v')).toEqual(['b', 't'])
    expect(sides(A, { x: 200, y: 150, w: 100, h: 50 }, 'h')).toEqual(['r', 'l'])
  })
  it('spreads ports along a side in the order of their lines', () => {
    const got: Record<string, Port> = {}
    spreadPorts([
      { rect: A, side: 'b', along: 500, set: (p) => (got.far = p) },
      { rect: A, side: 'b', along: -500, set: (p) => (got.near = p) },
    ])
    expect(got.near.x).toBeLessThan(got.far.x)
    expect(got.near.y).toBe(50)
    expect(got.near.x).toBeGreaterThan(0)
    expect(got.far.x).toBeLessThan(100)
  })
  it('draws orthogonal elbows', () => {
    const p = elbow({ x: 0, y: 0, side: 'b' }, { x: 100, y: 100, side: 't' })
    expect(p).toEqual([0, 0, 0, 50, 100, 50, 100, 100])
    for (let k = 0; k < p.length - 2; k += 2) expect(p[k] === p[k + 2] || p[k + 1] === p[k + 3]).toBe(true)
    // aligned ports: a straight segment
    expect(elbow({ x: 5, y: 0, side: 'b' }, { x: 5, y: 80, side: 't' })).toEqual([5, 0, 5, 80])
  })
  it('draws curves and straight lines between the ports', () => {
    const a: Port = { x: 0, y: 0, side: 'r' },
      b: Port = { x: 200, y: 80, side: 'l' }
    const c = curve(a, b)
    expect(c.slice(0, 2)).toEqual([0, 0])
    expect(c.slice(-2)).toEqual([200, 80])
    expect(c[2]).toBeGreaterThan(0) // leaves to the right
    expect(routeLine(a, b, 'straight')).toEqual([0, 0, 200, 80])
  })
  it('smooths a route keeping its ends', () => {
    const p = [0, 0, 0, 50, 100, 50, 100, 100]
    const s = smoothRoute(p)
    expect(s.slice(0, 2)).toEqual([0, 0])
    expect(s.slice(-2)).toEqual([100, 100])
    expect(s.length).toBeGreaterThan(p.length)
  })
  it('routes a branch from an inner card to where its trunk leaves the group', () => {
    const G = { x: 0, y: 0, w: 300, h: 200 },
      R = { x: 20, y: 50, w: 100, h: 40 }
    const pts = branchRoute([150, 200], R, G, 0, 'orthogonal')
    expect(pts.slice(0, 2)).toEqual([70, 90]) // bottom of the inner card
    expect(pts.slice(-2)).toEqual([150, 200])
  })
})

// ---------- scene editor ----------

function scene(): Scene {
  const group: Box = {
    key: 'tG',
    id: 'package:g',
    kind: 'group',
    x: 0,
    y: 0,
    w: 300,
    h: 200,
    fx: 0,
    fy: 0,
    minW: 300,
    minH: 200,
    depth: 1,
    title: 'g',
    sub: '',
    color: 'k-package',
  }
  const a: Box = { key: 'tA', id: 'file:a', kind: 'node', x: 20, y: 50, w: 100, h: 40, parent: 'tG', depth: 2, title: 'a', sub: '', color: 'k-file' }
  const b: Box = { key: 'tB', id: 'file:b', kind: 'node', x: 160, y: 130, w: 100, h: 40, parent: 'tG', depth: 2, title: 'b', sub: '', color: 'k-file' }
  const c: Box = { key: 'tC', id: 'package:c', kind: 'node', x: 500, y: 0, w: 100, h: 40, depth: 1, title: 'c', sub: '', color: 'k-package' }
  const agg = { n: 1, w: 1, types: { calls: 1 }, confs: { confirmed: 1 }, best: 'confirmed' as const, members: [] }
  const inner: SceneEdge = { key: 'T:ab', s: 'tA', t: 'tB', pts: [70, 90, 70, 110, 210, 110, 210, 130], conf: 'confirmed', n: 1, agg }
  const out: SceneEdge = { key: 'T:Gc', s: 'tG', t: 'tC', pts: [], local: true, conf: 'confirmed', n: 1, agg }
  const br: SceneEdge = { key: 'B:aG', s: 'tA', t: 'tG', trunk: out, end: 0, branch: true, pts: [], conf: 'confirmed', n: 1, agg }
  return {
    boxes: [group, a, b, c],
    edges: [inner, out, br],
    bounds: { x: 0, y: 0, w: 600, h: 200 },
    style: 'orthogonal',
    prefer: 'v',
  }
}

describe('SceneEditor', () => {
  it('routes local lines and branches of a new scene', () => {
    const s = scene()
    new SceneEditor(s).routeAll()
    const out = s.edges[1]
    expect(out.pts.length).toBeGreaterThanOrEqual(4)
    // leaves the group on its right, enters the card on its left
    expect(out.pts[0]).toBe(300)
    expect(out.pts[out.pts.length - 2]).toBe(500)
    const br = s.edges[2]
    expect(br.pts.slice(-2)).toEqual(out.pts.slice(0, 2))
  })

  it('moves a group with its content; lines inside are translated', () => {
    const s = scene()
    const ed = new SceneEditor(s)
    ed.routeAll()
    const inner = s.edges[0].pts.slice()
    const moved = ed.move(['tG'], 40, 10)
    expect(moved.map((b) => b.key)).toEqual(['tG'])
    expect([s.boxes[0].x, s.boxes[0].fx, s.boxes[1].x, s.boxes[2].y]).toEqual([40, 40, 60, 140])
    expect(s.edges[0].pts).toEqual(inner.map((v, k) => v + (k % 2 ? 10 : 40)))
    // the line to the outside card follows: it now starts at the moved group's side
    expect(s.edges[1].pts[0]).toBe(340)
    // positions are saved relative to the group (unchanged for its content)
    expect(relativePosition(ed, s.boxes[1])).toEqual({ x: 20, y: 50 })
    expect(relativePosition(ed, s.boxes[0])).toEqual({ x: 40, y: 10 })
  })

  it('grows the group when a child leaves it, and shrinks it back', () => {
    const s = scene()
    const ed = new SceneEditor(s)
    ed.routeAll()
    ed.move(['tB'], 200, 100)
    const g = s.boxes[0]
    expect(g.x + g.w).toBe(360 + 100 + PAD.right)
    expect(g.y + g.h).toBe(230 + 40 + PAD.bottom)
    // the line between the two children was routed again to the new place
    const p = s.edges[0].pts
    expect(p[p.length - 1]).toBe(230)
    ed.move(['tB'], -200, -100)
    expect([g.x, g.y, g.w, g.h]).toEqual([0, 0, 300, 200])
  })

  it('moves only the topmost of a selection, and never a locked card', () => {
    const s = scene()
    const ed = new SceneEditor(s)
    ed.routeAll()
    s.boxes[3].locked = true
    const moved = ed.move(['tG', 'tA', 'tC'], 10, 0)
    expect(moved.map((b) => b.key)).toEqual(['tG'])
    expect(s.boxes[1].x).toBe(30) // moved once, with its group
    expect(s.boxes[3].x).toBe(500)
  })

  it('updates the bounds', () => {
    const s = scene()
    const ed = new SceneEditor(s)
    ed.move(['tC'], 100, 300)
    expect(ed.bounds()).toEqual({ x: 0, y: 0, w: 700, h: 340 })
  })
})
