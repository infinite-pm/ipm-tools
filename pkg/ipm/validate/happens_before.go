package validate

import (
	"fmt"
	"sort"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// HappensBeforeCheck implements IPMV2.6: LeadsTo and PartOf compose into a
// happens-before partial order whose own cycles are temporal nonsense, even
// when each relation is individually acyclic.
//
// Propagation rules (from sst-34.md + the validator-ideas doc):
//
//  1. x --> y (LeadsTo)                 ⇒ x happens-before y
//  2. x --::P--> y AND y --> z (LeadsTo) ⇒ x happens-before z
//     (a sub-event must finish before its parent's successor starts)
//
// Implementation: build a derived edge set under those rules, then run SCC
// detection. Any non-trivial SCC indicates an inconsistency.
type HappensBeforeCheck struct{}

func (HappensBeforeCheck) Code() string { return "IPMV2.6" }

func (HappensBeforeCheck) Description() string {
	return "composition of LeadsTo and PartOf (happens-before) must be acyclic"
}

func (HappensBeforeCheck) Run(g *model.IpmGraph) []Finding {
	// 1. Build LeadsTo successors of each node.
	leadsTo := map[int][]int{}
	// 2. Build PartOf parents (child --::P--> parent).
	partOfParents := map[int][]int{}
	nodes := map[int]bool{}
	for _, e := range g.Edges {
		switch e.SstLinkType {
		case model.LeadsTo:
			leadsTo[e.Source] = append(leadsTo[e.Source], e.Target)
		case model.PartOf:
			partOfParents[e.Source] = append(partOfParents[e.Source], e.Target)
		}
		nodes[e.Source] = true
		nodes[e.Target] = true
	}
	if len(leadsTo) == 0 {
		// Happens-before only fires through LeadsTo; nothing to check.
		return nil
	}

	// 3. Derived happens-before adjacency:
	//    direct LeadsTo edges, plus
	//    for each LeadsTo z' = y --> z, every (transitive) PartOf descendant x of y
	//    contributes x --> z (sub-event finishes before parent's successor).
	hb := map[int]map[int]bool{}
	addHB := func(a, b int) {
		if hb[a] == nil {
			hb[a] = map[int]bool{}
		}
		hb[a][b] = true
	}

	// parent→children adjacency is invariant across the loop; build it once.
	children := partOfChildren(g)
	// Memoize the PartOf descendant set per distinct src.
	descCache := map[int]map[int]bool{}
	for src, tgts := range leadsTo {
		desc, ok := descCache[src]
		if !ok {
			desc = descendants(src, children)
			descCache[src] = desc
		}
		for _, t := range tgts {
			addHB(src, t)
			// All transitive PartOf descendants of src finish ≤ src finishes ≤ t starts.
			for d := range desc {
				if d == src {
					continue
				}
				addHB(d, t)
			}
		}
	}

	// 4. SCC over hb adjacency.
	adj := map[int][]int{}
	for s, ts := range hb {
		for t := range ts {
			adj[s] = append(adj[s], t)
		}
	}
	starts := make([]int, 0, len(adj))
	for n := range adj {
		starts = append(starts, n)
	}
	sort.Ints(starts)
	sccs := tarjanSCC(starts, adj)

	var out []Finding
	for _, comp := range sccs {
		if len(comp) <= 1 {
			continue
		}
		names := make([]string, 0, len(comp))
		for _, id := range comp {
			names = append(names, nodeLabel(nodeByID(g, id)))
		}
		anchorID := comp[0]
		for _, id := range comp {
			if id < anchorID {
				anchorID = id
			}
		}
		line, col := nodeLoc(g, anchorID)
		out = append(out, Finding{
			Code:     "IPMV2.6",
			Severity: SeverityError,
			Message: fmt.Sprintf(
				"happens-before cycle (LeadsTo + PartOf composition): %v",
				names,
			),
			Suggest: "a leads-to edge re-enters a finished branch via part-of; remove or redirect one edge",
			Line:    line,
			Column:  col,
			NodeIDs: append([]int(nil), comp...),
		})
	}
	return out
}

// partOfChildren returns parent → []children adjacency from PartOf edges.
// (Inverse of partOfParents.)
func partOfChildren(g *model.IpmGraph) map[int][]int {
	out := map[int][]int{}
	for _, e := range g.Edges {
		if e.SstLinkType != model.PartOf {
			continue
		}
		// edge: child --::P--> parent; we want parent → children.
		out[e.Target] = append(out[e.Target], e.Source)
	}
	return out
}

// descendants returns the closure under children (parent → child) starting
// at root, including root itself.
func descendants(root int, children map[int][]int) map[int]bool {
	seen := map[int]bool{root: true}
	stack := []int{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range children[n] {
			if seen[c] {
				continue
			}
			seen[c] = true
			stack = append(stack, c)
		}
	}
	return seen
}
