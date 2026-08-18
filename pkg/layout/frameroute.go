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
	frameBudget        = 1.0
	crossSameKind      = 1.0
	crossOtherKind     = 0.5
	crossFlowByTie     = 2.0  // P6/P9: a hierarchy tie never cuts the flow corridor — over budget alone
	crossBrushNear     = 0.25 // P9: two edges MEETING at a shared node brush near it; a quarter, not a tangle
	brushNearWithin    = 40   // px from the shared node within which a crossing is a brush (layout7 Clearance)
	grazeCost          = 0.5
	frameGrazeBand     = 8   // px: the checker's band (layoutcheck "within 8px"). pathGrazes' fat box is +8 shrunk by routeShrink, so 6..8px was free to the router and a fault to the checker; a V that skimmed Billie's corner at 8px beat a clean 40px detour on crossings alone
	grazeBoundaryMult  = 3.0 // P8 (ee960980): hugging S or E is prohibitive — boundaries weigh triple
	overlapCost        = 1.0 // dcc3ebf4: a covered line is a crossing you cannot even see — priced as one
	detourTaxOver      = 1.5
	hairpinCost        = 0.5 // a turn sharper than a right angle: the line overshoots and comes back — half a crossing, like a graze
	frameLaneSep       = 10  // px, route.go's LaneSep: half of layout7's 20px grid step
	frameLaneMinShare  = 16  // two runs closer than LaneSep for longer than this share a lane
	frameHugRun        = 20  // a segment beside its OWN box for longer than this is a rail, not an arrival
	frameBoundaryClear = 20  // S/E keep a full grid step of air (P8: hugging a boundary is prohibitive)
	// A tie longer than this hides regardless of how cleanly it could be
	// drawn. At 100% zoom a screen is ~1600 css px, and a 120x60 label stays
	// legible to about 50%, so ~3200px is the longest span whose two ends can
	// ever be on one readable screen. Past it the line carries no information
	// AS A LINE — the reader can see one end or the other, never both — while
	// the stub chips at each end say the same thing in the same place. Owner's
	// rule, 2026-08-17: "you never will be able to see both nodes connected on
	// one page; if you zoom out enough to, you cannot read it." Leads-to is
	// exempt: it is the story's spine and its length IS the information.
	frameMaxTieLen = 3200
	// A pre-move stub is reconsidered only if the frame put its endpoints this
	// close: a quarter of a readable screen. Farther than that the engine's
	// verdict stands — it was made with better information than geometry alone.
	frameRehideLen = 800
	frameGridStep  = 120 // fallback sweep, as detour.go
	frameGridPad   = 240
	frameGridCap   = 900
)

