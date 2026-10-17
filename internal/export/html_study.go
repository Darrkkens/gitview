package export

// Study Mode tab of the HTML report: a learning layer over the data the
// report already embeds (Code Flow index, Deep Inspector results) plus the
// Study model (#study-data). Like the other scripts it only inserts text
// (textContent) and is pinned by its hash in the CSP; progress is kept in
// localStorage when available.

const studyCSS = `
.study-wrap{max-width:1280px;margin:0 auto;padding:18px 24px 40px}
.study-nav{display:flex;flex-wrap:wrap;gap:6px;margin:10px 0 16px}
.study-nav button{background:var(--panel);border:1px solid var(--border);color:var(--muted);border-radius:16px;padding:4px 12px;font:inherit;font-size:13px;cursor:pointer}
.study-nav button.on{color:var(--text);border-color:var(--accent);background:var(--sel,var(--panel))}
.study-sum{color:var(--muted);font-size:13px;margin:4px 0 0}
.sb{display:inline-block;font:600 10.5px var(--mono);letter-spacing:.02em;border-radius:4px;padding:1px 5px;margin:0 3px;border:1px solid currentColor;vertical-align:1px;white-space:nowrap}
.sb-static{color:#5b9cf5}.sb-runtime{color:#3fb37f}.sb-coverage{color:#2fb1b8}.sb-sql{color:#c78c2b}.sb-git{color:#b07cd8}.sb-heuristic{color:#d06a6a}
.sb-layer{color:var(--text);border-color:var(--border);background:var(--panel)}
.st-card{border:1px solid var(--border);border-radius:8px;background:var(--panel);padding:10px 12px;margin:8px 0}
.st-card.changed{border-left:3px solid var(--accent)}
.st-head{display:flex;align-items:baseline;gap:8px;flex-wrap:wrap}
.st-n{font:600 13px var(--mono);color:var(--muted);min-width:24px}
.st-title{font:600 13px var(--mono);word-break:break-all}
.st-meta{color:var(--muted);font-size:12px}
.st-why{margin:6px 0 0 32px;padding:0;list-style:none;font-size:12.5px}
.st-why li{margin:2px 0}
.st-actions{margin:8px 0 0 32px;display:flex;flex-wrap:wrap;gap:6px}
.st-actions a,.st-actions button{font-size:12px}
.st-actions .off{opacity:.45;pointer-events:none}
.st-code{margin:8px 0 0 32px}
.chain{display:flex;flex-direction:column;align-items:flex-start;gap:0;margin:6px 0 14px}
.chain .cs{border:1px solid var(--border);border-radius:6px;padding:5px 9px;background:var(--panel);font-size:12.5px;max-width:100%}
.chain .cs b{font-family:var(--mono);font-weight:600;word-break:break-all}
.chain .arrow{color:var(--muted);padding:1px 0 1px 18px;font-size:12px}
.st-table{border-collapse:collapse;width:100%;font-size:12.5px}
.st-table th,.st-table td{border-bottom:1px solid var(--border);padding:4px 8px;text-align:left;vertical-align:top}
.st-table th{color:var(--muted);font-weight:600}
.st-table td.mono,.st-table .mono{font-family:var(--mono)}
.st-group{margin:14px 0}
.st-group h3{font-size:14px;margin:0 0 6px}
.st-chip{display:inline-block;font:12px var(--mono);border:1px solid var(--border);border-radius:10px;padding:0 7px;margin:2px 4px 2px 0}
.st-note{color:var(--muted);font-size:12.5px}
.st-graph{height:360px;border:1px solid var(--border);border-radius:8px;margin:8px 0}
.st-graph .insp-canvas{height:100%}
.st-legend{font-size:12px;color:var(--muted)}
.jr{display:flex;flex-direction:column;align-items:flex-start;margin:6px 0 4px}
.jr .js{border:1px solid var(--border);border-radius:6px;padding:4px 9px;background:var(--panel);font-size:12.5px;max-width:100%}
.jr .js b{font-family:var(--mono);font-weight:600;word-break:break-all}
.jr .jv{color:var(--muted);font-size:11.5px;padding:1px 0 1px 18px}
.role{display:inline-block;font:600 10px var(--mono);border-radius:3px;padding:0 4px;margin-right:5px}
.role-SOURCE{background:#2f6f4f;color:#fff}.role-TRANSFORMATION{background:var(--border);color:var(--text)}.role-DECISION{background:#8a6d1e;color:#fff}.role-SINK{background:#6d3b8a;color:#fff}
.dec-cond{font:600 13px var(--mono);background:var(--panel);border:1px solid var(--border);border-radius:6px;padding:4px 8px;display:inline-block;margin:4px 0}
.cov-OBSERVED{color:#3fb37f;font-weight:600}.cov-NOT{color:#d06a6a;font-weight:600}.cov-NO{color:var(--muted)}
.upath{font:12px var(--mono);margin:4px 0 10px 0;padding:6px 10px;border-left:2px solid #d06a6a;background:var(--panel)}
.pr-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}
.pr-grid h3{font-size:13px;margin:6px 0}
.pr-list{list-style:none;margin:0;padding:0}
.pr-list li{display:flex;align-items:center;gap:6px;border:1px solid var(--border);border-radius:6px;padding:3px 8px;margin:3px 0;background:var(--panel);font-size:12.5px}
.pr-list li .nm{font-family:var(--mono);flex:1;word-break:break-all}
.pr-list li.used{opacity:.45}
.pr-list li.st-meta{border-style:dashed}
.pr-ok{color:#3fb37f;font-weight:600}.pr-bad{color:#d06a6a;font-weight:600}.pr-warn{color:#c78c2b;font-weight:600}
.pr-sum{font:600 12.5px var(--mono);margin:10px 0 6px}
.ex-card .ex-q{font-size:13.5px;margin:4px 0}
.ex-code{font:12.5px var(--mono);background:var(--bg,var(--panel));border:1px solid var(--border);border-radius:6px;padding:5px 9px;margin:6px 0;white-space:pre-wrap;word-break:break-all}
.ex-opts{display:flex;flex-direction:column;gap:4px;margin:8px 0 0}
.ex-opts button{text-align:left;font:12.5px var(--mono);background:var(--panel);border:1px solid var(--border);border-radius:6px;padding:4px 9px;color:var(--text);cursor:pointer;word-break:break-all}
.ex-opts button.right{border-color:#3fb37f;box-shadow:inset 3px 0 0 #3fb37f}.ex-opts button.wrong{border-color:#d06a6a;box-shadow:inset 3px 0 0 #d06a6a}
.ex-opts button:disabled{cursor:default}
@media screen and (max-width:760px){.pr-grid{grid-template-columns:1fr}}
.sq-sql{font:12.5px var(--mono);white-space:pre-wrap;word-break:break-word;background:var(--bg,var(--panel));border:1px solid var(--border);border-radius:6px;padding:6px 10px;margin:6px 0}
.sq-kv{border-collapse:collapse;font-size:12.5px;margin:6px 0;width:100%}
.sq-kv th{color:var(--muted);font-weight:600;text-align:left;vertical-align:top;padding:3px 10px 3px 0;white-space:nowrap;width:120px}
.sq-kv td{padding:3px 0;vertical-align:top}
.sq-kv td div{font-family:var(--mono)}
.st-sec{border-left:3px solid var(--border);padding-left:12px;margin:16px 0}
.st-sec.obs{border-left-color:#3fb37f}.st-sec.sta{border-left-color:#5b9cf5}
.st-sec>h3{display:flex;align-items:center;gap:6px}
.st-table td.num{text-align:right;font-family:var(--mono);white-space:nowrap}
.st-filter{display:flex;gap:10px;align-items:center;font-size:12.5px;color:var(--muted);margin:6px 0}
.ex-item{border:1px solid var(--border);border-radius:6px;padding:6px 10px;margin:6px 0;background:var(--panel)}
.ex-item label{display:flex;gap:8px;align-items:flex-start;font-size:13px;cursor:pointer}
.ex-item .facts{margin:6px 0 0 26px;padding:0;list-style:none;font:12px var(--mono)}
.ex-item .facts li{margin:2px 0;word-break:break-word}
.ex-note{width:100%;min-height:70px;font:12.5px var(--mono);background:var(--panel);color:var(--text);border:1px solid var(--border);border-radius:6px;padding:6px;box-sizing:border-box}
.pg-bar{height:6px;background:var(--border);border-radius:3px;overflow:hidden;min-width:120px}
.pg-bar span{display:block;height:100%;background:var(--accent)}
@media screen and (max-width:760px){.study-wrap{padding:14px 10px}.st-why,.st-actions,.st-code{margin-left:0}}
`

