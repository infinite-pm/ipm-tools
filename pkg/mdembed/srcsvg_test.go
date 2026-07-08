package mdembed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Sample taken from ipm-intro/README.md Step 4 (the full graph).
// Exercises: comments, type markers (::e, ::c, ::a), arrows (-->),
// explicit relations (--::P-->), strings ("..."), comma fan-out.
const sampleIpmt = `# Top-level event
"Patrick wears black then wears white" ::e wearBW::a

# Mid-level sub-events — leads-to chain, each part-of the top
Patrick wears black t-shirt ::e wearB::a
  --> Patrick swaps t-shirt ::e swapT::a
  --> Patrick wears white t-shirt ::e wearW::a

wearB --::P--> wearBW
swapT --::P--> wearBW
wearW --::P--> wearBW

# Inner sub-events
Take off black ::e takeOff::a       --::P--> swapT
Patrick half-naked ::e halfNaked::a --::P--> swapT
Take on white ::e takeOn::a         --::P--> swapT

takeOff --> halfNaked --> takeOn

# Patrick is present for the whole swap — attach at the top.
Patrick --> wearBW
Patrick --> human ::c
swapT   --> swap of clothing ::c

# T-shirts attach at the finest level where they actually appear
t-shirt B --> wearB, takeOff
t-shirt W --> takeOn, wearW
t-shirt B --> t-shirt ::c, black ::c
t-shirt W --> t-shirt ::c, white ::c

# A color is itself a property — concept expresses concept
black ::c --> color ::c
white ::c --> color ::c
`

func TestRenderSourceSVG_BasicWellFormed(t *testing.T) {
	got, err := RenderSourceSVG(sampleIpmt)
	if err != nil {
		t.Fatalf("RenderSourceSVG returned err: %v", err)
	}
	out := string(got)
	for _, want := range []string{
		`<?xml version="1.0"`,
		`<svg xmlns="http://www.w3.org/2000/svg"`,
		`<rect`,
		`<text`,
		`<tspan fill="`,
		`</svg>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRenderSourceSVG_TokenColors(t *testing.T) {
	got, _ := RenderSourceSVG(sampleIpmt)
	out := string(got)

	// Comments should be coloured with the comment hue.
	if !strings.Contains(out, srcColors[srcTokComment]+`">`) {
		t.Errorf("expected comment colour %q in output", srcColors[srcTokComment])
	}
	// Arrows should appear.
	if !strings.Contains(out, srcColors[srcTokArrow]+`">`) {
		t.Errorf("expected arrow colour %q in output", srcColors[srcTokArrow])
	}
	// Type markers should appear.
	if !strings.Contains(out, srcColors[srcTokTypeMarker]+`">`) {
		t.Errorf("expected type-marker colour %q in output", srcColors[srcTokTypeMarker])
	}
	// Strings should appear (the sample has `"Patrick wears..."`).
	if !strings.Contains(out, srcColors[srcTokString]+`">`) {
		t.Errorf("expected string colour %q in output", srcColors[srcTokString])
	}
}

func TestRenderSourceSVG_TokenizerSpotChecks(t *testing.T) {
	cases := []struct {
		line   string
		wantTk srcTokKind // first non-default token
	}{
		{"# a comment", srcTokComment},
		{`"some string" ::e`, srcTokString},
		{`A --> B`, srcTokArrow},
		{`A <-- B`, srcTokArrow},
		{`A --- B`, srcTokArrow},
		{`A --::P--> B`, srcTokArrow},
		{`A --::N-- B`, srcTokArrow},
		{`foo ::e`, srcTokTypeMarker},
		{`foo ::tip`, srcTokTypeMarker},
	}
	for _, c := range cases {
		toks := tokenizeSourceLine(c.line)
		var firstNonDefault *srcTok
		for i := range toks {
			if toks[i].Kind != srcTokDefault {
				firstNonDefault = &toks[i]
				break
			}
		}
		if firstNonDefault == nil {
			t.Errorf("line %q: no non-default token found, want kind=%d", c.line, c.wantTk)
			continue
		}
		if firstNonDefault.Kind != c.wantTk {
			t.Errorf("line %q: first non-default token kind=%d, want %d (text=%q)",
				c.line, firstNonDefault.Kind, c.wantTk, firstNonDefault.Text)
		}
	}
}

// TestRenderSourceSVG_WriteSample writes the sample SVG to /tmp/ for visual
// inspection. Skipped in -short mode; useful when iterating on visuals.
func TestRenderSourceSVG_WriteSample(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped in -short mode")
	}
	got, err := RenderSourceSVG(sampleIpmt)
	if err != nil {
		t.Fatalf("RenderSourceSVG: %v", err)
	}
	path := filepath.Join(os.TempDir(), "ipmt-src-sample.svg")
	if err := os.WriteFile(path, got, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	t.Logf("wrote sample to %s (%d bytes)", path, len(got))
}
