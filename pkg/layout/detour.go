package layout

// Detour routing for a POSITIONED graph — the fallback that keeps an edge off
// the boxes when the engine's own route no longer applies.
//
// pkg/layout is renderer-agnostic infrastructure, not an engine: it does not
// place anything and does not know a leads-to from a tie. What it can do is
// geometry, and geometry is all this needs. A consumer that moved nodes after
// the engine routed them (zoom/canvas frames: anchor overwrite, container
// geometry, band compaction, satellite pinning) has to discard the
// engine's absolute bends, and is otherwise left drawing every edge straight —
// through whatever now sits in the way.
//
// This is deliberately NOT a second layout engine. It is conservative by
// construction: an edge whose straight line is already clear is never touched,
// and an edge with no clean detour is left straight rather than bent into
// something worse. It cannot improve on layout7's kind-aware routing (no
// crossing budget by relation kind, no hide ordering) — it only removes the
// edges that visibly cut through a box.

// detourClear is the gap a detour keeps from the box it routes around: enough
// to read as "going around" rather than grazing the border.
const detourClear = 20

// The fallback sweep's geometry (see pickDetour). Step is one node width — the
// gaps between obstacles are what the sweep is looking for. Cap bounds the work
// on a pathologically long edge.
const (
	detourGridStep = 120
	detourGridPad  = 240
	detourGridCap  = 900
)

// detourGraze is the margin at which a passing line reads as touching a box it
// does not connect to (the same 8px the universal invariants use). A candidate
// that clears every box but skims one is barely better than the straight line
// it replaced, so grazes are scored ahead of crossings and length.
const detourGraze = 8

// detourSeg is an edge's resolved straight geometry (port to port).
type detourSeg struct{ x1, y1, x2, y2 int }

