package main

// --table / --edges: the node-position and edge-route tables. Moved
// verbatim from cmd/layout-gen: the dev views leave the shipping tool
// for cmd-dev/layout-debug (docs/dev/layout-gen/layout-debug.md).

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// renderTable formats the node positions as an aligned text table, sorted by
// y then x then id — the reading order of the rendered diagram. Multi-line
// (wrapped) labels are flattened with a literal \n so every node stays on one
// row. The trailing COMP column is the node's structural component index
// (union over leads-to, part-of and expresses-to-non-event — the engine's
// membership relations), so component separation is checkable from the table
// without rendering anything (analyze layouts with the
// dedicated text tools, not images).
func renderTable(graph *layout.Graph) string {
	comp := nodeComponents(graph)
	nodes := make([]layout.Node, len(graph.Nodes))
	copy(nodes, graph.Nodes)
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Y != nodes[j].Y {
			return nodes[i].Y < nodes[j].Y
		}
		if nodes[i].X != nodes[j].X {
			return nodes[i].X < nodes[j].X
		}
		return nodes[i].ID < nodes[j].ID
	})

	var sb strings.Builder
	tw := tabwriter.NewWriter(&sb, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tLABEL\tTYPE\tX\tY\tW\tH\tCOMP")
	for _, n := range nodes {
		label := strings.ReplaceAll(n.Label, "\n", "\\n")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%d\tc%d\n",
			n.ID, label, n.Type, n.X, n.Y, n.Width, n.Height, comp[n.ID])
	}
	tw.Flush()
	return sb.String()
}

// nodeComponents unions the graph's nodes over its STRUCTURAL edges with the
// same ANCHOR-AND-TIE rule Phase-1 discovery uses (v7P1): event↔event
// leads-to/part-of always union; a placing edge touching a thing/concept
// unions only while it anchors — once both endpoint groups contain an
// event(-like) node, a further placing edge is a cross-component tie and
// does not merge; near-to and expresses between events never join
// components. Edges are processed in graph order (declaration order,
// synthesized S/E edges appended), so a shared aux stays with its first
// user. Indices are assigned by first appearance in graph.Nodes order, so
// c0 is the first-placed component.
func nodeComponents(graph *layout.Graph) map[string]int {
	nodeType := make(map[string]string, len(graph.Nodes))
	parent := make(map[string]string, len(graph.Nodes))
	hasEvent := make(map[string]bool, len(graph.Nodes))
	eventLike := func(t string) bool { return t == "event" || t == "boundary" }
	for _, n := range graph.Nodes {
		nodeType[n.ID] = n.Type
		parent[n.ID] = n.ID
		if eventLike(n.Type) {
			hasEvent[n.ID] = true
		}
	}
	var find func(string) string
	find = func(id string) string {
		if parent[id] != id {
			parent[id] = find(parent[id])
		}
		return parent[id]
	}
	for _, e := range graph.Edges {
		if _, ok := parent[e.From]; !ok {
			continue
		}
		if _, ok := parent[e.To]; !ok {
			continue
		}
		structural := false
		switch e.Base {
		case "leadsto", "partof":
			structural = true
		case "expresses":
			structural = nodeType[e.To] != "event"
		}
		if !structural {
			continue
		}
		ra, rb := find(e.From), find(e.To)
		if ra == rb {
			continue
		}
		if !(eventLike(nodeType[e.From]) && eventLike(nodeType[e.To])) && hasEvent[ra] && hasEvent[rb] {
			continue // anchor-and-tie: both ends already in event structures
		}
		parent[rb] = ra
		hasEvent[ra] = hasEvent[ra] || hasEvent[rb]
	}
	index := make(map[string]int, len(graph.Nodes))
	next := 0
	rootIndex := make(map[string]int)
	for _, n := range graph.Nodes {
		root := find(n.ID)
		if _, ok := rootIndex[root]; !ok {
			rootIndex[root] = next
			next++
		}
		index[n.ID] = rootIndex[root]
	}
	return index
}

