package main

// The report: an index, and a page per column.
//
// One page for a long history does not work. The whole-history report was
// 3.8 MB with two hundred inline diagrams, and the only reason it was not far
// worse is that panes were being thrown away to keep it openable. Splitting it
// is better than rationing it: the index stays a few kilobytes however long
// the history gets, each column page carries only its own diagrams, and
// nothing has to be dropped.
//
// It also matches how the thing is read — scan the grid, pick the column that
// moved, look at that column — which one long page actively works against.

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
	// Current is the NEWEST engine's rendering of each diagram, so a column
	// can be read against today as well as against the column before it.
	Current map[string][]byte
	// MaxBytes caps ONE PAGE's inlined diagrams (0 = no cap). Per page now,
	// not per report: splitting the report is what made the cap almost never
	// bind, and a cap that never binds is the right kind.
	MaxBytes int
}

// ---- view models -----------------------------------------------------------

type vmCell struct {
	Tier  string // "" = unchanged in that column
	Title string
	Href  string
}

type vmGridRow struct {
	ID    string
	Short string
	Cells []vmCell
	Moves int
}

type vmColumn struct {
	Label, Source, SHA, Subject, Note, Against, Span string
	Href                                             string // "" when the column has no page
	Changed, Identical, Skipped                      int
	Worst                                            string // most severe tier in the column
}

type vmChange struct {
	Kind, Ref, Label, Detail, Tier string
}

type vmRow struct {
	ID, Tier, Score, Summary, Bounds string
	Anchor                           string
	OldSVG, NewSVG, CurSVG           template.HTML
	OldWidth, NewWidth, CurWidth     string
	Changes                          []vmChange
	FindingsAdded                    []string
	Err                              string
	Cmd                              string
}

type vmIndex struct {
	Repo, Sources, Paths, Elapsed, At string
	Diagrams, TotalMoves              int
	WeekLabels                        []string
	Grid                              []vmGridRow
	Columns                           []vmColumn
}

type vmPage struct {
	Label, Source, SHA, Subject, Note, Against, Span string
	Changed, Identical, Skipped                      int
	Rows                                             []vmRow
	Unrendered                                       []string
	PrevHref, PrevLabel                              string
	NextHref, NextLabel                              string
}

// ---- assembly --------------------------------------------------------------

// pageDir is a column's directory under the report root.
func pageDir(label string) string { return "w/" + layoutaudit.Sanitize(label) }

// hasPage reports whether a column is worth its own page: one with nothing to
// show would be a link to an empty room.
func hasPage(w week) bool { return len(w.Changes) > 0 }

// buildIndex assembles the front page: the grid, then the columns in order.
func buildIndex(in timelineInput) vmIndex {
	m := vmIndex{
		Repo: in.Repo, Sources: in.Sources, Paths: strings.Join(in.Paths, " "),
		Diagrams: in.Diagrams, Elapsed: in.Elapsed.Round(time.Millisecond).String(), At: in.At,
	}

	cellsByID := map[string][]vmCell{}
	for wi, w := range in.Weeks {
		m.WeekLabels = append(m.WeekLabels, w.Label)
		href := ""
		if hasPage(w) {
			href = pageDir(w.Label) + "/index.html"
		}
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
			// A short title, deliberately: the grid holds one cell per
			// (diagram, column), which on a long history is tens of
			// thousands of them, and a full summary on each turned the index
			// into 867 KB of tooltip. The page behind the cell has the detail.
			cell := vmCell{Tier: tier, Title: w.Label + " · " + tier}
			if href != "" {
				cell.Href = href + "#" + anchor(c.ID)
			}
			row[wi] = cell
			m.TotalMoves++
		}

		m.Columns = append(m.Columns, vmColumn{
			Label: w.Label, Source: w.Source, SHA: layoutaudit.Short(w.SHA), Subject: w.Subject,
			Note: w.Note, Against: w.Against, Span: w.Span, Href: href,
			Changed: len(w.Changes), Identical: w.Identical, Skipped: w.Skipped,
			Worst: worstTier(w),
		})
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
	return m
}

