package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func tabs(cols ...string) string { return strings.Join(cols, "\t") + "\n" }

const goodManifest = "# comment\n\n" +
	"rule\tthing\n" +
	"file\tdocs/a.md\te=2 t=1\tteaches the markers\n" +
	"fence\tdocs/b.md\tbook ::t --> read ::e\te=1 t=1\tone teaching fence\n" +
	"line\tdocs/b.md\tUse ::e to mark an event.\tstates the notation\n"

func TestParseManifest(t *testing.T) {
	m, err := ParseManifest([]byte(goodManifest))
	if err != nil {
		t.Fatal(err)
	}
	if m.Rule != "thing" || len(m.Entries) != 3 {
		t.Fatalf("%+v", m)
	}
	f, fe, l := m.Entries[0], m.Entries[1], m.Entries[2]
	if f.Kind != KindFile || f.Path != "docs/a.md" || f.E != 2 || f.T != 1 || f.Why != "teaches the markers" {
		t.Fatalf("file entry %+v", f)
	}
	if fe.Kind != KindFence || fe.Anchor != "book ::t --> read ::e" || fe.E != 1 || fe.T != 1 {
		t.Fatalf("fence entry %+v", fe)
	}
	if l.Kind != KindLine || l.Text != "Use ::e to mark an event." || l.Why != "states the notation" {
		t.Fatalf("line entry %+v", l)
	}
	if _, err := ParseManifest([]byte("rule\tevent\n")); err != nil {
		t.Fatalf("rule event: %v", err)
	}
}

