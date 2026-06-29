package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A 3-edge SVG: one vertical, one horizontal, one diagonal (~45° from
// vertical), each in its own edge group with an arrowhead path that must be
// ignored.
const sampleSVG = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="400" height="400">
  <g id="node-shadows">
    <rect x="0" y="0" width="10" height="10"/>
  </g>
  <g id="edges">
    <g class="edge" data-edge-idx="0" data-edge-base="leadsto" data-edge-from="1" data-edge-to="2">
    <line x1="100.00" y1="50.00" x2="100.00" y2="150.00" stroke="#000" />
    <path d="M 100 150 L 95 140 L 105 140 Z" fill="#000"/>
    </g>
    <g class="edge" data-edge-idx="1" data-edge-base="leadsto" data-edge-from="2" data-edge-to="3">
    <line x1="100.00" y1="200.00" x2="300.00" y2="200.00" stroke="#000" />
    <path d="M 300 200 L 290 195 L 290 205 Z" fill="#000"/>
    </g>
    <g class="edge" data-edge-idx="2" data-edge-base="expresses" data-edge-from="3" data-edge-to="4">
    <line x1="100.00" y1="100.00" x2="200.00" y2="200.00" stroke="#000" />
    <path d="M 200 200 L 190 195 L 195 205 Z" fill="#000"/>
    </g>
  </g>
</svg>
`

func writeSample(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "case.ipm.svg")
	if err := os.WriteFile(path, []byte(sampleSVG), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseSVGEdges(t *testing.T) {
	path := writeSample(t)
	segs, err := parseSVGEdges(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 3 {
		t.Fatalf("want 3 segments (arrowhead paths ignored), got %d", len(segs))
	}

	// Edge 0: vertical.
	if got := segs[0].class(); got != "V" {
		t.Errorf("edge 0 class = %q, want V", got)
	}
	if segs[0].base != "leadsto" || segs[0].from != "1" || segs[0].to != "2" {
		t.Errorf("edge 0 attrs wrong: %+v", segs[0])
	}

	// Edge 1: horizontal.
	if got := segs[1].class(); got != "H" {
		t.Errorf("edge 1 class = %q, want H", got)
	}

	// Edge 2: 45° diagonal.
	if !segs[2].tilted() {
		t.Errorf("edge 2 should be tilted: %+v", segs[2])
	}
	if a := segs[2].angleFromVertical(); a < 44.9 || a > 45.1 {
		t.Errorf("edge 2 angle = %.2f, want ~45", a)
	}
}

func TestFilterSegments(t *testing.T) {
	path := writeSample(t)
	segs, err := parseSVGEdges(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := filterSegments(segs, "expresses", "", "", 0); len(got) != 1 || got[0].edgeIdx != "2" {
		t.Errorf("--base expresses should keep only edge 2, got %d", len(got))
	}
	if got := filterSegments(segs, "", "2", "3", 0); len(got) != 1 || got[0].edgeIdx != "1" {
		t.Errorf("--edge 2,3 should keep only edge 1, got %d", len(got))
	}
	// --min-angle drops the straight V/H edges, keeps the 45° diagonal.
	if got := filterSegments(segs, "", "", "", 10); len(got) != 1 || got[0].edgeIdx != "2" {
		t.Errorf("--min-angle 10 should keep only the diagonal, got %d", len(got))
	}
	if got := filterSegments(segs, "", "", "", 60); len(got) != 0 {
		t.Errorf("--min-angle 60 should keep nothing, got %d", len(got))
	}
}

func TestLoadLabelLookup(t *testing.T) {
	dir := t.TempDir()
	svg := filepath.Join(dir, "case.ipm.svg")
	if err := os.WriteFile(svg, []byte(sampleSVG), 0o644); err != nil {
		t.Fatal(err)
	}
	layout := filepath.Join(dir, "case.layout.json")
	if err := os.WriteFile(layout, []byte(`{"nodes":[{"id":"1","label":"Alpha"},{"id":"2","label":"Beta"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	lookup := loadLabelLookup(svg)
	if lookup["1"] != "Alpha" || lookup["2"] != "Beta" {
		t.Errorf("label lookup wrong: %v", lookup)
	}
	if got := labelOr(lookup, "9"); got != "9" {
		t.Errorf("labelOr should fall back to id, got %q", got)
	}
	// Absent sidecar → nil, no error.
	if loadLabelLookup(filepath.Join(dir, "missing.ipm.svg")) != nil {
		t.Error("missing sidecar should yield nil lookup")
	}
}