// worstTier is the most severe thing that happened in a column, for the
// index's at-a-glance count.
func worstTier(w week) string {
	rank := map[string]int{"broken": 0, "invariant": 1, "structural": 2, "geometry": 3, "repaired": 4}
	worst := ""
	for _, c := range w.Changes {
		t := c.Status
		if c.Status == "changed" {
			t = c.Report.Tier.String()
		}
		if worst == "" || rank[t] < rank[worst] {
			worst = t
		}
	}
	return worst
}

// buildPage assembles one column's page, with its neighbours for stepping.
func buildPage(in timelineInput, i int) vmPage {
	w := in.Weeks[i]
	p := vmPage{
		Label: w.Label, Source: w.Source, SHA: layoutaudit.Short(w.SHA), Subject: w.Subject,
		Note: w.Note, Against: w.Against, Span: w.Span,
		Changed: len(w.Changes), Identical: w.Identical, Skipped: w.Skipped,
	}
	for j := i - 1; j >= 0; j-- {
		if hasPage(in.Weeks[j]) {
			p.PrevHref = "../" + layoutaudit.Sanitize(in.Weeks[j].Label) + "/index.html"
			p.PrevLabel = in.Weeks[j].Label
			break
		}
	}
	for j := i + 1; j < len(in.Weeks); j++ {
		if hasPage(in.Weeks[j]) {
			p.NextHref = "../" + layoutaudit.Sanitize(in.Weeks[j].Label) + "/index.html"
			p.NextLabel = in.Weeks[j].Label
			break
		}
	}
	spent := 0
	for _, c := range w.Changes {
		cur := in.Current[c.ID]
		// "Has something to draw" is about the row's OWN panes. Counting the
		// current-engine overlay here drew rows whose before/after were both
		// missing as empty frames with one lonely picture in the reference —
		// which is what a reader sees as a broken page.
		own := len(c.OldSVG) + len(c.NewSVG)
		size := own + len(cur)
		// Decide BEFORE drawing, or the row that breaks the budget is the one
		// that gets drawn. The first row is always drawn: a page with a cap
		// and no picture would be a worse answer than a page slightly over.
		over := in.MaxBytes > 0 && spent > 0 && spent+size > in.MaxBytes
		if over || (own == 0 && c.Status == "changed") {
			p.Unrendered = append(p.Unrendered, c.ID)
			continue
		}
		spent += size
		p.Rows = append(p.Rows, buildRow(w, c, cur))
	}
	return p
}

func buildRow(w week, c change, cur []byte) vmRow {
	tier := c.Status
	if c.Status == "changed" {
		tier = c.Report.Tier.String()
	}
	row := vmRow{
		ID: c.ID, Tier: tier, Anchor: anchor(c.ID),
		Score:         fmt.Sprintf("%.0f", c.Report.Score),
		Summary:       layoutaudit.Summarize(c.Report),
		OldSVG:        template.HTML(layoutaudit.InlineSVG(c.OldSVG)), //nolint:gosec // our own renderer
		NewSVG:        template.HTML(layoutaudit.InlineSVG(c.NewSVG)), //nolint:gosec
		CurSVG:        template.HTML(layoutaudit.InlineSVG(cur)),      //nolint:gosec
		FindingsAdded: c.Report.FindingsAdded,
		Err:           c.Err,
	}
	ob, nb := c.Report.OldBounds, c.Report.NewBounds
	row.Bounds = fmt.Sprintf("%d×%d", nb.Width, nb.Height)
	if ob != nb {
		row.Bounds = fmt.Sprintf("%d×%d → %d×%d", ob.Width, ob.Height, nb.Width, nb.Height)
	}
	row.OldWidth, row.NewWidth = layoutaudit.PaneWidths(ob.Width, nb.Width)
	// The current rendering shares the reference pane's frame, so switching
	// between "previous" and "current" does not resize the picture.
	row.CurWidth = row.OldWidth
	for _, ch := range c.Report.Changes {
		row.Changes = append(row.Changes, vmChange{
			Kind: ch.Kind, Ref: ch.Ref, Label: ch.Label, Detail: ch.Detail, Tier: ch.Tier.String(),
		})
	}
	row.Cmd = fmt.Sprintf("go run ./cmd-dev/layout-audit --old %s --new %s", prevOf(w), w.SHA)
	return row
}

