package main

// Which diagrams the report sweeps.
//
// The engine belongs to THIS repository; the diagrams need not. An engine
// change is only interesting for what it does to real documents, and most of
// those live in sibling repositories — ipm-overview, ipm-graphs-mj41,
// ipm-drawio and the rest — which the published repo cannot name.
//
// So the corpus is a file, not a flag. Two of them, in practice:
//
//   - the BASE corpus is this repository's own default paths. It ships, it is
//     what CI could run, and it needs no config at all.
//   - an EXTENDED corpus lives OUTSIDE the published repo (ipm-drawio holds
//     ours), names sibling checkouts by relative path, and writes its report
//     next to itself. Nothing about it leaks into what is published.
//
// Paths are relative to the DIAGRAM ROOT (--sources, default --repo), so
// "../ipm-overview" from ipm-tools is the sibling checkout, and a diagram from
// it is named "../ipm-overview/docs/x.md#100" in the report.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultCorpusName is looked for beside the repository being reported on.
const DefaultCorpusName = "layout-corpus.json"

// Corpus is a named set of diagram paths and where its report goes.
type Corpus struct {
	// Name distinguishes one report from another in the header.
	Name string `json:"name,omitempty"`
	// Paths are what to sweep, relative to the diagram root (or absolute).
	Paths []string `json:"paths"`
	// Out is where the report is written, relative to THIS FILE — so a corpus
	// kept in ipm-drawio writes into ipm-drawio without naming a machine.
	Out string `json:"out,omitempty"`

	dir string // the config's own directory
}

const corpusExample = `{
  "_comment": "Diagrams to sweep. Paths are relative to the diagram root",
  "_comment2": "(--sources, default --repo), so a sibling checkout is ../name.",
  "_comment3": "Keep this file OUTSIDE the published repo if it names private ones.",
  "name": "extended",
  "out": "temp/layout-timeline",
  "paths": [
    "tests/layout-gen",
    "tests/layout-gen-ext",
    "examples",
    "docs",
    "../ipm-overview",
    "../ipm-graphs-mj41",
    "../ipm-drawio/docs"
  ]
}
`

// loadCorpus reads the corpus at path. A missing file is not an error: the
// tool then sweeps this repository's own defaults, as it always has.
func loadCorpus(path string) (*Corpus, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Corpus
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(c.Paths) == 0 {
		return nil, fmt.Errorf("%s: no paths", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	c.dir = filepath.Dir(abs)
	return &c, nil
}

// OutDir is where this corpus's report belongs, resolved against the config's
// own directory. A corpus kept in ipm-drawio therefore writes into ipm-drawio
// whatever directory the tool was invoked from.
func (c *Corpus) OutDir(fallback string) string {
	if c == nil || c.Out == "" {
		return fallback
	}
	if filepath.IsAbs(c.Out) {
		return c.Out
	}
	return filepath.Join(c.dir, c.Out)
}

// Describe names the corpus for the report header.
func (c *Corpus) Describe() string {
	if c == nil {
		return ""
	}
	if c.Name == "" {
		return fmt.Sprintf("%d path(s) from %s", len(c.Paths), c.dir)
	}
	return fmt.Sprintf("%s (%d path(s), %s)", c.Name, len(c.Paths), filepath.Join(c.dir, DefaultCorpusName))
}
