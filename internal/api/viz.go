package api

// vizHTML is the single-page visualization UI served at /ui.
// Uses vis-network (CDN) — no build step required.
const vizHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Assiter — Code Knowledge Graph</title>
<script src="https://unpkg.com/vis-network/standalone/umd/vis-network.min.js"></script>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
       background: #0d1117; color: #e6edf3; height: 100vh; display: flex; flex-direction: column; }

/* ── top bar ── */
#topbar { display: flex; align-items: center; gap: 10px; padding: 10px 16px;
          background: #161b22; border-bottom: 1px solid #30363d; flex-shrink: 0; }
#topbar h1 { font-size: 16px; font-weight: 600; color: #58a6ff; white-space: nowrap; }
#search { flex: 1; padding: 6px 10px; border-radius: 6px; border: 1px solid #30363d;
          background: #0d1117; color: #e6edf3; font-size: 14px; outline: none; }
#search:focus { border-color: #58a6ff; }
button { padding: 6px 14px; border-radius: 6px; border: none; cursor: pointer;
         font-size: 13px; font-weight: 500; }
#btnSearch { background: #238636; color: #fff; }
#btnSearch:hover { background: #2ea043; }
#btnClear  { background: #30363d; color: #e6edf3; }
#btnClear:hover { background: #484f58; }

/* ── main area ── */
#main { display: flex; flex: 1; overflow: hidden; }

/* ── graph canvas ── */
#graph { flex: 1; }

/* ── side panel ── */
#panel { width: 320px; border-left: 1px solid #30363d; background: #161b22;
         overflow-y: auto; padding: 14px; flex-shrink: 0; }
#panel h2 { font-size: 13px; color: #8b949e; margin-bottom: 10px; text-transform: uppercase;
            letter-spacing: .05em; }
#nodeDetail { font-size: 13px; line-height: 1.6; }
#nodeDetail .key   { color: #8b949e; }
#nodeDetail .value { color: #e6edf3; word-break: break-all; }
#nodeDetail .badge { display: inline-block; padding: 1px 7px; border-radius: 9px;
                     font-size: 11px; font-weight: 600; margin-bottom: 6px; }
.row { display: flex; gap: 6px; margin-bottom: 4px; }

/* ── stats bar ── */
#statsBar { display: flex; gap: 14px; padding: 6px 16px;
            background: #161b22; border-top: 1px solid #30363d;
            font-size: 12px; color: #8b949e; flex-shrink: 0; }
#statsBar span { color: #e6edf3; font-weight: 600; }

/* ── loading overlay ── */
#loading { display: none; position: absolute; inset: 0; background: rgba(13,17,23,.7);
           align-items: center; justify-content: center; font-size: 15px; color: #58a6ff; }
#loading.show { display: flex; }
</style>
</head>
<body>

<div id="topbar">
  <h1>🔭 Assiter</h1>
  <input id="search" type="text" placeholder="Search symbol name…" autofocus>
  <button id="btnSearch">Search</button>
  <button id="btnClear">Clear</button>
</div>

<div id="main">
  <div id="graph"></div>

  <div id="panel">
    <h2>Node detail</h2>
    <div id="nodeDetail"><p style="color:#8b949e">Click a node to inspect it.</p></div>
  </div>
</div>

<div id="statsBar">
  <div>Nodes: <span id="statNodes">0</span></div>
  <div>Edges: <span id="statEdges">0</span></div>
  <div id="statMsg"></div>
</div>

<div id="loading">Loading graph…</div>

<script>
// ── colour map by node type ──────────────────────────────────────────────
const COLORS = {
  File:      { bg: '#1f6feb', border: '#388bfd', font: '#fff' },
  Package:   { bg: '#388bfd', border: '#58a6ff', font: '#fff' },
  Function:  { bg: '#238636', border: '#2ea043', font: '#fff' },
  Method:    { bg: '#1a7f37', border: '#2ea043', font: '#fff' },
  Struct:    { bg: '#9e6a03', border: '#d29922', font: '#fff' },
  Interface: { bg: '#8957e5', border: '#a371f7', font: '#fff' },
  Variable:  { bg: '#6e7681', border: '#8b949e', font: '#fff' },
  Import:    { bg: '#31383f', border: '#484f58', font: '#e6edf3' },
  Symbol:    { bg: '#b62324', border: '#f85149', font: '#fff' },
};
function colorFor(group) {
  const c = COLORS[group] || { bg: '#30363d', border: '#484f58', font: '#e6edf3' };
  return { background: c.bg, border: c.border, highlight: { background: c.border, border: c.bg },
           font: { color: c.font } };
}

// ── vis-network setup ────────────────────────────────────────────────────
const container = document.getElementById('graph');
const nodesDS   = new vis.DataSet();
const edgesDS   = new vis.DataSet();

