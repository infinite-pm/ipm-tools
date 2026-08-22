package layoutdupe

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// taxonomy is the shape this pass exists for: many narrow things pointing at
// one broad concept. Each user also has a LOCAL partner it sits beside, so the
// diagram has short edges too — as every real one does. Without them the
// median edge would be a long one and nothing could stand out against it.
func taxonomy(n int) *model.IpmGraph {
	g := &model.IpmGraph{Version: "test"}
	id, eid := 0, 0
	var users []int
	for i := 0; i < n; i++ {
		id++
		u := id
		users = append(users, u)
		g.Nodes = append(g.Nodes, model.Node{ID: u, Name: fmt.Sprintf("t%d", i+1), Type: model.Thing})
		id++
		g.Nodes = append(g.Nodes, model.Node{ID: id, Name: fmt.Sprintf("d%d", i+1), Type: model.Thing})
		eid++
		g.Edges = append(g.Edges, model.Edge{ID: eid, Source: u, Target: id, SstLinkType: model.NearTo})
	}
	id++
	hub := id
	g.Nodes = append(g.Nodes, model.Node{ID: hub, Name: "artifact", Type: model.Concept})
	for _, u := range users {
		eid++
		g.Edges = append(g.Edges, model.Edge{ID: eid, Source: u, Target: hub, SstLinkType: model.Expresses})
	}
	return g
}

// hubID is the taxonomy's concept: the last node built.
func hubID(doc *model.IpmGraph) string {
	return strconv.Itoa(doc.Nodes[len(doc.Nodes)-1].ID)
}

// fakeLayout stands in for the engine so the test measures THIS pass, and it
// places nodes the way a real diagram looks: three tight clusters far apart.
// A node and its local partner land side by side (short edges, which set the
// median); the hub sits in the first cluster, so the users of the other two
// reach it across the diagram — exactly the situation a copy answers.
func fakeLayout(doc *model.IpmGraph) (*layout.Graph, error) {
	g := &layout.Graph{}
	for i, n := range doc.Nodes {
		pair := i / 2       // a user and its partner share a row
		cluster := pair % 3 // …and the rows fall into three far-apart columns
		g.Nodes = append(g.Nodes, layout.Node{
			ID: strconv.Itoa(n.ID), Label: n.Name, Type: string(n.Type),
			X: cluster*3000 + (i%2)*160, Y: (pair / 3) * 150, Width: 120, Height: 60,
		})
	}
	for _, e := range doc.Edges {
		g.Edges = append(g.Edges, layout.Edge{
			From: strconv.Itoa(e.Source), To: strconv.Itoa(e.Target),
			Base: baseOf(e.SstLinkType),
		})
	}
	return g, nil
}

func baseOf(t model.SstLinkType) string {
	switch t {
	case model.PartOf:
		return "partof"
	case model.LeadsTo:
		return "leadsto"
	case model.Expresses:
		return "expresses"
	}
	return "nearto"
}

func TestTaxonomyHubIsDrawnInSeveralPlaces(t *testing.T) {
	doc := taxonomy(8)
	res, err := GenerateWith(doc, Options{}, fakeLayout)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Copies) == 0 {
		t.Fatal("a hub every user reaches across the diagram must be copied")
	}
	hub := hubID(doc)
	places := res.Places[hub]
	if len(places) < 3 {
		t.Fatalf("hub drawn in %d places, want at least 3: %v", len(places), places)
	}
	if places[0] != hub {
		t.Errorf("the original must be one of the places (reading order): %v", places)
	}
	for _, p := range places[1:] {
		if res.Copies[p] != hub {
			t.Errorf("place %q does not map back to the hub", p)
		}
	}
}

// The graph is not edited: every edge of the original survives exactly once,
// and every copy carries the name and kind of what it copies.
func TestEveryEdgeSurvivesExactlyOnce(t *testing.T) {
	doc := taxonomy(9)
	res, err := GenerateWith(doc, Options{}, fakeLayout)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Graph.Edges) != len(doc.Edges) {
		t.Fatalf("edges: got %d, want %d — duplication must move edges, never add or drop them",
			len(res.Graph.Edges), len(doc.Edges))
	}
	label := map[string]string{}
	for _, n := range res.Graph.Nodes {
		label[n.ID] = n.Label
	}
	for copyID, origID := range res.Copies {
		if label[copyID] != label[origID] {
			t.Errorf("copy %s reads %q but copies %q", copyID, label[copyID], label[origID])
		}
	}
}

// A copy that carries no edges would be a box naming a thing and connecting
// to nothing — the bug the prototype rendered before it was fixed.
func TestNoNodeIsLeftWithoutEdges(t *testing.T) {
	doc := taxonomy(10)
	res, err := GenerateWith(doc, Options{}, fakeLayout)
	if err != nil {
		t.Fatal(err)
	}
	deg := map[string]int{}
	for _, e := range res.Graph.Edges {
		deg[e.From]++
		deg[e.To]++
	}
	for _, n := range res.Graph.Nodes {
		if deg[n.ID] == 0 {
			t.Errorf("node %s (%q) is drawn with no edges at all", n.ID, n.Label)
		}
	}
}

