package layoutdiff

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layoutcheck"
)

// Tier ranks a change by what it costs a reader. LOWER IS WORSE, so that a
// sort by (tier, -score) puts the diagram most worth looking at first.
type Tier int

const (
	// TierInvariant: the change made a universal invariant worse — a node
	// overlap, an edge through a box, a new crossing. Always looked at.
	TierInvariant Tier = 1
	// TierStructural: the same picture, drawn differently — an edge changed
	// side, a node appeared, an edge became a stub. Reads differently.
	TierStructural Tier = 2
	// TierGeometry: the same picture, moved — a node stepped, a port slid,
	// the canvas grew. Usually the intended cost of a fix.
	TierGeometry Tier = 3
	// TierNone: no difference at all.
	TierNone Tier = 9
)

// String renders the tier as the one-word label the reports use.
func (t Tier) String() string {
	switch t {
	case TierInvariant:
		return "invariant"
	case TierStructural:
		return "structural"
	case TierGeometry:
		return "geometry"
	}
	return "identical"
}

// Change kinds. Stable strings: they are grouped, counted and filtered on,
// which makes them API for every consumer (report rows, --json, tests).
const (
	KindFindingAdded   = "finding-added"
	KindFindingFixed   = "finding-fixed"
	KindNodeAdded      = "node-added"
	KindNodeRemoved    = "node-removed"
	KindEdgeAdded      = "edge-added"
	KindEdgeRemoved    = "edge-removed"
	KindVisibility     = "edge-visibility"
	KindDeferred       = "edge-deferred"
	KindPortSide       = "port-side"
	KindPortSlide      = "port-slide"
	KindBendCount      = "bend-count"
	KindBendMoved      = "bend-moved"
	KindNodeMoved      = "node-moved"
	KindNodeResized    = "node-resized"
	KindBoundsChanged  = "bounds-changed"
	KindRelabelled     = "node-relabelled"
	KindContainerShell = "container-shell"
)

// Box is a rectangle in layout coordinates, used by the overlay to draw a
// ghost of where something WAS.
type Box struct {
	X, Y, W, H int
}

// Change is one classified difference. Ref/Detail speak the same vocabulary
// as the fixture rule DSL and `layout-debug --facts` ("#3", "edge #3,#10",
// "target-side=left"), so a change can be pasted into a pin or grepped for
// with the same words used everywhere else.
type Change struct {
	Kind   string  `json:"kind"`
	Tier   Tier    `json:"tier"`
	Ref    string  `json:"ref"`              // "#3" | "edge #3,#10"
	Label  string  `json:"label,omitempty"`  // "humans" | "humans→Life"
	Detail string  `json:"detail,omitempty"` // "bottom→left", "dx=+20 dy=-40"
	Weight float64 `json:"weight"`

	// Geometry for the overlay; all in NEW-graph coordinates (an old box is
	// shifted by the canvas translation first, so the two are comparable).
	Old     *Box              `json:"-"`
	New     *Box              `json:"-"`
	OldPath []layout.Position `json:"-"`
	NewPath []layout.Position `json:"-"`
	At      *layout.Position  `json:"-"`
	Boxes   []Box             `json:"-"` // finding-derived boxes
}

// Report is the whole comparison of one diagram.
type Report struct {
	Tier    Tier     `json:"tier"`
	Score   float64  `json:"score"`
	Changes []Change `json:"changes"`

	Counts map[string]int `json:"counts"`

	// TranslationX/Y is the whole-canvas shift removed before measuring
	// movement (see translation()). Reported so a reader can tell "the
	// canvas grew" from "this node walked".
	TranslationX int `json:"translationX"`
	TranslationY int `json:"translationY"`

	OldBounds layout.Bounds `json:"oldBounds"`
	NewBounds layout.Bounds `json:"newBounds"`

	FindingsAdded []string `json:"findingsAdded,omitempty"`
	FindingsFixed []string `json:"findingsFixed,omitempty"`
	OldFindings   int      `json:"oldFindings"`
	NewFindings   int      `json:"newFindings"`
}

// Identical reports whether the two graphs are the same layout.
func (r Report) Identical() bool { return len(r.Changes) == 0 }

// Options tune the comparison.
type Options struct {
	// KeepCarried reports nodes that moved by exactly the canvas
	// translation. Off by default: they did not move relative to the
	// diagram, and reporting them buries the ones that did.
	KeepCarried bool
	// SkipFindings omits the invariant pass (the expensive half on a large
	// sweep, and pointless when only geometry is wanted).
	SkipFindings bool
}

