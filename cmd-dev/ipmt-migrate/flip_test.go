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

// flipProof re-checks, independently of the tool: old text under the current
// default equals new text under the event default.
func flipProof(t *testing.T, in, out string) {
	t.Helper()
	orig, err := parser.Parse([]byte(in), parser.Options{DefaultNodeType: model.Thing})
	if err != nil {
		t.Fatalf("original does not parse: %v", err)
	}
	ev, err := parser.Parse([]byte(out), parser.Options{DefaultNodeType: model.Event})
	if err != nil {
		t.Fatalf("flipped does not parse (event default): %v\n%s", err, out)
	}
	if err := sameGraph(orig, ev); err != nil {
		t.Fatalf("old/thing != new/event: %v\n%s", err, out)
	}
}

func TestFlipDiagrams(t *testing.T) {
	cases := []struct {
		name, in, want string
		marked, unmark int
	}{
		{"thing and event", "Book --> read ::e\n", "Book ::t --> read\n", 1, 1},
		{"quoted names", "\"the big book\" --> \"read it\" ::e\n", "\"the big book\" ::t --> \"read it\"\n", 1, 1},
		{"tooltip kept", "read ::e \"a tip\" ::tip\nBook --> read\n", "read \"a tip\" ::tip\nBook ::t --> read\n", 1, 1},
		{"::e word in tooltip kept", "read ::e \"has ::e inside\" ::tip\n", "read \"has ::e inside\" ::tip\n", 0, 1},
		{"alias prefix", "r::a read it ::e\nBook --> r\n", "r::a read it\nBook ::t --> r\n", 1, 1},
		{"undecided untouched", "a ::?et --> read ::e\n", "a ::?et --> read\n", 0, 1},
		{"concept kept", "read ::e --> pattern ::c\n", "read --> pattern ::c\n", 0, 1},
		{"repeated event reference", "Book --> read ::e\nCook --> read ::e\nread --> done ::e\n",
			"Book ::t --> read\nCook ::t --> read\nread --> done\n", 2, 3},
		{"several sources", "a, b --> read ::e\n", "a ::t, b ::t --> read\n", 2, 1},
		{"sub-event arrow", "read ::e --::P--> whole ::e\n", "read --::P--> whole\n", 0, 2},
		{"old unmarked event target is a thing", "Book ::t --> read\n", "Book ::t --> read ::t\n", 1, 0},
		{"event chain", "a ::e --> b ::e --> c ::e\n", "a --> b --> c\n", 0, 3},
		{"nothing to do", "x ::c\n", "x ::c\n", 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := FlipFile("d.ipmt", []byte(c.in), Keep{})
			if len(r.Fences) != 1 || r.Fences[0].Status != StatusOK {
				t.Fatalf("fences %+v", r.Fences)
			}
			if string(r.Out) != c.want {
				t.Fatalf("out %q want %q", r.Out, c.want)
			}
			if r.Marked != c.marked || r.Unmarked != c.unmark {
				t.Fatalf("marked %d unmarked %d, want %d %d", r.Marked, r.Unmarked, c.marked, c.unmark)
			}
			flipProof(t, c.in, string(r.Out))
		})
	}
}

// The proof holds, or the file is left alone, for every shape (alias after the marker,
// events with a comma list, a name that is also an alias).
func TestFlipProofOrUnchanged(t *testing.T) {
	ins := []string{
		"Ship ::e s::a\nBook --> s\n",
		"read ::e, write ::e --> done ::e\n",
		"x::a Name ::e\nx --> y ::e\ny --> x\n",
		"q::a ::e\nq --> z\n",
		"\"say \\\"hi\\\"\" ::e --> b\n",
		"a --> b ::e # trailing comment ::e\n",
	}
	for _, in := range ins {
		r := FlipFile("d.ipmt", []byte(in), Keep{})
		if len(r.Fences) != 1 {
			t.Fatalf("%q: %+v", in, r.Fences)
		}
		switch r.Fences[0].Status {
		case StatusOK:
			flipProof(t, in, string(r.Out))
		default:
			if string(r.Out) != in {
				t.Fatalf("%q: status %s but text changed to %q", in, r.Fences[0].Status, r.Out)
			}
		}
	}
}

