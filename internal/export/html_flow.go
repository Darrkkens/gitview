package export

// Code Flow tab of the HTML report. The script reads the call index from the
// inert #flow-data JSON block and only navigates it: no analysis runs in the
// browser. Every string from the repository is inserted with textContent or
// setAttribute, never as markup.

const flowCSS = `
.flow-wrap{padding:22px 24px 8px}
.flow-head p{margin:-6px 0 12px}
.flow-app{display:grid;grid-template-columns:minmax(0,1fr) 360px;gap:12px;align-items:start}
.flow-bar{grid-column:1/-1;display:flex;flex-wrap:wrap;gap:8px;align-items:center}
.flow-bar label{font-size:12.5px;color:var(--muted);display:flex;align-items:center;gap:6px}
.flow-bar select{max-width:min(320px,100%)}
.flow-bar select,.flow-bar input[type=search]{font:13px var(--sans);color:var(--text);background:var(--bg);border:1px solid var(--border);border-radius:6px;padding:4px 8px}
.flow-bar input[type=search]:focus,.flow-bar select:focus{border-color:var(--accent);outline:none}
.fsearch{position:relative}
.fsearch input{width:340px}
.suggest{position:absolute;top:calc(100% + 4px);left:0;z-index:40;min-width:100%;width:520px;max-width:90vw;max-height:360px;overflow:auto;margin:0;padding:4px;list-style:none;background:var(--surface);border:1px solid var(--border-strong);border-radius:6px}
.suggest li{padding:5px 8px;border-radius:4px;cursor:pointer;display:grid;gap:1px}
.suggest li[aria-selected="true"],.suggest li:hover{background:var(--surface-2)}
.suggest .s-name{font:600 12.5px var(--mono);color:var(--text);overflow-wrap:anywhere}
.suggest .s-meta{font-size:11.5px;color:var(--muted);overflow-wrap:anywhere}
.suggest .s-info{color:var(--muted);font-size:12px;cursor:default;padding:6px 8px}
.seg{display:inline-flex;border:1px solid var(--border);border-radius:6px;overflow:hidden}
.seg button{font:12.5px var(--sans);background:transparent;border:0;color:var(--muted);padding:4px 10px;cursor:pointer}
.seg button+button{border-left:1px solid var(--border)}
.seg button[aria-pressed="true"]{background:var(--surface-2);color:var(--text)}
.flow-msg{grid-column:1/-1;font-size:12.5px;color:var(--muted);min-height:18px}
.flow-msg.warn{color:var(--warn)}
.flow-canvas{position:relative;border:1px solid var(--border);border-radius:6px;background:var(--bg);height:min(72vh,720px);overflow:hidden;touch-action:none}
.flow-canvas svg{width:100%;height:100%;display:block;cursor:grab;user-select:none;-webkit-user-select:none}
.flow-canvas svg.panning{cursor:grabbing}
.flow-tools{position:absolute;right:8px;top:8px;display:flex;gap:4px;z-index:5}
.flow-legend{position:absolute;left:8px;bottom:8px;display:flex;flex-wrap:wrap;gap:4px 12px;font-size:11.5px;color:var(--muted);background:var(--bg);padding:4px 8px;border:1px solid var(--border);border-radius:6px;max-width:calc(100% - 16px)}
.flow-legend i{display:inline-block;width:22px;height:0;border-top:2px solid var(--muted);vertical-align:middle;margin-right:4px}
.flow-legend i.l-likely{border-top-style:dashed}.flow-legend i.l-possible{border-top:2px dashed var(--faint)}.flow-legend i.l-unresolved{border-top:2px dotted var(--warn)}
.flow-empty[hidden]{display:none}
.flow-empty{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;color:var(--muted);padding:24px;text-align:center}
.fn-node rect.box{fill:var(--surface);stroke:var(--border-strong);stroke-width:1}
.fn-node rect.bar{fill:var(--accent)}
.fn-node.k-closure rect.bar{fill:var(--muted)}
.fn-node.k-route rect.bar{fill:var(--accent)}
.fn-node.k-route rect.box{fill:var(--hunk)}
.fn-node.k-db rect.bar{fill:var(--db)}
.fn-node.k-external rect.bar{fill:var(--faint)}
.fn-node.k-external text.t1{fill:var(--muted)}
.fn-node.k-unresolved rect.bar{fill:var(--warn)}
.fn-node.k-unresolved rect.box{stroke:var(--warn);stroke-dasharray:3 3}
.fn-node text{font-family:var(--mono);pointer-events:none}
.fn-node text.t1{font-size:12px;font-weight:600;fill:var(--text)}
.fn-node text.t2{font-size:10.5px;fill:var(--muted)}
.fn-node{cursor:pointer}
.fn-node:focus{outline:none}
.fn-node:focus rect.box{stroke:var(--accent);stroke-width:2}
.fn-node.root rect.box{stroke:var(--accent);stroke-width:2}
.fn-node.selected rect.box{stroke:var(--text);stroke-width:2}
.fn-node.dim{opacity:.28}
.fn-node circle.chg{fill:var(--accent)}
.fn-node .tog{cursor:pointer}
.fn-node .tog circle{fill:var(--bg);stroke:var(--border-strong)}
.fn-node .tog text{font:600 11px var(--mono);fill:var(--muted);text-anchor:middle;pointer-events:none}
.fn-node .tog:hover circle{stroke:var(--accent)}
.fn-edge{fill:none;stroke:var(--muted);stroke-width:1.3}
.fn-edge.c-likely{stroke-dasharray:6 3}
.fn-edge.c-possible{stroke:var(--faint);stroke-dasharray:3 3}
.fn-edge.c-unresolved{stroke:var(--warn);stroke-dasharray:1.5 3}
.fn-edge.onpath{stroke:var(--accent);stroke-width:2.2}
.fn-edge.dim{opacity:.2}
.fn-elabel{font:10px var(--mono);fill:var(--muted)}
.flow-panel{border:1px solid var(--border);border-radius:6px;padding:12px 14px;font-size:13px;max-height:min(72vh,720px);overflow:auto;min-width:0}
.flow-panel h3{font:600 13px var(--mono);margin:2px 0 6px;overflow-wrap:anywhere}
.flow-panel .kind{font-size:11.5px;color:var(--muted)}
.flow-panel code.sig{display:block;background:var(--surface);border:1px solid var(--border);border-radius:4px;padding:5px 7px;overflow-wrap:anywhere;white-space:pre-wrap;font-size:12px}
.flow-panel dl{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:3px 12px;margin:10px 0}
.flow-panel dt{color:var(--muted)}.flow-panel dd{margin:0;overflow-wrap:anywhere}
.flow-panel .explain{color:var(--text);margin:10px 0;line-height:1.55}
.flow-panel .acts{display:flex;flex-wrap:wrap;gap:6px;margin:10px 0}
.flow-panel h4{font-size:12.5px;font-weight:600;margin:14px 0 4px;color:var(--muted)}
.flow-panel ul.rel{list-style:none;margin:0;padding:0}
.flow-panel ul.rel li{margin:1px 0}
.flow-panel ul.rel button{background:none;border:0;padding:2px 0;color:var(--accent);font:12px var(--mono);cursor:pointer;text-align:left;overflow-wrap:anywhere}
.flow-panel ul.rel .conf{font-size:11px;color:var(--muted);margin-left:6px}
.flow-panel pre{margin:6px 0 0;background:var(--surface);border:1px solid var(--border);border-radius:4px;padding:8px;font-size:12px;line-height:18px;overflow:auto;max-height:340px;tab-size:4}
.flow-panel .tags span{display:inline-block;font-size:11px;border:1px solid var(--border);border-radius:9px;padding:0 7px;margin:0 4px 4px 0;color:var(--muted)}
.flow-list{grid-column:1/-1;border:1px solid var(--border);border-radius:6px;padding:10px 14px;font-size:13px}
.flow-list h4{margin:8px 0 4px;font-size:12.5px;color:var(--muted)}
.flow-list ul{list-style:none;margin:0;padding-left:16px}
.flow-list>ul{padding-left:0}
.flow-list button{background:none;border:0;color:var(--text);font:12.5px var(--mono);cursor:pointer;padding:1px 0;text-align:left}
.flow-list button:hover{color:var(--accent)}
.flow-list .meta{color:var(--muted);font-size:11.5px;margin-left:6px}
:root[data-theme="dark"]{--db:#4fb6a6}
:root[data-theme="light"]{--db:#0f766e}
@media screen and (max-width:980px){
  .flow-app{grid-template-columns:minmax(0,1fr)}
  .flow-panel{max-height:none}
  .fsearch input{width:100%}.fsearch{flex-basis:100%}
}
@media screen and (max-width:760px){.flow-wrap{padding:16px 10px}.flow-canvas{height:60vh}}
@media print{.flow-bar,.flow-tools{display:none!important}.flow-canvas{height:auto;min-height:400px;overflow:visible}}
`

