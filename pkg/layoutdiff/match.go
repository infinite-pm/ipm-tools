package layoutdiff

import (
	"fmt"
	"sort"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// Node identity across two graphs is the parser's node ID, which the engine
// emits verbatim (layout7's emit stage). Same source + same parser ⇒ same
// IDs, so an ID match is exact, not a heuristic.
//
// The exception is the synthesized S/E boundary pair: it is numbered
// maxID+1… as the skeleton stage creates it, so its ID moves when the
// component count or order changes. Those are matched by (type, label,
// component-ish position) instead — see matchNodes.

// nodePair is one matched node, either side possibly absent.
type nodePair struct {
	ID  string
	Old *layout.Node
	New *layout.Node
}

// matchNodes pairs the nodes of two graphs. Order is deterministic:
// matched pairs in old-graph order, then old-only, then new-only.
func matchNodes(oldG, newG *layout.Graph) []nodePair {
	oldByID := map[string]*layout.Node{}
	newByID := map[string]*layout.Node{}
	for i := range oldG.Nodes {
		oldByID[oldG.Nodes[i].ID] = &oldG.Nodes[i]
	}
	for i := range newG.Nodes {
		newByID[newG.Nodes[i].ID] = &newG.Nodes[i]
	}

	usedNew := map[string]bool{}
	var pairs []nodePair
	for i := range oldG.Nodes {
		o := &oldG.Nodes[i]
		if n, ok := newByID[o.ID]; ok && sameIdentity(o, n) {
			pairs = append(pairs, nodePair{ID: o.ID, Old: o, New: n})
			usedNew[o.ID] = true
		}
	}

	// Second pass for the leftovers — a boundary marker that renumbered, or a
	// node whose ID moved because the PARSER changed between the two engines.
	// Key on what a reader would call the same node: kind + label + alias.
	leftoverNew := map[string][]*layout.Node{}
	for i := range newG.Nodes {
		n := &newG.Nodes[i]
		if usedNew[n.ID] {
			continue
		}
		leftoverNew[identityKey(n)] = append(leftoverNew[identityKey(n)], n)
	}
	var onlyOld []nodePair
	for i := range oldG.Nodes {
		o := &oldG.Nodes[i]
		if _, ok := newByID[o.ID]; ok && sameIdentity(o, newByID[o.ID]) {
			continue
		}
		k := identityKey(o)
		if cands := leftoverNew[k]; len(cands) > 0 {
			n := cands[0]
			leftoverNew[k] = cands[1:]
			usedNew[n.ID] = true
			pairs = append(pairs, nodePair{ID: o.ID, Old: o, New: n})
			continue
		}
		onlyOld = append(onlyOld, nodePair{ID: o.ID, Old: o})
	}
	pairs = append(pairs, onlyOld...)

	for i := range newG.Nodes {
		n := &newG.Nodes[i]
		if !usedNew[n.ID] {
			pairs = append(pairs, nodePair{ID: n.ID, New: n})
		}
	}
	return pairs
}

// sameIdentity guards the ID match: an ID that now names a different KIND of
// node is not the same node, and pairing them would report a nonsense move.
func sameIdentity(a, b *layout.Node) bool {
	return a.Type == b.Type
}

func identityKey(n *layout.Node) string {
	return n.Type + "\x00" + n.Label + "\x00" + n.Alias
}

// edgeKey identifies an edge by its endpoints and relation kind. Parallel
// edges between the same pair get an occurrence suffix in declaration order,
// which is stable because the engine emits edges in declaration order.
func edgeKey(e *layout.Edge, occ int) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d", e.From, e.To, e.Base, occ)
}

type edgePair struct {
	Key string
	Old *layout.Edge
	New *layout.Edge
}

// matchEdges pairs edges by (from, to, base, occurrence).
func matchEdges(oldG, newG *layout.Graph) []edgePair {
	index := func(g *layout.Graph) (map[string]*layout.Edge, []string) {
		seen := map[string]int{}
		out := map[string]*layout.Edge{}
		var order []string
		for i := range g.Edges {
			e := &g.Edges[i]
			base := e.From + "\x00" + e.To + "\x00" + e.Base
			k := edgeKey(e, seen[base])
			seen[base]++
			out[k] = e
			order = append(order, k)
		}
		return out, order
	}
	oldIdx, oldOrder := index(oldG)
	newIdx, newOrder := index(newG)

	var pairs []edgePair
	for _, k := range oldOrder {
		pairs = append(pairs, edgePair{Key: k, Old: oldIdx[k], New: newIdx[k]})
	}
	for _, k := range newOrder {
		if _, ok := oldIdx[k]; !ok {
			pairs = append(pairs, edgePair{Key: k, New: newIdx[k]})
		}
	}
	return pairs
}

// translation is the whole-canvas shift between the two graphs, per axis.
//
// It matters more than it looks: one node growing 20px wider shifts every
// node to its right by 20, and without removing that shift EVERY diagram
// reads as "everything moved". What a human perceives as movement is the
// residual after that shift.
//
// The statistic is the MODE — the delta that leaves the most nodes still —
// not the mean (which one wildly-moved node drags) and not the median
// (which, on an even split, picks a side arbitrarily: two nodes shifting
// {0, +60} would silently declare +60 "the canvas" and report the node that
// did NOT move as having moved -60).
//
// Ties break toward the SMALLEST absolute shift, and so toward zero. An
// audit that is unsure must over-report movement, never absorb it: a change
// wrongly shown is a second of a reader's time, a change wrongly hidden is
// the bug the tool exists to catch.
func translation(pairs []nodePair) (int, int) {
	var dxs, dys []int
	for _, p := range pairs {
		if p.Old == nil || p.New == nil {
			continue
		}
		dxs = append(dxs, p.New.X-p.Old.X)
		dys = append(dys, p.New.Y-p.Old.Y)
	}
	return mode(dxs), mode(dys)
}

func mode(v []int) int {
	if len(v) == 0 {
		return 0
	}
	freq := map[int]int{}
	for _, d := range v {
		freq[d]++
	}
	keys := make([]int, 0, len(freq))
	for d := range freq {
		keys = append(keys, d)
	}
	// Deterministic: most frequent first, then closest to zero, then the
	// lower value — no map-order dependence.
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if freq[a] != freq[b] {
			return freq[a] > freq[b]
		}
		if abs(a) != abs(b) {
			return abs(a) < abs(b)
		}
		return a < b
	})
	return keys[0]
}
