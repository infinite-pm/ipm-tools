package layout7

import (
	"sort"
	"strconv"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// groups implements v7P4 — aux attaches in groups: on the event's row,
// wholes outward, parts above, concepts down — plus the v7P5 pieces that
// feed it (sibling ties as declared affinity; the onion layer's position).
//
// Every aux node receives an offset RELATIVE to its anchor event's top-left
// corner; absolute positions appear only in the place stage (the spec's
// "positions are the LAST step"). Convention: thing groups take the LEFT
// side of their event, event-expressed concepts the RIGHT (the spec states
// the grammar for the spine's right side and mirrors the left; things
// left / concepts right is the adopted canon the acceptance targets pin).

type relPos struct {
	event  int // anchor event node idx
	dx, dy int // offset of the node's top-left vs the event's top-left
}

type groupsPlan struct {
	rel map[int]relPos // aux node idx -> relative position

	// stackNext chains each band-stack member to the one BELOW it (v7P4:
	// "siblings … never split"): the no-overlap floor steps a member
	// together with its stack suffix, so a stack yields space as one
	// line instead of leapfrogging member past member (leapfrogging
	// sank Miss Scarlet past her whole band; grow vertical space and
	// keep her beside Professor Plum).
	stackNext map[int]int

	// treeSpan is an exclusive subtree's vertical footprint around its
	// root, relative to the root's own dy (min offset, max offset+h):
	// band siblings re-stack clear of it — more vertical space instead
	// of interleaving with the tree's rows. treeOf
	// maps each tree member back to its root.
	treeSpan map[int][2]int
	treeOf   map[int]int

	// divertedLeaf marks sole leaf concepts sent into the concept COLUMN
	// by the sibling-spot exception — the column re-stacks them when a
	// bracket recentres above.
	divertedLeaf map[int]bool

	// extents in px beyond the event's own box, per event node idx
	leftExt, rightExt, aboveExt, belowExt map[int]int
}

func newGroupsPlan() *groupsPlan {
	return &groupsPlan{
		rel:          map[int]relPos{},
		stackNext:    map[int]int{},
		treeSpan:     map[int][2]int{},
		treeOf:       map[int]int{},
		divertedLeaf: map[int]bool{},
		leftExt:      map[int]int{},
		rightExt:     map[int]int{},
		aboveExt:     map[int]int{},
		belowExt:     map[int]int{},
	}
}

// affinityOrder implements v7P4's subgroup ordering for one sibling stack:
// "siblings sharing a FURTHER connection cluster ADJACENT … clusters and
// loose siblings then follow declaration order (a cluster sorts by its
// first-declared member)". A same-anchor sibling tie is a DECLARED affinity
// and feeds the same rule (v7P5).
func (g *graph) affinityOrder(siblings []int) []int {
	if len(siblings) < 3 {
		return append([]int(nil), siblings...)
	}
	pos := map[int]int{}
	for i, s := range siblings {
		pos[s] = i
	}
	// with a soft anchor (Options.Anchor: the zoom canvas's all-open
	// layout) the order is the ANCHOR's — siblings sorted by where the
	// anchor had them (top to bottom, then left to right) — so a stack
	// keeps its order from state to state; declaration order breaks ties
	if g.opts.Anchor != nil {
		all := true
		ay := map[int][2]int{}
		for _, s := range siblings {
			p, has := g.opts.Anchor[strconv.Itoa(g.nodes[s].id)]
			if !has {
				all = false
				break
			}
			ay[s] = p
		}
		if all {
			ordered := append([]int(nil), siblings...)
			sort.SliceStable(ordered, func(a, b int) bool {
				pa, pb := ay[ordered[a]], ay[ordered[b]]
				if pa[1] != pb[1] {
					return pa[1] < pb[1]
				}
				return pa[0] < pb[0]
			})
			for i, s := range ordered {
				pos[s] = i
			}
		}
	}
	// union siblings that share a further connection or a tie
	parent := map[int]int{}
	var find func(int) int
	find = func(a int) int {
		if parent[a] == a {
			return a
		}
		parent[a] = find(parent[a])
		return parent[a]
	}
	for _, s := range siblings {
		parent[s] = s
	}
	// shared further connection: a common placing target
	targets := map[int][]int{} // target -> siblings pointing at it
	for _, s := range siblings {
		for _, e := range g.out[s] {
			if g.isPlacing(e) {
				targets[e.to] = append(targets[e.to], s)
			}
		}
	}
	for _, group := range targets {
		for i := 1; i < len(group); i++ {
			parent[find(group[0])] = find(group[i])
		}
	}
	// sibling ties (v7P5): pull the pair adjacent
	for _, s := range siblings {
		for _, e := range append(append([]*edge{}, g.out[s]...), g.in[s]...) {
			if !e.sameTie {
				continue
			}
			other := e.from
			if other == s {
				other = e.to
			}
			if _, ok := pos[other]; ok {
				parent[find(s)] = find(other)
			}
		}
	}
	// clusters keyed and internally sorted by declaration order
	clusters := map[int][]int{}
	for _, s := range siblings {
		r := find(s)
		clusters[r] = append(clusters[r], s)
	}
	type cl struct {
		first   int
		members []int
	}
	var ordered []cl
	for _, members := range clusters {
		sort.Slice(members, func(a, b int) bool { return pos[members[a]] < pos[members[b]] })
		ordered = append(ordered, cl{first: pos[members[0]], members: members})
	}
	sort.Slice(ordered, func(a, b int) bool { return ordered[a].first < ordered[b].first })
	var out []int
	for _, c := range ordered {
		out = append(out, c.members...)
	}
	return out
}

// pullOf reports where a member's FURTHER event connections point relative
// to its anchor event: +1 downstream (below, v7P3: time reads down), -1
// upstream, 0 none/unrelated. Used to order a stack so each member sits
// nearest its further connection — the affinity force of v7P4, which kills
// the crossing a top-placed member's downward tie would otherwise make
// over its siblings' edges.
func (g *graph) pullOf(member, anchorEv int) int {
	pull := 0
	for _, e := range append(append([]*edge{}, g.out[member]...), g.in[member]...) {
		if !e.demotedTie {
			continue
		}
		other := e.to
		if other == member {
			other = e.from
		}
		if g.nodes[other].kind != KindEvent {
			continue
		}
		switch {
		case g.leadsToDownstream(anchorEv, other):
			pull++
		case g.leadsToDownstream(other, anchorEv):
			pull--
		}
	}
	switch {
	case pull > 0:
		return 1
	case pull < 0:
		return -1
	}
	return 0
}

// leadsToDownstream: b is reachable from a over leads-to edges.
func (g *graph) leadsToDownstream(a, b int) bool {
	if a == b {
		return false
	}
	seen := map[int]bool{a: true}
	queue := []int{a}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range g.out[cur] {
			if e.rel != RelLeadsTo {
				continue
			}
			if e.to == b {
				return true
			}
			if !seen[e.to] {
				seen[e.to] = true
				queue = append(queue, e.to)
			}
		}
	}
	return false
}