func TestFlipCRLFAndMarkdown(t *testing.T) {
	in := "# T\r\n\r\nprose ::e stays\r\n\r\n```ipmt\r\nBook --> read ::e\r\n```\r\n"
	r := FlipFile("d.md", []byte(in), Keep{})
	want := "# T\r\n\r\nprose ::e stays\r\n\r\n```ipmt\r\nBook ::t --> read\r\n```\r\n"
	if string(r.Out) != want {
		t.Fatalf("out %q", r.Out)
	}
}

func TestFlipKeptPlaces(t *testing.T) {
	kept := "Book ::t --> read ::e\n"
	other := "Cook ::t --> eat ::e\n"
	in := md(kept) + "\n" + md(other)
	// kept by anchor: that fence is untouched, the other is flipped
	r := FlipFile("d.md", []byte(in), Keep{Anchors: []string{"Book ::t --> read ::e"}})
	want := md(kept) + "\n" + md("Cook ::t --> eat\n")
	if string(r.Out) != want || r.Kept != 1 {
		t.Fatalf("out %q kept %d", r.Out, r.Kept)
	}
	// kept by file: nothing changes
	r = FlipFile("d.md", []byte(in), Keep{File: true})
	if string(r.Out) != in || r.Kept != 2 {
		t.Fatalf("file keep: kept %d out %q", r.Kept, r.Out)
	}
	// a .ipmt file kept as a whole
	r = FlipFile("k.ipmt", []byte(kept), Keep{File: true})
	if string(r.Out) != kept || r.Kept != 1 {
		t.Fatalf("ipmt keep: %d %q", r.Kept, r.Out)
	}
}

// A kept place keeps its explicit ::e, but an unmarked thing in it gets ::t:
// after the flip it would otherwise turn into an event.
func TestFlipKeptGetsThingMarkOnly(t *testing.T) {
	in := md("Book --> read ::e\n")
	r := FlipFile("x.md", []byte(in), Keep{File: true})
	want := md("Book ::t --> read ::e\n")
	if string(r.Out) != want || r.Kept != 1 || r.Marked != 1 || r.Unmarked != 0 {
		t.Fatalf("out %q kept %d marked %d unmarked %d", r.Out, r.Kept, r.Marked, r.Unmarked)
	}
	flipProof(t, "Book --> read ::e\n", "Book ::t --> read ::e\n")
}

func TestFlipUnparsableLeftAlone(t *testing.T) {
	in := md("bad ::e ::c\n")
	r := FlipFile("n.md", []byte(in), Keep{})
	if string(r.Out) != in || len(r.Fences) != 1 || r.Fences[0].Status != StatusUnparsable {
		t.Fatalf("%+v %q", r.Fences, r.Out)
	}
}

func TestFlipFailedProofNotWritten(t *testing.T) {
	corrupt = func(s string) string { return strings.Replace(s, "read", "reed", 1) }
	defer func() { corrupt = nil }()
	in := "Book --> read ::e\n"
	r := FlipFile("x.ipmt", []byte(in), Keep{})
	if string(r.Out) != in || len(r.Fences) != 1 || r.Fences[0].Status != StatusFail {
		t.Fatalf("out %q fences %+v", r.Out, r.Fences)
	}
}

// flipRepo makes a git repo with a flippable file and the manifest.
func flipRepo(t *testing.T, manifest string) string {
	t.Helper()
	return gitRepo(t, map[string]string{
		"a.ipmt":                "Book --> read ::e\n",
		"kept.md":               md("Cook ::t --> eat ::e\n"),
		"ipmt-markers.manifest": manifest,
	}, "a.ipmt", "kept.md")
}

const keepManifest = "rule\tthing\nfile\tkept.md\te=1 t=1\tteaching\n"

// A tracked symlink to a flipped file must not make the tool process the target twice
// (the second pass would read flipped text with the old default and add ::t to events).
func TestCLIFlipSkipsTrackedSymlinks(t *testing.T) {
	dir := gitRepo(t, map[string]string{
		"a.ipmt":                "Book --> read ::e\n",
		"ipmt-markers.manifest": "rule\tthing\n",
	}, "a.ipmt", "ipmt-markers.manifest")
	if err := os.Symlink("a.ipmt", filepath.Join(dir, "link.ipmt")); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command("git", "-C", dir, "add", "link.ipmt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, b)
	}
	var out, errb bytes.Buffer
	if code := run([]string{"-flip", "-write", "-root", dir}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s %s", code, out.String(), errb.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.ipmt")); string(b) != "Book ::t --> read\n" {
		t.Fatalf("flipped not exactly once: %q", b)
	}
	if !strings.Contains(out.String(), "TOTAL (write) files=1 changed=1 ") {
		t.Fatalf("file not counted once:\n%s", out.String())
	}
}

