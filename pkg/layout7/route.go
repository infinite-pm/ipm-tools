package layout7

import (
	"math"
	"sort"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// route implements v7P9 — edge routing: clean, kind-aware, or a stub.
//
//   - Ports FACE the partner (dominant axis); flow edges keep their pinned
//     vertical ports (source bottom → target top); several edges sharing a
//     node side SPREAD along it — never all from one point, never at
//     corners.
//   - Structural edges draw straight: the earlier stages placed their
//     endpoints adjacent by construction (v7P4/P6).
//   - Non-structural edges (demoted ties, same-kind ties, eXe/eNe) try
//     straight → dogleg → flank bypass (out of the border at 45°, along a
//     lane on the clear flank, back in at the same angle — v7P5/P9), each
//     candidate checked against nodes and the kind-aware crossing budget
//     (1.0 total): same-kind crossing 1.0, different kinds 0.5, a graze
//     (inside the v7P8 visible gap) 0.5, detour length past 1.5× direct
//     costs the excess — and a HIERARCHY tie crossing a FLOW edge costs
//     2.0 (v7P6: the corridor is never cut; only near-to may cross it).
//   - A tie with no acceptable route HIDES (stub) — the least structural
//     kind first by construction, since only non-structural edges reach
//     the fallback; leads-to NEVER hides (v7P6). Guard: a node's LAST
//     visible connection never hides — then the best candidate draws
//     over budget instead.
//
// Simplifications recorded for the next increment (layout7-engine.md):
// arrow-landing clearance is not yet enforced, and hide victims are chosen
// per-edge in declaration order rather than re-choosing globally by kind.

type routed struct {
	src, tgt layout.EdgePort
	bends    []layout.Position
	stubbed  bool
	leaned   bool // a leaned hop-diagonal: wins positionally, never by freeness
}

type edgeEnd struct {
	edge   *edge
	atFrom bool // this end sits on the edge's FROM node
}

// route computes every edge's ports, bends and visibility.
func (g *graph) route() []routed {
	routes := make([]routed, len(g.edges))

	// ---- 1. port sides. ----
	sideOf := make([][2]string, len(g.edges)) // [fromSide, toSide]
	for _, e := range g.edges {
		f, t := g.nodes[e.from], g.nodes[e.to]
		if g.isFlow(e) {
			sideOf[e.idx] = [2]string{"bottom", "top"} // v7P3: time reads down
			continue
		}
		dx := (t.x + t.w/2) - (f.x + f.w/2)
		dy := (t.y + t.h/2) - (f.y + f.h/2)
		abs := func(v int) int {
			if v < 0 {
				return -v
			}
			return v
		}
		// v7P3/P4: the hierarchy meets its event on the HORIZONTAL. An ePe
		// edge always does (part-of reads right); a STRUCTURAL band
		// connector (tPe into the event, eXc out of it) does too — the
		// band sits beside its event by construction, and a deep stack's
		// steep members would otherwise all fall back onto one centre
		// port and crowd (the fan must use the WHOLE
		// border at equal gaps). The vertical tie-break below is for aux
		// generations, not the event hierarchy.
		// ... but only when the boxes are horizontally DISJOINT — truly
		// beside the event. A band member stacked ABOVE/BELOW its event
		// (x-ranges overlapping) connects vertically like any stacked
		// neighbour (span→mid forced sideways squeezed
		// its arrow under the mid→outer shaft).
		// ... and only while the line still MEETS a vertical border at a
		// readable angle: past the 150°-cap ratio (|dy| > tan 75° × |dx|)
		// a side port reads as a corner landing — the edge falls through
		// to vertical ports and targets the top/bottom CENTRE instead
		// (the far-tie hugging the border).
		bandConn := e.structural && dx != 0 &&
			(f.x+f.w <= t.x || t.x+t.w <= f.x) &&
			abs(dy)*100 < abs(dx)*373 &&
			((e.rel == RelPartOf && t.kind == KindEvent) ||
				(e.rel == RelExpresses && f.kind == KindEvent && t.kind == KindConcept))
		// ... and the horizontal border-spread serves a FAN (v7P9: ports
		// face the partner). An event with
		// a SOLE band member has nothing to crowd, so when vertical travel
		// dominates the edge follows the dominant axis instead — wide
		// boxes make the drawn line far steeper than the centre-delta cap
		// sees, and a steep side landing hugs the corner
		// (swapT's only concept sat below-left, 74° into the right border —
		// it lands on the TOP instead).
		if bandConn && abs(dy) > abs(dx) {
			ev := e.from
			if t.kind == KindEvent {
				ev = e.to
			}
			deg := 0
			for _, o := range g.out[ev] {
				if o.structural && g.nodes[o.to].kind != KindEvent {
					deg++
				}
			}
			for _, o := range g.in[ev] {
				if o.structural && g.nodes[o.from].kind != KindEvent {
					deg++
				}
			}
			if deg < 2 {
				bandConn = false
			}
		}
		if (e.rel == RelPartOf && f.kind == KindEvent && t.kind == KindEvent && dx != 0) || bandConn {
			if dx >= 0 {
				sideOf[e.idx] = [2]string{"right", "left"}
			} else {
				sideOf[e.idx] = [2]string{"left", "right"}
			}
			continue
		}
		// v7P4's layered generations read DOWN: a placing edge between aux
		// kinds (a pure structure's skeleton — demoted or not, the
		// generation follows the RELATION) takes VERTICAL ports even when
		// it reaches further sideways than down — a fan lands on its row's
		// tops, never on an outer member's side (the
		// diamond's 45° rule generalized). A WIDE fan spills its outermost
		// edges (reaching twice as far sideways as down) to the SOURCE's
		// left/right flank instead of crowding one border — the flank port
		// keeps the line straight, and the target still lands on its row's
		// top, the border facing the hub (the ratified
		// gastownhall wide-fan design).
		// ... and only with a REAL vertical run: boxes whose facing borders
		// touch (or nearly — less than one grid step, no room for an
		// arrowhead) would make the "vertical" a horizontal line lying ON
		// both borders (tD→cX hidden by the box borders);
		// they fall through to the dominant axis instead.
		if g.isPlacing(e) && f.kind != KindEvent && t.kind != KindEvent &&
			(f.y+f.h+GridStep <= t.y || t.y+t.h+GridStep <= f.y) {
			srcSide, tgtSide := "bottom", "top"
			if dy < 0 {
				srcSide, tgtSide = "top", "bottom"
			}
			if abs(dx) >= 2*abs(dy) {
				// only a genuinely WIDE fan spills — a small fan's border
				// has room for all its lines
				fan := 0
				for _, o := range g.out[e.from] {
					if g.isPlacing(o) && g.nodes[o.to].kind != KindEvent {
						fan++
					}
				}
				if fan >= 5 {
					if dx >= 0 {
						srcSide = "right"
					} else {
						srcSide = "left"
					}
				}
			}
			sideOf[e.idx] = [2]string{srcSide, tgtSide}
			continue
		}
		// dominant axis, VERTICAL winning ties: hierarchy reads down, so a
		// 45-degree diamond edge leaves the bottom and lands on the top
		// CENTRE — never on a corner-ish side port.
		if abs(dx) > abs(dy) {
			if dx >= 0 {
				sideOf[e.idx] = [2]string{"right", "left"}
			} else {
				sideOf[e.idx] = [2]string{"left", "right"}
			}
		} else {
			if dy >= 0 {
				sideOf[e.idx] = [2]string{"bottom", "top"}
			} else {
				sideOf[e.idx] = [2]string{"top", "bottom"}
			}
		}
	}

	// ---- 1a. a BAND MEMBER's same-kind event ties fan from its FACING
	// side (v7P9 "use one side for the same edge type and direction", the
	// band reading): an aux node that sits ON THE ROW of an event it
	// connects to meets that event on the horizontal through its facing
	// side — its other same-rel ties to events in the SAME horizontal
	// direction join that side and land on the events' facing sides,
	// instead of leaving the bottom onto the events' top corners (a
	// person part-of every step of a chain, on the right flank: the
	// top-most tie went left, the other two dropped off the bottom onto
	// the events' top-right — "the other two should prefer the left side
	// too"). Each join must keep the drawn line inside the 150° cap by
	// BORDER gaps (a steeper tie keeps its vertical exit — prefer, not
	// force), have a real horizontal run, pass a clean trial straight
	// (no hit, no graze, no flow or band chord cut), and find room: a
	// receiving side takes at most two band arrivals. Every same-rel tie
	// on the facing side — joined or there already — is PINNED: 1b below
	// never pulls one back onto a farther sibling's vertical side.
	sidePinned := map[*edge]bool{}
	{
		type akey struct {
			n     int
			rel   Rel
			right bool // partner lies to the node's right
		}
		aligned := map[akey]bool{}
		bandOf := func(e *edge) (aux, ev *node, auxIsFrom bool, ok bool) {
			if g.isFlow(e) {
				return nil, nil, false, false
			}
			f, t := g.nodes[e.from], g.nodes[e.to]
			if f.boundary || t.boundary {
				return nil, nil, false, false
			}
			if f.kind != KindEvent && t.kind == KindEvent {
				return f, t, true, true
			}
			if f.kind == KindEvent && t.kind != KindEvent {
				return t, f, false, true
			}
			return nil, nil, false, false
		}
		facing := func(aux, ev *node) (ap, ep [2]int, auxSide, evSide string) {
			if ev.x > aux.x {
				return [2]int{aux.x + aux.w, aux.y + aux.h/2}, [2]int{ev.x, ev.y + ev.h/2}, "right", "left"
			}
			return [2]int{aux.x, aux.y + aux.h/2}, [2]int{ev.x + ev.w, ev.y + ev.h/2}, "left", "right"
		}
		for _, e := range g.edges {
			aux, ev, auxIsFrom, ok := bandOf(e)
			if !ok {
				continue
			}
			side := sideOf[e.idx][0]
			if !auxIsFrom {
				side = sideOf[e.idx][1]
			}
			if side != "left" && side != "right" {
				continue
			}
			dy := (ev.y + ev.h/2) - (aux.y + aux.h/2)
			if dy < -10 || dy > 10 {
				continue
			}
			// ... and it must actually MEET the event through that side:
			// an on-row connection whose straight is blocked — a box in
			// the way, or a flow corridor it would cut (v7P6: a hierarchy
			// tie never slices the timeline) — leaves for a lane off the
			// top instead (controllers → its loop, across the loop's own
			// sub-event link); no side to share, the premise fails.
			ap, ep, _, _ := facing(aux, ev)
			if g.hitsNode([][2]int{ap, ep}, e) || g.grazeCount([][2]int{ap, ep}, e) > 0 ||
				g.cutsFlowChord(ap, ep) {
				continue
			}
			aligned[akey{aux.idx, e.rel, ev.x > aux.x}] = true
		}
		// arrivals already assigned to a node's side (arrowheads need
		// room: a side takes at most TWO band arrivals — the on-row one
		// and one joined; a third would stack its head on a neighbour's)
		arrivals := func(n int, side string) int {
			c := 0
			for _, o := range g.edges {
				if o.to == n && sideOf[o.idx][1] == side {
					c++
				}
			}
			return c
		}
		for _, e := range g.edges {
			aux, ev, auxIsFrom, ok := bandOf(e)
			if !ok {
				continue
			}
			right := ev.x > aux.x
			if !aligned[akey{aux.idx, e.rel, right}] {
				continue
			}
			ap, ep, auxSide, evSide := facing(aux, ev)
			side := sideOf[e.idx][0]
			if !auxIsFrom {
				side = sideOf[e.idx][1]
			}
			if side == auxSide {
				// already on the facing side (the near tie, dominant
				// axis): PIN it too, else 1b's vertical unification pulls
				// it away under a farther sibling that stayed vertical
				sidePinned[e] = true
				continue
			}
			if side != "top" && side != "bottom" {
				continue
			}
			gapX := maxInt(aux.x, ev.x) - minInt(aux.x+aux.w, ev.x+ev.w)
			gapY := maxInt(aux.y, ev.y) - minInt(aux.y+aux.h, ev.y+ev.h)
			if gapY < 0 {
				gapY = 0
			}
			// the 150° cap by BORDER gaps (as 1b measures it) — and, since
			// TALL boxes lie the other way (a side port sits half a box
			// below the border gap's end), the CENTRE delta must not run
			// past ~79° either (a 240px event's tie read near-vertical)
			dyC := (ev.y + ev.h/2) - (aux.y + aux.h/2)
			if dyC < 0 {
				dyC = -dyC
			}
			if gapX < GridStep || gapY*100 > gapX*373 || dyC*100 > gapX*500 {
				continue
			}
			recv, recvSide := ev.idx, evSide
			if !auxIsFrom {
				recv, recvSide = aux.idx, auxSide
			}
			if arrivals(recv, recvSide) >= 2 {
				continue
			}
			// (a band-mate's on-row chord in the way is NOT a refusal:
			// one same-kind crossing is within the router's budget, and
			// refusing the side only sends the tie to a worse shape —
			// the router's currency decides, not this pass)
			trial := [][2]int{ap, ep}
			if g.hitsNode(trial, e) || g.grazeCount(trial, e) > 0 ||
				g.cutsFlowChord(ap, ep) {
				continue
			}
			if auxIsFrom {
				sideOf[e.idx] = [2]string{auxSide, evSide}
			} else {
				sideOf[e.idx] = [2]string{evSide, auxSide}
			}
			sidePinned[e] = true
		}
	}

	// ---- 1b. same-kind edges from one aux node UNIFY their exit side
	// (v7P9: "aim for symmetry — use one side
	// for the same edge type and direction"): when a node's same-rel
	// edges toward the same vertical direction split between vertical
	// and horizontal exits, the horizontal ones join the vertical side —
	// if their BOX GAP keeps the drawn line inside the 150° cap (border
	// gaps, not centre deltas: wide boxes lie). tW's part-ofs read as
	// one fan off the top; a far member whose line would fall flat keeps
	// its side exit (the canvas must not grow for symmetry's sake).
	{
		type gkey struct {
			n   int
			rel Rel
			up  bool
		}
		groups := map[gkey][]*edge{}
		hasVert := map[gkey]bool{}
		for _, e := range g.edges {
			if g.isFlow(e) {
				continue
			}
			f, t := g.nodes[e.from], g.nodes[e.to]
			if f.boundary || t.boundary || f.kind == KindEvent {
				continue
			}
			dy := (t.y + t.h/2) - (f.y + f.h/2)
			if dy == 0 {
				continue
			}
			// demotion status does not split the group: the USER wrote
			// one relation, and anchor-vs-tie is the engine's internal
			// coin (v7P7) — a tie exits beside its structural sibling
			k := gkey{e.from, e.rel, dy < 0}
			groups[k] = append(groups[k], e)
			if sideOf[e.idx][0] == "top" || sideOf[e.idx][0] == "bottom" {
				hasVert[k] = true
			}
		}
		for k, es := range groups {
			if len(es) < 2 || !hasVert[k] {
				continue
			}
			for _, e := range es {
				if sideOf[e.idx][0] == "top" || sideOf[e.idx][0] == "bottom" || sidePinned[e] {
					continue
				}
				f, t := g.nodes[e.from], g.nodes[e.to]
				gapX := maxInt(f.x, t.x) - minInt(f.x+f.w, t.x+t.w)
				if gapX < 0 {
					gapX = 0
				}
				var gapY int
				if k.up {
					gapY = f.y - (t.y + t.h)
				} else {
					gapY = t.y - (f.y + f.h)
				}
				if gapY < GridStep || gapX*100 > gapY*373 {
					continue
				}
				// ... and only for a CLEAN trial straight: joining the
				// vertical side must not send the tie through or against
				// boxes it previously stubbed around (the late-branch tB
				// swapped its stub for a grazing least-bad route)
				var sp2, tp2 [2]int
				if k.up {
					sp2 = [2]int{f.x + f.w/2, f.y}
					tp2 = [2]int{t.x + t.w/2, t.y + t.h}
				} else {
					sp2 = [2]int{f.x + f.w/2, f.y + f.h}
					tp2 = [2]int{t.x + t.w/2, t.y}
				}
				trial := [][2]int{sp2, tp2}
				if g.hitsNode(trial, e) || g.grazeCount(trial, e) > 0 {
					continue
				}
				if k.up {
					sideOf[e.idx] = [2]string{"top", "bottom"}
				} else {
					sideOf[e.idx] = [2]string{"bottom", "top"}
				}
			}
		}
	}

	// ---- 2. per-(node, side) spread (v7P9: ports spread, no corners). ----
	bySide := map[[2]int]map[string][]edgeEnd{} // key: node idx + dummy; side -> ends
	add := func(n int, side string, end edgeEnd) {
		k := [2]int{n, 0}
		if bySide[k] == nil {
			bySide[k] = map[string][]edgeEnd{}
		}
		bySide[k][side] = append(bySide[k][side], end)
	}
	for _, e := range g.edges {
		add(e.from, sideOf[e.idx][0], edgeEnd{e, true})
		add(e.to, sideOf[e.idx][1], edgeEnd{e, false})
	}
	// How many ends each border carried at SPREAD time. A border that
	// later loses a member leaves the survivor holding a slot it was
	// only given because the border was shared (v7P9: the survivor
	// re-centres instead of keeping an abandoned quarter).
	spreadN := map[int]map[string]int{}
	portPos := map[*edge][2]float64{} // fractions [fromPos, toPos]
	for _, e := range g.edges {
		portPos[e] = [2]float64{0.5, 0.5}
	}
	for k, sides := range bySide {
		n := g.nodes[k[0]]
		for side, ends := range sides {
			if len(ends) < 2 {
				continue
			}
			if spreadN[k[0]] == nil {
				spreadN[k[0]] = map[string]int{}
			}
			spreadN[k[0]][side] = len(ends)
			horizontal := side == "top" || side == "bottom"
			// Slots follow the APPROACH ANGLE (v7P9): the fan leaving a side
			// orders by where each partner lies, so neighbouring lines never
			// swap. Sorting by the partner's centre alone ties for same-row
			// partners at different distances — the steeper line must take
			// the slot nearer its heading (tW's ties to two
			// layers on one row crossed).
			angleOf := func(end edgeEnd) float64 {
				o := g.nodes[g.otherEnd(end)]
				dy := float64(o.y+o.h/2) - float64(n.y+n.h/2)
				dx := float64(o.x+o.w/2) - float64(n.x+n.w/2)
				a := math.Atan2(dy, dx) // (-pi, pi], 0 = right, pi/2 = down
				switch side {
				case "left": // slots top->bottom = up (3pi/2) -> left (pi) -> down (pi/2)
					if a < 0 {
						a += 2 * math.Pi
					}
					return -a
				case "right": // slots top->bottom = up (-pi/2) -> right (0) -> down (pi/2)
					return a
				case "top": // slots left->right = left (pi) -> up (3pi/2) -> right (2pi)
					if a <= 0 {
						a += 2 * math.Pi
					}
					return a
				default: // bottom: slots left->right = left (pi) -> down (pi/2) -> right (0)
					return -a
				}
			}
			sort.SliceStable(ends, func(a, b int) bool {
				return angleOf(ends[a]) < angleOf(ends[b])
			})
			// v7P9: edges SPREAD around the PINNED port. A lone leads-to
			// on this side keeps 0.5 (its straight corridor); with no flow
			// edge, a lone ALIGNED partner (same row for left/right sides,
			// same column for top/bottom) takes the midline instead —
			// aligned neighbours connect with one straight line. Everything
			// else takes the surrounding slots.
			flowCount := 0
			for _, end := range ends {
				if g.isFlow(end.edge) {
					flowCount++
				}
			}
			aligned := func(end edgeEnd) bool {
				self := g.nodes[k[0]]
				other := g.nodes[g.otherEnd(end)]
				if horizontal {
					d := (self.x + self.w/2) - (other.x + other.w/2)
					return d >= -10 && d <= 10
				}
				d := (self.y + self.h/2) - (other.y + other.h/2)
				return d >= -10 && d <= 10
			}
			alignedCount := 0
			for _, end := range ends {
				if !g.isFlow(end.edge) && aligned(end) {
					alignedCount++
				}
			}
			// A lone ALIGNED partner takes 0.5 wherever it falls in the
			// approach order, and the others STAY ON THEIR SIDE of it —
			// every end before it in the order below 0.5, every end after
			// it above, each at least one slot step away — so the fan
			// never folds back across the horizontal (a band member's
			// ties UP the chain sort before its on-row edge; the "+one
			// step" nudge assumed the aligned end was the natural middle
			// and left the middle end BELOW the horizontal though its
			// partner lay above — the straights crossed at the exit). A
			// lone FLOW port keeps the nudge as it was: on a wide fan
			// into a composite the flow's approach index is a poor guide
			// to its dogleg arrivals, and ordering everything around it
			// crammed the fan into half the border.
			alignedIdx := -1
			if flowCount == 0 && alignedCount == 1 {
				for i, end := range ends {
					if aligned(end) {
						alignedIdx = i
					}
				}
			}
			step := 0.5 / float64(len(ends)+1)
			for i, end := range ends {
				pos := float64(i+1) / float64(len(ends)+1)
				if len(ends) == 2 {
					// a PAIR spreads at the quarters — thirds bunch the
					// two arrows toward the middle
					// ("step should be 1/4 ... 1/2 ... 1/4")
					pos = 0.25 + 0.5*float64(i)
				}
				switch {
				case flowCount == 1 && g.isFlow(end.edge):
					pos = 0.5
				case alignedIdx < 0 && (flowCount == 1 || alignedCount == 1) && pos == 0.5:
					pos += step
				case i == alignedIdx:
					pos = 0.5
				case alignedIdx >= 0 && i < alignedIdx:
					pos = math.Min(pos, 0.5-float64(alignedIdx-i)*step)
				case alignedIdx >= 0 && i > alignedIdx:
					pos = math.Max(pos, 0.5+float64(i-alignedIdx)*step)
				}
				pp := portPos[end.edge]
				if end.atFrom {
					pp[0] = pos
				} else {
					pp[1] = pos
				}
				portPos[end.edge] = pp
			}
			_ = n
		}
	}

	// Alignments the paired-port pass below MANUFACTURED. Such an
	// alignment is weaker evidence of a genuine vertical than it looks:
	// the pass moves only the TARGET, onto whatever slot the spread
	// handed the source.
	straightened := map[*edge]bool{}
	// paired-port coordination (v7P9: aligned neighbours connect with ONE
	// straight line): a non-flow edge with vertical ports between
	// column-sharing boxes aligns both ports on one x — the segment runs
	// truly vertical even when spreads pushed the two sides apart.
	// (Mirror for horizontal ports between row-sharing boxes.)
	portTaken := func(n int, side string, self *edge, pos float64) bool {
		for _, other := range g.edges {
			if other == self {
				continue
			}
			op := portPos[other]
			if other.from == n && sideOf[other.idx][0] == side {
				if d := op[0] - pos; d > -0.1 && d < 0.1 {
					return true
				}
			}
			if other.to == n && sideOf[other.idx][1] == side {
				if d := op[1] - pos; d > -0.1 && d < 0.1 {
					return true
				}
			}
		}
		return false
	}
	for _, e := range g.edges {
		if g.isFlow(e) {
			continue
		}
		f, t := g.nodes[e.from], g.nodes[e.to]
		sides := sideOf[e.idx]
		pp := portPos[e]
		vertical := (sides[0] == "bottom" || sides[0] == "top") &&
			(sides[1] == "bottom" || sides[1] == "top")
		horiz := (sides[0] == "left" || sides[0] == "right") &&
			(sides[1] == "left" || sides[1] == "right")
		if vertical && f.x < t.x+t.w && t.x < f.x+f.w {
			sx := f.x + int(float64(f.w)*pp[0])
			want := (float64(sx) - float64(t.x)) / float64(t.w)
			if want >= 0.1 && want <= 0.9 && !portTaken(e.to, sides[1], e, want) {
				pp[1] = want
				portPos[e] = pp
				straightened[e] = true
			}
		} else if horiz && f.y < t.y+t.h && t.y < f.y+f.h {
			sy := f.y + int(float64(f.h)*pp[0])
			want := (float64(sy) - float64(t.y)) / float64(t.h)
			if want >= 0.1 && want <= 0.9 && !portTaken(e.to, sides[1], e, want) {
				pp[1] = want
				portPos[e] = pp
				straightened[e] = true
			}
		}
	}

	port := func(e *edge, from bool) layout.EdgePort {
		i := 1
		if from {
			i = 0
		}
		return layout.EdgePort{Side: sideOf[e.idx][i], Position: portPos[e][i]}
	}
	point := func(n *node, p layout.EdgePort) (int, int) {
		return layout.EdgePortPoint(g.layoutNode(n), layout.Node{}, p)
	}

	// ---- 3. structural pass: straight by construction. ----
	// Flow edges avoid nodes (v7P9): when a boundary fan's straight line
	// would spear a box, the edge drops in its OWN lane and converges just
	// before the boundary (ends -> E), or steps aside right below S and
	// drops in the target's lane (S -> starts). leads-to NEVER hides.
	var blocked []*edge
	for _, e := range g.edges {
		if !e.structural {
			continue
		}
		r := routed{src: port(e, true), tgt: port(e, false)}
		sx, sy := point(g.nodes[e.from], r.src)
		tx, ty := point(g.nodes[e.to], r.tgt)
		straight := polyline{pts: [][2]int{{sx, sy}, {tx, ty}}, e: e}
		if g.hitsNode(straight.pts, e) {
			if g.isFlow(e) && (g.nodes[e.from].boundary || g.nodes[e.to].boundary) {
				// a boundary fan drops in its own lane / steps aside below
				// S — and BENDS AS EARLY as the boxes (with their visible
				// gap) allow: the earliest turn gives the steepest
				// diagonal, nearest the 45° ideal (e3→E
				// ran its full lane and swept flat under c6; the vertical
				// shortens to the first clean turn instead)
				// STRICT clearance for the early turn (no fan-sibling
				// exemption — the sweep measures every box): the diagonal
				// keeps the full visible gap from every box that is not an
				// endpoint of this edge.
				clear := func(pts [][2]int) bool {
					m := GridStep / 2
					for i := range g.nodes {
						n := g.nodes[i]
						if !n.placed || n.idx == e.from || n.idx == e.to {
							continue
						}
						for k := 0; k+1 < len(pts); k++ {
							if segIntersectsBox(pts[k], pts[k+1],
								n.x-m, n.y-m, n.x+n.w+m, n.y+n.h+m) {
								return false
							}
						}
					}
					return true
				}
				// The lane to drop in: the edge's own first; when a box sits
				// IN that lane (an aux the no-overlap floor stepped down into
				// the corridor — brains: hypothalamus's tall concept under
				// "ultimately expand…", whose line to E then speared it),
				// the lanes beside it, nearest first, up to ten grid steps
				// either way (past a 120px box and its gap). Before this the own lane was the only try, every
				// bend hit, and the straight through the box was kept.
				var bend layout.Position
				found := false
				lanes := []int{0}
				for k := 1; k <= 10; k++ { // up to ten grid steps aside: past a box and its gap
					lanes = append(lanes, -k*GridStep, k*GridStep)
				}
				for _, dx := range lanes {
					if g.nodes[e.to].boundary {
						lx := sx + dx
						for y := sy + GridStep; y < ty-BoundaryGap; y += GridStep {
							if clear([][2]int{{sx, sy}, {lx, y}, {tx, ty}}) {
								bend, found = layout.Position{X: lx, Y: y}, true
								break
							}
						}
					} else {
						lx := tx + dx
						for y := ty - GridStep; y > sy+BoundaryGap; y -= GridStep {
							if clear([][2]int{{sx, sy}, {lx, y}, {tx, ty}}) {
								bend, found = layout.Position{X: lx, Y: y}, true
								break
							}
						}
					}
					if found {
						break
					}
				}
				if !found {
					if g.nodes[e.to].boundary {
						bend = layout.Position{X: sx, Y: ty - BoundaryGap}
					} else {
						bend = layout.Position{X: tx, Y: sy + BoundaryGap}
					}
				}
				pts := [][2]int{{sx, sy}, {bend.X, bend.Y}, {tx, ty}}
				if !g.hitsNode(pts, e) {
					r = routed{src: r.src, tgt: r.tgt, bends: []layout.Position{bend}}
				}
			} else {
				// structural edges never hide (v7P9's hierarchy); a blocked
				// straight resolves cost-aware below, once every structural
				// line is known. A FLOW leads-to lands here too since sub-grid
				// boundaries: the skeleton keeps predecessor over successor in
				// a lane for top-level pairs, but a leads-to INTO a member from
				// an event beside the grid comes from the side and crossed the
				// members above its target (CFEngine: `editfiles field_edits ->
				// insert_lines` through delete_lines and field_edits) — it was
				// left straight because "flow never hits a box".
				blocked = append(blocked, e)
			}
		} else if !g.isFlow(e) && g.grazeCount(straight.pts, e) > 0 {
			// a clear straight that GRAZES a box (v7P8: no visible gap)
			// competes with the detours too — it usually survives as the
			// least-bad, but a clean flank wins when one exists
			blocked = append(blocked, e)
		}
		routes[e.idx] = r
	}

	// ---- 4. tie pass: candidates under the kind budget. ----
	var placedLines []polyline
	line := func(e *edge, r routed) polyline {
		sx, sy := point(g.nodes[e.from], r.src)
		tx, ty := point(g.nodes[e.to], r.tgt)
		pts := [][2]int{{sx, sy}}
		for _, b := range r.bends {
			pts = append(pts, [2]int{b.X, b.Y})
		}
		pts = append(pts, [2]int{tx, ty})
		return polyline{pts: pts, e: e}
	}
	// Blocked structural straights resolve COST-AWARE (v7P9 binds structural
	// aux too): the ALTERNATE-AXIS straight and both side-lane doglegs
	// compete on crossings against the other structural lines — never just
	// "first that clears the boxes" (a blind alternate-axis
	// take crossed a whole part-of fan; the far side lane was clean).
	for _, e := range blocked {
		f, t := g.nodes[e.from], g.nodes[e.to]
		r := routes[e.idx]
		altSrc := layout.EdgePort{Side: "right", Position: 0.5}
		altTgt := layout.EdgePort{Side: "left", Position: 0.5}
		if r.src.Side == "left" || r.src.Side == "right" {
			altSrc = layout.EdgePort{Side: "bottom", Position: 0.5}
			altTgt = layout.EdgePort{Side: "top", Position: 0.5}
			if t.y+t.h/2 < f.y+f.h/2 {
				altSrc.Side, altTgt.Side = "top", "bottom"
			}
		} else if t.x+t.w/2 < f.x+f.w/2 {
			altSrc.Side, altTgt.Side = "left", "right"
		}
		cands := []routed{{src: altSrc, tgt: altTgt}}
		// ALIGNED-overlap SLID straights, extended from ties to
		// structurals (v7P9 aligned-neighbours:
		// natural-thing→thing must leave the BOTTOM as one vertical —
		// the zero-leg side dogleg hugged the box corner down): the
		// straight slides within the boxes' overlap, both ports
		// together, centre-out.
		slidS := func(o0, o1 int, vertical bool) {
			if o1-o0 < GridStep {
				return
			}
			mid := (o0 + o1) / 2 / GridStep * GridStep
			for d := 0; d <= o1-o0; d += GridStep {
				for _, at := range []int{mid + d, mid - d} {
					if at < o0 || at > o1 {
						continue
					}
					var sp2, tp2 float64
					var sSide, tSide string
					if vertical {
						sp2 = float64(at-f.x) / float64(f.w)
						tp2 = float64(at-t.x) / float64(t.w)
						sSide, tSide = "bottom", "top"
						if f.y >= t.y+t.h {
							sSide, tSide = "top", "bottom"
						}
					} else {
						sp2 = float64(at-f.y) / float64(f.h)
						tp2 = float64(at-t.y) / float64(t.h)
						sSide, tSide = "right", "left"
						if f.x >= t.x+t.w {
							sSide, tSide = "left", "right"
						}
					}
					if sp2 < 0.15 || sp2 > 0.85 || tp2 < 0.15 || tp2 > 0.85 {
						continue
					}
					cands = append(cands, routed{
						src: layout.EdgePort{Side: sSide, Position: sp2},
						tgt: layout.EdgePort{Side: tSide, Position: tp2},
					})
					if d == 0 {
						break
					}
				}
			}
		}
		if t.y >= f.y+f.h || f.y >= t.y+t.h {
			slidS(maxInt(f.x, t.x), minInt(f.x+f.w, t.x+t.w), true)
		}
		if t.x >= f.x+f.w || f.x >= t.x+t.w {
			slidS(maxInt(f.y, t.y), minInt(f.y+f.h, t.y+t.h), false)
		}
		// MIXED doglegs ("go a bit above first and then
		// continue horizontally"): out of the source's top/bottom, ONE
		// bend, into the target's facing side — the return edge of a
		// second-in-row fork branch clears its row-mate just above (or
		// below) the row instead of hugging borders or looping the grid.
		hSide := "right"
		srcPoses := []float64{0.15, 0.3, 0.5}
		if t.x+t.w/2 > f.x+f.w/2 {
			hSide = "left"
			srcPoses = []float64{0.85, 0.7, 0.5}
		}
		for _, vs := range []string{"top", "bottom"} {
			for _, sp0 := range srcPoses {
				sp := layout.EdgePort{Side: vs, Position: sp0}
				tp := layout.EdgePort{Side: hSide, Position: 0.5}
				sx, sy2 := point(f, sp)
				tx2, ty := point(t, tp)
				if absInt(ty-sy2) < GridStep || absInt(tx2-sx) < GridStep {
					continue // v7P9: a dogleg leg is at least an arrowhead long
				}
				cands = append(cands, routed{src: sp, tgt: tp,
					bends: []layout.Position{{X: sx, Y: ty}}})
			}
		}
		vSide := "bottom"
		if t.y+t.h/2 > f.y+f.h/2 {
			vSide = "top"
		}
		for _, hs := range []string{"left", "right"} {
			sp := layout.EdgePort{Side: hs, Position: 0.5}
			tp := layout.EdgePort{Side: vSide, Position: 0.5}
			sx2, sy := point(f, sp)
			tx, ty2 := point(t, tp)
			if absInt(tx-sx2) < GridStep || absInt(ty2-sy) < GridStep {
				continue // v7P9: a dogleg leg is at least an arrowhead long
			}
			cands = append(cands, routed{src: sp, tgt: tp,
				bends: []layout.Position{{X: tx, Y: sy}}})
		}
		// lane offsets are ROOM-AWARE (v7P8: the lane
		// centres in the free gap on its flank — a fixed clearance ran
		// too close to the next row's arrows), capped at one clearance
		structOff := func(flank int) int { // 0 below, 1 above, 2 right, 3 left
			lo, hi := minInt(f.x, t.x), maxInt(f.x+f.w, t.x+t.w)
			vlo, vhi := minInt(f.y, t.y), maxInt(f.y+f.h, t.y+t.h)
			free := 1 << 30
			for _, n := range g.nodes {
				if !n.placed || n == f || n == t {
					continue
				}
				d := free
				switch flank {
				case 0:
					if n.x < hi && lo < n.x+n.w && n.y >= vhi {
						d = n.y - vhi
					}
				case 1:
					if n.x < hi && lo < n.x+n.w && n.y+n.h <= vlo {
						d = vlo - (n.y + n.h)
					}
				case 2:
					if n.y < vhi && vlo < n.y+n.h && n.x >= hi {
						d = n.x - hi
					}
				default:
					if n.y < vhi && vlo < n.y+n.h && n.x+n.w <= lo {
						d = lo - (n.x + n.w)
					}
				}
				if d < free {
					free = d
				}
			}
			off := free / 2
			if off > Clearance {
				off = Clearance
			}
			if off < GridStep/2 {
				off = GridStep / 2
			}
			return off
		}
		// structural lanes take the SAME 45° border-to-lane hops the tie
		// bypass uses (v7P9 "a SHORT 45° hop, border to lane":
		// the square stub from c to its row lane
		// "can be diagonal — that would look nicer")
		hop45 := func(v int) int {
			if v < 0 {
				v = -v
			}
			if v > Clearance {
				return Clearance
			}
			return v
		}
		laneL := minInt(f.x, t.x) - structOff(3)
		laneR := maxInt(f.x+f.w, t.x+t.w) + structOff(2)
		vdir2 := 1
		if t.y+t.h/2 < f.y+f.h/2 {
			vdir2 = -1
		}
		for _, lane := range []int{laneL, laneR} {
			side := "left"
			if lane == laneR {
				side = "right"
			}
			p := layout.EdgePort{Side: side, Position: 0.5}
			ssx, bsy := point(f, p)
			stx, bty := point(t, p)
			cands = append(cands, routed{src: p, tgt: p, bends: []layout.Position{
				{X: lane, Y: bsy + vdir2*hop45(lane-ssx)},
				{X: lane, Y: bty - vdir2*hop45(lane-stx)},
			}})
		}
		// horizontal lanes too: a second-in-row fork branch reaches its
		// composite UNDER the row, not through its row-mate (v7P3 grids)
		laneT := minInt(f.y, t.y) - structOff(1)
		laneB := maxInt(f.y+f.h, t.y+t.h) + structOff(0)
		hdir2 := 1
		if t.x+t.w/2 < f.x+f.w/2 {
			hdir2 = -1
		}
		for _, lane := range []int{laneB, laneT} {
			side := "bottom"
			if lane == laneT {
				side = "top"
			}
			p := layout.EdgePort{Side: side, Position: 0.5}
			bsx, ssy := point(f, p)
			btx, sty := point(t, p)
			cands = append(cands, routed{src: p, tgt: p, bends: []layout.Position{
				{X: bsx + hdir2*hop45(lane-ssy), Y: lane},
				{X: btx - hdir2*hop45(lane-sty), Y: lane},
			}})
		}
		var others []polyline
		for _, o := range g.edges {
			if o.structural && o != e {
				others = append(others, line(o, routes[o.idx]))
			}
		}
		basePl := line(e, r)
		baseCross := g.crossingCost(basePl, others)
		baseGraze := 0.5 * g.grazeCount(basePl.pts, e)
		baseHits := g.hitCount(basePl.pts, e)
		baseHit := baseHits > 0
		best, bestScore := r, baseCross+baseGraze+100*float64(baseHits)
		if g.tracing() {
			g.emitCandidate(e, "structural", -1, r, baseCross, baseGraze, 0, baseHit, false)
		}
		// a candidate must DEPART its ports: the first segment leaves
		// through the source side's outward normal and the last arrives
		// through the target's — a zero-height "vertical" exiting a top
		// port sideways runs along its own border by construction (the
		// degenerate that hid c→parent under three borders)
		// ... and at a READABLE angle: the port-normal component must be
		// at least the tangential component over tan 75° (the 150°-cap
		// ratio) — an edge meeting a border at a few degrees reads as a
		// corner landing (minimum acceptable angle).
		outward := func(side string, dx, dy int) bool {
			abs2 := func(v int) int {
				if v < 0 {
					return -v
				}
				return v
			}
			switch side {
			case "top":
				return dy < 0 && abs2(dy)*373 >= abs2(dx)*100
			case "bottom":
				return dy > 0 && abs2(dy)*373 >= abs2(dx)*100
			case "left":
				return dx < 0 && abs2(dx)*373 >= abs2(dy)*100
			}
			return dx > 0 && abs2(dx)*373 >= abs2(dy)*100
		}
		departs := func(c routed) bool {
			pl := line(e, c)
			pts := pl.pts
			if len(pts) < 2 {
				return false
			}
			return outward(c.src.Side, pts[1][0]-pts[0][0], pts[1][1]-pts[0][1]) &&
				outward(c.tgt.Side,
					pts[len(pts)-2][0]-pts[len(pts)-1][0],
					pts[len(pts)-2][1]-pts[len(pts)-1][1])
		}
		// selection follows the tie pass's discipline (v7P9: candidates
		// are tried in PREFERENCE order — alternate straight, mixed
		// doglegs, lanes — and the first under budget wins; a FREE
		// candidate anywhere beats a priced one UNLESS it exits away from
		// its partner). Cheapest-wins traded a one-bend toward-exit
		// dogleg for a two-bend away-exit C-loop over a quarter crossing
		// (murder-sk's leaf edges, "not needed to be
		// bent"). With nothing under budget the global minimum still
		// draws: a structural edge never hides (v7P9 hierarchy).
		facing := func(side string) bool {
			switch side {
			case "left":
				return t.x+t.w/2 <= f.x+f.w/2
			case "right":
				return t.x+t.w/2 >= f.x+f.w/2
			case "top":
				return t.y+t.h/2 <= f.y+f.h/2
			}
			return t.y+t.h/2 >= f.y+f.h/2
		}
		const budget = 1.0
		haveChoice := !baseHit && bestScore <= budget // the straight is the most preferred
		bestBends := 0
		for i, c := range cands {
			if !departs(c) {
				continue
			}
			pl := line(e, c)
			cross := g.crossingCost(pl, others)
			graze := 0.5 * g.grazeCount(pl.pts, e)
			hits := g.hitCount(pl.pts, e)
			hit := hits > 0
			score := cross + graze + 100*float64(hits)
			take := false
			switch {
			case !haveChoice && !hit && score <= budget:
				take = true // first under budget in preference order
			case !haveChoice && score < bestScore-1e-9:
				take = true // fallback minimum while nothing passes
			case haveChoice && bestScore > 0 && !hit && score == 0 && facing(c.src.Side) &&
				len(pl.pts)-2 <= bestBends+1:
				// a FREE toward-exit candidate beats the priced pick — but
				// never by ADDING more than one bend: a two-bend detour
				// around never beats a budgeted crossing on a direct shape
				// (same currency as the tie pass)
				take = true
			}
			if g.tracing() {
				g.emitCandidate(e, "structural", i, c, cross, graze, 0, hit, take)
			}
			if take {
				best, bestScore = c, score
				bestBends = len(pl.pts) - 2
				if !hit && score <= budget {
					haveChoice = true
				}
				if !hit && score == 0 && facing(c.src.Side) {
					break // nothing improves on a free toward-exit route
				}
			}
		}
		routes[e.idx] = best
	}

	for _, e := range g.edges {
		if e.structural {
			placedLines = append(placedLines, line(e, routes[e.idx]))
		}
	}

	visibleCount := map[int]int{}
	for _, e := range g.edges {
		visibleCount[e.from]++
		visibleCount[e.to]++
	}

	for _, e := range g.edges {
		if e.structural {
			continue
		}
		cands := g.tieCandidates(e, port, point)
		// first-fit by preference order — but a FREE candidate anywhere
		// beats a priced earlier one (the slid vertical
		// that avoids the graced crossing must win over the first slide
		// that merely stays under budget)
		chosen, ok := -1, false
		chosenClean := false // chosen is free AND exits toward its partner
		chosenGraze := 0.0   // the pick's graze component (a v7P8 violation)
		chosenBends := 0     // the pick's bend count (shape complexity)
		for i, c := range cands {
			pl := line(e, c)
			hit := g.hitsNode(pl.pts, e)
			var cross, graze, detour float64
			if !hit {
				cross = g.crossingCost(pl, placedLines)
				graze = 0.5 * g.grazeCount(pl.pts, e)
				detour = detourTax(pl.pts)
			}
			cost := cross + graze + detour
			pass := !hit && cost <= 1.0
			if c.leaned && cost > 0 {
				// a LEANED hop-diagonal is an aesthetic upgrade over the
				// tidy shapes — never worth a crossing: it qualifies only
				// clean (priced leaned beelines reshuffled ratified fans)
				pass = false
			}
			if g.tracing() {
				g.emitCandidate(e, "tie", i, c, cross, graze, detour, hit, pass)
			}
			if pass && !ok {
				chosen, ok = i, true
				chosenClean = false // updated below once facing is known
				chosenGraze = graze
				chosenBends = len(pl.pts) - 2
			}
			// a FREE candidate beats a priced one — but never one that
			// EXITS AWAY from its partner: trading a graced 0.25 brush at
			// the shared source for a C-loop out of the far side reads
			// worse, not better ("directly, no bending")
			facing := func(side string) bool {
				fn, tn := g.nodes[e.from], g.nodes[e.to]
				switch side {
				case "left":
					return tn.x+tn.w/2 <= fn.x+fn.w/2
				case "right":
					return tn.x+tn.w/2 >= fn.x+fn.w/2
				case "top":
					return tn.y+tn.h/2 <= fn.y+fn.h/2
				}
				return tn.y+tn.h/2 >= fn.y+fn.h/2
			}
			if pass && cost == 0 && facing(c.src.Side) {
				if chosen == i {
					chosenClean = true // the pick already is a clean exit
				}
				// a clean-free pick is never overridden — the override
				// exists to rescue PRICED or away-exiting picks, not to
				// let a later lane steal from an equally free diagonal
				// (a1→e1); leaned diagonals win only
				// positionally and never override anyone.
				// a leaned candidate may rescue a GRAZING pick (v7P8:
				// the visible gap is violated outright) but never a
				// merely-priced one (crossings are budgeted currency —
				// that reshuffled the ratified fans)
				// ... and a free rescue never ADDS more than one bend: a
				// two-bend detour around never beats a budgeted crossing
				// on a direct shape ("one crossing ...
				// is fine. better than this two bends and edge around")
				if !chosenClean && (!c.leaned || chosenGraze > 0) &&
					len(pl.pts)-2 <= chosenBends+1 {
					chosen = i
					break
				}
				if chosenClean {
					break
				}
			}
		}
		if !ok {
			// hide — unless this is an endpoint's last visible connection
			// (v7P9 guard), then the least-bad candidate draws anyway.
			if visibleCount[e.from] <= 1 || visibleCount[e.to] <= 1 {
				chosen = g.leastBad(e, cands, line, placedLines)
			} else {
				visibleCount[e.from]--
				visibleCount[e.to]--
				r := cands[0]
				r.stubbed = true
				routes[e.idx] = r
				continue
			}
		}
		routes[e.idx] = cands[chosen]
		placedLines = append(placedLines, line(e, cands[chosen]))
	}

	g.separateLanes(routes, point)

	// v7P8: an ARROWHEAD LANDING demands 1.5× the visible gap from
	// through-traffic — generalized from the S/E corridors (the demand
	// loop's first slice) to NODE-side landings
	// (Pods→expose ran right beside the arrows landing on creates
	// service resource's border). A lane's MIDDLE segment running
	// parallel to a border that hosts landings shifts AWAY until the
	// heads have their clearance — guarded: the shifted route must not
	// hit a box or pay new crossings.
	{
		const headClear = GridStep + GridStep/2
		type landing struct {
			x, y int
			side string
		}
		var lands []landing
		for _, o := range g.edges {
			ro := routes[o.idx]
			if ro.stubbed {
				continue
			}
			tx, ty := point(g.nodes[o.to], ro.tgt)
			lands = append(lands, landing{tx, ty, ro.tgt.Side})
		}
		for _, e := range g.edges {
			r := routes[e.idx]
			if r.stubbed || len(r.bends) < 2 {
				continue
			}
			for bi := 0; bi+1 < len(r.bends); bi++ {
				a, b := r.bends[bi], r.bends[bi+1]
				if a.X != b.X || a.Y == b.Y {
					continue // vertical middle segments only (first slice)
				}
				lo, hi := minInt(a.Y, b.Y), maxInt(a.Y, b.Y)
				shift := 0
				for _, ld := range lands {
					if ld.side != "left" && ld.side != "right" {
						continue
					}
					if ld.y < lo-GridStep || ld.y > hi+GridStep {
						continue
					}
					// the head grows OUTWARD from the border
					if ld.side == "right" && a.X >= ld.x && a.X-ld.x < headClear {
						if d := headClear - (a.X - ld.x); d > shift {
							shift = d
						}
					}
					if ld.side == "left" && a.X <= ld.x && ld.x-a.X < headClear {
						if d := headClear - (ld.x - a.X); d > shift {
							shift = d
						}
					}
				}
				if shift == 0 {
					continue
				}
				// shift AWAY from the landing's border, grid-snapped
				dir := 1
				for _, ld := range lands {
					if (ld.side == "right" && a.X >= ld.x && a.X-ld.x < headClear) ||
						(ld.side == "left" && a.X <= ld.x && ld.x-a.X < headClear) {
						if ld.side == "left" {
							dir = -1
						}
						break
					}
				}
				shift = (shift + GridStep - 1) / GridStep * GridStep
				trial := r
				trial.bends = append([]layout.Position(nil), r.bends...)
				trial.bends[bi].X += dir * shift
				trial.bends[bi+1].X += dir * shift
				pl := line(e, trial)
				var others []polyline
				for _, o := range g.edges {
					if o != e && !routes[o.idx].stubbed {
						others = append(others, line(o, routes[o.idx]))
					}
				}
				if g.hitsNode(pl.pts, e) ||
					g.crossingCost(pl, others) > g.crossingCost(line(e, r), others)+1e-9 {
					continue
				}
				routes[e.idx] = trial
				r = trial
			}
		}
	}

	// v7P9 OPTICAL spread ("to human eye the
	// arrow should be a bit lower to look like being in the middle";
	// "top and bottom arrows closer to the central one"): arrivals at
	// one border space by their VISIBLE HEAD SPANS, not their tips — a
	// steep line's arrowhead occupies a run of the border (one arrowhead
	// length projected along it), a perpendicular line's almost none.
	// Middles equalize the CLEAR gaps between neighbouring spans;
	// unanchored extremes compact to one visible gap from their inner
	// neighbour. Pinned ends anchor the system: flow ports, and any
	// straight that is axis-aligned (moving it would tilt a ratified
	// horizontal/vertical). Order is preserved; every move is
	// hit-guarded.
	{
		type okey struct {
			n    int
			side string
		}
		type oent struct {
			e      *edge
			atFrom bool
		}
		groupsO := map[okey][]oent{}
		for _, e := range g.edges {
			r := routes[e.idx]
			if r.stubbed || (e.structural && g.isFlow(e)) {
				continue
			}
			groupsO[okey{e.from, r.src.Side}] = append(groupsO[okey{e.from, r.src.Side}], oent{e, true})
			groupsO[okey{e.to, r.tgt.Side}] = append(groupsO[okey{e.to, r.tgt.Side}], oent{e, false})
		}
		var keysO []okey
		for k := range groupsO {
			keysO = append(keysO, k)
		}
		sort.Slice(keysO, func(a, b int) bool {
			if keysO[a].n != keysO[b].n {
				return keysO[a].n < keysO[b].n
			}
			return keysO[a].side < keysO[b].side
		})
		for _, k := range keysO {
			ends := groupsO[k]
			if len(ends) < 2 {
				continue
			}
			n := g.nodes[k.n]
			vert := k.side == "left" || k.side == "right" // border runs in y
			span := n.h
			base := n.y
			if !vert {
				span = n.w
				base = n.x
			}
			type slot struct {
				ent    oent
				tip    float64 // along-border coordinate
				sLen   float64 // head-span length along the border
				before bool    // span sits BEFORE the tip (line arrives from lower coords)
				pinned bool
			}
			var sl []slot
			ok2 := true
			for _, ee := range ends {
				r := routes[ee.e.idx]
				fn, tn := g.nodes[ee.e.from], g.nodes[ee.e.to]
				sxp, syp := point(fn, r.src)
				txp, typ := point(tn, r.tgt)
				// last segment into this end
				var a, b [2]int
				if ee.atFrom {
					b = [2]int{sxp, syp}
					a = [2]int{txp, typ}
					if len(r.bends) > 0 {
						a = [2]int{r.bends[0].X, r.bends[0].Y}
					}
				} else {
					b = [2]int{txp, typ}
					a = [2]int{sxp, syp}
					if len(r.bends) > 0 {
						a = [2]int{r.bends[len(r.bends)-1].X, r.bends[len(r.bends)-1].Y}
					}
				}
				dx, dy := float64(b[0]-a[0]), float64(b[1]-a[1])
				l := math.Hypot(dx, dy)
				if l < 1 {
					ok2 = false
					break
				}
				uAlong := dy / l
				tip := float64(b[1])
				if !vert {
					uAlong = dx / l
					tip = float64(b[0])
				}
				pinned := len(r.bends) == 0 &&
					((vert && syp == typ) || (!vert && sxp == txp))
				sLen := float64(GridStep) * math.Abs(uAlong)
				if ee.atFrom {
					// an EXIT has no arrowhead: it anchors the spacing
					// but never moves and occupies no head span
					pinned, sLen = true, 0
				}
				sl = append(sl, slot{ee, tip, sLen, uAlong > 0, pinned})
			}
			if !ok2 || len(sl) < 3 {
				// a PAIR keeps its quarter slots: with no middle between
				// them, the two "extremes" would compact into each other
				continue
			}
			sort.SliceStable(sl, func(a, b int) bool { return sl[a].tip < sl[b].tip })
			lo2 := func(s2 slot) float64 {
				if s2.before {
					return s2.tip - s2.sLen
				}
				return s2.tip
			}
			hi2 := func(s2 slot) float64 {
				if s2.before {
					return s2.tip
				}
				return s2.tip + s2.sLen
			}
			minP, maxP := float64(base)+0.1*float64(span), float64(base)+0.9*float64(span)
			gapT := float64(GridStep / 2)
			for iter := 0; iter < 12; iter++ {
				moved := false
				for i := range sl {
					if sl[i].pinned {
						continue
					}
					var want float64
					switch {
					case i > 0 && i < len(sl)-1:
						room := lo2(sl[i+1]) - hi2(sl[i-1])
						free := room - sl[i].sLen
						off := free / 2
						want = hi2(sl[i-1]) + off
						if sl[i].before {
							want += sl[i].sLen
						}
					case i == 0 && len(sl) > 1:
						want = lo2(sl[1]) - gapT
						if !sl[i].before {
							want -= sl[i].sLen
						}
						// extremes move INWARD only: compaction never
						// drags ink toward the corner — a crowded head
						// is separated by the MIDDLE giving way
						if want < sl[i].tip {
							want = sl[i].tip
						}
					default:
						want = hi2(sl[len(sl)-2]) + gapT
						if sl[i].before {
							want += sl[i].sLen
						}
						if want > sl[i].tip {
							want = sl[i].tip
						}
					}
					if want < minP {
						want = minP
					}
					if want > maxP {
						want = maxP
					}
					// order guard: keep strictly between neighbours' tips
					if i > 0 && want <= sl[i-1].tip+2 {
						want = sl[i-1].tip + 2
					}
					if i < len(sl)-1 && want >= sl[i+1].tip-2 {
						want = sl[i+1].tip - 2
					}
					if math.Abs(want-sl[i].tip) >= 1 {
						sl[i].tip = want
						moved = true
					}
				}
				if !moved {
					break
				}
			}
			for _, s2 := range sl {
				r := routes[s2.ent.e.idx]
				pp := &r.tgt
				if s2.ent.atFrom {
					pp = &r.src
				}
				newPos := (s2.tip - float64(base)) / float64(span)
				if math.Abs(newPos-pp.Position) < 1e-9 {
					continue
				}
				old := pp.Position
				pp.Position = newPos
				pl := line(s2.ent.e, r)
				if g.hitsNode(pl.pts, s2.ent.e) {
					pp.Position = old
					continue
				}
				routes[s2.ent.e.idx] = r
			}
		}
	}

	// v7P9 CORNER landings ("in this special
	// case we can allow to target corners — e.g. the top-left corner for
	// edges with such angle as both targeting top side and left side"):
	// a vertical-port ARRIVAL whose line runs SHALLOWER than 45° suits
	// two faces at once — mid-border it reads weird, so its tip slides
	// INTO the corner nearest its approach and the line bisects both
	// faces. Deliberate corner tips only, and only for the OUTERMOST
	// arrival on its border (a fan keeps its approach order — the corner
	// is the extreme slot); every slide is hit-guarded.
	{
		type ckey struct {
			n    int
			side string
		}
		cgroups := map[ckey][]*edge{}
		for _, e := range g.edges {
			r := routes[e.idx]
			if r.stubbed || (e.structural && g.isFlow(e)) {
				continue
			}
			if r.tgt.Side != "top" && r.tgt.Side != "bottom" {
				continue
			}
			cgroups[ckey{e.to, r.tgt.Side}] = append(cgroups[ckey{e.to, r.tgt.Side}], e)
		}
		var ckeys []ckey
		for k := range cgroups {
			ckeys = append(ckeys, k)
		}
		sort.Slice(ckeys, func(a, b int) bool {
			if ckeys[a].n != ckeys[b].n {
				return ckeys[a].n < ckeys[b].n
			}
			return ckeys[a].side < ckeys[b].side
		})
		lastSeg := func(e *edge) ([2]int, [2]int) {
			r := routes[e.idx]
			fn, tn := g.nodes[e.from], g.nodes[e.to]
			sxp, syp := point(fn, r.src)
			txp, typ := point(tn, r.tgt)
			a := [2]int{sxp, syp}
			if len(r.bends) > 0 {
				a = [2]int{r.bends[len(r.bends)-1].X, r.bends[len(r.bends)-1].Y}
			}
			return a, [2]int{txp, typ}
		}
		linesBut := func(skip *edge) []polyline {
			var out []polyline
			for _, o := range g.edges {
				if o == skip || routes[o.idx].stubbed {
					continue
				}
				out = append(out, line(o, routes[o.idx]))
			}
			return out
		}
		for _, k := range ckeys {
			es := cgroups[k]
			sort.SliceStable(es, func(a, b int) bool {
				ra, rb := routes[es[a].idx], routes[es[b].idx]
				return ra.tgt.Position < rb.tgt.Position
			})
			trySide := func(e *edge, fromLeft bool) {
				a, b := lastSeg(e)
				dx, dy := b[0]-a[0], b[1]-a[1]
				if absInt(dx) <= absInt(dy) || dy == 0 {
					return // 45° or steeper — the border slot is honest
				}
				if (fromLeft && dx <= 0) || (!fromLeft && dx >= 0) {
					return // approach direction must face that flank
				}
				// only for a VERY FLAT arrival — flatter than 3:1
				// (~18°): a near-horizontal line onto a top border reads
				// wrong however open the border is
				// (black/white → color, and the shared diamond's cS and
				// cB), while a 2:1-ish V-fork keeps its quarter slots
				// (the ratified A→cX←B 1/4…1/2…1/4). A corner tip
				// floats off the rounded outline — the side is flush.
				if absInt(dx) < 3*absInt(dy) {
					return // 3:1 and flatter converts; steeper keeps slots
				}
				// ... and never out of a WIDE fan: a hub with three or
				// more same-rel children lands them uniformly on their
				// tops (the ratified sibling-cluster fan) — the flush
				// side is for the narrow shared-target weave
				fanN := 0
				for _, o := range g.out[e.from] {
					if o.rel == e.rel && g.nodes[o.to].kind == g.nodes[e.to].kind {
						fanN++
					}
				}
				if fanN >= 3 {
					return
				}
				side := "left"
				if !fromLeft {
					side = "right"
				}
				// the flush side slot: upper quarter for an arrival from
				// above, lower for one from below
				pos := 0.25
				if k.side == "bottom" {
					pos = 0.75
				}
				// the side must be free — another end there keeps its
				// border to itself
				for _, o := range g.edges {
					ro := routes[o.idx]
					if o != e && ((o.to == e.to && ro.tgt.Side == side) ||
						(o.from == e.to && ro.src.Side == side)) {
						return
					}
				}
				r := routes[e.idx]
				others := linesBut(e)
				before := g.crossingCost(line(e, r), others)
				oldSide, oldPos := r.tgt.Side, r.tgt.Position
				r.tgt.Side, r.tgt.Position = side, pos
				pl := line(e, r)
				// the NEW segment must still run SHALLOW into the side —
				// the pre-conversion segment can be shallow while the
				// converted one turns near-vertical
				// (declarative/configuration — a steep line touching a
				// side border is the corner-ish landing v7P9 bans)
				la := pl.pts[len(pl.pts)-2]
				lb := pl.pts[len(pl.pts)-1]
				steep := absInt(lb[0]-la[0]) <= absInt(lb[1]-la[1])
				// a side landing never pays: hit, new crossings, or a
				// new graze all revert (the slide is an aesthetic upgrade)
				if steep || g.hitsNode(pl.pts, e) ||
					g.crossingCost(pl, others) > before+1e-9 ||
					g.grazeCount(pl.pts, e) > 0 {
					r.tgt.Side, r.tgt.Position = oldSide, oldPos
					return
				}
				routes[e.idx] = r
			}
			trySide(es[0], true)
			if len(es) > 1 {
				trySide(es[len(es)-1], false)
			} else {
				// a SINGLE arrival tries both directions — the guards
				// let only the one matching its approach act
				// (declarative kept a shallow top landing
				// while its mirror sibling configuration went flush)
				trySide(es[0], false)
			}
		}
	}

	// v7P9 slot RE-CENTRE (s1—s2 ran at the quarter,
	// tB—tC likewise): the spread assigns slots BEFORE routing — when a
	// co-slotted edge later leaves for a flank lane, the survivor keeps
	// an off-centre slot on a border it now owns alone. Slots re-derive
	// from the FINAL side membership: a sole bend-free end on its
	// spread-assigned side takes the centre, a pair the quarters; three
	// or more keep their spread (the optical pass owns them). Flow
	// ports pin their side; flush/lane ends (side differs from the
	// spread's) never move; every change is hit-guarded.
	{
		// alignment snapshot: a bend-free tie that ran axis-aligned must
		// stay aligned — a one-sided re-centre would tilt it off the
		// row's midline (the e2—shared horizontal)
		type endPts struct {
			sx, sy, tx, ty int
		}
		before := map[*edge]endPts{}
		for _, e := range g.edges {
			r := routes[e.idx]
			if r.stubbed || len(r.bends) > 0 {
				continue
			}
			fn, tn := g.nodes[e.from], g.nodes[e.to]
			sx, sy := point(fn, r.src)
			tx, ty := point(tn, r.tgt)
			before[e] = endPts{sx, sy, tx, ty}
		}
		type rkey struct {
			n    int
			side string
		}
		grp := map[rkey][]edgeEnd{}
		var rkeys []rkey
		addR := func(k rkey, ee edgeEnd) {
			if _, ok := grp[k]; !ok {
				rkeys = append(rkeys, k)
			}
			grp[k] = append(grp[k], ee)
		}
		for _, e := range g.edges {
			r := routes[e.idx]
			if r.stubbed {
				continue
			}
			addR(rkey{e.from, r.src.Side}, edgeEnd{e, true})
			addR(rkey{e.to, r.tgt.Side}, edgeEnd{e, false})
		}
		sort.Slice(rkeys, func(a, b int) bool {
			if rkeys[a].n != rkeys[b].n {
				return rkeys[a].n < rkeys[b].n
			}
			return rkeys[a].side < rkeys[b].side
		})
		abandoned := map[*edge]bool{}
		wantSrc := map[*edge]float64{}
		wantTgt := map[*edge]float64{}
		for _, k := range rkeys {
			ends := grp[k]
			if len(ends) > 2 {
				continue // three or more: the optical pass owns the side
			}
			pinned := false
			var mov []edgeEnd
			for _, ee := range ends {
				if ee.edge.structural && g.isFlow(ee.edge) {
					pinned = true
					continue
				}
				r := routes[ee.edge.idx]
				if len(r.bends) > 0 {
					pinned = true // a lane's quarter slot is ratified
					continue
				}
				want := sideOf[ee.edge.idx][1]
				pp := r.tgt
				if ee.atFrom {
					want = sideOf[ee.edge.idx][0]
					pp = r.src
				}
				if pp.Side != want {
					continue // flush/lane relocation — not ours
				}
				mov = append(mov, ee)
			}
			if pinned || len(mov) == 0 || len(mov) > 2 {
				continue
			}
			// The co-slotted edge LEFT: this end's slot is abandoned.
			if len(mov) == 1 && spreadN[k.n][k.side] > len(mov) {
				abandoned[mov[0].edge] = true
			}
			wantPos := []float64{0.5}
			if len(mov) == 2 {
				wantPos = []float64{0.25, 0.75}
				sort.SliceStable(mov, func(a, b int) bool {
					ra, rb := routes[mov[a].edge.idx], routes[mov[b].edge.idx]
					pa, pb := ra.tgt.Position, rb.tgt.Position
					if mov[a].atFrom {
						pa = ra.src.Position
					}
					if mov[b].atFrom {
						pb = rb.src.Position
					}
					return pa < pb
				})
			}
			for i, ee := range mov {
				if ee.atFrom {
					wantSrc[ee.edge] = wantPos[i]
				} else {
					wantTgt[ee.edge] = wantPos[i]
				}
			}
		}
		// apply per EDGE — both ends together, so an aligned pair moves
		// coherently instead of the first move tripping the guard
		for _, e := range g.edges {
			ws, okS := wantSrc[e]
			wt, okT := wantTgt[e]
			if !okS && !okT {
				continue
			}
			r := routes[e.idx]
			oldS, oldT := r.src.Position, r.tgt.Position
			if okS {
				r.src.Position = ws
			}
			if okT {
				r.tgt.Position = wt
			}
			if r.src.Position == oldS && r.tgt.Position == oldT {
				continue
			}
			pl := line(e, r)
			bad := g.hitsNode(pl.pts, e)
			recentring := okS && okT && ws == 0.5 && wt == 0.5
			if bp, ok := before[e]; ok && !bad &&
				!(straightened[e] && abandoned[e] && recentring) {
				fn, tn := g.nodes[e.from], g.nodes[e.to]
				nsx, nsy := point(fn, r.src)
				ntx, nty := point(tn, r.tgt)
				if (bp.sy == bp.ty && nsy != nty) ||
					(bp.sx == bp.tx && nsx != ntx) {
					bad = true
				}
			}
			if bad {
				continue
			}
			routes[e.idx] = r
		}
	}

	// v7P9: parallel segments never COVER each other — enforced AFTER
	// the spread and lane passes (candidate-time ports sit at the centre
	// and would falsely flag fans the spread separates). A tie still
	// lying on another drawn line hides: the outlet when no lane has
	// room (twin flank lanes that chose one x and could not nest). The
	// LONGER tie hides (the kind-internal geometry tie-break), and a
	// node's last visible connection never does.
	{
		length := func(pts [][2]int) int {
			l := 0
			for k := 0; k+1 < len(pts); k++ {
				l += absInt(pts[k+1][0]-pts[k][0]) + absInt(pts[k+1][1]-pts[k][1])
			}
			return l
		}
		var drawn []polyline
		for _, e := range g.edges {
			if !routes[e.idx].stubbed {
				drawn = append(drawn, line(e, routes[e.idx]))
			}
		}
		for _, e := range g.edges {
			if e.structural || routes[e.idx].stubbed {
				continue
			}
			pl := line(e, routes[e.idx])
			for _, o := range drawn {
				if o.e == e || routes[o.e.idx].stubbed {
					continue
				}
				if !coversAny(pl, []polyline{o}) {
					continue
				}
				victim := e
				if !o.e.structural && length(o.pts) > length(pl.pts) {
					victim = o.e
				}
				if visibleCount[victim.from] <= 1 || visibleCount[victim.to] <= 1 {
					continue // the last visible connection never hides
				}
				visibleCount[victim.from]--
				visibleCount[victim.to]--
				r := routes[victim.idx]
				r.stubbed = true
				routes[victim.idx] = r
				if victim == e {
					break
				}
			}
		}
	}

	// v7P9 FINAL invariant (C→e4 crossed e2→e4 — the
	// leaned arrival had taken the steeper line's slot): entries at one
	// border keep their APPROACH order. After every pass that moves
	// ports (leaned hops carry their own lean, the spread
	// redistributes), each side's movable arrivals re-sort: the slot
	// SET stays, the assignment follows the approach angle.
	{
		type sideKey struct {
			n    int
			side string
		}
		groups := map[sideKey][]edgeEnd{}
		var keys []sideKey
		add := func(k sideKey, ee edgeEnd) {
			if _, ok := groups[k]; !ok {
				keys = append(keys, k)
			}
			groups[k] = append(groups[k], ee)
		}
		for _, e := range g.edges {
			r := routes[e.idx]
			if r.stubbed || (e.structural && g.isFlow(e)) {
				continue
			}
			add(sideKey{e.from, r.src.Side}, edgeEnd{e, true})
			add(sideKey{e.to, r.tgt.Side}, edgeEnd{e, false})
		}
		sort.Slice(keys, func(a, b int) bool {
			if keys[a].n != keys[b].n {
				return keys[a].n < keys[b].n
			}
			return keys[a].side < keys[b].side
		})
		for _, k := range keys {
			ends := groups[k]
			if len(ends) < 2 {
				continue
			}
			n := g.nodes[k.n]
			angle := func(ee edgeEnd) float64 {
				// the approach IS the last leg: a bent route arrives from
				// its final bend, not from its source's centre — sorting
				// by the source put a horizontally-arriving lane below a
				// diagonal it then crossed (my-nginx
				// Deployment's demoted tie × kubectl at create)
				o := g.nodes[g.otherEnd(ee)]
				ox, oy := float64(o.x+o.w/2), float64(o.y+o.h/2)
				r := routes[ee.edge.idx]
				if len(r.bends) > 0 {
					b := r.bends[0]
					if !ee.atFrom {
						b = r.bends[len(r.bends)-1]
					}
					ox, oy = float64(b.X), float64(b.Y)
				}
				dy := oy - float64(n.y+n.h/2)
				dx := ox - float64(n.x+n.w/2)
				a := math.Atan2(dy, dx)
				switch k.side {
				case "left":
					if a < 0 {
						a += 2 * math.Pi
					}
					return -a
				case "right":
					return a
				case "top":
					if a <= 0 {
						a += 2 * math.Pi
					}
					return a
				}
				return -a
			}
			pos := func(ee edgeEnd) float64 {
				r := routes[ee.edge.idx]
				if ee.atFrom {
					return r.src.Position
				}
				return r.tgt.Position
			}
			slots := make([]float64, len(ends))
			for i, ee := range ends {
				slots[i] = pos(ee)
			}
			sort.Float64s(slots)
			order := make([]edgeEnd, len(ends))
			copy(order, ends)
			sort.SliceStable(order, func(a, b int) bool { return angle(order[a]) < angle(order[b]) })
			for i, ee := range order {
				r := routes[ee.edge.idx]
				pp, bendAt := &r.src, 0
				if !ee.atFrom {
					pp, bendAt = &r.tgt, len(r.bends)-1
				}
				d := slots[i] - pp.Position
				if d == 0 {
					continue
				}
				cand := r
				cand.bends = append([]layout.Position(nil), r.bends...)
				cpp := &cand.src
				if !ee.atFrom {
					cpp = &cand.tgt
				}
				if len(cand.bends) > 0 && bendAt >= 0 {
					if k.side == "top" || k.side == "bottom" {
						cand.bends[bendAt].X += int(d * float64(n.w))
					} else {
						cand.bends[bendAt].Y += int(d * float64(n.h))
					}
				}
				cpp.Position = slots[i]
				// the re-slot may trade a crossing for a graze (cheaper by
				// the v7P9 currency) but never for a box HIT
				sx2, sy2 := point(g.nodes[ee.edge.from], cand.src)
				tx2, ty2 := point(g.nodes[ee.edge.to], cand.tgt)
				ps := [][2]int{{sx2, sy2}}
				for _, bd := range cand.bends {
					ps = append(ps, [2]int{bd.X, bd.Y})
				}
				ps = append(ps, [2]int{tx2, ty2})
				if g.hitsNode(ps, ee.edge) {
					continue
				}
				routes[ee.edge.idx] = cand
			}
		}
	}

	// v7P8 §4: a STRANDED sole leaf posts a row-gap
	// demand. The floors parked the leaf more than a row pitch below its
	// expresser because every near-anchor wedge is swept by a successor's
	// part-of diagonal; the fix is GROWTH, not distance (the Scarlet
	// rule: add vertical space and place the node closer):
	// the successor's row drops until the diagonal passes a full visible
	// gap under the wedge beside the flow corridor, and the layout
	// re-solves once — the rescue then lands the leaf next to its owner.
	for ai, b := range g.nodes {
		if b.kind != KindConcept || b.boundary || !b.placed {
			continue
		}
		deg := 0
		var owner *node
		oi := -1
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
			owner, oi = g.nodes[o], o
		}
		if !okLeaf || deg != 1 || owner == nil || owner.kind != KindEvent ||
			b.y < owner.y+owner.h || b.y-(owner.y+owner.h) <= RowPitch {
			continue
		}
		m := GridStep / 2
		spotBot := owner.y + owner.h + StackGap + b.h
		corX := owner.x + owner.w/2
		for _, se := range g.out[oi] {
			if se.rel != RelLeadsTo || g.nodes[se.to].kind != KindEvent ||
				g.nodes[se.to].boundary {
				continue
			}
			succ := g.nodes[se.to]
			for _, pe := range g.out[se.to] {
				if pe.rel != RelPartOf || g.nodes[pe.to].kind != KindEvent {
					continue
				}
				tn := g.nodes[pe.to]
				if !tn.placed || tn.boundary {
					continue
				}
				// the wedge box hugs the corridor on the target's side
				var x0, x1, tx2, sx2 int
				ty2 := tn.y + tn.h/2
				if tn.x+tn.w/2 < corX {
					x1 = corX - m - m
					x0 = x1 - b.w
					tx2 = tn.x + tn.w
					sx2 = succ.x
				} else {
					x0 = corX + m + m
					x1 = x0 + b.w
					tx2 = tn.x
					sx2 = succ.x + succ.w
				}
				// the diagonal descends from the target's centre toward
				// the successor: it is LOWEST — the clearance binds — at
				// the box edge NEAREST the target
				xe := x0 - m
				if absInt(x1+m-tx2) < absInt(xe-tx2) {
					xe = x1 + m
				}
				span := absInt(sx2 - tx2)
				fr := float64(absInt(xe-tx2)) / float64(span)
				if span == 0 || fr <= 0.05 || fr > 1 {
					continue
				}
				needCy := float64(ty2) + float64(spotBot+2*m-ty2)/fr
				want := int(math.Ceil(needCy)) - succ.h/2
				if want > succ.y {
					if extra := gridUp(want - succ.y); extra > g.rowExtra[oi] {
						g.rowExtra[oi] = extra
					}
				}
			}
		}
	}

	// v7P8 §4, first slice: a corridor LANE too close
	// to a flow ARROWHEAD posts a demand — the arrowhead side needs 1.5×
	// the visible gap (a landing head reads bigger than a line), the far
	// side one full gap; the S/E boundary edge lengthens and the layout
	// re-solves once.
	const arrowClear = GridStep + GridStep/2
	const tailClear = GridStep
	for _, e := range g.edges {
		r := routes[e.idx]
		if r.stubbed || (e.structural && g.isFlow(e)) {
			continue
		}
		pl := line(e, r)
		for k := 0; k+1 < len(pl.pts); k++ {
			a, b := pl.pts[k], pl.pts[k+1]
			if a[1] != b[1] || a[0] == b[0] {
				continue // horizontal lane segments only
			}
			y := a[1]
			lo, hi := minInt(a[0], b[0]), maxInt(a[0], b[0])
			for _, f := range g.edges {
				if !f.structural || !g.isFlow(f) {
					continue
				}
				fn, tn := g.nodes[f.from], g.nodes[f.to]
				if fn.boundary && !tn.boundary && // S → start corridor
					e.from != f.to && e.to != f.to && // through-traffic only:
					// an edge ARRIVING at the corridor's own event shares
					// the border via port spread, it does not lane past
					lo < tn.x+tn.w && tn.x < hi &&
					y > fn.y+fn.h-1 && y < tn.y {
					room := tn.y - (fn.y + fn.h)
					if tn.y-y < arrowClear || y-(fn.y+fn.h) < tailClear {
						if need := gridUp(arrowClear + tailClear - room); need > g.sExtra[fn.comp] {
							g.sExtra[fn.comp] = need
						}
					}
				}
				if tn.boundary && !fn.boundary && // end → E corridor
					e.from != f.from && e.to != f.from && // through-traffic only
					lo < fn.x+fn.w && fn.x < hi &&
					y > fn.y+fn.h && y < tn.y+1 {
					room := tn.y - (fn.y + fn.h)
					if y-(fn.y+fn.h) < tailClear || tn.y-y < arrowClear {
						if need := gridUp(arrowClear + tailClear - room); need > g.eExtra[tn.comp] {
							g.eExtra[tn.comp] = need
						}
					}
				}
			}
		}
	}
	return routes
}

