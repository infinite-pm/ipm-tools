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
	// Corpus describes how the diagram set moved since the last report.
	Corpus []string
	// Panes maps (column, diagram, which) to the shared, content-addressed
	// file that holds that picture.
	//
	// The COLUMN is part of the key and cannot be dropped. Keyed by (diagram,
	// which) alone the entries collide — one diagram has a "before" per
	// column, not one in total — and the last column written silently won.
	// Every page then showed the newest rendering whatever column it claimed
	// to be: a diagram page of fourteen versions displayed the same picture
	// fourteen times, and the report looked convincing while saying nothing.
	Panes map[string]string
	// IPMT is each diagram's source text. Every picture on a diagram page is
	// this one input read by a different engine, so the input is the thing
	// worth having at hand when the pictures disagree — and worth copying out
	// to reproduce one of them.
	IPMT map[string]string
}

// pane is where one of a row's pictures lives, "" when there is none.
func (in timelineInput) pane(col, id, which string) string {
	return in.Panes[col+"\x00"+id+"\x00"+which]
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
	// QuietAfter counts the columns following this one in which nothing
	// moved. They are not shown: a grid of 89 columns where 72 are blank is
	// mostly blank, and the eye has to find the 17 that matter. The count
	// keeps the time they represent visible.
	QuietAfter int
	QuietWhich string // their labels, for the tooltip
}

type vmChange struct {
	Kind, Ref, Label, Detail, Tier string
}

type vmRow struct {
	ID, Tier, Score, Summary, Bounds string
	Anchor                           string
	// Panes are FILES now; these are the hrefs, "" when there is none.
	BeforeSrc, AfterSrc, MarkedSrc, CurrentSrc string
	OldWidth, NewWidth, CurWidth               string
	Changes                                    []vmChange
	FindingsAdded                              []string
	Err                                        string
	Cmd                                        string
	// History is THIS diagram's own timeline: one box per column in which it
	// moved, the current one marked. With the arrows it walks a single
	// diagram through the history without going back to the index.
	History            []vmHistCell
	Moves              int
	HistPrev, HistNext string
	HistPrevLabel      string
	HistNextLabel      string
}

// vmVersion is one column's picture of ONE diagram, on that diagram's page.
type vmVersion struct {
	Label, Anchor, Source, SHA, Subject string
	Tier, Score, Summary, Bounds        string
	ColumnHref                          string // the column page, with every diagram
	// One pane, three states. There is no "current" here: the reference a
	// version is read against is the row above it, and the column page is
	// where a diagram is read against today.
	BeforeSrc, AfterSrc, MarkedSrc string
	OldWidth, NewWidth             string
	Changes                        []vmChange
	FindingsAdded                  []string
	Err                            string
	// Repo is the checkout this engine commit lives in — not always the one
	// the diagrams come from, and not always the same as the version before.
	Repo string
	// The version before this one on the page: what the "before" picture is,
	// and the other half of any regression report.
	PrevLabel, PrevSHA, PrevSource, PrevRepo string
	// Commit dates and what counts as the engine in this lineage. A report
	// that says "it broke between these two" is only actionable with the
	// means to narrow it: these are what the bisect commands need.
	Date, PrevDate string
	EnginePaths    []string
	// Ready-to-paste markdown. Built here rather than in the browser so it can
	// be tested, and because everything it needs is already in this struct.
	AgentMD, RegressionMD string
}

// vmDiagram is one diagram's whole life: every column it moved in, on one
// page. Following a history box used to open a column page carrying two
// hundred diagrams to show one of them; this carries one.
type vmDiagram struct {
	ID       string
	Moves    int
	History  []vmHistCell
	Versions []vmVersion
	IndexRef string
	// IPMT is the one input every version on this page was rendered from.
	IPMT  string
	Lines int
}

// vmHistCell is one box in a diagram's own history strip.
type vmHistCell struct {
	Label, Tier, Href string
	// Now is the version being shown — the "after" picture. Seen is the one
	// supplying its "before". A comparison is of TWO columns, and a strip
	// that marks only one leaves the reader to work out what the left-hand
	// picture came from.
	Now  bool
	Seen bool
	// Anchor identifies the cell to the script that moves the marks as a
	// diagram page is scrolled.
	Anchor string
	// AfterPane is the file this column rendered. The script marks a
	// predecessor only when its AfterPane IS the shown version's before —
	// the same identity check markSeen does for a column page.
	AfterPane string
}

