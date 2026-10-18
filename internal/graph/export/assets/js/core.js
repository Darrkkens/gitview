// GitView Architecture Explorer: shared namespace and helpers.
//
// The page is split in modules (see html.go for the order). Each one adds
// itself to GV and talks to the others through GV and the small event bus
// below. Nothing is fetched: the dataset, the sources and the scripts are in
// the page. Repository strings are always inserted with textContent, never
// as markup.
var GV = (function () {
  "use strict";
  const GV = {};

  // ---------- DOM ----------
  const SVG_NS = "http://www.w3.org/2000/svg";
  GV.$ = (id) => document.getElementById(id);
  GV.clear = (el) => { while (el.firstChild) el.removeChild(el.firstChild); return el; };
  // h(tag, attrs, children): attrs.text sets textContent, attrs.on adds
  // listeners, attrs.class/style/data-*/aria-* are attributes; false, null
  // and undefined skip the attribute.
  GV.h = function h(tag, attrs, kids) {
    const el = document.createElement(tag);
    if (attrs) for (const k in attrs) {
      const v = attrs[k];
      if (k === "text") el.textContent = v == null ? "" : String(v);
      else if (k === "on") for (const ev in v) el.addEventListener(ev, v[ev]);
      else if (k === "class") el.className = v;
      else if (k === "style" && typeof v === "object") Object.assign(el.style, v);
      else if (k === "value" && (tag === "input" || tag === "textarea" || tag === "select")) el.value = v;
      else if (v !== null && v !== undefined && v !== false) el.setAttribute(k, v === true ? "" : v);
    }
    append(el, kids);
    return el;
  };
  function append(el, kids) {
    if (kids == null) return;
    if (!Array.isArray(kids)) kids = [kids];
    for (const c of kids) {
      if (c == null || c === false) continue;
      if (Array.isArray(c)) append(el, c);
      else el.appendChild(typeof c === "string" || typeof c === "number" ? document.createTextNode(String(c)) : c);
    }
  }
  GV.append = append;

  // Icons: a few stroked 16×16 paths, built as SVG nodes.
  const ICONS = {
    chevron: "M6 3l5 5-5 5",
    copy: "M5.5 5.5V3a1 1 0 0 1 1-1H13a1 1 0 0 1 1 1v6.5a1 1 0 0 1-1 1h-2.5M3 5.5h6.5a1 1 0 0 1 1 1V13a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V6.5a1 1 0 0 1 1-1z",
    focus: "M8 5.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5M8 1.5v2.5M8 12v2.5M1.5 8H4M12 8h2.5",
    expand: "M3 6l5 5 5-5",
    collapse: "M3 10l5-5 5 5",
    target: "M8 2.5v3M8 10.5v3M2.5 8h3M10.5 8h3M8 6.5a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3",
    lock: "M4.5 7V5a3.5 3.5 0 0 1 7 0v2M3.5 7h9v6.5h-9z",
    warn: "M8 2 14.5 13.5h-13zM8 6.5v3.5M8 12v.01",
    back: "M10 3 5 8l5 5",
    close: "M4 4l8 8M12 4l-8 8",
    http: "M2 8h12M8 2c2 2 2.8 4 2.8 6S10 12 8 14M8 2C6 4 5.2 6 5.2 8S6 12 8 14M8 2a6 6 0 1 1 0 12A6 6 0 0 1 8 2",
    file: "M4 1.5h5L12.5 5v9.5h-8.5zM9 1.5V5h3.5",
    link: "M7 9a3 3 0 0 0 4.2 0l2-2a3 3 0 0 0-4.2-4.2l-.6.6M9 7a3 3 0 0 0-4.2 0l-2 2a3 3 0 0 0 4.2 4.2l.6-.6",
    download: "M8 2v8M4.5 6.5 8 10l3.5-3.5M2.5 13.5h11"
  };
  GV.icon = function (name, cls) {
    const s = document.createElementNS(SVG_NS, "svg");
    s.setAttribute("viewBox", "0 0 16 16");
    s.setAttribute("aria-hidden", "true");
    if (cls) s.setAttribute("class", cls);
    const p = document.createElementNS(SVG_NS, "path");
    p.setAttribute("d", ICONS[name] || "");
    s.appendChild(p);
    return s;
  };

  // ---------- formatting ----------
  GV.fmt = (n) => Number(n || 0).toLocaleString("en-US");
  GV.plural = (n, one, many) => GV.fmt(n) + " " + (n === 1 ? one : (many || one + "s"));
  GV.baseName = (p) => { const i = p.lastIndexOf("/"); return i >= 0 ? p.slice(i + 1) : p; };
  GV.loc = (n) => n && n.file ? n.file + (n.line ? ":" + n.line : "") : "";

  // ---------- events ----------
  const listeners = {};
  GV.on = (ev, fn) => { (listeners[ev] = listeners[ev] || []).push(fn); };
  GV.emit = (ev, arg) => { (listeners[ev] || []).forEach((fn) => fn(arg)); };

  // ---------- per-viewer preferences (best effort) ----------
  // Panel widths and toggles only: never state another viewer or a later
  // export needs. Storage can be unavailable (private windows, file://
  // restrictions), so every access is guarded.
  GV.pref = {
    get(key, def) {
      try { const v = window.localStorage.getItem("gitview." + key); return v === null ? def : JSON.parse(v); } catch (e) { return def; }
    },
    set(key, val) {
      try { window.localStorage.setItem("gitview." + key, JSON.stringify(val)); } catch (e) { /* unavailable */ }
    }
  };

  // ---------- theme tokens ----------
  let tokens = null;
  GV.token = function (name) {
    if (!tokens) tokens = {};
    if (!(name in tokens)) tokens[name] = getComputedStyle(document.documentElement).getPropertyValue("--" + name).trim() || "#888";
    return tokens[name];
  };
  GV.resetTokens = () => { tokens = null; };

  // ---------- clipboard and files ----------
  GV.copy = function (text) {
    return new Promise((resolve) => {
      try {
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(text).then(() => resolve(true), () => resolve(fallbackCopy(text)));
          return;
        }
      } catch (e) { /* fall back */ }
      resolve(fallbackCopy(text));
    });
  };
  function fallbackCopy(text) {
    const ta = GV.h("textarea", { style: { position: "fixed", left: "-9999px", top: "0" } });
    ta.value = text;
    document.body.appendChild(ta);
    ta.select();
    let ok = false;
    try { ok = document.execCommand("copy"); } catch (e) { ok = false; }
    document.body.removeChild(ta);
    return ok;
  }
  // copyButton: a small button that copies what get() returns.
  GV.copyButton = function (label, get, title) {
    const b = GV.h("button", { type: "button", class: "btn sm", title: title || "Copy " + label.toLowerCase() }, [GV.icon("copy"), label]);
    b.addEventListener("click", (ev) => {
      ev.stopPropagation();
      GV.copy(get()).then((ok) => GV.toast(ok ? "Copied " + label.toLowerCase() : "Copy failed: the browser blocked the clipboard"));
    });
    return b;
  };
  GV.download = function (name, text, type) {
    const a = GV.h("a", { download: name });
    a.href = URL.createObjectURL(new Blob([text], { type: type || "text/plain" }));
    document.body.appendChild(a);
    a.click();
    setTimeout(() => { URL.revokeObjectURL(a.href); document.body.removeChild(a); }, 0);
  };

  // ---------- toast ----------
  let toastTimer = 0;
  GV.toast = function (msg, action) {
    const t = GV.$("toast");
    if (!t) return;
    GV.clear(t);
    t.appendChild(document.createTextNode(msg));
    if (action) t.appendChild(GV.h("button", { type: "button", class: "btn sm", text: action.label, on: { click: () => { t.hidden = true; action.run(); } } }));
    t.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { t.hidden = true; }, action ? 6000 : 2200);
  };

  GV.debounce = function (fn, ms) {
    let t = 0;
    return function () { const args = arguments; clearTimeout(t); t = setTimeout(() => fn.apply(null, args), ms); };
  };

  return GV;
})();
