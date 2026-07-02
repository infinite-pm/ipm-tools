package validate

import (
	"fmt"
	"sort"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// SiblingOrderingCheck implements IPMV2.7: when two or more events are
// direct sub-events of the same parent event (via PartOf), the graph
// should declare a leads-to ordering between them — direct, transitive,
// or via a shared LeadsTo predecessor/successor (fan-out partial order).
// Otherwise the temporal sequence among siblings is unknown.
//
// Exemption: a NearTo edge between two siblings marks them as
// intentionally parallel; the pair is skipped.
//
// The rule applies recursively at every level of containment.
type SiblingOrderingCheck struct{}

func (SiblingOrderingCheck) Code() string { return "IPMV2.7" }

func (SiblingOrderingCheck) Description() string {
	return "sibling sub-events of the same parent should be ordered via leads-to (direct, transitive, or fan-out)"
}

func (SiblingOrderingCheck) Run(g *model.IpmGraph) []Finding {
	// Adjacency: forward (a --> b) and backward (b ← a) along LeadsTo edges only.
	forward := map[int]map[int]bool{}
	backward := map[int]map[int]bool{}
	nearto := map[int]map[int]bool{}
	for _, e := range g.Edges {
		switch e.SstLinkType {
		case model.LeadsTo:
			addAdj(forward, e.Source, e.Target)
			addAdj(backward, e.Target, e.Source)
		case model.NearTo:
			addAdj(nearto, e.Source, e.Target)
			addAdj(nearto, e.Target, e.Source)
		}
	}

	// Direct sub-events: for each event parent, list of event children via PartOf.
	children := map[int][]int{}
	for _, e := range g.Edges {
		if e.SstLinkType != model.PartOf {
			continue
		}
		src := nodeByID(g, e.Source)
		tgt := nodeByID(g, e.Target)
		if src == nil || tgt == nil {
			continue
		}
		// Use the effective kind (an Unresolved node is taken as its primary
		// candidate) so a grey-but-event-primary sub-event is ordered like any
		// confirmed event — consistent with TypePairCheck and how the node is
		// actually laid out and drawn.
		if effNodeType(src) != model.Event || effNodeType(tgt) != model.Event {
			continue
		}
		children[tgt.ID] = append(children[tgt.ID], src.ID)
	}

	// Per-node forward/backward reachability over LeadsTo.
	// Precomputing once is cheap and lets ordering checks be O(1) per pair.
	reach := func(start int, adj map[int]map[int]bool) map[int]bool {
		seen := map[int]bool{}
		stack := []int{start}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for nb := range adj[n] {
				if seen[nb] {
					continue
				}
				seen[nb] = true
				stack = append(stack, nb)
			}
		}
		return seen
	}

	fwdReach := map[int]map[int]bool{}
	bwdReach := map[int]map[int]bool{}
	for parent, kids := range children {
		_ = parent
		for _, k := range kids {
			if _, ok := fwdReach[k]; !ok {
				fwdReach[k] = reach(k, forward)
			}
			if _, ok := bwdReach[k]; !ok {
				bwdReach[k] = reach(k, backward)
			}
		}
	}

	var out []Finding
	// Stable parent iteration order.
	parents := make([]int, 0, len(children))
	for p := range children {
		parents = append(parents, p)
	}
	sort.Ints(parents)

	for _, parent := range parents {
		kids := append([]int(nil), children[parent]...)
		sort.Ints(kids)
		for i := 0; i < len(kids); i++ {
			for j := i + 1; j < len(kids); j++ {
				a, b := kids[i], kids[j]
				if isOrdered(a, b, fwdReach, bwdReach, nearto) {
					continue
				}
				parentNode := nodeByID(g, parent)
				aNode := nodeByID(g, a)
				bNode := nodeByID(g, b)
				lineA, colA := nodeLoc(g, a)
				out = append(out, Finding{
					Code:     "IPMV2.7",
					Severity: SeverityWarning,
					Message: fmt.Sprintf(
						"sibling sub-events %q and %q of %q have no declared leads-to order",
						nodeLabel(aNode), nodeLabel(bNode), nodeLabel(parentNode),
					),
					Suggest: fmt.Sprintf(
						"add %q --> %q (or reverse), or mark them parallel with %q --- %q",
						nodeLabel(aNode), nodeLabel(bNode), nodeLabel(aNode), nodeLabel(bNode),
					),
					Line:    lineA,
					Column:  colA,
					NodeIDs: idsOf(aNode, bNode, parentNode),
				})
			}
		}
	}
	return out
}

// isOrdered reports whether siblings a and b are temporally ordered.
//
// Cases counting as ordered:
//   - a leads-to-reaches b (or vice versa) — direct or transitive.
//   - a and b share any LeadsTo predecessor — fan-out partial order
//     (e.g. p --> a, b).
//   - a and b share any LeadsTo successor — fan-in convergence
//     (e.g. a, b --> q).
//   - a near-to b — explicitly marked parallel.
func isOrdered(a, b int, fwd, bwd map[int]map[int]bool, near map[int]map[int]bool) bool {
	if near[a][b] {
		return true
	}
	if fwd[a][b] || fwd[b][a] {
		return true
	}
	// Shared predecessor (someone reaches both forward).
	if hasOverlap(bwd[a], bwd[b]) {
		return true
	}
	// Shared successor (both reach someone forward).
	if hasOverlap(fwd[a], fwd[b]) {
		return true
	}
	return false
}

func addAdj(m map[int]map[int]bool, src, tgt int) {
	if m[src] == nil {
		m[src] = map[int]bool{}
	}
	m[src][tgt] = true
}

func hasOverlap(a, b map[int]bool) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	for k := range a {
		if b[k] {
			return true
		}
	}
	return false
}