// coversAny reports whether a candidate polyline lies ON an already
// placed line (v7P9: parallel segments never cover each other): an
// axis-aligned segment parallel to a placed one, closer than 8px, with
// more than 16px of shared run — the exact criterion of the sweep
// (pkg/layoutcheck), so engine and checker agree.
func coversAny(pl polyline, placed []polyline) bool {
	segs := func(p polyline) [][4]int { // {horizontal 1/0, coord, lo, hi}
		var out [][4]int
		for k := 0; k+1 < len(p.pts); k++ {
			a, b := p.pts[k], p.pts[k+1]
			switch {
			case a[1] == b[1] && a[0] != b[0]:
				out = append(out, [4]int{1, a[1], minInt(a[0], b[0]), maxInt(a[0], b[0])})
			case a[0] == b[0] && a[1] != b[1]:
				out = append(out, [4]int{0, a[0], minInt(a[1], b[1]), maxInt(a[1], b[1])})
			}
		}
		return out
	}
	mine := segs(pl)
	for _, o := range placed {
		if o.e == pl.e {
			continue
		}
		for _, os := range segs(o) {
			for _, ms := range mine {
				if ms[0] != os[0] {
					continue
				}
				d := ms[1] - os[1]
				if d < 0 {
					d = -d
				}
				if d < 8 && minInt(ms[3], os[3])-maxInt(ms[2], os[2]) > 16 {
					return true
				}
			}
		}
	}
	return false
}

