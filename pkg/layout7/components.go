package layout7

import (
	"math"
	"sort"
)

// assemble implements v7P2 — the most central component first, the rest
// around it, the whole arrangement aiming at a 16:9 canvas.
//
// Centrality sort: cross-component connections desc, declared nodes desc,
// declared edges desc, events desc, declaration order. Untied components
// follow in that order and WRAP toward the 16:9 rectangle: each next
// component extends the current row or starts a new one, whichever leaves
// the bounding box's aspect closer to 16:9.
//
// Tied components RING the already-placed one they tie to, at their
// anchor's height; each member's flank is chosen by the v7P2 priority —
// tie crossings (its straight against the hub's boxes), then 16:9, then
// the side nearest the anchor — so same-anchor siblings STACK on the
// winning side in one column and spread to the next clean flank when
// stacking degenerates. Untied components follow after the placed group.
func (g *graph) assemble() {
	if len(g.comps) == 0 {
		return
	}
	edgeCount := make([]int, len(g.comps))
	for _, e := range g.edges {
		cf, ct := g.nodes[e.from].comp, g.nodes[e.to].comp
		if cf >= 0 && cf == ct {
			edgeCount[cf]++
		}
	}
	order := make([]int, len(g.comps))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ca, cb := g.comps[order[a]], g.comps[order[b]]
		if ca.crossTies != cb.crossTies {
			return ca.crossTies > cb.crossTies
		}
		na, nb := len(ca.events)+len(ca.aux), len(cb.events)+len(cb.aux)
		if na != nb {
			return na > nb
		}
		if edgeCount[order[a]] != edgeCount[order[b]] {
			return edgeCount[order[a]] > edgeCount[order[b]]
		}
		if len(ca.events) != len(cb.events) {
			return len(ca.events) > len(cb.events)
		}
		return order[a] < order[b]
	})

	target := TargetAspectW / TargetAspectH
	aspectDev := func(w, h int) float64 {
		if w <= 0 || h <= 0 {
			return math.Inf(1)
		}
		return math.Abs(math.Log(float64(w) / float64(h) / target))
	}

	offsets := make(map[int][2]int, len(order))
	placedComp := map[int]bool{}
	absBox := func(ci int) (int, int, int, int) {
		c := g.comps[ci]
		off := offsets[ci]
		return c.minX + off[0], c.minY + off[1], c.maxX + off[0], c.maxY + off[1]
	}
	// node-level: a tile keeps its stand-off from the placed comps' BOXES,
	// not their bounding boxes — a bbox test vetoed the row-aware hug
	// whenever an out-of-span row overhung the flank (cX/cY pushed
	// off tD by tB's bbox contribution).
	// the clearance to the tile's OWN hub follows the tile's stand-off
	// (an event-less satellite tile hugs at column rhythm);
	// every other component keeps the full component gap
	overlapsPlaced := func(x0, y0, x1, y1, hub, hubGap int) bool {
		for pi := range placedComp {
			off := offsets[pi]
			gap := CompGap
			if pi == hub {
				gap = hubGap
			}
			for i := range g.nodes {
				n := g.nodes[i]
				if n.comp != pi || !n.placed {
					continue
				}
				nx0, ny0 := n.x+off[0], n.y+off[1]
				if x0 < nx0+n.w+gap && nx0 < x1+gap &&
					y0 < ny0+n.h+gap && ny0 < y1+gap {
					return true
				}
			}
		}
		return false
	}

	// ---- ring pass (v7P2): tied components around their placed anchor. ----
	// ties per component: the first-declared tie to each other component.
	type tieRef struct {
		other        int // the other component
		anchor, self int // node idx: anchor in `other`, self endpoint here
	}
	ties := map[int][]tieRef{}
	for _, e := range g.edges {
		if e.structural {
			continue
		}
		cf, ct := g.nodes[e.from].comp, g.nodes[e.to].comp
		if cf < 0 || ct < 0 || cf == ct {
			continue
		}
		ties[cf] = append(ties[cf], tieRef{other: ct, anchor: e.to, self: e.from})
		ties[ct] = append(ties[ct], tieRef{other: cf, anchor: e.from, self: e.to})
	}

	sideCount := map[[2]int]int{} // (hub comp, side 0..3) -> members placed
	type ringRec struct{ ci, hub, anchor, side int }
	var ringPlaced []ringRec
	var wrapList []int
	for i, ci := range order {
		if i == 0 {
			offsets[ci] = [2]int{0, 0}
			placedComp[ci] = true
			continue
		}
		var tie *tieRef
		for _, tr := range ties[ci] {
			if placedComp[tr.other] {
				tie = &tr
				break
			}
		}
		if tie == nil {
			wrapList = append(wrapList, ci)
			continue
		}
		hub := tie.other
		hx0, hy0, hx1, hy1 := absBox(hub)
		hubOff := offsets[hub]
		anchor := g.nodes[tie.anchor]
		anchorCx := anchor.x + anchor.w/2 + hubOff[0]
		anchorCy := anchor.y + anchor.h/2 + hubOff[1]
		selfN := g.nodes[tie.self]
		c := g.comps[ci]
		w, h := c.maxX-c.minX, c.maxY-c.minY

		// An EVENT-LESS tile is satellite CONTENT, not a separate story
		// (v7P2: tD—cX read too far apart at the
		// full component gap): tied by a placing-kind relation it stands
		// at the attached-column gap, by near-to alone at the near-to
		// stand-off; only event components keep the component gap.
		standOff := CompGap
		hasEvents := false
		for i := range g.nodes {
			if g.nodes[i].comp == ci && g.nodes[i].kind == KindEvent && !g.nodes[i].boundary {
				hasEvents = true
				break
			}
		}
		if !hasEvents {
			standOff = NearGap
			for _, e2 := range g.edges {
				fn, tn := g.nodes[e2.from], g.nodes[e2.to]
				if (fn.comp == ci && tn.comp == hub) || (tn.comp == ci && fn.comp == hub) {
					if e2.rel != RelNearTo {
						standOff = ColGap
						break
					}
				}
			}
		}

		// The side follows v7P2's PRIORITY: fewer tie crossings first, then
		// closer to 16:9, then the side nearest the anchor. Every flank is
		// EVALUATED — position resolved (with the stack slide), the tie's
		// straight tested against the hub's boxes, the resulting bounding
		// box scored — so same-anchor siblings stack on the anchor's side
		// while it stays crossing-free AND the canvas tolerates it, and
		// spread to the next clean flank once stacking degenerates
		// (four components tied to one concept must ring it,
		// not pile into one tall column).
		// the stand-off measures the hub's extent on the TILE'S OWN rows
		// (columns for top/bottom) — not the whole flank, exactly like
		// v7P5's satellite rule (cX/cY stood 260 off tD
		// because tB, three rows up, owned the flank's min-x).
		hubEdge := func(side, lo, hi int) int {
			edge, ok := 0, false
			for i := range g.nodes {
				n := g.nodes[i]
				if n.comp != hub || !n.placed {
					continue
				}
				nx0, ny0 := n.x+hubOff[0], n.y+hubOff[1]
				nx1, ny1 := nx0+n.w, ny0+n.h
				var inSpan bool
				var v int
				switch side {
				case 0:
					inSpan, v = ny0 < hi && lo < ny1, nx0
				case 1:
					inSpan, v = ny0 < hi && lo < ny1, nx1
				case 2:
					inSpan, v = nx0 < hi && lo < nx1, ny0
				default:
					inSpan, v = nx0 < hi && lo < nx1, ny1
				}
				if !inSpan {
					continue
				}
				if !ok || (side == 0 || side == 2) && v < edge || (side == 1 || side == 3) && v > edge {
					edge = v
				}
				ok = true
			}
			if !ok { // nothing on the span — the whole-flank edge
				switch side {
				case 0:
					return hx0
				case 1:
					return hx1
				case 2:
					return hy0
				}
				return hy1
			}
			return edge
		}
		resolveSide := func(side int) (int, int) {
			var offX, offY int
			switch side {
			case 0:
				offY = anchorCy - (selfN.y + selfN.h/2) // tie node at the anchor's height
				offX = hubEdge(0, c.minY+offY, c.minY+offY+h) - standOff - w - c.minX
			case 1:
				offY = anchorCy - (selfN.y + selfN.h/2)
				offX = hubEdge(1, c.minY+offY, c.minY+offY+h) + standOff - c.minX
			case 2:
				offX = anchorCx - (selfN.x + selfN.w/2)
				offY = hubEdge(2, c.minX+offX, c.minX+offX+w) - standOff - h - c.minY
			default:
				offX = anchorCx - (selfN.x + selfN.w/2)
				offY = hubEdge(3, c.minX+offX, c.minX+offX+w) + standOff - c.minY
			}
			// members already on this side: slide along it until JUST
			// clear — grid steps, so a stack lands at one component gap
			for guard := 0; guard < 512; guard++ {
				x0, y0 := c.minX+offX, c.minY+offY
				if !overlapsPlaced(x0, y0, x0+w, y0+h, hub, standOff-GridStep/2) {
					break
				}
				if side <= 1 {
					offY += GridStep
				} else {
					offX += GridStep
				}
			}
			return offX, offY
		}
		px0, py0 := math.MaxInt32, math.MaxInt32
		px1, py1 := math.MinInt32, math.MinInt32
		for pi := range placedComp {
			x0, y0, x1, y1 := absBox(pi)
			px0, py0 = minInt(px0, x0), minInt(py0, y0)
			px1, py1 = maxInt(px1, x1), maxInt(py1, y1)
		}
		nearest := 0
		if anchorCx*2 >= hx0+hx1 {
			nearest = 1
		}
		vertNear := 2
		if anchorCy*2 >= hy0+hy1 {
			vertNear = 3
		}
		bestSide, bestOffX, bestOffY := -1, 0, 0
		bestCross, bestDev := 1<<30, math.Inf(1)
		for _, side := range []int{nearest, 1 - nearest, vertNear, 5 - vertNear} {
			offX, offY := resolveSide(side)
			// the tie line vs the hub's boxes (anchor excluded). The
			// router may SLIDE a straight within the endpoints' overlap
			// (v7P9 slid straights), so the side is scored with the BEST
			// slide — a boundary box on the centre line no longer vetoes
			// the flank the tie can route beside (the
			// onion's sixth component belongs on the second row, not on
			// a third).
			countCross := func(shiftX, shiftY int, boundaries bool) int {
				p0 := [2]int{selfN.x + selfN.w/2 + offX + shiftX, selfN.y + selfN.h/2 + offY + shiftY}
				p1 := [2]int{anchorCx + shiftX, anchorCy + shiftY}
				n2 := 0
				for _, n := range g.nodes {
					if n.comp != hub || !n.placed || n.idx == tie.anchor || n.boundary != boundaries {
						continue
					}
					if segIntersectsBox(p0, p1,
						n.x+hubOff[0]+2, n.y+hubOff[1]+2,
						n.x+n.w+hubOff[0]-2, n.y+n.h+hubOff[1]-2) {
						n2++
					}
				}
				return n2
			}
			// content boxes veto at the centre line; a small S/E BOUNDARY
			// box does not — the router's slid straight passes beside it
			cross := countCross(0, 0, false)
			if side == 2 {
				// reading-direction handicap (v7P2/P3: time reads down): a
				// tied component ABOVE the hub competes with the S cap and
				// reads as a predecessor — the top flank wins only on
				// strictly FEWER crossings, never on aspect alone
				// (the onion fills its second ROW instead)
				cross++
			}
			bnd := countCross(0, 0, true)
			for _, d := range []int{GridStep, 2 * GridStep} {
				if bnd == 0 {
					break
				}
				if side >= 2 { // top/bottom: slide the line horizontally
					bnd = minInt(bnd, minInt(countCross(d, 0, true), countCross(-d, 0, true)))
				} else { // left/right: slide vertically
					bnd = minInt(bnd, minInt(countCross(0, d, true), countCross(0, -d, true)))
				}
			}
			cross += bnd
			ux0, uy0 := minInt(px0, c.minX+offX), minInt(py0, c.minY+offY)
			ux1, uy1 := maxInt(px1, c.maxX+offX), maxInt(py1, c.maxY+offY)
			dev := aspectDev(ux1-ux0, uy1-uy0)
			if g.tracing() {
				g.trace.Emit(TraceEvent{Stage: "assemble", Kind: "tile-candidate", Data: map[string]any{
					"comp": ci, "self": g.traceName(tie.self), "hub": hub,
					"anchor": g.traceName(tie.anchor), "side": [4]string{"left", "right", "top", "bottom"}[side],
					"cross": cross, "aspectDev": dev, "offX": offX, "offY": offY,
				}})
			}
			if cross < bestCross || (cross == bestCross && dev < bestDev-1e-9) {
				bestSide, bestOffX, bestOffY = side, offX, offY
				bestCross, bestDev = cross, dev
			}
		}
		if g.tracing() {
			g.trace.Emit(TraceEvent{Stage: "assemble", Kind: "tile", Data: map[string]any{
				"comp": ci, "self": g.traceName(tie.self), "hub": hub,
				"side":  [4]string{"left", "right", "top", "bottom"}[bestSide],
				"cross": bestCross, "aspectDev": bestDev,
			}})
		}
		offsets[ci] = [2]int{bestOffX, bestOffY}
		placedComp[ci] = true
		sideCount[[2]int{hub, bestSide}]++
		ringPlaced = append(ringPlaced, ringRec{ci: ci, hub: hub, anchor: tie.anchor, side: bestSide})
	}

	// Same-anchor PURE tiles centre on their anchor (v7P2:
	// two near-to thing structures tied to one anchor read
	// SYMMETRIC — one at the row and one dangling below reads lopsided).
	// The group's combined span recentres on the anchor like a band
	// stack on its event row; event tiles keep the stack (their S→E
	// verticality is a timeline).
	{
		type ringKey struct{ hub, anchor, side int }
		groups := map[ringKey][]int{}
		var keys []ringKey
		for _, r := range ringPlaced {
			if len(g.comps[r.ci].events) > 0 {
				continue
			}
			k := ringKey{r.hub, r.anchor, r.side}
			if _, ok := groups[k]; !ok {
				keys = append(keys, k)
			}
			groups[k] = append(groups[k], r.ci)
		}
		sort.Slice(keys, func(a, b int) bool {
			if keys[a].hub != keys[b].hub {
				return keys[a].hub < keys[b].hub
			}
			if keys[a].anchor != keys[b].anchor {
				return keys[a].anchor < keys[b].anchor
			}
			return keys[a].side < keys[b].side
		})
		for _, k := range keys {
			members := groups[k]
			if len(members) < 2 {
				continue
			}
			inGroup := map[int]bool{}
			for _, ci := range members {
				inGroup[ci] = true
			}
			vertical := k.side <= 1 // left/right flanks stack in Y
			lo, hi := 1<<30, -(1 << 30)
			for _, ci := range members {
				c, off := g.comps[ci], offsets[ci]
				if vertical {
					lo, hi = minInt(lo, c.minY+off[1]), maxInt(hi, c.maxY+off[1])
				} else {
					lo, hi = minInt(lo, c.minX+off[0]), maxInt(hi, c.maxX+off[0])
				}
			}
			an := g.nodes[k.anchor]
			hubOff := offsets[k.hub]
			anchorC := an.y + an.h/2 + hubOff[1]
			if !vertical {
				anchorC = an.x + an.w/2 + hubOff[0]
			}
			delta := (anchorC - (lo+hi)/2) / GridStep * GridStep
			// back the shift toward zero until every member clears the
			// placed comps outside the group (gaps only shrink to zero)
			clears := func(d int) bool {
				for _, ci := range members {
					c, off := g.comps[ci], offsets[ci]
					x0, y0 := c.minX+off[0], c.minY+off[1]
					if vertical {
						y0 += d
					} else {
						x0 += d
					}
					x1, y1 := x0+(c.maxX-c.minX), y0+(c.maxY-c.minY)
					for pi := range placedComp {
						if inGroup[pi] {
							continue
						}
						poff := offsets[pi]
						for ni := range g.nodes {
							n := g.nodes[ni]
							if n.comp != pi || !n.placed {
								continue
							}
							nx0, ny0 := n.x+poff[0], n.y+poff[1]
							if x0 < nx0+n.w+CompGap && nx0 < x1+CompGap &&
								y0 < ny0+n.h+CompGap && ny0 < y1+CompGap {
								return false
							}
						}
					}
				}
				return true
			}
			for ; delta != 0; delta -= sign(delta) * GridStep {
				if clears(delta) {
					break
				}
			}
			for _, ci := range members {
				if vertical {
					offsets[ci] = [2]int{offsets[ci][0], offsets[ci][1] + delta}
				} else {
					offsets[ci] = [2]int{offsets[ci][0] + delta, offsets[ci][1]}
				}
			}
		}
	}

	// ---- untied components: the count-ladder wrap after the placed group. ----
	wrapX, wrapTop, groupH := 0, 0, 0
	haveGroup := false
	if len(placedComp) > 0 && len(wrapList) < len(order) {
		minX, minY := math.MaxInt32, math.MaxInt32
		maxX, maxY := math.MinInt32, math.MinInt32
		for pi := range placedComp {
			x0, y0, x1, y1 := absBox(pi)
			minX, minY = minInt(minX, x0), minInt(minY, y0)
			maxX, maxY = maxInt(maxX, x1), maxInt(maxY, y1)
		}
		if len(wrapList) == len(order)-1 && len(placedComp) == 1 {
			// no ring happened — the hub joins the wrap flow as row 1
			wrapList = append([]int{order[0]}, wrapList...)
			delete(offsets, order[0])
		} else if len(wrapList) > 0 {
			wrapX, wrapTop, groupH = maxX, minY, maxY-minY
			haveGroup = true
		}
	}
	// COUNT LADDER (v7P2: "three components next to
	// each other, then a second row at four, a fourth column at seven,
	// a third row at nine — a rectangle canvas by default"): columns
	// start at three, then rows and columns grow ALTERNATELY —
	// 3×1, 3×2, 4×2, 4×3, 5×3, 5×4 … The ladder picks the row count by
	// COMPONENT COUNT (a placed tied group counts as one row-1 tile);
	// rows fill evenly in reading order.
	nTiles := len(wrapList)
	groupTile := 0
	if haveGroup {
		groupTile = 1
		nTiles++
	}
	lc, lr := 3, 1
	for lc*lr < nTiles {
		if lc-lr >= 2 {
			lr++
		} else {
			lc++
		}
	}
	counts := make([]int, lr)
	for ri := range counts {
		counts[ri] = nTiles / lr
		if ri < nTiles%lr {
			counts[ri]++
		}
	}
	counts[0] -= groupTile
	type rowState struct{ x, top, height int }
	row := rowState{x: wrapX, top: wrapTop, height: groupH}
	idx := 0
	for ri, cnt := range counts {
		if ri > 0 {
			row = rowState{top: row.top + row.height + CompGap}
		}
		for k := 0; k < cnt; k++ {
			ci := wrapList[idx]
			idx++
			c := g.comps[ci]
			w := c.maxX - c.minX
			h := c.maxY - c.minY
			x := row.x
			if row.x > 0 {
				x += CompGap
			}
			offsets[ci] = [2]int{x - c.minX, row.top - c.minY}
			row.x = x + w
			if h > row.height {
				row.height = h
			}
		}
	}

	for _, n := range g.nodes {
		if n.comp >= 0 && n.placed {
			off := offsets[n.comp]
			n.x += off[0]
			n.y += off[1]
		}
	}

	// normalize to margins
	minX, minY := math.MaxInt32, math.MaxInt32
	for _, n := range g.nodes {
		if !n.placed {
			continue
		}
		if n.x < minX {
			minX = n.x
		}
		if n.y < minY {
			minY = n.y
		}
	}
	if minX == math.MaxInt32 {
		return
	}
	for _, n := range g.nodes {
		if n.placed {
			n.x += Margin - minX
			n.y += Margin - minY
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func sign(v int) int {
	if v < 0 {
		return -1
	}
	if v > 0 {
		return 1
	}
	return 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
