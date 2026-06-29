package layout7

import (
	"math"
	"sort"
)

// skeleton implements v7P3 — the event skeleton: leads-to runs down, part-of
// indents right, forks spread symmetrically — and computes the fork pitch
// orbits of v7P6 (the fan grows to host aux beside its anchor, all gaps of
// an orbit alike).

type skeletonPlan struct {
	// per component (indexed like g.comps):
	rows      [][][]int     // comp -> rank -> events (top-level, lane order)
	rank      map[int]int   // top-level event -> rank
	subParent map[int]int   // sub-event -> composite event (ePe)
	subStacks map[int][]int // composite -> ordered sub-events (v7P3 chain+ID order)
	// composite -> rank rows over the siblings' own leads-to (v7P3
	// recursively: the sub-structure IS a skeleton — a chain is one
	// column, a fork spreads its branches on one ROW, a join rejoins
	// below). Row membership follows subStacks order.
	subRows   map[int][][]int
	desiredX  map[int]int   // event -> desired lane CENTER (component-local)
	extraDrop map[int]int   // fork parent -> extra row drop below it (fan-angle cap)
	starts    map[int][]int // comp -> start events (rank 0, declaration order)
	ends      map[int][]int // comp -> end events (no outgoing eLe)
}

// eLe successors/predecessors among TOP-LEVEL events only.
func (g *graph) topLevelFlow() (succ, pred map[int][]int, topLevel map[int]bool, subParent map[int]int) {
	succ, pred = map[int][]int{}, map[int][]int{}
	topLevel = map[int]bool{}
	subParent = map[int]int{}
	for i, n := range g.nodes {
		if n.kind == KindEvent && !n.boundary {
			topLevel[i] = true
		}
	}
	for _, e := range g.edges {
		f, t := g.nodes[e.from], g.nodes[e.to]
		if e.rel == RelPartOf && f.kind == KindEvent && t.kind == KindEvent {
			topLevel[e.from] = false // a sub-event nests beside its composite
			subParent[e.from] = e.to
		}
	}
	for _, e := range g.edges {
		if e.rel != RelLeadsTo {
			continue
		}
		if topLevel[e.from] && topLevel[e.to] {
			succ[e.from] = append(succ[e.from], e.to)
			pred[e.to] = append(pred[e.to], e.from)
		}
	}
	return
}

