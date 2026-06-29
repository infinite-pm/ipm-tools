// Package l7report narrates layout7's decision trace
// (docs/dev/layout-gen/layout-debug.md):
// the engine emits structured TraceEvents through its one seam
// (layout7.Trace); this package collects them and renders the
// human-readable views — Text for the terminal (`layout-debug --why`)
// and Explain for the narrated pipeline-ordered report
// (`layout-explain`). The engine itself contains no narration.
package l7report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layout7"
)

// Report is a collected trace plus the generated graph.
type Report struct {
	Events []layout7.TraceEvent
	Graph  *layout.Graph
}

// Emit implements layout7.Trace.
func (r *Report) Emit(e layout7.TraceEvent) { r.Events = append(r.Events, e) }

// Run generates the layout with tracing and returns the report.
func Run(doc *model.IpmGraph) (*Report, error) {
	if !layout7.TraceAvailable {
		return nil, fmt.Errorf("layout7 was built with -tags l7notrace: trace events are compiled out, no report is possible")
	}
	r := &Report{}
	g, err := layout7.GenerateTraced(doc, r)
	if err != nil {
		return nil, err
	}
	r.Graph = g
	return r, nil
}

func (r *Report) byKind(stage, kind string) []layout7.TraceEvent {
	var out []layout7.TraceEvent
	for _, e := range r.Events {
		if e.Stage == stage && e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func str(d map[string]any, k string) string {
	if v, ok := d[k].(string); ok {
		return v
	}
	return ""
}

func num(d map[string]any, k string) float64 {
	switch v := d[k].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

// mentions reports whether the event involves any of the selected names
// (empty selection = everything).
func mentions(e layout7.TraceEvent, sel map[string]bool) bool {
	if len(sel) == 0 {
		return true
	}
	for _, k := range []string{"node", "partner", "from", "to", "parent"} {
		if sel[str(e.Data, k)] {
			return true
		}
	}
	if rows, ok := e.Data["rows"].([][]string); ok {
		for _, row := range rows {
			for _, n := range row {
				if sel[n] {
					return true
				}
			}
		}
	}
	return false
}

// TextOpts filter the terminal view. Sel limits every section to events
// mentioning the named nodes (context budget: a 20-line filtered answer
// beats a 2000-line dump — docs/dev/layout-gen/layout-debug.md, output contract).
type TextOpts struct {
	Sel        []string
	Candidates bool // include the per-candidate route story
	Verbose    bool // append the FULL raw trace (every event, one line each)
}

// Text renders the decisions the way `layout-gen --why` prints them.
func (r *Report) Text(opts TextOpts) string {
	sel := map[string]bool{}
	for _, s := range opts.Sel {
		if s != "" {
			sel[s] = true
		}
	}
	var b strings.Builder

	fmt.Fprintf(&b, "== components (v7P1)\n")
	for _, e := range r.byKind("membership", "component") {
		evs, _ := e.Data["events"].([]string)
		aux, _ := e.Data["aux"].([]string)
		if len(sel) > 0 {
			hit := false
			for _, n := range append(append([]string{}, evs...), aux...) {
				if sel[n] {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		fmt.Fprintf(&b, "  comp %d: events [%s] aux [%s] cross-ties %d\n",
			int(num(e.Data, "index")), strings.Join(evs, " "), strings.Join(aux, " "),
			int(num(e.Data, "crossTies")))
	}

	fmt.Fprintf(&b, "== anchors and demotions (v7P7)\n")
	for _, e := range r.Events {
		if e.Stage != "membership" || !mentions(e, sel) {
			continue
		}
		switch e.Kind {
		case "satellite":
			fmt.Fprintf(&b, "  %s (%s): SATELLITE of %s (tie-only, v7P5)\n",
				str(e.Data, "node"), str(e.Data, "nodeKind"), str(e.Data, "partner"))
		case "anchor":
			fmt.Fprintf(&b, "  %s (%s): anchored by %s -> %s\n",
				str(e.Data, "node"), str(e.Data, "nodeKind"), str(e.Data, "from"), str(e.Data, "to"))
		case "unanchored":
			fmt.Fprintf(&b, "  %s (%s): unanchored (oriented by its parts/whole)\n",
				str(e.Data, "node"), str(e.Data, "nodeKind"))
		case "demote":
			fmt.Fprintf(&b, "  demoted tie: %s -> %s (%s) — lost the anchor election, draws or stubs\n",
				str(e.Data, "from"), str(e.Data, "to"), str(e.Data, "rel"))
		}
	}

	fmt.Fprintf(&b, "== bands (v7P4/P5 aux placement)\n")
	for _, e := range r.byKind("groups", "band") {
		if !mentions(e, sel) {
			continue
		}
		fmt.Fprintf(&b, "  %s (%s): %s of %s (offset %+d,%+d)\n",
			str(e.Data, "node"), str(e.Data, "nodeKind"), str(e.Data, "side"),
			str(e.Data, "anchor"), int(num(e.Data, "dx")), int(num(e.Data, "dy")))
	}

	fmt.Fprintf(&b, "== sub-structures (v7P3 rank rows)\n")
	for _, e := range r.byKind("skeleton", "subrows") {
		if !mentions(e, sel) {
			continue
		}
		fmt.Fprintf(&b, "  %s:\n", str(e.Data, "parent"))
		if rows, ok := e.Data["rows"].([][]string); ok {
			for ri, row := range rows {
				fmt.Fprintf(&b, "    row %d: %s\n", ri, strings.Join(row, " | "))
			}
		}
	}

	fmt.Fprintf(&b, "== routes (v7P9)\n")
	for _, e := range r.Events {
		if e.Stage != "route" || (e.Kind != "chosen" && e.Kind != "stubbed") || !mentions(e, sel) {
			continue
		}
		vis := "drawn"
		if e.Kind == "stubbed" {
			vis = "STUBBED (no candidate within the crossing budget)"
		}
		shape := "straight"
		switch int(num(e.Data, "bends")) {
		case 0:
		case 1:
			shape = "dogleg"
		default:
			shape = "lane"
		}
		fmt.Fprintf(&b, "  %s -> %s (%s): %s@%.2f -> %s@%.2f, %s — %s\n",
			str(e.Data, "from"), str(e.Data, "to"), str(e.Data, "rel"),
			str(e.Data, "srcSide"), num(e.Data, "srcPos"),
			str(e.Data, "tgtSide"), num(e.Data, "tgtPos"), shape, vis)
	}

	if opts.Candidates {
		fmt.Fprintf(&b, "== route candidates (v7P9 budget arithmetic)\n")
		b.WriteString(r.candidateText(sel))
	}
	if opts.Verbose {
		fmt.Fprintf(&b, "== full trace (every event)\n")
		b.WriteString(r.rawTrace(sel))
	}
	return b.String()
}

// rawTrace prints every trace event on one line — the MOST VERBOSE
// view; stable key order, greppable and diffable.
func (r *Report) rawTrace(sel map[string]bool) string {
	var b strings.Builder
	for _, e := range r.Events {
		if !mentions(e, sel) {
			continue
		}
		keys := make([]string, 0, len(e.Data))
		for k := range e.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(&b, "  %s/%s:", e.Stage, e.Kind)
		for _, k := range keys {
			fmt.Fprintf(&b, " %s=%v", k, e.Data[k])
		}
		b.WriteString("\n")
	}
	return b.String()
}

// geometryTable lists every node's final box — the coordinates a human
// otherwise reads off the SVG one hover at a time. nm formats each node
// name and kindFmt each kind word (Explain injects the as-token
// colouring; plain formatters just backtick).
func (r *Report) geometryTable(nm, kindFmt func(string) string) string {
	if r.Graph == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("| node | kind | x,y | w×h |\n|---|---|---|---|\n")
	for _, n := range r.Graph.Nodes {
		name := n.Alias
		if name == "" {
			name = n.Label
		}
		fmt.Fprintf(&b, "| %s | %s | %d,%d | %d×%d |\n",
			nm(name), kindFmt(n.Type), n.X, n.Y, n.Width, n.Height)
	}
	return b.String()
}

func (r *Report) candidateText(sel map[string]bool) string {
	var b strings.Builder
	for _, e := range r.byKind("route", "candidate") {
		if !mentions(e, sel) {
			continue
		}
		mark := " "
		if v, ok := e.Data["chosen"].(bool); ok && v {
			mark = "*"
		}
		hit := ""
		if v, ok := e.Data["hit"].(bool); ok && v {
			hit = " HITS-BOX"
		}
		fmt.Fprintf(&b, " %s %s -> %s [%s cand %d] %s->%s bends %d: cross %.2f graze %.2f detour %.2f%s\n",
			mark, str(e.Data, "from"), str(e.Data, "to"), str(e.Data, "pass"),
			int(num(e.Data, "cand")), str(e.Data, "srcSide"), str(e.Data, "tgtSide"),
			int(num(e.Data, "bends")), num(e.Data, "cross"), num(e.Data, "graze"),
			num(e.Data, "detour"), hit)
	}
	return b.String()
}

// The narrated per-block report is now l7report.Explain (explain.go) —
// the pipeline-ordered narrator that replaced the old flat Markdown
// view. Explain reuses the geometryTable /
// trajectoryTable / candidateText / rawTrace helpers below.

// trajectoryTable lists nodes whose position changed across the
// snapshot stages (floors -> pull -> place -> assemble), RELATIVE to
// the common place->assemble shift (assemble translates whole
// components; only relative movement is interesting). Stages absent
// from the trace (or where a node is not yet placed) show "-".
func (r *Report) trajectoryTable(nm func(string) string) string {
	type pos struct{ x, y int }
	order := []string{"floors", "pull", "place", "assemble"}
	snaps := map[string]map[string]pos{}
	var stages []string
	for _, st := range order {
		m := map[string]pos{}
		for _, e := range r.byKind(st, "positions") {
			m[str(e.Data, "node")] = pos{int(num(e.Data, "x")), int(num(e.Data, "y"))}
		}
		if len(m) > 0 {
			snaps[st] = m
			stages = append(stages, st)
		}
	}
	if len(stages) < 2 {
		return ""
	}
	place, asm := snaps["place"], snaps["assemble"]
	// the dominant delta = the component shift; report deviations from it
	var common [2]int
	if len(place) > 0 && len(asm) > 0 {
		deltas := map[[2]int]int{}
		for n, p := range place {
			if a, ok := asm[n]; ok {
				deltas[[2]int{a.x - p.x, a.y - p.y}]++
			}
		}
		best := -1
		for d, c := range deltas {
			if c > best {
				best, common = c, d
			}
		}
	}
	names := map[string]bool{}
	for _, m := range snaps {
		for n := range m {
			names[n] = true
		}
	}
	var sorted []string
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	var rows []string
	for _, n := range sorted {
		moved := false
		var prev *pos
		cells := make([]string, 0, len(stages))
		for _, st := range stages {
			p, ok := snaps[st][n]
			if !ok {
				cells = append(cells, "-")
				continue
			}
			shown := p
			if st == "assemble" {
				shown = pos{p.x - common[0], p.y - common[1]}
			}
			cells = append(cells, fmt.Sprintf("%d,%d", shown.x, shown.y))
			if prev != nil && (shown.x != prev.x || shown.y != prev.y) {
				moved = true
			}
			cp := shown
			prev = &cp
		}
		if moved {
			rows = append(rows, "| "+nm(n)+" | "+strings.Join(cells, " | ")+" |")
		}
	}
	if len(rows) == 0 {
		return ""
	}
	head := "| node | " + strings.Join(stages, " | ") + " |\n|---|" + strings.Repeat("---|", len(stages)) + "\n"
	return head + strings.Join(rows, "\n") +
		"\n\n(assemble column shown minus the component's common shift)\n"
}
