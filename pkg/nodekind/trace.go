package nodekind

// Solver audit trace — the node-kind analogue of pkg/layout's debug JSONL.
//
// The solver records one TraceRecord per decision as it runs (signals collected,
// base kind, resolution, split, each edge, and every post-pass mutation). The
// trace is exposed as Result.Trace; EncodeJSONL/DecodeJSONL serialize it and
// RenderReport narrativizes it to markdown. Driver commands that emit the JSONL
// or audit report (e.g. an n4l-import / solver-debug front-end) live in sibling
// repos; this repo ships cmd-dev/solver-example, which exercises Solve+ToGraph.
//
// Records are self-contained: a final "summary" record carries the counts, so a
// report can be rendered from the JSONL alone, with no access to the live graph.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// TraceRecord is one solver decision. Node is the originating N4L node text (the
// stable key across phases — a split fans one text to two resolved nodes, but
// both keep this Origin); Edge is "src → tgt". Payload holds event-specific data.
type TraceRecord struct {
	Phase    string         `json:"phase"`
	Event    string         `json:"event"`
	Producer string         `json:"producer,omitempty"`
	Node     string         `json:"node,omitempty"`
	Edge     string         `json:"edge,omitempty"`
	Detail   string         `json:"detail,omitempty"`
	Payload  map[string]any `json:"payload,omitempty"`
}

// trace appends a record to the in-memory trace (always on; serialized only when
// a caller asks). Mirrors layout's recorder but needs no file handle.
func (r *Result) trace(rec TraceRecord) { r.Trace = append(r.Trace, rec) }

// warn records a human-facing warning both on Result.Warnings (the summary list)
// and as a structured trace record, so the JSONL stream stays self-contained.
func (r *Result) warn(rec TraceRecord) {
	r.Warnings = append(r.Warnings, rec.Detail)
	rec.Payload = mergePayload(rec.Payload, map[string]any{"warning": true})
	r.trace(rec)
}

