package markdown

import (
	"strings"
	"testing"
)

func TestFindInlineIpmt_canonical(t *testing.T) {
	text := "A small color taxonomy (<!--ipmt-->`t-shirt ::c`, <!--ipmt-->`black ::c`).\n"
	got := FindInlineIpmt(text)
	if len(got) != 2 {
		t.Fatalf("want 2 matches, got %d: %+v", len(got), got)
	}
	if got[0].Code != "t-shirt ::c" {
		t.Errorf("first match code = %q, want %q", got[0].Code, "t-shirt ::c")
	}
	if got[1].Code != "black ::c" {
		t.Errorf("second match code = %q, want %q", got[1].Code, "black ::c")
	}
}

func TestFindInlineIpmt_caseAndWhitespace(t *testing.T) {
	cases := []string{
		"<!--ipmt-->`a ::c`",
		"<!-- ipmt -->`a ::c`",
		"<!--IPMT-->`a ::c`",
		"<!--   Ipmt   -->`a ::c`",
		"<!--ipmt--> `a ::c`",  // space between marker and backtick
		"<!--ipmt-->\n`a ::c`", // newline OK
	}
	for _, c := range cases {
		got := FindInlineIpmt(c)
		if len(got) != 1 {
			t.Errorf("%q → want 1 match, got %d: %+v", c, len(got), got)
			continue
		}
		if got[0].Code != "a ::c" {
			t.Errorf("%q → code = %q, want %q", c, got[0].Code, "a ::c")
		}
	}
}

func TestFindInlineIpmt_noMatchWithoutMarker(t *testing.T) {
	text := "A plain inline code `t-shirt ::c` with no marker.\n"
	if got := FindInlineIpmt(text); len(got) != 0 {
		t.Errorf("want 0 matches without marker, got %d: %+v", len(got), got)
	}
}

func TestFindInlineIpmt_noMatchWhenProseBetweenMarkerAndBacktick(t *testing.T) {
	text := "Some text <!--ipmt--> oops `t-shirt ::c` end.\n"
	if got := FindInlineIpmt(text); len(got) != 0 {
		t.Errorf("want 0 matches when prose intervenes, got %d: %+v", len(got), got)
	}
}

func TestFindInlineIpmt_doubleBacktickCodeSpan(t *testing.T) {
	// Standard CommonMark: ``code with `backtick` inside``.
	text := "<!--ipmt-->``a `b` c``\n"
	got := FindInlineIpmt(text)
	if len(got) != 1 {
		t.Fatalf("want 1 match, got %d: %+v", len(got), got)
	}
	if got[0].Code != "a `b` c" {
		t.Errorf("code = %q, want %q", got[0].Code, "a `b` c")
	}
}

func TestFindInlineIpmt_asToken(t *testing.T) {
	cases := []struct {
		text     string
		wantName string
		wantCode string
	}{
		{"the <!--ipmt:as-token:e-marker-->`::e` marker", "e-marker", "::e"},
		{"a <!--ipmt:as-token:L-->`-->` arrow", "L", "-->"}, // uppercase NAME, code with arrow
		{"<!--ipmt:as-token:type-marker-->`::a`", "type-marker", "::a"},
		{"<!--IPMT:as-token:c-title-->`x`", "c-title", "x"}, // case-insensitive keyword
	}
	for _, c := range cases {
		got := FindInlineIpmt(c.text)
		if len(got) != 1 {
			t.Errorf("%q → want 1 match, got %d", c.text, len(got))
			continue
		}
		if got[0].AsToken != c.wantName {
			t.Errorf("%q → AsToken=%q, want %q", c.text, got[0].AsToken, c.wantName)
		}
		if got[0].Code != c.wantCode {
			t.Errorf("%q → Code=%q, want %q", c.text, got[0].Code, c.wantCode)
		}
	}
}

func TestFindInlineIpmt_bareHasNoAsToken(t *testing.T) {
	got := FindInlineIpmt("see <!--ipmt-->`t-shirt ::c` ok\n")
	if len(got) != 1 {
		t.Fatalf("want 1 match, got %d", len(got))
	}
	if got[0].AsToken != "" {
		t.Errorf("bare marker AsToken = %q, want empty", got[0].AsToken)
	}
	if got[0].Code != "t-shirt ::c" {
		t.Errorf("Code = %q, want %q", got[0].Code, "t-shirt ::c")
	}
}

