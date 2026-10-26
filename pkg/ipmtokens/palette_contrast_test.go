package ipmtokens

import (
	"math"
	"strconv"
	"testing"
)

func relLuminance(t *testing.T, hex string) float64 {
	t.Helper()
	if len(hex) != 7 || hex[0] != '#' {
		t.Fatalf("bad color %q", hex)
	}
	var c [3]float64
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatalf("bad color %q: %v", hex, err)
		}
		s := float64(v) / 255
		if s <= 0.03928 {
			c[i] = s / 12.92
		} else {
			c[i] = math.Pow((s+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}

func contrast(t *testing.T, a, b string) float64 {
	x, y := relLuminance(t, a), relLuminance(t, b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

// TestPalette_AccessibleOnEditorBackgrounds: every palette color reads with
// at least 4.5:1 (WCAG AA text) on its editor background: fg on the dark
// background #1f1f1f, fgLight on white.
func TestPalette_AccessibleOnEditorBackgrounds(t *testing.T) {
	for _, r := range Palette {
		if r.FGLight == "" {
			t.Errorf("%s: missing fgLight", r.Class)
			continue
		}
		if c := contrast(t, r.FG, "#1f1f1f"); c < 4.5 {
			t.Errorf("%s: dark %s on #1f1f1f is %.2f:1, want >= 4.5", r.Class, r.FG, c)
		}
		if c := contrast(t, r.FGLight, "#ffffff"); c < 4.5 {
			t.Errorf("%s: light %s on #ffffff is %.2f:1, want >= 4.5", r.Class, r.FGLight, c)
		}
	}
}
