package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const draftDoc = "# Notes\n\nUse ::t for things and ::e for events.\n\nThe default node type is thing.\n\n" +
	"```ipmt\nbook ::t --> read ::e\n```\n\n```ipmt\nplain --> go ::e\n```\n"

func draftRepo(t *testing.T) string {
	t.Helper()
	return gitRepo(t, map[string]string{"doc.md": draftDoc, "plain.md": md("a --> b ::e\n")}, "doc.md", "plain.md")
}

func TestDraftManifest(t *testing.T) {
	dir := draftRepo(t)
	var out, errb bytes.Buffer
	if code := run([]string{"-manifest-draft", "-root", dir}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	d := out.String()
	if _, err := ParseManifest(out.Bytes()); err != nil {
		t.Fatalf("draft does not parse: %v\n%s", err, d)
	}
	e, tt := countMarks(draftDoc)
	wantFile := "file\tdoc.md\te=" + string(rune('0'+e)) + " t=" + string(rune('0'+tt)) + "\t"
	if !strings.Contains(d, wantFile) {
		t.Fatalf("no file entry %q:\n%s", wantFile, d)
	}
	if !strings.Contains(d, "line\tdoc.md\tUse ::t for things and ::e for events.\t") {
		t.Fatalf("no line entry:\n%s", d)
	}
	if strings.Contains(d, "\nline\tdoc.md\tThe default") || !strings.Contains(d, "# doc.md:5: The default node type is thing.") {
		t.Fatalf("marker-free line must be a comment only:\n%s", d)
	}
	if strings.Contains(d, "plain.md") {
		t.Fatalf("file without entries listed:\n%s", d)
	}
	if !strings.Contains(d, "# DRAFT-TOTAL files=1 file-entries=1 line-entries=1 comments=1") {
		t.Fatalf("total line:\n%s", d)
	}
	mf := filepath.Join(dir, "draft.manifest")
	if err := os.WriteFile(mf, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	fails, err := CheckManifestRoot(dir, mf)
	if err != nil || len(fails) != 0 {
		t.Fatalf("check: %v %v", err, fails)
	}
}

func TestDraftDeterministicAndReadOnly(t *testing.T) {
	dir := draftRepo(t)
	var a, b, errb bytes.Buffer
	run([]string{"-manifest-draft", "-root", dir}, &a, &errb)
	run([]string{"-manifest-draft", "-root", dir}, &b, &errb)
	if a.Len() == 0 || a.String() != b.String() {
		t.Fatalf("not deterministic:\n%s\n---\n%s", a.String(), b.String())
	}
	got, _ := os.ReadFile(filepath.Join(dir, "doc.md"))
	if string(got) != draftDoc {
		t.Fatalf("repo file changed")
	}
	if _, err := os.Stat(filepath.Join(dir, "ipmt-markers.manifest")); err == nil {
		t.Fatalf("manifest written")
	}
}

func TestDraftRefusesCombinations(t *testing.T) {
	dir := draftRepo(t)
	for _, flag := range []string{"-write", "-prose", "-flip", "-check-manifest"} {
		var out, errb bytes.Buffer
		if code := run([]string{"-manifest-draft", flag, "-root", dir}, &out, &errb); code != 2 {
			t.Errorf("%s: exit %d, want 2", flag, code)
		}
	}
}

func TestDraftTabInLine(t *testing.T) {
	dir := gitRepo(t, map[string]string{"t.md": "# T\n\nUse ::t\there.\n"}, "t.md")
	var out, errb bytes.Buffer
	if code := run([]string{"-manifest-draft", "-root", dir}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if strings.Contains(out.String(), "\nline\t") {
		t.Fatalf("tab line became an entry:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "# t.md:3:") {
		t.Fatalf("tab line not listed as comment:\n%s", out.String())
	}
	if _, err := ParseManifest(out.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func TestDraftInputErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"-manifest-draft"}, &out, &errb); code != 2 {
		t.Errorf("no input: exit %d", code)
	}
	if code := run([]string{"-manifest-draft", filepath.Join(t.TempDir(), "missing.md")}, &out, &errb); code != 2 {
		t.Errorf("missing file: exit %d", code)
	}
	if code := run([]string{"-manifest-draft", "-root", t.TempDir()}, &out, &errb); code != 2 {
		t.Errorf("not a repo: exit %d", code)
	}
}

func TestDraftNoHits(t *testing.T) {
	dir := gitRepo(t, map[string]string{"a.md": "# A\n\nnothing here\n"}, "a.md")
	var out, errb bytes.Buffer
	if code := run([]string{"-manifest-draft", "-root", dir}, &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "rule\tthing\n") || !strings.Contains(out.String(), "# DRAFT-TOTAL files=0 file-entries=0 line-entries=0 comments=0") {
		t.Fatalf("draft:\n%s", out.String())
	}
	if _, err := ParseManifest(out.Bytes()); err != nil {
		t.Fatal(err)
	}
}
