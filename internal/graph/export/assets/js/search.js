// Global search (Ctrl+K or /): every tree item and every HTTP route, ranked
// by how the query matches the name, then the qualified name, then the
// path. Enter opens the result where it lives.
(function (GV) {
  "use strict";
  const D = GV.data, T = D.T, h = GV.h;
  const pal = GV.$("palette"), input = GV.$("palette-input"), list = GV.$("palette-results"), kindsEl = GV.$("palette-kinds");
  const KINDS = [["all", "All"], ["package", "Packages"], ["file", "Files"], ["type", "Types"], ["func", "Functions"], ["route", "Routes"], ["db", "SQL"], ["external", "External"]];
  const GROUP = { module: "package", dir: "package", package: "package", file: "file", struct: "type", interface: "type", type: "type",
    func: "func", method: "func", closure: "func", db: "db", external: "external", unresolved: "func" };
  let kind = "all", hits = [], cur = 0, lastFocus = null;
  const LOWER = T.map((t) => [t.label.toLowerCase(), (t.short || "").toLowerCase(), t.n && t.n.qual ? t.n.qual.toLowerCase() : "",
    t.n && t.n.file ? t.n.file.toLowerCase() : "", t.n && t.n.type === "package" ? t.n.name.toLowerCase() : t.kind === "dir" ? t.name.toLowerCase() : ""]);
  const RLOWER = D.ROUTES.map((r) => [(r.method + " " + r.path).toLowerCase(), r.path.toLowerCase(), (r.handler ? r.handler.name : "").toLowerCase()]);

  function groupOf(t) {
    if (t.n && t.n.ext) return "external";
    if (t.kind === "dir" && t.ext) return "external";
    return GROUP[t.kind] || "";
  }
  function rank(L, q) {
    if (L[0] === q || L[1] === q) return 0;
    if (L[0].startsWith(q) || L[1].startsWith(q)) return 1;
    if (L[0].indexOf(q) >= 0) return 2;
    if (L[2].indexOf(q) >= 0 || L[4].indexOf(q) >= 0) return 3;
    if (L[3].indexOf(q) >= 0) return 4;
    return -1;
  }
  function search(text) {
    const q = text.trim().toLowerCase();
    const out = [];
    if (!q) return out;
    const words = q.split(/\s+/);
    for (let i = 0; i < T.length; i++) {
      const t = T[i];
      if (i === D.ROOT || t.kind === "route" || t.kind === "extgroup") continue;
      if (kind !== "all" && groupOf(t) !== kind) continue;
      let r = rank(LOWER[i], q);
      if (r < 0 && words.length > 1 && words.every((w) => LOWER[i].some((s) => s.indexOf(w) >= 0))) r = 5;
      if (r >= 0) out.push({ r, i, d: t.n ? t.n.deg : 100 });
    }
    if (kind === "all" || kind === "route") D.ROUTES.forEach((rt, k) => {
      const L = RLOWER[k];
      let r = L[0] === q || L[1] === q ? 0 : L[0].startsWith(q) || L[1].startsWith(q) ? 1 : L[0].indexOf(q) >= 0 ? 2 : L[2].indexOf(q) >= 0 ? 3 : -1;
      if (r < 0 && words.every((w) => L[0].indexOf(w) >= 0)) r = 2;
      if (r >= 0) out.push({ r, route: k, d: 50 });
    });
    out.sort((a, b) => a.r - b.r || b.d - a.d || (labelOf(a).length - labelOf(b).length) || (labelOf(a) < labelOf(b) ? -1 : 1));
    return out.slice(0, 60);
  }
  const labelOf = (x) => x.route !== undefined ? D.ROUTES[x.route].path : T[x.i].label;

  function mark(el, text, q) {
    const k = q ? text.toLowerCase().indexOf(q) : -1;
    if (k < 0) { el.textContent = text; return el; }
    el.appendChild(document.createTextNode(text.slice(0, k)));
    el.appendChild(h("span", { class: "hl", text: text.slice(k, k + q.length) }));
    el.appendChild(document.createTextNode(text.slice(k + q.length)));
    return el;
  }
  function draw() {
    GV.clear(list);
    const q = input.value.trim().toLowerCase();
    if (!q) {
      list.appendChild(h("li", { class: "empty" }, [h("strong", { text: "Search the repository" }), "Packages, files, types, functions, routes and SQL operations. Use Tab to narrow by kind."]));
      return;
    }
    if (!hits.length) { list.appendChild(h("li", { class: "empty", text: "Nothing matches “" + input.value.trim() + "”." })); return; }
    hits.forEach((x, j) => {
      let row;
      if (x.route !== undefined) {
        const r = D.ROUTES[x.route];
        row = h("li", { class: "row", role: "option", "aria-selected": String(j === cur), id: "pal-" + j }, [
          h("span", { class: "method m-" + r.method, text: r.method }),
          h("div", { class: "two" }, [mark(h("span", { class: "name mono" }), r.path, q), h("span", { class: "sub", text: (r.handler ? r.handler.name : "handler not resolved") + " · " + r.file + ":" + r.line })]),
          h("span", { class: "badge kind", text: "route" })]);
      } else {
        const t = T[x.i], n = t.n;
        const dot = h("span", { class: "kdot" + (D.FUNC[t.kind] ? " round" : "") });
        dot.style.background = GV.token(D.kindColor(t));
        const where = n ? (n.file ? GV.loc(n) : n.pkg || n.name) : t.name;
        row = h("li", { class: "row", role: "option", "aria-selected": String(j === cur), id: "pal-" + j, title: D.reference(t) }, [dot,
          h("div", { class: "two" }, [mark(h("span", { class: "name" }), D.displayName(t), q), h("span", { class: "sub", text: where })]),
          h("span", { class: "badge kind", text: D.kindLabel(t) })]);
      }
      row.addEventListener("mousedown", (ev) => { ev.preventDefault(); cur = j; pick(); });
      row.addEventListener("mousemove", () => { if (cur !== j) { cur = j; mark2(); } });
      list.appendChild(row);
    });
    input.setAttribute("aria-activedescendant", "pal-" + cur);
  }
  function mark2() {
    Array.from(list.children).forEach((li, j) => li.setAttribute("aria-selected", String(j === cur)));
    const el = list.children[cur];
    if (el && el.scrollIntoView) el.scrollIntoView({ block: "nearest" });
    input.setAttribute("aria-activedescendant", "pal-" + cur);
  }
  function drawKinds() {
    GV.clear(kindsEl);
    KINDS.forEach(([k, label]) => {
      if (k === "route" && !D.ROUTES.length) return;
      kindsEl.appendChild(h("button", { type: "button", "aria-pressed": String(kind === k), text: label, on: { mousedown: (ev) => { ev.preventDefault(); setKind(k); } } }));
    });
  }
  function setKind(k) { kind = k; drawKinds(); hits = search(input.value); cur = 0; draw(); }
  function pick() {
    const x = hits[cur];
    if (!x) return;
    close();
    if (x.route !== undefined) GV.app.openRoute(x.route);
    else GV.app.selectItem(x.i, { reveal: true, force: true, from: "search" });
  }
  function open(prefill) {
    lastFocus = document.activeElement;
    pal.hidden = false;
    if (prefill !== undefined) input.value = prefill;
    drawKinds();
    hits = search(input.value);
    cur = 0;
    draw();
    input.focus();
    input.select();
  }
  function close() {
    pal.hidden = true;
    if (lastFocus && lastFocus.focus) lastFocus.focus({ preventScroll: true });
  }
  input.addEventListener("input", () => { hits = search(input.value); cur = 0; draw(); });
  input.addEventListener("keydown", (ev) => {
    if (ev.key === "ArrowDown") { cur = Math.min(cur + 1, hits.length - 1); mark2(); ev.preventDefault(); }
    else if (ev.key === "ArrowUp") { cur = Math.max(cur - 1, 0); mark2(); ev.preventDefault(); }
    else if (ev.key === "Enter") { pick(); ev.preventDefault(); }
    else if (ev.key === "Escape") { close(); ev.preventDefault(); }
    else if (ev.key === "Tab") {
      const ks = KINDS.filter(([k]) => k !== "route" || D.ROUTES.length).map(([k]) => k);
      setKind(ks[(ks.indexOf(kind) + (ev.shiftKey ? ks.length - 1 : 1)) % ks.length]);
      ev.preventDefault();
    }
  });
  pal.addEventListener("mousedown", (ev) => { if (ev.target === pal) close(); });
  GV.$("search-open").addEventListener("click", () => open());

  GV.search = { open, close, search, isOpen: () => !pal.hidden };
})(GV);