// pullOrder stable-sorts stack members by pull (-1 top, 0 middle, +1
// bottom), preserving the affinity/declaration order within equal pulls so
// clusters stay adjacent.
func (g *graph) pullOrder(members []int, anchorEv int) []int {
	if anchorEv < 0 || len(members) < 2 {
		return members
	}
	out := append([]int(nil), members...)
	sort.SliceStable(out, func(a, b int) bool {
		return g.pullOf(out[a], anchorEv) < g.pullOf(out[b], anchorEv)
	})
	return out
}

// buildGroups computes every aux node's relative position and each event's
// extents.
func (g *graph) buildGroups(m *membership) *groupsPlan {
	p := newGroupsPlan()

	// roots per event and side, in connector declaration order
	leftRoots := map[int][]int{}  // event -> thing structure anchors (tPe)
	rightRoots := map[int][]int{} // event -> concept anchors (eXc)
	for _, e := range g.edges {
		if !e.structural {
			continue
		}
		if e.rel == RelPartOf && g.nodes[e.from].kind != KindEvent && g.nodes[e.to].kind == KindEvent {
			leftRoots[e.to] = append(leftRoots[e.to], e.from)
		}
		if e.rel == RelExpresses && g.nodes[e.from].kind == KindEvent && g.nodes[e.to].kind == KindConcept {
			rightRoots[e.from] = append(rightRoots[e.from], e.to)
		}
		// an event's own WHOLE (e --P--> t): "the whole of an outgoing edge
		// goes OUTWARD" (v7P4) — outward from the spine is the right band.
		if e.rel == RelPartOf && g.nodes[e.from].kind == KindEvent && g.nodes[e.to].kind != KindEvent {
			rightRoots[e.from] = append(rightRoots[e.from], e.to)
		}
	}

	// Span-flank election (v7P4): two thing roots stacked in ONE anchor
	// band that BOTH tie further along the spine cannot both draw clean
	// from one flank — whichever sits higher must fan past the other's
	// edges. The band's widest-spanning root — part-of strictly MORE
	// same-component events than every band rival (an equal span keeps the
	// canon: the split needs a unique protagonist) — takes the RIGHT flank
	// instead, when that flank is free over every event it ties: no
	// concept or whole band, no sub-event column, balanced split in force
	// (user: "Patrick should be on opposite side then other things").
	// Cross-component ties ride the v7P2 grid, not this flank, so they do
	// not count toward the span.
	hasSubGrid := map[int]bool{}
	for _, e := range g.edges {
		if e.structural && e.rel == RelPartOf &&
			g.nodes[e.from].kind == KindEvent && g.nodes[e.to].kind == KindEvent {
			hasSubGrid[e.to] = true
		}
	}
	tiedEvents := func(r int) []int {
		var evs []int
		for _, e := range g.edges {
			if e.rel != RelPartOf || e.from != r || g.nodes[e.to].kind != KindEvent {
				continue
			}
			if (!e.structural && !e.demotedTie) || g.nodes[e.to].comp != g.nodes[r].comp {
				continue
			}
			evs = append(evs, e.to)
		}
		return evs
	}
	spanHints := g.sideHints()
	for _, comp := range g.comps {
		for _, ev := range comp.events {
			roots := leftRoots[ev]
			if len(roots) < 2 || spanHints[ev] != 0 {
				continue
			}
			rivals, best, bestSpan, unique := 0, -1, 0, false
			for _, r := range roots {
				s := len(tiedEvents(r))
				if s >= 2 {
					rivals++
				}
				if s > bestSpan {
					best, bestSpan, unique = r, s, true
				} else if s == bestSpan {
					unique = false
				}
			}
			if rivals < 2 || !unique || bestSpan < 2 {
				continue
			}
			free := true
			for _, te := range tiedEvents(best) {
				if len(rightRoots[te]) > 0 || hasSubGrid[te] || spanHints[te] != 0 {
					free = false
					break
				}
			}
			if !free {
				continue
			}
			kept := roots[:0]
			for _, r := range roots {
				if r != best {
					kept = append(kept, r)
				}
			}
			leftRoots[ev] = kept
			rightRoots[ev] = append(rightRoots[ev], best)
		}
	}

	placed := map[int]bool{}
	// place registers a node's offset relative to its event. Aux never
	// overlaps aux (v7P8's minimums): a placement landing on an
	// already-placed box in the same frame steps DOWN until clear —
	// sibling subtrees meeting in one column stay separated, and the
	// structural router bows their edges around (v7P9).
	place := func(event, n, dx, dy int) {
		for guard := 0; guard < 64; guard++ {
			clear := true
			for j, rp := range p.rel {
				if j == n || rp.event != event {
					continue
				}
				jb := g.nodes[j]
				if dx < rp.dx+jb.w+10 && rp.dx < dx+g.nodes[n].w+10 &&
					dy < rp.dy+jb.h+10 && rp.dy < dy+g.nodes[n].h+10 {
					clear = false
					break
				}
			}
			if clear {
				break
			}
			dy += GridStep
		}
		p.rel[n] = relPos{event: event, dx: dx, dy: dy}
		placed[n] = true
	}

	// stackAt lays SIBLINGS as one vertical line on a shared CENTRE line
	// (v7P4: "siblings of one generation and one arrow direction never
	// split"); the stack's middle sits exactly on the referent's centre.
	stackAt := func(event int, members []int, colCenterX, refCenterY int) {
		total := 0
		for i, n := range members {
			if i > 0 {
				total += StackGap
			}
			total += g.nodes[n].h
		}
		y := refCenterY - total/2
		for i, n := range members {
			place(event, n, colCenterX-g.nodes[n].w/2, y)
			y += g.nodes[n].h + StackGap
			if i+1 < len(members) {
				p.stackNext[n] = members[i+1] // the floor keeps the stack one line
			}
		}
	}
	// colStep is the centre-to-centre distance of the NEXT column outward
	// for the given neighbours (v7P8's base column gap between the boxes).
	colStep := func(fromW, toW int) int { return ColGap + fromW/2 + toW/2 }
	maxW := func(members []int) int {
		w := 0
		for _, n := range members {
			if g.nodes[n].w > w {
				w = g.nodes[n].w
			}
		}
		return w
	}

	// orientTree lays an EXCLUSIVE subtree out as layered generations
	// hanging off its root — v7P4's pure grammar at band scale
	// (a hierarchy the root fully owns renders as if it were a
	// separate component; in the zoom canvas a click on the root folds
	// it). Expresses descends, part-of ascends, siblings share their
	// generation row, parents centre over children (barycentre + one
	// upward sweep, VPSC per row); a chain is the degenerate one-column
	// tree. Offsets flatten onto the anchor EVENT through the root's own
	// band spot, so extents, the rigid follow and the floor's
	// descendant-stepping see one group.
	orientTree := func(event, root int, members []int) {
		clo := append([]int{root}, members...)
		inClo := map[int]bool{}
		for _, n := range clo {
			inClo[n] = true
		}
		// generation index: longest path over placing edges, oriented
		// DOWN (p --P--> w: w below p; x --X--> c: c below x)
		layerOf := map[int]int{}
		var layer func(int, map[int]bool) int
		layer = func(n int, seen map[int]bool) int {
			if l, ok := layerOf[n]; ok {
				return l
			}
			if seen[n] {
				return 0
			}
			seen[n] = true
			l := 0
			for _, e := range g.in[n] {
				if !g.isPlacing(e) || !inClo[e.from] {
					continue
				}
				if pl := layer(e.from, seen) + 1; pl > l {
					l = pl
				}
			}
			delete(seen, n)
			layerOf[n] = l
			return l
		}
		maxLayer := 0
		for _, n := range clo {
			if l := layer(n, map[int]bool{}); l > maxLayer {
				maxLayer = l
			}
		}
		rows := make([][]int, maxLayer+1)
		for _, n := range clo { // declaration order within a row
			rows[layerOf[n]] = append(rows[layerOf[n]], n)
		}
		for r := range rows {
			rows[r] = g.affinityOrder(rows[r])
		}
		// per-lane Y: each member one stack gap below its own parents'
		// bottoms (a tall member never inflates a sibling's lane)
		nodeY := map[int]int{}
		for _, row := range rows {
			for _, n := range row {
				y := 0
				for _, e := range g.in[n] {
					if !g.isPlacing(e) || !inClo[e.from] {
						continue
					}
					if py, ok := nodeY[e.from]; ok {
						if b := py + g.nodes[e.from].h + StackGap; b > y {
							y = b
						}
					}
				}
				nodeY[n] = y
			}
		}
		// X: barycentre over placed neighbours, downward pass then one
		// upward sweep, each row ONE separation problem (v7P8)
		cx := map[int]float64{}
		soleParented := func(o int) bool {
			parents := 0
			for _, pe := range g.in[o] {
				if g.isPlacing(pe) && inClo[pe.from] {
					parents++
				}
			}
			return parents <= 1
		}
		neighbours := func(n int, placedOnly bool) []int {
			var out []int
			for _, e := range append(append([]*edge{}, g.out[n]...), g.in[n]...) {
				if !g.isPlacing(e) {
					continue
				}
				o := e.to
				if o == n {
					o = e.from
				}
				if !inClo[o] {
					continue
				}
				// a JOIN centres under its parents — it never pulls a
				// parent off its own chain (the skeleton's rule: S stays
				// on the start event; app-pods dragged
				// toward the shared wr-manage kinked its tPt chain)
				if e.from == n && !soleParented(e.to) {
					continue
				}
				if placedOnly {
					if _, ok := cx[o]; !ok {
						continue
					}
				}
				out = append(out, o)
			}
			return out
		}
		solveRow := func(row []int, placedOnly bool) {
			desired := map[int]float64{}
			cursor := 0.0
			for _, n := range row {
				ns := neighbours(n, placedOnly)
				if len(ns) == 0 {
					desired[n] = cursor + float64(g.nodes[n].w)/2
				} else {
					sum := 0.0
					for _, o := range ns {
						sum += cx[o]
					}
					desired[n] = sum / float64(len(ns))
				}
				cursor = desired[n] + float64(g.nodes[n].w)/2 + ColGap
			}
			ordered := append([]int(nil), row...)
			sort.SliceStable(ordered, func(a, b int) bool { return desired[ordered[a]] < desired[ordered[b]] })
			vars := make([]layout.VPSCVar, len(ordered))
			for i, n := range ordered {
				vars[i] = layout.VPSCVar{Desired: desired[n] - float64(g.nodes[n].w)/2, Weight: 1}
			}
			var cons []layout.VPSCConstraint
			for i := 0; i+1 < len(ordered); i++ {
				cons = append(cons, layout.VPSCConstraint{
					Left: i, Right: i + 1,
					Gap: float64(g.nodes[ordered[i]].w + ColGap),
				})
			}
			solved := layout.SolveSeparations(vars, cons)
			for i, n := range ordered {
				cx[n] = solved[i] + float64(g.nodes[n].w)/2
			}
		}
		for _, row := range rows {
			solveRow(row, true)
		}
		for r := len(rows) - 2; r >= 0; r-- {
			solveRow(rows[r], false)
		}
		// one more downward relaxation: the upward sweep moved parents
		// AFTER their children were solved — children re-centre on the
		// parents' final x (cS drifted off its pair's midpoint into a
		// corner contact without this). Only rows BELOW the root's layer:
		// the root's row is the tree's baseline (every offset is relative
		// to cx[root]) — re-solving it slid the baseline under an
		// ascending tree's top row and overlapped its members.
		for r := layerOf[root] + 1; r < len(rows); r++ {
			solveRow(rows[r], false)
		}
		// register relative to the root's band spot; ONE rounding
		// direction for the whole tree (v7P8: equal gaps stay equal)
		rp := p.rel[root]
		type off struct{ dx, dy int }
		offs := map[int]off{}
		for _, n := range members {
			dcx := int(cx[n] - cx[root])
			h := dcx + GridStep/2
			if h < 0 {
				h -= GridStep - 1
			}
			dcx = h / GridStep * GridStep
			offs[n] = off{
				dx: rp.dx + g.nodes[root].w/2 + dcx - g.nodes[n].w/2,
				dy: rp.dy + nodeY[n] - nodeY[root],
			}
		}
		// v7P6: the anchor event's flow corridor is RESERVED — a tree row
		// reaching over the event column would sit on the S/E or leads-to
		// line below (or above) it. The tree shifts aside as ONE unit,
		// away from the event, to the root's own flank; the root keeps
		// its band spot.
		hasFlow := func(below bool) bool {
			sub := false
			for _, e := range g.out[event] {
				if !g.isPlacing(e) {
					continue
				}
				if e.rel == RelLeadsTo && g.nodes[e.to].kind == KindEvent {
					if below {
						return true // a successor's edge runs down the column
					}
				}
				if e.rel == RelPartOf && g.nodes[e.to].kind == KindEvent {
					sub = true // a sub-event gets no S/E cap of its own
				}
			}
			if !below {
				for _, e := range g.in[event] {
					if g.isPlacing(e) && e.rel == RelLeadsTo && g.nodes[e.from].kind == KindEvent {
						return true // a predecessor's edge lands from above
					}
				}
			}
			return !sub // a top-level end (start) gets E below (S above)
		}
		lo, hi, above, below := 1<<30, -(1 << 30), false, false
		for _, n := range members {
			o := offs[n]
			if o.dx < lo {
				lo = o.dx
			}
			if o.dx+g.nodes[n].w > hi {
				hi = o.dx + g.nodes[n].w
			}
			if o.dy < rp.dy {
				above = true
			}
			if o.dy > rp.dy {
				below = true
			}
		}
		shift := 0
		evW := g.nodes[event].w
		if (below && hasFlow(true)) || (above && hasFlow(false)) {
			if rp.dx < 0 && hi > -GridStep/2 { // left-band root: clear the column leftward
				shift = -(hi + GridStep/2)
			} else if rp.dx >= 0 && lo < evW+GridStep/2 { // right-band root: rightward
				shift = evW + GridStep/2 - lo
			}
			if h := shift; h >= 0 {
				shift = (h + GridStep - 1) / GridStep * GridStep
			} else {
				shift = -((-h + GridStep - 1) / GridStep * GridStep)
			}
		}
		lo2, hi2 := 0, g.nodes[root].h // the root's own box
		for _, n := range members {
			o := offs[n]
			p.rel[n] = relPos{event: event, dx: o.dx + shift, dy: o.dy}
			placed[n] = true
			g.nodes[n].pureGen = true
			p.treeOf[n] = root
			if d := o.dy - rp.dy; d < lo2 {
				lo2 = d
			}
			if d := o.dy - rp.dy + g.nodes[n].h; d > hi2 {
				hi2 = d
			}
		}
		p.treeSpan[root] = [2]int{lo2, hi2}
	}

	// restackAroundTrees pushes a band stack's members clear of any
	// EXCLUSIVE subtree grown inside it (more vertical
	// space, siblings stay one line): the first tree-owning root keeps
	// its row; members ABOVE it re-stack upward from the tree's top,
	// members BELOW re-stack downward from its bottom, original pitch
	// preserved. Gaps only grow (v7P8).
	restackAroundTrees := func(band []int) {
		anchor := -1
		for _, n := range band {
			if _, ok := p.treeSpan[n]; ok {
				anchor = n
				break
			}
		}
		if anchor < 0 {
			return
		}
		span := p.treeSpan[anchor]
		base := p.rel[anchor].dy
		footprint := func(n int) (int, int) {
			rp := p.rel[n]
			if s, ok := p.treeSpan[n]; ok {
				return rp.dy + s[0], rp.dy + s[1]
			}
			return rp.dy, rp.dy + g.nodes[n].h
		}
		idx := 0
		for i, n := range band {
			if n == anchor {
				idx = i
				break
			}
		}
		top := base + span[0] // the tree's upper edge
		for i := idx - 1; i >= 0; i-- {
			n := band[i]
			lo, hi := footprint(n)
			if hi+StackGap > top {
				d := hi + StackGap - top // shift up by d
				rp := p.rel[n]
				rp.dy -= d
				p.rel[n] = rp
				g.shiftTree(p, n, -d)
				lo -= d
			}
			top = lo
		}
		bottom := base + span[1] // the tree's lower edge
		for i := idx + 1; i < len(band); i++ {
			n := band[i]
			lo, hi := footprint(n)
			if lo-StackGap < bottom {
				d := bottom - (lo - StackGap) // shift down by d
				rp := p.rel[n]
				rp.dy += d
				p.rel[n] = rp
				g.shiftTree(p, n, d)
				hi += d
			}
			bottom = hi
		}
	}

	// orient expands one member's substructure (v7P4's grammar), outward on
	// `side` (-1 left, +1 right). Concept fans stack BESIDE their expresser
	// (band-style — edges reach each member's facing side without cutting
	// the stack, v7P9); chains step diagonally. A member whose subtree is
	// EXCLUSIVE (self-contained, no other event touches it — the zoom
	// canvas's foldable unit) renders it as layered generations
	// instead.
	var orient func(event, member, side int)
	orient = func(event, member, side int) {
		if sub := g.exclusiveSubtree(member, placed); sub != nil {
			orientTree(event, member, sub)
			return
		}
		mp := p.rel[member]
		memberCenterX := mp.dx + g.nodes[member].w/2
		memberCenterY := mp.dy + g.nodes[member].h/2

		// wholes: "the whole of an outgoing tPt goes OUTWARD on the row" —
		// several wholes of one generation stack as one line one column
		// outward, affinity-ordered (v7P4 covers: B1, B3, B2).
		var wholes []int
		for _, e := range g.out[member] {
			if e.structural && e.rel == RelPartOf && g.nodes[e.to].kind != KindEvent && !placed[e.to] {
				wholes = append(wholes, e.to)
			}
		}
		if len(wholes) > 0 {
			wholes = g.affinityOrder(wholes)
			cx := memberCenterX + side*colStep(g.nodes[member].w, maxW(wholes))
			stackAt(event, wholes, cx, memberCenterY)
			for _, w := range wholes {
				orient(event, w, side)
			}
		}

		// incoming parts: "INCOMING parts stack ABOVE their whole" — a
		// tower on the whole's centre line, nearest part first.
		var parts []int
		for _, e := range g.in[member] {
			if e.structural && e.rel == RelPartOf && g.nodes[e.from].kind != KindEvent && !placed[e.from] {
				parts = append(parts, e.from)
			}
		}
		y := mp.dy
		for _, part := range parts {
			y -= StackGap + g.nodes[part].h
			place(event, part, memberCenterX-g.nodes[part].w/2, y)
			orient(event, part, side)
		}

		// concepts: "concepts (tXc, cXc) step DOWN-AND-OUTWARD, so a
		// concept chain reads as one diagonal line". Sibling concepts
		// continue the same diagonal. In a pure component (no event, no
		// row) a LONE chain keeps the standalone centre-line column; a fan
		// or a mid-structure expresser still steps diagonally — a shared
		// column would put every fan edge through the stack (v7P9).
		var concepts []int
		for _, e := range g.out[member] {
			if e.structural && e.rel == RelExpresses && g.nodes[e.to].kind == KindConcept && !placed[e.to] {
				concepts = append(concepts, e.to)
			}
		}
		switch {
		case len(concepts) == 0:
		case len(concepts) == 1:
			// a sole LEAF concept drops BELOW its expresser, centred —
			// the closest spot (the same call as the inner-fan and
			// composite cases; the diagonal cell read
			// corner-to-corner distant). A concept that HEADS A CHAIN or
			// is shared keeps the down-and-outward diagonal: a cXc chain
			// reads as one diagonal line (v7P4), and a shared concept's
			// column is the pull-slide's rail (v7P7).
			c := concepts[0]
			// a THING's sole concept child drops below — SHARED concepts
			// included: the second user's demoted tie draws long either
			// way, and the anchor spot is the closest (tA→shared
			// kept a corner-to-corner diagonal; the old "rail
			// for the far pull" rationale died with the anchor-reach
			// rule). A concept with its own placing children tree-ifies
			// or chains instead.
			leaf := g.nodes[member].kind == KindThing
			for _, oe := range g.out[c] {
				if g.isPlacing(oe) {
					leaf = false
					break
				}
			}
			if leaf {
				// ... but a shared concept whose OTHER user sits within
				// ONE flow step keeps the diagonal rail: the short-range
				// pull aligns it with BOTH users (the deep-shared ext
				// case); only a FAR second user leaves the concept at the
				// anchor spot below, its tie drawing long
				for _, ue := range g.userEdges(c) {
					u := g.userOf(c, ue)
					if u == member {
						continue
					}
					uev := u
					if g.nodes[u].kind != KindEvent {
						if pe := m.anchors[u].primary; pe != nil {
							if uu := g.userOf(u, pe); g.nodes[uu].kind == KindEvent {
								uev = uu
							}
						}
					}
					if g.nodes[uev].kind == KindEvent && g.flowNear(event, uev) {
						leaf = false
						break
					}
				}
			}
			cx := memberCenterX
			dy := mp.dy + g.nodes[member].h + StackGap
			diverted := false
			if leaf {
				// ... unless the spot right below is a SIBLING's (the
				// member sits in a stack): collision-stepping the concept
				// past the stack reads as one more stack member — it
				// takes the down-and-outward diagonal into the concept
				// column instead, below the bracketing shared concept
				// (Reviewer below Developer)
				for j, rp := range p.rel {
					if rp.event != mp.event || j == c {
						continue
					}
					jb := g.nodes[j]
					if cx-g.nodes[c].w/2 < rp.dx+jb.w+10 && rp.dx < cx+g.nodes[c].w/2+10 &&
						dy < rp.dy+jb.h+10 && rp.dy < dy+g.nodes[c].h+10 {
						leaf = false
						diverted = true
						break
					}
				}
			}
			if !leaf {
				cx = memberCenterX + side*colStep(g.nodes[member].w, g.nodes[c].w)
				if diverted {
					// the DIVERTED leaf joins the column at its rhythm —
					// directly under the column's lowest member, not at its
					// own diagonal offset; with a bracketed shared concept
					// above, that spot MIRRORS it about their common owner
					// (Reviewer symmetric to Developer
					// about bob). Chains and shared concepts keep the
					// diagonal (their line is the point).
					bottom, found := 0, false
					for j, rp := range p.rel {
						if rp.event != mp.event || j == c {
							continue
						}
						jb := g.nodes[j]
						if cx-g.nodes[c].w/2 < rp.dx+jb.w && rp.dx < cx+g.nodes[c].w/2 {
							if !found || rp.dy+jb.h > bottom {
								bottom = rp.dy + jb.h
							}
							found = true
						}
					}
					if found {
						dy = bottom + StackGap
					}
					p.divertedLeaf[c] = true
				}
			}
			place(event, c, cx-g.nodes[c].w/2, dy)
			orient(event, c, side)
		default:
			// a concept FAN stacks beside its expresser (band-style):
			// every edge reaches its member's facing side without cutting
			// the stack (v7P9)
			ordered := g.affinityOrder(concepts)
			cx := memberCenterX + side*colStep(g.nodes[member].w, maxW(ordered))
			stackAt(event, ordered, cx, memberCenterY)
			for _, c := range ordered {
				orient(event, c, side)
			}
		}
	}

	// Band sides: the spec states the grammar for the spine's right side and
	// MIRRORS the left (v7P4) — so a fork branch's aux takes the branch's
	// OUTER flank, while on-spine events keep the balanced split (things
	// left, concepts right — the adopted canon).
	sides := g.sideHints()
	for _, comp := range g.comps {
		for _, ev := range comp.events {
			evH := g.nodes[ev].h
			hint := sides[ev]

			roots := g.pullOrder(g.affinityOrder(leftRoots[ev]), ev)
			cRoots := g.pullOrder(g.affinityOrder(rightRoots[ev]), ev)
			if hint == 0 {
				// balanced split: things left, concepts right
				if len(roots) > 0 {
					cx := -(ColGap + maxW(roots)/2)
					stackAt(ev, roots, cx, evH/2)
					for _, r := range roots {
						orient(ev, r, -1)
					}
					restackAroundTrees(roots)
				}
				if len(cRoots) > 0 {
					cx := g.nodes[ev].w + ColGap + maxW(cRoots)/2
					stackAt(ev, cRoots, cx, evH/2)
					for _, c := range cRoots {
						orient(ev, c, +1)
					}
					restackAroundTrees(cRoots)
				}
				continue
			}
			// forced outer flank: one band, things first, concepts last
			// (v7P4 subgroup ordering). Exception: a
			// thing FAN (two or more) plus a SOLE leaf concept do NOT
			// share the band — stacked with the fan the concept reads as
			// one more part-of member; it drops BELOW the event,
			// x-centred, and the band centres on the fan alone.
			var below []int
			if len(roots) >= 2 && len(cRoots) == 1 {
				leaf := true
				for _, oe := range g.out[cRoots[0]] {
					if g.isPlacing(oe) {
						leaf = false
						break
					}
				}
				if leaf {
					below, cRoots = cRoots, nil
				}
			}
			band := g.pullOrder(append(append([]int{}, roots...), cRoots...), ev)
			if len(band) == 0 {
				continue
			}
			side := hint
			if side == -2 {
				side = -1
			}
			cx := -(ColGap + maxW(band)/2)
			if side > 0 {
				cx = g.nodes[ev].w + ColGap + maxW(band)/2
			}
			if hint == -2 {
				// BOTH row sides are part-of corridors (parent's edge left,
				// sub column right — v7P6), so the band drops BELOW the row
				// on the event's own centre line (the sole
				// concept drops below; shortest edge, layers stay clean).
				// If the composite itself flows downward, below is a flow
				// corridor too — then the band keeps v7P4's down-and-
				// outward diagonal on the left flank.
				flowsDown := false
				for _, fe := range g.out[ev] {
					if fe.rel == RelLeadsTo && g.nodes[fe.to].kind == KindEvent {
						flowsDown = true
						break
					}
				}
				if !flowsDown {
					// The below-centred band is ONE GENERATION: it lies in a
					// ROW under its owner (same-level
					// concepts share the horizontal), side by side and
					// centred — a column would read as layers and push the
					// lower connectors around the upper boxes (v7P9).
					total := 0
					for i, n := range band {
						if i > 0 {
							total += StackGap
						}
						total += g.nodes[n].w
					}
					x := g.nodes[ev].w/2 - total/2
					for _, n := range band {
						place(ev, n, x, evH+StackGap)
						orient(ev, n, side)
						x += g.nodes[n].w + StackGap
					}
					continue
				}
				total := 0
				for i, n := range band {
					if i > 0 {
						total += StackGap
					}
					total += g.nodes[n].h
				}
				stackAt(ev, band, cx, evH+StackGap+total/2)
			} else {
				stackAt(ev, band, cx, evH/2)
			}
			for _, r := range band {
				orient(ev, r, side)
			}
			restackAroundTrees(band)
			for _, c := range below {
				place(ev, c, g.nodes[ev].w/2-g.nodes[c].w/2, evH+StackGap)
				orient(ev, c, side)
			}
		}
	}

	// Brackets (v7P4/P7): a shared fan-in child whose two users ended up
	// ADJACENT in one stack re-centres on the pair's midpoint one step
	// outward — "the shared node can bracket the pair".
	for i := range g.nodes {
		if g.nodes[i].kind == KindEvent {
			continue
		}
		var users []int
		for _, e := range g.userEdges(i) {
			if e.structural || e.demotedTie {
				users = append(users, g.userOf(i, e))
			}
		}
		if len(users) != 2 {
			continue
		}
		a, ok1 := p.rel[users[0]]
		b, ok2 := p.rel[users[1]]
		aCx := a.dx + g.nodes[users[0]].w/2
		bCx := b.dx + g.nodes[users[1]].w/2
		if !ok1 || !ok2 || a.event != b.event || aCx != bCx {
			continue // users must share one stack (centre line)
		}
		self, ok := p.rel[i]
		if !ok || self.event != a.event {
			continue
		}
		// adjacent in the stack?
		top, bot := a, b
		topN, botN := users[0], users[1]
		if top.dy > bot.dy {
			top, bot = bot, top
			topN, botN = botN, topN
		}
		if bot.dy-(top.dy+g.nodes[topN].h) > StackGap {
			continue // not adjacent — no bracket
		}
		side := -1
		if aCx >= g.nodes[a.event].w/2 {
			side = +1
		}
		midY := (top.dy + g.nodes[topN].h/2 + bot.dy + g.nodes[botN].h/2) / 2
		pairW := maxInt(g.nodes[topN].w, g.nodes[botN].w)
		cx := aCx + side*(ColGap+pairW/2+g.nodes[i].w/2)
		oldRel := self
		p.rel[i] = relPos{
			event: a.event,
			dx:    cx - g.nodes[i].w/2,
			dy:    midY - g.nodes[i].h/2,
		}
		// the concept COLUMN follows the bracket: a diverted sole leaf
		// stacked at rhythm below this concept's old spot shifts by the
		// same delta, keeping the rhythm — and landing SYMMETRIC to the
		// bracketed concept about their shared owner
		// (Reviewer mirrors Developer about bob)
		if d := p.rel[i].dy - oldRel.dy; d != 0 {
			newCx := cx
			for j, rp := range p.rel {
				if j == i || rp.event != oldRel.event || !p.divertedLeaf[j] {
					continue
				}
				jb := g.nodes[j]
				if rp.dx < newCx+g.nodes[i].w/2 && newCx-g.nodes[i].w/2 < rp.dx+jb.w &&
					rp.dy >= oldRel.dy+g.nodes[i].h {
					rp.dy += d
					p.rel[j] = rp
				}
			}
		}
	}

	// Pure aux structures (v7P1: no events anywhere) lay out as LAYERED
	// GENERATIONS, exactly like the skeleton lays out events
	// (the thing-hierarchy diamond): part-of ascends — a part
	// sits one row ABOVE its whole — and expresses descends — an expressed
	// concept one row BELOW its expresser. Siblings sit side by side in
	// their generation row; every member centres on its connections
	// (barycentre + one upward sweep), and each row resolves as ONE
	// separation problem (v7P8). A diamond renders as a diamond.
	for _, comp := range g.comps {
		if len(comp.events) > 0 {
			continue
		}
		var pure []int
		for _, i := range comp.aux {
			if m.anchors[i].satelliteOf == nil {
				pure = append(pure, i)
			}
		}
		if len(pure) == 0 {
			continue
		}
		if _, ok := p.rel[pure[0]]; ok {
			continue
		}
		inPure := map[int]bool{}
		for _, i := range pure {
			inPure[i] = true
		}
		// Exclusive sole-parent LEAF concepts whose parent keeps a REAL
		// hierarchy child are AUX of their thing, not generation members
		// (v7P4): they LIFT out of the levels and ride x-centred directly
		// ABOVE it — left in the levels they pile into the join whole's
		// row (the k8s workload-taxonomy case). Shared concepts, concepts
		// with children, and things whose children are ONLY leaf concepts
		// keep the level layout.
		liftedOf := map[int][]int{} // parent -> lifted concepts, declaration order
		isLifted := map[int]bool{}
		{
			soleParent := map[int]int{}
			for _, i := range pure {
				if g.nodes[i].kind != KindConcept {
					continue
				}
				parents, children := []int{}, 0
				for _, e := range g.in[i] {
					if g.isPlacing(e) && inPure[e.from] {
						parents = append(parents, e.from)
					}
				}
				for _, e := range g.out[i] {
					if g.isPlacing(e) && inPure[e.to] {
						children++
					}
				}
				if len(parents) == 1 && children == 0 {
					soleParent[i] = parents[0]
				}
			}
			for _, i := range pure {
				par, ok := soleParent[i]
				if !ok {
					continue
				}
				// A real hierarchy CHILD — the generation below par — not
				// par's own parent: a chain member's leaf child stays in
				// the levels.
				realChild := false
				for _, e := range g.out[par] {
					if !g.isPlacing(e) || !inPure[e.to] {
						continue
					}
					if _, leaf := soleParent[e.to]; !leaf {
						realChild = true
						break
					}
				}
				if realChild {
					liftedOf[par] = append(liftedOf[par], i)
					isLifted[i] = true
				}
			}
		}
		// generation index: longest path over placing edges, oriented DOWN
		// (p --P--> w: w below p; x --X--> c: c below x)
		layerOf := map[int]int{}
		var layer func(int, map[int]bool) int
		layer = func(n int, seen map[int]bool) int {
			if l, ok := layerOf[n]; ok {
				return l
			}
			if seen[n] {
				return 0
			}
			seen[n] = true
			l := 0
			for _, e := range g.in[n] {
				// generation follows the RELATION, demoted or not — a whole
				// is one row below its part even when that edge lost the
				// placement election (anchor-and-tie)
				if !g.isPlacing(e) || !inPure[e.from] {
					continue
				}
				if pl := layer(e.from, seen) + 1; pl > l {
					l = pl
				}
			}
			delete(seen, n)
			layerOf[n] = l
			return l
		}
		maxLayer := 0
		for _, i := range pure {
			if isLifted[i] {
				continue
			}
			if l := layer(i, map[int]bool{}); l > maxLayer {
				maxLayer = l
			}
		}
		rows := make([][]int, maxLayer+1)
		for _, i := range pure { // declaration order within a row
			if isLifted[i] {
				continue
			}
			rows[layerOf[i]] = append(rows[layerOf[i]], i)
		}
		// A generation is a sibling stack lying down (v7P5): near-to ties
		// and shared further connections cluster members ADJACENT — the
		// tied pair sits side by side and the loose sibling steps away, so
		// the tie draws as one straight horizontal instead of spanning a
		// stranger (tB,tD,tC — not tB,tC,tD).
		for r := range rows {
			rows[r] = g.affinityOrder(rows[r])
		}
		// per-lane Y (the same rhythm the event skeleton keeps): each
		// member sits one stack gap below its own parents' bottoms — a
		// tall member never inflates a sibling's lane.
		nodeY := map[int]int{}
		for _, row := range rows {
			for _, n := range row {
				y := 0
				for _, e := range g.in[n] {
					if !g.isPlacing(e) || !inPure[e.from] {
						continue
					}
					if py, ok := nodeY[e.from]; ok {
						if b := py + g.nodes[e.from].h + StackGap; b > y {
							y = b
						}
					}
				}
				nodeY[n] = y
			}
		}
		frame := pure[0]
		for _, i := range pure { // the frame anchors the rows — never a lifted satellite
			if !isLifted[i] {
				frame = i
				break
			}
		}
		cx := map[int]float64{} // member centre-x
		neighbours := func(n int, placedOnly bool) []int {
			var out []int
			for _, e := range append(append([]*edge{}, g.out[n]...), g.in[n]...) {
				if !g.isPlacing(e) {
					continue
				}
				o := e.to
				if o == n {
					o = e.from
				}
				if !inPure[o] || isLifted[o] {
					// a lifted satellite follows its thing — it never pulls
					// the rows (a missing centre would read as x=0)
					continue
				}
				// a JOIN centres under its parents and never pulls one off
				// its chain (app-pods dragged toward the
				// shared wr-manage kinked its tPt chain)
				if e.from == n {
					parents := 0
					for _, pe := range g.in[e.to] {
						if g.isPlacing(pe) && inPure[pe.from] {
							parents++
						}
					}
					if parents > 1 {
						continue
					}
				}
				if placedOnly {
					if _, ok := cx[o]; !ok {
						continue
					}
				}
				out = append(out, o)
			}
			return out
		}
		solveRow := func(row []int, placedOnly bool) {
			desired := map[int]float64{}
			cursor := 0.0
			for _, n := range row {
				ns := neighbours(n, placedOnly)
				if len(ns) == 0 {
					desired[n] = cursor + float64(g.nodes[n].w)/2
				} else {
					sum := 0.0
					for _, o := range ns {
						sum += cx[o]
					}
					desired[n] = sum / float64(len(ns))
				}
				cursor = desired[n] + float64(g.nodes[n].w)/2 + ColGap
			}
			ordered := append([]int(nil), row...)
			sort.SliceStable(ordered, func(a, b int) bool { return desired[ordered[a]] < desired[ordered[b]] })
			vars := make([]layout.VPSCVar, len(ordered))
			for i, n := range ordered {
				vars[i] = layout.VPSCVar{Desired: desired[n] - float64(g.nodes[n].w)/2, Weight: 1}
			}
			var cons []layout.VPSCConstraint
			for i := 0; i+1 < len(ordered); i++ {
				cons = append(cons, layout.VPSCConstraint{
					Left: i, Right: i + 1,
					Gap: float64(g.nodes[ordered[i]].w + ColGap),
				})
			}
			solved := layout.SolveSeparations(vars, cons)
			for i, n := range ordered {
				cx[n] = solved[i] + float64(g.nodes[n].w)/2
			}
		}
		for _, row := range rows { // downward pass
			solveRow(row, true)
		}
		for r := len(rows) - 2; r >= 0; r-- { // upward sweep re-centres over ALL neighbours
			solveRow(rows[r], false)
		}
		for _, row := range rows {
			for _, n := range row {
				dx := int(cx[n]) - g.nodes[n].w/2
				// v7P8: the grid is exact — ONE rounding direction for the
				// whole row (nearest, ties toward +x), so equal continuous
				// gaps stay equal after the snap; rounding away from zero
				// broke row symmetry at the zero crossing (the diamond's
				// left sibling drifted a half step off its mirror)
				h := dx + GridStep/2
				if h < 0 {
					h -= GridStep - 1
				}
				dx = h / GridStep * GridStep
				p.rel[n] = relPos{event: frame, dx: dx, dy: nodeY[n]}
				placed[n] = true
				g.nodes[n].pureGen = true
			}
		}
		// PAIR PARITY (v7P8: "are they really precisely
		// symmetric?"): a node's two sole-parented LEAF children sit one
		// box-plus-gap apart — an ODD number of grid steps — so the
		// uniform snap leaves their midpoint half a step off the parent's
		// centre. The child on the offset side spreads ONE grid step
		// outward (gaps only grow), and the pair mirrors exactly.
		for _, par := range pure {
			if isLifted[par] {
				continue
			}
			var kids []int
			ok := true
			for _, e := range g.out[par] {
				if !g.isPlacing(e) || !inPure[e.to] || isLifted[e.to] {
					continue
				}
				c := e.to
				sole, leafOK := true, true
				for _, ie := range g.in[c] {
					if g.isPlacing(ie) && inPure[ie.from] && ie.from != par {
						sole = false
					}
				}
				for _, oe := range g.out[c] {
					if g.isPlacing(oe) && inPure[oe.to] {
						leafOK = false
					}
				}
				if !sole || !leafOK {
					ok = false
					break
				}
				kids = append(kids, c)
			}
			if !ok || len(kids) != 2 {
				continue
			}
			a, b2 := kids[0], kids[1]
			ra, rb := p.rel[a], p.rel[b2]
			if ra.dy != rb.dy {
				continue
			}
			if ra.dx > rb.dx {
				a, b2 = b2, a
				ra, rb = rb, ra
			}
			pc := p.rel[par].dx + g.nodes[par].w/2
			mid := (ra.dx + g.nodes[a].w/2 + rb.dx + g.nodes[b2].w/2) / 2
			switch mid - pc {
			case GridStep / 2:
				ra.dx -= GridStep
				p.rel[a] = ra
			case -GridStep / 2:
				rb.dx += GridStep
				p.rel[b2] = rb
			}
		}

		// lifted concepts ride above their thing, x-centred, nearest first
		for _, par := range pure {
			cs := liftedOf[par]
			if len(cs) == 0 {
				continue
			}
			base := p.rel[par]
			y := base.dy
			for _, c := range cs {
				y -= g.nodes[c].h + StackGap
				p.rel[c] = relPos{event: frame,
					dx: base.dx + (g.nodes[par].w-g.nodes[c].w)/2, dy: y}
				placed[c] = true
			}
		}
		// the frame is its own reference: its offset MUST be (0,0), or
		// every member's absolute position depends on whether the frame
		// resolved before or after it (map-order nondeterminism).
		if f := p.rel[frame]; f.dx != 0 || f.dy != 0 {
			for _, n := range pure {
				rp := p.rel[n]
				rp.dx -= f.dx
				rp.dy -= f.dy
				p.rel[n] = rp
			}
		}
	}

	// Onion satellites (v7P5): the outermost layer, right next to the
	// partner, outside everything already placed on that side.
	for i := range g.nodes {
		te := m.anchors[i].satelliteOf
		if te == nil {
			continue
		}
		partner := te.from
		if partner == i {
			partner = te.to
		}
		pp, ok := p.rel[partner]
		if !ok {
			continue // partner is an event or unplaced — leave to place stage defaults
		}
		// collect this partner's satellites once, in declaration order
		var sats []int
		for j := range g.nodes {
			t2 := m.anchors[j].satelliteOf
			if t2 == nil {
				continue
			}
			p2 := t2.from
			if p2 == j {
				p2 = t2.to
			}
			if p2 == partner {
				sats = append(sats, j)
			}
		}
		if placed[sats[0]] {
			continue // already laid out via an earlier member
		}
		pCx := pp.dx + g.nodes[partner].w/2
		side := -1
		if g.nodes[pp.event].kind == KindEvent {
			if pCx >= g.nodes[pp.event].w/2 {
				side = +1
			}
		} else {
			// a PURE grid's frame is an aux member, not the midline — the
			// onion satellite wraps from the grid's OUTER flank (v7P5:
			// 'human' sent INTO the grid had its tie
			// cross the descent corridor)
			lo, hi := 1<<30, -(1 << 30)
			for _, rp := range p.rel {
				if rp.event != pp.event {
					continue
				}
				if rp.dx < lo {
					lo = rp.dx
				}
				if rp.dx > hi {
					hi = rp.dx
				}
			}
			if 2*pCx >= lo+hi+g.nodes[partner].w {
				side = +1
			}
		}
		// outermost ON ITS OWN ROWS: beyond every box already placed on
		// that side that the satellite stack would actually meet (v7P5:
		// the layer wraps from outside — but only past boxes in its own
		// y-range; measuring the whole side pushed a satellite three
		// columns from its partner: "nearto(s) are too
		// far away")
		stackH := 0
		for i2, sat := range sats {
			if i2 > 0 {
				stackH += StackGap
			}
			stackH += g.nodes[sat].h
		}
		pcy := pp.dy + g.nodes[partner].h/2
		sy0, sy1 := pcy-stackH/2, pcy+stackH/2
		edgeX := pp.dx // left edge (or right edge for side>0)
		if side > 0 {
			edgeX = pp.dx + g.nodes[partner].w
		}
		for n2, rp := range p.rel {
			if rp.event != pp.event {
				continue
			}
			if rp.dy >= sy1 || rp.dy+g.nodes[n2].h <= sy0 {
				continue // clear of the stack's rows
			}
			if side < 0 && rp.dx < edgeX {
				edgeX = rp.dx
			}
			if side > 0 && rp.dx+g.nodes[n2].w > edgeX {
				edgeX = rp.dx + g.nodes[n2].w
			}
		}
		cx := edgeX + side*(NearGap+maxW(sats)/2)
		stackAt(pp.event, sats, cx, pp.dy+g.nodes[partner].h/2)
		// a satellite's own structure (its concepts, wholes, parts) lays
		// out by the same grammar, outward on its side (v7P4)
		for _, sat := range sats {
			orient(pp.event, sat, side)
		}
	}

	// Extents per event (input to v7P3's pitch and v7P8's row growth).
	for n, rp := range p.rel {
		ev := rp.event
		evn := g.nodes[ev]
		if d := -rp.dx; d > p.leftExt[ev] {
			p.leftExt[ev] = d
		}
		if d := rp.dx + g.nodes[n].w - evn.w; d > p.rightExt[ev] {
			p.rightExt[ev] = d
		}
		if d := -rp.dy; d > p.aboveExt[ev] {
			p.aboveExt[ev] = d
		}
		if d := rp.dy + g.nodes[n].h - evn.h; d > p.belowExt[ev] {
			p.belowExt[ev] = d
		}
	}
	return p
}

