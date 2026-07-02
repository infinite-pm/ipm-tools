package validate

import (
	"fmt"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// This file implements the structural invariants that hold on any IpmGraph
// regardless of how it was produced (ipmt source, graphJSON, programmatic).
// The parser also enforces these at parse time for early-fail diagnostics;
// the validator is the single source of truth for graphs constructed by
// any other means.

// --- IPMV1.1 self-loop ---

// SelfLoopCheck implements IPMV1.1: an edge whose source and target are
// the same node violates SST locality (an agent cannot relate to itself).
type SelfLoopCheck struct{}

func (SelfLoopCheck) Code() string { return "IPMV1.1" }

func (SelfLoopCheck) Description() string {
	return "edges may not connect a node to itself (self-loop)"
}

func (SelfLoopCheck) Run(g *model.IpmGraph) []Finding {
	var out []Finding
	for _, e := range g.Edges {
		if e.Source != e.Target {
			continue
		}
		n := nodeByID(g, e.Source)
		line, col := edgeLoc(g, e)
		out = append(out, Finding{
			Code:     "IPMV1.1",
			Severity: SeverityError,
			Message:  fmt.Sprintf("self-loop on node %q (source == target)", nodeLabel(n)),
			Suggest:  "remove the edge or split the node into two distinct nodes",
			Line:     line,
			Column:   col,
			EdgeID:   e.ID,
			NodeIDs:  idsOf(n),
		})
	}
	return out
}

// --- IPMV1.2 duplicate-edge per unordered pair ---

// DuplicatePairCheck implements IPMV1.2 and §1.7: at most one SST edge may
// connect any unordered pair of nodes. The four SST relations are mutually
// exclusive per pair.
type DuplicatePairCheck struct{}

func (DuplicatePairCheck) Code() string { return "IPMV1.2" }

func (DuplicatePairCheck) Description() string {
	return "at most one SST edge per unordered pair of nodes (four relations mutually exclusive)"
}

func (DuplicatePairCheck) Run(g *model.IpmGraph) []Finding {
	type pair struct{ a, b int } // a < b, canonical unordered key
	seen := map[pair]model.Edge{}
	var out []Finding
	for _, e := range g.Edges {
		a, b := e.Source, e.Target
		if a > b {
			a, b = b, a
		}
		key := pair{a, b}
		if prior, ok := seen[key]; ok {
			ns := nodeByID(g, e.Source)
			nt := nodeByID(g, e.Target)
			line, col := edgeLoc(g, e)
			out = append(out, Finding{
				Code:     "IPMV1.2",
				Severity: SeverityError,
				Message: fmt.Sprintf(
					"duplicate SST edge between %q and %q (prior edge: %s; this edge: %s)",
					nodeLabel(ns), nodeLabel(nt), prior.SstLinkType, e.SstLinkType,
				),
				Suggest: "the four SST relations are mutually exclusive per pair; remove one or restructure via an intermediate event",
				Line:    line,
				Column:  col,
				EdgeID:  e.ID,
				NodeIDs: idsOf(ns, nt),
			})
			continue
		}
		seen[key] = e
	}
	return out
}

// --- IPMV1.3 SST type-pair conformance (the spec table) ---

// TypePairCheck implements IPMV1.3: every SST edge must match the γ(3,4)
// type-pair table.
//
//	LeadsTo:   event   → event
//	PartOf:    event   → event
//	           thing   → event
//	           thing   → thing
//	Expresses: event   → event
//	           event   → concept
//	           thing   → concept
//	           concept → concept
//	NearTo:    event   --- event       (undirected, same-type only)
//	           thing   --- thing
//	           concept --- concept
//
// Anything else is a type-pair violation.
type TypePairCheck struct{}

func (TypePairCheck) Code() string { return "IPMV1.3" }

func (TypePairCheck) Description() string {
	return "SST edge must match the γ(3,4) type-pair table"
}

func (TypePairCheck) Run(g *model.IpmGraph) []Finding {
	var out []Finding
	for _, e := range g.Edges {
		src := nodeByID(g, e.Source)
		tgt := nodeByID(g, e.Target)
		if src == nil || tgt == nil {
			continue
		}
		// Check under each endpoint's effective type — an Unresolved node is taken
		// as its primary candidate, the kind it is actually laid out and drawn as.
		// (A grey node merely "to confirm" is reported by UndecidedKindCheck, not
		// here.) An undecided node whose primary still breaks the table is a real
		// type error and is flagged below.
		st, tt := effNodeType(src), effNodeType(tgt)
		if typePairValid(e.SstLinkType, st, tt) {
			continue
		}
		line, col := edgeLoc(g, e)
		out = append(out, Finding{
			Code:     "IPMV1.3",
			Severity: SeverityError,
			Message: fmt.Sprintf(
				"%s edge has invalid type pair %s → %s (between %q and %q)",
				e.SstLinkType, st, tt, nodeLabel(src), nodeLabel(tgt),
			),
			Suggest: typePairSuggestion(e.SstLinkType, st, tt),
			Line:    line,
			Column:  col,
			EdgeID:  e.ID,
			NodeIDs: idsOf(src, tgt),
		})
	}
	return out
}

// effNodeType returns a node's effective kind: an Unresolved node is taken as
// its primary candidate (Candidates[0]); any other node uses its Type. Same
// primary substitution the solver, layout, and renderer use.
func effNodeType(n *model.Node) model.NodeType {
	if n.Type == model.Unresolved && len(n.Candidates) > 0 {
		return n.Candidates[0]
	}
	return n.Type
}

// typePairValid is the spec table as a function.
func typePairValid(rel model.SstLinkType, src, tgt model.NodeType) bool {
	switch rel {
	case model.LeadsTo:
		return src == model.Event && tgt == model.Event
	case model.PartOf:
		// child → parent: thing|event → event|thing, except event → thing.
		// Canonical valid set: event→event, thing→event, thing→thing.
		if src == model.Concept || tgt == model.Concept {
			return false
		}
		if src == model.Event && tgt == model.Thing {
			return false
		}
		return true
	case model.Expresses:
		// Spec γ(3,4): event→event (#3), event→concept (#6), thing→concept (#9),
		// concept→concept (#10). Target is a concept for every case except the
		// event→event one.
		if src == model.Event && tgt == model.Event {
			return true
		}
		return tgt == model.Concept && (src == model.Event || src == model.Thing || src == model.Concept)
	case model.NearTo:
		return src == tgt
	}
	return false
}

// typePairSuggestion returns a short remediation hint per (rel, src, tgt) triple.
func typePairSuggestion(rel model.SstLinkType, src, tgt model.NodeType) string {
	switch rel {
	case model.LeadsTo:
		return "LeadsTo connects two events; promote both endpoints to ::e or use a different relation"
	case model.PartOf:
		if src == model.Concept {
			return "concepts cannot be contained; classify via expresses (--> concept ::c) or move composition to a thing"
		}
		if tgt == model.Concept {
			return "PartOf parent must be event or thing; concepts cannot contain"
		}
		if src == model.Event && tgt == model.Thing {
			return "participation is thing → event; flip the arrow"
		}
		return "consult the type-pair table for valid PartOf shapes"
	case model.Expresses:
		return "Expresses target must be a concept (::c); change the target's type or use a different relation"
	case model.NearTo:
		return "NearTo connects same-type nodes; cannot relate different SST kinds"
	}
	return ""
}
