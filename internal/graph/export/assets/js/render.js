// Canvas renderer. Draws a scene (boxes and edges in world coordinates,
// computed by the views) with a pan/zoom camera, culling, level of detail
// and highlight/dim states, plus the minimap. It knows nothing about the
// dataset: views give it boxes and edges, it reports clicks and hovers.
//
// Box:  {key, kind: "group"|"node"|"context"|"more", x, y, w, h, depth, title, sub,
//        color (token), count, chev ("open"|"closed"|null), order, chip, chipColor, muted, scope}
// Edge: {key, s, t (box keys), pts [x0, y0, x1, y1, …], conf, n, label, strong}
(function (GV) {
  "use strict";
  const canvas = GV.$("canvas"), mini = GV.$("minimap");
  const ctx = canvas.getContext("2d"), mctx = mini.getContext("2d");
  let W = 0, H = 0, dpr = 1;
  let scene = { boxes: [], edges: [], bounds: { x: 0, y: 0, w: 1, h: 1 } };
  let byKey = new Map(), edgeByKey = new Map();
  const cam = { x: 0, y: 0, z: 1 };
  let hl = null;        // {boxes: Map key → role, edges: Map key → role, keep: Set of group keys}
  let selected = null;  // box or edge key
  let hover = null, hoverEdge = null;
  let tween = null, camTween = null;
  let dirty = true, miniDirty = true;
  let showMini = true;
  const stats = { draws: 0, last: 0, max: 0 };

  const TITLE = () => GV.layout.font(500, 12.5), SUB = () => GV.layout.font(400, 11), HEAD = () => GV.layout.font(600, 12);
  const C = (name) => GV.token(name);

  // ---------- size ----------
  function resize() {
    const r = canvas.getBoundingClientRect();
    dpr = window.devicePixelRatio || 1;
    W = Math.max(1, r.width); H = Math.max(1, r.height);
    canvas.width = Math.round(W * dpr); canvas.height = Math.round(H * dpr);
    const m = mini.getBoundingClientRect();
    mini.width = Math.round(Math.max(1, m.width) * dpr); mini.height = Math.round(Math.max(1, m.height) * dpr);
    dirty = miniDirty = true;
    schedule();
  }
  if (window.ResizeObserver) new ResizeObserver(resize).observe(canvas.parentElement);
  window.addEventListener("resize", resize);

  // ---------- scene ----------
  // setScene replaces what is drawn. opts.animate tweens boxes that keep
  // their key from their previous place; opts.fit fits the camera;
  // opts.anchor keeps that box at the same place on screen.
  function setScene(next, opts) {
    opts = opts || {};
    const prev = byKey;
    const before = opts.anchor && prev.get(opts.anchor) ? screenOf(prev.get(opts.anchor)) : null;
    scene = next;
    const wrap = GV.$("minimap-wrap"), wasHidden = wrap.hidden;
    wrap.hidden = !next.boxes.length;
    if (wasHidden && !wrap.hidden) {
      // measured while hidden: size it now
      const m = mini.getBoundingClientRect();
      mini.width = Math.round(Math.max(1, m.width) * dpr); mini.height = Math.round(Math.max(1, m.height) * dpr);
    }
    byKey = new Map();
    edgeByKey = new Map();
    const tf = TITLE(), sf = SUB(), hf = HEAD();
    next.boxes.forEach((b) => {
      byKey.set(b.key, b);
      const countW = b.count ? GV.layout.textWidth(b.count, sf) + 10 : 0;
      if (b.kind === "group") {
        // header: ■ title  subtitle ……… count
        b._title = GV.layout.fit(b.title, hf, Math.max(10, b.w - 36 - countW));
        const rest = b.w - 36 - countW - GV.layout.textWidth(b._title, hf) - 10;
        b._sub = b.sub && rest > 40 ? GV.layout.fit(b.sub, sf, rest) : "";
      } else {
        const left = 22 + (b.order ? 20 : 0), right = 10 + (b.chev ? 16 : 0) + countW;
        b._title = GV.layout.fit(b.title, tf, Math.max(10, b.w - left - right));
        b._sub = b.sub ? GV.layout.fit(b.sub, sf, Math.max(10, b.w - left - 10 - (b.chip ? GV.layout.textWidth(b.chip, sf) + 18 : 0))) : "";
      }
    });
    next.edges.forEach((e) => {
      edgeByKey.set(e.key, e);
      let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
      for (let k = 0; k < e.pts.length; k += 2) { x0 = Math.min(x0, e.pts[k]); x1 = Math.max(x1, e.pts[k]); y0 = Math.min(y0, e.pts[k + 1]); y1 = Math.max(y1, e.pts[k + 1]); }
      e._bb = [x0, y0, x1, y1];
    });
    if (selected && !byKey.has(selected) && !edgeByKey.has(selected)) selected = null;
    if (hover && !byKey.has(hover)) hover = null;
    hoverEdge = null;
    if (opts.animate && prev.size) {
      const from = new Map();
      next.boxes.forEach((b) => { const p = prev.get(b.key); if (p) from.set(b.key, { x: p.x, y: p.y, w: p.w, h: p.h }); });
      tween = { t0: performance.now(), ms: 260, from };
    } else tween = null;
    if (opts.fit) fit(false, opts.fitOpts);
    else if (before) {
      const b = byKey.get(opts.anchor);
      if (b) { cam.x = b.x + b.w / 2 - (before.x - W / 2) / cam.z; cam.y = b.y + b.h / 2 - (before.y - H / 2) / cam.z; }
    }
    dirty = miniDirty = true;
    schedule();
  }
  function screenOf(b) { return { x: (b.x + b.w / 2 - cam.x) * cam.z + W / 2, y: (b.y + b.h / 2 - cam.y) * cam.z + H / 2 }; }

  // ---------- camera ----------
  function clampZ(z) { return Math.max(0.03, Math.min(z, 3)); }
  function fitZoom(bb, pad) {
    pad = pad == null ? 48 : pad;
    return clampZ(Math.min((W - pad * 2) / Math.max(bb.w, 1), (H - pad * 2 - 30) / Math.max(bb.h, 1), 1.25));
  }
  function animateTo(x, y, z, animate) {
    if (animate === false || reduced()) { cam.x = x; cam.y = y; cam.z = z; camTween = null; dirty = miniDirty = true; schedule(); return; }
    camTween = { t0: performance.now(), ms: 280, a: { x: cam.x, y: cam.y, z: cam.z }, b: { x, y, z } };
    schedule();
  }
  // fit shows the whole scene. With opts.minZoom, a scene too large to be
  // read at once is shown at that zoom instead, around opts.anchor (kept
  // at the left edge with align "left", centred otherwise).
  function fit(animate, opts) {
    const bb = scene.bounds;
    const z = fitZoom(bb);
    const b = opts && opts.anchor ? byKey.get(opts.anchor) : null;
    if (opts && opts.minZoom && z < opts.minZoom && b) {
      const m = opts.minZoom;
      const x = opts.align === "left" || opts.align === "topleft" ? b.x - 60 + W / 2 / m : b.x + b.w / 2;
      const y = opts.align === "topleft" ? b.y - 70 + H / 2 / m : b.y + b.h / 2;
      animateTo(x, y, m, animate);
      return;
    }
    animateTo(bb.x + bb.w / 2, bb.y + bb.h / 2 + 12 / z, z, animate);
  }
  // reveal brings boxes into view, zooming in only as far as needed.
  function reveal(keys, animate) {
    const bs = keys.map((k) => byKey.get(k)).filter(Boolean);
    if (!bs.length) return;
    let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
    bs.forEach((b) => { x0 = Math.min(x0, b.x); y0 = Math.min(y0, b.y); x1 = Math.max(x1, b.x + b.w); y1 = Math.max(y1, b.y + b.h); });
    const bb = { x: x0, y: y0, w: x1 - x0, h: y1 - y0 };
    let z = cam.z;
    const need = fitZoom(bb, 80);
    if (z > need) z = need;
    if (z < 0.55 && need >= 0.55) z = Math.min(need, 0.9);
    animateTo(bb.x + bb.w / 2, bb.y + bb.h / 2, z, animate);
  }
  function zoomBy(f, sx, sy) {
    const z = clampZ(cam.z * f);
    if (sx === undefined) { sx = W / 2; sy = H / 2; }
    const wx = (sx - W / 2) / cam.z + cam.x, wy = (sy - H / 2) / cam.z + cam.y;
    cam.z = z;
    cam.x = wx - (sx - W / 2) / z; cam.y = wy - (sy - H / 2) / z;
    camTween = null;
    dirty = miniDirty = true;
    schedule();
  }
  const reduced = () => window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  // ---------- state from the views ----------
  function setHighlight(h) { hl = h; dirty = true; schedule(); }
  function setSelected(key) { selected = key; dirty = true; schedule(); }
  function setMinimap(on) { showMini = on; miniDirty = true; schedule(); }

  // ---------- drawing ----------
  let raf = 0;
  function schedule() { if (!raf) raf = requestAnimationFrame(frame); }
  function frame(now) {
    raf = 0;
    if (camTween) {
      const p = Math.max(0, Math.min(1, (now - camTween.t0) / camTween.ms)), e = 1 - Math.pow(1 - p, 3);
      cam.x = camTween.a.x + (camTween.b.x - camTween.a.x) * e;
      cam.y = camTween.a.y + (camTween.b.y - camTween.a.y) * e;
      cam.z = camTween.a.z + (camTween.b.z - camTween.a.z) * e;
      dirty = miniDirty = true;
      if (p >= 1) camTween = null;
    }
    let tp = 1;
    // a frame's timestamp can precede the tween's start
    if (tween) { tp = Math.max(0, Math.min(1, (now - tween.t0) / tween.ms)); dirty = true; }
    if (dirty) {
      const t0 = performance.now();
      draw(tp);
      dirty = false;
      stats.draws++; stats.last = performance.now() - t0; stats.max = Math.max(stats.max, stats.last);
    }
    if (miniDirty && showMini) { drawMini(); miniDirty = false; }
    if (tween && tp >= 1) tween = null;
    if (camTween || tween) schedule();
  }

  function geom(b, tp) {
    if (!tween || tp >= 1) return b;
    const f = tween.from.get(b.key);
    const e = 1 - Math.pow(1 - tp, 3);
    if (!f) return b;
    return { x: f.x + (b.x - f.x) * e, y: f.y + (b.y - f.y) * e, w: f.w + (b.w - f.w) * e, h: f.h + (b.h - f.h) * e };
  }
  function rr(c, x, y, w, h, r) {
    w = Math.max(w, 1); h = Math.max(h, 1);
    r = Math.min(r, w / 2, h / 2);
    c.beginPath();
    c.moveTo(x + r, y);
    c.arcTo(x + w, y, x + w, y + h, r);
    c.arcTo(x + w, y + h, x, y + h, r);
    c.arcTo(x, y + h, x, y, r);
    c.arcTo(x, y, x + w, y, r);
    c.closePath();
  }
  function roleOf(key) { return hl && hl.boxes ? hl.boxes.get(key) : undefined; }
  function boxAlpha(b) {
    if (!hl) return 1;
    if (hl.boxes.has(b.key)) return 1;
    if (b.kind === "group" && hl.keep && hl.keep.has(b.key)) return 1;
    return Number(C("dim-alpha")) || 0.16;
  }

  function draw(tp) {
    const z = cam.z;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.fillStyle = C("canvas");
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.setTransform(dpr * z, 0, 0, dpr * z, dpr * (W / 2 - cam.x * z), dpr * (H / 2 - cam.y * z));
    const vx0 = cam.x - W / 2 / z, vy0 = cam.y - H / 2 / z, vx1 = cam.x + W / 2 / z, vy1 = cam.y + H / 2 / z;
    const inView = (x, y, w, h) => x + w >= vx0 && x <= vx1 && y + h >= vy0 && y <= vy1;
    const px = 1 / z; // one screen pixel in world units
    const showText = 12.5 * z >= 5.5, showSub = 11 * z >= 7.5;

    const late = [];
    // groups, outer first
    const groups = scene.boxes.filter((b) => b.kind === "group");
    for (const b of groups) {
      const g = geom(b, tp);
      if (!inView(g.x, g.y, g.w, g.h)) continue;
      ctx.globalAlpha = boxAlpha(b);
      rr(ctx, g.x, g.y, g.w, g.h, 8);
      ctx.fillStyle = C("group-fill");
      ctx.fill();
      ctx.lineWidth = (b.key === selected ? 2 : 1) * px;
      ctx.strokeStyle = b.key === selected ? C("accent") : b.scope ? C("border-strong") : C("group-border");
      if (b.muted) ctx.setLineDash([4 * px, 3 * px]);
      ctx.stroke();
      ctx.setLineDash([]);
      // header
      ctx.save();
      rr(ctx, g.x, g.y, g.w, g.h, 8);
      ctx.clip();
      ctx.fillStyle = C("group-head");
      ctx.fillRect(g.x, g.y, g.w, 30);
      ctx.restore();
      ctx.fillStyle = C(b.color);
      ctx.fillRect(g.x + 10, g.y + 11, 8, 8);
      if (showText && (!tween || tp >= 1)) {
        ctx.font = HEAD();
        ctx.textBaseline = "middle";
        ctx.fillStyle = C("text");
        ctx.fillText(b._title, g.x + 24, g.y + 15.5);
        if (showSub && b._sub) {
          const tw = GV.layout.textWidth(b._title, HEAD());
          ctx.font = SUB();
          ctx.fillStyle = C("text-3");
          ctx.fillText(b._sub, g.x + 24 + tw + 10, g.y + 15.5);
        }
        if (b.count) {
          ctx.font = SUB();
          ctx.fillStyle = C("text-3");
          ctx.textAlign = "right";
          ctx.fillText(b.count, g.x + g.w - 12, g.y + 15.5);
          ctx.textAlign = "left";
        }
      }
      if (!showText && (!tween || tp >= 1)) late.push([b.title, g, 11, 600, true, g.w * g.h * 4]);
      if (b.key === hover && b.key !== selected) {
        rr(ctx, g.x, g.y, g.w, g.h, 8);
        ctx.lineWidth = px;
        ctx.strokeStyle = C("edge-strong");
        ctx.stroke();
      }
    }
    ctx.globalAlpha = 1;

    // edges
    const edgeAlpha = tween && tp < 1 ? Math.max(0, (tp - 0.6) / 0.4) : 1;
    if (edgeAlpha > 0) {
      const pass = (want) => {
        for (const e of scene.edges) {
          const role = hl ? hl.edges.get(e.key) : undefined;
          const isHover = hoverEdge === e.key || (hover && (e.s === hover || e.t === hover || (e.units && e.units.has(hover))));
          // branches (inside a box, towards a bundled trunk) only show on demand
          if (e.branch && !role && !isHover && e.key !== selected) continue;
          const strong = role || e.key === selected || isHover;
          if ((want === "strong") !== !!strong) continue;
          const bb = e._bb;
          if (!inView(bb[0], bb[1], bb[2] - bb[0] + 1, bb[3] - bb[1] + 1)) continue;
          drawEdge(e, role, isHover, px, edgeAlpha);
        }
      };
      pass("weak");
      pass("strong");
    }

    // nodes
    for (const b of scene.boxes) {
      if (b.kind === "group") continue;
      const g = geom(b, tp);
      if (!inView(g.x, g.y, g.w, g.h)) continue;
      ctx.globalAlpha = boxAlpha(b) * (tween && !tween.from.has(b.key) && tp < 1 ? tp : 1);
      drawNode(b, g, px, showText, showSub);
      if (!showText && (!tween || tp >= 1)) late.push([b.title, g, 10, 500, false, g.w * g.h]);
    }
    // zoomed-out names last, larger boxes first, never over each other
    if (late.length) {
      const taken = [];
      late.sort((a, b) => b[5] - a[5]).forEach((l) => zoomedOutLabel(l[0], l[1], l[2], l[3], l[4], taken));
    }
    ctx.globalAlpha = 1;

    // labels of the strong edges, on top, never over each other
    if (edgeAlpha >= 1 && showText) {
      const taken = [];
      const want = (e) => {
        const role = hl ? hl.edges.get(e.key) : undefined;
        if (!e.label || e.branch) return 0;
        if (e.key === selected || hoverEdge === e.key) return 3;
        if (hover && (e.s === hover || e.t === hover || (e.units && e.units.has(hover)))) return 2;
        return (role === "out" || role === "in") && (e.n > 1 || Object.keys((e.agg && e.agg.types) || {}).length > 1) ? 1 : 0;
      };
      scene.edges.map((e) => [want(e), e]).filter((x) => x[0]).sort((a, b) => b[0] - a[0]).forEach(([, e]) => drawEdgeLabel(e, px, taken));
    }
  }

  // zoomedOutLabel: when the scene is too small for its text, boxes that
  // are still large on screen keep their name at a fixed screen size
  // (semantic zoom), a group's as a tag on its corner.
  function zoomedOutLabel(text, g, screenPx, weight, tag, taken) {
    const z = cam.z;
    if (g.w * z < (tag ? 70 : 46) || g.h * z < 9) return;
    const fs = Math.max(1, Math.round(screenPx / z));
    const f = GV.layout.font(weight, fs);
    const pad = 6 / z;
    const t = GV.layout.fit(text, f, g.w - pad * 3);
    if (!t || t === "…") return;
    const w = GV.layout.textWidth(t, f) + pad * 2, hh = (screenPx + 8) / z;
    const y0 = tag ? g.y + pad / 2 : g.y + g.h / 2 - hh / 2;
    const r = [g.x, y0, g.x + w + pad, y0 + hh];
    if (taken.some((q) => r[0] < q[2] && r[2] > q[0] && r[1] < q[3] && r[3] > q[1])) return;
    taken.push(r);
    ctx.font = f;
    ctx.textBaseline = "middle";
    if (tag) {
      rr(ctx, g.x + pad / 2, g.y + pad / 2, w, hh, 4 / z);
      ctx.fillStyle = C("surface-1");
      ctx.fill();
      ctx.lineWidth = 1 / z;
      ctx.strokeStyle = C("border-strong");
      ctx.stroke();
      ctx.fillStyle = C("text");
      ctx.fillText(t, g.x + pad * 1.5, g.y + pad / 2 + hh / 2);
    } else {
      ctx.fillStyle = C("text-2");
      ctx.fillText(t, g.x + pad * 1.4, g.y + g.h / 2);
    }
  }

  function drawNode(b, g, px, showText, showSub) {
    const role = roleOf(b.key);
    const sel = b.key === selected;
    rr(ctx, g.x, g.y, g.w, g.h, 6);
    ctx.fillStyle = b.kind === "context" || b.kind === "more" ? C("context-fill") : b.key === hover ? C("node-fill-hover") : C("node-fill");
    ctx.fill();
    ctx.lineWidth = (sel ? 2 : role ? 1.5 : 1) * px;
    ctx.strokeStyle = sel ? C("accent") : role === "out" ? C("hl-out") : role === "in" ? C("hl-in") : role === "both" ? C("hl-out") : b.key === hover ? C("edge-strong") : C("node-border");
    if (role === "sel" && b.kind !== "group") ctx.strokeStyle = C("accent");
    if (b.kind === "context" || b.kind === "more" || b.muted) ctx.setLineDash([4 * px, 3 * px]);
    ctx.stroke();
    ctx.setLineDash([]);
    // category bar
    if (b.kind !== "more") {
      ctx.save();
      rr(ctx, g.x, g.y, g.w, g.h, 6);
      ctx.clip();
      ctx.fillStyle = C(b.color);
      ctx.globalAlpha *= b.kind === "context" ? 0.5 : 1;
      ctx.fillRect(g.x, g.y, 3, g.h);
      ctx.restore();
    }
    if (!showText) return;
    const twoLines = showSub && b._sub && g.h >= 34;
    let tx = g.x + 12;
    if (b.order) {
      ctx.fillStyle = C("surface-3");
      ctx.beginPath(); ctx.arc(g.x + 20, g.y + g.h / 2, 8, 0, Math.PI * 2); ctx.fill();
      ctx.font = GV.layout.font(600, 10);
      ctx.fillStyle = C("text-2");
      ctx.textAlign = "center"; ctx.textBaseline = "middle";
      ctx.fillText(String(b.order), g.x + 20, g.y + g.h / 2 + 0.5);
      ctx.textAlign = "left";
      tx += 20;
    }
    ctx.textBaseline = "middle";
    ctx.font = TITLE();
    ctx.fillStyle = b.kind === "context" || b.kind === "more" ? C("text-2") : C("text");
    ctx.fillText(b._title, tx, twoLines ? g.y + g.h / 2 - 7 : g.y + g.h / 2 + 0.5);
    if (twoLines) {
      ctx.font = SUB();
      ctx.fillStyle = C("text-3");
      ctx.fillText(b._sub, tx, g.y + g.h / 2 + 8);
    }
    let rx = g.x + g.w - 10;
    if (b.chev) {
      ctx.strokeStyle = C("text-3");
      ctx.lineWidth = 1.4 * px;
      ctx.beginPath();
      const cx = rx - 4, cy = g.y + g.h / 2;
      if (b.chev === "closed") { ctx.moveTo(cx - 2, cy - 4); ctx.lineTo(cx + 2, cy); ctx.lineTo(cx - 2, cy + 4); }
      else { ctx.moveTo(cx - 4, cy - 2); ctx.lineTo(cx, cy + 2); ctx.lineTo(cx + 4, cy - 2); }
      ctx.stroke();
      rx -= 16;
    }
    if (b.count) {
      ctx.font = SUB();
      ctx.fillStyle = C("text-3");
      ctx.textAlign = "right";
      ctx.fillText(b.count, rx, twoLines ? g.y + g.h / 2 - 7 : g.y + g.h / 2 + 0.5);
      ctx.textAlign = "left";
    }
    if (b.chip && twoLines) {
      ctx.font = SUB();
      const w = GV.layout.textWidth(b.chip, SUB()) + 10;
      const x = g.x + g.w - w - 8, y = g.y + g.h / 2 + 1;
      rr(ctx, x, y, w, 15, 7.5);
      ctx.fillStyle = C("surface-3");
      ctx.fill();
      ctx.fillStyle = C(b.chipColor || "text-2");
      ctx.textAlign = "center";
      ctx.fillText(b.chip, x + w / 2, y + 8);
      ctx.textAlign = "left";
    }
  }

  const DASH = { likely: [6, 4], possible: [2, 4], unresolved: [5, 4], chain: [3, 3], finding: [1, 3] };
  function drawEdge(e, role, isHover, px, alpha) {
    const p = e.pts;
    if (p.length < 4) return;
    const far = role === "out2" || role === "in2";
    const color = role === "out" || role === "out2" ? C("hl-out") : role === "in" || role === "in2" ? C("hl-in") : e.key === selected ? C("accent") : e.color ? C(e.color) : e.conf === "unresolved" ? C("c-unresolved") : isHover ? C("edge-strong") : C("edge");
    const width = (1 + Math.min(Math.log2(1 + (e.n || 1)), 3) * 0.45 + ((role && !far) || isHover || e.key === selected ? 0.7 : 0)) * px;
    ctx.globalAlpha = alpha * (hl && !role && e.key !== selected ? Number(C("dim-alpha")) || 0.16 : far && !isHover ? 0.45 : 1);
    ctx.strokeStyle = color;
    ctx.fillStyle = color;
    ctx.lineWidth = width;
    const dash = DASH[e.conf];
    ctx.setLineDash(dash ? dash.map((d) => d * px) : []);
    ctx.beginPath();
    ctx.moveTo(p[0], p[1]);
    const r = 6;
    for (let k = 2; k < p.length - 2; k += 2) ctx.arcTo(p[k], p[k + 1], p[k + 2], p[k + 3], r);
    // stop the line at the arrow's base
    const ex = p[p.length - 2], ey = p[p.length - 1], sx = p[p.length - 4], sy = p[p.length - 3];
    const len = Math.hypot(ex - sx, ey - sy) || 1;
    const ah = Math.min(8 * px, len * 0.6), ux = (ex - sx) / len, uy = (ey - sy) / len;
    ctx.lineTo(ex - ux * ah * 0.8, ey - uy * ah * 0.8);
    ctx.stroke();
    ctx.setLineDash([]);
    ctx.beginPath();
    ctx.moveTo(ex, ey);
    ctx.lineTo(ex - ux * ah - uy * ah * 0.45, ey - uy * ah + ux * ah * 0.45);
    ctx.lineTo(ex - ux * ah + uy * ah * 0.45, ey - uy * ah - ux * ah * 0.45);
    ctx.closePath();
    ctx.fill();
    ctx.globalAlpha = 1;
  }
  function midpoint(p) {
    let total = 0;
    for (let k = 0; k < p.length - 2; k += 2) total += Math.hypot(p[k + 2] - p[k], p[k + 3] - p[k + 1]);
    let half = total / 2;
    for (let k = 0; k < p.length - 2; k += 2) {
      const l = Math.hypot(p[k + 2] - p[k], p[k + 3] - p[k + 1]);
      if (half <= l) { const f = l ? half / l : 0; return [p[k] + (p[k + 2] - p[k]) * f, p[k + 1] + (p[k + 3] - p[k + 1]) * f]; }
      half -= l;
    }
    return [p[0], p[1]];
  }
  function drawEdgeLabel(e, px, taken) {
    const [mx, my] = midpoint(e.pts);
    const f = GV.layout.font(500, 10.5);
    ctx.font = f;
    const w = GV.layout.textWidth(e.label, f) + 10;
    const r = [mx - w / 2 - 2, my - 10, mx + w / 2 + 2, my + 10];
    if (taken.some((q) => r[0] < q[2] && r[2] > q[0] && r[1] < q[3] && r[3] > q[1])) return;
    taken.push(r);
    rr(ctx, mx - w / 2, my - 8, w, 16, 8);
    ctx.fillStyle = C("surface-1");
    ctx.fill();
    ctx.lineWidth = px;
    ctx.strokeStyle = C("border-strong");
    ctx.stroke();
    ctx.fillStyle = C("text-2");
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.fillText(e.label, mx, my + 0.5);
    ctx.textAlign = "left";
  }

  // ---------- minimap ----------
  let miniT = null; // world → minimap transform
  function drawMini() {
    const mw = mini.width, mh = mini.height;
    mctx.setTransform(1, 0, 0, 1, 0, 0);
    mctx.fillStyle = C("surface-1");
    mctx.fillRect(0, 0, mw, mh);
    const bb = scene.bounds;
    if (!scene.boxes.length) return;
    const s = Math.min((mw - 12 * dpr) / Math.max(bb.w, 1), (mh - 12 * dpr) / Math.max(bb.h, 1));
    const ox = (mw - bb.w * s) / 2 - bb.x * s, oy = (mh - bb.h * s) / 2 - bb.y * s;
    miniT = { s, ox, oy };
    for (const b of scene.boxes) {
      if (b.kind === "group") {
        mctx.strokeStyle = C("group-border");
        mctx.lineWidth = dpr;
        mctx.strokeRect(b.x * s + ox, b.y * s + oy, Math.max(1, b.w * s), Math.max(1, b.h * s));
      }
    }
    for (const b of scene.boxes) {
      if (b.kind === "group") continue;
      mctx.globalAlpha = hl && !hl.boxes.has(b.key) ? 0.25 : 0.85;
      mctx.fillStyle = b.key === selected ? C("accent") : C(b.color);
      mctx.fillRect(b.x * s + ox, b.y * s + oy, Math.max(1.5, b.w * s), Math.max(1.5, b.h * s));
    }
    mctx.globalAlpha = 1;
    const vx = (cam.x - W / 2 / cam.z) * s + ox, vy = (cam.y - H / 2 / cam.z) * s + oy;
    mctx.strokeStyle = C("accent");
    mctx.lineWidth = 1.5 * dpr;
    mctx.strokeRect(vx, vy, W / cam.z * s, H / cam.z * s);
    mctx.fillStyle = C("accent-weak");
    mctx.fillRect(vx, vy, W / cam.z * s, H / cam.z * s);
  }
  function miniMove(ev) {
    if (!miniT) return;
    const r = mini.getBoundingClientRect();
    const mx = (ev.clientX - r.left) * dpr, my = (ev.clientY - r.top) * dpr;
    cam.x = (mx - miniT.ox) / miniT.s; cam.y = (my - miniT.oy) / miniT.s;
    camTween = null;
    dirty = miniDirty = true;
    schedule();
  }
  mini.addEventListener("pointerdown", (ev) => {
    mini.setPointerCapture(ev.pointerId);
    miniMove(ev);
    const move = (e) => miniMove(e);
    const up = () => { mini.removeEventListener("pointermove", move); mini.removeEventListener("pointerup", up); };
    mini.addEventListener("pointermove", move);
    mini.addEventListener("pointerup", up);
  });

  // ---------- hit testing ----------
  function toWorld(sx, sy) { return { x: (sx - W / 2) / cam.z + cam.x, y: (sy - H / 2) / cam.z + cam.y }; }
  function boxAt(sx, sy) {
    const p = toWorld(sx, sy);
    for (let k = scene.boxes.length - 1; k >= 0; k--) {
      const b = scene.boxes[k];
      if (b.kind === "group") continue;
      if (p.x >= b.x && p.x <= b.x + b.w && p.y >= b.y && p.y <= b.y + b.h) return b;
    }
    let best = null;
    for (const b of scene.boxes) {
      if (b.kind !== "group") continue;
      if (p.x >= b.x && p.x <= b.x + b.w && p.y >= b.y && p.y <= b.y + b.h && (!best || b.depth >= best.depth)) best = b;
    }
    return best;
  }
  function inHeader(b, sx, sy) { const p = toWorld(sx, sy); return b.kind === "group" && p.y - b.y <= 30; }
  function edgeAt(sx, sy) {
    const p = toWorld(sx, sy), tol = 5 / cam.z;
    let best = null, bd = tol;
    for (const e of scene.edges) {
      const bb = e._bb;
      if (p.x < bb[0] - tol || p.x > bb[2] + tol || p.y < bb[1] - tol || p.y > bb[3] + tol) continue;
      for (let k = 0; k < e.pts.length - 2; k += 2) {
        const d = segDist(p.x, p.y, e.pts[k], e.pts[k + 1], e.pts[k + 2], e.pts[k + 3]);
        if (d < bd) { bd = d; best = e; }
      }
    }
    return best;
  }
  function segDist(px, py, x0, y0, x1, y1) {
    const dx = x1 - x0, dy = y1 - y0, l2 = dx * dx + dy * dy;
    let t = l2 ? ((px - x0) * dx + (py - y0) * dy) / l2 : 0;
    t = Math.max(0, Math.min(1, t));
    return Math.hypot(px - (x0 + t * dx), py - (y0 + t * dy));
  }

  // ---------- input ----------
  let drag = null;
  canvas.addEventListener("pointerdown", (ev) => {
    if (ev.button !== 0) return;
    canvas.focus({ preventScroll: true });
    drag = { x: ev.clientX, y: ev.clientY, cx: cam.x, cy: cam.y, moved: false, id: ev.pointerId };
    canvas.setPointerCapture(ev.pointerId);
  });
  canvas.addEventListener("pointermove", (ev) => {
    const r = canvas.getBoundingClientRect();
    const sx = ev.clientX - r.left, sy = ev.clientY - r.top;
    if (drag) {
      const dx = ev.clientX - drag.x, dy = ev.clientY - drag.y;
      if (!drag.moved && Math.hypot(dx, dy) > 4) { drag.moved = true; canvas.classList.add("grabbing"); }
      if (drag.moved) {
        cam.x = drag.cx - dx / cam.z; cam.y = drag.cy - dy / cam.z;
        camTween = null;
        dirty = miniDirty = true;
        schedule();
      }
      return;
    }
    const b = boxAt(sx, sy);
    const e = b && b.kind !== "group" ? null : edgeAt(sx, sy);
    const hk = b && b.kind !== "group" ? b.key : b && inHeader(b, sx, sy) ? b.key : null;
    const ek = e ? e.key : null;
    if (hk !== hover || ek !== hoverEdge) {
      hover = hk; hoverEdge = hk ? null : ek;
      canvas.classList.toggle("pointer", !!(hover || hoverEdge));
      canvas.title = hoverEdge && edgeByKey.get(hoverEdge).label ? edgeByKey.get(hoverEdge).label : hover && byKey.get(hover).tip ? byKey.get(hover).tip : "";
      dirty = true;
      schedule();
      GV.emit("canvas:hover", hover);
    }
  });
  canvas.addEventListener("pointerup", (ev) => {
    if (!drag) return;
    const moved = drag.moved;
    drag = null;
    canvas.classList.remove("grabbing");
    if (moved) return;
    const r = canvas.getBoundingClientRect();
    const sx = ev.clientX - r.left, sy = ev.clientY - r.top;
    const b = boxAt(sx, sy);
    const e = b && b.kind !== "group" ? null : edgeAt(sx, sy);
    if (e && !(b && b.kind !== "group")) GV.emit("canvas:edge", e);
    else if (b) GV.emit("canvas:box", { box: b, header: inHeader(b, sx, sy), ev });
    else GV.emit("canvas:empty");
  });
  canvas.addEventListener("pointerleave", () => { if (hover || hoverEdge) { hover = hoverEdge = null; dirty = true; schedule(); } });
  canvas.addEventListener("dblclick", (ev) => {
    const r = canvas.getBoundingClientRect();
    const b = boxAt(ev.clientX - r.left, ev.clientY - r.top);
    if (b) GV.emit("canvas:dblbox", { box: b, header: inHeader(b, ev.clientX - r.left, ev.clientY - r.top) });
    else fit();
  });
  canvas.addEventListener("wheel", (ev) => {
    ev.preventDefault();
    const r = canvas.getBoundingClientRect();
    const k = ev.ctrlKey ? 0.012 : 0.0018;
    const d = ev.deltaMode === 1 ? ev.deltaY * 16 : ev.deltaY;
    zoomBy(Math.exp(-d * k), ev.clientX - r.left, ev.clientY - r.top);
  }, { passive: false });
  canvas.addEventListener("keydown", (ev) => {
    const step = 60 / cam.z;
    const moves = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] };
    if (moves[ev.key] && !ev.altKey) {
      cam.x += moves[ev.key][0]; cam.y += moves[ev.key][1];
      dirty = miniDirty = true; schedule();
      ev.preventDefault();
    }
  });
  GV.on("theme", () => { dirty = miniDirty = true; schedule(); });

  resize();
  GV.render = {
    setScene, setHighlight, setSelected, setMinimap, fit, reveal, zoomBy,
    box: (k) => byKey.get(k), edge: (k) => edgeByKey.get(k), scene: () => scene, camera: () => Object.assign({}, cam), highlight: () => hl, selected: () => selected, stats,
    redraw: () => { dirty = miniDirty = true; schedule(); },
    screenOf: (k) => byKey.get(k) ? screenOf(byKey.get(k)) : null
  };
})(GV);
