package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCapture lays down a raw capture the way ipm-rpc-tee would.
func writeCapture(t *testing.T, dir string, lines []rawState) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i, l := range lines {
		l.Seq = i + 1
		l.TS = fmt.Sprintf("2026-08-16T09:00:%02dZ", i)
		data, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "session-1.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func uriFor(scene, file string) string {
	return "file:///work/out/workspaces/" + scene + "/" + file
}

// typing is one line of ipmt being typed character by character, the way a
// scene types it — the states the corpus exists to hold.
func typing(scene, file, base, added string) []rawState {
	var out []rawState
	out = append(out, rawState{Cadence: "open", URI: uriFor(scene, file), Text: base})
	for i := 1; i <= len(added); i++ {
		out = append(out, rawState{Cadence: "change", URI: uriFor(scene, file), Text: base + added[:i]})
	}
	return out
}

func readManifest(t *testing.T, dir string) []entry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Scene  string  `json:"scene"`
		States []entry `json:"states"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m.States
}

func TestCurateSplitsScenesByWorkspace(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw")
	writeCapture(t, raw, append(
		typing("ipmt-preview", "40.ipmt", "e1 ::e\n", "--> e2 ::e\n"),
		typing("diagnostics", "40.ipmt", "e1 ::e\n", "--> e3 ::e\n")...))

	out := filepath.Join(dir, "corpus")
	if code := curate(raw, out, false); code != 0 {
		t.Fatalf("curate exited %d", code)
	}
	for _, scene := range []string{"ipmt-preview", "diagnostics"} {
		if _, err := os.Stat(filepath.Join(out, scene, "manifest.json")); err != nil {
			t.Fatalf("scene %s not curated: %v", scene, err)
		}
	}
}

// The corpus must be STABLE under re-capture: content-addressed names mean a
// take that types the same thing produces the same files, and `git diff`
// shows real change rather than the fact that a recording was re-run.
func TestRecaptureProducesIdenticalCorpus(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw")
	states := typing("ipmt-preview", "40.ipmt", "e1 ::e\n", "--> e2 ::e\n")
	writeCapture(t, raw, states)

	first := filepath.Join(dir, "a")
	second := filepath.Join(dir, "b")
	if code := curate(raw, first, false); code != 0 {
		t.Fatalf("curate exited %d", code)
	}
	// A second take: same typing, different timestamps and sequence numbers.
	writeCapture(t, raw, states)
	if code := curate(raw, second, false); code != 0 {
		t.Fatalf("curate exited %d", code)
	}

	a := listFiles(t, first)
	b := listFiles(t, second)
	if len(a) != len(b) {
		t.Fatalf("re-capture changed the file set:\n%v\n%v", a, b)
	}
	for name, content := range a {
		if b[name] != content {
			t.Fatalf("re-capture changed %s", name)
		}
	}
}

// signatures.txt is the tripwire: one line per state, naming what it lays out
// to. An engine change that moves a state must move exactly that line.
func TestSignaturesNameEveryLaidOutState(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw")
	writeCapture(t, raw, typing("s", "a.ipmt", "e1 ::e\n", "--> e2 ::e\n--> e3 ::e\n"))
	out := filepath.Join(dir, "corpus")
	if code := curate(raw, out, false); code != 0 {
		t.Fatalf("curate exited %d", code)
	}

	sig, err := os.ReadFile(filepath.Join(out, "s", "signatures.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var rows int
	for _, line := range strings.Split(strings.TrimSpace(string(sig)), "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		rows++
		if n := len(strings.Fields(line)); n != 5 {
			t.Fatalf("signature line has %d fields, want 5: %q", n, line)
		}
	}
	laidOut := 0
	for _, e := range readManifest(t, filepath.Join(out, "s")) {
		if e.Status == "laid-out" {
			laidOut++
		}
	}
	if rows != laidOut || rows == 0 {
		t.Fatalf("%d signature rows for %d laid-out states", rows, laidOut)
	}
}

// Typing a label one character at a time produces states whose layouts are
// identical; keeping them all buries the ones that differ.
func TestLayoutDuplicatesAreDroppedUnlessAllIsAsked(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw")
	// A trailing comment grows character by character: the graph, and so the
	// layout, never changes.
	base := "e1 ::e --> e2 ::e\n"
	writeCapture(t, raw, typing("s", "a.ipmt", base, "// a comment\n"))

	lean := filepath.Join(dir, "lean")
	full := filepath.Join(dir, "full")
	if code := curate(raw, lean, false); code != 0 {
		t.Fatalf("curate exited %d", code)
	}
	if code := curate(raw, full, true); code != 0 {
		t.Fatalf("curate --all exited %d", code)
	}

	leanN := countStatus(readManifest(t, filepath.Join(lean, "s")), "laid-out")
	fullN := countStatus(readManifest(t, filepath.Join(full, "s")), "laid-out")
	if leanN >= fullN {
		t.Fatalf("dedup kept %d of %d states; identical layouts should collapse", leanN, fullN)
	}
	if leanN == 0 {
		t.Fatal("dedup dropped everything")
	}
}

// A half-typed line is a state a user really passes through. It belongs in
// the corpus — in the invalid lane, with the reason — not thrown away.
func TestRejectedStatesAreKeptSeparatelyWithTheirError(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw")
	writeCapture(t, raw, []rawState{
		{Cadence: "open", URI: uriFor("s", "a.ipmt"), Text: "e1 ::e\n"},
		{Cadence: "change", URI: uriFor("s", "a.ipmt"), Text: "e1 ::e\n--> \"unterminated\n"},
	})
	out := filepath.Join(dir, "corpus")
	if code := curate(raw, out, false); code != 0 {
		t.Fatalf("curate exited %d", code)
	}

	var rejected []entry
	for _, e := range readManifest(t, filepath.Join(out, "s")) {
		if e.Status == "rejected" {
			rejected = append(rejected, e)
		}
	}
	if len(rejected) == 0 {
		t.Fatal("the invalid state was dropped instead of kept")
	}
	e := rejected[0]
	if e.Error == "" {
		t.Error("a rejected state carries no reason")
	}
	if !strings.HasPrefix(e.File, "invalid/") {
		t.Errorf("rejected state written to %s, want the invalid lane", e.File)
	}
	if _, err := os.Stat(filepath.Join(out, "s", e.File)); err != nil {
		t.Errorf("rejected state file missing: %v", err)
	}
	// It must NOT appear in signatures.txt — there is no layout to pin.
	sig, _ := os.ReadFile(filepath.Join(out, "s", "signatures.txt"))
	if strings.Contains(string(sig), e.Hash) {
		t.Error("a rejected state leaked into the signature tripwire")
	}
}

// A markdown buffer contributes its ipmt BLOCKS, decided by pkg/mdembed —
// the same code that decides what the editor renders.
func TestMarkdownBuffersContributeTheirBlocks(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw")
	md := "# Title\n\nprose\n\n```ipmt\ne1 ::e --> e2 ::e\n```\n<!-- ipm-svg id=100 hash=abc -->\n![](_ipm/life/100.ipm.svg)\n"
	writeCapture(t, raw, []rawState{{Cadence: "open", URI: uriFor("s", "life.md"), Text: md}})
	out := filepath.Join(dir, "corpus")
	if code := curate(raw, out, false); code != 0 {
		t.Fatalf("curate exited %d", code)
	}
	entries := readManifest(t, filepath.Join(out, "s"))
	if len(entries) != 1 {
		t.Fatalf("got %d states from one markdown buffer: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Block != "100" || e.Source != "life.md#100" {
		t.Fatalf("block identity = %q / %q, want the marker id so it matches _ipm/", e.Source, e.Block)
	}
	body, err := os.ReadFile(filepath.Join(out, "s", e.File))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "# Title") {
		t.Fatalf("the markdown prose was stored instead of the block:\n%s", body)
	}
}

// The cadence tells apart what the typist saw from what the engine actually
// laid out; losing it would make "these states were rendered" unprovable.
func TestRenderedFlagMarksStatesTheEngineLaidOut(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw")
	uri := uriFor("s", "a.ipmt")
	writeCapture(t, raw, []rawState{
		{Cadence: "open", URI: uri, Text: "e1 ::e\n"},
		{Cadence: "change", URI: uri, Text: "e1 ::e\n--> e2 ::e\n"},
		{Cadence: "embed-tokens", URI: uri}, // colouring only: no layout
		{Cadence: "change", URI: uri, Text: "e1 ::e\n--> e2 ::e\n--> e3 ::e\n"},
		{Cadence: "embed", URI: uri}, // the typist paused: the engine ran
	})
	out := filepath.Join(dir, "corpus")
	if code := curate(raw, out, false); code != 0 {
		t.Fatalf("curate exited %d", code)
	}
	entries := readManifest(t, filepath.Join(out, "s"))
	if len(entries) != 3 {
		t.Fatalf("got %d states, want 3: %+v", len(entries), entries)
	}
	if entries[0].Rendered || entries[1].Rendered {
		t.Error("a state with only a tokens-only call after it was marked rendered")
	}
	if !entries[2].Rendered {
		t.Error("the state a full embedBuffer followed was not marked rendered")
	}
}

func countStatus(entries []entry, status string) int {
	n := 0
	for _, e := range entries {
		if e.Status == status {
			n++
		}
	}
	return n
}

func listFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		// manifest.json carries the capture's own ordering, which is stable;
		// nothing in the corpus may carry a timestamp.
		if strings.Contains(string(data), "2026-08-16T09:00") {
			t.Errorf("%s carries a capture timestamp; the corpus must be time-free", rel)
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
