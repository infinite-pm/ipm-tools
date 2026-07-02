package validate

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// IPMV4.3 was previously tested here but the check was reclassified as a
// modelling tip (see docs/ipm-modeling-tips.md §B) because it produced
// false positives on legitimate multi-level event nesting.

// --- IPMV4.5.3 stranded thing ---

func TestStrandedThing_Flags(t *testing.T) {
	g := graphWith(
		[]model.Node{thingNode(1, "alone")},
		nil,
	)
	if got := (StrandedThingCheck{}).Run(g); len(got) != 1 || got[0].Severity != SeverityInfo {
		t.Fatalf("expected one info finding, got %v", got)
	}
}

func TestStrandedThing_AttachedOK(t *testing.T) {
	g := graphWith(
		[]model.Node{thingNode(1, "x"), eventNode(2, "ev")},
		[]model.Edge{edge(3, 1, 2, model.PartOf)},
	)
	if got := (StrandedThingCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("attached thing should not trigger, got %v", got)
	}
}

// --- IPMV4.5.4 stranded event ---

func TestStrandedEvent_Flags(t *testing.T) {
	g := graphWith(
		[]model.Node{eventNode(1, "ghost")},
		nil,
	)
	if got := (StrandedEventCheck{}).Run(g); len(got) != 1 || got[0].Severity != SeverityInfo {
		t.Fatalf("expected one info finding, got %v", got)
	}
}

func TestStrandedEvent_ConnectedOK(t *testing.T) {
	g := graphWith(
		[]model.Node{eventNode(1, "a"), eventNode(2, "b")},
		[]model.Edge{edge(3, 1, 2, model.LeadsTo)},
	)
	if got := (StrandedEventCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("connected events should not trigger, got %v", got)
	}
}