// separateLanes enforces v7P9's no-covering rule: parallel segments never
// lie on each other — every pair keeps at least LaneSep apart (several
// times the stroke width), and two edges never leave one point. Flow and
// structural edges hold their corridors; ties shift, lane segments by
// their bends, straight ties by both ports together.
func (g *graph) separateLanes(routes []routed, point func(*node, layout.EdgePort) (int, int)) {
	const LaneSep = GridStep / 2 // ≥3× the stroke width

	type seg struct {
		horizontal bool
		coord      int // y of a horizontal segment, x of a vertical one
		lo, hi     int
	}
	var taken []seg
	overlap := func(a, b seg) bool {
		return a.horizontal == b.horizontal &&
			a.coord-b.coord < LaneSep && b.coord-a.coord < LaneSep &&
			a.lo < b.hi && b.lo < a.hi
	}
	segsOf := func(e *edge, r routed) []seg {
		sx, sy := point(g.nodes[e.from], r.src)
		tx, ty := point(g.nodes[e.to], r.tgt)
		pts := [][2]int{{sx, sy}}
		for _, b := range r.bends {
			pts = append(pts, [2]int{b.X, b.Y})
		}
		pts = append(pts, [2]int{tx, ty})
		var out []seg
		for i := 0; i+1 < len(pts); i++ {
			a, b := pts[i], pts[i+1]
			switch {
			case a[1] == b[1] && a[0] != b[0]:
				lo, hi := a[0], b[0]
				if lo > hi {
					lo, hi = hi, lo
				}
				out = append(out, seg{true, a[1], lo, hi})
			case a[0] == b[0] && a[1] != b[1]:
				lo, hi := a[1], b[1]
				if lo > hi {
					lo, hi = hi, lo
				}
				out = append(out, seg{false, a[0], lo, hi})
			}
		}
		return out
	}
	conflicted := func(e *edge, r routed) bool {
		for _, sg := range segsOf(e, r) {
			for _, t := range taken {
				if overlap(sg, t) {
					return true
				}
			}
		}
		return false
	}

	// pass 1: FLOW corridors register and never move (v7P6); everything
	// else — structural aux included — shifts off occupied lanes
	for _, e := range g.edges {
		if e.structural && g.isFlow(e) {
			taken = append(taken, segsOf(e, routes[e.idx])...)
		}
	}
	// Same-flank lanes NEST (v7P9): a shorter tie dives back up sooner, so
	// it takes the INNER lane — aux edges claim lanes nearest-first
	// (tW's ties to two layers crossed each other).
	span := func(e *edge) int {
		f, t := g.nodes[e.from], g.nodes[e.to]
		dx := (t.x + t.w/2) - (f.x + f.w/2)
		dy := (t.y + t.h/2) - (f.y + f.h/2)
		if dx < 0 {
			dx = -dx
		}
		if dy < 0 {
			dy = -dy
		}
		return dx + dy
	}
	auxOrder := make([]*edge, 0, len(g.edges))
	for _, e := range g.edges {
		if !(e.structural && g.isFlow(e)) {
			auxOrder = append(auxOrder, e)
		}
	}
	sort.SliceStable(auxOrder, func(a, b int) bool {
		return span(auxOrder[a]) < span(auxOrder[b])
	})
	// two edges never leave one point (v7P9): dedup port points per
	// (node, side); the later tie steps aside.
	type portKey struct {
		n    int
		side string
	}
	seen := map[portKey][]float64{}
	claim := func(n int, side string, pos float64, prefDir float64) float64 {
		k := portKey{n, side}
		dir := prefDir
		if dir == 0 {
			dir = 0.15
			if pos > 0.5 {
				dir = -0.15
			}
		}
		for guard := 0; guard < 8; guard++ {
			clear := true
			for _, p := range seen[k] {
				if pos-p < 0.06 && p-pos < 0.06 {
					clear = false
					break
				}
			}
			if clear {
				break
			}
			pos += dir
			if pos < 0.08 || pos > 0.92 {
				dir = -dir
				pos += 2 * dir
			}
		}
		seen[k] = append(seen[k], pos)
		return pos
	}
	for _, e := range g.edges {
		r := &routes[e.idx]
		if e.structural && g.isFlow(e) {
			seen[portKey{e.from, r.src.Side}] = append(seen[portKey{e.from, r.src.Side}], r.src.Position)
			seen[portKey{e.to, r.tgt.Side}] = append(seen[portKey{e.to, r.tgt.Side}], r.tgt.Position)
		}
	}
	// Claim order: STRAIGHT edges first — a rigid line tilts when its port
	// moves, while a bended route absorbs the shift in its first leg — then
	// FARTHEST-FIRST, the mirror of the lane nesting above: the far tie
	// keeps its requested slot, the near one (inner lane, earlier climb-out)
	// bumps ALONG travel, so same-flank exits nest instead of crossing.
	// When a claim moves a port, the adjacent bend FOLLOWS it — a 45-degree
	// exit belongs to its port, not to where the port used to be.
	claimOrder := append([]*edge(nil), auxOrder...)
	sort.SliceStable(claimOrder, func(a, b int) bool {
		la := len(routes[claimOrder[a].idx].bends) >= 2
		lb := len(routes[claimOrder[b].idx].bends) >= 2
		if la != lb {
			return !la
		}
		if la {
			// lane ties: NEAREST first — the inner lane owns the corner
			return span(claimOrder[a]) < span(claimOrder[b])
		}
		return span(claimOrder[a]) > span(claimOrder[b])
	})
	for _, e := range claimOrder {
		r := &routes[e.idx]
		f, t := g.nodes[e.from], g.nodes[e.to]
		// the bump step is at least one lane separation IN PIXELS — a
		// 0.15 fraction of a 60px side is 9px, and neighbouring laddered
		// exits would themselves violate the covering rule, leaving the
		// lane-shift pass nothing it can fix
		stepFor := func(nd *node, side string) float64 {
			size := nd.w
			if side == "left" || side == "right" {
				size = nd.h
			}
			s := float64(LaneSep+2) / float64(size)
			if s < 0.15 {
				s = 0.15
			}
			return s
		}
		travel := func(side string, toward *node, from *node, step float64) float64 {
			if side == "top" || side == "bottom" {
				if toward.x+toward.w/2 < from.x+from.w/2 {
					return -step
				}
				return step
			}
			if toward.y+toward.h/2 < from.y+from.h/2 {
				return -step
			}
			return step
		}
		follow := func(n *node, side string, oldPos, newPos float64, bendAt int) {
			if newPos == oldPos || len(r.bends) == 0 {
				return
			}
			if side == "top" || side == "bottom" {
				r.bends[bendAt].X += int((newPos - oldPos) * float64(n.w))
			} else {
				r.bends[bendAt].Y += int((newPos - oldPos) * float64(n.h))
			}
		}
		reqS, dirS := r.src.Position, travel(r.src.Side, t, f, stepFor(f, r.src.Side))
		reqT, dirT := r.tgt.Position, travel(r.tgt.Side, f, t, stepFor(t, r.tgt.Side))
		if len(r.bends) >= 2 {
			// corner NESTING (v7P9): a lane tie asks for the slot at the
			// travel EXTREME, and later (farther) ties bump AGAINST travel —
			// processed nearest-first, the nearest tie hugs the corner on
			// the inner lane and the farthest swings widest, so the
			// 45-degree climbs never cut a neighbour's lane run
			// (three bypasses tangled beside one event).
			// The extreme is CLAMPED to box-clear slots first: a shifted
			// exit segment must not land in a neighbour's band (a concept
			// stack right beside the fan owner sits exactly there).
			clearReq := func(nd *node, side string, req, dir, orig float64, bendAt int) float64 {
				for guard := 0; guard < 6; guard++ {
					px, py := point(nd, layout.EdgePort{Side: side, Position: req})
					bx, by := r.bends[bendAt].X, r.bends[bendAt].Y
					if side == "top" || side == "bottom" {
						bx += int((req - orig) * float64(nd.w))
					} else {
						by += int((req - orig) * float64(nd.h))
					}
					if !g.hitsNode([][2]int{{px, py}, {bx, by}}, e) {
						return req
					}
					req -= dir
				}
				return orig
			}
			// a side with NO other edge need not hug the corner: its
			// sole lane takes the QUARTER slot; nested
			// lanes on shared sides keep the extreme so later ties bump
			// inward without tangling
			sideUsers := func(ni int, side string) int {
				n2 := 0
				for _, o := range g.edges {
					if routes[o.idx].stubbed {
						continue
					}
					or := routes[o.idx]
					if o.from == ni && or.src.Side == side {
						n2++
					}
					if o.to == ni && or.tgt.Side == side {
						n2++
					}
				}
				return n2
			}
			extS, extT := 0.1, 0.1
			if sideUsers(e.from, r.src.Side) <= 1 {
				extS = 0.25
			}
			if sideUsers(e.to, r.tgt.Side) <= 1 {
				extT = 0.25
			}
			if dirS > 0 {
				extS = 1 - extS
			}
			if dirT > 0 {
				extT = 1 - extT
			}
			// same-side arrivals keep their APPROACH order (v7P9:
			// entries at one border must not swap — the
			// higher source enters higher): each end's request steps off
			// the extreme by its rank among the side's lane ends, ranked
			// by where the OTHER endpoint lies along the side's axis.
			approachRank := func(ni int, side string, otherIdx int, ext float64) int {
				vert := side == "left" || side == "right"
				coord := func(oi int) int {
					on := g.nodes[oi]
					if vert {
						return on.y + on.h/2
					}
					return on.x + on.w/2
				}
				mine := coord(otherIdx)
				rank := 0
				for _, o := range auxOrder {
					if o == e || routes[o.idx].stubbed || len(routes[o.idx].bends) == 0 {
						continue
					}
					var oOther int
					switch {
					case o.from == ni && routes[o.idx].src.Side == side:
						oOther = o.to
					case o.to == ni && routes[o.idx].tgt.Side == side:
						oOther = o.from
					default:
						continue
					}
					c := coord(oOther)
					if (ext < 0.5 && c < mine) || (ext >= 0.5 && c > mine) {
						rank++
					}
				}
				return rank
			}
			step := func(ext float64, rank int) float64 {
				d := 0.15 * float64(rank)
				if ext < 0.5 {
					ext += d
				} else {
					ext -= d
				}
				if ext < 0.08 {
					ext = 0.08
				}
				if ext > 0.92 {
					ext = 0.92
				}
				return ext
			}
			extS = step(extS, approachRank(e.from, r.src.Side, e.to, extS))
			extT = step(extT, approachRank(e.to, r.tgt.Side, e.from, extT))
			reqS = clearReq(f, r.src.Side, extS, dirS, r.src.Position, 0)
			reqT = clearReq(t, r.tgt.Side, extT, dirT, r.tgt.Position, len(r.bends)-1)
			dirS, dirT = -dirS, -dirT
		}
		sp := claim(e.from, r.src.Side, reqS, dirS)
		follow(f, r.src.Side, r.src.Position, sp, 0)
		r.src.Position = sp
		tp := claim(e.to, r.tgt.Side, reqT, dirT)
		follow(t, r.tgt.Side, r.tgt.Position, tp, len(r.bends)-1)
		r.tgt.Position = tp
	}

	// pass 2 (after the port claims — coincident exits would make
	// identical lanes unshiftable): aux edges shift off occupied lanes
	for _, e := range auxOrder {
		if routes[e.idx].stubbed {
			continue
		}
		r := routes[e.idx]
		if conflicted(e, r) {
			shifted := false
			dirs := []int{LaneSep, -LaneSep, 2 * LaneSep, -2 * LaneSep, 3 * LaneSep, -3 * LaneSep}
			if len(r.bends) >= 2 {
				// nested lanes shift OUTWARD only (v7P9): the earlier
				// (nearer) tie keeps the inner lane, a later one steps AWAY
				// from the content — an inward step would put the longer
				// tie's lane between a shorter tie and its boxes.
				_, sy := point(g.nodes[e.from], r.src)
				_, ty := point(g.nodes[e.to], r.tgt)
				sx, _ := point(g.nodes[e.from], r.src)
				tx, _ := point(g.nodes[e.to], r.tgt)
				out := 0
				if r.bends[0].Y == r.bends[1].Y {
					if r.bends[0].Y >= sy && r.bends[0].Y >= ty {
						out = 1
					} else if r.bends[0].Y <= sy && r.bends[0].Y <= ty {
						out = -1
					}
				} else if r.bends[0].X == r.bends[1].X {
					if r.bends[0].X >= sx && r.bends[0].X >= tx {
						out = 1
					} else if r.bends[0].X <= sx && r.bends[0].X <= tx {
						out = -1
					}
				}
				if out != 0 {
					dirs = []int{out * LaneSep, 2 * out * LaneSep,
						3 * out * LaneSep, 4 * out * LaneSep, -out * LaneSep}
				}
			}
			for _, d := range dirs {
				cand := r
				if len(r.bends) > 0 {
					// lane segments move by their bends
					cand.bends = append([]layout.Position(nil), r.bends...)
					horizontal := len(r.bends) >= 2 && r.bends[0].Y == r.bends[1].Y
					for i := range cand.bends {
						if horizontal || len(r.bends) < 2 {
							cand.bends[i].Y += d
						} else {
							cand.bends[i].X += d
						}
					}
				} else {
					// straight ties move both ports together
					f, t := g.nodes[e.from], g.nodes[e.to]
					sizeAt := func(n *node, side string) int {
						if side == "left" || side == "right" {
							return n.h
						}
						return n.w
					}
					sp := cand.src.Position + float64(d)/float64(sizeAt(f, cand.src.Side))
					tp := cand.tgt.Position + float64(d)/float64(sizeAt(t, cand.tgt.Side))
					if sp < 0.08 || sp > 0.92 || tp < 0.08 || tp > 0.92 {
						continue
					}
					cand.src.Position = sp
					cand.tgt.Position = tp
				}
				// a shift must clear BOXES too, not only parallel
				// segments — sliding a lane into a neighbouring column
				// trades a covering for a spearing
				csx, csy := point(g.nodes[e.from], cand.src)
				ctx, cty := point(g.nodes[e.to], cand.tgt)
				candPts := [][2]int{{csx, csy}}
				for _, b := range cand.bends {
					candPts = append(candPts, [2]int{b.X, b.Y})
				}
				candPts = append(candPts, [2]int{ctx, cty})
				if !conflicted(e, cand) && !g.hitsNode(candPts, e) {
					routes[e.idx] = cand
					r = cand
					shifted = true
					break
				}
			}
			_ = shifted
		}
		taken = append(taken, segsOf(e, r)...)
	}

	// pass 3 — even SPREAD (v7P9: the middle arrival
	// sits in the centre of its neighbours): per (node, side), lane
	// ends re-space UNIFORMLY between the fixed anchors (straight and
	// flow ports) and the border, keeping their claimed approach order.
	{
		type endRef struct {
			e      *edge
			atFrom bool
			pos    float64
		}
		movable := map[portKey][]endRef{}
		fixedAt := map[portKey][]float64{}
		for _, e := range g.edges {
			r := routes[e.idx]
			if r.stubbed {
				continue
			}
			if len(r.bends) == 0 || (e.structural && g.isFlow(e)) {
				fixedAt[portKey{e.from, r.src.Side}] = append(fixedAt[portKey{e.from, r.src.Side}], r.src.Position)
				fixedAt[portKey{e.to, r.tgt.Side}] = append(fixedAt[portKey{e.to, r.tgt.Side}], r.tgt.Position)
				continue
			}
			movable[portKey{e.from, r.src.Side}] = append(movable[portKey{e.from, r.src.Side}], endRef{e, true, r.src.Position})
			movable[portKey{e.to, r.tgt.Side}] = append(movable[portKey{e.to, r.tgt.Side}], endRef{e, false, r.tgt.Position})
		}
		var keys []portKey
		for k := range movable {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(a, b int) bool {
			if keys[a].n != keys[b].n {
				return keys[a].n < keys[b].n
			}
			return keys[a].side < keys[b].side
		})
		for _, k := range keys {
			ends := movable[k]
			if len(ends) == 0 {
				continue
			}
			if len(ends) == 1 && len(fixedAt[k]) == 0 {
				continue // a SOLE lane keeps its quarter slot
			}
			sort.SliceStable(ends, func(a, b int) bool { return ends[a].pos < ends[b].pos })
			fx := append([]float64{0}, fixedAt[k]...)
			fx = append(fx, 1)
			sort.Float64s(fx)
			// group the movable ends by the fixed interval they sit in
			i := 0
			for seg := 0; seg+1 < len(fx) && i < len(ends); seg++ {
				lo, hi := fx[seg], fx[seg+1]
				j := i
				for j < len(ends) && ends[j].pos < hi {
					j++
				}
				m := j - i
				for x := 0; x < m; x++ {
					want := lo + (hi-lo)*float64(x+1)/float64(m+1)
					want = math.Round(want*100) / 100 // pin-expressible
					er := ends[i+x]
					r := routes[er.e.idx]
					nd := g.nodes[er.e.from]
					pp, bendAt := &r.src, 0
					if !er.atFrom {
						nd = g.nodes[er.e.to]
						pp, bendAt = &r.tgt, len(r.bends)-1
					}
					// apply the slot on a COPY and commit only when it does
					// not spear a box the claimed route clears — the even
					// spread must never trade spacing for a through-node
					// (the lane into cwr slid down into
					// 'workload resource')
					cand := r
					cand.bends = append([]layout.Position(nil), r.bends...)
					cpp := &cand.src
					if !er.atFrom {
						cpp = &cand.tgt
					}
					if len(cand.bends) > 0 && bendAt >= 0 {
						if k.side == "top" || k.side == "bottom" {
							cand.bends[bendAt].X += int((want - pp.Position) * float64(nd.w))
						} else {
							cand.bends[bendAt].Y += int((want - pp.Position) * float64(nd.h))
						}
					}
					cpp.Position = want
					pts := func(rr routed) [][2]int {
						sx, sy := point(g.nodes[er.e.from], rr.src)
						tx, ty := point(g.nodes[er.e.to], rr.tgt)
						out := [][2]int{{sx, sy}}
						for _, b := range rr.bends {
							out = append(out, [2]int{b.X, b.Y})
						}
						return append(out, [2]int{tx, ty})
					}
					// the slot must not introduce a hit OR a graze the
					// claimed route did not have (v7P8: a visible gap
					// between any line and any box it does not connect
					// to — the spread slid e2→e4 onto cX's corner)
					strict := func(rr routed) bool {
						ps := pts(rr)
						m2 := GridStep / 2
						for ni := range g.nodes {
							n := g.nodes[ni]
							if !n.placed || n.idx == er.e.from || n.idx == er.e.to {
								continue
							}
							for k2 := 0; k2+1 < len(ps); k2++ {
								if segIntersectsBox(ps[k2], ps[k2+1],
									n.x-m2, n.y-m2, n.x+n.w+m2, n.y+n.h+m2) {
									return false
								}
							}
						}
						return true
					}
					if !strict(cand) && strict(r) {
						continue // keep the claimed slot rather than graze
					}
					routes[er.e.idx] = cand
				}
				i = j
			}
		}
	}
}

