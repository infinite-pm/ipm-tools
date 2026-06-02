package markdown

import (
	"slices"
	"testing"
)

func TestFenceMeta(t *testing.T) {
	cases := map[string][]string{
		"```ipmt":               nil,
		"```ipmt ":              nil,
		"```ipmt unresolved":    {"unresolved"},
		"   ```ipmt unresolved": {"unresolved"},
		"````ipmt a b":          {"a", "b"},
		// "ipmt" must be the whole language token — a non-space suffix means a
		// different language, not ipmt metadata.
		"```ipmtX":   nil,
		"```ipmt-x":  nil,
		"```ipmtfoo": nil,
	}
	for line, want := range cases {
		if got := FenceMeta(line); !slices.Equal(got, want) {
			t.Errorf("FenceMeta(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestIsIPMTFenceStart(t *testing.T) {
	cases := map[string]bool{
		"```ipmt":            true,
		"```ipmt unresolved": true,
		"   ```ipmt":         true,
		"````ipmt":           true, // 4+ backticks
		"```ipmt ":           true,
		"```ipmtX":           false, // different language
		"```ipmt-x":          false, // different language
		"```ipmtfoo":         false,
		"```":                false,
		"```yaml":            false,
		"not a fence":        false,
	}
	for line, want := range cases {
		if got := IsIPMTFenceStart(line); got != want {
			t.Errorf("IsIPMTFenceStart(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestScanIPMTBlocksMeta(t *testing.T) {
	_, blocks := ScanIPMTBlocks("x\n\n```ipmt unresolved\na ::e\n```\n\n```ipmt\nb ::t\n```\n")
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(blocks))
	}
	if got := blocks[0].Meta; !slices.Equal(got, []string{"unresolved"}) {
		t.Errorf("block 0 Meta = %v, want [unresolved]", got)
	}
	if len(blocks[1].Meta) != 0 {
		t.Errorf("block 1 (bare) Meta = %v, want empty", blocks[1].Meta)
	}
}
