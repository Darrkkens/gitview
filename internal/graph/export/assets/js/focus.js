// Focus mode: one element and what it is connected to, as a left-to-right
// flow (what depends on it on the left, what it depends on on the right),
// with the depth set by the viewer.
//   symbol            callers and callees (types: implementations, embeds,
//                     type uses and methods), from the dataset's edges
//   package · folder  packages (directories collapse to their packages)
//   file              files
// Each column holds one distance; a column keeps its strongest relations
// first and says how many more there are.
(function (GV) {
  "use strict";
  const D = GV.data, T = D.T, N = D.N, E = D.E;
  const PER_COL = 36;
  const TYPE_RELS = { implements: 1, embeds: 1, references: 1, method: 1 };

  function relevant(e, center, S) {
    const f = S.filters;
    if (f.rels[e.type] === false && !(e.type === "method" && D.TYPE[center.kind])) return false;
    if (f.confs[e.conf] === false) return false;
    if (center.n && D.SYMBOL[center.kind]) {
      if (D.TYPE[center.kind]) return !!TYPE_RELS[e.type];
      return !!D.FLOW[e.type] || (e.type === "references" && S.focus.refs);
    }
    return !D.STRUCTURAL[e.type];
  }

  // unitFn: how a node is represented in the focus graph.
  function unitFn(center) {
    const c = center.i;
    if (center.n && D.SYMBOL[center.kind]) return (x) => x;
    const level = center.kind === "file" ? "file" : "package";
    return (x) => {
      if (x === c || D.isAncestor(c, x)) return c;
      for (let p = x; p >= 0; p = T[p].parent) if (T[p].kind === level || (level === "file" && T[p].kind === "package")) return p;
      return -1;
    };
  }

  async function build(S, aspect) {
    const center = T[S.focus.i];
    const unit = unitFn(center);
    const f = S.filters;
    // unit graph (lazily, only around the center)
    const symbolMode = !!(center.n && D.SYMBOL[center.kind]);
    function neighbours(u, dir) {
      // edges of every node the unit stands for
      const list = new Map();
      for (const x of memberNodes(u)) {
        for (const ei of dir > 0 ? N[x].out : N[x].inn) {
          const e = E[ei];
          if (!relevant(e, center, S)) continue;
          const o = unit(dir > 0 ? e.t : e.s);
          if (o < 0 || o === u) continue;
          const ot = T[o];
          if (o !== center.i && !GV.arch.shown(ot, f)) continue;
          const cur = list.get(o);
          if (!cur || D.CONF_RANK[e.conf] > D.CONF_RANK[cur]) list.set(o, e.conf);
        }
      }
      return list;
    }
    const memberCache = new Map();
    function memberNodes(u) {
      if (memberCache.has(u)) return memberCache.get(u);
      const out = [];
      if (T[u].n) out.push(u);
      if (!symbolMode) D.descendants(u, (d) => { if (T[d].n) out.push(d); });
      memberCache.set(u, out);
      return out;
    }
    const col = new Map([[center.i, 0]]);
    const cols = { 0: [center.i] }, more = {};
    function walk(dir, depth) {
      let frontier = [center.i];
      for (let d = 1; d <= depth && frontier.length; d++) {
        const next = new Map();
        for (const u of frontier) neighbours(u, dir).forEach((conf, o) => {
          if (col.has(o)) return;
          const c = next.get(o);
          if (!c || D.CONF_RANK[conf] > D.CONF_RANK[c]) next.set(o, conf);
        });
        let list = Array.from(next.keys()).sort((a, b) => D.CONF_RANK[next.get(b)] - D.CONF_RANK[next.get(a)] || (T[a].label < T[b].label ? -1 : 1));
        if (list.length > PER_COL) { more[dir * d] = list.length - PER_COL; list = list.slice(0, PER_COL); }
        list.forEach((o) => col.set(o, dir * d));
        cols[dir * d] = list;
        frontier = list;
      }
    }
    walk(-1, S.focus.up);
    walk(1, S.focus.down);

    // every relevant relation between drawn units, aggregated per pair
    const agg = new Map();
    col.forEach((c, u) => {
      for (const x of memberNodes(u)) for (const ei of N[x].out) {
        const e = E[ei];
        if (!relevant(e, center, S)) continue;
        const o = unit(e.t);
        if (o < 0 || o === u || !col.has(o)) continue;
        const key = "f" + u + ">f" + o;
        let g = agg.get(key);
        if (!g) { g = { key, a: u, b: o, n: 0, w: 0, types: {}, confs: {}, best: "unresolved", real: [] }; agg.set(key, g); }
        g.n++; g.w += e.w;
        g.types[e.type] = (g.types[e.type] || 0) + 1;
        g.confs[e.conf] = (g.confs[e.conf] || 0) + 1;
        if (D.CONF_RANK[e.conf] > D.CONF_RANK[g.best]) g.best = e.conf;
        g.real.push(e.i);
      }
    });

    const minCol = -S.focus.up;
    const nodes = [], sizes = new Map();
    Object.keys(cols).map(Number).sort((a, b) => a - b).forEach((c) => {
      cols[c].forEach((u) => {
        const s = GV.arch.leafSize(T[u], false, true);
        sizes.set("f" + u, s);
        nodes.push({ id: "f" + u, width: s.w, height: s.h, c });
      });
      if (more[c]) { sizes.set("m" + c, { w: 150, h: 34 }); nodes.push({ id: "m" + c, width: 150, height: 34, c }); }
    });
    const edges = Array.from(agg.values()).map((g) => ({ id: g.key, s: "f" + g.a, t: "f" + g.b }));
    const key = "focus|" + T[S.focus.i].id + "|" + nodes.map((n) => n.id + ":" + n.c).join(",") + "|" + edges.map((e) => e.id).join(",");
    const res = await GV.layout.runPartitioned(key, nodes, edges, minCol, aspect);

    const boxes = nodes.map((nd) => {
      const p = res.pos[nd.id] || { x: 0, y: 0 }, s = sizes.get(nd.id);
      if (nd.id.charAt(0) === "m") {
        const d = Number(nd.id.slice(1));
        return { key: nd.id, kind: "more", x: p.x, y: p.y, w: s.w, h: s.h, depth: 0, title: "+" + more[d] + " more", sub: "", color: "k-group",
          tip: more[d] + " more at distance " + Math.abs(d) + " (strongest relations are drawn)" };
      }
      const u = Number(nd.id.slice(1)), t = T[u];
      return { key: nd.id, ref: u, kind: "node", x: p.x, y: p.y, w: s.w, h: s.h, depth: t.depth, title: D.displayName(t), sub: D.subtitle(t),
        color: D.kindColor(t), tip: D.reference(t), focusCol: nd.c };
    });
    const sceneEdges = Array.from(agg.values()).map((g) => ({ key: g.key, s: "f" + g.a, t: "f" + g.b, conf: g.best, n: g.n, label: GV.arch.summary(g), pts: res.edges[g.key] || null, agg: g }));
    const byKey = new Map(boxes.map((b) => [b.key, b]));
    sceneEdges.forEach((e) => {
      if (e.pts) return;
      const A = byKey.get(e.s), B = byKey.get(e.t);
      const ax = A.x + A.w, ay = A.y + A.h / 2, bx = B.x, by = B.y + B.h / 2, mx = (ax + bx) / 2;
      e.pts = [ax, ay, mx, ay, mx, by, bx, by];
    });
    let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
    boxes.forEach((b) => { x0 = Math.min(x0, b.x); y0 = Math.min(y0, b.y); x1 = Math.max(x1, b.x + b.w); y1 = Math.max(y1, b.y + b.h); });
    const out = new Map(), inn = new Map();
    sceneEdges.forEach((e) => {
      if (!out.has(e.s)) out.set(e.s, []);
      if (!inn.has(e.t)) inn.set(e.t, []);
      out.get(e.s).push(e); inn.get(e.t).push(e);
    });
    let callers = 0, callees = 0;
    col.forEach((c) => { if (c < 0) callers++; else if (c > 0) callees++; });
    return {
      kind: "focus", center: center.i, edges: sceneEdges, out, inn, more,
      scene: { boxes, edges: sceneEdges, bounds: { x: x0, y: y0, w: x1 - x0, h: y1 - y0 } },
      counts: { boxes: boxes.length, callers, callees, edges: sceneEdges.length },
      keyOf: (i) => col.has(i) ? "f" + i : null, symbolMode
    };
  }

  GV.focus = { build, PER_COL };
})(GV);