// shiftTree moves a re-stacked band member's OWN exclusive-subtree
// members along with it (the tree is one unit, v7P4): the member's rel
// already moved by d; its tree members follow.
func (g *graph) shiftTree(p *groupsPlan, root, d int) {
	if _, ok := p.treeSpan[root]; !ok {
		return
	}
	for i, r := range p.treeOf {
		if r != root {
			continue
		}
		if rp, ok := p.rel[i]; ok {
			rp.dy += d
			p.rel[i] = rp
		}
	}
}

// exclusiveSubtree returns the aux nodes a connector-attached root fully
// OWNS: the closure reachable from the root over placing tPt/tXc/cXc
// edges, when that closure is SELF-CONTAINED — no member touches an
// event or any node outside the closure, none is already placed, and the
// closure is a real HIERARCHY (some member places another member; a flat
// fan keeps the band grammar). This is the static mirror of the zoom
// canvas's foldable unit (such a hierarchy renders as
// if it were a separate component, so a click on the root can fold it).
// Returns nil when the subtree does not qualify.
func (g *graph) exclusiveSubtree(root int, placed map[int]bool) []int {
	inClo := map[int]bool{root: true}
	var members []int
	queue := []int{root}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		// follow the ELECTED placement forest (structural edges), never
		// demoted ties — a demoted expresses reaches into aux that
		// anchors elsewhere, possibly another component's (tD→cX)
		for _, e := range append(append([]*edge{}, g.out[n]...), g.in[n]...) {
			if !e.structural || !g.isPlacing(e) {
				continue
			}
			o := e.to
			if o == n {
				o = e.from
			}
			if inClo[o] || g.nodes[o].kind == KindEvent {
				continue // the root's own event connectors stay outside
			}
			inClo[o] = true
			members = append(members, o)
			queue = append(queue, o)
		}
	}
	if len(members) < 1 || (len(members) < 2 && g.nodes[root].kind != KindConcept) {
		// a THING's single leaf keeps the richer sole-leaf grammar
		// (below-drop, sibling divert, strand rescue); a CONCEPT root's
		// single child is the degenerate one-column tree — the old
		// "chain keeps the diagonal" rule read corner-to-corner distant
		// (c5→c6 longer than needed)
		return nil
	}
	for _, n := range members {
		if placed[n] || g.nodes[n].boundary {
			return nil // shared with an earlier frame — not exclusive
		}
		for _, e := range append(append([]*edge{}, g.out[n]...), g.in[n]...) {
			o := e.to
			if o == n {
				o = e.from
			}
			if g.nodes[o].kind == KindEvent || !inClo[o] {
				return nil // a member touches an event or leaves the closure
			}
		}
	}
	// A LINEAR PART-OF CHAIN does not qualify. Layered generations exist so
	// a branching hierarchy reads as one foldable tree — siblings on a row,
	// parents centred over children. A chain has no siblings and no width:
	// every generation is one node, and "layered" degenerates into a column
	// beside the event that costs a row per link and pushes the whole story
	// down (two-containers: t1→t1a→t1b→t1c stood 400px tall next to e2a1 and
	// moved e2a2 from y=300 to y=730; the old row read it as what it is, a
	// chain running outward). v7P4's band grammar already says which way each
	// kind of chain reads: "the whole of an outgoing tPt goes OUTWARD on the
	// row", while "concepts step DOWN-AND-OUTWARD, so a concept chain reads
	// as one diagonal line" — and the aux-chain-gets-its-own-corridor fixture
	// pins the concept case (c6 below c5, same centre-x). So a subtree that
	// is a straight run of part-of, with no member placing two children,
	// keeps the band grammar and lays out sideways; a concept chain, or any
	// tree that branches (a flat fan of two parts is one generation, one
	// row), still renders as generations. Either way it is still one unit
	// for the zoom canvas: the band grammar places the chain as a group.
	// "Linear" is a single path: within the closure every member has at
	// most one placing edge in and one out. Out-degree alone is not enough —
	// a part-of tree CONVERGES (tB --P--> tA, tC --P--> tA: two parts of one
	// whole, the fixture a-thing-hierarchy-and-its-concepts-join-the-event-
	// chain), and that is a tree with two nodes on one generation, which
	// counting children of the whole would miss.
	linearPartOf := true
	for _, n := range append([]int{root}, members...) {
		in, out := 0, 0
		for _, e := range g.out[n] {
			if !e.structural || !g.isPlacing(e) || !inClo[e.to] || e.to == n {
				continue
			}
			out++
			if e.rel != RelPartOf {
				linearPartOf = false // a concept link: generations, as pinned
			}
		}
		for _, e := range g.in[n] {
			if !e.structural || !g.isPlacing(e) || !inClo[e.from] || e.from == n {
				continue
			}
			in++
		}
		if in >= 2 || out >= 2 {
			linearPartOf = false // it branches or converges: a real tree
		}
	}
	if linearPartOf {
		return nil
	}
	return members
}

// flowNear: the two events are the same or one leads-to step apart —
// the range within which the pull-slide can align a shared concept
// with both its users (v7P4/P7).
func (g *graph) flowNear(a, b int) bool {
	if a == b {
		return true
	}
	for _, e := range g.out[a] {
		if e.rel == RelLeadsTo && e.to == b {
			return true
		}
	}
	for _, e := range g.out[b] {
		if e.rel == RelLeadsTo && e.to == a {
			return true
		}
	}
	return false
}