// buildSkeleton derives ranks, lane order (join-affinity clustering), the
// symmetric fork pitches, and sub-event stacks. Lane centers are DESIRED
// positions — the place stage resolves each row as one separation problem
// (v7P8).
func (g *graph) buildSkeleton(gp *groupsPlan) *skeletonPlan {
	sp := &skeletonPlan{
		rows:      make([][][]int, len(g.comps)),
		rank:      map[int]int{},
		subParent: map[int]int{},
		subStacks: map[int][]int{},
		subRows:   map[int][][]int{},
		desiredX:  map[int]int{},
		extraDrop: map[int]int{},
		starts:    map[int][]int{},
		ends:      map[int][]int{},
	}
	succ, pred, topLevel, subParent := g.topLevelFlow()
	sp.subParent = subParent

	// ---- sub-event stacks (v7P3: "a declared leads-to between sub-event
	// siblings ORDERS that column and keeps the pair adjacent in flow
	// order — the rest follow in [node ID] order"). ----
	subsOf := map[int][]int{}
	for sub, parent := range subParent {
		subsOf[parent] = append(subsOf[parent], sub)
	}
	for parent, subs := range subsOf {
		sort.Ints(subs)
		set := map[int]bool{}
		for _, s := range subs {
			set[s] = true
		}
		// chains: eLe among the siblings
		next, hasPrev := map[int]int{}, map[int]bool{}
		for _, e := range g.edges {
			if e.rel == RelLeadsTo && set[e.from] && set[e.to] {
				if _, dup := next[e.from]; !dup {
					next[e.from] = e.to
					hasPrev[e.to] = true
				}
			}
		}
		var units [][]int
		seen := map[int]bool{}
		for _, s := range subs {
			if hasPrev[s] || seen[s] {
				continue
			}
			unit := []int{s}
			seen[s] = true
			for cur := s; ; {
				n, ok := next[cur]
				if !ok || seen[n] {
					break
				}
				unit = append(unit, n)
				seen[n] = true
				cur = n
			}
			units = append(units, unit)
		}
		// unit order: smallest member node ID first
		sort.Slice(units, func(a, b int) bool {
			minA, minB := units[a][0], units[b][0]
			for _, x := range units[a] {
				if x < minA {
					minA = x
				}
			}
			for _, x := range units[b] {
				if x < minB {
					minB = x
				}
			}
			return minA < minB
		})
		var ordered []int
		for _, u := range units {
			ordered = append(ordered, u...)
		}
		sp.subStacks[parent] = ordered

		// Rank rows PER FLOW UNIT (connected component over the siblings'
		// own leads-to): each unit is a mini-skeleton — rank rows, a fork's
		// branches side by side — and the units stack VERTICALLY in their
		// chain+ID order, so loose siblings keep the plain column.
		inSubs := map[int]bool{}
		for _, s := range ordered {
			inSubs[s] = true
		}
		up := map[int]int{}
		for _, s := range ordered {
			up[s] = s
		}
		var findU func(int) int
		findU = func(a int) int {
			if up[a] != a {
				up[a] = findU(up[a])
			}
			return up[a]
		}
		for _, s := range ordered {
			for _, e := range g.out[s] {
				if e.rel == RelLeadsTo && inSubs[e.to] {
					up[findU(s)] = findU(e.to)
				}
			}
		}
		srank := map[int]int{}
		var srankOf func(int, map[int]bool) int
		srankOf = func(s int, seen map[int]bool) int {
			if r, ok := srank[s]; ok {
				return r
			}
			if seen[s] {
				return 0
			}
			seen[s] = true
			r := 0
			for _, e := range g.in[s] {
				if e.rel == RelLeadsTo && inSubs[e.from] {
					if pr := srankOf(e.from, seen) + 1; pr > r {
						r = pr
					}
				}
			}
			delete(seen, s)
			srank[s] = r
			return r
		}
		var unitRoots []int
		unitOf := map[int][]int{}
		for _, s := range ordered { // first-appearance unit order
			r := findU(s)
			if len(unitOf[r]) == 0 {
				unitRoots = append(unitRoots, r)
			}
			unitOf[r] = append(unitOf[r], s)
		}
		var rows [][]int
		for _, root := range unitRoots {
			members := unitOf[root]
			maxR := 0
			for _, s := range members {
				if r := srankOf(s, map[int]bool{}); r > maxR {
					maxR = r
				}
			}
			urows := make([][]int, maxR+1)
			for _, s := range members {
				urows[srank[s]] = append(urows[srank[s]], s)
			}
			rows = append(rows, urows...)
		}
		sp.subRows[parent] = rows
		if g.rowMates == nil {
			g.rowMates = map[int]map[int]bool{}
		}
		for _, row := range rows {
			for _, s := range row {
				for _, o := range row {
					if o == s {
						continue
					}
					if g.rowMates[s] == nil {
						g.rowMates[s] = map[int]bool{}
					}
					g.rowMates[s][o] = true
				}
			}
		}
	}

	// ---- ranks per component: longest path over eLe (v7P3: time reads
	// top-to-bottom). Cycles are semantically invalid upstream; a back
	// edge is skipped rather than looping. ----
	state := map[int]int{} // 0 unvisited, 1 visiting, 2 done
	var rankOf func(int) int
	rankOf = func(n int) int {
		if r, ok := sp.rank[n]; ok && state[n] == 2 {
			return r
		}
		if state[n] == 1 {
			return 0
		}
		state[n] = 1
		r := 0
		for _, p := range pred[n] {
			if pr := rankOf(p) + 1; pr > r {
				r = pr
			}
		}
		state[n] = 2
		sp.rank[n] = r
		return r
	}
	for i := range g.nodes {
		if topLevel[i] {
			rankOf(i)
		}
	}

	for _, comp := range g.comps {
		maxRank := 0
		for _, ev := range comp.events {
			if topLevel[ev] && sp.rank[ev] > maxRank {
				maxRank = sp.rank[ev]
			}
		}
		rows := make([][]int, maxRank+1)
		for _, ev := range comp.events { // declaration order
			if !topLevel[ev] {
				continue
			}
			rows[sp.rank[ev]] = append(rows[sp.rank[ev]], ev)
			if len(pred[ev]) == 0 {
				sp.starts[comp.idx] = append(sp.starts[comp.idx], ev)
			}
			if len(succ[ev]) == 0 {
				sp.ends[comp.idx] = append(sp.ends[comp.idx], ev)
			}
		}
		sp.rows[comp.idx] = rows
	}

	// ---- lane centers (v7P3 forks + v7P6 pitch orbits). ----
	// subtree span: [left,right] relative to the event's center, including
	// its own aux extents and its fork descendants. A join is measured in
	// the first parent's subtree only (its lane is re-centred under all
	// predecessors afterwards).
	fullLeftExt := func(ev int) int { return gp.leftExt[ev] + g.subTreeLeftPad(ev) }
	fullRightExt := func(ev int) int {
		return gp.rightExt[ev] + g.subColumnWidth(ev, sp, gp)
	}

	spanSeen := map[int]bool{}
	var span func(ev int) (int, int)
	span = func(ev int) (int, int) {
		l := -g.nodes[ev].w/2 - fullLeftExt(ev)
		r := g.nodes[ev].w/2 + fullRightExt(ev)
		kids := g.orderedChildren(ev, succ, spanSeen)
		// a JOIN belongs to no single branch — it re-centres under ALL its
		// predecessors (v7P3), so it must not widen any one subtree's span.
		filtered := kids[:0:0]
		for _, c := range kids {
			if len(pred[c]) <= 1 {
				filtered = append(filtered, c)
			}
		}
		kids = filtered
		if len(kids) > 0 {
			offs := g.forkOffsets(ev, kids, span)
			for i, c := range kids {
				cl, cr := g.spanCache[c][0], g.spanCache[c][1]
				if offs[i]+cl < l {
					l = offs[i] + cl
				}
				if offs[i]+cr > r {
					r = offs[i] + cr
				}
			}
		}
		g.spanCache[ev] = [2]int{l, r}
		return l, r
	}
	_ = span

	// desired centers, processed per component top-down
	for _, comp := range g.comps {
		rows := sp.rows[comp.idx]
		if len(rows) == 0 {
			continue
		}
		g.spanCache = map[int][2]int{}
		spanSeen = map[int]bool{}
		claimed := map[int]bool{}
		// place rank-0 starts side by side (a virtual root fork): each
		// gap is PER-PAIR — previous subtree's right reach + one column
		// gap + next subtree's left reach (adding the gap on both sides
		// doubled it; side aux must widen only its own flank, v7P8)
		bg := breatheGap(len(rows[0]))
		x, prevR := 0, 0
		for i, s := range rows[0] {
			sl, sr := span(s)
			if i > 0 {
				x += gridUp(prevR + bg - sl)
			}
			sp.desiredX[s] = x
			prevR = sr
			claimed[s] = true
		}
		for r := 0; r < len(rows); r++ {
			for _, ev := range rows[r] {
				kids := g.orderedChildren(ev, succ, nil)
				if len(kids) == 0 {
					continue
				}
				// only assign kids one rank down and not yet claimed by an
				// earlier parent; a join re-centres below.
				var mine []int
				for _, c := range kids {
					if sp.rank[c] == sp.rank[ev]+1 && !claimed[c] && len(pred[c]) == 1 {
						mine = append(mine, c)
					}
				}
				if len(mine) > 0 {
					offs := g.forkOffsets(ev, mine, span)
					half := 0
					for i, c := range mine {
						sp.desiredX[c] = sp.desiredX[ev] + offs[i]
						claimed[c] = true
						if o := offs[i]; o > half {
							half = o
						} else if -o > half {
							half = -o
						}
					}
					// fan-angle cap (v7P3/P8): a wide fan drops its row —
					// the outermost edge's dy (the row GAP) must keep the
					// fan angle within 150°: dy >= halfSpan / tan(75°).
					if need := int(math.Ceil(float64(half) / math.Tan(75.0/180.0*math.Pi))); need > RowGap {
						sp.extraDrop[ev] = gridUp(need - RowGap)
					}
				}
			}
			// joins and stragglers of the NEXT row: centre under solved preds
			if r+1 < len(rows) {
				for _, ev := range rows[r+1] {
					if claimed[ev] {
						continue
					}
					ps := pred[ev]
					if len(ps) == 0 {
						// disconnected extra start at a lower rank
						sp.desiredX[ev] = 0
						claimed[ev] = true
						continue
					}
					// v7P3: a join centres the way a FORK centres — under
					// the MIDDLE predecessor lane (odd), the middle pair's
					// midpoint (even) — never the mean: per-pair fan growth
					// skews the mean off the corridor axis and
					// kinked the S→s→m→j→E spine
					xs := make([]int, 0, len(ps))
					for _, p := range ps {
						xs = append(xs, sp.desiredX[p])
					}
					sort.Ints(xs)
					k := len(xs)
					mid := xs[(k-1)/2]
					if k%2 == 0 {
						mid = (xs[k/2-1] + xs[k/2]) / 2
					}
					sp.desiredX[ev] = mid
					claimed[ev] = true
				}
			}
		}
	}
	return sp
}

