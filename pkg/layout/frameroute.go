package layout

// Routing for a POSITIONED graph whose nodes were moved after the engine
// routed them — the zoom-canvas frames. It exists because DetourBlockedEdges,
// the pass it replaces in that pipeline, could only get an edge OFF a box; it
// had no notion of staying clear of one, of keeping two edges out of each
// other's lane, or of giving up on an edge that cannot be drawn well and hiding
// it — three faults a reader pointed at in one published diagram, all three
// the output of that pass's own detour candidates.
//
// This is still not a second layout engine. It places nothing, re-chooses no
// ports, and knows a relation only well enough to price a crossing. What it
// reproduces from layout7's v7P9 router (route.go) is exactly the part that
// matters once positions are final, with the SAME numbers so the canvas does
// not judge an edge differently from the flat render of the same document:
//
//   - clearance is a RULE, not a score: a segment within frameClear of a box
//     it does not connect to is blocked, the same as cutting it;
//   - parallel segments of different edges are separated by LaneSep, nested
//     lanes outward (separateLanes, reduced to what a positioned graph needs);
//   - every routed edge is priced — crossings by kind, grazes, detour tax —
//     against budget 1.0, and an edge over budget is HIDDEN as a stub, with
//     the two guards the engine has: leads-to never hides, and a node's last
//     visible connection never hides (it draws least-bad instead).
//
// It runs on a layout.Graph and only writes Route.Bends and Visibility, so
// ipmsvg draws the result with no other change. Nothing here is reachable
// from a flat render: the only caller is the zoom pipeline. DetourBlockedEdges
// is left as it was.

import "sort"

// frameClear is the gap every segment keeps from a box it does not touch:
// v7P8's VISIBLE GAP, 10px, "any drawn line and any box it does not connect
// to". Half of layout7's grid step; pkg/layout's GridStep happens to be that
// same 10. A first draft used 20 (detour.go's detourClear, the distance it
// routed AROUND a blocker at) and so judged a frame stricter than the flat
// render judges the same document — an edge 15px from a box was fine in
// layout-gen and blocked here. Rails around blockers still stand off further
// (frameRailPad); only the RULE is P8's number.
const frameClear = 10

// frameRailPad is how far a detour rail stands off the box it routes around:
// the visible gap plus one lane, so a rail sits at clearance and one lane out
// from where a neighbour's rail would be.
const frameRailPad = frameClear + frameLaneSep

// frameBudget and the prices are route.go's (budget 1.0; same-kind crossing
// 1.0, different kinds 0.5, graze 0.5, detour past 1.5x direct costs the
// excess). Copied, not tuned: an edge that the flat render of a document hides
// should be hidden on the canvas for the same reason.
const (
	frameBudget       = 1.0
	crossSameKind     = 1.0
	crossOtherKind    = 0.5
	crossFlowByTie    = 2.0  // P6/P9: a hierarchy tie never cuts the flow corridor — over budget alone
	crossBrushNear    = 0.25 // P9: two edges MEETING at a shared node brush near it; a quarter, not a tangle
	brushNearWithin   = 40   // px from the shared node within which a crossing is a brush (layout7 Clearance)
	grazeCost         = 0.5
	grazeBoundaryMult = 3.0 // P8 (ee960980): hugging S or E is prohibitive — boundaries weigh triple
	detourTaxOver     = 1.5
	frameLaneSep      = 10  // px, route.go's LaneSep: half of layout7's 20px grid step
	frameLaneMinShare = 16  // two runs closer than LaneSep for longer than this share a lane
	frameGridStep     = 120 // fallback sweep, as detour.go
	frameGridPad      = 240
	frameGridCap      = 900
)

// FrameRouteStats says what the pass did, for logs and tests.
type FrameRouteStats struct {
	Routed      int // edges given bends
	Straight    int // edges left straight (clear, or unroutable and drawn least-bad)
	Hidden      int // edges set to Visibility "stubbed" here
	Separated   int // lane shifts applied
	Unseparated int // overlaps that could not be shifted without a new fault
}

