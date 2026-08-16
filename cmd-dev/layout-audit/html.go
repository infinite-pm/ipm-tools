package main

// The report: one self-contained HTML file, no external assets, readable
// from file://.
//
// Shape of a row: the OLD diagram on the left, the NEW one on the right
// flapping between its plain rendering and the same rendering with the
// differences drawn over it. The flap is what makes a structural change
// findable in a second — two static pictures side by side leave the reader
// hunting, and a blink comparator does not.
//
// Both panes are scaled to ONE pixel scale (the larger of the two canvases),
// so a diagram that grew looks bigger instead of being silently re-fitted to
// the same box — that re-fit is exactly how a real change hides.

import (
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/infinite-pm/ipm-tools/pkg/layoutaudit"
)

type reportInput struct {
	Old, New layoutaudit.Engine
	Paths    []string
	Results  []result
	Elapsed  time.Duration
	Warnings []string
	Limit    int
}

type vmChange struct {
	Kind, Ref, Label, Detail, Tier string
}

type vmRow struct {
	Rank                         int
	ID, Origin, Status, Tier     string
	Line                         int
	Score                        string
	Summary                      string
	Aliases                      []string
	Bounds                       string
	OldSVG, NewSVG               template.HTML
	OldWidth, NewWidth           string // percent of the shared scale
	Changes                      []vmChange
	FindingsAdded, FindingsFixed []string
	NewCmd, OldCmd               string
	OldLayout, NewLayout         string
	Err                          string
}

type vmModel struct {
	OldDesc, NewDesc string
	Paths            string
	Elapsed          string
	Generated        string
	Warnings         []string
	Rows             []vmRow
	Identical        []string
	Skipped          []vmRow
	Counts           counts
	Total            int
	LimitNote        string
}

func renderHTML(in reportInput) string {
	m := vmModel{
		OldDesc: in.Old.Describe(), NewDesc: in.New.Describe(),
		Paths:   strings.Join(in.Paths, " "),
		Elapsed: in.Elapsed.Round(time.Millisecond).String(),
		Counts:  tally(in.Results), Total: len(in.Results),
		Warnings: in.Warnings,
	}
	rank := 0
	for _, r := range in.Results {
		switch r.Status {
		case statusIdentical:
			m.Identical = append(m.Identical, r.ID)
			continue
		case statusSkipped:
			m.Skipped = append(m.Skipped, vmRow{ID: r.ID, Status: r.Status, Err: r.NewErr})
			continue
		}
		rank++
		m.Rows = append(m.Rows, buildRow(rank, r, in.Old))
	}
	if in.Limit > 0 && m.Counts.Changed > in.Limit {
		m.LimitNote = fmt.Sprintf("--limit %d: %d further changed diagrams are counted and listed but not rendered.",
			in.Limit, m.Counts.Changed-in.Limit)
	}
	var b strings.Builder
	if err := reportTmpl.Execute(&b, m); err != nil {
		return "<!doctype html><meta charset=utf-8><pre>report template: " +
			template.HTMLEscapeString(err.Error()) + "</pre>"
	}
	return b.String()
}

func buildRow(rank int, r result, oldEng layoutaudit.Engine) vmRow {
	row := vmRow{
		Rank: rank, ID: r.ID, Origin: r.Origin, Line: r.Line, Aliases: r.Aliases,
		Status: r.Status, Tier: r.Report.Tier.String(),
		Score:         fmt.Sprintf("%.0f", r.Report.Score),
		OldSVG:        template.HTML(layoutaudit.InlineSVG(r.OldSVG)), //nolint:gosec // our own renderer's output
		NewSVG:        template.HTML(layoutaudit.InlineSVG(r.NewSVG)), //nolint:gosec
		FindingsAdded: r.Report.FindingsAdded,
		FindingsFixed: r.Report.FindingsFixed,
		OldLayout:     relLink(r.OldLayout),
		NewLayout:     relLink(r.NewLayout),
	}
	switch r.Status {
	case statusBroken:
		row.Tier = "broken"
		row.Err = "the new engine cannot lay this out: " + r.NewErr
	case statusRepaired:
		row.Tier = "repaired"
		row.Err = "the old engine could not lay this out: " + r.OldErr
	}

	ob, nb := r.Report.OldBounds, r.Report.NewBounds
	row.Bounds = fmt.Sprintf("%d×%d → %d×%d", ob.Width, ob.Height, nb.Width, nb.Height)
	if ob == nb {
		row.Bounds = fmt.Sprintf("%d×%d", nb.Width, nb.Height)
	}
	row.OldWidth, row.NewWidth = layoutaudit.PaneWidths(ob.Width, nb.Width)
	row.Summary = layoutaudit.Summarize(r.Report)

	for _, c := range r.Report.Changes {
		row.Changes = append(row.Changes, vmChange{
			Kind: c.Kind, Ref: c.Ref, Label: c.Label, Detail: c.Detail, Tier: c.Tier.String(),
		})
	}

	sel := layoutaudit.Selectors(r.Report)
	row.NewCmd = fmt.Sprintf("go run ./cmd-dev/layout-debug --in %s --why%s", r.Diagram.Path, sel)
	if oldEng.LayoutDbg != "" {
		row.OldCmd = fmt.Sprintf("%s --in %s --why%s", oldEng.LayoutDbg, r.Diagram.Path, sel)
	}
	return row
}

