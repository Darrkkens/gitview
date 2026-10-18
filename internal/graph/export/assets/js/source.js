// Source code: the Go files embedded in the page (gzip + base64, decoded
// on first use with the browser's DecompressionStream), a small Go
// highlighter and the code view of the inspector. Code is data: every token
// becomes a text node inside a span.
(function (GV) {
  "use strict";
  const D = GV.data, h = GV.h;
  const info = D.raw.sources || {};
  let files = null, loading = null;

  function load() {
    if (loading) return loading;
    const el = document.getElementById("gitview-sources");
    const b64 = el ? el.textContent.trim() : "";
    if (!b64) { loading = Promise.resolve(null); return loading; }
    if (typeof DecompressionStream === "undefined") {
      loading = Promise.reject(new Error("This browser cannot decompress the embedded source (no DecompressionStream)."));
      return loading;
    }
    const bin = atob(b64);
    const u = new Uint8Array(bin.length);
    for (let k = 0; k < bin.length; k++) u[k] = bin.charCodeAt(k);
    loading = new Response(new Blob([u]).stream().pipeThrough(new DecompressionStream("gzip"))).text()
      .then((t) => { files = JSON.parse(t); return files; });
    return loading;
  }

  // ---------- tokenizer ----------
  const KW = new Set("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var".split(" "));
  const TYPES = new Set("bool byte complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr any comparable".split(" "));
  const BUILTIN = new Set("append cap clear close complex copy delete imag len make max min new panic print println real recover true false iota nil".split(" "));
  const RULES = [
    ["ws", /\s+/y],
    ["com", /\/\/[^\n]*/y],
    ["com", /\/\*[\s\S]*?(?:\*\/|$)/y],
    ["str", /`[^`]*(?:`|$)/y],
    ["str", /"(?:[^"\\\n]|\\.)*(?:"|$)/y],
    ["str", /'(?:[^'\\\n]|\\.)*(?:'|$)/y],
    ["num", /(?:0[xXbBoO][0-9a-fA-F_]+|\d[\d_]*(?:\.[\d_]*)?(?:[eE][+-]?\d+)?|\.\d[\d_]*(?:[eE][+-]?\d+)?)i?/y],
    ["id", /[A-Za-z_À-￿][\wÀ-￿]*/y],
    ["p", /[^\s\w"'`]/y]
  ];
  // tokenize returns [class, text] pairs covering the whole input.
  function tokenize(src) {
    const out = [];
    let pos = 0, prevId = "";
    while (pos < src.length) {
      let matched = false;
      for (const [cls, re] of RULES) {
        re.lastIndex = pos;
        const m = re.exec(src);
        if (!m || !m[0].length) continue;
        let c = cls;
        const text = m[0];
        if (cls === "id") {
          if (KW.has(text)) c = "kw";
          else if (TYPES.has(text)) c = "type";
          else if (BUILTIN.has(text)) c = "bi";
          else {
            // a call or a declaration: the next non-space character is "("
            let j = pos + text.length;
            while (j < src.length && (src[j] === " " || src[j] === "\t")) j++;
            if (src[j] === "(") c = "fn";
            else if (prevId === "type") c = "type";
            else c = "";
          }
          prevId = text;
        }
        out.push([c === "ws" ? "" : c, text]);
        pos += text.length;
        matched = true;
        break;
      }
      if (!matched) { out.push(["", src[pos]]); pos++; }
    }
    return out;
  }
  // lines splits the tokens at newlines: one array of tokens per line.
  function lines(tokens) {
    const rows = [[]];
    for (const [c, text] of tokens) {
      const parts = text.split("\n");
      parts.forEach((part, k) => {
        if (k) rows.push([]);
        if (part) rows[rows.length - 1].push([c, part]);
      });
    }
    return rows;
  }
  function spanOf(c, text) {
    if (!c) return document.createTextNode(text);
    const s = document.createElement("span");
    s.className = "tk-" + (c === "bi" ? "bi" : c);
    s.textContent = text;
    return s;
  }
  // inline highlights a short snippet (a signature) into one element.
  function inline(text, cls) {
    const el = GV.h("pre", { class: cls || "sig" });
    for (const [c, t] of tokenize(text || "")) el.appendChild(spanOf(c, t));
    return el;
  }

  const cache = new Map(); // path → token lines
  function tokenLines(path, text) {
    if (!cache.has(path)) cache.set(path, lines(tokenize(text)));
    return cache.get(path);
  }

  // span of a declaration: [from, to] lines (1-based, inclusive).
  function spanFor(n, text) {
    const total = text.split("\n").length;
    if (n.type === "file") return [1, total];
    if (!n.line) return [1, Math.min(total, 40)];
    if (n.end && n.end >= n.line) return [n.line, n.end];
    if (D.TYPE[n.type]) {
      // types carry only their first line: follow brackets to the end.
      const rows = text.split("\n");
      let depth = 0, seen = false;
      for (let k = n.line - 1; k < rows.length && k < n.line + 400; k++) {
        for (const ch of rows[k].replace(/\/\/.*$/, "").replace(/"(?:[^"\\]|\\.)*"|`[^`]*`/g, "")) {
          if (ch === "{" || ch === "(") { depth++; seen = true; }
          else if (ch === "}" || ch === ")") depth--;
        }
        if (depth <= 0 && (seen || k > n.line - 1 || !/[{(]\s*$/.test(rows[k]))) return [n.line, k + 1];
      }
      return [n.line, n.line];
    }
    return [n.line, n.line];
  }

  // view renders lines [from, to] of a file with line numbers; target
  // lines get a marker. ctx extra lines are shown around a call site.
  function view(path, text, from, to, opts) {
    opts = opts || {};
    const rows = tokenLines(path, text);
    const a = Math.max(1, from - (opts.ctx || 0)), b = Math.min(rows.length, to + (opts.ctx || 0));
    const table = GV.h("table");
    const frag = document.createDocumentFragment();
    for (let k = a; k <= b; k++) {
      const src = GV.h("td", { class: "src" });
      for (const [c, t] of rows[k - 1] || []) src.appendChild(spanOf(c, t));
      if (!src.firstChild) src.appendChild(document.createTextNode(" "));
      const cls = (k >= from && k <= to && opts.ctx ? "hl" : "") + (opts.target === k ? " target" : "");
      frag.appendChild(GV.h("tr", { class: cls.trim() || null, "data-line": k }, [GV.h("td", { class: "ln", text: String(k) }), src]));
    }
    table.appendChild(frag);
    return GV.h("div", { class: "code" }, [table]);
  }

  function unavailable(msg) {
    return GV.h("div", { class: "empty" }, [GV.h("strong", { text: "Source not available" }), msg]);
  }

  // render fills el with the source of node n (async).
  function render(el, n, opts) {
    opts = opts || {};
    GV.clear(el);
    if (n.ext) { el.appendChild(unavailable("Declared outside the analysed modules; GitView only embeds the repository's own files.")); return; }
    if (!n.file) { el.appendChild(unavailable("This element has no source location.")); return; }
    if (!info.embedded) {
      // A security report embeds only the excerpts its findings point to.
      const from = n.line || 1, to = n.end || (n.line ? n.line + 20 : 40);
      const ex = GV.security && GV.security.available ? GV.security.codeExcerpts(n.file, from, to, new Set(n.line ? [n.line] : [])) : null;
      if (ex) {
        el.appendChild(h("div", { class: "code-head" }, [h("span", { class: "path", text: n.file }), h("span", { class: "muted", text: "excerpts embedded by the security report" })]));
        el.appendChild(ex);
        return;
      }
      el.appendChild(unavailable(info.disabled ? "The page was exported without whole source files" + (GV.security && GV.security.available ? "; this element is not near any finding's evidence." : " (--include-source=false).") : "No source file could be embedded for this repository."));
      return;
    }
    el.appendChild(GV.h("div", { class: "skeleton" }));
    el.appendChild(GV.h("div", { class: "skeleton", style: { width: "70%" } }));
    load().then((fs) => {
      GV.clear(el);
      const text = fs && fs[n.file];
      if (text == null) {
        const skipped = (info.skipped || []).find((s) => s.startsWith(n.file + ":"));
        el.appendChild(unavailable(skipped ? "Left out of the page: " + skipped.slice(n.file.length + 2) + "." : "This file is not in the embedded source."));
        return;
      }
      let [from, to] = spanFor(n, text);
      const site = n.type === "db" || n.type === "unresolved" || n.type === "route";
      const whole = !!opts.whole;
      if (whole) { from = 1; to = text.split("\n").length; }
      const head = GV.h("div", { class: "code-head" }, [
        GV.h("span", { class: "path", title: n.file, text: n.file + ":" + from + (to > from ? "–" + to : "") }),
        n.type !== "file" ? GV.h("button", { type: "button", class: "btn sm ghost", text: whole ? "Show declaration" : "Whole file",
          on: { click: () => render(el, n, Object.assign({}, opts, { whole: !whole })) } }) : null,
        GV.copyButton("Code", () => text.split("\n").slice(from - 1, to).join("\n"), "Copy these lines")
      ]);
      el.appendChild(head);
      const code = view(n.file, text, from, to, { ctx: site && !whole ? 4 : 0, target: n.line });
      el.appendChild(code);
      if (whole || site) {
        const tr = code.querySelector('tr[data-line="' + n.line + '"]');
        if (tr) requestAnimationFrame(() => { code.scrollTop = Math.max(0, tr.offsetTop - 60); });
      }
    }, (err) => {
      GV.clear(el);
      el.appendChild(unavailable(err.message));
    });
  }

  GV.source = { info, load, tokenize, inline, render, spanFor, view, tokenLines: (text) => lines(tokenize(text)), span: spanOf };
})(GV);