// orderedChildren returns an event's eLe successors in v7P3 fork order:
// declaration order, EXCEPT branches that later JOIN into one event cluster
// adjacent (a cluster sorts by its first-declared member).
func (g *graph) orderedChildren(ev int, succ map[int][]int, _ map[int]bool) []int {
	kids := succ[ev]
	if len(kids) < 3 {
		return kids
	}
	posOf := map[int]int{}
	for i, k := range kids {
		posOf[k] = i
	}
	// cluster by shared direct join target
	parent := map[int]int{}
	var find func(int) int
	find = func(a int) int {
		if parent[a] == a {
			return a
		}
		parent[a] = find(parent[a])
		return parent[a]
	}
	for _, k := range kids {
		parent[k] = k
	}
	joint := map[int][]int{}
	for _, k := range kids {
		for _, e := range g.out[k] {
			if e.rel == RelLeadsTo {
				joint[e.to] = append(joint[e.to], k)
			}
		}
	}
	for _, group := range joint {
		for i := 1; i < len(group); i++ {
			parent[find(group[0])] = find(group[i])
		}
	}
	clusters := map[int][]int{}
	for _, k := range kids {
		clusters[find(k)] = append(clusters[find(k)], k)
	}
	type cl struct {
		first   int
		members []int
	}
	var cls []cl
	for _, ms := range clusters {
		sort.Slice(ms, func(a, b int) bool { return posOf[ms[a]] < posOf[ms[b]] })
		cls = append(cls, cl{posOf[ms[0]], ms})
	}
	sort.Slice(cls, func(a, b int) bool { return cls[a].first < cls[b].first })
	var out []int
	for _, c := range cls {
		out = append(out, c.members...)
	}
	return out
}

