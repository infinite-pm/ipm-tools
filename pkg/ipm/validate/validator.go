package validate

import "github.com/infinite-pm/ipm-tools/pkg/ipm/model"

// Check is one semantic validation pass over an IpmGraph.
type Check interface {
	// Code returns the stable identifier (e.g. "IPMV2.7") used to tag findings.
	Code() string
	// Description returns a one-line description used in --list-checks output.
	Description() string
	// Run inspects g and returns any findings.
	Run(g *model.IpmGraph) []Finding
}

// AllChecks returns the full set of checks the validator runs.
//
// Validator-owned, parser also enforces (early-fail diagnostics):
//
//	IPMV1.1  Self-loop
//	IPMV1.2  Duplicate edge per unordered node pair
//	IPMV1.3  SST type-pair conformance against the γ(3,4) table
//
// Validator-only (parser does not catch):
//
//	IPMV2.1  PartOf is a DAG
//	IPMV2.2  LeadsTo is a DAG
//	IPMV2.6  Happens-before (LeadsTo + PartOf composition) is acyclic
//	IPMV2.7  Sibling sub-events should have declared leads-to order
//	IPMV2.8  Leads-to edge crossing into a container the predecessor is outside of
//	IPMV2.9  Leads-to must connect whole (outermost) events, not a part-of sub-part
//	IPMV4.5.3 Stranded thing
//	IPMV4.5.4 Stranded event
//
// Order is fixed for deterministic output before per-finding sorting.
func AllChecks() []Check {
	return []Check{
		// Structural invariants (errors).
		SelfLoopCheck{},
		DuplicatePairCheck{},
		TypePairCheck{},
		// Implicit-relation redundancy (warning).
		RedundantParentParticipationCheck{},
		// Acyclicity (errors).
		PartOfDagCheck{},
		LeadsToDagCheck{},
		HappensBeforeCheck{},
		// Modelling / temporal (warnings).
		SiblingOrderingCheck{},
		LeadsToContainerCrossCheck{},
		LeadsToWholeEventCheck{},
		// Hygiene (info).
		StrandedThingCheck{},
		StrandedEventCheck{},
		// Undecided kinds (warning by default; error under the publish gate —
		// see AllChecksWith).
		UndecidedKindCheck{},
	}
}

// AllChecksWith returns AllChecks with the undecided-kind strictness set:
// strict=true makes Unresolved nodes errors (the publish gate), false leaves
// them as warnings (draft / live editing).
func AllChecksWith(strict bool) []Check {
	checks := AllChecks()
	for i, c := range checks {
		if _, ok := c.(UndecidedKindCheck); ok {
			checks[i] = UndecidedKindCheck{Strict: strict}
		}
	}
	return checks
}

// Run executes every check in AllChecks against g and returns the merged,
// sorted findings list.
func Run(g *model.IpmGraph) []Finding {
	return RunChecks(g, AllChecks())
}

// RunChecks executes the given checks against g.
func RunChecks(g *model.IpmGraph, checks []Check) []Finding {
	var all []Finding
	for _, c := range checks {
		all = append(all, c.Run(g)...)
	}
	sortFindings(all)
	return all
}

// HasErrors reports whether any finding has SeverityError.
func HasErrors(fs []Finding) bool {
	for _, f := range fs {
		if f.Severity == SeverityError {
			return true
		}
	}
	return false
}