func (g *graph) otherEnd(end edgeEnd) int {
	if end.atFrom {
		return end.edge.to
	}
	return end.edge.from
}

// isFlow: leads-to between events (incl. boundary edges) — the skeleton's
// vertical corridors (v7P3/P6).
// subOwner: the composite a sub-event returns to (the target of its
// structural ePe), or -1.
func (g *graph) subOwner(n int) int {
	if g.nodes[n].kind != KindEvent {
		return -1
	}
	for _, e := range g.out[n] {
		if e.structural && e.rel == RelPartOf && g.nodes[e.to].kind == KindEvent {
			return e.to
		}
	}
	return -1
}

func (g *graph) isFlow(e *edge) bool {
	return e.rel == RelLeadsTo &&
		g.nodes[e.from].kind == KindEvent && g.nodes[e.to].kind == KindEvent
}

func (g *graph) layoutNode(n *node) layout.Node {
	return layout.Node{X: n.x, Y: n.y, Width: n.w, Height: n.h}
}

// hop45 is the offset of a 45° border-to-lane hop: equal to the distance to the
// lane, capped at Clearance so the exit stays SHORT (v7P5/P9 "a SHORT 45° hop,
// border to lane", never a drift across the diagram).
func hop45(v int) int {
	v = absInt(v)
	if v > Clearance {
		return Clearance
	}
	return v
}

