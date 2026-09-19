package refs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTripAndRekey(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json")
	if err := writeFile(in, "{}"); err != nil {
		t.Fatal(err)
	}
	r := New("test-tool", []string{"-x"}, in, filepath.Join(dir, "missing.json"))
	if len(r.Build.Inputs) != 2 || r.Build.Inputs[0].SHA256 == "" || r.Build.Inputs[1].SHA256 != "" {
		t.Fatalf("inputs: %+v", r.Build.Inputs)
	}
	r.Node(7, Ref{Kind: KindLoop, At: "m@v1/a.go#f#L1", By: "tool:loopscan", Hash: "abc"})
	r.Node(7, Ref{Kind: KindCandidate, At: "m@v1/a.go#f#L1", By: "tool:loopmap", Signal: "ring", Conf: 0.95})
	r.Node(0, Ref{Kind: KindCut}) // id 0 is "no node": ignored
	r.Edge(12, Ref{Kind: KindCurated, At: "config/curation/x.md", By: "person", Mark: "[x]"})

	path := Path(filepath.Join(dir, "g.json"))
	if filepath.Base(path) != "g.refs.json" {
		t.Errorf("side-car path = %s", path)
	}
	if err := r.Write(path); err != nil {
		t.Fatal(err)
	}
	back, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Nodes[7]) != 2 || len(back.Edges[12]) != 1 || len(back.Nodes) != 1 {
		t.Fatalf("round trip: nodes %v edges %v", back.Nodes, back.Edges)
	}
	if got := back.Kinds(); len(got) != 3 {
		t.Errorf("kinds = %v", got)
	}

	cut := back.Rekey(map[int]int{1: 7}, "g.json", Build{Tool: "cutter"})
	if rs := cut.Nodes[1]; len(rs) != 3 || rs[2].Kind != KindCut {
		t.Errorf("rekeyed refs = %+v", rs)
	}
	if _, ok := cut.Nodes[7]; ok {
		t.Error("the original id must not survive a rekey")
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
