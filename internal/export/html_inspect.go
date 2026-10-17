package export

// Go Deep Inspector tab of the HTML report. The script reads the results
// embedded in #inspect-data (computed by GitView when the report was made)
// and only displays them: nothing is analysed or executed in the browser.
// Every string from the repository is inserted with textContent or
// setAttribute, never as markup.

const inspectCSS = `
.insp-wrap{padding:22px 24px 8px}
.insp-app{display:grid;gap:10px}
.insp-top{display:flex;flex-wrap:wrap;gap:8px;align-items:center}
.insp-top select,.insp-top input[type=search],.insp-bar select,.insp-bar input[type=search],.insp-bar input[type=number]{font:13px var(--sans);color:var(--text);background:var(--bg);border:1px solid var(--border);border-radius:6px;padding:4px 8px;max-width:100%}
.insp-top select{max-width:min(560px,100%)}
.insp-id{font:12px var(--mono);color:var(--muted);overflow-wrap:anywhere}
.insp-tabs{display:flex;flex-wrap:wrap;gap:4px;border-bottom:1px solid var(--border)}
.insp-tabs button{font:13px var(--sans);background:transparent;border:0;border-bottom:2px solid transparent;color:var(--muted);padding:6px 10px;cursor:pointer;margin-bottom:-1px}
.insp-tabs button[aria-selected="true"]{color:var(--text);border-bottom-color:var(--accent)}
.insp-tabs button .dot{display:inline-block;width:6px;height:6px;border-radius:3px;background:var(--faint);margin-left:6px;vertical-align:middle}
.insp-tabs button .dot.on{background:var(--ok)}
.insp-bar{display:flex;flex-wrap:wrap;gap:8px;align-items:center;font-size:12.5px;color:var(--muted)}
.insp-bar label{display:flex;align-items:center;gap:6px}
.insp-grid{display:grid;grid-template-columns:minmax(0,1fr) 380px;gap:12px;align-items:start}
.insp-two{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:12px;align-items:start}
.insp-canvas{position:relative;border:1px solid var(--border);border-radius:6px;background:var(--bg);height:min(66vh,640px);overflow:hidden;touch-action:none}
.insp-canvas svg{width:100%;height:100%;display:block;cursor:grab;user-select:none;-webkit-user-select:none}
.insp-canvas svg.panning{cursor:grabbing}
.insp-canvas canvas{display:block;width:100%;height:100%;cursor:crosshair}
.insp-tools{position:absolute;right:8px;top:8px;display:flex;gap:4px;z-index:5}
.insp-panel{border:1px solid var(--border);border-radius:6px;padding:12px 14px;font-size:13px;max-height:min(66vh,640px);overflow:auto;min-width:0}
.insp-panel h3{font:600 13px var(--mono);margin:2px 0 6px;overflow-wrap:anywhere}
.insp-panel h4{font-size:12.5px;font-weight:600;margin:12px 0 4px;color:var(--muted)}
.insp-panel dl{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:3px 12px;margin:8px 0}
.insp-panel dt{color:var(--muted)}.insp-panel dd{margin:0;overflow-wrap:anywhere}
.insp-panel pre,.insp-pre{margin:6px 0 0;background:var(--surface);border:1px solid var(--border);border-radius:4px;padding:8px;font:12px/18px var(--mono);overflow:auto;max-height:360px;tab-size:4;white-space:pre}
.insp-pre .hl{background:var(--hl);display:inline-block;min-width:100%}
.insp-msg{color:var(--muted);font-size:13px;padding:14px;border:1px dashed var(--border);border-radius:6px}
.insp-msg.warn{color:var(--warn);border-color:var(--warn)}
.insp-note{font-size:12px;color:var(--muted);margin:4px 0}
.insp-kv{display:flex;flex-wrap:wrap;gap:4px 14px;font-size:12.5px;color:var(--muted)}
.insp-kv b{color:var(--text);font-weight:600}
.badge{display:inline-block;font:11px var(--sans);border:1px solid var(--border);border-radius:9px;padding:0 7px;color:var(--muted);margin-right:4px;white-space:nowrap}
.badge.static{border-color:var(--accent);color:var(--accent)}
.badge.observed{border-color:var(--ok);color:var(--ok)}
.badge.estimated{border-color:var(--warn);color:var(--warn)}
.insp-table{width:100%;border-collapse:collapse;font-size:12.5px}
.insp-table th{text-align:left;color:var(--muted);font-weight:600;border-bottom:1px solid var(--border);padding:4px 6px;cursor:default;white-space:nowrap}
.insp-table th.sort{cursor:pointer}
.insp-table td{padding:3px 6px;border-bottom:1px solid var(--border);vertical-align:top;overflow-wrap:anywhere}
.insp-table td.num{text-align:right;font-family:var(--mono);white-space:nowrap}
.insp-table tr.mine td{color:var(--text)}
.insp-table tr.other td{color:var(--muted)}
.insp-table tr.sel td{background:var(--hl)}
.insp-table tbody tr{cursor:pointer}
.mono{font-family:var(--mono)}
.g-node rect{fill:var(--surface);stroke:var(--border-strong);stroke-width:1}
.g-node text{font:11.5px var(--mono);fill:var(--text);pointer-events:none}
.g-node text.sub{fill:var(--muted);font-size:10.5px}
.g-node{cursor:pointer}
.g-node:focus{outline:none}
.g-node:focus rect,.g-node.sel rect{stroke:var(--accent);stroke-width:2}
.g-node.dec rect{fill:var(--hunk)}
.g-node.term rect{stroke-width:2}
.g-node.bad rect{stroke:var(--bad)}
.g-node.dead{opacity:.55}.g-node.dead rect{stroke-dasharray:4 3}
.g-node.root rect{stroke:var(--accent);stroke-width:2}
.g-node.k-source rect{fill:var(--add-line);stroke:var(--ok)}
.g-node.k-sql rect{stroke:var(--db,#4fb6a6);stroke-width:2}
.g-node.k-check rect{stroke:var(--warn)}
.g-node.k-unresolved rect{stroke:var(--warn);stroke-dasharray:3 3}
.g-node.k-external rect,.g-node.k-return rect{stroke-dasharray:2 2}
.g-node.cov-covered rect{fill:var(--add-line);stroke:var(--ok)}
.g-node.cov-uncovered rect{fill:var(--del-line);stroke:var(--bad)}
.g-node.cov-partial rect{stroke:var(--warn)}
.g-node.dim{opacity:.25}
.g-node.onpath rect{stroke:var(--accent);stroke-width:2.5}
.g-edge{fill:none;stroke:var(--muted);stroke-width:1.3}
.g-edge.back{stroke-dasharray:5 4}
.g-edge.err{stroke:var(--bad)}
.g-edge.c-derived{stroke-dasharray:6 3}
.g-edge.c-likely{stroke-dasharray:6 3;stroke:var(--faint)}
.g-edge.c-possible{stroke-dasharray:3 3;stroke:var(--faint)}
.g-edge.c-unresolved{stroke-dasharray:1.5 3;stroke:var(--warn)}
.g-edge.onpath{stroke:var(--accent);stroke-width:2.4}
.g-edge.dim{opacity:.2}
.g-label{font:10px var(--mono);fill:var(--muted);pointer-events:none}
.g-label.err{fill:var(--bad)}
.flame rect{stroke:var(--bg);stroke-width:1;cursor:pointer}
.flame text{font:11px var(--mono);fill:#1b1f24;pointer-events:none}
.flame g.mine rect{fill:#e8a25a}.flame g.other rect{fill:#d6c27a}.flame g.rt rect{fill:#b9c6d2}
.flame g.hit rect{fill:var(--accent)}
.legend{display:flex;flex-wrap:wrap;gap:4px 14px;font-size:11.5px;color:var(--muted)}
.legend i{display:inline-block;width:12px;height:10px;margin-right:4px;vertical-align:middle;border-radius:2px}
.plan ul{list-style:none;margin:0;padding-left:18px}
.plan>ul{padding-left:0}
.plan li{margin:3px 0}
.plan .pn{font:600 12.5px var(--mono)}
.plan .pc{font:11.5px var(--mono);color:var(--muted)}
.sqlbox{border:1px solid var(--border);border-radius:6px;padding:10px 12px;margin:0 0 10px}
.sqlbox h3{font:600 13px var(--mono);margin:0 0 4px}
@media screen and (max-width:980px){.insp-grid,.insp-two{grid-template-columns:minmax(0,1fr)}.insp-panel{max-height:none}}
@media screen and (max-width:760px){.insp-wrap{padding:16px 10px}.insp-canvas{height:58vh}}
@media print{.insp-tools,.insp-bar{display:none!important}.insp-canvas{height:auto;min-height:420px}}
`

