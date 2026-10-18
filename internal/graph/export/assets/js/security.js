// Security Journey (CodeAudit): the audit embedded by gitview audit
// export/serve. The sidebar lists findings, routes and rules; the canvas
// draws the path from a route to the finding (route, middleware chain,
// handler, the calls down to the sensitive operation) coloured by what the
// analysis found, always with the state written next to the colour; the
// inspector shows the finding's evidence, code, data path, remedies, the AI
// investigations that were asked for and the review decisions.
//
// Static analysis findings come from deterministic rules; AI text is
// shown apart, marked as such, and never changes a finding or its status.
// Asking the model and recording decisions need the local server
// (gitview audit serve); the exported file is read-only and offline.
(function (GV) {
  "use strict";
  const D = GV.data, T = D.T, h = GV.h;
  const dataEl = document.getElementById("gitview-security");
  if (!dataEl) { GV.security = { available: false }; return; }
  const SEC = JSON.parse(dataEl.textContent);
  const liveEl = document.getElementById("gitview-live");
  const LIVE = liveEl ? JSON.parse(liveEl.textContent) : null;
  const A = SEC.audit;
  const F = A.findings || [];
  const byId = new Map(F.map((f) => [f.id, f]));
  const reviews = SEC.reviews || {};
  const revalidate = SEC.revalidate || {};
  const ai = {};
  Object.keys(SEC.ai || {}).forEach((k) => { ai[k] = SEC.ai[k]; });
  const SEVS = ["critical", "high", "medium", "low", "info"];
  const SEV_RANK = { critical: 5, high: 4, medium: 3, low: 2, info: 1 };
  const CATS = { authentication: "Authentication", authorization: "Authorization", injection: "Injection", "sensitive-data": "Sensitive data", configuration: "Configuration", dependencies: "Dependencies" };
  const STATUSES = ["open", "investigating", "confirmed", "false_positive", "accepted_risk", "fixed", "needs_revalidation"];
  const STATUS_LABEL = { open: "Open", investigating: "Investigating", confirmed: "Confirmed", false_positive: "False positive", accepted_risk: "Accepted risk", fixed: "Fixed", needs_revalidation: "Needs revalidation" };
  const diffOf = new Map();
  if (SEC.diff) {
    (SEC.diff.new || []).forEach((x) => diffOf.set(x.id, { kind: "new" }));
    (SEC.diff.changed || []).forEach((x) => diffOf.set(x.id, { kind: "changed", changes: x.changes }));
  }

  // ---------- states ----------
  const status = (f) => (reviews[f.id] && reviews[f.id].status) || "open";
  // A reviewer's decision overrides the verdict in the graph.
  function stateOf(f) {
    switch (status(f)) {
      case "confirmed": return { cls: "vulnerable", label: "Confirmed by review", token: "s-vuln" };
      case "false_positive": return { cls: "dismissed", label: "False positive (review)", token: "s-unres" };
      case "accepted_risk": return { cls: "review", label: "Accepted risk (review)", token: "s-review" };
      case "fixed": return { cls: "check", label: "Marked fixed (review)", token: "s-check" };
    }
    if (f.verdict === "vulnerable") return { cls: "vulnerable", label: "Vulnerable", token: "s-vuln" };
    if (f.verdict === "incomplete") return { cls: "incomplete", label: "Analysis incomplete", token: "s-unres" };
    return { cls: "review", label: "Review required", token: "s-review" };
  }
  function sevBadge(s) { return h("span", { class: "sev " + s, text: s }); }
  function stateBadge(f) { const st = stateOf(f); return h("span", { class: "badge", text: st.label, style: { color: GV.token(st.token), borderColor: GV.token(st.token) } }); }

  // ---------- sidebar ----------
  const L = { list: "findings", q: "", sev: {}, cat: "", verdict: "", status: "" };
  function matches(f) {
    const anySev = SEVS.some((s) => L.sev[s]);
    if (anySev && !L.sev[f.severity]) return false;
    if (L.cat && f.category !== L.cat) return false;
    if (L.verdict && f.verdict !== L.verdict) return false;
    if (L.status && status(f) !== L.status) return false;
    const q = L.q.trim().toLowerCase();
    if (!q) return true;
    const hay = (f.title + " " + f.ruleId + " " + f.message + " " + f.file + " " + (f.routes || []).map((r) => r.key).join(" ")).toLowerCase();
    return q.split(/\s+/).every((w) => hay.indexOf(w) >= 0);
  }
  let built = false;
  function drawSidebar(S) {
    const el = GV.$("security");
    if (!built) {
      built = true;
      const tabs = h("div", { class: "seg", role: "tablist" }, ["findings", "routes", "rules"].map((k) => h("button", { type: "button", "data-list": k, text: k[0].toUpperCase() + k.slice(1),
        on: { click: () => { L.list = k; drawSidebar(S); } } })));
      const q = h("input", { type: "search", placeholder: "Filter findings…", "aria-label": "Filter findings", autocomplete: "off", spellcheck: "false", on: { input: (ev) => { L.q = ev.target.value; drawList(S); } } });
      const cat = h("select", { "aria-label": "Category", on: { change: (ev) => { L.cat = ev.target.value; drawList(S); } } },
        [h("option", { value: "", text: "All categories" })].concat(Object.keys(CATS).filter((c) => F.some((f) => f.category === c)).map((c) => h("option", { value: c, text: CATS[c] }))));
      const ver = h("select", { "aria-label": "Verdict or review status", on: { change: (ev) => { const v = ev.target.value; L.verdict = v.startsWith("v:") ? v.slice(2) : ""; L.status = v.startsWith("s:") ? v.slice(2) : ""; drawList(S); } } },
        [["", "Any state"], ["v:vulnerable", "Vulnerable"], ["v:review", "Review required"], ["v:incomplete", "Incomplete"]].concat(STATUSES.map((s) => ["s:" + s, "Review: " + STATUS_LABEL[s]])).map((o) => h("option", { value: o[0], text: o[1] })));
      el.appendChild(h("div", { class: "stools" }, [tabs, q, h("div", { class: "sevchips", id: "sevchips" }), h("div", { class: "line" }, [cat, ver])]));
      el.appendChild(h("div", { class: "slist", id: "slist", role: "listbox", "aria-label": "Findings" }));
    }
    el.querySelectorAll("[data-list]").forEach((b) => b.setAttribute("aria-pressed", String(b.dataset.list === L.list)));
    const chips = GV.clear(GV.$("sevchips"));
    SEVS.forEach((s) => {
      const n = F.filter((f) => f.severity === s).length;
      if (!n) return;
      chips.appendChild(h("button", { type: "button", "aria-pressed": String(!!L.sev[s]), on: { click: () => { L.sev[s] = !L.sev[s]; drawSidebar(S); } } }, [sevBadge(s), " " + n]));
    });
    drawList(S);
  }
  function drawList(S) {
    const list = GV.clear(GV.$("slist"));
    if (L.list === "rules") { (A.rules || []).forEach((r) => list.appendChild(ruleRow(r))); return; }
    if (L.list === "routes") { drawRoutes(list, S); return; }
    const shown = F.filter(matches);
    if (!F.length) { list.appendChild(h("div", { class: "empty" }, [h("strong", { text: "No findings" }), "The enabled rules found nothing in the analysed code. That is not a proof of security: see the analysis status and limits in the inspector."])); return; }
    if (!shown.length) { list.appendChild(h("div", { class: "empty", text: "No finding matches the filters." })); return; }
    Object.keys(CATS).forEach((cat) => {
      const items = shown.filter((f) => f.category === cat);
      if (!items.length) return;
      list.appendChild(h("div", { class: "rgroup" }, [h("span", { class: "grow", text: CATS[cat] }), h("span", { text: String(items.length) })]));
      items.forEach((f) => list.appendChild(findingRow(f, S)));
    });
  }
  function findingRow(f, S) {
    const st = stateOf(f);
    const tags = h("span", { class: "tags" }, [sevBadge(f.severity), h("span", { class: "badge", text: f.ruleId }),
      status(f) !== "open" ? h("span", { class: "badge", text: STATUS_LABEL[status(f)] }) : null,
      revalidate[f.id] ? h("span", { class: "badge warn", title: revalidate[f.id], text: "revalidate" }) : null,
      ai[f.id] ? h("span", { class: "ai-tag", text: "AI" }) : null,
      diffOf.has(f.id) ? h("span", { class: "badge", text: diffOf.get(f.id).kind }) : null]);
    return h("div", { class: "sitem", role: "option", tabindex: "0", "aria-current": String(S.sec.finding === f.id), title: f.verdictLabel,
      on: { click: () => GV.app.openFinding(f.id), keydown: (ev) => { if (ev.key === "Enter") GV.app.openFinding(f.id); } } },
      [h("span", { class: "sdot " + st.cls, title: st.label }), h("div", { class: "two" }, [h("span", { class: "name", text: f.title }), h("span", { class: "sub", text: (f.file || "") + (f.line ? ":" + f.line : "") }), tags])]);
  }
  function drawRoutes(list, S) {
    const q = L.q.trim().toLowerCase();
    (A.routes || []).forEach((r) => {
      if (q && r.key.toLowerCase().indexOf(q) < 0) return;
      const fs = (r.findings || []).map((id) => byId.get(id)).filter(Boolean);
      let worst = null;
      fs.forEach((f) => { if (!worst || SEV_RANK[f.severity] > SEV_RANK[worst.severity]) worst = f; });
      const k = r.key.indexOf(" ");
      const method = r.key.slice(0, k), path = r.key.slice(k + 1);
      const flags = h("span", { class: "flags" });
      if (r.auth !== "none identified") flags.appendChild(h("span", { class: "flag auth", title: "authentication " + r.auth }, [GV.icon("lock")]));
      if (r.unresolved) flags.appendChild(h("span", { class: "flag partial", title: r.unresolved + " call(s) without a static target" }, [GV.icon("warn")]));
      list.appendChild(h("div", { class: "ritem", role: "option", tabindex: "0", "aria-current": String(S.sec.route === r.id), title: r.key + " · authentication " + r.auth,
        on: { click: () => GV.app.openSecRoute(r.id) } },
        [h("span", { class: "method m-" + method, text: method }), GV.http.pathNode(path), worst ? sevBadge(worst.severity) : null, fs.length ? h("span", { class: "count", text: String(fs.length) }) : null, flags]));
    });
  }
  function ruleRow(r) {
    return h("div", { class: "sitem static", title: r.detects }, [h("span", { class: "sdot " + (r.findings ? "review" : "struct") }),
      h("div", { class: "two" }, [h("span", { class: "name", text: r.title }), h("span", { class: "sub", text: r.id + " · " + r.category + " · " + r.basis }),
        h("span", { class: "tags" }, [sevBadge(r.severity), h("span", { class: "badge", text: r.enabled ? r.findings + " finding(s)" : "not run" })])])]);
  }

  // ---------- graph ----------
  function chainSteps(rt) {
    const out = [{ id: rt.id, name: rt.key, role: "route", file: rt.file, line: rt.line }];
    (rt.middlewares || []).forEach((m) => out.push({ id: m.id, name: m.name, role: "middleware", file: m.file, line: m.line, conf: m.confidence }));
    if (rt.handler) out.push({ id: rt.handler.id, name: rt.handler.name, role: "handler", file: rt.handler.file, line: rt.handler.line, conf: rt.handler.confidence });
    return out;
  }
  const routeIdx = new Map(D.ROUTES.map((r, k) => [r.id, k]));
  const postureOf = new Map((A.routes || []).map((r) => [r.id, r]));
  async function build(S, aspect) {
    const sec = S.sec;
    const paths = [];
    let findings = [];
    if (sec.finding && byId.has(sec.finding)) {
      const f = byId.get(sec.finding);
      findings = [f];
      (f.routes || []).slice(0, 6).forEach((r) => paths.push({ steps: r.path, f, route: r.id }));
      if (!paths.length) {
        const st = { id: f.symbolId || ("file:" + f.file), name: f.symbol ? f.symbol.name : f.file, role: "sink", file: f.file, line: f.line };
        paths.push({ steps: [st], f });
      }
    } else if (sec.route) {
      findings = F.filter((f) => (f.routes || []).some((r) => r.id === sec.route));
      findings.forEach((f) => f.routes.filter((r) => r.id === sec.route).forEach((r) => paths.push({ steps: r.path, f, route: r.id })));
      const k = routeIdx.get(sec.route);
      if (k !== undefined) paths.push({ steps: chainSteps(D.ROUTES[k]), f: null, route: sec.route });
    } else return null;
    const nodes = new Map(), edges = new Map();
    const keyOfStep = (st) => st.id && D.byId.has(st.id) ? "s" + D.byId.get(st.id) : "x" + (st.id || st.name);
    paths.forEach((p) => {
      let prev = null;
      p.steps.forEach((st, i) => {
        const key = keyOfStep(st);
        let n = nodes.get(key);
        if (!n) {
          n = { key, ref: st.id && D.byId.has(st.id) ? D.byId.get(st.id) : undefined, id: st.id, name: st.name, role: st.role, file: st.file, line: st.line, check: "", depth: i, sinks: [] };
          nodes.set(key, n);
        }
        n.depth = Math.min(n.depth, i);
        if (st.check && !n.check) n.check = st.check;
        if (n.role !== "route" && st.role === "sink") n.role = "sink";
        if (p.f && i === p.steps.length - 1 && n.sinks.indexOf(p.f) < 0) n.sinks.push(p.f);
        if (prev) {
          const chain = prev.role === "route" || prev.role === "middleware";
          const ek = prev.key + ">" + key;
          if (!edges.has(ek)) edges.set(ek, { key: ek, s: prev.key, t: key, conf: chain ? "chain" : (st.conf === "possible" || st.conf === "likely" || st.conf === "unresolved" ? st.conf : "confirmed"), label: chain ? "next in chain" : "calls" });
        }
        prev = n;
      });
    });
    // one box per finding, under its sensitive step
    findings.forEach((f) => {
      const sink = Array.from(nodes.values()).find((n) => n.sinks.indexOf(f) >= 0);
      if (!sink) return;
      const st = stateOf(f);
      nodes.set("F" + f.id, { key: "F" + f.id, finding: f, name: f.title, depth: sink.depth + 1, state: st });
      edges.set(sink.key + ">F" + f.id, { key: sink.key + ">F" + f.id, s: sink.key, t: "F" + f.id, conf: "finding", color: st.token, label: f.ruleId });
    });
    // unresolved calls of the route's chain, as grey boxes
    const rid = sec.route || (findings[0] && findings[0].routes && findings[0].routes[0] && findings[0].routes[0].id);
    const post = rid ? postureOf.get(rid) : null;
    if (post && post.unresolved && sec.route) {
      const hd = Array.from(nodes.values()).find((n) => n.role === "handler");
      if (hd) {
        nodes.set("U", { key: "U", name: post.unresolved + " unresolved call(s)", depth: hd.depth + 1, unresolved: true });
        edges.set(hd.key + ">U", { key: hd.key + ">U", s: hd.key, t: "U", conf: "unresolved", label: "target not determinable statically" });
      }
    }
    const list = Array.from(nodes.values());
    const sizes = new Map();
    list.forEach((n) => {
      const title = n.name || "";
      const w = Math.round(Math.max(170, Math.min(GV.layout.textWidth(title, GV.layout.font(500, 12.5)) + 40, 340)));
      sizes.set(n.key, { w, h: 46 });
    });
    const lnodes = list.map((n) => ({ id: n.key, width: sizes.get(n.key).w, height: 46, c: n.depth }));
    const ledges = Array.from(edges.values()).map((e) => ({ id: e.key, s: e.s, t: e.t }));
    const key = "sec|" + (sec.finding || sec.route) + "|" + lnodes.map((n) => n.id + ":" + n.c).join(",") + "|" + ledges.map((e) => e.id).join(",");
    const res = await GV.layout.runPartitioned(key, lnodes, ledges, 0, aspect, "DOWN");
    const boxes = list.map((n) => {
      const p = res.pos[n.key] || { x: 0, y: 0 }, s = sizes.get(n.key);
      const b = { key: n.key, kind: "node", x: p.x, y: p.y, w: s.w, h: s.h, depth: 0, title: n.name, ref: n.ref };
      if (n.finding) {
        Object.assign(b, { color: n.state.token, sub: n.state.label + " · " + n.finding.severity, chip: n.finding.severity, chipColor: "sev-" + n.finding.severity, findingId: n.finding.id, tip: n.finding.verdictLabel });
      } else if (n.unresolved) {
        Object.assign(b, { color: "s-unres", sub: "not followed by the analysis", chip: "unresolved", chipColor: "s-unres", muted: true });
      } else {
        let token = "s-struct", chip = "", sub = n.role + (n.file ? " · " + GV.baseName(n.file) + (n.line ? ":" + n.line : "") : "");
        if (n.sinks.length) {
          let worst = n.sinks[0];
          n.sinks.forEach((f) => { if (SEV_RANK[f.severity] > SEV_RANK[worst.severity]) worst = f; });
          const st = stateOf(worst);
          token = st.token; chip = st.label.toLowerCase();
        } else if (/authentication|authorization/.test(n.check)) {
          token = "s-check"; chip = "✓ " + n.check.replace(" (answers 403)", "");
        } else if (n.check) chip = n.check;
        Object.assign(b, { color: token, sub, chip, chipColor: token, tip: n.id || n.name });
      }
      return b;
    });
    const byKey = new Map(boxes.map((b) => [b.key, b]));
    const sceneEdges = Array.from(edges.values()).map((e) => {
      let pts = res.edges[e.key];
      if (!pts) {
        const A1 = byKey.get(e.s), B1 = byKey.get(e.t);
        const ax = A1.x + A1.w / 2, ay = A1.y + A1.h, bx = B1.x + B1.w / 2, by = B1.y, my = (ay + by) / 2;
        pts = [ax, ay, ax, my, bx, my, bx, by];
      }
      return { key: e.key, s: e.s, t: e.t, conf: e.conf, color: e.color, n: 1, label: e.label, pts };
    });
    let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
    boxes.forEach((b) => { x0 = Math.min(x0, b.x); y0 = Math.min(y0, b.y); x1 = Math.max(x1, b.x + b.w); y1 = Math.max(y1, b.y + b.h); });
    const out = new Map(), inn = new Map();
    sceneEdges.forEach((e) => {
      if (!out.has(e.s)) out.set(e.s, []);
      if (!inn.has(e.t)) inn.set(e.t, []);
      out.get(e.s).push(e); inn.get(e.t).push(e);
    });
    return {
      kind: "security", edges: sceneEdges, out, inn, findings,
      scene: { boxes, edges: sceneEdges, bounds: { x: x0, y: y0, w: x1 - x0, h: y1 - y0 } },
      counts: { boxes: boxes.length, edges: sceneEdges.length },
      keyOf: (i) => nodes.has("s" + i) ? "s" + i : null, root: boxes[0] ? boxes[0].key : null
    };
  }

  // pathKeys finds the drawn path between two boxes (following the arrows).
  function pathKeys(view, a, b) {
    const prev = new Map([[a, null]]);
    const q = [a];
    while (q.length) {
      const x = q.shift();
      if (x === b) break;
      for (const e of view.out.get(x) || []) if (!prev.has(e.t)) { prev.set(e.t, x); q.push(e.t); }
    }
    if (!prev.has(b)) return null;
    const keys = [];
    for (let x = b; x !== null; x = prev.get(x)) keys.unshift(x);
    return keys;
  }

  // ---------- code excerpts ----------
  function excerptFor(file, from, to) {
    const list = (SEC.snippets || {})[file];
    if (!list) return null;
    return list.filter((x) => x.to >= from && x.from <= to);
  }
  // codeExcerpts renders the embedded excerpts of a file overlapping
  // [from, to], marking the given lines.
  function codeExcerpts(file, from, to, marks) {
    const parts = excerptFor(file, from, to);
    if (!parts || !parts.length) return null;
    const table = h("table");
    parts.forEach((x, k) => {
      if (k) table.appendChild(h("tr", { class: "gap" }, [h("td", { colspan: "2", text: "⋯" })]));
      const rows = GV.source.tokenLines(x.lines.join("\n"));
      rows.forEach((toks, i) => {
        const n = x.from + i;
        if (n < from || n > to) return;
        const src = h("td", { class: "src" });
        toks.forEach(([c, t]) => src.appendChild(GV.source.span(c, t)));
        if (!src.firstChild) src.appendChild(document.createTextNode(" "));
        table.appendChild(h("tr", { class: marks && marks.has(n) ? "mark" : null, "data-line": n }, [h("td", { class: "ln", text: String(n) }), src]));
      });
    });
    return h("div", { class: "code" }, [table]);
  }

  // ---------- live server ----------
  async function call(path, body, signal) {
    const r = await fetch(path, { method: body ? "POST" : "GET", signal, headers: { "Content-Type": "application/json", "X-GitView-Token": LIVE.token }, body: body ? JSON.stringify(body) : undefined });
    let j = null;
    try { j = await r.json(); } catch (e) { j = null; }
    if (!r.ok) throw new Error((j && j.error) || r.status + " " + r.statusText);
    return j;
  }
  let pending = null; // {id, ctrl}

  // ---------- inspector ----------
  const tabs = { finding: "overview" };
  function draw(target, P) {
    if (target.type === "secFinding") return drawFinding(byId.get(target.id), P);
    if (target.type === "secRoute") return drawRoute(target.id, P);
    if (target.type === "secPath") return drawPath(target, P);
    return drawSummary(P);
  }
  function cmdLine(text) {
    return h("div", { class: "cmd" }, [h("code", { text }), GV.copyButton("Command", () => text)]);
  }
  function drawFinding(f, P) {
    if (!f) return drawSummary(P);
    const st = stateOf(f);
    P.drawHead("finding", f.title, (f.file || "") + (f.line ? ":" + f.line : ""), [
      GV.copyButton("ID", () => f.id), f.file ? GV.copyButton("Path", () => f.file + ":" + f.line, "Copy file:line") : null,
      h("button", { type: "button", class: "btn sm", title: "Open the full report", on: { click: report } }, [GV.icon("file"), "Report"])
    ].filter(Boolean), [sevBadge(f.severity), h("span", { class: "badge kind rule", text: f.ruleId }), h("span", { class: "badge kind", text: CATS[f.category] || f.category })]);
    P.tabBar([["overview", "Overview"], ["evidence", "Evidence", f.evidence.length], ["source", "Source"], ["flow", "Flow"], ["fix", "Fix"], ["ai", "AI"], ["review", "Review"]], tabs.finding, (id) => { tabs.finding = id; GV.app.noteTab(id); });
    const body = P.body;
    GV.clear(body);
    ({ overview: fOverview, evidence: fEvidence, source: fSource, flow: fFlow, fix: fFix, ai: fAI, review: fReview }[tabs.finding] || fOverview)(f, P, body, st);
  }
  function fOverview(f, P, body, st) {
    const s = P.section(null);
    s.appendChild(h("div", { class: "verdict " + st.cls }, [h("span", { class: "sdot " + st.cls }), f.verdictLabel + (st.label !== "Vulnerable" && st.label !== "Review required" && st.label !== "Analysis incomplete" ? " · " + st.label : "")]));
    s.appendChild(h("p", { text: f.message }));
    s.appendChild(P.kv([["Severity", f.severity], ["Confidence", f.confidence + " (strength of the static evidence)"], ["Verdict", f.verdict + " (from the analysis)"],
      ["Review", STATUS_LABEL[status(f)] + (revalidate[f.id] ? " — revalidate: " + revalidate[f.id] : "")], ["Analysis", f.analysisStatus],
      ["Rule", f.ruleId + " · " + (rule(f.ruleId).basis || "")], ["Origin", "deterministic rule (static analysis)"],
      f.symbol ? ["Function", h("a", { href: "#", class: "mono", text: f.symbol.qualified, on: { click: (ev) => { ev.preventDefault(); if (D.byId.has(f.symbolId)) GV.app.selectItem(D.byId.get(f.symbolId), { reveal: true, mode: "arch" }); } } })] : null,
      diffOf.has(f.id) ? ["Since the previous audit", diffOf.get(f.id).kind + (diffOf.get(f.id).changes ? ": " + diffOf.get(f.id).changes.join("; ") : "")] : null]));
    body.appendChild(s);
    const w = P.section("Why it can be a problem");
    w.appendChild(h("p", { class: "note", text: f.why }));
    body.appendChild(w);
    if ((f.checks || []).length) {
      const c = P.section("Checks identified", f.checks.length);
      f.checks.forEach((x) => c.appendChild(h("div", { class: "conn static" }, [h("span", { class: "sdot " + (x.kind === "validation" ? "review" : "check") }), h("div", { class: "two" }, [h("span", { class: "name", text: x.label }), h("span", { class: "sub", text: x.kind + (x.file ? " · " + x.file + ":" + x.line : "") })])])));
      body.appendChild(c);
    }
    if ((f.routes || []).length) {
      const r = P.section("Routes that may reach it", f.routes.length);
      f.routes.forEach((x) => r.appendChild(h("div", { class: "conn", on: { click: () => GV.app.openSecRoute(x.id) } }, [h("div", { class: "two" }, [h("span", { class: "name mono", text: x.key }), h("span", { class: "sub", text: "authentication " + x.auth + " · " + x.path.length + " steps" })])])));
      body.appendChild(r);
    }
    const lim = (f.limits || []).concat((f.unresolved || []).map((u) => "unresolved: " + u));
    if (lim.length) {
      const l = P.section("Limits of the analysis", lim.length);
      lim.forEach((x) => l.appendChild(h("p", { class: "note warn", text: x })));
      body.appendChild(l);
    }
  }
  function rule(id) { return (A.rules || []).find((r) => r.id === id) || {}; }
  function fEvidence(f, P, body) {
    const s = P.section("Evidence", f.evidence.length);
    const t = h("table", { class: "grid evid" });
    t.appendChild(h("tr", null, ["ID", "Kind", "Evidence", "Where"].map((x) => h("th", { text: x }))));
    f.evidence.forEach((e) => {
      t.appendChild(h("tr", { on: { click: () => { tabs.finding = "source"; drawFinding(f, P); } }, style: { cursor: e.file ? "pointer" : "default" } }, [
        h("td", { class: "mono", text: e.id }), h("td", null, [h("span", { class: "ev-role " + e.kind, text: e.kind }), e.basis === "heuristic" ? h("div", { class: "muted", text: "heuristic" }) : null]),
        h("td", null, [e.text, e.code ? h("div", { class: "mono muted", text: e.code }) : null]),
        h("td", { class: "mono", text: e.file ? e.file + ":" + e.line : "—" })]));
    });
    s.appendChild(t);
    s.appendChild(h("p", { class: "note", text: "Static evidence: where the analysis read each fact (go/ssa, go/types, Data Flow, the SQL Explorer, the HTTP catalog). Heuristic evidence relies on names or paths." }));
    body.appendChild(s);
  }
  function fSource(f, P, body) {
    const files = new Map();
    const mark = (file, line) => { if (!file || !line) return; if (!files.has(file)) files.set(file, new Set()); files.get(file).add(line); };
    mark(f.file, f.line);
    f.evidence.forEach((e) => mark(e.file, e.line));
    (f.flow || []).forEach((st) => mark(st.file, st.line));
    let any = false;
    files.forEach((lines, file) => {
      const ls = Array.from(lines);
      const view = codeExcerpts(file, Math.min.apply(null, ls) - 40, Math.max.apply(null, ls) + 40, lines);
      if (!view) return;
      any = true;
      const s = P.section(null);
      s.appendChild(h("div", { class: "code-head" }, [h("span", { class: "path", text: file }), h("span", { class: "muted", text: "marked: lines with evidence" })]));
      s.appendChild(view);
      body.appendChild(s);
      const first = view.querySelector("tr.mark");
      if (first) requestAnimationFrame(() => { view.scrollTop = Math.max(0, first.offsetTop - 60); });
    });
    if (!any) body.appendChild(h("div", { class: "empty" }, [h("strong", { text: "No code embedded" }), f.file === "go.mod" || !f.file ? "This finding is about configuration or dependencies." : "The file was not available when the report was exported."]));
    const route = (f.routes || [])[0];
    if (route) {
      const s = P.section("Callers and callees on the path");
      route.path.forEach((st) => {
        const i = st.id && D.byId.has(st.id) ? D.byId.get(st.id) : -1;
        s.appendChild(h("div", { class: "conn", on: { click: () => { if (i >= 0) GV.app.selectItem(i, {}); } } }, [h("span", { class: "ev-role", text: st.role }), h("div", { class: "two" }, [h("span", { class: "name mono", text: st.name }), h("span", { class: "sub", text: (st.file || "") + (st.line ? ":" + st.line : "") + (st.check ? " · ✓ " + st.check : "") })])]));
      });
      body.appendChild(s);
    }
  }
  function fFlow(f, P, body) {
    if ((f.flow || []).length) {
      const s = P.section("Data path (Data Flow, static)", f.flow.length);
      const ol = h("ol", { class: "flowsteps" });
      f.flow.forEach((st) => ol.appendChild(h("li", { class: st.role, title: st.via || "" }, [h("div", null, [h("span", { class: "ev-role " + st.role, text: st.role }), " ", h("span", { class: "mono", text: st.label })]),
        h("div", { class: "muted mono", text: (st.func ? st.func + " · " : "") + (st.file ? st.file + ":" + st.line : "") + (st.conf ? " · " + st.conf : "") }), st.code ? h("div", { class: "code", text: st.code }) : null])));
      s.appendChild(ol);
      s.appendChild(h("p", { class: "note", text: "A possible path found by static analysis, not a recorded execution. Sources are HTTP inputs; a sanitizer is listed only when its effect is known (a numeric conversion, filepath.Base)." }));
      body.appendChild(s);
    }
    (f.routes || []).slice(0, 4).forEach((r) => {
      const s = P.section("Execution path · " + r.key, r.path.length);
      r.path.forEach((st, k) => s.appendChild(h("div", { class: "conn static" }, [h("span", { class: "ord", text: String(k + 1) }), h("div", { class: "two" }, [h("span", { class: "name mono", text: st.name }), h("span", { class: "sub", text: st.role + (st.conf ? " · " + st.conf : "") + (st.check ? " · ✓ " + st.check : "") })])])));
      body.appendChild(s);
    });
    if (!(f.flow || []).length && !(f.routes || []).length) body.appendChild(h("div", { class: "empty", text: "This finding has no data path or route: it is a declaration or a setting." }));
  }
  function fFix(f, P, body) {
    const s = P.section("How to fix (from the rule)");
    f.remediation.forEach((x) => s.appendChild(h("p", { text: "• " + x })));
    body.appendChild(s);
    const t = P.section("How to test the fix");
    f.tests.forEach((x) => t.appendChild(h("p", { text: "• " + x })));
    body.appendChild(t);
    const res = ai[f.id];
    if (res && res.answer && res.validation && res.validation.valid) {
      const a = P.section(null);
      a.appendChild(h("div", { class: "ai-box" }, [h("span", { class: "ai-tag", text: "AI · " + res.model }), h("p", { class: "note", text: "Suggestions of the model, from the evidence it was given:" }),
        (res.answer.recommendations || []).map((x) => h("p", { text: "• " + x })), (res.answer.suggested_tests || []).map((x) => h("p", { text: "• test: " + x }))]));
      body.appendChild(a);
    }
  }
  function aiResult(res, P) {
    const box = h("div", { class: "ai-box" });
    const v = res.validation || {};
    box.appendChild(h("div", { class: "btn-row" }, [h("span", { class: "ai-tag", text: "AI analysis" }), h("span", { class: "muted", text: res.model + " · " + (res.generated || "").replace("T", " ").slice(0, 16) + (res.cached ? " · cached" : "") })]));
    box.appendChild(h("p", { class: "note", text: "Interpretation by a local model, not a finding. References were checked against the context it received: " + (v.acceptedEvidenceIds || []).length + " accepted, " + (v.rejected || []).length + " rejected." }));
    const an = res.answer;
    if (!an) { box.appendChild(h("p", { class: "note warn", text: "The model's answer was not valid JSON; nothing from it is shown." })); return box; }
    box.appendChild(h("p", { style: { fontWeight: "600" }, text: an.summary }));
    const rows = [["Possible problem", an.possible_problem], ["Why", an.why], ["Model confidence", an.confidence]];
    box.appendChild(P.kv(rows));
    const list = (title, xs) => { if (xs && xs.length) { box.appendChild(h("div", { class: "section-title", style: { marginTop: "10px" }, text: title })); xs.forEach((x) => box.appendChild(h("p", { class: "note", text: "• " + x }))); } };
    list("Data path", an.data_path); list("Transformations", an.transformations); list("Validations", an.validations); list("Security checks", an.security_checks);
    list("Review points", an.review_points); list("Undetermined relations", an.undetermined_relations); list("Missing information", an.missing_information);
    list("Recommendations", an.recommendations); list("Tests", an.suggested_tests);
    if ((an.references || []).length) list("References (validated)", an.references.map((r) => r.evidence_id + " " + r.file + ":" + r.start_line + "-" + r.end_line + (r.symbol ? " " + r.symbol : "")));
    if ((v.rejected || []).length) list("Rejected references", v.rejected.map((r) => r.reference + " — " + r.reason));
    if ((v.warnings || []).length) list("Warnings", v.warnings);
    if (an.suggested_status) box.appendChild(h("p", { class: "note", text: "Suggested review status (not applied): " + (STATUS_LABEL[an.suggested_status] || an.suggested_status) + (an.status_rationale ? " — " + an.status_rationale : "") }));
    if (!v.valid) box.appendChild(h("p", { class: "note warn", text: "The answer cites no valid evidence of its context: treat it with care." }));
    return box;
  }
  function fAI(f, P, body) {
    const s = P.section("Explain with Gemma");
    if (ai[f.id]) s.appendChild(aiResult(ai[f.id], P));
    if (LIVE) {
      const btn = h("button", { type: "button", class: "btn primary", text: ai[f.id] ? "Ask again" : "Explain with Gemma" });
      const state = h("span", { class: "muted" });
      const cancel = h("button", { type: "button", class: "btn sm", text: "Cancel", hidden: true });
      btn.addEventListener("click", () => {
        const ctrl = new AbortController();
        pending = { id: f.id, ctrl };
        btn.disabled = true; cancel.hidden = false;
        GV.clear(state).appendChild(h("span", { class: "btn-row" }, [h("span", { class: "spinner" }), "asking the local model… the evidence and snippets of this finding are sent to it"]));
        call("api/explain", { finding: f.id }, ctrl.signal).then((res) => { ai[f.id] = res; pending = null; GV.app.refreshSecurity(); },
          (err) => { pending = null; btn.disabled = false; cancel.hidden = true; state.textContent = err.name === "AbortError" ? "cancelled" : "failed: " + err.message; });
      });
      cancel.addEventListener("click", () => { if (pending && pending.ctrl) pending.ctrl.abort(); });
      s.appendChild(h("div", { class: "btn-row", style: { marginTop: "10px" } }, [btn, cancel, state]));
      s.appendChild(h("p", { class: "note", text: "Only the evidence of this finding and short snippets (secrets redacted) go to the model at " + "the local Ollama; the answer is validated before it is shown. The model never changes a finding or its status." }));
      call("api/ai/status").then((st) => {
        state.textContent = st.reachable ? "model: " + (st.selected || "none installed") : "Ollama not reachable: " + (st.error || "");
        if (!st.reachable || !st.selected) btn.disabled = true;
      }, () => {});
    } else {
      if (!ai[f.id]) s.appendChild(h("p", { class: "note", text: "No AI analysis was requested for this finding before the report was exported." }));
      s.appendChild(h("p", { class: "note", text: "This report is an offline file: it cannot reach a model. Ask from the terminal (the answer is embedded in the next export), or open the live Security Journey:" }));
      s.appendChild(cmdLine("gitview audit explain " + f.id));
      s.appendChild(cmdLine("gitview audit serve"));
    }
    body.appendChild(s);
  }
  function fReview(f, P, body) {
    const rec = reviews[f.id];
    const s = P.section("Review status");
    s.appendChild(P.kv([["Status", STATUS_LABEL[status(f)]], revalidate[f.id] ? ["Revalidate", revalidate[f.id]] : null]));
    if (LIVE) {
      const sel = h("select", { "aria-label": "New status" }, STATUSES.map((x) => h("option", { value: x, text: STATUS_LABEL[x], selected: x === status(f) ? true : null })));
      const note = h("textarea", { class: "note", placeholder: "Justification (required for confirmed, false positive, accepted risk and fixed)", "aria-label": "Justification" });
      const msg = h("span", { class: "muted" });
      const save = h("button", { type: "button", class: "btn primary", text: "Record decision", on: { click: () => {
        save.disabled = true;
        call("api/review", { finding: f.id, status: sel.value, note: note.value }).then((res) => {
          reviews[f.id] = res.record;
          if (res.revalidate) revalidate[f.id] = res.revalidate; else delete revalidate[f.id];
          GV.app.refreshSecurity();
        }, (err) => { save.disabled = false; msg.textContent = err.message; });
      } } });
      s.appendChild(h("div", { style: { display: "grid", gap: "6px", marginTop: "8px" } }, [sel, note, h("div", { class: "btn-row" }, [save, msg])]));
      s.appendChild(h("p", { class: "note", text: "Decisions are kept outside the repository with the commit analysed. The AI never records one." }));
    } else {
      s.appendChild(h("p", { class: "note", text: "This report is read-only. Record a decision from the terminal:" }));
      s.appendChild(cmdLine("gitview audit review " + f.id + " --status confirmed --note \"…\""));
    }
    body.appendChild(s);
    const hs = P.section("History", rec ? rec.history.length : 0);
    if (!rec || !rec.history.length) hs.appendChild(h("p", { class: "note", text: "No decision recorded." }));
    else {
      const t = h("table", { class: "grid" });
      t.appendChild(h("tr", null, ["Date", "From", "To", "Justification", "Commit"].map((x) => h("th", { text: x }))));
      rec.history.slice().reverse().forEach((d) => t.appendChild(h("tr", null, [h("td", { class: "mono", text: (d.time || "").replace("T", " ").slice(0, 16) }), h("td", { text: STATUS_LABEL[d.from] || d.from }),
        h("td", { text: STATUS_LABEL[d.to] || d.to }), h("td", { text: d.justification || "—" }), h("td", { class: "mono", text: (d.commit || "").slice(0, 12) })])));
      hs.appendChild(t);
    }
    body.appendChild(hs);
  }

  function drawRoute(id, P) {
    const post = postureOf.get(id);
    if (!post) return drawSummary(P);
    const k = routeIdx.get(id);
    P.drawHead("route", post.key, D.ROUTES[k] ? D.ROUTES[k].file + ":" + D.ROUTES[k].line : "", [k !== undefined ? h("button", { type: "button", class: "btn sm", on: { click: () => GV.app.openRoute(k) } }, [GV.icon("http"), "HTTP journey"]) : null].filter(Boolean),
      [h("span", { class: "badge kind", text: "route" })]);
    GV.clear(P.tabsEl);
    const body = GV.clear(P.body);
    const s = P.section("Security posture");
    s.appendChild(P.kv([["Authentication", post.auth], ["Unresolved calls", String(post.unresolved || 0)], ["SQL built at runtime", String(post.dynamicSql || 0) + " operation(s) reached"]]));
    body.appendChild(s);
    const c = P.section("Checks identified", (post.checks || []).length);
    (post.checks || []).forEach((x) => c.appendChild(h("div", { class: "conn static" }, [h("span", { class: "sdot check" }), h("div", { class: "two" }, [h("span", { class: "name", text: x.label }), h("span", { class: "sub", text: x.kind + (x.file ? " · " + x.file + ":" + x.line : "") })])])));
    if (!(post.checks || []).length) c.appendChild(h("p", { class: "note", text: "None identified in the chain." }));
    body.appendChild(c);
    const fs = (post.findings || []).map((x) => byId.get(x)).filter(Boolean);
    const fl = P.section("Findings", fs.length);
    fs.forEach((f) => fl.appendChild(findingRow(f, GV.app.state())));
    if (!fs.length) fl.appendChild(h("p", { class: "note", text: "No finding reaches this route." }));
    body.appendChild(fl);
  }

  function drawPath(t, P) {
    P.drawHead("path", "Explain this path", t.names.join(" → "), [], [h("span", { class: "badge kind", text: "path" })]);
    GV.clear(P.tabsEl);
    const body = GV.clear(P.body);
    const s = P.section("Steps", t.names.length);
    t.names.forEach((n, k) => s.appendChild(h("div", { class: "conn static" }, [h("span", { class: "ord", text: String(k + 1) }), h("span", { class: "name mono", text: n })])));
    body.appendChild(s);
    const a = P.section("Explain with Gemma");
    const key = t.from + "→" + t.to;
    if (ai[key]) a.appendChild(aiResult(ai[key], P));
    if (LIVE) {
      const btn = h("button", { type: "button", class: "btn primary", text: "Explain this path" });
      const msg = h("span", { class: "muted" });
      btn.addEventListener("click", () => {
        btn.disabled = true;
        GV.clear(msg).appendChild(h("span", { class: "btn-row" }, [h("span", { class: "spinner" }), "asking the local model…"]));
        call("api/explain-path", { from: t.from, to: t.to }).then((res) => { ai[key] = res; GV.app.refreshSecurity(); }, (err) => { btn.disabled = false; msg.textContent = "failed: " + err.message; });
      });
      a.appendChild(h("div", { class: "btn-row" }, [btn, msg]));
    } else {
      a.appendChild(h("p", { class: "note", text: "This report is offline. From the terminal:" }));
      a.appendChild(cmdLine("gitview audit explain-path --from '" + t.from + "' --to '" + t.to + "'"));
    }
    a.appendChild(h("p", { class: "note", text: "The relations between the steps are those the analysis found; a path is possible, not executed on every request." }));
    body.appendChild(a);
  }

  function drawSummary(P) {
    const commit = (A.commit || "").slice(0, 12) + (A.dirty ? " (uncommitted changes)" : "");
    P.drawHead("audit", "Security Journey", A.repository + " · commit " + commit, [h("button", { type: "button", class: "btn sm primary", on: { click: report } }, [GV.icon("file"), "Full report"])],
      [h("span", { class: "badge kind", text: "CodeAudit" }), h("span", { class: "badge " + (A.status.status === "complete" ? "ok" : "warn"), text: "analysis " + A.status.status })]);
    GV.clear(P.tabsEl);
    const body = GV.clear(P.body);
    const g = P.section("Findings", F.length);
    const grid = h("div", { class: "metrics" });
    SEVS.forEach((s) => grid.appendChild(h("div", { class: "metric" }, [h("div", { class: "v", text: String(F.filter((f) => f.severity === s).length) }), h("div", { class: "l" }, [sevBadge(s)])])));
    g.appendChild(grid);
    g.appendChild(P.kv([["Vulnerable", String(F.filter((f) => f.verdict === "vulnerable").length) + " (evidence sufficient)"], ["Review required", String(F.filter((f) => f.verdict === "review").length)],
      ["Incomplete", String(F.filter((f) => f.verdict === "incomplete").length)], ["Reviewed", String(F.filter((f) => status(f) !== "open").length)]]));
    body.appendChild(g);
    const sc = P.section("Scope");
    sc.appendChild(P.kv([["Repository", A.repository], ["Commit", commit], ["Analysed", (A.generated || "").replace("T", " ").slice(0, 19) + " UTC"], ["Modules", (A.modules || []).join(", ")],
      ["Rules", A.rulesVersion + " · " + (A.rules || []).filter((r) => r.enabled).length + " enabled"], ["Analyzer", "GitView " + (A.analyzerVersion || "")],
      ["Dependencies", A.dependencies.checked ? "checked against " + (A.dependencies.modified || "").slice(0, 10) + " database" + (A.dependencies.stale ? " (possibly out of date)" : "") : "not checked"]]));
    (A.status.reasons || []).forEach((r) => sc.appendChild(h("p", { class: "note warn", text: r })));
    if (A.dependencies.note) sc.appendChild(h("p", { class: "note", text: A.dependencies.note }));
    body.appendChild(sc);
    if (SEC.diff) {
      const d = SEC.diff;
      const ds = P.section("Compared with the previous audit");
      ds.appendChild(P.kv([["Previous commit", (d.baseCommit || "").slice(0, 12)], ["New", String((d.new || []).length)], ["Resolved", String((d.resolved || []).length)], ["Unchanged", String((d.unchanged || []).length)],
        ["Changed", String((d.changed || []).length)], ["Not confirmed gone", String((d.unconfirmed || []).length)]]));
      (d.resolved || []).concat(d.unconfirmed || []).slice(0, 12).forEach((x) => ds.appendChild(h("p", { class: "note", text: (x.reason ? "not confirmed: " : "resolved: ") + x.ruleId + " " + x.title + " (" + x.file + ":" + x.line + ")" + (x.reason ? " — " + x.reason : "") })));
      body.appendChild(ds);
    }
    const lg = P.section("Colours of the graph");
    const leg = h("div", { class: "legend-states" });
    [["check", "security check identified (authentication, authorization)"], ["review", "possible risk: review required"], ["vulnerable", "vulnerable by the evidence, or confirmed by a reviewer"],
      ["incomplete", "analysis incomplete or relation not resolved"], ["struct", "structural element without findings"]].forEach(([c, t]) => { leg.appendChild(h("span", { class: "sdot " + c })); leg.appendChild(h("span", { text: t })); });
    lg.appendChild(leg);
    lg.appendChild(h("p", { class: "note", text: "Every coloured element also says its state in text. Severity, confidence, verdict and review status are separate: see each finding." }));
    body.appendChild(lg);
    const ab = P.section("AI");
    ab.appendChild(h("p", { class: "note", text: LIVE ? "Live session: findings and paths can be explained by the local model, and decisions recorded." : "Offline report: it shows the AI analyses requested before the export (" + Object.keys(SEC.ai || {}).length + "), marked as such. The static findings do not depend on them." }));
    body.appendChild(ab);
  }

  // ---------- full report ----------
  function report() {
    const el = GV.$("report");
    GV.clear(el);
    const page = h("div", { class: "report-page" });
    page.appendChild(h("div", { class: "report-bar" }, [h("strong", { text: "Security report" }), h("span", { class: "grow" }),
      h("button", { type: "button", class: "btn", text: "Print", on: { click: () => window.print() } }), h("button", { type: "button", class: "btn", text: "Close", on: { click: () => { el.hidden = true; } } })]));
    const commit = (A.commit || "").slice(0, 12) + (A.dirty ? " (uncommitted changes)" : "");
    page.appendChild(h("h1", { text: "Security audit — " + A.repository }));
    page.appendChild(h("p", { class: "muted", text: "Commit " + commit + " · analysed " + (A.generated || "").replace("T", " ").slice(0, 19) + " UTC · rules " + A.rulesVersion + " · GitView " + (A.analyzerVersion || "") + " · analysis " + A.status.status }));
    page.appendChild(h("p", { text: F.length + " findings: " + SEVS.map((s) => F.filter((f) => f.severity === s).length + " " + s).join(", ") + ". Findings come from deterministic static analysis rules: each one cites its evidence. A finding is a possible problem; its review status records the decision of a person." }));
    (A.status.reasons || []).forEach((r) => page.appendChild(h("p", { class: "note warn", text: "Limit: " + r })));
    if (A.dependencies.note) page.appendChild(h("p", { class: "note", text: A.dependencies.note }));
    Object.keys(CATS).forEach((cat) => {
      const fs = F.filter((f) => f.category === cat);
      if (!fs.length) return;
      page.appendChild(h("h2", { text: CATS[cat] + " (" + fs.length + ")" }));
      fs.forEach((f) => {
        const box = h("div", { class: "finding" });
        box.appendChild(h("h3", null, [sevBadge(f.severity), h("span", { text: f.title }), h("span", { class: "badge kind rule", text: f.ruleId })]));
        box.appendChild(h("div", { class: "verdict " + stateOf(f).cls, text: f.verdictLabel + " · confidence " + f.confidence + " · review: " + STATUS_LABEL[status(f)] }));
        box.appendChild(h("p", { text: f.message }));
        box.appendChild(h("p", { class: "muted mono", text: f.id + " · " + (f.file || "") + (f.line ? ":" + f.line : "") }));
        if ((f.routes || []).length) box.appendChild(h("p", { class: "note", text: "Routes: " + f.routes.map((r) => r.key).slice(0, 8).join(", ") + (f.routes.length > 8 ? " …" : "") }));
        if ((f.routes || []).length) box.appendChild(h("p", { class: "note", text: "Path: " + f.routes[0].path.map((s) => s.name).join(" → ") }));
        if ((f.flow || []).length) box.appendChild(h("p", { class: "note", text: "Data: " + f.flow.map((s) => s.role + " " + s.label).join(" → ") }));
        box.appendChild(h("p", { class: "note", text: "Evidence: " + f.evidence.map((e) => e.id + " " + e.kind + (e.file ? " " + e.file + ":" + e.line : "")).join("; ") }));
        const code = f.file && f.line ? codeExcerpts(f.file, f.line - 4, f.line + 4, new Set([f.line])) : null;
        if (code) box.appendChild(code);
        box.appendChild(h("p", null, [h("strong", { text: "Fix: " }), f.remediation.join(" ")]));
        box.appendChild(h("p", null, [h("strong", { text: "Tests: " }), f.tests.join(" ")]));
        (f.limits || []).forEach((l) => box.appendChild(h("p", { class: "note warn", text: "Limit: " + l })));
        if (ai[f.id]) box.appendChild(aiResult(ai[f.id], GV.inspector.parts));
        page.appendChild(box);
      });
    });
    if (SEC.diff) {
      const d = SEC.diff;
      page.appendChild(h("h2", { text: "Comparison with the previous audit" }));
      page.appendChild(h("p", { text: "Previous commit " + (d.baseCommit || "").slice(0, 12) + ": " + (d.new || []).length + " new, " + (d.resolved || []).length + " resolved, " + (d.unchanged || []).length + " unchanged, " + (d.changed || []).length + " changed, " + (d.unconfirmed || []).length + " not confirmed gone." }));
    }
    page.appendChild(h("h2", { text: "Rules" }));
    (A.rules || []).forEach((r) => page.appendChild(h("p", { class: "note", text: r.id + " · " + r.title + " · " + r.severity + " · " + r.basis + " · " + (r.enabled ? r.findings + " finding(s)" : "not run") + " — " + r.limits })));
    el.appendChild(page);
    el.hidden = false;
  }

  GV.security = {
    available: true, live: !!LIVE, audit: A, findings: F, finding: (id) => byId.get(id), stateOf, status,
    build, drawSidebar, draw, report, pathKeys, excerptFor, codeExcerpts, tabs,
    setTab: (t) => { tabs.finding = t; }, tab: () => tabs.finding, pending: () => pending
  };
})(GV);
