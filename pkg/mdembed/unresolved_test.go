package mdembed

import (
	"strings"
	"testing"
)

// A ```ipmt unresolved fence is detected and its metadata captured; a bare
// ```ipmt fence carries no metadata.
func TestFenceMetaUnresolved(t *testing.T) {
	_, blocks := ScanBlocks("intro\n\n```ipmt unresolved\na ::?etc --::L--> b ::?etc\n```\n\nend\n")
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	if got := blocks[0].Meta; len(got) != 1 || got[0] != "unresolved" {
		t.Fatalf("Meta = %v, want [unresolved]", got)
	}
	_, bare := ScanBlocks("```ipmt\nx ::e\n```\n")
	if len(bare) != 1 || len(bare[0].Meta) != 0 {
		t.Errorf("bare fence Meta = %v, want empty", bare[0].Meta)
	}
}

// End-to-end: an `unresolved` block is solved before rendering, so an undecided
// node the solver can decide renders as its resolved kind — not grey — while the
// same content without the directive renders grey.
func TestRenderUnresolvedSolvesBeforeSVG(t *testing.T) {
	const content = "a ::?etc --::L--> b ::?etc" // leads-to forces both Event

	solved, err := RenderSVGBytes("/root", "/root/x.md",
		BlockResult{Content: content, Meta: []string{"unresolved"}}, "test")
	if err != nil {
		t.Fatalf("render unresolved: %v", err)
	}
	plain, err := RenderSVGBytes("/root", "/root/x.md",
		BlockResult{Content: content}, "test")
	if err != nil {
		t.Fatalf("render plain: %v", err)
	}

	const greyFill = "#ececec"    // ipmsvg "unresolved" node fill
	const eventStroke = "#ff8000" // ipmsvg event styling

	// Solved → both nodes resolved to events: event styling, no grey fill.
	if strings.Contains(string(solved), greyFill) {
		t.Errorf("solved SVG must not render grey nodes (%s)", greyFill)
	}
	if !strings.Contains(string(solved), eventStroke) {
		t.Errorf("solved SVG should contain event styling (%s)", eventStroke)
	}
	// Plain (no directive) → the ::?etc nodes render grey.
	if !strings.Contains(string(plain), greyFill) {
		t.Errorf("plain unsolved SVG should render grey nodes (%s)", greyFill)
	}
}

// The `defaults` directive fully resolves a block: nodes the type-pairs leave grey
// are decided to their role-preferred kind, so the SVG has NO grey nodes — even an
// Expresses target (grey under strict `unresolved`) becomes a decided Concept.
func TestRenderDefaultsFullyResolves(t *testing.T) {
	const content = "server ::?etc --::X--> reliability ::?etc" // strict: both grey
	const greyFill = "#ececec"

	strict, err := RenderSVGBytes("/root", "/root/x.md",
		BlockResult{Content: content, Meta: []string{"unresolved"}}, "test")
	if err != nil {
		t.Fatalf("render unresolved: %v", err)
	}
	defaulted, err := RenderSVGBytes("/root", "/root/x.md",
		BlockResult{Content: content, Meta: []string{"unresolved", "defaults"}}, "test")
	if err != nil {
		t.Fatalf("render defaults: %v", err)
	}

	if !strings.Contains(string(strict), greyFill) {
		t.Errorf("strict `unresolved` should leave the Expresses pair grey (%s)", greyFill)
	}
	if strings.Contains(string(defaulted), greyFill) {
		t.Errorf("`defaults` must fully resolve — no grey nodes (%s)", greyFill)
	}
}
