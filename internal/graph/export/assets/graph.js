// GitView Repository Graph. Reads the dataset from the inert #graph-data
// block and only navigates it: no analysis runs in the browser and nothing is
// fetched. Three scales share the same data and positions:
//   Overview (packages, files, functions, types or everything)
//   Module Explorer (one package: its files, types and functions)
//   Symbol Explorer (one symbol: callers and callees, layered by depth)
// Repository strings are inserted with textContent, never as markup.
(function () {
  "use strict";

  var raw = JSON.parse(document.getElementById("graph-data").textContent);
  var K = raw.kinds, ET = raw.etypes, CF = raw.confs, ES = raw.esrc;

  // ---------- data ----------
  var N = raw.nodes.map(function (a, i) {
    return { i: i, id: a[0], type: K[a[1]], name: a[2], qual: a[3], pkg: a[4], file: a[5], line: a[6], end: a[7],
      sig: a[8], ext: !!a[9], parent: a[10], group: a[11], comm: a[12], deg: a[13], x: a[14], y: a[15],
      meta: a[16] || {}, out: [], inn: [], members: [] };
  });
  var E = raw.edges.map(function (a, i) {
    var e = { i: i, s: a[0], t: a[1], type: ET[a[2]], conf: CF[a[3]], w: a[4] || 1, file: a[5], line: a[6],
      note: a[7], meta: a[8] || {}, src: ES[a[9]] };
    N[e.s].out.push(i);
    N[e.t].inn.push(i);
    return e;
  });
  N.forEach(function (n) { if (n.group >= 0 && n.group !== n.i) N[n.group].members.push(n.i); });
  var COMM = {};
  raw.communities.forEach(function (c) { COMM[c[0]] = { id: c[0], level: c[1], label: c[2], size: c[3], members: c[4] }; });
  var fileIdx = {};
  N.forEach(function (n) { if (n.type === "file") fileIdx[n.file] = n.i; });

  var FUNC = { func: 1, method: 1, closure: 1 };
  var TYPE = { struct: 1, interface: 1, type: 1 };
  var FLOW_EDGES = { calls: 1, go: 1, defer: 1, handles: 1, queries: 1 };
  var TYPE_EDGES = { implements: 1, embeds: 1, references: 1, method: 1 };
  var CONF_RANK = { confirmed: 4, likely: 3, possible: 2, unresolved: 1 };
  var KIND_ORDER = ["package", "file", "struct", "interface", "type", "func", "method", "closure", "route", "db", "external", "unresolved", "module"];
  var EDGE_ORDER = ["imports", "calls", "go", "defer", "handles", "queries", "implements", "embeds", "references", "method", "contains"];
  var KIND_LABEL = { package: "package", file: "file", struct: "struct", interface: "interface", type: "named type", func: "function",
    method: "method", closure: "closure", route: "HTTP route", db: "database op", external: "external function", unresolved: "unresolved", module: "module" };

  // Distinct hues only: grey means "no community" (external, unresolved).
  var PALETTE = ["#6ea8fe", "#f2a65a", "#e8696b", "#5fc7b4", "#7bc96f", "#e9cf5f", "#c39bd3", "#f39fbe", "#c49a6c", "#5ad1e6",
    "#9d8cff", "#ffb071", "#ff8a80", "#3fbf8f", "#b6e36b", "#ffd86b", "#e08ce6", "#ff9ecb", "#e6a15c", "#7fb3ff"];
  var EDGE_COLOR = { imports: "#6f7f99", calls: "#7f8fab", go: "#5fc7b4", defer: "#5fa8a0", handles: "#7bc96f", queries: "#e9cf5f",
    implements: "#c39bd3", embeds: "#b08a68", references: "#5b7186", method: "#58657a", contains: "#2f3747", depends: "#7f8fab", agg: "#6f7f99" };
  var NONE = "#566072", EXT = "#465062", DIM = "#1c2230", DIM_EDGE = "rgba(60,70,90,0.12)";

  function commColor(c) { return c >= 0 ? PALETTE[c % PALETTE.length] : NONE; }
  function nodeColor(n) { return n.ext ? EXT : n.type === "unresolved" ? "#e8a33d" : commColor(n.comm); }
  function baseName(p) { var i = p.lastIndexOf("/"); return i >= 0 ? p.slice(i + 1) : p; }
  function label(n) {
    if (n.type === "package") return n.ext ? n.name : baseName(n.name);
    return n.name;
  }
  function display(n) {
    if (n.type === "package") return n.ext ? n.name : (n.file && n.file !== "." ? n.file : baseName(n.name));
    return n.name;
  }
  function loc(n) { return n.file ? n.file + (n.line ? ":" + n.line : "") : ""; }

  // ---------- state ----------
  var init = raw.initial || {};
  var S = {
    scale: "repo", level: init.level && init.level !== "full" ? init.level : (init.level === "full" ? "full" : "packages"),
    pkg: -1, file: -1, sym: -1, sel: -1, hover: -1, comm: null, path: null, pathFrom: -1,
    up: 2, down: 2, focus: false, focusDepth: 1,
    kinds: {}, edges: {}, hideExt: false, closures: false, labels: true, arrows: true, isolatedOnly: false,
    hiddenComms: {}, maxNodes: init.maxNodes || 5000, maxEdges: init.maxEdges || 25000
  };
  KIND_ORDER.forEach(function (k) { S.kinds[k] = k !== "module"; });
  EDGE_ORDER.forEach(function (k) { S.edges[k] = k !== "contains"; });

  // ---------- DOM helpers ----------
  function h(tag, attrs, kids) {
    var el = document.createElement(tag);
    if (attrs) for (var k in attrs) {
      if (k === "text") el.textContent = attrs[k];
      else if (k === "on") for (var ev in attrs.on) el.addEventListener(ev, attrs.on[ev]);
      else if (k === "class") el.className = attrs[k];
      else if (attrs[k] !== null && attrs[k] !== undefined && attrs[k] !== false) el.setAttribute(k, attrs[k] === true ? "" : attrs[k]);
    }
    (kids || []).forEach(function (c) { if (c) el.appendChild(typeof c === "string" ? document.createTextNode(c) : c); });
    return el;
  }
  function $(id) { return document.getElementById(id); }
  function clear(el) { while (el.firstChild) el.removeChild(el.firstChild); }
  function fmt(n) { return n.toLocaleString("en-US"); }

  // ---------- renderer ----------
  var graph = new graphology.MultiDirectedGraph();
  var renderer = null, webgl = true;
  var nodeBorder = null;
  try {
    nodeBorder = Sigma.rendering.createNodeBorderProgram({
      borders: [{ size: { value: 0.22 }, color: { attribute: "ring" } }, { size: { fill: true }, color: { attribute: "color" } }]
    });
  } catch (e) { nodeBorder = null; }

  function drawHover(ctx, d, settings) {
    var size = settings.labelSize, font = settings.labelFont, weight = settings.labelWeight;
    ctx.font = weight + " " + size + "px " + font;
    var text = d.label || "";
    var w = ctx.measureText(text).width + 10, hh = size + 8;
    ctx.fillStyle = "rgba(18,23,34,0.95)";
    ctx.strokeStyle = "#3a4760";
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.rect(d.x + d.size + 3, d.y - hh / 2, w, hh);
    ctx.fill();
    ctx.stroke();
    ctx.beginPath();
    ctx.arc(d.x, d.y, d.size + 2, 0, Math.PI * 2);
    ctx.strokeStyle = "#e6ecf5";
    ctx.stroke();
    ctx.fillStyle = "#e6ecf5";
    ctx.fillText(text, d.x + d.size + 8, d.y + size / 3);
  }

  try {
    var programs = {};
    if (nodeBorder) programs.ring = nodeBorder;
    renderer = new Sigma(graph, $("graph"), {
      allowInvalidContainer: true,
      defaultEdgeType: "line",
      edgeProgramClasses: { arrow: Sigma.rendering.EdgeArrowProgram, line: Sigma.rendering.EdgeLineProgram || Sigma.rendering.EdgeRectangleProgram },
      nodeProgramClasses: programs,
      labelColor: { color: "#c7cedb" },
      labelFont: "system-ui, -apple-system, Segoe UI, sans-serif",
      labelSize: 12,
      labelDensity: 0.6,
      labelGridCellSize: 70,
      labelRenderedSizeThreshold: 7,
      zIndex: true,
      minCameraRatio: 0.005,
      maxCameraRatio: 8,
      hideEdgesOnMove: false,
      defaultDrawNodeHover: drawHover,
      nodeReducer: nodeReducer,
      edgeReducer: edgeReducer
    });
  } catch (e) {
    webgl = false;
    renderer = null;
    $("graph").appendChild(h("div", { class: "fallback", text: "This browser could not start WebGL, so the graph cannot be drawn. Search, the lists in the side panel and node details still work." }));
  }

  // ---------- highlight ----------
  var HL = null; // Set of node keys to highlight, or null
  var HLE = null; // Set of edge keys
  var focusKey = null;

  function computeHighlight() {
    HL = null; HLE = null;
    if (S.path) {
      HL = new Set(S.path.nodes); HLE = new Set(S.path.edges);
      return;
    }
    if (S.comm !== null) {
      HL = new Set();
      graph.forEachNode(function (k, a) { if (a.comm === S.comm) HL.add(k); });
      HLE = new Set();
      graph.forEachEdge(function (k, a, s, t) { if (HL.has(s) || HL.has(t)) HLE.add(k); });
      return;
    }
    var c = S.hover >= 0 && graph.hasNode(String(S.hover)) ? String(S.hover) : (S.sel >= 0 && graph.hasNode(String(S.sel)) ? String(S.sel) : null);
    if (!c) return;
    // In the Symbol Explorer every drawn node already relates to the root.
    if (S.scale === "symbol" && c === String(S.sym)) return;
    HL = new Set([c]); HLE = new Set();
    graph.forEachEdge(c, function (k, a, s, t) { HLE.add(k); HL.add(s); HL.add(t); });
  }
  function nodeReducer(k, a) {
    var r = Object.assign({}, a);
    if (!S.labels && !a.forceLabel) r.label = "";
    if (HL) {
      if (HL.has(k)) { r.forceLabel = true; r.zIndex = 2; if (a.extLabel) r.label = a.extLabel; }
      else { r.color = DIM; r.ring = DIM; r.label = ""; r.zIndex = 0; }
    }
    if (String(S.sel) === k || String(S.hover) === k) { r.highlighted = String(S.sel) === k; r.forceLabel = true; r.zIndex = 3; if (a.extLabel) r.label = a.extLabel; }
    if (S.pathFrom >= 0 && String(S.pathFrom) === k) { r.highlighted = true; r.forceLabel = true; }
    return r;
  }
  function edgeReducer(k, a) {
    var r = Object.assign({}, a);
    if (HLE) {
      if (HLE.has(k)) { r.zIndex = 2; r.size = Math.max(a.size * 1.8, 1.2); r.color = a.hl || a.color; }
      else { r.color = DIM_EDGE; r.zIndex = 0; }
    }
    return r;
  }
  function refresh() {
    computeHighlight();
    if (renderer) renderer.refresh({ skipIndexation: true });
  }

  // ---------- view construction ----------
  function kindOk(n) {
    if (n.type === "closure" && !S.closures) return false;
    if (n.ext && S.hideExt) return false;
    if (!S.kinds[n.type]) return false;
    if (n.comm >= 0 && S.hiddenComms[n.comm]) return false;
    return true;
  }
  function edgeOk(e) { return S.edges[e.type] !== false; }

  function nodeSize(n, scale) {
    var base = { package: 5, file: 3, struct: 3.2, interface: 3.4, type: 3, func: 2.4, method: 2.4, closure: 1.7, route: 3.4, db: 3, external: 1.9, unresolved: 2, module: 6 }[n.type] || 2.5;
    if (n.ext && n.type === "package") return Math.min(2.2 + Math.sqrt(n.deg || 0) * 0.5, 7);
    var extra = Math.sqrt(n.deg || 0) * (n.type === "package" ? 1.6 : 0.9);
    if (n.type === "package" && scale === "repo") extra += Math.sqrt(n.members.length) * 0.35;
    return Math.min(base + extra, n.type === "package" ? 26 : 16);
  }
  function edgeColor(e) {
    if (e.conf === "unresolved") return "rgba(232,163,61,0.75)";
    var c = EDGE_COLOR[e.type] || EDGE_COLOR.calls;
    var alpha = e.conf === "possible" ? 0.35 : e.conf === "likely" ? 0.6 : 0.55;
    return hexA(c, alpha);
  }
  function hexA(hex, a) {
    var n = parseInt(hex.slice(1), 16);
    return "rgba(" + (n >> 16 & 255) + "," + (n >> 8 & 255) + "," + (n & 255) + "," + a + ")";
  }

  var view = null; // {nodes:[idx], pos:{idx:{x,y}}, edges:[{key,s,t,type,conf,w,real:[edge idx]}], note}

  function addNodeTo(v, i, x, y, extra) {
    if (v.has[i]) return;
    v.has[i] = true;
    v.nodes.push({ i: i, x: x, y: y, extra: extra || {} });
  }
  function addEdgeTo(v, key, s, t, type, conf, w, real) {
    var e = v.emap[key];
    if (e) {
      e.w += w;
      if (CONF_RANK[conf] > CONF_RANK[e.conf]) e.conf = conf;
      if (real !== undefined) e.real.push(real);
      return;
    }
    e = { key: key, s: s, t: t, type: type, conf: conf, w: w, real: real !== undefined ? [real] : [] };
    v.emap[key] = e;
    v.edges.push(e);
  }
  function newView() { return { nodes: [], has: {}, edges: [], emap: {}, notes: [] }; }

  function overview() {
    var v = newView();
    var lvl = S.level;
    var pick;
    if (lvl === "packages") pick = function (n) { return n.type === "package"; };
    else if (lvl === "files") pick = function (n) { return n.type === "file"; };
    else if (lvl === "functions") pick = function (n) { return FUNC[n.type] || n.type === "route" || n.type === "db" || n.type === "external" || n.type === "unresolved"; };
    else if (lvl === "types") pick = function (n) { return !!TYPE[n.type]; };
    else pick = function (n) { return n.type !== "module"; };
    var cand = N.filter(function (n) { return pick(n) && kindOk(n); });
    cand = limitNodes(v, cand);
    cand.forEach(function (n) { addNodeTo(v, n.i, n.x, n.y); });

    if (lvl === "files") {
      // File → file relations derived from the relations of their declarations.
      E.forEach(function (e) {
        if (e.type === "contains" || e.type === "imports" || !edgeOk(e)) return;
        var a = N[e.s], b = N[e.t];
        if (!a.file || !b.file || a.file === b.file) return;
        var fa = fileIdx[a.file], fb = fileIdx[b.file];
        if (fa === undefined || fb === undefined || !v.has[fa] || !v.has[fb]) return;
        addEdgeTo(v, "d|" + fa + "|" + fb, fa, fb, "depends", e.conf, e.w, e.i);
      });
      v.notes.push("File relations are derived: each line summarises the calls and type relations between declarations of two files.");
    } else {
      E.forEach(function (e) {
        if (!edgeOk(e) || !v.has[e.s] || !v.has[e.t]) return;
        if (lvl === "packages" && e.type !== "imports" && e.type !== "contains") return;
        if (lvl === "functions" && !FLOW_EDGES[e.type]) return;
        if (lvl === "types" && !(e.type === "implements" || e.type === "embeds" || e.type === "references")) return;
        addEdgeTo(v, "e" + e.i, e.s, e.t, e.type, e.conf, e.w, e.i);
      });
    }
    return v;
  }

  function limitNodes(v, cand) {
    if (S.isolatedOnly) cand = cand.filter(function (n) { return n.deg === 0; });
    if (cand.length <= S.maxNodes) return cand;
    var total = cand.length;
    cand = cand.slice().sort(function (a, b) { return b.deg - a.deg || (a.id < b.id ? -1 : 1); }).slice(0, S.maxNodes);
    v.notes.push("Showing the " + fmt(S.maxNodes) + " most connected of " + fmt(total) + " nodes. Raise the limit in Filters, or open a package.");
    v.warn = true;
    return cand;
  }

  function moduleView() {
    var v = newView();
    var P = N[S.pkg];
    var mem = P.members.map(function (i) { return N[i]; }).filter(function (n) {
      if (!kindOk(n)) return false;
      if (S.file >= 0) return n.i === S.file || n.file === N[S.file].file;
      return true;
    });
    mem = limitNodes(v, mem);
    addNodeTo(v, P.i, P.x, P.y, { anchor: true });
    var maxR = 1;
    mem.forEach(function (n) {
      addNodeTo(v, n.i, n.x, n.y);
      maxR = Math.max(maxR, Math.hypot(n.x - P.x, n.y - P.y));
    });
    // Relations inside the package, and aggregated relations with the
    // packages around it.
    var ctx = {};
    mem.forEach(function (n) {
      n.out.concat(n.inn).forEach(function (ei) {
        var e = E[ei];
        if (!edgeOk(e)) return;
        if (e.type === "contains" && !S.edges.contains) return;
        var other = e.s === n.i ? e.t : e.s;
        if (v.has[other]) {
          if (other !== P.i) addEdgeTo(v, "e" + e.i, e.s, e.t, e.type, e.conf, e.w, e.i);
          return;
        }
        var o = N[other];
        var g = o.group >= 0 ? o.group : (o.type === "package" ? o.i : -1);
        if (g < 0 || g === P.i) return;
        var G = N[g];
        if (G.ext && S.hideExt) return;
        if (G.comm >= 0 && S.hiddenComms[G.comm]) return;
        ctx[g] = true;
        var out = e.s === n.i;
        addEdgeTo(v, "a|" + n.i + "|" + g + "|" + (out ? 1 : 0) + "|" + e.type, out ? n.i : g, out ? g : n.i, e.type, e.conf, e.w, e.i);
      });
    });
    if (S.edges.imports) {
      P.out.concat(P.inn).forEach(function (ei) {
        var e = E[ei];
        if (e.type !== "imports") return;
        var g = e.s === P.i ? e.t : e.s;
        if (N[g].ext && S.hideExt) return;
        ctx[g] = true;
        addEdgeTo(v, "e" + e.i, e.s, e.t, e.type, e.conf, e.w, e.i);
      });
    }
    // Context packages on a ring around the package, in the direction where
    // they are in the overview.
    var ring = Object.keys(ctx).map(Number).map(function (g) {
      return { g: g, a: Math.atan2(N[g].y - P.y, N[g].x - P.x) };
    }).sort(function (a, b) { return a.a - b.a || a.g - b.g; });
    var R = maxR * 1.45 + 30;
    var minGap = ring.length ? Math.min(2 * Math.PI / ring.length, 0.35) : 0;
    for (var i = 1; i < ring.length; i++) if (ring[i].a - ring[i - 1].a < minGap) ring[i].a = ring[i - 1].a + minGap;
    ring.forEach(function (r) { addNodeTo(v, r.g, P.x + R * Math.cos(r.a), P.y + R * Math.sin(r.a), { context: true }); });
    if (ring.length) v.notes.push(ring.length + " connected package(s) shown on the outer ring; relations with them are aggregated per symbol.");
    return v;
  }

  function relevantEdge(e, root) {
    if (!edgeOk(e)) return false;
    if (FUNC[root.type] || root.type === "route" || root.type === "db" || root.type === "external" || root.type === "unresolved") {
      return !!FLOW_EDGES[e.type] || (e.type === "references" && S.edges.references && S.symRefs);
    }
    if (TYPE[root.type]) return !!TYPE_EDGES[e.type];
    if (root.type === "package") return e.type === "imports";
    if (root.type === "file") return e.type === "contains";
    return e.type !== "contains";
  }

  function symbolView() {
    var v = newView();
    var root = N[S.sym];
    var LAYER = 60, DX = 240, DY = 34;
    var depth = {}; depth[root.i] = 0;
    var layers = { 0: [root.i] };
    var more = {};
    function walk(dir, maxD) {
      var frontier = [root.i];
      for (var d = 1; d <= maxD && frontier.length; d++) {
        var next = [], seen = {};
        frontier.forEach(function (i) {
          var list = dir > 0 ? N[i].out : N[i].inn;
          list.forEach(function (ei) {
            var e = E[ei];
            if (!relevantEdge(e, root)) return;
            var o = dir > 0 ? e.t : e.s;
            if (!kindOk(N[o]) && o !== root.i) return;
            if (depth[o] === undefined && !seen[o]) { seen[o] = true; next.push({ i: o, r: CONF_RANK[e.conf] }); }
          });
        });
        next.sort(function (a, b) { return b.r - a.r || (N[a.i].name < N[b.i].name ? -1 : 1); });
        var key = dir * d;
        if (next.length > LAYER) { more[key] = next.length - LAYER; next = next.slice(0, LAYER); }
        layers[key] = next.map(function (x) { depth[x.i] = key; return x.i; });
        frontier = layers[key];
      }
    }
    walk(-1, S.up);
    walk(1, S.down);
    Object.keys(layers).forEach(function (k) {
      var L = layers[k], d = Number(k);
      L.forEach(function (i, j) { addNodeTo(v, i, d * DX, (j - (L.length - 1) / 2) * DY, { layer: d }); });
    });
    // Every relevant edge between shown nodes (recursion and cycles included).
    v.nodes.forEach(function (x) {
      N[x.i].out.forEach(function (ei) {
        var e = E[ei];
        if (v.has[e.t] && relevantEdge(e, root)) addEdgeTo(v, "e" + e.i, e.s, e.t, e.type, e.conf, e.w, e.i);
      });
    });
    Object.keys(more).forEach(function (k) {
      v.notes.push((Number(k) < 0 ? "Callers" : "Callees") + " at depth " + Math.abs(k) + ": " + more[k] + " more not drawn (strongest relations first).");
      v.warn = true;
    });
    return v;
  }

  function buildView() {
    if (S.scale === "module" && S.pkg >= 0) return moduleView();
    if (S.scale === "symbol" && S.sym >= 0) return symbolView();
    return overview();
  }

  function focusFilter(v) {
    if (!S.focus || S.sel < 0 || !v.has[S.sel]) return v;
    var keep = {}; keep[S.sel] = true;
    var frontier = [S.sel];
    var adj = {};
    v.edges.forEach(function (e) { (adj[e.s] = adj[e.s] || []).push(e.t); (adj[e.t] = adj[e.t] || []).push(e.s); });
    for (var d = 0; d < S.focusDepth; d++) {
      var next = [];
      frontier.forEach(function (i) { (adj[i] || []).forEach(function (o) { if (!keep[o]) { keep[o] = true; next.push(o); } }); });
      frontier = next;
    }
    v.nodes = v.nodes.filter(function (x) { return keep[x.i]; });
    v.edges = v.edges.filter(function (e) { return keep[e.s] && keep[e.t]; });
    v.notes.push("Focused view: " + N[S.sel].name + " and its relations up to depth " + S.focusDepth + ".");
    return v;
  }

  function apply(fit) {
    view = focusFilter(buildView());
    graph.clear();
    var scale = S.scale;
    // Room for the labels of the outer columns of the Symbol Explorer.
    if (renderer) renderer.setSetting("stagePadding", scale === "symbol" ? 150 : 30);
    var useArrows = S.arrows && (scale !== "repo" || S.level === "packages" || view.edges.length < 4000);
    view.nodes.forEach(function (x) {
      var n = N[x.i];
      var color = nodeColor(n);
      var attrs = { x: x.x, y: -x.y, size: nodeSize(n, scale), label: label(n), color: color, comm: n.comm, kind: n.type };
      if (n.ext && n.type === "package" && !x.extra.context) { attrs.color = hexA(EXT, 0.8); attrs.ring = "#3a4558"; attrs.extLabel = attrs.label; attrs.label = ""; }
      if (x.extra.context) { attrs.color = hexA(n.ext ? EXT : commColor(n.comm), 0.55); attrs.size = Math.max(attrs.size * 0.8, 4); attrs.label = label(n); }
      if (x.extra.anchor) { attrs.size = Math.max(attrs.size, 9); attrs.forceLabel = true; }
      if (scale === "symbol" && n.i === S.sym) { attrs.size = 12; attrs.forceLabel = true; }
      if (scale === "symbol") attrs.forceLabel = view.nodes.length <= 80 || attrs.forceLabel;
      if (nodeBorder && (TYPE[n.type] || n.type === "route" || n.type === "db" || n.type === "package" || n.type === "file")) {
        attrs.type = "ring";
        attrs.ring = n.type === "interface" ? "#e6ecf5" : n.type === "route" ? "#7bc96f" : n.type === "db" ? "#e9cf5f" : n.type === "file" ? "#3a4558" : "#9aa6ba";
      }
      graph.addNode(String(x.i), attrs);
    });
    var edges = view.edges;
    if (edges.length > S.maxEdges) {
      var total = edges.length;
      edges = edges.slice().sort(function (a, b) { return CONF_RANK[b.conf] - CONF_RANK[a.conf] || b.w - a.w || (a.key < b.key ? -1 : 1); }).slice(0, S.maxEdges);
      view.notes.push("Showing " + fmt(S.maxEdges) + " of " + fmt(total) + " relations (strongest first).");
      view.warn = true;
    }
    view.drawn = edges;
    var layer = {};
    view.nodes.forEach(function (x) { if (x.extra.layer !== undefined) layer[x.i] = x.extra.layer; });
    edges.forEach(function (e) {
      var c = edgeColor(e);
      if (N[e.t].ext && N[e.t].type === "package" && e.type === "imports") c = hexA(EDGE_COLOR.imports, 0.22);
      if (e.key.charAt(0) === "a") c = hexA(EDGE_COLOR[e.type] || EDGE_COLOR.agg, 0.16); // aggregated, to a context package
      // Symbol Explorer: relations that skip a layer (or stay in one) are
      // drawn fainter than the caller → callee chain.
      if (scale === "symbol" && Math.abs((layer[e.t] || 0) - (layer[e.s] || 0)) !== 1 && e.s !== e.t) c = hexA(EDGE_COLOR[e.type] || EDGE_COLOR.calls, 0.2);
      graph.addEdgeWithKey(e.key, String(e.s), String(e.t), {
        type: useArrows && e.s !== e.t ? "arrow" : "line",
        size: Math.min(0.6 + Math.log2(1 + e.w) * 0.5, 3.5) * (scale === "symbol" ? 1.6 : 1),
        color: c, hl: hexA(EDGE_COLOR[e.type] || EDGE_COLOR.calls, 0.95), etype: e.type
      });
    });
    if (S.sel >= 0 && !graph.hasNode(String(S.sel))) S.sel = -1;
    if (S.pathFrom >= 0 && !graph.hasNode(String(S.pathFrom))) S.pathFrom = -1;
    S.path = null;
    refresh();
    if (renderer && fit !== false) renderer.getCamera().animatedReset({ duration: 250 });
    drawChrome();
  }

  // ---------- camera ----------
  function centerOn(keys, ratio) {
    if (!renderer || !keys.length) return;
    var minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    keys.forEach(function (k) {
      var d = renderer.getNodeDisplayData(k);
      if (!d) return;
      minX = Math.min(minX, d.x); maxX = Math.max(maxX, d.x); minY = Math.min(minY, d.y); maxY = Math.max(maxY, d.y);
    });
    if (minX === Infinity) return;
    var r = ratio || Math.max(Math.max(maxX - minX, maxY - minY) * 1.4, 0.06);
    renderer.getCamera().animate({ x: (minX + maxX) / 2, y: (minY + maxY) / 2, ratio: Math.min(r, 1.2) }, { duration: 400 });
  }
  function zoomTo(i) {
    var k = String(i);
    if (!graph.hasNode(k)) return;
    var keys = [k];
    graph.forEachNeighbor(k, function (o) { keys.push(o); });
    centerOn(keys.length > 1 ? keys : [k], keys.length > 1 ? null : 0.12);
  }

  // ---------- navigation ----------
  function openRepo(level) {
    S.scale = "repo"; S.pkg = -1; S.file = -1; S.sym = -1; S.comm = null;
    if (level) S.level = level;
    apply();
  }
  function openPackage(p, file) {
    if (p < 0) return;
    if (N[p].ext) { select(p, true); return; }
    S.scale = "module"; S.pkg = p; S.file = file >= 0 ? file : -1; S.sym = -1; S.comm = null;
    apply();
  }
  function openSymbol(i) {
    S.scale = "symbol"; S.sym = i; S.sel = i; S.comm = null;
    if (N[i].group >= 0 && !N[N[i].group].ext) S.pkg = N[i].group;
    apply();
  }
  // goTo shows a node wherever it lives: in the current view when it is
  // there, otherwise at the scale it belongs to.
  function goTo(i) {
    var n = N[i];
    if (!graph.hasNode(String(i))) {
      if (n.type === "package") {
        if (S.scale !== "repo" || S.level !== "packages") openRepo("packages");
      } else if (n.type === "file") {
        openPackage(n.group, -1);
      } else if (n.ext || n.type === "unresolved" || n.group < 0 || N[n.group].ext) {
        openSymbol(i);
      } else if (n.type === "closure" && !S.closures) {
        S.closures = true; openPackage(n.group, -1);
      } else {
        openPackage(n.group, -1);
      }
      if (!graph.hasNode(String(i))) {
        // Hidden by a filter: show it in the Symbol Explorer.
        openSymbol(i);
      }
    }
    select(i, true);
  }
  function select(i, zoom) {
    if (S.pathFrom >= 0 && i >= 0 && i !== S.pathFrom) {
      findPath(S.pathFrom, i);
      S.pathFrom = -1;
      S.sel = i;
      drawPanel();
      if (renderer) renderer.refresh({ skipIndexation: true });
      return;
    }
    S.sel = i; S.comm = null; S.path = null;
    refresh();
    drawPanel();
    if (zoom && i >= 0) zoomTo(i);
  }
  function up() {
    if (S.scale === "symbol") {
      if (S.pkg >= 0) openPackage(S.pkg, S.file); else openRepo();
    } else if (S.scale === "module") {
      if (S.file >= 0) openPackage(S.pkg, -1); else { var p = S.pkg; openRepo("packages"); select(p, true); }
    }
  }

  // ---------- path between two nodes ----------
  function findPath(a, b) {
    var adj = {}, radj = {};
    (view.drawn || []).forEach(function (e) {
      (adj[e.s] = adj[e.s] || []).push(e);
      (radj[e.t] = radj[e.t] || []).push(e);
    });
    function bfs(directed) {
      var prev = {}; prev[a] = null;
      var q = [a];
      while (q.length) {
        var x = q.shift();
        if (x === b) break;
        var nb = (adj[x] || []).map(function (e) { return [e.t, e]; });
        if (!directed) nb = nb.concat((radj[x] || []).map(function (e) { return [e.s, e]; }));
        nb.sort(function (p, q2) { return p[0] - q2[0]; });
        for (var j = 0; j < nb.length; j++) {
          var o = nb[j][0];
          if (!(o in prev)) { prev[o] = { from: x, e: nb[j][1] }; q.push(o); }
        }
      }
      if (!(b in prev)) return null;
      var nodes = [String(b)], edges = [];
      for (var c = b; prev[c]; c = prev[c].from) { edges.push(prev[c].e.key); nodes.push(String(prev[c].from)); }
      return { nodes: nodes.reverse(), edges: edges.reverse(), directed: directed };
    }
    var p = bfs(true) || bfs(false);
    S.path = p ? Object.assign(p, { a: a, b: b }) : { nodes: [String(a), String(b)], edges: [], none: true, a: a, b: b };
    computeHighlight();
  }

  // ---------- chrome: bar, breadcrumbs, legend ----------
  function drawChrome() {
    drawCrumbs();
    drawTools();
    drawLegend();
    var m = $("msg");
    m.textContent = view.notes.join("  ");
    m.className = "msg" + (view.warn ? " warn" : "");
    var comms = {};
    view.nodes.forEach(function (x) { if (N[x.i].comm >= 0) comms[N[x.i].comm] = 1; });
    $("counts").textContent = fmt(graph.order) + " / " + fmt(N.length) + " nodes · " + fmt(graph.size) + " / " + fmt(E.length) + " edges · " + Object.keys(comms).length + " communities";
    drawPanel();
  }

  function drawCrumbs() {
    var c = $("crumbs");
    clear(c);
    var items = [{ t: "Repository", f: function () { openRepo(); } }];
    if (S.scale !== "repo" && S.pkg >= 0) items.push({ t: label(N[S.pkg]), title: N[S.pkg].name, f: function () { openPackage(S.pkg, -1); } });
    if (S.scale !== "repo" && S.file >= 0) items.push({ t: N[S.file].name, title: N[S.file].file, f: function () { openPackage(S.pkg, S.file); } });
    if (S.scale === "symbol") items.push({ t: N[S.sym].name, title: N[S.sym].qual || N[S.sym].id, f: function () { } });
    items.forEach(function (it, j) {
      if (j) c.appendChild(h("span", { class: "sep", text: "›" }));
      var last = j === items.length - 1;
      c.appendChild(h("button", { text: it.t, title: it.title || it.t, "aria-current": last ? "page" : null, on: { click: last ? function () { } : it.f } }));
    });
  }

  function seg(options, current, onPick) {
    var el = h("div", { class: "seg", role: "group" });
    options.forEach(function (o) {
      el.appendChild(h("button", { text: o[1], title: o[2] || o[1], "aria-pressed": String(o[0] === current), disabled: o[3] ? true : null,
        on: { click: function () { onPick(o[0]); } } }));
    });
    return el;
  }

  var menuOpen = false;
  function drawTools() {
    var t = $("tools");
    clear(t);
    t.appendChild(seg([
      ["repo", "Overview", "Repository Overview: packages and their dependencies"],
      ["module", "Module", "Module Explorer: files, types and functions of a package", S.pkg < 0],
      ["symbol", "Symbol", "Symbol Explorer: callers and callees of a symbol", S.sym < 0]
    ], S.scale, function (s) {
      if (s === "repo") openRepo(); else if (s === "module") openPackage(S.pkg, S.file); else openSymbol(S.sym);
    }));
    if (S.scale === "repo") {
      var sel = h("select", { "aria-label": "Level", title: "What the Overview shows", on: { change: function () { openRepo(this.value); } } });
      [["packages", "Package graph"], ["files", "File graph"], ["functions", "Function graph"], ["types", "Type graph"], ["full", "Full repository"]].forEach(function (o) {
        sel.appendChild(h("option", { value: o[0], text: o[1], selected: o[0] === S.level ? true : null }));
      });
      t.appendChild(sel);
    }
    if (S.scale === "symbol") {
      t.appendChild(stepper("Callers", S.up, 0, 6, function (v) { S.up = v; apply(false); }));
      t.appendChild(stepper("Callees", S.down, 0, 6, function (v) { S.down = v; apply(false); }));
    }
    t.appendChild(h("button", { text: S.focus ? "Global view" : "Focus", title: "Show only the selected node and its relations (or everything again)", "aria-pressed": String(S.focus),
      disabled: !S.focus && S.sel < 0 ? true : null, on: { click: function () { S.focus = !S.focus; apply(); } } }));
    t.appendChild(filtersMenu());
    t.appendChild(h("button", { text: "Center", title: "Fit the graph in the window (f)", on: { click: function () { if (renderer) renderer.getCamera().animatedReset({ duration: 300 }); } } }));
  }

  function stepper(name, val, lo, hi, set) {
    return h("label", { title: name + " depth" }, [name,
      h("button", { text: "−", "aria-label": "Less " + name, disabled: val <= lo ? true : null, on: { click: function () { set(val - 1); } } }),
      h("span", { class: "count", text: String(val) }),
      h("button", { text: "+", "aria-label": "More " + name, disabled: val >= hi ? true : null, on: { click: function () { set(val + 1); } } })]);
  }

  function filtersMenu() {
    var d = h("details", { class: "menu", open: menuOpen ? true : null, on: { toggle: function () { menuOpen = this.open; } } });
    d.appendChild(h("summary", { text: "Filters" }));
    var pop = h("div", { class: "pop" });
    var present = {}, presentE = {};
    N.forEach(function (n) { present[n.type] = 1; });
    E.forEach(function (e) { presentE[e.type] = 1; });
    var kinds = h("div", null, [h("h4", { text: "Node types" })]);
    KIND_ORDER.forEach(function (k) {
      if (!present[k] || k === "module") return;
      kinds.appendChild(check(KIND_LABEL[k], S.kinds[k], function (v) { S.kinds[k] = v; apply(false); }));
    });
    var edges = h("div", null, [h("h4", { text: "Relations" })]);
    EDGE_ORDER.forEach(function (k) {
      if (!presentE[k]) return;
      var sw = h("span", { class: "swatch" });
      sw.style.background = EDGE_COLOR[k];
      var l = check(k, S.edges[k], function (v) { S.edges[k] = v; apply(false); });
      l.insertBefore(sw, l.childNodes[1]);
      edges.appendChild(l);
    });
    var opts = h("div", { class: "full row" }, [
      check("Hide external dependencies", S.hideExt, function (v) { S.hideExt = v; apply(false); }),
      check("Closures", S.closures, function (v) { S.closures = v; apply(false); }),
      check("Labels", S.labels, function (v) { S.labels = v; refresh(); }),
      check("Arrows", S.arrows, function (v) { S.arrows = v; apply(false); }),
      check("Only isolated nodes", S.isolatedOnly, function (v) { S.isolatedOnly = v; apply(false); }),
      check("Type references in Symbol Explorer", !!S.symRefs, function (v) { S.symRefs = v; apply(false); })
    ]);
    var lim = h("div", { class: "full row" }, [
      num("Max nodes", S.maxNodes, function (v) { S.maxNodes = v; apply(false); }),
      num("Max edges", S.maxEdges, function (v) { S.maxEdges = v; apply(false); }),
      num("Focus depth", S.focusDepth, function (v) { S.focusDepth = Math.max(1, Math.min(v, 6)); apply(false); })
    ]);
    pop.appendChild(kinds);
    pop.appendChild(edges);
    pop.appendChild(opts);
    pop.appendChild(lim);
    d.appendChild(pop);
    return d;
  }
  function check(text, val, set) {
    return h("label", null, [h("input", { type: "checkbox", checked: val ? true : null, on: { change: function () { set(this.checked); } } }), text]);
  }
  function num(text, val, set) {
    return h("label", null, [text, h("input", { type: "number", min: "1", value: String(val), on: { change: function () { var v = parseInt(this.value, 10); if (v > 0) set(v); } } })]);
  }

  function drawLegend() {
    var l = $("legend");
    clear(l);
    var types = {};
    graph.forEachEdge(function (k, a) { types[a.etype] = 1; });
    EDGE_ORDER.concat(["depends"]).forEach(function (k) {
      if (!types[k]) return;
      var sw = h("span", { class: "swatch" });
      sw.style.background = EDGE_COLOR[k];
      l.appendChild(h("span", null, [sw, k]));
    });
    if (graph.size) {
      var u = h("span", { class: "swatch" });
      u.style.background = "#e8a33d";
      l.appendChild(h("span", null, [u, "unresolved"]));
      var p = h("span", { class: "swatch" });
      p.style.background = "rgba(127,143,171,0.35)";
      l.appendChild(h("span", null, [p, "possible (faint)"]));
    }
  }

  // ---------- side panel ----------
  function drawPanel() {
    var p = $("panel");
    clear(p);
    if (S.path) drawPath(p);
    if (S.sel >= 0) drawNode(p, N[S.sel]);
    else drawSummary(p);
    drawCommunities(p);
    drawHubs(p);
  }

  function drawPath(p) {
    var path = S.path;
    p.appendChild(h("h3", null, ["Path", h("button", { text: "Clear", on: { click: function () { S.path = null; refresh(); drawPanel(); } } })]));
    if (path.none) {
      p.appendChild(h("p", { class: "note warn", text: "No path between " + N[path.a].name + " and " + N[path.b].name + " in the current view." }));
      return;
    }
    p.appendChild(h("p", { class: "note", text: (path.directed ? "Directed path" : "No directed path; shortest path ignoring direction") + ", " + path.edges.length + " step(s), in the current view:" }));
    var ul = h("ul", { class: "list" });
    path.nodes.forEach(function (k, j) {
      var n = N[Number(k)];
      var sub = "";
      if (j > 0) {
        var e = view.emap[path.edges[j - 1]];
        if (e) sub = e.type + " · " + e.conf;
      }
      ul.appendChild(item(n, sub));
    });
    p.appendChild(ul);
  }

  function item(n, sub, conf, onClick) {
    var dot = h("span", { class: "dot" });
    dot.style.background = nodeColor(n);
    var li = h("li", { tabindex: "0", title: n.qual || n.id, on: {
      click: onClick || function () { goTo(n.i); },
      keydown: function (ev) { if (ev.key === "Enter") (onClick || function () { goTo(n.i); })(); }
    } }, [dot, h("span", { class: "n", text: display(n) }), conf ? h("span", { class: "badge " + conf, text: conf }) : h("span", { class: "badge kind", text: KIND_LABEL[n.type] || n.type })]);
    if (sub) li.appendChild(h("span", { class: "sub", text: sub }));
    return li;
  }

  function drawSummary(p) {
    var st = raw.stats;
    if (S.scale === "module" && S.pkg >= 0) {
      drawNode(p, N[S.pkg]);
      return;
    }
    p.appendChild(h("h3", { text: "Repository" }));
    p.appendChild(h("div", { class: "title", text: raw.repository }));
    var g = h("div", { class: "stats" });
    [["Modules", st.modules], ["Packages", st.packages], ["External packages", st.externalPackages], ["Files", st.files],
      ["Functions and methods", st.functions], ["Types", st.types], ["Nodes", st.nodes], ["Edges", st.edges],
      ["Communities", st.communities], ["Package modularity", (st.modularity || 0).toFixed(3)],
      ["Analysis", st.analysisMs + " ms"], ["Graph build", st.buildMs + " ms"], ["Layout", st.layoutMs + " ms"]].forEach(function (r) {
      g.appendChild(h("span", { text: r[0] }));
      g.appendChild(h("span", { text: typeof r[1] === "number" ? fmt(r[1]) : String(r[1]) }));
    });
    p.appendChild(g);
    var conf = st.edgesByConfidence || {};
    p.appendChild(h("p", { class: "note", text: "Calls by confidence: " + ["confirmed", "likely", "possible", "unresolved"].map(function (c) { return (conf[c] || 0) + " " + c; }).join(" · ") }));
    if (raw.completeness && raw.completeness.status !== "complete") {
      var d = h("details", null, [h("summary", { class: "note warn", text: "Analysis is " + raw.completeness.status + " (" + (raw.completeness.reasons || []).length + " reason(s))" })]);
      var ul = h("ul");
      (raw.completeness.reasons || []).slice(0, 50).forEach(function (r) { ul.appendChild(h("li", { class: "note", text: r })); });
      d.appendChild(ul);
      p.appendChild(d);
    }
    var m = h("details", null, [h("summary", { class: "note", text: "How relations were computed" })]);
    var ml = h("ul");
    (raw.method || []).forEach(function (r) { ml.appendChild(h("li", { class: "note", text: r })); });
    m.appendChild(ml);
    p.appendChild(m);
    p.appendChild(h("p", { class: "note", text: "Click a node for details. Double-click a package to open it, a function or type to open the Symbol Explorer. Keys: / search · Esc clear · Backspace up · f center." }));
  }

  function rel(n, dir, pred) {
    var out = [];
    (dir > 0 ? n.out : n.inn).forEach(function (ei) {
      var e = E[ei];
      if (pred(e)) out.push(e);
    });
    out.sort(function (a, b) { return CONF_RANK[b.conf] - CONF_RANK[a.conf] || (N[dir > 0 ? a.t : a.s].name < N[dir > 0 ? b.t : b.s].name ? -1 : 1); });
    return out;
  }

  function relList(p, title, n, dir, pred, max) {
    var list = rel(n, dir, pred);
    if (!list.length) return;
    p.appendChild(h("h3", null, [title, h("small", { class: "count", text: String(list.length) })]));
    var ul = h("ul", { class: "list" });
    list.slice(0, max || 40).forEach(function (e) {
      var o = N[dir > 0 ? e.t : e.s];
      var sub = (e.type !== "calls" ? e.type + " · " : "") + (e.file ? e.file + ":" + e.line : "") + (e.w > 1 ? " · " + e.w + " sites" : "") + (e.note ? " · " + e.note : "");
      ul.appendChild(item(o, sub, FLOW_EDGES[e.type] ? e.conf : null));
    });
    if (list.length > (max || 40)) ul.appendChild(h("li", { class: "more", text: (list.length - (max || 40)) + " more" }));
    p.appendChild(ul);
  }

  function drawNode(p, n) {
    p.appendChild(h("div", { class: "title", text: n.type === "package" ? n.name : n.name }));
    var kv = h("dl", { class: "kv" });
    function row(k, v) { if (v === "" || v === undefined || v === null) return; kv.appendChild(h("dt", { text: k })); kv.appendChild(h("dd", { text: String(v) })); }
    row("Type", (KIND_LABEL[n.type] || n.type) + (n.ext ? " (external)" : ""));
    if (n.qual && n.qual !== n.name) row("Qualified", n.qual);
    if (n.type !== "package") row("Package", n.pkg);
    row("File", n.file ? n.file + (n.line ? ":" + n.line + (n.end && n.end !== n.line ? "–" + n.end : "") : "") : "");
    if (n.comm >= 0 && COMM[n.comm]) row("Community", COMM[n.comm].label + " (#" + n.comm + ", " + COMM[n.comm].level + " level)");
    row("Connections", n.deg + " (in " + n.inn.filter(nonStruct).length + ", out " + n.out.filter(nonStruct).length + ")");
    var m = n.meta;
    if (m.receiver) row("Receiver", m.receiver + (m.pointerReceiver ? " (pointer)" : " (value)"));
    if (m.role) row("Role", m.role + " (registered as a route handler)");
    if (m.method !== undefined && n.type === "route") row("Route", (m.method || "ANY") + " " + m.path + " · " + m.router + (m.partial ? " · partially resolved" : ""));
    if (n.type === "db") row("Operation", m.lib + " " + m.op + (m.sqlDynamic ? " · SQL built at runtime" : ""));
    if (m.exported !== undefined) row("Exported", m.exported ? "yes" : "no");
    if (m.std !== undefined) row("Standard library", m.std ? "yes" : "no");
    if (n.type === "package" && !n.ext) row("Members", n.members.length + " (" + summarizeKinds(n.members) + ")");
    if (m.note) row("Note", m.note);
    if (m.flowId) row("Code Flow ID", m.flowId);
    else row("ID", n.id);
    p.appendChild(kv);
    if (n.sig) p.appendChild(h("pre", { class: "sig", text: n.sig }));
    if (m.sql) p.appendChild(h("pre", { class: "sig", text: m.sql }));
    if (m.annotations && m.annotations.length) {
      p.appendChild(h("h3", { text: "Annotations" }));
      m.annotations.forEach(function (a) { p.appendChild(h("p", { class: "note", text: "[" + a.kind + (a.severity ? "/" + a.severity : "") + "] " + a.title })); });
    }

    var acts = h("div", { class: "actions" });
    if (n.type === "package" && !n.ext && !(S.scale === "module" && S.pkg === n.i && S.file < 0)) acts.appendChild(h("button", { text: "Open package", on: { click: function () { openPackage(n.i, -1); } } }));
    if (n.type === "file") acts.appendChild(h("button", { text: "Open file", on: { click: function () { openPackage(n.group, n.i); } } }));
    if (n.type !== "package" && n.type !== "file" && n.type !== "module" && !(S.scale === "symbol" && S.sym === n.i)) acts.appendChild(h("button", { text: "Symbol Explorer", on: { click: function () { openSymbol(n.i); } } }));
    if (n.group >= 0 && n.type !== "package" && !N[n.group].ext && !(S.scale === "module" && S.pkg === n.group)) acts.appendChild(h("button", { text: "Show in package", on: { click: function () { openPackage(n.group, -1); select(n.i, true); } } }));
    if (graph.hasNode(String(n.i))) {
      acts.appendChild(h("button", { text: S.pathFrom === n.i ? "Pick the target node…" : "Path from here", "aria-pressed": String(S.pathFrom === n.i), title: "Then click another node to highlight the shortest path",
        on: { click: function () { S.pathFrom = S.pathFrom === n.i ? -1 : n.i; drawPanel(); if (renderer) renderer.refresh({ skipIndexation: true }); } } }));
      acts.appendChild(h("button", { text: "Zoom", on: { click: function () { zoomTo(n.i); } } }));
    }
    p.appendChild(acts);

    if (FUNC[n.type] || n.type === "external" || n.type === "unresolved" || n.type === "route" || n.type === "db") {
      relList(p, "Callers", n, -1, function (e) { return !!FLOW_EDGES[e.type]; });
      relList(p, "Calls", n, 1, function (e) { return !!FLOW_EDGES[e.type]; });
      relList(p, "Types used", n, 1, function (e) { return e.type === "references"; });
      relList(p, "Method of", n, -1, function (e) { return e.type === "method"; });
      relList(p, "Closures", n, 1, function (e) { return e.type === "contains" && N[e.t].type === "closure"; });
    } else if (TYPE[n.type]) {
      relList(p, "Methods", n, 1, function (e) { return e.type === "method"; });
      relList(p, "Implements", n, 1, function (e) { return e.type === "implements"; });
      relList(p, "Implemented by", n, -1, function (e) { return e.type === "implements"; });
      relList(p, "Embeds", n, 1, function (e) { return e.type === "embeds"; });
      relList(p, "Embedded by", n, -1, function (e) { return e.type === "embeds"; });
      relList(p, "Uses types", n, 1, function (e) { return e.type === "references"; });
      relList(p, "Used by", n, -1, function (e) { return e.type === "references"; });
    } else if (n.type === "package") {
      relList(p, "Imports", n, 1, function (e) { return e.type === "imports"; }, 60);
      relList(p, "Imported by", n, -1, function (e) { return e.type === "imports"; }, 60);
      if (!n.ext) {
        var files = n.members.filter(function (i) { return N[i].type === "file"; }).map(function (i) { return N[i]; });
        if (files.length) {
          p.appendChild(h("h3", null, ["Files", h("small", { class: "count", text: String(files.length) })]));
          var ul = h("ul", { class: "list" });
          files.forEach(function (f) { ul.appendChild(item(f, "", null, function () { openPackage(n.i, f.i); })); });
          p.appendChild(ul);
        }
      } else {
        relList(p, "Functions used", n, 1, function (e) { return e.type === "contains"; }, 60);
      }
    } else if (n.type === "file") {
      relList(p, "Declarations", n, 1, function (e) { return e.type === "contains"; }, 80);
    }
  }
  function nonStruct(ei) { return E[ei].type !== "contains"; }
  function summarizeKinds(list) {
    var c = {};
    list.forEach(function (i) { c[N[i].type] = (c[N[i].type] || 0) + 1; });
    return KIND_ORDER.filter(function (k) { return c[k]; }).map(function (k) { return c[k] + " " + (KIND_LABEL[k] || k); }).join(", ");
  }

  function drawCommunities(p) {
    var counts = {};
    view.nodes.forEach(function (x) { var c = N[x.i].comm; if (c >= 0) counts[c] = (counts[c] || 0) + 1; });
    var ids = Object.keys(counts).map(Number).sort(function (a, b) { return counts[b] - counts[a] || a - b; });
    if (!ids.length) return;
    var hidden = Object.keys(S.hiddenComms).filter(function (k) { return S.hiddenComms[k]; }).length;
    p.appendChild(h("h3", null, ["Communities", h("small", { class: "count", text: ids.length + " in view" + (hidden ? " · " + hidden + " hidden" : "") })]));
    var all = h("label", { class: "note" }, [h("input", { type: "checkbox", checked: hidden ? null : true, on: { change: function () {
      var on = this.checked;
      Object.keys(COMM).forEach(function (k) { S.hiddenComms[k] = !on && counts[k] !== undefined; });
      if (on) S.hiddenComms = {};
      apply(false);
    } } }), " Select all"]);
    p.appendChild(all);
    var ul = h("ul", { class: "list comm" });
    ids.slice(0, 60).forEach(function (c) {
      var info = COMM[c] || { label: "#" + c };
      var dot = h("span", { class: "dot" });
      dot.style.background = commColor(c);
      var cb = h("input", { type: "checkbox", checked: S.hiddenComms[c] ? null : true, "aria-label": "Show community " + info.label,
        on: { click: function (ev) { ev.stopPropagation(); }, change: function () { S.hiddenComms[c] = !this.checked; apply(false); } } });
      var li = h("li", { tabindex: "0", title: "Highlight community " + info.label + " and its external connections", "aria-pressed": String(S.comm === c),
        on: { click: function () { S.comm = S.comm === c ? null : c; S.sel = -1; S.path = null; refresh(); drawPanel(); if (S.comm !== null) centerOn(Array.from(HL || [])); } } },
        [cb, dot, h("span", { class: "n", text: info.label }), h("span", { class: "count", text: String(counts[c]) })]);
      ul.appendChild(li);
    });
    if (ids.length > 60) ul.appendChild(h("li", { class: "more", text: (ids.length - 60) + " smaller communities" }));
    p.appendChild(ul);
    if (S.comm !== null) {
      var ext = 0, inside = 0;
      graph.forEachEdge(function (k, a, s, t, sa, ta) {
        var x = sa.comm === S.comm, y = ta.comm === S.comm;
        if (x && y) inside++; else if (x || y) ext++;
      });
      p.appendChild(h("p", { class: "note", text: "Community " + (COMM[S.comm] ? COMM[S.comm].label : S.comm) + ": " + inside + " internal and " + ext + " external relation(s) in view." }));
    }
  }

  function drawHubs(p) {
    if (!view.nodes.length) return;
    var deg = {};
    graph.forEachNode(function (k) { deg[k] = graph.degree(k); });
    var keys = Object.keys(deg).sort(function (a, b) { return deg[b] - deg[a] || Number(a) - Number(b); });
    var hubs = keys.filter(function (k) { return deg[k] > 0; }).slice(0, 10);
    if (hubs.length) {
      p.appendChild(h("h3", null, ["Most connected", h("small", { class: "count", text: "in view" })]));
      var ul = h("ul", { class: "list" });
      hubs.forEach(function (k) { ul.appendChild(item(N[Number(k)], deg[k] + " relation(s)" + (loc(N[Number(k)]) ? " · " + loc(N[Number(k)]) : ""))); });
      p.appendChild(ul);
    }
    var iso = keys.filter(function (k) { return deg[k] === 0; });
    if (iso.length) {
      p.appendChild(h("h3", null, ["Isolated in view", h("small", { class: "count", text: String(iso.length) })]));
      var ul2 = h("ul", { class: "list" });
      iso.slice(0, 15).forEach(function (k) { ul2.appendChild(item(N[Number(k)], loc(N[Number(k)]))); });
      if (iso.length > 15) ul2.appendChild(h("li", { class: "more", text: (iso.length - 15) + " more · Filters › Only isolated nodes" }));
      p.appendChild(ul2);
    }
    // Weakly connected components of the view.
    var comp = {}, ncomp = 0, sizes = [];
    graph.forEachNode(function (k) {
      if (comp[k] !== undefined) return;
      var q = [k], sz = 0;
      comp[k] = ncomp;
      while (q.length) {
        var x = q.pop(); sz++;
        graph.forEachNeighbor(x, function (o) { if (comp[o] === undefined) { comp[o] = ncomp; q.push(o); } });
      }
      sizes.push(sz); ncomp++;
    });
    if (ncomp > 1) p.appendChild(h("p", { class: "note", text: ncomp + " connected components in view (largest " + Math.max.apply(null, sizes) + " nodes, " + sizes.filter(function (s) { return s === 1; }).length + " isolated)." }));
  }

  // ---------- search ----------
  var q = $("q"), sug = $("suggest"), hits = [], cur = -1;
  var LOWER = N.map(function (n) { return [n.name.toLowerCase(), (n.qual || "").toLowerCase(), (n.file || "").toLowerCase(), n.type === "package" ? n.name.toLowerCase() : ""]; });
  function search(text) {
    text = text.trim().toLowerCase();
    if (!text) return [];
    var out = [];
    for (var i = 0; i < N.length; i++) {
      if (N[i].type === "module") continue;
      var L = LOWER[i], r = -1;
      if (L[0] === text) r = 0;
      else if (L[0].indexOf(text) === 0) r = 1;
      else if (L[0].indexOf(text) >= 0) r = 2;
      else if (L[1].indexOf(text) >= 0 || L[3].indexOf(text) >= 0) r = 3;
      else if (L[2].indexOf(text) >= 0) r = 4;
      if (r >= 0) out.push([r, i]);
    }
    out.sort(function (a, b) { return a[0] - b[0] || N[b[1]].deg - N[a[1]].deg || (N[a[1]].id < N[b[1]].id ? -1 : 1); });
    return out.slice(0, 40).map(function (x) { return x[1]; });
  }
  function drawSuggest() {
    clear(sug);
    if (!hits.length) {
      if (q.value.trim()) { sug.appendChild(h("li", { class: "s2", text: "No node matches." })); sug.hidden = false; }
      else sug.hidden = true;
      return;
    }
    hits.forEach(function (i, j) {
      var n = N[i];
      sug.appendChild(h("li", { role: "option", "aria-selected": String(j === cur), on: { mousedown: function (ev) { ev.preventDefault(); pick(i); } } },
        [h("span", { class: "s1", text: n.type === "package" ? n.name : (n.qual || n.name) }),
          h("span", { class: "s2", text: (KIND_LABEL[n.type] || n.type) + (n.ext ? " · external" : "") + (loc(n) ? " · " + loc(n) : (n.pkg && n.type !== "package" ? " · " + n.pkg : "")) })]));
    });
    sug.hidden = false;
  }
  function pick(i) {
    sug.hidden = true;
    q.blur();
    goTo(i);
  }
  q.addEventListener("input", function () { hits = search(q.value); cur = hits.length ? 0 : -1; drawSuggest(); });
  q.addEventListener("focus", function () { if (q.value.trim()) { hits = search(q.value); drawSuggest(); } });
  q.addEventListener("blur", function () { setTimeout(function () { sug.hidden = true; }, 120); });
  q.addEventListener("keydown", function (ev) {
    if (ev.key === "ArrowDown") { cur = Math.min(cur + 1, hits.length - 1); drawSuggest(); ev.preventDefault(); }
    else if (ev.key === "ArrowUp") { cur = Math.max(cur - 1, 0); drawSuggest(); ev.preventDefault(); }
    else if (ev.key === "Enter" && cur >= 0) { pick(hits[cur]); ev.preventDefault(); }
    else if (ev.key === "Escape") { q.value = ""; sug.hidden = true; q.blur(); }
  });

  // ---------- events ----------
  if (renderer) {
    renderer.on("clickNode", function (ev) { select(Number(ev.node), false); });
    renderer.on("doubleClickNode", function (ev) {
      ev.preventSigmaDefault();
      var n = N[Number(ev.node)];
      if (n.type === "package" && !n.ext) openPackage(n.i, -1);
      else if (n.type === "file") openPackage(n.group, n.i);
      else if (n.type !== "package") openSymbol(n.i);
    });
    renderer.on("enterNode", function (ev) { S.hover = Number(ev.node); refresh(); $("graph").style.cursor = "pointer"; });
    renderer.on("leaveNode", function () { S.hover = -1; refresh(); $("graph").style.cursor = ""; });
    renderer.on("clickStage", function () {
      if (S.sel >= 0 || S.comm !== null || S.path) { S.sel = -1; S.comm = null; S.path = null; S.pathFrom = -1; refresh(); drawPanel(); }
    });
    $("zin").addEventListener("click", function () { renderer.getCamera().animatedZoom({ duration: 200 }); });
    $("zout").addEventListener("click", function () { renderer.getCamera().animatedUnzoom({ duration: 200 }); });
    $("fit").addEventListener("click", function () { renderer.getCamera().animatedReset({ duration: 300 }); });
  }
  document.addEventListener("keydown", function (ev) {
    var tag = (ev.target.tagName || "").toLowerCase();
    if (tag === "input" || tag === "select" || tag === "textarea") return;
    if (ev.key === "/") { q.focus(); q.select(); ev.preventDefault(); }
    else if (ev.key === "Escape") { S.sel = -1; S.comm = null; S.path = null; S.pathFrom = -1; S.hover = -1; refresh(); drawPanel(); }
    else if (ev.key === "Backspace") { up(); ev.preventDefault(); }
    else if (ev.key === "f" && renderer) renderer.getCamera().animatedReset({ duration: 300 });
    else if ((ev.key === "+" || ev.key === "=") && renderer) renderer.getCamera().animatedZoom({ duration: 200 });
    else if (ev.key === "-" && renderer) renderer.getCamera().animatedUnzoom({ duration: 200 });
  });

  // ---------- deep links ----------
  // #level=functions, #package=<id>[&file=<path>], #symbol=<id>, #node=<id>
  var byId = {};
  N.forEach(function (n) { byId[n.id] = n.i; });
  var restoring = false;
  function writeHash() {
    if (restoring || !window.history || !history.replaceState) return;
    var parts = [];
    if (S.scale === "repo") parts.push("level=" + S.level);
    if (S.scale === "module" && S.pkg >= 0) { parts.push("package=" + encodeURIComponent(N[S.pkg].id)); if (S.file >= 0) parts.push("file=" + encodeURIComponent(N[S.file].file)); }
    if (S.scale === "symbol" && S.sym >= 0) parts.push("symbol=" + encodeURIComponent(N[S.sym].id));
    if (S.sel >= 0 && S.sel !== S.sym && S.sel !== S.pkg) parts.push("node=" + encodeURIComponent(N[S.sel].id));
    try { history.replaceState(null, "", "#" + parts.join("&")); } catch (e) { /* file:// in some browsers */ }
  }
  function readHash() {
    var p = {};
    (location.hash || "").replace(/^#/, "").split("&").forEach(function (kv) {
      var j = kv.indexOf("=");
      if (j > 0) { try { p[kv.slice(0, j)] = decodeURIComponent(kv.slice(j + 1)); } catch (e) { } }
    });
    return p;
  }
  function restore() {
    var p = readHash();
    restoring = true;
    try {
      if (p.level && ["packages", "files", "functions", "types", "full"].indexOf(p.level) >= 0) S.level = p.level;
      if (p.symbol !== undefined && byId[p.symbol] !== undefined) openSymbol(byId[p.symbol]);
      else if (p.package !== undefined && byId[p.package] !== undefined) {
        var f = p.file !== undefined && fileIdx[p.file] !== undefined ? fileIdx[p.file] : -1;
        openPackage(byId[p.package], f);
      } else apply();
      if (p.node !== undefined && byId[p.node] !== undefined) goTo(byId[p.node]);
    } finally { restoring = false; }
    writeHash();
  }
  var drawPanelInner = drawPanel;
  drawPanel = function () { drawPanelInner(); writeHash(); };
  window.addEventListener("hashchange", function () { if (!restoring) restore(); });

  // Expose a read-only hook for automated checks of the page.
  window.__gitviewGraph = { state: S, nodes: N, edges: E, graph: graph, renderer: renderer, goTo: goTo, openPackage: openPackage, openSymbol: openSymbol, openRepo: openRepo, select: select, findPath: findPath, view: function () { return view; } };

  restore();
})();
