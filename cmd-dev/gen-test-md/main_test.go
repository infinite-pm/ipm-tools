package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadRuleFilesRealisticNames is a regression test for the bug where
// loadRuleFiles globbed *.dsl and required a non-existent "rule-<int>.dsl"
// filename shape, so every real (anchor-derived) fixture was skipped and an
// empty slice was returned. It builds a rules dir with realistically-named
// .dsl files and asserts they are loaded with the line numbers taken from
// _refs.json (item.Line), not parsed out of the filename.
func TestLoadRuleFilesRealisticNames(t *testing.T) {
	base := t.TempDir()
	rulesDir := filepath.Join(base, "layout-gen-rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	refs := `[
  {
    "source": "../../docs/dev/layout-gen/layout-alg.md",
    "items": [
      {
        "destination": "a-concept-at-every-layer-02",
        "line": 42,
        "outputs": {"dsl": "deadbeef"},
        "heading": "a concept at every layer",
        "section": "Layers"
      },
      {
        "destination": "edges-run-straight-by-default",
        "line": 75,
        "outputs": {"dsl": "cafef00d"},
        "heading": "edges run straight by default",
        "section": "Edge routing invariants"
      }
    ]
  }
]`
	if err := os.WriteFile(filepath.Join(rulesDir, "_refs.json"), []byte(refs), 0o644); err != nil {
		t.Fatal(err)
	}

	mustWrite := func(name, content string) {
		if err := os.WriteFile(filepath.Join(rulesDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("a-concept-at-every-layer-02.dsl", "expect node count >= 1\n")
	mustWrite("edges-run-straight-by-default.dsl", "expect edge straight\n")

	// loadRuleFiles derives the rules dir as filepath.Dir(testDir)/layout-gen-rules,
	// so pass a sibling "layout-gen" test dir.
	got, err := loadRuleFiles(filepath.Join(base, "layout-gen"))
	if err != nil {
		t.Fatalf("loadRuleFiles error: %v", err)
	}

	// Collect the non-heading rule files keyed by name.
	byName := map[string]RuleFile{}
	for _, rf := range got {
		if rf.IsHeading {
			continue
		}
		byName[rf.Name] = rf
	}

	if len(byName) != 2 {
		t.Fatalf("expected 2 rule files, got %d: %+v", len(byName), got)
	}
	if rf, ok := byName["a-concept-at-every-layer-02.dsl"]; !ok || rf.LineNum != 42 {
		t.Errorf("a-concept-at-every-layer-02.dsl: ok=%v line=%d (want line 42)", ok, rf.LineNum)
	}
	if rf, ok := byName["edges-run-straight-by-default.dsl"]; !ok || rf.LineNum != 75 {
		t.Errorf("edges-run-straight-by-default.dsl: ok=%v line=%d (want line 75)", ok, rf.LineNum)
	}
}
