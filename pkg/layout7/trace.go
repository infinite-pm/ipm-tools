package layout7

import (
	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"strings"
)

// Trace receives structured events during GenerateTraced. It is the
// engine's ONLY debug surface (docs/dev/layout-gen/layout-debug.md):
// all narration lives outside (pkg/l7report); the engine just states
// facts. A nil trace costs one pointer check per site; a `-tags
// l7notrace` build compiles the sites away entirely.
type Trace interface {
	Emit(e TraceEvent)
}

// TraceAvailable reports whether this build carries the trace emit
// sites (false under `-tags l7notrace`) — consumers like pkg/l7report
// check it to fail loudly instead of returning an empty report.
const TraceAvailable = traceEnabled

// tracing gates every emit site: compile-time false under l7notrace
// (the branch and its payload construction are removed), a nil check
// otherwise.
func (g *graph) tracing() bool { return traceEnabled && g.trace != nil }

// TraceEvent is one engine decision or snapshot. Data holds small,
// stage-specific payloads with STABLE keys — consumers grep and diff
// these; keys are API.
type TraceEvent struct {
	Stage string // membership | groups | skeleton | floors | pull | place | assemble | route
	Kind  string // component | election | anchor | satellite | unanchored | demote |
	// band | subrows | positions | candidate | chosen | stubbed | tile | tile-candidate
	Data map[string]any
}

// GenerateTraced is Generate with a decision trace. Generate(doc) ==
// GenerateTraced(doc, nil).
func GenerateTraced(doc *model.IpmGraph, t Trace) (*layout.Graph, error) {
	return GenerateTracedWithOptions(doc, Options{}, t)
}

// GenerateTracedWithOptions is GenerateWithOptions with a decision trace —
// the zoom canvas runs the engine with Containers on (and on a lifted graph),
// and its decisions could not be narrated by --why before this.
func GenerateTracedWithOptions(doc *model.IpmGraph, opts Options, t Trace) (*layout.Graph, error) {
	g, err := normalize(doc)
	if err != nil {
		return nil, err
	}
	g.opts = opts
	if opts.Shells {
		g.opts.Containers = true
	}
	g.trace = t
	m := g.resolveMembership()
	if g.tracing() {
		g.emitMembership(m)
	}
	gp := g.buildGroups(m)
	if g.tracing() {
		g.emitGroups(gp)
	}
	sp := g.buildSkeleton(gp)
	if g.tracing() {
		g.emitRankRows(sp)
		g.emitSubRows(sp)
	}
	g.addShellNodes(sp)
	g.place(m, gp, sp)
	if g.tracing() {
		g.emitPositions("place", -1)
	}
	g.assemble()
	if g.tracing() {
		g.emitPositions("assemble", -1)
	}
	routes := g.route()
	g.stubCorridorDemands(routes)
	if len(g.sExtra)+len(g.eExtra)+len(g.rowExtra) > 0 { // v7P8 §4 demand re-solve
		g.place(m, gp, sp)
		g.assemble()
		routes = g.route()
	}
	if g.tracing() {
		g.emitRoutes(routes)
	}
	return g.emit(routes), nil
}

// traceName resolves a node for event payloads: the authored alias wins
// (the #name the rule DSL addresses), then the label.
func (g *graph) traceName(i int) string {
	n := g.nodes[i]
	if n.alias != "" {
		return n.alias
	}
	return n.name
}

func (g *graph) traceKind(i int) string {
	if g.nodes[i].boundary {
		return "boundary"
	}
	switch g.nodes[i].kind {
	case KindEvent:
		return "event"
	case KindThing:
		return "thing"
	}
	return "concept"
}

