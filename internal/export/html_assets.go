package export

// Static assets of the HTML report. Nothing here is built from repository
// data: content reaches the page only through html/template escaping, and the
// scripts below are fixed strings whose hashes are pinned in the CSP.

const htmlSource = `<!doctype html>
<html lang="en" data-theme="dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="{{.CSP}}">
<meta name="generator" content="GitView">
<title>{{.R.Project}} · {{.R.Branch}} · GitView report</title>
<script>{{.HeadJS}}</script>
<style>{{.Style}}{{.ThemeCSS}}</style>
</head>
<body>
<header class="wrap report-head" id="overview-top">
  <div class="eyebrow">GitView report</div>
  <h1>{{.R.Project}}</h1>
  <dl class="meta">
    {{- if .R.Branch}}<div><dt>Branch</dt><dd class="mono">{{.R.Branch}}</dd></div>{{end}}
    {{- with .R.Base}}<div><dt>Base</dt><dd><span class="mono" title="{{.Hash}}">{{.Short}}</span> <span class="subject">{{.Subject}}</span></dd></div>{{end}}
    <div><dt>Compared</dt><dd>{{if .R.Base}}working tree (uncommitted){{else}}working tree · no commits yet{{end}}</dd></div>
    <div><dt>Generated</dt><dd><time datetime="{{.GeneratedTS}}">{{.Generated}}</time></dd></div>
    {{- if .R.Scope}}<div class="opt"><dt>Scope</dt><dd>{{.R.Scope}}</dd></div>{{end}}
    {{- if .R.Query}}<div class="opt"><dt>Search</dt><dd class="mono">{{.R.Query}}</dd></div>{{end}}
  </dl>
  <p class="statline">
    <span><b>{{len .Files}}</b> {{if eq (len .Files) 1}}file{{else}}files{{end}} changed</span><span class="sep">·</span><span class="ok-text"><b>{{.Added}}</b> {{if eq .Added 1}}addition{{else}}additions{{end}}</span><span class="sep">·</span><span class="bad-text"><b>{{.Deleted}}</b> {{if eq .Deleted 1}}deletion{{else}}deletions{{end}}</span><span class="sep">·</span><a href="#tests" class="{{.TestsLine.Class}}-text">{{.TestsLine.Text}}</a>
  </p>
</header>

<nav class="tabs" aria-label="Report sections">
  <div class="wrap tabs-inner">
    <a href="#overview" data-section="overview">Overview</a>
    <a href="#files" data-section="files">Files changed <span class="count">{{len .Files}}</span></a>
    <a href="#tests" data-section="tests">Tests{{with .Tests}} <span class="count {{.Class}}">{{.Status}}</span>{{end}}</a>
    {{- if .HasFlow}}
    <a href="#codeflow" data-section="codeflow">Code Flow</a>
    {{- end}}
    {{- if .HasInspect}}
    <a href="#inspect" data-section="inspect">Go Deep Inspector</a>
    {{- end}}
    {{- if .HasStudy}}
    <a href="#study" data-section="study">Study</a>
    {{- end}}
    <span class="grow"></span>
    <button type="button" class="ghost" data-action="theme" id="theme-toggle" aria-pressed="false">Light theme</button>
  </div>
</nav>

<main>
<section id="overview" class="wrap" data-section="overview">
  <h2>Overview</h2>
  {{- if .Files}}
  <div class="flist" role="list">
    {{- range .Files}}
    <a class="frow" role="listitem" href="#{{.ID}}" data-path="{{.Path}}">
      <span class="st st-{{.Letter}}" title="{{.StatusName}}">{{.Letter}}</span>
      <span class="fpath"><span class="fname">{{.Name}}</span>{{if .DirLabel}}<span class="fdir">{{.DirLabel}}</span>{{end}}{{if .OldPath}}<span class="fdir">renamed from {{.OldPath}}</span>{{end}}</span>
      <span class="fstat">{{template "stat" .}}</span>
    </a>
    {{- end}}
  </div>
  {{- else}}
  <p class="note">No changes in this report.</p>
  {{- end}}
  {{- if or .R.Warnings .R.Omitted}}
  <h3>Warnings</h3>
  <ul class="warnings">
    {{- range .R.Warnings}}<li>{{.}}</li>{{end}}
    {{- range .R.Omitted}}<li>Content of <code>{{.}}</code> was omitted because it may contain secrets.</li>{{end}}
  </ul>
  {{- end}}
</section>

<section id="files" class="files-wrap" data-section="files">
  <div class="toolbar">
    <h2>Files changed <span class="sub">{{len .Files}}</span></h2>
    <span class="grow"></span>
    <label class="filter"><span class="sr">Filter files</span><input id="file-filter" type="search" placeholder="Filter files…" autocomplete="off" spellcheck="false"><kbd>/</kbd></label>
    <span id="filter-count" class="muted small" aria-live="polite"></span>
    <button type="button" class="ghost" data-action="expand-all">Expand all</button>
    <button type="button" class="ghost" data-action="collapse-all">Collapse all</button>
  </div>
  {{- range .Files}}{{$fid := .ID}}
  <details class="file" id="{{.ID}}" data-path="{{.Path}}"{{if .Open}} open{{end}}>
    <summary class="fhead">
      <span class="chev" aria-hidden="true"></span>
      <span class="st st-{{.Letter}}" title="{{.StatusName}}">{{.Letter}}</span>
      <span class="hpath mono"><span class="hdir">{{.Dir}}</span><span class="hname">{{.Name}}</span></span>
      {{- if .OldPath}}<span class="tag">from {{.OldPath}}</span>{{end}}
      {{- if .Omitted}}<span class="tag warn-text">content omitted</span>{{end}}
      <span class="grow"></span>
      <span class="fstat">{{template "stat" .}}</span>
      <span class="actions">
        <button type="button" class="ghost small" data-action="copy-path" data-path="{{.Path}}" title="Copy file path">Copy path</button>
        <a class="ghost small anchor" href="#{{.ID}}" title="Link to this file" aria-label="Link to {{.Path}}">#</a>
      </span>
    </summary>
    {{- if .Omitted}}
    <p class="note">Content omitted: this file may contain secrets. Export again with sensitive content explicitly included to show it.</p>
    {{- else if .Binary}}
    <p class="note">Binary file not shown.</p>
    {{- else if not .Hunks}}
    <p class="note">No content changes (mode change, rename or empty file).</p>
    {{- else}}
    <div class="diff" tabindex="0" aria-label="Diff of {{.Path}}">
    <table style="{{digits .NumDigits}}">
      <colgroup><col class="c-ln"><col class="c-ln"><col class="c-sg"><col></colgroup>
      {{- range .Hunks}}
      <tbody class="hunk">
        <tr class="hh"><td class="ln old"></td><td class="ln new"></td><td colspan="2" class="hcell"><span class="range">{{.Range}}</span>{{if .Section}} <span class="section">{{.Section}}</span>{{end}}<button type="button" class="ghost small copy-hunk" data-action="copy-hunk" title="Copy this hunk">Copy</button></td></tr>
        {{- range .Lines}}
        <tr class="l {{.Kind}}{{if .Empty}} empty{{end}}"{{if .New}} id="{{$fid}}-L{{.New}}"{{end}}><td class="ln old">{{if .Old}}{{.Old}}{{end}}</td><td class="ln new">{{if .New}}{{.New}}{{end}}</td><td class="sg">{{.Sign}}</td><td class="cd">{{range .Spans}}{{if .Class}}<span class="{{.Class}}">{{.Text}}</span>{{else}}{{.Text}}{{end}}{{end}}</td></tr>
        {{- end}}
      </tbody>
      {{- end}}
    </table>
    </div>
    {{- end}}
  </details>
  {{- end}}
  <p id="filter-empty" class="note" hidden>No files match the filter.</p>
</section>

<section id="tests" class="wrap" data-section="tests">
  {{- with .Tests}}
  <h2>Tests{{range .Summary}} <span class="sep">·</span> <span class="{{.Class}}-text">{{.Text}}</span>{{end}}</h2>
  <dl class="kv">
    <dt>Status</dt><dd class="{{.Class}}-text">{{.Status}}</dd>
    {{- if .Target}}<dt>Target</dt><dd>{{.Target}}</dd>{{end}}
    {{- range .Commands}}<dt>Command</dt><dd><code class="cmd">{{.Line}}</code>{{if and .Dir (ne .Dir ".")}} <span class="muted">in {{.Dir}}</span>{{end}}</dd>{{end}}
    {{- if .Duration}}<dt>Duration</dt><dd class="mono">{{.Duration}}</dd>{{end}}
    <dt>Exit code</dt><dd class="mono">{{.ExitCode}}</dd>
    {{- if .Started}}<dt>Started</dt><dd>{{.Started}}</dd>{{end}}
  </dl>
  {{- if .Error}}<p class="callout warn">{{.Error}}</p>{{end}}

  {{- if not .Structured}}
  <p class="note">The test command did not produce a structured report, so individual test results are not available. The outcome is based on the process exit code.</p>
  {{- else if not .Packages}}
  <p class="note">No tests were reported.</p>
  {{- else}}
  <div class="tlist">
    {{- range .Packages}}
    <div class="pkg">
      <div class="pkg-name mono">{{if .Name}}{{.Name}}{{else}}(no package){{end}}</div>
      {{- range .Outcomes}}
      <div class="trow outcome"><span class="glyph {{.Class}}-text">{{.Glyph}}</span><span class="tname">{{.Label}} <span class="tag">package result</span></span><span class="dur mono">{{.Duration}}</span></div>
      {{- end}}
      {{- range .Tests}}
      <div class="trow"><span class="glyph {{.Class}}-text" title="{{.Status}}">{{.Glyph}}</span><span class="tname mono">{{.Label}}</span><span class="dur mono">{{.Duration}}</span></div>
      {{- end}}
    </div>
    {{- end}}
  </div>
  {{- end}}

  {{- range .Failures}}
  <details class="failure" open>
    <summary><span class="bad-text">Failure</span> <span class="mono">{{.Label}}</span>{{if .Package}} <span class="muted mono">{{.Package}}</span>{{end}}</summary>
    <pre class="out">{{.Output}}</pre>
  </details>
  {{- end}}

  {{- if .LogShown}}
  <details class="log">
    <summary>Full log <span class="muted">{{plural .LogShown "line" "lines"}}{{if .LogHidden}}, last of {{.LogTotal}}{{end}}</span></summary>
    <pre class="out">{{.LogText}}</pre>
  </details>
  {{- end}}
  {{- else}}
  <h2>Tests</h2>
  <p class="note">Tests were not run in this session.</p>
  {{- end}}
</section>
{{- if .HasFlow}}

<section id="codeflow" class="flow-wrap" data-section="codeflow">
  <div class="flow-head">
    <h2>Code Flow{{if .FlowStats}} <span class="sub">{{.FlowStats}}</span>{{end}}</h2>
    <p class="muted small">Static analysis of the Go code at export time (go/packages, go/types, SSA). Edges are possible execution paths, not observed calls.</p>
  </div>
  {{- if .FlowError}}
  <p class="callout warn">Code Flow is not available in this report: {{.FlowError}}</p>
  {{- else}}
  <div id="flow-app" class="flow-app">
    <noscript><p class="note">The Code Flow graph needs JavaScript. The call index is embedded in this file as JSON (element #flow-data).</p></noscript>
  </div>
  <script type="application/json" id="flow-data">{{.FlowJSON}}</script>
  {{- end}}
</section>
{{- end}}
{{- if .HasInspect}}

<section id="inspect" class="insp-wrap" data-section="inspect">
  <div class="flow-head">
    <h2>Go Deep Inspector <span class="sub">{{.InspectInfo}}</span></h2>
    <p class="muted small">Static results (Code Flow, Data Flow, Branch Explorer, SQL Explorer) describe possible paths found when the report was generated. Execution traces and benchmarks are observations of the runs that produced them; profiles and query plans are estimates. Nothing runs in the browser.</p>
  </div>
  <div id="inspect-app" class="insp-app">
    <noscript><p class="note">The inspector needs JavaScript. Its data is embedded in this file as JSON (element #inspect-data).</p></noscript>
  </div>
  <script type="application/json" id="inspect-data">{{.InspectJSON}}</script>
</section>
{{- end}}
{{- if .HasStudy}}

<section id="study" class="study-wrap" data-section="study">
  <div class="flow-head">
    <h2>Study <span class="sub">{{.StudyInfo}}</span></h2>
    <p class="muted small">Tools to discover the change by yourself: where execution enters, who calls whom, which files take part. Everything is derived from the analyses above (call graph, CFG, data flow, coverage, traces, diff) and labelled with its source — STATIC, RUNTIME, COVERAGE, SQL, GIT or HEURISTIC. Nothing is generated text, and no AI is involved.</p>
  </div>
  <div id="study-app" class="study-app">
    <noscript><p class="note">Study needs JavaScript. Its data is embedded in this file as JSON (element #study-data).</p></noscript>
  </div>
  <script type="application/json" id="study-data">{{.StudyJSON}}</script>
</section>
{{- end}}
</main>

<footer class="wrap foot">Generated locally by GitView · <time datetime="{{.GeneratedTS}}">{{.Generated}}</time> · <span class="kbd-hint">Keys: <kbd>/</kbd> filter <kbd>j</kbd>/<kbd>k</kbd> next/previous file <kbd>o</kbd> toggle <kbd>e</kbd>/<kbd>c</kbd> expand/collapse all</span></footer>
<script>{{.JS}}</script>
<script>{{.FlowJS}}</script>
{{- if .HasInspect}}
<script>{{.InspectJS}}</script>
{{- end}}
{{- if .HasStudy}}
<script>{{.StudyJS}}</script>
{{- end}}
</body>
</html>
{{define "stat"}}<span class="ok-text">+{{.Added}}</span> <span class="bad-text">−{{.Deleted}}</span><span class="bar" aria-hidden="true">{{range .Bar}}<i class="b-{{.}}"></i>{{end}}</span>{{end}}`