const flowJS = `(function(){
"use strict";
var dataEl=document.getElementById("flow-data");
var host=document.getElementById("flow-app");
if(!dataEl||!host)return;
var D;
try{D=JSON.parse(dataEl.textContent);}catch(e){host.textContent="The Code Flow data could not be read.";return;}
var SVGNS="http://www.w3.org/2000/svg";
var nodes=Object.create(null),outE=Object.create(null),inE=Object.create(null);
(D.nodes||[]).forEach(function(n){nodes[n.id]=n;outE[n.id]=[];inE[n.id]=[];});
(D.edges||[]).forEach(function(e){if(nodes[e.from]&&nodes[e.to]){outE[e.from].push(e);inE[e.to].push(e);}});
Object.keys(outE).forEach(function(k){outE[k].sort(function(a,b){return (a.order||0)-(b.order||0);});});
var anchors=D.anchors||{};
var searchable=(D.nodes||[]).filter(function(n){return n.kind==="func"||n.kind==="closure"||n.kind==="route";});
var KIND={func:"Function",closure:"Anonymous function",route:"HTTP route",external:"External",db:"Database operation",unresolved:"Unresolved call"};
var CONF={confirmed:"confirmed",likely:"likely",possible:"possible",unresolved:"unresolved"};

var S={root:null,dir:"both",depth:3,external:true,collapsed:Object.create(null),selected:null,find:"",list:false,k:1,tx:0,ty:0};
var LIMIT=320;

function h(tag,attrs,text){var el=document.createElement(tag);if(attrs)for(var k in attrs){if(k==="class")el.className=attrs[k];else el.setAttribute(k,attrs[k]);}if(text!=null)el.textContent=text;return el;}
function s(tag,attrs){var el=document.createElementNS(SVGNS,tag);if(attrs)for(var k in attrs)el.setAttribute(k,attrs[k]);return el;}

// ---------- toolbar ----------
var bar=h("div",{class:"flow-bar"});
var sWrap=h("div",{class:"fsearch"});
var sInput=h("input",{type:"search",placeholder:"Find function…  (name, package, receiver or file)",autocomplete:"off",spellcheck:"false","aria-label":"Find function","role":"combobox","aria-expanded":"false","aria-controls":"flow-suggest","aria-autocomplete":"list"});
var sList=h("ul",{class:"suggest",id:"flow-suggest",role:"listbox",hidden:""});
sWrap.appendChild(sInput);sWrap.appendChild(sList);bar.appendChild(sWrap);
var rootSel=h("select",{"aria-label":"Start from"});
bar.appendChild(rootSel);
var seg=h("div",{class:"seg",role:"group","aria-label":"Direction"});
["callers","both","callees"].forEach(function(d){var b=h("button",{type:"button","data-dir":d,"aria-pressed":String(d===S.dir)},d==="both"?"Both":d==="callers"?"Callers":"Callees");seg.appendChild(b);});
bar.appendChild(seg);
var depthLbl=h("label",null,"Depth ");var depthSel=h("select",{"aria-label":"Depth"});
for(var i=1;i<=5;i++){var o=h("option",{value:String(i)},String(i));if(i===S.depth)o.selected=true;depthSel.appendChild(o);}
depthLbl.appendChild(depthSel);bar.appendChild(depthLbl);
var extLbl=h("label",null);var extChk=h("input",{type:"checkbox",checked:""});extLbl.appendChild(extChk);extLbl.appendChild(document.createTextNode("External"));bar.appendChild(extLbl);
var findIn=h("input",{type:"search",placeholder:"Search in graph",autocomplete:"off","aria-label":"Search in graph"});findIn.style.width="160px";bar.appendChild(findIn);
var listBtn=h("button",{type:"button",class:"ghost","aria-pressed":"false"},"List view");bar.appendChild(listBtn);
host.appendChild(bar);
var msg=h("div",{class:"flow-msg",role:"status","aria-live":"polite"});host.appendChild(msg);

var canvas=h("div",{class:"flow-canvas"});
var svg=s("svg",{role:"application","aria-label":"Call graph. Tab to a function, Enter to select, arrow keys to follow calls."});
var defs=s("defs");
[["a-def","var(--muted)"],["a-poss","var(--faint)"],["a-unres","var(--warn)"],["a-path","var(--accent)"]].forEach(function(m){
  var mk=s("marker",{id:m[0],viewBox:"0 0 10 10",refX:"9",refY:"5",markerWidth:"7",markerHeight:"7",orient:"auto-start-reverse"});
  var p=s("path",{d:"M0,1 L9,5 L0,9 z"});p.style.fill=m[1];mk.appendChild(p);defs.appendChild(mk);});
svg.appendChild(defs);
var vp=s("g");svg.appendChild(vp);
canvas.appendChild(svg);
var tools=h("div",{class:"flow-tools"});
[["fit","Fit"],["in","+"],["out","−"]].forEach(function(t){var b=h("button",{type:"button",class:"ghost small","data-zoom":t[0],"aria-label":t[0]==="fit"?"Fit graph":t[0]==="in"?"Zoom in":"Zoom out"},t[1]);tools.appendChild(b);});
canvas.appendChild(tools);
var legend=h("div",{class:"flow-legend","aria-hidden":"true"});
[["","confirmed"],["l-likely","likely (single implementation)"],["l-possible","possible (interface / callback)"],["l-unresolved","unresolved"]].forEach(function(l){var sp=h("span");sp.appendChild(h("i",{class:l[0]}));sp.appendChild(document.createTextNode(l[1]));legend.appendChild(sp);});
canvas.appendChild(legend);
var empty=h("div",{class:"flow-empty"});canvas.appendChild(empty);
host.appendChild(canvas);
var panel=h("aside",{class:"flow-panel","aria-label":"Function details"});host.appendChild(panel);
var listEl=h("div",{class:"flow-list",hidden:""});host.appendChild(listEl);

// Start-from list: modified functions and routes.
function fillRoots(){
  rootSel.textContent="";
  rootSel.appendChild(h("option",{value:""},"Start from…"));
  var changed=(D.roots||[]).filter(function(r){return nodes[r.id];});
  if(changed.length){var g=h("optgroup",{label:"Modified functions"});changed.forEach(function(r){g.appendChild(h("option",{value:r.id},r.label));});rootSel.appendChild(g);}
  var routes=(D.nodes||[]).filter(function(n){return n.kind==="route";}).sort(function(a,b){return a.path<b.path?-1:a.path>b.path?1:0;});
  if(routes.length){var g2=h("optgroup",{label:"HTTP routes"});routes.forEach(function(r){g2.appendChild(h("option",{value:r.id},r.name+"  ("+r.router+")"));});rootSel.appendChild(g2);}
}
fillRoots();

// ---------- symbol search ----------
var sugg=[],active=-1;
// Matching is case-insensitive, but only an exact, case-sensitive match of a
// Go identifier (or qualified name) counts as "the same name".
function score(n,q,raw){
  var name=n.name.toLowerCase(),bare=name.slice(name.lastIndexOf(".")+1);
  var hay=(n.name+" "+(n.pkg||"")+" "+(n.file||"")).toLowerCase();
  var toks=q.split(/\s+/).filter(Boolean);
  for(var i=0;i<toks.length;i++){if(hay.indexOf(toks[i])<0)return -1;}
  var rawBare=n.name.slice(n.name.lastIndexOf(".")+1);
  if(raw&&(rawBare===raw||n.name===raw))return 0;
  if(bare===q||name===q)return 1;
  if(bare.indexOf(q)===0)return 2;
  if(name.indexOf(q)>=0)return 3;
  return 4;
}
function suggest(){
  var raw=sInput.value.trim(),q=raw.toLowerCase();
  sList.textContent="";active=-1;sugg=[];
  if(!q){sList.hidden=true;sInput.setAttribute("aria-expanded","false");return;}
  var res=[];
  searchable.forEach(function(n){var sc=score(n,q,raw);if(sc>=0)res.push([sc,n]);});
  res.sort(function(a,b){return a[0]-b[0]||(a[1].name<b[1].name?-1:1);});
  var exact=res.filter(function(r){return r[0]===0;}).length;
  sugg=res.slice(0,14).map(function(r){return r[1];});
  if(!sugg.length){sList.appendChild(h("li",{class:"s-info"},"No function matching “"+sInput.value.trim()+"” in this report’s index."));}
  if(exact>1){sList.appendChild(h("li",{class:"s-info"},exact+" functions share this name — select the exact one."));}
  sugg.forEach(function(n,i){
    var li=h("li",{role:"option",id:"flow-opt-"+i,"aria-selected":"false"});
    li.appendChild(h("span",{class:"s-name"},n.name));
    li.appendChild(h("span",{class:"s-meta"},(n.pkg||n.router||"")+(n.file?"  ·  "+n.file+":"+n.line:"")));
    li.addEventListener("mousedown",function(e){e.preventDefault();pick(n);});
    sList.appendChild(li);
  });
  if(res.length>sugg.length)sList.appendChild(h("li",{class:"s-info"},(res.length-sugg.length)+" more — refine the search."));
  sList.hidden=false;sInput.setAttribute("aria-expanded","true");
}
function highlightOpt(i){
  var opts=sList.querySelectorAll("[role=option]");
  opts.forEach(function(o,j){o.setAttribute("aria-selected",String(j===i));});
  active=i;
  if(opts[i]){sInput.setAttribute("aria-activedescendant",opts[i].id);opts[i].scrollIntoView({block:"nearest"});}
}
function pick(n){sList.hidden=true;sInput.setAttribute("aria-expanded","false");sInput.value=n.name;setRoot(n.id);}
sInput.addEventListener("input",suggest);
sInput.addEventListener("focus",function(){if(sInput.value)suggest();});
sInput.addEventListener("blur",function(){setTimeout(function(){sList.hidden=true;sInput.setAttribute("aria-expanded","false");},120);});
sInput.addEventListener("keydown",function(e){
  if(e.key==="ArrowDown"){e.preventDefault();highlightOpt(Math.min(active+1,sugg.length-1));}
  else if(e.key==="ArrowUp"){e.preventDefault();highlightOpt(Math.max(active-1,0));}
  else if(e.key==="Enter"){
    e.preventDefault();
    if(active>=0&&sugg[active]){pick(sugg[active]);return;}
    var raw=sInput.value.trim(),q=raw.toLowerCase();
    var exact=searchable.filter(function(n){return score(n,q,raw)===0;});
    if(exact.length===1){pick(exact[0]);}
    else if(exact.length>1){say(exact.length+" functions are named “"+sInput.value.trim()+"”. Select one from the list.",true);suggest();}
    else if(sugg.length===1){pick(sugg[0]);}
    else{say(sugg.length?"Select a function from the list.":"No function matching “"+sInput.value.trim()+"” in this report’s index.",true);}
  }else if(e.key==="Escape"){sList.hidden=true;}
});

function say(t,warn){msg.textContent=t||"";msg.className="flow-msg"+(warn?" warn":"");}

// ---------- controls ----------
rootSel.addEventListener("change",function(){if(rootSel.value)setRoot(rootSel.value);});
seg.addEventListener("click",function(e){var b=e.target.closest("button[data-dir]");if(!b)return;S.dir=b.getAttribute("data-dir");seg.querySelectorAll("button").forEach(function(x){x.setAttribute("aria-pressed",String(x===b));});render(true);});
depthSel.addEventListener("change",function(){S.depth=+depthSel.value;render(true);});
extChk.addEventListener("change",function(){S.external=extChk.checked;render(true);});
findIn.addEventListener("input",function(){S.find=findIn.value.trim().toLowerCase();applyFind();});
findIn.addEventListener("keydown",function(e){if(e.key==="Enter"){e.preventDefault();var m=vp.querySelector(".fn-node:not(.dim)");if(m&&S.find){m.focus();select(m.getAttribute("data-id"),false);}}});
listBtn.addEventListener("click",function(){S.list=!S.list;listBtn.setAttribute("aria-pressed",String(S.list));listEl.hidden=!S.list;if(S.list)renderList();});
tools.addEventListener("click",function(e){var b=e.target.closest("[data-zoom]");if(!b)return;var z=b.getAttribute("data-zoom");if(z==="fit")fit();else zoomAt(z==="in"?1.25:0.8,canvas.clientWidth/2,canvas.clientHeight/2);});

function setRoot(id){
  if(!nodes[id])return;
  S.root=id;S.selected=id;S.collapsed=Object.create(null);
  if(rootSel.querySelector('option[value="'+cssEsc(id)+'"]'))rootSel.value=id;else rootSel.value="";
  try{history.replaceState(null,"","#codeflow:"+encodeURIComponent(id));}catch(e){}
  render(true);
}
function cssEsc(v){return window.CSS&&CSS.escape?CSS.escape(v):v.replace(/["\\\\]/g,"\\\\$&");}

// ---------- graph model for the current view ----------
var G=null;
function build(){
  var vis=Object.create(null),order=[],edges=[],eset=Object.create(null),parent=Object.create(null),more=Object.create(null),trunc=false;
  vis[S.root]=0;order.push(S.root);
  function walk(sign,adj,next){
    var frontier=[S.root];
    for(var d=1;d<=S.depth&&frontier.length;d++){
      var nf=[];
      frontier.forEach(function(id){
        if(S.collapsed[id+"|"+sign])return;
        (adj[id]||[]).forEach(function(e){
          var t=next(e),n=nodes[t];
          if(!S.external&&n.kind==="external")return;
          if(!(t in vis)){
            if(order.length>=LIMIT){trunc=true;return;}
            vis[t]=sign*d;order.push(t);nf.push(t);parent[t+"|"+sign]=e;
          }
          var key=e.from+">"+e.to+">"+(e.line||0)+">"+(e.order||0);
          if(!eset[key]){eset[key]=1;edges.push(e);}
        });
      });
      frontier=nf;
    }
    frontier.forEach(function(id){var c=(adj[id]||[]).filter(function(e){var n=nodes[next(e)];return S.external||n.kind!=="external";}).length;if(c)more[id+"|"+sign]=c;});
  }
  if(S.dir!=="callers")walk(1,outE,function(e){return e.to;});
  if(S.dir!=="callees")walk(-1,inE,function(e){return e.from;});
  return {vis:vis,order:order,edges:edges,parent:parent,more:more,trunc:trunc};
}

// ---------- layout (layered, left = callers, right = callees) ----------
var measureCtx=document.createElement("canvas").getContext("2d");
function textW(t,font){measureCtx.font=font;return measureCtx.measureText(t).width;}
var MONO=getComputedStyle(document.documentElement).getPropertyValue("--mono")||"monospace";
function fitText(t,max,font){if(textW(t,font)<=max)return t;var lo=0,hi=t.length;while(lo<hi){var m=(lo+hi+1)>>1;if(textW("…"+t.slice(t.length-m),font)<=max)lo=m;else hi=m-1;}return "…"+t.slice(t.length-lo);}
function sub(n){
  switch(n.kind){
    case "route":return n.router+(n.partial?" · partial":"")+" · "+n.file+":"+n.line;
    case "db":return (n.sql?"SQL literal":n.sqlDynamic?"dynamic SQL":"")+(n.file?" · "+n.file+":"+n.line:"");
    case "external":return n.pkg||"external";
    case "unresolved":return n.file?n.file+":"+n.line:"unresolved";
    default:return (n.file||"")+(n.line?":"+n.line:"");
  }
}
function layout(g){
  var cols=Object.create(null);
  g.order.forEach(function(id){var l=g.vis[id];(cols[l]=cols[l]||[]).push(id);});
  var keys=Object.keys(cols).map(Number).sort(function(a,b){return a-b;});
  var pos=Object.create(null);
  keys.forEach(function(l){cols[l].forEach(function(id,i){pos[id]=i;});});
  var nb=Object.create(null);
  g.edges.forEach(function(e){(nb[e.from]=nb[e.from]||[]).push(e.to);(nb[e.to]=nb[e.to]||[]).push(e.from);});
  for(var it=0;it<4;it++){
    keys.forEach(function(l){
      if(l===0)return;
      var toward=l>0?l-1:l+1;
      cols[l].sort(function(a,b){return bary(a)-bary(b);});
      function bary(id){var ns=(nb[id]||[]).filter(function(x){return g.vis[x]===toward;});if(!ns.length)return pos[id];return ns.reduce(function(s,x){return s+pos[x];},0)/ns.length;}
      cols[l].forEach(function(id,i){pos[id]=i;});
    });
  }
  var F1="600 12px "+MONO,NH=40,VG=14,HG=90,box=Object.create(null),x=0;
  keys.forEach(function(l){
    var w=0;cols[l].forEach(function(id){var n=nodes[id];w=Math.max(w,Math.min(360,Math.max(textW(n.name+(n.recursive?" ↻":""),F1),textW(sub(n),"10.5px "+MONO))+34));});
    var total=cols[l].length*(NH+VG)-VG,y=-total/2;
    cols[l].forEach(function(id){box[id]={x:x,y:y,w:w,h:NH,col:l};y+=NH+VG;});
    x+=w+HG;
  });
  var shift=box[S.root]?box[S.root].x+box[S.root].w/2:0;
  Object.keys(box).forEach(function(id){box[id].x-=shift;});
  return {box:box,cols:cols,keys:keys};
}

// ---------- rendering ----------
var L=null;
function render(refit){
  vp.textContent="";
  if(!S.root){empty.hidden=false;empty.textContent=searchable.length?"Search for a function or pick a starting point to see who calls it and what it calls.":"No Go functions were indexed for this report.";panel.textContent="";panel.appendChild(h("p",{class:"muted"},"Select a function to see its details."));return;}
  empty.hidden=true;
  G=build();L=layout(G);
  var info=G.order.length+" functions shown";
  if(G.trunc)info+=" — view limited to "+LIMIT+" nodes; narrow the depth or set another root";
  if(D.truncated)info+=" — the report's index itself was limited, some relations are missing";
  say(info,G.trunc||D.truncated);
  var eg=s("g"),ng=s("g");vp.appendChild(eg);vp.appendChild(ng);
  G.edges.forEach(function(e){
    var a=L.box[e.from],b=L.box[e.to];if(!a||!b)return;
    var x1=a.x+a.w,y1=a.y+a.h/2,x2=b.x,y2=b.y+b.h/2,d;
    if(x2>x1+10){var dx=Math.max(30,(x2-x1)/2);d="M"+x1+","+y1+" C"+(x1+dx)+","+y1+" "+(x2-dx)+","+y2+" "+x2+","+y2;}
    else{var lift=Math.max(a.h,Math.abs(y2-y1)/2)+30;d="M"+x1+","+y1+" C"+(x1+60)+","+(y1-lift)+" "+(x2-60)+","+(y2-lift)+" "+x2+","+y2;}
    var p=s("path",{d:d,class:"fn-edge c-"+e.conf,"data-from":e.from,"data-to":e.to,"marker-end":"url(#"+(e.conf==="unresolved"?"a-unres":e.conf==="possible"?"a-poss":"a-def")+")"});
    var tt=s("title");tt.textContent=nodes[e.from].name+" → "+nodes[e.to].name+" ("+CONF[e.conf]+(e.kind!=="call"?", "+e.kind:"")+")"+(e.file?" at "+e.file+":"+e.line:"")+(e.note?" — "+e.note:"");p.appendChild(tt);
    eg.appendChild(p);
    if(e.kind==="go"||e.kind==="defer"||e.order){var lb=s("text",{class:"fn-elabel",x:String((x1+x2)/2),y:String((y1+y2)/2-4),"text-anchor":"middle"});lb.textContent=e.kind==="handler"?String(e.order):e.kind;eg.appendChild(lb);}
  });
  G.order.forEach(function(id){
    var n=nodes[id],b=L.box[id];
    var cls="fn-node k-"+n.kind+(id===S.root?" root":"")+(id===S.selected?" selected":"");
    var g=s("g",{class:cls,transform:"translate("+b.x+","+b.y+")",tabindex:"0",role:"button","data-id":id,"aria-label":KIND[n.kind]+" "+n.name+(n.file?", "+n.file+" line "+n.line:"")});
    g.appendChild(s("rect",{class:"box",width:String(b.w),height:String(b.h),rx:"6"}));
    g.appendChild(s("rect",{class:"bar",width:"3",height:String(b.h-12),x:"6",y:"6",rx:"1.5"}));
    var t1=s("text",{class:"t1",x:"16",y:"17"});t1.textContent=fitText(n.name+(n.recursive?" ↻":""),b.w-30,"600 12px "+MONO);g.appendChild(t1);
    var t2=s("text",{class:"t2",x:"16",y:"32"});t2.textContent=fitText(sub(n),b.w-30,"10.5px "+MONO);g.appendChild(t2);
    if(n.changed){g.appendChild(s("circle",{class:"chg",cx:String(b.w-9),cy:"9",r:"3.5"}));}
    var tt=s("title");tt.textContent=n.name+(n.changed?" (modified)":"");g.appendChild(tt);
    // Expand / collapse toggles on the side the branch grows.
    [[1,b.w,outE],[-1,0,inE]].forEach(function(side){
      var sign=side[0],col=b.col;
      if(id!==S.root&&((sign>0&&col<0)||(sign<0&&col>0)))return;
      if(sign>0&&S.dir==="callers"||sign<0&&S.dir==="callees")return;
      var shown=G.edges.some(function(e){return sign>0?e.from===id&&G.vis[e.to]>col:e.to===id&&G.vis[e.from]<col;});
      var collapsed=!!S.collapsed[id+"|"+sign],moreN=G.more[id+"|"+sign];
      if(!shown&&!collapsed&&!moreN)return;
      var tg=s("g",{class:"tog",transform:"translate("+side[1]+","+(b.h/2)+")"});
      tg.appendChild(s("circle",{r:"7"}));
      var tx=s("text",{y:"3.5"});tx.textContent=collapsed?"+":(shown?"−":"…");tg.appendChild(tx);
      var t3=s("title");t3.textContent=collapsed?"Expand":(shown?"Collapse":(moreN+" more call(s) beyond the depth limit — set as root to explore"));tg.appendChild(t3);
      tg.addEventListener("click",function(ev){ev.stopPropagation();if(!shown&&!collapsed){setRoot(id);return;}var k=id+"|"+sign;if(S.collapsed[k])delete S.collapsed[k];else S.collapsed[k]=1;render(false);});
      g.appendChild(tg);
    });
    g.addEventListener("click",function(){select(id,true);});
    g.addEventListener("dblclick",function(){setRoot(id);});
    g.addEventListener("keydown",nodeKey);
    ng.appendChild(g);
  });
  applyFind();
  if(!(S.selected in G.vis))S.selected=S.root;
  select(S.selected,false);
  if(refit)fit();else applyT();
  if(S.list)renderList();
}

function pathTo(id){
  var ids=Object.create(null),es=[];ids[id]=1;
  var sign=G.vis[id]>0?1:-1,cur=id,guard=0;
  while(cur!==S.root&&guard++<50){var e=G.parent[cur+"|"+sign];if(!e)break;es.push(e);cur=sign>0?e.from:e.to;ids[cur]=1;}
  return {ids:ids,edges:es};
}
function select(id,focusPanel){
  if(!nodes[id])return;
  S.selected=id;
  var p=pathTo(id);
  vp.querySelectorAll(".fn-node").forEach(function(el){el.classList.toggle("selected",el.getAttribute("data-id")===id);});
  vp.querySelectorAll(".fn-edge").forEach(function(el){
    var on=p.edges.some(function(e){return e.from===el.getAttribute("data-from")&&e.to===el.getAttribute("data-to");});
    el.classList.toggle("onpath",on);
    el.setAttribute("marker-end","url(#"+(on?"a-path":el.classList.contains("c-unresolved")?"a-unres":el.classList.contains("c-possible")?"a-poss":"a-def")+")");
  });
  renderPanel(id);
}
function applyFind(){
  vp.querySelectorAll(".fn-node").forEach(function(el){var n=nodes[el.getAttribute("data-id")];el.classList.toggle("dim",!!S.find&&(n.name+" "+(n.file||"")).toLowerCase().indexOf(S.find)<0);});
  vp.querySelectorAll(".fn-edge").forEach(function(el){el.classList.toggle("dim",!!S.find);});
}

function nodeKey(e){
  var id=this.getAttribute("data-id"),n=null;
  if(e.key==="Enter"&&e.shiftKey||e.key==="r"){e.preventDefault();setRoot(id);return;}
  if(e.key==="Enter"||e.key===" "){e.preventDefault();select(id,true);return;}
  if(e.key==="ArrowRight"){var o=G.edges.filter(function(x){return x.from===id&&x.to in G.vis;});if(o.length)n=o[0].to;}
  else if(e.key==="ArrowLeft"){var i=G.edges.filter(function(x){return x.to===id&&x.from in G.vis;});if(i.length)n=i[0].from;}
  else if(e.key==="ArrowDown"||e.key==="ArrowUp"){var col=L.cols[G.vis[id]].slice().sort(function(a,b){return L.box[a].y-L.box[b].y;});var k=col.indexOf(id)+(e.key==="ArrowDown"?1:-1);if(k>=0&&k<col.length)n=col[k];}
  else if(e.key==="+"||e.key==="="){zoomAt(1.25,canvas.clientWidth/2,canvas.clientHeight/2);return;}
  else if(e.key==="-"){zoomAt(0.8,canvas.clientWidth/2,canvas.clientHeight/2);return;}
  else if(e.key==="0"){fit();return;}
  if(n){e.preventDefault();var el=vp.querySelector('.fn-node[data-id="'+cssEsc(n)+'"]');if(el){el.focus();select(n,false);center(n);}}
}

// ---------- detail panel ----------
function relList(title,edges,other){
  var frag=document.createDocumentFragment();
  frag.appendChild(h("h4",null,title+" ("+edges.length+")"));
  if(!edges.length){frag.appendChild(h("p",{class:"muted small"},"None found."));return frag;}
  var ul=h("ul",{class:"rel"});
  edges.slice(0,60).forEach(function(e){
    var t=nodes[other(e)],li=h("li");
    var b=h("button",{type:"button"},t.name);
    b.addEventListener("click",function(){if(t.id in (G?G.vis:{})){select(t.id,false);center(t.id);var el=vp.querySelector('.fn-node[data-id="'+cssEsc(t.id)+'"]');if(el)el.focus();}else setRoot(t.id);});
    li.appendChild(b);
    li.appendChild(h("span",{class:"conf"},CONF[e.conf]+(e.kind!=="call"?" · "+e.kind:"")+(e.line?" · line "+e.line:"")));
    ul.appendChild(li);
  });
  if(edges.length>60)ul.appendChild(h("li",{class:"muted small"},(edges.length-60)+" more"));
  frag.appendChild(ul);return frag;
}
function diffLink(n){
  var fid=anchors[n.file];if(!fid||!n.line)return null;
  var end=n.end||n.line;
  for(var l=n.line;l<=end;l++){if(document.getElementById(fid+"-L"+l))return "#"+fid+"-L"+l;}
  return n.changed?"#"+fid:null;
}
function renderPanel(id){
  var n=nodes[id];panel.textContent="";
  panel.appendChild(h("div",{class:"kind"},KIND[n.kind]+(id===S.root?" · root":"")+(n.changed?" · modified":"")));
  panel.appendChild(h("h3",null,n.name));
  if(n.sig)panel.appendChild(h("code",{class:"sig"},n.sig));
  var dl=h("dl");
  function row(k,v){if(!v)return;dl.appendChild(h("dt",null,k));dl.appendChild(h("dd",null,v));}
  row("Package",n.pkg);row("Location",n.file?n.file+":"+n.line+(n.end&&n.end!==n.line?"–"+n.end:""):"");
  if(n.kind==="route"){row("Method",n.method);row("Path",n.path);row("Router",n.router);}
  if(n.kind==="db"){row("Library",n.dbLib);row("Operation",n.dbOp);row("SQL",n.sql?"literal (below)":n.sqlDynamic?"built at runtime (dynamic)":"");}
  panel.appendChild(dl);
  if(n.tags&&n.tags.length){var tg=h("div",{class:"tags"});n.tags.forEach(function(t){tg.appendChild(h("span",null,t));});panel.appendChild(tg);}
  var acts=h("div",{class:"acts"});
  if(id!==S.root&&(n.kind==="func"||n.kind==="closure"||n.kind==="route")){var sr=h("button",{type:"button",class:"ghost small"},"Set as root");sr.addEventListener("click",function(){setRoot(id);});acts.appendChild(sr);}
  var dlk=diffLink(n);
  if(dlk){var a=h("a",{class:"ghost small",href:dlk},"Open in diff");acts.appendChild(a);}
  else if(n.file&&(n.kind==="func"||n.kind==="closure")){acts.appendChild(h("span",{class:"muted small"},"Not modified in this report"));}
  if(acts.childNodes.length)panel.appendChild(acts);
  if(n.explain)panel.appendChild(h("p",{class:"explain"},n.explain));
  if(n.sql){panel.appendChild(h("h4",null,"SQL"));panel.appendChild(h("pre",null,n.sql));}
  if(n.note&&n.kind!=="route")panel.appendChild(h("p",{class:"muted small"},n.note));
  panel.appendChild(relList("Calls",outE[id]||[],function(e){return e.to;}));
  panel.appendChild(relList("Called by",inE[id]||[],function(e){return e.from;}));
  if(n.code){panel.appendChild(h("h4",null,"Code"));panel.appendChild(h("pre",null,n.code));}
}

// ---------- accessible list view ----------
function renderList(){
  listEl.textContent="";
  if(!G)return;
  function tree(id,sign,depth,seen){
    var ul=h("ul");
    var adj=sign>0?outE[id]:inE[id];
    (adj||[]).forEach(function(e){
      var t=sign>0?e.to:e.from;if(!(t in G.vis)||G.vis[t]!==sign*depth)return;
      var li=h("li"),b=h("button",{type:"button"},(sign>0?"→ ":"← ")+nodes[t].name);
      b.addEventListener("click",function(){select(t,true);});
      li.appendChild(b);li.appendChild(h("span",{class:"meta"},CONF[e.conf]+(nodes[t].file?" · "+nodes[t].file+":"+nodes[t].line:"")));
      if(!seen[t]){seen[t]=1;var sub=tree(t,sign,depth+1,seen);if(sub.childNodes.length)li.appendChild(sub);}
      ul.appendChild(li);
    });
    return ul;
  }
  listEl.appendChild(h("h4",null,nodes[S.root].name));
  if(S.dir!=="callees"){listEl.appendChild(h("h4",null,"Callers"));listEl.appendChild(tree(S.root,-1,1,Object.create(null)));}
  if(S.dir!=="callers"){listEl.appendChild(h("h4",null,"Callees"));listEl.appendChild(tree(S.root,1,1,Object.create(null)));}
}

// ---------- zoom & pan ----------
function applyT(){vp.setAttribute("transform","translate("+S.tx+","+S.ty+") scale("+S.k+")");}
// fit shows the whole graph when it stays readable; otherwise it keeps a
// readable scale and centres the root, leaving the rest to pan and zoom.
function fit(){
  if(!L)return;
  var minX=1e9,minY=1e9,maxX=-1e9,maxY=-1e9;
  Object.keys(L.box).forEach(function(id){var b=L.box[id];minX=Math.min(minX,b.x);minY=Math.min(minY,b.y);maxX=Math.max(maxX,b.x+b.w);maxY=Math.max(maxY,b.y+b.h);});
  var W=canvas.clientWidth||800,H=canvas.clientHeight||500,pad=40,MINK=0.7;
  var k=Math.min(1.2,Math.min((W-2*pad)/(maxX-minX||1),(H-2*pad)/(maxY-minY||1)));
  if(k>=MINK){S.k=k;S.tx=W/2-(minX+maxX)/2*S.k;S.ty=H/2-(minY+maxY)/2*S.k;applyT();return;}
  S.k=MINK;var r=L.box[S.root]||{x:0,y:0,w:0,h:0};
  S.tx=W/2-(r.x+r.w/2)*S.k;S.ty=H/2-(r.y+r.h/2)*S.k;applyT();
  say(msg.textContent.replace(/ — larger than the view.*$/,"")+" — larger than the view: drag to pan, scroll to zoom",msg.classList.contains("warn"));
}
function center(id){var b=L.box[id];if(!b)return;var W=canvas.clientWidth,H=canvas.clientHeight;var cx=S.tx+(b.x+b.w/2)*S.k,cy=S.ty+(b.y+b.h/2)*S.k;if(cx<40||cx>W-40||cy<40||cy>H-40){S.tx=W/2-(b.x+b.w/2)*S.k;S.ty=H/2-(b.y+b.h/2)*S.k;applyT();}}
function zoomAt(f,x,y){var k=Math.max(0.15,Math.min(3,S.k*f));S.tx=x-(x-S.tx)*(k/S.k);S.ty=y-(y-S.ty)*(k/S.k);S.k=k;applyT();}
svg.addEventListener("wheel",function(e){e.preventDefault();var r=svg.getBoundingClientRect();zoomAt(Math.exp(-e.deltaY*0.0015),e.clientX-r.left,e.clientY-r.top);},{passive:false});
var drag=null;
svg.addEventListener("pointerdown",function(e){if(e.target.closest(".fn-node"))return;drag={x:e.clientX,y:e.clientY,tx:S.tx,ty:S.ty};svg.setPointerCapture(e.pointerId);svg.classList.add("panning");});
svg.addEventListener("pointermove",function(e){if(!drag)return;S.tx=drag.tx+e.clientX-drag.x;S.ty=drag.ty+e.clientY-drag.y;applyT();});
svg.addEventListener("pointerup",function(){drag=null;svg.classList.remove("panning");});

// ---------- start ----------
function fromHash(){var hsh=location.hash;if(hsh.indexOf("#codeflow:")===0){var id=decodeURIComponent(hsh.slice(10));if(nodes[id]&&id!==S.root){S.root=id;S.selected=id;S.collapsed=Object.create(null);render(true);}return true;}return false;}
window.addEventListener("hashchange",function(){if(location.hash.indexOf("#codeflow")===0){fromHash();if(S.root&&L)setTimeout(fit,0);}});
if(!fromHash()){var first=(D.roots||[]).filter(function(r){return nodes[r.id];})[0];if(first){S.root=first.id;S.selected=first.id;rootSel.value=first.id;}render(true);}
if(D.errors&&D.errors.length)say("The Go code has "+D.errors.length+" compile error(s); results for the affected packages may be partial.",true);
var obs=new MutationObserver(function(){if(document.documentElement.classList.contains("view-flow")&&S.root)setTimeout(fit,0);});
obs.observe(document.documentElement,{attributes:true,attributeFilter:["class"]});
})();`
