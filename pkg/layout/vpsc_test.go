package layout

import (
	"math"
	"testing"
)

func almostEq(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// A single violated constraint: both endpoints share the displacement in
// inverse proportion to weight, meeting at the weighted optimum.
func TestVPSCSingleConstraint(t *testing.T) {
	vars := []VPSCVar{{Desired: 0, Weight: 1}, {Desired: 4, Weight: 1}}
	pos := SolveSeparations(vars, []VPSCConstraint{{Left: 0, Right: 1, Gap: 10}})
	if !almostEq(pos[1]-pos[0], 10) {
		t.Fatalf("constraint not tight: %v", pos)
	}
	if !almostEq(pos[0], -3) || !almostEq(pos[1], 7) {
		t.Fatalf("expected symmetric split (-3, 7), got %v", pos)
	}
}

// A heavy variable barely moves: the light one absorbs the displacement.
func TestVPSCWeightedSplit(t *testing.T) {
	vars := []VPSCVar{{Desired: 0, Weight: 1000}, {Desired: 0, Weight: 1}}
	pos := SolveSeparations(vars, []VPSCConstraint{{Left: 0, Right: 1, Gap: 100}})
	if !almostEq(pos[1]-pos[0], 100) {
		t.Fatalf("constraint not tight: %v", pos)
	}
	if pos[0] < -0.2 {
		t.Fatalf("heavy variable moved too far: %v", pos)
	}
}

// A chain that must spread: transitive merges keep every gap and stay
// feasible; the chain centers on the weighted mean of desires.
func TestVPSCChainSpread(t *testing.T) {
	vars := []VPSCVar{{0, 1}, {0, 1}, {0, 1}, {0, 1}}
	cs := []VPSCConstraint{{0, 1, 10}, {1, 2, 10}, {2, 3, 10}}
	pos := SolveSeparations(vars, cs)
	for _, c := range cs {
		if pos[c.Right]-pos[c.Left] < c.Gap-1e-6 {
			t.Fatalf("constraint %v violated: %v", c, pos)
		}
	}
	mid := (pos[0] + pos[3]) / 2
	if !almostEq(mid, 0) {
		t.Fatalf("chain should center on the desired mean 0, got mid=%v (%v)", mid, pos)
	}
}

// Already-satisfied constraints move nothing — the no-op guarantee that
// makes the solver safe to call unconditionally.
func TestVPSCNoOpWhenSatisfied(t *testing.T) {
	vars := []VPSCVar{{0, 1}, {50, 1}, {120, 1}}
	cs := []VPSCConstraint{{0, 1, 40}, {1, 2, 40}}
	pos := SolveSeparations(vars, cs)
	for i, v := range vars {
		if !almostEq(pos[i], v.Desired) {
			t.Fatalf("satisfied system must not move: %v", pos)
		}
	}
}

// GrowToFit inserts space exactly between the requested pair; rows before
// the pair stay, rows after shift by the deficit.
func TestGrowToFitElasticBetween(t *testing.T) {
	rows := []float64{0, 100, 200, 300}
	order := []int{0, 1, 2, 3}
	pos := GrowToFit(rows, order, 1, 2, 250, nil)
	if !almostEq(pos[2]-pos[1], 250) {
		t.Fatalf("grown gap wrong: %v", pos)
	}
	if !almostEq(pos[1]-pos[0], 100) || !almostEq(pos[3]-pos[2], 100) {
		t.Fatalf("other gaps must keep their size: %v", pos)
	}
	// The 150px deficit splits across the system's freedom, but ORDER holds.
	for k := 0; k+1 < len(order); k++ {
		if pos[order[k]] >= pos[order[k+1]] {
			t.Fatalf("order violated: %v", pos)
		}
	}
}

// Skeleton immobility: with heavy skeleton weights, growth comes from moving
// the light rows, and the heavy anchor stays put.
func TestGrowToFitRespectsWeights(t *testing.T) {
	rows := []float64{0, 100, 200}
	order := []int{0, 1, 2}
	w := []float64{1000, 1, 1}
	pos := GrowToFit(rows, order, 0, 1, 180, w)
	if math.Abs(pos[0]) > 0.5 {
		t.Fatalf("heavy row 0 should stay, got %v", pos)
	}
	if pos[1]-pos[0] < 180-1e-6 {
		t.Fatalf("grown gap not satisfied: %v", pos)
	}
}

// Determinism: same input, same output, every time.
func TestVPSCDeterministic(t *testing.T) {
	vars := []VPSCVar{{3, 2}, {1, 1}, {4, 5}, {1, 1}, {5, 3}}
	cs := []VPSCConstraint{{1, 0, 4}, {0, 2, 4}, {3, 2, 6}, {2, 4, 2}}
	first := SolveSeparations(vars, cs)
	for run := 0; run < 10; run++ {
		again := SolveSeparations(vars, cs)
		for i := range first {
			if first[i] != again[i] {
				t.Fatalf("nondeterministic at %d: %v vs %v", i, first, again)
			}
		}
	}
	for _, c := range cs {
		if first[c.Right]-first[c.Left] < c.Gap-1e-6 {
			t.Fatalf("constraint %v violated: %v", c, first)
		}
	}
	_ = sortedByDesired(vars)
}
