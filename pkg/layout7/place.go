package layout7

import (
	"math"
	"sort"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// place computes absolute component-local coordinates — the LAST step of the
// relative pipeline (spec Algorithm, stage 4) — and creates the S/E
// boundaries (v7P1). X first (rows resolve as ONE separation problem each,
// v7P8), then Y (rows with symmetric growth: space yields, the skeleton
// never does — v7P6), then aux by their relative offsets (v7P4/P5).
func (g *graph) place(m *membership, gp *groupsPlan, sp *skeletonPlan) {
	for ci, comp := range g.comps {
		rows := sp.rows[ci]

		// ---- X: per-row separation solve (v7P8). ----
		// desiredNow preserves the skeleton's fork offsets around the
		// SOLVED predecessor positions, so chains stay vertical and joins
		// stay centred (v7P3) even when an earlier row had to spread.
		solvedC := map[int]int{} // event -> solved center
		for r := 0; r < len(rows); r++ {
			row := rows[r]
			if len(row) == 0 {
				continue
			}
			desired := make(map[int]float64, len(row))
			for _, ev := range row {
				d := float64(sp.desiredX[ev])
				var preds []int
				for _, e := range g.in[ev] {
					if e.rel == RelLeadsTo {
						if _, ok := solvedC[e.from]; ok {
							preds = append(preds, e.from)
						}
					}
				}
				if len(preds) > 0 {
					sumSolved, sumSkel := 0, 0
					for _, p := range preds {
						sumSolved += solvedC[p]
						sumSkel += sp.desiredX[p]
					}
					d += float64(sumSolved-sumSkel) / float64(len(preds))
				}
				desired[ev] = d
			}
			ordered := append([]int(nil), row...)
			sort.SliceStable(ordered, func(a, b int) bool {
				return desired[ordered[a]] < desired[ordered[b]]
			})
			vars := make([]layout.VPSCVar, len(ordered))
			for i, ev := range ordered {
				vars[i] = layout.VPSCVar{
					Desired: desired[ev] - float64(g.nodes[ev].w)/2,
					Weight:  1,
				}
			}
			var cons []layout.VPSCConstraint
			for i := 0; i+1 < len(ordered); i++ {
				a, b := ordered[i], ordered[i+1]
				gap := g.nodes[a].w + gp.rightExt[a] + g.subColumnWidth(a, sp, gp) +
					ColGap + gp.leftExt[b]
				cons = append(cons, layout.VPSCConstraint{Left: i, Right: i + 1, Gap: float64(gap)})
			}
			solved := layout.SolveSeparations(vars, cons)
			for i, ev := range ordered {
				x := int(math.Round(solved[i]))
				g.nodes[ev].x = x
				solvedC[ev] = x + g.nodes[ev].w/2
			}
		}

		// ---- sub-events: part-of indents right (v7P3), nested; the GRID's
		// middle sits on the composite's centre. The sub-structure is a
		// skeleton recursively: rank rows over the siblings' own leads-to —
		// a chain is one column, a FORK spreads its branches side by side
		// on one row (the Pod-lifecycle phases), a join
		// rejoins below. Rows left-align on the column axis and grow
		// rightward, keeping the parent's ePe corridor clean. Invoked per
		// rank inside the Y pass so later rows can grow around the grids.
		var placeSubs func(parent int)
		placeSubs = func(parent int) {
			rows := sp.subRows[parent]
			if len(rows) == 0 {
				return
			}
			pn := g.nodes[parent]
			axis := pn.x + pn.w + gp.rightExt[parent] + ColGap
			colPad := 0
			for _, s := range sp.subStacks[parent] {
				if gp.leftExt[s] > colPad {
					colPad = gp.leftExt[s] // the GRID reserves the widest flank once
				}
			}
			axis += colPad
			rowH, rowGapAt := g.subGridRowMetrics(parent, sp, gp)
			total := 0
			for ri := range rows {
				if ri > 0 {
					total += rowGapAt[ri]
				}
				total += rowH[ri]
			}
			// rows CENTRE on the grid's midline (v7P3: forks spread
			// symmetrically — the fork parent sits over its branches'
			// midpoint, the join lands back under it; a plain column's
			// equal rows keep one line). The midline is measured over the
			// BOXES only — a hanging nested grid or a side band trails
			// off the right without pushing its owner off the corridor.
			// Grid-snapped per v7P8.
			rowW := make([]int, len(rows))
			gridW := 0
			for ri, row := range rows {
				w := 0
				for i, s := range row {
					if i > 0 {
						w += ColGap + gp.leftExt[s]
					}
					w += g.nodes[s].w
				}
				rowW[ri] = w
				if w > gridW {
					gridW = w
				}
			}
			sy := pn.y + pn.h/2 - total/2
			for ri, row := range rows {
				if ri > 0 {
					sy += rowGapAt[ri] - RowGap // the hang-grown share
				}
				off := ((gridW-rowW[ri])/2 + GridStep/2) / GridStep * GridStep
				x := axis + off
				for i, s := range row {
					sn := g.nodes[s]
					if i > 0 {
						x += gp.leftExt[s]
					}
					sn.x = x
					sn.y = sy
					sn.placed = true
					placeSubs(s)
					x += sn.w + gp.rightExt[s] + g.subColumnWidth(s, sp, gp) + ColGap
				}
				sy += rowH[ri] + RowGap
			}
		}

		// ---- Y: per-lane rhythm (v7P3: "each branch owns a vertical LANE:
		// it grows downward inside it, uneven depths and all") — every event
		// sits one gap below its own predecessors' bottoms; fork siblings
		// share their row (same parent bottom); a join takes the deepest
		// predecessor. Gaps are minimums grown per-box where what hangs
		// BELOW a predecessor's neighbourhood meets what rises ABOVE this
		// event (v7P6/P8) — a side stack may rise beside another lane
		// without inflating it. ----
		xOverlap := func(a0, a1, b0, b1 int) bool { return a0 < b1 && b0 < a1 }
		type hangBox struct{ x0, x1, over int }
		boxesOf := func(ev int, above bool) []hangBox {
			en := g.nodes[ev]
			out := []hangBox{{en.x, en.x + en.w, 0}}
			for n, rp := range gp.rel {
				if rp.event != ev {
					continue
				}
				nb := g.nodes[n]
				over := 0
				if above {
					over = -rp.dy
				} else {
					over = rp.dy + nb.h - en.h
				}
				if over < 0 {
					over = 0
				}
				// en.x is solved; nb.x is not yet (aux places later)
				bx := en.x + rp.dx
				out = append(out, hangBox{bx, bx + nb.w, over})
			}
			// the sub-event column hangs off the composite too (v7P3);
			// subs are placed as soon as their composite's y is known.
			var walkSubs func(parent int)
			walkSubs = func(parent int) {
				for _, sub := range sp.subStacks[parent] {
					sn := g.nodes[sub]
					if !sn.placed {
						continue
					}
					over := 0
					if above {
						over = en.y - sn.y
					} else {
						over = sn.y + sn.h - (en.y + en.h)
					}
					if over < 0 {
						over = 0
					}
					out = append(out, hangBox{sn.x - gp.leftExt[sub], sn.x + sn.w + gp.rightExt[sub], over})
					walkSubs(sub)
				}
			}
			walkSubs(ev)
			return out
		}
		for r := 0; r < len(rows); r++ {
			for _, ev := range rows[r] {
				var preds []int
				for _, e := range g.in[ev] {
					if e.rel == RelLeadsTo && g.nodes[e.from].kind == KindEvent &&
						!g.nodes[e.from].boundary && // a stale S from the
						// demand re-solve is not a predecessor (v7P8 §4)
						g.nodes[e.from].placed {
						preds = append(preds, e.from)
					}
				}
				y := BoundarySize + BoundaryGap // below the S row
				if len(preds) > 0 {
					aboveB := boxesOf(ev, true)
					y = 0
					evCx := g.nodes[ev].x + g.nodes[ev].w/2
					for _, p := range preds {
						gap := RowGap
						if d := sp.extraDrop[p]; d > 0 {
							gap = RowGap + d
						}
						// fan-IN cap (v7P3/P8, unified with the boundary
						// fan): a wide join drops until its flattest
						// incoming edge is back inside the 150° fan —
						// dy >= dx / tan(75°).
						if len(preds) > 1 {
							dx := g.nodes[p].x + g.nodes[p].w/2 - evCx
							if dx < 0 {
								dx = -dx
							}
							if need := gridUp(int(math.Ceil(float64(dx) / 3.7320508))); need > gap {
								gap = need
							}
						}
						for _, a := range boxesOf(p, false) {
							for _, b := range aboveB {
								if a.over == 0 && b.over == 0 {
									continue
								}
								if !xOverlap(a.x0, a.x1, b.x0, b.x1) {
									continue
								}
								// what hangs below keeps the FULL row gap to
								// the next lane row (v7P8 proven constant)
								if need := a.over + RowGap + b.over; need > gap {
									gap = need
								}
							}
						}
						// Container reservation (Options.Containers): a composite's
						// sub-grid claims its vertical band EXCLUSIVELY, so the
						// spine neighbour clears the grid's whole span instead of
						// tucking beside it. Unlike the hang-box growth above this
						// is NOT gated on x-overlap — the grid hangs in its own
						// column, which is exactly why the flat layout lets a
						// neighbour sit level with it, and why a bbox shell around
						// {composite ∪ subtree} then encloses that neighbour.
						if need := g.subGridOverhang(p, sp, gp) + RowGap +
							g.subGridOverhang(ev, sp, gp); need > gap {
							gap = need
						}
						// demand-pass growth below the pred (v7P8 §4; a stranded
						// sole leaf posted it so the part-of diagonal drops under
						// the leaf's near-anchor wedge)
						if d := g.rowExtra[p]; d > 0 {
							gap += d
						}
						if bot := g.nodes[p].y + g.nodes[p].h + gap; bot > y {
							y = bot
						}
					}
				}
				g.nodes[ev].y = y
				g.nodes[ev].placed = true
			}
			placedRank := map[int]bool{}
			for _, ev := range rows[r] {
				placedRank[ev] = true
			}
			// fork siblings share one ROW below their predecessor (v7P3);
			// when one sibling's lane had to grow, the whole fan drops
			// alike (v7P6's symmetric growth, vertically).
			for pass := 0; pass < 2; pass++ {
				byParent := map[int][]int{}
				for _, ev := range rows[r] {
					for _, e := range g.in[ev] {
						if e.rel == RelLeadsTo && g.nodes[e.from].kind == KindEvent {
							byParent[e.from] = append(byParent[e.from], ev)
						}
					}
				}
				for _, kids := range byParent {
					if len(kids) < 2 {
						continue
					}
					maxY := 0
					for _, k := range kids {
						if g.nodes[k].y > maxY {
							maxY = g.nodes[k].y
						}
					}
					for _, k := range kids {
						g.nodes[k].y = maxY
					}
				}
			}
			for _, ev := range rows[r] {
				placeSubs(ev)
			}
			// rank-level collision push (v7P6: space yields): sub columns
			// of DIFFERENT ranks share x-lanes, and hangs can reach further
			// than one rank back — when this rank's family (events, their
			// sub columns, their aux) would touch anything placed earlier,
			// the WHOLE rank steps down together (fork rows stay shared,
			// growth stays symmetric).
			family := map[int]bool{}
			var collect func(ev int)
			collect = func(ev int) {
				family[ev] = true
				for _, sub := range sp.subStacks[ev] {
					collect(sub)
				}
			}
			for _, ev := range rows[r] {
				collect(ev)
			}
			boxes := func(members map[int]bool, invert bool) [][4]int {
				// component-scoped: other components share these local
				// coordinates until assemble() spreads them (v7P2)
				var out [][4]int
				for n, rp := range gp.rel {
					owner := rp.event
					if g.nodes[owner].comp != ci || members[owner] == invert {
						continue
					}
					en := g.nodes[owner]
					if !en.placed {
						continue
					}
					nb := g.nodes[n]
					out = append(out, [4]int{en.x + rp.dx, en.y + rp.dy, en.x + rp.dx + nb.w, en.y + rp.dy + nb.h})
				}
				for i, n := range g.nodes {
					if !n.placed || n.kind != KindEvent || n.comp != ci {
						continue
					}
					if members[i] != invert {
						out = append(out, [4]int{n.x, n.y, n.x + n.w, n.y + n.h})
					}
				}
				return out
			}
			for guard := 0; guard < 200; guard++ {
				mine := boxes(family, false)
				others := boxes(family, true)
				hit := false
				for _, a := range mine {
					for _, b := range others {
						if a[0] < b[2]+10 && b[0] < a[2]+10 && a[1] < b[3]+10 && b[1] < a[3]+10 {
							hit = true
							break
						}
					}
					if hit {
						break
					}
				}
				if !hit {
					break
				}
				for n := range family {
					g.nodes[n].y += GridStep
				}
			}
		}

		// ---- aux: groups follow their event rigidly (v7P4: a group moves
		// only as a whole). ----
		for n, rp := range gp.rel {
			if g.nodes[n].comp != ci {
				continue
			}
			en := g.nodes[rp.event]
			g.nodes[n].x = en.x + rp.dx
			g.nodes[n].y = en.y + rp.dy
			g.nodes[n].placed = true
		}
		// ---- final no-overlap floor (v7P8): aux placed in different
		// frames (bands, fan-ins, satellites, spans) cannot see each other
		// during placement — the later-declared box steps down until every
		// pair keeps a clear gap. The structural router bows edges around
		// (v7P9); overlap is never acceptable.
		var auxIdx, evIdx []int
		for i, n := range g.nodes {
			if n.comp != ci || !n.placed || n.boundary {
				continue
			}
			if n.kind == KindEvent {
				evIdx = append(evIdx, i)
			} else {
				auxIdx = append(auxIdx, i)
			}
		}
		// step moves a colliding aux box down ONE grid step — together with
		// its placement DESCENDANTS inside its aux structure (v7P4: a
		// group's internal shape survives outer adjustments). Stepping the
		// box alone sheared chain links out of their group's diagonal when
		// a foreign band shared one member's column (the
		// murder-by-subtle-knife fan); stepping the WHOLE
		// structure dragged band members off their anchor rows for a mere
		// leaf collision (the stranded-leaf fixture). The subtree is the
		// unit: a chain member carries everything it places, a leaf
		// carries nothing, and an anchor-row member moves only for its own
		// collisions.
		auxKids := map[int][]int{}
		for _, i := range auxIdx {
			// a band-stack member carries its stack SUFFIX (v7P4: siblings
			// never split — the stack yields space as one line instead of
			// leapfrogging member past member)
			if nxt, ok := gp.stackNext[i]; ok {
				auxKids[i] = append(auxKids[i], nxt)
			}
			if p := m.anchors[i].primary; p != nil {
				if u := g.userOf(i, p); g.nodes[u].kind != KindEvent {
					auxKids[u] = append(auxKids[u], i)
				}
				continue
			}
			// a near-to SATELLITE rides with its partner (v7P5: "right
			// next to it") — left behind, a stepped partner strands it a
			// row away
			if te := m.anchors[i].satelliteOf; te != nil {
				auxKids[g.userOf(i, te)] = append(auxKids[g.userOf(i, te)], i)
			}
		}
		step := func(b *node) {
			// a generation-row member moves with its WHOLE tree body: its
			// row is structure (v7P4) — stepping it alone would desync the
			// row exactly as per-node stepping tore chains. The root is
			// band-bound and stays; only the pureGen body shifts.
			if b.pureGen {
				sid := m.structOf[b.idx]
				if sid >= 0 {
					for _, mi := range m.structRoots[sid] {
						if n := g.nodes[mi]; n.pureGen && n.comp == ci && n.placed && !n.boundary {
							n.y += GridStep
						}
					}
					return
				}
			}
			stack := []int{b.idx}
			seen := map[int]bool{}
			for len(stack) > 0 {
				i := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if seen[i] {
					continue
				}
				seen[i] = true
				if n := g.nodes[i]; n.comp == ci && n.placed && !n.boundary {
					n.y += GridStep
				}
				stack = append(stack, auxKids[i]...)
			}
		}
		sameStruct := func(a, b *node) bool {
			sa, sb := m.structOf[a.idx], m.structOf[b.idx]
			return sa >= 0 && sa == sb
		}
		// ---- TREE RE-COHESION (v7P4: "the murder by
		// subtle knife hierarchy should be rendered as ONE so we can
		// fold/unfold later"): an exclusive subtree is a foldable UNIT,
		// but the per-row X solve and the row-gap growth scatter its
		// members among foreign columns and ragged rows. After solving,
		// every tree member snaps back to its PLANNED offset from its
		// tree ROOT — the root keeps whatever the solve gave it, the
		// body hangs rigidly — and the cohesive floors below resolve
		// any collision by stepping the tree as one body. ----
		for _, ai := range auxIdx {
			b := g.nodes[ai]
			root, ok := gp.treeOf[ai]
			if !ok || !b.pureGen || ai == root {
				continue
			}
			rp, ok1 := gp.rel[ai]
			rr, ok2 := gp.rel[root]
			rn := g.nodes[root]
			if !ok1 || !ok2 || !rn.placed {
				continue
			}
			b.x = rn.x + (rp.dx - rr.dx)
			b.y = rn.y + (rp.dy - rr.dy)
		}
		// ... and the tree's FOOTPRINT is exclusive — the foldable unit
		// reads as ONE only when nothing foreign interleaves it: an aux
		// box inside a tree's bounding box (plus the band rhythm's
		// margins) moves OUTWARD past the nearer vertical flank
		// (marriage and refusal merged into the murder
		// tree's right flank).
		{
			// The move is an aesthetic upgrade and NEVER pays: the
			// destination must be free of boxes and — v7P6, absolute —
			// clear of every FLOW CORRIDOR, the vertical column through
			// each event's centre where S, the chain and E draw their
			// pinned line; when neither flank qualifies the intruder
			// stays (user 2026-07-20: MJ was thrown out of its band
			// spot into the S→E line).
			corridorHit := func(x, w int) bool {
				for ei := range g.nodes {
					ev := g.nodes[ei]
					if ev.kind != KindEvent || ev.boundary ||
						ev.comp != ci || !ev.placed {
						continue
					}
					cx := ev.x + ev.w/2
					m := GridStep / 2
					if x < cx+m && cx-m < x+w {
						return true
					}
				}
				return false
			}
			type tbox struct{ x0, y0, x1, y1 int }
			trees := map[int]*tbox{}
			for _, ai := range auxIdx {
				b := g.nodes[ai]
				root, ok := gp.treeOf[ai]
				if !ok || !b.pureGen {
					continue
				}
				t := trees[root]
				if t == nil {
					rn := g.nodes[root]
					t = &tbox{rn.x, rn.y, rn.x + rn.w, rn.y + rn.h}
					trees[root] = t
				}
				t.x0 = minInt(t.x0, b.x)
				t.y0 = minInt(t.y0, b.y)
				t.x1 = maxInt(t.x1, b.x+b.w)
				t.y1 = maxInt(t.y1, b.y+b.h)
			}
			for root, t := range trees {
				for _, ai := range auxIdx {
					b := g.nodes[ai]
					if b.pureGen || ai == root {
						continue
					}
					// the rule separates a tree from FOREIGN bands: a box
					// of the SAME anchor event is family — the tree's own
					// frame, its satellite mates, their concepts — and
					// keeps the band grammar (user 2026-07-20: MJ is the
					// frame mj41.cz hangs off; the murder tree's
					// intruders belonged to another event's band)
					if gp.rel[ai].event == gp.rel[root].event {
						continue
					}
					if b.x >= t.x1 || b.x+b.w <= t.x0 ||
						b.y >= t.y1 || b.y+b.h <= t.y0 {
						continue // strict overlap — a margin clip is not interleaving
					}
					free := func(x int) bool {
						if corridorHit(x, b.w) {
							return false
						}
						for _, oi := range auxIdx {
							o := g.nodes[oi]
							if o == b {
								continue
							}
							if x < o.x+o.w+10 && o.x < x+b.w+10 &&
								b.y < o.y+o.h+10 && o.y < b.y+b.h+10 {
								return false
							}
						}
						for _, ei := range evIdx {
							o := g.nodes[ei]
							if x < o.x+o.w+10 && o.x < x+b.w+10 &&
								b.y < o.y+o.h+10 && o.y < b.y+b.h+10 {
								return false
							}
						}
						return true
					}
					near, far := t.x1+ColGap, t.x0-ColGap-b.w
					if b.x+b.w/2 < (t.x0+t.x1)/2 {
						near, far = far, near
					}
					if free(near) {
						b.x = near
					} else if free(far) {
						b.x = far
					}
				}
			}
		}
		// aux vs the SKELETON's lines (v7P8: a VISIBLE gap between any
		// edge and any box): an event-to-event edge approximated by its
		// default PORT line — flow leaves the bottom centre for the top
		// centre, part-of the facing side centres. Shared by the floor
		// loop and the leaf rescue's spot search (the
		// rescue must not park a leaf ON the line the floors stepped it
		// off of).
		sweptBySkeleton := func(bx, by, bw, bh int) bool {
			for _, e := range g.edges {
				fn, tn := g.nodes[e.from], g.nodes[e.to]
				if fn.kind != KindEvent || tn.kind != KindEvent ||
					fn.boundary || tn.boundary || !g.isPlacing(e) {
					continue
				}
				if fn.comp != ci || !fn.placed || !tn.placed {
					continue
				}
				var p0, p1 [2]int
				if e.rel == RelLeadsTo {
					p0 = [2]int{fn.x + fn.w/2, fn.y + fn.h}
					p1 = [2]int{tn.x + tn.w/2, tn.y}
				} else if tn.x+tn.w/2 < fn.x+fn.w/2 {
					p0 = [2]int{fn.x, fn.y + fn.h/2}
					p1 = [2]int{tn.x + tn.w, tn.y + tn.h/2}
				} else {
					p0 = [2]int{fn.x + fn.w, fn.y + fn.h/2}
					p1 = [2]int{tn.x, tn.y + tn.h/2}
				}
				margin := GridStep / 2
				if segIntersectsBox(p0, p1, bx-margin, by-margin, bx+bw+margin, by+bh+margin) {
					return true
				}
			}
			return false
		}
		for guard := 0; guard < 200; guard++ {
			moved := false
			for ai := 0; ai < len(auxIdx); ai++ {
				b := g.nodes[auxIdx[ai]]
				// aux vs aux: the later-declared box steps down. Members of
				// ONE structure keep per-node stepping (moving the whole
				// group with the collider would never resolve the pair).
				for bi := 0; bi < ai; bi++ {
					a := g.nodes[auxIdx[bi]]
					if a.x < b.x+b.w+10 && b.x < a.x+a.w+10 &&
						a.y < b.y+b.h+10 && b.y < a.y+a.h+10 {
						if sameStruct(a, b) {
							b.y += GridStep
						} else {
							step(b)
						}
						moved = true
					}
				}
				// aux vs events: the skeleton never yields (v7P6) — the
				// aux box moves
				for _, ei := range evIdx {
					a := g.nodes[ei]
					if a.x < b.x+b.w+10 && b.x < a.x+a.w+10 &&
						a.y < b.y+b.h+10 && b.y < a.y+a.h+10 {
						step(b)
						moved = true
					}
				}
				// aux vs the SKELETON's lines (v7P8: a VISIBLE gap between
				// any edge and any box): an event-to-event edge approximated
				// by its default PORT line — flow leaves the bottom centre
				// for the top centre, part-of the facing side centres — and
				// an aux box within half a grid step of it steps down until
				// clear (the e3→e0 fan edge cutting cS's corner). A centre-
				// to-centre line would sweep territory the real edge never
				// enters and push innocent band members. Boundary fan lines
				// are steered by the router instead.
				if sweptBySkeleton(b.x, b.y, b.w, b.h) {
					step(b)
					moved = true
				}
			}
			if !moved {
				break
			}
		}
		// a sole LEAF concept stays ADJACENT to its expresser (v7P4:
		// a one-edge leaf has no reason to end up far
		// away): when the no-overlap floors stepped it more than a row
		// gap off its owner, it relocates to the nearest FREE spot
		// around the owner — below-centred, below-outward, beside.
		for _, ai := range auxIdx {
			c := g.nodes[ai]
			if c.kind != KindConcept {
				continue
			}
			deg := 0
			var owner *node
			ok := true
			for _, e := range append(append([]*edge{}, g.out[ai]...), g.in[ai]...) {
				deg++
				if e.rel != RelExpresses {
					ok = false // a near-to satellite is v7P5's business
				}
				o := e.from
				if o == ai {
					o = e.to
				}
				owner = g.nodes[o]
			}
			if !ok || deg != 1 || owner == nil || !owner.placed {
				continue
			}
			// stranded means more than a full row pitch below the
			// owner's bottom — the own-column floor steps AND the
			// offset-column band spot alike (swap of
			// clothing parked two rows down-left of swapT while the
			// wedge beside the corridor was free; the below-band rhythm
			// may legitimately sit one clearance step lower — that is
			// not stranded)
			if c.y < owner.y+owner.h || c.y-(owner.y+owner.h) <= RowPitch {
				continue
			}
			// a spot is good when it is FREE and does not read as
			// somebody else's member: no unrelated box side-adjacent
			// (aligned within one stack/column gap) — a leaf parked at
			// band rhythm beside a foreign stack pairs with it visually
			// ("unique cluster-wide IP is below
			// Gateway API")
			clear := func(x, y int) bool {
				for _, o := range g.nodes {
					if o == c || !o.placed || o.comp != c.comp {
						continue
					}
					if x < o.x+o.w+10 && o.x < x+c.w+10 &&
						y < o.y+o.h+10 && o.y < y+c.h+10 {
						return false
					}
					if o == owner || o.boundary {
						continue
					}
					xOv := x < o.x+o.w && o.x < x+c.w
					yOv := y < o.y+o.h && o.y < y+c.h
					vGap := minInt(absInt(y-(o.y+o.h)), absInt(o.y-(y+c.h)))
					hGap := minInt(absInt(x-(o.x+o.w)), absInt(o.x-(x+c.w)))
					if (xOv && vGap <= StackGap+4) || (yOv && hGap <= ColGap+4) {
						return false
					}
				}
				return true
			}
			side := 1
			if c.x+c.w/2 < owner.x+owner.w/2 {
				side = -1
			}
			ocx := owner.x + owner.w/2
			spots := [][2]int{
				{ocx - c.w/2, owner.y + owner.h + StackGap},
				{ocx + side*ColPitch - c.w/2, owner.y + owner.h + StackGap},
				{owner.x + owner.w + ColGap, owner.y + (owner.h-c.h)/2},
			}
			if side < 0 {
				spots[2][0] = owner.x - ColGap - c.w
			}
			// each spot family SLIDES laterally (grid steps, centre-out)
			// before the next family is tried: the ideal spot may be
			// swept by a skeleton line or the flow corridor while the
			// wedge one column-notch over is free
			try := func(x, y int) bool {
				if clear(x, y) && !sweptBySkeleton(x, y, c.w, c.h) {
					c.x, c.y = x, y
					return true
				}
				return false
			}
			relocated := false
			for _, sp2 := range spots {
				for d := 0; d <= ColPitch/2 && !relocated; d += GridStep {
					if try(sp2[0]-d, sp2[1]) {
						relocated = true
					} else if d > 0 && try(sp2[0]+d, sp2[1]) {
						relocated = true
					}
				}
				if relocated {
					break
				}
			}
		}
		// ---- RATIFIED: a sole leaf concept never reads as
		// PAIRED with an unrelated row-mate — v7P8's reads-as-paired
		// guard promoted from checker finding to placement force. When
		// the band's down-and-outward spot lands a thing's sole concept
		// beside an UNRELATED box at column rhythm, the concept moves UP
		// to its owner's row ("nodes sit next to what places them",
		// v7P4: the pair reads as one unit — container image + its
		// ready-to-run concept vs. the stranger containers); when the
		// owner-row spot is taken or swept, the concept slides OUTWARD
		// to the near-to stand-off instead — distance encodes relation
		// strength. Horizontal pairing only: vertical stacking IS the
		// band's design. ----
		for _, ai := range auxIdx {
			c := g.nodes[ai]
			if c.kind != KindConcept {
				continue
			}
			deg := 0
			var owner *node
			okLeaf := true
			for _, e := range append(append([]*edge{}, g.out[ai]...), g.in[ai]...) {
				deg++
				if e.rel != RelExpresses {
					okLeaf = false
				}
				o := e.from
				if o == ai {
					o = e.to
				}
				owner = g.nodes[o]
			}
			if !okLeaf || deg != 1 || owner == nil || owner.kind != KindThing || !owner.placed {
				continue
			}
			partners := func(ni int) map[int]bool {
				out := map[int]bool{}
				for _, e := range append(append([]*edge{}, g.out[ni]...), g.in[ni]...) {
					out[e.from], out[e.to] = true, true
				}
				return out
			}
			cp := partners(ai)
			related := func(oi int) bool {
				if cp[oi] {
					return true
				}
				for pi := range partners(oi) {
					if cp[pi] {
						return true
					}
				}
				return false
			}
			mateGap, mateDir, paired := 1<<30, 0, false
			for oi := range g.nodes {
				o := g.nodes[oi]
				if o == c || o == owner || !o.placed || o.comp != c.comp || o.boundary {
					continue
				}
				if o.kind == KindConcept {
					// concept-beside-concept is the concept LAYER (the
					// satellite row over sibling things, the concept
					// column) — the false-pairing read is CROSS-KIND: a
					// concept hugging a stranger THING reads as that
					// thing's concept
					continue
				}
				if !(c.y < o.y+o.h && o.y < c.y+c.h) {
					continue // horizontal pairing only
				}
				gap, dir := 0, 0
				if o.x >= c.x+c.w {
					gap, dir = o.x-(c.x+c.w), -1
				} else if c.x >= o.x+o.w {
					gap, dir = c.x-(o.x+o.w), 1
				} else {
					continue
				}
				if gap <= ColGap+4 && !related(oi) && gap < mateGap {
					mateGap, mateDir, paired = gap, dir, true
				}
			}
			if !paired {
				continue
			}
			// ... but a concept stacked in a COLUMN with a RELATED mate
			// is not a stray: the concept column (Reviewer under
			// Developer, mirrored about their shared owner) is a
			// ratified unit — the promotion is for the LONE dropped
			// leaf beside strangers
			inColumn := false
			for oi := range g.nodes {
				o := g.nodes[oi]
				if o == c || !o.placed || o.comp != c.comp || o.kind != KindConcept {
					continue
				}
				if !(c.x < o.x+o.w && o.x < c.x+c.w) {
					continue
				}
				vGap := o.y - (c.y + c.h)
				if o.y+o.h <= c.y {
					vGap = c.y - (o.y + o.h)
				}
				if vGap <= StackGap+4 && related(oi) {
					inColumn = true
					break
				}
			}
			if inColumn {
				continue
			}
			freeAt := func(x, y int) bool {
				for oi := range g.nodes {
					o := g.nodes[oi]
					if o == c || !o.placed || o.comp != c.comp {
						continue
					}
					if x < o.x+o.w+10 && o.x < x+c.w+10 &&
						y < o.y+o.h+10 && o.y < y+c.h+10 {
						return false
					}
				}
				return !sweptBySkeleton(x, y, c.w, c.h)
			}
			outDir := -1
			if c.x+c.w/2 > owner.x+owner.w/2 {
				outDir = 1
			}
			nx := owner.x - ColGap - c.w
			if outDir > 0 {
				nx = owner.x + owner.w + ColGap
			}
			ny := owner.y + (owner.h-c.h)/2
			if freeAt(nx, ny) {
				c.x, c.y = nx, ny
				continue
			}
			// (b) the near-to stand-off from the stranger
			if d := NearGap - mateGap; d > 0 {
				tx := c.x + mateDir*d
				if freeAt(tx, c.y) {
					c.x = tx
				}
			}
		}
		if g.tracing() {
			g.emitPositions("floors", ci)
		}

		// ---- PULL (v7P4): an aux node whose single demoted tie reaches a
		// placed partner slides along its column TOWARD it — "sit nearest
		// your further connection" — while every clearance holds and the
		// boxes' distance keeps shrinking; it stops at closest approach
		// (rows aligned). A shared node parked at its anchor's row while
		// its tie spans the whole chain reads as unrelated
		// (minimum distance for the closest points). ----
		for _, ai := range auxIdx {
			b := g.nodes[ai]
			if b.pureGen {
				continue // a generation member's row is structural (v7P4)
			}
			// only the tie's OWNER slides — the node this edge would have
			// placed had it won the anchor election; the partner is anchored
			// elsewhere and stays
			var ties []*edge
			for _, e := range g.userEdges(ai) {
				if e.demotedTie {
					ties = append(ties, e)
				}
			}
			if len(ties) != 1 {
				continue
			}
			pi := ties[0].from
			if pi == ai {
				pi = ties[0].to
			}
			p := g.nodes[pi]
			if !p.placed {
				continue
			}
			boxGapY := func(y int) int {
				switch {
				case p.y >= y+b.h:
					return p.y - (y + b.h)
				case y >= p.y+p.h:
					return y - (p.y + p.h)
				}
				return 0
			}
			// the slide keeps the STANDARD stack rhythm, not the hard
			// 10px floor — pulling a node to within 20px of the box it
			// was stacked above reads as touching (v7P8: gaps are
			// minimums; the pull only moves where full gaps hold)
			clearAt := func(y int) bool {
				for _, oi := range auxIdx {
					o := g.nodes[oi]
					if oi == ai {
						continue
					}
					if o.x < b.x+b.w+10 && b.x < o.x+o.w+10 &&
						o.y < y+b.h+StackGap && y < o.y+o.h+StackGap {
						return false
					}
				}
				for _, ei := range evIdx {
					o := g.nodes[ei]
					if o.x < b.x+b.w+10 && b.x < o.x+o.w+10 &&
						o.y < y+b.h+StackGap && y < o.y+o.h+StackGap {
						return false
					}
				}
				return true
			}
			carryDependents := func(y0 int) {
				// the slide CARRIES the slid node's own dependents (v7P4: a
				// group moves with its owner): an aux neighbour whose EVERY
				// placing edge touches the slid node — e.g. its outward
				// whole — rides along, or its sole tie stretches across the
				// vacated rows (tN stranded on the old row)
				dy := b.y - y0
				if dy == 0 {
					return
				}
				for _, oi := range auxIdx {
					if oi == ai {
						continue
					}
					sole := true
					n := 0
					for _, oe := range append(append([]*edge{}, g.out[oi]...), g.in[oi]...) {
						n++
						if oe.from != ai && oe.to != ai {
							sole = false
							break
						}
					}
					if sole && n > 0 {
						g.nodes[oi].y += dy
					}
				}
			}
			// v7P3 reaches into the aux lattice: when the anchor's user and
			// the tie partner are PEERS in one stack column and this node
			// hangs one layer out, it reads as their shared CHILD — a join —
			// and a join centres between its parents, not at closest
			// approach to the demoted tie. Which parent won the anchor
			// election is a declaration-order coin flip there (v7P7), and
			// symmetry must not depend on it (`color` sits
			// symmetric between `black` and `white`).
			if pe := m.anchors[ai].primary; pe != nil {
				an := g.nodes[g.userOf(ai, pe)]
				xOver := func(a, o *node) bool { return a.x < o.x+o.w && o.x < a.x+a.w }
				if an != p && an.placed && an.kind != KindEvent && p.kind != KindEvent &&
					xOver(an, p) && !xOver(b, an) && !xOver(b, p) {
					target := (an.y+an.h/2+p.y+p.h/2)/2 - b.h/2
					y0 := b.y
					for guard := 0; guard < 200 && b.y != target; guard++ {
						step := target - b.y
						if step > GridStep {
							step = GridStep
						} else if step < -GridStep {
							step = -GridStep
						}
						if !clearAt(b.y + step) {
							break
						}
						b.y += step
					}
					if b.y != target {
						// symmetric or nothing — a partial slide breaks the
						// join reading for nothing (v7P4's no-twitch rule)
						b.y = y0
					} else {
						// vertically FAR from both parents the child needs
						// no full column of separation — nothing sits
						// between, so it TUCKS to half a node width off
						// the parents' column and the join edges steepen
						// toward the hierarchy's vertical read
						// (color sat a whole pitch out "while
						// half of the node width would be fine")
						gapTo := func(o *node) int {
							if o.y >= b.y+b.h {
								return o.y - (b.y + b.h)
							}
							if b.y >= o.y+o.h {
								return b.y - (o.y + o.h)
							}
							return 0
						}
						if minInt(gapTo(an), gapTo(p)) >= RowPitch {
							colL := minInt(an.x, p.x)
							colR := maxInt(an.x+an.w, p.x+p.w)
							tx := colR - b.w/2
							if b.x+b.w/2 < (colL+colR)/2 {
								tx = colL - b.w/2
							}
							clearBox := func(x, y int) bool {
								for _, oi := range auxIdx {
									o := g.nodes[oi]
									if o == b {
										continue
									}
									if x < o.x+o.w+10 && o.x < x+b.w+10 &&
										y < o.y+o.h+10 && o.y < y+b.h+10 {
										return false
									}
								}
								for _, ei := range evIdx {
									o := g.nodes[ei]
									if x < o.x+o.w+10 && o.x < x+b.w+10 &&
										y < o.y+o.h+10 && o.y < y+b.h+10 {
										return false
									}
								}
								return !sweptBySkeleton(x, y, b.w, b.h)
							}
							if tx != b.x && clearBox(tx, b.y) {
								b.x = tx
							}
						}
					}
					carryDependents(y0)
					continue
				}
			}
			if boxGapY(b.y) <= RowGap {
				continue // near enough — only a genuinely FAR tie pulls
			}
			y0 := b.y
			cyDiff := func(y int) int {
				d := (y + b.h/2) - (p.y + p.h/2)
				if d < 0 {
					return -d
				}
				return d
			}
			for guard := 0; guard < 100; guard++ {
				dir := GridStep
				if p.y+p.h/2 < b.y+b.h/2 {
					dir = -GridStep
				}
				next := b.y + dir
				// closest approach first, then ROW alignment — an
				// x-disjoint partner ends up beside the node, tie
				// horizontal
				better := boxGapY(next) < boxGapY(b.y) ||
					(boxGapY(next) == 0 && cyDiff(next) < cyDiff(b.y))
				if !better || !clearAt(next) {
					break
				}
				b.y = next
			}
			// a pull that cannot REACH pays nothing: if the best slide
			// still leaves the partner more than a row gap away, the
			// node stays at its stack spot — a 20px twitch toward a far
			// tie breaks the band's symmetry for nothing
			// (kubectl must keep mirroring user)
			if boxGapY(b.y) > RowGap {
				b.y = y0
			}
			// ... and a pull never ABANDONS its anchor
			// ("far is connected to e1, so it should be next to e1 — then
			// go diagonal to e8"): the structural anchor edge outranks
			// the demoted tie (the kind hierarchy), so a slide that would
			// leave the anchor more than a row pitch behind reverts and
			// the TIE pays the distance instead. Short-range alignment
			// slides (the deep-shared concept reaching its second user
			// one row up) survive.
			if pe := m.anchors[ai].primary; pe != nil && b.y != y0 {
				an := g.nodes[g.userOf(ai, pe)]
				agap := 0
				switch {
				case an.y >= b.y+b.h:
					agap = an.y - (b.y + b.h)
				case b.y >= an.y+an.h:
					agap = b.y - (an.y + an.h)
				}
				if agap > RowPitch {
					b.y = y0
				}
			}
			carryDependents(y0)
		}
		if g.tracing() {
			g.emitPositions("pull", ci)
		}

		// ---- S/E boundaries (v7P1/P3): S stays on the start event; E caps
		// the timeline centred under the end events. ----
		if len(comp.events) > 0 && len(rows) > 0 && len(rows[0]) > 0 {
			starts, ends := sp.starts[ci], sp.ends[ci]
			if len(starts) == 0 {
				starts = rows[0]
			}
			if len(ends) == 0 {
				ends = rows[len(rows)-1]
			}
			// v7P3: S stays on the START event; several starts centre it on
			// their midpoint (the timeline axis). E caps the timeline on the
			// SAME axis.
			sSum := 0
			for _, st := range starts {
				sSum += g.nodes[st].x + g.nodes[st].w/2
			}
			sX := sSum / len(starts)
			maxBot := 0
			for _, e := range ends {
				if b := g.nodes[e].y + g.nodes[e].h; b > maxBot {
					maxBot = b
				}
			}
			eX := sX

			sNode, eNode := comp.sNode, comp.eNode
			if sNode < 0 {
				sNode = g.addBoundary("S", ci)
				eNode = g.addBoundary("E", ci)
				comp.sNode, comp.eNode = sNode, eNode
				for _, s := range starts {
					g.addBoundaryEdge(sNode, s)
				}
				for _, e := range ends {
					g.addBoundaryEdge(e, eNode)
				}
			}
			g.nodes[sNode].x = sX - BoundarySize/2
			// S CAPS the component's timeline (v7P3: S and
			// E are the OUTERMOST elements): it sits above every box.
			sY := 0
			for _, n := range g.nodes {
				if n.comp != ci || !n.placed || n.boundary {
					continue
				}
				if n.y-BoundaryGap-BoundarySize < sY {
					sY = n.y - BoundaryGap - BoundarySize
				}
			}
			// S keeps its fan inside the SAME 150° cap as E (v7P3/P8,
			// unified): the flattest S->start edge still needs
			// dy >= dx / tan(75°), so a wide start row lifts S higher.
			minTop := 0
			for i, st := range starts {
				if t := g.nodes[st].y; i == 0 || t < minTop {
					minTop = t
				}
			}
			for _, st := range starts {
				dx := g.nodes[st].x + g.nodes[st].w/2 - sX
				if dx < 0 {
					dx = -dx
				}
				if need := gridUp(int(math.Ceil(float64(dx) / 3.7320508))); minTop-need-BoundarySize < sY {
					sY = minTop - need - BoundarySize
				}
			}
			g.nodes[sNode].y = sY - g.sExtra[ci] // demand-grown (v7P8 §4)
			// E CAPS the component's timeline (v7P3): it sits below EVERY
			// box of the component — aux hanging under a row included —
			// and keeps the boundary fan's slope inside the angle cap
			// (v7P3/P8): the flattest end->E edge still needs
			// dy >= dx / tan(75 deg).
			eY := maxBot + BoundaryGap
			for _, n := range g.nodes {
				if n.comp != ci || !n.placed || n.boundary {
					continue
				}
				if n.y+n.h+BoundaryGap > eY {
					eY = n.y + n.h + BoundaryGap
				}
			}
			for _, e := range ends {
				dx := g.nodes[e].x + g.nodes[e].w/2 - eX
				if dx < 0 {
					dx = -dx
				}
				if need := gridUp(int(math.Ceil(float64(dx) / 3.7320508))); maxBot+need > eY {
					eY = maxBot + need
				}
			}
			g.nodes[eNode].x = eX - BoundarySize/2
			g.nodes[eNode].y = eY + g.eExtra[ci] // demand-grown (v7P8 §4)
		}

		// ---- component bbox (local coords). ----
		first := true
		for _, n := range g.nodes {
			if n.comp != ci || !n.placed {
				continue
			}
			if first {
				comp.minX, comp.minY = n.x, n.y
				comp.maxX, comp.maxY = n.x+n.w, n.y+n.h
				first = false
				continue
			}
			if n.x < comp.minX {
				comp.minX = n.x
			}
			if n.y < comp.minY {
				comp.minY = n.y
			}
			if n.x+n.w > comp.maxX {
				comp.maxX = n.x + n.w
			}
			if n.y+n.h > comp.maxY {
				comp.maxY = n.y + n.h
			}
		}
	}
}

// subGridRowMetrics returns a composite's sub-grid row heights and the gap
// ABOVE each row (rowGapAt[0] is unused). One formula, two consumers: the
// placement in place() and the container reservation in subGridExtent.
//
// The gaps GROW for the rows' band hangs (v7P8: gaps are minimums; 'user'
// cascaded past a packed column because wr's stack reached into cwr's row
// band): the gap under a row fits what hangs BELOW its members plus what
// rises ABOVE the next row's, one clearance apart.
func (g *graph) subGridRowMetrics(parent int, sp *skeletonPlan, gp *groupsPlan) (rowH, rowGapAt []int) {
	rows := sp.subRows[parent]
	rowH = make([]int, len(rows))
	rowGapAt = make([]int, len(rows))
	for ri, row := range rows {
		for _, s := range row {
			if g.nodes[s].h > rowH[ri] {
				rowH[ri] = g.nodes[s].h
			}
		}
	}
	for ri := 1; ri < len(rows); ri++ {
		below, above := 0, 0
		for _, s := range rows[ri-1] {
			if d := gp.belowExt[s]; d > below {
				below = d
			}
		}
		for _, s := range rows[ri] {
			if d := gp.aboveExt[s]; d > above {
				above = d
			}
		}
		rowGapAt[ri] = RowGap
		if need := below + StackGap + above; need > RowGap {
			rowGapAt[ri] = (need + GridStep - 1) / GridStep * GridStep
		}
		// demand-pass growth below a member (v7P8 §4): a stranded sole leaf
		// posted it so the next row's part-of diagonal drops under the
		// leaf's near-anchor wedge (swap of clothing closer to swapT)
		for _, s := range rows[ri-1] {
			if d := g.rowExtra[s]; d > 0 {
				rowGapAt[ri] += d
			}
		}
	}
	return rowH, rowGapAt
}

// subGridExtent is the total vertical span a composite's sub-grid occupies,
// nested grids included — a member's effective height is the taller of its
// own box and the grid hanging off it. Zero for a leaf event.
//
// place() centres the grid on the composite (sy = pn.y + pn.h/2 - total/2)
// using the members' plain box heights, so this is an UPPER bound on the real
// span: a nested grid may pack tighter than its own extent, never wider.
// Options.Containers reserves against it, and over-reserving only widens the
// band that keeps a shell exclusive.
func (g *graph) subGridExtent(ev int, sp *skeletonPlan, gp *groupsPlan) int {
	rows := sp.subRows[ev]
	if len(rows) == 0 {
		return 0
	}
	_, rowGapAt := g.subGridRowMetrics(ev, sp, gp)
	total := 0
	for ri, row := range rows {
		h := 0
		for _, s := range row {
			eh := g.nodes[s].h
			if nested := g.subGridExtent(s, sp, gp); nested > eh {
				eh = nested
			}
			if eh > h {
				h = eh
			}
		}
		if ri > 0 {
			total += rowGapAt[ri]
		}
		total += h
	}
	return total
}

// subGridOverhang is how far a composite's sub-grid reaches ABOVE (and,
// symmetrically, below) the composite's own box — the grid is centred on it,
// so each side gets half of whatever the grid is taller by. Zero unless
// Options.Containers is on: this reservation is exactly what that option buys.
func (g *graph) subGridOverhang(ev int, sp *skeletonPlan, gp *groupsPlan) int {
	if !g.opts.Containers {
		return 0
	}
	over := g.subGridExtent(ev, sp, gp) - g.nodes[ev].h
	if over <= 0 {
		return 0
	}
	return gridUp((over + 1) / 2)
}

// addBoundary appends a synthesized S/E node (v7P1: every component with
// events gets its own).
func (g *graph) addBoundary(label string, comp int) int {
	maxID := 0
	for _, n := range g.nodes {
		if n.id > maxID {
			maxID = n.id
		}
	}
	n := &node{
		idx:      len(g.nodes),
		id:       maxID + 1,
		name:     label,
		kind:     KindEvent,
		w:        BoundarySize,
		h:        BoundarySize,
		comp:     comp,
		boundary: true,
		placed:   true,
	}
	g.nodes = append(g.nodes, n)
	return n.idx
}

func (g *graph) addBoundaryEdge(from, to int) {
	e := &edge{
		idx:        len(g.edges),
		from:       from,
		to:         to,
		rel:        RelLeadsTo,
		structural: true,
	}
	g.edges = append(g.edges, e)
	g.out[from] = append(g.out[from], e)
	g.in[to] = append(g.in[to], e)
}
