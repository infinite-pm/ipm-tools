package validate

import (
	"fmt"
	"sort"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// RedundantParentParticipationCheck implements IPMV1.4: when a thing X is
// part-of a parent thing Y (X --::P--> Y, both things) and X also
// participates in event E (X --> E, part-of), then Y's own
// participation in E (Y --> E) is implicit by SST convention and should
// not be declared explicitly. Only the most granular thing carries the
// explicit thing-in-event edge; the parent thing's involvement follows.
//
// See SKILL.md "Implicit Relations via Part-Of":
//
//	configured prefix prefix::a --::P--> config file configFile::a
//	prefix --> Load source-prefix configuration ::e loadPrefix::a
//
// In the model above, configFile --> loadPrefix would be redundant
// because prefix part-of configFile + prefix in loadPrefix already
// places configFile inside loadPrefix's involvement by SST convention.
//
// Severity: warning. The graph is not structurally invalid, but the
// explicit parent-thing edge is noise that obscures which sub-thing
// actually participates.
type RedundantParentParticipationCheck struct{}

func (RedundantParentParticipationCheck) Code() string { return "IPMV1.4" }

func (RedundantParentParticipationCheck) Description() string {
	return "parent thing's part-of-event edge is redundant when a sub-thing of it participates in the same event"
}

func (RedundantParentParticipationCheck) Run(g *model.IpmGraph) []Finding {
	// thingChildren[parent] = direct sub-things via thing→thing PartOf.
	thingChildren := map[int][]int{}
	// thingInEvent[thing][event] = the thing participates in the event.
	thingInEvent := map[int]map[int]bool{}
	// edgeOf[(thing,event)] = the actual thing→event PartOf edge.
	edgeOf := map[[2]int]model.Edge{}

	for _, e := range g.Edges {
		if e.SstLinkType != model.PartOf {
			continue
		}
		src := nodeByID(g, e.Source)
		tgt := nodeByID(g, e.Target)
		if src == nil || tgt == nil {
			continue
		}
		switch {
		case src.Type == model.Thing && tgt.Type == model.Thing:
			thingChildren[tgt.ID] = append(thingChildren[tgt.ID], src.ID)
		case src.Type == model.Thing && tgt.Type == model.Event:
			if thingInEvent[src.ID] == nil {
				thingInEvent[src.ID] = map[int]bool{}
			}
			thingInEvent[src.ID][tgt.ID] = true
			edgeOf[[2]int{src.ID, tgt.ID}] = e
		}
	}
	if len(thingChildren) == 0 {
		return nil
	}

	// Transitive sub-things of a given parent (excludes the parent itself).
	subThings := func(parent int) map[int]bool {
		seen := map[int]bool{}
		stack := append([]int(nil), thingChildren[parent]...)
		for _, s := range stack {
			seen[s] = true
		}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, c := range thingChildren[n] {
				if seen[c] {
					continue
				}
				seen[c] = true
				stack = append(stack, c)
			}
		}
		return seen
	}

	// Stable iteration: by parent ID.
	parents := make([]int, 0, len(thingChildren))
	for p := range thingChildren {
		parents = append(parents, p)
	}
	sort.Ints(parents)

	var out []Finding
	for _, parent := range parents {
		events := thingInEvent[parent]
		if len(events) == 0 {
			continue
		}
		subs := subThings(parent)
		// Stable iteration: by event ID then sub-thing ID.
		eventIDs := make([]int, 0, len(events))
		for ev := range events {
			eventIDs = append(eventIDs, ev)
		}
		sort.Ints(eventIDs)
		for _, ev := range eventIDs {
			var hits []int
			for sub := range subs {
				if thingInEvent[sub] != nil && thingInEvent[sub][ev] {
					hits = append(hits, sub)
				}
			}
			if len(hits) == 0 {
				continue
			}
			sort.Ints(hits)
			subNames := make([]string, 0, len(hits))
			for _, h := range hits {
				subNames = append(subNames, nodeLabel(nodeByID(g, h)))
			}
			edge := edgeOf[[2]int{parent, ev}]
			parentNode := nodeByID(g, parent)
			evNode := nodeByID(g, ev)
			line, col := edgeLoc(g, edge)
			finding := Finding{
				Code:     "IPMV1.4",
				Severity: SeverityWarning,
				Message: fmt.Sprintf(
					"parent thing %q participation in event %q is redundant; sub-thing(s) %v already participate",
					nodeLabel(parentNode), nodeLabel(evNode), subNames,
				),
				Suggest: fmt.Sprintf(
					"remove %q --> %q; the parent thing's involvement is implicit (SKILL.md: Implicit Relations via Part-Of)",
					nodeLabel(parentNode), nodeLabel(evNode),
				),
				Line:    line,
				Column:  col,
				EdgeID:  edge.ID,
				NodeIDs: append([]int{parent, ev}, hits...),
			}
			out = append(out, finding)
		}
	}
	return out
}