// DetourBlockedEdges gives a bend path to every edge whose straight
// port-to-port line cuts a node box, and leaves every other edge alone. It
// returns the number of edges it rerouted.
//
// Obstacles are the graph's real boxes. CONTAINER nodes are excluded: a shell
// encloses its own members by construction, so counting it would make every
// edge inside a container look blocked and there would be no clean route to
// find. An edge's own endpoints are excluded too.
//
// Existing Route ports are honoured as the endpoints to route between; only
// Bends are written. Call it after the positions are final.
func DetourBlockedEdges(g *Graph) int {
	if g == nil || len(g.Edges) == 0 {
		return 0
	}
	byID := make(map[string]Node, len(g.Nodes))
	obstacles := make([]Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		byID[n.ID] = n
		if n.Container != nil {
			continue
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

	// Live geometry of every edge, for the crossing score. This starts as the
	// straight set and is UPDATED as edges are rerouted, so a later edge is
	// scored against where the earlier ones actually ended up. Scoring against
	// the frozen straight set instead made a detour dodge a line that had
	// already moved, and cross the one that replaced it.
	paths := make([][][2]int, len(g.Edges))
	for i := range g.Edges {
		if ok[i] {
			paths[i] = [][2]int{{ends[i].x1, ends[i].y1}, {ends[i].x2, ends[i].y2}}
		}
	}
	rerouted := 0
	for i := range g.Edges {
		if !ok[i] {
			continue
		}
		e := &g.Edges[i]
		blockers := cutBoxes(ends[i].x1, ends[i].y1, ends[i].x2, ends[i].y2,
			obstacles, e.From, e.To)
		if len(blockers) == 0 {
			continue
		}
		best := pickDetour(ends[i].x1, ends[i].y1, ends[i].x2, ends[i].y2,
			blockers, obstacles, e.From, e.To, paths, i)
		if best == nil {
			continue // no clean way round: a bent-but-still-blocked line reads worse
		}
		if e.Route == nil {
			e.Route = &EdgeRouteJSON{
				Source: PortJSON{Side: routes[i].Source.Side, Position: routes[i].Source.Position},
				Target: PortJSON{Side: routes[i].Target.Side, Position: routes[i].Target.Position},
			}
		}
		e.Route.Bends = best
		pts := append([][2]int{{ends[i].x1, ends[i].y1}}, positionsToPts(best)...)
		paths[i] = append(pts, [2]int{ends[i].x2, ends[i].y2})
		rerouted++
	}
	return rerouted
}

// cutBoxes returns the obstacle boxes a straight segment passes through,
// skipping the segment's own endpoints.
func cutBoxes(x1, y1, x2, y2 int, obstacles []Node, fromID, toID string) []Node {
	var out []Node
	for _, n := range obstacles {
		if n.ID == fromID || n.ID == toID {
			continue
		}
		if segmentCutsBox(x1, y1, x2, y2, n) {
			out = append(out, n)
		}
	}
	return out
}

// pickDetour proposes single-bend and two-bend paths around the blocking
// boxes and returns the best clean one, or nil when none is clean.
//
// Candidates are the four corners of the blockers' combined bounding box
// (pushed out by detourClear) as a single waypoint, plus the two L-shapes
// through the endpoints' own axes. That is a small, predictable set: the point
// is to get off the box, not to find an optimal path.
func pickDetour(x1, y1, x2, y2 int, blockers, obstacles []Node, fromID, toID string,
	paths [][][2]int, self int) []Position {

	minX, minY := blockers[0].X, blockers[0].Y
	maxX, maxY := blockers[0].X+blockers[0].Width, blockers[0].Y+blockers[0].Height
	for _, b := range blockers[1:] {
		if b.X < minX {
			minX = b.X
		}
		if b.Y < minY {
			minY = b.Y
		}
		if b.X+b.Width > maxX {
			maxX = b.X + b.Width
		}
		if b.Y+b.Height > maxY {
			maxY = b.Y + b.Height
		}
	}
	// Two offsets from the blockers: the tight one, and a wider lane for when
	// the tight route is clean but tangles with other edges. Both the combined
	// bounding box and each blocker on its own are tried — with several
	// blockers the combined box can be far larger than any real obstruction,
	// and hugging one of them is often the shorter, less crossing way through.
	//
	// The wide lane is 2x, chosen by measurement over the zoom corpus: 3x cut
	// two more crossings but bought them with grazes, which sit in the more
	// severe finding bucket, and 4x was worse on both. Wider lanes also mean
	// longer detours, which read worse even when they score the same.
	boxes := [][4]int{{minX, minY, maxX, maxY}}
	for _, bl := range blockers {
		boxes = append(boxes, [4]int{bl.X, bl.Y, bl.X + bl.Width, bl.Y + bl.Height})
	}
	var candidates [][]Position
	candidates = append(candidates, []Position{{X: x1, Y: y2}}, []Position{{X: x2, Y: y1}})
	for _, bx := range boxes {
		for _, pad := range [2]int{detourClear, detourClear * 2} {
			l, r := bx[0]-pad, bx[2]+pad
			t, b := bx[1]-pad, bx[3]+pad
			candidates = append(candidates,
				[]Position{{X: l, Y: t}}, []Position{{X: r, Y: t}},
				[]Position{{X: l, Y: b}}, []Position{{X: r, Y: b}},
				[]Position{{X: l, Y: y1}, {X: l, Y: y2}},
				[]Position{{X: r, Y: y1}, {X: r, Y: y2}},
				[]Position{{X: x1, Y: t}, {X: x2, Y: t}},
				[]Position{{X: x1, Y: b}, {X: x2, Y: b}},
			)
		}
	}

	bestScore := [3]int{1 << 30, 1 << 30, 1 << 30}
	var best []Position
	consider := func(c []Position) {
		pts := append([][2]int{{x1, y1}}, positionsToPts(c)...)
		pts = append(pts, [2]int{x2, y2})
		if pathCuts(pts, obstacles, fromID, toID) {
			return
		}
		score := [3]int{
			pathGrazes(pts, obstacles, fromID, toID),
			pathCrossings(pts, paths, self),
			pathLength(pts),
		}
		if score[0] < bestScore[0] ||
			(score[0] == bestScore[0] && score[1] < bestScore[1]) ||
			(score[0] == bestScore[0] && score[1] == bestScore[1] && score[2] < bestScore[2]) {
			bestScore, best = score, c
		}
	}
	for _, c := range candidates {
		consider(c)
	}
	if best != nil {
		return best
	}

	// Fallback sweep. The curated set above routes around the BLOCKERS, which
	// is enough in open space but not in a crowded one: the clean way through
	// is often around some third box the blockers' own corners never suggest.
	// On blue-taskman's story_07, 550 edges were left straight and 289 of them
	// had a clean single-bend route the curated set simply never proposed.
	//
	// So when nothing curated is clean, sweep a coarse grid of single
	// waypoints. detourGridStep is one node width, because obstacles are that
	// wide and the gaps between them are what we are trying to find; a finer
	// step buys almost nothing (step 60 recovered 230 of those 289 against
	// step 120's 228, for four times the work). Only edges that are actually
	// blocked AND have no curated route pay for this.
	lo := func(a, b int) int {
		if a < b {
			return a
		}
		return b
	}
	hi := func(a, b int) int {
		if a > b {
			return a
		}
		return b
	}
	gx0, gx1 := lo(x1, x2)-detourGridPad, hi(x1, x2)+detourGridPad
	gy0, gy1 := lo(y1, y2)-detourGridPad, hi(y1, y2)+detourGridPad
	tried := 0
	for wx := gx0; wx <= gx1 && tried < detourGridCap; wx += detourGridStep {
		for wy := gy0; wy <= gy1 && tried < detourGridCap; wy += detourGridStep {
			tried++
			consider([]Position{{X: wx, Y: wy}})
		}
	}
	return best
}

func positionsToPts(ps []Position) [][2]int {
	out := make([][2]int, len(ps))
	for i, p := range ps {
		out[i] = [2]int{p.X, p.Y}
	}
	return out
}

func pathCuts(pts [][2]int, obstacles []Node, fromID, toID string) bool {
	for i := 0; i+1 < len(pts); i++ {
		if len(cutBoxes(pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1],
			obstacles, fromID, toID)) > 0 {
			return true
		}
	}
	return false
}

// pathGrazes counts boxes the path clears but skims within detourGraze.
func pathGrazes(pts [][2]int, obstacles []Node, fromID, toID string) int {
	n := 0
	for _, o := range obstacles {
		if o.ID == fromID || o.ID == toID {
			continue
		}
		fat := o
		fat.X, fat.Y = o.X-detourGraze, o.Y-detourGraze
		fat.Width, fat.Height = o.Width+2*detourGraze, o.Height+2*detourGraze
		for i := 0; i+1 < len(pts); i++ {
			if segmentCutsBox(pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1], fat) {
				n++
				break
			}
		}
	}
	return n
}

func pathCrossings(pts [][2]int, paths [][][2]int, self int) int {
	n := 0
	for j, other := range paths {
		if j == self || len(other) < 2 {
			continue
		}
		if pathsCross(pts, other) {
			n++
		}
	}
	return n
}

func pathsCross(a, b [][2]int) bool {
	for i := 0; i+1 < len(a); i++ {
		for j := 0; j+1 < len(b); j++ {
			if segmentsCross(a[i][0], a[i][1], a[i+1][0], a[i+1][1],
				b[j][0], b[j][1], b[j+1][0], b[j+1][1]) {
				return true
			}
		}
	}
	return false
}

func pathLength(pts [][2]int) int {
	total := 0
	for i := 0; i+1 < len(pts); i++ {
		dx, dy := pts[i+1][0]-pts[i][0], pts[i+1][1]-pts[i][1]
		if dx < 0 {
			dx = -dx
		}
		if dy < 0 {
			dy = -dy
		}
		total += dx + dy
	}
	return total
}
