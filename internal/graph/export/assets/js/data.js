// Dataset and hierarchy. Reads the inert #gitview-data block once and
// builds the indexes every view shares:
//   N, E     nodes and edges as exported (static analysis results, unchanged)
//   T        the exploration tree: Repository → Module → directory →
//            Package → File → declaration (→ method, closure, call site).
//            T[i] for i < N.length is node N[i]; synthetic items (the
//            repository, directories, external groups) come after.
// Only the presentation is derived here: a directory item groups packages
// by import path, a method sits under its receiver type when both are in
// the same file, a database operation or unresolved call under the only
// function of its file that reaches it. Relations are never invented.
(function (GV) {
  "use strict";
  const raw = JSON.parse(document.getElementById("gitview-data").textContent);
  const K = raw.kinds, ET = raw.etypes, CF = raw.confs, ES = raw.esrc;

  const N = raw.nodes.map((a, i) => ({
    i, id: a[0], type: K[a[1]], name: a[2], qual: a[3], pkg: a[4], file: a[5], line: a[6], end: a[7], sig: a[8],
    ext: !!a[9], parent: a[10], group: a[11], comm: a[12], deg: a[13], meta: a[14] || {}, out: [], inn: []
  }));
  const E = raw.edges.map((a, i) => {
    const e = { i, s: a[0], t: a[1], type: ET[a[2]], conf: CF[a[3]], w: a[4] || 1, file: a[5], line: a[6], note: a[7], meta: a[8] || {}, src: ES[a[9]] };
    N[e.s].out.push(i);
    N[e.t].inn.push(i);
    return e;
  });
  const byId = new Map();
  N.forEach((n) => byId.set(n.id, n.i));
  const COMM = {};
  raw.communities.forEach((c) => { COMM[c[0]] = { id: c[0], level: c[1], label: c[2], size: c[3], members: c[4] }; });

  // ---------- vocabulary ----------
  const KIND = {
    repo: { label: "repository", color: "k-module", order: 0 },
    module: { label: "module", color: "k-module", order: 1 },
    dir: { label: "directory", color: "k-group", order: 2 },
    extgroup: { label: "external dependencies", color: "k-external", order: 90 },
    package: { label: "package", color: "k-package", order: 3 },
    file: { label: "file", color: "k-file", order: 4 },
    struct: { label: "struct", color: "k-type", order: 10 },
    interface: { label: "interface", color: "k-interface", order: 11 },
    type: { label: "named type", color: "k-type", order: 12 },
    func: { label: "function", color: "k-func", order: 13 },
    method: { label: "method", color: "k-func", order: 14 },
    closure: { label: "closure", color: "k-func", order: 15 },
    route: { label: "HTTP route", color: "k-route", order: 16 },
    db: { label: "database operation", color: "k-db", order: 17 },
    external: { label: "external function", color: "k-external", order: 18 },
    unresolved: { label: "unresolved call", color: "k-unresolved", order: 19 }
  };
  const REL = {
    imports: { label: "imports", inv: "imported by", order: 0 },
    calls: { label: "calls", inv: "called by", order: 1 },
    go: { label: "starts goroutine", inv: "goroutine started by", order: 2 },
    defer: { label: "defers", inv: "deferred by", order: 3 },
    handles: { label: "handled by", inv: "handles", order: 4 },
    queries: { label: "queries", inv: "queried by", order: 5 },
    implements: { label: "implements", inv: "implemented by", order: 6 },
    embeds: { label: "embeds", inv: "embedded by", order: 7 },
    references: { label: "uses type", inv: "type used by", order: 8 },
    method: { label: "has method", inv: "method of", order: 9 },
    contains: { label: "contains", inv: "contained in", order: 10 }
  };
  const CONF_RANK = { confirmed: 4, likely: 3, possible: 2, unresolved: 1 };
  const CONFS = ["confirmed", "likely", "possible", "unresolved"];
  const FUNC = { func: 1, method: 1, closure: 1 };
  const TYPE = { struct: 1, interface: 1, type: 1 };
  const SYMBOL = { struct: 1, interface: 1, type: 1, func: 1, method: 1, closure: 1, route: 1, db: 1, unresolved: 1, external: 1 };
  const FLOW = { calls: 1, go: 1, defer: 1, handles: 1, queries: 1 };
  // Structural relations are drawn as nesting, not as lines.
  const STRUCTURAL = { contains: 1, method: 1 };

  // ---------- tree ----------
  const T = N.map((n) => ({ i: n.i, n, id: n.id, kind: n.type, name: n.name, label: n.name, parent: -1, kids: [], depth: 0, count: 0, nfiles: 0, npkgs: 0 }));
  function synth(id, kind, name, label) {
    const t = { i: T.length, n: null, id, kind, name, label: label || name, parent: -1, kids: [], depth: 0, count: 0, nfiles: 0, npkgs: 0 };
    T.push(t);
    byId.set(id, t.i);
    return t.i;
  }
  const ROOT = synth("repo:" + raw.repository, "repo", raw.repository);
  function setParent(c, p) { T[c].parent = p; }

  // Modules, ordered by path.
  const modules = N.filter((n) => n.type === "module").sort((a, b) => a.name < b.name ? -1 : 1);
  modules.forEach((m) => { setParent(m.i, ROOT); T[m.i].label = GV.baseName(m.name) || m.name; });
  const modOf = (pkgPath) => {
    let best = null;
    for (const m of modules) if ((pkgPath === m.name || pkgPath.startsWith(m.name + "/")) && (!best || m.name.length > best.name.length)) best = m;
    return best;
  };

  // Packages of each module, grouped by directory.
  const internalPkgs = N.filter((n) => n.type === "package" && !n.ext);
  const byModule = new Map();
  internalPkgs.forEach((p) => {
    const m = modOf(p.name);
    const key = m ? m.i : ROOT;
    if (!byModule.has(key)) byModule.set(key, []);
    byModule.get(key).push(p);
  });
  byModule.forEach((pkgs, mi) => {
    const prefix = mi === ROOT ? "" : N[mi].name;
    // trie of directories
    const trie = { name: "", kids: new Map(), pkg: null };
    pkgs.forEach((p) => {
      const rel = prefix && p.name.startsWith(prefix) ? p.name.slice(prefix.length).replace(/^\//, "") : p.name;
      let node = trie;
      if (rel) for (const seg of rel.split("/")) {
        if (!node.kids.has(seg)) node.kids.set(seg, { name: seg, kids: new Map(), pkg: null });
        node = node.kids.get(seg);
      }
      node.pkg = p;
      T[p.i].label = rel ? GV.baseName(rel) : (GV.baseName(p.name) || p.name);
    });
    const count = (d) => (d.pkg ? 1 : 0) + Array.from(d.kids.values()).reduce((s, k) => s + count(k), 0);
    // place(d, parent, path): a directory with one package and nothing else
    // is that package; a directory chain with a single child is merged.
    function place(d, parent, path) {
      const kids = Array.from(d.kids.values()).sort((a, b) => a.name < b.name ? -1 : 1);
      if (!kids.length) { if (d.pkg) setParent(d.pkg.i, parent); return; }
      if (!d.pkg && kids.length === 1) { place(kids[0], parent, path + kids[0].name + "/"); return; }
      const total = count(d);
      if (total === 1 && d.pkg) { setParent(d.pkg.i, parent); return; }
      const g = synth("dir:" + (prefix || "") + ":" + path, "dir", path, path);
      setParent(g, parent);
      if (d.pkg) setParent(d.pkg.i, g);
      kids.forEach((k) => place(k, g, k.name + "/"));
    }
    // The module root: its own package sits directly in the module.
    const top = mi === ROOT ? ROOT : mi;
    if (trie.pkg) setParent(trie.pkg.i, top);
    Array.from(trie.kids.values()).sort((a, b) => a.name < b.name ? -1 : 1).forEach((k) => place(k, top, k.name + "/"));
  });

  // External packages: standard library and third party.
  const extPkgs = N.filter((n) => n.type === "package" && n.ext);
  let EXT = -1;
  if (extPkgs.length) {
    EXT = synth("ext:", "extgroup", "External dependencies");
    setParent(EXT, ROOT);
    const sub = (id, name, pkgs) => {
      if (!pkgs.length) return;
      const g = synth(id, "dir", name);
      T[g].ext = true;
      setParent(g, EXT);
      pkgs.forEach((p) => setParent(p.i, g));
    };
    sub("ext:std", "Standard library", extPkgs.filter((p) => p.meta.std));
    sub("ext:third", "Third-party", extPkgs.filter((p) => !p.meta.std));
  }

  // Declarations: the structural parent of the dataset, refined.
  const sameFile = (a, b) => a.file && a.file === b.file;
  N.forEach((n) => {
    if (n.type === "module" || n.type === "package") return;
    let p = n.parent;
    if (n.type === "method") {
      for (const ei of n.inn) {
        const e = E[ei];
        if (e.type === "method" && sameFile(N[e.s], n)) { p = e.s; break; }
      }
    } else if (n.type === "db" || n.type === "unresolved") {
      const callers = new Set();
      for (const ei of n.inn) {
        const e = E[ei];
        if (FLOW[e.type] && FUNC[N[e.s].type] && sameFile(N[e.s], n)) callers.add(e.s);
      }
      if (callers.size === 1) p = callers.values().next().value;
      // a call site without a position sits under its only caller
      if (!n.file && p < 0) {
        const any = new Set(n.inn.filter((ei) => FLOW[E[ei].type]).map((ei) => E[ei].s));
        if (any.size === 1) p = any.values().next().value;
      }
    }
    if (p < 0 && n.ext && n.group >= 0) p = n.group;
    setParent(n.i, p >= 0 ? p : ROOT);
  });
  // Anything left without a parent (a package outside every module and
  // directory) hangs from the repository.
  T.forEach((t) => { if (t.i !== ROOT && t.parent < 0) setParent(t.i, ROOT); });
  T.forEach((t) => { if (t.parent >= 0) T[t.parent].kids.push(t.i); });
  // Inside its type a method reads M, inside its function a closure func1.
  T.forEach((t) => {
    if (!t.n || t.parent < 0) return;
    const p = T[t.parent];
    if (t.kind === "method" && TYPE[p.kind]) t.short = t.name.replace(/^\(.*?\)\./, "");
    else if (t.kind === "closure" && p.n && t.name.startsWith(p.name + ".")) t.short = t.name.slice(p.name.length + 1);
  });

  // Order: directories and containers first, then source order inside a
  // file, names otherwise.
  const ord = (t) => (KIND[t.kind] || { order: 50 }).order;
  T.forEach((t) => {
    t.kids.sort((a, b) => {
      const x = T[a], y = T[b];
      if (t.kind === "file" || FUNC[t.kind] || TYPE[t.kind]) {
        const lx = x.n ? x.n.line || 0 : 0, ly = y.n ? y.n.line || 0 : 0;
        if (lx !== ly) return lx - ly;
      }
      return ord(x) - ord(y) || (x.label.toLowerCase() < y.label.toLowerCase() ? -1 : x.label.toLowerCase() > y.label.toLowerCase() ? 1 : a - b);
    });
  });
  // Depth (iterative: closures can nest deeply) and counts, bottom-up.
  const order = [];
  (function walk() {
    const stack = [ROOT];
    while (stack.length) {
      const i = stack.pop();
      order.push(i);
      for (const c of T[i].kids) { T[c].depth = T[i].depth + 1; stack.push(c); }
    }
  })();
  for (let k = order.length - 1; k >= 0; k--) {
    const t = T[order[k]];
    if (t.n && SYMBOL[t.kind]) t.count += 1;
    if (t.kind === "file") t.nfiles += 1;
    if (t.kind === "package") t.npkgs += 1;
    if (t.parent >= 0) { const p = T[t.parent]; p.count += t.count; p.nfiles += t.nfiles; p.npkgs += t.npkgs; }
  }
  function ancestors(i) { const out = []; for (let p = T[i].parent; p >= 0; p = T[p].parent) out.push(p); return out; }
  function isAncestor(a, b) { // a is a strict ancestor of b
    if (T[a].depth >= T[b].depth) return false;
    for (let p = T[b].parent; p >= 0; p = T[p].parent) { if (p === a) return true; if (T[p].depth <= T[a].depth) return false; }
    return false;
  }
  // childOf(c, x): the child of container c on the way down to x.
  function childOf(c, x) {
    let y = x;
    while (y >= 0 && T[y].parent !== c) y = T[y].parent;
    return y;
  }
  function path(i) { return ancestors(i).reverse().concat([i]); }
  // packageOf: the package tree item a node belongs to (itself for packages).
  function packageOf(i) {
    for (let p = i; p >= 0; p = T[p].parent) if (T[p].kind === "package") return p;
    return -1;
  }
  function fileOf(i) {
    for (let p = i; p >= 0; p = T[p].parent) if (T[p].kind === "file") return p;
    return -1;
  }
  function descendants(i, fn) {
    const stack = T[i].kids.slice();
    while (stack.length) { const c = stack.pop(); fn(c); for (const k of T[c].kids) stack.push(k); }
  }

  // ---------- labels ----------
  function kindLabel(t) { return (KIND[t.kind] || { label: t.kind }).label; }
  function kindColor(t) {
    if (t.kind === "dir" && t.ext) return "k-external";
    if (t.n && t.n.ext && t.kind === "package") return "k-external";
    return (KIND[t.kind] || { color: "k-group" }).color;
  }
  // subtitle: one short line describing an item.
  function subtitle(t) {
    const k = t.kind;
    if (k === "repo") return GV.plural(modules.length, "module") + " · " + GV.plural(raw.stats.packages, "package");
    if (k === "module") return "module · " + GV.plural(t.npkgs, "package") + " · " + GV.plural(t.count, "symbol");
    if (k === "dir") return (t.ext ? "" : "directory · ") + GV.plural(t.npkgs, "package") + (t.ext ? "" : " · " + GV.plural(t.count, "symbol"));
    if (k === "extgroup") return GV.plural(t.npkgs, "package");
    if (k === "package") return t.n.ext ? (t.n.meta.std ? "standard library" : "third-party") + " · " + GV.plural(t.count, "function") + " used" : "package · " + GV.plural(t.nfiles, "file") + " · " + GV.plural(t.count, "symbol");
    if (k === "file") return "file · " + GV.plural(t.count, "symbol");
    const n = t.n;
    if (k === "route") return "route · " + (n.meta.router || "") + (n.line ? " · line " + n.line : "");
    if (k === "db") return (n.meta.lib || "db") + " " + (n.meta.op || "") + (n.line ? " · line " + n.line : "");
    if (k === "external") return "external · " + (n.pkg || "");
    if (k === "unresolved") return "unresolved" + (n.line ? " · line " + n.line : "");
    return kindLabel(t) + (n && n.line ? " · line " + n.line : "");
  }
  // reference: the fully qualified Go name of an item.
  function reference(t) {
    const n = t.n;
    if (!n) return t.kind === "dir" ? t.name : t.id;
    if (n.type === "package" || n.type === "module") return n.name;
    if (n.type === "file") return n.file;
    if (n.type === "route") return (n.meta.method || "ANY") + " " + (n.meta.path || n.name);
    if (n.type === "db" || n.type === "unresolved") return GV.loc(n);
    if (n.pkg && n.qual) {
      const i = n.qual.indexOf(".");
      return i >= 0 ? n.pkg + n.qual.slice(i) : n.pkg + "." + n.qual;
    }
    return n.qual || n.name;
  }
  // displayName: nested is true where the parent is visible around the
  // item (tree, expanded boxes), so the receiver or function can be omitted.
  function displayName(t, nested) {
    if (t.kind === "package") return t.n.ext ? t.n.name : t.label;
    return nested && t.short ? t.short : t.label;
  }

  // ---------- HTTP catalog ----------
  const HTTP = raw.http || null;
  const ROUTES = HTTP ? HTTP.routes || [] : [];
  const DEFS = HTTP ? HTTP.schemas || {} : {};
  const routeOfNode = new Map(); // node index → route index
  const nodeOfRoute = ROUTES.map((r, k) => {
    const i = byId.has(r.id) ? byId.get(r.id) : -1;
    if (i >= 0) routeOfNode.set(i, k);
    return i;
  });

  GV.data = {
    raw, N, E, T, ROOT, EXT, byId, COMM, KIND, REL, CONF_RANK, CONFS, FUNC, TYPE, SYMBOL, FLOW, STRUCTURAL,
    HTTP, ROUTES, DEFS, routeOfNode, nodeOfRoute, modules, order,
    ancestors, isAncestor, childOf, path, packageOf, fileOf, descendants,
    kindLabel, kindColor, subtitle, reference, displayName
  };
})(GV);
