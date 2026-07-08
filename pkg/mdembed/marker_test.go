package mdembed

import (
	"reflect"
	"testing"
)

// markersEqual deep-compares two Markers, including the Unknown slice (which
// rules out plain == on the struct).
func markersEqual(a, b Marker) bool {
	return reflect.DeepEqual(a, b)
}

func TestParseMarker_minimal(t *testing.T) {
	m, ok := ParseMarker(
		`<!-- ipm-svg id=01 hash=ab12cd34 -->`,
		`![](./_ipm/foo/01.ipm.svg)`,
	)
	if !ok {
		t.Fatal("want ok")
	}
	want := Marker{
		ID:        "01",
		Hash:      "ab12cd34",
		ImagePath: "./_ipm/foo/01.ipm.svg",
	}
	if !markersEqual(m, want) {
		t.Fatalf("got %+v, want %+v", m, want)
	}
}

func TestParseMarker_allAttrs(t *testing.T) {
	m, ok := ParseMarker(
		`<!-- ipm-svg id=02 hash=deadbeef path=./diagrams/x.svg pos=before -->`,
		`![diagram](./diagrams/x.svg)`,
	)
	if !ok {
		t.Fatal("want ok")
	}
	want := Marker{
		ID:        "02",
		Hash:      "deadbeef",
		Path:      "./diagrams/x.svg",
		Pos:       "before",
		ImageAlt:  "diagram",
		ImagePath: "./diagrams/x.svg",
	}
	if !markersEqual(m, want) {
		t.Fatalf("got %+v, want %+v", m, want)
	}
}

func TestParseMarker_notAMarker(t *testing.T) {
	cases := [][2]string{
		{"", ""},
		{"plain text", "![](x.svg)"},
		{"<!-- some other comment -->", "![](x.svg)"},
		{"<!-- ipm-svg id=01 -->", "![](x.svg)"},         // missing hash
		{"<!-- ipm-svg hash=ab12cd34 -->", "![](x.svg)"}, // missing id
		{"<!-- ipm-svg id=01 hash=ab -->", "not an image"},
		{"<!-- ipm-svg id=01 hash=ab -->", ""},
	}
	for _, c := range cases {
		if _, ok := ParseMarker(c[0], c[1]); ok {
			t.Errorf("ParseMarker(%q, %q) unexpectedly matched", c[0], c[1])
		}
	}
}

func TestParseMarker_preservesUnknownAttrs(t *testing.T) {
	// Unknown attributes (e.g. attributes added by a future toolchain version)
	// must be parsed into the Unknown slice so they survive a round-trip
	// through FormatLines. Prereq F.
	m, ok := ParseMarker(
		`<!-- ipm-svg id=01 hash=ab12cd34 foo=bar style=compact -->`,
		`![](./x.svg)`,
	)
	if !ok || m.ID != "01" || m.Hash != "ab12cd34" {
		t.Fatalf("got %+v ok=%v", m, ok)
	}
	want := []UnknownAttr{
		{Key: "foo", Value: "bar"},
		{Key: "style", Value: "compact"},
	}
	if !reflect.DeepEqual(m.Unknown, want) {
		t.Fatalf("Unknown = %+v, want %+v", m.Unknown, want)
	}
}

func TestMarker_UnknownAttrsRoundTrip(t *testing.T) {
	// FormatLines must emit unknown attributes in their original order so the
	// re-parsed marker is byte-equivalent to the source.
	src := `<!-- ipm-svg id=01 hash=ab12cd34 foo=bar baz=qux -->`
	m, ok := ParseMarker(src, `![](./x.svg)`)
	if !ok {
		t.Fatal("want ok")
	}
	ls := m.FormatLines()
	if ls[0] != src {
		t.Fatalf("round-trip mismatch\n  got  %q\n  want %q", ls[0], src)
	}
	m2, ok := ParseMarker(ls[0], ls[1])
	if !ok {
		t.Fatal("re-parse failed")
	}
	if !markersEqual(m, m2) {
		t.Fatalf("round-tripped Marker differs:\n  before: %+v\n  after:  %+v", m, m2)
	}
}

func TestMarker_UnknownAttrsFollowKnownAttrs(t *testing.T) {
	// Known attributes come first in their fixed order; unknown attributes
	// follow in encounter order. This keeps the canonical layout predictable.
	m := Marker{
		ID:        "01",
		Hash:      "ab",
		Path:      "p.svg",
		ImagePath: "p.svg",
		Unknown: []UnknownAttr{
			{Key: "experimental-z", Value: "1"},
			{Key: "experimental-a", Value: "2"},
		},
	}
	ls := m.FormatLines()
	want := `<!-- ipm-svg id=01 hash=ab path=p.svg experimental-z=1 experimental-a=2 -->`
	if ls[0] != want {
		t.Fatalf("attr order wrong:\n  got  %q\n  want %q", ls[0], want)
	}
}

