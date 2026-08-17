package layout

import "sort"

// OrderSharedPorts fixes the crossing that happens when several edges leave one
// side of a node in the wrong order.
//
// An edge port is box-relative — "left side, 40% down" — so it survives a node
// moving. Its ORDER along that side does not: the engine spread the fan to
// match where the partners were when it placed them, and once a consumer moves
// nodes (a zoom/canvas consumer's frames), two edges sharing a side can end up
// assigned the opposite way round from their partners. They then cross
// immediately, right next to the node they share.
//
// This permutes the fractional positions WITHIN each (node, side) group so
// their order matches the partners' order along that side. The set of
// fractions is unchanged — the engine's chosen spacing is kept exactly, only
// which edge gets which slot changes — and no edge moves to a different side,
// so nothing else about the drawing shifts.
//
// It cannot introduce a crossing it did not remove: after the permutation the
// ports are monotonic in the partner coordinate, which is the definition of
// "these two do not cross at this end".
//
// Returns the number of endpoints whose slot changed.
func OrderSharedPorts(g *Graph) int {
	if g == nil || len(g.Edges) < 2 {
		return 0
	}
	idx := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		idx[n.ID] = i
	}
	routes := RoutesOf(g)

	// One entry per edge END that sits on a resolvable side.
	type endRef struct {
		edge   int
		atFrom bool
		along  int // partner's coordinate along the side's axis
	}
	type groupKey struct {
		node string
		side string
	}
	groups := make(map[groupKey][]endRef)

	for i := range g.Edges {
		e := &g.Edges[i]
		if e.From == e.To {
			continue
		}
		fi, okF := idx[e.From]
		ti, okT := idx[e.To]
		if !okF || !okT {
			continue
		}
		from, to := g.Nodes[fi], g.Nodes[ti]
		// The partner anchor is the opposite end's port point: where the line
		// is actually heading, not just the box centre.
		sx, sy := EdgePortPoint(from, to, routes[i].Source)
		tx, ty := EdgePortPoint(to, from, routes[i].Target)

		for _, end := range [2]struct {
			atFrom bool
			side   string
			node   string
			px, py int
		}{
			{true, routes[i].Source.Side, e.From, tx, ty},
			{false, routes[i].Target.Side, e.To, sx, sy},
		} {
			along, ok := alongAxis(end.side, end.px, end.py)
			if !ok {
				continue // "center" and unknown sides have no order to fix
			}
			k := groupKey{end.node, end.side}
			groups[k] = append(groups[k], endRef{edge: i, atFrom: end.atFrom, along: along})
		}
		_, _ = sx, sy
	}

	// Deterministic group order — the permutation itself does not depend on it,
	// but the traversal should not vary run to run.
	keys := make([]groupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].node != keys[b].node {
			return keys[a].node < keys[b].node
		}
		return keys[a].side < keys[b].side
	})

	changed := 0
	for _, k := range keys {
		ends := groups[k]
		if len(ends) < 2 {
			continue
		}
		// A FLOW end — S/E, or a leads-to between events — keeps the slot it
		// has: the flow corridor owns its node's midline (v7P6) and a tie
		// must not be permuted onto it. The engine pins flow ends at
		// 0.5; without this exclusion the permutation handed
		// that 0.5 to whichever tie's partner was more central along the
		// side, and S -> a process stopped being vertical. Only the non-flow
		// ends are permuted, among the non-flow slots.
		var flow, rest []endRef
		for _, en := range ends {
			if isCorridorEdge(g, g.Edges[en.edge]) {
				flow = append(flow, en)
			} else {
				rest = append(rest, en)
			}
		}
		if len(rest) == 0 || (len(rest) < 2 && len(flow) == 0) {
			continue // nothing to permute among
		}
		ends = rest
		// The slots the engine chose for the non-flow ends, in order.
		slots := make([]float64, len(ends))
		for i, en := range ends {
			slots[i] = portPosition(routes[en.edge], en.atFrom)
		}
		sort.Float64s(slots)

		// With a flow end on the side, a tie sits on the same side of the
		// flow line as its partner — as the engine spreads them (v7P6: the
		// corridor never yields, so ties leave beside it, not across it).
		// The frame moved partners; a tie the engine put at 0.25 whose
		// partner is now to the right would cross the S stub at the port
		// (an environment... -> A pod, kubernetes s:87+126). So the SLOT SET
		// is mirrored across the flow's slot until as many slots lie on
		// each side of it as there are partners on that side; the partner-
		// order assignment below then hands the right-hand slots to the
		// right-hand partners. A mirror that would land on a taken slot is
		// skipped — the engine's spread stays rather than two ends on one
		// point.
		if len(flow) > 0 {
			fp := portPosition(routes[flow[0].edge], flow[0].atFrom)
			// The divider is the flow's PARTNER position, not the node's
			// centre: two lines out of one side are uncrossed when their
			// slots are in the same order as their partners, and in a frame
			// the flow's partner is wherever E ended up (reasoning: E left-
			// below; a tie to "near", right of E's line, mirrored to 0.25 by
			// the centre rule crossed it). So the ties are counted against
			// the flow's own along-coordinate — the group's ordinary
			// uncrossed rule, with the flow's slot held where it is.
			fa := flow[0].along
			wantLess := 0
			for _, en := range ends {
				if en.along < fa {
					wantLess++
				}
			}
			taken := map[float64]bool{fp: true}
			for _, p := range slots {
				taken[p] = true
			}
			mirror := func(need func(less int) bool, pick func() int) {
				for {
					less := 0
					for _, p := range slots {
						if p < fp {
							less++
						}
					}
					if !need(less) {
						return
					}
					j := pick()
					if j < 0 {
						return
					}
					q := 2*fp - slots[j]
					if q <= 0 || q >= 1 || taken[q] {
						return
					}
					delete(taken, slots[j])
					taken[q] = true
					slots[j] = q
					sort.Float64s(slots)
				}
			}
			// too few on the low side: mirror the highest slot down
			mirror(func(less int) bool { return less < wantLess },
				func() int {
					for j := len(slots) - 1; j >= 0; j-- {
						if slots[j] > fp {
							return j
						}
					}
					return -1
				})
			// too many on the low side: mirror the lowest slot up
			mirror(func(less int) bool { return less > wantLess },
				func() int {
					for j := 0; j < len(slots); j++ {
						if slots[j] < fp {
							return j
						}
					}
					return -1
				})
		}

		// No two ends on one slot. Pins can collide — applyLiftedEdgePorts
		// gives every edge lifted out of a closed composite the slot of its
		// child's Y, so two edges lifted from one child share a slot; and a
		// tie can sit exactly on the flow's. Two lines out of one point can
		// only overlap. A duplicate is nudged to the nearest free slot,
		// outward from the flow when there is one (so it stays on its
		// partner's side of the flow line), else alternating around it.
		{
			taken := map[float64]bool{}
			var fp float64
			hasFlow := len(flow) > 0
			if hasFlow {
				fp = portPosition(routes[flow[0].edge], flow[0].atFrom)
				taken[fp] = true
			}
			for j := range slots {
				if !taken[slots[j]] {
					taken[slots[j]] = true
					continue
				}
				dir := 1.0
				if hasFlow && slots[j] <= fp && j < len(slots)/2 {
					dir = -1
				} else if !hasFlow && j%2 == 0 {
					dir = -1
				}
				for _, d := range []float64{0.15, 0.25, 0.35, 0.1, 0.2, 0.3, 0.4} {
					for _, sgn := range []float64{dir, -dir} {
						q := slots[j] + sgn*d
						if q > 0.04 && q < 0.96 && !taken[q] {
							slots[j] = q
							taken[q] = true
							goto placed
						}
					}
				}
			placed:
			}
			sort.Float64s(slots)
		}

		// Ends in partner order. Ties keep declaration order so the result is
		// stable; a tie means the two partners are level, where either
		// assignment is equally uncrossed.
		sort.SliceStable(ends, func(a, b int) bool { return ends[a].along < ends[b].along })

		for i, en := range ends {
			e := &g.Edges[en.edge]
			if e.Route == nil {
				e.Route = &EdgeRouteJSON{
					Source: PortJSON{Side: routes[en.edge].Source.Side, Position: routes[en.edge].Source.Position},
					Target: PortJSON{Side: routes[en.edge].Target.Side, Position: routes[en.edge].Target.Position},
				}
			}
			port := &e.Route.Target
			if en.atFrom {
				port = &e.Route.Source
			}
			if port.Position != slots[i] {
				port.Position = slots[i]
				changed++
			}
		}
	}
	return changed
}

