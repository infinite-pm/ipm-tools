// Package layoutdupe draws a participant more than once so a reader can read
// every participant of an event without moving to another part of the diagram.
//
// The complaint this answers is a distance: an edge long enough that its far
// end cannot be read from here. The answer is not to route that edge better —
// it is to draw the thing at the far end HERE, next to the event that uses it,
// and let the reader walk the copies (the atlas's badge-and-walk, inside one
// diagram; design: ipm-drawio wip/zoom-duplicates).
//
// Two properties keep it cheap and safe:
//
//   - The GRAPH never changes. Duplication is a placement decision, like a
//     container shell or an S/E marker. The parser, the validator, the union
//     and every committed .ipm.json still see one node. This is not "two
//     things"; it is one thing drawn twice.
//   - The ENGINE never changes. A pre-pass rewrites the layout INPUT — copies
//     take fresh ids and carry their local edges — layout7 runs unmodified,
//     and the result carries a map back to the real ids.
//
// What may be copied is a matter of ROLE, not node kind: a node that plays a
// reference role (a tool used in four steps, a concept many things express, a
// definition) may be drawn wherever it is used, while the story's own spine
// may not. So a node is eligible when it is NOT on a leads-to chain and NOT a
// part-of parent — never a step, never a whole.
package layoutdupe

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layout7"
)

// Options tunes the pass. The defaults are the ones measured in the design's
// prototype table; they are parameters, not truths.
type Options struct {
	// Off disables the pass entirely — the caller gets the plain layout. It
	// is spelled negatively so the zero value DOES something: a caller that
	// asks for duplication and forgets to fill the thresholds gets the
	// measured defaults, not silence.
	Off bool
	// LongFactor multiplies the diagram's MEDIAN edge length to decide what
	// counts as too long to read from here.
	LongFactor float64
	// MinLongPx is the floor under that: in a small tight diagram, twice the
	// median is still readable.
	MinLongPx float64
	// MinNodes is the size below which the pass does nothing. Small diagrams
	// gained nothing and lost area in measurement.
	MinNodes int
}

// DefaultOptions are the measured starting values.
func DefaultOptions() Options {
	return Options{LongFactor: 2.0, MinLongPx: 320, MinNodes: 16}
}

func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.LongFactor <= 0 {
		o.LongFactor = d.LongFactor
	}
	if o.MinLongPx <= 0 {
		o.MinLongPx = d.MinLongPx
	}
	if o.MinNodes <= 0 {
		// Zero means "unset": a caller that really wants every diagram
		// considered passes MinNodes: 1.
		o.MinNodes = d.MinNodes
	}
	return o
}

// Result is a laid-out graph plus what the reader needs to be told about the
// copies in it.
type Result struct {
	Graph *layout.Graph
	// Copies maps a copy's layout node id to the id of the node it copies.
	// Empty when nothing was duplicated.
	Copies map[string]string
	// Places maps an original node id to every place it is drawn, the
	// original included, in reading order (top-to-bottom, left-to-right).
	// A node drawn once does not appear here.
	Places map[string][]string
	// Threshold is the distance that counted as "too long to read from here",
	// and Median the diagram's median edge length — both reported so a caller
	// can explain the decision.
	Threshold, Median float64
	// Rejected is true when copies were planned but the resulting layout was
	// not an improvement, so the single-placement layout was kept.
	Rejected bool
	// Before and After are the two layouts' costs, for a report.
	Before, After Cost
}

// Cost is what "better" means here: the reader's complaint is distance, so it
// is measured in distance — how much of it there is, how bad the worst one is,
// and how many edges are still too long to read from where they start.
type Cost struct {
	Total, Max float64
	Long       int
}

func (a Cost) betterThan(b Cost) bool {
	// Total length must not grow, and the worst edge must not get worse by
	// more than a whisker (the engine shuffles; 5% is noise, 3x is not).
	if a.Total > b.Total {
		return false
	}
	if a.Max > b.Max*1.05 {
		return false
	}
	return a.Long <= b.Long
}

// cost measures a laid-out graph the way the reader feels it.
func cost(g *layout.Graph, edges []model.Edge) Cost {
	center := map[string][2]float64{}
	for _, n := range g.Nodes {
		center[n.ID] = [2]float64{float64(n.X) + float64(n.Width)/2, float64(n.Y) + float64(n.Height)/2}
	}
	var c Cost
	for _, e := range edges {
		a, oka := center[strconv.Itoa(e.Source)]
		b, okb := center[strconv.Itoa(e.Target)]
		if !oka || !okb {
			continue
		}
		d := euclid(a, b)
		c.Total += d
		if d > c.Max {
			c.Max = d
		}
		if d > 400 {
			c.Long++
		}
	}
	return c
}

