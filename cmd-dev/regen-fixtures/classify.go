package main

import (
	"path"
	"sort"
	"strings"
)

// Classes of a changed file after a regeneration run.
const (
	ClassExpected  = "EXPECTED"
	ClassStale     = "STALE-AT-BASE"
	ClassNewCase   = "NEW-CASE"
	ClassDeleted   = "DELETED"
	ClassDocRehash = "DOC-REHASH"
	ClassOther     = "OTHER" // changed outside tests/ and not a rehashed doc: never restored
)

// Classes lists the classes in report order.
var Classes = []string{ClassExpected, ClassDocRehash, ClassNewCase, ClassDeleted, ClassStale, ClassOther}

// Change is one line of git status: the path, the status letter (M, A, D, ?)
// and, for an untracked file, whether no tracked file shares its stem.
type Change struct {
	Path             string
	Status           string
	NoTrackedSibling bool
}

// Report maps a class to its sorted paths.
type Report map[string][]string

// stemSuffixes are the endings that make a path a fixture of one case; the
// longest match is removed. The stem rule: a case is its directory plus the
// file name without that ending, so tests/ipmt/files-md/a.layout.json and
// tests/ipmt/files-md/a.ipmt both have the stem tests/ipmt/files-md/a, and so
// do tests/layout-gen/b.ipm.svg and tests/layout-gen/b.dsl.json.
var stemSuffixes = []string{
	".parser.error.json", ".validate.error.json", ".layout.json", ".ipm.svg",
	".solved.ipmt", ".solved.md", ".defaults.ipmt", ".defaults.md",
	".dsl.json", ".ipmt-invalid", ".ipmt", ".json", ".md",
}

// sourceSuffixes are the endings a case source can have next to its stem.
var sourceSuffixes = []string{".ipmt", ".md", ".dsl.json"}

// Stem returns the case stem of a fixture path (directory kept).
func Stem(p string) string {
	best := ""
	for _, s := range stemSuffixes {
		if strings.HasSuffix(p, s) && len(s) > len(best) {
			best = s
		}
	}
	return strings.TrimSuffix(p, best)
}

// Classify sorts changed files into classes. sourceChanged says whether the
// given source path (same stem .ipmt/.md/.dsl.json) is new or modified.
// A generated file is EXPECTED when a source of its stem changed, otherwise it
// is STALE-AT-BASE. An untracked file without a tracked sibling is NEW-CASE,
// a deleted file is DELETED, a modified .md outside tests/ is DOC-REHASH (the
// md-embed hash markers). _refs.json follows the other files of its directory.
func Classify(changed []Change, sourceChanged func(path string) bool) Report {
	r := Report{}
	var refs []Change
	expectedDir := map[string]bool{}
	for _, c := range changed {
		switch {
		case c.Status == "D":
			r[ClassDeleted] = append(r[ClassDeleted], c.Path)
		case c.Status == "?" && c.NoTrackedSibling:
			r[ClassNewCase] = append(r[ClassNewCase], c.Path)
		case path.Base(c.Path) == "_refs.json":
			refs = append(refs, c)
		case !strings.HasPrefix(c.Path, "tests/") && strings.HasSuffix(c.Path, ".md") && c.Status == "M":
			r[ClassDocRehash] = append(r[ClassDocRehash], c.Path)
		case !strings.HasPrefix(c.Path, "tests/"):
			// outside tests/ and not a rehashed doc: the lead looks at it
			r[ClassOther] = append(r[ClassOther], c.Path)
		case c.Status == "?" || hasChangedSource(c.Path, sourceChanged):
			r[ClassExpected] = append(r[ClassExpected], c.Path)
			expectedDir[path.Dir(c.Path)] = true
		default:
			r[ClassStale] = append(r[ClassStale], c.Path)
		}
	}
	for _, c := range refs {
		if expectedDir[path.Dir(c.Path)] {
			r[ClassExpected] = append(r[ClassExpected], c.Path)
		} else {
			r[ClassStale] = append(r[ClassStale], c.Path)
		}
	}
	for k := range r {
		sort.Strings(r[k])
	}
	return r
}

func hasChangedSource(p string, sourceChanged func(string) bool) bool {
	// A file is never its own source: a regenerated .md or .ipmt is itself
	// generated (from the doc fence), so only its siblings count.
	stem := Stem(p)
	for _, s := range sourceSuffixes {
		if stem+s != p && sourceChanged(stem+s) {
			return true
		}
	}
	return false
}