// forkPitch computes one fork's orbit pitch (v7P6): the ONE shared gap all
// the fan's gaps use, grown closed-form so every child subtree — aux
// included — fits beside its anchor. Minimum is the standard column pitch.
// forkOffsets returns each kid's centre offset from the PARENT's axis.
// Gaps are PER-PAIR (v7P8 revised): each gap fits the
// two subtrees it separates — a chain corridor on one branch no longer
// mirrors onto the far flank. The CORRIDOR child (the middle one) stays
// on the parent's axis (v7P6: the flow corridor never yields); the rest
// spread from it by their own needs.
func (g *graph) forkOffsets(ev int, kids []int, span func(int) (int, int)) []int {
	k := len(kids)
	spans := make([][2]int, k)
	for i, c := range kids {
		if s, ok := g.spanCache[c]; ok {
			spans[i] = s
		} else {
			l, r := span(c)
			spans[i] = [2]int{l, r}
		}
	}
	bg := breatheGap(k)
	pos := make([]int, k)
	for i := 1; i < k; i++ {
		need := spans[i-1][1] + bg - spans[i][0]
		if need < NodeW+bg {
			need = NodeW + bg
		}
		pos[i] = pos[i-1] + gridUp(need)
	}
	// centre: the middle child keeps the axis (odd k); an even fork
	// centres on its two middle children's midpoint
	mid := pos[(k-1)/2]
	if k%2 == 0 {
		mid = (pos[k/2-1] + pos[k/2]) / 2
	}
	for i := range pos {
		pos[i] -= mid
	}
	return pos
}