// RouteFrameEdges routes every visible edge of a positioned graph, hides the
// ones that cannot be drawn within budget, and separates parallel lanes.
// Positions must be final. Existing Route ports are honoured; only Bends and
// Visibility are written.
func RouteFrameEdges(g *Graph) FrameRouteStats {
	var st FrameRouteStats
	if g == nil || len(g.Edges) == 0 {
		return st
	}
	byID := make(map[string]Node, len(g.Nodes))
	obstacles := make([]Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		byID[n.ID] = n
		if n.Container != nil {
			continue // a shell encloses its members by construction
		}
		obstacles = append(obstacles, n)
	}

	routes := RoutesOf(g)
	ends := make([]detourSeg, len(g.Edges))
	ok := make([]bool, len(g.Edges))
	for i := range g.Edges {
		from, okF := byID[g.Edges[i].From]
		to, okT := byID[g.Edges[i].To]
		if !okF || !okT {
			continue
		}
		x1, y1 := EdgePortPoint(from, to, routes[i].Source)
		x2, y2 := EdgePortPoint(to, from, routes[i].Target)
		ends[i], ok[i] = detourSeg{x1, y1, x2, y2}, true
	}

	// The last-connection guard needs a node's VISIBLE degree, over the edges
	// as they stand before this pass; it is decremented as edges hide.
	visible := map[string]int{}
	for i := range g.Edges {
		if !ok[i] || g.Edges[i].Visibility == visibilityStubbed {
			continue
		}
		visible[g.Edges[i].From]++
		visible[g.Edges[i].To]++
	}

	// Live geometry for the crossing and lane scores: the straight set, updated
	// as edges are routed or hidden — a hidden edge's line must not be dodged.
	paths := make([][][2]int, len(g.Edges))
	for i := range g.Edges {
		if ok[i] && g.Edges[i].Visibility != visibilityStubbed {
			paths[i] = [][2]int{{ends[i].x1, ends[i].y1}, {ends[i].x2, ends[i].y2}}
		}
	}

	// P9's hide priority: "the less structural kind hides first — near-to
	// before expresses before part-of, and leads-to never". The engine gets
	// that order for free from the order it routes kinds; a positioned-graph
	// pass has to make it: route leads-to first, then part-of, expresses,
	// near-to, so the more structural kind claims the space and the less
	// structural one is the one that pays for crossing it — and, over
	// budget, the one that hides. Within a kind, slice order (deterministic).
	order := make([]int, 0, len(g.Edges))
	for i := range g.Edges {
		order = append(order, i)
	}
	sort.SliceStable(order, func(a, b int) bool {
		return kindRank(g.Edges[order[a]].Base) < kindRank(g.Edges[order[b]].Base)
	})

	for _, i := range order {
		e := &g.Edges[i]
		if !ok[i] || e.Visibility == visibilityStubbed {
			continue
		}
		straight := [][2]int{{ends[i].x1, ends[i].y1}, {ends[i].x2, ends[i].y2}}
		blocked := pathBlocked(straight, obstacles, e.From, e.To)

		var chosen [][2]int
		var cost float64
		if !blocked {
			chosen = straight
			cost = pathCost(straight, obstacles, paths, i, e.Base, g)
		}
		// The least-bad route, kept alongside the clean search: route.go's
		// leastBad scores every candidate INCLUDING the blocked ones (a hit
		// costs +100 there), so an edge that must draw — a leads-to, a last
		// connection — draws the least damaging thing available, not the raw
		// straight. Without this, etcd's last visible edge was a diagonal
		// through five boxes when a two-bend route that cleared four of them
		// existed and simply was not "clean".
		var leastBad [][2]int
		leastBadCost := 1e18
		if blocked || cost > frameBudget {
			// Look for a clean route. A candidate must be clear (not blocked)
			// to be considered at all; among clean ones the cheapest wins.
			cands := frameCandidates(ends[i], obstacles, e.From, e.To)
			var best [][2]int
			bestCost := 1e18
			// Least-bad is only ever USED for an edge the guards force to
			// draw. Scoring the full badness of every blocked candidate — 900
			// grid points, each swept against every box three times — is what
			// took NDA (248 nodes) from 40s to 60s and over corpus-gallery's
			// per-bundle timeout. So it is computed only when it can matter.
			mustDraw := e.Base == string(EdgeLeadsTo) || visible[e.From] <= 1 || visible[e.To] <= 1
			for _, c := range cands {
				if pathBlocked(c, obstacles, e.From, e.To) {
					if !mustDraw {
						continue
					}
					cc := pathCost(c, obstacles, paths, i, e.Base, g) + badness(c, obstacles, e.From, e.To)
					if cc < leastBadCost || (cc == leastBadCost && pathLength(c) < pathLength(leastBad)) {
						leastBad, leastBadCost = c, cc
					}
					continue
				}
				cc := pathCost(c, obstacles, paths, i, e.Base, g)
				if cc < bestCost || (cc == bestCost && pathLength(c) < pathLength(best)) {
					best, bestCost = c, cc
				}
				if cc < leastBadCost {
					leastBad, leastBadCost = c, cc
				}
			}
			if best != nil && (chosen == nil || bestCost < cost) {
				chosen, cost = best, bestCost
			}
		}

		switch {
		case chosen != nil && cost <= frameBudget:
			// Drawn within budget.
		case e.Base == string(EdgeLeadsTo) || visible[e.From] <= 1 || visible[e.To] <= 1:
			// Never hide a leads-to, and never take a node's last visible
			// connection: draw the least-bad thing available. The straight is
			// one of the candidates for that; it wins only when nothing bent
			// does less damage.
			sc := pathCost(straight, obstacles, paths, i, e.Base, g) +
				badness(straight, obstacles, e.From, e.To)
			if leastBad == nil || sc <= leastBadCost {
				chosen = straight
			} else {
				chosen = leastBad
			}
		default:
			e.Visibility = visibilityStubbed
			if e.Route != nil {
				e.Route.Bends = nil
			}
			visible[e.From]--
			visible[e.To]--
			paths[i] = nil
			st.Hidden++
			continue
		}

		if len(chosen) > 2 {
			if e.Route == nil {
				e.Route = &EdgeRouteJSON{
					Source: PortJSON{Side: routes[i].Source.Side, Position: routes[i].Source.Position},
					Target: PortJSON{Side: routes[i].Target.Side, Position: routes[i].Target.Position},
				}
			}
			e.Route.Bends = ptsToPositions(chosen[1 : len(chosen)-1])
			st.Routed++
		} else {
			if e.Route != nil {
				e.Route.Bends = nil
			}
			st.Straight++
		}
		paths[i] = chosen
	}

	sep, unsep := separateFrameLanes(g, paths, ends, ok, obstacles)
	st.Separated, st.Unseparated = sep, unsep
	return st
}