// mergePayload returns a new map overlaying b onto a, without mutating either —
// so a caller's payload literal is never altered by warn().
func mergePayload(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// EncodeJSONL writes records as one JSON object per line.
func EncodeJSONL(w io.Writer, recs []TraceRecord) error {
	enc := json.NewEncoder(w)
	for _, rec := range recs {
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return nil
}

// DecodeJSONL reads a JSONL trace stream produced by EncodeJSONL.
func DecodeJSONL(rd io.Reader) ([]TraceRecord, error) {
	var out []TraceRecord
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec TraceRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("decode trace line: %w", err)
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

// --- report rendering (consumes records only, so solver-debug needs just JSONL) ---

// RenderReport narrativizes a trace into a markdown report: a summary, a per-node
// resolution table (signals → base → final, with notes), a per-edge table
// (kept / dropped / relaxed / cycle-broken), the warnings, and a collapsed timeline.
func RenderReport(recs []TraceRecord) string {
	var b strings.Builder
	b.WriteString("<!-- AUTO-GENERATED from a nodekind solver trace (RenderReport) -->\n")
	b.WriteString("# Node-kind solver audit\n\n")

	// Summary (from the final summary record).
	for _, rec := range recs {
		if rec.Event == "summary" {
			p := rec.Payload
			b.WriteString("## Summary\n\n")
			fmt.Fprintf(&b, "**Resolved nodes**: %d — %d event, %d thing, %d concept, %d unresolved (grey)  \n",
				num(p, "nodes"), num(p, "event"), num(p, "thing"), num(p, "concept"), num(p, "unresolved"))
			fmt.Fprintf(&b, "**Original N4L nodes**: %d (%d split poly-kinded)  \n", num(p, "originNodes"), num(p, "splits"))
			fmt.Fprintf(&b, "**Edges**: %d  **Dropped edges**: %d  **Warnings**: %d  \n\n",
				num(p, "edges"), num(p, "dropped"), num(p, "warnings"))
		}
	}

	stories := collectNodeStories(recs)

	// Highlight the cases a reviewer cares about: grey nodes the user must confirm.
	var grey []*nodeStory
	for _, s := range stories {
		if s.finalType == string(model.Unresolved) {
			grey = append(grey, s)
		}
	}
	if len(grey) > 0 {
		b.WriteString("## Unresolved (grey) — confirm the kind\n\n")
		for _, s := range grey {
			fmt.Fprintf(&b, "- %q → candidates [%s]\n", s.name, strings.Join(s.candidates, ", "))
		}
		b.WriteString("\n")
	}

	// Per-node table (sorted for stable diffs). "init domain" is the starting
	// candidate set; "final" is the arc-consistency result.
	b.WriteString("## All nodes\n\n")
	b.WriteString("| node | init domain | final | notes |\n| --- | --- | --- | --- |\n")
	for _, s := range stories {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
			mdCell(s.name), s.base, mdCell(s.finalText()), mdCell(strings.Join(s.notes, "; ")))
	}
	b.WriteString("\n")

	// Per-edge events that changed an edge (dropped / relaxed / cycle-broken).
	var edgeEvents []TraceRecord
	for _, rec := range recs {
		if rec.Edge != "" && rec.Phase != "D" {
			edgeEvents = append(edgeEvents, rec)
		}
	}
	if len(edgeEvents) > 0 {
		b.WriteString("## Edge changes\n\n")
		b.WriteString("| edge | event | detail |\n| --- | --- | --- |\n")
		for _, rec := range edgeEvents {
			fmt.Fprintf(&b, "| %s | %s/%s | %s |\n", mdCell(rec.Edge), rec.Phase, rec.Event, mdCell(rec.Detail))
		}
		b.WriteString("\n")
	}

	// Warnings.
	var warnings []string
	for _, rec := range recs {
		if rec.Payload != nil && rec.Payload["warning"] == true {
			warnings = append(warnings, rec.Detail)
		}
	}
	if len(warnings) > 0 {
		b.WriteString("## Warnings\n\n")
		for _, w := range warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
		b.WriteString("\n")
	}

	// Full timeline (collapsed).
	b.WriteString("<details><summary>Timeline (every record)</summary>\n\n")
	b.WriteString("| phase | event | node/edge | detail |\n| --- | --- | --- | --- |\n")
	for _, rec := range recs {
		ne := rec.Node
		if ne == "" {
			ne = rec.Edge
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", rec.Phase, rec.Event, mdCell(ne), mdCell(rec.Detail))
	}
	b.WriteString("\n</details>\n")
	return b.String()
}

type nodeStory struct {
	name       string
	base       string // the node's initial kind domain (init/domain record)
	finalType  string
	candidates []string
	split      bool
	notes      []string
}

func (s *nodeStory) finalText() string {
	if s.split {
		return "split: " + s.finalType
	}
	if s.finalType == string(model.Unresolved) {
		return "Unresolved[" + strings.Join(s.candidates, ", ") + "]"
	}
	return s.finalType
}

// collectNodeStories aggregates per-node records (keyed by Node text) into the
// signals → base → final narrative, in first-seen order.
func collectNodeStories(recs []TraceRecord) []*nodeStory {
	byName := map[string]*nodeStory{}
	var order []*nodeStory
	get := func(name string) *nodeStory {
		s := byName[name]
		if s == nil {
			s = &nodeStory{name: name}
			byName[name] = s
			order = append(order, s)
		}
		return s
	}
	for _, rec := range recs {
		if rec.Node == "" {
			continue
		}
		s := get(rec.Node)
		switch {
		case rec.Phase == "init" && rec.Event == "domain":
			s.base = joinOr(asStrings(rec.Payload["candidates"]), "—")
		case rec.Event == "split":
			s.split = true
			s.finalType = fmt.Sprintf("%s + %s", str(rec.Payload, "solid"), str(rec.Payload, "concept"))
		default:
			// Any record carrying a "type" updates the final state (resolve, decide…)
			// — but never overwrite a split node's "solid + concept" final (a later
			// post-pass, e.g. promote-container, targets the same Origin).
			if !s.split && rec.Payload != nil {
				if t, ok := rec.Payload["type"]; ok {
					s.finalType = fmt.Sprint(t)
					s.candidates = asStrings(rec.Payload["candidates"])
				}
			}
			if rec.Detail != "" && rec.Phase != "C" {
				s.notes = append(s.notes, rec.Phase+": "+rec.Detail)
			}
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].name < order[j].name })
	return order
}

// --- small helpers ---

func num(p map[string]any, k string) int {
	if p == nil {
		return 0
	}
	switch n := p[k].(type) {
	case float64: // JSON numbers decode to float64
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func str(p map[string]any, k string) string {
	if p == nil {
		return ""
	}
	if v, ok := p[k]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

func asStrings(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			out = append(out, fmt.Sprint(e))
		}
		return out
	}
	return nil
}

func kindStrings(ks []model.NodeType) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = string(k)
	}
	return out
}

func joinOr(parts []string, empty string) string {
	if len(parts) == 0 {
		return empty
	}
	return strings.Join(parts, "; ")
}

// mdCell escapes a value for a markdown table cell (pipes, newlines) and clamps
// length. Clamping is rune-aware: node/edge text is frequently non-ASCII, so a
// byte-index cut could split a multi-byte UTF-8 sequence and emit invalid UTF-8.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > 80 {
		s = string(r[:77]) + "…"
	}
	return s
}
