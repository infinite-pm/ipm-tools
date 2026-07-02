package validate

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

func undecidedNode(id int, name string, cands ...model.NodeType) model.Node {
	return model.Node{ID: id, Name: name, Type: model.Unresolved, Candidates: cands}
}

// An edge into an Unresolved node must be judged under the node's primary
// candidate, not rejected outright: thing --Expresses--> ?[concept,thing] is
// valid because the primary is Concept (tXc). No IPMV1.3 finding.
func TestTypePairUsesPrimaryCandidate(t *testing.T) {
	g := graphWith(
		[]model.Node{thingNode(1, "a"), undecidedNode(2, "x", model.Concept, model.Thing)},
		[]model.Edge{edge(1, 1, 2, model.Expresses)},
	)
	for _, f := range (TypePairCheck{}).Run(g) {
		t.Errorf("unexpected type-pair finding under primary candidate: %s", f.Message)
	}
}

// UndecidedKindCheck flags the grey node: a warning by default, an error in
// strict (publish-gate) mode.
func TestUndecidedKindCheckSeverity(t *testing.T) {
	g := graphWith(
		[]model.Node{undecidedNode(1, "x", model.Event, model.Thing)},
		nil,
	)
	draft := UndecidedKindCheck{}.Run(g)
	if len(draft) != 1 || draft[0].Severity != SeverityWarning {
		t.Fatalf("draft: want 1 warning, got %v", draft)
	}
	strict := UndecidedKindCheck{Strict: true}.Run(g)
	if len(strict) != 1 || strict[0].Severity != SeverityError {
		t.Fatalf("strict: want 1 error, got %v", strict)
	}
}

// IPMV1.5 findings must carry the unresolved node's source location so editors
// and the CLI can jump to it (regression: Line/Column were left at 0).
func TestUndecidedKindCheckSetsLocation(t *testing.T) {
	g := graphWith(
		[]model.Node{undecidedNode(7, "x", model.Event, model.Thing)},
		nil,
	)
	// "ab\ncd x" — node 7's first byte offset is 6 (the 'x'), i.e. line 2, col 4.
	g.Src.Ipmt = "ab\ncd x"
	g.Src.Positions.Nodes[7] = [2]int{6, 7}

	fs := UndecidedKindCheck{}.Run(g)
	if len(fs) != 1 {
		t.Fatalf("want 1 finding, got %v", fs)
	}
	if fs[0].Line != 2 || fs[0].Column != 4 {
		t.Errorf("want location 2:4, got %d:%d", fs[0].Line, fs[0].Column)
	}
}

// AllChecksWith must substitute exactly the UndecidedKindCheck slot with the
// requested strictness, end-to-end through RunChecks: strict=true flips IPMV1.5
// to an error (publish gate), strict=false leaves it a warning.
func TestAllChecksWithFlipsUndecidedSeverity(t *testing.T) {
	g := graphWith(
		[]model.Node{undecidedNode(1, "x", model.Event, model.Thing)},
		nil,
	)

	findIPMV15 := func(fs []Finding) *Finding {
		var hits []Finding
		for _, f := range fs {
			if f.Code == "IPMV1.5" {
				hits = append(hits, f)
			}
		}
		if len(hits) != 1 {
			t.Fatalf("want exactly 1 IPMV1.5 finding, got %d: %v", len(hits), fs)
		}
		return &hits[0]
	}

	draft := RunChecks(g, AllChecksWith(false))
	if f := findIPMV15(draft); f.Severity != SeverityWarning {
		t.Errorf("draft: want IPMV1.5 warning, got %s", f.Severity)
	}
	if HasErrors(draft) {
		t.Errorf("draft: want no errors, got %v", draft)
	}

	strict := RunChecks(g, AllChecksWith(true))
	if f := findIPMV15(strict); f.Severity != SeverityError {
		t.Errorf("strict: want IPMV1.5 error, got %s", f.Severity)
	}
	if !HasErrors(strict) {
		t.Errorf("strict: want HasErrors true, got %v", strict)
	}

	// The substitution must leave the check count unchanged (exactly one slot
	// replaced, none added or dropped).
	if got, want := len(AllChecksWith(true)), len(AllChecks()); got != want {
		t.Errorf("AllChecksWith changed check count: got %d, want %d", got, want)
	}
}
