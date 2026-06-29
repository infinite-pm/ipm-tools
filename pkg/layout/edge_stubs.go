package layout

import (
	"math"
	"sort"
	"strings"
)

// Stub geometry — relocated from the renderer per the layout-owns-geometry
// contract (docs/dev/layout-engine.md "Edge routes in the layout output").
//
// A stubbed edge's code chip hangs just off the border that FACES its
// partner: a short visible stub from that border
// to the chip, a faint ghost line continuing chip→chip, and the far stub
// into the partner's facing border. For a vertically stacked tie the facing
// borders are the two nodes' top/bottom — their closest edges — so the chip
// never reads as starting at a side corner.
//
// placeChip ranks the four borders by how squarely each faces the partner,
// then on the best border LADDERS the position (toward the partner first)
// and the reach until the chip clears every node box, earlier chip and
// visible edge — so a crowded chip steps along its border to dodge an
// obstacle (e.g. an S boundary between the two nodes) rather than fleeing to
// a side. The chosen port writes back to the route so the emitted geometry
// matches what was drawn.

// EdgeStubs is one stubbed edge's pair of rendered polylines.
type EdgeStubs struct {
	Source []Position
	Target []Position
}

const (
	// stubChipHalf is half the rendered chip box plus a small margin — the
	// clearance footprint used when laddering chips off boxes and siblings.
	stubChipHalf = 10.0
	// stubBaseReach is the preferred port→chip distance in open space:
	// 22px ("chip 2 next to application is too far — should be 25%
	// less") — a faint code chip just off the border, not a long
	// leader line; the renderer holds the stub's arrowhead in a 5px
	// gap off the border.
	stubBaseReach = 22.0
	// stubReachStep is the ladder increment when the chip spot is taken.
	stubReachStep = 12.0
	// chipNodeGap is the minimum clearance a code chip keeps from any node
	// that is NOT one of its own stubbed edge's endpoints (a
	// chip must never hug a foreign node). At least the chip's own reach off
	// its node (a chip closer to a foreign node than to its
	// own reads as connected to the foreign one — services' chip hung 10px
	// under Service API while sitting 30px off services). Applied on top of
	// stubChipHalf.
	chipNodeGap = stubBaseReach
)

