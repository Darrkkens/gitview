// The Security Journey's graph: the path from a route to the finding (route,
// middleware chain, handler, the calls down to the sensitive operation),
// top to bottom, coloured by what the analysis found and always labelled
// with the state in text. For a route: every finding reaching it plus its
// handler chain.

import { baseName } from '../../lib/format'
import type { Graph } from '../http/graph'
import type { Box, Edge, Scene } from '../http/journey/Canvas'
import { boundsOf, font, layoutColumns, textWidth, type LEdge, type LNode } from '../http/journey/layout'
import { SEV_RANK, type Sec } from './model'
import type { Finding, PathStep } from './types'

export interface SecView {
  scene: Scene
  findings: Finding[]
  root: string | null
  keyOf: (i: number) => string | null
  ms: number
}

interface SNode {
  key: string
  ref?: number
  id?: string
  name: string
  role?: string
  file?: string
  line?: number
  check: string
  depth: number
  sinks: Finding[]
  finding?: Finding
  unresolved?: boolean
}

interface SEdge {
  key: string
  s: string
  t: string
  conf: string
  label: string
  color?: string
}

function chainSteps(g: Graph | null, routeId: string): PathStep[] | null {
  const k = g?.routeById.get(routeId)
  if (!g || k === undefined) return null
  const rt = g.routes[k]
  const out: PathStep[] = [{ id: rt.id, name: rt.key, role: 'route', file: rt.file, line: rt.line }]
  for (const m of rt.middlewares ?? [])
    out.push({ id: m.id ?? '', name: m.name, role: 'middleware', file: m.file, line: m.line, conf: m.confidence })
  if (rt.handler)
    out.push({
      id: rt.handler.id ?? '',
      name: rt.handler.name,
      role: 'handler',
      file: rt.handler.file,
      line: rt.handler.line,
      conf: rt.handler.confidence,
    })
  return out
}

type Look = Pick<Box, 'color' | 'sub' | 'chip' | 'chipColor' | 'findingId' | 'tip' | 'muted'>

/** look: colour, subtitle and state chip of a box. */
function look(sec: Sec, n: SNode): Look {
  if (n.finding) {
    const st = sec.stateOf(n.finding)
    return {
      color: st.token,
      sub: st.label + ' · ' + n.finding.severity,
      chip: n.finding.severity,
      chipColor: 'sev-' + n.finding.severity,
      findingId: n.finding.id,
      tip: n.finding.verdictLabel,
    }
  }
  if (n.unresolved)
    return {
      color: 's-unres',
      sub: 'not followed by the analysis',
      chip: 'unresolved',
      chipColor: 's-unres',
      muted: true,
    }
  let token = 's-struct',
    chip = ''
  const sub = (n.role || '') + (n.file ? ' · ' + baseName(n.file) + (n.line ? ':' + n.line : '') : '')
  if (n.sinks.length) {
    let worst = n.sinks[0]
    for (const f of n.sinks) if (SEV_RANK[f.severity] > SEV_RANK[worst.severity]) worst = f
    const st = sec.stateOf(worst)
    token = st.token
    chip = st.label.toLowerCase()
  } else if (/authentication|authorization/.test(n.check)) {
    token = 's-check'
    chip = '✓ ' + n.check.replace(' (answers 403)', '')
  } else if (n.check) chip = n.check
  return { color: token, sub, chip, chipColor: token, tip: n.id || n.name }
}

