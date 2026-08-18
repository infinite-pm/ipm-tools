package main

// --stats: the STRUCTURAL SIZE of a diagram, one line per file — the numbers
// an outlier list is drawn from (docs/dev/layout-gen/layout-corpus.md): a
// corpus diagram whose composite has 45 members or whose hub fans onto 14
// boxes exercises the engine at a scale the fixtures never will, and a
// metric summed over it is a metric of that one diagram. Sweeps positional
// <paths…> like --check; no layout is run, the parsed graph is enough.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
)

// diagramStats are the per-diagram size numbers.
type diagramStats struct {
	nodes, edges                    int
	events, things, concepts        int
	composites                      int // events with ≥1 event member (PartOf event→event)
	maxMembers                      int // the largest composite's direct member count
	maxFanIn                        int // most edges into one node (any relation)
	maxHubPartOf                    int // most PartOf edges into one event (members + things)
	maxLeadsToChain                 int // longest leads-to chain (edges) among events
	maxExpressFan, maxNearTo        int // widest expresses fan-out; most near-to at one node
	crossCompositeLeadsTo           int // leads-to between members of DIFFERENT composites
	depth                           int // deepest event nesting (PartOf event→event hops)
}

// diagramStatsOf measures the parsed graph.
func diagramStatsOf(doc *model.IpmGraph) diagramStats {
	var s diagramStats
	s.nodes, s.edges = len(doc.Nodes), len(doc.Edges)
	kind := map[int]model.NodeType{}
	for _, n := range doc.Nodes {
		t := n.Type
		if t == model.Unresolved && len(n.Candidates) > 0 {
			t = n.Candidates[0]
		}
		kind[n.ID] = t
		switch t {
		case model.Event:
			s.events++
		case model.Thing:
			s.things++
		case model.Concept:
			s.concepts++
		}
	}
	fanIn := map[int]int{}
	hubPartOf := map[int]int{}
	members := map[int]int{}   // composite → direct member count
	parentOf := map[int]int{}  // member event → composite
	expressFan := map[int]int{}
	nearTo := map[int]int{}
	succ := map[int][]int{}
	for _, e := range doc.Edges {
		fanIn[e.Target]++
		switch e.SstLinkType {
		case model.PartOf:
			if kind[e.Target] == model.Event {
				hubPartOf[e.Target]++
				if kind[e.Source] == model.Event {
					members[e.Target]++
					parentOf[e.Source] = e.Target
				}
			}
		case model.LeadsTo:
			if kind[e.Source] == model.Event && kind[e.Target] == model.Event {
				succ[e.Source] = append(succ[e.Source], e.Target)
			}
		case model.Expresses:
			expressFan[e.Source]++
		case model.NearTo:
			nearTo[e.Source]++
			nearTo[e.Target]++
		}
	}
	s.composites = len(members)
	for _, m := range members {
		s.maxMembers = maxInt(s.maxMembers, m)
	}
	for _, f := range fanIn {
		s.maxFanIn = maxInt(s.maxFanIn, f)
	}
	for _, h := range hubPartOf {
		s.maxHubPartOf = maxInt(s.maxHubPartOf, h)
	}
	for _, f := range expressFan {
		s.maxExpressFan = maxInt(s.maxExpressFan, f)
	}
	for _, n := range nearTo {
		s.maxNearTo = maxInt(s.maxNearTo, n)
	}
	// leads-to across composite boundaries: both ends members, different roots
	root := func(ev int) int {
		for hops := 0; hops < len(doc.Nodes); hops++ {
			p, ok := parentOf[ev]
			if !ok {
				return ev
			}
			ev = p
		}
		return ev
	}
	for from, tos := range succ {
		for _, to := range tos {
			_, fm := parentOf[from]
			_, tm := parentOf[to]
			if fm && tm && root(from) != root(to) {
				s.crossCompositeLeadsTo++
			}
		}
	}
	// nesting depth
	for ev := range parentOf {
		d := 0
		for hops := 0; hops < len(doc.Nodes); hops++ {
			p, ok := parentOf[ev]
			if !ok {
				break
			}
			d++
			ev = p
		}
		s.depth = maxInt(s.depth, d)
	}
	// longest leads-to chain (DAG assumed; cycles are cut by the visit guard)
	memo := map[int]int{}
	var longest func(ev int, seen map[int]bool) int
	longest = func(ev int, seen map[int]bool) int {
		if v, ok := memo[ev]; ok {
			return v
		}
		if seen[ev] {
			return 0
		}
		seen[ev] = true
		best := 0
		for _, t := range succ[ev] {
			best = maxInt(best, 1+longest(t, seen))
		}
		delete(seen, ev)
		memo[ev] = best
		return best
	}
	for ev := range succ {
		s.maxLeadsToChain = maxInt(s.maxLeadsToChain, longest(ev, map[int]bool{}))
	}
	return s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// runStats sweeps <paths…> (files or directories of .ipmt) and prints one
// row per diagram, sorted by node count, plus the column maxima — the row
// that reads far above the others is the outlier candidate.
func runStats(paths []string) int {
	if len(paths) == 0 {
		paths = []string{"tests"}
	}
	var files []string
	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "layout-debug: skip %s: %v\n", root, err)
			continue
		}
		if !info.IsDir() {
			files = append(files, root)
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".ipmt") {
				files = append(files, path)
			}
			return nil
		})
	}
	type row struct {
		file string
		s    diagramStats
	}
	var rows []row
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "layout-debug: skip %s: %v\n", file, err)
			continue
		}
		doc, err := parser.Parse(data, parser.Options{Filename: file})
		if err != nil {
			fmt.Fprintf(os.Stderr, "layout-debug: skip %s: parse: %v\n", file, err)
			continue
		}
		rows = append(rows, row{file, diagramStatsOf(doc)})
	}
	sort.SliceStable(rows, func(a, b int) bool { return rows[a].s.nodes < rows[b].s.nodes })
	fmt.Printf("%-6s %-6s %-5s %-5s %-5s %-5s %-7s %-6s %-6s %-6s %-6s %-6s %-6s %-5s %s\n",
		"nodes", "edges", "ev", "th", "co", "comps", "members", "fanin", "hubP", "chain", "xfan", "near", "xcomp", "depth", "file")
	var mx diagramStats
	for _, r := range rows {
		s := r.s
		fmt.Printf("%-6d %-6d %-5d %-5d %-5d %-5d %-7d %-6d %-6d %-6d %-6d %-6d %-6d %-5d %s\n",
			s.nodes, s.edges, s.events, s.things, s.concepts, s.composites, s.maxMembers, s.maxFanIn,
			s.maxHubPartOf, s.maxLeadsToChain, s.maxExpressFan, s.maxNearTo, s.crossCompositeLeadsTo, s.depth, r.file)
		mx.nodes = maxInt(mx.nodes, s.nodes)
		mx.edges = maxInt(mx.edges, s.edges)
		mx.maxMembers = maxInt(mx.maxMembers, s.maxMembers)
		mx.maxFanIn = maxInt(mx.maxFanIn, s.maxFanIn)
		mx.maxHubPartOf = maxInt(mx.maxHubPartOf, s.maxHubPartOf)
		mx.maxLeadsToChain = maxInt(mx.maxLeadsToChain, s.maxLeadsToChain)
		mx.maxExpressFan = maxInt(mx.maxExpressFan, s.maxExpressFan)
		mx.maxNearTo = maxInt(mx.maxNearTo, s.maxNearTo)
		mx.crossCompositeLeadsTo = maxInt(mx.crossCompositeLeadsTo, s.crossCompositeLeadsTo)
		mx.depth = maxInt(mx.depth, s.depth)
	}
	fmt.Printf("layout-debug --stats: %d diagram(s); max nodes %d, edges %d, members %d, fanin %d, hubP %d, chain %d, xfan %d, near %d, xcomp %d, depth %d\n",
		len(rows), mx.nodes, mx.edges, mx.maxMembers, mx.maxFanIn, mx.maxHubPartOf, mx.maxLeadsToChain, mx.maxExpressFan, mx.maxNearTo, mx.crossCompositeLeadsTo, mx.depth)
	return 0
}