// emitElection reports an anchor election (v7P7): among an aux node's
// candidate placing edges, which event USER won the primary and WHY —
// DEPTH (strictly part-most: md, then event nesting) or DECLARATION order
// (a depth tie, earliest-declared wins). Only a real contest (≥2
// candidates) is emitted; the winner-vs-loser test reuses the same
// `deeper` relation the election itself decides on, so the reported rule
// cannot disagree with the placement. Payload keys are the grep/diff API.
func (g *graph) emitElection(node int, best *edge, cands []*edge, deeper func(a, b int) bool) {
	if len(cands) < 2 {
		return
	}
	winner := g.userOf(node, best)
	losers := make([]string, 0, len(cands)-1)
	rule := "depth"
	for _, e := range cands {
		if e == best {
			continue
		}
		u := g.userOf(node, e)
		losers = append(losers, g.traceName(u))
		if !deeper(winner, u) {
			// the loser is not strictly shallower — they tied on depth and
			// declaration order broke it.
			rule = "declaration"
		}
	}
	g.trace.Emit(TraceEvent{Stage: "membership", Kind: "election", Data: map[string]any{
		"node": g.traceName(node), "winner": g.traceName(winner),
		"losers": losers, "rule": rule,
	}})
}

func (g *graph) emitMembership(m *membership) {
	for ci, c := range g.comps {
		evs := make([]string, 0, len(c.events))
		for _, i := range c.events {
			evs = append(evs, g.traceName(i))
		}
		aux := make([]string, 0, len(c.aux))
		for _, i := range c.aux {
			aux = append(aux, g.traceName(i))
		}
		g.trace.Emit(TraceEvent{Stage: "membership", Kind: "component", Data: map[string]any{
			"index": ci, "events": evs, "aux": aux, "crossTies": c.crossTies,
		}})
	}
	for i, n := range g.nodes {
		if n.kind == KindEvent || n.boundary {
			continue
		}
		a := m.anchors[i]
		switch {
		case a.satelliteOf != nil:
			partner := a.satelliteOf.from
			if partner == i {
				partner = a.satelliteOf.to
			}
			g.trace.Emit(TraceEvent{Stage: "membership", Kind: "satellite", Data: map[string]any{
				"node": g.traceName(i), "nodeKind": g.traceKind(i), "partner": g.traceName(partner),
			}})
		case a.primary != nil:
			g.trace.Emit(TraceEvent{Stage: "membership", Kind: "anchor", Data: map[string]any{
				"node": g.traceName(i), "nodeKind": g.traceKind(i),
				"from": g.traceName(a.primary.from), "to": g.traceName(a.primary.to),
			}})
		default:
			g.trace.Emit(TraceEvent{Stage: "membership", Kind: "unanchored", Data: map[string]any{
				"node": g.traceName(i), "nodeKind": g.traceKind(i),
			}})
		}
	}
	for _, e := range g.edges {
		if e.demotedTie {
			g.trace.Emit(TraceEvent{Stage: "membership", Kind: "demote", Data: map[string]any{
				"from": g.traceName(e.from), "to": g.traceName(e.to), "rel": emitBase(e.rel),
			}})
		}
	}
}

// emitGroups reports each aux node's band assignment (v7P4/P5): which
// anchor it rides, on which side, at what offset. The side is derived
// from the relative box geometry — the same fact a reader takes from
// the render, stated before placement makes it absolute.
func (g *graph) emitGroups(gp *groupsPlan) {
	for i := range g.nodes {
		r, ok := gp.rel[i]
		if !ok || r.event == i {
			continue
		}
		n, a := g.nodes[i], g.nodes[r.event]
		side := "beside"
		switch {
		case r.dx+n.w <= 0:
			side = "left"
		case r.dx >= a.w:
			side = "right"
		case r.dy+n.h <= 0:
			side = "above"
		case r.dy >= a.h:
			side = "below"
		}
		g.trace.Emit(TraceEvent{Stage: "groups", Kind: "band", Data: map[string]any{
			"node": g.traceName(i), "nodeKind": g.traceKind(i),
			"anchor": g.traceName(r.event), "side": side, "dx": r.dx, "dy": r.dy,
		}})
	}
}

