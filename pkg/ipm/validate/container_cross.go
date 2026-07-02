package validate

import (
	"fmt"
	"sort"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// LeadsToContainerCrossCheck implements IPMV2.8: a LeadsTo edge `A → B`
// should not "dip into" a container event P that A is outside of. If B
// is (transitively) part-of P, then A must either also be part-of P
// (the cascade stays within P) or have a LeadsTo path to P (A is a
// predecessor of the whole container). Otherwise the leads-to edge
// implicitly reaches into a container without explanation; the cleaner
// model is to lead-to the container itself.
//
// Motivation: the observation/observer asymmetry surfaced by the UC-3
// case where `probe-health-9 -->
// stalled-9` with `stalled-9 --::P--> task-007` puts the *observation*
// inside task-007 even though the *observer* (probe-health-9) is
// outside it. The validator's §2.6 happens-before check passes (no
// cycle), but the modelling is asymmetric.
//
// Detection: for each LeadsTo edge A → B, walk B's transitive PartOf
// ancestors. For each ancestor P that's not also an ancestor of A and
// that A doesn't reach via LeadsTo, flag the edge.
type LeadsToContainerCrossCheck struct{}

func (LeadsToContainerCrossCheck) Code() string { return "IPMV2.8" }

func (LeadsToContainerCrossCheck) Description() string {
	return "leads-to edge crossing into a container that the predecessor is outside of (neither part-of nor leads-to)"
}

func (LeadsToContainerCrossCheck) Run(g *model.IpmGraph) []Finding {
	partOfParents := map[int][]int{}
	leadsToFwd := map[int][]int{}
	for _, e := range g.Edges {
		switch e.SstLinkType {
		case model.PartOf:
			partOfParents[e.Source] = append(partOfParents[e.Source], e.Target)
		case model.LeadsTo:
			leadsToFwd[e.Source] = append(leadsToFwd[e.Source], e.Target)
		}
	}
	if len(leadsToFwd) == 0 {
		return nil
	}

	closure := func(start int, adj map[int][]int) map[int]bool {
		seen := map[int]bool{}
		stack := []int{start}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, nb := range adj[n] {
				if seen[nb] {
					continue
				}
				seen[nb] = true
				stack = append(stack, nb)
			}
		}
		return seen
	}

	var out []Finding
	for _, e := range g.Edges {
		if e.SstLinkType != model.LeadsTo {
			continue
		}
		bAncestors := closure(e.Target, partOfParents)
		if len(bAncestors) == 0 {
			continue
		}
		aAncestors := closure(e.Source, partOfParents)
		aReach := closure(e.Source, leadsToFwd)

		var orphans []int
		for c := range bAncestors {
			if aAncestors[c] || aReach[c] {
				continue
			}
			orphans = append(orphans, c)
		}
		if len(orphans) == 0 {
			continue
		}
		sort.Ints(orphans)
		names := make([]string, 0, len(orphans))
		for _, c := range orphans {
			names = append(names, nodeLabel(nodeByID(g, c)))
		}
		src := nodeByID(g, e.Source)
		tgt := nodeByID(g, e.Target)
		line, col := edgeLoc(g, e)
		nodeIDs := append([]int{e.Source, e.Target}, orphans...)
		out = append(out, Finding{
			Code:     "IPMV2.8",
			Severity: SeverityWarning,
			Message: fmt.Sprintf(
				"leads-to %q → %q reaches into container(s) %v that %q is outside of (not part-of, no leads-to path)",
				nodeLabel(src), nodeLabel(tgt), names, nodeLabel(src),
			),
			Suggest: fmt.Sprintf(
				"either add a leads-to from %q to the container, or move the cascade endpoint outside it",
				nodeLabel(src),
			),
			Line:    line,
			Column:  col,
			EdgeID:  e.ID,
			NodeIDs: nodeIDs,
		})
	}
	return out
}
