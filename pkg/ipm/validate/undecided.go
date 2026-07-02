package validate

import (
	"fmt"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// UndecidedKindCheck flags nodes whose kind is Unresolved (grey — kind not yet
// pinned down). In the default draft mode each is a warning ("confirm this
// kind"); in Strict mode — the publish gate — each is an error, since a finished
// diagram should carry no undecided kinds.
//
// Type-pair validity for edges touching such nodes is handled separately by
// TypePairCheck, which substitutes the node's primary candidate.
type UndecidedKindCheck struct{ Strict bool }

func (UndecidedKindCheck) Code() string { return "IPMV1.5" }

func (UndecidedKindCheck) Description() string {
	return "node kind should be resolved (no Unresolved/grey nodes)"
}

func (c UndecidedKindCheck) Run(g *model.IpmGraph) []Finding {
	var out []Finding
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != model.Unresolved {
			continue
		}
		sev := SeverityWarning
		if c.Strict {
			sev = SeverityError
		}
		line, col := nodeLoc(g, n.ID)
		out = append(out, Finding{
			Code:     "IPMV1.5",
			Severity: sev,
			Message:  fmt.Sprintf("unresolved node kind: confirm %q (candidates: %s)", nodeLabel(n), candidateList(n.Candidates)),
			Suggest:  "replace the ::?… marker with a single ::e/::t/::c (or split the node)",
			Line:     line,
			Column:   col,
			NodeIDs:  []int{n.ID},
		})
	}
	return out
}

func candidateList(cs []model.NodeType) string {
	if len(cs) == 0 {
		return "unknown"
	}
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = string(c)
	}
	return strings.Join(parts, "/")
}
