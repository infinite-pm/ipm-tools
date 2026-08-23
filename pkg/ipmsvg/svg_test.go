package ipmsvg

import (
	"encoding/xml"
	"io"
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

// A label wide enough to be hard-split mid-word, with apostrophes sitting where
// the split lands. Escaping before wrapping turned the "&#39;" entities into
// budget-eating five-character words and let the split cut one in half, which
// made the whole document unparseable.
func TestGenerateEscapesNodeLabelAfterWrapping(t *testing.T) {
	graph := &layout.Graph{
		Nodes: []layout.Node{
			{ID: "1", Type: "thing", Label: "tAB'tC'tD'tE'tF", X: 40, Y: 120, Width: 120, Height: 60},
			{ID: "2", Type: "concept", Label: "cA \"cB\" & cC", X: 280, Y: 120, Width: 120, Height: 60},
		},
		Edges: []layout.Edge{{From: "1", To: "2", Dir: "fwd", Base: "expresses", Style: "expresses"}},
		Meta:  layout.Meta{Bounds: layout.Bounds{Width: 440, Height: 300}},
	}

	svg, err := Render(graph)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := string(svg)

	dec := xml.NewDecoder(strings.NewReader(out))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("rendered SVG is not well-formed XML: %v\ngot: %s", err, out)
		}
	}

	// The wrap budget counts what the reader sees, so a 17-character label in a
	// 12-character-wide node splits once — not four times, once per entity.
	if n := strings.Count(out, "<text"); n > 4 {
		t.Fatalf("label wrapped on escaped text (%d <text> elements), got: %s", n, out)
	}
}

// An edge tooltip (`--"why it happens"-->`) hovers like a node's. It was
// parsed and then dropped at the layout boundary, so the four tooltip examples
// in the spec rendered as plain arrows; the <title> is the first child of the
// edge group, covering line, arrowhead and stub badges alike.
func TestGenerateRendersEdgeTooltipAsTitle(t *testing.T) {
	graph := &layout.Graph{
		Nodes: []layout.Node{
			{ID: "1", Type: "event", Label: "e1", X: 40, Y: 120, Width: 120, Height: 60},
			{ID: "2", Type: "event", Label: "e2", X: 40, Y: 240, Width: 120, Height: 60},
		},
		Edges: []layout.Edge{
			{From: "1", To: "2", Dir: "fwd", Base: "leadsto", Style: "leadsto", Tooltip: "why it happens <&>"},
		},
		Meta: layout.Meta{Bounds: layout.Bounds{Width: 200, Height: 360}},
	}

	svg, err := Render(graph)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := string(svg)
	if !strings.Contains(out, "<title>why it happens &lt;&amp;&gt;</title>") {
		t.Fatalf("edge tooltip should be an escaped <title>, got: %s", out)
	}
	edgeAt := strings.Index(out, "<g class=\"edge\"")
	titleAt := strings.Index(out, "<title>why it happens")
	lineAt := strings.Index(out, "<line")
	if edgeAt < 0 || titleAt < edgeAt || (lineAt >= 0 && titleAt > lineAt) {
		t.Fatalf("the title must open the edge group, before its geometry; got: %s", out)
	}
}

// A participant drawn in several places (pkg/layoutdupe stamps layout.Node.CopyOf)
// wears the place it is — "1/2", "2/2" — in reading order. A still picture needs
// the badge more than the canvas does: it has no click to prove two boxes are one
// thing.
func TestGenerateBadgesEveryPlaceOfACopiedNode(t *testing.T) {
	graph := &layout.Graph{
		Nodes: []layout.Node{
			{ID: "1", Type: "event", Label: "e1", X: 40, Y: 120, Width: 120, Height: 60},
			{ID: "2", Type: "concept", Label: "cX", X: 280, Y: 120, Width: 120, Height: 60},
			{ID: "3", Type: "concept", Label: "cX", X: 280, Y: 400, Width: 120, Height: 60, CopyOf: "2"},
		},
		Edges: []layout.Edge{{From: "1", To: "2", Dir: "fwd", Base: "expresses", Style: "expresses"}},
		Meta:  layout.Meta{Bounds: layout.Bounds{Width: 460, Height: 520}},
	}

	svg, err := Render(graph)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := string(svg)
	for _, want := range []string{
		`data-node-id="2" data-of="2"`, // the original is a place too
		`data-node-id="3" data-of="2"`,
		">1/2</text>",
		">2/2</text>",
		"<title>drawn in 2 places — this is 1/2</title>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in: %s", want, out)
		}
	}
	if strings.Count(out, "copy-badge") != 2 {
		t.Fatalf("want one badge per place, got %d: %s", strings.Count(out, "copy-badge"), out)
	}

	// A node drawn once is never badged.
	graph.Nodes = graph.Nodes[:2]
	svg, err = Render(graph)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(svg), "copy-badge") {
		t.Fatalf("a single-placement diagram must carry no badges: %s", svg)
	}
}
