// Requests built locally from the static contract and never sent: the page
// has no HTTP client and its CSP forbids connections. defaultRequest
// mirrors httpapi.DefaultRequest (Go), so the first cURL shown is the one
// `gitview graph journey` prints. Values are synthetic; credentials are
// placeholders.
(function (GV) {
  "use strict";
  const hex = (c) => "%" + c.charCodeAt(0).toString(16).toUpperCase();
  // pathEscape and queryEscape follow Go's url.PathEscape and url.QueryEscape.
  function pathEscape(s) {
    return encodeURIComponent(s).replace(/[!'()*]/g, hex)
      .replace(/%(24|26|2B|2C|3A|3B|3D|40)/g, (m, x) => String.fromCharCode(parseInt(x, 16)));
  }
  function queryEscape(s) { return encodeURIComponent(s).replace(/[!'()*]/g, hex).replace(/%20/g, "+"); }
  function shellQuote(s) { return "'" + String(s).replace(/'/g, "'\\''") + "'"; }
  function exampleParam(p) {
    if (p.default) return p.default;
    const sh = p.schema || {};
    if (sh.type === "integer" || sh.type === "number") return "1";
    if (sh.type === "boolean") return "true";
    if (sh.format === "uuid") return "00000000-0000-4000-8000-000000000000";
    return "example";
  }
  function compactJSON(s) { try { return JSON.stringify(JSON.parse(s)); } catch (e) { return s; } }

  function defaultRequest(r) {
    const q = { base: r.example ? r.example.baseUrl : "http://localhost:8080", path: {}, query: [], headers: [], body: "", ct: "" };
    (r.params || []).forEach((p) => {
      if (p.in === "path") q.path[p.name] = exampleParam(p);
      else if (p.in === "query") q.query.push({ k: p.name, v: exampleParam(p), on: p.required === "yes" || !!p.default, note: p });
      else if (p.in === "header" && !/^(authorization|content-type)$/i.test(p.name)) q.headers.push({ k: p.name, v: "<" + p.name.toLowerCase() + ">", on: p.required === "yes", note: p });
    });
    const a = r.auth;
    if (a && a.recognized) {
      if (a.scheme === "bearer") q.headers.push({ k: "Authorization", v: "Bearer <token>", on: true });
      else if (a.scheme === "basic") q.headers.push({ k: "Authorization", v: "Basic <credentials>", on: true });
      else if (a.scheme === "cookie") {
        let name = (a.credential || "").replace(/^cookie /, "");
        if (name.indexOf(" ") >= 0) name = "<cookie>";
        q.headers.push({ k: "Cookie", v: name + "=<value>", on: true });
      }
    }
    if (r.body && r.example && r.example.body) {
      q.ct = (r.body.contentTypes && r.body.contentTypes[0]) || "application/json";
      q.headers.push({ k: "Content-Type", v: q.ct, on: true });
      q.body = r.example.body;
    }
    return q;
  }
  const edited = new Map(); // route id → request being edited in this page
  function reqOf(r) {
    if (!edited.has(r.id)) edited.set(r.id, defaultRequest(r));
    return edited.get(r.id);
  }
  function reset(r) { edited.delete(r.id); }
  function requestURL(r, q) {
    let path = r.template;
    Object.keys(q.path).sort().forEach((k) => { path = path.split("{" + k + "}").join(pathEscape(q.path[k])); });
    const u = q.base.replace(/\/$/, "") + path;
    const qs = q.query.filter((x) => x.on).map((x) => queryEscape(x.k) + "=" + queryEscape(x.v));
    return qs.length ? u + "?" + qs.join("&") : u;
  }
  function curlOf(r, q) {
    const m = r.method === "ANY" || r.method === "?" ? "GET" : r.method;
    let out = "curl" + (m !== "GET" ? " -X " + m : "") + " " + shellQuote(requestURL(r, q));
    q.headers.forEach((x) => { if (x.on) out += " \\\n  -H " + shellQuote(x.k + ": " + x.v); });
    if (q.body) out += " \\\n  --data-raw " + shellQuote(compactJSON(q.body));
    return out;
  }
  function fileName(r, ext) { return (r.method + "_" + r.path).replace(/[^A-Za-z0-9_.-]+/g, "_").replace(/_+/g, "_") + ext; }

  GV.requests = { pathEscape, queryEscape, shellQuote, exampleParam, defaultRequest, reqOf, reset, requestURL, curlOf, fileName };
})(GV);
