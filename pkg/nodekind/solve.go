// Package nodekind resolves the event/thing/concept kind of every node and emits
// an ipm-oriented node+edge graph.
//
// Kind is never stored on a node (N4L README §3): it is emergent from the
// incident relations. ipm requires kinds conforming to the γ(3,4) type-pair table
// enforced by ipm-tools/pkg/ipm/validate (IPMV1.3) — the 11 legal combinations:
//
//	LeadsTo:   event → event                                   (eLe)
//	PartOf:    thing → thing,  thing → event,  event → event   (tPt tPe ePe)
//	Expresses: event → event,  event|thing|concept → concept   (eXe eXc tXc cXc)
//	NearTo:    same type only                                  (eNe tNt cNc)
//
// The solver is CONSTRAINT-FAITHFUL: it derives kinds ONLY from these type-pairs,
// with no semantic heuristics. Every node starts with the full domain
// {Event,Thing,Concept}; arc-consistency (orderCandidates) prunes each node's
// domain to the kinds that keep every incident edge a legal pair, to a fixpoint.
// A node is DECIDED only when a single kind survives — in practice the only kind a
// structure can uniquely force is Event (via LeadsTo, and what that propagates to
// through NearTo / PartOf / Expresses). Otherwise the node stays Unresolved (grey)
// carrying its surviving candidate set; we never guess. `eXe` is a legal, emitted
// outcome, so a node that is both a LeadsTo endpoint and an Expresses target is
// simply an Event — there is no split. A final pass drops any edge still outside
// the table, so output always passes ipm-validate IPMV1.3.
package nodekind