func relLink(abs string) string {
	if abs == "" {
		return ""
	}
	if i := strings.LastIndex(abs, "/pairs/"); i >= 0 {
		return "pairs/" + abs[i+len("/pairs/"):]
	}
	return abs
}

var reportTmpl = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>layout audit</title>
<style>
:root{
  --bg:#f6f7f9; --card:#fff; --ink:#1a1c1f; --muted:#5c636b; --line:#dfe3e8;
  --worse:#e03131; --better:#2f9e44; --changed:#7048e8; --moved:#f08c00;
  --pane:#fbfbfc;
}
@media (prefers-color-scheme: dark){
  :root{ --bg:#16181c; --card:#1e2126; --ink:#e8eaed; --muted:#9aa3ad; --line:#2c3138; --pane:#f4f5f7; }
}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);
  font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Inter,Helvetica,Arial,sans-serif}
header{padding:22px 26px 14px;border-bottom:1px solid var(--line);background:var(--card);
  position:sticky;top:0;z-index:20}
h1{margin:0 0 10px;font-size:19px;letter-spacing:.2px}
.prov{display:grid;grid-template-columns:auto 1fr;gap:2px 12px;font-size:13px;color:var(--muted);margin-bottom:10px}
.prov b{color:var(--ink);font-weight:600}
.tallies{display:flex;flex-wrap:wrap;gap:8px;align-items:center;font-size:13px}
.pill{padding:2px 9px;border-radius:999px;border:1px solid var(--line);background:var(--bg)}
.pill.invariant{border-color:var(--worse);color:var(--worse);font-weight:600}
.pill.structural{border-color:var(--changed);color:var(--changed);font-weight:600}
.pill.geometry{border-color:var(--moved);color:var(--moved)}
.pill.broken{background:var(--worse);color:#fff;border-color:var(--worse);font-weight:700}
.controls{display:flex;flex-wrap:wrap;gap:8px;margin-top:12px;align-items:center;font-size:13px}
button,select,input{font:inherit;font-size:13px;padding:4px 10px;border-radius:6px;
  border:1px solid var(--line);background:var(--bg);color:var(--ink);cursor:pointer}
input{cursor:text;min-width:220px}
button:hover{border-color:var(--muted)}
.legend{display:flex;gap:14px;font-size:12px;color:var(--muted);margin-top:10px;flex-wrap:wrap}
.legend i{display:inline-block;width:11px;height:11px;border-radius:3px;margin-right:4px;vertical-align:-1px}
main{padding:20px 26px 60px;max-width:1600px;margin:0 auto}
.row{background:var(--card);border:1px solid var(--line);border-radius:10px;margin-bottom:20px;overflow:hidden}
.row.hide{display:none}
.rowhead{padding:12px 16px;border-bottom:1px solid var(--line);display:flex;gap:12px;align-items:baseline;flex-wrap:wrap}
.rank{font-variant-numeric:tabular-nums;color:var(--muted);font-weight:700}
.id{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:13px;word-break:break-all}
.score{margin-left:auto;color:var(--muted);font-size:12px}
.summary{padding:8px 16px;font-size:13px;color:var(--muted);border-bottom:1px solid var(--line)}
.panes{display:grid;grid-template-columns:1fr 1fr;gap:1px;background:var(--line)}
.pane{background:var(--pane);padding:10px;min-height:80px;position:relative}
.pane h4{margin:0 0 6px;font-size:11px;letter-spacing:.6px;text-transform:uppercase;color:#5c636b;
  display:flex;justify-content:space-between;align-items:center}
.pane svg{display:block;height:auto;max-width:100%}
.pane-new{cursor:pointer}
.state{font-size:10px;padding:1px 6px;border-radius:999px;background:#0002;color:#333}
.audit-overlay{opacity:0}
.row.auto .pane-new .audit-overlay{animation:flap 2.4s ease-in-out infinite}
.row.high .audit-overlay{opacity:1;animation:none}
.row.plain .audit-overlay{opacity:0;animation:none}
.pane-new:hover .audit-overlay{opacity:1;animation:none}
body.paused .audit-overlay{animation-play-state:paused!important}
@keyframes flap{0%,42%{opacity:0}52%,92%{opacity:1}100%{opacity:0}}
@media (prefers-reduced-motion: reduce){
  .row.auto .pane-new .audit-overlay{animation:none;opacity:0}
}
details{border-top:1px solid var(--line)}
summary{padding:8px 16px;font-size:13px;cursor:pointer;color:var(--muted)}
.detail{padding:0 16px 14px}
table{border-collapse:collapse;width:100%;font-size:13px}
td,th{text-align:left;padding:3px 10px 3px 0;vertical-align:top}
th{color:var(--muted);font-weight:600;font-size:12px}
code,pre{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}
pre{background:var(--bg);border:1px solid var(--line);border-radius:6px;padding:8px 10px;overflow-x:auto}
.k{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;color:var(--changed)}
.k.invariant{color:var(--worse)} .k.geometry{color:var(--moved)}
.added{color:var(--worse)} .fixed{color:var(--better)}
.err{padding:10px 16px;color:var(--worse);font-size:13px}
.note{color:var(--muted);font-size:12px;margin:6px 0 0}
.quiet{color:var(--muted);font-size:13px}
.quiet li{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}
</style></head><body>
<header>
  <h1>layout audit</h1>
  <div class="prov">
    <b>old</b><span>{{.OldDesc}}</span>
    <b>new</b><span>{{.NewDesc}}</span>
    <b>paths</b><span>{{.Paths}}</span>
    <b>swept</b><span>{{.Total}} diagrams in {{.Elapsed}}</span>
  </div>
  <div class="tallies">
    {{if .Counts.Broken}}<span class="pill broken">{{.Counts.Broken}} broken</span>{{end}}
    {{if .Counts.Invariant}}<span class="pill invariant">{{.Counts.Invariant}} invariant</span>{{end}}
    {{if .Counts.Structural}}<span class="pill structural">{{.Counts.Structural}} structural</span>{{end}}
    {{if .Counts.Geometry}}<span class="pill geometry">{{.Counts.Geometry}} geometry</span>{{end}}
    <span class="pill">{{.Counts.Identical}} identical</span>
    {{if .Counts.Skipped}}<span class="pill">{{.Counts.Skipped}} skipped</span>{{end}}
    {{if .Counts.Repaired}}<span class="pill">{{.Counts.Repaired}} repaired</span>{{end}}
  </div>
  <div class="controls">
    <button onclick="setAll('auto')">flap (a)</button>
    <button onclick="setAll('high')">highlighted (h)</button>
    <button onclick="setAll('plain')">plain (n)</button>
    <button onclick="togglePause()">pause (space)</button>
    <select id="tier" onchange="filter()">
      <option value="">all severities</option>
      <option value="broken">broken only</option>
      <option value="invariant">invariant and worse</option>
      <option value="structural">structural and worse</option>
    </select>
    <input id="q" placeholder="filter by name…" oninput="filter()">
  </div>
  <div class="legend">
    <span><i style="background:var(--worse)"></i>removed / invariant broken</span>
    <span><i style="background:var(--better)"></i>added / invariant fixed</span>
    <span><i style="background:var(--changed)"></i>drawn differently</span>
    <span><i style="background:var(--moved)"></i>moved</span>
    <span>hover a right pane to hold the highlight · click to pin</span>
  </div>
</header>
<main>
{{if .LimitNote}}<p class="note">{{.LimitNote}}</p>{{end}}
{{range .Rows}}
<section class="row auto" data-tier="{{.Tier}}" data-id="{{.ID}}">
  <div class="rowhead">
    <span class="rank">{{.Rank}}</span>
    <span class="id">{{.ID}}</span>
    <span class="pill {{.Tier}}">{{.Tier}}</span>
    <span class="score">score {{.Score}} · {{.Bounds}}</span>
  </div>
  {{if .Aliases}}<div class="summary quiet">same source as {{range .Aliases}}<code>{{.}}</code> {{end}}</div>{{end}}
  {{if .Summary}}<div class="summary">{{.Summary}}</div>{{end}}
  {{if .Err}}<div class="err">{{.Err}}</div>{{end}}
  <div class="panes">
    <div class="pane pane-old">
      <h4><span>old</span></h4>
      <div style="width:{{.OldWidth}}">{{.OldSVG}}</div>
    </div>
    <div class="pane pane-new" onclick="cycle(this)">
      <h4><span>new</span><span class="state">flap</span></h4>
      <div style="width:{{.NewWidth}}">{{.NewSVG}}</div>
    </div>
  </div>
  <details>
    <summary>{{len .Changes}} change(s) · commands · layout json</summary>
    <div class="detail">
      <table>
        <tr><th>kind</th><th>what</th><th>detail</th></tr>
        {{range .Changes}}
        <tr><td class="k {{.Tier}}">{{.Kind}}</td>
            <td>{{.Ref}}{{if .Label}} <span class="quiet">{{.Label}}</span>{{end}}</td>
            <td>{{.Detail}}</td></tr>
        {{end}}
      </table>
      {{if .FindingsAdded}}<p class="added"><b>invariants broken</b></p><ul class="quiet">
        {{range .FindingsAdded}}<li>{{.}}</li>{{end}}</ul>{{end}}
      {{if .FindingsFixed}}<p class="fixed"><b>invariants fixed</b></p><ul class="quiet">
        {{range .FindingsFixed}}<li>{{.}}</li>{{end}}</ul>{{end}}
      <pre>{{if .OldCmd}}{{.OldCmd}}
{{end}}{{.NewCmd}}</pre>
      <p class="quiet">source: {{.Origin}}{{if .Line}}:{{.Line}}{{end}}
      {{if .OldLayout}} · <a href="{{.OldLayout}}">old layout.json</a>{{end}}
      {{if .NewLayout}} · <a href="{{.NewLayout}}">new layout.json</a>{{end}}</p>
    </div>
  </details>
</section>
{{end}}

{{if .Identical}}
<details><summary>{{len .Identical}} identical diagram(s)</summary>
<div class="detail"><ul class="quiet">{{range .Identical}}<li>{{.}}</li>{{end}}</ul></div></details>
{{end}}
{{if .Skipped}}
<details><summary>{{len .Skipped}} diagram(s) neither engine lays out</summary>
<div class="detail"><ul class="quiet">{{range .Skipped}}<li>{{.ID}} — {{.Err}}</li>{{end}}</ul></div></details>
{{end}}
{{if .Warnings}}
<details><summary>{{len .Warnings}} warning(s)</summary>
<div class="detail"><ul class="quiet">{{range .Warnings}}<li>{{.}}</li>{{end}}</ul></div></details>
{{end}}
</main>
<script>
const MODES = ['auto','high','plain'];
function setMode(row, mode){
  row.classList.remove('auto','high','plain');
  row.classList.add(mode);
  const s = row.querySelector('.state');
  if (s) s.textContent = mode === 'auto' ? 'flap' : mode;
}
function cycle(pane){
  const row = pane.closest('.row');
  const cur = MODES.find(m => row.classList.contains(m)) || 'auto';
  setMode(row, MODES[(MODES.indexOf(cur)+1) % MODES.length]);
}
function setAll(mode){ document.querySelectorAll('.row').forEach(r => setMode(r, mode)); }
function togglePause(){ document.body.classList.toggle('paused'); }
const RANK = {broken:0, invariant:1, structural:2, geometry:3, repaired:4};
function filter(){
  const want = document.getElementById('tier').value;
  const q = document.getElementById('q').value.toLowerCase();
  document.querySelectorAll('.row').forEach(r => {
    const tier = r.dataset.tier;
    const okTier = !want || (want === 'broken' ? tier === 'broken' : RANK[tier] <= RANK[want]);
    const okText = !q || r.dataset.id.toLowerCase().includes(q);
    r.classList.toggle('hide', !(okTier && okText));
  });
}
document.addEventListener('keydown', e => {
  if (e.target.tagName === 'INPUT' || e.metaKey || e.ctrlKey) return;
  if (e.key === ' ') { e.preventDefault(); togglePause(); }
  if (e.key === 'h') setAll('high');
  if (e.key === 'n') setAll('plain');
  if (e.key === 'a') setAll('auto');
});
</script>
</body></html>
`))
