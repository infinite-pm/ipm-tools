package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A corpus kept OUTSIDE the published repository writes its report beside
// ITSELF, so the extended run cannot overwrite the base one — whatever
// directory the tool was invoked from.
func TestCorpusReportLandsBesideItsConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultCorpusName)
	if err := os.WriteFile(path, []byte(`{
	  "name": "extended", "out": "temp/layout-timeline",
	  "paths": ["docs", "../ipm-overview"]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := loadCorpus(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.OutDir("/elsewhere"), filepath.Join(dir, "temp/layout-timeline"); got != want {
		t.Errorf("report goes to %s, want %s — beside the corpus, not the caller", got, want)
	}
	if len(c.Paths) != 2 || c.Paths[1] != "../ipm-overview" {
		t.Errorf("paths = %v", c.Paths)
	}
	if !filepath.IsAbs(c.OutDir("")) {
		t.Error("the output directory is not absolute")
	}

	// No "out": the caller's directory stands.
	bare := filepath.Join(dir, "bare.json")
	if err := os.WriteFile(bare, []byte(`{"paths":["docs"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := loadCorpus(bare)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.OutDir("/fallback"); got != "/fallback" {
		t.Errorf("a corpus with no out overrode the caller: %s", got)
	}

	// A missing file is not an error — the tool then sweeps its own defaults.
	if c, err := loadCorpus(filepath.Join(dir, "absent.json")); err != nil || c != nil {
		t.Errorf("a missing corpus should be silent, got %v / %v", c, err)
	}
	// One with no paths IS an error: it asked for nothing.
	empty := filepath.Join(dir, "empty.json")
	os.WriteFile(empty, []byte(`{"name":"x"}`), 0o644)
	if _, err := loadCorpus(empty); err == nil {
		t.Error("a corpus naming no paths was accepted")
	}
}