// ComputeEdgeStubs returns the stub polylines of every edge classified
// "stubbed" under the canvas policy, keyed by edge index.
func ComputeEdgeStubs(g *Graph, routes []EdgeRoute) map[int]EdgeStubs {
	// Stub set is the authoritative Edge.Visibility (stamped by
	// stampEdgeVisibility on real routed geometry) — NOT a re-classification,
	// so the chips render for exactly the edges the layout hid.
	stubbed := func(i int) bool { return g.Edges[i].Visibility == visibilityStubbed }
	nodeByID := make(map[string]Node, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeByID[n.ID] = n
	}

	// Visible edges' polylines: a chip must not sit ON a normal edge
	// ("normal edge should not collide with hidden"); the
	// node-and-chip checks alone let a chip land on a passing line.
	visSegs := make([][4]int, 0, len(g.Edges))
	for i, e := range g.Edges {
		if i >= len(routes) || stubbed(i) {
			continue
		}
		from, fok := nodeByID[e.From]
		to, tok := nodeByID[e.To]
		if !fok || !tok {
			continue
		}
		sx, sy := EdgePortPoint(from, to, routes[i].Source)
		tx, ty := EdgePortPoint(to, from, routes[i].Target)
		pts := make([]Position, 0, len(routes[i].Bends)+2)
		pts = append(pts, Position{X: sx, Y: sy})
		pts = append(pts, routes[i].Bends...)
		pts = append(pts, Position{X: tx, Y: ty})
		for s := 0; s+1 < len(pts); s++ {
			visSegs = append(visSegs, [4]int{pts[s].X, pts[s].Y, pts[s+1].X, pts[s+1].Y})
		}
	}

	// Chips placed so far (all edges) — later chips ladder past them.
	placed := make([][2]float64, 0, 8)
	// ownID/otherID are the stubbed edge's own endpoints — a chip legitimately
	// hangs just off its own node's border and points at its partner, so the
	// foreign-node clearance (chipNodeGap) must NOT apply to those two; only the
	// zero-gap on-node test does. Every other node keeps chipNodeGap.
	chipFree := func(cx, cy float64, ownID, otherID string) bool {
		for _, n := range g.Nodes {
			if n.Container != nil && strings.HasSuffix(n.RenderKind, "-container") {
				continue
			}
			gap := chipNodeGap
			if n.ID == ownID || n.ID == otherID {
				gap = 0
			}
			if cx+stubChipHalf+gap > float64(n.X) && float64(n.X+n.Width)+gap > cx-stubChipHalf &&
				cy+stubChipHalf+gap > float64(n.Y) && float64(n.Y+n.Height)+gap > cy-stubChipHalf {
				return false
			}
		}
		for _, p := range placed {
			if math.Abs(p[0]-cx) < 2*stubChipHalf && math.Abs(p[1]-cy) < 2*stubChipHalf {
				return false
			}
		}
		chipBox := Node{X: int(cx - stubChipHalf), Y: int(cy - stubChipHalf), Width: 2 * stubChipHalf, Height: 2 * stubChipHalf}
		for _, s := range visSegs {
			if segmentCutsBox(s[0], s[1], s[2], s[3], chipBox) {
				return false
			}
		}
		return true
	}
	// stubSegClear keeps the SHORT visible stub line (border port → chip) clear
	// of every visible edge — chipFree only tests the chip BOX, so a stub whose
	// LINE crosses (or hugs the landing port of) a passing edge slipped through.
	// The ladder then steps the chip along its border / out its reach until the
	// stub itself is clear (tA→e3's chip stub cut across S→e3;
	// nudging the chip right of S→e3's e3-port clears it).
	stubSegClear := func(bx, by, cx, cy float64) bool {
		for _, s := range visSegs {
			if segSegDist(bx, by, cx, cy, float64(s[0]), float64(s[1]), float64(s[2]), float64(s[3])) < stubChipHalf {
				return false
			}
		}
		return true
	}
	outward := func(side string) (float64, float64) {
		switch side {
		case "left":
			return -1, 0
		case "right":
			return 1, 0
		case "top":
			return 0, -1
		}
		return 0, 1 // bottom
	}
	// placeChip hangs an end's chip just off the border that FACES the partner
	// ("the closest borders of e1 and a1 are bottom of e1 and
	// top of a1 — these can be used; now it seems the line starts at a
	// corner"). The previous chord-based chip rode the side-distribution port
	// — which for vertically-stacked ties sat on the side, reading as a corner
	// start. Now: rank the four borders by how squarely their outward normal
	// faces the partner, and on the best available border ladder the position
	// (toward the partner first) and the reach until the chip clears boxes,
	// other chips and visible edges — so a chip steps along the top border to
	// dodge an obstacle (the S boundary between a1 and e1) instead of fleeing
	// to the side. The chosen port writes back so the emitted route matches.
	// segmentCutsForeignNode reports whether the segment (x1,y1)-(x2,y2) passes
	// through a node box other than ownID/otherID — used to keep a stubbed
	// edge's HIDDEN body (the chip→chip ghost line) from reading as piercing a
	// node.
	segmentCutsForeignNode := func(x1, y1, x2, y2 float64, ownID, otherID string) bool {
		for _, n := range g.Nodes {
			if n.ID == ownID || n.ID == otherID {
				continue
			}
			if n.Container != nil && strings.HasSuffix(n.RenderKind, "-container") {
				continue
			}
			if segmentCutsBox(int(math.Round(x1)), int(math.Round(y1)), int(math.Round(x2)), int(math.Round(y2)), n) {
				return true
			}
		}
		return false
	}
	// restrict (when non-nil) limits placeChip to the named border sides — used
	// by the symmetric-side retry below.
	placeChip := func(node, other Node, port *EdgePort, align *Position, restrict map[string]bool) (float64, float64, float64, float64) {
		ncx := float64(node.X) + float64(node.Width)/2
		ncy := float64(node.Y) + float64(node.Height)/2
		ocx := float64(other.X) + float64(other.Width)/2
		ocy := float64(other.Y) + float64(other.Height)/2
		ux, uy := ocx-ncx, ocy-ncy
		if d := math.Hypot(ux, uy); d > 0 {
			ux, uy = ux/d, uy/d
		}
		type sideCand struct {
			side string
			dot  float64
		}
		cands := make([]sideCand, 0, 4)
		for _, side := range []string{"top", "bottom", "left", "right"} {
			ox, oy := outward(side)
			cands = append(cands, sideCand{side, ox*ux + oy*uy})
		}
		sort.SliceStable(cands, func(a, b int) bool { return cands[a].dot > cands[b].dot })
		// Position candidates along a side, ordered by preference. When the
		// partner end's chip is already placed (align != nil), the preferred
		// spot is the one that lines the two chips UP — a straight ghost line,
		// not a slight lean (the two chips of a stacked tie
		// should share an x). Otherwise the preference is the partner's
		// projection, so a first chip sits beside its obstacle without
		// drifting to a far corner.
		positionsToward := func(side string) []float64 {
			pref := 0.5
			switch side {
			case "top", "bottom":
				if node.Width > 0 {
					if align != nil {
						pref = (float64(align.X) - float64(node.X)) / float64(node.Width)
					} else {
						pref = (ocx - float64(node.X)) / float64(node.Width)
					}
				}
			default:
				if node.Height > 0 {
					if align != nil {
						pref = (float64(align.Y) - float64(node.Y)) / float64(node.Height)
					} else {
						pref = (ocy - float64(node.Y)) / float64(node.Height)
					}
				}
			}
			base := []float64{0.5, 0.32, 0.68, 0.16, 0.84}
			sort.SliceStable(base, func(a, b int) bool {
				return math.Abs(base[a]-pref) < math.Abs(base[b]-pref)
			})
			return base
		}
		reachLadder := []float64{stubBaseReach, stubBaseReach + stubReachStep, stubBaseReach + 2*stubReachStep, stubBaseReach + 3*stubReachStep}
		// Two ladder rounds: the partner-facing sides first, then — before
		// any squeezed fallback — the away sides too: a clean full-reach
		// chip on the node's free back beats a chip crushed into a tight
		// stack gap with no visible stub (workload
		// resource's chip read "too short, or not existent" while its
		// canvas-facing side sat empty).
		for round := 0; round < 2; round++ {
			for ci, c := range cands {
				if restrict != nil && !restrict[c.side] {
					continue
				}
				// Round 0 skips a side facing away from the partner unless it
				// is the only option left (keeps a chip off the node's back).
				// A RESTRICTED call never skips: the caller explicitly demands
				// this side, and skipping it meant the ladder never ran at all
				// and the blind fallback placed the chip on top of whatever
				// was there (gw's chip landed on the S→gw
				// leads-to). Round 1 visits exactly the skipped sides.
				skipped := restrict == nil && c.dot < 0 && ci < len(cands)-1
				if (round == 0) == skipped {
					continue
				}
				ox, oy := outward(c.side)
				for _, pos := range positionsToward(c.side) {
					bxI, byI := EdgePortPoint(node, other, EdgePort{Side: c.side, Position: pos})
					bx, by := float64(bxI), float64(byI)
					for _, reach := range reachLadder {
						cx, cy := bx+ox*reach, by+oy*reach
						if chipFree(cx, cy, node.ID, other.ID) && stubSegClear(bx, by, cx, cy) {
							*port = EdgePort{Side: c.side, Position: pos}
							placed = append(placed, [2]float64{cx, cy})
							return bx, by, cx, cy
						}
					}
				}
			}
		}
		// Everything taken: no candidate is fully clear, so take the LEAST-BAD
		// one instead of a blind unchecked spot — score every candidate by what
		// it violates (foreign-node proximity worst, then a visible edge cutting
		// the chip box, then the stub line hugging an edge, then chip-chip) and
		// keep the first minimum in preference order (side facing, position
		// preference, shortest reach).
		penalty := func(bx, by, cx, cy float64) float64 {
			p := 0.0
			for _, n := range g.Nodes {
				if n.ID == node.ID || n.ID == other.ID {
					continue
				}
				if n.Container != nil && strings.HasSuffix(n.RenderKind, "-container") {
					continue
				}
				overX := math.Min(cx+stubChipHalf+chipNodeGap, float64(n.X+n.Width)) - math.Max(cx-stubChipHalf-chipNodeGap, float64(n.X))
				overY := math.Min(cy+stubChipHalf+chipNodeGap, float64(n.Y+n.Height)) - math.Max(cy-stubChipHalf-chipNodeGap, float64(n.Y))
				if overX > 0 && overY > 0 {
					p += 1000 + overX*overY
				}
			}
			chipBox := Node{X: int(cx - stubChipHalf), Y: int(cy - stubChipHalf), Width: 2 * stubChipHalf, Height: 2 * stubChipHalf}
			for _, sgm := range visSegs {
				if segmentCutsBox(sgm[0], sgm[1], sgm[2], sgm[3], chipBox) {
					p += 500
				}
				if d := segSegDist(bx, by, cx, cy, float64(sgm[0]), float64(sgm[1]), float64(sgm[2]), float64(sgm[3])); d < stubChipHalf {
					p += 200 + (stubChipHalf - d)
				}
			}
			for _, pc := range placed {
				if math.Abs(pc[0]-cx) < 2*stubChipHalf && math.Abs(pc[1]-cy) < 2*stubChipHalf {
					p += 300
				}
			}
			return p
		}
		bestPen := math.Inf(1)
		var bBx, bBy, bCx, bCy float64
		var bPort EdgePort
		// The fallback also tries a SHORTER reach: a chip squeezed into a
		// narrow gap between its node and a foreign one centres in the gap
		// (own distance == foreign clearance) instead of hugging the foreign
		// box — the stub line still anchors it to its own node
		// (services' chip in the 40px gap under Service API).
		fallbackReaches := append([]float64{stubBaseReach * 2.0 / 3.0}, reachLadder...)
		for _, c := range cands {
			if restrict != nil && !restrict[c.side] {
				continue
			}
			ox, oy := outward(c.side)
			for _, pos := range positionsToward(c.side) {
				bxI, byI := EdgePortPoint(node, other, EdgePort{Side: c.side, Position: pos})
				bx, by := float64(bxI), float64(byI)
				for _, reach := range fallbackReaches {
					cx, cy := bx+ox*reach, by+oy*reach
					if pen := penalty(bx, by, cx, cy); pen < bestPen {
						bestPen = pen
						bBx, bBy, bCx, bCy = bx, by, cx, cy
						bPort = EdgePort{Side: c.side, Position: pos}
					}
				}
			}
		}
		*port = bPort
		placed = append(placed, [2]float64{bCx, bCy})
		return bBx, bBy, bCx, bCy
	}

	out := map[int]EdgeStubs{}
	for i := range g.Edges {
		e := g.Edges[i]
		if i >= len(routes) || !stubbed(i) {
			continue
		}
		from, fok := nodeByID[e.From]
		to, tok := nodeByID[e.To]
		if !fok || !tok {
			continue
		}
		placedMark := len(placed)
		sPx, sPy, sBadgeX, sBadgeY := placeChip(from, to, &routes[i].Source, nil, nil)
		// The target chip aligns to the source chip (already placed), so the
		// ghost line between them runs straight rather than leaning.
		srcChip := Position{X: int(math.Round(sBadgeX)), Y: int(math.Round(sBadgeY))}
		tPx, tPy, tBadgeX, tBadgeY := placeChip(to, from, &routes[i].Target, &srcChip, nil)

		// Symmetric clear-side retry: if the straight ghost
		// between the two chips pierces a foreign node, re-place BOTH chips on a
		// shared SIDE border (left or right) so the hidden body runs in a clear
		// lane beside the obstacle — source-left ⇒ target-left, etc. Strictly
		// non-regressive: only adopt when the new ghost clears AND the original
		// did not, otherwise restore the per-edge placement.
		if segmentCutsForeignNode(sBadgeX, sBadgeY, tBadgeX, tBadgeY, e.From, e.To) {
			origSrc, origTgt := routes[i].Source, routes[i].Target
			origPlaced := append([][2]float64(nil), placed[placedMark:]...)
			ov := [8]float64{sPx, sPy, sBadgeX, sBadgeY, tPx, tPy, tBadgeX, tBadgeY}
			adopted := false
			// Only sides PERPENDICULAR to the tie can host a symmetric clear
			// lane: a side facing toward one end faces away from the other (the
			// ends are opposite), so a vertical tie clears on left/right, a
			// horizontal tie on top/bottom, and a steep diagonal on neither
			// (keep the per-edge placement then). This also keeps a chip off the
			// BACK of its node.
			fcx := float64(from.X+from.Width/2) - float64(to.X+to.Width/2)
			fcy := float64(from.Y+from.Height/2) - float64(to.Y+to.Height/2)
			if d := math.Hypot(fcx, fcy); d > 0 {
				fcx, fcy = fcx/d, fcy/d
			}
			sideOrder := make([]string, 0, 2)
			for _, s := range []string{"left", "right", "top", "bottom"} {
				if ox, oy := outward(s); math.Abs(ox*fcx+oy*fcy) <= 0.5 {
					sideOrder = append(sideOrder, s)
				}
			}
			for _, side := range sideOrder {
				placed = placed[:placedMark]
				restrict := map[string]bool{side: true}
				var sp, tp EdgePort
				s2Px, s2Py, s2Bx, s2By := placeChip(from, to, &sp, nil, restrict)
				chip2 := Position{X: int(math.Round(s2Bx)), Y: int(math.Round(s2By))}
				t2Px, t2Py, t2Bx, t2By := placeChip(to, from, &tp, &chip2, restrict)
				if !segmentCutsForeignNode(s2Bx, s2By, t2Bx, t2By, e.From, e.To) {
					routes[i].Source, routes[i].Target = sp, tp
					sPx, sPy, sBadgeX, sBadgeY = s2Px, s2Py, s2Bx, s2By
					tPx, tPy, tBadgeX, tBadgeY = t2Px, t2Py, t2Bx, t2By
					adopted = true
					break
				}
			}
			if !adopted {
				placed = append(placed[:placedMark], origPlaced...)
				routes[i].Source, routes[i].Target = origSrc, origTgt
				sPx, sPy, sBadgeX, sBadgeY = ov[0], ov[1], ov[2], ov[3]
				tPx, tPy, tBadgeX, tBadgeY = ov[4], ov[5], ov[6], ov[7]
			}
		}

		out[i] = EdgeStubs{
			Source: []Position{{X: int(math.Round(sPx)), Y: int(math.Round(sPy))}, {X: int(math.Round(sBadgeX)), Y: int(math.Round(sBadgeY))}},
			Target: []Position{{X: int(math.Round(tPx)), Y: int(math.Round(tPy))}, {X: int(math.Round(tBadgeX)), Y: int(math.Round(tBadgeY))}},
		}
	}

	// Type-separated fan: where several stub chips share one
	// (node, side) the per-edge placement could interleave near-to and
	// expresses and crowd them. Re-lay each such group: order chips by TYPE
	// (so a kind keeps its own contiguous run/stack), then by partner
	// direction (so the ghost lines don't cross), and fan them across the
	// border — each chip its own outward stub at its own position, adjacent
	// chips staggered in reach so neither a chip nor a further chip's stub
	// sits on a nearer one. Reach ladders further only to clear a box or a
	// visible edge.
	type endRef struct {
		ei     int
		src    bool
		base   string
		pcoord float64 // partner center along the border's tangent axis
	}
	groups := map[string][]endRef{}
	for i := range g.Edges {
		if i >= len(routes) || !stubbed(i) {
			continue
		}
		from := nodeByID[g.Edges[i].From]
		to := nodeByID[g.Edges[i].To]
		add := func(node, partner Node, side string, src bool) {
			pc := float64(partner.X) + float64(partner.Width)/2
			if side == "left" || side == "right" {
				pc = float64(partner.Y) + float64(partner.Height)/2
			}
			groups[node.ID+"|"+side] = append(groups[node.ID+"|"+side], endRef{i, src, g.Edges[i].Base, pc})
		}
		add(from, to, routes[i].Source.Side, true)
		add(to, from, routes[i].Target.Side, false)
	}
	// chipClearOfFixtures: like chipFree but ignores other CHIPS (the fan
	// spaces them itself) — only boxes and visible edges must be dodged. Like
	// chipFree it keeps chipNodeGap from every node except the chip's own edge
	// endpoints (ownID/otherID); without this the type-separated fan re-lay
	// silently dropped the foreign-node clearance the per-edge pass enforces.
	chipClearOfFixtures := func(cx, cy float64, ownID, otherID string) bool {
		for _, n := range g.Nodes {
			if n.Container != nil && strings.HasSuffix(n.RenderKind, "-container") {
				continue
			}
			gap := chipNodeGap
			if n.ID == ownID || n.ID == otherID {
				gap = 0
			}
			if cx+stubChipHalf+gap > float64(n.X) && float64(n.X+n.Width)+gap > cx-stubChipHalf &&
				cy+stubChipHalf+gap > float64(n.Y) && float64(n.Y+n.Height)+gap > cy-stubChipHalf {
				return false
			}
		}
		chipBox := Node{X: int(cx - stubChipHalf), Y: int(cy - stubChipHalf), Width: 2 * stubChipHalf, Height: 2 * stubChipHalf}
		for _, s := range visSegs {
			if segmentCutsBox(s[0], s[1], s[2], s[3], chipBox) {
				return false
			}
		}
		return true
	}
	typeRank := func(b string) int {
		switch b {
		case "expresses":
			return 0
		case "nearto":
			return 1
		}
		return 2
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		members := groups[key]
		if len(members) < 2 {
			continue
		}
		cut := strings.LastIndex(key, "|")
		node := nodeByID[key[:cut]]
		side := key[cut+1:]
		ox, oy := outward(side)
		sort.SliceStable(members, func(a, b int) bool {
			if ra, rb := typeRank(members[a].base), typeRank(members[b].base); ra != rb {
				return ra < rb
			}
			return members[a].pcoord < members[b].pcoord
		})
		n := len(members)
		localPlaced := make([][2]float64, 0, n)
		notNearLocal := func(cx, cy float64) bool {
			for _, p := range localPlaced {
				if math.Abs(p[0]-cx) < 2*stubChipHalf && math.Abs(p[1]-cy) < 2*stubChipHalf {
					return false
				}
			}
			return true
		}
		// Span the border ends (not bunched at centre) so the chips separate
		// as much as a short border allows.
		const fanLo, fanHi = 0.15, 0.85
		for k, m := range members {
			pos := fanLo + (fanHi-fanLo)*float64(k)/float64(n-1)
			partner := nodeByID[g.Edges[m.ei].To]
			if !m.src {
				partner = nodeByID[g.Edges[m.ei].From]
			}
			bxI, byI := EdgePortPoint(node, partner, EdgePort{Side: side, Position: pos})
			bx, by := float64(bxI), float64(byI)
			// Adjacent chips stagger their starting reach so they separate even
			// on a short border; the reach then ladders out until the chip
			// clears boxes, visible edges and the group's earlier chips. A spot
			// is taken ONLY when genuinely clear — if none is found the chip
			// keeps its obstacle-aware per-edge placement (never forced onto a
			// box), so the fan can only improve, never regress.
			startReach := stubBaseReach
			var cx, cy float64
			found := false
			for step := 0; step < 6; step++ {
				r := startReach + float64(step)*stubReachStep
				tx, ty := bx+ox*r, by+oy*r
				if chipClearOfFixtures(tx, ty, node.ID, partner.ID) && notNearLocal(tx, ty) {
					cx, cy, found = tx, ty, true
					break
				}
			}
			if !found {
				// Keep the per-edge placement; record its chip so later
				// members in this group still avoid it.
				st := out[m.ei]
				if m.src && len(st.Source) == 2 {
					localPlaced = append(localPlaced, [2]float64{float64(st.Source[1].X), float64(st.Source[1].Y)})
				} else if !m.src && len(st.Target) == 2 {
					localPlaced = append(localPlaced, [2]float64{float64(st.Target[1].X), float64(st.Target[1].Y)})
				}
				continue
			}
			localPlaced = append(localPlaced, [2]float64{cx, cy})
			port := EdgePort{Side: side, Position: pos}
			st := out[m.ei]
			pair := []Position{{X: int(math.Round(bx)), Y: int(math.Round(by))}, {X: int(math.Round(cx)), Y: int(math.Round(cy))}}
			if m.src {
				routes[m.ei].Source = port
				st.Source = pair
			} else {
				routes[m.ei].Target = port
				st.Target = pair
			}
			out[m.ei] = st
		}
	}
	return out
}

// ptSegDist is the distance from point (px,py) to segment (ax,ay)-(bx,by).
func ptSegDist(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	if dx == 0 && dy == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

// segSegDist is the minimum distance between segments (ax,ay)-(bx,by) and
// (cx,cy)-(dx,dy); 0 if they cross. Used to keep a stub line clear of (and not
// hugging the shared port of) a visible edge.
func segSegDist(ax, ay, bx, by, cx, cy, dx, dy float64) float64 {
	if segmentsCross(int(ax), int(ay), int(bx), int(by), int(cx), int(cy), int(dx), int(dy)) {
		return 0
	}
	return math.Min(
		math.Min(ptSegDist(ax, ay, cx, cy, dx, dy), ptSegDist(bx, by, cx, cy, dx, dy)),
		math.Min(ptSegDist(cx, cy, ax, ay, bx, by), ptSegDist(dx, dy, ax, ay, bx, by)),
	)
}