func TestMarkerFormat_roundTrip(t *testing.T) {
	cases := []Marker{
		{ID: "01", Hash: "ab12cd34", ImagePath: "./_ipm/foo/01.ipm.svg"},
		{ID: "02", Hash: "deadbeef", Pos: "before", ImagePath: "./_ipm/foo/02.ipm.svg"},
		{ID: "03", Hash: "00112233", Path: "./d/x.svg", ImagePath: "./d/x.svg"},
		{ID: "04", Hash: "44556677", ImageAlt: "diagram", ImagePath: "./_ipm/foo/04.ipm.svg"},
	}
	for _, want := range cases {
		ls := want.FormatLines()
		got, ok := ParseMarker(ls[0], ls[1])
		if !ok {
			t.Fatalf("FormatLines()=%v did not parse", ls)
		}
		if !markersEqual(got, want) {
			t.Fatalf("round-trip:\n  in:  %+v\n  lines: %v\n  out: %+v", want, ls, got)
		}
	}
}

func TestMarkerFormatLines_attrOrder(t *testing.T) {
	m := Marker{ID: "01", Hash: "ab", Path: "p", Pos: "before", ImagePath: "p"}
	ls := m.FormatLines()
	wantComment := `<!-- ipm-svg id=01 hash=ab path=p pos=before -->`
	wantImage := `![](p)`
	if ls[0] != wantComment {
		t.Fatalf("comment line:\n  got  %q\n  want %q", ls[0], wantComment)
	}
	if ls[1] != wantImage {
		t.Fatalf("image line:\n  got  %q\n  want %q", ls[1], wantImage)
	}
}

func TestParseMarker_posBefore(t *testing.T) {
	m, ok := ParseMarker(
		`<!-- ipm-svg id=01 hash=ab12cd34 pos=before -->`,
		`![](./x.svg)`,
	)
	if !ok {
		t.Fatal("want ok")
	}
	if m.Pos != "before" {
		t.Fatalf("Pos = %q, want %q", m.Pos, "before")
	}
}

func TestParseMarker_posAfterCanonicalizesToEmpty(t *testing.T) {
	// `pos=after` is the default; the parser canonicalizes it to "" so the
	// formatter omits the attribute on re-emission.
	m, ok := ParseMarker(
		`<!-- ipm-svg id=01 hash=ab12cd34 pos=after -->`,
		`![](./x.svg)`,
	)
	if !ok {
		t.Fatal("want ok")
	}
	if m.Pos != "" {
		t.Fatalf("Pos = %q, want empty (canonicalized)", m.Pos)
	}
}

func TestMarkerFormatLines_posOmittedByDefault(t *testing.T) {
	m := Marker{ID: "01", Hash: "ab12cd34", ImagePath: "x.svg"}
	ls := m.FormatLines()
	want := `<!-- ipm-svg id=01 hash=ab12cd34 -->`
	if ls[0] != want {
		t.Fatalf("default Pos should not appear; got %q, want %q", ls[0], want)
	}
}

func TestMarkerFormat_joinsWithNewline(t *testing.T) {
	m := Marker{ID: "01", Hash: "ab", ImagePath: "x.svg"}
	got := m.Format()
	want := "<!-- ipm-svg id=01 hash=ab -->\n![](x.svg)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestIsMarkerCommentLine(t *testing.T) {
	yes := []string{
		`<!-- ipm-svg id=01 hash=ab12cd34 -->`,
		`<!-- ipm-svg id=01 hash=ab source=hidden -->`,
		`   <!-- ipm-svg id=01 hash=ab -->   `,
	}
	no := []string{
		``,
		`<!-- ipm-svg-src id=01`,
		`![](./x.svg)`,
		`<!-- ipm-svg id=01 hash=ab --> ![](./x.svg)`, // legacy one-line form: rejected
		`<!-- not-our-comment -->`,
	}
	for _, ln := range yes {
		if !IsMarkerCommentLine(ln) {
			t.Errorf("IsMarkerCommentLine(%q) = false, want true", ln)
		}
	}
	for _, ln := range no {
		if IsMarkerCommentLine(ln) {
			t.Errorf("IsMarkerCommentLine(%q) = true, want false", ln)
		}
	}
}
