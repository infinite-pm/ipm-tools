package layout7

import (
	"fmt"
	"sort"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// Kind is a node's layout kind. Unresolved input nodes act as their primary
// candidate (Candidates[0]) for every layout decision but keep their input
// type in the emitted graph.
type Kind int

const (
	KindEvent Kind = iota
	KindThing
	KindConcept
)

// Rel is an edge's relation kind. The kind hierarchy L > P > X > N governs
// membership (v7P1), layer order (v7P5) and visibility (v7P9) alike — the
// extracted pattern of the spec's decision log.
type Rel int

const (
	RelLeadsTo Rel = iota
	RelPartOf
	RelExpresses
	RelNearTo
)

// node is the engine's working node. Geometry fields are filled by the place
// stage; everything before works with relative structure only (the spec's
// "positions are the LAST step").
type node struct {
	idx   int // index into graph.nodes — the engine-wide handle
	id    int // parser node ID (emitted as the output node ID)
	name  string
	alias string
	kind  Kind
	// emitType/emitCandidates preserve the input typing for output
	// ("unresolved" nodes keep their candidates).
	emitType       model.NodeType
	emitCandidates []model.NodeType
	tooltip        string

	w, h int // v7P8 sizing (size.go)
	x, y int // absolute position, set by place/assemble

	comp     int  // component index (v7P1), set by membership
	placed   bool // true once x/y are meaningful
	boundary bool // synthesized S/E node (v7P1: every event component gets its own)
	pureGen  bool // member of a layered-generation row (v7P4) — its row is structural
}

// edge is the engine's working edge.
type edge struct {
	idx      int // index into graph.edges (declaration order)
	from, to int // node idx
	rel      Rel
	undir    bool

	// Classification (membership.go). Exactly one of these is the edge's
	// job; every edge is still DRAWN unless routing hides it (v7P9).
	structural bool // primary placing edge — defines skeleton/group shape (v7P1/P7)
	demotedTie bool // placing edge demoted by anchor-and-tie (v7P1/P7)
	sameTie    bool // tNt/cNc — draw / order / onion (v7P5)
	nonPlacing bool // eXe, eNe — places nothing (v7P1)
}

// graph is the normalized input plus everything the stages derive.
type graph struct {
	nodes []*node
	edges []*edge

	// opts are the caller's engine options (generate.go). Zero value = the
	// plain flat layout.
	opts Options

	// adjacency by relation, from the normalize step
	out map[int][]*edge // edges with from == idx
	in  map[int][]*edge // edges with to == idx

	// sExtra/eExtra grow a component's S/E boundary gap on the demand
	// loop's second pass (v7P8 §4, first slice: a
	// corridor lane sat too close to the S→start arrowhead — the arrow
	// side needs 1.5× the visible gap, so the boundary edge lengthens).
	sExtra, eExtra map[int]int
	// rowExtra grows the row gap BELOW an event (keyed by the event) on
	// the demand pass: a stranded sole leaf posts it so the successor's
	// part-of diagonal drops under the leaf's near-anchor wedge (v7P8
	// growth: "swap of clothing can be closer ... so we
	// can avoid edges crossing")
	rowExtra map[int]int

	comps []*component

	// rowMates: sub-grid ROW-MATES (v7P3 rank rows, skeleton.go) — a
	// border-run along a row-mate is never exempt from the graze cost,
	// so a branch's part-of goes AROUND the row instead of hugging it.
	rowMates map[int]map[int]bool

	// trace: the optional decision trace (trace.go). Nil in normal runs.
	trace Trace

	// spanCache holds subtree spans during one component's lane pass
	// (skeleton.go). Per-graph, NOT package-level: Generate may run
	// concurrently (e.g. parallel ipm.embedBuffer LSP requests) and a
	// shared map is a fatal concurrent read/write.
	spanCache map[int][2]int
}

// component is a v7P1 component: one event structure plus the aux anchored
// to it (or a pure aux structure with no events).
type component struct {
	idx    int
	events []int // node idx, declaration order
	aux    []int // things+concepts anchored here (incl. onion satellites)

	// boundary nodes (S/E), created by the skeleton stage for event
	// components; -1 when absent.
	sNode, eNode int

	// ties reaching OTHER components (v7P1 "cross-component ties"),
	// counted for v7P2 centrality.
	crossTies int

	// bbox in component-local coordinates, set by place.
	minX, minY, maxX, maxY int
}

func (c *component) declaredNodes() int { return len(c.events) + len(c.aux) }

// kindOf resolves a model node's layout kind: Unresolved nodes act as their
// primary candidate (the parser/solver orders Candidates most-likely-first).
func kindOf(n model.Node) (Kind, error) {
	t := n.Type
	if t == model.Unresolved && len(n.Candidates) > 0 {
		t = n.Candidates[0]
	}
	switch t {
	case model.Event:
		return KindEvent, nil
	case model.Thing:
		return KindThing, nil
	case model.Concept:
		return KindConcept, nil
	}
	return KindThing, fmt.Errorf("node %q: unsupported kind %q", n.Name, t)
}

func relOf(t model.SstLinkType) (Rel, error) {
	switch t {
	case model.LeadsTo:
		return RelLeadsTo, nil
	case model.PartOf:
		return RelPartOf, nil
	case model.Expresses:
		return RelExpresses, nil
	case model.NearTo:
		return RelNearTo, nil
	}
	return RelNearTo, fmt.Errorf("unsupported link type %q", t)
}

// normalize converts the parser model into the engine's working graph.
// Node order (and so every declaration-order tiebreak in v7P1/P3/P4/P7)
// follows the parser's node IDs, which record declaration order.
func normalize(doc *model.IpmGraph) (*graph, error) {
	g := &graph{
		out:      map[int][]*edge{},
		in:       map[int][]*edge{},
		sExtra:   map[int]int{},
		eExtra:   map[int]int{},
		rowExtra: map[int]int{},
	}

	ordered := make([]model.Node, len(doc.Nodes))
	copy(ordered, doc.Nodes)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })

	byID := map[int]int{} // parser ID -> node idx
	for _, mn := range ordered {
		k, err := kindOf(mn)
		if err != nil {
			return nil, err
		}
		n := &node{
			idx:            len(g.nodes),
			id:             mn.ID,
			name:           mn.Name,
			alias:          mn.Alias,
			kind:           k,
			emitType:       mn.Type,
			emitCandidates: mn.Candidates,
			tooltip:        mn.Tooltip,
			comp:           -1,
		}
		n.w, n.h = computeSize(mn.Name) // v7P8
		byID[mn.ID] = n.idx
		g.nodes = append(g.nodes, n)
	}

	edges := make([]model.Edge, len(doc.Edges))
	copy(edges, doc.Edges)
	sort.SliceStable(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	for _, me := range edges {
		r, err := relOf(me.SstLinkType)
		if err != nil {
			return nil, err
		}
		from, ok := byID[me.Source]
		if !ok {
			return nil, fmt.Errorf("edge %d: unknown source %d", me.ID, me.Source)
		}
		to, ok := byID[me.Target]
		if !ok {
			return nil, fmt.Errorf("edge %d: unknown target %d", me.ID, me.Target)
		}
		e := &edge{
			idx:   len(g.edges),
			from:  from,
			to:    to,
			rel:   r,
			undir: me.Dir == model.DirUndir,
		}
		g.edges = append(g.edges, e)
		g.out[from] = append(g.out[from], e)
		g.in[to] = append(g.in[to], e)
	}
	return g, nil
}

// isPlacing reports whether an edge is a PLACING relation per v7P1's terms:
// leads-to, part-of, and expresses whose target is a concept. near-to places
// nothing anywhere; expresses BETWEEN events (eXe) places nothing.
func (g *graph) isPlacing(e *edge) bool {
	switch e.rel {
	case RelLeadsTo, RelPartOf:
		return true
	case RelExpresses:
		return g.nodes[e.to].kind == KindConcept
	}
	return false
}

// isEventConnector reports whether the edge attaches aux directly to an
// event — v7P4's two CONNECTORS: tPe (thing part-of event) and eXc (event
// expresses concept).
func (g *graph) isEventConnector(e *edge) bool {
	f, t := g.nodes[e.from], g.nodes[e.to]
	if e.rel == RelPartOf && f.kind == KindThing && t.kind == KindEvent {
		return true // tPe — the only part-of into an event besides ePe
	}
	if e.rel == RelExpresses && f.kind == KindEvent && t.kind == KindConcept {
		return true // eXc
	}
	return false
}

// absInt is the plain integer absolute value.
func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