export async function buildSecurity(
  sec: Sec,
  g: Graph | null,
  finding: string | null,
  route: string | null,
): Promise<SecView | null> {
  const paths: { steps: PathStep[]; f: Finding | null }[] = []
  let findings: Finding[] = []
  const f0 = finding ? sec.byId.get(finding) : undefined
  if (f0) {
    findings = [f0]
    for (const r of (f0.routes ?? []).slice(0, 6)) paths.push({ steps: r.path, f: f0 })
    if (!paths.length) {
      const st: PathStep = {
        id: f0.symbolId || 'file:' + f0.file,
        name: f0.symbol ? f0.symbol.name : f0.file || f0.title,
        role: 'sink',
        file: f0.file,
        line: f0.line,
      }
      paths.push({ steps: [st], f: f0 })
    }
  } else if (route) {
    findings = sec.F.filter((f) => (f.routes ?? []).some((r) => r.id === route))
    for (const f of findings)
      for (const r of (f.routes ?? []).filter((x) => x.id === route)) paths.push({ steps: r.path, f })
    const chain = chainSteps(g, route)
    if (chain) paths.push({ steps: chain, f: null })
    if (!paths.length) {
      const post = sec.postureOf.get(route)
      if (!post) return null
      paths.push({ steps: [{ id: route, name: post.key, role: 'route' }], f: null })
    }
  } else return null

  const nodes = new Map<string, SNode>(),
    edges = new Map<string, SEdge>()
  const refOf = (st: PathStep) => (st.id && g?.byId.has(st.id) ? g.byId.get(st.id)! : undefined)
  const keyOfStep = (st: PathStep) => {
    const i = refOf(st)
    return i !== undefined ? 's' + i : 'x' + (st.id || st.name)
  }
  for (const p of paths) {
    let prev: SNode | null = null
    p.steps.forEach((st, i) => {
      const key = keyOfStep(st)
      let n = nodes.get(key)
      if (!n) {
        n = {
          key,
          ref: refOf(st),
          id: st.id,
          name: st.name,
          role: st.role,
          file: st.file,
          line: st.line,
          check: '',
          depth: i,
          sinks: [],
        }
        nodes.set(key, n)
      }
      n.depth = Math.min(n.depth, i)
      if (st.check && !n.check) n.check = st.check
      if (n.role !== 'route' && st.role === 'sink') n.role = 'sink'
      if (p.f && i === p.steps.length - 1 && !n.sinks.includes(p.f)) n.sinks.push(p.f)
      if (prev) {
        const chain = prev.role === 'route' || prev.role === 'middleware'
        const ek = prev.key + '>' + key
        if (!edges.has(ek))
          edges.set(ek, {
            key: ek,
            s: prev.key,
            t: key,
            conf: chain
              ? 'chain'
              : st.conf === 'possible' || st.conf === 'likely' || st.conf === 'unresolved'
                ? st.conf
                : 'confirmed',
            label: chain ? 'next in chain' : 'calls',
          })
      }
      prev = n
    })
  }
  // one box per finding, under its sensitive step
  for (const f of findings) {
    const sink = [...nodes.values()].find((n) => n.sinks.includes(f))
    if (!sink) continue
    const st = sec.stateOf(f)
    nodes.set('F' + f.id, { key: 'F' + f.id, finding: f, name: f.title, depth: sink.depth + 1, check: '', sinks: [] })
    edges.set(sink.key + '>F' + f.id, {
      key: sink.key + '>F' + f.id,
      s: sink.key,
      t: 'F' + f.id,
      conf: 'finding',
      color: st.token,
      label: f.ruleId,
    })
  }
  // unresolved calls of the route's chain, as a grey box
  const post = route ? sec.postureOf.get(route) : null
  if (post && post.unresolved) {
    const hd = [...nodes.values()].find((n) => n.role === 'handler')
    if (hd) {
      nodes.set('U', {
        key: 'U',
        name: post.unresolved + ' unresolved call(s)',
        depth: hd.depth + 1,
        unresolved: true,
        check: '',
        sinks: [],
      })
      edges.set(hd.key + '>U', {
        key: hd.key + '>U',
        s: hd.key,
        t: 'U',
        conf: 'unresolved',
        label: 'target not determinable statically',
      })
    }
  }
  const list = [...nodes.values()]
  const looks = new Map(list.map((n) => [n.key, look(sec, n)]))
  const width = (n: SNode) => {
    const l = looks.get(n.key)!
    const tw = textWidth(n.name || '', font(500, 12.5)) + 40
    // the subtitle and the state chip share the second line
    const sw = textWidth(l.sub ?? '', font(400, 11)) + 30 + (l.chip ? textWidth(l.chip, font(400, 11)) + 22 : 0)
    return Math.round(Math.max(170, Math.min(Math.max(tw, sw), 340)))
  }
  const lnodes: LNode[] = list.map((n) => ({ id: n.key, width: width(n), height: 46, c: n.depth }))
  const ledges: LEdge[] = [...edges.values()].map((e) => ({ id: e.key, s: e.s, t: e.t }))
  const key =
    'sec|' +
    (finding || route) +
    '|' +
    lnodes.map((n) => n.id + ':' + n.c + ':' + n.width).join(',') +
    '|' +
    ledges.map((e) => e.id).join(',')
  const res = await layoutColumns(key, lnodes, ledges, 'DOWN', 1.6)
  const boxes: Box[] = list.map((n) => {
    const p = res.pos[n.key] ?? { x: 0, y: 0 }
    return { key: n.key, x: p.x, y: p.y, w: width(n), h: 46, title: n.name, ref: n.ref, ...looks.get(n.key)! }
  })
  const byKey = new Map(boxes.map((b) => [b.key, b]))
  const sceneEdges: Edge[] = [...edges.values()].map((e) => {
    let pts = res.edges[e.key]
    if (!pts) {
      const A = byKey.get(e.s)!,
        B = byKey.get(e.t)!
      const ax = A.x + A.w / 2,
        ay = A.y + A.h,
        bx = B.x + B.w / 2,
        by = B.y,
        my = (ay + by) / 2
      pts = [ax, ay, ax, my, bx, my, bx, by]
    }
    return { key: e.key, s: e.s, t: e.t, conf: e.conf, color: e.color, n: 1, label: e.label, pts }
  })
  return {
    scene: { boxes, edges: sceneEdges, bounds: boundsOf(boxes) },
    findings,
    root: boxes[0]?.key ?? null,
    keyOf: (i: number) => (nodes.has('s' + i) ? 's' + i : null),
    ms: res.ms,
  }
}
