package main

// Which diagrams the report sweeps: the corpus file, shared with layout-audit
// and ipm-drawio's framecheck — see pkg/layoutaudit/corpus.go for the schema
// (paths, zoom bundle dirs, and the OUTLIERS every sweep skips) and why it is
// a file rather than a flag.

import "github.com/infinite-pm/ipm-tools/pkg/layoutaudit"

// DefaultCorpusName is looked for beside the repository being reported on.
const DefaultCorpusName = layoutaudit.DefaultCorpusName

// Corpus is the shared corpus config.
type Corpus = layoutaudit.Corpus

const corpusExample = layoutaudit.CorpusExample

// loadCorpus reads the corpus at path. A missing file is not an error: the
// tool then sweeps this repository's own defaults, as it always has.
func loadCorpus(path string) (*Corpus, error) {
	return layoutaudit.LoadCorpus(path)
}