// Generate lays out doc, drawing eligible participants once per cluster of
// users that would otherwise be too far away to read.
//
// It runs the engine twice: once to learn where things actually land (the
// distances are a property of the layout, not of the graph), then once more on
// the rewritten input. With duplication off, or nothing eligible, the second
// pass is skipped and the first layout is returned unchanged — so a caller can
// always ask for this and pay only when it does something.
func Generate(doc *model.IpmGraph, opts Options) (*Result, error) {
	return GenerateWith(doc, opts, layout7.Generate)
}

// GenerateWith is Generate with the engine call injected, so a caller that
// runs the engine with its own options keeps them.
func GenerateWith(doc *model.IpmGraph, opts Options, gen func(*model.IpmGraph) (*layout.Graph, error)) (*Result, error) {
	return GenerateWithHints(doc, opts, func(d *model.IpmGraph, _ map[string][2]int) (*layout.Graph, error) {
		return gen(d)
	})
}

// GenerateWithHints is GenerateWith for an engine that PLACES BY POSITION —
// the canvas anchors every component where the all-open layout had it, keyed
// by node id. A copy has an id that anchor has never seen, so without a hint
// it lands wherever the engine likes: measured on the kubernetes corpus, the
// rewritten layout came out with its longest edge at 4897px against 1069px
// before, and the pass rejected its own work. The hint says where each copy
// belongs — the centre of the users it serves — and the caller merges it into
// whatever positional option its engine takes.
func GenerateWithHints(doc *model.IpmGraph, opts Options, gen func(*model.IpmGraph, map[string][2]int) (*layout.Graph, error)) (*Result, error) {
	opts = opts.withDefaults()
	first, err := gen(doc, nil)
	if err != nil {
		return nil, err
	}
	res := &Result{Graph: first, Copies: map[string]string{}, Places: map[string][]string{}}
	if opts.Off || len(doc.Nodes) < opts.MinNodes {
		return res, nil
	}

	plan, threshold, median := planCopies(doc, first, opts)
	res.Threshold, res.Median = threshold, median
	if len(plan) == 0 {
		return res, nil
	}

	dup, copies := rewrite(doc, plan)
	second, err := gen(dup, copyHints(first, dup, plan, copies))
	if err != nil {
		// A rewrite that the engine cannot lay out is a bug in this pass, not
		// the caller's problem: fall back to the honest single-placement
		// layout rather than failing the render.
		return res, nil
	}

	// The pass may only IMPROVE the picture. Copies change the input, and the
	// engine re-places everything from it, so a rewrite can land worse than
	// what it replaced — measured: one panel's total edge length tripled and
	// its longest edge went from 987px to 4870px when the threshold was
	// loosened. Compare and keep the better one.
	before, after := cost(first, doc.Edges), cost(second, dup.Edges)
	if !after.betterThan(before) {
		res.Rejected = true
		res.Before, res.After = before, after
		return res, nil
	}
	res.Graph = second
	res.Copies = copies
	res.Places = places(second, copies)
	res.Before, res.After = before, after
	return res, nil
}

// cluster is one group of users far from their participant: the edges that
// reach them, which a single copy will serve.
type cluster struct {
	node  int   // the node being copied
	edges []int // edge ids that move to the copy
}

