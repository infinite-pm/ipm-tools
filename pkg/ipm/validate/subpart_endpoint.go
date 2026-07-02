package validate

import (
	"fmt"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// LeadsToWholeEventCheck implements IPMV2.9: a leads-to edge between events
// must connect WHOLE (outermost) events, never a sub-part.
//
// Leads-to expresses temporal flow between whole events. If an event Y is
// part-of some event Z (directly or transitively), then Y is a sub-part of Z
// and a leads-to edge with Y as its source OR target is wrong: the flow
// should connect Z — the outermost part-of ancestor — not its sub-part Y.
//
// Motivation (abstract late-side-branch fixture): the chain
// `e1 --> e2 --> e3` is wrong when `e2 --::P--> e8` (e2 is part-of e8).
// The correct model is `e1 --> e8 --> e3`, because e2 is only a component of
// the whole event e8.
//
// Detection: for each LeadsTo edge A → B, resolve each endpoint to its
// outermost whole event. If both resolve to the SAME whole, the edge sequences
// sibling sub-events inside one container — legitimate (IPMV2.7 recommends it),
// so it is skipped. Otherwise the edge crosses a container boundary: flag each
// endpoint that is a sub-part and resolve its outermost part-of ancestor for
// the message. Applied transitively (sub-sub-parts).
//
// Severity: warning (matches the IPMV2.x temporal-order family).
type LeadsToWholeEventCheck struct{}

func (LeadsToWholeEventCheck) Code() string { return "IPMV2.9" }

func (LeadsToWholeEventCheck) Description() string {
	return "leads-to must connect whole (outermost) events, not a part-of sub-part"
}

func (LeadsToWholeEventCheck) Run(g *model.IpmGraph) []Finding {
	// PartOf parents (child → parents). Only consider event→event containment:
	// leads-to endpoints are events, and the "whole event" we want is an event
	// ancestor. A thing parent would not be a candidate leads-to endpoint.
	partOfParents := map[int][]int{}
	for _, e := range g.Edges {
		if e.SstLinkType != model.PartOf {
			continue
		}
		src := nodeByID(g, e.Source)
		tgt := nodeByID(g, e.Target)
		if src == nil || tgt == nil {
			continue
		}
		if effNodeType(src) != model.Event || effNodeType(tgt) != model.Event {
			continue
		}
		partOfParents[src.ID] = append(partOfParents[src.ID], tgt.ID)
	}
	if len(partOfParents) == 0 {
		return nil
	}

	// effWhole resolves an endpoint to the outermost whole event it belongs to
	// (itself if it is not a sub-part).
	effWhole := func(n int) int {
		if top, isSub := outermostPartOfAncestor(n, partOfParents); isSub {
			return top
		}
		return n
	}

	var out []Finding
	for _, e := range g.Edges {
		if e.SstLinkType != model.LeadsTo {
			continue
		}
		// Same-container sub-event chains are LEGITIMATE — sequencing sibling
		// sub-events INSIDE one whole event is exactly what IPMV2.7 recommends,
		// and the canonical dressing tutorial is built on them. Only a leads-to
		// that crosses a container boundary is wrong: flag it only when the two
		// endpoints resolve to DIFFERENT whole events (one reaches into a
		// container the other sits outside of). Same outermost whole → skip.
		if effWhole(e.Source) == effWhole(e.Target) {
			continue
		}
		// Flag each endpoint that is a sub-part. An edge may have both
		// endpoints reaching into containers; report each as its own finding
		// so the message names the right outermost ancestor for that endpoint.
		for _, endpoint := range []int{e.Source, e.Target} {
			top, isSubpart := outermostPartOfAncestor(endpoint, partOfParents)
			if !isSubpart {
				continue
			}
			src := nodeByID(g, e.Source)
			tgt := nodeByID(g, e.Target)
			sub := nodeByID(g, endpoint)
			whole := nodeByID(g, top)
			line, col := edgeLoc(g, e)
			out = append(out, Finding{
				Code:     "IPMV2.9",
				Severity: SeverityWarning,
				Message: fmt.Sprintf(
					"leads-to %q → %q: %q is a sub-part of %q; leads-to must connect the whole event %q, not its sub-part",
					nodeLabel(src), nodeLabel(tgt), nodeLabel(sub), nodeLabel(whole), nodeLabel(whole),
				),
				Suggest: fmt.Sprintf(
					"redirect the leads-to endpoint from %q to its whole event %q",
					nodeLabel(sub), nodeLabel(whole),
				),
				Line:    line,
				Column:  col,
				EdgeID:  e.ID,
				NodeIDs: idsOf(src, tgt, sub, whole),
			})
		}
	}
	return out
}

// outermostPartOfAncestor walks node's transitive PartOf parents and returns
// the outermost (top-most) ancestor — the whole event the node is a sub-part
// of — together with whether node has any part-of parent at all.
//
// When containment branches (a node part-of two parents), the deterministic
// lowest-ID top ancestor is chosen so output is stable. A part-of cycle (which
// IPMV2.1 already flags as an error) is broken by the visited set, so this
// terminates regardless.
func outermostPartOfAncestor(node int, parents map[int][]int) (top int, isSubpart bool) {
	if len(parents[node]) == 0 {
		return 0, false
	}
	visited := map[int]bool{node: true}
	best := -1
	stack := append([]int(nil), parents[node]...)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[n] {
			continue
		}
		visited[n] = true
		if len(parents[n]) == 0 {
			// n is a top-most ancestor (no further parent). Prefer lowest ID.
			if best == -1 || n < best {
				best = n
			}
			continue
		}
		stack = append(stack, parents[n]...)
	}
	if best == -1 {
		// Every reachable ancestor had a parent — only possible under a
		// part-of cycle (IPMV2.1 territory). Fall back to the lowest-ID
		// direct parent so we still report something sensible.
		for _, p := range parents[node] {
			if best == -1 || p < best {
				best = p
			}
		}
	}
	return best, true
}
