package nodekind

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// mdCell clamps long values on a RUNE boundary: a byte-index cut through a
// multi-byte UTF-8 sequence (node text is frequently non-ASCII) would emit
// invalid UTF-8 into the report. Regression for the byte-slice truncation bug.
func TestMdCellClampsOnRuneBoundary(t *testing.T) {
	// 100 × '世' (3 bytes each = 300 bytes), over the 80-rune clamp — a byte clamp
	// at s[:77] would land mid-rune (byte 77 = 25*3+2) and emit invalid UTF-8.
	long := strings.Repeat("世", 100)
	got := mdCell(long)
	if !utf8.ValidString(got) {
		t.Fatalf("mdCell produced invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("clamped value should end with ellipsis, got %q", got)
	}
	// 77 kept runes + the ellipsis rune.
	if n := utf8.RuneCountInString(got); n != 78 {
		t.Errorf("clamped rune count = %d, want 78", n)
	}
	// A short non-ASCII value is returned unchanged (and still valid UTF-8).
	if got := mdCell("café"); got != "café" {
		t.Errorf("short value altered: %q", got)
	}
}

// findTrace returns the first record matching phase+event (event "" = any).
func findTrace(recs []TraceRecord, phase, event string) *TraceRecord {
	for i := range recs {
		if recs[i].Phase == phase && (event == "" || recs[i].Event == event) {
			return &recs[i]
		}
	}
	return nil
}

// medge is a test edge in ipm orientation (the orientation Solve consumes — for
// PartOf that means part→container; the n4l-direction adapter is tested
// separately in the n4l-import suite).
type medge struct {
	src, tgt string
	link     model.SstLinkType
}

func me(src, tgt string, link model.SstLinkType) medge { return medge{src, tgt, link} }

// mg builds a model.IpmGraph from ipm-oriented edges, deduplicating node names.
func mg(edges ...medge) *model.IpmGraph {
	g := &model.IpmGraph{}
	id := map[string]int{}
	ensure := func(name string) int {
		if _, ok := id[name]; !ok {
			id[name] = len(g.Nodes) + 1
			g.Nodes = append(g.Nodes, model.Node{ID: id[name], Name: name})
		}
		return id[name]
	}
	for _, e := range edges {
		g.Edges = append(g.Edges, model.Edge{Source: ensure(e.src), Target: ensure(e.tgt), SstLinkType: e.link})
	}
	return g
}

// Solve records a trace covering every phase, ending with a summary.
func TestTracePopulated(t *testing.T) {
	r := Solve(mg(
		me("Start", "Find Door", model.LeadsTo),
		me("Open Door", "urgent", model.Expresses),
		me("crowbar", "Open Door", model.PartOf), // part→container (ipm orientation)
	))
	for _, pe := range [][2]string{{"init", "domain"}, {"C", "resolve"}, {"D", "edge"}, {"done", "summary"}} {
		if findTrace(r.Trace, pe[0], pe[1]) == nil {
			t.Errorf("missing trace record %s/%s", pe[0], pe[1])
		}
	}
	// The summary counts must match the resolved graph.
	sum := findTrace(r.Trace, "done", "summary")
	if got := num(sum.Payload, "edges"); got != len(r.Edges) {
		t.Errorf("summary edges=%v, want %d", got, len(r.Edges))
	}
	// Every node starts with the full three-kind domain (constraint-faithful seed).
	for _, rec := range r.Trace {
		if rec.Phase == "init" && rec.Node == "Start" {
			if c := asStrings(rec.Payload["candidates"]); len(c) != 3 {
				t.Errorf("Start init domain=%v, want all three kinds", c)
			}
		}
	}
}

// The trace round-trips through JSONL unchanged (the n4l-import --debug-jsonl →
// solver-debug contract).
func TestTraceJSONLRoundTrip(t *testing.T) {
	r := Solve(mg(me("a", "b", model.LeadsTo)))
	var buf bytes.Buffer
	if err := EncodeJSONL(&buf, r.Trace); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeJSONL(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(r.Trace) {
		t.Fatalf("round-trip len: got %d, want %d", len(got), len(r.Trace))
	}
	for i := range got {
		if got[i].Phase != r.Trace[i].Phase || got[i].Event != r.Trace[i].Event ||
			got[i].Node != r.Trace[i].Node || got[i].Edge != r.Trace[i].Edge || got[i].Detail != r.Trace[i].Detail {
			t.Errorf("record %d header differs: %+v vs %+v", i, got[i], r.Trace[i])
		}
	}
	// Payload must survive (it carries the load-bearing counts/signals). Compare the
	// summary record's counts before vs after the round-trip.
	a, b := findTrace(r.Trace, "done", "summary"), findTrace(got, "done", "summary")
	if a == nil || b == nil {
		t.Fatal("summary record missing after round-trip")
	}
	for _, k := range []string{"nodes", "edges", "event", "thing", "concept", "unresolved", "splits"} {
		if num(a.Payload, k) != num(b.Payload, k) {
			t.Errorf("summary payload %q: in-memory %d != round-tripped %d", k, num(a.Payload, k), num(b.Payload, k))
		}
	}
}

// The report renders identically whether built from the in-memory trace (one-shot
// --audit) or from the JSONL round-trip (two-step solver-debug). This is the
// doc's byte-identity contract and implicitly covers full Payload fidelity.
func TestReportInMemoryEqualsRoundTrip(t *testing.T) {
	// A payload-rich graph: split node + a cycle (warnings) + a grey pair.
	r := Solve(mg(
		me("Start", "Mark", model.LeadsTo), me("report", "Mark", model.Expresses), // split Mark
		me("a", "b", model.LeadsTo), me("b", "c", model.LeadsTo), me("c", "a", model.LeadsTo), // cycle
		me("x", "y", model.NearTo), // grey pair
	))
	var buf bytes.Buffer
	if err := EncodeJSONL(&buf, r.Trace); err != nil {
		t.Fatal(err)
	}
	round, err := DecodeJSONL(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if inMem, twoStep := RenderReport(r.Trace), RenderReport(round); inMem != twoStep {
		t.Errorf("report differs between in-memory and JSONL round-trip\n--- in-memory ---\n%s\n--- two-step ---\n%s", inMem, twoStep)
	}
}

// RenderReport exercises the grey, edge-changes, and warnings sections (not just
// the always-present headings + split covered by TestRenderReportSections).
func TestRenderReportGreyEdgeWarnings(t *testing.T) {
	r := Solve(mg(
		me("a", "b", model.LeadsTo), me("b", "c", model.LeadsTo), me("c", "a", model.LeadsTo), // cycle → break
		me("x", "y", model.NearTo), // x,y stay grey
	))
	rep := RenderReport(r.Trace)
	for _, want := range []string{"## Unresolved (grey)", "## Edge changes", "## Warnings", "broke"} {
		if !strings.Contains(rep, want) {
			t.Errorf("report missing %q\n---\n%s", want, rep)
		}
	}
}

// RenderReport (used by solver-debug and --audit) produces the expected sections
// and highlights grey nodes.
func TestRenderReportSections(t *testing.T) {
	// A bare Expresses leaves both ends grey ({Event,Thing,Concept} source,
	// {Event,Concept} target) — exercises the grey section. Nothing splits under
	// constraint-faithful (an event that's also an expresses-target is just eXe).
	r := Solve(mg(
		me("doc", "topic", model.Expresses),
	))
	rep := RenderReport(r.Trace)
	for _, want := range []string{"# Node-kind solver audit", "**Resolved nodes**", "## All nodes", "## Unresolved (grey)", "Timeline"} {
		if !strings.Contains(rep, want) {
			t.Errorf("report missing %q\n---\n%s", want, rep)
		}
	}
	if !strings.Contains(rep, "topic") {
		t.Errorf("report should mention the grey node topic")
	}
}