// pathBlocked is the clearance rule: any segment that cuts a foreign box, or
// passes within frameClear of one, blocks the path. The first frameClear px
// out of a port are exempt from the clearance (not the cut) test: a segment
// leaves its own box's border, and in a stack the neighbour is legitimately
// that close to the port.
func pathBlocked(pts [][2]int, obstacles []Node, fromID, toID string) bool {
	// One sweep over the boxes, not two. The fat (clearance) box contains the
	// real one, so a segment that cuts a box also cuts its fat box — EXCEPT in
	// the first frameClear px out of a port, which the clearance test trims off
	// (a segment starts on its own border). A box that close to a port would
	// be cut inside the trimmed stretch and missed, so the real box is tested
	// untrimmed and the fat box trimmed, in the same loop; pathCuts' slice
	// allocation per candidate was the profile's hottest line and is gone.
	for _, o := range obstacles {
		if o.ID == fromID || o.ID == toID {
			continue
		}
		fat := o
		fat.X, fat.Y = o.X-frameClear, o.Y-frameClear
		fat.Width, fat.Height = o.Width+2*frameClear, o.Height+2*frameClear
		for i := 0; i+1 < len(pts); i++ {
			a, b := pts[i], pts[i+1]
			if segmentCutsBox(a[0], a[1], b[0], b[1], o) {
				return true // cuts the box itself, wherever along the segment
			}
			// Trim the exempt stretch off the segments that leave the ports.
			if i == 0 {
				a = advance(a, b, frameClear)
			}
			if i+2 == len(pts) {
				b = advance(b, a, frameClear)
			}
			if a == b {
				continue
			}
			if segmentCutsBox(a[0], a[1], b[0], b[1], fat) {
				return true
			}
		}
	}
	return false
}

