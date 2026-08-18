package layoutaudit

// The CORPUS: which diagrams a sweep measures — and which it deliberately
// leaves out.
//
// The engine belongs to this repository; the diagrams need not. An engine
// change is only interesting for what it does to real documents, and most of
// those live in sibling repositories the published repo cannot name — so the
// corpus is a FILE, not a flag (docs/dev-tools/layout-timeline.md). Two of
// them in practice: the BASE corpus is this repository's own default paths;
// an EXTENDED corpus lives outside the published repo, names sibling checkouts
// by relative path, and writes its report next to itself.
//
// OUTLIERS: a corpus diagram whose structural size is far outside the rest
// (`layout-debug --stats`: a composite of 45 members where the next has 4, a
// hub with 60 part-of edges where the next has 6) exercises the engine at a
// scale no real story does, and a metric summed over the corpus becomes a
// metric of that one diagram — one that rewards whatever makes ITS picture
// less bad. Such a diagram is listed under "outliers" with the reason; every
// sweep (layout-audit, layout-timeline, ipm-drawio's framecheck) skips it and
// says so. Moderate cases built from its shapes belong in the fixture corpora
// instead. The rule of thumb for the list: a size number more than three
// times the next-highest in the corpus.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultCorpusName is looked for beside the repository being reported on.
const DefaultCorpusName = "layout-corpus.json"

// Corpus is a named set of diagram paths, where its report goes, and what it
// leaves out.
type Corpus struct {
	// Name distinguishes one report from another in the header.
	Name string `json:"name,omitempty"`
	// Paths are what to sweep, relative to the diagram root (or absolute).
	Paths []string `json:"paths"`
	// Zoom are directories of zoom BUNDLES — every `X.ipmt` beside an
	// `X.zoom.html` — for the click-path sweeps (ipm-drawio framecheck).
	Zoom []string `json:"zoom,omitempty"`
	// Outliers are diagrams every sweep skips, each with its reason.
	Outliers []Outlier `json:"outliers,omitempty"`
	// Out is where the report is written, relative to THIS FILE — so a corpus
	// kept in ipm-drawio writes into ipm-drawio without naming a machine.
	Out string `json:"out,omitempty"`

	dir string // the config's own directory
}

// Outlier names a diagram (a glob on its base name, or on its path relative
// to the diagram root) the sweeps leave out, and why.
type Outlier struct {
	Glob string `json:"glob"`
	Why  string `json:"why,omitempty"`
}

// CorpusExample is a corpus config to start from.
const CorpusExample = `{
  "_comment": "Diagrams to sweep. Paths are relative to the diagram root",
  "_comment2": "(--sources, default --repo), so a sibling checkout is ../name.",
  "_comment3": "Keep this file OUTSIDE the published repo if it names private ones.",
  "_comment4": "outliers: diagrams every sweep skips (glob on the base name or the",
  "_comment5": "relative path), each with why — see pkg/layoutaudit/corpus.go.",
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
  ],
  "zoom": [
    "../infinite-pm-lab/docs/260606-sstcorpus"
  ],
  "outliers": [
    {"glob": "NDA*.ipmt", "why": "45-member grids of tall clauses, a hub with 60 part-of edges, a 183-step chain: a contract, not a story"}
  ]
}
`

// LoadCorpus reads the corpus at path. A missing file is not an error: the
// tool then sweeps this repository's own defaults, as it always has.
func LoadCorpus(path string) (*Corpus, error) {
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
	if len(c.Paths) == 0 && len(c.Zoom) == 0 {
		return nil, fmt.Errorf("%s: no paths", path)
	}
	for _, o := range c.Outliers {
		if o.Glob == "" {
			return nil, fmt.Errorf("%s: an outlier without a glob", path)
		}
		if _, err := filepath.Match(o.Glob, ""); err != nil {
			return nil, fmt.Errorf("%s: outlier glob %q: %v", path, o.Glob, err)
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	c.dir = filepath.Dir(abs)
	return &c, nil
}

// Dir is the config's own directory ("" for a nil corpus).
func (c *Corpus) Dir() string {
	if c == nil {
		return ""
	}
	return c.dir
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
	return fmt.Sprintf("%s (%d path(s), %d outlier(s), %s)", c.Name, len(c.Paths), len(c.Outliers), filepath.Join(c.dir, DefaultCorpusName))
}

// Outlier reports whether path (a file path or a diagram ID such as
// "../lab/docs/x.md#100") is on the outlier list — matched by base name and
// by the whole path, the "#block" suffix and any ".nodefaults"/".sst"
// variant infix ignored so one glob names a document in every form it comes
// in — and the reason it is.
func (c *Corpus) Outlier(path string) (why string, ok bool) {
	if c == nil {
		return "", false
	}
	p := path
	if i := strings.LastIndex(p, "#"); i >= 0 {
		p = p[:i]
	}
	base := filepath.Base(p)
	// "NDA.nodefaults.ipmt" and "NDA.sst.ipmt" are NDA in another lane
	stripped := base
	for _, infix := range []string{".nodefaults", ".sst"} {
		stripped = strings.Replace(stripped, infix, "", 1)
	}
	for _, o := range c.Outliers {
		for _, cand := range []string{base, stripped, p, filepath.ToSlash(p)} {
			if m, _ := filepath.Match(o.Glob, cand); m {
				return o.Why, true
			}
		}
	}
	return "", false
}

// ExcludeFunc is the filter Collect takes: true for a diagram to skip.
func (c *Corpus) ExcludeFunc() func(path string) (string, bool) {
	if c == nil {
		return nil
	}
	return c.Outlier
}
