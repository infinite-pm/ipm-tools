package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
)

// md wraps diagram lines in a heading and one ipmt fence.
func md(body string) string {
	return "# T\n\n```ipmt\n" + body + "```\n"
}

// proof re-checks, independently of the tool, that out is the same graph as
// in under the current grammar and under DefaultNodeType=event.
func proof(t *testing.T, in, out string) {
	t.Helper()
	orig, err := parser.Parse([]byte(in), parser.Options{DefaultNodeType: model.Thing})
	if err != nil {
		t.Fatalf("original does not parse: %v", err)
	}
	cur, err := parser.Parse([]byte(out), parser.Options{DefaultNodeType: model.Thing})
	if err != nil {
		t.Fatalf("migrated does not parse (current grammar): %v", err)
	}
	if err := sameGraph(orig, cur); err != nil {
		t.Fatalf("current-grammar graph differs: %v", err)
	}
	ev, err := parser.Parse([]byte(out), parser.Options{DefaultNodeType: model.Event})
	if err != nil {
		t.Fatalf("migrated does not parse (event default): %v", err)
	}
	if err := sameGraph(orig, ev); err != nil {
		t.Fatalf("event-default graph differs: %v", err)
	}
}

func TestMigrateDiagrams(t *testing.T) {
	cases := []struct {
		name       string
		in, want   string
		wantMarked int
	}{
		{"file one thing", "Book --> read ::e\n", "Book ::t --> read ::e\n", 1},
		{"quoted name with spaces", "\"the big book\" --> read ::e\n", "\"the big book\" ::t --> read ::e\n", 1},
		{"escaped quote inside", "\"say \\\"hi\\\" now\" --> read ::e\n", "\"say \\\"hi\\\" now\" ::t --> read ::e\n", 1},
		{"::t word inside quoted label", "\"a ::t b\" --> read ::e\n", "\"a ::t b\" ::t --> read ::e\n", 1},
		{"::t word inside tooltip", "book \"has ::t inside\" ::tip --> read ::e\n", "book ::t \"has ::t inside\" ::tip --> read ::e\n", 1},
		{"tooltip after bare name", "book \"a tip\" ::tip --> read ::e\n", "book ::t \"a tip\" ::tip --> read ::e\n", 1},
		{"edge only", "a --> b\n", "a ::t --> b ::t\n", 2},
		{"several sources", "c, d --> e\n", "c ::t, d ::t --> e ::t\n", 3},
		{"alias prefix", "x::a Name --> read ::e\n", "x::a Name ::t --> read ::e\n", 1},
		{"alias suffix, event stays, alias ref gets none",
			"read ::e\nShip ::e s::a\ns --> read\ny::a Other\ny --> read\n",
			"read ::e\nShip ::e s::a\ns --> read\ny::a Other ::t\ny --> read\n", 1},
		{"bracket label", "e1 [long label] --> e2 ::e\n", "e1 ::t [long label] --> e2 ::e\n", 1},
		{"container thing in event", "whole ::e\npart --::P--> whole\n", "whole ::e\npart ::t --::P--> whole\n", 1},
		{"container thing in thing", "wheel --::P--> car\n", "wheel ::t --::P--> car ::t\n", 2},
		{"comments and blank lines", "# a --> b\n\nbook --> read ::e\n# end\n", "# a --> b\n\nbook ::t --> read ::e\n# end\n", 1},
		{"later bare event stays", "read ::e\nreader --> read\nread --> done ::e\n", "read ::e\nreader ::t --> read\nread --> done ::e\n", 1},
		{"later bare concept stays", "idea ::c\nread ::e\nread --> idea\nidea\n", "idea ::c\nread ::e\nread --> idea\nidea\n", 0},
		{"later bare thing gets ::t", "book --> read ::e\nbook --> write ::e\n", "book ::t --> read ::e\nbook ::t --> write ::e\n", 2},
		{"undecided untouched", "maybe ::?et --> read ::e\n", "maybe ::?et --> read ::e\n", 0},
		{"already marked untouched", "a ::t --> r ::e\nidea ::c\nr --> idea\n", "a ::t --> r ::e\nidea ::c\nr --> idea\n", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := MigrateFile("x.ipmt", []byte(c.in))
			if len(r.Fences) != 1 || r.Fences[0].Status != StatusOK {
				t.Fatalf("fences: %+v", r.Fences)
			}
			if string(r.Out) != c.want {
				t.Fatalf("out:\n%q\nwant:\n%q", r.Out, c.want)
			}
			if r.Marked != c.wantMarked {
				t.Fatalf("marked %d, want %d", r.Marked, c.wantMarked)
			}
			proof(t, c.in, string(r.Out))
			// idempotent
			r2 := MigrateFile("x.ipmt", r.Out)
			if string(r2.Out) != string(r.Out) || r2.Marked != 0 {
				t.Fatalf("second run changed %q (marked %d)", r2.Out, r2.Marked)
			}
		})
	}
}