// prevOf names the ref the column is compared against, for the copy-paste
// command that reproduces it as a full audit.
func prevOf(w week) string {
	if w.SHA == "" {
		return "HEAD"
	}
	return w.SHA + "~1"
}

// anchor identifies a row within its column's page.
func anchor(id string) string { return "d-" + layoutaudit.Sanitize(id) }

// shortID keeps the tail of a long path, which is the part that identifies a
// diagram in a grid row.
func shortID(id string) string {
	if len(id) <= 46 {
		return id
	}
	return "…" + id[len(id)-45:]
}

func tierClass(t string) string { return t }

// ---- rendering -------------------------------------------------------------

func renderIndex(in timelineInput) string {
	var b strings.Builder
	if err := indexTmpl.Execute(&b, buildIndex(in)); err != nil {
		return errPage("index", err)
	}
	return b.String()
}

func renderPage(in timelineInput, i int) string {
	var b strings.Builder
	if err := pageTmpl.Execute(&b, buildPage(in, i)); err != nil {
		return errPage("column", err)
	}
	return b.String()
}

func errPage(what string, err error) string {
	return "<!doctype html><meta charset=utf-8><pre>" + what + " template: " +
		template.HTMLEscapeString(err.Error()) + "</pre>"
}

// sharedCSS is the look both pages share. The pane BEHAVIOUR comes from
// pkg/layoutaudit ({{paneCSS}}), so the two reports cannot drift.
const sharedCSS = `
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
header{padding:18px 26px 14px;border-bottom:1px solid var(--line);background:var(--card)}
h1{margin:0 0 8px;font-size:19px}
h2{font-size:16px;margin:26px 0 10px}
a{color:inherit}
.prov{display:grid;grid-template-columns:auto 1fr;gap:2px 12px;font-size:13px;color:var(--muted)}
.prov b{color:var(--ink);font-weight:600}
main{padding:18px 26px 60px;max-width:1600px;margin:0 auto}
.pill{padding:1px 8px;border-radius:999px;border:1px solid var(--line);font-size:12px}
.pill.invariant{border-color:var(--worse);color:var(--worse);font-weight:600}
.pill.structural{border-color:var(--changed);color:var(--changed);font-weight:600}
.pill.geometry{border-color:var(--moved);color:var(--moved)}
.pill.broken{background:var(--worse);color:#fff;border-color:var(--worse);font-weight:700}
.pill.repaired{border-color:var(--better);color:var(--better)}
.quiet{color:var(--muted);font-size:12px}
.sha{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;color:var(--muted);font-size:12px}
.note{padding:8px 0;color:var(--muted);font-size:13px}
pre{background:var(--bg);border:1px solid var(--line);border-radius:6px;padding:7px 10px;overflow-x:auto;font-size:12px}
`