// finding weights, by severity class. The classes are layoutcheck's own
// message prefixes; the ordering mirrors the ratchet's severity order
// (overlaps > through-node family > crossings > badges).
func findingWeight(f string) (float64, Tier) {
	switch {
	case strings.HasPrefix(f, "node overlap"):
		return 1000, TierInvariant
	case strings.HasPrefix(f, "edge through node"), strings.HasPrefix(f, "edges overlap"):
		return 600, TierInvariant
	case strings.HasPrefix(f, "edges cross"):
		return 300, TierInvariant
	case strings.HasPrefix(f, "badge on edge"), strings.HasPrefix(f, "badge overlap"),
		strings.HasPrefix(f, "stub on node"):
		return 250, TierInvariant
	case strings.HasPrefix(f, "edge grazes node"):
		return 200, TierInvariant
	case strings.HasPrefix(f, "reads as paired"):
		return 100, TierInvariant
	}
	return 150, TierInvariant
}

// Structural weights. Deliberately far apart from the geometry scale so a
// single side flip always outranks a page of nodes stepping 20px.
const (
	wNodeSet    = 300
	wEdgeSet    = 300
	wVisibility = 250
	wPortSide   = 200
	wBendCount  = 120
	wRelabel    = 150
	wContainer  = 200
	wBounds     = 5
	// Geometry is measured in GRID STEPS (layout.GridStep), so "moved one
	// grid step" costs 1 whatever the canvas size.
	gridStep = float64(layout.GridStep)
)

// Diff compares two layouts of the same source.
func Diff(oldG, newG *layout.Graph, opts Options) Report {
	rep := Report{Tier: TierNone, Counts: map[string]int{}}
	if oldG == nil || newG == nil {
		return rep
	}
	rep.OldBounds, rep.NewBounds = oldG.Meta.Bounds, newG.Meta.Bounds

	nodes := matchNodes(oldG, newG)
	tx, ty := translation(nodes)
	rep.TranslationX, rep.TranslationY = tx, ty

	add := func(c Change) {
		rep.Changes = append(rep.Changes, c)
		rep.Counts[c.Kind]++
		rep.Score += c.Weight
		if c.Tier < rep.Tier {
			rep.Tier = c.Tier
		}
	}

	// --- nodes ---------------------------------------------------------
	for _, p := range nodes {
		switch {
		case p.Old == nil:
			add(Change{Kind: KindNodeAdded, Tier: TierStructural, Ref: "#" + p.New.ID,
				Label: nodeLabel(p.New), Detail: p.New.Type, Weight: wNodeSet,
				New: boxOf(p.New)})
		case p.New == nil:
			add(Change{Kind: KindNodeRemoved, Tier: TierStructural, Ref: "#" + p.Old.ID,
				Label: nodeLabel(p.Old), Detail: p.Old.Type, Weight: wNodeSet,
				Old: shiftBox(boxOf(p.Old), tx, ty)})
		default:
			o, n := p.Old, p.New
			dx, dy := (n.X-o.X)-tx, (n.Y-o.Y)-ty
			if dx != 0 || dy != 0 {
				dist := math.Hypot(float64(dx), float64(dy))
				add(Change{Kind: KindNodeMoved, Tier: TierGeometry, Ref: "#" + n.ID,
					Label: nodeLabel(n), Detail: fmt.Sprintf("dx=%+d dy=%+d", dx, dy),
					Weight: dist / gridStep,
					Old:    shiftBox(boxOf(o), tx, ty), New: boxOf(n)})
			} else if opts.KeepCarried && (n.X != o.X || n.Y != o.Y) {
				add(Change{Kind: KindNodeMoved, Tier: TierGeometry, Ref: "#" + n.ID,
					Label: nodeLabel(n), Detail: "carried by the canvas shift", Weight: 0,
					Old: shiftBox(boxOf(o), tx, ty), New: boxOf(n)})
			}
			if n.Width != o.Width || n.Height != o.Height {
				dw, dh := n.Width-o.Width, n.Height-o.Height
				add(Change{Kind: KindNodeResized, Tier: TierGeometry, Ref: "#" + n.ID,
					Label:  nodeLabel(n),
					Detail: fmt.Sprintf("%dx%d → %dx%d", o.Width, o.Height, n.Width, n.Height),
					Weight: float64(abs(dw)+abs(dh)) / gridStep,
					Old:    shiftBox(boxOf(o), tx, ty), New: boxOf(n)})
			}
			if n.Label != o.Label {
				add(Change{Kind: KindRelabelled, Tier: TierStructural, Ref: "#" + n.ID,
					Detail: fmt.Sprintf("%q → %q", o.Label, n.Label), Weight: wRelabel,
					New: boxOf(n)})
			}
			if (o.Container == nil) != (n.Container == nil) {
				state := "shell dropped"
				if n.Container != nil {
					state = "shell added"
				}
				add(Change{Kind: KindContainerShell, Tier: TierStructural, Ref: "#" + n.ID,
					Label: nodeLabel(n), Detail: state, Weight: wContainer, New: boxOf(n)})
			}
		}
	}

	// --- edges ---------------------------------------------------------
	for _, p := range matchEdges(oldG, newG) {
		switch {
		case p.Old == nil:
			add(Change{Kind: KindEdgeAdded, Tier: TierStructural, Ref: edgeRef(p.New),
				Label: edgeLabel(newG, p.New), Detail: p.New.Base, Weight: wEdgeSet,
				NewPath: polyline(newG, p.New)})
		case p.New == nil:
			add(Change{Kind: KindEdgeRemoved, Tier: TierStructural, Ref: edgeRef(p.Old),
				Label: edgeLabel(oldG, p.Old), Detail: p.Old.Base, Weight: wEdgeSet,
				OldPath: shiftPath(polyline(oldG, p.Old), tx, ty)})
		default:
			diffEdge(oldG, newG, p.Old, p.New, tx, ty, add)
		}
	}

	if rep.OldBounds != rep.NewBounds {
		add(Change{Kind: KindBoundsChanged, Tier: TierGeometry, Ref: "canvas",
			Detail: fmt.Sprintf("%dx%d → %dx%d",
				rep.OldBounds.Width, rep.OldBounds.Height,
				rep.NewBounds.Width, rep.NewBounds.Height),
			Weight: wBounds})
	}

	// --- universal invariants -------------------------------------------
	if !opts.SkipFindings {
		oldF := layoutcheck.Findings(oldG, layoutcheck.Options{})
		newF := layoutcheck.Findings(newG, layoutcheck.Options{})
		rep.OldFindings, rep.NewFindings = len(oldF), len(newF)
		added, fixed := diffStrings(oldF, newF)
		rep.FindingsAdded, rep.FindingsFixed = added, fixed
		for _, f := range added {
			w, t := findingWeight(f)
			add(Change{Kind: KindFindingAdded, Tier: t, Ref: "check", Detail: f,
				Weight: w, Boxes: findingBoxes(f)})
		}
		for _, f := range fixed {
			// A fix is reported but never scored: an audit ranks by what
			// needs looking at, and a diagram that got BETTER does not
			// deserve to outrank one that got worse.
			add(Change{Kind: KindFindingFixed, Tier: TierGeometry, Ref: "check", Detail: f,
				Weight: 0, Boxes: findingBoxes(f)})
		}
	}

	sort.SliceStable(rep.Changes, func(i, j int) bool {
		if rep.Changes[i].Tier != rep.Changes[j].Tier {
			return rep.Changes[i].Tier < rep.Changes[j].Tier
		}
		return rep.Changes[i].Weight > rep.Changes[j].Weight
	})
	return rep
}