type vmIndex struct {
	Repo, Sources, Paths, Elapsed, At string
	Diagrams, TotalMoves              int
	WeekLabels                        []string
	Grid                              []vmGridRow
	Columns                           []vmColumn
	QuietTotal                        int
	QuietList                         []string
	Corpus                            []string
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

// shownColumns are the columns worth a place: the ones in which something
// moved. Everything else is counted, named and folded into the column before
// it — dropping them silently would hide the passage of time, and showing
// them all made a grid that was seven-eighths empty.
func shownColumns(weeks []week) []int {
	var out []int
	for i := range weeks {
		if hasPage(weeks[i]) {
			out = append(out, i)
		}
	}
	return out
}

// quietAfter counts (and names) the columns between a shown column and the
// next one.
func quietAfter(weeks []week, shown []int, at int) (int, string) {
	next := len(weeks)
	for _, j := range shown {
		if j > at {
			next = j
			break
		}
	}
	var names []string
	for j := at + 1; j < next; j++ {
		names = append(names, weeks[j].Label)
	}
	if len(names) == 0 {
		return 0, ""
	}
	which := strings.Join(names, ", ")
	if len(names) > 6 {
		which = strings.Join(names[:6], ", ") + fmt.Sprintf(" … (%d)", len(names))
	}
	return len(names), which
}

// historyOf is one diagram's own timeline across the shown columns.
func historyOf(weeks []week, shown []int, id string, now int) ([]vmHistCell, int, string, string, string, string) {
	var cells []vmHistCell
	var prevHref, nextHref, prevLabel, nextLabel string
	for _, i := range shown {
		var tier string
		for _, c := range weeks[i].Changes {
			if c.ID != id {
				continue
			}
			tier = c.Status
			if c.Status == "changed" {
				tier = c.Report.Tier.String()
			}
			break
		}
		if tier == "" {
			continue
		}
		// Following a box opens THIS DIAGRAM's page — one diagram, every
		// version — not another column carrying two hundred others to show
		// one of them.
		anchor := "c-" + layoutaudit.Sanitize(weeks[i].Label)
		cell := vmHistCell{Label: weeks[i].Label, Tier: tier, Now: i == now, Anchor: anchor}
		// Following a box opens THIS DIAGRAM's page — one diagram, every
		// version — not another column carrying two hundred others to show
		// one of them.
		cell.Href = "../../" + diagramDir(id) + "/index.html#" + anchor
		cells = append(cells, cell)
		switch {
		case i < now:
			prevHref, prevLabel = cell.Href, cell.Label
		case i > now && nextHref == "":
			nextHref, nextLabel = cell.Href, cell.Label
		}
	}
	return cells, len(cells), prevHref, nextHref, prevLabel, nextLabel
}

// markSeen flags the cell whose rendering IS this row's "before" picture.
//
// The cell before the current one is the obvious candidate — a column the
// diagram did not move in rendered the same picture, so the previous box it
// DID move in should be where the left-hand image came from. Obvious is not
// the same as true. A "repaired" row has no before picture at all, and a row
// can be compared against a column this diagram was skipped in, so the
// predecessor's rendering is not always the one on screen.
//
// Panes are content-addressed, so this is checkable rather than assumable:
// mark the predecessor only when its "after" file IS this row's "before"
// file. When it is not, no box stands for what is being shown, and marking
// one anyway would name the wrong column with total confidence.
func markSeen(cells []vmHistCell, before string, afterOf func(label string) string) {
	if before == "" {
		return
	}
	for i := range cells {
		if !cells[i].Now {
			continue
		}
		if i > 0 && afterOf(cells[i-1].Label) == before {
			cells[i-1].Seen = true
		}
		return
	}
}

// diagramDir is a diagram's own page.
func diagramDir(id string) string { return "d/" + layoutaudit.Sanitize(id) }

// movedDiagrams lists every diagram that moved at least once, in the index's
// order, so each can be given a page.
func movedDiagrams(weeks []week) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range weeks {
		for _, c := range w.Changes {
			if !seen[c.ID] {
				seen[c.ID] = true
				out = append(out, c.ID)
			}
		}
	}
	sort.Strings(out)
	return out
}

// buildDiagram assembles one diagram's page: its versions, oldest first.
func buildDiagram(in timelineInput, id string) vmDiagram {
	d := vmDiagram{ID: id, IndexRef: "../../index.html", IPMT: in.IPMT[id]}
	if d.IPMT != "" {
		d.Lines = strings.Count(strings.TrimRight(d.IPMT, "\n"), "\n") + 1
	}
	for _, i := range shownColumns(in.Weeks) {
		w := in.Weeks[i]
		for _, c := range w.Changes {
			if c.ID != id {
				continue
			}
			tier := c.Status
			if c.Status == "changed" {
				tier = c.Report.Tier.String()
			}
			anchor := "c-" + layoutaudit.Sanitize(w.Label)
			ob, nb := c.Report.OldBounds, c.Report.NewBounds
			bounds := fmt.Sprintf("%d×%d", nb.Width, nb.Height)
			if ob != nb {
				bounds = fmt.Sprintf("%d×%d → %d×%d", ob.Width, ob.Height, nb.Width, nb.Height)
			}
			ow, nw := layoutaudit.PaneWidths(ob.Width, nb.Width)
			v := vmVersion{
				Label: w.Label, Anchor: anchor, Source: w.Source, Repo: w.Snap.Repo,
				Date: stampDate(w.Snap.Date), EnginePaths: w.Snap.EnginePaths,
				SHA: layoutaudit.Short(w.SHA), Subject: w.Subject,
				Tier: tier, Score: fmt.Sprintf("%.0f", c.Report.Score),
				Summary: layoutaudit.Summarize(c.Report), Bounds: bounds,
				ColumnHref:    "../../" + pageDir(w.Label) + "/index.html#" + anchorOf(id),
				BeforeSrc:     in.pane(w.Label, id, "before"),
				AfterSrc:      in.pane(w.Label, id, "after"),
				MarkedSrc:     in.pane(w.Label, id, "marked"),
				OldWidth:      ow,
				NewWidth:      nw,
				FindingsAdded: c.Report.FindingsAdded,
				Err:           c.Err,
			}
			for _, ch := range c.Report.Changes {
				v.Changes = append(v.Changes, vmChange{Kind: ch.Kind, Ref: ch.Ref,
					Label: ch.Label, Detail: ch.Detail, Tier: ch.Tier.String()})
			}
			if n := len(d.Versions); n > 0 {
				p := d.Versions[n-1]
				v.PrevLabel, v.PrevSHA = p.Label, p.SHA
				v.PrevSource, v.PrevRepo = p.Source, p.Repo
				v.PrevDate = p.Date
			}
			d.Versions = append(d.Versions, v)
			d.History = append(d.History, vmHistCell{
				Label: w.Label, Tier: tier, Href: "#" + anchor, Anchor: anchor,
				AfterPane: in.pane(w.Label, id, "after")})
			break
		}
	}
	d.Moves = len(d.Versions)
	for i := range d.Versions {
		d.Versions[i].AgentMD = agentMarkdown(d, d.Versions[i], in.Sources)
		d.Versions[i].RegressionMD = regressionMarkdown(d, d.Versions[i], in.Sources)
	}
	return d
}

func renderDiagram(in timelineInput, id string) string {
	var b strings.Builder
	if err := diagramTmpl.Execute(&b, buildDiagram(in, id)); err != nil {
		return errPage("diagram", err)
	}
	return b.String()
}