// fitHops shrinks a pair of 45° hop offsets proportionally so together they fit
// inside the span between the two ports — two hops must never overshoot each
// other and invert the lane leg.
func fitHops(h1, h2, span int) (int, int) {
	span = absInt(span)
	if h1+h2 <= span || h1+h2 == 0 {
		return h1, h2
	}
	first := span * h1 / (h1 + h2)
	return first, span - first
}

// tieCandidates builds the ordered candidate routes for a non-structural
// edge: straight, two doglegs, below bypass, above bypass (v7P9's flank
// bypass at 45°).
func (g *graph) tieCandidates(e *edge,
	port func(*edge, bool) layout.EdgePort,
	point func(*node, layout.EdgePort) (int, int)) []routed {

	f, t := g.nodes[e.from], g.nodes[e.to]
	src, tgt := port(e, true), port(e, false)
	sx, sy := point(f, src)
	tx, ty := point(t, tgt)

	var cands []routed
	cands = append(cands, routed{src: src, tgt: tgt}) // straight
	if sx != tx && sy != ty {
		// Doglegs carry their own ports, FACING the first segment — an
		// edge never runs along a border it touches (no
		// corner landings, no border-hugging). The leg leaves the side's
		// CENTRE, turns once, and enters the target's facing side.
		hSrc := layout.EdgePort{Side: "right", Position: 0.5}
		if tx < sx {
			hSrc.Side = "left"
		}
		vTgt := layout.EdgePort{Side: "top", Position: 0.5}
		if ty < sy {
			vTgt.Side = "bottom"
		}
		vSrc := layout.EdgePort{Side: "bottom", Position: 0.5}
		if ty < sy {
			vSrc.Side = "top"
		}
		hTgt := layout.EdgePort{Side: "left", Position: 0.5}
		if tx < sx {
			hTgt.Side = "right"
		}
		_, hsy := point(f, hSrc)
		vtx, _ := point(t, vTgt)
		vsx, _ := point(f, vSrc)
		_, hty := point(t, hTgt)
		// HOP-DIAGONALS ("go horizontal only a bit to
		// be far away of e7 and then diagonal — minimal gaps to avoid
		// without increasing the number of bends"): one MINIMAL
		// axis-aligned hop out of the source — just past whatever blocks
		// the straight — then a single beeline to the target port. Same
		// bend count as a dogleg, but the long segment heads AT the
		// target. Smallest hops first (the route loop takes the first
		// clean candidate), tried BEFORE the axis-aligned doglegs — the
		// diagonal is preferred. The beeline must still meet the target
		// border head-on enough (the 150° cap) and keep travelling in
		// the hop's direction.
		hDir := 1
		if hSrc.Side == "left" {
			hDir = -1
		}
		vDir := 1
		if vSrc.Side == "top" {
			vDir = -1
		}
		// ... the beeline's TARGET port may also LEAN toward the source
		// (within the spread range, centre first — minimal deviation): a
		// centre-aimed beeline can graze a cap the leaned one clears by a
		// column (a1→e1 wants the diagonal; top-centre
		// grazed e1's S cap, top@0.85 clears it).
		hopDiag := func(lean float64) []routed {
			// the SOURCE port leans toward the beeline's travel (v7P9:
			// spread slots sit on the edge's travel side) — a diagonal
			// that exits above a row-mate's tie and then dives across it
			// swaps the departures and crosses (the onion's a1→c1 ×
			// a1→e1)
			hs := hSrc
			vs := vSrc
			if lean > 0 { // centre sources for the plain hops (ratified green)
				if ty > sy {
					hs.Position = 0.7
				} else if ty < sy {
					hs.Position = 0.3
				}
				if tx > sx {
					vs.Position = 0.7
				} else if tx < sx {
					vs.Position = 0.3
				}
			}
			hsxL, hsyL := point(f, hs)
			vsxL, vsyL := point(f, vs)
			var out []routed
			for _, hop := range []int{GridStep, 2 * GridStep, 3 * GridStep} {
				bx := hsxL + hDir*hop
				by := vsyL + vDir*hop
				// horizontal hop, then diagonal into the vertical-facing port
				vt := vTgt
				if bx > vtx {
					vt.Position = 0.5 + lean
				} else {
					vt.Position = 0.5 - lean
				}
				vtxL, vtyL := point(t, vt)
				if (vtxL-bx)*hDir >= 0 &&
					absInt(vtyL-hsyL)*373 >= absInt(vtxL-bx)*100 {
					out = append(out, routed{src: hs, tgt: vt,
						bends: []layout.Position{{X: bx, Y: hsyL}}})
				}
				// vertical hop, then diagonal into the horizontal-facing port
				ht := hTgt
				if by > hty {
					ht.Position = 0.5 + lean
				} else {
					ht.Position = 0.5 - lean
				}
				htxL, htyL := point(t, ht)
				if (htyL-by)*vDir >= 0 &&
					absInt(htxL-vsxL)*373 >= absInt(htyL-by)*100 {
					out = append(out, routed{src: vs, tgt: ht,
						bends: []layout.Position{{X: vsxL, Y: by}}})
				}
			}
			return out
		}
		// centre-aimed hops rank before the doglegs (the beeline is the
		// most direct legal shape) ...
		cands = append(cands, hopDiag(0)...)
		// SLID BEELINES (evidence's tie should cross ONE
		// expresses edge as a plain diagonal, "better than this two bends
		// and edge around"): in a true diagonal quadrant, a straight
		// between SLID ports on the facing sides can clear what the
		// centre straight and hops hit — zero bends, the most direct
		// legal shape, tried before any dogleg. Centre-out order (minimal
		// deviation first); both ports keep their side's 150°-cap angle
		// legality and true outward/inward travel (v7P9: no corner-ish
		// landings, no border-hugging exits).
		if (f.x+f.w < t.x || t.x+t.w < f.x) && (f.y+f.h < t.y || t.y+t.h < f.y) {
			// the SPREAD-assigned slot leads each port's pose list: the
			// approach-order discipline (arrivals spread evenly, sources
			// keep order) owns the positions, and the beeline slides off
			// the assigned slot only when the assigned one cannot clear
			// (the ratified kubectl fan keeps its thirds)
			posesFor := func(assigned layout.EdgePort, side string) []float64 {
				var out []float64
				if assigned.Side == side {
					out = append(out, assigned.Position)
				}
				for _, q := range []float64{0.5, 0.3, 0.7, 0.15, 0.85} {
					dup := false
					for _, have := range out {
						if math.Abs(q-have) < 1e-9 {
							dup = true
						}
					}
					if !dup {
						out = append(out, q)
					}
				}
				return out
			}
			home := func(assigned layout.EdgePort, side string) float64 {
				if assigned.Side == side {
					return assigned.Position
				}
				return 0.5
			}
			type slidPair struct{ s, t layout.EdgePort }
			var pairs []slidPair
			for _, ss := range []layout.EdgePort{hSrc, vSrc} {
				for _, ts := range []layout.EdgePort{vTgt, hTgt} {
					for _, sp := range posesFor(src, ss.Side) {
						for _, tp := range posesFor(tgt, ts.Side) {
							s2, t2 := ss, ts
							s2.Position, t2.Position = sp, tp
							pairs = append(pairs, slidPair{s2, t2})
						}
					}
				}
			}
			sort.SliceStable(pairs, func(a, b int) bool {
				da := math.Abs(pairs[a].s.Position-home(src, pairs[a].s.Side)) +
					math.Abs(pairs[a].t.Position-home(tgt, pairs[a].t.Side))
				db := math.Abs(pairs[b].s.Position-home(src, pairs[b].s.Side)) +
					math.Abs(pairs[b].t.Position-home(tgt, pairs[b].t.Side))
				return da < db
			})
			angleOK := func(side string, dx, dy int) bool {
				if side == "left" || side == "right" {
					return absInt(dy)*100 <= absInt(dx)*373
				}
				return absInt(dx)*100 <= absInt(dy)*373
			}
			outOK := func(side string, dx, dy int) bool {
				switch side {
				case "right":
					return dx > 0
				case "left":
					return dx < 0
				case "bottom":
					return dy > 0
				}
				return dy < 0
			}
			inOK := func(side string, dx, dy int) bool {
				switch side {
				case "right":
					return dx < 0
				case "left":
					return dx > 0
				case "bottom":
					return dy < 0
				}
				return dy > 0
			}
			for _, pr := range pairs {
				if pr.s.Side == src.Side && pr.s.Position == src.Position &&
					pr.t.Side == tgt.Side && pr.t.Position == tgt.Position {
					continue // the straight already covers this shape
				}
				x0, y0 := point(f, pr.s)
				x1, y1 := point(t, pr.t)
				dx2, dy2 := x1-x0, y1-y0
				if !angleOK(pr.s.Side, dx2, dy2) || !angleOK(pr.t.Side, dx2, dy2) ||
					!outOK(pr.s.Side, dx2, dy2) || !inOK(pr.t.Side, dx2, dy2) {
					continue
				}
				cands = append(cands, routed{src: pr.s, tgt: pr.t})
			}
		}
		// a dogleg LEG must be at least an arrowhead long (one grid
		// step): a micro-drop reads as a horizontal line lying on the
		// border beside the flow arrow (Plum's tie
		// landed via a 12px stub over refusal-mspp's flow landing)
		vsy2 := 0
		if _, y := point(f, vSrc); true {
			vsy2 = y
		}
		hty2 := 0
		if _, y := point(t, hTgt); true {
			hty2 = y
		}
		htx2, _ := point(t, hTgt)
		vty2 := 0
		if _, y := point(t, vTgt); true {
			vty2 = y
		}
		if absInt(hty2-vsy2) >= GridStep && absInt(htx2-vsx) >= GridStep {
			// vertical-first: down/up the source's column, then across
			cands = append(cands, routed{src: vSrc, tgt: hTgt, bends: []layout.Position{{X: vsx, Y: hty2}}})
		}
		hsx2, _ := point(f, hSrc)
		if absInt(vtx-hsx2) >= GridStep && absInt(vty2-hsy) >= GridStep {
			// horizontal-first: out of the source's side, then up/down
			cands = append(cands, routed{src: hSrc, tgt: vTgt, bends: []layout.Position{{X: vtx, Y: hsy}}})
		}
		// ... LEANED hops rank between the doglegs and the flank lanes:
		// a leaned beeline cuts midfield, so it fires only when the tidy
		// shapes fail (a1→e1's diagonal clears the S cap
		// at top@0.85 — but it must not displace clean doglegs elsewhere)
		for _, c := range append(hopDiag(0.2), hopDiag(0.35)...) {
			c.leaned = true
			cands = append(cands, c)
		}
	}

	// SLID straights (v7P9: a tie between stacked,
	// x-overlapping boxes reads as ONE vertical line): the straight may
	// slide WITHIN the overlap — both ports move together — to clear
	// whatever sits on the centre line (e.g. the lower component's own
	// S boundary). Centre-out order so the least-slid clear line wins
	// ties. Mirrored for y-overlapping side-by-side boxes.
	slid := func(o0, o1 int, vertical bool) {
		if o1-o0 < GridStep {
			return
		}
		mid := (o0 + o1) / 2 / GridStep * GridStep
		for d := 0; d <= o1-o0; d += GridStep {
			for _, at := range []int{mid + d, mid - d} {
				if at < o0 || at > o1 {
					continue
				}
				var sp, tp float64
				var sSide, tSide string
				if vertical {
					sp = float64(at-f.x) / float64(f.w)
					tp = float64(at-t.x) / float64(t.w)
					sSide, tSide = "bottom", "top"
					if f.y >= t.y+t.h {
						sSide, tSide = "top", "bottom"
					}
				} else {
					sp = float64(at-f.y) / float64(f.h)
					tp = float64(at-t.y) / float64(t.h)
					sSide, tSide = "right", "left"
					if f.x >= t.x+t.w {
						sSide, tSide = "left", "right"
					}
				}
				if sp < 0.15 || sp > 0.85 || tp < 0.15 || tp > 0.85 {
					continue // keep clear of the corners
				}
				cands = append(cands, routed{
					src: layout.EdgePort{Side: sSide, Position: sp},
					tgt: layout.EdgePort{Side: tSide, Position: tp},
				})
				if d == 0 {
					break // mid+0 == mid-0
				}
			}
		}
	}
	if t.y >= f.y+f.h || f.y >= t.y+t.h {
		slid(maxInt(f.x, t.x), minInt(f.x+f.w, t.x+t.w), true)
	}
	if t.x >= f.x+f.w || f.x >= t.x+t.w {
		slid(maxInt(f.y, t.y), minInt(f.y+f.h, t.y+t.h), false)
	}

	// INTER-COLUMN descent (a1→e1 draws — out of the
	// source's facing side, down a lane centred in the horizontal gap,
	// into the target's facing side): for diagonally separated boxes the
	// corridor between the columns is the natural route.
	if (t.x+t.w <= f.x || f.x+f.w <= t.x) && (t.y >= f.y+f.h || f.y >= t.y+t.h) {
		gapLo, gapHi := f.x+f.w, t.x
		sSide, tSide := "right", "left"
		if t.x+t.w <= f.x {
			gapLo, gapHi = t.x+t.w, f.x
			sSide, tSide = "left", "right"
		}
		if gapHi-gapLo >= GridStep {
			// several lane positions, centre-out (the
			// descent leaves the FACING border — a clear lane anywhere in
			// the gap beats detouring around the far flank)
			sp := layout.EdgePort{Side: sSide, Position: 0.5}
			tp := layout.EdgePort{Side: tSide, Position: 0.5}
			_, sy := point(f, sp)
			_, ty := point(t, tp)
			mid := (gapLo + gapHi) / 2 / GridStep * GridStep
			for d := 0; d <= (gapHi-gapLo)/2; d += 2 * GridStep {
				for _, laneX := range []int{mid + d, mid - d} {
					if laneX-gapLo < GridStep/2 || gapHi-laneX < GridStep/2 {
						continue
					}
					// 45° border-to-lane hops, like the below/above
					// bypasses: the vertical offset equals the horizontal
					// distance to the lane (capped, so the exit stays SHORT
					// — v7P5/P9 "a SHORT 45° hop, border to lane"). Shrunk
					// proportionally when the two hops would not fit in the
					// vertical span, so they never overshoot each other.
					h1, h2 := fitHops(hop45(laneX-sx), hop45(laneX-tx), ty-sy)
					vDir := 1
					if ty < sy {
						vDir = -1
					}
					cands = append(cands, routed{src: sp, tgt: tp, bends: []layout.Position{
						{X: laneX, Y: sy + vDir*h1}, {X: laneX, Y: ty - vDir*h2}}})
					if h1 != 0 || h2 != 0 { // square fallback
						cands = append(cands, routed{src: sp, tgt: tp, bends: []layout.Position{
							{X: laneX, Y: sy}, {X: laneX, Y: ty}}})
					}
					if d == 0 {
						break
					}
				}
			}
		}
	}

	// LOCAL lanes around the endpoints' own rows and columns — the tie
	// analog of the blocked-structural side lanes: a same-row tie whose
	// gap is occupied goes UNDER or OVER its row (a satellite beyond its
	// partner's band neighbour must not spear the neighbour), a
	// same-column one around the side. Shorter than the component-wide
	// bypasses below, so the detour tax prefers them.
	{
		lo, hi := minInt(f.x, t.x), maxInt(f.x+f.w, t.x+t.w)
		vlo, vhi := minInt(f.y, t.y), maxInt(f.y+f.h, t.y+t.h)
		// room-aware offset: the lane CENTRES in the free gap on its
		// flank (v7P8's visible gap on both sides), capped at one
		// clearance
		localOff := func(flank int) int { // 0 below, 1 above, 2 right, 3 left
			free := 1 << 30
			for _, n := range g.nodes {
				if !n.placed || n == f || n == t {
					continue
				}
				d := free
				switch flank {
				case 0:
					if n.x < hi && lo < n.x+n.w && n.y >= vhi {
						d = n.y - vhi
					}
				case 1:
					if n.x < hi && lo < n.x+n.w && n.y+n.h <= vlo {
						d = vlo - (n.y + n.h)
					}
				case 2:
					if n.y < vhi && vlo < n.y+n.h && n.x >= hi {
						d = n.x - hi
					}
				default:
					if n.y < vhi && vlo < n.y+n.h && n.x+n.w <= lo {
						d = lo - (n.x + n.w)
					}
				}
				if d < free {
					free = d
				}
			}
			off := free / 2
			if off > Clearance {
				off = Clearance
			}
			if off < GridStep/2 {
				off = GridStep / 2
			}
			return off
		}
		for _, fl := range []int{0, 1} {
			lane := vhi + localOff(0)
			side := "bottom"
			if fl == 1 {
				lane = vlo - localOff(1)
				side = "top"
			}
			p := layout.EdgePort{Side: side, Position: 0.5}
			bsx, bsy := point(f, p)
			btx, bty := point(t, p)
			// 45° border-to-lane hops (v7P5/P9), fitted to the horizontal span
			hs, ht := fitHops(hop45(lane-bsy), hop45(lane-bty), btx-bsx)
			hdir := 1
			if btx < bsx {
				hdir = -1
			}
			cands = append(cands, routed{src: p, tgt: p, bends: []layout.Position{
				{X: bsx + hdir*hs, Y: lane}, {X: btx - hdir*ht, Y: lane},
			}})
			if hs != 0 || ht != 0 { // square fallback when the hop would not be clean
				cands = append(cands, routed{src: p, tgt: p, bends: []layout.Position{
					{X: bsx, Y: lane}, {X: btx, Y: lane},
				}})
			}
		}
		for _, fl := range []int{2, 3} {
			lane := hi + localOff(2)
			side := "right"
			if fl == 3 {
				lane = lo - localOff(3)
				side = "left"
			}
			p := layout.EdgePort{Side: side, Position: 0.5}
			bsx, bsy := point(f, p)
			btx, bty := point(t, p)
			// 45° border-to-lane hops (v7P5/P9), fitted to the vertical span
			vs, vt := fitHops(hop45(lane-bsx), hop45(lane-btx), bty-bsy)
			vdirL := 1
			if bty < bsy {
				vdirL = -1
			}
			cands = append(cands, routed{src: p, tgt: p, bends: []layout.Position{
				{X: lane, Y: bsy + vdirL*vs}, {X: lane, Y: bty - vdirL*vt},
			}})
			if vs != 0 || vt != 0 { // square fallback when the hop would not be clean
				cands = append(cands, routed{src: p, tgt: p, bends: []layout.Position{
					{X: lane, Y: bsy}, {X: lane, Y: bty},
				}})
			}
		}
	}

	// flank bypasses run along a lane outside the component (v7P5's
	// outermost layer is inside it; other components begin beyond). A
	// cross-component tie (v7P1/P2) takes its lane outside BOTH components.
	if f.comp >= 0 && t.comp >= 0 {
		// lanes hug the CONTENT: S and E stay the outermost elements
		// (v7P3), so a bypass runs inside them, not around them
		bot, top := 0, 1<<30
		for _, n := range g.nodes {
			if (n.comp != f.comp && n.comp != t.comp) || !n.placed || n.boundary {
				continue
			}
			if n.y+n.h > bot {
				bot = n.y + n.h
			}
			if n.y < top {
				top = n.y
			}
		}
		dir := 1
		if tx < sx {
			dir = -1
		}
		clamp := func(v int) int { // the 45-degree exit is SHORT (border to
			if v < 0 { // lane), never a drift across the diagram
				v = -v
			}
			if v > Clearance {
				return Clearance
			}
			return v
		}
		// Lanes keep a VISIBLE gap (v7P8): the offset from the content is
		// a quarter of the free room to the nearest obstacle on that flank
		// (other boxes, S/E included), clamped to [half a grid step, one
		// clearance] — a lane hugs only where the flank is genuinely
		// tight, and nested lanes still step outward inside that room
		// (a bypass 10px over the row reads as touching,
		// and the arrowhead has no air).
		left, right := 1<<30, 0
		for _, n := range g.nodes {
			if (n.comp != f.comp && n.comp != t.comp) || !n.placed {
				continue
			}
			if n.x < left {
				left = n.x
			}
			if n.x+n.w > right {
				right = n.x + n.w
			}
		}
		laneOff := func(flank int) int { // 0 below, 1 above, 2 right, 3 left
			lo, hi := minInt(f.x, t.x), maxInt(f.x+f.w, t.x+t.w)
			vlo, vhi := minInt(f.y, t.y), maxInt(f.y+f.h, t.y+t.h)
			free := 1 << 30
			for _, n := range g.nodes {
				if !n.placed || n == f || n == t {
					continue
				}
				d := free
				switch flank {
				case 0:
					if n.x < hi && lo < n.x+n.w && n.y >= bot {
						d = n.y - bot
					}
				case 1:
					if n.x < hi && lo < n.x+n.w && n.y+n.h <= top {
						d = top - (n.y + n.h)
					}
				case 2:
					if n.y < vhi && vlo < n.y+n.h && n.x >= right {
						d = n.x - right
					}
				default:
					if n.y < vhi && vlo < n.y+n.h && n.x+n.w <= left {
						d = left - (n.x + n.w)
					}
				}
				if d < free {
					free = d
				}
			}
			// CENTRE in the free room (the over-the-
			// component lane hugged the arrowhead at a quarter; midway
			// between the row and the S cap reads clean), capped at one
			// clearance
			off := free / 2
			if off > Clearance {
				off = Clearance
			}
			if off < GridStep/2 {
				off = GridStep / 2
			}
			return off
		}
		srcPos := 0.5 + 0.25*float64(dir) // exit on the travel side of the centre
		tgtPos := 0.5 - 0.25*float64(dir) // enter on the approach side
		below := func() routed {
			laneY := bot + laneOff(0)
			bSrc := layout.EdgePort{Side: "bottom", Position: srcPos}
			bTgt := layout.EdgePort{Side: "bottom", Position: tgtPos}
			bsx, bsy := point(f, bSrc)
			btx, bty := point(t, bTgt)
			return routed{src: bSrc, tgt: bTgt, bends: []layout.Position{
				{X: bsx + dir*clamp(laneY-bsy), Y: laneY},
				{X: btx - dir*clamp(laneY-bty), Y: laneY},
			}}
		}
		above := func() routed {
			laneY := top - laneOff(1)
			aSrc := layout.EdgePort{Side: "top", Position: srcPos}
			aTgt := layout.EdgePort{Side: "top", Position: tgtPos}
			asx, asy := point(f, aSrc)
			atx, aty := point(t, aTgt)
			return routed{src: aSrc, tgt: aTgt, bends: []layout.Position{
				{X: asx + dir*clamp(asy-laneY), Y: laneY},
				{X: atx - dir*clamp(aty-laneY), Y: laneY},
			}}
		}
		// vertical side lanes: out of the facing side, along a lane beside
		// BOTH endpoints, in at the target's facing side — the natural
		// route past a boundary column for a long climb.
		vdir := 1
		if ty < sy {
			vdir = -1
		}
		sideLane := func(side string, laneX int) routed {
			sSrc := layout.EdgePort{Side: side, Position: 0.5 + 0.25*float64(vdir)}
			sTgt := layout.EdgePort{Side: side, Position: 0.5 - 0.25*float64(vdir)}
			ssx, ssy := point(f, sSrc)
			stx, sty := point(t, sTgt)
			return routed{src: sSrc, tgt: sTgt, bends: []layout.Position{
				{X: laneX, Y: ssy + vdir*clamp(laneX-ssx)},
				{X: laneX, Y: sty - vdir*clamp(laneX-stx)},
			}}
		}
		// lanes matching the edge's travel axis come first: a mostly-
		// horizontal tie detours over/under, a mostly-vertical one takes a
		// side lane — never a far side-trip for a same-row pair
		adx, ady := tx-sx, ty-sy
		if adx < 0 {
			adx = -adx
		}
		if ady < 0 {
			ady = -ady
		}
		if adx >= ady {
			cands = append(cands, below(), above(),
				sideLane("right", right+laneOff(2)),
				sideLane("left", left-laneOff(3)))
		} else {
			cands = append(cands,
				sideLane("right", right+laneOff(2)),
				sideLane("left", left-laneOff(3)),
				below(), above())
		}
	}
	return cands
}

