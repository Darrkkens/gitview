// Architecture view: the exploration tree drawn as nested boxes.
//
// What is drawn: the scope (a tree item, the repository by default) and
// its children; an expanded child shows its own children inside it, and so
// on. Every relation of the dataset is lifted to the drawn items: a call
// between two functions of collapsed packages is a line between the
// packages, counted with its kind and confidence. Elements outside the
// scope that relate to it are drawn around it as context, at the coarsest
// level that still tells them apart (siblings of the scope's ancestors).
//
// Layout runs per container, bottom-up: a container's children are laid
// out with the relations between them, then the container is a box of that
// size in its parent's layout. Each result is cached by content.
(function (GV) {
  "use strict";
  const D = GV.data, T = D.T, E = D.E;
  const PAD = { top: 42, left: 14, right: 14, bottom: 14 };
  const SHORT = { imports: "import", calls: "call", go: "goroutine", defer: "defer", handles: "handler", queries: "query",
    implements: "implements", embeds: "embeds", references: "type ref" };

  function shown(t, f) {
    if (t.kind === "extgroup" || t.ext || (t.n && t.n.ext)) { if (!f.ext) return false; }
    if (t.n && f.kinds[t.kind] === false) return false;
    return true;
  }
  const expandable = (t) => t.kids.length > 0;

  // visible computes which tree items are drawn and how every item maps to
  // a drawn one.
  function visible(S) {
    const f = S.filters, root = S.root;
    const vis = new Uint8Array(T.length);
    const groups = [];
    vis[root] = 1;
    const stack = [root];
    while (stack.length) {
      const c = stack.pop();
      groups.push(c);
      for (const k of T[c].kids) {
        if (!shown(T[k], f)) continue;
        vis[k] = 1;
        if (expandable(T[k]) && S.open.has(k)) stack.push(k);
      }
    }
    // unit: the drawn item standing for each item inside the scope;
    // ctx: the context item for an item outside it.
    const chain = new Set(D.ancestors(root).concat([root]));
    const unit = new Int32Array(T.length).fill(-1), ctx = new Int32Array(T.length).fill(-1);
    const inside = new Uint8Array(T.length);
    for (const i of D.order) {
      const p = T[i].parent;
      inside[i] = i === root || (p >= 0 && inside[p]) ? 1 : 0;
      if (inside[i]) unit[i] = vis[i] ? i : p >= 0 ? unit[p] : -1;
      else if (!chain.has(i)) ctx[i] = chain.has(p) ? (shown(T[i], f) ? i : -2) : p >= 0 ? ctx[p] : -1;
    }
    return { vis, groups, unit, ctx, inside, root };
  }

  // aggregate lifts the dataset's relations to drawn items.
  function aggregate(S, v) {
    const f = S.filters;
    const agg = new Map();
    for (const e of E) {
      if (D.STRUCTURAL[e.type] || f.rels[e.type] === false || f.confs[e.conf] === false) continue;
      let a = v.unit[e.s], b = v.unit[e.t], ca = false, cb = false;
      if (a < 0) { a = v.ctx[e.s]; ca = true; }
      if (b < 0) { b = v.ctx[e.t]; cb = true; }
      if (a < 0 || b < 0 || a === b || (ca && cb)) continue;
      if (!ca && !cb && (D.isAncestor(a, b) || D.isAncestor(b, a))) continue;
      const key = (ca ? "c" : "t") + a + ">" + (cb ? "c" : "t") + b;
      let g = agg.get(key);
      if (!g) {
        g = { key, a, b, ca, cb, n: 0, w: 0, types: {}, confs: {}, best: "unresolved", real: [] };
        agg.set(key, g);
      }
      g.n++; g.w += e.w;
      g.types[e.type] = (g.types[e.type] || 0) + 1;
      g.confs[e.conf] = (g.confs[e.conf] || 0) + 1;
      if (D.CONF_RANK[e.conf] > D.CONF_RANK[g.best]) g.best = e.conf;
      g.real.push(e.i);
    }
    return agg;
  }
  function summary(g) {
    return Object.keys(g.types).sort((x, y) => g.types[y] - g.types[x]).slice(0, 3)
      .map((k) => g.types[k] + " " + SHORT[k] + (g.types[k] > 1 && !/s$/.test(SHORT[k]) && k !== "implements" && k !== "embeds" ? "s" : "")).join(" · ");
  }

  // ---------- sizes ----------
  // leafSize: the box of an item drawn collapsed; flat when it is drawn
  // without its parent around it (focus mode).
  function leafSize(t, ctxNode, flat) {
    const title = D.displayName(t, !ctxNode && !flat), sub = ctxNode ? contextSub(t) : D.subtitle(t);
    const tw = GV.layout.textWidth(title, GV.layout.font(500, 12.5)) + 22 + (expandable(t) && !ctxNode ? 20 : 0) + 12;
    const sw = GV.layout.textWidth(sub, GV.layout.font(400, 11)) + 34;
    const symbol = t.n && D.SYMBOL[t.kind];
    return { w: Math.round(Math.max(symbol ? 140 : 160, Math.min(Math.max(tw, sw), symbol ? 280 : 300))), h: symbol ? 42 : 46 };
  }
  function contextSub(t) { return "outside the scope · " + D.kindLabel(t); }

  // ---------- build ----------
  // build returns a promise of the view: scene for the renderer, plus what
  // the inspector and the highlight need.
  async function build(S, aspect) {
    const v = visible(S);
    const agg = aggregate(S, v);
    const groupSet = new Set(v.groups);
    // children of each group that are drawn
    const kids = new Map();
    v.groups.forEach((c) => kids.set(c, T[c].kids.filter((k) => v.vis[k])));
    // layout pairs per container: relations lifted to direct children
    const pairs = new Map(); // container → Map(pairKey → {s, t})
    const ctxUnits = new Set();
    agg.forEach((g) => {
      if (g.ca || g.cb) { ctxUnits.add(g.ca ? g.a : g.b); return; }
      // lowest group containing both
      let c = T[g.a].parent;
      while (c >= 0 && !(groupSet.has(c) && (c === g.b || D.isAncestor(c, g.b)))) c = T[c].parent;
      if (c < 0) return;
      const sa = D.childOf(c, g.a), sb = D.childOf(c, g.b);
      if (sa < 0 || sb < 0 || sa === sb) return;
      if (!pairs.has(c)) pairs.set(c, new Map());
      const pk = "p" + sa + ">" + sb;
      pairs.get(c).set(pk, { id: pk, s: "t" + sa, t: "t" + sb });
    });
    // bottom-up layouts
    const size = new Map(), lay = new Map();
    const deepFirst = v.groups.slice().sort((a, b) => T[b].depth - T[a].depth);
    for (const c of deepFirst) {
      const ks = kids.get(c);
      const nodes = ks.map((k) => {
        const s = groupSet.has(k) ? size.get(k) : leafSize(T[k]);
        return { id: "t" + k, width: s.w, height: s.h };
      });
      const edges = pairs.has(c) ? Array.from(pairs.get(c).values()) : [];
      const asp = c === v.root ? Math.max(0.6, Math.min(3, Math.round(aspect * 10) / 10)) : 1.6;
      const key = T[c].id + "|" + asp + "|" + nodes.map((n) => n.id + ":" + n.width + "x" + n.height).join(",") + "|" + edges.map((e) => e.id).join(",");
      const res = await GV.layout.run(key, nodes, edges, "DOWN", asp);
      lay.set(c, res);
      const headW = GV.layout.textWidth(D.displayName(T[c], true), GV.layout.font(600, 12)) + 60;
      const w = Math.max(res.width + PAD.left + PAD.right, headW, 180);
      const h = Math.max(res.height, ks.length ? 0 : 24) + PAD.top + PAD.bottom;
      size.set(c, { w: Math.round(w), h: Math.round(h) });
    }
    // top level: the scope plus its context
    const top = [{ id: "t" + v.root, width: size.get(v.root).w, height: size.get(v.root).h }];
    const ctxList = Array.from(ctxUnits).sort((a, b) => T[a].depth - T[b].depth || a - b);
    ctxList.forEach((c) => { const s = leafSize(T[c], true); top.push({ id: "c" + c, width: s.w, height: s.h }); });
    const topEdges = new Map();
    agg.forEach((g) => {
      if (!(g.ca || g.cb)) return;
      const s = g.ca ? "c" + g.a : "t" + v.root, t = g.cb ? "c" + g.b : "t" + v.root;
      topEdges.set(s + ">" + t, { id: s + ">" + t, s, t });
    });
    const topRes = ctxList.length
      ? await GV.layout.run("top|" + T[v.root].id + "|" + top.map((n) => n.id + ":" + n.width + "x" + n.height).join(",") + "|" + Array.from(topEdges.keys()).join(","),
        top, Array.from(topEdges.values()), "DOWN", Math.max(0.6, Math.min(3, aspect)), { "elk.layered.spacing.nodeNodeBetweenLayers": "70", "elk.spacing.nodeNode": "28" })
      : { pos: { ["t" + v.root]: { x: 0, y: 0 } }, edges: {} };

    // absolute positions, top-down
    const boxes = [], abs = new Map(), origin = new Map();
    function placeGroup(c, x, y) {
      const s = size.get(c);
      abs.set(c, { x, y, w: s.w, h: s.h });
      const res = lay.get(c);
      const ox = x + PAD.left + Math.max(0, (s.w - PAD.left - PAD.right - res.width) / 2), oy = y + PAD.top;
      origin.set(c, { x: ox, y: oy });
      for (const k of kids.get(c)) {
        const p = res.pos["t" + k] || { x: 0, y: 0 };
        if (groupSet.has(k)) placeGroup(k, ox + p.x, oy + p.y);
        else { const ls = leafSize(T[k]); abs.set(k, { x: ox + p.x, y: oy + p.y, w: ls.w, h: ls.h }); }
      }
    }
    const rp = topRes.pos["t" + v.root];
    placeGroup(v.root, rp.x, rp.y);
    // boxes in drawing order: groups by depth, then leaves
    v.groups.slice().sort((a, b) => T[a].depth - T[b].depth).forEach((c) => {
      const r = abs.get(c), t = T[c];
      boxes.push({ key: "t" + c, ref: c, kind: "group", x: r.x, y: r.y, w: r.w, h: r.h, depth: t.depth, title: D.displayName(t, true), sub: D.subtitle(t),
        color: D.kindColor(t), count: "", scope: c === v.root, tip: D.reference(t) });
    });
    for (let i = 0; i < T.length; i++) {
      if (!v.vis[i] || groupSet.has(i)) continue;
      const r = abs.get(i), t = T[i];
      if (!r) continue;
      boxes.push({ key: "t" + i, ref: i, kind: "node", x: r.x, y: r.y, w: r.w, h: r.h, depth: t.depth, title: D.displayName(t, true), sub: D.subtitle(t),
        color: D.kindColor(t), chev: expandable(t) ? "closed" : null, tip: D.reference(t) });
    }
    ctxList.forEach((c) => {
      const p = topRes.pos["c" + c], s = leafSize(T[c], true), t = T[c];
      abs.set("c" + c, { x: p.x, y: p.y, w: s.w, h: s.h });
      boxes.push({ key: "c" + c, ref: c, kind: "context", x: p.x, y: p.y, w: s.w, h: s.h, depth: t.depth, title: D.displayName(t), sub: contextSub(t),
        color: D.kindColor(t), tip: D.reference(t) });
    });
    const rectOf = (key) => key.charAt(0) === "c" ? abs.get(key) : abs.get(Number(key.slice(1)));

    // Edges are bundled by the hierarchy. A relation between two drawn
    // items a and b belongs to the lowest group holding both; there it is
    // one "trunk" between the group's children on the way to a and b
    // (sa, sb), routed by ELK with the group's layout. Every relation
    // between items inside sa and sb shares that trunk; the short
    // "branches" from a to the trunk and from the trunk to b are drawn
    // only when one of their ends is selected or hovered.
    const trunks = new Map(), branches = new Map(), sceneOf = new Map();
    const logical = [];
    const ports = new Map(); // box key + side → connectors ending there
    const wantPort = (key, side, e, end, other) => {
      const k = key + side;
      if (!ports.has(k)) ports.set(k, []);
      ports.get(k).push({ e, end, other });
    };
    function connector(e, A, B, sk, tk) {
      if (B.y >= A.y + A.h + 8) { wantPort(sk, "b", e, 0, B.x + B.w / 2); wantPort(tk, "t", e, 1, A.x + A.w / 2); e.dir = "v"; }
      else if (A.y >= B.y + B.h + 8) { wantPort(sk, "t", e, 0, B.x + B.w / 2); wantPort(tk, "b", e, 1, A.x + A.w / 2); e.dir = "v"; }
      else if (B.x >= A.x + A.w) { wantPort(sk, "r", e, 0, B.y + B.h / 2); wantPort(tk, "l", e, 1, A.y + A.h / 2); e.dir = "h"; }
      else { wantPort(sk, "l", e, 0, B.y + B.h / 2); wantPort(tk, "r", e, 1, A.y + A.h / 2); e.dir = "h"; }
      e.ends = [null, null];
    }
    function merge(into, g) {
      into.n += g.n; into.w += g.w;
      for (const k in g.types) into.types[k] = (into.types[k] || 0) + g.types[k];
      for (const k in g.confs) into.confs[k] = (into.confs[k] || 0) + g.confs[k];
      if (D.CONF_RANK[g.best] > D.CONF_RANK[into.best]) into.best = g.best;
      into.members.push(g);
    }
    agg.forEach((g) => {
      const sk = (g.ca ? "c" : "t") + g.a, tk = (g.cb ? "c" : "t") + g.b;
      logical.push({ key: g.key, s: sk, t: tk, agg: g });
      let c, sa, sb, route = null;
      if (g.ca || g.cb) {
        c = "top"; sa = g.ca ? sk : "t" + v.root; sb = g.cb ? tk : "t" + v.root;
        route = topRes.edges[sa + ">" + sb] || null;
      } else {
        let x = T[g.a].parent;
        while (x >= 0 && !(groupSet.has(x) && (x === g.b || D.isAncestor(x, g.b)))) x = T[x].parent;
        if (x < 0) return;
        c = x; sa = "t" + D.childOf(x, g.a); sb = "t" + D.childOf(x, g.b);
        const res = lay.get(x), o = origin.get(x), r = res.edges["p" + sa.slice(1) + ">" + sb.slice(1)];
        if (r) route = r.map((q, k) => q + (k % 2 ? o.y : o.x));
      }
      const tkey = "T" + c + ":" + sa + ">" + sb;
      let tr = trunks.get(tkey);
      if (!tr) {
        tr = { key: tkey, s: sa, t: sb, pts: route, ends: null, units: new Set(), agg: { n: 0, w: 0, types: {}, confs: {}, best: "unresolved", members: [], get real() { return [].concat.apply([], this.members.map((m) => m.real)); } } };
        trunks.set(tkey, tr);
        if (!route) connector(tr, rectOf(sa), rectOf(sb), sa, sb);
      }
      merge(tr.agg, g);
      tr.units.add(sk); tr.units.add(tk);
      const list = [tkey];
      if (sk !== sa) { const bk = "B" + sk + "@" + tkey; if (!branches.has(bk)) branches.set(bk, { key: bk, s: sk, t: sa, trunk: tr, end: 0, branch: true, units: new Set([sk]), agg: tr.agg }); list.push(bk); }
      if (tk !== sb) { const bk = "B" + tkey + "@" + tk; if (!branches.has(bk)) branches.set(bk, { key: bk, s: sb, t: tk, trunk: tr, end: 1, branch: true, units: new Set([tk]), agg: tr.agg }); list.push(bk); }
      sceneOf.set(g.key, list);
    });
    // spread connector ends along each side, ordered by where they go
    ports.forEach((list, k) => {
      const side = k.slice(-1), r = rectOf(k.slice(0, -1));
      list.sort((x, y) => x.other - y.other);
      const n = list.length;
      list.forEach((p, j) => {
        const f = (j + 1) / (n + 1);
        const along = side === "t" || side === "b" ? r.x + r.w * (0.15 + 0.7 * f) : r.y + r.h * (0.2 + 0.6 * f);
        p.e.ends[p.end] = side === "t" ? [along, r.y] : side === "b" ? [along, r.y + r.h] : side === "l" ? [r.x, along] : [r.x + r.w, along];
      });
    });
    const elbow = (a, b, dir) => {
      let pts;
      if (dir === "v") { const my = (a[1] + b[1]) / 2; pts = [a[0], a[1], a[0], my, b[0], my, b[0], b[1]]; }
      else { const mx = (a[0] + b[0]) / 2; pts = [a[0], a[1], mx, a[1], mx, b[1], b[0], b[1]]; }
      return Math.abs(pts[0] - pts[6]) < 1 || Math.abs(pts[1] - pts[7]) < 1 ? [a[0], a[1], b[0], b[1]] : pts;
    };
    trunks.forEach((tr) => { if (!tr.pts) tr.pts = elbow(tr.ends[0], tr.ends[1], tr.dir); });
    // a branch runs inside the trunk's end box, from the inner item to
    // the point where the trunk leaves (or enters) the box
    branches.forEach((b) => {
      const tp = b.trunk.pts;
      const P = b.end === 0 ? [tp[0], tp[1]] : [tp[tp.length - 2], tp[tp.length - 1]];
      const R = rectOf(b.end === 0 ? b.s : b.t), box = b.end === 0 ? rectOf(b.t) : rectOf(b.s);
      const onTopOrBottom = Math.abs(P[1] - box.y) < 2 || Math.abs(P[1] - (box.y + box.h)) < 2;
      let q;
      if (onTopOrBottom) {
        const below = P[1] > R.y + R.h / 2;
        q = [R.x + R.w / 2, below ? R.y + R.h : R.y];
        b.pts = b.end === 0 ? elbow(q, P, "v") : elbow(P, q, "v");
      } else {
        const right = P[0] > R.x + R.w / 2;
        q = [right ? R.x + R.w : R.x, R.y + R.h / 2];
        b.pts = b.end === 0 ? elbow(q, P, "h") : elbow(P, q, "h");
      }
      b.conf = b.trunk.agg.best;
      b.n = 1;
    });
    const edges = [];
    trunks.forEach((tr) => { tr.conf = tr.agg.best; tr.n = tr.agg.n; tr.label = summary(tr.agg); edges.push(tr); });
    branches.forEach((b) => edges.push(b));

    let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
    boxes.forEach((b) => { x0 = Math.min(x0, b.x); y0 = Math.min(y0, b.y); x1 = Math.max(x1, b.x + b.w); y1 = Math.max(y1, b.y + b.h); });
    if (!boxes.length) { x0 = y0 = 0; x1 = y1 = 1; }
    // adjacency between drawn items (logical relations) for the highlight
    const out = new Map(), inn = new Map();
    logical.forEach((e) => {
      if (!out.has(e.s)) out.set(e.s, []);
      if (!inn.has(e.t)) inn.set(e.t, []);
      out.get(e.s).push(e); inn.get(e.t).push(e);
    });
    const leaves = boxes.filter((b) => b.kind !== "group").length;
    return {
      kind: "arch", v, agg, edges: logical, out, inn, sceneOf,
      scene: { boxes, edges, bounds: { x: x0, y: y0, w: x1 - x0, h: y1 - y0 } },
      counts: { boxes: leaves, groups: v.groups.length, context: ctxList.length, edges: trunks.size },
      keyOf: (i) => v.vis[i] ? "t" + i : null
    };
  }

  // highlight: the dependency path of a drawn item. Downstream are the
  // items it depends on (following relations forward), upstream the items
  // that depend on it. For an expanded group, its drawn content counts as
  // the selection.
  function highlight(view, key, depth) {
    if (!key || !view.scene) return null;
    const box = view.scene.boxes.find((b) => b.key === key);
    if (!box) return null;
    const sources = new Set([key]);
    if (box.kind === "group") view.scene.boxes.forEach((b) => {
      if (b.key !== key && b.x >= box.x && b.y >= box.y && b.x + b.w <= box.x + box.w && b.y + b.h <= box.y + box.h) sources.add(b.key);
    });
    const max = depth || Infinity;
    function walk(adj, end) {
      const seen = new Map();
      let frontier = Array.from(sources), d = 0;
      while (frontier.length && d < max) {
        d++;
        const next = [];
        for (const k of frontier) for (const e of adj.get(k) || []) {
          const o = e[end];
          if (sources.has(o) || seen.has(o)) continue;
          seen.set(o, d);
          next.push(o);
        }
        frontier = next;
      }
      return seen;
    }
    const down = walk(view.out, "t"), up = walk(view.inn, "s");
    // roles: out/in for direct neighbours and their relations, out2/in2
    // further along the path (drawn lighter)
    const boxes = new Map(), edges = new Map();
    sources.forEach((k) => boxes.set(k, "sel"));
    down.forEach((d, k) => boxes.set(k, d === 1 ? "out" : "out2"));
    up.forEach((d, k) => { const r = d === 1 ? "in" : "in2"; const cur = boxes.get(k); boxes.set(k, cur === "out" || cur === "out2" ? (cur === "out" || r === "in" ? "both" : "out2") : r); });
    const RANK = { out: 4, in: 3, out2: 2, in2: 1 };
    const mark = (k, role) => {
      for (const sk of view.sceneOf ? view.sceneOf.get(k) || [] : [k]) if (!edges.has(sk) || RANK[role] > RANK[edges.get(sk)]) edges.set(sk, role);
    };
    view.edges.forEach((e) => {
      if ((sources.has(e.s) || down.has(e.s)) && down.has(e.t) && !sources.has(e.t)) mark(e.key, sources.has(e.s) ? "out" : "out2");
      else if ((sources.has(e.t) || up.has(e.t)) && up.has(e.s) && !sources.has(e.s)) mark(e.key, sources.has(e.t) ? "in" : "in2");
    });
    // groups that hold a highlighted item stay readable
    const keep = new Set();
    boxes.forEach((r, k) => {
      if (k.charAt(0) !== "t") return;
      for (const a of D.ancestors(Number(k.slice(1)))) keep.add("t" + a);
    });
    return { boxes, edges, keep, down: down.size, up: up.size };
  }

  GV.arch = { build, highlight, visible, aggregate, summary, shown, expandable, leafSize };
})(GV);
