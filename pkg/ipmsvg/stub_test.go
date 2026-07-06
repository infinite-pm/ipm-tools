package ipmsvg

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

func TestStubFanOutSeparatesBadges(t *testing.T) {
	g := &layout.Graph{
		Nodes: []layout.Node{
			{ID: "C", Type: "event", RenderKind: "event-container", X: 40, Y: 40, Width: 200, Height: 280,
				Container: &layout.Container{ChildNodeIDs: []string{"ch"}}},
			{ID: "ch", Type: "event", X: 70, Y: 160, Width: 90, Height: 40, ParentNodeIDs: []string{"C"}},
			{ID: "o1", Type: "concept", X: 600, Y: 60, Width: 90, Height: 40},
			{ID: "o2", Type: "concept", X: 600, Y: 170, Width: 90, Height: 40},
			{ID: "o3", Type: "concept", X: 600, Y: 280, Width: 90, Height: 40},
		},
		Edges: []layout.Edge{
			{From: "ch", To: "o1", Base: "expresses", Style: "expresses", Visibility: "stubbed"},
			{From: "ch", To: "o2", Base: "expresses", Style: "expresses", Visibility: "stubbed"},
			{From: "ch", To: "o3", Base: "expresses", Style: "expresses", Visibility: "stubbed"},
		},
	}
	g.Meta.Bounds.Width = 800
	g.Meta.Bounds.Height = 400

	gen, err := NewGenerator()
	if err != nil {
		t.Fatal(err)
	}
	out, err := gen.Generate(g)
	if err != nil {
		t.Fatal(err)
	}

	rx := regexp.MustCompile(`class="edge-badge">(?:<title>[^<]*</title>)?<rect x="([-\d.]+)" y="([-\d.]+)" width="([-\d.]+)" height="([-\d.]+)"`)
	var rects [][4]float64
	for _, m := range rx.FindAllStringSubmatch(string(out), -1) {
		var r [4]float64
		for i := range 4 {
			r[i], _ = strconv.ParseFloat(m[i+1], 64)
		}
		rects = append(rects, r)
	}
	if len(rects) != 6 {
		t.Fatalf("expected 6 badges (3 stub edges × 2 ends), got %d", len(rects))
	}
	if !strings.Contains(string(out), `class="edge-badge"><title>Expresses`) {
		t.Errorf("expected a native <title> tooltip on the badge (hover the ? to see where it leads)")
	}
	for i := range rects {
		for j := i + 1; j < len(rects); j++ {
			a, b := rects[i], rects[j]
			if a[0] < b[0]+b[2] && b[0] < a[0]+a[2] && a[1] < b[1]+b[3] && b[1] < a[1]+a[3] {
				t.Errorf("badges overlap (fan-out failed): %v vs %v", a, b)
			}
		}
	}
}

// An Expresses/NearTo edge that crosses a
// composite-container boundary renders as stubs (hidden edge-full + two edge-stub
// + "?" badges). Intra-component edges and leadsto/partof edges are untouched, and
// the option off renders everything normally.
func TestStubCrossComponentEdges(t *testing.T) {
	g := &layout.Graph{
		Nodes: []layout.Node{
			{ID: "C", Type: "event", RenderKind: "event-container", X: 40, Y: 40, Width: 200, Height: 140,
				Container: &layout.Container{ChildNodeIDs: []string{"ch"}}},
			{ID: "ch", Type: "event", X: 70, Y: 80, Width: 90, Height: 40, ParentNodeIDs: []string{"C"}},
			{ID: "out", Type: "concept", X: 600, Y: 80, Width: 90, Height: 40},
			{ID: "out2", Type: "concept", X: 760, Y: 80, Width: 90, Height: 40},
		},
		Edges: []layout.Edge{
			{From: "ch", To: "out", Base: "expresses", Style: "expresses", Label: "has example", Visibility: "stubbed"}, // cross-component → stub
			{From: "out", To: "out2", Base: "expresses", Style: "expresses"},                                            // both top-level → no stub
			{From: "ch", To: "out", Base: "leadsto", Style: "leadsto"},                                                  // leadsto → never stub
		},
	}
	g.Meta.Bounds.Width = 900
	g.Meta.Bounds.Height = 300

	gen, err := NewGenerator()
	if err != nil {
		t.Fatal(err)
	}
	out, err := gen.Generate(g)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	// Exactly one edge (the cross-component expresses) is stubbed → 1 hidden full +
	// 2 stubs (source/target) + the numbered pair badges. Stubbing follows
	// Edge.Visibility by DEFAULT: what the canvas
	// hides, the flat SVG shows as a numbered stub pair.
	if n := strings.Count(s, `class="edge-full"`); n != 1 {
		t.Errorf("edge-full count = %d, want 1 (only the cross-component expresses)\n%s", n, s)
	}
	if n := strings.Count(s, `class="edge-stub"`); n != 2 {
		t.Errorf("edge-stub count = %d, want 2 (source+target of the one stubbed edge)", n)
	}
	// Both ends carry the SAME pair number so the reader can match them. Codes
	// start at "2": "1" is reserved for the always-visible
	// first connection, so a hidden edge is numbered from 2.
	if n := strings.Count(s, ">2</text>"); n != 2 {
		t.Errorf("pair-number badge count = %d, want 2 (same number on both ends)\n%s", n, s)
	}
	// The ghost line: one faint straight badge-to-badge hairline per
	// stubbed edge, so the eye can trace the connection in the background.
	if n := strings.Count(s, `class="edge-ghost"`); n != 1 {
		t.Errorf("edge-ghost count = %d, want 1\n%s", n, s)
	}
}

func TestStubPairCodes(t *testing.T) {
	cases := map[int]string{
		1:  "1",
		9:  "9",
		10: "A",
		11: "C", // B is a lookalike of 8 and skipped
		28: "Y",
		29: "11", // single chars exhausted, two-char codes begin
		30: "12",
		56: "1Y",
		57: "21",
	}
	for n, want := range cases {
		if got := stubPairCode(n); got != want {
			t.Errorf("stubPairCode(%d) = %q, want %q", n, got, want)
		}
	}
	for _, banned := range "BGIOQSZ0" {
		if strings.ContainsRune(stubPairAlphabet, banned) {
			t.Errorf("lookalike %q must not be in the badge alphabet", banned)
		}
	}
}

// The visibility class also stubs long Expresses/NearTo edges regardless of container
// membership — the cross-canvas glue that most kubernetes graphs draw between
// top-level T/C nodes (which the cross-container rule alone misses).
func TestStubLongEdges(t *testing.T) {
	g := &layout.Graph{
		Nodes: []layout.Node{
			{ID: "a", Type: "concept", X: 40, Y: 80, Width: 90, Height: 40},
			{ID: "b", Type: "concept", X: 2000, Y: 80, Width: 90, Height: 40}, // far → long edge
			{ID: "c", Type: "concept", X: 180, Y: 80, Width: 90, Height: 40},  // near a → short edge
		},
		Edges: []layout.Edge{
			{From: "a", To: "b", Base: "expresses", Style: "expresses", Visibility: "stubbed"}, // long → stub (class from layout)
			{From: "a", To: "c", Base: "expresses", Style: "expresses"},                        // short → no stub
		},
	}
	g.Meta.Bounds.Width = 2200
	g.Meta.Bounds.Height = 200

	gen, err := NewGenerator()
	if err != nil {
		t.Fatal(err)
	}
	out, err := gen.Generate(g)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(out), `class="edge-full"`); n != 1 {
		t.Errorf("edge-full = %d, want 1 (only the long a→b edge stubs)\n%s", n, string(out))
	}
}
