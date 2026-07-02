package validate

import (
	"fmt"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// This file collects modelling-antipattern checks: warnings and info-level
// findings that surface designs the parser permits but SST conventions
// discourage.
//
// IPMV4.3 was previously implemented here but was reclassified as a
// modelling tip rather than a graph validation: the antipattern depends
// on whether the parent event is a *relation event* (canonical own-007
// case) versus a normal nested *story event* (e.g. murder-e in the
// classic example). That distinction is semantic; a structural check
// produces false positives on legitimate multi-level event nesting. See
// docs/ipm-modeling-tips.md §B for the description.

// --- IPMV4.5.3 stranded thing ---

// StrandedThingCheck implements IPMV4.5.3: a thing with no part-of edge
// to any event/thing and no expresses edge to any concept is detached
// from the model. Usually a typo or an in-progress draft.
type StrandedThingCheck struct{}

func (StrandedThingCheck) Code() string { return "IPMV4.5.3" }

func (StrandedThingCheck) Description() string {
	return "thing with no PartOf or Expresses outgoing edges (info)"
}

func (StrandedThingCheck) Run(g *model.IpmGraph) []Finding {
	// A thing is anchored if it is an endpoint of any PartOf, Expresses,
	// LeadsTo, or near-to edge (either as source or target).
	anchored := map[int]bool{}
	for _, e := range g.Edges {
		switch e.SstLinkType {
		case model.PartOf:
			anchored[e.Source] = true
			anchored[e.Target] = true
		case model.Expresses, model.LeadsTo, model.NearTo:
			anchored[e.Source] = true
			anchored[e.Target] = true
		}
	}
	var out []Finding
	for _, n := range g.Nodes {
		if n.Type != model.Thing {
			continue
		}
		if anchored[n.ID] {
			continue
		}
		line, col := nodeLoc(g, n.ID)
		out = append(out, Finding{
			Code:     "IPMV4.5.3",
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("thing %q has no edges (stranded)", nodeLabel(&n)),
			Suggest:  "attach via thing --> event (part-of) or thing --> concept ::c (expresses), or remove if obsolete",
			Line:     line,
			Column:   col,
			NodeIDs:  []int{n.ID},
		})
	}
	return out
}

// --- IPMV4.5.4 stranded event ---

// StrandedEventCheck implements IPMV4.5.4: an event with no participants,
// no sub-events, no LeadsTo edges in/out, and no Expresses to any concept.
// Such an event doesn't *do* anything in the graph.
type StrandedEventCheck struct{}

func (StrandedEventCheck) Code() string { return "IPMV4.5.4" }

func (StrandedEventCheck) Description() string {
	return "event with no participants, sub-events, leads-to, or expresses (info)"
}

func (StrandedEventCheck) Run(g *model.IpmGraph) []Finding {
	hasAnything := map[int]bool{}
	for _, e := range g.Edges {
		hasAnything[e.Source] = true
		hasAnything[e.Target] = true
	}
	var out []Finding
	for _, n := range g.Nodes {
		if n.Type != model.Event {
			continue
		}
		if hasAnything[n.ID] {
			continue
		}
		line, col := nodeLoc(g, n.ID)
		out = append(out, Finding{
			Code:     "IPMV4.5.4",
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("event %q has no edges (stranded)", nodeLabel(&n)),
			Suggest:  "give it participants (thing --> event), a leads-to predecessor or successor, or remove",
			Line:     line,
			Column:   col,
			NodeIDs:  []int{n.ID},
		})
	}
	return out
}