// buildIndex assembles the front page: the grid, then the columns in order.
func buildIndex(in timelineInput) vmIndex {
	m := vmIndex{
		Repo: in.Repo, Sources: in.Sources, Paths: strings.Join(in.Paths, " "),
		Diagrams: in.Diagrams, Elapsed: in.Elapsed.Round(time.Millisecond).String(), At: in.At,
		Corpus: in.Corpus,
	}

	shown := shownColumns(in.Weeks)
	cellsByID := map[string][]vmCell{}
	for wi, i := range shown {
		w := in.Weeks[i]
		m.WeekLabels = append(m.WeekLabels, w.Label)
		href := pageDir(w.Label) + "/index.html"
		for _, c := range w.Changes {
			row, ok := cellsByID[c.ID]
			if !ok {
				row = make([]vmCell, len(shown))
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
			// From the grid, a cell opens THAT diagram at THAT column — one
			// diagram's page, not a column page holding hundreds.
			cell := vmCell{Tier: tier, Title: w.Label + " · " + tier,
				Href: diagramDir(c.ID) + "/index.html#c-" + layoutaudit.Sanitize(w.Label)}
			row[wi] = cell
			m.TotalMoves++
		}

		n, which := quietAfter(in.Weeks, shown, i)
		m.QuietTotal += n
		m.Columns = append(m.Columns, vmColumn{
			Label: w.Label, Source: w.Source, SHA: layoutaudit.Short(w.SHA), Subject: w.Subject,
			Note: w.Note, Against: w.Against, Span: w.Span, Href: href,
			Changed: len(w.Changes), Identical: w.Identical, Skipped: w.Skipped,
			Worst: worstTier(w), QuietAfter: n, QuietWhich: which,
		})
	}
	// The columns nobody sees are still named, once, so a reader can check
	// that a silence really was a silence.
	for i := range in.Weeks {
		if !hasPage(in.Weeks[i]) {
			note := in.Weeks[i].Note
			if note == "" {
				note = "nothing moved"
			}
			m.QuietList = append(m.QuietList, in.Weeks[i].Label+" — "+note)
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
		own := len(c.OldSVG) + len(c.NewSVG) + len(c.NewMarked)
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
		row := buildRow(w, c, cur)
		row.BeforeSrc = in.pane(w.Label, c.ID, "before")
		row.AfterSrc = in.pane(w.Label, c.ID, "after")
		row.MarkedSrc = in.pane(w.Label, c.ID, "marked")
		row.CurrentSrc = in.pane(w.Label, c.ID, "current")
		row.History, row.Moves, row.HistPrev, row.HistNext, row.HistPrevLabel, row.HistNextLabel =
			historyOf(in.Weeks, shownColumns(in.Weeks), c.ID, i)
		markSeen(row.History, row.BeforeSrc, func(label string) string {
			return in.pane(label, c.ID, "after")
		})
		p.Rows = append(p.Rows, row)
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

// anchorOf is anchor, named for call sites that read better with it.
func anchorOf(id string) string { return anchor(id) }

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
.cell{display:block;width:16px;height:16px;border-radius:3px;background:var(--line)}
a.cell{text-decoration:none}
.cell.invariant{background:var(--worse)} .cell.structural{background:var(--changed)}
.cell.geometry{background:var(--moved)} .cell.broken{background:#000;border:2px solid var(--worse)}
.cell.repaired{background:var(--better)}
.sha{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;color:var(--muted);font-size:12px}
.note{padding:8px 0;color:var(--muted);font-size:13px}
/* One navigation, the same on every page: where am I, and what else can I
   look at from here. */
nav.top{display:flex;gap:10px;align-items:center;font-size:12.5px;flex-wrap:wrap;margin-top:8px}
nav.top a{color:var(--muted);text-decoration:none;border:1px solid var(--line);
  border-radius:6px;padding:2px 9px}
nav.top a:hover{border-color:var(--muted);color:var(--ink)}
nav.top .here{border-color:var(--ink);color:var(--ink);font-weight:600}
nav.top .sep{color:var(--line)}
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
/* The header is the way to a whole column; the cells lead to one diagram. */
table.grid th.wk a{color:var(--ink);text-decoration:none;border-bottom:1px solid var(--line)}
table.grid th.wk a:hover{color:var(--changed);border-bottom-color:var(--changed)}
table.grid td{padding:1px 2px}
table.grid td.name{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;white-space:nowrap;padding-right:10px}
/* An unchanged cell is an EMPTY <td>, drawn by CSS. On a long history the
   grid holds tens of thousands of them (224 diagrams × 89 columns here), and
   spelling each one out in markup cost more than everything else on the page
   put together. */
table.grid td:empty::before{content:"";display:block;width:16px;height:16px;
  border-radius:3px;background:var(--line);opacity:.35}
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
    <b>columns</b><span>{{len .WeekLabels}} that moved{{if .QuietTotal}} (+{{.QuietTotal}} with no change, folded in){{end}} · {{.At}} · {{.TotalMoves}} diagram-change(s) · {{.Elapsed}}</span>
  </div>
  {{range .Corpus}}<div class="note">⚠ {{.}}</div>{{end}}
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
  <tr><th>diagram</th>{{range .Columns}}<th class="wk">{{if .Href}}<a href="{{.Href}}" title="every diagram that moved in {{.Label}}, on one page">{{.Label}}</a>{{else}}{{.Label}}{{end}}{{if .QuietAfter}} +{{.QuietAfter}}{{end}}</th>{{end}}<th class="moves">moves</th></tr>
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
    <td class="quiet">{{if .Against}}vs {{.Against}}{{end}} {{if .Span}}· {{.Span}}{{end}}
      {{if .QuietAfter}}<br><span title="{{.QuietWhich}}">then {{.QuietAfter}} column(s) with no change</span>{{end}}</td>
  </tr>
  {{end}}
</table>
{{if .QuietList}}
<details><summary>{{len .QuietList}} column(s) in which nothing moved — not shown above</summary>
<div class="detail"><ul class="quiet">{{range .QuietList}}<li>{{.}}</li>{{end}}</ul></div></details>
{{end}}
</main>
</body></html>
`))

// reportFuncs are this file's own template helpers. copyJS and stripJS are
// shared between page kinds so two pages cannot drift into behaving
// differently while looking identical.
func reportFuncs() template.FuncMap {
	return template.FuncMap{
		"tier":    tierClass,
		"copyJS":  func() template.JS { return template.JS(copyJS) },
		"copyCSS": func() template.CSS { return template.CSS(copyCSS) },
		"stripJS": func() template.JS { return template.JS(stripJS) },
	}
}

var pageTmpl = template.Must(template.New("page").
	Funcs(reportFuncs()).
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
/* One diagram's own history: every column it moved in, and WHICH ONES THIS
   ROW IS SHOWING. A row shows two pictures — the column it is, and the one
   its "before" came from — so two boxes are marked, solid for the after and
   dashed for the before. Marking only one left the reader to work out where
   the left-hand picture came from; marking none, which is what a stale
   href condition actually did, left them to work out both. */
.hist{display:flex;align-items:center;gap:3px;flex-wrap:wrap;padding:8px 16px;
  border-top:1px solid var(--line);background:var(--bg)}
.hist .cell{width:14px;height:14px}
.hist .cell.now{outline:2px solid var(--ink);outline-offset:1px}
.hist .cell.seen{outline:2px dashed #8b939c;outline-offset:1px}
.histlabel{font-size:12px;color:var(--muted);margin-right:6px}
.steps{margin-left:auto;display:flex;gap:12px;font-size:12px}
.steps a{color:var(--muted)}
.steps .off{color:var(--line)}
{{copyCSS}}
</style></head><body>
<header>
  <h1>{{.Label}} {{if .Source}}<span class="pill">{{.Source}}</span>{{end}}
      <span class="sha">{{.SHA}}</span></h1>
  <div class="prov">
    <b>commit</b><span>{{.Subject}}</span>
    <b>compared</b><span>{{if .Against}}against {{.Against}} — {{end}}{{.Changed}} changed, {{.Identical}} identical{{if .Skipped}}, {{.Skipped}} skipped{{end}}</span>
    {{if .Span}}<b>spans</b><span>{{.Span}}</span>{{end}}
  </div>
  <nav class="top">
    <a href="../../index.html">← all diagrams &amp; columns</a>
    <span class="here">this column, all diagrams</span>
    <span class="sep">|</span>
    {{if .PrevHref}}<a href="{{.PrevHref}}">← {{.PrevLabel}}</a>{{end}}
    {{if .NextHref}}<a href="{{.NextHref}}">{{.NextLabel}} →</a>{{end}}
  </nav>
  {{if .Note}}<div class="note">{{.Note}}</div>{{end}}
</header>
<main>
{{range .Rows}}
<section class="row first{{if not .BeforeSrc}} no-before{{end}}" id="{{.Anchor}}">
  <div class="rowhead">
    <button type="button" class="copy anchor" data-anchor="{{.Anchor}}" title="copy a link straight to this diagram in this column">&#128279;</button>
    <span class="id">{{.ID}}</span>
    <span class="pill {{tier .Tier}}">{{.Tier}}</span>
    <span class="quiet">score {{.Score}} · {{.Bounds}}</span>
  </div>
  {{if .Summary}}<div class="summary">{{.Summary}}</div>{{end}}
  {{if .Err}}<div class="summary">{{.Err}}</div>{{end}}
  <div class="panes">
    <div class="pane pane-old">
      <h4><span>reference</span>{{if .CurrentSrc}}{{leftControls}}{{end}}</h4>
      <div class="stack">
        {{if .BeforeSrc}}<div class="layer layer-before" style="width:{{.OldWidth}}"><img src="{{.BeforeSrc}}" loading="lazy" alt="{{.ID}} before"><span class="chip">before</span></div>{{end}}
        {{if .CurrentSrc}}<div class="layer layer-current" style="width:{{.CurWidth}}"><img src="{{.CurrentSrc}}" loading="lazy" alt="{{.ID}} current"><span class="chip">current</span></div>{{end}}
      </div>
    </div>
    <div class="pane pane-new">
      <h4><span>this column</span>{{paneControls}}</h4>
      <div class="stack">
        {{if .BeforeSrc}}<div class="layer layer-before" style="width:{{.OldWidth}}"><img src="{{.BeforeSrc}}" loading="lazy" alt="{{.ID}} before"><span class="chip">before</span></div>{{end}}
        {{if .AfterSrc}}<div class="layer layer-after" style="width:{{.NewWidth}}"><img src="{{.AfterSrc}}" loading="lazy" alt="{{.ID}} after"><span class="chip">after</span></div>{{end}}
        {{if .MarkedSrc}}<div class="layer layer-marked" style="width:{{.NewWidth}}"><img src="{{.MarkedSrc}}" loading="lazy" alt="{{.ID}} differences marked"><span class="chip">marked</span></div>{{end}}
      </div>
    </div>
  </div>
  {{if .History}}
  <div class="hist">
    <span class="histlabel">this diagram moved {{.Moves}}×</span>
    {{range .History}}<a class="cell {{tier .Tier}}{{if .Now}} now{{end}}{{if .Seen}} seen{{end}}" href="{{.Href}}" title="{{.Label}} · {{.Tier}}{{if .Now}} · showing this one (after){{end}}{{if .Seen}} · showing this one (before){{end}}"></a>{{end}}
    <span class="steps">
      {{if .HistPrev}}<a href="{{.HistPrev}}" title="this diagram's previous change: {{.HistPrevLabel}}">◀ {{.HistPrevLabel}}</a>{{else}}<span class="off">◀ first</span>{{end}}
      {{if .HistNext}}<a href="{{.HistNext}}" title="this diagram's next change: {{.HistNextLabel}}">{{.HistNextLabel}} ▶</a>{{else}}<span class="off">last ▶</span>{{end}}
    </span>
  </div>
  {{end}}
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
{{copyJS}}
</script>
</body></html>
`))

// copyCSS styles every copy control. The source box's button is pinned to the
// corner of the box; the anchor icon sits inline in a row header — so the
// positioning belongs to the PLACE, not to the button, or one of the two ends
// up in the wrong corner of the other.
const copyCSS = `
button.copy{font:inherit;font-size:11px;line-height:1.6;padding:1px 9px;border-radius:5px;
  border:1px solid #d3d7dd;background:#fff;color:#3b4148;cursor:pointer}
button.copy:hover{border-color:#9aa3ad}
button.copy[data-state="ok"]{background:#3b4148;border-color:#3b4148;color:#fff}
button.copy[data-state="fail"]{border-color:var(--worse);color:var(--worse)}
.srcwrap button.copy{position:absolute;top:7px;right:8px}
button.copy.anchor{padding:0 6px;font-size:12px;border-color:transparent;background:none;opacity:.45}
.rowhead:hover button.copy.anchor{opacity:1}
button.copy.anchor:hover{border-color:#9aa3ad;background:#fff;opacity:1}
button.copy.anchor[data-state]{opacity:1;font-size:11px;background:#3b4148;border-color:#3b4148;color:#fff}
button.copy.hand{padding:0 8px;font-size:11px;opacity:.55}
.rowhead:hover button.copy.hand{opacity:1}
button.copy.hand:hover{border-color:#9aa3ad;opacity:1}
button.copy.hand.warn{color:var(--worse);border-color:#e3c9c9}
button.copy.hand.warn:hover{border-color:var(--worse)}
pre.payload{display:none}
/* Which comparison is on screen, in words as well as marks. */
.histnow{font-size:12px;color:var(--muted);margin-left:8px;font-variant-numeric:tabular-nums}
`

// copyJS backs every copy control on every page: the ipmt source box, and the
// anchor icon that hands back a link to one version.
//
// A report is opened from file://, where the async clipboard API is not
// available in every browser — a button that silently does nothing is worse
// than no button, so this falls back to the old selection trick and SAYS
// which happened either way.
const copyJS = `
(function () {
  function flash(btn, state, ok) {
    // Capture the label ONCE. A second click inside the 1400ms window would
    // otherwise capture the already-swapped text and restore "copied"
    // permanently, leaving the button wearing its own feedback.
    if (btn._label === undefined) { btn._label = btn.innerHTML; }
    if (btn._t) { clearTimeout(btn._t); }
    btn.dataset.state = state;
    btn.textContent = ok;
    btn._t = setTimeout(function () {
      btn.innerHTML = btn._label;
      delete btn.dataset.state;
      btn._t = null;
    }, 1400);
  }
  // A link to THIS page at THIS anchor. Built from the address bar so it is
  // correct under file:// and under a served copy alike, and with any
  // existing fragment dropped rather than appended to.
  function anchorURL(anchor) {
    return location.href.split("#")[0] + "#" + anchor;
  }
  function writeText(text, btn, el) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(
        function () { flash(btn, "ok", "copied"); },
        function () { selectFallback(text, btn, el); });
    } else {
      selectFallback(text, btn, el);
    }
  }
  function selectFallback(text, btn, el) {
    var node = el, temp = null;
    if (!node) {
      temp = document.createElement("span");
      temp.textContent = text;
      temp.style.position = "fixed";
      temp.style.left = "-9999px";
      temp.style.whiteSpace = "pre";
      document.body.appendChild(temp);
      node = temp;
    }
    var sel = window.getSelection(), range = document.createRange();
    range.selectNodeContents(node);
    sel.removeAllRanges(); sel.addRange(range);
    var ok = false;
    try { ok = document.execCommand("copy"); } catch (e) { ok = false; }
    if (temp) { document.body.removeChild(temp); sel.removeAllRanges(); }
    else if (ok) { sel.removeAllRanges(); }
    // On failure of the source box the text stays selected, so it can still
    // be copied by hand.
    flash(btn, ok ? "ok" : "fail", ok ? "copied" : "press \u2318/Ctrl+C");
  }
  document.querySelectorAll("button.copy").forEach(function (btn) {
    btn.addEventListener("click", function () {
      // ONE path, no early return. A button can carry both data-copy and
      // data-anchor — the agent payloads do — and an "if anchor, copy the
      // URL" shortcut in front silently won that case, so those buttons
      // copied a bare link and the substitution below was unreachable.
      // data-copy names WHAT to copy; data-anchor only says where the page's
      // own URL should be spliced in.
      var el = btn.dataset.copy ? document.getElementById(btn.dataset.copy) : null;
      var text = el ? el.textContent
        : (btn.dataset.anchor ? anchorURL(btn.dataset.anchor) : "");
      // Only the browser knows where this page was opened from, so the
      // payload carries a placeholder and it is filled in here.
      if (btn.dataset.anchor) {
        text = text.split("__URL__").join(anchorURL(btn.dataset.anchor));
      }
      if (!text) { return; }
      // A selection fallback can only select a node that is ON the page; a
      // hidden payload is not, so those copy through the synthetic path.
      writeText(text, btn, el && !el.hidden ? el : null);
    });
  });
})();
`

// stripJS moves the strip's two marks as a diagram page is scrolled.
//
// On a column page the marks are static — that page IS one column. A diagram
// page carries every version at once, so which comparison is on screen is a
// fact about the scroll position and only the browser knows it. Same two
// marks either way: solid on the version being shown, dashed on the one its
// "before" came from.
const stripJS = `
(function () {
  var strip = document.getElementById("strip");
  if (!strip) { return; }
  var cells = Array.prototype.slice.call(strip.querySelectorAll(".cell[data-col]"));
  var caption = document.getElementById("histnow");
  var sections = cells
    .map(function (c) { return document.getElementById(c.dataset.col); })
    .filter(Boolean);
  if (!cells.length || !sections.length) { return; }

  var onScreen = [];
  function paint() {
    // The topmost version still on screen is the one being read; below the
    // last one, the last one stays marked rather than nothing.
    var best = null, bestTop = Infinity;
    for (var i = 0; i < onScreen.length; i++) {
      var top = onScreen[i].getBoundingClientRect().top;
      if (top < bestTop) { bestTop = top; best = onScreen[i]; }
    }
    if (!best) { return; }
    var at = -1;
    for (var j = 0; j < cells.length; j++) {
      cells[j].classList.remove("now", "seen");
      if (cells[j].dataset.col === best.id) { at = j; }
    }
    if (at < 0) { return; }
    cells[at].classList.add("now");
    // Mark a predecessor only if its rendering IS the picture on screen.
    // Panes are content-addressed, so equal filename means equal bytes; an
    // unequal one means no box stands for this "before", and marking one
    // would name the wrong column with total confidence.
    var before = best.dataset.before || "";
    var prev = at > 0 ? cells[at - 1] : null;
    var paired = prev && before && prev.dataset.after === before;
    if (paired) { prev.classList.add("seen"); }
    if (caption) {
      caption.textContent = paired
        ? "showing " + prev.dataset.label + " \u2192 " + cells[at].dataset.label
        : "showing " + cells[at].dataset.label + (before ? "" : " (nothing before it)");
    }
  }

  if (!("IntersectionObserver" in window)) {
    // No observer: mark the first version rather than leaving the strip mute.
    onScreen = [sections[0]];
    paint();
    return;
  }
  var io = new IntersectionObserver(function (entries) {
    entries.forEach(function (e) {
      var at = onScreen.indexOf(e.target);
      if (e.isIntersecting && at < 0) { onScreen.push(e.target); }
      else if (!e.isIntersecting && at >= 0) { onScreen.splice(at, 1); }
    });
    paint();
  }, { rootMargin: "0px 0px -55% 0px" });
  sections.forEach(function (s) { io.observe(s); });
})();
`

var diagramTmpl = template.Must(template.New("diagram").
	Funcs(reportFuncs()).
	Funcs(layoutaudit.PaneFuncs()).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.ID}} — layout timeline</title>
<style>` + sharedCSS + `
{{paneCSS}}
.row{background:var(--card);border:1px solid var(--line);border-radius:10px;margin-bottom:18px;overflow:hidden}
.rowhead{padding:9px 16px;display:flex;gap:10px;align-items:baseline;flex-wrap:wrap;border-bottom:1px solid var(--line)}
.summary{padding:8px 16px;font-size:13px;color:var(--muted);border-bottom:1px solid var(--line)}
details{border-top:1px solid var(--line)}
summary{padding:7px 16px;font-size:13px;cursor:pointer;color:var(--muted)}
.detail{padding:0 16px 12px}
table.ch{border-collapse:collapse;width:100%;font-size:12.5px}
table.ch td,table.ch th{text-align:left;padding:2px 10px 2px 0;vertical-align:top}
.k{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;color:var(--changed)}
.k.invariant{color:var(--worse)} .k.geometry{color:var(--moved)}
.hist{display:flex;align-items:center;gap:3px;flex-wrap:wrap;padding:10px 0 0}
.hist .cell{width:14px;height:14px}
.hist .cell.now{outline:2px solid var(--ink);outline-offset:1px}
.hist .cell.seen{outline:2px dashed #8b939c;outline-offset:1px}
.histlabel{font-size:12px;color:var(--muted);margin-right:6px}
/* Every version offers the other half of the comparison: this diagram against
   the rest of ITS column. Buried in the changes fold it was a link nobody
   found, so it sits in the header the version is read from. */
.allref{margin-left:auto;font-size:12px;white-space:nowrap;color:var(--changed);text-decoration:none}
.allref:hover{text-decoration:underline}

/* ONE COLUMN, one version per row.
   A column page compares two engines, so it needs two panes side by side. A
   diagram page compares a diagram against ITSELF over time, and there the
   second pane was showing a picture the page already had: this version's
   "before" is the row above's "after". The reference is the previous ROW, so
   the comparison runs DOWN the page, and each row keeps the whole width for
   one picture — larger, and with the three states still swapping in place,
   which is the registration a blink comparison needs. */
.panes.one{grid-template-columns:1fr}

/* The input, at hand. Every picture on this page is this one text read by a
   different engine, so when two versions disagree the question is always
   "what does the source actually say" — and the answer should not require
   finding the file. Folded shut: it is reference, not the subject. */
.srcbox{margin-top:12px;border:1px solid var(--line);border-radius:8px;background:var(--card)}
.srcbox summary{padding:7px 12px;font-size:12px;color:var(--muted);cursor:pointer}
.srcwrap{position:relative;border-top:1px solid var(--line)}
.srcwrap pre{margin:0;padding:11px 12px;overflow-x:auto;font-size:12.5px;line-height:1.5;
  font-family:ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre;color:var(--ink)}
{{copyCSS}}
</style></head><body>
<header>
  <h1><span class="id">{{.ID}}</span></h1>
  <div class="prov">
    <b>versions</b><span>this diagram moved {{.Moves}}×; every one of them is on this page</span>
  </div>
  <nav class="top">
    <a href="{{.IndexRef}}">← all diagrams &amp; columns</a>
    <span class="here">this diagram, all versions</span>
  </nav>
  <div class="hist" id="strip">
    <span class="histlabel">this diagram moved {{.Moves}}×</span>
    {{range .History}}<a class="cell {{tier .Tier}}" href="{{.Href}}" data-col="{{.Anchor}}" data-label="{{.Label}}" data-after="{{.AfterPane}}" title="{{.Label}} · {{.Tier}}"></a>{{end}}
    <span class="histnow" id="histnow"></span>
  </div>
  {{if .IPMT}}<details class="srcbox">
    <summary>ipmt source — {{.Lines}} line(s), the one input every version below was rendered from</summary>
    <div class="srcwrap">
      <button type="button" class="copy" data-copy="ipmt-src">copy</button>
      <pre id="ipmt-src">{{.IPMT}}</pre>
    </div>
  </details>{{end}}
</header>
<main>
{{range .Versions}}
<section class="row first{{if not .BeforeSrc}} no-before{{end}}" id="{{.Anchor}}" data-before="{{.BeforeSrc}}">
  <div class="rowhead">
    <span class="id">{{.Label}}</span>
    {{if .Source}}<span class="pill">{{.Source}}</span>{{end}}
    <span class="pill {{tier .Tier}}">{{.Tier}}</span>
    <span class="quiet">score {{.Score}} · {{.Bounds}} · <span class="sha">{{.SHA}}</span> {{.Subject}}</span>
    <button type="button" class="copy anchor" data-anchor="{{.Anchor}}" title="copy a link straight to this version">&#128279;</button>
    <button type="button" class="copy hand" data-copy="md-{{.Anchor}}" data-anchor="{{.Anchor}}" title="copy a description of THIS version for an agent: the diagram, the engine commit, the source and what changed">for agent</button>
    <button type="button" class="copy hand warn" data-copy="rg-{{.Anchor}}" data-anchor="{{.Anchor}}" title="copy a regression report: the same, plus the version before it — the rendering that still looked right">regression</button>
    <a class="allref" href="{{.ColumnHref}}">all diagrams in {{.Label}} →</a>
  </div>
  <pre class="payload" id="md-{{.Anchor}}" hidden>{{.AgentMD}}</pre>
  <pre class="payload" id="rg-{{.Anchor}}" hidden>{{.RegressionMD}}</pre>
  {{if .Summary}}<div class="summary">{{.Summary}}</div>{{end}}
  {{if .Err}}<div class="summary">{{.Err}}</div>{{end}}
  <div class="panes one">
    <div class="pane pane-new">
      <h4><span>this version</span>{{paneControls}}</h4>
      <div class="stack">
        {{if .BeforeSrc}}<div class="layer layer-before" style="width:{{.OldWidth}}"><img src="{{.BeforeSrc}}" loading="lazy" alt="before"><span class="chip">before</span></div>{{end}}
        {{if .AfterSrc}}<div class="layer layer-after" style="width:{{.NewWidth}}"><img src="{{.AfterSrc}}" loading="lazy" alt="after"><span class="chip">after</span></div>{{end}}
        {{if .MarkedSrc}}<div class="layer layer-marked" style="width:{{.NewWidth}}"><img src="{{.MarkedSrc}}" loading="lazy" alt="marked"><span class="chip">marked</span></div>{{end}}
      </div>
    </div>
  </div>
  <details>
    <summary>{{len .Changes}} change(s) · this column with every other diagram</summary>
    <div class="detail">
      <table class="ch">
        {{range .Changes}}<tr><td class="k {{tier .Tier}}">{{.Kind}}</td><td>{{.Ref}} <span class="quiet">{{.Label}}</span></td><td>{{.Detail}}</td></tr>{{end}}
      </table>
      {{if .FindingsAdded}}<ul class="quiet">{{range .FindingsAdded}}<li>{{.}}</li>{{end}}</ul>{{end}}
    </div>
  </details>
</section>
{{end}}
</main>
<script>
{{paneJS}}

{{copyJS}}

{{stripJS}}
</script>
</body></html>
`))

// ---- payloads for an agent --------------------------------------------------

// The report is read by eye; what is found in it has to be handed to someone
// who cannot see it. Both payloads below are that handover: enough for an
// agent to identify the exact diagram, the exact engine commit, and the exact
// source, without the reader retyping any of it.
//
// urlMark is substituted by the browser, which is the only thing that knows
// where the page was opened from.
const urlMark = "__URL__"

// stampDate renders a commit date for a --since/--until flag, "" when unset.
func stampDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// fileOf is the path a diagram id points at: "docs/x.md#100" -> "docs/x.md".
func fileOf(id string) string {
	if i := strings.IndexByte(id, '#'); i >= 0 {
		return id[:i]
	}
	return id
}

// whichDiagram names the diagram the way it actually exists: a block inside a
// markdown file, or a whole .ipmt file. Saying "block X of X" for the second
// kind is noise a reader has to decode before trusting the rest.
func whichDiagram(id string) string {
	if i := strings.IndexByte(id, '#'); i >= 0 {
		return fmt.Sprintf("`%s` (block `%s` of `%s`)", id, id[i+1:], id[:i])
	}
	return fmt.Sprintf("`%s` (a whole .ipmt file)", id)
}

// fence wraps the source so it survives being pasted into a chat, using a
// longer fence when the source itself contains one.
func fence(body string) string {
	f := "```"
	for strings.Contains(body, f) {
		f += "`"
	}
	return f + "ipmt\n" + body + "\n" + f
}

// engineOf names an engine commit the way a human would have to: which
// lineage, which commit, what it said.
func engineOf(label, source, sha, subject string) string {
	s := label
	if source != "" {
		s += " (lineage `" + source + "`"
		if sha != "" {
			s += ", engine `" + sha + "`"
		}
		s += ")"
	} else if sha != "" {
		s += " (engine `" + sha + "`)"
	}
	if subject != "" {
		s += " — " + subject
	}
	return s
}

// maxChangeLines caps the list a payload carries. A busy column reports sixty
// changes for one diagram, and a paste that long buries the three lines that
// matter. What is dropped is COUNTED, never silently cut: the changes are
// already ordered worst-first, so the tail is the least of them.
const maxChangeLines = 30

// changeLines lists what the structural diff actually reported.
//
// FindingsAdded repeats what the "finding-added" changes already say, so it
// contributes only what is not already there — the same fact twice reads as
// two facts.
func changeLines(v vmVersion) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range v.Changes {
		line := "- `" + c.Kind + "` " + c.Ref
		if c.Label != "" {
			line += " " + c.Label
		}
		if c.Detail != "" {
			line += " — " + c.Detail
			seen[c.Detail] = true
		}
		out = append(out, line)
	}
	for _, f := range v.FindingsAdded {
		if !seen[f] {
			out = append(out, "- NEW FINDING: "+f)
		}
	}
	if n := len(out); n > maxChangeLines {
		out = append(out[:maxChangeLines:maxChangeLines],
			fmt.Sprintf("- …and %d more, least severe last — the page lists them all", n-maxChangeLines))
	}
	return out
}

// agentMarkdown is the OPENING of a prompt about one version: what is being
// looked at, rendered from what, by which engine — so a question can be asked
// about it without the reader retyping any of that.
func agentMarkdown(d vmDiagram, v vmVersion, sources string) string {
	var b strings.Builder
	b.WriteString("I am looking at one version of one diagram in the ipm layout-timeline\n")
	b.WriteString("report — the same diagram source rendered by the layout engine as it stood\n")
	b.WriteString("at different points in its history.\n\n")

	fmt.Fprintf(&b, "## The diagram\n\n")
	fmt.Fprintf(&b, "- id: %s\n", whichDiagram(d.ID))
	fmt.Fprintf(&b, "- rendered by: %s\n", engineOf(v.Label, v.Source, v.SHA, v.Subject))
	if v.PrevLabel != "" {
		fmt.Fprintf(&b, "- compared against: %s\n",
			engineOf(v.PrevLabel, v.PrevSource, v.PrevSHA, ""))
	} else {
		b.WriteString("- compared against: nothing on this page — this is its first version\n")
	}
	fmt.Fprintf(&b, "- the structural diff called this: %s", v.Tier)
	if v.Score != "" {
		fmt.Fprintf(&b, ", score %s", v.Score)
	}
	fmt.Fprintf(&b, "\n- canvas: %s\n", v.Bounds)
	if v.Repo != "" {
		fmt.Fprintf(&b, "- engine repository: `%s`\n", v.Repo)
	}
	if sources != "" {
		fmt.Fprintf(&b, "- diagrams came from: `%s`\n", sources)
	}
	fmt.Fprintf(&b, "- report page (all versions of it): %s\n", urlMark)
	if v.Err != "" {
		fmt.Fprintf(&b, "- error at this version: %s\n", v.Err)
	}
	writeSource(&b, d)
	writeChanges(&b, v, "## What the engine changed at this version")
	b.WriteString("\n## What I want\n\n")
	b.WriteString("<!-- replace this line with the question -->\n")
	return b.String()
}

// regressionMarkdown is the opening of a BUG REPORT: what looks wrong, the two
// renderings it is wrong between, and the commands to reproduce it and find
// the commit that introduced it.
//
// A regression is a statement about two renderings. The one that still looked
// right is exactly the half a reader can see and an agent cannot, so it is
// carried here — along with the means to narrow the gap between them to a
// single commit, which is the question a bug report actually has to answer.
func regressionMarkdown(d vmDiagram, v vmVersion, sources string) string {
	var b strings.Builder
	b.WriteString("Investigate a layout regression in the ipm layout engine.\n\n")
	fmt.Fprintf(&b, "I found this by eye in the layout-timeline report: the diagram below is\n")
	fmt.Fprintf(&b, "drawn WORSE at %s than it was at %s. The source did not change between\n",
		v.Label, orUnknown(v.PrevLabel))
	b.WriteString("them — only the layout engine did.\n\n")

	fmt.Fprintf(&b, "## The diagram\n\n")
	fmt.Fprintf(&b, "- id: %s\n", whichDiagram(d.ID))
	if v.Repo != "" {
		fmt.Fprintf(&b, "- engine repository: `%s`\n", v.Repo)
	}
	if sources != "" {
		fmt.Fprintf(&b, "- diagrams came from: `%s`\n", sources)
	}
	fmt.Fprintf(&b, "- report page (both renderings, flip between them): %s\n", urlMark)

	fmt.Fprintf(&b, "\n## The two renderings\n\n")
	b.WriteString("| | version | engine |\n|---|---|---|\n")
	if v.PrevLabel != "" {
		fmt.Fprintf(&b, "| looked right | %s | %s |\n", v.PrevLabel,
			lineageCell(v.PrevSource, v.PrevSHA, ""))
	} else {
		b.WriteString("| looked right | UNKNOWN — first version on the page | |\n")
	}
	fmt.Fprintf(&b, "| looks wrong | %s | %s |\n", v.Label,
		lineageCell(v.Source, v.SHA, v.Subject))
	fmt.Fprintf(&b, "\n- the structural diff called this: %s", v.Tier)
	if v.Score != "" {
		fmt.Fprintf(&b, ", score %s", v.Score)
	}
	fmt.Fprintf(&b, "\n- canvas: %s\n", v.Bounds)

	writeSource(&b, d)
	if len(changeLines(v)) > 0 {
		writeChanges(&b, v, "## What the structural diff reported here")
	} else {
		b.WriteString("\n## What the structural diff reported here\n\n")
		b.WriteString("Nothing — so whatever is wrong is either below the diff's tiers or in\n" +
			"the SVG rendering rather than in the layout.\n")
	}

	b.WriteString("\n## Reproduce\n\n")
	b.WriteString(reproduceCmd(d, v, sources))
	b.WriteString("\n## Find the commit that introduced it\n\n")
	b.WriteString(bisectCmd(d, v))
	b.WriteString("\n## What I want\n\n")
	b.WriteString("1. Reproduce both renderings and confirm the regression.\n")
	b.WriteString("2. Narrow it to the single engine commit that introduced it.\n")
	b.WriteString("3. Say whether it is a genuine regression or a deliberate trade-off,\n")
	b.WriteString("   and propose a fix if it is a regression.\n")
	return b.String()
}

func orUnknown(s string) string {
	if s == "" {
		return "an earlier version not shown on the page"
	}
	return s
}

// lineageCell names an engine commit inside a table cell.
func lineageCell(source, sha, subject string) string {
	var parts []string
	if source != "" {
		parts = append(parts, "`"+source+"`")
	}
	if sha != "" {
		parts = append(parts, "`"+sha+"`")
	}
	cell := strings.Join(parts, " ")
	if subject != "" {
		cell += " — " + subject
	}
	if cell == "" {
		return "unknown"
	}
	return cell
}

func writeSource(b *strings.Builder, d vmDiagram) {
	if d.IPMT == "" {
		return
	}
	fmt.Fprintf(b, "\n## The source both renderings came from\n\n%s\n", fence(d.IPMT))
}

func writeChanges(b *strings.Builder, v vmVersion, heading string) {
	lines := changeLines(v)
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s\n\n%s\n", heading, strings.Join(lines, "\n"))
}

// reproduceCmd is the audit that puts these two engines side by side — or an
// honest account of why one command cannot.
func reproduceCmd(d vmDiagram, v vmVersion, sources string) string {
	if v.PrevSHA == "" || v.SHA == "" {
		return "The two commits are not both known here, so there is no single command\n" +
			"to re-run this pair. The report page above shows both renderings.\n"
	}
	if v.PrevRepo != v.Repo {
		return fmt.Sprintf(
			"These two versions come from DIFFERENT repositories (`%s` and `%s`).\n"+
				"layout-audit compares two refs in ONE repository, so this pair cannot be\n"+
				"reproduced with a single command — the engine moved between checkouts here.\n"+
				"See `layout-history.json` for how the lineages are chained.\n",
			v.PrevRepo, v.Repo)
	}
	cmd := "go run ./cmd-dev/layout-audit"
	if v.Repo != "" {
		cmd += " --repo " + v.Repo
	}
	cmd += fmt.Sprintf(" --old %s --new %s", v.PrevSHA, v.SHA)
	if sources != "" && sources != v.Repo {
		cmd += " --sources " + sources
	}
	cmd += " " + fileOf(d.ID)
	return "```sh\n" + cmd + "\n```\n\n" +
		"That writes an HTML report comparing exactly these two engines over this file.\n"
}

// bisectCmd narrows "somewhere between these two" to one commit.
func bisectCmd(d vmDiagram, v vmVersion) string {
	if v.PrevSHA == "" || v.SHA == "" || v.PrevRepo != v.Repo {
		return "Not available for this pair: the two versions do not share a repository,\n" +
			"so there is no single commit range between them to walk.\n"
	}
	paths := strings.Join(v.EnginePaths, " ")
	if paths == "" {
		paths = "pkg/layout7 pkg/layout cmd/layout-gen"
	}
	var b strings.Builder
	b.WriteString("The candidates — every engine commit between the good and bad versions:\n\n")
	fmt.Fprintf(&b, "```sh\ngit -C %s log --oneline %s..%s -- %s\n```\n\n",
		v.Repo, v.PrevSHA, v.SHA, paths)
	b.WriteString("Then give each of those commits its own column, for this one diagram,\n")
	b.WriteString("so the exact commit where the picture changes is visible:\n\n")
	cmd := fmt.Sprintf("go run ./cmd-dev/layout-timeline --repo %s --by engine-commit", v.Repo)
	if v.PrevDate != "" {
		cmd += " --since " + v.PrevDate
	}
	if v.Date != "" {
		cmd += " --until " + v.Date
	}
	cmd += " " + fileOf(d.ID)
	fmt.Fprintf(&b, "```sh\n%s\n```\n", cmd)
	return b.String()
}