// grazeCount counts boxes a polyline passes TOO CLOSE to — clear of the
// box but inside the visible-gap margin of half a grid step (v7P8): an
// arrowhead or a parallel run there reads as touching. The edge's own
// endpoints are excluded; boundary boxes count.
func (g *graph) grazeCount(pts [][2]int, e *edge) float64 {
	n := 0.0
	m := GridStep / 2
	// A box CONNECTED to either endpoint is exempt: a wide fan's outer
	// edges naturally skim their siblings' corners on the shared row —
	// detouring them into lanes reads far worse than the skim
	// ("bending not needed here"). Growth (v7P8) is the real
	// cure for a fan that tight, not a detour.
	neighbour := map[int]bool{}
	for _, o := range g.out[e.from] {
		neighbour[o.from], neighbour[o.to] = true, true
	}
	for _, o := range g.in[e.from] {
		neighbour[o.from], neighbour[o.to] = true, true
	}
	for _, o := range g.out[e.to] {
		neighbour[o.from], neighbour[o.to] = true, true
	}
	for _, o := range g.in[e.to] {
		neighbour[o.from], neighbour[o.to] = true, true
	}
	for _, nd := range g.nodes {
		if !nd.placed || nd.idx == e.from || nd.idx == e.to {
			continue
		}
		if nd.shell && shellExempts(nd, e) {
			continue
		}
		// a ROW-MATE's border is never exempt: a branch hugging its
		// row-mate for the run reads as touching — the
		// around-the-row lane wins instead. BOUNDARY boxes are never
		// exempt either, and hugging S or E is prohibitive on its own —
		// the timeline caps stay untouched ("there
		// should be a gap").
		if !nd.boundary && neighbour[nd.idx] &&
			!g.rowMates[e.from][nd.idx] && !g.rowMates[e.to][nd.idx] {
			// the sibling exemption never covers a FLUSH contact: a line
			// through a box's corner reads as touching no matter the
			// kinship (kubeadm→join across net's corner)
			m2 := GridStep / 4
			flush, hard2 := false, false
			for i := 0; i+1 < len(pts) && !flush; i++ {
				if segIntersectsBox(pts[i], pts[i+1], nd.x-m2, nd.y-m2, nd.x+nd.w+m2, nd.y+nd.h+m2) {
					flush = true
				}
			}
			for i := 0; i+1 < len(pts) && !hard2; i++ {
				if segIntersectsBox(pts[i], pts[i+1], nd.x+2, nd.y+2, nd.x+nd.w-2, nd.y+nd.h-2) {
					hard2 = true
				}
			}
			if flush && !hard2 {
				n += 3
			}
			continue
		}
		near, hard := false, false
		for i := 0; i+1 < len(pts) && !near; i++ {
			if segIntersectsBox(pts[i], pts[i+1], nd.x-m, nd.y-m, nd.x+nd.w+m, nd.y+nd.h+m) {
				near = true
			}
		}
		if !near {
			continue
		}
		for i := 0; i+1 < len(pts) && !hard; i++ {
			if segIntersectsBox(pts[i], pts[i+1], nd.x+2, nd.y+2, nd.x+nd.w-2, nd.y+nd.h-2) {
				hard = true
			}
		}
		if !hard {
			// FLUSH runs weigh double: a line at zero
			// gap reads as touching the border — a candidate that keeps
			// the visible gap, even one with a distant crossing, wins
			flush := false
			m2 := GridStep / 4
			for i := 0; i+1 < len(pts) && !flush; i++ {
				if segIntersectsBox(pts[i], pts[i+1], nd.x-m2, nd.y-m2, nd.x+nd.w+m2, nd.y+nd.h+m2) {
					flush = true
				}
			}
			switch {
			case nd.boundary:
				n += 3 // × the callers' 0.5 = 1.5: over budget alone
			case flush:
				n += 3 // × 0.5 = 1.5: prohibitive alone, like an S/E hug
			default:
				n++
			}
		}
	}
	return n
}

