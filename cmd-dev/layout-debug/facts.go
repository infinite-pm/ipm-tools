package main

// --facts: the layout as OBSERVED rule-DSL facts (docs/dev/layout-gen/layout-debug.md,
// "AI as a first-class consumer"). One fact per line, deterministic
// order, the same vocabulary the fixture pins use — drafting a fixture
// becomes paste-and-curate, and canon-vs-actual is a plain diff.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

func splitSel(sel string) []string {
	if sel == "" {
		return nil
	}
	var out []string
	for _, s := range strings.Split(sel, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func renderFacts(graph *layout.Graph, sel []string) string {
	lt := layouttest.FromLayoutGraph(graph)
	selected := map[string]bool{}
	for _, s := range sel {
		selected[s] = true
	}
	pick := func(names ...string) bool {
		if len(selected) == 0 {
			return true
		}
		for _, n := range names {
			if selected[n] {
				return true
			}
		}
		return false
	}
	// names with spaces cannot be addressed by #name selectors; skip
	// their positional facts rather than emit unusable pins
	addressable := func(n layouttest.Node) bool { return !strings.Contains(n.ID, " ") }

	nodes := make([]layouttest.Node, 0, len(lt.Nodes))
	for _, n := range lt.Nodes {
		if n.Type != "boundary" {
			nodes = append(nodes, n)
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Y != nodes[j].Y {
			return nodes[i].Y < nodes[j].Y
		}
		return nodes[i].X < nodes[j].X
	})

	var b strings.Builder

	// same-y groups (rows), reading order
	byY := map[int][]layouttest.Node{}
	var ys []int
	for _, n := range nodes {
		if len(byY[n.Y]) == 0 {
			ys = append(ys, n.Y)
		}
		byY[n.Y] = append(byY[n.Y], n)
	}
	sort.Ints(ys)
	for _, y := range ys {
		row := byY[y]
		if len(row) < 2 {
			continue
		}
		var names []string
		ok := true
		for _, n := range row {
			if !addressable(n) {
				ok = false
			}
			names = append(names, "#"+n.ID)
		}
		if ok && pick(idsOf(row)...) {
			fmt.Fprintf(&b, "all %s have same y\n", strings.Join(names, ","))
		}
		// adjacent left-of gaps within the row
		for i := 0; i+1 < len(row); i++ {
			a, c := row[i], row[i+1]
			if !addressable(a) || !addressable(c) || !pick(a.ID, c.ID) {
				continue
			}
			gap := c.X - (a.X + a.Width)
			fmt.Fprintf(&b, "#%s is left-of #%s with gap=%d\n", a.ID, c.ID, gap)
		}
	}

	// same-center-x groups (columns) + adjacent vertical gaps
	byCx := map[int][]layouttest.Node{}
	var cxs []int
	for _, n := range nodes {
		cx := n.X + n.Width/2
		if len(byCx[cx]) == 0 {
			cxs = append(cxs, cx)
		}
		byCx[cx] = append(byCx[cx], n)
	}
	sort.Ints(cxs)
	for _, cx := range cxs {
		col := byCx[cx]
		if len(col) < 2 {
			continue
		}
		sort.SliceStable(col, func(i, j int) bool { return col[i].Y < col[j].Y })
		var names []string
		ok := true
		for _, n := range col {
			if !addressable(n) {
				ok = false
			}
			names = append(names, "#"+n.ID)
		}
		if ok && pick(idsOf(col)...) {
			fmt.Fprintf(&b, "all %s have same center-x\n", strings.Join(names, ","))
		}
		for i := 0; i+1 < len(col); i++ {
			a, c := col[i], col[i+1]
			if !addressable(a) || !addressable(c) || !pick(a.ID, c.ID) {
				continue
			}
			if a.Y+a.Height <= c.Y {
				fmt.Fprintf(&b, "#%s is below #%s with gap=%d\n", c.ID, a.ID, c.Y-(a.Y+a.Height))
			}
		}
	}

	// edges: ports, bends, visibility
	for _, e := range lt.Edges {
		if strings.Contains(e.From, " ") || strings.Contains(e.To, " ") || !pick(e.From, e.To) {
			continue
		}
		ref := fmt.Sprintf("edge #%s,#%s", e.From, e.To)
		bends := 0
		if len(e.Points) > 2 {
			bends = len(e.Points) - 2
		}
		fmt.Fprintf(&b, "%s has max-bends=%d\n", ref, bends)
		if e.Visibility == "stubbed" {
			fmt.Fprintf(&b, "%s has visibility=stubbed\n", ref)
			continue
		}
		fmt.Fprintf(&b, "%s has source-side=%s\n", ref, e.SourceSide)
		fmt.Fprintf(&b, "%s has source-position=%.2f\n", ref, e.SourcePosition)
		fmt.Fprintf(&b, "%s has target-side=%s\n", ref, e.TargetSide)
		fmt.Fprintf(&b, "%s has target-position=%.2f\n", ref, e.TargetPosition)
	}

	// crossings between visible edges (each pair once)
	for i, a := range lt.Edges {
		if a.Visibility == "stubbed" {
			continue
		}
		for j := i + 1; j < len(lt.Edges); j++ {
			c := lt.Edges[j]
			if c.Visibility == "stubbed" || !edgesCross(a, c) {
				continue
			}
			if strings.ContainsAny(a.From+a.To+c.From+c.To, " ") ||
				!pick(a.From, a.To, c.From, c.To) {
				continue
			}
			fmt.Fprintf(&b, "edge #%s,#%s crosses edge #%s,#%s\n", a.From, a.To, c.From, c.To)
		}
	}
	return b.String()
}

func idsOf(ns []layouttest.Node) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.ID)
	}
	return out
}

func edgesCross(a, b layouttest.Edge) bool {
	for i := 0; i+1 < len(a.Points); i++ {
		for j := 0; j+1 < len(b.Points); j++ {
			if segsCrossFacts(
				a.Points[i].X, a.Points[i].Y, a.Points[i+1].X, a.Points[i+1].Y,
				b.Points[j].X, b.Points[j].Y, b.Points[j+1].X, b.Points[j+1].Y) {
				return true
			}
		}
	}
	return false
}

func segsCrossFacts(ax0, ay0, ax1, ay1, bx0, by0, bx1, by1 int) bool {
	d := func(px, py, qx, qy, rx, ry int) int {
		return (qx-px)*(ry-py) - (qy-py)*(rx-px)
	}
	d1 := d(bx0, by0, bx1, by1, ax0, ay0)
	d2 := d(bx0, by0, bx1, by1, ax1, ay1)
	d3 := d(ax0, ay0, ax1, ay1, bx0, by0)
	d4 := d(ax0, ay0, ax1, ay1, bx1, by1)
	return ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) &&
		((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0))
}
