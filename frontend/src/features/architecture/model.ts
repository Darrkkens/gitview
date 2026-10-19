// The model: the compact dataset decoded once into nodes, edges and the
// exploration tree every part of the view shares.
//   N, E   nodes and edges as exported (static analysis results, unchanged)
//   T      the exploration tree: Repository → Module → directory → Package →
//          File → declaration (→ method, closure, call site). T[i] for
//          i < N.length is node N[i]; synthetic items (the repository,
//          directories, external groups) come after.
// Only the presentation is derived here: a directory item groups packages by
// import path, a method sits under its receiver type when both are in the
// same file, a database operation or unresolved call under the only function
// of its file that reaches it. Relations are never invented.

import { baseName, plural } from '../../lib/format'
import type { Community, Conf, GEdge, GNode, GraphSection, RouteRef, TItem } from './types'
import { FLOW, FUNC, KIND, STRUCTURAL, SYMBOL, TYPE } from './vocab'

export interface Relations {
  out: number[]
  inn: number[]
}

export interface Model {
  raw: GraphSection
  N: GNode[]
  E: GEdge[]
  T: TItem[]
  ROOT: number
  /** The "External dependencies" group (-1 when there is none). */
  EXT: number
  byId: Map<string, number>
  COMM: Map<number, Community>
  modules: GNode[]
  /** Tree items in depth-first order from the root (parents before children). */
  order: number[]
  ROUTES: RouteRef[]
  routeOfNode: Map<number, number>
  nodeOfRoute: number[]
  /** Element and relation kinds present, with their counts (for the filters). */
  presentKinds: Record<string, number>
  presentRels: Record<string, number>

  ancestors(i: number): number[]
  isAncestor(a: number, b: number): boolean
  childOf(c: number, x: number): number
  path(i: number): number[]
  packageOf(i: number): number
  descendants(i: number, fn: (d: number) => void): void
  kindLabel(t: TItem): string
  kindColor(t: TItem): string
  subtitle(t: TItem): string
  reference(t: TItem): string
  displayName(t: TItem, nested?: boolean): string
  relationsOf(i: number): Relations
  routesOf(i: number): number[]
}

const cache = new WeakMap<GraphSection, Model>()

/** modelOf decodes a graph section once (the same object returns the same model). */
export function modelOf(raw: GraphSection): Model {
  let m = cache.get(raw)
  if (!m) {
    m = build(raw)
    cache.set(raw, m)
  }
  return m
}

const byName = (a: { name: string }, b: { name: string }) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0)