// alongAxis returns the partner coordinate that orders a side: a left or right
// side is ordered top-to-bottom, a top or bottom side left-to-right.
func alongAxis(side string, px, py int) (int, bool) {
	switch side {
	case "left", "right":
		return py, true
	case "top", "bottom":
		return px, true
	}
	return 0, false
}

func portPosition(r EdgeRoute, atFrom bool) float64 {
	if atFrom {
		return r.Source.Position
	}
	return r.Target.Position
}

// isCorridorEdge is the flow corridor as the PORT passes see it: a leads-to
// between two events (isFlowEdge, the router's notion) OR a boundary's edge —
// S -> first event, last event -> E. The corridor is "the vertical column
// through each event's centre where S, the chain and E draw their pinned
// line" (v7P6), so the S/E ends own their event's midline just as the chain
// does; without them S -> a process sat at 0.25 and was not vertical.
//
// Deliberately NOT folded into isFlowEdge itself: the router prices a tie
// crossing a flow edge as "slicing the timeline", and counting the S/E stubs
// there hid 450 more ties in the NDA corpus (ties fanning past a 40px S stub
// each paid the flow price). Owning a midline and being un-crossable are two
// different things.
func isCorridorEdge(g *Graph, e Edge) bool {
	if isFlowEdge(g, e) {
		return true
	}
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
	isEv := func(t string) bool { return t == "event" || t == "boundary" }
	return isEv(ft) && isEv(tt) && (ft == "boundary" || tt == "boundary")
}