// emitRankRows reports the TOP-LEVEL rank rows of every component (v7P3:
// leads-to runs down) with each event's flow predecessors and the sub-event a
// successor is laned under — the question "why is this event on that row"
// had no answer in --why before; only sub-rows were shown.
func (g *graph) emitRankRows(sp *skeletonPlan) {
	succ, pred, _, _, viaSub, _ := g.topLevelFlow()
	_ = succ
	for ci, rows := range sp.rows {
		if len(rows) == 0 {
			continue
		}
		rr := make([][]string, 0, len(rows))
		for _, row := range rows {
			if len(row) == 0 {
				continue // a component without top-level events; and an
				// empty row would not survive the recording round trip
				// (the shape-typed decoder cannot type an empty slice)
			}
			names := make([]string, 0, len(row))
			for _, ev := range row {
				name := g.traceName(ev)
				if ps := pred[ev]; len(ps) > 0 {
					pn := make([]string, 0, len(ps))
					for _, p := range ps {
						pn = append(pn, g.traceName(p))
					}
					name += " <- " + strings.Join(pn, ",")
				}
				if s, ok := viaSub[ev]; ok {
					name += " (under " + g.traceName(s) + ")"
				}
				names = append(names, name)
			}
			rr = append(rr, names)
		}
		if len(rr) == 0 {
			continue
		}
		g.trace.Emit(TraceEvent{Stage: "skeleton", Kind: "rows", Data: map[string]any{
			"comp": ci, "rows": rr,
		}})
	}
}

func (g *graph) emitSubRows(sp *skeletonPlan) {
	// deterministic order: by parent node index
	for parent := range g.nodes {
		rows := sp.subRows[parent]
		if len(rows) == 0 {
			continue
		}
		rr := make([][]string, 0, len(rows))
		for _, row := range rows {
			names := make([]string, 0, len(row))
			for _, s := range row {
				names = append(names, g.traceName(s))
			}
			rr = append(rr, names)
		}
		g.trace.Emit(TraceEvent{Stage: "skeleton", Kind: "subrows", Data: map[string]any{
			"parent": g.traceName(parent), "rows": rr,
		}})
	}
}

// emitPositions snapshots placed nodes after a pipeline stage — the
// trajectory a movement table diffs. comp restricts the snapshot to one
// component (the intra-place passes run per component); -1 takes all.
func (g *graph) emitPositions(afterStage string, comp int) {
	for _, n := range g.nodes {
		if !n.placed || (comp >= 0 && n.comp != comp) {
			continue
		}
		g.trace.Emit(TraceEvent{Stage: afterStage, Kind: "positions", Data: map[string]any{
			"node": g.traceName(n.idx), "x": n.x, "y": n.y, "w": n.w, "h": n.h,
		}})
	}
}

func (g *graph) emitRoutes(routes []routed) {
	for _, e := range g.edges {
		r := routes[e.idx]
		kind := "chosen"
		if r.stubbed {
			kind = "stubbed"
		}
		g.trace.Emit(TraceEvent{Stage: "route", Kind: kind, Data: map[string]any{
			"from": g.traceName(e.from), "to": g.traceName(e.to), "rel": emitBase(e.rel),
			"srcSide": r.src.Side, "srcPos": r.src.Position,
			"tgtSide": r.tgt.Side, "tgtPos": r.tgt.Position,
			"bends": len(r.bends),
		}})
	}
}

// emitCandidate reports one route candidate's score breakdown from
// inside the routing pass — the piece a post-hoc reading cannot recover
// (docs/dev/layout-gen/layout-debug.md: the AI reasons about the budget arithmetic).
func (g *graph) emitCandidate(e *edge, pass string, idx int, r routed,
	cross, graze, detour float64, hit, chosen bool) {
	g.trace.Emit(TraceEvent{Stage: "route", Kind: "candidate", Data: map[string]any{
		"pass": pass, "from": g.traceName(e.from), "to": g.traceName(e.to),
		"cand": idx, "srcSide": r.src.Side, "tgtSide": r.tgt.Side, "bends": len(r.bends),
		"cross": cross, "graze": graze, "detour": detour, "hit": hit, "chosen": chosen,
	}})
}