// hitsNode reports whether the polyline passes through any placed node box
// (shrunk 2px — touching a border is fine), endpoints excluded.
func (g *graph) hitsNode(pts [][2]int, e *edge) bool {
	return g.hitCount(pts, e) > 0
}

// hitCount is how MANY boxes the polyline spears — the currency when no
// candidate is clean (a structural edge never hides, v7P9): one box
// speared beats twenty-five. A boolean hit priced every candidate alike,
// and the cheapest-crossing pick then ran a member's part-of straight down
// its 45-member column (NDA: 6. INTELLECTUAL PROPERTY → part 1 through
// every clause below it) because the flank straight brushed one box.
func (g *graph) hitCount(pts [][2]int, e *edge) int {
	hits := 0
	for _, n := range g.nodes {
		if !n.placed || n.idx == e.from || n.idx == e.to {
			continue
		}
		if n.shell && shellExempts(n, e) {
			continue // a member's edge crosses its own shell's border
		}
		x0, y0 := n.x+2, n.y+2
		x1, y1 := n.x+n.w-2, n.y+n.h-2
		for i := 0; i+1 < len(pts); i++ {
			if segIntersectsBox(pts[i], pts[i+1], x0, y0, x1, y1) {
				hits++
				break
			}
		}
	}
	return hits
}