function build(raw: GraphSection): Model {
  const K = raw.kinds,
    ET = raw.etypes,
    CF = raw.confs,
    ES = raw.esrc
  const N: GNode[] = raw.nodes.map((a, i) => ({
    i,
    id: a[0],
    type: K[a[1]],
    name: a[2] ?? '',
    qual: a[3] ?? '',
    pkg: a[4] ?? '',
    file: a[5] ?? '',
    line: a[6] || 0,
    end: a[7] || 0,
    sig: a[8] ?? '',
    ext: !!a[9],
    parent: a[10] ?? -1,
    group: a[11] ?? -1,
    comm: a[12] ?? -1,
    deg: a[13] || 0,
    meta: a[14] || {},
    out: [],
    inn: [],
  }))
  const E: GEdge[] = raw.edges.map((a, i) => {
    const e: GEdge = {
      i,
      s: a[0],
      t: a[1],
      type: ET[a[2]],
      conf: CF[a[3]] as Conf,
      w: a[4] || 1,
      file: a[5] ?? '',
      line: a[6] || 0,
      note: a[7] ?? '',
      meta: a[8] || {},
      src: ES[a[9]] ?? '',
    }
    N[e.s].out.push(i)
    N[e.t].inn.push(i)
    return e
  })
  const byId = new Map<string, number>()
  N.forEach((n) => byId.set(n.id, n.i))
  const COMM = new Map<number, Community>()
  for (const c of raw.communities ?? [])
    COMM.set(c[0], { id: c[0], level: c[1], label: c[2], size: c[3], members: c[4] })

  // ---------- tree ----------
  const T: TItem[] = N.map((n) => ({
    i: n.i,
    n,
    id: n.id,
    kind: n.type,
    name: n.name,
    label: n.name,
    parent: -1,
    kids: [],
    depth: 0,
    count: 0,
    nfiles: 0,
    npkgs: 0,
  }))
  function synth(id: string, kind: string, name: string, label?: string): number {
    const t: TItem = {
      i: T.length,
      n: null,
      id,
      kind,
      name,
      label: label || name,
      parent: -1,
      kids: [],
      depth: 0,
      count: 0,
      nfiles: 0,
      npkgs: 0,
    }
    T.push(t)
    byId.set(id, t.i)
    return t.i
  }
  const ROOT = synth('repo:' + raw.repository, 'repo', raw.repository)
  const setParent = (c: number, p: number) => {
    T[c].parent = p
  }

  // Modules, ordered by path.
  const modules = N.filter((n) => n.type === 'module').sort(byName)
  modules.forEach((m) => {
    setParent(m.i, ROOT)
    T[m.i].label = baseName(m.name) || m.name
  })
  const modOf = (pkgPath: string) => {
    let best: GNode | null = null
    for (const m of modules)
      if ((pkgPath === m.name || pkgPath.startsWith(m.name + '/')) && (!best || m.name.length > best.name.length))
        best = m
    return best
  }

  // Packages of each module, grouped by directory.
  interface Dir {
    name: string
    kids: Map<string, Dir>
    pkg: GNode | null
  }
  const byModule = new Map<number, GNode[]>()
  N.filter((n) => n.type === 'package' && !n.ext).forEach((p) => {
    const m = modOf(p.name)
    const key = m ? m.i : ROOT
    if (!byModule.has(key)) byModule.set(key, [])
    byModule.get(key)!.push(p)
  })
  byModule.forEach((pkgs, mi) => {
    const prefix = mi === ROOT ? '' : N[mi].name
    const trie: Dir = { name: '', kids: new Map(), pkg: null }
    pkgs.forEach((p) => {
      const rel = prefix && p.name.startsWith(prefix) ? p.name.slice(prefix.length).replace(/^\//, '') : p.name
      let node = trie
      if (rel)
        for (const seg of rel.split('/')) {
          if (!node.kids.has(seg)) node.kids.set(seg, { name: seg, kids: new Map(), pkg: null })
          node = node.kids.get(seg)!
        }
      node.pkg = p
      T[p.i].label = rel ? baseName(rel) : baseName(p.name) || p.name
    })
    const count = (d: Dir): number => (d.pkg ? 1 : 0) + Array.from(d.kids.values()).reduce((s, k) => s + count(k), 0)
    // place(d, parent, path): a directory with one package and nothing else
    // is that package; a directory chain with a single child is merged.
    function place(d: Dir, parent: number, path: string) {
      const kids = Array.from(d.kids.values()).sort(byName)
      if (!kids.length) {
        if (d.pkg) setParent(d.pkg.i, parent)
        return
      }
      if (!d.pkg && kids.length === 1) {
        place(kids[0], parent, path + kids[0].name + '/')
        return
      }
      const total = count(d)
      if (total === 1 && d.pkg) {
        setParent(d.pkg.i, parent)
        return
      }
      const g = synth('dir:' + (prefix || '') + ':' + path, 'dir', path, path)
      setParent(g, parent)
      if (d.pkg) setParent(d.pkg.i, g)
      kids.forEach((k) => place(k, g, k.name + '/'))
    }
    // The module root: its own package sits directly in the module.
    const top = mi === ROOT ? ROOT : mi
    if (trie.pkg) setParent(trie.pkg.i, top)
    Array.from(trie.kids.values())
      .sort(byName)
      .forEach((k) => place(k, top, k.name + '/'))
  })

  // External packages: standard library and third party.
  const extPkgs = N.filter((n) => n.type === 'package' && n.ext)
  let EXT = -1
  if (extPkgs.length) {
    EXT = synth('ext:', 'extgroup', 'External dependencies')
    setParent(EXT, ROOT)
    const sub = (id: string, name: string, pkgs: GNode[]) => {
      if (!pkgs.length) return
      const g = synth(id, 'dir', name)
      T[g].ext = true
      setParent(g, EXT)
      pkgs.forEach((p) => setParent(p.i, g))
    }
    sub(
      'ext:std',
      'Standard library',
      extPkgs.filter((p) => p.meta.std),
    )
    sub(
      'ext:third',
      'Third-party',
      extPkgs.filter((p) => !p.meta.std),
    )
  }

  // Declarations: the structural parent of the dataset, refined.
  const sameFile = (a: GNode, b: GNode) => !!a.file && a.file === b.file
  N.forEach((n) => {
    if (n.type === 'module' || n.type === 'package') return
    let p = n.parent
    if (n.type === 'method') {
      for (const ei of n.inn) {
        const e = E[ei]
        if (e.type === 'method' && sameFile(N[e.s], n)) {
          p = e.s
          break
        }
      }
    } else if (n.type === 'db' || n.type === 'unresolved') {
      const callers = new Set<number>()
      for (const ei of n.inn) {
        const e = E[ei]
        if (FLOW[e.type] && FUNC[N[e.s].type] && sameFile(N[e.s], n)) callers.add(e.s)
      }
      if (callers.size === 1) p = callers.values().next().value as number
      // a call site without a position sits under its only caller
      if (!n.file && p < 0) {
        const any = new Set(n.inn.filter((ei) => FLOW[E[ei].type]).map((ei) => E[ei].s))
        if (any.size === 1) p = any.values().next().value as number
      }
    }
    if (p < 0 && n.ext && n.group >= 0) p = n.group
    setParent(n.i, p >= 0 ? p : ROOT)
  })
  // Anything left without a parent hangs from the repository.
  T.forEach((t) => {
    if (t.i !== ROOT && t.parent < 0) setParent(t.i, ROOT)
  })
  T.forEach((t) => {
    if (t.parent >= 0) T[t.parent].kids.push(t.i)
  })
  // Inside its type a method reads M, inside its function a closure func1.
  T.forEach((t) => {
    if (!t.n || t.parent < 0) return
    const p = T[t.parent]
    if (t.kind === 'method' && TYPE[p.kind]) t.short = t.name.replace(/^\(.*?\)\./, '')
    else if (t.kind === 'closure' && p.n && t.name.startsWith(p.name + '.')) t.short = t.name.slice(p.name.length + 1)
  })

  // Order: directories and containers first, then source order inside a
  // file, names otherwise.
  const ord = (t: TItem) => (KIND[t.kind] ?? { order: 50 }).order
  T.forEach((t) => {
    const bySource = t.kind === 'file' || FUNC[t.kind] || TYPE[t.kind]
    t.kids.sort((a, b) => {
      const x = T[a],
        y = T[b]
      if (bySource) {
        const lx = x.n ? x.n.line || 0 : 0,
          ly = y.n ? y.n.line || 0 : 0
        if (lx !== ly) return lx - ly
      }
      const xl = x.label.toLowerCase(),
        yl = y.label.toLowerCase()
      return ord(x) - ord(y) || (xl < yl ? -1 : xl > yl ? 1 : a - b)
    })
  })
  // Depth (iterative: closures can nest deeply) and counts, bottom-up.
  const order: number[] = []
  const stack = [ROOT]
  while (stack.length) {
    const i = stack.pop()!
    order.push(i)
    for (const c of T[i].kids) {
      T[c].depth = T[i].depth + 1
      stack.push(c)
    }
  }
  for (let k = order.length - 1; k >= 0; k--) {
    const t = T[order[k]]
    if (t.n && SYMBOL[t.kind]) t.count += 1
    if (t.kind === 'file') t.nfiles += 1
    if (t.kind === 'package') t.npkgs += 1
    if (t.parent >= 0) {
      const p = T[t.parent]
      p.count += t.count
      p.nfiles += t.nfiles
      p.npkgs += t.npkgs
    }
  }

  function ancestors(i: number): number[] {
    const out: number[] = []
    for (let p = T[i].parent; p >= 0; p = T[p].parent) out.push(p)
    return out
  }
  // a is a strict ancestor of b
  function isAncestor(a: number, b: number): boolean {
    if (T[a].depth >= T[b].depth) return false
    for (let p = T[b].parent; p >= 0; p = T[p].parent) {
      if (p === a) return true
      if (T[p].depth <= T[a].depth) return false
    }
    return false
  }
  // childOf(c, x): the child of container c on the way down to x.
  function childOf(c: number, x: number): number {
    let y = x
    while (y >= 0 && T[y].parent !== c) y = T[y].parent
    return y
  }
  const path = (i: number) => ancestors(i).reverse().concat([i])
  function packageOf(i: number): number {
    for (let p = i; p >= 0; p = T[p].parent) if (T[p].kind === 'package') return p
    return -1
  }
  function descendants(i: number, fn: (d: number) => void) {
    const st = T[i].kids.slice()
    while (st.length) {
      const c = st.pop()!
      fn(c)
      for (const k of T[c].kids) st.push(k)
    }
  }

  // ---------- labels ----------
  const kindLabel = (t: TItem) => (KIND[t.kind] ?? { label: t.kind }).label
  function kindColor(t: TItem): string {
    if (t.kind === 'dir' && t.ext) return 'k-external'
    if (t.n && t.n.ext && t.kind === 'package') return 'k-external'
    return (KIND[t.kind] ?? { color: 'k-group' }).color
  }
  const stats = raw.stats
  // subtitle: one short line describing an item.
  function subtitle(t: TItem): string {
    const k = t.kind
    if (k === 'repo') return plural(modules.length, 'module') + ' · ' + plural(stats.packages, 'package')
    if (k === 'module') return 'module · ' + plural(t.npkgs, 'package') + ' · ' + plural(t.count, 'symbol')
    if (k === 'dir')
      return (
        (t.ext ? '' : 'directory · ') + plural(t.npkgs, 'package') + (t.ext ? '' : ' · ' + plural(t.count, 'symbol'))
      )
    if (k === 'extgroup') return plural(t.npkgs, 'package')
    if (k === 'package')
      return t.n!.ext
        ? (t.n!.meta.std ? 'standard library' : 'third-party') + ' · ' + plural(t.count, 'function') + ' used'
        : 'package · ' + plural(t.nfiles, 'file') + ' · ' + plural(t.count, 'symbol')
    if (k === 'file') return 'file · ' + plural(t.count, 'symbol')
    const n = t.n
    if (!n) return kindLabel(t)
    if (k === 'route') return 'route · ' + (n.meta.router || '') + (n.line ? ' · line ' + n.line : '')
    if (k === 'db') return (n.meta.lib || 'db') + ' ' + (n.meta.op || '') + (n.line ? ' · line ' + n.line : '')
    if (k === 'external') return 'external · ' + (n.pkg || '')
    if (k === 'unresolved') return 'unresolved' + (n.line ? ' · line ' + n.line : '')
    return kindLabel(t) + (n.line ? ' · line ' + n.line : '')
  }
  // reference: the fully qualified Go name of an item.
  function reference(t: TItem): string {
    const n = t.n
    if (!n) return t.kind === 'dir' ? t.name : t.kind === 'repo' ? raw.repository : t.id
    if (n.type === 'package' || n.type === 'module') return n.name
    if (n.type === 'file') return n.file
    if (n.type === 'route') return (n.meta.method || 'ANY') + ' ' + (n.meta.path || n.name)
    if (n.type === 'db' || n.type === 'unresolved') return n.file ? n.file + (n.line ? ':' + n.line : '') : n.name
    if (n.pkg && n.qual) {
      const i = n.qual.indexOf('.')
      return i >= 0 ? n.pkg + n.qual.slice(i) : n.pkg + '.' + n.qual
    }
    return n.qual || n.name
  }
  // displayName: nested is true where the parent is visible around the item
  // (tree, expanded boxes), so the receiver or function can be omitted.
  function displayName(t: TItem, nested?: boolean): string {
    if (t.kind === 'package') return t.n!.ext ? t.n!.name : t.label
    return nested && t.short ? t.short : t.label
  }

  // ---------- relations of an item ----------
  // For a symbol, its own relations; for a container, the relations of
  // everything inside it with the outside.
  const relCache = new Map<number, Relations>()
  function relationsOf(i: number): Relations {
    const hit = relCache.get(i)
    if (hit) return hit
    const t = T[i]
    const out: number[] = [],
      inn: number[] = []
    if (t.n && !t.kids.length) {
      t.n.out.forEach((ei) => {
        if (!STRUCTURAL[E[ei].type]) out.push(ei)
      })
      t.n.inn.forEach((ei) => {
        if (!STRUCTURAL[E[ei].type]) inn.push(ei)
      })
    } else {
      const inside = new Set<number>()
      if (t.n) inside.add(i)
      descendants(i, (d) => {
        if (T[d].n) inside.add(d)
      })
      inside.forEach((x) => {
        N[x].out.forEach((ei) => {
          const e = E[ei]
          if (!STRUCTURAL[e.type] && !inside.has(e.t)) out.push(ei)
        })
        N[x].inn.forEach((ei) => {
          const e = E[ei]
          if (!STRUCTURAL[e.type] && !inside.has(e.s)) inn.push(ei)
        })
      })
    }
    const r = { out, inn }
    relCache.set(i, r)
    return r
  }

  // ---------- HTTP catalog ----------
  const ROUTES = raw.http?.routes ?? []
  const routeOfNode = new Map<number, number>()
  const nodeOfRoute = ROUTES.map((r, k) => {
    const i = byId.get(r.id) ?? -1
    if (i >= 0) routeOfNode.set(i, k)
    return i
  })
  // routesOf: the routes registered in, or handled by, a container.
  function routesOf(p: number): number[] {
    const out: number[] = []
    ROUTES.forEach((r, k) => {
      const hn = r.handler?.id ? (byId.get(r.handler.id) ?? -1) : -1
      const rn = nodeOfRoute[k]
      if ((hn >= 0 && (hn === p || isAncestor(p, hn))) || (rn >= 0 && (rn === p || isAncestor(p, rn)))) out.push(k)
    })
    return out
  }

  const presentKinds: Record<string, number> = {},
    presentRels: Record<string, number> = {}
  N.forEach((n) => (presentKinds[n.type] = (presentKinds[n.type] || 0) + 1))
  E.forEach((e) => (presentRels[e.type] = (presentRels[e.type] || 0) + 1))

  return {
    raw,
    N,
    E,
    T,
    ROOT,
    EXT,
    byId,
    COMM,
    modules,
    order,
    ROUTES,
    routeOfNode,
    nodeOfRoute,
    presentKinds,
    presentRels,
    ancestors,
    isAncestor,
    childOf,
    path,
    packageOf,
    descendants,
    kindLabel,
    kindColor,
    subtitle,
    reference,
    displayName,
    relationsOf,
    routesOf,
  }
}

/** loc formats "file:line" of a node (empty without a file). */
export const nodeLoc = (n: GNode | null | undefined) => (n && n.file ? n.file + (n.line ? ':' + n.line : '') : '')

/** findingIdOf returns the finding an annotation comes from: its ref, or the
 *  ID the audit also writes into its details. */
export function findingIdOf(a: { ref?: string; details?: string }): string | null {
  if (a.ref?.startsWith('finding:')) return a.ref
  const details = a.details
  if (!details) return null
  for (const part of details.split(' · ')) if (/^finding:\S+$/.test(part.trim())) return part.trim()
  return null
}
