package markdown

import "testing"

// TestGenerate pins the anchor-slug behavior, including the slash-collapse
// step (guards the hoisted multipleSlashes regexp) and the documented
// ASCII-only divergence from GitHub.
func TestGenerate(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Hello World", "hello-world"},
		{"a/b//c", "a/b/c"},      // consecutive slashes collapse
		{"a///b", "a/b"},         // 3+ slashes collapse
		{"Foo  Bar", "foo-bar"},  // multiple spaces collapse to one hyphen
		{"-trim-", "trim"},       // leading/trailing hyphens trimmed
		{"file.md", "file.md"},   // dots retained
		{"Über Café", "ber-caf"}, // ASCII-only: non-ASCII letters dropped
		{"日本語", ""},              // all non-ASCII → empty
	}
	for _, c := range cases {
		if got := Generate(c.in); got != c.want {
			t.Errorf("Generate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