// diffEdge compares one matched edge pair.
func diffEdge(oldG, newG *layout.Graph, o, n *layout.Edge, tx, ty int, add func(Change)) {
	ref, label := edgeRef(n), edgeLabel(newG, n)
	newPath := polyline(newG, n)
	oldPath := shiftPath(polyline(oldG, o), tx, ty)

	if o.Visibility != n.Visibility {
		add(Change{Kind: KindVisibility, Tier: TierStructural, Ref: ref, Label: label,
			Detail: fmt.Sprintf("%s → %s", visLabel(o.Visibility), visLabel(n.Visibility)),
			Weight: wVisibility, NewPath: newPath, At: midpoint(newPath)})
	}
	if o.Deferred != n.Deferred {
		add(Change{Kind: KindDeferred, Tier: TierStructural, Ref: ref, Label: label,
			Detail: fmt.Sprintf("deferred %v → %v", o.Deferred, n.Deferred),
			Weight: wVisibility, NewPath: newPath, At: midpoint(newPath)})
	}
	if o.Route == nil || n.Route == nil {
		return
	}
	for _, end := range []struct {
		name   string
		o, n   layout.PortJSON
		anchor func([]layout.Position) *layout.Position
	}{
		{"source", o.Route.Source, n.Route.Source, firstPoint},
		{"target", o.Route.Target, n.Route.Target, lastPoint},
	} {
		switch {
		case end.o.Side != end.n.Side:
			add(Change{Kind: KindPortSide, Tier: TierStructural, Ref: ref, Label: label,
				Detail:  fmt.Sprintf("%s-side=%s (was %s)", end.name, end.n.Side, end.o.Side),
				Weight:  wPortSide,
				NewPath: newPath, OldPath: oldPath, At: end.anchor(newPath)})
		case end.o.Position != end.n.Position:
			// A slide along the same side: cost it in grid steps of the
			// distance the port actually travelled.
			add(Change{Kind: KindPortSlide, Tier: TierGeometry, Ref: ref, Label: label,
				Detail: fmt.Sprintf("%s-position=%.2f (was %.2f)", end.name, end.n.Position, end.o.Position),
				Weight: math.Abs(end.n.Position-end.o.Position) * 60 / gridStep,
				At:     end.anchor(newPath)})
		}
	}
	switch {
	case len(o.Route.Bends) != len(n.Route.Bends):
		add(Change{Kind: KindBendCount, Tier: TierStructural, Ref: ref, Label: label,
			Detail: fmt.Sprintf("bends %d → %d", len(o.Route.Bends), len(n.Route.Bends)),
			Weight: wBendCount, NewPath: newPath, OldPath: oldPath})
	case !samePositions(o.Route.Bends, n.Route.Bends, tx, ty):
		add(Change{Kind: KindBendMoved, Tier: TierGeometry, Ref: ref, Label: label,
			Detail:  fmt.Sprintf("%d bend(s) moved", len(n.Route.Bends)),
			Weight:  bendDrift(o.Route.Bends, n.Route.Bends, tx, ty) / gridStep,
			NewPath: newPath, OldPath: oldPath})
	}
}