// advance moves p toward q by d px along the segment (or to q if shorter).
func advance(p, q [2]int, d int) [2]int {
	dx, dy := q[0]-p[0], q[1]-p[1]
	l := absInt(dx) + absInt(dy)
	if l <= d {
		return q
	}
	// Axis-aligned or not, walk d Manhattan px along the direction.
	return [2]int{p[0] + dx*d/l, p[1] + dy*d/l}
}

// pathCost is route.go's leastBad score, minus hitsNode (which pathBlocked has
// already excluded): crossings priced by kind, grazes, and detour tax.
func pathCost(pts [][2]int, obstacles []Node, paths [][][2]int, self int, base string, g *Graph) float64 {
	c := 0.0
	me := g.Edges[self]
	for j, other := range paths {
		if j == self || len(other) < 2 {
			continue
		}
		if !pathsCross(pts, other) {
			continue
		}
		o := g.Edges[j]
		switch {
		case isHierarchyTie(base) && isFlowEdge(g, o):
			// P6/P9: slicing the timeline reads as breaking the story.
			c += crossFlowByTie
		case sharesNode(me, o) && crossNear(pts, other, sharedNodeBoxes(g, me, o), brushNearWithin):
			// P9: fork lines necessarily brush at their box — a quarter.
			c += crossBrushNear
		case o.Base == base:
			c += crossSameKind
		default:
			c += crossOtherKind
		}
	}
	// P8: a graze is half a crossing; a graze of a BOUNDARY (S/E) is triple
	// that — never exempt, prohibitive on its own.
	for _, o := range obstacles {
		if o.ID == me.From || o.ID == me.To {
			continue
		}
		if pathGrazes(pts, []Node{o}, me.From, me.To) > 0 {
			if o.Type == "boundary" {
				c += grazeCost * grazeBoundaryMult
			} else {
				c += grazeCost
			}
		}
	}
	if len(pts) > 2 {
		direct := absInt(pts[len(pts)-1][0]-pts[0][0]) + absInt(pts[len(pts)-1][1]-pts[0][1])
		if direct > 0 {
			ratio := float64(pathLength(pts)) / float64(direct)
			if ratio > detourTaxOver {
				c += ratio - detourTaxOver
			}
		}
	}
	return c
}

// kindRank orders relation kinds from most to least structural, for the
// routing (and therefore hiding) order. Unknown kinds go last.
func kindRank(base string) int {
	switch base {
	case string(EdgeLeadsTo):
		return 0
	case string(EdgePartOf):
		return 1
	case string(EdgeExpresses):
		return 2
	case string(EdgeNearTo):
		return 3
	}
	return 4
}

func isHierarchyTie(base string) bool {
	return base == string(EdgePartOf) || base == string(EdgeExpresses)
}

// isFlowEdge is route.go's isFlow: a leads-to between two events.
func isFlowEdge(g *Graph, e Edge) bool {
	if e.Base != string(EdgeLeadsTo) {
		return false
	}
	var ft, tt string
	for _, n := range g.Nodes {
		if n.ID == e.From {
			ft = n.Type
		}
		if n.ID == e.To {
			tt = n.Type
		}
	}
	return ft == "event" && tt == "event"
}

