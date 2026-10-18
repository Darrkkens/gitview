// Filters popover: element kinds, relation kinds and confidence levels
// shown on the canvas, how far the selection's dependency path goes, and
// the theme. Filters only hide what is drawn; the inspector still lists
// every relation.
(function (GV) {
  "use strict";
  const D = GV.data, h = GV.h;
  const pop = GV.$("filters"), btn = GV.$("filters-open"), badgeEl = GV.$("filters-badge");
  const KINDS = ["struct", "interface", "type", "func", "method", "closure", "route", "db", "unresolved"];
  const RELS = ["imports", "calls", "go", "defer", "handles", "queries", "implements", "embeds", "references"];

  function defaults() {
    const f = { kinds: {}, rels: {}, confs: {}, ext: false };
    KINDS.forEach((k) => { f.kinds[k] = true; });
    RELS.forEach((k) => { f.rels[k] = true; });
    D.CONFS.forEach((k) => { f.confs[k] = true; });
    return f;
  }
  function changed(f) {
    const d = defaults();
    return f.ext !== d.ext || KINDS.some((k) => f.kinds[k] !== d.kinds[k]) || RELS.some((k) => f.rels[k] !== d.rels[k]) || D.CONFS.some((k) => f.confs[k] !== d.confs[k]);
  }
  const present = {}, presentRel = {};
  D.N.forEach((n) => { present[n.type] = (present[n.type] || 0) + 1; });
  D.E.forEach((e) => { presentRel[e.type] = (presentRel[e.type] || 0) + 1; });

  function check(label, on, set, extra, count) {
    return h("label", { class: "check" }, [h("input", { type: "checkbox", checked: on ? true : null, on: { change: (ev) => set(ev.target.checked) } }), extra || null, label,
      count != null ? h("span", { class: "muted", text: GV.fmt(count) }) : null]);
  }
  function draw() {
    const S = GV.app.state(), f = S.filters;
    GV.clear(pop);
    const update = () => { GV.app.setFilters(f); draw(); };
    const el = h("div", { class: "section" }, [h("div", { class: "section-title" }, ["Elements"])]);
    const opts = h("div", { class: "opts" });
    KINDS.forEach((k) => { if (present[k]) opts.appendChild(check(D.KIND[k].label, f.kinds[k] !== false, (v) => { f.kinds[k] = v; update(); }, null, present[k])); });
    if (present.external || D.EXT >= 0) opts.appendChild(check("external dependencies", f.ext, (v) => { f.ext = v; update(); }, null, present.external || 0));
    el.appendChild(opts);
    pop.appendChild(el);
    const rl = h("div", { class: "section" }, [h("div", { class: "section-title" }, ["Relations"])]);
    const ro = h("div", { class: "opts" });
    RELS.forEach((k) => { if (presentRel[k]) ro.appendChild(check(D.REL[k].label, f.rels[k] !== false, (v) => { f.rels[k] = v; update(); }, null, presentRel[k])); });
    rl.appendChild(ro);
    pop.appendChild(rl);
    const cf = h("div", { class: "section" }, [h("div", { class: "section-title" }, ["Confidence"])]);
    const co = h("div", { class: "opts one" });
    D.CONFS.forEach((k) => co.appendChild(check(k, f.confs[k] !== false, (v) => { f.confs[k] = v; update(); }, h("span", { class: "sw " + k }))));
    cf.appendChild(co);
    pop.appendChild(cf);
    const pv = h("div", { class: "section" }, [h("div", { class: "section-title" }, ["Selection"])]);
    pv.appendChild(h("div", { class: "seg" }, [
      h("button", { type: "button", "aria-pressed": String(!S.pathDepth), text: "Full dependency path", on: { click: () => { GV.app.setPathDepth(0); draw(); } } }),
      h("button", { type: "button", "aria-pressed": String(S.pathDepth === 1), text: "Direct only", on: { click: () => { GV.app.setPathDepth(1); draw(); } } })]));
    pop.appendChild(pv);
    const th = h("div", { class: "section" }, [h("div", { class: "section-title" }, ["Theme"])]);
    const cur = document.documentElement.getAttribute("data-theme") || "dark";
    th.appendChild(h("div", { class: "seg" }, ["dark", "light"].map((t) => h("button", { type: "button", "aria-pressed": String(cur === t), text: t === "dark" ? "Dark" : "Light", on: { click: () => { GV.app.setTheme(t); draw(); } } }))));
    pop.appendChild(th);
    pop.appendChild(h("div", { class: "section" }, [h("button", { type: "button", class: "btn sm", text: "Reset filters", on: { click: () => { GV.app.setFilters(defaults()); draw(); } } })]));
    badgeEl.hidden = !changed(f);
  }
  function toggle(force) {
    const on = force !== undefined ? force : pop.hidden;
    pop.hidden = !on;
    btn.setAttribute("aria-pressed", String(on));
    btn.setAttribute("aria-expanded", String(on));
    if (on) draw();
  }
  btn.addEventListener("click", () => toggle());
  document.addEventListener("mousedown", (ev) => { if (!pop.hidden && !pop.contains(ev.target) && !btn.contains(ev.target)) toggle(false); });

  GV.filters = { defaults, changed, toggle, isOpen: () => !pop.hidden, refreshBadge: () => { badgeEl.hidden = !changed(GV.app.state().filters); } };
})(GV);