// Role, not kind: the story's spine and the containers are never copied, even
// when their edges are long.
func TestSpineAndContainersAreNeverCopied(t *testing.T) {
	g := &model.IpmGraph{Version: "test"}
	for i := 1; i <= 20; i++ {
		g.Nodes = append(g.Nodes, model.Node{ID: i, Name: fmt.Sprintf("e%d", i), Type: model.Event})
	}
	whole := 21
	g.Nodes = append(g.Nodes, model.Node{ID: whole, Name: "the whole", Type: model.Event})
	eid := 0
	for i := 1; i < 20; i++ { // a leads-to chain: the spine
		eid++
		g.Edges = append(g.Edges, model.Edge{ID: eid, Source: i, Target: i + 1, SstLinkType: model.LeadsTo})
	}
	for i := 1; i <= 20; i++ { // every step part of one whole: containment
		eid++
		g.Edges = append(g.Edges, model.Edge{ID: eid, Source: i, Target: whole, SstLinkType: model.PartOf})
	}
	res, err := GenerateWith(g, Options{}, fakeLayout)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Copies) != 0 {
		t.Fatalf("nothing here may be copied (all spine and container), got %d copies: %v",
			len(res.Copies), res.Copies)
	}
}

// Two runs of the same graph must produce the same copies with the same ids —
// the corpus ratchet compares layouts across runs.
func TestDeterministic(t *testing.T) {
	doc := taxonomy(12)
	a, err := GenerateWith(doc, Options{}, fakeLayout)
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateWith(doc, Options{}, fakeLayout)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Copies) != len(b.Copies) {
		t.Fatalf("copy count differs between runs: %d vs %d", len(a.Copies), len(b.Copies))
	}
	for k, v := range a.Copies {
		if b.Copies[k] != v {
			t.Fatalf("copy %s maps to %q then %q", k, v, b.Copies[k])
		}
	}
	for orig, pa := range a.Places {
		pb := b.Places[orig]
		if len(pa) != len(pb) {
			t.Fatalf("places for %s differ: %v vs %v", orig, pa, pb)
		}
		for i := range pa {
			if pa[i] != pb[i] {
				t.Fatalf("place order for %s differs: %v vs %v", orig, pa, pb)
			}
		}
	}
}

// Small diagrams are left alone: measurement showed they gain nothing and pay
// area for it.
func TestSmallDiagramsAreLeftAlone(t *testing.T) {
	doc := taxonomy(4) // 5 nodes, under MinNodes
	res, err := GenerateWith(doc, Options{}, fakeLayout)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Copies) != 0 {
		t.Fatalf("a %d-node diagram must be left alone, got %d copies", len(doc.Nodes), len(res.Copies))
	}
}

// With everything close together there is nothing to answer: no distance, no
// copies.
func TestNothingIsCopiedWhenEverythingIsClose(t *testing.T) {
	doc := taxonomy(20)
	tight := func(d *model.IpmGraph) (*layout.Graph, error) {
		g, _ := fakeLayout(d)
		for i := range g.Nodes { // stack them tight: nothing is far from anything
			g.Nodes[i].X = 0
			g.Nodes[i].Y = i * 4
		}
		return g, nil
	}
	res, err := GenerateWith(doc, Options{}, tight)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Copies) != 0 {
		t.Fatalf("nothing is too far to read here; got %d copies", len(res.Copies))
	}
}

// The pass may only improve the picture. When the rewritten layout is worse —
// and it can be, because the engine re-places everything from the new input —
// the single-placement layout is kept and the caller is told.
func TestAWorseLayoutIsRejected(t *testing.T) {
	doc := taxonomy(9)
	worse := func(d *model.IpmGraph) (*layout.Graph, error) {
		g, _ := fakeLayout(d)
		// The rewritten graph has more nodes; scatter THAT one violently.
		if len(d.Nodes) > len(doc.Nodes) {
			for i := range g.Nodes {
				g.Nodes[i].X = i * 4000
				g.Nodes[i].Y = i * 4000
			}
		}
		return g, nil
	}
	res, err := GenerateWith(doc, Options{}, worse)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Rejected {
		t.Fatal("a worse layout must be rejected")
	}
	if len(res.Copies) != 0 {
		t.Fatalf("a rejected pass must report no copies, got %d", len(res.Copies))
	}
	if len(res.Graph.Nodes) != len(doc.Nodes) {
		t.Fatalf("a rejected pass must return the original layout: %d nodes, want %d",
			len(res.Graph.Nodes), len(doc.Nodes))
	}
}