// ---- helpers ---------------------------------------------------------------

func nodeLabel(n *layout.Node) string {
	if s := strings.TrimSpace(n.Label); s != "" {
		return s
	}
	if n.Alias != "" {
		return n.Alias
	}
	return n.Type + ":" + n.ID
}

func edgeRef(e *layout.Edge) string { return fmt.Sprintf("edge #%s,#%s", e.From, e.To) }

func edgeLabel(g *layout.Graph, e *layout.Edge) string {
	return labelOfID(g, e.From) + "→" + labelOfID(g, e.To)
}

func labelOfID(g *layout.Graph, id string) string {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return nodeLabel(&g.Nodes[i])
		}
	}
	return "#" + id
}

func visLabel(v string) string {
	if v == "" {
		return "full"
	}
	return v
}

func boxOf(n *layout.Node) *Box { return &Box{X: n.X, Y: n.Y, W: n.Width, H: n.Height} }

func shiftBox(b *Box, dx, dy int) *Box {
	if b == nil {
		return nil
	}
	return &Box{X: b.X + dx, Y: b.Y + dy, W: b.W, H: b.H}
}

func shiftPath(p []layout.Position, dx, dy int) []layout.Position {
	out := make([]layout.Position, len(p))
	for i, q := range p {
		out[i] = layout.Position{X: q.X + dx, Y: q.Y + dy}
	}
	return out
}

// polyline resolves an edge's drawn path: source port, bends, target port.
func polyline(g *layout.Graph, e *layout.Edge) []layout.Position {
	if e.Route == nil {
		return nil
	}
	var from, to layout.Node
	for i := range g.Nodes {
		if g.Nodes[i].ID == e.From {
			from = g.Nodes[i]
		}
		if g.Nodes[i].ID == e.To {
			to = g.Nodes[i]
		}
	}
	sx, sy := layout.EdgePortPoint(from, to, layout.EdgePort{Side: e.Route.Source.Side, Position: e.Route.Source.Position})
	tx, ty := layout.EdgePortPoint(to, from, layout.EdgePort{Side: e.Route.Target.Side, Position: e.Route.Target.Position})
	pts := []layout.Position{{X: sx, Y: sy}}
	pts = append(pts, e.Route.Bends...)
	return append(pts, layout.Position{X: tx, Y: ty})
}

func firstPoint(p []layout.Position) *layout.Position {
	if len(p) == 0 {
		return nil
	}
	q := p[0]
	return &q
}

func lastPoint(p []layout.Position) *layout.Position {
	if len(p) == 0 {
		return nil
	}
	q := p[len(p)-1]
	return &q
}

func midpoint(p []layout.Position) *layout.Position {
	if len(p) == 0 {
		return nil
	}
	q := p[len(p)/2]
	return &q
}

func samePositions(a, b []layout.Position, dx, dy int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].X+dx != b[i].X || a[i].Y+dy != b[i].Y {
			return false
		}
	}
	return true
}

func bendDrift(a, b []layout.Position, dx, dy int) float64 {
	total := 0.0
	for i := range a {
		if i >= len(b) {
			break
		}
		total += math.Hypot(float64(b[i].X-a[i].X-dx), float64(b[i].Y-a[i].Y-dy))
	}
	return total
}

// diffStrings returns the multiset difference both ways, deterministically.
func diffStrings(oldS, newS []string) (added, removed []string) {
	count := map[string]int{}
	for _, s := range oldS {
		count[s]++
	}
	for _, s := range newS {
		if count[s] > 0 {
			count[s]--
			continue
		}
		added = append(added, s)
	}
	seen := map[string]int{}
	for _, s := range newS {
		seen[s]++
	}
	for _, s := range oldS {
		if seen[s] > 0 {
			seen[s]--
			continue
		}
		removed = append(removed, s)
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
