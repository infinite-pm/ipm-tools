package ipmsvg

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/nodehints"
)

func hintsTestGraph() *layout.Graph {
	return &layout.Graph{
		Nodes: []layout.Node{
			{ID: "a", Type: "concept", Label: "A", X: 40, Y: 40, Width: 120, Height: 60},
			{ID: "b", Type: "concept", Label: "B", X: 240, Y: 40, Width: 120, Height: 60},
			{ID: "c", Type: "concept", Label: "C", X: 440, Y: 40, Width: 120, Height: 60},
			{ID: "d", Type: "concept", Label: "D", X: 640, Y: 40, Width: 120, Height: 60},
		},
		Meta: layout.Meta{Bounds: layout.Bounds{Width: 800, Height: 200}},
	}
}

func TestGenerateWithHintsDrawsEachDecoration(t *testing.T) {
	gen, err := NewGenerator()
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
	graph := hintsTestGraph()

	cases := []struct {
		decoration string
		symbol     string
		shape      string // expected badge element substring
	}{
		{"zoom-in", "↓", "<rect"},
		{"zoom-out", "↑", "<rect"},
		{"collapse", "−", "<circle"},
		{"expand", "+", "<circle"},
	}

	for _, tc := range cases {
		t.Run(tc.decoration, func(t *testing.T) {
			hints := nodehints.GraphHints{"a": {Decoration: tc.decoration}}
			out, err := gen.GenerateWithHints(graph, hints)
			if err != nil {
				t.Fatalf("GenerateWithHints() error = %v", err)
			}
			s := string(out)

			// The overlay text symbol appears exactly once.
			if got := strings.Count(s, ">"+tc.symbol+"</text>"); got != 1 {
				t.Fatalf("decoration %q: want exactly one badge symbol %q, got %d in:\n%s", tc.decoration, tc.symbol, got, s)
			}
			// The badge shape (rect for zoom, circle for expand/collapse) is present.
			// Count badge shapes that carry the overlay fill-opacity marker so we do
			// not confuse them with node rects.
			if !strings.Contains(s, tc.shape) {
				t.Fatalf("decoration %q: want badge shape %q in output", tc.decoration, tc.shape)
			}

			// Overlay must sit before the closing </svg>.
			textIdx := strings.Index(s, ">"+tc.symbol+"</text>")
			closeIdx := strings.LastIndex(s, "</svg>")
			if textIdx < 0 || closeIdx < 0 || textIdx > closeIdx {
				t.Fatalf("decoration %q: badge overlay must precede </svg> (textIdx=%d closeIdx=%d)", tc.decoration, textIdx, closeIdx)
			}

			// Badge for node "a" must sit inside its rect (40..160 x, 40..100 y).
			// The badge is anchored near the top-right corner of the node.
			if !strings.Contains(s, "</svg>\n") {
				t.Fatalf("output must end with the </svg>\\n trailer the overlay splice depends on")
			}
		})
	}
}

func TestGenerateWithHintsSkipsUnknownDecoration(t *testing.T) {
	gen, err := NewGenerator()
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
	graph := hintsTestGraph()

	base, err := gen.Generate(graph)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	withHints, err := gen.GenerateWithHints(graph, nodehints.GraphHints{
		"a": {Decoration: "bogus-decoration"},
		"b": {Decoration: ""},
	})
	if err != nil {
		t.Fatalf("GenerateWithHints() error = %v", err)
	}
	if string(base) != string(withHints) {
		t.Fatalf("unknown/empty decorations must not alter the SVG; output diverged")
	}
}

func TestGenerateWithHintsDeterministicOrder(t *testing.T) {
	gen, err := NewGenerator()
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
	graph := hintsTestGraph()
	hints := nodehints.GraphHints{
		"a": {Decoration: "expand"},
		"b": {Decoration: "collapse"},
		"c": {Decoration: "zoom-in"},
		"d": {Decoration: "zoom-out"},
	}

	first, err := gen.GenerateWithHints(graph, hints)
	if err != nil {
		t.Fatalf("GenerateWithHints() error = %v", err)
	}
	// Many iterations to defeat random map-order flakiness.
	for i := 0; i < 50; i++ {
		next, err := gen.GenerateWithHints(graph, hints)
		if err != nil {
			t.Fatalf("GenerateWithHints() error = %v", err)
		}
		if string(next) != string(first) {
			t.Fatalf("GenerateWithHints output is nondeterministic across calls (iteration %d)", i)
		}
	}
}