const inspectJS = `(function(){
"use strict";
var dataEl=document.getElementById("inspect-data"),host=document.getElementById("inspect-app");
if(!dataEl||!host)return;
var D;
try{D=JSON.parse(dataEl.textContent);}catch(e){host.textContent="The inspector data could not be read.";return;}
var FLOW=null;
try{var fe=document.getElementById("flow-data");if(fe&&fe.textContent)FLOW=JSON.parse(fe.textContent);}catch(e){FLOW=null;}
var SVGNS="http://www.w3.org/2000/svg";
var SYMS=D.symbols||[],ANCH=D.anchors||{};
function h(tag,attrs,text){var el=document.createElement(tag);if(attrs)for(var k in attrs){if(attrs[k]==null)continue;if(k==="class")el.className=attrs[k];else el.setAttribute(k,attrs[k]);}if(text!=null)el.textContent=text;return el;}
function s(tag,attrs){var el=document.createElementNS(SVGNS,tag);if(attrs)for(var k in attrs)el.setAttribute(k,attrs[k]);return el;}
function add(p){for(var i=1;i<arguments.length;i++){if(arguments[i])p.appendChild(arguments[i]);}return p;}
function txt(t){return document.createTextNode(t);}
function cssv(n){return getComputedStyle(document.documentElement).getPropertyValue(n).trim();}
function fmtNs(ns){if(ns==null)return "";var a=Math.abs(ns);if(a>=1e9)return (ns/1e9).toFixed(2)+"s";if(a>=1e6)return (ns/1e6).toFixed(2)+"ms";if(a>=1e3)return (ns/1e3).toFixed(1)+"µs";return Math.round(ns)+"ns";}
function fmtVal(v,unit){if(unit==="nanoseconds")return fmtNs(v);if(unit==="bytes"){if(v>=1073741824)return (v/1073741824).toFixed(2)+"GB";if(v>=1048576)return (v/1048576).toFixed(2)+"MB";if(v>=1024)return (v/1024).toFixed(1)+"kB";return v+"B";}return String(v);}
function pct(x){return (x||0).toFixed(2)+"%";}
function loc(f,l){return f?(l?f+":"+l:f):"";}
function badge(kind,label){return h("span",{class:"badge "+kind},label||kind);}
function msg(t,warn){return h("div",{class:"insp-msg"+(warn?" warn":"")},t);}
function diffHref(file,line){var fid=ANCH[file];if(!fid||!line)return null;return document.getElementById(fid+"-L"+line)?"#"+fid+"-L"+line:null;}
function locLink(file,line){var href=diffHref(file,line),t=loc(file,line);if(!t)return null;if(href){var a=h("a",{href:href,class:"mono"},t);a.title="Open in diff";return a;}return h("span",{class:"mono muted"},t);}
function measure(){var c=document.createElement("canvas").getContext("2d");return function(t,font){c.font=font;return c.measureText(t).width;};}
var tw=measure(),MONO=cssv("--mono")||"monospace";
// Shared with the Study tab, which reuses the graph and code renderers.
window.GitViewInspect={Graph:Graph,codeLines:codeLines,symbolIndex:function(id){for(var i=0;i<SYMS.length;i++)if(SYMS[i].symbol.id===id)return i;return -1;},bundle:function(id){for(var i=0;i<SYMS.length;i++)if(SYMS[i].symbol.id===id)return SYMS[i];return null;}};
function clip(t,n){t=String(t==null?"":t);return t.length>n?t.slice(0,n-1)+"…":t;}

if(!SYMS.length){host.appendChild(msg((D.notes&&D.notes[0])||"No function was included in this report."));return;}

// ---------- state ----------
var TABS=[["codeflow","Code Flow"],["dataflow","Data Flow"],["trace","Execution Trace"],["branches","Branch Explorer"],["perf","Performance"],["sql","SQL Explorer"]];
var S={sym:0,tab:"branches",df:null,dfDir:"both",dfDepth:3,dfHide:false,cfgPath:-1,perf:"cpu",perfMine:false,perfFilter:"",perfSort:"flat",flameRoot:null,flameFind:"",trAll:false,trFilter:"",trTask:"",trView:null,trSel:null};
function sym(){return SYMS[S.sym];}

// ---------- header ----------
var top=h("div",{class:"insp-top"});
var symSel=h("select",{"aria-label":"Function"});
SYMS.forEach(function(b,i){symSel.appendChild(h("option",{value:String(i)},b.symbol.qualified+"  ("+loc(b.symbol.file,b.symbol.line)+")"));});
var findIn=h("input",{type:"search",placeholder:"Filter functions…","aria-label":"Filter functions",autocomplete:"off"});
add(top,symSel,findIn);
var idLine=h("div",{class:"insp-id"});
var tabs=h("div",{class:"insp-tabs",role:"tablist"});
var body=h("div");
add(host,top,idLine,tabs,body);
findIn.addEventListener("input",function(){var q=findIn.value.trim().toLowerCase(),first=-1;Array.prototype.forEach.call(symSel.options,function(o,i){var b=SYMS[i],hit=!q||(b.symbol.qualified+" "+b.symbol.file).toLowerCase().indexOf(q)>=0;o.hidden=!hit;if(hit&&first<0)first=i;});if(first>=0&&symSel.options[S.sym].hidden){setSym(first);}});
symSel.addEventListener("change",function(){setSym(+symSel.value);});

function hasData(t){var b=sym();switch(t){case "codeflow":return !!(FLOW&&FLOW.nodes);case "dataflow":return !!(b.dataflow&&Object.keys(b.dataflow).length);case "trace":return !!(b.trace&&b.trace.loaded);case "branches":return !!b.branches;case "perf":return !!(b.performance&&!b.performance.message);case "sql":return !!(b.sql&&b.sql.ops&&b.sql.ops.length);}return false;}
function drawTabs(){
  tabs.textContent="";
  TABS.forEach(function(t){var bt=h("button",{type:"button",role:"tab","aria-selected":String(S.tab===t[0]),"data-tab":t[0]},t[1]);bt.appendChild(h("span",{class:"dot"+(hasData(t[0])?" on":""),title:hasData(t[0])?"data available":"no data"}));tabs.appendChild(bt);});
}
tabs.addEventListener("click",function(e){var b=e.target.closest("button[data-tab]");if(!b)return;S.tab=b.getAttribute("data-tab");sync();render();});
tabs.addEventListener("keydown",function(e){if(e.key!=="ArrowRight"&&e.key!=="ArrowLeft")return;var i=TABS.findIndex(function(t){return t[0]===S.tab;});i=(i+(e.key==="ArrowRight"?1:TABS.length-1))%TABS.length;S.tab=TABS[i][0];sync();render();var b=tabs.querySelector('[data-tab="'+S.tab+'"]');if(b)b.focus();});

function setSym(i){S.sym=i;S.df=null;S.cfgPath=-1;S.flameRoot=null;S.trSel=null;symSel.value=String(i);sync();render();}
function sync(){try{history.replaceState(null,"","#inspect:"+encodeURIComponent(sym().symbol.id)+":"+S.tab);}catch(e){}}
function fromHash(){
  var hh=location.hash;if(hh.indexOf("#inspect")!==0)return false;
  var rest=hh.slice(9);if(!rest)return true;
  var i=rest.lastIndexOf(":"),id,tab=i>=0?rest.slice(i+1):"";
  try{id=decodeURIComponent(i>=0?rest.slice(0,i):rest);}catch(e){return true;}
  SYMS.forEach(function(b,k){if(b.symbol.id===id)S.sym=k;});
  if(TABS.some(function(t){return t[0]===tab;}))S.tab=tab;
  symSel.value=String(S.sym);return true;
}

function render(){
  var b=sym(),r=b.symbol;
  idLine.textContent="";
  add(idLine,txt(r.id+"  ·  "),locLink(r.file,r.line),txt("  ·  module "+(r.module||"")+" · code version "+(r.version||"")));
  drawTabs();
  body.textContent="";
  try{
    switch(S.tab){
      case "codeflow":tabCodeFlow(b);break;
      case "dataflow":tabDataFlow(b);break;
      case "trace":tabTrace(b);break;
      case "branches":tabBranches(b);break;
      case "perf":tabPerf(b);break;
      case "sql":tabSQL(b);break;
    }
  }catch(err){body.appendChild(msg("This view could not be displayed: "+err.message,true));}
}

// ---------- generic layered graph with zoom & pan ----------
function Graph(parent,opt){
  var g=this;g.opt=opt||{};
  g.wrap=h("div",{class:"insp-canvas"});
  g.svg=s("svg",{role:"application","aria-label":g.opt.label||"Graph"});
  var defs=s("defs");
  [["ga","var(--muted)"],["gp","var(--accent)"],["gb","var(--bad)"],["gw","var(--warn)"]].forEach(function(m){var mk=s("marker",{id:"m-"+m[0],viewBox:"0 0 10 10",refX:"9",refY:"5",markerWidth:"7",markerHeight:"7",orient:"auto-start-reverse"});var p=s("path",{d:"M0,1 L9,5 L0,9 z"});p.style.fill=m[1];mk.appendChild(p);defs.appendChild(mk);});
  g.svg.appendChild(defs);g.vp=s("g");g.svg.appendChild(g.vp);g.wrap.appendChild(g.svg);
  var tools=h("div",{class:"insp-tools"});
  [["fit","Fit","Fit graph"],["in","+","Zoom in"],["out","−","Zoom out"]].forEach(function(t){var b=h("button",{type:"button",class:"ghost small","aria-label":t[2]},t[1]);b.addEventListener("click",function(){if(t[0]==="fit")g.fit();else g.zoom(t[0]==="in"?1.25:0.8,g.wrap.clientWidth/2,g.wrap.clientHeight/2);});tools.appendChild(b);});
  g.wrap.appendChild(tools);parent.appendChild(g.wrap);
  g.k=1;g.tx=0;g.ty=0;
  g.svg.addEventListener("wheel",function(e){e.preventDefault();var r=g.svg.getBoundingClientRect();g.zoom(Math.exp(-e.deltaY*0.0015),e.clientX-r.left,e.clientY-r.top);},{passive:false});
  var drag=null;
  g.svg.addEventListener("pointerdown",function(e){if(e.target.closest(".g-node"))return;drag={x:e.clientX,y:e.clientY,tx:g.tx,ty:g.ty};g.svg.setPointerCapture(e.pointerId);g.svg.classList.add("panning");});
  g.svg.addEventListener("pointermove",function(e){if(!drag)return;g.tx=drag.tx+e.clientX-drag.x;g.ty=drag.ty+e.clientY-drag.y;g.apply();});
  g.svg.addEventListener("pointerup",function(){drag=null;g.svg.classList.remove("panning");});
}
Graph.prototype.apply=function(){this.vp.setAttribute("transform","translate("+this.tx+","+this.ty+") scale("+this.k+")");};
Graph.prototype.zoom=function(f,x,y){var k=Math.max(0.12,Math.min(3,this.k*f));this.tx=x-(x-this.tx)*(k/this.k);this.ty=y-(y-this.ty)*(k/this.k);this.k=k;this.apply();};
Graph.prototype.fit=function(){
  var b=this.bounds;if(!b)return;var W=this.wrap.clientWidth||800,H=this.wrap.clientHeight||500,pad=30;
  var k=Math.min(1.15,Math.min((W-2*pad)/Math.max(b.w,1),(H-2*pad)/Math.max(b.h,1)));
  if(k<0.8){k=0.8;var r=this.focusBox||b;this.k=k;
    if(this.model&&this.model.dir==="LR"){this.tx=W/2-(r.x+r.w/2)*k;this.ty=H/2-(r.y+r.h/2)*k;}
    else{this.tx=W/2-(r.x+r.w/2)*k;this.ty=pad-(r.y)*k;}
    this.apply();return;}
  this.k=k;this.tx=W/2-(b.x+b.w/2)*k;this.ty=H/2-(b.y+b.h/2)*k;this.apply();
};
Graph.prototype.center=function(id){var p=this.pos[id];if(!p)return;var W=this.wrap.clientWidth,H=this.wrap.clientHeight;this.tx=W/2-(p.x+p.w/2)*this.k;this.ty=H/2-(p.y+p.h/2)*this.k;this.apply();};
// model: {nodes:[{id,lines,cls,title,aria}], edges:[{from,to,label,cls,back,title}], rank:{id:r}, dir:"TB"|"LR", focus:id}
Graph.prototype.draw=function(m,onSelect){
  var g=this;g.vp.textContent="";g.onSelect=onSelect;g.model=m;
  var F="11.5px "+MONO,LH=15,PADX=10,PADY=7,GAP=m.dir==="LR"?70:46,SEP=m.dir==="LR"?14:22;
  var ranks={},order={};m.nodes.forEach(function(n){var r=m.rank[n.id]||0;(ranks[r]=ranks[r]||[]).push(n.id);});
  var keys=Object.keys(ranks).map(Number).sort(function(a,b){return a-b;});
  keys.forEach(function(r){ranks[r].forEach(function(id,i){order[id]=i;});});
  var nb={};m.edges.forEach(function(e){if(e.back)return;(nb[e.from]=nb[e.from]||[]).push(e.to);(nb[e.to]=nb[e.to]||[]).push(e.from);});
  for(var it=0;it<6;it++){keys.forEach(function(r,ki){if(ki===0)return;var prev=keys[ki-1];ranks[r].sort(function(a,b){return bary(a)-bary(b);});function bary(id){var ns=(nb[id]||[]).filter(function(x){return m.rank[x]===prev;});if(!ns.length)return order[id];return ns.reduce(function(s,x){return s+order[x];},0)/ns.length;}ranks[r].forEach(function(id,i){order[id]=i;});});}
  var size={};m.nodes.forEach(function(n){var w=0;n.lines.forEach(function(l){w=Math.max(w,tw(l.t,(l.b?"600 ":"")+F));});size[n.id]={w:Math.min(Math.max(w+2*PADX,60),460),h:n.lines.length*LH+2*PADY};});
  var pos={},cur=0;
  keys.forEach(function(r){
    var ids=ranks[r],thick=0,len=0;ids.forEach(function(id){thick=Math.max(thick,m.dir==="LR"?size[id].w:size[id].h);len+=(m.dir==="LR"?size[id].h:size[id].w)+SEP;});
    var off=-len/2;
    ids.forEach(function(id){var sz=size[id];if(m.dir==="LR"){pos[id]={x:cur,y:off,w:sz.w,h:sz.h};off+=sz.h+SEP;}else{pos[id]={x:off,y:cur,w:sz.w,h:sz.h};off+=sz.w+SEP;}});
    cur+=thick+GAP;
  });
  g.pos=pos;
  var minX=1e9,minY=1e9,maxX=-1e9,maxY=-1e9;Object.keys(pos).forEach(function(id){var p=pos[id];minX=Math.min(minX,p.x);minY=Math.min(minY,p.y);maxX=Math.max(maxX,p.x+p.w);maxY=Math.max(maxY,p.y+p.h);});
  g.bounds={x:minX,y:minY,w:maxX-minX,h:maxY-minY};g.focusBox=m.focus&&pos[m.focus];
  var eg=s("g"),ng=s("g");g.vp.appendChild(eg);g.vp.appendChild(ng);g.edgeEls=[];
  m.edges.forEach(function(e,i){
    var a=pos[e.from],b=pos[e.to];if(!a||!b)return;var d,lx,ly;
    if(m.dir==="LR"){
      var x1=a.x+a.w,y1=a.y+a.h/2,x2=b.x,y2=b.y+b.h/2;
      if(x2>x1+5){var dx=Math.max(24,(x2-x1)/2);d="M"+x1+","+y1+" C"+(x1+dx)+","+y1+" "+(x2-dx)+","+y2+" "+x2+","+y2;lx=(x1+x2)/2;ly=(y1+y2)/2-4;}
      else{var lift=Math.max(a.h,Math.abs(y2-y1)/2)+26;d="M"+x1+","+y1+" C"+(x1+50)+","+(y1+lift)+" "+(x2-50)+","+(y2+lift)+" "+x2+","+y2;lx=(x1+x2)/2;ly=Math.max(y1,y2)+lift-6;}
    }else{
      var X1=a.x+a.w/2,Y1=a.y+a.h,X2=b.x+b.w/2,Y2=b.y;
      if(Y2>Y1+5){var dy=Math.max(20,(Y2-Y1)/2);d="M"+X1+","+Y1+" C"+X1+","+(Y1+dy)+" "+X2+","+(Y2-dy)+" "+X2+","+Y2;lx=(X1+X2)/2;ly=(Y1+Y2)/2;}
      else{var sx=Math.max(a.x+a.w,b.x+b.w)+30+((i%3)*10);d="M"+(a.x+a.w)+","+(a.y+a.h/2)+" C"+sx+","+(a.y+a.h/2)+" "+sx+","+(b.y+b.h/2)+" "+(b.x+b.w)+","+(b.y+b.h/2);lx=sx;ly=(a.y+b.y)/2+a.h/2;}
    }
    var p=s("path",{d:d,class:"g-edge "+(e.cls||"")+(e.back?" back":""),"marker-end":"url(#m-"+(e.cls&&e.cls.indexOf("err")>=0?"gb":"ga")+")"});
    if(e.title){var t=s("title");t.textContent=e.title;p.appendChild(t);}
    eg.appendChild(p);g.edgeEls[i]=p;
    if(e.label){var lb=s("text",{class:"g-label"+(e.cls&&e.cls.indexOf("err")>=0?" err":""),x:String(lx+4),y:String(ly),"text-anchor":"start"});lb.textContent=clip(e.label,40);eg.appendChild(lb);}
  });
  g.nodeEls={};
  m.nodes.forEach(function(n){
    var p=pos[n.id];
    var el=s("g",{class:"g-node "+(n.cls||""),transform:"translate("+p.x+","+p.y+")",tabindex:"0",role:"button","data-id":n.id,"aria-label":n.aria||n.lines.map(function(l){return l.t;}).join(" ")});
    el.appendChild(s("rect",{width:String(p.w),height:String(p.h),rx:n.round?"12":"5"}));
    n.lines.forEach(function(l,i){var t=s("text",{x:String(PADX),y:String(PADY+11+i*LH),class:l.sub?"sub":""});if(l.b)t.setAttribute("font-weight","600");var w=p.w-2*PADX,str=l.t;while(str.length>4&&tw(str,(l.b?"600 ":"")+F)>w)str=str.slice(0,-2);if(str!==l.t)str=str.slice(0,-1)+"…";t.textContent=str;el.appendChild(t);});
    if(n.title){var tt=s("title");tt.textContent=n.title;el.appendChild(tt);}
    el.addEventListener("click",function(){g.select(n.id);});
    el.addEventListener("keydown",function(e){if(e.key==="Enter"||e.key===" "){e.preventDefault();g.select(n.id);}});
    ng.appendChild(el);g.nodeEls[n.id]=el;
  });
};
Graph.prototype.select=function(id){var g=this;Object.keys(g.nodeEls).forEach(function(k){g.nodeEls[k].classList.toggle("sel",k===String(id));});if(g.onSelect)g.onSelect(id);};

function detailRows(dl,rows){rows.forEach(function(r){if(r[1]==null||r[1]==="")return;dl.appendChild(h("dt",null,r[0]));var dd=h("dd");if(typeof r[1]==="string"||typeof r[1]==="number")dd.textContent=String(r[1]);else dd.appendChild(r[1]);dl.appendChild(dd);});}
function codeLines(code,from,mark){var pre=h("pre",{class:"insp-pre"});(code||"").split("\n").forEach(function(l,i){var n=from+i,line=(String(n)+"     ").slice(0,5)+" "+l;if(mark&&mark.indexOf(n)>=0)pre.appendChild(h("span",{class:"hl"},line));else pre.appendChild(txt(line));pre.appendChild(txt("\n"));});return pre;}
function fnCode(b,lines){if(!b.code)return null;return codeLines(b.code,b.symbol.line,lines);}

// ---------- Code Flow ----------
function tabCodeFlow(b){
  var r=b.symbol;
  var bar=add(h("div",{class:"insp-bar"}),badge("static","static analysis"),txt("Callers and callees from the Code Flow index of this report. "));
  body.appendChild(bar);
  if(!FLOW||!FLOW.nodes){body.appendChild(msg("This report has no Code Flow index (export it with the Code Flow option)."));return;}
  var nodes={};FLOW.nodes.forEach(function(n){nodes[n.id]=n;});
  if(!nodes[r.id]){body.appendChild(msg("No matching symbol found in the Code Flow index."));return;}
  var a=h("a",{href:"#codeflow:"+encodeURIComponent(r.id),class:"ghost small"},"Open in Code Flow graph");
  body.appendChild(h("p",null)).appendChild(a);
  var grid=h("div",{class:"insp-two"}),left=h("div",{class:"insp-panel"}),right=h("div",{class:"insp-panel"});add(body,add(grid,left,right));
  function list(title,edges,other,el){el.appendChild(h("h4",null,title+" ("+edges.length+")"));if(!edges.length){el.appendChild(h("p",{class:"insp-note"},"None found."));return;}var t=h("table",{class:"insp-table"});var tb=h("tbody");edges.forEach(function(e){var n=nodes[other(e)];if(!n)return;var tr=h("tr");add(tr,h("td",{class:"mono"},n.name),h("td",null,e.conf+(e.kind!=="call"?" · "+e.kind:"")),add(h("td"),locLink(e.file,e.line)));tb.appendChild(tr);});add(el,add(t,tb));}
  list("Called by",(FLOW.edges||[]).filter(function(e){return e.to===r.id;}),function(e){return e.from;},left);
  list("Calls",(FLOW.edges||[]).filter(function(e){return e.from===r.id;}),function(e){return e.to;},right);
  var n=nodes[r.id];if(n&&n.explain)left.appendChild(h("p",{class:"insp-note"},n.explain));
}

// ---------- Data Flow ----------
function dfModel(res){
  var nodes=res.nodes.slice(),edges=res.edges.slice();
  var keep={};nodes.forEach(function(n){keep[n.id]=Math.abs(n.level||0)<=S.dfDepth;});
  edges=edges.filter(function(e){return keep[e.from]&&keep[e.to];});
  if(S.dfHide){
    var roots={};(res.roots||[]).forEach(function(id){roots[id]=1;});
    nodes.forEach(function(n){
      if(!n.intermediate||roots[n.id])return;
      var ins=edges.filter(function(e){return e.to===n.id&&e.from!==n.id;}),outs=edges.filter(function(e){return e.from===n.id&&e.to!==n.id;});
      edges=edges.filter(function(e){return e.from!==n.id&&e.to!==n.id;});
      ins.forEach(function(a){outs.forEach(function(b2){edges.push({from:a.from,to:b2.to,kind:b2.kind==="assign"?a.kind:b2.kind,conf:weaker(a.conf,b2.conf),label:[a.label,b2.label].filter(Boolean).join(" · "),file:b2.file,line:b2.line,dir:b2.dir});});});
      keep[n.id]=false;
    });
  }
  edges=edges.filter(function(e){return S.dfDir==="both"||e.dir===S.dfDir;});
  var used={};(res.roots||[]).forEach(function(id){used[id]=1;});edges.forEach(function(e){used[e.from]=1;used[e.to]=1;});
  // rank: signed distance from the selection
  var rank={};(res.roots||[]).forEach(function(id){rank[id]=0;});
  var changed=true,guard=0;
  while(changed&&guard++<200){changed=false;edges.forEach(function(e){if(e.dir==="origin"&&rank[e.to]!=null){var r=rank[e.to]-1;if(rank[e.from]==null||r<rank[e.from]&&rank[e.from]<=0){if(rank[e.from]!==r){rank[e.from]=r;changed=true;}}}if(e.dir==="destination"&&rank[e.from]!=null){var r2=rank[e.from]+1;if(rank[e.to]==null||(r2<rank[e.to]&&rank[e.to]>=0)){if(rank[e.to]!==r2){rank[e.to]=r2;changed=true;}}}});}
  var gnodes=[];res.nodes.forEach(function(n){if(!keep[n.id]||!used[n.id]||rank[n.id]==null)return;var cls="k-"+n.kind+((res.roots||[]).indexOf(n.id)>=0?" root":"");var lines=[{t:(n.kind==="source"?"⇢ ":n.kind==="sql"?"⛁ ":n.kind==="check"?"✓ ":"")+n.label,b:true},{t:(n.funcName||"")+(n.line?"  :"+n.line:""),sub:true}];if(n.var&&n.label.indexOf(n.var)<0)lines.splice(1,0,{t:"var "+n.var,sub:true});gnodes.push({id:String(n.id),lines:lines,cls:cls,title:(n.code||"")+(n.detail?"\n"+n.detail:""),round:n.kind==="source"||n.kind==="sql"});});
  var gedges=edges.filter(function(e){return rank[e.from]!=null&&rank[e.to]!=null;}).map(function(e){return {from:String(e.from),to:String(e.to),label:e.kind,cls:"c-"+e.conf,title:e.kind+" ["+e.conf+"] "+(e.label||"")+(e.file?" at "+e.file+":"+e.line:"")};});
  var r2={};Object.keys(rank).forEach(function(k){r2[String(k)]=rank[k];});
  return {nodes:gnodes,edges:gedges,rank:r2,dir:"LR",focus:String((res.roots||[0])[0])};
}
var CONFR={exact:4,derived:3,likely:2,possible:1,unresolved:0};
function weaker(a,b){return (CONFR[a]||0)<=(CONFR[b]||0)?a:b;}
function tabDataFlow(b){
  var keys=Object.keys(b.dataflow||{});
  body.appendChild(add(h("div",{class:"insp-bar"}),badge("static","static analysis (SSA)"),txt("Possible flows on some execution path, not observed values. Edge style shows the confidence.")));
  if(!keys.length){body.appendChild(msg((b.variables&&b.variables.length)?"No data flow traces were computed for this function in this report.":"This function has no variables to trace."));return;}
  if(!S.df||keys.indexOf(S.df)<0){
    // Start with the variable whose trace reaches the most: an input source
    // or a SQL operation first, then any non-receiver parameter.
    var best=-1;S.df=keys[0];
    keys.forEach(function(k,i){var r=b.dataflow[k],sc=0;(r.nodes||[]).forEach(function(n){if(n.kind==="source"&&n.label.indexOf("HTTP")===0)sc=Math.max(sc,4);if(n.kind==="sql")sc=Math.max(sc,3);if(n.kind==="source")sc=Math.max(sc,2);});if(sc===0&&r.selection&&r.selection.kind==="param"&&i>0&&r.selection.name!=="ctx")sc=1;if(sc>best){best=sc;S.df=k;}});
  }
  var bar=h("div",{class:"insp-bar"});
  var vs=h("select",{"aria-label":"Variable"});keys.forEach(function(k){var r=b.dataflow[k],sel=r.selection;vs.appendChild(h("option",{value:k},(sel.kind==="param"?"param ":"")+sel.name+" "+(sel.type||"")+"  (line "+sel.line+")"));});vs.value=S.df;
  vs.addEventListener("change",function(){S.df=vs.value;render();});
  var seg=h("span",{class:"seg",role:"group","aria-label":"Direction"});[["origin","Trace Origin"],["both","Trace Both"],["destination","Trace Destination"]].forEach(function(d){var bt=h("button",{type:"button","aria-pressed":String(S.dfDir===d[0])},d[1]);bt.addEventListener("click",function(){S.dfDir=d[0];render();});seg.appendChild(bt);});
  var dep=h("select",{"aria-label":"Depth"});for(var i=0;i<=(b.dataflow[S.df].maxDepth||3);i++){var o=h("option",{value:String(i)},String(i));dep.appendChild(o);}dep.value=String(Math.min(S.dfDepth,b.dataflow[S.df].maxDepth||3));dep.addEventListener("change",function(){S.dfDepth=+dep.value;render();});
  var hid=h("input",{type:"checkbox"});hid.checked=S.dfHide;hid.addEventListener("change",function(){S.dfHide=hid.checked;render();});
  add(bar,add(h("label"),txt("Variable "),vs),seg,add(h("label"),txt("Call depth "),dep),add(h("label"),hid,txt("Hide intermediate values")));
  body.appendChild(bar);
  var res=b.dataflow[S.df];
  var grid=h("div",{class:"insp-grid"}),left=h("div"),panel=h("aside",{class:"insp-panel","aria-label":"Details"});add(body,add(grid,left,panel));
  var gr=new Graph(left,{label:"Data flow graph"});
  var m=dfModel(res);
  var byId={};res.nodes.forEach(function(n){byId[String(n.id)]=n;});
  function show(id){
    var n=byId[id];panel.textContent="";if(!n)return;
    add(panel,h("div",{class:"insp-note"},n.kind+(n.dir?" · "+n.dir:"")+" · confidence "+(n.conf||"")),h("h3",null,n.label));
    var dl=h("dl");detailRows(dl,[["Variable",n.var],["Type",n.type],["Function",n.funcName],["Location",locLink(n.file,n.line)],["Call level",String(n.level||0)],["Detail",n.detail],["Tags",(n.tags||[]).join(", ")]]);panel.appendChild(dl);
    if(n.code){panel.appendChild(h("h4",null,"Go code responsible"));panel.appendChild(codeLines(n.code,n.line,[n.line]));}
    var ins=res.edges.filter(function(e){return String(e.to)===id;}),outs=res.edges.filter(function(e){return String(e.from)===id;});
    [["Comes from",ins,function(e){return e.from;}],["Flows to",outs,function(e){return e.to;}]].forEach(function(sec){if(!sec[1].length)return;panel.appendChild(h("h4",null,sec[0]));var ul=h("ul",{class:"insp-note"});sec[1].forEach(function(e){var o=byId[String(sec[2](e))];if(!o)return;var li=h("li");var a=h("a",{href:"#"},o.label);a.addEventListener("click",function(ev){ev.preventDefault();gr.select(String(o.id));gr.center(String(o.id));});add(li,a,txt("  "+e.kind+" ["+e.conf+"] "+(e.label||"")));ul.appendChild(li);});panel.appendChild(ul);});
  }
  gr.draw(m,function(id){show(id);});
  setTimeout(function(){gr.fit();},0);
  var info=h("div",{class:"insp-note"});
  var src=(res.sources||[]).map(function(id){return byId[String(id)];}).filter(Boolean).map(function(n){return n.label;});
  add(info,txt((res.validations&&res.validations.length?res.validations.length+" validation(s): "+res.validations.map(function(v){return v.text+" ("+v.func+")";}).join("; ")+". ":"")+(src.length?"Sources: "+src.join("; ")+". ":"")+(res.status&&res.status.status!=="complete"?"Result "+res.status.status+": "+(res.status.reasons||[]).join("; ")+".":"")));
  left.appendChild(info);
  (res.notes||[]).forEach(function(nn){left.appendChild(h("div",{class:"insp-note"},nn));});
  show(String((res.roots||[0])[0]));
}

// ---------- Branch Explorer ----------
function cfgRanks(g){
  var out={},indeg={},fwd={};g.blocks.forEach(function(b){fwd[b.id]=[];indeg[b.id]=0;});
  g.edges.forEach(function(e){if(e.kind==="loop")return;fwd[e.from].push(e.to);indeg[e.to]++;});
  var rank={},queue=[];g.blocks.forEach(function(b){if(indeg[b.id]===0)queue.push(b.id);rank[b.id]=0;});
  var seen=0;
  while(queue.length){var id=queue.shift();seen++;fwd[id].forEach(function(t){rank[t]=Math.max(rank[t],rank[id]+1);if(--indeg[t]===0)queue.push(t);});}
  var maxR=0;g.blocks.forEach(function(b){if(b.reachable)maxR=Math.max(maxR,rank[b.id]);});
  g.blocks.forEach(function(b){out[String(b.id)]=b.reachable?rank[b.id]:maxR+1+(rank[b.id]||0);});
  return out;
}
function blockLines(b){
  var lines=[{t:"B"+b.id+(b.entry?"  entry":"")+(b.loopHead?"  loop head":"")+(b.terminal&&b.terminal!=="return"?"  "+b.terminal:""),sub:true}];
  var st=b.stmts||[];st.slice(0,6).forEach(function(x){lines.push({t:x.text});});
  if(st.length>6)lines.push({t:"… "+(st.length-6)+" more",sub:true});
  if(b.decision){var c=b.decision==="if"?"if "+b.cond:b.decision==="for"?"for "+b.cond:b.decision==="range"?"for "+b.cond:b.decision==="select"?"select":"switch "+b.cond;lines.push({t:c+(b.decision==="select"||b.decision==="switch"||b.decision==="type switch"?"":" ?"),b:true});}
  if(!st.length&&!b.decision)lines.push({t:b.entry?"(start)":"(empty)",sub:true});
  return lines;
}
function tabBranches(b){
  var g=b.branches;
  body.appendChild(add(h("div",{class:"insp-bar"}),badge("static","static analysis (go/cfg)"),txt("Structural paths: allowed by the code, not observed; some may be infeasible.")));
  if(!g){body.appendChild(msg((b.errors&&b.errors.branches)||"No control-flow graph for this function.",true));return;}
  var m=g.metrics,kv=h("div",{class:"insp-kv"});
  [["blocks",m.blocks],["edges",m.edges],["decisions",m.decisionPoints],["cyclomatic",m.cyclomatic],["returns",m.returns],["panic/exit",m.panics],["loops",m.loops],["unreachable",m.unreachable],["paths",m.paths+(m.pathsSaturated?"+":"")]].forEach(function(x){add(kv,add(h("span"),h("b",null,String(x[1])),txt(" "+x[0])));});
  body.appendChild(kv);
  if(g.coverage){var c=g.coverage;body.appendChild(add(h("div",{class:"insp-kv"}),badge("observed","coverage "+c.profile),txt(c.covered+" covered · "+c.uncovered+" not covered · "+c.partial+" partial · "+c.unknown+" unknown — "+c.note)));}
  else body.appendChild(h("div",{class:"insp-note"},"No coverage data available."));
  var bar=h("div",{class:"insp-bar"});var ps=h("select",{"aria-label":"Highlight path"});ps.appendChild(h("option",{value:"-1"},"Highlight a path…"));
  (g.paths||[]).forEach(function(p,i){var end=g.blocks[p.end];ps.appendChild(h("option",{value:String(i)},(i+1)+". "+(p.steps&&p.steps.length?p.steps.join(" · "):"(no decisions)")+" ⇒ "+(end?clip(end.stmts&&end.stmts.length?end.stmts[end.stmts.length-1].text:end.terminal,40):"")+(p.error?"  [error]":"")));});
  ps.value=String(S.cfgPath);ps.addEventListener("change",function(){S.cfgPath=+ps.value;highlight();});
  add(bar,ps);if(m.pathsListed<m.paths)add(bar,txt(m.pathsListed+" of "+m.paths+" paths listed"));body.appendChild(bar);
  var grid=h("div",{class:"insp-grid"}),left=h("div"),panel=h("aside",{class:"insp-panel","aria-label":"Block details"});add(body,add(grid,left,panel));
  var gr=new Graph(left,{label:"Control-flow graph"});
  var nodes=g.blocks.map(function(bl){var cls=[];if(bl.decision)cls.push("dec");if(bl.terminal)cls.push("term");if(bl.terminal==="panic"||bl.terminal==="exit"||bl.returnsError)cls.push("bad");if(!bl.reachable)cls.push("dead");if(bl.coverage)cls.push("cov-"+bl.coverage);return {id:String(bl.id),lines:blockLines(bl),cls:cls.join(" "),aria:"Block "+bl.id+" line "+bl.line};});
  var edges=g.edges.map(function(e){return {from:String(e.from),to:String(e.to),label:e.label||"",back:e.kind==="loop",cls:(e.error?"err":""),title:(e.label||e.kind)+(e.error?" (error branch)":"")};});
  gr.draw({nodes:nodes,edges:edges,rank:cfgRanks(g),dir:"TB",focus:"0"},function(id){show(+id);});
  setTimeout(function(){gr.fit();},0);
  function highlight(){var p=S.cfgPath>=0?g.paths[S.cfgPath]:null,on={},onE={};if(p){p.blocks.forEach(function(x){on[x]=1;});p.edges.forEach(function(x){onE[x]=1;});}
    Object.keys(gr.nodeEls).forEach(function(k){gr.nodeEls[k].classList.toggle("onpath",!!p&&!!on[k]);gr.nodeEls[k].classList.toggle("dim",!!p&&!on[k]);});
    gr.edgeEls.forEach(function(el,i){if(!el)return;el.classList.toggle("onpath",!!p&&!!onE[i]);el.classList.toggle("dim",!!p&&!onE[i]);});}
  function show(id){
    var bl=g.blocks[id];panel.textContent="";if(!bl)return;
    add(panel,h("div",{class:"insp-note"},(bl.decision?bl.decision+" decision":"block")+(bl.terminal?" · exits: "+bl.terminal:"")+(bl.reachable?"":" · unreachable")),h("h3",null,"B"+bl.id+"  "+loc(bl.file,bl.line)+(bl.end&&bl.end!==bl.line?"–"+bl.end:"")));
    var dl=h("dl");detailRows(dl,[["Condition",bl.cond],["Location",locLink(bl.file,bl.line)],["Coverage",bl.coverage?bl.coverage+(bl.coverCount?" ("+bl.coverCount+")":""):"no coverage data"],["Error check",bl.errorCheck?"yes":""],["Returns an error",bl.returnsError?"yes":""],["Defers",bl.defers?String(bl.defers):""]]);panel.appendChild(dl);
    if((bl.stmts||[]).length){panel.appendChild(h("h4",null,"Statements"));var ul=h("pre",{class:"insp-pre"});bl.stmts.forEach(function(x){ul.appendChild(txt(((x.line||"")+"     ").slice(0,5)+" "+x.text+"\n"));});panel.appendChild(ul);}
    var outs=g.edges.filter(function(e){return e.from===id;});if(outs.length){panel.appendChild(h("h4",null,"Successors"));var u2=h("ul",{class:"insp-note"});outs.forEach(function(e){var li=h("li");var a=h("a",{href:"#"},(e.label?e.label+" → ":"")+"B"+e.to);a.addEventListener("click",function(ev){ev.preventDefault();gr.select(String(e.to));gr.center(String(e.to));});add(li,a,txt(e.kind==="loop"?"  (loop back)":""));u2.appendChild(li);});panel.appendChild(u2);}
    var lines=[];for(var l=bl.line;l<=(bl.end||bl.line);l++)lines.push(l);if(b.code){panel.appendChild(h("h4",null,"Code"));panel.appendChild(fnCode(b,lines));}
  }
  show(0);highlight();
  (g.notes||[]).forEach(function(n){left.appendChild(h("div",{class:"insp-note"},n));});
  if(g.defers&&g.defers.length)left.appendChild(h("div",{class:"insp-note"},"Deferred calls (run on every exit after registration): "+g.defers.map(function(d){return d.text+" (line "+d.line+")";}).join("; ")));
}

// ---------- SQL Explorer ----------
function planTree(n,analyzed){
  var li=h("li");var t=n.nodeType+(n.joinType&&n.nodeType.indexOf(n.joinType)<0?" ("+n.joinType+")":"")+(n.relation?" on "+n.relation+(n.alias&&n.alias!==n.relation?" "+n.alias:""):"")+(n.index?" using "+n.index:"");
  add(li,h("span",{class:"pn"},t),h("div",{class:"pc"},"cost "+n.startupCost.toFixed(2)+".."+n.totalCost.toFixed(2)+" · est. rows "+n.planRows+" · width "+n.planWidth+(analyzed&&n.actualRows!=null?" · actual rows "+n.actualRows+" × "+(n.actualLoops||1)+" loops · "+(n.actualTotalMs||0).toFixed(3)+" ms":"")));
  (n.conditions||[]).concat(n.keys||[]).forEach(function(c){li.appendChild(h("div",{class:"pc"},c.name+": "+c.value));});
  if(n.rowsRemovedByFilter!=null)li.appendChild(h("div",{class:"pc"},"rows removed by filter: "+n.rowsRemovedByFilter));
  if(n.buffers)li.appendChild(h("div",{class:"pc"},"buffers: "+n.buffers));
  if(n.plans&&n.plans.length){var ul=h("ul");n.plans.forEach(function(c){ul.appendChild(planTree(c,analyzed));});li.appendChild(ul);}
  return li;
}
function planBox(p){
  var box=h("div",{class:"sqlbox plan"});
  add(box,add(h("div",{class:"insp-kv"}),badge(p.analyzed?"observed":"estimated",p.analyzed?"EXPLAIN ANALYZE":"EXPLAIN"),txt(p.target+" · PostgreSQL "+p.server+" · "+p.mode)));
  var ul=h("ul");ul.appendChild(planTree(p.root,p.analyzed));box.appendChild(ul);
  var info=[];if(p.indexes&&p.indexes.length)info.push("Indexes: "+p.indexes.join(", "));if(p.planningMs!=null)info.push("planning "+p.planningMs.toFixed(3)+" ms");if(p.executionMs!=null)info.push("execution "+p.executionMs.toFixed(3)+" ms (one run)");if(p.paramsGiven)info.push(p.paramsGiven+" parameter value(s) supplied, not recorded");
  if(info.length)box.appendChild(h("div",{class:"insp-note"},info.join(" · ")));
  (p.warnings||[]).forEach(function(w){box.appendChild(h("div",{class:"insp-note"},w));});
  return box;
}
function tabSQL(b){
  var r=b.sql;
  body.appendChild(add(h("div",{class:"insp-bar"}),badge("static","static discovery"),txt("pgx, pgxpool, database/sql, sqlx. Operations of the function and of the module functions it may call; nothing was executed.")));
  if(!r||!r.ops||!r.ops.length){body.appendChild(msg("No database operations found"));return;}
  var plans=D.plans||[];
  r.ops.forEach(function(op,i){
    var box=h("div",{class:"sqlbox"});
    var head=h("h3",null,(i+1)+". "+op.lib+" "+op.op);
    add(box,head,add(h("div",{class:"insp-kv"}),txt(op.method+" on "+op.receiver+" · in "+op.func.qualified+" · "),locLink(op.file,op.line),op.conf!=="exact"?badge("estimated",op.conf):null,op.deferred?badge("static","deferred"):null));
    if(op.via&&op.via.length)box.appendChild(h("div",{class:"insp-note"},"Reached via "+op.via.join(" → ")));
    if(op.call)box.appendChild(h("pre",{class:"insp-pre"},op.call));
    var q=op.sql;
    if(q.kind!=="none"){
      if(q.text){box.appendChild(h("div",{class:"insp-note"},"SQL ("+q.kind+(q.constant?" "+q.constant+" at "+loc(q.constFile,q.constLine):"")+", "+q.statement+")"));box.appendChild(h("pre",{class:"insp-pre"},q.formatted||q.text));}
      else if(q.variants&&q.variants.length){box.appendChild(h("div",{class:"insp-note"},"One of "+q.variants.length+" literal statements ("+q.kind+")"));q.variants.forEach(function(v){box.appendChild(h("pre",{class:"insp-pre"},v));});}
      else if(q.template){box.appendChild(h("div",{class:"insp-msg warn"},"SQL query not statically resolved — "+q.kind+": ⟨…⟩ parts are computed at runtime"));box.appendChild(h("pre",{class:"insp-pre"},q.formatted||q.template));}
      else box.appendChild(h("div",{class:"insp-msg warn"},"SQL query not statically resolved ("+q.kind+")"+(q.note?": "+q.note:"")));
      if(q.note&&(q.text||q.variants))box.appendChild(h("div",{class:"insp-note"},q.note));
    }
    if(op.params&&op.params.length){
      var t=h("table",{class:"insp-table"}),th=h("thead"),tb=h("tbody");add(th,add(h("tr"),h("th",null,"param"),h("th",null,"Go expression"),h("th",null,"type"),h("th",null,"origin (Data Flow)")));
      op.params.forEach(function(p){var o=h("td");(p.origins||[]).forEach(function(or){add(o,add(h("div"),txt(or.label+" ["+or.conf+"] "),locLink(or.file,or.line)));});if(p.originStatus==="partial")o.appendChild(h("div",{class:"insp-note"},"origin search stopped at its limits"));add(tb,add(h("tr"),h("td",{class:"mono"},"$"+p.index),h("td",{class:"mono"},p.expr),h("td",{class:"mono"},p.type||""),o));});
      add(box,add(t,th,tb));
    }
    if(op.paramsKnown===false)box.appendChild(h("div",{class:"insp-msg warn"},"Parameters passed as a slice: not known statically."));
    if(op.scan){var sc=op.scan;box.appendChild(add(h("div",{class:"insp-note"}),txt(sc.note?sc.method+": "+sc.note:(sc.inLoop?"rows.Scan (each row) into ":"Scan into ")+(sc.targets||[]).map(function(x){return x.expr+(x.type?" ("+x.type+")":"");}).join(", ")+"  "),locLink(sc.file,sc.line)));}
    if(op.tx){var tx=op.tx;box.appendChild(h("div",{class:"insp-note"},"Transaction: "+tx.role+(tx.var?" ("+tx.var+")":"")+(tx.fromParam?" — received as parameter":tx.beginLine&&tx.role!=="begin"?", begun at line "+tx.beginLine:"")+(tx.deferred?", deferred":"")));}
    (op.notes||[]).forEach(function(n){box.appendChild(h("div",{class:"insp-msg warn"},n));});
    plans.forEach(function(p){if(q.text&&p.sql===q.text.trim())box.appendChild(planBox(p));});
    body.appendChild(box);
  });
  if(r.transactions&&r.transactions.length){body.appendChild(h("h4",null,"Transactions"));r.transactions.forEach(function(t){body.appendChild(h("div",{class:"insp-note"},t.var+" begun at line "+t.line+" in "+t.func+": "+t.ops.length+" operation(s); commit at "+(t.commits||[]).join(",")+"; rollback at "+(t.rollbacks||[]).join(",")+(t.deferredRollback?" (deferred)":"")));});}
}

// ---------- Performance ----------
function benchStats(run){
  var g={},order=[];(run.results||[]).forEach(function(r){(r.units||[]).forEach(function(u){var k=r.pkg+"|"+r.name+"|"+r.procs+"|"+u;if(!g[k]){g[k]={pkg:r.pkg,name:r.name,procs:r.procs,unit:u,v:[]};order.push(k);}g[k].v.push(r.metrics[u]);});});
  return order.map(function(k){var x=g[k],v=x.v.slice().sort(function(a,b){return a-b;}),n=v.length;x.n=n;x.min=v[0];x.max=v[n-1];x.median=n%2?v[(n-1)/2]:(v[n/2-1]+v[n/2])/2;return x;});
}
function num(v){return v>=1e6||v<0.01&&v!==0?v.toExponential(3):(Math.round(v*1000)/1000).toString();}
function tabPerf(b){
  var pv=b.performance||{},cpu=D.cpu,mem=D.mem;
  var bar0=add(h("div",{class:"insp-bar"}),badge("estimated","profiles: sampled estimates"),badge("observed","benchmarks: measurements"),badge("static","compiler diagnostics"));body.appendChild(bar0);
  if(!cpu&&!mem&&!(D.benches&&D.benches.length)&&!D.escape){body.appendChild(msg("No profiling data available"));return;}
  // symbol summary
  var sumBox=h("div",{class:"insp-kv"});
  if(pv.cpu)add(sumBox,add(h("span"),txt("CPU ("+pv.cpuProfile+"): "),h("b",null,pv.cpu.found?"flat "+pct(pv.cpu.flatPct)+" · cum "+pct(pv.cpu.cumPct):"no samples in this function")));
  if(pv.mem)add(sumBox,add(h("span"),txt("Memory ("+pv.memProfile+", "+pv.memType+"): "),h("b",null,pv.mem.found?"flat "+pct(pv.mem.flatPct)+" · cum "+pct(pv.mem.cumPct):"no samples in this function")));
  body.appendChild(sumBox);
  var bar=h("div",{class:"insp-bar"});
  var seg=h("span",{class:"seg",role:"group","aria-label":"Profile"});[["cpu","CPU",cpu],["mem","Memory",mem]].forEach(function(p){if(!p[2])return;var bt=h("button",{type:"button","aria-pressed":String(S.perf===p[0])},p[1]);bt.addEventListener("click",function(){S.perf=p[0];S.flameRoot=null;render();});seg.appendChild(bt);});
  if(!cpu&&S.perf==="cpu")S.perf="mem";if(!mem&&S.perf==="mem")S.perf="cpu";
  var mine=h("input",{type:"checkbox"});mine.checked=S.perfMine;mine.addEventListener("change",function(){S.perfMine=mine.checked;render();});
  var flt=h("input",{type:"search",placeholder:"Filter functions",value:S.perfFilter});flt.addEventListener("input",function(){S.perfFilter=flt.value;drawTable();});
  add(bar,seg,add(h("label"),mine,txt("Module functions only")),flt);body.appendChild(bar);
  var rep=S.perf==="cpu"?cpu:mem;
  if(rep){
    body.appendChild(add(h("div",{class:"insp-note"}),txt(rep.kind+" profile "+rep.artifact.name+" · "+rep.sampleType+" · total "+fmtVal(rep.total,rep.unit)+" · "+rep.samples+" samples"+(rep.period?" · "+rep.period:"")+" — "+rep.meaning)));
    (rep.notes||[]).forEach(function(n){body.appendChild(h("div",{class:"insp-note"},n));});
    body.appendChild(h("h4",null,"Flame graph"));
    var ff=h("input",{type:"search",placeholder:"Highlight frames",value:S.flameFind});var fbar=add(h("div",{class:"insp-bar"}),ff);var reset=h("button",{type:"button",class:"ghost small"},"Reset zoom");reset.addEventListener("click",function(){S.flameRoot=null;drawFlame();});fbar.appendChild(reset);body.appendChild(fbar);
    var flameBox=h("div",{class:"insp-canvas"});flameBox.style.height="auto";body.appendChild(flameBox);
    ff.addEventListener("input",function(){S.flameFind=ff.value.trim().toLowerCase();drawFlame();});
    function drawFlame(){
      flameBox.textContent="";var root=S.flameRoot||rep.flame;if(!root)return;
      var W=Math.max(flameBox.clientWidth||900,300),RH=18,maxD=0;(function md(n,d){maxD=Math.max(maxD,d);(n.children||[]).forEach(function(c){md(c,d+1);});})(root,0);
      maxD=Math.min(maxD,40);var svg=s("svg",{class:"flame",width:String(W),height:String((maxD+1)*RH),role:"img","aria-label":"Flame graph"});svg.style.width="100%";svg.style.height=((maxD+1)*RH)+"px";
      function walk(n,x,d,w){if(d>40||w<1)return;var g=s("g",{class:n.symbol?"mine":n.name.indexOf("runtime.")===0?"rt":"other"});if(S.flameFind&&n.name.toLowerCase().indexOf(S.flameFind)>=0)g.classList.add("hit");
        var r=s("rect",{x:String(x),y:String(d*RH),width:String(Math.max(w-0.5,0.5)),height:String(RH-1)});g.appendChild(r);
        var t=s("title");t.textContent=n.name+"\n"+fmtVal(n.value,rep.unit)+" ("+pct(100*n.value/Math.max(rep.total,1))+")"+(n.self?"\nself "+fmtVal(n.self,rep.unit):"");g.appendChild(t);
        if(w>40){var tx=s("text",{x:String(x+4),y:String(d*RH+13)});var lab=n.name,max=Math.floor((w-8)/6.6);tx.textContent=lab.length>max?lab.slice(0,Math.max(max-1,1))+"…":lab;g.appendChild(tx);}
        g.addEventListener("click",function(){if(n!==root){S.flameRoot=n;drawFlame();}});svg.appendChild(g);
        var cx=x;(n.children||[]).forEach(function(c){var cw=w*c.value/Math.max(n.value,1);walk(c,cx,d+1,cw);cx+=cw;});}
      walk(root,0,0,W);flameBox.appendChild(svg);
    }
    setTimeout(drawFlame,0);
    body.appendChild(h("h4",null,"Functions"));
    var tableBox=h("div");body.appendChild(tableBox);
    function drawTable(){
      tableBox.textContent="";var t=h("table",{class:"insp-table"}),th=h("thead"),tb=h("tbody");
      var hr=h("tr");[["flat","flat"],["flat%",""],["cum","cum"],["cum%",""],["function",""],["location",""]].forEach(function(c){var x=h("th",{class:c[1]?"sort":""},c[0]+(c[1]&&S.perfSort===c[1]?" ▾":""));if(c[1])x.addEventListener("click",function(){S.perfSort=c[1];drawTable();});hr.appendChild(x);});th.appendChild(hr);
      var rows=(rep.funcs||[]).slice();if(S.perfSort==="cum")rows.sort(function(a,b){return b.cum-a.cum;});
      var q=S.perfFilter.trim().toLowerCase(),shown=0;
      rows.forEach(function(f){if(S.perfMine&&!f.symbol)return;if(q&&f.name.toLowerCase().indexOf(q)<0)return;if(shown++>=200)return;var tr=h("tr",{class:(f.symbol?"mine":"other")+(f.symbol===b.symbol.id?" sel":"")});add(tr,h("td",{class:"num"},fmtVal(f.flat,rep.unit)),h("td",{class:"num"},pct(f.flatPct)),h("td",{class:"num"},fmtVal(f.cum,rep.unit)),h("td",{class:"num"},pct(f.cumPct)),h("td",{class:"mono"},f.name),add(h("td"),locLink(f.file,f.line)));
        if(f.symbol){tr.addEventListener("click",function(){var k=SYMS.findIndex(function(x){return x.symbol.id===f.symbol;});if(k>=0)setSym(k);});tr.title="Inspect this function (if included in the report)";}
        tb.appendChild(tr);});
      add(t,th,tb);tableBox.appendChild(t);if(shown>200)tableBox.appendChild(h("div",{class:"insp-note"},(shown-200)+" more — refine the filter."));
    }
    drawTable();
    var sp=S.perf==="cpu"?pv.cpu:pv.mem;
    if(sp&&sp.found){body.appendChild(h("h4",null,"This function in the profile"));var box=h("div",{class:"insp-note"});
      (sp.lines||[]).forEach(function(l){add(box,add(h("div"),txt("line "+l.line+": flat "+fmtVal(l.flat,rep.unit)+" · cum "+fmtVal(l.cum,rep.unit)+"  "),locLink(l.file,l.line)));});
      (sp.callers||[]).forEach(function(e){box.appendChild(h("div",null,"called by "+e.from+" — "+fmtVal(e.weight,rep.unit)));});
      (sp.callees||[]).forEach(function(e){box.appendChild(h("div",null,"calls "+e.to+" — "+fmtVal(e.weight,rep.unit)));});
      body.appendChild(box);}
  }
  (D.benches||[]).forEach(function(run){
    body.appendChild(h("h4",null,"Benchmarks · "+(run.artifact&&run.artifact.name||"")));
    var cfg=run.config||{};body.appendChild(h("div",{class:"insp-note"},["goos","goarch","pkg","cpu"].filter(function(k){return cfg[k];}).map(function(k){return k+"="+cfg[k];}).join(" · ")+(run.artifact&&run.artifact.command?" · $ "+run.artifact.command:"")));
    var t=h("table",{class:"insp-table"}),th=h("thead"),tb=h("tbody");add(th,add(h("tr"),h("th",null,"benchmark"),h("th",null,"unit"),h("th",null,"median"),h("th",null,"min"),h("th",null,"max"),h("th",null,"n")));
    benchStats(run).forEach(function(x){add(tb,add(h("tr",{class:x.pkg===b.symbol.pkg?"mine":"other"}),h("td",{class:"mono"},x.name+(x.procs?"-"+x.procs:"")),h("td",null,x.unit),h("td",{class:"num"},num(x.median)),h("td",{class:"num"},num(x.min)),h("td",{class:"num"},num(x.max)),h("td",{class:"num"},String(x.n))));});
    add(t,th,tb);body.appendChild(t);
  });
  if(D.compare){var c=D.compare;body.appendChild(h("h4",null,"Before / after comparison"));body.appendChild(h("div",{class:"insp-note"},c.method));(c.configDiffs||[]).forEach(function(d){body.appendChild(h("div",{class:"insp-msg warn"},"Conditions differ: "+d));});
    var t2=h("table",{class:"insp-table"}),th2=h("thead"),tb2=h("tbody");add(th2,add(h("tr"),h("th",null,"benchmark"),h("th",null,"unit"),h("th",null,"old median"),h("th",null,"new median"),h("th",null,"Δ"),h("th",null,"p"),h("th",null,"note")));
    (c.rows||[]).forEach(function(r){add(tb2,add(h("tr"),h("td",{class:"mono"},r.name),h("td",null,r.unit),h("td",{class:"num"},r.old?num(r.old.median):"-"),h("td",{class:"num"},r.new?num(r.new.median):"-"),h("td",{class:"num"},r.old&&r.new?(r.deltaPct>=0?"+":"")+r.deltaPct.toFixed(2)+"%":""),h("td",{class:"num"},r.p>=0?r.p.toFixed(3):""),h("td",null,(r.significant?"significant · ":"")+(r.note||""))));});
    add(t2,th2,tb2);body.appendChild(t2);if(c.benchstat)body.appendChild(h("pre",{class:"insp-pre"},c.benchstat));}
  if(D.escape){var e=D.escape;body.appendChild(h("h4",null,"Compiler diagnostics (go build -gcflags=-m=2)"));body.appendChild(h("div",{class:"insp-note"},"Verbatim compiler output for "+(e.packages||[]).join(", ")+"; no interpretation added."));
    var mineOnly=(pv.escape||[]);var list=mineOnly.length?mineOnly:[];if(!list.length)body.appendChild(h("div",{class:"insp-note"},"No diagnostics located in this function."));
    var t3=h("table",{class:"insp-table"}),tb3=h("tbody");list.forEach(function(d){var td=h("td",{class:"mono"},d.message);if(d.flow&&d.flow.length){var det=h("details");add(det,h("summary",null,"compiler explanation"),h("pre",{class:"insp-pre"},d.flow.join("\n")));td.appendChild(det);}add(tb3,add(h("tr"),h("td",null,d.kind),td,add(h("td"),locLink(d.file,d.line))));});add(t3,tb3);body.appendChild(t3);}
}

// ---------- Execution Trace ----------
function tabTrace(b){
  var T=D.trace,tv=b.trace||{};
  body.appendChild(add(h("div",{class:"insp-bar"}),badge("observed","events recorded by runtime/trace"),txt("The trace does not record every function call: per-function durations exist only for regions.")));
  if(!T){body.appendChild(msg("No runtime trace available"));return;}
  var st=T.stats||{};
  body.appendChild(add(h("div",{class:"insp-kv"}),txt((T.artifact&&T.artifact.name||"trace")+" · duration "+fmtNs(T.durationNs)+" · "+st.goroutines+" goroutines ("+st.userGoroutines+" non-runtime) · "+st.procs+" Ps · "+st.gcCycles+" GC cycles · "+(T.tasks||[]).length+" tasks · "+(T.regions||[]).length+" regions · "+(T.logs||[]).length+" logs")));
  if(T.artifact&&T.artifact.command)body.appendChild(h("div",{class:"insp-note mono"},"$ "+T.artifact.command));
  var sy=tv.symbol;
  if(sy){var box=h("div",{class:"insp-kv"});if(!sy.found)box.appendChild(txt(tv.message||"No event attributed to this function."));else{add(box,h("b",null,"This function:"),txt((sy.goroutines||[]).length+" goroutine(s) started here · "+sy.spanCount+" measured span(s)"+(sy.spanCount?" (total "+fmtNs(sy.spanTotalNs)+", min "+fmtNs(sy.spanMinNs)+", max "+fmtNs(sy.spanMaxNs)+")":"")+" · "+(sy.blocks||[]).length+" blocking event(s) with it on the stack"));}body.appendChild(box);}
  var bar=h("div",{class:"insp-bar"});
  var all=h("input",{type:"checkbox"});all.checked=S.trAll;all.addEventListener("change",function(){S.trAll=all.checked;draw();});
  var gf=h("input",{type:"search",placeholder:"Goroutine id or function",value:S.trFilter});gf.addEventListener("input",function(){S.trFilter=gf.value.trim().toLowerCase();draw();});
  var ts=h("select",{"aria-label":"Task"});ts.appendChild(h("option",{value:""},"All tasks"));(T.tasks||[]).forEach(function(t){ts.appendChild(h("option",{value:String(t.id)},t.type+" #"+t.id));});ts.value=S.trTask;ts.addEventListener("change",function(){S.trTask=ts.value;if(S.trTask){var t=(T.tasks||[]).filter(function(x){return String(x.id)===S.trTask;})[0];if(t)S.trView=[Math.max(t.s-1,0),(t.e>=0?t.e:T.durationNs)+1];}draw();});
  var from=h("input",{type:"number",step:"0.001",min:"0","aria-label":"From (ms)"}),to=h("input",{type:"number",step:"0.001",min:"0","aria-label":"To (ms)"});
  function setRange(){var a=parseFloat(from.value),z=parseFloat(to.value);if(!isNaN(a)&&!isNaN(z)&&z>a){S.trView=[a*1e6,z*1e6];draw();}}
  from.addEventListener("change",setRange);to.addEventListener("change",setRange);
  var rs=h("button",{type:"button",class:"ghost small"},"Whole trace");rs.addEventListener("click",function(){S.trView=null;draw();});
  add(bar,add(h("label"),all,txt("Runtime goroutines")),gf,ts,add(h("label"),txt("From (ms) "),from),add(h("label"),txt("to "),to),rs);
  body.appendChild(bar);
  var legend=h("div",{class:"legend"});[["running","--ok"],["runnable","--warn"],["waiting","--faint"],["syscall","--accent"],["GC","--bad"],["region / span","--db"]].forEach(function(l){var i=h("i");i.style.background="var("+l[1]+")";add(legend,add(h("span"),i,txt(l[0])));});body.appendChild(legend);
  var grid=h("div",{class:"insp-grid"}),left=h("div"),panel=h("aside",{class:"insp-panel","aria-label":"Event details"});add(body,add(grid,left,panel));
  var wrap=h("div",{class:"insp-canvas"}),cv=h("canvas",{role:"img","aria-label":"Execution timeline: wheel to zoom, drag to pan, click a block for details"});wrap.appendChild(cv);left.appendChild(wrap);
  var rows=[],LBL=190,RH=20,TOP=22;
  function visible(){var out=[];(T.goroutines||[]).forEach(function(g){if(!S.trAll&&g.system)return;if(S.trFilter&&("g"+g.id+" "+g.id+" "+g.name).toLowerCase().indexOf(S.trFilter)<0)return;if(S.trTask){var t=(T.tasks||[]).filter(function(x){return String(x.id)===S.trTask;})[0];if(t&&(t.goroutines||[]).indexOf(g.id)<0)return;}out.push(g);});return out;}
  var view=function(){return S.trView||[0,T.durationNs||1];};
  var regionsBy={};(T.regions||[]).forEach(function(r){(regionsBy[r.g]=regionsBy[r.g]||[]).push(r);});
  function draw(){
    rows=visible();var dpr=window.devicePixelRatio||1,W=wrap.clientWidth||900,H=Math.max(TOP+(rows.length+1)*RH+10,200);
    wrap.style.height=Math.min(H,640)+"px";wrap.style.overflowY=H>640?"auto":"hidden";
    cv.width=W*dpr;cv.height=H*dpr;cv.style.height=H+"px";var c=cv.getContext("2d");c.setTransform(dpr,0,0,dpr,0,0);
    var v=view(),a=v[0],z=v[1],sc=(W-LBL-8)/Math.max(z-a,1);from.value=(a/1e6).toFixed(3);to.value=(z/1e6).toFixed(3);
    var col={running:cssv("--ok"),runnable:cssv("--warn"),waiting:cssv("--faint"),syscall:cssv("--accent"),text:cssv("--text"),muted:cssv("--muted"),gc:cssv("--bad"),region:cssv("--db")||"#4fb6a6",line:cssv("--border"),sel:cssv("--accent")};
    c.clearRect(0,0,W,H);c.font="11px "+MONO;c.fillStyle=col.muted;
    for(var i=0;i<=5;i++){var t=a+(z-a)*i/5,x=LBL+(t-a)*sc;c.fillText(fmtNs(t),Math.min(x,W-60),12);c.fillStyle=col.line;c.fillRect(x,TOP-4,1,H);c.fillStyle=col.muted;}
    rows.forEach(function(g,ri){var y=TOP+ri*RH;c.fillStyle=g.symbol?col.text:col.muted;c.fillText(clip("G"+g.id+" "+g.name.slice(g.name.lastIndexOf("/")+1),26),4,y+13);
      (g.segments||[]).forEach(function(sg){if(sg.e<a||sg.s>z)return;var x1=LBL+(Math.max(sg.s,a)-a)*sc,x2=LBL+(Math.min(sg.e,z)-a)*sc;c.fillStyle=col[sg.st]||col.muted;c.fillRect(x1,y+3,Math.max(x2-x1,1),sg.st==="waiting"?6:12);});
      (regionsBy[g.id]||[]).forEach(function(r){var e=r.e>=0?r.e:z;if(e<a||r.s>z)return;var x1=LBL+(Math.max(r.s,a)-a)*sc,x2=LBL+(Math.min(e,z)-a)*sc;c.fillStyle=col.region;c.fillRect(x1,y+16,Math.max(x2-x1,1),3);});
      if(S.trSel&&S.trSel.g===g.id){c.strokeStyle=col.sel;c.lineWidth=2;var sx1=LBL+(Math.max(S.trSel.s,a)-a)*sc,sx2=LBL+(Math.min(S.trSel.e,z)-a)*sc;c.strokeRect(sx1-1,y+1,Math.max(sx2-sx1,2)+2,17);}
    });
    var gy=TOP+rows.length*RH;c.fillStyle=col.muted;c.fillText("GC / STW",4,gy+13);(T.gc||[]).forEach(function(sp){if(sp.e<a||sp.s>z)return;var x1=LBL+(Math.max(sp.s,a)-a)*sc,x2=LBL+(Math.min(sp.e,z)-a)*sc;c.fillStyle=col.gc;c.globalAlpha=sp.name.indexOf("stop-the-world")===0?1:0.55;c.fillRect(x1,gy+4,Math.max(x2-x1,1),10);c.globalAlpha=1;});
    if(!rows.length){c.fillStyle=col.muted;c.fillText("No goroutine matches the filters.",LBL,TOP+14);}
  }
  function hit(ev){var r=cv.getBoundingClientRect(),x=ev.clientX-r.left,y=ev.clientY-r.top,ri=Math.floor((y-TOP)/RH);var v=view(),sc=((wrap.clientWidth||900)-LBL-8)/Math.max(v[1]-v[0],1),t=v[0]+(x-LBL)/sc;
    if(ri===rows.length){var gc=(T.gc||[]).filter(function(sp){return t>=sp.s&&t<=sp.e;})[0];return gc?{gc:gc}:null;}
    var g=rows[ri];if(!g||x<LBL)return null;var inRegion=(y-TOP-ri*RH)>=15;
    if(inRegion){var rg=(regionsBy[g.id]||[]).filter(function(q){return t>=q.s&&t<=(q.e>=0?q.e:v[1]);}).pop();if(rg)return {g:g,region:rg};}
    var sg=(g.segments||[]).filter(function(q){return t>=q.s&&t<=q.e;})[0];return sg?{g:g,seg:sg}:{g:g};}
  function stackList(k){var pre=h("pre",{class:"insp-pre"});var st=(T.stacks||[])[k];if(!st)return h("div",{class:"insp-note"},"No stack recorded for this event.");st.forEach(function(f){pre.appendChild(txt(f.func+"\n      "+loc(f.file,f.line)+(f.symbol?"   [module]":"")+"\n"));});return pre;}
  function show(sel){panel.textContent="";if(!sel){panel.appendChild(h("p",{class:"insp-note"},"Click a block to see the event."));return;}
    if(sel.gc){add(panel,h("h3",null,sel.gc.name),h("dl"));detailRows(panel.lastChild,[["Start",fmtNs(sel.gc.s)],["Duration",fmtNs(sel.gc.e-sel.gc.s)]]);return;}
    var g=sel.g;add(panel,h("div",{class:"insp-note"},"goroutine G"+g.id+(g.system?" · runtime":"")),h("h3",null,g.name));
    var dl=h("dl");detailRows(dl,[["Created",g.created>=0?fmtNs(g.created)+(g.creator?" by G"+g.creator:""):"before the trace started"],["Ended",g.ended>=0?fmtNs(g.ended):"alive at the end"],["Running",fmtNs(g.runningNs)],["Runnable",fmtNs(g.runnableNs)],["Waiting",fmtNs(g.waitingNs)],["Syscall",fmtNs(g.syscallNs)]]);panel.appendChild(dl);
    if(sel.region){var r=sel.region;panel.appendChild(h("h4",null,"Region"));var d2=h("dl");detailRows(d2,[["Type",r.type],["Measures",r.instrumented?"function "+r.symbol+" (GitView instrumentation; includes its overhead)":""],["Task",r.task?String(r.task):""],["Start",fmtNs(r.s)],["Duration",r.e>=0?fmtNs(r.e-r.s):"did not end"]]);panel.appendChild(d2);panel.appendChild(stackList(r.k));}
    if(sel.seg){var sg=sel.seg;panel.appendChild(h("h4",null,"State"));var d3=h("dl");detailRows(d3,[["State",sg.st],["Reason",sg.r],["Start",fmtNs(sg.s)],["Duration",fmtNs(sg.e-sg.s)],["P",sg.st==="running"?String(sg.p):""]]);panel.appendChild(d3);if(sg.k>=0){panel.appendChild(h("h4",null,"Stack"));panel.appendChild(stackList(sg.k));}}
    if(g.createStack>=0){panel.appendChild(h("h4",null,"Created at"));panel.appendChild(stackList(g.createStack));}
  }
  cv.addEventListener("click",function(ev){var x=hit(ev);if(x&&(x.seg||x.region)){var q=x.seg||x.region;S.trSel={g:x.g.id,s:q.s,e:q.e>=0?q.e:view()[1]};}show(x);draw();});
  cv.addEventListener("wheel",function(ev){ev.preventDefault();var r=cv.getBoundingClientRect(),x=ev.clientX-r.left;if(x<LBL)return;var v=view(),sc=((wrap.clientWidth||900)-LBL-8)/Math.max(v[1]-v[0],1),t=v[0]+(x-LBL)/sc,f=Math.exp(ev.deltaY*0.0015),na=t-(t-v[0])*f,nz=t+(v[1]-t)*f;na=Math.max(0,na);nz=Math.min(T.durationNs,nz);if(nz-na>1000){S.trView=[na,nz];draw();}},{passive:false});
  var drag=null;cv.addEventListener("pointerdown",function(ev){drag={x:ev.clientX,v:view().slice()};});
  cv.addEventListener("pointermove",function(ev){if(!drag)return;var dx=ev.clientX-drag.x;if(Math.abs(dx)<3)return;var sc=((wrap.clientWidth||900)-LBL-8)/Math.max(drag.v[1]-drag.v[0],1),dt=-dx/sc,a=Math.max(0,drag.v[0]+dt),z=Math.min(T.durationNs,drag.v[1]+dt);if(z-a>0){S.trView=[a,z];draw();}});
  TR.release=function(){drag=null;};
  setTimeout(draw,0);show(null);
  var side=h("div");
  var blocks=(T.events||[]).filter(function(e){return e.kind==="block"&&e.dur>0;}).sort(function(x,y){return y.dur-x.dur;});var sys={};(T.goroutines||[]).forEach(function(g){sys[g.id]=g.system;});blocks=blocks.filter(function(e){return !sys[e.g];}).slice(0,15);
  if(blocks.length){side.appendChild(h("h4",null,"Longest observed waits"));var t=h("table",{class:"insp-table"}),tb=h("tbody");blocks.forEach(function(e){var st=(T.stacks||[])[e.k]||[],fr=st.filter(function(f){return f.symbol;})[0]||st[0]||{};var tr=add(h("tr"),h("td",{class:"num"},fmtNs(e.dur)),h("td",null,"G"+e.g+" "+e.detail),add(h("td"),txt((fr.func||"")+" "),locLink(fr.file,fr.line)));tr.addEventListener("click",function(){S.trView=[Math.max(e.t-e.dur*0.2,0),Math.min(e.t+e.dur*1.2,T.durationNs)];S.trSel={g:e.g,s:e.t,e:e.t+e.dur};draw();});tb.appendChild(tr);});add(t,tb);side.appendChild(t);}
  if((T.logs||[]).length){side.appendChild(h("h4",null,"Logs"));T.logs.slice(0,30).forEach(function(l){side.appendChild(h("div",{class:"insp-note mono"},fmtNs(l.t)+" G"+l.g+" ["+l.category+"] "+l.message));});}
  (T.notes||[]).forEach(function(n){side.appendChild(h("div",{class:"insp-note"},n));});
  left.appendChild(side);
  TR.redraw=draw;
}
// Listeners for the trace view are installed once; each render points them
// at its own drawing function.
var TR={release:null,redraw:null};
window.addEventListener("pointerup",function(){if(TR.release)TR.release();});
new MutationObserver(function(){if(TR.redraw&&S.tab==="trace")TR.redraw();}).observe(document.documentElement,{attributes:true,attributeFilter:["data-theme"]});

// ---------- start ----------
fromHash();render();
window.addEventListener("hashchange",function(){if(location.hash.indexOf("#inspect")===0){var was=S.sym+":"+S.tab;fromHash();if(was!==S.sym+":"+S.tab)render();}});
var obs=new MutationObserver(function(){if(document.documentElement.classList.contains("view-inspect"))setTimeout(render,0);});
obs.observe(document.documentElement,{attributes:true,attributeFilter:["class"]});
})();`