func TestParseManifestBad(t *testing.T) {
	bad := map[string]string{
		"no rule line":      tabs("file", "a.md", "e=1 t=0", "why"),
		"bad rule":          tabs("rule", "concept"),
		"unknown kind":      "rule\tthing\n" + tabs("sheet", "a.md", "e=1 t=0", "why"),
		"file field count":  "rule\tthing\n" + tabs("file", "a.md", "e=1 t=0"),
		"fence field count": "rule\tthing\n" + tabs("fence", "a.md", "x", "e=1 t=0"),
		"bad counts":        "rule\tthing\n" + tabs("file", "a.md", "e=x t=0", "why"),
		"counts order":      "rule\tthing\n" + tabs("file", "a.md", "t=1 e=0", "why"),
		"line without mark": "rule\tthing\n" + tabs("line", "a.md", "nothing here", "why"),
		"empty why":         "rule\tthing\n" + tabs("file", "a.md", "e=1 t=0", ""),
		"empty":             "# only a comment\n",
	}
	for name, in := range bad {
		if _, err := ParseManifest([]byte(in)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for n, c := range files {
		p := filepath.Join(dir, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

var mutRepo = map[string]string{
	"docs/a.md":             "# A\n\n```ipmt\nbook ::t --> read ::e\n```\n\nand ::e again\n",
	"docs/b.md":             "# B\n\nUse ::e to mark an event.\n\n```ipmt\nbook ::t --> read ::e\n```\n\n```ipmt\nother --> thing ::e\n```\n",
	"ipmt-markers.manifest": goodManifest,
}

func checkRepo(t *testing.T, files map[string]string) []string {
	t.Helper()
	dir := writeFiles(t, files)
	fails, err := CheckManifestRoot(dir, filepath.Join(dir, "ipmt-markers.manifest"))
	if err != nil {
		t.Fatal(err)
	}
	return fails
}

func TestCheckManifestClean(t *testing.T) {
	if fails := checkRepo(t, mutRepo); len(fails) != 0 {
		t.Fatalf("unexpected FAILs: %v", fails)
	}
}

func mutated(file, from, to string) map[string]string {
	m := map[string]string{}
	for k, v := range mutRepo {
		m[k] = v
	}
	m[file] = strings.Replace(m[file], from, to, 1)
	return m
}

func wantFail(t *testing.T, fails []string, path, sub string) {
	t.Helper()
	for _, f := range fails {
		if strings.HasPrefix(f, "FAIL "+path+":") && strings.Contains(f, sub) {
			return
		}
	}
	t.Fatalf("want FAIL %s containing %q, got %v", path, sub, fails)
}

func TestCheckManifestMutations(t *testing.T) {
	// an explicit ::e removed from a file entry
	f := checkRepo(t, mutated("docs/a.md", "and ::e again", "and again"))
	wantFail(t, f, "docs/a.md", "::e")
	// an explicit ::t removed from a file entry
	f = checkRepo(t, mutated("docs/a.md", "book ::t -->", "book -->"))
	wantFail(t, f, "docs/a.md", "::t")
	// the anchor line of a fence entry gone
	f = checkRepo(t, mutated("docs/b.md", "```ipmt\nbook ::t --> read ::e\n```", "```ipmt\nbook --> read\n```"))
	wantFail(t, f, "docs/b.md", "anchor")
	// a fence entry whose fence lost a marker but kept the anchor
	f = checkRepo(t, mutated("docs/b.md", "other --> thing ::e", "other --> thing ::e"))
	if len(f) != 0 {
		t.Fatalf("unrelated fence changed: %v", f)
	}
	// the line entry text edited
	f = checkRepo(t, mutated("docs/b.md", "Use ::e to mark", "Use ::e to flag"))
	wantFail(t, f, "docs/b.md", "line")
	// a missing file
	m := map[string]string{"docs/b.md": mutRepo["docs/b.md"], "ipmt-markers.manifest": goodManifest}
	f = checkRepo(t, m)
	wantFail(t, f, "docs/a.md", "")
}

func TestCheckManifestAnchorAmbiguous(t *testing.T) {
	files := map[string]string{
		"docs/a.md":             mutRepo["docs/a.md"],
		"docs/b.md":             mutRepo["docs/b.md"] + "\n```ipmt\nbook ::t --> read ::e\n```\n",
		"ipmt-markers.manifest": goodManifest,
	}
	wantFail(t, checkRepo(t, files), "docs/b.md", "2 fences")
}

func TestCheckManifestMalformedAndMissing(t *testing.T) {
	dir := writeFiles(t, map[string]string{"ipmt-markers.manifest": "rule\tnope\n"})
	fails, err := CheckManifestRoot(dir, filepath.Join(dir, "ipmt-markers.manifest"))
	if err != nil || len(fails) != 1 || !strings.HasPrefix(fails[0], "FAIL ") {
		t.Fatalf("malformed: %v %v", fails, err)
	}
	dir = t.TempDir()
	fails, err = CheckManifestRoot(dir, filepath.Join(dir, "ipmt-markers.manifest"))
	if err != nil || len(fails) != 1 || !strings.HasPrefix(fails[0], "FAIL ") {
		t.Fatalf("missing: %v %v", fails, err)
	}
}

func TestCLICheckManifest(t *testing.T) {
	var out, errb bytes.Buffer
	dir := writeFiles(t, mutRepo)
	if code := run([]string{"-check-manifest", "-root", dir}, &out, &errb); code != 0 {
		t.Fatalf("clean exit %d: %s %s", code, out.String(), errb.String())
	}
	if err := os.WriteFile(filepath.Join(dir, "docs/a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := run([]string{"-check-manifest", "-root", dir}, &out, &errb); code != 1 || !strings.Contains(out.String(), "FAIL docs/a.md:") {
		t.Fatalf("broken exit %d: %s", code, out.String())
	}
	errb.Reset()
	if code := run([]string{"-check-manifest"}, &out, &errb); code != 2 {
		t.Fatalf("no root: exit %d", code)
	}
	if code := run([]string{"-check-manifest", "-root", dir, "-manifest", dir}, &out, &errb); code != 2 {
		t.Fatalf("unreadable manifest (a directory): exit %d", code)
	}
}

// repoRoot walks up to go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	d, _ := os.Getwd()
	for ; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	t.Fatal("no repo root")
	return ""
}

// The marked places of this repo keep their explicit ::e and ::t.
func TestRepoManifest(t *testing.T) {
	root := repoRoot(t)
	fails, err := CheckManifestRoot(root, filepath.Join(root, "ipmt-markers.manifest"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fails {
		t.Error(f)
	}
}
