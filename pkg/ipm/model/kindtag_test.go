package model

import "testing"

func TestKindTag(t *testing.T) {
	for _, c := range []struct{ in, tag, bare string }{
		{"controller (c)", "c", "controller"},
		{"Job controller (t)", "t", "Job controller"},
		{"reconcile (e)", "e", "reconcile"},
		{"controller", "", "controller"},
		{"f(x)", "", "f(x)"},       // not a tag: no space, not a kind letter
		{"a (x)", "", "a (x)"},     // not a kind letter
		{"(c)", "", "(c)"},         // nothing to tag
		{"note (c) ", "c", "note"}, // trailing space is fine
		{"two words (c)", "c", "two words"},
	} {
		tag, bare := KindTag(c.in)
		if tag != c.tag || bare != c.bare {
			t.Errorf("KindTag(%q) = %q, %q; want %q, %q", c.in, tag, bare, c.tag, c.bare)
		}
	}
	if DisplayName("controller (c)") != "controller" {
		t.Fatal("DisplayName drops the tag")
	}
}