import (
	"fmt"
	"slices"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// Confidence tags how a node's kind was decided.
type Confidence string

const (
	High    Confidence = "inferred-high"    // forced by a hard constraint or structural role
	Low     Confidence = "inferred-low"     // no disambiguating signal — left Unresolved
	Default Confidence = "inferred-default" // decided by a role-based default (SolveWithDefaults), not forced
)

// Unresolved (model.Unresolved) marks a node whose event/thing/concept kind
// cannot be determined from the graph — we do NOT guess Thing. It is not a
// γ(3,4) kind (so edges touching it can fall outside the 11-combination
// triangle); renderers show it neutral/grey. It marks "confirm this kind".

// ResolvedNode is an ipm node with a decided kind.
type ResolvedNode struct {
	Name       string
	Alias      string
	Type       model.NodeType
	Candidates []model.NodeType // possible kinds when Type==Unresolved, primary first; nil otherwise
	Confidence Confidence
	Tooltip    string // free-text annotation (e.g. aggregated note text)
	Origin     string // originating N4L node text
}

// ResolvedEdge is an ipm-oriented edge referencing ResolvedNode indices.
type ResolvedEdge struct {
	Src     int
	Tgt     int
	Link    model.SstLinkType
	Dir     model.ArrowDir
	Tooltip string
}

// Result is the resolved graph plus human-facing notes and a decision trace.
type Result struct {
	Nodes    []ResolvedNode
	Edges    []ResolvedEdge
	Warnings []string      // human-facing summary notes (also mirrored into Trace)
	Trace    []TraceRecord // per-decision audit trace (see solver-audit tool)
}

// semEdge is an edge in canonical ipm role orientation — From --Link--> To,
// where for PartOf From is the part and To is the container. The resolver core
// works only on these; callers supply already-oriented model edges.
type semEdge struct {
	From, To string
	Tooltip  string
	Link     model.SstLinkType
}

// Solve resolves node kinds for an already-ipm-oriented typed graph: each edge
// carries its SstLinkType and points the ipm way (PartOf is part→container,
// Expresses source→concept, LeadsTo src→tgt). Node identity is by Name; any
// Type/Candidates on the input nodes are advisory and ignored — kinds are
// derived from the edges.
//
// This is the solver's only entry point: it is format-independent. It consumes a
// pre-oriented model.IpmGraph — the ipmt path feeds it a parsed graph directly,
// and any N4L front-end is expected to convert to model.IpmGraph (owning the n4l
// direction / PartOf-inversion rules) before calling Solve.
func Solve(g *model.IpmGraph) *Result {
	nodes, sems := toRecs(g)
	return solveCore(nodes, sems, false)
}

// SolveWithDefaults is Solve plus a role-based default pass: every node the
// type-pairs leave grey is decided to its preferred kind (Expresses target →
// Concept, PartOf participant → Thing, Expresses source → Thing, otherwise Event),
// so the graph is fully resolved. Opt-in (the `ipmt unresolved defaults` fence
// directive); Solve alone leaves those nodes grey.
func SolveWithDefaults(g *model.IpmGraph) *Result {
	nodes, sems := toRecs(g)
	return solveCore(nodes, sems, true)
}

// toRecs flattens an ipm graph into the resolver core's node/edge records.
func toRecs(g *model.IpmGraph) ([]nodeRec, []semEdge) {
	nodes := make([]nodeRec, len(g.Nodes))
	nameByID := make(map[int]string, len(g.Nodes))
	for i, n := range g.Nodes {
		nodes[i] = nodeRec{Text: n.Name, Alias: n.Alias, Tooltip: n.Tooltip}
		nameByID[n.ID] = n.Name
	}
	sems := make([]semEdge, 0, len(g.Edges))
	for _, e := range g.Edges {
		// Direct mapping: an ipm edge is already in semEdge orientation
		// (PartOf source=part→target=container == From=part,To=container).
		sems = append(sems, semEdge{From: nameByID[e.Source], To: nameByID[e.Target], Link: e.SstLinkType, Tooltip: e.Tooltip})
	}
	return nodes, sems
}

// nodeRec is the minimal node info the resolver core needs (text identity plus
// passthrough alias/tooltip), independent of the input format.
type nodeRec struct {
	Text, Alias, Tooltip string
}

// solveCore is the format-independent resolution core. It seeds every node with
// the full {Event,Thing,Concept} domain, then runs arc-consistency over the
// type-pair table (dedupe → orderCandidates → assignPrimaries → drop → cycles).
func solveCore(nodes []nodeRec, sems []semEdge, defaults bool) *Result {
	r := &Result{}

	// Materialize one resolved node per distinct text, seeded fully undecided:
	// every node starts Unresolved with all three γ(3,4) kinds. Constraint-faithful
	// — kinds come only from arc-consistency over the type-pair table
	// (typePairValid); there are no semantic heuristics, and input markers are
	// ignored (kinds are derived purely from the edges).
	idx := map[string]int{}
	for _, n := range nodes {
		if _, ok := idx[n.Text]; ok {
			continue
		}
		idx[n.Text] = len(r.Nodes)
		r.Nodes = append(r.Nodes, ResolvedNode{
			Name: n.Text, Alias: n.Alias, Type: model.Unresolved,
			Candidates: []model.NodeType{model.Event, model.Thing, model.Concept},
			Confidence: Low, Tooltip: n.Tooltip, Origin: n.Text,
		})
		r.trace(TraceRecord{Phase: "init", Event: "domain", Producer: "Solve#init", Node: n.Text,
			Payload: map[string]any{"candidates": kindStrings([]model.NodeType{model.Event, model.Thing, model.Concept})}})
	}

	// Edges: semEdges are already ipm-oriented (From --Link--> To). No split, so
	// each endpoint maps straight to its single resolved node.
	for _, se := range sems {
		sid, okS := idx[se.From]
		tid, okT := idx[se.To]
		if !okS || !okT {
			continue
		}
		dir := model.DirFwd
		if se.Link == model.NearTo {
			dir = model.DirUndir
		}
		r.Edges = append(r.Edges, ResolvedEdge{Src: sid, Tgt: tid, Link: se.Link, Dir: dir, Tooltip: se.Tooltip})
		r.trace(TraceRecord{Phase: "D", Event: "edge", Producer: "Solve#D",
			Edge:    edgeKey(r.Nodes[sid].Name, r.Nodes[tid].Name),
			Payload: map[string]any{"link": string(se.Link), "dir": string(dir), "phrase": se.Tooltip}})
	}

	r.dedupe()          // self-loops + duplicate node-pairs (IPMV1.1/1.2)
	r.orderCandidates() // arc-consistency: prune domains by typePairValid to a fixpoint; decide singletons
	r.assignPrimaries() // choose a globally type-consistent primary kind per still-grey node
	if defaults {
		r.applyDefaults() // opt-in: collapse each still-grey node to its preferred primary
	}
	r.dropInvalidEdges()
	r.breakCycles()

	// Record each node's final resolution for the audit.
	for i := range r.Nodes {
		n := &r.Nodes[i]
		payload := map[string]any{"type": string(n.Type), "confidence": string(n.Confidence)}
		if n.Type == model.Unresolved {
			payload["candidates"] = kindStrings(n.Candidates)
		}
		r.trace(TraceRecord{Phase: "C", Event: "resolve", Producer: "Solve#C", Node: n.Origin, Payload: payload})
	}

	r.traceSummary(len(nodes))
	return r
}

// edgeKey is the stable "src → tgt" label used for edge trace records.
func edgeKey(src, tgt string) string { return src + " → " + tgt }

// traceSummary appends the final counts record, so a report can be rendered from
// the JSONL stream alone. originNodes is the number of input N4L nodes.
func (r *Result) traceSummary(originNodes int) {
	var ev, th, co, un int
	for _, n := range r.Nodes {
		switch n.Type {
		case model.Event:
			ev++
		case model.Thing:
			th++
		case model.Concept:
			co++
		case model.Unresolved:
			un++
		}
	}
	splits, dropped := 0, 0
	for _, rec := range r.Trace {
		switch {
		case rec.Event == "split":
			splits++
		case rec.Phase == "drop" || rec.Phase == "dedupe" || rec.Phase == "cycle":
			// every edge removal: dropInvalidEdges, dedupe (self-loop/dup), breakCycles
			dropped++
		}
	}
	r.trace(TraceRecord{Phase: "done", Event: "summary", Producer: "Solve", Payload: map[string]any{
		"nodes": len(r.Nodes), "event": ev, "thing": th, "concept": co, "unresolved": un,
		"originNodes": originNodes, "splits": splits, "edges": len(r.Edges),
		"dropped": dropped, "warnings": len(r.Warnings),
	}})
}

// applyDefaults collapses every still-grey node to its primary candidate — the
// role-preferred kind assignPrimaries already chose (and proved globally
// consistent) — turning the constraint-faithful output into a fully-decided
// graph. This is the opt-in `defaults` mode; the strict solver leaves these nodes
// grey. The primaries form a satisfying assignment, so no edge becomes invalid.
func (r *Result) applyDefaults() {
	for i := range r.Nodes {
		n := &r.Nodes[i]
		if n.Type != model.Unresolved || len(n.Candidates) == 0 {
			continue
		}
		k := n.Candidates[0]
		r.trace(TraceRecord{Phase: "default", Event: "resolve", Producer: "applyDefaults", Node: n.Origin,
			Detail: fmt.Sprintf("defaulted %q to %s (role-preferred)", n.Name, k), Payload: map[string]any{"type": string(k)}})
		n.Type, n.Candidates, n.Confidence = k, nil, Default
	}
}

// breakCycles makes the directed (non-NearTo) edge set acyclic so LeadsTo,
// PartOf, and their happens-before composition pass the validator's DAG checks
// (IPMV2.1/2.2/2.6). SSTorytime tolerates cycles (it's a knowledge base); IPM's
// layout requires a DAG. A DFS drops each back-edge that would close a cycle.
func (r *Result) breakCycles() {
	adj := make(map[int][]int, len(r.Nodes)) // node index -> outgoing edge indices
	for i, e := range r.Edges {
		if e.Link == model.NearTo {
			continue
		}
		adj[e.Src] = append(adj[e.Src], i)
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[int]int, len(r.Nodes))
	drop := make(map[int]bool)
	var dfs func(u int)
	dfs = func(u int) {
		color[u] = gray
		for _, ei := range adj[u] {
			if drop[ei] {
				continue
			}
			switch v := r.Edges[ei].Tgt; color[v] {
			case gray:
				drop[ei] = true // back-edge closes a cycle
				r.warn(TraceRecord{Phase: "cycle", Event: "break", Producer: "breakCycles",
					Edge:   edgeKey(r.Nodes[r.Edges[ei].Src].Name, r.Nodes[v].Name),
					Detail: fmt.Sprintf("broke %s cycle: dropped %q -> %q", r.Edges[ei].Link, r.Nodes[r.Edges[ei].Src].Name, r.Nodes[v].Name)})
			case white:
				dfs(v)
			}
		}
		color[u] = black
	}
	for i := range r.Nodes {
		if color[i] == white {
			dfs(i)
		}
	}
	if len(drop) == 0 {
		return
	}
	out := r.Edges[:0:0]
	for i, e := range r.Edges {
		if !drop[i] {
			out = append(out, e)
		}
	}
	r.Edges = out
}

// gammaKind reports whether t is one of the three γ(3,4) node kinds (Event,
// Thing, Concept). Unresolved and any future off-model kind return false.
func gammaKind(t model.NodeType) bool {
	return t == model.Event || t == model.Thing || t == model.Concept
}

// typePairValid is the γ(3,4) type-pair table (IPMV1.3 in ipm-tools/pkg/ipm/validate)
// — the 11 legal <src><edge><tgt> combinations, in shortcut notation:
//
//	L: eLe
//	P: ePe tPe tPt
//	X: eXe eXc tXc cXc      (target is a concept except eXe; no t→t X)
//	N: eNe tNt cNc          (undirected, same kind)
func typePairValid(rel model.SstLinkType, src, tgt model.NodeType) bool {
	// Only the three γ(3,4) kinds can form a valid pair. An Unresolved (or any
	// other off-model) endpoint makes the edge invalid so it is dropped — the grey
	// node is left stranded rather than emitting an edge the validator rejects.
	if !gammaKind(src) || !gammaKind(tgt) {
		return false
	}
	switch rel {
	case model.LeadsTo:
		return src == model.Event && tgt == model.Event
	case model.PartOf:
		if src == model.Concept || tgt == model.Concept {
			return false
		}
		return !(src == model.Event && tgt == model.Thing)
	case model.Expresses:
		if src == model.Event && tgt == model.Event {
			return true
		}
		return tgt == model.Concept
	case model.NearTo:
		return src == tgt
	}
	return false
}

// effType returns a node's effective kind for type-pair checks: an Unresolved
// node is treated as its primary candidate (Candidates[0]); decided nodes use
// their Type. Same primary substitution that layout and rendering use.
func (r *Result) effType(i int) model.NodeType {
	n := r.Nodes[i]
	if n.Type == model.Unresolved && len(n.Candidates) > 0 {
		return n.Candidates[0]
	}
	return n.Type
}

// orderCandidates is arc-consistency over the γ(3,4) table: it prunes each
// Unresolved node's candidate list to the kinds that keep every incident edge a
// valid triangle, where the *neighbour* is taken at its current domain — its
// decided type, or its candidate set if still grey (see candidateValidates). A
// type constraint can eliminate a candidate outright — e.g. an Expresses *target*
// can never be a Thing (only eXe/eXc/tXc/cXc are legal), so a node fed by
// `… --X-->` drops its Thing option. When exactly one kind survives the node is
// decided; when none survives it is left as-is and its unsatisfiable edges are
// dropped by dropInvalidEdges. Iterates to a fixpoint (deciding or pruning one
// node can constrain a neighbour).
func (r *Result) orderCandidates() {
	// Invariant: candidates belong only to Unresolved nodes. Clear any stale
	// candidate set on an already-decided node before pruning the rest. In the
	// current single-call pipeline solveCore seeds every node Unresolved right
	// before this, so no node is decided yet and this loop is a no-op; it is kept
	// purely as a defensive guard so orderCandidates stays correct if it is ever
	// re-invoked on a partially-decided graph.
	for i := range r.Nodes {
		if r.Nodes[i].Type != model.Unresolved {
			if len(r.Nodes[i].Candidates) > 0 {
				r.trace(TraceRecord{Phase: "order", Event: "clear-stale", Producer: "orderCandidates", Node: r.Nodes[i].Origin,
					Detail: "cleared stale candidate set on an already-resolved node"})
			}
			r.Nodes[i].Candidates = nil
		}
	}
	inc := make([][]int, len(r.Nodes)) // node index -> incident edge indices
	for ei, e := range r.Edges {
		inc[e.Src] = append(inc[e.Src], ei)
		inc[e.Tgt] = append(inc[e.Tgt], ei)
	}
	maxPasses := len(r.Nodes) + 2 // fixpoint: a node decides at most once
	for range maxPasses {
		changed := false
		for i := range r.Nodes {
			n := &r.Nodes[i]
			if n.Type != model.Unresolved || len(n.Candidates) == 0 {
				continue
			}
			valid := n.Candidates[:0:0] // type-consistent kinds, likelihood order kept
			for _, cand := range n.Candidates {
				if r.candidateValidates(i, cand, inc) {
					valid = append(valid, cand)
				}
			}
			switch len(valid) {
			case 0:
				// Unsatisfiable: no kind keeps the edges valid. Leave as-is;
				// dropInvalidEdges removes the offending edge(s).
				r.trace(TraceRecord{Phase: "order", Event: "unsatisfiable", Producer: "orderCandidates", Node: n.Origin,
					Detail: fmt.Sprintf("no candidate keeps %q's edges valid; offending edge(s) will be dropped", n.Name)})
			case 1:
				// Only one kind is type-consistent → the node is decided.
				r.trace(TraceRecord{Phase: "order", Event: "decide", Producer: "orderCandidates", Node: n.Origin,
					Detail:  fmt.Sprintf("pruned to a single γ(3,4)-valid kind: %s", valid[0]),
					Payload: map[string]any{"type": string(valid[0])}})
				n.Type, n.Candidates, n.Confidence = valid[0], nil, High
				changed = true
			default:
				if len(valid) != len(n.Candidates) {
					r.trace(TraceRecord{Phase: "order", Event: "prune", Producer: "orderCandidates", Node: n.Origin,
						Detail:  fmt.Sprintf("pruned candidates to %v", kindStrings(valid)),
						Payload: map[string]any{"type": string(model.Unresolved), "candidates": kindStrings(valid)}})
					n.Candidates = valid
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
}

// candidateValidates reports whether assigning kind cand to node i keeps every
// incident edge arc-consistent: for each edge, *some* kind in the OTHER
// endpoint's current domain (its decided Type, or its candidate set if still
// grey) forms a valid γ(3,4) pair. Trusting neighbour *domains* (not only decided
// neighbours) is what makes this true arc-consistency — a type constraint
// propagates through still-grey nodes, e.g. an Expresses target loses its Thing
// candidate even while its source is undecided.
func (r *Result) candidateValidates(i int, cand model.NodeType, inc [][]int) bool {
	for _, ei := range inc[i] {
		e := r.Edges[ei]
		if e.Src == e.Tgt {
			continue // self-loop; deduped separately
		}
		other := e.Tgt
		if e.Tgt == i {
			other = e.Src
		}
		supported := false
		for _, ok := range r.domainOf(other) {
			src, tgt := cand, ok
			if e.Tgt == i {
				src, tgt = ok, cand
			}
			if typePairValid(e.Link, src, tgt) {
				supported = true
				break
			}
		}
		if !supported {
			return false
		}
	}
	return true
}

// domainOf returns a node's current kind domain: its candidate set while still
// Unresolved, or the singleton of its decided Type once resolved.
func (r *Result) domainOf(i int) []model.NodeType {
	if r.Nodes[i].Type == model.Unresolved {
		return r.Nodes[i].Candidates
	}
	return []model.NodeType{r.Nodes[i].Type}
}

// assignPrimaries chooses one globally type-consistent kind for each still-grey
// node and rotates it to the front of the node's candidate list, so effType
// (Candidates[0]) yields a satisfiable assignment — emitted edges validate and
// layout uses a coherent kind. Decided nodes are fixed. If no full assignment
// exists (genuinely over-constrained), the canonical order is left untouched and
// dropInvalidEdges removes the offending edges.
func (r *Result) assignPrimaries() {
	var grey []int
	for i := range r.Nodes {
		if r.Nodes[i].Type == model.Unresolved && len(r.Nodes[i].Candidates) > 0 {
			grey = append(grey, i)
		}
	}
	if len(grey) == 0 {
		return
	}
	inc := make([][]int, len(r.Nodes))
	exprTarget := make([]bool, len(r.Nodes))
	for ei, e := range r.Edges {
		inc[e.Src] = append(inc[e.Src], ei)
		inc[e.Tgt] = append(inc[e.Tgt], ei)
		if e.Link == model.Expresses {
			exprTarget[e.Tgt] = true
		}
	}
	// preferKind is the role-based default for a grey node. Only two kinds are ever
	// preferred: an Expresses TARGET is a Concept (the one positive concept signal —
	// an Expresses target can never be a Thing), and everything else is a Thing.
	// Event is NEVER a default; it is only ever FORCED by a LeadsTo edge (eLe),
	// because an event is dynamic and only makes sense with a causal/leads-to link.
	preferKind := func(i int) model.NodeType {
		if exprTarget[i] {
			return model.Concept
		}
		return model.Thing
	}
	// tryOrder lists a node's candidates with its preferred kind first, so the
	// backtracking picks the preferred kind as the primary whenever that is globally
	// consistent (the rest follow in canonical order). This makes a grey node render
	// preferred-first, e.g. an Expresses target as `::?ce`, without deciding it.
	tryOrder := func(i int) []model.NodeType {
		order := make([]model.NodeType, 0, len(r.Nodes[i].Candidates))
		seen := map[model.NodeType]bool{}
		push := func(k model.NodeType) {
			if !seen[k] && slices.Contains(r.Nodes[i].Candidates, k) {
				order = append(order, k)
				seen[k] = true
			}
		}
		push(preferKind(i))
		for _, k := range []model.NodeType{model.Event, model.Thing, model.Concept} {
			push(k)
		}
		return order
	}
	pick := map[int]model.NodeType{}
	// consistent reports whether kind k at grey node i conflicts with any already
	// assigned (pick) or decided neighbour. Unassigned grey neighbours are checked
	// when they get their turn.
	consistent := func(i int, k model.NodeType) bool {
		for _, ei := range inc[i] {
			e := r.Edges[ei]
			if e.Src == e.Tgt {
				continue
			}
			other := e.Tgt
			if e.Tgt == i {
				other = e.Src
			}
			var ok model.NodeType
			if k2, has := pick[other]; has {
				ok = k2
			} else if r.Nodes[other].Type != model.Unresolved {
				ok = r.Nodes[other].Type
			} else {
				continue // neighbour not yet pinned
			}
			src, tgt := k, ok
			if e.Tgt == i {
				src, tgt = ok, k
			}
			if !typePairValid(e.Link, src, tgt) {
				return false
			}
		}
		return true
	}
	var bt func(pos int) bool
	bt = func(pos int) bool {
		if pos == len(grey) {
			return true
		}
		i := grey[pos]
		for _, k := range tryOrder(i) {
			if consistent(i, k) {
				pick[i] = k
				if bt(pos + 1) {
					return true
				}
				delete(pick, i)
			}
		}
		return false
	}
	if !bt(0) {
		return
	}
	for _, i := range grey {
		front := pick[i]
		reordered := []model.NodeType{front}
		for _, c := range r.Nodes[i].Candidates {
			if c != front {
				reordered = append(reordered, c)
			}
		}
		r.Nodes[i].Candidates = reordered
	}
}

// dropInvalidEdges is the final safety net: any edge that still violates the
// γ(3,4) type-pair table under each endpoint's effective type (primary
// candidate for Unresolved nodes) is dropped with a warning, guaranteeing the
// output passes ipm-validate IPMV1.3. After orderCandidates this only fires for
// genuinely-unsatisfiable nodes.
func (r *Result) dropInvalidEdges() {
	out := r.Edges[:0:0]
	for _, e := range r.Edges {
		if typePairValid(e.Link, r.effType(e.Src), r.effType(e.Tgt)) {
			out = append(out, e)
			continue
		}
		r.warn(TraceRecord{Phase: "drop", Event: "invalid-edge", Producer: "dropInvalidEdges",
			Edge: edgeKey(r.Nodes[e.Src].Name, r.Nodes[e.Tgt].Name),
			Detail: fmt.Sprintf("dropped invalid %s edge %q(%s) -> %q(%s)",
				e.Link, r.Nodes[e.Src].Name, r.Nodes[e.Src].Type, r.Nodes[e.Tgt].Name, r.Nodes[e.Tgt].Type),
			Payload: map[string]any{"link": string(e.Link), "src": string(r.effType(e.Src)), "tgt": string(r.effType(e.Tgt))}})
	}
	r.Edges = out
}

// dedupe drops self-loops (IPMV1.1) and keeps at most one edge per unordered
// node pair (IPMV1.2) — the first edge seen for a pair wins.
func (r *Result) dedupe() {
	type pair struct{ a, b int }
	seen := map[pair]bool{}
	out := r.Edges[:0:0]
	for _, e := range r.Edges {
		if e.Src == e.Tgt {
			r.warn(TraceRecord{Phase: "dedupe", Event: "self-loop", Producer: "dedupe",
				Edge:   edgeKey(r.Nodes[e.Src].Name, r.Nodes[e.Tgt].Name),
				Detail: fmt.Sprintf("dropped self-loop on %q", r.Nodes[e.Src].Name)})
			continue
		}
		a, b := e.Src, e.Tgt
		if a > b {
			a, b = b, a
		}
		key := pair{a, b}
		if seen[key] {
			r.warn(TraceRecord{Phase: "dedupe", Event: "duplicate", Producer: "dedupe",
				Edge:   edgeKey(r.Nodes[e.Src].Name, r.Nodes[e.Tgt].Name),
				Detail: fmt.Sprintf("dropped duplicate edge %q—%q", r.Nodes[e.Src].Name, r.Nodes[e.Tgt].Name)})
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	r.Edges = out
}
