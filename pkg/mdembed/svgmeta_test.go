package mdembed

import (
	"bytes"
	"strings"
	"testing"
)

const minimalSVG = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
  <rect width="10" height="10"/>
</svg>`

func TestEmbedMeta_insertsAfterRoot(t *testing.T) {
	meta := Meta{
		Hash:        "ab12cd34",
		SourceID:    "01",
		SourceFile:  "docs/x.md",
		GeneratedBy: "md-embed@dev",
	}
	out := EmbedMeta([]byte(minimalSVG), meta)
	if !bytes.Contains(out, []byte("ipm-svg meta:")) {
		t.Fatalf("output does not contain meta comment:\n%s", out)
	}
	// Comment must appear after the opening <svg ...> tag, before the next element.
	idxRoot := bytes.Index(out, []byte("<svg"))
	idxRootEnd := bytes.Index(out[idxRoot:], []byte(">")) + idxRoot
	idxMeta := bytes.Index(out, []byte("ipm-svg meta:"))
	idxRect := bytes.Index(out, []byte("<rect"))
	if !(idxRootEnd < idxMeta && idxMeta < idxRect) {
		t.Fatalf("meta comment in wrong place; rootEnd=%d meta=%d rect=%d", idxRootEnd, idxMeta, idxRect)
	}
}

func TestEmbedMeta_replacesPriorMeta(t *testing.T) {
	out := EmbedMeta([]byte(minimalSVG), Meta{Hash: "aaaaaaaa", SourceID: "01"})
	out = EmbedMeta(out, Meta{Hash: "bbbbbbbb", SourceID: "01"})
	if c := bytes.Count(out, []byte("ipm-svg meta:")); c != 1 {
		t.Fatalf("want exactly 1 meta comment after re-embed, got %d:\n%s", c, out)
	}
	if !bytes.Contains(out, []byte("hash=bbbbbbbb")) {
		t.Fatalf("re-embed didn't update hash:\n%s", out)
	}
	if bytes.Contains(out, []byte("hash=aaaaaaaa")) {
		t.Fatalf("re-embed left old hash behind:\n%s", out)
	}
}

func TestExtractMeta_roundTrip(t *testing.T) {
	want := Meta{
		Hash:        "ab12cd34",
		SourceID:    "01",
		SourceFile:  "docs/x.md",
		GeneratedBy: "md-embed@dev",
	}
	svg := EmbedMeta([]byte(minimalSVG), want)
	got, ok := ExtractMeta(svg)
	if !ok {
		t.Fatal("ExtractMeta returned ok=false")
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestExtractMeta_missing(t *testing.T) {
	if _, ok := ExtractMeta([]byte(minimalSVG)); ok {
		t.Fatal("ExtractMeta on un-annotated SVG returned ok=true")
	}
}

func TestEmbedMeta_unrecognizedInput(t *testing.T) {
	in := []byte("not an svg at all")
	out := EmbedMeta(in, Meta{Hash: "abcdef01", SourceID: "01"})
	if !bytes.Equal(in, out) {
		t.Fatalf("non-SVG input should pass through unchanged; got %q", out)
	}
}

func TestEmbedMeta_omitsEmptyFields(t *testing.T) {
	out := EmbedMeta([]byte(minimalSVG), Meta{Hash: "ab12cd34", SourceID: "01"})
	s := string(out)
	if !strings.Contains(s, "hash=ab12cd34") || !strings.Contains(s, "source-id=01") {
		t.Fatalf("missing required meta keys in %q", s)
	}
	if strings.Contains(s, "source-file=") || strings.Contains(s, "generated-by=") {
		t.Fatalf("empty fields should be omitted; got %q", s)
	}
}

func TestEqualExceptGeneratedBy(t *testing.T) {
	base := Meta{Hash: "ab12cd34", SourceID: "1a0", SourceFile: "dev/x.md", GeneratedBy: "ipm-rpc@v0.0.0-20250805100000-feb8cd534086"}
	a := EmbedMeta([]byte(minimalSVG), base)

	// Same diagram, re-rendered by a different tool/version: equal.
	b := base
	b.GeneratedBy = "md-embed@dev"
	if !EqualExceptGeneratedBy(a, EmbedMeta([]byte(minimalSVG), b)) {
		t.Fatal("generated-by-only difference should compare equal")
	}

	// A real content change must NOT compare equal.
	diffBody := EmbedMeta([]byte(strings.Replace(minimalSVG, "10", "20", 1)), b)
	if EqualExceptGeneratedBy(a, diffBody) {
		t.Fatal("a changed SVG body must not compare equal")
	}

	// A different source hash (genuine source edit) must NOT compare equal.
	h := base
	h.Hash = "ffffffff"
	if EqualExceptGeneratedBy(a, EmbedMeta([]byte(minimalSVG), h)) {
		t.Fatal("a changed hash must not compare equal")
	}

	// Byte-identical inputs are trivially equal.
	if !EqualExceptGeneratedBy(a, a) {
		t.Fatal("identical bytes should compare equal")
	}
}
