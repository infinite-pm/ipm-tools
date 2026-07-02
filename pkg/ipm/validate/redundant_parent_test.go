package validate

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// SKILL.md "Avoid" example: prefix --::P--> configFile, prefix --> loadPrefix,
// configFile --> loadPrefix. The parent thing's edge is redundant.
func TestRedundantParent_FlagsParentParticipation(t *testing.T) {
	g := graphWith(
		[]model.Node{
			thingNode(1, "prefix"),
			thingNode(2, "configFile"),
			eventNode(3, "loadPrefix"),
		},
		[]model.Edge{
			edge(4, 1, 2, model.PartOf), // prefix part-of configFile
			edge(5, 1, 3, model.PartOf), // prefix in loadPrefix
			edge(6, 2, 3, model.PartOf), // configFile in loadPrefix (redundant)
		},
	)
	fs := RedundantParentParticipationCheck{}.Run(g)
	if len(fs) != 1 || fs[0].Severity != SeverityWarning {
		t.Fatalf("expected one warning, got %v", fs)
	}
	// The flagged edge should be the parent thing → event one (edge 6).
	if fs[0].EdgeID != 6 {
		t.Errorf("expected edge 6 (parent→event) to be flagged, got edge %d", fs[0].EdgeID)
	}
}

// Only sub-thing participates: no redundancy.
func TestRedundantParent_OnlyChildOK(t *testing.T) {
	g := graphWith(
		[]model.Node{
			thingNode(1, "prefix"),
			thingNode(2, "configFile"),
			eventNode(3, "loadPrefix"),
		},
		[]model.Edge{
			edge(4, 1, 2, model.PartOf),
			edge(5, 1, 3, model.PartOf),
		},
	)
	if got := (RedundantParentParticipationCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("only child participating should pass, got %v", got)
	}
}

// Only parent participates: no redundancy (no sub-thing in the event).
func TestRedundantParent_OnlyParentOK(t *testing.T) {
	g := graphWith(
		[]model.Node{
			thingNode(1, "prefix"),
			thingNode(2, "configFile"),
			eventNode(3, "loadPrefix"),
		},
		[]model.Edge{
			edge(4, 1, 2, model.PartOf),
			edge(5, 2, 3, model.PartOf),
		},
	)
	if got := (RedundantParentParticipationCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("only parent participating should pass, got %v", got)
	}
}

// Transitive: imu part-of phone part-of devices, imu in evt, devices in evt.
// Even though phone is not in evt directly, devices is, and imu (transitive
// sub-thing of devices) is. So devices → evt is redundant.
func TestRedundantParent_TransitiveSubThing(t *testing.T) {
	g := graphWith(
		[]model.Node{
			thingNode(1, "imu"),
			thingNode(2, "phone"),
			thingNode(3, "devices"),
			eventNode(4, "evt"),
		},
		[]model.Edge{
			edge(5, 1, 2, model.PartOf), // imu part-of phone
			edge(6, 2, 3, model.PartOf), // phone part-of devices
			edge(7, 1, 4, model.PartOf), // imu in evt
			edge(8, 3, 4, model.PartOf), // devices in evt (redundant)
		},
	)
	fs := RedundantParentParticipationCheck{}.Run(g)
	if len(fs) != 1 {
		t.Fatalf("expected one warning for transitive case, got %v", fs)
	}
	if fs[0].EdgeID != 8 {
		t.Errorf("expected edge 8 (devices→evt) flagged, got %d", fs[0].EdgeID)
	}
}

// Both child and grandparent participate, but parent does not. Two redundant
// edges? No — the redundancy is about the parent in this triple; here the
// grandparent has imu as a transitive sub-thing in the same event, so
// grandparent's edge is redundant. Phone's edge is the granular one
// (well, phone doesn't participate, so only imu and devices). Same as above.
func TestRedundantParent_SiblingThingsNotConfused(t *testing.T) {
	// Two unrelated things both participating in the same event — no redundancy.
	g := graphWith(
		[]model.Node{
			thingNode(1, "alice"),
			thingNode(2, "bob"),
			eventNode(3, "meeting"),
		},
		[]model.Edge{
			edge(4, 1, 3, model.PartOf),
			edge(5, 2, 3, model.PartOf),
		},
	)
	if got := (RedundantParentParticipationCheck{}).Run(g); len(got) != 0 {
		t.Fatalf("unrelated things in same event should pass, got %v", got)
	}
}