// FrameRouteStats says what the pass did, for logs and tests.
type FrameRouteStats struct {
	Routed      int // edges given bends
	Straight    int // edges left straight (clear, or unroutable and drawn least-bad)
	Hidden      int // edges set to Visibility "stubbed" here
	Refaced     int // edges drawn on re-faced ports (a stale side re-chosen on the final boxes)
	Separated   int // lane shifts applied
	Unseparated int // overlaps that could not be shifted without a new fault
	// The bounding box every drawn route needs, ports and bends included.
	// Meta.Bounds is computed from the NODES before this pass runs; a route
	// that a forced edge takes around the top row can sit above y=0, and the
	// renderer's viewBox then clips it — 232 routes across 7 corpus bundles
	// were partly off-canvas. The caller grows the bounds to hold this.
	MinX, MinY, MaxX, MaxY int
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

	// Every edge is a candidate for drawing, on THIS frame's geometry. The
	// engine's Visibility verdicts came from the whole-document layout, at
	// positions the frame then discarded — and inheriting them as fixed facts
	// went wrong in the one way that matters: etcd had two edges, the engine
	// had hidden the one whose partner the frame later put 189px away, and
	// the last-connection guard then FORCED the other one — 3189px across the
	// canvas — to draw, because it was "etcd's last visible connection". So a
	// pre-move stub is un-hidden here and re-decided like the rest; the
	// budget, the kind order and the guards do the deciding, on real geometry.
	// The visible-degree count for the guard is therefore over ALL edges.
	//
	// But not un-hidden BLINDLY. Un-hiding every pre-move stub and letting the
	// budget re-decide made three corpus models worse (CFEngine's grazes
	// 64 -> 176): the engine's stub verdicts were mostly right, and a re-drawn
	// long tie displaces the edges around it. So a pre-move stub comes back
	// only when THIS frame makes it plainly worth drawing — its endpoints now
	// sit within frameRehideLen of each other. That is exactly the etcd case:
	// the engine hid it at 5000px apart, the frame put the endpoints 189px
	// apart. Everything else stays hidden. The un-hidden ones then go through
	// the same budget as the rest, and can hide again there.
	// ...AND its straight is clean. Un-hiding on distance alone still made
	// CFEngine's grazes 64 -> 176: a short stub whose straight skims a box
	// came back as a graze. Short and clear is the bar; short and dirty stays
	// the engine's call.
	adj := adjacency(g)
	for i := range g.Edges {
		if !ok[i] || g.Edges[i].Visibility != visibilityStubbed {
			continue
		}
		if absInt(ends[i].x2-ends[i].x1)+absInt(ends[i].y2-ends[i].y1) > frameRehideLen {
			continue
		}
		e := g.Edges[i]
		straight := [][2]int{{ends[i].x1, ends[i].y1}, {ends[i].x2, ends[i].y2}}
		if pathBlocked(straight, obstacles, e.From, e.To, neighbourSet(adj, e.From, e.To)) ||
			pathGrazes(straight, obstacles, e.From, e.To) > 0 {
			continue
		}
		g.Edges[i].Visibility = ""
		if g.Edges[i].Route != nil {
			g.Edges[i].Route.SourceStub, g.Edges[i].Route.TargetStub = nil, nil
		}
	}
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
	// Within a kind, SLICE order — deliberately not shorter-first. Shorter-
	// first sounded like P9's "the longer, more-bent edge hides", and it was
	// tried: it made FriendsAndFiends' grazes 37 -> 114 and NDA's crossings
	// 2267 -> 2751, because a short edge routed first claims a lane a long
	// structural one then has to bend around. The engine's tie-break applies
	// among candidates for ONE edge, not as a global routing order. Slice
	// order is the engine's own emit order, which already places skeleton
	// before aux; keep it.
	sort.SliceStable(order, func(a, b int) bool {
		return kindRank(g.Edges[order[a]].Base) < kindRank(g.Edges[order[b]].Base)
	})

	// The canvas a route may use: the declared bounds when the caller set
	// them, else the nodes' extent plus the layout margins. A candidate with a
	// point outside it is not a candidate — the viewBox would clip it, and a
	// bend the reader cannot see is worse than the crossing it avoided. Ports
	// are on boxes and always inside; only bends can leave.
	canvasX0, canvasY0, canvasX1, canvasY1 := frameCanvas(g)
	inCanvas := func(pts [][2]int) bool {
		for _, p := range pts[1 : len(pts)-1] {
			if p[0] < canvasX0 || p[1] < canvasY0 || p[0] > canvasX1 || p[1] > canvasY1 {
				return false
			}
		}
		return true
	}
	st.MinX, st.MinY, st.MaxX, st.MaxY = canvasX1, canvasY1, canvasX0, canvasY0
	extend := func(pts [][2]int) {
		for _, p := range pts {
			st.MinX, st.MinY = minInt(st.MinX, p[0]), minInt(st.MinY, p[1])
			st.MaxX, st.MaxY = maxInt(st.MaxX, p[0]), maxInt(st.MaxY, p[1])
		}
	}

	for _, i := range order {
		e := &g.Edges[i]
		if !ok[i] || e.Visibility == visibilityStubbed {
			continue
		}
		straight := [][2]int{{ends[i].x1, ends[i].y1}, {ends[i].x2, ends[i].y2}}
		if e.Base != string(EdgeLeadsTo) && absInt(ends[i].x2-ends[i].x1)+absInt(ends[i].y2-ends[i].y1) > frameMaxTieLen {
			// Too long to ever be seen whole. Hidden even when it is a node's
			// last connection: the guard exists so a node is not left with no
			// mark of its relation, and the stub chips ARE that mark — a chip
			// at each end beats a line the reader can only ever see one end of.
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
		nb := neighbourSet(adj, e.From, e.To)
		mustDraw := e.Base == string(EdgeLeadsTo) || visible[e.From] <= 1 || visible[e.To] <= 1

		// evalEnds routes ONE end-pair: the clean choice (straight if clear,
		// else the cheapest clean candidate) and the least-bad fallback the
		// guards may need. It is a closure so the same evaluation can be
		// run for the engine's ports and, below, for a re-faced pair.
		evalEnds := func(seg detourSeg, srcSide, tgtSide string) (chosen [][2]int, cost float64, leastBad [][2]int, leastBadCost float64) {
			straight := [][2]int{{seg.x1, seg.y1}, {seg.x2, seg.y2}}
			blocked := pathBlocked(straight, obstacles, e.From, e.To, nb)
			leastBadCost = 1e18
			price := func(pts [][2]int) float64 { return pathCost(pts, obstacles, paths, i, e.Base, g) }
			if !blocked {
				chosen = straight
				cost = price(straight)
			}
			// The least-bad route, kept alongside the clean search: route.go's
			// leastBad scores every candidate INCLUDING the blocked ones (a hit
			// costs +100 there), so an edge that must draw — a leads-to, a last
			// connection — draws the least damaging thing available, not the raw
			// straight. Without this, etcd's last visible edge was a diagonal
			// through five boxes when a two-bend route that cleared four of them
			// existed and simply was not "clean".
			if blocked || cost > frameBudget {
				// Look for a clean route. A candidate must be clear (not blocked)
				// to be considered at all; among clean ones the cheapest wins.
				cands := frameCandidates(seg, obstacles, e.From, e.To, sideOut(srcSide), sideOut(tgtSide))
				var best [][2]int
				bestCost := 1e18
				// Least-bad is only ever USED for an edge the guards force to
				// draw. Scoring the full badness of every blocked candidate — 900
				// grid points, each swept against every box three times — is what
				// took NDA (248 nodes) from 40s to 60s and over corpus-gallery's
				// per-bundle timeout. So it is computed only when it can matter.
				for _, c := range cands {
					if !inCanvas(c) {
						continue
					}
					if pathBlocked(c, obstacles, e.From, e.To, nb) {
						if !mustDraw {
							continue
						}
						cc := price(c) + badness(c, obstacles, e.From, e.To, nb)
						if cc < leastBadCost || (cc == leastBadCost && pathLength(c) < pathLength(leastBad)) {
							leastBad, leastBadCost = c, cc
						}
						continue
					}
					cc := price(c)
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
			return chosen, cost, leastBad, leastBadCost
		}

		chosen, cost, leastBad, leastBadCost := evalEnds(ends[i], routes[i].Source.Side, routes[i].Target.Side)

		// An edge with a STALE end — a port facing away from its partner,
		// because the frame moved the boxes after the engine chose the sides
		// (A pod stacked over a process, then set beside it: the near-to left
		// the pod's bottom, ran under both, and climbed into the process's
		// top; controller's top port with control plane below-right) — is
		// also tried on re-faced ports, the sides pickPortSide names on the
		// FINAL boxes. The re-faced ends win only when they route at least
		// as well: a re-facing pass that ran blind before the router hid 36
		// more edges in the reasoning corpus, where the re-faced straight ran
		// into a third box the U had cleared. The router knows; the pass did
		// not.
		if alt, okAlt := staleEnds(g, byID, routes, i, e); okAlt {
			c2, k2, lb2, lbk2 := evalEnds(alt.seg, alt.source.Side, alt.target.Side)
			if c2 != nil && (chosen == nil || k2 <= cost) {
				chosen, cost, leastBad, leastBadCost = c2, k2, lb2, lbk2
				ends[i] = alt.seg
				if e.Route == nil {
					e.Route = &EdgeRouteJSON{}
				}
				e.Route.Source, e.Route.Target = alt.source, alt.target
				routes[i].Source = EdgePort{Side: alt.source.Side, Position: alt.source.Position}
				routes[i].Target = EdgePort{Side: alt.target.Side, Position: alt.target.Position}
				st.Refaced++
			}
		}
		straight = [][2]int{{ends[i].x1, ends[i].y1}, {ends[i].x2, ends[i].y2}}

		switch {
		case chosen != nil && cost <= frameBudget:
			// Drawn within budget.
		case e.Base == string(EdgeLeadsTo) || visible[e.From] <= 1 || visible[e.To] <= 1:
			// Never hide a leads-to, and never take a node's last visible
			// connection: draw the least-bad thing available. The straight is
			// one of the candidates for that; it wins only when nothing bent
			// does less damage.
			sc := pathCost(straight, obstacles, paths, i, e.Base, g) +
				badness(straight, obstacles, e.From, e.To, nb)
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

		extend(chosen)
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

	sep, unsep := separateFrameLanes(g, paths, ends, ok, obstacles, adj)
	st.Separated, st.Unseparated = sep, unsep
	return st
}

// pathBlocked is the clearance rule: any segment that cuts a foreign box, or
// passes within frameClear of one, blocks the path. The first frameClear px
// out of a port are exempt from the clearance (not the cut) test: a segment
// leaves its own box's border, and in a stack the neighbour is legitimately
// that close to the port.
// frameFlush is the margin at which even a NEIGHBOUR's box counts as touched:
// layout7's GridStep/4 = 5. "The sibling exemption never covers a FLUSH
// contact: a line through a box's corner reads as touching no matter the
// kinship."
const frameFlush = 5

// pathBlocked is the clearance rule with layout7's neighbour exemption. A
// segment that cuts a foreign box, or passes within frameClear of one, blocks
// the path — UNLESS that box is a NEIGHBOUR of the edge (connected to either
// endpoint), in which case only a flush contact (within frameFlush) blocks.
// route.go's grazeCount: "a wide fan's outer edges naturally skim their
// siblings' corners on the shared row — detouring them into lanes reads far
// worse than the skim". On NDA a column of fifteen things converges on one hub
// across a 20px gutter; without the exemption every one of them was forced
// into an L in a gutter that holds one lane, and they stacked. With it they
// fan as diagonals, skimming their siblings, as the flat render draws them.
func pathBlocked(pts [][2]int, obstacles []Node, fromID, toID string, neighbour map[string]bool) bool {
	// One sweep over the boxes, not two. The fat (clearance) box contains the
	// real one, so a segment that cuts a box also cuts its fat box — EXCEPT in
	// the first frameClear px out of a port, which the clearance test trims off
	// (a segment starts on its own border). A box that close to a port would
	// be cut inside the trimmed stretch and missed, so the real box is tested
	// untrimmed and the fat box trimmed, in the same loop; pathCuts' slice
	// allocation per candidate was the profile's hottest line and is gone.
	//
	// The edge's OWN endpoints are exempt from the cut test (a segment must
	// enter its box) but NOT from running along that box's other sides: on
	// NDA, fifteen part-of edges from a stacked column each took the L via
	// the hub's x and ran up the hub's own left border for hundreds of px to
	// their port — thirteen of them on one line — because "own endpoint" waived
	// the whole box. hugsOwnBox catches a segment that lies beside its own
	// endpoint for more than the arrival's worth of run.
	if hugsOwnBox(pts, obstacles, fromID, toID) {
		return true
	}
	for _, o := range obstacles {
		if o.ID == fromID || o.ID == toID {
			continue
		}
		clear := frameClear
		switch {
		case o.Type == "boundary":
			// S/E are never exempt and hugging them is prohibitive (P8,
			// ee960980): a full grid step of air, not just the visible gap.
			clear = frameBoundaryClear
		case neighbour[o.ID]:
			clear = frameFlush // a sibling may be skimmed, not touched
		}
		// segmentCutsBox shrinks its box by routeShrink (2px) so a line
		// exactly ON a border does not count as cutting it; for the clearance
		// test that shrink is wrong — a segment exactly `clear` away sat 10px
		// from an S box and passed. Pad by the shrink so the fat box's edge is
		// where clearance actually ends.
		fat := o
		fat.X, fat.Y = o.X-clear-routeShrink, o.Y-clear-routeShrink
		fat.Width, fat.Height = o.Width+2*(clear+routeShrink), o.Height+2*(clear+routeShrink)
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

// hugsOwnBox says whether some segment of pts runs ALONG a side of one of the
// edge's own endpoint boxes — axis-parallel, within frameClear of the side, and
// sharing more than an arrival's worth of run with it (frameHugRun) — rather
// than merely entering it. The segment that carries the port itself is judged
// only on the part beyond frameHugRun from the port, so a normal approach is
// never a hug and a rail along the box always is.
func hugsOwnBox(pts [][2]int, obstacles []Node, fromID, toID string) bool {
	if len(pts) < 2 {
		return false
	}
	for _, o := range obstacles {
		if o.ID != fromID && o.ID != toID {
			continue
		}
		l, r, tp, bt := o.X, o.X+o.Width, o.Y, o.Y+o.Height
		// The arrival/departure is trimmed by PATH distance from THIS box's
		// port — frameHugRun px along the polyline, across bends — so a
		// stub of one lane out of the port plus the start of the run beside
		// the box is one arrival, and a rail along the box always exceeds it.
		var fromLeft, toLeft int
		if o.ID == fromID {
			fromLeft = frameHugRun
		}
		if o.ID == toID {
			toLeft = frameHugRun
		}
		// distance from the source port to pts[i], and from pts[i+1] to
		// the target port, along the path
		n := len(pts)
		fromDist := make([]int, n)
		for i := 1; i < n; i++ {
			fromDist[i] = fromDist[i-1] + absInt(pts[i][0]-pts[i-1][0]) + absInt(pts[i][1]-pts[i-1][1])
		}
		total := fromDist[n-1]
		for i := 0; i+1 < len(pts); i++ {
			a, b := pts[i], pts[i+1]
			if fromLeft > 0 {
				if left := fromLeft - fromDist[i]; left > 0 {
					a = advance(a, b, left)
				}
			}
			if toLeft > 0 {
				if left := toLeft - (total - fromDist[i+1]); left > 0 {
					b = advance(b, a, left)
				}
			}
			if a == b || (a[0] != b[0] && a[1] != b[1]) {
				continue
			}
			if (b[0]-a[0])*(pts[i+1][0]-pts[i][0]) < 0 || (b[1]-a[1])*(pts[i+1][1]-pts[i][1]) < 0 {
				continue // trims met and crossed: the segment is all arrival
			}
			switch {
			case a[0] == b[0]: // vertical: beside the left or right side?
				x := a[0]
				if absInt(x-l) <= frameClear || absInt(x-r) <= frameClear {
					lo, hi := minInt(a[1], b[1]), maxInt(a[1], b[1])
					if minInt(hi, bt)-maxInt(lo, tp) > frameHugRun {
						return true
					}
				}
			case a[1] == b[1]: // horizontal: beside the top or bottom?
				y := a[1]
				if absInt(y-tp) <= frameClear || absInt(y-bt) <= frameClear {
					lo, hi := minInt(a[0], b[0]), maxInt(a[0], b[0])
					if minInt(hi, r)-maxInt(lo, l) > frameHugRun {
						return true
					}
				}
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
	// A shared lane. dcc3ebf4 made the engine's lane pass register separated
	// lanes "as crossings where covered lines hid them — honest accounting";
	// pricing the overlap here means the candidate that takes an EMPTY lane
	// wins before the lane pass ever has to move anything, and a fan into a
	// hub spreads by construction rather than stacking on the first rail.
	c += overlapCost * float64(pathOverlaps(pts, paths, self))
	// P8: a graze is half a crossing; a graze of a BOUNDARY (S/E) is triple
	// that — never exempt, prohibitive on its own.
	for _, o := range obstacles {
		if o.ID == me.From || o.ID == me.To {
			continue
		}
		if pathGrazesWithin(pts, o, frameGrazeBand) {
			if o.Type == "boundary" {
				c += grazeCost * grazeBoundaryMult
			} else {
				c += grazeCost
			}
		}
	}
	// A HAIRPIN — adjacent segments turning back on themselves, sharper than
	// a right angle — is a zig-zag by construction: the line overshoots and
	// returns. An L (exactly a right angle) and a Z are free; the fallback
	// sweep's single-waypoint V that went 1000px up past controller and came
	// back down into its top port (kubernetes s:, pe=117) is what this
	// prices, so a same-cost L, or re-faced ports, win over it.
	for k := 1; k+1 < len(pts); k++ {
		ax, ay := pts[k][0]-pts[k-1][0], pts[k][1]-pts[k-1][1]
		bx, by := pts[k+1][0]-pts[k][0], pts[k+1][1]-pts[k][1]
		if ax*bx+ay*by < 0 {
			c += hairpinCost
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

// frameCanvas is the rectangle a route may occupy: the graph's declared bounds
// when set (the zoom pipeline sets them from the nodes plus margins before
// this pass), else the nodes' extent plus the layout margins.
func frameCanvas(g *Graph) (x0, y0, x1, y1 int) {
	if g.Meta.Bounds.Width > 0 && g.Meta.Bounds.Height > 0 {
		return 0, 0, g.Meta.Bounds.Width, g.Meta.Bounds.Height
	}
	first := true
	for _, n := range g.Nodes {
		if first {
			x0, y0, x1, y1 = n.X, n.Y, n.X+n.Width, n.Y+n.Height
			first = false
			continue
		}
		x0, y0 = minInt(x0, n.X), minInt(y0, n.Y)
		x1, y1 = maxInt(x1, n.X+n.Width), maxInt(y1, n.Y+n.Height)
	}
	return x0 - MarginX, y0 - MarginY, x1 + MarginX, y1 + MarginY
}

func inCanvasPts(pts [][2]int, x0, y0, x1, y1 int) bool {
	for _, p := range pts[1 : len(pts)-1] {
		if p[0] < x0 || p[1] < y0 || p[0] > x1 || p[1] > y1 {
			return false
		}
	}
	return true
}

// adjacency is node id -> the ids it shares an edge with, built once per graph.
func adjacency(g *Graph) map[string][]string {
	adj := map[string][]string{}
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}
	return adj
}

// neighbourSet is the set of node ids connected to either endpoint of an edge
// — layout7's sibling exemption for the graze/clearance rule.
func neighbourSet(adj map[string][]string, fromID, toID string) map[string]bool {
	nb := map[string]bool{fromID: true, toID: true}
	for _, n := range adj[fromID] {
		nb[n] = true
	}
	for _, n := range adj[toID] {
		nb[n] = true
	}
	return nb
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
			// The actual crossing point — the segment midpoint was the proxy
			// before, and for a 450px straight whose crossing sits at its very
			// end (an arrival brushing a sibling's arrival 12px from their
			// shared box) the midpoint was 200px away, so the brush was priced
			// as a tangle and the straight lost to a route through the box.
			mx, my := crossingPoint(a[i], a[i+1], b[j], b[j+1])
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
func frameCandidates(s detourSeg, obstacles []Node, fromID, toID string, srcOut, tgtOut [2]int) [][][2]int {
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

	// Port STUBS (v7P8/P9's port claim, the shape the lane pass cannot make):
	// two edges leaving adjacent slots of one face and turning the same way
	// run their first segment along their own border on top of each other —
	// the lane pass never moves a port-touching segment, and no curated
	// candidate here left the port any other way. So every curated candidate
	// whose FIRST segment runs along the source's face gets variants that
	// leave the port by one and two lanes first, then run parallel to the
	// original; likewise before the target port. The router prices the shared
	// run (overlapCost), so the stubbed variant wins exactly when the plain
	// one would lie on an earlier edge. Curated only — the sweep below is
	// single waypoints, which have no face-parallel first segment to stub.
	curated := len(out)
	for _, d := range [2]int{frameLaneSep, 2 * frameLaneSep} {
		for ci := 0; ci < curated; ci++ {
			c := out[ci]
			if len(c) < 3 {
				continue
			}
			if v, ok := stubbedAtSource(c, srcOut, d); ok {
				out = append(out, v)
			}
			if v, ok := stubbedAtTarget(c, tgtOut, d); ok {
				out = append(out, v)
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
func separateFrameLanes(g *Graph, paths [][][2]int, ends []detourSeg, ok []bool, obstacles []Node, adj map[string][]string) (shifted, failed int) {
	canvasX0, canvasY0, canvasX1, canvasY1 := frameCanvas(g)
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
			if !inCanvasPts(np, canvasX0, canvasY0, canvasX1, canvasY1) ||
				pathBlocked(np, obstacles, e.From, e.To, neighbourSet(adj, e.From, e.To)) {
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
func badness(pts [][2]int, obstacles []Node, fromID, toID string, neighbour map[string]bool) float64 {
	return float64(100*cutCount(pts, obstacles, fromID, toID) + 10*hugCount(pts, obstacles, fromID, toID, neighbour))
}

// hugCount is how many foreign boxes a path passes within frameClear of
// without cutting them — the clearance breaches pathBlocked refuses.
func hugCount(pts [][2]int, obstacles []Node, fromID, toID string, neighbour map[string]bool) int {
	n := 0
	for _, o := range obstacles {
		if o.ID == fromID || o.ID == toID {
			continue
		}
		clear := frameClear
		switch {
		case o.Type == "boundary":
			clear = frameBoundaryClear
		case neighbour[o.ID]:
			clear = frameFlush
		}
		fat := o
		fat.X, fat.Y = o.X-clear-routeShrink, o.Y-clear-routeShrink
		fat.Width, fat.Height = o.Width+2*(clear+routeShrink), o.Height+2*(clear+routeShrink)
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

// uturnAlt is a re-faced end-pair for a U-turn edge: the ports and the
// segment between them.
type uturnAlt struct {
	source, target PortJSON
	seg            detourSeg
}

// staleEnds returns re-faced ports for edge i when at least one of its ports
// faces AWAY from the partner — the partner's near edge lies BEHIND the
// port's side, so no line out of that side reaches it without turning back.
// A stale end is re-faced to pickPortSide's choice on the final boxes; an end
// that still faces its partner keeps the engine's side. The slot on a new
// side is the midline unless another edge of the node already holds it, then
// the first free of 0.35/0.65/0.25/0.75.
//
// Both-stale is the U-turn (A pod stacked over a process, then set beside
// it). Single-stale is a port the frame turned away from its partner while
// the other end still faces it: controller's TOP port with control plane
// below-right — the only clean route the sweep found went 1000px up past the
// target and hairpinned back down into the top (kubernetes s:, pe=117).
// Re-facing single ends BLINDLY, in a pass before the router, was measured
// and rejected (+200 hidden in kubernetes: K8s's stale right port re-faced to
// top landed under a box its own-border rail had cleared). As a router
// ALTERNATIVE — taken only when it routes at least as well — that case keeps
// its rail and this one gets its L.
func staleEnds(g *Graph, byID map[string]Node, routes []EdgeRoute, i int, e *Edge) (uturnAlt, bool) {
	from, okF := byID[e.From]
	to, okT := byID[e.To]
	if !okF || !okT || from.Container != nil || to.Container != nil || e.From == e.To {
		return uturnAlt{}, false
	}
	sF := refacedSide(from, to, routes[i].Source.Side)
	sT := refacedSide(to, from, routes[i].Target.Side)
	if sF == "" && sT == "" {
		return uturnAlt{}, false
	}
	src := PortJSON{Side: routes[i].Source.Side, Position: routes[i].Source.Position}
	tgt := PortJSON{Side: routes[i].Target.Side, Position: routes[i].Target.Position}
	// the partner points that order the slots: the OTHER end's port as it
	// stands (its own re-face, if any, is applied first for the source so the
	// target's slot is ordered against the final source point)
	x2, y2 := EdgePortPoint(to, from, EdgePort{Side: tgt.Side, Position: tgt.Position})
	if sF != "" {
		src = PortJSON{Side: sF, Position: freeSlot(g, byID, routes, i, e.From, sF, [2]int{x2, y2})}
	}
	x1, y1 := EdgePortPoint(from, to, EdgePort{Side: src.Side, Position: src.Position})
	if sT != "" {
		tgt = PortJSON{Side: sT, Position: freeSlot(g, byID, routes, i, e.To, sT, [2]int{x1, y1})}
		x2, y2 = EdgePortPoint(to, from, EdgePort{Side: tgt.Side, Position: tgt.Position})
	}
	return uturnAlt{source: src, target: tgt, seg: detourSeg{x1, y1, x2, y2}}, true
}

// refacedSide returns the side `node`'s port should move to when `side` no
// longer faces `other` at all, or "" when it still does. "No longer faces"
// is strict: the partner's NEAR edge lies behind the side's line — for a
// bottom port, the partner's top is above the node's bottom. A partner that
// is in front of the side but far off its axis (a satellite column beside a
// hub) keeps the engine's side: that is the engine's fan onto one face.
func refacedSide(node, other Node, side string) string {
	inFront := true
	switch side {
	case "top":
		inFront = other.Y+other.Height <= node.Y
	case "bottom":
		inFront = other.Y >= node.Y+node.Height
	case "left":
		inFront = other.X+other.Width <= node.X
	case "right":
		inFront = other.X >= node.X+node.Width
	default:
		return ""
	}
	if inFront {
		return ""
	}
	want := pickPortSide(node, other)
	if want == side {
		return ""
	}
	return want
}

// freeSlot picks a slot on (node, side) for edge `self`'s re-faced end: not
// held by another edge's port, and in PARTNER ORDER with the ends already on
// that side — the same monotonicity OrderSharedPorts guarantees, which that
// pass cannot apply here because the re-face happens after it. Without the
// order, three arrivals re-faced onto API's right side took 0.5 / 0.35 / 0.65
// in edge order; the one from above got the lowest slot and needed an L that
// came down the box's border where the straight would have done.
//
// `partner` is the far end's port point; the side's axis coordinate of it
// (y for left/right, x for top/bottom) orders the slots. Among the free
// candidate slots the one with no order violation against the placed ends
// wins, then the fewest, then the one nearest the midline.
func freeSlot(g *Graph, byID map[string]Node, routes []EdgeRoute, self int, node, side string, partner [2]int) float64 {
	type placed struct {
		slot  float64
		along int
	}
	var ends []placed
	used := map[float64]bool{}
	for j := range g.Edges {
		if j == self || g.Edges[j].Visibility == visibilityStubbed {
			continue
		}
		o := g.Edges[j]
		var slot float64
		var other Node
		var okO bool
		switch {
		case o.From == node && routes[j].Source.Side == side:
			slot = routes[j].Source.Position
			other, okO = byID[o.To]
			if okO {
				px, py := EdgePortPoint(other, byID[node], routes[j].Target)
				ends = append(ends, placed{slot, alongOf(side, px, py)})
			}
		case o.To == node && routes[j].Target.Side == side:
			slot = routes[j].Target.Position
			other, okO = byID[o.From]
			if okO {
				px, py := EdgePortPoint(other, byID[node], routes[j].Source)
				ends = append(ends, placed{slot, alongOf(side, px, py)})
			}
		default:
			continue
		}
		used[slot] = true
	}
	mine := alongOf(side, partner[0], partner[1])
	// the ordered neighbours: the highest slot among ends whose partner is
	// before mine, the lowest among those after — a slot between them keeps
	// the side monotonic
	lo, hi := 0.0, 1.0
	for _, e := range ends {
		if e.along < mine && e.slot > lo {
			lo = e.slot
		}
		if e.along > mine && e.slot < hi {
			hi = e.slot
		}
	}
	best, bestDist := -1.0, 2.0
	for _, p := range []float64{0.5, 0.35, 0.65, 0.25, 0.75, 0.15, 0.85} {
		if used[p] || p <= lo || p >= hi {
			continue
		}
		dist := p - 0.5
		if dist < 0 {
			dist = -dist
		}
		if dist < bestDist {
			best, bestDist = p, dist
		}
	}
	if best >= 0 {
		return best
	}
	// no discrete slot fits between the neighbours: take their midpoint when
	// there is room for a lane (API's right side had 88 at 0.35 and 98 at
	// 0.50 with the third arrival's partner between theirs), else the
	// nearest free discrete slot, order be damned — a wrong slot beats two
	// ends on one point
	if hi-lo >= 0.1 {
		return (lo + hi) / 2
	}
	for _, p := range []float64{0.5, 0.35, 0.65, 0.25, 0.75, 0.15, 0.85} {
		if !used[p] {
			return p
		}
	}
	return 0.5
}

// alongOf is the coordinate that orders a side's slots: y for left/right,
// x for top/bottom.
func alongOf(side string, x, y int) int {
	if side == "left" || side == "right" {
		return y
	}
	return x
}

// pathGrazesWithin says whether any segment of pts passes within `band` px
// of box o (the box grown by band, tested UNSHRUNK — segmentCutsBox shrinks
// by routeShrink, so the growth compensates).
func pathGrazesWithin(pts [][2]int, o Node, band int) bool {
	fat := o
	fat.X, fat.Y = o.X-band-routeShrink, o.Y-band-routeShrink
	fat.Width, fat.Height = o.Width+2*(band+routeShrink), o.Height+2*(band+routeShrink)
	for i := 0; i+1 < len(pts); i++ {
		if segmentCutsBox(pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1], fat) {
			return true
		}
	}
	return false
}

// sideOut is the unit vector pointing OUT of a port side; zero for center or
// unknown (no stubs then).
func sideOut(side string) [2]int {
	switch side {
	case "left":
		return [2]int{-1, 0}
	case "right":
		return [2]int{1, 0}
	case "top":
		return [2]int{0, -1}
	case "bottom":
		return [2]int{0, 1}
	}
	return [2]int{}
}

// stubbedAtSource returns c with a stub of d px out of the source port before
// its first segment, when that segment is axis-parallel and runs ALONG the
// port's face (perpendicular to out) — the case where two departures from
// one face coincide. The first bend moves with it, so the second segment
// stays axis-parallel.
func stubbedAtSource(c [][2]int, out [2]int, d int) ([][2]int, bool) {
	if out == [2]int{} || len(c) < 3 {
		return nil, false
	}
	a, b := c[0], c[1]
	dx, dy := b[0]-a[0], b[1]-a[1]
	if (dx != 0 && dy != 0) || (dx == 0 && dy == 0) {
		return nil, false // slanted or degenerate
	}
	if dx*out[0]+dy*out[1] != 0 {
		return nil, false // already leaves along the out vector
	}
	stub := [2]int{a[0] + d*out[0], a[1] + d*out[1]}
	moved := [2]int{b[0] + d*out[0], b[1] + d*out[1]}
	v := make([][2]int, 0, len(c)+1)
	v = append(v, a, stub, moved)
	v = append(v, c[2:]...)
	return v, true
}

// stubbedAtTarget is stubbedAtSource mirrored at the target port.
func stubbedAtTarget(c [][2]int, out [2]int, d int) ([][2]int, bool) {
	if out == [2]int{} || len(c) < 3 {
		return nil, false
	}
	n := len(c)
	a, b := c[n-2], c[n-1]
	dx, dy := a[0]-b[0], a[1]-b[1] // from the port back along the last segment
	if (dx != 0 && dy != 0) || (dx == 0 && dy == 0) {
		return nil, false
	}
	if dx*out[0]+dy*out[1] != 0 {
		return nil, false
	}
	stub := [2]int{b[0] + d*out[0], b[1] + d*out[1]}
	moved := [2]int{a[0] + d*out[0], a[1] + d*out[1]}
	v := make([][2]int, 0, n+1)
	v = append(v, c[:n-2]...)
	v = append(v, moved, stub, b)
	return v, true
}

// crossingPoint returns the intersection of two segments known to cross
// (segmentsCross said so), rounded to px; falls back to the first segment's
// midpoint if the lines are parallel.
func crossingPoint(p1, p2, q1, q2 [2]int) (int, int) {
	x1, y1, x2, y2 := float64(p1[0]), float64(p1[1]), float64(p2[0]), float64(p2[1])
	x3, y3, x4, y4 := float64(q1[0]), float64(q1[1]), float64(q2[0]), float64(q2[1])
	den := (x1-x2)*(y3-y4) - (y1-y2)*(x3-x4)
	if den == 0 {
		return (p1[0] + p2[0]) / 2, (p1[1] + p2[1]) / 2
	}
	t := ((x1-x3)*(y3-y4) - (y1-y3)*(x3-x4)) / den
	return int(x1 + t*(x2-x1) + 0.5), int(y1 + t*(y2-y1) + 0.5)
}