const headJS = `document.documentElement.classList.add("js");if(location.hash==="#tests")document.documentElement.classList.add("view-tests");if(location.hash.indexOf("#codeflow")===0)document.documentElement.classList.add("view-flow");if(location.hash.indexOf("#inspect")===0)document.documentElement.classList.add("view-inspect");if(location.hash.indexOf("#study")===0)document.documentElement.classList.add("view-study");try{if(localStorage.getItem("gitview-theme")==="light")document.documentElement.setAttribute("data-theme","light")}catch(e){}`

const reportCSS = `
:root{
  --mono:ui-monospace,"SFMono-Regular","JetBrains Mono","Cascadia Code","Fira Code",Menlo,Consolas,"Liberation Mono",monospace;
  --sans:system-ui,-apple-system,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;
  --nav-h:44px;
}
:root[data-theme="dark"]{
  color-scheme:dark;
  --bg:#111317;--surface:#171a1f;--surface-2:#1e2228;--border:#2a2f37;--border-strong:#3a404a;
  --text:#e3e6ea;--muted:#9aa1ab;--faint:#6b727d;--accent:#7cacf8;
  --ok:#4ac26b;--bad:#f47067;--warn:#d4a72c;
  --add-line:rgba(46,160,67,.11);--add-num:rgba(46,160,67,.24);--add-fg:#7ee2a0;
  --del-line:rgba(248,81,73,.11);--del-num:rgba(248,81,73,.24);--del-fg:#ffa198;
  --hunk:rgba(124,172,248,.08);--hl:rgba(124,172,248,.14);
}
:root[data-theme="light"]{
  color-scheme:light;
  --bg:#ffffff;--surface:#f6f8fa;--surface-2:#eef1f4;--border:#d9dee3;--border-strong:#c4cbd3;
  --text:#1f2328;--muted:#59636e;--faint:#818b98;--accent:#0969da;
  --ok:#1a7f37;--bad:#cf222e;--warn:#9a6700;
  --add-line:rgba(26,127,55,.08);--add-num:rgba(26,127,55,.17);--add-fg:#116329;
  --del-line:rgba(207,34,46,.07);--del-num:rgba(207,34,46,.15);--del-fg:#a40e26;
  --hunk:rgba(9,105,218,.06);--hl:rgba(9,105,218,.10);
}
*{box-sizing:border-box}
html{scroll-padding-top:calc(var(--nav-h) + 12px)}
body{margin:0;background:var(--bg);color:var(--text);font:14px/1.5 var(--sans);-webkit-text-size-adjust:100%}
a{color:var(--accent);text-decoration:none}a:hover{text-decoration:underline}
code,kbd,pre,.mono{font-family:var(--mono)}
code{font-size:12.5px}
.wrap{max-width:1200px;margin:0 auto;padding:0 24px}
.files-wrap{padding:0 24px}
.grow{flex:1}
.muted,.muted-text{color:var(--muted)}.small{font-size:12px}
.ok-text{color:var(--ok)}.bad-text{color:var(--bad)}.warn-text{color:var(--warn)}
.sr{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0)}
.sep{color:var(--faint);margin:0 .45em}

/* Header */
.report-head{padding-top:28px;padding-bottom:14px}
.eyebrow{font-size:12.5px;color:var(--muted)}
h1{font-size:21px;font-weight:600;margin:2px 0 10px;letter-spacing:-.01em;overflow-wrap:anywhere}
.meta{display:flex;flex-wrap:wrap;gap:4px 22px;margin:0;font-size:12.5px}
.meta div{display:flex;gap:6px;min-width:0}
.meta dt{color:var(--muted)}.meta dd{margin:0;min-width:0;overflow-wrap:anywhere}
.meta .subject{color:var(--muted)}
.statline{margin:12px 0 0;font-size:13.5px;color:var(--muted)}
.statline b{color:var(--text);font-weight:600}
.statline .ok-text b{color:var(--ok)}.statline .bad-text b{color:var(--bad)}

/* Navigation */
.tabs{position:sticky;top:0;z-index:30;background:var(--bg);border-bottom:1px solid var(--border)}
.tabs-inner{display:flex;align-items:center;gap:22px;height:var(--nav-h)}
.tabs a{color:var(--muted);font-size:13.5px;height:var(--nav-h);display:flex;align-items:center;gap:6px;border-bottom:2px solid transparent;margin-bottom:-1px}
.tabs a:hover{color:var(--text);text-decoration:none}
.tabs a[aria-current="true"]{color:var(--text);border-bottom-color:var(--accent)}
.count{font-size:12px;line-height:18px;padding:0 7px;border-radius:9px;background:var(--surface-2);color:var(--muted)}
.count.ok{color:var(--ok)}.count.bad{color:var(--bad)}.count.warn{color:var(--warn)}

/* Buttons */
button,.ghost{font:inherit}
.ghost{background:transparent;border:1px solid var(--border);color:var(--muted);border-radius:6px;padding:3px 10px;font-size:12.5px;cursor:pointer;line-height:18px;display:inline-flex;align-items:center}
.ghost:hover{color:var(--text);border-color:var(--border-strong);text-decoration:none}
.ghost.small{padding:1px 7px;font-size:12px}
.ghost.done{color:var(--ok);border-color:var(--ok)}
:focus-visible{outline:2px solid var(--accent);outline-offset:1px}

/* Sections */
main section{padding:22px 0 8px}
h2{font-size:16px;font-weight:600;margin:0 0 12px}
h2 .sub{color:var(--muted);font-weight:400;margin-left:4px}
h3{font-size:14px;font-weight:600;margin:20px 0 8px}
.note{color:var(--muted);margin:10px 0}

/* Tests is a separate tab: with scripts on, only one view is shown. */
.js.view-tests #overview,.js.view-tests #files,.js.view-flow #overview,.js.view-flow #files,.js.view-flow #tests{display:none}
.js.view-inspect #overview,.js.view-inspect #files,.js.view-inspect #tests,.js.view-inspect #codeflow{display:none}
.js.view-study #overview,.js.view-study #files,.js.view-study #tests,.js.view-study #codeflow,.js.view-study #inspect{display:none}
.js:not(.view-inspect) #inspect{display:none}
.js:not(.view-study) #study{display:none}
.js:not(.view-tests) #tests{display:none}
.js:not(.view-flow) #codeflow{display:none}

/* Overview file list */
.flist{border:1px solid var(--border);border-radius:6px;overflow:hidden}
.frow{display:grid;grid-template-columns:22px minmax(0,1fr) auto;align-items:center;gap:10px;padding:6px 12px;color:var(--text);border-top:1px solid var(--border)}
.frow:first-child{border-top:0}
.frow:hover{background:var(--surface);text-decoration:none}
.frow[hidden]{display:none}
.fpath{min-width:0;display:flex;align-items:baseline;gap:10px;overflow:hidden;white-space:nowrap}
.fname{font-weight:600;overflow:hidden;text-overflow:ellipsis}
.fdir{color:var(--muted);font-size:12.5px;overflow:hidden;text-overflow:ellipsis;direction:rtl;text-align:left}
.fstat{font-family:var(--mono);font-size:12px;white-space:nowrap;display:inline-flex;align-items:center;gap:6px}
.bar{display:inline-flex;gap:1px;margin-left:2px}
.bar i{width:7px;height:7px;border-radius:1px;background:var(--border)}
.bar .b-add{background:var(--ok)}.bar .b-del{background:var(--bad)}
.st{font:600 11.5px/18px var(--mono);width:18px;text-align:center;border-radius:4px;flex:none}
.st-A{color:var(--ok)}.st-D{color:var(--bad)}.st-M{color:var(--accent)}.st-R{color:var(--muted)}
.warnings{list-style:none;padding:0;margin:0}
.warnings li{border-left:2px solid var(--warn);padding:2px 0 2px 10px;margin:4px 0;color:var(--muted)}

/* Files toolbar */
.toolbar{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:12px}
.toolbar h2{margin:0}
.filter{position:relative;display:flex;align-items:center}
.filter input{font:13px var(--sans);color:var(--text);background:var(--bg);border:1px solid var(--border);border-radius:6px;padding:4px 30px 4px 9px;width:240px}
.filter input:focus{border-color:var(--accent);outline:none}
.filter kbd{position:absolute;right:7px}
kbd{font-size:11px;color:var(--muted);border:1px solid var(--border);border-radius:4px;padding:0 4px;line-height:16px}

/* File diff */
.file{border:1px solid var(--border);border-radius:6px;margin:0 0 14px;background:var(--bg)}
.file[hidden]{display:none}
.fhead{position:sticky;top:var(--nav-h);z-index:20;display:flex;align-items:center;gap:8px;padding:6px 10px;background:var(--surface);border-radius:6px;cursor:pointer;list-style:none;font-size:13px;min-height:38px}
.file[open]>.fhead{border-bottom:1px solid var(--border);border-radius:6px 6px 0 0}
.fhead::-webkit-details-marker{display:none}
.chev{width:16px;height:16px;flex:none;position:relative}
.chev::before{content:"";position:absolute;left:5px;top:4px;width:5px;height:5px;border-right:1.5px solid var(--muted);border-bottom:1.5px solid var(--muted);transform:rotate(-45deg);transition:transform .12s}
.file[open] .chev::before{transform:rotate(45deg);left:4px;top:3px}
.hpath{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12.5px}
.hdir{color:var(--muted)}.hname{font-weight:600;color:var(--text)}
.tag{font-size:11.5px;color:var(--muted);border:1px solid var(--border);border-radius:9px;padding:0 7px;white-space:nowrap}
.actions{display:flex;gap:6px;flex:none}
.anchor{font-family:var(--mono)}
.file.current>.fhead{box-shadow:inset 2px 0 0 var(--accent)}
.diff{overflow-x:auto;border-radius:0 0 6px 6px}
.diff table{border-collapse:separate;border-spacing:0;width:100%;font:12.5px/20px var(--mono);tab-size:4;-moz-tab-size:4}
.diff col.c-ln{width:calc(var(--d) * 1ch + 20px)}
.diff col.c-sg{width:calc(1ch + 12px)}
.ln{color:var(--faint);text-align:right;padding:0 10px;user-select:none;-webkit-user-select:none;position:sticky;background:var(--bg);white-space:nowrap;vertical-align:top}
.ln.old{left:0}.ln.new{left:calc(var(--d) * 1ch + 20px);border-right:1px solid var(--border)}
.sg{padding:0 4px 0 8px;user-select:none;-webkit-user-select:none;color:var(--faint);vertical-align:top}
.cd{white-space:pre;padding:0 16px 0 2px;color:var(--text);vertical-align:top}
tr.add .ln{background:linear-gradient(var(--add-num),var(--add-num)),var(--bg);color:var(--add-fg)}
tr.del .ln{background:linear-gradient(var(--del-num),var(--del-num)),var(--bg);color:var(--del-fg)}
tr.add .sg{color:var(--add-fg);background:var(--add-line)}
tr.del .sg{color:var(--del-fg);background:var(--del-line)}
tr.add .cd{background:var(--add-line)}
tr.del .cd{background:var(--del-line)}
tr.empty .cd,tr.empty .sg{background:transparent}
tr.flash td{box-shadow:inset 0 0 0 9999px var(--hl)}
tr.hh td{background:linear-gradient(var(--hunk),var(--hunk)),var(--bg);color:var(--muted);padding-top:3px;padding-bottom:3px}
tr.hh .hcell{white-space:nowrap;padding-left:10px}
.range{color:var(--faint)}
.section{color:var(--muted)}
tbody.hunk+tbody.hunk tr.flash td{box-shadow:inset 0 0 0 9999px var(--hl)}
tr.hh td{border-top:1px solid var(--border)}
.copy-hunk{margin-left:10px;opacity:0}
tr.hh:hover .copy-hunk,.copy-hunk:focus-visible,.copy-hunk.done{opacity:1}
@media (hover:none){.copy-hunk{opacity:1}}
.file>.note{padding:4px 14px 12px;margin:10px 0 0}

/* Tests */
.kv{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:4px 18px;margin:0 0 14px;font-size:13px}
.kv dt{color:var(--muted)}.kv dd{margin:0;min-width:0;overflow-wrap:anywhere}
.cmd{background:var(--surface);border:1px solid var(--border);border-radius:4px;padding:1px 6px;overflow-wrap:anywhere}
.callout{border-left:2px solid var(--warn);padding:4px 10px;color:var(--muted);margin:10px 0}
.tlist{border:1px solid var(--border);border-radius:6px;overflow:hidden}
.pkg+.pkg{border-top:1px solid var(--border)}
.pkg-name{font-size:12px;color:var(--muted);padding:6px 12px;background:var(--surface);border-bottom:1px solid var(--border);overflow-wrap:anywhere}
.trow{display:grid;grid-template-columns:18px minmax(0,1fr) auto;gap:8px;padding:3px 12px;font-size:13px;align-items:baseline}
.trow+.trow{border-top:1px solid var(--surface-2)}
.glyph{font-weight:700;text-align:center}
.tname{overflow-wrap:anywhere}.trow .tname.mono{font-size:12.5px}
.dur{color:var(--faint);font-size:12px}
.outcome .tname{color:var(--text)}
.failure,.log{margin:12px 0;border:1px solid var(--border);border-radius:6px}
.failure>summary,.log>summary{cursor:pointer;padding:7px 12px;background:var(--surface);border-radius:6px;font-size:13px}
.failure[open]>summary,.log[open]>summary{border-bottom:1px solid var(--border);border-radius:6px 6px 0 0}
.failure{border-color:color-mix(in srgb,var(--bad) 40%,var(--border))}
pre.out{margin:0;padding:10px 12px;font-size:12.5px;line-height:19px;overflow:auto;max-height:520px;white-space:pre;tab-size:4}

.foot{color:var(--faint);font-size:12px;padding-top:28px;padding-bottom:36px}
.foot kbd{margin:0 1px}

/* Narrow screens: simplify metadata, keep code readable and scrollable. */
@media screen and (max-width:760px){
  .wrap,.files-wrap{padding:0 14px}
  .report-head{padding-top:18px}
  .meta .opt{display:none}
  .meta .subject{display:none}
  .tabs-inner{gap:16px}
  #theme-toggle{display:none}
  .frow{grid-template-columns:20px minmax(0,1fr);row-gap:0}
  .frow .fstat{grid-column:2}
  .fpath{flex-direction:column;gap:0}
  .bar{display:none}
  .toolbar .filter{order:5;flex-basis:100%}.filter input{width:100%}
  .fhead{flex-wrap:wrap;row-gap:2px}
  .fhead .grow{display:none}
  .hpath{flex-basis:calc(100% - 60px);white-space:normal;overflow-wrap:anywhere}
  .fhead .fstat{margin-left:24px}
  .actions .anchor{display:none}
  .kbd-hint{display:none}
  .kv{grid-template-columns:1fr;gap:0}.kv dd{margin-bottom:6px}
}

@media print{
  :root{--nav-h:0px}
  main section{display:block!important}
  .tabs,.toolbar .filter,#filter-count,.actions,.copy-hunk,[data-action],.kbd-hint{display:none!important}
  body{background:#fff}
  .fhead{position:static}
  .ln{position:static}
  .diff{overflow:visible}
  .cd,pre.out{white-space:pre-wrap;word-break:break-all}
  pre.out{max-height:none}
  tr{break-inside:avoid}
  .file,.failure,.log{break-inside:auto}
  a{color:inherit}
}
`

