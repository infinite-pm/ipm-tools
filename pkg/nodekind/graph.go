package nodekind

import "github.com/infinite-pm/ipm-tools/pkg/ipm/model"

// graphVersion stamps graphs materialized by ToGraph.
const graphVersion = "25.09"

// ToGraph materializes a solver Result as a model.IpmGraph (the inverse of
// resolution): nodes get sequential IDs 1..N, edges reference them and keep their
// resolved Dir and SstLinkType. Src.Type is left empty for the caller to set.
// Used by Solve consumers (e.g. the solver-example tool and pkg/mdembed) to
// round-trip the resolved graph back to ipmt via pkg/ipmtext.
func ToGraph(r *Result) *model.IpmGraph {
	g := &model.IpmGraph{Version: graphVersion}
	for i, n := range r.Nodes {
		g.Nodes = append(g.Nodes, model.Node{
			ID:         i + 1,
			Name:       n.Name,
			Alias:      n.Alias,
			Type:       n.Type,
			Candidates: n.Candidates,
			Tooltip:    n.Tooltip,
		})
	}
	for i, e := range r.Edges {
		g.Edges = append(g.Edges, model.Edge{
			ID:          len(r.Nodes) + i + 1,
			Source:      e.Src + 1,
			Target:      e.Tgt + 1,
			Dir:         e.Dir,
			Tooltip:     e.Tooltip,
			SstLinkType: e.Link,
			SemOrigin:   model.SemExplicit,
		})
	}
	return g
}
