package layout7

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// parse builds the engine's working graph from ipmt source.
func parse(t *testing.T, src string) *graph {
	t.Helper()
	doc, err := parser.Parse([]byte(src), parser.Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	g, err := normalize(doc)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	return g
}

func (g *graph) byName(t *testing.T, name string) *node {
	t.Helper()
	for _, n := range g.nodes {
		if n.name == name {
			return n
		}
	}
	t.Fatalf("node %q not found", name)
	return nil
}

// TestMembershipTwoComponents pins v7P1's covers example (spec id=100): a
// thing joins the component of the event structure that anchors it; two
// unconnected event chains separate.
func TestMembershipTwoComponents(t *testing.T) {
	g := parse(t, `
e1 ::e --> e2 ::e
A ::t --> e1
e3 ::e --> e4 ::e
`)
	g.resolveMembership()
	if got := g.byName(t, "A").comp; got != g.byName(t, "e1").comp {
		t.Errorf("A joins e1's component, got comp %d vs %d", got, g.byName(t, "e1").comp)
	}
	if g.byName(t, "e1").comp == g.byName(t, "e3").comp {
		t.Errorf("the two event chains must separate (v7P1)")
	}
}

// TestMembershipCrossLinksDoNotMerge pins v7P1's does-not-cover example
// (spec id=110): near-to anywhere, expresses between events, shared aux —
// none of them merges the two event chains; shared aux anchors-and-ties.
func TestMembershipCrossLinksDoNotMerge(t *testing.T) {
	g := parse(t, `
e1 ::e --> e2 ::e
A ::t --> e1
e3 ::e --> e4 ::e
B ::t --> e3
A --- B
e2 --::X--> e4
e1 --- e3
A, B --> cX ::c
e2, e4 --> cY ::c
C --> e2, e4
`)
	g.resolveMembership()
	c1, c2 := g.byName(t, "e1").comp, g.byName(t, "e3").comp
	if c1 == c2 {
		t.Fatalf("cross-links must not merge the chains (v7P1)")
	}
	// shared concepts anchor at equal depth -> first declared user's side
	if got := g.byName(t, "cX").comp; got != c1 {
		t.Errorf("cX anchors with A (first declared user), got comp %d", got)
	}
	if got := g.byName(t, "cY").comp; got != c1 {
		t.Errorf("cY anchors with e2 (first declared user), got comp %d", got)
	}
	// the shared thing C anchors-and-ties: e2's component, C->e4 demoted
	if got := g.byName(t, "C").comp; got != c1 {
		t.Errorf("C anchors at e2 (first declared connector), got comp %d", got)
	}
	for _, e := range g.edges {
		if g.nodes[e.from].name == "C" && g.nodes[e.to].name == "e4" {
			if !e.demotedTie {
				t.Errorf("C -> e4 must demote to a cross-component tie (v7P1/P7)")
			}
		}
	}
}

// TestDeepestUserWins pins v7P7's covers example (spec id=1d0): T is part-of
// W1 (depth 1) and W2 (depth 2); the deeper user anchors T, T -> W1 demotes.
func TestDeepestUserWins(t *testing.T) {
	g := parse(t, `
e1 ::e --> e2 ::e
W1 ::t --> e1
V ::t --> e2
W2 ::t --> V
T ::t --> W1
T --> W2
`)
	m := g.resolveMembership()
	tn := g.byName(t, "T")
	p := m.anchors[tn.idx].primary
	if p == nil || g.nodes[g.userOf(tn.idx, p)].name != "W2" {
		t.Fatalf("T must anchor at its deepest user W2 (v7P7)")
	}
	for _, e := range g.edges {
		if g.nodes[e.from].name == "T" && g.nodes[e.to].name == "W1" && !e.demotedTie {
			t.Errorf("T -> W1 must demote to a drawn tie (v7P7)")
		}
	}
}

// TestGroupAnchorPartMost pins v7P4's group anchor election (spec id=160):
// the group anchors through its part-most member with an event connector —
// p (a part of w) beats w; w's own connector demotes to a drawn edge.
func TestGroupAnchorPartMost(t *testing.T) {
	g := parse(t, `
e1 ::e --> e2 ::e
w ::t --> e1
p ::t --> w
p --> e2
q ::t --> p
p --> cX ::c
`)
	m := g.resolveMembership()
	pn := g.byName(t, "p")
	sid := m.structOf[pn.idx]
	if sid < 0 || m.structAnchor[sid] != pn.idx {
		t.Fatalf("the group must anchor through p, the part-most connected member (v7P4)")
	}
	for _, e := range g.edges {
		if g.nodes[e.from].name == "w" && g.nodes[e.to].name == "e1" && !e.demotedTie {
			t.Errorf("w -> e1 must demote to a plain drawn edge (v7P4 anchor-and-tie)")
		}
	}
}

// TestUntiedComponentsWrap pins v7P2: eight identical untied components wrap
// toward 16:9 instead of tiling one row (the acceptance case's shape).
func TestUntiedComponentsWrap(t *testing.T) {
	g := parse(t, "a1 ::e\nb1 ::e\nc1 ::e\nd1 ::e\nf1 ::e\ng1 ::e\nh1 ::e\ni1 ::e\n")
	m := g.resolveMembership()
	gp := g.buildGroups(m)
	sp := g.buildSkeleton(gp)
	g.place(m, gp, sp)
	g.assemble()
	first, last := g.byName(t, "a1"), g.byName(t, "i1")
	if last.y <= first.y {
		t.Errorf("the last component must wrap onto a lower row (v7P2); a1.y=%d i1.y=%d", first.y, last.y)
	}
}

// TestCorpusSmoke runs the whole fixture corpus through the engine: no
// errors, and no two EVENT boxes of one component overlap (the universal
// invariant the sweep checks on the current engine).
func TestCorpusSmoke(t *testing.T) {
	for _, dir := range []string{"../../tests/layout-gen", "../../tests/layout-gen-ext"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Skipf("no corpus at %s: %v", dir, err)
		}
		for _, ent := range entries {
			if !strings.HasSuffix(ent.Name(), ".ipmt") {
				continue
			}
			name := ent.Name()
			t.Run(name, func(t *testing.T) {
				b, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				doc, err := parser.Parse(b, parser.Options{})
				if err != nil {
					t.Skipf("parse: %v", err) // corpus validity is the runner's gate
				}
				out, err := Generate(doc)
				if err != nil {
					t.Fatalf("Generate: %v", err)
				}
				// determinism: same input, byte-identical output (the
				// ipm-simple-graph contract)
				again, err := Generate(doc)
				if err != nil {
					t.Fatalf("Generate (2nd): %v", err)
				}
				j1, _ := json.Marshal(out)
				j2, _ := json.Marshal(again)
				if !bytes.Equal(j1, j2) {
					t.Errorf("nondeterministic layout for %s", name)
				}
				var events []layout.Node
				for _, n := range out.Nodes {
					if n.Type == "event" {
						events = append(events, n)
					}
				}
				for i := 0; i < len(events); i++ {
					for j := i + 1; j < len(events); j++ {
						a, b := events[i], events[j]
						if a.X < b.X+b.Width && b.X < a.X+a.Width &&
							a.Y < b.Y+b.Height && b.Y < a.Y+a.Height {
							t.Errorf("event boxes overlap: %s and %s", a.Label, b.Label)
						}
					}
				}
			})
		}
	}
}

// TestGenerateConcurrent pins that Generate is safe to call from multiple
// goroutines at once. ipm-rpc dispatches parallel ipm.embedBuffer requests
// (one per open markdown file after an LSP restart), so any package-level
// mutable state in the engine is a fatal "concurrent map read and map
// write" that kills the whole server. Run with -race to catch regressions
// even when the scheduler doesn't trip the fatal path.
func TestGenerateConcurrent(t *testing.T) {
	// Fork/join-heavy source: exercises the span/forkOffsets lane pass
	// (the shared spanCache that crashed ipm-rpc).
	src := `
start ::e --> a1 ::e, b1 ::e, c1 ::e
a1 --> a2 ::e, a3 ::e
b1 --> b2 ::e
c1 --> c2 ::e, c3 ::e, c4 ::e
a2, b2 --> joined ::e
T ::t --> start
K ::c <-- a1
`
	doc, err := parser.Parse([]byte(src), parser.Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	const workers = 8
	const rounds = 20
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if _, err := Generate(doc); err != nil {
					t.Errorf("Generate: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
