package validate

import (
	"fmt"
	"sort"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// This file implements the acyclicity rules: PartOf and LeadsTo each form
// a DAG when considered in isolation, and their composition (happens-before)
// must also be acyclic.

// --- IPMV2.1 PartOf is a DAG ---

// PartOfDagCheck implements IPMV2.1: containment must be acyclic.
// `A --::P--> B --::P--> C --::P--> A` makes A its own ancestor, which is
// nonsensical in SST.
type PartOfDagCheck struct{}

func (PartOfDagCheck) Code() string { return "IPMV2.1" }

func (PartOfDagCheck) Description() string {
	return "containment (PartOf) edges must form a DAG"
}

func (PartOfDagCheck) Run(g *model.IpmGraph) []Finding {
	return dagCycleCheck(g, model.PartOf, "IPMV2.1", "containment cycle")
}

// --- IPMV2.2 LeadsTo is a DAG ---

// LeadsToDagCheck implements IPMV2.2: causal flow must be acyclic.
// A cycle in LeadsTo implies an event leading temporally back to itself.
// Loops in process models are expressed as repeated sub-events, not as
// cyclic edges.
type LeadsToDagCheck struct{}

func (LeadsToDagCheck) Code() string { return "IPMV2.2" }

func (LeadsToDagCheck) Description() string {
	return "causal (LeadsTo) edges must form a DAG"
}

func (LeadsToDagCheck) Run(g *model.IpmGraph) []Finding {
	return dagCycleCheck(g, model.LeadsTo, "IPMV2.2", "causal cycle")
}

// dagCycleCheck runs Tarjan's SCC algorithm restricted to edges of the
// given SST type, and emits one finding per non-trivial SCC found.
func dagCycleCheck(g *model.IpmGraph, rel model.SstLinkType, code, label string) []Finding {
	adj := map[int][]int{}
	nodesInGraph := map[int]bool{}
	for _, e := range g.Edges {
		if e.SstLinkType != rel {
			continue
		}
		adj[e.Source] = append(adj[e.Source], e.Target)
		nodesInGraph[e.Source] = true
		nodesInGraph[e.Target] = true
	}
	if len(adj) == 0 {
		return nil
	}

	starts := make([]int, 0, len(nodesInGraph))
	for n := range nodesInGraph {
		starts = append(starts, n)
	}
	sort.Ints(starts)

	sccs := tarjanSCC(starts, adj)

	var out []Finding
	for _, comp := range sccs {
		if !isCycleSCC(comp) {
			continue
		}
		names := make([]string, 0, len(comp))
		for _, id := range comp {
			n := nodeByID(g, id)
			names = append(names, nodeLabel(n))
		}
		// Anchor location at the smallest-id node in the SCC.
		anchorID := comp[0]
		for _, id := range comp {
			if id < anchorID {
				anchorID = id
			}
		}
		line, col := nodeLoc(g, anchorID)
		out = append(out, Finding{
			Code:     code,
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s (%s): %v", label, rel, names),
			Suggest:  "break the cycle by removing or redirecting one edge; loops in process should be modelled as repeated sub-events",
			Line:     line,
			Column:   col,
			NodeIDs:  append([]int(nil), comp...),
		})
	}
	return out
}

// isCycleSCC reports whether an SCC has more than one node (a genuine
// multi-node cycle). Single-node self-loops are intentionally NOT reported
// here; IPMV1.1 catches them.
func isCycleSCC(comp []int) bool {
	return len(comp) > 1
}

// tarjanSCC computes strongly connected components of a directed graph
// rooted at starts, walking adj. Returns the list of SCCs as slices of
// node IDs.
func tarjanSCC(starts []int, adj map[int][]int) [][]int {
	type state struct {
		index, lowlink int
		onStack        bool
	}
	st := map[int]*state{}
	var stack []int
	var sccs [][]int
	idx := 0

	var strongConnect func(v int)
	strongConnect = func(v int) {
		st[v] = &state{index: idx, lowlink: idx, onStack: true}
		idx++
		stack = append(stack, v)

		neighbours := append([]int(nil), adj[v]...)
		sort.Ints(neighbours)
		for _, w := range neighbours {
			if _, seen := st[w]; !seen {
				strongConnect(w)
				if st[w].lowlink < st[v].lowlink {
					st[v].lowlink = st[w].lowlink
				}
			} else if st[w].onStack {
				if st[w].index < st[v].lowlink {
					st[v].lowlink = st[w].index
				}
			}
		}

		if st[v].lowlink == st[v].index {
			var comp []int
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				st[w].onStack = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			sccs = append(sccs, comp)
		}
	}

	for _, v := range starts {
		if _, seen := st[v]; !seen {
			strongConnect(v)
		}
	}
	return sccs
}