// polyline is a routed edge's concrete geometry, used for crossing and
// node-hit checks.
type polyline struct {
	pts [][2]int
	e   *edge
}

// crossingCost sums the kind-aware crossing budget of v7P9.
func (g *graph) crossingCost(pl polyline, placed []polyline) float64 {
	cost := 0.0
	for _, other := range placed {
		if other.e == pl.e {
			continue
		}
		// Shared endpoints (fork/join meeting at a node) don't count NEAR
		// that node — lines converging on one box necessarily brush there.
		// Farther out the same pair is a REAL tangle (a
		// concept corridor cutting its owner's other structural edge 100px
		// away read as clean). The grace holds only when both edges point
		// the SAME WAY at the shared node (both arriving or both leaving —
		// a fan). One edge LEAVING the node across another's ARRIVING
		// arrowhead is an ARROW CUT (v7P9: edges never cross arrows) and
		// is charged like a corridor cut.
		var sharedBoxes []*node
		arrowCutAt := map[*node]bool{}
		for _, ni := range []int{pl.e.from, pl.e.to} {
			if other.e.from == ni || other.e.to == ni {
				nd := g.nodes[ni]
				sharedBoxes = append(sharedBoxes, nd)
				plArrives := pl.e.to == ni
				otherArrives := other.e.to == ni
				arrowCutAt[nd] = plArrives != otherArrives
			}
		}
		nearShared := func(x, y float64) (bool, bool) {
			for _, n := range sharedBoxes {
				dx := math.Max(0, math.Max(float64(n.x)-x, x-float64(n.x+n.w)))
				dy := math.Max(0, math.Max(float64(n.y)-y, y-float64(n.y+n.h)))
				if math.Hypot(dx, dy) < Clearance {
					return true, arrowCutAt[n]
				}
			}
			return false, false
		}
		crossed, arrowCut, graced := false, false, false
		for i := 0; i+1 < len(pl.pts) && !crossed; i++ {
			for j := 0; j+1 < len(other.pts) && !crossed; j++ {
				if segsCross(pl.pts[i], pl.pts[i+1], other.pts[j], other.pts[j+1]) {
					if len(sharedBoxes) > 0 {
						x, y := crossPoint(pl.pts[i], pl.pts[i+1], other.pts[j], other.pts[j+1])
						if near, cut := nearShared(x, y); near {
							if cut {
								crossed, arrowCut = true, true
							}
							graced = true
							continue
						}
					}
					crossed = true
				}
			}
		}
		if arrowCut {
			cost += 2.0
			continue
		}
		if graced && !crossed {
			// a same-direction crossing right at the shared node stays
			// UNDER every budget (fan siblings may skim), but it is not
			// free: an equal candidate that avoids it wins the tie
			// (two arrivals need not cross).
			cost += 0.25
			continue
		}
		if crossed {
			tie := pl.e
			if g.isFlow(tie) {
				tie = other.e
			}
			switch {
			case (g.isFlow(other.e) || g.isFlow(pl.e)) && tie.rel != RelNearTo:
				// v7P6: the flow corridor never yields — and a HIERARCHY
				// tie (part-of/expresses) never CUTS it: slicing the
				// timeline to reach a cheaper lane reads as breaking the
				// story. Over budget on its own. A
				// NEAR-TO association may still cross under the kind
				// budget — the ratified flank-tie and diagonal near-to
				// read fine across one flow edge.
				// EXCEPTION: a sub-grid's own RETURN
				// edge (structural ePe back to its composite) may cross
				// the grid's INTERNAL fork links at 1.5 — under the
				// prohibitive corridor cut (the sub-grid is dense and
				// the return sometimes has nowhere else) but above every
				// clean alternative, so a crossing-free lane still wins
				// when one exists. The protected corridor is the main
				// timeline, not a composite's inner links.
				flow := other.e
				if g.isFlow(pl.e) {
					flow = pl.e
				}
				if tie.structural && tie.rel == RelPartOf &&
					g.nodes[tie.from].kind == KindEvent && g.nodes[tie.to].kind == KindEvent &&
					g.subOwner(flow.from) == tie.to && g.subOwner(flow.to) == tie.to {
					cost += 1.5
				} else {
					cost += 2.0
				}
			case other.e.rel == pl.e.rel:
				cost += 1.0
			default:
				cost += 0.5
			}
		}
	}
	return cost
}

func (g *graph) leastBad(e *edge, cands []routed,
	line func(*edge, routed) polyline, placed []polyline) int {
	best, bestScore := 0, 1e18
	for i, c := range cands {
		pl := line(e, c)
		score := g.crossingCost(pl, placed) + 0.5*g.grazeCount(pl.pts, e) + detourTax(pl.pts) +
			100*float64(g.hitCount(pl.pts, e))
		if score < bestScore {
			best, bestScore = i, score
		}
	}
	return best
}

// ---- geometry ----

// detourTax charges a tie whose path runs far past its direct distance —
// a clean loop swinging around the whole diagram still reads as lost
// (v7P9). Up to 1.5× direct is free (C-routes and flank
// bypasses live there); beyond that each multiple costs a full crossing,
// so past ~2.5× the tie stubs instead.
func detourTax(pts [][2]int) float64 {
	if len(pts) < 3 {
		return 0
	}
	plen := 0.0
	for i := 0; i+1 < len(pts); i++ {
		plen += math.Hypot(float64(pts[i+1][0]-pts[i][0]), float64(pts[i+1][1]-pts[i][1]))
	}
	last := len(pts) - 1
	direct := math.Hypot(float64(pts[last][0]-pts[0][0]), float64(pts[last][1]-pts[0][1]))
	if direct < 1 {
		return 0
	}
	if r := plen / direct; r > 1.5 {
		return r - 1.5
	}
	return 0
}

// crossPoint returns the intersection of two crossing segments (callers
// check segsCross first; a degenerate pair returns the first endpoint).
func crossPoint(a0, a1, b0, b1 [2]int) (float64, float64) {
	x1, y1 := float64(a0[0]), float64(a0[1])
	x2, y2 := float64(a1[0]), float64(a1[1])
	x3, y3 := float64(b0[0]), float64(b0[1])
	x4, y4 := float64(b1[0]), float64(b1[1])
	den := (x1-x2)*(y3-y4) - (y1-y2)*(x3-x4)
	if den == 0 {
		return x1, y1
	}
	t := ((x1-x3)*(y3-y4) - (y1-y3)*(x3-x4)) / den
	return x1 + t*(x2-x1), y1 + t*(y2-y1)
}

// cutsFlowChord reports whether the segment a0→a1 properly crosses the
// pinned chord of any flow edge (leads-to / boundary link) between placed
// nodes — bottom centre to top centre, the corridor a flow edge holds
// before routing (v7P6). A pre-routing stand-in for the router's
// prohibitive corridor cut, used where ports are still being decided.
func (g *graph) cutsFlowChord(a0, a1 [2]int) bool {
	for _, fe := range g.edges {
		if !g.isFlow(fe) {
			continue
		}
		f, t := g.nodes[fe.from], g.nodes[fe.to]
		if !f.placed || !t.placed {
			continue
		}
		b0 := [2]int{f.x + f.w/2, f.y + f.h}
		b1 := [2]int{t.x + t.w/2, t.y}
		if t.y+t.h/2 < f.y+f.h/2 {
			b0 = [2]int{f.x + f.w/2, f.y}
			b1 = [2]int{t.x + t.w/2, t.y + t.h}
		}
		if segsCross(a0, a1, b0, b1) {
			return true
		}
	}
	return false
}

func segsCross(a0, a1, b0, b1 [2]int) bool {
	d := func(p, q, r [2]int) int {
		return (q[0]-p[0])*(r[1]-p[1]) - (q[1]-p[1])*(r[0]-p[0])
	}
	d1 := d(b0, b1, a0)
	d2 := d(b0, b1, a1)
	d3 := d(a0, a1, b0)
	d4 := d(a0, a1, b1)
	return ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) &&
		((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0))
}

func segIntersectsBox(p0, p1 [2]int, x0, y0, x1, y1 int) bool {
	if x0 > x1 || y0 > y1 {
		return false
	}
	inside := func(p [2]int) bool {
		return p[0] > x0 && p[0] < x1 && p[1] > y0 && p[1] < y1
	}
	if inside(p0) || inside(p1) {
		return true
	}
	corners := [4][2]int{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
	for i := 0; i < 4; i++ {
		if segsCross(p0, p1, corners[i], corners[(i+1)%4]) {
			return true
		}
	}
	return false
}
