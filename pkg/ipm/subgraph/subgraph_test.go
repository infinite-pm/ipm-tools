package subgraph

import (
	"testing"

	ipmgraph "github.com/infinite-pm/ipm-tools/pkg/ipm/graph"
)

// concept ← loop ⊂ func ⊂ file ⊂ package ⊂ module@v; a run event expressing the concept.
func fixture() (*ipmgraph.Graph, map[string]int) {
	b := ipmgraph.New()
	id := map[string]int{}
	id["c"] = b.Node(ipmgraph.Concept, "control loop", "", "")
	id["far"] = b.Node(ipmgraph.Concept, "Reconciliation", "", "")
	id["mod"] = b.Node(ipmgraph.Thing, "k8s.io/kubernetes@v1", "", "")
	id["pkg"] = b.Node(ipmgraph.Thing, "k8s.io/kubernetes@v1/pkg/controller/job", "", "")
	id["file"] = b.Node(ipmgraph.Thing, "k8s.io/kubernetes@v1/pkg/controller/job/job_controller.go", "", "")
	id["fn"] = b.Node(ipmgraph.Thing, "job.(*Controller).worker()", "", "")
	id["loop"] = b.Node(ipmgraph.Thing, "worker ring · job_controller.go:635", "", "")
	id["run"] = b.Node(ipmgraph.Event, "reconcile #1", "", "")
	b.Edge(id["pkg"], id["mod"], ipmgraph.PartOf, "")
	b.Edge(id["file"], id["pkg"], ipmgraph.PartOf, "")
	b.Edge(id["fn"], id["file"], ipmgraph.PartOf, "")
	b.Edge(id["loop"], id["fn"], ipmgraph.PartOf, "")
	b.Edge(id["loop"], id["c"], ipmgraph.Expresses, "")
	b.Edge(id["run"], id["c"], ipmgraph.Expresses, "")
	b.Edge(id["c"], id["far"], ipmgraph.NearTo, "")
	return b.Graph(), id
}

func names(g *ipmgraph.Graph) map[string]bool {
	out := map[string]bool{}
	for _, n := range g.Nodes {
		out[n.Name] = true
	}
	return out
}

func TestCutKeepsNeighboursAndContainersDropsFileSpace(t *testing.T) {
	g, id := fixture()
	p := Cut(g, Concepts(g, []string{"control loop"}), Options{Depth: 1, Containers: 2, Drop: FileSpace})
	got := names(p.Graph)
	for _, want := range []string{"control loop", "worker ring · job_controller.go:635", "reconcile #1", "Reconciliation", "job.(*Controller).worker()"} {
		if !got[want] {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	for _, no := range []string{"k8s.io/kubernetes@v1/pkg/controller/job/job_controller.go", "k8s.io/kubernetes@v1"} {
		if got[no] {
			t.Errorf("file space %q should be dropped", no)
		}
	}
	if len(p.Seeds) != 1 || p.Seeds[0] != id["c"] {
		t.Errorf("seeds = %v, want [%d]", p.Seeds, id["c"])
	}
	if orig := p.ToOrig[p.ToNew[id["loop"]]]; orig != id["loop"] {
		t.Errorf("id map does not round-trip: %d → %d", id["loop"], orig)
	}
}

func TestCapNeverDropsASeed(t *testing.T) {
	g, id := fixture()
	p := Cut(g, []int{id["c"]}, Options{Depth: 2, Cap: 2})
	if len(p.Graph.Nodes) != 2 {
		t.Fatalf("cap 2 → %d nodes", len(p.Graph.Nodes))
	}
	if _, ok := p.ToNew[id["c"]]; !ok {
		t.Error("the seed was capped away")
	}
}

func TestNeighborhoodByName(t *testing.T) {
	b := ipmgraph.New()
	c := b.Node(ipmgraph.Concept, "Reconciliation", "", "")
	sub := b.Node(ipmgraph.Concept, "level-triggered", "", "")
	ev := b.Node(ipmgraph.Event, "reconcile #1", "", "")
	far := b.Node(ipmgraph.Thing, "kubeconfig", "", "")
	b.Edge(c, sub, ipmgraph.Expresses, "")
	b.Edge(ev, c, ipmgraph.Expresses, "")
	b.Edge(far, ev, ipmgraph.PartOf, "")
	nb := Neighborhood(b.Graph(), "Reconciliation", 1)
	if len(nb.Nodes) != 3 {
		t.Fatalf("depth 1 around Reconciliation: %d nodes, want 3", len(nb.Nodes))
	}
	for _, n := range nb.Nodes {
		if n.Name == "kubeconfig" {
			t.Error("kubeconfig is two hops away and must not be in a depth-1 neighbourhood")
		}
	}
	if got := Neighborhood(b.Graph(), "does-not-exist", 1); len(got.Nodes) != 0 {
		t.Errorf("no seed → empty graph, got %d nodes", len(got.Nodes))
	}
	if got := Neighborhood(b.Graph(), "reconcil", 0); len(got.Nodes) != 2 {
		t.Errorf("substring seed matches Reconciliation and reconcile #1: got %d", len(got.Nodes))
	}
}
