package l7report

// Recorded mode (docs/dev/layout-gen/layout-debug.md): layout-gen can
// write a run — the trace events plus the resulting graph — as a JSON
// artifact, and the dev tools replay it with --in <path> instead of
// re-generating. ONE generation, MANY questions (--why, then --facts,
// then explain, all describing the SAME run); capture where re-running is
// impossible (another machine, a CI artifact, an ipm-rpc render a user
// reports on). Like every debug artifact it is THROWAWAY — temp/ only,
// never committed.

import (
	"encoding/json"
	"fmt"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layout7"
)

// RecordingVersion is bumped whenever the event vocabulary or graph shape
// changes in a way that would make an older recording narrate wrong. The
// version guards against narrating a stale recording with newer templates.
const RecordingVersion = 1

// Source locates the block a recording was made from.
type Source struct {
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
}

// Recording is a captured run: the decision trace plus the resulting
// graph. Deterministic (no timestamps) — event kinds and payload keys ARE
// the grep/diff contract, so two recordings diff clean and a recording
// diff doubles as a behaviour diff between engine versions.
type Recording struct {
	Version int                  `json:"version"`
	Source  Source               `json:"source,omitempty"`
	Events  []layout7.TraceEvent `json:"events"`
	Graph   *layout.Graph        `json:"graph"`
}

// NewRecording snapshots a report for serialization.
func NewRecording(r *Report, src Source) *Recording {
	return &Recording{
		Version: RecordingVersion,
		Source:  src,
		Events:  r.Events,
		Graph:   r.Graph,
	}
}

// JSON marshals the recording deterministically (indented; json.Marshal
// sorts map keys, so payloads are stable line-for-line).
func (rec *Recording) JSON() ([]byte, error) {
	out, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// DecodeRecording parses a .debug.json back into a Report. Event payloads
// round-trip through JSON as map[string]any with []interface{} slices;
// normalizeDecoded restores the []string / [][]string shapes the
// renderers type-assert on. A version mismatch is a hard error —
// narrating a stale recording with the current templates is worse than
// refusing.
func DecodeRecording(data []byte) (*Report, error) {
	var rec Recording
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse debug.json: %w", err)
	}
	if rec.Version != RecordingVersion {
		return nil, fmt.Errorf("debug.json is version %d but this build narrates version %d — re-record with the current layout-gen", rec.Version, RecordingVersion)
	}
	for i := range rec.Events {
		rec.Events[i].Data = normalizeDecoded(rec.Events[i].Data)
	}
	return &Report{Events: rec.Events, Graph: rec.Graph}, nil
}

// normalizeDecoded restores the concrete slice types the renderers assert
// on ([]string, [][]string) from the []interface{} that JSON decoding
// produces. It works by SHAPE, not by key, so a new slice payload
// round-trips without a change here. (Numbers stay float64 — num() reads
// them the same, and %v prints whole floats like the ints they were.)
func normalizeDecoded(m map[string]any) map[string]any {
	for k, v := range m {
		m[k] = normalizeAny(v)
	}
	return m
}

func normalizeAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return normalizeDecoded(t)
	case []any:
		conv := make([]any, len(t))
		allStr, allStrSlice := len(t) > 0, len(t) > 0
		for i, e := range t {
			conv[i] = normalizeAny(e)
			if _, ok := conv[i].(string); !ok {
				allStr = false
			}
			if _, ok := conv[i].([]string); !ok {
				allStrSlice = false
			}
		}
		switch {
		case allStr:
			out := make([]string, len(conv))
			for i, e := range conv {
				out[i] = e.(string)
			}
			return out
		case allStrSlice:
			out := make([][]string, len(conv))
			for i, e := range conv {
				out[i] = e.([]string)
			}
			return out
		default:
			return conv
		}
	default:
		return v
	}
}
