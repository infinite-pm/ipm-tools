// Package subgraph cuts a readable piece out of a big graph: the neighbourhood
// of a set of seed nodes, plus the containers that make code things legible,
// under a size cap — re-id'd into a fresh valid graph, with the map back to the
// original ids so a rendering of the piece can still point into the whole
// (the zoom canvas addresses nodes by the original ids).
//
// It began in a consumer as the filter its design said it would reuse and
// nothing offered; it settled, and it lives beside the model now, with the
// older name-seeded Neighborhood beside it.
package subgraph

import (
	"sort"
	"strings"

	ipmgraph "github.com/infinite-pm/ipm-tools/pkg/ipm/graph"
)

// Options shape the cut.
type Options struct {
	// Depth is how many undirected hops from the seeds are kept. 1 is "the
	// concept and what expresses it"; 2 starts to pull in whole neighbourhoods.
	Depth int
	// Containers walks part-of upward from every kept Thing and keeps the
	// ancestors, so a loop comes with its function and package instead of
	// floating. 0 keeps none; 2 reaches the package from a loop (loop → func →
	// file is 2; the package is a third, which is what MaxContainers usually is).
	Containers int
	// Cap is the most nodes the piece may have; past it the farthest nodes go
	// first (higher hop count), then the most recently added at the same hop.
	// 0 means no cap.
	Cap int
	// Drop names node-name prefixes to leave out even when reached — the
	// file-space above the package, for instance, whose names are module paths.
	Drop func(n ipmgraph.Node) bool
}

// Piece is the cut, with the map back.
type Piece struct {
	Graph  *ipmgraph.Graph
	ToOrig map[int]int // new id → original id
	ToNew  map[int]int // original id → new id
	Seeds  []int       // original ids of the seeds that were found
}

// Cut takes the neighbourhood of the seed node ids.
func Cut(g *ipmgraph.Graph, seeds []int, o Options) *Piece {
	byID := map[int]ipmgraph.Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	adj := map[int][]int{}
	parent := map[int][]int{} // part-of: child → parents
	for _, e := range g.Edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
		if e.SstLinkType == ipmgraph.PartOf {
			parent[e.Source] = append(parent[e.Source], e.Target)
		}
	}
	drop := func(id int) bool {
		if o.Drop == nil {
			return false
		}
		n, ok := byID[id]
		return ok && o.Drop(n)
	}

	hop := map[int]int{} // kept node → hops from a seed (containers count as their child's hop)
	var order []int      // insertion order, for the cap
	keep := func(id, h int) bool {
		if _, ok := hop[id]; ok || drop(id) {
			return false
		}
		if _, ok := byID[id]; !ok {
			return false
		}
		hop[id] = h
		order = append(order, id)
		return true
	}

	var found []int
	var frontier []int
	for _, s := range seeds {
		if keep(s, 0) {
			found = append(found, s)
			frontier = append(frontier, s)
		}
	}
	for d := 1; d <= o.Depth; d++ {
		var next []int
		for _, id := range frontier {
			for _, nb := range adj[id] {
				if keep(nb, d) {
					next = append(next, nb)
				}
			}
		}
		frontier = next
	}

	// Containers: from every kept Thing, up the part-of chain, up to o.Containers
	// steps. A container inherits its child's hop so the cap treats it as near.
	if o.Containers > 0 {
		snapshot := append([]int(nil), order...)
		for _, id := range snapshot {
			if byID[id].Type != ipmgraph.Thing {
				continue
			}
			cur := []int{id}
			for step := 0; step < o.Containers && len(cur) > 0; step++ {
				var up []int
				for _, c := range cur {
					for _, p := range parent[c] {
						if byID[p].Type == ipmgraph.Thing && keep(p, hop[id]) {
							up = append(up, p)
						}
					}
				}
				cur = up
			}
		}
	}

	// The cap: farthest first, newest first within a hop.
	if o.Cap > 0 && len(order) > o.Cap {
		idx := map[int]int{}
		for i, id := range order {
			idx[id] = i
		}
		victims := append([]int(nil), order...)
		sort.SliceStable(victims, func(i, j int) bool {
			if hop[victims[i]] != hop[victims[j]] {
				return hop[victims[i]] > hop[victims[j]]
			}
			return idx[victims[i]] > idx[victims[j]]
		})
		for _, v := range victims[:len(order)-o.Cap] {
			if hop[v] == 0 {
				continue // never drop a seed
			}
			delete(hop, v)
		}
		var kept []int
		for _, id := range order {
			if _, ok := hop[id]; ok {
				kept = append(kept, id)
			}
		}
		order = kept
	}

	p := &Piece{ToOrig: map[int]int{}, ToNew: map[int]int{}, Seeds: found}
	b := ipmgraph.New()
	for _, n := range g.Nodes { // original order, so the piece reads like the whole
		if _, ok := hop[n.ID]; !ok {
			continue
		}
		id := b.Node(n.Type, n.Name, n.Alias, n.Tooltip)
		p.ToNew[n.ID], p.ToOrig[id] = id, n.ID
	}
	for _, e := range g.Edges {
		s, okS := p.ToNew[e.Source]
		t, okT := p.ToNew[e.Target]
		if okS && okT {
			b.Edge(s, t, e.SstLinkType, e.Tooltip)
		}
	}
	p.Graph = b.Graph()
	return p
}

