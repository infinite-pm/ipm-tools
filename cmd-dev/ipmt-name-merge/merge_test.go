package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The 2025 engines split parse from layout: names came out of ipmt-parse,
// geometry out of layout-gen, and that era's renderer joined them. Reading
// only the engine's stdout lost every label — which drew unlabelled boxes AND,
// because the structural diff identifies a node by Type+Label+Alias, made an
// unlabelled side match nothing, so whole columns reported as wholly changed.
func TestNamesComeBackFromTheParseOutput(t *testing.T) {
	parse := []byte(`{"nodes":[{"id":1,"name":"Commit","type":"Event"},{"id":2,"name":"Build","type":"Event"}]}`)
	layout := []byte(`{"version":"25.09-layout-v2","nodes":[
	  {"id":"1","type":"event","x":40,"y":150,"width":120,"height":60},
	  {"id":"2","type":"event","x":40,"y":270,"width":120,"height":60}],"edges":[]}`)

	out, err := Merge(layout, parse)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version string           `json:"version"`
		Nodes   []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != "25.09-layout-v2" {
		t.Errorf("the merge lost the rest of the document (version = %q)", got.Version)
	}
	for i, want := range []string{"Commit", "Build"} {
		if got.Nodes[i]["label"] != want {
			t.Errorf("node %d label = %v, want %q", i+1, got.Nodes[i]["label"], want)
		}
	}
	// Geometry must survive exactly — a coordinate that round-trips as
	// 1.2e+02 is a diagram that moved for no reason.
	if !strings.Contains(string(out), `"y":150`) {
		t.Errorf("coordinates did not survive the round trip:\n%s", out)
	}
}

// An id is a number on one side and a string on the other. Matching on the Go
// type would join nothing at all, silently.
func TestIDsMatchAcrossJSONTypes(t *testing.T) {
	out, err := Merge(
		[]byte(`{"nodes":[{"id":"7"}]}`),
		[]byte(`{"nodes":[{"id":7,"name":"seven"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"label":"seven"`) {
		t.Errorf("a numeric id did not match a string id: %s", out)
	}
}

// An engine that labels its own output is telling the truth about itself.
func TestAnEngineThatLabelsIsLeftAlone(t *testing.T) {
	_, err := Merge(
		[]byte(`{"nodes":[{"id":"1","label":"Mine"}]}`),
		[]byte(`{"nodes":[{"id":1,"name":"Theirs"}]}`))
	if err == nil {
		t.Fatal("expected the merge to report that it had nothing to do")
	}
	if !strings.Contains(err.Error(), "no node took a name") {
		t.Errorf("unexpected error: %v", err)
	}
}
