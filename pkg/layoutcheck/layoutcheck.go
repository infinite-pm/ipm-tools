// Package layoutcheck holds the UNIVERSAL visual invariants a layout must
// satisfy no matter its shape — node boxes must not overlap, a routed edge
// must not cut through or graze a box it is not connected to, edges must
// not cross/cover, stub badges and chips must keep clear of boxes, and
// unrelated boxes must not sit at band rhythm so they read as paired. The
// fitness corpora assert precise per-shape geometry; these checks catch
// the combinations no fixture anticipates.
//
// It is a DEBUG VIEW (what is visually wrong with THIS diagram) that
// doubles as a regression harness when run wide against a baseline — one
// code path, one vocabulary. cmd-dev/layout-debug exposes both:
//
//	layout-debug --in c.ipmt --check                    # one diagram, findings
//	layout-debug --check --baseline <f> <paths…>        # the ratchet
//
// Everything here is a pure function over layout.Graph or a baseline
// blob — the CLI owns file IO and the engine run.
package layoutcheck

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// Options tune the checks.
type Options struct {
	// LegacyAllEdges counts stubbed (hidden) edges like full edges, as
	// before the visibility-aware metrics — kept for A/B measurement.
	LegacyAllEdges bool
}

// Findings runs every universal invariant over g and returns one string
// per violation, in a deterministic order. An empty slice means the
// diagram is clean. The strings are the grep/diff vocabulary — their
// prefixes drive Categorize, so a new finding phrasing must stay
// consistent with it.
func Findings(g *layout.Graph, opts Options) []string {
	var findings []string

	// Visibility-aware edge metrics: edges the canvas stubs (hidden behind
	// "?" badges — cross-shell or long Expresses/NearTo glue) do not count as
	// drawn lines; what the user actually sees of them is two small badges,
	// which get their own least-severe check below.
	// Stub set is the authoritative Edge.Visibility stamped by Generate
	// (decided on real routed geometry), so the check measures exactly what
	// the renderer draws — a re-classification could disagree.
	stubbed := make([]bool, len(g.Edges))
	if !opts.LegacyAllEdges {
		for i := range g.Edges {
			stubbed[i] = g.Edges[i].Visibility == "stubbed"
		}
	}

	// Node-node overlaps.
	for i := 0; i < len(g.Nodes); i++ {
		for j := i + 1; j < len(g.Nodes); j++ {
			a, b := g.Nodes[i], g.Nodes[j]
			if a.X < b.X+b.Width && b.X < a.X+a.Width &&
				a.Y < b.Y+b.Height && b.Y < a.Y+a.Height {
				findings = append(findings, fmt.Sprintf("node overlap: %q (%d,%d %dx%d) × %q (%d,%d %dx%d)",
					clip(a.Label), a.X, a.Y, a.Width, a.Height, clip(b.Label), b.X, b.Y, b.Width, b.Height))
			}
		}
	}

	// Edge-node crossings (routed straight segments vs non-incident boxes,
	// boxes shrunk 2px so grazing a border does not count).
	// FALSE PAIRING (the stranded leaf parked beside a
	// foreign stack): two UNRELATED boxes — no edge between them, no
	// shared edge partner — sitting side-adjacent at band rhythm
	// (aligned within one stack gap vertically or one column gap
	// horizontally) read as members of one group.
	{
		partners := map[string]map[string]bool{}
		link := func(a, b string) {
			if partners[a] == nil {
				partners[a] = map[string]bool{}
			}
			partners[a][b] = true
		}
		for _, e := range g.Edges {
			link(e.From, e.To)
			link(e.To, e.From)
		}
		shared := func(a, b string) bool {
			for p := range partners[a] {
				if partners[b][p] {
					return true
				}
			}
			return false
		}
		for i := 0; i < len(g.Nodes); i++ {
			for j := i + 1; j < len(g.Nodes); j++ {
				a, b := g.Nodes[i], g.Nodes[j]
				if a.Type == "boundary" || b.Type == "boundary" {
					continue
				}
				if partners[a.ID][b.ID] || shared(a.ID, b.ID) {
					continue
				}
				xOv := a.X < b.X+b.Width && b.X < a.X+a.Width
				yOv := a.Y < b.Y+b.Height && b.Y < a.Y+a.Height
				vGap := b.Y - (a.Y + a.Height)
				if a.Y > b.Y {
					vGap = a.Y - (b.Y + b.Height)
				}
				hGap := b.X - (a.X + a.Width)
				if a.X > b.X {
					hGap = a.X - (b.X + b.Width)
				}
				if (xOv && !yOv && vGap >= 0 && vGap <= 64) ||
					(yOv && !xOv && hGap >= 0 && hGap <= 64) {
					findings = append(findings, fmt.Sprintf("reads as paired: %q [%d,%d %dx%d] beside unrelated %q [%d,%d %dx%d]",
						clip(a.Label), a.X, a.Y, a.Width, a.Height, clip(b.Label), b.X, b.Y, b.Width, b.Height))
				}
			}
		}
	}

	byID := make(map[string]layout.Node, len(g.Nodes))
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	routes := layout.RoutesOf(g)
	for i, e := range g.Edges {
		if i >= len(routes) {
			break
		}
		from, fok := byID[e.From]
		to, tok := byID[e.To]
		if !fok || !tok {
			continue
		}
		sx, sy := layout.EdgePortPoint(from, to, routes[i].Source)
		tx, ty := layout.EdgePortPoint(to, from, routes[i].Target)
		if stubbed[i] {
			// Only the two badges exist visually: the REAL chip boxes from the
			// emitted stub geometry when present (they ladder/re-port around
			// boxes and visible edges), else the fixed approximation.
			for _, bb := range realOrApproxBadgeBoxes(g.Edges[i], sx, sy, tx, ty) {
				for _, n := range g.Nodes {
					if n.ID == e.From || n.ID == e.To {
						continue
					}
					if bb.x < n.X+n.Width && n.X < bb.x+bb.w &&
						bb.y < n.Y+n.Height && n.Y < bb.y+bb.h {
						findings = append(findings, fmt.Sprintf("badge overlap: %s→%s (%s) badge on %q [%d,%d %dx%d]",
							clip(from.Label), clip(to.Label), e.Base, clip(n.Label), n.X, n.Y, n.Width, n.Height))
					}
				}
				// v7P8: a chip keeps a visible gap from EDGES too — any
				// orientation, diagonals included
				chip := layout.Node{X: bb.x, Y: bb.y, Width: bb.w, Height: bb.h}
				for j, o := range g.Edges {
					if j == i || stubbed[j] || j >= len(routes) {
						continue
					}
					of, ook := byID[o.From]
					ot, tok2 := byID[o.To]
					if !ook || !tok2 {
						continue
					}
					osx, osy := layout.EdgePortPoint(of, ot, routes[j].Source)
					otx, oty := layout.EdgePortPoint(ot, of, routes[j].Target)
					for _, s := range routes[j].Segments(osx, osy, otx, oty) {
						if segmentIntersectsBoxMargin(s[0], s[1], s[2], s[3], chip, 2) {
							findings = append(findings, fmt.Sprintf("badge on edge: %s→%s (%s) badge over %s→%s",
								clip(from.Label), clip(to.Label), e.Base, clip(of.Label), clip(ot.Label)))
							break
						}
					}
				}
			}
			continue
		}
		for _, n := range g.Nodes {
			if n.ID == e.From || n.ID == e.To {
				continue
			}
			hit, near := false, false
			for _, s := range routes[i].Segments(sx, sy, tx, ty) {
				if !hit && segmentIntersectsShrunkBox(s[0], s[1], s[2], s[3], n) {
					hit = true
				}
				if !near && segmentIntersectsBoxMargin(s[0], s[1], s[2], s[3], n, 8) {
					near = true
				}
			}
			if hit {
				findings = append(findings, fmt.Sprintf("edge through node: %s→%s (%s) cuts %q [%d,%d %dx%d]",
					clip(from.Label), clip(to.Label), e.Base, clip(n.Label), n.X, n.Y, n.Width, n.Height))
			} else if near {
				// v7P8: a VISIBLE gap between any edge and any box it does
				// not connect to — S/E included; an arrowhead or a parallel
				// run inside 8px reads as touching.
				findings = append(findings, fmt.Sprintf("edge grazes node: %s→%s (%s) within 8px of %q [%d,%d %dx%d]",
					clip(from.Label), clip(to.Label), e.Base, clip(n.Label), n.X, n.Y, n.Width, n.Height))
			}
		}
	}
	// Edge-edge crossings: two routed segments that properly cross (shared
	// endpoints — forks/joins meeting at one node — do not count).
	type seg struct {
		x1, y1, x2, y2 int
		from, to       string
	}
	segs := make([]seg, 0, len(g.Edges))
	for i, e := range g.Edges {
		if i >= len(routes) {
			break
		}
		if stubbed[i] {
			continue
		}
		from, fok := byID[e.From]
		to, tok := byID[e.To]
		if !fok || !tok {
			continue
		}
		sx, sy := layout.EdgePortPoint(from, to, routes[i].Source)
		tx, ty := layout.EdgePortPoint(to, from, routes[i].Target)
		for _, s := range routes[i].Segments(sx, sy, tx, ty) {
			segs = append(segs, seg{s[0], s[1], s[2], s[3], e.From, e.To})
		}
	}
	for i := 0; i < len(segs); i++ {
		for j := i + 1; j < len(segs); j++ {
			a, b := segs[i], segs[j]
			// Skip only edges that share an endpoint POINT (a fork/join meeting
			// at one port). Edges sharing a NODE but leaving from different
			// ports can still make a real X — e.g. two expresses edges out of
			// one event whose port order disagrees with their targets' order.
			samePoint := (a.x1 == b.x1 && a.y1 == b.y1) || (a.x1 == b.x2 && a.y1 == b.y2) ||
				(a.x2 == b.x1 && a.y2 == b.y1) || (a.x2 == b.x2 && a.y2 == b.y2)
			if samePoint {
				continue
			}
			if segmentsProperlyCross(a.x1, a.y1, a.x2, a.y2, b.x1, b.y1, b.x2, b.y2) {
				findings = append(findings, fmt.Sprintf("edges cross: %s→%s × %s→%s",
					clip(byID[a.from].Label), clip(byID[a.to].Label), clip(byID[b.from].Label), clip(byID[b.to].Label)))
			}
		}
	}
	// Edges never COVER each other (v7P9): two parallel segments closer
	// than 8px with more than 16px of shared run read as one line.
	{
		type oseg struct {
			horizontal bool
			coord      int
			lo, hi     int
			from, to   string
		}
		var osegs []oseg
		for i, e := range g.Edges {
			if stubbed[i] || e.Route == nil {
				continue
			}
			pts := edgePoints(g, e)
			for k := 0; k+1 < len(pts); k++ {
				a, b := pts[k], pts[k+1]
				switch {
				case a[1] == b[1] && a[0] != b[0]:
					lo, hi := a[0], b[0]
					if lo > hi {
						lo, hi = hi, lo
					}
					osegs = append(osegs, oseg{true, a[1], lo, hi, e.From, e.To})
				case a[0] == b[0] && a[1] != b[1]:
					lo, hi := a[1], b[1]
					if lo > hi {
						lo, hi = hi, lo
					}
					osegs = append(osegs, oseg{false, a[0], lo, hi, e.From, e.To})
				}
			}
		}
		for i := 0; i < len(osegs); i++ {
			for j := i + 1; j < len(osegs); j++ {
				a, b := osegs[i], osegs[j]
				if a.from == b.from && a.to == b.to {
					continue
				}
				d := a.coord - b.coord
				if d < 0 {
					d = -d
				}
				if a.horizontal == b.horizontal && d < 8 &&
					minInt(a.hi, b.hi)-maxInt(a.lo, b.lo) > 16 {
					findings = append(findings, fmt.Sprintf("edges overlap: %s→%s × %s→%s (parallel %dpx apart, %dpx shared)",
						clip(labelOf(g, a.from)), clip(labelOf(g, a.to)),
						clip(labelOf(g, b.from)), clip(labelOf(g, b.to)),
						d, minInt(a.hi, b.hi)-maxInt(a.lo, b.lo)))
				}
			}
		}
	}

	// Stub chips keep clear of nodes: a hidden edge's stump (and the chip
	// at its tip) must not lie on any box — same severity family as
	// edge-through-node.
	for i, e := range g.Edges {
		if !stubbed[i] || e.Route == nil {
			continue
		}
		check := func(pl []layout.Position, own string) {
			if len(pl) < 2 {
				return
			}
			tipX, tipY := pl[len(pl)-1].X, pl[len(pl)-1].Y
			for _, n := range g.Nodes {
				if n.ID == own {
					continue
				}
				// the tip needs air for the chip: 8px margin box
				if tipX > n.X-8 && tipX < n.X+n.Width+8 &&
					tipY > n.Y-8 && tipY < n.Y+n.Height+8 {
					findings = append(findings, fmt.Sprintf("stub on node: %s→%s (%s) chip touches %q [%d,%d %dx%d]",
						clip(labelOf(g, e.From)), clip(labelOf(g, e.To)), e.Base, clip(n.Label), n.X, n.Y, n.Width, n.Height))
					return
				}
			}
		}
		check(e.Route.SourceStub, e.From)
		check(e.Route.TargetStub, e.To)
	}

	return findings
}