func TestLineStartInlineIpmt(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []int
	}{
		{"lineStart_arrow", "<!--ipmt:as-token:L-->`-->` flow\n", []int{1}},
		{"lineStart_bare", "<!--ipmt-->`a ::e`\n", []int{1}},
		{"lineStart_indent3", "   <!--ipmt-->`a ::e`\n", []int{1}},
		{"indent4_isCodeBlock", "    <!--ipmt-->`a ::e`\n", nil},
		{"prose_before_ok", "see <!--ipmt:as-token:L-->`-->` ok\n", nil},
		{"table_cell_ok", "| <!--ipmt:as-token:e-title-->`Event` | x |\n", nil},
		{"inside_fence_ok", "```\n<!--ipmt-->`a ::e`\n```\n", nil},
		{"inside_ipmt_fence_ok", "```ipmt\n<!--ipmt-->`a ::e`\n```\n", nil},
		{"multiple_lines", "<!--ipmt-->`a ::e`\nok\n<!--ipmt-->`b ::t`\n", []int{1, 3}},
		{"not_ipmt_comment", "<!-- ipm-svg id=01 -->\n", nil},
	}
	for _, c := range cases {
		got := LineStartInlineIpmt(c.text)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestFindInlineIpmt_oldSyntaxIgnored(t *testing.T) {
	// No backward compatibility: the old cut=/as=/from=/pick= attribute
	// forms are NOT recognized as ipmt markers.
	for _, text := range []string{
		"x <!--ipmt cut=\"cA ::c\"-->`::c`",
		"x <!--ipmt as=\"::e\"-->`(orange)`",
		"x <!--ipmt from=\"e1 ::e\" pick=\"e1\"-->`(orange)`",
	} {
		if got := FindInlineIpmt(text); len(got) != 0 {
			t.Errorf("old syntax %q should match 0, got %d: %+v", text, len(got), got)
		}
	}
}

func TestFindInlineIpmt_offsetsAreAccurate(t *testing.T) {
	text := "x <!--ipmt-->`abc` y"
	got := FindInlineIpmt(text)
	if len(got) != 1 {
		t.Fatalf("want 1 match, got %d", len(got))
	}
	m := got[0]
	if text[m.MarkerStart:m.MarkerEnd] != "<!--ipmt-->" {
		t.Errorf("marker range wrong: %q", text[m.MarkerStart:m.MarkerEnd])
	}
	if text[m.CodeStart:m.CodeEnd] != "`abc`" {
		t.Errorf("code range wrong: %q", text[m.CodeStart:m.CodeEnd])
	}
	if text[m.ContentStart:m.ContentEnd] != "abc" {
		t.Errorf("content range wrong: %q", text[m.ContentStart:m.ContentEnd])
	}
}

// Markers inside fenced code blocks are literal text (a doc SHOWING the
// marker syntax), not inline ipmt.
func TestFindInlineIpmt_skipsMarkersInsideFences(t *testing.T) {
	text := "```md\n<!--ipmt--> `A ::e`\n```\n\nprose <!--ipmt--> `B ::e` end\n"
	got := FindInlineIpmt(text)
	if len(got) != 1 {
		t.Fatalf("want 1 match (the one outside the fence), got %d: %+v", len(got), got)
	}
	if got[0].Code != "B ::e" {
		t.Errorf("Code = %q, want %q", got[0].Code, "B ::e")
	}
}

// A marker sitting right above a fence must not swallow the fence's
// delimiter as its "inline code" (backtick runs across a block boundary
// are not inline code in CommonMark).
func TestFindInlineIpmt_markerBeforeFenceDoesNotMatch(t *testing.T) {
	text := "<!--ipmt-->\n```go\ncode\n```\n"
	if got := FindInlineIpmt(text); len(got) != 0 {
		t.Fatalf("marker before a fence must not match; got %+v", got)
	}
}

// LineStartInlineIpmt must track fence LENGTHS: inside a ````md example
// the inner ```ipmt lines do not close the fence (the old any-fence-line
// toggle desynced here), so a line-leading marker in the example stays
// unflagged while a real one after the example is still caught.
func TestLineStartInlineIpmt_nestedExampleFence(t *testing.T) {
	text := strings.Join([]string{
		"````md",          // 1
		"```ipmt",         // 2 (does NOT close ````)
		"A --> B",         // 3
		"```",             // 4 (does NOT close ```` either)
		"<!--ipmt--> `x`", // 5 still inside the ````md example: literal
		"````",            // 6 closes
		"<!--ipmt--> `y`", // 7 real, line-leading: flagged
	}, "\n")
	got := LineStartInlineIpmt(text)
	if len(got) != 1 || got[0] != 7 {
		t.Fatalf("want [7], got %v", got)
	}
}