// planCopies decides what to duplicate. Deterministic: nodes in id order,
// clusters ordered by size then distance, so two runs of the same graph
// produce the same copies in the same order.
func planCopies(doc *model.IpmGraph, first *layout.Graph, opts Options) ([]cluster, float64, float64) {
	center := map[int][2]float64{}
	for _, n := range first.Nodes {
		id, err := strconv.Atoi(n.ID)
		if err != nil {
			continue // shells and other layout-added nodes are not graph nodes
		}
		center[id] = [2]float64{float64(n.X) + float64(n.Width)/2, float64(n.Y) + float64(n.Height)/2}
	}

	var lens []float64
	for _, e := range doc.Edges {
		if d, ok := dist(center, e.Source, e.Target); ok {
			lens = append(lens, d)
		}
	}
	if len(lens) == 0 {
		return nil, 0, 0
	}
	sort.Float64s(lens)
	median := lens[len(lens)/2]
	threshold := math.Max(opts.LongFactor*median, opts.MinLongPx)

	// Role, not kind: the spine and the containers are never copied.
	onSpine := map[int]bool{}
	isParent := map[int]bool{}
	incident := map[int][]model.Edge{}
	for _, e := range doc.Edges {
		incident[e.Source] = append(incident[e.Source], e)
		incident[e.Target] = append(incident[e.Target], e)
		switch e.SstLinkType {
		case model.LeadsTo:
			onSpine[e.Source] = true
			onSpine[e.Target] = true
		case model.PartOf:
			isParent[e.Target] = true
		}
	}

	// Which END of a long edge gets copied? The one that is SHARED — the
	// participant many places use. Copying the other way round (drawing each
	// user next to the participant) would move the problem rather than answer
	// it: the reader would still have to travel, just in the other direction.
	degree := map[int]int{}
	for _, e := range doc.Edges {
		degree[e.Source]++
		degree[e.Target]++
	}
	eligible := func(id int) bool { return !onSpine[id] && !isParent[id] }

	// far users, grouped by the node that will be copied for them
	farOf := map[int][]farUser{}
	nearCount := map[int]int{}
	for _, e := range doc.Edges {
		a, b := e.Source, e.Target
		pa, oka := center[a]
		pb, okb := center[b]
		if !oka || !okb {
			continue
		}
		if euclid(pa, pb) <= threshold {
			nearCount[a]++
			nearCount[b]++
			continue
		}
		var copySide, farSide int
		switch {
		case eligible(a) && eligible(b):
			// the shared one; ties to the target of expresses (the reference
			// side of a definition), then to the lower id for determinism
			switch {
			case degree[a] != degree[b]:
				if degree[a] > degree[b] {
					copySide, farSide = a, b
				} else {
					copySide, farSide = b, a
				}
			case e.SstLinkType == model.Expresses:
				copySide, farSide = b, a
			case a < b:
				copySide, farSide = a, b
			default:
				copySide, farSide = b, a
			}
		case eligible(a):
			copySide, farSide = a, b
		case eligible(b):
			copySide, farSide = b, a
		default:
			continue // spine to spine: not this pass's business
		}
		farOf[copySide] = append(farOf[copySide], farUser{e, center[farSide]})
	}

	ids := make([]int, 0, len(farOf))
	for id := range farOf {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	var plan []cluster
	for _, id := range ids {
		here := center[id]
		fars := farOf[id]
		sort.SliceStable(fars, func(i, j int) bool { return fars[i].edge.ID < fars[j].edge.ID })

		// Group far users by proximity, so two neighbouring events share one
		// copy instead of getting one each.
		var groups [][]farUser
		for _, f := range fars {
			placed := false
			for gi := range groups {
				for _, g := range groups[gi] {
					if euclid(g.at, f.at) <= threshold*0.9 {
						groups[gi] = append(groups[gi], f)
						placed = true
						break
					}
				}
				if placed {
					break
				}
			}
			if !placed {
				groups = append(groups, []farUser{f})
			}
		}
		sort.SliceStable(groups, func(i, j int) bool {
			if len(groups[i]) != len(groups[j]) {
				return len(groups[i]) > len(groups[j])
			}
			return minDist(here, groups[i]) < minDist(here, groups[j])
		})
		// The ORIGINAL keeps its best group when it has no near edges at all —
		// otherwise it would be left naming a thing and connecting to nothing.
		if nearCount[id] == 0 && len(groups) > 0 {
			groups = groups[1:]
		}
		for _, g := range groups {
			c := cluster{node: id}
			for _, f := range g {
				c.edges = append(c.edges, f.edge.ID)
			}
			sort.Ints(c.edges)
			plan = append(plan, c)
		}
	}
	return plan, threshold, median
}

// rewrite builds the placement graph: one extra node per cluster, carrying
// that cluster's edges. Returns it and the copy→original id map (layout ids,
// which are strings).
func rewrite(doc *model.IpmGraph, plan []cluster) (*model.IpmGraph, map[string]string) {
	out := *doc
	out.Nodes = append([]model.Node(nil), doc.Nodes...)
	out.Edges = append([]model.Edge(nil), doc.Edges...)

	byID := map[int]model.Node{}
	maxID := 0
	for _, n := range doc.Nodes {
		byID[n.ID] = n
		if n.ID > maxID {
			maxID = n.ID
		}
	}
	edgeAt := map[int]int{} // edge id -> index in out.Edges
	for i, e := range out.Edges {
		edgeAt[e.ID] = i
	}

	copies := map[string]string{}
	for _, c := range plan {
		maxID++
		src := byID[c.node]
		cp := src
		cp.ID = maxID
		out.Nodes = append(out.Nodes, cp)
		copies[strconv.Itoa(cp.ID)] = strconv.Itoa(c.node)
		for _, eid := range c.edges {
			i, ok := edgeAt[eid]
			if !ok {
				continue
			}
			if out.Edges[i].Source == c.node {
				out.Edges[i].Source = cp.ID
			} else if out.Edges[i].Target == c.node {
				out.Edges[i].Target = cp.ID
			}
		}
	}
	return &out, copies
}

// copyHints says where each copy belongs: the centre of the users it serves,
// in the coordinates of the layout that was just measured. An engine that
// places by position takes these for the ids it has never seen.
func copyHints(first *layout.Graph, dup *model.IpmGraph, plan []cluster, copies map[string]string) map[string][2]int {
	center := map[int][2]float64{}
	for _, n := range first.Nodes {
		id, err := strconv.Atoi(n.ID)
		if err != nil {
			continue
		}
		center[id] = [2]float64{float64(n.X) + float64(n.Width)/2, float64(n.Y) + float64(n.Height)/2}
	}
	// which edges each copy carries, by copy id
	edgeOf := map[int][]int{}
	copyIDs := make([]string, 0, len(copies))
	for id := range copies {
		copyIDs = append(copyIDs, id)
	}
	sort.Strings(copyIDs)
	// plan and the copy ids were assigned in the same order by rewrite
	byPlan := make([]int, 0, len(plan))
	for _, e := range dup.Nodes {
		if _, ok := copies[strconv.Itoa(e.ID)]; ok {
			byPlan = append(byPlan, e.ID)
		}
	}
	for i, c := range plan {
		if i < len(byPlan) {
			edgeOf[byPlan[i]] = c.edges
		}
	}
	edgeByID := map[int]model.Edge{}
	for _, e := range dup.Edges {
		edgeByID[e.ID] = e
	}
	hints := map[string][2]int{}
	for copyID, edges := range edgeOf {
		var sx, sy, n float64
		for _, eid := range edges {
			e, ok := edgeByID[eid]
			if !ok {
				continue
			}
			other := e.Source
			if other == copyID {
				other = e.Target
			}
			if p, ok := center[other]; ok {
				sx += p[0]
				sy += p[1]
				n++
			}
		}
		if n > 0 {
			hints[strconv.Itoa(copyID)] = [2]int{int(sx / n), int(sy / n)}
		}
	}
	return hints
}

// places lists every drawing of a duplicated node in reading order, the
// original included — what a badge means by "2 of 4".
func places(g *layout.Graph, copies map[string]string) map[string][]string {
	if len(copies) == 0 {
		return map[string][]string{}
	}
	pos := map[string][2]int{}
	for _, n := range g.Nodes {
		pos[n.ID] = [2]int{n.Y, n.X}
	}
	groups := map[string][]string{}
	for copyID, origID := range copies {
		if len(groups[origID]) == 0 {
			groups[origID] = append(groups[origID], origID)
		}
		groups[origID] = append(groups[origID], copyID)
	}
	for orig, list := range groups {
		sort.Slice(list, func(i, j int) bool {
			a, b := pos[list[i]], pos[list[j]]
			if a[0] != b[0] {
				return a[0] < b[0]
			}
			if a[1] != b[1] {
				return a[1] < b[1]
			}
			return list[i] < list[j]
		})
		groups[orig] = list
	}
	return groups
}

func dist(center map[int][2]float64, a, b int) (float64, bool) {
	pa, ok1 := center[a]
	pb, ok2 := center[b]
	if !ok1 || !ok2 {
		return 0, false
	}
	return euclid(pa, pb), true
}

func euclid(a, b [2]float64) float64 {
	return math.Hypot(a[0]-b[0], a[1]-b[1])
}

// farUser is one user of a participant that sits too far to read from it.
type farUser struct {
	edge model.Edge
	at   [2]float64
}

func minDist(from [2]float64, g []farUser) float64 {
	best := math.Inf(1)
	for _, f := range g {
		if d := euclid(from, f.at); d < best {
			best = d
		}
	}
	return best
}

// Describe is a one-line summary for a log or a report.
func (r *Result) Describe() string {
	if r.Rejected {
		return fmt.Sprintf("copies rejected: the layout was not better (length %.0f→%.0f, longest %.0f→%.0f, too-long edges %d→%d)",
			r.Before.Total, r.After.Total, r.Before.Max, r.After.Max, r.Before.Long, r.After.Long)
	}
	if len(r.Copies) == 0 {
		return "no copies"
	}
	return fmt.Sprintf("%d copies of %d participants (long edge > %.0fpx, median %.0fpx): length %.0f→%.0f, longest %.0f→%.0f, too-long edges %d→%d",
		len(r.Copies), len(r.Places), r.Threshold, r.Median,
		r.Before.Total, r.After.Total, r.Before.Max, r.After.Max, r.Before.Long, r.After.Long)
}
