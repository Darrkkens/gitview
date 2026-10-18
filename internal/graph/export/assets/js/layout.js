// Layout: ELK.js (vendored, run in the page's thread through its built-in
// fake worker) lays out one container at a time. Results are cached by the
// container's content, so expanding a package re-lays only that package
// and its ancestors, and coming back to a view costs nothing.
(function (GV) {
  "use strict";
  const elk = typeof ELK !== "undefined" ? new ELK() : null;
  const BIG = 260; // children beyond which a container is packed instead of layered
  const cache = new Map();
  const stats = { runs: 0, hits: 0, ms: 0, last: 0, failures: 0 };

  // ---------- text ----------
  const mctx = document.createElement("canvas").getContext("2d");
  const widths = new Map();
  let uiFont = null;
  function font(weight, size) {
    if (!uiFont) uiFont = GV.token("font-ui") || "sans-serif";
    return weight + " " + size + "px " + uiFont;
  }
  function textWidth(text, f) {
    const key = f + "\u0000" + text;
    let w = widths.get(key);
    if (w === undefined) {
      mctx.font = f;
      w = mctx.measureText(text).width;
      if (widths.size > 20000) widths.clear();
      widths.set(key, w);
    }
    return w;
  }
  // fit shortens text with an ellipsis to fit max world units.
  function fit(text, f, max) {
    if (!text) return "";
    if (textWidth(text, f) <= max) return text;
    let lo = 0, hi = text.length;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (textWidth(text.slice(0, mid) + "…", f) <= max) lo = mid; else hi = mid - 1;
    }
    return text.slice(0, lo) + "…";
  }

  // packed reports whether a graph is too large to be layered (flows with
  // fixed columns always are).
  const packed = (n, extra) => n > BIG && !(extra && extra["elk.partitioning.activate"]);
  function options(dir, aspect, n, extra) {
    const o = packed(n, extra) ? {
      "elk.algorithm": "rectpacking",
      "elk.aspectRatio": String(aspect),
      "elk.spacing.nodeNode": "16",
      "elk.padding": "[top=0,left=0,bottom=0,right=0]"
    } : {
      "elk.algorithm": "layered",
      "elk.direction": dir,
      "elk.edgeRouting": "ORTHOGONAL",
      "elk.aspectRatio": String(aspect),
      "elk.spacing.nodeNode": "22",
      "elk.layered.spacing.nodeNodeBetweenLayers": "46",
      "elk.spacing.edgeNode": "14",
      "elk.spacing.edgeEdge": "8",
      "elk.layered.spacing.edgeNodeBetweenLayers": "14",
      "elk.spacing.componentComponent": "30",
      "elk.layered.thoroughness": "4",
      "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
      "elk.layered.nodePlacement.strategy": "BRANDES_KOEPF",
      "elk.separateConnectedComponents": "true",
      "elk.padding": "[top=0,left=0,bottom=0,right=0]"
    };
    return Object.assign(o, extra || {});
  }

  // grid places nodes in rows, the fallback when ELK is missing or fails.
  function grid(nodes, aspect) {
    const area = nodes.reduce((s, n) => s + (n.width + 20) * (n.height + 20), 0);
    const rowW = Math.max(Math.sqrt(area * aspect), 200);
    let x = 0, y = 0, rowH = 0, W = 0;
    const out = {};
    nodes.forEach((n) => {
      if (x > 0 && x + n.width > rowW) { x = 0; y += rowH + 20; rowH = 0; }
      out[n.id] = { x, y };
      x += n.width + 20;
      rowH = Math.max(rowH, n.height);
      W = Math.max(W, x - 20);
    });
    return { pos: out, edges: {}, width: W, height: y + rowH };
  }

  // run lays out a flat graph: nodes [{id, width, height}], edges
  // [{id, s, t}]. Resolves to {pos: id → {x, y}, edges: id → points, width, height}.
  async function run(key, nodes, edges, dir, aspect, extra) {
    if (cache.has(key)) { stats.hits++; return cache.get(key); }
    let res;
    const t0 = performance.now();
    if (!elk || !nodes.length) res = grid(nodes, aspect);
    else {
      const ids = new Set(nodes.map((n) => n.id));
      const g = {
        id: "root",
        layoutOptions: options(dir, aspect, nodes.length, extra),
        children: nodes.map((n) => n.opts ? { id: n.id, width: n.width, height: n.height, layoutOptions: n.opts } : { id: n.id, width: n.width, height: n.height }),
        edges: packed(nodes.length, extra) ? [] : edges.filter((e) => e.s !== e.t && ids.has(e.s) && ids.has(e.t)).map((e) => ({ id: e.id, sources: [e.s], targets: [e.t] }))
      };
      try {
        const out = await elk.layout(g);
        const pos = {}, routes = {};
        out.children.forEach((c) => { pos[c.id] = { x: c.x || 0, y: c.y || 0 }; });
        (out.edges || []).forEach((e) => {
          const s = e.sections && e.sections[0];
          if (!s) return;
          const pts = [s.startPoint.x, s.startPoint.y];
          (s.bendPoints || []).forEach((b) => pts.push(b.x, b.y));
          pts.push(s.endPoint.x, s.endPoint.y);
          routes[e.id] = pts;
        });
        res = { pos, edges: routes, width: out.width || 0, height: out.height || 0 };
      } catch (err) {
        stats.failures++;
        res = grid(nodes, aspect);
      }
    }
    const ms = performance.now() - t0;
    stats.runs++; stats.ms += ms; stats.last = ms;
    if (cache.size > 600) cache.delete(cache.keys().next().value);
    cache.set(key, res);
    return res;
  }

  // runPartitioned lays out a left-to-right flow whose columns are fixed:
  // node.c is the column (minCol the leftmost).
  function runPartitioned(key, nodes, edges, minCol, aspect, dir) {
    nodes.forEach((n) => { n.opts = { "elk.partitioning.partition": String(n.c - minCol) }; });
    return run(key, nodes, edges, dir || "RIGHT", aspect, {
      "elk.partitioning.activate": "true",
      "elk.separateConnectedComponents": "false",
      "elk.layered.spacing.nodeNodeBetweenLayers": "64",
      "elk.spacing.nodeNode": "14"
    });
  }

  GV.layout = { run, runPartitioned, font, textWidth, fit, stats, available: !!elk, BIG };
})(GV);