// Categorize buckets findings into the four severity classes the ratchet
// tracks: {node overlaps, edge-through-node family, edge-edge crossings,
// badge overlaps}. The through-node bucket is everything not one of the
// three named prefixes — grazes, false pairings, covers, stubs.
func Categorize(findings []string) [4]int {
	ov, ee, bo := 0, 0, 0
	for _, f := range findings {
		switch {
		case strings.HasPrefix(f, "node overlap"):
			ov++
		case strings.HasPrefix(f, "edges cross"):
			ee++
		case strings.HasPrefix(f, "badge overlap"):
			bo++
		}
	}
	return [4]int{ov, len(findings) - ov - ee - bo, ee, bo}
}

// Regressed reports whether current counts n are worse than baseline was,
// using the lexicographic severity order overlaps[0] > through-node[1] >
// edge-edge[2] > badge[3]: a file regresses when a worse-severity kind
// grows, or it ties on all worse kinds and a lesser kind grows. This is
// the ratchet core; any change to the ordering changes which regressions
// the CI gate catches.
func Regressed(was, n [4]int) bool {
	return n[0] > was[0] ||
		(n[0] == was[0] && n[1] > was[1]) ||
		(n[0] == was[0] && n[1] == was[1] && n[2] > was[2]) ||
		(n[0] == was[0] && n[1] == was[1] && n[2] == was[2] && n[3] > was[3])
}