// subColumnWidth: horizontal room the composite's sub-event GRID claims to
// its right (v7P3: part-of indents right), nested recursively — the widest
// rank row decides (a fork row is as wide as its branches side by side).
func (g *graph) subColumnWidth(ev int, sp *skeletonPlan, gp *groupsPlan) int {
	rows := sp.subRows[ev]
	if len(rows) == 0 {
		return 0
	}
	w := 0
	for _, row := range rows {
		rw := 0
		for i, s := range row {
			if i > 0 {
				rw += ColGap + gp.leftExt[s]
			}
			rw += g.nodes[s].w + gp.rightExt[s] + g.subColumnWidth(s, sp, gp)
		}
		if rw > w {
			w = rw
		}
	}
	return ColGap + w
}

// subTreeLeftPad: room a composite's sub-events claim to the LEFT (their own
// aux bands). Conservative: the widest sub left extent.
func (g *graph) subTreeLeftPad(int) int { return 0 }

func gridUp(v int) int {
	if v%GridStep == 0 {
		return v
	}
	return v + GridStep - v%GridStep
}

// sideHints assigns each fork child its flank (v7P4: the grammar is stated
// for the spine's right side and mirrored on the left — a branch's aux takes
// the branch's OUTER side). -1 = left flank, +1 = right flank, absent/0 =
// on-spine or middle branch (balanced split). Structural only — computed
// before any coordinate exists.
func (g *graph) sideHints() map[int]int {
	hints := map[int]int{}
	succ, pred, _, subParent := g.topLevelFlow()
	// sub-events nest RIGHT of their composite (v7P3), so their aux takes
	// the right flank — the outer side of the sub-event column.
	for sub := range subParent {
		hints[sub] = +1
	}
	for ev := range g.nodes {
		kids := succ[ev]
		if len(kids) < 2 {
			continue
		}
		ordered := g.orderedChildren(ev, succ, nil)
		k := len(ordered)
		for i, c := range ordered {
			switch {
			case 2*i < k-1:
				hints[c] = -1
			case 2*i > k-1:
				hints[c] = +1
			}
		}
	}
	// several rank-0 starts fan out from S exactly like a fork's children
	// (v7P3: S caps the timeline), so they take the same outer flanks.
	startsByComp := map[int][]int{}
	for i, n := range g.nodes {
		if n.kind != KindEvent || n.boundary {
			continue
		}
		if _, isSub := subParent[i]; isSub {
			continue
		}
		if len(pred[i]) == 0 {
			startsByComp[n.comp] = append(startsByComp[n.comp], i)
		}
	}
	for _, starts := range startsByComp {
		k := len(starts)
		if k < 2 {
			continue
		}
		for i, c := range starts {
			switch {
			case 2*i < k-1:
				hints[c] = -1
			case 2*i > k-1:
				hints[c] = +1
			}
		}
	}
	// a branch's flank carries down its lane: single-predecessor chain
	// events inherit their predecessor's side (v7P4's mirror is about the
	// SPINE, and the whole lane sits off it). Joins re-centre and reset.
	for changed := true; changed; {
		changed = false
		for ev := range g.nodes {
			if _, ok := hints[ev]; ok {
				continue
			}
			ps := pred[ev]
			if len(ps) == 1 {
				if h, ok := hints[ps[0]]; ok && h != 0 {
					hints[ev] = h
					changed = true
				}
			}
		}
	}
	// a COMPOSITE's sub-event column owns its right side (v7P3: part-of
	// indents right), so the composite's own aux mirrors to the LEFT flank
	// (v7P4). A composite that is ITSELF a sub-event also has the parent's
	// ePe corridor on its left ROW (v7P6: reserved) — its band takes the
	// DOWN-AND-OUTWARD step of v7P4's concept grammar instead (hint -2).
	for _, parent := range subParent {
		hints[parent] = -1
		if _, isSub := subParent[parent]; isSub {
			hints[parent] = -2
		}
	}
	return hints
}