// renderEdgeTable formats every edge's computed route as an aligned table:
// from/to (resolved to labels), base relation, visibility class, the source and
// target port (side@position and the resolved x,y the renderer attaches at), and
// the bend points. Rows are sorted by source node, then source side, then source
// position — so a fan-out's port ORDER along each border is read top-to-bottom in
// the table, which is exactly what you need to spot crossings or a scrambled fan.
func renderEdgeTable(graph *layout.Graph) string {
	nodeByID := make(map[string]layout.Node, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nodeByID[n.ID] = n
	}
	label := func(id string) string {
		if n, ok := nodeByID[id]; ok && strings.TrimSpace(n.Label) != "" {
			return strings.ReplaceAll(n.Label, "\n", " ")
		}
		return id
	}
	routes := layout.RoutesOf(graph)

	// Polyline segments per edge (port → bends → port), for the CROSS (distinct
	// visible edges this edge's line crosses) and LEN (path/chord ratio — how
	// far the route detours past a straight line) debug columns.
	segsOf := make([][][4]int, len(graph.Edges))
	for i := range graph.Edges {
		if i >= len(routes) {
			continue
		}
		fn, fok := nodeByID[graph.Edges[i].From]
		tn, tok := nodeByID[graph.Edges[i].To]
		if !fok || !tok {
			continue
		}
		sx, sy := layout.EdgePortPoint(fn, tn, routes[i].Source)
		tx, ty := layout.EdgePortPoint(tn, fn, routes[i].Target)
		pts := []layout.Position{{X: sx, Y: sy}}
		pts = append(pts, routes[i].Bends...)
		pts = append(pts, layout.Position{X: tx, Y: ty})
		for s := 0; s+1 < len(pts); s++ {
			segsOf[i] = append(segsOf[i], [4]int{pts[s].X, pts[s].Y, pts[s+1].X, pts[s+1].Y})
		}
	}
	crossCount := func(i int) int {
		seen := map[int]bool{}
		for _, a := range segsOf[i] {
			for j := range graph.Edges {
				if j == i || graph.Edges[j].Visibility == "stubbed" {
					continue
				}
				for _, b := range segsOf[j] {
					if segCross(a, b) {
						seen[j] = true
					}
				}
			}
		}
		return len(seen)
	}
	lenRatio := func(i int) float64 {
		segs := segsOf[i]
		if len(segs) == 0 {
			return 0
		}
		path := 0.0
		for _, s := range segs {
			path += math.Hypot(float64(s[2]-s[0]), float64(s[3]-s[1]))
		}
		chord := math.Hypot(float64(segs[len(segs)-1][2]-segs[0][0]), float64(segs[len(segs)-1][3]-segs[0][1]))
		if chord == 0 {
			return 0
		}
		return path / chord
	}

	type row struct {
		from, to, base, vis string
		srcSide, tgtSide    string
		srcPos, tgtPos      float64
		sx, sy, tx, ty      int
		bends               string
		cross               int
		lenR                float64
	}
	rows := make([]row, 0, len(graph.Edges))
	for i, e := range graph.Edges {
		if i >= len(routes) {
			break
		}
		r := routes[i]
		var sx, sy, tx, ty int
		fn, fok := nodeByID[e.From]
		tn, tok := nodeByID[e.To]
		if fok && tok {
			sx, sy = layout.EdgePortPoint(fn, tn, r.Source)
			tx, ty = layout.EdgePortPoint(tn, fn, r.Target)
		}
		vis := e.Visibility
		if vis == "" {
			vis = "visible"
		}
		bends := "-"
		if len(r.Bends) > 0 {
			parts := make([]string, len(r.Bends))
			for k, b := range r.Bends {
				parts[k] = fmt.Sprintf("%d,%d", b.X, b.Y)
			}
			bends = "[" + strings.Join(parts, " ") + "]"
		}
		rows = append(rows, row{
			from: label(e.From), to: label(e.To), base: e.Base, vis: vis,
			srcSide: r.Source.Side, tgtSide: r.Target.Side,
			srcPos: r.Source.Position, tgtPos: r.Target.Position,
			sx: sx, sy: sy, tx: tx, ty: ty, bends: bends,
			cross: crossCount(i), lenR: lenRatio(i),
		})
	}
	sideRank := map[string]int{"top": 0, "right": 1, "bottom": 2, "left": 3, "center": 4}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].from != rows[j].from {
			return rows[i].from < rows[j].from
		}
		if rows[i].srcSide != rows[j].srcSide {
			return sideRank[rows[i].srcSide] < sideRank[rows[j].srcSide]
		}
		if rows[i].srcPos != rows[j].srcPos {
			return rows[i].srcPos < rows[j].srcPos
		}
		return rows[i].to < rows[j].to
	})

	var sb strings.Builder
	tw := tabwriter.NewWriter(&sb, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "FROM\tTO\tBASE\tVIS\tSRC side@pos→x,y\tTGT side@pos→x,y\tCROSS\tLEN\tBENDS")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s@%.2f→%d,%d\t%s@%.2f→%d,%d\t%d\t%.2f\t%s\n",
			r.from, r.to, r.base, r.vis,
			r.srcSide, r.srcPos, r.sx, r.sy,
			r.tgtSide, r.tgtPos, r.tx, r.ty, r.cross, r.lenR, r.bends)
	}
	tw.Flush()
	return sb.String()
}

// segCross reports whether segments a=[x1,y1,x2,y2] and b=[...] properly cross
// (endpoints touching does not count) — the orientation test, for the --edges
// CROSS column.
func segCross(a, b [4]int) bool {
	o := func(px, py, qx, qy, rx, ry int) int {
		v := (qy-py)*(rx-qx) - (qx-px)*(ry-qy)
		switch {
		case v > 0:
			return 1
		case v < 0:
			return -1
		default:
			return 0
		}
	}
	d1 := o(b[0], b[1], b[2], b[3], a[0], a[1])
	d2 := o(b[0], b[1], b[2], b[3], a[2], a[3])
	d3 := o(a[0], a[1], a[2], a[3], b[0], b[1])
	d4 := o(a[0], a[1], a[2], a[3], b[2], b[3])
	return ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0))
}
