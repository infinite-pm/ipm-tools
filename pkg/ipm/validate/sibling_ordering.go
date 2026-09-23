package validate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// SiblingOrderingCheck implements IPMV2.7, read per PARENT: the direct
// sub-events of one parent event should read as one kind of thing — a
// SEQUENCE, whose parts are ordered by leads-to, or a SET, whose parts are
// not ordered at all. What the rule flags is a parent that is half of each:
// some parts take part in a leads-to order with their siblings and some stand
// outside it, so a reader cannot tell whether the loose ones run in parallel
// or whether somebody simply never wrote their order down.
//
// A part counts as ORDERED when a leads-to relates it to a sibling — direct,
// transitive, or through a shared LeadsTo predecessor or successor (a fan-out
// or fan-in partial order). Otherwise it is LOOSE.
//
//   - every part ordered — a sequence. Silent.
//   - every part loose — a set. Silent: "these are the parts, their order is
//     not stated" is a complete model, and it does not need an edge per pair
//     to say so.
//   - mixed — one finding for the parent, naming its loose parts.
//
// Exemption: a loose part carrying a NearTo to a sibling is declared parallel
// and is not named, so a mixed parent whose loose parts each carry one is
// silent. This keeps the older per-pair reading working — a pair a person
// marked `a --- b` was accepted before and is accepted now — while a producer
// with n genuinely parallel parts no longer has to draw n(n-1)/2 near-to
// edges to satisfy the check: it writes none, and the parent reads as a set.
// A near-to is for a tie the structure cannot carry; between two parts of one
// parent the containment already says "side by side".
//
// The rule applies recursively at every level of containment.
//
// See docs/ipm-validator-rules.md §3 for the rule as documented, and
// docs/how-to-model.md for when a near-to is worth drawing.
type SiblingOrderingCheck struct{}

func (SiblingOrderingCheck) Code() string { return "IPMV2.7" }

func (SiblingOrderingCheck) Description() string {
	return "a parent's sub-events should read as a sequence or as a set, not half of each"
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

	// Direct sub-events: for each event parent, the set of event children via
	// PartOf. A set, so a graph built outside the parser (which rejects the
	// duplicate) cannot list one child twice and have it compared with itself.
	childSet := map[int]map[int]bool{}
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
		addAdj(childSet, tgt.ID, src.ID)
	}

	// Stable parent iteration order, each parent's children sorted by ID.
	parents := make([]int, 0, len(childSet))
	children := map[int][]int{}
	for p, set := range childSet {
		parents = append(parents, p)
		kids := make([]int, 0, len(set))
		for k := range set {
			kids = append(kids, k)
		}
		sort.Ints(kids)
		children[p] = kids
	}
	sort.Ints(parents)

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
	for _, kids := range children {
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
	for _, parent := range parents {
		kids := children[parent]
		if len(kids) < 2 {
			continue
		}

		// Classify: a part is ordered as soon as one sibling orders it.
		ordered := map[int]bool{}
		for i := 0; i < len(kids); i++ {
			for j := i + 1; j < len(kids); j++ {
				a, b := kids[i], kids[j]
				if isOrdered(a, b, fwdReach, bwdReach) {
					ordered[a] = true
					ordered[b] = true
				}
			}
		}

		var loose []int
		for _, k := range kids {
			if !ordered[k] {
				loose = append(loose, k)
			}
		}
		// A sequence (no loose part) or a set (no ordered part) — both are
		// whole statements; only the mix leaves the reader guessing.
		if len(loose) == 0 || len(loose) == len(kids) {
			continue
		}

		// A loose part that carries a near-to to a sibling is declared
		// parallel: one edge per loose part, not one per pair.
		var unmarked []*model.Node
		for _, k := range loose {
			if hasNearToSibling(k, kids, nearto) {
				continue
			}
			if n := nodeByID(g, k); n != nil {
				unmarked = append(unmarked, n)
			}
		}
		if len(unmarked) == 0 {
			continue
		}

		parentNode := nodeByID(g, parent)
		partner := orderedSibling(g, kids, ordered, unmarked[0].ID)
		noun, verb := "sub-event", "has"
		if len(unmarked) > 1 {
			noun, verb = "sub-events", "have"
		}
		lineA, colA := nodeLoc(g, unmarked[0].ID)
		out = append(out, Finding{
			Code:     "IPMV2.7",
			Severity: SeverityWarning,
			Message: fmt.Sprintf(
				"%s %s of %q %s no declared leads-to order, while the other sub-events are ordered",
				noun, labelList(unmarked), nodeLabel(parentNode), verb,
			),
			Suggest: suggestForLoose(unmarked, partner),
			Line:    lineA,
			Column:  colA,
			NodeIDs: idsOf(append(append([]*model.Node(nil), unmarked...), parentNode)...),
		})
	}
	return out
}