func TestMigrateMarkdownTwoFences(t *testing.T) {
	in := "# T\n\n```ipmt\nalpha --> e1 ::e\n```\n\ntext ::t stays\n\n```ipmt\ne2 ::e\nbeta --> e2\n```\n"
	want := "# T\n\n```ipmt\nalpha ::t --> e1 ::e\n```\n\ntext ::t stays\n\n```ipmt\ne2 ::e\nbeta ::t --> e2\n```\n"
	r := MigrateFile("a.md", []byte(in))
	if string(r.Out) != want {
		t.Fatalf("out:\n%q\nwant:\n%q", r.Out, want)
	}
	if len(r.Fences) != 2 || r.Fences[0].Line != 3 || r.Fences[1].Line != 9 || r.Marked != 2 || r.Nodes != 4 {
		t.Fatalf("fences %+v marked %d nodes %d", r.Fences, r.Marked, r.Nodes)
	}
	r2 := MigrateFile("a.md", r.Out)
	if string(r2.Out) != want || r2.Marked != 0 {
		t.Fatalf("not idempotent: %q", r2.Out)
	}
}

func TestMigrateCRLF(t *testing.T) {
	in := "# H\r\n\r\n```ipmt\r\nbook --> read ::e\r\n```\r\n"
	want := "# H\r\n\r\n```ipmt\r\nbook ::t --> read ::e\r\n```\r\n"
	r := MigrateFile("a.md", []byte(in))
	if string(r.Out) != want {
		t.Fatalf("out %q want %q", r.Out, want)
	}
}

func TestMigrateWrapperFenceUntouched(t *testing.T) {
	in := "# T\n\n````md\n```ipmt\nbook --> read ::e\n```\n````\n"
	r := MigrateFile("a.md", []byte(in))
	if string(r.Out) != in || len(r.Fences) != 0 {
		t.Fatalf("out %q fences %+v", r.Out, r.Fences)
	}
}

func TestMigrateUnparsableSkipped(t *testing.T) {
	in := "# T\n\n```ipmt\nbad ::e ::c\n```\n\n```ipmt\nbook --> read ::e\n```\n"
	want := "# T\n\n```ipmt\nbad ::e ::c\n```\n\n```ipmt\nbook ::t --> read ::e\n```\n"
	r := MigrateFile("a.md", []byte(in))
	if string(r.Out) != want {
		t.Fatalf("out %q", r.Out)
	}
	if len(r.Fences) != 2 || r.Fences[0].Status != StatusUnparsable || r.Fences[0].Line != 3 || r.Fences[1].Status != StatusOK {
		t.Fatalf("fences %+v", r.Fences)
	}
}

func TestMigrateEmptyAndNoFences(t *testing.T) {
	for _, in := range []string{"", "# only prose ::t\n", "\n"} {
		r := MigrateFile("a.md", []byte(in))
		if string(r.Out) != in || len(r.Fences) != 0 {
			t.Fatalf("%q -> %q %+v", in, r.Out, r.Fences)
		}
	}
	r := MigrateFile("e.ipmt", nil)
	if len(r.Out) != 0 || r.Fences[0].Status != StatusOK {
		t.Fatalf("empty ipmt: %+v", r)
	}
}

// A check that fails must leave the text unchanged and be listed as FAIL.
func TestMigrateFailedProofNotWritten(t *testing.T) {
	corrupt = func(s string) string { return strings.Replace(s, "read", "reed", 1) }
	defer func() { corrupt = nil }()
	in := "book --> read ::e\n"
	r := MigrateFile("x.ipmt", []byte(in))
	if string(r.Out) != in || r.Fences[0].Status != StatusFail {
		t.Fatalf("out %q fences %+v", r.Out, r.Fences)
	}
}