const studyJS = `(function(){
"use strict";
var dataEl=document.getElementById("study-data"),host=document.getElementById("study-app");
if(!dataEl||!host)return;
var M;
try{M=JSON.parse(dataEl.textContent);}catch(e){host.textContent="The Study data could not be read.";return;}
var FLOW=null,ANCH={};
try{var fe=document.getElementById("flow-data");if(fe&&fe.textContent){FLOW=JSON.parse(fe.textContent);ANCH=FLOW.anchors||{};}}catch(e){FLOW=null;}
var NODES={};if(FLOW&&FLOW.nodes)FLOW.nodes.forEach(function(n){NODES[n.id]=n;});
var GI=window.GitViewInspect||null;
function h(tag,attrs,text){var el=document.createElement(tag);if(attrs)for(var k in attrs){if(attrs[k]==null)continue;if(k==="class")el.className=attrs[k];else el.setAttribute(k,attrs[k]);}if(text!=null)el.textContent=String(text);return el;}
function add(p){for(var i=1;i<arguments.length;i++){var c=arguments[i];if(c)p.appendChild(typeof c==="string"?document.createTextNode(c):c);}return p;}
function loc(f,l){return f?(l?f+":"+l:f):"";}
var BASIS_HELP={STATIC:"Static analysis (SSA call graph, CFG, types): a possible path, not an observed one",RUNTIME:"Observed when the program ran (trace, profile, test run)",COVERAGE:"Observed by a coverage profile",SQL:"Read from SQL text",GIT:"From the working-tree diff",HEURISTIC:"Inferred from names, paths or extensions"};
function badge(b){var e=h("span",{class:"sb sb-"+String(b||"").toLowerCase(),title:BASIS_HELP[b]||""},b);return e;}
function layerBadge(l){return h("span",{class:"sb sb-layer"},l);}
// Progress (localStorage; disabled when unavailable).
var PKEY="gitview-study:"+(M.fingerprint||"");
var P={};try{P=JSON.parse(localStorage.getItem(PKEY)||"{}")||{};}catch(e){P={};}
function track(cat,id){if(!cat||!id)return;P[cat]=P[cat]||{};if(P[cat][id])return;P[cat][id]=1;try{localStorage.setItem(PKEY,JSON.stringify(P));}catch(e){}}
// Navigation targets.
function href(ref){
  if(!ref)return null;var i=ref.indexOf(":");var kind=ref.slice(0,i),rest=ref.slice(i+1);
  switch(kind){
  case "codeflow":return NODES[rest]?"#codeflow:"+encodeURIComponent(rest):null;
  case "inspect":var j=rest.lastIndexOf(":");var sym=rest.slice(0,j),tab=rest.slice(j+1);return GI&&GI.symbolIndex(sym)>=0?"#inspect:"+encodeURIComponent(sym)+":"+tab:null;
  case "diff":var k=rest.lastIndexOf(":");var file=rest.slice(0,k),line=+rest.slice(k+1);var fid=ANCH[file];if(!fid)return null;if(line>0&&document.getElementById(fid+"-L"+line))return "#"+fid+"-L"+line;return "#"+fid;
  }
  return null;
}
function evidenceList(evs){var ul=h("ul",{class:"st-why"});(evs||[]).forEach(function(ev){var li=h("li");add(li,badge(ev.basis),document.createTextNode(" "+ev.text+" "));var t=loc(ev.file,ev.line);var u=href(ev.ref);if(t){if(u){var a=h("a",{href:u,class:"mono"},t);li.appendChild(a);}else li.appendChild(h("span",{class:"mono st-meta"},t));}else if(u){li.appendChild(h("a",{href:u},"open"));}ul.appendChild(li);});return ul;}
function link(label,u,cat,id){if(!u)return h("span",{class:"ghost small off"},label);var a=h("a",{href:u,class:"ghost small"},label);a.addEventListener("click",function(){track(cat,id);});return a;}
// Source of a function: the inspector's copy, else the Code Flow snippet.
function sourceOf(id){var b=GI&&GI.bundle(id);if(b&&b.code)return {code:b.code,line:b.symbol.line};var n=NODES[id];if(n&&n.code)return {code:n.code,line:n.line};return null;}
function codeBox(id,mark){var src=sourceOf(id);if(!src)return h("p",{class:"st-note"},"Source not embedded in this report for this function.");if(GI&&GI.codeLines)return GI.codeLines(src.code,src.line,mark?[mark]:[]);var pre=h("pre",{class:"insp-pre"},src.code);return pre;}
function actions(id,file,line,cat){
  var box=h("div",{class:"st-actions"});
  var src=h("button",{type:"button",class:"ghost small"},"Open Source");var shown=null;
  var hasSrc=!!sourceOf(id)||!!href("diff:"+file+":"+(line||0));
  if(!hasSrc)src.className+=" off";
  src.addEventListener("click",function(){track(cat,id);if(sourceOf(id)){if(shown){shown.remove();shown=null;return;}shown=h("div",{class:"st-code"});shown.appendChild(codeBox(id,line));box.parentNode.insertBefore(shown,box.nextSibling);}else{var u=href("diff:"+file+":"+(line||0));if(u)location.hash=u;}});
  add(box,src,link("Open Code Flow",href("codeflow:"+id),cat,id),link("Open Data Flow",href("inspect:"+id+":dataflow"),cat,id),link("Open Branch Explorer",href("inspect:"+id+":branches"),cat,id));
  return box;
}
var views=[],cur=0;
function view(name,key,fn){views.push({name:name,key:key,fn:fn});}
var nav=h("nav",{class:"study-nav","aria-label":"Study sections"}),body=h("div");
var head=h("div");
add(head,h("p",{class:"study-sum"},summaryText()));
add(host,head,nav,body);
function summaryText(){var s=M.summary||{};var parts=[(s.functions||0)+" functions",(s.routes||0)+" routes",(s.changedFiles||0)+" changed files",(s.changedFunctions||0)+" changed functions"];parts.push(s.hasCoverage?"coverage: "+(s.coverageProfile||"loaded"):"no coverage profile");parts.push(s.hasTrace?"execution trace loaded":"no execution trace");return parts.join(" · ")+". Every item shows where it comes from; nothing here is generated text.";}
function show(i,user){cur=i;body.textContent="";Array.prototype.forEach.call(nav.children,function(b,k){b.className=k===i?"on":"";});views[i].fn(body);if(user){try{history.replaceState(null,"","#study:"+views[i].key);}catch(e){}}}

// --- Start Here -----------------------------------------------------------------
view("Start Here","start",function(el){
  add(el,h("h3",null,"Recommended reading order"));
  add(el,h("p",{class:"st-note"},"Entry points that lead to the change first, then the changed functions, their data access, changed SQL, the tests that reach the change and the other changed files. The order is computed from the call graph and the diff: the same change always gives the same order."));
  if(!M.readingOrder||!M.readingOrder.length){el.appendChild(h("p",{class:"st-note"},"No reading order: no changed Go code was found in this report."));return;}
  M.readingOrder.forEach(function(s){
    var card=h("div",{class:"st-card"+(s.changed?" changed":"")});
    var hd=h("div",{class:"st-head"});
    add(hd,h("span",{class:"st-n"},s.n+"."),layerBadge(s.layer),h("span",{class:"st-title"},s.title));
    if(s.changed)hd.appendChild(h("span",{class:"tag"},"changed"));
    var where=loc(s.file,s.line);if(where)hd.appendChild(h("span",{class:"st-meta mono"},where));
    card.appendChild(hd);
    if(s.detail&&s.detail!==s.file)card.appendChild(h("div",{class:"st-meta mono",style:"margin-left:32px"},s.detail));
    card.appendChild(evidenceList(s.why));
    if(s.kind==="function"||s.kind==="route")card.appendChild(actions(s.id,s.file,s.line,"codeflow"));
    else{var box=h("div",{class:"st-actions"});box.appendChild(link("Open in diff",href("diff:"+s.file+":"+(s.line||0)),"files",s.file));card.appendChild(box);}
    el.appendChild(card);
  });
});

// --- Architecture ---------------------------------------------------------------
view("Architecture","architecture",function(el){
  var A=M.architecture||{};
  add(el,h("h3",null,"Paths from the routes to the data"));
  add(el,h("p",{class:"st-note"},"Each path is the shortest chain of calls in the static call graph from a route's handler to a database operation (or, failing that, an outbound HTTP call). Layers come from structure (STATIC) or, when no structural signal exists, from names and paths (HEURISTIC)."));
  if(!(A.chains||[]).length)el.appendChild(h("p",{class:"st-note"},"No HTTP route was found in the call graph."));
  (A.chains||[]).forEach(function(c){
    var g=h("div",{class:"st-group"});add(g,h("h3",{class:"mono"},c.title));
    var ch=h("div",{class:"chain"});
    c.steps.forEach(function(s,i){
      if(i>0){var ar=h("div",{class:"arrow"},"↓ "+(s.via?s.via.text+" "+loc(s.via.file,s.via.line):""));ch.appendChild(ar);}
      var box=h("div",{class:"cs"});add(box,layerBadge(s.layer),badge(s.basis)," ");
      var u=href("codeflow:"+s.id);if(u){var a=h("a",{href:u});a.appendChild(h("b",null,s.name));a.addEventListener("click",function(){track("architecture",c.route);});box.appendChild(a);}else box.appendChild(h("b",null,s.name));
      var w=loc(s.file,s.line);if(w)box.appendChild(h("span",{class:"st-meta mono"}," "+w));
      ch.appendChild(box);
    });
    g.appendChild(ch);if(c.note)g.appendChild(h("p",{class:"st-note"},c.note));
    el.appendChild(g);
  });
  // Layer graph: the inspector's renderer.
  var edges=A.layerEdges||[];
  if(edges.length&&GI&&GI.Graph){
    add(el,h("h3",null,"Calls between layers"));
    var present={};edges.forEach(function(e){present[e.from]=1;present[e.to]=1;});
    var order=M.layers||[];var nodes=[],rank={};
    order.forEach(function(l,i){if(present[l]){nodes.push({id:l,lines:[{t:l,b:true}],cls:"fn"});rank[l]=i;}});
    var box=h("div",{class:"st-graph"});el.appendChild(box);
    var gr=new GI.Graph(box,{label:"Calls between layers"});
    gr.draw({nodes:nodes,edges:edges.map(function(e){return {from:e.from,to:e.to,label:e.count+" call"+(e.count>1?"s":""),title:e.example.text+" ("+loc(e.example.file,e.example.line)+")"};}),rank:rank,dir:"TB"},function(id){track("architecture","layer:"+id);});
    setTimeout(function(){gr.fit();},0);
  }
  add(el,h("h3",null,"Packages"));
  var t=h("table",{class:"st-table"}),tb=h("tbody");
  add(t,add(h("thead"),add(h("tr"),h("th",null,"Package"),h("th",null,"Layer"),h("th",null,"Why"),h("th",null,"Functions"))));
  (A.packages||[]).forEach(function(p){
    var tr=h("tr");var why=h("td");why.appendChild(evidenceList(p.evidence));
    add(tr,h("td",{class:"mono"},p.path),add(h("td"),layerBadge(p.layer),badge(p.basis)),why,h("td",null,p.functions));
    tb.appendChild(tr);
  });
  add(t,tb);el.appendChild(t);
  var fns=(A.functions||[]).filter(function(f){return f.basis==="STATIC";});
  if(fns.length){
    add(el,h("h3",null,"Functions classified by their structure"));
    var t2=h("table",{class:"st-table"}),tb2=h("tbody");
    add(t2,add(h("thead"),add(h("tr"),h("th",null,"Function"),h("th",null,"Layer"),h("th",null,"Evidence"))));
    fns.forEach(function(f){var tr=h("tr");var name=h("td",{class:"mono"});var u=href("codeflow:"+f.id);if(u){var a=h("a",{href:u},f.name);a.addEventListener("click",function(){track("architecture",f.id);});name.appendChild(a);}else name.textContent=f.name;var ev=h("td");ev.appendChild(evidenceList(f.evidence));add(tr,name,add(h("td"),layerBadge(f.layer),badge(f.basis)),ev);tb2.appendChild(tr);});
    add(t2,tb2);el.appendChild(t2);
  }
  (A.notes||[]).forEach(function(n){el.appendChild(h("p",{class:"st-note"},n));});
});

// --- Change Journey -------------------------------------------------------------
view("Change Journey","changes",function(el){
  add(el,h("h3",null,"The change, layer by layer"));
  add(el,h("p",{class:"st-note"},"Files of the working-tree diff grouped by the layer their changed functions belong to (Go), or by name and extension (other files), with the calls that connect them."));
  if(!(M.changes||[]).length){el.appendChild(h("p",{class:"st-note"},"No changed files in this report."));return;}
  M.changes.forEach(function(g){
    var sec=h("div",{class:"st-group"});add(sec,h("h3",null,g.layer));
    g.files.forEach(function(f){
      var card=h("div",{class:"st-card changed"});var hd=h("div",{class:"st-head"});
      var u=href("diff:"+f.path+":0");var title=u?h("a",{href:u,class:"st-title"},f.path):h("span",{class:"st-title"},f.path);
      if(u)title.addEventListener("click",function(){track("files",f.path);});
      add(hd,title,h("span",{class:"ok-text mono"},"+"+f.added),h("span",{class:"bad-text mono"},"−"+f.deleted),badge(f.basis));
      card.appendChild(hd);card.appendChild(evidenceList(f.evidence));
      if((f.symbols||[]).length){var syms=h("div",{style:"margin:6px 0 0 32px"});add(syms,h("span",{class:"st-meta"},"changed functions: "));f.symbols.forEach(function(s){var c=h("span",{class:"st-chip"});var su=href("codeflow:"+s.id);if(su){var a=h("a",{href:su},s.name);c.appendChild(a);}else c.textContent=s.name;syms.appendChild(c);});card.appendChild(syms);}
      if((f.related||[]).length){var rl=h("ul",{class:"st-why"});f.related.forEach(function(r){var li=h("li");add(li,badge("STATIC")," "+r.dir+" ",h("span",{class:"mono"},r.toName)," ",layerBadge(r.layer));if(r.changed)li.appendChild(h("span",{class:"tag"},"changed"));var w=loc(r.evidence.file,r.evidence.line);var ru=href(r.evidence.ref);if(w)li.appendChild(ru?h("a",{href:ru,class:"mono"}," "+w):h("span",{class:"st-meta mono"}," "+w));rl.appendChild(li);});card.appendChild(rl);}
      sec.appendChild(card);
    });
    el.appendChild(sec);
  });
});

// --- Data Journeys --------------------------------------------------------------
view("Data Journeys","data",function(el){
  add(el,h("h3",null,"Where the data comes from and where it goes"));
  add(el,h("p",{class:"st-note"},"Each journey follows one variable of a function through the Data Flow analysis (SSA def-use, across calls), from where the value enters to where it ends. SOURCE: input (HTTP request, database row, parameter, constant). TRANSFORMATION: assignments, calls, fields. DECISION: a condition that tests it. SINK: SQL, a call outside the module, a return. Only relations the analysis proved are shown."));
  var J=M.dataJourneys||[];
  if(!J.length){el.appendChild(h("p",{class:"st-note"},"No data journey: the report has no Data Flow results for the functions of the reading order."));return;}
  var cur="";
  J.forEach(function(j){
    if(j.funcName!==cur){cur=j.funcName;el.appendChild(h("h3",{class:"mono"},j.funcName));}
    var card=h("div",{class:"st-card"});var hd=h("div",{class:"st-head"});
    add(hd,h("span",{class:"st-title"},j.variable),badge("STATIC"),h("span",{class:"tag"},j.complete?"source → sink":"partial path"));
    card.appendChild(hd);
    var ch=h("div",{class:"jr"});
    j.steps.forEach(function(st,i){
      if(i>0)ch.appendChild(h("div",{class:"jv"},"↓ "+st.via+(st.viaText?" ("+st.viaText+")":"")+(st.conf&&st.conf!=="exact"?" · "+st.conf:"")));
      var box=h("div",{class:"js"});add(box,h("span",{class:"role role-"+st.role},st.role),h("b",null,st.label));
      var meta=[];if(st.var&&st.var!==st.label)meta.push(st.var);if(st.type)meta.push(st.type);if(st.funcName)meta.push("in "+st.funcName);
      if(meta.length)box.appendChild(h("span",{class:"st-meta"}," · "+meta.join(" · ")));
      var w=loc(st.file,st.line);if(w){var u=href("diff:"+st.file+":"+st.line);box.appendChild(u?h("a",{href:u,class:"mono"}," "+w):h("span",{class:"st-meta mono"}," "+w));}
      if(st.code)box.appendChild(h("div",{class:"st-meta mono"},st.code.trim()));
      ch.appendChild(box);
    });
    card.appendChild(ch);
    (j.partial||[]).forEach(function(p){card.appendChild(h("p",{class:"st-note"},"Partial: "+p));});
    var acts=h("div",{class:"st-actions"});acts.appendChild(link("Open Data Flow",href(j.evidence.ref),"dataflow",j.func+"|"+j.variable));
    var b=h("button",{type:"button",class:"ghost small"},"I followed this journey");b.addEventListener("click",function(){track("dataflow",j.func+"|"+j.variable);b.textContent="Followed ✓";});acts.appendChild(b);
    card.appendChild(acts);el.appendChild(card);
  });
});

// --- Decisions ------------------------------------------------------------------
view("Decisions","decisions",function(el){
  add(el,h("h3",null,"Decisions in the code"));
  add(el,h("p",{class:"st-note"},"Every decision point of the control-flow graph (Branch Explorer) of the functions in the report: the condition as written, and for each outcome the statement that follows, how the block ends, whether it is an error path and what it assigns. This is structure, not intent: the code does not say why a rule exists."));
  var F=M.decisions||[];
  if(!F.length){el.appendChild(h("p",{class:"st-note"},"No decision found in the functions of the report."));return;}
  F.forEach(function(f){
    var sec=h("div",{class:"st-group"});var hd=h("h3",null);add(hd,h("span",{class:"mono"},f.funcName),h("span",{class:"st-meta"}," · cyclomatic complexity "+f.cyclomatic+(f.coverage?" · coverage: "+f.coverage:"")));sec.appendChild(hd);
    f.decisions.forEach(function(d){
      var card=h("div",{class:"st-card"});var top=h("div",{class:"st-head"});
      add(top,h("span",{class:"tag"},d.kind),d.errorCheck?h("span",{class:"tag"},"error check"):null,badge("STATIC"));
      var w=loc(d.file,d.line);var u=href("diff:"+d.file+":"+d.line);top.appendChild(u?h("a",{href:u,class:"mono"},w):h("span",{class:"st-meta mono"},w));
      card.appendChild(top);card.appendChild(h("div",{class:"dec-cond"},d.cond||"(no condition)"));
      var t=h("table",{class:"st-table"}),tb=h("tbody");
      add(t,add(h("thead"),add(h("tr"),h("th",null,"Outcome"),h("th",null,"Then"),h("th",null,"Ends with"),h("th",null,"Assigns"),h("th",null,"Coverage"))));
      d.outcomes.forEach(function(o){
        var then=o.first?o.first:"next block (B"+o.to+")";
        var end=o.terminal?o.terminal+(o.returnsError?" (error)":""):(o.jump||"continues");
        var cov=h("td");if(o.coverage&&o.coverage!=="unknown"){add(cov,badge("COVERAGE")," "+o.coverage);}else cov.appendChild(h("span",{class:"st-meta"},f.coverage?"unknown":"no coverage data"));
        var tr=h("tr");add(tr,h("td",{class:"mono"},o.label.toUpperCase()+(o.errorPath?" · error path":"")),h("td",{class:"mono"},then+(o.firstLine?"  :"+o.firstLine:"")),h("td",null,end),h("td",{class:"mono"},(o.assigns||[]).join(", ")),cov);tb.appendChild(tr);
      });
      add(t,tb);card.appendChild(t);
      if((d.preds||[]).length)card.appendChild(h("p",{class:"st-note"},"Reached from block"+(d.preds.length>1?"s":"")+" B"+d.preds.join(", B")+" · decision block B"+d.block));
      var acts=h("div",{class:"st-actions"});acts.appendChild(link("Open Branch Explorer",href(d.evidence.ref),"branches",f.func+"|"+d.block));
      var b=h("button",{type:"button",class:"ghost small"},"Studied");b.addEventListener("click",function(){track("branches",f.func+"|"+d.block);b.textContent="Studied ✓";});acts.appendChild(b);
      card.appendChild(acts);sec.appendChild(card);
    });
    el.appendChild(sec);
  });
});

// --- Tests ----------------------------------------------------------------------
view("Tests","tests",function(el){
  add(el,h("h3",null,"Branches and tests"));
  add(el,add(h("p",{class:"st-note"}),"Two different kinds of facts, never mixed: which tests ",badge("STATIC")," reach a function (read from the test source and the call graph; ",badge("HEURISTIC")," when only a method name matches), and which outcomes ",badge("COVERAGE")," a coverage profile shows ran. The profile covers a whole test run, so it does not say which test took which branch."));
  var F=M.testMatrix||[];
  if(!F.length){el.appendChild(h("p",{class:"st-note"},"No function of the report has decisions or tests reaching it."));return;}
  F.forEach(function(f){
    var card=h("div",{class:"st-card"});var hd=h("div",{class:"st-head"});
    add(hd,h("span",{class:"st-title"},f.funcName),h("span",{class:"st-meta"},"cyclomatic complexity "+f.cyclomatic));
    if(f.coverage){add(hd,badge("COVERAGE"),h("span",{class:"st-meta"},f.coverage+(f.stale?" (older than the code: not used)":"")));}
    card.appendChild(hd);
    if(!f.coverage)card.appendChild(h("p",{class:"st-note"},"No coverage profile in this report: run the package tests with coverage (Branch Explorer, key c, in the TUI) or pass --cover cover.out to gitview inspect report / gitview study."));
    var tl=h("ul",{class:"st-why"});
    if(!(f.tests||[]).length)tl.appendChild(h("li",{class:"st-meta"},"No test reaches this function in the call graph."));
    (f.tests||[]).forEach(function(t){var li=h("li");add(li,badge(t.basis)," ",h("span",{class:"mono"},t.name)," "+(t.hops?"reaches it in "+t.hops+" call(s) from "+t.via:"calls it directly: "+t.via)+" ",h("span",{class:"st-meta mono"},loc(t.file,t.line)));if(t.status){add(li," ",badge("RUNTIME")," last run: "+t.status);}tl.appendChild(li);});
    add(card,h("div",{class:"st-meta",style:"margin:6px 0 0 32px"},"Tests that reach this function"),tl);
    if((f.rows||[]).length){
      var t=h("table",{class:"st-table"}),tb=h("tbody");
      add(t,add(h("thead"),add(h("tr"),h("th",null,"Scenario"),h("th",null,"Status"),h("th",null,"Executions"),h("th",null,"Line"))));
      f.rows.forEach(function(r){var st=h("td");if(r.status==="NO DATA")st.appendChild(h("span",{class:"cov-NO"},"no data"));else add(st,badge("COVERAGE")," ",h("span",{class:r.status==="OBSERVED"?"cov-OBSERVED":"cov-NOT"},r.status));var u=href("diff:"+r.file+":"+r.line);var ln=h("td");ln.appendChild(u?h("a",{href:u,class:"mono"},loc(r.file,r.line)):h("span",{class:"mono st-meta"},loc(r.file,r.line)));add(tb,add(h("tr"),h("td",{class:"mono"},r.scenario),st,h("td",null,r.count?String(r.count):""),ln));});
      add(t,tb);card.appendChild(t);
      card.appendChild(h("p",{class:"st-note"},"Branches: "+f.rows.length+" · observed: "+f.observed+" · not observed: "+f.notObserved+(f.noData?" · no data: "+f.noData:"")));
    }
    if((f.unobserved||[]).length){
      var btn=h("button",{type:"button",class:"ghost small"},"Show unobserved branches ("+f.unobserved.length+")");var box=h("div");box.hidden=true;
      btn.addEventListener("click",function(){box.hidden=!box.hidden;track("tests",f.func);});
      f.unobserved.forEach(function(u){add(box,h("div",{class:"st-meta mono"},u.scenario+"  (line "+u.line+")"),h("div",{class:"upath"},u.path.join("\n ↓ ")));});
      box.querySelectorAll(".upath").forEach(function(p){p.style.whiteSpace="pre-wrap";});
      add(card,add(h("div",{class:"st-actions"}),btn,link("Open Branch Explorer",href("inspect:"+f.func+":branches"),"tests",f.func)),box);
    }
    if(f.limited)card.appendChild(h("p",{class:"st-note"},f.limited));
    el.appendChild(card);
  });
});

function fmtNs(ns){if(ns==null)return "";var a=Math.abs(ns);if(a>=1e9)return (ns/1e9).toFixed(2)+"s";if(a>=1e6)return (ns/1e6).toFixed(2)+"ms";if(a>=1e3)return (ns/1e3).toFixed(1)+"µs";return Math.round(ns)+"ns";}
// Exercise card shared by challenges and quizzes: options are compared by
// index with the answer the model recorded; Show Evidence reveals its source.
var RES={};try{RES=JSON.parse(localStorage.getItem(PKEY+":answers")||"{}")||{};}catch(e){RES={};}
function answered(id,ok){RES[id]=ok?1:0;try{localStorage.setItem(PKEY+":answers",JSON.stringify(RES));}catch(e){}}
function exerciseCard(e,cat){
  var card=h("div",{class:"st-card ex-card"});var hd=h("div",{class:"st-head"});
  add(hd,badge(e.evidence.basis),h("span",{class:"st-meta"},e.type+" · source: "+e.sourceType),e.sourceFile?h("span",{class:"st-meta mono"},loc(e.sourceFile,e.sourceLine)):null);card.appendChild(hd);
  card.appendChild(h("p",{class:"ex-q"},e.prompt));
  if(e.code)card.appendChild(h("div",{class:"ex-code"},e.code));
  if((e.given||[]).length)card.appendChild(h("div",{class:"ex-code"},"given: "+e.given.join(", ")));
  var opts=h("div",{class:"ex-opts",role:"group"});var fb=h("p",{class:"st-meta"});var ev=h("div");ev.hidden=true;
  var btns=e.options.map(function(o,i){var b=h("button",{type:"button"},o);b.addEventListener("click",function(){
    btns.forEach(function(x,k){x.disabled=true;if(k===e.answer)x.className="right";});
    if(i!==e.answer)b.className="wrong";
    fb.textContent="";add(fb,h("span",{class:i===e.answer?"pr-ok":"pr-bad"},i===e.answer?"Correct.":"Not this one."),i===e.answer?"":" The answer is marked; see the evidence.");
    answered(e.id,i===e.answer);track(cat,e.id);ev.hidden=false;
  });opts.appendChild(b);return b;});
  card.appendChild(opts);card.appendChild(fb);
  var u=href(e.evidence.ref);var li=h("li");add(li,badge(e.evidence.basis)," "+e.evidence.text+" ");
  var w=loc(e.evidence.file||e.sourceFile,e.evidence.line||e.sourceLine);if(w)li.appendChild(h("span",{class:"mono st-meta"},w+" "));if(u)li.appendChild(h("a",{href:u},"open"));
  add(ev,add(h("ul",{class:"st-why"}),li));
  var sh=h("button",{type:"button",class:"ghost small"},"Show Evidence");sh.addEventListener("click",function(){ev.hidden=!ev.hidden;});
  card.appendChild(add(h("div",{class:"st-actions"}),sh));card.appendChild(ev);
  if(RES[e.id]!=null)card.appendChild(h("p",{class:"st-meta"},"Answered before: "+(RES[e.id]?"correct":"not correct")+"."));
  return card;
}
// Longest subsequence of ids whose reference ranks never decrease (ties in
// the reference are interchangeable): the items in the right order.
function inOrder(ids,rank){
  var n=ids.length,len=[],prev=[],best=-1;
  for(var i=0;i<n;i++){len[i]=1;prev[i]=-1;for(var j=0;j<i;j++)if(rank[ids[j]]<=rank[ids[i]]&&len[j]+1>len[i]){len[i]=len[j]+1;prev[i]=j;}if(best<0||len[i]>len[best])best=i;}
  var ok={};for(var k=best;k>=0;k=prev[k])ok[ids[k]]=1;return ok;
}
view("Predict Execution","predict",function(el){
  var PR=M.predict||{},E=PR.entries||[],OB=PR.observed||null;
  add(el,add(h("p",{class:"st-note"}),"Choose an entry point and build the order in which you expect its functions to run. The candidates are the module functions the call graph says it may reach ",badge("STATIC")," (up to 5 calls deep). Then compare your order with the execution trace ",badge("RUNTIME")," or with the order of the calls in the code ",badge("STATIC")," — a possible order, not an observed one. The comparison only matches function IDs and positions."));
  if(!E.length){el.appendChild(h("p",{class:"st-note"},"No entry point of the change reaches other functions of the module."));return;}
  if(OB)el.appendChild(add(h("p",{class:"st-note"}),badge("RUNTIME")," Trace "+OB.source+": "+OB.functions+" module functions observed. "+OB.note));
  else el.appendChild(h("p",{class:"st-note"},"No execution trace in this report, so only the static call order can be compared. To add one: gitview study --trace trace.out (from go test -trace, or captured with gitview inspect trace)."));
  var sel=h("select",{"aria-label":"Entry point"});E.forEach(function(e,i){sel.appendChild(h("option",{value:String(i)},e.title));});
  el.appendChild(add(h("div",{class:"st-actions",style:"margin-left:0;align-items:center"}),h("span",{class:"st-meta"},"Entry point:"),sel));
  var area=h("div");el.appendChild(area);
  function render(){
    area.textContent="";var en=E[+sel.value],seq=[],byId={};
    en.candidates.forEach(function(c){byId[c.id]=c;});
    var head=h("div",{class:"st-card"});add(head,add(h("div",{class:"st-head"}),layerBadge(en.layer),h("span",{class:"st-title"},en.title),h("span",{class:"st-meta mono"},loc(en.file,en.line))));
    head.appendChild(actions(en.id,en.file,en.line,"codeflow"));area.appendChild(head);
    var grid=h("div",{class:"pr-grid"}),cl=h("ul",{class:"pr-list"}),yl=h("ol",{class:"pr-list"}),res=h("div");
    add(grid,add(h("div"),add(h("h3"),"Candidates ",badge("STATIC")),cl),add(h("div"),h("h3",null,"Your prediction (after "+en.title+")"),yl));
    area.appendChild(grid);
    function mv(i,d){var t=seq[i];seq[i]=seq[i+d];seq[i+d]=t;res.textContent="";draw();}
    function draw(){
      cl.textContent="";yl.textContent="";
      en.candidates.forEach(function(c){var used=seq.indexOf(c.id)>=0;var li=h("li",{class:used?"used":null});var b=h("button",{type:"button",class:"ghost small",title:"Add to the prediction"},"+");b.disabled=used;b.addEventListener("click",function(){seq.push(c.id);res.textContent="";draw();});add(li,b,h("span",{class:"nm"},c.name),layerBadge(c.layer),h("span",{class:"st-meta"},"depth "+c.depth));cl.appendChild(li);});
      if(!seq.length)yl.appendChild(h("li",{class:"st-meta"},"Add candidates in the order you expect them to run."));
      seq.forEach(function(id,i){var li=h("li");var up=h("button",{type:"button",class:"ghost small",title:"Move up"},"↑"),dn=h("button",{type:"button",class:"ghost small",title:"Move down"},"↓"),rm=h("button",{type:"button",class:"ghost small",title:"Remove"},"×");
        up.disabled=i===0;dn.disabled=i===seq.length-1;up.addEventListener("click",function(){mv(i,-1);});dn.addEventListener("click",function(){mv(i,1);});rm.addEventListener("click",function(){seq.splice(i,1);res.textContent="";draw();});
        add(li,h("span",{class:"st-n"},String(i+1)),h("span",{class:"nm"},byId[id].name),up,dn,rm);yl.appendChild(li);});
    }
    function compare(useTrace){
      res.textContent="";var ref=[],rank={},info={};
      if(useTrace){(en.observed||[]).forEach(function(c){if(byId[c.id]){ref.push(c.id);rank[c.id]=c.seq;info[c.id]=c;}});}
      else{(en.staticOrder||[]).forEach(function(id){if(byId[id]&&rank[id]==null){rank[id]=ref.length;ref.push(id);}});}
      var basis=useTrace?"RUNTIME":"STATIC";
      var common=seq.filter(function(id){return rank[id]!=null;});
      var unexpected=seq.filter(function(id){return rank[id]==null;});
      var missed=ref.filter(function(id){return seq.indexOf(id)<0;});
      var ok=inOrder(common,rank),nOk=0;common.forEach(function(id){if(ok[id])nOk++;});
      track("codeflow","predict:"+en.id);
      var box=h("div",{class:"st-card"});add(box,add(h("div",{class:"st-head"}),badge(basis),h("b",null,useTrace?"Compared with the trace "+OB.source:"Compared with the static call order")));
      if(useTrace&&!ref.length){box.appendChild(h("p",{class:"st-note"},"None of these candidates appears in the trace under this entry point. That does not mean they did not run: the trace has no stack for them."));res.appendChild(box);return;}
      box.appendChild(h("div",{class:"pr-sum"},"Correct: "+common.length+" · Missed: "+missed.length+" · Unexpected: "+unexpected.length+" · Correct order: "+nOk+" · Incorrect order: "+(common.length-nOk)));
      var t=h("table",{class:"st-table"}),tb=h("tbody");
      add(t,add(h("thead"),add(h("tr"),h("th",null,"#"),h("th",null,"Your prediction"),h("th",null,"Result"),h("th",null,useTrace?"First seen in the trace":"Position in the code order"))),tb);
      seq.forEach(function(id,i){var r=h("td"),w=h("td",{class:"mono st-meta"});
        if(rank[id]==null){r.appendChild(h("span",{class:"pr-bad"},"Unexpected"));r.appendChild(document.createTextNode(useTrace?" — not observed in the trace under this entry (absence is not proof it did not run)":" — not in the static call order"));}
        else{add(r,h("span",{class:"pr-ok"},"Correct"),document.createTextNode(" · "),ok[id]?h("span",{class:"pr-ok"},"correct order"):h("span",{class:"pr-warn"},"incorrect order"));w.textContent=useTrace?"#"+(ref.indexOf(id)+1)+" at "+fmtNs(info[id].firstNs)+" ("+info[id].how+")":"#"+(ref.indexOf(id)+1);}
        add(tb,add(h("tr"),h("td",{class:"mono"},String(i+1)),h("td",{class:"mono"},byId[id].name),r,w));});
      box.appendChild(t);
      if(missed.length){var ml=h("ul",{class:"st-why"});missed.forEach(function(id){add(ml,add(h("li"),h("span",{class:"pr-warn"},"Missed "),h("span",{class:"mono"},byId[id].name),h("span",{class:"st-meta"},useTrace?" — observed at "+fmtNs(info[id].firstNs)+" ("+info[id].how+")":" — position #"+(ref.indexOf(id)+1)+" in the code order")));});add(box,h("p",{class:"st-meta"},useTrace?"Observed under this entry point but not in your prediction:":"In the static order but not in your prediction:"),ml);}
      var rv=h("button",{type:"button",class:"ghost small"},useTrace?"Show the observed order":"Show the static order");var ol=h("ol",{class:"st-why"});ol.hidden=true;
      ref.forEach(function(id){var c=byId[id];add(ol,add(h("li"),h("span",{class:"mono"},c.name),h("span",{class:"st-meta"},useTrace?"  "+fmtNs(info[id].firstNs)+" · "+info[id].how+(ref.some(function(o){return o!==id&&rank[o]===rank[id];})?" · same instant as another (order unknown)":""):"  depth "+c.depth+" · "+loc(c.file,c.line))));});
      rv.addEventListener("click",function(){ol.hidden=!ol.hidden;});add(box,add(h("div",{class:"st-actions"}),rv),ol);
      if(useTrace)box.appendChild(h("p",{class:"st-note"},"Order = first appearance in the trace (time, then outer frames before inner ones); a function that runs several times counts once."));
      else box.appendChild(h("p",{class:"st-note"},"The static order lists calls depth first in the order they appear in the code. Branches, loops and early returns decide the real order, so this is a possible order, not an observed one."));
      res.appendChild(box);
    }
    var bt=h("button",{type:"button",class:"ghost small"},"Compare With Trace");bt.disabled=!OB;if(!OB)bt.title="No execution trace in this report";
    var bs=h("button",{type:"button",class:"ghost small"},"Compare With Static Order");var bc=h("button",{type:"button",class:"ghost small"},"Clear");
    bt.addEventListener("click",function(){if(seq.length)compare(true);});bs.addEventListener("click",function(){if(seq.length)compare(false);});bc.addEventListener("click",function(){seq=[];res.textContent="";draw();});
    area.appendChild(add(h("div",{class:"st-actions",style:"margin-left:0"}),bt,bs,bc));area.appendChild(res);
    draw();
  }
  sel.addEventListener("change",render);render();
  var TX=(M.exercises||[]).filter(function(e){return e.type==="trace";});
  if(TX.length){add(el,add(h("h3",{style:"margin-top:22px"}),"Trace Challenge ",badge("RUNTIME")));TX.forEach(function(e){el.appendChild(exerciseCard(e,"challenges"));});}
});
function fmtBytes(v){if(v>=1073741824)return (v/1073741824).toFixed(2)+" GB";if(v>=1048576)return (v/1048576).toFixed(2)+" MB";if(v>=1024)return (v/1024).toFixed(1)+" kB";return v+" B";}
function fmtVal(v,unit){if(unit==="nanoseconds")return fmtNs(v);if(unit==="bytes")return fmtBytes(v);return String(v)+(unit&&unit!=="count"?" "+unit:"");}
function kvRow(tb,label,val){if(val==null||val===""||(Array.isArray(val)&&!val.length))return;var td=h("td");(Array.isArray(val)?val:[val]).forEach(function(v){if(v&&v.nodeType)td.appendChild(v);else td.appendChild(h("div",null,v));});add(tb,add(h("tr"),h("th",null,label),td));}
function locLinkS(file,line){var w=loc(file,line);if(!w)return null;var u=href("diff:"+file+":"+(line||0));return u?h("a",{href:u,class:"mono"},w):h("span",{class:"mono st-meta"},w);}
view("SQL","sql",function(el){
  var S=M.sql||{},Q=S.queries||[];
  add(el,add(h("p",{class:"st-note"}),"Every query the code sends, as read from the source ",badge("SQL"),": the tables it names, its joins, filters and clauses. Indexes come from CREATE INDEX, PRIMARY KEY and UNIQUE in the project's .sql files ",badge("SQL"),"; a column that appears in a query only says the text matches, not that PostgreSQL uses the index or that the index is good or bad. Only an EXPLAIN plan shows which index the planner chooses."));
  add(el,h("p",{class:"st-meta"},Q.length+" queries · "+((S.schemaFiles||[]).length?(S.indexes||0)+" indexes in "+S.schemaFiles.join(", "):"no .sql file in the project")));
  (S.notes||[]).forEach(function(n){el.appendChild(h("p",{class:"st-note"},n));});
  if(!Q.length){el.appendChild(h("p",{class:"st-note"},"No database operation with SQL was found in the module (pgx, pgxpool, database/sql, sqlx)."));return;}
  var nIn=Q.filter(function(q){return q.inReport;}).length;var only=null,list=h("div");
  if(nIn&&nIn<Q.length){only=h("input",{type:"checkbox",id:"sq-only"});add(el,add(h("label",{class:"st-filter",for:"sq-only"}),only," Only queries of the change's reading order ("+nIn+")"));}
  el.appendChild(list);
  function render(){list.textContent="";Q.forEach(function(q){if(only&&only.checked&&!q.inReport)return;list.appendChild(card(q));});}
  function card(q){
    var c=h("div",{class:"st-card"+(q.inReport?" changed":"")});var hd=h("div",{class:"st-head"});
    add(hd,badge("SQL"),h("span",{class:"st-title"},(q.statement||"?").toUpperCase()),h("span",{class:"st-meta"},q.lib+" "+q.method+" in "),h("span",{class:"mono"},q.funcName),locLinkS(q.file,q.line));c.appendChild(hd);
    if(q.sql)c.appendChild(h("div",{class:"sq-sql"},q.sql));
    var t=h("table",{class:"sq-kv"}),tb=h("tbody");t.appendChild(tb);var s=q.structure;
    if(s){
      kvRow(tb,"Tables",(s.tables||[]).map(function(x){return x.name+(x.alias?" AS "+x.alias:"")+"  · "+x.role+(x.depth?" (in a subquery)":"");}));
      kvRow(tb,"CTEs",s.ctes);
      kvRow(tb,"JOINs",(s.joins||[]).map(function(j){return j.kind+" "+j.table+(j.alias?" "+j.alias:"")+(j.on?(j.on.indexOf("USING")===0?" ":" ON ")+j.on:"");}));
      kvRow(tb,s.statement==="insert"?"Inserted columns":s.statement==="update"?"Updated columns":"Columns",s.columns);
      kvRow(tb,"Filters (WHERE)",(s.filters||[]).length?s.filters:(s.statement==="select"||s.statement==="update"||s.statement==="delete"?"none — no WHERE clause":null));
      kvRow(tb,"GROUP BY",s.groupBy);kvRow(tb,"HAVING",s.having);kvRow(tb,"ORDER BY",s.orderBy);kvRow(tb,"LIMIT",s.limit);kvRow(tb,"OFFSET",s.offset);
      kvRow(tb,"ON CONFLICT",s.conflict);kvRow(tb,"RETURNING",s.returning);kvRow(tb,"Row lock",s.lock);
      (s.notes||[]).forEach(function(n){kvRow(tb,"Note",n);});
    }
    kvRow(tb,"Parameters",q.params);kvRow(tb,"Scanned into",q.scan);kvRow(tb,"Transaction",q.tx);
    (q.notes||[]).forEach(function(n){kvRow(tb,"Note",n);});
    if(q.callers&&q.callers.length){var cl=[];q.callers.forEach(function(r){var d=h("div");var u=href("codeflow:"+r.id);add(d,badge("STATIC")," ",u?h("a",{href:u},r.name):h("span",null,r.name)," ",locLinkS(r.file,r.line));cl.push(d);});kvRow(tb,"Callers of "+q.funcName.split(".").pop(),cl);}
    c.appendChild(t);
    if((q.indexes||[]).length){
      var it=h("table",{class:"st-table"}),ib=h("tbody");add(it,add(h("thead"),add(h("tr"),h("th",null,"Index"),h("th",null,"Columns (in order)"),h("th",null,"Defined at"),h("th",null,"Columns that appear in this query (text match)"),h("th",null,"EXPLAIN"))),ib);
      q.indexes.forEach(function(ix){var name=ix.name||(ix.primary?"PRIMARY KEY":"UNIQUE constraint");var kind=[];if(ix.primary)kind.push("primary key");else if(ix.unique)kind.push("unique");if(ix.method)kind.push(ix.method);if(ix.implicit)kind.push("implicit index of a constraint");
        var nm=add(h("td"),badge("SQL")," ",h("span",{class:"mono"},name+" on "+ix.table),kind.length?h("div",{class:"st-meta"},kind.join(" · ")):null,ix.where?h("div",{class:"st-meta mono"},"partial: WHERE "+ix.where):null,ix.dropped?h("div",{class:"pr-warn"},"a DROP INDEX with this name exists at "+ix.dropped):null);
        var ex=h("td");if(q.plan)ex.appendChild(ix.inPlan?add(h("span"),badge(q.plan.evidence.basis)," the plan uses it"):h("span",{class:"st-meta"},"not in the plan"));else ex.appendChild(h("span",{class:"st-meta"},"no plan"));
        add(ib,add(h("tr"),nm,h("td",{class:"mono"},(ix.columns||[]).join(", ")+((ix.include||[]).length?" INCLUDE ("+ix.include.join(", ")+")":"")),add(h("td"),locLinkS(ix.file,ix.line)),h("td",{class:"mono"},(ix.referenced||[]).join(", ")||"—"),ex));});
      add(c,h("p",{class:"st-meta",style:"margin:8px 0 2px"},"Indexes on the tables of this query:"),it);
    }else if(s&&(s.tables||[]).length)c.appendChild(h("p",{class:"st-meta"},"No index on these tables was found in the project's .sql files."));
    if(q.plan){var p=q.plan;var pb=h("div",{class:"st-card"});add(pb,add(h("div",{class:"st-head"}),badge(p.evidence.basis),h("b",null,p.mode+(p.generic?" (generic plan)":"")),h("span",{class:"st-meta"},p.evidence.text)));var pt=h("table",{class:"sq-kv"}),pbd=h("tbody");pt.appendChild(pbd);kvRow(pbd,"Scans",p.scans);kvRow(pbd,"Joins",p.joins);kvRow(pbd,"Indexes",p.indexes);kvRow(pbd,"Warnings",p.warnings);if(p.analyzed){kvRow(pbd,"Planning",p.planningMs!=null?p.planningMs.toFixed(3)+" ms":null);kvRow(pbd,"Execution",p.executionMs!=null?p.executionMs.toFixed(3)+" ms":null);}pb.appendChild(pt);c.appendChild(pb);}
    else c.appendChild(h("p",{class:"st-note"},"No EXPLAIN plan in this report. gitview inspect sql --function "+q.funcName.split(".").pop()+" --explain --dsn <development database> asks PostgreSQL for the plan (EXPLAIN without ANALYZE: the statement is not executed)."));
    var acts=h("div",{class:"st-actions"});acts.appendChild(link("Open SQL Explorer",href("inspect:"+q.func+":sql"),"sql",q.id));acts.appendChild(link("Open Code Flow",href("codeflow:"+q.id)||href("codeflow:"+q.func),"sql",q.id));
    var b=h("button",{type:"button",class:"ghost small"},"Studied");b.addEventListener("click",function(){track("sql",q.id);b.textContent="Studied ✓";});acts.appendChild(b);c.appendChild(acts);
    return c;
  }
  if(only)only.addEventListener("change",render);render();
});
view("Performance","performance",function(el){
  var PF=M.performance||{},O=PF.observed||{},ST=PF.static||[];
  add(el,add(h("p",{class:"st-note"}),"Two separate sections that are never mixed. ",badge("RUNTIME")," measurements: sampled profiles, benchmarks and trace regions, valid only for the run that produced them. ",badge("STATIC")," facts read from the code and the compiler: loops, decisions, database calls, escapes to the heap. A static fact is not a cost, and a function missing from a profile was not measured, which is not the same as cheap."));
  var obs=h("div",{class:"st-sec obs"});add(obs,add(h("h3"),badge("RUNTIME"),"Observed"));el.appendChild(obs);
  function prof(P,title){
    var c=h("div",{class:"st-card"});add(c,add(h("div",{class:"st-head"}),badge("RUNTIME"),h("b",null,title+" · "+P.source),h("span",{class:"st-meta"},P.sampleType+" · total "+fmtVal(P.total,P.unit)+" · "+P.samples+" samples"+(P.durationMs?" · "+(P.durationMs/1000).toFixed(1)+" s":""))));
    if(P.meaning)c.appendChild(h("p",{class:"st-note"},P.meaning));
    if(!P.samples)c.appendChild(h("p",{class:"st-note"},"The profile has no samples: nothing was measured (the run may have been shorter than the sampling interval)."));
    function tbl(rows,head,showFound){var t=h("table",{class:"st-table"}),tb=h("tbody");add(t,add(h("thead"),add(h("tr"),h("th",null,head),h("th",null,"flat"),h("th",null,"flat %"),h("th",null,"cum %"))),tb);
      rows.forEach(function(f){var nm=h("td",{class:"mono"});var u=f.id?href("inspect:"+f.id+":perf"):null;if(u){var a=h("a",{href:u},f.name);a.addEventListener("click",function(){track("performance",f.id);});nm.appendChild(a);}else nm.textContent=f.name;
        if(showFound&&!f.found){add(tb,add(h("tr"),nm,add(h("td",{colspan:"3"}),h("span",{class:"st-meta"},"no samples in this profile (not measured)"))));return;}
        add(tb,add(h("tr"),nm,h("td",{class:"num"},fmtVal(f.flat,P.unit)),h("td",{class:"num"},f.flatPct.toFixed(1)+"%"),h("td",{class:"num"},f.cumPct.toFixed(1)+"%")));});return t;}
    if((P.top||[]).length)add(c,h("p",{class:"st-meta"},"Highest flat cost in the whole profile:"),tbl(P.top,"function",false));
    if((P.report||[]).length)add(c,h("p",{class:"st-meta"},"Functions of the reading order:"),tbl(P.report,"function",true));
    return c;
  }
  if(O.cpu)obs.appendChild(prof(O.cpu,"CPU profile"));
  if(O.mem)obs.appendChild(prof(O.mem,"Memory profile"));
  if((O.benchmarks||[]).length){var c=h("div",{class:"st-card"});add(c,add(h("div",{class:"st-head"}),badge("RUNTIME"),h("b",null,"Benchmarks · "+(O.benchFile||""))));
    var t=h("table",{class:"st-table"}),tb=h("tbody");add(t,add(h("thead"),add(h("tr"),h("th",null,"benchmark"),h("th",null,"unit"),h("th",null,"runs"),h("th",null,"median"),h("th",null,"min"),h("th",null,"max"))),tb);
    O.benchmarks.forEach(function(b){var f=function(v){return b.unit==="ns/op"?(v<1000?(+v.toPrecision(3))+"ns":fmtNs(v)):String(+v.toPrecision(4));};add(tb,add(h("tr"),h("td",{class:"mono"},b.name+(b.procs?"-"+b.procs:"")),h("td",null,b.unit),h("td",{class:"num"},String(b.n)),h("td",{class:"num"},f(b.median)),h("td",{class:"num"},f(b.min)),h("td",{class:"num"},f(b.max))));});
    add(c,t,h("p",{class:"st-note"},"Values of the last benchmark run in the report; with fewer than about 6 runs (-count) differences between them are not reliable."));obs.appendChild(c);}
  if((O.regions||[]).length){var c2=h("div",{class:"st-card"});add(c2,add(h("div",{class:"st-head"}),badge("RUNTIME"),h("b",null,"Trace regions · "+(O.traceFile||""))));
    var t2=h("table",{class:"st-table"}),tb2=h("tbody");add(t2,add(h("thead"),add(h("tr"),h("th",null,"region"),h("th",null,"count"),h("th",null,"total"),h("th",null,"min"),h("th",null,"max"))),tb2);
    O.regions.forEach(function(r){add(tb2,add(h("tr"),add(h("td",{class:"mono"}),r.type,r.instrumented?h("div",{class:"st-meta"},"GitView instrumentation: includes its overhead"):null,r.unfinished?h("div",{class:"st-meta"},r.unfinished+" did not end in the trace (not counted)"):null),h("td",{class:"num"},String(r.count)),h("td",{class:"num"},fmtNs(r.totalNs)),h("td",{class:"num"},fmtNs(r.minNs)),h("td",{class:"num"},fmtNs(r.maxNs))));});
    c2.appendChild(t2);obs.appendChild(c2);}
  if((O.missing||[]).length){var ml=h("ul",{class:"st-why",style:"margin-left:0"});O.missing.forEach(function(m){ml.appendChild(h("li",{class:"st-meta"},m));});add(obs,h("p",{class:"st-meta"},"Not measured in this report:"),ml);}
  var sta=h("div",{class:"st-sec sta"});add(sta,add(h("h3"),badge("STATIC"),"Static (no measurement)"));el.appendChild(sta);
  (PF.notes||[]).forEach(function(n){sta.appendChild(h("p",{class:"st-note"},n));});
  if(!ST.length){sta.appendChild(h("p",{class:"st-note"},"No function in the reading order."));return;}
  var t3=h("table",{class:"st-table"}),tb3=h("tbody");add(t3,add(h("thead"),add(h("tr"),h("th",null,"function"),h("th",null,"loops"),h("th",null,"cyclomatic"),h("th",null,"database calls (direct / reachable)"),h("th",null,"compiler: escapes and inlining"))),tb3);
  ST.forEach(function(s){var nm=h("td",{class:"mono"});var u=href("inspect:"+s.func+":perf");if(u){var a=h("a",{href:u},s.funcName);a.addEventListener("click",function(){track("performance",s.func);});nm.appendChild(a);}else nm.textContent=s.funcName;
    var cc=h("td");if(!s.escapeRun)cc.appendChild(h("span",{class:"st-meta"},"not loaded"));else{if(s.inline)cc.appendChild(h("div",{class:"mono"},s.inline));(s.escapes||[]).forEach(function(e){cc.appendChild(h("div",{class:"mono"},"line "+e));});if(!s.inline&&!(s.escapes||[]).length)cc.appendChild(h("span",{class:"st-meta"},"no escape or inlining diagnostic"));}
    add(tb3,add(h("tr"),nm,h("td",{class:"num"},String(s.loops)),h("td",{class:"num"},String(s.cyclomatic)),h("td",{class:"num"},s.dbOps+" / "+s.dbReach),cc));});
  sta.appendChild(t3);
});
var EKEY=PKEY+":explain";var EX={};try{EX=JSON.parse(localStorage.getItem(EKEY)||"{}")||{};}catch(e){EX={};}
function saveEX(){try{localStorage.setItem(EKEY,JSON.stringify(EX));}catch(e){}}
view("Explain It Yourself","explain",function(el){
  var F=M.explain||[];
  add(el,h("p",{class:"st-note"},"Pick a function and explain it in your own words, out loud or in the box below, before revealing anything. Each point of the checklist has a Reveal button with the facts the analysis found, labelled with their source, so you can check yourself. GitView does not judge your explanation."));
  if(!F.length){el.appendChild(h("p",{class:"st-note"},"No function in the reading order."));return;}
  var sel=h("select",{"aria-label":"Function"});F.forEach(function(f,i){sel.appendChild(h("option",{value:String(i)},f.funcName));});
  el.appendChild(add(h("div",{class:"st-actions",style:"margin-left:0;align-items:center"}),h("span",{class:"st-meta"},"Function:"),sel));
  var area=h("div");el.appendChild(area);
  function render(){
    area.textContent="";var f=F[+sel.value];
    var head=h("div",{class:"st-card"});add(head,add(h("div",{class:"st-head"}),layerBadge(f.layer),h("span",{class:"st-title"},f.funcName),locLinkS(f.file,f.line)));head.appendChild(actions(f.func,f.file,f.line,"codeflow"));area.appendChild(head);
    var nk=f.func+"|note";var ta=h("textarea",{class:"ex-note","aria-label":"Your explanation",placeholder:"Your explanation (kept only in this browser)"});ta.value=EX[nk]||"";ta.addEventListener("input",function(){EX[nk]=ta.value;saveEX();});
    f.items.forEach(function(it){
      var id=f.func+"|"+it.key;var box=h("div",{class:"ex-item"});
      var cb=h("input",{type:"checkbox"});cb.checked=!!EX[id];cb.addEventListener("change",function(){if(cb.checked)EX[id]=1;else delete EX[id];saveEX();});
      add(box,add(h("label"),cb,h("span",null,it.prompt)));
      var facts=h("ul",{class:"facts"});facts.hidden=true;
      if(!it.facts||!it.facts.length)facts.appendChild(h("li",{class:"st-meta"},"Nothing found by the analysis."));
      else it.facts.forEach(function(x){facts.appendChild(add(h("li"),badge(it.basis)," "+x));});
      var u=href(it.ref);if(u)facts.appendChild(add(h("li"),h("a",{href:u},"open the evidence")));
      var rv=h("button",{type:"button",class:"ghost small"},"Reveal");rv.addEventListener("click",function(){facts.hidden=!facts.hidden;rv.textContent=facts.hidden?"Reveal":"Hide";});
      add(box,add(h("div",{class:"st-actions",style:"margin-left:26px"}),rv),facts);area.appendChild(box);
    });
    area.appendChild(ta);
  }
  sel.addEventListener("change",render);render();
});
function exerciseView(kind,intro,empty){return function(el){
  var X=(M.exercises||[]).filter(function(e){return e.kind===kind;});
  add(el,h("p",{class:"st-note"},intro));
  if(!X.length){el.appendChild(h("p",{class:"st-note"},empty));return;}
  var done=X.filter(function(e){return RES[e.id]!=null;}).length,ok=X.filter(function(e){return RES[e.id]===1;}).length;
  el.appendChild(h("p",{class:"st-meta"},X.length+" "+(kind==="quiz"?"questions":"challenges")+" · answered "+done+" · correct "+ok));
  var by={},order=[];X.forEach(function(e){if(!by[e.type]){by[e.type]=[];order.push(e.type);}by[e.type].push(e);});
  order.forEach(function(t){var g=h("div",{class:"st-group"});add(g,h("h3",null,TYPE_NAMES[t]||t));by[t].forEach(function(e){g.appendChild(exerciseCard(e,kind==="quiz"?"quiz":"challenges"));});el.appendChild(g);});
};}
var TYPE_NAMES={signature:"Signatures",callgraph:"Call graph",cfg:"Control flow",sql:"SQL",dataflow:"Data Flow",route:"Routes",architecture:"Architecture",branch:"Branch challenge",trace:"Trace challenge"};
view("Quiz","quiz",exerciseView("quiz","Questions built from fixed templates over the analysis: each has one right answer, alternatives that are certainly wrong, and the evidence of the answer. A question is only asked when such alternatives exist.","Not enough facts for questions with reliable alternatives."));
view("Challenges","challenges",exerciseView("challenge","Follow the code: decide which way a branch goes for given values (the condition is evaluated, never guessed, and only simple integer and boolean conditions are used), where a value ends up, which expression a query receives, which function reaches the database, and in which order the trace shows functions.","No challenge could be built from this report."));
view("Progress","progress",function(el){
  add(el,h("p",{class:"st-note"},"What you opened, marked or answered in this browser for this report. It counts activity; it is not a measure of what you know. Nothing leaves this browser."));
  var A=M.architecture||{},cnt=function(o){return o?Object.keys(o).length:0;};
  var nDec=0;(M.decisions||[]).forEach(function(f){nDec+=(f.decisions||[]).length;});
  var rows=[
    ["Architecture","architecture",(A.chains||[]).length+(A.functions||[]).filter(function(f){return f.basis==="STATIC";}).length,0],
    ["Code Flow","codeflow",(M.readingOrder||[]).length+((M.predict||{}).entries||[]).length,cnt(P.files)],
    ["Data Flow","dataflow",(M.dataJourneys||[]).length,0],
    ["Branches","branches",nDec,0],
    ["SQL","sql",((M.sql||{}).queries||[]).length,0],
    ["Performance","performance",((M.performance||{}).static||[]).length,0],
    ["Tests","tests",(M.testMatrix||[]).length,0]];
  var t=h("table",{class:"st-table"}),tb=h("tbody");add(t,add(h("thead"),add(h("tr"),h("th",null,"Area"),h("th",null,"Visited"),h("th",null,""))),tb);
  rows.forEach(function(r){var n=Math.min(cnt(P[r[1]])+r[3],r[2]);var bar=h("div",{class:"pg-bar"});var sp=h("span");sp.style.width=(r[2]?Math.round(100*n/r[2]):0)+"%";bar.appendChild(sp);
    add(tb,add(h("tr"),h("td",null,r[0]),h("td",{class:"num"},r[2]?n+" of "+r[2]:"nothing to visit"),add(h("td"),bar)));});
  el.appendChild(t);
  var X=M.exercises||[];
  ["quiz","challenge"].forEach(function(k){var xs=X.filter(function(e){return e.kind===k;});if(!xs.length)return;var d=xs.filter(function(e){return RES[e.id]!=null;}).length,o=xs.filter(function(e){return RES[e.id]===1;}).length;el.appendChild(h("p",{class:"st-meta"},(k==="quiz"?"Quiz":"Challenges")+": answered "+d+" of "+xs.length+", correct on the last answer "+o+"."));});
  var nItems=0;(M.explain||[]).forEach(function(f){nItems+=(f.items||[]).length;});var checked=Object.keys(EX).filter(function(k){return k.slice(-5)!=="|note";}).length;
  if(nItems)el.appendChild(h("p",{class:"st-meta"},"Explain It Yourself: "+Math.min(checked,nItems)+" of "+nItems+" points checked."));
  var rs=h("button",{type:"button",class:"ghost small"},"Reset Progress");
  rs.addEventListener("click",function(){if(!window.confirm("Reset the progress of this report in this browser (visited items, answers, checklist and notes)?"))return;P={};RES={};EX={};try{localStorage.removeItem(PKEY);localStorage.removeItem(PKEY+":answers");localStorage.removeItem(EKEY);}catch(e){}show(cur);});
  el.appendChild(add(h("div",{class:"st-actions",style:"margin-left:0"}),rs));
});
views.forEach(function(v,i){var b=h("button",{type:"button"},v.name);b.addEventListener("click",function(){show(i,true);});nav.appendChild(b);});
function fromHash(){var hh=location.hash;if(hh.indexOf("#study")!==0)return false;var key=hh.slice(7);for(var i=0;i<views.length;i++)if(views[i].key===key){show(i);return true;}if(!body.firstChild)show(0);return true;}
if(!fromHash())show(0);
window.addEventListener("hashchange",function(){if(location.hash.indexOf("#study")===0){var key=location.hash.slice(7);if(!views[cur]||views[cur].key!==key)fromHash();}});
})();`