func TestCLIFlipDryRunAndWrite(t *testing.T) {
	dir := flipRepo(t, keepManifest)
	var out, errb bytes.Buffer
	if code := run([]string{"-flip", "-root", dir}, &out, &errb); code != 0 {
		t.Fatalf("dry exit %d: %s %s", code, out.String(), errb.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.ipmt")); string(b) != "Book --> read ::e\n" {
		t.Fatalf("dry run wrote %q", b)
	}
	for _, want := range []string{"a.ipmt: fences=1 things-marked=1 events-unmarked=1",
		"TOTAL (dry-run) files=2 changed=1 fences=2 things-marked=1 events-unmarked=1 kept=1 unparsable=0 fail=0"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("report lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	if code := run([]string{"-flip", "-write", "-root", dir}, &out, &errb); code != 0 {
		t.Fatalf("write exit %d: %s %s", code, out.String(), errb.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.ipmt")); string(b) != "Book ::t --> read\n" {
		t.Fatalf("not written: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "kept.md")); string(b) != md("Cook ::t --> eat ::e\n") {
		t.Fatalf("kept file changed: %q", b)
	}
	mb, _ := os.ReadFile(filepath.Join(dir, "ipmt-markers.manifest"))
	if !strings.HasPrefix(string(mb), "rule\tevent\n") {
		t.Fatalf("rule line not set: %q", mb)
	}
	// second run: already flipped, nothing changes (never re-adds ::t)
	out.Reset()
	if code := run([]string{"-flip", "-write", "-root", dir}, &out, &errb); code != 0 || !strings.Contains(out.String(), "already flipped") {
		t.Fatalf("second run exit %d: %s", code, out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.ipmt")); string(b) != "Book ::t --> read\n" {
		t.Fatalf("second run changed: %q", b)
	}
}

func TestCLIFlipRuleEventUntouched(t *testing.T) {
	dir := flipRepo(t, "rule\tevent\n")
	var out, errb bytes.Buffer
	if code := run([]string{"-flip", "-write", "-root", dir}, &out, &errb); code != 0 || !strings.Contains(out.String(), "already flipped") {
		t.Fatalf("exit %d: %s %s", code, out.String(), errb.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.ipmt")); string(b) != "Book --> read ::e\n" {
		t.Fatalf("changed: %q", b)
	}
}

func TestCLIFlipNeedsManifest(t *testing.T) {
	dir := gitRepo(t, map[string]string{"a.ipmt": "Book --> read ::e\n"}, "a.ipmt")
	var out, errb bytes.Buffer
	if code := run([]string{"-flip", "-root", dir}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "manifest") {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	errb.Reset()
	f := filepath.Join(dir, "a.ipmt")
	if code := run([]string{"-flip", f}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "manifest") {
		t.Fatalf("explicit file without -manifest: exit %d: %s", code, errb.String())
	}
}

func TestCLIFlipKeptFileKeepsE(t *testing.T) {
	dir := flipRepo(t, "rule\tthing\nfile\ta.ipmt\te=1 t=0\tteaching\n")
	var out, errb bytes.Buffer
	if code := run([]string{"-flip", "-write", "-root", dir}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s %s", code, out.String(), errb.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.ipmt")); string(b) != "Book ::t --> read ::e\n" {
		t.Fatalf("kept file: %q", b)
	}
	if !strings.Contains(out.String(), "kept=1") {
		t.Fatalf("report: %s", out.String())
	}
}

func TestCLIFlipFailKeepsRule(t *testing.T) {
	corrupt = func(s string) string { return strings.Replace(s, "read", "reed", 1) }
	defer func() { corrupt = nil }()
	dir := flipRepo(t, keepManifest)
	var out, errb bytes.Buffer
	if code := run([]string{"-flip", "-write", "-root", dir}, &out, &errb); code != 1 || !strings.Contains(out.String(), "FAIL") {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.ipmt")); string(b) != "Book --> read ::e\n" {
		t.Fatalf("changed on FAIL: %q", b)
	}
	mb, _ := os.ReadFile(filepath.Join(dir, "ipmt-markers.manifest"))
	if !strings.HasPrefix(string(mb), "rule\tthing\n") {
		t.Fatalf("rule changed on failure: %q", mb)
	}
}

// The invalid lane: text that must not parse. The proof is the equal error message
// under the old default (thing) and the new one (event).
func TestFlipInvalidSameError(t *testing.T) {
	in := "A --> B\nB --> C\nA --> B\n"
	r := FlipFile("dup.ipmt-invalid", []byte(in), Keep{})
	if len(r.Fences) != 1 || r.Fences[0].Status != StatusOK {
		t.Fatalf("fences %+v", r.Fences)
	}
	want := "A ::t --> B ::t\nB ::t --> C ::t\nA ::t --> B ::t\n"
	if string(r.Out) != want || r.Marked != 6 {
		t.Fatalf("out %q marked %d", r.Out, r.Marked)
	}
	for _, d := range []model.NodeType{model.Thing, model.Event} {
		src := in
		if d == model.Event {
			src = string(r.Out)
		}
		if _, err := parser.Parse([]byte(src), parser.Options{DefaultNodeType: d}); err == nil || !strings.Contains(err.Error(), "duplicate edge") {
			t.Fatalf("default %v: error %v", d, err)
		}
	}
}

// The conflict case: the explicit ::t added to both things keeps the old PartOf
// reading, so the message stays the same and the flip is proven.
func TestFlipInvalidConflictStaysSame(t *testing.T) {
	in := "tA --> tB\ntA --::N-- tB\n"
	r := FlipFile("n.ipmt-invalid", []byte(in), Keep{})
	want := "tA ::t --> tB ::t\ntA ::t --::N-- tB ::t\n"
	if len(r.Fences) != 1 || r.Fences[0].Status != StatusOK || string(r.Out) != want {
		t.Fatalf("fences %+v out %q", r.Fences, r.Out)
	}
}

// A changed message, or a flipped text that parses, is listed as FAIL with both
// messages and the text stays unchanged (the owner rewrites the fixture by hand).
// The flip itself almost never changes a message, so the damage is simulated.
func TestFlipInvalidMessageChangesIsFail(t *testing.T) {
	defer func() { corrupt = nil }()
	in := "A --> B\nB --> C\nA --> B\n"
	corrupt = func(string) string { return "a, b --> c, d\n" }
	r := FlipFile("n.ipmt-invalid", []byte(in), Keep{})
	if len(r.Fences) != 1 || r.Fences[0].Status != StatusFail || string(r.Out) != in {
		t.Fatalf("fences %+v out %q", r.Fences, r.Out)
	}
	if !strings.Contains(r.Fences[0].Detail, "duplicate edge") || !strings.Contains(r.Fences[0].Detail, "multiple sources") {
		t.Fatalf("detail lacks both messages: %s", r.Fences[0].Detail)
	}
	corrupt = func(string) string { return "A --> B\n" }
	r = FlipFile("n.ipmt-invalid", []byte(in), Keep{})
	if len(r.Fences) != 1 || r.Fences[0].Status != StatusFail || string(r.Out) != in {
		t.Fatalf("parsing flipped text: fences %+v out %q", r.Fences, r.Out)
	}
}

// Text that parses but the validator rejects (self-loop): the proof is the equal
// validator error findings.
func TestFlipInvalidValidatorLane(t *testing.T) {
	in := "tA --> tA\ne1 ::e --> e1 ::e\n"
	r := FlipFile("s.ipmt-invalid", []byte(in), Keep{})
	want := "tA ::t --> tA ::t\ne1 --> e1\n"
	if len(r.Fences) != 1 || r.Fences[0].Status != StatusOK || string(r.Out) != want {
		t.Fatalf("fences %+v out %q", r.Fences, r.Out)
	}
}

func TestFlipInvalidFenceInMarkdown(t *testing.T) {
	in := "# T\n\n```ipmt\nBook --> read ::e\n```\n\ntext\n\n```ipmt-invalid\nA --> B\nB --> C\nA --> B\n```\n"
	r := FlipFile("d.md", []byte(in), Keep{})
	want := "# T\n\n```ipmt\nBook ::t --> read\n```\n\ntext\n\n```ipmt-invalid\nA ::t --> B ::t\nB ::t --> C ::t\nA ::t --> B ::t\n```\n"
	if string(r.Out) != want || len(r.Fences) != 2 {
		t.Fatalf("out %q fences %+v", r.Out, r.Fences)
	}
}

// An invalid fence that parses, or one that fails before any spec is resolved, is
// listed and left unchanged, never a crash.
func TestFlipInvalidNoHookAndParsing(t *testing.T) {
	in := md("x\n")
	in = strings.Replace(in, "```ipmt", "```ipmt-invalid", 1)
	r := FlipFile("p.md", []byte(in), Keep{})
	if string(r.Out) != in || len(r.Fences) != 1 || r.Fences[0].Status != StatusFail {
		t.Fatalf("parsing invalid: %+v %q", r.Fences, r.Out)
	}
	bad := "bad ::e ::c\n"
	r = FlipFile("b.ipmt-invalid", []byte(bad), Keep{})
	if string(r.Out) != bad || len(r.Fences) != 1 || r.Fences[0].Status != StatusUnparsable {
		t.Fatalf("no hook: %+v %q", r.Fences, r.Out)
	}
}

func TestCLIFlipPicksInvalidFiles(t *testing.T) {
	dir := gitRepo(t, map[string]string{
		"i.ipmt-invalid":        "A --> B\nB --> C\nA --> B\n",
		"ipmt-markers.manifest": "rule\tthing\n",
	}, "i.ipmt-invalid")
	var out, errb bytes.Buffer
	if code := run([]string{"-flip", "-write", "-root", dir}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s %s", code, out.String(), errb.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "i.ipmt-invalid")); string(b) != "A ::t --> B ::t\nB ::t --> C ::t\nA ::t --> B ::t\n" {
		t.Fatalf("not flipped: %q", b)
	}
}

// -invalid-only: the invalid lane only; valid fences and .ipmt files stay as they are
// (already flipped), the manifest rule is neither checked nor rewritten.
func TestFlipInvalidOnly(t *testing.T) {
	validFence := "```ipmt\nBook --> read\n```\n"
	invFence := "```ipmt-invalid\nA --> B\nB --> C\nA --> B\n```\n"
	mdIn := "# T\n\n" + validFence + "\ntext\n\n" + invFence
	mdWant := "# T\n\n" + validFence + "\ntext\n\n" +
		"```ipmt-invalid\nA ::t --> B ::t\nB ::t --> C ::t\nA ::t --> B ::t\n```\n"
	manifest := "rule\tevent\n"
	dir := gitRepo(t, map[string]string{
		"d.md":                  mdIn,
		"v.ipmt":                "Book --> read\n",
		"i.ipmt-invalid":        "A --> B\nB --> C\nA --> B\n",
		"ipmt-markers.manifest": manifest,
	}, "d.md", "v.ipmt", "i.ipmt-invalid")
	var out, errb bytes.Buffer

	if code := run([]string{"-flip", "-invalid-only", "-root", dir}, &out, &errb); code != 0 {
		t.Fatalf("dry exit %d: %s %s", code, out.String(), errb.String())
	}
	if strings.Contains(out.String(), "already flipped") || !strings.Contains(out.String(), "skipped=2") {
		t.Fatalf("report:\n%s", out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "d.md")); string(b) != mdIn {
		t.Fatalf("dry run wrote %q", b)
	}

	out.Reset()
	if code := run([]string{"-flip", "-invalid-only", "-write", "-root", dir}, &out, &errb); code != 0 {
		t.Fatalf("write exit %d: %s %s", code, out.String(), errb.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "d.md")); string(b) != mdWant {
		t.Fatalf("d.md: %q want %q", b, mdWant)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "v.ipmt")); string(b) != "Book --> read\n" {
		t.Fatalf("valid .ipmt changed: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "i.ipmt-invalid")); string(b) != "A ::t --> B ::t\nB ::t --> C ::t\nA ::t --> B ::t\n" {
		t.Fatalf("invalid file: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "ipmt-markers.manifest")); string(b) != manifest {
		t.Fatalf("manifest changed: %q", b)
	}
}

func TestCLIInvalidOnlyNeedsFlip(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"-invalid-only", "-root", "."}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "-flip") {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
}

func TestCLIFlipExplicitFilesWithManifest(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x.ipmt")
	mf := filepath.Join(dir, "m")
	os.WriteFile(f, []byte("Book --> read ::e\n"), 0o644)
	os.WriteFile(mf, []byte("rule\tthing\n"), 0o644)
	var out, errb bytes.Buffer
	if code := run([]string{"-flip", "-manifest", mf, f}, &out, &errb); code != 0 || !strings.Contains(out.String(), "things-marked=1 events-unmarked=1") {
		t.Fatalf("exit %d: %s %s", code, out.String(), errb.String())
	}
}