// FormatTotals sums the per-file finding counts into one summary line.
// files is the count of files considered; dirty counts files with any
// finding.
func FormatTotals(files int, counts map[string][4]int) string {
	dirty, ov, en, ee, bo := 0, 0, 0, 0, 0
	for _, n := range counts {
		if n[0] > 0 || n[1] > 0 || n[2] > 0 || n[3] > 0 {
			dirty++
		}
		ov += n[0]
		en += n[1]
		ee += n[2]
		bo += n[3]
	}
	return fmt.Sprintf("TOTAL: files=%d dirty=%d overlaps=%d edge-through=%d crossings=%d badge=%d",
		files, dirty, ov, en, ee, bo)
}

// FormatBaseline renders the per-file finding counts as the ratchet
// baseline file: only dirty files, sorted, four counts then the path.
func FormatBaseline(counts map[string][4]int) []byte {
	files := make([]string, 0, len(counts))
	for f, n := range counts {
		if n[0] > 0 || n[1] > 0 || n[2] > 0 || n[3] > 0 {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	var b strings.Builder
	b.WriteString("# layout-check baseline: <node-overlaps> <edge-through-node> <edge-edge-crossings> <badge-overlaps> <file>\n")
	b.WriteString("# stubbed (hidden) Expresses/NearTo edges are excluded from edge metrics; their\n")
	b.WriteString("# badges are checked instead. --legacy-all-edges restores the old measurement.\n")
	b.WriteString("# regenerate: make layout-check-baseline  (layout-debug --check --write-baseline <file> <paths...>)\n")
	for _, f := range files {
		fmt.Fprintf(&b, "%d %d %d %d %s\n", counts[f][0], counts[f][1], counts[f][2], counts[f][3], f)
	}
	return []byte(b.String())
}

// ParseBaseline reads a baseline blob into per-file counts. A record is N
// leading integer counts (3 legacy, 4 current) followed by a file path;
// the leading integers are parsed greedily and the rest rejoined as the
// path, so a path containing spaces survives. Unparsable lines are
// skipped and returned as warnings (line-numbered) rather than failing.
func ParseBaseline(data []byte) (map[string][4]int, []string) {
	out := map[string][4]int{}
	var warnings []string
	for lineNo, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		nums := 0
		var counts [4]int
		for nums < 4 && nums < len(fields) {
			v, err := strconv.Atoi(fields[nums])
			if err != nil {
				break
			}
			counts[nums] = v
			nums++
		}
		if (nums != 3 && nums != 4) || nums >= len(fields) {
			warnings = append(warnings, fmt.Sprintf("ignoring unparsable baseline line %d: %q", lineNo+1, line))
			continue
		}
		out[strings.Join(fields[nums:], " ")] = counts
	}
	return out, warnings
}

// realOrApproxBadgeBoxes prefers the emitted chip centers (Route.SourceStub/
// TargetStub second points — the layout's REAL badge spots) and falls back
// to the fixed-reach approximation for graphs without emitted stubs.
func realOrApproxBadgeBoxes(e layout.Edge, sx, sy, tx, ty int) []struct{ x, y, w, h int } {
	if r := e.Route; r != nil && len(r.SourceStub) == 2 && len(r.TargetStub) == 2 {
		const chip = 20
		mk := func(p layout.Position) struct{ x, y, w, h int } {
			return struct{ x, y, w, h int }{p.X - chip/2, p.Y - chip/2, chip, chip}
		}
		return []struct{ x, y, w, h int }{mk(r.SourceStub[1]), mk(r.TargetStub[1])}
	}
	return badgeBoxes(sx, sy, tx, ty)
}

// badgeBoxes approximates the two "?" badge footprints of a stubbed edge: a
// badgeSize box centered badgeReach px from each endpoint along the segment.
// The renderer adapts its reach to local crowding; this fixed approximation
// is deliberately simple — the column it feeds is the least severe and only
// has to flag badges parked ON a foreign node.
func badgeBoxes(sx, sy, tx, ty int) []struct{ x, y, w, h int } {
	const badgeReach = 40
	const badgeSize = 24
	dx, dy := float64(tx-sx), float64(ty-sy)
	dist := math.Hypot(dx, dy)
	if dist == 0 {
		return nil
	}
	ux, uy := dx/dist, dy/dist
	mk := func(cx, cy float64) struct{ x, y, w, h int } {
		return struct{ x, y, w, h int }{int(cx) - badgeSize/2, int(cy) - badgeSize/2, badgeSize, badgeSize}
	}
	return []struct{ x, y, w, h int }{
		mk(float64(sx)+ux*badgeReach, float64(sy)+uy*badgeReach),
		mk(float64(tx)-ux*badgeReach, float64(ty)-uy*badgeReach),
	}
}

func segmentsProperlyCross(ax1, ay1, ax2, ay2, bx1, by1, bx2, by2 int) bool {
	o := func(px, py, qx, qy, rx, ry int) int {
		v := (qx-px)*(ry-py) - (qy-py)*(rx-px)
		switch {
		case v > 0:
			return 1
		case v < 0:
			return -1
		}
		return 0
	}
	o1 := o(ax1, ay1, ax2, ay2, bx1, by1)
	o2 := o(ax1, ay1, ax2, ay2, bx2, by2)
	o3 := o(bx1, by1, bx2, by2, ax1, ay1)
	o4 := o(bx1, by1, bx2, by2, ax2, ay2)
	return o1 != 0 && o2 != 0 && o3 != 0 && o4 != 0 && o1 != o2 && o3 != o4
}

func segmentIntersectsShrunkBox(x1, y1, x2, y2 int, n layout.Node) bool {
	return segmentIntersectsBoxMargin(x1, y1, x2, y2, n, -2)
}

// segmentIntersectsBoxMargin tests against the box grown by m (negative m
// shrinks — "touching the border is fine"; positive m demands a VISIBLE GAP
// around the box, v7P8).
func segmentIntersectsBoxMargin(x1, y1, x2, y2 int, n layout.Node, m int) bool {
	x0, y0 := float64(n.X-m), float64(n.Y-m)
	x3, y3 := float64(n.X+n.Width+m), float64(n.Y+n.Height+m)
	if x0 >= x3 || y0 >= y3 {
		return false
	}
	px, py := float64(x1), float64(y1)
	dx, dy := float64(x2-x1), float64(y2-y1)
	t0, t1 := 0.0, 1.0
	clipEdge := func(den, num float64) bool {
		if den == 0 {
			return num >= 0
		}
		t := num / den
		if den < 0 {
			if t > t1 {
				return false
			}
			if t > t0 {
				t0 = t
			}
		} else {
			if t < t0 {
				return false
			}
			if t < t1 {
				t1 = t
			}
		}
		return true
	}
	return clipEdge(-dx, px-x0) && clipEdge(dx, x3-px) && clipEdge(-dy, py-y0) && clipEdge(dy, y3-py) && t0 < t1
}

func clip(s string) string {
	// Clip on a rune boundary so a multibyte UTF-8 label is never split
	// mid-rune (which would emit an invalid/garbled sequence).
	r := []rune(s)
	if len(r) > 28 {
		return string(r[:28]) + "…"
	}
	return s
}

func labelOf(g *layout.Graph, id string) string {
	for _, n := range g.Nodes {
		if n.ID == id {
			if n.Label != "" {
				return n.Label
			}
			return n.ID
		}
	}
	return id
}

// edgePoints resolves an edge's drawn polyline (ports + bends).
func edgePoints(g *layout.Graph, e layout.Edge) [][2]int {
	var from, to layout.Node
	for _, n := range g.Nodes {
		if n.ID == e.From {
			from = n
		}
		if n.ID == e.To {
			to = n
		}
	}
	r := e.Route
	sx, sy := layout.EdgePortPoint(from, to, layout.EdgePort{Side: r.Source.Side, Position: r.Source.Position})
	tx, ty := layout.EdgePortPoint(to, from, layout.EdgePort{Side: r.Target.Side, Position: r.Target.Position})
	pts := [][2]int{{sx, sy}}
	for _, b := range r.Bends {
		pts = append(pts, [2]int{b.X, b.Y})
	}
	return append(pts, [2]int{tx, ty})
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
