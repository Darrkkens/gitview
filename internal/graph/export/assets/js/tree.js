// Explorer tree (sidebar). Virtualized: only the rows in view exist in the
// DOM, so the whole repository can be listed. Its expansion is its own;
// selecting a row reveals the item on the canvas.
(function (GV) {
  "use strict";
  const D = GV.data, T = D.T, h = GV.h;
  const ROW = 24;
  const el = GV.$("tree");
  const spacer = h("div", { class: "spacer" });
  el.appendChild(spacer);
  const open = new Set([D.ROOT].concat(D.modules.map((m) => m.i)));
  let rows = [], filter = "", sel = -1, cursor = -1;

  function flatten() {
    rows = [];
    if (filter) {
      const q = filter.toLowerCase();
      const keep = new Uint8Array(T.length);
      let hits = 0;
      for (let i = 0; i < T.length && hits < 3000; i++) {
        const t = T[i];
        if (i === D.ROOT) continue;
        if (t.label.toLowerCase().indexOf(q) >= 0 || (t.n && t.n.qual && t.n.qual.toLowerCase().indexOf(q) >= 0)) {
          keep[i] = 1; hits++;
          for (const a of D.ancestors(i)) if (!keep[a]) keep[a] = 2;
        }
      }
      const stack = [[D.ROOT, 0]];
      while (stack.length) {
        const [i, d] = stack.pop();
        rows.push([i, d]);
        const kids = T[i].kids.filter((k) => keep[k]);
        for (let k = kids.length - 1; k >= 0; k--) stack.push([kids[k], d + 1]);
      }
      return;
    }
    const stack = [[D.ROOT, 0]];
    while (stack.length) {
      const [i, d] = stack.pop();
      rows.push([i, d]);
      if (!open.has(i)) continue;
      const kids = T[i].kids;
      for (let k = kids.length - 1; k >= 0; k--) stack.push([kids[k], d + 1]);
    }
  }

  function row(k) {
    const [i, d] = rows[k], t = T[i];
    const hasKids = t.kids.length > 0;
    const isOpen = filter ? true : open.has(i);
    const tw = h("span", { class: "tw" + (isOpen ? " open" : ""), "aria-hidden": "true" }, hasKids ? [GV.icon("chevron")] : []);
    if (hasKids) tw.addEventListener("click", (ev) => { ev.stopPropagation(); toggle(i); });
    const dot = h("span", { class: "kdot" + (D.FUNC[t.kind] ? " round" : "") });
    dot.style.background = GV.token(D.kindColor(t));
    const label = h("span", { class: "label" });
    appendMatch(label, D.displayName(t, true));
    const r = h("div", {
      class: "trow" + (t.ext || (t.n && t.n.ext) ? " ext" : "") + (k === cursor ? " cursor" : ""),
      role: "treeitem", "aria-level": d + 1, "aria-selected": String(i === sel), "aria-expanded": hasKids ? String(isOpen) : null,
      title: D.reference(t) + " · " + D.kindLabel(t), "data-i": i,
      style: { top: k * ROW + "px", paddingLeft: 6 + d * 14 + "px" },
      on: {
        click: () => { cursor = k; pick(i); },
        dblclick: () => { if (hasKids) toggle(i); }
      }
    }, [tw, dot, label]);
    if (hasKids && t.count) r.appendChild(h("span", { class: "count", text: GV.fmt(t.count) }));
    return r;
  }
  function appendMatch(el, text) {
    if (!filter) { el.textContent = text; return; }
    const k = text.toLowerCase().indexOf(filter.toLowerCase());
    if (k < 0) { el.textContent = text; return; }
    el.appendChild(document.createTextNode(text.slice(0, k)));
    el.appendChild(h("span", { class: "m", text: text.slice(k, k + filter.length) }));
    el.appendChild(document.createTextNode(text.slice(k + filter.length)));
  }

  let raf = 0;
  function paint() {
    raf = 0;
    spacer.style.height = rows.length * ROW + "px";
    const top = el.scrollTop, hgt = el.clientHeight || 600;
    const a = Math.max(0, Math.floor(top / ROW) - 8), b = Math.min(rows.length, Math.ceil((top + hgt) / ROW) + 8);
    GV.clear(spacer);
    const frag = document.createDocumentFragment();
    for (let k = a; k < b; k++) frag.appendChild(row(k));
    spacer.appendChild(frag);
  }
  function render() { flatten(); if (!raf) raf = requestAnimationFrame(paint); }
  el.addEventListener("scroll", () => { if (!raf) raf = requestAnimationFrame(paint); });
  if (window.ResizeObserver) new ResizeObserver(() => { if (!raf) raf = requestAnimationFrame(paint); }).observe(el);

  function toggle(i) {
    if (open.has(i)) open.delete(i); else open.add(i);
    render();
  }
  function pick(i) {
    sel = i;
    render();
    GV.app.selectItem(i, { from: "tree" });
  }
  // select marks an item chosen elsewhere and scrolls it into view.
  function select(i) {
    sel = i;
    if (i >= 0 && !filter) for (const a of D.ancestors(i)) open.add(a);
    flatten();
    const k = rows.findIndex((r) => r[0] === i);
    if (k >= 0) {
      cursor = k;
      const top = k * ROW, vh = el.clientHeight || 600;
      if (top < el.scrollTop + ROW || top > el.scrollTop + vh - ROW * 2) el.scrollTop = Math.max(0, top - vh / 3);
    }
    if (!raf) raf = requestAnimationFrame(paint);
  }

  el.addEventListener("keydown", (ev) => {
    if (!rows.length) return;
    if (cursor < 0) cursor = 0;
    const [i] = rows[cursor] || [];
    let handled = true;
    if (ev.key === "ArrowDown") cursor = Math.min(rows.length - 1, cursor + 1);
    else if (ev.key === "ArrowUp") cursor = Math.max(0, cursor - 1);
    else if (ev.key === "ArrowRight") { if (T[i].kids.length && !open.has(i)) toggle(i); else if (T[i].kids.length) cursor++; }
    else if (ev.key === "ArrowLeft") {
      if (open.has(i) && T[i].kids.length) toggle(i);
      else { const p = T[i].parent; const k = rows.findIndex((r) => r[0] === p); if (k >= 0) cursor = k; }
    } else if (ev.key === "Enter" || ev.key === " ") pick(i);
    else if (ev.key === "Home") cursor = 0;
    else if (ev.key === "End") cursor = rows.length - 1;
    else handled = false;
    if (!handled) return;
    ev.preventDefault();
    ev.stopPropagation();
    const top = cursor * ROW;
    if (top < el.scrollTop) el.scrollTop = top;
    else if (top + ROW > el.scrollTop + el.clientHeight) el.scrollTop = top + ROW - el.clientHeight;
    if (!raf) raf = requestAnimationFrame(paint);
  });

  const input = GV.$("sidebar-filter");
  input.addEventListener("input", GV.debounce(() => { filter = input.value.trim(); el.scrollTop = 0; render(); }, 120));
  input.addEventListener("keydown", (ev) => { if (ev.key === "Escape") { input.value = ""; filter = ""; render(); input.blur(); } else if (ev.key === "ArrowDown") { el.focus(); cursor = 0; render(); ev.preventDefault(); } });

  GV.tree = { render, select, isOpen: (i) => open.has(i) };
  render();
})(GV);
