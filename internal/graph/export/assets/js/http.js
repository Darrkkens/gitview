// HTTP journey: the endpoint list (sidebar) and, for the selected route, the
// code it may run, left to right: the route, its handler chain in
// registration order, then what the chain calls, by call depth. Every step
// is an edge of the dataset (static analysis): a possible path, not the
// trace of a request. Architecture layers come from Study Mode's
// classification (STATIC from the code's structure, HEURISTIC from
// directory names); an element without a layer is left unclassified.
(function (GV) {
  "use strict";
  const D = GV.data, N = D.N, E = D.E, T = D.T, h = GV.h;
  const PER_COL = 60;
  const LAYERS = [
    ["Route", "l-route"], ["Middleware", "l-middleware"], ["Handler", "l-handler"], ["Service", "l-service"],
    ["Repository", "l-repository"], ["Database", "l-database"], ["External API", "l-external"], ["Entrypoint", "k-group"],
    ["Config", "k-group"], ["Model", "k-type"], ["External", "k-external"], ["Unresolved", "k-unresolved"]
  ];
  const LAYER_COLOR = Object.fromEntries(LAYERS);
  // the canonical path the strip always shows; absent steps are marked
  const SPINE = ["Middleware", "Handler", "Service", "Repository", "Database"];

  function layerOf(n) {
    if (n.type === "route") return "Route";
    if (n.type === "db") return "Database";
    if (n.type === "unresolved") return "Unresolved";
    if (n.ext) return "External";
    if (n.meta.role === "middleware") return "Middleware";
    if (n.meta.layer && n.meta.layer !== "HTTP Route") return n.meta.layer;
    if (n.meta.role === "http handler") return "Handler";
    return "";
  }
  const layerColor = (n) => LAYER_COLOR[layerOf(n)] || D.kindColor(T[n.i]);

  function relOk(e, S) { return S.filters.rels[e.type] !== false && S.filters.confs[e.conf] !== false; }

  async function build(S, aspect) {
    const r = D.ROUTES[S.route], root = D.nodeOfRoute[S.route];
    if (!r || root < 0) return null;
    const col = new Map(), depth = new Map(), more = new Map(), chainPos = new Map(), order = [];
    const place = (i, c) => { col.set(i, c); order.push(i); };
    place(root, 0); depth.set(root, 0);
    const chain = N[root].out.map((ei) => E[ei]).filter((e) => e.type === "handles").sort((a, b) => (a.meta.order || 0) - (b.meta.order || 0));
    const q = [];
    const perCol = new Map();
    chain.forEach((e, k) => {
      if (col.has(e.t)) return;
      place(e.t, 1); depth.set(e.t, 1); chainPos.set(e.t, k + 1); q.push(e.t);
    });
    perCol.set(1, chain.length);
    while (q.length) {
      const i = q.shift(), d = depth.get(i);
      const open = S.jexp[i] === true || (S.jexp[i] !== false && d <= S.jdepth);
      const callees = N[i].out.map((ei) => E[ei]).filter((e) => D.FLOW[e.type] && e.type !== "handles" && relOk(e, S) && !(N[e.t].ext && !S.httpExt));
      callees.sort((a, b) => D.CONF_RANK[b.conf] - D.CONF_RANK[a.conf] || (N[a.t].name < N[b.t].name ? -1 : 1));
      let hidden = 0;
      for (const e of callees) {
        if (col.has(e.t)) continue;
        const c = col.get(i) + 1;
        if (!open || (perCol.get(c) || 0) >= PER_COL) { hidden++; continue; }
        place(e.t, c); depth.set(e.t, d + 1); q.push(e.t);
        perCol.set(c, (perCol.get(c) || 0) + 1);
      }
      if (hidden) more.set(i, hidden);
    }
    // relations between drawn steps
    const agg = new Map();
    order.forEach((i) => {
      for (const ei of N[i].out) {
        const e = E[ei];
        if (!col.has(e.t) || e.t === i || !(D.FLOW[e.type]) || !relOk(e, S)) continue;
        const key = "h" + i + ">h" + e.t;
        let g = agg.get(key);
        if (!g) { g = { key, a: i, b: e.t, n: 0, w: 0, types: {}, confs: {}, best: "unresolved", real: [] }; agg.set(key, g); }
        g.n++; g.w += e.w;
        g.types[e.type] = (g.types[e.type] || 0) + 1;
        g.confs[e.conf] = (g.confs[e.conf] || 0) + 1;
        if (D.CONF_RANK[e.conf] > D.CONF_RANK[g.best]) g.best = e.conf;
        g.real.push(e.i);
      }
    });
    const sizeOf = (i) => {
      const n = N[i];
      const title = n.type === "route" ? r.key : n.name;
      const s = GV.arch.leafSize(T[i]);
      const tw = GV.layout.textWidth(title, GV.layout.font(500, 12.5)) + 30 + (chainPos.has(i) ? 20 : 0) + (more.has(i) ? 34 : 0);
      return { w: Math.round(Math.max(s.w, Math.min(tw, 320))), h: 44 };
    };
    const nodes = order.map((i) => { const s = sizeOf(i); return { id: "h" + i, width: s.w, height: s.h, c: col.get(i) }; });
    const edges = Array.from(agg.values()).map((g) => ({ id: g.key, s: "h" + g.a, t: "h" + g.b }));
    const key = "http|" + r.id + "|" + nodes.map((n) => n.id + ":" + n.c).join(",") + "|" + edges.map((e) => e.id).join(",");
    const res = await GV.layout.runPartitioned(key, nodes, edges, 0, aspect);
    const layers = {};
    const boxes = order.map((i) => {
      const n = N[i], p = res.pos["h" + i] || { x: 0, y: 0 }, s = sizeOf(i);
      const lyr = layerOf(n);
      if (lyr) layers[lyr] = (layers[lyr] || 0) + 1;
      const where = n.file ? GV.baseName(n.file) + (n.line ? ":" + n.line : "") : n.ext ? n.pkg : "";
      return {
        key: "h" + i, ref: i, kind: "node", x: p.x, y: p.y, w: s.w, h: s.h, depth: 0,
        title: n.type === "route" ? r.key : n.name,
        sub: n.type === "route" ? r.framework + " · " + where : (chainPos.has(i) ? (lyr === "Middleware" ? "middleware" : "handler") + " · " : "") + where,
        color: layerColor(n), chip: lyr && n.type !== "route" ? lyr : "", chipColor: LAYER_COLOR[lyr] || "text-2",
        order: chainPos.get(i) || 0, count: more.has(i) ? "+" + more.get(i) : "", chev: more.has(i) ? "closed" : null, tip: D.reference(T[i]), layer: lyr
      };
    });
    const byKey = new Map(boxes.map((b) => [b.key, b]));
    const sceneEdges = Array.from(agg.values()).map((g) => {
      const e = { key: g.key, s: "h" + g.a, t: "h" + g.b, conf: g.best, n: g.n, label: GV.arch.summary(g), pts: res.edges[g.key] || null, agg: g };
      if (!e.pts) {
        const A = byKey.get(e.s), B = byKey.get(e.t);
        const ax = A.x + A.w, ay = A.y + A.h / 2, bx = B.x, by = B.y + B.h / 2, mx = (ax + bx) / 2;
        e.pts = [ax, ay, mx, ay, mx, by, bx, by];
      }
      return e;
    });
    let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
    boxes.forEach((b) => { x0 = Math.min(x0, b.x); y0 = Math.min(y0, b.y); x1 = Math.max(x1, b.x + b.w); y1 = Math.max(y1, b.y + b.h); });
    const out = new Map(), inn = new Map();
    sceneEdges.forEach((e) => {
      if (!out.has(e.s)) out.set(e.s, []);
      if (!inn.has(e.t)) inn.set(e.t, []);
      out.get(e.s).push(e); inn.get(e.t).push(e);
    });
    let hidden = 0;
    more.forEach((n) => { hidden += n; });
    return {
      kind: "http", route: S.route, root, edges: sceneEdges, out, inn, layers, hidden, more, chainPos,
      scene: { boxes, edges: sceneEdges, bounds: { x: x0, y: y0, w: x1 - x0, h: y1 - y0 } },
      counts: { boxes: boxes.length, edges: sceneEdges.length },
      keyOf: (i) => col.has(i) ? "h" + i : null
    };
  }

  // ---------- layer strip ----------
  function drawLayers(view, onPick, active) {
    const el = GV.$("layerstrip");
    GV.clear(el);
    if (!view || view.kind !== "http") { el.hidden = true; return; }
    el.hidden = false;
    const present = Object.keys(view.layers);
    const steps = ["Route"].concat(SPINE, present.filter((l) => l !== "Route" && SPINE.indexOf(l) < 0));
    steps.forEach((l, k) => {
      if (k) el.appendChild(h("span", { class: "arrow", text: k <= SPINE.length ? "→" : "·" }));
      const n = view.layers[l] || 0;
      const sw = h("span", { class: "kdot" });
      sw.style.background = GV.token(LAYER_COLOR[l] || "k-group");
      el.appendChild(h("button", {
        type: "button", class: "lyr" + (n ? "" : " missing"), "aria-pressed": String(active === l), disabled: n ? null : true,
        title: n ? n + " step(s) classified as " + l + " in the drawn graph; click to highlight them" : "No step classified as " + l + " is reachable in the drawn graph" + (l === "Route" ? "" : " (raise the depth or expand a step)"),
        on: { click: () => onPick(active === l ? null : l) }
      }, [sw, l === "Handler" ? "Handler (controller)" : l, h("span", { class: "n", text: String(n) })]));
    });
  }

  // ---------- endpoint list ----------
  const RS = { q: "", methods: {}, group: "module", filter: "" };
  const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "ANY"];
  let built = false;
  function moduleOfRoute(r) {
    const n = D.nodeOfRoute[D.ROUTES.indexOf(r)];
    for (let p = n; p >= 0; p = T[p].parent) if (T[p].kind === "module") return T[p].label;
    return D.raw.repository;
  }
  function handlerPkg(r) {
    const i = r.handler && r.handler.id && D.byId.has(r.handler.id) ? D.byId.get(r.handler.id) : -1;
    const p = i >= 0 ? D.packageOf(i) : -1;
    return p >= 0 ? D.displayName(T[p]) : "(unresolved handler)";
  }
  function groupKeys(r) {
    if (RS.group === "prefix") return [r.group || "/"];
    if (RS.group === "package") return [handlerPkg(r)];
    if (RS.group === "file") return [r.file];
    return [moduleOfRoute(r), r.method];
  }
  function matches(r) {
    const anyM = Object.keys(RS.methods).some((k) => RS.methods[k]);
    if (anyM && !RS.methods[r.method]) return false;
    if (RS.filter === "auth" && !(r.auth && r.auth.recognized)) return false;
    if (RS.filter === "noauth" && r.auth && r.auth.recognized) return false;
    if (RS.filter === "partial" && r.status === "complete") return false;
    const t = RS.q.trim().toLowerCase();
    if (!t) return true;
    const hay = (r.method + " " + r.path + " " + r.template + " " + (r.handler ? r.handler.name : "") + " " + r.group).toLowerCase();
    return t.split(/\s+/).every((w) => hay.indexOf(w) >= 0);
  }
  function pathNode(path) {
    const el = h("span", { class: "p", title: path });
    path.split(/(:[A-Za-z0-9_]+|\{[^}]+\}|\*[A-Za-z0-9_]*)/).forEach((part) => {
      if (!part) return;
      if (/^(:|\{|\*)/.test(part)) { el.appendChild(h("span", { class: "var", text: part })); return; }
      // long paths wrap after a slash, never inside a segment
      part.split(/(?<=\/)/).forEach((seg, k) => { if (k) el.appendChild(document.createElement("wbr")); el.appendChild(document.createTextNode(seg)); });
    });
    return el;
  }
  function drawRoutes(current) {
    const el = GV.$("routes");
    if (!built) {
      built = true;
      const search = h("input", { type: "search", placeholder: "Filter endpoints…", "aria-label": "Filter endpoints", autocomplete: "off", spellcheck: "false",
        on: { input: (ev) => { RS.q = ev.target.value; drawList(current()); } } });
      const gsel = h("select", { "aria-label": "Group endpoints by", on: { change: (ev) => { RS.group = ev.target.value; drawList(current()); } } },
        [["module", "Module · method"], ["prefix", "Path prefix"], ["package", "Handler package"], ["file", "Registration file"]].map((o) => h("option", { value: o[0], text: o[1] })));
      const fsel = h("select", { "aria-label": "Show", on: { change: (ev) => { RS.filter = ev.target.value; drawList(current()); } } },
        [["", "All endpoints"], ["auth", "Authentication recognised"], ["noauth", "No authentication"], ["partial", "Partial contracts"]].map((o) => h("option", { value: o[0], text: o[1] })));
      el.appendChild(h("div", { class: "rtools" }, [search, h("div", { class: "chips", id: "rchips" }), h("div", { class: "line" }, [gsel, fsel])]));
      el.appendChild(h("div", { class: "rlist", id: "rlist", role: "listbox", "aria-label": "Endpoints" }));
    }
    const chips = GV.clear(GV.$("rchips"));
    const present = {};
    D.ROUTES.forEach((r) => { present[r.method] = (present[r.method] || 0) + 1; });
    METHODS.concat(Object.keys(present).filter((m) => METHODS.indexOf(m) < 0)).forEach((m) => {
      if (!present[m]) return;
      chips.appendChild(h("button", { type: "button", class: "m-" + m, "aria-pressed": String(!!RS.methods[m]), title: "Only " + m + " endpoints (combine several)",
        on: { click: () => { RS.methods[m] = !RS.methods[m]; drawRoutes(current); } } }, [m, h("span", { class: "n", text: String(present[m]) })]));
    });
    drawList(current());
  }
  function drawList(cur) {
    const list = GV.clear(GV.$("rlist"));
    const tree = new Map();
    let shown = 0;
    D.ROUTES.forEach((r, i) => {
      if (!matches(r)) return;
      shown++;
      const [g1, g2] = groupKeys(r);
      if (!tree.has(g1)) tree.set(g1, new Map());
      const sub = tree.get(g1);
      const k2 = g2 || "";
      if (!sub.has(k2)) sub.set(k2, []);
      sub.get(k2).push(i);
    });
    if (!D.ROUTES.length) { list.appendChild(h("div", { class: "empty" }, [h("strong", { text: "No HTTP endpoint" }), "The analysis found no route registration (gin, echo, chi or net/http)."])); return; }
    if (!shown) { list.appendChild(h("div", { class: "empty", text: "No endpoint matches the filter." })); return; }
    const mOrder = (m) => { const k = METHODS.indexOf(m); return k < 0 ? 99 : k; };
    Array.from(tree.keys()).sort().forEach((g1) => {
      const sub = tree.get(g1);
      let total = 0;
      sub.forEach((l) => { total += l.length; });
      list.appendChild(h("div", { class: "rgroup" }, [h("span", { class: "grow", text: g1, title: g1 }), h("span", { text: String(total) })]));
      Array.from(sub.keys()).sort((a, b) => mOrder(a) - mOrder(b) || (a < b ? -1 : 1)).forEach((g2) => {
        if (g2) list.appendChild(h("div", { class: "rgroup sub" }, [h("span", { class: "method m-" + g2, text: g2 }), h("span", { class: "grow" }), h("span", { text: String(sub.get(g2).length) })]));
        sub.get(g2).sort((a, b) => D.ROUTES[a].path < D.ROUTES[b].path ? -1 : 1).forEach((i) => {
          const r = D.ROUTES[i];
          const flags = h("span", { class: "flags" });
          if (r.auth && r.auth.recognized) flags.appendChild(h("span", { class: "flag auth", title: "Authentication recognised: " + (r.auth.by || r.auth.scheme || "") }, [GV.icon("lock")]));
          if (r.status !== "complete") flags.appendChild(h("span", { class: "flag partial", title: "Contract " + r.status + ": " + (r.reasons || []).join("; ") }, [GV.icon("warn")]));
          list.appendChild(h("div", {
            class: "ritem", role: "option", tabindex: "0", "aria-current": String(cur === i), "data-route": i,
            title: r.key + (r.handler ? " → " + r.handler.name : ""),
            on: { click: () => GV.app.openRoute(i), keydown: (ev) => { if (ev.key === "Enter") GV.app.openRoute(i); } }
          }, [h("span", { class: "method m-" + r.method, text: r.method }), pathNode(r.path), flags]));
        });
      });
    });
    const curEl = list.querySelector('.ritem[aria-current="true"]');
    if (curEl && curEl.scrollIntoView) curEl.scrollIntoView({ block: "nearest" });
  }

  function routesOfPackage(p) {
    const out = [];
    D.ROUTES.forEach((r, i) => {
      const hn = r.handler && r.handler.id && D.byId.has(r.handler.id) ? D.byId.get(r.handler.id) : -1;
      const rn = D.nodeOfRoute[i];
      if ((hn >= 0 && (hn === p || D.isAncestor(p, hn))) || (rn >= 0 && (rn === p || D.isAncestor(p, rn)))) out.push(i);
    });
    return out;
  }

  GV.http = { build, drawLayers, drawRoutes, layerOf, layerColor, LAYER_COLOR, routesOfPackage, pathNode };
})(GV);