const reportJS = `(function(){
"use strict";
var root=document.documentElement;
var KEY="gitview-theme";
var themeBtn=document.getElementById("theme-toggle");
function setTheme(t){
  root.setAttribute("data-theme",t);
  if(themeBtn){themeBtn.textContent=t==="dark"?"Light theme":"Dark theme";themeBtn.setAttribute("aria-pressed",String(t==="light"));}
}
function saveTheme(t){try{localStorage.setItem(KEY,t);}catch(e){}}
setTheme(root.getAttribute("data-theme")==="light"?"light":"dark");

var files=Array.prototype.slice.call(document.querySelectorAll("details.file"));
var rows=Array.prototype.slice.call(document.querySelectorAll(".frow"));
var filter=document.getElementById("file-filter");
var countEl=document.getElementById("filter-count");
var emptyEl=document.getElementById("filter-empty");
var current=-1;

function visibleFiles(){return files.filter(function(f){return !f.hidden;});}
function setAll(open){visibleFiles().forEach(function(f){f.open=open;});}

function flash(btn,label){
  var old=btn.getAttribute("data-label")||btn.textContent;
  btn.setAttribute("data-label",old);
  btn.textContent=label;btn.classList.add("done");
  setTimeout(function(){btn.textContent=old;btn.classList.remove("done");},1400);
}
function copyText(text,btn){
  function fallback(){
    var ta=document.createElement("textarea");
    ta.value=text;ta.setAttribute("readonly","");ta.style.position="fixed";ta.style.opacity="0";
    document.body.appendChild(ta);ta.select();
    var ok=false;try{ok=document.execCommand("copy");}catch(e){}
    document.body.removeChild(ta);
    flash(btn,ok?"Copied":"Copy failed");
  }
  if(navigator.clipboard&&window.isSecureContext){
    navigator.clipboard.writeText(text).then(function(){flash(btn,"Copied");},fallback);
  }else{fallback();}
}
function hunkText(tbody){
  var out=[];
  var head=tbody.querySelector(".hh .hcell");
  if(head){out.push(head.querySelector(".range").textContent+(head.querySelector(".section")?" "+head.querySelector(".section").textContent:""));}
  tbody.querySelectorAll("tr.l").forEach(function(tr){out.push(tr.cells[2].textContent+tr.cells[3].textContent);});
  return out.join("\n")+"\n";
}

document.addEventListener("click",function(e){
  var el=e.target.closest("[data-action]");
  var inSummary=e.target.closest("summary");
  if(el){
    if(inSummary)e.preventDefault();
    switch(el.getAttribute("data-action")){
      case "theme":var t=root.getAttribute("data-theme")==="dark"?"light":"dark";setTheme(t);saveTheme(t);break;
      case "expand-all":setAll(true);break;
      case "collapse-all":setAll(false);break;
      case "copy-path":copyText(el.getAttribute("data-path"),el);break;
      case "copy-hunk":copyText(hunkText(el.closest("tbody")),el);break;
    }
    return;
  }
  var a=e.target.closest("a.anchor");
  if(a&&inSummary){e.preventDefault();history.replaceState(null,"",a.getAttribute("href"));openHash();}
});

function applyFilter(){
  var q=(filter.value||"").trim().toLowerCase();
  var n=0;
  files.forEach(function(f){var m=!q||f.getAttribute("data-path").toLowerCase().indexOf(q)>=0;f.hidden=!m;if(m)n++;});
  rows.forEach(function(r){r.hidden=!(!q||r.getAttribute("data-path").toLowerCase().indexOf(q)>=0);});
  countEl.textContent=q?n+" of "+files.length:"";
  emptyEl.hidden=!(q&&n===0);
}
if(filter){filter.addEventListener("input",applyFilter);}

function openHash(){
  var id=decodeURIComponent(location.hash.slice(1));
  if(!id)return;
  var el=document.getElementById(id);
  if(el&&el.tagName==="TR"){
    var det=el.closest("details.file");
    if(det){if(det.hidden){filter.value="";applyFilter();}det.open=true;current=files.indexOf(det);mark();}
    el.scrollIntoView({block:"center"});
    el.classList.add("flash");setTimeout(function(){el.classList.remove("flash");},1600);
    return;
  }
  if(el&&el.tagName==="DETAILS"){
    if(el.hidden){filter.value="";applyFilter();}
    el.open=true;
    current=files.indexOf(el);
    mark();
    el.scrollIntoView({block:"start"});
  }
}
function route(){
  var h=location.hash;
  var view=h==="#tests"?"tests":(h.indexOf("#codeflow")===0?"codeflow":(h.indexOf("#inspect")===0?"inspect":(h.indexOf("#study")===0?"study":"review")));
  var was=root.classList.contains("view-tests")?"tests":(root.classList.contains("view-flow")?"codeflow":(root.classList.contains("view-inspect")?"inspect":(root.classList.contains("view-study")?"study":"review")));
  root.classList.toggle("view-tests",view==="tests");
  root.classList.toggle("view-flow",view==="codeflow");
  root.classList.toggle("view-inspect",view==="inspect");
  root.classList.toggle("view-study",view==="study");
  if(view!=="review"){activate(view);if(was!==view)window.scrollTo(0,0);}
  else if(was!=="review"){activate(h==="#files"||/^#file-/.test(h)?"files":"overview");}
}
window.addEventListener("hashchange",function(){route();openHash();});

function mark(){files.forEach(function(f,i){f.classList.toggle("current",i===current);});}
function step(dir){
  var vis=visibleFiles();
  if(!vis.length)return;
  var pos=vis.indexOf(files[current]);
  pos=pos<0?(dir>0?0:vis.length-1):Math.min(Math.max(pos+dir,0),vis.length-1);
  current=files.indexOf(vis[pos]);
  mark();
  var s=vis[pos].querySelector("summary");
  s.focus({preventScroll:true});
  vis[pos].scrollIntoView({block:"start"});
}
document.addEventListener("keydown",function(e){
  var t=e.target;
  if(e.metaKey||e.ctrlKey||e.altKey)return;
  if(t&&(t.tagName==="INPUT"||t.tagName==="TEXTAREA"||t.isContentEditable)){
    if(e.key==="Escape"&&t===filter){filter.value="";applyFilter();filter.blur();}
    return;
  }
  switch(e.key){
    case "/":e.preventDefault();filter.focus();filter.select();break;
    case "j":step(1);break;
    case "k":step(-1);break;
    case "o":if(files[current]){files[current].open=!files[current].open;}break;
    case "e":setAll(true);break;
    case "c":setAll(false);break;
  }
});

var links=Array.prototype.slice.call(document.querySelectorAll(".tabs a[data-section]"));
function activate(name){links.forEach(function(l){l.setAttribute("aria-current",String(l.getAttribute("data-section")===name));});}
if("IntersectionObserver" in window){
  var io=new IntersectionObserver(function(entries){
    if(root.classList.contains("view-tests")||root.classList.contains("view-flow")||root.classList.contains("view-inspect")||root.classList.contains("view-study"))return;
    entries.forEach(function(en){if(en.isIntersecting)activate(en.target.getAttribute("data-section"));});
  },{rootMargin:"-45% 0px -50% 0px"});
  document.querySelectorAll("main section[data-section]").forEach(function(s){io.observe(s);});
}
activate("overview");
route();
openHash();

var printState=null;
window.addEventListener("beforeprint",function(){
  printState={theme:root.getAttribute("data-theme"),open:files.map(function(f){return f.open;})};
  setTheme("light");files.forEach(function(f){f.open=true;});
});
window.addEventListener("afterprint",function(){
  if(!printState)return;
  setTheme(printState.theme);files.forEach(function(f,i){f.open=printState.open[i];});printState=null;
});
})();`