const network = new vis.Network(container, { nodes: nodesDS, edges: edgesDS }, {
  nodes: { shape: 'box', borderWidth: 1.5, font: { size: 13 },
           widthConstraint: { maximum: 180 }, margin: { top: 6, bottom: 6, left: 8, right: 8 } },
  edges: { arrows: { to: { enabled: true, scaleFactor: .6 } },
           font: { size: 10, color: '#8b949e', align: 'middle' },
           color: { color: '#30363d', highlight: '#58a6ff' }, smooth: { type: 'cubicBezier' } },
  layout: { improvedLayout: true },
  physics: { stabilization: { iterations: 150 },
             barnesHut: { gravitationalConstant: -8000, springLength: 160, damping: 0.5 } },
  interaction: { tooltipDelay: 200, navigationButtons: true, keyboard: true },
});

// ── node click → expand neighbours ──────────────────────────────────────
network.on('click', async params => {
  if (!params.nodes.length) return;
  const id = params.nodes[0];
  showDetail(nodesDS.get(id));
  await expandNode(id);
});

async function expandNode(id) {
  setLoading(true);
  try {
    const r  = await fetch('/graph/subgraph/node/' + encodeURIComponent(id));
    const vg = await r.json();
    mergeGraph(vg);
  } catch(e) { console.error(e); }
  setLoading(false);
}

// ── search ───────────────────────────────────────────────────────────────
document.getElementById('btnSearch').addEventListener('click', doSearch);
document.getElementById('search').addEventListener('keydown', e => { if (e.key === 'Enter') doSearch(); });

async function doSearch() {
  const name = document.getElementById('search').value.trim();
  if (!name) return;
  setLoading(true);
  try {
    const r  = await fetch('/graph/subgraph?name=' + encodeURIComponent(name));
    const vg = await r.json();
    mergeGraph(vg);
    document.getElementById('statMsg').textContent =
      'Search: "' + name + '" — ' + (vg.nodes||[]).length + ' matching nodes';
  } catch(e) { console.error(e); }
  setLoading(false);
}

// ── clear ────────────────────────────────────────────────────────────────
document.getElementById('btnClear').addEventListener('click', () => {
  nodesDS.clear(); edgesDS.clear();
  document.getElementById('statNodes').textContent = '0';
  document.getElementById('statEdges').textContent = '0';
  document.getElementById('statMsg').textContent = '';
  document.getElementById('nodeDetail').innerHTML = '<p style="color:#8b949e">Click a node to inspect it.</p>';
});

// ── helpers ──────────────────────────────────────────────────────────────
function mergeGraph(vg) {
  (vg.nodes || []).forEach(n => {
    const c = colorFor(n.group);
    const item = { id: n.id, label: n.label, title: n.title,
                   color: c, font: c.font, _raw: n };
    if (!nodesDS.get(n.id)) nodesDS.add(item);
  });
  (vg.edges || []).forEach(e => {
    const eid = e.from + '||' + e.to + '||' + e.label;
    if (!edgesDS.get(eid)) edgesDS.add({ id: eid, from: e.from, to: e.to, label: e.label });
  });
  document.getElementById('statNodes').textContent = nodesDS.length;
  document.getElementById('statEdges').textContent = edgesDS.length;
  network.fit({ animation: { duration: 500, easingFunction: 'easeInOutQuad' } });
}

function showDetail(node) {
  if (!node) return;
  const r = node._raw || {};
  const badgeColor = (COLORS[r.group] || {}).bg || '#30363d';
  document.getElementById('nodeDetail').innerHTML = ` + "`" + `
    <div class="badge" style="background:${badgeColor}">${r.group || '?'}</div>
    <div class="row"><span class="key">Name&nbsp;&nbsp;&nbsp;</span><span class="value">${node.label}</span></div>
    <div class="row"><span class="key">File&nbsp;&nbsp;&nbsp;&nbsp;</span><span class="value">${r.filePath || '—'}</span></div>
    <div class="row"><span class="key">Line&nbsp;&nbsp;&nbsp;&nbsp;</span><span class="value">${r.line || '—'}</span></div>
    <div class="row"><span class="key">ID&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;</span><span class="value" style="font-size:11px;color:#8b949e">${node.id}</span></div>
  ` + "`" + `;
}

function setLoading(on) {
  document.getElementById('loading').classList.toggle('show', on);
}

// ── load stats on startup ────────────────────────────────────────────────
(async () => {
  try {
    const r = await fetch('/graph/stats');
    const stats = await r.json();
    const total = Object.values(stats).reduce((a,b) => a+b, 0);
    document.getElementById('statMsg').textContent =
      'Graph: ' + Object.entries(stats).map(([k,v]) => k+': '+v).join(' · ') +
      ' — total '+total+' nodes';
  } catch(e) {}
})();
</script>
</body>
</html>`
