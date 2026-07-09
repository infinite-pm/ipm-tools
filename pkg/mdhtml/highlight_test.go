package mdhtml

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipmtokens"
)

func TestHighlightTokens_basicNodesAndArrow(t *testing.T) {
	// Event → event = leadsTo (SST relation that the arrow inherits).
	src := "Alice ::e --> bob ::e\n"
	toks := ipmtokens.Collect(src, src, 0)
	out := HighlightTokens(src, toks)

	want := []string{
		`<span class="ipm-e-title">Alice</span>`,
		`<span class="ipm-e-marker">::e</span>`,
		`<span class="ipm-L">--&gt;</span>`,
		`<span class="ipm-e-title">bob</span>`,
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q\nout=%s", w, out)
		}
	}
}

func TestHighlightTokens_multibyteUTF16Offsets(t *testing.T) {
	// Token positions are UTF-16 code units, not bytes. Content with
	// multi-byte runes (subscript ₑ = 3 bytes / 1 unit; astral 𞁞 = 4
	// bytes / 2 units) must not be cut mid-character. Regression for the
	// as-token `Nₑ` / `N𞁞` case.
	cases := []struct{ name, visible string }{
		{"bmp_subscript", "Nₑ"},
		{"bmp_subscript_t", "Nₜ"},
		{"astral", "N\U0001E05E"},
		{"mixed", "aₑb𞁞c"},
	}
	for _, c := range cases {
		toks, ok := ipmtokens.TokensForClass("N", c.visible)
		if !ok {
			t.Fatalf("%s: TokensForClass returned ok=false", c.name)
		}
		out := HighlightTokens(c.visible, toks)
		want := `<span class="ipm-N">` + c.visible + `</span>`
		if out != want {
			t.Errorf("%s: got %q, want %q", c.name, out, want)
		}
		if strings.ContainsRune(out, '�') {
			t.Errorf("%s: output contains replacement char (split rune): %q", c.name, out)
		}
	}
}

func TestHighlightTokens_emptyInput(t *testing.T) {
	if got := HighlightTokens("", nil); got != "" {
		t.Errorf("empty input should yield empty output; got %q", got)
	}
}

func TestHighlightTokens_unknownTypeIsBareText(t *testing.T) {
	// A token whose type index doesn't appear in the palette table
	// should fall through to plain escaped text (no span wrapper).
	src := "abc"
	toks := []ipmtokens.Token{{Line: 0, StartCol: 0, Length: 3, Type: 999}}
	out := HighlightTokens(src, toks)
	if strings.Contains(out, "<span") {
		t.Errorf("unknown token type leaked a <span>: %q", out)
	}
	if !strings.Contains(out, "abc") {
		t.Errorf("output missing source text: %q", out)
	}
}
