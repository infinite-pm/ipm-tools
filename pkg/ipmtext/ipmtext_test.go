package ipmtext

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
)

// An Unresolved node must serialize with the ::?<letters> marker (not ::t) and
// round-trip back to Unresolved with the same candidate order — otherwise the
// text misrepresents a grey node as a Thing (kubernetes "compares states" bug).
func TestSerializeUnresolvedRoundTrips(t *testing.T) {
	g := &model.IpmGraph{
		Version: "test",
		Nodes: []model.Node{
			{ID: 1, Name: "e91", Type: model.Event},
			{ID: 2, Name: "compares states", Type: model.Unresolved, Candidates: []model.NodeType{model.Concept, model.Thing}},
		},
		Edges: []model.Edge{
			{ID: 3, Source: 1, Target: 2, SstLinkType: model.Expresses, Dir: model.DirFwd, Tooltip: "has behaviour"},
		},
	}
	out, err := Serialize(g)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "::?ct") {
		t.Fatalf("expected ::?ct marker for Unresolved node, got:\n%s", out)
	}
	if strings.Contains(out, "\"compares states\" ::t") {
		t.Fatalf("Unresolved node must not be rendered as ::t:\n%s", out)
	}

	doc, err := parser.Parse([]byte(out), parser.Options{})
	if err != nil {
		t.Fatalf("re-parse failed: %v\n%s", err, out)
	}
	var n *model.Node
	for i := range doc.Nodes {
		if doc.Nodes[i].Name == "compares states" {
			n = &doc.Nodes[i]
		}
	}
	if n == nil || n.Type != model.Unresolved {
		t.Fatalf("round-trip: got %v, want Unresolved", n)
	}
	if len(n.Candidates) != 2 || n.Candidates[0] != model.Concept || n.Candidates[1] != model.Thing {
		t.Errorf("round-trip candidates: got %v, want [Concept Thing]", n.Candidates)
	}
}

// A node name with every character the serializer escapes — tab, newline,
// backslash, double-quote — must round-trip back identically. SSTorytime carries
// Go/SQL code snippets with literal tabs; the parser's unescapeQuoted was missing
// the \t case, so the serialized ipmt failed to re-parse ("invalid escape \t")
// and canvas-server couldn't build the zoom.
func TestSerializeSpecialCharsRoundTrip(t *testing.T) {
	const name = "type Node struct {\n\tArr int // tab\n}\tand a \"quote\" and a back\\slash"
	g := &model.IpmGraph{
		Version: "test",
		Nodes: []model.Node{
			{ID: 1, Name: "e1", Type: model.Event},
			{ID: 2, Name: name, Type: model.Concept},
		},
		Edges: []model.Edge{
			{ID: 3, Source: 1, Target: 2, SstLinkType: model.Expresses, Dir: model.DirFwd},
		},
	}
	out, err := Serialize(g)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parser.Parse([]byte(out), parser.Options{})
	if err != nil {
		t.Fatalf("re-parse failed: %v\n%s", err, out)
	}
	var got string
	for i := range doc.Nodes {
		if strings.Contains(doc.Nodes[i].Name, "Arr") {
			got = doc.Nodes[i].Name
		}
	}
	if got != name {
		t.Errorf("round-trip mismatch:\n got %q\nwant %q", got, name)
	}
}