func sharesNode(a, b Edge) bool {
	return a.From == b.From || a.From == b.To || a.To == b.From || a.To == b.To
}

func sharedNodeBoxes(g *Graph, a, b Edge) []Node {
	var out []Node
	for _, n := range g.Nodes {
		if (n.ID == a.From || n.ID == a.To) && (n.ID == b.From || n.ID == b.To) {
			out = append(out, n)
		}
	}
	return out
}

// crossNear says whether every crossing between the two polylines lies within
// `within` px of one of the given boxes — a brush at the shared node rather
// than a tangle farther out.
func crossNear(a, b [][2]int, boxes []Node, within int) bool {
	if len(boxes) == 0 {
		return false
	}
	near := false
	for i := 0; i+1 < len(a); i++ {
		for j := 0; j+1 < len(b); j++ {
			if !segmentsCross(a[i][0], a[i][1], a[i+1][0], a[i+1][1], b[j][0], b[j][1], b[j+1][0], b[j+1][1]) {
				continue
			}
			// The crossing point is somewhere on both segments; the segment
			// midpoint-to-box distance is a fine proxy at this granularity.
			mx, my := (a[i][0]+a[i+1][0])/2, (a[i][1]+a[i+1][1])/2
			ok := false
			for _, n := range boxes {
				dx := maxInt(0, maxInt(n.X-mx, mx-(n.X+n.Width)))
				dy := maxInt(0, maxInt(n.Y-my, my-(n.Y+n.Height)))
				if dx <= within && dy <= within {
					ok = true
					break
				}
			}
			if !ok {
				return false
			}
			near = true
		}
	}
	return near
}

// frameCandidates is detour.go's curated set (L-shapes, blocker-box corners
// and rails at two pads) plus its grid sweep — the SAME shapes, so an edge the
// old pass could route can still be routed here — but the pad is frameClear
// plus LaneSep, so a rail along a box sits at clearance and one lane out from
// where a neighbour's rail would be, instead of exactly at the old detourClear
// that the clearance rule now forbids.
func frameCandidates(s detourSeg, obstacles []Node, fromID, toID string) [][][2]int {
	x1, y1, x2, y2 := s.x1, s.y1, s.x2, s.y2
	blockers := cutBoxes(x1, y1, x2, y2, obstacles, fromID, toID)
	// Boxes within clearance of the straight count as blockers too: they are
	// what the straight has to be routed around now.
	for _, o := range obstacles {
		if o.ID == fromID || o.ID == toID {
			continue
		}
		fat := o
		fat.X, fat.Y = o.X-frameClear, o.Y-frameClear
		fat.Width, fat.Height = o.Width+2*frameClear, o.Height+2*frameClear
		if segmentCutsBox(x1, y1, x2, y2, fat) {
			dup := false
			for _, b := range blockers {
				if b.ID == o.ID {
					dup = true
					break
				}
			}
			if !dup {
				blockers = append(blockers, o)
			}
		}
	}

	var out [][][2]int
	add := func(mid ...[2]int) {
		p := append([][2]int{{x1, y1}}, mid...)
		out = append(out, append(p, [2]int{x2, y2}))
	}
	add([2]int{x1, y2})
	add([2]int{x2, y1})

	if len(blockers) > 0 {
		minX, minY := blockers[0].X, blockers[0].Y
		maxX, maxY := blockers[0].X+blockers[0].Width, blockers[0].Y+blockers[0].Height
		for _, b := range blockers[1:] {
			minX, minY = minInt(minX, b.X), minInt(minY, b.Y)
			maxX, maxY = maxInt(maxX, b.X+b.Width), maxInt(maxY, b.Y+b.Height)
		}
		boxes := [][4]int{{minX, minY, maxX, maxY}}
		for _, bl := range blockers {
			boxes = append(boxes, [4]int{bl.X, bl.Y, bl.X + bl.Width, bl.Y + bl.Height})
		}
		for _, bx := range boxes {
			for _, pad := range [2]int{frameRailPad, frameRailPad * 2} {
				l, r := bx[0]-pad, bx[2]+pad
				t, b := bx[1]-pad, bx[3]+pad
				add([2]int{l, t})
				add([2]int{r, t})
				add([2]int{l, b})
				add([2]int{r, b})
				add([2]int{l, y1}, [2]int{l, y2})
				add([2]int{r, y1}, [2]int{r, y2})
				add([2]int{x1, t}, [2]int{x2, t})
				add([2]int{x1, b}, [2]int{x2, b})
			}
		}
	}

	// Sweep, as detour.go: single waypoints on a coarse grid around the edge.
	gx0, gx1 := minInt(x1, x2)-frameGridPad, maxInt(x1, x2)+frameGridPad
	gy0, gy1 := minInt(y1, y2)-frameGridPad, maxInt(y1, y2)+frameGridPad
	tried := 0
	for wx := gx0; wx <= gx1 && tried < frameGridCap; wx += frameGridStep {
		for wy := gy0; wy <= gy1 && tried < frameGridCap; wy += frameGridStep {
			tried++
			add([2]int{wx, wy})
		}
	}
	return out
}

