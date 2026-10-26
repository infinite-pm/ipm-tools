package main

import (
	"reflect"
	"testing"
)

func TestClassify(t *testing.T) {
	changed := map[string]bool{
		"tests/ipmt/files-md/a.ipmt":               true,
		"tests/layout-gen/b.md":                    false,
		"tests/layout-gen/c.ipmt":                  true,
		"tests/layout-gen/d.ipmt":                  false,
		"tests/layout-gen-rules/r.dsl.json":        true,
		"tests/layout-gen-rules/q.dsl.json":        false,
		"tests/ipmt/files-md/new-source-only.ipmt": true,
		"tests/ipmt/invalid/bad.md":                true,
		"tests/ipmt/invalid/ok.md":                 false,
		"tests/ipmt/files-md/gone.ipmt":            false,
	}
	src := func(p string) bool { return changed[p] }

	in := []Change{
		// files-md: the sibling .ipmt changed -> EXPECTED
		{Path: "tests/ipmt/files-md/a.layout.json", Status: "M"},
		{Path: "tests/ipmt/files-md/a.ipm.svg", Status: "M"},
		{Path: "tests/ipmt/files-md/a.json", Status: "M"},
		// layout-gen: source unchanged -> STALE-AT-BASE
		{Path: "tests/layout-gen/d.ipm.svg", Status: "M"},
		{Path: "tests/layout-gen/d.layout.json", Status: "M"},
		// layout-gen: a sibling source changed -> EXPECTED
		{Path: "tests/layout-gen/c.ipm.svg", Status: "M"},
		// rules dir: stem q has an unchanged .dsl.json, r a changed one
		{Path: "tests/layout-gen-rules/q.layout.json", Status: "M"},
		{Path: "tests/layout-gen-rules/r.layout.json", Status: "A"},
		// invalid sidecars
		{Path: "tests/ipmt/invalid/bad.parser.error.json", Status: "M"},
		{Path: "tests/ipmt/invalid/ok.validate.error.json", Status: "M"},
		// a regenerated .md is generated too: alone it is not its own source
		{Path: "tests/ipmt/ipmt-spec/m.md", Status: "M"},
		// md-embed rehash of a doc
		{Path: "docs/x.md", Status: "M"},
		// new untracked file with no tracked sibling
		{Path: "tests/ipmt/files-md/flow.ipm.svg", Status: "?", NoTrackedSibling: true},
		// untracked file whose stem has a tracked sibling is expected
		{Path: "tests/ipmt/files-md/a.extra.json", Status: "?"},
		// deleted
		{Path: "tests/ipmt/files-md/gone.json", Status: "D"},
	}
	r := Classify(in, src)

	want := map[string][]string{
		ClassExpected: {
			"tests/ipmt/files-md/a.extra.json",
			"tests/ipmt/files-md/a.ipm.svg",
			"tests/ipmt/files-md/a.json",
			"tests/ipmt/files-md/a.layout.json",
			"tests/ipmt/invalid/bad.parser.error.json",
			"tests/layout-gen-rules/r.layout.json",
			"tests/layout-gen/c.ipm.svg",
		},
		ClassStale: {
			"tests/ipmt/invalid/ok.validate.error.json",
			"tests/ipmt/ipmt-spec/m.md",
			"tests/layout-gen-rules/q.layout.json",
			"tests/layout-gen/d.ipm.svg",
			"tests/layout-gen/d.layout.json",
		},
		ClassNewCase:   {"tests/ipmt/files-md/flow.ipm.svg"},
		ClassDeleted:   {"tests/ipmt/files-md/gone.json"},
		ClassDocRehash: {"docs/x.md"},
	}
	for class, paths := range want {
		if got := r[class]; !reflect.DeepEqual(got, paths) {
			t.Errorf("class %s:\n got  %v\n want %v", class, got, paths)
		}
	}
}

func TestStem(t *testing.T) {
	cases := map[string]string{
		"tests/ipmt/files-md/a.layout.json":        "tests/ipmt/files-md/a",
		"tests/layout-gen/b.ipm.svg":               "tests/layout-gen/b",
		"tests/ipmt/invalid/x.parser.error.json":   "tests/ipmt/invalid/x",
		"tests/ipmt/invalid/x.validate.error.json": "tests/ipmt/invalid/x",
		"tests/layout-gen-rules/r.dsl.json":        "tests/layout-gen-rules/r",
		"tests/layout-gen/b.json":                  "tests/layout-gen/b",
	}
	for in, want := range cases {
		if got := Stem(in); got != want {
			t.Errorf("Stem(%q) = %q, want %q", in, got, want)
		}
	}
}
