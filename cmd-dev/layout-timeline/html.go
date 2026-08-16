package main

// The timeline report: a grid first, then the weeks.
//
// The grid is the point. One row per diagram that ever moved, one column per
// week, a coloured cell where it moved — so "when did this diagram last
// change, and was it ever broken" is answered by looking, not by reading. The
// per-week sections underneath carry the same old/new panes as layout-audit,
// with the same flap.

import (
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"github.com/infinite-pm/ipm-tools/pkg/layoutaudit"
)

type timelineInput struct {
	Repo     string
	Sources  string
	Paths    []string
	Diagrams int
	Weeks    []week
	Elapsed  time.Duration
	At       string
	NoSVG    bool
}

type vmCell struct {
	Tier   string // "" = unchanged that week
	Title  string
	Anchor string
}

type vmGridRow struct {
	ID    string
	Short string
	Cells []vmCell
	Moves int
}

type vmChange struct {
	Kind, Ref, Label, Detail, Tier string
}

type vmRow struct {
	ID, Tier, Score, Summary, Bounds string
	Anchor                           string
	OldSVG, NewSVG                   template.HTML
	OldWidth, NewWidth               string
	Changes                          []vmChange
	FindingsAdded                    []string
	Err                              string
	Cmd                              string
}

type vmWeek struct {
	Label, SHA, Subject, Note   string
	Against, Span, Source       string
	PanesDropped                bool
	Changed, Identical, Skipped int
	Rows                        []vmRow
	Unrendered                  []string
}

type vmModel struct {
	Repo, Sources, Paths, Elapsed, At string
	Diagrams                          int
	WeekLabels                        []string
	Grid                              []vmGridRow
	Weeks                             []vmWeek
	TotalMoves                        int
	NoSVG                             bool
}

func renderHTML(in timelineInput) string {
	m := vmModel{
		Repo: in.Repo, Sources: in.Sources, Paths: strings.Join(in.Paths, " "), Diagrams: in.Diagrams,
		Elapsed: in.Elapsed.Round(time.Millisecond).String(), At: in.At, NoSVG: in.NoSVG,
	}

	// cellsByID[diagram][weekIndex] = tier label
	cellsByID := map[string][]vmCell{}
	for wi, w := range in.Weeks {
		m.WeekLabels = append(m.WeekLabels, w.Label)
		for _, c := range w.Changes {
			row, ok := cellsByID[c.ID]
			if !ok {
				row = make([]vmCell, len(in.Weeks))
				cellsByID[c.ID] = row
			}
			tier := c.Status
			if c.Status == "changed" {
				tier = c.Report.Tier.String()
			}
			row[wi] = vmCell{
				Tier:   tier,
				Title:  fmt.Sprintf("%s — %s: %s", w.Label, tier, layoutaudit.Summarize(c.Report)),
				Anchor: anchor(w.Label, c.ID),
			}
			m.TotalMoves++
		}
	}
	ids := make([]string, 0, len(cellsByID))
	for id := range cellsByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		cells := cellsByID[id]
		moves := 0
		for _, c := range cells {
			if c.Tier != "" {
				moves++
			}
		}
		m.Grid = append(m.Grid, vmGridRow{ID: id, Short: shortID(id), Cells: cells, Moves: moves})
	}
	// Busiest diagrams first: the ones that moved most are the ones a reader
	// most wants to understand.
	sort.SliceStable(m.Grid, func(i, j int) bool {
		if m.Grid[i].Moves != m.Grid[j].Moves {
			return m.Grid[i].Moves > m.Grid[j].Moves
		}
		return m.Grid[i].ID < m.Grid[j].ID
	})

	for _, w := range in.Weeks {
		vw := vmWeek{Label: w.Label, SHA: layoutaudit.Short(w.SHA), Subject: w.Subject,
			Note: w.Note, Against: w.Against, Span: w.Span, Source: w.Source,
			PanesDropped: w.PanesDropped,
			Changed:      len(w.Changes), Identical: w.Identical, Skipped: w.Skipped}
		for _, c := range w.Changes {
			if len(c.OldSVG) == 0 && len(c.NewSVG) == 0 && c.Status == "changed" {
				_ = w.PanesDropped
				vw.Unrendered = append(vw.Unrendered, c.ID)
				continue
			}
			vw.Rows = append(vw.Rows, buildRow(w, c))
		}
		m.Weeks = append(m.Weeks, vw)
	}

	var b strings.Builder
	if err := timelineTmpl.Execute(&b, m); err != nil {
		return "<!doctype html><meta charset=utf-8><pre>timeline template: " +
			template.HTMLEscapeString(err.Error()) + "</pre>"
	}
	return b.String()
}

