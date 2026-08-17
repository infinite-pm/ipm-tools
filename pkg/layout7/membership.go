package layout7

import "sort"

// membership implements v7P1 (components separate along event structure) and
// v7P7 (shared nodes anchor at their deepest user), plus the tie
// classification feeding v7P5.
//
// Products, in order:
//  1. event components — two events share a component iff connected through
//     leads-to or part-of (v7P1; eXe/eNe never merge);
//  2. per-aux primary anchors — every thing/concept keeps exactly ONE
//     placing edge as its primary; the rest DEMOTE to drawn ties
//     (anchor-and-tie, v7P1/P7);
//  3. aux structures — connectivity over the remaining primary edges; each
//     structure elects its anchor MEMBER (part-most member with an event
//     connector, v7P4/P7) and attaches to that member's event;
//  4. onion satellites — tie-only nodes join their partner's component as
//     the outermost layer (v7P5);
//  5. cross-component tie counts for v7P2 centrality.

// anchorInfo records where an aux node hangs.
type anchorInfo struct {
	primary *edge // the one placing edge that positions this node (nil for events/satellites)
	// satellite support (v7P5): the tie that places a tie-only node.
	satelliteOf *edge
}

type membership struct {
	anchors []anchorInfo // by node idx

	// structures: per aux structure, the member list and elected anchor.
	structOf     []int   // node idx -> structure id (-1 for events / satellites)
	structAnchor []int   // structure id -> anchor member node idx
	structRoots  [][]int // structure id -> members, declaration order
}

