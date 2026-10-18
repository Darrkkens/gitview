// Application: state, navigation and the chrome around the canvas.
//
// State lives here and in the URL hash, so a view can be linked and the
// browser's back and forward buttons walk through scopes, focus targets
// and routes (selection and expansion replace the current entry).
(function (GV) {
  "use strict";
  const D = GV.data, T = D.T, N = D.N, h = GV.h;
  const init = D.raw.initial || {};

  // ---------- state ----------
  function initialOpen() {
    const open = new Set(D.modules.map((m) => m.i));
    const lvl = init.level || "";
    if (lvl && lvl !== "modules") {
      D.descendants(D.ROOT, (i) => {
        const t = T[i];
        if (t.ext || (t.n && t.n.ext) || t.kind === "extgroup") return;
        if (t.kind === "dir") open.add(i);
        if (t.kind === "package" && lvl !== "packages") open.add(i);
      });
    }
    return open;
  }
  const S = {
    mode: "arch", root: D.ROOT, open: initialOpen(), focus: null, sel: null,
    route: -1, jdepth: 3, jexp: {}, httpExt: false, layerPick: null,
    filters: GV.filters.defaults(), pathDepth: 0,
    maxNodes: init.maxNodes || 5000, maxEdges: init.maxEdges || 25000,
    sec: { finding: null, route: null, pick: null, path: null }
  };
  const hasSecurity = !!(GV.security && GV.security.available);
  let view = null, building = 0, restoring = false, ready = null, resolveReady = null, lastBuild = Promise.resolve();
  ready = new Promise((r) => { resolveReady = r; });

  // ---------- view ----------
  function aspect() {
    const st = GV.$("stage");
    return Math.max(0.5, Math.min(3, (st.clientWidth || 1200) / Math.max(1, st.clientHeight || 800)));
  }
  function setState(text, kind) {
    const el = GV.$("stage-state");
    GV.clear(el);
    el.className = "stage-state" + (text ? " on " + (kind || "") : "");
    if (!text) return;
    el.appendChild(typeof text === "string" ? h("div", { class: "box" }, [kind === "loading" ? h("span", { class: "spinner" }) : null, h("span", { text })]) : text);
  }
  function rebuild(opts) {
    lastBuild = build(opts || {});
    return lastBuild;
  }
  async function build(opts) {
    const id = ++building;
    const slow = setTimeout(() => { if (id === building) setState("Laying out…", "loading"); }, 150);
    let v = null;
    try {
      if (S.mode === "security") v = await GV.security.build(S, aspect());
      else if (S.mode === "http") v = S.route >= 0 ? await GV.http.build(S, aspect()) : null;
      else if (S.focus) v = await GV.focus.build(S, aspect());
      else v = await GV.arch.build(S, aspect());
    } catch (err) {
      clearTimeout(slow);
      if (id !== building) return;
      setState(h("div", { class: "box col" }, [h("strong", { text: "This view could not be drawn" }), h("span", { text: String(err && err.message || err) })]));
      console.error(err);
      return;
    }
    clearTimeout(slow);
    if (id !== building) return;
    view = v;
    setState("");
    if (!v) {
      GV.render.setScene({ boxes: [], edges: [], bounds: { x: 0, y: 0, w: 1, h: 1 } }, { fit: true });
      if (S.mode === "security") setState(h("div", { class: "box col" }, [h("strong", { text: GV.security.findings.length ? "Pick a finding or a route" : "No findings" }),
        h("span", { text: GV.security.findings.length ? "The list on the left has what the security rules found. The path from the route to the code is drawn here, with the evidence in the inspector." : "The enabled rules found nothing. The inspector shows the scope and the limits of the analysis." })]));
      if (S.mode === "http") setState(h("div", { class: "box col" }, [h("strong", { text: D.ROUTES.length ? "Pick an endpoint" : "No HTTP endpoint" }),
        h("span", { text: D.ROUTES.length ? "The list on the left has every route the analysis found. Its journey through the code is drawn here." : "The analysis found no route registration in this repository." })]));
    } else {
      GV.render.setScene(v.scene, { fit: opts.fit, animate: opts.animate && !opts.fit, anchor: opts.anchor, fitOpts: fitOpts(v) });
      if (!v.scene.boxes.length) setState(h("div", { class: "box col" }, [h("strong", { text: "Nothing to draw" }), h("span", { text: "The filters hide every element of this scope." })]));
    }
    applySelection(false);
    chrome();
    if (opts.reveal && view) {
      const k = view.keyOf(opts.reveal);
      if (k) GV.render.reveal([k]);
    }
    resolveReady(true);
  }

  // Journeys and focus graphs can be long: fit them only while their text
  // stays readable, starting from the route or the focused element.
  function fitOpts(v) {
    if (v.kind === "http") return { minZoom: 0.62, anchor: "h" + v.root, align: "left" };
    if (v.kind === "focus") return { minZoom: 0.62, anchor: "f" + v.center, align: "center" };
    if (v.kind === "security") return { minZoom: 0.62, anchor: v.root, align: "topleft" };
    return { minZoom: 0.4, anchor: v.scene.boxes.length ? v.scene.boxes[0].key : null, align: "topleft" };
  }
  function fitView() { if (view) GV.render.fit(true, fitOpts(view)); else GV.render.fit(); }

  // ---------- selection ----------
  function selectedKey() {
    if (!S.sel || !view) return null;
    if (S.sel.kind === "edge") return S.sel.key;
    if (S.sel.kind === "item") return view.keyOf(S.sel.i) || (view.kind === "arch" && view.v.ctx[S.sel.i] === S.sel.i ? "c" + S.sel.i : null);
    return null;
  }
  function applySelection(scroll) {
    const key = selectedKey();
    GV.render.setSelected(key);
    let hl = null;
    if (view && S.layerPick && view.kind === "http") {
      const boxes = new Map();
      view.scene.boxes.forEach((b) => { if (b.layer === S.layerPick) boxes.set(b.key, "sel"); });
      hl = { boxes, edges: new Map(), keep: new Set() };
    } else if (view && S.mode === "security" && S.sec.path && S.sec.path.keys) {
      const ks = S.sec.path.keys, boxes = new Map(ks.map((k) => [k, "sel"])), edges = new Map();
      for (let k = 1; k < ks.length; k++) (view.out.get(ks[k - 1]) || []).forEach((e) => { if (e.t === ks[k]) edges.set(e.key, "out"); });
      hl = { boxes, edges, keep: new Set() };
    } else if (view && key && S.sel.kind === "item") hl = GV.arch.highlight(view, key, S.pathDepth || 0);
    else if (view && key && S.sel.kind === "edge") {
      const e = GV.render.edge(key);
      if (e) {
        const ends = e.units ? Array.from(e.units) : [e.s, e.t];
        hl = { boxes: new Map(ends.map((k) => [k, "sel"])), edges: new Map([[key, "out"]]), keep: keepOf(ends) };
        if (view.sceneOf && e.agg && e.agg.members) e.agg.members.forEach((g) => (view.sceneOf.get(g.key) || []).forEach((sk) => hl.edges.set(sk, "out")));
      }
    }
    GV.render.setHighlight(hl);
    // inspector
    if (S.mode === "security" && !(S.sel && S.sel.kind === "item")) {
      if (S.sec.path) GV.inspector.show({ type: "secPath", id: S.sec.path.from + ">" + S.sec.path.to, from: S.sec.path.from, to: S.sec.path.to, names: S.sec.path.names });
      else if (S.sec.finding) GV.inspector.show({ type: "secFinding", id: S.sec.finding });
      else if (S.sec.route) GV.inspector.show({ type: "secRoute", id: S.sec.route });
      else GV.inspector.show({ type: "secSummary", id: "" });
    } else if (!S.sel) GV.inspector.show(S.mode === "http" && S.route >= 0 ? { type: "route", k: S.route } : null);
    else if (S.sel.kind === "edge") { const e = GV.render.edge(S.sel.key); GV.inspector.show(e ? { type: "edge", edge: e, key: e.key } : null); }
    else if (S.mode === "http" && S.route >= 0 && S.sel.i === D.nodeOfRoute[S.route]) GV.inspector.show({ type: "route", k: S.route });
    else GV.inspector.show({ type: "item", i: S.sel.i });
    if (S.sel && S.sel.kind === "item") GV.tree.select(S.sel.i);
    drawCrumbs();
    drawViewbar(hl);
    drawLegend(hl);
    writeHash();
    if (scroll && key) GV.render.reveal([key]);
  }
  function keepOf(keys) {
    const keep = new Set();
    keys.forEach((k) => { if (k.charAt(0) === "t") D.ancestors(Number(k.slice(1))).forEach((a) => keep.add("t" + a)); });
    return keep;
  }
  function isDrawn(i) { return !!(view && view.keyOf(i)); }

  // selectItem selects a tree item. opts.reveal makes it visible on the
  // canvas: in the architecture it expands the item's ancestors (widening
  // the scope when the item is outside it).
  async function selectItem(i, opts) {
    opts = opts || {};
    if (i < 0 || !T[i]) return;
    // switching to the architecture always draws the item there
    let switched = false;
    if (opts.mode === "arch" && (S.mode !== "arch" || S.focus)) { pushHistory(); S.mode = "arch"; S.focus = null; switched = true; }
    S.layerPick = null;
    S.sel = { kind: "item", i };
    if (!switched && view && view.keyOf(i)) { applySelection(!!opts.reveal); return; }
    if (!switched && view && view.kind === "arch" && view.v.ctx[i] === i && !opts.force) { applySelection(true); return; }
    if (!opts.reveal && !switched) { applySelection(false); return; }
    if (S.mode === "http" || S.mode === "security" || S.focus) {
      if (S.mode === "http" || S.mode === "security" || opts.from === "tree" || opts.from === "search" || opts.force) { pushHistory(); S.mode = "arch"; S.focus = null; }
      else { applySelection(false); return; }
    }
    // make it drawable
    const t = T[i];
    if ((t.n && t.n.ext) || t.ext || t.kind === "extgroup") {
      if (!S.filters.ext) { S.filters.ext = true; GV.toast("External dependencies are now shown (Filters)."); }
    } else if (t.n && S.filters.kinds[t.kind] === false) { S.filters.kinds[t.kind] = true; GV.toast(D.kindLabel(t) + " elements are now shown (Filters)."); }
    for (const a of D.ancestors(i)) if (T[a].n && S.filters.kinds[T[a].kind] === false) S.filters.kinds[T[a].kind] = true;
    if (i !== S.root && !D.isAncestor(S.root, i)) {
      let r = S.root;
      while (r !== D.ROOT && !D.isAncestor(r, i)) r = T[r].parent;
      if (r !== S.root) { pushHistory(); S.root = r; }
    }
    if (i === S.root) { await rebuild({ fit: true }); return; }
    for (const a of D.ancestors(i)) { if (a === S.root) break; S.open.add(a); }
    await rebuild({ animate: true, reveal: i });
  }
  function selectEdge(e) { S.sel = { kind: "edge", key: e.key }; S.layerPick = null; applySelection(false); }
  function clearSelection() { if (!S.sel && !S.layerPick) return; S.sel = null; S.layerPick = null; applySelection(false); }

  // ---------- navigation ----------
  function visibleCount() { return view && view.scene ? view.scene.boxes.length : 0; }
  function toggle(i) {
    if (S.mode !== "arch" || S.focus) return;
    const t = T[i];
    if (!GV.arch.expandable(t) || i === S.root) return;
    if (S.open.has(i)) S.open.delete(i);
    else {
      if (visibleCount() + t.kids.length > S.maxNodes) { GV.toast("Expanding would draw more than " + GV.fmt(S.maxNodes) + " elements. Open it as the scope instead.", { label: "Open as scope", run: () => enter(i) }); return; }
      if (t.kids.length > 400) GV.toast("Large group: " + GV.fmt(t.kids.length) + " elements, packed without layers.");
      S.open.add(i);
    }
    S.sel = { kind: "item", i };
    rebuild({ animate: true, anchor: "t" + i });
  }
  function enter(i) {
    if (!GV.arch.expandable(T[i])) return;
    pushHistory();
    S.mode = "arch"; S.focus = null; S.root = i;
    S.open.add(i);
    S.sel = null;
    rebuild({ fit: true });
  }
  function focusOn(i) {
    if (i < 0 || i === D.ROOT) return;
    pushHistory();
    S.mode = "arch";
    S.focus = { i, up: S.focus ? S.focus.up : 2, down: S.focus ? S.focus.down : 2, refs: S.focus ? S.focus.refs : false };
    S.sel = { kind: "item", i };
    rebuild({ fit: true });
  }
  function exitFocus() {
    if (!S.focus) return;
    pushHistory();
    const i = S.focus.i;
    S.focus = null;
    selectItem(i, { reveal: true });
  }
  function setMode(m) {
    if (m === S.mode && !(m === "arch" && S.focus)) return;
    if (m === "http" && !D.ROUTES.length) { GV.toast("No HTTP route was found in this repository."); return; }
    if (m === "security" && !hasSecurity) { GV.toast("This page has no security audit: export it with gitview audit export."); return; }
    if (m === "security") S.sel = null;
    pushHistory();
    S.mode = m;
    S.layerPick = null;
    if (m === "http") S.sel = S.route >= 0 ? { kind: "item", i: D.nodeOfRoute[S.route] } : null;
    rebuild({ fit: true });
  }
  function openRoute(k) {
    if (k < 0 || !D.ROUTES[k]) return;
    if (S.mode !== "http" || S.route !== k) pushHistory();
    const same = S.route === k;
    S.mode = "http"; S.route = k; S.focus = null; S.layerPick = null;
    if (!same) S.jexp = {};
    S.sel = { kind: "item", i: D.nodeOfRoute[k] };
    GV.http.drawRoutes(() => S.route);
    rebuild({ fit: true });
  }
  function setJourney(p) { Object.assign(S, p); rebuild({ fit: false }); }
  function setFilters(f) { S.filters = f; GV.filters.refreshBadge(); rebuild({ animate: true }); }
  function setPathDepth(d) { S.pathDepth = d; applySelection(false); }
  function setTheme(t) {
    document.documentElement.setAttribute("data-theme", t);
    GV.pref.set("theme", t);
    GV.resetTokens();
    GV.emit("theme");
    GV.tree.render();
    GV.inspector.refresh();
    chrome();
  }
  function up() {
    if (S.sel && S.sel.kind === "item") {
      const p = T[S.sel.i].parent;
      if (S.mode === "arch" && !S.focus && p >= 0 && (p === S.root || D.isAncestor(S.root, p))) { selectItem(p); return; }
    }
    if (S.focus) { exitFocus(); return; }
    if (S.mode === "arch" && S.root !== D.ROOT) { const r = S.root; enterAncestor(T[r].parent, r); }
  }
  function enterAncestor(a, sel) {
    pushHistory();
    S.root = a;
    S.open.add(a);
    S.sel = sel >= 0 ? { kind: "item", i: sel } : null;
    rebuild({ fit: true });
  }

  // ---------- security ----------
  function openFinding(id) {
    if (!hasSecurity || !GV.security.finding(id)) return;
    if (S.mode !== "security" || S.sec.finding !== id) pushHistory();
    S.mode = "security"; S.focus = null; S.sel = null; S.layerPick = null;
    S.sec = { finding: id, route: null, pick: null, path: null };
    rebuild({ fit: true });
  }
  function openSecRoute(id) {
    if (!hasSecurity) return;
    if (S.mode !== "security" || S.sec.route !== id) pushHistory();
    S.mode = "security"; S.focus = null; S.sel = null;
    S.sec = { finding: null, route: id, pick: null, path: null };
    rebuild({ fit: true });
  }
  function refreshSecurity() {
    if (!hasSecurity) return;
    GV.security.drawSidebar(S);
    rebuild({ fit: false });
  }

  // ---------- history and links ----------
  function hashOf() {
    const p = [];
    const enc = encodeURIComponent;
    p.push("m=" + S.mode);
    if (S.mode === "arch") {
      if (S.root !== D.ROOT) p.push("root=" + enc(T[S.root].id));
      if (S.focus) p.push("focus=" + enc(T[S.focus.i].id), "up=" + S.focus.up, "down=" + S.focus.down);
      else Array.from(S.open).filter((i) => i !== S.root && D.isAncestor(S.root, i)).sort((a, b) => a - b).forEach((i) => p.push("open=" + enc(T[i].id)));
    } else if (S.mode === "security") {
      if (S.sec.finding) p.push("finding=" + enc(S.sec.finding));
      if (S.sec.route) p.push("sroute=" + enc(S.sec.route));
      const tab = GV.security.tab();
      if (S.sec.finding && tab && tab !== "overview") p.push("tab=" + tab);
    } else if (S.route >= 0) {
      p.push("route=" + enc(D.ROUTES[S.route].id), "depth=" + S.jdepth);
      const tab = GV.inspector.tab("route");
      if (tab && tab !== "overview") p.push("tab=" + tab);
    }
    if (S.sel && S.sel.kind === "item" && !(S.mode === "http" && S.sel.i === D.nodeOfRoute[S.route])) p.push("sel=" + enc(T[S.sel.i].id));
    return "#" + p.join("&");
  }
  // Navigation (scope, focus, mode, route) adds a history entry when the
  // new view is written; selection and expansion replace the current one.
  let pendingPush = false;
  function writeHash() {
    if (restoring) return;
    const hsh = hashOf();
    const push = pendingPush;
    pendingPush = false;
    if (hsh === location.hash) return;
    try { if (push) history.pushState(null, "", hsh); else history.replaceState(null, "", hsh); } catch (e) { /* some file:// contexts */ }
  }
  function pushHistory() { if (!restoring) pendingPush = true; }
  function linkTo(i) {
    const base = location.href.replace(/#.*$/, "");
    return base + "#m=arch&sel=" + encodeURIComponent(T[i].id);
  }
  function readHash() {
    const p = { open: [] };
    (location.hash || "").replace(/^#/, "").split("&").forEach((kv) => {
      const j = kv.indexOf("=");
      if (j <= 0) return;
      let v;
      try { v = decodeURIComponent(kv.slice(j + 1)); } catch (e) { return; }
      const k = kv.slice(0, j);
      if (k === "open") p.open.push(v); else p[k] = v;
    });
    return p;
  }
  const idx = (id) => (id && D.byId.has(id) ? D.byId.get(id) : -1);
  function restore() {
    const p = readHash();
    restoring = true;
    pendingPush = false;
    try {
      S.layerPick = null;
      if ((p.m === "security" || !p.m) && hasSecurity) {
        S.mode = "security";
        S.sec = { finding: p.finding && GV.security.finding(p.finding) ? p.finding : null, route: p.sroute || null, pick: null, path: null };
        if (p.tab) GV.security.setTab(p.tab);
        S.sel = null;
      } else if (p.m === "http" && D.ROUTES.length) {
        S.mode = "http";
        const k = D.ROUTES.findIndex((r) => r.id === p.route);
        S.route = k;
        if (p.depth) S.jdepth = Math.max(0, Math.min(8, Number(p.depth) || 3));
        if (p.tab) GV.inspector.setTab("route", p.tab);
        S.sel = k >= 0 ? { kind: "item", i: D.nodeOfRoute[k] } : null;
      } else {
        S.mode = "arch";
        const r = idx(p.root);
        S.root = r >= 0 && GV.arch.expandable(T[r]) ? r : D.ROOT;
        // a hash written by the page lists every expansion; no hash means
        // the initial view
        if (p.m) { S.open = new Set(p.open.map(idx).filter((i) => i >= 0)); S.open.add(S.root); }
        else S.open = initialOpen();
        const f = idx(p.focus);
        S.focus = f >= 0 ? { i: f, up: clampDepth(p.up), down: clampDepth(p.down), refs: false } : null;
        S.sel = null;
      }
      const s = idx(p.sel);
      if (s >= 0) S.sel = { kind: "item", i: s };
      GV.http.drawRoutes(() => S.route);
    } finally { restoring = false; }
    rebuild({ fit: true });
  }
  const clampDepth = (v) => { const n = Number(v); return n >= 0 && n <= 6 ? n : 2; };
  window.addEventListener("popstate", restore);

  // ---------- chrome ----------
  function chrome() {
    document.querySelectorAll("#modes button").forEach((b) => {
      b.setAttribute("aria-selected", String(b.dataset.mode === S.mode));
      if (b.dataset.mode === "http") { b.disabled = !D.ROUTES.length; b.title = D.ROUTES.length ? GV.plural(D.ROUTES.length, "HTTP route") + " (2)" : "No HTTP route was found"; }
      else if (b.dataset.mode === "security") b.title = "Security Journey: " + GV.plural(GV.security.findings.length, "finding") + " (3)";
      else b.title = "Architecture (1)";
    });
    document.body.classList.toggle("mode-http", S.mode === "http");
    GV.$("tree").hidden = S.mode !== "arch";
    GV.$("sidebar-filter").parentElement.hidden = S.mode !== "arch";
    GV.$("routes").hidden = S.mode !== "http";
    GV.$("security").hidden = S.mode !== "security";
    GV.$("sidebar-title").textContent = S.mode === "http" ? "Endpoints" : S.mode === "security" ? "Security" : "Explorer";
    if (S.mode === "http") GV.http.drawRoutes(() => S.route);
    if (S.mode === "security") GV.security.drawSidebar(S);
    GV.http.drawLayers(view, (l) => { S.layerPick = l; applySelection(false); }, S.layerPick);
    const tb = document.querySelector('[data-act="focus"]');
    if (tb) tb.setAttribute("aria-pressed", String(!!S.focus));
    drawCrumbs();
    drawViewbar();
    drawStatus();
  }
  function crumb(text, title, onClick, cls, color) {
    const b = h("button", { type: "button", class: cls || null, title: title || text, on: { click: onClick } });
    if (color) { const d = h("span", { class: "kind-dot" }); d.style.background = GV.token(color); b.appendChild(d); }
    b.appendChild(document.createTextNode(text));
    return b;
  }
  function drawCrumbs() {
    const c = GV.clear(GV.$("crumbs"));
    const items = [];
    if (S.mode === "security") {
      items.push(crumb("CodeAudit", "Security Journey", () => { pushHistory(); S.sec = { finding: null, route: null, pick: null, path: null }; S.sel = null; rebuild({ fit: true }); }));
      const f = S.sec.finding && GV.security.finding(S.sec.finding);
      if (f) items.push(crumb(f.title, f.id, () => { S.sel = null; S.sec.path = null; applySelection(false); }, null, GV.security.stateOf(f).token));
      if (S.sec.route) items.push(crumb(S.sec.route.replace(/^route:/, "").replace(/@.*$/, ""), S.sec.route, () => { S.sel = null; applySelection(false); }, null, "l-route"));
      if (S.sel && S.sel.kind === "item") items.push(crumb(D.displayName(T[S.sel.i]), D.reference(T[S.sel.i]), () => { }, "rel", D.kindColor(T[S.sel.i])));
    } else if (S.mode === "http") {
      items.push(crumb("HTTP", "All endpoints", () => { S.route = -1; S.sel = null; pushHistory(); rebuild({ fit: true }); }));
      if (S.route >= 0) items.push(crumb(D.ROUTES[S.route].key, D.ROUTES[S.route].id, () => { S.sel = { kind: "item", i: D.nodeOfRoute[S.route] }; applySelection(false); }, null, "l-route"));
      if (S.sel && S.sel.kind === "item" && S.sel.i !== D.nodeOfRoute[S.route]) items.push(crumb(D.displayName(T[S.sel.i]), D.reference(T[S.sel.i]), () => { }, "rel", D.kindColor(T[S.sel.i])));
    } else {
      // scope path
      D.path(S.root).forEach((a) => {
        items.push(crumb(a === D.ROOT ? "Repository" : D.displayName(T[a], true), a === D.ROOT ? D.raw.repository : D.reference(T[a]), () => { if (a !== S.root) enterAncestor(a, S.root); else { S.sel = null; applySelection(false); } }, null, D.kindColor(T[a])));
      });
      if (S.focus) items.push(crumb("Focus: " + D.displayName(T[S.focus.i]), "Leave focus mode", exitFocus, null, D.kindColor(T[S.focus.i])));
      // selection inside the scope
      if (S.sel && S.sel.kind === "item" && !S.focus && D.isAncestor(S.root, S.sel.i)) {
        const rel = D.path(S.sel.i).filter((a) => D.isAncestor(S.root, a));
        rel.forEach((a) => items.push(crumb(D.displayName(T[a], true), D.reference(T[a]), () => selectItem(a, { reveal: true }), "rel", D.kindColor(T[a]))));
      }
    }
    items.forEach((b, k) => {
      if (k) c.appendChild(h("span", { class: "sep", "aria-hidden": "true", text: "›" }));
      if (k === items.length - 1) b.setAttribute("aria-current", "page");
      c.appendChild(b);
    });
  }
  function stepper(label, val, lo, hi, set, title) {
    return h("span", { class: "stepper", title: title || label }, [label,
      h("button", { type: "button", "aria-label": "Less " + label, disabled: val <= lo ? true : null, text: "−", on: { click: () => set(val - 1) } }),
      h("span", { text: String(val) }),
      h("button", { type: "button", "aria-label": "More " + label, disabled: val >= hi ? true : null, text: "+", on: { click: () => set(val + 1) } })]);
  }
  function drawViewbar(hl) {
    const bar = GV.clear(GV.$("viewbar"));
    if (!hl) hl = view && S.sel ? GV.arch.highlight(view, selectedKey(), S.pathDepth || 0) : null;
    if (S.mode === "arch" && S.focus) {
      const f = S.focus;
      bar.appendChild(h("span", { class: "chip accent" }, [GV.icon("focus"), h("strong", { text: D.displayName(T[f.i]) }),
        h("button", { type: "button", class: "icon-btn", title: "Leave focus mode (Esc)", "aria-label": "Leave focus mode", text: "×", on: { click: exitFocus } })]));
      const sym = view && view.symbolMode;
      bar.appendChild(h("span", { class: "chip plain" }, [stepper(sym ? "← callers" : "← dependents", f.up, 0, 6, (v) => { f.up = v; writeHash(); rebuild({ fit: true }); }, "Distance followed backwards")]));
      bar.appendChild(h("span", { class: "chip plain" }, [stepper(sym ? "callees →" : "dependencies →", f.down, 0, 6, (v) => { f.down = v; writeHash(); rebuild({ fit: true }); }, "Distance followed forwards")]));
      if (sym && !D.TYPE[T[f.i].kind]) bar.appendChild(h("span", { class: "chip plain" }, [h("label", { class: "check" }, [h("input", { type: "checkbox", checked: f.refs ? true : null, on: { change: (ev) => { f.refs = ev.target.checked; rebuild({ fit: true }); } } }), "type uses"])]));
      if (view && Object.keys(view.more || {}).length) bar.appendChild(h("span", { class: "chip plain warn", text: "Columns keep the " + GV.focus.PER_COL + " strongest relations" }));
    } else if (S.mode === "arch") {
      if (S.root !== D.ROOT) bar.appendChild(h("span", { class: "chip" }, ["Scope", h("strong", { text: D.displayName(T[S.root], true) }),
        h("button", { type: "button", class: "icon-btn", title: "Back to the whole repository", "aria-label": "Leave the scope", text: "×", on: { click: () => enterAncestor(D.ROOT, S.root) } })]));
      if (hl && S.sel && S.sel.kind === "item") bar.appendChild(h("span", { class: "chip plain", title: "Following relations " + (S.pathDepth ? "one step" : "transitively") + " from the selection" },
        [h("span", { style: { color: GV.token("hl-out") }, text: "↓ " + GV.plural(hl.down, "dependency", "dependencies") }), h("span", { style: { color: GV.token("hl-in") }, text: "↑ " + GV.plural(hl.up, "dependent") }),
          h("span", { class: "seg" }, [h("button", { type: "button", text: "Full path", "aria-pressed": String(!S.pathDepth), on: { click: () => setPathDepth(0) } }),
            h("button", { type: "button", text: "Direct", "aria-pressed": String(S.pathDepth === 1), on: { click: () => setPathDepth(1) } })])]));
      if (!S.filters.ext && D.EXT >= 0) bar.appendChild(h("span", { class: "chip plain", title: "External dependencies are hidden; show them in Filters" }, [h("span", { class: "muted", text: "External hidden" }),
        h("button", { type: "button", class: "btn sm ghost", text: "Show", on: { click: () => { S.filters.ext = true; setFilters(S.filters); } } })]));
    } else if (S.mode === "security" && view) {
      if (S.sec.path) bar.appendChild(h("span", { class: "chip accent" }, [h("strong", { text: "Path: " + S.sec.path.names[0] + " → " + S.sec.path.names[S.sec.path.names.length - 1] }),
        h("button", { type: "button", class: "icon-btn", text: "×", title: "Clear the path", "aria-label": "Clear the path", on: { click: () => { S.sec.path = null; S.sec.pick = null; applySelection(false); } } })]));
      else if (S.sec.pick) bar.appendChild(h("span", { class: "chip plain", text: "Shift+click another step to explain the path from the selected one" }));
      else bar.appendChild(h("span", { class: "chip plain muted", text: "Click a step for its code; Shift+click a second step to explain the path between them" }));
    } else if (S.mode === "http" && S.route >= 0) {
      bar.appendChild(h("span", { class: "chip plain" }, [stepper("Depth", S.jdepth, 0, 8, (v) => { S.jdepth = v; S.jexp = {}; writeHash(); rebuild({ fit: true }); }, "Calls followed from the handler chain")]));
      bar.appendChild(h("span", { class: "chip plain" }, [h("label", { class: "check" }, [h("input", { type: "checkbox", checked: S.httpExt ? true : null, on: { change: (ev) => { S.httpExt = ev.target.checked; rebuild({ fit: true }); } } }), "external calls"])]));
      if (view && view.hidden) bar.appendChild(h("span", { class: "chip plain", title: "Double-click a step with +N to open it" }, [h("span", { class: "muted", text: GV.plural(view.hidden, "call") + " collapsed" })]));
    }
  }
  function drawLegend(hl) {
    const l = GV.clear(GV.$("legend"));
    if (!view || !view.scene.edges.length) return;
    const confs = {};
    view.scene.edges.forEach((e) => { confs[e.conf] = 1; });
    D.CONFS.forEach((c) => { if (confs[c]) l.appendChild(h("span", { title: "Line style: " + c }, [h("span", { class: "ln " + c }), c])); });
    if (hl && (hl.down || hl.up)) {
      l.appendChild(h("span", null, [h("span", { class: "ln out" }), "depends on"]));
      l.appendChild(h("span", null, [h("span", { class: "ln in" }), "used by"]));
    }
  }
  function drawStatus() {
    const el = GV.$("status-view");
    const st = GV.layout.stats;
    if (!view) { el.textContent = S.mode === "http" ? "HTTP · " + GV.plural(D.ROUTES.length, "route") : ""; return; }
    const c = view.counts;
    el.textContent = (S.mode === "http" ? "HTTP journey" : S.mode === "security" ? "Security Journey" : S.focus ? "Focus" : "Architecture") + " · " + GV.plural(c.boxes, "element") + " · " + GV.plural(c.edges, "line") +
      (view.kind === "arch" && c.context ? " · " + c.context + " outside" : "") + " · layout " + Math.round(st.last) + " ms";
    const comp = D.raw.completeness;
    const an = GV.$("status-analysis");
    an.textContent = comp && comp.status !== "complete" ? "analysis " + comp.status : "";
    an.className = comp && comp.status !== "complete" ? "warn" : "";
    an.title = comp && comp.reasons ? comp.reasons.slice(0, 10).join("\n") : "";
  }

  // ---------- canvas events ----------
  GV.on("canvas:box", ({ box, ev }) => {
    if (box.kind === "more") { GV.toast(box.tip || "More elements at this distance"); return; }
    if (S.mode === "security") {
      if (box.findingId) { S.sel = null; S.sec.path = null; S.sec.pick = null; if (S.sec.finding !== box.findingId) openFinding(box.findingId); else applySelection(false); return; }
      if (ev && ev.shiftKey && S.sec.pick && S.sec.pick !== box.key && view) {
        const keys = GV.security.pathKeys(view, S.sec.pick, box.key) || GV.security.pathKeys(view, box.key, S.sec.pick);
        const ends = keys ? [GV.render.box(keys[0]), GV.render.box(keys[keys.length - 1])] : [];
        if (!keys || ends.some((b) => !b || b.ref === undefined)) { GV.toast("No path between these steps in the drawn graph."); return; }
        S.sel = null;
        S.sec.path = { from: T[ends[0].ref].id, to: T[ends[1].ref].id, keys, names: keys.map((k) => GV.render.box(k).title) };
        applySelection(false);
        return;
      }
      S.sec.path = null;
      S.sec.pick = box.key;
    }
    if (box.ref === undefined) return;
    if (S.sel && S.sel.kind === "item" && S.sel.i === box.ref && box.kind !== "group") { applySelection(false); return; }
    S.sel = { kind: "item", i: box.ref };
    S.layerPick = null;
    applySelection(false);
  });
  GV.on("canvas:edge", (e) => selectEdge(e));
  GV.on("canvas:empty", () => clearSelection());
  GV.on("canvas:dblbox", ({ box }) => {
    if (box.ref === undefined) return;
    const t = T[box.ref];
    if (S.mode === "http") {
      if (!view || view.root === box.ref) return;
      // a step with hidden callees opens; an open one collapses
      S.jexp[box.ref] = view.more.has(box.ref);
      S.sel = { kind: "item", i: box.ref };
      rebuild({ animate: true, anchor: box.key });
      return;
    }
    if (S.focus) { if (box.ref !== S.focus.i) focusOn(box.ref); return; }
    if (box.kind === "context") { selectItem(box.ref, { reveal: true, force: true }); return; }
    // functions open in focus mode; containers and types expand in place
    if (GV.arch.expandable(t) && !D.FUNC[t.kind]) toggle(box.ref);
    else focusOn(box.ref);
  });

  // ---------- toolbar, panels, keys ----------
  GV.$("toolbar").addEventListener("click", (ev) => {
    const b = ev.target.closest("button[data-act]");
    if (!b) return;
    const act = b.dataset.act;
    if (act === "zoom-in") GV.render.zoomBy(1.25);
    else if (act === "zoom-out") GV.render.zoomBy(0.8);
    else if (act === "fit") fitView();
    else if (act === "focus") toggleFocus();
    else if (act === "minimap") toggleMinimap();
    else if (act === "fullscreen") toggleFullscreen();
  });
  function toggleFocus() {
    if (S.focus) { exitFocus(); return; }
    if (S.sel && S.sel.kind === "item") focusOn(S.sel.i);
    else GV.toast("Select an element first: focus mode draws it with what it is connected to.");
  }
  function toggleMinimap() {
    const on = document.body.classList.toggle("no-minimap");
    GV.render.setMinimap(!on);
    GV.pref.set("minimap", !on);
    const b = document.querySelector('[data-act="minimap"]');
    if (b) b.setAttribute("aria-pressed", String(!on));
  }
  function toggleFullscreen() {
    try {
      if (document.fullscreenElement) document.exitFullscreen();
      else if (document.documentElement.requestFullscreen) document.documentElement.requestFullscreen();
    } catch (e) { GV.toast("Fullscreen is not available here."); }
  }
  document.querySelectorAll("#modes button").forEach((b) => b.addEventListener("click", () => setMode(b.dataset.mode)));
  GV.$("help-open").addEventListener("click", () => openDialog("help"));
  function openDialog(id) {
    const d = GV.$(id);
    d.hidden = false;
    const close = d.querySelector("[data-close]");
    if (close) { close.onclick = () => { d.hidden = true; }; close.focus(); }
    d.onmousedown = (ev) => { if (ev.target === d) d.hidden = true; };
  }

  function panel(name, on, keep) {
    const cls = name === "sidebar" ? "no-sidebar" : "no-inspector";
    document.body.classList.toggle(cls, !on);
    GV.$(name + "-reopen").hidden = on;
    if (!keep) GV.pref.set(name, on);
  }
  // On a narrow screen the panels cover the canvas: they start closed.
  const narrow = () => window.innerWidth <= 820;
  GV.$("sidebar-collapse").addEventListener("click", () => panel("sidebar", false));
  GV.$("sidebar-reopen").addEventListener("click", () => panel("sidebar", true));
  GV.$("inspector-collapse").addEventListener("click", () => panel("inspector", false));
  GV.$("inspector-reopen").addEventListener("click", () => panel("inspector", true));
  function resizer(handle, varName, sign, min, maxFrac, pref) {
    handle.addEventListener("pointerdown", (ev) => {
      ev.preventDefault();
      handle.setPointerCapture(ev.pointerId);
      handle.classList.add("dragging");
      document.body.classList.add("resizing");
      const start = ev.clientX, w0 = parseFloat(getComputedStyle(document.documentElement).getPropertyValue(varName)) || 300;
      const move = (e) => {
        const w = Math.max(min, Math.min(window.innerWidth * maxFrac, w0 + sign * (e.clientX - start)));
        document.documentElement.style.setProperty(varName, Math.round(w) + "px");
      };
      const upFn = () => {
        handle.removeEventListener("pointermove", move);
        handle.removeEventListener("pointerup", upFn);
        handle.classList.remove("dragging");
        document.body.classList.remove("resizing");
        GV.pref.set(pref, parseFloat(getComputedStyle(document.documentElement).getPropertyValue(varName)));
      };
      handle.addEventListener("pointermove", move);
      handle.addEventListener("pointerup", upFn);
    });
    handle.addEventListener("dblclick", () => { document.documentElement.style.removeProperty(varName); GV.pref.set(pref, null); });
  }
  resizer(GV.$("sidebar-resize"), "--sidebar-w", 1, 200, 0.4, "sidebarW");
  resizer(GV.$("inspector-resize"), "--inspector-w", -1, 300, 0.7, "inspectorW");

  document.addEventListener("keydown", (ev) => {
    const tag = (ev.target.tagName || "").toLowerCase();
    const typing = tag === "input" || tag === "textarea" || tag === "select" || ev.target.isContentEditable;
    if ((ev.ctrlKey || ev.metaKey) && (ev.key === "k" || ev.key === "K")) { GV.search.open(); ev.preventDefault(); return; }
    if (ev.key === "Escape") {
      if (GV.search.isOpen()) { GV.search.close(); return; }
      const dlg = document.querySelector(".dialog:not([hidden])");
      if (dlg) { dlg.hidden = true; return; }
      if (GV.filters.isOpen()) { GV.filters.toggle(false); return; }
      if (typing) return;
      if (S.sel || S.layerPick) clearSelection();
      else if (S.focus) exitFocus();
      return;
    }
    if (typing || GV.search.isOpen()) return;
    if (ev.altKey && ev.key === "ArrowLeft") { history.back(); ev.preventDefault(); return; }
    if (ev.altKey && ev.key === "ArrowRight") { history.forward(); ev.preventDefault(); return; }
    if (ev.ctrlKey || ev.metaKey || ev.altKey) return;
    const k = ev.key;
    if (k === "/") { GV.search.open(); ev.preventDefault(); }
    else if (k === "1") setMode("arch");
    else if (k === "2") setMode("http");
    else if (k === "3") setMode("security");
    else if (k === "f") fitView();
    else if (k === "F") toggleFullscreen();
    else if (k === "+" || k === "=") GV.render.zoomBy(1.25);
    else if (k === "-" || k === "_") GV.render.zoomBy(0.8);
    else if (k === "m") toggleMinimap();
    else if (k === ".") toggleFocus();
    else if (k === "b") panel("sidebar", document.body.classList.contains("no-sidebar"));
    else if (k === "i") panel("inspector", document.body.classList.contains("no-inspector"));
    else if (k === "?") openDialog("help");
    else if (k === "Backspace") { up(); ev.preventDefault(); }
    else if (k === "Enter" && S.sel && S.sel.kind === "item" && ev.target.id !== "tree") {
      if (S.mode === "http") GV.emit("canvas:dblbox", { box: GV.render.box(selectedKey()) || {} });
      else toggle(S.sel.i);
    } else return;
  });
  // The layout follows the stage's proportions: lay out again and fit.
  window.addEventListener("resize", GV.debounce(() => rebuild({ fit: true }), 300));

  // ---------- preferences ----------
  const theme = GV.pref.get("theme", null);
  if (theme === "light" || theme === "dark") { document.documentElement.setAttribute("data-theme", theme); GV.resetTokens(); }
  const sw = GV.pref.get("sidebarW", null), iw = GV.pref.get("inspectorW", null);
  if (sw) document.documentElement.style.setProperty("--sidebar-w", sw + "px");
  if (iw) document.documentElement.style.setProperty("--inspector-w", iw + "px");
  if (narrow()) { panel("sidebar", false, true); panel("inspector", false, true); }
  else {
    if (GV.pref.get("sidebar", true) === false) panel("sidebar", false);
    if (GV.pref.get("inspector", true) === false) panel("inspector", false);
  }
  if (GV.pref.get("minimap", true) === false) { document.body.classList.add("no-minimap"); GV.render.setMinimap(false); }
  else { const b = document.querySelector('[data-act="minimap"]'); if (b) b.setAttribute("aria-pressed", "true"); }

  GV.app = {
    state: () => S, view: () => view, selectItem, selectEdge, clearSelection, toggle, enter, focusOn, exitFocus, setMode, openRoute,
    openFinding, openSecRoute, refreshSecurity,
    setJourney, setFilters, setPathDepth, setTheme, isDrawn, linkTo, noteTab: () => writeHash(), ready: () => ready,
    rebuild: () => rebuild({ fit: true }),
    // idle resolves when the view being built is on the canvas
    idle: () => lastBuild.then(() => lastBuild)
  };
  // Read-only hook for automated checks of the page.
  window.__gitview = { app: GV.app, data: D, render: GV.render, layout: GV.layout, requests: GV.requests, search: GV.search, source: GV.source, tree: GV.tree, inspector: GV.inspector };
  restore();
})(GV);