func buildRow(w week, c change) vmRow {
	tier := c.Status
	if c.Status == "changed" {
		tier = c.Report.Tier.String()
	}
	row := vmRow{
		ID: c.ID, Tier: tier, Anchor: anchor(w.Label, c.ID),
		Score:         fmt.Sprintf("%.0f", c.Report.Score),
		Summary:       layoutaudit.Summarize(c.Report),
		OldSVG:        template.HTML(layoutaudit.InlineSVG(c.OldSVG)), //nolint:gosec // our own renderer
		NewSVG:        template.HTML(layoutaudit.InlineSVG(c.NewSVG)), //nolint:gosec
		FindingsAdded: c.Report.FindingsAdded,
		Err:           c.Err,
	}
	ob, nb := c.Report.OldBounds, c.Report.NewBounds
	row.Bounds = fmt.Sprintf("%d×%d", nb.Width, nb.Height)
	if ob != nb {
		row.Bounds = fmt.Sprintf("%d×%d → %d×%d", ob.Width, ob.Height, nb.Width, nb.Height)
	}
	row.OldWidth, row.NewWidth = layoutaudit.PaneWidths(ob.Width, nb.Width)
	for _, ch := range c.Report.Changes {
		row.Changes = append(row.Changes, vmChange{
			Kind: ch.Kind, Ref: ch.Ref, Label: ch.Label, Detail: ch.Detail, Tier: ch.Tier.String(),
		})
	}
	row.Cmd = fmt.Sprintf("go run ./cmd-dev/layout-audit --old %s --new %s", prevOf(w), w.SHA)
	return row
}

// prevOf names the ref the week is being compared against, for the
// copy-paste command that reproduces one week as a full audit.
func prevOf(w week) string {
	if w.SHA == "" {
		return "HEAD"
	}
	return w.SHA + "~1"
}

// anchor names a row so the grid can link to it. Both halves go through
// Sanitize: the "now" column's label carries a space, which survives into an
// id attribute and quietly breaks the link it is the target of.
func anchor(week, id string) string {
	return "w-" + layoutaudit.Sanitize(week) + "-" + layoutaudit.Sanitize(id)
}

// shortID keeps the tail of a long path, which is the part that identifies a
// diagram in a grid row.
func shortID(id string) string {
	if len(id) <= 46 {
		return id
	}
	return "…" + id[len(id)-45:]
}

// tierOf is used by the template to colour a cell.
func tierClass(t string) string { return t }

var timelineTmpl = template.Must(template.New("timeline").
	Funcs(template.FuncMap{"tier": tierClass}).
	Funcs(layoutaudit.PaneFuncs()).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>layout timeline</title>
<style>
:root{
  --bg:#f6f7f9; --card:#fff; --ink:#1a1c1f; --muted:#5c636b; --line:#dfe3e8;
  --worse:#e03131; --better:#2f9e44; --changed:#7048e8; --moved:#f08c00; --pane:#fbfbfc;
}
@media (prefers-color-scheme: dark){
  :root{ --bg:#16181c; --card:#1e2126; --ink:#e8eaed; --muted:#9aa3ad; --line:#2c3138; --pane:#f4f5f7; }
}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);
  font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Inter,Helvetica,Arial,sans-serif}