func gitRepo(t *testing.T, files map[string]string, track ...string) string {
	t.Helper()
	dir := t.TempDir()
	for n, c := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, append([]string{"add"}, track...)} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return dir
}

func TestCLIRootDryRunAndWrite(t *testing.T) {
	dir := gitRepo(t, map[string]string{
		"a.ipmt":   "book --> read ::e\n",
		"u.ipmt":   "untracked --> read ::e\n",
		"n.md":     md("bad ::e ::c\n"),
		"notes.go": "package x\n",
	}, "a.ipmt", "n.md", "notes.go")

	var out, errb bytes.Buffer
	if code := run([]string{"-root", dir}, &out, &errb); code != 0 {
		t.Fatalf("dry-run exit %d: %s %s", code, out.String(), errb.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.ipmt")); string(b) != "book --> read ::e\n" {
		t.Fatalf("dry-run wrote: %q", b)
	}
	rep := out.String()
	for _, want := range []string{"a.ipmt: fences=1 nodes=2 marked=1", "TOTAL", "marked=1", "unparsable", "n.md:3"} {
		if !strings.Contains(rep, want) {
			t.Fatalf("report lacks %q:\n%s", want, rep)
		}
	}
	if strings.Contains(rep, "u.ipmt") {
		t.Fatalf("untracked file in report:\n%s", rep)
	}

	out.Reset()
	if code := run([]string{"-root", dir, "-write"}, &out, &errb); code != 0 {
		t.Fatalf("write exit %d: %s", code, errb.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.ipmt")); string(b) != "book ::t --> read ::e\n" {
		t.Fatalf("not written: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "u.ipmt")); string(b) != "untracked --> read ::e\n" {
		t.Fatalf("untracked touched: %q", b)
	}
}

func TestCLIInputErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(nil, &out, &errb); code != 2 || errb.Len() == 0 {
		t.Fatalf("no input: exit %d %q", code, errb.String())
	}
	errb.Reset()
	if code := run([]string{"-root", t.TempDir()}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "git") {
		t.Fatalf("non-git root: exit %d %q", code, errb.String())
	}
	errb.Reset()
	if code := run([]string{"/nonexistent/x.ipmt"}, &out, &errb); code != 2 {
		t.Fatalf("missing file: exit %d", code)
	}
}

func TestCLIFailExitsOne(t *testing.T) {
	corrupt = func(s string) string { return strings.Replace(s, "read", "reed", 1) }
	defer func() { corrupt = nil }()
	f := filepath.Join(t.TempDir(), "x.ipmt")
	os.WriteFile(f, []byte("book --> read ::e\n"), 0o644)
	var out, errb bytes.Buffer
	if code := run([]string{"-write", f}, &out, &errb); code != 1 || !strings.Contains(out.String(), "FAIL") {
		t.Fatalf("exit %d out %s", code, out.String())
	}
	if b, _ := os.ReadFile(f); string(b) != "book --> read ::e\n" {
		t.Fatalf("changed on FAIL: %q", b)
	}
}

func TestProseReport(t *testing.T) {
	in := "# Title\n\nThe default node type is thing; ::t is optional.\n\n```ipmt\n# comment: unmarked nodes are things by default\nbook ::t --> read ::e\n```\n\n```ipmt\na --> b\n```\n"
	got := strings.Join(ProseReport("d.md", []byte(in)), "\n")
	for _, want := range []string{
		"d.md:3: The default node type is thing; ::t is optional.",
		"d.md:6: # comment: unmarked nodes are things by default",
		"d.md:5: ipmt fence with explicit ::t",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "d.md:7") || strings.Contains(got, "d.md:9") || strings.Contains(got, "d.md:10") {
		t.Fatalf("fence body or plain fence listed:\n%s", got)
	}
	prose := MigrateFile("d.md", []byte(in))
	if !strings.Contains(string(prose.Out), "The default node type is thing; ::t is optional.") {
		t.Fatalf("migration changed prose")
	}
}