// separateFrameLanes shifts axis-parallel segments of different edges apart
// when they run within LaneSep of each other for more than frameLaneMinShare —
// route.go's separateLanes pass 2, for a graph where every edge is an aux edge.
// Only bend coordinates move (a port cannot), a shift that would block the
// edge or land it in another lane is rejected, and nested lanes shift outward
// so a stack of parallels fans rather than swaps.
func separateFrameLanes(g *Graph, paths [][][2]int, ends []detourSeg, ok []bool, obstacles []Node) (shifted, failed int) {
	type run struct {
		edge, seg int // paths[edge][seg]..[seg+1]
		vertical  bool
		coord     int // x for vertical, y for horizontal
		lo, hi    int
	}
	collect := func() []run {
		var rs []run
		for i, p := range paths {
			if len(p) < 3 { // a straight has no bend to move; ports are fixed
				continue
			}
			for s := 0; s+1 < len(p); s++ {
				a, b := p[s], p[s+1]
				// Only INTERIOR segments (both ends are bends) can shift as a
				// unit; a segment touching a port could only pivot.
				if s == 0 || s+2 == len(p) {
					continue
				}
				switch {
				case a[0] == b[0]:
					rs = append(rs, run{i, s, true, a[0], minInt(a[1], b[1]), maxInt(a[1], b[1])})
				case a[1] == b[1]:
					rs = append(rs, run{i, s, false, a[1], minInt(a[0], b[0]), maxInt(a[0], b[0])})
				}
			}
		}
		return rs
	}
	// Runs of ANY edge (straights included) that a shifted run must not land on.
	allRuns := func() []run {
		var rs []run
		for i, p := range paths {
			for s := 0; s+1 < len(p); s++ {
				a, b := p[s], p[s+1]
				switch {
				case a[0] == b[0]:
					rs = append(rs, run{i, s, true, a[0], minInt(a[1], b[1]), maxInt(a[1], b[1])})
				case a[1] == b[1]:
					rs = append(rs, run{i, s, false, a[1], minInt(a[0], b[0]), maxInt(a[0], b[0])})
				}
			}
		}
		return rs
	}
	overlaps := func(a, b run) bool {
		return a.vertical == b.vertical && a.edge != b.edge &&
			absInt(a.coord-b.coord) < frameLaneSep &&
			minInt(a.hi, b.hi)-maxInt(a.lo, b.lo) > frameLaneMinShare
	}
	// Deterministic order: by edge, then segment.
	movable := collect()
	sort.Slice(movable, func(i, j int) bool {
		if movable[i].edge != movable[j].edge {
			return movable[i].edge < movable[j].edge
		}
		return movable[i].seg < movable[j].seg
	})
	for _, m := range movable {
		all := allRuns()
		clash := false
		for _, o := range all {
			if overlaps(m, o) {
				clash = true
				break
			}
		}
		if !clash {
			continue
		}
		e := &g.Edges[m.edge]
		p := paths[m.edge]
		// Try offsets outward first (away from the edge's own straight line),
		// then inward, at 1, 2, 3 lanes.
		center := (ends[m.edge].x1 + ends[m.edge].x2) / 2
		if !m.vertical {
			center = (ends[m.edge].y1 + ends[m.edge].y2) / 2
		}
		dir := 1
		if m.coord < center {
			dir = -1
		}
		done := false
		for _, k := range []int{1, 2, 3, -1, -2, -3} {
			d := dir * k * frameLaneSep
			np := make([][2]int, len(p))
			copy(np, p)
			if m.vertical {
				np[m.seg][0] += d
				np[m.seg+1][0] += d
			} else {
				np[m.seg][1] += d
				np[m.seg+1][1] += d
			}
			if pathBlocked(np, obstacles, e.From, e.To) {
				continue
			}
			nm := m
			nm.coord += d
			bad := false
			for _, o := range all {
				if overlaps(nm, o) {
					bad = true
					break
				}
			}
			if bad {
				continue
			}
			paths[m.edge] = np
			e.Route.Bends = ptsToPositions(np[1 : len(np)-1])
			shifted++
			done = true
			break
		}
		if !done {
			failed++
		}
	}
	return shifted, failed
}