var indexTmpl = template.Must(template.New("index").
	Funcs(template.FuncMap{"tier": tierClass}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>layout timeline</title>
<style>` + sharedCSS + `
.gridwrap{overflow-x:auto;background:var(--card);border:1px solid var(--line);border-radius:10px;padding:10px}
table.grid{border-collapse:collapse;font-size:12px}
table.grid th{font-weight:600;color:var(--muted);padding:2px 4px;text-align:left;white-space:nowrap}
table.grid th.wk{writing-mode:vertical-rl;transform:rotate(180deg);height:74px;font-variant-numeric:tabular-nums}
table.grid td{padding:1px 2px}
table.grid td.name{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;white-space:nowrap;padding-right:10px}
/* An unchanged cell is an EMPTY <td>, drawn by CSS. On a long history the
   grid holds tens of thousands of them (224 diagrams × 89 columns here), and
   spelling each one out in markup cost more than everything else on the page
   put together. */
table.grid td:empty::before{content:"";display:block;width:16px;height:16px;
  border-radius:3px;background:var(--line);opacity:.35}
.cell{display:block;width:16px;height:16px;border-radius:3px;background:var(--line)}
a.cell{text-decoration:none}
.cell.invariant{background:var(--worse)} .cell.structural{background:var(--changed)}
.cell.geometry{background:var(--moved)} .cell.broken{background:#000;border:2px solid var(--worse)}
.cell.repaired{background:var(--better)}
.moves{color:var(--muted);font-variant-numeric:tabular-nums;padding-left:8px}
table.cols{border-collapse:collapse;width:100%;background:var(--card);border:1px solid var(--line);border-radius:10px}
table.cols th{text-align:left;font-size:12px;color:var(--muted);padding:8px 10px;border-bottom:1px solid var(--line)}
table.cols td{padding:7px 10px;border-bottom:1px solid var(--line);font-size:13px;vertical-align:top}
table.cols tr:last-child td{border-bottom:none}
table.cols tr.quietrow td{color:var(--muted)}
td.n{font-variant-numeric:tabular-nums;text-align:right;white-space:nowrap}
td.date{white-space:nowrap;font-weight:600}
.legend{display:flex;gap:14px;font-size:12px;color:var(--muted);margin:10px 0 0;flex-wrap:wrap}
.legend i{display:inline-block;width:11px;height:11px;border-radius:3px;margin-right:4px;vertical-align:-1px}
</style></head><body>
<header>
  <h1>layout timeline — today's diagrams, column by column</h1>
  <div class="prov">
    <b>history</b><span>{{.Repo}}</span>
    <b>sources</b><span>{{.Diagrams}} diagrams from {{.Sources}} ({{.Paths}}) — fixed; only the engine moves</span>
    <b>columns</b><span>{{len .WeekLabels}} ({{.At}}), {{.TotalMoves}} diagram-change(s), {{.Elapsed}}</span>
  </div>
  <div class="legend">
    <span><i class="cell invariant" style="opacity:1"></i>invariant got worse</span>
    <span><i class="cell structural" style="opacity:1"></i>drawn differently</span>
    <span><i class="cell geometry" style="opacity:1"></i>moved</span>
    <span><i class="cell broken" style="opacity:1"></i>could not lay it out</span>
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
    {{range .Cells}}{{if .Tier}}<td>{{if .Href}}<a class="cell {{tier .Tier}}" href="{{.Href}}" title="{{.Title}}"></a>{{else}}<span class="cell {{tier .Tier}}" title="{{.Title}}"></span>{{end}}</td>{{else}}<td></td>{{end}}{{end}}
    <td class="moves">{{.Moves}}</td>
  </tr>
  {{end}}
</table>
</div>
{{else}}
<p class="quiet">No diagram changed in any column of this range.</p>
{{end}}

<h2>Columns</h2>
<table class="cols">
  <tr><th>column</th><th>lineage</th><th>commit</th><th class="n">changed</th><th class="n">same</th><th>what happened</th></tr>
  {{range .Columns}}
  <tr {{if not .Href}}class="quietrow"{{end}}>
    <td class="date">{{if .Href}}<a href="{{.Href}}">{{.Label}}</a>{{else}}{{.Label}}{{end}}</td>
    <td>{{if .Source}}<span class="pill">{{.Source}}</span>{{end}}</td>
    <td><span class="sha">{{.SHA}}</span> {{.Subject}}</td>
    <td class="n">{{if .Changed}}<span class="pill {{tier .Worst}}">{{.Changed}}</span>{{end}}</td>
    <td class="n">{{if .Identical}}{{.Identical}}{{end}}</td>
    <td class="quiet">{{if .Note}}{{.Note}}{{else}}{{if .Against}}vs {{.Against}}{{end}} {{if .Span}}· {{.Span}}{{end}}{{end}}</td>
  </tr>
  {{end}}
</table>
</main>
</body></html>
`))

var pageTmpl = template.Must(template.New("page").
	Funcs(template.FuncMap{"tier": tierClass}).
	Funcs(layoutaudit.PaneFuncs()).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Label}} — layout timeline</title>
<style>` + sharedCSS + `
{{paneCSS}}
.row{background:var(--card);border:1px solid var(--line);border-radius:10px;margin-bottom:18px;overflow:hidden}
.rowhead{padding:9px 16px;display:flex;gap:10px;align-items:baseline;flex-wrap:wrap;border-bottom:1px solid var(--line)}
.id{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12.5px;word-break:break-all}
.summary{padding:8px 16px;font-size:13px;color:var(--muted);border-bottom:1px solid var(--line)}
details{border-top:1px solid var(--line)}
summary{padding:7px 16px;font-size:13px;cursor:pointer;color:var(--muted)}
.detail{padding:0 16px 12px}
table.ch{border-collapse:collapse;width:100%;font-size:12.5px}
table.ch td,table.ch th{text-align:left;padding:2px 10px 2px 0;vertical-align:top}
.k{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;color:var(--changed)}
.k.invariant{color:var(--worse)} .k.geometry{color:var(--moved)}
nav{display:flex;gap:14px;align-items:center;font-size:13px;margin-top:8px}
nav a{color:var(--muted)}
</style></head><body>
<header>
  <h1>{{.Label}} {{if .Source}}<span class="pill">{{.Source}}</span>{{end}}
      <span class="sha">{{.SHA}}</span></h1>
  <div class="prov">
    <b>commit</b><span>{{.Subject}}</span>
    <b>compared</b><span>{{if .Against}}against {{.Against}} — {{end}}{{.Changed}} changed, {{.Identical}} identical{{if .Skipped}}, {{.Skipped}} skipped{{end}}</span>
    {{if .Span}}<b>spans</b><span>{{.Span}}</span>{{end}}
  </div>
  <nav>
    <a href="../../index.html">← all columns</a>
    {{if .PrevHref}}<a href="{{.PrevHref}}">← {{.PrevLabel}}</a>{{end}}
    {{if .NextHref}}<a href="{{.NextHref}}">{{.NextLabel}} →</a>{{end}}
  </nav>
  {{if .Note}}<div class="note">{{.Note}}</div>{{end}}
</header>
<main>
{{range .Rows}}
<section class="row first{{if not .OldSVG}} no-before{{end}}" id="{{.Anchor}}">
  <div class="rowhead">
    <span class="id">{{.ID}}</span>
    <span class="pill {{tier .Tier}}">{{.Tier}}</span>
    <span class="quiet">score {{.Score}} · {{.Bounds}}</span>
  </div>
  {{if .Summary}}<div class="summary">{{.Summary}}</div>{{end}}
  {{if .Err}}<div class="summary">{{.Err}}</div>{{end}}
  <div class="panes">
    <div class="pane pane-old">
      <h4><span>reference</span>{{if .CurSVG}}{{leftControls}}{{end}}</h4>
      <div class="stack">
        <div class="layer layer-prev" style="width:{{.OldWidth}}">{{.OldSVG}}<span class="chip">previous</span></div>
        {{if .CurSVG}}<div class="layer layer-current" style="width:{{.CurWidth}}">{{.CurSVG}}<span class="chip">current</span></div>{{end}}
      </div>
    </div>
    <div class="pane pane-new">
      <h4><span>this column</span>{{paneControls}}</h4>
      <div class="stack">
        {{if .OldSVG}}<div class="layer layer-before" style="width:{{.OldWidth}}">{{.OldSVG}}<span class="chip">before</span></div>{{end}}
        <div class="layer layer-after" style="width:{{.NewWidth}}">{{.NewSVG}}<span class="chip">after</span></div>
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
</section>
{{end}}
{{if .Unrendered}}
<details><summary>{{len .Unrendered}} further changed diagram(s), not drawn</summary>
<div class="detail"><ul class="quiet">{{range .Unrendered}}<li>{{.}}</li>{{end}}</ul></div></details>
{{end}}
</main>
<script>
{{paneJS}}
</script>
</body></html>
`))
