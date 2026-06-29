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
		// The slots the engine chose, in order along the side.
		slots := make([]float64, len(ends))
		for i, en := range ends {
			slots[i] = portPosition(routes[en.edge], en.atFrom)
		}
		sort.Float64s(slots)

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