// eventNesting computes each event's ePe hierarchy depth — v7P7's
// "part-most" reading for events themselves (md is 0 for every event; a
// sub-sub-event still outranks a top-level one as a user). Non-events stay 0.
func (g *graph) eventNesting() []int {
	nest := make([]int, len(g.nodes))
	for iter := 0; iter < len(g.nodes); iter++ {
		changed := false
		for _, e := range g.edges {
			if e.rel != RelPartOf ||
				g.nodes[e.from].kind != KindEvent || g.nodes[e.to].kind != KindEvent {
				continue
			}
			if d := nest[e.to] + 1; d > nest[e.from] {
				nest[e.from] = d
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return nest
}

// eventDistance computes md(n): the minimal number of placing edges between
// an event and n (undirected walk over placing edges). This is the "depth"
// v7P7 compares users by — an event is 0, a thing directly on an event 1, a
// part of that thing 2, and so on.
func (g *graph) eventDistance() []int {
	const inf = 1 << 30
	dist := make([]int, len(g.nodes))
	queue := []int{}
	for i, n := range g.nodes {
		if n.kind == KindEvent {
			dist[i] = 0
			queue = append(queue, i)
		} else {
			dist[i] = inf
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		visit := func(e *edge, other int) {
			if !g.isPlacing(e) {
				return
			}
			// eXe reaches an event on both sides and is non-placing by
			// isPlacing already; eLe/ePe stay inside the skeleton.
			if dist[other] > dist[cur]+1 {
				dist[other] = dist[cur] + 1
				queue = append(queue, other)
			}
		}
		for _, e := range g.out[cur] {
			visit(e, e.to)
		}
		for _, e := range g.in[cur] {
			visit(e, e.from)
		}
	}
	return dist
}

// userEdges returns the placing edges that could PLACE n, each with its
// "user" partner:
//   - n --P--> u : u is n's whole (or event) — n stacks above u / sits on
//     u's row (v7P4);
//   - u --X--> n (n a concept): u expresses n — n steps down-and-outward
//     from u (v7P4).
//
// An edge u --P--> n (n is the whole) places n OUTWARD of u only through
// group orientation once u is anchored — it is u's edge, not n's, so it is
// not returned here.
func (g *graph) userEdges(n int) []*edge {
	var out []*edge
	for _, e := range g.out[n] {
		if e.rel == RelPartOf {
			out = append(out, e)
		}
	}
	if g.nodes[n].kind == KindConcept {
		for _, e := range g.in[n] {
			if e.rel == RelExpresses {
				out = append(out, e)
			}
		}
	}
	return out
}

func (g *graph) userOf(n int, e *edge) int {
	if e.from == n {
		return e.to
	}
	return e.from
}

// resolveMembership runs the whole v7P1/P7 pipeline described at the top of
// the file.
func (g *graph) resolveMembership() *membership {
	m := &membership{
		anchors:  make([]anchorInfo, len(g.nodes)),
		structOf: make([]int, len(g.nodes)),
	}
	for i := range m.structOf {
		m.structOf[i] = -1
	}

	// ---- 1. event components (v7P1): union over eLe + ePe. ----
	parent := make([]int, len(g.nodes))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(a int) int {
		for parent[a] != a {
			parent[a] = parent[parent[a]]
			a = parent[a]
		}
		return a
	}
	union := func(a, b int) { parent[find(a)] = find(b) }
	for _, e := range g.edges {
		f, t := g.nodes[e.from], g.nodes[e.to]
		if f.kind == KindEvent && t.kind == KindEvent &&
			(e.rel == RelLeadsTo || e.rel == RelPartOf) {
			union(e.from, e.to)
		}
	}

	md := g.eventDistance()
	nest := g.eventNesting()
	// deeper: v7P7's user comparison — md first (a thing user outranks a
	// direct event user), event NESTING breaks the tie (every event has
	// md 0, but a sub-sub-event is the part-most user of its things).
	deeper := func(a, b int) bool {
		return md[a] > md[b] || (md[a] == md[b] && nest[a] > nest[b])
	}
	// upstream reports whether event a reaches event b along leads-to: a is
	// EARLIER in the flow. Among users of equal depth this breaks the tie
	// before declaration order does — v7P3, time reads down: a shared node
	// anchored at the LAST of the events it spans reads as arriving at the
	// end of the story; anchored at the first it reads as present throughout,
	// which is what "part of e1, e2 and e3" says. `tP --> e3, e2, e1` used to
	// anchor at e3 purely because e3 was declared first (layout-alg's "fan
	// from its facing side too" case, written to test the ports going UP);
	// it anchors at e1 now, like its mirror `tP --> e1, e2, e3`. Declaration
	// order still decides between users that flow does NOT order (parallel
	// branches, separate chains).
	upstream := func(a, b int) bool {
		if a == b || g.nodes[a].kind != KindEvent || g.nodes[b].kind != KindEvent {
			return false
		}
		seen := map[int]bool{a: true}
		queue := []int{a}
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			for _, e := range g.out[n] {
				if e.rel != RelLeadsTo || seen[e.to] {
					continue
				}
				if e.to == b {
					return true
				}
				seen[e.to] = true
				queue = append(queue, e.to)
			}
		}
		return false
	}
	// better says whether user a should beat the current best user b: deeper
	// wins; at equal depth the flow-upstream one wins; else the incumbent
	// (declaration order) stands.
	better := func(a, b int) bool {
		if deeper(a, b) {
			return true
		}
		if deeper(b, a) {
			return false
		}
		return upstream(a, b)
	}

	// ---- 2. per-node primary anchors (v7P7). ----
	// "The DEEPEST / part-most user wins — one rule for things and concepts
	// alike … Declaration order only breaks depth ties. The remaining
	// placing relations become ties."
	for i, n := range g.nodes {
		if n.kind == KindEvent {
			continue
		}
		users := g.userEdges(i)
		if len(users) == 0 {
			continue // a pure whole, oriented by its parts' side (groups.go)
		}
		if n.kind == KindThing {
			// A thing that DIRECTLY participates in an event keeps all its
			// tPe connectors here: v7P4's group-anchor election (step 3)
			// decides between direct participation and deeper wholes — "the
			// most elementary thing that directly participates in an event"
			// (the p-over-w case of the spec's example 160).
			hasConnector := false
			for _, e := range users {
				if g.nodes[g.userOf(i, e)].kind == KindEvent {
					hasConnector = true
					break
				}
			}
			if hasConnector {
				// Primary among connectors: the DEEPEST user, exactly as for
				// every shared node (v7P7) — declaration order only breaks
				// depth ties (v7P1's shared thing example: C --> e2, e4 are
				// equally deep, so C anchors at e2). Whole-edges are NOT
				// demoted here: the thing is placed by its connector, and
				// each whole-edge stays structural to place the WHOLE
				// outward on the row (v7P4 — the spec's `e2 | p | w` shape).
				var connectors []*edge
				for _, e := range users {
					if g.nodes[g.userOf(i, e)].kind == KindEvent {
						connectors = append(connectors, e)
					}
				}
				// Further connectors DEMOTE to drawn ties — a shared thing
				// is placed by its band like any other; it never hovers
				// centred between its anchors and never moves the skeleton
				// (leads-to has priority over part-of;
				// replaces the earlier span exception).
				best := connectors[0]
				for _, e := range connectors[1:] {
					if better(g.userOf(i, e), g.userOf(i, best)) {
						best = e
					}
				}
				m.anchors[i].primary = best
				best.structural = true
				if g.tracing() {
					g.emitElection(i, best, connectors, better)
				}
				for _, e := range connectors {
					if e != best {
						e.demotedTie = true
					}
				}
				continue
			}
		}
		// Deepest user wins; at equal depth the flow-upstream user; then
		// declaration (edge order).
		best := users[0]
		for _, e := range users[1:] {
			if better(g.userOf(i, e), g.userOf(i, best)) {
				best = e
			}
		}
		m.anchors[i].primary = best
		best.structural = true
		if g.tracing() {
			g.emitElection(i, best, users, better)
		}
		for _, e := range users {
			if e != best {
				e.demotedTie = true
			}
		}
	}

	// Whole-edges (u --P--> n where n is not an event) become structural
	// from the USER side when u kept them: they orient n outward (v7P4).
	// They were already demoted above when u chose a different primary or
	// direct participation; every remaining placing edge not yet classified
	// is structural.
	for _, e := range g.edges {
		if g.isPlacing(e) && !e.structural && !e.demotedTie {
			f, t := g.nodes[e.from], g.nodes[e.to]
			if f.kind == KindEvent && t.kind == KindEvent {
				e.structural = true // skeleton edges (eLe/ePe), v7P3's business
				continue
			}
			e.structural = true // e.g. p --P--> w placing the whole outward
		}
	}

	// ---- 3. aux structures over primary edges + anchor election. ----
	sparent := make([]int, len(g.nodes))
	for i := range sparent {
		sparent[i] = i
	}
	var sfind func(int) int
	sfind = func(a int) int {
		for sparent[a] != a {
			sparent[a] = sparent[sparent[a]]
			a = sparent[a]
		}
		return a
	}
	for _, e := range g.edges {
		if !e.structural {
			continue
		}
		f, t := g.nodes[e.from], g.nodes[e.to]
		if f.kind == KindEvent || t.kind == KindEvent {
			continue // connectors attach structures; they don't merge aux with events here
		}
		sparent[sfind(e.from)] = sfind(e.to)
	}
	structIDs := map[int]int{}
	for i, n := range g.nodes {
		if n.kind == KindEvent {
			continue
		}
		root := sfind(i)
		sid, ok := structIDs[root]
		if !ok {
			sid = len(m.structRoots)
			structIDs[root] = sid
			m.structRoots = append(m.structRoots, nil)
			m.structAnchor = append(m.structAnchor, -1)
		}
		m.structOf[i] = sid
		m.structRoots[sid] = append(m.structRoots[sid], i)
	}

	// partRank: longest part-of chain upward INSIDE the structure — the
	// "part-most" order of v7P4's anchor election (p rank 1 beats w rank 0).
	partRank := make([]int, len(g.nodes))
	var rankOf func(int, map[int]bool) int
	rankOf = func(n int, seen map[int]bool) int {
		if seen[n] {
			return 0 // guarded: containment cycles are rejected upstream
		}
		seen[n] = true
		best := 0
		for _, e := range g.out[n] {
			if e.rel != RelPartOf || !e.structural {
				continue
			}
			if g.nodes[e.to].kind == KindEvent {
				continue
			}
			if r := 1 + rankOf(e.to, seen); r > best {
				best = r
			}
		}
		delete(seen, n)
		return best
	}
	for i, n := range g.nodes {
		if n.kind != KindEvent {
			partRank[i] = rankOf(i, map[int]bool{})
		}
	}

	for sid, members := range m.structRoots {
		bestMember, bestRank := -1, -1
		for _, i := range members {
			hasConn := false
			if p := m.anchors[i].primary; p != nil && g.nodes[g.userOf(i, p)].kind == KindEvent {
				hasConn = true
			}
			if !hasConn {
				continue
			}
			if partRank[i] > bestRank {
				bestMember, bestRank = i, partRank[i]
			}
		}
		m.structAnchor[sid] = bestMember
		if bestMember < 0 {
			continue // tie-only structure (satellites) or pure aux component
		}
		// Non-anchor members' event connectors demote (v7P4 anchor-and-tie
		// at group scale — the spec's w → e1 plain edge).
		for _, i := range members {
			if i == bestMember {
				continue
			}
			p := m.anchors[i].primary
			if p != nil && g.nodes[g.userOf(i, p)].kind == KindEvent {
				p.structural = false
				p.demotedTie = true
				m.anchors[i].primary = nil
				// The member now hangs off the structure's internal shape:
				// re-anchor to its deepest non-event user if it has one.
				users := g.userEdges(i)
				var best *edge
				for _, e := range users {
					if g.nodes[g.userOf(i, e)].kind == KindEvent || e.demotedTie {
						continue
					}
					if best == nil || md[g.userOf(i, e)] > md[g.userOf(i, best)] {
						best = e
					}
				}
				if best != nil {
					m.anchors[i].primary = best
					best.structural = true
				}
			}
		}
	}

	// ---- 4. same-kind ties (v7P5) + non-placing edges (v7P1). ----
	for _, e := range g.edges {
		f, t := g.nodes[e.from], g.nodes[e.to]
		switch {
		case e.rel == RelNearTo && f.kind == t.kind && f.kind != KindEvent:
			e.sameTie = true // tNt / cNc — draw, order, or wrap (v7P5)
		case e.rel == RelNearTo:
			e.nonPlacing = true // eNe and mixed near-to: places nothing (v7P1)
		case e.rel == RelExpresses && t.kind == KindEvent:
			e.nonPlacing = true // eXe: valid, draws, places nothing (v7P1)
		}
	}

	// Satellites: a node whose ONLY connections are ties is placed BY its
	// first tie, joining the partner's component as the outermost onion
	// layer (v7P5).
	hasPlacement := func(i int) bool {
		return g.nodes[i].kind == KindEvent || m.anchors[i].primary != nil ||
			func() bool { // member of an anchored structure (e.g. a whole)
				sid := m.structOf[i]
				return sid >= 0 && m.structAnchor[sid] >= 0
			}()
	}
	for i, n := range g.nodes {
		if n.kind == KindEvent || hasPlacement(i) {
			continue
		}
		var ties []*edge
		for _, e := range append(append([]*edge{}, g.out[i]...), g.in[i]...) {
			if e.sameTie {
				ties = append(ties, e)
			}
		}
		sort.Slice(ties, func(a, b int) bool { return ties[a].idx < ties[b].idx })
		for _, e := range ties {
			other := e.from
			if other == i {
				other = e.to
			}
			if hasPlacement(other) {
				m.anchors[i].satelliteOf = e
				break
			}
		}
	}

	// ---- 5. components: events by union-find; aux via anchor chains. ----
	compIDs := map[int]int{}
	compOf := func(root int) *component {
		id, ok := compIDs[root]
		if !ok {
			id = len(g.comps)
			compIDs[root] = id
			g.comps = append(g.comps, &component{idx: id, sNode: -1, eNode: -1})
		}
		return g.comps[id]
	}
	for i, n := range g.nodes {
		if n.kind != KindEvent {
			continue
		}
		c := compOf(find(i))
		n.comp = c.idx
		c.events = append(c.events, i)
	}
	// aux: follow primary anchors / structure anchors / satellite partners
	// to an event; unresolved chains form pure aux components (v7P1: "a
	// pure thing/concept structure … forms its own component").
	var compFor func(i int, seen map[int]bool) int
	compFor = func(i int, seen map[int]bool) int {
		n := g.nodes[i]
		if n.comp >= 0 {
			return n.comp
		}
		if seen[i] {
			return -1
		}
		seen[i] = true
		if p := m.anchors[i].primary; p != nil {
			return compFor(g.userOf(i, p), seen)
		}
		if sid := m.structOf[i]; sid >= 0 && m.structAnchor[sid] >= 0 && m.structAnchor[sid] != i {
			return compFor(m.structAnchor[sid], seen)
		}
		if te := m.anchors[i].satelliteOf; te != nil {
			other := te.from
			if other == i {
				other = te.to
			}
			return compFor(other, seen)
		}
		// oriented as someone's whole: follow any structural part edge
		for _, e := range g.in[i] {
			if e.structural && e.rel == RelPartOf {
				return compFor(e.from, seen)
			}
		}
		return -1
	}
	for i, n := range g.nodes {
		if n.kind == KindEvent || n.comp >= 0 {
			continue
		}
		if c := compFor(i, map[int]bool{}); c >= 0 {
			n.comp = c
			g.comps[c].aux = append(g.comps[c].aux, i)
		}
	}
	// leftovers: pure thing/concept components form by PLACING connectivity
	// (v7P1) — a demoted placing edge still holds the structure together
	// (anchor-and-tie splits placement, never a pure component).
	pparent := make([]int, len(g.nodes))
	for i := range pparent {
		pparent[i] = i
	}
	var pfind func(int) int
	pfind = func(a int) int {
		for pparent[a] != a {
			pparent[a] = pparent[pparent[a]]
			a = pparent[a]
		}
		return a
	}
	for _, e := range g.edges {
		if !g.isPlacing(e) {
			continue
		}
		if g.nodes[e.from].comp < 0 && g.nodes[e.to].comp < 0 {
			pparent[pfind(e.from)] = pfind(e.to)
		}
	}
	pureComp := map[int]int{}
	for i, n := range g.nodes {
		if n.comp >= 0 {
			continue
		}
		root := pfind(i)
		cid, ok := pureComp[root]
		if !ok {
			cid = len(g.comps)
			g.comps = append(g.comps, &component{idx: cid, sNode: -1, eNode: -1})
			pureComp[root] = cid
		}
		n.comp = cid
		g.comps[cid].aux = append(g.comps[cid].aux, i)
	}

	// satellites of PURE-component partners resolved before their partner's
	// component existed — re-follow the tie now (v7P5: the satellite joins
	// its partner's component).
	for i := range g.nodes {
		te := m.anchors[i].satelliteOf
		if te == nil {
			continue
		}
		partner := te.from
		if partner == i {
			partner = te.to
		}
		pc := g.nodes[partner].comp
		if pc < 0 {
			continue
		}
		// the satellite brings its whole structure along (v7P5: the layer
		// belongs to the component)
		members := []int{i}
		if sid := m.structOf[i]; sid >= 0 {
			members = m.structRoots[sid]
		}
		for _, mem := range members {
			mn := g.nodes[mem]
			if mn.comp == pc {
				continue
			}
			old := mn.comp
			mn.comp = pc
			g.comps[pc].aux = append(g.comps[pc].aux, mem)
			if old >= 0 {
				aux := g.comps[old].aux[:0]
				for _, a := range g.comps[old].aux {
					if a != mem {
						aux = append(aux, a)
					}
				}
				g.comps[old].aux = aux
			}
		}
	}

	// cross-component ties (v7P2 centrality input)
	for _, e := range g.edges {
		if e.structural {
			continue
		}
		cf, ct := g.nodes[e.from].comp, g.nodes[e.to].comp
		if cf >= 0 && ct >= 0 && cf != ct {
			g.comps[cf].crossTies++
			g.comps[ct].crossTies++
		}
	}
	return m
}
