package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Docs must not call thing the default node type: the default is event.
var (
	badThingDefault = regexp.MustCompile("(?i)(thing.*default|default.*thing|`::t`.*optional|optional.*`::t`)")
	okEventDefault  = regexp.MustCompile("(?i)(event is the default|default node type is (now )?(an )?event|unmarked node is an event|event, default|undecided node|\\bold\\b|Options\\.DefaultNodeType)")
)

func TestDocsDoNotCallThingTheDefault(t *testing.T) {
	root := filepath.Join("..", "..")
	files := []string{
		"docs/ipmt-spec.md", "docs/how-to-model.md", "docs/ipmt-parser.md",
		"docs/ipmt-unresolved.md", "docs/dev-tools/ipmt-migrate.md",
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if !badThingDefault.MatchString(line) || okEventDefault.MatchString(line) {
				continue
			}
			// the migrate tool's own text talks about events and the flip on purpose
			if strings.HasSuffix(f, "ipmt-migrate.md") && strings.Contains(strings.ToLower(line), "event") {
				continue
			}
			t.Errorf("%s:%d: thing called the default: %s", f, i+1, line)
		}
	}
}