// FindByName returns the ids of the nodes whose name matches exactly, else
// (when none does) case-insensitively.
func FindByName(g *ipmgraph.Graph, name string) []int {
	var ids []int
	for _, n := range g.Nodes {
		if n.Name == name {
			ids = append(ids, n.ID)
		}
	}
	if len(ids) > 0 {
		return ids
	}
	for _, n := range g.Nodes {
		if strings.EqualFold(n.Name, name) {
			ids = append(ids, n.ID)
		}
	}
	return ids
}

// Concepts returns the ids of the Concept nodes with these names.
func Concepts(g *ipmgraph.Graph, names []string) []int {
	var ids []int
	for _, name := range names {
		for _, id := range FindByName(g, name) {
			for _, n := range g.Nodes {
				if n.ID == id && n.Type == ipmgraph.Concept {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

// FileSpace reports whether a node is the file-space above the package level —
// a module (`x@v`), a package directory or a source file — which a section
// diagram is better off without: the loops and functions say where they are
// in their names already.
func FileSpace(n ipmgraph.Node) bool {
	if n.Type != ipmgraph.Thing {
		return false
	}
	name := n.Name
	if strings.HasSuffix(name, ".go") {
		return true
	}
	if at := strings.Index(name, "@"); at >= 0 {
		return true // a module, or a package path under one
	}
	return false
}

// Neighborhood is the older, name-seeded cut: every node within depth
// undirected hops of the nodes named seed (exact name, else a case-insensitive
// substring), re-id'd into a fresh valid graph. A curated "concept neighbourhood"
// tutorial view is cut with it. Empty when no seed matches.
func Neighborhood(g *ipmgraph.Graph, seed string, depth int) *ipmgraph.Graph {
	return NeighborhoodPiece(g, seed, depth).Graph
}

// NeighborhoodPiece is Neighborhood with the map back — what a side-car
// needs to carry the whole's refs onto the view (refs.Rekey). An empty piece
// has an empty graph and empty maps.
func NeighborhoodPiece(g *ipmgraph.Graph, seed string, depth int) *Piece {
	var seeds []int
	for _, n := range g.Nodes {
		if n.Name == seed {
			seeds = append(seeds, n.ID)
		}
	}
	if len(seeds) == 0 {
		for _, n := range g.Nodes {
			if strings.Contains(strings.ToLower(n.Name), strings.ToLower(seed)) {
				seeds = append(seeds, n.ID)
			}
		}
	}
	if len(seeds) == 0 {
		return &Piece{Graph: ipmgraph.New().Graph(), ToOrig: map[int]int{}, ToNew: map[int]int{}}
	}
	return Cut(g, seeds, Options{Depth: depth})
}
