package mdhtml

import (
	"strings"
	"testing"
)

// TestExtractTitle_skipsFences asserts a `# ` comment inside an opening
// fenced code block is NOT mistaken for the page <title>; the first real
// ATX H1 outside any fence wins.
func TestExtractTitle_skipsFences(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "hash comment inside fence is ignored",
			in:   "```bash\n# This is a shell comment\necho hi\n```\n\n# Real Title\n",
			want: "Real Title",
		},
		{
			name: "plain h1 still found",
			in:   "# Just A Title\n\nbody\n",
			want: "Just A Title",
		},
		{
			name: "no h1 anywhere",
			in:   "```sh\n# only a comment\n```\n\nno heading here\n",
			want: "",
		},
		{
			name: "real h1 before a fenced comment",
			in:   "# First\n\n```py\n# second\n```\n",
			want: "First",
		},
		{
			// Length-aware tracking: the inner ``` lines do not close the
			// ````md wrapper, so the # inside the example stays ignored.
			// The old any-fence-line toggle desynced here and returned it.
			name: "h1 inside a nested fence example is ignored",
			in:   "````md\n```ipmt\nA --> B\n```\n# Not The Title\n````\n\n# Real Title\n",
			want: "Real Title",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractTitle(c.in); got != c.want {
				t.Errorf("extractTitle() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestHasScheme covers the scheme-detection edge cases that drive
// rewriteLinksAndImages: real schemes are left alone, but relative paths
// whose first segment contains a colon (the CommonMark/RFC-3986 ambiguity)
// are NOT treated as schemes so a `.md` target still gets rewritten.
func TestHasScheme(t *testing.T) {
	cases := []struct {
		u    string
		want bool
	}{
		{"http://example.com/x.md", true},
		{"https://example.com/x", true},
		{"ftp://host/file", true},
		{"mailto:me@example.com", true},
		{"data:image/png;base64,AAAA", true},
		{"tel:+15551234", true},
		{"./foo.md", false},
		{"../a/b.md", false},
		{"foo.md#sec", false},
		{"a:b.md", false},       // colon-in-first-segment relative .md path
		{"C:/x/y.md", false},    // windows-style relative .md path
		{"a:b.markdown", false}, // .markdown extension also rewritten
		{"//host/x.md", false},  // protocol-relative: no scheme
		{"#frag", false},
	}
	for _, c := range cases {
		if got := hasScheme(c.u); got != c.want {
			t.Errorf("hasScheme(%q) = %v, want %v", c.u, got, c.want)
		}
	}
}

// TestRewriteLinksAndImages_skips asserts that scheme/data/protocol-relative
// links are left untouched, fragments+queries on relative .md links are
// preserved, and a colon-in-path relative .md link is still rewritten.
func TestRewriteLinksAndImages_skips(t *testing.T) {
	// srcAbs/outAbs only matter for image copying; use temp-irrelevant paths.
	in := strings.Join([]string{
		`<a href="https://example.com/page.md">ext</a>`,
		`<a href="mailto:x@y.com">mail</a>`,
		`<a href="./rel.md#sec">rel</a>`,
		`<a href="../q.md?x=1">q</a>`,
		`<a href="a:b.md">colon</a>`,
		`<img alt="" src="data:image/png;base64,AAAA">`,
		`<img alt="" src="//cdn/x.png">`,
	}, "\n")
	out := rewriteLinksAndImages(in, "/src/in.md", "/out/in.html")

	mustContain := []string{
		`<a href="https://example.com/page.md">ext</a>`, // external .md untouched
		`<a href="mailto:x@y.com">mail</a>`,             // mailto untouched
		`<a href="./rel.html#sec">rel</a>`,              // rewritten, fragment kept
		`<a href="../q.html?x=1">q</a>`,                 // rewritten, query kept
		`<a href="a:b.html">colon</a>`,                  // colon-path rewritten
		`<img alt="" src="data:image/png;base64,AAAA">`, // data URI untouched
		`<img alt="" src="//cdn/x.png">`,                // protocol-relative untouched
	}
	for _, c := range mustContain {
		if !strings.Contains(out, c) {
			t.Errorf("output missing %q\nfull output:\n%s", c, out)
		}
	}
}

// TestAddHeadingControls_multilineInner guards the (?s) flag on headingRE:
// a heading whose inner HTML spans a newline must still receive the control
// cluster (permalink + back-to-top).
func TestAddHeadingControls_multilineInner(t *testing.T) {
	in := "<h2 id=\"sec\">line one\nline two</h2>"
	out := addHeadingControls(in)
	if !strings.Contains(out, `class="heading-ctls"`) {
		t.Errorf("multi-line heading not decorated; got:\n%s", out)
	}
	if !strings.Contains(out, `href="#sec"`) {
		t.Errorf("permalink missing; got:\n%s", out)
	}
}
