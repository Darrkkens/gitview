// Inspector (right panel): what is selected, in tabs.
//   element   Overview · Connections · Source · Metadata
//   relation  the relations a drawn line stands for, with their evidence
//   route     Overview · Request · Response · Flow · Source · Evidence
//   nothing   the repository (or the HTTP catalog) in numbers
(function (GV) {
  "use strict";
  const D = GV.data, N = D.N, E = D.E, T = D.T, h = GV.h;
  const head = GV.$("insp-head"), tabsEl = GV.$("insp-tabs"), body = GV.$("insp-body");
  let target = null;           // {type: "item"|"edge"|"route", …}
  const tabs = { item: "overview", route: "overview" };
  const I = { confs: {}, bodyView: "example", respOpen: {}, showAll: {} };

  // ---------- small parts ----------
  function badge(text, cls, title) { return h("span", { class: "badge " + (cls || ""), text, title: title || null }); }
  function confBadge(c) { return badge(c, "conf-" + c, CONF_HELP[c]); }
  const CONF_HELP = {
    confirmed: "confirmed: a static call or a relation read from the types",
    likely: "likely: the only implementation of an interface method in the analysed code",
    possible: "possible: one of several implementations, or a function passed as a value",
    unresolved: "unresolved: no static target (call through a function value or reflection)"
  };
  function kdot(t, round) { const d = h("span", { class: "kdot" + (round ? " round" : "") }); d.style.background = GV.token(D.kindColor(t)); return d; }
  function section(title, count, extra) {
    const s = h("div", { class: "section" });
    if (title) s.appendChild(h("div", { class: "section-title" }, [title, count != null ? h("span", { class: "count", text: GV.fmt(count) }) : null, extra ? h("span", { class: "grow" }) : null, extra || null]));
    return s;
  }
  function kv(rows) {
    const dl = h("dl", { class: "kv" });
    rows.forEach((r) => {
      if (!r || r[1] === "" || r[1] == null) return;
      dl.appendChild(h("dt", { text: r[0] }));
      dl.appendChild(typeof r[1] === "string" || typeof r[1] === "number" ? h("dd", { text: String(r[1]) }) : h("dd", null, [r[1]]));
    });
    return dl;
  }
  function itemLink(i, text) {
    return h("a", { href: "#", class: "mono", text: text || D.displayName(T[i]), title: D.reference(T[i]), on: { click: (ev) => { ev.preventDefault(); GV.app.selectItem(i, { reveal: true }); } } });
  }
  function rowItem(i, sub, right, onClick) {
    const t = T[i];
    return h("div", { class: "conn", tabindex: "0", title: D.reference(t),
      on: { click: onClick || (() => GV.app.selectItem(i, { reveal: true })), keydown: (ev) => { if (ev.key === "Enter") (onClick || (() => GV.app.selectItem(i, { reveal: true })))(); } } },
      [kdot(t, D.FUNC[t.kind]), h("div", { class: "two" }, [h("span", { class: "name", text: D.displayName(t) }), sub ? h("span", { class: "sub", text: sub }) : null]), right || null]);
  }
  function tabBar(list, cur, set) {
    GV.clear(tabsEl);
    list.forEach(([id, label, count]) => {
      tabsEl.appendChild(h("button", { type: "button", role: "tab", "aria-selected": String(cur === id), on: { click: () => { set(id); draw(); } } },
        [label, count != null ? h("span", { class: "count", text: GV.fmt(count) }) : null]));
    });
  }

  // ---------- relations of an item ----------
  // For a symbol, its own relations; for a container, the relations of
  // everything inside it with the outside.
  const relCache = new Map();
  function relationsOf(i) {
    if (relCache.has(i)) return relCache.get(i);
    const t = T[i];
    const out = [], inn = [];
    if (t.n && !t.kids.length) {
      t.n.out.forEach((ei) => { if (!D.STRUCTURAL[E[ei].type]) out.push(ei); });
      t.n.inn.forEach((ei) => { if (!D.STRUCTURAL[E[ei].type]) inn.push(ei); });
    } else {
      const inside = new Set();
      if (t.n) inside.add(i);
      D.descendants(i, (d) => { if (T[d].n) inside.add(d); });
      inside.forEach((x) => {
        N[x].out.forEach((ei) => { const e = E[ei]; if (!D.STRUCTURAL[e.type] && !inside.has(e.t)) out.push(ei); });
        N[x].inn.forEach((ei) => { const e = E[ei]; if (!D.STRUCTURAL[e.type] && !inside.has(e.s)) inn.push(ei); });
      });
    }
    const r = { out, inn };
    relCache.set(i, r);
    return r;
  }
  function confBar(list) {
    const c = {};
    list.forEach((ei) => { c[E[ei].conf] = (c[E[ei].conf] || 0) + 1; });
    const total = list.length || 1;
    const bar = h("div", { class: "bar", title: D.CONFS.map((k) => (c[k] || 0) + " " + k).join(" · ") });
    D.CONFS.forEach((k) => { if (c[k]) { const s = h("span"); s.style.width = (100 * c[k] / total) + "%"; s.style.background = GV.token("c-" + k); bar.appendChild(s); } });
    const legend = h("div", { class: "btn-row" }, D.CONFS.filter((k) => c[k]).map((k) => h("span", { class: "badge conf-" + k, title: CONF_HELP[k], text: GV.fmt(c[k]) + " " + k })));
    return [bar, legend];
  }

  // ---------- header ----------
  function drawHead(kind, title, locText, actions, badges, titlePrefix) {
    GV.clear(head);
    head.appendChild(h("div", { class: "insp-kind" }, badges));
    head.appendChild(h("div", { class: "insp-title" }, [titlePrefix || null, h("span", { text: title })]));
    if (locText) head.appendChild(h("div", { class: "insp-loc", title: locText }, [h("span", { class: "path", text: "‎" + locText + "‎" })]));
    if (actions && actions.length) head.appendChild(h("div", { class: "insp-actions" }, actions));
  }

  function itemActions(i) {
    const t = T[i], n = t.n, acts = [];
    acts.push(GV.copyButton("Name", () => D.displayName(t)));
    if (n && n.file) acts.push(GV.copyButton("Path", () => GV.loc(n), "Copy file:line"));
    acts.push(GV.copyButton("Reference", () => D.reference(t), "Copy the fully qualified reference"));
    const st = GV.app.state();
    if (st.mode === "arch" && !st.focus && GV.arch.expandable(t) && GV.app.isDrawn(i) && i !== st.root) {
      const opened = st.open.has(i);
      acts.push(h("button", { type: "button", class: "btn sm", title: (opened ? "Collapse" : "Expand") + " on the canvas (Enter)", on: { click: () => GV.app.toggle(i) } },
        [GV.icon(opened ? "collapse" : "expand"), opened ? "Collapse" : "Expand"]));
    }
    if (st.mode === "arch" && !st.focus && GV.arch.expandable(t) && i !== st.root && !(t.n && D.SYMBOL[t.kind]))
      acts.push(h("button", { type: "button", class: "btn sm", title: "Make it the scope of the canvas", on: { click: () => GV.app.enter(i) } }, [GV.icon("target"), "Open as scope"]));
    if (i !== D.ROOT && t.kind !== "extgroup" && !(st.focus && st.focus.i === i))
      acts.push(h("button", { type: "button", class: "btn sm", title: "Focus mode: only this element and what it is connected to (.)", on: { click: () => GV.app.focusOn(i) } }, [GV.icon("focus"), "Focus"]));
    if (n && n.type === "route" && D.routeOfNode.has(i))
      acts.push(h("button", { type: "button", class: "btn sm primary", on: { click: () => GV.app.openRoute(D.routeOfNode.get(i)) } }, [GV.icon("http"), "HTTP journey"]));
    if (!GV.app.isDrawn(i) && st.mode === "arch")
      acts.push(h("button", { type: "button", class: "btn sm", title: "Show it on the canvas", on: { click: () => GV.app.selectItem(i, { reveal: true, force: true }) } }, [GV.icon("target"), "Show on canvas"]));
    return acts;
  }
  function itemBadges(t) {
    const n = t.n;
    const b = [kdot(t, D.FUNC[t.kind]), badge(D.kindLabel(t), "kind")];
    if (n && n.ext) b.push(badge("external", "kind"));
    const lyr = n ? GV.http.layerOf(n) : "";
    if (lyr && lyr !== "External" && lyr !== "Route" && lyr !== "Database" && lyr !== "Unresolved")
      b.push(badge(lyr + (n.meta.layerBasis ? " · " + n.meta.layerBasis.toLowerCase() : ""), "", n.meta.layerEvidence || "role in the handler chain"));
    if (n && n.meta.exported === false) b.push(badge("unexported", "kind"));
    return b;
  }

  // ---------- element ----------
  function drawItem(i) {
    const t = T[i], n = t.n;
    const rel = relationsOf(i);
    const loc = n && n.file ? n.file + (n.line ? ":" + n.line + (n.end && n.end !== n.line ? "–" + n.end : "") : "") : t.kind === "dir" ? t.name : n && n.type === "package" ? n.name : "";
    drawHead("item", D.displayName(t), loc, itemActions(i), itemBadges(t));
    const hasSource = n && (n.file || n.ext) && n.type !== "package" && n.type !== "module";
    tabBar([["overview", "Overview"], ["connections", "Connections", rel.out.length + rel.inn.length], hasSource ? ["source", "Source"] : null, ["metadata", "Metadata"]].filter(Boolean),
      tabs.item, (id) => { tabs.item = id; });
    if (tabs.item === "source" && !hasSource) tabs.item = "overview";
    GV.clear(body);
    ({ overview: itemOverview, connections: itemConnections, source: itemSource, metadata: itemMetadata }[tabs.item] || itemOverview)(i);
  }

  function itemOverview(i) {
    const t = T[i], n = t.n, m = n ? n.meta : {};
    if (n && n.sig) { const s = section("Signature"); s.appendChild(GV.source.inline(n.sig)); body.appendChild(s); }
    if (n && m.sql) { const s = section("SQL" + (m.sqlDynamic ? " (built at runtime)" : "")); s.appendChild(h("pre", { class: "sig", text: m.sql.replace(/^\s*\n/, "").replace(/\s+$/, "") })); body.appendChild(s); }
    const s = section("Details");
    const rows = [["Kind", D.kindLabel(t) + (n && n.ext ? " (external)" : "")]];
    if (n) {
      if (n.qual && n.qual !== n.name) rows.push(["Qualified", n.qual]);
      if (n.type !== "package" && n.type !== "module" && n.pkg) rows.push(["Package", n.pkg]);
      if (n.type === "package") rows.push(["Import path", n.name]);
      if (n.file && n.type !== "package") rows.push(["File", n.file]);
      if (n.line) rows.push(["Lines", n.end && n.end !== n.line ? n.line + "–" + n.end : String(n.line)]);
      if (m.receiver) rows.push(["Receiver", m.receiver + (m.pointerReceiver ? " (pointer)" : " (value)")]);
      if (m.role) rows.push(["Role", m.role + " (registered in a handler chain)"]);
      if (m.layer) rows.push(["Layer", m.layer + " · " + (m.layerBasis || "") + (m.layerEvidence ? " — " + m.layerEvidence : "")]);
      if (n.type === "route") rows.push(["Route", (m.method || "ANY") + " " + m.path + " · " + m.router + (m.partial ? " · partially resolved" : "")]);
      if (n.type === "db") rows.push(["Operation", (m.lib || "") + " " + (m.op || "")]);
      if (m.std !== undefined) rows.push(["Standard library", m.std ? "yes" : "no"]);
      if (m.note) rows.push(["Note", m.note]);
      if (n.comm >= 0 && D.COMM[n.comm]) rows.push(["Community", D.COMM[n.comm].label + " (" + D.COMM[n.comm].level + " level)"]);
    } else if (t.kind === "dir") rows.push(["Path", t.name]);
    s.appendChild(kv(rows));
    body.appendChild(s);
    if (t.kids.length) {
      const c = {};
      D.descendants(i, (d) => { c[T[d].kind] = (c[T[d].kind] || 0) + 1; });
      const ms = section("Contains");
      const grid = h("div", { class: "metrics" });
      Object.keys(c).sort((a, b) => (D.KIND[a] || {}).order - (D.KIND[b] || {}).order).slice(0, 9).forEach((k) => {
        grid.appendChild(h("div", { class: "metric" }, [h("div", { class: "v", text: GV.fmt(c[k]) }), h("div", { class: "l", text: (D.KIND[k] || { label: k }).label + (c[k] > 1 ? "s" : "") })]));
      });
      ms.appendChild(grid);
      body.appendChild(ms);
    }
    const rel = relationsOf(i);
    const rs = section("Relations", rel.out.length + rel.inn.length);
    rs.appendChild(kv([["Depends on", GV.plural(rel.out.length, "relation") + (t.kids.length ? " leaving it" : "")], ["Used by", GV.plural(rel.inn.length, "relation") + (t.kids.length ? " reaching it" : "")]]));
    if (rel.out.length + rel.inn.length) GV.append(rs, confBar(rel.out.concat(rel.inn)));
    body.appendChild(rs);
    if (n && (n.type === "package" || t.kind === "dir" || t.kind === "module" || n.type === "file") && D.ROUTES.length) {
      const list = GV.http.routesOfPackage(i);
      if (list.length) {
        const r = section("HTTP routes", list.length);
        list.slice(0, 40).forEach((k) => {
          const rt = D.ROUTES[k];
          r.appendChild(h("div", { class: "conn", on: { click: () => GV.app.openRoute(k) } }, [h("span", { class: "method m-" + rt.method, text: rt.method }), h("div", { class: "two" }, [h("span", { class: "name mono", text: rt.path })])]));
        });
        if (list.length > 40) r.appendChild(h("p", { class: "note", text: (list.length - 40) + " more in the HTTP mode." }));
        body.appendChild(r);
      }
    }
    if (m.annotations && m.annotations.length) {
      const a = section("Annotations", m.annotations.length);
      m.annotations.forEach((x) => a.appendChild(h("p", { class: "note", text: "[" + x.kind + (x.severity ? "/" + x.severity : "") + "] " + x.title + (x.details ? " — " + x.details : "") })));
      body.appendChild(a);
    }
  }

  function itemConnections(i) {
    const t = T[i];
    const rel = relationsOf(i);
    const container = t.kids.length > 0;
    const filt = h("div", { class: "conf-filter", role: "group", "aria-label": "Confidence" });
    D.CONFS.forEach((c) => {
      filt.appendChild(h("button", { type: "button", "aria-pressed": String(I.confs[c] !== false), title: CONF_HELP[c],
        on: { click: () => { I.confs[c] = I.confs[c] === false; draw(); } } }, [c]));
    });
    const top = section(null);
    top.appendChild(filt);
    top.appendChild(h("p", { class: "note", text: container
      ? "Relations between what this " + D.kindLabel(t) + " contains and the rest, grouped by the package (or file) on the other side."
      : "Relations of this element in the dataset, each with the place in the code it was read from." }));
    body.appendChild(top);
    [["Outgoing · depends on", rel.out, 1], ["Incoming · used by", rel.inn, -1]].forEach(([title, list, dir]) => {
      list = list.filter((ei) => I.confs[E[ei].conf] !== false);
      const s = section(title, list.length);
      if (!list.length) s.appendChild(h("p", { class: "note", text: "None" + (rel.out.length + rel.inn.length ? " with the selected confidences." : ".") }));
      else if (container) groupedByUnit(s, list, dir, i);
      else groupedByType(s, list, dir, i);
      body.appendChild(s);
    });
  }
  function otherEnd(e, dir) { return dir > 0 ? e.t : e.s; }
  function evidenceText(e) { return (e.file ? e.file + ":" + e.line : "") + (e.w > 1 ? " · " + e.w + " sites" : "") + (e.note ? " · " + e.note : ""); }
  function groupedByType(s, list, dir, self) {
    const by = {};
    list.forEach((ei) => { (by[E[ei].type] = by[E[ei].type] || []).push(ei); });
    Object.keys(by).sort((a, b) => (D.REL[a] || {}).order - (D.REL[b] || {}).order).forEach((type) => {
      const g = h("div", { class: "conn-group" });
      const rel = D.REL[type] || { label: type, inv: type };
      g.appendChild(h("div", { class: "conn-group-title" }, [h("span", { class: "rel", text: dir > 0 ? rel.label : rel.inv }), h("span", { text: String(by[type].length) })]));
      const items = by[type].sort((a, b) => D.CONF_RANK[E[b].conf] - D.CONF_RANK[E[a].conf] || (T[otherEnd(E[a], dir)].label < T[otherEnd(E[b], dir)].label ? -1 : 1));
      const key = self + ":" + dir + ":" + type;
      const max = I.showAll[key] ? items.length : 60;
      items.slice(0, max).forEach((ei) => {
        const e = E[ei], o = otherEnd(e, dir);
        g.appendChild(rowItem(o, evidenceText(e), confBadge(e.conf)));
      });
      if (items.length > max) g.appendChild(h("button", { type: "button", class: "btn sm ghost", text: "Show all " + items.length, on: { click: () => { I.showAll[key] = true; draw(); } } }));
      s.appendChild(g);
    });
  }
  function groupedByUnit(s, list, dir, self) {
    const level = T[self].kind === "file" ? "file" : "package";
    const by = new Map();
    list.forEach((ei) => {
      const o = otherEnd(E[ei], dir);
      let u = -1;
      for (let p = o; p >= 0; p = T[p].parent) if (T[p].kind === level || T[p].kind === "package") { u = p; break; }
      if (u < 0) u = o;
      if (!by.has(u)) by.set(u, []);
      by.get(u).push(ei);
    });
    const units = Array.from(by.keys()).sort((a, b) => by.get(b).length - by.get(a).length || (T[a].label < T[b].label ? -1 : 1));
    const key = self + ":" + dir;
    const max = I.showAll[key] ? units.length : 40;
    units.slice(0, max).forEach((u) => {
      const items = by.get(u);
      const types = {};
      let best = "unresolved";
      items.forEach((ei) => { types[E[ei].type] = (types[E[ei].type] || 0) + 1; if (D.CONF_RANK[E[ei].conf] > D.CONF_RANK[best]) best = E[ei].conf; });
      const sum = Object.keys(types).sort((a, b) => types[b] - types[a]).map((k) => types[k] + " " + ((D.REL[k] || {}).label || k)).join(" · ");
      const d = h("details", { class: "fold" });
      const sm = h("summary");
      sm.appendChild(rowItem(u, sum, confBadge(best), () => GV.app.selectItem(u, { reveal: true })));
      d.appendChild(sm);
      d.addEventListener("toggle", () => {
        if (!d.open || d.dataset.filled) return;
        d.dataset.filled = "1";
        items.sort((a, b) => D.CONF_RANK[E[b].conf] - D.CONF_RANK[E[a].conf]).slice(0, 120).forEach((ei) => {
          const e = E[ei];
          const from = T[e.s], to = T[e.t];
          d.appendChild(h("div", { class: "conn", on: { click: () => GV.app.selectItem(dir > 0 ? e.t : e.s, { reveal: true }) } }, [
            h("div", { class: "two" }, [h("span", { class: "name mono", text: D.displayName(from) + " → " + D.displayName(to) }), h("span", { class: "sub", text: ((D.REL[e.type] || {}).label || e.type) + " · " + evidenceText(e) })]),
            confBadge(e.conf)]));
        });
        if (items.length > 120) d.appendChild(h("p", { class: "note", text: (items.length - 120) + " more (all in the JSON export)." }));
      });
      s.appendChild(d);
    });
    if (units.length > max) s.appendChild(h("button", { type: "button", class: "btn sm ghost", text: "Show all " + units.length, on: { click: () => { I.showAll[key] = true; draw(); } } }));
  }

  function itemSource(i) {
    const s = section(null);
    const box = h("div");
    s.appendChild(box);
    body.appendChild(s);
    GV.source.render(box, T[i].n);
  }

  function itemMetadata(i) {
    const t = T[i], n = t.n;
    const s = section("Identity");
    s.appendChild(kv([["ID", h("span", { class: "mono", text: t.id })], ["Reference", h("span", { class: "mono", text: D.reference(t) })],
      n && n.meta.flowId ? ["Code Flow ID", h("span", { class: "mono", text: n.meta.flowId })] : null]));
    s.appendChild(h("div", { class: "btn-row", style: { marginTop: "8px" } }, [GV.copyButton("ID", () => t.id), GV.copyButton("Link", () => GV.app.linkTo(i), "Copy a link to this element in this page")]));
    body.appendChild(s);
    if (n) {
      const m = section("Graph");
      m.appendChild(kv([["Degree", n.deg], ["Out", n.out.length], ["In", n.inn.length],
        n.comm >= 0 && D.COMM[n.comm] ? ["Community", D.COMM[n.comm].label + " · #" + n.comm + " · " + D.COMM[n.comm].level + " level · " + D.COMM[n.comm].size + " members"] : ["Community", "not clustered"],
        ["Tree parent", T[t.parent] ? D.reference(T[t.parent]) : ""]]));
      body.appendChild(m);
      const keys = Object.keys(n.meta).filter((k) => k !== "annotations" && k !== "sql").sort();
      if (keys.length) {
        const a = section("Attributes", keys.length);
        a.appendChild(kv(keys.map((k) => [k, typeof n.meta[k] === "object" ? JSON.stringify(n.meta[k]) : String(n.meta[k])])));
        body.appendChild(a);
      }
    }
    body.appendChild(h("p", { class: "note", style: { padding: "0 12px" }, text: "Everything here comes from static analysis (go/packages, go/types, SSA); nothing was executed." }));
  }

  // ---------- relation (drawn line) ----------
  function drawEdge(se) {
    const g = se.agg;
    const from = refOfKey(se.s), to = refOfKey(se.t);
    drawHead("edge", (from >= 0 ? D.displayName(T[from]) : "?") + " → " + (to >= 0 ? D.displayName(T[to]) : "?"), "", [
      from >= 0 ? h("button", { type: "button", class: "btn sm", text: "Select source", on: { click: () => GV.app.selectItem(from, { reveal: true }) } }) : null,
      to >= 0 ? h("button", { type: "button", class: "btn sm", text: "Select target", on: { click: () => GV.app.selectItem(to, { reveal: true }) } }) : null
    ].filter(Boolean), [badge("relation", "kind"), confBadge(g.best), badge(GV.plural(g.n, "relation"), "kind")]);
    GV.clear(tabsEl);
    GV.clear(body);
    const s = section("Summary");
    s.appendChild(kv(Object.keys(g.types).map((k) => [(D.REL[k] || { label: k }).label, GV.fmt(g.types[k])])));
    GV.append(s, confBar(g.real));
    s.appendChild(h("p", { class: "note", text: "The line stands for every relation between what the two boxes contain; its style is the strongest confidence among them." }));
    body.appendChild(s);
    const l = section("Relations", g.real.length);
    const list = g.real.slice().sort((a, b) => D.CONF_RANK[E[b].conf] - D.CONF_RANK[E[a].conf] || a - b);
    list.slice(0, 200).forEach((ei) => {
      const e = E[ei];
      l.appendChild(h("div", { class: "conn", on: { click: () => GV.app.selectItem(e.t, { reveal: true }) } }, [
        h("div", { class: "two" }, [h("span", { class: "name mono", text: D.displayName(T[e.s]) + " → " + D.displayName(T[e.t]) }),
          h("span", { class: "sub", text: ((D.REL[e.type] || {}).label || e.type) + " · " + evidenceText(e) })]), confBadge(e.conf)]));
    });
    if (list.length > 200) l.appendChild(h("p", { class: "note", text: (list.length - 200) + " more (all in the JSON export)." }));
    body.appendChild(l);
  }
  function refOfKey(k) { const b = GV.render.box(k); return b && b.ref !== undefined ? b.ref : -1; }

  // ---------- nothing selected ----------
  function drawSummary() {
    const st = GV.app.state();
    GV.clear(tabsEl);
    GV.clear(body);
    if (st.mode === "http") return drawHTTPSummary();
    const r = D.raw, s = r.stats;
    drawHead("repo", r.repository, D.modules.map((m) => m.name).join(", "), [], [badge("repository", "kind")]);
    const g = section("Repository");
    const grid = h("div", { class: "metrics" });
    [["Modules", s.modules], ["Packages", s.packages], ["Files", s.files], ["Functions", s.functions], ["Types", s.types], ["Routes", s.routes || 0],
      ["External packages", s.externalPackages], ["Nodes", s.nodes], ["Relations", s.edges]].forEach(([l, v]) => {
      grid.appendChild(h("div", { class: "metric" }, [h("div", { class: "v", text: GV.fmt(v) }), h("div", { class: "l", text: l })]));
    });
    g.appendChild(grid);
    body.appendChild(g);
    const all = E.map((e) => e.i).filter((ei) => !D.STRUCTURAL[E[ei].type]);
    const c = section("Confidence of the relations", all.length);
    GV.append(c, confBar(all));
    D.CONFS.forEach((k) => c.appendChild(h("p", { class: "note", text: CONF_HELP[k] })));
    body.appendChild(c);
    if (r.completeness && r.completeness.status !== "complete") {
      const w = section("Completeness");
      w.appendChild(h("p", { class: "note warn", text: "The analysis is " + r.completeness.status + ":" }));
      (r.completeness.reasons || []).slice(0, 30).forEach((x) => w.appendChild(h("p", { class: "note", text: "• " + x })));
      body.appendChild(w);
    }
    const m = section("How it was computed");
    (r.method || []).forEach((x) => m.appendChild(h("p", { class: "note", text: "• " + x })));
    const src = D.raw.sources || {};
    m.appendChild(h("p", { class: "note", text: src.embedded ? "Source: " + GV.plural(src.files, "Go file") + " embedded for the Source tab" + ((src.skipped || []).length ? " (" + src.skipped.length + " left out)" : "") + "." : src.disabled ? "Source not embedded (--include-source=false)." : "No source embedded." }));
    body.appendChild(m);
    const k = section("Navigate");
    ["Click to select, double-click a box to expand it or a symbol to focus on it.", "Ctrl+K searches everything; the explorer on the left lists the whole tree.",
      "Selecting shows what the element depends on (blue) and what depends on it (amber)."].forEach((x) => k.appendChild(h("p", { class: "note", text: x })));
    body.appendChild(k);
  }
  function drawHTTPSummary() {
    const R = D.ROUTES, H = D.HTTP;
    drawHead("http", "HTTP endpoints", "", [], [badge("HTTP", "kind")]);
    const s = section("Catalog");
    const auth = R.filter((r) => r.auth && r.auth.recognized).length, partial = R.filter((r) => r.status !== "complete").length;
    const grid = h("div", { class: "metrics" });
    [["Routes", R.length], ["With auth", auth], ["Partial", partial]].forEach(([l, v]) => grid.appendChild(h("div", { class: "metric" }, [h("div", { class: "v", text: GV.fmt(v) }), h("div", { class: "l", text: l })])));
    s.appendChild(grid);
    body.appendChild(s);
    if (H && (H.servers || []).length) {
      const sv = section("Listen addresses");
      H.servers.forEach((x) => sv.appendChild(h("p", { class: "note", text: x.addr + (x.url ? " → " + x.url : "") + " · " + x.evidence.details + " (" + x.evidence.file + ":" + x.evidence.line + ")" })));
      body.appendChild(sv);
    }
    if (H && (H.notes || []).length) { const n = section("Notes"); H.notes.forEach((x) => n.appendChild(h("p", { class: "note warn", text: x }))); body.appendChild(n); }
    const t = section(null);
    t.appendChild(h("p", { class: "note", text: "Pick an endpoint on the left: the canvas draws the code it may run, the inspector its contract and a request you can copy as cURL. Nothing is sent." }));
    body.appendChild(t);
  }

  // ---------- route ----------
  function drawRoute(k) {
    const r = D.ROUTES[k], ri = D.nodeOfRoute[k];
    drawHead("route", r.path, r.file + ":" + r.line, [
      GV.copyButton("Path", () => r.path), GV.copyButton("cURL", () => GV.requests.curlOf(r, GV.requests.reqOf(r))),
      ri >= 0 ? h("button", { type: "button", class: "btn sm", title: "Show the registration in the architecture", on: { click: () => GV.app.selectItem(ri, { reveal: true, mode: "arch" }) } }, [GV.icon("target"), "In architecture"]) : null
    ].filter(Boolean), [badge("HTTP route", "kind"), badge(r.framework, "kind"), r.status === "complete" ? badge("contract complete", "ok") : badge("contract " + r.status, "warn", (r.reasons || []).join("; ")),
      r.auth && r.auth.recognized ? badge("auth: " + (r.auth.scheme || "recognised"), "ok") : null].filter(Boolean),
    h("span", { class: "method m-" + r.method, text: r.method }));
    tabBar([["overview", "Overview"], ["request", "Request"], ["response", "Response", (r.responses || []).length], ["flow", "Flow"], ["source", "Source"], ["evidence", "Evidence"]],
      tabs.route, (id) => { tabs.route = id; GV.app.noteTab(id); });
    GV.clear(body);
    ({ overview: routeOverview, request: routeRequest, response: routeResponse, flow: routeFlow, source: routeSource, evidence: routeEvidence }[tabs.route] || routeOverview)(r, k);
  }

  function chainList(r) {
    const ol = h("ol", { class: "chain" });
    const all = (r.middlewares || []).concat(r.handler ? [r.handler] : []);
    all.forEach((m, k) => {
      const i = m.id && D.byId.has(m.id) ? D.byId.get(m.id) : -1;
      const mw = k < (r.middlewares || []).length;
      const role = mw ? "middleware · " + (m.scope || "") + (m.addedFile ? " · added at " + m.addedFile + ":" + m.addedLine : "") : "handler" + (m.file ? " · " + m.file + ":" + m.line : "");
      const dot = h("span", { class: "kdot round" });
      dot.style.background = GV.token(i >= 0 ? GV.http.layerColor(N[i]) : "k-unresolved");
      ol.appendChild(h("li", { tabindex: "0", on: { click: () => { if (i >= 0) GV.app.selectItem(i, { reveal: true }); } } }, [
        h("span", { class: "ord", text: String(k + 1) }), dot,
        h("div", { class: "two" }, [h("span", { class: "name mono", text: m.name }), h("span", { class: "sub", text: role + (m.note ? " · " + m.note : "") })]),
        confBadge(m.confidence)]));
    });
    return ol;
  }
  function routeOverview(r) {
    const s = section("Contract");
    s.appendChild(kv([
      ["Handler", r.handler ? h("span", null, [r.handler.id && D.byId.has(r.handler.id) ? itemLink(D.byId.get(r.handler.id), r.handler.name) : h("span", { class: "mono", text: r.handler.name }), " ", confBadge(r.handler.confidence)]) : "not resolved"],
      ["Registered", r.file + ":" + r.line + " (" + r.framework + ")"],
      ["Group", r.group || "/"],
      ["Status", r.status + (r.reasons && r.reasons.length ? " — " + r.reasons.join("; ") : "")],
      r.unresolvedCalls ? ["Unresolved", r.unresolvedCalls + " call(s) without a static target in the chain"] : null,
      ["Auth", r.auth ? (r.auth.recognized ? "recognised" : "not recognised") + (r.auth.scheme ? " · " + r.auth.scheme : "") + (r.auth.credential ? " · " + r.auth.credential : "") + (r.auth.by ? " · by " + r.auth.by : "") + (r.auth.note ? " · " + r.auth.note : "")
        : "no middleware or handler of the chain answers 401 or reads a credential"]
    ]));
    body.appendChild(s);
    const c = section("Handler chain", (r.middlewares || []).length + (r.handler ? 1 : 0));
    c.appendChild(chainList(r));
    body.appendChild(c);
    const view = GV.app.view();
    if (view && view.kind === "http") {
      const byLayer = {};
      view.scene.boxes.forEach((b) => { if (b.layer && b.layer !== "Route") (byLayer[b.layer] = byLayer[b.layer] || []).push(b.ref); });
      ["Middleware", "Handler", "Service", "Repository", "Database", "External API", "External", "Unresolved"].forEach((l) => {
        if (!byLayer[l]) return;
        const ls = section(l === "Database" ? "SQL operations" : l, byLayer[l].length);
        byLayer[l].slice(0, 30).forEach((i) => {
          const n = N[i];
          ls.appendChild(rowItem(i, n.meta.sql ? n.meta.sql.replace(/\s+/g, " ").trim().slice(0, 120) : (n.meta.layerBasis ? n.meta.layerBasis + " · " : "") + GV.loc(n), null, () => GV.app.selectItem(i, { reveal: true })));
        });
        if (byLayer[l].length > 30) ls.appendChild(h("p", { class: "note", text: (byLayer[l].length - 30) + " more" }));
        body.appendChild(ls);
      });
      body.appendChild(h("p", { class: "note", style: { padding: "0 12px 12px" }, text: "Layers come from the code's structure (STATIC) or, when it says nothing, from directory names (HEURISTIC). Only what the drawn graph reaches is listed: raise the depth or expand a step to follow more calls." }));
    }
  }

  function shapeRows(tbody, sh, prefix, enforced, depth, seen) {
    if (!sh || depth > 6) return;
    if (sh.ref) {
      if (seen[sh.ref]) { tbody.appendChild(h("tr", null, [h("td", { class: "mono", text: prefix || "(root)" }), h("td", { text: sh.name + " (recursive)", colspan: "4" })])); return; }
      seen = Object.assign({}, seen); seen[sh.ref] = true;
      sh = D.DEFS[sh.ref] || sh;
    }
    if (sh.type === "array" && sh.items) { shapeRows(tbody, sh.items, prefix + "[]", enforced, depth + 1, seen); return; }
    (sh.fields || []).forEach((f) => {
      const path = prefix ? prefix + "." + f.name : f.name;
      const fs = f.schema || {};
      const rs = D.DEFS[fs.ref] || fs;
      const type = rs.type + (rs.format ? "/" + rs.format : "") + (fs.nullable ? " | null" : "");
      const req = (f.rules || []).some((x) => x.name === "required" && enforced.indexOf(x.source) >= 0);
      const declared = (f.rules || []).some((x) => x.name === "required");
      const rules = (f.rules || []).map((x) => x.name + (x.param ? "=" + x.param : "")).join(", ");
      const src = (f.goName || "") + (f.embedded ? " (from " + f.embedded + ")" : "") + (f.conditional ? " · some paths only" : "");
      tbody.appendChild(h("tr", null, [h("td", { class: "mono", text: path }), h("td", { text: type + (rs.name && rs.type === "object" ? " " + rs.name : "") }),
        h("td", { text: req ? "yes" : declared ? "declared, not applied" : "no" }), h("td", { class: "mono", text: rules }), h("td", { text: src })]));
      if (rs.type === "object" || rs.type === "array") shapeRows(tbody, fs, path, enforced, depth + 1, seen);
    });
    if (sh.values) tbody.appendChild(h("tr", null, [h("td", { class: "mono", text: (prefix || "") + "{key}" }), h("td", { text: (sh.values.type || "") + " (map values)", colspan: "4" })]));
    if (sh.partial) tbody.appendChild(h("tr", null, [h("td", { text: "…", class: "mono" }), h("td", { text: sh.note || "more keys may be present", colspan: "4" })]));
  }
  function shapeTable(sh, enforced) {
    const t = h("table", { class: "grid" });
    t.appendChild(h("tr", null, ["Field", "Type", "Required", "Rules", "Go field"].map((x) => h("th", { text: x }))));
    const tb = h("tbody");
    shapeRows(tb, sh, "", enforced || [], 0, {});
    t.appendChild(tb);
    return t;
  }
  const evLoc = (ev) => ev && ev.length ? ev[0].file + ":" + ev[0].line : "";

  function routeRequest(r) {
    const RQ = GV.requests, q = RQ.reqOf(r);
    const curlPre = h("pre", { class: "sig", id: "curl" });
    const urlOut = h("input", { type: "text", readonly: true, "aria-label": "Request URL" });
    const update = () => { urlOut.value = RQ.requestURL(r, q); curlPre.textContent = RQ.curlOf(r, q); };
    const top = section("Request");
    top.appendChild(h("div", { class: "urlbar" }, [h("span", { class: "method m-" + r.method, text: r.method }), urlOut]));
    top.appendChild(h("div", { class: "urlbar" }, [h("span", { class: "lbl", text: "Base URL" }), h("input", { type: "text", value: q.base, "aria-label": "Base URL", on: { input: (ev) => { q.base = ev.target.value; update(); } } })]));
    top.appendChild(h("p", { class: "note", text: "Base URL: " + (r.example ? r.example.baseUrlSource : "placeholder") + ". Values are synthetic; edit them freely. This page never sends the request." }));
    body.appendChild(top);
    const pathParams = (r.params || []).filter((x) => x.in === "path");
    if (pathParams.length) {
      const s = section("Path parameters", pathParams.length);
      const t = h("table", { class: "grid" });
      pathParams.forEach((x) => {
        t.appendChild(h("tr", null, [h("td", { class: "mono", text: x.name }), h("td", null, [h("input", { type: "text", value: q.path[x.name] || "", "aria-label": x.name, on: { input: (ev) => { q.path[x.name] = ev.target.value; update(); } } })]),
          h("td", { class: "muted", text: ((x.schema || {}).type || "string") + " · " + x.source + (x.note ? " · " + x.note : "") })]));
      });
      s.appendChild(t);
      body.appendChild(s);
    }
    const kvTable = (title, list, kind) => {
      const s = section(title, list.length, h("button", { type: "button", class: "btn sm ghost", text: "+ Add", on: { click: () => { list.push({ k: "", v: "", on: true }); draw(); } } }));
      const t = h("table", { class: "grid" });
      list.forEach((x) => {
        const note = x.note ? x.note.source + (x.note.from && r.handler && x.note.from !== r.handler.name ? " · read by " + x.note.from : "") + " · required: " + x.note.required + (x.note.default ? " · default " + x.note.default : "") : "";
        t.appendChild(h("tr", { class: note ? "has-note" : null }, [
          h("td", { class: "cb" }, [h("input", { type: "checkbox", checked: x.on ? true : null, "aria-label": "Send " + x.k, on: { change: (ev) => { x.on = ev.target.checked; update(); } } })]),
          h("td", null, [h("input", { type: "text", value: x.k, "aria-label": kind + " name", on: { input: (ev) => { x.k = ev.target.value; update(); } } })]),
          h("td", null, [h("input", { type: "text", value: x.v, "aria-label": kind + " value", on: { input: (ev) => { x.v = ev.target.value; update(); } } })])]));
        if (note) t.appendChild(h("tr", { class: "note-row" }, [h("td"), h("td", { colspan: "2", class: "muted", text: note })]));
      });
      if (!list.length) t.appendChild(h("tr", null, [h("td", { class: "muted", colspan: "3", text: "none read by the code" })]));
      s.appendChild(t);
      body.appendChild(s);
    };
    kvTable("Query parameters", q.query, "Query");
    kvTable("Headers", q.headers, "Header");
    if (r.auth && r.auth.recognized) body.appendChild(h("p", { class: "note", style: { padding: "0 12px" }, text: "Authorization: placeholder only (" + (r.auth.scheme || "unknown scheme") + "); GitView never exports credentials." }));
    const bs = section("Body", null, h("span", { class: "seg" }, [
      h("button", { type: "button", text: "Example", "aria-pressed": String(I.bodyView !== "schema"), on: { click: () => { I.bodyView = "example"; draw(); } } }),
      h("button", { type: "button", text: "Schema", "aria-pressed": String(I.bodyView === "schema"), on: { click: () => { I.bodyView = "schema"; draw(); } } })]));
    if (!r.body) bs.appendChild(h("p", { class: "note", text: "The chain decodes no request body." }));
    else {
      bs.appendChild(h("p", { class: "note", text: (r.body.contentTypes || []).join(", ") + " · read with " + r.body.source + " at " + evLoc(r.body.evidence) +
        (r.body.enforces && r.body.enforces.length ? " · applies " + r.body.enforces.join(", ") + " tags" : " · applies no validation tag") +
        (r.body.disallowUnknown ? " · rejects unknown fields" : "") + ((r.body.notes || []).length ? " · " + r.body.notes.join("; ") : "") }));
      if (I.bodyView === "schema") { if (r.body.schema) bs.appendChild(shapeTable(r.body.schema, r.body.enforces)); }
      else {
        const ta = h("textarea", { class: "body", spellcheck: "false", "aria-label": "Request body", on: { input: (ev) => { q.body = ev.target.value; update(); } } });
        ta.value = q.body;
        bs.appendChild(ta);
        if (r.example) bs.appendChild(h("p", { class: r.example.valid === "compatible" ? "note" : "note warn",
          text: r.example.valid === "compatible" ? "Synthetic example compatible with the recognised validation rules." : "Synthetic example; it may not satisfy every rule: " + (r.example.notes || []).join("; ") }));
      }
    }
    body.appendChild(bs);
    const cs = section("cURL");
    cs.appendChild(curlPre);
    cs.appendChild(h("div", { class: "btn-row", style: { marginTop: "8px" } }, [
      GV.copyButton("cURL", () => RQ.curlOf(r, q)),
      q.body ? GV.copyButton("JSON", () => q.body) : null,
      h("button", { type: "button", class: "btn sm", title: "Save the command as a .sh file", on: { click: () => GV.download(RQ.fileName(r, ".sh"), "#!/bin/sh\n# " + r.key + " — generated by GitView; synthetic values, review before running.\n" + RQ.curlOf(r, q) + "\n") } }, [GV.icon("download"), "Export cURL"]),
      q.body ? h("button", { type: "button", class: "btn sm", on: { click: () => GV.download(RQ.fileName(r, ".json"), q.body + "\n", "application/json") } }, [GV.icon("download"), "Export JSON"]) : null,
      h("button", { type: "button", class: "btn sm ghost", text: "Reset", on: { click: () => { RQ.reset(r); draw(); } } })
    ].filter(Boolean)));
    body.appendChild(cs);
    update();
  }

  function routeResponse(r) {
    const top = section(null);
    if (!(r.responses || []).length) { top.appendChild(h("p", { class: "note warn", text: "No response identified statically." })); body.appendChild(top); return; }
    top.appendChild(h("p", { class: "note", text: "Statuses the chain may answer with, found in the code. static: status and body identified · partial: one of them is not · unknown: neither. None of them was observed at runtime." }));
    r.responses.forEach((x, k) => {
      const box = h("div", { class: "resp" });
      const cls = "s" + (x.status ? String(x.status).charAt(0) : "0");
      box.appendChild(h("div", { class: "top" }, [h("span", { class: "status " + cls, text: x.status ? String(x.status) : "???" }),
        h("span", { text: x.status ? x.statusText : "status not determinable statically" }),
        x.contentType ? h("span", { class: "mono muted", text: x.contentType }) : null,
        h("span", { class: "grow" }),
        badge(x.basis, x.basis === "static" ? "conf-confirmed" : x.basis === "partial" ? "conf-possible" : "conf-unresolved"), confBadge(x.confidence)]));
      const name = x.schema ? (x.schema.name || x.schema.goType || x.schema.type) : "no body";
      const b = h("div", { class: "body" });
      b.appendChild(h("div", { class: "note", text: name + " · " + x.source + " · from " + x.from + (x.via && x.via.length ? " → " + x.via.join(" → ") : "") }));
      b.appendChild(h("div", { class: "note", text: evLoc(x.evidence) + (x.evidenceTotal > (x.evidence || []).length ? " · " + x.evidenceTotal + " evidence entries (all in the JSON export)" : "") + (x.implicit ? " · implicit status" : "") + (x.note ? " · " + x.note : "") }));
      if (x.schema && (x.schema.fields || x.schema.ref || x.schema.items)) {
        const d = h("details", { class: "fold", open: I.respOpen[k] ? true : null, on: { toggle: (ev) => { I.respOpen[k] = ev.target.open; } } }, [h("summary", { text: "Schema and synthetic example" })]);
        d.appendChild(shapeTable(x.schema, []));
        if (x.example) d.appendChild(h("pre", { class: "sig", style: { marginTop: "6px" }, text: x.example }));
        b.appendChild(d);
      }
      box.appendChild(b);
      top.appendChild(box);
    });
    body.appendChild(top);
  }

  function routeFlow(r, k) {
    const st = GV.app.state(), view = GV.app.view();
    const s = section("Call tree", null, h("span", { class: "btn-row" }, [
      h("button", { type: "button", class: "btn sm", text: "Collapse all", on: { click: () => GV.app.setJourney({ jdepth: 0, jexp: {} }) } })]));
    s.appendChild(h("p", { class: "note", text: "The handler chain and what it calls, as drawn (depth " + st.jdepth + "). ↻ marks recursion, ⋯ collapsed calls. Every line is a possible path found by static analysis." }));
    const tree = h("div", { class: "flowtree" });
    if (view && view.kind === "http") {
      const drawnOut = new Map();
      view.edges.forEach((e) => { const a = Number(e.s.slice(1)); if (!drawnOut.has(a)) drawnOut.set(a, []); drawnOut.get(a).push(e); });
      const printed = new Set();
      const line = (i, e, indent, mark) => {
        const n = N[i], l = GV.http.layerOf(n);
        const site = e && E[e.agg.real[0]] ? E[e.agg.real[0]].file + ":" + E[e.agg.real[0]].line : GV.loc(n);
        tree.appendChild(h("div", { title: site, on: { click: () => GV.app.selectItem(i, { reveal: true }) } }, [indent + (e ? "→ " : "") + n.name, h("span", { class: "c", text: (l ? "  [" + l + "]" : "") + (e ? "  " + e.conf : "") + (mark || "") })]));
      };
      const walk = (i, indent, path, d) => {
        (drawnOut.get(i) || []).filter((e) => !e.agg.types.handles).forEach((e) => {
          const t = Number(e.t.slice(1));
          if (path.has(t)) { line(t, e, indent, "  ↻"); return; }
          if (printed.has(t)) { line(t, e, indent, "  (above)"); return; }
          printed.add(t);
          line(t, e, indent, view.more.has(t) ? "  ⋯" : "");
          path.add(t);
          if (d < 40) walk(t, indent + "   ", path, d + 1);
          path.delete(t);
        });
      };
      (drawnOut.get(view.root) || []).filter((e) => e.agg.types.handles).sort((a, b) => (view.chainPos.get(Number(a.t.slice(1))) || 0) - (view.chainPos.get(Number(b.t.slice(1))) || 0)).forEach((e) => {
        const t = Number(e.t.slice(1));
        printed.add(t);
        line(t, null, "", "");
        walk(t, "   ", new Set([t]), 0);
      });
    }
    s.appendChild(tree);
    body.appendChild(s);
  }

  function routeSource(r, k) {
    const ri = D.nodeOfRoute[k];
    if (ri >= 0) {
      const s = section("Registration");
      const box = h("div");
      s.appendChild(box);
      body.appendChild(s);
      GV.source.render(box, N[ri]);
    }
    const hi = r.handler && r.handler.id && D.byId.has(r.handler.id) ? D.byId.get(r.handler.id) : -1;
    if (hi >= 0) {
      const s = section("Handler · " + r.handler.name);
      const box = h("div");
      s.appendChild(box);
      body.appendChild(s);
      GV.source.render(box, N[hi]);
    }
  }

  function routeEvidence(r) {
    const rows = [["Registration", r.key, r.file + ":" + r.line, r.framework, "confirmed"]];
    (r.middlewares || []).forEach((m) => rows.push(["Middleware (" + m.scope + ")", m.name, m.addedFile ? m.addedFile + ":" + m.addedLine : "", m.note || "", m.confidence]));
    if (r.handler) rows.push(["Handler", r.handler.name, r.handler.file ? r.handler.file + ":" + r.handler.line : "", r.handler.note || "", r.handler.confidence]);
    (r.params || []).forEach((x) => (x.evidence || []).slice(0, 3).forEach((ev) => rows.push(["Param " + x.in, x.name, ev.file + ":" + ev.line, ev.details || x.source, x.confidence])));
    if (r.body) (r.body.evidence || []).slice(0, 4).forEach((ev) => rows.push(["Body", r.body.source, ev.file + ":" + ev.line, ev.details || "", r.body.confidence]));
    (r.responses || []).forEach((x) => (x.evidence || []).slice(0, 4).forEach((ev) => rows.push(["Response " + (x.status || "?"), x.source, ev.file + ":" + ev.line, ev.details || "", x.confidence])));
    if (r.auth) (r.auth.evidence || []).forEach((ev) => rows.push(["Auth", r.auth.credential || "", ev.file + ":" + ev.line, ev.details || "", r.auth.recognized ? "recognised" : "—"]));
    const s = section("Evidence", rows.length);
    const t = h("table", { class: "grid evid" });
    t.appendChild(h("tr", null, ["What", "Element", "Where", "Details", "Confidence"].map((x) => h("th", { text: x }))));
    rows.forEach((rw) => t.appendChild(h("tr", null, rw.map((x, k) => k === 4 && D.CONF_RANK[x] ? h("td", null, [confBadge(x)]) : h("td", { class: k === 2 ? "mono" : null, text: x })))));
    s.appendChild(t);
    s.appendChild(h("p", { class: "note", text: "Every row is static evidence (go/ssa, go/types): where the relation was read in the code." }));
    body.appendChild(s);
  }

  // ---------- entry ----------
  const parts = { badge, section, kv, confBadge, kdot, drawHead, tabBar: (list, cur, set) => tabBar(list, cur, set), head, tabsEl, body };
  function draw() {
    const scroll = body.scrollTop;
    const same = draw.last === JSON.stringify(target && { t: target.type, i: target.i, k: target.k, key: target.key, id: target.id });
    if (!target) drawSummary();
    else if (target.type.startsWith("sec")) GV.security.draw(target, parts);
    else if (target.type === "item") drawItem(target.i);
    else if (target.type === "edge") drawEdge(target.edge);
    else if (target.type === "route") drawRoute(target.k);
    if (same) body.scrollTop = scroll;
    draw.last = JSON.stringify(target && { t: target.type, i: target.i, k: target.k, key: target.key, id: target.id });
  }
  GV.inspector = {
    show(t) { target = t; draw(); },
    refresh: draw,
    setTab(kind, tab) { tabs[kind] = tab; },
    tab: (kind) => tabs[kind],
    relationsOf, parts
  };
})(GV);
