package main

// Where the old engines live.
//
// A rebase does not delete history, it relocates it: the published repository
// carries a squash, and the lineages it was squashed from sit in a sibling
// checkout on branches nobody has checked out. Passing --repo/--rev/--sources
// for each of them on every invocation is how a useful command stops being
// run, so the layout is written down once instead.
//
// The file is LOCAL: it names paths outside the repository, which exist only
// on the machine that has those checkouts. It is gitignored, and
// `--config-example` prints one to start from.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultConfigName is looked for beside the repository being reported on.
const DefaultConfigName = "layout-history.json"

// Source is one lineage: a repository, a ref in it, and what counts as the
// engine there (package layouts change across a rewrite — pkg/layoutpasses
// became pkg/layout7).
type Source struct {
	Name        string   `json:"name"`
	Repo        string   `json:"repo"`
	Rev         string   `json:"rev"`
	EnginePaths []string `json:"enginePaths,omitempty"`

	// tip is the ref's newest commit; resolved at load time. Sources are
	// chained by it — see chainWindows.
	tip     string
	tipDate time.Time
}

// Config is the whole history, oldest lineage first.
type Config struct {
	Sources  []Source `json:"sources"`
	Diagrams []string `json:"diagrams,omitempty"`
	// EnginePaths is the default for sources that do not name their own.
	EnginePaths []string `json:"enginePaths,omitempty"`

	dir string // the config's own directory; relative paths resolve against it
}

const configExample = `{
  "_comment": "Where this project's engine history lives. Oldest lineage first;",
  "_comment2": "each one contributes the commits after the previous one's tip,",
  "_comment3": "which is exactly what a rebase does to a history.",
  "sources": [
    {
      "name": "pre2",
      "repo": "../pre-ipm-tools",
      "rev": "main-pre2",
      "enginePaths": ["pkg/layout", "pkg/layoutpasses", "cmd/layout-gen"]
    },
    {
      "name": "pre1",
      "repo": "../pre-ipm-tools",
      "rev": "main-pre1"
    },
    {
      "name": "main",
      "repo": ".",
      "rev": "HEAD"
    }
  ],
  "enginePaths": ["pkg/layout7", "pkg/layout", "cmd/layout-gen"],
  "diagrams": ["tests/layout-gen", "tests/layout-gen-ext", "examples", "docs"]
}
`

// loadConfig reads the config at path. A missing file is not an error: the
// tool then works on one repository, as it always has.
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(c.Sources) == 0 {
		return nil, fmt.Errorf("%s: no sources", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	c.dir = filepath.Dir(abs)
	for i := range c.Sources {
		s := &c.Sources[i]
		if s.Repo == "" || s.Rev == "" {
			return nil, fmt.Errorf("%s: source %q needs both repo and rev", path, s.Name)
		}
		if !filepath.IsAbs(s.Repo) {
			s.Repo = filepath.Join(c.dir, s.Repo)
		}
		if s.Name == "" {
			s.Name = filepath.Base(s.Repo) + "@" + s.Rev
		}
		if len(s.EnginePaths) == 0 {
			s.EnginePaths = c.EnginePaths
		}
	}
	return &c, nil
}

// resolveTips asks each source for its ref's newest commit and orders the
// sources by it, oldest first — the order a reader would tell the story in.
func (c *Config) resolveTips() error {
	for i := range c.Sources {
		s := &c.Sources[i]
		sha, err := Git(s.Repo, "rev-parse", s.Rev+"^{commit}")
		if err != nil {
			return fmt.Errorf("source %q: resolve %s in %s: %w", s.Name, s.Rev, s.Repo, err)
		}
		s.tip = sha
		ts, err := Git(s.Repo, "log", "-1", "--format=%cI", sha)
		if err != nil {
			return fmt.Errorf("source %q: date of %s: %w", s.Name, sha, err)
		}
		if s.tipDate, err = time.Parse(time.RFC3339, ts); err != nil {
			return fmt.Errorf("source %q: parse date %q: %w", s.Name, ts, err)
		}
	}
	sort.SliceStable(c.Sources, func(i, j int) bool {
		return c.Sources[i].tipDate.Before(c.Sources[j].tipDate)
	})
	return nil
}

// chainWindows gives each source the span it OWNS: everything after the
// previous source's tip, up to its own.
//
// This is the rule that makes a rebased history read as one line. Lineages
// overlap in wall-clock time — a branch rewritten in July still carries
// commits dated March — so slicing by "when a lineage's commits happen"
// double-counts the rewritten past. Slicing by "what each lineage added after
// the one it replaced" does not, and matches what a rebase actually did.
func (c *Config) chainWindows(until time.Time) []window {
	out := make([]window, 0, len(c.Sources))
	var prev time.Time
	for _, s := range c.Sources {
		end := s.tipDate
		if end.After(until) {
			end = until
		}
		out = append(out, window{src: s, from: prev, to: end})
		if s.tipDate.After(prev) {
			prev = s.tipDate
		}
	}
	return out
}

// window is one source and the slice of time it owns.
type window struct {
	src  Source
	from time.Time // exclusive (zero = from the beginning of that history)
	to   time.Time // inclusive
}

func (w window) String() string {
	return fmt.Sprintf("%s (%s@%s) %s → %s", w.src.Name, filepath.Base(w.src.Repo), w.src.Rev,
		stamp(w.from), stamp(w.to))
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return "start"
	}
	return t.Format("2006-01-02")
}

// describeSources is the one-line-per-lineage summary the report and --list
// print, so a reader knows which repository a column came from.
func (c *Config) describeSources() string {
	var b strings.Builder
	for i, s := range c.Sources {
		if i > 0 {
			b.WriteString(" · ")
		}
		fmt.Fprintf(&b, "%s=%s@%s", s.Name, filepath.Base(s.Repo), s.Rev)
	}
	return b.String()
}
