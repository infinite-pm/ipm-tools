package main

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

func TestRenderTable(t *testing.T) {
	graph := &layout.Graph{
		Nodes: []layout.Node{
			{ID: "2", Label: "e2", Type: "event", X: 40, Y: 240, Width: 120, Height: 60},
			{ID: "1", Label: "long\nlabel", Type: "event", X: 40, Y: 120, Width: 120, Height: 60},
			{ID: "3", Label: "S", Type: "boundary", X: 80, Y: 40, Width: 40, Height: 40},
		},
	}

	got := renderTable(graph)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want header + 3 rows, got %d lines:\n%s", len(lines), got)
	}
	if !strings.HasPrefix(lines[0], "ID") || !strings.Contains(lines[0], "LABEL") {
		t.Errorf("missing header: %q", lines[0])
	}

	// Sorted by y: S (40), then node 1 (120), then node 2 (240).
	for i, wantID := range []string{"3", "1", "2"} {
		if !strings.HasPrefix(lines[i+1], wantID) {
			t.Errorf("row %d: want node %s first, got %q", i+1, wantID, lines[i+1])
		}
	}

	// Multi-line label flattened to a single row.
	if !strings.Contains(lines[2], `long\nlabel`) {
		t.Errorf("wrapped label not flattened: %q", lines[2])
	}
}