// isOrdered reports whether siblings a and b are temporally ordered by
// leads-to. Near-to plays no part here: it does not order anything, it
// declares that the order is deliberately absent, which the parent-level
// rule handles.
//
// Cases counting as ordered:
//   - a leads-to-reaches b (or vice versa) — direct or transitive.
//   - a and b share any LeadsTo predecessor — fan-out partial order
//     (e.g. p --> a, b).
//   - a and b share any LeadsTo successor — fan-in convergence
//     (e.g. a, b --> q).
func isOrdered(a, b int, fwd, bwd map[int]map[int]bool) bool {
	if a == b {
		return false
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

// hasNearToSibling reports whether k carries a NearTo to one of its siblings —
// the author's (or producer's) way of saying "this part runs beside the rest".
func hasNearToSibling(k int, kids []int, near map[int]map[int]bool) bool {
	for _, s := range kids {
		if s == k {
			continue
		}
		if near[k][s] {
			return true
		}
	}
	return false
}

// orderedSibling returns a sibling of k that IS part of the parent's order —
// the natural other end for a leads-to or a near-to. Falls back to any other
// sibling, and to nil when k has none.
func orderedSibling(g *model.IpmGraph, kids []int, ordered map[int]bool, k int) *model.Node {
	var fallback *model.Node
	for _, s := range kids {
		if s == k {
			continue
		}
		n := nodeByID(g, s)
		if n == nil {
			continue
		}
		if ordered[s] {
			return n
		}
		if fallback == nil {
			fallback = n
		}
	}
	return fallback
}

// labelList renders node labels as `"a"`, `"a" and "b"`, or `"a", "b" and 2
// more` — a message names the first few and leaves the rest to NodeIDs.
func labelList(ns []*model.Node) string {
	const maxNamed = 3
	quoted := make([]string, 0, maxNamed)
	for _, n := range ns {
		if len(quoted) == maxNamed {
			break
		}
		quoted = append(quoted, fmt.Sprintf("%q", nodeLabel(n)))
	}
	rest := len(ns) - len(quoted)
	if rest > 0 {
		return strings.Join(quoted, ", ") + fmt.Sprintf(" and %d more", rest)
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
}

// suggestForLoose spells out the two ways to finish a mixed parent: order the
// loose parts, or declare them parallel with ONE near-to each.
func suggestForLoose(unmarked []*model.Node, partner *model.Node) string {
	other := "a sibling"
	if partner != nil {
		other = fmt.Sprintf("%q", nodeLabel(partner))
	}
	if len(unmarked) == 1 {
		return fmt.Sprintf(
			"add %q --> %s (or reverse), or declare it parallel with one near-to %q --- %s",
			nodeLabel(unmarked[0]), other, nodeLabel(unmarked[0]), other,
		)
	}
	return fmt.Sprintf(
		"order each of them against a sibling with -->, or declare each parallel with ONE near-to (e.g. %q --- %s); a parent whose sub-events are all parallel needs no near-to at all",
		nodeLabel(unmarked[0]), other,
	)
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
