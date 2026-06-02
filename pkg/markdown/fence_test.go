package markdown

import (
	"strings"
	"testing"
)

func TestParseFenceOpen(t *testing.T) {
	cases := []struct {
		line     string
		wantOK   bool
		wantChar byte
		wantLen  int
		wantInfo string
	}{
		{"```", true, '`', 3, ""},
		{"```ipmt", true, '`', 3, "ipmt"},
		{"```ipmt unresolved", true, '`', 3, "ipmt unresolved"},
		{"````md", true, '`', 4, "md"},
		{"~~~", true, '~', 3, ""},
		{"~~~~text", true, '~', 4, "text"},
		{"  ```go", true, '`', 3, "go"},   // list-indented fences stay fences (lenient indent)
		{"\t```go", true, '`', 3, "go"},   // tab indent too
		{"``", false, 0, 0, ""},           // too short
		{"~~x~~ strike", false, 0, 0, ""}, // too short + prose
		{"prose ``` prose", false, 0, 0, ""},
		{"", false, 0, 0, ""},
		// CommonMark: a backtick fence's info string cannot contain a
		// backtick — the line is inline code, not a fence. This is what
		// keeps prose like "A markdown ```` ```ipmt ```` fenced block…"
		// (docs/ipmt-unresolved.md line 3, were it line-leading) from
		// swallowing the rest of the document.
		{"```` ```ipmt ```` fenced", false, 0, 0, ""},
		{"```ipmt`x", false, 0, 0, ""},
		// The single-backtick inline form at line start: a 1-backtick run
		// is too short to be a fence, so prose like "` ```ipmt ` code
		// blocks…" never opens anything either.
		{"` ```ipmt ` code blocks", false, 0, 0, ""},
		// …but a tilde fence's info string may contain backticks.
		{"~~~ has ` tick", true, '~', 3, "has ` tick"},
	}
	for _, c := range cases {
		f, info, ok := ParseFenceOpen(c.line)
		if ok != c.wantOK {
			t.Errorf("ParseFenceOpen(%q) ok = %v, want %v", c.line, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if f.Char != c.wantChar || f.Len != c.wantLen || info != c.wantInfo {
			t.Errorf("ParseFenceOpen(%q) = {%q %d} info=%q, want {%q %d} info=%q",
				c.line, f.Char, f.Len, info, c.wantChar, c.wantLen, c.wantInfo)
		}
	}
}

func TestFenceClosedBy(t *testing.T) {
	cases := []struct {
		f    Fence
		line string
		want bool
	}{
		{Fence{'`', 3}, "```", true},
		{Fence{'`', 3}, "````", true},     // longer run closes
		{Fence{'`', 4}, "```", false},     // shorter run does not close ````
		{Fence{'`', 3}, "```text", false}, // a closer carries no info string
		{Fence{'`', 3}, "  ```  ", true},  // surrounding whitespace ok
		{Fence{'`', 3}, "~~~", false},     // wrong character
		{Fence{'~', 3}, "~~~", true},
		{Fence{'~', 3}, "```", false},
		{Fence{'~', 4}, "~~~", false},  // shorter tilde run does not close ~~~~
		{Fence{'~', 4}, "~~~~", true},  // exact tilde length closes
		{Fence{'~', 4}, "~~~~~", true}, // longer tilde run closes
		{Fence{'`', 3}, "prose", false},
	}
	for _, c := range cases {
		if got := c.f.ClosedBy(c.line); got != c.want {
			t.Errorf("Fence{%q,%d}.ClosedBy(%q) = %v, want %v", c.f.Char, c.f.Len, c.line, got, c.want)
		}
	}
}

func TestSkipFence(t *testing.T) {
	lines := []string{"````md", "```ipmt", "A --> B", "```", "````", "after"}
	f, _, ok := ParseFenceOpen(lines[0])
	if !ok {
		t.Fatal("opener not recognized")
	}
	// The inner ``` lines are shorter than the ```` opener, so the skip
	// lands past the ```` closer at index 4.
	if got := SkipFence(lines, 0, f); got != 5 {
		t.Fatalf("SkipFence = %d, want 5 (the line after ````)", got)
	}
	// Unterminated: skip to len(lines).
	if got := SkipFence(lines[:3], 0, f); got != 3 {
		t.Fatalf("SkipFence unterminated = %d, want 3", got)
	}

	// The tilde-wrapper form: backtick fences inside ~~~~md are content
	// (wrong character, can never close it); the skip lands past ~~~~.
	tl := []string{"~~~~md", "```ipmt", "A --> B", "```", "~~~~", "after"}
	tf, _, ok := ParseFenceOpen(tl[0])
	if !ok {
		t.Fatal("tilde opener not recognized")
	}
	if got := SkipFence(tl, 0, tf); got != 5 {
		t.Fatalf("SkipFence tilde = %d, want 5 (the line after ~~~~)", got)
	}
}

func TestIsIPMTInfo(t *testing.T) {
	for info, want := range map[string]bool{
		"ipmt":            true,
		"ipmt unresolved": true,
		"ipmt\tdefaults":  true,
		"":                false,
		"ipmtX":           false,
		"ipmt-x":          false,
		"ipmt-invalid":    false, // the negative-example lane is a foreign fence, skipped for free
		"md":              false,
	} {
		if got := IsIPMTInfo(info); got != want {
			t.Errorf("IsIPMTInfo(%q) = %v, want %v", info, got, want)
		}
	}
}

func TestFencedByteRanges(t *testing.T) {
	text := strings.Join([]string{
		"prose",             // 0-5
		"```go",             // 6-11
		"code",              // 12-16
		"```",               // 17-20
		"more prose",        // 21-31
		"~~~",               // 32-35
		"unterminated tail", // to EOF
	}, "\n")
	got := FencedByteRanges(text)
	if len(got) != 2 {
		t.Fatalf("ranges = %v, want 2", got)
	}
	if want := [2]int{6, 20}; got[0] != want {
		t.Errorf("range 0 = %v, want %v (%q)", got[0], want, text[got[0][0]:got[0][1]])
	}
	if want := [2]int{32, len(text)}; got[1] != want {
		t.Errorf("range 1 = %v, want %v", got[1], want)
	}
	if FencedByteRanges("no fences at all") != nil {
		t.Error("fence-free text should yield nil ranges")
	}
}
