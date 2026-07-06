package ipmsvg

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

func TestGenerateClipsExpressesArrowToNodeBorder(t *testing.T) {
	graph := &layout.Graph{
		Nodes: []layout.Node{
			{ID: "1", Type: "thing", Label: "tA", X: 40, Y: 120, Width: 120, Height: 60},
			{ID: "2", Type: "concept", Label: "cA", X: 280, Y: 120, Width: 120, Height: 60},
		},
		Edges: []layout.Edge{{From: "1", To: "2", Dir: "fwd", Base: "expresses", Style: "expresses"}},
		Meta:  layout.Meta{Bounds: layout.Bounds{Width: 440, Height: 300}},
	}

	svg, err := Render(graph)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := string(svg)

	if !strings.Contains(out, "stroke-dasharray=\"9 6\"") {
		t.Fatalf("expresses edge should use longer dash pattern, got: %s", out)
	}
	if strings.Contains(out, "M 340.00 150.00") {
		t.Fatalf("expresses arrow tip should not be drawn at the target center, got: %s", out)
	}
	if !strings.Contains(out, "M 275.00 150.00") {
		t.Fatalf("expresses arrow tip should sit a clear gap off the target border, got: %s", out)
	}
	if !strings.Contains(out, "x2=\"268.25\"") {
		t.Fatalf("expresses line should end before the arrowhead at the target border, got: %s", out)
	}
}

func TestGenerateKeepsNearToArrowless(t *testing.T) {
	graph := &layout.Graph{
		Nodes: []layout.Node{
			{ID: "1", Type: "event", Label: "e1", X: 40, Y: 120, Width: 120, Height: 60},
			{ID: "2", Type: "event", Label: "e2", X: 280, Y: 120, Width: 120, Height: 60},
		},
		Edges: []layout.Edge{{From: "1", To: "2", Dir: "undir", Base: "nearto", Style: "nearto"}},
		Meta:  layout.Meta{Bounds: layout.Bounds{Width: 440, Height: 300}},
	}

	svg, err := Render(graph)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := string(svg)

	// the dot period is FITTED to the drawn length so the last dot
	// lands beside the far node — assert the dot pattern
	// ("0 <gap>") and the round caps, not the exact period
	if !strings.Contains(out, "stroke-dasharray=\"0 ") || !strings.Contains(out, "stroke-linecap=\"round\"") {
		t.Fatalf("near-to edge should render as round-capped dots, got: %s", out)
	}
	if strings.Contains(out, "<path d=\"M 280.00 150.00") || strings.Contains(out, "<path d=\"M 160.00 150.00") {
		t.Fatalf("near-to edge should not render arrowheads, got: %s", out)
	}
}
