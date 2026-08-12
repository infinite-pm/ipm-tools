// Command release-notes prints the CHANGELOG.md section for a tag, so a
// release carries the notes that were written for it rather than a generated
// list of commit subjects.
//
// It is also the release preflight: it fails, before anything is published,
// when the section is missing, empty, or still says "unreleased". Dating the
// heading is the last edit before tagging, and this is what enforces it.
//
//	go run ./cmd-dev/release-notes v0.4.2 > notes.md
//
// The tag may be given with or without the leading v. Sections look like
// `## 0.4.2 — 2026-08-12`; the version token is matched and the rest of the
// heading left free-form, so `— unreleased` is a valid intermediate state that
// this command rejects at release time.
//
// The sibling vscode-infinite-pm repo does the same job in
// scripts/release-notes.ts; this is the Go rewrite of it, because ipm-tools has
// no Node toolchain.
package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		die("usage: release-notes <tag>   (e.g. release-notes v0.4.2)")
	}
	want := strings.TrimPrefix(os.Args[1], "v")
	if !regexp.MustCompile(`^\d+\.\d+\.\d+`).MatchString(want) {
		die("%q is not a version tag — expected something like v0.4.2", os.Args[1])
	}

	raw, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		die("read CHANGELOG.md: %v (run from the repo root)", err)
	}
	lines := strings.Split(string(raw), "\n")

	head := regexp.MustCompile(`^##\s+v?` + regexp.QuoteMeta(want) + `(\s|$)`)
	start := -1
	for i, l := range lines {
		if head.MatchString(l) {
			start = i
			break
		}
	}
	if start < 0 {
		die("CHANGELOG.md has no %q section — write the release notes before tagging.", "## "+want)
	}
	if strings.Contains(strings.ToLower(lines[start]), "unreleased") {
		die("the %q heading still says \"unreleased\" — date it before tagging.", "## "+want)
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	body := strings.TrimSpace(strings.Join(lines[start+1:end], "\n"))
	if body == "" {
		die("the %q section of CHANGELOG.md is empty.", "## "+want)
	}
	fmt.Println(body)
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "release-notes: "+format+"\n", a...)
	os.Exit(1)
}