// badness is the least-bad penalty for a route that is not clean: 100 per
// foreign box it CUTS (route.go's hitsNode) plus 10 per box it merely runs
// within clearance of. Both are far above any crossing price, so a clean route
// always wins; between two unclean ones, cutting a box is ten times worse than
// hugging it, and hugging one is worse than crossing every edge in the graph.
// Without the second term the etcd edge, forced to draw, chose a run along
// three borders over a straight that crossed a few lines — a strictly worse
// picture by the rules this file exists to enforce.
func badness(pts [][2]int, obstacles []Node, fromID, toID string) float64 {
	return float64(100*cutCount(pts, obstacles, fromID, toID) + 10*hugCount(pts, obstacles, fromID, toID))
}

// hugCount is how many foreign boxes a path passes within frameClear of
// without cutting them — the clearance breaches pathBlocked refuses.
func hugCount(pts [][2]int, obstacles []Node, fromID, toID string) int {
	n := 0
	for _, o := range obstacles {
		if o.ID == fromID || o.ID == toID {
			continue
		}
		fat := o
		fat.X, fat.Y = o.X-frameClear, o.Y-frameClear
		fat.Width, fat.Height = o.Width+2*frameClear, o.Height+2*frameClear
		hug := false
		for i := 0; i+1 < len(pts) && !hug; i++ {
			a, b := pts[i], pts[i+1]
			if i == 0 {
				a = advance(a, b, frameClear)
			}
			if i+2 == len(pts) {
				b = advance(b, a, frameClear)
			}
			if a != b && segmentCutsBox(a[0], a[1], b[0], b[1], fat) && !segmentCutsBox(a[0], a[1], b[0], b[1], o) {
				hug = true
			}
		}
		if hug {
			n++
		}
	}
	return n
}

// cutCount is how many foreign boxes a path cuts through.
func cutCount(pts [][2]int, obstacles []Node, fromID, toID string) int {
	n := 0
	for i := 0; i+1 < len(pts); i++ {
		n += len(cutBoxes(pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1], obstacles, fromID, toID))
	}
	return n
}

func ptsToPositions(pts [][2]int) []Position {
	out := make([]Position, len(pts))
	for i, p := range pts {
		out[i] = Position{X: p[0], Y: p[1]}
	}
	return out
}