header{padding:20px 26px 14px;border-bottom:1px solid var(--line);background:var(--card)}
h1{margin:0 0 8px;font-size:19px}
h2{font-size:16px;margin:28px 0 10px}
.prov{display:grid;grid-template-columns:auto 1fr;gap:2px 12px;font-size:13px;color:var(--muted)}
.prov b{color:var(--ink);font-weight:600}
main{padding:18px 26px 60px;max-width:1600px;margin:0 auto}
.gridwrap{overflow-x:auto;background:var(--card);border:1px solid var(--line);border-radius:10px;padding:10px}
table.grid{border-collapse:collapse;font-size:12px}
table.grid th{font-weight:600;color:var(--muted);padding:2px 4px;text-align:left;white-space:nowrap}
table.grid th.wk{writing-mode:vertical-rl;transform:rotate(180deg);height:74px;font-variant-numeric:tabular-nums}
table.grid td{padding:1px 2px}
table.grid td.name{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;white-space:nowrap;padding-right:10px}
.cell{display:block;width:16px;height:16px;border-radius:3px;background:var(--line);opacity:.35}
a.cell{opacity:1;text-decoration:none}
.cell.invariant{background:var(--worse)} .cell.structural{background:var(--changed)}
.cell.geometry{background:var(--moved)} .cell.broken{background:#000;border:2px solid var(--worse)}
.cell.repaired{background:var(--better)}
.moves{color:var(--muted);font-variant-numeric:tabular-nums;padding-left:8px}
.week{background:var(--card);border:1px solid var(--line);border-radius:10px;margin:14px 0;overflow:hidden}
.weekhead{padding:10px 16px;border-bottom:1px solid var(--line);display:flex;gap:10px;align-items:baseline;flex-wrap:wrap}
.weekhead .date{font-weight:700}
.weekhead .sha{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;color:var(--muted);font-size:12px}
.weekhead .subject{color:var(--muted);font-size:13px}
.tallies{margin-left:auto;font-size:12px;color:var(--muted)}
.note{padding:10px 16px;color:var(--muted);font-size:13px}
.row{border-top:1px solid var(--line)}
.rowhead{padding:9px 16px;display:flex;gap:10px;align-items:baseline;flex-wrap:wrap}
.id{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12.5px;word-break:break-all}
.pill{padding:1px 8px;border-radius:999px;border:1px solid var(--line);font-size:12px}
.pill.invariant{border-color:var(--worse);color:var(--worse);font-weight:600}
.pill.structural{border-color:var(--changed);color:var(--changed);font-weight:600}
.pill.geometry{border-color:var(--moved);color:var(--moved)}
.pill.broken{background:var(--worse);color:#fff;border-color:var(--worse);font-weight:700}
.summary{padding:0 16px 8px;font-size:13px;color:var(--muted)}
{{paneCSS}}
details{border-top:1px solid var(--line)}
summary{padding:7px 16px;font-size:13px;cursor:pointer;color:var(--muted)}
.detail{padding:0 16px 12px}
table.ch{border-collapse:collapse;width:100%;font-size:12.5px}
table.ch td,table.ch th{text-align:left;padding:2px 10px 2px 0;vertical-align:top}
.k{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;color:var(--changed)}
.k.invariant{color:var(--worse)} .k.geometry{color:var(--moved)}
pre{background:var(--bg);border:1px solid var(--line);border-radius:6px;padding:7px 10px;overflow-x:auto;font-size:12px}
.quiet{color:var(--muted);font-size:12px}
.legend{display:flex;gap:14px;font-size:12px;color:var(--muted);margin:10px 0 0;flex-wrap:wrap}
.legend i{display:inline-block;width:11px;height:11px;border-radius:3px;margin-right:4px;vertical-align:-1px}
</style></head><body>
<header>
  <h1>layout timeline — today's diagrams, week by week</h1>
  <div class="prov">
    <b>repo</b><span>{{.Repo}}</span>
    <b>sources</b><span>{{.Diagrams}} diagrams from {{.Sources}} ({{.Paths}}) — fixed; only the engine moves</span>
    <b>weeks</b><span>{{len .WeekLabels}} Monday snapshots ({{.At}}), {{.TotalMoves}} diagram-change(s), {{.Elapsed}}</span>
  </div>
  <div class="legend">
    <span><i class="cell invariant" style="opacity:1"></i>invariant got worse</span>
    <span><i class="cell structural" style="opacity:1"></i>drawn differently</span>
    <span><i class="cell geometry" style="opacity:1"></i>moved</span>
    <span><i class="cell broken" style="opacity:1"></i>engine could not lay it out</span>
    <span><i class="cell repaired" style="opacity:1"></i>became layoutable</span>
  </div>
</header>
<main>
{{if .Grid}}
<h2>When each diagram moved</h2>
<div class="gridwrap">
<table class="grid">
  <tr><th>diagram</th>{{range .WeekLabels}}<th class="wk">{{.}}</th>{{end}}<th class="moves">moves</th></tr>
  {{range .Grid}}
  <tr>
    <td class="name" title="{{.ID}}">{{.Short}}</td>
    {{range .Cells}}<td>{{if .Tier}}<a class="cell {{tier .Tier}}" href="#{{.Anchor}}" title="{{.Title}}"></a>{{else}}<span class="cell"></span>{{end}}</td>{{end}}
    <td class="moves">{{.Moves}}</td>
  </tr>
  {{end}}
</table>
</div>
{{else}}
<p class="quiet">No diagram changed in any week of this range.</p>
{{end}}

<h2>Week by week</h2>
{{range .Weeks}}
<section class="week">
  <div class="weekhead">
    <span class="date">{{.Label}}</span>
    {{if .Source}}<span class="pill">{{.Source}}</span>{{end}}
    {{if .SHA}}<span class="sha">{{.SHA}}</span>{{end}}
    <span class="subject">{{.Subject}}</span>
    {{if .Against}}<span class="quiet">vs {{.Against}}</span>{{end}}
    {{if .Span}}<span class="quiet">· {{.Span}}</span>{{end}}
    <span class="tallies">{{if .Changed}}{{.Changed}} changed · {{end}}{{.Identical}} identical{{if .Skipped}} · {{.Skipped}} skipped{{end}}</span>
  </div>
  {{if .Note}}<div class="note">{{.Note}}</div>{{end}}
  {{if .PanesDropped}}<div class="note">diagrams not drawn for this column — the report would not open; the changes below are complete</div>{{end}}
  {{range .Rows}}
  <div class="row first{{if not .OldSVG}} no-before{{end}}" id="{{.Anchor}}">
    <div class="rowhead">
      <span class="id">{{.ID}}</span>
      <span class="pill {{tier .Tier}}">{{.Tier}}</span>
      <span class="quiet">score {{.Score}} · {{.Bounds}}</span>
    </div>
    {{if .Summary}}<div class="summary">{{.Summary}}</div>{{end}}
    {{if .Err}}<div class="summary">{{.Err}}</div>{{end}}
    <div class="panes">
      <div class="pane"><h4><span>before</span></h4><div style="width:{{.OldWidth}}">{{.OldSVG}}</div></div>
      <div class="pane pane-new">
        <h4><span>this week</span>{{paneControls}}</h4>
        <div class="stack">
          {{if .OldSVG}}<div class="layer layer-before" style="width:{{.OldWidth}}">{{.OldSVG}}<span class="chip">before</span></div>{{end}}
          <div class="layer layer-after" style="width:{{.NewWidth}}">{{.NewSVG}}<span class="chip">this week</span></div>
        </div>
      </div>
    </div>
    <details>
      <summary>{{len .Changes}} change(s)</summary>
      <div class="detail">
        <table class="ch">
          {{range .Changes}}<tr><td class="k {{tier .Tier}}">{{.Kind}}</td><td>{{.Ref}} <span class="quiet">{{.Label}}</span></td><td>{{.Detail}}</td></tr>{{end}}
        </table>
        {{if .FindingsAdded}}<ul class="quiet">{{range .FindingsAdded}}<li>{{.}}</li>{{end}}</ul>{{end}}
        <pre>{{.Cmd}}</pre>
      </div>
    </details>
  </div>
  {{end}}
  {{if .Unrendered}}
  <details><summary>{{len .Unrendered}} further changed diagram(s), not rendered</summary>
  <div class="detail"><ul class="quiet">{{range .Unrendered}}<li>{{.}}</li>{{end}}</ul></div></details>
  {{end}}
</section>
{{end}}
</main>
<script>
{{paneJS}}
</script>
</body></html>
`))
