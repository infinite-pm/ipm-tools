package layout

import "sort"

// VPSC — Variable Placement with Separation Constraints (Dwyer & Marriott),
// the per-axis grow/settle solver of the v6 substrate: given
// desired positions, weights, and hard "left + gap ≤ right" constraints,
// find positions satisfying every constraint with (near-)minimal weighted
// squared displacement. Because the constraints encode the skeleton's
// existing order, a solve can stretch gaps but provably cannot reorder —
// the mechanical form of the orthogonal-ordering contract (R3).
//
// The implementation is the incremental block-merge algorithm: variables
// start as singleton blocks at their desired positions; the most-violated
// constraint merges the two blocks it spans (the constraint becomes active,
// the merged block re-centers at its weighted optimum). Merge-only is always
// FEASIBLE and DETERMINISTIC; it is optimal whenever no active constraint
// would need to deactivate again, which holds for the grow use-case (the
// desired positions already respect the constraint order, displacement only
// pushes one way). The classic split refinement can be added when a client
// needs optimality under disorderly desired positions; SolveSeparations'
// tests pin the optimal cases brute-force.

// VPSCVar is one variable on one axis.
type VPSCVar struct {
	// Desired is the position the variable wants (current/ideal coordinate).
	Desired float64
	// Weight scales the displacement cost; use large weights for boxes that
	// must not move (skeleton rows/columns) and small ones for movable aux.
	// Must be > 0.
	Weight float64
}

// VPSCConstraint demands Position[Left] + Gap ≤ Position[Right].
type VPSCConstraint struct {
	Left, Right int
	Gap         float64
}

type vpscBlock struct {
	vars  []int // indexes into the solver's variable slice
	posn  float64
	wSum  float64 // Σ w_i
	wdSum float64 // Σ w_i (d_i − offset_i)
}

// SolveSeparations returns positions for vars satisfying every constraint.
// Constraint graphs must be acyclic in the Left→Right direction (a cycle has
// no feasible solution); the caller guarantees this by construction — the
// skeleton's order relation is a DAG by definition.
func SolveSeparations(vars []VPSCVar, constraints []VPSCConstraint) []float64 {
	n := len(vars)
	blockOf := make([]int, n)
	offset := make([]float64, n)
	blocks := make([]*vpscBlock, n)
	for i := range vars {
		blockOf[i] = i
		blocks[i] = &vpscBlock{
			vars:  []int{i},
			posn:  vars[i].Desired,
			wSum:  vars[i].Weight,
			wdSum: vars[i].Weight * vars[i].Desired,
		}
	}

	violation := func(c VPSCConstraint) float64 {
		lp := blocks[blockOf[c.Left]].posn + offset[c.Left]
		rp := blocks[blockOf[c.Right]].posn + offset[c.Right]
		return lp + c.Gap - rp
	}

	// Deterministic processing: repeatedly satisfy the most violated
	// constraint (ties by index). Each merge strictly reduces the block
	// count, so at most n−1 merges happen; the scan is O(m) per round —
	// comfortably fast at diagram scale.
	for {
		worst, worstV := -1, 1e-9
		for ci, c := range constraints {
			if blockOf[c.Left] == blockOf[c.Right] {
				continue // same block: separation fixed by active offsets
			}
			if v := violation(c); v > worstV {
				worst, worstV = ci, v
			}
		}
		if worst < 0 {
			break
		}
		c := constraints[worst]
		lb, rb := blockOf[c.Left], blockOf[c.Right]
		L, R := blocks[lb], blocks[rb]
		// Right block joins the left one; every member's offset becomes
		// relative to L's reference, displaced so the constraint is tight.
		shift := offset[c.Left] + c.Gap - offset[c.Right]
		for _, vi := range R.vars {
			offset[vi] += shift
			blockOf[vi] = lb
			L.vars = append(L.vars, vi)
			L.wSum += vars[vi].Weight
		}
		// Recompute L.posn as the weighted mean of (desired − offset) over all
		// members; wdSum is rebuilt from scratch since R's vars now carry new
		// offsets.
		L.wdSum = 0
		for _, vi := range L.vars {
			L.wdSum += vars[vi].Weight * (vars[vi].Desired - offset[vi])
		}
		L.posn = L.wdSum / L.wSum
		blocks[rb] = nil
	}

	out := make([]float64, n)
	for i := range vars {
		out[i] = blocks[blockOf[i]].posn + offset[i]
	}
	return out
}

// GrowToFit is the substrate's "make room here" helper: positions holds the
// current coordinates of ordered interval starts (e.g. row tops), order is
// their index order along the axis, and need demands that
// positions[after]−positions[before] ≥ minSpan for one adjacent pair. All
// other adjacent gaps keep at least their current size, so growth inserts
// space exactly between the colliding rows and shifts everything beyond —
// the elastic-between behavior as one solve instead of cascading shifts.
func GrowToFit(positions []float64, order []int, before, after int, minSpan float64, weights []float64) []float64 {
	vars := make([]VPSCVar, len(positions))
	for i, p := range positions {
		w := 1.0
		if weights != nil {
			w = weights[i]
		}
		vars[i] = VPSCVar{Desired: p, Weight: w}
	}
	var cs []VPSCConstraint
	for k := 0; k+1 < len(order); k++ {
		a, b := order[k], order[k+1]
		gap := positions[b] - positions[a]
		if a == before && b == after && gap < minSpan {
			gap = minSpan
		}
		cs = append(cs, VPSCConstraint{Left: a, Right: b, Gap: gap})
	}
	return SolveSeparations(vars, cs)
}

// sortedByDesired is a test helper exposed for determinism checks: VPSC must
// not depend on input slice order beyond indexes.
func sortedByDesired(vars []VPSCVar) []int {
	idx := make([]int, len(vars))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return vars[idx[a]].Desired < vars[idx[b]].Desired })
	return idx
}
